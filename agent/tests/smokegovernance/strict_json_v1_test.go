package smokegovernance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// smokeGovernanceStrictDecodeV1 是 Smoke Governance 独立测试包的严格 JSON 解码器。
// 它先拒绝重复字段，再拒绝目标 DTO 未声明的字段和尾随 JSON，避免治理清单被宽松解析成多种含义。
func smokeGovernanceStrictDecodeV1(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid UTF-8 JSON")
	}
	if err := smokeGovernanceInspectJSONV1(raw); err != nil {
		return err
	}
	var shape any
	shapeDecoder := json.NewDecoder(bytes.NewReader(raw))
	shapeDecoder.UseNumber()
	if err := shapeDecoder.Decode(&shape); err != nil {
		return err
	}
	targetType := reflect.TypeOf(target)
	if targetType == nil || targetType.Kind() != reflect.Pointer || targetType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("strict JSON target 必须是 struct pointer")
	}
	if err := smokeGovernanceRequireJSONShapeV1(shape, targetType.Elem(), "$"); err != nil {
		return err
	}
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

// smokeGovernanceRequireJSONShapeV1 要求 schema struct 的每个非 omitempty 字段显式出现，并拒绝所有必填字段的 null。
func smokeGovernanceRequireJSONShapeV1(value any, targetType reflect.Type, fieldPath string) error {
	if value == nil {
		return fmt.Errorf("%s 不得为 null", fieldPath)
	}
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	switch targetType.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 必须是 object", fieldPath)
		}
		for index := 0; index < targetType.NumField(); index++ {
			field := targetType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			optional := false
			for _, option := range parts[1:] {
				optional = optional || option == "omitempty"
			}
			child, exists := object[name]
			if !exists {
				if optional {
					continue
				}
				return fmt.Errorf("%s.%s 缺少必填字段", fieldPath, name)
			}
			if err := smokeGovernanceRequireJSONShapeV1(child, field.Type, fieldPath+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s 必须是 array", fieldPath)
		}
		for index, item := range items {
			if err := smokeGovernanceRequireJSONShapeV1(item, targetType.Elem(), fmt.Sprintf("%s[%d]", fieldPath, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// smokeGovernanceInspectJSONV1 使用 Token 流递归校验对象键唯一性和容器闭合。
func smokeGovernanceInspectJSONV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := smokeGovernanceInspectJSONValueV1(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}

// smokeGovernanceInspectJSONValueV1 校验当前 JSON 值；对象字段名在各自作用域内必须唯一。
func smokeGovernanceInspectJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key must be string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if err := smokeGovernanceInspectJSONValueV1(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid object close")
		}
	case '[':
		for decoder.More() {
			if err := smokeGovernanceInspectJSONValueV1(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("invalid array close")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}

// TestSmokeGovernanceStrictJSONV1 固定重复字段、未知字段与尾随值全部失败关闭。
func TestSmokeGovernanceStrictJSONV1(t *testing.T) {
	type strictFixtureV1 struct {
		SchemaVersion string `json:"schema_version"`
		Enabled       bool   `json:"enabled"`
	}
	valid := []byte(`{"schema_version":"strict.v1","enabled":false}`)
	var decoded strictFixtureV1
	if err := smokeGovernanceStrictDecodeV1(valid, &decoded); err != nil {
		t.Fatalf("valid strict JSON rejected: %v", err)
	}
	cases := map[string][]byte{
		"duplicate field": []byte(`{"schema_version":"strict.v1","enabled":false,"enabled":true}`),
		"unknown field":   []byte(`{"schema_version":"strict.v1","enabled":false,"future":true}`),
		"trailing value":  []byte(`{"schema_version":"strict.v1","enabled":false}{}`),
		"invalid close":   []byte(`{"schema_version":"strict.v1","enabled":false`),
		"missing field":   []byte(`{"schema_version":"strict.v1"}`),
		"null boolean":    []byte(`{"schema_version":"strict.v1","enabled":null}`),
		"invalid utf8":    []byte("{\"schema_version\":\"\xff\",\"enabled\":false}"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var target strictFixtureV1
			if err := smokeGovernanceStrictDecodeV1(raw, &target); err == nil {
				t.Fatal("non-canonical JSON was accepted")
			}
		})
	}
}
