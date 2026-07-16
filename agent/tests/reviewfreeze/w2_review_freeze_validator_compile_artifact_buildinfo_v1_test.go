package reviewfreeze_test

import (
	"bytes"
	"context"
	buildinfo "debug/buildinfo"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	runtimedebug "runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"
)

// reviewFreezeCompileAttestationArtifactBuildInfoResultV1 只表达 verified artifact bytes
// 的 ELF/BuildInfo 语义和三方 projection 绑定结果。后三个字段恒为 false：该校验不证明
// builder 命令实际执行、不证明 artifact 的 source closure，也不授予任何签名 authority。
type reviewFreezeCompileAttestationArtifactBuildInfoResultV1 struct {
	Projection                  reviewFreezeCompileAttestationBuildInfoV1
	ProjectionSHA256            string
	BuilderExecutionProven      bool
	ArtifactSourceClosureProven bool
	SignatureAuthority          bool
}

// reviewFreezeValidateCompileAttestationArtifactBuildInfoV1 只消费 material verifier 已
// 冻结的副本，不重新打开 loader。它把 compiled_test_binary 作为主事实源，再将派生结果
// 与 strict statement 和 build_info_raw parser 做三方一致性校验。
func reviewFreezeValidateCompileAttestationArtifactBuildInfoV1(verified *reviewFreezeVerifiedAttestationMaterialBundleV1) (reviewFreezeCompileAttestationArtifactBuildInfoResultV1, error) {
	var zero reviewFreezeCompileAttestationArtifactBuildInfoResultV1
	if verified == nil {
		return zero, fmt.Errorf("artifact build info verified bundle 不能为空")
	}
	if got, want := verified.Roles(), reviewFreezeCompileDirectMaterialExpectedRolesV1(); !reflect.DeepEqual(got, want) {
		return zero, fmt.Errorf("artifact build info material roles=%v want exact-set=%v", got, want)
	}
	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(verified.StatementRaw())
	if err != nil {
		return zero, fmt.Errorf("artifact build info strict statement: %w", err)
	}

	artifactRef, artifactRaw, exists := verified.Material(reviewFreezeAttestationMaterialRoleArtifactV1)
	if !exists {
		return zero, fmt.Errorf("artifact build info compiled_test_binary material 缺失")
	}
	if !reflect.DeepEqual(artifactRef, statement.BuilderRun.Compile.ArtifactRef) {
		return zero, fmt.Errorf("artifact build info material ref 与 statement artifact_ref 不一致")
	}
	buildInfoRef, buildInfoRaw, exists := verified.Material(reviewFreezeAttestationMaterialRoleBuildInfoV1)
	if !exists {
		return zero, fmt.Errorf("artifact build info build_info_raw material 缺失")
	}
	if !reflect.DeepEqual(buildInfoRef, statement.BuilderRun.Compile.BuildInfoRawRef) {
		return zero, fmt.Errorf("artifact build info material ref 与 statement build_info_raw_ref 不一致")
	}
	return reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(
		artifactRaw,
		buildInfoRaw,
		statement.BuilderRun.Compile,
	)
}

// reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1 验证 typed ref 后，以
// debug/buildinfo.Read(io.ReaderAt) 直接读取 compiled binary。ELF header 约束 amd64，
// BuildInfo 的 GOOS exact-set 约束 linux；二者共同固定 linux/amd64，而不是依赖文件名。
func reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(
	artifactRaw []byte,
	buildInfoRaw []byte,
	compile reviewFreezeCompileAttestationCompileV1,
) (reviewFreezeCompileAttestationArtifactBuildInfoResultV1, error) {
	var zero reviewFreezeCompileAttestationArtifactBuildInfoResultV1
	if len(artifactRaw) == 0 || int64(len(artifactRaw)) > reviewFreezeCompileAttestationArtifactMaxBytesV1 {
		return zero, fmt.Errorf("compiled artifact size=%d limit=%d", len(artifactRaw), reviewFreezeCompileAttestationArtifactMaxBytesV1)
	}
	if compile.ArtifactPath != reviewFreezeCompileAttestationBinaryPathV1 {
		return zero, fmt.Errorf("compiled artifact path=%q want=%q", compile.ArtifactPath, reviewFreezeCompileAttestationBinaryPathV1)
	}
	if compile.ArtifactMode != "0755" {
		return zero, fmt.Errorf("compiled artifact mode=%q want=0755", compile.ArtifactMode)
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		compile.ArtifactRef,
		reviewFreezeCompileAttestationArtifactKindV1,
		reviewFreezeCompileAttestationArtifactContentSchemaV1,
		reviewFreezeCompileAttestationArtifactMediaTypeV1,
		reviewFreezeCompileAttestationArtifactMaxBytesV1,
		"compiled artifact build info",
	); err != nil {
		return zero, err
	}
	wantArtifactSHA := reviewFreezeSHA256V1(artifactRaw)
	if compile.ArtifactRef.SHA256 != wantArtifactSHA || compile.ArtifactRef.SizeBytes != int64(len(artifactRaw)) {
		return zero, fmt.Errorf("compiled artifact ref digest/size=%q/%d want=%q/%d", compile.ArtifactRef.SHA256, compile.ArtifactRef.SizeBytes, wantArtifactSHA, len(artifactRaw))
	}
	if err := reviewFreezeValidateCompileAttestationArtifactELFV1(artifactRaw); err != nil {
		return zero, err
	}

	// bytes.Reader 实现 io.ReaderAt；这里直接走标准库 binary parser，不信任 raw 文本。
	embedded, err := buildinfo.Read(bytes.NewReader(artifactRaw))
	if err != nil {
		return zero, fmt.Errorf("compiled artifact debug/buildinfo.Read: %w", err)
	}
	artifactProjection, err := reviewFreezeProjectCompileAttestationArtifactBuildInfoV1(embedded)
	if err != nil {
		return zero, err
	}
	artifactProjectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(artifactProjection)
	if err != nil {
		return zero, err
	}
	if compile.BuildInfoProjectionSHA256 != artifactProjectionSHA {
		return zero, fmt.Errorf("artifact-derived projection digest=%q statement=%q", artifactProjectionSHA, compile.BuildInfoProjectionSHA256)
	}
	if !reflect.DeepEqual(compile.BuildInfoProjection, artifactProjection) {
		return zero, fmt.Errorf("artifact-derived projection=%+v statement=%+v", artifactProjection, compile.BuildInfoProjection)
	}

	rawProjection, err := reviewFreezeParseCompileAttestationBuildInfoRawV1(buildInfoRaw)
	if err != nil {
		return zero, fmt.Errorf("artifact build info raw parser: %w", err)
	}
	rawProjectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(rawProjection)
	if err != nil {
		return zero, err
	}
	if rawProjectionSHA != artifactProjectionSHA || !reflect.DeepEqual(rawProjection, artifactProjection) {
		return zero, fmt.Errorf("artifact/raw projection mismatch sha=%q/%q artifact=%+v raw=%+v", artifactProjectionSHA, rawProjectionSHA, artifactProjection, rawProjection)
	}
	if err := reviewFreezeValidateCompileAttestationBuildInfoRawV1(buildInfoRaw, compile); err != nil {
		return zero, fmt.Errorf("artifact build info raw binding: %w", err)
	}

	return reviewFreezeCompileAttestationArtifactBuildInfoResultV1{
		Projection:       artifactProjection,
		ProjectionSHA256: artifactProjectionSHA,
		// 这里只关闭 binary/raw/statement 的 BuildInfo 语义 gap；三项 authority 均保留 false。
		BuilderExecutionProven:      false,
		ArtifactSourceClosureProven: false,
		SignatureAuthority:          false,
	}, nil
}

// reviewFreezeValidateCompileAttestationArtifactELFV1 固定 Go internal link 产出的
// 64-bit little-endian x86-64 ET_EXEC。ELF OSABI 本身不能可靠表达 linux，因此 linux
// 仍由 embedded BuildInfo 中 GOOS=linux 的 exact-set 共同确认。
func reviewFreezeValidateCompileAttestationArtifactELFV1(raw []byte) error {
	file, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("compiled artifact ELF parse: %w", err)
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Version != elf.EV_CURRENT {
		return fmt.Errorf("compiled artifact ELF encoding class/data/version=%v/%v/%v", file.Class, file.Data, file.Version)
	}
	if file.Machine != elf.EM_X86_64 {
		return fmt.Errorf("compiled artifact ELF machine=%v want=%v", file.Machine, elf.EM_X86_64)
	}
	if file.Type != elf.ET_EXEC {
		return fmt.Errorf("compiled artifact ELF type=%v want=%v", file.Type, elf.ET_EXEC)
	}
	if file.OSABI != elf.ELFOSABI_NONE || file.ABIVersion != 0 {
		return fmt.Errorf("compiled artifact ELF OSABI/version=%v/%d want=NONE/0", file.OSABI, file.ABIVersion)
	}
	return nil
}

// reviewFreezeProjectCompileAttestationArtifactBuildInfoV1 将标准库 BuildInfo 转为稳定
// DTO，并对 GoVersion、path、main、zero deps 和 build settings 执行 exact-set 校验。
func reviewFreezeProjectCompileAttestationArtifactBuildInfoV1(info *runtimedebug.BuildInfo) (reviewFreezeCompileAttestationBuildInfoV1, error) {
	var zero reviewFreezeCompileAttestationBuildInfoV1
	if info == nil {
		return zero, fmt.Errorf("compiled artifact BuildInfo 不能为空")
	}
	want := reviewFreezeExpectedCompileAttestationBuildInfoV1()
	if info.GoVersion != want.GoVersion {
		return zero, fmt.Errorf("compiled artifact go version=%q want=%q", info.GoVersion, want.GoVersion)
	}
	if info.Path != want.Path {
		return zero, fmt.Errorf("compiled artifact path=%q want=%q", info.Path, want.Path)
	}
	if info.Main.Path != want.MainPath || info.Main.Version != want.MainVersion || info.Main.Sum != "" {
		return zero, fmt.Errorf("compiled artifact main=%q@%q sum=%q want=%q@%q empty-sum", info.Main.Path, info.Main.Version, info.Main.Sum, want.MainPath, want.MainVersion)
	}
	if info.Main.Replace != nil {
		return zero, fmt.Errorf("compiled artifact main replace 禁止=%+v", info.Main.Replace)
	}
	if len(info.Deps) != 0 {
		return zero, fmt.Errorf("compiled artifact dependencies 必须为 zero exact-set got=%d", len(info.Deps))
	}

	wantSettings := make(map[string]string, len(want.Settings))
	for _, setting := range want.Settings {
		if _, duplicate := wantSettings[setting.Name]; duplicate {
			return zero, fmt.Errorf("compiled artifact expected setting 重复=%q", setting.Name)
		}
		wantSettings[setting.Name] = setting.Value
	}
	if len(info.Settings) != len(wantSettings) {
		return zero, fmt.Errorf("compiled artifact build settings count=%d want exact-set=%d", len(info.Settings), len(wantSettings))
	}
	settings := make([]reviewFreezeCompileAttestationBuildSettingV1, 0, len(info.Settings))
	seen := make(map[string]struct{}, len(info.Settings))
	for _, setting := range info.Settings {
		wantValue, known := wantSettings[setting.Key]
		if !known {
			return zero, fmt.Errorf("compiled artifact unknown build setting=%q", setting.Key)
		}
		if _, duplicate := seen[setting.Key]; duplicate {
			return zero, fmt.Errorf("compiled artifact duplicate build setting=%q", setting.Key)
		}
		if setting.Value != wantValue {
			return zero, fmt.Errorf("compiled artifact build setting drift %s=%q want=%q", setting.Key, setting.Value, wantValue)
		}
		seen[setting.Key] = struct{}{}
		settings = append(settings, reviewFreezeCompileAttestationBuildSettingV1{Name: setting.Key, Value: setting.Value})
	}
	for name := range wantSettings {
		if _, exists := seen[name]; !exists {
			return zero, fmt.Errorf("compiled artifact missing build setting=%q", name)
		}
	}
	sort.Slice(settings, func(i, j int) bool { return settings[i].Name < settings[j].Name })
	projection := reviewFreezeCompileAttestationBuildInfoV1{
		SchemaVersion: want.SchemaVersion,
		GoVersion:     info.GoVersion,
		Path:          info.Path,
		MainPath:      info.Main.Path,
		MainVersion:   info.Main.Version,
		Dependencies:  []reviewFreezeCompileAttestationBuildInfoDepV1{},
		Settings:      settings,
	}
	if !reflect.DeepEqual(projection, want) {
		return zero, fmt.Errorf("compiled artifact canonical projection=%+v want=%+v", projection, want)
	}
	return projection, nil
}

func reviewFreezeTestCompileArtifactBuildInfoMaterialV1(t *testing.T) ([]byte, []byte) {
	t.Helper()
	if runtime.Version() != "go1.26.3" {
		t.Fatalf("compile artifact buildinfo golden requires go1.26.3, got %q", runtime.Version())
	}
	moduleRoot := reviewFreezeTestCompileAttestationGoListRawModuleRootV1(t)
	goRoot := filepath.Clean(runtime.GOROOT())
	goBinary := filepath.Join(goRoot, "bin", "go")
	moduleCache := reviewFreezeTestCompileAttestationGoListRawModuleCacheV1(t, goBinary, moduleRoot)
	tempRoot := filepath.Clean(t.TempDir())
	goCache := filepath.Join(tempRoot, "gocache")
	goTmp := filepath.Join(tempRoot, "gotmp")
	outDir := filepath.Join(tempRoot, "out")
	for _, directory := range []string{goCache, goTmp, outDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create compile artifact temp directory %q: %v", directory, err)
		}
	}
	artifactPath := filepath.Join(outDir, "w2r01.test")
	environment := []string{
		"HOME=/nonexistent",
		"TMPDIR=" + goTmp,
		"TZ=UTC",
		"LANG=C",
		"LC_ALL=C",
		"GOROOT=" + goRoot,
		"GOMODCACHE=" + moduleCache,
		"GOCACHE=" + goCache,
		"GOTMPDIR=" + goTmp,
		"GOPATH=/nonexistent/go",
		"GOENV=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOOS=linux",
		"GOARCH=amd64",
		"GOAMD64=v1",
		"CGO_ENABLED=0",
		"GOEXPERIMENT=",
		"GOFIPS140=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOVCS=off",
		"GOAUTH=off",
		"GOTELEMETRY=off",
		"GODEBUG=",
		"GOMAXPROCS=1",
		"GOMEMLIMIT=off",
	}

	buildContext, cancelBuild := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelBuild()
	command := exec.CommandContext(
		buildContext,
		goBinary,
		"test", "-c", "-mod=readonly", "-trimpath", "-buildvcs=false", "-vet=off", "-p=1", "-ldflags=-buildid=",
		"-o", artifactPath, reviewFreezeCompileAttestationPackagePatternV1,
	)
	command.Dir = moduleRoot
	command.Env = environment
	var buildStderr bytes.Buffer
	command.Stderr = &buildStderr
	buildStdout, err := command.Output()
	if err != nil {
		t.Fatalf("run offline go test -c: %v context=%v stderr=%s", err, buildContext.Err(), buildStderr.String())
	}
	if len(buildStdout) != 0 || buildStderr.Len() != 0 {
		t.Fatalf("offline go test -c wrote output stdout=%q stderr=%q", buildStdout, buildStderr.Bytes())
	}
	artifactStat, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("stat compiled artifact: %v", err)
	}
	if artifactStat.Mode().Perm() != 0o755 {
		t.Fatalf("compiled artifact mode=%04o want=0755", artifactStat.Mode().Perm())
	}
	if artifactStat.Size() <= 0 || artifactStat.Size() > reviewFreezeCompileAttestationArtifactMaxBytesV1 {
		t.Fatalf("compiled artifact size=%d limit=%d", artifactStat.Size(), reviewFreezeCompileAttestationArtifactMaxBytesV1)
	}
	artifactRaw, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read compiled artifact: %v", err)
	}

	versionContext, cancelVersion := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelVersion()
	versionCommand := exec.CommandContext(versionContext, goBinary, "version", "-m", artifactPath)
	versionCommand.Dir = moduleRoot
	versionCommand.Env = environment
	var versionStderr bytes.Buffer
	versionCommand.Stderr = &versionStderr
	hostRaw, err := versionCommand.Output()
	if err != nil {
		t.Fatalf("run offline go version -m: %v context=%v stderr=%s", err, versionContext.Err(), versionStderr.String())
	}
	if versionStderr.Len() != 0 {
		t.Fatalf("offline go version -m wrote stderr=%q", versionStderr.Bytes())
	}
	buildInfoRaw := reviewFreezeNormalizeCompileArtifactBuildInfoRawV1(t, hostRaw, artifactPath)
	return artifactRaw, buildInfoRaw
}

// reviewFreezeNormalizeCompileArtifactBuildInfoRawV1 只将 fresh temp 输出的首行物理路径
// 改写为 statement 的隔离逻辑路径；其余真实 go version -m bytes 保持逐字节不变。
func reviewFreezeNormalizeCompileArtifactBuildInfoRawV1(t *testing.T, hostRaw []byte, artifactPath string) []byte {
	t.Helper()
	newline := bytes.IndexByte(hostRaw, '\n')
	if newline < 0 {
		t.Fatalf("go version -m output missing first-line LF: %q", hostRaw)
	}
	wantHostHeader := artifactPath + ": go1.26.3"
	if string(hostRaw[:newline]) != wantHostHeader {
		t.Fatalf("go version -m host header=%q want=%q", hostRaw[:newline], wantHostHeader)
	}
	logicalHeader := []byte(reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3")
	normalized := make([]byte, 0, len(logicalHeader)+len(hostRaw)-newline)
	normalized = append(normalized, logicalHeader...)
	normalized = append(normalized, hostRaw[newline:]...)
	if len(normalized) == 0 || len(normalized) > reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1 {
		t.Fatalf("normalized go version -m size=%d limit=%d", len(normalized), reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1)
	}
	return normalized
}

func reviewFreezeTestBindCompileArtifactV1(raw []byte, compile *reviewFreezeCompileAttestationCompileV1) {
	compile.ArtifactRef = reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationArtifactKindV1,
		reviewFreezeCompileAttestationArtifactContentSchemaV1,
		reviewFreezeCompileAttestationArtifactMediaTypeV1,
		raw,
	)
}

func reviewFreezeTestCompileArtifactBuildInfoBundleV1(t *testing.T, artifactRaw, buildInfoRaw []byte) *reviewFreezeVerifiedAttestationMaterialBundleV1 {
	t.Helper()
	statement, roleBytes := reviewFreezeAttestationMaterialBundleFixtureStatementV1(t)
	roleBytes[reviewFreezeAttestationMaterialRoleArtifactV1] = append([]byte(nil), artifactRaw...)
	roleBytes[reviewFreezeAttestationMaterialRoleBuildInfoV1] = append([]byte(nil), buildInfoRaw...)

	artifactRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.Compile.ArtifactRef, artifactRaw)
	statement.BuilderRun.Compile.ArtifactRef = artifactRef
	statement.BuilderRun.Test.PreExecutionArtifactSHA256 = artifactRef.SHA256
	statement.BuilderRun.Test.PostExecutionArtifactSHA256 = artifactRef.SHA256
	statement.BuilderRun.SBOM.SubjectArtifactSHA256 = artifactRef.SHA256
	buildInfoRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(statement.BuilderRun.Compile.BuildInfoRawRef, buildInfoRaw)
	statement.BuilderRun.Compile.BuildInfoRawRef = buildInfoRef
	statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSHA256 = buildInfoRef.SHA256
	statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSizeBytes = buildInfoRef.SizeBytes

	statementRaw := reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, statement)
	if _, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw); err != nil {
		t.Fatalf("real artifact material bundle statement invalid: %v", err)
	}
	descriptor, err := reviewFreezeAttestationMaterialBundleDescriptorV1(statementRaw, statement)
	if err != nil {
		t.Fatalf("build real artifact material bundle descriptor: %v", err)
	}
	materials := make(map[string][]byte, len(descriptor.Entries))
	for _, entry := range descriptor.Entries {
		materials[entry.Ref.SHA256] = append([]byte(nil), roleBytes[entry.Role]...)
	}
	loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(materials)
	verifyContext, cancelVerify := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelVerify()
	verified, err := reviewFreezeVerifyAttestationMaterialBundleV1(
		verifyContext,
		reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor),
		statementRaw,
		loader,
	)
	if err != nil {
		t.Fatalf("verify real artifact material bundle: %v", err)
	}
	return verified
}

func reviewFreezeTestMutateCompileArtifactBuildInfoStringV1(t *testing.T, raw []byte, old, replacement string) []byte {
	t.Helper()
	if len(old) != len(replacement) {
		t.Fatalf("compiled artifact mutation must preserve length old=%q replacement=%q", old, replacement)
	}
	file, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open ELF for BuildInfo mutation: %v", err)
	}
	defer file.Close()
	section := file.Section(".go.buildinfo")
	if section == nil {
		t.Fatal("compiled artifact missing .go.buildinfo section")
	}
	sectionRaw, err := section.Data()
	if err != nil {
		t.Fatalf("read .go.buildinfo section: %v", err)
	}
	if count := bytes.Count(sectionRaw, []byte(old)); count != 1 {
		t.Fatalf(".go.buildinfo mutation count=%d old=%q", count, old)
	}
	sectionIndex := bytes.Index(sectionRaw, []byte(old))
	if section.Offset > uint64(len(raw)) || uint64(sectionIndex+len(old)) > uint64(len(raw))-section.Offset {
		t.Fatalf(".go.buildinfo mutation offset out of range offset=%d index=%d size=%d", section.Offset, sectionIndex, len(raw))
	}
	mutated := append([]byte(nil), raw...)
	start := int(section.Offset) + sectionIndex
	copy(mutated[start:start+len(replacement)], replacement)
	return mutated
}

func reviewFreezeTestMutateCompileArtifactMachineV1(t *testing.T, raw []byte) []byte {
	t.Helper()
	const elf64MachineOffset = 18
	if len(raw) < elf64MachineOffset+2 {
		t.Fatalf("compiled artifact too short for ELF machine mutation=%d", len(raw))
	}
	mutated := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint16(mutated[elf64MachineOffset:elf64MachineOffset+2], uint16(elf.EM_AARCH64))
	return mutated
}

func reviewFreezeTestCloneRuntimeBuildInfoV1(source *runtimedebug.BuildInfo) *runtimedebug.BuildInfo {
	clone := *source
	clone.Deps = append([]*runtimedebug.Module(nil), source.Deps...)
	clone.Settings = append([]runtimedebug.BuildSetting(nil), source.Settings...)
	return &clone
}

func reviewFreezeTestMutateRuntimeBuildSettingV1(t *testing.T, info *runtimedebug.BuildInfo, key, value string) {
	t.Helper()
	for index := range info.Settings {
		if info.Settings[index].Key == key {
			info.Settings[index].Value = value
			return
		}
	}
	t.Fatalf("runtime BuildInfo setting not found=%q", key)
}

func TestW2ReviewFreezeCompileArtifactBuildInfoV1(t *testing.T) {
	artifactRaw, buildInfoRaw := reviewFreezeTestCompileArtifactBuildInfoMaterialV1(t)
	_, compile := reviewFreezeCompileAttestationBuildInfoRawFixtureV1()
	reviewFreezeTestBindCompileArtifactV1(artifactRaw, &compile)
	reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(buildInfoRaw, &compile)

	t.Run("verified material bundle derives the three-way projection", func(t *testing.T) {
		verified := reviewFreezeTestCompileArtifactBuildInfoBundleV1(t, artifactRaw, buildInfoRaw)
		result, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoV1(verified)
		if err != nil {
			t.Fatalf("valid verified compiled artifact rejected: %v", err)
		}
		want := reviewFreezeExpectedCompileAttestationBuildInfoV1()
		if !reflect.DeepEqual(result.Projection, want) {
			t.Fatalf("artifact-derived projection=%+v want=%+v", result.Projection, want)
		}
		wantSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(want)
		if err != nil {
			t.Fatal(err)
		}
		if result.ProjectionSHA256 != wantSHA {
			t.Fatalf("artifact-derived projection digest=%q want=%q", result.ProjectionSHA256, wantSHA)
		}
		if result.BuilderExecutionProven || result.ArtifactSourceClosureProven || result.SignatureAuthority {
			t.Fatalf("artifact BuildInfo validator overclaimed authority=%+v", result)
		}
	})

	t.Run("path main dependencies and settings are exact sets", func(t *testing.T) {
		embedded, err := buildinfo.Read(bytes.NewReader(artifactRaw))
		if err != nil {
			t.Fatalf("read valid embedded BuildInfo: %v", err)
		}
		if _, err := reviewFreezeProjectCompileAttestationArtifactBuildInfoV1(embedded); err != nil {
			t.Fatalf("valid embedded BuildInfo projection rejected: %v", err)
		}
		tests := []struct {
			name   string
			mutate func(*runtimedebug.BuildInfo)
			want   string
		}{
			{name: "go version drift", mutate: func(info *runtimedebug.BuildInfo) { info.GoVersion = "go1.26.4" }, want: "go version"},
			{name: "path drift", mutate: func(info *runtimedebug.BuildInfo) { info.Path += ".other" }, want: "artifact path"},
			{name: "main path drift", mutate: func(info *runtimedebug.BuildInfo) { info.Main.Path += "/other" }, want: "artifact main"},
			{name: "main version drift", mutate: func(info *runtimedebug.BuildInfo) { info.Main.Version = "v1.0.0" }, want: "artifact main"},
			{name: "main sum injection", mutate: func(info *runtimedebug.BuildInfo) { info.Main.Sum = "h1:not-allowed" }, want: "artifact main"},
			{name: "main replace injection", mutate: func(info *runtimedebug.BuildInfo) {
				info.Main.Replace = &runtimedebug.Module{Path: "example.invalid/replacement", Version: "v1.0.0"}
			}, want: "main replace"},
			{name: "dependency injection", mutate: func(info *runtimedebug.BuildInfo) {
				info.Deps = []*runtimedebug.Module{{Path: "example.invalid/dep", Version: "v1.0.0", Sum: "h1:not-allowed"}}
			}, want: "dependencies"},
			{name: "missing setting", mutate: func(info *runtimedebug.BuildInfo) { info.Settings = info.Settings[:len(info.Settings)-1] }, want: "settings count"},
			{name: "duplicate setting", mutate: func(info *runtimedebug.BuildInfo) { info.Settings[1] = info.Settings[0] }, want: "duplicate build setting"},
			{name: "unknown setting", mutate: func(info *runtimedebug.BuildInfo) { info.Settings[0].Key = "UNKNOWN" }, want: "unknown build setting"},
			{name: "setting value drift", mutate: func(info *runtimedebug.BuildInfo) {
				reviewFreezeTestMutateRuntimeBuildSettingV1(t, info, "GOOS", "darwin")
			}, want: "setting drift"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				candidate := reviewFreezeTestCloneRuntimeBuildInfoV1(embedded)
				test.mutate(candidate)
				_, err := reviewFreezeProjectCompileAttestationArtifactBuildInfoV1(candidate)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("mutated BuildInfo error=%v want substring=%q", err, test.want)
				}
			})
		}
	})

	t.Run("artifact semantic drift is rejected after digest rebinding", func(t *testing.T) {
		tests := []struct {
			name   string
			mutate func(*testing.T, []byte) []byte
			want   string
		}{
			{name: "non ELF", mutate: func(_ *testing.T, _ []byte) []byte { return []byte("not an ELF compiled artifact") }, want: "ELF parse"},
			{name: "truncated ELF", mutate: func(_ *testing.T, raw []byte) []byte { return append([]byte(nil), raw[:len(raw)/2]...) }, want: "ELF"},
			{name: "architecture drift", mutate: reviewFreezeTestMutateCompileArtifactMachineV1, want: "ELF machine"},
			{name: "embedded Go version drift", mutate: func(t *testing.T, raw []byte) []byte {
				return reviewFreezeTestMutateCompileArtifactBuildInfoStringV1(t, raw, "go1.26.3", "go1.26.4")
			}, want: "go version"},
			{name: "embedded setting drift", mutate: func(t *testing.T, raw []byte) []byte {
				return reviewFreezeTestMutateCompileArtifactBuildInfoStringV1(t, raw, "CGO_ENABLED=0", "CGO_ENABLED=1")
			}, want: "setting drift"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				candidateRaw := test.mutate(t, artifactRaw)
				candidateCompile := compile
				reviewFreezeTestBindCompileArtifactV1(candidateRaw, &candidateCompile)
				_, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(candidateRaw, buildInfoRaw, candidateCompile)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("mutated artifact error=%v want substring=%q", err, test.want)
				}
			})
		}
	})

	t.Run("statement and raw drift are rejected by three-way binding", func(t *testing.T) {
		t.Run("statement projection body", func(t *testing.T) {
			candidate := compile
			candidate.BuildInfoProjection.Path += ".other"
			projectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(candidate.BuildInfoProjection)
			if err != nil {
				t.Fatal(err)
			}
			candidate.BuildInfoProjectionSHA256 = projectionSHA
			_, err = reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(artifactRaw, buildInfoRaw, candidate)
			if err == nil || !strings.Contains(err.Error(), "projection digest") {
				t.Fatalf("statement projection drift error=%v", err)
			}
		})
		t.Run("statement projection digest", func(t *testing.T) {
			candidate := compile
			candidate.BuildInfoProjectionSHA256 = reviewFreezeSHA256V1([]byte("other projection"))
			_, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(artifactRaw, buildInfoRaw, candidate)
			if err == nil || !strings.Contains(err.Error(), "projection digest") {
				t.Fatalf("statement projection digest drift error=%v", err)
			}
		})
		t.Run("raw text", func(t *testing.T) {
			candidateRaw := reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, buildInfoRaw, "GOOS=linux", "GOOS=darwin")
			candidate := compile
			reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(candidateRaw, &candidate)
			_, err := reviewFreezeValidateCompileAttestationArtifactBuildInfoBytesV1(artifactRaw, candidateRaw, candidate)
			if err == nil || !strings.Contains(err.Error(), "raw parser") {
				t.Fatalf("build info raw drift error=%v", err)
			}
		})
	})
}
