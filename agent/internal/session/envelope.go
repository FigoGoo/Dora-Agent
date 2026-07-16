package session

import "fmt"

const (
	protectedEnvelopeV1Version    byte = 1
	protectedEnvelopeV1HeaderSize      = 7
	protectedEnvelopeV1NonceSize       = 12
	protectedEnvelopeV1TagSize         = 16
	protectedEnvelopeV1Magic           = "DRAE"
)

// EnvelopeAlgorithm 是自描述 AEAD Envelope 中冻结的算法编号。
type EnvelopeAlgorithm byte

const (
	// EnvelopeAlgorithmAES256GCM 表示 AES-256-GCM，Nonce 固定 12 字节，认证标签固定 16 字节。
	EnvelopeAlgorithmAES256GCM EnvelopeAlgorithm = 1
)

// ParsedEnvelopeV1 是经过结构校验的 DRAE v1 只读视图。
// 字段均为独立副本，调用方可以安全交给 AEAD Open，不能用它绕过认证标签校验。
type ParsedEnvelopeV1 struct {
	// Algorithm 是冻结的 AEAD 算法编号。
	Algorithm EnvelopeAlgorithm
	// Nonce 是 DRAE v1 中固定 12 字节的随机 Nonce。
	Nonce []byte
	// CiphertextAndTag 是密文与 16 字节 GCM 认证标签的拼接值。
	CiphertextAndTag []byte
}

// BuildEnvelopeV1 按 W0 二进制格式构建版本化、自描述 AEAD Envelope。
// 布局固定为 magic `DRAE`、version、algorithm、nonce_length、nonce、ciphertext||auth_tag；
// 调用方必须先完成真实 AEAD 加密，本函数只组装和复制已经生成的非秘密元数据与密文结果。
func BuildEnvelopeV1(algorithm EnvelopeAlgorithm, nonce, ciphertextAndTag []byte) ([]byte, error) {
	if algorithm != EnvelopeAlgorithmAES256GCM {
		return nil, fmt.Errorf("%w: unsupported algorithm", ErrInvalidProtectedContentEnvelope)
	}
	if len(nonce) != protectedEnvelopeV1NonceSize {
		return nil, fmt.Errorf("%w: nonce must be %d bytes", ErrInvalidProtectedContentEnvelope, protectedEnvelopeV1NonceSize)
	}
	// AES-GCM 输出必须至少包含一个字节密文和 16 字节认证标签；空明文不属于 W0 非空 Prompt 契约。
	if len(ciphertextAndTag) <= protectedEnvelopeV1TagSize {
		return nil, fmt.Errorf("%w: ciphertext and tag are too short", ErrInvalidProtectedContentEnvelope)
	}
	envelope := make([]byte, 0, protectedEnvelopeV1HeaderSize+len(nonce)+len(ciphertextAndTag))
	envelope = append(envelope, protectedEnvelopeV1Magic...)
	envelope = append(envelope, protectedEnvelopeV1Version, byte(algorithm), byte(len(nonce)))
	envelope = append(envelope, nonce...)
	envelope = append(envelope, ciphertextAndTag...)
	return envelope, nil
}

// ValidateEnvelopeV1 验证持久化值满足 W0 自描述 AEAD Envelope 的可执行结构契约。
// 本方法不持有密钥，不能替代 AEAD Open 的认证校验；真实解密适配器仍必须验证认证标签。
func ValidateEnvelopeV1(envelope []byte) error {
	minimumLength := protectedEnvelopeV1HeaderSize + protectedEnvelopeV1NonceSize + protectedEnvelopeV1TagSize + 1
	if len(envelope) < minimumLength {
		return fmt.Errorf("%w: envelope is too short", ErrInvalidProtectedContentEnvelope)
	}
	if string(envelope[:len(protectedEnvelopeV1Magic)]) != protectedEnvelopeV1Magic {
		return fmt.Errorf("%w: magic mismatch", ErrInvalidProtectedContentEnvelope)
	}
	if envelope[4] != protectedEnvelopeV1Version {
		return fmt.Errorf("%w: unsupported version", ErrInvalidProtectedContentEnvelope)
	}
	if EnvelopeAlgorithm(envelope[5]) != EnvelopeAlgorithmAES256GCM {
		return fmt.Errorf("%w: unsupported algorithm", ErrInvalidProtectedContentEnvelope)
	}
	if int(envelope[6]) != protectedEnvelopeV1NonceSize {
		return fmt.Errorf("%w: nonce length mismatch", ErrInvalidProtectedContentEnvelope)
	}
	return nil
}

// ParseEnvelopeV1 在完整结构校验后拆分 Nonce 与密文认证标签。
// 该方法不持有密钥，因此只证明格式可解析；调用方必须继续执行 AES-GCM Open。
func ParseEnvelopeV1(envelope []byte) (ParsedEnvelopeV1, error) {
	if err := ValidateEnvelopeV1(envelope); err != nil {
		return ParsedEnvelopeV1{}, err
	}
	nonceStart := protectedEnvelopeV1HeaderSize
	nonceEnd := nonceStart + protectedEnvelopeV1NonceSize
	return ParsedEnvelopeV1{
		Algorithm:        EnvelopeAlgorithm(envelope[5]),
		Nonce:            append([]byte(nil), envelope[nonceStart:nonceEnd]...),
		CiphertextAndTag: append([]byte(nil), envelope[nonceEnd:]...),
	}, nil
}
