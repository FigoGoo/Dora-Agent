package reviewfreeze_test

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	reviewFreezeCompileGitRawAdmissionAssuranceV1 = "raw_object_recomputed_test_only"

	reviewFreezeCompileGitRawAdmissionClosedCommitIdentityV1 = "base_commit_object_identity_verified"
	reviewFreezeCompileGitRawAdmissionClosedCommitTreeV1     = "base_commit_tree_binding_verified"
	reviewFreezeCompileGitRawAdmissionClosedTreeMembershipV1 = "repository_14_leaf_base_tree_membership_verified"

	reviewFreezeCompileGitRawAdmissionGapExpectedSourceV1      = "subject_commit_expected_source_binding_unverified"
	reviewFreezeCompileGitRawAdmissionGapGitHubRepositoryV1    = "github_repository_authority_unverified"
	reviewFreezeCompileGitRawAdmissionGapGitHubStableReadV1    = "github_reported_oid_stable_double_read_unverified"
	reviewFreezeCompileGitRawAdmissionGapGitHubObservationV1   = "github_observation_authority_unverified"
	reviewFreezeCompileGitRawAdmissionGapArtifactDerivationV1  = "compiled_test_binary_source_derivation_unverified"
	reviewFreezeCompileGitRawAdmissionGapBuilderExecutionV1    = "builder_execution_unverified"
	reviewFreezeCompileGitRawAdmissionGapIndependentBuildersV1 = "independent_builder_pair_unverified"
	reviewFreezeCompileGitRawAdmissionGapSignatureV1           = "signature_and_trust_root_unverified"
)

// reviewFreezeCompileGitRawAdmissionV1 是 leaf/artifact admission 与同一次 raw Git CAS
// 观察的不可变组合结论。它只在 test-only assurance 下新增关闭 commit object identity、
// commit-to-tree binding 和 14-leaf tree membership；远端观测、来源、构建者和签名权威
// 始终保持 false，因此不能升级为 Formal Freeze。
type reviewFreezeCompileGitRawAdmissionV1 struct {
	decision                              string
	assurance                             string
	statementSHA256                       string
	snapshotSHA256                        string
	baseCommitSHA                         string
	baseTreeSHA                           string
	repositoryPaths                       []string
	gitObjectIDs                          []string
	closedSemanticGaps                    []string
	openGaps                              []string
	baseCommitObjectIdentityVerified      bool
	baseCommitTreeBindingVerified         bool
	repositoryBaseTreeMembershipVerified  bool
	githubReportedOIDStableDoubleRead     bool
	trustedExpectedSourceCommitExactEqual bool
	githubRepositoryAuthority             bool
	githubObservationAuthority            bool
	artifactSourceDerivation              bool
	builderExecutionProven                bool
	buildClosureVerified                  bool
	sbomVerified                          bool
	toolchainVerified                     bool
	independentBuilderPairVerified        bool
	signatureAuthority                    bool
	authority                             bool
	formalFreezeEligible                  bool
}

// reviewFreezeCompileGitRawAdmissionComponentsV1 冻结同一次 end-to-end 调用的全部纯内存
// 证据。leaf、raw bundle、commit 与 tree 结果只保存在本对象内部；对外 accessor 均返回
// 深副本，且不会再次访问 repository/module/raw CAS loader。
type reviewFreezeCompileGitRawAdmissionComponentsV1 struct {
	admission      *reviewFreezeCompileGitRawAdmissionV1
	leafComponents *reviewFreezeCompileLeafArtifactComponentsV1
	rawBundle      *reviewFreezeCompileGitRawObjectBundleV1
	commitBinding  *reviewFreezeVerifiedCompileCommitBindingV1
	treeMembership *reviewFreezeVerifiedCompileGitBaseTreeV1
}

// Assurance 返回本阶段固定的 test-only assurance 名称。
func (result *reviewFreezeCompileGitRawAdmissionV1) Assurance() string {
	if result == nil {
		return ""
	}
	return result.assurance
}

// RepositoryPaths 返回已绑定到同一 snapshot、leaf bundle 和 BaseTree 的 14 个路径副本。
func (result *reviewFreezeCompileGitRawAdmissionV1) RepositoryPaths() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.repositoryPaths...)
}

// GitObjectIDs 返回同一次共享 raw CAS exact-set 的有序副本。
func (result *reviewFreezeCompileGitRawAdmissionV1) GitObjectIDs() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.gitObjectIDs...)
}

// ClosedSemanticGaps 返回 prior admission 加本阶段三项新增关闭结论的 exact-set 副本。
func (result *reviewFreezeCompileGitRawAdmissionV1) ClosedSemanticGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.closedSemanticGaps...)
}

// OpenGaps 返回仍阻止 Formal Freeze 的来源、权威、构建和签名 gap exact-set 副本。
func (result *reviewFreezeCompileGitRawAdmissionV1) OpenGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.openGaps...)
}

// Authority 返回本结果是否具有外部信任根权威；raw test-only 阶段恒为 false。
func (result *reviewFreezeCompileGitRawAdmissionV1) Authority() bool {
	return result != nil && result.authority
}

// FormalFreezeEligible 返回本结果是否可进入 Formal Freeze；本阶段恒为 false。
func (result *reviewFreezeCompileGitRawAdmissionV1) FormalFreezeEligible() bool {
	return result != nil && result.formalFreezeEligible
}

// Admission 返回 end-to-end admission 深副本；该调用不会重新读取任何 loader。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) Admission() *reviewFreezeCompileGitRawAdmissionV1 {
	if components == nil {
		return nil
	}
	return reviewFreezeCompileGitRawAdmissionCloneV1(components.admission)
}

// StatementRaw 返回参与本次组合准入的 strict statement raw 副本。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) StatementRaw() []byte {
	if components == nil || components.leafComponents == nil {
		return nil
	}
	return components.leafComponents.StatementRaw()
}

// SnapshotRaw 返回参与本次组合准入的 strict snapshot raw 副本。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) SnapshotRaw() []byte {
	if components == nil || components.leafComponents == nil {
		return nil
	}
	return components.leafComponents.SnapshotRaw()
}

// CommitBinding 返回 commit semantic verifier 结果深副本；frame 与 parent 均不共享。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) CommitBinding() *reviewFreezeVerifiedCompileCommitBindingV1 {
	if components == nil {
		return nil
	}
	return reviewFreezeCompileGitRawAdmissionCommitCloneV1(components.commitBinding)
}

// TreeMembership 返回 tree semantic verifier 结果深副本；path/OID accessor 不暴露内部切片。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) TreeMembership() *reviewFreezeVerifiedCompileGitBaseTreeV1 {
	if components == nil {
		return nil
	}
	return reviewFreezeCompileGitRawAdmissionTreeCloneV1(components.treeMembership)
}

// RawBundleObjectIDs 返回共享 resolver 冻结的对象 exact-set 副本。
func (components *reviewFreezeCompileGitRawAdmissionComponentsV1) RawBundleObjectIDs() []string {
	if components == nil || components.rawBundle == nil {
		return nil
	}
	return components.rawBundle.ObjectIDs()
}

// reviewFreezeCompileGitRawAdmissionClosedGapsV1 仅在 prior exact-set 尾部新增三项。
// 顺序固定可防止后续实现借重排、漏项或同义词静默扩大证明边界。
func reviewFreezeCompileGitRawAdmissionClosedGapsV1() []string {
	closed := reviewFreezeCompileLeafArtifactClosedGapsV1()
	return append(closed,
		reviewFreezeCompileGitRawAdmissionClosedCommitIdentityV1,
		reviewFreezeCompileGitRawAdmissionClosedCommitTreeV1,
		reviewFreezeCompileGitRawAdmissionClosedTreeMembershipV1,
	)
}

// reviewFreezeCompileGitRawAdmissionOpenGapsV1 用 expected source commit 精确相等替代
// 普通 ancestry。ancestor 允许陈旧源码，不能作为源码准入条件；GitHub structured
// observation 也必须与 raw object recomputation 分层表达，不能混成一个 authority claim。
func reviewFreezeCompileGitRawAdmissionOpenGapsV1() []string {
	return []string{
		reviewFreezeCompileGitRawAdmissionGapExpectedSourceV1,
		reviewFreezeCompileGitRawAdmissionGapGitHubRepositoryV1,
		reviewFreezeCompileGitRawAdmissionGapGitHubStableReadV1,
		reviewFreezeCompileGitRawAdmissionGapGitHubObservationV1,
		reviewFreezeCompileGitRawAdmissionGapArtifactDerivationV1,
		reviewFreezeCompileGitRawAdmissionGapBuilderExecutionV1,
		reviewFreezeCompileDirectMaterialGapBuildClosureV1,
		reviewFreezeCompileDirectMaterialGapSBOMBinaryV1,
		reviewFreezeCompileDirectMaterialGapSBOMRawV1,
		reviewFreezeCompileDirectMaterialGapSBOMDeriveV1,
		reviewFreezeCompileDirectMaterialGapToolchainV1,
		reviewFreezeCompileGitRawAdmissionGapIndependentBuildersV1,
		reviewFreezeCompileGitRawAdmissionGapSignatureV1,
	}
}

// reviewFreezeAdmitCompileGitRawV1 保持简洁的组合入口；内部所有 loader 只由 components
// 入口消费一次，成功后返回不可变 admission 副本。
func reviewFreezeAdmitCompileGitRawV1(
	ctx context.Context,
	verified *reviewFreezeVerifiedAttestationMaterialBundleV1,
	repositoryLoader reviewFreezeCompileRepositoryLeafLoaderV1,
	moduleLoader reviewFreezeCompileModuleLeafLoaderV1,
	rawLoader reviewFreezeCompileGitRawObjectLoaderV1,
) (*reviewFreezeCompileGitRawAdmissionV1, error) {
	components, err := reviewFreezeAdmitCompileGitRawComponentsV1(ctx, verified, repositoryLoader, moduleLoader, rawLoader)
	if err != nil {
		return nil, err
	}
	return components.Admission(), nil
}

// reviewFreezeAdmitCompileGitRawComponentsV1 先让 prior admission 完成 repository/module
// 单次读取，再让共享 raw resolver 恰好 Resolve 一次。commit/tree verifier 只消费 bundle
// 的只读 view，不回访底层 CAS；任一步失败都不返回部分 evidence。
func reviewFreezeAdmitCompileGitRawComponentsV1(
	ctx context.Context,
	verified *reviewFreezeVerifiedAttestationMaterialBundleV1,
	repositoryLoader reviewFreezeCompileRepositoryLeafLoaderV1,
	moduleLoader reviewFreezeCompileModuleLeafLoaderV1,
	rawLoader reviewFreezeCompileGitRawObjectLoaderV1,
) (*reviewFreezeCompileGitRawAdmissionComponentsV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile git raw admission context 不能为空")
	}
	if rawLoader == nil {
		return nil, fmt.Errorf("compile git raw admission loader 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile git raw admission context before leaf admission: %w", err)
	}
	leafComponents, err := reviewFreezeAdmitCompileLeafArtifactComponentsV1(ctx, verified, repositoryLoader, moduleLoader)
	if err != nil {
		return nil, fmt.Errorf("compile git raw prior leaf/artifact admission: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile git raw admission context before raw Resolve: %w", err)
	}
	rawBundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(ctx, rawLoader)
	if err != nil {
		return nil, fmt.Errorf("compile git raw shared bundle: %w", err)
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(leafComponents.StatementRaw())
	if err != nil {
		return nil, fmt.Errorf("compile git raw strict statement: %w", err)
	}
	commitBinding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(ctx, statement, rawBundle.NewCommitObjectLoaderView())
	if err != nil {
		return nil, fmt.Errorf("compile git raw commit binding: %w", err)
	}
	treeMembership, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(
		ctx,
		leafComponents.SnapshotRaw(),
		statement,
		leafComponents.RepositoryLeaves(),
		rawBundle.NewTreeObjectLoaderView(),
	)
	if err != nil {
		return nil, fmt.Errorf("compile git raw tree membership: %w", err)
	}
	admission, err := reviewFreezeAssembleCompileGitRawAdmissionV1(leafComponents, rawBundle, commitBinding, treeMembership)
	if err != nil {
		return nil, err
	}
	components := &reviewFreezeCompileGitRawAdmissionComponentsV1{
		admission:      reviewFreezeCompileGitRawAdmissionCloneV1(admission),
		leafComponents: leafComponents,
		rawBundle:      rawBundle,
		commitBinding:  reviewFreezeCompileGitRawAdmissionCommitCloneV1(commitBinding),
		treeMembership: reviewFreezeCompileGitRawAdmissionTreeCloneV1(treeMembership),
	}
	if err := reviewFreezeValidateCompileGitRawAdmissionComponentsV1(components); err != nil {
		return nil, fmt.Errorf("compile git raw components boundary: %w", err)
	}
	return components, nil
}

// reviewFreezeAssembleCompileGitRawAdmissionV1 只组合四个已经冻结的子结果。它再次绑定
// statement/snapshot/prior admission/leaf/path/commit/tree，并要求 semantic verifier 消费
// OID 的无重复 union 与共享 bundle exact-set 完全相等。
func reviewFreezeAssembleCompileGitRawAdmissionV1(
	leafComponents *reviewFreezeCompileLeafArtifactComponentsV1,
	rawBundle *reviewFreezeCompileGitRawObjectBundleV1,
	commitBinding *reviewFreezeVerifiedCompileCommitBindingV1,
	treeMembership *reviewFreezeVerifiedCompileGitBaseTreeV1,
) (*reviewFreezeCompileGitRawAdmissionV1, error) {
	statement, prior, objectIDs, err := reviewFreezeValidateCompileGitRawAdmissionEvidenceV1(
		leafComponents, rawBundle, commitBinding, treeMembership,
	)
	if err != nil {
		return nil, fmt.Errorf("compile git raw assemble evidence: %w", err)
	}
	statementRaw := leafComponents.StatementRaw()
	snapshotRaw := leafComponents.SnapshotRaw()
	result := &reviewFreezeCompileGitRawAdmissionV1{
		decision:                             prior.decision,
		assurance:                            reviewFreezeCompileGitRawAdmissionAssuranceV1,
		statementSHA256:                      reviewFreezeSHA256V1(statementRaw),
		snapshotSHA256:                       reviewFreezeSHA256V1(snapshotRaw),
		baseCommitSHA:                        statement.Subject.BaseCommitSHA,
		baseTreeSHA:                          statement.Subject.BaseTreeSHA,
		repositoryPaths:                      prior.RepositoryPaths(),
		gitObjectIDs:                         append([]string(nil), objectIDs...),
		closedSemanticGaps:                   reviewFreezeCompileGitRawAdmissionClosedGapsV1(),
		openGaps:                             reviewFreezeCompileGitRawAdmissionOpenGapsV1(),
		baseCommitObjectIdentityVerified:     true,
		baseCommitTreeBindingVerified:        true,
		repositoryBaseTreeMembershipVerified: true,
		// raw object recomputation 不包含 GitHub 观测、构建执行或外部 trust root。
		githubReportedOIDStableDoubleRead:     false,
		trustedExpectedSourceCommitExactEqual: false,
		githubRepositoryAuthority:             false,
		githubObservationAuthority:            false,
		artifactSourceDerivation:              false,
		builderExecutionProven:                false,
		buildClosureVerified:                  false,
		sbomVerified:                          false,
		toolchainVerified:                     false,
		independentBuilderPairVerified:        false,
		signatureAuthority:                    false,
		authority:                             false,
		formalFreezeEligible:                  false,
	}
	if err := reviewFreezeValidateCompileGitRawAdmissionBoundaryV1(result, leafComponents, rawBundle, commitBinding, treeMembership); err != nil {
		return nil, err
	}
	return result, nil
}

// reviewFreezeValidateCompileGitRawAdmissionEvidenceV1 校验所有子结果仍属于同一份
// statement/snapshot，并把 exact-set union 返回给 assemble/boundary 共用。该函数是纯内存
// 边界，不调用任何 loader。
func reviewFreezeValidateCompileGitRawAdmissionEvidenceV1(
	leafComponents *reviewFreezeCompileLeafArtifactComponentsV1,
	rawBundle *reviewFreezeCompileGitRawObjectBundleV1,
	commitBinding *reviewFreezeVerifiedCompileCommitBindingV1,
	treeMembership *reviewFreezeVerifiedCompileGitBaseTreeV1,
) (reviewFreezeValidatorCompileAttestationV1, *reviewFreezeCompileLeafArtifactAdmissionV1, []string, error) {
	if leafComponents == nil || rawBundle == nil || commitBinding == nil || treeMembership == nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("compile git raw evidence 子结果不能为空")
	}
	if err := reviewFreezeValidateCompileLeafArtifactComponentsBoundaryV1(leafComponents); err != nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("prior components: %w", err)
	}
	statementRaw := leafComponents.StatementRaw()
	snapshotRaw := leafComponents.SnapshotRaw()
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("strict statement: %w", err)
	}
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(snapshotRaw, statement); err != nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("strict snapshot: %w", err)
	}
	prior := leafComponents.Admission()
	if err := reviewFreezeValidateCompileLeafArtifactAdmissionBoundaryV1(prior, statementRaw, snapshotRaw); err != nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("prior admission: %w", err)
	}
	leaves := leafComponents.RepositoryLeaves()
	if leaves == nil || !reflect.DeepEqual(leaves.Paths(), prior.RepositoryPaths()) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("repository leaf/prior path 错配")
	}

	commitScope := commitBinding.Scope()
	if commitScope.VerifiedClaim != reviewFreezeCompileCommitBindingVerifiedClaimV1 ||
		!commitScope.BaseCommitToTreeBound || commitScope.TrustedSourceCommitEqual ||
		commitScope.CommitAncestryProven || commitScope.GitHubAuthorityProven ||
		commitScope.FormalFreezeStatus != reviewFreezeCompileCommitFormalFreezeStatusV1 ||
		commitScope.RequiredTypedAnchorSchema != reviewFreezeCompileCommitRequiredAnchorSchemaV1 ||
		!reflect.DeepEqual(commitScope.OpenGaps, []string{
			reviewFreezeCompileCommitSourceEqualityGapV1,
			reviewFreezeCompileCommitAncestryGapV1,
			reviewFreezeCompileCommitRemoteAuthorityGapV1,
		}) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("commit verifier scope 越界=%+v", commitScope)
	}
	treeScope := treeMembership.Scope()
	if treeScope.VerifiedClaim != reviewFreezeCompileGitBaseTreeClaimV1 ||
		!treeScope.BaseTreeMembership || treeScope.BaseCommitBinding || treeScope.CommitAncestry ||
		treeScope.GitHubAuthority || treeScope.FormalFreezeStatus != reviewFreezeCompileGitFormalFreezeNotProvenV1 ||
		!reflect.DeepEqual(treeScope.OpenGaps, []string{
			reviewFreezeCompileGitBaseCommitGapV1,
			reviewFreezeCompileGitCommitAncestryGapV1,
			reviewFreezeCompileGitGitHubAuthorityGapV1,
		}) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("tree verifier scope 越界=%+v", treeScope)
	}
	if commitBinding.CommitSHA() != statement.Subject.BaseCommitSHA {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("commit/statement 错配 actual=%q want=%q", commitBinding.CommitSHA(), statement.Subject.BaseCommitSHA)
	}
	if commitBinding.TreeSHA() != statement.Subject.BaseTreeSHA || treeMembership.BaseTreeSHA() != statement.Subject.BaseTreeSHA {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("commit/tree/statement BaseTree 错配 commit=%q membership=%q statement=%q", commitBinding.TreeSHA(), treeMembership.BaseTreeSHA(), statement.Subject.BaseTreeSHA)
	}
	if reviewFreezeCompileCommitObjectSHAV1(commitBinding.FramedObjectBytes()) != statement.Subject.BaseCommitSHA {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("commit framed object 与 statement identity 错配")
	}
	bundleCommitFrame, exists := rawBundle.CanonicalFrameBytes(statement.Subject.BaseCommitSHA)
	if !exists || !bytes.Equal(bundleCommitFrame, commitBinding.FramedObjectBytes()) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("commit verifier frame 与共享 raw bundle bytes 错配")
	}
	if !reflect.DeepEqual(treeMembership.Paths(), prior.RepositoryPaths()) ||
		!reflect.DeepEqual(treeMembership.Paths(), leaves.Paths()) ||
		!reflect.DeepEqual(treeMembership.Paths(), reviewFreezeCompileInputSnapshotRepositoryPathsV1()) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("tree/leaf/prior path exact-set 错配 tree=%v leaf=%v prior=%v", treeMembership.Paths(), leaves.Paths(), prior.RepositoryPaths())
	}
	objectIDs, err := reviewFreezeCompileGitRawAdmissionExactUnionV1(
		commitBinding.UsedObjectIDs(), treeMembership.ObjectIDs(), rawBundle.ObjectIDs(),
	)
	if err != nil {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, err
	}
	descriptors := rawBundle.Descriptors()
	commitDescriptors := 0
	treeDescriptorIDs := make([]string, 0, len(descriptors)-1)
	for _, descriptor := range descriptors {
		switch descriptor.Kind {
		case "commit":
			commitDescriptors++
			if descriptor.ObjectID != statement.Subject.BaseCommitSHA {
				return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("raw commit descriptor/statement 错配=%q/%q", descriptor.ObjectID, statement.Subject.BaseCommitSHA)
			}
		case "tree":
			treeDescriptorIDs = append(treeDescriptorIDs, descriptor.ObjectID)
		default:
			return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("raw descriptor kind 非法=%q", descriptor.Kind)
		}
	}
	if commitDescriptors != 1 || !reflect.DeepEqual(treeDescriptorIDs, treeMembership.ObjectIDs()) {
		return reviewFreezeValidatorCompileAttestationV1{}, nil, nil, fmt.Errorf("raw descriptor/semantic exact-set 错配 commit_count=%d tree=%v/%v", commitDescriptors, treeDescriptorIDs, treeMembership.ObjectIDs())
	}
	return statement, prior, objectIDs, nil
}

// reviewFreezeCompileGitRawAdmissionExactUnionV1 拒绝 semantic verifier 间的重复 OID，
// 再要求排序 union 与共享 resolver 的 descriptor exact-set 完全相同。missing、extra 和
// stale same-count replacement 都在此失败关闭。
func reviewFreezeCompileGitRawAdmissionExactUnionV1(commitObjectIDs, treeObjectIDs, wantObjectIDs []string) ([]string, error) {
	seen := make(map[string]string, len(commitObjectIDs)+len(treeObjectIDs))
	union := make([]string, 0, len(commitObjectIDs)+len(treeObjectIDs))
	consume := func(source string, objectIDs []string) error {
		for _, objectID := range objectIDs {
			if !reviewFreezeGitSHA1V1.MatchString(objectID) || objectID == strings.Repeat("0", 40) {
				return fmt.Errorf("compile git raw exact union %s OID 非法=%q", source, objectID)
			}
			if previous, duplicate := seen[objectID]; duplicate {
				return fmt.Errorf("compile git raw exact union duplicate OID=%q sources=%s/%s", objectID, previous, source)
			}
			seen[objectID] = source
			union = append(union, objectID)
		}
		return nil
	}
	if err := consume("commit", commitObjectIDs); err != nil {
		return nil, err
	}
	if err := consume("tree", treeObjectIDs); err != nil {
		return nil, err
	}
	sort.Strings(union)
	if !reflect.DeepEqual(union, wantObjectIDs) {
		return nil, fmt.Errorf("compile git raw exact union 错配 used=%v bundle=%v", union, wantObjectIDs)
	}
	return append([]string(nil), union...), nil
}

// reviewFreezeValidateCompileGitRawAdmissionBoundaryV1 对 result 和全部 evidence 做完整
// exact-set 复核。任何 scope 提权、gap 删除或 raw/GitHub assurance 混淆都会失败关闭。
func reviewFreezeValidateCompileGitRawAdmissionBoundaryV1(
	result *reviewFreezeCompileGitRawAdmissionV1,
	leafComponents *reviewFreezeCompileLeafArtifactComponentsV1,
	rawBundle *reviewFreezeCompileGitRawObjectBundleV1,
	commitBinding *reviewFreezeVerifiedCompileCommitBindingV1,
	treeMembership *reviewFreezeVerifiedCompileGitBaseTreeV1,
) error {
	if result == nil {
		return fmt.Errorf("compile git raw admission result 不能为空")
	}
	statement, prior, objectIDs, err := reviewFreezeValidateCompileGitRawAdmissionEvidenceV1(
		leafComponents, rawBundle, commitBinding, treeMembership,
	)
	if err != nil {
		return fmt.Errorf("compile git raw admission boundary evidence: %w", err)
	}
	if result.decision != prior.decision || result.assurance != reviewFreezeCompileGitRawAdmissionAssuranceV1 {
		return fmt.Errorf("compile git raw decision/assurance 错配=%q/%q", result.decision, result.assurance)
	}
	if result.statementSHA256 != reviewFreezeSHA256V1(leafComponents.StatementRaw()) ||
		result.snapshotSHA256 != reviewFreezeSHA256V1(leafComponents.SnapshotRaw()) {
		return fmt.Errorf("compile git raw statement/snapshot digest 错配")
	}
	if result.baseCommitSHA != statement.Subject.BaseCommitSHA || result.baseTreeSHA != statement.Subject.BaseTreeSHA {
		return fmt.Errorf("compile git raw subject identity 错配 commit/tree=%q/%q", result.baseCommitSHA, result.baseTreeSHA)
	}
	if !reflect.DeepEqual(result.repositoryPaths, prior.RepositoryPaths()) ||
		!reflect.DeepEqual(result.gitObjectIDs, objectIDs) ||
		!reflect.DeepEqual(result.closedSemanticGaps, reviewFreezeCompileGitRawAdmissionClosedGapsV1()) ||
		!reflect.DeepEqual(result.openGaps, reviewFreezeCompileGitRawAdmissionOpenGapsV1()) {
		return fmt.Errorf("compile git raw exact-set 错配 paths=%v objects=%v closed=%v open=%v", result.repositoryPaths, result.gitObjectIDs, result.closedSemanticGaps, result.openGaps)
	}
	if !result.baseCommitObjectIdentityVerified || !result.baseCommitTreeBindingVerified || !result.repositoryBaseTreeMembershipVerified {
		return fmt.Errorf("compile git raw 三项新增关闭结论缺失")
	}
	if result.githubReportedOIDStableDoubleRead || result.trustedExpectedSourceCommitExactEqual ||
		result.githubRepositoryAuthority || result.githubObservationAuthority || result.artifactSourceDerivation ||
		result.builderExecutionProven || result.buildClosureVerified || result.sbomVerified ||
		result.toolchainVerified || result.independentBuilderPairVerified || result.signatureAuthority ||
		result.authority || result.formalFreezeEligible {
		return fmt.Errorf("compile git raw scope 不得提权 github/source/artifact/builder/buildclosure/sbom/toolchain/pair/signature/authority/freeze")
	}
	for _, gap := range result.openGaps {
		if strings.Contains(gap, "ancestry") {
			return fmt.Errorf("compile git raw 源码门禁不得使用 ancestry=%q", gap)
		}
	}
	return nil
}

// reviewFreezeValidateCompileGitRawAdmissionComponentsV1 确认 components 内部 result 与
// 四段 evidence 相互绑定。该检查不会重新 Resolve 或触碰 loader。
func reviewFreezeValidateCompileGitRawAdmissionComponentsV1(components *reviewFreezeCompileGitRawAdmissionComponentsV1) error {
	if components == nil {
		return fmt.Errorf("compile git raw components 不能为空")
	}
	return reviewFreezeValidateCompileGitRawAdmissionBoundaryV1(
		components.admission,
		components.leafComponents,
		components.rawBundle,
		components.commitBinding,
		components.treeMembership,
	)
}

// reviewFreezeCompileGitRawAdmissionCloneV1 深复制组合结果中的全部 exact-set。
func reviewFreezeCompileGitRawAdmissionCloneV1(source *reviewFreezeCompileGitRawAdmissionV1) *reviewFreezeCompileGitRawAdmissionV1 {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.repositoryPaths = append([]string(nil), source.repositoryPaths...)
	cloned.gitObjectIDs = append([]string(nil), source.gitObjectIDs...)
	cloned.closedSemanticGaps = append([]string(nil), source.closedSemanticGaps...)
	cloned.openGaps = append([]string(nil), source.openGaps...)
	return &cloned
}

// reviewFreezeCompileGitRawAdmissionCommitCloneV1 深复制 commit verifier 结果。
func reviewFreezeCompileGitRawAdmissionCommitCloneV1(source *reviewFreezeVerifiedCompileCommitBindingV1) *reviewFreezeVerifiedCompileCommitBindingV1 {
	if source == nil {
		return nil
	}
	return &reviewFreezeVerifiedCompileCommitBindingV1{
		commitSHA: source.CommitSHA(),
		treeSHA:   source.TreeSHA(),
		parents:   source.ParentSHAs(),
		frame:     string(source.FramedObjectBytes()),
	}
}

// reviewFreezeCompileGitRawAdmissionTreeCloneV1 深复制 tree verifier 结果。
func reviewFreezeCompileGitRawAdmissionTreeCloneV1(source *reviewFreezeVerifiedCompileGitBaseTreeV1) *reviewFreezeVerifiedCompileGitBaseTreeV1 {
	if source == nil {
		return nil
	}
	return &reviewFreezeVerifiedCompileGitBaseTreeV1{
		baseTreeSHA: source.BaseTreeSHA(),
		paths:       source.Paths(),
		objectIDs:   source.ObjectIDs(),
		objectCount: source.ObjectCount(),
		totalBytes:  source.TotalBytes(),
		maxDepth:    source.MaxDepth(),
	}
}

// reviewFreezeCompileGitRawAdmissionFixtureV1 把真实当前 HEAD/worktree leaf gate、真实
// module cache/artifact fixture 和 body-only raw Git CAS 绑定到同一 statement/snapshot。
type reviewFreezeCompileGitRawAdmissionFixtureV1 struct {
	verified      *reviewFreezeVerifiedAttestationMaterialBundleV1
	repository    reviewFreezeCompileRepositoryLeafFixtureV1
	module        *reviewFreezeCompileModuleLeafFixtureV1
	rawObjects    []reviewFreezeCompileGitRawFixtureObjectV1
	baseCommitSHA string
	baseTreeSHA   string
}

// reviewFreezeNewCompileGitRawAdmissionFixtureV1 仅在 fixture 阶段调用 git cat-file。
// verifier 入口收到的只有 immutable bytes 与内存 loader，且当前 HEAD 的 14 个目标 leaf
// 必须与 worktree 相同，否则 clean-worktree gate 会失败。
func reviewFreezeNewCompileGitRawAdmissionFixtureV1(t *testing.T) reviewFreezeCompileGitRawAdmissionFixtureV1 {
	t.Helper()
	leaf := reviewFreezeNewCompileLeafArtifactFixtureV1(t)
	root := reviewFreezeCompileRepositoryLeafFixtureRootV1(t)
	commitSHA := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{commit}")
	treeSHA := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{tree}")

	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(leaf.statement)
	if err != nil {
		t.Fatalf("decode leaf fixture statement: %v", err)
	}
	statement.Subject.BaseCommitSHA = commitSHA
	statement.Subject.BaseTreeSHA = treeSHA
	roleBytes := make(map[string][]byte, len(leaf.verified.Roles()))
	for _, role := range leaf.verified.Roles() {
		_, raw, exists := leaf.verified.Material(role)
		if !exists {
			t.Fatalf("leaf fixture material missing role=%q", role)
		}
		roleBytes[role] = raw
	}
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, leaf.snapshot)
	snapshot.Subject = statement.Subject
	snapshotRaw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1] = snapshotRaw
	reviewFreezeCompileDirectMaterialBindRoleV1(&statement, reviewFreezeAttestationMaterialRoleSnapshotV1, snapshotRaw)
	direct := reviewFreezeCompileDirectMaterialFixtureBuildV1(t, statement, roleBytes)
	verified := reviewFreezeCompileDirectMaterialFixtureVerifyV1(t, direct)

	treeFrames := reviewFreezeCompileGitCollectTargetTreesV1(
		t, root, treeSHA, reviewFreezeCompileInputSnapshotRepositoryPathsV1(),
	)
	commitBody := reviewFreezeCompileGitCommandV1(t, root, "cat-file", "commit", commitSHA)
	commitDescriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("commit", commitBody)
	if commitDescriptor.ObjectID != commitSHA {
		t.Fatalf("HEAD commit body OID=%q want=%q", commitDescriptor.ObjectID, commitSHA)
	}
	rawObjects := []reviewFreezeCompileGitRawFixtureObjectV1{{Kind: "commit", Body: append([]byte(nil), commitBody...)}}
	for objectID, frame := range treeFrames {
		body := reviewFreezeCompileGitRawSplitFrameFixtureV1(t, "tree", frame)
		if actual := reviewFreezeCompileGitRawDescriptorFixtureV1("tree", body).ObjectID; actual != objectID {
			t.Fatalf("HEAD tree body OID=%q want=%q", actual, objectID)
		}
		rawObjects = append(rawObjects, reviewFreezeCompileGitRawFixtureObjectV1{Kind: "tree", Body: body})
	}
	return reviewFreezeCompileGitRawAdmissionFixtureV1{
		verified:      verified,
		repository:    leaf.repository,
		module:        leaf.module,
		rawObjects:    rawObjects,
		baseCommitSHA: commitSHA,
		baseTreeSHA:   treeSHA,
	}
}

// TestW2ReviewFreezeCompileGitRawAdmissionV1HEADTreeCleanWorktreeEndToEnd 证明当前 HEAD
// commit/tree 与 worktree 的 14 个目标 leaf clean gate 可以形成组合结论。它不是脱离
// worktree 的纯 HEAD golden；view 消费也不会导致底层 CAS 二次 Open。
func TestW2ReviewFreezeCompileGitRawAdmissionV1HEADTreeCleanWorktreeEndToEnd(t *testing.T) {
	fixture := reviewFreezeNewCompileGitRawAdmissionFixtureV1(t)
	repositoryLoader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture.repository)
	moduleLoader := reviewFreezeNewCompileModuleLeafFixtureLoaderV1(t, fixture.module.files, "git-raw-admission-head-tree-clean-worktree-x-text-v0.34.0")
	rawLoader := reviewFreezeCompileGitRawLoaderFixtureNewV1(fixture.rawObjects)
	components, err := reviewFreezeAdmitCompileGitRawComponentsV1(
		context.Background(), fixture.verified, repositoryLoader, moduleLoader, rawLoader,
	)
	if err != nil {
		t.Fatalf("admit HEAD tree/clean-worktree git raw components: %v", err)
	}
	result := components.Admission()
	if result.baseCommitSHA != fixture.baseCommitSHA || result.baseTreeSHA != fixture.baseTreeSHA ||
		result.Assurance() != reviewFreezeCompileGitRawAdmissionAssuranceV1 {
		t.Fatalf("HEAD tree/clean-worktree subject/assurance=%q/%q/%q", result.baseCommitSHA, result.baseTreeSHA, result.Assurance())
	}
	if rawLoader.listCalls != 1 || rawLoader.totalOpenCalls() != len(rawLoader.descriptors) {
		t.Fatalf("raw loader lifecycle List/Open=%d/%d descriptors=%d", rawLoader.listCalls, rawLoader.totalOpenCalls(), len(rawLoader.descriptors))
	}
	for _, descriptor := range rawLoader.descriptors {
		if rawLoader.openCalls[descriptor.ObjectID] != 1 {
			t.Fatalf("raw loader object=%s Open=%d want=1", descriptor.ObjectID, rawLoader.openCalls[descriptor.ObjectID])
		}
	}
	openBeforeAccessor := rawLoader.openCallSnapshot()
	_ = components.CommitBinding()
	_ = components.TreeMembership()
	_ = components.RawBundleObjectIDs()
	if !reflect.DeepEqual(openBeforeAccessor, rawLoader.openCallSnapshot()) {
		t.Fatalf("component accessor revisited raw CAS before=%v after=%v", openBeforeAccessor, rawLoader.openCallSnapshot())
	}
	if repositoryLoader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(repositoryLoader) != reviewFreezeCompileRepositoryLeafCountV1 {
		t.Fatalf("repository loader List/Open=%d/%d", repositoryLoader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(repositoryLoader))
	}
	if moduleLoader.listCalls != 1 || len(moduleLoader.openCalls) != len(reviewFreezeCompileInputSnapshotModulePathsV1()) {
		t.Fatalf("module loader List/distinct Open=%d/%d", moduleLoader.listCalls, len(moduleLoader.openCalls))
	}
	if !reflect.DeepEqual(result.ClosedSemanticGaps(), reviewFreezeCompileGitRawAdmissionClosedGapsV1()) ||
		!reflect.DeepEqual(result.OpenGaps(), reviewFreezeCompileGitRawAdmissionOpenGapsV1()) ||
		result.Authority() || result.FormalFreezeEligible() {
		t.Fatalf("HEAD tree/clean-worktree result boundary closed=%v open=%v authority/freeze=%v/%v", result.ClosedSemanticGaps(), result.OpenGaps(), result.Authority(), result.FormalFreezeEligible())
	}
	if err := reviewFreezeValidateCompileGitRawAdmissionComponentsV1(components); err != nil {
		t.Fatalf("validate HEAD tree/clean-worktree components: %v", err)
	}

	t.Run("accessors are immutable", func(t *testing.T) {
		statementRaw := components.StatementRaw()
		snapshotRaw := components.SnapshotRaw()
		paths := result.RepositoryPaths()
		objects := result.GitObjectIDs()
		closed := result.ClosedSemanticGaps()
		open := result.OpenGaps()
		statementRaw[0] ^= 0xff
		snapshotRaw[0] ^= 0xff
		paths[0] = "forged"
		objects[0] = strings.Repeat("f", 40)
		closed[0] = "forged"
		open[0] = "forged"
		commit := components.CommitBinding()
		commit.parents = append(commit.parents, strings.Repeat("f", 40))
		commit.frame = "forged"
		tree := components.TreeMembership()
		tree.paths[0] = "forged"
		tree.objectIDs[0] = strings.Repeat("f", 40)
		if bytes.Equal(statementRaw, components.StatementRaw()) || bytes.Equal(snapshotRaw, components.SnapshotRaw()) ||
			reflect.DeepEqual(paths, components.Admission().RepositoryPaths()) ||
			reflect.DeepEqual(objects, components.Admission().GitObjectIDs()) ||
			reflect.DeepEqual(closed, components.Admission().ClosedSemanticGaps()) ||
			reflect.DeepEqual(open, components.Admission().OpenGaps()) ||
			components.CommitBinding().frame == "forged" || components.TreeMembership().paths[0] == "forged" {
			t.Fatal("combined admission accessor 暴露了可变内部状态")
		}
	})

	t.Run("exact union rejects missing extra and stale", func(t *testing.T) {
		commitIDs := components.CommitBinding().UsedObjectIDs()
		treeIDs := components.TreeMembership().ObjectIDs()
		want := components.RawBundleObjectIDs()
		if len(treeIDs) < 2 {
			t.Fatalf("tree fixture object count=%d want>=2", len(treeIDs))
		}
		tests := []struct {
			name   string
			mutate func([]string) []string
		}{
			{name: "missing", mutate: func(ids []string) []string { return ids[:len(ids)-1] }},
			{name: "extra", mutate: func(ids []string) []string {
				return append(ids, reviewFreezeCompileGitRawAdmissionUnusedOIDV1(want, 'e'))
			}},
			{name: "stale replacement", mutate: func(ids []string) []string {
				ids[0] = reviewFreezeCompileGitRawAdmissionUnusedOIDV1(want, 'd')
				return ids
			}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				changed := test.mutate(append([]string(nil), treeIDs...))
				if _, err := reviewFreezeCompileGitRawAdmissionExactUnionV1(commitIDs, changed, want); err == nil || !strings.Contains(err.Error(), "exact union") {
					t.Fatalf("exact union mutation error=%v", err)
				}
			})
		}
	})

	t.Run("cross statement tree and leaf mismatch", func(t *testing.T) {
		tests := []struct {
			name   string
			mutate func(*reviewFreezeVerifiedCompileCommitBindingV1, *reviewFreezeVerifiedCompileGitBaseTreeV1)
		}{
			{name: "statement commit", mutate: func(commit *reviewFreezeVerifiedCompileCommitBindingV1, _ *reviewFreezeVerifiedCompileGitBaseTreeV1) {
				commit.commitSHA = reviewFreezeCompileGitRawAdmissionUnusedOIDV1(components.RawBundleObjectIDs(), 'c')
			}},
			{name: "commit tree", mutate: func(commit *reviewFreezeVerifiedCompileCommitBindingV1, _ *reviewFreezeVerifiedCompileGitBaseTreeV1) {
				commit.treeSHA = reviewFreezeCompileGitRawAdmissionUnusedOIDV1(components.RawBundleObjectIDs(), 'b')
			}},
			{name: "tree leaf paths", mutate: func(_ *reviewFreezeVerifiedCompileCommitBindingV1, tree *reviewFreezeVerifiedCompileGitBaseTreeV1) {
				tree.paths = tree.paths[:len(tree.paths)-1]
			}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				commit := components.CommitBinding()
				tree := components.TreeMembership()
				test.mutate(commit, tree)
				if _, err := reviewFreezeAssembleCompileGitRawAdmissionV1(components.leafComponents, components.rawBundle, commit, tree); err == nil {
					t.Fatal("cross-result mismatch was admitted")
				}
			})
		}
	})

	t.Run("scope elevation is rejected", func(t *testing.T) {
		mutations := []struct {
			name   string
			mutate func(*reviewFreezeCompileGitRawAdmissionV1)
		}{
			{name: "github stable read", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.githubReportedOIDStableDoubleRead = true }},
			{name: "trusted source", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) {
				result.trustedExpectedSourceCommitExactEqual = true
			}},
			{name: "github repository", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.githubRepositoryAuthority = true }},
			{name: "github observation", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.githubObservationAuthority = true }},
			{name: "artifact derivation", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.artifactSourceDerivation = true }},
			{name: "builder", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.builderExecutionProven = true }},
			{name: "build closure", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.buildClosureVerified = true }},
			{name: "sbom", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.sbomVerified = true }},
			{name: "toolchain", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.toolchainVerified = true }},
			{name: "builder pair", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.independentBuilderPairVerified = true }},
			{name: "signature", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.signatureAuthority = true }},
			{name: "authority", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.authority = true }},
			{name: "formal freeze", mutate: func(result *reviewFreezeCompileGitRawAdmissionV1) { result.formalFreezeEligible = true }},
		}
		for _, mutation := range mutations {
			t.Run(mutation.name, func(t *testing.T) {
				forged := reviewFreezeCompileGitRawAdmissionCloneV1(result)
				mutation.mutate(forged)
				if err := reviewFreezeValidateCompileGitRawAdmissionBoundaryV1(
					forged, components.leafComponents, components.rawBundle, components.commitBinding, components.treeMembership,
				); err == nil {
					t.Fatal("scope elevation was admitted")
				}
			})
		}
	})
}

// reviewFreezeCompileGitRawAdmissionUnusedOIDV1 返回不属于给定 exact-set 的合法 lowercase
// SHA-1 字符串，用于 missing/extra/stale 和 cross-result 对抗。
func reviewFreezeCompileGitRawAdmissionUnusedOIDV1(existing []string, seed byte) string {
	for candidate := seed; candidate >= '1' && candidate <= 'f'; candidate++ {
		if (candidate > '9' && candidate < 'a') || candidate == '0' {
			continue
		}
		objectID := strings.Repeat(string(candidate), 40)
		found := false
		for _, existingID := range existing {
			if objectID == existingID {
				found = true
				break
			}
		}
		if !found {
			return objectID
		}
	}
	return strings.Repeat("1", 40)
}
