// Package w2r01_test 承载尚在评审期的 W2-R01 跨语言契约校验器，不提供生产 Runtime。
package w2r01_test

// strictDecode 是 R01 所有 DTO 的唯一严格解码入口；未知字段或尾随值必须失败关闭。
func strictDecode(raw []byte, target any) error {
	return strictDecodeV1(raw, target)
}

// inspectJSON 在 DTO 解码前统一执行 UTF-8、重复键、null 与尾随值检查。
func inspectJSON(raw []byte) error {
	return inspectJSONV1(raw)
}

// validateJSONUnicodeEscapes 拒绝孤立 UTF-16 surrogate，避免跨语言解码结果分歧。
func validateJSONUnicodeEscapes(raw []byte) error {
	return validateJSONUnicodeEscapesV1(raw)
}

// canonicalUUIDv7 使用标准库字节级规则校验 UUIDv7 的小写 canonical 形式、版本位和 RFC 4122 variant。
func canonicalUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	var raw [16]byte
	rawIndex := 0
	for index := 0; index < len(value); {
		if value[index] == '-' {
			index++
			continue
		}
		if index+1 >= len(value) || rawIndex >= len(raw) {
			return false
		}
		high, highOK := canonicalLowerHexNibbleV1(value[index])
		low, lowOK := canonicalLowerHexNibbleV1(value[index+1])
		if !highOK || !lowOK {
			return false
		}
		raw[rawIndex] = high<<4 | low
		rawIndex++
		index += 2
	}
	return rawIndex == len(raw) && raw[6]>>4 == 7 && raw[8]&0xc0 == 0x80
}

func canonicalLowerHexNibbleV1(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}
