package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	"golang.org/x/crypto/argon2"
)

// authServiceTestClock 为会话用例测试固定 UTC 时间。
type authServiceTestClock struct{ now time.Time }

// Now 返回测试冻结时间。
func (c authServiceTestClock) Now() time.Time { return c.now }

// authServiceTestIDs 为会话用例测试返回固定 UUIDv7。
type authServiceTestIDs struct {
	id  string
	err error
}

// New 返回测试标识或注入错误。
func (g authServiceTestIDs) New() (string, error) { return g.id, g.err }

// authServiceTestUsers 为登录用例注入认证记录或稳定错误。
type authServiceTestUsers struct {
	record user.AuthenticationRecord
	err    error
	email  string
}

// FindAuthenticationRecord 记录规范化邮箱并返回测试事实。
func (r *authServiceTestUsers) FindAuthenticationRecord(_ context.Context, _ user.IdentityType, email string) (user.AuthenticationRecord, error) {
	r.email = email
	return r.record, r.err
}

// authServiceTestSessions 在内存中捕获会话创建、解析和撤销调用。
type authServiceTestSessions struct {
	created       WebSession
	identity      SessionIdentity
	findErr       error
	createErr     error
	revokeErr     error
	touchErr      error
	revokeCalls   int
	touchCalls    int
	touchedAt     time.Time
	touchedExpiry time.Time
	revokedID     string
	revokedReason string
	maxConcurrent int
}

// Create 捕获新会话摘要，不接收原始 Token。
func (r *authServiceTestSessions) Create(_ context.Context, session WebSession, maxConcurrentSessions int) error {
	r.created = session
	r.maxConcurrent = maxConcurrentSessions
	return r.createErr
}

// FindByTokenDigest 返回预置的会话身份。
func (r *authServiceTestSessions) FindByTokenDigest(_ context.Context, _ Digest) (SessionIdentity, error) {
	return r.identity, r.findErr
}

// Touch 捕获滑动会话空闲期限的 CAS 写入。
func (r *authServiceTestSessions) Touch(_ context.Context, _ string, _ int64, lastSeenAt time.Time, idleExpiresAt time.Time) error {
	r.touchCalls++
	r.touchedAt = lastSeenAt
	r.touchedExpiry = idleExpiresAt
	return r.touchErr
}

// Revoke 捕获撤销事实并返回注入错误。
func (r *authServiceTestSessions) Revoke(_ context.Context, sessionID string, _ int64, _ time.Time, reason string) error {
	r.revokeCalls++
	r.revokedID = sessionID
	r.revokedReason = reason
	return r.revokeErr
}

// authServiceTestVerifier 为登录路径记录 Argon2id 校验次数与凭据。
type authServiceTestVerifier struct {
	matched      bool
	err          error
	calls        int
	credential   user.PasswordCredential
	lastPassword string
}

// authServiceTestLimiter 捕获不可逆身份摘要限流和成功重置。
type authServiceTestLimiter struct {
	allowed    bool
	allowErr   error
	resetErr   error
	allowCalls int
	resetCalls int
	digest     Digest
}

// Allow 返回预置限流判断。
func (limiter *authServiceTestLimiter) Allow(_ context.Context, digest Digest) (bool, error) {
	limiter.allowCalls++
	limiter.digest = digest
	return limiter.allowed, limiter.allowErr
}

// Reset 捕获密码成功后的窗口清理。
func (limiter *authServiceTestLimiter) Reset(_ context.Context, _ Digest) error {
	limiter.resetCalls++
	return limiter.resetErr
}

// Verify 记录密码和凭据并返回注入结果。
func (v *authServiceTestVerifier) Verify(password string, credential user.PasswordCredential) (bool, error) {
	v.calls++
	v.credential = credential
	v.lastPassword = password
	return v.matched, v.err
}

// validAuthServiceRecord 构造满足认证领域不变量的 active 用户记录。
func validAuthServiceRecord(now time.Time) user.AuthenticationRecord {
	verifiedAt := now.Add(-time.Hour)
	return user.AuthenticationRecord{
		Account: user.Account{
			ID: "019f0000-0000-7000-8000-000000000011", DisplayName: "测试用户", Type: user.TypePersonal,
			Status: user.StatusActive, Version: 1, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
		Identity: user.LoginIdentity{
			ID: "019f0000-0000-7000-8000-000000000012", UserID: "019f0000-0000-7000-8000-000000000011",
			Type: user.IdentityTypeEmail, NormalizedIdentifier: "user@example.com", Verified: true, VerifiedAt: &verifiedAt,
			CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
		Credential: user.PasswordCredential{
			ID: "019f0000-0000-7000-8000-000000000013", UserID: "019f0000-0000-7000-8000-000000000011",
			Algorithm: "argon2id", MemoryKiB: 64, Iterations: 1, Parallelism: 1,
			Salt: []byte("1234567890123456"), PasswordHash: bytes.Repeat([]byte{1}, 32), CredentialVersion: 1,
			PasswordChangedAt: now.Add(-time.Hour), CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
	}
}

// newAuthServiceForTest 用可预测但仅存在于测试的随机字节创建会话用例。
func newAuthServiceForTest(t *testing.T, users *authServiceTestUsers, sessions *authServiceTestSessions, verifier *authServiceTestVerifier, now time.Time) *Service {
	t.Helper()
	service, err := NewService(
		users, sessions, authServiceTestClock{now: now}, authServiceTestIDs{id: "019f0000-0000-7000-8000-000000000021"},
		bytes.NewReader(bytes.Repeat([]byte{7}, opaqueTokenBytes)), verifier, &authServiceTestLimiter{allowed: true},
		SessionConfig{
			IdleTTL: 30 * time.Minute, AbsoluteTTL: 24 * time.Hour,
			CSRFSecret: bytes.Repeat([]byte{9}, 32), MaxConcurrentSessions: 5,
		},
	)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	return service
}

func TestLoginNormalizesEmailAndStoresOnlyDigests(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{matched: true}
	service := newAuthServiceForTest(t, users, sessions, verifier, now)

	result, err := service.Login(context.Background(), "  USER@Example.COM ", "correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if users.email != "user@example.com" || verifier.calls != 1 {
		t.Fatalf("unexpected normalization or verify calls: email=%q calls=%d", users.email, verifier.calls)
	}
	if len(result.CookieToken) != 43 || len(result.CSRFToken) != 43 || result.CookieToken == result.CSRFToken {
		t.Fatalf("unexpected issued token lengths or equality: cookie=%d csrf=%d", len(result.CookieToken), len(result.CSRFToken))
	}
	if sessions.created.TokenDigest != digestToken(result.CookieToken) || sessions.created.CSRFTokenDigest != digestToken(result.CSRFToken) {
		t.Fatal("repository did not receive token SHA-256 digests")
	}
	if sessions.created.IdleExpiresAt != now.Add(30*time.Minute) || sessions.created.AbsoluteExpiresAt != now.Add(24*time.Hour) {
		t.Fatalf("unexpected configured expiry: %+v", sessions.created)
	}
	if sessions.maxConcurrent != 5 {
		t.Fatalf("concurrent session policy was not passed to repository: %d", sessions.maxConcurrent)
	}
	if result.Principal.Email != "u***@example.com" || result.Principal.Roles == nil || result.Principal.Capabilities == nil {
		t.Fatalf("unexpected safe principal: %+v", result.Principal)
	}
}

func TestLoginRateLimitStopsBeforePasswordLookup(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{matched: true}
	limiter := &authServiceTestLimiter{allowed: false}
	service, err := NewService(
		users, sessions, authServiceTestClock{now: now}, authServiceTestIDs{id: "019f0000-0000-7000-8000-000000000021"},
		bytes.NewReader(bytes.Repeat([]byte{7}, opaqueTokenBytes)), verifier, limiter,
		SessionConfig{
			IdleTTL: 30 * time.Minute, AbsoluteTTL: 24 * time.Hour,
			CSRFSecret: bytes.Repeat([]byte{9}, 32), MaxConcurrentSessions: 5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "User@Example.com", "candidate"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Login() error = %v", err)
	}
	if limiter.allowCalls != 1 || verifier.calls != 0 || users.email != "" || limiter.digest == (Digest{}) {
		t.Fatalf("rate-limited login reached credential path: limiter=%+v verifier=%+v users=%+v", limiter, verifier, users)
	}
}

func TestLoginResetsRateWindowOnlyAfterPasswordMatch(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{matched: true}
	limiter := &authServiceTestLimiter{allowed: true}
	service, err := NewService(
		users, sessions, authServiceTestClock{now: now}, authServiceTestIDs{id: "019f0000-0000-7000-8000-000000000021"},
		bytes.NewReader(bytes.Repeat([]byte{7}, opaqueTokenBytes)), verifier, limiter,
		SessionConfig{
			IdleTTL: 30 * time.Minute, AbsoluteTTL: 24 * time.Hour,
			CSRFSecret: bytes.Repeat([]byte{9}, 32), MaxConcurrentSessions: 5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "user@example.com", "correct-password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if limiter.allowCalls != 1 || limiter.resetCalls != 1 {
		t.Fatalf("successful login did not reset one rate window: %+v", limiter)
	}

	verifier.matched = false
	if _, err := service.Login(context.Background(), "user@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong-password Login() error = %v", err)
	}
	if limiter.allowCalls != 2 || limiter.resetCalls != 1 {
		t.Fatalf("failed password unexpectedly reset rate window: %+v", limiter)
	}
}

func TestLoginUsesFakeArgonPathForMissingUser(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{err: user.ErrUserNotFound}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{}
	service := newAuthServiceForTest(t, users, sessions, verifier, now)

	_, err := service.Login(context.Background(), "missing@example.com", "candidate")
	if !errors.Is(err, ErrInvalidCredentials) || verifier.calls != 1 || verifier.credential.MemoryKiB != 64*1024 {
		t.Fatalf("missing user did not use uniform fake path: err=%v calls=%d credential=%+v", err, verifier.calls, verifier.credential)
	}
}

func TestResolveRebuildsSessionBoundCSRFAndRejectsExpiredAccount(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{matched: true}
	service := newAuthServiceForTest(t, users, sessions, verifier, now)
	login, err := service.Login(context.Background(), "user@example.com", "correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	sessions.identity = SessionIdentity{
		Session: sessions.created, UserID: sessions.created.UserID, DisplayName: "测试用户",
		NormalizedEmail: "user@example.com", AccountStatus: string(user.StatusActive),
	}
	resolved, err := service.Resolve(context.Background(), login.CookieToken)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.CSRFToken != login.CSRFToken || resolved.Principal.ID != sessions.created.UserID ||
		resolved.WebSessionID != sessions.created.ID || resolved.WebSessionVersion != sessions.created.SessionVersion {
		t.Fatalf("resolved session changed bound projection: %+v", resolved)
	}

	sessions.identity.AccountStatus = string(user.StatusDisabled)
	if _, err := service.Resolve(context.Background(), login.CookieToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected disabled account rejection, got %v", err)
	}
}

func TestResolveSlidesIdleExpiryWithoutCrossingAbsoluteLimit(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	service := newAuthServiceForTest(t, users, sessions, &authServiceTestVerifier{matched: true}, now)
	login, err := service.Login(context.Background(), "user@example.com", "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	sessions.created.IdleExpiresAt = now.Add(10 * time.Minute)
	sessions.identity = SessionIdentity{
		Session: sessions.created, UserID: sessions.created.UserID, DisplayName: "测试用户",
		NormalizedEmail: "user@example.com", AccountStatus: string(user.StatusActive),
	}
	resolved, err := service.Resolve(context.Background(), login.CookieToken)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if sessions.touchCalls != 1 || sessions.touchedAt != now || sessions.touchedExpiry != now.Add(30*time.Minute) || resolved.SessionExpiresAt != now.Add(30*time.Minute) {
		t.Fatalf("idle expiry was not slid safely: sessions=%+v resolved=%+v", sessions, resolved)
	}
}

func TestLogoutValidatesBoundCSRFAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	users := &authServiceTestUsers{record: validAuthServiceRecord(now)}
	sessions := &authServiceTestSessions{}
	verifier := &authServiceTestVerifier{matched: true}
	service := newAuthServiceForTest(t, users, sessions, verifier, now)
	login, err := service.Login(context.Background(), "user@example.com", "correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	sessions.identity = SessionIdentity{
		Session: sessions.created, UserID: sessions.created.UserID, DisplayName: "测试用户",
		NormalizedEmail: "user@example.com", AccountStatus: string(user.StatusActive),
	}
	if err := service.Logout(context.Background(), login.CookieToken, "wrong"); !errors.Is(err, ErrInvalidCSRF) || sessions.revokeCalls != 0 {
		t.Fatalf("invalid CSRF reached revoke: err=%v calls=%d", err, sessions.revokeCalls)
	}
	if err := service.Logout(context.Background(), login.CookieToken, login.CSRFToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if sessions.revokeCalls != 1 || sessions.revokedReason != "user_logout" {
		t.Fatalf("unexpected revoke calls: %+v", sessions)
	}
	sessions.identity.Session.Status = SessionStatusRevoked
	revokedAt := now
	reason := "user_logout"
	sessions.identity.Session.RevokedAt = &revokedAt
	sessions.identity.Session.RevokeReason = &reason
	if err := service.Logout(context.Background(), login.CookieToken, login.CSRFToken); err != nil || sessions.revokeCalls != 1 {
		t.Fatalf("repeated logout was not idempotent: err=%v calls=%d", err, sessions.revokeCalls)
	}
	sessions.identity.Session.Status = SessionStatusExpired
	sessions.identity.Session.RevokedAt = nil
	sessions.identity.Session.RevokeReason = nil
	if err := service.Logout(context.Background(), login.CookieToken, login.CSRFToken); err != nil || sessions.revokeCalls != 1 {
		t.Fatalf("expired-session logout was not idempotent: err=%v calls=%d", err, sessions.revokeCalls)
	}
}

func TestArgon2idVerifierMatchesAndRejectsUnsafeParameters(t *testing.T) {
	password := "correct horse battery staple"
	credential := validAuthServiceRecord(time.Now().UTC()).Credential
	credential.PasswordHash = argon2.IDKey([]byte(password), credential.Salt, uint32(credential.Iterations), uint32(credential.MemoryKiB), uint8(credential.Parallelism), 32)
	matched, err := (Argon2idVerifier{}).Verify(password, credential)
	if err != nil || !matched {
		t.Fatalf("verify correct Argon2id password: matched=%v err=%v", matched, err)
	}
	matched, err = (Argon2idVerifier{}).Verify("wrong", credential)
	if err != nil || matched {
		t.Fatalf("verify wrong Argon2id password: matched=%v err=%v", matched, err)
	}
	credential.MemoryKiB = 262145
	if _, err := (Argon2idVerifier{}).Verify(password, credential); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unsafe Argon2id parameter rejection, got %v", err)
	}
}

func TestNormalizeEmailRejectsDisplayNameAndControlCharacters(t *testing.T) {
	if normalized, err := NormalizeEmail(" User@Example.COM "); err != nil || normalized != "user@example.com" {
		t.Fatalf("normalize email: %q, %v", normalized, err)
	}
	for _, invalid := range []string{"Name <user@example.com>", "user @example.com", "user@example.com\r\nX-Test: value", ""} {
		if _, err := NormalizeEmail(invalid); !errors.Is(err, ErrInvalidLoginInput) {
			t.Fatalf("expected invalid email %q rejection, got %v", invalid, err)
		}
	}
}
