package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	"golang.org/x/crypto/argon2"
)

func TestRepositoryAuthPostgreSQLW0Semantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	client := &Client{db: db}
	userRepository, err := NewUserRepository(client)
	if err != nil {
		t.Fatalf("create integration user repository: %v", err)
	}
	authRepository, err := NewAuthRepository(client)
	if err != nil {
		t.Fatalf("create integration auth repository: %v", err)
	}

	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	verifiedAt := now
	salt := []byte("1234567890123456")
	record := user.AuthenticationRecord{
		Account: user.Account{
			ID: "019f0000-0000-7000-8000-000000000011", DisplayName: "集成测试用户", Type: user.TypePersonal,
			Status: user.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Identity: user.LoginIdentity{
			ID: "019f0000-0000-7000-8000-000000000012", UserID: "019f0000-0000-7000-8000-000000000011",
			Type: user.IdentityTypeEmail, NormalizedIdentifier: "integration@example.com", Verified: true, VerifiedAt: &verifiedAt,
			CreatedAt: now, UpdatedAt: now,
		},
		Credential: user.PasswordCredential{
			ID: "019f0000-0000-7000-8000-000000000013", UserID: "019f0000-0000-7000-8000-000000000011",
			Algorithm: "argon2id", MemoryKiB: 64, Iterations: 1, Parallelism: 1, Salt: salt,
			PasswordHash: argon2.IDKey([]byte("integration-password"), salt, 1, 64, 1, 32), CredentialVersion: 1,
			PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := userRepository.CreateAuthenticationRecord(context.Background(), record); err != nil {
		t.Fatalf("create PostgreSQL authentication record: %v", err)
	}
	readRecord, err := userRepository.FindAuthenticationRecord(context.Background(), user.IdentityTypeEmail, record.Identity.NormalizedIdentifier)
	if err != nil {
		t.Fatalf("read PostgreSQL authentication record: %v", err)
	}
	if readRecord.Account.ID != record.Account.ID || readRecord.Account.DisplayName != record.Account.DisplayName ||
		string(readRecord.Credential.PasswordHash) != string(record.Credential.PasswordHash) {
		t.Fatalf("PostgreSQL authentication JOIN changed facts: %+v", readRecord.Account)
	}

	// Repository 仅接收并保存原始 Cookie/CSRF 的 SHA-256 摘要，数据库表不具备原文字段。
	tokenDigest := sha256.Sum256([]byte("integration-cookie-token"))
	csrfDigest := sha256.Sum256([]byte("integration-csrf-token"))
	session := auth.WebSession{
		ID: "019f0000-0000-7000-8000-000000000021", UserID: record.Account.ID,
		TokenDigest: tokenDigest, CSRFTokenDigest: csrfDigest, Status: auth.SessionStatusActive, SessionVersion: 1,
		LastSeenAt: now, IdleExpiresAt: now.Add(30 * time.Minute), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := authRepository.Create(context.Background(), session, 5); err != nil {
		t.Fatalf("create PostgreSQL web session: %v", err)
	}
	identity, err := authRepository.FindByTokenDigest(context.Background(), tokenDigest)
	if err != nil {
		t.Fatalf("resolve PostgreSQL web session: %v", err)
	}
	if identity.UserID != record.Account.ID || identity.DisplayName != record.Account.DisplayName || identity.NormalizedEmail != record.Identity.NormalizedIdentifier {
		t.Fatalf("PostgreSQL session resolver changed safe identity: %+v", identity)
	}
	touchedAt := now.Add(time.Minute)
	touchedIdleExpiry := touchedAt.Add(30 * time.Minute)
	if err := authRepository.Touch(context.Background(), session.ID, identity.Session.SessionVersion, touchedAt, touchedIdleExpiry); err != nil {
		t.Fatalf("touch PostgreSQL web session: %v", err)
	}
	touchedIdentity, err := authRepository.FindByTokenDigest(context.Background(), tokenDigest)
	if err != nil || touchedIdentity.Session.SessionVersion != identity.Session.SessionVersion+1 || !touchedIdentity.Session.LastSeenAt.Equal(touchedAt) || !touchedIdentity.Session.IdleExpiresAt.Equal(touchedIdleExpiry) {
		t.Fatalf("unexpected PostgreSQL session touch projection: identity=%+v err=%v", touchedIdentity, err)
	}
	revokedAt := now.Add(2 * time.Minute)
	if err := authRepository.Revoke(context.Background(), session.ID, touchedIdentity.Session.SessionVersion, revokedAt, "user_logout"); err != nil {
		t.Fatalf("revoke PostgreSQL web session: %v", err)
	}
	revokedIdentity, err := authRepository.FindByTokenDigest(context.Background(), tokenDigest)
	if err != nil {
		t.Fatalf("read revoked PostgreSQL web session: %v", err)
	}
	if revokedIdentity.Session.Status != auth.SessionStatusRevoked || revokedIdentity.Session.RevokedAt == nil || revokedIdentity.Session.SessionVersion != touchedIdentity.Session.SessionVersion+1 {
		t.Fatalf("unexpected PostgreSQL revoke projection: %+v", revokedIdentity.Session)
	}
	if err := authRepository.Revoke(context.Background(), session.ID, revokedIdentity.Session.SessionVersion, revokedAt.Add(time.Second), "user_logout"); err != nil {
		t.Fatalf("repeat PostgreSQL revoke should be idempotent: %v", err)
	}

	// 同一用户的新登录在同一事务内撤销最旧超额会话；max=1 时第二个新会话必须替换第一个。
	firstLimited := auth.WebSession{
		ID: "019f0000-0000-7000-8000-000000000031", UserID: record.Account.ID,
		TokenDigest: sha256.Sum256([]byte("first-limited-cookie")), CSRFTokenDigest: sha256.Sum256([]byte("first-limited-csrf")),
		Status: auth.SessionStatusActive, SessionVersion: 1, LastSeenAt: now.Add(3 * time.Minute),
		IdleExpiresAt: now.Add(33 * time.Minute), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(3 * time.Minute), UpdatedAt: now.Add(3 * time.Minute),
	}
	if err := authRepository.Create(context.Background(), firstLimited, 1); err != nil {
		t.Fatalf("create first concurrent-policy session: %v", err)
	}
	secondLimited := auth.WebSession{
		ID: "019f0000-0000-7000-8000-000000000032", UserID: record.Account.ID,
		TokenDigest: sha256.Sum256([]byte("second-limited-cookie")), CSRFTokenDigest: sha256.Sum256([]byte("second-limited-csrf")),
		Status: auth.SessionStatusActive, SessionVersion: 1, LastSeenAt: now.Add(4 * time.Minute),
		IdleExpiresAt: now.Add(34 * time.Minute), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(4 * time.Minute), UpdatedAt: now.Add(4 * time.Minute),
	}
	if err := authRepository.Create(context.Background(), secondLimited, 1); err != nil {
		t.Fatalf("create second concurrent-policy session: %v", err)
	}
	firstLimitedIdentity, err := authRepository.FindByTokenDigest(context.Background(), firstLimited.TokenDigest)
	if err != nil || firstLimitedIdentity.Session.Status != auth.SessionStatusRevoked ||
		firstLimitedIdentity.Session.RevokeReason == nil || *firstLimitedIdentity.Session.RevokeReason != "concurrent_session_limit" {
		t.Fatalf("oldest concurrent session was not revoked: identity=%+v err=%v", firstLimitedIdentity, err)
	}
	secondLimitedIdentity, err := authRepository.FindByTokenDigest(context.Background(), secondLimited.TokenDigest)
	if err != nil || secondLimitedIdentity.Session.Status != auth.SessionStatusActive {
		t.Fatalf("newest concurrent session is not active: identity=%+v err=%v", secondLimitedIdentity, err)
	}

	if err := db.Exec("UPDATE business.user_account SET status = 'disabled', updated_at = ? WHERE id = ?", now.Add(2*time.Minute), record.Account.ID).Error; err != nil {
		t.Fatalf("disable integration account: %v", err)
	}
	if _, err := userRepository.FindAuthenticationRecord(context.Background(), user.IdentityTypeEmail, record.Identity.NormalizedIdentifier); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("disabled account remained login-visible: %v", err)
	}
}
