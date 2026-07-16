package promptcrypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
)

func TestBootstrapV2ProtectorUsesPurposeAndAAD(t *testing.T) {
	key := bytes.Repeat([]byte{0x4a}, aes256KeyBytes)
	nonce := []byte("123456789012")
	protector, err := newBootstrapV2AESGCMProtector(key, "bootstrap-v2", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("创建 Bootstrap v2 保护器失败: %v", err)
	}
	plaintext := []byte(`{"schema_version":"session_bootstrap_outbox_payload.v2","initial_prompt":"secret"}`)
	aad := []byte(`{"schema_version":"session_bootstrap_outbox_payload.v2","command_id":"019f0000-0000-7000-8000-000000000001"}`)
	envelope, err := protector.Protect(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatalf("保护 Bootstrap v2 失败: %v", err)
	}
	if envelope.Algorithm != projectskillbinding.OutboxEncryptionAlgorithm || envelope.KeyVersion != "bootstrap-v2" ||
		!bytes.Equal(envelope.Nonce, nonce) || bytes.Contains(envelope.CiphertextAndTag, []byte("secret")) {
		t.Fatalf("Bootstrap v2 envelope 漂移: %+v", envelope)
	}
	opened, err := protector.Open(context.Background(), envelope, aad)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("解密 Bootstrap v2 失败: plaintext=%q err=%v", opened, err)
	}

	// 使用同一根密钥的 W0 AES key 不能解开 V2 密文，证明 purpose 派生确实分域。
	plainBlock, _ := aes.NewCipher(key)
	plainAEAD, _ := cipher.NewGCM(plainBlock)
	if _, err := plainAEAD.Open(nil, envelope.Nonce, envelope.CiphertextAndTag, aad); err == nil {
		t.Fatal("W0 Prompt key 意外解开了 Bootstrap v2 密文")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("dora.business.key-purpose:" + BootstrapOutboxKeyPurposeV2))
	if bytes.Equal(mac.Sum(nil), key) {
		t.Fatal("用途派生 key 不应等于根密钥")
	}
}

func TestBootstrapV2ProtectorRejectsTamperingAndWrongAAD(t *testing.T) {
	protector, err := newBootstrapV2AESGCMProtector(
		bytes.Repeat([]byte{0x31}, aes256KeyBytes), "bootstrap-v2", bytes.NewReader([]byte("123456789012")),
	)
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	plaintext := []byte(`{"schema_version":"session_bootstrap_outbox_payload.v2"}`)
	aad := []byte(`{"command_id":"019f0000-0000-7000-8000-000000000001"}`)
	envelope, err := protector.Protect(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatalf("保护失败: %v", err)
	}
	if _, err := protector.Open(context.Background(), envelope, []byte(`{"command_id":"019f0000-0000-7000-8000-000000000002"}`)); err == nil {
		t.Fatal("错误 AAD 被接受")
	}
	tampered := envelope
	tampered.CiphertextAndTag = append([]byte(nil), envelope.CiphertextAndTag...)
	tampered.CiphertextAndTag[0] ^= 0xff
	if _, err := protector.Open(context.Background(), tampered, aad); err == nil {
		t.Fatal("损坏密文被接受")
	}
	wrongVersion := envelope
	wrongVersion.KeyVersion = "bootstrap-v3"
	if _, err := protector.Open(context.Background(), wrongVersion, aad); err == nil {
		t.Fatal("错误 key version 被接受")
	}
}

func TestBootstrapV2ProtectorOpensPreviousKeyAfterRotation(t *testing.T) {
	activeKey := bytes.Repeat([]byte{0x41}, aes256KeyBytes)
	previousKey := bytes.Repeat([]byte{0x31}, aes256KeyBytes)
	oldProtector, err := newBootstrapV2AESGCMProtector(
		previousKey, "bootstrap-v1", bytes.NewReader([]byte("123456789012")),
	)
	if err != nil {
		t.Fatalf("创建旧 Bootstrap v2 保护器失败: %v", err)
	}
	plaintext := []byte(`{"schema_version":"session_bootstrap_outbox_payload.v2"}`)
	aad := []byte(`{"command_id":"019f0000-0000-7000-8000-000000000001"}`)
	envelope, err := oldProtector.Protect(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatalf("保护旧 KeyVersion payload 失败: %v", err)
	}
	keyring, err := NewBootstrapV2AESGCMProtectorWithPrevious(
		activeKey, "bootstrap-v2", previousKey, "bootstrap-v1",
	)
	if err != nil {
		t.Fatalf("创建 Bootstrap v2 轮换 Keyring 失败: %v", err)
	}
	opened, err := keyring.Open(context.Background(), envelope, aad)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("previous key payload 无法读取: plaintext=%q err=%v", opened, err)
	}
	newEnvelope, err := keyring.Protect(context.Background(), plaintext, aad)
	if err != nil || newEnvelope.KeyVersion != "bootstrap-v2" {
		t.Fatalf("轮换后新写未使用 active key: envelope=%+v err=%v", newEnvelope, err)
	}
}

func TestBootstrapV2ProtectorRejectsInvalidPreviousPair(t *testing.T) {
	key := bytes.Repeat([]byte{0x41}, aes256KeyBytes)
	if _, err := NewBootstrapV2AESGCMProtectorWithPrevious(
		key, "bootstrap-v2", append([]byte(nil), key...), "bootstrap-v1",
	); err == nil {
		t.Fatal("Bootstrap v2 Keyring 接受了不同版本复用同一根密钥")
	}
	if _, err := NewBootstrapV2AESGCMProtectorWithPrevious(
		key, "bootstrap-v2", bytes.Repeat([]byte{0x31}, aes256KeyBytes), "bootstrap-v2",
	); err == nil {
		t.Fatal("Bootstrap v2 Keyring 接受了重复 KeyVersion")
	}
}
