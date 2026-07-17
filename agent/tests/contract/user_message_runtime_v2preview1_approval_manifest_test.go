package contract_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const userMessageRuntimeV2Preview1ApprovalManifestPath = "docs/design/agent/approvals/user_message_runtime_v2preview1/approval_manifest.json"

type userMessageRuntimeV2Preview1ApprovalManifest struct {
	SchemaVersion                    string                                             `json:"schema_version"`
	Profile                          string                                             `json:"profile"`
	Decision                         userMessageRuntimeV2Preview1ApprovalDecision       `json:"decision"`
	GlobalProductionGate             userMessageRuntimeV2Preview1GlobalProductionGate   `json:"global_production_gate"`
	Batches                          []userMessageRuntimeV2Preview1ApprovalBatch        `json:"batches"`
	ActivationRequirements           []string                                           `json:"activation_requirements"`
	ExclusivePreviewDirectories      []string                                           `json:"exclusive_preview_directories"`
	ImplementationFileAllowlist      []string                                           `json:"implementation_file_allowlist"`
	ForbiddenCapabilities            []string                                           `json:"forbidden_capabilities"`
	ArtifactRefs                     []approvalArtifactRefV1                            `json:"artifact_refs"`
	RequiredContractTests            []userMessageRuntimeV2Preview1ApprovalRequiredTest `json:"required_contract_tests"`
	RequiredImplementationTests      []approvalRequiredTestV1                           `json:"required_implementation_tests"`
	RequiredEvidenceBeforeActivation []string                                           `json:"required_evidence_before_activation"`
	DeferredRealProviderEvidence     []string                                           `json:"deferred_real_provider_evidence"`
}

type userMessageRuntimeV2Preview1ApprovalDecision struct {
	SelectedScheme string `json:"selected_scheme"`
	Status         string `json:"status"`
	ApprovedAt     string `json:"approved_at"`
	ApprovalBasis  string `json:"approval_basis"`
}

type userMessageRuntimeV2Preview1GlobalProductionGate struct {
	ImmutableTurnContextStatus                 string `json:"immutable_turn_context_status"`
	ImmutableTurnContextImplementationUnlocked bool   `json:"immutable_turn_context_implementation_unlocked"`
	ProductionAuthorized                       bool   `json:"production_authorized"`
}

type userMessageRuntimeV2Preview1ApprovalBatch struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
}

type userMessageRuntimeV2Preview1ApprovalRequiredTest struct {
	SourcePath string `json:"source_path"`
	TestName   string `json:"test_name"`
}

func TestUserMessageRuntimeV2Preview1ApprovalManifest(t *testing.T) {
	manifest, _ := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	if manifest.SchemaVersion != "user_message_runtime_v2preview1_approval_manifest.v1" || manifest.Profile != "user_message.runtime.v2preview1" {
		t.Fatalf("invalid manifest identity: schema=%q profile=%q", manifest.SchemaVersion, manifest.Profile)
	}
	wantDecision := userMessageRuntimeV2Preview1ApprovalDecision{
		SelectedScheme: "A",
		Status:         "approved_for_development_preview",
		ApprovedAt:     "2026-07-17",
		ApprovalBasis:  "user_instruction_continue_development",
	}
	if !reflect.DeepEqual(manifest.Decision, wantDecision) {
		t.Fatalf("decision=%+v want=%+v", manifest.Decision, wantDecision)
	}
	if manifest.GlobalProductionGate.ImmutableTurnContextStatus != "awaiting_owner_approval" ||
		manifest.GlobalProductionGate.ImmutableTurnContextImplementationUnlocked || manifest.GlobalProductionGate.ProductionAuthorized {
		t.Fatalf("global production gate unlocked: %+v", manifest.GlobalProductionGate)
	}
	globalManifest, _ := approvalLoadManifestV1(t)
	if globalManifest.GlobalStatus != approvalAwaitingV1 || globalManifest.ImplementationUnlocked {
		t.Fatalf("immutable Turn Context manifest was unlocked: status=%q unlocked=%t", globalManifest.GlobalStatus, globalManifest.ImplementationUnlocked)
	}
	wantBatches := []userMessageRuntimeV2Preview1ApprovalBatch{
		{BatchID: "B0", Status: "approved"},
		{BatchID: "B1", Status: "completed_legacy_helper_verified"},
		{BatchID: "B2", Status: "implemented_scanner_only"},
		{BatchID: "B3", Status: "implemented_local_fake_only"},
		{BatchID: "B4", Status: "completed_canonical_smoke_passed"},
	}
	if !reflect.DeepEqual(manifest.Batches, wantBatches) {
		t.Fatalf("batches=%v want=%v", manifest.Batches, wantBatches)
	}
	wantActivation := []string{
		"DORA_ENV=local", "loopback_listener", "local_dedicated_database",
		"DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true",
		"DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE=user_message.runtime.v2preview1",
		"migration_generation_match", "legacy_blocker_count_zero", "single_unified_processor_owner",
	}
	if !reflect.DeepEqual(manifest.ActivationRequirements, wantActivation) {
		t.Fatalf("activation_requirements=%v want=%v", manifest.ActivationRequirements, wantActivation)
	}
	if !reflect.DeepEqual(manifest.ExclusivePreviewDirectories, []string{"agent/internal/usermessageruntime"}) {
		t.Fatalf("exclusive_preview_directories=%v", manifest.ExclusivePreviewDirectories)
	}
	wantForbidden := []string{
		"non_empty_executable_tool_registry", "graph_tool_call", "analyze_materials", "plan_creation_spec",
		"assistant_or_tool_message_history", "workspace_chat_post", "approval", "billing",
		"operation_batch_job_worker", "business_draft_write", "source_type_rewrite", "preview_fact_fabrication",
		"hol_skip_or_reset", "dual_processor_overlap", "marker_or_retention_rollout",
		"production_turn_run_context_names", "model_unknown_resend_or_failover", "shared_or_production_activation",
	}
	if !reflect.DeepEqual(manifest.ForbiddenCapabilities, wantForbidden) {
		t.Fatalf("forbidden_capabilities=%v want=%v", manifest.ForbiddenCapabilities, wantForbidden)
	}
	wantEvidence := []string{
		"empty_database_and_migration_007_upgrade", "all_table_and_column_comments", "no_physical_foreign_keys",
		"check_index_and_down_guard", "context_terminal_receipt_and_ledger_append_only",
		"legacy_helper_crash_replay_and_rooted_anti_join", "lease_fence_takeover_and_stale_fence_zero_write",
		"lost_wake_postgresql_scanner", "terminal_receipt_event_projection_atomicity",
		"response_loss_frozen_replay", "local_only_activation_fails_closed",
		"plan_creation_spec_preview_regression", "real_postgresql_redis_etcd_and_chromium_smoke",
	}
	if !reflect.DeepEqual(manifest.RequiredEvidenceBeforeActivation, wantEvidence) {
		t.Fatalf("required_evidence_before_activation=%v want=%v", manifest.RequiredEvidenceBeforeActivation, wantEvidence)
	}
	if !reflect.DeepEqual(manifest.DeferredRealProviderEvidence, []string{"model_unknown_never_resends"}) {
		t.Fatalf("deferred_real_provider_evidence=%v", manifest.DeferredRealProviderEvidence)
	}
}

func TestUserMessageRuntimeV2Preview1ApprovalManifestArtifacts(t *testing.T) {
	manifest, _ := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	root := approvalRepoRootV1(t)
	want := []approvalArtifactRefV1{
		{Path: "docs/design/agent/user-message-runtime-v2-design.md", Kind: "design_doc"},
		{Path: "agent/tests/contract/testdata/v2_user_message_runtime_preview1/manifest.json", Kind: "corpus_manifest"},
		{Path: "scripts/check-database-contracts.sh", Kind: "required_mode_script"},
		{Path: "scripts/smoke-user-message-runtime.sh", Kind: "canonical_smoke_script"},
		{Path: "scripts/tests/user-message-runtime-smoke-test.sh", Kind: "smoke_contract_test"},
		{Path: "frontend/e2e/user-message-runtime.spec.js", Kind: "canonical_browser_spec"},
	}
	if !reflect.DeepEqual(approvalArtifactShapeV1(manifest.ArtifactRefs), want) {
		t.Fatalf("artifact_refs=%v want=%v", approvalArtifactShapeV1(manifest.ArtifactRefs), want)
	}
	for _, artifact := range manifest.ArtifactRefs {
		approvalAssertArtifactV1(t, root, artifact.Path, artifact.SHA256)
	}
}

func TestUserMessageRuntimeV2Preview1ApprovalManifestFileScope(t *testing.T) {
	manifest, _ := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	want := userMessageRuntimeV2Preview1ImplementationFiles()
	if !reflect.DeepEqual(manifest.ImplementationFileAllowlist, want) {
		t.Fatalf("implementation_file_allowlist=%v want=%v", manifest.ImplementationFileAllowlist, want)
	}
	root := approvalRepoRootV1(t)
	seen := make(map[string]struct{}, len(want))
	globalManifest, _ := approvalLoadManifestV1(t)
	globalAllowed := make(map[string]struct{}, len(globalManifest.AwaitingProductionGate.AllowedPreviewFiles))
	for _, relative := range globalManifest.AwaitingProductionGate.AllowedPreviewFiles {
		globalAllowed[relative] = struct{}{}
	}
	for _, relative := range want {
		if relative == "" || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative ||
			strings.HasPrefix(relative, "../") || strings.ContainsAny(relative, "*?[]{}") {
			t.Fatalf("unsafe or non-exact allowlist path=%q", relative)
		}
		if _, duplicate := seen[relative]; duplicate {
			t.Fatalf("duplicate allowlist path=%q", relative)
		}
		seen[relative] = struct{}{}
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			t.Fatalf("exact allowlist file is missing: %s", relative)
		}
		if err != nil {
			t.Fatalf("inspect allowlist path %s: %v", relative, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("allowlist path must be a regular non-symlink file: %s mode=%s", relative, info.Mode())
		}
		for _, forbiddenDir := range globalManifest.AwaitingProductionGate.ForbiddenDirectories {
			if relative == forbiddenDir || strings.HasPrefix(relative, forbiddenDir+"/") {
				if _, allowed := globalAllowed[relative]; !allowed && !strings.HasSuffix(relative, "_test.go") {
					t.Fatalf("shared forbidden-directory file lacks global Preview exception: %s", relative)
				}
			}
		}
	}
	for _, relative := range manifest.ExclusivePreviewDirectories {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("exclusive Preview directory must be a real directory: %s info=%v err=%v", relative, info, err)
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
			if _, allowed := seen[candidateRelative]; !allowed {
				return fmt.Errorf("exclusive Preview directory contains non-allowlisted file: %s", candidateRelative)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUserMessageRuntimeV2Preview1ApprovalManifestRequiredTests(t *testing.T) {
	manifest, _ := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	want := userMessageRuntimeV2Preview1RequiredTests()
	if !reflect.DeepEqual(manifest.RequiredContractTests, want) {
		t.Fatalf("required_contract_tests=%v want=%v", manifest.RequiredContractTests, want)
	}
	root := approvalRepoRootV1(t)
	seen := make(map[string]struct{}, len(want))
	for _, required := range want {
		key := required.SourcePath + "\x00" + required.TestName
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate required test=%+v", required)
		}
		seen[key] = struct{}{}
		if !strings.HasPrefix(required.SourcePath, "agent/tests/contract/") || !strings.HasSuffix(required.SourcePath, "_test.go") {
			t.Fatalf("invalid required test source=%q", required.SourcePath)
		}
		approvalAssertGoTestV1(t, approvalResolveFileV1(t, root, required.SourcePath), required.TestName)
	}
}

func TestUserMessageRuntimeV2Preview1ApprovalManifestImplementationTests(t *testing.T) {
	manifest, _ := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	want := []approvalRequiredTestV1{
		{Module: "agent", SourcePath: "agent/internal/postgres/user_message_runtime_migration_test.go", TestName: "TestUserMessageRuntimePreviewMigrationContract", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/usermessageruntime/legacy_upgrade_test.go", TestName: "TestLegacyUpgradeBlockerMakesWholeBatchZeroWrite", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/usermessageruntime/legacy_upgrade_test.go", TestName: "TestLegacyUpgradeCryptographicBlockerMakesWholeBatchZeroWrite", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/usermessageruntime/legacy_upgrade_test.go", TestName: "TestLegacyUpgradeAbsentConvergesPreparedAppliedVerified", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/cmd/local-smoke-user-message-legacy-seeder/main_test.go", TestName: "TestSeedOutputIsSingleLineSafeExactJSON", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/cmd/local-smoke-user-message-legacy-seeder/main_test.go", TestName: "TestIsLocalSmokeAgentDSNExactDatabaseAllowlist", Mode: "normal"},
		{Module: "agent", SourcePath: "agent/internal/postgres/user_message_runtime_migration_test.go", TestName: "TestUserMessageLegacyUpgradeGuardsPostgreSQL", Mode: "required_pg"},
		{Module: "agent", SourcePath: "agent/internal/postgres/user_message_legacy_upgrade_repository_integration_test.go", TestName: "TestUserMessageLegacyUpgradePostgreSQLRecoveryAndConcurrency", Mode: "required_pg"},
		{Module: "agent", SourcePath: "agent/internal/postgres/user_message_runtime_repository_integration_test.go", TestName: "TestUserMessageRuntimePostgreSQLLifecycle", Mode: "required_pg"},
	}
	if !reflect.DeepEqual(manifest.RequiredImplementationTests, want) {
		t.Fatalf("required_implementation_tests=%v want=%v", manifest.RequiredImplementationTests, want)
	}
	root := approvalRepoRootV1(t)
	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts/check-database-contracts.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range want {
		approvalAssertGoTestV1(t, approvalResolveFileV1(t, root, required.SourcePath), required.TestName)
		switch required.Mode {
		case "normal":
		case "required_pg":
			if !approvalRequiredPGTestBoundV1(string(scriptRaw), required.TestName) {
				t.Fatalf("required_pg test is not bound by required-mode script: %s", required.TestName)
			}
		default:
			t.Fatalf("unsupported implementation test mode=%q", required.Mode)
		}
	}
}

func TestUserMessageRuntimeV2Preview1ApprovalManifestStrictJSON(t *testing.T) {
	_, raw := loadUserMessageRuntimeV2Preview1ApprovalManifest(t)
	cases := map[string][]byte{
		"unknown": []byte(strings.Replace(string(raw), "{", "{\"future\":true,", 1)),
		"duplicate": []byte(strings.Replace(
			string(raw),
			`"schema_version": "user_message_runtime_v2preview1_approval_manifest.v1",`,
			`"schema_version": "user_message_runtime_v2preview1_approval_manifest.v1", "schema_version": "duplicate",`,
			1,
		)),
		"trailing": append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			var manifest userMessageRuntimeV2Preview1ApprovalManifest
			if err := messageSetStrictDecodeV1(candidate, &manifest); err == nil {
				t.Fatalf("strict decoder accepted %s", name)
			}
		})
	}
}

func loadUserMessageRuntimeV2Preview1ApprovalManifest(t *testing.T) (userMessageRuntimeV2Preview1ApprovalManifest, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(approvalRepoRootV1(t), userMessageRuntimeV2Preview1ApprovalManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	var manifest userMessageRuntimeV2Preview1ApprovalManifest
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatalf("strict decode approval manifest: %v", err)
	}
	return manifest, raw
}

func userMessageRuntimeV2Preview1ImplementationFiles() []string {
	return []string{
		".env.example",
		"README.md",
		"Makefile",
		"scripts/check-database-contracts.sh",
		"scripts/smoke-user-message-runtime.sh",
		"scripts/tests/user-message-runtime-smoke-test.sh",
		"agent/internal/usermessageruntime/dto.go",
		"agent/internal/usermessageruntime/dto_test.go",
		"agent/internal/usermessageruntime/processor.go",
		"agent/internal/usermessageruntime/processor_test.go",
		"agent/internal/usermessageruntime/eino_runner.go",
		"agent/internal/usermessageruntime/eino_runner_test.go",
		"agent/internal/usermessageruntime/model_receipt.go",
		"agent/internal/usermessageruntime/model_receipt_test.go",
		"agent/internal/usermessageruntime/profile.go",
		"agent/internal/usermessageruntime/profile_test.go",
		"agent/internal/usermessageruntime/legacy_upgrade.go",
		"agent/internal/usermessageruntime/legacy_upgrade_test.go",
		"agent/internal/chatmodel/user_message_fake.go",
		"agent/internal/chatmodel/user_message_fake_test.go",
		"agent/internal/chatmodelagent/direct_response.go",
		"agent/internal/chatmodelagent/direct_response_test.go",
		"agent/internal/turncontext/user_message.go",
		"agent/internal/turncontext/user_message_test.go",
		"agent/internal/session/entity.go",
		"agent/internal/session/service.go",
		"agent/internal/session/service_v2.go",
		"agent/internal/session/user_message_runtime.go",
		"agent/internal/session/user_message_runtime_test.go",
		"agent/internal/postgres/user_message_runtime_model.go",
		"agent/internal/postgres/user_message_runtime_repository.go",
		"agent/internal/postgres/user_message_runtime_migration_test.go",
		"agent/internal/postgres/user_message_runtime_repository_integration_test.go",
		"agent/internal/postgres/user_message_legacy_upgrade_repository.go",
		"agent/internal/postgres/user_message_legacy_upgrade_repository_integration_test.go",
		"agent/internal/postgres/session_repository.go",
		"agent/internal/postgres/session_repository_contract_test.go",
		"agent/internal/postgres/session_mapper.go",
		"agent/internal/postgres/client.go",
		"agent/internal/postgres/workspace_repository.go",
		"agent/internal/config/config.go",
		"agent/internal/config/config_test.go",
		"agent/internal/bootstrap/bootstrap.go",
		"agent/internal/event/record.go",
		"agent/internal/event/record_test.go",
		"agent/internal/workspace/dto.go",
		"agent/internal/workspace/repository.go",
		"agent/internal/workspace/service.go",
		"agent/internal/workspace/service_test.go",
		"agent/internal/postgres/workspace_repository_test.go",
		"agent/migrations/20260717000800_add_user_message_runtime_v2preview1.up.sql",
		"agent/migrations/20260717000800_add_user_message_runtime_v2preview1.down.sql",
		"agent/migrations/20260717000900_guard_user_message_legacy_upgrade.up.sql",
		"agent/migrations/20260717000900_guard_user_message_legacy_upgrade.down.sql",
		"agent/cmd/local-smoke-user-message-legacy-seeder/main.go",
		"agent/cmd/local-smoke-user-message-legacy-seeder/main_test.go",
		"frontend/src/features/workspace/turnOutputContract.js",
		"frontend/src/features/workspace/turnOutputContract.test.js",
		"frontend/src/features/workspace/TurnOutputCard.jsx",
		"frontend/src/features/workspace/TurnOutputCard.test.jsx",
		"frontend/src/features/workspace/workspaceContract.js",
		"frontend/src/features/workspace/workspaceContract.test.js",
		"frontend/src/features/workspace/workspaceReducer.js",
		"frontend/src/features/workspace/workspaceReducer.test.js",
		"frontend/src/features/workspace/workspaceEventStream.test.js",
		"frontend/src/features/projects/ProjectWorkspacePage.jsx",
		"frontend/src/features/projects/ProjectWorkspacePage.test.jsx",
		"frontend/src/features/tools/ToolCatalogPanel.jsx",
		"frontend/src/test/workspaceFixtures.js",
		"frontend/src/styles/landing.css",
		"frontend/e2e/user-message-runtime.spec.js",
	}
}

func userMessageRuntimeV2Preview1RequiredTests() []userMessageRuntimeV2Preview1ApprovalRequiredTest {
	return []userMessageRuntimeV2Preview1ApprovalRequiredTest{
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifest"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifestArtifacts"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifestFileScope"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifestRequiredTests"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifestImplementationTests"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_approval_manifest_test.go", TestName: "TestUserMessageRuntimeV2Preview1ApprovalManifestStrictJSON"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1CorpusManifest"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1Corpus"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1ExactSets"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1ContextContractDigest"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1NegativeCapabilities"},
		{SourcePath: "agent/tests/contract/user_message_runtime_v2preview1_corpus_test.go", TestName: "TestUserMessageRuntimeV2Preview1StrictJSON"},
	}
}
