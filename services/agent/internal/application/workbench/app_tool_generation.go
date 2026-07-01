package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
	"gorm.io/gorm"
)

func (a *App) ensureM4ToolPlanPreflight(ctx context.Context, auth AuthContextDTO, agentRun model.AgentRunRecord, board pr2.CreativeBoard, traceID string) (*pr3.ToolPlan, error) {
	if !board.ToolPlanAllowed || board.Status != "approved" {
		return nil, nil
	}
	if !a.repo.DB().Migrator().HasTable(&model.ToolPlanRecord{}) {
		return nil, nil
	}
	if existing, err := a.repo.GetToolPlanByBoardVersionV1(ctx, board.BoardID, board.Version); err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	run, err := a.repo.GetRun(ctx, agentRun.RunID)
	if err != nil {
		return nil, err
	}
	prompt, err := a.latestUserPrompt(ctx, run.SessionID)
	if err != nil {
		return nil, err
	}
	resourceType := m4ResourceTypeForBoard(board)
	defaultModel, err := a.gateway.ResolveDefaultModel(ctx, auth, resourceType, traceID)
	if err != nil {
		return nil, mapBusinessError(err)
	}
	snapshot, err := a.gateway.ResolveGenerationModelSnapshot(ctx, auth, resourceType, defaultModel.ModelID, defaultModel.PricingSnapshotID, traceID)
	if err != nil {
		return nil, mapBusinessError(err)
	}
	safety, err := a.recordPromptSafetyEvaluation(ctx, run, "m4_tool_generation_preflight", "prompt", run.ID, prompt, traceID)
	if err != nil {
		return nil, err
	}
	if safety.Result == "failed" || safety.Result == "blocked" {
		return nil, apperror.New(apperror.CodePermissionDenied, "prompt safety check failed")
	}
	estimate, err := a.gateway.EstimateToolCredits(ctx, auth, EstimateToolCreditsRequest{
		ProjectID: run.ProjectID,
		ToolUsageItems: []ToolUsageEstimateItemDTO{{
			ToolName: "model_generation", ToolType: snapshot.ResourceType, BillingUnit: defaultToolBillingUnit(snapshot.ResourceType), Quantity: 1,
			MetadataSummary: map[string]string{"run_id": run.ID, "board_id": board.BoardID, "board_version": fmt.Sprint(board.Version)},
		}},
		SafetyEvidence: safetyEvidenceToRPC(safety),
		IdempotencyKey: "toolplan:" + run.ID + ":" + board.BoardID + ":v" + fmt.Sprint(board.Version) + ":estimate",
	}, traceID)
	if err != nil {
		return nil, mapBusinessError(err)
	}
	if estimate.Insufficient {
		_ = a.appendRunEvent(ctx, run, "credits.insufficient", traceID, map[string]any{
			"estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
			"user_message": "积分不足，无法生成素材", "retryable": true,
			"credit_account_id": estimate.CreditAccountID, "estimate_id": estimate.EstimateID,
		})
		return nil, apperror.New(apperror.CodeStateConflict, "credit account has insufficient points")
	}
	plan, expiresAt, err := buildM4ToolPlan(run.ID, board, snapshot, estimate, prompt)
	if err != nil {
		return nil, err
	}
	precondition := pr3.ToolPlanPrecondition{
		BoardID: board.BoardID, BoardVersion: board.Version, BoardStatus: board.Status, GraphPlanID: stringPtrValue(board.GraphPlanID),
	}
	if err := pr3.ValidateToolPlanForApprovedBoard(precondition, plan); err != nil {
		return nil, err
	}
	if err := a.repo.SaveToolPlanV1(ctx, plan); err != nil {
		return nil, err
	}
	if err := a.appendM4GenerationCostDisclosure(ctx, run, plan, expiresAt, traceID); err != nil {
		return nil, err
	}
	if err := a.createM4ToolPlanConfirmation(ctx, run, plan, snapshot, estimate, safety, prompt, expiresAt, traceID); err != nil {
		return nil, err
	}
	return &plan, nil
}

func buildM4ToolPlan(runID string, board pr2.CreativeBoard, snapshot ModelRuntimeSnapshotDTO, estimate CreditEstimateDTO, prompt string) (pr3.ToolPlan, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := parseM4ExpiresAt(estimate.ExpiresAt, now.Add(15*time.Minute))
	items, total, err := buildM4ToolPlanItems(board, snapshot, estimate, prompt)
	if err != nil {
		return pr3.ToolPlan{}, time.Time{}, err
	}
	plan := pr3.ToolPlan{
		SchemaVersion:        pr3.SchemaVersionToolPlan,
		ToolPlanID:           "tpl_" + shortStableID(runID+":"+board.BoardID+":"+fmt.Sprint(board.Version)),
		RunID:                runID,
		BoardID:              board.BoardID,
		BoardVersion:         board.Version,
		GraphPlanID:          stringPtrValue(board.GraphPlanID),
		Status:               "confirmation_required",
		Items:                items,
		EstimatedCredits:     total,
		Currency:             pr3.CurrencyCredits,
		ConfirmationRequired: true,
		ExpiresAt:            &expiresAt,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	digest, err := pr1.CanonicalDigest(map[string]any{
		"tool_plan_id":        plan.ToolPlanID,
		"run_id":              plan.RunID,
		"board_id":            plan.BoardID,
		"board_version":       plan.BoardVersion,
		"graph_plan_id":       plan.GraphPlanID,
		"status":              plan.Status,
		"items":               plan.Items,
		"estimated_credits":   plan.EstimatedCredits,
		"currency":            plan.Currency,
		"expires_at":          expiresAt,
		"model_id":            snapshot.ModelID,
		"pricing_snapshot_id": snapshot.PricingSnapshotID,
	})
	if err != nil {
		return pr3.ToolPlan{}, time.Time{}, err
	}
	plan.ToolPlanDigest = digest
	return plan, expiresAt, nil
}

func buildM4ToolPlanItems(board pr2.CreativeBoard, snapshot ModelRuntimeSnapshotDTO, estimate CreditEstimateDTO, prompt string) ([]pr3.ToolPlanItem, int, error) {
	lines := estimate.LineItems
	if len(lines) == 0 {
		lines = []CreditEstimateLineItemDTO{{
			EstimateItemID: "est_item_" + shortStableID(estimate.EstimateID+":"+board.BoardID),
			ItemType:       "model_generation", ToolName: "model_generation", ToolType: snapshot.ResourceType,
			ModelID: snapshot.ModelID, ResourceType: snapshot.ResourceType, BillingUnit: defaultToolBillingUnit(snapshot.ResourceType),
			EstimatePoints: estimate.EstimatePoints,
		}}
	}
	items := make([]pr3.ToolPlanItem, 0, len(lines))
	total := 0
	for index, line := range lines {
		credits := line.EstimatePoints
		if credits <= 0 && len(lines) == 1 {
			credits = estimate.EstimatePoints
		}
		inputDigest, err := pr1.CanonicalDigest(map[string]any{
			"board_id": board.BoardID, "board_version": board.Version, "graph_plan_id": stringPtrValue(board.GraphPlanID),
			"prompt_digest": digestText(prompt), "estimate_item_id": line.EstimateItemID, "model_id": firstNonEmpty(line.ModelID, snapshot.ModelID),
		})
		if err != nil {
			return nil, 0, err
		}
		itemIDSeed := firstNonEmpty(line.EstimateItemID, fmt.Sprintf("%s:%d", estimate.EstimateID, index+1))
		item := pr3.ToolPlanItem{
			ToolPlanItemID:   "tpi_" + shortStableID(board.BoardID+":"+fmt.Sprint(board.Version)+":"+itemIDSeed),
			ToolID:           firstNonEmpty(line.ToolName, "model_generation"),
			ToolVersion:      firstNonEmpty(estimate.PricingSnapshotID, snapshot.PricingSnapshotID, "v1"),
			ResourceType:     firstNonEmpty(line.ResourceType, snapshot.ResourceType, pr3.ResourceTypeImage),
			Quantity:         1,
			EstimatedCredits: int(credits),
			InputDigest:      inputDigest,
		}
		items = append(items, item)
		total += int(credits)
	}
	return items, total, nil
}

func (a *App) appendM4GenerationCostDisclosure(ctx context.Context, run *model.Run, plan pr3.ToolPlan, expiresAt time.Time, traceID string) error {
	payload := pr3.GenerationCostDisclosurePayload{
		ToolPlanID: plan.ToolPlanID, ToolPlanDigest: plan.ToolPlanDigest, BoardID: plan.BoardID, BoardVersion: plan.BoardVersion,
		EstimatedCredits: plan.EstimatedCredits, Currency: plan.Currency, ExpiresAt: expiresAt,
	}
	if err := pr3.ValidateGenerationCostDisclosurePayload(payload); err != nil {
		return err
	}
	seq, err := a.nextPR2AGUISequence(ctx, run)
	if err != nil {
		return err
	}
	body := map[string]any{
		"tool_plan_id":       payload.ToolPlanID,
		"tool_plan_digest":   payload.ToolPlanDigest,
		"board_id":           payload.BoardID,
		"board_version":      payload.BoardVersion,
		"estimated_credits":  payload.EstimatedCredits,
		"currency":           payload.Currency,
		"expires_at":         payload.ExpiresAt,
		"skill_usage_status": payload.SkillUsageStatus,
	}
	payloadDigest, err := pr1.CanonicalDigest(body)
	if err != nil {
		return err
	}
	event, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortStableID(run.ID+":cost_disclosure_generation:"+plan.ToolPlanID),
		EventType:     pr3.EventTypeCostDisclosureGenerationPresented,
		ProjectID:     run.ProjectID,
		SpaceID:       run.SpaceID,
		ActorUserID:   run.UserID,
		SessionID:     run.SessionID,
		RunID:         run.ID,
		Seq:           seq,
		CreatedAt:     time.Now().UTC(),
		PayloadDigest: payloadDigest,
		TraceID:       traceID,
		Payload:       body,
	})
	if err != nil {
		return err
	}
	if err := a.repo.AppendRunEventsV1(ctx, []pr1.AGUIEnvelope{event}); err != nil {
		return err
	}
	a.publishPR2AGUIEvents(ctx, []pr1.AGUIEnvelope{event})
	return nil
}

func (a *App) createM4ToolPlanConfirmation(ctx context.Context, run *model.Run, plan pr3.ToolPlan, snapshot ModelRuntimeSnapshotDTO, estimate CreditEstimateDTO, safety *model.SafetyEvaluation, prompt string, expiresAt time.Time, traceID string) error {
	outputElements := a.m4OutputElementsForBoard(ctx, plan.BoardID)
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return a.createConfirmationInterrupt(ctx, run, "tool_generation_confirmation", "tool generation credit charge requires confirmation", map[string]any{
		"m4_flow":             "generation_asset_commit",
		"tool_plan_id":        plan.ToolPlanID,
		"tool_plan_digest":    plan.ToolPlanDigest,
		"board_id":            plan.BoardID,
		"board_version":       plan.BoardVersion,
		"graph_plan_id":       plan.GraphPlanID,
		"estimate_id":         estimate.EstimateID,
		"estimate_points":     int64(plan.EstimatedCredits),
		"points":              int64(plan.EstimatedCredits),
		"available_points":    estimate.AvailablePoints,
		"credit_account_id":   estimate.CreditAccountID,
		"pricing_snapshot_id": firstNonEmpty(estimate.PricingSnapshotID, snapshot.PricingSnapshotID),
		"model_snapshot":      snapshot,
		"estimate":            estimate,
		"safety_evidence":     safetyEvidenceToRPC(safety),
		"prompt_digest":       digestText(prompt),
		"output_elements":     outputElements,
	}, "Tool 生成费确认", "确认后将冻结 Tool 生成费，生成完成并保存资产后扣费", []string{"tool_generation", "credit_freeze", "asset_commit", "tool_plan:" + plan.ToolPlanID}, ttl, traceID)
}

func (a *App) startM4ToolTaskForConfirmation(ctx context.Context, run *model.Run, payload m4ConfirmationPayload, generationTask *model.Task, idempotencyKey string, traceID string) (*pr3.ToolTask, error) {
	if strings.TrimSpace(payload.ToolPlanID) == "" {
		return nil, nil
	}
	plan, err := a.repo.GetToolPlanV1(ctx, payload.ToolPlanID)
	if err != nil {
		return nil, err
	}
	if payload.ToolPlanDigest != "" && plan.ToolPlanDigest != payload.ToolPlanDigest {
		return nil, apperror.New(apperror.CodeStateConflict, "tool plan digest mismatch")
	}
	if len(plan.Items) == 0 {
		return nil, apperror.New(apperror.CodeStateConflict, "tool plan has no executable item")
	}
	item := plan.Items[0]
	timeoutMS := int(payload.ModelSnapshot.TimeoutMS)
	if timeoutMS < 1000 {
		timeoutMS = 30000
	}
	now := time.Now().UTC()
	task := pr3.ToolTask{
		SchemaVersion:  pr3.SchemaVersionToolTask,
		ToolTaskID:     "ttask_" + shortStableID(run.ID+":"+plan.ToolPlanID+":primary"),
		ToolPlanID:     plan.ToolPlanID,
		ToolPlanItemID: item.ToolPlanItemID,
		RunID:          run.ID,
		Status:         "running",
		Progress:       10,
		ProviderPolicy: pr3.ProviderPolicy{Mode: pr3.ProviderModeSync, TimeoutMS: timeoutMS, Retryable: true},
		IdempotencyKey: m4ToolTaskIdempotencyKey(run.ID, payload, idempotencyKey),
		InputDigest:    item.InputDigest,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := a.repo.SaveToolTaskV1(ctx, task); err != nil {
		return nil, err
	}
	if generationTask != nil {
		_ = a.updateGenerationTaskStage(ctx, generationTask, 12, "tool_task_running", map[string]any{
			"tool_task_id": task.ToolTaskID, "tool_plan_id": task.ToolPlanID, "tool_plan_digest": plan.ToolPlanDigest,
			"tool_plan_item_id": task.ToolPlanItemID,
		})
	}
	if err := a.appendM4ToolTaskUpdated(ctx, run, task, traceID); err != nil {
		return nil, err
	}
	return &task, nil
}

func (a *App) completeM4ToolTaskFromResult(ctx context.Context, run *model.Run, task *pr3.ToolTask, artifacts []modeltool.Artifact, traceID string) error {
	if task == nil {
		return nil
	}
	outputDigest, err := m4ArtifactOutputDigest(artifacts)
	if err != nil {
		return err
	}
	updated, err := a.repo.ApplyToolTaskCompletedEventV1(ctx, pr3.ToolTaskCompletedStreamEvent{
		Stream:         pr3.ToolTaskCompletedStream,
		DedupeKey:      "provider:" + task.ToolTaskID + ":" + outputDigest,
		ToolTaskID:     task.ToolTaskID,
		ProviderStatus: "succeeded",
		OutputDigest:   outputDigest,
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	return a.appendM4ToolTaskUpdated(ctx, run, updated, traceID)
}

func (a *App) appendM4ToolTaskUpdated(ctx context.Context, run *model.Run, task pr3.ToolTask, traceID string) error {
	payload := pr3.ToolTaskUpdatedPayload{
		ToolTaskID:   task.ToolTaskID,
		ToolPlanID:   task.ToolPlanID,
		Status:       task.Status,
		Progress:     task.Progress,
		OutputDigest: task.OutputDigest,
		ErrorCode:    task.ErrorCode,
	}
	if err := pr3.ValidateToolTaskUpdatedPayload(payload); err != nil {
		return err
	}
	body := map[string]any{
		"tool_task_id": payload.ToolTaskID,
		"tool_plan_id": payload.ToolPlanID,
		"status":       payload.Status,
		"progress":     payload.Progress,
		"error_code":   payload.ErrorCode,
	}
	if payload.OutputDigest != nil {
		body["output_digest"] = *payload.OutputDigest
	}
	return a.appendM4PR3AGUIEvent(ctx, run, pr3.EventTypeToolTaskUpdated, body, payload.ToolTaskID+":"+payload.Status+":"+fmt.Sprint(payload.Progress), traceID)
}

func (a *App) appendM4AssetCommitUpdatedFromTask(ctx context.Context, run *model.Run, task *model.Task, commit AssetCommitDTO, traceID string) error {
	if task == nil {
		return nil
	}
	detail := jsonMap(task.ProgressDetail)
	toolTaskID := stringFromMap(detail, "tool_task_id")
	if strings.TrimSpace(toolTaskID) == "" {
		return nil
	}
	payload := pr3.AssetCommitUpdatedPayload{
		ToolTaskID:        toolTaskID,
		CommitStatus:      m4CommitStatusForAGUI(commit.CommitStatus, len(commit.AssetRefs)),
		CommittedAssetIDs: m4CommittedAssetIDs(commit.AssetRefs),
		FailedAssetCount:  0,
	}
	body := map[string]any{
		"tool_task_id":        payload.ToolTaskID,
		"commit_status":       payload.CommitStatus,
		"committed_asset_ids": payload.CommittedAssetIDs,
		"failed_asset_count":  payload.FailedAssetCount,
	}
	return a.appendM4PR3AGUIEvent(ctx, run, pr3.EventTypeAssetCommitUpdated, body, toolTaskID+":"+payload.CommitStatus, traceID)
}

func (a *App) appendM4PR3AGUIEvent(ctx context.Context, run *model.Run, eventType string, payload map[string]any, seed string, traceID string) error {
	seq, err := a.nextPR2AGUISequence(ctx, run)
	if err != nil {
		return err
	}
	payloadDigest, err := pr1.CanonicalDigest(payload)
	if err != nil {
		return err
	}
	event, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortStableID(run.ID+":"+eventType+":"+seed),
		EventType:     eventType,
		ProjectID:     run.ProjectID,
		SpaceID:       run.SpaceID,
		ActorUserID:   run.UserID,
		SessionID:     run.SessionID,
		RunID:         run.ID,
		Seq:           seq,
		CreatedAt:     time.Now().UTC(),
		PayloadDigest: payloadDigest,
		TraceID:       traceID,
		Payload:       payload,
	})
	if err != nil {
		return err
	}
	if err := a.repo.AppendRunEventsV1(ctx, []pr1.AGUIEnvelope{event}); err != nil {
		return err
	}
	a.publishPR2AGUIEvents(ctx, []pr1.AGUIEnvelope{event})
	return nil
}

func (a *App) m4OutputElementsForBoard(ctx context.Context, boardID string) []SkillOutputElementDTO {
	snapshot, err := a.repo.GetBoardSnapshotV1(ctx, boardID)
	if err != nil || len(snapshot.Elements) == 0 {
		return []SkillOutputElementDTO{{ElementType: "image_ref", ElementName: "生成资产", Required: true, UseDraft: true, UseFinal: true, Editable: true, Referable: true, DisplayOrder: 1, DisplaySlot: "board"}}
	}
	out := make([]SkillOutputElementDTO, 0, len(snapshot.Elements))
	for _, element := range snapshot.Elements {
		out = append(out, SkillOutputElementDTO{
			ElementType: element.ElementType, ElementName: stringFromAny(element.Content["title"]), Required: true,
			UseDraft: true, UseFinal: true, Editable: true, Referable: true, DisplayOrder: int32(element.Position.Order), DisplaySlot: "board",
		})
	}
	return out
}

func m4ResourceTypeForBoard(board pr2.CreativeBoard) string {
	return pr3.ResourceTypeImage
}

func m4FreezeIdempotencyKey(runID string, payload m4ConfirmationPayload, fallback string) string {
	if strings.TrimSpace(payload.ToolPlanDigest) == "" || strings.TrimSpace(payload.CreditAccountID) == "" {
		return fallback + ":freeze"
	}
	return "toolplan:" + runID + ":" + payload.ToolPlanDigest + ":" + payload.CreditAccountID + ":freeze"
}

func m4ToolTaskIdempotencyKey(runID string, payload m4ConfirmationPayload, fallback string) string {
	if strings.TrimSpace(payload.ToolPlanDigest) == "" {
		return fallback + ":tool_task"
	}
	return "tooltask:" + runID + ":" + payload.ToolPlanDigest
}

func m4ArtifactOutputDigest(artifacts []modeltool.Artifact) (string, error) {
	items := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, map[string]any{
			"artifact_id":   artifact.ArtifactID,
			"resource_type": artifact.ResourceType,
			"element_type":  artifact.ElementType,
			"checksum":      artifact.Checksum,
			"size_bytes":    artifact.SizeBytes,
		})
	}
	return pr1.CanonicalDigest(items)
}

func m4CommittedAssetIDs(refs []CommittedAssetRefDTO) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.AssetID) != "" {
			out = append(out, ref.AssetID)
		}
	}
	return out
}

func m4CommitStatusForAGUI(status string, committedCount int) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed":
		return "failed"
	case "partially_committed", "partial", "partial_success":
		return "partially_committed"
	case "committed", "success", "succeeded":
		return "committed"
	default:
		if committedCount > 0 {
			return "committed"
		}
		return "failed"
	}
}

func parseM4ExpiresAt(raw string, fallback time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback.UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return fallback.UTC()
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func shortStableID(value string) string {
	digest := strings.TrimPrefix(digestText(value), "sha256:")
	if len(digest) > 16 {
		return digest[:16]
	}
	return digest
}
