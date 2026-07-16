package reviewfreeze_test

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// reviewFreezeValidateCompileAttestationBuildInfoRawV1 校验 builder 提交的
// go1.26.3_version_m_text.v1 辅助材料，并把 raw bytes、typed ref、固定探针命令和
// canonical projection 逐项绑定。最终 authority 必须优先从已验 compiled binary 使用
// debug/buildinfo 派生；go version -m 文本只作为可复核的辅助表示层，不能替代 binary 验真。
func reviewFreezeValidateCompileAttestationBuildInfoRawV1(raw []byte, compile reviewFreezeCompileAttestationCompileV1) error {
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1 {
		return fmt.Errorf("build info raw size=%d limit=%d", len(raw), reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1)
	}
	if err := reviewFreezeValidateAttestationContentRefV1(
		compile.BuildInfoRawRef,
		reviewFreezeCompileAttestationBuildInfoRawArtifactKindV1,
		reviewFreezeCompileAttestationBuildInfoRawContentSchemaV1,
		reviewFreezeCompileAttestationBuildInfoRawMediaTypeV1,
		reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1,
		"build info raw",
	); err != nil {
		return err
	}
	wantRawSHA := reviewFreezeSHA256V1(raw)
	wantRawSize := int64(len(raw))
	if compile.BuildInfoRawRef.SHA256 != wantRawSHA || compile.BuildInfoRawRef.SizeBytes != wantRawSize {
		return fmt.Errorf("build info raw ref digest/size=%q/%d want=%q/%d", compile.BuildInfoRawRef.SHA256, compile.BuildInfoRawRef.SizeBytes, wantRawSHA, wantRawSize)
	}
	if compile.ArtifactPath != reviewFreezeCompileAttestationBinaryPathV1 {
		return fmt.Errorf("build info artifact_path=%q want=%q", compile.ArtifactPath, reviewFreezeCompileAttestationBinaryPathV1)
	}
	if err := reviewFreezeValidateCompileAttestationBuilderInvocationV1(
		compile.BuildInfoInvocation,
		[]string{"/opt/go/bin/go", "version", "-m", reviewFreezeCompileAttestationBinaryPathV1},
		reviewFreezeCompileAttestationLogicalCWDV1,
		wantRawSHA,
		wantRawSize,
		reviewFreezeSHA256V1(nil),
		0,
		"go version -m raw",
	); err != nil {
		return err
	}

	projection, err := reviewFreezeParseCompileAttestationBuildInfoRawV1(raw)
	if err != nil {
		return err
	}
	projectionSHA, err := reviewFreezeCompileAttestationBuildInfoProjectionSHA256V1(projection)
	if err != nil {
		return err
	}
	if compile.BuildInfoProjectionSHA256 != projectionSHA {
		return fmt.Errorf("build info raw-derived projection digest=%q want=%q", compile.BuildInfoProjectionSHA256, projectionSHA)
	}
	if !reflect.DeepEqual(compile.BuildInfoProjection, projection) {
		return fmt.Errorf("build info raw-derived projection=%+v statement=%+v", projection, compile.BuildInfoProjection)
	}
	return nil
}

// reviewFreezeParseCompileAttestationBuildInfoRawV1 解析锁定 Go 1.26.3、linux/amd64、
// CGO=0 test binary 的 go version -m 文本。path 必须先于 main mod，build records 只能
// 出现在两者之后；build settings 本身按 key 归一化，因此其 raw 行顺序不进入 projection。
func reviewFreezeParseCompileAttestationBuildInfoRawV1(raw []byte) (reviewFreezeCompileAttestationBuildInfoV1, error) {
	var zero reviewFreezeCompileAttestationBuildInfoV1
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1 {
		return zero, fmt.Errorf("build info raw size=%d limit=%d", len(raw), reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1)
	}
	if !utf8.Valid(raw) {
		return zero, fmt.Errorf("build info raw 不是合法 UTF-8")
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return zero, fmt.Errorf("build info raw 禁止 NUL")
	}
	if bytes.IndexByte(raw, '\r') >= 0 {
		return zero, fmt.Errorf("build info raw 只允许 LF，禁止 CR/CRLF")
	}
	if raw[len(raw)-1] != '\n' {
		return zero, fmt.Errorf("build info raw 必须以唯一 LF 结束")
	}

	lines := strings.Split(string(raw), "\n")
	lines = lines[:len(lines)-1]
	for index, line := range lines {
		if line == "" {
			return zero, fmt.Errorf("build info raw empty/trailing line=%d", index+1)
		}
	}
	wantHeader := reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3"
	if len(lines) == 0 || lines[0] != wantHeader {
		return zero, fmt.Errorf("build info raw binary header=%q want=%q", reviewFreezeFirstCompileAttestationBuildInfoRawLineV1(lines), wantHeader)
	}

	wantProjection := reviewFreezeExpectedCompileAttestationBuildInfoV1()
	wantSettings := make(map[string]string, len(wantProjection.Settings))
	for _, setting := range wantProjection.Settings {
		if _, duplicate := wantSettings[setting.Name]; duplicate {
			return zero, fmt.Errorf("build info parser expected setting 重复=%q", setting.Name)
		}
		wantSettings[setting.Name] = setting.Value
	}

	seenPath := false
	seenMainMod := false
	parsedPath := ""
	parsedMainPath := ""
	parsedMainVersion := ""
	parsedSettings := make(map[string]string, len(wantSettings))
	for index, line := range lines[1:] {
		lineNumber := index + 2
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] != "" {
			return zero, fmt.Errorf("build info raw record shape 非法 line=%d value=%q", lineNumber, line)
		}
		switch fields[1] {
		case "path":
			if seenPath {
				return zero, fmt.Errorf("build info raw duplicate path line=%d", lineNumber)
			}
			if seenMainMod || len(parsedSettings) != 0 {
				return zero, fmt.Errorf("build info raw path order ambiguity line=%d", lineNumber)
			}
			if len(fields) != 3 || fields[2] != reviewFreezeCompileAttestationTargetImportV1+".test" {
				return zero, fmt.Errorf("build info raw path record=%q", line)
			}
			seenPath = true
			parsedPath = fields[2]
		case "mod":
			if seenMainMod {
				return zero, fmt.Errorf("build info raw duplicate main mod line=%d", lineNumber)
			}
			if !seenPath || len(parsedSettings) != 0 {
				return zero, fmt.Errorf("build info raw main mod order ambiguity line=%d", lineNumber)
			}
			if len(fields) != 5 || fields[2] != reviewFreezeCompileAttestationModulePathV1 || fields[3] != "(devel)" || fields[4] != "" {
				return zero, fmt.Errorf("build info raw main mod record=%q", line)
			}
			seenMainMod = true
			parsedMainPath = fields[2]
			parsedMainVersion = fields[3]
		case "dep":
			return zero, fmt.Errorf("build info raw dependencies 必须为 zero exact-set line=%d", lineNumber)
		case "=>":
			return zero, fmt.Errorf("build info raw 禁止 replace line=%d", lineNumber)
		case "build":
			if !seenPath || !seenMainMod {
				return zero, fmt.Errorf("build info raw build setting order ambiguity line=%d", lineNumber)
			}
			if len(fields) != 3 || strings.Count(fields[2], "=") != 1 {
				return zero, fmt.Errorf("build info raw build setting shape 非法 line=%d value=%q", lineNumber, line)
			}
			name, value, _ := strings.Cut(fields[2], "=")
			if name == "vcs" || strings.HasPrefix(name, "vcs.") {
				return zero, fmt.Errorf("build info raw 禁止 vcs setting=%q", name)
			}
			wantValue, known := wantSettings[name]
			if !known {
				return zero, fmt.Errorf("build info raw unknown build setting=%q", name)
			}
			if _, duplicate := parsedSettings[name]; duplicate {
				return zero, fmt.Errorf("build info raw duplicate build setting=%q", name)
			}
			if value != wantValue {
				return zero, fmt.Errorf("build info raw build setting drift %s=%q want=%q", name, value, wantValue)
			}
			parsedSettings[name] = value
		default:
			return zero, fmt.Errorf("build info raw unknown record=%q line=%d", fields[1], lineNumber)
		}
	}
	if !seenPath {
		return zero, fmt.Errorf("build info raw missing path")
	}
	if !seenMainMod {
		return zero, fmt.Errorf("build info raw missing main mod")
	}
	for _, setting := range wantProjection.Settings {
		if _, exists := parsedSettings[setting.Name]; !exists {
			return zero, fmt.Errorf("build info raw missing build setting=%q", setting.Name)
		}
	}

	settings := make([]reviewFreezeCompileAttestationBuildSettingV1, 0, len(parsedSettings))
	for name, value := range parsedSettings {
		settings = append(settings, reviewFreezeCompileAttestationBuildSettingV1{Name: name, Value: value})
	}
	sort.Slice(settings, func(i, j int) bool { return settings[i].Name < settings[j].Name })
	return reviewFreezeCompileAttestationBuildInfoV1{
		SchemaVersion: reviewFreezeCompileAttestationBuildInfoProjectionSchemaV1,
		GoVersion:     "go1.26.3",
		Path:          parsedPath,
		MainPath:      parsedMainPath,
		MainVersion:   parsedMainVersion,
		Dependencies:  []reviewFreezeCompileAttestationBuildInfoDepV1{},
		Settings:      settings,
	}, nil
}

func reviewFreezeFirstCompileAttestationBuildInfoRawLineV1(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func reviewFreezeCompileAttestationBuildInfoRawFixtureV1() ([]byte, reviewFreezeCompileAttestationCompileV1) {
	raw := []byte(
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
	runs, _, _ := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
	compile := runs[0].Compile
	reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(raw, &compile)
	return raw, compile
}

func reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(raw []byte, compile *reviewFreezeCompileAttestationCompileV1) {
	ref := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
		reviewFreezeCompileAttestationBuildInfoRawArtifactKindV1,
		reviewFreezeCompileAttestationBuildInfoRawContentSchemaV1,
		reviewFreezeCompileAttestationBuildInfoRawMediaTypeV1,
		raw,
	)
	compile.BuildInfoRawRef = ref
	compile.BuildInfoInvocation.StdoutSHA256 = ref.SHA256
	compile.BuildInfoInvocation.StdoutSizeBytes = ref.SizeBytes
}

func reviewFreezeReplaceCompileBuildInfoRawOnceV1(t *testing.T, raw []byte, old, replacement string) []byte {
	t.Helper()
	if count := bytes.Count(raw, []byte(old)); count != 1 {
		t.Fatalf("build info raw fixture replacement count=%d old=%q", count, old)
	}
	return bytes.Replace(raw, []byte(old), []byte(replacement), 1)
}

func TestW2ReviewFreezeCompileBuildInfoRawV1(t *testing.T) {
	raw, compile := reviewFreezeCompileAttestationBuildInfoRawFixtureV1()
	if err := reviewFreezeValidateCompileAttestationBuildInfoRawV1(raw, compile); err != nil {
		t.Fatalf("valid Go 1.26.3 build info raw rejected: %v", err)
	}

	projection, err := reviewFreezeParseCompileAttestationBuildInfoRawV1(raw)
	if err != nil {
		t.Fatalf("parse valid Go 1.26.3 build info raw: %v", err)
	}
	if want := reviewFreezeExpectedCompileAttestationBuildInfoV1(); !reflect.DeepEqual(projection, want) {
		t.Fatalf("raw-derived projection=%+v want=%+v", projection, want)
	}

	t.Run("build settings are canonicalized", func(t *testing.T) {
		reordered := []byte(
			reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3\n" +
				"\tpath\t" + reviewFreezeCompileAttestationTargetImportV1 + ".test\n" +
				"\tmod\t" + reviewFreezeCompileAttestationModulePathV1 + "\t(devel)\t\n" +
				"\tbuild\tGOAMD64=v1\n" +
				"\tbuild\tGOOS=linux\n" +
				"\tbuild\tGOARCH=amd64\n" +
				"\tbuild\tCGO_ENABLED=0\n" +
				"\tbuild\t-trimpath=true\n" +
				"\tbuild\t-compiler=gc\n" +
				"\tbuild\t-buildmode=exe\n",
		)
		_, reorderedCompile := reviewFreezeCompileAttestationBuildInfoRawFixtureV1()
		reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(reordered, &reorderedCompile)
		if err := reviewFreezeValidateCompileAttestationBuildInfoRawV1(reordered, reorderedCompile); err != nil {
			t.Fatalf("semantically identical reordered settings rejected: %v", err)
		}
	})
}

func TestW2ReviewFreezeCompileBuildInfoRawAdversarialV1(t *testing.T) {
	pathLine := "\tpath\t" + reviewFreezeCompileAttestationTargetImportV1 + ".test\n"
	modLine := "\tmod\t" + reviewFreezeCompileAttestationModulePathV1 + "\t(devel)\t\n"
	tests := []struct {
		name          string
		mutateRaw     func(*testing.T, []byte) []byte
		mutateCompile func(*testing.T, *reviewFreezeCompileAttestationCompileV1)
		want          string
	}{
		{name: "empty", mutateRaw: func(_ *testing.T, _ []byte) []byte { return []byte{} }, want: "raw size"},
		{name: "oversize", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append(raw, bytes.Repeat([]byte{'x'}, reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1)...)
		}, want: "raw size"},
		{name: "invalid UTF-8", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append(append([]byte{}, raw[:len(raw)-1]...), 0xff, '\n')
		}, want: "UTF-8"},
		{name: "NUL", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append(append([]byte{}, raw[:len(raw)-1]...), 0, '\n')
		}, want: "NUL"},
		{name: "CRLF", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))
		}, want: "CR/CRLF"},
		{name: "missing final LF", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append([]byte{}, raw[:len(raw)-1]...)
		}, want: "唯一 LF"},
		{name: "trailing blank line", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append(append([]byte{}, raw...), '\n')
		}, want: "empty/trailing"},
		{name: "binary path drift", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, reviewFreezeCompileAttestationBinaryPathV1+": go1.26.3", "/tmp/w2r01.test: go1.26.3")
		}, want: "binary header"},
		{name: "Go version drift", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, "go1.26.3\n", "go1.26.4\n")
		}, want: "binary header"},
		{name: "path drift", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, pathLine, "\tpath\texample.invalid/other.test\n")
		}, want: "path record"},
		{name: "duplicate path", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, pathLine, pathLine+pathLine)
		}, want: "duplicate path"},
		{name: "missing path", mutateRaw: func(_ *testing.T, _ []byte) []byte {
			return []byte(reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3\n")
		}, want: "missing path"},
		{name: "main mod drift", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, "\tmod\texample.invalid/main\t(devel)\t\n")
		}, want: "main mod record"},
		{name: "main mod sum forbidden", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, "\tmod\t"+reviewFreezeCompileAttestationModulePathV1+"\t(devel)\th1:forbidden\n")
		}, want: "main mod record"},
		{name: "duplicate main mod", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, modLine+modLine)
		}, want: "duplicate main mod"},
		{name: "missing main mod", mutateRaw: func(_ *testing.T, _ []byte) []byte {
			return []byte(
				reviewFreezeCompileAttestationBinaryPathV1 + ": go1.26.3\n" +
					"\tpath\t" + reviewFreezeCompileAttestationTargetImportV1 + ".test\n",
			)
		}, want: "missing main mod"},
		{name: "path and mod order ambiguity", mutateRaw: func(t *testing.T, raw []byte) []byte {
			withoutPath := reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, pathLine, "")
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, withoutPath, modLine, modLine+pathLine)
		}, want: "order ambiguity"},
		{name: "dependency forbidden", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, modLine+"\tdep\tgolang.org/x/text\tv0.34.0\th1:oL/Q\n")
		}, want: "dependencies"},
		{name: "replace forbidden", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, modLine+"\t=>\texample.invalid/replacement\tv1.0.0\th1:sum\n")
		}, want: "replace"},
		{name: "unknown record", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, modLine+"\ttool\tunknown\n")
		}, want: "unknown record"},
		{name: "build before main mod", mutateRaw: func(t *testing.T, raw []byte) []byte {
			setting := "\tbuild\t-buildmode=exe\n"
			withoutSetting := reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, setting, "")
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, withoutSetting, modLine, setting+modLine)
		}, want: "order ambiguity"},
		{name: "duplicate build setting", mutateRaw: func(t *testing.T, raw []byte) []byte {
			setting := "\tbuild\tGOOS=linux\n"
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, setting, setting+setting)
		}, want: "duplicate build setting"},
		{name: "missing build setting", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, "\tbuild\tGOAMD64=v1\n", "")
		}, want: "missing build setting"},
		{name: "unknown build setting", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, "\tbuild\tGOAMD64=v1\n", "\tbuild\tGO386=sse2\n")
		}, want: "unknown build setting"},
		{name: "build setting drift", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, "\tbuild\tCGO_ENABLED=0\n", "\tbuild\tCGO_ENABLED=1\n")
		}, want: "setting drift"},
		{name: "vcs forbidden", mutateRaw: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeReplaceCompileBuildInfoRawOnceV1(t, raw, modLine, modLine+"\tbuild\tvcs=git\n")
		}, want: "vcs"},
		{name: "trailing unknown data", mutateRaw: func(_ *testing.T, raw []byte) []byte {
			return append(append([]byte{}, raw...), []byte("trailing\n")...)
		}, want: "record shape"},
		{name: "raw ref schema drift", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoRawRef.ContentSchemaVersion = "go1.26.3_version_m_text.v2"
		}, want: "content ref identity"},
		{name: "raw ref digest mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoRawRef.SHA256 = reviewFreezeSHA256V1([]byte("other build info raw"))
		}, want: "raw ref digest/size"},
		{name: "raw ref size mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoRawRef.SizeBytes++
		}, want: "raw ref digest/size"},
		{name: "invocation stdout mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoInvocation.StdoutSHA256 = reviewFreezeSHA256V1([]byte("other stdout"))
		}, want: "stdout/stderr"},
		{name: "artifact path mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.ArtifactPath = "/out/other.test"
		}, want: "artifact_path"},
		{name: "projection digest mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoProjectionSHA256 = reviewFreezeSHA256V1([]byte("other projection"))
		}, want: "raw-derived projection digest"},
		{name: "projection body mismatch", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoProjection.Settings[0].Value = "pie"
		}, want: "raw-derived projection="},
		{name: "projection dependency injected", mutateCompile: func(_ *testing.T, compile *reviewFreezeCompileAttestationCompileV1) {
			compile.BuildInfoProjection.Dependencies = []reviewFreezeCompileAttestationBuildInfoDepV1{{Path: "example.invalid/dep", Version: "v1.0.0", Sum: "h1:forged"}}
		}, want: "raw-derived projection="},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, compile := reviewFreezeCompileAttestationBuildInfoRawFixtureV1()
			if test.mutateRaw != nil {
				raw = test.mutateRaw(t, raw)
			}
			reviewFreezeBindCompileAttestationBuildInfoRawFixtureV1(raw, &compile)
			if test.mutateCompile != nil {
				test.mutateCompile(t, &compile)
			}
			err := reviewFreezeValidateCompileAttestationBuildInfoRawV1(raw, compile)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}
