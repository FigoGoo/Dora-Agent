package reviewfreeze_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"go/build"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	reviewFreezeXTextModulePathV1       = "golang.org/x/text"
	reviewFreezeXTextModuleVersionV1    = "v0.34.0"
	reviewFreezeModuleZipMaxBytesV1     = 16 << 20
	reviewFreezeModuleZipMaxFilesV1     = 2048
	reviewFreezeModuleZipMaxFileBytesV1 = 16 << 20
	reviewFreezeModuleZipMaxTotalV1     = 64 << 20
	reviewFreezeExternalGoModMaxBytesV1 = 1 << 20
)

// reviewFreezeExternalModuleArtifactsV1 承载 resolver 唯一可信的外部 Module 输入。
// 调用方只提供原始 zip 与 .mod 字节；解压目录、ziphash 旁路文件和网络响应均不参与信任决策。
type reviewFreezeExternalModuleArtifactsV1 struct {
	moduleZip []byte
	goMod     []byte
}

// reviewFreezeExternalModuleArtifactLoaderV1 按已声明的 path/version 返回隔离的 Module 原始字节。
// Loader 不得自行修改版本或从 workspace/vendor 推导替代来源。
type reviewFreezeExternalModuleArtifactLoaderV1 func(modulePath, version string) (reviewFreezeExternalModuleArtifactsV1, error)

// reviewFreezeResolvedModuleZipV1 保存通过 h1、路径和大小门禁后的 Module zip 内容。
// files 的 key 是 Module Root 相对路径，且已保证大小写无歧义与 exact uniqueness。
type reviewFreezeResolvedModuleZipV1 struct {
	files map[string][]byte
}

// reviewFreezeNewModuleDownloadCacheArtifactLoaderV1 从显式 GOMODCACHE 根的 cache/download 树只读加载 x/text .zip/.mod。
// Loader 拒绝符号链接和路径身份漂移，且故意不读 .ziphash/.info/.lock；内容信任仍由后续 canonical h1 重算建立。
func reviewFreezeNewModuleDownloadCacheArtifactLoaderV1(moduleCacheRoot string) (reviewFreezeExternalModuleArtifactLoaderV1, error) {
	if moduleCacheRoot == "" || !filepath.IsAbs(moduleCacheRoot) || filepath.Clean(moduleCacheRoot) != moduleCacheRoot {
		return nil, fmt.Errorf("module download cache root 必须是规范绝对路径=%q", moduleCacheRoot)
	}
	rootInfo, err := os.Lstat(moduleCacheRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("module download cache root 不是普通目录=%q err=%v", moduleCacheRoot, err)
	}
	realRoot, err := filepath.EvalSymlinks(moduleCacheRoot)
	if err != nil {
		return nil, fmt.Errorf("解析 module download cache root=%q: %w", moduleCacheRoot, err)
	}
	return func(modulePath, version string) (reviewFreezeExternalModuleArtifactsV1, error) {
		if modulePath != reviewFreezeXTextModulePathV1 || version != reviewFreezeXTextModuleVersionV1 {
			return reviewFreezeExternalModuleArtifactsV1{}, fmt.Errorf("module download cache loader 只允许 %s@%s, got=%s@%s", reviewFreezeXTextModulePathV1, reviewFreezeXTextModuleVersionV1, modulePath, version)
		}
		versionRoot := pathpkg.Join("cache/download", modulePath, "@v")
		zipRaw, err := reviewFreezeReadModuleCacheRegularFileV1(realRoot, pathpkg.Join(versionRoot, version+".zip"), reviewFreezeModuleZipMaxBytesV1)
		if err != nil {
			return reviewFreezeExternalModuleArtifactsV1{}, err
		}
		modRaw, err := reviewFreezeReadModuleCacheRegularFileV1(realRoot, pathpkg.Join(versionRoot, version+".mod"), reviewFreezeExternalGoModMaxBytesV1)
		if err != nil {
			return reviewFreezeExternalModuleArtifactsV1{}, err
		}
		return reviewFreezeExternalModuleArtifactsV1{moduleZip: zipRaw, goMod: modRaw}, nil
	}, nil
}

// reviewFreezeReadModuleCacheRegularFileV1 在 canonical cache root 内逐层拒绝 symlink，并有界读取一个普通文件。
func reviewFreezeReadModuleCacheRegularFileV1(root, relative string, maxBytes int64) ([]byte, error) {
	if err := reviewFreezeValidateSafePathV1(relative, "cache/download/"); err != nil {
		return nil, fmt.Errorf("module cache artifact: %w", err)
	}
	current := root
	segments := strings.Split(relative, "/")
	var finalInfo os.FileInfo
	for index, segment := range segments {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("读取 module cache artifact %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("module cache artifact 禁止 symlink=%q", relative)
		}
		if index < len(segments)-1 && !info.IsDir() {
			return nil, fmt.Errorf("module cache artifact 中间路径不是目录=%q", relative)
		}
		if index == len(segments)-1 {
			finalInfo = info
		}
	}
	if finalInfo == nil || !finalInfo.Mode().IsRegular() || finalInfo.Size() < 0 || finalInfo.Size() > maxBytes {
		return nil, fmt.Errorf("module cache artifact 不是有界普通文件=%q", relative)
	}
	opened, err := os.Open(current)
	if err != nil {
		return nil, fmt.Errorf("打开 module cache artifact %q: %w", relative, err)
	}
	openedInfo, statErr := opened.Stat()
	if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, openedInfo) {
		opened.Close()
		return nil, fmt.Errorf("module cache artifact 读取期间身份漂移=%q err=%v", relative, statErr)
	}
	raw, readErr := io.ReadAll(io.LimitReader(opened, maxBytes+1))
	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != finalInfo.Size() || int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("读取 module cache artifact %q 失败 read=%v close=%v size=%d/%d", relative, readErr, closeErr, len(raw), finalInfo.Size())
	}
	return raw, nil
}

// reviewFreezeNewExternalModuleZipResolverV1 构造 x/text v0.34.0 的最小 zip-backed selected-source resolver 候选。
// Resolver 不执行 Go 命令、不使用网络且不信任本机解压 cache；任一证据缺失均返回错误。
func reviewFreezeNewExternalModuleZipResolverV1(loader reviewFreezeExternalModuleArtifactLoaderV1) reviewFreezeExternalPackageResolverV1 {
	return func(module reviewFreezeValidatorExternalModuleV1, importPath string, environment reviewFreezeValidatorBuildEnvironmentV1, goSumRaw []byte) (map[string][]byte, error) {
		if loader == nil {
			return nil, fmt.Errorf("external module artifact loader 未接入")
		}
		artifacts, err := loader(module.ModulePath, module.Version)
		if err != nil {
			return nil, fmt.Errorf("读取 external module artifacts %s@%s: %w", module.ModulePath, module.Version, err)
		}
		return reviewFreezeResolveExternalPackageFromZipV1(module, importPath, environment, goSumRaw, artifacts)
	}
}

// reviewFreezeResolveExternalPackageFromZipV1 合并 go.sum lock、Module zip/.mod 实体与 Go build selection。
// 成功时返回 module-relative source exact-set 的隔离副本；不产生文件系统或网络副作用。
func reviewFreezeResolveExternalPackageFromZipV1(module reviewFreezeValidatorExternalModuleV1, importPath string, environment reviewFreezeValidatorBuildEnvironmentV1, goSumRaw []byte, artifacts reviewFreezeExternalModuleArtifactsV1) (map[string][]byte, error) {
	if module.ModulePath != reviewFreezeXTextModulePathV1 || module.Version != reviewFreezeXTextModuleVersionV1 {
		return nil, fmt.Errorf("zip resolver 只允许 %s@%s, got=%s@%s", reviewFreezeXTextModulePathV1, reviewFreezeXTextModuleVersionV1, module.ModulePath, module.Version)
	}
	if importPath != reviewFreezeXTextModulePathV1+"/unicode/norm" && importPath != reviewFreezeXTextModulePathV1+"/transform" {
		return nil, fmt.Errorf("zip resolver 未知 x/text package=%q", importPath)
	}
	if err := reviewFreezeValidateValidatorBuildEnvironmentV1(environment); err != nil {
		return nil, err
	}
	if runtime.Version() != environment.Toolchain {
		return nil, fmt.Errorf("resolver runtime toolchain=%q want=%q", runtime.Version(), environment.Toolchain)
	}
	if err := reviewFreezeValidateExternalModuleGoSumV1(goSumRaw, module); err != nil {
		return nil, err
	}
	if err := reviewFreezeValidateExternalModuleGoModV1(artifacts.goMod, module.ModulePath); err != nil {
		return nil, err
	}
	goModSum, err := reviewFreezeHash1V1([]string{"go.mod"}, map[string][]byte{"go.mod": artifacts.goMod})
	if err != nil {
		return nil, fmt.Errorf("计算 external go.mod h1: %w", err)
	}
	if !reviewFreezeEqualH1V1(goModSum, module.GoModSum) {
		return nil, fmt.Errorf("external go.mod h1=%s want=%s", goModSum, module.GoModSum)
	}

	resolvedZip, moduleSum, err := reviewFreezeReadAndHashExternalModuleZipV1(artifacts.moduleZip, module.ModulePath, module.Version)
	if err != nil {
		return nil, err
	}
	if !reviewFreezeEqualH1V1(moduleSum, module.ModuleSum) {
		return nil, fmt.Errorf("external module zip h1=%s want=%s", moduleSum, module.ModuleSum)
	}
	zipGoMod, exists := resolvedZip.files["go.mod"]
	if !exists {
		return nil, fmt.Errorf("external module zip 缺根 go.mod")
	}
	if !bytes.Equal(zipGoMod, artifacts.goMod) {
		return nil, fmt.Errorf("external module zip go.mod 与 .mod artifact 字节不一致")
	}
	return reviewFreezeSelectExternalPackageSourcesV1(resolvedZip, module.ModulePath, importPath, environment)
}

// reviewFreezeValidateExternalModuleGoSumV1 要求 go.sum 全文结构合法，且目标 Module 的 zip/go.mod 两条 h1 各唯一且精确匹配。
func reviewFreezeValidateExternalModuleGoSumV1(raw []byte, module reviewFreezeValidatorExternalModuleV1) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("external module go.sum 不是合法 UTF-8")
	}
	want := map[string]string{
		module.Version:             module.ModuleSum,
		module.Version + "/go.mod": module.GoModSum,
	}
	seenAll := make(map[string]struct{})
	seenFold := make(map[string]string)
	found := make(map[string]bool, len(want))
	for lineIndex, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.Join(fields, " ") != line || !reviewFreezeExternalImportPathV1.MatchString(fields[0]) || !reviewFreezeGoModuleChecksumV1.MatchString(fields[2]) {
			return fmt.Errorf("external module go.sum line=%d 形状非法", lineIndex+1)
		}
		key := fields[0] + "\x00" + fields[1]
		if _, duplicate := seenAll[key]; duplicate {
			return fmt.Errorf("external module go.sum 重复项=%s %s", fields[0], fields[1])
		}
		seenAll[key] = struct{}{}
		folded := strings.ToLower(key)
		if previous, ambiguous := seenFold[folded]; ambiguous && previous != key {
			return fmt.Errorf("external module go.sum 大小写歧义=%q/%q", previous, key)
		}
		seenFold[folded] = key
		if strings.EqualFold(fields[0], module.ModulePath) && fields[0] != module.ModulePath {
			return fmt.Errorf("external module go.sum module path 大小写歧义=%q", fields[0])
		}
		wantSum, target := want[fields[1]]
		if fields[0] != module.ModulePath || !target {
			continue
		}
		if !reviewFreezeEqualH1V1(fields[2], wantSum) {
			return fmt.Errorf("external module go.sum %s %s h1=%s want=%s", fields[0], fields[1], fields[2], wantSum)
		}
		found[fields[1]] = true
	}
	for version := range want {
		if !found[version] {
			return fmt.Errorf("external module go.sum 缺精确项=%s %s", module.ModulePath, version)
		}
	}
	return nil
}

// reviewFreezeValidateExternalModuleGoModV1 校验 .mod 字节的 Module 身份，并阻断 replace/use/workspace 类隐式来源。
func reviewFreezeValidateExternalModuleGoModV1(raw []byte, modulePath string) error {
	if len(raw) == 0 || len(raw) > reviewFreezeExternalGoModMaxBytesV1 || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return fmt.Errorf("external go.mod 字节非法或超限")
	}
	if reviewFreezeGoModReplaceDirectiveV1.Match(raw) {
		return fmt.Errorf("external go.mod 禁止 replace")
	}
	moduleDirectives := 0
	for lineIndex, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && (fields[0] == "use" || fields[0] == "workspace") {
			return fmt.Errorf("external go.mod 禁止 workspace directive line=%d", lineIndex+1)
		}
		if len(fields) > 0 && fields[0] == "module" {
			moduleDirectives++
			if len(fields) != 2 || fields[1] != modulePath {
				return fmt.Errorf("external go.mod module path=%v want=%s", fields, modulePath)
			}
		}
	}
	if moduleDirectives != 1 {
		return fmt.Errorf("external go.mod module directive 数=%d want=1", moduleDirectives)
	}
	return nil
}

// reviewFreezeReadAndHashExternalModuleZipV1 一次性验证 zip 全部 entry 并计算 Go dirhash h1。
// 重复名、大小写碰撞、路径穿越、vendor/workspace、嵌套 go.mod 或非普通文件均失败关闭。
func reviewFreezeReadAndHashExternalModuleZipV1(raw []byte, modulePath, version string) (reviewFreezeResolvedModuleZipV1, string, error) {
	resolved := reviewFreezeResolvedModuleZipV1{files: make(map[string][]byte)}
	if len(raw) == 0 || len(raw) > reviewFreezeModuleZipMaxBytesV1 {
		return resolved, "", fmt.Errorf("external module zip 字节为空或超限=%d", len(raw))
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return resolved, "", fmt.Errorf("解析 external module zip: %w", err)
	}
	if len(reader.File) == 0 || len(reader.File) > reviewFreezeModuleZipMaxFilesV1 {
		return resolved, "", fmt.Errorf("external module zip entry 数非法=%d", len(reader.File))
	}
	prefix := modulePath + "@" + version + "/"
	fullFiles := make(map[string][]byte, len(reader.File))
	seenFold := make(map[string]string, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		name := file.Name
		if err := reviewFreezeValidateExternalZipPathV1(name, prefix); err != nil {
			return resolved, "", err
		}
		if _, duplicate := fullFiles[name]; duplicate {
			return resolved, "", fmt.Errorf("external module zip 重复 entry=%q", name)
		}
		folded := strings.ToLower(name)
		if previous, ambiguous := seenFold[folded]; ambiguous && previous != name {
			return resolved, "", fmt.Errorf("external module zip 大小写歧义 entry=%q/%q", previous, name)
		}
		seenFold[folded] = name
		if file.Flags&1 != 0 || file.Method != zip.Store && file.Method != zip.Deflate || file.Mode()&os.ModeType != 0 {
			return resolved, "", fmt.Errorf("external module zip entry 非普通可读文件=%q", name)
		}
		if file.UncompressedSize64 > reviewFreezeModuleZipMaxFileBytesV1 || total+file.UncompressedSize64 > reviewFreezeModuleZipMaxTotalV1 {
			return resolved, "", fmt.Errorf("external module zip entry 或总字节超限=%q", name)
		}
		opened, err := file.Open()
		if err != nil {
			return resolved, "", fmt.Errorf("打开 external module zip entry %q: %w", name, err)
		}
		content, readErr := io.ReadAll(io.LimitReader(opened, reviewFreezeModuleZipMaxFileBytesV1+1))
		closeErr := opened.Close()
		if readErr != nil || closeErr != nil || uint64(len(content)) != file.UncompressedSize64 {
			return resolved, "", fmt.Errorf("读取 external module zip entry %q 失败 read=%v close=%v size=%d/%d", name, readErr, closeErr, len(content), file.UncompressedSize64)
		}
		relative := strings.TrimPrefix(name, prefix)
		fullFiles[name] = content
		resolved.files[relative] = content
		total += file.UncompressedSize64
	}
	moduleSum, err := reviewFreezeHash1V1(reviewFreezeSortedKeysV1(fullFiles), fullFiles)
	if err != nil {
		return resolved, "", fmt.Errorf("计算 external module zip h1: %w", err)
	}
	return resolved, moduleSum, nil
}

// reviewFreezeValidateExternalZipPathV1 要求 zip entry 完全位于唯一 Module prefix 下，并拒绝对当前 x/text 信任闭包无必要的路径形状。
func reviewFreezeValidateExternalZipPathV1(name, prefix string) error {
	if !utf8.ValidString(name) || len(name) == 0 || len(name) > 512 || !strings.HasPrefix(name, prefix) {
		return fmt.Errorf("external module zip entry prefix/UTF-8/长度非法=%q", name)
	}
	for _, value := range []string{name, strings.TrimPrefix(name, prefix)} {
		for _, current := range []byte(value) {
			if current < 0x20 || current > 0x7e {
				return fmt.Errorf("external module zip entry 只允许无歧义 ASCII 路径=%q", name)
			}
		}
	}
	relative := strings.TrimPrefix(name, prefix)
	if err := reviewFreezeValidateSafePathV1(relative, ""); err != nil {
		return fmt.Errorf("external module zip entry: %w", err)
	}
	segments := strings.Split(strings.ToLower(relative), "/")
	for _, segment := range segments {
		if segment == "vendor" {
			return fmt.Errorf("external module zip 禁止 vendor entry=%q", name)
		}
	}
	base := pathpkg.Base(relative)
	if strings.EqualFold(base, "go.work") || strings.EqualFold(base, "go.work.sum") {
		return fmt.Errorf("external module zip 禁止 workspace entry=%q", name)
	}
	if strings.EqualFold(base, "go.mod") && relative != "go.mod" {
		return fmt.Errorf("external module zip 禁止嵌套/大小写歧义 go.mod=%q", name)
	}
	return nil
}

// reviewFreezeSelectExternalPackageSourcesV1 使用候选策略显式固定的 Go 1.26.3 linux/amd64 build tags
// 对 zip 内单 package 做静态 source selection；它不替代后续受信 go list/typecheck/compile attestation。
// 目标目录中除 Go source 以外的未建模文件会直接失败，防止 asm/cgo/syso 或隐藏文件绕过 exact-set。
func reviewFreezeSelectExternalPackageSourcesV1(resolved reviewFreezeResolvedModuleZipV1, modulePath, importPath string, environment reviewFreezeValidatorBuildEnvironmentV1) (map[string][]byte, error) {
	packageDirectory := strings.TrimPrefix(importPath, modulePath)
	packageDirectory = strings.TrimPrefix(packageDirectory, "/")
	if packageDirectory == "" || strings.HasPrefix(packageDirectory, "../") || pathpkg.Clean(packageDirectory) != packageDirectory {
		return nil, fmt.Errorf("external package 相对路径非法=%q", packageDirectory)
	}
	selected := make(map[string][]byte)
	// 禁止从 build.Default 继承宿主 GOARCH feature 或 GOEXPERIMENT tags。v1 候选环境的
	// linux/amd64 缺省值在此固定为 GOAMD64=v1、GOEXPERIMENT=""、GOFIPS140=off；
	// 正式 attestation v2 会把这些值提升为显式 manifest/runner 字段。
	context := build.Context{
		GOOS:       environment.GOOS,
		GOARCH:     environment.GOARCH,
		CgoEnabled: environment.CGOEnabled == "1",
		Compiler:   "gc",
		ToolTags: []string{
			"goexperiment.regabiwrappers",
			"goexperiment.regabiargs",
			"goexperiment.dwarf5",
			"goexperiment.greenteagc",
			"goexperiment.randomizedheapbase64",
			"amd64.v1",
		},
	}
	for minor := 1; minor <= 26; minor++ {
		context.ReleaseTags = append(context.ReleaseTags, fmt.Sprintf("go1.%d", minor))
	}
	context.JoinPath = pathpkg.Join
	context.OpenFile = func(name string) (io.ReadCloser, error) {
		raw, exists := resolved.files[name]
		if !exists {
			return nil, fmt.Errorf("external module zip source 不存在=%q", name)
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	prefix := packageDirectory + "/"
	for relative, raw := range resolved.files {
		if !strings.HasPrefix(relative, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(relative, prefix)
		if strings.Contains(remainder, "/") {
			return nil, fmt.Errorf("external package %s 包含未知子路径=%q", importPath, relative)
		}
		if strings.HasPrefix(remainder, ".") || strings.HasPrefix(remainder, "_") {
			return nil, fmt.Errorf("external package %s 包含未知隐藏/忽略文件=%q", importPath, relative)
		}
		if pathpkg.Ext(remainder) != ".go" {
			return nil, fmt.Errorf("external package %s 包含未知非 Go build input=%q", importPath, relative)
		}
		if strings.HasSuffix(remainder, "_test.go") {
			continue
		}
		matches, err := context.MatchFile(packageDirectory, remainder)
		if err != nil {
			return nil, fmt.Errorf("external package %s build selection %q: %w", importPath, relative, err)
		}
		if matches {
			selected[relative] = append([]byte(nil), raw...)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("external package %s selected source exact-set 为空", importPath)
	}
	return selected, nil
}

// reviewFreezeHash1V1 按 Go go.sum 的 dirhash Hash1 规则计算 h1。
func reviewFreezeHash1V1(names []string, files map[string][]byte) (string, error) {
	names = append([]string(nil), names...)
	sort.Strings(names)
	directoryHash := sha256.New()
	for _, name := range names {
		if strings.Contains(name, "\n") {
			return "", fmt.Errorf("h1 文件名含换行=%q", name)
		}
		raw, exists := files[name]
		if !exists {
			return "", fmt.Errorf("h1 文件缺失=%q", name)
		}
		fileHash := sha256.Sum256(raw)
		if _, err := fmt.Fprintf(directoryHash, "%x  %s\n", fileHash, name); err != nil {
			return "", err
		}
	}
	return "h1:" + base64.StdEncoding.EncodeToString(directoryHash.Sum(nil)), nil
}

// reviewFreezeEqualH1V1 用常量时间比较已格式化的 Go h1，避免校验路径出现多套宽松比较语义。
func reviewFreezeEqualH1V1(actual, expected string) bool {
	return len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

// reviewFreezeExternalModuleZipEntryFixtureV1 描述单个可重组的测试 zip entry，保留 slice 以构造重复名对抗输入。
type reviewFreezeExternalModuleZipEntryFixtureV1 struct {
	name string
	raw  []byte
}

// reviewFreezeExternalModuleZipFixtureV1 将 module lock、go.sum、.mod 与 zip 变异绑定在同一测试对象中。
type reviewFreezeExternalModuleZipFixtureV1 struct {
	module     reviewFreezeValidatorExternalModuleV1
	importPath string
	goSum      []byte
	goMod      []byte
	entries    []reviewFreezeExternalModuleZipEntryFixtureV1
	moduleZip  []byte
}

// reviewFreezeNewExternalModuleZipFixtureV1 创建最小的 x/text/unicode/norm zip 信任闭包。
func reviewFreezeNewExternalModuleZipFixtureV1(t *testing.T) *reviewFreezeExternalModuleZipFixtureV1 {
	t.Helper()
	goMod := []byte("module golang.org/x/text\n\ngo 1.24.0\n")
	prefix := reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1 + "/"
	fixture := &reviewFreezeExternalModuleZipFixtureV1{
		module: reviewFreezeValidatorExternalModuleV1{
			ModulePath: reviewFreezeXTextModulePathV1,
			Version:    reviewFreezeXTextModuleVersionV1,
			Packages: []reviewFreezeValidatorExternalPackageV1{{
				ImportPath: reviewFreezeXTextModulePathV1 + "/unicode/norm",
			}},
		},
		importPath: reviewFreezeXTextModulePathV1 + "/unicode/norm",
		goMod:      goMod,
		entries: []reviewFreezeExternalModuleZipEntryFixtureV1{
			{name: prefix + "go.mod", raw: append([]byte(nil), goMod...)},
			{name: prefix + "unicode/norm/normalize.go", raw: []byte("package norm\n\nimport \"unicode/utf8\"\n")},
			{name: prefix + "unicode/norm/readwriter.go", raw: []byte("package norm\n\nimport \"io\"\n")},
		},
	}
	fixture.rebind(t)
	return fixture
}

// rebind 重新计算合法 fixture 的 zip/go.mod h1 和 go.sum，使对抗测试可穿过摘要门禁后命中目标语义。
func (fixture *reviewFreezeExternalModuleZipFixtureV1) rebind(t *testing.T) {
	t.Helper()
	fullFiles := make(map[string][]byte, len(fixture.entries))
	for _, entry := range fixture.entries {
		if _, duplicate := fullFiles[entry.name]; duplicate {
			t.Fatalf("cannot rebind duplicate fixture entry=%s", entry.name)
		}
		fullFiles[entry.name] = entry.raw
	}
	moduleSum, err := reviewFreezeHash1V1(reviewFreezeSortedKeysV1(fullFiles), fullFiles)
	if err != nil {
		t.Fatal(err)
	}
	goModSum, err := reviewFreezeHash1V1([]string{"go.mod"}, map[string][]byte{"go.mod": fixture.goMod})
	if err != nil {
		t.Fatal(err)
	}
	fixture.module.ModuleSum = moduleSum
	fixture.module.GoModSum = goModSum
	fixture.goSum = []byte(fmt.Sprintf("%s %s %s\n%s %s/go.mod %s\n", fixture.module.ModulePath, fixture.module.Version, moduleSum, fixture.module.ModulePath, fixture.module.Version, goModSum))
	fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
}

// resolver 使用隔离副本创建候选 resolver，并确保 loader 没有改写请求的 Module 身份。
func (fixture *reviewFreezeExternalModuleZipFixtureV1) resolver() reviewFreezeExternalPackageResolverV1 {
	return reviewFreezeNewExternalModuleZipResolverV1(func(modulePath, version string) (reviewFreezeExternalModuleArtifactsV1, error) {
		if modulePath != fixture.module.ModulePath || version != fixture.module.Version {
			return reviewFreezeExternalModuleArtifactsV1{}, fmt.Errorf("fixture module identity drift=%s@%s", modulePath, version)
		}
		return reviewFreezeExternalModuleArtifactsV1{
			moduleZip: append([]byte(nil), fixture.moduleZip...),
			goMod:     append([]byte(nil), fixture.goMod...),
		}, nil
	})
}

// resolve 执行 fixture 当前的底层 resolver 请求。
func (fixture *reviewFreezeExternalModuleZipFixtureV1) resolve() (map[string][]byte, error) {
	return fixture.resolver()(fixture.module, fixture.importPath, reviewFreezeExpectedValidatorBuildEnvironmentV1(), fixture.goSum)
}

// reviewFreezeWriteExternalModuleZipFixtureV1 创建原始 Module zip 字节，允许重复/非法路径用例由 resolver 自身拒绝。
func reviewFreezeWriteExternalModuleZipFixtureV1(t *testing.T, entries []reviewFreezeExternalModuleZipEntryFixtureV1) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(0o644)
		opened, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := opened.Write(entry.raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// reviewFreezeReplaceExternalModuleZipEntryV1 在保持 entry 顺序的前提下替换唯一文件。
func reviewFreezeReplaceExternalModuleZipEntryV1(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1, name string, raw []byte) {
	t.Helper()
	matched := 0
	for index := range fixture.entries {
		if fixture.entries[index].name == name {
			fixture.entries[index].raw = append([]byte(nil), raw...)
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("replace fixture entry=%s matched=%d", name, matched)
	}
}

// TestW2ReviewFreezeExternalModuleZipResolverV1Valid 证明完整 lock + go.sum + zip/.mod 能产生隔离的 selected-source exact-set。
func TestW2ReviewFreezeExternalModuleZipResolverV1Valid(t *testing.T) {
	fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
	selected, err := fixture.resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"unicode/norm/normalize.go", "unicode/norm/readwriter.go"}
	if got := reviewFreezeSortedKeysV1(selected); !reflect.DeepEqual(got, want) {
		t.Fatalf("selected sources=%v want=%v", got, want)
	}
	selected[want[0]][0] ^= 0xff
	again, err := fixture.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(selected[want[0]], again[want[0]]) {
		t.Fatal("resolver returned aliased source bytes")
	}
}

// TestW2ReviewFreezeExternalModuleZipResolverV1GoModHash 用当前 x/text .mod/go.sum 已知值校验 Hash1 实现与 Go canonical h1 兼容。
func TestW2ReviewFreezeExternalModuleZipResolverV1GoModHash(t *testing.T) {
	raw := []byte("module golang.org/x/text\n\ngo 1.24.0\n\nrequire golang.org/x/tools v0.41.0 // tagx:ignore\n\nrequire (\n\tgolang.org/x/mod v0.32.0 // indirect; tagx:ignore\n\tgolang.org/x/sync v0.19.0 // indirect\n)\n")
	got, err := reviewFreezeHash1V1([]string{"go.mod"}, map[string][]byte{"go.mod": raw})
	if err != nil {
		t.Fatal(err)
	}
	const want = "h1:homfLqTYRFyVYemLBFl5GgL/DWEiH5wcsQ5gSh1yziA="
	if got != want {
		t.Fatalf("go.mod h1=%s want=%s", got, want)
	}
}

// TestW2ReviewFreezeModuleDownloadCacheArtifactLoaderV1 覆盖真实 cache/download 路径、.ziphash 非信任语义与 symlink 失败关闭。
func TestW2ReviewFreezeModuleDownloadCacheArtifactLoaderV1(t *testing.T) {
	t.Run("valid zip and mod only", func(t *testing.T) {
		fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
		root, versionRoot := reviewFreezeWriteModuleDownloadCacheFixtureV1(t, fixture)
		if err := os.WriteFile(filepath.Join(versionRoot, fixture.module.Version+".ziphash"), []byte("h1:untrusted-sidecar\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		loader, err := reviewFreezeNewModuleDownloadCacheArtifactLoaderV1(root)
		if err != nil {
			t.Fatal(err)
		}
		resolver := reviewFreezeNewExternalModuleZipResolverV1(loader)
		selected, err := resolver(fixture.module, fixture.importPath, reviewFreezeExpectedValidatorBuildEnvironmentV1(), fixture.goSum)
		if err != nil {
			t.Fatal(err)
		}
		if got := reviewFreezeSortedKeysV1(selected); !reflect.DeepEqual(got, []string{"unicode/norm/normalize.go", "unicode/norm/readwriter.go"}) {
			t.Fatalf("selected sources=%v", got)
		}
	})

	t.Run("module identity rejected", func(t *testing.T) {
		fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
		root, _ := reviewFreezeWriteModuleDownloadCacheFixtureV1(t, fixture)
		loader, err := reviewFreezeNewModuleDownloadCacheArtifactLoaderV1(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loader("example.invalid/text", fixture.module.Version); err == nil || !strings.Contains(err.Error(), "只允许") {
			t.Fatalf("identity error=%v", err)
		}
	})

	t.Run("symlink artifact rejected", func(t *testing.T) {
		fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
		root, versionRoot := reviewFreezeWriteModuleDownloadCacheFixtureV1(t, fixture)
		zipPath := filepath.Join(versionRoot, fixture.module.Version+".zip")
		realZipPath := filepath.Join(root, "untrusted.zip")
		if err := os.WriteFile(realZipPath, fixture.moduleZip, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(zipPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realZipPath, zipPath); err != nil {
			t.Fatal(err)
		}
		loader, err := reviewFreezeNewModuleDownloadCacheArtifactLoaderV1(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loader(fixture.module.ModulePath, fixture.module.Version); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlink error=%v", err)
		}
	})

	t.Run("relative root rejected", func(t *testing.T) {
		if _, err := reviewFreezeNewModuleDownloadCacheArtifactLoaderV1("relative/cache"); err == nil || !strings.Contains(err.Error(), "绝对路径") {
			t.Fatalf("relative root error=%v", err)
		}
	})
}

// reviewFreezeWriteModuleDownloadCacheFixtureV1 在测试临时目录创建 Go module download cache 的唯一受理路径。
func reviewFreezeWriteModuleDownloadCacheFixtureV1(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) (string, string) {
	t.Helper()
	root := t.TempDir()
	versionRoot := filepath.Join(root, "cache", "download", "golang.org", "x", "text", "@v")
	if err := os.MkdirAll(versionRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionRoot, fixture.module.Version+".zip"), fixture.moduleZip, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionRoot, fixture.module.Version+".mod"), fixture.goMod, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, versionRoot
}

// TestW2ReviewFreezeExternalModuleZipResolverV1BuildSelection 覆盖 GOOS/GOARCH、架构 feature、
// experiment、go1.x build tag 与 _test.go 排除语义，并证明结果不继承运行测试的宿主 ToolTags。
func TestW2ReviewFreezeExternalModuleZipResolverV1BuildSelection(t *testing.T) {
	fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
	prefix := reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1 + "/unicode/norm/"
	fixture.entries = append(fixture.entries,
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "platform_linux_amd64.go", raw: []byte("package norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "platform_windows.go", raw: []byte("package norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "target_feature.go", raw: []byte("//go:build amd64.v1\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "host_feature.go", raw: []byte("//go:build arm64.v8.0\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "target_experiment.go", raw: []byte("//go:build goexperiment.dwarf5\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unknown_experiment.go", raw: []byte("//go:build goexperiment.synthetic\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "current_release.go", raw: []byte("//go:build go1.26\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "future.go", raw: []byte("//go:build go1.27\n\npackage norm\n")},
		reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "normalize_test.go", raw: []byte("package norm\n")},
	)
	fixture.rebind(t)
	selected, err := fixture.resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"unicode/norm/current_release.go",
		"unicode/norm/normalize.go",
		"unicode/norm/platform_linux_amd64.go",
		"unicode/norm/readwriter.go",
		"unicode/norm/target_experiment.go",
		"unicode/norm/target_feature.go",
	}
	if got := reviewFreezeSortedKeysV1(selected); !reflect.DeepEqual(got, want) {
		t.Fatalf("selected sources=%v want=%v", got, want)
	}
}

// TestW2ReviewFreezeExternalModuleZipResolverV1Adversarial 覆盖 Module 身份、两条 h1、replace/vendor/workspace、路径、重复和未知文件。
func TestW2ReviewFreezeExternalModuleZipResolverV1Adversarial(t *testing.T) {
	prefix := reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1 + "/"
	t.Run("nil artifact loader", func(t *testing.T) {
		fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
		resolver := reviewFreezeNewExternalModuleZipResolverV1(nil)
		if _, err := resolver(fixture.module, fixture.importPath, reviewFreezeExpectedValidatorBuildEnvironmentV1(), fixture.goSum); err == nil || !strings.Contains(err.Error(), "loader 未接入") {
			t.Fatalf("nil loader error=%v", err)
		}
	})
	tests := []struct {
		name   string
		mutate func(*testing.T, *reviewFreezeExternalModuleZipFixtureV1)
		want   string
	}{
		{name: "module path rejected", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.module.ModulePath = "example.invalid/text"
		}, want: "\u53ea\u5141\u8bb8"},
		{name: "module version rejected", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.module.Version = "v0.33.0"
		}, want: "\u53ea\u5141\u8bb8"},
		{name: "unknown package rejected", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.importPath = reviewFreezeXTextModulePathV1 + "/encoding"
		}, want: "\u672a\u77e5 x/text package"},
		{name: "go sum module h1 drift", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.module.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}, want: "go.sum"},
		{name: "go sum go mod h1 drift", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.module.GoModSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}, want: "go.sum"},
		{name: "go sum duplicate", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goSum = append(fixture.goSum, fixture.goSum[:bytes.IndexByte(fixture.goSum, '\n')+1]...)
		}, want: "\u91cd\u590d\u9879"},
		{name: "go sum case ambiguity", mutate: func(_ *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goSum = append(fixture.goSum, []byte(fmt.Sprintf("Golang.org/x/text %s %s\n", fixture.module.Version, fixture.module.ModuleSum))...)
		}, want: "\u5927\u5c0f\u5199\u6b67\u4e49"},
		{name: "replace rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goMod = []byte("module golang.org/x/text\n\ngo 1.24.0\n\nreplace golang.org/x/text => ../text\n")
			fixture.rebind(t)
		}, want: "replace"},
		{name: "go mod module identity rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goMod = []byte("module example.invalid/text\n\ngo 1.24.0\n")
			fixture.rebind(t)
		}, want: "go.mod module path"},
		{name: "zip module content drift", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			reviewFreezeReplaceExternalModuleZipEntryV1(t, fixture, prefix+"unicode/norm/normalize.go", []byte("package norm\n\nvar drift = true\n"))
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "module zip h1"},
		{name: "go mod artifact content drift", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goMod = append(fixture.goMod, []byte("\n// drift\n")...)
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "go.mod h1"},
		{name: "zip and mod artifacts disagree", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.goMod = append(fixture.goMod, []byte("\n// trusted mod artifact\n")...)
			fixture.rebind(t)
		}, want: "\u5b57\u8282\u4e0d\u4e00\u81f4"},
		{name: "wrong zip prefix", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries[0].name = "example.invalid/text@v0.34.0/go.mod"
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "prefix"},
		{name: "path traversal", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/../escape.go", raw: []byte("package norm\n")})
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "\u4e0d\u5b89\u5168\u8def\u5f84"},
		{name: "duplicate zip entry", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, fixture.entries[1])
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "\u91cd\u590d entry"},
		{name: "case ambiguous zip entry", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/Normalize.go", raw: []byte("package norm\n")})
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "\u5927\u5c0f\u5199\u6b67\u4e49"},
		{name: "vendor rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "vendor/example.invalid/dependency.go", raw: []byte("package dependency\n")})
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "vendor"},
		{name: "workspace rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "go.work", raw: []byte("go 1.26\n")})
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "workspace"},
		{name: "nested go mod rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/go.mod", raw: []byte("module example.invalid/norm\n")})
			fixture.moduleZip = reviewFreezeWriteExternalModuleZipFixtureV1(t, fixture.entries)
		}, want: "\u5d4c\u5957"},
		{name: "unknown package file rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/payload.syso", raw: []byte("synthetic")})
			fixture.rebind(t)
		}, want: "\u672a\u77e5\u975e Go build input"},
		{name: "hidden package file rejected", mutate: func(t *testing.T, fixture *reviewFreezeExternalModuleZipFixtureV1) {
			fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/_hidden.go", raw: []byte("package norm\n")})
			fixture.rebind(t)
		}, want: "\u672a\u77e5\u9690\u85cf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
			tc.mutate(t, fixture)
			_, err := fixture.resolve()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// TestW2ReviewFreezeExternalModuleZipResolverV1ClosureExactSet 证明 zip resolver 输出仍必须与 manifest 声明 source exact-set 二次对齐。
func TestW2ReviewFreezeExternalModuleZipResolverV1ClosureExactSet(t *testing.T) {
	manifest, files, _ := reviewFreezeSyntheticBuildClosureFilesV1(t, "r01")
	fixture := reviewFreezeNewExternalModuleZipFixtureV1(t)
	module := &manifest.ValidatorBuildClosure.Entrypoints[0].ExternalModules[0]
	module.ModuleSum = fixture.module.ModuleSum
	module.GoModSum = fixture.module.GoModSum
	files["agent/go.sum"] = append([]byte(nil), fixture.goSum...)
	manifest.ValidatorBuildSources[1].SHA256 = reviewFreezeSHA256V1(files["agent/go.sum"])

	for sourceIndex := range module.Packages[0].Sources {
		path := module.Packages[0].Sources[sourceIndex].Path
		var raw []byte
		for _, entry := range fixture.entries {
			if entry.name == reviewFreezeXTextModulePathV1+"@"+reviewFreezeXTextModuleVersionV1+"/"+path {
				raw = entry.raw
				break
			}
		}
		if raw == nil {
			t.Fatalf("fixture source missing=%s", path)
		}
		module.Packages[0].Sources[sourceIndex].SHA256 = reviewFreezeSHA256V1(raw)
	}
	if err := reviewFreezeValidateValidatorBuildClosureV1(manifest, reviewFreezeMapLoaderV1(files), fixture.resolver()); err != nil {
		t.Fatalf("valid zip-backed closure rejected: %v", err)
	}

	prefix := reviewFreezeXTextModulePathV1 + "@" + reviewFreezeXTextModuleVersionV1 + "/"
	fixture.entries = append(fixture.entries, reviewFreezeExternalModuleZipEntryFixtureV1{name: prefix + "unicode/norm/extra.go", raw: []byte("package norm\n")})
	fixture.rebind(t)
	module.ModuleSum = fixture.module.ModuleSum
	module.GoModSum = fixture.module.GoModSum
	files["agent/go.sum"] = append([]byte(nil), fixture.goSum...)
	manifest.ValidatorBuildSources[1].SHA256 = reviewFreezeSHA256V1(files["agent/go.sum"])
	err := reviewFreezeValidateValidatorBuildClosureV1(manifest, reviewFreezeMapLoaderV1(files), fixture.resolver())
	if err == nil || !strings.Contains(err.Error(), "selected source exact-set") {
		t.Fatalf("extra selected source error=%v", err)
	}
}
