// Package contentcrypto 提供 Session 敏感正文的真实 AEAD 加密适配器。
package contentcrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

const aes256KeySize = 32

const maxPlaintextBytes = 65_536

// AES256GCMProtector 使用启动时冻结的 32 字节密钥，把非空 Prompt 加密为 DRAE v1 Envelope。
// 该类型同时按明确 KeyVersion 提供 active/previous 只读解密，不记录明文、密钥、Nonce 或完整密文。
type AES256GCMProtector struct {
	aead       cipher.AEAD
	keyVersion string
	random     io.Reader
	readKeys   map[string]cipher.AEAD
}

// NewAES256GCMProtector 创建真实 AES-256-GCM 保护器并校验密钥长度、版本与随机源。
// 成功后复制构造所需状态，不保留调用方可变密钥切片；配置错误会阻止 Transport 启动。
func NewAES256GCMProtector(key []byte, keyVersion string) (*AES256GCMProtector, error) {
	return newAES256GCMProtector(key, keyVersion, rand.Reader)
}

// NewAES256GCMProtectorWithPrevious 创建同时支持 active 写入与 active/previous 只读解密的轮换 Keyring。
// previousKey 与 previousKeyVersion 必须同时为空或同时有效，且旧版本不得与 active 版本相同。
func NewAES256GCMProtectorWithPrevious(
	key []byte,
	keyVersion string,
	previousKey []byte,
	previousKeyVersion string,
) (*AES256GCMProtector, error) {
	protector, err := newAES256GCMProtector(key, keyVersion, rand.Reader)
	if err != nil {
		return nil, err
	}
	previousKeyVersion = strings.TrimSpace(previousKeyVersion)
	if len(previousKey) == 0 && previousKeyVersion == "" {
		return protector, nil
	}
	if len(previousKey) != aes256KeySize || !validKeyVersion(previousKeyVersion) || previousKeyVersion == protector.keyVersion {
		return nil, fmt.Errorf("create AES-256-GCM content protector: previous key pair is invalid")
	}
	previousAEAD, err := newGCM(previousKey)
	if err != nil {
		return nil, err
	}
	protector.readKeys[previousKeyVersion] = previousAEAD
	return protector, nil
}

// newAES256GCMProtector 允许测试注入确定性随机源；生产 Composition Root 只能调用导出构造函数。
func newAES256GCMProtector(key []byte, keyVersion string, random io.Reader) (*AES256GCMProtector, error) {
	if len(key) != aes256KeySize {
		return nil, fmt.Errorf("create AES-256-GCM content protector: key must be 32 bytes")
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if !validKeyVersion(keyVersion) {
		return nil, fmt.Errorf("create AES-256-GCM content protector: key version is required")
	}
	if random == nil {
		return nil, fmt.Errorf("create AES-256-GCM content protector: random source is required")
	}
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return &AES256GCMProtector{
		aead: aead, keyVersion: keyVersion, random: random,
		readKeys: map[string]cipher.AEAD{keyVersion: aead},
	}, nil
}

// validKeyVersion 限制内容密钥版本为 1..64 字节的小写 ASCII 唯一表示，禁止空白和控制字符进入持久字段。
func validKeyVersion(value string) bool {
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

// newGCM 从独立密钥副本创建固定维度的 AES-256-GCM 实例。
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, fmt.Errorf("create AES-256 block cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-256-GCM: %w", err)
	}
	if aead.NonceSize() != 12 || aead.Overhead() != 16 {
		return nil, fmt.Errorf("create AES-256-GCM content protector: unsupported GCM dimensions")
	}
	return aead, nil
}

// Protect 加密非空敏感正文并返回包含算法、Nonce、密文和认证标签的 DRAE v1 Envelope。
// 请求取消在读取随机数和加密前后都被检查；失败时不返回部分 Envelope 或密钥材料。
func (p *AES256GCMProtector) Protect(ctx context.Context, plaintext []byte) (session.ProtectedContent, error) {
	if err := ctx.Err(); err != nil {
		return session.ProtectedContent{}, err
	}
	if len(plaintext) == 0 {
		return session.ProtectedContent{}, fmt.Errorf("protect Session content: plaintext is empty")
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(p.random, nonce); err != nil {
		return session.ProtectedContent{}, fmt.Errorf("protect Session content: generate nonce: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return session.ProtectedContent{}, err
	}
	ciphertextAndTag := p.aead.Seal(nil, nonce, plaintext, nil)
	envelope, err := session.BuildEnvelopeV1(session.EnvelopeAlgorithmAES256GCM, nonce, ciphertextAndTag)
	if err != nil {
		return session.ProtectedContent{}, fmt.Errorf("protect Session content: build envelope: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return session.ProtectedContent{}, err
	}
	return session.ProtectedContent{Ciphertext: envelope, KeyVersion: p.keyVersion}, nil
}

// Open 校验 DRAE v1 结构、按明确 KeyVersion 选择 active/previous 密钥、认证解密并核对 UTF-8、长度与摘要。
// 任一失败只返回稳定错误，不返回部分明文，也不会遍历试用其他密钥版本。
func (p *AES256GCMProtector) Open(
	ctx context.Context,
	protected session.ProtectedContent,
	contentDigest string,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed, err := session.ParseEnvelopeV1(protected.Ciphertext)
	if err != nil || parsed.Algorithm != session.EnvelopeAlgorithmAES256GCM {
		return nil, session.ErrContentUnavailable
	}
	aead, ok := p.readKeys[protected.KeyVersion]
	if !ok {
		return nil, session.ErrContentUnavailable
	}
	plaintext, err := aead.Open(nil, parsed.Nonce, parsed.CiphertextAndTag, nil)
	if err != nil || len(plaintext) == 0 || len(plaintext) > maxPlaintextBytes || !utf8.Valid(plaintext) {
		return nil, session.ErrContentUnavailable
	}
	expectedDigest, err := hex.DecodeString(contentDigest)
	if err != nil || len(expectedDigest) != sha256.Size || strings.ToLower(contentDigest) != contentDigest {
		return nil, session.ErrContentUnavailable
	}
	actualDigest := sha256.Sum256(plaintext)
	if subtle.ConstantTimeCompare(expectedDigest, actualDigest[:]) != 1 {
		return nil, session.ErrContentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plaintext, nil
}

var _ session.ContentProtector = (*AES256GCMProtector)(nil)
