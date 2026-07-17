package contract_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const analyzeMaterialsRuntimeV2Preview1ApprovalManifestPath = "docs/design/agent/approvals/analyze_materials_runtime_v2preview1/approval_manifest.json"

type analyzeMaterialsRuntimeV2Preview1ApprovalManifest struct {
	SchemaVersion                    string                                                          `json:"schema_version"`
	Profile                          string                                                          `json:"profile"`
	Decision                         analyzeMaterialsRuntimeV2Preview1ApprovalDecision               `json:"decision"`
	GlobalProductionGate             analyzeMaterialsRuntimeV2Preview1GlobalProductionGate           `json:"global_production_gate"`
	Batches                          []analyzeMaterialsRuntimeV2Preview1ApprovalBatch                `json:"batches"`
	ActivationRequirements           []string                                                        `json:"activation_requirements"`
	ExclusivePreviewDirectories      []string                                                        `json:"exclusive_preview_directories"`
	ImplementationFileAllowlist      []string                                                        `json:"implementation_file_allowlist"`
	ForbiddenCapabilities            []string                                                        `json:"forbidden_capabilities"`
	ArtifactRefs                     []approvalArtifactRefV1                                         `json:"artifact_refs"`
	RequiredContractTests            []analyzeMaterialsRuntimeV2Preview1ApprovalRequiredContractTest `json:"required_contract_tests"`
	RequiredImplementationTests      []approvalRequiredTestV1                                        `json:"required_implementation_tests"`
	RequiredEvidenceBeforeActivation []string                                                        `json:"required_evidence_before_activation"`
	M4Evidence                       analyzeMaterialsRuntimeV2Preview1M4Evidence                     `json:"m4_evidence"`
	M4EvidenceStatus                 string                                                          `json:"m4_evidence_status"`
}

type analyzeMaterialsRuntimeV2Preview1ApprovalDecision struct {
	SelectedProfile string `json:"selected_profile"`
	Status          string `json:"status"`
	ApprovedAt      string `json:"approved_at"`
	ApprovalBasis   string `json:"approval_basis"`
}

type analyzeMaterialsRuntimeV2Preview1GlobalProductionGate struct {
	ImmutableTurnContextStatus                 string `json:"immutable_turn_context_status"`
	ImmutableTurnContextImplementationUnlocked bool   `json:"immutable_turn_context_implementation_unlocked"`
	ProductionAuthorized                       bool   `json:"production_authorized"`
	StaticCatalogStatus                        string `json:"static_catalog_status"`
	StaticCatalogReasonCode                    string `json:"static_catalog_reason_code"`
}

type analyzeMaterialsRuntimeV2Preview1ApprovalBatch struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
}

type analyzeMaterialsRuntimeV2Preview1M4Evidence struct {
	Status             string `json:"status"`
	SchemaVersion      string `json:"schema_version"`
	Path               string `json:"path"`
	RunID              string `json:"run_id"`
	SourceDigest       string `json:"source_digest"`
	FileSHA256         string `json:"file_sha256"`
	FileMode           string `json:"file_mode"`
	AssertionCount     int    `json:"assertion_count"`
	TrueAssertionCount int    `json:"true_assertion_count"`
}

type analyzeMaterialsRuntimeV2Preview1ApprovalRequiredContractTest struct {
	SourcePath string `json:"source_path"`
	TestName   string `json:"test_name"`
}

func TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t *testing.T) {
	manifest, _ := loadAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t)
	if manifest.SchemaVersion != "analyze_materials_runtime_v2preview1_approval_manifest.v1" ||
		manifest.Profile != "analyze_materials.runtime.v2preview1" {
		t.Fatalf("invalid manifest identity: schema=%q profile=%q", manifest.SchemaVersion, manifest.Profile)
	}
	wantDecision := analyzeMaterialsRuntimeV2Preview1ApprovalDecision{
		SelectedProfile: "analyze_materials.runtime.v2preview1",
		Status:          "approved_for_development_preview",
		ApprovedAt:      "2026-07-17",
		ApprovalBasis:   "user_instruction_continue_development",
	}
	if manifest.Decision != wantDecision {
		t.Fatalf("decision=%+v want=%+v", manifest.Decision, wantDecision)
	}
	wantGate := analyzeMaterialsRuntimeV2Preview1GlobalProductionGate{
		ImmutableTurnContextStatus: "awaiting_owner_approval",
		StaticCatalogStatus:        "unavailable",
		StaticCatalogReasonCode:    "DESIGN_REVIEW_PENDING",
	}
	if manifest.GlobalProductionGate != wantGate {
		t.Fatalf("global_production_gate=%+v want=%+v", manifest.GlobalProductionGate, wantGate)
	}
	globalManifest, _ := approvalLoadManifestV1(t)
	if globalManifest.GlobalStatus != approvalAwaitingV1 || globalManifest.ImplementationUnlocked {
		t.Fatalf("immutable Turn Context gate drifted: status=%q unlocked=%t", globalManifest.GlobalStatus, globalManifest.ImplementationUnlocked)
	}
	wantBatches := []analyzeMaterialsRuntimeV2Preview1ApprovalBatch{
		{BatchID: "M0", Status: "approved"},
		{BatchID: "M1", Status: "implemented"},
		{BatchID: "M2", Status: "implemented_local_fake_only"},
		{BatchID: "M3", Status: "implemented_safe_projection"},
		{BatchID: "M4", Status: "completed_canonical_smoke_passed"},
	}
	if !reflect.DeepEqual(manifest.Batches, wantBatches) || manifest.M4EvidenceStatus != "completed_canonical_smoke_passed" {
		t.Fatalf("batches=%v m4_status=%q", manifest.Batches, manifest.M4EvidenceStatus)
	}
	if manifest.M4Evidence.Status != "passed" ||
		manifest.M4Evidence.SchemaVersion != "analyze_materials_runtime_v2_smoke_evidence.v1" ||
		manifest.M4Evidence.Path != ".local/smoke/analyze-materials-runtime-v2.json" ||
		manifest.M4Evidence.FileMode != "0600" || manifest.M4Evidence.AssertionCount != 22 ||
		manifest.M4Evidence.TrueAssertionCount != manifest.M4Evidence.AssertionCount ||
		!regexp.MustCompile(`^20[0-9]{6}T[0-9]{6}Z-[1-9][0-9]*$`).MatchString(manifest.M4Evidence.RunID) ||
		!regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(manifest.M4Evidence.SourceDigest) ||
		!regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(manifest.M4Evidence.FileSHA256) {
		t.Fatalf("invalid m4_evidence=%+v", manifest.M4Evidence)
	}
	wantActivation := []string{
		"DORA_ENV=local", "loopback_listener", "local_dedicated_database",
		"DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=true",
		"DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE=analyze_materials.runtime.v2preview1",
		"DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=true", "AGENT_SSE_MAX_EVENT_BYTES=131072",
		"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false", "DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false",
		"profile_exact_match", "migration_generation_match", "single_unified_processor_owner",
	}
	if !reflect.DeepEqual(manifest.ActivationRequirements, wantActivation) {
		t.Fatalf("activation_requirements=%v want=%v", manifest.ActivationRequirements, wantActivation)
	}
	if !reflect.DeepEqual(manifest.ExclusivePreviewDirectories, []string{"agent/internal/analyzematerialsruntime"}) {
		t.Fatalf("exclusive_preview_directories=%v", manifest.ExclusivePreviewDirectories)
	}
	wantForbidden := []string{
		"production_authorization", "static_catalog_availability", "second_executable_tool", "dynamic_tool_search",
		"assistant_or_tool_message_history", "quick_create_or_free_text_auto_routing", "business_material_analysis_write",
		"business_creation_spec_or_storyboard_write", "approval", "billing", "operation_batch_job_worker",
		"real_model_provider", "model_unknown_resend_or_failover", "evidence_upload_or_extraction",
		"pdf_audio_video_evidence", "checkpoint_interrupt_resume", "shared_or_production_activation",
	}
	if !reflect.DeepEqual(manifest.ForbiddenCapabilities, wantForbidden) {
		t.Fatalf("forbidden_capabilities=%v want=%v", manifest.ForbiddenCapabilities, wantForbidden)
	}
	wantEvidence := []string{
		"strict_intent_expected_assets_exact_set", "accepted_event_request_id_and_context_digest",
		"router_and_graph_model_receipt_replay", "tool_receipt_first_write_wins",
		"semantic_tool_failure_distinct_from_runtime_failure", "full_source_session_hol",
		"lease_fence_takeover_and_stale_fence_zero_write", "lost_wake_postgresql_scanner",
		"frozen_tool_projection_only_replay", "business_material_analysis_write_delta_zero",
		"approval_billing_operation_job_worker_delta_zero", "static_catalog_unavailable",
		"local_only_activation_fails_closed", "real_postgresql_redis_etcd_and_chromium_smoke",
	}
	if !reflect.DeepEqual(manifest.RequiredEvidenceBeforeActivation, wantEvidence) {
		t.Fatalf("required_evidence_before_activation=%v want=%v", manifest.RequiredEvidenceBeforeActivation, wantEvidence)
	}
	assertAnalyzeMaterialsRuntimeV2Preview1RequiredTests(t, manifest)
}

func TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestFileScope(t *testing.T) {
	manifest, _ := loadAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t)
	root := approvalRepoRootV1(t)
	seen := make(map[string]struct{}, len(manifest.ImplementationFileAllowlist))
	globalManifest, _ := approvalLoadManifestV1(t)
	globalAllowed := make(map[string]struct{}, len(globalManifest.AwaitingProductionGate.AllowedPreviewFiles))
	for _, relative := range globalManifest.AwaitingProductionGate.AllowedPreviewFiles {
		globalAllowed[relative] = struct{}{}
	}
	for _, relative := range manifest.ImplementationFileAllowlist {
		if relative == "" || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative ||
			strings.HasPrefix(relative, "../") || strings.ContainsAny(relative, "*?[]{}") {
			t.Fatalf("unsafe or non-exact allowlist path=%q", relative)
		}
		if _, duplicate := seen[relative]; duplicate {
			t.Fatalf("duplicate allowlist path=%q", relative)
		}
		seen[relative] = struct{}{}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("inspect allowlist path %s: %v", relative, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("allowlist path must be a regular non-symlink file: %s mode=%s", relative, info.Mode())
		}
		for _, forbiddenDir := range globalManifest.AwaitingProductionGate.ForbiddenDirectories {
			if relative == forbiddenDir || strings.HasPrefix(relative, forbiddenDir+"/") {
				if _, allowed := globalAllowed[relative]; !allowed && !strings.HasSuffix(relative, "_test.go") {
					t.Fatalf("shared forbidden-directory file lacks global exact Preview exception: %s", relative)
				}
			}
		}
	}
	for _, required := range analyzeMaterialsRuntimeV2Preview1SharedImplementationFiles() {
		if _, ok := seen[required]; !ok {
			t.Fatalf("implementation allowlist is missing shared integration file: %s", required)
		}
	}
	for _, relative := range manifest.ExclusivePreviewDirectories {
		assertAnalyzeMaterialsRuntimeV2Preview1DirectoryAllowlisted(t, root, relative, seen)
	}
	assertAnalyzeMaterialsRuntimeV2Preview1DirectoryAllowlisted(t, root, "agent/internal/graphtool/analyzematerials", seen)
	for _, searchRoot := range []string{"agent", "business", "frontend", "scripts"} {
		err := filepath.WalkDir(filepath.Join(root, searchRoot), func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if filepath.ToSlash(candidate) == filepath.ToSlash(filepath.Join(root, "agent/tests/contract")) {
					return filepath.SkipDir
				}
				return nil
			}
			base := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(entry.Name(), "_", ""), "-", ""))
			if !strings.Contains(base, "analyzematerials") && !strings.Contains(base, "materialanalysis") {
				return nil
			}
			relative, err := filepath.Rel(root, candidate)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if _, ok := seen[relative]; !ok {
				return fmt.Errorf("Analyze Materials implementation file is not allowlisted: %s", relative)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestArtifacts(t *testing.T) {
	manifest, _ := loadAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t)
	root := approvalRepoRootV1(t)
	want := []approvalArtifactRefV1{
		{Path: "docs/design/agent/analyze-materials-runtime-v2-design.md", Kind: "design_doc"},
		{Path: "agent/tests/contract/testdata/v2_analyze_materials_runtime_preview1/manifest.json", Kind: "corpus_manifest"},
		{Path: "scripts/analyze-materials-runtime-v2-smoke.sh", Kind: "canonical_smoke_script"},
		{Path: "scripts/tests/analyze-materials-runtime-v2-smoke-test.sh", Kind: "smoke_contract_test"},
	}
	if !reflect.DeepEqual(approvalArtifactShapeV1(manifest.ArtifactRefs), want) {
		t.Fatalf("artifact_refs=%v want=%v", approvalArtifactShapeV1(manifest.ArtifactRefs), want)
	}
	for _, artifact := range manifest.ArtifactRefs {
		approvalAssertArtifactV1(t, root, artifact.Path, artifact.SHA256)
	}
	if manifest.M4EvidenceStatus != "completed_canonical_smoke_passed" ||
		manifest.M4Evidence.Status != "passed" || manifest.GlobalProductionGate.ProductionAuthorized {
		t.Fatalf("M4 Evidence or production gate drifted: m4=%q evidence=%+v gate=%+v", manifest.M4EvidenceStatus, manifest.M4Evidence, manifest.GlobalProductionGate)
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestStrictJSON(t *testing.T) {
	_, raw := loadAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t)
	cases := map[string][]byte{
		"unknown": []byte(strings.Replace(string(raw), "{", "{\"future\":true,", 1)),
		"duplicate": []byte(strings.Replace(
			string(raw),
			`"schema_version": "analyze_materials_runtime_v2preview1_approval_manifest.v1",`,
			`"schema_version": "analyze_materials_runtime_v2preview1_approval_manifest.v1", "schema_version": "duplicate",`,
			1,
		)),
		"trailing": append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			var manifest analyzeMaterialsRuntimeV2Preview1ApprovalManifest
			if err := messageSetStrictDecodeV1(candidate, &manifest); err == nil {
				t.Fatalf("strict decoder accepted %s", name)
			}
		})
	}
}

func loadAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest(t *testing.T) (analyzeMaterialsRuntimeV2Preview1ApprovalManifest, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(approvalRepoRootV1(t), analyzeMaterialsRuntimeV2Preview1ApprovalManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	var manifest analyzeMaterialsRuntimeV2Preview1ApprovalManifest
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatalf("strict decode approval manifest: %v", err)
	}
	return manifest, raw
}

func assertAnalyzeMaterialsRuntimeV2Preview1RequiredTests(t *testing.T, manifest analyzeMaterialsRuntimeV2Preview1ApprovalManifest) {
	t.Helper()
	wantContract := []analyzeMaterialsRuntimeV2Preview1ApprovalRequiredContractTest{
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_approval_manifest_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifest"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_approval_manifest_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestFileScope"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_approval_manifest_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestArtifacts"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_approval_manifest_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ApprovalManifestStrictJSON"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1CorpusManifest"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1Corpus"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ExactSets"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1ContextContractDigest"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1OutcomeAndNegativeInvariants"},
		{SourcePath: "agent/tests/contract/analyze_materials_runtime_v2preview1_corpus_test.go", TestName: "TestAnalyzeMaterialsRuntimeV2Preview1StrictJSON"},
	}
	if !reflect.DeepEqual(manifest.RequiredContractTests, wantContract) {
		t.Fatalf("required_contract_tests=%v want=%v", manifest.RequiredContractTests, wantContract)
	}
	root := approvalRepoRootV1(t)
	for _, required := range wantContract {
		approvalAssertGoTestV1(t, approvalResolveFileV1(t, root, required.SourcePath), required.TestName)
	}
	wantImplementation := []approvalRequiredTestV1{
		{Module: "agent", SourcePath: "agent/internal/analyzematerialsruntime/runtime_test.go", TestName: "TestToolReceiptFreezesSemanticFailedAndReplays", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/analyzematerialsruntime/runtime_test.go", TestName: "TestProcessorOnlyDefersProjectionForKnownFrozenResult", Mode: "race"},
		{Module: "agent", SourcePath: "agent/internal/postgres/analyze_materials_runtime_migration_test.go", TestName: "TestAnalyzeMaterialsRuntimeMigrationContract", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/postgres/analyze_materials_runtime_repository_integration_test.go", TestName: "TestAnalyzeMaterialsRuntimePostgreSQLLifecycle", Mode: "required_pg"},
		{Module: "agent", SourcePath: "agent/internal/httpserver/analyze_materials_preview_handler_test.go", TestName: "TestAnalyzeMaterialsPreviewPOSTReturnsExactAcceptedDTO", Mode: "normal"},
		{Module: "business", SourcePath: "business/internal/httpserver/analyze_materials_preview_proxy_test.go", TestName: "TestAnalyzeMaterialsPreviewProxyReturnsExact202AndCanonicalIntent", Mode: "normal"},
	}
	if !reflect.DeepEqual(manifest.RequiredImplementationTests, wantImplementation) {
		t.Fatalf("required_implementation_tests=%v want=%v", manifest.RequiredImplementationTests, wantImplementation)
	}
	for _, required := range wantImplementation {
		if required.Mode != "normal" && required.Mode != "race" && required.Mode != "required_pg" {
			t.Fatalf("unsupported required test mode=%q", required.Mode)
		}
		approvalAssertGoTestV1(t, approvalResolveFileV1(t, root, required.SourcePath), required.TestName)
	}
}

func assertAnalyzeMaterialsRuntimeV2Preview1DirectoryAllowlisted(t *testing.T, root, relative string, allowed map[string]struct{}) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Preview directory must be a real directory: %s info=%v err=%v", relative, info, err)
	}
	if err := filepath.WalkDir(path, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		candidateRelative, err := filepath.Rel(root, candidate)
		if err != nil {
			return err
		}
		candidateRelative = filepath.ToSlash(candidateRelative)
		if _, ok := allowed[candidateRelative]; !ok {
			return fmt.Errorf("Preview directory contains non-allowlisted file: %s", candidateRelative)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func analyzeMaterialsRuntimeV2Preview1SharedImplementationFiles() []string {
	return []string{
		"scripts/check-database-contracts.sh",
		"scripts/analyze-materials-runtime-v2-smoke.sh",
		"scripts/tests/analyze-materials-runtime-v2-smoke-test.sh",
		"agent/cmd/local-smoke-analyze-materials-authority/main.go",
		"agent/cmd/local-smoke-analyze-materials-authority/main_test.go",
		"agent/internal/config/config.go",
		"agent/internal/bootstrap/bootstrap.go",
		"agent/internal/businessrpc/client.go",
		"agent/internal/event/record.go",
		"agent/internal/httpidentity/verifier.go",
		"agent/internal/httpserver/server.go",
		"agent/internal/postgres/client.go",
		"agent/internal/postgres/workspace_repository.go",
		"agent/internal/session/entity.go",
		"agent/internal/turncontext/context.go",
		"agent/internal/workspace/dto.go",
		"agent/internal/workspace/repository.go",
		"agent/internal/workspace/service.go",
		"agent/migrations/20260717001000_add_analyze_materials_runtime_v2preview1.up.sql",
		"agent/migrations/20260717001000_add_analyze_materials_runtime_v2preview1.down.sql",
		"business/internal/config/config.go",
		"business/internal/bootstrap/bootstrap.go",
		"business/cmd/local-smoke-analyze-materials-fixture/main.go",
		"business/cmd/local-smoke-analyze-materials-fixture/main_test.go",
		"business/cmd/local-smoke-analyze-materials-fixture/repository.go",
		"business/internal/localseed/dsn.go",
		"business/internal/localseed/dsn_test.go",
		"business/internal/agentidentity/signer.go",
		"business/internal/httpserver/agent_proxy_handler.go",
		"frontend/src/features/workspace/TurnOutputCard.jsx",
		"frontend/e2e/analyze-materials-runtime.spec.js",
		"frontend/src/features/workspace/workspaceContract.js",
		"frontend/src/features/workspace/workspaceReducer.js",
		"frontend/src/features/projects/ProjectWorkspacePage.jsx",
		"frontend/src/test/workspaceFixtures.js",
		"frontend/src/styles/aigc-workspace.css",
	}
}
