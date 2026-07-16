package reviewfreeze_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	xtexttransform "golang.org/x/text/transform"
)

const (
	reviewFreezeCompileModuleLeafKindRegularV1 = "regular"
	reviewFreezeCompileModuleLeafMaxTotalV1    = 80 << 20
	reviewFreezeCompileModuleInfoMinimalRawV1  = `{"Version":"v0.34.0","Time":"2026-02-09T16:14:29Z"}`
	reviewFreezeCompileModuleInfoOriginRawV1   = `{"Version":"v0.34.0","Time":"2026-02-09T16:14:29Z","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text","Hash":"817fba9abd337b4d9097b10c61a540c74feaaeff","Ref":"refs/tags/v0.34.0"}}`
)

// reviewFreezeCanonicalizeCompileModuleInfoFixtureV1 只在宿主 Go cache 进入隔离测试
// fixture 时识别两种已复现的 proxy 原始表示，并统一输出既有 51-byte canonical leaf。
// v1 snapshot/validator 的 accepted-set 因而保持不变；任意第三种 JSON 表示都失败关闭。
func reviewFreezeCanonicalizeCompileModuleInfoFixtureV1(raw []byte) ([]byte, error) {
	switch string(raw) {
	case reviewFreezeCompileModuleInfoMinimalRawV1, reviewFreezeCompileModuleInfoOriginRawV1:
		return []byte(reviewFreezeCompileModuleInfoMinimalRawV1), nil
	default:
		return nil, fmt.Errorf("compile module fixture .info host raw 未命中 exact acquisition allowlist")
	}
}

// reviewFreezeCompileModuleLeafListedV1 是 loader 在打开文件前提供的不可变对象身份。
// Identity 必须来自同一隔离 module cache 的稳定对象标识，并在 Open 后再次返回；resolver
// 不解释其格式，只用它关闭 list/open 之间替换对象的 TOCTOU 窗口。
type reviewFreezeCompileModuleLeafListedV1 struct {
	Path     string
	Mode     string
	Kind     string
	Identity string
}

type reviewFreezeCompileModuleLeafOpenedV1 struct {
	Path     string
	Mode     string
	Kind     string
	Identity string
	Reader   io.ReadCloser
}

// reviewFreezeCompileModuleLeafLoaderV1 明确拆分 exact listing 和按 path 打开。实现不得把
// List 返回的摘要当作内容证明；每个声明 path 仍必须恰好 Open 一次并重算原始字节摘要。
// Open 返回的 Reader 必须保证 Close 可中断 Read；resolver 在 bounded Read 期间监听 ctx，
// 取消时主动 Close，并关闭所有已经返回的 Reader。
type reviewFreezeCompileModuleLeafLoaderV1 interface {
	List(context.Context) ([]reviewFreezeCompileModuleLeafListedV1, error)
	Open(context.Context, string) (reviewFreezeCompileModuleLeafOpenedV1, error)
}

// reviewFreezeCompileModuleLeafBundleV1 只保存已经通过 module-cache leaf identity/internal
// semantics admission 的 15 个 leaf。它不是 Git repository、Go toolchain、父级 artifact
// ref 或签名 authority；
// 这些边界必须继续由各自 verifier 证明。
type reviewFreezeCompileModuleLeafBundleV1 struct {
	paths []string
	files map[string][]byte
}

func (bundle *reviewFreezeCompileModuleLeafBundleV1) Paths() []string {
	if bundle == nil {
		return nil
	}
	return append([]string(nil), bundle.paths...)
}

func (bundle *reviewFreezeCompileModuleLeafBundleV1) Bytes(path string) ([]byte, bool) {
	if bundle == nil {
		return nil, false
	}
	raw, exists := bundle.files[path]
	if !exists {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

// reviewFreezeResolveCompileModuleLeafBundleV1 是 input snapshot 严格 JSON/typed 校验之后的
// 第二阶段 resolver。它在接触内容前复核 module metadata exact-set，再把 List 身份、Open
// 身份、实际 size/SHA256 和 statement module 语义收敛为一个不可变 bundle。
func reviewFreezeResolveCompileModuleLeafBundleV1(
	ctx context.Context,
	snapshotRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
	loader reviewFreezeCompileModuleLeafLoaderV1,
) (*reviewFreezeCompileModuleLeafBundleV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile module leaf context 为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile module leaf context: %w", err)
	}
	if loader == nil {
		return nil, fmt.Errorf("compile module leaf loader 为空")
	}
	frozenSnapshotRaw := append([]byte(nil), snapshotRaw...)
	if err := reviewFreezeValidateCompileInputSnapshotJSONV1(frozenSnapshotRaw, statement); err != nil {
		return nil, fmt.Errorf("compile module leaf strict snapshot raw gate: %w", err)
	}
	var snapshot reviewFreezeCompileInputSnapshotV1
	if err := json.Unmarshal(frozenSnapshotRaw, &snapshot); err != nil {
		return nil, fmt.Errorf("compile module leaf 解码已验证 snapshot raw: %w", err)
	}
	if !reflect.DeepEqual(snapshot.ExternalModules, statement.ExternalModules) {
		return nil, fmt.Errorf("compile module leaf snapshot/statement external_modules 不一致")
	}
	if len(statement.ExternalModules) != 1 {
		return nil, fmt.Errorf("compile module leaf statement external_modules exact-set 长度=%d want=1", len(statement.ExternalModules))
	}
	if err := reviewFreezeValidateCompileAttestationModuleV1(statement.ExternalModules[0]); err != nil {
		return nil, fmt.Errorf("compile module leaf fixed statement module gate: %w", err)
	}
	if err := reviewFreezeValidateCompileInputSnapshotModuleFilesV1(snapshot.ModuleCacheFiles, statement.ExternalModules); err != nil {
		return nil, fmt.Errorf("compile module leaf metadata gate: %w", err)
	}

	var declaredTotal int64
	for _, file := range snapshot.ModuleCacheFiles {
		if file.SizeBytes > reviewFreezeCompileModuleLeafMaxTotalV1-declaredTotal {
			return nil, fmt.Errorf("compile module leaf 声明总字节超限 path=%q total>%d", file.Path, reviewFreezeCompileModuleLeafMaxTotalV1)
		}
		declaredTotal += file.SizeBytes
	}

	listed, err := loader.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile module leaf exact List: %w", err)
	}
	if len(listed) != len(snapshot.ModuleCacheFiles) {
		return nil, fmt.Errorf("compile module leaf List exact-set 长度=%d want=%d", len(listed), len(snapshot.ModuleCacheFiles))
	}
	listedByPath := make(map[string]reviewFreezeCompileModuleLeafListedV1, len(listed))
	for index, entry := range listed {
		want := snapshot.ModuleCacheFiles[index]
		wantActualMode, exists := reviewFreezeCompileInputSnapshotModuleModeV1(want.Path)
		if !exists {
			return nil, fmt.Errorf("compile module leaf actual mode policy 未覆盖=%q", want.Path)
		}
		if entry.Path != want.Path {
			return nil, fmt.Errorf("compile module leaf List 未按 canonical exact-set 排序 index=%d path=%q want=%q", index, entry.Path, want.Path)
		}
		if _, duplicate := listedByPath[entry.Path]; duplicate {
			return nil, fmt.Errorf("compile module leaf List 重复 path=%q", entry.Path)
		}
		if entry.Kind != reviewFreezeCompileModuleLeafKindRegularV1 {
			return nil, fmt.Errorf("compile module leaf List symlink-or-nonregular path=%q kind=%q", entry.Path, entry.Kind)
		}
		if entry.Mode != wantActualMode {
			return nil, fmt.Errorf("compile module leaf List actual mode=%q path=%q want=%q", entry.Mode, entry.Path, wantActualMode)
		}
		if entry.Identity == "" {
			return nil, fmt.Errorf("compile module leaf List identity 为空 path=%q", entry.Path)
		}
		listedByPath[entry.Path] = entry
	}

	verified := make(map[string][]byte, len(snapshot.ModuleCacheFiles))
	paths := make([]string, 0, len(snapshot.ModuleCacheFiles))
	for _, file := range snapshot.ModuleCacheFiles {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compile module leaf context before Open %q: %w", file.Path, err)
		}
		entry := listedByPath[file.Path]
		raw, err := reviewFreezeReadCompileModuleLeafV1(ctx, loader, entry, file)
		if err != nil {
			return nil, err
		}
		verified[file.Path] = raw
		paths = append(paths, file.Path)
	}
	if err := reviewFreezeValidateCompileModuleLeafSemanticsV1(snapshot, statement, verified); err != nil {
		return nil, err
	}
	return &reviewFreezeCompileModuleLeafBundleV1{paths: paths, files: verified}, nil
}

func reviewFreezeReadCompileModuleLeafV1(
	ctx context.Context,
	loader reviewFreezeCompileModuleLeafLoaderV1,
	listed reviewFreezeCompileModuleLeafListedV1,
	want reviewFreezeCompileInputSnapshotModuleFileV1,
) ([]byte, error) {
	wantActualMode, exists := reviewFreezeCompileInputSnapshotModuleModeV1(want.Path)
	if !exists {
		return nil, fmt.Errorf("compile module leaf actual mode policy 未覆盖=%q", want.Path)
	}
	opened, err := loader.Open(ctx, want.Path)
	if err != nil {
		if opened.Reader != nil {
			closeErr := opened.Reader.Close()
			if closeErr != nil {
				return nil, fmt.Errorf("compile module leaf Open %q: %v; error reader Close: %v", want.Path, err, closeErr)
			}
		}
		return nil, fmt.Errorf("compile module leaf Open %q: %w", want.Path, err)
	}
	if opened.Reader == nil {
		return nil, fmt.Errorf("compile module leaf Open reader 为空 path=%q", want.Path)
	}
	closeWith := func(cause error) ([]byte, error) {
		closeErr := opened.Reader.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("compile module leaf Close %q: %v (cause=%v)", want.Path, closeErr, cause)
		}
		return nil, cause
	}
	if err := ctx.Err(); err != nil {
		return closeWith(fmt.Errorf("compile module leaf context after Open %q: %w", want.Path, err))
	}
	if opened.Path != want.Path || opened.Path != listed.Path {
		return closeWith(fmt.Errorf("compile module leaf Open path drift=%q want=%q", opened.Path, want.Path))
	}
	if opened.Kind != reviewFreezeCompileModuleLeafKindRegularV1 || opened.Kind != listed.Kind {
		return closeWith(fmt.Errorf("compile module leaf Open symlink-or-nonregular path=%q kind=%q", want.Path, opened.Kind))
	}
	if opened.Mode != wantActualMode || opened.Mode != listed.Mode {
		return closeWith(fmt.Errorf("compile module leaf Open actual mode drift=%q path=%q want=%q", opened.Mode, want.Path, wantActualMode))
	}
	if opened.Identity == "" || opened.Identity != listed.Identity {
		return closeWith(fmt.Errorf("compile module leaf Open TOCTOU identity drift path=%q listed=%q opened=%q", want.Path, listed.Identity, opened.Identity))
	}
	raw, err := reviewFreezeReadCompileModuleLeafBoundedV1(ctx, opened.Reader, want.Path, want.SizeBytes+1)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != want.SizeBytes {
		return nil, fmt.Errorf("compile module leaf size=%d path=%q want=%d", len(raw), want.Path, want.SizeBytes)
	}
	if actual := reviewFreezeSHA256V1(raw); actual != want.SHA256 {
		return nil, fmt.Errorf("compile module leaf sha256=%q path=%q want=%q", actual, want.Path, want.SHA256)
	}
	return raw, nil
}

// reviewFreezeReadCompileModuleLeafBoundedV1 主动兑现 context 取消：读取最多 limit 字节，
// 若 Reader 阻塞则取消路径 Close 句柄并立即失败。loader 必须保证 Close 可中断 Read；
// 这也是 module leaf loader 接入真实文件句柄时的最小生命周期契约。
func reviewFreezeReadCompileModuleLeafBoundedV1(ctx context.Context, reader io.ReadCloser, path string, limit int64) ([]byte, error) {
	type readResult struct {
		raw []byte
		err error
	}
	result := make(chan readResult, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(reader, limit))
		result <- readResult{raw: raw, err: err}
	}()

	finish := func(read readResult) ([]byte, error) {
		closeErr := reader.Close()
		if read.err != nil {
			return nil, fmt.Errorf("compile module leaf Read %q: %w", path, read.err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("compile module leaf Close %q: %w", path, closeErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compile module leaf context after Read %q: %w", path, err)
		}
		return read.raw, nil
	}

	select {
	case read := <-result:
		return finish(read)
	case <-ctx.Done():
		// 若 Read 与取消同时完成，优先收割结果并走统一 Close/after-Read 检查。
		select {
		case read := <-result:
			return finish(read)
		default:
		}
		if closeErr := reader.Close(); closeErr != nil {
			return nil, fmt.Errorf("compile module leaf context during Read %q: %w; Close=%v", path, ctx.Err(), closeErr)
		}
		return nil, fmt.Errorf("compile module leaf context during Read %q: %w", path, ctx.Err())
	}
}

// reviewFreezeValidateCompileModuleLeafSemanticsV1 是 admission 内部的 byte-level helper；
// 它不返回可复用 bundle，也不替代上层 strict raw 与 fixed statement module gate。受控小 zip
// 只允许直接测试这里的失败分支，不能作为高层 resolver 正例。
func reviewFreezeValidateCompileModuleLeafSemanticsV1(
	snapshot reviewFreezeCompileInputSnapshotV1,
	statement reviewFreezeValidatorCompileAttestationV1,
	files map[string][]byte,
) error {
	if !reflect.DeepEqual(snapshot.ExternalModules, statement.ExternalModules) || len(statement.ExternalModules) != 1 {
		return fmt.Errorf("compile module leaf semantic external_modules exact binding 失败")
	}
	module := statement.ExternalModules[0]
	if module.ModulePath != reviewFreezeXTextModulePathV1 || module.Version != reviewFreezeXTextModuleVersionV1 {
		return fmt.Errorf("compile module leaf semantic module identity=%s@%s", module.ModulePath, module.Version)
	}
	infoPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.info"
	modPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.mod"
	zipPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.zip"
	zipHashPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.ziphash"
	materializedGoModPath := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod"

	wantInfo := []byte(`{"Version":"` + module.Version + `","Time":"` + reviewFreezeCompileInputSnapshotModuleInfoTimeV1 + `"}`)
	if !bytes.Equal(files[infoPath], wantInfo) {
		return fmt.Errorf("compile module leaf .info 非 canonical Version/Time")
	}
	var info struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}
	decoder := json.NewDecoder(bytes.NewReader(files[infoPath]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&info); err != nil || info.Version != module.Version || info.Time != reviewFreezeCompileInputSnapshotModuleInfoTimeV1 {
		return fmt.Errorf("compile module leaf .info 语义非法: %v", err)
	}
	if !bytes.Equal(files[zipHashPath], []byte(module.ModuleSum)) {
		return fmt.Errorf("compile module leaf .ziphash 不等于 exact module sum")
	}
	if module.HashDirBefore != module.ModuleSum || module.HashDirAfter != module.ModuleSum {
		return fmt.Errorf("compile module leaf HashDir pre/post 未绑定 module sum")
	}

	downloadGoMod := files[modPath]
	materializedGoMod := files[materializedGoModPath]
	if !bytes.Equal(downloadGoMod, materializedGoMod) {
		return fmt.Errorf("compile module leaf download .mod 与 materialized root go.mod 不一致")
	}
	if err := reviewFreezeValidateExternalModuleGoModV1(downloadGoMod, module.ModulePath); err != nil {
		return fmt.Errorf("compile module leaf download .mod: %w", err)
	}
	goModSum, err := reviewFreezeHash1V1([]string{"go.mod"}, map[string][]byte{"go.mod": downloadGoMod})
	if err != nil || !reviewFreezeEqualH1V1(goModSum, module.GoModSum) {
		return fmt.Errorf("compile module leaf .mod h1=%q want=%q err=%v", goModSum, module.GoModSum, err)
	}
	if reviewFreezeSHA256V1(downloadGoMod) != module.GoModSHA256 || int64(len(downloadGoMod)) != module.GoModSizeBytes ||
		reviewFreezeSHA256V1(materializedGoMod) != module.ZipRootGoModSHA256 || int64(len(materializedGoMod)) != module.ZipRootGoModSizeBytes {
		return fmt.Errorf("compile module leaf .mod/root go.mod statement metadata 不一致")
	}

	moduleZip := files[zipPath]
	if reviewFreezeSHA256V1(moduleZip) != module.ZipSHA256 || int64(len(moduleZip)) != module.ZipSizeBytes {
		return fmt.Errorf("compile module leaf .zip statement raw metadata 不一致")
	}
	resolved, moduleSum, err := reviewFreezeReadAndHashExternalModuleZipV1(moduleZip, module.ModulePath, module.Version)
	if err != nil {
		return fmt.Errorf("compile module leaf .zip resolver: %w", err)
	}
	if !reviewFreezeEqualH1V1(moduleSum, module.ModuleSum) {
		return fmt.Errorf("compile module leaf .zip h1=%q want=%q", moduleSum, module.ModuleSum)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(moduleZip), int64(len(moduleZip)))
	if err != nil {
		return fmt.Errorf("compile module leaf .zip metadata: %w", err)
	}
	var uncompressed int64
	for _, entry := range zipReader.File {
		if entry.UncompressedSize64 > uint64(^uint64(0)>>1)-uint64(uncompressed) {
			return fmt.Errorf("compile module leaf .zip uncompressed size overflow")
		}
		uncompressed += int64(entry.UncompressedSize64)
	}
	if len(zipReader.File) != module.ZipEntryCount || uncompressed != module.ZipUncompressedBytes {
		return fmt.Errorf("compile module leaf .zip entry/size metadata=%d/%d want=%d/%d", len(zipReader.File), uncompressed, module.ZipEntryCount, module.ZipUncompressedBytes)
	}
	zipRootGoMod, exists := resolved.files["go.mod"]
	if !exists || !bytes.Equal(zipRootGoMod, downloadGoMod) || !bytes.Equal(zipRootGoMod, materializedGoMod) {
		return fmt.Errorf("compile module leaf zip root go.mod 与 download/materialized bytes 不一致")
	}

	selected := make(map[string]struct{}, 10)
	selectedCount := 0
	for _, selectedPackage := range module.Packages {
		for _, source := range selectedPackage.Sources {
			if _, duplicate := selected[source.Path]; duplicate {
				return fmt.Errorf("compile module leaf selected source 重复=%q", source.Path)
			}
			selected[source.Path] = struct{}{}
			selectedCount++
			materializedPath := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/" + source.Path
			materialized, materializedExists := files[materializedPath]
			zipped, zipExists := resolved.files[source.Path]
			if !materializedExists || !zipExists {
				return fmt.Errorf("compile module leaf selected source 缺失 materialized/zip=%q", source.Path)
			}
			if reviewFreezeSHA256V1(materialized) != source.SHA256 {
				return fmt.Errorf("compile module leaf selected source statement digest 漂移=%q", source.Path)
			}
			if !bytes.Equal(materialized, zipped) {
				return fmt.Errorf("compile module leaf selected source zip/materialized bytes 不一致=%q", source.Path)
			}
		}
	}
	if selectedCount != 10 {
		return fmt.Errorf("compile module leaf selected source 数=%d want=10", selectedCount)
	}
	for _, file := range snapshot.ModuleCacheFiles {
		prefix := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/"
		if file.Path == materializedGoModPath || len(file.Path) <= len(prefix) || file.Path[:len(prefix)] != prefix {
			continue
		}
		relative := file.Path[len(prefix):]
		if _, exists := selected[relative]; !exists {
			return fmt.Errorf("compile module leaf materialized source 未被 statement 选择=%q", relative)
		}
	}
	return nil
}

type reviewFreezeCompileModuleLeafFixtureObjectV1 struct {
	path              string
	mode              string
	kind              string
	identity          string
	raw               []byte
	openErr           error
	readErr           error
	closeErr          error
	nilReader         bool
	readerWithOpenErr bool
	afterOpen         func()
	afterRead         func()
	readerOverride    io.ReadCloser
}

type reviewFreezeCompileModuleLeafFixtureLoaderV1 struct {
	listed       []reviewFreezeCompileModuleLeafListedV1
	objects      map[string]*reviewFreezeCompileModuleLeafFixtureObjectV1
	listErr      error
	listCalls    int
	openCalls    map[string]int
	closeCalls   map[string]int
	listContext  context.Context
	openContexts map[string][]context.Context
	beforeList   func()
}

func (loader *reviewFreezeCompileModuleLeafFixtureLoaderV1) List(ctx context.Context) ([]reviewFreezeCompileModuleLeafListedV1, error) {
	loader.listCalls++
	loader.listContext = ctx
	if loader.beforeList != nil {
		loader.beforeList()
	}
	if loader.listErr != nil {
		return nil, loader.listErr
	}
	return append([]reviewFreezeCompileModuleLeafListedV1(nil), loader.listed...), nil
}

func (loader *reviewFreezeCompileModuleLeafFixtureLoaderV1) Open(ctx context.Context, path string) (reviewFreezeCompileModuleLeafOpenedV1, error) {
	loader.openCalls[path]++
	loader.openContexts[path] = append(loader.openContexts[path], ctx)
	object, exists := loader.objects[path]
	if !exists {
		return reviewFreezeCompileModuleLeafOpenedV1{}, fmt.Errorf("fixture object 缺失=%q", path)
	}
	if object.openErr != nil && !object.readerWithOpenErr {
		return reviewFreezeCompileModuleLeafOpenedV1{}, object.openErr
	}
	opened := reviewFreezeCompileModuleLeafOpenedV1{
		Path:     object.path,
		Mode:     object.mode,
		Kind:     object.kind,
		Identity: object.identity,
	}
	if object.readerOverride != nil {
		opened.Reader = object.readerOverride
	} else if !object.nilReader {
		opened.Reader = &reviewFreezeCompileModuleLeafFixtureReaderV1{
			reader:    bytes.NewReader(append([]byte(nil), object.raw...)),
			readErr:   object.readErr,
			closeErr:  object.closeErr,
			afterRead: object.afterRead,
			onClose: func() {
				loader.closeCalls[path]++
			},
		}
	}
	if object.afterOpen != nil {
		object.afterOpen()
	}
	return opened, object.openErr
}

type reviewFreezeCompileModuleLeafBlockingReaderV1 struct {
	started chan struct{}
	unblock chan struct{}
	onClose func()
}

func reviewFreezeNewCompileModuleLeafBlockingReaderV1(onClose func()) *reviewFreezeCompileModuleLeafBlockingReaderV1 {
	return &reviewFreezeCompileModuleLeafBlockingReaderV1{started: make(chan struct{}), unblock: make(chan struct{}), onClose: onClose}
}

func (reader *reviewFreezeCompileModuleLeafBlockingReaderV1) Read([]byte) (int, error) {
	close(reader.started)
	<-reader.unblock
	return 0, io.EOF
}

func (reader *reviewFreezeCompileModuleLeafBlockingReaderV1) Close() error {
	close(reader.unblock)
	reader.onClose()
	return nil
}

type reviewFreezeCompileModuleLeafFixtureReaderV1 struct {
	reader    *bytes.Reader
	readErr   error
	closeErr  error
	afterRead func()
	onClose   func()
	closed    bool
	readDone  bool
}

func (reader *reviewFreezeCompileModuleLeafFixtureReaderV1) Read(raw []byte) (int, error) {
	defer func() {
		if !reader.readDone && reader.afterRead != nil {
			reader.readDone = true
			reader.afterRead()
		}
	}()
	if reader.readErr != nil {
		err := reader.readErr
		reader.readErr = nil
		return 0, err
	}
	return reader.reader.Read(raw)
}

func (reader *reviewFreezeCompileModuleLeafFixtureReaderV1) Close() error {
	if !reader.closed {
		reader.closed = true
		reader.onClose()
	}
	return reader.closeErr
}

type reviewFreezeCompileModuleLeafFixtureV1 struct {
	snapshot  reviewFreezeCompileInputSnapshotV1
	statement reviewFreezeValidatorCompileAttestationV1
	files     map[string][]byte
	loader    *reviewFreezeCompileModuleLeafFixtureLoaderV1
}

func (fixture *reviewFreezeCompileModuleLeafFixtureV1) snapshotRaw(t *testing.T) []byte {
	t.Helper()
	raw := reviewFreezeCompileInputSnapshotFixtureMarshalV1(t, fixture.snapshot)
	reviewFreezeCompileInputSnapshotFixtureBindRefsV1(raw, &fixture.statement)
	return raw
}

func reviewFreezeResolveCompileModuleLeafFixtureV1(
	t *testing.T,
	ctx context.Context,
	fixture *reviewFreezeCompileModuleLeafFixtureV1,
) (*reviewFreezeCompileModuleLeafBundleV1, error) {
	t.Helper()
	raw := fixture.snapshotRaw(t)
	return reviewFreezeResolveCompileModuleLeafBundleV1(ctx, raw, fixture.statement, fixture.loader)
}

func reviewFreezeNewCompileModuleLeafFixtureLoaderV1(
	t *testing.T,
	files map[string][]byte,
	identityPrefix string,
) *reviewFreezeCompileModuleLeafFixtureLoaderV1 {
	t.Helper()
	loader := &reviewFreezeCompileModuleLeafFixtureLoaderV1{
		objects:      make(map[string]*reviewFreezeCompileModuleLeafFixtureObjectV1, len(files)),
		openCalls:    make(map[string]int, len(files)),
		closeCalls:   make(map[string]int, len(files)),
		openContexts: make(map[string][]context.Context, len(files)),
	}
	for _, path := range reviewFreezeCompileInputSnapshotModulePathsV1() {
		mode, exists := reviewFreezeCompileInputSnapshotModuleModeV1(path)
		if !exists {
			t.Fatalf("module leaf loader mode policy 缺失=%q", path)
		}
		raw := files[path]
		identity := identityPrefix + ":" + path
		loader.listed = append(loader.listed, reviewFreezeCompileModuleLeafListedV1{
			Path:     path,
			Mode:     mode,
			Kind:     reviewFreezeCompileModuleLeafKindRegularV1,
			Identity: identity,
		})
		loader.objects[path] = &reviewFreezeCompileModuleLeafFixtureObjectV1{
			path:     path,
			mode:     mode,
			kind:     reviewFreezeCompileModuleLeafKindRegularV1,
			identity: identity,
			raw:      append([]byte(nil), raw...),
		}
	}
	return loader
}

// reviewFreezeNewCompileModuleLeafAdmissionFixtureV1 保留固定 statement/snapshot metadata，
// 供在首个 Open 前失败的 exact-list、resource 和 Open 生命周期对抗测试使用。它不是正例，
// 因而没有把未打开的占位 bytes 冒充真实 x/text material。
func reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t *testing.T) *reviewFreezeCompileModuleLeafFixtureV1 {
	t.Helper()
	snapshotRaw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, snapshotRaw)
	files := make(map[string][]byte, len(snapshot.ModuleCacheFiles))
	files[reviewFreezeCompileInputSnapshotModuleDownloadRootV1+"/v0.34.0.info"] = []byte(`{"Version":"v0.34.0","Time":"` + reviewFreezeCompileInputSnapshotModuleInfoTimeV1 + `"}`)
	loader := reviewFreezeNewCompileModuleLeafFixtureLoaderV1(t, files, "fixed-metadata-adversarial")
	return &reviewFreezeCompileModuleLeafFixtureV1{snapshot: snapshot, statement: statement, files: files, loader: loader}
}

func reviewFreezeCompileModuleLeafModuleCacheRootV1(t *testing.T) string {
	t.Helper()
	function := runtime.FuncForPC(reflect.ValueOf(xtexttransform.String).Pointer())
	if function != nil {
		file, _ := function.FileLine(function.Entry())
		moduleRoot := filepath.Dir(filepath.Dir(file))
		suffix := filepath.FromSlash(reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1)
		if strings.HasSuffix(moduleRoot, suffix) {
			candidate := strings.TrimSuffix(strings.TrimSuffix(moduleRoot, suffix), string(filepath.Separator))
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	if value := os.Getenv("GOMODCACHE"); value != "" {
		return value
	}
	if values := filepath.SplitList(os.Getenv("GOPATH")); len(values) > 0 && values[0] != "" {
		return filepath.Join(values[0], "pkg", "mod")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("locate x/text module cache: %v", err)
	}
	return filepath.Join(home, "go", "pkg", "mod")
}

// reviewFreezeNewRealCompileModuleLeafFixtureV1 从当前 Go 构建已经选中的固定
// golang.org/x/text@v0.34.0 cache 读取 15 项宿主对象与 mode；其中 14 个内容叶保留真实
// bytes，`.info` 在严格识别宿主 51/191-byte 表示后收敛为既有 canonical fixture leaf。
// import x/text/transform 保证 Go 在运行本测试前已取得同一固定 module，而非测试发起网络访问。
func reviewFreezeNewRealCompileModuleLeafFixtureV1(t *testing.T) *reviewFreezeCompileModuleLeafFixtureV1 {
	t.Helper()
	snapshotRaw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, snapshotRaw)
	cacheRoot := reviewFreezeCompileModuleLeafModuleCacheRootV1(t)
	files := make(map[string][]byte, len(snapshot.ModuleCacheFiles))
	for index := range snapshot.ModuleCacheFiles {
		leaf := &snapshot.ModuleCacheFiles[index]
		fullPath := filepath.Join(cacheRoot, filepath.FromSlash(leaf.Path))
		before, err := os.Lstat(fullPath)
		if err != nil {
			t.Fatalf("read fixed x/text material %q (run go mod download first): %v", fullPath, err)
		}
		if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("fixed x/text material 非普通文件=%q mode=%v", fullPath, before.Mode())
		}
		wantMode, exists := reviewFreezeCompileInputSnapshotModuleModeV1(leaf.Path)
		actualMode := fmt.Sprintf("%04o", before.Mode().Perm())
		if !exists || actualMode != wantMode {
			t.Fatalf("fixed x/text material mode=%q path=%q want=%q", actualMode, fullPath, wantMode)
		}
		if before.Size() <= 0 || before.Size() > reviewFreezeCompileInputSnapshotMaxModuleCacheFileV1 {
			t.Fatalf("fixed x/text material size=%d path=%q", before.Size(), fullPath)
		}
		hostRaw, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("read fixed x/text material %q: %v", fullPath, err)
		}
		after, err := os.Lstat(fullPath)
		if err != nil || !os.SameFile(before, after) || after.Size() != int64(len(hostRaw)) || after.Mode() != before.Mode() {
			t.Fatalf("fixed x/text material fixture TOCTOU path=%q before=%v after=%v err=%v", fullPath, before, after, err)
		}
		raw := hostRaw
		if leaf.Path == reviewFreezeCompileInputSnapshotModuleDownloadRootV1+"/v0.34.0.info" {
			raw, err = reviewFreezeCanonicalizeCompileModuleInfoFixtureV1(hostRaw)
			if err != nil {
				t.Fatalf("canonicalize fixed x/text .info fixture %q: %v", fullPath, err)
			}
		}
		files[leaf.Path] = raw
		leaf.Mode = wantMode
		leaf.SHA256 = reviewFreezeSHA256V1(raw)
		leaf.SizeBytes = int64(len(raw))
	}
	loader := reviewFreezeNewCompileModuleLeafFixtureLoaderV1(t, files, "real-fixed-x-text-v0.34.0")
	return &reviewFreezeCompileModuleLeafFixtureV1{snapshot: snapshot, statement: statement, files: files, loader: loader}
}

func reviewFreezeNewCompileModuleLeafFixtureV1(t *testing.T) *reviewFreezeCompileModuleLeafFixtureV1 {
	t.Helper()
	snapshotRaw, statement := reviewFreezeCompileInputSnapshotFixtureV1(t)
	snapshot := reviewFreezeCompileInputSnapshotFixtureDecodeV1(t, snapshotRaw)
	packages := reviewFreezeExpectedCompileAttestationExternalPackagesV1()
	selectedRaw := make(map[string][]byte, 10)
	for packageIndex := range packages {
		for sourceIndex := range packages[packageIndex].Sources {
			source := &packages[packageIndex].Sources[sourceIndex]
			packageName := "norm"
			if packageIndex == 0 {
				packageName = "transform"
			}
			raw := []byte("// controlled review-freeze source: " + source.Path + "\npackage " + packageName + "\n")
			selectedRaw[source.Path] = raw
			source.SHA256 = reviewFreezeSHA256V1(raw)
		}
	}
	goMod := []byte("module golang.org/x/text\n\ngo 1.24.0\n")
	prefix := reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1 + "/"
	entries := []reviewFreezeExternalModuleZipEntryFixtureV1{{name: prefix + "go.mod", raw: append([]byte(nil), goMod...)}}
	selectedPaths := make([]string, 0, len(selectedRaw))
	for path := range selectedRaw {
		selectedPaths = append(selectedPaths, path)
	}
	sort.Strings(selectedPaths)
	var uncompressed int64 = int64(len(goMod))
	for _, path := range selectedPaths {
		raw := selectedRaw[path]
		entries = append(entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + path, raw: append([]byte(nil), raw...)})
		uncompressed += int64(len(raw))
	}
	moduleZip := reviewFreezeWriteExternalModuleZipFixtureV1(t, entries)
	_, moduleSum, err := reviewFreezeReadAndHashExternalModuleZipV1(moduleZip, reviewFreezeXTextModulePathV1, reviewFreezeXTextModuleVersionV1)
	if err != nil {
		t.Fatalf("build controlled module zip: %v", err)
	}
	goModSum, err := reviewFreezeHash1V1([]string{"go.mod"}, map[string][]byte{"go.mod": goMod})
	if err != nil {
		t.Fatalf("build controlled go.mod h1: %v", err)
	}
	module := reviewFreezeCompileAttestationModuleV1{
		ModulePath:            reviewFreezeXTextModulePathV1,
		Version:               reviewFreezeXTextModuleVersionV1,
		ZipSHA256:             reviewFreezeSHA256V1(moduleZip),
		ZipSizeBytes:          int64(len(moduleZip)),
		ZipEntryCount:         len(entries),
		ZipUncompressedBytes:  uncompressed,
		ModuleSum:             moduleSum,
		GoModSHA256:           reviewFreezeSHA256V1(goMod),
		GoModSizeBytes:        int64(len(goMod)),
		GoModSum:              goModSum,
		ZipRootGoModSHA256:    reviewFreezeSHA256V1(goMod),
		ZipRootGoModSizeBytes: int64(len(goMod)),
		HashDirBefore:         moduleSum,
		HashDirAfter:          moduleSum,
		Packages:              packages,
	}
	statement.ExternalModules = []reviewFreezeCompileAttestationModuleV1{module}
	snapshot.ExternalModules = []reviewFreezeCompileAttestationModuleV1{module}
	files := map[string][]byte{
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.info":    []byte(`{"Version":"v0.34.0","Time":"` + reviewFreezeCompileInputSnapshotModuleInfoTimeV1 + `"}`),
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.mod":     append([]byte(nil), goMod...),
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.zip":     append([]byte(nil), moduleZip...),
		reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.ziphash": []byte(moduleSum),
		reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod":          append([]byte(nil), goMod...),
	}
	for path, raw := range selectedRaw {
		files[reviewFreezeCompileInputSnapshotModuleMaterializedV1+"/"+path] = append([]byte(nil), raw...)
	}
	snapshot.ModuleCacheFiles = make([]reviewFreezeCompileInputSnapshotModuleFileV1, 0, len(files))
	loader := &reviewFreezeCompileModuleLeafFixtureLoaderV1{
		objects:      make(map[string]*reviewFreezeCompileModuleLeafFixtureObjectV1, len(files)),
		openCalls:    make(map[string]int, len(files)),
		closeCalls:   make(map[string]int, len(files)),
		openContexts: make(map[string][]context.Context, len(files)),
	}
	for _, path := range reviewFreezeCompileInputSnapshotModulePathsV1() {
		raw, exists := files[path]
		if !exists {
			t.Fatalf("controlled module leaf fixture path 缺失=%q", path)
		}
		purpose, exists := reviewFreezeCompileInputSnapshotModulePurposeV1(path)
		if !exists {
			t.Fatalf("controlled module leaf purpose 缺失=%q", path)
		}
		mode, exists := reviewFreezeCompileInputSnapshotModuleModeV1(path)
		if !exists {
			t.Fatalf("controlled module leaf mode 缺失=%q", path)
		}
		snapshot.ModuleCacheFiles = append(snapshot.ModuleCacheFiles, reviewFreezeCompileInputSnapshotModuleFileV1{
			Path:      path,
			Purpose:   purpose,
			Mode:      mode,
			SHA256:    reviewFreezeSHA256V1(raw),
			SizeBytes: int64(len(raw)),
		})
		identity := "controlled-object-v1:" + path
		loader.listed = append(loader.listed, reviewFreezeCompileModuleLeafListedV1{
			Path:     path,
			Mode:     mode,
			Kind:     reviewFreezeCompileModuleLeafKindRegularV1,
			Identity: identity,
		})
		loader.objects[path] = &reviewFreezeCompileModuleLeafFixtureObjectV1{
			path:     path,
			mode:     mode,
			kind:     reviewFreezeCompileModuleLeafKindRegularV1,
			identity: identity,
			raw:      append([]byte(nil), raw...),
		}
	}
	return &reviewFreezeCompileModuleLeafFixtureV1{snapshot: snapshot, statement: statement, files: files, loader: loader}
}

func reviewFreezeCompileModuleLeafFixtureFileIndexV1(fixture *reviewFreezeCompileModuleLeafFixtureV1, path string) int {
	for index, file := range fixture.snapshot.ModuleCacheFiles {
		if file.Path == path {
			return index
		}
	}
	return -1
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1Valid(t *testing.T) {
	fixture := reviewFreezeNewRealCompileModuleLeafFixtureV1(t)
	infoPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.info"
	infoIndex := reviewFreezeCompileModuleLeafFixtureFileIndexV1(fixture, infoPath)
	wantInfoRaw := []byte(reviewFreezeCompileModuleInfoMinimalRawV1)
	if infoIndex < 0 || !bytes.Equal(fixture.files[infoPath], wantInfoRaw) ||
		!bytes.Equal(fixture.loader.objects[infoPath].raw, wantInfoRaw) ||
		fixture.snapshot.ModuleCacheFiles[infoIndex].SHA256 != reviewFreezeSHA256V1(wantInfoRaw) ||
		fixture.snapshot.ModuleCacheFiles[infoIndex].SizeBytes != int64(len(wantInfoRaw)) {
		t.Fatal("real fixture .info 未在 snapshot/files/loader 三方收敛为既有 canonical leaf")
	}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("admission"), "module-leaves")
	snapshotRaw := fixture.snapshotRaw(t)
	fixture.loader.beforeList = func() {
		for index := range snapshotRaw {
			snapshotRaw[index] = 'x'
		}
	}
	bundle, err := reviewFreezeResolveCompileModuleLeafBundleV1(ctx, snapshotRaw, fixture.statement, fixture.loader)
	if err != nil {
		t.Fatalf("valid fixed x/text module leaf bundle rejected: %v", err)
	}
	if fixture.loader.listCalls != 1 || fixture.loader.listContext != ctx {
		t.Fatalf("List calls/context=%d/%v want=1/same", fixture.loader.listCalls, fixture.loader.listContext == ctx)
	}
	if got, want := bundle.Paths(), reviewFreezeCompileInputSnapshotModulePathsV1(); !reflect.DeepEqual(got, want) {
		t.Fatalf("bundle paths=%v want=%v", got, want)
	}
	for _, path := range reviewFreezeCompileInputSnapshotModulePathsV1() {
		if fixture.loader.openCalls[path] != 1 || fixture.loader.closeCalls[path] != 1 {
			t.Fatalf("path=%q Open/Close=%d/%d want=1/1", path, fixture.loader.openCalls[path], fixture.loader.closeCalls[path])
		}
		if len(fixture.loader.openContexts[path]) != 1 || fixture.loader.openContexts[path][0] != ctx {
			t.Fatalf("path=%q context 未原样传播", path)
		}
	}
	path := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/normalize.go"
	first, exists := bundle.Bytes(path)
	if !exists || len(first) == 0 {
		t.Fatalf("bundle bytes 缺失=%q", path)
	}
	first[0] ^= 0xff
	second, exists := bundle.Bytes(path)
	if !exists || bytes.Equal(first, second) || !bytes.Equal(second, fixture.files[path]) {
		t.Fatal("bundle Bytes 未提供不可变隔离副本")
	}
	paths := bundle.Paths()
	paths[0] = "mutated"
	if bundle.Paths()[0] == "mutated" {
		t.Fatal("bundle Paths 未提供不可变隔离副本")
	}
	if _, exists := bundle.Bytes("not/a/module/leaf"); exists {
		t.Fatal("bundle 返回未声明 leaf")
	}
	if fixture.loader.listCalls != 1 {
		t.Fatalf("immutable reuse 不应重复 List，got=%d", fixture.loader.listCalls)
	}
	for _, path := range reviewFreezeCompileInputSnapshotModulePathsV1() {
		if fixture.loader.openCalls[path] != 1 {
			t.Fatalf("immutable reuse 不应重复 Open path=%q calls=%d", path, fixture.loader.openCalls[path])
		}
	}
}

func TestW2ReviewFreezeCompileModuleInfoFixtureCanonicalizationV1(t *testing.T) {
	originRaw := []byte(reviewFreezeCompileModuleInfoOriginRawV1)
	if len(originRaw) != 191 || reviewFreezeSHA256V1(originRaw) != "sha256:1d44e5dc46abd9d3b552e466a3992a4845bd91c98d46ffde21e39dee7c3d8020" {
		t.Fatalf("official Origin host .info identity=%d/%s", len(originRaw), reviewFreezeSHA256V1(originRaw))
	}
	for name, raw := range map[string][]byte{
		"minimal": []byte(reviewFreezeCompileModuleInfoMinimalRawV1),
		"origin":  originRaw,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := reviewFreezeCanonicalizeCompileModuleInfoFixtureV1(raw)
			if err != nil {
				t.Fatalf("canonicalize exact host .info: %v", err)
			}
			if string(got) != reviewFreezeCompileModuleInfoMinimalRawV1 || len(got) != 51 || reviewFreezeSHA256V1(got) != "sha256:28dd596571b8d43955f059757e0cefd9e6e76d17b4e6a042020172ac45325196" {
				t.Fatalf("canonical .info identity=%q/%d/%s", string(got), len(got), reviewFreezeSHA256V1(got))
			}
			raw[0] ^= 0xff
			if string(got) != reviewFreezeCompileModuleInfoMinimalRawV1 {
				t.Fatal("canonical .info 输出与宿主输入共享可变底层字节")
			}
		})
	}

	origin := reviewFreezeCompileModuleInfoOriginRawV1
	tests := map[string]string{
		"version_drift":          strings.Replace(reviewFreezeCompileModuleInfoMinimalRawV1, `"v0.34.0"`, `"v0.34.1"`, 1),
		"time_drift":             strings.Replace(reviewFreezeCompileModuleInfoMinimalRawV1, "16:14:29Z", "16:14:30Z", 1),
		"origin_vcs_drift":       strings.Replace(origin, `"VCS":"git"`, `"VCS":"hg"`, 1),
		"origin_url_drift":       strings.Replace(origin, "https://go.googlesource.com/text", "https://example.invalid/text", 1),
		"origin_hash_drift":      strings.Replace(origin, "817fba9abd337b4d9097b10c61a540c74feaaeff", strings.Repeat("0", 40), 1),
		"origin_ref_drift":       strings.Replace(origin, "refs/tags/v0.34.0", "refs/tags/v0.34.1", 1),
		"origin_missing_vcs":     strings.Replace(origin, `"VCS":"git",`, "", 1),
		"origin_missing_url":     strings.Replace(origin, `"URL":"https://go.googlesource.com/text",`, "", 1),
		"origin_missing_hash":    strings.Replace(origin, `"Hash":"817fba9abd337b4d9097b10c61a540c74feaaeff",`, "", 1),
		"origin_missing_ref":     strings.Replace(origin, `,"Ref":"refs/tags/v0.34.0"`, "", 1),
		"top_unknown_field":      strings.Replace(reviewFreezeCompileModuleInfoMinimalRawV1, "}", `,"Unknown":true}`, 1),
		"origin_unknown_field":   strings.Replace(origin, "}}", `,"Unknown":true}}`, 1),
		"origin_null":            strings.Replace(reviewFreezeCompileModuleInfoMinimalRawV1, "}", `,"Origin":null}`, 1),
		"duplicate_version":      strings.Replace(reviewFreezeCompileModuleInfoMinimalRawV1, `"Version":"v0.34.0"`, `"Version":"v0.34.0","Version":"v0.34.0"`, 1),
		"noncanonical_order":     `{"Time":"2026-02-09T16:14:29Z","Version":"v0.34.0"}`,
		"origin_reordered":       strings.Replace(origin, `"VCS":"git","URL":"https://go.googlesource.com/text"`, `"URL":"https://go.googlesource.com/text","VCS":"git"`, 1),
		"extra_whitespace":       reviewFreezeCompileModuleInfoMinimalRawV1 + " ",
		"trailing_newline":       reviewFreezeCompileModuleInfoMinimalRawV1 + "\n",
		"trailing_second_object": reviewFreezeCompileModuleInfoMinimalRawV1 + `{}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := reviewFreezeCanonicalizeCompileModuleInfoFixtureV1([]byte(raw))
			if err == nil || len(got) != 0 || !strings.Contains(err.Error(), ".info") {
				t.Fatalf("unsupported host .info 未失败关闭: raw=%q output=%q err=%v", raw, got, err)
			}
		})
	}
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1ListAndMetadataFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileModuleLeafFixtureV1)
		want   string
	}{
		{name: "list_error", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listErr = errors.New("list failed") }, want: "exact List"},
		{name: "missing", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.loader.listed = f.loader.listed[:len(f.loader.listed)-1]
		}, want: "exact-set"},
		{name: "extra", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.loader.listed = append(f.loader.listed, reviewFreezeCompileModuleLeafListedV1{Path: "extra", Mode: "0644", Kind: reviewFreezeCompileModuleLeafKindRegularV1, Identity: "extra"})
		}, want: "exact-set"},
		{name: "out_of_order", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.loader.listed[0], f.loader.listed[1] = f.loader.listed[1], f.loader.listed[0]
		}, want: "canonical exact-set"},
		{name: "duplicate", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[1] = f.loader.listed[0] }, want: "canonical exact-set"},
		{name: "listed_symlink", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[0].Kind = "symlink" }, want: "symlink-or-nonregular"},
		{name: "listed_directory", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[0].Kind = "directory" }, want: "symlink-or-nonregular"},
		{name: "listed_mode", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[0].Mode = "0600" }, want: "mode"},
		{name: "materialized_listed_mode", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[4].Mode = "0644" }, want: "actual mode"},
		{name: "listed_identity_empty", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) { f.loader.listed[0].Identity = "" }, want: "identity"},
		{name: "purpose_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.snapshot.ModuleCacheFiles[0].Purpose = reviewFreezeCompileInputSnapshotModuleGoInputV1
		}, want: "purpose"},
		{name: "materialized_snapshot_mode_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.snapshot.ModuleCacheFiles[4].Mode = "0644"
		}, want: "mode"},
		{name: "snapshot_missing", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.snapshot.ModuleCacheFiles = f.snapshot.ModuleCacheFiles[:len(f.snapshot.ModuleCacheFiles)-1]
		}, want: "exact-set"},
		{name: "statement_module_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.statement.ExternalModules[0].ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}, want: "x/text module/go.mod identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
			test.mutate(fixture)
			_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, context.Background(), fixture)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("want error containing %q, got=%v", test.want, err)
			}
			for path, calls := range fixture.loader.openCalls {
				if calls != 0 {
					t.Fatalf("metadata/List failure 后不应 Open path=%q calls=%d", path, calls)
				}
			}
		})
	}
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1OpenFailClosed(t *testing.T) {
	firstPath := reviewFreezeCompileInputSnapshotModulePathsV1()[0]
	tests := []struct {
		name       string
		mutate     func(*reviewFreezeCompileModuleLeafFixtureObjectV1)
		want       string
		wantClosed int
	}{
		{name: "open_error", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.openErr = errors.New("open failed") }, want: "Open", wantClosed: 0},
		{name: "open_error_with_reader", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) {
			object.openErr = errors.New("open failed with reader")
			object.readerWithOpenErr = true
		}, want: "Open", wantClosed: 1},
		{name: "nil_reader", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.nilReader = true }, want: "reader", wantClosed: 0},
		{name: "path_drift", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.path += ".replacement" }, want: "path drift", wantClosed: 1},
		{name: "identity_toctou", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.identity += ":replacement" }, want: "TOCTOU", wantClosed: 1},
		{name: "opened_mode", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.mode = "0600" }, want: "mode drift", wantClosed: 1},
		{name: "opened_symlink", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.kind = "symlink" }, want: "symlink-or-nonregular", wantClosed: 1},
		{name: "truncated", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) {
			object.raw = object.raw[:len(object.raw)-1]
		}, want: "size=", wantClosed: 1},
		{name: "oversized", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.raw = append(object.raw, 'x') }, want: "size=", wantClosed: 1},
		{name: "same_size_hash_drift", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.raw[0] ^= 0xff }, want: "sha256", wantClosed: 1},
		{name: "read_error", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) { object.readErr = errors.New("read failed") }, want: "Read", wantClosed: 1},
		{name: "close_error", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1) {
			object.closeErr = errors.New("close failed")
		}, want: "Close", wantClosed: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
			test.mutate(fixture.loader.objects[firstPath])
			_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, context.Background(), fixture)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("want error containing %q, got=%v", test.want, err)
			}
			if fixture.loader.listCalls != 1 || fixture.loader.openCalls[firstPath] != 1 || fixture.loader.closeCalls[firstPath] != test.wantClosed {
				t.Fatalf("List/Open/Close=%d/%d/%d want=1/1/%d", fixture.loader.listCalls, fixture.loader.openCalls[firstPath], fixture.loader.closeCalls[firstPath], test.wantClosed)
			}
		})
	}
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1ContextAfterOpenAndRead(t *testing.T) {
	firstPath := reviewFreezeCompileInputSnapshotModulePathsV1()[0]
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileModuleLeafFixtureObjectV1, context.CancelFunc)
		want   string
	}{
		{name: "cancel_after_open", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1, cancel context.CancelFunc) {
			object.afterOpen = cancel
		}, want: "after Open"},
		{name: "cancel_after_read", mutate: func(object *reviewFreezeCompileModuleLeafFixtureObjectV1, cancel context.CancelFunc) {
			object.afterRead = cancel
		}, want: "after Read"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
			ctx, cancel := context.WithCancel(context.Background())
			test.mutate(fixture.loader.objects[firstPath], cancel)
			_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, ctx, fixture)
			if !errors.Is(err, context.Canceled) || !bytes.Contains([]byte(err.Error()), []byte("context")) {
				t.Fatalf("want context cancellation %q, got=%v", test.want, err)
			}
			if fixture.loader.openCalls[firstPath] != 1 || fixture.loader.closeCalls[firstPath] != 1 {
				t.Fatalf("cancel path Open/Close=%d/%d want=1/1", fixture.loader.openCalls[firstPath], fixture.loader.closeCalls[firstPath])
			}
		})
	}

	t.Run("cancel_blocking_read", func(t *testing.T) {
		fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		blocking := reviewFreezeNewCompileModuleLeafBlockingReaderV1(func() {
			fixture.loader.closeCalls[firstPath]++
		})
		fixture.loader.objects[firstPath].readerOverride = blocking
		cancelled := make(chan struct{})
		go func() {
			<-blocking.started
			cancel()
			close(cancelled)
		}()
		_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, ctx, fixture)
		<-cancelled
		if !errors.Is(err, context.Canceled) || !bytes.Contains([]byte(err.Error()), []byte("during Read")) {
			t.Fatalf("blocking Read 未被 context 取消: %v", err)
		}
		if fixture.loader.openCalls[firstPath] != 1 || fixture.loader.closeCalls[firstPath] != 1 {
			t.Fatalf("blocking Read cancel Open/Close=%d/%d want=1/1", fixture.loader.openCalls[firstPath], fixture.loader.closeCalls[firstPath])
		}
	})
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1SemanticFailClosed(t *testing.T) {
	infoPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.info"
	zipHashPath := reviewFreezeCompileInputSnapshotModuleDownloadRootV1 + "/v0.34.0.ziphash"
	materializedGoModPath := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/go.mod"
	selectedPath := reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/normalize.go"
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileModuleLeafFixtureV1)
		want   string
	}{
		{name: "info_noncanonical_order", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.files[infoPath] = []byte(`{"Time":"` + reviewFreezeCompileInputSnapshotModuleInfoTimeV1 + `","Version":"v0.34.0"}`)
		}, want: ".info"},
		{name: "info_time_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.files[infoPath] = []byte(`{"Version":"v0.34.0","Time":"2026-02-09T16:14:30Z"}`)
		}, want: ".info"},
		{name: "info_origin_raw_bypasses_fixture", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.files[infoPath] = []byte(reviewFreezeCompileModuleInfoOriginRawV1)
		}, want: ".info"},
		{name: "ziphash_newline", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.files[zipHashPath] = append(f.files[zipHashPath], '\n')
		}, want: ".ziphash"},
		{name: "download_materialized_mod_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.files[materializedGoModPath] = append([]byte(nil), f.files[materializedGoModPath]...)
			f.files[materializedGoModPath][0] ^= 0x01
		}, want: "download .mod"},
		{name: "gomod_sum_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.statement.ExternalModules[0].GoModSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: ".mod h1"},
		{name: "zip_entry_count_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.statement.ExternalModules[0].ZipEntryCount++
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: "entry/size metadata"},
		{name: "zip_uncompressed_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.statement.ExternalModules[0].ZipUncompressedBytes++
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: "entry/size metadata"},
		{name: "hashdir_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			f.statement.ExternalModules[0].HashDirAfter = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: "HashDir"},
		{name: "selected_materialized_zip_drift", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			raw := append([]byte(nil), f.files[selectedPath]...)
			raw[0] ^= 0x01
			f.files[selectedPath] = raw
			for packageIndex := range f.statement.ExternalModules[0].Packages {
				for sourceIndex := range f.statement.ExternalModules[0].Packages[packageIndex].Sources {
					source := &f.statement.ExternalModules[0].Packages[packageIndex].Sources[sourceIndex]
					if reviewFreezeCompileInputSnapshotModuleMaterializedV1+"/"+source.Path == selectedPath {
						source.SHA256 = reviewFreezeSHA256V1(raw)
					}
				}
			}
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: "zip/materialized"},
		{name: "selected_duplicate", mutate: func(f *reviewFreezeCompileModuleLeafFixtureV1) {
			sources := f.statement.ExternalModules[0].Packages[1].Sources
			sources[len(sources)-1] = sources[0]
			f.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), f.statement.ExternalModules...)
		}, want: "重复"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeNewCompileModuleLeafFixtureV1(t)
			test.mutate(fixture)
			err := reviewFreezeValidateCompileModuleLeafSemanticsV1(fixture.snapshot, fixture.statement, fixture.files)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.want)) {
				t.Fatalf("want semantic error containing %q, got=%v", test.want, err)
			}
		})
	}
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1ZipRootGoModFailClosed(t *testing.T) {
	fixture := reviewFreezeNewCompileModuleLeafFixtureV1(t)
	module := &fixture.statement.ExternalModules[0]
	prefix := module.ModulePath + "@" + module.Version + "/"
	zipGoMod := []byte("module golang.org/x/text\n\ngo 1.24.1\n")
	entries := []reviewFreezeExternalModuleZipEntryFixtureV1{{name: prefix + "go.mod", raw: zipGoMod}}
	var uncompressed int64 = int64(len(zipGoMod))
	for _, selectedPackage := range module.Packages {
		for _, source := range selectedPackage.Sources {
			raw := fixture.files[reviewFreezeCompileInputSnapshotModuleMaterializedV1+"/"+source.Path]
			entries = append(entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + source.Path, raw: append([]byte(nil), raw...)})
			uncompressed += int64(len(raw))
		}
	}
	moduleZip := reviewFreezeWriteExternalModuleZipFixtureV1(t, entries)
	_, moduleSum, err := reviewFreezeReadAndHashExternalModuleZipV1(moduleZip, module.ModulePath, module.Version)
	if err != nil {
		t.Fatalf("rebuild zip root drift fixture: %v", err)
	}
	module.ZipSHA256 = reviewFreezeSHA256V1(moduleZip)
	module.ZipSizeBytes = int64(len(moduleZip))
	module.ZipEntryCount = len(entries)
	module.ZipUncompressedBytes = uncompressed
	module.ModuleSum = moduleSum
	module.HashDirBefore = moduleSum
	module.HashDirAfter = moduleSum
	fixture.files[reviewFreezeCompileInputSnapshotModuleDownloadRootV1+"/v0.34.0.zip"] = moduleZip
	fixture.files[reviewFreezeCompileInputSnapshotModuleDownloadRootV1+"/v0.34.0.ziphash"] = []byte(moduleSum)
	fixture.snapshot.ExternalModules = append([]reviewFreezeCompileAttestationModuleV1(nil), fixture.statement.ExternalModules...)
	err = reviewFreezeValidateCompileModuleLeafSemanticsV1(fixture.snapshot, fixture.statement, fixture.files)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("zip root go.mod")) {
		t.Fatalf("want zip root go.mod byte mismatch, got=%v", err)
	}
}

func TestW2ReviewFreezeCompileModuleLeafBundleV1ResourceAndContextBudget(t *testing.T) {
	t.Run("declared_total", func(t *testing.T) {
		fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
		for _, path := range []string{
			reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/composition.go",
			reviewFreezeCompileInputSnapshotModuleMaterializedV1 + "/unicode/norm/forminfo.go",
		} {
			index := reviewFreezeCompileModuleLeafFixtureFileIndexV1(fixture, path)
			if index < 0 {
				t.Fatalf("fixture path 缺失=%q", path)
			}
			fixture.snapshot.ModuleCacheFiles[index].SizeBytes = 48 << 20
		}
		_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, context.Background(), fixture)
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("声明总字节超限")) {
			t.Fatalf("want total budget error, got=%v", err)
		}
		if fixture.loader.listCalls != 0 {
			t.Fatalf("budget failure 不应调用 List，got=%d", fixture.loader.listCalls)
		}
	})
	t.Run("cancelled_before_list", func(t *testing.T) {
		fixture := reviewFreezeNewCompileModuleLeafAdmissionFixtureV1(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := reviewFreezeResolveCompileModuleLeafFixtureV1(t, ctx, fixture)
		if !errors.Is(err, context.Canceled) || fixture.loader.listCalls != 0 {
			t.Fatalf("cancelled context err/List=%v/%d", err, fixture.loader.listCalls)
		}
	})
}
