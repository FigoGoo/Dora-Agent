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

const (
	reviewFreezeCompileLeafArtifactClosedSnapshotRawV1 = "input_snapshot_raw_exact_bytes_and_strict_json_semantics_bound"
	reviewFreezeCompileLeafArtifactClosedRepositoryV1  = "repository_14_leaf_blob_identity_verified"
	reviewFreezeCompileLeafArtifactClosedModuleV1      = "module_cache_15_leaf_identity_and_internal_semantics_verified"
	reviewFreezeCompileLeafArtifactClosedBuildInfoV1   = "compiled_artifact_embedded_build_info_semantics_bound"

	reviewFreezeCompileLeafArtifactGapArtifactSourceV1 = "compiled_test_binary_source_derivation_unverified"
	reviewFreezeCompileLeafArtifactGapBuilderRunV1     = "builder_execution_unverified"
	reviewFreezeCompileLeafArtifactGapBuilderPairV1    = "independent_builder_pair_unverified"
	reviewFreezeCompileLeafArtifactGapSignatureV1      = "signature_and_trust_root_unverified"
)

// reviewFreezeCompileLeafArtifactAdmissionV1 是四段验证器对同一份 verified direct
// material bundle 得出的不可变测试结论。它只关闭 snapshot raw、repository blob
// identity、module-cache leaf 内部关系和 binary/BuildInfo 语义；所有来源、执行和信任
// authority 字段必须保持 false。
type reviewFreezeCompileLeafArtifactAdmissionV1 struct {
	decision                          string
	statementSHA256                   string
	snapshotSHA256                    string
	artifactBuildInfoProjectionSHA256 string
	repositoryPaths                   []string
	modulePaths                       []string
	closedSemanticGaps                []string
	openGaps                          []string
	repositoryLeafCount               int
	moduleLeafCount                   int
	moduleGoCommandInputCount         int
	moduleAcquisitionEvidenceCount    int
	moduleMaterializationCount        int
	snapshotRawBound                  bool
	repositoryBlobIdentityVerified    bool
	moduleLeafSemanticsVerified       bool
	artifactBuildInfoSemanticsBound   bool
	repositoryBaseTreeMembership      bool
	repositoryCommitAncestry          bool
	artifactSourceDerivation          bool
	builderExecutionProven            bool
	authority                         bool
	formalFreezeEligible              bool
}

// reviewFreezeCompileLeafArtifactComponentsV1 冻结一次 leaf/artifact admission
// 调用已经验证的可复用组件。raw bytes 使用 string 保存，repository/admission 只通过
// 深复制 accessor 暴露，后续组合层可以复用同一次 loader 结果而不能修改本对象。
type reviewFreezeCompileLeafArtifactComponentsV1 struct {
	admission        *reviewFreezeCompileLeafArtifactAdmissionV1
	statementRaw     string
	snapshotRaw      string
	repositoryLeaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1
}

// Admission 返回本次组件调用形成的 admission 深副本；该方法不会重新访问 loader。
func (components *reviewFreezeCompileLeafArtifactComponentsV1) Admission() *reviewFreezeCompileLeafArtifactAdmissionV1 {
	if components == nil {
		return nil
	}
	return reviewFreezeCompileLeafArtifactResultCloneV1(components.admission)
}

// StatementRaw 返回已经过 strict decode 且参与组合 admission 的 statement raw 副本。
func (components *reviewFreezeCompileLeafArtifactComponentsV1) StatementRaw() []byte {
	if components == nil {
		return nil
	}
	return []byte(components.statementRaw)
}

// SnapshotRaw 返回已经与 statement before/after ref 绑定的 snapshot raw 副本。
func (components *reviewFreezeCompileLeafArtifactComponentsV1) SnapshotRaw() []byte {
	if components == nil {
		return nil
	}
	return []byte(components.snapshotRaw)
}

// RepositoryLeaves 返回同一次 admission 已验证的 repository leaf bundle 深副本；
// 读取或修改该副本不会再次打开 loader，也不会污染 components 内部结果。
func (components *reviewFreezeCompileLeafArtifactComponentsV1) RepositoryLeaves() *reviewFreezeVerifiedCompileRepositoryLeafBundleV1 {
	if components == nil {
		return nil
	}
	return reviewFreezeCompileLeafArtifactRepositoryBundleCloneV1(components.repositoryLeaves)
}

// RepositoryPaths 返回已验证 repository leaf path 的有序副本；调用方修改结果不会
// 改变 admission 内部 exact-set。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) RepositoryPaths() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.repositoryPaths...)
}

// ModulePaths 返回已验证 module-cache leaf path 的有序副本；该方法不会
// 重新访问任何 loader。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) ModulePaths() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.modulePaths...)
}

// ClosedSemanticGaps 返回本阶段诚实关闭的语义 gap exact-set 副本。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) ClosedSemanticGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.closedSemanticGaps...)
}

// OpenGaps 返回仍阻止 Formal Freeze 的 gap exact-set 副本。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) OpenGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.openGaps...)
}

// Authority 返回该测试候选是否具有签名或发布 authority；本阶段恒为 false。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) Authority() bool {
	return result != nil && result.authority
}

// FormalFreezeEligible 返回该测试候选是否满足 Formal Freeze；本阶段恒为 false。
func (result *reviewFreezeCompileLeafArtifactAdmissionV1) FormalFreezeEligible() bool {
	return result != nil && result.formalFreezeEligible
}

// reviewFreezeCompileLeafArtifactClosedGapsV1 固定组合准入真正关闭的 exact-set。前三项
// 来自 direct semantic admission；后四项分别把 snapshot raw、两组叶子和 artifact
// embedded BuildInfo 收敛到同一 statement，但不扩张证明边界。
func reviewFreezeCompileLeafArtifactClosedGapsV1() []string {
	return []string{
		"build_info_raw_text_semantics_bound",
		"go_list_raw_projection_semantics_bound",
		"input_snapshot_json_semantics_bound",
		reviewFreezeCompileLeafArtifactClosedSnapshotRawV1,
		reviewFreezeCompileLeafArtifactClosedRepositoryV1,
		reviewFreezeCompileLeafArtifactClosedModuleV1,
		reviewFreezeCompileLeafArtifactClosedBuildInfoV1,
	}
}

// reviewFreezeCompileLeafArtifactOpenGapsV1 固定剩余 gap exact-set。repository blob
// identity 不能替代 Git tree/commit membership；artifact BuildInfo 语义也不能替代
// builder execution、source derivation、SBOM、toolchain 或签名信任链。
func reviewFreezeCompileLeafArtifactOpenGapsV1() []string {
	return []string{
		reviewFreezeCompileRepositoryLeafBaseTreeGapV1,
		reviewFreezeCompileRepositoryLeafCommitAncestryGapV1,
		reviewFreezeCompileLeafArtifactGapArtifactSourceV1,
		reviewFreezeCompileLeafArtifactGapBuilderRunV1,
		reviewFreezeCompileDirectMaterialGapBuildClosureV1,
		reviewFreezeCompileDirectMaterialGapSBOMBinaryV1,
		reviewFreezeCompileDirectMaterialGapSBOMRawV1,
		reviewFreezeCompileDirectMaterialGapSBOMDeriveV1,
		reviewFreezeCompileDirectMaterialGapToolchainV1,
		reviewFreezeCompileLeafArtifactGapBuilderPairV1,
		reviewFreezeCompileLeafArtifactGapSignatureV1,
	}
}

// reviewFreezeAdmitCompileLeafArtifactV1 保持原 admission 入口兼容；内部只执行一次
// components helper，再返回不可变 admission 副本，不重复访问 repository/module loader。
func reviewFreezeAdmitCompileLeafArtifactV1(
	ctx context.Context,
	verified *reviewFreezeVerifiedAttestationMaterialBundleV1,
	repositoryLoader reviewFreezeCompileRepositoryLeafLoaderV1,
	moduleLoader reviewFreezeCompileModuleLeafLoaderV1,
) (*reviewFreezeCompileLeafArtifactAdmissionV1, error) {
	components, err := reviewFreezeAdmitCompileLeafArtifactComponentsV1(ctx, verified, repositoryLoader, moduleLoader)
	if err != nil {
		return nil, err
	}
	return components.Admission(), nil
}

// reviewFreezeAdmitCompileLeafArtifactComponentsV1 按固定顺序消费同一个 verified
// bundle：先做 direct material semantic admission，再从其冻结的 strict
// statement/snapshot raw 驱动 repository、module-cache 与 artifact BuildInfo verifier。
// 任一步失败都不返回部分组件；成功时每个 loader 仍只 List 一次、每个 leaf 只 Open 一次。
func reviewFreezeAdmitCompileLeafArtifactComponentsV1(
	ctx context.Context,
	verified *reviewFreezeVerifiedAttestationMaterialBundleV1,
	repositoryLoader reviewFreezeCompileRepositoryLeafLoaderV1,
	moduleLoader reviewFreezeCompileModuleLeafLoaderV1,
) (*reviewFreezeCompileLeafArtifactComponentsV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile leaf/artifact admission context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact admission context: %w", err)
	}
	if verified == nil {
		return nil, fmt.Errorf("compile leaf/artifact verified bundle 不能为空")
	}

	direct, err := reviewFreezeAdmitCompileDirectMaterialsV1(verified)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact direct admission: %w", err)
	}
	if err := reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(direct); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact direct boundary: %w", err)
	}

	statementRaw := verified.StatementRaw()
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact strict statement: %w", err)
	}
	snapshotRef, snapshotRaw, exists := verified.Material(reviewFreezeAttestationMaterialRoleSnapshotV1)
	if !exists {
		return nil, fmt.Errorf("compile leaf/artifact input snapshot material 缺失")
	}
	if !reflect.DeepEqual(snapshotRef, statement.BuilderRun.InputSnapshotBeforeRef) ||
		!reflect.DeepEqual(snapshotRef, statement.BuilderRun.InputSnapshotAfterRef) {
		return nil, fmt.Errorf("compile leaf/artifact input snapshot before/after ref 不一致")
	}
	if snapshotRef.SHA256 != reviewFreezeSHA256V1(snapshotRaw) || snapshotRef.SizeBytes != int64(len(snapshotRaw)) {
		return nil, fmt.Errorf("compile leaf/artifact input snapshot raw ref 绑定失败")
	}

	repository, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(ctx, snapshotRaw, statement, repositoryLoader)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact repository leaves: %w", err)
	}
	modules, err := reviewFreezeResolveCompileModuleLeafBundleV1(ctx, snapshotRaw, statement, moduleLoader)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact module leaves: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact context before artifact semantics: %w", err)
	}
	artifact, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoV1(verified)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact binary BuildInfo: %w", err)
	}

	admission, err := reviewFreezeAssembleCompileLeafArtifactAdmissionV1(
		statementRaw,
		snapshotRaw,
		direct,
		repository,
		modules,
		artifact,
	)
	if err != nil {
		return nil, err
	}
	components := &reviewFreezeCompileLeafArtifactComponentsV1{
		admission:        reviewFreezeCompileLeafArtifactResultCloneV1(admission),
		statementRaw:     string(statementRaw),
		snapshotRaw:      string(snapshotRaw),
		repositoryLeaves: reviewFreezeCompileLeafArtifactRepositoryBundleCloneV1(repository),
	}
	if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(components); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact components boundary: %w", err)
	}
	return components, nil
}

// reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1 校验 components 只包含
// 同一 statement/snapshot 形成的非空 admission 与 repository bundle。它不访问 loader，
// 也不关闭任何新增 claim。
func reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(components *reviewFreezeCompileLeafArtifactComponentsV1) error {
	if components == nil {
		return fmt.Errorf("compile leaf/artifact components 不能为空")
	}
	if components.admission == nil || components.repositoryLeaves == nil || components.statementRaw == "" || components.snapshotRaw == "" {
		return fmt.Errorf("compile leaf/artifact components admission/raw/repository 不能为空")
	}
	statementRaw := []byte(components.statementRaw)
	snapshotRaw := []byte(components.snapshotRaw)
	if err := reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(components.admission, statementRaw, snapshotRaw); err != nil {
		return fmt.Errorf("compile leaf/artifact components admission: %w", err)
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return fmt.Errorf("compile leaf/artifact components statement: %w", err)
	}
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(snapshotRaw, statement); err != nil {
		return fmt.Errorf("compile leaf/artifact components snapshot: %w", err)
	}
	var snapshot reviewFreezeCompileInputSnapshotV1
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		return fmt.Errorf("compile leaf/artifact components decode snapshot: %w", err)
	}
	repositoryPaths, err := reviewFreezeValidateCompileLeafArtifactRepositoryResultV1(snapshot, components.repositoryLeaves)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(repositoryPaths, components.admission.repositoryPaths) {
		return fmt.Errorf("compile leaf/artifact components repository/admission paths 错配=%v/%v", repositoryPaths, components.admission.repositoryPaths)
	}
	return nil
}

// reviewFreezeValidateCompileLeafArtifactRepositoryResultV1 把已验证 repository 子结果
// 重新绑定到同一 strict snapshot。该纯内存边界由 assemble 与 components 共同复用，
// 不会重新 List/Open repository loader。
func reviewFreezeValidateCompileLeafArtifactRepositoryResultV1(
	snapshot reviewFreezeCompileInputSnapshotV1,
	repository *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
) ([]string, error) {
	if repository == nil {
		return nil, fmt.Errorf("compile leaf/artifact repository result 不能为空")
	}
	repositoryPaths := repository.Paths()
	if !reflect.DeepEqual(repositoryPaths, reviewFreezeCompileInputSnapshotRepositoryPathsV1()) {
		return nil, fmt.Errorf("compile leaf/artifact repository result paths=%v", repositoryPaths)
	}
	repositoryScope := repository.Scope()
	if repositoryScope.VerifiedClaim != reviewFreezeCompileRepositoryLeafVerifiedClaimV1 ||
		repositoryScope.FormalFreezeStatus != reviewFreezeCompileRepositoryLeafFormalFreezeStatusV1 ||
		!reflect.DeepEqual(repositoryScope.OpenGaps, []string{
			reviewFreezeCompileRepositoryLeafBaseTreeGapV1,
			reviewFreezeCompileRepositoryLeafCommitAncestryGapV1,
		}) {
		return nil, fmt.Errorf("compile leaf/artifact repository scope 漂移=%+v", repositoryScope)
	}
	repositoryByPath := make(map[string]reviewFreezeCompileInputSnapshotRepoFileV1, len(snapshot.RepositoryFiles))
	for _, leaf := range snapshot.RepositoryFiles {
		repositoryByPath[leaf.Path] = leaf
	}
	repositoryTotal := int64(0)
	for _, path := range repositoryPaths {
		want, exists := repositoryByPath[path]
		if !exists {
			return nil, fmt.Errorf("compile leaf/artifact repository result path 不属于 snapshot=%q", path)
		}
		metadata, raw, exists := repository.Leaf(path)
		if !exists {
			return nil, fmt.Errorf("compile leaf/artifact repository result leaf 缺失=%q", path)
		}
		if !reflect.DeepEqual(metadata, want) {
			return nil, fmt.Errorf("compile leaf/artifact repository metadata 与 snapshot 错配 path=%q", path)
		}
		if int64(len(raw)) != want.SizeBytes || reviewFreezeSHA256V1(raw) != want.SHA256 || reviewFreezeCompileRepositoryLeafGitBlobSHAV1(raw) != want.GitBlobSHA {
			return nil, fmt.Errorf("compile leaf/artifact repository bytes 与 snapshot 错配 path=%q", path)
		}
		repositoryTotal += int64(len(raw))
	}
	if repository.TotalBytes() != repositoryTotal {
		return nil, fmt.Errorf("compile leaf/artifact repository total bytes=%d want=%d", repository.TotalBytes(), repositoryTotal)
	}
	return repositoryPaths, nil
}

// reviewFreezeAssembleCompileLeafArtifactAdmissionV1 只组合已验证子结果，并再次执行
// cross-result exact-set 校验。该步骤拒绝 loader 结果属于不同 snapshot、子结果计数漂移
// 或 artifact validator 将语义结果升级为 builder/signature authority。
func reviewFreezeAssembleCompileLeafArtifactAdmissionV1(
	statementRaw []byte,
	snapshotRaw []byte,
	direct reviewFreezeCompileDirectMaterialAdmissionV1,
	repository *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
	modules *reviewFreezeCompileModuleLeafBundleV1,
	artifact reviewFreezeCompileAttestationArtifactBuildInfoResultV1,
) (*reviewFreezeCompileLeafArtifactAdmissionV1, error) {
	if err := reviewFreezeValidateCompileDirectMaterialAdmissionBoundaryV1(direct); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact assemble direct result: %w", err)
	}
	if repository == nil || modules == nil {
		return nil, fmt.Errorf("compile leaf/artifact assemble leaf bundle 不能为空")
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return nil, fmt.Errorf("compile leaf/artifact assemble statement: %w", err)
	}
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(snapshotRaw, statement); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact assemble snapshot: %w", err)
	}
	var snapshot reviewFreezeCompileInputSnapshotV1
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		return nil, fmt.Errorf("compile leaf/artifact decode verified snapshot: %w", err)
	}

	repositoryPaths, err := reviewFreezeValidateCompileLeafArtifactRepositoryResultV1(snapshot, repository)
	if err != nil {
		return nil, err
	}
	modulePaths := modules.Paths()
	if !reflect.DeepEqual(modulePaths, reviewFreezeCompileInputSnapshotModulePathsV1()) {
		return nil, fmt.Errorf("compile leaf/artifact module result paths=%v", modulePaths)
	}
	if artifact.BuilderExecutionProven || artifact.ArtifactSourceClosureProven || artifact.SignatureAuthority {
		return nil, fmt.Errorf("compile leaf/artifact artifact result 越权 execution/source/signature=%v/%v/%v", artifact.BuilderExecutionProven, artifact.ArtifactSourceClosureProven, artifact.SignatureAuthority)
	}
	if artifact.ProjectionSHA256 != statement.BuilderRun.Compile.BuildInfoProjectionSHA256 ||
		!reflect.DeepEqual(artifact.Projection, statement.BuilderRun.Compile.BuildInfoProjection) {
		return nil, fmt.Errorf("compile leaf/artifact artifact projection 与 statement 错配")
	}

	purposeCounts := make(map[string]int, 3)
	moduleByPath := make(map[string]reviewFreezeCompileInputSnapshotModuleFileV1, len(snapshot.ModuleCacheFiles))
	for _, leaf := range snapshot.ModuleCacheFiles {
		moduleByPath[leaf.Path] = leaf
	}
	for _, path := range modulePaths {
		leaf, exists := moduleByPath[path]
		if !exists {
			return nil, fmt.Errorf("compile leaf/artifact module result path 不属于 snapshot=%q", path)
		}
		raw, exists := modules.Bytes(path)
		if !exists {
			return nil, fmt.Errorf("compile leaf/artifact module result bytes 缺失=%q", path)
		}
		if int64(len(raw)) != leaf.SizeBytes || reviewFreezeSHA256V1(raw) != leaf.SHA256 {
			return nil, fmt.Errorf("compile leaf/artifact module bytes 与 snapshot 错配 path=%q", path)
		}
		purposeCounts[leaf.Purpose]++
	}

	result := &reviewFreezeCompileLeafArtifactAdmissionV1{
		decision:                          reviewFreezeCompileDirectMaterialDecisionPartialV1,
		statementSHA256:                   reviewFreezeSHA256V1(statementRaw),
		snapshotSHA256:                    reviewFreezeSHA256V1(snapshotRaw),
		artifactBuildInfoProjectionSHA256: artifact.ProjectionSHA256,
		repositoryPaths:                   append([]string(nil), repositoryPaths...),
		modulePaths:                       append([]string(nil), modulePaths...),
		closedSemanticGaps:                reviewFreezeCompileLeafArtifactClosedGapsV1(),
		openGaps:                          reviewFreezeCompileLeafArtifactOpenGapsV1(),
		repositoryLeafCount:               len(repositoryPaths),
		moduleLeafCount:                   len(modulePaths),
		moduleGoCommandInputCount:         purposeCounts[reviewFreezeCompileInputSnapshotModuleGoInputV1],
		moduleAcquisitionEvidenceCount:    purposeCounts[reviewFreezeCompileInputSnapshotModuleAcquisitionV1],
		moduleMaterializationCount:        purposeCounts[reviewFreezeCompileInputSnapshotModuleMaterializationEvidenceV1],
		snapshotRawBound:                  true,
		repositoryBlobIdentityVerified:    true,
		moduleLeafSemanticsVerified:       true,
		artifactBuildInfoSemanticsBound:   true,
		// blob identity、embedded BuildInfo 和 cache leaf 内部关系都不证明下面这些 authority。
		repositoryBaseTreeMembership: false,
		repositoryCommitAncestry:     false,
		artifactSourceDerivation:     false,
		builderExecutionProven:       false,
		authority:                    false,
		formalFreezeEligible:         false,
	}
	if err := reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(result, statementRaw, snapshotRaw); err != nil {
		return nil, err
	}
	return result, nil
}

// reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1 对组合结果做完整 exact-set
// 校验。删减任何 open gap、改变 path/count/digest，或把任一来源/authority 字段提权，
// 都会失败关闭。
func reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(
	result *reviewFreezeCompileLeafArtifactAdmissionV1,
	statementRaw []byte,
	snapshotRaw []byte,
) error {
	if result == nil {
		return fmt.Errorf("compile leaf/artifact admission result 不能为空")
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return fmt.Errorf("compile leaf/artifact admission boundary statement: %w", err)
	}
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(snapshotRaw, statement); err != nil {
		return fmt.Errorf("compile leaf/artifact admission boundary snapshot: %w", err)
	}
	if result.decision != reviewFreezeCompileDirectMaterialDecisionPartialV1 {
		return fmt.Errorf("compile leaf/artifact decision=%q", result.decision)
	}
	if result.statementSHA256 != reviewFreezeSHA256V1(statementRaw) || result.snapshotSHA256 != reviewFreezeSHA256V1(snapshotRaw) {
		return fmt.Errorf("compile leaf/artifact statement/snapshot digest 错配")
	}
	if result.artifactBuildInfoProjectionSHA256 != statement.BuilderRun.Compile.BuildInfoProjectionSHA256 {
		return fmt.Errorf("compile leaf/artifact BuildInfo projection digest 错配")
	}
	if !reflect.DeepEqual(result.repositoryPaths, reviewFreezeCompileInputSnapshotRepositoryPathsV1()) ||
		!reflect.DeepEqual(result.modulePaths, reviewFreezeCompileInputSnapshotModulePathsV1()) {
		return fmt.Errorf("compile leaf/artifact path exact-set 错配 repository=%v module=%v", result.repositoryPaths, result.modulePaths)
	}
	if !reflect.DeepEqual(result.closedSemanticGaps, reviewFreezeCompileLeafArtifactClosedGapsV1()) ||
		!reflect.DeepEqual(result.openGaps, reviewFreezeCompileLeafArtifactOpenGapsV1()) {
		return fmt.Errorf("compile leaf/artifact gap exact-set 错配 closed=%v open=%v", result.closedSemanticGaps, result.openGaps)
	}
	if result.repositoryLeafCount != reviewFreezeCompileRepositoryLeafCountV1 ||
		result.moduleLeafCount != len(reviewFreezeCompileInputSnapshotModulePathsV1()) ||
		result.moduleGoCommandInputCount != reviewFreezeCompileInputSnapshotModuleGoInputCountV1 ||
		result.moduleAcquisitionEvidenceCount != reviewFreezeCompileInputSnapshotModuleAcquisitionCountV1 ||
		result.moduleMaterializationCount != reviewFreezeCompileInputSnapshotModuleMaterializationCountV1 {
		return fmt.Errorf("compile leaf/artifact count 错配 repository/module/purpose=%d/%d/%d/%d/%d", result.repositoryLeafCount, result.moduleLeafCount, result.moduleGoCommandInputCount, result.moduleAcquisitionEvidenceCount, result.moduleMaterializationCount)
	}
	if !result.snapshotRawBound || !result.repositoryBlobIdentityVerified || !result.moduleLeafSemanticsVerified || !result.artifactBuildInfoSemanticsBound {
		return fmt.Errorf("compile leaf/artifact 已关闭语义声明缺失")
	}
	if result.repositoryBaseTreeMembership || result.repositoryCommitAncestry || result.artifactSourceDerivation ||
		result.builderExecutionProven || result.authority || result.formalFreezeEligible {
		return fmt.Errorf("compile leaf/artifact 不得提权 tree/commit/source/execution/authority/freeze=%v/%v/%v/%v/%v/%v", result.repositoryBaseTreeMembership, result.repositoryCommitAncestry, result.artifactSourceDerivation, result.builderExecutionProven, result.authority, result.formalFreezeEligible)
	}
	return nil
}

// reviewFreezeCompileLeafArtifactFixtureV1 聚合四个现有真实 fixture。verified bundle、
// repository loader 与 module loader 最终都绑定到同一份重新冻结的 statement/snapshot。
type reviewFreezeCompileLeafArtifactFixtureV1 struct {
	verified   *reviewFreezeVerifiedAttestationMaterialBundleV1
	repository reviewFreezeCompileRepositoryLeafFixtureV1
	module     *reviewFreezeCompileModuleLeafFixtureV1
	statement  []byte
	snapshot   []byte
}

// reviewFreezeNewCompileLeafArtifactFixtureV1 使用真实 Go 1.26.3 go-list raw、当前 14 个
// repository bytes、固定 x/text@v0.34.0 的 15 个 cache bytes，以及 fresh offline
// `go test -c` ELF 构造组合正例。
func reviewFreezeNewCompileLeafArtifactFixtureV1(t *testing.T) reviewFreezeCompileLeafArtifactFixtureV1 {
	t.Helper()
	direct := reviewFreezeCompileDirectMaterialFixtureNewV1(t)
	repository := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	module := reviewFreezeNewRealCompileModuleLeafFixtureV1(t)
	artifactRaw, buildInfoRaw := reviewFreezeTestCompileArtifactBuildInfoMaterialV1(t)

	statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, direct.Statement)
	if !reflect.DeepEqual(statement.ExternalModules, module.statement.ExternalModules) {
		t.Fatal("combined fixture direct go-list module 与 real module leaf statement 不一致")
	}
	statement.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), module.statement.ExternalModules...)
	// 只采用 repository fixture 的真实 subject identity；随后按 direct role bytes 重新绑定
	// build-closure ref，避免覆盖与 repository 无关的 direct material ref。
	statement.Subject = repository.Statement.Subject
	roleBytes := reviewFreezeCompileDirectMaterialCloneRoleBytesV1(direct.RoleBytes)
	roleBytes[reviewFreezeAttestationMaterialRoleArtifactV1] = append([]byte(nil), artifactRaw...)
	roleBytes[reviewFreezeAttestationMaterialRoleBuildInfoV1] = append([]byte(nil), buildInfoRaw...)
	for _, role := range []string{
		reviewFreezeAttestationMaterialRoleBuildClosureV1,
		reviewFreezeAttestationMaterialRoleBuildInfoV1,
		reviewFreezeAttestationMaterialRoleArtifactV1,
		reviewFreezeAttestationMaterialRoleGoListV1,
		reviewFreezeAttestationMaterialRoleSBOMBinaryV1,
		reviewFreezeAttestationMaterialRoleSBOMRawV1,
	} {
		reviewFreezeCompileDirectMaterialBindRoleV1(&statement, role, roleBytes[role])
	}

	repositorySnapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, repository.SnapshotRaw)
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, direct.RoleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1])
	snapshot.Subject = statement.Subject
	snapshot.ExternalModules = statement.ExternalModules
	snapshot.Environment = statement.Environment
	snapshot.Toolchain = statement.Toolchain
	snapshot.ExecutionPolicy = reviewFreezeCompileInputSnapshotPolicyFromStatementV1(statement)
	snapshot.RepositoryFiles = append([]reviewFreezeCompileInputSnapshotRepoFileV1(nil), repositorySnapshot.RepositoryFiles...)
	snapshot.ModuleCacheFiles = append([]reviewFreezeCompileInputSnapshotModuleFileV1(nil), module.snapshot.ModuleCacheFiles...)
	snapshotRaw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1] = snapshotRaw
	reviewFreezeCompileDirectMaterialBindRoleV1(&statement, reviewFreezeAttestationMaterialRoleSnapshotV1, snapshotRaw)

	combined := reviewFreezeCompileDirectMaterialFixtureBuildV1(t, statement, roleBytes)
	verified := reviewFreezeCompileDirectMaterialFixtureVerifyV1(t, combined)
	_, verifiedSnapshotRaw, exists := verified.Material(reviewFreezeAttestationMaterialRoleSnapshotV1)
	if !exists || !bytes.Equal(verifiedSnapshotRaw, snapshotRaw) {
		t.Fatal("combined fixture verified snapshot raw 与两个 leaf resolver 输入不同源")
	}
	return reviewFreezeCompileLeafArtifactFixtureV1{
		verified:   verified,
		repository: repository,
		module:     module,
		statement:  verified.StatementRaw(),
		snapshot:   append([]byte(nil), snapshotRaw...),
	}
}

// reviewFreezeCompileLeafArtifactResultCloneV1 深复制组合结果，供失败关闭测试隔离修改。
func reviewFreezeCompileLeafArtifactResultCloneV1(source *reviewFreezeCompileLeafArtifactAdmissionV1) *reviewFreezeCompileLeafArtifactAdmissionV1 {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.repositoryPaths = append([]string(nil), source.repositoryPaths...)
	cloned.modulePaths = append([]string(nil), source.modulePaths...)
	cloned.closedSemanticGaps = append([]string(nil), source.closedSemanticGaps...)
	cloned.openGaps = append([]string(nil), source.openGaps...)
	return &cloned
}

// reviewFreezeCompileLeafArtifactRepositoryBundleCloneV1 深复制 repository 子结果，供
// assemble 层验证“相同 path、不同 snapshot bytes/metadata”不能混入组合结论。
func reviewFreezeCompileLeafArtifactRepositoryBundleCloneV1(source *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) *reviewFreezeVerifiedCompileRepositoryLeafBundleV1 {
	if source == nil {
		return nil
	}
	cloned := &reviewFreezeVerifiedCompileRepositoryLeafBundleV1{
		paths:      source.Paths(),
		leaves:     make(map[string]reviewFreezeVerifiedCompileRepositoryLeafV1, len(source.leaves)),
		totalBytes: source.totalBytes,
	}
	for path, leaf := range source.leaves {
		cloned.leaves[path] = leaf
	}
	return cloned
}

// reviewFreezeCompileLeafArtifactModuleBundleCloneV1 深复制 module 子结果，供 assemble
// 层验证同名 leaf 的 bytes 必须仍与该次 verified snapshot 一致。
func reviewFreezeCompileLeafArtifactModuleBundleCloneV1(source *reviewFreezeCompileModuleLeafBundleV1) *reviewFreezeCompileModuleLeafBundleV1 {
	if source == nil {
		return nil
	}
	cloned := &reviewFreezeCompileModuleLeafBundleV1{
		paths: source.Paths(),
		files: make(map[string][]byte, len(source.files)),
	}
	for path, raw := range source.files {
		cloned.files[path] = append([]byte(nil), raw...)
	}
	return cloned
}

func TestW2ReviewFreezeCompileLeafArtifactAdmissionV1(t *testing.T) {
	fixture := reviewFreezeNewCompileLeafArtifactFixtureV1(t)
	newRepositoryLoader := func() *reviewFreezeCompileRepositoryLeafLoaderFixtureV1 {
		return reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture.repository)
	}
	newModuleLoader := func() *reviewFreezeCompileModuleLeafFixtureLoaderV1 {
		return reviewFreezeNewCompileModuleLeafFixtureLoaderV1(t, fixture.module.files, "combined-real-x-text-v0.34.0")
	}
	assertLoaderOnePass := func(
		t *testing.T,
		repositoryLoader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1,
		moduleLoader *reviewFreezeCompileModuleLeafFixtureLoaderV1,
	) {
		t.Helper()
		if repositoryLoader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(repositoryLoader) != reviewFreezeCompileRepositoryLeafCountV1 {
			t.Fatalf("repository loader calls list/open=%d/%d", repositoryLoader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(repositoryLoader))
		}
		for _, path := range reviewFreezeCompileInputSnapshotRepositoryPathsV1() {
			if repositoryLoader.openCalls[path] != 1 {
				t.Fatalf("repository loader path=%q open calls=%d", path, repositoryLoader.openCalls[path])
			}
		}
		moduleOpenCalls := 0
		for _, calls := range moduleLoader.openCalls {
			moduleOpenCalls += calls
		}
		if moduleLoader.listCalls != 1 || moduleOpenCalls != len(reviewFreezeCompileInputSnapshotModulePathsV1()) {
			t.Fatalf("module loader calls list/open=%d/%d", moduleLoader.listCalls, moduleOpenCalls)
		}
		for _, path := range reviewFreezeCompileInputSnapshotModulePathsV1() {
			if moduleLoader.openCalls[path] != 1 {
				t.Fatalf("module loader path=%q open calls=%d", path, moduleLoader.openCalls[path])
			}
		}
	}

	wrapperRepositoryLoader := newRepositoryLoader()
	wrapperModuleLoader := newModuleLoader()
	result, err := reviewFreezeAdmitCompileLeafArtifactV1(context.Background(), fixture.verified, wrapperRepositoryLoader, wrapperModuleLoader)
	if err != nil {
		t.Fatalf("valid compile leaf/artifact admission rejected: %v", err)
	}
	assertLoaderOnePass(t, wrapperRepositoryLoader, wrapperModuleLoader)
	if err := reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(result, fixture.statement, fixture.snapshot); err != nil {
		t.Fatalf("valid compile leaf/artifact boundary rejected: %v", err)
	}
	if result.Authority() || result.FormalFreezeEligible() {
		t.Fatalf("compile leaf/artifact result overclaimed authority/freeze=%v/%v", result.Authority(), result.FormalFreezeEligible())
	}

	// 所有 accessor 都返回副本；mutation 不得删掉内部 path/gap exact-set。
	repositoryPaths := result.RepositoryPaths()
	modulePaths := result.ModulePaths()
	closedGaps := result.ClosedSemanticGaps()
	openGaps := result.OpenGaps()
	repositoryPaths[0], modulePaths[0], closedGaps[0], openGaps[0] = "forged", "forged", "forged", "forged"
	if result.RepositoryPaths()[0] == "forged" || result.ModulePaths()[0] == "forged" || result.ClosedSemanticGaps()[0] == "forged" || result.OpenGaps()[0] == "forged" {
		t.Fatal("compile leaf/artifact result accessor 未提供 immutable copy")
	}

	t.Run("components_match_wrapper_without_reopening_loaders", func(t *testing.T) {
		repositoryLoader := newRepositoryLoader()
		moduleLoader := newModuleLoader()
		components, err := reviewFreezeAdmitCompileLeafArtifactComponentsV1(context.Background(), fixture.verified, repositoryLoader, moduleLoader)
		if err != nil {
			t.Fatalf("valid compile leaf/artifact components rejected: %v", err)
		}
		assertLoaderOnePass(t, repositoryLoader, moduleLoader)
		if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(components); err != nil {
			t.Fatalf("valid compile leaf/artifact components boundary rejected: %v", err)
		}
		componentAdmission := components.Admission()
		if componentAdmission == nil || !reflect.DeepEqual(componentAdmission, result) {
			t.Fatalf("components/wrapper admission mismatch components=%+v wrapper=%+v", componentAdmission, result)
		}
		statementRaw := components.StatementRaw()
		snapshotRaw := components.SnapshotRaw()
		repository := components.RepositoryLeaves()
		if len(statementRaw) == 0 || len(snapshotRaw) == 0 || repository == nil {
			t.Fatal("components accessor 返回 nil/empty verified component")
		}

		// 修改所有 accessor 返回值不能污染冻结 components，也不能触发第二次 List/Open。
		componentAdmission.repositoryPaths[0] = "forged"
		statementRaw[0] ^= 0xff
		snapshotRaw[0] ^= 0xff
		firstPath := repository.paths[0]
		forgedLeaf := repository.leaves[firstPath]
		forgedLeaf.raw = "forged"
		repository.leaves[firstPath] = forgedLeaf
		if components.Admission().repositoryPaths[0] == "forged" ||
			bytes.Equal(components.StatementRaw(), statementRaw) ||
			bytes.Equal(components.SnapshotRaw(), snapshotRaw) {
			t.Fatal("components accessor 修改污染内部冻结值")
		}
		_, currentRaw, exists := components.RepositoryLeaves().Leaf(firstPath)
		if !exists || string(currentRaw) == "forged" {
			t.Fatal("components repository accessor 修改污染内部 leaf")
		}
		assertLoaderOnePass(t, repositoryLoader, moduleLoader)
		if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(components); err != nil {
			t.Fatalf("accessor mutation polluted components boundary: %v", err)
		}

		if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(nil); err == nil {
			t.Fatal("nil components boundary accepted")
		}
		forgedComponents := *components
		forgedComponents.repositoryLeaves = nil
		if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(&forgedComponents); err == nil {
			t.Fatal("components nil repository boundary accepted")
		}
	})

	t.Run("loader_result_mismatch", func(t *testing.T) {
		t.Run("repository_loader_bytes_from_other_result", func(t *testing.T) {
			repositoryLoader := newRepositoryLoader()
			firstPath := reviewFreezeCompileInputSnapshotRepositoryPathsV1()[0]
			drift := append([]byte(nil), fixture.repository.Materials[firstPath]...)
			drift[0] ^= 0xff
			repositoryLoader.overrides[firstPath] = drift
			_, err := reviewFreezeAdmitCompileLeafArtifactV1(context.Background(), fixture.verified, repositoryLoader, newModuleLoader())
			if err == nil || !strings.Contains(err.Error(), "SHA-256 drift") {
				t.Fatalf("repository loader mismatch error=%v", err)
			}
		})

		t.Run("module_loader_bytes_from_other_result", func(t *testing.T) {
			moduleLoader := newModuleLoader()
			firstPath := reviewFreezeCompileInputSnapshotModulePathsV1()[0]
			moduleLoader.objects[firstPath].raw[0] ^= 0xff
			_, err := reviewFreezeAdmitCompileLeafArtifactV1(context.Background(), fixture.verified, newRepositoryLoader(), moduleLoader)
			if err == nil || !strings.Contains(err.Error(), "sha256") {
				t.Fatalf("module loader mismatch error=%v", err)
			}
		})

		t.Run("assemble_rejects_same_paths_from_different_snapshot", func(t *testing.T) {
			direct, err := reviewFreezeAdmitCompileDirectMaterialsV1(fixture.verified)
			if err != nil {
				t.Fatal(err)
			}
			statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(fixture.statement)
			if err != nil {
				t.Fatal(err)
			}
			repository, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), fixture.snapshot, statement, newRepositoryLoader())
			if err != nil {
				t.Fatal(err)
			}
			modules, err := reviewFreezeResolveCompileModuleLeafBundleV1(context.Background(), fixture.snapshot, statement, newModuleLoader())
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoV1(fixture.verified)
			if err != nil {
				t.Fatal(err)
			}

			forgedRepository := reviewFreezeCompileLeafArtifactRepositoryBundleCloneV1(repository)
			firstRepositoryPath := forgedRepository.paths[0]
			forgedLeaf := forgedRepository.leaves[firstRepositoryPath]
			forgedRaw := []byte(forgedLeaf.raw)
			forgedRaw[0] ^= 0xff
			forgedLeaf.raw = string(forgedRaw)
			forgedLeaf.metadata.SHA256 = reviewFreezeSHA256V1(forgedRaw)
			forgedLeaf.metadata.GitBlobSHA = reviewFreezeCompileRepositoryLeafGitBlobSHAV1(forgedRaw)
			forgedRepository.leaves[firstRepositoryPath] = forgedLeaf
			if _, err := reviewFreezeAssembleCompileLeafArtifactAdmissionV1(fixture.statement, fixture.snapshot, direct, forgedRepository, modules, artifact); err == nil || !strings.Contains(err.Error(), "metadata 与 snapshot 错配") {
				t.Fatalf("different repository snapshot result error=%v", err)
			}

			forgedModules := reviewFreezeCompileLeafArtifactModuleBundleCloneV1(modules)
			firstModulePath := forgedModules.paths[0]
			forgedModules.files[firstModulePath][0] ^= 0xff
			if _, err := reviewFreezeAssembleCompileLeafArtifactAdmissionV1(fixture.statement, fixture.snapshot, direct, repository, forgedModules, artifact); err == nil || !strings.Contains(err.Error(), "module bytes 与 snapshot 错配") {
				t.Fatalf("different module snapshot result error=%v", err)
			}
		})
	})

	t.Run("result_gap_and_authority_fail_closed", func(t *testing.T) {
		tests := []struct {
			name   string
			mutate func(*reviewFreezeCompileLeafArtifactAdmissionV1)
		}{
			{name: "repository_result_count_mismatch", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.repositoryLeafCount-- }},
			{name: "module_result_path_mismatch", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) {
				value.modulePaths = value.modulePaths[:len(value.modulePaths)-1]
			}},
			{name: "artifact_result_projection_mismatch", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) {
				value.artifactBuildInfoProjectionSHA256 = strings.Repeat("f", 71)
			}},
			{name: "delete_closed_gap", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) {
				value.closedSemanticGaps = value.closedSemanticGaps[:len(value.closedSemanticGaps)-1]
			}},
			{name: "delete_open_gap", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) {
				value.openGaps = value.openGaps[:len(value.openGaps)-1]
			}},
			{name: "claim_base_tree_membership", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.repositoryBaseTreeMembership = true }},
			{name: "claim_commit_ancestry", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.repositoryCommitAncestry = true }},
			{name: "claim_artifact_source_derivation", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.artifactSourceDerivation = true }},
			{name: "claim_builder_execution", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.builderExecutionProven = true }},
			{name: "claim_authority", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.authority = true }},
			{name: "claim_formal_freeze", mutate: func(value *reviewFreezeCompileLeafArtifactAdmissionV1) { value.formalFreezeEligible = true }},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				forged := reviewFreezeCompileLeafArtifactResultCloneV1(result)
				test.mutate(forged)
				if err := reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(forged, fixture.statement, fixture.snapshot); err == nil {
					t.Fatal("forged compile leaf/artifact result accepted")
				}
			})
		}
	})
}
