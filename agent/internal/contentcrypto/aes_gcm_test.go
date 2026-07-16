package contentcrypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

// TestAES256GCMProtectorBuildsAuthenticatedDRAEEnvelope 验证真实 GCM 密文可认证解开且持久化格式符合冻结布局。
func TestAES256GCMProtectorBuildsAuthenticatedDRAEEnvelope(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	nonce := []byte("123456789012")
	protector, err := newAES256GCMProtector(key, "content-key-v1", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	protected, err := protector.Protect(context.Background(), []byte(" e\u0301 "))
	if err != nil {
		t.Fatalf("加密 Prompt 失败: %v", err)
	}
	if protected.KeyVersion != "content-key-v1" {
		t.Fatalf("KeyVersion=%q", protected.KeyVersion)
	}
	if err := session.ValidateEnvelopeV1(protected.Ciphertext); err != nil {
		t.Fatalf("DRAE Envelope 无效: %v", err)
	}
	if got := protected.Ciphertext[7:19]; !bytes.Equal(got, nonce) {
		t.Fatalf("Nonce=%x want=%x", got, nonce)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("创建测试 AES 失败: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("创建测试 GCM 失败: %v", err)
	}
	plaintext, err := aead.Open(nil, protected.Ciphertext[7:19], protected.Ciphertext[19:], nil)
	if err != nil {
		t.Fatalf("认证解密失败: %v", err)
	}
	if string(plaintext) != " e\u0301 " {
		t.Fatalf("明文=%q", plaintext)
	}
}

// TestAES256GCMProtectorRejectsInvalidConfiguration 验证错误密钥、空版本或空随机源均阻止构造。
func TestAES256GCMProtectorRejectsInvalidConfiguration(t *testing.T) {
	testCases := []struct {
		name      string
		key       []byte
		version   string
		randomNil bool
	}{
		{name: "short key", key: make([]byte, 31), version: "v1"},
		{name: "blank version", key: make([]byte, 32), version: "  "},
		{name: "nil random", key: make([]byte, 32), version: "v1", randomNil: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var random io.Reader = bytes.NewReader(make([]byte, 12))
			if testCase.randomNil {
				random = nil
			}
			if _, err := newAES256GCMProtector(testCase.key, testCase.version, random); err == nil {
				t.Fatal("期望非法配置被拒绝")
			}
		})
	}
}

// TestAES256GCMProtectorPreservesCancellation 验证取消请求不会生成或返回部分敏感内容。
func TestAES256GCMProtectorPreservesCancellation(t *testing.T) {
	protector, err := newAES256GCMProtector(make([]byte, 32), "v1", bytes.NewReader(make([]byte, 12)))
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	protected, err := protector.Protect(ctx, []byte("secret"))
	if !errors.Is(err, context.Canceled) || len(protected.Ciphertext) != 0 {
		t.Fatalf("取消结果=%+v err=%v", protected, err)
	}
}

// TestAES256GCMProtectorOpensActiveAndPrevious 验证轮换窗口按明确 KeyVersion 解密 active/previous，且不会遍历试签。
func TestAES256GCMProtectorOpensActiveAndPrevious(t *testing.T) {
	activeKey := []byte("0123456789abcdef0123456789abcdef")
	previousKey := []byte("abcdef0123456789abcdef0123456789")
	keyring, err := NewAES256GCMProtectorWithPrevious(activeKey, "active-v2", previousKey, "previous-v1")
	if err != nil {
		t.Fatalf("创建轮换 Keyring 失败: %v", err)
	}
	for _, fixture := range []struct {
		name      string
		key       []byte
		version   string
		plaintext []byte
	}{
		{name: "active", key: activeKey, version: "active-v2", plaintext: []byte("当前密钥正文")},
		{name: "previous", key: previousKey, version: "previous-v1", plaintext: []byte("旧密钥正文")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			protected := sealTestEnvelope(t, fixture.key, fixture.version, fixture.plaintext)
			digest := sha256.Sum256(fixture.plaintext)
			opened, err := keyring.Open(context.Background(), protected, hex.EncodeToString(digest[:]))
			if err != nil || !bytes.Equal(opened, fixture.plaintext) {
				t.Fatalf("解密=%q err=%v", opened, err)
			}
		})
	}
	unknown := sealTestEnvelope(t, activeKey, "unknown-v3", []byte("secret"))
	digest := sha256.Sum256([]byte("secret"))
	if _, err := keyring.Open(context.Background(), unknown, hex.EncodeToString(digest[:])); !errors.Is(err, session.ErrContentUnavailable) {
		t.Fatalf("未知 KeyVersion 错误=%v", err)
	}
}

// TestAES256GCMProtectorOpenRejectsTamperDigestAndUTF8 验证认证标签、摘要和明文 UTF-8 任一异常均返回同一稳定错误。
func TestAES256GCMProtectorOpenRejectsTamperDigestAndUTF8(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	protector, err := NewAES256GCMProtector(key, "active-v1")
	if err != nil {
		t.Fatalf("创建 Keyring 失败: %v", err)
	}
	plaintext := []byte("安全正文")
	digest := sha256.Sum256(plaintext)
	valid := sealTestEnvelope(t, key, "active-v1", plaintext)

	tampered := valid
	tampered.Ciphertext = append([]byte(nil), valid.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 1
	invalidUTF8 := sealTestEnvelope(t, key, "active-v1", []byte{0xff})
	invalidUTF8Digest := sha256.Sum256([]byte{0xff})
	for _, fixture := range []struct {
		name      string
		protected session.ProtectedContent
		digest    string
	}{
		{name: "认证标签篡改", protected: tampered, digest: hex.EncodeToString(digest[:])},
		{name: "摘要不一致", protected: valid, digest: hex.EncodeToString(make([]byte, sha256.Size))},
		{name: "非法 UTF-8", protected: invalidUTF8, digest: hex.EncodeToString(invalidUTF8Digest[:])},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := protector.Open(context.Background(), fixture.protected, fixture.digest); !errors.Is(err, session.ErrContentUnavailable) {
				t.Fatalf("错误=%v", err)
			}
		})
	}
}

// TestAES256GCMProtectorRejectsInvalidPreviousVersion 验证 previous KeyVersion 的空值、重复、超长与非规范编码均阻止启动。
func TestAES256GCMProtectorRejectsInvalidPreviousVersion(t *testing.T) {
	key := make([]byte, 32)
	previous := bytes.Repeat([]byte{1}, 32)
	for _, version := range []string{"active-v1", "Upper", strings.Repeat("a", 65), "bad version"} {
		if _, err := NewAES256GCMProtectorWithPrevious(key, "active-v1", previous, version); err == nil {
			t.Fatalf("非法 previous version=%q 被接受", version)
		}
	}
}

// sealTestEnvelope 使用独立 GCM Fixture 构造可供只读解密测试的 DRAE v1 Envelope。
func sealTestEnvelope(t *testing.T, key []byte, version string, plaintext []byte) session.ProtectedContent {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("创建 Fixture AES 失败: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("创建 Fixture GCM 失败: %v", err)
	}
	nonce := []byte("123456789012")
	envelope, err := session.BuildEnvelopeV1(
		session.EnvelopeAlgorithmAES256GCM, nonce, aead.Seal(nil, nonce, plaintext, nil),
	)
	if err != nil {
		t.Fatalf("构建 Fixture Envelope 失败: %v", err)
	}
	return session.ProtectedContent{Ciphertext: envelope, KeyVersion: version}
}
