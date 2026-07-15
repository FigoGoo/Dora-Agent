package reviewfreeze_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	reviewFreezeCompileInputSnapshotSchemaV1              = "w2_compile_input_snapshot.v1"
	reviewFreezeCompileInputSnapshotSourceTreeSchemaV1    = "w2_validator_source_tree.v1"
	reviewFreezeCompileInputSnapshotMaxRepositoryFileV1   = 8 << 20
	reviewFreezeCompileInputSnapshotMaxModuleCacheFileV1  = 64 << 20
	reviewFreezeCompileInputSnapshotRepositoryFileModeV1  = "100644"
	reviewFreezeCompileInputSnapshotModuleCacheFileModeV1 = "0644"
	reviewFreezeCompileInputSnapshotModuleDownloadRootV1  = "cache/download/golang.org/x/text/@v"
	reviewFreezeCompileInputSnapshotModuleMaterializedV1  = "golang.org/x/text@v0.34.0"
	reviewFreezeCompileInputSnapshotManifestSHA256V1      = "sha256:41e554faaec0f023e7542eb5d5cbe4638ccbb871c1179906e69215b52538f497"
)

// reviewFreezeCompileInputSnapshotV1 是 clean builder 在执行任何受管命令前后都必须重算的
// typed 输入元数据契约。它不携带文件原始字节，因而不能单独证明 sha256、size 或 git blob
// 与真实 material 一致；该职责保留给 Batch 3C material bundle verifier。
type reviewFreezeCompileInputSnapshotV1 struct {
	SchemaVersion    string                                         `json:"schema_version"`
	Subject          reviewFreezeCompileAttestationSubjectV1        `json:"subject"`
	ExternalModules  []reviewFreezeCompileAttestationModuleV1       `json:"external_modules"`
	Environment      reviewFreezeCompileAttestationEnvironmentV1    `json:"environment"`
	Toolchain        reviewFreezeCompileAttestationToolchainV1      `json:"toolchain"`
	ExecutionPolicy  reviewFreezeCompileInputSnapshotPolicyV1       `json:"execution_policy"`
	RepositoryFiles  []reviewFreezeCompileInputSnapshotRepoFileV1   `json:"repository_files"`
	ModuleCacheFiles []reviewFreezeCompileInputSnapshotModuleFileV1 `json:"module_cache_files"`
}

// reviewFreezeCompileInputSnapshotPolicyV1 只投影真正影响构建选择与执行边界的 argv/cwd
// 和 sandbox policy；命令输出摘要属于 run evidence，不得反向污染输入快照。
type reviewFreezeCompileInputSnapshotPolicyV1 struct {
	GoList                      reviewFreezeCompileInputSnapshotCommandV1 `json:"go_list"`
	Compile                     reviewFreezeCompileInputSnapshotCommandV1 `json:"compile"`
	BuildInfo                   reviewFreezeCompileInputSnapshotCommandV1 `json:"build_info"`
	Test                        reviewFreezeCompileInputSnapshotCommandV1 `json:"test"`
	SBOM                        reviewFreezeCompileInputSnapshotCommandV1 `json:"sbom"`
	SBOMGeneratorBinaryRef      reviewFreezeAttestationContentRefV1       `json:"sbom_generator_binary_ref"`
	RawToProjectionPolicy       string                                    `json:"raw_to_projection_policy"`
	SandboxPolicySHA256         string                                    `json:"sandbox_policy_sha256"`
	NetworkPolicy               string                                    `json:"network_policy"`
	SecretPolicy                string                                    `json:"secret_policy"`
	SourceFilesystemPolicy      string                                    `json:"source_filesystem_policy"`
	ModuleCacheFilesystemPolicy string                                    `json:"module_cache_filesystem_policy"`
}

type reviewFreezeCompileInputSnapshotCommandV1 struct {
	Argv []string `json:"argv"`
	CWD  string   `json:"cwd"`
}

type reviewFreezeCompileInputSnapshotRepoFileV1 struct {
	Path       string `json:"path"`
	Mode       string `json:"mode"`
	GitBlobSHA string `json:"git_blob_sha"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
}

type reviewFreezeCompileInputSnapshotModuleFileV1 struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type reviewFreezeCompileInputSnapshotSourceTreeV1 struct {
	SchemaVersion string                                             `json:"schema_version"`
	Files         []reviewFreezeCompileInputSnapshotSourceTreeFileV1 `json:"files"`
}

type reviewFreezeCompileInputSnapshotSourceTreeFileV1 struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// reviewFreezeCompileInputSnapshotManifestBindingV1 是已由 manifest 原始摘要选中的语义
// 投影项。Role 保留来源字段，防止只校验一组无来源含义的硬编码路径。
type reviewFreezeCompileInputSnapshotManifestBindingV1 struct {
	Role   string
	Path   string
	SHA256 string
}

// reviewFreezeValidateCompileInputSnapshotJSONV1 执行 bundle-side 元数据校验：严格 JSON、
// statement 交叉绑定、snapshot raw ref 与文件元数据投影。它只对 snapshot JSON 自身重算
// SHA/size；repository/module 原始字节和 git blob 仍须由 Batch 3C material bundle 复核。
func reviewFreezeValidateCompileInputSnapshotJSONV1(raw []byte, statement reviewFreezeValidatorCompileAttestationV1) error {
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationInputSnapshotMaxBytesV1 {
		return fmt.Errorf("compile input snapshot size=%d limit=%d", len(raw), reviewFreezeCompileAttestationInputSnapshotMaxBytesV1)
	}
	if !utf8.Valid(raw) {
		return fmt.Errorf("compile input snapshot 不是合法 UTF-8")
	}
	if err := reviewFreezeValidateCompileInputSnapshotRefsV1(raw, statement.BuilderRun); err != nil {
		return err
	}
	if err := reviewFreezeInspectCompileAttestationJSONV1(raw); err != nil {
		return fmt.Errorf("compile input snapshot JSON: %w", err)
	}

	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return err
	}
	if err := reviewFreezeRejectCompileAttestationNullV1(generic, "$snapshot"); err != nil {
		return err
	}
	if err := reviewFreezeRequireCompileAttestationFieldsV1(generic, reflect.TypeOf(reviewFreezeCompileInputSnapshotV1{}), "$snapshot"); err != nil {
		return err
	}

	var snapshot reviewFreezeCompileInputSnapshotV1
	strictDecoder := json.NewDecoder(bytes.NewReader(raw))
	strictDecoder.DisallowUnknownFields()
	if err := strictDecoder.Decode(&snapshot); err != nil {
		return err
	}
	var trailing any
	if err := strictDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("compile input snapshot trailing JSON")
	}
	canonicalRaw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("编码 compile input snapshot canonical JSON: %w", err)
	}
	if !bytes.Equal(raw, canonicalRaw) {
		return fmt.Errorf("compile input snapshot 必须使用 canonical JSON")
	}
	if err := reviewFreezeValidateCompileAttestationStatementV1(statement); err != nil {
		return fmt.Errorf("compile input snapshot statement: %w", err)
	}
	return reviewFreezeValidateCompileInputSnapshotV1(snapshot, statement)
}

func reviewFreezeValidateCompileInputSnapshotRefsV1(raw []byte, run reviewFreezeCompileAttestationBuilderRunV1) error {
	for _, item := range []struct {
		name string
		ref  reviewFreezeAttestationContentRefV1
	}{
		{name: "before", ref: run.InputSnapshotBeforeRef},
		{name: "after", ref: run.InputSnapshotAfterRef},
	} {
		if err := reviewFreezeValidateAttestationContentRefV1(
			item.ref,
			reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
			reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
			reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
			reviewFreezeCompileAttestationInputSnapshotMaxBytesV1,
			"input snapshot "+item.name,
		); err != nil {
			return err
		}
	}
	if !reflect.DeepEqual(run.InputSnapshotBeforeRef, run.InputSnapshotAfterRef) {
		return fmt.Errorf("compile input snapshot pre/post refs 不一致")
	}
	wantSHA := reviewFreezeSHA256V1(raw)
	wantSize := int64(len(raw))
	if run.InputSnapshotBeforeRef.SHA256 != wantSHA || run.InputSnapshotBeforeRef.SizeBytes != wantSize {
		return fmt.Errorf("compile input snapshot raw digest/size=%q/%d want=%q/%d", run.InputSnapshotBeforeRef.SHA256, run.InputSnapshotBeforeRef.SizeBytes, wantSHA, wantSize)
	}
	return nil
}

func reviewFreezeValidateCompileInputSnapshotV1(snapshot reviewFreezeCompileInputSnapshotV1, statement reviewFreezeValidatorCompileAttestationV1) error {
	if snapshot.SchemaVersion != reviewFreezeCompileInputSnapshotSchemaV1 {
		return fmt.Errorf("compile input snapshot schema_version=%q", snapshot.SchemaVersion)
	}
	if !reflect.DeepEqual(snapshot.Subject, statement.Subject) {
		return fmt.Errorf("compile input snapshot subject 与 statement 不一致")
	}
	if !reflect.DeepEqual(snapshot.ExternalModules, statement.ExternalModules) {
		return fmt.Errorf("compile input snapshot external_modules 与 statement 不一致")
	}
	if !reflect.DeepEqual(snapshot.Environment, statement.Environment) {
		return fmt.Errorf("compile input snapshot environment 与 statement 不一致")
	}
	if !reflect.DeepEqual(snapshot.Toolchain, statement.Toolchain) {
		return fmt.Errorf("compile input snapshot toolchain 与 statement 不一致")
	}
	wantPolicy := reviewFreezeCompileInputSnapshotPolicyFromStatementV1(statement)
	if !reflect.DeepEqual(snapshot.ExecutionPolicy, wantPolicy) {
		return fmt.Errorf("compile input snapshot execution_policy=%+v want=%+v", snapshot.ExecutionPolicy, wantPolicy)
	}
	if err := reviewFreezeValidateCompileInputSnapshotRepositoryFilesV1(snapshot.RepositoryFiles, statement.Subject); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileInputSnapshotModuleFilesV1(snapshot.ModuleCacheFiles, statement.ExternalModules); err != nil {
		return err
	}
	return nil
}

func reviewFreezeCompileInputSnapshotPolicyFromStatementV1(statement reviewFreezeValidatorCompileAttestationV1) reviewFreezeCompileInputSnapshotPolicyV1 {
	run := statement.BuilderRun
	return reviewFreezeCompileInputSnapshotPolicyV1{
		GoList:                      reviewFreezeCompileInputSnapshotCommandFromInvocationV1(run.GoListInvocation),
		Compile:                     reviewFreezeCompileInputSnapshotCommandFromInvocationV1(run.Compile.Invocation),
		BuildInfo:                   reviewFreezeCompileInputSnapshotCommandFromInvocationV1(run.Compile.BuildInfoInvocation),
		Test:                        reviewFreezeCompileInputSnapshotCommandFromInvocationV1(run.Test.Invocation),
		SBOM:                        reviewFreezeCompileInputSnapshotCommandFromInvocationV1(run.SBOM.Invocation),
		SBOMGeneratorBinaryRef:      run.SBOM.GeneratorBinaryRef,
		RawToProjectionPolicy:       statement.GoList.RawToProjectionPolicy,
		SandboxPolicySHA256:         run.Test.SandboxPolicySHA256,
		NetworkPolicy:               run.Test.NetworkPolicy,
		SecretPolicy:                run.Test.SecretPolicy,
		SourceFilesystemPolicy:      run.Test.SourceFilesystemPolicy,
		ModuleCacheFilesystemPolicy: run.Test.ModuleCacheFilesystemPolicy,
	}
}

func reviewFreezeCompileInputSnapshotCommandFromInvocationV1(invocation reviewFreezeCompileAttestationInvocationV1) reviewFreezeCompileInputSnapshotCommandV1 {
	return reviewFreezeCompileInputSnapshotCommandV1{
		Argv: append([]string(nil), invocation.Argv...),
		CWD:  invocation.CWD,
	}
}

func reviewFreezeCompileInputSnapshotRepositoryPathsV1() []string {
	paths := []string{"agent/tests/contract/testdata/w2_r01/manifest.json"}
	for _, binding := range reviewFreezeCompileInputSnapshotManifestBindingsV1() {
		paths = append(paths, binding.Path)
	}
	sort.Strings(paths)
	return paths
}

func reviewFreezeCompileInputSnapshotValidatorSourcePathsV1() []string {
	paths := make([]string, 0, 6)
	for _, binding := range reviewFreezeCompileInputSnapshotManifestBindingsV1() {
		if binding.Role == "validator_source" {
			paths = append(paths, binding.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// reviewFreezeCompileInputSnapshotManifestBindingsV1 是当前 R01 manifest 四组文件引用
// （files/design_sources/validator_sources/validator_build_sources）的 typed 语义投影。
// 只有 ContractManifestSHA256 命中上面的 manifest 注册摘要时才能使用该投影。
func reviewFreezeCompileInputSnapshotManifestBindingsV1() []reviewFreezeCompileInputSnapshotManifestBindingV1 {
	return []reviewFreezeCompileInputSnapshotManifestBindingV1{
		{Role: "validator_build_source", Path: "agent/go.mod", SHA256: "sha256:498332d1fc9199de3a1d008bd3057e072b2993f261ffa4d9ece319d5b41cb4bf"},
		{Role: "validator_build_source", Path: "agent/go.sum", SHA256: "sha256:fcd462279ba6e0207c5e6de66443c40c6069815b3c3bd08b491a23b2d10d9c73"},
		{Role: "corpus_file", Path: "agent/tests/contract/testdata/w2_r01/graph_tool_result_v1.json", SHA256: "sha256:0217f80689927017541fa07e7f78fd40eede77ff82bed94c6f8c30172ec5a63a"},
		{Role: "corpus_file", Path: "agent/tests/contract/testdata/w2_r01/tool_receipt_v1.json", SHA256: "sha256:abe0ebd3c99e11bf3d7d0e9d59a68ceca0db0f087893a76eb177099b22ff0d89"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/approval_continuation_parent_receipt_v1.go", SHA256: "sha256:b271b6a7f3a3a1b781c9005cdc12c2474e363878776d5667e8d682c346692aed"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/graph_tool_result_v1.go", SHA256: "sha256:3a72531ec8d475ff50516153a1fb38e1be856cb452c07379676aef08505f7863"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/graph_tool_result_v1_corpus_test.go", SHA256: "sha256:7aba1bf17641f5779b4854f881ec13daa411f800a343bc12b27af0380a8f056f"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/tool_receipt_v1.go", SHA256: "sha256:ad9f3125a6986f03875305268f558859d747bdce1a11fb51de9f77f1e328b968"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/tool_receipt_v1_corpus_test.go", SHA256: "sha256:6729632378086fcab3218e8cf63f367920eb15824e90f3dcf1fd35d19859369f"},
		{Role: "validator_source", Path: "agent/tests/contract/w2r01/validator_support_v1.go", SHA256: "sha256:cb67e7e505cd17cb1a762b01bf0368fe9f9fa16cc09905f9148c25e4613d3647"},
		{Role: "design_source", Path: "docs/design/agent/graph-tool-result-receipt-contract-v1.md", SHA256: "sha256:9cfad34de8b40cd958ae99116c9314579235ff1c7eb9b8426629e368901fdee5"},
		{Role: "design_source", Path: "docs/design/agent/runner-session-lane-review-v1.md", SHA256: "sha256:dbc1a01a3054ca6e963981ba231ebaee81eb60ac16887c3e5aeeaea078e5bccb"},
		{Role: "design_source", Path: "docs/design/cross-module/aigc-contract-catalog.md", SHA256: "sha256:d7a92ad2c7d6ebb4173a880f2d86a8f6270be7a61c52e47e50502f41c6b07151"},
	}
}

func reviewFreezeValidateCompileInputSnapshotRepositoryFilesV1(files []reviewFreezeCompileInputSnapshotRepoFileV1, subject reviewFreezeCompileAttestationSubjectV1) error {
	wantPaths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	if files == nil || len(files) != len(wantPaths) {
		return fmt.Errorf("compile input snapshot repository_files exact-set 长度=%d want=%d", len(files), len(wantPaths))
	}
	paths := make([]string, len(files))
	byPath := make(map[string]reviewFreezeCompileInputSnapshotRepoFileV1, len(files))
	lastPath := ""
	zeroGitSHA := strings.Repeat("0", 40)
	for index, file := range files {
		if err := reviewFreezeValidateSafePathV1(file.Path, ""); err != nil {
			return fmt.Errorf("compile input snapshot repository file: %w", err)
		}
		if file.Path <= lastPath {
			return fmt.Errorf("compile input snapshot repository_files 未排序或重复=%q", file.Path)
		}
		if file.Mode != reviewFreezeCompileInputSnapshotRepositoryFileModeV1 {
			return fmt.Errorf("compile input snapshot repository file mode=%q path=%q", file.Mode, file.Path)
		}
		if !reviewFreezeGitSHA1V1.MatchString(file.GitBlobSHA) || file.GitBlobSHA == zeroGitSHA {
			return fmt.Errorf("compile input snapshot repository git_blob_sha 非法 path=%q", file.Path)
		}
		if !reviewFreezePrefixedSHA256V1.MatchString(file.SHA256) || file.SHA256 == reviewFreezeSHA256V1(nil) {
			return fmt.Errorf("compile input snapshot repository sha256 非法 path=%q", file.Path)
		}
		if file.SizeBytes <= 0 || file.SizeBytes > reviewFreezeCompileInputSnapshotMaxRepositoryFileV1 {
			return fmt.Errorf("compile input snapshot repository size=%d path=%q", file.SizeBytes, file.Path)
		}
		paths[index] = file.Path
		byPath[file.Path] = file
		lastPath = file.Path
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		return fmt.Errorf("compile input snapshot repository_files exact-set=%v want=%v", paths, wantPaths)
	}
	if subject.ContractManifestSHA256 != reviewFreezeCompileInputSnapshotManifestSHA256V1 {
		return fmt.Errorf("compile input snapshot contract manifest digest=%q want registered=%q", subject.ContractManifestSHA256, reviewFreezeCompileInputSnapshotManifestSHA256V1)
	}
	for _, binding := range []struct {
		path string
		want string
	}{
		{path: "agent/go.mod", want: subject.GoModSHA256},
		{path: "agent/go.sum", want: subject.GoSumSHA256},
		{path: subject.ContractManifestPath, want: subject.ContractManifestSHA256},
	} {
		if byPath[binding.path].SHA256 != binding.want {
			return fmt.Errorf("compile input snapshot repository %s digest=%q want=%q", binding.path, byPath[binding.path].SHA256, binding.want)
		}
	}
	for _, binding := range reviewFreezeCompileInputSnapshotManifestBindingsV1() {
		file, exists := byPath[binding.Path]
		if !exists {
			return fmt.Errorf("compile input snapshot manifest %s 缺失=%q", binding.Role, binding.Path)
		}
		if file.SHA256 != binding.SHA256 {
			return fmt.Errorf("compile input snapshot manifest %s digest=%q path=%q want=%q", binding.Role, file.SHA256, binding.Path, binding.SHA256)
		}
	}

	sourceTreeSHA, err := reviewFreezeCompileInputSnapshotSourceTreeSHA256V1(byPath)
	if err != nil {
		return err
	}
	if sourceTreeSHA != subject.ValidatorSourceTreeSHA256 {
		return fmt.Errorf("compile input snapshot validator source tree digest=%q want=%q", sourceTreeSHA, subject.ValidatorSourceTreeSHA256)
	}
	return nil
}

func reviewFreezeCompileInputSnapshotSourceTreeSHA256V1(files map[string]reviewFreezeCompileInputSnapshotRepoFileV1) (string, error) {
	projection := reviewFreezeCompileInputSnapshotSourceTreeV1{
		SchemaVersion: reviewFreezeCompileInputSnapshotSourceTreeSchemaV1,
		Files:         make([]reviewFreezeCompileInputSnapshotSourceTreeFileV1, 0, 6),
	}
	for _, path := range reviewFreezeCompileInputSnapshotValidatorSourcePathsV1() {
		file, exists := files[path]
		if !exists {
			return "", fmt.Errorf("compile input snapshot validator source 缺失=%q", path)
		}
		projection.Files = append(projection.Files, reviewFreezeCompileInputSnapshotSourceTreeFileV1{
			Path:      file.Path,
			Mode:      file.Mode,
			SHA256:    file.SHA256,
			SizeBytes: file.SizeBytes,
		})
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("编码 validator source tree projection: %w", err)
	}
	return reviewFreezeSHA256V1(raw), nil
}

func reviewFreezeCompileInputSnapshotModulePathsV1() []string {
	return []string{
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.mod",
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.zip",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/transform/transform.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/composition.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/forminfo.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/input.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/iter.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/normalize.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/readwriter.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/tables15.0.0.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/transform.go",
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/trie.go",
	}
}

func reviewFreezeValidateCompileInputSnapshotModuleFilesV1(files []reviewFreezeCompileInputSnapshotModuleFileV1, modules []reviewFreezeCompileAttestationModuleV1) error {
	wantPaths := reviewFreezeCompileInputSnapshotModulePathsV1()
	if files == nil || len(files) != len(wantPaths) {
		return fmt.Errorf("compile input snapshot module_cache_files exact-set 长度=%d want=%d", len(files), len(wantPaths))
	}
	paths := make([]string, len(files))
	byPath := make(map[string]reviewFreezeCompileInputSnapshotModuleFileV1, len(files))
	lastPath := ""
	for index, file := range files {
		if err := reviewFreezeValidateSafePathV1(file.Path, ""); err != nil {
			return fmt.Errorf("compile input snapshot module cache file: %w", err)
		}
		if file.Path <= lastPath {
			return fmt.Errorf("compile input snapshot module_cache_files 未排序或重复=%q", file.Path)
		}
		if file.Mode != reviewFreezeCompileInputSnapshotModuleCacheFileModeV1 {
			return fmt.Errorf("compile input snapshot module cache mode=%q path=%q", file.Mode, file.Path)
		}
		if !reviewFreezePrefixedSHA256V1.MatchString(file.SHA256) || file.SHA256 == reviewFreezeSHA256V1(nil) {
			return fmt.Errorf("compile input snapshot module cache sha256 非法 path=%q", file.Path)
		}
		if file.SizeBytes <= 0 || file.SizeBytes > reviewFreezeCompileInputSnapshotMaxModuleCacheFileV1 {
			return fmt.Errorf("compile input snapshot module cache size=%d path=%q", file.SizeBytes, file.Path)
		}
		paths[index] = file.Path
		byPath[file.Path] = file
		lastPath = file.Path
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		return fmt.Errorf("compile input snapshot module_cache_files exact-set=%v want=%v", paths, wantPaths)
	}
	if len(modules) != 1 {
		return fmt.Errorf("compile input snapshot external_modules exact-set 长度=%d want=1", len(modules))
	}
	module := modules[0]
	bindings := []struct {
		name string
		path string
		sha  string
		size int64
	}{
		{name: "module .mod", path: reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.mod", sha: module.GoModSHA256, size: module.GoModSizeBytes},
		{name: "module zip", path: reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.zip", sha: module.ZipSHA256, size: module.ZipSizeBytes},
		{name: "materialized root go.mod", path: reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod", sha: module.ZipRootGoModSHA256, size: module.ZipRootGoModSizeBytes},
	}
	for _, binding := range bindings {
		file := byPath[binding.path]
		if file.SHA256 != binding.sha || file.SizeBytes != binding.size {
			return fmt.Errorf("compile input snapshot %s digest/size=%q/%d want=%q/%d", binding.name, file.SHA256, file.SizeBytes, binding.sha, binding.size)
		}
	}

	selectedSourceCount := 0
	for _, selectedPackage := range module.Packages {
		for _, source := range selectedPackage.Sources {
			path := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/" + source.Path
			file, exists := byPath[path]
			if !exists {
				return fmt.Errorf("compile input snapshot selected module source 缺失=%q", path)
			}
			if file.SHA256 != source.SHA256 {
				return fmt.Errorf("compile input snapshot selected module source digest=%q path=%q want=%q", file.SHA256, path, source.SHA256)
			}
			selectedSourceCount++
		}
	}
	if selectedSourceCount != 10 {
		return fmt.Errorf("compile input snapshot selected module source 数量=%d want=10", selectedSourceCount)
	}
	return nil
}

func reviewFreezeCompileInputSnapshotFixtureRepositoryFilesV1(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) []reviewFreezeCompileInputSnapshotRepoFileV1 {
	t.Helper()
	paths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	files := make([]reviewFreezeCompileInputSnapshotRepoFileV1, len(paths))
	byPath := make(map[string]reviewFreezeCompileInputSnapshotRepoFileV1, len(paths))
	manifestBindings := make(map[string]string)
	for _, binding := range reviewFreezeCompileInputSnapshotManifestBindingsV1() {
		manifestBindings[binding.Path] = binding.SHA256
	}
	for index, path := range paths {
		raw := []byte("compile input snapshot repository file: " + path)
		file := reviewFreezeCompileInputSnapshotRepoFileV1{
			Path:       path,
			Mode:       reviewFreezeCompileInputSnapshotRepositoryFileModeV1,
			GitBlobSHA: fmt.Sprintf("%040x", index+1),
			SHA256:     reviewFreezeSHA256V1(raw),
			SizeBytes:  int64(len(raw)),
		}
		if manifestSHA, exists := manifestBindings[path]; exists {
			file.SHA256 = manifestSHA
		}
		if path == statement.Subject.ContractManifestPath {
			file.SHA256 = reviewFreezeCompileInputSnapshotManifestSHA256V1
		}
		files[index] = file
		byPath[path] = file
	}
	statement.Subject.GoModSHA256 = byPath["agent/go.mod"].SHA256
	statement.Subject.GoSumSHA256 = byPath["agent/go.sum"].SHA256
	statement.Subject.ContractManifestSHA256 = byPath[statement.Subject.ContractManifestPath].SHA256
	sourceTreeSHA, err := reviewFreezeCompileInputSnapshotSourceTreeSHA256V1(byPath)
	if err != nil {
		t.Fatalf("build source tree fixture: %v", err)
	}
	statement.Subject.ValidatorSourceTreeSHA256 = sourceTreeSHA
	return files
}

func reviewFreezeCompileInputSnapshotFixtureModuleFilesV1(t *testing.T, module reviewFreezeCompileAttestationModuleV1) []reviewFreezeCompileInputSnapshotModuleFileV1 {
	t.Helper()
	selectedSources := make(map[string]string)
	for _, selectedPackage := range module.Packages {
		for _, source := range selectedPackage.Sources {
			selectedSources[reviewFreezeCompileInputSnapshotModuleMaterializedV1+"/"+source.Path] = source.SHA256
		}
	}
	paths := reviewFreezeCompileInputSnapshotModulePathsV1()
	files := make([]reviewFreezeCompileInputSnapshotModuleFileV1, len(paths))
	for index, path := range paths {
		file := reviewFreezeCompileInputSnapshotModuleFileV1{
			Path:      path,
			Mode:      reviewFreezeCompileInputSnapshotModuleCacheFileModeV1,
			SizeBytes: int64(1024 + index),
		}
		switch path {
		case reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.mod":
			file.SHA256, file.SizeBytes = module.GoModSHA256, module.GoModSizeBytes
		case reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.zip":
			file.SHA256, file.SizeBytes = module.ZipSHA256, module.ZipSizeBytes
		case reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod":
			file.SHA256, file.SizeBytes = module.ZipRootGoModSHA256, module.ZipRootGoModSizeBytes
		default:
			sha, exists := selectedSources[path]
			if !exists {
				t.Fatalf("module file fixture path 未绑定 selected source=%q", path)
			}
			file.SHA256 = sha
		}
		files[index] = file
	}
	return files
}

func reviewFreezeCompileInputSnapshotFixtureV1(t *testing.T) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
	t.Helper()
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	repositoryFiles := reviewFreezeCompileInputSnapshotFixtureRepositoryFilesV1(t, &statement)
	snapshot := reviewFreezeCompileInputSnapshotV1{
		SchemaVersion:    reviewFreezeCompileInputSnapshotSchemaV1,
		Subject:          statement.Subject,
		ExternalModules:  statement.ExternalModules,
		Environment:      statement.Environment,
		Toolchain:        statement.Toolchain,
		ExecutionPolicy:  reviewFreezeCompileInputSnapshotPolicyFromStatementV1(statement),
		RepositoryFiles:  repositoryFiles,
		ModuleCacheFiles: reviewFreezeCompileInputSnapshotFixtureModuleFilesV1(t, statement.ExternalModules[0]),
	}
	raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
	return raw, statement
}

func reviewFreezeCompileInputSnapshotFixtureMarshalV1(t *testing.T, snapshot reviewFreezeCompileInputSnapshotV1) []byte {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal compile input snapshot fixture: %v", err)
	}
	return raw
}

func reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw []byte, statement *reviewFreezeValidatorCompileAttestationV1) {
	ref := reviewFreezeAttestationContentRefV1{
		RefSchemaVersion:     reviewFreezeAttestationContentRefSchemaV1,
		ArtifactKind:         reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
		ContentSchemaVersion: reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
		MediaType:            reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
		SHA256:               reviewFreezeSHA256V1(raw),
		SizeBytes:            int64(len(raw)),
	}
	statement.BuilderRun.InputSnapshotBeforeRef = ref
	statement.BuilderRun.InputSnapshotAfterRef = ref
}

func reviewFreezeCompileInputSnapshotFixtureDecodeV1(t *testing.T, raw []byte) reviewFreezeCompileInputSnapshotV1 {
	t.Helper()
	var snapshot reviewFreezeCompileInputSnapshotV1
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode compile input snapshot fixture: %v", err)
	}
	return snapshot
}

func TestW2ReviewFreezeCompileInputSnapshotV1(t *testing.T) {
	raw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(raw, statement); err != nil {
		t.Fatalf("valid compile input snapshot rejected: %v", err)
	}
}

func TestW2ReviewFreezeCompileInputSnapshotAdversarialV1(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*reviewFreezeCompileInputSnapshotV1, *reviewFreezeValidatorCompileAttestationV1)
		preserveRefs bool
		mutateRaw    func([]byte) []byte
		want         string
	}{
		{
			name: "repository path escape",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.RepositoryFiles[0].Path = "../agent/go.mod"
			},
			want: "不安全路径",
		},
		{
			name: "repository digest mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.RepositoryFiles[0].SHA256 = reviewFreezeSHA256V1([]byte("other go.mod"))
			},
			want: "agent/go.mod digest",
		},
		{
			name: "runtime corpus missing",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.RepositoryFiles = append(snapshot.RepositoryFiles[:2], snapshot.RepositoryFiles[3:]...)
			},
			want: "repository_files exact-set 长度",
		},
		{
			name: "extra repository material",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.RepositoryFiles = append(snapshot.RepositoryFiles, reviewFreezeCompileInputSnapshotRepoFileV1{
					Path:       "zz-extra/input.txt",
					Mode:       reviewFreezeCompileInputSnapshotRepositoryFileModeV1,
					GitBlobSHA: strings.Repeat("c", 40),
					SHA256:     reviewFreezeSHA256V1([]byte("extra input")),
					SizeBytes:  11,
				})
			},
			want: "repository_files exact-set 长度",
		},
		{
			name: "manifest design source digest mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				for index := range snapshot.RepositoryFiles {
					if snapshot.RepositoryFiles[index].Path == "docs/design/agent/runner-session-lane-review-v1.md" {
						snapshot.RepositoryFiles[index].SHA256 = reviewFreezeSHA256V1([]byte("other design source"))
						return
					}
				}
			},
			want: "manifest design_source digest",
		},
		{
			name: "manifest validator source digest mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.RepositoryFiles[5].SHA256 = reviewFreezeSHA256V1([]byte("other validator source"))
			},
			want: "manifest validator_source digest",
		},
		{
			name: "validator source tree mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, statement *reviewFreezeValidatorCompileAttestationV1) {
				otherTree := reviewFreezeSHA256V1([]byte("other validator source tree"))
				snapshot.Subject.ValidatorSourceTreeSHA256 = otherTree
				statement.Subject.ValidatorSourceTreeSHA256 = otherTree
			},
			want: "validator source tree digest",
		},
		{
			name: "module zip mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.ModuleCacheFiles[1].SHA256 = reviewFreezeSHA256V1([]byte("other module zip"))
			},
			want: "module zip digest/size",
		},
		{
			name: "command projection mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.ExecutionPolicy.Compile.Argv = append(snapshot.ExecutionPolicy.Compile.Argv, "-race")
			},
			want: "execution_policy",
		},
		{
			name: "SBOM generator binary mismatch",
			mutate: func(snapshot *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {
				snapshot.ExecutionPolicy.SBOMGeneratorBinaryRef.SHA256 = reviewFreezeSHA256V1([]byte("other SBOM generator"))
			},
			want: "execution_policy",
		},
		{
			name: "before after ref mismatch",
			mutate: func(_ *reviewFreezeCompileInputSnapshotV1, statement *reviewFreezeValidatorCompileAttestationV1) {
				statement.BuilderRun.InputSnapshotAfterRef.SHA256 = reviewFreezeSHA256V1([]byte("other snapshot"))
			},
			preserveRefs: true,
			want:         "pre/post refs",
		},
		{
			name:         "raw ref mismatch",
			mutate:       func(_ *reviewFreezeCompileInputSnapshotV1, _ *reviewFreezeValidatorCompileAttestationV1) {},
			preserveRefs: true,
			mutateRaw: func(raw []byte) []byte {
				return append(raw, '\n')
			},
			want: "raw digest/size",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
			snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, raw)
			test.mutate(&snapshot, &statement)
			raw = reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
			if !test.preserveRefs {
				reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
			}
			if test.mutateRaw != nil {
				raw = test.mutateRaw(raw)
			}
			err := reviewFreezeValidateCompileInputSnapshotJSONV1(raw, statement)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileInputSnapshotStrictJSONV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unknown locator", mutate: func(object map[string]any) {
			object["locator"] = "file:///tmp/input-snapshot.json"
		}},
		{name: "top level case alias", mutate: func(object map[string]any) {
			object["SCHEMA_VERSION"] = object["schema_version"]
		}},
		{name: "nested case alias", mutate: func(object map[string]any) {
			repositoryFiles := object["repository_files"].([]any)
			firstFile := repositoryFiles[0].(map[string]any)
			firstFile["SHA256"] = firstFile["sha256"]
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatalf("decode snapshot object: %v", err)
			}
			test.mutate(object)
			mutated, err := json.Marshal(object)
			if err != nil {
				t.Fatalf("marshal snapshot with unknown field: %v", err)
			}
			reviewFreezeCompileInputSnapshotFixtureBindRefsV1(mutated, &statement)
			err = reviewFreezeValidateCompileInputSnapshotJSONV1(mutated, statement)
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("case=%s error=%v", test.name, err)
			}
		})
	}
}

func TestW2ReviewFreezeCompileInputSnapshotCanonicalJSONV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, []byte) []byte
	}{
		{name: "trailing whitespace", mutate: func(_ *testing.T, raw []byte) []byte {
			return append(raw, '\n')
		}},
		{name: "reordered fields", mutate: func(t *testing.T, raw []byte) []byte {
			t.Helper()
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatalf("decode snapshot object: %v", err)
			}
			reordered, err := json.Marshal(object)
			if err != nil {
				t.Fatalf("marshal reordered snapshot: %v", err)
			}
			return reordered
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
			raw = test.mutate(t, raw)
			reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
			err := reviewFreezeValidateCompileInputSnapshotJSONV1(raw, statement)
			if err == nil || !strings.Contains(err.Error(), "canonical JSON") {
				t.Fatalf("error=%v want canonical JSON rejection", err)
			}
		})
	}
}
