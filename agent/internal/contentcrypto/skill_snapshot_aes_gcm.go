package contentcrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/google/uuid"
)

const (
	// SkillSnapshotKeyPurposeV1 是 Session Skill Runtime Content 的独立密钥派生与 AAD 域。
	// 它与 Prompt 正文保护 purpose 分离，即使配置误用了相同根密钥，也不会生成可跨域解密的 AEAD key。
	SkillSnapshotKeyPurposeV1  = "session_skill_snapshot_item.v1"
	skillSnapshotMaxItemsV1    = 32
	skillSnapshotMaxItemBytes  = 128 * 1024
	skillSnapshotMaxTotalBytes = 1024 * 1024
)

// SkillSnapshotAES256GCMProtector 使用独立 purpose 派生的 AES-256-GCM key，按 Item 身份 AAD 批量保护 Runtime Content。
// active key 只用于新写入，active/previous key 按明确 KeyVersion 用于历史只读；类型不记录明文、AAD、Nonce、密钥或密文。
type SkillSnapshotAES256GCMProtector struct {
	aead       cipher.AEAD
	keyVersion string
	random     io.Reader
	readKeys   map[string]cipher.AEAD
}

// NewSkillSnapshotAES256GCMProtector 创建 Agent Skill Snapshot 专用保护器。
// key 必须是 32 字节根密钥；构造函数以固定 purpose 派生实际 AEAD key，配置或随机源无效时失败启动。
func NewSkillSnapshotAES256GCMProtector(key []byte, keyVersion string) (*SkillSnapshotAES256GCMProtector, error) {
	return newSkillSnapshotAES256GCMProtector(key, keyVersion, rand.Reader)
}

// NewSkillSnapshotAES256GCMProtectorWithPrevious 创建支持 active 写入和 active/previous 只读的专用轮换 Keyring。
// previous key/version 必须同时为空或同时有效，且旧版本不能与 active 版本相同。
func NewSkillSnapshotAES256GCMProtectorWithPrevious(
	key []byte,
	keyVersion string,
	previousKey []byte,
	previousKeyVersion string,
) (*SkillSnapshotAES256GCMProtector, error) {
	protector, err := newSkillSnapshotAES256GCMProtector(key, keyVersion, rand.Reader)
	if err != nil {
		return nil, err
	}
	previousKeyVersion = strings.TrimSpace(previousKeyVersion)
	if len(previousKey) == 0 && previousKeyVersion == "" {
		return protector, nil
	}
	if len(previousKey) != aes256KeySize || !validSkillSnapshotKeyVersion(previousKeyVersion) ||
		previousKeyVersion == protector.keyVersion || subtle.ConstantTimeCompare(key, previousKey) == 1 {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM protector: previous key pair is invalid")
	}
	previousAEAD, err := newSkillSnapshotGCM(previousKey)
	if err != nil {
		return nil, err
	}
	protector.readKeys[previousKeyVersion] = previousAEAD
	return protector, nil
}

// newSkillSnapshotAES256GCMProtector 仅允许同包测试注入确定性随机源；生产装配必须使用导出构造函数。
func newSkillSnapshotAES256GCMProtector(
	key []byte,
	keyVersion string,
	random io.Reader,
) (*SkillSnapshotAES256GCMProtector, error) {
	if len(key) != aes256KeySize {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM protector: key must be 32 bytes")
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if !validSkillSnapshotKeyVersion(keyVersion) {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM protector: key version is invalid")
	}
	if random == nil {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM protector: random source is required")
	}
	aead, err := newSkillSnapshotGCM(key)
	if err != nil {
		return nil, err
	}
	return &SkillSnapshotAES256GCMProtector{
		aead: aead, keyVersion: keyVersion, random: random,
		readKeys: map[string]cipher.AEAD{keyVersion: aead},
	}, nil
}

// newSkillSnapshotGCM 以固定 purpose 从根密钥派生专用 32 字节 key，再创建固定维度 GCM。
// 派生保证该 key 与 Session Prompt 使用的无 AAD key 在密码学上分域，不能互换密文。
func newSkillSnapshotGCM(rootKey []byte) (cipher.AEAD, error) {
	mac := hmac.New(sha256.New, append([]byte(nil), rootKey...))
	_, _ = mac.Write([]byte("dora.agent.key-purpose:" + SkillSnapshotKeyPurposeV1))
	derivedKey := mac.Sum(nil)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("create Skill Snapshot AES-256 block cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM: %w", err)
	}
	if aead.NonceSize() != 12 || aead.Overhead() != 16 {
		return nil, fmt.Errorf("create Skill Snapshot AES-256-GCM protector: unsupported GCM dimensions")
	}
	return aead, nil
}

// validSkillSnapshotKeyVersion 限制持久化 key version 为 1..128 字节的小写 ASCII 唯一表示。
func validSkillSnapshotKeyVersion(value string) bool {
	if len(value) == 0 || len(value) > 128 {
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

// skillSnapshotAADV1 是 compact JSON AAD 的固定字段顺序，禁止使用 map 或拼接歧义字符串。
type skillSnapshotAADV1 struct {
	// Purpose 是固定密钥与 AAD 域，阻止跨内容类型解密。
	Purpose string `json:"purpose"`
	// SessionID 是快照所属 Session UUIDv7。
	SessionID string `json:"session_id"`
	// SkillID 是 Business Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// PublishedSnapshotID 是不可变 Published Snapshot UUIDv7。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// RuntimeContentDigest 是明文 canonical 摘要并绑定认证身份。
	RuntimeContentDigest string `json:"runtime_content_digest"`
}

// ProtectBatch 在数据库事务前一次性保护全部 canonical Runtime Content。
// 输入按协议 hard ceiling 有界；每个 Item 使用新随机 Nonce，任一步失败都丢弃局部结果并返回稳定失败。
func (p *SkillSnapshotAES256GCMProtector) ProtectBatch(
	ctx context.Context,
	plaintexts []session.SkillSnapshotPlaintext,
) ([]session.SkillSnapshotCiphertext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(plaintexts) == 0 || len(plaintexts) > skillSnapshotMaxItemsV1 {
		return nil, fmt.Errorf("protect Skill Snapshot content: item count is invalid")
	}
	results := make([]session.SkillSnapshotCiphertext, len(plaintexts))
	totalBytes := 0
	for index, plaintext := range plaintexts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateSkillSnapshotContentIdentity(plaintext.Identity); err != nil {
			return nil, fmt.Errorf("protect Skill Snapshot content: invalid identity")
		}
		if len(plaintext.CanonicalBytes) == 0 || len(plaintext.CanonicalBytes) > skillSnapshotMaxItemBytes ||
			!utf8.Valid(plaintext.CanonicalBytes) {
			return nil, fmt.Errorf("protect Skill Snapshot content: invalid canonical content")
		}
		totalBytes += len(plaintext.CanonicalBytes)
		if totalBytes > skillSnapshotMaxTotalBytes {
			return nil, fmt.Errorf("protect Skill Snapshot content: total canonical content exceeds hard ceiling")
		}
		aad, err := buildSkillSnapshotAADV1(plaintext.Identity)
		if err != nil {
			return nil, fmt.Errorf("protect Skill Snapshot content: encode AAD")
		}
		nonce := make([]byte, p.aead.NonceSize())
		if _, err := io.ReadFull(p.random, nonce); err != nil {
			return nil, fmt.Errorf("protect Skill Snapshot content: generate nonce")
		}
		ciphertextAndTag := p.aead.Seal(nil, nonce, plaintext.CanonicalBytes, aad)
		envelope, err := session.BuildEnvelopeV1(session.EnvelopeAlgorithmAES256GCM, nonce, ciphertextAndTag)
		if err != nil {
			return nil, fmt.Errorf("protect Skill Snapshot content: build envelope")
		}
		results[index] = session.SkillSnapshotCiphertext{
			Identity:  plaintext.Identity,
			Protected: session.ProtectedContent{Ciphertext: envelope, KeyVersion: p.keyVersion},
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// OpenBatch 按每个 Item 的明确 KeyVersion 和 AAD 认证解密，随后重验 UTF-8、大小和 Runtime Content digest。
// 任一 key 不可用、密文损坏、身份串线或摘要不一致都返回同一稳定错误，不返回部分明文。
func (p *SkillSnapshotAES256GCMProtector) OpenBatch(
	ctx context.Context,
	ciphertexts []session.SkillSnapshotCiphertext,
) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ciphertexts) == 0 || len(ciphertexts) > skillSnapshotMaxItemsV1 {
		return nil, session.ErrContentUnavailable
	}
	plaintexts := make([][]byte, len(ciphertexts))
	totalBytes := 0
	for index, encrypted := range ciphertexts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if validateSkillSnapshotContentIdentity(encrypted.Identity) != nil {
			return nil, session.ErrContentUnavailable
		}
		parsed, err := session.ParseEnvelopeV1(encrypted.Protected.Ciphertext)
		if err != nil || parsed.Algorithm != session.EnvelopeAlgorithmAES256GCM {
			return nil, session.ErrContentUnavailable
		}
		aead, ok := p.readKeys[encrypted.Protected.KeyVersion]
		if !ok {
			return nil, session.ErrContentUnavailable
		}
		aad, err := buildSkillSnapshotAADV1(encrypted.Identity)
		if err != nil {
			return nil, session.ErrContentUnavailable
		}
		plaintext, err := aead.Open(nil, parsed.Nonce, parsed.CiphertextAndTag, aad)
		if err != nil || len(plaintext) == 0 || len(plaintext) > skillSnapshotMaxItemBytes || !utf8.Valid(plaintext) {
			return nil, session.ErrContentUnavailable
		}
		totalBytes += len(plaintext)
		if totalBytes > skillSnapshotMaxTotalBytes {
			return nil, session.ErrContentUnavailable
		}
		expectedDigest, err := hex.DecodeString(encrypted.Identity.RuntimeContentDigest)
		actualDigest := sha256.Sum256(plaintext)
		if err != nil || subtle.ConstantTimeCompare(expectedDigest, actualDigest[:]) != 1 {
			return nil, session.ErrContentUnavailable
		}
		plaintexts[index] = plaintext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plaintexts, nil
}

// validateSkillSnapshotContentIdentity 校验 AAD 身份使用规范 UUIDv7 和小写 SHA-256，拒绝第二种文本表示。
func validateSkillSnapshotContentIdentity(identity session.SkillSnapshotContentIdentity) error {
	for _, value := range []string{identity.SessionID, identity.SkillID, identity.PublishedSnapshotID} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 7 || parsed.String() != value {
			return fmt.Errorf("invalid UUIDv7")
		}
	}
	if len(identity.RuntimeContentDigest) != sha256.Size*2 ||
		strings.ToLower(identity.RuntimeContentDigest) != identity.RuntimeContentDigest {
		return fmt.Errorf("invalid digest")
	}
	decoded, err := hex.DecodeString(identity.RuntimeContentDigest)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid digest")
	}
	return nil
}

// buildSkillSnapshotAADV1 生成字段顺序固定的 compact JSON AAD，不包含 Runtime Content 明文。
func buildSkillSnapshotAADV1(identity session.SkillSnapshotContentIdentity) ([]byte, error) {
	return json.Marshal(skillSnapshotAADV1{
		Purpose: SkillSnapshotKeyPurposeV1, SessionID: identity.SessionID,
		SkillID: identity.SkillID, PublishedSnapshotID: identity.PublishedSnapshotID,
		RuntimeContentDigest: identity.RuntimeContentDigest,
	})
}

var _ session.SkillSnapshotContentProtector = (*SkillSnapshotAES256GCMProtector)(nil)
