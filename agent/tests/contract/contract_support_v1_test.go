package contract_test

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
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// contractError 保存根 contract 语料共用的稳定错误码与最小定位路径。
type contractError struct {
	code string
	path string
}

func (e *contractError) Error() string { return e.code + ": " + e.path }

// reject 构造失败关闭的稳定语料错误。
func reject(code, path string) error { return &contractError{code: code, path: path} }

func errorCode(err error) string {
	var target *contractError
	if errors.As(err, &target) {
		return target.code
	}
	return "INTERNAL_TEST_ERROR"
}

// contractManifestTargetTestNamesV1 从指定源码提取顶层 Test exact-set。
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

// semanticDigest 以 domain、NUL 与 canonical bytes 生成稳定语义摘要。
func semanticDigest(domain string, canonical []byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(canonical)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

// canonicalJSON 使用当前 contract 既有的字段顺序与 HTML escaping 策略编码 DTO。
func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

// strictDecode 拒绝未知字段与尾随 JSON 值。
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

// inspectJSON 统一拒绝非法 UTF-8、孤立 surrogate、null、重复键与尾随值。
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
