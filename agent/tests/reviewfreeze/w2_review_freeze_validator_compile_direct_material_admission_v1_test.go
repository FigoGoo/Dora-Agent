package reviewfreeze_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const reviewFreezeCompileDirectMaterialDecisionPartialV1 = "partial_test_only_admission"

const (
	reviewFreezeCompileDirectMaterialGapArtifactV1       = "compiled_test_binary_semantics_and_derivation_unverified"
	reviewFreezeCompileDirectMaterialGapBuildClosureV1   = "build_closure_projection_semantics_unverified"
	reviewFreezeCompileDirectMaterialGapBuildInfoLinkV1  = "build_info_not_derived_from_verified_artifact"
	reviewFreezeCompileDirectMaterialGapBuilderPairV1    = "independent_builder_pair_and_trust_root_unverified"
	reviewFreezeCompileDirectMaterialGapSnapshotLeavesV1 = "input_snapshot_14_repository_and_15_module_cache_provenance_leaves_unverified"
	reviewFreezeCompileDirectMaterialGapSBOMBinaryV1     = "sbom_generator_binary_semantics_unverified"
	reviewFreezeCompileDirectMaterialGapSBOMDeriveV1     = "sbom_not_regenerated_from_verified_artifact"
	reviewFreezeCompileDirectMaterialGapSBOMRawV1        = "sbom_raw_semantics_unverified"
	reviewFreezeCompileDirectMaterialGapToolchainV1      = "toolchain_and_image_materials_unverified"
)

// reviewFreezeCompileDirectMaterialAdmissionV1 是 direct material 的测试候选结论。
// Decision、role 分类和 gap 都是 exact-set；Authority/FormalFreezeEligible 必须恒为
// false，避免把三项语义绑定误报为编译证明或 Formal Freeze authority。
type reviewFreezeCompileDirectMaterialAdmissionV1 struct {
	Decision               string
	SemanticallyBoundRoles []string
	HashSizeOnlyRoles      []string
	ClosedSemanticGaps     []string
	OpenGaps               []string
	Authority              bool
	FormalFreezeEligible   bool
}

// reviewFreezeCompileDirectMaterialFixtureV1 聚合测试所需的 statement、descriptor 和
// role 原始字节。所有变体都重新生成 typed refs，确保语义漂移测试越过 hash/size 门禁。
type reviewFreezeCompileDirectMaterialFixtureV1 struct {
	DescriptorRaw []byte
	Statement     reviewFreezeValidatorCompileAttestationV1
	StatementRaw  []byte
	RoleBytes     map[string][]byte
	Materials     map[string][]byte
}

// reviewFreezeCompileDirectMaterialExpectedRolesV1 返回 material bundle 的完整有序 role
// exact-set；admission 不接受漏项，也不接受未来字段静默加入。
func reviewFreezeCompileDirectMaterialExpectedRolesV1() []string {
	return []string{
		reviewFreezeAttestationMaterialRoleBuildClosureV1,
		reviewFreezeAttestationMaterialRoleBuildInfoV1,
		reviewFreezeAttestationMaterialRoleArtifactV1,
		reviewFreezeAttestationMaterialRoleGoListV1,
		reviewFreezeAttestationMaterialRoleSnapshotV1,
		reviewFreezeAttestationMaterialRoleSBOMBinaryV1,
		reviewFreezeAttestationMaterialRoleSBOMRawV1,
	}
}

// reviewFreezeCompileDirectMaterialExpectedAdmissionV1 固定 partial/test-only 的能力边界。
// build closure、artifact 与两项 SBOM material 只有内容身份验证；snapshot 内 14 项
// repository 与 15 项 provenance-complete module-cache 叶子字节也尚未加载验真。后者
// 分为 12 项 go_command_input、2 项 acquisition_evidence 和 1 项
// materialization_evidence，不能把 15 项都误称为 execution input。
func reviewFreezeCompileDirectMaterialExpectedAdmissionV1() reviewFreezeCompileDirectMaterialAdmissionV1 {
	return reviewFreezeCompileDirectMaterialAdmissionV1{
		Decision: reviewFreezeCompileDirectMaterialDecisionPartialV1,
		SemanticallyBoundRoles: []string{
			reviewFreezeAttestationMaterialRoleBuildInfoV1,
			reviewFreezeAttestationMaterialRoleGoListV1,
			reviewFreezeAttestationMaterialRoleSnapshotV1,
		},
		HashSizeOnlyRoles: []string{
			reviewFreezeAttestationMaterialRoleBuildClosureV1,
			reviewFreezeAttestationMaterialRoleArtifactV1,
			reviewFreezeAttestationMaterialRoleSBOMBinaryV1,
			reviewFreezeAttestationMaterialRoleSBOMRawV1,
		},
		ClosedSemanticGaps: []string{
			"build_info_raw_text_semantics_bound",
			"go_list_raw_projection_semantics_bound",
			"input_snapshot_json_semantics_bound",
		},
		OpenGaps: []string{
			reviewFreezeCompileDirectMaterialGapBuildClosureV1,
			reviewFreezeCompileDirectMaterialGapBuildInfoLinkV1,
			reviewFreezeCompileDirectMaterialGapArtifactV1,
			reviewFreezeCompileDirectMaterialGapBuilderPairV1,
			reviewFreezeCompileDirectMaterialGapSnapshotLeavesV1,
			reviewFreezeCompileDirectMaterialGapSBOMBinaryV1,
			reviewFreezeCompileDirectMaterialGapSBOMDeriveV1,
			reviewFreezeCompileDirectMaterialGapSBOMRawV1,
			reviewFreezeCompileDirectMaterialGapToolchainV1,
		},
		Authority:            false,
		FormalFreezeEligible: false,
	}
}

// reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1 对结论执行 exact-set 校验。
// 任何删 gap、把 hash-only role 提升为语义 role 或宣称 authority 的行为都会失败关闭。
func reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(result reviewFreezeCompileDirectMaterialAdmissionV1) error {
	want := reviewFreezeCompileDirectMaterialExpectedAdmissionV1()
	if result.Decision != want.Decision {
		return fmt.Errorf("direct material admission decision=%q want=%q", result.Decision, want.Decision)
	}
	for _, set := range []struct {
		name string
		got  []string
		want []string
	}{
		{name: "semantically bound roles", got: result.SemanticallyBoundRoles, want: want.SemanticallyBoundRoles},
		{name: "hash/size-only roles", got: result.HashSizeOnlyRoles, want: want.HashSizeOnlyRoles},
		{name: "closed semantic gaps", got: result.ClosedSemanticGaps, want: want.ClosedSemanticGaps},
		{name: "open gaps", got: result.OpenGaps, want: want.OpenGaps},
	} {
		if !reflect.DeepEqual(set.got, set.want) {
			return fmt.Errorf("direct material admission %s=%v want exact-set=%v", set.name, set.got, set.want)
		}
	}
	if result.Authority || result.FormalFreezeEligible {
		return fmt.Errorf("direct material admission 不得授予 authority/Formal Freeze=%v/%v", result.Authority, result.FormalFreezeEligible)
	}
	return nil
}

// reviewFreezeAdmitCompileDirectMaterialsV1 对 verified bundle 中可直接解释的三项材料
// 重新做语义绑定。其余四项只沿用 bundle 的 SHA/size 结论，因此返回值只能是 partial。
func reviewFreezeAdmitCompileDirectMaterialsV1(verified *reviewFreezeVerifiedAttestationMaterialBundleV1) (reviewFreezeCompileDirectMaterialAdmissionV1, error) {
	var zero reviewFreezeCompileDirectMaterialAdmissionV1
	if verified == nil {
		return zero, fmt.Errorf("direct material verified bundle 不能为空")
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(verified.StatementRaw())
	if err != nil {
		return zero, fmt.Errorf("direct material strict statement: %w", err)
	}
	if got, want := verified.Roles(), reviewFreezeCompileDirectMaterialExpectedRolesV1(); !reflect.DeepEqual(got, want) {
		return zero, fmt.Errorf("direct material roles=%v want exact-set=%v", got, want)
	}

	snapshotRaw, err := reviewFreezeCompileDirectMaterialBytesV1(verified, reviewFreezeAttestationMaterialRoleSnapshotV1)
	if err != nil {
		return zero, err
	}
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(snapshotRaw, statement); err != nil {
		return zero, fmt.Errorf("direct material input snapshot semantics: %w", err)
	}

	goListRaw, err := reviewFreezeCompileDirectMaterialBytesV1(verified, reviewFreezeAttestationMaterialRoleGoListV1)
	if err != nil {
		return zero, err
	}
	rawContext, err := reviewFreezeCompileDirectMaterialRawContextFromStatementV1(statement)
	if err != nil {
		return zero, err
	}
	projection, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(goListRaw, rawContext)
	if err != nil {
		return zero, fmt.Errorf("direct material go list raw semantics: %w", err)
	}
	if err := reviewFreezeValidateCompileDirectMaterialInfoTimeBindingV1(goListRaw, statement.ExternalModules[0]); err != nil {
		return zero, err
	}
	projectionSHA, err := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(projection)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(projection, statement.GoList.Projection) ||
		projectionSHA != statement.GoList.ProjectionSHA256 ||
		projectionSHA != statement.BuilderRun.GoListProjectionSHA {
		return zero, fmt.Errorf("direct material go list raw-derived projection mismatch sha=%q statement=%q run=%q", projectionSHA, statement.GoList.ProjectionSHA256, statement.BuilderRun.GoListProjectionSHA)
	}

	buildInfoRaw, err := reviewFreezeCompileDirectMaterialBytesV1(verified, reviewFreezeAttestationMaterialRoleBuildInfoV1)
	if err != nil {
		return zero, err
	}
	if err := reviewFreezeValidateCompileAttestationBuildInfoRawV1(buildInfoRaw, statement.BuilderRun.Compile); err != nil {
		return zero, fmt.Errorf("direct material build info raw semantics: %w", err)
	}

	result := reviewFreezeCompileDirectMaterialExpectedAdmissionV1()
	if err := reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(result); err != nil {
		return zero, err
	}
	return result, nil
}

// reviewFreezeValidateCompileDirectMaterialInfoTimeBindingV1 补足 snapshot 与 raw 的同次
// 运行绑定：standalone build projection 可以忽略 acquisition `.info`，但该 admission 已
// 要求 snapshot 的 15 项 union 包含锁定 `.info`，因此两个 external package 的 raw
// Module.Time 都必须出现且一致。否则只能说明 build selection 相同，不能说明 raw 来自
// 所声明的 snapshot。
func reviewFreezeValidateCompileDirectMaterialInfoTimeBindingV1(raw []byte, module reviewFreezeCompileAttestationModuleV1) error {
	packages, err := reviewFreezeParseCompileAttestationGoListRawStreamV1(raw)
	if err != nil {
		return fmt.Errorf("direct material snapshot .info/raw Time parse: %w", err)
	}
	externalCount := 0
	for _, selectedPackage := range packages {
		if selectedPackage.Module == nil || selectedPackage.Module.Path != module.ModulePath || selectedPackage.Module.Main {
			continue
		}
		externalCount++
		if selectedPackage.Module.Time != reviewFreezeCompileInputSnapshotModuleInfoTimeV1 {
			return fmt.Errorf("direct material snapshot .info/raw Time mismatch package=%q got=%q want=%q", selectedPackage.ImportPath, selectedPackage.Module.Time, reviewFreezeCompileInputSnapshotModuleInfoTimeV1)
		}
	}
	if externalCount != len(module.Packages) {
		return fmt.Errorf("direct material snapshot .info/raw Time package count=%d want=%d", externalCount, len(module.Packages))
	}
	return nil
}

// reviewFreezeCompileDirectMaterialBytesV1 只读取 verifier 已冻结的副本，禁止再次访问
// loader，避免语义校验阶段出现 TOCTOU 或按 role 漏材。
func reviewFreezeCompileDirectMaterialBytesV1(verified *reviewFreezeVerifiedAttestationMaterialBundleV1, role string) ([]byte, error) {
	_, raw, exists := verified.Material(role)
	if !exists {
		return nil, fmt.Errorf("direct material role 缺失=%q", role)
	}
	return raw, nil
}

// reviewFreezeCompileDirectMaterialRawContextFromStatementV1 只从 strict statement 的
// Environment、GoList invocation 与 Toolchain 构造 raw canonicalizer 上下文，调用方
// 无法另行注入宿主路径、archive digest 或 external module。
func reviewFreezeCompileDirectMaterialRawContextFromStatementV1(statement reviewFreezeValidatorCompileAttestationV1) (reviewFreezeCompileAttestationGoListRawContextV1, error) {
	if len(statement.ExternalModules) != 1 {
		return reviewFreezeCompileAttestationGoListRawContextV1{}, fmt.Errorf("direct material external modules exact-set=%d want=1", len(statement.ExternalModules))
	}
	return reviewFreezeCompileAttestationGoListRawContextV1{
		GoRoot:          statement.Environment.GOROOT,
		ModuleRoot:      statement.BuilderRun.GoListInvocation.CWD,
		ModuleCacheRoot: statement.Environment.GOMODCACHE,
		GoCacheRoot:     statement.Environment.GOCACHE,
		GoArchiveSHA256: statement.Toolchain.GoArchiveSHA256,
		ExternalModule:  statement.ExternalModules[0],
	}, nil
}

// reviewFreezeCompileDirectMaterialFixtureNewV1 使用真实 Go 1.26.3 go-list raw stream
// 构造完整候选。宿主绝对路径被规范化为 statement 声明的隔离路径，随后再由 raw
// canonicalizer 逐字段验证，不能用 statement 自报 projection 替代解析。
func reviewFreezeCompileDirectMaterialFixtureNewV1(t *testing.T) reviewFreezeCompileDirectMaterialFixtureV1 {
	t.Helper()
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	hostRaw, hostContext := reviewFreezeTestCompileAttestationRunGoListRawV1(t)
	logicalContext, err := reviewFreezeCompileDirectMaterialRawContextFromStatementV1(statement)
	if err != nil {
		t.Fatal(err)
	}
	goListRaw := reviewFreezeCompileDirectMaterialRewriteGoListRawV1(t, hostRaw, []reviewFreezeCompileDirectMaterialPathRewriteV1{
		{From: hostContext.GoRoot, To: logicalContext.GoRoot},
		{From: hostContext.ModuleRoot, To: logicalContext.ModuleRoot},
		{From: hostContext.ModuleCacheRoot, To: logicalContext.ModuleCacheRoot},
		{From: hostContext.GoCacheRoot, To: logicalContext.GoCacheRoot},
	})
	projection, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(goListRaw, logicalContext)
	if err != nil {
		t.Fatalf("canonicalize direct material go list raw: %v", err)
	}
	projectionSHA, err := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(projection)
	if err != nil {
		t.Fatal(err)
	}
	statement.GoList.Projection = projection
	statement.GoList.ProjectionSHA256 = projectionSHA
	statement.BuilderRun.GoListProjectionSHA = projectionSHA

	buildInfoRaw, _ := reviewFreezeCompileAttestationBuildInfoRawFixtureV1()
	roleBytes := map[string][]byte{
		reviewFreezeAttestationMaterialRoleBuildClosureV1: []byte("partial candidate build closure projection"),
		reviewFreezeAttestationMaterialRoleBuildInfoV1:    append([]byte(nil), buildInfoRaw...),
		reviewFreezeAttestationMaterialRoleArtifactV1:     []byte("partial candidate compiled test binary"),
		reviewFreezeAttestationMaterialRoleGoListV1:       append([]byte(nil), goListRaw...),
		reviewFreezeAttestationMaterialRoleSBOMBinaryV1:   []byte("partial candidate deterministic SBOM generator binary"),
		reviewFreezeAttestationMaterialRoleSBOMRawV1:      []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`),
	}
	for _, role := range []string{
		reviewFreezeAttestationMaterialRoleBuildClosureV1,
		reviewFreezeAttestationMaterialRoleGoListV1,
		reviewFreezeAttestationMaterialRoleArtifactV1,
		reviewFreezeAttestationMaterialRoleBuildInfoV1,
		reviewFreezeAttestationMaterialRoleSBOMBinaryV1,
		reviewFreezeAttestationMaterialRoleSBOMRawV1,
	} {
		reviewFreezeCompileDirectMaterialBindRoleV1(&statement, role, roleBytes[role])
	}

	// repository fixture 同时把 manifest/go.mod/go.sum/source-tree 摘要绑定回 subject；
	// snapshot 必须在这一步之后冻结，否则 statement 与叶子元数据会产生伪一致性。
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
	snapshotRaw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1] = snapshotRaw
	reviewFreezeCompileDirectMaterialBindRoleV1(&statement, reviewFreezeAttestationMaterialRoleSnapshotV1, snapshotRaw)
	return reviewFreezeCompileDirectMaterialFixtureBuildV1(t, statement, roleBytes)
}

// reviewFreezeCompileDirectMaterialPathRewriteV1 描述测试 raw stream 的单个可信路径映射。
type reviewFreezeCompileDirectMaterialPathRewriteV1 struct {
	From string
	To   string
}

// reviewFreezeCompileDirectMaterialRewriteGoListRawV1 在 JSON value 层重写绝对路径前缀，
// 不做无边界字符串替换；重编码后的每个 object 仍由 strict raw parser 完整校验。
func reviewFreezeCompileDirectMaterialRewriteGoListRawV1(t *testing.T, raw []byte, rewrites []reviewFreezeCompileDirectMaterialPathRewriteV1) []byte {
	t.Helper()
	messages := reviewFreezeTestCompileAttestationGoListRawMessagesV1(t, raw)
	for index, message := range messages {
		decoder := json.NewDecoder(bytes.NewReader(message))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("decode go list raw path rewrite: %v", err)
		}
		value = reviewFreezeCompileDirectMaterialRewriteJSONValueV1(value, rewrites)
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encode go list raw path rewrite: %v", err)
		}
		messages[index] = encoded
	}
	return reviewFreezeTestCompileAttestationGoListRawJoinV1(messages)
}

// reviewFreezeCompileDirectMaterialRewriteJSONValueV1 递归处理 JSON string；只有等于
// root 或位于 root 路径分隔符之下的值才会改写，普通文档文本和 import path 不受影响。
func reviewFreezeCompileDirectMaterialRewriteJSONValueV1(value any, rewrites []reviewFreezeCompileDirectMaterialPathRewriteV1) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = reviewFreezeCompileDirectMaterialRewriteJSONValueV1(child, rewrites)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = reviewFreezeCompileDirectMaterialRewriteJSONValueV1(child, rewrites)
		}
		return typed
	case string:
		for _, rewrite := range rewrites {
			if typed == rewrite.From {
				return rewrite.To
			}
			if strings.HasPrefix(typed, rewrite.From+"/") {
				return rewrite.To + strings.TrimPrefix(typed, rewrite.From)
			}
		}
		return typed
	default:
		return value
	}
}

// reviewFreezeCompileDirectMaterialBindRoleV1 更新 direct role 的 typed ref 及 statement
// 内部交叉绑定。它不改变任何 semantic projection，供“hash 正确但语义漂移”测试使用。
func reviewFreezeCompileDirectMaterialBindRoleV1(statement *reviewFreezeValidatorCompileAttestationV1, role string, raw []byte) {
	switch role {
	case reviewFreezeAttestationMaterialRoleBuildClosureV1:
		statement.Subject.BuildClosureProjectionRef = reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.Subject.BuildClosureProjectionRef, raw)
	case reviewFreezeAttestationMaterialRoleBuildInfoV1:
		ref := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.Compile.BuildInfoRawRef, raw)
		statement.BuilderRun.Compile.BuildInfoRawRef = ref
		statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSHA256 = ref.SHA256
		statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSizeBytes = ref.SizeBytes
	case reviewFreezeAttestationMaterialRoleArtifactV1:
		ref := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.Compile.ArtifactRef, raw)
		statement.BuilderRun.Compile.ArtifactRef = ref
		statement.BuilderRun.Test.PreExecutionArtifactSHA256 = ref.SHA256
		statement.BuilderRun.Test.PostExecutionArtifactSHA256 = ref.SHA256
		statement.BuilderRun.SBOM.SubjectArtifactSHA256 = ref.SHA256
	case reviewFreezeAttestationMaterialRoleGoListV1:
		ref := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.GoListRawRef, raw)
		statement.BuilderRun.GoListRawRef = ref
		statement.BuilderRun.GoListInvocation.StdoutSHA256 = ref.SHA256
		statement.BuilderRun.GoListInvocation.StdoutSizeBytes = ref.SizeBytes
	case reviewFreezeAttestationMaterialRoleSnapshotV1:
		ref := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.InputSnapshotBeforeRef, raw)
		statement.BuilderRun.InputSnapshotBeforeRef = ref
		statement.BuilderRun.InputSnapshotAfterRef = ref
	case reviewFreezeAttestationMaterialRoleSBOMBinaryV1:
		statement.BuilderRun.SBOM.GeneratorBinaryRef = reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.SBOM.GeneratorBinaryRef, raw)
	case reviewFreezeAttestationMaterialRoleSBOMRawV1:
		ref := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.SBOM.RawRef, raw)
		statement.BuilderRun.SBOM.RawRef = ref
		statement.BuilderRun.SBOM.Invocation.StdoutSHA256 = ref.SHA256
		statement.BuilderRun.SBOM.Invocation.StdoutSizeBytes = ref.SizeBytes
	default:
		panic("unknown direct material role: " + role)
	}
}

// reviewFreezeCompileDirectMaterialFixtureReplaceRoleV1 克隆 fixture、替换一个 role 并
// 重新冻结受影响的 snapshot/statement/descriptor。snapshot 自身漂移时保留畸变内容，
// 使 admission 而非 bundle hash 门禁负责拒绝。
func reviewFreezeCompileDirectMaterialFixtureReplaceRoleV1(t *testing.T, base reviewFreezeCompileDirectMaterialFixtureV1, role string, raw []byte) reviewFreezeCompileDirectMaterialFixtureV1 {
	t.Helper()
	statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base.Statement)
	roleBytes := reviewFreezeCompileDirectMaterialCloneRoleBytesV1(base.RoleBytes)
	roleBytes[role] = append([]byte(nil), raw...)
	reviewFreezeCompileDirectMaterialBindRoleV1(&statement, role, raw)
	if role != reviewFreezeAttestationMaterialRoleSnapshotV1 {
		snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1])
		snapshot.Subject = statement.Subject
		snapshot.ExternalModules = statement.ExternalModules
		snapshot.Environment = statement.Environment
		snapshot.Toolchain = statement.Toolchain
		snapshot.ExecutionPolicy = reviewFreezeCompileInputSnapshotPolicyFromStatementV1(statement)
		snapshotRaw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
		roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1] = snapshotRaw
		reviewFreezeCompileDirectMaterialBindRoleV1(&statement, reviewFreezeAttestationMaterialRoleSnapshotV1, snapshotRaw)
	}
	return reviewFreezeCompileDirectMaterialFixtureBuildV1(t, statement, roleBytes)
}

// reviewFreezeCompileDirectMaterialCloneRoleBytesV1 深复制全部 role 字节，防止子测试的
// 对抗变体互相污染。
func reviewFreezeCompileDirectMaterialCloneRoleBytesV1(source map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(source))
	for role, raw := range source {
		cloned[role] = append([]byte(nil), raw...)
	}
	return cloned
}

// reviewFreezeCompileDirectMaterialFixtureBuildV1 对更新后的 statement 做完整 strict
// 校验，并从 role bytes 重新生成 descriptor/CAS map；失败表示测试变体没有隔离目标层。
func reviewFreezeCompileDirectMaterialFixtureBuildV1(t *testing.T, statement reviewFreezeValidatorCompileAttestationV1, roleBytes map[string][]byte) reviewFreezeCompileDirectMaterialFixtureV1 {
	t.Helper()
	statementRaw := reviewFreezeCompileAttestationFixtureMarshalV1(t, statement)
	if _, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw); err != nil {
		t.Fatalf("direct material fixture statement invalid: %v", err)
	}
	descriptor, err := reviewFreezeAttestationMaterialBundleDescriptorV1(statementRaw, statement)
	if err != nil {
		t.Fatalf("build direct material descriptor: %v", err)
	}
	materials := make(map[string][]byte, len(descriptor.Entries))
	for _, entry := range descriptor.Entries {
		raw, exists := roleBytes[entry.Role]
		if !exists {
			t.Fatalf("direct material fixture role bytes missing=%q", entry.Role)
		}
		if reviewFreezeSHA256V1(raw) != entry.Ref.SHA256 || int64(len(raw)) != entry.Ref.SizeBytes {
			t.Fatalf("direct material fixture role ref mismatch=%q", entry.Role)
		}
		materials[entry.Ref.SHA256] = append([]byte(nil), raw...)
	}
	return reviewFreezeCompileDirectMaterialFixtureV1{
		DescriptorRaw: reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor),
		Statement:     statement,
		StatementRaw:  statementRaw,
		RoleBytes:     reviewFreezeCompileDirectMaterialCloneRoleBytesV1(roleBytes),
		Materials:     materials,
	}
}

// reviewFreezeCompileDirectMaterialFixtureVerifyV1 先运行 bundle 的单次加载、hash/size
// 验证，再把 immutable 结果交给 semantic admission。
func reviewFreezeCompileDirectMaterialFixtureVerifyV1(t *testing.T, fixture reviewFreezeCompileDirectMaterialFixtureV1) *reviewFreezeVerifiedAttestationMaterialBundleV1 {
	t.Helper()
	loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
	verified, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), fixture.DescriptorRaw, fixture.StatementRaw, loader)
	if err != nil {
		t.Fatalf("verify direct material fixture bundle: %v", err)
	}
	return verified
}

// reviewFreezeCompileDirectMaterialAdmissionCloneV1 深复制结论中的 exact-set，供 gap
// 篡改测试使用。
func reviewFreezeCompileDirectMaterialAdmissionCloneV1(source reviewFreezeCompileDirectMaterialAdmissionV1) reviewFreezeCompileDirectMaterialAdmissionV1 {
	cloned := source
	cloned.SemanticallyBoundRoles = append([]string(nil), source.SemanticallyBoundRoles...)
	cloned.HashSizeOnlyRoles = append([]string(nil), source.HashSizeOnlyRoles...)
	cloned.ClosedSemanticGaps = append([]string(nil), source.ClosedSemanticGaps...)
	cloned.OpenGaps = append([]string(nil), source.OpenGaps...)
	return cloned
}

// reviewFreezeCompileDirectMaterialContainsV1 判断 exact-set 中是否保留指定边界。
func reviewFreezeCompileDirectMaterialContainsV1(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestW2ReviewFreezeCompileDirectMaterialPartialAdmissionV1(t *testing.T) {
	fixture := reviewFreezeCompileDirectMaterialFixtureNewV1(t)
	verified := reviewFreezeCompileDirectMaterialFixtureVerifyV1(t, fixture)
	result, err := reviewFreezeAdmitCompileDirectMaterialsV1(verified)
	if err != nil {
		t.Fatalf("valid direct material partial admission rejected: %v", err)
	}
	if err := reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(result); err != nil {
		t.Fatalf("valid direct material boundary rejected: %v", err)
	}
	if result.Authority || result.FormalFreezeEligible || result.Decision != reviewFreezeCompileDirectMaterialDecisionPartialV1 {
		t.Fatalf("direct material result overclaimed=%+v", result)
	}
}

func TestW2ReviewFreezeCompileDirectMaterialSemanticAndContextAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileDirectMaterialFixtureNewV1(t)

	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, base.RoleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1])
	snapshot.ExecutionPolicy.NetworkPolicy = "allow"
	driftedSnapshot := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	driftedGoList := reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, base.RoleBytes[reviewFreezeAttestationMaterialRoleGoListV1], "bytes", func(object map[string]any) {
		files := append(object["GoFiles"].([]any), "zz_semantic_drift.go")
		object["GoFiles"] = files
	})
	driftedBuildInfo := bytes.Replace(base.RoleBytes[reviewFreezeAttestationMaterialRoleBuildInfoV1], []byte("GOAMD64=v1"), []byte("GOAMD64=v2"), 1)
	driftedContext := bytes.ReplaceAll(base.RoleBytes[reviewFreezeAttestationMaterialRoleGoListV1], []byte("/opt/go"), []byte("/drift/go"))
	missingInfoTime := reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, base.RoleBytes[reviewFreezeAttestationMaterialRoleGoListV1], "golang.org/x/text/transform", func(object map[string]any) {
		delete(object["Module"].(map[string]any), "Time")
	})
	missingInfoTime = reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, missingInfoTime, "golang.org/x/text/unicode/norm", func(object map[string]any) {
		delete(object["Module"].(map[string]any), "Time")
	})

	tests := []struct {
		name string
		role string
		raw  []byte
		want string
	}{
		{name: "snapshot semantic drift", role: reviewFreezeAttestationMaterialRoleSnapshotV1, raw: driftedSnapshot, want: "execution_policy"},
		{name: "go list semantic drift", role: reviewFreezeAttestationMaterialRoleGoListV1, raw: driftedGoList, want: "projection mismatch"},
		{name: "build info semantic drift", role: reviewFreezeAttestationMaterialRoleBuildInfoV1, raw: driftedBuildInfo, want: "build setting drift"},
		{name: "statement-derived raw context drift", role: reviewFreezeAttestationMaterialRoleGoListV1, raw: driftedContext, want: "go list raw"},
		{name: "snapshot info raw time mismatch", role: reviewFreezeAttestationMaterialRoleGoListV1, raw: missingInfoTime, want: "snapshot .info/raw Time mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeCompileDirectMaterialFixtureReplaceRoleV1(t, base, test.role, test.raw)
			verified := reviewFreezeCompileDirectMaterialFixtureVerifyV1(t, fixture)
			_, err := reviewFreezeAdmitCompileDirectMaterialsV1(verified)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("semantic/context drift error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileDirectMaterialHashOnlyBoundaryV1(t *testing.T) {
	base := reviewFreezeCompileDirectMaterialFixtureNewV1(t)
	tests := []struct {
		name string
		role string
		raw  []byte
		gap  string
	}{
		{name: "build closure unknown semantics", role: reviewFreezeAttestationMaterialRoleBuildClosureV1, raw: []byte(`{"schema_version":"unknown"}`), gap: reviewFreezeCompileDirectMaterialGapBuildClosureV1},
		{name: "artifact not executable", role: reviewFreezeAttestationMaterialRoleArtifactV1, raw: []byte("not-an-ELF-binary"), gap: reviewFreezeCompileDirectMaterialGapArtifactV1},
		{name: "SBOM generator not executable", role: reviewFreezeAttestationMaterialRoleSBOMBinaryV1, raw: []byte("not-an-SBOM-generator"), gap: reviewFreezeCompileDirectMaterialGapSBOMBinaryV1},
		{name: "SBOM unknown semantics", role: reviewFreezeAttestationMaterialRoleSBOMRawV1, raw: []byte(`{"bomFormat":"unknown"}`), gap: reviewFreezeCompileDirectMaterialGapSBOMRawV1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeCompileDirectMaterialFixtureReplaceRoleV1(t, base, test.role, test.raw)
			verified := reviewFreezeCompileDirectMaterialFixtureVerifyV1(t, fixture)
			result, err := reviewFreezeAdmitCompileDirectMaterialsV1(verified)
			if err != nil {
				t.Fatalf("hash/size-only role should remain partial: %v", err)
			}
			if !reviewFreezeCompileDirectMaterialContainsV1(result.HashSizeOnlyRoles, test.role) ||
				!reviewFreezeCompileDirectMaterialContainsV1(result.OpenGaps, test.gap) ||
				result.Authority || result.FormalFreezeEligible {
				t.Fatalf("hash-only role overclaimed role=%q result=%+v", test.role, result)
			}
		})
	}
}

func TestW2ReviewFreezeCompileDirectMaterialGapExactSetAdversarialV1(t *testing.T) {
	valid := reviewFreezeCompileDirectMaterialExpectedAdmissionV1()
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileDirectMaterialAdmissionV1)
	}{
		{name: "delete open gap", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.OpenGaps = append([]string(nil), result.OpenGaps[1:]...)
		}},
		{name: "falsely close artifact gap", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.OpenGaps = append([]string(nil), result.OpenGaps[:2]...)
			result.OpenGaps = append(result.OpenGaps, valid.OpenGaps[3:]...)
			result.ClosedSemanticGaps = append(result.ClosedSemanticGaps, reviewFreezeCompileDirectMaterialGapArtifactV1)
		}},
		{name: "promote hash-only role", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.SemanticallyBoundRoles = append(result.SemanticallyBoundRoles, reviewFreezeAttestationMaterialRoleArtifactV1)
			result.HashSizeOnlyRoles = result.HashSizeOnlyRoles[1:]
		}},
		{name: "claim authority decision", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.Decision = "formal_freeze_authority"
		}},
		{name: "claim authority boolean", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.Authority = true
		}},
		{name: "claim Formal Freeze eligible", mutate: func(result *reviewFreezeCompileDirectMaterialAdmissionV1) {
			result.FormalFreezeEligible = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := reviewFreezeCompileDirectMaterialAdmissionCloneV1(valid)
			test.mutate(&mutated)
			if err := reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(mutated); err == nil {
				t.Fatalf("overclaimed direct material boundary accepted=%+v", mutated)
			}
		})
	}
}
