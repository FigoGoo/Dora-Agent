// Package w2r00candidatepreparationguard_test 从语义 validator 包外固定 R00 候选准备请求的源码与失败关闭边界。
package w2r00candidatepreparationguard_test

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
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	requestPathV1            = "docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json"
	validatorPathV1          = "agent/tests/contract/w2r00candidatepreparation/candidate_preparation_request_v1_test.go"
	guardPathV1              = "agent/tests/contract/w2r00candidatepreparationguard/candidate_preparation_request_guard_v1_test.go"
	itemRequirementsSHA256V1 = "sha256:34c85a8d0f9c72df0eb1a52dee54a150175084a48b395f561a6049787e9f6f2b"
	blockStatementV1         = "本 CPR 只固定 R00-D01～D14 的 readiness 基线，以及 R00-D05/D07/D08/D09/D11/D13 仍缺少的版本化候选输入与关闭证据；它不提交任何 Policy 数值、Price/Model mapping、Provider 能力事实、ModelReceipt 字段、时钟阈值、slot ordinal、Owner 角色或 ballot 选项。R00 继续为 expansion_frozen、candidate_evidence 为空、freeze/reopen_exception 为空，禁止生成 DR-W2-R00-v1、canonical/IDL/vector/test manifest、Owner approval、Formal Freeze、W2-B0a/W2-B1 或生产实现。"
)

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type requestProjectionV1 struct {
	SchemaVersion          string          `json:"schema_version"`
	RequestID              string          `json:"request_id"`
	RequestKind            string          `json:"request_kind"`
	Gate                   string          `json:"gate"`
	Status                 string          `json:"status"`
	ApprovalStatus         string          `json:"approval_status"`
	ImplementationStatus   string          `json:"implementation_status"`
	EvidenceStatus         string          `json:"evidence_status"`
	ImplementationUnlocked bool            `json:"implementation_unlocked"`
	BallotEnabled          bool            `json:"ballot_enabled"`
	OwnerRoleSetStatus     string          `json:"owner_role_set_status"`
	DecisionDocument       json.RawMessage `json:"decision_document"`
	SourceContract         json.RawMessage `json:"source_contract"`
	GateManifest           json.RawMessage `json:"gate_manifest"`
	ValidatorSources       []artifactRefV1 `json:"validator_sources"`
	ReadinessBaseline      json.RawMessage `json:"readiness_baseline"`
	Items                  json.RawMessage `json:"items"`
	UnmetEvidence          []string        `json:"unmet_evidence"`
	BlockedProductionGates []string        `json:"blocked_production_gates"`
	ForbiddenCapabilities  []string        `json:"forbidden_capabilities"`
	BlockStatement         string          `json:"block_statement"`
}

type itemProjectionV1 struct {
	DecisionID                string `json:"decision_id"`
	ReadinessStatus           string `json:"readiness_status"`
	CandidateSubmissionStatus string `json:"candidate_submission_status"`
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Imports     []string
}

// TestW2R00CandidatePreparationRequestGuardV1 交叉固定请求身份、六项 readiness 与两份单源码 stdlib-only validator。
func TestW2R00CandidatePreparationRequestGuardV1(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV1(t)
	verifyRequestDirectoryV1(t, repoRoot)
	raw := readFileV1(t, repoRoot, requestPathV1)
	request, err := strictDecodeProjectionV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyRootExactKeysV1(t, raw)
	if request.SchemaVersion != "w2_r00_candidate_preparation_request.v1" || request.RequestID != "CPR-W2-R00-v1" || request.RequestKind != "candidate_input_requirements_only" || request.Gate != "W2-R00" {
		t.Fatalf("R00 candidate preparation identity 漂移: %+v", request)
	}
	if request.Status != "readiness_inventory_only" || request.ApprovalStatus != "not_requested" || request.ImplementationStatus != "prohibited" || request.EvidenceStatus != "candidate_only" || request.ImplementationUnlocked || request.BallotEnabled || request.OwnerRoleSetStatus != "scope_not_derived" || request.BlockStatement != blockStatementV1 {
		t.Fatalf("R00 candidate preparation 失败关闭状态漂移: %+v", request)
	}
	if !itemRequirementsDigestMatchesV1(request.Items) {
		t.Fatalf("R00 candidate preparation items semantic SHA 漂移")
	}
	var items []itemProjectionV1
	if err := json.Unmarshal(request.Items, &items); err != nil {
		t.Fatal(err)
	}
	wantDecisions := []string{"R00-D05", "R00-D07", "R00-D08", "R00-D09", "R00-D11", "R00-D13"}
	if len(items) != len(wantDecisions) {
		t.Fatalf("R00 candidate preparation items=%d want=%d", len(items), len(wantDecisions))
	}
	for index, item := range items {
		if item.DecisionID != wantDecisions[index] || item.ReadinessStatus != "candidate_incomplete_not_ballot_ready" || item.CandidateSubmissionStatus != "missing_not_submitted" {
			t.Fatalf("R00 candidate preparation item[%d] 漂移: %+v", index, item)
		}
	}
	verifyForbiddenKeysV1(t, raw)

	specs := sourceSpecsV1()
	if len(request.ValidatorSources) != len(specs) {
		t.Fatalf("validator_sources=%v", request.ValidatorSources)
	}
	for index, spec := range specs {
		verifyArtifactRefV1(t, repoRoot, request.ValidatorSources[index], spec.Path)
		verifySourcePackageV1(t, repoRoot, spec)
	}
}

// TestW2R00CandidatePreparationRequestGuardV1StrictJSON 防止外部 guard 被弱化为宽松 JSON 读取。
func TestW2R00CandidatePreparationRequestGuardV1StrictJSON(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"duplicate": []byte(`{"x":"a","x":"b"}`),
		"null":      []byte(`{"x":null}`),
		"number":    []byte(`{"x":1}`),
		"trailing":  []byte(`{"x":"a"}{}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateJSONShapeV1(raw); err == nil {
				t.Fatalf("guard 必须拒绝 %s JSON", name)
			}
		})
	}
	if _, err := strictDecodeProjectionV1([]byte(`{"schema_version":"x","future":true}`)); err == nil {
		t.Fatal("guard projection 必须拒绝未知字段")
	}
}

// TestW2R00CandidatePreparationRequestGuardV1AuthorityFieldMutation 固定外层 guard 自身也拒绝选择与批准者自报字段。
func TestW2R00CandidatePreparationRequestGuardV1AuthorityFieldMutation(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV1(t)
	raw := readFileV1(t, repoRoot, requestPathV1)
	for name, field := range map[string]string{
		"selected_option": `"selected_option":"accept_recommendation",`,
		"approver_role":   `"approver_role":"finance_owner",`,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := bytes.Replace(raw, []byte(`"request_kind":`), []byte(field+`"request_kind":`), 1)
			if _, err := strictDecodeProjectionV1(mutated); err == nil {
				t.Fatalf("guard projection 必须拒绝 %s", name)
			}
		})
	}
}

func strictDecodeProjectionV1(raw []byte) (requestProjectionV1, error) {
	if err := validateJSONShapeV1(raw); err != nil {
		return requestProjectionV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request requestProjectionV1
	if err := decoder.Decode(&request); err != nil {
		return requestProjectionV1{}, err
	}
	if err := requireEOFV1(decoder); err != nil {
		return requestProjectionV1{}, err
	}
	return request, nil
}

func verifyRootExactKeysV1(t *testing.T, raw []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	actual := make([]string, 0, len(root))
	for key := range root {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	want := []string{
		"approval_status", "ballot_enabled", "block_statement", "blocked_production_gates", "decision_document",
		"evidence_status", "forbidden_capabilities", "gate", "gate_manifest", "implementation_status",
		"implementation_unlocked", "items", "owner_role_set_status", "readiness_baseline", "request_id",
		"request_kind", "schema_version", "source_contract", "status", "unmet_evidence", "validator_sources",
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("guard root keys=%v want=%v", actual, want)
	}
}

func itemRequirementsDigestMatchesV1(raw json.RawMessage) bool {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return false
	}
	digest := sha256.Sum256(compact.Bytes())
	return "sha256:"+hex.EncodeToString(digest[:]) == itemRequirementsSHA256V1
}

func verifyRequestDirectoryV1(t *testing.T, repoRoot string) {
	t.Helper()
	directory := filepath.Dir(filepath.Join(repoRoot, filepath.FromSlash(requestPathV1)))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("candidate preparation request 不得为 symlink: %s", entry.Name())
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			t.Fatalf("candidate preparation request 必须是 mode 0644 regular file: %s mode=%s", entry.Name(), info.Mode())
		}
		names = append(names, entry.Name())
	}
	if want := []string{filepath.Base(requestPathV1)}; !reflect.DeepEqual(names, want) {
		t.Fatalf("candidate preparation request exact-set=%v want=%v", names, want)
	}
}

func verifyForbiddenKeysV1(t *testing.T, raw []byte) {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	forbidden := make(map[string]struct{})
	for _, key := range []string{
		"accepted", "accepted_option_id", "actor_id", "allowed_option_ids", "approval_refs", "approved", "approved_at", "approver_role",
		"candidate_contract_manifest", "candidate_evidence", "candidate_owner_roles", "canonical_contract_manifest", "commit_sha",
		"compile_attestation", "contract_manifest_sha256", "exact_value", "freeze", "head_sha", "idl_manifest",
		"owner_approvals", "recommended_option_id", "reopen_exception", "required_owner_roles", "review_commit_sha",
		"review_id", "review_state", "review_url", "reviewer_id", "selected_option", "selected_option_id", "target_tests",
		"validator_build_closure", "value", "vector_exact_set",
	} {
		forbidden[key] = struct{}{}
	}
	walkKeysV1(t, "$", value, forbidden)
}

func walkKeysV1(t *testing.T, path string, value any, forbidden map[string]struct{}) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, denied := forbidden[key]; denied {
				t.Fatalf("guard 发现禁止字段 %s.%s", path, key)
			}
			walkKeysV1(t, path+"."+key, child, forbidden)
		}
	case []any:
		for index, child := range typed {
			walkKeysV1(t, fmt.Sprintf("%s[%d]", path, index), child, forbidden)
		}
	case json.Number, float64:
		t.Fatalf("guard 发现数值 %s=%v", path, typed)
	case nil:
		t.Fatalf("guard 发现 null %s", path)
	}
}

func validateJSONShapeV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
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
	if _, number := token.(json.Number); number {
		return fmt.Errorf("JSON number 被禁止")
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
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 含尾随值")
		}
		return err
	}
	return nil
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath {
		t.Fatalf("validator source path=%q want=%q", ref.Path, wantPath)
	}
	raw := readFileV1(t, repoRoot, ref.Path)
	digest := sha256.Sum256(raw)
	wantHash := "sha256:" + hex.EncodeToString(digest[:])
	if ref.SHA256 != wantHash {
		t.Fatalf("%s raw SHA-256=%s want=%s", ref.Path, wantHash, ref.SHA256)
	}
}

func verifySourcePackageV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	sourcePath := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(sourcePath) {
		t.Fatalf("%s source directory exact-set=%v want=%s", spec.PackageName, entries, filepath.Base(sourcePath))
	}
	entry := entries[0]
	if entry.Type()&os.ModeSymlink != 0 {
		t.Fatalf("validator source 不得为 symlink: %s", entry.Name())
	}
	info, infoErr := entry.Info()
	if infoErr != nil {
		t.Fatal(infoErr)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("validator source 必须是 mode 0644 regular file: %s mode=%s", entry.Name(), info.Mode())
	}
	raw := readFileV1(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) || bytes.Contains(raw, []byte("//"+"go:embed")) {
		t.Fatalf("%s 禁止 build constraint/embed", spec.PackageName)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("validator package=%q want=%q", parsed.Name.Name, spec.PackageName)
	}
	var imports []string
	for _, importSpec := range parsed.Imports {
		if importSpec.Name != nil {
			t.Fatalf("validator import 禁止 alias/dot/blank: %s", importSpec.Path.Value)
		}
		imports = append(imports, strings.Trim(importSpec.Path.Value, `"`))
	}
	if !reflect.DeepEqual(imports, spec.Imports) {
		t.Fatalf("%s stdlib import exact-set=%v want=%v", spec.PackageName, imports, spec.Imports)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("%s 禁止 %s", spec.PackageName, function.Name.Name)
		}
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	return []sourceSpecV1{
		{
			Path:        validatorPathV1,
			PackageName: "w2r00candidatepreparation_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path/filepath", "reflect", "regexp", "runtime", "sort", "strings", "testing",
			},
		},
		{
			Path:        guardPathV1,
			PackageName: "w2r00candidatepreparationguard_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing",
			},
		},
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 R00 candidate preparation guard")
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
