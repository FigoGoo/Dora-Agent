package reviewfreeze_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// reviewFreezeGoModReplaceDirectiveV1 匹配 go.mod 的单行或分组 replace 指令；Validator 构建闭包不允许本地或远端替换绕过 go.sum。
var reviewFreezeGoModReplaceDirectiveV1 = regexp.MustCompile(`(?m)^[\t ]*replace(?:[\t (]|$)`)

// reviewFreezeGoBuildConstraintDirectiveV1 同时拒绝现代与 legacy build constraint，防止已登记测试在受信 runner 上被选择性排除。
var reviewFreezeGoBuildConstraintDirectiveV1 = regexp.MustCompile(`(?m)^//go:build(?:[\t ]|$)|^//[\t ]+\+build(?:[\t ]|$)`)

// reviewFreezeValidateValidatorBuildSourcesV1 要求每个 Validator Go Module 精确绑定 go.mod/go.sum，且禁止未显式闭包的 replace。
func reviewFreezeValidateValidatorBuildSourcesV1(validatorSources, buildSources []reviewFreezeValidatorSourceV1, loader reviewFreezeArtifactLoaderV1) error {
	wantPaths, err := reviewFreezeExpectedValidatorBuildSourcePathsV1(validatorSources)
	if err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorExecutionShapeV1(validatorSources, loader); err != nil {
		return err
	}
	if len(buildSources) == 0 {
		return fmt.Errorf("validator_build_sources exact-set 不能为空")
	}
	if len(buildSources) != len(wantPaths) {
		return fmt.Errorf("validator_build_sources paths=%v want=%v", reviewFreezeValidatorSourcePathsV1(buildSources), wantPaths)
	}
	for index, source := range buildSources {
		if err := reviewFreezeValidateSafePathV1(source.Path, ""); err != nil {
			return fmt.Errorf("validator build source: %w", err)
		}
		if source.Path != wantPaths[index] {
			return fmt.Errorf("validator_build_sources 未排序、重复或不是 module exact-set=%v want=%v", reviewFreezeValidatorSourcePathsV1(buildSources), wantPaths)
		}
		raw, err := loader(source.Path)
		if err != nil {
			return fmt.Errorf("读取 validator build source %s: %w", source.Path, err)
		}
		if err := reviewFreezeCheckSHA256V1(raw, source.SHA256); err != nil {
			return fmt.Errorf("validator build source %s: %w", source.Path, err)
		}
		if pathpkg.Base(source.Path) == "go.mod" && reviewFreezeGoModReplaceDirectiveV1.Match(raw) {
			// replace 可把编译输入切到 go.sum 未覆盖的位置；v1 没有第三方源码闭包字段，因此必须失败关闭。
			return fmt.Errorf("validator build source %s 含未受信 replace", source.Path)
		}
	}
	return nil
}

// reviewFreezeValidateValidatorExecutionShapeV1 禁止 build constraint 与 TestMain 控制目标测试的选择或退出码。
func reviewFreezeValidateValidatorExecutionShapeV1(sources []reviewFreezeValidatorSourceV1, loader reviewFreezeArtifactLoaderV1) error {
	for _, source := range sources {
		raw, err := loader(source.Path)
		if err != nil {
			return fmt.Errorf("读取 validator source %s: %w", source.Path, err)
		}
		if reviewFreezeGoBuildConstraintDirectiveV1.Match(raw) {
			return fmt.Errorf("validator source %s 禁止 build constraint", source.Path)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), source.Path, raw, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("解析 validator source %s: %w", source.Path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "TestMain" {
				return fmt.Errorf("validator source %s 禁止 TestMain", source.Path)
			}
		}
	}
	return nil
}

// reviewFreezeExpectedValidatorBuildSourcePathsV1 从 Validator 源所属 Module 推导唯一允许的构建元数据 exact-set。
func reviewFreezeExpectedValidatorBuildSourcePathsV1(validatorSources []reviewFreezeValidatorSourceV1) ([]string, error) {
	moduleRoots := make(map[string]struct{})
	for _, source := range validatorSources {
		moduleRoot, err := reviewFreezeValidatorModuleRootV1(source.Path)
		if err != nil {
			return nil, err
		}
		moduleRoots[moduleRoot] = struct{}{}
	}
	paths := make([]string, 0, len(moduleRoots)*2)
	for moduleRoot := range moduleRoots {
		paths = append(paths, moduleRoot+"/go.mod", moduleRoot+"/go.sum")
	}
	sort.Strings(paths)
	return paths, nil
}

// reviewFreezeValidatorModuleRootV1 只允许三个独立 Go Module 的 contract test package 成为 Go Validator authority。
func reviewFreezeValidatorModuleRootV1(sourcePath string) (string, error) {
	for _, moduleRoot := range []string{"agent", "business", "worker"} {
		if strings.HasPrefix(sourcePath, moduleRoot+"/tests/contract/") && pathpkg.Ext(sourcePath) == ".go" {
			return moduleRoot, nil
		}
	}
	return "", fmt.Errorf("validator source 不属于受支持 Go Module 的 contract test package=%q", sourcePath)
}

// reviewFreezeValidatorSourcePathsV1 提取保持声明顺序的路径，供 exact-set 错误安全诊断。
func reviewFreezeValidatorSourcePathsV1(sources []reviewFreezeValidatorSourceV1) []string {
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		paths = append(paths, source.Path)
	}
	return paths
}

// reviewFreezeValidateHeadValidatorClosuresV1 对 HEAD 中每个候选或正式 Corpus 执行直接 Go package source 与 Module metadata 闭包。
// 它不声称覆盖 embed、同 Module 传递依赖、vendor、第三方源码或 toolchain；这些仍由 formal Freeze blocker 追踪。
func reviewFreezeValidateHeadValidatorClosuresV1(repository reviewFreezeGitRepositoryV1, headSHA string, manifest reviewFreezeManifestV1) error {
	manifestPaths := make(map[string]struct{})
	for _, gate := range manifest.Gates {
		for _, candidate := range gate.CandidateEvidence {
			manifestPaths[candidate.ContractManifestPath] = struct{}{}
		}
		if gate.Freeze != nil {
			manifestPaths[gate.Freeze.ContractManifestPath] = struct{}{}
		}
	}
	orderedPaths := make([]string, 0, len(manifestPaths))
	for manifestPath := range manifestPaths {
		orderedPaths = append(orderedPaths, manifestPath)
	}
	sort.Strings(orderedPaths)
	for _, manifestPath := range orderedPaths {
		if err := reviewFreezeValidateGitValidatorContractV1(repository, headSHA, manifestPath); err != nil {
			return fmt.Errorf("contract manifest %s validator build closure: %w", manifestPath, err)
		}
	}
	return nil
}

// reviewFreezeValidateGitValidatorContractV1 从一个不可变 Git commit 重建 Corpus 的直接 Go 源与 go.mod/go.sum 闭包。
func reviewFreezeValidateGitValidatorContractV1(repository reviewFreezeGitRepositoryV1, commitSHA, manifestPath string) error {
	raw, err := repository.readFile(commitSHA, manifestPath)
	if err != nil {
		return err
	}
	var manifest reviewFreezeCorpusManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		return err
	}
	loader := repository.loader(commitSHA)
	if err := reviewFreezeValidateValidatorSourcesV1(manifest.ValidatorSources, loader); err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorBuildSourcesV1(manifest.ValidatorSources, manifest.ValidatorBuildSources, loader); err != nil {
		return err
	}
	return reviewFreezeValidateGitValidatorPackageExactSetV1(repository, commitSHA, manifest.ValidatorSources)
}

// reviewFreezeValidateGitValidatorPackageExactSetV1 要求声明源等于各 Validator package 直接目录下全部 .go Git blob 的并集。
func reviewFreezeValidateGitValidatorPackageExactSetV1(repository reviewFreezeGitRepositoryV1, commitSHA string, sources []reviewFreezeValidatorSourceV1) error {
	directories := make(map[string]struct{})
	for _, source := range sources {
		directories[pathpkg.Dir(source.Path)] = struct{}{}
	}
	orderedDirectories := make([]string, 0, len(directories))
	for directory := range directories {
		orderedDirectories = append(orderedDirectories, directory)
	}
	sort.Strings(orderedDirectories)

	actual := make([]string, 0, len(sources))
	for _, directory := range orderedDirectories {
		paths, err := repository.listDirectGoSources(commitSHA, directory)
		if err != nil {
			return fmt.Errorf("枚举 validator package %s: %w", directory, err)
		}
		actual = append(actual, paths...)
	}
	sort.Strings(actual)
	declared := reviewFreezeValidatorSourcePathsV1(sources)
	if !reflect.DeepEqual(declared, actual) {
		return fmt.Errorf("validator package sources=%v want Git tree exact-set=%v", declared, actual)
	}
	return nil
}

// listDirectGoSources 从指定 commit 递归读取目录 tree，但只选择直接子级 .go，并拒绝 symlink、可执行 blob 与 submodule。
func (repository reviewFreezeGitRepositoryV1) listDirectGoSources(commitSHA, directory string) ([]string, error) {
	if err := reviewFreezeValidateSafePathV1(directory, ""); err != nil {
		return nil, err
	}
	raw, err := repository.git("ls-tree", "--full-tree", "-r", "-z", commitSHA, "--", directory)
	if err != nil {
		return nil, err
	}
	entries, err := reviewFreezeParseTreeEntriesV1(raw)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(directory, "/") + "/"
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.path, prefix) {
			return nil, fmt.Errorf("git ls-tree 返回越界路径=%q", entry.path)
		}
		remainder := strings.TrimPrefix(entry.path, prefix)
		if strings.Contains(remainder, "/") || pathpkg.Ext(remainder) != ".go" {
			continue
		}
		if err := reviewFreezeValidateRegularBlobEntryV1(entry); err != nil {
			return nil, err
		}
		paths = append(paths, entry.path)
	}
	sort.Strings(paths)
	return paths, nil
}

// TestW2ReviewFreezeManifestV1ValidatorBuildSources 覆盖 Module 元数据 exact-set、摘要与 replace 的通用失败关闭语义。
func TestW2ReviewFreezeManifestV1ValidatorBuildSources(t *testing.T) {
	validatorRaw := []byte("package contract_test\n")
	goModRaw := []byte("module example.invalid/agent\n\ngo 1.26\n")
	goSumRaw := []byte("example.invalid/dependency v1.0.0 h1:synthetic\n")
	values := map[string][]byte{
		"agent/tests/contract/validator_test.go": validatorRaw,
		"agent/go.mod":                           goModRaw,
		"agent/go.sum":                           goSumRaw,
	}
	loader := func(path string) ([]byte, error) {
		raw, ok := values[path]
		if !ok {
			return nil, fmt.Errorf("missing %s", path)
		}
		return raw, nil
	}
	validatorSources := []reviewFreezeValidatorSourceV1{{Path: "agent/tests/contract/validator_test.go", SHA256: reviewFreezeSHA256V1(validatorRaw)}}
	valid := []reviewFreezeValidatorSourceV1{
		{Path: "agent/go.mod", SHA256: reviewFreezeSHA256V1(goModRaw)},
		{Path: "agent/go.sum", SHA256: reviewFreezeSHA256V1(goSumRaw)},
	}
	if err := reviewFreezeValidateValidatorBuildSourcesV1(validatorSources, valid, loader); err != nil {
		t.Fatalf("valid validator build sources rejected: %v", err)
	}

	tests := []struct {
		name       string
		validators []reviewFreezeValidatorSourceV1
		build      []reviewFreezeValidatorSourceV1
		loader     reviewFreezeArtifactLoaderV1
		want       string
	}{
		{name: "missing", validators: validatorSources, want: "不能为空", loader: loader},
		{name: "missing go sum", validators: validatorSources, build: valid[:1], want: "paths=", loader: loader},
		{name: "unsorted", validators: validatorSources, build: []reviewFreezeValidatorSourceV1{valid[1], valid[0]}, want: "未排序", loader: loader},
		{name: "digest drift", validators: validatorSources, build: []reviewFreezeValidatorSourceV1{valid[0], {Path: "agent/go.sum", SHA256: reviewFreezeSHA256V1(goModRaw)}}, want: "sha256=", loader: loader},
		{name: "unsupported module", validators: []reviewFreezeValidatorSourceV1{{Path: "frontend/tests/contract/validator.go", SHA256: reviewFreezeSHA256V1(validatorRaw)}}, build: valid, want: "不属于受支持", loader: loader},
		{name: "replace", validators: validatorSources, build: []reviewFreezeValidatorSourceV1{{Path: "agent/go.mod", SHA256: reviewFreezeSHA256V1([]byte("module example.invalid/agent\nreplace example.invalid/a => ../a\n"))}, valid[1]}, want: "replace", loader: reviewFreezeOverlayLoaderV1(map[string][]byte{"agent/go.mod": []byte("module example.invalid/agent\nreplace example.invalid/a => ../a\n")}, loader)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reviewFreezeValidateValidatorBuildSourcesV1(tc.validators, tc.build, tc.loader)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// TestW2ReviewFreezeTransitionGitV1ValidatorBuildAdversarial 使用真实 Git commit 阻断未登记 Go 源、删除、mode 与 Module 元数据漂移。
func TestW2ReviewFreezeTransitionGitV1ValidatorBuildAdversarial(t *testing.T) {
	tests := []struct {
		name            string
		mutate          func(t *testing.T, root string)
		rewriteManifest bool
		want            string
	}{
		{name: "extra TestMain", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/testmain_test.go", []byte("package contract_test\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) { m.Run() }\n"))
		}, want: "exact-set"},
		{name: "extra init", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/init_test.go", []byte("package contract_test\n\nfunc init() {}\n"))
		}, want: "exact-set"},
		{name: "deleted source", mutate: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "agent/tests/contract/b_test.go")); err != nil {
				t.Fatal(err)
			}
		}, want: "不存在"},
		{name: "source symlink", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, "agent/tests/contract/b_test.go")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("a_test.go", path); err != nil {
				t.Fatal(err)
			}
		}, want: "100644"},
		{name: "source executable", mutate: func(t *testing.T, root string) {
			if err := os.Chmod(filepath.Join(root, "agent/tests/contract/b_test.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, want: "100644"},
		{name: "go mod drift", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/go.mod", []byte("module example.invalid/agent\n\ngo 1.26.1\n"))
		}, want: "sha256="},
		{name: "go sum drift", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/go.sum", []byte("example.invalid/dependency v1.0.1 h1:drift\n"))
		}, want: "sha256="},
		{name: "registered TestMain with rebound digest", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/b_test.go", []byte("package contract_test\n\nimport \"testing\"\n\nfunc TestMain(*testing.M) {}\n"))
		}, rewriteManifest: true, want: "TestMain"},
		{name: "registered build constraint with rebound digest", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/b_test.go", []byte("//go:build !linux\n\npackage contract_test\n\nfunc TestB() {}\n"))
		}, rewriteManifest: true, want: "build constraint"},
		{name: "go mod replace with rebound digest", mutate: func(t *testing.T, root string) {
			reviewFreezeWriteTestFileV1(t, root, "agent/go.mod", []byte("module example.invalid/agent\n\ngo 1.26\n\nreplace example.invalid/dependency => ../dependency\n"))
		}, rewriteManifest: true, want: "replace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := reviewFreezeNewTestGitRepositoryV1(t)
			manifestPath := reviewFreezeWriteTestValidatorClosureV1(t, repository.root)
			validCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "valid validator closure")
			if err := reviewFreezeValidateGitValidatorContractV1(repository, validCommit, manifestPath); err != nil {
				t.Fatalf("valid Git validator closure rejected: %v", err)
			}

			tc.mutate(t, repository.root)
			if tc.rewriteManifest {
				reviewFreezeWriteTestValidatorManifestV1(t, repository.root, manifestPath)
			}
			headCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "adversarial validator closure")
			err := reviewFreezeValidateGitValidatorContractV1(repository, headCommit, manifestPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// TestW2ReviewFreezeTransitionGitV1LegacyActivationToStrictValidatorClosure 证明旧 pre-formal Corpus 可在 trust-root 首次激活时迁移到严格直接源码与 Module metadata 闭包。
func TestW2ReviewFreezeTransitionGitV1LegacyActivationToStrictValidatorClosure(t *testing.T) {
	repository := reviewFreezeNewTestGitRepositoryV1(t)
	manifestPath := reviewFreezeWriteTestValidatorClosureV1(t, repository.root)
	strictRaw := reviewFreezeReadTestFileV1(t, repository.root, manifestPath)
	var strictCorpus reviewFreezeCorpusManifestV1
	if err := messageSetStrictDecodeV1(strictRaw, &strictCorpus); err != nil {
		t.Fatal(err)
	}
	legacyRaw := reviewFreezeMarshalV1(t, struct {
		SchemaVersion    string                          `json:"schema_version"`
		Files            []reviewFreezeCorpusFileV1      `json:"files"`
		FixtureIDs       []string                        `json:"fixture_ids"`
		ValidatorSources []reviewFreezeValidatorSourceV1 `json:"validator_sources"`
		VectorIDs        []string                        `json:"vector_ids"`
		TotalVectorCount int                             `json:"total_vector_count"`
		TargetTests      []string                        `json:"target_tests"`
	}{
		SchemaVersion: strictCorpus.SchemaVersion, Files: strictCorpus.Files, FixtureIDs: strictCorpus.FixtureIDs,
		ValidatorSources: strictCorpus.ValidatorSources, VectorIDs: strictCorpus.VectorIDs,
		TotalVectorCount: strictCorpus.TotalVectorCount, TargetTests: strictCorpus.TargetTests,
	})
	reviewFreezeWriteTestFileV1(t, repository.root, manifestPath, legacyRaw)

	baseManifest, _ := reviewFreezeLoadCurrentV1(t)
	for index := range baseManifest.Gates {
		gate := &baseManifest.Gates[index]
		gate.Status = "expansion_frozen"
		gate.CandidateEvidence = nil
		gate.Freeze = nil
		gate.ReopenException = nil
		gate.Blockers = []reviewFreezeBlockerV1{{
			Code:      strings.ReplaceAll(gate.Gate, "-", "_") + "_LEGACY_PENDING",
			Statement: "legacy pre-formal validator closure 尚未迁移",
		}}
	}
	baseManifest.Gates[0].CandidateEvidence = []reviewFreezeCandidateEvidenceV1{{
		Scope: "synthetic.validator", Coverage: "partial_candidate", ContractManifestPath: manifestPath,
		ContractManifestSHA256: reviewFreezeSHA256V1(legacyRaw), VectorIDs: []string{"V-001"}, TargetTests: []string{"TestA"},
	}}
	reviewFreezeWriteTestFileV1(t, repository.root, reviewFreezeManifestPathV1, reviewFreezeMarshalV1(t, baseManifest))
	baseCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "legacy pre-trust-root base")

	reviewFreezeWriteTestValidatorManifestV1(t, repository.root, manifestPath)
	strictRaw = reviewFreezeReadTestFileV1(t, repository.root, manifestPath)
	headManifest := baseManifest
	headManifest.Gates = append([]reviewFreezeGateV1(nil), baseManifest.Gates...)
	headManifest.Gates[0].CandidateEvidence = []reviewFreezeCandidateEvidenceV1{{
		Scope: "synthetic.validator", Coverage: "partial_candidate", ContractManifestPath: manifestPath,
		ContractManifestSHA256: reviewFreezeSHA256V1(strictRaw), VectorIDs: []string{"V-001"}, TargetTests: []string{"TestA"},
	}}
	reviewFreezeWriteTestFileV1(t, repository.root, reviewFreezeManifestPathV1, reviewFreezeMarshalV1(t, headManifest))
	reviewFreezeWriteTestFileV1(t, repository.root, reviewFreezeTransitionWorkflowPathV1, []byte("name: synthetic review freeze transition\n"))
	headCommit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "activate strict validator closure")

	if err := reviewFreezeValidateGitTransitionV1(repository, baseCommit, headCommit); err != nil {
		t.Fatalf("legacy pre-trust-root base -> strict head rejected: %v", err)
	}
}

// reviewFreezeWriteTestValidatorClosureV1 创建带两个直接 Go 源和 go.mod/go.sum 的最小 Validator package。
func reviewFreezeWriteTestValidatorClosureV1(t *testing.T, root string) string {
	t.Helper()
	reviewFreezeWriteTestFileV1(t, root, "agent/go.mod", []byte("module example.invalid/agent\n\ngo 1.26\n"))
	reviewFreezeWriteTestFileV1(t, root, "agent/go.sum", []byte("example.invalid/dependency v1.0.0 h1:synthetic\n"))
	reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/a_test.go", []byte("package contract_test\n\nfunc TestA() {}\n"))
	reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/b_test.go", []byte("package contract_test\n\nfunc TestB() {}\n"))
	manifestPath := "agent/tests/contract/testdata/synthetic/manifest.json"
	reviewFreezeWriteTestFileV1(t, root, "agent/tests/contract/testdata/synthetic/vectors.json", []byte("[\"V-001\"]\n"))
	reviewFreezeWriteTestValidatorManifestV1(t, root, manifestPath)
	return manifestPath
}

// reviewFreezeWriteTestValidatorManifestV1 重新计算测试仓库声明源摘要，供 replace 已绑定但仍失败关闭的用例使用。
func reviewFreezeWriteTestValidatorManifestV1(t *testing.T, root, manifestPath string) {
	t.Helper()
	sourcePaths := []string{"agent/tests/contract/a_test.go", "agent/tests/contract/b_test.go"}
	buildPaths := []string{"agent/go.mod", "agent/go.sum"}
	readSources := func(paths []string) []reviewFreezeValidatorSourceV1 {
		sources := make([]reviewFreezeValidatorSourceV1, 0, len(paths))
		for _, path := range paths {
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			sources = append(sources, reviewFreezeValidatorSourceV1{Path: path, SHA256: reviewFreezeSHA256V1(raw)})
		}
		return sources
	}
	manifestRaw := reviewFreezeMarshalV1(t, reviewFreezeCorpusManifestV1{
		SchemaVersion: "synthetic_validator_corpus.v1",
		Files: []reviewFreezeCorpusFileV1{{
			File: "vectors.json", SHA256: reviewFreezeSHA256V1(reviewFreezeReadTestFileV1(t, root, "agent/tests/contract/testdata/synthetic/vectors.json")), VectorCount: 1,
		}},
		FixtureIDs:            []string{"synthetic.open"},
		ValidatorSources:      readSources(sourcePaths),
		ValidatorBuildSources: readSources(buildPaths),
		VectorIDs:             []string{"V-001"},
		TotalVectorCount:      1,
		TargetTests:           []string{"TestA"},
	})
	reviewFreezeWriteTestFileV1(t, root, manifestPath, manifestRaw)
}

// reviewFreezeReadTestFileV1 从临时 Git 仓库读取测试 fixture，并将失败归属到调用方。
func reviewFreezeReadTestFileV1(t *testing.T, root, relative string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return raw
}
