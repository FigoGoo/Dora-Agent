package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// AuthRepository 使用 GORM 实现 Web Session 创建、单次 JOIN 解析与幂等撤销。
type AuthRepository struct {
	// db 只在 Repository 内部使用，不向认证用例或 HTTP 层暴露。
	db *gorm.DB
}

var _ auth.Repository = (*AuthRepository)(nil)

// sessionIdentityRow 承接 Web Session、账户与已验证邮箱的单次 JOIN 结果，明确不包含密码凭据。
type sessionIdentityRow struct {
	// SessionID Web Session 标识。
	SessionID string `gorm:"column:session_id"`
	// SessionUserID 会话所属用户标识。
	SessionUserID string `gorm:"column:session_user_id"`
	// SessionTokenDigest 会话 Cookie 的 SHA-256 摘要。
	SessionTokenDigest []byte `gorm:"column:session_token_digest"`
	// SessionCSRFTokenDigest 会话绑定 CSRF Token 的 SHA-256 摘要。
	SessionCSRFTokenDigest []byte `gorm:"column:session_csrf_token_digest"`
	// SessionStatus 会话状态。
	SessionStatus string `gorm:"column:session_status"`
	// SessionVersion 会话并发版本。
	SessionVersion int64 `gorm:"column:session_version"`
	// SessionLastSeenAt 会话最近有效活动时间。
	SessionLastSeenAt time.Time `gorm:"column:session_last_seen_at"`
	// SessionIdleExpiresAt 会话空闲过期时间。
	SessionIdleExpiresAt time.Time `gorm:"column:session_idle_expires_at"`
	// SessionAbsoluteExpiresAt 会话绝对过期时间。
	SessionAbsoluteExpiresAt time.Time `gorm:"column:session_absolute_expires_at"`
	// SessionRevokedAt 会话撤销时间。
	SessionRevokedAt *time.Time `gorm:"column:session_revoked_at"`
	// SessionRevokeReason 会话撤销稳定原因。
	SessionRevokeReason *string `gorm:"column:session_revoke_reason"`
	// SessionCreatedAt 会话创建时间。
	SessionCreatedAt time.Time `gorm:"column:session_created_at"`
	// SessionUpdatedAt 会话更新时间。
	SessionUpdatedAt time.Time `gorm:"column:session_updated_at"`
	// AccountDisplayName 用户安全展示名。
	AccountDisplayName string `gorm:"column:account_display_name"`
	// AccountStatus 用户当前状态。
	AccountStatus string `gorm:"column:account_status"`
	// IdentityNormalizedEmail 已验证规范化邮箱。
	IdentityNormalizedEmail string `gorm:"column:identity_normalized_email"`
}

// revokeSessionResult 承接单条 PostgreSQL CTE 撤销的稳定结果代码。
type revokeSessionResult struct {
	// Disposition 是 revoked、already_revoked、conflict 或 not_found。
	Disposition string `gorm:"column:disposition"`
}

// NewAuthRepository 从 Business PostgreSQL Client 创建会话 Repository；Client 未初始化时失败关闭。
func NewAuthRepository(client *Client) (*AuthRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create auth repository: postgres client is nil")
	}
	return &AuthRepository{db: client.db}, nil
}

// Create 在用户行锁保护的事务内撤销最旧超额会话并保存新会话摘要；原始 Token 不被本 Repository 接收。
func (r *AuthRepository) Create(ctx context.Context, session auth.WebSession, maxConcurrentSessions int) error {
	model, err := authWebSessionModelFromEntity(session)
	if err != nil || maxConcurrentSessions < 1 || maxConcurrentSessions > 100 {
		return auth.ErrInvalidWebSession
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 锁定权威用户行，使同一用户的并发登录串行执行会话上限策略；不同用户不互相阻塞。
		var lockedUserID string
		if err := tx.Raw(`
			SELECT id
			FROM business.user_account
			WHERE id = ?
			FOR UPDATE`, session.UserID).Scan(&lockedUserID).Error; err != nil {
			return err
		}
		if lockedUserID != session.UserID {
			return auth.ErrPersistence
		}

		// 新会话占用一个名额，因此只保留最近的 maxConcurrentSessions-1 个当前有效会话。
		if err := tx.Exec(`
			WITH ranked AS (
				SELECT id,
					ROW_NUMBER() OVER (
						ORDER BY last_seen_at DESC, created_at DESC, id DESC
					) AS session_rank
				FROM business.auth_web_session
				WHERE user_id = ?
				  AND status = 'active'
				  AND idle_expires_at > ?
				  AND absolute_expires_at > ?
			)
			UPDATE business.auth_web_session AS session
			SET status = 'revoked',
				session_version = session.session_version + 1,
				revoked_at = ?,
				revoke_reason = 'concurrent_session_limit',
				updated_at = ?
			WHERE session.id IN (
				SELECT ranked.id
				FROM ranked
				WHERE ranked.session_rank >= ?
			)`,
			session.UserID,
			session.CreatedAt.UTC(),
			session.CreatedAt.UTC(),
			session.CreatedAt.UTC(),
			session.CreatedAt.UTC(),
			maxConcurrentSessions,
		).Error; err != nil {
			return err
		}
		return tx.Create(&model).Error
	})
	if err != nil {
		if errors.Is(err, auth.ErrPersistence) {
			return err
		}
		return mapAuthRepositoryError(err)
	}
	return nil
}

// FindByTokenDigest 使用一次显式字段 JOIN/LATERAL 查询返回会话、账户和一个已验证邮箱；不查询密码表。
func (r *AuthRepository) FindByTokenDigest(ctx context.Context, tokenDigest auth.Digest) (auth.SessionIdentity, error) {
	var row sessionIdentityRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			session.id AS session_id,
			session.user_id AS session_user_id,
			session.token_digest AS session_token_digest,
			session.csrf_token_digest AS session_csrf_token_digest,
			session.status AS session_status,
			session.session_version AS session_version,
			session.last_seen_at AS session_last_seen_at,
			session.idle_expires_at AS session_idle_expires_at,
			session.absolute_expires_at AS session_absolute_expires_at,
			session.revoked_at AS session_revoked_at,
			session.revoke_reason AS session_revoke_reason,
			session.created_at AS session_created_at,
			session.updated_at AS session_updated_at,
			account.display_name AS account_display_name,
			account.status AS account_status,
			identity.normalized_identifier AS identity_normalized_email
		FROM business.auth_web_session AS session
		LEFT JOIN business.user_account AS account ON account.id = session.user_id
		LEFT JOIN LATERAL (
			SELECT login_identity.normalized_identifier
			FROM business.user_login_identity AS login_identity
			WHERE login_identity.user_id = session.user_id
			  AND login_identity.identity_type = 'email'
			  AND login_identity.verified = TRUE
			ORDER BY login_identity.created_at ASC, login_identity.id ASC
			LIMIT 1
		) AS identity ON TRUE
		WHERE session.token_digest = ?
		LIMIT 1`, tokenDigest[:]).Scan(&row).Error
	if err != nil {
		return auth.SessionIdentity{}, mapAuthRepositoryError(err)
	}
	if row.SessionID == "" {
		return auth.SessionIdentity{}, auth.ErrWebSessionNotFound
	}
	model := authWebSessionModel{
		ID: row.SessionID, UserID: row.SessionUserID, TokenDigest: row.SessionTokenDigest, CSRFTokenDigest: row.SessionCSRFTokenDigest,
		Status: row.SessionStatus, SessionVersion: row.SessionVersion, LastSeenAt: row.SessionLastSeenAt,
		IdleExpiresAt: row.SessionIdleExpiresAt, AbsoluteExpiresAt: row.SessionAbsoluteExpiresAt,
		RevokedAt: row.SessionRevokedAt, RevokeReason: row.SessionRevokeReason,
		CreatedAt: row.SessionCreatedAt, UpdatedAt: row.SessionUpdatedAt,
	}
	session, err := authWebSessionEntity(model)
	if err != nil {
		return auth.SessionIdentity{}, auth.ErrPersistence
	}
	if row.SessionUserID == "" {
		return auth.SessionIdentity{}, auth.ErrPersistence
	}
	return auth.SessionIdentity{
		Session: session, UserID: row.SessionUserID, DisplayName: row.AccountDisplayName,
		NormalizedEmail: row.IdentityNormalizedEmail, AccountStatus: row.AccountStatus,
	}, nil
}

// Touch 使用预期版本只滑动仍有效的 active 会话；撤销、过期或并发更新统一返回版本冲突，由 Service 重新读取判定。
func (r *AuthRepository) Touch(ctx context.Context, sessionID string, expectedVersion int64, lastSeenAt time.Time, idleExpiresAt time.Time) error {
	if sessionID == "" || expectedVersion < 1 || lastSeenAt.IsZero() || !idleExpiresAt.After(lastSeenAt) {
		return auth.ErrInvalidWebSession
	}
	updated := r.db.WithContext(ctx).Model(&authWebSessionModel{}).
		Where(`id = ? AND session_version = ? AND status = 'active'
			AND idle_expires_at > ? AND absolute_expires_at > ? AND absolute_expires_at >= ?`,
			sessionID, expectedVersion, lastSeenAt.UTC(), lastSeenAt.UTC(), idleExpiresAt.UTC()).
		Updates(map[string]any{
			"last_seen_at": lastSeenAt.UTC(), "idle_expires_at": idleExpiresAt.UTC(),
			"session_version": gorm.Expr("session_version + 1"), "updated_at": lastSeenAt.UTC(),
		})
	if updated.Error != nil {
		return mapAuthRepositoryError(updated.Error)
	}
	if updated.RowsAffected != 1 {
		return auth.ErrWebSessionVersionConflict
	}
	return nil
}

// Revoke 用单条 PostgreSQL CTE 按预期版本撤销 active 会话；已撤销会话幂等成功，其他版本不一致返回稳定冲突。
func (r *AuthRepository) Revoke(ctx context.Context, sessionID string, expectedVersion int64, revokedAt time.Time, reason string) error {
	if sessionID == "" || expectedVersion < 1 || revokedAt.IsZero() || reason == "" || len(reason) > 128 {
		return auth.ErrInvalidWebSession
	}
	var result revokeSessionResult
	err := r.db.WithContext(ctx).Raw(`
		WITH updated AS (
			UPDATE business.auth_web_session
			SET status = 'revoked',
				session_version = session_version + 1,
				revoked_at = ?,
				revoke_reason = ?,
				updated_at = ?
			WHERE id = ? AND session_version = ? AND status = 'active'
			RETURNING 1
		)
		SELECT CASE
			WHEN EXISTS (SELECT 1 FROM updated) THEN 'revoked'
			WHEN EXISTS (SELECT 1 FROM business.auth_web_session WHERE id = ? AND status = 'revoked') THEN 'already_revoked'
			WHEN EXISTS (SELECT 1 FROM business.auth_web_session WHERE id = ?) THEN 'conflict'
			ELSE 'not_found'
		END AS disposition`, revokedAt.UTC(), reason, revokedAt.UTC(), sessionID, expectedVersion, sessionID, sessionID).Scan(&result).Error
	if err != nil {
		return mapAuthRepositoryError(err)
	}
	switch result.Disposition {
	case "revoked", "already_revoked":
		return nil
	case "conflict":
		return auth.ErrWebSessionVersionConflict
	case "not_found":
		return auth.ErrWebSessionNotFound
	default:
		return auth.ErrPersistence
	}
}

// mapAuthRepositoryError 保留 Context 语义、识别 Token 摘要冲突，其他底层错误收敛为不泄漏的稳定错误。
func mapAuthRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "uq_auth_web_session__token_digest" {
		return auth.ErrWebSessionTokenCollision
	}
	return auth.ErrPersistence
}
