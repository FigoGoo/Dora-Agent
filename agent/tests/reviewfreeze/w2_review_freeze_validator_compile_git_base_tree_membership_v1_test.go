package reviewfreeze_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	reviewFreezeCompileGitTreeMaxObjectsV1     = 64
	reviewFreezeCompileGitTreeMaxTotalBytesV1  = 8 << 20
	reviewFreezeCompileGitTreeMaxObjectBytesV1 = 1 << 20
	reviewFreezeCompileGitTreeMaxDepthV1       = 16
	reviewFreezeCompileGitTreeMaxEntriesV1     = 32768

	reviewFreezeCompileGitDirectMaxTreeBodyBytesV1  = reviewFreezeCompileGitRawBundleMaxTreeBodyV1
	reviewFreezeCompileGitDirectMaxTotalBodyBytesV1 = reviewFreezeCompileGitRawBundleMaxTreeTotalBodyV1
	reviewFreezeCompileGitDirectMaxObjectsV1        = reviewFreezeCompileGitRawBundleMaxTreeObjectsV1
	reviewFreezeCompileGitDirectMaxDepthV1          = reviewFreezeCompileGitTreeMaxDepthV1
	reviewFreezeCompileGitDirectMaxEntriesPerTreeV1 = 4096
	reviewFreezeCompileGitDirectMaxTotalEntriesV1   = reviewFreezeCompileGitTreeMaxEntriesV1

	reviewFreezeCompileGitBaseTreeClaimV1           = "base_tree_membership_verified"
	reviewFreezeCompileGitBaseCommitGapV1           = "base_commit_binding_unverified"
	reviewFreezeCompileGitCommitAncestryGapV1       = "commit_ancestry_unverified"
	reviewFreezeCompileGitGitHubAuthorityGapV1      = "github_authority_unverified"
	reviewFreezeCompileGitFormalFreezeNotProvenV1   = "not_established"
	reviewFreezeCompileGitTreeRegularBlobModeV1     = "100644"
	reviewFreezeCompileGitTreeExecutableBlobModeV1  = "100755"
	reviewFreezeCompileGitTreeSymlinkBlobModeV1     = "120000"
	reviewFreezeCompileGitTreeDirectoryModeV1       = "40000"
	reviewFreezeCompileGitTreeSubmoduleCommitModeV1 = "160000"
)

var errReviewFreezeCompileGitTreeEntryBudgetV1 = errors.New("git tree parser entry budget exhausted")

// reviewFreezeCompileGitObjectOpenedV1 是 CAS 对一次 object Open 的原子观察。
// ObjectID 必须在打开后重新返回，Reader.Close 必须能主动打断阻塞 Read。
type reviewFreezeCompileGitObjectOpenedV1 struct {
	ObjectID string
	Reader   io.ReadCloser
}

// reviewFreezeCompileGitObjectDescriptorV1 是 CAS List 冻结的 object descriptor。
// SHA256 覆盖完整 raw framed object；Kind/DeclaredBodySize 还必须与 Open 后的 framing
// 三向一致，不能仅因 OID 看似合法就信任 loader 元数据。
type reviewFreezeCompileGitObjectDescriptorV1 struct {
	ObjectID         string
	Kind             string
	DeclaredBodySize int64
	SHA256           string
}

// reviewFreezeCompileGitObjectLoaderV1 是 test-only Git object CAS 的最小消费方边界。
// verifier 先 exact List descriptor，再只按 statement 的 base_tree_sha 及 tree entry
// 派生的 SHA-1 各 Open 一次；成功时所有 listed object 必须恰好被目标路径消费。
type reviewFreezeCompileGitObjectLoaderV1 interface {
	List(context.Context) ([]reviewFreezeCompileGitObjectDescriptorV1, error)
	Open(context.Context, string) (reviewFreezeCompileGitObjectOpenedV1, error)
}

// reviewFreezeCompileGitTreeBudgetsV1 冻结一次递归验证共享的对象、字节和深度预算。
type reviewFreezeCompileGitTreeBudgetsV1 struct {
	MaxObjects     int
	MaxTotalBytes  int64
	MaxObjectBytes int64
	MaxDepth       int
	MaxEntries     int
}

// reviewFreezeCompileGitDirectTreeBudgetsV1 是 raw-bundle 后的纯语义预算。
// 它按唯一 used tree body 计费，不重复承担 resolver 已完成的外部 I/O、OID 或摘要门禁。
type reviewFreezeCompileGitDirectTreeBudgetsV1 struct {
	MaxTreeBodyBytes  int64
	MaxTotalBodyBytes int64
	MaxObjects        int
	MaxDepth          int
	MaxEntriesPerTree int
	MaxTotalEntries   int
}

// reviewFreezeCompileGitTreeEntryV1 是 raw Git tree payload 的一个严格条目。
type reviewFreezeCompileGitTreeEntryV1 struct {
	Mode     string
	Name     string
	ObjectID string
}

// reviewFreezeCompileGitBaseTreeScopeV1 明确本证明层只关闭 base tree membership。
// Base commit、commit ancestry、GitHub authority 和 Formal Freeze 均仍未建立。
type reviewFreezeCompileGitBaseTreeScopeV1 struct {
	VerifiedClaim      string
	BaseTreeMembership bool
	BaseCommitBinding  bool
	CommitAncestry     bool
	GitHubAuthority    bool
	FormalFreezeStatus string
	OpenGaps           []string
}

// reviewFreezeVerifiedCompileGitBaseTreeV1 是不可变的 base tree membership 结果。
// paths 只包含 strict snapshot 的 14 个 repository leaf，不枚举或背书无关 tree entry。
type reviewFreezeVerifiedCompileGitBaseTreeV1 struct {
	baseTreeSHA string
	paths       []string
	objectIDs   []string
	objectCount int
	totalBytes  int64
	maxDepth    int
}

// ObjectIDs 返回本次 exact consumption 使用的排序 tree OID 副本。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) ObjectIDs() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.objectIDs...)
}

// BaseTreeSHA 返回本次已逐对象验证的 root tree SHA-1。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) BaseTreeSHA() string {
	if result == nil {
		return ""
	}
	return result.baseTreeSHA
}

// Paths 返回已证明映射到 snapshot GitBlobSHA 的有序路径副本。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) Paths() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.paths...)
}

// ObjectCount 返回实际校验 framing、SHA-1、类型和 size 的唯一 tree object 数。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) ObjectCount() int {
	if result == nil {
		return 0
	}
	return result.objectCount
}

// TotalBytes 返回已读取并纳入共享预算的 raw framed object 总字节数。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) TotalBytes() int64 {
	if result == nil {
		return 0
	}
	return result.totalBytes
}

// MaxDepth 返回 root=0 计数时实际到达的最大目标 tree 深度。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) MaxDepth() int {
	if result == nil {
		return 0
	}
	return result.maxDepth
}

// Scope 返回证明边界副本，调用方不能借此结果宣称 base commit 或 Formal Freeze。
func (result *reviewFreezeVerifiedCompileGitBaseTreeV1) Scope() reviewFreezeCompileGitBaseTreeScopeV1 {
	if result == nil {
		return reviewFreezeCompileGitBaseTreeScopeV1{}
	}
	return reviewFreezeCompileGitBaseTreeScopeV1{
		VerifiedClaim:      reviewFreezeCompileGitBaseTreeClaimV1,
		BaseTreeMembership: true,
		FormalFreezeStatus: reviewFreezeCompileGitFormalFreezeNotProvenV1,
		OpenGaps: []string{
			reviewFreezeCompileGitBaseCommitGapV1,
			reviewFreezeCompileGitCommitAncestryGapV1,
			reviewFreezeCompileGitGitHubAuthorityGapV1,
		},
	}
}

// reviewFreezeCompileGitTargetNodeV1 是由 strict snapshot exact-set 派生的路径 trie。
// leaf 与 children 互斥，避免文件/目录双重解释。
type reviewFreezeCompileGitTargetNodeV1 struct {
	children map[string]*reviewFreezeCompileGitTargetNodeV1
	leaf     *reviewFreezeCompileInputSnapshotRepoFileV1
}

// reviewFreezeCompileGitTreeVerifierV1 保存单次递归调用共享的 CAS cache 和预算计数。
type reviewFreezeCompileGitTreeVerifierV1 struct {
	ctx         context.Context
	loader      reviewFreezeCompileGitObjectLoaderV1
	budgets     reviewFreezeCompileGitTreeBudgetsV1
	descriptors map[string]reviewFreezeCompileGitObjectDescriptorV1
	consumed    map[string]struct{}
	cache       map[string][]reviewFreezeCompileGitTreeEntryV1
	objectCount int
	totalBytes  int64
	entryCount  int
	maxDepth    int
	paths       []string
}

// reviewFreezeCompileGitDirectTreeVerifierV1 直接遍历 raw bundle 的冻结 tree body。
// cache 允许同一 tree 在 DAG 的不同已退出分支复用；active 只表示当前递归栈，用来
// 拒绝祖先回边。body/framed/entry 计数均只对唯一 used OID 记一次。
type reviewFreezeCompileGitDirectTreeVerifierV1 struct {
	ctx              context.Context
	bundle           *reviewFreezeCompileGitRawObjectBundleV1
	budgets          reviewFreezeCompileGitDirectTreeBudgetsV1
	cache            map[string][]reviewFreezeCompileGitTreeEntryV1
	active           map[string]struct{}
	used             map[string]struct{}
	objectCount      int
	totalBodyBytes   int64
	totalFramedBytes int64
	totalEntries     int
	maxDepth         int
	paths            []string
}

// reviewFreezeVerifyCompileGitBaseTreeMembershipV1 使用生产候选默认预算验证 BaseTree。
func reviewFreezeVerifyCompileGitBaseTreeMembershipV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	leaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
	loader reviewFreezeCompileGitObjectLoaderV1,
) (*reviewFreezeVerifiedCompileGitBaseTreeV1, error) {
	return reviewFreezeVerifyCompileGitBaseTreeMembershipWithBudgetsV1(
		ctx,
		snapshotRaw,
		statement,
		leaves,
		loader,
		reviewFreezeCompileGitTreeBudgetsV1{
			MaxObjects:     reviewFreezeCompileGitTreeMaxObjectsV1,
			MaxTotalBytes:  reviewFreezeCompileGitTreeMaxTotalBytesV1,
			MaxObjectBytes: reviewFreezeCompileGitTreeMaxObjectBytesV1,
			MaxDepth:       reviewFreezeCompileGitTreeMaxDepthV1,
			MaxEntries:     reviewFreezeCompileGitTreeMaxEntriesV1,
		},
	)
}

// reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1 直接消费 resolver 已冻结的
// tree body。该入口不创建 List/Open view，不读取 Reader，也不重算 body digest、Git OID
// 或 transport frame；这些外部边界只归 raw resolver。tree 层只执行目标 membership、
// raw tree grammar、DAG/cycle 与 used-object 语义预算。
func reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	leaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
	bundle *reviewFreezeCompileGitRawObjectBundleV1,
) (*reviewFreezeVerifiedCompileGitBaseTreeV1, error) {
	return reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleWithBudgetsV1(
		ctx,
		snapshotRaw,
		statement,
		leaves,
		bundle,
		reviewFreezeCompileGitDirectDefaultBudgetsV1(),
	)
}

// reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleWithBudgetsV1 是 direct 入口的
// 可测核心。extra bundle object 故意不在这里拒绝；commit/tree union exact-set 由更高层
// admission 统一判断，tree semantic 层只返回实际 Used ObjectIDs。
func reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleWithBudgetsV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	leaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
	bundle *reviewFreezeCompileGitRawObjectBundleV1,
	budgets reviewFreezeCompileGitDirectTreeBudgetsV1,
) (*reviewFreezeVerifiedCompileGitBaseTreeV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("git direct base tree context 不能为空")
	}
	if leaves == nil {
		return nil, fmt.Errorf("git direct base tree verified repository leaves 不能为空")
	}
	if bundle == nil {
		return nil, fmt.Errorf("git direct base tree raw bundle 不能为空")
	}
	if budgets.MaxTreeBodyBytes <= 0 || budgets.MaxTotalBodyBytes <= 0 || budgets.MaxObjects <= 0 || budgets.MaxDepth < 0 || budgets.MaxEntriesPerTree <= 0 || budgets.MaxTotalEntries <= 0 {
		return nil, fmt.Errorf("git direct base tree budgets 非法=%+v", budgets)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("git direct base tree context: %w", err)
	}
	if !reviewFreezeGitSHA1V1.MatchString(statement.Subject.BaseTreeSHA) || statement.Subject.BaseTreeSHA == strings.Repeat("0", 40) {
		return nil, fmt.Errorf("git direct base tree SHA-1 非法=%q", statement.Subject.BaseTreeSHA)
	}

	expected, _, err := reviewFreezeDeriveCompileRepositoryLeavesV1(snapshotRaw, statement)
	if err != nil {
		return nil, fmt.Errorf("git direct base tree strict inputs: %w", err)
	}
	if err := reviewFreezeValidateCompileGitLeafBundleBindingV1(expected, leaves); err != nil {
		return nil, err
	}
	targets, err := reviewFreezeBuildCompileGitTargetTrieV1(expected)
	if err != nil {
		return nil, err
	}
	verifier := &reviewFreezeCompileGitDirectTreeVerifierV1{
		ctx:     ctx,
		bundle:  bundle,
		budgets: budgets,
		cache:   make(map[string][]reviewFreezeCompileGitTreeEntryV1),
		active:  make(map[string]struct{}),
		used:    make(map[string]struct{}),
		paths:   make([]string, 0, len(expected)),
	}
	if err := verifier.walk(statement.Subject.BaseTreeSHA, targets, "", 0); err != nil {
		return nil, err
	}
	wantPaths := make([]string, len(expected))
	for index := range expected {
		wantPaths[index] = expected[index].Path
	}
	sort.Strings(verifier.paths)
	if !reflect.DeepEqual(verifier.paths, wantPaths) {
		return nil, fmt.Errorf("git direct base tree verified path exact-set=%v want=%v", verifier.paths, wantPaths)
	}
	objectIDs := make([]string, 0, len(verifier.used))
	for objectID := range verifier.used {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Strings(objectIDs)
	return &reviewFreezeVerifiedCompileGitBaseTreeV1{
		baseTreeSHA: statement.Subject.BaseTreeSHA,
		paths:       append([]string(nil), verifier.paths...),
		objectIDs:   objectIDs,
		objectCount: verifier.objectCount,
		// TotalBytes 保持 legacy 的 canonical framed 统计口径；语义预算使用
		// totalBodyBytes，绝不把派生 header 字节挤占 8 MiB tree body 上限。
		totalBytes: verifier.totalFramedBytes,
		maxDepth:   verifier.maxDepth,
	}, nil
}

// reviewFreezeVerifyCompileGitBaseTreeMembershipWithBudgetsV1 先完成 strict snapshot 与
// 已验证 leaf bundle 的同源绑定，再触碰 CAS。成功只表示 14 个 path 在 BaseTree 中以
// mode 100644 指向相同 Git blob SHA-1；它故意不读取或解释 BaseCommitSHA。
func reviewFreezeVerifyCompileGitBaseTreeMembershipWithBudgetsV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	leaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1,
	loader reviewFreezeCompileGitObjectLoaderV1,
	budgets reviewFreezeCompileGitTreeBudgetsV1,
) (*reviewFreezeVerifiedCompileGitBaseTreeV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("git base tree context 不能为空")
	}
	if leaves == nil {
		return nil, fmt.Errorf("git base tree verified repository leaves 不能为空")
	}
	if loader == nil {
		return nil, fmt.Errorf("git base tree object loader 不能为空")
	}
	if budgets.MaxObjects <= 0 || budgets.MaxTotalBytes <= 0 || budgets.MaxObjectBytes <= 0 || budgets.MaxDepth < 0 || budgets.MaxEntries <= 0 {
		return nil, fmt.Errorf("git base tree budgets 非法=%+v", budgets)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("git base tree context: %w", err)
	}
	if !reviewFreezeGitSHA1V1.MatchString(statement.Subject.BaseTreeSHA) || statement.Subject.BaseTreeSHA == strings.Repeat("0", 40) {
		return nil, fmt.Errorf("git base tree SHA-1 非法=%q", statement.Subject.BaseTreeSHA)
	}

	expected, _, err := reviewFreezeDeriveCompileRepositoryLeavesV1(snapshotRaw, statement)
	if err != nil {
		return nil, fmt.Errorf("git base tree strict inputs: %w", err)
	}
	if err := reviewFreezeValidateCompileGitLeafBundleBindingV1(expected, leaves); err != nil {
		return nil, err
	}
	targets, err := reviewFreezeBuildCompileGitTargetTrieV1(expected)
	if err != nil {
		return nil, err
	}

	verifier := &reviewFreezeCompileGitTreeVerifierV1{
		ctx:         ctx,
		loader:      loader,
		budgets:     budgets,
		descriptors: make(map[string]reviewFreezeCompileGitObjectDescriptorV1),
		consumed:    make(map[string]struct{}),
		cache:       make(map[string][]reviewFreezeCompileGitTreeEntryV1),
		paths:       make([]string, 0, len(expected)),
	}
	if err := verifier.listExactDescriptors(statement.Subject.BaseTreeSHA); err != nil {
		return nil, err
	}
	if err := verifier.walk(statement.Subject.BaseTreeSHA, targets, "", 0); err != nil {
		return nil, err
	}
	if len(verifier.consumed) != len(verifier.descriptors) {
		extra := make([]string, 0, len(verifier.descriptors)-len(verifier.consumed))
		for objectID := range verifier.descriptors {
			if _, consumed := verifier.consumed[objectID]; !consumed {
				extra = append(extra, objectID)
			}
		}
		sort.Strings(extra)
		return nil, fmt.Errorf("git tree descriptor exact consumption 存在 extra objects=%v", extra)
	}
	wantPaths := make([]string, len(expected))
	for index := range expected {
		wantPaths[index] = expected[index].Path
	}
	sort.Strings(verifier.paths)
	if !reflect.DeepEqual(verifier.paths, wantPaths) {
		return nil, fmt.Errorf("git base tree verified path exact-set=%v want=%v", verifier.paths, wantPaths)
	}
	objectIDs := make([]string, 0, len(verifier.consumed))
	for objectID := range verifier.consumed {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Strings(objectIDs)
	return &reviewFreezeVerifiedCompileGitBaseTreeV1{
		baseTreeSHA: statement.Subject.BaseTreeSHA,
		paths:       append([]string(nil), verifier.paths...),
		objectIDs:   objectIDs,
		objectCount: verifier.objectCount,
		totalBytes:  verifier.totalBytes,
		maxDepth:    verifier.maxDepth,
	}, nil
}

// reviewFreezeValidateCompileGitLeafBundleBindingV1 防止调用方把另一份 snapshot 的
// 已验证 leaf bundle 与当前 statement/BaseTree 拼接。失败发生在第一次 CAS Open 前。
func reviewFreezeValidateCompileGitLeafBundleBindingV1(expected []reviewFreezeCompileInputSnapshotRepoFileV1, leaves *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) error {
	wantPaths := make([]string, len(expected))
	for index, want := range expected {
		wantPaths[index] = want.Path
	}
	if !reflect.DeepEqual(leaves.Paths(), wantPaths) {
		return fmt.Errorf("git base tree repository leaf paths=%v want=%v", leaves.Paths(), wantPaths)
	}
	for _, want := range expected {
		metadata, raw, exists := leaves.Leaf(want.Path)
		if !exists || metadata != want {
			return fmt.Errorf("git base tree repository leaf metadata 未绑定 path=%q", want.Path)
		}
		if int64(len(raw)) != want.SizeBytes || reviewFreezeSHA256V1(raw) != want.SHA256 || reviewFreezeCompileRepositoryLeafGitBlobSHAV1(raw) != want.GitBlobSHA {
			return fmt.Errorf("git base tree repository leaf bytes 未绑定 path=%q", want.Path)
		}
	}
	return nil
}

// reviewFreezeBuildCompileGitTargetTrieV1 把已排序、已 strict 校验的 14 个路径转换为 trie。
func reviewFreezeBuildCompileGitTargetTrieV1(expected []reviewFreezeCompileInputSnapshotRepoFileV1) (*reviewFreezeCompileGitTargetNodeV1, error) {
	root := &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
	for index := range expected {
		leaf := expected[index]
		segments := strings.Split(leaf.Path, "/")
		node := root
		for segmentIndex, segment := range segments {
			child := node.children[segment]
			if child == nil {
				child = &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
				node.children[segment] = child
			}
			if node.leaf != nil {
				return nil, fmt.Errorf("git base tree target path prefix conflict=%q", leaf.Path)
			}
			node = child
			if segmentIndex == len(segments)-1 {
				if node.leaf != nil || len(node.children) != 0 {
					return nil, fmt.Errorf("git base tree duplicate/file-directory target conflict=%q", leaf.Path)
				}
				node.leaf = &expected[index]
			}
		}
	}
	return root, nil
}

// listExactDescriptors 在任何 Open 前一次性冻结 CAS descriptor 集合，拒绝空集合、
// duplicate、非法 lowercase OID、非 tree kind、摘要/size 漂移和声明预算超额。
// 由于 descendant OID 只能从 raw parent tree 得知，exact-set 的另一半在成功出口通过
// “所有 listed object 均被消费”完成，extra descriptor 不会被静默接受。
func (verifier *reviewFreezeCompileGitTreeVerifierV1) listExactDescriptors(rootObjectID string) error {
	listed, err := verifier.loader.List(verifier.ctx)
	if err != nil {
		return fmt.Errorf("git tree descriptor List: %w", err)
	}
	if err := verifier.ctx.Err(); err != nil {
		return fmt.Errorf("git tree context after List: %w", err)
	}
	if len(listed) == 0 || len(listed) > verifier.budgets.MaxObjects {
		return fmt.Errorf("git tree descriptor object count budget count=%d limit=%d", len(listed), verifier.budgets.MaxObjects)
	}
	declaredFramedTotal := int64(0)
	for _, descriptor := range listed {
		if !reviewFreezeGitSHA1V1.MatchString(descriptor.ObjectID) || descriptor.ObjectID == strings.Repeat("0", 40) {
			return fmt.Errorf("git tree descriptor lowercase OID 非法=%q", descriptor.ObjectID)
		}
		if _, duplicate := verifier.descriptors[descriptor.ObjectID]; duplicate {
			return fmt.Errorf("git tree descriptor duplicate OID=%q", descriptor.ObjectID)
		}
		if descriptor.Kind != "tree" {
			return fmt.Errorf("git tree descriptor kind=%q object=%s", descriptor.Kind, descriptor.ObjectID)
		}
		if descriptor.DeclaredBodySize < 0 {
			return fmt.Errorf("git tree descriptor body size=%d object=%s", descriptor.DeclaredBodySize, descriptor.ObjectID)
		}
		if descriptor.DeclaredBodySize > verifier.budgets.MaxObjectBytes {
			return fmt.Errorf("git tree descriptor object bytes budget object=%s body=%d limit=%d", descriptor.ObjectID, descriptor.DeclaredBodySize, verifier.budgets.MaxObjectBytes)
		}
		framedSize := reviewFreezeCompileGitTreeFramedSizeV1(descriptor.DeclaredBodySize)
		if framedSize < descriptor.DeclaredBodySize || framedSize > verifier.budgets.MaxObjectBytes {
			return fmt.Errorf("git tree descriptor object bytes budget object=%s framed=%d limit=%d", descriptor.ObjectID, framedSize, verifier.budgets.MaxObjectBytes)
		}
		if !reviewFreezePrefixedSHA256V1.MatchString(descriptor.SHA256) {
			return fmt.Errorf("git tree descriptor SHA-256 非法 object=%s digest=%q", descriptor.ObjectID, descriptor.SHA256)
		}
		if framedSize > verifier.budgets.MaxTotalBytes-declaredFramedTotal {
			return fmt.Errorf("git tree descriptor total bytes budget object=%s limit=%d", descriptor.ObjectID, verifier.budgets.MaxTotalBytes)
		}
		declaredFramedTotal += framedSize
		verifier.descriptors[descriptor.ObjectID] = descriptor
	}
	if _, exists := verifier.descriptors[rootObjectID]; !exists {
		return fmt.Errorf("git tree descriptor missing root OID=%q", rootObjectID)
	}
	return nil
}

// walk 执行 direct raw-bundle 的统一目标遍历。active 只覆盖当前递归调用栈，因此
// ancestor 回边失败关闭，而已经退出的相同 OID 可从 cache 在另一 DAG 分支复用。
func (verifier *reviewFreezeCompileGitDirectTreeVerifierV1) walk(treeID string, targets *reviewFreezeCompileGitTargetNodeV1, prefix string, depth int) error {
	if err := verifier.ctx.Err(); err != nil {
		return fmt.Errorf("git direct base tree context before walk prefix=%q: %w", prefix, err)
	}
	if depth > verifier.budgets.MaxDepth {
		return fmt.Errorf("git direct base tree depth budget prefix=%q depth=%d limit=%d", prefix, depth, verifier.budgets.MaxDepth)
	}
	if _, cyclic := verifier.active[treeID]; cyclic {
		return fmt.Errorf("git direct base tree active recursion cycle prefix=%q object=%s", prefix, treeID)
	}
	entries, err := verifier.loadTree(treeID)
	if err != nil {
		return fmt.Errorf("git direct base tree load prefix=%q object=%s: %w", prefix, treeID, err)
	}
	if depth > verifier.maxDepth {
		verifier.maxDepth = depth
	}
	verifier.active[treeID] = struct{}{}
	defer delete(verifier.active, treeID)

	byName := make(map[string]reviewFreezeCompileGitTreeEntryV1, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	targetNames := make([]string, 0, len(targets.children))
	for name := range targets.children {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	for _, name := range targetNames {
		child := targets.children[name]
		entry, exists := byName[name]
		path := reviewFreezeCompileGitJoinPathV1(prefix, name)
		if !exists {
			return fmt.Errorf("git direct base tree target missing path=%q", path)
		}
		if child.leaf != nil {
			if len(child.children) != 0 {
				return fmt.Errorf("git direct base tree internal leaf/children conflict path=%q", path)
			}
			if entry.Mode != reviewFreezeCompileGitTreeRegularBlobModeV1 {
				return fmt.Errorf("git direct base tree target 必须是 exact 100644 blob path=%q mode=%q", path, entry.Mode)
			}
			if entry.ObjectID != child.leaf.GitBlobSHA {
				return fmt.Errorf("git direct base tree target blob drift path=%q actual=%q want=%q", path, entry.ObjectID, child.leaf.GitBlobSHA)
			}
			verifier.paths = append(verifier.paths, path)
			continue
		}
		if entry.Mode != reviewFreezeCompileGitTreeDirectoryModeV1 {
			return fmt.Errorf("git direct base tree target ancestor 必须是 tree path=%q mode=%q", path, entry.Mode)
		}
		if err := verifier.walk(entry.ObjectID, child, path, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// loadTree 从 frozen bundle 取得一个 tree body 并执行纯语义预算/grammar。
// 它不调用 transport accessor、不构造 frame、不重算 OID 或 body digest；同一 OID 的
// cache 命中不重复计对象、body 或 entries 预算。
func (verifier *reviewFreezeCompileGitDirectTreeVerifierV1) loadTree(objectID string) ([]reviewFreezeCompileGitTreeEntryV1, error) {
	if entries, exists := verifier.cache[objectID]; exists {
		return append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...), nil
	}
	if verifier.objectCount >= verifier.budgets.MaxObjects {
		return nil, fmt.Errorf("git direct tree object count budget limit=%d", verifier.budgets.MaxObjects)
	}
	descriptor, exists := verifier.bundle.Descriptor(objectID)
	if !exists {
		return nil, fmt.Errorf("git direct tree frozen object missing=%q", objectID)
	}
	if descriptor.Kind != "tree" {
		return nil, fmt.Errorf("git direct tree object kind=%q want=tree object=%s", descriptor.Kind, objectID)
	}
	body, exists := verifier.bundle.BodyBytes(objectID)
	if !exists {
		return nil, fmt.Errorf("git direct tree frozen body missing=%q", objectID)
	}
	bodyBytes := int64(len(body))
	if bodyBytes > verifier.budgets.MaxTreeBodyBytes {
		return nil, fmt.Errorf("git direct tree body budget object=%s actual=%d limit=%d", objectID, bodyBytes, verifier.budgets.MaxTreeBodyBytes)
	}
	if bodyBytes > verifier.budgets.MaxTotalBodyBytes-verifier.totalBodyBytes {
		return nil, fmt.Errorf("git direct tree total body budget object=%s consumed=%d actual=%d limit=%d", objectID, verifier.totalBodyBytes, bodyBytes, verifier.budgets.MaxTotalBodyBytes)
	}
	remainingEntries := verifier.budgets.MaxTotalEntries - verifier.totalEntries
	entryLimit := verifier.budgets.MaxEntriesPerTree
	entryBudgetName := "per-tree"
	if remainingEntries < entryLimit {
		entryLimit = remainingEntries
		entryBudgetName = "total"
	}
	entries, err := reviewFreezeParseCompileGitTreePayloadBoundedV1(body, entryLimit)
	if err != nil {
		if errors.Is(err, errReviewFreezeCompileGitTreeEntryBudgetV1) {
			return nil, fmt.Errorf("git direct tree %s entry budget object=%s consumed=%d limit=%d: %w", entryBudgetName, objectID, verifier.totalEntries, entryLimit, err)
		}
		return nil, err
	}
	if len(entries) > verifier.budgets.MaxEntriesPerTree {
		return nil, fmt.Errorf("git direct tree per-tree entry budget object=%s actual=%d limit=%d", objectID, len(entries), verifier.budgets.MaxEntriesPerTree)
	}
	if len(entries) > verifier.budgets.MaxTotalEntries-verifier.totalEntries {
		return nil, fmt.Errorf("git direct tree total entry budget object=%s consumed=%d actual=%d limit=%d", objectID, verifier.totalEntries, len(entries), verifier.budgets.MaxTotalEntries)
	}
	verifier.objectCount++
	verifier.totalBodyBytes += bodyBytes
	verifier.totalFramedBytes += reviewFreezeCompileGitTreeFramedSizeV1(bodyBytes)
	verifier.totalEntries += len(entries)
	verifier.used[objectID] = struct{}{}
	verifier.cache[objectID] = append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...)
	return append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...), nil
}

// walk 只递归目标 path 的 ancestor tree。无关条目只经过 raw Git framing、排序、
// duplicate 和 mode 校验；其 bytes 名称及 executable/symlink/submodule mode 不会被
// 错误当成目标 authority。只有固定 ASCII 目标 leaf/ancestor 才精确匹配并强制
// 100644/tree。targetNames 排序保证 CAS Open 与首个错误顺序不受 Go map 迭代影响。
func (verifier *reviewFreezeCompileGitTreeVerifierV1) walk(treeID string, targets *reviewFreezeCompileGitTargetNodeV1, prefix string, depth int) error {
	if err := verifier.ctx.Err(); err != nil {
		return fmt.Errorf("git base tree context before walk prefix=%q: %w", prefix, err)
	}
	if depth > verifier.budgets.MaxDepth {
		return fmt.Errorf("git base tree depth budget prefix=%q depth=%d limit=%d", prefix, depth, verifier.budgets.MaxDepth)
	}
	if depth > verifier.maxDepth {
		verifier.maxDepth = depth
	}
	entries, err := verifier.loadTree(treeID)
	if err != nil {
		return fmt.Errorf("git base tree load prefix=%q object=%s: %w", prefix, treeID, err)
	}
	byName := make(map[string]reviewFreezeCompileGitTreeEntryV1, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	targetNames := make([]string, 0, len(targets.children))
	for name := range targets.children {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	for _, name := range targetNames {
		child := targets.children[name]
		entry, exists := byName[name]
		if !exists {
			return fmt.Errorf("git base tree target missing path=%q", reviewFreezeCompileGitJoinPathV1(prefix, name))
		}
		path := reviewFreezeCompileGitJoinPathV1(prefix, name)
		if child.leaf != nil {
			if len(child.children) != 0 {
				return fmt.Errorf("git base tree internal leaf/children conflict path=%q", path)
			}
			if entry.Mode != reviewFreezeCompileGitTreeRegularBlobModeV1 {
				return fmt.Errorf("git base tree target 必须是 exact 100644 blob path=%q mode=%q", path, entry.Mode)
			}
			if entry.ObjectID != child.leaf.GitBlobSHA {
				return fmt.Errorf("git base tree target blob drift path=%q actual=%q want=%q", path, entry.ObjectID, child.leaf.GitBlobSHA)
			}
			verifier.paths = append(verifier.paths, path)
			continue
		}
		if entry.Mode != reviewFreezeCompileGitTreeDirectoryModeV1 {
			return fmt.Errorf("git base tree target ancestor 必须是 tree path=%q mode=%q", path, entry.Mode)
		}
		if err := verifier.walk(entry.ObjectID, child, path, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// loadTree 对每个 SHA-1 最多 Open 一次，逐对象验证 raw framing、requested identity、
// SHA-1、type 和 size，再解析 tree payload。共享预算在任何超额 Open 前失败关闭。
func (verifier *reviewFreezeCompileGitTreeVerifierV1) loadTree(objectID string) ([]reviewFreezeCompileGitTreeEntryV1, error) {
	if entries, exists := verifier.cache[objectID]; exists {
		return append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...), nil
	}
	if !reviewFreezeGitSHA1V1.MatchString(objectID) || objectID == strings.Repeat("0", 40) {
		return nil, fmt.Errorf("git tree child SHA-1 非法=%q", objectID)
	}
	descriptor, listed := verifier.descriptors[objectID]
	if !listed {
		return nil, fmt.Errorf("git tree descriptor missing discovered OID=%q", objectID)
	}
	if verifier.objectCount >= verifier.budgets.MaxObjects {
		return nil, fmt.Errorf("git tree object count budget limit=%d", verifier.budgets.MaxObjects)
	}
	if verifier.totalBytes >= verifier.budgets.MaxTotalBytes {
		return nil, fmt.Errorf("git tree total bytes budget limit=%d", verifier.budgets.MaxTotalBytes)
	}
	opened, err := verifier.loader.Open(verifier.ctx, objectID)
	if err != nil {
		if opened.Reader != nil {
			if closeErr := opened.Reader.Close(); closeErr != nil {
				return nil, fmt.Errorf("git tree Open object=%s: %w; close=%v", objectID, err, closeErr)
			}
		}
		return nil, fmt.Errorf("git tree Open object=%s: %w", objectID, err)
	}
	if opened.Reader == nil {
		return nil, fmt.Errorf("git tree Open missing reader object=%s", objectID)
	}
	if opened.ObjectID != objectID {
		closeErr := opened.Reader.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("git tree opened identity=%q want=%q; close=%v", opened.ObjectID, objectID, closeErr)
		}
		return nil, fmt.Errorf("git tree opened identity=%q want=%q", opened.ObjectID, objectID)
	}
	remaining := verifier.budgets.MaxTotalBytes - verifier.totalBytes
	limit := reviewFreezeCompileGitTreeFramedSizeV1(descriptor.DeclaredBodySize)
	if remaining < limit {
		limit = remaining
	}
	raw, err := reviewFreezeReadCompileGitObjectBoundedV1(verifier.ctx, opened.Reader, objectID, limit+1)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > verifier.budgets.MaxObjectBytes {
		return nil, fmt.Errorf("git tree object bytes budget object=%s actual=%d limit=%d", objectID, len(raw), verifier.budgets.MaxObjectBytes)
	}
	if int64(len(raw)) > remaining {
		return nil, fmt.Errorf("git tree total bytes budget object=%s actual>remaining=%d", objectID, remaining)
	}
	if int64(len(raw)) != reviewFreezeCompileGitTreeFramedSizeV1(descriptor.DeclaredBodySize) {
		return nil, fmt.Errorf("git tree descriptor framed size drift object=%s actual=%d declared_body=%d", objectID, len(raw), descriptor.DeclaredBodySize)
	}
	if actualSHA256 := reviewFreezeSHA256V1(raw); actualSHA256 != descriptor.SHA256 {
		return nil, fmt.Errorf("git tree descriptor SHA-256 drift object=%s actual=%q want=%q", objectID, actualSHA256, descriptor.SHA256)
	}
	entries, err := reviewFreezeParseCompileGitTreeObjectV1(objectID, raw)
	if err != nil {
		return nil, err
	}
	if len(entries) > verifier.budgets.MaxEntries-verifier.entryCount {
		return nil, fmt.Errorf("git tree total entry budget object=%s actual=%d consumed=%d limit=%d", objectID, len(entries), verifier.entryCount, verifier.budgets.MaxEntries)
	}
	verifier.objectCount++
	verifier.totalBytes += int64(len(raw))
	verifier.entryCount += len(entries)
	verifier.consumed[objectID] = struct{}{}
	verifier.cache[objectID] = append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...)
	return append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...), nil
}

// reviewFreezeReadCompileGitObjectBoundedV1 有界读取一个 CAS object。context 取消时
// 主动 Close 来打断 Read，防止 loader 阻塞导致验证 goroutine 泄漏。
func reviewFreezeReadCompileGitObjectBoundedV1(ctx context.Context, reader io.ReadCloser, objectID string, limit int64) ([]byte, error) {
	type readResult struct {
		raw []byte
		err error
	}
	result := make(chan readResult, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(reader, limit))
		result <- readResult{raw: raw, err: err}
	}()
	select {
	case <-ctx.Done():
		if closeErr := reader.Close(); closeErr != nil {
			return nil, fmt.Errorf("git tree context during read object=%s: %w; close=%v", objectID, ctx.Err(), closeErr)
		}
		return nil, fmt.Errorf("git tree context during read object=%s: %w", objectID, ctx.Err())
	case read := <-result:
		closeErr := reader.Close()
		if read.err != nil {
			return nil, fmt.Errorf("git tree read object=%s: %w", objectID, read.err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("git tree close object=%s: %w", objectID, closeErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("git tree context after read object=%s: %w", objectID, err)
		}
		return read.raw, nil
	}
}

// reviewFreezeParseCompileGitTreeObjectV1 严格解析 canonical
// `tree <decimal-size>\x00<payload>`，先验证 requested SHA-1 再解释 payload。
func reviewFreezeParseCompileGitTreeObjectV1(objectID string, raw []byte) ([]reviewFreezeCompileGitTreeEntryV1, error) {
	digest := sha1.Sum(raw)
	actualID := hex.EncodeToString(digest[:])
	if actualID != objectID {
		return nil, fmt.Errorf("git tree object SHA-1 drift actual=%q want=%q", actualID, objectID)
	}
	nul := bytes.IndexByte(raw, 0)
	if nul <= 0 {
		return nil, fmt.Errorf("git tree object framing 缺 NUL")
	}
	header := string(raw[:nul])
	parts := strings.Split(header, " ")
	if len(parts) != 2 || parts[0] != "tree" {
		return nil, fmt.Errorf("git tree object type/header 非法=%q", header)
	}
	if parts[1] == "" || (len(parts[1]) > 1 && parts[1][0] == '0') {
		return nil, fmt.Errorf("git tree object size framing 非 canonical=%q", parts[1])
	}
	for _, character := range parts[1] {
		if character < '0' || character > '9' {
			return nil, fmt.Errorf("git tree object size framing 非法=%q", parts[1])
		}
	}
	declared, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || declared < 0 {
		return nil, fmt.Errorf("git tree object size framing 非法=%q", parts[1])
	}
	payload := raw[nul+1:]
	if int64(len(payload)) != declared {
		return nil, fmt.Errorf("git tree object declared size=%d actual=%d", declared, len(payload))
	}
	return reviewFreezeParseCompileGitTreePayloadV1(payload)
}

// reviewFreezeParseCompileGitTreePayloadV1 解析 Git binary tree entry：
// `<mode> <name>\x00<20-byte-object-id>`。Git path 是 raw bytes，不要求 UTF-8，
// 也不把反斜杠、控制字符、dot entry 或大小写并存误判为 object framing 非法。
func reviewFreezeParseCompileGitTreePayloadV1(payload []byte) ([]reviewFreezeCompileGitTreeEntryV1, error) {
	return reviewFreezeParseCompileGitTreePayloadBoundedV1(payload, int(^uint(0)>>1))
}

// reviewFreezeParseCompileGitTreePayloadBoundedV1 在解析第 maxEntries+1 个 entry 的任何
// append/allocation 前失败，避免“先完整 materialize 再检查”让 entry 预算失去防线意义。
func reviewFreezeParseCompileGitTreePayloadBoundedV1(payload []byte, maxEntries int) ([]reviewFreezeCompileGitTreeEntryV1, error) {
	if maxEntries < 0 {
		return nil, fmt.Errorf("git tree parser max entries 非法=%d", maxEntries)
	}
	entries := make([]reviewFreezeCompileGitTreeEntryV1, 0)
	seen := make(map[string]struct{})
	for offset := 0; offset < len(payload); {
		if len(entries) >= maxEntries {
			return nil, fmt.Errorf("%w limit=%d", errReviewFreezeCompileGitTreeEntryBudgetV1, maxEntries)
		}
		spaceRelative := bytes.IndexByte(payload[offset:], ' ')
		if spaceRelative <= 0 {
			return nil, fmt.Errorf("git tree entry mode/name 分隔非法 offset=%d", offset)
		}
		space := offset + spaceRelative
		mode := string(payload[offset:space])
		if !reviewFreezeCompileGitTreeKnownModeV1(mode) {
			return nil, fmt.Errorf("git tree entry mode 非 canonical=%q", mode)
		}
		nulRelative := bytes.IndexByte(payload[space+1:], 0)
		if nulRelative < 0 {
			return nil, fmt.Errorf("git tree entry name 缺 NUL offset=%d", offset)
		}
		nul := space + 1 + nulRelative
		nameRaw := payload[space+1 : nul]
		if err := reviewFreezeValidateCompileGitTreeNameV1(nameRaw); err != nil {
			return nil, err
		}
		objectStart := nul + 1
		objectEnd := objectStart + sha1.Size
		if objectEnd > len(payload) {
			return nil, fmt.Errorf("git tree entry object id truncated name=%q", string(nameRaw))
		}
		entry := reviewFreezeCompileGitTreeEntryV1{
			Mode:     mode,
			Name:     string(nameRaw),
			ObjectID: hex.EncodeToString(payload[objectStart:objectEnd]),
		}
		if entry.ObjectID == strings.Repeat("0", 40) {
			return nil, fmt.Errorf("git tree entry zero object id name=%q", entry.Name)
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return nil, fmt.Errorf("git tree duplicate entry name=%q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		if len(entries) > 0 && reviewFreezeCompareCompileGitTreeEntriesV1(entries[len(entries)-1], entry) >= 0 {
			return nil, fmt.Errorf("git tree entries 未按 canonical Git 顺序 previous=%q current=%q", entries[len(entries)-1].Name, entry.Name)
		}
		entries = append(entries, entry)
		offset = objectEnd
	}
	return entries, nil
}

func reviewFreezeCompileGitTreeKnownModeV1(mode string) bool {
	switch mode {
	case reviewFreezeCompileGitTreeRegularBlobModeV1,
		reviewFreezeCompileGitTreeExecutableBlobModeV1,
		reviewFreezeCompileGitTreeSymlinkBlobModeV1,
		reviewFreezeCompileGitTreeDirectoryModeV1,
		reviewFreezeCompileGitTreeSubmoduleCommitModeV1:
		return true
	default:
		return false
	}
}

// reviewFreezeValidateCompileGitTreeNameV1 只执行 raw Git tree name 的必要边界：
// entry name 非空且不能含 `/`。NUL 已作为结构分隔符消费；其他任意 bytes 均保留。
func reviewFreezeValidateCompileGitTreeNameV1(raw []byte) error {
	if len(raw) == 0 || bytes.IndexByte(raw, '/') >= 0 {
		return fmt.Errorf("git tree raw entry name 非法=%q", string(raw))
	}
	return nil
}

// reviewFreezeCompareCompileGitTreeEntriesV1 实现 Git base_name_compare：directory 的
// name 以 `/` 终止，其他 mode 以 NUL 终止，不能退化为普通 string sort。
func reviewFreezeCompareCompileGitTreeEntriesV1(left, right reviewFreezeCompileGitTreeEntryV1) int {
	leftKey := append([]byte(left.Name), 0)
	if left.Mode == reviewFreezeCompileGitTreeDirectoryModeV1 {
		leftKey[len(leftKey)-1] = '/'
	}
	rightKey := append([]byte(right.Name), 0)
	if right.Mode == reviewFreezeCompileGitTreeDirectoryModeV1 {
		rightKey[len(rightKey)-1] = '/'
	}
	return bytes.Compare(leftKey, rightKey)
}

func reviewFreezeCompileGitJoinPathV1(prefix, segment string) string {
	if prefix == "" {
		return segment
	}
	return prefix + "/" + segment
}

// reviewFreezeCompileGitTreeFrameV1 生成 canonical raw Git tree object 及其 SHA-1。
func reviewFreezeCompileGitTreeFrameV1(payload []byte) (string, []byte) {
	raw := make([]byte, 0, len(payload)+32)
	raw = append(raw, "tree "...)
	raw = strconv.AppendInt(raw, int64(len(payload)), 10)
	raw = append(raw, 0)
	raw = append(raw, payload...)
	digest := sha1.Sum(raw)
	return hex.EncodeToString(digest[:]), raw
}

func reviewFreezeCompileGitTreeFramedSizeV1(bodySize int64) int64 {
	return int64(len("tree ")) + int64(len(strconv.FormatInt(bodySize, 10))) + 1 + bodySize
}

// reviewFreezeEncodeCompileGitTreePayloadV1 只用于受控 fixture 重写；调用方决定是否
// 先排序，以便同时构造 canonical 与故意乱序的对抗 object。
func reviewFreezeEncodeCompileGitTreePayloadV1(entries []reviewFreezeCompileGitTreeEntryV1) []byte {
	payload := make([]byte, 0)
	for _, entry := range entries {
		payload = append(payload, entry.Mode...)
		payload = append(payload, ' ')
		payload = append(payload, entry.Name...)
		payload = append(payload, 0)
		objectID, err := hex.DecodeString(entry.ObjectID)
		if err != nil || len(objectID) != sha1.Size {
			panic(fmt.Sprintf("invalid fixture object id=%q", entry.ObjectID))
		}
		payload = append(payload, objectID...)
	}
	return payload
}

// reviewFreezeCompileGitObjectLoaderFixtureV1 是线程安全的内存 CAS；它记录 Open，
// 并可注入 error+Reader、identity drift 或阻塞 Reader 对抗资源边界。
type reviewFreezeCompileGitObjectLoaderFixtureV1 struct {
	mu               sync.Mutex
	objects          map[string][]byte
	descriptors      []reviewFreezeCompileGitObjectDescriptorV1
	listErr          error
	listCalls        int
	openCalls        map[string]int
	openOrder        []string
	openErrors       map[string]error
	errorReaders     map[string]*reviewFreezeCompileGitTrackingReaderV1
	identityOverride map[string]string
	readerOverrides  map[string]io.ReadCloser
}

func reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects map[string][]byte) *reviewFreezeCompileGitObjectLoaderFixtureV1 {
	cloned := make(map[string][]byte, len(objects))
	descriptors := make([]reviewFreezeCompileGitObjectDescriptorV1, 0, len(objects))
	for objectID, raw := range objects {
		cloned[objectID] = append([]byte(nil), raw...)
		descriptors = append(descriptors, reviewFreezeCompileGitDescriptorFixtureV1(objectID, raw))
	}
	sort.Slice(descriptors, func(left, right int) bool { return descriptors[left].ObjectID > descriptors[right].ObjectID })
	return &reviewFreezeCompileGitObjectLoaderFixtureV1{
		objects:          cloned,
		descriptors:      descriptors,
		openCalls:        make(map[string]int),
		openErrors:       make(map[string]error),
		errorReaders:     make(map[string]*reviewFreezeCompileGitTrackingReaderV1),
		identityOverride: make(map[string]string),
		readerOverrides:  make(map[string]io.ReadCloser),
	}
}

func reviewFreezeCompileGitDescriptorFixtureV1(objectID string, raw []byte) reviewFreezeCompileGitObjectDescriptorV1 {
	kind := "tree"
	bodySize := int64(0)
	if nul := bytes.IndexByte(raw, 0); nul >= 0 {
		header := strings.Split(string(raw[:nul]), " ")
		if len(header) > 0 && header[0] != "" {
			kind = header[0]
		}
		if len(header) == 2 {
			if parsed, err := strconv.ParseInt(header[1], 10, 64); err == nil {
				bodySize = parsed
			} else {
				bodySize = int64(len(raw) - nul - 1)
			}
		}
	}
	return reviewFreezeCompileGitObjectDescriptorV1{
		ObjectID:         objectID,
		Kind:             kind,
		DeclaredBodySize: bodySize,
		SHA256:           reviewFreezeSHA256V1(raw),
	}
}

func (loader *reviewFreezeCompileGitObjectLoaderFixtureV1) List(ctx context.Context) ([]reviewFreezeCompileGitObjectDescriptorV1, error) {
	loader.mu.Lock()
	loader.listCalls++
	listed := append([]reviewFreezeCompileGitObjectDescriptorV1(nil), loader.descriptors...)
	listErr := loader.listErr
	loader.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if listErr != nil {
		return nil, listErr
	}
	return listed, nil
}

func (loader *reviewFreezeCompileGitObjectLoaderFixtureV1) Open(ctx context.Context, objectID string) (reviewFreezeCompileGitObjectOpenedV1, error) {
	loader.mu.Lock()
	loader.openCalls[objectID]++
	loader.openOrder = append(loader.openOrder, objectID)
	openErr := loader.openErrors[objectID]
	errorReader := loader.errorReaders[objectID]
	identity := objectID
	if override, exists := loader.identityOverride[objectID]; exists {
		identity = override
	}
	readerOverride := loader.readerOverrides[objectID]
	raw, exists := loader.objects[objectID]
	cloned := append([]byte(nil), raw...)
	loader.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileGitObjectOpenedV1{}, err
	}
	if openErr != nil {
		return reviewFreezeCompileGitObjectOpenedV1{ObjectID: identity, Reader: errorReader}, openErr
	}
	if readerOverride != nil {
		return reviewFreezeCompileGitObjectOpenedV1{ObjectID: identity, Reader: readerOverride}, nil
	}
	if !exists {
		return reviewFreezeCompileGitObjectOpenedV1{ObjectID: identity}, nil
	}
	return reviewFreezeCompileGitObjectOpenedV1{ObjectID: identity, Reader: io.NopCloser(bytes.NewReader(cloned))}, nil
}

func (loader *reviewFreezeCompileGitObjectLoaderFixtureV1) totalOpenCalls() int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	total := 0
	for _, calls := range loader.openCalls {
		total += calls
	}
	return total
}

func (loader *reviewFreezeCompileGitObjectLoaderFixtureV1) openedObjectOrder() []string {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	return append([]string(nil), loader.openOrder...)
}

type reviewFreezeCompileGitTrackingReaderV1 struct {
	io.Reader
	closed bool
}

func (reader *reviewFreezeCompileGitTrackingReaderV1) Close() error {
	reader.closed = true
	return nil
}

type reviewFreezeCompileGitBlockingReaderV1 struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func reviewFreezeCompileGitBlockingReaderNewV1() *reviewFreezeCompileGitBlockingReaderV1 {
	return &reviewFreezeCompileGitBlockingReaderV1{started: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *reviewFreezeCompileGitBlockingReaderV1) Read([]byte) (int, error) {
	reader.startOnce.Do(func() { close(reader.started) })
	<-reader.closed
	return 0, io.EOF
}

func (reader *reviewFreezeCompileGitBlockingReaderV1) Close() error {
	reader.closeOnce.Do(func() { close(reader.closed) })
	return nil
}

// reviewFreezeCompileGitBaseTreeFixtureV1 绑定一份 strict snapshot、verified leaves、
// statement BaseTree/BaseCommit 以及只含目标沿线 raw tree object 的内存 CAS。
type reviewFreezeCompileGitBaseTreeFixtureV1 struct {
	SnapshotRaw []byte
	Statement   reviewFreezeValidatorCompileAttestationV1
	Leaves      *reviewFreezeVerifiedCompileRepositoryLeafBundleV1
	Objects     map[string][]byte
}

// reviewFreezeCompileGitDirectRawBundleFixtureV1 先让共享 resolver 完成唯一一次外部
// List/Open/OID/digest/body 预算，再把冻结 bundle 交给 direct semantic 测试。
func reviewFreezeCompileGitDirectRawBundleFixtureV1(
	t *testing.T,
	fixture reviewFreezeCompileGitBaseTreeFixtureV1,
	extraTreeBodies ...[]byte,
) (*reviewFreezeCompileGitRawObjectBundleV1, *reviewFreezeCompileGitRawLoaderFixtureV1) {
	t.Helper()
	objects := []reviewFreezeCompileGitRawFixtureObjectV1{{
		Kind: "commit",
		Body: reviewFreezeCompileCommitPayloadV1(fixture.Statement.Subject.BaseTreeSHA),
	}}
	for _, frame := range fixture.Objects {
		nul := bytes.IndexByte(frame, 0)
		if nul <= 0 || !bytes.HasPrefix(frame[:nul], []byte("tree ")) {
			t.Fatalf("direct raw fixture tree frame 非法 header=%q", frame[:max(nul, 0)])
		}
		objects = append(objects, reviewFreezeCompileGitRawFixtureObjectV1{
			Kind: "tree",
			Body: append([]byte(nil), frame[nul+1:]...),
		})
	}
	for _, body := range extraTreeBodies {
		objects = append(objects, reviewFreezeCompileGitRawFixtureObjectV1{Kind: "tree", Body: append([]byte(nil), body...)})
	}
	loader := reviewFreezeCompileGitRawLoaderFixtureNewV1(objects)
	bundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err != nil {
		t.Fatalf("resolve direct raw bundle fixture: %v", err)
	}
	return bundle, loader
}

// reviewFreezeCompileGitDirectCloneBundleWithBodyV1 只用于 active-stack 对抗：它克隆
// resolver 产物并替换一个 frozen body，同时保持对象 ID 不变，以隔离验证 semantic
// traversal 即使面对被破坏的内部不变量也不会因 cache 掩盖 recursion cycle。
func reviewFreezeCompileGitDirectCloneBundleWithBodyV1(
	t *testing.T,
	bundle *reviewFreezeCompileGitRawObjectBundleV1,
	objectID string,
	body []byte,
) *reviewFreezeCompileGitRawObjectBundleV1 {
	t.Helper()
	cloned := &reviewFreezeCompileGitRawObjectBundleV1{
		descriptors: bundle.Descriptors(),
		objects:     make(map[string]reviewFreezeCompileGitRawFrozenObjectV1, len(bundle.ObjectIDs())),
	}
	for _, descriptor := range cloned.descriptors {
		originalBody, exists := bundle.BodyBytes(descriptor.ObjectID)
		if !exists {
			t.Fatalf("clone direct raw bundle body missing=%s", descriptor.ObjectID)
		}
		if descriptor.ObjectID == objectID {
			originalBody = append([]byte(nil), body...)
			descriptor.BodySizeBytes = int64(len(originalBody))
			descriptor.BodySHA256 = reviewFreezeSHA256V1(originalBody)
		}
		cloned.objects[descriptor.ObjectID] = reviewFreezeCompileGitRawFrozenObjectV1{
			descriptor: descriptor,
			body:       string(originalBody),
		}
		for index := range cloned.descriptors {
			if cloned.descriptors[index].ObjectID == descriptor.ObjectID {
				cloned.descriptors[index] = descriptor
				break
			}
		}
	}
	return cloned
}

func reviewFreezeCompileGitDirectDefaultBudgetsV1() reviewFreezeCompileGitDirectTreeBudgetsV1 {
	return reviewFreezeCompileGitDirectTreeBudgetsV1{
		MaxTreeBodyBytes:  reviewFreezeCompileGitDirectMaxTreeBodyBytesV1,
		MaxTotalBodyBytes: reviewFreezeCompileGitDirectMaxTotalBodyBytesV1,
		MaxObjects:        reviewFreezeCompileGitDirectMaxObjectsV1,
		MaxDepth:          reviewFreezeCompileGitDirectMaxDepthV1,
		MaxEntriesPerTree: reviewFreezeCompileGitDirectMaxEntriesPerTreeV1,
		MaxTotalEntries:   reviewFreezeCompileGitDirectMaxTotalEntriesV1,
	}
}

// reviewFreezeCompileGitBindBaseV1 只重绑 statement/snapshot 的 BaseCommit/BaseTree 和
// input snapshot content refs，不把 commit 与 tree 的关系冒充为已证明。
func reviewFreezeCompileGitBindBaseV1(
	t *testing.T,
	fixture reviewFreezeCompileRepositoryLeafFixtureV1,
	baseCommitSHA string,
	baseTreeSHA string,
) ([]byte, reviewFreezeValidatorCompileAttestationV1, *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) {
	t.Helper()
	statement := fixture.Statement
	statement.Subject.BaseCommitSHA = baseCommitSHA
	statement.Subject.BaseTreeSHA = baseTreeSHA
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, fixture.SnapshotRaw)
	snapshot.Subject = statement.Subject
	raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
	leaves, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(
		context.Background(), raw, statement, reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture),
	)
	if err != nil {
		t.Fatalf("bind base verified repository leaves: %v", err)
	}
	return raw, statement, leaves
}

// reviewFreezeCompileGitCommandV1 仅供 fixture 调用本机 Git 构造/读取对象；verifier
// 的依赖闭包中不存在 os/exec 或 workspace path。
func reviewFreezeCompileGitCommandV1(t *testing.T, root string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture %s: %v: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(raw)))
	}
	return raw
}

func reviewFreezeCompileGitRevParseV1(t *testing.T, root, revision string) string {
	t.Helper()
	return strings.TrimSpace(string(reviewFreezeCompileGitCommandV1(t, root, "rev-parse", "--verify", revision)))
}

// reviewFreezeCompileGitCollectTargetTreesV1 通过 git cat-file 仅在 fixture 阶段取得
// root 与目标 ancestor tree payload，并重构 canonical raw framed object 放入内存 CAS。
func reviewFreezeCompileGitCollectTargetTreesV1(t *testing.T, root, rootTreeID string, targetPaths []string) map[string][]byte {
	t.Helper()
	targets := &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
	for _, path := range targetPaths {
		node := targets
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			child := node.children[segment]
			if child == nil {
				child = &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
				node.children[segment] = child
			}
			node = child
			if index == len(segments)-1 {
				node.leaf = &reviewFreezeCompileInputSnapshotRepoFileV1{Path: path}
			}
		}
	}
	objects := make(map[string][]byte)
	var collect func(string, *reviewFreezeCompileGitTargetNodeV1, string)
	collect = func(treeID string, node *reviewFreezeCompileGitTargetNodeV1, prefix string) {
		if _, exists := objects[treeID]; exists {
			return
		}
		payload := reviewFreezeCompileGitCommandV1(t, root, "cat-file", "tree", treeID)
		actualID, framed := reviewFreezeCompileGitTreeFrameV1(payload)
		if actualID != treeID {
			t.Fatalf("git cat-file tree framing id=%s want=%s prefix=%q", actualID, treeID, prefix)
		}
		objects[treeID] = framed
		entries, err := reviewFreezeParseCompileGitTreePayloadV1(payload)
		if err != nil {
			t.Fatalf("parse git fixture tree prefix=%q: %v", prefix, err)
		}
		byName := make(map[string]reviewFreezeCompileGitTreeEntryV1, len(entries))
		for _, entry := range entries {
			byName[entry.Name] = entry
		}
		for name, child := range node.children {
			if child.leaf != nil {
				continue
			}
			entry, exists := byName[name]
			if !exists || entry.Mode != reviewFreezeCompileGitTreeDirectoryModeV1 {
				t.Fatalf("git fixture target ancestor invalid path=%q entry=%+v", reviewFreezeCompileGitJoinPathV1(prefix, name), entry)
			}
			collect(entry.ObjectID, child, reviewFreezeCompileGitJoinPathV1(prefix, name))
		}
	}
	collect(rootTreeID, targets, "")
	return objects
}

// reviewFreezeCompileGitControlledFixtureV1 用真实临时 Git index/commit/write-tree
// 构造只含 14 个 leaf 的受控 object graph，再以 cat-file 转为内存 raw CAS。
func reviewFreezeCompileGitControlledFixtureV1(t *testing.T) reviewFreezeCompileGitBaseTreeFixtureV1 {
	t.Helper()
	repositoryFixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	root := t.TempDir()
	reviewFreezeCompileGitCommandV1(t, root, "init", "--quiet")
	reviewFreezeCompileGitCommandV1(t, root, "config", "user.name", "Dora Review Freeze Fixture")
	reviewFreezeCompileGitCommandV1(t, root, "config", "user.email", "review-freeze@example.invalid")
	reviewFreezeCompileGitCommandV1(t, root, "config", "core.fileMode", "false")
	for path, raw := range repositoryFixture.Materials {
		hostPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
			t.Fatalf("mkdir controlled git fixture path=%q: %v", path, err)
		}
		if err := os.WriteFile(hostPath, raw, 0o644); err != nil {
			t.Fatalf("write controlled git fixture path=%q: %v", path, err)
		}
	}
	reviewFreezeCompileGitCommandV1(t, root, "add", "--all")
	commit := exec.Command("git", "-C", root, "commit", "--quiet", "--no-gpg-sign", "-m", "controlled fixture")
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
	if raw, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit controlled git fixture: %v: %s", err, strings.TrimSpace(string(raw)))
	}
	commitID := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{commit}")
	treeID := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{tree}")
	paths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	objects := reviewFreezeCompileGitCollectTargetTreesV1(t, root, treeID, paths)
	snapshotRaw, statement, leaves := reviewFreezeCompileGitBindBaseV1(t, repositoryFixture, commitID, treeID)
	return reviewFreezeCompileGitBaseTreeFixtureV1{SnapshotRaw: snapshotRaw, Statement: statement, Leaves: leaves, Objects: objects}
}

// reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1 是 clean-worktree gate fixture：
// tree 来自当前 HEAD，leaf bytes 来自当前 worktree。二者成功匹配只证明目标文件相对
// HEAD 干净及本 verifier 可消费真实 tree，不是脱离 worktree 的纯 HEAD golden。
// verifier 接收的仍只是 strict bytes、verified leaves 与内存 CAS。
func reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1(t *testing.T) reviewFreezeCompileGitBaseTreeFixtureV1 {
	t.Helper()
	repositoryFixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	root := reviewFreezeCompileRepositoryLeafFixtureRootV1(t)
	commitID := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{commit}")
	treeID := reviewFreezeCompileGitRevParseV1(t, root, "HEAD^{tree}")
	objects := reviewFreezeCompileGitCollectTargetTreesV1(t, root, treeID, reviewFreezeCompileInputSnapshotRepositoryPathsV1())
	snapshotRaw, statement, leaves := reviewFreezeCompileGitBindBaseV1(t, repositoryFixture, commitID, treeID)
	return reviewFreezeCompileGitBaseTreeFixtureV1{SnapshotRaw: snapshotRaw, Statement: statement, Leaves: leaves, Objects: objects}
}

// reviewFreezeCompileGitRealHEADFixtureV1 是共享 raw-CAS 测试的兼容入口。
// 其真实语义仍是 HEAD tree + worktree leaf clean gate，不是纯 HEAD fixture；新测试应
// 使用 reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1 表达该边界。
func reviewFreezeCompileGitRealHEADFixtureV1(t *testing.T) reviewFreezeCompileGitBaseTreeFixtureV1 {
	t.Helper()
	return reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1(t)
}

// reviewFreezeCompileGitRewriteTreePathV1 从 root 到指定 tree path 逐层重算 SHA-1，
// 用于构造 mode、missing、blob drift、排序和 case 冲突等受控对抗。
func reviewFreezeCompileGitRewriteTreePathV1(
	t *testing.T,
	objects map[string][]byte,
	rootID string,
	treePath []string,
	mutate func([]reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1,
) (string, map[string][]byte) {
	t.Helper()
	cloned := make(map[string][]byte, len(objects)+len(treePath)+1)
	for objectID, raw := range objects {
		cloned[objectID] = append([]byte(nil), raw...)
	}
	var rewrite func(string, int) string
	rewrite = func(treeID string, depth int) string {
		entries, err := reviewFreezeParseCompileGitTreeObjectV1(treeID, cloned[treeID])
		if err != nil {
			t.Fatalf("parse rewrite tree depth=%d: %v", depth, err)
		}
		if depth == len(treePath) {
			entries = mutate(append([]reviewFreezeCompileGitTreeEntryV1(nil), entries...))
		} else {
			found := false
			for index := range entries {
				if entries[index].Name == treePath[depth] {
					if entries[index].Mode != reviewFreezeCompileGitTreeDirectoryModeV1 {
						t.Fatalf("rewrite ancestor non-tree=%q", treePath[depth])
					}
					entries[index].ObjectID = rewrite(entries[index].ObjectID, depth+1)
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("rewrite tree path missing segment=%q", treePath[depth])
			}
		}
		payload := reviewFreezeEncodeCompileGitTreePayloadV1(entries)
		newID, raw := reviewFreezeCompileGitTreeFrameV1(payload)
		cloned[newID] = raw
		return newID
	}
	newRoot := rewrite(rootID, 0)
	return newRoot, reviewFreezeCompileGitPruneTargetObjectsV1(t, cloned, newRoot)
}

// reviewFreezeCompileGitPruneTargetObjectsV1 让 mutation fixture 的 descriptor List 只含
// 新 root 可达且确实用于 14 个目标路径的 tree，避免把重写前旧对象误当 expected extra。
func reviewFreezeCompileGitPruneTargetObjectsV1(t *testing.T, objects map[string][]byte, rootID string) map[string][]byte {
	t.Helper()
	paths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	targets := &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
	for _, path := range paths {
		node := targets
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			if node.children[segment] == nil {
				node.children[segment] = &reviewFreezeCompileGitTargetNodeV1{children: make(map[string]*reviewFreezeCompileGitTargetNodeV1)}
			}
			node = node.children[segment]
			if index == len(segments)-1 {
				node.leaf = &reviewFreezeCompileInputSnapshotRepoFileV1{Path: path}
			}
		}
	}
	pruned := make(map[string][]byte)
	var visit func(string, *reviewFreezeCompileGitTargetNodeV1)
	visit = func(treeID string, node *reviewFreezeCompileGitTargetNodeV1) {
		if _, seen := pruned[treeID]; seen {
			return
		}
		raw, exists := objects[treeID]
		if !exists {
			t.Fatalf("prune target object missing=%s", treeID)
		}
		pruned[treeID] = append([]byte(nil), raw...)
		entries, err := reviewFreezeParseCompileGitTreeObjectV1(treeID, raw)
		if err != nil {
			// Payload parser adversarial cases intentionally become invalid only inside
			// verifier. They mutate root, so no descendant discovery is needed here.
			if treeID == rootID {
				for objectID, candidate := range objects {
					if objectID != rootID {
						pruned[objectID] = append([]byte(nil), candidate...)
					}
				}
				return
			}
			t.Fatalf("prune parse descendant object=%s: %v", treeID, err)
		}
		byName := make(map[string]reviewFreezeCompileGitTreeEntryV1, len(entries))
		for _, entry := range entries {
			byName[entry.Name] = entry
		}
		for name, child := range node.children {
			if child.leaf != nil {
				continue
			}
			entry, exists := byName[name]
			if exists {
				visit(entry.ObjectID, child)
			}
		}
	}
	visit(rootID, targets)
	return pruned
}

func reviewFreezeCompileGitSortEntriesV1(entries []reviewFreezeCompileGitTreeEntryV1) {
	sort.Slice(entries, func(left, right int) bool {
		return reviewFreezeCompareCompileGitTreeEntriesV1(entries[left], entries[right]) < 0
	})
}

func reviewFreezeCompileGitMutateEntryV1(t *testing.T, entries []reviewFreezeCompileGitTreeEntryV1, name string, mutate func(*reviewFreezeCompileGitTreeEntryV1)) []reviewFreezeCompileGitTreeEntryV1 {
	t.Helper()
	for index := range entries {
		if entries[index].Name == name {
			mutate(&entries[index])
			return entries
		}
	}
	t.Fatalf("fixture tree entry not found=%q", name)
	return nil
}

func reviewFreezeCompileGitRebindRootV1(t *testing.T, fixture reviewFreezeCompileGitBaseTreeFixtureV1, rootID string) reviewFreezeCompileGitBaseTreeFixtureV1 {
	t.Helper()
	statement := fixture.Statement
	statement.Subject.BaseTreeSHA = rootID
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, fixture.SnapshotRaw)
	snapshot.Subject = statement.Subject
	raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
	return reviewFreezeCompileGitBaseTreeFixtureV1{SnapshotRaw: raw, Statement: statement, Leaves: fixture.Leaves, Objects: fixture.Objects}
}

func TestW2ReviewFreezeCompileGitBaseTreeMembershipControlledV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
	if err != nil {
		t.Fatalf("valid controlled Git base tree rejected: %v", err)
	}
	if verified.BaseTreeSHA() != fixture.Statement.Subject.BaseTreeSHA || !reflect.DeepEqual(verified.Paths(), reviewFreezeCompileInputSnapshotRepositoryPathsV1()) {
		t.Fatalf("verified base tree result drift sha=%q paths=%v", verified.BaseTreeSHA(), verified.Paths())
	}
	if verified.ObjectCount() <= 1 || verified.ObjectCount() != loader.totalOpenCalls() || verified.TotalBytes() <= 0 || verified.MaxDepth() <= 0 {
		t.Fatalf("verified object counters invalid objects/open=%d/%d bytes=%d depth=%d", verified.ObjectCount(), loader.totalOpenCalls(), verified.TotalBytes(), verified.MaxDepth())
	}
	wantObjectIDs := make([]string, 0, len(fixture.Objects))
	for objectID := range fixture.Objects {
		wantObjectIDs = append(wantObjectIDs, objectID)
	}
	sort.Strings(wantObjectIDs)
	if !reflect.DeepEqual(verified.ObjectIDs(), wantObjectIDs) {
		t.Fatalf("verified object IDs=%v want=%v", verified.ObjectIDs(), wantObjectIDs)
	}
	objectIDs := verified.ObjectIDs()
	objectIDs[0] = strings.Repeat("f", 40)
	if !reflect.DeepEqual(verified.ObjectIDs(), wantObjectIDs) {
		t.Fatal("base tree result ObjectIDs 不是 immutable copy")
	}
	for objectID, calls := range loader.openCalls {
		if calls != 1 {
			t.Fatalf("CAS object=%s Open calls=%d want=1", objectID, calls)
		}
	}
	scope := verified.Scope()
	wantGaps := []string{reviewFreezeCompileGitBaseCommitGapV1, reviewFreezeCompileGitCommitAncestryGapV1, reviewFreezeCompileGitGitHubAuthorityGapV1}
	if scope.VerifiedClaim != reviewFreezeCompileGitBaseTreeClaimV1 || !scope.BaseTreeMembership || scope.BaseCommitBinding || scope.CommitAncestry || scope.GitHubAuthority || scope.FormalFreezeStatus != reviewFreezeCompileGitFormalFreezeNotProvenV1 || !reflect.DeepEqual(scope.OpenGaps, wantGaps) {
		t.Fatalf("base tree scope overclaim/drift=%+v", scope)
	}
	scope.OpenGaps[0] = "forged"
	if !reflect.DeepEqual(verified.Scope().OpenGaps, wantGaps) {
		t.Fatal("base tree scope OpenGaps 不是 immutable copy")
	}
	paths := verified.Paths()
	paths[0] = "forged"
	if !reflect.DeepEqual(verified.Paths(), reviewFreezeCompileInputSnapshotRepositoryPathsV1()) {
		t.Fatal("base tree result Paths 不是 immutable copy")
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDeterministicOpenOrderV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	orders := make([][]string, 2)
	for index := range orders {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		if _, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader); err != nil {
			t.Fatalf("deterministic traversal run=%d rejected: %v", index, err)
		}
		orders[index] = loader.openedObjectOrder()
	}
	if len(orders[0]) == 0 || !reflect.DeepEqual(orders[0], orders[1]) {
		t.Fatalf("CAS Open order 不确定 first=%v second=%v", orders[0], orders[1])
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDirectRawBundleV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	bundle, rawLoader := reviewFreezeCompileGitDirectRawBundleFixtureV1(t, fixture)
	openSnapshot := rawLoader.openCallSnapshot()
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1(
		context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, bundle,
	)
	if err != nil {
		t.Fatalf("direct raw-bundle base tree rejected: %v", err)
	}
	if !reviewFreezeCompileGitRawEqualStringIntMapV1(openSnapshot, rawLoader.openCallSnapshot()) {
		t.Fatal("direct tree semantic 回访了 raw resolver 外部 CAS")
	}
	wantObjectIDs := make([]string, 0, len(fixture.Objects))
	wantFramedBytes := int64(0)
	for objectID, frame := range fixture.Objects {
		wantObjectIDs = append(wantObjectIDs, objectID)
		wantFramedBytes += int64(len(frame))
	}
	sort.Strings(wantObjectIDs)
	if verified.BaseTreeSHA() != fixture.Statement.Subject.BaseTreeSHA ||
		!reflect.DeepEqual(verified.Paths(), reviewFreezeCompileInputSnapshotRepositoryPathsV1()) ||
		!reflect.DeepEqual(verified.ObjectIDs(), wantObjectIDs) ||
		verified.TotalBytes() != wantFramedBytes || verified.ObjectCount() != len(wantObjectIDs) {
		t.Fatalf("direct tree result drift sha=%s paths=%v objects=%v/%d bytes=%d wantObjects=%v bytes=%d", verified.BaseTreeSHA(), verified.Paths(), verified.ObjectIDs(), verified.ObjectCount(), verified.TotalBytes(), wantObjectIDs, wantFramedBytes)
	}
	scope := verified.Scope()
	if !scope.BaseTreeMembership || scope.BaseCommitBinding || scope.CommitAncestry || scope.GitHubAuthority || scope.FormalFreezeStatus != reviewFreezeCompileGitFormalFreezeNotProvenV1 {
		t.Fatalf("direct tree scope overclaim=%+v", scope)
	}

	// bundle/result accessor 都返回副本；修改调用方视图不会污染已形成的语义结论。
	body, exists := bundle.BodyBytes(fixture.Statement.Subject.BaseTreeSHA)
	if !exists || len(body) == 0 {
		t.Fatal("direct root body fixture missing")
	}
	body[0] ^= 0xff
	paths := verified.Paths()
	paths[0] = "forged"
	objectIDs := verified.ObjectIDs()
	objectIDs[0] = strings.Repeat("f", 40)
	scope.OpenGaps[0] = "forged"
	if !reflect.DeepEqual(verified.Paths(), reviewFreezeCompileInputSnapshotRepositoryPathsV1()) ||
		!reflect.DeepEqual(verified.ObjectIDs(), wantObjectIDs) ||
		verified.Scope().OpenGaps[0] == "forged" {
		t.Fatal("direct tree result/bundle 不是 immutable projection")
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDirectAllowsExtraBundleObjectV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	extraBody := []byte{}
	extraFrame := reviewFreezeCompileGitRawCanonicalFrameV1("tree", extraBody)
	extraDigest := sha1.Sum(extraFrame)
	extraObjectID := hex.EncodeToString(extraDigest[:])
	bundle, _ := reviewFreezeCompileGitDirectRawBundleFixtureV1(t, fixture, extraBody)
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1(
		context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, bundle,
	)
	if err != nil {
		t.Fatalf("direct tree 不应接管 union extra-object policy: %v", err)
	}
	usedObjectIDs := verified.ObjectIDs()
	usedIndex := sort.SearchStrings(usedObjectIDs, extraObjectID)
	if usedIndex < len(usedObjectIDs) && usedObjectIDs[usedIndex] == extraObjectID {
		t.Fatalf("extra bundle tree 被错误标记为 used=%s", extraObjectID)
	}
	if _, listed := bundle.Descriptor(extraObjectID); !listed {
		t.Fatalf("extra bundle fixture 未包含 object=%s", extraObjectID)
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDirectBudgetsV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	bundle, _ := reviewFreezeCompileGitDirectRawBundleFixtureV1(t, fixture)
	rootBody, exists := bundle.BodyBytes(fixture.Statement.Subject.BaseTreeSHA)
	if !exists || len(rootBody) < 2 {
		t.Fatal("direct budget root body fixture missing")
	}
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileGitDirectTreeBudgetsV1)
		want   string
	}{
		{name: "per tree body", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxTreeBodyBytes = int64(len(rootBody) - 1) }, want: "tree body budget"},
		{name: "total body", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxTotalBodyBytes = int64(len(rootBody)) }, want: "total body budget"},
		{name: "object count", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxObjects = 1 }, want: "object count budget"},
		{name: "depth", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxDepth = 0 }, want: "depth budget"},
		{name: "per tree entries allocation before", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxEntriesPerTree = 1 }, want: "per-tree entry budget"},
		{name: "total entries allocation before", mutate: func(b *reviewFreezeCompileGitDirectTreeBudgetsV1) { b.MaxTotalEntries = 2 }, want: "total entry budget"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			budgets := reviewFreezeCompileGitDirectDefaultBudgetsV1()
			test.mutate(&budgets)
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleWithBudgetsV1(
				context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, bundle, budgets,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("direct budget error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDirectRejectsActiveStackCycleV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	bundle, _ := reviewFreezeCompileGitDirectRawBundleFixtureV1(t, fixture)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	rootBody, exists := bundle.BodyBytes(rootID)
	if !exists {
		t.Fatal("cycle root body fixture missing")
	}
	entries, err := reviewFreezeParseCompileGitTreePayloadV1(rootBody)
	if err != nil {
		t.Fatalf("parse cycle root fixture: %v", err)
	}
	entries = reviewFreezeCompileGitMutateEntryV1(t, entries, "agent", func(entry *reviewFreezeCompileGitTreeEntryV1) {
		entry.ObjectID = rootID
	})
	cyclicBundle := reviewFreezeCompileGitDirectCloneBundleWithBodyV1(t, bundle, rootID, reviewFreezeEncodeCompileGitTreePayloadV1(entries))
	_, err = reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1(
		context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, cyclicBundle,
	)
	if err == nil || !strings.Contains(err.Error(), "active recursion cycle") {
		t.Fatalf("direct active-stack cycle error=%v", err)
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeDirectAllowsDAGCacheReuseV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	rootEntries, err := reviewFreezeParseCompileGitTreeObjectV1(rootID, fixture.Objects[rootID])
	if err != nil {
		t.Fatalf("parse DAG root fixture: %v", err)
	}
	entryByName := make(map[string]reviewFreezeCompileGitTreeEntryV1, len(rootEntries))
	for _, entry := range rootEntries {
		entryByName[entry.Name] = entry
	}
	agentEntry, agentExists := entryByName["agent"]
	docsEntry, docsExists := entryByName["docs"]
	if !agentExists || !docsExists {
		t.Fatalf("DAG root fixture missing agent/docs=%v", rootEntries)
	}
	agentEntries, err := reviewFreezeParseCompileGitTreeObjectV1(agentEntry.ObjectID, fixture.Objects[agentEntry.ObjectID])
	if err != nil {
		t.Fatalf("parse DAG agent fixture: %v", err)
	}
	docsEntries, err := reviewFreezeParseCompileGitTreeObjectV1(docsEntry.ObjectID, fixture.Objects[docsEntry.ObjectID])
	if err != nil {
		t.Fatalf("parse DAG docs fixture: %v", err)
	}
	sharedEntries := append(append([]reviewFreezeCompileGitTreeEntryV1(nil), agentEntries...), docsEntries...)
	reviewFreezeCompileGitSortEntriesV1(sharedEntries)
	sharedID, sharedFrame := reviewFreezeCompileGitTreeFrameV1(reviewFreezeEncodeCompileGitTreePayloadV1(sharedEntries))
	for index := range rootEntries {
		if rootEntries[index].Name == "agent" || rootEntries[index].Name == "docs" {
			rootEntries[index].ObjectID = sharedID
		}
	}
	newRootID, newRootFrame := reviewFreezeCompileGitTreeFrameV1(reviewFreezeEncodeCompileGitTreePayloadV1(rootEntries))
	objects := make(map[string][]byte, len(fixture.Objects)+2)
	for objectID, frame := range fixture.Objects {
		objects[objectID] = append([]byte(nil), frame...)
	}
	objects[sharedID] = sharedFrame
	objects[newRootID] = newRootFrame
	dagFixture := reviewFreezeCompileGitRebindRootV1(t, fixture, newRootID)
	dagFixture.Objects = objects
	bundle, _ := reviewFreezeCompileGitDirectRawBundleFixtureV1(t, dagFixture)
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleV1(
		context.Background(), dagFixture.SnapshotRaw, dagFixture.Statement, dagFixture.Leaves, bundle,
	)
	if err != nil {
		t.Fatalf("valid direct DAG rejected: %v", err)
	}
	sharedUses := 0
	uniqueBodyBytes := int64(0)
	for _, objectID := range verified.ObjectIDs() {
		if objectID == sharedID {
			sharedUses++
		}
		body, exists := bundle.BodyBytes(objectID)
		if !exists {
			t.Fatalf("DAG used body missing=%s", objectID)
		}
		uniqueBodyBytes += int64(len(body))
	}
	if sharedUses != 1 {
		t.Fatalf("DAG shared tree used IDs count=%d want=1 ids=%v", sharedUses, verified.ObjectIDs())
	}
	budgets := reviewFreezeCompileGitDirectDefaultBudgetsV1()
	budgets.MaxTotalBodyBytes = uniqueBodyBytes
	budgets.MaxObjects = verified.ObjectCount()
	if _, err := reviewFreezeVerifyCompileGitBaseTreeMembershipFromRawBundleWithBudgetsV1(
		context.Background(), dagFixture.SnapshotRaw, dagFixture.Statement, dagFixture.Leaves, bundle, budgets,
	); err != nil {
		t.Fatalf("DAG cache 对 shared tree 重复计 body/object 预算: %v", err)
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeHEADTreeCleanWorktreeGateV1(t *testing.T) {
	fixture := reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1(t)
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(
		context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves,
		reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects),
	)
	if err != nil {
		t.Fatalf("HEAD tree/worktree leaf clean gate rejected: %v", err)
	}
	if verified.BaseTreeSHA() != reviewFreezeCompileGitRevParseV1(t, reviewFreezeCompileRepositoryLeafFixtureRootV1(t), "HEAD^{tree}") {
		t.Fatalf("HEAD tree/worktree leaf clean gate root mismatch=%q", verified.BaseTreeSHA())
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeTargetModeAndMappingAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	targetPath := "agent/go.mod"
	treePath := []string{"agent"}
	tests := []struct {
		name   string
		mutate func([]reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1
		want   string
	}{
		{name: "executable target", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			return reviewFreezeCompileGitMutateEntryV1(t, entries, "go.mod", func(entry *reviewFreezeCompileGitTreeEntryV1) {
				entry.Mode = reviewFreezeCompileGitTreeExecutableBlobModeV1
			})
		}, want: "exact 100644"},
		{name: "symlink target", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			return reviewFreezeCompileGitMutateEntryV1(t, entries, "go.mod", func(entry *reviewFreezeCompileGitTreeEntryV1) {
				entry.Mode = reviewFreezeCompileGitTreeSymlinkBlobModeV1
			})
		}, want: "exact 100644"},
		{name: "submodule target", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			return reviewFreezeCompileGitMutateEntryV1(t, entries, "go.mod", func(entry *reviewFreezeCompileGitTreeEntryV1) {
				entry.Mode = reviewFreezeCompileGitTreeSubmoduleCommitModeV1
			})
		}, want: "exact 100644"},
		{name: "blob identity drift", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			return reviewFreezeCompileGitMutateEntryV1(t, entries, "go.mod", func(entry *reviewFreezeCompileGitTreeEntryV1) { entry.ObjectID = strings.Repeat("f", 40) })
		}, want: "target blob drift"},
		{name: "missing target", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			filtered := make([]reviewFreezeCompileGitTreeEntryV1, 0, len(entries)-1)
			for _, entry := range entries {
				if entry.Name != "go.mod" {
					filtered = append(filtered, entry)
				}
			}
			return filtered
		}, want: "target missing"},
		{name: "case-only target is missing", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries = reviewFreezeCompileGitMutateEntryV1(t, entries, "go.mod", func(entry *reviewFreezeCompileGitTreeEntryV1) { entry.Name = "Go.mod" })
			reviewFreezeCompileGitSortEntriesV1(entries)
			return entries
		}, want: "target missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newRoot, objects := reviewFreezeCompileGitRewriteTreePathV1(t, fixture.Objects, rootID, treePath, test.mutate)
			changed := reviewFreezeCompileGitRebindRootV1(t, fixture, newRoot)
			changed.Objects = objects
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), changed.SnapshotRaw, changed.Statement, changed.Leaves, reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("target=%s error=%v want contains %q", targetPath, err, test.want)
			}
		})
	}

	t.Run("non-tree target ancestor", func(t *testing.T) {
		newRoot, objects := reviewFreezeCompileGitRewriteTreePathV1(t, fixture.Objects, rootID, nil, func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			return reviewFreezeCompileGitMutateEntryV1(t, entries, "agent", func(entry *reviewFreezeCompileGitTreeEntryV1) {
				entry.Mode = reviewFreezeCompileGitTreeSymlinkBlobModeV1
			})
		})
		changed := reviewFreezeCompileGitRebindRootV1(t, fixture, newRoot)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), changed.SnapshotRaw, changed.Statement, changed.Leaves, reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects))
		if err == nil || !strings.Contains(err.Error(), "target ancestor 必须是 tree") {
			t.Fatalf("ancestor mode error=%v", err)
		}
	})
}

func TestW2ReviewFreezeCompileGitBaseTreeUnrelatedRawNamesAndSpecialModesAllowedV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	newRoot, objects := reviewFreezeCompileGitRewriteTreePathV1(t, fixture.Objects, fixture.Statement.Subject.BaseTreeSHA, nil, func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
		entries = append(entries,
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeExecutableBlobModeV1, Name: "unrelated-executable", ObjectID: strings.Repeat("1", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeSymlinkBlobModeV1, Name: "unrelated-symlink", ObjectID: strings.Repeat("2", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeSubmoduleCommitModeV1, Name: "unrelated-submodule", ObjectID: strings.Repeat("3", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: string([]byte{0xff, '-', 'x'}), ObjectID: strings.Repeat("4", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: "control-\n-byte", ObjectID: strings.Repeat("5", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: `raw\backslash`, ObjectID: strings.Repeat("6", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: ".", ObjectID: strings.Repeat("7", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: "..", ObjectID: strings.Repeat("8", 40)},
			reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeDirectoryModeV1, Name: "Agent", ObjectID: strings.Repeat("9", 40)},
		)
		reviewFreezeCompileGitSortEntriesV1(entries)
		return entries
	})
	changed := reviewFreezeCompileGitRebindRootV1(t, fixture, newRoot)
	verified, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), changed.SnapshotRaw, changed.Statement, changed.Leaves, reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects))
	if err != nil {
		t.Fatalf("unrelated raw names/modes should not become target authority: %v", err)
	}
	if len(verified.Paths()) != reviewFreezeCompileRepositoryLeafCountV1 {
		t.Fatalf("unrelated entries polluted verified paths=%v", verified.Paths())
	}
}

func TestW2ReviewFreezeCompileGitBaseTreePayloadAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	tests := []struct {
		name   string
		mutate func([]reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1
		want   string
	}{
		{name: "duplicate", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries = append(entries, entries[0])
			reviewFreezeCompileGitSortEntriesV1(entries)
			return entries
		}, want: "duplicate entry"},
		{name: "slash in raw name", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries = append(entries, reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: "slash/name", ObjectID: strings.Repeat("a", 40)})
			reviewFreezeCompileGitSortEntriesV1(entries)
			return entries
		}, want: "raw entry name 非法"},
		{name: "empty raw name", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries = append(entries, reviewFreezeCompileGitTreeEntryV1{Mode: reviewFreezeCompileGitTreeRegularBlobModeV1, Name: "", ObjectID: strings.Repeat("b", 40)})
			reviewFreezeCompileGitSortEntriesV1(entries)
			return entries
		}, want: "raw entry name 非法"},
		{name: "non canonical mode", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries[0].Mode = "040000"
			return entries
		}, want: "mode 非 canonical"},
		{name: "zero object id", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries[0].ObjectID = strings.Repeat("0", 40)
			return entries
		}, want: "zero object id"},
		{name: "unsorted", mutate: func(entries []reviewFreezeCompileGitTreeEntryV1) []reviewFreezeCompileGitTreeEntryV1 {
			entries[0], entries[1] = entries[1], entries[0]
			return entries
		}, want: "canonical Git 顺序"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newRoot, objects := reviewFreezeCompileGitRewriteTreePathV1(t, fixture.Objects, rootID, nil, test.mutate)
			changed := reviewFreezeCompileGitRebindRootV1(t, fixture, newRoot)
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), changed.SnapshotRaw, changed.Statement, changed.Leaves, reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("payload error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileGitBaseTreeFramingAndLoaderAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	rootRaw := append([]byte(nil), fixture.Objects[rootID]...)
	t.Run("SHA drift", func(t *testing.T) {
		drifted := append([]byte(nil), rootRaw...)
		drifted[len(drifted)-1] ^= 0xff
		objects := map[string][]byte{rootID: drifted}
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "SHA-1 drift") {
			t.Fatalf("SHA drift error=%v", err)
		}
	})

	framingTests := []struct {
		name string
		raw  func() []byte
		want string
	}{
		{name: "wrong type", raw: func() []byte {
			nul := bytes.IndexByte(rootRaw, 0)
			return append(append([]byte("blob "+strconv.Itoa(len(rootRaw)-nul-1)), 0), rootRaw[nul+1:]...)
		}, want: "type/header"},
		{name: "non canonical size", raw: func() []byte {
			nul := bytes.IndexByte(rootRaw, 0)
			return append(append([]byte("tree 0"+strconv.Itoa(len(rootRaw)-nul-1)), 0), rootRaw[nul+1:]...)
		}, want: "framed size drift"},
		{name: "declared size mismatch", raw: func() []byte {
			nul := bytes.IndexByte(rootRaw, 0)
			return append(append([]byte("tree "+strconv.Itoa(len(rootRaw)-nul)), 0), rootRaw[nul+1:]...)
		}, want: "framed size drift"},
	}
	for _, test := range framingTests {
		t.Run(test.name, func(t *testing.T) {
			raw := test.raw()
			digest := sha1.Sum(raw)
			newRoot := hex.EncodeToString(digest[:])
			objects := map[string][]byte{newRoot: raw}
			changed := reviewFreezeCompileGitRebindRootV1(t, fixture, newRoot)
			loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(objects)
			if test.name == "wrong type" {
				loader.descriptors[0].Kind = "tree"
			}
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), changed.SnapshotRaw, changed.Statement, changed.Leaves, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("framing error=%v want contains %q", err, test.want)
			}
		})
	}

	t.Run("missing object", func(t *testing.T) {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		delete(loader.objects, rootID)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "missing reader") {
			t.Fatalf("missing object error=%v", err)
		}
	})

	t.Run("opened identity drift", func(t *testing.T) {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		loader.identityOverride[rootID] = strings.Repeat("f", 40)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "opened identity") {
			t.Fatalf("identity drift error=%v", err)
		}
	})

	t.Run("Open error plus Reader closes", func(t *testing.T) {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		tracking := &reviewFreezeCompileGitTrackingReaderV1{Reader: bytes.NewReader(rootRaw)}
		loader.openErrors[rootID] = errors.New("injected CAS failure")
		loader.errorReaders[rootID] = tracking
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "injected CAS failure") || !tracking.closed {
			t.Fatalf("Open error+Reader error=%v closed=%v", err, tracking.closed)
		}
	})
}

func TestW2ReviewFreezeCompileGitBaseTreeDescriptorListingAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	findRoot := func(t *testing.T, loader *reviewFreezeCompileGitObjectLoaderFixtureV1) int {
		t.Helper()
		for index := range loader.descriptors {
			if loader.descriptors[index].ObjectID == rootID {
				return index
			}
		}
		t.Fatalf("root descriptor not found=%s", rootID)
		return -1
	}
	tests := []struct {
		name      string
		mutate    func(*reviewFreezeCompileGitObjectLoaderFixtureV1)
		want      string
		wantOpens int
	}{
		{name: "List error", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.listErr = errors.New("injected List failure")
		}, want: "injected List failure"},
		{name: "empty List", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors = nil
		}, want: "object count budget"},
		{name: "missing root", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			index := findRoot(t, loader)
			loader.descriptors = append(loader.descriptors[:index], loader.descriptors[index+1:]...)
		}, want: "missing root"},
		{name: "duplicate OID", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors = append(loader.descriptors, loader.descriptors[0])
		}, want: "duplicate OID"},
		{name: "uppercase OID", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors[0].ObjectID = strings.ToUpper(loader.descriptors[0].ObjectID)
		}, want: "lowercase OID"},
		{name: "wrong kind", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors[0].Kind = "blob"
		}, want: "descriptor kind"},
		{name: "negative body size", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors[0].DeclaredBodySize = -1
		}, want: "body size"},
		{name: "bad SHA-256", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors[0].SHA256 = "sha256:bad"
		}, want: "SHA-256 非法"},
		{name: "object bytes budget", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			loader.descriptors[0].DeclaredBodySize = reviewFreezeCompileGitTreeMaxObjectBytesV1
		}, want: "object bytes budget"},
		{name: "object count budget", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			for len(loader.descriptors) <= reviewFreezeCompileGitTreeMaxObjectsV1 {
				index := len(loader.descriptors)
				loader.descriptors = append(loader.descriptors, reviewFreezeCompileGitObjectDescriptorV1{
					ObjectID: fmt.Sprintf("%040x", index+1), Kind: "tree", DeclaredBodySize: 0,
					SHA256: reviewFreezeSHA256V1([]byte(fmt.Sprintf("extra-%d", index))),
				})
			}
		}, want: "object count budget"},
		{name: "descriptor body drift", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			index := findRoot(t, loader)
			loader.descriptors[index].DeclaredBodySize--
		}, want: "framed size drift", wantOpens: 1},
		{name: "descriptor digest drift", mutate: func(loader *reviewFreezeCompileGitObjectLoaderFixtureV1) {
			index := findRoot(t, loader)
			loader.descriptors[index].SHA256 = reviewFreezeSHA256V1([]byte("wrong raw object"))
		}, want: "SHA-256 drift", wantOpens: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
			test.mutate(loader)
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("descriptor error=%v want contains %q", err, test.want)
			}
			if loader.listCalls != 1 || loader.totalOpenCalls() != test.wantOpens {
				t.Fatalf("descriptor List/Open=%d/%d want=1/%d", loader.listCalls, loader.totalOpenCalls(), test.wantOpens)
			}
		})
	}

	t.Run("missing discovered descendant descriptor", func(t *testing.T) {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		removeIndex := -1
		for index, descriptor := range loader.descriptors {
			if descriptor.ObjectID != rootID {
				removeIndex = index
				break
			}
		}
		if removeIndex < 0 {
			t.Fatal("fixture missing descendant descriptor")
		}
		missingID := loader.descriptors[removeIndex].ObjectID
		loader.descriptors = append(loader.descriptors[:removeIndex], loader.descriptors[removeIndex+1:]...)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || (!strings.Contains(err.Error(), "descriptor missing discovered OID") && !strings.Contains(err.Error(), "descriptor exact consumption")) {
			t.Fatalf("missing descendant=%s error=%v", missingID, err)
		}
		if loader.openCalls[missingID] != 0 {
			t.Fatalf("missing descriptor object was opened id=%s calls=%d", missingID, loader.openCalls[missingID])
		}
	})

	t.Run("extra listed object is never opened and fails exact consumption", func(t *testing.T) {
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		extraRawID, extraRaw := reviewFreezeCompileGitTreeFrameV1(nil)
		loader.objects[extraRawID] = extraRaw
		loader.descriptors = append(loader.descriptors, reviewFreezeCompileGitDescriptorFixtureV1(extraRawID, extraRaw))
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "exact consumption") {
			t.Fatalf("extra descriptor error=%v", err)
		}
		if loader.openCalls[extraRawID] != 0 {
			t.Fatalf("extra descriptor object was opened calls=%d", loader.openCalls[extraRawID])
		}
	})
}

func TestW2ReviewFreezeCompileGitBaseTreeBudgetAndPhaseAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	valid, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects))
	if err != nil {
		t.Fatalf("derive valid budget counters: %v", err)
	}
	defaultBudgets := reviewFreezeCompileGitTreeBudgetsV1{MaxObjects: reviewFreezeCompileGitTreeMaxObjectsV1, MaxTotalBytes: reviewFreezeCompileGitTreeMaxTotalBytesV1, MaxObjectBytes: reviewFreezeCompileGitTreeMaxObjectBytesV1, MaxDepth: reviewFreezeCompileGitTreeMaxDepthV1, MaxEntries: reviewFreezeCompileGitTreeMaxEntriesV1}
	tests := []struct {
		name    string
		budgets reviewFreezeCompileGitTreeBudgetsV1
		want    string
	}{
		{name: "object count", budgets: func() reviewFreezeCompileGitTreeBudgetsV1 {
			b := defaultBudgets
			b.MaxObjects = valid.ObjectCount() - 1
			return b
		}(), want: "object count budget"},
		{name: "total bytes", budgets: func() reviewFreezeCompileGitTreeBudgetsV1 {
			b := defaultBudgets
			b.MaxTotalBytes = valid.TotalBytes() - 1
			return b
		}(), want: "total bytes budget"},
		{name: "depth", budgets: func() reviewFreezeCompileGitTreeBudgetsV1 {
			b := defaultBudgets
			b.MaxDepth = valid.MaxDepth() - 1
			return b
		}(), want: "depth budget"},
		{name: "entry count", budgets: func() reviewFreezeCompileGitTreeBudgetsV1 { b := defaultBudgets; b.MaxEntries = 1; return b }(), want: "entry budget"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
			_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipWithBudgetsV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader, test.budgets)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("budget error=%v want contains %q", err, test.want)
			}
		})
	}

	t.Run("invalid strict snapshot before CAS", func(t *testing.T) {
		snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, fixture.SnapshotRaw)
		snapshot.Subject.BaseTreeSHA = strings.Repeat("e", 40)
		raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), raw, fixture.Statement, fixture.Leaves, loader)
		if err == nil || !strings.Contains(err.Error(), "strict inputs") || loader.totalOpenCalls() != 0 {
			t.Fatalf("invalid snapshot error=%v Open=%d", err, loader.totalOpenCalls())
		}
	})

	t.Run("leaf bundle mismatch before CAS", func(t *testing.T) {
		forged := &reviewFreezeVerifiedCompileRepositoryLeafBundleV1{
			paths:      fixture.Leaves.Paths(),
			leaves:     make(map[string]reviewFreezeVerifiedCompileRepositoryLeafV1),
			totalBytes: fixture.Leaves.TotalBytes(),
		}
		for _, path := range fixture.Leaves.Paths() {
			metadata, raw, _ := fixture.Leaves.Leaf(path)
			forged.leaves[path] = reviewFreezeVerifiedCompileRepositoryLeafV1{metadata: metadata, raw: string(raw)}
		}
		first := forged.paths[0]
		leaf := forged.leaves[first]
		leaf.metadata.GitBlobSHA = strings.Repeat("f", 40)
		forged.leaves[first] = leaf
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, forged, loader)
		if err == nil || !strings.Contains(err.Error(), "metadata 未绑定") || loader.totalOpenCalls() != 0 {
			t.Fatalf("forged leaf error=%v Open=%d", err, loader.totalOpenCalls())
		}
	})

	t.Run("pre-canceled context before CAS", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(ctx, fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		if !errors.Is(err, context.Canceled) || loader.totalOpenCalls() != 0 {
			t.Fatalf("pre-canceled error=%v Open=%d", err, loader.totalOpenCalls())
		}
	})
}

func TestW2ReviewFreezeCompileGitBaseTreeBlockingReadCancellationV1(t *testing.T) {
	fixture := reviewFreezeCompileGitControlledFixtureV1(t)
	rootID := fixture.Statement.Subject.BaseTreeSHA
	loader := reviewFreezeCompileGitObjectLoaderFixtureNewV1(fixture.Objects)
	blocking := reviewFreezeCompileGitBlockingReaderNewV1()
	loader.readerOverrides[rootID] = blocking
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(ctx, fixture.SnapshotRaw, fixture.Statement, fixture.Leaves, loader)
		result <- err
	}()
	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking CAS reader did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocking read cancellation error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not actively Close blocking CAS reader")
	}
}
