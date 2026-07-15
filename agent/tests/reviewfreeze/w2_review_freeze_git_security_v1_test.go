package reviewfreeze_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestW2ReviewFreezeTransitionGitV1RejectsNonRegularTreeEntries 使用真实 Git tree 证明 symlink 与 tree 不能成为治理输入。
func TestW2ReviewFreezeTransitionGitV1RejectsNonRegularTreeEntries(t *testing.T) {
	repository := reviewFreezeNewTestGitRepositoryV1(t)
	directory := strings.TrimSuffix(reviewFreezeOwnerApprovalDirV1, "/")
	targetPath := reviewFreezeOwnerApprovalDirV1 + "target.json"
	linkPath := reviewFreezeOwnerApprovalDirV1 + "link.json"
	reviewFreezeWriteTestFileV1(t, repository.root, targetPath, []byte("target"))
	if err := os.Symlink("target.json", filepath.Join(repository.root, filepath.FromSlash(linkPath))); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	commit := reviewFreezeCommitTestRepositoryV1(t, repository.root, "symlink")

	if _, err := repository.readFile(commit, linkPath); err == nil {
		t.Fatal("symlink tree entry was accepted")
	}
	if _, err := repository.readFile(commit, directory); err == nil {
		t.Fatal("tree entry was accepted as file")
	}
	if _, err := repository.listFiles(commit, reviewFreezeOwnerApprovalDirV1); err == nil {
		t.Fatal("governance directory containing symlink was accepted")
	}
}

// TestW2ReviewFreezeTransitionGitV1RejectsExecutableModeChange 证明同字节 mode-only 变化仍破坏 append-only authority。
func TestW2ReviewFreezeTransitionGitV1RejectsExecutableModeChange(t *testing.T) {
	repository := reviewFreezeNewTestGitRepositoryV1(t)
	path := reviewFreezeOwnerApprovalDirV1 + "approval.json"
	reviewFreezeWriteTestFileV1(t, repository.root, path, []byte("immutable"))
	base := reviewFreezeCommitTestRepositoryV1(t, repository.root, "base regular")
	if _, err := repository.readFile(base, path); err != nil {
		t.Fatalf("regular base rejected: %v", err)
	}
	if err := os.Chmod(filepath.Join(repository.root, filepath.FromSlash(path)), 0o755); err != nil {
		t.Fatalf("chmod executable: %v", err)
	}
	head := reviewFreezeCommitTestRepositoryV1(t, repository.root, "mode change")

	if _, err := repository.readFile(head, path); err == nil {
		t.Fatal("100755 blob was accepted")
	}
	if _, err := repository.listFiles(head, reviewFreezeOwnerApprovalDirV1); err == nil {
		t.Fatal("append-only directory accepted executable authority")
	}
}

// TestW2ReviewFreezeTransitionGitV1RejectsAppendOnlySplitBrain 构造分叉历史，证明 head 不能以另一条追加链丢弃 base authority。
func TestW2ReviewFreezeTransitionGitV1RejectsAppendOnlySplitBrain(t *testing.T) {
	repository := reviewFreezeNewTestGitRepositoryV1(t)
	commonPath := reviewFreezeOwnerApprovalDirV1 + "common.json"
	leftPath := reviewFreezeOwnerApprovalDirV1 + "left.json"
	rightPath := reviewFreezeOwnerApprovalDirV1 + "right.json"
	reviewFreezeWriteTestFileV1(t, repository.root, commonPath, []byte("common"))
	common := reviewFreezeCommitTestRepositoryV1(t, repository.root, "common")
	reviewFreezeRunTestGitV1(t, repository.root, "checkout", "-q", "-b", "left", common)
	reviewFreezeWriteTestFileV1(t, repository.root, leftPath, []byte("left authority"))
	left := reviewFreezeCommitTestRepositoryV1(t, repository.root, "left append")
	reviewFreezeRunTestGitV1(t, repository.root, "checkout", "-q", "-b", "right", common)
	reviewFreezeWriteTestFileV1(t, repository.root, rightPath, []byte("right authority"))
	right := reviewFreezeCommitTestRepositoryV1(t, repository.root, "right append")

	baseFiles, err := repository.listFiles(left, reviewFreezeOwnerApprovalDirV1)
	if err != nil {
		t.Fatalf("list left: %v", err)
	}
	headFiles, err := repository.listFiles(right, reviewFreezeOwnerApprovalDirV1)
	if err != nil {
		t.Fatalf("list right: %v", err)
	}
	err = reviewFreezeValidateAppendOnlyPathsV1(
		baseFiles, headFiles, repository.loader(left), repository.loader(right), map[string]struct{}{rightPath: {}},
	)
	if err == nil {
		t.Fatal("split-brain append-only history was accepted")
	}
}

// TestW2ReviewFreezeTransitionGitV1TwoPhaseCFEDiff 固定 reopen 只治理、reapproval 才消费 allowed_files 的两阶段边界。
func TestW2ReviewFreezeTransitionGitV1TwoPhaseCFEDiff(t *testing.T) {
	approved, _ := reviewFreezeSyntheticFormalV1(t, "approved", false)
	reopened, reopenedOverlay := reviewFreezeSyntheticFormalV1(t, "reopened", false)
	reapproved, reapprovedOverlay := reviewFreezeSyntheticFormalV1(t, "approved", true)
	rootLoader := reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t))
	reopenedLoader := reviewFreezeOverlayLoaderV1(reopenedOverlay, rootLoader)
	reapprovedLoader := reviewFreezeOverlayLoaderV1(reapprovedOverlay, rootLoader)
	manifestPath := reopened.Gates[0].Freeze.ContractManifestPath

	reopenGovernanceOnly := []string{
		reviewFreezeManifestPathV1,
		reopened.Gates[0].Freeze.OwnerApprovalRef.Path,
		reopened.Gates[0].ReopenException.Path,
	}
	if err := reviewFreezeValidateFormalTransitionDiffV1(approved, reopened, 0, reopenGovernanceOnly, reopenedLoader); err != nil {
		t.Fatalf("governance-only reopen rejected: %v", err)
	}
	if err := reviewFreezeValidateFormalTransitionDiffV1(approved, reopened, 0, append(reopenGovernanceOnly, manifestPath), reopenedLoader); err == nil {
		t.Fatal("reopen consumed allowed_file before reapproval")
	}

	reapprovalDiff := []string{
		reviewFreezeManifestPathV1,
		reapproved.Gates[0].Freeze.OwnerApprovalRef.Path,
		manifestPath,
	}
	if err := reviewFreezeValidateFormalTransitionDiffV1(reopened, reapproved, 0, reapprovalDiff, reapprovedLoader); err != nil {
		t.Fatalf("exact CFE reapproval diff rejected: %v", err)
	}
	if err := reviewFreezeValidateFormalTransitionDiffV1(reopened, reapproved, 0, reapprovalDiff[:2], reapprovedLoader); err == nil {
		t.Fatal("reapproval without exact allowed_file was accepted")
	}
	if err := reviewFreezeValidateReopenedSameStateDiffV1(reopened, reopened, []string{manifestPath}, reopenedLoader); err == nil {
		t.Fatal("reopened same-state modified allowed_file")
	}
}

// TestW2ReviewFreezeTransitionGitV1ImmutableTrustRoot 固定 workflow 与 verifier 目录的新增、修改和删除均由 path diff 失败关闭。
func TestW2ReviewFreezeTransitionGitV1ImmutableTrustRoot(t *testing.T) {
	if err := reviewFreezeValidateImmutableTrustRootV1([]string{"docs/allowed.md"}); err != nil {
		t.Fatalf("unrelated path rejected: %v", err)
	}
	for _, path := range []string{
		reviewFreezeTransitionWorkflowPathV1,
		"agent/tests/reviewfreeze/w2_review_freeze_transition_git_v1_test.go",
		"agent/tests/reviewfreeze/new_bypass_test.go",
	} {
		t.Run(path, func(t *testing.T) {
			if err := reviewFreezeValidateImmutableTrustRootV1([]string{path}); err == nil {
				t.Fatal("trust root mutation was accepted")
			}
		})
	}
}

// TestW2ReviewFreezeTransitionGitV1ActivationRejectsFormalDowngrade 防止无旧 workflow 时把正式 base 降级成 pre-formal head。
func TestW2ReviewFreezeTransitionGitV1ActivationRejectsFormalDowngrade(t *testing.T) {
	formalBase, _ := reviewFreezeSyntheticFormalV1(t, "approved", false)
	preFormalHead, _ := reviewFreezeLoadCurrentV1(t)
	if err := reviewFreezeValidateTrustRootActivationBootstrapV1(formalBase, preFormalHead); err == nil {
		t.Fatal("formal base to pre-formal head was accepted during trust-root activation")
	}
}

// reviewFreezeNewTestGitRepositoryV1 创建只存在于 t.TempDir 的最小 Git 仓库，避免测试读取调用者工作树状态。
func reviewFreezeNewTestGitRepositoryV1(t *testing.T) reviewFreezeGitRepositoryV1 {
	t.Helper()
	root := t.TempDir()
	reviewFreezeRunTestGitV1(t, root, "init", "-q")
	reviewFreezeRunTestGitV1(t, root, "config", "user.name", "Review Freeze Test")
	reviewFreezeRunTestGitV1(t, root, "config", "user.email", "review-freeze@example.invalid")
	return reviewFreezeGitRepositoryV1{root: root}
}

// reviewFreezeWriteTestFileV1 写入普通 100644 测试文件，并显式消除宿主 umask 对 Git mode 的影响。
func reviewFreezeWriteTestFileV1(t *testing.T, root, relative string, raw []byte) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(absolute, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
	if err := os.Chmod(absolute, 0o644); err != nil {
		t.Fatalf("chmod %s: %v", relative, err)
	}
}

// reviewFreezeCommitTestRepositoryV1 提交当前临时仓库全部变化并返回完整小写 SHA。
func reviewFreezeCommitTestRepositoryV1(t *testing.T, root, message string) string {
	t.Helper()
	reviewFreezeRunTestGitV1(t, root, "add", "-A")
	reviewFreezeRunTestGitV1(t, root, "commit", "-q", "-m", message)
	return strings.TrimSpace(reviewFreezeRunTestGitV1(t, root, "rev-parse", "HEAD"))
}

// reviewFreezeRunTestGitV1 执行临时仓库命令；失败时只输出测试仓库元数据诊断。
func reviewFreezeRunTestGitV1(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(raw)))
	}
	return string(raw)
}
