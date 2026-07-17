package contract_test

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const (
	approvalManifestPathV1 = "docs/design/agent/approvals/immutable_turn_context_v1/approval_manifest.json"
	approvalAwaitingV1     = "awaiting_owner_approval"
)

// approvalManifestV1 描述不可变 Turn Context 的最小审批门禁。
type approvalManifestV1 struct {
	SchemaVersion          string                   `json:"schema_version"`
	DecisionDocument       approvalDocumentRefV1    `json:"decision_document"`
	GlobalStatus           string                   `json:"global_status"`
	ImplementationUnlocked bool                     `json:"implementation_unlocked"`
	AwaitingProductionGate approvalProductionGateV1 `json:"awaiting_production_gate"`
	Items                  []approvalItemV1         `json:"items"`
}

// approvalDocumentRefV1 将审批输入绑定到仓库内文件及其内容摘要。
type approvalDocumentRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// approvalProductionGateV1 描述未完成审批时禁止出现的生产目录与标识。
type approvalProductionGateV1 struct {
	BlockCode              string   `json:"block_code"`
	BlockStatement         string   `json:"block_statement"`
	ForbiddenDirectories   []string `json:"forbidden_directories"`
	ForbiddenNonTestTokens []string `json:"forbidden_non_test_tokens"`
	AllowedPreviewFiles    []string `json:"allowed_preview_files"`
}

// approvalItemV1 描述一个 P0 决策项的 Owner、证据和生产阻断范围。
type approvalItemV1 struct {
	P0ID            string                   `json:"p0_id"`
	Title           string                   `json:"title"`
	Status          string                   `json:"status"`
	OwnerRoles      []string                 `json:"owner_roles"`
	ArtifactRefs    []approvalArtifactRefV1  `json:"artifact_refs"`
	RequiredTests   []approvalRequiredTestV1 `json:"required_tests"`
	ApprovalRefs    []approvalSignatureRefV1 `json:"approval_refs"`
	UnmetEvidence   []string                 `json:"unmet_evidence"`
	BlockCode       string                   `json:"block_code"`
	BlockStatement  string                   `json:"block_statement"`
	ProductionGates []string                 `json:"production_gates"`
}

type approvalSignatureRefV1 struct {
	OwnerRole  string `json:"owner_role"`
	ReviewURL  string `json:"review_url"`
	CommitSHA  string `json:"commit_sha"`
	ApprovedAt string `json:"approved_at"`
}

// approvalArtifactRefV1 描述审批候选证据的路径、摘要和类型。
type approvalArtifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
}

// approvalRequiredTestV1 将审批候选证据绑定到实际 Go 测试及执行模式。
type approvalRequiredTestV1 struct {
	Module     string `json:"module"`
	SourcePath string `json:"source_path"`
	TestName   string `json:"test_name"`
	Mode       string `json:"mode"`
}

// approvalExpectedItemV1 是测试内不可被 Manifest 自身弱化的冻结基线。
type approvalExpectedItemV1 struct {
	Title           string
	OwnerRoles      []string
	ArtifactRefs    []approvalArtifactRefV1
	RequiredTests   []approvalRequiredTestV1
	UnmetEvidence   []string
	BlockCode       string
	BlockStatement  string
	ProductionGates []string
}

func TestImmutableTurnContextApprovalManifestV1(t *testing.T) {
	manifest, _ := approvalLoadManifestV1(t)
	if manifest.SchemaVersion != "immutable_turn_context_approval_manifest.v1" {
		t.Fatalf("schema_version=%q", manifest.SchemaVersion)
	}
	if manifest.GlobalStatus != approvalAwaitingV1 || manifest.ImplementationUnlocked {
		t.Fatalf("global gate status=%q unlocked=%t", manifest.GlobalStatus, manifest.ImplementationUnlocked)
	}
	if manifest.DecisionDocument.Path != "docs/design/agent/immutable-turn-context-decision-review-v1.md" {
		t.Fatalf("decision_document.path=%q", manifest.DecisionDocument.Path)
	}

	expected := approvalExpectedItemsV1()
	if len(manifest.Items) != len(expected) {
		t.Fatalf("items=%d want=%d", len(manifest.Items), len(expected))
	}
	seenBlocks := make(map[string]struct{}, len(manifest.Items))
	for index, item := range manifest.Items {
		id := fmt.Sprintf("TC-P%02d", index+1)
		want, ok := expected[id]
		if !ok || item.P0ID != id {
			t.Fatalf("items[%d].p0_id=%q want=%q", index, item.P0ID, id)
		}
		if item.Title != want.Title || !reflect.DeepEqual(item.OwnerRoles, want.OwnerRoles) {
			t.Fatalf("%s title/owner_roles drift: title=%q owners=%v", id, item.Title, item.OwnerRoles)
		}
		if item.Status != approvalAwaitingV1 || len(item.ApprovalRefs) != 0 {
			t.Fatalf("%s must remain unsigned: status=%q approvals=%d", id, item.Status, len(item.ApprovalRefs))
		}
		if !reflect.DeepEqual(item.UnmetEvidence, want.UnmetEvidence) || !approvalContainsV1(item.UnmetEvidence, "owner_signatures") {
			t.Fatalf("%s unmet_evidence=%v want=%v", id, item.UnmetEvidence, want.UnmetEvidence)
		}
		if item.BlockCode != want.BlockCode || item.BlockStatement != want.BlockStatement || !strings.Contains(item.BlockStatement, "禁止") {
			t.Fatalf("%s invalid block: code=%q statement=%q", id, item.BlockCode, item.BlockStatement)
		}
		if _, exists := seenBlocks[item.BlockCode]; exists {
			t.Fatalf("duplicate block_code=%q", item.BlockCode)
		}
		seenBlocks[item.BlockCode] = struct{}{}
		if !reflect.DeepEqual(item.ProductionGates, want.ProductionGates) {
			t.Fatalf("%s production_gates=%v want=%v", id, item.ProductionGates, want.ProductionGates)
		}
		if !reflect.DeepEqual(approvalArtifactShapeV1(item.ArtifactRefs), want.ArtifactRefs) {
			t.Fatalf("%s artifact_refs drift: got=%v want=%v", id, approvalArtifactShapeV1(item.ArtifactRefs), want.ArtifactRefs)
		}
		if !reflect.DeepEqual(item.RequiredTests, want.RequiredTests) {
			t.Fatalf("%s required_tests drift: got=%v want=%v", id, item.RequiredTests, want.RequiredTests)
		}
	}
}

func TestImmutableTurnContextApprovalManifestV1StrictJSON(t *testing.T) {
	_, raw := approvalLoadManifestV1(t)
	cases := map[string][]byte{
		"unknown field": []byte(strings.Replace(string(raw), "{", "{\"future\":true,", 1)),
		"duplicate field": []byte(strings.Replace(
			string(raw),
			`"schema_version": "immutable_turn_context_approval_manifest.v1",`,
			`"schema_version": "immutable_turn_context_approval_manifest.v1", "schema_version": "duplicate",`,
			1,
		)),
		"trailing value": append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			var manifest approvalManifestV1
			if err := messageSetStrictDecodeV1(candidate, &manifest); err == nil {
				t.Fatalf("strict decoder accepted %s", name)
			}
		})
	}
}

func TestImmutableTurnContextApprovalManifestV1Artifacts(t *testing.T) {
	manifest, _ := approvalLoadManifestV1(t)
	root := approvalRepoRootV1(t)
	approvalAssertArtifactV1(t, root, manifest.DecisionDocument.Path, manifest.DecisionDocument.SHA256)
	for _, item := range manifest.Items {
		for _, artifact := range item.ArtifactRefs {
			switch artifact.Kind {
			case "design_doc", "corpus_manifest", "required_mode_script":
			default:
				t.Fatalf("%s artifact %q kind=%q", item.P0ID, artifact.Path, artifact.Kind)
			}
			approvalAssertArtifactV1(t, root, artifact.Path, artifact.SHA256)
		}
	}
}

func TestImmutableTurnContextApprovalManifestV1RequiredTests(t *testing.T) {
	manifest, _ := approvalLoadManifestV1(t)
	root := approvalRepoRootV1(t)
	requiredModeScript, err := os.ReadFile(filepath.Join(root, "scripts/check-database-contracts.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest.Items {
		seen := make(map[string]struct{}, len(item.RequiredTests))
		for _, required := range item.RequiredTests {
			key := required.Module + "\x00" + required.SourcePath + "\x00" + required.TestName + "\x00" + required.Mode
			if _, exists := seen[key]; exists {
				t.Fatalf("%s duplicate required test: %+v", item.P0ID, required)
			}
			seen[key] = struct{}{}
			if required.Module != "agent" && required.Module != "business" {
				t.Fatalf("%s test %s module=%q", item.P0ID, required.TestName, required.Module)
			}
			if !strings.HasPrefix(required.SourcePath, required.Module+"/") || !strings.HasSuffix(required.SourcePath, "_test.go") {
				t.Fatalf("%s test %s invalid source_path=%q", item.P0ID, required.TestName, required.SourcePath)
			}
			source := approvalResolveFileV1(t, root, required.SourcePath)
			approvalAssertGoTestV1(t, source, required.TestName)
			switch required.Mode {
			case "normal", "race":
			case "required_pg":
				if !approvalRequiredPGTestBoundV1(string(requiredModeScript), required.TestName) {
					t.Fatalf("%s required_pg test %s is not bound by scripts/check-database-contracts.sh", item.P0ID, required.TestName)
				}
			default:
				t.Fatalf("%s test %s invalid mode=%q", item.P0ID, required.TestName, required.Mode)
			}
		}
	}
}

func approvalRequiredPGTestBoundV1(script string, testName string) bool {
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, testName) {
			continue
		}
		// required-mode 的目标测试必须出现在实际调用或 -run 参数中；注释和普通说明不构成绑定证据。
		if strings.HasPrefix(line, "run_required_go_tests ") || strings.Contains(line, "-run ") {
			return true
		}
	}
	return false
}

func TestImmutableTurnContextApprovalManifestV1AwaitingProductionGate(t *testing.T) {
	manifest, _ := approvalLoadManifestV1(t)
	root := approvalRepoRootV1(t)
	wantDirectories := []string{
		"agent/internal/turncontext",
		"agent/internal/runtime",
		"agent/internal/chatmodelagent",
		"agent/internal/middleware",
		"agent/internal/checkpoint",
		"agent/internal/receipt",
		"agent/internal/approval",
		"agent/internal/prompt",
		"agent/internal/graphtool",
		"business/internal/approval",
		"business/internal/aigcapproval",
		"business/api/thrift/approval",
		"worker/internal/approval",
		"frontend/src/features/aigc/a2ui/action",
		"frontend/src/features/aigc/a2ui/actions",
	}
	wantTokens := []string{
		"dora.session_turn_context.v1",
		"session_turn_context",
		"SessionTurnContext",
		"TurnContextRepository",
		"ApprovalStore",
		"ApprovalRepository",
		"ApprovalDecisionRequestV1",
		"ApprovalConsumptionReceiptV1",
		"ApprovalContinuationResult",
		"BatchContinuationResult",
		"DecideCreationSpecCandidate",
		"creation_spec_candidate_decision_command.v1",
		"creation_spec_candidate_decision_query.v1",
		"creation_spec_candidate_decision_query_result.v1",
		"creation_spec_candidate_decision_authority.v1",
		"CreationSpecCandidateDecisionCommandV1",
		"CreationSpecCandidateDecisionQueryV1",
		"CreationSpecCandidateDecisionAuthorityV1",
		"decide_creation_spec_candidate.v1",
		"business.creation_spec_candidate_decision.query.v1",
		"DecideStoryboardCandidate",
		"DecidePromptResults",
		"DecideAssemblyPlanCandidate",
		"approval.decide",
		"approval_continuation",
		"dora.approval_binding.v1",
		"tool_receipt_owner_record.v1",
		"agent.session_turn",
		"agent.session_run",
		"agent.session_event_marker",
		"agent.legacy_authority_attestation",
		"agent.session_lane_upgrade_ledger",
	}
	wantPreviewFiles := []string{
		"agent/internal/turncontext/context.go",
		"agent/internal/turncontext/user_message.go",
		"agent/internal/turncontext/analyze_materials.go",
		"agent/internal/turncontext/plan_storyboard.go",
		"agent/internal/turncontext/write_prompts.go",
		"agent/internal/runtime/dto.go",
		"agent/internal/runtime/eino_runner.go",
		"agent/internal/runtime/processor.go",
		"agent/internal/runtime/service.go",
		"agent/internal/chatmodelagent/main.go",
		"agent/internal/chatmodelagent/direct_response.go",
		"agent/internal/chatmodelagent/analyze_materials.go",
		"agent/internal/chatmodelagent/plan_storyboard.go",
		"agent/internal/chatmodelagent/write_prompts.go",
		"agent/internal/graphtool/analyzematerials/branch.go",
		"agent/internal/graphtool/analyzematerials/dto.go",
		"agent/internal/graphtool/analyzematerials/errors.go",
		"agent/internal/graphtool/analyzematerials/graph.go",
		"agent/internal/graphtool/analyzematerials/node_evidence.go",
		"agent/internal/graphtool/analyzematerials/node_load_inputs.go",
		"agent/internal/graphtool/analyzematerials/node_model.go",
		"agent/internal/graphtool/analyzematerials/node_prompt.go",
		"agent/internal/graphtool/analyzematerials/node_result.go",
		"agent/internal/graphtool/analyzematerials/node_validate.go",
		"agent/internal/graphtool/analyzematerials/node_validate_candidate.go",
		"agent/internal/graphtool/analyzematerials/state.go",
		"agent/internal/graphtool/analyzematerials/tool.go",
		"agent/internal/graphtool/analyzematerials/validation.go",
		"agent/internal/graphtool/plancreationspec/dto.go",
		"agent/internal/graphtool/plancreationspec/graph.go",
		"agent/internal/graphtool/plancreationspec/tool.go",
		"agent/internal/graphtool/plancreationspec/validation.go",
		"agent/internal/graphtool/planstoryboard/artifacts.go",
		"agent/internal/graphtool/planstoryboard/dto.go",
		"agent/internal/graphtool/planstoryboard/errors.go",
		"agent/internal/graphtool/planstoryboard/graph.go",
		"agent/internal/graphtool/planstoryboard/prompt.go",
		"agent/internal/graphtool/planstoryboard/tool.go",
		"agent/internal/graphtool/planstoryboard/validation.go",
		"agent/internal/graphtool/writeprompts/artifacts.go",
		"agent/internal/graphtool/writeprompts/branch.go",
		"agent/internal/graphtool/writeprompts/dto.go",
		"agent/internal/graphtool/writeprompts/errors.go",
		"agent/internal/graphtool/writeprompts/graph.go",
		"agent/internal/graphtool/writeprompts/node_candidate.go",
		"agent/internal/graphtool/writeprompts/node_persistence.go",
		"agent/internal/graphtool/writeprompts/node_result.go",
		"agent/internal/graphtool/writeprompts/node_scope.go",
		"agent/internal/graphtool/writeprompts/prompt.go",
		"agent/internal/graphtool/writeprompts/tool.go",
		"agent/internal/graphtool/writeprompts/validation.go",
	}
	gate := manifest.AwaitingProductionGate
	if gate.BlockCode != "TC_APPROVAL_GLOBAL_AWAITING_OWNER_APPROVAL" || strings.TrimSpace(gate.BlockStatement) == "" {
		t.Fatalf("invalid global production block: code=%q statement=%q", gate.BlockCode, gate.BlockStatement)
	}
	if !reflect.DeepEqual(gate.ForbiddenDirectories, wantDirectories) || !reflect.DeepEqual(gate.ForbiddenNonTestTokens, wantTokens) ||
		!reflect.DeepEqual(gate.AllowedPreviewFiles, wantPreviewFiles) {
		t.Fatalf("%s gate list drift: directories=%v tokens=%v preview_files=%v", gate.BlockCode,
			gate.ForbiddenDirectories, gate.ForbiddenNonTestTokens, gate.AllowedPreviewFiles)
	}
	allowedPreview := make(map[string]struct{}, len(wantPreviewFiles))
	for _, relative := range wantPreviewFiles {
		allowedPreview[relative] = struct{}{}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil || info.IsDir() {
			t.Fatalf("approved Preview exception must be one existing file: %s err=%v", relative, err)
		}
	}
	for _, relative := range gate.ForbiddenDirectories {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("inspect forbidden path %s: %v", relative, err)
		}
		if err := filepath.WalkDir(path, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			candidateRelative, err := filepath.Rel(root, candidate)
			if err != nil {
				return err
			}
			candidateRelative = filepath.ToSlash(candidateRelative)
			if _, approved := allowedPreview[candidateRelative]; !approved {
				return fmt.Errorf("%s: %s; forbidden production file exists: %s", gate.BlockCode, gate.BlockStatement, candidateRelative)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	allowedExtensions := map[string]struct{}{
		".go": {}, ".sql": {}, ".thrift": {}, ".json": {}, ".yaml": {}, ".yml": {}, ".ts": {}, ".tsx": {},
	}
	productionRoots := []string{
		"agent/internal", "agent/api", "agent/cmd", "agent/migrations", "agent/kitex_gen",
		"business/internal", "business/api", "business/cmd", "business/migrations", "business/kitex_gen",
		"worker/internal", "worker/cmd", "worker/migrations", "frontend/src",
	}
	for _, scanRoot := range productionRoots {
		err := filepath.WalkDir(filepath.Join(root, scanRoot), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if strings.Contains(relative, "/tests/") || strings.HasSuffix(relative, "_test.go") || strings.Contains(relative, ".test.") {
				return nil
			}
			if _, ok := allowedExtensions[filepath.Ext(relative)]; !ok {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range gate.ForbiddenNonTestTokens {
				pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(forbidden) + `([^A-Za-z0-9_]|$)`)
				if pattern.Match(raw) {
					return fmt.Errorf("%s: %s; forbidden token %q in %s", gate.BlockCode, gate.BlockStatement, forbidden, relative)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func approvalLoadManifestV1(t *testing.T) (approvalManifestV1, []byte) {
	t.Helper()
	path := filepath.Join(approvalRepoRootV1(t), filepath.FromSlash(approvalManifestPathV1))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest approvalManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatalf("strict decode %s: %v", approvalManifestPathV1, err)
	}
	return manifest, raw
}

func approvalRepoRootV1(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("resolve repository root from %s: %v", cwd, err)
	}
	return root
}

func approvalResolveFileV1(t *testing.T, root, relative string) string {
	t.Helper()
	if relative == "" || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative || strings.HasPrefix(relative, "../") {
		t.Fatalf("unsafe project-relative path=%q", relative)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", relative, err)
	}
	inside, err := filepath.Rel(realRoot, realPath)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		t.Fatalf("path escapes repository: %q -> %q", relative, realPath)
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("artifact is not a regular file: %q err=%v", relative, err)
	}
	return realPath
}

func approvalAssertArtifactV1(t *testing.T, root, relative, expectedDigest string) {
	t.Helper()
	path := approvalResolveFileV1(t, root, relative)
	if matched, _ := regexp.MatchString(`^sha256:[0-9a-f]{64}$`, expectedDigest); !matched {
		t.Fatalf("artifact %s invalid sha256=%q", relative, expectedDigest)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expectedDigest)) != 1 {
		t.Fatalf("artifact %s sha256=%s want=%s", relative, actual, expectedDigest)
	}
}

func approvalAssertGoTestV1(t *testing.T, source, testName string) {
	t.Helper()
	if matched, _ := regexp.MatchString(`^Test[A-Za-z0-9_]+$`, testName); !matched {
		t.Fatalf("invalid Go test name=%q", testName)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != testName {
			continue
		}
		if function.Recv != nil || function.Type.Results != nil || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
			t.Fatalf("%s has invalid Go test signature", testName)
		}
		parameter := function.Type.Params.List[0]
		if len(parameter.Names) > 1 || !approvalIsTestingTV1(parameter.Type) {
			t.Fatalf("%s must have signature func(*testing.T)", testName)
		}
		return
	}
	t.Fatalf("Go test %s not found in %s", testName, source)
}

func approvalIsTestingTV1(expression ast.Expr) bool {
	pointer, ok := expression.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "testing"
}

func approvalContainsV1(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func approvalArtifactShapeV1(values []approvalArtifactRefV1) []approvalArtifactRefV1 {
	shapes := make([]approvalArtifactRefV1, len(values))
	for index, value := range values {
		shapes[index] = approvalArtifactRefV1{Path: value.Path, Kind: value.Kind}
	}
	return shapes
}

func approvalExpectedItemsV1() map[string]approvalExpectedItemV1 {
	artifact := func(path, kind string) approvalArtifactRefV1 {
		return approvalArtifactRefV1{Path: path, Kind: kind}
	}
	required := func(module, source, name, mode string) approvalRequiredTestV1 {
		return approvalRequiredTestV1{Module: module, SourcePath: source, TestName: name, Mode: mode}
	}
	result := map[string]approvalExpectedItemV1{
		"TC-P01": {
			Title: "冻结时点", OwnerRoles: []string{"agent.runtime", "agent.postgresql"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02_ingress/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_lane_ingress_v1_corpus_test.go", "TestSessionLaneIngressV1ReplayUsesFrozenResultAfterRuntimeProgress", "normal"),
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1FrozenReplay", "normal"),
			},
			BlockCode: "TC_APPROVAL_P01_INGRESS_FREEZE_EVIDENCE_MISSING", ProductionGates: []string{"turn_context_migration", "turn_context_repository", "ingress_writer", "claim_runner"},
		},
		"TC-P02": {
			Title: "模型可见历史", OwnerRoles: []string{"agent.history", "agent.runtime", "product", "security"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_marker/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_message_set_v1_corpus_test.go", "TestSessionMessageSetFullArrayV1ToolCausality", "normal"),
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1SummaryCutoffIsolation", "normal"),
				required("agent", "agent/tests/contract/session_event_marker_v1_corpus_test.go", "TestSessionEventMarkerV1EventBinding", "normal"),
			},
			BlockCode: "TC_APPROVAL_P02_HISTORY_SUMMARY_EVIDENCE_MISSING", ProductionGates: []string{"context_loader", "history_summary_store", "model_history_injection"},
		},
		"TC-P03": {
			Title: "Message Set", OwnerRoles: []string{"agent.postgresql", "data", "agent.runtime"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_upgrade/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_message_set_v1_corpus_test.go", "TestSessionMessageSetFullArrayV1GoldenDigests", "normal"),
				required("agent", "agent/tests/contract/session_message_set_v1_corpus_test.go", "TestSessionMessageSetFullArrayV1Limit", "normal"),
				required("agent", "agent/tests/contract/session_message_set_v1_corpus_test.go", "TestSessionMessageSetFullArrayV1CanonicalFieldSensitivity", "normal"),
			},
			BlockCode: "TC_APPROVAL_P03_MESSAGE_SET_PHYSICAL_EVIDENCE_MISSING", ProductionGates: []string{"message_set_production_digest", "legacy_upgrade_helper", "turn_context_repository"},
		},
		"TC-P04": {
			Title: "Prompt Bundle", OwnerRoles: []string{"agent.prompt_registry", "agent.runtime", "product", "security"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("docs/design/agent/immutable-turn-context-design-v1.md", "design_doc"),
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1ConditionalGroups", "normal"),
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1FrozenReplay", "normal"),
			},
			BlockCode: "TC_APPROVAL_P04_PROMPT_REGISTRY_EVIDENCE_MISSING", ProductionGates: []string{"prompt_registry", "prompt_resolver", "chat_model_agent"},
		},
		"TC-P05": {
			Title: "Executable Tool Registry", OwnerRoles: []string{"agent.runtime", "agent.tool_registry", "graph_tool.plan_creation_spec", "graph_tool.analyze_materials", "graph_tool.plan_storyboard", "graph_tool.generate_media", "graph_tool.write_prompts", "graph_tool.assemble_output"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r01/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/tool_receipt_v1_corpus_test.go", "TestToolReceiptV1Corpus", "normal"),
				required("agent", "agent/internal/tool/catalog_test.go", "TestCatalogProviderReturnsExactIndependentCopies", "normal"),
				required("agent", "agent/tests/contract/session_message_set_v1_corpus_test.go", "TestSessionMessageSetFullArrayV1AllToolKeys", "normal"),
			},
			BlockCode: "TC_APPROVAL_P05_EXECUTABLE_REGISTRY_EVIDENCE_MISSING", ProductionGates: []string{"executable_tool_registry", "graph_runtime", "chat_model_agent"},
		},
		"TC-P06": {
			Title: "Runtime/Model/Budget", OwnerRoles: []string{"agent.runner_policy", "agent.model_gateway", "agent.budget_policy", "finance", "operations_sre"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_lane_v1_corpus_test.go", "TestSessionLaneV1Corpus", "normal"),
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1FrozenReplay", "normal"),
			},
			BlockCode: "TC_APPROVAL_P06_POLICY_MODEL_BUDGET_EVIDENCE_MISSING", ProductionGates: []string{"runner", "model_gateway", "budget_enforcer"},
		},
		"TC-P07": {
			Title: "Access/Approval", OwnerRoles: []string{"business.authorization", "business.creation_spec", "agent.access_snapshot", "agent.approval_store", "product", "frontend", "security", "finance", "testing"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r01/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_turn_context/manifest.json", "corpus_manifest"),
				artifact("docs/design/agent/approval-continuation-cross-object-evidence-v1.md", "design_doc"),
				artifact("agent/tests/contract/testdata/w2_r03_cross_object/manifest.json", "corpus_manifest"),
				artifact("docs/design/agent/approval-consumption-receipt-contract-v1.md", "design_doc"),
				artifact("agent/tests/contract/testdata/w2_r04_approval_consumption/manifest.json", "corpus_manifest"),
				artifact("docs/design/agent/continuation-child-tool-receipt-contract-v1.md", "design_doc"),
				artifact("agent/tests/contract/testdata/w2_r04_continuation_child/manifest.json", "corpus_manifest"),
				artifact("docs/design/cross-module/creation-spec-candidate-decision-contract-v1.md", "design_doc"),
				artifact("agent/tests/contract/testdata/w2_r04_creation_spec_decision/manifest.json", "corpus_manifest"),
				artifact("scripts/check-database-contracts.sh", "required_mode_script"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/approval_continuation_cross_object_evidence_v1_corpus_test.go", "TestW2R03CrossObjectEvidenceManifest", "normal"),
				required("agent", "agent/tests/contract/approval_continuation_cross_object_evidence_v1_corpus_test.go", "TestApprovalContinuationCrossObjectEvidenceV1Corpus", "normal"),
				required("agent", "agent/tests/contract/session_turn_context_v1_corpus_test.go", "TestSessionTurnContextV1ContinuationStructuralGroups", "normal"),
				required("agent", "agent/tests/contract/tool_receipt_v1_corpus_test.go", "TestToolReceiptV1Corpus", "normal"),
				required("agent", "agent/tests/contract/tool_receipt_failed_after_v1_corpus_test.go", "TestToolReceiptFailedAfterV1Corpus", "normal"),
				required("agent", "agent/tests/contract/approval_continuation_cross_object_evidence_v1_corpus_test.go", "TestApprovalContinuationCrossObjectEvidenceV1TurnContextBinding", "normal"),
				required("agent", "agent/tests/contract/approval_consumption_receipt_v1_corpus_test.go", "TestW2R04ApprovalConsumptionManifest", "normal"),
				required("agent", "agent/tests/contract/approval_consumption_receipt_v1_corpus_test.go", "TestApprovalConsumptionReceiptCoreV1Corpus", "normal"),
				required("agent", "agent/tests/contract/approval_consumption_receipt_v1_corpus_test.go", "TestApprovalConsumptionReceiptCoreV1ReplayAndQuery", "normal"),
				required("agent", "agent/tests/contract/approval_consumption_receipt_v1_corpus_test.go", "TestApprovalConsumptionReceiptCoreV1SingleUseConflicts", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestW2R04ContinuationChildToolReceiptManifest", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestContinuationChildToolReceiptV1Corpus", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestContinuationChildToolReceiptV1CausalBindings", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestContinuationChildToolReceiptV1ActionSlotExactSets", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestContinuationChildToolReceiptV1ReplayAndRecovery", "normal"),
				required("agent", "agent/tests/contract/continuation_child_tool_receipt_v1_corpus_test.go", "TestContinuationChildToolReceiptV1R01R04Bridge", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestW2R04CreationSpecCandidateDecisionManifest", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1Corpus", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1GoldenDigestAndIdempotency", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1ApproveRejectUnion", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1AuthorityOutcomes", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1QueryUnknownOutcome", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1ReasonPriority", "normal"),
				required("agent", "agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go", "TestCreationSpecCandidateDecisionV1StrictJSON", "normal"),
				required("business", "business/internal/postgres/authorization_repository_integration_test.go", "TestAuthorizationRepositoryPostgreSQLLifecycle", "required_pg"),
			},
			BlockCode: "TC_APPROVAL_P07_ACCESS_APPROVAL_EVIDENCE_MISSING", ProductionGates: []string{"approval_action", "approval_repository", "approval_continuation", "high_risk_side_effect"},
		},
		"TC-P08": {
			Title: "被引用事实不可变性", OwnerRoles: []string{"agent.postgresql", "data", "security", "operations_sre"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02_marker/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_upgrade/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_event_marker_v1_corpus_test.go", "TestSessionEventMarkerV1Corpus", "normal"),
				required("agent", "agent/tests/contract/session_lane_legacy_upgrade_v1_corpus_test.go", "TestLegacyEventRetentionV1FailsClosed", "normal"),
				required("agent", "agent/internal/postgres/session_repository_contract_test.go", "TestSessionSkillSnapshotV2MigrationDeclaresImmutableTriggers", "normal"),
			},
			BlockCode: "TC_APPROVAL_P08_IMMUTABILITY_RETENTION_EVIDENCE_MISSING", ProductionGates: []string{"append_only_writer", "event_retention", "legacy_upgrade_helper", "runner"},
		},
		"TC-P09": {
			Title: "Turn/Run 与 Activation", OwnerRoles: []string{"agent.runtime", "agent.postgresql", "operations_sre"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("agent/tests/contract/testdata/w2_r02/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_ingress/manifest.json", "corpus_manifest"),
				artifact("agent/tests/contract/testdata/w2_r02_upgrade/manifest.json", "corpus_manifest"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/tests/contract/session_lane_v1_corpus_test.go", "TestSessionLaneV1Corpus", "normal"),
				required("agent", "agent/tests/contract/session_lane_ingress_v1_corpus_test.go", "TestSessionLaneIngressV1ReplayUsesFrozenResultAfterRuntimeProgress", "normal"),
				required("agent", "agent/tests/contract/session_lane_legacy_upgrade_v1_corpus_test.go", "TestLegacyLaneReadinessV1SeparatesFoundation", "normal"),
			},
			BlockCode: "TC_APPROVAL_P09_LANE_ACTIVATION_EVIDENCE_MISSING", ProductionGates: []string{"lane_capability", "processor", "scanner", "claim", "run"},
		},
		"TC-P10": {
			Title: "物理与证据", OwnerRoles: []string{"data", "quality_engineering", "operations_sre", "security"},
			ArtifactRefs: []approvalArtifactRefV1{
				artifact("docs/design/agent/session-lane-postgresql-design-v1.md", "design_doc"),
				artifact("scripts/check-database-contracts.sh", "required_mode_script"),
			},
			RequiredTests: []approvalRequiredTestV1{
				required("agent", "agent/internal/postgres/session_lane_upgrade_baseline_contract_test.go", "TestSessionLaneUpgradeBaselinePostgreSQLContract", "required_pg"),
				required("agent", "agent/internal/postgres/client_contract_test.go", "TestMigratedSchemaContract", "required_pg"),
				required("agent", "agent/tests/contract/session_lane_legacy_upgrade_v1_corpus_test.go", "TestLegacyUpgradeDownGuardV1", "normal"),
			},
			BlockCode: "TC_APPROVAL_P10_PHYSICAL_PG_EVIDENCE_MISSING", ProductionGates: []string{"turn_context_migration", "turn_context_repository", "readiness"},
		},
	}
	unmet := map[string][]string{
		"TC-P01": {"real_pg_ingress_atomicity", "owner_signatures"},
		"TC-P02": {"history_summary_owner_corpus", "summary_retention_and_security", "owner_signatures"},
		"TC-P03": {"total_byte_limit", "legacy_real_recalculation", "real_pg_explain_and_capacity", "owner_signatures"},
		"TC-P04": {"prompt_bundle_owner_corpus", "publish_withdraw_retention", "secret_scan", "owner_signatures"},
		"TC-P05": {"six_graph_tool_owner_approvals", "startup_compile", "per_turn_and_per_call_pin", "tool_reduction", "owner_signatures"},
		"TC-P06": {"runtime_policy_owner_corpus", "model_route_owner_corpus", "budget_snapshot_owner_corpus", "provider_unknown_outcome", "remaining_budget_recovery", "owner_signatures"},
		"TC-P07": {"cross_module_approval_contract", "cross_user_and_project", "resource_version_revalidation", "decision_consumption_once", "real_pg_decision_expiry_cancel_atomicity", "child_receipt_business_decide_unknown_outcome", "owner_signatures"},
		"TC-P08": {"real_pg_append_only_triggers", "controlled_retention", "rooted_anti_join", "key_rotation_semantic_digest", "owner_signatures"},
		"TC-P09": {"turn_run_repository", "lease_fence_race", "redis_lost_wake_scanner", "old_writer_drain", "stale_generation", "quarantine_recovery", "owner_signatures"},
		"TC-P10": {"forward_migration_and_upgrade", "crash_and_race", "cli_down_guard", "explain_and_capacity", "evidence_redaction", "owner_signatures"},
	}
	statements := map[string]string{
		"TC-P01": "未取得 Agent Runtime 与 Agent PostgreSQL 对同事务冻结及真实 PostgreSQL 原子性证据的签字；禁止 Turn/Context Writer、Claim 和 Runner。",
		"TC-P02": "History/Summary 来源、保留与安全证据尚未签字；禁止 Context Loader、Summary Store 与模型历史注入。",
		"TC-P03": "Message Set 物理上限、legacy 回算或 PostgreSQL 性能证据尚未签字；禁止生产 digest 与 legacy Helper。",
		"TC-P04": "Prompt canonical、发布撤回保留和无 Secret 证据尚未签字；禁止 Prompt Registry/Resolver 接入 Runner。",
		"TC-P05": "六 Tool Owner、Compile 与 Pin 证据尚未齐备；Catalog 必须保持 unavailable，禁止 Executable Registry 和 Graph 注册。",
		"TC-P06": "Policy/Route/Budget 默认值、费用和恢复剩余额度尚未签字；禁止生产 Runner、Model 调用和预算执行。",
		"TC-P07": "Access/Approval 跨 Module、再验证、Decision/Consumption 和 child Receipt 恢复证据尚未签字；禁止 Approval Action、Continuation 与高风险副作用。",
		"TC-P08": "引用事实不可变、Retention 与 Key Rotation 的真实 PostgreSQL 证据尚未签字；禁止 Writer、Retention 和 Runner 读取。",
		"TC-P09": "HOL/Fence/Scanner/Drain/Generation 的生产证据尚未签字；禁止启动 Processor、Scanner、Claim、Wake 与 Run。",
		"TC-P10": "新物理 Schema、PostgreSQL 16 upgrade/crash/race/Down/Explain/脱敏证据尚未签字；禁止新 Migration、Repository 与 Readiness。",
	}
	for id, item := range result {
		item.UnmetEvidence = unmet[id]
		item.BlockStatement = statements[id]
		result[id] = item
	}
	return result
}
