package reviewfreeze_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	reviewFreezeValidatorCompileAttestationSchemaV1            = "w2_validator_compile_attestation_run_statement.v1"
	reviewFreezeAttestationContentRefSchemaV1                  = "w2_attestation_content_ref.v1"
	reviewFreezeGoListProjectionSchemaV1                       = "w2_go_list_canonical_projection.v1"
	reviewFreezeGoListRawToProjectionPolicyV1                  = "w2_go_list_raw_to_projection.v1"
	reviewFreezeCompileAttestationBuildInfoProjectionSchemaV1  = "w2_go_build_info_projection.v1"
	reviewFreezeCompileAttestationEntrypointV1                 = "W2-R01.graph_tool_result"
	reviewFreezeCompileAttestationPackagePathV1                = "agent/tests/contract/w2r01"
	reviewFreezeCompileAttestationPackagePatternV1             = "./tests/contract/w2r01"
	reviewFreezeCompileAttestationTargetImportV1               = "github.com/FigoGoo/Dora-Agent/agent/tests/contract/w2r01"
	reviewFreezeCompileAttestationModulePathV1                 = "github.com/FigoGoo/Dora-Agent/agent"
	reviewFreezeCompileAttestationLogicalCWDV1                 = "/workspace/agent"
	reviewFreezeCompileAttestationBinaryPathV1                 = "/out/w2r01.test"
	reviewFreezeCompileAttestationMaxJSONBytesV1               = 8 << 20
	reviewFreezeCompileAttestationMaxJSONDepthV1               = 64
	reviewFreezeCompileAttestationMaxGoArchiveV1               = 256 << 20
	reviewFreezeCompileAttestationBuildClosureMaxBytesV1       = 8 << 20
	reviewFreezeCompileAttestationInputSnapshotMaxBytesV1      = 16 << 20
	reviewFreezeCompileAttestationGoListRawMaxBytesV1          = 64 << 20
	reviewFreezeCompileAttestationArtifactMaxBytesV1           = 64 << 20
	reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1       = 64 << 10
	reviewFreezeCompileAttestationSBOMGeneratorMaxBytesV1      = 64 << 20
	reviewFreezeCompileAttestationSBOMRawMaxBytesV1            = 16 << 20
	reviewFreezeCompileAttestationBuildClosureArtifactKindV1   = "validator_build_closure"
	reviewFreezeCompileAttestationBuildClosureContentSchemaV1  = "w2_validator_build_closure.v2"
	reviewFreezeCompileAttestationBuildClosureMediaTypeV1      = "application/vnd.dora.validator-build-closure+json"
	reviewFreezeCompileAttestationInputSnapshotArtifactKindV1  = "compile_input_snapshot"
	reviewFreezeCompileAttestationInputSnapshotContentSchemaV1 = "w2_compile_input_snapshot.v1"
	reviewFreezeCompileAttestationInputSnapshotMediaTypeV1     = "application/vnd.dora.compile-input-snapshot+json"
	reviewFreezeCompileAttestationGoListRawArtifactKindV1      = "go_list_stdout"
	reviewFreezeCompileAttestationGoListRawContentSchemaV1     = "go1.26.3_go_list_json_stream.v1"
	reviewFreezeCompileAttestationGoListRawMediaTypeV1         = "application/vnd.dora.go-list-stream+json"
	reviewFreezeCompileAttestationArtifactKindV1               = "compiled_test_binary"
	reviewFreezeCompileAttestationArtifactContentSchemaV1      = "go1.26.3_linux_amd64_test_binary.v1"
	reviewFreezeCompileAttestationArtifactMediaTypeV1          = "application/vnd.dora.go-test-binary"
	reviewFreezeCompileAttestationBuildInfoRawArtifactKindV1   = "go_build_info_stdout"
	reviewFreezeCompileAttestationBuildInfoRawContentSchemaV1  = "go1.26.3_version_m_text.v1"
	reviewFreezeCompileAttestationBuildInfoRawMediaTypeV1      = "text/plain; charset=utf-8"
	reviewFreezeCompileAttestationSBOMGeneratorArtifactKindV1  = "sbom_generator_binary"
	reviewFreezeCompileAttestationSBOMGeneratorContentSchemaV1 = "dora_w2_sbom_v1_linux_amd64_binary.v1"
	reviewFreezeCompileAttestationSBOMGeneratorMediaTypeV1     = "application/vnd.dora.sbom-generator"
	reviewFreezeCompileAttestationSBOMRawArtifactKindV1        = "cyclonedx_sbom"
	reviewFreezeCompileAttestationSBOMRawContentSchemaV1       = "cyclonedx_1_6_dora_canonical.v1"
	reviewFreezeCompileAttestationSBOMRawMediaTypeV1           = "application/vnd.cyclonedx+json"
)

var reviewFreezePrefixedSHA256V1 = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var reviewFreezeGitSHA1V1 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var reviewFreezeNumericIDV1 = regexp.MustCompile(`^[1-9][0-9]*$`)
var reviewFreezeUUIDv7V1 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// reviewFreezeValidatorCompileAttestationV1 是纯单次运行 statement，只约束一个 clean
// builder run 的 claim 结构与内部绑定。原始材料、双 builder 配对、签名 envelope 与
// activation policy 都属于后续独立契约，statement 本身永远不能产生 Review Freeze authority。
type reviewFreezeValidatorCompileAttestationV1 struct {
	SchemaVersion     string                                            `json:"schema_version"`
	EvaluationRequest reviewFreezeCompileAttestationEvaluationRequestV1 `json:"evaluation_request"`
	Subject           reviewFreezeCompileAttestationSubjectV1           `json:"subject"`
	ExternalModules   []reviewFreezeCompileAttestationModuleV1          `json:"external_modules"`
	Environment       reviewFreezeCompileAttestationEnvironmentV1       `json:"environment"`
	GoList            reviewFreezeCompileAttestationGoListV1            `json:"go_list"`
	Toolchain         reviewFreezeCompileAttestationToolchainV1         `json:"toolchain"`
	BuilderRun        reviewFreezeCompileAttestationBuilderRunV1        `json:"builder_run"`
}

// reviewFreezeCompileAttestationEvaluationRequestV1 把一次运行绑定到 pair evaluator 预先
// 分配的 UUIDv7、challenge、policy 与槽位；run statement 不自行选择配对对象。
type reviewFreezeCompileAttestationEvaluationRequestV1 struct {
	EvaluationID           string `json:"evaluation_id"`
	PairingChallengeSHA256 string `json:"pairing_challenge_sha256"`
	PairPolicySHA256       string `json:"pair_policy_sha256"`
	BuilderSlot            string `json:"builder_slot"`
}

// reviewFreezeAttestationContentRefV1 只绑定内容身份和有界大小，禁止携带 URI、路径、
// bucket 或其他可变 locator；内容获取与授权属于 statement 之外的受信边界。
type reviewFreezeAttestationContentRefV1 struct {
	RefSchemaVersion     string `json:"ref_schema_version"`
	ArtifactKind         string `json:"artifact_kind"`
	ContentSchemaVersion string `json:"content_schema_version"`
	MediaType            string `json:"media_type"`
	SHA256               string `json:"sha256"`
	SizeBytes            int64  `json:"size_bytes"`
}

// reviewFreezeCompileAttestationSubjectV1 把 run claim 绑定到声明的 base subject identity。
type reviewFreezeCompileAttestationSubjectV1 struct {
	RepositoryID              string                              `json:"repository_id"`
	BaseCommitSHA             string                              `json:"base_commit_sha"`
	BaseTreeSHA               string                              `json:"base_tree_sha"`
	EntrypointID              string                              `json:"entrypoint_id"`
	PackagePath               string                              `json:"package_path"`
	PackagePattern            string                              `json:"package_pattern"`
	ContractManifestPath      string                              `json:"contract_manifest_path"`
	ContractManifestSHA256    string                              `json:"contract_manifest_sha256"`
	BuildClosureProjectionRef reviewFreezeAttestationContentRefV1 `json:"build_closure_projection_ref"`
	ValidatorSourceTreeSHA256 string                              `json:"validator_source_tree_sha256"`
	GoModSHA256               string                              `json:"go_mod_sha256"`
	GoSumSHA256               string                              `json:"go_sum_sha256"`
}

// reviewFreezeCompileAttestationModuleV1 同时固定 archive 表示层、Go h1 内容层和
// go list 实际选中的 package/source/import 图。
type reviewFreezeCompileAttestationModuleV1 struct {
	ModulePath            string                                            `json:"module_path"`
	Version               string                                            `json:"version"`
	ZipSHA256             string                                            `json:"zip_sha256"`
	ZipSizeBytes          int64                                             `json:"zip_size_bytes"`
	ZipEntryCount         int                                               `json:"zip_entry_count"`
	ZipUncompressedBytes  int64                                             `json:"zip_uncompressed_bytes"`
	ModuleSum             string                                            `json:"module_sum"`
	GoModSHA256           string                                            `json:"go_mod_sha256"`
	GoModSizeBytes        int64                                             `json:"go_mod_size_bytes"`
	GoModSum              string                                            `json:"go_mod_sum"`
	ZipRootGoModSHA256    string                                            `json:"zip_root_go_mod_sha256"`
	ZipRootGoModSizeBytes int64                                             `json:"zip_root_go_mod_size_bytes"`
	HashDirBefore         string                                            `json:"hash_dir_before"`
	HashDirAfter          string                                            `json:"hash_dir_after"`
	Packages              []reviewFreezeCompileAttestationExternalPackageV1 `json:"packages"`
}

type reviewFreezeCompileAttestationExternalPackageV1 struct {
	ImportPath string                                           `json:"import_path"`
	Sources    []reviewFreezeCompileAttestationExternalSourceV1 `json:"sources"`
	Imports    []string                                         `json:"imports"`
}

type reviewFreezeCompileAttestationExternalSourceV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// reviewFreezeCompileAttestationEnvironmentV1 冻结 acquisition 之后离线校验、编译与执行的
// 全部可见 Go/build 环境；路径值只描述隔离策略，不进入 go list canonical projection。
type reviewFreezeCompileAttestationEnvironmentV1 struct {
	GoVersion        string   `json:"go_version"`
	Toolchain        string   `json:"toolchain"`
	EnvironmentMode  string   `json:"environment_mode"`
	GOOS             string   `json:"goos"`
	GOARCH           string   `json:"goarch"`
	GOAMD64          string   `json:"goamd64"`
	CGOEnabled       string   `json:"cgo_enabled"`
	GOExperiment     string   `json:"goexperiment"`
	GOFIPS140        string   `json:"gofips140"`
	BuildTags        []string `json:"build_tags"`
	GOROOT           string   `json:"goroot"`
	GOMODCACHE       string   `json:"gomodcache"`
	GOCACHE          string   `json:"gocache"`
	GOTMPDIR         string   `json:"gotmpdir"`
	GOPATH           string   `json:"gopath"`
	GOENV            string   `json:"goenv"`
	GOWORK           string   `json:"gowork"`
	GOTOOLCHAIN      string   `json:"gotoolchain"`
	GOFLAGS          string   `json:"goflags"`
	GOPROXY          string   `json:"goproxy"`
	GOSUMDB          string   `json:"gosumdb"`
	GOVCS            string   `json:"govcs"`
	GOAUTH           string   `json:"goauth"`
	GOTELEMETRY      string   `json:"gotelemetry"`
	GODEBUG          string   `json:"godebug"`
	GOMAXPROCS       string   `json:"gomaxprocs"`
	GOMEMLIMIT       string   `json:"gomemlimit"`
	GOMODCACHEPolicy string   `json:"gomodcache_policy"`
	GOCACHEPolicy    string   `json:"gocache_policy"`
	HOME             string   `json:"home"`
	TMPDIR           string   `json:"tmpdir"`
	TZ               string   `json:"tz"`
	LANG             string   `json:"lang"`
	LCAll            string   `json:"lc_all"`
	Umask            string   `json:"umask"`
}

type reviewFreezeCompileAttestationGoListV1 struct {
	RawToProjectionPolicy string                                  `json:"raw_to_projection_policy"`
	Projection            reviewFreezeGoListCanonicalProjectionV1 `json:"projection"`
	ProjectionSHA256      string                                  `json:"projection_sha256"`
}

// reviewFreezeGoListCanonicalProjectionV1 排除 Dir/Root/GoMod/Export/BuildID/Target/Stale/Error
// 等宿主字段，只保留待 raw canonicalizer 重算的 build-selection claim。
type reviewFreezeGoListCanonicalProjectionV1 struct {
	SchemaVersion             string                                 `json:"schema_version"`
	EntrypointID              string                                 `json:"entrypoint_id"`
	ModuleRoot                string                                 `json:"module_root"`
	PackagePattern            string                                 `json:"package_pattern"`
	TargetImportPath          string                                 `json:"target_import_path"`
	TargetTestVariantObserved bool                                   `json:"target_test_variant_observed"`
	SyntheticTestMainObserved bool                                   `json:"synthetic_test_main_observed"`
	Packages                  []reviewFreezeGoListCanonicalPackageV1 `json:"packages"`
}

type reviewFreezeGoListCanonicalPackageV1 struct {
	ImportPath       string                               `json:"import_path"`
	PackageName      string                               `json:"package_name"`
	PackageKind      string                               `json:"package_kind"`
	Module           reviewFreezeGoListCanonicalModuleV1  `json:"module"`
	GoFiles          []string                             `json:"go_files"`
	CompiledGoFiles  []string                             `json:"compiled_go_files"`
	TestGoFiles      []string                             `json:"test_go_files"`
	XTestGoFiles     []string                             `json:"x_test_go_files"`
	Imports          []string                             `json:"imports"`
	TestImports      []string                             `json:"test_imports"`
	XTestImports     []string                             `json:"x_test_imports"`
	EmbedFiles       []string                             `json:"embed_files"`
	TestEmbedFiles   []string                             `json:"test_embed_files"`
	XTestEmbedFiles  []string                             `json:"x_test_embed_files"`
	OtherBuildInputs reviewFreezeGoListOtherBuildInputsV1 `json:"other_build_inputs"`
}

type reviewFreezeGoListCanonicalModuleV1 struct {
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	Version         string `json:"version"`
	GoVersion       string `json:"go_version"`
	ModuleSum       string `json:"module_sum"`
	GoModSum        string `json:"go_mod_sum"`
	GoArchiveSHA256 string `json:"go_archive_sha256"`
}

type reviewFreezeGoListOtherBuildInputsV1 struct {
	CgoFiles     []string `json:"cgo_files"`
	CFiles       []string `json:"c_files"`
	CXXFiles     []string `json:"cxx_files"`
	MFiles       []string `json:"m_files"`
	FFiles       []string `json:"f_files"`
	SFiles       []string `json:"s_files"`
	SysoFiles    []string `json:"syso_files"`
	SwigFiles    []string `json:"swig_files"`
	SwigCXXFiles []string `json:"swig_cxx_files"`
}

type reviewFreezeCompileAttestationToolchainV1 struct {
	InstallRoot                string `json:"install_root"`
	GoArchiveFile              string `json:"go_archive_file"`
	GoArchiveSHA256            string `json:"go_archive_sha256"`
	GoArchiveSize              int64  `json:"go_archive_size_bytes"`
	GOROOTTreeProjectionSHA256 string `json:"goroot_tree_projection_sha256"`
	GoBinarySHA256             string `json:"go_binary_sha256"`
	BuilderImageDigest         string `json:"builder_image_digest"`
	RunnerImageDigest          string `json:"runner_image_digest"`
}

type reviewFreezeCompileAttestationBuilderRunV1 struct {
	BuilderID              string                                     `json:"builder_id"`
	WorkspaceID            string                                     `json:"workspace_id"`
	ModuleCacheID          string                                     `json:"module_cache_id"`
	BuildCacheID           string                                     `json:"build_cache_id"`
	BuilderImageDigest     string                                     `json:"builder_image_digest"`
	RunnerImageDigest      string                                     `json:"runner_image_digest"`
	InputSnapshotBeforeRef reviewFreezeAttestationContentRefV1        `json:"input_snapshot_before_ref"`
	InputSnapshotAfterRef  reviewFreezeAttestationContentRefV1        `json:"input_snapshot_after_ref"`
	GoListInvocation       reviewFreezeCompileAttestationInvocationV1 `json:"go_list_invocation"`
	GoListRawRef           reviewFreezeAttestationContentRefV1        `json:"go_list_raw_ref"`
	GoListProjectionSHA    string                                     `json:"go_list_projection_sha256"`
	ToolchainVersion       reviewFreezeCompileAttestationInvocationV1 `json:"toolchain_version"`
	Compile                reviewFreezeCompileAttestationCompileV1    `json:"compile"`
	Test                   reviewFreezeCompileAttestationTestV1       `json:"test"`
	SBOM                   reviewFreezeCompileAttestationSBOMV1       `json:"sbom"`
}

type reviewFreezeCompileAttestationInvocationV1 struct {
	Argv            []string `json:"argv"`
	CWD             string   `json:"cwd"`
	ExitCode        int      `json:"exit_code"`
	StdoutSHA256    string   `json:"stdout_sha256"`
	StdoutSizeBytes int64    `json:"stdout_size_bytes"`
	StderrSHA256    string   `json:"stderr_sha256"`
	StderrSizeBytes int64    `json:"stderr_size_bytes"`
}

type reviewFreezeCompileAttestationCompileV1 struct {
	Invocation                reviewFreezeCompileAttestationInvocationV1 `json:"invocation"`
	ArtifactPath              string                                     `json:"artifact_path"`
	ArtifactMode              string                                     `json:"artifact_mode"`
	ArtifactRef               reviewFreezeAttestationContentRefV1        `json:"artifact_ref"`
	BuildInfoInvocation       reviewFreezeCompileAttestationInvocationV1 `json:"build_info_invocation"`
	BuildInfoRawRef           reviewFreezeAttestationContentRefV1        `json:"build_info_raw_ref"`
	BuildInfoProjection       reviewFreezeCompileAttestationBuildInfoV1  `json:"build_info_projection"`
	BuildInfoProjectionSHA256 string                                     `json:"build_info_projection_sha256"`
}

// reviewFreezeCompileAttestationBuildInfoV1 定义目标 Go BuildInfo 投影；Batch 3C 必须从
// 已验 binary 重新派生它。投影不保留 raw 输出顺序，依赖和 setting 必须显式、排序、唯一。
type reviewFreezeCompileAttestationBuildInfoV1 struct {
	SchemaVersion string                                         `json:"schema_version"`
	GoVersion     string                                         `json:"go_version"`
	Path          string                                         `json:"path"`
	MainPath      string                                         `json:"main_path"`
	MainVersion   string                                         `json:"main_version"`
	Dependencies  []reviewFreezeCompileAttestationBuildInfoDepV1 `json:"dependencies"`
	Settings      []reviewFreezeCompileAttestationBuildSettingV1 `json:"settings"`
}

type reviewFreezeCompileAttestationBuildInfoDepV1 struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Sum     string `json:"sum"`
}

type reviewFreezeCompileAttestationBuildSettingV1 struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type reviewFreezeCompileAttestationTestV1 struct {
	Invocation                  reviewFreezeCompileAttestationInvocationV1 `json:"invocation"`
	TestEntrypoints             []string                                   `json:"test_entrypoints"`
	PreExecutionArtifactSHA256  string                                     `json:"pre_execution_artifact_sha256"`
	PostExecutionArtifactSHA256 string                                     `json:"post_execution_artifact_sha256"`
	SandboxPolicySHA256         string                                     `json:"sandbox_policy_sha256"`
	NetworkPolicy               string                                     `json:"network_policy"`
	SecretPolicy                string                                     `json:"secret_policy"`
	SourceFilesystemPolicy      string                                     `json:"source_filesystem_policy"`
	ModuleCacheFilesystemPolicy string                                     `json:"module_cache_filesystem_policy"`
}

type reviewFreezeCompileAttestationSBOMV1 struct {
	Format                string                                     `json:"format"`
	FormatVersion         string                                     `json:"format_version"`
	GeneratorName         string                                     `json:"generator_name"`
	GeneratorVersion      string                                     `json:"generator_version"`
	GeneratorBinaryRef    reviewFreezeAttestationContentRefV1        `json:"generator_binary_ref"`
	Invocation            reviewFreezeCompileAttestationInvocationV1 `json:"invocation"`
	SubjectArtifactSHA256 string                                     `json:"subject_artifact_sha256"`
	RawRef                reviewFreezeAttestationContentRefV1        `json:"raw_ref"`
}

// reviewFreezeValidateCompileAttestationStatementJSONV1 执行 UTF-8、无 null、字段完整、
// 严格 DTO 与语义 exact-set 四层校验。缺字段不会因 Go 零值恰好等于期望值而被接受。
func reviewFreezeValidateCompileAttestationStatementJSONV1(raw []byte) error {
	_, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(raw)
	return err
}

// reviewFreezeDecodeCompileAttestationStatementJSONV1 是所有跨信任边界调用的唯一入口；
// 它在返回 typed statement 前完成严格 JSON 与完整语义校验，避免上层先 permissive
// unmarshal 后丢失重复字段、unknown field 或 null 证据。
func reviewFreezeDecodeCompileAttestationStatementJSONV1(raw []byte) (reviewFreezeValidatorCompileAttestationV1, error) {
	var zero reviewFreezeValidatorCompileAttestationV1
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationMaxJSONBytesV1 {
		return zero, fmt.Errorf("compile attestation statement size=%d limit=%d", len(raw), reviewFreezeCompileAttestationMaxJSONBytesV1)
	}
	if !utf8.Valid(raw) {
		return zero, fmt.Errorf("compile attestation statement 不是合法 UTF-8")
	}
	if err := reviewFreezeInspectCompileAttestationJSONV1(raw); err != nil {
		return zero, err
	}
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return zero, err
	}
	if err := reviewFreezeRejectCompileAttestationNullV1(generic, "$statement"); err != nil {
		return zero, err
	}
	if err := reviewFreezeRequireCompileAttestationFieldsV1(generic, reflect.TypeOf(reviewFreezeValidatorCompileAttestationV1{}), "$statement"); err != nil {
		return zero, err
	}
	var statement reviewFreezeValidatorCompileAttestationV1
	strictDecoder := json.NewDecoder(bytes.NewReader(raw))
	strictDecoder.DisallowUnknownFields()
	if err := strictDecoder.Decode(&statement); err != nil {
		return zero, err
	}
	var trailing any
	if err := strictDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("compile attestation trailing JSON")
	}
	if err := reviewFreezeValidateCompileAttestationStatementV1(statement); err != nil {
		return zero, err
	}
	return statement, nil
}

// reviewFreezeInspectCompileAttestationJSONV1 在严格 DTO 解码前拒绝重复 key，并给递归
// 深度设置硬上限，避免攻击者用深层未知字段耗尽 verifier 栈空间。
func reviewFreezeInspectCompileAttestationJSONV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := reviewFreezeInspectCompileAttestationJSONValueV1(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("compile attestation trailing JSON")
		}
		return err
	}
	return nil
}

func reviewFreezeInspectCompileAttestationJSONValueV1(decoder *json.Decoder, depth int) error {
	if depth > reviewFreezeCompileAttestationMaxJSONDepthV1 {
		return fmt.Errorf("compile attestation JSON depth=%d limit=%d", depth, reviewFreezeCompileAttestationMaxJSONDepthV1)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
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
				return fmt.Errorf("compile attestation object key 必须是 string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("compile attestation duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if err := reviewFreezeInspectCompileAttestationJSONValueV1(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return fmt.Errorf("compile attestation object close 非法")
		}
	case '[':
		for decoder.More() {
			if err := reviewFreezeInspectCompileAttestationJSONValueV1(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return fmt.Errorf("compile attestation array close 非法")
		}
	default:
		return fmt.Errorf("compile attestation delimiter 非法=%q", delimiter)
	}
	return nil
}

func reviewFreezeRejectCompileAttestationNullV1(value any, path string) error {
	if value == nil {
		return fmt.Errorf("compile attestation 禁止 null=%s", path)
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := typed[key]
			if err := reviewFreezeRejectCompileAttestationNullV1(child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := reviewFreezeRejectCompileAttestationNullV1(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func reviewFreezeRequireCompileAttestationFieldsV1(value any, target reflect.Type, path string) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	switch target.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("compile attestation %s 必须是 object", path)
		}
		allowed := make(map[string]reflect.StructField, target.NumField())
		for index := 0; index < target.NumField(); index++ {
			field := target.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			allowed[name] = field
			child, exists := object[name]
			if !exists {
				return fmt.Errorf("compile attestation 缺必填字段=%s.%s", path, name)
			}
			if err := reviewFreezeRequireCompileAttestationFieldsV1(child, field.Type, path+"."+name); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, exists := allowed[key]; !exists {
				return fmt.Errorf("compile attestation unknown field=%s.%s", path, key)
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("compile attestation %s 必须是 array", path)
		}
		for index, child := range array {
			if err := reviewFreezeRequireCompileAttestationFieldsV1(child, target.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func reviewFreezeValidateCompileAttestationStatementV1(statement reviewFreezeValidatorCompileAttestationV1) error {
	if statement.SchemaVersion != reviewFreezeValidatorCompileAttestationSchemaV1 {
		return fmt.Errorf("compile attestation schema_version=%q", statement.SchemaVersion)
	}
	if err := reviewFreezeValidateCompileAttestationEvaluationRequestV1(statement.EvaluationRequest); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationSubjectV1(statement.Subject); err != nil {
		return err
	}
	if len(statement.ExternalModules) != 1 {
		return fmt.Errorf("compile attestation external_modules exact-set 长度=%d want=1", len(statement.ExternalModules))
	}
	if err := reviewFreezeValidateCompileAttestationModuleV1(statement.ExternalModules[0]); err != nil {
		return err
	}
	wantEnvironment := reviewFreezeExpectedCompileAttestationEnvironmentV1()
	if !reflect.DeepEqual(statement.Environment, wantEnvironment) {
		return fmt.Errorf("compile attestation environment=%+v want=%+v", statement.Environment, wantEnvironment)
	}
	if err := reviewFreezeValidateCompileAttestationToolchainV1(statement.Toolchain); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationGoListV1(statement.GoList, statement.Toolchain.GoArchiveSHA256, statement.ExternalModules[0]); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationBuilderRunV1(statement.BuilderRun, statement.GoList.ProjectionSHA256, statement.Toolchain); err != nil {
		return err
	}
	return nil
}

func reviewFreezeValidateCompileAttestationEvaluationRequestV1(request reviewFreezeCompileAttestationEvaluationRequestV1) error {
	if !reviewFreezeUUIDv7V1.MatchString(request.EvaluationID) {
		return fmt.Errorf("compile attestation evaluation_id 非法=%q", request.EvaluationID)
	}
	for _, field := range []struct {
		name   string
		digest string
	}{
		{name: "pairing_challenge", digest: request.PairingChallengeSHA256},
		{name: "pair_policy", digest: request.PairPolicySHA256},
	} {
		if !reviewFreezePrefixedSHA256V1.MatchString(field.digest) || field.digest == reviewFreezeSHA256V1(nil) {
			return fmt.Errorf("compile attestation %s digest 非法=%q", field.name, field.digest)
		}
	}
	if request.BuilderSlot != "builder_a" && request.BuilderSlot != "builder_b" {
		return fmt.Errorf("compile attestation builder_slot 非法=%q", request.BuilderSlot)
	}
	return nil
}

func reviewFreezeValidateCompileAttestationSubjectV1(subject reviewFreezeCompileAttestationSubjectV1) error {
	zeroGitSHA := strings.Repeat("0", 40)
	_, repositoryIDErr := strconv.ParseUint(subject.RepositoryID, 10, 64)
	if len(subject.RepositoryID) > 20 ||
		!reviewFreezeNumericIDV1.MatchString(subject.RepositoryID) ||
		repositoryIDErr != nil ||
		!reviewFreezeGitSHA1V1.MatchString(subject.BaseCommitSHA) ||
		!reviewFreezeGitSHA1V1.MatchString(subject.BaseTreeSHA) ||
		subject.BaseCommitSHA == zeroGitSHA || subject.BaseTreeSHA == zeroGitSHA {
		return fmt.Errorf("compile attestation repository/base identity 非法")
	}
	if subject.EntrypointID != reviewFreezeCompileAttestationEntrypointV1 ||
		subject.PackagePath != reviewFreezeCompileAttestationPackagePathV1 ||
		subject.PackagePattern != reviewFreezeCompileAttestationPackagePatternV1 ||
		subject.ContractManifestPath != "agent/tests/contract/testdata/w2_r01/manifest.json" {
		return fmt.Errorf("compile attestation subject identity 漂移=%+v", subject)
	}
	if err := reviewFreezeValidateSafePathV1(subject.ContractManifestPath, "agent/tests/contract/testdata/"); err != nil {
		return err
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		subject.BuildClosureProjectionRef,
		reviewFreezeCompileAttestationBuildClosureArtifactKindV1,
		reviewFreezeCompileAttestationBuildClosureContentSchemaV1,
		reviewFreezeCompileAttestationBuildClosureMediaTypeV1,
		reviewFreezeCompileAttestationBuildClosureMaxBytesV1,
		"build closure projection",
	); err != nil {
		return err
	}
	for _, field := range []struct {
		name   string
		digest string
	}{
		{name: "contract_manifest", digest: subject.ContractManifestSHA256},
		{name: "validator_sources", digest: subject.ValidatorSourceTreeSHA256},
		{name: "go.mod", digest: subject.GoModSHA256},
		{name: "go.sum", digest: subject.GoSumSHA256},
	} {
		if !reviewFreezePrefixedSHA256V1.MatchString(field.digest) || field.digest == reviewFreezeSHA256V1(nil) {
			return fmt.Errorf("compile attestation subject %s digest 非法=%q", field.name, field.digest)
		}
	}
	return nil
}

// reviewFreezeValidateAttestationContentRefV1 校验不可变内容引用的类型、Schema、媒体类型、
// 非空摘要和有界大小。ref 禁止携带 locator，因此调用者只能从受信 bundle/CAS 按摘要取材。
func reviewFreezeValidateAttestationContentRefV1(ref reviewFreezeAttestationContentRefV1, artifactKind, contentSchema, mediaType string, maxSize int64, field string) error {
	if ref.RefSchemaVersion != reviewFreezeAttestationContentRefSchemaV1 ||
		ref.ArtifactKind != artifactKind ||
		ref.ContentSchemaVersion != contentSchema ||
		ref.MediaType != mediaType {
		return fmt.Errorf("compile attestation %s content ref identity 漂移=%+v", field, ref)
	}
	if !reviewFreezePrefixedSHA256V1.MatchString(ref.SHA256) || ref.SHA256 == reviewFreezeSHA256V1(nil) {
		return fmt.Errorf("compile attestation %s content ref digest 非法=%q", field, ref.SHA256)
	}
	if ref.SizeBytes <= 0 || ref.SizeBytes > maxSize {
		return fmt.Errorf("compile attestation %s content ref size=%d limit=%d", field, ref.SizeBytes, maxSize)
	}
	return nil
}

func reviewFreezeExpectedCompileAttestationEnvironmentV1() reviewFreezeCompileAttestationEnvironmentV1 {
	return reviewFreezeCompileAttestationEnvironmentV1{
		GoVersion:        "1.26",
		Toolchain:        "go1.26.3",
		EnvironmentMode:  "env_i_exact_set",
		GOOS:             "linux",
		GOARCH:           "amd64",
		GOAMD64:          "v1",
		CGOEnabled:       "0",
		GOExperiment:     "",
		GOFIPS140:        "off",
		BuildTags:        []string{},
		GOROOT:           "/opt/go",
		GOMODCACHE:       "/gomodcache",
		GOCACHE:          "/gocache",
		GOTMPDIR:         "/tmp/go-build",
		GOPATH:           "/nonexistent/go",
		GOENV:            "off",
		GOWORK:           "off",
		GOTOOLCHAIN:      "local",
		GOFLAGS:          "",
		GOPROXY:          "off",
		GOSUMDB:          "off",
		GOVCS:            "off",
		GOAUTH:           "off",
		GOTELEMETRY:      "off",
		GODEBUG:          "",
		GOMAXPROCS:       "1",
		GOMEMLIMIT:       "off",
		GOMODCACHEPolicy: "fresh_isolated_verified_readonly",
		GOCACHEPolicy:    "fresh_isolated",
		HOME:             "/nonexistent",
		TMPDIR:           "/tmp",
		TZ:               "UTC",
		LANG:             "C",
		LCAll:            "C",
		Umask:            "0022",
	}
}

func reviewFreezeValidateCompileAttestationModuleV1(module reviewFreezeCompileAttestationModuleV1) error {
	if module.ModulePath != reviewFreezeXTextModulePathV1 || module.Version != reviewFreezeXTextModuleVersionV1 {
		return fmt.Errorf("compile attestation external module identity=%s@%s", module.ModulePath, module.Version)
	}
	if module.ZipSHA256 != "sha256:67a6cab352a4f313d56671618dfff2f82d908a17151dc4ca9b7fbbfd40828134" ||
		module.ZipSizeBytes != 7015063 || module.ZipEntryCount != 488 || module.ZipUncompressedBytes != 29567429 {
		return fmt.Errorf("compile attestation x/text zip raw identity 漂移")
	}
	if module.ModuleSum != "h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=" ||
		module.GoModSHA256 != "sha256:43b8c43b254e9c1895aca686f562dc9029ec3051f5fc568c56eff7c282084dcf" ||
		module.GoModSizeBytes != 190 ||
		module.GoModSum != "h1:homfLqTYRFyVYemLBFl5GgL/DWEiH5wcsQ5gSh1yziA=" ||
		module.ZipRootGoModSHA256 != module.GoModSHA256 ||
		module.ZipRootGoModSizeBytes != module.GoModSizeBytes {
		return fmt.Errorf("compile attestation x/text module/go.mod identity 漂移")
	}
	if module.HashDirBefore != module.ModuleSum || module.HashDirAfter != module.ModuleSum {
		return fmt.Errorf("compile attestation x/text HashDir pre/post=%q/%q want=%q", module.HashDirBefore, module.HashDirAfter, module.ModuleSum)
	}
	wantPackages := reviewFreezeExpectedCompileAttestationExternalPackagesV1()
	if !reflect.DeepEqual(module.Packages, wantPackages) {
		return fmt.Errorf("compile attestation x/text selected package/source/import exact-set 漂移")
	}
	return nil
}

func reviewFreezeExpectedCompileAttestationExternalPackagesV1() []reviewFreezeCompileAttestationExternalPackageV1 {
	return []reviewFreezeCompileAttestationExternalPackageV1{
		{
			ImportPath: "golang.org/x/text/transform",
			Sources: []reviewFreezeCompileAttestationExternalSourceV1{
				{Path: "transform/transform.go", SHA256: "sha256:7716301e210f42b764ec78f4adffb05075f1d5103446a40b8a38043f5a7abd88"},
			},
			Imports: []string{"bytes", "errors", "io", "unicode/utf8"},
		},
		{
			ImportPath: "golang.org/x/text/unicode/norm",
			Sources: []reviewFreezeCompileAttestationExternalSourceV1{
				{Path: "unicode/norm/composition.go", SHA256: "sha256:3d1be52960f2693926472819b747646e7d2371d2bb5f53097cf9d46c913052df"},
				{Path: "unicode/norm/forminfo.go", SHA256: "sha256:fceb28bef5efec8347536a89018e906f607a70a91051d9360b3656867efe2d9e"},
				{Path: "unicode/norm/input.go", SHA256: "sha256:965b431790bb139543d71a9c497920ef7d9a15af417456a2bbd0cdb629330e8d"},
				{Path: "unicode/norm/iter.go", SHA256: "sha256:16a40d326f9b37c5430a18894f718007728ce8324afb08d8c0a17ce7289714b0"},
				{Path: "unicode/norm/normalize.go", SHA256: "sha256:b9e7aeff51e6cb036ff4e8c6255410e7068c112e329a137d333e639f63c4a8ab"},
				{Path: "unicode/norm/readwriter.go", SHA256: "sha256:d600d1f6cff2536dbb49a0f6bb8c06f6fc05f2754dad2dc05f1d44f9e8e48a35"},
				{Path: "unicode/norm/tables15.0.0.go", SHA256: "sha256:49dda94f9429bac8c29efe11e09558d46abfaa3c8e1de59816f2910b99cade04"},
				{Path: "unicode/norm/transform.go", SHA256: "sha256:6f8014595643e2acae76d47ed6abe8ace969a0c9070dbfa5a89c86bebcec812d"},
				{Path: "unicode/norm/trie.go", SHA256: "sha256:d87793d558251ee8824954f0b7bc5564803e4c9d59a8c4eebb4c3c5cfbd19492"},
			},
			Imports: []string{"encoding/binary", "fmt", "golang.org/x/text/transform", "io", "sync", "unicode/utf8"},
		},
	}
}

func reviewFreezeValidateCompileAttestationToolchainV1(toolchain reviewFreezeCompileAttestationToolchainV1) error {
	if toolchain.InstallRoot != "/opt/go" ||
		toolchain.GoArchiveFile != "/artifacts/go1.26.3.linux-amd64.tar.gz" ||
		toolchain.GoArchiveSize <= 0 || toolchain.GoArchiveSize > reviewFreezeCompileAttestationMaxGoArchiveV1 {
		return fmt.Errorf("compile attestation toolchain path/size 漂移=%+v", toolchain)
	}
	for _, field := range []struct {
		name   string
		digest string
	}{
		{name: "go archive", digest: toolchain.GoArchiveSHA256},
		{name: "GOROOT tree", digest: toolchain.GOROOTTreeProjectionSHA256},
		{name: "go binary", digest: toolchain.GoBinarySHA256},
		{name: "builder image", digest: toolchain.BuilderImageDigest},
		{name: "runner image", digest: toolchain.RunnerImageDigest},
	} {
		if !reviewFreezePrefixedSHA256V1.MatchString(field.digest) || field.digest == reviewFreezeSHA256V1(nil) {
			return fmt.Errorf("compile attestation %s digest 非法=%q", field.name, field.digest)
		}
	}
	return nil
}
