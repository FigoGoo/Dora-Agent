package workbench

import (
	"context"
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestM6IndependentToolChargeClosesRPCChain(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "m6", IdempotencyKey: "idem-m6-session"}, "trace-m6-tool")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-m6-run-tool-charge",
		UserInput: UserInputDTO{ClientMessageID: "cm_m6_tool", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-m6-tool")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if !containsCall(gateway.calls, "ListAvailableGenerationModels") {
		t.Fatalf("ListAvailableGenerationModels was not consumed by Agent gateway: %v", gateway.calls)
	}
	want := []string{"EstimateToolCredits", "FreezeCredits", "ChargeToolUsageCredits"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing independent tool billing RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !hasEvent(events, "tool.call.completed") || !hasEvent(events, "credits.charged") {
		t.Fatalf("tool charge events missing: %#v", events)
	}
}

func TestM6IndependentToolChargeFailureReleasesFreeze(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	gateway.chargeErr = errors.New("STATE_CONFLICT: duplicate estimate item")
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "m6 fail", IdempotencyKey: "idem-m6-session-fail"}, "trace-m6-tool-fail")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-m6-run-tool-charge-fail",
		UserInput: UserInputDTO{ClientMessageID: "cm_m6_tool_fail", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-m6-tool-fail")
	if err == nil {
		t.Fatal("expected charge failure")
	}
	want := []string{"EstimateToolCredits", "FreezeCredits", "ChargeToolUsageCredits", "ReleaseFrozenCredits"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing failure release RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
	run, err := app.repo.GetRunByIdempotencyKey(t.Context(), "idem-m6-run-tool-charge-fail")
	if err != nil {
		t.Fatalf("get failed run: %v", err)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.ID, 0, 100)
	if err != nil {
		t.Fatalf("list failed events: %v", err)
	}
	if !hasEvent(events, "credits.released") || !hasEvent(events, "tool.call.failed") {
		t.Fatalf("release/failure events missing: %#v", events)
	}
}

func TestM6SkillTestConsumesReviewCandidateRPC(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	result, err := app.RunSkillTestCase(t.Context(), auth, SkillTestCaseRequest{
		SkillID: "sk_review", VersionID: "skv_review", TestRunID: "skrun_m6", TestCaseID: "skcase_m6",
		IdempotencyKey: "skill_test:skrun_m6",
	}, "trace-m6-skilltest")
	if err != nil {
		t.Fatalf("run skill test case: %v", err)
	}
	if result.Status != "passed" || !result.Saved {
		t.Fatalf("unexpected skill test result: %#v", result)
	}
	want := []string{"GetReviewCandidateSkillSpec", "ListAssetElementTypes", "SaveSkillTestResult"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing skill test RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
}

func newM6ServiceApp(t *testing.T) (*App, *recordingGateway) {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_agent_m6_service")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	gateway := &recordingGateway{StaticGateway: StaticGateway{
		Auth:   AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
		Skills: []SkillSummaryDTO{{SkillID: "sk_tool", SkillName: "Tool Skill", SkillScope: "public", Version: "1.0.0", Status: "published", RouteHints: map[string]string{"intent": "lookup"}}},
		SkillSpec: SkillSpecDTO{
			SkillID: "sk_tool", Version: "1.0.0", SkillSpecJSON: `{"name":"tool"}`, OutputSchemaJSON: `{"type":"object"}`,
			ToolRefs: []string{"web_fetch:browser"}, ConfirmationPolicyJSON: `{"requires_confirmation":false}`,
		},
		Models: []ModelSummaryDTO{{ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true, PricingSnapshotID: "price_static_image", ResourceType: "image"}},
		Model:  ModelSummaryDTO{ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true, PricingSnapshotID: "price_static_image", ResourceType: "image"},
		ModelSnapshot: ModelRuntimeSnapshotDTO{
			ModelID: "mdl_static_image", DisplayName: "Static Image", ResourceType: "image", PricingSnapshotID: "price_static_image",
			ProviderRuntimeRef: "static:local", TimeoutMS: 30000,
		},
		ToolEstimate: CreditEstimateDTO{
			EstimateID: "est_tool_m6", EstimatePoints: 4, AvailablePoints: 100, CreditAccountScope: "personal", CreditAccountID: "ca_personal_1001",
			LineItems: []CreditEstimateLineItemDTO{{EstimateItemID: "est_item_tool_m6", ItemType: "tool_usage", ToolName: "web_fetch", ToolType: "browser", BillingUnit: "call", EstimatePoints: 4}},
			ExpiresAt: "2026-06-28T00:00:00Z",
		},
		Freeze: FreezeCreditsDTO{FreezeID: "frz_tool_m6", FrozenPoints: 4, ExpiresAt: "2026-06-28T00:15:00Z"},
		ToolCharge: ToolChargeDTO{
			ToolChargeID: "toolchg_m6", ChargedPoints: 4, ReleasedPoints: 0, FreezeStatus: "charged",
			LedgerEntryIDs:   []string{"cled_tool_m6"},
			ChargedLineItems: []ChargedLineItemDTO{{EstimateItemID: "est_item_tool_m6", ChargedPoints: 4, Status: "charged", ToolCallID: "tool_m6"}},
		},
		Estimate: CreditEstimateDTO{
			EstimateID: "est_generation_m6", EstimatePoints: 10, AvailablePoints: 100, CreditAccountScope: "personal", CreditAccountID: "ca_personal_1001",
			PricingSnapshotID: "price_static_image",
			LineItems:         []CreditEstimateLineItemDTO{{EstimateItemID: "est_item_generation_m6", ItemType: "model_generation", ModelID: "mdl_static_image", ResourceType: "image", BillingUnit: "image", EstimatePoints: 10}},
			ExpiresAt:         "2026-06-28T00:00:00Z",
		},
		ReviewSpec: ReviewCandidateSkillSpecDTO{
			SkillID: "sk_review", VersionID: "skv_review", SkillSpecJSON: `{"name":"review"}`,
			InputSchemaJSON: `{"type":"object"}`, OutputSchemaJSON: `{"required":["structured_object"]}`,
			ToolRefs: []string{"web_fetch:browser"}, MemoryPolicyJSON: `{"enabled":false}`,
			ConfirmationPolicyJSON: `{"requires_confirmation":false}`, TestInputJSON: `{"prompt":"safe"}`,
			ExpectedElementsJSON: `["structured_object"]`,
		},
		ElementTypes: []AssetElementTypeDTO{{
			ElementType: "structured_object", DisplayName: "Structured Object", Category: "data", SchemaVersion: "2026-06-28",
			SchemaHintJSON: `{"type":"object"}`, RenderHintJSON: `{"component":"json"}`, Active: true, ResourceType: "data",
			Status: "active", UsageStage: "draft_final", DraftEnabled: true, FinalEnabled: true, Editable: true, Referable: true,
		}},
	}}
	return New(repository.New(db.DB), gateway, "m6-service"), gateway
}

type recordingGateway struct {
	StaticGateway
	calls     []string
	chargeErr error
}

func (g *recordingGateway) record(call string) {
	g.calls = append(g.calls, call)
}

func (g *recordingGateway) ListAvailableGenerationModels(ctx context.Context, auth AuthContextDTO, resourceType string, limit int, cursor string, traceID string) ([]ModelSummaryDTO, string, error) {
	g.record("ListAvailableGenerationModels")
	return g.StaticGateway.ListAvailableGenerationModels(ctx, auth, resourceType, limit, cursor, traceID)
}

func (g *recordingGateway) GetReviewCandidateSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, versionID string, testCaseID string, testRunID string, traceID string) (ReviewCandidateSkillSpecDTO, error) {
	g.record("GetReviewCandidateSkillSpec")
	return g.StaticGateway.GetReviewCandidateSkillSpec(ctx, auth, skillID, versionID, testCaseID, testRunID, traceID)
}

func (g *recordingGateway) ListAssetElementTypes(ctx context.Context, auth AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]AssetElementTypeDTO, string, error) {
	g.record("ListAssetElementTypes")
	return g.StaticGateway.ListAssetElementTypes(ctx, auth, pageSize, schemaVersion, traceID)
}

func (g *recordingGateway) SaveSkillTestResult(ctx context.Context, auth AuthContextDTO, req SkillTestResultRequest, traceID string) (SkillTestResultDTO, error) {
	g.record("SaveSkillTestResult")
	return g.StaticGateway.SaveSkillTestResult(ctx, auth, req, traceID)
}

func (g *recordingGateway) EstimateToolCredits(ctx context.Context, auth AuthContextDTO, req EstimateToolCreditsRequest, traceID string) (CreditEstimateDTO, error) {
	g.record("EstimateToolCredits")
	return g.StaticGateway.EstimateToolCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) FreezeCredits(ctx context.Context, auth AuthContextDTO, req FreezeCreditsRequest, traceID string) (FreezeCreditsDTO, error) {
	g.record("FreezeCredits")
	return g.StaticGateway.FreezeCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) ChargeToolUsageCredits(ctx context.Context, auth AuthContextDTO, req ChargeToolUsageCreditsRequest, traceID string) (ToolChargeDTO, error) {
	g.record("ChargeToolUsageCredits")
	if g.chargeErr != nil {
		return ToolChargeDTO{}, g.chargeErr
	}
	return g.StaticGateway.ChargeToolUsageCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) ReleaseFrozenCredits(ctx context.Context, auth AuthContextDTO, req ReleaseFrozenCreditsRequest, traceID string) (ReleaseCreditsDTO, error) {
	g.record("ReleaseFrozenCredits")
	return g.StaticGateway.ReleaseFrozenCredits(ctx, auth, req, traceID)
}

func containsSubsequence(calls, want []string) bool {
	if len(want) == 0 {
		return true
	}
	pos := 0
	for _, call := range calls {
		if call == want[pos] {
			pos++
			if pos == len(want) {
				return true
			}
		}
	}
	return false
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func hasEvent(events []model.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
