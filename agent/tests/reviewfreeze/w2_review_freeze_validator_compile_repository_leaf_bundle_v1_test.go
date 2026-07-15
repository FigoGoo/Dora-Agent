package reviewfreeze_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	reviewFreezeCompileRepositoryLeafCountV1         = 14
	reviewFreezeCompileRepositoryLeafMaxTotalBytesV1 = 32 << 20

	reviewFreezeCompileRepositoryLeafVerifiedClaimV1      = "repository_leaf_blob_identity_verified"
	reviewFreezeCompileRepositoryLeafFormalFreezeStatusV1 = "not_established"
	reviewFreezeCompileRepositoryLeafBaseTreeGapV1        = "base_tree_membership_unverified"
	reviewFreezeCompileRepositoryLeafCommitAncestryGapV1  = "commit_ancestry_unverified"
)

// reviewFreezeCompileRepositoryLeafListEntryV1 是 repository leaf loader 的一次
// List 结果。Mode 只用于拒绝 symlink 和非普通文件，不把宿主权限位冒充为 Git tree mode。
type reviewFreezeCompileRepositoryLeafListEntryV1 struct {
	Path string
	Mode fs.FileMode
}

// reviewFreezeCompileRepositoryLeafOpenedV1 是一次 Open 的原子结果。loader 实现应以
// no-follow 语义打开 Path，并返回打开时重新观察到的类型，避免只信任先前 List 结果。
// Reader.Close 必须能中断进行中的 Read，使 verifier 可以兑现 context 取消边界。
type reviewFreezeCompileRepositoryLeafOpenedV1 struct {
	Path   string
	Mode   fs.FileMode
	Reader io.ReadCloser
}

// reviewFreezeCompileRepositoryLeafLoaderV1 是 repository leaf resolver 的最小消费方
// 接口。一次验证只调用 List 一次，并按 strict snapshot 派生的每个 Path 调用 Open 一次。
type reviewFreezeCompileRepositoryLeafLoaderV1 interface {
	List(context.Context) ([]reviewFreezeCompileRepositoryLeafListEntryV1, error)
	Open(context.Context, string) (reviewFreezeCompileRepositoryLeafOpenedV1, error)
}

// reviewFreezeVerifiedCompileRepositoryLeafV1 保存已经逐字节验证的单个 leaf。raw 使用
// string 冻结，避免调用方修改返回切片后污染后续复用。
type reviewFreezeVerifiedCompileRepositoryLeafV1 struct {
	metadata reviewFreezeCompileInputSnapshotRepoFileV1
	raw      string
}

// reviewFreezeCompileRepositoryLeafVerificationScopeV1 明确本 verifier 的证明边界。
// Blob identity 已验证不等于该 blob 属于某个 base tree，更不等于某个 commit 的 ancestry。
type reviewFreezeCompileRepositoryLeafVerificationScopeV1 struct {
	VerifiedClaim      string
	FormalFreezeStatus string
	OpenGaps           []string
}

// reviewFreezeVerifiedCompileRepositoryLeafBundleV1 是一次 resolver 调用的不可变结果。
// 它只证明 snapshot 的 14 个 path 各自读取到的 bytes 与 size/SHA-256/Git blob SHA-1
// 一致；base tree membership 和 commit ancestry 仍是 open gap，不能据此宣称 Formal Freeze。
type reviewFreezeVerifiedCompileRepositoryLeafBundleV1 struct {
	paths      []string
	leaves     map[string]reviewFreezeVerifiedCompileRepositoryLeafV1
	totalBytes int64
}

// Paths 返回已验证 repository path 的有序副本，不暴露内部切片。
func (bundle *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) Paths() []string {
	if bundle == nil {
		return nil
	}
	return append([]string(nil), bundle.paths...)
}

// Leaf 返回一个 leaf 的 metadata 值副本和 bytes 副本；该方法不会再次调用 loader。
func (bundle *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) Leaf(path string) (reviewFreezeCompileInputSnapshotRepoFileV1, []byte, bool) {
	if bundle == nil {
		return reviewFreezeCompileInputSnapshotRepoFileV1{}, nil, false
	}
	leaf, exists := bundle.leaves[path]
	if !exists {
		return reviewFreezeCompileInputSnapshotRepoFileV1{}, nil, false
	}
	return leaf.metadata, []byte(leaf.raw), true
}

// TotalBytes 返回本次调用实际完成 size/hash/blob 校验的总字节数。
func (bundle *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) TotalBytes() int64 {
	if bundle == nil {
		return 0
	}
	return bundle.totalBytes
}

// Scope 返回固定证明范围的值副本。Formal Freeze 明确保持 not_established，直到独立
// verifier 补齐 base tree membership 和 commit ancestry 两个 open gap。
func (bundle *reviewFreezeVerifiedCompileRepositoryLeafBundleV1) Scope() reviewFreezeCompileRepositoryLeafVerificationScopeV1 {
	if bundle == nil {
		return reviewFreezeCompileRepositoryLeafVerificationScopeV1{}
	}
	return reviewFreezeCompileRepositoryLeafVerificationScopeV1{
		VerifiedClaim:      reviewFreezeCompileRepositoryLeafVerifiedClaimV1,
		FormalFreezeStatus: reviewFreezeCompileRepositoryLeafFormalFreezeStatusV1,
		OpenGaps: []string{
			reviewFreezeCompileRepositoryLeafBaseTreeGapV1,
			reviewFreezeCompileRepositoryLeafCommitAncestryGapV1,
		},
	}
}

// reviewFreezeDeriveCompileRepositoryLeavesV1 是 two-phase resolver 的纯第一阶段：先
// 完成 snapshot strict JSON、statement/ref 绑定和全部语义校验，成功后才复制并返回 14
// 个 repository leaf ref。任何 loader 调用都不得发生在该函数成功之前。
func reviewFreezeDeriveCompileRepositoryLeavesV1(snapshotRaw []byte, statement reviewFreezeValidatorCompileAttestationV1) ([]reviewFreezeCompileInputSnapshotRepoFileV1, int64, error) {
	// 冻结调用方切片，保证 strict validation 与随后 typed decode 观察同一组字节。
	frozenSnapshotRaw := append([]byte(nil), snapshotRaw...)
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(frozenSnapshotRaw, statement); err != nil {
		return nil, 0, fmt.Errorf("repository leaf strict snapshot: %w", err)
	}
	var snapshot reviewFreezeCompileInputSnapshotV1
	if err := json.Unmarshal(frozenSnapshotRaw, &snapshot); err != nil {
		return nil, 0, fmt.Errorf("repository leaf decode verified snapshot: %w", err)
	}
	if len(snapshot.RepositoryFiles) != reviewFreezeCompileRepositoryLeafCountV1 {
		return nil, 0, fmt.Errorf("repository leaf derived count=%d want=%d", len(snapshot.RepositoryFiles), reviewFreezeCompileRepositoryLeafCountV1)
	}

	expected := append([]reviewFreezeCompileInputSnapshotRepoFileV1(nil), snapshot.RepositoryFiles...)
	total := int64(0)
	for _, leaf := range expected {
		if leaf.SizeBytes > reviewFreezeCompileRepositoryLeafMaxTotalBytesV1-total {
			return nil, 0, fmt.Errorf("repository leaf declared total bytes 超出预算 path=%q limit=%d", leaf.Path, reviewFreezeCompileRepositoryLeafMaxTotalBytesV1)
		}
		total += leaf.SizeBytes
	}
	if total <= 0 {
		return nil, 0, fmt.Errorf("repository leaf declared total bytes 非法=%d", total)
	}
	return expected, total, nil
}

// reviewFreezeVerifyCompileRepositoryLeafBundleV1 执行严格的 two-phase 解析。第一阶段
// 只验证 snapshot 并派生路径；第二阶段才 List exact-set，并按派生顺序逐 path 单次有界
// Open。成功结论只到 blob identity，不能替代 Git tree/commit provenance verifier。
func reviewFreezeVerifyCompileRepositoryLeafBundleV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	loader reviewFreezeCompileRepositoryLeafLoaderV1,
) (*reviewFreezeVerifiedCompileRepositoryLeafBundleV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("repository leaf context 不能为空")
	}
	if loader == nil {
		return nil, fmt.Errorf("repository leaf loader 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("repository leaf context: %w", err)
	}

	expected, declaredTotal, err := reviewFreezeDeriveCompileRepositoryLeavesV1(snapshotRaw, statement)
	if err != nil {
		return nil, err
	}
	listed, err := loader.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository leaf List: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("repository leaf context after List: %w", err)
	}
	if err := reviewFreezeValidateCompileRepositoryLeafListingV1(listed, expected); err != nil {
		return nil, err
	}

	verified := &reviewFreezeVerifiedCompileRepositoryLeafBundleV1{
		paths:  make([]string, 0, len(expected)),
		leaves: make(map[string]reviewFreezeVerifiedCompileRepositoryLeafV1, len(expected)),
	}
	for _, metadata := range expected {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("repository leaf context before Open path=%q: %w", metadata.Path, err)
		}
		raw, loadErr := reviewFreezeLoadCompileRepositoryLeafOnceV1(ctx, loader, metadata)
		if loadErr != nil {
			return nil, loadErr
		}
		if int64(len(raw)) > reviewFreezeCompileRepositoryLeafMaxTotalBytesV1-verified.totalBytes {
			return nil, fmt.Errorf("repository leaf loaded bytes 超出预算 path=%q", metadata.Path)
		}
		verified.totalBytes += int64(len(raw))
		verified.paths = append(verified.paths, metadata.Path)
		verified.leaves[metadata.Path] = reviewFreezeVerifiedCompileRepositoryLeafV1{
			metadata: metadata,
			raw:      string(raw),
		}
	}
	if verified.totalBytes != declaredTotal {
		return nil, fmt.Errorf("repository leaf loaded total=%d want=%d", verified.totalBytes, declaredTotal)
	}
	return verified, nil
}

// reviewFreezeValidateCompileRepositoryLeafListingV1 在任何 Open 前校验 List exact-set。
// List 顺序不受信任，但路径唯一性、普通文件类型和 symlink 拒绝必须确定性完成。
func reviewFreezeValidateCompileRepositoryLeafListingV1(listed []reviewFreezeCompileRepositoryLeafListEntryV1, expected []reviewFreezeCompileInputSnapshotRepoFileV1) error {
	if len(listed) > reviewFreezeCompileRepositoryLeafCountV1 {
		return fmt.Errorf("repository leaf extra/list entry budget count=%d want=%d", len(listed), reviewFreezeCompileRepositoryLeafCountV1)
	}
	expectedPaths := make(map[string]struct{}, len(expected))
	for _, leaf := range expected {
		expectedPaths[leaf.Path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(listed))
	for _, entry := range listed {
		if err := reviewFreezeValidateSafePathV1(entry.Path, ""); err != nil {
			return fmt.Errorf("repository leaf listed path: %w", err)
		}
		if _, duplicate := seen[entry.Path]; duplicate {
			return fmt.Errorf("repository leaf duplicate listed path=%q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
		if _, exists := expectedPaths[entry.Path]; !exists {
			return fmt.Errorf("repository leaf extra listed path=%q", entry.Path)
		}
		if entry.Mode&fs.ModeSymlink != 0 {
			return fmt.Errorf("repository leaf symlink 禁止 path=%q", entry.Path)
		}
		if !entry.Mode.IsRegular() {
			return fmt.Errorf("repository leaf non-regular 禁止 path=%q mode=%v", entry.Path, entry.Mode)
		}
	}
	for _, leaf := range expected {
		if _, exists := seen[leaf.Path]; !exists {
			return fmt.Errorf("repository leaf missing listed path=%q", leaf.Path)
		}
	}
	return nil
}

// reviewFreezeLoadCompileRepositoryLeafOnceV1 只 Open 指定 path 一次，并最多读取声明
// size+1 字节。打开时的 path/type 会再次校验，以失败关闭 List/Open 间的替换或 symlink。
func reviewFreezeLoadCompileRepositoryLeafOnceV1(
	ctx context.Context,
	loader reviewFreezeCompileRepositoryLeafLoaderV1,
	metadata reviewFreezeCompileInputSnapshotRepoFileV1,
) ([]byte, error) {
	opened, err := loader.Open(ctx, metadata.Path)
	if err != nil {
		// Go 接口允许实现同时返回资源与错误；失败路径也必须关闭已经交出的句柄，
		// 否则恶意或故障 loader 可以按 path 稳定泄漏文件描述符。
		if opened.Reader != nil {
			if closeErr := opened.Reader.Close(); closeErr != nil {
				return nil, fmt.Errorf("repository leaf Open path=%q: %w; close=%v", metadata.Path, err, closeErr)
			}
		}
		return nil, fmt.Errorf("repository leaf Open path=%q: %w", metadata.Path, err)
	}
	if opened.Reader == nil {
		return nil, fmt.Errorf("repository leaf missing content path=%q", metadata.Path)
	}
	if opened.Path != metadata.Path {
		closeErr := opened.Reader.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("repository leaf opened path mismatch=%q want=%q; close: %w", opened.Path, metadata.Path, closeErr)
		}
		return nil, fmt.Errorf("repository leaf opened path mismatch=%q want=%q", opened.Path, metadata.Path)
	}
	if opened.Mode&fs.ModeSymlink != 0 || !opened.Mode.IsRegular() {
		closeErr := opened.Reader.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("repository leaf opened non-regular/symlink path=%q mode=%v; close: %w", metadata.Path, opened.Mode, closeErr)
		}
		return nil, fmt.Errorf("repository leaf opened non-regular/symlink path=%q mode=%v", metadata.Path, opened.Mode)
	}
	if err := ctx.Err(); err != nil {
		closeErr := opened.Reader.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("repository leaf context after Open path=%q: %w; close=%v", metadata.Path, err, closeErr)
		}
		return nil, fmt.Errorf("repository leaf context after Open path=%q: %w", metadata.Path, err)
	}

	raw, err := reviewFreezeReadCompileRepositoryLeafBoundedV1(ctx, opened.Reader, metadata.Path, metadata.SizeBytes+1)
	if err != nil {
		return nil, err
	}
	actualSize := int64(len(raw))
	switch {
	case actualSize < metadata.SizeBytes:
		return nil, fmt.Errorf("repository leaf truncated path=%q actual=%d want=%d", metadata.Path, actualSize, metadata.SizeBytes)
	case actualSize > metadata.SizeBytes:
		return nil, fmt.Errorf("repository leaf oversized path=%q actual>=%d want=%d", metadata.Path, actualSize, metadata.SizeBytes)
	}
	if actualSHA := reviewFreezeSHA256V1(raw); actualSHA != metadata.SHA256 {
		return nil, fmt.Errorf("repository leaf SHA-256 drift path=%q actual=%q want=%q", metadata.Path, actualSHA, metadata.SHA256)
	}
	if actualGitBlob := reviewFreezeCompileRepositoryLeafGitBlobSHAV1(raw); actualGitBlob != metadata.GitBlobSHA {
		return nil, fmt.Errorf("repository leaf git blob SHA-1 drift path=%q actual=%q want=%q", metadata.Path, actualGitBlob, metadata.GitBlobSHA)
	}
	return raw, nil
}

// reviewFreezeReadCompileRepositoryLeafBoundedV1 把 generic Reader 的阻塞读取放入至多一个
// goroutine；context 取消时主动 Close 句柄并立即失败关闭。loader 契约要求 Close 能中断
// Read，避免取消路径残留读取 goroutine。成功路径也只读取 limit 字节并恰好关闭一次。
func reviewFreezeReadCompileRepositoryLeafBoundedV1(ctx context.Context, reader io.ReadCloser, path string, limit int64) ([]byte, error) {
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
			return nil, fmt.Errorf("repository leaf context during read path=%q: %w; close=%v", path, ctx.Err(), closeErr)
		}
		return nil, fmt.Errorf("repository leaf context during read path=%q: %w", path, ctx.Err())
	case read := <-result:
		closeErr := reader.Close()
		if read.err != nil {
			return nil, fmt.Errorf("repository leaf read path=%q: %w", path, read.err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("repository leaf close path=%q: %w", path, closeErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("repository leaf context after read path=%q: %w", path, err)
		}
		return read.raw, nil
	}
}

// reviewFreezeCompileRepositoryLeafGitBlobSHAV1 按 Git blob object framing 重算 SHA-1：
// `blob <decimal-byte-length>\x00<raw>`，不是对 raw 直接执行 SHA-1。
func reviewFreezeCompileRepositoryLeafGitBlobSHAV1(raw []byte) string {
	hasher := sha1.New()
	_, _ = io.WriteString(hasher, "blob ")
	_, _ = io.WriteString(hasher, strconv.FormatInt(int64(len(raw)), 10))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(raw)
	return hex.EncodeToString(hasher.Sum(nil))
}

// reviewFreezeCompileRepositoryLeafFixtureV1 使用当前 14 个真实 repository 文件只构造
// 测试 bytes/metadata；生产 verifier 从不读取 workspace，也不把 fixture 当作 tree/commit 证明。
type reviewFreezeCompileRepositoryLeafFixtureV1 struct {
	SnapshotRaw []byte
	Statement   reviewFreezeValidatorCompileAttestationV1
	Listing     []reviewFreezeCompileRepositoryLeafListEntryV1
	Materials   map[string][]byte
}

func reviewFreezeCompileRepositoryLeafFixtureRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository leaf fixture source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "agent", "go.mod")); err != nil {
		t.Fatalf("resolve repository root %q: %v", root, err)
	}
	return root
}

func reviewFreezeCompileRepositoryLeafFixtureNewV1(t *testing.T) reviewFreezeCompileRepositoryLeafFixtureV1 {
	t.Helper()
	root := reviewFreezeCompileRepositoryLeafFixtureRootV1(t)
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	paths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	if len(paths) != reviewFreezeCompileRepositoryLeafCountV1 {
		t.Fatalf("repository leaf fixture path count=%d want=%d", len(paths), reviewFreezeCompileRepositoryLeafCountV1)
	}

	registeredSHA := make(map[string]string, len(paths))
	registeredSHA[statement.Subject.ContractManifestPath] = reviewFreezeCompileInputSnapshotManifestSHA256V1
	for _, binding := range reviewFreezeCompileInputSnapshotManifestBindingsV1() {
		registeredSHA[binding.Path] = binding.SHA256
	}
	repositoryFiles := make([]reviewFreezeCompileInputSnapshotRepoFileV1, 0, len(paths))
	listing := make([]reviewFreezeCompileRepositoryLeafListEntryV1, 0, len(paths))
	materials := make(map[string][]byte, len(paths))
	byPath := make(map[string]reviewFreezeCompileInputSnapshotRepoFileV1, len(paths))
	for _, path := range paths {
		hostPath := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(hostPath)
		if err != nil {
			t.Fatalf("lstat repository leaf fixture %q: %v", path, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			t.Fatalf("repository leaf fixture 必须是普通非 symlink 文件 path=%q mode=%v", path, info.Mode())
		}
		raw, err := os.ReadFile(hostPath)
		if err != nil {
			t.Fatalf("read repository leaf fixture %q: %v", path, err)
		}
		metadata := reviewFreezeCompileInputSnapshotRepoFileV1{
			Path:       path,
			Mode:       reviewFreezeCompileInputSnapshotRepositoryFileModeV1,
			GitBlobSHA: reviewFreezeCompileRepositoryLeafGitBlobSHAV1(raw),
			SHA256:     reviewFreezeSHA256V1(raw),
			SizeBytes:  int64(len(raw)),
		}
		if wantSHA, exists := registeredSHA[path]; !exists || metadata.SHA256 != wantSHA {
			if path == statement.Subject.ContractManifestPath {
				t.Fatalf("fixed manifest SHA mismatch path=%q actual=%q registered=%q", path, metadata.SHA256, wantSHA)
			}
			t.Fatalf("registered repository SHA mismatch path=%q actual=%q registered=%q", path, metadata.SHA256, wantSHA)
		}
		repositoryFiles = append(repositoryFiles, metadata)
		listing = append(listing, reviewFreezeCompileRepositoryLeafListEntryV1{Path: path, Mode: info.Mode()})
		materials[path] = append([]byte(nil), raw...)
		byPath[path] = metadata
	}
	// 反转 List 顺序，证明 resolver 的 Open 顺序只由 strict snapshot 决定。
	for left, right := 0, len(listing)-1; left < right; left, right = left+1, right-1 {
		listing[left], listing[right] = listing[right], listing[left]
	}

	statement.Subject.GoModSHA256 = byPath["agent/go.mod"].SHA256
	statement.Subject.GoSumSHA256 = byPath["agent/go.sum"].SHA256
	statement.Subject.ContractManifestSHA256 = byPath[statement.Subject.ContractManifestPath].SHA256
	validatorSourceTreeSHA, err := reviewFreezeCompileInputSnapshotSourceTreeSHA256V1(byPath)
	if err != nil {
		t.Fatalf("derive real repository validator source tree: %v", err)
	}
	statement.Subject.ValidatorSourceTreeSHA256 = validatorSourceTreeSHA
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
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(snapshotRaw, &statement)
	return reviewFreezeCompileRepositoryLeafFixtureV1{
		SnapshotRaw: snapshotRaw,
		Statement:   statement,
		Listing:     listing,
		Materials:   materials,
	}
}

// reviewFreezeCompileRepositoryLeafLoaderFixtureV1 是内存 loader；它记录 List/Open 次数，
// 并允许测试注入内容、类型、路径、错误和 context 竞态，不改变 production verifier 逻辑。
type reviewFreezeCompileRepositoryLeafLoaderFixtureV1 struct {
	listing          []reviewFreezeCompileRepositoryLeafListEntryV1
	materials        map[string][]byte
	overrides        map[string][]byte
	openErrors       map[string]error
	openErrorReaders map[string]*reviewFreezeCompileRepositoryLeafTrackingReaderV1
	openModeOverride map[string]fs.FileMode
	openPathOverride map[string]string
	nilReaders       map[string]bool
	readerOverrides  map[string]io.ReadCloser
	listErr          error
	afterList        func()
	afterOpen        map[string]func()
	listCalls        int
	openCalls        map[string]int
}

type reviewFreezeCompileRepositoryLeafTrackingReaderV1 struct {
	io.Reader
	closed bool
}

func (reader *reviewFreezeCompileRepositoryLeafTrackingReaderV1) Close() error {
	reader.closed = true
	return nil
}

func reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture reviewFreezeCompileRepositoryLeafFixtureV1) *reviewFreezeCompileRepositoryLeafLoaderFixtureV1 {
	listing := append([]reviewFreezeCompileRepositoryLeafListEntryV1(nil), fixture.Listing...)
	materials := make(map[string][]byte, len(fixture.Materials))
	for path, raw := range fixture.Materials {
		materials[path] = append([]byte(nil), raw...)
	}
	return &reviewFreezeCompileRepositoryLeafLoaderFixtureV1{
		listing:          listing,
		materials:        materials,
		overrides:        make(map[string][]byte),
		openErrors:       make(map[string]error),
		openErrorReaders: make(map[string]*reviewFreezeCompileRepositoryLeafTrackingReaderV1),
		openModeOverride: make(map[string]fs.FileMode),
		openPathOverride: make(map[string]string),
		nilReaders:       make(map[string]bool),
		readerOverrides:  make(map[string]io.ReadCloser),
		afterOpen:        make(map[string]func()),
		openCalls:        make(map[string]int),
	}
}

func (loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) List(ctx context.Context) ([]reviewFreezeCompileRepositoryLeafListEntryV1, error) {
	loader.listCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if loader.listErr != nil {
		return nil, loader.listErr
	}
	listed := append([]reviewFreezeCompileRepositoryLeafListEntryV1(nil), loader.listing...)
	if loader.afterList != nil {
		loader.afterList()
	}
	return listed, nil
}

func (loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) Open(ctx context.Context, path string) (reviewFreezeCompileRepositoryLeafOpenedV1, error) {
	loader.openCalls[path]++
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileRepositoryLeafOpenedV1{}, err
	}
	if openErr := loader.openErrors[path]; openErr != nil {
		opened := reviewFreezeCompileRepositoryLeafOpenedV1{}
		if reader := loader.openErrorReaders[path]; reader != nil {
			opened.Reader = reader
		}
		return opened, openErr
	}
	if loader.afterOpen[path] != nil {
		loader.afterOpen[path]()
	}
	openedPath := path
	if override, exists := loader.openPathOverride[path]; exists {
		openedPath = override
	}
	openedMode := fs.FileMode(0o644)
	if override, exists := loader.openModeOverride[path]; exists {
		openedMode = override
	}
	if loader.nilReaders[path] {
		return reviewFreezeCompileRepositoryLeafOpenedV1{Path: openedPath, Mode: openedMode}, nil
	}
	if reader, exists := loader.readerOverrides[path]; exists {
		return reviewFreezeCompileRepositoryLeafOpenedV1{Path: openedPath, Mode: openedMode, Reader: reader}, nil
	}
	raw, exists := loader.materials[path]
	if override, overridden := loader.overrides[path]; overridden {
		raw, exists = override, true
	}
	if !exists {
		return reviewFreezeCompileRepositoryLeafOpenedV1{Path: openedPath, Mode: openedMode}, nil
	}
	return reviewFreezeCompileRepositoryLeafOpenedV1{
		Path:   openedPath,
		Mode:   openedMode,
		Reader: io.NopCloser(bytes.NewReader(raw)),
	}, nil
}

func reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) int {
	total := 0
	for _, calls := range loader.openCalls {
		total += calls
	}
	return total
}

type reviewFreezeCompileRepositoryLeafBlockingReaderV1 struct {
	started chan struct{}
	unblock chan struct{}
}

func reviewFreezeCompileRepositoryLeafBlockingReaderNewV1() *reviewFreezeCompileRepositoryLeafBlockingReaderV1 {
	return &reviewFreezeCompileRepositoryLeafBlockingReaderV1{started: make(chan struct{}), unblock: make(chan struct{})}
}

func (reader *reviewFreezeCompileRepositoryLeafBlockingReaderV1) Read([]byte) (int, error) {
	close(reader.started)
	<-reader.unblock
	return 0, io.EOF
}

func (reader *reviewFreezeCompileRepositoryLeafBlockingReaderV1) Close() error {
	close(reader.unblock)
	return nil
}

func reviewFreezeCompileRepositoryLeafMutatedSnapshotV1(
	t *testing.T,
	fixture reviewFreezeCompileRepositoryLeafFixtureV1,
	mutate func(*reviewFreezeCompileInputSnapshotV1),
) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
	t.Helper()
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, fixture.SnapshotRaw)
	mutate(&snapshot)
	raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, snapshot)
	statement := fixture.Statement
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &statement)
	return raw, statement
}

func reviewFreezeCompileRepositoryLeafListingIndexV1(t *testing.T, listing []reviewFreezeCompileRepositoryLeafListEntryV1, path string) int {
	t.Helper()
	for index := range listing {
		if listing[index].Path == path {
			return index
		}
	}
	t.Fatalf("repository leaf listing path not found=%q", path)
	return -1
}

func TestW2ReviewFreezeCompileRepositoryLeafBundleV1(t *testing.T) {
	fixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
	verified, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, loader)
	if err != nil {
		t.Fatalf("valid repository leaf bundle rejected: %v", err)
	}
	if loader.listCalls != 1 {
		t.Fatalf("List calls=%d want=1", loader.listCalls)
	}
	if got := reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader); got != reviewFreezeCompileRepositoryLeafCountV1 {
		t.Fatalf("Open calls=%d want=%d", got, reviewFreezeCompileRepositoryLeafCountV1)
	}
	wantPaths := reviewFreezeCompileInputSnapshotRepositoryPathsV1()
	if !reflect.DeepEqual(verified.Paths(), wantPaths) {
		t.Fatalf("verified paths=%v want=%v", verified.Paths(), wantPaths)
	}
	wantTotal := int64(0)
	for _, path := range wantPaths {
		if loader.openCalls[path] != 1 {
			t.Fatalf("path=%q Open calls=%d want=1", path, loader.openCalls[path])
		}
		wantRaw := append([]byte(nil), fixture.Materials[path]...)
		metadata, first, exists := verified.Leaf(path)
		if !exists || metadata.Path != path || !bytes.Equal(first, wantRaw) {
			t.Fatalf("verified leaf mismatch path=%q metadata=%+v", path, metadata)
		}
		wantTotal += int64(len(wantRaw))
		first[0] ^= 0xff
		loader.materials[path][0] ^= 0xff
		_, second, exists := verified.Leaf(path)
		if !exists || !bytes.Equal(second, wantRaw) {
			t.Fatalf("verified leaf 不是 immutable copy path=%q", path)
		}
	}
	if verified.TotalBytes() != wantTotal {
		t.Fatalf("verified total=%d want=%d", verified.TotalBytes(), wantTotal)
	}
	if got := reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader); got != reviewFreezeCompileRepositoryLeafCountV1 {
		t.Fatalf("immutable reuse 触发额外 Open calls=%d", got)
	}
	scope := verified.Scope()
	wantGaps := []string{reviewFreezeCompileRepositoryLeafBaseTreeGapV1, reviewFreezeCompileRepositoryLeafCommitAncestryGapV1}
	if scope.VerifiedClaim != reviewFreezeCompileRepositoryLeafVerifiedClaimV1 || scope.FormalFreezeStatus != reviewFreezeCompileRepositoryLeafFormalFreezeStatusV1 || !reflect.DeepEqual(scope.OpenGaps, wantGaps) {
		t.Fatalf("repository leaf verification scope 漂移=%+v", scope)
	}
	scope.OpenGaps[0] = "forged"
	if !reflect.DeepEqual(verified.Scope().OpenGaps, wantGaps) {
		t.Fatal("verification scope open gaps 不是 immutable copy")
	}
}

func TestW2ReviewFreezeCompileRepositoryLeafGitBlobFramingV1(t *testing.T) {
	if got, want := reviewFreezeCompileRepositoryLeafGitBlobSHAV1(nil), "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"; got != want {
		t.Fatalf("empty Git blob SHA=%q want=%q", got, want)
	}
	if got := reviewFreezeCompileRepositoryLeafGitBlobSHAV1(nil); got == "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Fatal("Git blob SHA 错误退化为 raw SHA-1")
	}
}

func TestW2ReviewFreezeCompileRepositoryLeafListingAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	wantFirst := reviewFreezeCompileInputSnapshotRepositoryPathsV1()[0]
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileRepositoryLeafLoaderFixtureV1)
		want   string
	}{
		{name: "missing", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			loader.listing = loader.listing[:len(loader.listing)-1]
		}, want: "missing listed path"},
		{name: "extra", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			loader.listing[0].Path = "extra/repository-leaf.txt"
		}, want: "extra listed path"},
		{name: "duplicate", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			loader.listing[0].Path = loader.listing[1].Path
		}, want: "duplicate listed path"},
		{name: "list budget", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			loader.listing = append(loader.listing, reviewFreezeCompileRepositoryLeafListEntryV1{Path: "extra/leaf.txt", Mode: 0o644})
		}, want: "extra/list entry budget"},
		{name: "unsafe", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			loader.listing[0].Path = "../escape"
		}, want: "不安全路径"},
		{name: "symlink", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			index := reviewFreezeCompileRepositoryLeafListingIndexV1(t, loader.listing, wantFirst)
			loader.listing[index].Mode = fs.ModeSymlink | 0o777
		}, want: "symlink"},
		{name: "non-regular", mutate: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) {
			index := reviewFreezeCompileRepositoryLeafListingIndexV1(t, loader.listing, wantFirst)
			loader.listing[index].Mode = fs.ModeDir | 0o755
		}, want: "non-regular"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
			test.mutate(loader)
			_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
			if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
				t.Fatalf("invalid listing calls List/Open=%d/%d want=1/0", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
			}
		})
	}
}

func TestW2ReviewFreezeCompileRepositoryLeafContentAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	firstPath := reviewFreezeCompileInputSnapshotRepositoryPathsV1()[0]
	tests := []struct {
		name          string
		prepare       func(*reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1)
		want          string
		wantOpenCalls int
	}{
		{name: "missing content", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			delete(loader.materials, firstPath)
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "missing content", wantOpenCalls: 1},
		{name: "nil reader", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.nilReaders[firstPath] = true
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "missing content", wantOpenCalls: 1},
		{name: "open error", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.openErrors[firstPath] = errors.New("injected open failure")
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "injected open failure", wantOpenCalls: 1},
		{name: "open error closes returned reader", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.openErrors[firstPath] = errors.New("injected open failure with reader")
			loader.openErrorReaders[firstPath] = &reviewFreezeCompileRepositoryLeafTrackingReaderV1{Reader: bytes.NewReader(fixture.Materials[firstPath])}
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "injected open failure with reader", wantOpenCalls: 1},
		{name: "open path substitution", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.openPathOverride[firstPath] = "other/path"
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "opened path mismatch", wantOpenCalls: 1},
		{name: "open-time symlink", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.openModeOverride[firstPath] = fs.ModeSymlink | 0o777
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "opened non-regular/symlink", wantOpenCalls: 1},
		{name: "truncated", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.overrides[firstPath] = append([]byte(nil), fixture.Materials[firstPath][:len(fixture.Materials[firstPath])-1]...)
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "truncated", wantOpenCalls: 1},
		{name: "oversized", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			loader.overrides[firstPath] = append(append([]byte(nil), fixture.Materials[firstPath]...), 0)
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "oversized", wantOpenCalls: 1},
		{name: "SHA-256 drift", prepare: func(loader *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			drifted := append([]byte(nil), fixture.Materials[firstPath]...)
			drifted[0] ^= 0xff
			loader.overrides[firstPath] = drifted
			return fixture.SnapshotRaw, fixture.Statement
		}, want: "SHA-256 drift", wantOpenCalls: 1},
		{name: "git blob drift", prepare: func(_ *reviewFreezeCompileRepositoryLeafLoaderFixtureV1) ([]byte, reviewFreezeValidatorCompileAttestationV1) {
			return reviewFreezeCompileRepositoryLeafMutatedSnapshotV1(t, fixture, func(snapshot *reviewFreezeCompileInputSnapshotV1) {
				snapshot.RepositoryFiles[0].GitBlobSHA = strings.Repeat("f", 40)
			})
		}, want: "git blob SHA-1 drift", wantOpenCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
			snapshotRaw, statement := test.prepare(loader)
			_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), snapshotRaw, statement, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
			if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != test.wantOpenCalls {
				t.Fatalf("content failure calls List/Open=%d/%d want=1/%d", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader), test.wantOpenCalls)
			}
			for path, calls := range loader.openCalls {
				if calls > 1 {
					t.Fatalf("path=%q Open calls=%d want<=1", path, calls)
				}
			}
			if reader := loader.openErrorReaders[firstPath]; reader != nil && !reader.closed {
				t.Fatal("Open 返回 error+Reader 时未关闭 reader")
			}
		})
	}
}

func TestW2ReviewFreezeCompileRepositoryLeafPhaseAndBudgetAdversarialV1(t *testing.T) {
	fixture := reviewFreezeCompileRepositoryLeafFixtureNewV1(t)
	t.Run("invalid snapshot before List", func(t *testing.T) {
		snapshotRaw, statement := reviewFreezeCompileRepositoryLeafMutatedSnapshotV1(t, fixture, func(snapshot *reviewFreezeCompileInputSnapshotV1) {
			snapshot.RepositoryFiles[0].SHA256 = reviewFreezeSHA256V1([]byte("semantic drift"))
		})
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), snapshotRaw, statement, loader)
		if err == nil || !strings.Contains(err.Error(), "strict snapshot") {
			t.Fatalf("invalid snapshot error=%v", err)
		}
		if loader.listCalls != 0 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
			t.Fatalf("invalid snapshot touched loader List/Open=%d/%d", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("declared total budget before List", func(t *testing.T) {
		validatorSources := make(map[string]struct{})
		for _, path := range reviewFreezeCompileInputSnapshotValidatorSourcePathsV1() {
			validatorSources[path] = struct{}{}
		}
		snapshotRaw, statement := reviewFreezeCompileRepositoryLeafMutatedSnapshotV1(t, fixture, func(snapshot *reviewFreezeCompileInputSnapshotV1) {
			changed := 0
			for index := range snapshot.RepositoryFiles {
				if _, validatorSource := validatorSources[snapshot.RepositoryFiles[index].Path]; validatorSource {
					continue
				}
				snapshot.RepositoryFiles[index].SizeBytes = reviewFreezeCompileInputSnapshotMaxRepositoryFileV1
				changed++
				if changed == 5 {
					break
				}
			}
		})
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), snapshotRaw, statement, loader)
		if err == nil || !strings.Contains(err.Error(), "declared total bytes 超出预算") {
			t.Fatalf("total budget error=%v", err)
		}
		if loader.listCalls != 0 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
			t.Fatalf("budget failure touched loader List/Open=%d/%d", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("pre-canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(ctx, fixture.SnapshotRaw, fixture.Statement, loader)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled context error=%v", err)
		}
		if loader.listCalls != 0 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
			t.Fatalf("pre-canceled context touched loader List/Open=%d/%d", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("context canceled after List", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		loader.afterList = cancel
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(ctx, fixture.SnapshotRaw, fixture.Statement, loader)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("after-List context error=%v", err)
		}
		if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
			t.Fatalf("after-List cancellation calls List/Open=%d/%d want=1/0", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("context canceled after Open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		firstPath := reviewFreezeCompileInputSnapshotRepositoryPathsV1()[0]
		loader.afterOpen[firstPath] = cancel
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(ctx, fixture.SnapshotRaw, fixture.Statement, loader)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("after-Open context error=%v", err)
		}
		if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 1 {
			t.Fatalf("after-Open cancellation calls List/Open=%d/%d want=1/1", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("context cancels blocking Read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		firstPath := reviewFreezeCompileInputSnapshotRepositoryPathsV1()[0]
		blocking := reviewFreezeCompileRepositoryLeafBlockingReaderNewV1()
		loader.readerOverrides[firstPath] = blocking
		cancelled := make(chan struct{})
		go func() {
			<-blocking.started
			cancel()
			close(cancelled)
		}()
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(ctx, fixture.SnapshotRaw, fixture.Statement, loader)
		<-cancelled
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("blocking Read cancellation error=%v", err)
		}
		if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 1 {
			t.Fatalf("blocking Read cancellation calls List/Open=%d/%d want=1/1", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})

	t.Run("List error", func(t *testing.T) {
		loader := reviewFreezeCompileRepositoryLeafLoaderFixtureNewV1(fixture)
		loader.listErr = errors.New("injected List failure")
		_, err := reviewFreezeVerifyCompileRepositoryLeafBundleV1(context.Background(), fixture.SnapshotRaw, fixture.Statement, loader)
		if err == nil || !strings.Contains(err.Error(), "injected List failure") {
			t.Fatalf("List error=%v", err)
		}
		if loader.listCalls != 1 || reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader) != 0 {
			t.Fatalf("List error calls List/Open=%d/%d want=1/0", loader.listCalls, reviewFreezeCompileRepositoryLeafLoaderOpenCountV1(loader))
		}
	})
}
