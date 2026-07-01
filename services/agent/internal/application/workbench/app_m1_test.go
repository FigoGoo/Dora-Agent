package workbench

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestM1EntryGuideRunEmitsGuideOnly(t *testing.T) {
	app, gateway := newM1App(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "m1 guide", IdempotencyKey: "idem-m1-session-guide",
	}, "trace-m1-guide")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "entry_guide", IdempotencyKey: "idem-m1-run-guide",
		UserInput: UserInputDTO{ClientMessageID: "cm_m1_guide", ContentType: "text", Text: "进入工作台"},
	}, "trace-m1-guide")
	if err != nil {
		t.Fatalf("create guide run: %v", err)
	}
	if run.Status != state.RunStatusCompleted {
		t.Fatalf("guide run should complete after presenting guide, got %#v", run)
	}
	events := m1Events(t, app, run.RunID)
	if !hasEvent(events, "creative.guide.presented") || hasEvent(events, "generation.progress") || hasEvent(events, "credits.estimated") {
		t.Fatalf("guide run should emit only M1 guide events, got %#v", m1EventTypes(events))
	}
	payload := m1Payload(t, events, "creative.guide.presented")
	guide := payload["creative_guide"].(map[string]any)
	if guide["schema_version"] != "creative_guide_output.v1" || len(guide["suggested_prompts"].([]any)) == 0 {
		t.Fatalf("guide payload is incomplete: %#v", payload)
	}
	if containsCall(gateway.calls, "ListAvailableGenerationModels") || containsCall(gateway.calls, "EstimateGenerationCredits") {
		t.Fatalf("M1 guide must not enter generation model or credit flow: %v", gateway.calls)
	}
}

func TestM1NormalRunRoutesSystemDefaultSkillWithoutToolOrCredit(t *testing.T) {
	app, gateway := newM1App(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "m1 route", IdempotencyKey: "idem-m1-session-route",
	}, "trace-m1-route")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "normal", IdempotencyKey: "idem-m1-run-route",
		UserInput: UserInputDTO{ClientMessageID: "cm_m1_route", ContentType: "text", Text: "帮我做一个杭州夏季文旅宣传视频，现代国风，30 秒"},
	}, "trace-m1-route")
	if err != nil {
		t.Fatalf("create route run: %v", err)
	}
	if run.Status != state.RunStatusCompleted {
		t.Fatalf("router-only run should complete after decision, got %#v", run)
	}
	events := m1Events(t, app, run.RunID)
	if !hasEvent(events, "creative.router.decided") || !hasEvent(events, "agent.skill.selected") {
		t.Fatalf("route run missing router or skill-selected event: %#v", m1EventTypes(events))
	}
	for _, forbidden := range []string{"generation.progress", "credits.estimated", "confirmation.required"} {
		if hasEvent(events, forbidden) {
			t.Fatalf("M1 router must not enter %s, events=%#v", forbidden, m1EventTypes(events))
		}
	}
	payload := m1Payload(t, events, "creative.router.decided")
	decision := m1Decision(t, payload)
	if decision.Decision != pr1.RouterDecisionSelectSkill || decision.SkillID == nil || *decision.SkillID != "skill_city_tourism_video" {
		t.Fatalf("unexpected router decision: %#v", decision)
	}
	if decision.SafeToExecute || decision.RequiresSkillUsageConfirmation {
		t.Fatalf("system default M1 skill route must not be executable or paid: %#v", decision)
	}
	persisted, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	selection := m1JSONMap(t, persisted.SkillSelection)
	if selection["skill_id"] != "skill_city_tourism_video" || selection["router_decision_digest"] == "" {
		t.Fatalf("router decision was not persisted in run skill_selection: %#v", selection)
	}
	if containsCall(gateway.calls, "ListAvailableGenerationModels") || containsCall(gateway.calls, "EstimateGenerationCredits") || containsCall(gateway.calls, "FreezeCredits") {
		t.Fatalf("M1 route must not call model/credit RPCs: %v", gateway.calls)
	}
}

func TestM1AmbiguousInputClarifiesWithoutGeneration(t *testing.T) {
	app, _ := newM1App(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "m1 clarify", IdempotencyKey: "idem-m1-session-clarify",
	}, "trace-m1-clarify")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "normal", IdempotencyKey: "idem-m1-run-clarify",
		UserInput: UserInputDTO{ClientMessageID: "cm_m1_clarify", ContentType: "text", Text: "帮我做一个产品宣传片，年轻一点"},
	}, "trace-m1-clarify")
	if err != nil {
		t.Fatalf("create clarify run: %v", err)
	}
	if run.Status != state.RunStatusWaitingInput {
		t.Fatalf("ambiguous M1 run should wait for user input, got %#v", run)
	}
	events := m1Events(t, app, run.RunID)
	if hasEvent(events, "generation.progress") || hasEvent(events, "credits.estimated") {
		t.Fatalf("clarify must stay before generation/credit flow: %#v", m1EventTypes(events))
	}
	decision := m1Decision(t, m1Payload(t, events, "creative.router.decided"))
	if decision.Decision != pr1.RouterDecisionClarify || len(decision.MissingFields) == 0 {
		t.Fatalf("ambiguous input should produce clarify decision: %#v", decision)
	}
	messages, err := app.repo.ListMessages(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if !m1HasAssistantMessage(messages) {
		t.Fatalf("clarify run should persist assistant message, got %#v", messages)
	}
}

func newM1App(t *testing.T) (*App, *recordingGateway) {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_agent_m1")
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
	}}
	return New(repository.New(db.DB), gateway, "m1-service"), gateway
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

func m1Events(t *testing.T, app *App, runID string) []model.Event {
	t.Helper()
	events, err := app.repo.ListEventsAfterSequence(t.Context(), runID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	return events
}

func m1Payload(t *testing.T, events []model.Event, eventType string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		return m1JSONMap(t, event.Payload)
	}
	t.Fatalf("event %s not found in %#v", eventType, m1EventTypes(events))
	return nil
}

func m1Decision(t *testing.T, payload map[string]any) pr1.RouterDecision {
	t.Helper()
	data, err := json.Marshal(payload["router_decision"])
	if err != nil {
		t.Fatalf("marshal router_decision: %v", err)
	}
	var decision pr1.RouterDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		t.Fatalf("decode router_decision: %v", err)
	}
	if err := pr1.ValidateRouterDecision(decision); err != nil {
		t.Fatalf("router_decision violates PR-1 contract: %v", err)
	}
	return decision
}

func m1JSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode json map: %v", err)
	}
	return out
}

func m1EventTypes(events []model.Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func m1HasAssistantMessage(messages []model.Message) bool {
	for _, message := range messages {
		if message.Role == "assistant" && message.Content != "" {
			return true
		}
	}
	return false
}
