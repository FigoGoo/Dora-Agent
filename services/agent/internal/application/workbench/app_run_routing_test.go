package workbench

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestRouterEntryGuideRunEmitsGuideOnly(t *testing.T) {
	app, gateway := newRouterApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "router guide", IdempotencyKey: "idem-router-session-guide",
	}, "trace-router-guide")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "entry_guide", IdempotencyKey: "idem-router-run-guide",
		UserInput: UserInputDTO{ClientMessageID: "cm_router_guide", ContentType: "text", Text: "进入工作台"},
	}, "trace-router-guide")
	if err != nil {
		t.Fatalf("create guide run: %v", err)
	}
	if run.Status != state.RunStatusCompleted {
		t.Fatalf("guide run should complete after presenting guide, got %#v", run)
	}
	events := routerEvents(t, app, run.RunID)
	if !hasEvent(events, "creative.guide.presented") || hasEvent(events, "generation.progress") || hasEvent(events, "credits.estimated") {
		t.Fatalf("guide run should emit only router guide events, got %#v", routerEventTypes(events))
	}
	payload := routerPayload(t, events, "creative.guide.presented")
	guide := payload["creative_guide"].(map[string]any)
	if guide["schema_version"] != "creative_guide_output.v1" || len(guide["suggested_prompts"].([]any)) == 0 {
		t.Fatalf("guide payload is incomplete: %#v", payload)
	}
	if containsCall(gateway.calls, "ListAvailableGenerationModels") || containsCall(gateway.calls, "EstimateGenerationCredits") {
		t.Fatalf("router guide must not enter generation model or credit flow: %v", gateway.calls)
	}
}

func TestSkillGraphNormalRunRoutesPublishedSkillAndCreatesBoardWithoutToolOrCredit(t *testing.T) {
	app, gateway := newRouterApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "router route", IdempotencyKey: "idem-router-session-route",
	}, "trace-router-route")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "normal", IdempotencyKey: "idem-router-run-route",
		UserInput: UserInputDTO{ClientMessageID: "cm_router_route", ContentType: "text", Text: "帮我做一个杭州夏季文旅宣传视频，现代国风，30 秒"},
	}, "trace-router-route")
	if err != nil {
		t.Fatalf("create route run: %v", err)
	}
	if run.Status != state.RunStatusWaitingConfirmation {
		t.Fatalf("Skill graph run should wait for Board review after Skill graph, got %#v", run)
	}
	events := routerEvents(t, app, run.RunID)
	if !hasEvent(events, "creative.router.decided") || !hasEvent(events, "agent.skill.selected") {
		t.Fatalf("route run missing router or skill-selected event: %#v", routerEventTypes(events))
	}
	for _, forbidden := range []string{"generation.progress", "credits.estimated", "confirmation.required"} {
		if hasEvent(events, forbidden) {
			t.Fatalf("router router must not enter %s, events=%#v", forbidden, routerEventTypes(events))
		}
	}
	payload := routerPayload(t, events, "creative.router.decided")
	decision := routerDecision(t, payload)
	if decision.Decision != foundation.RouterDecisionSelectSkill || decision.SkillID == nil || *decision.SkillID != "skill_city_tourism_video" {
		t.Fatalf("unexpected router decision: %#v", decision)
	}
	if decision.SafeToExecute || decision.RequiresSkillUsageConfirmation {
		t.Fatalf("system default router skill route must not be executable or paid: %#v", decision)
	}
	persisted, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	selection := jsonMapFromRaw(t, persisted.SkillSelection)
	boardID, _ := selection["current_board_id"].(string)
	graphPlanID, _ := selection["current_graph_plan_id"].(string)
	if selection["skill_id"] != "skill_city_tourism_video" || selection["router_decision_digest"] == "" || selection["skill_spec_digest"] == "" || boardID == "" || graphPlanID == "" {
		t.Fatalf("router decision and Skill graph metadata were not persisted in run skill_selection: %#v", selection)
	}
	board, err := app.repo.GetCreativeBoardV1(t.Context(), boardID)
	if err != nil {
		t.Fatalf("get created board: %v", err)
	}
	if board.Status != "ready" || board.Version != 1 || board.RunID != run.RunID || board.ToolPlanAllowed {
		t.Fatalf("unexpected board graph: %#v", board)
	}
	snapshot, err := app.repo.GetBoardSnapshotV1(t.Context(), boardID)
	if err != nil {
		t.Fatalf("get board snapshot: %v", err)
	}
	if snapshot.Status != "ready" || len(snapshot.Elements) == 0 {
		t.Fatalf("unexpected board graph snapshot: %#v", snapshot)
	}
	boardGraphEvents, err := app.repo.ListRunEventsV1AfterSeq(t.Context(), run.RunID, 0, 10)
	if err != nil {
		t.Fatalf("list board graph events: %v", err)
	}
	if len(boardGraphEvents) != 2 || boardGraphEvents[0].EventType != boardgraph.EventTypeGraphPlanCreated || boardGraphEvents[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("board graph should persist graph+board AG-UI events, got %#v", boardGraphEvents)
	}
	plan, err := app.repo.GetGraphPlanV1(t.Context(), graphPlanID)
	if err != nil {
		t.Fatalf("get graph plan: %v", err)
	}
	if plan.GraphTemplateID == "gtemplate_generic_creation" || plan.ValueDeliveredStage != boardgraph.ValueDeliveredStageStoryboardReady {
		t.Fatalf("expected compiled Skill graph plan, got %#v", plan)
	}
	replay, err := app.ReplayEvents(t.Context(), auth, run.RunID, 0, 20, "trace-router-route")
	if err != nil {
		t.Fatalf("replay merged events: %v", err)
	}
	if !hasReplayType(replay.Events, "creative.router.decided") || !hasReplayType(replay.Events, boardgraph.EventTypeGraphPlanCreated) || !hasReplayType(replay.Events, boardgraph.EventTypeBoardSnapshotUpdated) {
		t.Fatalf("merged replay missing router or board graph events: %#v", replay.Events)
	}
	if !containsCall(gateway.calls, "GetPublishedSkillSpec") {
		t.Fatalf("Skill graph route must load published Skill spec, calls=%v", gateway.calls)
	}
	if containsCall(gateway.calls, "ListAvailableGenerationModels") || containsCall(gateway.calls, "EstimateGenerationCredits") || containsCall(gateway.calls, "FreezeCredits") {
		t.Fatalf("Skill graph route must not call model/credit RPCs: %v", gateway.calls)
	}
}

func TestRouterAmbiguousInputClarifiesWithoutGeneration(t *testing.T) {
	app, _ := newRouterApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "router clarify", IdempotencyKey: "idem-router-session-clarify",
	}, "trace-router-clarify")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "normal", IdempotencyKey: "idem-router-run-clarify",
		UserInput: UserInputDTO{ClientMessageID: "cm_router_clarify", ContentType: "text", Text: "帮我做一个产品宣传片，年轻一点"},
	}, "trace-router-clarify")
	if err != nil {
		t.Fatalf("create clarify run: %v", err)
	}
	if run.Status != state.RunStatusWaitingInput {
		t.Fatalf("ambiguous router run should wait for user input, got %#v", run)
	}
	events := routerEvents(t, app, run.RunID)
	if hasEvent(events, "generation.progress") || hasEvent(events, "credits.estimated") {
		t.Fatalf("clarify must stay before generation/credit flow: %#v", routerEventTypes(events))
	}
	decision := routerDecision(t, routerPayload(t, events, "creative.router.decided"))
	if decision.Decision != foundation.RouterDecisionClarify || len(decision.MissingFields) == 0 {
		t.Fatalf("ambiguous input should produce clarify decision: %#v", decision)
	}
	messages, err := app.repo.ListMessages(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if !hasAssistantMessage(messages) {
		t.Fatalf("clarify run should persist assistant message, got %#v", messages)
	}
}

func newRouterApp(t *testing.T) (*App, *recordingGateway) {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_agent_router")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	gateway := &recordingGateway{StaticGateway: StaticGateway{
		Auth:   AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
		Skills: []SkillSummaryDTO{
			{
				SkillID: "skill_city_tourism_video", SkillName: "城市文旅视频", SkillScope: "system_default", Version: "1.0.0", Status: "published",
				RouteHints: map[string]string{
					"keywords":       "文旅宣传视频,城市文旅,旅游推广视频,宣传视频",
					"output_types":   "video,storyboard",
					"intent":         "城市文旅宣传视频",
					"priority":       "80",
					"skill_source":   "system_default",
					"entitlement":    "available",
					"example_prompt": "帮我做一个杭州文旅宣传视频",
				},
			},
			{
				SkillID: "skill_generic_creation", SkillName: "自由创作", SkillScope: "system_builtin", Version: "1.0.0", Status: "published",
				RouteHints: map[string]string{"keywords": "自由创作,通用创作", "output_types": "brief,prompt", "priority": "1", "skill_source": "system_builtin"},
			},
		},
		SkillSpec: SkillSpecDTO{
			SkillID:  "skill_city_tourism_video",
			Version:  "1.0.0",
			ToolRefs: []string{},
			SkillSpecJSON: `{
				"schema_version":"skill_runtime_spec.v1",
				"skill_id":"skill_city_tourism_video",
				"version":"1.0.0",
				"status":"published",
				"level":"L3",
				"scope":"system_default",
				"stages":["brief","storyboard","board_review"],
				"graph_template":{
					"entry_node":"brief_builder",
					"nodes":[
						{"node_key":"brief_builder","node_type":"llm","display_name":"生成 brief"},
						{"node_key":"storyboard_planner","node_type":"llm","display_name":"生成分镜"},
						{"node_key":"board_review_gate","node_type":"user_gate","display_name":"Board 审核"}
					],
					"edges":[
						{"from":"brief_builder","to":"storyboard_planner"},
						{"from":"storyboard_planner","to":"board_review_gate"}
					],
					"terminal_nodes":["board_review_gate"]
				}
			}`,
			OutputSchemaJSON:           `{"type":"object"}`,
			ConfirmationPolicyJSON:     `{"requires_confirmation":false}`,
			ExecutionPolicySummaryJSON: `{"tool_refs":[]}`,
			OutputElements:             []SkillOutputElementDTO{{ElementType: "storyboard_frame", ElementName: "分镜", Required: true, UseDraft: true, UseFinal: true, Editable: true, Referable: true, DisplayOrder: 1, DisplaySlot: "board"}},
			MemoryPolicyJSON:           `{"enabled":false}`,
		},
	}}
	return New(repository.New(db.DB), gateway, "router-service"), gateway
}

func (g *recordingGateway) ListRoutableSkills(ctx context.Context, auth AuthContextDTO, scopeFilter string, limit int, cursor string, traceID string) ([]SkillSummaryDTO, string, error) {
	g.record("ListRoutableSkills")
	return g.StaticGateway.ListRoutableSkills(ctx, auth, scopeFilter, limit, cursor, traceID)
}

func (g *recordingGateway) GetPublishedSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, version string, traceID string) (SkillSpecDTO, error) {
	g.record("GetPublishedSkillSpec")
	return g.StaticGateway.GetPublishedSkillSpec(ctx, auth, skillID, version, traceID)
}

func (g *recordingGateway) ResolveCurrentSpaceContext(ctx context.Context, auth AuthContextDTO, expectedSpaceID string, traceID string) (SpaceContextDTO, error) {
	g.record("ResolveCurrentSpaceContext")
	return g.StaticGateway.ResolveCurrentSpaceContext(ctx, auth, expectedSpaceID, traceID)
}

func routerEvents(t *testing.T, app *App, runID string) []model.Event {
	t.Helper()
	events, err := app.repo.ListEventsAfterSequence(t.Context(), runID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	return events
}

func routerPayload(t *testing.T, events []model.Event, eventType string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		return jsonMapFromRaw(t, event.Payload)
	}
	t.Fatalf("event %s not found in %#v", eventType, routerEventTypes(events))
	return nil
}

func routerDecision(t *testing.T, payload map[string]any) foundation.RouterDecision {
	t.Helper()
	data, err := json.Marshal(payload["router_decision"])
	if err != nil {
		t.Fatalf("marshal router_decision: %v", err)
	}
	var decision foundation.RouterDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		t.Fatalf("decode router_decision: %v", err)
	}
	if err := foundation.ValidateRouterDecision(decision); err != nil {
		t.Fatalf("router_decision violates foundation contract: %v", err)
	}
	return decision
}

func jsonMapFromRaw(t *testing.T, data []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode json map: %v", err)
	}
	return out
}

func routerEventTypes(events []model.Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func hasAssistantMessage(messages []model.Message) bool {
	for _, message := range messages {
		if message.Role == "assistant" && message.Content != "" {
			return true
		}
	}
	return false
}

func hasReplayType(events []EventDTO, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
