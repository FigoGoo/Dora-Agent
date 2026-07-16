package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// authTestUUIDv7 生成安全会话测试使用的 UUIDv7。
func authTestUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate auth test UUIDv7: %v", err)
	}
	return id.String()
}

// validAuthTestSession 构造满足 W0 期限和状态不变量的安全会话。
func validAuthTestSession(t *testing.T) WebSession {
	t.Helper()
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	return WebSession{
		ID: authTestUUIDv7(t), UserID: authTestUUIDv7(t), TokenDigest: Digest{1}, CSRFTokenDigest: Digest{2},
		Status: SessionStatusActive, SessionVersion: 1, LastSeenAt: now,
		IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}
}

func TestWebSessionRejectsZeroTokenDigests(t *testing.T) {
	session := validAuthTestSession(t)
	session.TokenDigest = Digest{}
	if err := session.Validate(); !errors.Is(err, ErrInvalidWebSession) {
		t.Fatalf("expected zero session token digest rejection, got %v", err)
	}
	session = validAuthTestSession(t)
	session.CSRFTokenDigest = Digest{}
	if err := session.Validate(); !errors.Is(err, ErrInvalidWebSession) {
		t.Fatalf("expected zero CSRF token digest rejection, got %v", err)
	}
}

func TestWebSessionAcceptsNonZeroTokenDigests(t *testing.T) {
	if err := validAuthTestSession(t).Validate(); err != nil {
		t.Fatalf("validate active web session: %v", err)
	}
}
