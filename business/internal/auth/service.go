package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

const opaqueTokenBytes = 32

var (
	// ErrInvalidCredentials 表示邮箱、密码、账户状态或验证状态不允许登录，对外必须统一语义。
	ErrInvalidCredentials = errors.New("invalid authentication credentials")
	// ErrUnauthenticated 表示请求未携带可构造可信 Principal 的有效会话。
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrInvalidCSRF 表示会话绑定的 CSRF Token 缺失或校验失败。
	ErrInvalidCSRF = errors.New("invalid csrf token")
	// ErrInvalidLoginInput 表示登录邮箱或密码超出 W0 协议边界。
	ErrInvalidLoginInput = errors.New("invalid login input")
	// ErrRateLimited 表示同一规范化身份在版本化窗口内超过登录尝试上限。
	ErrRateLimited = errors.New("authentication rate limited")
	// ErrUnavailable 表示认证依赖、随机源或持久化不可用，不附带敏感底层原错。
	ErrUnavailable = errors.New("authentication unavailable")
)

// UserRepository 定义登录用例所需的单次认证事实查询，不暴露 GORM 或持久化模型。
type UserRepository interface {
	// FindAuthenticationRecord 按规范化邮箱读取 active 账户、已验证身份与 Argon2id 凭据。
	FindAuthenticationRecord(ctx context.Context, identityType user.IdentityType, normalizedIdentifier string) (user.AuthenticationRecord, error)
}

// Clock 为会话期限计算提供可注入 UTC 时钟。
type Clock interface {
	// Now 返回当前时间，用例会立即转为 UTC 并在本次操作内冻结。
	Now() time.Time
}

// IDGenerator 为 Web Session 提供可注入 UUIDv7 生成能力。
type IDGenerator interface {
	// New 生成新 UUIDv7，失败时不得降级为可预测标识。
	New() (string, error)
}

// PasswordVerifier 隔离 Argon2id 计算，便于测试验证不存在用户的假路径。
type PasswordVerifier interface {
	// Verify 使用凭据自带参数执行 Argon2id 并常量时间比较；参数不安全时返回错误。
	Verify(password string, credential user.PasswordCredential) (bool, error)
}

// LoginRateLimiter 使用不可逆身份摘要限制登录尝试，不接收完整邮箱或密码。
type LoginRateLimiter interface {
	// Allow 原子增加当前窗口计数，并返回本次是否仍在上限内。
	Allow(ctx context.Context, subjectDigest Digest) (bool, error)
	// Reset 在密码确认成功后清除失败窗口，避免合法用户持续占用旧计数。
	Reset(ctx context.Context, subjectDigest Digest) error
}

// SessionConfig 保存版本化 Web Session 安全参数，避免有效期与 CSRF 密钥写死在用例。
type SessionConfig struct {
	// IdleTTL 会话自最近有效活动起的空闲有效期。
	IdleTTL time.Duration
	// AbsoluteTTL 会话从创建起的绝对有效期。
	AbsoluteTTL time.Duration
	// CSRFSecret 用于从不透明 Cookie Token 派生会话绑定 CSRF Token，必须由安全配置注入。
	CSRFSecret []byte
	// MaxConcurrentSessions 是单个用户同时有效的浏览器会话上限。
	MaxConcurrentSessions int
}

// LoginResult 登录成功结果，原始 Token 只能短暂交给 HTTP Handler，不得记录或持久化。
type LoginResult struct {
	// Principal 已验证用户的安全前端投影。
	Principal Principal
	// CookieToken 256-bit 不透明会话 Token，只写入 HttpOnly Cookie。
	CookieToken string
	// CSRFToken 由会话 Token 与服务端密钥派生的内存态 Token。
	CSRFToken string
	// SessionExpiresAt 当前会话窗口的最早过期时间。
	SessionExpiresAt time.Time
}

// ResolvedSession 是有效 Cookie 经权威持久化校验后的可信会话投影。
type ResolvedSession struct {
	// Principal 可写入私有 Context 的可信用户身份。
	Principal Principal
	// WebSessionID 是 Business 权威 Web Session 标识，只供内部 BFF 身份断言使用，不得进入前端 DTO。
	WebSessionID string
	// WebSessionVersion 是完成本次解析后的权威会话版本，只供内部身份断言绑定撤销语义。
	WebSessionVersion int64
	// CSRFToken 供前端仅在内存保留的会话绑定 Token。
	CSRFToken string
	// SessionExpiresAt 当前会话窗口的最早过期时间。
	SessionExpiresAt time.Time
}

// Service 实现 W0 密码登录、会话解析与幂等退出，并统一防止账号枚举。
type Service struct {
	users      UserRepository
	sessions   Repository
	authorizer authorization.Resolver
	clock      Clock
	ids        IDGenerator
	random     io.Reader
	verifier   PasswordVerifier
	limiter    LoginRateLimiter
	config     SessionConfig
	fake       user.PasswordCredential
}

// NewService 校验依赖与会话安全参数并创建用例；任一依赖缺失都会阻止 Runtime 启动。
func NewService(users UserRepository, sessions Repository, authorizer authorization.Resolver, clock Clock, ids IDGenerator, random io.Reader, verifier PasswordVerifier, limiter LoginRateLimiter, cfg SessionConfig) (*Service, error) {
	if users == nil || sessions == nil || authorizer == nil || clock == nil || ids == nil || random == nil || verifier == nil || limiter == nil {
		return nil, fmt.Errorf("create auth service: dependency is nil")
	}
	if cfg.IdleTTL <= 0 || cfg.AbsoluteTTL <= 0 || cfg.IdleTTL > cfg.AbsoluteTTL || len(cfg.CSRFSecret) < 32 || cfg.MaxConcurrentSessions <= 0 {
		return nil, fmt.Errorf("create auth service: invalid session security config")
	}
	return &Service{
		users: users, sessions: sessions, authorizer: authorizer, clock: clock, ids: ids, random: random, verifier: verifier, limiter: limiter,
		config: SessionConfig{
			IdleTTL: cfg.IdleTTL, AbsoluteTTL: cfg.AbsoluteTTL,
			CSRFSecret: append([]byte(nil), cfg.CSRFSecret...), MaxConcurrentSessions: cfg.MaxConcurrentSessions,
		},
		fake: fakeCredential(),
	}, nil
}

// Login 规范化邮箱、执行统一 Argon2id 校验路径并建立新会话；不存在、密码错误和账户不可用统一返回 ErrInvalidCredentials。
func (s *Service) Login(ctx context.Context, email string, password string) (LoginResult, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil || password == "" || len(password) > 1024 {
		return LoginResult{}, ErrInvalidLoginInput
	}
	subjectDigest := Digest(sha256.Sum256([]byte(normalizedEmail)))
	allowed, err := s.limiter.Allow(ctx, subjectDigest)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrUnavailable
	}
	if !allowed {
		return LoginResult{}, ErrRateLimited
	}

	record, findErr := s.users.FindAuthenticationRecord(ctx, user.IdentityTypeEmail, normalizedEmail)
	if findErr != nil {
		if errors.Is(findErr, context.Canceled) || errors.Is(findErr, context.DeadlineExceeded) {
			return LoginResult{}, findErr
		}
		if !errors.Is(findErr, user.ErrUserNotFound) {
			return LoginResult{}, ErrUnavailable
		}
		// 用户不存在仍执行与真实凭据同类的 Argon2id 路径，降低时序侧信道导致的账号枚举风险。
		if _, verifyErr := s.verifier.Verify(password, s.fake); verifyErr != nil {
			return LoginResult{}, ErrUnavailable
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	if record.Account.ID == "" || record.Account.ID != record.Identity.UserID || record.Account.ID != record.Credential.UserID ||
		record.Account.Status != user.StatusActive || !record.Identity.Verified || record.Identity.NormalizedIdentifier != normalizedEmail {
		return LoginResult{}, ErrUnavailable
	}

	matched, err := s.verifier.Verify(password, record.Credential)
	if err != nil {
		return LoginResult{}, ErrUnavailable
	}
	if !matched {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := s.limiter.Reset(ctx, subjectDigest); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrUnavailable
	}
	authorizationProjection, err := s.authorizer.Resolve(ctx, record.Account.ID)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return LoginResult{}, err
		case errors.Is(err, authorization.ErrSubjectInactive):
			// 密码验证与角色解析之间账户可能失效；登录仍使用统一凭据失败语义，避免枚举账户状态。
			return LoginResult{}, ErrInvalidCredentials
		default:
			return LoginResult{}, ErrUnavailable
		}
	}

	now := s.clock.Now().UTC()
	sessionID, err := s.ids.New()
	if err != nil {
		return LoginResult{}, ErrUnavailable
	}
	cookieToken, err := generateOpaqueToken(s.random)
	if err != nil {
		return LoginResult{}, ErrUnavailable
	}
	csrfToken := deriveCSRFToken(s.config.CSRFSecret, cookieToken)
	session := WebSession{
		ID: sessionID, UserID: record.Account.ID, TokenDigest: digestToken(cookieToken), CSRFTokenDigest: digestToken(csrfToken),
		Status: SessionStatusActive, SessionVersion: 1, LastSeenAt: now, IdleExpiresAt: now.Add(s.config.IdleTTL),
		AbsoluteExpiresAt: now.Add(s.config.AbsoluteTTL), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.sessions.Create(ctx, session, s.config.MaxConcurrentSessions); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrUnavailable
	}
	return LoginResult{
		Principal: principalFromAuthenticationRecord(record, authorizationProjection), CookieToken: cookieToken, CSRFToken: csrfToken,
		SessionExpiresAt: earliestExpiry(session),
	}, nil
}

// Resolve 校验不透明 Cookie、会话状态、双重期限、账户状态和 CSRF 绑定摘要；成功后才返回可信 Principal。
func (s *Service) Resolve(ctx context.Context, cookieToken string) (ResolvedSession, error) {
	tokenDigest, ok := parseOpaqueTokenDigest(cookieToken)
	if !ok {
		return ResolvedSession{}, ErrUnauthenticated
	}
	identity, err := s.sessions.FindByTokenDigest(ctx, tokenDigest)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ResolvedSession{}, err
		}
		if errors.Is(err, ErrWebSessionNotFound) {
			return ResolvedSession{}, ErrUnauthenticated
		}
		return ResolvedSession{}, ErrUnavailable
	}
	if identity.Session.TokenDigest != tokenDigest || identity.Session.UserID != identity.UserID {
		return ResolvedSession{}, ErrUnavailable
	}
	now := s.clock.Now().UTC()
	if identity.Session.Status != SessionStatusActive || identity.AccountStatus != string(user.StatusActive) ||
		identity.DisplayName == "" || identity.NormalizedEmail == "" ||
		!now.Before(identity.Session.IdleExpiresAt) || !now.Before(identity.Session.AbsoluteExpiresAt) {
		return ResolvedSession{}, ErrUnauthenticated
	}
	csrfToken := deriveCSRFToken(s.config.CSRFSecret, cookieToken)
	csrfDigest := digestToken(csrfToken)
	if subtle.ConstantTimeCompare(identity.Session.CSRFTokenDigest[:], csrfDigest[:]) != 1 {
		return ResolvedSession{}, ErrUnauthenticated
	}
	// 每次有效受保护访问都按 CAS 滑动空闲期限，但绝不越过登录时冻结的绝对上限。
	refreshedIdleExpiresAt := now.Add(s.config.IdleTTL)
	if refreshedIdleExpiresAt.After(identity.Session.AbsoluteExpiresAt) {
		refreshedIdleExpiresAt = identity.Session.AbsoluteExpiresAt
	}
	if refreshedIdleExpiresAt.After(identity.Session.IdleExpiresAt) {
		err := s.sessions.Touch(ctx, identity.Session.ID, identity.Session.SessionVersion, now, refreshedIdleExpiresAt)
		switch {
		case err == nil:
			identity.Session.LastSeenAt = now
			identity.Session.IdleExpiresAt = refreshedIdleExpiresAt
			identity.Session.UpdatedAt = now
			identity.Session.SessionVersion++
		case errors.Is(err, ErrWebSessionVersionConflict):
			// 并发请求或撤销可能先更新版本；重新读取并完整校验，禁止容忍撤销竞态。
			identity, err = s.sessions.FindByTokenDigest(ctx, tokenDigest)
			if err != nil {
				switch {
				case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
					return ResolvedSession{}, err
				case errors.Is(err, ErrWebSessionNotFound):
					return ResolvedSession{}, ErrUnauthenticated
				default:
					return ResolvedSession{}, ErrUnavailable
				}
			}
			if !resolvedIdentityValid(identity, tokenDigest, now) {
				return ResolvedSession{}, ErrUnauthenticated
			}
		default:
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ResolvedSession{}, err
			}
			return ResolvedSession{}, ErrUnavailable
		}
	}
	authorizationProjection, err := s.authorizer.Resolve(ctx, identity.UserID)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return ResolvedSession{}, err
		case errors.Is(err, authorization.ErrSubjectInactive):
			return ResolvedSession{}, ErrUnauthenticated
		default:
			return ResolvedSession{}, ErrUnavailable
		}
	}
	return ResolvedSession{
		Principal: principalFromSessionIdentity(identity, authorizationProjection), WebSessionID: identity.Session.ID,
		WebSessionVersion: identity.Session.SessionVersion, CSRFToken: csrfToken,
		SessionExpiresAt: earliestExpiry(identity.Session),
	}, nil
}

// resolvedIdentityValid 对 CAS 冲突后的新快照执行与首次 Resolve 相同的权威安全校验。
func resolvedIdentityValid(identity SessionIdentity, tokenDigest Digest, now time.Time) bool {
	return identity.Session.TokenDigest == tokenDigest && identity.Session.UserID == identity.UserID &&
		identity.Session.Status == SessionStatusActive && identity.AccountStatus == string(user.StatusActive) &&
		identity.DisplayName != "" && identity.NormalizedEmail != "" &&
		now.Before(identity.Session.IdleExpiresAt) && now.Before(identity.Session.AbsoluteExpiresAt)
}

// Logout 在 Cookie 存在时先校验会话绑定 CSRF 再撤销；会话已撤销、不存在或 Cookie 已清理均幂等成功。
func (s *Service) Logout(ctx context.Context, cookieToken string, csrfToken string) error {
	if strings.TrimSpace(cookieToken) == "" {
		return nil
	}
	tokenDigest, ok := parseOpaqueTokenDigest(cookieToken)
	if !ok {
		return nil
	}
	identity, err := s.sessions.FindByTokenDigest(ctx, tokenDigest)
	if err != nil {
		if errors.Is(err, ErrWebSessionNotFound) {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return ErrUnavailable
	}
	if identity.Session.TokenDigest != tokenDigest || identity.Session.UserID != identity.UserID {
		return ErrUnavailable
	}
	expectedCSRF := deriveCSRFToken(s.config.CSRFSecret, cookieToken)
	presentedDigest := digestToken(csrfToken)
	expectedDigest := digestToken(expectedCSRF)
	if csrfToken == "" || subtle.ConstantTimeCompare([]byte(csrfToken), []byte(expectedCSRF)) != 1 ||
		subtle.ConstantTimeCompare(identity.Session.CSRFTokenDigest[:], expectedDigest[:]) != 1 ||
		subtle.ConstantTimeCompare(identity.Session.CSRFTokenDigest[:], presentedDigest[:]) != 1 {
		return ErrInvalidCSRF
	}
	if identity.Session.Status == SessionStatusRevoked || identity.Session.Status == SessionStatusExpired {
		return nil
	}
	now := s.clock.Now().UTC()
	if err := s.sessions.Revoke(ctx, identity.Session.ID, identity.Session.SessionVersion, now, "user_logout"); err != nil {
		if errors.Is(err, ErrWebSessionNotFound) {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return ErrUnavailable
	}
	return nil
}

// NormalizeEmail 对登录邮箱执行 Unicode NFC、去除首尾空白、语法校验与小写规范化；不接受带展示名的地址。
func NormalizeEmail(raw string) (string, error) {
	value := norm.NFC.String(strings.TrimSpace(raw))
	if value == "" || len(value) > 320 || strings.ContainsAny(value, "\r\n\x00") {
		return "", ErrInvalidLoginInput
	}
	for _, char := range value {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return "", ErrInvalidLoginInput
		}
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value || strings.Count(value, "@") != 1 {
		return "", ErrInvalidLoginInput
	}
	parts := strings.SplitN(value, "@", 2)
	if parts[0] == "" || parts[1] == "" {
		return "", ErrInvalidLoginInput
	}
	return strings.ToLower(value), nil
}

// Argon2idVerifier 使用 x/crypto 执行有资源上限的 Argon2id 密码校验。
type Argon2idVerifier struct{}

// Verify 重算凭据指定长度的 Argon2id 摘要并常量时间比较；超出上限的数据库参数失败关闭。
func (Argon2idVerifier) Verify(password string, credential user.PasswordCredential) (bool, error) {
	if credential.Algorithm != "argon2id" || credential.MemoryKiB < 8 || credential.MemoryKiB > 262144 ||
		credential.Iterations < 1 || credential.Iterations > 10 || credential.Parallelism < 1 || credential.Parallelism > 16 ||
		len(credential.Salt) < 16 || len(credential.Salt) > 64 || len(credential.PasswordHash) < 16 || len(credential.PasswordHash) > 64 {
		return false, ErrUnavailable
	}
	candidate := argon2.IDKey([]byte(password), credential.Salt, uint32(credential.Iterations), uint32(credential.MemoryKiB), uint8(credential.Parallelism), uint32(len(credential.PasswordHash)))
	return subtle.ConstantTimeCompare(candidate, credential.PasswordHash) == 1, nil
}

// generateOpaqueToken 从可注入随机源读取完整 256 bit 并使用无补位 Base64URL 编码。
func generateOpaqueToken(random io.Reader) (string, error) {
	bytes := make([]byte, opaqueTokenBytes)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", fmt.Errorf("generate opaque auth token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// parseOpaqueTokenDigest 仅接受规范 Base64URL 编码的 256-bit Token，成功时返回 SHA-256 摘要。
func parseOpaqueTokenDigest(token string) (Digest, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != opaqueTokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return Digest{}, false
	}
	return digestToken(token), true
}

// deriveCSRFToken 用 HMAC-SHA-256 将 CSRF Token 绑定到当前会话 Cookie，使 GET 会话可重建内存态 Token。
func deriveCSRFToken(secret []byte, cookieToken string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("dora.auth.csrf.v1\x00"))
	_, _ = mac.Write([]byte(cookieToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// digestToken 计算原始令牌文本的 SHA-256 摘要，原值不会进入持久化模型。
func digestToken(token string) Digest {
	return sha256.Sum256([]byte(token))
}

// principalFromAuthenticationRecord 从已完成密码校验与动态授权解析的事实构造不含凭据的前端投影。
func principalFromAuthenticationRecord(record user.AuthenticationRecord, projection authorization.Projection) Principal {
	return Principal{
		ID: record.Account.ID, DisplayName: record.Account.DisplayName, Email: maskEmail(record.Identity.NormalizedIdentifier),
		AccountStatus: string(record.Account.Status), Roles: append([]string{}, projection.Roles...),
		Capabilities: append([]string{}, projection.Capabilities...),
	}
}

// principalFromSessionIdentity 从会话 JOIN 与本次动态授权结果构造不含密码凭据的可信投影。
func principalFromSessionIdentity(identity SessionIdentity, projection authorization.Projection) Principal {
	return Principal{
		ID: identity.UserID, DisplayName: identity.DisplayName, Email: maskEmail(identity.NormalizedEmail),
		AccountStatus: identity.AccountStatus, Roles: append([]string{}, projection.Roles...),
		Capabilities: append([]string{}, projection.Capabilities...),
	}
}

// earliestExpiry 返回空闲与绝对期限中较早者，作为前端会话投影。
func earliestExpiry(session WebSession) time.Time {
	if session.IdleExpiresAt.Before(session.AbsoluteExpiresAt) {
		return session.IdleExpiresAt
	}
	return session.AbsoluteExpiresAt
}

// maskEmail 仅保留邮箱本地部分首个 Unicode 字符与域名，避免完整地址进入普通 DTO。
func maskEmail(email string) string {
	separator := strings.LastIndexByte(email, '@')
	if separator <= 0 || separator == len(email)-1 {
		return "***"
	}
	first, _ := utf8.DecodeRuneInString(email[:separator])
	if first == utf8.RuneError {
		return "***@" + email[separator+1:]
	}
	return string(first) + "***@" + email[separator+1:]
}

// fakeCredential 构造不存在用户的固定 Argon2id 工作负载，结果永远不作为登录成功依据。
func fakeCredential() user.PasswordCredential {
	now := time.Unix(1, 0).UTC()
	return user.PasswordCredential{
		ID: "019f0000-0000-7000-8000-000000000001", UserID: "019f0000-0000-7000-8000-000000000002", Algorithm: "argon2id",
		MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, Salt: []byte("dora-auth-fake-salt-v1"),
		PasswordHash: []byte("dora-auth-fake-hash-value-32byte"), CredentialVersion: 1,
		PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
}
