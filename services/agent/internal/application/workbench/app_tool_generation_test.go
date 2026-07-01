package workbench

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestM4BoardApproveCreatesToolPlanAndConfirmationThenCommitsAsset(t *testing.T) {
	app, gateway := newM4ToolPlanApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{
		ProjectID: "prj_active_1001", InitialTitle: "m4 toolplan", IdempotencyKey: "idem-m4-session",
	}, "trace-m4")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: session.ProjectID, RunIntent: "normal", IdempotencyKey: "idem-m4-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_m4", ContentType: "text", Text: "帮我做一个杭州夏季文旅宣传视频，现代国风，30 秒"},
	}, "trace-m4")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	persisted, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	selection := m1JSONMap(t, persisted.SkillSelection)
	boardID := selection["current_board_id"].(string)

	approved, err := app.ApproveCreativeBoard(t.Context(), auth, boardID, ApproveCreativeBoardRequest{
		ApprovedBy: auth.ActorUserID, BoardVersion: 1, IdempotencyKey: "idem-m4-board-approve",
	}, "trace-m4")
	if err != nil {
		t.Fatalf("approve board: %v", err)
	}
	if approved.ToolPlan == nil {
		t.Fatalf("approve should create M4 ToolPlan: %#v", approved)
	}
	if approved.ToolPlan.Status != "confirmation_required" || !approved.ToolPlan.ConfirmationRequired {
		t.Fatalf("ToolPlan should wait for generation confirmation: %#v", approved.ToolPlan)
	}
	if approved.ToolPlan.BoardID != boardID || approved.ToolPlan.BoardVersion != 2 {
		t.Fatalf("ToolPlan should bind approved board version, got %#v", approved.ToolPlan)
	}
	if !containsCall(gateway.calls, "EstimateToolCredits") {
		t.Fatalf("M4 preflight must estimate Tool credits, calls=%v", gateway.calls)
	}
	stored, err := app.repo.GetToolPlanV1(t.Context(), approved.ToolPlan.ToolPlanID)
	if err != nil {
		t.Fatalf("get stored tool plan: %v", err)
	}
	if stored.ToolPlanDigest != approved.ToolPlan.ToolPlanDigest {
		t.Fatalf("stored tool plan digest mismatch, stored=%s response=%s", stored.ToolPlanDigest, approved.ToolPlan.ToolPlanDigest)
	}
	pr3Events, err := app.repo.ListRunEventsV1AfterSeq(t.Context(), run.RunID, 0, 20)
	if err != nil {
		t.Fatalf("list PR-3 events: %v", err)
	}
	if !hasRunEventV1(pr3Events, pr3.EventTypeCostDisclosureGenerationPresented) {
		t.Fatalf("missing cost_disclosure.generation.presented event: %#v", pr3Events)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get generation confirmation interrupt: %v", err)
	}
	var confirmation map[string]any
	if err := json.Unmarshal(interrupt.ConfirmationPayload, &confirmation); err != nil {
		t.Fatalf("decode confirmation payload: %v", err)
	}
	if confirmation["tool_plan_id"] != approved.ToolPlan.ToolPlanID || confirmation["tool_plan_digest"] != approved.ToolPlan.ToolPlanDigest {
		t.Fatalf("confirmation must bind ToolPlan digest, payload=%#v tool_plan=%#v", confirmation, approved.ToolPlan)
	}

	gateway.calls = nil
	accepted, err := app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-m4-toolplan-confirm",
	}, "trace-m4")
	if err != nil {
		t.Fatalf("accept ToolPlan confirmation: %v", err)
	}
	if accepted.Status != state.RunStatusCompleted {
		t.Fatalf("M4 confirmation should complete generation asset commit, got %#v", accepted)
	}
	if !containsSubsequence(gateway.calls, []string{"FreezeCredits", "PrepareGeneratedAssetObjects", "CommitGeneratedAssetAndCharge"}) {
		t.Fatalf("M4 confirmation should freeze, prepare and commit assets, calls=%v", gateway.calls)
	}
	if gateway.lastCommit.RunID != run.RunID || gateway.lastCommit.FreezeID == "" || len(gateway.lastCommit.Artifacts) == 0 {
		t.Fatalf("unexpected asset commit request: %#v", gateway.lastCommit)
	}
	finalEvents, err := app.repo.ListRunEventsV1AfterSeq(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list final PR-3 events: %v", err)
	}
	if !hasRunEventV1(finalEvents, pr3.EventTypeToolTaskUpdated) {
		t.Fatalf("missing tool.task.updated event: %#v", finalEvents)
	}
	if !hasRunEventV1(finalEvents, pr3.EventTypeAssetCommitUpdated) {
		t.Fatalf("missing asset.commit.updated event: %#v", finalEvents)
	}
}

func newM4ToolPlanApp(t *testing.T) (*App, *recordingGateway) {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_agent_m4_toolplan")
	baseMigrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, baseMigrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/agent/0001_agent_tool_plan_task.up.sql"))
	gateway := &recordingGateway{StaticGateway: StaticGateway{
		Auth:   AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
		Skills: []SkillSummaryDTO{{
			SkillID: "skill_city_tourism_video", SkillName: "城市文旅视频", SkillScope: "system_default", Version: "1.0.0", Status: "published",
			RouteHints: map[string]string{
				"keywords": "文旅宣传视频,城市文旅,旅游推广视频,宣传视频", "output_types": "video,storyboard",
				"intent": "城市文旅宣传视频", "priority": "80", "skill_source": "system_default", "entitlement": "available",
			},
		}},
		SkillSpec: SkillSpecDTO{
			SkillID:  "skill_city_tourism_video",
			Version:  "1.0.0",
			ToolRefs: []string{"model_generation:image"},
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
			OutputElements: []SkillOutputElementDTO{{
				ElementType: "image_ref", ElementName: "成片封面", Required: true, UseDraft: true, UseFinal: true,
				Editable: true, Referable: true, DisplayOrder: 1, DisplaySlot: "board",
			}},
			OutputSchemaJSON:           `{"type":"object"}`,
			ConfirmationPolicyJSON:     `{"requires_confirmation":false}`,
			ExecutionPolicySummaryJSON: `{"tool_refs":["model_generation:image"]}`,
			MemoryPolicyJSON:           `{"enabled":false}`,
		},
		Model: ModelSummaryDTO{
			ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true,
			PricingSnapshotID: "price_static_image", ResourceType: "image",
		},
		ModelSnapshot: ModelRuntimeSnapshotDTO{
			ModelID: "mdl_static_image", DisplayName: "Static Image", ResourceType: "image",
			PricingSnapshotID: "price_static_image", ProviderRuntimeRef: "static:local", TimeoutMS: 30000,
		},
		ToolEstimate: CreditEstimateDTO{
			EstimateID: "est_m4_toolplan", EstimatePoints: 10, AvailablePoints: 100,
			CreditAccountScope: "personal", CreditAccountID: "ca_personal_1001", PricingSnapshotID: "price_static_image",
			LineItems: []CreditEstimateLineItemDTO{{
				EstimateItemID: "est_item_m4_toolplan", ItemType: "model_generation", ToolName: "model_generation",
				ToolType: "image", ModelID: "mdl_static_image", ResourceType: "image", BillingUnit: "image", EstimatePoints: 10,
			}},
			ExpiresAt: "2026-07-01T11:00:00Z",
		},
		Freeze: FreezeCreditsDTO{FreezeID: "frz_m4_toolplan", FrozenPoints: 10, ExpiresAt: "2026-07-01T11:15:00Z"},
	}}
	return New(repository.New(db.DB), gateway, "m4-toolplan-service"), gateway
}

func (g *recordingGateway) ResolveDefaultModel(ctx context.Context, auth AuthContextDTO, resourceType string, traceID string) (ModelSummaryDTO, error) {
	g.record("ResolveDefaultModel")
	return g.StaticGateway.ResolveDefaultModel(ctx, auth, resourceType, traceID)
}

func (g *recordingGateway) ResolveGenerationModelSnapshot(ctx context.Context, auth AuthContextDTO, resourceType string, modelID string, pricingSnapshotID string, traceID string) (ModelRuntimeSnapshotDTO, error) {
	g.record("ResolveGenerationModelSnapshot")
	return g.StaticGateway.ResolveGenerationModelSnapshot(ctx, auth, resourceType, modelID, pricingSnapshotID, traceID)
}

func hasRunEventV1(events []model.RunEventRecord, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}
