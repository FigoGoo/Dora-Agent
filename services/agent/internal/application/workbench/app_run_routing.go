package workbench

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	runtimestream "github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
	runtimeeino "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
	runtimeguide "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/guide"
	runtimerouter "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/router"
	runtimeskillgraph "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skillgraph"
)

func (a *App) recordRouterRunEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, req CreateRunRequest, traceID string) error {
	if a.gateway == nil {
		return apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	skills, _, err := a.gateway.ListRoutableSkills(ctx, auth, "", 20, "", traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "business_rpc", "error_code": "SKILL_CATALOG_UNAVAILABLE", "user_message": "Skill Catalog 暂不可用",
			"retryable": true, "support_trace_id": traceID,
		})
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SKILL_CATALOG_UNAVAILABLE", "skill catalog unavailable")
		return mapBusinessError(err)
	}
	catalog := creativeRouterCatalog(skills)
	switch req.RunIntent {
	case RunIntentEntryGuide:
		guide := runtimeguide.Build(run.SessionID, creativeGuideCatalog(catalog))
		if err := a.appendRouterGuide(ctx, run, guide, traceID); err != nil {
			return err
		}
		if err := a.createRouterAssistantMessage(ctx, run, "我已根据当前空间的可用 Skill 准备好创作建议，你可以直接选择一个方向开始。", map[string]any{
			"message_kind": "creative_guide",
			"guide_id":     guide.GuideID,
		}, traceID); err != nil {
			return err
		}
		return a.completeRouterRun(ctx, run, traceID, map[string]any{"router_result": "guide_presented", "guide_id": guide.GuideID})
	case RunIntentCapabilityQuestion:
		guide := runtimeguide.Build(run.SessionID, creativeGuideCatalog(catalog))
		if err := a.createRouterAssistantMessage(ctx, run, capabilityAnswer(catalog), map[string]any{
			"message_kind": "capability_answer",
			"skill_count":  len(catalog),
		}, traceID); err != nil {
			return err
		}
		if err := a.appendRouterGuide(ctx, run, guide, traceID); err != nil {
			return err
		}
		return a.completeRouterRun(ctx, run, traceID, map[string]any{"router_result": "capability_answered"})
	default:
		selectedSkillID, selectedListingID := selectedSkillFromControls(req.ControlInputs)
		return a.routeAndDispatch(ctx, auth, run, routeDispatchInput{
			Prompt:            req.UserInput.Text,
			RunIntent:         req.RunIntent,
			SelectedSkillID:   selectedSkillID,
			SelectedListingID: selectedListingID,
			Catalog:           catalog,
		}, traceID)
	}
}

type routeDispatchInput struct {
	Prompt            string
	RunIntent         string
	SelectedSkillID   string
	SelectedListingID string
	Catalog           []runtimerouter.CatalogSkill
}

// routeAndDispatch 执行一次意图路由并派发决策分支；
// CreateRun 首轮与 AppendUserInput 澄清续跑共用此入口。
func (a *App) routeAndDispatch(ctx context.Context, auth AuthContextDTO, run *model.Run, in routeDispatchInput, traceID string) error {
	routed, err := a.chatRouter.Route(ctx, runtimerouter.Input{
		UserInput:         in.Prompt,
		RunIntent:         in.RunIntent,
		SelectedSkillID:   in.SelectedSkillID,
		SelectedListingID: in.SelectedListingID,
		Catalog:           in.Catalog,
	})
	if err != nil {
		return err
	}
	decision := routed.Decision
	if err := foundation.ValidateRouterDecision(decision); err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "ROUTER_DECISION_INVALID", err.Error())
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "agent_runtime", "error_code": "ROUTER_DECISION_INVALID", "user_message": "路由结果不符合契约",
			"retryable": true, "support_trace_id": traceID,
		})
		return apperror.New(apperror.CodeInternal, "router decision is invalid")
	}
	decisionDigest, err := foundation.CanonicalDigest(decision)
	if err != nil {
		return err
	}
	if err := a.persistRouterDecision(ctx, run, in.RunIntent, decision, decisionDigest, in.Catalog); err != nil {
		return err
	}
	if err := a.appendRouterDecision(ctx, run, decision, decisionDigest, traceID); err != nil {
		return err
	}
	switch decision.Decision {
	case foundation.RouterDecisionSelectSkill, foundation.RouterDecisionGenericCreation:
		if err := a.appendRouterSkillSelected(ctx, run, decision, decisionDigest, traceID); err != nil {
			return err
		}
		if decision.RequiresSkillUsageConfirmation {
			return a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusWaitingConfirmation, "", "")
		}
		if decision.Decision == foundation.RouterDecisionGenericCreation {
			return a.startBoardRuntime(ctx, auth, run, in.Prompt, decisionDigest, traceID)
		}
		spec, err := a.loadSkillGraphPublishedSkillSpec(ctx, auth, decision, in.Catalog, traceID)
		if err != nil {
			_ = a.appendSkillMissingEvent(ctx, run, traceID, "skill_spec_unavailable", err.Error())
			_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SKILL_SPEC_UNAVAILABLE", "published skill spec unavailable")
			return err
		}
		return a.startSkillGraphRuntime(ctx, auth, run, in.Prompt, decision, decisionDigest, spec, traceID)
	case foundation.RouterDecisionClarify:
		question := strings.TrimSpace(routed.ClarifyQuestion)
		if question == "" {
			question = clarifyMessage(decision)
		}
		if err := a.createRouterAssistantMessage(ctx, run, question, map[string]any{
			"message_kind":           "clarify",
			"missing_fields":         decision.MissingFields,
			"suggested_questions":    routed.SuggestedQuestions,
			"router_source":          routed.Source,
			"router_decision_digest": decisionDigest,
		}, traceID); err != nil {
			return err
		}
		return a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusWaitingInput, "", "")
	case foundation.RouterDecisionCapabilityAnswer, foundation.RouterDecisionTextAnswer:
		if err := a.createRouterAssistantMessage(ctx, run, capabilityAnswer(in.Catalog), map[string]any{
			"message_kind":           decision.Decision,
			"router_decision_digest": decisionDigest,
		}, traceID); err != nil {
			return err
		}
		return a.completeRouterRun(ctx, run, traceID, map[string]any{"router_result": decision.Decision, "router_decision_digest": decisionDigest})
	case foundation.RouterDecisionReject:
		if err := a.createRouterAssistantMessage(ctx, run, "这个请求暂时无法继续，请调整创作目标后再试。", map[string]any{
			"message_kind":           "reject",
			"router_decision_digest": decisionDigest,
		}, traceID); err != nil {
			return err
		}
		return a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "ROUTER_REJECTED", "router rejected user input")
	default:
		return a.completeRouterRun(ctx, run, traceID, map[string]any{"router_result": decision.Decision, "router_decision_digest": decisionDigest})
	}
}

const maxClarifyRounds = 3

// continueClarifiedRouterRun 处理 clarify 后的追加输入：合并该 run 的全部用户输入重新路由；
// 澄清满 maxClarifyRounds 轮后不再追问，直接进入内置场景引导（generic_creation Graph）。
func (a *App) continueClarifiedRouterRun(ctx context.Context, auth AuthContextDTO, run *model.Run, traceID string) error {
	skills, _, err := a.gateway.ListRoutableSkills(ctx, auth, "", 20, "", traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "business_rpc", "error_code": "SKILL_CATALOG_UNAVAILABLE", "user_message": "Skill Catalog 暂不可用",
			"retryable": true, "support_trace_id": traceID,
		})
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SKILL_CATALOG_UNAVAILABLE", "skill catalog unavailable")
		return mapBusinessError(err)
	}
	merged, err := a.mergedRunUserPrompt(ctx, run)
	if err != nil {
		return err
	}
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRouting, "", ""); err != nil {
		return err
	}
	rounds, err := a.clarifyRounds(ctx, run)
	if err != nil {
		return err
	}
	if rounds >= maxClarifyRounds {
		// 引导兜底：不再追问，进入内置 L0 场景引导 Graph。
		digest, digestErr := foundation.CanonicalDigest(map[string]any{"clarify_rounds": rounds, "prompt": merged})
		if digestErr != nil {
			return digestErr
		}
		return a.startBoardRuntime(ctx, auth, run, merged, digest, traceID)
	}
	return a.routeAndDispatch(ctx, auth, run, routeDispatchInput{
		Prompt:    merged,
		RunIntent: RunIntentNormal,
		Catalog:   creativeRouterCatalog(skills),
	}, traceID)
}

// mergedRunUserPrompt 汇总该 run 的全部用户输入（按 sequence 升序），作为续跑路由的上下文。
func (a *App) mergedRunUserPrompt(ctx context.Context, run *model.Run) (string, error) {
	messages, err := a.repo.ListMessages(ctx, run.SessionID, 100, 0)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 4)
	for _, message := range messages {
		if message.RunID != run.ID || message.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(message.Content); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", apperror.New(apperror.CodeStateConflict, "run has no user input to resume")
	}
	return strings.Join(parts, "\n"), nil
}

// clarifyRounds 统计该 run 已发出的澄清轮数（assistant clarify 消息数）。
func (a *App) clarifyRounds(ctx context.Context, run *model.Run) (int, error) {
	messages, err := a.repo.ListMessages(ctx, run.SessionID, 100, 0)
	if err != nil {
		return 0, err
	}
	rounds := 0
	for _, message := range messages {
		if message.RunID != run.ID || message.Role != "assistant" {
			continue
		}
		summary := jsonMapFromString(string(message.ContentSummary))
		if summary != nil && summary["message_kind"] == "clarify" {
			rounds++
		}
	}
	return rounds, nil
}

func (a *App) loadSkillGraphPublishedSkillSpec(ctx context.Context, auth AuthContextDTO, decision foundation.RouterDecision, catalog []runtimerouter.CatalogSkill, traceID string) (SkillSpecDTO, error) {
	skillID := stringPtrValue(decision.SkillID)
	if skillID == "" {
		return SkillSpecDTO{}, apperror.New(apperror.CodeInvalidArgument, "selected skill_id is required")
	}
	version := ""
	for _, skill := range catalog {
		if skill.SkillID == skillID {
			version = skill.SkillVersion
			break
		}
	}
	return a.gateway.GetPublishedSkillSpec(ctx, auth, skillID, version, traceID)
}

func (a *App) startSkillGraphRuntime(ctx context.Context, auth AuthContextDTO, run *model.Run, prompt string, decision foundation.RouterDecision, routerDecisionDigest string, spec SkillSpecDTO, traceID string) error {
	runner, err := runtimeeino.NewSkillGraphRunner(ctx, nil)
	if err != nil {
		return err
	}
	result, err := runner.Execute(ctx, runtimeskillgraph.Input{
		RunID:                run.ID,
		ProjectID:            run.ProjectID,
		SessionID:            run.SessionID,
		SpaceID:              run.SpaceID,
		ActorUserID:          auth.ActorUserID,
		TraceID:              traceID,
		Prompt:               prompt,
		SkillID:              spec.SkillID,
		SkillVersion:         spec.Version,
		SkillSource:          stringPtrValue(decision.SkillSource),
		SkillSpecJSON:        spec.SkillSpecJSON,
		OutputElements:       skillGraphOutputElements(spec.OutputElements),
		RouterDecisionDigest: routerDecisionDigest,
	})
	if err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SKILL_RUNTIME_FAILED", err.Error())
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "agent_runtime", "error_code": "SKILL_RUNTIME_FAILED", "user_message": "Skill Graph 编译失败",
			"retryable": true, "support_trace_id": traceID,
		})
		return err
	}
	firstSeq, err := a.nextBoardGraphAGUISequence(ctx, run)
	if err != nil {
		return err
	}
	events, err := rebaseBoardGraphAGUIEvents(result.Events, firstSeq)
	if err != nil {
		return err
	}
	if err := a.repo.SaveBoardGraphForWorkbenchRun(ctx, run, result.GraphTemplate, result.GraphPlan, result.Board, result.Elements, events, routerDecisionDigest, map[string]any{
		"skill_spec_digest": result.SkillSpecDigest,
		"skill_runtime":     "published_skill_graph",
	}); err != nil {
		return err
	}
	a.publishBoardGraphAGUIEvents(ctx, events)
	if a.snapshotCache != nil {
		if body, err := json.Marshal(result.Snapshot); err == nil {
			_ = a.snapshotCache.Set(ctx, runtimestream.BoardSnapshotKey(result.Board.BoardID, result.Board.Version), body, 30*time.Minute)
		}
	}
	return nil
}

func (a *App) startBoardRuntime(ctx context.Context, auth AuthContextDTO, run *model.Run, prompt string, routerDecisionDigest string, traceID string) error {
	runner, err := runtimeeino.NewGenericCreationGraphRunner(ctx, nil)
	if err != nil {
		return err
	}
	result, err := runner.Execute(ctx, creation.GenericCreationInput{
		RunID:                run.ID,
		ProjectID:            run.ProjectID,
		SessionID:            run.SessionID,
		SpaceID:              run.SpaceID,
		ActorUserID:          auth.ActorUserID,
		TraceID:              traceID,
		Prompt:               prompt,
		RouterDecisionDigest: routerDecisionDigest,
	})
	if err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "BOARD_RUNTIME_FAILED", err.Error())
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "agent_runtime", "error_code": "BOARD_RUNTIME_FAILED", "user_message": "Creative Board 初始化失败",
			"retryable": true, "support_trace_id": traceID,
		})
		return err
	}
	firstSeq, err := a.nextBoardGraphAGUISequence(ctx, run)
	if err != nil {
		return err
	}
	events, err := rebaseBoardGraphAGUIEvents(result.Events, firstSeq)
	if err != nil {
		return err
	}
	if err := a.repo.SaveBoardGraphForWorkbenchRun(ctx, run, result.GraphTemplate, result.GraphPlan, result.Board, result.Elements, events, routerDecisionDigest, nil); err != nil {
		return err
	}
	a.publishBoardGraphAGUIEvents(ctx, events)
	if a.snapshotCache != nil {
		if body, err := json.Marshal(result.Snapshot); err == nil {
			_ = a.snapshotCache.Set(ctx, runtimestream.BoardSnapshotKey(result.Board.BoardID, result.Board.Version), body, 30*time.Minute)
		}
	}
	return nil
}

func skillGraphOutputElements(items []SkillOutputElementDTO) []runtimeskillgraph.OutputElement {
	out := make([]runtimeskillgraph.OutputElement, 0, len(items))
	for _, item := range items {
		out = append(out, runtimeskillgraph.OutputElement{
			ElementType:  item.ElementType,
			ElementName:  item.ElementName,
			Required:     item.Required,
			UseDraft:     item.UseDraft,
			UseFinal:     item.UseFinal,
			Editable:     item.Editable,
			Referable:    item.Referable,
			DisplayOrder: item.DisplayOrder,
			DisplaySlot:  item.DisplaySlot,
			SchemaJSON:   item.SchemaJSON,
		})
	}
	return out
}

func (a *App) nextBoardGraphAGUISequence(ctx context.Context, run *model.Run) (int64, error) {
	session, err := a.repo.GetSession(ctx, run.SessionID)
	if err != nil {
		return 0, err
	}
	return session.LastEventSequence + 1, nil
}

func rebaseBoardGraphAGUIEvents(events []foundation.AGUIEnvelope, firstSeq int64) ([]foundation.AGUIEnvelope, error) {
	if len(events) == 0 {
		return nil, apperror.New(apperror.CodeInternal, "board graph AG-UI events are required")
	}
	out := make([]foundation.AGUIEnvelope, 0, len(events))
	for index, event := range events {
		event.Seq = firstSeq + int64(index)
		event.DedupeKey = foundation.DedupeKey(event.RunID, event.EventType, event.Seq)
		if err := foundation.ValidateAGUIEnvelope(event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (a *App) appendRouterGuide(ctx context.Context, run *model.Run, guide runtimeguide.CreativeGuideOutput, traceID string) error {
	digest, err := foundation.CanonicalDigest(guide)
	if err != nil {
		return err
	}
	return a.appendRunEvent(ctx, run, "creative.guide.presented", traceID, map[string]any{
		"guide_id":       guide.GuideID,
		"creative_guide": guide,
		"payload_digest": digest,
	})
}

func (a *App) appendRouterDecision(ctx context.Context, run *model.Run, decision foundation.RouterDecision, digest string, traceID string) error {
	return a.appendRunEvent(ctx, run, "creative.router.decided", traceID, map[string]any{
		"router_decision": decision,
		"payload_digest":  digest,
	})
}

func (a *App) appendRouterSkillSelected(ctx context.Context, run *model.Run, decision foundation.RouterDecision, digest string, traceID string) error {
	skillID := stringPtrValue(decision.SkillID)
	if skillID == "" {
		return nil
	}
	return a.appendRunEvent(ctx, run, "agent.skill.selected", traceID, map[string]any{
		"skill_id":               skillID,
		"skill_source":           stringPtrValue(decision.SkillSource),
		"listing_id":             stringPtrValue(decision.ListingID),
		"confidence":             decision.Confidence,
		"reason_code":            decision.ReasonCode,
		"router_decision_digest": digest,
		"requires_confirmation":  decision.RequiresSkillUsageConfirmation,
		"entitlement_status":     stringPtrValue(decision.EntitlementStatus),
		"safe_to_execute":        false,
		"next_step":              "await_next_stage",
	})
}

func (a *App) persistRouterDecision(ctx context.Context, run *model.Run, runIntent string, decision foundation.RouterDecision, digest string, catalog []runtimerouter.CatalogSkill) error {
	skillID := stringPtrValue(decision.SkillID)
	skillVersion := ""
	skillSource := stringPtrValue(decision.SkillSource)
	for _, skill := range catalog {
		if skill.SkillID == skillID {
			skillVersion = skill.SkillVersion
			if skillSource == "" {
				skillSource = skill.SkillSource
			}
			break
		}
	}
	snapshot := map[string]any{
		"run_intent":                        runIntent,
		"router_decision":                   decision,
		"router_decision_digest":            digest,
		"decision":                          decision.Decision,
		"skill_id":                          skillID,
		"skill_version":                     skillVersion,
		"skill_source":                      skillSource,
		"listing_id":                        stringPtrValue(decision.ListingID),
		"confidence":                        decision.Confidence,
		"requires_skill_usage_confirmation": decision.RequiresSkillUsageConfirmation,
		"entitlement_status":                stringPtrValue(decision.EntitlementStatus),
		"safe_to_execute":                   false,
	}
	return a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("skill_selection", jsonObject(snapshot)).Error
}

func (a *App) createRouterAssistantMessage(ctx context.Context, run *model.Run, content string, summary map[string]any, traceID string) error {
	sequence, err := a.repo.NextMessageSequence(ctx, run.SessionID)
	if err != nil {
		return err
	}
	message := &model.Message{
		ID:             securityID("msg_"),
		SessionID:      run.SessionID,
		RunID:          run.ID,
		Role:           "assistant",
		ContentType:    "text",
		Content:        content,
		ContentSummary: jsonObject(summary),
		Sequence:       sequence,
		TraceID:        traceID,
		Metadata:       jsonObject(map[string]any{"source": "creative_router"}),
	}
	if err := a.repo.CreateMessage(ctx, message); err != nil {
		return err
	}
	return a.appendRunEvent(ctx, run, "agent.message.completed", traceID, map[string]any{
		"message_id":       message.ID,
		"role":             "assistant",
		"content_type":     "text",
		"content":          content,
		"message_sequence": sequence,
		"content_summary":  summary,
	})
}

func (a *App) completeRouterRun(ctx context.Context, run *model.Run, traceID string, payload map[string]any) error {
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCompleted, "", ""); err != nil {
		return err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["run_status"] = state.RunStatusCompleted
	payload["completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	return a.appendRunEvent(ctx, run, "agent.run.completed", traceID, payload)
}

func creativeRouterCatalog(items []SkillSummaryDTO) []runtimerouter.CatalogSkill {
	out := make([]runtimerouter.CatalogSkill, 0, len(items))
	for _, item := range items {
		source := skillCatalogSource(item.SkillScope, item.RouteHints)
		out = append(out, runtimerouter.CatalogSkill{
			SkillID:              item.SkillID,
			SkillName:            item.SkillName,
			SkillSource:          source,
			ListingID:            strings.TrimSpace(item.RouteHints["listing_id"]),
			SkillVersion:         item.Version,
			Status:               item.Status,
			SupportedOutputTypes: splitRouteHints(firstNonEmpty(item.RouteHints["output_types"], item.RouteHints["output_type"])),
			RoutingExamples:      skillRoutingExamples(item.RouteHints),
			RouteHints:           item.RouteHints,
			PricingSummary:       jsonMapFromString(item.RouteHints["pricing_summary_json"]),
			CreatorSummary:       jsonMapFromString(item.RouteHints["creator_summary_json"]),
			EntitlementStatus:    firstNonEmpty(item.RouteHints["entitlement_status"], item.RouteHints["entitlement"]),
		})
	}
	return out
}

func creativeGuideCatalog(items []runtimerouter.CatalogSkill) []runtimeguide.CatalogSkill {
	out := make([]runtimeguide.CatalogSkill, 0, len(items))
	for _, item := range items {
		out = append(out, runtimeguide.CatalogSkill{
			SkillID:              item.SkillID,
			SkillName:            item.SkillName,
			SkillSource:          item.SkillSource,
			Status:               item.Status,
			SupportedOutputTypes: item.SupportedOutputTypes,
			RoutingExamples:      item.RoutingExamples,
		})
	}
	return out
}

func skillCatalogSource(scope string, hints map[string]string) string {
	source := firstNonEmpty(hints["skill_source"], scope)
	switch source {
	case foundation.SkillSourceSystemDefault, foundation.SkillSourceSystemBuiltin, foundation.SkillSourceInstalled, foundation.SkillSourceMarketplace:
		return source
	case "public", "default", "":
		return foundation.SkillSourceSystemDefault
	default:
		return source
	}
}

func skillRoutingExamples(hints map[string]string) []string {
	return splitRouteHints(firstNonEmpty(hints["intent_examples"], hints["routing_examples"], hints["example_prompt"], hints["intent"]))
}

func selectedSkillFromControls(inputs []ControlInputDTO) (string, string) {
	selectedSkillID := ""
	selectedListingID := ""
	for _, input := range inputs {
		value, _ := input.Value.(string)
		switch input.ControlID {
		case "selected_skill_id":
			selectedSkillID = strings.TrimSpace(value)
		case "selected_listing_id":
			selectedListingID = strings.TrimSpace(value)
		}
	}
	return selectedSkillID, selectedListingID
}

func capabilityAnswer(catalog []runtimerouter.CatalogSkill) string {
	names := make([]string, 0, len(catalog))
	for _, skill := range catalog {
		if skill.Status != "published" || skill.SkillID == "skill_generic_creation" {
			continue
		}
		if strings.TrimSpace(skill.SkillName) != "" {
			names = append(names, strings.TrimSpace(skill.SkillName))
		}
	}
	if len(names) == 0 {
		return "我可以先帮你整理创作 brief、澄清目标和推荐适合的 Skill。"
	}
	return "当前空间可用的创作能力包括：" + strings.Join(names, "、") + "。你可以描述目标、风格、时长和投放平台，我会先完成路由和澄清。"
}

func clarifyMessage(decision foundation.RouterDecision) string {
	if len(decision.MissingFields) == 0 {
		return "我需要再确认一些创作信息后才能继续。"
	}
	return "还需要补充：" + strings.Join(decision.MissingFields, "、") + "。"
}

func splitRouteHints(value string) []string {
	value = strings.NewReplacer("，", ",", "\n", ",", ";", ",", "；", ",").Replace(value)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func jsonMapFromString(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
