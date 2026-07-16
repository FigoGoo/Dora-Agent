package smokegovernance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	smokeGovernanceBaseSHAEnvV1 = "W2_SMOKE_GOVERNANCE_BASE_SHA"
	smokeGovernanceHeadSHAEnvV1 = "W2_SMOKE_GOVERNANCE_HEAD_SHA"
)

// smokeGovernanceGitRepositoryV1 只通过 Git object database 读取 base/head，不依赖调用者 worktree 内容。
type smokeGovernanceGitRepositoryV1 struct {
	root string
}

// smokeGovernanceGitTreeEntryV1 保留 ls-tree 的 mode、object type、object ID 和精确路径。
type smokeGovernanceGitTreeEntryV1 struct {
	mode     string
	object   string
	objectID string
	path     string
}

// TestW2S0G0TransitionGitV1FromObjects 从两个明确 commit object 验证真实仓库候选迁移；普通 go test 缺 SHA 时跳过该入口。
func TestW2S0G0TransitionGitV1FromObjects(t *testing.T) {
	baseSHA := os.Getenv(smokeGovernanceBaseSHAEnvV1)
	headSHA := os.Getenv(smokeGovernanceHeadSHAEnvV1)
	if baseSHA == "" && headSHA == "" {
		t.Skip("仅由受信 transition runner 提供 base/head SHA")
	}
	if baseSHA == "" || headSHA == "" {
		t.Fatalf("%s 与 %s 必须同时提供", smokeGovernanceBaseSHAEnvV1, smokeGovernanceHeadSHAEnvV1)
	}
	repository := smokeGovernanceGitRepositoryV1{root: smokeGovernanceRepositoryRootV1(t)}
	if err := smokeGovernanceValidateGitTransitionV1(repository, baseSHA, headSHA); err != nil {
		t.Fatalf("W2-S0-G0 Git-object transition 失败关闭: %v", err)
	}
}

// smokeGovernanceValidateGitTransitionV1 校验未激活候选 HEAD 的清单、绑定产物和全树禁区，并拒绝 trust-root 提前出现。
func smokeGovernanceValidateGitTransitionV1(repository smokeGovernanceGitRepositoryV1, baseSHA, headSHA string) error {
	if err := repository.validateCommit(baseSHA); err != nil {
		return fmt.Errorf("base commit: %w", err)
	}
	if err := repository.validateCommit(headSHA); err != nil {
		return fmt.Errorf("head commit: %w", err)
	}
	if !repository.isAncestor(baseSHA, headSHA) {
		return fmt.Errorf("base 必须是 head 祖先")
	}
	manifestArtifact, err := repository.readArtifact(headSHA, smokeGovernanceManifestPathV1)
	if err != nil {
		return fmt.Errorf("读取 head approval manifest: %w", err)
	}
	if manifestArtifact.Mode != "100644" {
		return fmt.Errorf("approval manifest 必须是 100644 blob，mode=%q", manifestArtifact.Mode)
	}
	var manifest smokeGovernanceManifestV1
	if err := smokeGovernanceStrictDecodeV1(manifestArtifact.Raw, &manifest); err != nil {
		return fmt.Errorf("approval manifest strict JSON: %w", err)
	}
	if err := smokeGovernanceValidateManifestV1(manifest, repository.loader(headSHA)); err != nil {
		return err
	}
	entries, err := repository.listAllEntries(headSHA)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := smokeGovernanceValidateCanonicalGitPathV1(entry.path); err != nil {
			return fmt.Errorf("head tree path: %w", err)
		}
		if err := smokeGovernanceRejectPrematureTrustRootPathV1(entry.path); err != nil {
			return err
		}
		if err := smokeGovernanceRejectForbiddenPathV1(entry.path); err != nil {
			return err
		}
	}

	return nil
}

// smokeGovernanceRejectPrematureTrustRootPathV1 防止候选阶段落入可自报激活的 workflow、release、active pointer 或 handoff。
// 候选 checker 只能在后续外部治理 bootstrap 中被固定摘要并激活。
func smokeGovernanceRejectPrematureTrustRootPathV1(relative string) error {
	if err := smokeGovernanceValidateCanonicalGitPathV1(relative); err != nil {
		return err
	}
	components := strings.Split(relative, "/")
	if len(components) < 2 || !strings.EqualFold(components[0], ".github") {
		return nil
	}
	if strings.EqualFold(components[1], "smoke-governance") {
		return fmt.Errorf("Smoke Governance trust root 尚未完成 release/rekey/handoff，禁止提前声明 path=%q", relative)
	}
	if len(components) != 3 || !strings.EqualFold(components[1], "workflows") {
		return nil
	}
	name := strings.ToLower(components[2])
	if (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) && strings.HasPrefix(name, "w2-smoke-governance") {
		return fmt.Errorf("Smoke Governance trust root 尚未完成 release/rekey/handoff，禁止提前激活 workflow=%q", relative)
	}
	return nil
}

// validateCommit 只接受完整小写 SHA，并证明对象确实是 commit，避免可移动 ref 或缩写在验证中漂移。
func (repository smokeGovernanceGitRepositoryV1) validateCommit(commitSHA string) error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commitSHA) {
		return fmt.Errorf("commit SHA 格式非法=%q", commitSHA)
	}
	if _, err := repository.git("cat-file", "-e", commitSHA+"^{commit}"); err != nil {
		return fmt.Errorf("commit object 不存在: %w", err)
	}
	return nil
}

// smokeGovernanceRejectForbiddenPathV1 拒绝三个 canonical root、其后代和 ASCII 大小写变体。
// Git 路径以 `/` 且大小写敏感；治理层额外保留大小写变体，避免在不同宿主文件系统上产生双重解释。
func smokeGovernanceRejectForbiddenPathV1(relative string) error {
	if err := smokeGovernanceValidateCanonicalGitPathV1(relative); err != nil {
		return err
	}
	components := strings.Split(relative, "/")
	forbidden := false
	switch {
	case strings.EqualFold(components[0], "smoke"):
		forbidden = true
	case strings.EqualFold(components[0], "test-adapters"):
		forbidden = true
	case len(components) >= 2 && strings.EqualFold(components[0], "deploy") && strings.EqualFold(components[1], "local-smoke"):
		forbidden = true
	}
	if forbidden {
		return fmt.Errorf("W2-S0-G0 awaiting 状态禁止路径=%q", relative)
	}
	return nil
}

// loader 返回固定 commit 的只读 artifact loader，并保留 Git mode 供调用方做语义校验。
func (repository smokeGovernanceGitRepositoryV1) loader(commitSHA string) smokeGovernanceArtifactLoaderV1 {
	return func(relative string) (smokeGovernanceArtifactV1, error) {
		return repository.readArtifact(commitSHA, relative)
	}
}

// readArtifact 精确读取一个 blob；symlink、gitlink 和 tree 不会被自动解引用。
func (repository smokeGovernanceGitRepositoryV1) readArtifact(commitSHA, relative string) (smokeGovernanceArtifactV1, error) {
	entry, exists, err := repository.treeEntry(commitSHA, relative)
	if err != nil {
		return smokeGovernanceArtifactV1{}, err
	}
	if !exists {
		return smokeGovernanceArtifactV1{}, fmt.Errorf("tree entry 不存在=%q", relative)
	}
	if entry.object != "blob" {
		return smokeGovernanceArtifactV1{}, fmt.Errorf("tree entry 必须是 blob path=%q type=%q", relative, entry.object)
	}
	raw, err := repository.git("cat-file", "blob", entry.objectID)
	if err != nil {
		return smokeGovernanceArtifactV1{}, err
	}
	return smokeGovernanceArtifactV1{Raw: raw, Mode: entry.mode}, nil
}

// treeEntry 使用精确 pathspec 查询单个 Git tree entry，拒绝模糊或多项结果。
func (repository smokeGovernanceGitRepositoryV1) treeEntry(commitSHA, relative string) (smokeGovernanceGitTreeEntryV1, bool, error) {
	if err := smokeGovernanceValidateCanonicalGitPathV1(relative); err != nil {
		return smokeGovernanceGitTreeEntryV1{}, false, err
	}
	raw, err := repository.git("ls-tree", "--full-tree", "-z", commitSHA, "--", relative)
	if err != nil {
		return smokeGovernanceGitTreeEntryV1{}, false, err
	}
	entries, err := smokeGovernanceParseTreeEntriesV1(raw)
	if err != nil {
		return smokeGovernanceGitTreeEntryV1{}, false, err
	}
	if len(entries) == 0 {
		return smokeGovernanceGitTreeEntryV1{}, false, nil
	}
	if len(entries) != 1 || entries[0].path != relative {
		return smokeGovernanceGitTreeEntryV1{}, false, fmt.Errorf("tree entry 非精确路径=%q entries=%d", relative, len(entries))
	}
	return entries[0], true, nil
}

// listAllEntries 返回指定 commit 的全部递归 tree entries，用于检查 HEAD 全树而非只看 diff。
func (repository smokeGovernanceGitRepositoryV1) listAllEntries(commitSHA string) ([]smokeGovernanceGitTreeEntryV1, error) {
	raw, err := repository.git("ls-tree", "--full-tree", "-r", "-z", commitSHA)
	if err != nil {
		return nil, err
	}
	return smokeGovernanceParseTreeEntriesV1(raw)
}

// smokeGovernanceParseTreeEntriesV1 解析 NUL 分隔的 mode/type/object/path，避免换行文件名改变集合边界。
func smokeGovernanceParseTreeEntriesV1(raw []byte) ([]smokeGovernanceGitTreeEntryV1, error) {
	if len(raw) == 0 {
		return []smokeGovernanceGitTreeEntryV1{}, nil
	}
	records := bytes.Split(raw, []byte{0})
	entries := make([]smokeGovernanceGitTreeEntryV1, 0, len(records))
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
		entries = append(entries, smokeGovernanceGitTreeEntryV1{
			mode: header[0], object: header[1], objectID: header[2], path: string(headerAndPath[1]),
		})
	}
	return entries, nil
}

// isAncestor 确保验证的 base 属于 head 历史，防止分叉 authority 替换。
func (repository smokeGovernanceGitRepositoryV1) isAncestor(baseSHA, headSHA string) bool {
	command := exec.Command("git", "-C", repository.root, "merge-base", "--is-ancestor", baseSHA, headSHA)
	return command.Run() == nil
}

// git 执行只读 Git object 命令，并将诊断限制为临时仓库元数据。
func (repository smokeGovernanceGitRepositoryV1) git(arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", repository.root}, arguments...)...)
	raw, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// smokeGovernanceNewTestGitRepositoryV1 创建隔离临时 Git 仓库，开启 filemode 以验证 mode-only 攻击。
func smokeGovernanceNewTestGitRepositoryV1(t *testing.T) smokeGovernanceGitRepositoryV1 {
	t.Helper()
	root := t.TempDir()
	smokeGovernanceRunTestGitV1(t, root, "init", "-q")
	smokeGovernanceRunTestGitV1(t, root, "config", "user.name", "Smoke Governance Test")
	smokeGovernanceRunTestGitV1(t, root, "config", "user.email", "smoke-governance@example.invalid")
	smokeGovernanceRunTestGitV1(t, root, "config", "core.filemode", "true")
	return smokeGovernanceGitRepositoryV1{root: root}
}

// smokeGovernanceWriteTestFileV1 写入测试文件并显式固定 mode，避免宿主 umask 改变 Git tree。
func smokeGovernanceWriteTestFileV1(t *testing.T, root, relative string, raw []byte, mode os.FileMode) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(absolute, raw, mode); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
	if err := os.Chmod(absolute, mode); err != nil {
		t.Fatalf("chmod %s: %v", relative, err)
	}
}

// smokeGovernanceWriteSyntheticBundleV1 将未激活的自包含候选写入临时仓库；workflow 与正式 trust root 不得在本阶段出现。
func smokeGovernanceWriteSyntheticBundleV1(t *testing.T, root string, bundle smokeGovernanceSyntheticBundleV1) {
	t.Helper()
	for relative, artifact := range bundle.Artifacts {
		mode, err := strconv.ParseUint(artifact.Mode, 8, 32)
		if err != nil {
			t.Fatalf("parse mode %s: %v", artifact.Mode, err)
		}
		smokeGovernanceWriteTestFileV1(t, root, relative, artifact.Raw, os.FileMode(mode))
	}
	manifestRaw, err := json.MarshalIndent(bundle.Manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	smokeGovernanceWriteTestFileV1(t, root, smokeGovernanceManifestPathV1, append(manifestRaw, '\n'), 0o644)
}

// smokeGovernanceCommitTestRepositoryV1 提交临时仓库全部变化并返回完整 commit SHA。
func smokeGovernanceCommitTestRepositoryV1(t *testing.T, root, message string) string {
	t.Helper()
	smokeGovernanceRunTestGitV1(t, root, "add", "-A")
	smokeGovernanceRunTestGitV1(t, root, "commit", "-q", "-m", message)
	return strings.TrimSpace(smokeGovernanceRunTestGitV1(t, root, "rev-parse", "HEAD"))
}

// smokeGovernanceRunTestGitV1 执行临时仓库命令；失败时返回有限元数据诊断。
func smokeGovernanceRunTestGitV1(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(raw)))
	}
	return string(raw)
}

// TestW2S0G0TransitionGitV1Candidate 证明未激活阶段可以引入 locked manifest、绑定产物与无禁区的 HEAD。
func TestW2S0G0TransitionGitV1Candidate(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("base\n"), 0o644)
	base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "base")
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "add locked smoke governance candidate")
	if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err != nil {
		t.Fatalf("valid governance candidate rejected: %v", err)
	}
}

// TestW2S0G0TransitionGitV1RejectsForbiddenHeadPaths 使用真实 Git tree 覆盖路径本身、后代、大小写和分隔绕过。
func TestW2S0G0TransitionGitV1RejectsForbiddenHeadPaths(t *testing.T) {
	paths := []string{
		"smoke",
		"smoke/runner.ts",
		"Smoke/runner.ts",
		"SMOKE/hidden.ts",
		"test-adapters",
		"test-adapters/model.ts",
		"Test-Adapters/model.ts",
		"deploy/local-smoke",
		"deploy/local-smoke/compose.yml",
		"Deploy/Local-Smoke/compose.yml",
		`smoke\runner.ts`,
		`deploy\local-smoke\compose.yml`,
	}
	for _, relative := range paths {
		t.Run(relative, func(t *testing.T) {
			repository := smokeGovernanceNewTestGitRepositoryV1(t)
			smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("base\n"), 0o644)
			base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "base")
			smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
			smokeGovernanceWriteTestFileV1(t, repository.root, relative, []byte("forbidden\n"), 0o644)
			head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "attempt forbidden harness")
			if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err == nil {
				t.Fatalf("forbidden path accepted=%q", relative)
			}
		})
	}
}

// TestW2S0G0TransitionGitV1AllowsDirectoryBoundaryNearMisses 防止禁区检查误伤仅共享字符串前缀或非根目录的普通路径。
func TestW2S0G0TransitionGitV1AllowsDirectoryBoundaryNearMisses(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("base\n"), 0o644)
	base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "base")
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	for _, relative := range []string{
		"smokehouse/runner.ts",
		"smoke-file.txt",
		"test-adapters-v2/model.ts",
		"deploy/local-smokehouse/compose.yml",
		"docs/smoke/readme.md",
	} {
		smokeGovernanceWriteTestFileV1(t, repository.root, relative, []byte("allowed boundary\n"), 0o644)
	}
	head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "allowed near misses")
	if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err != nil {
		t.Fatalf("directory-boundary near miss rejected: %v", err)
	}
}

// TestW2S0G0TransitionGitV1RejectsBoundArtifactTreeAttacks 覆盖文档可执行、symlink、source mode 和 source content 漂移。
func TestW2S0G0TransitionGitV1RejectsBoundArtifactTreeAttacks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "artifact executable", mutate: func(t *testing.T, root string) {
			if err := os.Chmod(filepath.Join(root, filepath.FromSlash(smokeGovernanceADRPathV1)), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "artifact symlink", mutate: func(t *testing.T, root string) {
			absolute := filepath.Join(root, filepath.FromSlash(smokeGovernanceADRPathV1))
			if err := os.Remove(absolute); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../../../testing/w2-smoke-context-registry-contract-v1.md", absolute); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "api source mode drift", mutate: func(t *testing.T, root string) {
			if err := os.Chmod(filepath.Join(root, filepath.FromSlash(smokeGovernanceAPISourcePathV1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "api source content drift", mutate: func(t *testing.T, root string) {
			absolute := filepath.Join(root, filepath.FromSlash(smokeGovernanceAPISourcePathV1))
			file, err := os.OpenFile(absolute, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("drift\n"); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := smokeGovernanceNewTestGitRepositoryV1(t)
			smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("base\n"), 0o644)
			base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "base")
			smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
			tc.mutate(t, repository.root)
			head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "tree attack")
			if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err == nil {
				t.Fatal("bound artifact tree attack was accepted")
			}
		})
	}
}

// TestW2S0G0TransitionGitV1RejectsPrematureTrustRoot 防止候选 checker 在 handoff/rekey 完成前用 workflow、release 或 pointer 自我激活。
func TestW2S0G0TransitionGitV1RejectsPrematureTrustRoot(t *testing.T) {
	for _, relative := range []string{
		smokeGovernanceWorkflowPathV1,
		".github/workflows/w2-smoke-governance-bootstrap.yaml",
		".github/workflows/W2-SMOKE-GOVERNANCE-next.yml",
		".github/smoke-governance/releases/v1.json",
		".github/SMOKE-GOVERNANCE/active.json",
	} {
		t.Run(relative, func(t *testing.T) {
			repository := smokeGovernanceNewTestGitRepositoryV1(t)
			smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("base\n"), 0o644)
			base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "base")
			smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
			smokeGovernanceWriteTestFileV1(t, repository.root, relative, []byte("premature trust root\n"), 0o644)
			head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "attempt premature trust root")
			if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err == nil || !strings.Contains(err.Error(), "禁止提前") {
				t.Fatalf("premature trust-root path=%q error=%v", relative, err)
			}
		})
	}
}

// TestW2S0G0PrematureTrustRootPathV1Unit 固定文件名边界，避免误伤普通 workflow 或文档。
func TestW2S0G0PrematureTrustRootPathV1Unit(t *testing.T) {
	for _, relative := range []string{".github/workflows/w2-smoke-governance.yml", ".github/workflows/w2-smoke-governance-v2.yaml", ".github/smoke-governance/active.json"} {
		if err := smokeGovernanceRejectPrematureTrustRootPathV1(relative); err == nil {
			t.Fatalf("premature trust-root accepted=%q", relative)
		}
	}
	for _, relative := range []string{".github/workflows/w2-contract-governance.yml", ".github/workflows/smoke-governance.yml", "docs/smoke-governance/design.md"} {
		if err := smokeGovernanceRejectPrematureTrustRootPathV1(relative); err != nil {
			t.Fatalf("trust-root near miss rejected=%q: %v", relative, err)
		}
	}
}

// TestW2S0G0TransitionGitV1AllowsVerifierEvolutionBeforeActivation 保证候选 checker 在正式 trust-root 激活前仍可修复，不形成永久自锁。
func TestW2S0G0TransitionGitV1AllowsVerifierEvolutionBeforeActivation(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "locked candidate")
	smokeGovernanceWriteTestFileV1(t, repository.root, smokeGovernanceTrustRootV1+"future_fix_test.go", []byte("package smokegovernance_test\n"), 0o644)
	head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "candidate verifier fix")
	if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err != nil {
		t.Fatalf("pre-activation verifier evolution rejected: %v", err)
	}
}

// TestW2S0G0TransitionGitV1RejectsSplitBrain 证明另一条分叉不能替换已选择的 base authority。
func TestW2S0G0TransitionGitV1RejectsSplitBrain(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteTestFileV1(t, repository.root, "README.md", []byte("common\n"), 0o644)
	common := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "common")
	smokeGovernanceRunTestGitV1(t, repository.root, "checkout", "-q", "-b", "left", common)
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	left := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "left authority")
	smokeGovernanceRunTestGitV1(t, repository.root, "checkout", "-q", "-b", "right", common)
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	smokeGovernanceWriteTestFileV1(t, repository.root, smokeGovernanceContractPathV1, []byte("different right contract\n"), 0o644)
	right := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "right authority")
	if err := smokeGovernanceValidateGitTransitionV1(repository, left, right); err == nil {
		t.Fatal("split-brain authority was accepted")
	}
}

// TestW2S0G0TransitionGitV1RejectsAbbreviatedCommit 固定 transition 只接受不可变完整 commit object。
func TestW2S0G0TransitionGitV1RejectsAbbreviatedCommit(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "candidate")
	if err := smokeGovernanceValidateGitTransitionV1(repository, head[:12], head); err == nil || !strings.Contains(err.Error(), "SHA 格式非法") {
		t.Fatalf("abbreviated commit error=%v", err)
	}
}

// TestW2S0G0TransitionGitV1AllowsRemovingHistoricalForbiddenPath 证明全树门禁允许删除历史误入禁区的修复 PR。
func TestW2S0G0TransitionGitV1AllowsRemovingHistoricalForbiddenPath(t *testing.T) {
	repository := smokeGovernanceNewTestGitRepositoryV1(t)
	smokeGovernanceWriteTestFileV1(t, repository.root, "smoke/legacy.txt", []byte("legacy forbidden\n"), 0o644)
	base := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "legacy forbidden base")
	if err := os.Remove(filepath.Join(repository.root, "smoke/legacy.txt")); err != nil {
		t.Fatal(err)
	}
	smokeGovernanceWriteSyntheticBundleV1(t, repository.root, smokeGovernanceNewSyntheticBundleV1(t))
	head := smokeGovernanceCommitTestRepositoryV1(t, repository.root, "remove legacy forbidden path")
	if err := smokeGovernanceValidateGitTransitionV1(repository, base, head); err != nil {
		t.Fatalf("forbidden-path cleanup rejected: %v", err)
	}
}

// TestW2S0G0ForbiddenPathV1Unit 固定 canonical `/`、ASCII 大小写和目录边界语义。
func TestW2S0G0ForbiddenPathV1Unit(t *testing.T) {
	for _, relative := range []string{"smoke", "smoke/a", "SMOKE/a", "test-adapters/x", "Deploy/Local-Smoke/x", `smoke\x`, "smoke.", "smoke ", "smoke::$DATA", "smo\u202eke/x"} {
		if err := smokeGovernanceRejectForbiddenPathV1(relative); err == nil {
			t.Fatalf("forbidden path accepted=%q", relative)
		}
	}
	for _, relative := range []string{"smokehouse/x", "test-adapters-v2/x", "deploy/local-smokehouse/x", "docs/smoke/x"} {
		if err := smokeGovernanceRejectForbiddenPathV1(relative); err != nil {
			t.Fatalf("boundary near miss rejected=%q: %v", relative, err)
		}
	}
}
