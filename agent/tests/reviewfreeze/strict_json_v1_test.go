package reviewfreeze_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// messageSetStrictDecodeV1 是 Review Freeze 独立信任根内的严格 JSON 解码器。
// 它拒绝重复字段、未知字段和尾随 JSON，名称暂时保持与迁移前调用点一致以降低切换风险。
func messageSetStrictDecodeV1(raw []byte, target any) error {
	if err := reviewFreezeInspectJSONV1(raw); err != nil {
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

// reviewFreezeInspectJSONV1 先以 Token 流扫描 JSON，避免 encoding/json 静默接受重复字段。
func reviewFreezeInspectJSONV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := reviewFreezeInspectJSONValueV1(decoder); err != nil {
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

// reviewFreezeInspectJSONValueV1 递归校验对象键唯一性和容器闭合，不把 JSON 结构错误留给宽松解码器处理。
func reviewFreezeInspectJSONValueV1(decoder *json.Decoder) error {
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
			if err := reviewFreezeInspectJSONValueV1(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid object close")
		}
	case '[':
		for decoder.More() {
			if err := reviewFreezeInspectJSONValueV1(decoder); err != nil {
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
