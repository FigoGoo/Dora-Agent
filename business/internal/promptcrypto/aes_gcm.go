// Package promptcrypto 实现 Business 首提示词 Outbox 的认证加密适配器。
package promptcrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

const aes256KeyBytes = 32

// AESGCMProtector 使用启动时注入的 256-bit Key 和版本引用保护非空首提示词。
// 该类型只保留 AEAD 实例，不对外暴露 Key；每次调用从随机源读取独立 12-byte Nonce。
type AESGCMProtector struct {
	aead       cipher.AEAD
	keyVersion string
	random     io.Reader
}

// NewAESGCMProtector 校验 AES-256 Key、Key Version 与随机源并创建可并发复用的保护器。
func NewAESGCMProtector(key []byte, keyVersion string, random io.Reader) (*AESGCMProtector, error) {
	if len(key) != aes256KeyBytes || strings.TrimSpace(keyVersion) == "" || len(keyVersion) > 64 || random == nil {
		return nil, errors.New("create prompt protector: invalid key configuration")
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, fmt.Errorf("create prompt AES block: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create prompt AES-GCM: %w", err)
	}
	return &AESGCMProtector{aead: aead, keyVersion: keyVersion, random: random}, nil
}

// NewAESGCMProtectorWithSystemRandom 使用 crypto/rand 创建生产保护器。
func NewAESGCMProtectorWithSystemRandom(key []byte, keyVersion string) (*AESGCMProtector, error) {
	return NewAESGCMProtector(key, keyVersion, rand.Reader)
}

// Protect 核对调用方已经执行冻结的 Prompt 规范化与 Digest，再返回 AES-256-GCM 密文元数据。
// 方法不记录正文或 Key，失败由 Project 应用服务收敛为稳定 ErrPromptProtection。
func (protector *AESGCMProtector) Protect(ctx context.Context, normalizedPrompt string, promptDigest project.Digest) (*project.EncryptedPayload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonical, actualDigest, present, err := project.NormalizeEnsureSessionPrompt(normalizedPrompt)
	if err != nil || !present || canonical != normalizedPrompt || actualDigest != promptDigest {
		return nil, errors.New("protect prompt: canonical prompt mismatch")
	}
	nonce := make([]byte, protector.aead.NonceSize())
	if _, err := io.ReadFull(protector.random, nonce); err != nil {
		return nil, fmt.Errorf("read prompt nonce: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// W0 不定义额外 AAD；Project、Owner 和语义摘要由独立 Canonical Digest 与 Agent RPC 校验绑定。
	ciphertext := protector.aead.Seal(nil, nonce, []byte(normalizedPrompt), nil)
	return &project.EncryptedPayload{
		Algorithm: project.PromptEncryptionAlgorithm, KeyVersion: protector.keyVersion,
		Nonce: append([]byte(nil), nonce...), Ciphertext: ciphertext, PayloadDigest: promptDigest,
	}, nil
}

// Reveal 校验算法、Key Version、Nonce 和认证标签后返回规范化明文，并再次核对持久化 Digest。
// 任一校验失败都只返回本包错误，由 Dispatcher 收敛为稳定错误码，正文与密钥元数据不得进入日志。
func (protector *AESGCMProtector) Reveal(ctx context.Context, payload project.EncryptedPayload) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if payload.Algorithm != project.PromptEncryptionAlgorithm || payload.KeyVersion != protector.keyVersion ||
		len(payload.Nonce) != protector.aead.NonceSize() || len(payload.Ciphertext) <= protector.aead.Overhead() {
		return "", errors.New("reveal prompt: invalid encryption metadata")
	}
	plaintext, err := protector.aead.Open(nil, payload.Nonce, payload.Ciphertext, nil)
	if err != nil {
		return "", errors.New("reveal prompt: authentication failed")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	prompt := string(plaintext)
	normalized, digest, present, err := project.NormalizeEnsureSessionPrompt(prompt)
	if err != nil || !present || normalized != prompt || digest != payload.PayloadDigest {
		return "", errors.New("reveal prompt: canonical digest mismatch")
	}
	return prompt, nil
}
