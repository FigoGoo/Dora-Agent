// Package w2r00d09candidateguard_test 从独立源码固定 R00-D09 候选输入包的完整语义与不越权边界。
package w2r00d09candidateguard_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	candidateDirectoryV1     = "docs/design/agent/approvals/w2-r00-candidate-inputs/R00-D09-v1"
	candidatePathV1          = candidateDirectoryV1 + "/candidate-input.json"
	vectorsPathV1            = candidateDirectoryV1 + "/terminal-model-receipt-v1-vectors.json"
	validatorPathV1          = "agent/tests/contract/w2r00d09candidate/model_receipt_candidate_v1_test.go"
	guardPathV1              = "agent/tests/contract/w2r00d09candidateguard/model_receipt_candidate_guard_v1_test.go"
	contractSemanticSHA256V1 = "sha256:151a262aa00d365b0acfb380ec2681a45dbc33e0acc2c39d21ffa26e15532ac3"
	vectorsSemanticSHA256V1  = "sha256:d8871cd22c3b1cc9eb9c1aed32e936fa0c7c9d2cfa30d5d5f9b0f8ea25fe120e"
	maxSafeIntegerV1         = int64(9007199254740991)
)

type candidateProjectionV1 struct {
	SchemaVersion          string          `json:"schema_version"`
	ArtifactID             string          `json:"artifact_id"`
	ArtifactKind           string          `json:"artifact_kind"`
	Gate                   string          `json:"gate"`
	DecisionID             string          `json:"decision_id"`
	SourceOpenItemIDs      json.RawMessage `json:"source_open_item_ids"`
	Status                 string          `json:"status"`
	RegistrationStatus     string          `json:"registration_status"`
	OwnerRequestStatus     string          `json:"owner_request_status"`
	ApprovalStatus         string          `json:"approval_status"`
	ImplementationStatus   string          `json:"implementation_status"`
	EvidenceStatus         string          `json:"evidence_status"`
	ImplementationUnlocked bool            `json:"implementation_unlocked"`
	BallotEnabled          bool            `json:"ballot_enabled"`
	PrerequisiteRefs       json.RawMessage `json:"prerequisite_refs"`
	CrossGateAlignmentRefs json.RawMessage `json:"cross_gate_alignment_refs"`
	SourceArtifacts        json.RawMessage `json:"source_artifacts"`
	ValidatorSources       json.RawMessage `json:"validator_sources"`
	ContractCandidate      json.RawMessage `json:"contract_candidate"`
	RequiredEvidence       json.RawMessage `json:"required_evidence_projection"`
	UnmetEvidence          []string        `json:"unmet_registration_evidence"`
	ForbiddenClaims        []string        `json:"forbidden_claims"`
	BlockStatement         string          `json:"block_statement"`
}

type vectorProjectionV1 struct {
	SchemaVersion                string          `json:"schema_version"`
	ArtifactID                   string          `json:"artifact_id"`
	CandidateInputID             string          `json:"candidate_input_id"`
	TerminalReceiptSchemaVersion string          `json:"terminal_receipt_schema_version"`
	DigestDomain                 string          `json:"digest_domain"`
	ValidCases                   json.RawMessage `json:"valid_cases"`
	InvalidCases                 json.RawMessage `json:"invalid_cases"`
	TerminalUniquenessCases      json.RawMessage `json:"terminal_uniqueness_cases"`
	FinalizeGuardCases           json.RawMessage `json:"finalize_guard_cases"`
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Imports     []string
}

// TestW2R00D09CandidateGuardV1 交叉固定候选状态、完整 contract/vector 摘要、文件 exact-set 与两个独立 validator 包。
func TestW2R00D09CandidateGuardV1(t *testing.T) {
	t.Parallel()

	root := repoRootV1(t)
	verifyCandidateDirectoryV1(t, root)
	candidateRaw := readFileV1(t, root, candidatePathV1)
	if err := validateJSONShapeV1(candidateRaw); err != nil {
		t.Fatal(err)
	}
	candidate := strictDecodeCandidateV1(t, candidateRaw)
	if candidate.SchemaVersion != "w2_r00_d09_candidate_input.v1" || candidate.ArtifactID != "CI-W2-R00-D09-v1" || candidate.ArtifactKind != "versioned_candidate_input_only" || candidate.Gate != "W2-R00" || candidate.DecisionID != "R00-D09" {
		t.Fatalf("D09 candidate identity 漂移: %+v", candidate)
	}
	if !candidateFailureClosedV1(candidate) {
		t.Fatalf("D09 candidate failure-closed 状态漂移: %+v", candidate)
	}
	if semanticDigestV1(candidate.ContractCandidate) != contractSemanticSHA256V1 {
		t.Fatalf("contract semantic digest 漂移: %s", semanticDigestV1(candidate.ContractCandidate))
	}
	if !reflect.DeepEqual(candidate.UnmetEvidence, []string{
		"BUSINESS_FINALIZE_PUBLIC_IDL_AND_GOLDEN_MISSING",
		"D08_REAL_PROVIDER_CAPABILITY_AND_MAPPING_MISSING",
		"D11_CLOCK_THRESHOLD_AND_MONITORING_MISSING",
		"D13_FINALIZE_SLOT_ORDINAL_MISSING",
		"OWNER_DISPOSITIONS_R01_D02_R02_D09_R04_D14_MISSING",
		"POSTGRESQL_TERMINAL_UNIQUENESS_AND_CRASH_EVIDENCE_MISSING",
		"PRODUCTION_MODEL_RECEIPT_IMPLEMENTATION_MISSING",
		"TRUSTED_BUILD_AND_REVIEW_FREEZE_MISSING",
	}) {
		t.Fatalf("unmet evidence 漂移: %v", candidate.UnmetEvidence)
	}
	verifyForbiddenKeysV1(t, candidateRaw)
	verifySourceRefsV1(t, root, candidate.SourceArtifacts)
	verifyVectorsDigestV1(t, readFileV1(t, root, vectorsPathV1))
	verifyValidatorRefsV1(t, root, candidate.ValidatorSources)
	for _, spec := range sourceSpecsV1() {
		verifySourcePackageV1(t, root, spec)
	}
}

func candidateFailureClosedV1(candidate candidateProjectionV1) bool {
	return candidate.Status == "prepared_unregistered" && candidate.RegistrationStatus == "not_registered" && candidate.OwnerRequestStatus == "not_created" && candidate.ApprovalStatus == "not_requested" && candidate.ImplementationStatus == "prohibited" && candidate.EvidenceStatus == "candidate_only" && !candidate.ImplementationUnlocked && !candidate.BallotEnabled
}

func verifySourceRefsV1(t *testing.T, repoRoot string, raw json.RawMessage) {
	t.Helper()
	var refs []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{
		"docs/design/agent/w2-r00-owner-decision-matrix-v1.md",
		"docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json",
		"docs/design/cross-module/graph-execution-billing-contract-v1.md",
		"docs/design/agent/runner-session-lane-review-v1.md",
		"docs/design/agent/w2-r01-owner-decision-matrix-v1.md",
		"docs/design/agent/w2-r02-owner-decision-matrix-v1.md",
		"docs/design/agent/w2-r04-owner-decision-matrix-v1.md",
		"docs/design/agent/graph-tool-result-receipt-contract-v1.md",
		"docs/design/agent/immutable-turn-context-design-v1.md",
		"docs/design/agent/graphtool/plan_creation_spec-w2-r04-gap-review.md",
	}
	if len(refs) != len(wantPaths) {
		t.Fatalf("source refs=%d want=%d", len(refs), len(wantPaths))
	}
	for index, ref := range refs {
		if ref.Path != wantPaths[index] {
			t.Fatalf("source ref[%d]=%s want=%s", index, ref.Path, wantPaths[index])
		}
		verifyRegularFileV1(t, repoRoot, ref.Path)
		digest := sha256.Sum256(readFileV1(t, repoRoot, ref.Path))
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != ref.SHA256 {
			t.Fatalf("source ref[%d] digest=%s want=%s", index, got, ref.SHA256)
		}
	}
}

func verifyValidatorRefsV1(t *testing.T, repoRoot string, raw json.RawMessage) {
	t.Helper()
	var refs []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) != 2 {
		t.Fatalf("validator refs=%d err=%v", len(refs), err)
	}
	specs := sourceSpecsV1()
	for index, ref := range refs {
		if ref.Path != specs[index].Path {
			t.Fatalf("validator ref[%d] path=%s want=%s", index, ref.Path, specs[index].Path)
		}
		verifyRegularFileV1(t, repoRoot, ref.Path)
		digest := sha256.Sum256(readFileV1(t, repoRoot, ref.Path))
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != ref.SHA256 {
			t.Fatalf("validator ref[%d] digest=%s want=%s", index, got, ref.SHA256)
		}
	}
}

// TestW2R00D09CandidateGuardMutationV1 确保独立 guard 会拒绝候选注册、Owner 请求、批准、实现解锁和真实 Provider 声称。
func TestW2R00D09CandidateGuardMutationV1(t *testing.T) {
	t.Parallel()

	raw := readFileV1(t, repoRootV1(t), candidatePathV1)
	mutations := map[string][]byte{
		"registered":        bytes.Replace(raw, []byte(`"registration_status": "not_registered"`), []byte(`"registration_status": "registered"`), 1),
		"owner_request":     bytes.Replace(raw, []byte(`"owner_request_status": "not_created"`), []byte(`"owner_request_status": "created"`), 1),
		"approved":          bytes.Replace(raw, []byte(`"approval_status": "not_requested"`), []byte(`"approval_status": "approved"`), 1),
		"unlocked":          bytes.Replace(raw, []byte(`"implementation_unlocked": false`), []byte(`"implementation_unlocked": true`), 1),
		"provider_verified": bytes.Replace(raw, []byte(`"status": "prepared_unregistered"`), []byte(`"provider_capability_verified": true,"status": "prepared_unregistered"`), 1),
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate, err := decodeCandidateV1(mutated)
			if err != nil {
				return
			}
			if candidateFailureClosedV1(candidate) {
				t.Fatalf("guard 接受了 %s mutation", name)
			}
		})
	}
}

// TestW2R00D09CandidateGuardStrictJSONV1 固定 guard 本身拒绝递归重复键、null、浮点、指数和尾随值。
func TestW2R00D09CandidateGuardStrictJSONV1(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"duplicate":        []byte(`{"x":{"y":"a","y":"b"}}`),
		"null":             []byte(`{"x":null}`),
		"float":            []byte(`{"x":1.5}`),
		"exponent":         []byte(`{"x":1e2}`),
		"non_safe_integer": []byte(`{"x":9007199254740992}`),
		"invalid_utf8":     {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"trailing":         []byte(`{"x":"a"}{}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateJSONShapeV1(raw); err == nil {
				t.Fatalf("guard 必须拒绝 %s", name)
			}
		})
	}
}

func strictDecodeCandidateV1(t *testing.T, raw []byte) candidateProjectionV1 {
	t.Helper()
	candidate, err := decodeCandidateV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func decodeCandidateV1(raw []byte) (candidateProjectionV1, error) {
	if err := validateJSONShapeV1(raw); err != nil {
		return candidateProjectionV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var candidate candidateProjectionV1
	if err := decoder.Decode(&candidate); err != nil {
		return candidateProjectionV1{}, err
	}
	if err := requireEOFV1(decoder); err != nil {
		return candidateProjectionV1{}, err
	}
	return candidate, nil
}

func verifyVectorsDigestV1(t *testing.T, raw []byte) {
	t.Helper()
	if err := validateJSONShapeV1(raw); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var vectors vectorProjectionV1
	if err := decoder.Decode(&vectors); err != nil {
		t.Fatal(err)
	}
	if err := requireEOFV1(decoder); err != nil {
		t.Fatal(err)
	}
	if vectors.SchemaVersion != "w2_r00_d09_terminal_model_receipt_vectors.v1" || vectors.ArtifactID != "VEC-W2-R00-D09-v1" || vectors.CandidateInputID != "CI-W2-R00-D09-v1" || vectors.TerminalReceiptSchemaVersion != "dora.agent.terminal_model_receipt.v1" || vectors.DigestDomain != "dora.agent.model_receipt.terminal.v1" {
		t.Fatalf("vector identity 漂移: %+v", vectors)
	}
	payload := struct {
		ValidCases              json.RawMessage `json:"valid_cases"`
		InvalidCases            json.RawMessage `json:"invalid_cases"`
		TerminalUniquenessCases json.RawMessage `json:"terminal_uniqueness_cases"`
		FinalizeGuardCases      json.RawMessage `json:"finalize_guard_cases"`
	}{vectors.ValidCases, vectors.InvalidCases, vectors.TerminalUniquenessCases, vectors.FinalizeGuardCases}
	compact, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(compact)
	if got := "sha256:" + hex.EncodeToString(digest[:]); got != vectorsSemanticSHA256V1 {
		t.Fatalf("vector semantic digest=%s want=%s", got, vectorsSemanticSHA256V1)
	}
}

func semanticDigestV1(raw json.RawMessage) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return ""
	}
	digest := sha256.Sum256(compact.Bytes())
	return "sha256:" + hex.EncodeToString(digest[:])
}

func verifyForbiddenKeysV1(t *testing.T, raw []byte) {
	t.Helper()
	for _, key := range []string{
		"approver", "approver_role", "candidate_owner_roles", "candidate_evidence", "compile_attestation", "freeze",
		"head_sha", "idl_manifest", "owner_approval", "platform_review", "provider_capability_verified", "required_owner_roles",
		"selected_option", "slot_ordinal", "trusted_build", "unlocked_gate",
	} {
		if jsonKeyExistsV1(raw, key) {
			t.Fatalf("candidate artifact 含禁止字段 %q", key)
		}
	}
}

func jsonKeyExistsV1(raw []byte, target string) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	return containsJSONKeyV1(value, target)
}

func containsJSONKeyV1(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == target || containsJSONKeyV1(nested, target) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsJSONKeyV1(nested, target) {
				return true
			}
		}
	}
	return false
}

func verifyCandidateDirectoryV1(t *testing.T, repoRoot string) {
	t.Helper()
	directory := filepath.Join(repoRoot, filepath.FromSlash(candidateDirectoryV1))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			t.Fatalf("candidate directory 含非法 entry %s", entry.Name())
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("candidate file %s mode=%o want=644", entry.Name(), info.Mode().Perm())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if want := []string{"candidate-input.json", "terminal-model-receipt-v1-vectors.json"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("candidate directory files=%v want=%v", names, want)
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	validatorImports := []string{
		"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "errors", "fmt", "io", "os", "path/filepath", "reflect", "regexp", "runtime", "sort", "strconv", "strings", "testing", "unicode/utf8",
	}
	guardImports := []string{
		"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "errors", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "runtime", "sort", "strconv", "strings", "testing", "unicode/utf8",
	}
	return []sourceSpecV1{
		{Path: validatorPathV1, PackageName: "w2r00d09candidate_test", Imports: validatorImports},
		{Path: guardPathV1, PackageName: "w2r00d09candidateguard_test", Imports: guardImports},
	}
}

func verifySourcePackageV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	path := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	directory := filepath.Dir(path)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("validator package 含非法 entry: %s", entry.Name())
		}
		files = append(files, entry.Name())
	}
	if !reflect.DeepEqual(files, []string{filepath.Base(path)}) {
		t.Fatalf("validator package %s files=%v", directory, files)
	}
	verifyRegularFileV1(t, repoRoot, spec.Path)
	raw := readFileV1(t, repoRoot, spec.Path)
	for _, directive := range [][]byte{[]byte("//" + "go:build"), []byte("//" + " +build"), []byte("//" + "go:embed")} {
		if bytes.Contains(raw, directive) {
			t.Fatalf("%s 不得使用 directive %q", spec.Path, directive)
		}
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("package=%s want=%s", parsed.Name.Name, spec.PackageName)
	}
	var imports []string
	for _, item := range parsed.Imports {
		if item.Name != nil {
			t.Fatalf("%s 不得使用 import alias/dot/blank: %s", spec.Path, item.Name.Name)
		}
		value, unquoteErr := strconv.Unquote(item.Path.Value)
		if unquoteErr != nil {
			t.Fatal(unquoteErr)
		}
		imports = append(imports, value)
	}
	sort.Strings(imports)
	wantImports := append([]string(nil), spec.Imports...)
	sort.Strings(wantImports)
	if !reflect.DeepEqual(imports, wantImports) {
		t.Fatalf("%s imports=%v want=%v", spec.Path, imports, wantImports)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("%s 不得声明 %s", spec.Path, function.Name.Name)
		}
	}
}

func validateJSONShapeV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkJSONValueV1(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func walkJSONValueV1(decoder *json.Decoder) error {
	tokenValue, err := decoder.Token()
	if err != nil {
		return err
	}
	switch value := tokenValue.(type) {
	case nil:
		return errors.New("null not allowed")
	case json.Number:
		if strings.ContainsAny(value.String(), ".eE") {
			return errors.New("non-integer number")
		}
		number, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return err
		}
		if number < -maxSafeIntegerV1 || number > maxSafeIntegerV1 {
			return errors.New("integer outside JSON safe range")
		}
	case json.Delim:
		switch value {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return keyErr
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate key %s", key)
				}
				seen[key] = struct{}{}
				if err := walkJSONValueV1(decoder); err != nil {
					return err
				}
			}
			end, endErr := decoder.Token()
			if endErr != nil || end != json.Delim('}') {
				return errors.New("invalid object end")
			}
		case '[':
			for decoder.More() {
				if err := walkJSONValueV1(decoder); err != nil {
					return err
				}
			}
			end, endErr := decoder.Token()
			if endErr != nil || end != json.Delim(']') {
				return errors.New("invalid array end")
			}
		default:
			return errors.New("unexpected closing delimiter")
		}
	}
	return nil
}

func requireEOFV1(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func readFileV1(t *testing.T, repoRoot, relativePath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifyRegularFileV1(t *testing.T, repoRoot, relativePath string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("artifact %s mode=%v want regular 0644", relativePath, info.Mode())
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../"))
	if _, err := os.Stat(filepath.Join(root, "agent", "go.mod")); err != nil {
		t.Fatalf("repo root invalid: %v", err)
	}
	return root
}
