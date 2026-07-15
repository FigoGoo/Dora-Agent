package w2r04approvalconsumption_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const maxSafeIntegerV1 int64 = 9_007_199_254_740_991

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// contractError 保存语料 validator 的稳定错误码和最小定位路径。
type contractError struct {
	code string
	path string
}

// corpusManifestFileV1 固定语料文件的原始摘要与向量数。
type corpusManifestFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

// corpusManifestSourceV1 固定 validator 源文件或构建输入的仓库路径与原始摘要。
type corpusManifestSourceV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Error 返回不包含内部堆栈的稳定语料错误文本。
func (e *contractError) Error() string { return e.code + ": " + e.path }

// reject 生成一个失败关闭的稳定语料错误。
func reject(code, path string) error { return &contractError{code: code, path: path} }

// errorCode 只投影可对外比对的稳定错误码。
func errorCode(err error) string {
	var target *contractError
	if errors.As(err, &target) {
		return target.code
	}
	return "INTERNAL_TEST_ERROR"
}

// contractPackageValidatorSourcePathsV1 返回 R04 独立 package 的 direct-source exact-set。
func contractPackageValidatorSourcePathsV1() []string {
	return []string{
		"agent/tests/contract/w2r04approvalconsumption/approval_consumption_receipt_v1_corpus_test.go",
		"agent/tests/contract/w2r04approvalconsumption/validator_support_v1_test.go",
	}
}

// contractManifestRepositoryRootV1 从独立 validator package 定位仓库根目录。
func contractManifestRepositoryRootV1(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(directory, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(repositoryRoot, "agent", "go.mod")); err != nil {
		t.Fatalf("定位仓库根目录: %v", err)
	}
	return repositoryRoot
}

// validateCorpusManifestSourceClosureV1 校验 source exact-set、安全路径和原始文件摘要。
func validateCorpusManifestSourceClosureV1(repositoryRoot, sourceKind string, sources []corpusManifestSourceV1, wantPaths []string) error {
	if err := validateCorpusManifestSourceSetV1(sourceKind, sources, wantPaths); err != nil {
		return err
	}
	for _, source := range sources {
		fullPath, err := corpusManifestSourceFullPathV1(repositoryRoot, source.Path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", source.Path, err)
		}
		actual := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(actual[:]); got != source.SHA256 {
			return fmt.Errorf("%s raw digest=%s want=%s", source.Path, got, source.SHA256)
		}
	}
	return nil
}

// validateCorpusManifestSourceSetV1 要求 source 路径严格升序、唯一且与预期集合完全一致。
func validateCorpusManifestSourceSetV1(sourceKind string, sources []corpusManifestSourceV1, wantPaths []string) error {
	if len(sources) != len(wantPaths) {
		return fmt.Errorf("%s source count=%d want=%d", sourceKind, len(sources), len(wantPaths))
	}
	for index, source := range sources {
		if err := validateCorpusManifestSourcePathV1(sourceKind, source.Path); err != nil {
			return err
		}
		if !digestPattern.MatchString(source.SHA256) {
			return fmt.Errorf("%s 摘要格式非法: %s", source.Path, source.SHA256)
		}
		if index > 0 && sources[index-1].Path >= source.Path {
			return fmt.Errorf("%s sources 必须按 path 严格升序且唯一", sourceKind)
		}
		if source.Path != wantPaths[index] {
			return fmt.Errorf("%s source[%d]=%s want=%s", sourceKind, index, source.Path, wantPaths[index])
		}
	}
	return nil
}

// validateCorpusManifestSourcePathV1 限制 source 为对应边界内的规范仓库相对路径。
func validateCorpusManifestSourcePathV1(sourceKind, sourcePath string) error {
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(sourcePath)))
	if sourcePath == "" || !utf8.ValidString(sourcePath) || strings.Contains(sourcePath, "\\") || filepath.IsAbs(sourcePath) || cleaned != sourcePath {
		return fmt.Errorf("%s source path 非安全仓库相对路径: %q", sourceKind, sourcePath)
	}
	for _, current := range sourcePath {
		if current < 0x20 || current >= 0x7f && current <= 0x9f {
			return fmt.Errorf("%s source path 含控制字符: %q", sourceKind, sourcePath)
		}
	}
	switch sourceKind {
	case "validator":
		if !strings.HasPrefix(sourcePath, "agent/tests/contract/") || filepath.Ext(sourcePath) != ".go" {
			return fmt.Errorf("validator source 必须是 agent/tests/contract/ 下的 Go 文件: %s", sourcePath)
		}
	case "build":
		if sourcePath != "agent/go.mod" && sourcePath != "agent/go.sum" {
			return fmt.Errorf("validator build source 必须是 agent/go.mod 或 agent/go.sum: %s", sourcePath)
		}
	default:
		return fmt.Errorf("未知 source kind: %s", sourceKind)
	}
	return nil
}

// validateCorpusManifestGoPackageExactSetV1 防止未入 manifest 的 Go 文件隐式进入 validator package。
func validateCorpusManifestGoPackageExactSetV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	declaredByDirectory := make(map[string][]string)
	for _, source := range sources {
		directory := filepath.ToSlash(filepath.Dir(filepath.FromSlash(source.Path)))
		declaredByDirectory[directory] = append(declaredByDirectory[directory], source.Path)
	}
	for directory, declared := range declaredByDirectory {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, filepath.FromSlash(directory)))
		if err != nil {
			return fmt.Errorf("读取 validator package %s: %w", directory, err)
		}
		actual := make([]string, 0, len(entries))
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".go" {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("validator package Go source 不是普通文件: %s/%s", directory, entry.Name())
			}
			actual = append(actual, directory+"/"+entry.Name())
		}
		sort.Strings(actual)
		sort.Strings(declared)
		if !reflect.DeepEqual(actual, declared) {
			return fmt.Errorf("validator package %s sources=%v want=%v", directory, declared, actual)
		}
	}
	return validateCorpusManifestGoSourceExecutionShapeV1(repositoryRoot, sources)
}

// validateCorpusManifestGoSourceExecutionShapeV1 禁止构建条件或 TestMain 改变受审 validator 的执行集合。
func validateCorpusManifestGoSourceExecutionShapeV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	buildConstraintPattern := regexp.MustCompile(`(?m)^//go:build(?:[\t ]|$)|^//[\t ]+\+build(?:[\t ]|$)`)
	for _, source := range sources {
		if err := validateCorpusManifestGoSourceFilenameV1(source.Path); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(source.Path)))
		if err != nil {
			return fmt.Errorf("读取 validator source %s: %w", source.Path, err)
		}
		if buildConstraintPattern.Match(raw) {
			return fmt.Errorf("validator source 禁止 build constraint: %s", source.Path)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), source.Path, raw, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("解析 validator source %s: %w", source.Path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "TestMain" {
				return fmt.Errorf("validator source 禁止 TestMain: %s", source.Path)
			}
		}
	}
	return nil
}

// validateCorpusManifestGoSourceFilenameV1 禁止 Go 工具链隐式忽略或按平台筛选 source。
func validateCorpusManifestGoSourceFilenameV1(sourcePath string) error {
	base := filepath.Base(filepath.FromSlash(sourcePath))
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
		return fmt.Errorf("validator source filename 会被 Go 忽略: %s", sourcePath)
	}
	stem := strings.TrimSuffix(base, ".go")
	parts := strings.Split(stem, "_")
	if len(parts) > 0 && parts[len(parts)-1] == "test" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	knownOSArch := map[string]struct{}{
		"386": {}, "aix": {}, "amd64": {}, "amd64p32": {}, "android": {}, "arm": {}, "arm64": {}, "arm64be": {}, "armbe": {},
		"darwin": {}, "dragonfly": {}, "freebsd": {}, "hurd": {}, "illumos": {}, "ios": {}, "js": {}, "linux": {}, "loong64": {},
		"mips": {}, "mips64": {}, "mips64le": {}, "mips64p32": {}, "mips64p32le": {}, "mipsle": {}, "nacl": {}, "netbsd": {},
		"openbsd": {}, "plan9": {}, "ppc": {}, "ppc64": {}, "ppc64le": {}, "riscv": {}, "riscv64": {}, "s390": {}, "s390x": {},
		"solaris": {}, "sparc": {}, "sparc64": {}, "wasip1": {}, "wasm": {}, "windows": {}, "zos": {},
	}
	if _, constrained := knownOSArch[parts[len(parts)-1]]; constrained {
		return fmt.Errorf("validator source filename 禁止 GOOS/GOARCH build constraint: %s", sourcePath)
	}
	return nil
}

// validateCorpusManifestGoBuildInputsV1 拒绝 go.mod replace 绕过已冻结的构建输入。
func validateCorpusManifestGoBuildInputsV1(repositoryRoot string, sources []corpusManifestSourceV1) error {
	for _, source := range sources {
		if source.Path != "agent/go.mod" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(source.Path)))
		if err != nil {
			return err
		}
		if regexp.MustCompile(`(?m)^\s*replace(?:\s|\()`).Match(raw) {
			return fmt.Errorf("validator go.mod 禁止未闭合 replace")
		}
	}
	return nil
}

// corpusManifestSourceFullPathV1 将受审相对路径投影为仓库内普通文件。
func corpusManifestSourceFullPathV1(repositoryRoot, sourcePath string) (string, error) {
	fullPath := filepath.Join(repositoryRoot, filepath.FromSlash(sourcePath))
	relative, err := filepath.Rel(repositoryRoot, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path 越出仓库: %s", sourcePath)
	}
	info, err := os.Lstat(fullPath)
	if err != nil {
		return "", fmt.Errorf("检查 %s: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source 不是普通文件: %s", sourcePath)
	}
	return fullPath, nil
}

// contractManifestTargetTestNamesV1 从 direct source AST 提取顶层 Test 入口 exact-set。
func contractManifestTargetTestNamesV1(t *testing.T, files []string) []string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0)
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			result = append(result, function.Name.Name)
		}
	}
	sort.Strings(result)
	return result
}

// canonicalUUIDv7 仅接受小写规范形式、v7 版本与 RFC 4122 variant 的 UUID。
func canonicalUUIDv7(value string) bool {
	if len(value) != 36 || value != strings.ToLower(value) || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	decoded := make([]byte, 16)
	if _, err := hex.Decode(decoded, []byte(compact)); err != nil {
		return false
	}
	return decoded[6]>>4 == 7 && decoded[8]&0xc0 == 0x80
}

// safePositiveIntegerV1 限制正整数为 JSON/JavaScript 可精确表示的边界。
func safePositiveIntegerV1(value int64) bool {
	return value >= 1 && value <= maxSafeIntegerV1
}

// semanticDigest 通过显式 domain separator 计算稳定语义摘要。
func semanticDigest(domain string, canonical []byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(canonical)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

// digestValueV1 对有类型值进行稳定 JSON 编码并计算 domain-separated 摘要。
func digestValueV1(domain string, value any) (string, error) {
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	return semanticDigest(domain, canonical), nil
}

// canonicalJSON 生成不转义 HTML 且不带尾换行的确定性 JSON。
func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

// strictDecode 拒绝未知字段和任何尾随 JSON 值。
func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

// inspectJSON 先于有类型解码检查 UTF-8、UTF-16 escape、null、重复键和尾随值。
func inspectJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return reject("INVALID_JSON", "utf-8")
	}
	if err := validateJSONUnicodeEscapes(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return reject("TRAILING_VALUE", "result")
	}
	return nil
}

// validateJSONUnicodeEscapes 拒绝非法或未配对的 UTF-16 surrogate escape。
func validateJSONUnicodeEscapes(raw []byte) error {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			index++
			if raw[index] != 'u' {
				continue
			}
			value, next, ok := parseHexUTF16Unit(raw, index+1)
			if !ok {
				return reject("INVALID_UNICODE_ESCAPE", "unicode escape")
			}
			index = next - 1
			if value >= 0xD800 && value <= 0xDBFF {
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return reject("INVALID_UNICODE_ESCAPE", "unpaired high surrogate")
				}
				low, afterLow, lowOK := parseHexUTF16Unit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return reject("INVALID_UNICODE_ESCAPE", "invalid surrogate pair")
				}
				index = afterLow - 1
			} else if value >= 0xDC00 && value <= 0xDFFF {
				return reject("INVALID_UNICODE_ESCAPE", "unpaired low surrogate")
			}
		}
	}
	return nil
}

// parseHexUTF16Unit 解析四位十六进制 UTF-16 code unit，不接受宽松形式。
func parseHexUTF16Unit(raw []byte, start int) (uint16, int, bool) {
	if start+4 > len(raw) {
		return 0, start, false
	}
	var value uint16
	for index := start; index < start+4; index++ {
		value <<= 4
		switch current := raw[index]; {
		case current >= '0' && current <= '9':
			value += uint16(current - '0')
		case current >= 'a' && current <= 'f':
			value += uint16(current-'a') + 10
		case current >= 'A' && current <= 'F':
			value += uint16(current-'A') + 10
		default:
			return 0, start, false
		}
	}
	return value, start + 4, true
}

// inspectJSONValue 递归检查 JSON 值，在解码时保留对重复键的可见性。
func inspectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return reject("INVALID_JSON", "token")
	}
	if token == nil {
		return reject("NULL_NOT_ALLOWED", "null")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return reject("INVALID_JSON", "object key")
			}
			key, ok := keyToken.(string)
			if !ok {
				return reject("INVALID_JSON", "object key type")
			}
			if _, duplicate := seen[key]; duplicate {
				return reject("DUPLICATE_KEY", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return reject("INVALID_JSON", "object end")
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return reject("INVALID_JSON", "array end")
		}
	default:
		return reject("INVALID_JSON", "unexpected delimiter")
	}
	return nil
}
