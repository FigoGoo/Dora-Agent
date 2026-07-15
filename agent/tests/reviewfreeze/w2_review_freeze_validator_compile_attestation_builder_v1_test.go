package reviewfreeze_test

import (
	"encoding/json"
	"fmt"
	pathpkg "path"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const (
	reviewFreezeCompileAttestationBuilderTestCWDV1        = "/workspace/agent/tests/contract/w2r01"
	reviewFreezeCompileAttestationBuilderSBOMCWDV1        = "/work"
	reviewFreezeCompileAttestationBuilderSBOMNameV1       = "dora-w2-sbom"
	reviewFreezeCompileAttestationBuilderSBOMVersionV1    = "v1"
	reviewFreezeCompileAttestationBuilderSBOMExecutableV1 = "/opt/dora/bin/dora-w2-sbom"
)

var reviewFreezeCompileAttestationBuilderIDV1 = regexp.MustCompile("^[a-z][a-z0-9-]{0,62}[a-z0-9]$")

// reviewFreezeValidateCompileAttestationBuilderRunV1 只校验一个 clean builder run 的执行
// claim 结构与内部一致性，不证明命令实际发生或 content ref 所指字节存在。双 builder
// 配对、material 复核、独立 issuer 与跨 run 复现性由后续 verifier 完成。
func reviewFreezeValidateCompileAttestationBuilderRunV1(run reviewFreezeCompileAttestationBuilderRunV1, goListProjectionSHA string, toolchain reviewFreezeCompileAttestationToolchainV1) error {
	if !reviewFreezePrefixedSHA256V1.MatchString(goListProjectionSHA) {
		return fmt.Errorf("compile attestation builder go list projection digest 非法=%q", goListProjectionSHA)
	}
	for _, identity := range []struct {
		name  string
		value string
	}{
		{name: "builder_id", value: run.BuilderID},
		{name: "workspace_id", value: run.WorkspaceID},
		{name: "module_cache_id", value: run.ModuleCacheID},
		{name: "build_cache_id", value: run.BuildCacheID},
	} {
		if !reviewFreezeCompileAttestationBuilderIDV1.MatchString(identity.value) {
			return fmt.Errorf("compile attestation %s 非法=%q", identity.name, identity.value)
		}
	}
	if run.BuilderImageDigest != toolchain.BuilderImageDigest || run.RunnerImageDigest != toolchain.RunnerImageDigest {
		return fmt.Errorf("image digest 未绑定 toolchain builder=%q/%q runner=%q/%q", run.BuilderImageDigest, toolchain.BuilderImageDigest, run.RunnerImageDigest, toolchain.RunnerImageDigest)
	}
	if run.GoListProjectionSHA != goListProjectionSHA {
		return fmt.Errorf("go list projection digest=%q want=%q", run.GoListProjectionSHA, goListProjectionSHA)
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		run.InputSnapshotBeforeRef,
		reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
		reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
		reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
		reviewFreezeCompileAttestationInputSnapshotMaxBytesV1,
		"input snapshot before",
	); err != nil {
		return err
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		run.InputSnapshotAfterRef,
		reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
		reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
		reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
		reviewFreezeCompileAttestationInputSnapshotMaxBytesV1,
		"input snapshot after",
	); err != nil {
		return err
	}
	if !reflect.DeepEqual(run.InputSnapshotBeforeRef, run.InputSnapshotAfterRef) {
		return fmt.Errorf("input snapshot pre/post=%+v/%+v", run.InputSnapshotBeforeRef, run.InputSnapshotAfterRef)
	}
	if err := reviewFreezeValidateCompileAttestationBuilderGoListInvocationV1(run.GoListInvocation, run.GoListRawRef); err != nil {
		return err
	}

	wantVersionArgv := []string{"/opt/go/bin/go", "version"}
	versionOutput := []byte("go version go1.26.3 linux/amd64\n")
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(run.ToolchainVersion, wantVersionArgv, reviewFreezeCompileAttestationLogicalCWDV1, reviewFreezeSHA256V1(versionOutput), int64(len(versionOutput)), reviewFreezeSHA256V1(nil), 0, "go version"); err != nil {
		return err
	}

	wantCompileArgv := []string{
		"/opt/go/bin/go", "test", "-c", "-mod=readonly", "-trimpath", "-buildvcs=false", "-vet=off", "-p=1", "-ldflags=-buildid=",
		"-o", reviewFreezeCompileAttestationBinaryPathV1, reviewFreezeCompileAttestationPackagePatternV1,
	}
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(run.Compile.Invocation, wantCompileArgv, reviewFreezeCompileAttestationLogicalCWDV1, reviewFreezeSHA256V1(nil), 0, reviewFreezeSHA256V1(nil), 0, "go test -c"); err != nil {
		return err
	}
	if run.Compile.ArtifactPath != reviewFreezeCompileAttestationBinaryPathV1 {
		return fmt.Errorf("compile artifact_path=%q want=%q", run.Compile.ArtifactPath, reviewFreezeCompileAttestationBinaryPathV1)
	}
	if run.Compile.ArtifactMode != "0755" {
		return fmt.Errorf("compile artifact_mode=%q want=0755", run.Compile.ArtifactMode)
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		run.Compile.ArtifactRef,
		reviewFreezeCompileAttestationArtifactKindV1,
		reviewFreezeCompileAttestationArtifactContentSchemaV1,
		reviewFreezeCompileAttestationArtifactMediaTypeV1,
		reviewFreezeCompileAttestationArtifactMaxBytesV1,
		"compile artifact",
	); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationBuilderBuildInfoV1(run.Compile); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationBuilderTestV1(run.Test, run.Compile.ArtifactRef.SHA256); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationBuilderSBOMV1(run.SBOM, run.Compile.ArtifactRef.SHA256); err != nil {
		return err
	}
	return nil
}

// reviewFreezeValidateCompileAttestationBuilderInvocationV1 固定可执行文件、argv、cwd、退出码与
// 输出摘要；绝对 executable 使 PATH 或 shell 包装无法替换受信 Go/binary。
func reviewFreezeValidateCompileAttestationBuilderInvocationV1(invocation reviewFreezeCompileAttestationInvocationV1, wantArgv []string, wantCWD, wantStdoutSHA string, wantStdoutSize int64, wantStderrSHA string, wantStderrSize int64, step string) error {
	if len(invocation.Argv) == 0 || !pathpkg.IsAbs(invocation.Argv[0]) || !reflect.DeepEqual(invocation.Argv, wantArgv) {
		return fmt.Errorf("%s argv=%v want=%v", step, invocation.Argv, wantArgv)
	}
	if invocation.CWD != wantCWD {
		return fmt.Errorf("%s cwd=%q want=%q", step, invocation.CWD, wantCWD)
	}
	if invocation.ExitCode != 0 {
		return fmt.Errorf("%s exit_code=%d", step, invocation.ExitCode)
	}
	if invocation.StdoutSHA256 != wantStdoutSHA || invocation.StdoutSizeBytes != wantStdoutSize || invocation.StderrSHA256 != wantStderrSHA || invocation.StderrSizeBytes != wantStderrSize {
		return fmt.Errorf("%s stdout/stderr digest+size=%q/%d %q/%d want=%q/%d %q/%d", step, invocation.StdoutSHA256, invocation.StdoutSizeBytes, invocation.StderrSHA256, invocation.StderrSizeBytes, wantStdoutSHA, wantStdoutSize, wantStderrSHA, wantStderrSize)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationBuilderGoListInvocationV1 把每次 go list 的 stdout
// 与 typed raw ref 绑定；后续受信 adapter 必须按摘要取得 raw stream 并重算 canonical projection。
func reviewFreezeValidateCompileAttestationBuilderGoListInvocationV1(invocation reviewFreezeCompileAttestationInvocationV1, rawRef reviewFreezeAttestationContentRefV1) error {
	if err := reviewFreezeValidateAttestationContentRefV1(
		rawRef,
		reviewFreezeCompileAttestationGoListRawArtifactKindV1,
		reviewFreezeCompileAttestationGoListRawContentSchemaV1,
		reviewFreezeCompileAttestationGoListRawMediaTypeV1,
		reviewFreezeCompileAttestationGoListRawMaxBytesV1,
		"go list stdout",
	); err != nil {
		return err
	}
	wantArgv := []string{
		"/opt/go/bin/go", "list", "-deps", "-test", "-compiled", "-json", "-mod=readonly", "-trimpath", "-p=1", reviewFreezeCompileAttestationPackagePatternV1,
	}
	return reviewFreezeValidateCompileAttestationBuilderInvocationV1(
		invocation,
		wantArgv,
		reviewFreezeCompileAttestationLogicalCWDV1,
		rawRef.SHA256,
		rawRef.SizeBytes,
		reviewFreezeSHA256V1(nil),
		0,
		"go list",
	)
}

// reviewFreezeExpectedCompileAttestationBuildInfoV1 固定经本地 Go 1.26.3 linux/amd64
// test binary 探针校准的目标投影；settings 按 name 字节序排序，依赖必须显式为空。
func reviewFreezeExpectedCompileAttestationBuildInfoV1() reviewFreezeCompileAttestationBuildInfoV1 {
	return reviewFreezeCompileAttestationBuildInfoV1{
		SchemaVersion: reviewFreezeCompileAttestationBuildInfoProjectionSchemaV1,
		GoVersion:     "go1.26.3",
		Path:          reviewFreezeCompileAttestationTargetImportV1 + ".test",
		MainPath:      reviewFreezeCompileAttestationModulePathV1,
		MainVersion:   "(devel)",
		Dependencies:  []reviewFreezeCompileAttestationBuildInfoDepV1{},
		Settings: []reviewFreezeCompileAttestationBuildSettingV1{
			{Name: "-buildmode", Value: "exe"},
			{Name: "-compiler", Value: "gc"},
			{Name: "-trimpath", Value: "true"},
			{Name: "CGO_ENABLED", Value: "0"},
			{Name: "GOAMD64", Value: "v1"},
			{Name: "GOARCH", Value: "amd64"},
			{Name: "GOOS", Value: "linux"},
		},
	}
}

// reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1 使用 Go DTO 的固定字段顺序重算
// canonical JSON 摘要，避免接受与嵌入 projection 无关的自报 digest。
func reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(projection reviewFreezeCompileAttestationBuildInfoV1) (string, error) {
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("编码 build info projection: %w", err)
	}
	return reviewFreezeSHA256V1(raw), nil
}

// reviewFreezeValidateCompileAttestationBuilderBuildInfoV1 固定版本探针命令、raw ref 与
// canonical projection，禁止只附一个无法复核来源的 buildinfo 摘要。
func reviewFreezeValidateCompileAttestationBuilderBuildInfoV1(compile reviewFreezeCompileAttestationCompileV1) error {
	if err := reviewFreezeValidateAttestationContentRefV1(
		compile.BuildInfoRawRef,
		reviewFreezeCompileAttestationBuildInfoRawArtifactKindV1,
		reviewFreezeCompileAttestationBuildInfoRawContentSchemaV1,
		reviewFreezeCompileAttestationBuildInfoRawMediaTypeV1,
		reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1,
		"build info stdout",
	); err != nil {
		return err
	}
	wantArgv := []string{"/opt/go/bin/go", "version", "-m", reviewFreezeCompileAttestationBinaryPathV1}
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(
		compile.BuildInfoInvocation,
		wantArgv,
		reviewFreezeCompileAttestationLogicalCWDV1,
		compile.BuildInfoRawRef.SHA256,
		compile.BuildInfoRawRef.SizeBytes,
		reviewFreezeSHA256V1(nil),
		0,
		"go version -m",
	); err != nil {
		return err
	}
	wantProjectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(compile.BuildInfoProjection)
	if err != nil {
		return err
	}
	if compile.BuildInfoProjectionSHA256 != wantProjectionSHA {
		return fmt.Errorf("build info projection digest=%q want=%q", compile.BuildInfoProjectionSHA256, wantProjectionSHA)
	}
	wantProjection := reviewFreezeExpectedCompileAttestationBuildInfoV1()
	if !reflect.DeepEqual(compile.BuildInfoProjection, wantProjection) {
		return fmt.Errorf("build info projection 漂移=%+v want=%+v", compile.BuildInfoProjection, wantProjection)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationBuilderTestV1 固定“只运行已编译 binary”的
// anchored run claim 及 R01 manifest 四个测试入口；sandbox claim 缺一即失败关闭。
func reviewFreezeValidateCompileAttestationBuilderTestV1(testRun reviewFreezeCompileAttestationTestV1, artifactSHA string) error {
	wantEntrypoints := []string{
		"TestGraphToolResultV1Corpus",
		"TestToolReceiptV1Corpus",
		"TestW2R01CorpusManifest",
		"TestWarningIntegerPolicySafeBoundaryV1",
	}
	if !reflect.DeepEqual(testRun.TestEntrypoints, wantEntrypoints) {
		return fmt.Errorf("test_entrypoints=%v want=%v", testRun.TestEntrypoints, wantEntrypoints)
	}
	wantTestArgv := []string{
		reviewFreezeCompileAttestationBinaryPathV1,
		"-test.run",
		"^(TestGraphToolResultV1Corpus|TestToolReceiptV1Corpus|TestW2R01CorpusManifest|TestWarningIntegerPolicySafeBoundaryV1)$",
		"-test.count=1",
	}
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(testRun.Invocation, wantTestArgv, reviewFreezeCompileAttestationBuilderTestCWDV1, reviewFreezeSHA256V1([]byte("PASS\n")), 5, reviewFreezeSHA256V1(nil), 0, "compiled test binary"); err != nil {
		return err
	}
	if testRun.PreExecutionArtifactSHA256 != artifactSHA || testRun.PostExecutionArtifactSHA256 != artifactSHA {
		return fmt.Errorf("test pre/post artifact digest=%q/%q want=%q", testRun.PreExecutionArtifactSHA256, testRun.PostExecutionArtifactSHA256, artifactSHA)
	}
	if err := reviewFreezeValidateCompileAttestationBuilderNonEmptyDigestV1(testRun.SandboxPolicySHA256, "sandbox policy"); err != nil {
		return err
	}
	if testRun.NetworkPolicy != "off" || testRun.SecretPolicy != "none" || testRun.SourceFilesystemPolicy != "read_only" || testRun.ModuleCacheFilesystemPolicy != "readonly_verified" {
		return fmt.Errorf("test sandbox/network/secret/filesystem policy 漂移=%+v", testRun)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationBuilderSBOMV1 只允许声明项目自有的确定性生成器；
// helper 实现、raw 解析和确定性仍由 Batch 3C 复核，Syft 随机输出不进入该契约。
func reviewFreezeValidateCompileAttestationBuilderSBOMV1(sbom reviewFreezeCompileAttestationSBOMV1, artifactSHA string) error {
	if sbom.Format != "cyclonedx-json" || sbom.FormatVersion != "1.6" {
		return fmt.Errorf("SBOM format=%q version=%q", sbom.Format, sbom.FormatVersion)
	}
	if sbom.GeneratorName != reviewFreezeCompileAttestationBuilderSBOMNameV1 || sbom.GeneratorVersion != reviewFreezeCompileAttestationBuilderSBOMVersionV1 {
		return fmt.Errorf("SBOM generator identity=%q@%q want=%q@%q", sbom.GeneratorName, sbom.GeneratorVersion, reviewFreezeCompileAttestationBuilderSBOMNameV1, reviewFreezeCompileAttestationBuilderSBOMVersionV1)
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		sbom.GeneratorBinaryRef,
		reviewFreezeCompileAttestationSBOMGeneratorArtifactKindV1,
		reviewFreezeCompileAttestationSBOMGeneratorContentSchemaV1,
		reviewFreezeCompileAttestationSBOMGeneratorMediaTypeV1,
		reviewFreezeCompileAttestationSBOMGeneratorMaxBytesV1,
		"SBOM generator binary",
	); err != nil {
		return err
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		sbom.RawRef,
		reviewFreezeCompileAttestationSBOMRawArtifactKindV1,
		reviewFreezeCompileAttestationSBOMRawContentSchemaV1,
		reviewFreezeCompileAttestationSBOMRawMediaTypeV1,
		reviewFreezeCompileAttestationSBOMRawMaxBytesV1,
		"SBOM raw",
	); err != nil {
		return err
	}
	wantArgv := []string{
		reviewFreezeCompileAttestationBuilderSBOMExecutableV1,
		"generate",
		"--input", reviewFreezeCompileAttestationBinaryPathV1,
		"--format", "cyclonedx-json-1.6",
		"--deterministic",
	}
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(
		sbom.Invocation,
		wantArgv,
		reviewFreezeCompileAttestationBuilderSBOMCWDV1,
		sbom.RawRef.SHA256,
		sbom.RawRef.SizeBytes,
		reviewFreezeSHA256V1(nil),
		0,
		"SBOM deterministic generator",
	); err != nil {
		return err
	}
	if sbom.SubjectArtifactSHA256 != artifactSHA {
		return fmt.Errorf("SBOM subject artifact=%q want=%q", sbom.SubjectArtifactSHA256, artifactSHA)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationBuilderNonEmptyDigestV1 拒绝格式错误与空字节摘要。
func reviewFreezeValidateCompileAttestationBuilderNonEmptyDigestV1(digest, field string) error {
	if !reviewFreezePrefixedSHA256V1.MatchString(digest) || digest == reviewFreezeSHA256V1(nil) {
		return fmt.Errorf("compile attestation builder %s digest 非法或为空=%q", field, digest)
	}
	return nil
}

func reviewFreezeCompileAttestationBuilderContentRefFixtureV1(artifactKind, contentSchema, mediaType string, raw []byte) reviewFreezeAttestationContentRefV1 {
	return reviewFreezeAttestationContentRefV1{
		RefSchemaVersion:     reviewFreezeAttestationContentRefSchemaV1,
		ArtifactKind:         artifactKind,
		ContentSchemaVersion: contentSchema,
		MediaType:            mediaType,
		SHA256:               reviewFreezeSHA256V1(raw),
		SizeBytes:            int64(len(raw)),
	}
}

// reviewFreezeValidateCompileAttestationBuilderFixtureV1 创建两个可由后续 pair evaluator
// 配对的独立单-run fixture；本文件只逐个验证，不在一个 statement 内聚合或互信。
func reviewFreezeValidateCompileAttestationBuilderFixtureV1() ([]reviewFreezeCompileAttestationBuilderRunV1, string, reviewFreezeCompileAttestationToolchainV1) {
	projectionSHA := reviewFreezeSHA256V1([]byte("canonical go list projection"))
	artifactRaw := []byte("compiled w2r01 test artifact")
	artifactRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationArtifactKindV1,
		reviewFreezeCompileAttestationArtifactContentSchemaV1,
		reviewFreezeCompileAttestationArtifactMediaTypeV1,
		artifactRaw,
	)
	inputSnapshotRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
		reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
		reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
		[]byte("stable compile input snapshot"),
	)
	buildInfoRaw := []byte(
		reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3\n" +
			"\tpath\t" + reviewFreezeCompileAttestationTargetImportV1 + ".test\n" +
			"\tmod\t" + reviewFreezeCompileAttestationModulePathV1 + "\t(devel)\t\n" +
			"\tbuild\t-buildmode=exe\n" +
			"\tbuild\t-compiler=gc\n" +
			"\tbuild\t-trimpath=true\n" +
			"\tbuild\tCGO_ENABLED=0\n" +
			"\tbuild\tGOARCH=amd64\n" +
			"\tbuild\tGOOS=linux\n" +
			"\tbuild\tGOAMD64=v1\n",
	)
	buildInfoRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationBuildInfoRawArtifactKindV1,
		reviewFreezeCompileAttestationBuildInfoRawContentSchemaV1,
		reviewFreezeCompileAttestationBuildInfoRawMediaTypeV1,
		buildInfoRaw,
	)
	buildInfoProjection := reviewFreezeExpectedCompileAttestationBuildInfoV1()
	buildInfoProjectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(buildInfoProjection)
	if err != nil {
		panic(err)
	}
	sbomRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationSBOMRawArtifactKindV1,
		reviewFreezeCompileAttestationSBOMRawContentSchemaV1,
		reviewFreezeCompileAttestationSBOMRawMediaTypeV1,
		[]byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`),
	)
	sbomGeneratorRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationSBOMGeneratorArtifactKindV1,
		reviewFreezeCompileAttestationSBOMGeneratorContentSchemaV1,
		reviewFreezeCompileAttestationSBOMGeneratorMediaTypeV1,
		[]byte("dora-w2-sbom binary"),
	)
	sandboxSHA := reviewFreezeSHA256V1([]byte("sandbox policy"))
	toolchain := reviewFreezeCompileAttestationToolchainV1{
		BuilderImageDigest: reviewFreezeSHA256V1([]byte("builder image")),
		RunnerImageDigest:  reviewFreezeSHA256V1([]byte("runner image")),
	}
	newRun := func(builderID string) reviewFreezeCompileAttestationBuilderRunV1 {
		suffix := strings.TrimPrefix(builderID, "builder-")
		// 这里只为 statement Schema 测试提供可区分、语法合法的 JSON stream placeholder；
		// 它不是完整 go-list material，真实 parser/golden 由 Batch 3C 独立验证。
		goListOutput := []byte(`{"ImportPath":"schema.invalid/` + suffix + `","Name":"fixture"}` + "\n")
		goListRef := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
			reviewFreezeCompileAttestationGoListRawArtifactKindV1,
			reviewFreezeCompileAttestationGoListRawContentSchemaV1,
			reviewFreezeCompileAttestationGoListRawMediaTypeV1,
			goListOutput,
		)
		return reviewFreezeCompileAttestationBuilderRunV1{
			BuilderID:              builderID,
			WorkspaceID:            "workspace-" + suffix,
			ModuleCacheID:          "module-cache-" + suffix,
			BuildCacheID:           "build-cache-" + suffix,
			BuilderImageDigest:     toolchain.BuilderImageDigest,
			RunnerImageDigest:      toolchain.RunnerImageDigest,
			InputSnapshotBeforeRef: inputSnapshotRef,
			InputSnapshotAfterRef:  inputSnapshotRef,
			GoListInvocation: reviewFreezeCompileAttestationInvocationV1{
				Argv: []string{"/opt/go/bin/go", "list", "-deps", "-test", "-compiled", "-json", "-mod=readonly", "-trimpath", "-p=1", reviewFreezeCompileAttestationPackagePatternV1},
				CWD:  reviewFreezeCompileAttestationLogicalCWDV1, StdoutSHA256: goListRef.SHA256, StdoutSizeBytes: goListRef.SizeBytes,
				StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
			},
			GoListRawRef:        goListRef,
			GoListProjectionSHA: projectionSHA,
			ToolchainVersion: reviewFreezeCompileAttestationInvocationV1{
				Argv: []string{"/opt/go/bin/go", "version"}, CWD: reviewFreezeCompileAttestationLogicalCWDV1,
				StdoutSHA256: reviewFreezeSHA256V1([]byte("go version go1.26.3 linux/amd64\n")), StdoutSizeBytes: int64(len("go version go1.26.3 linux/amd64\n")),
				StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
			},
			Compile: reviewFreezeCompileAttestationCompileV1{
				Invocation: reviewFreezeCompileAttestationInvocationV1{
					Argv: []string{"/opt/go/bin/go", "test", "-c", "-mod=readonly", "-trimpath", "-buildvcs=false", "-vet=off", "-p=1", "-ldflags=-buildid=", "-o", reviewFreezeCompileAttestationBinaryPathV1, reviewFreezeCompileAttestationPackagePatternV1},
					CWD:  reviewFreezeCompileAttestationLogicalCWDV1, StdoutSHA256: reviewFreezeSHA256V1(nil), StdoutSizeBytes: 0,
					StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
				},
				ArtifactPath: reviewFreezeCompileAttestationBinaryPathV1,
				ArtifactMode: "0755",
				ArtifactRef:  artifactRef,
				BuildInfoInvocation: reviewFreezeCompileAttestationInvocationV1{
					Argv: []string{"/opt/go/bin/go", "version", "-m", reviewFreezeCompileAttestationBinaryPathV1},
					CWD:  reviewFreezeCompileAttestationLogicalCWDV1, StdoutSHA256: buildInfoRef.SHA256, StdoutSizeBytes: buildInfoRef.SizeBytes,
					StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
				},
				BuildInfoRawRef:           buildInfoRef,
				BuildInfoProjection:       buildInfoProjection,
				BuildInfoProjectionSHA256: buildInfoProjectionSHA,
			},
			Test: reviewFreezeCompileAttestationTestV1{
				Invocation: reviewFreezeCompileAttestationInvocationV1{
					Argv: []string{reviewFreezeCompileAttestationBinaryPathV1, "-test.run", "^(TestGraphToolResultV1Corpus|TestToolReceiptV1Corpus|TestW2R01CorpusManifest|TestWarningIntegerPolicySafeBoundaryV1)$", "-test.count=1"},
					CWD:  reviewFreezeCompileAttestationBuilderTestCWDV1, StdoutSHA256: reviewFreezeSHA256V1([]byte("PASS\n")), StdoutSizeBytes: 5,
					StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
				},
				TestEntrypoints:            []string{"TestGraphToolResultV1Corpus", "TestToolReceiptV1Corpus", "TestW2R01CorpusManifest", "TestWarningIntegerPolicySafeBoundaryV1"},
				PreExecutionArtifactSHA256: artifactRef.SHA256, PostExecutionArtifactSHA256: artifactRef.SHA256, SandboxPolicySHA256: sandboxSHA,
				NetworkPolicy: "off", SecretPolicy: "none", SourceFilesystemPolicy: "read_only", ModuleCacheFilesystemPolicy: "readonly_verified",
			},
			SBOM: reviewFreezeCompileAttestationSBOMV1{
				Format: "cyclonedx-json", FormatVersion: "1.6", GeneratorName: reviewFreezeCompileAttestationBuilderSBOMNameV1, GeneratorVersion: reviewFreezeCompileAttestationBuilderSBOMVersionV1,
				GeneratorBinaryRef: sbomGeneratorRef,
				Invocation: reviewFreezeCompileAttestationInvocationV1{
					Argv: []string{reviewFreezeCompileAttestationBuilderSBOMExecutableV1, "generate", "--input", reviewFreezeCompileAttestationBinaryPathV1, "--format", "cyclonedx-json-1.6", "--deterministic"},
					CWD:  reviewFreezeCompileAttestationBuilderSBOMCWDV1, StdoutSHA256: sbomRef.SHA256, StdoutSizeBytes: sbomRef.SizeBytes,
					StderrSHA256: reviewFreezeSHA256V1(nil), StderrSizeBytes: 0,
				},
				SubjectArtifactSHA256: artifactRef.SHA256,
				RawRef:                sbomRef,
			},
		}
	}
	runs := []reviewFreezeCompileAttestationBuilderRunV1{newRun("builder-a"), newRun("builder-b")}
	// Workspace 与两个 cache 是独立分配的实例身份；正例刻意让它们和 BuilderID 反向，
	// 防止 pair evaluator 把“跨 run 不复用”误收紧成无业务依据的同向排序。
	runs[0].WorkspaceID, runs[1].WorkspaceID = runs[1].WorkspaceID, runs[0].WorkspaceID
	runs[0].ModuleCacheID, runs[1].ModuleCacheID = runs[1].ModuleCacheID, runs[0].ModuleCacheID
	runs[0].BuildCacheID, runs[1].BuildCacheID = runs[1].BuildCacheID, runs[0].BuildCacheID
	return runs, projectionSHA, toolchain
}

func reviewFreezeRefreshCompileAttestationBuildInfoProjectionDigestV1(t *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
	t.Helper()
	digest, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(run.Compile.BuildInfoProjection)
	if err != nil {
		t.Fatalf("refresh build info projection digest: %v", err)
	}
	run.Compile.BuildInfoProjectionSHA256 = digest
}

func TestW2ReviewFreezeCompileAttestationBuilderRunV1(t *testing.T) {
	runs, projectionSHA, toolchain := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
	for _, run := range runs {
		run := run
		t.Run(run.BuilderID, func(t *testing.T) {
			if err := reviewFreezeValidateCompileAttestationBuilderRunV1(run, projectionSHA, toolchain); err != nil {
				t.Fatalf("valid builder run rejected: %v", err)
			}
		})
	}
}

func TestW2ReviewFreezeCompileAttestationBuilderIdentityAdversarialV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileAttestationBuilderRunV1, *string, *reviewFreezeCompileAttestationToolchainV1)
		want   string
	}{
		{name: "invalid builder", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.BuilderID = "1"
		}, want: "builder_id 非法"},
		{name: "invalid workspace", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.WorkspaceID = "workspace/escape"
		}, want: "workspace_id 非法"},
		{name: "invalid module cache", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.ModuleCacheID = "module_cache"
		}, want: "module_cache_id 非法"},
		{name: "invalid build cache", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.BuildCacheID = "build cache"
		}, want: "build_cache_id 非法"},
		{name: "builder image drift", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.BuilderImageDigest = reviewFreezeSHA256V1([]byte("other image"))
		}, want: "image digest"},
		{name: "projection drift", mutate: func(run *reviewFreezeCompileAttestationBuilderRunV1, _ *string, _ *reviewFreezeCompileAttestationToolchainV1) {
			run.GoListProjectionSHA = reviewFreezeSHA256V1([]byte("other projection"))
		}, want: "projection digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runs, projectionSHA, toolchain := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
			run := runs[0]
			test.mutate(&run, &projectionSHA, &toolchain)
			err := reviewFreezeValidateCompileAttestationBuilderRunV1(run, projectionSHA, toolchain)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileAttestationBuilderExecutionAdversarialV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *reviewFreezeCompileAttestationBuilderRunV1)
		want   string
	}{
		{name: "snapshot schema drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.InputSnapshotBeforeRef.ContentSchemaVersion = "w2_compile_input_snapshot.v2"
		}, want: "input snapshot before content ref identity"},
		{name: "snapshot changed during run", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.InputSnapshotAfterRef.SHA256 = reviewFreezeSHA256V1([]byte("changed snapshot"))
		}, want: "input snapshot pre/post"},
		{name: "go list argv drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.GoListInvocation.Argv[1] = "env"
		}, want: "go list argv"},
		{name: "go list raw type drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.GoListRawRef.MediaType = "application/json"
		}, want: "go list stdout content ref identity"},
		{name: "go list raw mismatch", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.GoListRawRef.SHA256 = reviewFreezeSHA256V1([]byte("other raw"))
		}, want: "go list stdout/stderr"},
		{name: "go list failed", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) { run.GoListInvocation.ExitCode = 1 }, want: "go list exit_code"},
		{name: "go list stderr", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.GoListInvocation.StderrSHA256 = reviewFreezeSHA256V1([]byte("warning"))
			run.GoListInvocation.StderrSizeBytes = 7
		}, want: "go list stdout/stderr"},
		{name: "relative go executable", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.ToolchainVersion.Argv[0] = "go"
		}, want: "go version argv"},
		{name: "go version size drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.ToolchainVersion.StdoutSizeBytes++
		}, want: "go version stdout/stderr"},
		{name: "compile output drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.Invocation.Argv[10] = "/out/other.test"
		}, want: "go test -c argv"},
		{name: "compile failed", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.Invocation.ExitCode = 1
		}, want: "go test -c exit_code"},
		{name: "artifact mode", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) { run.Compile.ArtifactMode = "0644" }, want: "artifact_mode"},
		{name: "artifact ref kind", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.ArtifactRef.ArtifactKind = "source_archive"
		}, want: "compile artifact content ref identity"},
		{name: "artifact ref empty", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.ArtifactRef.SizeBytes = 0
		}, want: "compile artifact content ref size"},
		{name: "build info argv", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoInvocation.Argv[2] = "-json"
		}, want: "go version -m argv"},
		{name: "build info raw schema", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoRawRef.ContentSchemaVersion = "go1.26.3_version_m_json.v1"
		}, want: "build info stdout content ref identity"},
		{name: "build info raw mismatch", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoRawRef.SHA256 = reviewFreezeSHA256V1([]byte("other build info raw"))
		}, want: "go version -m stdout/stderr"},
		{name: "build info projection digest", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoProjectionSHA256 = reviewFreezeSHA256V1([]byte("other projection"))
		}, want: "build info projection digest"},
		{name: "build info projection semantic drift", mutate: func(t *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoProjection.Path = "example.invalid/other.test"
			reviewFreezeRefreshCompileAttestationBuildInfoProjectionDigestV1(t, run)
		}, want: "build info projection 漂移"},
		{name: "build info settings order", mutate: func(t *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Compile.BuildInfoProjection.Settings[0], run.Compile.BuildInfoProjection.Settings[1] = run.Compile.BuildInfoProjection.Settings[1], run.Compile.BuildInfoProjection.Settings[0]
			reviewFreezeRefreshCompileAttestationBuildInfoProjectionDigestV1(t, run)
		}, want: "build info projection 漂移"},
		{name: "test different binary", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.Invocation.Argv[0] = "/out/other.test"
		}, want: "compiled test binary argv"},
		{name: "test PASS size drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.Invocation.StdoutSizeBytes = 4
		}, want: "compiled test binary stdout/stderr"},
		{name: "test exact-set missing", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.TestEntrypoints = run.Test.TestEntrypoints[:3]
		}, want: "test_entrypoints"},
		{name: "artifact changed after test", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.PostExecutionArtifactSHA256 = reviewFreezeSHA256V1([]byte("changed"))
		}, want: "pre/post artifact"},
		{name: "network enabled", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) { run.Test.NetworkPolicy = "allow" }, want: "sandbox/network"},
		{name: "secret mounted", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) { run.Test.SecretPolicy = "mounted" }, want: "sandbox/network"},
		{name: "module cache writable", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.ModuleCacheFilesystemPolicy = "read_write"
		}, want: "sandbox/network"},
		{name: "source writable", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.Test.SourceFilesystemPolicy = "read_write"
		}, want: "sandbox/network"},
		{name: "Syft generator forbidden", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.GeneratorName = "syft"
			run.SBOM.GeneratorVersion = "1.30.0"
		}, want: "SBOM generator identity"},
		{name: "SBOM generator binary type", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.GeneratorBinaryRef.MediaType = "application/octet-stream"
		}, want: "SBOM generator binary content ref identity"},
		{name: "SBOM invokes go", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.Invocation.Argv = []string{"/opt/go/bin/go", "test", "-c", "-o", reviewFreezeCompileAttestationBinaryPathV1, reviewFreezeCompileAttestationPackagePatternV1}
		}, want: "SBOM deterministic generator argv"},
		{name: "SBOM cwd", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.Invocation.CWD = reviewFreezeCompileAttestationLogicalCWDV1
		}, want: "SBOM deterministic generator cwd"},
		{name: "SBOM subject drift", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.SubjectArtifactSHA256 = reviewFreezeSHA256V1([]byte("other artifact"))
		}, want: "SBOM subject"},
		{name: "SBOM raw type", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.RawRef.MediaType = "application/json"
		}, want: "SBOM raw content ref identity"},
		{name: "SBOM raw mismatch", mutate: func(_ *testing.T, run *reviewFreezeCompileAttestationBuilderRunV1) {
			run.SBOM.RawRef.SHA256 = reviewFreezeSHA256V1([]byte("other SBOM"))
		}, want: "SBOM deterministic generator stdout/stderr"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runs, projectionSHA, toolchain := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
			run := runs[0]
			test.mutate(t, &run)
			err := reviewFreezeValidateCompileAttestationBuilderRunV1(run, projectionSHA, toolchain)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}
