package promptcrypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

func TestAESGCMProtectorEncryptsCanonicalPrompt(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, aes256KeyBytes)
	nonce := []byte("123456789012")
	protector, err := NewAESGCMProtector(key, "prompt-key-v1", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	prompt := " é "
	digest := project.SHA256Digest([]byte(prompt))

	payload, err := protector.Protect(context.Background(), prompt, digest)
	if err != nil {
		t.Fatalf("protect prompt: %v", err)
	}
	if payload.Algorithm != project.PromptEncryptionAlgorithm || payload.KeyVersion != "prompt-key-v1" ||
		!bytes.Equal(payload.Nonce, nonce) || payload.PayloadDigest != digest || bytes.Contains(payload.Ciphertext, []byte(prompt)) {
		t.Fatalf("unexpected protected payload: %+v", payload)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	plaintext, err := aead.Open(nil, payload.Nonce, payload.Ciphertext, nil)
	if err != nil || string(plaintext) != prompt {
		t.Fatalf("decrypt protected prompt: plaintext=%q err=%v", plaintext, err)
	}
	revealed, err := protector.Reveal(context.Background(), *payload)
	if err != nil || revealed != prompt {
		t.Fatalf("reveal protected prompt: prompt=%q err=%v", revealed, err)
	}
}

func TestAESGCMProtectorRejectsConfigurationAndSemanticDrift(t *testing.T) {
	if _, err := NewAESGCMProtector([]byte("short"), "key-v1", bytes.NewReader(nil)); err == nil {
		t.Fatal("expected short key rejection")
	}
	protector, err := NewAESGCMProtector(bytes.Repeat([]byte{1}, aes256KeyBytes), "key-v1", strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	if _, err := protector.Protect(context.Background(), "e\u0301", project.SHA256Digest([]byte("é"))); err == nil {
		t.Fatal("expected non-canonical prompt rejection")
	}
	if _, err := protector.Protect(context.Background(), "prompt", project.SHA256Digest([]byte("other"))); err == nil {
		t.Fatal("expected digest mismatch rejection")
	}
}

func TestAESGCMProtectorPreservesContextCancellation(t *testing.T) {
	protector, err := NewAESGCMProtector(bytes.Repeat([]byte{1}, aes256KeyBytes), "key-v1", strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = protector.Protect(ctx, "prompt", project.SHA256Digest([]byte("prompt")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestAESGCMProtectorRevealRejectsTamperingAndWrongKeyVersion(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, aes256KeyBytes)
	protector, err := NewAESGCMProtector(key, "key-v1", strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	prompt := "prompt"
	payload, err := protector.Protect(context.Background(), prompt, project.SHA256Digest([]byte(prompt)))
	if err != nil {
		t.Fatalf("protect prompt: %v", err)
	}

	wrongVersion := *payload
	wrongVersion.KeyVersion = "key-v2"
	if _, err := protector.Reveal(context.Background(), wrongVersion); err == nil {
		t.Fatal("expected wrong key version rejection")
	}
	tampered := *payload
	tampered.Ciphertext = append([]byte(nil), payload.Ciphertext...)
	tampered.Ciphertext[0] ^= 0xff
	if _, err := protector.Reveal(context.Background(), tampered); err == nil {
		t.Fatal("expected authentication failure")
	}
	wrongDigest := *payload
	wrongDigest.PayloadDigest = project.SHA256Digest([]byte("other"))
	if _, err := protector.Reveal(context.Background(), wrongDigest); err == nil {
		t.Fatal("expected digest mismatch rejection")
	}
}

func TestAESGCMProtectorReadsPreviousKeyAndWritesActiveKey(t *testing.T) {
	activeKey := bytes.Repeat([]byte{0x42}, aes256KeyBytes)
	previousKey := bytes.Repeat([]byte{0x31}, aes256KeyBytes)
	oldProtector, err := NewAESGCMProtector(previousKey, "key-v1", strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("create old protector: %v", err)
	}
	prompt := "historical prompt"
	payload, err := oldProtector.Protect(context.Background(), prompt, project.SHA256Digest([]byte(prompt)))
	if err != nil {
		t.Fatalf("protect old prompt: %v", err)
	}
	keyring, err := NewAESGCMProtectorWithPreviousSystemRandom(activeKey, "key-v2", previousKey, "key-v1")
	if err != nil {
		t.Fatalf("create prompt keyring: %v", err)
	}
	revealed, err := keyring.Reveal(context.Background(), *payload)
	if err != nil || revealed != prompt {
		t.Fatalf("reveal previous prompt=%q err=%v", revealed, err)
	}
	newPayload, err := keyring.Protect(context.Background(), prompt, project.SHA256Digest([]byte(prompt)))
	if err != nil || newPayload.KeyVersion != "key-v2" {
		t.Fatalf("new prompt did not use active key: payload=%+v err=%v", newPayload, err)
	}
	if _, err := NewAESGCMProtectorWithPreviousSystemRandom(
		activeKey, "key-v2", append([]byte(nil), activeKey...), "key-v1",
	); err == nil {
		t.Fatal("prompt keyring accepted the same root key under two versions")
	}
}
