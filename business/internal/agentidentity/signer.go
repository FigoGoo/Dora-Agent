// Package agentidentity 负责签发 Business 到 Agent HTTP 的一次性用户身份断言。
package agentidentity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// HeaderAssertion 携带 Base64URL 无补位编码的 16 行 Canonical 断言。
	HeaderAssertion = "X-Dora-Identity-Assertion"
	// HeaderKeyVersion 携带签名密钥版本，必须和 Canonical 第四行一致。
	HeaderKeyVersion = "X-Dora-Identity-Key-Version"
	// HeaderSignature 携带小写十六进制 HMAC-SHA256 签名。
	HeaderSignature = "X-Dora-Identity-Signature"
	// ScopeWorkspaceRead 允许读取单个已绑定 Session 的 Workspace Snapshot。
	ScopeWorkspaceRead = "agent.session.workspace.read"
	// ScopeEventsRead 允许读取单个已绑定 Session 的持久事件流。
	ScopeEventsRead = "agent.session.events.read"
	// ScopeToolsRead 允许读取单个已绑定 Session 的静态 Tool Definition Catalog。
	ScopeToolsRead = "agent.session.tools.read"

	assertionSchema              = "agent_http_identity_assertion.v1"
	assertionIssuer              = "dora-business-service"
	assertionAudience            = "dora.agent.http.v1"
	nonceBytes                   = 16
	maximumTTL                   = 60 * time.Second
	maximumJavaScriptSafeInteger = uint64(1<<53 - 1)
)

var (
	// ErrInvalidAssertionInput 表示调用方提供的身份、路径、Scope 或会话版本不满足冻结协议。
	ErrInvalidAssertionInput = errors.New("invalid agent identity assertion input")
	// ErrAssertionSigningUnavailable 表示随机源或时钟无法安全签发一次性断言。
	ErrAssertionSigningUnavailable = errors.New("agent identity assertion signing unavailable")
)

// Clock 为身份断言冻结签发时间提供可注入时钟。
type Clock interface {
	// Now 返回当前时间；Signer 会在一次签发内冻结为 Unix 毫秒。
	Now() time.Time
}

// Config 保存当前 Business Active 身份断言密钥和短期有效期。
// 密钥轮换由配置切换 active kid 完成，Agent 在切换窗口同时接受旧、新版本。
type Config struct {
	// KeyVersion 是当前 Active 密钥的唯一版本标识，禁止复用旧版本。
	KeyVersion string
	// Secret 是独立于 Cookie、CSRF 和 Session RPC 的 32 字节 HMAC 密钥。
	Secret []byte
	// TTL 是一次性断言有效期，不得超过协议 60 秒硬上限。
	TTL time.Duration
}

// Identity 是签发 Canonical 所需的全部权威内部身份事实。
type Identity struct {
	// RequestID 是本次 BFF 请求生成的规范小写 UUIDv7。
	RequestID string
	// CanonicalTarget 是固定 allowlist 路径以及规范化后的可选 Cursor Query。
	CanonicalTarget string
	// PrincipalUserID 是 Business Auth Resolver 确认的用户 UUIDv7。
	PrincipalUserID string
	// WebSessionID 是当前权威浏览器会话 UUIDv7。
	WebSessionID string
	// WebSessionVersion 是解析完成后的权威会话版本。
	WebSessionVersion int64
	// ProjectID 是 Business 所有权 JOIN 确认的 Project UUIDv7。
	ProjectID string
	// AgentSessionID 是 Business ready Binding 确认的 Agent Session UUIDv7。
	AgentSessionID string
	// Scope 是与 CanonicalTarget 严格对应的最小读取权限。
	Scope string
}

// Assertion 是可直接写入新建内部 HTTP Request 的三个 Header 值。
type Assertion struct {
	// EncodedCanonical 是 Canonical UTF-8 的 Base64URL 无补位编码。
	EncodedCanonical string
	// KeyVersion 是签名使用的 Active kid。
	KeyVersion string
	// Signature 是 Canonical 的小写十六进制 HMAC-SHA256。
	Signature string
}

// Signer 使用固定 16 行 Canonical 和注入随机源签发不可重放的短期断言。
type Signer struct {
	clock      Clock
	random     io.Reader
	keyVersion string
	secret     []byte
	ttl        time.Duration
}

// NewSigner 校验独立 HMAC 密钥、kid、TTL、时钟和随机源并复制密钥材料。
func NewSigner(clock Clock, random io.Reader, cfg Config) (*Signer, error) {
	if clock == nil || random == nil || !validKeyVersion(cfg.KeyVersion) || len(cfg.Secret) != sha256.Size ||
		cfg.TTL < time.Millisecond || cfg.TTL > maximumTTL || cfg.TTL%time.Millisecond != 0 {
		return nil, fmt.Errorf("create agent identity signer: invalid dependency or config")
	}
	return &Signer{
		clock: clock, random: random, keyVersion: cfg.KeyVersion,
		secret: append([]byte(nil), cfg.Secret...), ttl: cfg.TTL,
	}, nil
}

// Sign 校验路径与身份绑定，生成 16-byte Nonce，并返回固定三 Header 断言。
// 任一权威 ID、Scope 或路径不匹配都会在签名之前失败，避免产生可跨资源使用的凭据。
func (s *Signer) Sign(identity Identity) (Assertion, error) {
	if err := validateIdentity(identity); err != nil {
		return Assertion{}, err
	}
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(s.random, nonce); err != nil {
		return Assertion{}, ErrAssertionSigningUnavailable
	}
	now := s.clock.Now().UTC()
	if now.IsZero() || now.UnixMilli() <= 0 {
		return Assertion{}, ErrAssertionSigningUnavailable
	}
	iat := now.UnixMilli()
	exp := iat + s.ttl.Milliseconds()
	canonical := strings.Join([]string{
		assertionSchema,
		assertionIssuer,
		assertionAudience,
		s.keyVersion,
		identity.RequestID,
		"GET",
		identity.CanonicalTarget,
		identity.PrincipalUserID,
		identity.WebSessionID,
		strconv.FormatInt(identity.WebSessionVersion, 10),
		identity.ProjectID,
		identity.AgentSessionID,
		identity.Scope,
		strconv.FormatInt(iat, 10),
		strconv.FormatInt(exp, 10),
		base64.RawURLEncoding.EncodeToString(nonce),
	}, "\n")
	if len(canonical) > 2048 {
		return Assertion{}, ErrInvalidAssertionInput
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(canonical))
	return Assertion{
		EncodedCanonical: base64.RawURLEncoding.EncodeToString([]byte(canonical)),
		KeyVersion:       s.keyVersion,
		Signature:        hex.EncodeToString(mac.Sum(nil)),
	}, nil
}

// validateIdentity 确保所有 UUID、版本、Scope 与 CanonicalTarget 使用唯一规范编码。
func validateIdentity(identity Identity) error {
	if !canonicalUUIDv7(identity.RequestID) || !canonicalUUIDv7(identity.PrincipalUserID) ||
		!canonicalUUIDv7(identity.WebSessionID) || !canonicalUUIDv7(identity.ProjectID) ||
		!canonicalUUIDv7(identity.AgentSessionID) || identity.WebSessionVersion < 1 {
		return ErrInvalidAssertionInput
	}
	workspaceTarget := "/api/v1/agent/sessions/" + identity.AgentSessionID + "/workspace"
	eventsPrefix := "/api/v1/agent/sessions/" + identity.AgentSessionID + "/events?after_seq="
	toolsTarget := "/api/v1/agent/sessions/" + identity.AgentSessionID + "/tools"
	switch identity.Scope {
	case ScopeWorkspaceRead:
		if identity.CanonicalTarget != workspaceTarget {
			return ErrInvalidAssertionInput
		}
	case ScopeEventsRead:
		cursor := strings.TrimPrefix(identity.CanonicalTarget, eventsPrefix)
		if cursor == identity.CanonicalTarget || !canonicalNonNegativeInt(cursor) {
			return ErrInvalidAssertionInput
		}
	case ScopeToolsRead:
		if identity.CanonicalTarget != toolsTarget {
			return ErrInvalidAssertionInput
		}
	default:
		return ErrInvalidAssertionInput
	}
	return nil
}

// canonicalUUIDv7 校验 UUIDv7 已使用小写连字符标准形式，防止同一值出现多种签名编码。
func canonicalUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

// canonicalNonNegativeInt 校验零或无前导零正整数的唯一十进制编码。
func canonicalNonNegativeInt(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed <= maximumJavaScriptSafeInteger
}

// validKeyVersion 与 Agent Verifier 共用唯一语义：首字节为小写字母或数字，后续才允许 ._-。
func validKeyVersion(value string) bool {
	if value == "" || len(value) > 64 {
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
