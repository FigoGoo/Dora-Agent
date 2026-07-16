// Package w2r01_test 承载尚在评审期的 W2-R01 跨语言契约校验器，不提供生产 Runtime。
package w2r01_test

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
	pathpkg "path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	graphToolResultSchemaVersionV1       = "graph_tool_result.v1"
	graphToolResultDigestDomainV1        = "dora.graph_tool_result.v1"
	corpusDataDirectoryV1                = "../testdata/w2_r01"
	corpusManifestPath                   = "../testdata/w2_r01/manifest.json"
	resultCorpusPath                     = "../testdata/w2_r01/graph_tool_result_v1.json"
	maxSafeIntegerV1               int64 = 9_007_199_254_740_991
)

var (
	upperCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
	snakeKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	digestPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type contractError struct {
	code string
	path string
}

type corpusManifestV1 struct {
	SchemaVersion         string                   `json:"schema_version"`
	Files                 []corpusManifestFileV1   `json:"files"`
	DesignSources         []corpusManifestSourceV1 `json:"design_sources"`
	ValidatorSources      []corpusManifestSourceV1 `json:"validator_sources"`
	ValidatorBuildSources []corpusManifestSourceV1 `json:"validator_build_sources"`
	FixtureIDs            []string                 `json:"fixture_ids"`
	VectorIDs             []string                 `json:"vector_ids"`
	TotalVectorCount      int                      `json:"total_vector_count"`
	TargetTests           []string                 `json:"target_tests"`
}

type corpusManifestFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type corpusManifestSourceV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func (e *contractError) Error() string { return e.code + ": " + e.path }

func reject(code, path string) error { return &contractError{code: code, path: path} }

func errorCode(err error) string {
	var target *contractError
	if errors.As(err, &target) {
		return target.code
	}
	return "INTERNAL_TEST_ERROR"
}

type resultCorpusV1 struct {
	SchemaVersion   string                  `json:"schema_version"`
	ResultPolicies  []resultCodePolicyV1    `json:"result_code_policies"`
	WarningPolicies []warningCodePolicyV1   `json:"warning_code_policies"`
	ResourceTypes   []string                `json:"resource_types"`
	Cases           []graphToolResultCaseV1 `json:"cases"`
}

type resultCodePolicyV1 struct {
	ResultCode        string `json:"result_code"`
	Status            string `json:"status"`
	Retryable         bool   `json:"retryable"`
	EffectClass       string `json:"effect_class"`
	CancellationStage string `json:"cancellation_stage,omitempty"`
}

type warningCodePolicyV1 struct {
	Code              string                 `json:"code"`
	Params            []warningParamPolicyV1 `json:"params"`
	RequireTargetRefs bool                   `json:"require_target_refs"`
	AllowedStatuses   []string               `json:"allowed_statuses"`
	EffectClass       string                 `json:"effect_class"`
}

type warningParamPolicyV1 struct {
	Key            string `json:"key"`
	Type           string `json:"type"`
	Required       bool   `json:"required"`
	MinInteger     *int64 `json:"min_integer,omitempty"`
	MaxInteger     *int64 `json:"max_integer,omitempty"`
	MaxStringRunes *int   `json:"max_string_runes,omitempty"`
}

type graphToolResultCaseV1 struct {
	ID                   string          `json:"id"`
	Expect               string          `json:"expect"`
	ErrorCode            string          `json:"error_code"`
	ExpectedResultDigest string          `json:"expected_result_digest"`
	Payload              json.RawMessage `json:"payload"`
	RawJSON              string          `json:"raw_json"`
}

type graphToolResultWireV1 struct {
	SchemaVersion     string          `json:"schema_version"`
	Status            string          `json:"status"`
	ResultCode        string          `json:"result_code"`
	Summary           *string         `json:"summary"`
	ResourceRefs      []resourceRefV1 `json:"resource_refs"`
	ApprovalRef       *approvalRefV1  `json:"approval_ref,omitempty"`
	OperationRef      *operationRefV1 `json:"operation_ref,omitempty"`
	ReceiptRef        *receiptRefV1   `json:"receipt_ref"`
	Warnings          []warningV1     `json:"warnings"`
	CancellationStage *string         `json:"cancellation_stage,omitempty"`
	Retryable         *bool           `json:"retryable"`
}

type graphToolResultV1 struct {
	SchemaVersion     string          `json:"schema_version"`
	Status            string          `json:"status"`
	ResultCode        string          `json:"result_code"`
	Summary           string          `json:"summary"`
	ResourceRefs      []resourceRefV1 `json:"resource_refs"`
	ApprovalRef       *approvalRefV1  `json:"approval_ref,omitempty"`
	OperationRef      *operationRefV1 `json:"operation_ref,omitempty"`
	ReceiptRef        receiptRefV1    `json:"receipt_ref"`
	Warnings          []warningV1     `json:"warnings"`
	CancellationStage *string         `json:"cancellation_stage,omitempty"`
	Retryable         bool            `json:"retryable"`
}

type resourceRefV1 struct {
	ResourceType    string `json:"resource_type"`
	ResourceID      string `json:"resource_id"`
	ResourceVersion int64  `json:"resource_version"`
	ContentDigest   string `json:"content_digest"`
}

type approvalRefV1 struct {
	ApprovalID      string `json:"approval_id"`
	ApprovalVersion int64  `json:"approval_version"`
	ApprovalDigest  string `json:"approval_digest"`
	CardID          string `json:"card_id"`
}

type operationRefV1 struct {
	OperationID      string `json:"operation_id"`
	OperationVersion int64  `json:"operation_version"`
	OperationDigest  string `json:"operation_digest"`
	BatchID          string `json:"batch_id"`
	BatchVersion     int64  `json:"batch_version"`
	BatchDigest      string `json:"batch_digest"`
}

type receiptRefV1 struct {
	ReceiptID              string  `json:"receipt_id"`
	DispatchReceiptID      *string `json:"dispatch_receipt_id,omitempty"`
	DispatchReceiptVersion *int64  `json:"dispatch_receipt_version,omitempty"`
	DispatchReceiptDigest  *string `json:"dispatch_receipt_digest,omitempty"`
}

type warningV1 struct {
	Code       string           `json:"code"`
	Params     []warningParamV1 `json:"params"`
	TargetRefs []targetRefV1    `json:"target_refs"`
}

type warningParamV1 struct {
	Key          string  `json:"key"`
	StringValue  *string `json:"string_value,omitempty"`
	IntegerValue *int64  `json:"integer_value,omitempty"`
	BooleanValue *bool   `json:"boolean_value,omitempty"`
}

type targetRefV1 struct {
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	TargetVersion int64  `json:"target_version"`
	InputDigest   string `json:"input_digest"`
}

type resultPolicySetV1 struct {
	resultCodes   map[string]resultCodePolicyV1
	warningCodes  map[string]warningCodePolicyV1
	resourceTypes map[string]struct{}
}

func runW2R01CorpusManifestV1(t *testing.T) {
	entries, err := os.ReadDir(corpusDataDirectoryV1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Name() != "graph_tool_result_v1.json" || entries[1].Name() != "manifest.json" || entries[2].Name() != "tool_receipt_v1.json" {
		t.Fatalf("Corpus 出现未登记文件或文件缺失: %v", entries)
	}
	raw, err := os.ReadFile(corpusManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("manifest JSON 非法: %v", err)
	}
	var manifest corpusManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatalf("解析 manifest: %v", err)
	}
	if manifest.SchemaVersion != "w2_r01_contract_corpus_manifest.v1" || len(manifest.Files) != 2 || manifest.TotalVectorCount != 87 {
		t.Fatalf("manifest 版本或文件数错误: %+v", manifest)
	}
	// 设计与校验器源码属于候选语义的一部分；固定 exact-set 和 raw digest，避免只改文档或测试实现却沿用旧 Corpus 审核结论。
	repositoryRoot := contractManifestRepositoryRootV1(t)
	wantDesignSources := []string{
		"docs/design/agent/graph-tool-result-receipt-contract-v1.md",
		"docs/design/agent/runner-session-lane-review-v1.md",
		"docs/design/cross-module/aigc-contract-catalog.md",
	}
	wantValidatorSources := contractPackageValidatorSourcePathsV1()
	wantValidatorBuildSources := []string{
		"agent/go.mod",
		"agent/go.sum",
	}
	if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "build", manifest.ValidatorBuildSources, wantValidatorBuildSources); err != nil {
		t.Fatalf("manifest validator_build_sources 未闭合: %v", err)
	}
	if err := validateCorpusManifestGoBuildInputsV1(repositoryRoot, manifest.ValidatorBuildSources); err != nil {
		t.Fatalf("manifest Go build inputs 非法: %v", err)
	}
	if err := validateCorpusManifestGoPackageExactSetV1(repositoryRoot, manifest.ValidatorSources); err != nil {
		t.Fatalf("manifest validator package source exact-set 未闭合: %v", err)
	}
	/*
		目标 Test 仍只来自以下两份语义校验器；完整 validator_sources 额外冻结同包所有参与编译的 Go 文件，
		防止未登记 TestMain/init/共享全局改变进程行为。
	*/
	targetValidatorSources := []string{
		"agent/tests/contract/w2r01/graph_tool_result_v1_corpus_test.go",
		"agent/tests/contract/w2r01/tool_receipt_v1_corpus_test.go",
	}
	if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "design", manifest.DesignSources, wantDesignSources); err != nil {
		t.Fatalf("manifest design_sources 未闭合: %v", err)
	}
	if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "validator", manifest.ValidatorSources, wantValidatorSources); err != nil {
		t.Fatalf("manifest validator_sources 未闭合: %v", err)
	}
	if err := validateCorpusManifestDecoderSourcesV1(repositoryRoot, manifest.ValidatorSources); err != nil {
		t.Fatalf("manifest 未绑定实际共享严格解码器源码: %v", err)
	}
	assertCorpusManifestSourceClosureRejectsV1(t, repositoryRoot, manifest.DesignSources, wantDesignSources, manifest.ValidatorSources, wantValidatorSources)
	actualTests := contractManifestTargetTestNamesV1(t, []string{
		filepath.Base(targetValidatorSources[0]), filepath.Base(targetValidatorSources[1]),
	})
	wantFiles := []corpusManifestFileV1{
		{File: "graph_tool_result_v1.json", VectorCount: 48},
		{File: "tool_receipt_v1.json", VectorCount: 39},
	}
	for index, item := range manifest.Files {
		if item.File != wantFiles[index].File || item.VectorCount != wantFiles[index].VectorCount || !digestPattern.MatchString(item.SHA256) {
			t.Fatalf("manifest 文件顺序或摘要格式错误: %+v", item)
		}
		content, readErr := os.ReadFile(filepath.Join(corpusDataDirectoryV1, item.File))
		if readErr != nil {
			t.Fatal(readErr)
		}
		actual := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(actual[:]); got != item.SHA256 {
			t.Fatalf("%s raw digest=%s want=%s", item.File, got, item.SHA256)
		}
	}
	resultCorpus := loadResultCorpus(t)
	receiptCorpus := loadReceiptCorpus(t)
	if got := len(resultCorpus.Cases); got != manifest.Files[0].VectorCount {
		t.Fatalf("%s vector_count=%d want=%d", manifest.Files[0].File, manifest.Files[0].VectorCount, got)
	}
	if got := len(receiptCorpus.TransitionCases) + len(receiptCorpus.EvidenceCases); got != manifest.Files[1].VectorCount {
		t.Fatalf("%s vector_count=%d want=%d", manifest.Files[1].File, manifest.Files[1].VectorCount, got)
	}
	fixtureIDs := []string{receiptCorpus.InitialState.StateID}
	vectorIDs := make([]string, 0, len(resultCorpus.Cases)+len(receiptCorpus.TransitionCases)+len(receiptCorpus.EvidenceCases))
	for _, testCase := range resultCorpus.Cases {
		vectorIDs = append(vectorIDs, testCase.ID)
	}
	for _, testCase := range receiptCorpus.TransitionCases {
		vectorIDs = append(vectorIDs, testCase.ID)
	}
	for _, testCase := range receiptCorpus.EvidenceCases {
		vectorIDs = append(vectorIDs, testCase.ID)
	}
	if len(vectorIDs) != manifest.TotalVectorCount || !reflect.DeepEqual(manifest.FixtureIDs, fixtureIDs) || !reflect.DeepEqual(manifest.VectorIDs, vectorIDs) {
		t.Fatalf("manifest 未绑定 Corpus exact-set fixtures=%v vectors=%v", manifest.FixtureIDs, manifest.VectorIDs)
	}
	wantTests := []string{
		"TestW2R01CorpusManifest", "TestGraphToolResultV1Corpus", "TestWarningIntegerPolicySafeBoundaryV1",
		"TestToolReceiptV1Corpus",
	}
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("manifest target tests=%v want=%v", manifest.TargetTests, wantTests)
	}
	manifestTests := append([]string(nil), manifest.TargetTests...)
	sort.Strings(manifestTests)
	if !reflect.DeepEqual(actualTests, manifestTests) {
		t.Fatalf("manifest target tests 未绑定实际 Test 函数 actual=%v manifest=%v", actualTests, manifestTests)
	}
}

func contractPackageValidatorSourcePathsV1() []string {
	return []string{
		"agent/tests/contract/w2r01/approval_continuation_parent_receipt_v1.go",
		"agent/tests/contract/w2r01/graph_tool_result_v1.go",
		"agent/tests/contract/w2r01/graph_tool_result_v1_corpus_test.go",
		"agent/tests/contract/w2r01/tool_receipt_v1.go",
		"agent/tests/contract/w2r01/tool_receipt_v1_corpus_test.go",
		"agent/tests/contract/w2r01/validator_support_v1.go",
	}
}

func contractManifestRepositoryRootV1(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(directory, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(repositoryRoot, "agent", "go.mod")); err != nil {
		t.Fatalf("定位仓库根目录: %v", err)
	}
	return repositoryRoot
}

func validateCorpusManifestSourceClosureV1(repositoryRoot, sourceKind string, sources []corpusManifestSourceV1, wantPaths []string) error {
	if err := validateCorpusManifestSourceSetV1(sourceKind, sources, wantPaths); err != nil {
		return err
	}
	for _, source := range sources {
		fullPath, err := corpusManifestSourceFullPathV1(repositoryRoot, source.Path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", source.Path, err)
		}
		actual := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(actual[:]); got != source.SHA256 {
			return fmt.Errorf("%s raw digest=%s want=%s", source.Path, got, source.SHA256)
		}
	}
	return nil
}

func validateCorpusManifestSourceSetV1(sourceKind string, sources []corpusManifestSourceV1, wantPaths []string) error {
	if len(sources) != len(wantPaths) {
		return fmt.Errorf("%s source count=%d want=%d", sourceKind, len(sources), len(wantPaths))
	}
	for index, source := range sources {
		if err := validateCorpusManifestSourcePathV1(sourceKind, source.Path); err != nil {
			return err
		}
		if !digestPattern.MatchString(source.SHA256) {
			return fmt.Errorf("%s 摘要格式非法: %s", source.Path, source.SHA256)
		}
		if index > 0 && sources[index-1].Path >= source.Path {
			return fmt.Errorf("%s sources 必须按 path 严格升序且唯一", sourceKind)
		}
		if source.Path != wantPaths[index] {
			return fmt.Errorf("%s source[%d]=%s want=%s", sourceKind, index, source.Path, wantPaths[index])
		}
	}
	return nil
}

func validateCorpusManifestSourcePathV1(sourceKind, sourcePath string) error {
	if sourcePath == "" || !utf8.ValidString(sourcePath) || strings.Contains(sourcePath, "\\") || pathpkg.Clean(sourcePath) != sourcePath || strings.HasPrefix(sourcePath, "/") {
		return fmt.Errorf("%s source path 非安全仓库相对路径: %q", sourceKind, sourcePath)
	}
	for _, current := range sourcePath {
		if unicode.IsControl(current) {
			return fmt.Errorf("%s source path 含控制字符: %q", sourceKind, sourcePath)
		}
	}
	switch sourceKind {
	case "design":
		if !strings.HasPrefix(sourcePath, "docs/") {
			return fmt.Errorf("design source 越出 docs/: %s", sourcePath)
		}
	case "validator":
		if !strings.HasPrefix(sourcePath, "agent/tests/contract/") || pathpkg.Ext(sourcePath) != ".go" {
			return fmt.Errorf("validator source 必须是 agent/tests/contract/ 下的 Go 文件: %s", sourcePath)
		}
	case "build":
		if sourcePath != "agent/go.mod" && sourcePath != "agent/go.sum" {
			return fmt.Errorf("validator build source 必须是 agent/go.mod 或 agent/go.sum: %s", sourcePath)
		}
	default:
		return fmt.Errorf("未知 source kind: %s", sourceKind)
	}
	return nil
}

func validateCorpusManifestGoPackageExactSetV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	declaredByDirectory := make(map[string][]string)
	for _, source := range sources {
		directory := pathpkg.Dir(source.Path)
		declaredByDirectory[directory] = append(declaredByDirectory[directory], source.Path)
	}
	for directory, declared := range declaredByDirectory {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, filepath.FromSlash(directory)))
		if err != nil {
			return fmt.Errorf("读取 validator package %s: %w", directory, err)
		}
		actual := make([]string, 0, len(entries))
		for _, entry := range entries {
			if pathpkg.Ext(entry.Name()) != ".go" {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("validator package Go source 不是普通文件: %s/%s", directory, entry.Name())
			}
			actual = append(actual, pathpkg.Join(directory, entry.Name()))
		}
		sort.Strings(actual)
		sort.Strings(declared)
		if !reflect.DeepEqual(actual, declared) {
			return fmt.Errorf("validator package %s sources=%v want=%v", directory, declared, actual)
		}
	}
	return validateCorpusManifestGoSourceExecutionShapeV1(repositoryRoot, sources)
}

func validateCorpusManifestGoSourceExecutionShapeV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	buildConstraintPattern := regexp.MustCompile(`(?m)^//go:build(?:[\t ]|$)|^//[\t ]+\+build(?:[\t ]|$)`)
	for _, source := range sources {
		if err := validateCorpusManifestGoSourceFilenameV1(source.Path); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(source.Path)))
		if err != nil {
			return fmt.Errorf("读取 validator source %s: %w", source.Path, err)
		}
		if buildConstraintPattern.Match(raw) {
			return fmt.Errorf("validator source 禁止 build constraint: %s", source.Path)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), source.Path, raw, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("解析 validator source %s: %w", source.Path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "TestMain" {
				return fmt.Errorf("validator source 禁止 TestMain: %s", source.Path)
			}
		}
	}
	return nil
}

func validateCorpusManifestGoSourceFilenameV1(sourcePath string) error {
	base := pathpkg.Base(sourcePath)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
		return fmt.Errorf("validator source filename 会被 Go 忽略: %s", sourcePath)
	}
	stem := strings.TrimSuffix(base, ".go")
	parts := strings.Split(stem, "_")
	if len(parts) > 0 && parts[len(parts)-1] == "test" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	knownOSArch := map[string]struct{}{
		"386": {}, "aix": {}, "amd64": {}, "amd64p32": {}, "android": {}, "arm": {}, "arm64": {}, "arm64be": {}, "armbe": {},
		"darwin": {}, "dragonfly": {}, "freebsd": {}, "hurd": {}, "illumos": {}, "ios": {}, "js": {}, "linux": {}, "loong64": {},
		"mips": {}, "mips64": {}, "mips64le": {}, "mips64p32": {}, "mips64p32le": {}, "mipsle": {}, "nacl": {}, "netbsd": {},
		"openbsd": {}, "plan9": {}, "ppc": {}, "ppc64": {}, "ppc64le": {}, "riscv": {}, "riscv64": {}, "s390": {}, "s390x": {},
		"solaris": {}, "sparc": {}, "sparc64": {}, "wasip1": {}, "wasm": {}, "windows": {}, "zos": {},
	}
	if _, constrained := knownOSArch[parts[len(parts)-1]]; constrained {
		return fmt.Errorf("validator source filename 禁止 GOOS/GOARCH build constraint: %s", sourcePath)
	}
	return nil
}

func validateCorpusManifestGoBuildInputsV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	for _, source := range sources {
		if source.Path != "agent/go.mod" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(source.Path)))
		if err != nil {
			return err
		}
		if regexp.MustCompile(`(?m)^\s*replace(?:\s|\()`).Match(raw) {
			return fmt.Errorf("validator go.mod 禁止未闭合 replace")
		}
	}
	return nil
}

func corpusManifestSourceFullPathV1(repositoryRoot, sourcePath string) (string, error) {
	fullPath := filepath.Join(repositoryRoot, filepath.FromSlash(sourcePath))
	relative, err := filepath.Rel(repositoryRoot, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path 越出仓库: %s", sourcePath)
	}
	info, err := os.Lstat(fullPath)
	if err != nil {
		return "", fmt.Errorf("检查 %s: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source 不是普通文件: %s", sourcePath)
	}
	return fullPath, nil
}

func validateCorpusManifestDecoderSourcesV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	wantOwners := []struct {
		functionName string
		sourcePath   string
	}{
		{functionName: "inspectJSON", sourcePath: "agent/tests/contract/w2r01/validator_support_v1.go"},
		{functionName: "strictDecode", sourcePath: "agent/tests/contract/w2r01/validator_support_v1.go"},
		{functionName: "validateJSONUnicodeEscapes", sourcePath: "agent/tests/contract/w2r01/validator_support_v1.go"},
	}
	found := make(map[string]string, len(wantOwners))
	required := make(map[string]struct{}, len(wantOwners))
	for _, owner := range wantOwners {
		required[owner.functionName] = struct{}{}
	}
	for _, source := range sources {
		fullPath, err := corpusManifestSourceFullPathV1(repositoryRoot, source.Path)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), fullPath, nil, 0)
		if err != nil {
			return fmt.Errorf("解析 validator source %s: %w", source.Path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			if _, isRequired := required[function.Name.Name]; isRequired {
				found[function.Name.Name] = source.Path
			}
		}
	}
	for _, owner := range wantOwners {
		if found[owner.functionName] != owner.sourcePath {
			return fmt.Errorf("%s owner=%s want=%s", owner.functionName, found[owner.functionName], owner.sourcePath)
		}
	}
	return nil
}

func assertCorpusManifestSourceClosureRejectsV1(t *testing.T, repositoryRoot string, designSources []corpusManifestSourceV1, wantDesignPaths []string, validatorSources []corpusManifestSourceV1, wantValidatorPaths []string) {
	t.Helper()
	clone := func(sources []corpusManifestSourceV1) []corpusManifestSourceV1 {
		return append([]corpusManifestSourceV1(nil), sources...)
	}
	t.Run("source_closure_rejects_missing_unknown_and_digest_drift", func(t *testing.T) {
		missing := clone(designSources[:len(designSources)-1])
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "design", missing, wantDesignPaths); err == nil {
			t.Fatal("缺失 design source 必须失败关闭")
		}
		unknown := clone(designSources)
		unknown[len(unknown)-1].Path = "docs/design/unknown-r01-source.md"
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "design", unknown, wantDesignPaths); err == nil {
			t.Fatal("未知 design source 必须失败关闭")
		}
		drifted := clone(validatorSources)
		drifted[0].SHA256 = "sha256:" + strings.Repeat("0", 64)
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "validator", drifted, wantValidatorPaths); err == nil {
			t.Fatal("validator source 摘要漂移必须失败关闭")
		}
		unsafe := clone(validatorSources)
		unsafe[0].Path = "agent/tests/contract/../contract/graph_tool_result_v1_corpus_test.go"
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "validator", unsafe, wantValidatorPaths); err == nil {
			t.Fatal("非规范化 validator source 路径必须失败关闭")
		}
		duplicate := clone(validatorSources)
		duplicate[1] = duplicate[0]
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "validator", duplicate, wantValidatorPaths); err == nil {
			t.Fatal("重复 validator source 必须失败关闭")
		}
		unsorted := clone(designSources)
		unsorted[0], unsorted[1] = unsorted[1], unsorted[0]
		if err := validateCorpusManifestSourceClosureV1(repositoryRoot, "design", unsorted, wantDesignPaths); err == nil {
			t.Fatal("未排序 design sources 必须失败关闭")
		}
	})
}

func contractManifestTargetTestNamesV1(t *testing.T, files []string) []string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0)
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			result = append(result, function.Name.Name)
		}
	}
	sort.Strings(result)
	return result
}

func runGraphToolResultV1Corpus(t *testing.T) {
	corpus := loadResultCorpus(t)
	policies := buildResultPolicies(t, corpus)
	seenIDs := make(map[string]struct{}, len(corpus.Cases))
	acceptedStatuses := make(map[string]struct{}, 6)
	wantCaseIDs := []string{
		"GTR-P01-completed", "GTR-P02-accepted", "GTR-P03-waiting-user", "GTR-P04-partial",
		"GTR-P05-failed", "GTR-P06-cancelled", "GTR-P07-unicode-canonical", "GTR-P08-cancelled-after-side-effects",
		"GTR-N01-malformed", "GTR-N02-duplicate-top-level", "GTR-N03-duplicate-nested", "GTR-N04-trailing-value",
		"GTR-N05-null", "GTR-N06-unknown-field", "GTR-N07-unknown-version", "GTR-N08-missing-summary",
		"GTR-N09-unknown-status", "GTR-N10-unregistered-code", "GTR-N11-forbidden-internal-outcome",
		"GTR-N12-retryable-policy-mismatch", "GTR-N13-accepted-missing-operation", "GTR-N14-accepted-missing-dispatch",
		"GTR-N15-waiting-missing-approval", "GTR-N16-waiting-with-operation", "GTR-N17-partial-without-success",
		"GTR-N18-partial-without-warning", "GTR-N19-failed-with-resource", "GTR-N20-cancelled-without-stage",
		"GTR-N21-missing-receipt", "GTR-N22-invalid-resource-id", "GTR-N23-invalid-digest",
		"GTR-N24-unsorted-resources", "GTR-N25-duplicate-resource", "GTR-N26-warning-param-shape",
		"GTR-N27-null-array", "GTR-N28-warning-param-extra", "GTR-N29-invalid-uuid-variant",
		"GTR-N30-unpaired-surrogate", "GTR-N31-unknown-field-precedes-version", "GTR-N32-missing-precedes-version",
		"GTR-N33-forbidden-code-precedes-summary", "GTR-N34-warning-integer-overflow",
		"GTR-N35-warning-js-safe-integer-overflow", "GTR-N36-null-precedes-duplicate", "GTR-N37-duplicate-precedes-null",
		"GTR-N38-unknown-precedes-type", "GTR-N39-type-precedes-unknown", "GTR-N40-cancellation-code-stage-mismatch",
	}
	gotCaseIDs := make([]string, 0, len(corpus.Cases))

	for _, fixture := range corpus.Cases {
		fixture := fixture
		gotCaseIDs = append(gotCaseIDs, fixture.ID)
		t.Run(fixture.ID, func(t *testing.T) {
			if _, exists := seenIDs[fixture.ID]; exists {
				t.Fatalf("重复 case id %q", fixture.ID)
			}
			seenIDs[fixture.ID] = struct{}{}
			raw := fixture.Payload
			if fixture.RawJSON != "" {
				if len(fixture.Payload) != 0 {
					t.Fatal("payload 与 raw_json 不能同时出现")
				}
				raw = []byte(fixture.RawJSON)
			}
			result, _, digest, err := decodeGraphToolResultV1(raw, policies)
			if fixture.Expect == "reject" {
				if err == nil {
					t.Fatalf("期望拒绝，实际接受 digest=%s", digest)
				}
				if got := errorCode(err); got != fixture.ErrorCode {
					t.Fatalf("拒绝码=%s want=%s err=%v", got, fixture.ErrorCode, err)
				}
				return
			}
			if fixture.Expect != "accept" || fixture.ErrorCode != "" || err != nil {
				t.Fatalf("合法 case 元数据或结果错误: expect=%q code=%q err=%v", fixture.Expect, fixture.ErrorCode, err)
			}
			if fixture.ExpectedResultDigest == "" || digest != fixture.ExpectedResultDigest {
				t.Fatalf("result digest=%s want=%s", digest, fixture.ExpectedResultDigest)
			}
			acceptedStatuses[result.Status] = struct{}{}
		})
	}
	if strings.Join(gotCaseIDs, "\n") != strings.Join(wantCaseIDs, "\n") {
		t.Fatalf("case 集合或顺序变化: got=%v want=%v", gotCaseIDs, wantCaseIDs)
	}

	wantStatuses := []string{"accepted", "cancelled", "completed", "failed", "partial", "waiting_user"}
	gotStatuses := make([]string, 0, len(acceptedStatuses))
	for status := range acceptedStatuses {
		gotStatuses = append(gotStatuses, status)
	}
	sort.Strings(gotStatuses)
	if strings.Join(gotStatuses, ",") != strings.Join(wantStatuses, ",") {
		t.Fatalf("合法状态 fixed vectors=%v want=%v", gotStatuses, wantStatuses)
	}
}

func runWarningIntegerPolicySafeBoundaryV1(t *testing.T) {
	minimum := int64(1)
	unsafeMaximum := maxSafeIntegerV1 + 1
	policy := warningParamPolicyV1{Key: "count", Type: "integer", Required: true, MinInteger: &minimum, MaxInteger: &unsafeMaximum}
	if validWarningParamPolicyV1(policy) {
		t.Fatalf("Warning integer policy 不得越过 JS safe integer: %+v", policy)
	}
}

func loadResultCorpus(t *testing.T) resultCorpusV1 {
	t.Helper()
	raw, err := os.ReadFile(resultCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("corpus JSON 非法: %v", err)
	}
	var corpus resultCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatalf("解析 corpus: %v", err)
	}
	if corpus.SchemaVersion != "graph_tool_result_v1_corpus.v1" || len(corpus.Cases) < 20 {
		t.Fatalf("corpus 版本或覆盖不足: version=%q cases=%d", corpus.SchemaVersion, len(corpus.Cases))
	}
	return corpus
}

func buildResultPolicies(t *testing.T, corpus resultCorpusV1) resultPolicySetV1 {
	t.Helper()
	policies := resultPolicySetV1{
		resultCodes:   make(map[string]resultCodePolicyV1, len(corpus.ResultPolicies)),
		warningCodes:  make(map[string]warningCodePolicyV1, len(corpus.WarningPolicies)),
		resourceTypes: make(map[string]struct{}, len(corpus.ResourceTypes)),
	}
	lastResultCode := ""
	for index, policy := range corpus.ResultPolicies {
		if !upperCodePattern.MatchString(policy.ResultCode) || !validResultStatus(policy.Status) || !validResultEffectPolicy(policy) || isForbiddenInternalOutcome(policy.ResultCode) {
			t.Fatalf("非法 result code policy: %+v", policy)
		}
		if index > 0 && policy.ResultCode <= lastResultCode {
			t.Fatalf("result code policy 非规范顺序: %s", policy.ResultCode)
		}
		lastResultCode = policy.ResultCode
		if _, exists := policies.resultCodes[policy.ResultCode]; exists {
			t.Fatalf("重复 result code policy: %s", policy.ResultCode)
		}
		policies.resultCodes[policy.ResultCode] = policy
	}
	lastWarningCode := ""
	for index, policy := range corpus.WarningPolicies {
		if !upperCodePattern.MatchString(policy.Code) || (policy.EffectClass != "informational" && policy.EffectClass != "failed_target") {
			t.Fatalf("非法 warning policy: %+v", policy)
		}
		if index > 0 && policy.Code <= lastWarningCode {
			t.Fatalf("warning policy 非规范顺序: %s", policy.Code)
		}
		lastWarningCode = policy.Code
		if _, exists := policies.warningCodes[policy.Code]; exists {
			t.Fatalf("重复 warning policy: %s", policy.Code)
		}
		seenParams := make(map[string]struct{}, len(policy.Params))
		lastParamKey := ""
		for index, param := range policy.Params {
			if !snakeKeyPattern.MatchString(param.Key) || (param.Type != "string" && param.Type != "integer" && param.Type != "boolean") {
				t.Fatalf("非法 warning param policy: %+v", param)
			}
			if !validWarningParamPolicyV1(param) {
				t.Fatalf("warning param policy 边界非法: %+v", param)
			}
			if index > 0 && param.Key <= lastParamKey {
				t.Fatalf("warning param policy 非规范顺序: %s/%s", policy.Code, param.Key)
			}
			lastParamKey = param.Key
			if _, exists := seenParams[param.Key]; exists {
				t.Fatalf("重复 warning param policy: %s/%s", policy.Code, param.Key)
			}
			seenParams[param.Key] = struct{}{}
		}
		if len(policy.AllowedStatuses) == 0 {
			t.Fatalf("warning policy 缺少 allowed_statuses: %s", policy.Code)
		}
		lastStatus := ""
		for index, status := range policy.AllowedStatuses {
			if !validResultStatus(status) || (index > 0 && status <= lastStatus) {
				t.Fatalf("warning allowed_statuses 非法: %s/%v", policy.Code, policy.AllowedStatuses)
			}
			lastStatus = status
		}
		policies.warningCodes[policy.Code] = policy
	}
	lastResourceType := ""
	for index, resourceType := range corpus.ResourceTypes {
		if !snakeKeyPattern.MatchString(resourceType) {
			t.Fatalf("非法 resource type: %s", resourceType)
		}
		if index > 0 && resourceType <= lastResourceType {
			t.Fatalf("resource type 非规范顺序: %s", resourceType)
		}
		lastResourceType = resourceType
		if _, exists := policies.resourceTypes[resourceType]; exists {
			t.Fatalf("重复 resource type: %s", resourceType)
		}
		policies.resourceTypes[resourceType] = struct{}{}
	}
	return policies
}

func decodeGraphToolResultV1(raw []byte, policies resultPolicySetV1) (graphToolResultV1, []byte, string, error) {
	if len(raw) == 0 || len(raw) > 64*1024 {
		return graphToolResultV1{}, nil, "", reject("LIMIT_EXCEEDED", "result bytes")
	}
	if err := inspectJSON(raw); err != nil {
		return graphToolResultV1{}, nil, "", err
	}
	var wire graphToolResultWireV1
	if err := strictDecode(raw, &wire); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return graphToolResultV1{}, nil, "", reject("UNKNOWN_FIELD", "result")
		}
		if strings.Contains(err.Error(), "cannot unmarshal") {
			return graphToolResultV1{}, nil, "", reject("TYPE_MISMATCH", "result")
		}
		return graphToolResultV1{}, nil, "", reject("INVALID_JSON", "result")
	}
	if wire.SchemaVersion == "" || wire.Status == "" || wire.ResultCode == "" || wire.Summary == nil || wire.ResourceRefs == nil || wire.ReceiptRef == nil || wire.Warnings == nil || wire.Retryable == nil {
		return graphToolResultV1{}, nil, "", reject("MISSING_REQUIRED_FIELD", "result")
	}
	result := graphToolResultV1{
		SchemaVersion: wire.SchemaVersion, Status: wire.Status, ResultCode: wire.ResultCode,
		Summary: *wire.Summary, ResourceRefs: wire.ResourceRefs, ApprovalRef: wire.ApprovalRef,
		OperationRef: wire.OperationRef, ReceiptRef: *wire.ReceiptRef, Warnings: wire.Warnings,
		CancellationStage: wire.CancellationStage, Retryable: *wire.Retryable,
	}
	if err := validateGraphToolResultV1(result, policies); err != nil {
		return graphToolResultV1{}, nil, "", err
	}
	canonical, err := canonicalJSON(result)
	if err != nil {
		return graphToolResultV1{}, nil, "", reject("INVALID_JSON", "canonical result")
	}
	return result, canonical, semanticDigest(graphToolResultDigestDomainV1, canonical), nil
}

func validateGraphToolResultV1(result graphToolResultV1, policies resultPolicySetV1) error {
	if result.SchemaVersion != graphToolResultSchemaVersionV1 {
		return reject("UNKNOWN_VERSION", "schema_version")
	}
	if !validResultStatus(result.Status) {
		return reject("UNKNOWN_ENUM", "status")
	}
	if !upperCodePattern.MatchString(result.ResultCode) {
		return reject("INVALID_VALUE", "result_code")
	}
	if isForbiddenInternalOutcome(result.ResultCode) {
		return reject("FORBIDDEN_INTERNAL_OUTCOME", "result_code")
	}
	policy, exists := policies.resultCodes[result.ResultCode]
	if !exists || policy.Status != result.Status || policy.Retryable != result.Retryable {
		return reject("RESULT_CODE_POLICY_MISMATCH", "result_code/status/retryable")
	}
	if err := validateDisplayText(result.Summary, 280); err != nil {
		return err
	}
	if len(result.ResourceRefs) > 32 || len(result.Warnings) > 32 {
		return reject("LIMIT_EXCEEDED", "result lists")
	}
	if err := validateResourceRefs(result.ResourceRefs, policies.resourceTypes); err != nil {
		return err
	}
	if err := validateWarnings(result.Status, result.Warnings, policies.warningCodes); err != nil {
		return err
	}
	if err := validateReceiptRef(result.ReceiptRef, result.Status); err != nil {
		return err
	}

	switch result.Status {
	case "completed":
		if result.ApprovalRef != nil || result.OperationRef != nil || result.CancellationStage != nil || result.Retryable {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "completed")
		}
	case "accepted":
		if result.ApprovalRef != nil || result.OperationRef == nil || result.CancellationStage != nil || result.Retryable || len(result.ResourceRefs) != 0 {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "accepted")
		}
		if err := validateOperationRef(*result.OperationRef); err != nil {
			return err
		}
	case "waiting_user":
		if result.ApprovalRef == nil || result.OperationRef != nil || result.CancellationStage != nil || result.Retryable {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "waiting_user")
		}
		if err := validateApprovalRef(*result.ApprovalRef); err != nil {
			return err
		}
	case "partial":
		if len(result.ResourceRefs) == 0 || len(result.Warnings) == 0 || result.ApprovalRef != nil || result.OperationRef != nil || result.CancellationStage != nil || result.Retryable || !hasFailedTarget(result.Warnings, policies.warningCodes) {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "partial")
		}
	case "failed":
		if len(result.ResourceRefs) != 0 || result.ApprovalRef != nil || result.OperationRef != nil || result.CancellationStage != nil {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "failed")
		}
	case "cancelled":
		if len(result.ResourceRefs) != 0 || result.ApprovalRef != nil || result.OperationRef != nil || result.CancellationStage == nil || result.Retryable {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "cancelled")
		}
		if *result.CancellationStage != "before_side_effect" && *result.CancellationStage != "after_side_effects_resolved" {
			return reject("UNKNOWN_ENUM", "cancellation_stage")
		}
		if policy.CancellationStage != *result.CancellationStage {
			return reject("RESULT_CODE_POLICY_MISMATCH", "result_code/cancellation_stage")
		}
	}
	return nil
}

func validResultStatus(status string) bool {
	switch status {
	case "completed", "accepted", "waiting_user", "partial", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validResultEffectPolicy(policy resultCodePolicyV1) bool {
	switch policy.Status {
	case "completed":
		return policy.EffectClass == "bounded_success" && !policy.Retryable && policy.CancellationStage == ""
	case "accepted":
		return policy.EffectClass == "async_dispatch" && !policy.Retryable && policy.CancellationStage == ""
	case "waiting_user":
		return policy.EffectClass == "approval_wait" && !policy.Retryable && policy.CancellationStage == ""
	case "partial":
		return policy.EffectClass == "partial_success" && !policy.Retryable && policy.CancellationStage == ""
	case "failed":
		return policy.CancellationStage == "" && ((policy.EffectClass == "permanent_failure" && !policy.Retryable) ||
			(policy.EffectClass == "safe_failure_before_side_effect" && policy.Retryable))
	case "cancelled":
		return !policy.Retryable && ((policy.EffectClass == "cancelled_before_side_effect" && policy.CancellationStage == "before_side_effect") ||
			(policy.EffectClass == "cancelled_after_side_effects_resolved" && policy.CancellationStage == "after_side_effects_resolved"))
	default:
		return false
	}
}

func validWarningParamPolicyV1(param warningParamPolicyV1) bool {
	switch param.Type {
	case "integer":
		return param.MinInteger != nil && param.MaxInteger != nil && safePositiveIntegerV1(*param.MinInteger) &&
			safePositiveIntegerV1(*param.MaxInteger) && *param.MinInteger <= *param.MaxInteger && param.MaxStringRunes == nil
	case "string":
		return param.MinInteger == nil && param.MaxInteger == nil && param.MaxStringRunes != nil &&
			*param.MaxStringRunes >= 1 && *param.MaxStringRunes <= 256
	case "boolean":
		return param.MinInteger == nil && param.MaxInteger == nil && param.MaxStringRunes == nil
	default:
		return false
	}
}

func isForbiddenInternalOutcome(code string) bool {
	switch code {
	case "UNKNOWN_OUTCOME", "STALE_ATTEMPT", "STALE_FENCE", "TOOL_RECEIPT_CONFLICT", "TOOL_EXECUTION_REF_CONFLICT",
		"RECEIPT_VERSION_CONFLICT", "TOOL_RECEIPT_FROZEN", "TOOL_EXECUTION_SLOT_UNRESOLVED", "RESULT_DIGEST_MISMATCH", "RESULT_REF_MISMATCH":
		return true
	default:
		return false
	}
}

func validateResourceRefs(refs []resourceRefV1, allowed map[string]struct{}) error {
	lastKey := ""
	for index, ref := range refs {
		if _, exists := allowed[ref.ResourceType]; !exists || !canonicalUUIDv7(ref.ResourceID) || !safePositiveIntegerV1(ref.ResourceVersion) || !digestPattern.MatchString(ref.ContentDigest) {
			return reject("INVALID_VALUE", fmt.Sprintf("resource_refs[%d]", index))
		}
		key := fmt.Sprintf("%s\x00%s\x00%020d", ref.ResourceType, ref.ResourceID, ref.ResourceVersion)
		if index > 0 && key <= lastKey {
			return reject("NON_CANONICAL_ORDER", "resource_refs")
		}
		lastKey = key
	}
	return nil
}

func validateWarnings(status string, warnings []warningV1, policies map[string]warningCodePolicyV1) error {
	lastCode := ""
	for index, warning := range warnings {
		policy, exists := policies[warning.Code]
		if !exists || warning.Params == nil || warning.TargetRefs == nil {
			return reject("INVALID_VALUE", fmt.Sprintf("warnings[%d]", index))
		}
		if !containsString(policy.AllowedStatuses, status) {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", fmt.Sprintf("warnings[%d] status", index))
		}
		if index > 0 && warning.Code <= lastCode {
			return reject("NON_CANONICAL_ORDER", "warnings")
		}
		lastCode = warning.Code
		if len(warning.Params) > 16 || len(warning.TargetRefs) > 64 || (policy.RequireTargetRefs && len(warning.TargetRefs) == 0) {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", fmt.Sprintf("warnings[%d]", index))
		}
		if err := validateWarningParams(warning.Params, policy); err != nil {
			return err
		}
		if err := validateTargetRefs(warning.TargetRefs); err != nil {
			return err
		}
	}
	return nil
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func validateWarningParams(params []warningParamV1, policy warningCodePolicyV1) error {
	allowed := make(map[string]warningParamPolicyV1, len(policy.Params))
	for _, item := range policy.Params {
		allowed[item.Key] = item
	}
	seen := make(map[string]struct{}, len(params))
	lastKey := ""
	for index, param := range params {
		rule, exists := allowed[param.Key]
		if !exists || !snakeKeyPattern.MatchString(param.Key) || (index > 0 && param.Key <= lastKey) {
			return reject("INVALID_VALUE", "warning params")
		}
		lastKey = param.Key
		if _, duplicate := seen[param.Key]; duplicate {
			return reject("INVALID_VALUE", "warning params duplicate")
		}
		seen[param.Key] = struct{}{}
		valueCount := 0
		actualType := ""
		if param.StringValue != nil {
			valueCount++
			actualType = "string"
			if rule.MaxStringRunes == nil {
				return reject("INVALID_VALUE", "warning string param policy")
			}
			if err := validateDisplayText(*param.StringValue, *rule.MaxStringRunes); err != nil {
				return err
			}
		}
		if param.IntegerValue != nil {
			valueCount++
			actualType = "integer"
			if !safePositiveIntegerV1(*param.IntegerValue) || rule.MinInteger == nil || rule.MaxInteger == nil || *param.IntegerValue < *rule.MinInteger || *param.IntegerValue > *rule.MaxInteger {
				return reject("INVALID_VALUE", "warning integer param range")
			}
		}
		if param.BooleanValue != nil {
			valueCount++
			actualType = "boolean"
		}
		if valueCount != 1 || actualType != rule.Type {
			return reject("INVALID_VALUE", "warning param tagged union")
		}
	}
	for _, rule := range policy.Params {
		if _, exists := seen[rule.Key]; rule.Required && !exists {
			return reject("MISSING_REQUIRED_FIELD", "warning param "+rule.Key)
		}
	}
	return nil
}

func validateTargetRefs(refs []targetRefV1) error {
	lastKey := ""
	for index, ref := range refs {
		if !snakeKeyPattern.MatchString(ref.TargetType) || !canonicalUUIDv7(ref.TargetID) || !safePositiveIntegerV1(ref.TargetVersion) || !digestPattern.MatchString(ref.InputDigest) {
			return reject("INVALID_VALUE", fmt.Sprintf("target_refs[%d]", index))
		}
		key := fmt.Sprintf("%s\x00%s\x00%020d", ref.TargetType, ref.TargetID, ref.TargetVersion)
		if index > 0 && key <= lastKey {
			return reject("NON_CANONICAL_ORDER", "target_refs")
		}
		lastKey = key
	}
	return nil
}

func validateApprovalRef(ref approvalRefV1) error {
	if !canonicalUUIDv7(ref.ApprovalID) || !safePositiveIntegerV1(ref.ApprovalVersion) || !digestPattern.MatchString(ref.ApprovalDigest) || !canonicalUUIDv7(ref.CardID) {
		return reject("INVALID_VALUE", "approval_ref")
	}
	return nil
}

func validateOperationRef(ref operationRefV1) error {
	if !canonicalUUIDv7(ref.OperationID) || !safePositiveIntegerV1(ref.OperationVersion) || !digestPattern.MatchString(ref.OperationDigest) ||
		!canonicalUUIDv7(ref.BatchID) || !safePositiveIntegerV1(ref.BatchVersion) || !digestPattern.MatchString(ref.BatchDigest) {
		return reject("INVALID_VALUE", "operation_ref")
	}
	return nil
}

func validateReceiptRef(ref receiptRefV1, status string) error {
	if !canonicalUUIDv7(ref.ReceiptID) {
		return reject("INVALID_VALUE", "receipt_ref.receipt_id")
	}
	hasDispatch := ref.DispatchReceiptID != nil || ref.DispatchReceiptVersion != nil || ref.DispatchReceiptDigest != nil
	if status == "accepted" {
		if ref.DispatchReceiptID == nil || ref.DispatchReceiptVersion == nil || ref.DispatchReceiptDigest == nil ||
			!canonicalUUIDv7(*ref.DispatchReceiptID) || !safePositiveIntegerV1(*ref.DispatchReceiptVersion) || !digestPattern.MatchString(*ref.DispatchReceiptDigest) {
			return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "accepted dispatch receipt")
		}
	} else if hasDispatch {
		return reject("ILLEGAL_STATUS_FIELD_COMBINATION", "dispatch receipt")
	}
	return nil
}

func hasFailedTarget(warnings []warningV1, policies map[string]warningCodePolicyV1) bool {
	for _, warning := range warnings {
		if policy, exists := policies[warning.Code]; exists && policy.EffectClass == "failed_target" && len(warning.TargetRefs) > 0 {
			return true
		}
	}
	return false
}

func validateDisplayText(value string, maxRunes int) error {
	if value == "" || value != strings.TrimSpace(value) || !norm.NFC.IsNormalString(value) || utf8.RuneCountInString(value) > maxRunes {
		if utf8.RuneCountInString(value) > maxRunes {
			return reject("LIMIT_EXCEEDED", "display text")
		}
		return reject("INVALID_VALUE", "display text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return reject("INVALID_VALUE", "display text control")
		}
	}
	return nil
}

func safePositiveIntegerV1(value int64) bool {
	return value >= 1 && value <= maxSafeIntegerV1
}

func semanticDigest(domain string, canonical []byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(canonical)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func strictDecodeV1(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func inspectJSONV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return reject("INVALID_JSON", "utf-8")
	}
	if err := validateJSONUnicodeEscapesV1(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return reject("TRAILING_VALUE", "result")
	}
	return nil
}

func validateJSONUnicodeEscapesV1(raw []byte) error {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			index++
			if raw[index] != 'u' {
				continue
			}
			value, next, ok := parseHexUTF16Unit(raw, index+1)
			if !ok {
				return reject("INVALID_UNICODE_ESCAPE", "unicode escape")
			}
			index = next - 1
			if value >= 0xD800 && value <= 0xDBFF {
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return reject("INVALID_UNICODE_ESCAPE", "unpaired high surrogate")
				}
				low, afterLow, lowOK := parseHexUTF16Unit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return reject("INVALID_UNICODE_ESCAPE", "invalid surrogate pair")
				}
				index = afterLow - 1
			} else if value >= 0xDC00 && value <= 0xDFFF {
				return reject("INVALID_UNICODE_ESCAPE", "unpaired low surrogate")
			}
		}
	}
	return nil
}

func parseHexUTF16Unit(raw []byte, start int) (uint16, int, bool) {
	if start+4 > len(raw) {
		return 0, start, false
	}
	var value uint16
	for index := start; index < start+4; index++ {
		value <<= 4
		switch current := raw[index]; {
		case current >= '0' && current <= '9':
			value += uint16(current - '0')
		case current >= 'a' && current <= 'f':
			value += uint16(current-'a') + 10
		case current >= 'A' && current <= 'F':
			value += uint16(current-'A') + 10
		default:
			return 0, start, false
		}
	}
	return value, start + 4, true
}

func inspectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return reject("INVALID_JSON", "token")
	}
	if token == nil {
		return reject("NULL_NOT_ALLOWED", "null")
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
				return reject("INVALID_JSON", "object key")
			}
			key, ok := keyToken.(string)
			if !ok {
				return reject("INVALID_JSON", "object key type")
			}
			if _, duplicate := seen[key]; duplicate {
				return reject("DUPLICATE_KEY", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return reject("INVALID_JSON", "object end")
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return reject("INVALID_JSON", "array end")
		}
	default:
		return reject("INVALID_JSON", "unexpected delimiter")
	}
	return nil
}
