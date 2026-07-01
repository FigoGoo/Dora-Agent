package http

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc"
)

func TestBusinessConfigHTTPAndRPC(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_catalog")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	projectApp := project.New(repo, guard, auditWriter)
	modelApp := modelconfig.New(repo)
	toolApp := toolpolicy.New(repo)
	skillApp := skillcatalog.New(repo)
	dictApp := assetdict.New(repo)
	router := NewRouter(RouterOptions{
		AccountSpace: accountApp, Admin: adminApp, Project: projectApp,
		Model: modelApp, Tool: toolApp, Skill: skillApp, Dictionary: dictApp,
	})

	userToken := loginUser(t, router, "user1001@dora.local", "local-user-change-me")
	models := requestJSON(t, router, http.MethodGet, "/api/models/generation?resource_type=image", userToken, "", nil)
	if len(models["data"].(map[string]any)["models"].([]any)) == 0 {
		t.Fatalf("expected generation model list: %#v", models)
	}
	tools := requestJSON(t, router, http.MethodGet, "/api/tools/bindable", userToken, "", nil)
	if len(tools["data"].(map[string]any)["items"].([]any)) < 3 {
		t.Fatalf("expected bindable tools: %#v", tools)
	}
	skills := requestJSON(t, router, http.MethodGet, "/api/skills", userToken, "", nil)
	if len(skills["data"].(map[string]any)["items"].([]any)) == 0 {
		t.Fatalf("expected skill list: %#v", skills)
	}
	elements := requestJSON(t, router, http.MethodGet, "/api/asset-element-types", userToken, "", nil)
	if got := len(elements["data"].(map[string]any)["element_types"].([]any)); got < 14 {
		t.Fatalf("expected 14 asset element types, got %d: %#v", got, elements)
	}
	firstElement := elements["data"].(map[string]any)["element_types"].([]any)[0].(map[string]any)
	for _, field := range []string{"resource_type", "status", "usage_stage", "draft_enabled", "final_enabled", "editable", "referable", "render_hint"} {
		if _, ok := firstElement[field]; !ok {
			t.Fatalf("asset element type missing %s: %#v", field, firstElement)
		}
	}

	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	for _, path := range []string{
		"/api/admin/models/providers",
		"/api/admin/models",
		"/api/admin/tools",
		"/api/admin/skills/system",
		"/api/admin/skills/reviews",
		"/api/admin/asset-element-types",
	} {
		requestJSON(t, router, http.MethodGet, path, adminToken, "", nil)
	}
	createdProvider := requestJSON(t, router, http.MethodPost, "/api/admin/models/providers", adminToken, "idem-catalog-provider-create", map[string]any{
		"provider_code": "catalog_patch_provider",
		"provider_name": "Catalog Patch Provider",
		"provider_type": "openai_compatible",
		"status":        "active",
		"base_url":      "https://example.com/v1",
		"config":        map[string]any{"secret_key_ref": "secret/catalog/provider"},
	})
	providerID := createdProvider["data"].(map[string]any)["provider_id"].(string)
	patchedProvider := requestJSON(t, router, http.MethodPatch, "/api/admin/models/providers/"+providerID, adminToken, "idem-catalog-provider-status", map[string]any{
		"status": "disabled",
	})
	patchedProviderData := patchedProvider["data"].(map[string]any)
	if patchedProviderData["provider_code"] != "catalog_patch_provider" ||
		patchedProviderData["provider_name"] != "Catalog Patch Provider" ||
		patchedProviderData["provider_type"] != "openai_compatible" ||
		patchedProviderData["base_url"] != "https://example.com/v1" ||
		patchedProviderData["secret_ref_status"] != "configured" ||
		patchedProviderData["status"] != "disabled" {
		t.Fatalf("provider PATCH dropped existing fields: %#v", patchedProviderData)
	}

	handler := rpc.NewHandler(accountApp, projectApp, modelApp, toolApp, skillApp, dictApp)
	authResp, err := handler.ResolveAuthContextFromToken(t.Context(), &businessagent.ResolveAuthContextFromTokenRequest{
		Authorization:   "Bearer " + userToken,
		ExpectedSpaceId: ptr("sp_personal_1001"),
		RequestMeta:     &businessagent.RequestMeta{RequestId: "req-catalog-token", TraceId: "trace-catalog-token", Source: "agent_service"},
	})
	if err != nil {
		t.Fatalf("resolve auth context from token: %v", err)
	}
	if authResp.AuthContext.ActorUserId != "usr_1001" || authResp.SpaceContext.SpaceId != "sp_personal_1001" {
		t.Fatalf("unexpected token auth response: %#v", authResp)
	}
	_, err = handler.ResolveAuthContextFromToken(t.Context(), &businessagent.ResolveAuthContextFromTokenRequest{
		Authorization:   "Bearer " + userToken,
		ExpectedSpaceId: ptr("sp_personal_1002"),
		RequestMeta:     &businessagent.RequestMeta{RequestId: "req-catalog-token-denied", TraceId: "trace-catalog-token-denied", Source: "agent_service"},
	})
	if codeOf(err) != bizerrors.CodeCrossSpaceDenied {
		t.Fatalf("expected CROSS_SPACE_DENIED for token expected space mismatch, got %v", err)
	}
	auth := authResp.AuthContext
	listSkills, err := handler.ListRoutableSkills(t.Context(), &businessagent.ListRoutableSkillsRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-skill"), PageSize: int32ptr(10)})
	if err != nil || len(listSkills.Skills) == 0 {
		t.Fatalf("list routable skills resp=%#v err=%v", listSkills, err)
	}
	spec, err := handler.GetPublishedSkillSpec(t.Context(), &businessagent.GetPublishedSkillSpecRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-spec"), SkillId: "sk_seed_storyboard"})
	if err != nil || len(spec.ToolRefs) == 0 || spec.ConfirmationPolicyJson == `{"requires_confirmation":false}` {
		t.Fatalf("get skill spec resp=%#v err=%v", spec, err)
	}
	tool, err := handler.CheckToolExecutionPolicy(t.Context(), &businessagent.CheckToolExecutionPolicyRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-tool"), ToolName: "image_generate", ToolType: "model_generation", ProjectId: "prj_active_1001"})
	if err != nil || !tool.Allowed || !tool.RequiresConfirmation {
		t.Fatalf("tool policy resp=%#v err=%v", tool, err)
	}
	def, err := handler.ResolveDefaultModel(t.Context(), &businessagent.ResolveDefaultModelRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-model-default"), ResourceType: "image"})
	if err != nil || def.ModelId == "" {
		t.Fatalf("default model resp=%#v err=%v", def, err)
	}
	snapshot, err := handler.ResolveGenerationModelSnapshot(t.Context(), &businessagent.ResolveGenerationModelSnapshotRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-model-snapshot"), ResourceType: "image", ModelId: def.ModelId, PricingSnapshotId: def.PricingSnapshotId})
	if err != nil || snapshot.ProviderRuntimeRef == "" {
		t.Fatalf("model snapshot resp=%#v err=%v", snapshot, err)
	}
	dict, err := handler.ListAssetElementTypes(t.Context(), &businessagent.ListAssetElementTypesRequest{AuthContext: auth, RequestMeta: rpcMeta("catalog-dict"), PageSize: int32ptr(50)})
	if err != nil || len(dict.ElementTypes) < 14 {
		t.Fatalf("dictionary resp=%#v err=%v", dict, err)
	}
	if dict.ElementTypes[0].UsageStage == "" || dict.ElementTypes[0].ResourceType == "" || dict.ElementTypes[0].RenderHint == nil {
		t.Fatalf("dictionary dropped asset element type catalog fields: %#v", dict.ElementTypes[0])
	}
	safetyEvidence := skillTestEvidenceJSON("skrun_catalog_001", "", "passed", "trace-catalog-skill-test")
	saved, err := handler.SaveSkillTestResult_(t.Context(), &businessagent.SaveSkillTestResultRequest{
		AuthContext: auth, RequestMeta: &businessagent.RequestMeta{RequestId: "req-catalog-skill-test", TraceId: "trace-catalog-skill-test", Source: "agent_service", IdempotencyKey: ptr("skill_test:skrun_catalog_001")},
		SkillId: "sk_seed_storyboard", VersionId: "skv_seed_storyboard_100", TestRunId: "skrun_catalog_001", Status: "passed",
		ActualElementsJson: `[{"element_type":"image.primary"}]`, SafetyEvidenceJson: ptr(safetyEvidence), AgentTraceId: "trace-catalog-skill-test",
	})
	if err != nil || !saved.Saved {
		t.Fatalf("save skill test result resp=%#v err=%v", saved, err)
	}
	replayed, err := handler.SaveSkillTestResult_(t.Context(), &businessagent.SaveSkillTestResultRequest{
		AuthContext: auth, RequestMeta: &businessagent.RequestMeta{RequestId: "req-catalog-skill-test-replay", TraceId: "trace-catalog-skill-test", Source: "agent_service", IdempotencyKey: ptr("skill_test:skrun_catalog_001")},
		SkillId: "sk_seed_storyboard", VersionId: "skv_seed_storyboard_100", TestRunId: "skrun_catalog_001", Status: "passed",
		ActualElementsJson: `[{"element_type":"image.primary"}]`, SafetyEvidenceJson: ptr(safetyEvidence), AgentTraceId: "trace-catalog-skill-test",
	})
	if err != nil || replayed.TestRunId != "skrun_catalog_001" {
		t.Fatalf("expected skill test replay resp=%#v err=%v", replayed, err)
	}
	_, err = handler.SaveSkillTestResult_(t.Context(), &businessagent.SaveSkillTestResultRequest{
		AuthContext: auth, RequestMeta: &businessagent.RequestMeta{RequestId: "req-catalog-skill-test-conflict", TraceId: "trace-catalog-skill-test", Source: "agent_service", IdempotencyKey: ptr("skill_test:skrun_catalog_001")},
		SkillId: "sk_seed_storyboard", VersionId: "skv_seed_storyboard_100", TestRunId: "skrun_catalog_001", Status: "failed",
		ActualElementsJson: `[{"element_type":"text.caption"}]`, SafetyEvidenceJson: ptr(skillTestEvidenceJSON("skrun_catalog_001", "", "passed", "trace-catalog-skill-test")), AgentTraceId: "trace-catalog-skill-test",
	})
	if codeOf(err) != bizerrors.CodeIdempotencyConflict {
		t.Fatalf("expected skill test idempotency conflict, got %v", err)
	}
	_, err = handler.SaveSkillTestResult_(t.Context(), &businessagent.SaveSkillTestResultRequest{
		AuthContext: auth, RequestMeta: &businessagent.RequestMeta{RequestId: "req-catalog-skill-test-no-evidence", TraceId: "trace-catalog-skill-test-2", Source: "agent_service", IdempotencyKey: ptr("skill_test:skrun_catalog_002")},
		SkillId: "sk_seed_storyboard", VersionId: "skv_seed_storyboard_100", TestRunId: "skrun_catalog_002", Status: "passed",
		ActualElementsJson: `[{"element_type":"image.primary"}]`, AgentTraceId: "trace-catalog-skill-test-2",
	})
	if codeOf(err) != bizerrors.CodeSafetyEvidenceInvalid {
		t.Fatalf("expected safety evidence validation error, got %v", err)
	}
}

func rpcMeta(traceID string) *businessagent.RequestMeta {
	return &businessagent.RequestMeta{RequestId: "req-" + traceID, TraceId: traceID, Source: "test"}
}

func int32ptr(value int32) *int32 {
	return &value
}

func skillTestEvidenceJSON(testRunID, testCaseID, result, traceID string) string {
	targetRefID := testRunID
	if testCaseID != "" {
		targetRefID = testCaseID
	}
	return fmt.Sprintf(`{"scene":"skill_test","target_type":"skill_test_prompt","target_ref_id":%q,"evaluated_object_digest":"sha256:test-prompt","policy_version":"local-skill-runtime","evidence_version":"2026-06-27","result":%q,"source_run_id":%q,"trace_id":%q,"expires_at":%q}`,
		targetRefID, result, testRunID, traceID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
}
