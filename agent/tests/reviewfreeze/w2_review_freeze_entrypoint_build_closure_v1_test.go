package reviewfreeze_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	pathpkg "path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

const reviewFreezeValidatorBuildClosureSchemaV1 = "w2_validator_build_closure.v1"

var (
	reviewFreezeGoEmbedDirectiveV1   = regexp.MustCompile(`(?m)^[\t ]*//go:embed(?:[\t ]|$)`)
	reviewFreezeEntrypointIDV1       = regexp.MustCompile(`^W2-R(?:01|04)\.[a-z][a-z0-9_]*$`)
	reviewFreezeModuleVersionV1      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?$`)
	reviewFreezeGoModuleChecksumV1   = regexp.MustCompile(`^h1:[A-Za-z0-9+/]{43}=$`)
	reviewFreezeExternalImportPathV1 = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+/-]*$`)
)

// reviewFreezeValidatorBuildClosureV1 描述独立 Validator entrypoint 的候选构建闭包。
// 该扩展在旧 v1 manifest 中可缺失；一旦出现，所有字段和 exact-set 都必须失败关闭校验。
type reviewFreezeValidatorBuildClosureV1 struct {
	// SchemaVersion 固定独立构建闭包的严格版本。
	SchemaVersion string `json:"schema_version"`
	// ActivationStatus 明确批次一只形成未激活候选，不能据此移除 blocker 或进入 formal 状态。
	ActivationStatus string `json:"activation_status"`
	// Environment 固定 Go 语言、工具链、平台和影响构建选择的环境变量。
	Environment reviewFreezeValidatorBuildEnvironmentV1 `json:"environment"`
	// Entrypoints 按 package_path 排序固定全部独立 verifier package。
	Entrypoints []reviewFreezeValidatorEntrypointV1 `json:"entrypoints"`
}

// reviewFreezeValidatorBuildEnvironmentV1 固定受信 verifier 的构建选择环境。
type reviewFreezeValidatorBuildEnvironmentV1 struct {
	// GoVersion 对齐 go.mod 的 go language version。
	GoVersion string `json:"go_version"`
	// Toolchain 对齐 go.mod 的精确 toolchain directive。
	Toolchain string `json:"toolchain"`
	// GOOS 固定目标操作系统。
	GOOS string `json:"goos"`
	// GOARCH 固定目标架构。
	GOARCH string `json:"goarch"`
	// CGOEnabled 禁止环境依赖的 C toolchain 输入。
	CGOEnabled string `json:"cgo_enabled"`
	// GOWORK 禁止根 go.work 隐式改变 Module 图。
	GOWORK string `json:"gowork"`
	// GOTOOLCHAIN 禁止自动下载或切换未绑定工具链。
	GOTOOLCHAIN string `json:"gotoolchain"`
	// GOFLAGS 固定只读 Module 与可复现路径参数。
	GOFLAGS string `json:"goflags"`
	// GOENV 禁止用户级 Go 环境文件注入构建参数。
	GOENV string `json:"goenv"`
}

// reviewFreezeValidatorEntrypointV1 描述一个可单独执行且不依赖共享 contract_test 包的 verifier package。
type reviewFreezeValidatorEntrypointV1 struct {
	// EntrypointID 是 Gate 与 verifier 语义的稳定标识。
	EntrypointID string `json:"entrypoint_id"`
	// PackageName 固定 Go package declaration。
	PackageName string `json:"package_name"`
	// PackagePath 固定仓库相对 package 目录。
	PackagePath string `json:"package_path"`
	// PackagePattern 固定从所属 Module Root 执行的唯一 go test package 参数。
	PackagePattern string `json:"package_pattern"`
	// DependencyPolicy 区分 stdlib-only 与 R01 受控 NFC 例外。
	DependencyPolicy string `json:"dependency_policy"`
	// TestEntrypoints 固定该 package 可执行的顶层 Test exact-set。
	TestEntrypoints []string `json:"test_entrypoints"`
	// DirectSources 固定 package 直接 Go source exact-set 及原始摘要。
	DirectSources []reviewFreezeValidatorSourceV1 `json:"direct_sources"`
	// AllowedImports 固定直接 source 的 import path exact-set。
	AllowedImports []string `json:"allowed_imports"`
	// ExternalModules 固定非标准库 Module lock 与 build-selected package source exact-set。
	ExternalModules []reviewFreezeValidatorExternalModuleV1 `json:"external_modules"`
}

// reviewFreezeValidatorExternalModuleV1 将外部 Module 版本、go.sum 内容与实际选中 package 源绑定。
type reviewFreezeValidatorExternalModuleV1 struct {
	// ModulePath 是外部 Module 的 canonical path。
	ModulePath string `json:"module_path"`
	// Version 是 go.mod require 的精确版本。
	Version string `json:"version"`
	// ModuleSum 是 go.sum 中 Module zip 的 h1 摘要。
	ModuleSum string `json:"module_sum"`
	// GoModSum 是 go.sum 中 Module go.mod 的 h1 摘要。
	GoModSum string `json:"go_mod_sum"`
	// Packages 按 import_path 排序固定该环境实际选中的外部 package 闭包。
	Packages []reviewFreezeValidatorExternalPackageV1 `json:"packages"`
}

// reviewFreezeValidatorExternalPackageV1 描述外部 Module 中一个 build-selected package。
type reviewFreezeValidatorExternalPackageV1 struct {
	// ImportPath 是该 package 的完整 Go import path。
	ImportPath string `json:"import_path"`
	// Sources 固定受信环境实际选中的直接 Go source exact-set。
	Sources []reviewFreezeValidatorExternalSourceV1 `json:"sources"`
	// AllowedImports 固定这些选中 source 的 import path exact-set。
	AllowedImports []string `json:"allowed_imports"`
}

// reviewFreezeValidatorExternalSourceV1 描述外部 Module 内的 module-relative build-selected source。
type reviewFreezeValidatorExternalSourceV1 struct {
	// Path 是相对 Module Root 的安全 Go source 路径。
	Path string `json:"path"`
	// SHA256 固定 source 原始字节摘要。
	SHA256 string `json:"sha256"`
}

// reviewFreezeExternalPackageResolverV1 在固定环境下返回外部 package 实际被 Go build 选中的全部 source。
// Module lock 与 go.sum 必须同时传入，防止 resolver 只根据 path/version 信任本机 cache 或网络结果。
type reviewFreezeExternalPackageResolverV1 func(module reviewFreezeValidatorExternalModuleV1, importPath string, environment reviewFreezeValidatorBuildEnvironmentV1, goSumRaw []byte) (map[string][]byte, error)

// reviewFreezeGoModShapeV1 提取独立 Module 构建闭包所需的最小 go.mod 事实。
type reviewFreezeGoModShapeV1 struct {
	modulePath string
	goVersion  string
	toolchain  string
	requires   map[string]string
}

// reviewFreezeExpectedValidatorBuildEnvironmentV1 返回当前 Dora Gate 已批准的唯一候选环境形状。
func reviewFreezeExpectedValidatorBuildEnvironmentV1() reviewFreezeValidatorBuildEnvironmentV1 {
	return reviewFreezeValidatorBuildEnvironmentV1{
		GoVersion:   "1.26",
		Toolchain:   "go1.26.3",
		GOOS:        "linux",
		GOARCH:      "amd64",
		CGOEnabled:  "0",
		GOWORK:      "off",
		GOTOOLCHAIN: "local",
		GOFLAGS:     "-mod=readonly -trimpath",
		GOENV:       "off",
	}
}

// reviewFreezeValidateValidatorBuildClosureV1 校验可选独立 entrypoint 闭包；缺失只表示旧候选仍受 blocker 约束。
func reviewFreezeValidateValidatorBuildClosureV1(manifest reviewFreezeCorpusManifestV1, loader reviewFreezeArtifactLoaderV1, externalResolver reviewFreezeExternalPackageResolverV1) error {
	closure := manifest.ValidatorBuildClosure
	if closure == nil {
		// 兼容既有 v1 manifest 仅用于平滑迁移；缺字段不会产生完整 build closure 或 formal Freeze 结论。
		return nil
	}
	if closure.SchemaVersion != reviewFreezeValidatorBuildClosureSchemaV1 {
		return fmt.Errorf("validator_build_closure schema_version=%q", closure.SchemaVersion)
	}
	if closure.ActivationStatus != "candidate_unactivated" {
		return fmt.Errorf("validator_build_closure activation_status=%q", closure.ActivationStatus)
	}
	if err := reviewFreezeValidateValidatorBuildEnvironmentV1(closure.Environment); err != nil {
		return err
	}
	if len(closure.Entrypoints) != 1 {
		return fmt.Errorf("validator_build_closure entrypoints exact-set 长度=%d want=1", len(closure.Entrypoints))
	}
	if err := reviewFreezeValidateValidatorSourcesV1(manifest.ValidatorSources, loader); err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorBuildSourcesV1(manifest.ValidatorSources, manifest.ValidatorBuildSources, loader); err != nil {
		return err
	}

	wantPackagePaths := reviewFreezeSourcePackagePathsV1(manifest.ValidatorSources)
	actualPackagePaths := make([]string, 0, len(closure.Entrypoints))
	declaredSources := make([]reviewFreezeValidatorSourceV1, 0, len(manifest.ValidatorSources))
	declaredTests := make([]string, 0, len(manifest.TargetTests))
	seenIDs := make(map[string]struct{}, len(closure.Entrypoints))
	lastPackagePath := ""
	for _, entrypoint := range closure.Entrypoints {
		if entrypoint.PackagePath <= lastPackagePath {
			return fmt.Errorf("validator entrypoints 未按 package_path 排序或重复=%q", entrypoint.PackagePath)
		}
		if _, exists := seenIDs[entrypoint.EntrypointID]; exists {
			return fmt.Errorf("validator entrypoint_id 重复=%q", entrypoint.EntrypointID)
		}
		seenIDs[entrypoint.EntrypointID] = struct{}{}
		if err := reviewFreezeValidateValidatorEntrypointV1(entrypoint, closure.Environment, loader, externalResolver); err != nil {
			return fmt.Errorf("validator entrypoint %s: %w", entrypoint.EntrypointID, err)
		}
		actualPackagePaths = append(actualPackagePaths, entrypoint.PackagePath)
		declaredSources = append(declaredSources, entrypoint.DirectSources...)
		declaredTests = append(declaredTests, entrypoint.TestEntrypoints...)
		lastPackagePath = entrypoint.PackagePath
	}
	wantCorpusSchemas := map[string]string{
		"W2-R01.graph_tool_result":    "w2_r01_contract_corpus_manifest.v1",
		"W2-R04.approval_consumption": "w2_r04_approval_consumption_manifest.v1",
	}
	if wantSchema := wantCorpusSchemas[closure.Entrypoints[0].EntrypointID]; manifest.SchemaVersion != wantSchema {
		return fmt.Errorf("entrypoint %s corpus schema_version=%q want=%q", closure.Entrypoints[0].EntrypointID, manifest.SchemaVersion, wantSchema)
	}
	if !reflect.DeepEqual(actualPackagePaths, wantPackagePaths) {
		return fmt.Errorf("validator entrypoint package exact-set=%v want=%v", actualPackagePaths, wantPackagePaths)
	}
	sort.Slice(declaredSources, func(i, j int) bool { return declaredSources[i].Path < declaredSources[j].Path })
	if !reflect.DeepEqual(declaredSources, manifest.ValidatorSources) {
		return fmt.Errorf("validator entrypoint direct source exact-set=%v want=%v", reviewFreezeValidatorSourcePathsV1(declaredSources), reviewFreezeValidatorSourcePathsV1(manifest.ValidatorSources))
	}
	sort.Strings(declaredTests)
	wantTests := append([]string(nil), manifest.TargetTests...)
	sort.Strings(wantTests)
	if !reflect.DeepEqual(declaredTests, wantTests) {
		return fmt.Errorf("validator entrypoint test exact-set=%v want=%v", declaredTests, wantTests)
	}
	return nil
}

// reviewFreezeValidateValidatorBuildClosureJSONV1 区分旧 manifest 的字段缺失与显式 null；只有缺失可走兼容路径。
func reviewFreezeValidateValidatorBuildClosureJSONV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("validator build closure JSON 不是合法 UTF-8")
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawFields); err != nil {
		return err
	}
	if closureRaw, present := rawFields["validator_build_closure"]; present && strings.TrimSpace(string(closureRaw)) == "null" {
		return fmt.Errorf("禁止显式 null validator_build_closure")
	}
	return nil
}

// TestW2ReviewFreezeValidatorBuildClosureV1InvalidUTF8Adversarial 要求 worktree、Gate 与 Git-object JSON 入口均在解码前拒绝非法 UTF-8。
func TestW2ReviewFreezeValidatorBuildClosureV1InvalidUTF8Adversarial(t *testing.T) {
	invalidRaw := append([]byte(`{"schema_version":"`), byte(0xff))
	invalidRaw = append(invalidRaw, []byte(`"}`)...)
	manifestPath := "agent/tests/contract/testdata/invalid_utf8/manifest.json"
	loader := reviewFreezeMapLoaderV1(map[string][]byte{manifestPath: invalidRaw})

	repository := reviewFreezeNewTestGitRepositoryV1(t)
	reviewFreezeWriteTestFileV1(t, repository.root, manifestPath, invalidRaw)
	commitSHA := reviewFreezeCommitTestRepositoryV1(t, repository.root, "invalid UTF-8 contract manifest")

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "raw extension inspector", run: func() error {
			return reviewFreezeValidateValidatorBuildClosureJSONV1(invalidRaw)
		}},
		{name: "contract ref", run: func() error {
			return reviewFreezeValidateContractRefV1(manifestPath, reviewFreezeSHA256V1(invalidRaw), []string{"V-001"}, []string{"TestV001"}, loader)
		}},
		{name: "git object", run: func() error {
			return reviewFreezeValidateGitValidatorContractV1(repository, commitSHA, manifestPath)
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.run()
			if err == nil || !strings.Contains(err.Error(), "UTF-8") {
				t.Fatalf("error=%v want UTF-8 failure", err)
			}
		})
	}
}

// reviewFreezeValidateGateBuildClosurePolicyV1 把旧 shape 兼容限定在 R01/R04 pre-formal 且显式 blocker 尚存的状态。
// 这条 gate-context 规则阻止 `validator_build_closure` 缺失被 nil 兼容错误解释为可进入 Review Frozen/Approved。
func reviewFreezeValidateGateBuildClosurePolicyV1(gate reviewFreezeGateV1, loader reviewFreezeArtifactLoaderV1) error {
	if gate.Gate != "W2-R01" && gate.Gate != "W2-R04" {
		return nil
	}
	paths := make([]string, 0, len(gate.CandidateEvidence)+1)
	for _, candidate := range gate.CandidateEvidence {
		paths = append(paths, candidate.ContractManifestPath)
	}
	if gate.Freeze != nil {
		paths = append(paths, gate.Freeze.ContractManifestPath)
	}
	pending := make([]string, 0)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		raw, err := loader(path)
		if err != nil {
			return fmt.Errorf("读取 build closure policy contract %s: %w", path, err)
		}
		if err := reviewFreezeValidateValidatorBuildClosureJSONV1(raw); err != nil {
			return fmt.Errorf("contract %s: %w", path, err)
		}
		var corpus reviewFreezeCorpusManifestV1
		if err := messageSetStrictDecodeV1(raw, &corpus); err != nil {
			return fmt.Errorf("解析 build closure policy contract %s: %w", path, err)
		}
		if corpus.ValidatorBuildClosure == nil {
			pending = append(pending, path+"(missing)")
			continue
		}
		if corpus.ValidatorBuildClosure.ActivationStatus == "candidate_unactivated" {
			pending = append(pending, path+"(candidate_unactivated)")
		}
		for _, entrypoint := range corpus.ValidatorBuildClosure.Entrypoints {
			if !strings.HasPrefix(entrypoint.EntrypointID, gate.Gate+".") {
				return fmt.Errorf("%s contract %s 引用了其他 Gate entrypoint_id=%q", gate.Gate, path, entrypoint.EntrypointID)
			}
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if reviewFreezeIsFormalStatusV1(gate.Status) {
		return fmt.Errorf("%s formal status=%s 禁止引用未激活 validator_build_closure=%v", gate.Gate, gate.Status, pending)
	}
	wantBlocker := strings.ReplaceAll(gate.Gate, "-", "_") + "_VALIDATOR_BUILD_CLOSURE_PENDING"
	for _, blocker := range gate.Blockers {
		if blocker.Code == wantBlocker {
			return nil
		}
	}
	return fmt.Errorf("%s validator_build_closure 未激活时必须保留 blocker=%s manifests=%v", gate.Gate, wantBlocker, pending)
}

// reviewFreezeValidateValidatorBuildEnvironmentV1 要求所有影响 build selection 的值与候选信任根一致。
func reviewFreezeValidateValidatorBuildEnvironmentV1(environment reviewFreezeValidatorBuildEnvironmentV1) error {
	want := reviewFreezeExpectedValidatorBuildEnvironmentV1()
	if !reflect.DeepEqual(environment, want) {
		return fmt.Errorf("validator build environment=%+v want=%+v", environment, want)
	}
	return nil
}

// reviewFreezeSourcePackagePathsV1 从顶层 validator_sources 推导 entrypoint package path exact-set。
func reviewFreezeSourcePackagePathsV1(sources []reviewFreezeValidatorSourceV1) []string {
	seen := make(map[string]struct{})
	for _, source := range sources {
		seen[pathpkg.Dir(source.Path)] = struct{}{}
	}
	paths := make([]string, 0, len(seen))
	for packagePath := range seen {
		paths = append(paths, packagePath)
	}
	sort.Strings(paths)
	return paths
}

// reviewFreezeValidateValidatorEntrypointV1 校验 package/path/source/test/import exact-set 与依赖策略。
func reviewFreezeValidateValidatorEntrypointV1(entrypoint reviewFreezeValidatorEntrypointV1, environment reviewFreezeValidatorBuildEnvironmentV1, loader reviewFreezeArtifactLoaderV1, externalResolver reviewFreezeExternalPackageResolverV1) error {
	if !reviewFreezeEntrypointIDV1.MatchString(entrypoint.EntrypointID) {
		return fmt.Errorf("entrypoint_id 非法=%q", entrypoint.EntrypointID)
	}
	wantEntrypoints := map[string]struct {
		packageName      string
		packagePath      string
		dependencyPolicy string
	}{
		"W2-R01.graph_tool_result":    {packageName: "w2r01_test", packagePath: "agent/tests/contract/w2r01", dependencyPolicy: "x_text_nfc_only"},
		"W2-R04.approval_consumption": {packageName: "w2r04approvalconsumption_test", packagePath: "agent/tests/contract/w2r04approvalconsumption", dependencyPolicy: "stdlib_only"},
	}
	wantEntrypoint, exists := wantEntrypoints[entrypoint.EntrypointID]
	if !exists || entrypoint.PackageName != wantEntrypoint.packageName || entrypoint.PackagePath != wantEntrypoint.packagePath || entrypoint.DependencyPolicy != wantEntrypoint.dependencyPolicy {
		return fmt.Errorf("entrypoint identity/package/policy 绑定非法 id=%q package=%q path=%q policy=%q", entrypoint.EntrypointID, entrypoint.PackageName, entrypoint.PackagePath, entrypoint.DependencyPolicy)
	}
	moduleRoot, err := reviewFreezeValidatorModuleRootFromPackagePathV1(entrypoint.PackagePath)
	if err != nil {
		return err
	}
	wantPattern := "./" + strings.TrimPrefix(entrypoint.PackagePath, moduleRoot+"/")
	if entrypoint.PackagePattern != wantPattern {
		return fmt.Errorf("package_pattern=%q want=%q", entrypoint.PackagePattern, wantPattern)
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(entrypoint.PackageName) {
		return fmt.Errorf("package_name 非法=%q", entrypoint.PackageName)
	}
	if len(entrypoint.DirectSources) == 0 {
		return fmt.Errorf("direct_sources exact-set 不能为空")
	}
	if err := reviewFreezeValidateSortedStringSliceV1(entrypoint.TestEntrypoints, "test_entrypoints", false); err != nil {
		return err
	}
	if err := reviewFreezeValidateSortedStringSliceV1(entrypoint.AllowedImports, "allowed_imports", true); err != nil {
		return err
	}
	if entrypoint.ExternalModules == nil {
		return fmt.Errorf("external_modules 必须显式声明数组")
	}

	moduleRaw, err := loader(moduleRoot + "/go.mod")
	if err != nil {
		return fmt.Errorf("读取 %s/go.mod: %w", moduleRoot, err)
	}
	moduleShape, err := reviewFreezeParseGoModShapeV1(moduleRaw)
	if err != nil {
		return fmt.Errorf("解析 %s/go.mod: %w", moduleRoot, err)
	}
	if moduleShape.goVersion != environment.GoVersion || moduleShape.toolchain != environment.Toolchain {
		return fmt.Errorf("go.mod go/toolchain=%s/%s want=%s/%s", moduleShape.goVersion, moduleShape.toolchain, environment.GoVersion, environment.Toolchain)
	}
	wantModulePaths := map[string]string{
		"agent":    "github.com/FigoGoo/Dora-Agent/agent",
		"business": "github.com/FigoGoo/Dora-Agent/business",
		"worker":   "github.com/FigoGoo/Dora-Agent/worker",
	}
	if moduleShape.modulePath != wantModulePaths[moduleRoot] {
		return fmt.Errorf("go.mod module path=%q want=%q", moduleShape.modulePath, wantModulePaths[moduleRoot])
	}

	actualImports := make(map[string]struct{})
	actualTests := make(map[string]struct{})
	lastSourcePath := ""
	for _, source := range entrypoint.DirectSources {
		if source.Path <= lastSourcePath || pathpkg.Dir(source.Path) != entrypoint.PackagePath {
			return fmt.Errorf("direct_sources 未排序、重复或越出 package=%q", source.Path)
		}
		raw, err := loader(source.Path)
		if err != nil {
			return fmt.Errorf("读取 direct source %s: %w", source.Path, err)
		}
		if err := reviewFreezeCheckSHA256V1(raw, source.SHA256); err != nil {
			return fmt.Errorf("direct source %s: %w", source.Path, err)
		}
		parsed, imports, tests, err := reviewFreezeParseValidatorSourceV1(source.Path, raw, true)
		if err != nil {
			return err
		}
		if parsed.Name.Name != entrypoint.PackageName {
			return fmt.Errorf("direct source %s package=%q want=%q", source.Path, parsed.Name.Name, entrypoint.PackageName)
		}
		if len(tests) != 0 && !strings.HasSuffix(source.Path, "_test.go") {
			return fmt.Errorf("Go Test entrypoint 只能声明于 _test.go=%s", source.Path)
		}
		for _, importPath := range imports {
			actualImports[importPath] = struct{}{}
		}
		for _, testName := range tests {
			if _, duplicate := actualTests[testName]; duplicate {
				return fmt.Errorf("direct sources 顶层 Test 重复=%q", testName)
			}
			actualTests[testName] = struct{}{}
		}
		lastSourcePath = source.Path
	}
	if got := reviewFreezeSortedKeysV1(actualImports); !reflect.DeepEqual(got, entrypoint.AllowedImports) {
		return fmt.Errorf("allowed_imports=%v want source exact-set=%v", entrypoint.AllowedImports, got)
	}
	if got := reviewFreezeSortedKeysV1(actualTests); !reflect.DeepEqual(got, entrypoint.TestEntrypoints) {
		return fmt.Errorf("test_entrypoints=%v want source exact-set=%v", entrypoint.TestEntrypoints, got)
	}
	return reviewFreezeValidateValidatorDependencyClosureV1(entrypoint, environment, moduleRoot, moduleShape, loader, externalResolver)
}

// reviewFreezeValidatorModuleRootFromPackagePathV1 要求 entrypoint 位于独立 contract 子 package，而不是共享 contract_test 根包。
func reviewFreezeValidatorModuleRootFromPackagePathV1(packagePath string) (string, error) {
	if err := reviewFreezeValidateSafePathV1(packagePath, ""); err != nil {
		return "", err
	}
	for _, moduleRoot := range []string{"agent", "business", "worker"} {
		prefix := moduleRoot + "/tests/contract/"
		if strings.HasPrefix(packagePath, prefix) && strings.TrimPrefix(packagePath, prefix) != "" && !strings.Contains(strings.TrimPrefix(packagePath, prefix), "/") {
			return moduleRoot, nil
		}
	}
	return "", fmt.Errorf("entrypoint 必须位于独立 contract 子 package=%q", packagePath)
}

// reviewFreezeParseValidatorSourceV1 严格解析 source，并拒绝 embed、cgo、dot/blank import；entrypoint source 还拒绝隐藏执行入口。
func reviewFreezeParseValidatorSourceV1(sourcePath string, raw []byte, enforceEntrypoint bool) (*ast.File, []string, []string, error) {
	if reviewFreezeGoEmbedDirectiveV1.Match(raw) {
		return nil, nil, nil, fmt.Errorf("validator source %s 禁止 go:embed", sourcePath)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析 validator source %s: %w", sourcePath, err)
	}
	imports := make(map[string]struct{}, len(parsed.Imports))
	importAliases := make(map[string]string, len(parsed.Imports))
	for _, importSpec := range parsed.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil || !reviewFreezeExternalImportPathV1.MatchString(importPath) {
			return nil, nil, nil, fmt.Errorf("validator source %s import path 非法=%q", sourcePath, importSpec.Path.Value)
		}
		if importPath == "C" {
			return nil, nil, nil, fmt.Errorf("validator source %s 禁止 cgo import C", sourcePath)
		}
		if importSpec.Name != nil && (importSpec.Name.Name == "." || importSpec.Name.Name == "_") {
			return nil, nil, nil, fmt.Errorf("validator source %s 禁止 dot/blank import=%q", sourcePath, importPath)
		}
		imports[importPath] = struct{}{}
		alias := pathpkg.Base(importPath)
		if importSpec.Name != nil {
			alias = importSpec.Name.Name
		}
		importAliases[importPath] = alias
	}
	tests := make(map[string]struct{})
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}
		if !enforceEntrypoint {
			continue
		}
		if function.Name.Name == "init" {
			return nil, nil, nil, fmt.Errorf("validator source %s 禁止 init", sourcePath)
		}
		if function.Name.Name == "TestMain" {
			return nil, nil, nil, fmt.Errorf("validator source %s 禁止 TestMain", sourcePath)
		}
		if strings.HasPrefix(function.Name.Name, "Example") || reviewFreezeIsGoTestNameV1(function.Name.Name, "Fuzz") || reviewFreezeIsGoTestNameV1(function.Name.Name, "Benchmark") {
			return nil, nil, nil, fmt.Errorf("validator source %s 禁止额外 Go test entrypoint=%s", sourcePath, function.Name.Name)
		}
		if reviewFreezeIsGoTestNameV1(function.Name.Name, "Test") {
			if !reviewFreezeValidGoTestSignatureV1(function, importAliases["testing"]) {
				return nil, nil, nil, fmt.Errorf("validator source %s Test signature 非法=%s", sourcePath, function.Name.Name)
			}
			tests[function.Name.Name] = struct{}{}
		}
	}
	return parsed, reviewFreezeSortedKeysV1(imports), reviewFreezeSortedKeysV1(tests), nil
}

// reviewFreezeIsGoTestNameV1 复刻 cmd/go 对 Test/Benchmark/Fuzz 名称后首个 Unicode rune 的判定。
func reviewFreezeIsGoTestNameV1(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(next)
}

// reviewFreezeValidGoTestSignatureV1 只接受 Go testing 识别的 `func TestX(*testing.T)` 形状。
func reviewFreezeValidGoTestSignatureV1(function *ast.FuncDecl, testingAlias string) bool {
	if testingAlias == "" || function.Type.TypeParams != nil || function.Type.Params == nil || len(function.Type.Params.List) != 1 || function.Type.Results != nil && len(function.Type.Results.List) != 0 {
		return false
	}
	parameter := function.Type.Params.List[0]
	if len(parameter.Names) > 1 {
		return false
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == testingAlias
}

// reviewFreezeValidateValidatorDependencyClosureV1 校验 stdlib-only 或 x/text NFC 例外及外部 selected-source 闭包。
func reviewFreezeValidateValidatorDependencyClosureV1(entrypoint reviewFreezeValidatorEntrypointV1, environment reviewFreezeValidatorBuildEnvironmentV1, moduleRoot string, moduleShape reviewFreezeGoModShapeV1, loader reviewFreezeArtifactLoaderV1, externalResolver reviewFreezeExternalPackageResolverV1) error {
	externalImports := make([]string, 0)
	for _, importPath := range entrypoint.AllowedImports {
		if reviewFreezeIsStandardLibraryImportV1(importPath) {
			continue
		}
		if importPath == moduleShape.modulePath || strings.HasPrefix(importPath, moduleShape.modulePath+"/") {
			return fmt.Errorf("独立 entrypoint 禁止同 Module 传递 import=%q", importPath)
		}
		externalImports = append(externalImports, importPath)
	}

	switch entrypoint.DependencyPolicy {
	case "stdlib_only":
		if len(externalImports) != 0 || len(entrypoint.ExternalModules) != 0 {
			return fmt.Errorf("stdlib_only 禁止 external imports/modules=%v/%v", externalImports, reviewFreezeExternalModulePathsV1(entrypoint.ExternalModules))
		}
	case "x_text_nfc_only":
		wantImports := []string{"golang.org/x/text/unicode/norm"}
		if !reflect.DeepEqual(externalImports, wantImports) {
			return fmt.Errorf("x_text_nfc_only external imports=%v want=%v", externalImports, wantImports)
		}
		if len(entrypoint.ExternalModules) != 1 || entrypoint.ExternalModules[0].ModulePath != "golang.org/x/text" {
			return fmt.Errorf("x_text_nfc_only module lock=%v", reviewFreezeExternalModulePathsV1(entrypoint.ExternalModules))
		}
	default:
		return fmt.Errorf("dependency_policy 非法=%q", entrypoint.DependencyPolicy)
	}

	if len(entrypoint.ExternalModules) == 0 {
		return nil
	}
	if externalResolver == nil {
		return fmt.Errorf("external build-selected source resolver 未接入")
	}
	goSumRaw, err := loader(moduleRoot + "/go.sum")
	if err != nil {
		return fmt.Errorf("读取 %s/go.sum: %w", moduleRoot, err)
	}
	return reviewFreezeValidateExternalModulesV1(entrypoint.ExternalModules, externalImports, environment, moduleShape, goSumRaw, externalResolver)
}

// reviewFreezeValidateExternalModulesV1 绑定 require/go.sum，并验证 resolver 返回的 build-selected source 与 import 图 exact-set。
func reviewFreezeValidateExternalModulesV1(modules []reviewFreezeValidatorExternalModuleV1, roots []string, environment reviewFreezeValidatorBuildEnvironmentV1, moduleShape reviewFreezeGoModShapeV1, goSumRaw []byte, resolver reviewFreezeExternalPackageResolverV1) error {
	packageOwners := make(map[string]string)
	packageImports := make(map[string][]string)
	lastModule := ""
	for _, module := range modules {
		if module.ModulePath <= lastModule || !reviewFreezeExternalImportPathV1.MatchString(module.ModulePath) {
			return fmt.Errorf("external_modules 未排序、重复或 module_path 非法=%q", module.ModulePath)
		}
		if !reviewFreezeModuleVersionV1.MatchString(module.Version) {
			return fmt.Errorf("external module %s version 非法=%q", module.ModulePath, module.Version)
		}
		if !reviewFreezeGoModuleChecksumV1.MatchString(module.ModuleSum) || !reviewFreezeGoModuleChecksumV1.MatchString(module.GoModSum) {
			return fmt.Errorf("external module %s h1 checksum 非法", module.ModulePath)
		}
		if moduleShape.requires[module.ModulePath] != module.Version {
			return fmt.Errorf("external module %s version=%s want go.mod require=%s", module.ModulePath, module.Version, moduleShape.requires[module.ModulePath])
		}
		if !reviewFreezeGoSumContainsV1(goSumRaw, module.ModulePath, module.Version, module.ModuleSum, module.GoModSum) {
			return fmt.Errorf("external module %s@%s 未绑定 go.sum exact sums", module.ModulePath, module.Version)
		}
		if len(module.Packages) == 0 {
			return fmt.Errorf("external module %s packages exact-set 不能为空", module.ModulePath)
		}
		lastPackage := ""
		for _, selectedPackage := range module.Packages {
			if selectedPackage.ImportPath <= lastPackage || (selectedPackage.ImportPath != module.ModulePath && !strings.HasPrefix(selectedPackage.ImportPath, module.ModulePath+"/")) {
				return fmt.Errorf("external module %s packages 未排序、重复或越界=%q", module.ModulePath, selectedPackage.ImportPath)
			}
			if _, exists := packageOwners[selectedPackage.ImportPath]; exists {
				return fmt.Errorf("external package 被重复声明=%q", selectedPackage.ImportPath)
			}
			imports, err := reviewFreezeValidateExternalPackageV1(module, selectedPackage, environment, goSumRaw, resolver)
			if err != nil {
				return err
			}
			packageOwners[selectedPackage.ImportPath] = module.ModulePath
			packageImports[selectedPackage.ImportPath] = imports
			lastPackage = selectedPackage.ImportPath
		}
		lastModule = module.ModulePath
	}

	// 外部 package 图必须从 direct import 完整可达，且每条非 stdlib import 都有唯一 selected package。
	queue := append([]string(nil), roots...)
	reachable := make(map[string]struct{}, len(packageOwners))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := reachable[current]; seen {
			continue
		}
		if _, exists := packageOwners[current]; !exists {
			return fmt.Errorf("external import 缺 build-selected package=%q", current)
		}
		reachable[current] = struct{}{}
		for _, dependency := range packageImports[current] {
			if reviewFreezeIsStandardLibraryImportV1(dependency) {
				continue
			}
			if _, exists := packageOwners[dependency]; !exists {
				return fmt.Errorf("external package %s import 未闭包=%q", current, dependency)
			}
			queue = append(queue, dependency)
		}
	}
	if len(reachable) != len(packageOwners) {
		return fmt.Errorf("external build-selected package 含不可达声明=%v reachable=%v", reviewFreezeSortedKeysV1(packageOwners), reviewFreezeSortedKeysV1(reachable))
	}
	return nil
}

// reviewFreezeValidateExternalPackageV1 对 resolver 的实际 selected source 做 exact-set、摘要、embed 和 import 校验。
func reviewFreezeValidateExternalPackageV1(module reviewFreezeValidatorExternalModuleV1, selectedPackage reviewFreezeValidatorExternalPackageV1, environment reviewFreezeValidatorBuildEnvironmentV1, goSumRaw []byte, resolver reviewFreezeExternalPackageResolverV1) ([]string, error) {
	if len(selectedPackage.Sources) == 0 {
		return nil, fmt.Errorf("external package %s sources exact-set 不能为空", selectedPackage.ImportPath)
	}
	if err := reviewFreezeValidateSortedStringSliceV1(selectedPackage.AllowedImports, "external package allowed_imports", true); err != nil {
		return nil, err
	}
	actual, err := resolver(module, selectedPackage.ImportPath, environment, goSumRaw)
	if err != nil {
		return nil, fmt.Errorf("解析 external package %s selected sources: %w", selectedPackage.ImportPath, err)
	}
	declaredPaths := make([]string, 0, len(selectedPackage.Sources))
	actualImports := make(map[string]struct{})
	wantDir := strings.TrimPrefix(selectedPackage.ImportPath, module.ModulePath)
	wantDir = strings.TrimPrefix(wantDir, "/")
	lastPath := ""
	for _, source := range selectedPackage.Sources {
		if source.Path <= lastPath || pathpkg.Ext(source.Path) != ".go" || pathpkg.Dir(source.Path) != wantDir {
			return nil, fmt.Errorf("external package %s source 未排序、重复、非 Go 或越界=%q", selectedPackage.ImportPath, source.Path)
		}
		if err := reviewFreezeValidateSafePathV1(source.Path, ""); err != nil {
			return nil, err
		}
		raw, exists := actual[source.Path]
		if !exists {
			return nil, fmt.Errorf("external package %s declared source 未被 build 选择=%q", selectedPackage.ImportPath, source.Path)
		}
		if err := reviewFreezeCheckSHA256V1(raw, source.SHA256); err != nil {
			return nil, fmt.Errorf("external source %s@%s/%s: %w", module.ModulePath, module.Version, source.Path, err)
		}
		_, imports, _, err := reviewFreezeParseValidatorSourceV1(source.Path, raw, false)
		if err != nil {
			return nil, fmt.Errorf("external package %s: %w", selectedPackage.ImportPath, err)
		}
		for _, importPath := range imports {
			actualImports[importPath] = struct{}{}
		}
		declaredPaths = append(declaredPaths, source.Path)
		lastPath = source.Path
	}
	actualPaths := make([]string, 0, len(actual))
	for sourcePath := range actual {
		actualPaths = append(actualPaths, sourcePath)
	}
	sort.Strings(actualPaths)
	if !reflect.DeepEqual(declaredPaths, actualPaths) {
		return nil, fmt.Errorf("external package %s selected source exact-set=%v want=%v", selectedPackage.ImportPath, declaredPaths, actualPaths)
	}
	imports := reviewFreezeSortedKeysV1(actualImports)
	if !reflect.DeepEqual(selectedPackage.AllowedImports, imports) {
		return nil, fmt.Errorf("external package %s allowed_imports=%v want source exact-set=%v", selectedPackage.ImportPath, selectedPackage.AllowedImports, imports)
	}
	return imports, nil
}

// reviewFreezeValidateGitValidatorAuxiliaryBuildInputsV1 在不可变 Git tree 中拒绝未建模的 asm/cgo/syso 输入与 vendor 覆盖。
func reviewFreezeValidateGitValidatorAuxiliaryBuildInputsV1(repository reviewFreezeGitRepositoryV1, commitSHA string, manifest reviewFreezeCorpusManifestV1) error {
	if manifest.ValidatorBuildClosure == nil {
		return nil
	}
	forbiddenExtensions := map[string]struct{}{
		".S": {}, ".c": {}, ".cc": {}, ".cpp": {}, ".cxx": {}, ".f": {}, ".F": {}, ".for": {}, ".f90": {},
		".h": {}, ".hh": {}, ".hpp": {}, ".hxx": {}, ".m": {}, ".mm": {}, ".s": {}, ".sx": {}, ".syso": {}, ".swig": {}, ".swigcxx": {},
	}
	moduleRoots := make(map[string]struct{})
	for _, entrypoint := range manifest.ValidatorBuildClosure.Entrypoints {
		moduleRoot, err := reviewFreezeValidatorModuleRootFromPackagePathV1(entrypoint.PackagePath)
		if err != nil {
			return err
		}
		moduleRoots[moduleRoot] = struct{}{}
		raw, err := repository.git("ls-tree", "--full-tree", "-r", "-z", commitSHA, "--", entrypoint.PackagePath)
		if err != nil {
			return err
		}
		entries, err := reviewFreezeParseTreeEntriesV1(raw)
		if err != nil {
			return err
		}
		prefix := entrypoint.PackagePath + "/"
		for _, entry := range entries {
			if !strings.HasPrefix(entry.path, prefix) {
				return fmt.Errorf("git ls-tree 返回 entrypoint 越界路径=%q", entry.path)
			}
			remainder := strings.TrimPrefix(entry.path, prefix)
			if strings.Contains(remainder, "/") {
				continue
			}
			if _, forbidden := forbiddenExtensions[pathpkg.Ext(remainder)]; forbidden {
				return fmt.Errorf("validator entrypoint 禁止未绑定非 Go build input=%q", entry.path)
			}
		}
	}
	for moduleRoot := range moduleRoots {
		vendorPrefix := moduleRoot + "/vendor"
		raw, err := repository.git("ls-tree", "--full-tree", "-r", "-z", commitSHA, "--", vendorPrefix)
		if err != nil {
			return err
		}
		entries, err := reviewFreezeParseTreeEntriesV1(raw)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("validator build closure 禁止 vendor tree=%s entries=%d", vendorPrefix, len(entries))
		}
	}
	return nil
}

// reviewFreezeParseGoModShapeV1 解析 module/go/toolchain/require，拒绝缺失和重复的关键构建事实。
func reviewFreezeParseGoModShapeV1(raw []byte) (reviewFreezeGoModShapeV1, error) {
	shape := reviewFreezeGoModShapeV1{requires: make(map[string]string)}
	inRequireBlock := false
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		switch {
		case len(fields) == 2 && fields[0] == "module":
			if shape.modulePath != "" {
				return shape, fmt.Errorf("module directive 重复")
			}
			shape.modulePath = fields[1]
		case len(fields) == 2 && fields[0] == "go":
			if shape.goVersion != "" {
				return shape, fmt.Errorf("go directive 重复")
			}
			shape.goVersion = fields[1]
		case len(fields) == 2 && fields[0] == "toolchain":
			if shape.toolchain != "" {
				return shape, fmt.Errorf("toolchain directive 重复")
			}
			shape.toolchain = fields[1]
		case trimmed == "require (":
			if inRequireBlock {
				return shape, fmt.Errorf("require block 嵌套 line=%d", lineNumber+1)
			}
			inRequireBlock = true
		case trimmed == ")" && inRequireBlock:
			inRequireBlock = false
		case len(fields) == 3 && fields[0] == "require":
			if err := reviewFreezeRecordGoModRequireV1(shape.requires, fields[1], fields[2]); err != nil {
				return shape, err
			}
		case inRequireBlock && len(fields) >= 2:
			if err := reviewFreezeRecordGoModRequireV1(shape.requires, fields[0], fields[1]); err != nil {
				return shape, err
			}
		}
	}
	if inRequireBlock || shape.modulePath == "" || shape.goVersion == "" || shape.toolchain == "" {
		return shape, fmt.Errorf("go.mod 缺 module/go/toolchain 或 require 未闭合")
	}
	return shape, nil
}

// reviewFreezeRecordGoModRequireV1 记录唯一 Module require，避免同一模块多版本歧义。
func reviewFreezeRecordGoModRequireV1(requires map[string]string, modulePath, version string) error {
	if old, exists := requires[modulePath]; exists {
		return fmt.Errorf("go.mod require 重复=%s old=%s new=%s", modulePath, old, version)
	}
	requires[modulePath] = version
	return nil
}

// reviewFreezeGoSumContainsV1 要求 Module zip 与 go.mod 两条摘要均精确存在。
func reviewFreezeGoSumContainsV1(raw []byte, modulePath, version, moduleSum, goModSum string) bool {
	wantModule := modulePath + " " + version + " " + moduleSum
	wantGoMod := modulePath + " " + version + "/go.mod " + goModSum
	seenModule := false
	seenGoMod := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch line {
		case wantModule:
			seenModule = true
		case wantGoMod:
			seenGoMod = true
		}
	}
	return seenModule && seenGoMod
}

// reviewFreezeIsStandardLibraryImportV1 使用 Go import path 第一段是否含点区分 stdlib 与 Module import。
func reviewFreezeIsStandardLibraryImportV1(importPath string) bool {
	first := strings.SplitN(importPath, "/", 2)[0]
	return !strings.Contains(first, ".")
}

// reviewFreezeValidateSortedStringSliceV1 校验显式数组的排序唯一性；allowEmpty 仅允许非 nil 空数组。
func reviewFreezeValidateSortedStringSliceV1(values []string, field string, allowEmpty bool) error {
	if values == nil {
		return fmt.Errorf("%s 必须显式声明数组", field)
	}
	if len(values) == 0 && !allowEmpty {
		return fmt.Errorf("%s exact-set 不能为空", field)
	}
	last := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" || value <= last {
			return fmt.Errorf("%s 未排序、重复或为空=%q", field, value)
		}
		last = value
	}
	return nil
}

// reviewFreezeSortedKeysV1 返回 map key 的稳定排序 exact-set。
func reviewFreezeSortedKeysV1[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// reviewFreezeExternalModulePathsV1 返回外部 Module 声明顺序，供失败诊断使用。
func reviewFreezeExternalModulePathsV1(modules []reviewFreezeValidatorExternalModuleV1) []string {
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		paths = append(paths, module.ModulePath)
	}
	return paths
}

// TestW2ReviewFreezeValidatorBuildClosureV1LegacyCompatible 证明旧 v1 manifest 可继续缺少扩展，
// 同时要求已迁移的 R04 manifest 携带唯一未激活候选闭包。
func TestW2ReviewFreezeValidatorBuildClosureV1LegacyCompatible(t *testing.T) {
	manifest, _ := reviewFreezeLoadCurrentV1(t)
	loader := reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t))
	for _, gate := range manifest.Gates {
		for _, candidate := range gate.CandidateEvidence {
			raw, err := loader(candidate.ContractManifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var corpus reviewFreezeCorpusManifestV1
			if err := messageSetStrictDecodeV1(raw, &corpus); err != nil {
				t.Fatal(err)
			}
			if gate.Gate == "W2-R04" {
				if corpus.ValidatorBuildClosure == nil || corpus.ValidatorBuildClosure.ActivationStatus != "candidate_unactivated" ||
					len(corpus.ValidatorBuildClosure.Entrypoints) != 1 || corpus.ValidatorBuildClosure.Entrypoints[0].EntrypointID != "W2-R04.approval_consumption" {
					t.Fatalf("R04 candidate 缺少唯一未激活 validator_build_closure: %+v", corpus.ValidatorBuildClosure)
				}
			} else if corpus.ValidatorBuildClosure != nil {
				t.Fatalf("legacy candidate unexpectedly has validator_build_closure: %s", candidate.ContractManifestPath)
			}
			if err := reviewFreezeValidateValidatorBuildClosureV1(corpus, loader, nil); err != nil {
				t.Fatalf("legacy candidate rejected: %v", err)
			}
		}
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1MissingExtensionCannotBecomeFormal 证明缺失或 candidate_unactivated
// 闭包只兼容带 blocker 的 pre-formal R01/R04。
func TestW2ReviewFreezeValidatorBuildClosureV1MissingExtensionCannotBecomeFormal(t *testing.T) {
	manifest, _ := reviewFreezeLoadCurrentV1(t)
	loader := reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t))
	for _, gateIndex := range []int{1, 4} {
		gate := manifest.Gates[gateIndex]
		if err := reviewFreezeValidateGateBuildClosurePolicyV1(gate, loader); err != nil {
			t.Fatalf("current %s pre-formal blocker rejected: %v", gate.Gate, err)
		}

		withoutBlocker := gate
		withoutBlocker.Blockers = append([]reviewFreezeBlockerV1(nil), gate.Blockers...)
		wantBlocker := strings.ReplaceAll(gate.Gate, "-", "_") + "_VALIDATOR_BUILD_CLOSURE_PENDING"
		filtered := withoutBlocker.Blockers[:0]
		for _, blocker := range withoutBlocker.Blockers {
			if blocker.Code != wantBlocker {
				filtered = append(filtered, blocker)
			}
		}
		withoutBlocker.Blockers = filtered
		if err := reviewFreezeValidateGateBuildClosurePolicyV1(withoutBlocker, loader); err == nil || !strings.Contains(err.Error(), wantBlocker) {
			t.Fatalf("%s missing extension without blocker error=%v", gate.Gate, err)
		}

		for _, status := range []string{"review_frozen", "approved", "reopened"} {
			formal := gate
			formal.Status = status
			if err := reviewFreezeValidateGateBuildClosurePolicyV1(formal, loader); err == nil || !strings.Contains(err.Error(), "formal status") {
				t.Fatalf("%s status=%s missing extension error=%v", gate.Gate, status, err)
			}
		}

		candidatePath := gate.CandidateEvidence[0].ContractManifestPath
		candidateRaw, err := loader(candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		var rawFields map[string]json.RawMessage
		if err := json.Unmarshal(candidateRaw, &rawFields); err != nil {
			t.Fatal(err)
		}
		rawFields["validator_build_closure"] = json.RawMessage("null")
		nullRaw, err := json.Marshal(rawFields)
		if err != nil {
			t.Fatal(err)
		}
		nullLoader := reviewFreezeOverlayLoaderV1(map[string][]byte{candidatePath: nullRaw}, loader)
		if err := reviewFreezeValidateGateBuildClosurePolicyV1(gate, nullLoader); err == nil || !strings.Contains(err.Error(), "显式 null") {
			t.Fatalf("%s explicit null extension error=%v", gate.Gate, err)
		}
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1GateBinding 禁止 Gate 引用另一 Gate 自报的 entrypoint/schema policy。
func TestW2ReviewFreezeValidatorBuildClosureV1GateBinding(t *testing.T) {
	corpus, files, _ := reviewFreezeSyntheticBuildClosureFilesV1(t, "r01")
	contractPath := "agent/tests/contract/w2r01/testdata/manifest.json"
	files[contractPath] = reviewFreezeMarshalV1(t, corpus)
	gate := reviewFreezeGateV1{
		Gate: "W2-R04", Status: "expansion_frozen",
		CandidateEvidence: []reviewFreezeCandidateEvidenceV1{{ContractManifestPath: contractPath}},
		Blockers:          []reviewFreezeBlockerV1{{Code: "W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING", Statement: "synthetic"}},
	}
	if err := reviewFreezeValidateGateBuildClosurePolicyV1(gate, reviewFreezeMapLoaderV1(files)); err == nil || !strings.Contains(err.Error(), "其他 Gate entrypoint_id") {
		t.Fatalf("cross-gate entrypoint error=%v", err)
	}

	r04Corpus, r04Files, _ := reviewFreezeSyntheticBuildClosureFilesV1(t, "r04")
	r04Path := "agent/tests/contract/w2r04approvalconsumption/testdata/manifest.json"
	r04Files[r04Path] = reviewFreezeMarshalV1(t, r04Corpus)
	r04Loader := reviewFreezeMapLoaderV1(r04Files)
	r04Gate := reviewFreezeGateV1{
		Gate: "W2-R04", Status: "expansion_frozen",
		CandidateEvidence: []reviewFreezeCandidateEvidenceV1{{ContractManifestPath: r04Path}},
		Blockers:          []reviewFreezeBlockerV1{{Code: "W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING", Statement: "synthetic"}},
	}
	if err := reviewFreezeValidateGateBuildClosurePolicyV1(r04Gate, r04Loader); err != nil {
		t.Fatalf("candidate_unactivated with blocker rejected: %v", err)
	}
	withoutBlocker := r04Gate
	withoutBlocker.Blockers = nil
	if err := reviewFreezeValidateGateBuildClosurePolicyV1(withoutBlocker, r04Loader); err == nil || !strings.Contains(err.Error(), "W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING") {
		t.Fatalf("candidate_unactivated without blocker error=%v", err)
	}
	for _, status := range []string{"review_frozen", "approved", "reopened"} {
		formal := r04Gate
		formal.Status = status
		if err := reviewFreezeValidateGateBuildClosurePolicyV1(formal, r04Loader); err == nil || !strings.Contains(err.Error(), "formal status") {
			t.Fatalf("candidate_unactivated status=%s formal error=%v", status, err)
		}
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1GoTestNameUnicode 对齐 cmd/go 的 Unicode lowercase 判定。
func TestW2ReviewFreezeValidatorBuildClosureV1GoTestNameUnicode(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "Test", want: true},
		{name: "TestA", want: true},
		{name: "Test1", want: true},
		{name: "Testé", want: false},
		{name: "TestÉ", want: true},
		{name: "Testing", want: false},
	}
	for _, tc := range tests {
		if got := reviewFreezeIsGoTestNameV1(tc.name, "Test"); got != tc.want {
			t.Fatalf("isGoTestName(%q)=%t want=%t", tc.name, got, tc.want)
		}
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1ValidShapes 覆盖 R04 stdlib-only 与 R01 x/text NFC 两种已批准候选形状。
func TestW2ReviewFreezeValidatorBuildClosureV1ValidShapes(t *testing.T) {
	t.Run("R04 stdlib only", func(t *testing.T) {
		manifest, loader, resolver := reviewFreezeSyntheticBuildClosureV1(t, "r04")
		if err := reviewFreezeValidateValidatorBuildClosureV1(manifest, loader, resolver); err != nil {
			t.Fatalf("valid R04 closure rejected: %v", err)
		}
	})
	t.Run("R01 x/text NFC", func(t *testing.T) {
		manifest, loader, resolver := reviewFreezeSyntheticBuildClosureV1(t, "r01")
		if err := reviewFreezeValidateValidatorBuildClosureV1(manifest, loader, resolver); err != nil {
			t.Fatalf("valid R01 closure rejected: %v", err)
		}
	})
}

// TestW2ReviewFreezeValidatorBuildClosureV1EnvironmentAdversarial 固定全部九项 Go/toolchain/build-selection 环境字段。
func TestW2ReviewFreezeValidatorBuildClosureV1EnvironmentAdversarial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeValidatorBuildEnvironmentV1)
	}{
		{name: "go", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GoVersion = "1.27" }},
		{name: "toolchain", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.Toolchain = "go1.26.4" }},
		{name: "GOOS", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOOS = "darwin" }},
		{name: "GOARCH", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOARCH = "arm64" }},
		{name: "CGO_ENABLED", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.CGOEnabled = "1" }},
		{name: "GOWORK", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOWORK = "auto" }},
		{name: "GOTOOLCHAIN", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOTOOLCHAIN = "auto" }},
		{name: "GOFLAGS", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOFLAGS = "-mod=mod" }},
		{name: "GOENV", mutate: func(value *reviewFreezeValidatorBuildEnvironmentV1) { value.GOENV = "/tmp/goenv" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, loader, resolver := reviewFreezeSyntheticBuildClosureV1(t, "r04")
			tc.mutate(&manifest.ValidatorBuildClosure.Environment)
			err := reviewFreezeValidateValidatorBuildClosureV1(manifest, loader, resolver)
			if err == nil || !strings.Contains(err.Error(), "build environment") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1Adversarial 覆盖 entrypoint、source、embed、import、Module lock 与 selected-source 失败关闭。
func TestW2ReviewFreezeValidatorBuildClosureV1Adversarial(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		mutate func(*reviewFreezeCorpusManifestV1, map[string][]byte, *reviewFreezeSyntheticExternalResolverV1)
		want   string
	}{
		{name: "package path escapes independent package", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].PackagePath = "agent/tests/contract"
		}, want: "identity/package/policy"},
		{name: "R04 self reports R01 identity and x/text policy", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].EntrypointID = "W2-R01.graph_tool_result"
			manifest.ValidatorBuildClosure.Entrypoints[0].DependencyPolicy = "x_text_nfc_only"
		}, want: "identity/package/policy"},
		{name: "package pattern drift", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].PackagePattern = "./tests/contract/..."
		}, want: "package_pattern"},
		{name: "candidate activation status escalation", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.ActivationStatus = "active"
		}, want: "activation_status"},
		{name: "corpus schema drift", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.SchemaVersion = "synthetic_other_manifest.v1"
		}, want: "corpus schema_version"},
		{name: "direct source omitted", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorSources = manifest.ValidatorSources[:1]
		}, want: "direct source exact-set"},
		{name: "direct source digest rebound rejected", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nfunc TestR04Manifest() {}\n")
		}, want: "sha256="},
		{name: "go embed rebound rejected", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nimport _ \"embed\"\n\n//go:embed vectors.json\nvar vectors []byte\n\nfunc TestR04Manifest() {}\n")
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "go:embed"},
		{name: "invalid Test signature", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nimport \"testing\"\n\nfunc TestR04Consumption(testing.T) {}\n")
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "Test signature"},
		{name: "Test declared outside _test.go", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			oldPath := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			newPath := strings.TrimSuffix(oldPath, "_test.go") + ".go"
			files[newPath] = files[oldPath]
			delete(files, oldPath)
			manifest.ValidatorSources[0].Path = newPath
			manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path = newPath
		}, want: "只能声明于 _test.go"},
		{name: "duplicate Test entrypoint", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[1].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nimport \"testing\"\n\nfunc TestR04Consumption(*testing.T) {}\n")
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "Test 重复"},
		{name: "init entrypoint", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = append(files[path], []byte("\nfunc init() {}\n")...)
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "禁止 init"},
		{name: "Fuzz entrypoint", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = append(files[path], []byte("\nfunc FuzzHidden(*testing.F) {}\n")...)
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "额外 Go test entrypoint"},
		{name: "Example entrypoint", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = append(files[path], []byte("\nfunc ExampleHidden() {}\n")...)
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "额外 Go test entrypoint"},
		{name: "undeclared import", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nimport (\"encoding/json\"; \"testing\")\n\nfunc TestR04Consumption(*testing.T) { _ = json.Valid(nil) }\n")
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
		}, want: "allowed_imports"},
		{name: "same module import forbidden", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, files map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			path := manifest.ValidatorBuildClosure.Entrypoints[0].DirectSources[0].Path
			files[path] = []byte("package w2r04approvalconsumption_test\n\nimport (\"github.com/FigoGoo/Dora-Agent/agent/internal/event\"; \"testing\")\n\nfunc TestR04Consumption(*testing.T) {}\n")
			reviewFreezeRebindSyntheticSourceV1(manifest, path, files[path])
			manifest.ValidatorBuildClosure.Entrypoints[0].AllowedImports = []string{"github.com/FigoGoo/Dora-Agent/agent/internal/event", "testing"}
		}, want: "同 Module"},
		{name: "R04 external dependency forbidden", kind: "r04", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules = []reviewFreezeValidatorExternalModuleV1{{ModulePath: "golang.org/x/text"}}
		}, want: "stdlib_only"},
		{name: "R01 missing resolver", kind: "r01", mutate: func(_ *reviewFreezeCorpusManifestV1, _ map[string][]byte, resolver *reviewFreezeSyntheticExternalResolverV1) {
			resolver.disabled = true
		}, want: "resolver 未接入"},
		{name: "R01 module version drift", kind: "r01", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules[0].Version = "v0.33.0"
		}, want: "want go.mod require"},
		{name: "R01 module sum drift", kind: "r01", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules[0].ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}, want: "go.sum exact sums"},
		{name: "R01 selected source omitted", kind: "r01", mutate: func(_ *reviewFreezeCorpusManifestV1, _ map[string][]byte, resolver *reviewFreezeSyntheticExternalResolverV1) {
			delete(resolver.sources["golang.org/x/text/unicode/norm"], "unicode/norm/readwriter.go")
		}, want: "declared source 未被 build 选择"},
		{name: "R01 selected source digest drift", kind: "r01", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, _ *reviewFreezeSyntheticExternalResolverV1) {
			manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules[0].Packages[0].Sources[0].SHA256 = strings.Repeat("0", 64)
		}, want: "sha256"},
		{name: "R01 selected package import closure missing", kind: "r01", mutate: func(manifest *reviewFreezeCorpusManifestV1, _ map[string][]byte, resolver *reviewFreezeSyntheticExternalResolverV1) {
			resolver.sources["golang.org/x/text/unicode/norm"]["unicode/norm/normalize.go"] = []byte("package norm\n\nimport \"golang.org/x/text/transform\"\n")
			selected := &manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules[0].Packages[0]
			selected.Sources[0].SHA256 = reviewFreezeSHA256V1(resolver.sources[selected.ImportPath][selected.Sources[0].Path])
			selected.AllowedImports = []string{"golang.org/x/text/transform", "io"}
		}, want: "import 未闭包"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, files, syntheticResolver := reviewFreezeSyntheticBuildClosureFilesV1(t, tc.kind)
			tc.mutate(&manifest, files, syntheticResolver)
			var resolver reviewFreezeExternalPackageResolverV1 = syntheticResolver.resolve
			if syntheticResolver.disabled {
				resolver = nil
			}
			err := reviewFreezeValidateValidatorBuildClosureV1(manifest, reviewFreezeMapLoaderV1(files), resolver)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// TestW2ReviewFreezeValidatorBuildClosureV1GitAuxiliaryInputsAdversarial 阻断 package 非 Go 输入和 Module vendor 覆盖。
func TestW2ReviewFreezeValidatorBuildClosureV1GitAuxiliaryInputsAdversarial(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "assembly", path: "agent/tests/contract/w2r04approvalconsumption/entrypoint.s", want: "非 Go build input"},
		{name: "syso", path: "agent/tests/contract/w2r04approvalconsumption/payload.syso", want: "非 Go build input"},
		{name: "c header", path: "agent/tests/contract/w2r04approvalconsumption/payload.h", want: "非 Go build input"},
		{name: "vendor override", path: "agent/vendor/example.invalid/dependency/dependency.go", want: "vendor tree"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := reviewFreezeNewTestGitRepositoryV1(t)
			manifest, files, _ := reviewFreezeSyntheticBuildClosureFilesV1(t, "r04")
			manifestPath := "agent/tests/contract/w2r04approvalconsumption/testdata/manifest.json"
			for path, raw := range files {
				reviewFreezeWriteTestFileV1(t, repository.root, path, raw)
			}
			reviewFreezeWriteTestFileV1(t, repository.root, manifestPath, reviewFreezeMarshalV1(t, manifest))
			validCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "valid independent entrypoint")
			if err := reviewFreezeValidateGitValidatorContractV1(repository, validCommit, manifestPath); err != nil {
				t.Fatalf("valid independent Git closure rejected: %v", err)
			}

			reviewFreezeWriteTestFileV1(t, repository.root, tc.path, []byte("synthetic adversarial build input\n"))
			headCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "adversarial auxiliary build input")
			err := reviewFreezeValidateGitValidatorContractV1(repository, headCommit, manifestPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// reviewFreezeSyntheticExternalResolverV1 保存测试用外部 package build-selected source。
type reviewFreezeSyntheticExternalResolverV1 struct {
	sources  map[string]map[string][]byte
	disabled bool
}

// resolve 返回隔离副本，模拟受信 go list/module cache resolver 的 exact-set 输出。
func (resolver *reviewFreezeSyntheticExternalResolverV1) resolve(_ reviewFreezeValidatorExternalModuleV1, importPath string, _ reviewFreezeValidatorBuildEnvironmentV1, _ []byte) (map[string][]byte, error) {
	sources, ok := resolver.sources[importPath]
	if !ok {
		return nil, fmt.Errorf("missing selected package %s", importPath)
	}
	copySources := make(map[string][]byte, len(sources))
	for path, raw := range sources {
		copySources[path] = append([]byte(nil), raw...)
	}
	return copySources, nil
}

// reviewFreezeSyntheticBuildClosureV1 创建一份合法闭包及其 loader/resolver。
func reviewFreezeSyntheticBuildClosureV1(t *testing.T, kind string) (reviewFreezeCorpusManifestV1, reviewFreezeArtifactLoaderV1, reviewFreezeExternalPackageResolverV1) {
	manifest, files, resolver := reviewFreezeSyntheticBuildClosureFilesV1(t, kind)
	return manifest, reviewFreezeMapLoaderV1(files), resolver.resolve
}

// reviewFreezeSyntheticBuildClosureFilesV1 创建允许对抗用例重写的 R01/R04 纯内存构建闭包。
func reviewFreezeSyntheticBuildClosureFilesV1(t *testing.T, kind string) (reviewFreezeCorpusManifestV1, map[string][]byte, *reviewFreezeSyntheticExternalResolverV1) {
	t.Helper()
	files := map[string][]byte{}
	moduleRaw := []byte("module github.com/FigoGoo/Dora-Agent/agent\n\ngo 1.26\n\ntoolchain go1.26.3\n")
	goSumRaw := []byte{}
	entrypoint := reviewFreezeValidatorEntrypointV1{
		ExternalModules: []reviewFreezeValidatorExternalModuleV1{},
	}
	resolver := &reviewFreezeSyntheticExternalResolverV1{sources: make(map[string]map[string][]byte)}
	var sourceFiles map[string][]byte
	var corpusSchema string
	switch kind {
	case "r04":
		corpusSchema = "w2_r04_approval_consumption_manifest.v1"
		entrypoint.EntrypointID = "W2-R04.approval_consumption"
		entrypoint.PackageName = "w2r04approvalconsumption_test"
		entrypoint.PackagePath = "agent/tests/contract/w2r04approvalconsumption"
		entrypoint.PackagePattern = "./tests/contract/w2r04approvalconsumption"
		entrypoint.DependencyPolicy = "stdlib_only"
		entrypoint.TestEntrypoints = []string{"TestR04Consumption", "TestR04Manifest"}
		entrypoint.AllowedImports = []string{"testing"}
		sourceFiles = map[string][]byte{
			entrypoint.PackagePath + "/consumption_test.go": []byte("package w2r04approvalconsumption_test\n\nimport \"testing\"\n\nfunc TestR04Consumption(*testing.T) {}\n"),
			entrypoint.PackagePath + "/manifest_test.go":    []byte("package w2r04approvalconsumption_test\n\nimport \"testing\"\n\nfunc TestR04Manifest(*testing.T) {}\n"),
		}
	case "r01":
		corpusSchema = "w2_r01_contract_corpus_manifest.v1"
		moduleRaw = append(moduleRaw, []byte("\nrequire golang.org/x/text v0.34.0\n")...)
		goSumRaw = []byte("golang.org/x/text v0.34.0 h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=\n" +
			"golang.org/x/text v0.34.0/go.mod h1:homfLqTYRFyVYemLBFl5GgL/DWEiH5wcsQ5gSh1yziA=\n")
		entrypoint.EntrypointID = "W2-R01.graph_tool_result"
		entrypoint.PackageName = "w2r01_test"
		entrypoint.PackagePath = "agent/tests/contract/w2r01"
		entrypoint.PackagePattern = "./tests/contract/w2r01"
		entrypoint.DependencyPolicy = "x_text_nfc_only"
		entrypoint.TestEntrypoints = []string{"TestR01Manifest", "TestR01Vectors"}
		entrypoint.AllowedImports = []string{"golang.org/x/text/unicode/norm", "testing"}
		sourceFiles = map[string][]byte{
			entrypoint.PackagePath + "/manifest_test.go": []byte("package w2r01_test\n\nimport \"testing\"\n\nfunc TestR01Manifest(*testing.T) {}\n"),
			entrypoint.PackagePath + "/vectors_test.go":  []byte("package w2r01_test\n\nimport (\"golang.org/x/text/unicode/norm\"; \"testing\")\n\nfunc TestR01Vectors(*testing.T) { _ = norm.NFC }\n"),
		}
		normSources := map[string][]byte{
			"unicode/norm/normalize.go":  []byte("package norm\n\nimport \"unicode/utf8\"\n"),
			"unicode/norm/readwriter.go": []byte("package norm\n\nimport \"io\"\n"),
		}
		resolver.sources["golang.org/x/text/unicode/norm"] = normSources
		entrypoint.ExternalModules = []reviewFreezeValidatorExternalModuleV1{{
			ModulePath: "golang.org/x/text",
			Version:    "v0.34.0",
			ModuleSum:  "h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=",
			GoModSum:   "h1:homfLqTYRFyVYemLBFl5GgL/DWEiH5wcsQ5gSh1yziA=",
			Packages: []reviewFreezeValidatorExternalPackageV1{{
				ImportPath:     "golang.org/x/text/unicode/norm",
				AllowedImports: []string{"io", "unicode/utf8"},
				Sources: []reviewFreezeValidatorExternalSourceV1{
					{Path: "unicode/norm/normalize.go", SHA256: reviewFreezeSHA256V1(normSources["unicode/norm/normalize.go"])},
					{Path: "unicode/norm/readwriter.go", SHA256: reviewFreezeSHA256V1(normSources["unicode/norm/readwriter.go"])},
				},
			}},
		}}
	default:
		t.Fatalf("unknown synthetic build closure kind=%s", kind)
	}
	files["agent/go.mod"] = moduleRaw
	files["agent/go.sum"] = goSumRaw
	for path, raw := range sourceFiles {
		files[path] = raw
		entrypoint.DirectSources = append(entrypoint.DirectSources, reviewFreezeValidatorSourceV1{Path: path, SHA256: reviewFreezeSHA256V1(raw)})
	}
	sort.Slice(entrypoint.DirectSources, func(i, j int) bool { return entrypoint.DirectSources[i].Path < entrypoint.DirectSources[j].Path })
	manifest := reviewFreezeCorpusManifestV1{
		SchemaVersion:    corpusSchema,
		ValidatorSources: append([]reviewFreezeValidatorSourceV1(nil), entrypoint.DirectSources...),
		ValidatorBuildSources: []reviewFreezeValidatorSourceV1{
			{Path: "agent/go.mod", SHA256: reviewFreezeSHA256V1(moduleRaw)},
			{Path: "agent/go.sum", SHA256: reviewFreezeSHA256V1(goSumRaw)},
		},
		TargetTests: append([]string(nil), entrypoint.TestEntrypoints...),
		ValidatorBuildClosure: &reviewFreezeValidatorBuildClosureV1{
			SchemaVersion:    reviewFreezeValidatorBuildClosureSchemaV1,
			ActivationStatus: "candidate_unactivated",
			Environment:      reviewFreezeExpectedValidatorBuildEnvironmentV1(),
			Entrypoints:      []reviewFreezeValidatorEntrypointV1{entrypoint},
		},
	}
	return manifest, files, resolver
}

// reviewFreezeMapLoaderV1 从纯内存仓库返回隔离副本。
func reviewFreezeMapLoaderV1(files map[string][]byte) reviewFreezeArtifactLoaderV1 {
	return func(path string) ([]byte, error) {
		raw, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("missing %s", path)
		}
		return append([]byte(nil), raw...), nil
	}
}

// reviewFreezeRebindSyntheticSourceV1 同步重绑顶层与 entrypoint source 摘要，确保对抗用例穿过 raw SHA 门禁后命中语义校验。
func reviewFreezeRebindSyntheticSourceV1(manifest *reviewFreezeCorpusManifestV1, path string, raw []byte) {
	digest := reviewFreezeSHA256V1(raw)
	for index := range manifest.ValidatorSources {
		if manifest.ValidatorSources[index].Path == path {
			manifest.ValidatorSources[index].SHA256 = digest
		}
	}
	for entrypointIndex := range manifest.ValidatorBuildClosure.Entrypoints {
		for sourceIndex := range manifest.ValidatorBuildClosure.Entrypoints[entrypointIndex].DirectSources {
			if manifest.ValidatorBuildClosure.Entrypoints[entrypointIndex].DirectSources[sourceIndex].Path == path {
				manifest.ValidatorBuildClosure.Entrypoints[entrypointIndex].DirectSources[sourceIndex].SHA256 = digest
			}
		}
	}
}
