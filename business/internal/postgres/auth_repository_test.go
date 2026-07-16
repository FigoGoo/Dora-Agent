package postgres

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newAuthRepositoryTestDB 创建 sqlmock 驱动的 GORM Client，同时返回 Auth 与 User Repository。
func newAuthRepositoryTestDB(t *testing.T) (*AuthRepository, *UserRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create auth sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open auth gorm sqlmock: %v", err)
	}
	client := &Client{db: db}
	authRepository, err := NewAuthRepository(client)
	if err != nil {
		t.Fatalf("create auth repository: %v", err)
	}
	userRepository, err := NewUserRepository(client)
	if err != nil {
		t.Fatalf("create user repository: %v", err)
	}
	return authRepository, userRepository, mock
}

// validAuthRepositorySession 构造可持久化的 active Web Session。
func validAuthRepositorySession() auth.WebSession {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	return auth.WebSession{
		ID: "019f0000-0000-7000-8000-000000000021", UserID: "019f0000-0000-7000-8000-000000000011",
		TokenDigest: auth.Digest{1}, CSRFTokenDigest: auth.Digest{2}, Status: auth.SessionStatusActive, SessionVersion: 1,
		LastSeenAt: now, IdleExpiresAt: now.Add(30 * time.Minute), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
}

// authSessionIdentityRows 构造 Auth Repository 单次 JOIN 查询行。
func authSessionIdentityRows(session auth.WebSession) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"session_id", "session_user_id", "session_token_digest", "session_csrf_token_digest", "session_status", "session_version",
		"session_last_seen_at", "session_idle_expires_at", "session_absolute_expires_at", "session_revoked_at", "session_revoke_reason",
		"session_created_at", "session_updated_at", "account_display_name", "account_status", "identity_normalized_email",
	}).AddRow(
		session.ID, session.UserID, session.TokenDigest[:], session.CSRFTokenDigest[:], string(session.Status), session.SessionVersion,
		session.LastSeenAt, session.IdleExpiresAt, session.AbsoluteExpiresAt, session.RevokedAt, session.RevokeReason,
		session.CreatedAt, session.UpdatedAt, "测试用户", "active", "user@example.com",
	)
}

func TestAuthRepositoryCreatesDigestOnlySession(t *testing.T) {
	repository, _, mock := newAuthRepositoryTestDB(t)
	session := validAuthRepositorySession()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM business\.user_account.*FOR UPDATE`).
		WithArgs(session.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(session.UserID))
	mock.ExpectExec(`(?s)WITH ranked AS \(.*ROW_NUMBER\(\).*UPDATE business\.auth_web_session AS session.*concurrent_session_limit`).
		WithArgs(session.UserID, session.CreatedAt, session.CreatedAt, session.CreatedAt, session.CreatedAt, 5).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."auth_web_session"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repository.Create(context.Background(), session, 5); err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet auth create SQL expectations: %v", err)
	}
}

func TestAuthRepositoryRejectsInvalidConcurrentSessionLimitBeforeSQL(t *testing.T) {
	repository, _, mock := newAuthRepositoryTestDB(t)
	if err := repository.Create(context.Background(), validAuthRepositorySession(), 0); !errors.Is(err, auth.ErrInvalidWebSession) {
		t.Fatalf("Create() error = %v, want ErrInvalidWebSession", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("invalid policy reached PostgreSQL: %v", err)
	}
}

func TestAuthRepositoryFindsSessionIdentityWithOneJoinQuery(t *testing.T) {
	repository, _, mock := newAuthRepositoryTestDB(t)
	session := validAuthRepositorySession()
	mock.ExpectQuery(`(?s)FROM business\.auth_web_session AS session.*LEFT JOIN business\.user_account.*LEFT JOIN LATERAL`).
		WithArgs(session.TokenDigest[:]).
		WillReturnRows(authSessionIdentityRows(session))
	identity, err := repository.FindByTokenDigest(context.Background(), session.TokenDigest)
	if err != nil {
		t.Fatalf("find auth session identity: %v", err)
	}
	if identity.Session.ID != session.ID || identity.UserID != session.UserID || identity.NormalizedEmail != "user@example.com" {
		t.Fatalf("unexpected session identity: %+v", identity)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("session resolver executed unexpected SQL count: %v", err)
	}
}

func TestAuthRepositoryTouchesActiveSessionWithVersionFence(t *testing.T) {
	repository, _, mock := newAuthRepositoryTestDB(t)
	session := validAuthRepositorySession()
	touchedAt := session.LastSeenAt.Add(time.Minute)
	idleExpiresAt := touchedAt.Add(30 * time.Minute)
	mock.ExpectExec(`(?s)UPDATE "business"\."auth_web_session" SET .*last_seen_at.*session_version.*WHERE .*session_version.*status = 'active'`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.Touch(context.Background(), session.ID, session.SessionVersion, touchedAt, idleExpiresAt); err != nil {
		t.Fatalf("touch active auth session: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet touch SQL expectations: %v", err)
	}
}

func TestAuthRepositoryRevokesAndAcceptsAlreadyRevoked(t *testing.T) {
	for _, disposition := range []string{"revoked", "already_revoked"} {
		t.Run(disposition, func(t *testing.T) {
			repository, _, mock := newAuthRepositoryTestDB(t)
			session := validAuthRepositorySession()
			now := session.CreatedAt.Add(time.Minute)
			mock.ExpectQuery(`(?s)WITH updated AS \(.*UPDATE business\.auth_web_session.*END AS disposition`).
				WithArgs(now, "user_logout", now, session.ID, session.SessionVersion, session.ID, session.ID).
				WillReturnRows(sqlmock.NewRows([]string{"disposition"}).AddRow(disposition))
			if err := repository.Revoke(context.Background(), session.ID, session.SessionVersion, now, "user_logout"); err != nil {
				t.Fatalf("revoke auth session with %s: %v", disposition, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet revoke SQL expectations: %v", err)
			}
		})
	}
}

func TestUserRepositoryFindAuthenticationRecordUsesSingleJoin(t *testing.T) {
	_, repository, mock := newAuthRepositoryTestDB(t)
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	rows := sqlmock.NewRows([]string{
		"account_id", "account_display_name", "account_user_type", "account_status", "account_version", "account_created_at", "account_updated_at",
		"identity_id", "identity_user_id", "identity_type", "identity_normalized_identifier", "identity_verified", "identity_verified_at", "identity_created_at", "identity_updated_at",
		"credential_id", "credential_user_id", "credential_algorithm", "credential_memory_kib", "credential_iterations", "credential_parallelism", "credential_salt", "credential_password_hash", "credential_version", "credential_password_changed_at", "credential_created_at", "credential_updated_at",
	}).AddRow(
		"019f0000-0000-7000-8000-000000000011", "测试用户", "personal", "active", 1, now.Add(-24*time.Hour), now,
		"019f0000-0000-7000-8000-000000000012", "019f0000-0000-7000-8000-000000000011", "email", "user@example.com", true, verifiedAt, now.Add(-24*time.Hour), now,
		"019f0000-0000-7000-8000-000000000013", "019f0000-0000-7000-8000-000000000011", "argon2id", 64, 1, 1, []byte("1234567890123456"), []byte("12345678901234567890123456789012"), 1, now, now.Add(-24*time.Hour), now,
	)
	mock.ExpectQuery(`(?s)FROM business\.user_login_identity AS identity.*JOIN business\.user_account.*JOIN business\.user_password_credential`).
		WithArgs("email", "user@example.com").WillReturnRows(rows)
	record, err := repository.FindAuthenticationRecord(context.Background(), user.IdentityTypeEmail, "user@example.com")
	if err != nil {
		t.Fatalf("find authentication record: %v", err)
	}
	if record.Account.DisplayName != "测试用户" || record.Credential.Algorithm != "argon2id" || string(record.Credential.PasswordHash) == "" {
		t.Fatalf("unexpected authentication record: %+v", record)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("authentication lookup executed unexpected SQL count: %v", err)
	}
}

func TestAuthRepositoriesRedactDatabaseErrors(t *testing.T) {
	t.Run("session", func(t *testing.T) {
		repository, _, mock := newAuthRepositoryTestDB(t)
		session := validAuthRepositorySession()
		mock.ExpectQuery(`(?s)FROM business\.auth_web_session`).WillReturnError(errors.New("postgres password=secret SQL=SELECT token"))
		_, err := repository.FindByTokenDigest(context.Background(), session.TokenDigest)
		if !errors.Is(err, auth.ErrPersistence) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "SQL") {
			t.Fatalf("auth repository leaked database details: %v", err)
		}
	})
	t.Run("credential", func(t *testing.T) {
		_, repository, mock := newAuthRepositoryTestDB(t)
		mock.ExpectQuery(`(?s)FROM business\.user_login_identity`).WillReturnError(errors.New("postgres dsn=secret SQL=SELECT password_hash"))
		_, err := repository.FindAuthenticationRecord(context.Background(), user.IdentityTypeEmail, "user@example.com")
		if !errors.Is(err, user.ErrPersistence) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "SQL") {
			t.Fatalf("user repository leaked database details: %v", err)
		}
	})
}
