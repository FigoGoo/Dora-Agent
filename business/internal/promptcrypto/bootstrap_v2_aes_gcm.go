package promptcrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
)

const (
	// BootstrapOutboxKeyPurposeV2 把完整 V2 Bootstrap Outbox 与 W0 Prompt 密文分隔到不同密码学用途域。
	BootstrapOutboxKeyPurposeV2 = "session_bootstrap_outbox_payload.v2"
	bootstrapOutboxMaxPlaintext = 4 * 1024 * 1024
	bootstrapOutboxMaxAAD       = 4 * 1024
)

// BootstrapV2AESGCMProtector 使用用途派生的 AES-256-GCM key 保护完整 Session Bootstrap v2 plaintext。
// 即使根密钥与 W0 Prompt 共用，派生 key 与必需 AAD 也禁止两类密文跨域解密。
type BootstrapV2AESGCMProtector struct {
	aead       cipher.AEAD
	keyVersion string
	random     io.Reader
	readKeys   map[string]cipher.AEAD
}

// NewBootstrapV2AESGCMProtector 使用系统随机源创建生产保护器。
func NewBootstrapV2AESGCMProtector(key []byte, keyVersion string) (*BootstrapV2AESGCMProtector, error) {
	return newBootstrapV2AESGCMProtector(key, keyVersion, rand.Reader)
}

// NewBootstrapV2AESGCMProtectorWithPrevious 创建 active-write、active/previous-read 的有限轮换 Keyring。
// Previous pair 必须同时为空或同时有效；不同版本不得复用同一根密钥。
func NewBootstrapV2AESGCMProtectorWithPrevious(
	key []byte,
	keyVersion string,
	previousKey []byte,
	previousKeyVersion string,
) (*BootstrapV2AESGCMProtector, error) {
	protector, err := newBootstrapV2AESGCMProtector(key, keyVersion, rand.Reader)
	if err != nil {
		return nil, err
	}
	if len(previousKey) == 0 && previousKeyVersion == "" {
		return protector, nil
	}
	if len(previousKey) != aes256KeyBytes || !validBootstrapKeyVersion(previousKeyVersion) ||
		previousKeyVersion == protector.keyVersion || subtle.ConstantTimeCompare(key, previousKey) == 1 {
		return nil, errors.New("create Bootstrap v2 protector: previous key pair is invalid")
	}
	previousAEAD, err := newBootstrapV2AEAD(previousKey)
	if err != nil {
		return nil, err
	}
	protector.readKeys[previousKeyVersion] = previousAEAD
	return protector, nil
}

func newBootstrapV2AESGCMProtector(key []byte, keyVersion string, random io.Reader) (*BootstrapV2AESGCMProtector, error) {
	if len(key) != aes256KeyBytes || !validBootstrapKeyVersion(keyVersion) || random == nil {
		return nil, errors.New("create Bootstrap v2 protector: invalid key configuration")
	}
	aead, err := newBootstrapV2AEAD(key)
	if err != nil {
		return nil, err
	}
	return &BootstrapV2AESGCMProtector{
		aead: aead, keyVersion: keyVersion, random: random,
		readKeys: map[string]cipher.AEAD{keyVersion: aead},
	}, nil
}

func newBootstrapV2AEAD(key []byte) (cipher.AEAD, error) {
	mac := hmac.New(sha256.New, append([]byte(nil), key...))
	_, _ = mac.Write([]byte("dora.business.key-purpose:" + BootstrapOutboxKeyPurposeV2))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create Bootstrap v2 AES block: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create Bootstrap v2 AES-GCM: %w", err)
	}
	if aead.NonceSize() != 12 || aead.Overhead() != 16 {
		return nil, errors.New("create Bootstrap v2 protector: unsupported GCM dimensions")
	}
	return aead, nil
}

// Protect 只执行有界本地认证加密，可在 Repository 事务内使用；它不访问网络、文件或 KMS。
func (protector *BootstrapV2AESGCMProtector) Protect(ctx context.Context, plaintext []byte, aad []byte) (projectskillbinding.EncryptedEnvelopeV2, error) {
	if err := ctx.Err(); err != nil {
		return projectskillbinding.EncryptedEnvelopeV2{}, err
	}
	if !validBootstrapPlaintextAndAAD(plaintext, aad) {
		return projectskillbinding.EncryptedEnvelopeV2{}, errors.New("protect Bootstrap v2 payload: invalid plaintext or AAD")
	}
	nonce := make([]byte, protector.aead.NonceSize())
	if _, err := io.ReadFull(protector.random, nonce); err != nil {
		return projectskillbinding.EncryptedEnvelopeV2{}, errors.New("protect Bootstrap v2 payload: random source unavailable")
	}
	if err := ctx.Err(); err != nil {
		return projectskillbinding.EncryptedEnvelopeV2{}, err
	}
	ciphertext := protector.aead.Seal(nil, nonce, plaintext, aad)
	return projectskillbinding.EncryptedEnvelopeV2{
		Algorithm:        projectskillbinding.OutboxEncryptionAlgorithm,
		KeyVersion:       protector.keyVersion,
		Nonce:            append([]byte(nil), nonce...),
		CiphertextAndTag: ciphertext,
	}, nil
}

// Open 使用持久化 envelope 与同一 Canonical AAD 认证解密；任一元数据、AAD 或密文漂移均失败关闭。
func (protector *BootstrapV2AESGCMProtector) Open(ctx context.Context, envelope projectskillbinding.EncryptedEnvelopeV2, aad []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	aead, keyAvailable := protector.readKeys[envelope.KeyVersion]
	if envelope.Algorithm != projectskillbinding.OutboxEncryptionAlgorithm || !keyAvailable ||
		len(envelope.Nonce) != aead.NonceSize() || len(envelope.CiphertextAndTag) <= aead.Overhead() ||
		len(aad) == 0 || len(aad) > bootstrapOutboxMaxAAD || !utf8.Valid(aad) {
		return nil, errors.New("open Bootstrap v2 payload: content unavailable")
	}
	plaintext, err := aead.Open(nil, envelope.Nonce, envelope.CiphertextAndTag, aad)
	if err != nil || !validBootstrapPlaintextAndAAD(plaintext, aad) {
		return nil, errors.New("open Bootstrap v2 payload: content unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plaintext, nil
}

func validBootstrapPlaintextAndAAD(plaintext []byte, aad []byte) bool {
	return len(plaintext) > 0 && len(plaintext) <= bootstrapOutboxMaxPlaintext && utf8.Valid(plaintext) &&
		len(aad) > 0 && len(aad) <= bootstrapOutboxMaxAAD && utf8.Valid(aad)
}

func validBootstrapKeyVersion(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, character := range []byte(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

var _ projectskillbinding.OutboxPayloadProtectorV2 = (*BootstrapV2AESGCMProtector)(nil)
