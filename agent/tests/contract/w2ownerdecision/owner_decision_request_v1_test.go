// Package w2ownerdecision_test 验证 R03/R04 Owner 待决请求只承载候选输入，不产生批准或实现解锁能力。
package w2ownerdecision_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	r03OwnerDecisionRequestPathV1 = "docs/design/agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json"
	r04OwnerDecisionRequestPathV1 = "docs/design/agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json"
	ownerDecisionValidatorPathV1  = "agent/tests/contract/w2ownerdecision/owner_decision_request_v1_test.go"
	ownerDecisionGuardPathV1      = "agent/tests/contract/w2ownerdecisionguard/owner_decision_request_guard_v1_test.go"
	reviewFreezeManifestPathV1    = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
)

type ownerDecisionRequestV1 struct {
	SchemaVersion             string                       `json:"schema_version"`
	RequestID                 string                       `json:"request_id"`
	Gate                      string                       `json:"gate"`
	Status                    string                       `json:"status"`
	ImplementationUnlocked    bool                         `json:"implementation_unlocked"`
	OwnerRoleSetStatus        string                       `json:"owner_role_set_status"`
	DecisionDocument          artifactRefV1                `json:"decision_document"`
	GateManifest              artifactRefV1                `json:"gate_manifest"`
	CandidateContractManifest *artifactRefV1               `json:"candidate_contract_manifest,omitempty"`
	ValidatorSources          []artifactRefV1              `json:"validator_sources"`
	Items                     []ownerDecisionRequestItemV1 `json:"items"`
	UnmetEvidence             []string                     `json:"unmet_evidence"`
	BlockedProductionGates    []string                     `json:"blocked_production_gates"`
	BlockStatement            string                       `json:"block_statement"`
}

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ownerDecisionRequestItemV1 struct {
	DecisionID          string   `json:"decision_id"`
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	AllowedOptionIDs    []string `json:"allowed_option_ids"`
	RecommendedOptionID string   `json:"recommended_option_id"`
	CandidateOwnerRoles []string `json:"candidate_owner_roles"`
	MatrixRowID         string   `json:"matrix_row_id"`
	BlockCode           string   `json:"block_code"`
}

type reviewFreezeManifestProjectionV1 struct {
	Gates []struct {
		Gate               string          `json:"gate"`
		Status             string          `json:"status"`
		RequiredOwnerRoles []string        `json:"required_owner_roles"`
		Freeze             json.RawMessage `json:"freeze"`
		ReopenException    json.RawMessage `json:"reopen_exception"`
		CandidateEvidence  []struct {
			Scope                  string   `json:"scope"`
			Coverage               string   `json:"coverage"`
			ContractManifestPath   string   `json:"contract_manifest_path"`
			ContractManifestSHA256 string   `json:"contract_manifest_sha256"`
			VectorIDs              []string `json:"vector_ids"`
			TargetTests            []string `json:"target_tests"`
		} `json:"candidate_evidence"`
		Blockers []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	} `json:"gates"`
}

type candidateContractManifestProjectionV1 struct {
	SchemaVersion         string   `json:"schema_version"`
	FixtureIDs            []string `json:"fixture_ids"`
	VectorIDs             []string `json:"vector_ids"`
	TotalVectorCount      int      `json:"total_vector_count"`
	TargetTests           []string `json:"target_tests"`
	ValidatorBuildClosure struct {
		SchemaVersion    string `json:"schema_version"`
		ActivationStatus string `json:"activation_status"`
		Entrypoints      []struct {
			EntrypointID     string          `json:"entrypoint_id"`
			PackagePath      string          `json:"package_path"`
			DependencyPolicy string          `json:"dependency_policy"`
			TestEntrypoints  []string        `json:"test_entrypoints"`
			DirectSources    []artifactRefV1 `json:"direct_sources"`
			ExternalModules  []string        `json:"external_modules"`
		} `json:"entrypoints"`
	} `json:"validator_build_closure"`
}

type requestSpecV1 struct {
	RequestPath                   string
	SchemaVersion                 string
	RequestID                     string
	Gate                          string
	DecisionPrefix                string
	DecisionCount                 int
	DecisionDocumentPath          string
	CandidateOwnerRoles           [][]string
	RequiredOwnerRoles            []string
	UnmetEvidence                 []string
	BlockedProductionGates        []string
	BlockerCodes                  []string
	CandidateEvidenceManifestPath string
	CandidateEvidenceManifestHash string
}

var ownerRolePatternV1 = regexp.MustCompile(`^[a-z][a-z0-9_]*_owner$`)

func TestW2R03OwnerDecisionRequestV1(t *testing.T) {
	t.Parallel()
	verifyRequestV1(t, requestSpecsV1()[0])
}

func TestW2R04OwnerDecisionRequestV1(t *testing.T) {
	t.Parallel()
	verifyRequestV1(t, requestSpecsV1()[1])
}

func TestW2R03R04OwnerDecisionRequestV1StrictJSON(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"unknown":   []byte(`{"schema_version":"x","future":true}`),
		"duplicate": []byte(`{"schema_version":"x","schema_version":"y"}`),
		"trailing":  []byte(`{"schema_version":"x"}{}`),
		"null":      []byte(`{"implementation_unlocked":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := strictDecodeV1(raw); err == nil {
				t.Fatalf("Owner request 必须拒绝 %s JSON", name)
			}
		})
	}
}

func verifyRequestV1(t *testing.T, spec requestSpecV1) {
	t.Helper()
	repoRoot := repoRootV1(t)
	raw := readFileV1(t, repoRoot, spec.RequestPath)
	request, err := strictDecodeV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyRequestShapeV1(t, raw, spec.CandidateEvidenceManifestPath != "")

	if request.SchemaVersion != spec.SchemaVersion || request.RequestID != spec.RequestID || request.Gate != spec.Gate {
		t.Fatalf("%s Owner request identity 非法: %+v", spec.Gate, request)
	}
	if request.Status != "awaiting_owner_decision" || request.ImplementationUnlocked || request.OwnerRoleSetStatus != "provisional_candidates_not_final" {
		t.Fatalf("%s Owner request 不得提升状态或冻结 Owner: %+v", spec.Gate, request)
	}
	if !strings.Contains(request.BlockStatement, "禁止") || !strings.Contains(request.BlockStatement, "expansion_frozen") {
		t.Fatalf("%s Owner request 缺少失败关闭声明: %q", spec.Gate, request.BlockStatement)
	}

	verifyArtifactRefV1(t, repoRoot, request.DecisionDocument, spec.DecisionDocumentPath)
	verifyMatrixRowsV1(t, repoRoot, request.DecisionDocument.Path, spec)
	verifyArtifactRefV1(t, repoRoot, request.GateManifest, reviewFreezeManifestPathV1)
	verifyValidatorPackageV1(t, repoRoot, request.ValidatorSources)
	verifyLiveGateV1(t, repoRoot, request, spec)

	if !reflect.DeepEqual(request.UnmetEvidence, spec.UnmetEvidence) {
		t.Fatalf("%s unmet evidence=%v want=%v", spec.Gate, request.UnmetEvidence, spec.UnmetEvidence)
	}
	if !reflect.DeepEqual(request.BlockedProductionGates, spec.BlockedProductionGates) {
		t.Fatalf("%s blocked production gates=%v want=%v", spec.Gate, request.BlockedProductionGates, spec.BlockedProductionGates)
	}
	if len(request.Items) != spec.DecisionCount || len(spec.CandidateOwnerRoles) != spec.DecisionCount {
		t.Fatalf("%s decision items=%d roles=%d want=%d", spec.Gate, len(request.Items), len(spec.CandidateOwnerRoles), spec.DecisionCount)
	}
	for index, item := range request.Items {
		wantID := fmt.Sprintf("%s-D%02d", spec.DecisionPrefix, index+1)
		if item.DecisionID != wantID || item.Status != "awaiting_owner_decision" || strings.TrimSpace(item.Title) == "" {
			t.Fatalf("%s decision item[%d] identity/status 非法: %+v", spec.Gate, index, item)
		}
		wantOptions := []string{"accept_recommendation", "reject_keep_blocked"}
		if !reflect.DeepEqual(item.AllowedOptionIDs, wantOptions) || item.RecommendedOptionID != wantOptions[0] {
			t.Fatalf("%s option exact-set 非法: %+v", item.DecisionID, item)
		}
		if item.MatrixRowID != wantID || item.BlockCode != fmt.Sprintf("W2_%s_D%02d_OWNER_DECISION_PENDING", spec.DecisionPrefix, index+1) {
			t.Fatalf("%s matrix locator/block code 非法: %+v", item.DecisionID, item)
		}
		if !reflect.DeepEqual(item.CandidateOwnerRoles, spec.CandidateOwnerRoles[index]) {
			t.Fatalf("%s candidate owner roles=%v want=%v", item.DecisionID, item.CandidateOwnerRoles, spec.CandidateOwnerRoles[index])
		}
		if err := validateSortedRolesV1(item.CandidateOwnerRoles); err != nil {
			t.Fatalf("%s candidate owner roles 非法: %v", item.DecisionID, err)
		}
	}
}

func strictDecodeV1(raw []byte) (ownerDecisionRequestV1, error) {
	if err := validateJSONShapeV1(raw); err != nil {
		return ownerDecisionRequestV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request ownerDecisionRequestV1
	if err := decoder.Decode(&request); err != nil {
		return ownerDecisionRequestV1{}, err
	}
	if err := requireEOFV1(decoder); err != nil {
		return ownerDecisionRequestV1{}, err
	}
	return request, nil
}

func validateJSONShapeV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := readJSONValueV1(decoder); err != nil {
		return err
	}
	return requireEOFV1(decoder)
}

func readJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("JSON null 被禁止")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key 非字符串")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("JSON object 重复键 %q", key)
			}
			seen[key] = struct{}{}
			if valueErr := readJSONValueV1(decoder); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for decoder.More() {
			if valueErr := readJSONValueV1(decoder); valueErr != nil {
				return valueErr
			}
		}
	default:
		return fmt.Errorf("JSON delimiter 非法: %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	wantClosing := json.Delim('}')
	if delimiter == '[' {
		wantClosing = ']'
	}
	if closing != wantClosing {
		return fmt.Errorf("JSON closing delimiter=%v want=%v", closing, wantClosing)
	}
	return nil
}

func requireEOFV1(decoder *json.Decoder) error {
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("存在尾随 JSON token %v", token)
	}
	return nil
}

func verifyRequestShapeV1(t *testing.T, raw []byte, hasCandidate bool) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	wantRoot := []string{
		"block_statement", "blocked_production_gates", "decision_document", "gate", "gate_manifest",
		"implementation_unlocked", "items", "owner_role_set_status", "request_id", "schema_version",
		"status", "unmet_evidence", "validator_sources",
	}
	if hasCandidate {
		wantRoot = append(wantRoot, "candidate_contract_manifest")
		sort.Strings(wantRoot)
	}
	requireExactKeysV1(t, "request", root, wantRoot)
	for _, key := range []string{"decision_document", "gate_manifest"} {
		verifyArtifactShapeV1(t, key, root[key])
	}
	var validatorSources []json.RawMessage
	if err := json.Unmarshal(root["validator_sources"], &validatorSources); err != nil || validatorSources == nil || len(validatorSources) != 2 {
		t.Fatalf("validator_sources 必须是 two-source non-null array: %v", err)
	}
	for index, source := range validatorSources {
		verifyArtifactShapeV1(t, fmt.Sprintf("validator_sources[%d]", index), source)
	}
	if hasCandidate {
		verifyArtifactShapeV1(t, "candidate_contract_manifest", root["candidate_contract_manifest"])
	}
	var items []json.RawMessage
	if err := json.Unmarshal(root["items"], &items); err != nil || items == nil {
		t.Fatalf("items 必须是非 null array: %v", err)
	}
	itemKeys := []string{"allowed_option_ids", "block_code", "candidate_owner_roles", "decision_id", "matrix_row_id", "recommended_option_id", "status", "title"}
	for index, rawItem := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			t.Fatalf("item[%d] 非法: %v", index, err)
		}
		requireExactKeysV1(t, fmt.Sprintf("item[%d]", index), item, itemKeys)
	}
}

func verifyArtifactShapeV1(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		t.Fatalf("%s 不得为 null", label)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("%s 非法: %v", label, err)
	}
	requireExactKeysV1(t, label, object, []string{"path", "sha256"})
}

func requireExactKeysV1(t *testing.T, label string, object map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s keys=%v want=%v", label, got, want)
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 Owner request 测试源文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func readFileV1(t *testing.T, repoRoot, repoPath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(repoPath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath || path.Clean(ref.Path) != ref.Path || path.IsAbs(ref.Path) || strings.HasPrefix(ref.Path, "../") || strings.HasPrefix(ref.Path, ".github/") {
		t.Fatalf("Owner request artifact path=%q want=%q", ref.Path, wantPath)
	}
	if !strings.HasPrefix(ref.SHA256, "sha256:") || len(ref.SHA256) != len("sha256:")+sha256.Size*2 {
		t.Fatalf("%s SHA-256 格式非法: %q", ref.Path, ref.SHA256)
	}
	raw := readFileV1(t, repoRoot, ref.Path)
	digest := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(digest[:])
	if got != ref.SHA256 {
		t.Fatalf("%s raw SHA-256=%s want=%s", ref.Path, got, ref.SHA256)
	}
}

func verifyValidatorPackageV1(t *testing.T, repoRoot string, refs []artifactRefV1) {
	t.Helper()
	specs := []struct {
		Path        string
		PackageName string
		Imports     []string
	}{
		{
			Path:        ownerDecisionValidatorPathV1,
			PackageName: "w2ownerdecision_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path", "path/filepath", "reflect", "regexp", "runtime", "sort", "strings", "testing",
			},
		},
		{
			Path:        ownerDecisionGuardPathV1,
			PackageName: "w2ownerdecisionguard_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "go/ast", "go/parser", "go/token",
				"os", "path/filepath", "reflect", "runtime", "strings", "testing",
			},
		},
	}
	if len(refs) != len(specs) {
		t.Fatalf("validator_sources=%v want=%d-item exact-set", refs, len(specs))
	}
	for index, spec := range specs {
		verifyArtifactRefV1(t, repoRoot, refs[index], spec.Path)
		verifyValidatorSourcePackageV1(t, repoRoot, spec.Path, spec.PackageName, spec.Imports)
	}
}

func verifyValidatorSourcePackageV1(t *testing.T, repoRoot, repoPath, packageName string, wantImports []string) {
	t.Helper()
	sourcePath := filepath.Join(repoRoot, filepath.FromSlash(repoPath))
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	var goSources []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("validator source 不得为 symlink: %s", entry.Name())
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("validator source 必须是 regular file: %s", entry.Name())
		}
		goSources = append(goSources, entry.Name())
	}
	if want := []string{filepath.Base(sourcePath)}; !reflect.DeepEqual(goSources, want) {
		t.Fatalf("validator package Go source exact-set=%v want=%v", goSources, want)
	}

	raw := readFileV1(t, repoRoot, repoPath)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) {
		t.Fatal("validator source 禁止 build constraint")
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != packageName {
		t.Fatalf("validator package=%q", parsed.Name.Name)
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, importSpec := range parsed.Imports {
		if importSpec.Name != nil {
			t.Fatalf("validator import 禁止 alias/dot/blank: %s", importSpec.Path.Value)
		}
		imports = append(imports, strings.Trim(importSpec.Path.Value, `"`))
	}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Fatalf("validator stdlib import exact-set=%v want=%v", imports, wantImports)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("validator source 禁止 %s", function.Name.Name)
		}
	}
}

func verifyMatrixRowsV1(t *testing.T, repoRoot, matrixPath string, spec requestSpecV1) {
	t.Helper()
	raw := readFileV1(t, repoRoot, matrixPath)
	startMarker := []byte("## 3. Stable Owner 决策")
	endMarker := []byte("\n## 4.")
	if bytes.Count(raw, startMarker) != 1 {
		t.Fatalf("%s matrix 缺少唯一 Stable Owner 决策章节", spec.Gate)
	}
	start := bytes.Index(raw, startMarker) + len(startMarker)
	endOffset := bytes.Index(raw[start:], endMarker)
	if endOffset < 0 {
		t.Fatalf("%s matrix Stable Owner 决策章节缺少 §4 边界", spec.Gate)
	}
	section := raw[start : start+endOffset]
	pattern := regexp.MustCompile(`(?m)^\| \x60(` + spec.DecisionPrefix + `-D[0-9]{2})\x60 \|`)
	matches := pattern.FindAllSubmatch(section, -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, string(match[1]))
	}
	want := make([]string, spec.DecisionCount)
	for index := range want {
		want[index] = fmt.Sprintf("%s-D%02d", spec.DecisionPrefix, index+1)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s matrix rows=%v want=%v", spec.Gate, got, want)
	}
}

func verifyLiveGateV1(t *testing.T, repoRoot string, request ownerDecisionRequestV1, spec requestSpecV1) {
	t.Helper()
	raw := readFileV1(t, repoRoot, request.GateManifest.Path)
	var manifest reviewFreezeManifestProjectionV1
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, gate := range manifest.Gates {
		if gate.Gate != spec.Gate {
			continue
		}
		if gate.Status != "expansion_frozen" || string(gate.Freeze) != "null" || string(gate.ReopenException) != "null" {
			t.Fatalf("%s live Gate 不再是失败关闭状态: %+v", spec.Gate, gate)
		}
		if !reflect.DeepEqual(gate.RequiredOwnerRoles, spec.RequiredOwnerRoles) {
			t.Fatalf("%s live provisional roles=%v want=%v", spec.Gate, gate.RequiredOwnerRoles, spec.RequiredOwnerRoles)
		}
		codes := make([]string, len(gate.Blockers))
		for index, blocker := range gate.Blockers {
			codes[index] = blocker.Code
		}
		if !reflect.DeepEqual(codes, spec.BlockerCodes) {
			t.Fatalf("%s live blocker codes=%v want=%v", spec.Gate, codes, spec.BlockerCodes)
		}
		verifyCandidateV1(t, repoRoot, request, spec, gate.CandidateEvidence)
		return
	}
	t.Fatalf("Review Freeze manifest 缺少 %s", spec.Gate)
}

func verifyCandidateV1(t *testing.T, repoRoot string, request ownerDecisionRequestV1, spec requestSpecV1, candidates []struct {
	Scope                  string   `json:"scope"`
	Coverage               string   `json:"coverage"`
	ContractManifestPath   string   `json:"contract_manifest_path"`
	ContractManifestSHA256 string   `json:"contract_manifest_sha256"`
	VectorIDs              []string `json:"vector_ids"`
	TargetTests            []string `json:"target_tests"`
}) {
	t.Helper()
	if spec.CandidateEvidenceManifestPath == "" {
		if request.CandidateContractManifest != nil || len(candidates) != 0 {
			t.Fatalf("%s 不得自报 candidate manifest: request=%+v live=%d", spec.Gate, request.CandidateContractManifest, len(candidates))
		}
		return
	}
	if request.CandidateContractManifest == nil || len(candidates) != 1 {
		t.Fatalf("%s 必须绑定唯一 partial candidate", spec.Gate)
	}
	verifyArtifactRefV1(t, repoRoot, *request.CandidateContractManifest, spec.CandidateEvidenceManifestPath)
	if request.CandidateContractManifest.SHA256 != spec.CandidateEvidenceManifestHash {
		t.Fatalf("%s candidate request hash=%s want=%s", spec.Gate, request.CandidateContractManifest.SHA256, spec.CandidateEvidenceManifestHash)
	}
	candidate := candidates[0]
	if candidate.Scope != "creation_spec_activation_consumption_core_candidate" || candidate.Coverage != "partial_candidate" || candidate.ContractManifestPath != request.CandidateContractManifest.Path || candidate.ContractManifestSHA256 != request.CandidateContractManifest.SHA256 {
		t.Fatalf("%s live candidate identity 非法: %+v", spec.Gate, candidate)
	}

	raw := readFileV1(t, repoRoot, request.CandidateContractManifest.Path)
	var contract candidateContractManifestProjectionV1
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	wantFixtureIDs := []string{
		"acr.creation_spec_activation.approved_recorded",
		"acr.creation_spec_activation.approved_recorded_current_drift",
		"acr.creation_spec_activation.approved_unconsumed",
		"acr.creation_spec_activation.rejected_unconsumed",
	}
	if contract.SchemaVersion != "w2_r04_approval_consumption_manifest.v1" || !reflect.DeepEqual(contract.FixtureIDs, wantFixtureIDs) || contract.TotalVectorCount != 111 || len(contract.VectorIDs) != 111 || len(contract.TargetTests) != 11 {
		t.Fatalf("%s candidate manifest coverage 非法: %+v", spec.Gate, contract)
	}
	if !equalStringSetV1(candidate.VectorIDs, contract.VectorIDs) || !equalStringSetV1(candidate.TargetTests, contract.TargetTests) {
		t.Fatalf("%s Gate candidate 与 contract manifest exact-set 漂移", spec.Gate)
	}
	closure := contract.ValidatorBuildClosure
	if closure.SchemaVersion != "w2_validator_build_closure.v1" || closure.ActivationStatus != "candidate_unactivated" || len(closure.Entrypoints) != 1 {
		t.Fatalf("%s candidate build closure 非法: %+v", spec.Gate, closure)
	}
	entrypoint := closure.Entrypoints[0]
	if entrypoint.EntrypointID != "W2-R04.approval_consumption" || entrypoint.PackagePath != "agent/tests/contract/w2r04approvalconsumption" || entrypoint.DependencyPolicy != "stdlib_only" || len(entrypoint.DirectSources) != 2 || len(entrypoint.ExternalModules) != 0 || !equalStringSetV1(entrypoint.TestEntrypoints, contract.TargetTests) {
		t.Fatalf("%s candidate validator entrypoint 非法: %+v", spec.Gate, entrypoint)
	}
}

func equalStringSetV1(left, right []string) bool {
	leftCopy, leftUnique := sortedUniqueStringsV1(left)
	rightCopy, rightUnique := sortedUniqueStringsV1(right)
	if !leftUnique || !rightUnique {
		return false
	}
	return reflect.DeepEqual(leftCopy, rightCopy)
}

func sortedUniqueStringsV1(values []string) ([]string, bool) {
	if len(values) == 0 {
		return nil, false
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if strings.TrimSpace(value) == "" || (index > 0 && result[index-1] == value) {
			return nil, false
		}
	}
	return result, true
}

func validateSortedRolesV1(roles []string) error {
	if len(roles) == 0 || !sort.StringsAreSorted(roles) {
		return fmt.Errorf("role 集必须非空且排序: %v", roles)
	}
	for index, role := range roles {
		if !ownerRolePatternV1.MatchString(role) {
			return fmt.Errorf("role key 非法: %q", role)
		}
		if index > 0 && roles[index-1] == role {
			return fmt.Errorf("role key 重复: %q", role)
		}
	}
	return nil
}

func requestSpecsV1() []requestSpecV1 {
	return []requestSpecV1{
		{
			RequestPath:          r03OwnerDecisionRequestPathV1,
			SchemaVersion:        "w2_r03_owner_decision_request.v1",
			RequestID:            "DR-W2-R03-v1",
			Gate:                 "W2-R03",
			DecisionPrefix:       "R03",
			DecisionCount:        14,
			DecisionDocumentPath: "docs/design/agent/w2-r03-owner-decision-matrix-v1.md",
			CandidateOwnerRoles: [][]string{
				{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "security_owner", "test_owner"},
				{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "frontend_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "data_owner", "finance_owner", "frontend_owner", "integration_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
			},
			RequiredOwnerRoles: []string{"agent_owner", "business_owner", "finance_owner", "frontend_owner", "product_owner", "security_owner", "test_owner"},
			UnmetEvidence: []string{
				"ADR_DISPOSITIONS_PENDING",
				"BUILD_TRUST_CLOSURE_MISSING",
				"CHILD_CORPUS_MISSING",
				"FINAL_OWNER_ROLE_EXACT_SET_MISSING",
				"GOVERNANCE_AUTHORITY_NOT_ACTIVE",
				"OWNER_DECISIONS_PENDING",
				"PRODUCTION_AUTHORITY_QUERY_MISSING",
				"PRODUCTION_DATABASE_EVIDENCE_MISSING",
				"R03_AGGREGATE_MANIFEST_MISSING",
			},
			BlockedProductionGates: []string{"W2-B1"},
			BlockerCodes: []string{
				"W2_R03_CHILD_CORPUS_PENDING",
				"W2_R03_OWNER_APPROVAL_MISSING",
			},
		},
		{
			RequestPath:          r04OwnerDecisionRequestPathV1,
			SchemaVersion:        "w2_r04_owner_decision_request.v1",
			RequestID:            "DR-W2-R04-v1",
			Gate:                 "W2-R04",
			DecisionPrefix:       "R04",
			DecisionCount:        20,
			DecisionDocumentPath: "docs/design/agent/w2-r04-owner-decision-matrix-v1.md",
			CandidateOwnerRoles: [][]string{
				{"agent_owner", "business_owner", "finance_owner", "frontend_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner"},
				{"agent_owner", "business_owner", "finance_owner", "test_owner"},
				{"agent_owner", "business_owner", "finance_owner", "security_owner", "test_owner"},
				{"business_owner", "finance_owner", "product_owner", "security_owner"},
				{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner"},
				{"agent_owner", "business_owner", "finance_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "operations_owner", "security_owner", "test_owner"},
				{"business_owner", "finance_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "product_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "security_owner", "test_owner"},
				{"agent_owner", "business_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
			},
			RequiredOwnerRoles: []string{"agent_owner", "business_owner", "finance_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
			UnmetEvidence: []string{
				"ACTIVATION_CONSUMPTION_CANDIDATE_PARTIAL_ONLY",
				"ADR_DISPOSITIONS_PENDING",
				"BUILD_TRUST_CLOSURE_MISSING",
				"FINAL_OWNER_ROLE_EXACT_SET_MISSING",
				"FULL_GATE_BASELINE_MISSING",
				"GOVERNANCE_AUTHORITY_NOT_ACTIVE",
				"OWNER_DECISIONS_PENDING",
				"R04_AGGREGATE_MANIFEST_MISSING",
				"UPSTREAM_GATES_NOT_APPROVED",
			},
			BlockedProductionGates: []string{"W2-B0b", "W2-B1"},
			BlockerCodes: []string{
				"W2_R04_FULL_GATE_BASELINE_MISSING",
				"W2_R04_OWNER_APPROVAL_MISSING",
				"W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING",
			},
			CandidateEvidenceManifestPath: "agent/tests/contract/testdata/w2_r04_approval_consumption/manifest.json",
			CandidateEvidenceManifestHash: "sha256:6ad8c58dbeeaf514994fc3db8c8beca0192de6b2e52121201060f37a7c900ba0",
		},
	}
}
