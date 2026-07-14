// Package auth 定义 Business 浏览器安全会话的领域边界。
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidWebSession 表示 Web Session 字段不满足摘要、状态、时间或 UUIDv7 不变量。
	ErrInvalidWebSession = errors.New("invalid web session")
	// ErrWebSessionNotFound 表示令牌摘要对应的 Web Session 不存在。
	ErrWebSessionNotFound = errors.New("web session not found")
	// ErrWebSessionVersionConflict 表示撤销或续期时的预期版本已过期。
	ErrWebSessionVersionConflict = errors.New("web session version conflict")
	// ErrWebSessionTokenCollision 表示极小概率的会话 Token 摘要唯一约束冲突，不包含原始 Token。
	ErrWebSessionTokenCollision = errors.New("web session token collision")
	// ErrPersistence 表示会话持久化不可用，不向上层泄漏底层 SQL 或连接细节。
	ErrPersistence = errors.New("auth persistence unavailable")
)

// SessionStatus Web Session 状态，决定会话是否仍可构造可信身份。
type SessionStatus string

const (
	// SessionStatusActive 表示会话尚未撤销且仍在有效期内。
	SessionStatusActive SessionStatus = "active"
	// SessionStatusRevoked 表示会话已被主动退出、密码变更或风控撤销。
	SessionStatusRevoked SessionStatus = "revoked"
	// SessionStatusExpired 表示会话已超过空闲或绝对有效期。
	SessionStatusExpired SessionStatus = "expired"
)

// Digest 安全令牌的 SHA-256 摘要，避免原始 Cookie 或 CSRF Token 进入数据库。
type Digest [32]byte

// WebSession 浏览器安全会话实体，保存服务端不透明会话的摘要、期限和撤销版本。
type WebSession struct {
	// ID Web Session 唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// UserID 会话所属用户标识，是 Business 内逻辑关联。
	UserID string
	// TokenDigest 随机不透明会话令牌摘要，不能反向恢复原始 Cookie。
	TokenDigest Digest
	// CSRFTokenDigest 会话绑定防跨站令牌摘要，不能反向恢复原始令牌。
	CSRFTokenDigest Digest
	// Status 会话当前状态。
	Status SessionStatus
	// SessionVersion 会话版本，用于撤销和短期内部身份断言失效判断。
	SessionVersion int64
	// LastSeenAt 最近一次有效访问的 UTC 时间。
	LastSeenAt time.Time
	// IdleExpiresAt 空闲有效期截止的 UTC 时间。
	IdleExpiresAt time.Time
	// AbsoluteExpiresAt 绝对有效期截止的 UTC 时间。
	AbsoluteExpiresAt time.Time
	// RevokedAt 会话撤销时间，仅 revoked 状态存在。
	RevokedAt *time.Time
	// RevokeReason 会话撤销稳定原因代码，仅 revoked 状态存在。
	RevokeReason *string
	// CreatedAt 会话创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 会话最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// SessionIdentity 会话身份查询记录，仅组合会话、账户和安全邮箱，不包含密码凭据。
type SessionIdentity struct {
	// Session 令牌摘要命中的权威 Web Session。
	Session WebSession
	// UserID 会话所属用户唯一标识。
	UserID string
	// DisplayName 用户安全展示名。
	DisplayName string
	// NormalizedEmail 已验证的规范化邮箱，只用于构造安全响应。
	NormalizedEmail string
	// AccountStatus 用户账户当前状态，只有 active 可构造可信 Principal。
	AccountStatus string
}

// Validate 校验 Web Session 的状态、期限、撤销字段和摘要不变量；失败返回 ErrInvalidWebSession。
func (s WebSession) Validate() error {
	if !isUUIDv7(s.ID) || !isUUIDv7(s.UserID) || s.SessionVersion < 1 {
		return ErrInvalidWebSession
	}
	// 全零摘要代表令牌尚未生成，必须在进入唯一索引前失败关闭，防止多个默认会话共享可预测摘要。
	if s.TokenDigest == (Digest{}) || s.CSRFTokenDigest == (Digest{}) {
		return ErrInvalidWebSession
	}
	if s.Status != SessionStatusActive && s.Status != SessionStatusRevoked && s.Status != SessionStatusExpired {
		return ErrInvalidWebSession
	}
	if s.CreatedAt.IsZero() || s.LastSeenAt.IsZero() || s.LastSeenAt.After(s.IdleExpiresAt) || s.IdleExpiresAt.After(s.AbsoluteExpiresAt) {
		return ErrInvalidWebSession
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return ErrInvalidWebSession
	}
	// 撤销状态必须带稳定原因和时间，其他状态不得残留撤销信息，避免鉴权投影出现歧义。
	if s.Status == SessionStatusRevoked {
		if s.RevokedAt == nil || s.RevokeReason == nil || *s.RevokeReason == "" {
			return ErrInvalidWebSession
		}
	} else if s.RevokedAt != nil || s.RevokeReason != nil {
		return ErrInvalidWebSession
	}
	return nil
}

// Repository 定义登录、会话解析、续期和撤销所需的最小持久化能力。
type Repository interface {
	// Create 原子执行用户级并发会话上限并保存新会话摘要；Token 摘要重复时不得覆盖旧会话。
	Create(ctx context.Context, session WebSession, maxConcurrentSessions int) error
	// FindByTokenDigest 用单次集合查询按 Token 摘要读取会话与安全身份；不存在时返回 ErrWebSessionNotFound。
	FindByTokenDigest(ctx context.Context, tokenDigest Digest) (SessionIdentity, error)
	// Touch 按预期版本滑动 active 会话的最近活动和空闲期限；并发版本变化必须返回 ErrWebSessionVersionConflict。
	Touch(ctx context.Context, sessionID string, expectedVersion int64, lastSeenAt time.Time, idleExpiresAt time.Time) error
	// Revoke 按预期版本撤销会话；版本不匹配时返回 ErrWebSessionVersionConflict。
	Revoke(ctx context.Context, sessionID string, expectedVersion int64, revokedAt time.Time, reason string) error
}

// isUUIDv7 校验标识是否为应用侧生成的 UUIDv7，避免身份事实使用其他 UUID 版本。
func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7
}
