package session

import (
	"bytes"
	"errors"
	"testing"
)

// TestBuildAndValidateEnvelopeV1 验证 W0 固定二进制布局可执行且 Builder 不复用调用方切片。
func TestBuildAndValidateEnvelopeV1(t *testing.T) {
	nonce := bytes.Repeat([]byte{0x11}, protectedEnvelopeV1NonceSize)
	ciphertextAndTag := bytes.Repeat([]byte{0x22}, protectedEnvelopeV1TagSize+1)
	envelope, err := BuildEnvelopeV1(EnvelopeAlgorithmAES256GCM, nonce, ciphertextAndTag)
	if err != nil {
		t.Fatalf("构建 Envelope v1 失败: %v", err)
	}
	if err := ValidateEnvelopeV1(envelope); err != nil {
		t.Fatalf("验证 Envelope v1 失败: %v", err)
	}
	if !bytes.Equal(envelope[:4], []byte("DRAE")) || envelope[4] != 1 || envelope[5] != 1 || envelope[6] != 12 {
		t.Fatalf("Envelope v1 Header 漂移: %x", envelope[:7])
	}
	nonce[0] = 0xff
	ciphertextAndTag[0] = 0xff
	if envelope[7] == 0xff || envelope[protectedEnvelopeV1HeaderSize+protectedEnvelopeV1NonceSize] == 0xff {
		t.Fatalf("Envelope Builder 复用了调用方可变切片")
	}
}

// TestValidateEnvelopeV1RejectsRawAndMalformedData 验证裸明文及 Header/长度篡改均失败关闭。
func TestValidateEnvelopeV1RejectsRawAndMalformedData(t *testing.T) {
	valid := mustTestEnvelope(t)
	testCases := []struct {
		name     string
		envelope []byte
	}{
		{name: "裸明文", envelope: []byte("plaintext")},
		{name: "错误 Magic", envelope: mutateEnvelope(valid, 0, 'X')},
		{name: "错误 Version", envelope: mutateEnvelope(valid, 4, 2)},
		{name: "错误 Algorithm", envelope: mutateEnvelope(valid, 5, 2)},
		{name: "错误 Nonce 长度", envelope: mutateEnvelope(valid, 6, 8)},
		{name: "只有认证标签无密文", envelope: valid[:protectedEnvelopeV1HeaderSize+protectedEnvelopeV1NonceSize+protectedEnvelopeV1TagSize]},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateEnvelopeV1(testCase.envelope); !errors.Is(err, ErrInvalidProtectedContentEnvelope) {
				t.Fatalf("畸形 Envelope 错误=%v，want ErrInvalidProtectedContentEnvelope", err)
			}
		})
	}
}

// mutateEnvelope 复制固定 Envelope 并篡改单字节，避免测试用例之间共享可变状态。
func mutateEnvelope(source []byte, offset int, value byte) []byte {
	copyValue := append([]byte(nil), source...)
	copyValue[offset] = value
	return copyValue
}
