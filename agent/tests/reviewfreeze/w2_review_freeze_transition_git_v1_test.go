package reviewfreeze_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	reviewFreezeBaseSHAEnvV1             = "W2_REVIEW_FREEZE_BASE_SHA"
	reviewFreezeHeadSHAEnvV1             = "W2_REVIEW_FREEZE_HEAD_SHA"
	reviewFreezeOwnerApprovalDirV1       = "docs/design/agent/approvals/w2-review-freeze-owner-approvals/"
	reviewFreezeExceptionDirV1           = "docs/design/agent/approvals/w2-review-freeze-exceptions/"
	reviewFreezeTransitionWorkflowPathV1 = ".github/workflows/w2-review-freeze-transition.yml"
)

// reviewFreezeGitRepositoryV1 只通过 Git object database 读取 base/head，不 checkout 或执行待评审分支代码。
type reviewFreezeGitRepositoryV1 struct {
	root string
}

// reviewFreezeGitTreeEntryV1 描述 Git tree 中一个不可变路径；治理输入只接受普通非可执行 blob。
type reviewFreezeGitTreeEntryV1 struct {
	mode     string
	object   string
	objectID string
	path     string
}

// TestW2ReviewFreezeTransitionGitV1FromObjects 从受信 base checker 比较两个明确提交；普通 go test 缺 SHA 时只跳过该 CI 入口。
func TestW2ReviewFreezeTransitionGitV1FromObjects(t *testing.T) {
	baseSHA := os.Getenv(reviewFreezeBaseSHAEnvV1)
	headSHA := os.Getenv(reviewFreezeHeadSHAEnvV1)
	if baseSHA == "" && headSHA == "" {
		t.Skip("仅由 base-owned transition job 提供 base/head SHA")
	}
	if baseSHA == "" || headSHA == "" {
		t.Fatalf("%s 与 %s 必须同时提供", reviewFreezeBaseSHAEnvV1, reviewFreezeHeadSHAEnvV1)
	}
	repository := reviewFreezeGitRepositoryV1{root: reviewFreezeRepoRootV1(t)}
	if err := reviewFreezeValidateGitTransitionV1(repository, baseSHA, headSHA); err != nil {
		t.Fatalf("Review Freeze 历史迁移失败关闭: %v", err)
	}
}

// reviewFreezeValidateGitTransitionV1 校验 HEAD shape、逐 Gate 迁移、历史产物保留和正式迁移 diff。
func reviewFreezeValidateGitTransitionV1(repository reviewFreezeGitRepositoryV1, baseSHA, headSHA string) error {
	if err := repository.validateCommit(baseSHA); err != nil {
		return fmt.Errorf("base commit: %w", err)
	}
	if err := repository.validateCommit(headSHA); err != nil {
		return fmt.Errorf("head commit: %w", err)
	}

	headManifest, err := repository.loadManifest(headSHA)
	if err != nil {
		return fmt.Errorf("读取 head manifest: %w", err)
	}
	headLoader := repository.loader(headSHA)
	if err := reviewFreezeValidateManifestV1(headManifest, headLoader); err != nil {
		return fmt.Errorf("head shape: %w", err)
	}

	changedFiles, err := repository.changedFiles(baseSHA, headSHA)
	if err != nil {
		return err
	}
	baseTrustRootExists, err := repository.fileExists(baseSHA, reviewFreezeTransitionWorkflowPathV1)
	if err != nil {
		return fmt.Errorf("检查 base trust root: %w", err)
	}
	if baseTrustRootExists {
		if err := reviewFreezeValidateImmutableTrustRootV1(changedFiles); err != nil {
			return err
		}
	}

	baseExists, err := repository.fileExists(baseSHA, reviewFreezeManifestPathV1)
	if err != nil {
		return fmt.Errorf("检查 base manifest tree entry: %w", err)
	}
	if !baseExists {
		if err := reviewFreezeValidateBootstrapV1(headManifest); err != nil {
			return err
		}
		return reviewFreezeValidateAppendOnlyGovernanceArtifactsV1(repository, baseSHA, headSHA, headManifest)
	}

	baseManifest, err := repository.loadManifest(baseSHA)
	if err != nil {
		return fmt.Errorf("读取 base manifest: %w", err)
	}
	if err := reviewFreezeValidateManifestV1(baseManifest, repository.loader(baseSHA)); err != nil {
		return fmt.Errorf("base shape: %w", err)
	}
	if !baseTrustRootExists {
		// 首次激活 trust root 不能把既有正式 authority 降级伪装成 bootstrap；base/head 都必须为 pre-formal。
		if err := reviewFreezeValidateTrustRootActivationBootstrapV1(baseManifest, headManifest); err != nil {
			return err
		}
	}
	if err := reviewFreezeValidateManifestEnvelopeTransitionV1(baseManifest, headManifest); err != nil {
		return err
	}

	formalTransitions := make([]int, 0, 1)
	for index := range baseManifest.Gates {
		baseGate := baseManifest.Gates[index]
		headGate := headManifest.Gates[index]
		if err := reviewFreezeValidateGateTransitionV1(baseGate, headGate); err != nil {
			return err
		}
		if reviewFreezeIsFormalStatusV1(baseGate.Status) {
			// 已形成的正式 authority 必须仍能从 HEAD tree 按原路径和原 SHA 完整验证，不能依赖 Git 历史中的幽灵文件。
			exception, err := reviewFreezeValidateExceptionV1(baseManifest, baseGate, headLoader)
			if err != nil {
				return fmt.Errorf("%s 历史 CFE 未保留: %w", baseGate.Gate, err)
			}
			if err := reviewFreezeValidateFormalRecordV1(baseManifest, baseGate, exception, headLoader); err != nil {
				return fmt.Errorf("%s 历史 Freeze/Approval 未保留: %w", baseGate.Gate, err)
			}
		}
		if baseGate.Status != headGate.Status && (reviewFreezeIsFormalStatusV1(baseGate.Status) || reviewFreezeIsFormalStatusV1(headGate.Status)) {
			formalTransitions = append(formalTransitions, index)
		}
	}
	if len(formalTransitions) > 1 {
		return fmt.Errorf("一个提交区间最多迁移一个正式 Gate，实际=%d", len(formalTransitions))
	}
	if err := reviewFreezeValidateAppendOnlyGovernanceArtifactsV1(repository, baseSHA, headSHA, headManifest); err != nil {
		return err
	}
	for _, gate := range headManifest.Gates {
		if reviewFreezeIsFormalStatusV1(gate.Status) {
			// 每次检查都重新建立全部现行 authority 的提交祖先证明，不能只依赖发生状态变化的 Gate。
			if err := reviewFreezeValidateApprovalCommitBindingsV1(repository, headSHA, gate, headLoader); err != nil {
				return err
			}
		}
	}
	if err := reviewFreezeValidateReopenedSameStateDiffV1(baseManifest, headManifest, changedFiles, headLoader); err != nil {
		return err
	}
	if len(formalTransitions) == 1 {
		index := formalTransitions[0]
		if err := reviewFreezeValidateFormalTransitionDiffV1(baseManifest, headManifest, index, changedFiles, headLoader); err != nil {
			return err
		}
	}
	return nil
}

// reviewFreezeValidateBootstrapV1 允许首次引入治理清单，但全部 Gate 必须仍是无正式 authority 的 pre-formal 状态。
func reviewFreezeValidateBootstrapV1(head reviewFreezeManifestV1) error {
	for _, gate := range head.Gates {
		if reviewFreezeIsFormalStatusV1(gate.Status) || gate.Freeze != nil || gate.ReopenException != nil {
			return fmt.Errorf("bootstrap 禁止携带正式 Gate=%s status=%s", gate.Gate, gate.Status)
		}
	}
	return nil
}

// reviewFreezeValidateTrustRootActivationBootstrapV1 要求首次激活 checker 前后的清单都不含正式 authority。
func reviewFreezeValidateTrustRootActivationBootstrapV1(base, head reviewFreezeManifestV1) error {
	if err := reviewFreezeValidateBootstrapV1(base); err != nil {
		return fmt.Errorf("trust root activation base 非 pre-formal: %w", err)
	}
	if err := reviewFreezeValidateBootstrapV1(head); err != nil {
		return fmt.Errorf("trust root activation head 非 pre-formal: %w", err)
	}
	return nil
}

// reviewFreezeValidateImmutableTrustRootV1 保证已激活 workflow 与独立 verifier 永远由 base 版本执行和解释。
func reviewFreezeValidateImmutableTrustRootV1(changedFiles []string) error {
	for _, path := range changedFiles {
		if path == reviewFreezeTransitionWorkflowPathV1 || strings.HasPrefix(path, "agent/tests/reviewfreeze/") {
			return fmt.Errorf("已激活 Review Freeze trust root 禁止修改、新增或删除=%q", path)
		}
	}
	return nil
}

// reviewFreezeValidateManifestEnvelopeTransitionV1 保持治理 Owner、Gate 顺序和 Owner policy 不被 transition PR 顺带替换。
func reviewFreezeValidateManifestEnvelopeTransitionV1(base, head reviewFreezeManifestV1) error {
	if base.SchemaVersion != head.SchemaVersion || base.GovernanceOwnerRole != head.GovernanceOwnerRole ||
		!reflect.DeepEqual(base.ImplementationOwnerRoles, head.ImplementationOwnerRoles) || len(base.Gates) != len(head.Gates) {
		return fmt.Errorf("manifest governance envelope 不得在状态迁移中改变")
	}
	for index := range base.Gates {
		if base.Gates[index].Gate != head.Gates[index].Gate || !reflect.DeepEqual(base.Gates[index].RequiredOwnerRoles, head.Gates[index].RequiredOwnerRoles) {
			return fmt.Errorf("gate/required_owner_roles policy 不得在状态迁移中改变: index=%d", index)
		}
	}
	return nil
}

// reviewFreezeValidateAppendOnlyGovernanceArtifactsV1 要求历史 Approval/CFE 原字节保留，新增文件必须被当前 Gate 精确引用。
func reviewFreezeValidateAppendOnlyGovernanceArtifactsV1(repository reviewFreezeGitRepositoryV1, baseSHA, headSHA string, head reviewFreezeManifestV1) error {
	referenced := make(map[string]struct{})
	for _, gate := range head.Gates {
		if gate.Freeze != nil {
			referenced[gate.Freeze.OwnerApprovalRef.Path] = struct{}{}
		}
		if gate.ReopenException != nil {
			referenced[gate.ReopenException.Path] = struct{}{}
		}
	}
	for _, prefix := range []string{reviewFreezeOwnerApprovalDirV1, reviewFreezeExceptionDirV1} {
		baseFiles, err := repository.listFiles(baseSHA, prefix)
		if err != nil {
			return err
		}
		headFiles, err := repository.listFiles(headSHA, prefix)
		if err != nil {
			return err
		}
		if err := reviewFreezeValidateAppendOnlyPathsV1(baseFiles, headFiles, repository.loader(baseSHA), repository.loader(headSHA), referenced); err != nil {
			return fmt.Errorf("%s append-only: %w", prefix, err)
		}
	}
	return nil
}

// reviewFreezeValidateAppendOnlyPathsV1 对任意治理目录执行纯函数式的保留和 orphan 检查。
func reviewFreezeValidateAppendOnlyPathsV1(baseFiles, headFiles []string, baseLoader, headLoader reviewFreezeArtifactLoaderV1, referenced map[string]struct{}) error {
	headSet := make(map[string]struct{}, len(headFiles))
	for _, path := range headFiles {
		headSet[path] = struct{}{}
	}
	baseSet := make(map[string]struct{}, len(baseFiles))
	for _, path := range baseFiles {
		baseSet[path] = struct{}{}
		if _, ok := headSet[path]; !ok {
			return fmt.Errorf("历史治理文件被删除=%q", path)
		}
		baseRaw, err := baseLoader(path)
		if err != nil {
			return fmt.Errorf("读取 base %s: %w", path, err)
		}
		headRaw, err := headLoader(path)
		if err != nil {
			return fmt.Errorf("读取 head %s: %w", path, err)
		}
		if !bytes.Equal(baseRaw, headRaw) {
			return fmt.Errorf("历史治理文件被覆盖=%q", path)
		}
	}
	for _, path := range headFiles {
		if _, existed := baseSet[path]; existed {
			continue
		}
		if _, ok := referenced[path]; !ok {
			return fmt.Errorf("新增治理文件未被任何 Gate 引用=%q", path)
		}
	}
	return nil
}

// reviewFreezeValidateFormalTransitionDiffV1 将正式状态变化隔离为治理文件和 CFE 明确允许的真实改动集合。
func reviewFreezeValidateFormalTransitionDiffV1(base, head reviewFreezeManifestV1, gateIndex int, changedFiles []string, loader reviewFreezeArtifactLoaderV1) error {
	baseGate := base.Gates[gateIndex]
	headGate := head.Gates[gateIndex]
	exempt := map[string]struct{}{reviewFreezeManifestPathV1: {}}
	if headGate.Freeze != nil {
		exempt[headGate.Freeze.OwnerApprovalRef.Path] = struct{}{}
	}
	if headGate.ReopenException != nil {
		exempt[headGate.ReopenException.Path] = struct{}{}
	}
	protected := map[string]struct{}{
		"Makefile": {},
		"agent/tests/contract/w2_review_freeze_manifest_v1_test.go":              {},
		"agent/tests/contract/w2_review_freeze_transition_git_v1_test.go":        {},
		"agent/tests/contract/w2_review_freeze_transition_policy_v1_test.go":     {},
		"agent/tests/reviewfreeze/strict_json_v1_test.go":                        {},
		"agent/tests/reviewfreeze/w2_review_freeze_manifest_v1_test.go":          {},
		"agent/tests/reviewfreeze/w2_review_freeze_transition_git_v1_test.go":    {},
		"agent/tests/reviewfreeze/w2_review_freeze_transition_policy_v1_test.go": {},
		".github/workflows/w2-contract-governance.yml":                           {},
		reviewFreezeTransitionWorkflowPathV1:                                     {},
	}

	allowed := []string{}
	if baseGate.Status == "reopened" && headGate.Status == "approved" {
		exception, err := reviewFreezeValidateExceptionV1(head, headGate, loader)
		if err != nil {
			return err
		}
		if exception == nil {
			return fmt.Errorf("%s 正式 CFE 迁移缺 exception", headGate.Gate)
		}
		allowed = append(allowed, exception.AllowedFiles...)
	}
	return reviewFreezeValidateControlledDiffV1(changedFiles, allowed, exempt, protected)
}

// reviewFreezeValidateReopenedSameStateDiffV1 阻止 reopened 基线在等待重批期间提前消费 CFE allowed_files。
func reviewFreezeValidateReopenedSameStateDiffV1(base, head reviewFreezeManifestV1, changedFiles []string, loader reviewFreezeArtifactLoaderV1) error {
	changed := make(map[string]struct{}, len(changedFiles))
	for _, path := range changedFiles {
		changed[path] = struct{}{}
	}
	for index := range base.Gates {
		baseGate := base.Gates[index]
		headGate := head.Gates[index]
		if baseGate.Status != "reopened" || headGate.Status != "reopened" {
			continue
		}
		exception, err := reviewFreezeValidateExceptionV1(head, headGate, loader)
		if err != nil {
			return err
		}
		if exception == nil {
			return fmt.Errorf("%s reopened same-state 缺 CFE", headGate.Gate)
		}
		for _, path := range exception.AllowedFiles {
			if _, exists := changed[path]; exists {
				return fmt.Errorf("%s reopened same-state 禁止提前修改 CFE allowed_file=%q", headGate.Gate, path)
			}
		}
	}
	return nil
}

// reviewFreezeValidateControlledDiffV1 要求非治理改动与 CFE allowed_files 完全一致，并禁止同一正式迁移修改 checker。
func reviewFreezeValidateControlledDiffV1(changedFiles, allowed []string, exempt, protected map[string]struct{}) error {
	actual := make([]string, 0, len(changedFiles))
	for _, path := range changedFiles {
		if _, ok := exempt[path]; ok {
			continue
		}
		if _, guarded := protected[path]; guarded {
			return fmt.Errorf("正式 Gate 迁移不得同时修改治理 checker=%q", path)
		}
		actual = append(actual, path)
	}
	want := append([]string(nil), allowed...)
	sort.Strings(actual)
	sort.Strings(want)
	if len(actual) != len(want) {
		return fmt.Errorf("正式迁移 changed files=%v want CFE allowed_files=%v", actual, want)
	}
	for index := range actual {
		if actual[index] != want[index] {
			return fmt.Errorf("正式迁移 changed files=%v want CFE allowed_files=%v", actual, want)
		}
	}
	return nil
}

// reviewFreezeValidateApprovalCommitBindingsV1 要求每个 Owner 签字提交是 HEAD 祖先，并确实包含所审批的 contract manifest 原始摘要。
func reviewFreezeValidateApprovalCommitBindingsV1(repository reviewFreezeGitRepositoryV1, headSHA string, gate reviewFreezeGateV1, loader reviewFreezeArtifactLoaderV1) error {
	if gate.Freeze == nil {
		return fmt.Errorf("%s formal transition 缺 freeze", gate.Gate)
	}
	raw, err := loader(gate.Freeze.OwnerApprovalRef.Path)
	if err != nil {
		return err
	}
	var approval reviewFreezeOwnerApprovalManifestV1
	if err := messageSetStrictDecodeV1(raw, &approval); err != nil {
		return err
	}
	for _, signature := range approval.OwnerApprovals {
		if err := reviewFreezeValidateSignatureCommitV1(repository, headSHA, signature); err != nil {
			return err
		}
		contractRaw, err := repository.readFile(signature.CommitSHA, gate.Freeze.ContractManifestPath)
		if err != nil {
			return fmt.Errorf("owner %s commit 未包含 contract manifest: %w", signature.OwnerRole, err)
		}
		if err := reviewFreezeCheckSHA256V1(contractRaw, gate.Freeze.ContractManifestSHA256); err != nil {
			return fmt.Errorf("owner %s commit 的 contract manifest 未绑定同一摘要: %w", signature.OwnerRole, err)
		}
	}
	if gate.ReopenException != nil {
		exceptionRaw, err := loader(gate.ReopenException.Path)
		if err != nil {
			return err
		}
		var exception reviewFreezeExceptionManifestV1
		if err := messageSetStrictDecodeV1(exceptionRaw, &exception); err != nil {
			return err
		}
		for _, signature := range exception.OwnerApprovals {
			if err := reviewFreezeValidateSignatureCommitV1(repository, headSHA, signature); err != nil {
				return fmt.Errorf("CFE %s: %w", exception.ExceptionID, err)
			}
		}
	}
	return nil
}

// reviewFreezeValidateSignatureCommitV1 确认签字引用的提交真实存在且位于被审批 HEAD 的祖先链。
func reviewFreezeValidateSignatureCommitV1(repository reviewFreezeGitRepositoryV1, headSHA string, signature reviewFreezeOwnerSignatureV1) error {
	if err := repository.validateCommit(signature.CommitSHA); err != nil {
		return fmt.Errorf("owner %s commit 不存在: %w", signature.OwnerRole, err)
	}
	if !repository.isAncestor(signature.CommitSHA, headSHA) {
		return fmt.Errorf("owner %s commit=%s 不是 HEAD 祖先", signature.OwnerRole, signature.CommitSHA)
	}
	return nil
}

// reviewFreezeIsFormalStatusV1 判断状态是否已经产生必须跨提交保留的正式 authority。
func reviewFreezeIsFormalStatusV1(status string) bool {
	return status == "review_frozen" || status == "approved" || status == "reopened"
}

// validateCommit 拒绝非完整小写 SHA 或本地 object database 中不存在的提交。
func (repository reviewFreezeGitRepositoryV1) validateCommit(sha string) error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) {
		return fmt.Errorf("commit SHA 格式非法=%q", sha)
	}
	_, err := repository.git("cat-file", "-e", sha+"^{commit}")
	return err
}

// loadManifest 从指定提交严格读取 Review Freeze manifest。
func (repository reviewFreezeGitRepositoryV1) loadManifest(sha string) (reviewFreezeManifestV1, error) {
	raw, err := repository.readFile(sha, reviewFreezeManifestPathV1)
	if err != nil {
		return reviewFreezeManifestV1{}, err
	}
	var manifest reviewFreezeManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		return reviewFreezeManifestV1{}, err
	}
	return manifest, nil
}

// loader 创建只读指定 Git tree 的治理产物加载器。
func (repository reviewFreezeGitRepositoryV1) loader(sha string) reviewFreezeArtifactLoaderV1 {
	return func(relative string) ([]byte, error) {
		if err := reviewFreezeValidateSafePathV1(relative, ""); err != nil {
			return nil, err
		}
		return repository.readFile(sha, relative)
	}
}

// fileExists 判断路径是否存在，并在存在时拒绝 symlink、tree、submodule 与可执行文件。
func (repository reviewFreezeGitRepositoryV1) fileExists(sha, relative string) (bool, error) {
	entry, exists, err := repository.treeEntry(sha, relative)
	if err != nil || !exists {
		return exists, err
	}
	if err := reviewFreezeValidateRegularBlobEntryV1(entry); err != nil {
		return false, err
	}
	return true, nil
}

// readFile 先精确解析 tree entry，再按 object ID 读取普通 100644 blob，禁止 Git 自动解引用或路径模糊匹配。
func (repository reviewFreezeGitRepositoryV1) readFile(sha, relative string) ([]byte, error) {
	entry, exists, err := repository.treeEntry(sha, relative)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("tree entry 不存在=%q", relative)
	}
	if err := reviewFreezeValidateRegularBlobEntryV1(entry); err != nil {
		return nil, err
	}
	return repository.git("cat-file", "blob", entry.objectID)
}

// treeEntry 精确查询一个仓库相对路径，拒绝 pathspec 展开为别的路径或多个 entry。
func (repository reviewFreezeGitRepositoryV1) treeEntry(sha, relative string) (reviewFreezeGitTreeEntryV1, bool, error) {
	if err := reviewFreezeValidateSafePathV1(relative, ""); err != nil {
		return reviewFreezeGitTreeEntryV1{}, false, err
	}
	raw, err := repository.git("ls-tree", "--full-tree", "-z", sha, "--", relative)
	if err != nil {
		return reviewFreezeGitTreeEntryV1{}, false, err
	}
	entries, err := reviewFreezeParseTreeEntriesV1(raw)
	if err != nil {
		return reviewFreezeGitTreeEntryV1{}, false, err
	}
	if len(entries) == 0 {
		return reviewFreezeGitTreeEntryV1{}, false, nil
	}
	if len(entries) != 1 || entries[0].path != relative {
		return reviewFreezeGitTreeEntryV1{}, false, fmt.Errorf("tree entry 非精确路径=%q entries=%d", relative, len(entries))
	}
	return entries[0], true, nil
}

// listFiles 返回指定 tree 中某治理目录的排序普通文件列表，目录中任一非 100644 blob 都失败关闭。
func (repository reviewFreezeGitRepositoryV1) listFiles(sha, prefix string) ([]string, error) {
	raw, err := repository.git("ls-tree", "--full-tree", "-r", "-z", sha, "--", strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return nil, err
	}
	entries, err := reviewFreezeParseTreeEntriesV1(raw)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.path, prefix) {
			return nil, fmt.Errorf("git ls-tree 返回越界路径=%q", entry.path)
		}
		if err := reviewFreezeValidateSafePathV1(entry.path, prefix); err != nil {
			return nil, err
		}
		if err := reviewFreezeValidateRegularBlobEntryV1(entry); err != nil {
			return nil, err
		}
		paths = append(paths, entry.path)
	}
	sort.Strings(paths)
	return paths, nil
}

// reviewFreezeParseTreeEntriesV1 解析 `git ls-tree -z` 的 mode/type/object/path 记录，保留异常路径字节用于精确拒绝。
func reviewFreezeParseTreeEntriesV1(raw []byte) ([]reviewFreezeGitTreeEntryV1, error) {
	if len(raw) == 0 {
		return []reviewFreezeGitTreeEntryV1{}, nil
	}
	records := bytes.Split(raw, []byte{0})
	entries := make([]reviewFreezeGitTreeEntryV1, 0, len(records))
	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		headerAndPath := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(headerAndPath) != 2 {
			return nil, fmt.Errorf("git ls-tree record 缺 path 分隔")
		}
		header := strings.Fields(string(headerAndPath[0]))
		if len(header) != 3 || !regexp.MustCompile(`^[0-9a-f]{40,64}$`).MatchString(header[2]) {
			return nil, fmt.Errorf("git ls-tree record header 非法=%q", string(headerAndPath[0]))
		}
		entries = append(entries, reviewFreezeGitTreeEntryV1{
			mode: header[0], object: header[1], objectID: header[2], path: string(headerAndPath[1]),
		})
	}
	return entries, nil
}

// reviewFreezeValidateRegularBlobEntryV1 将治理 authority 限定为普通不可执行文件，防止 symlink/tree/mode 形成双重解释。
func reviewFreezeValidateRegularBlobEntryV1(entry reviewFreezeGitTreeEntryV1) error {
	if entry.mode != "100644" || entry.object != "blob" {
		return fmt.Errorf("治理输入必须是 exact 100644 blob path=%q mode=%q type=%q", entry.path, entry.mode, entry.object)
	}
	return nil
}

// changedFiles 返回 base 到 head 的无 rename 归并路径集合。
func (repository reviewFreezeGitRepositoryV1) changedFiles(baseSHA, headSHA string) ([]string, error) {
	raw, err := repository.git("diff", "--no-ext-diff", "--name-only", "--no-renames", "-z", baseSHA, headSHA, "--")
	if err != nil {
		return nil, err
	}
	lines := reviewFreezeSplitNULPathsV1(raw)
	sort.Strings(lines)
	return lines, nil
}

// reviewFreezeSplitNULPathsV1 解析 Git -z 输出，避免换行文件名改变路径集合边界。
func reviewFreezeSplitNULPathsV1(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	parts := bytes.Split(raw, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			paths = append(paths, string(part))
		}
	}
	return paths
}

// isAncestor 判断审批提交是否位于 HEAD 可追溯历史中。
func (repository reviewFreezeGitRepositoryV1) isAncestor(ancestor, head string) bool {
	command := exec.Command("git", "-C", repository.root, "merge-base", "--is-ancestor", ancestor, head)
	return command.Run() == nil
}

// git 执行只读 Git object 命令，并限制错误输出为仓库元数据诊断。
func (repository reviewFreezeGitRepositoryV1) git(arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", repository.root}, arguments...)
	command := exec.Command("git", commandArguments...)
	raw, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// TestW2ReviewFreezeTransitionGitV1Bootstrap 验证首次引入只能包含 pre-formal Gate。
func TestW2ReviewFreezeTransitionGitV1Bootstrap(t *testing.T) {
	manifest, _ := reviewFreezeLoadCurrentV1(t)
	if err := reviewFreezeValidateBootstrapV1(manifest); err != nil {
		t.Fatalf("current pre-formal bootstrap rejected: %v", err)
	}
	manifest.Gates[0].Status = "review_frozen"
	manifest.Gates[0].Freeze = &reviewFreezeRecordV1{FreezeID: "CF-W2-R00-v1"}
	if err := reviewFreezeValidateBootstrapV1(manifest); err == nil {
		t.Fatal("bootstrap accepted formal gate")
	}
}

// TestW2ReviewFreezeTransitionGitV1AppendOnly 覆盖删除、覆盖和 orphan 新增治理文件的失败关闭。
func TestW2ReviewFreezeTransitionGitV1AppendOnly(t *testing.T) {
	baseRaw := map[string][]byte{"approvals/a.json": []byte("a")}
	headRaw := map[string][]byte{"approvals/a.json": []byte("a"), "approvals/b.json": []byte("b")}
	loader := func(values map[string][]byte) reviewFreezeArtifactLoaderV1 {
		return func(path string) ([]byte, error) {
			raw, ok := values[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return raw, nil
		}
	}
	if err := reviewFreezeValidateAppendOnlyPathsV1(
		[]string{"approvals/a.json"}, []string{"approvals/a.json", "approvals/b.json"},
		loader(baseRaw), loader(headRaw), map[string]struct{}{"approvals/b.json": {}},
	); err != nil {
		t.Fatalf("valid append rejected: %v", err)
	}
	cases := []struct {
		name       string
		headFiles  []string
		headValues map[string][]byte
		referenced map[string]struct{}
	}{
		{name: "deleted", headFiles: []string{}, headValues: map[string][]byte{}, referenced: map[string]struct{}{}},
		{name: "overwritten", headFiles: []string{"approvals/a.json"}, headValues: map[string][]byte{"approvals/a.json": []byte("changed")}, referenced: map[string]struct{}{}},
		{name: "orphan", headFiles: []string{"approvals/a.json", "approvals/b.json"}, headValues: headRaw, referenced: map[string]struct{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := reviewFreezeValidateAppendOnlyPathsV1(
				[]string{"approvals/a.json"}, tc.headFiles, loader(baseRaw), loader(tc.headValues), tc.referenced,
			); err == nil {
				t.Fatal("invalid append-only transition accepted")
			}
		})
	}
}

// TestW2ReviewFreezeTransitionGitV1ControlledDiff 覆盖 CFE exact-set、治理 checker 隔离和多余文件拒绝。
func TestW2ReviewFreezeTransitionGitV1ControlledDiff(t *testing.T) {
	exempt := map[string]struct{}{reviewFreezeManifestPathV1: {}}
	protected := map[string]struct{}{"Makefile": {}}
	if err := reviewFreezeValidateControlledDiffV1(
		[]string{reviewFreezeManifestPathV1, "agent/tests/contract/testdata/vector.json"},
		[]string{"agent/tests/contract/testdata/vector.json"}, exempt, protected,
	); err != nil {
		t.Fatalf("valid controlled diff rejected: %v", err)
	}
	for name, changed := range map[string][]string{
		"extra file":        {reviewFreezeManifestPathV1, "docs/unlisted.md"},
		"protected checker": {reviewFreezeManifestPathV1, "Makefile"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := reviewFreezeValidateControlledDiffV1(changed, nil, exempt, protected); err == nil {
				t.Fatal("invalid controlled diff accepted")
			}
		})
	}
}
