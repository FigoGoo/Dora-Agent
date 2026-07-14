package postgres

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/user"
)

// userAccountModel 用户账户持久化模型，只负责 business.user_account 与用户实体之间的显式映射。
type userAccountModel struct {
	// ID 用户 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// DisplayName 用户安全展示名。
	DisplayName string `gorm:"column:display_name"`
	// UserType 用户类型稳定代码。
	UserType string `gorm:"column:user_type"`
	// Status 用户状态稳定代码。
	Status string `gorm:"column:status"`
	// Version 用户聚合乐观并发版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 用户账户创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 用户账户最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回用户账户权威表名，禁止 GORM 根据结构体名推导其他 Schema。
func (userAccountModel) TableName() string { return "business.user_account" }

// userLoginIdentityModel 登录身份持久化模型，不包含密码摘要或前端 DTO 行为。
type userLoginIdentityModel struct {
	// ID 登录身份 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// UserID 所属用户逻辑关联标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// IdentityType 登录身份类型稳定代码。
	IdentityType string `gorm:"column:identity_type"`
	// NormalizedIdentifier 规范化登录标识。
	NormalizedIdentifier string `gorm:"column:normalized_identifier"`
	// Verified 登录身份是否已验证。
	Verified bool `gorm:"column:verified"`
	// VerifiedAt 登录身份验证时间，未验证时为空。
	VerifiedAt *time.Time `gorm:"column:verified_at"`
	// CreatedAt 登录身份创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 登录身份最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回登录身份权威表名，禁止 GORM 自动创建或修改表。
func (userLoginIdentityModel) TableName() string { return "business.user_login_identity" }

// userPasswordCredentialModel 密码凭证持久化模型，敏感字段只能在 PostgreSQL Repository 内使用。
type userPasswordCredentialModel struct {
	// ID 密码凭证 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// UserID 所属用户逻辑关联标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// Algorithm 密码摘要算法。
	Algorithm string `gorm:"column:algorithm"`
	// MemoryKiB Argon2id 内存参数，单位为 KiB。
	MemoryKiB int32 `gorm:"column:memory_kib"`
	// Iterations Argon2id 迭代次数。
	Iterations int32 `gorm:"column:iterations"`
	// Parallelism Argon2id 并行度。
	Parallelism int16 `gorm:"column:parallelism"`
	// Salt 密码摘要随机盐。
	Salt []byte `gorm:"column:salt"`
	// PasswordHash 不可逆密码摘要。
	PasswordHash []byte `gorm:"column:password_hash"`
	// CredentialVersion 凭证版本。
	CredentialVersion int64 `gorm:"column:credential_version"`
	// PasswordChangedAt 最近一次密码变更时间。
	PasswordChangedAt time.Time `gorm:"column:password_changed_at"`
	// CreatedAt 密码凭证创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 密码凭证最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回密码凭证权威表名，确保敏感字段不会进入其他自动推导表。
func (userPasswordCredentialModel) TableName() string {
	return "business.user_password_credential"
}

// authWebSessionModel 浏览器安全会话持久化模型，只保存 Token 和 CSRF Token 的摘要。
type authWebSessionModel struct {
	// ID Web Session UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// UserID 会话所属用户逻辑关联标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// TokenDigest 不透明会话 Token 的 SHA-256 摘要。
	TokenDigest []byte `gorm:"column:token_digest"`
	// CSRFTokenDigest 会话绑定 CSRF Token 的 SHA-256 摘要。
	CSRFTokenDigest []byte `gorm:"column:csrf_token_digest"`
	// Status 会话状态稳定代码。
	Status string `gorm:"column:status"`
	// SessionVersion 会话撤销与身份断言版本。
	SessionVersion int64 `gorm:"column:session_version"`
	// LastSeenAt 最近一次有效访问时间。
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
	// IdleExpiresAt 空闲有效期截止时间。
	IdleExpiresAt time.Time `gorm:"column:idle_expires_at"`
	// AbsoluteExpiresAt 绝对有效期截止时间。
	AbsoluteExpiresAt time.Time `gorm:"column:absolute_expires_at"`
	// RevokedAt 会话撤销时间，仅 revoked 状态存在。
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	// RevokeReason 会话撤销稳定原因代码。
	RevokeReason *string `gorm:"column:revoke_reason"`
	// CreatedAt Web Session 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt Web Session 最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回浏览器安全会话权威表名，避免跨 Schema 推导。
func (authWebSessionModel) TableName() string { return "business.auth_web_session" }

// userAccountModelFromEntity 校验并映射用户账户；失败表示领域事实不允许进入 Repository。
func userAccountModelFromEntity(entity user.Account) (userAccountModel, error) {
	if err := entity.Validate(); err != nil {
		return userAccountModel{}, fmt.Errorf("map user account to persistence model: %w", err)
	}
	return userAccountModel{
		ID: entity.ID, DisplayName: entity.DisplayName, UserType: string(entity.Type), Status: string(entity.Status), Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}, nil
}

// userAccountEntity 将持久化模型恢复为用户账户并再次校验数据库事实。
func userAccountEntity(model userAccountModel) (user.Account, error) {
	entity := user.Account{
		ID: model.ID, DisplayName: model.DisplayName, Type: user.Type(model.UserType), Status: user.Status(model.Status), Version: model.Version,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := entity.Validate(); err != nil {
		return user.Account{}, fmt.Errorf("map persistence model to user account: %w", err)
	}
	return entity, nil
}

// userLoginIdentityModelFromEntity 校验并映射登录身份，不记录完整规范化标识。
func userLoginIdentityModelFromEntity(entity user.LoginIdentity) (userLoginIdentityModel, error) {
	if err := entity.Validate(); err != nil {
		return userLoginIdentityModel{}, fmt.Errorf("map login identity to persistence model: %w", err)
	}
	return userLoginIdentityModel{
		ID: entity.ID, UserID: entity.UserID, IdentityType: string(entity.Type), NormalizedIdentifier: entity.NormalizedIdentifier,
		Verified: entity.Verified, VerifiedAt: entity.VerifiedAt, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}, nil
}

// userLoginIdentityEntity 将持久化模型恢复为登录身份并校验验证状态一致性。
func userLoginIdentityEntity(model userLoginIdentityModel) (user.LoginIdentity, error) {
	entity := user.LoginIdentity{
		ID: model.ID, UserID: model.UserID, Type: user.IdentityType(model.IdentityType), NormalizedIdentifier: model.NormalizedIdentifier,
		Verified: model.Verified, VerifiedAt: model.VerifiedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := entity.Validate(); err != nil {
		return user.LoginIdentity{}, fmt.Errorf("map persistence model to login identity: %w", err)
	}
	return entity, nil
}

// userPasswordCredentialModelFromEntity 校验并深拷贝敏感凭证，防止调用方并发修改切片。
func userPasswordCredentialModelFromEntity(entity user.PasswordCredential) (userPasswordCredentialModel, error) {
	if err := entity.Validate(); err != nil {
		return userPasswordCredentialModel{}, fmt.Errorf("map password credential to persistence model: %w", err)
	}
	return userPasswordCredentialModel{
		ID: entity.ID, UserID: entity.UserID, Algorithm: entity.Algorithm, MemoryKiB: entity.MemoryKiB,
		Iterations: entity.Iterations, Parallelism: entity.Parallelism, Salt: append([]byte(nil), entity.Salt...),
		PasswordHash: append([]byte(nil), entity.PasswordHash...), CredentialVersion: entity.CredentialVersion,
		PasswordChangedAt: entity.PasswordChangedAt, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}, nil
}

// userPasswordCredentialEntity 将持久化凭证深拷贝为领域实体并校验 Argon2id 不变量。
func userPasswordCredentialEntity(model userPasswordCredentialModel) (user.PasswordCredential, error) {
	entity := user.PasswordCredential{
		ID: model.ID, UserID: model.UserID, Algorithm: model.Algorithm, MemoryKiB: model.MemoryKiB,
		Iterations: model.Iterations, Parallelism: model.Parallelism, Salt: append([]byte(nil), model.Salt...),
		PasswordHash: append([]byte(nil), model.PasswordHash...), CredentialVersion: model.CredentialVersion,
		PasswordChangedAt: model.PasswordChangedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := entity.Validate(); err != nil {
		return user.PasswordCredential{}, fmt.Errorf("map persistence model to password credential: %w", err)
	}
	return entity, nil
}

// authWebSessionModelFromEntity 校验并映射会话摘要，不接收或保存原始 Cookie 和 CSRF Token。
func authWebSessionModelFromEntity(entity auth.WebSession) (authWebSessionModel, error) {
	if err := entity.Validate(); err != nil {
		return authWebSessionModel{}, fmt.Errorf("map web session to persistence model: %w", err)
	}
	return authWebSessionModel{
		ID: entity.ID, UserID: entity.UserID, TokenDigest: append([]byte(nil), entity.TokenDigest[:]...),
		CSRFTokenDigest: append([]byte(nil), entity.CSRFTokenDigest[:]...), Status: string(entity.Status),
		SessionVersion: entity.SessionVersion, LastSeenAt: entity.LastSeenAt, IdleExpiresAt: entity.IdleExpiresAt,
		AbsoluteExpiresAt: entity.AbsoluteExpiresAt, RevokedAt: entity.RevokedAt, RevokeReason: entity.RevokeReason,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}, nil
}

// authWebSessionEntity 将持久化模型恢复为会话实体并拒绝长度异常的安全摘要。
func authWebSessionEntity(model authWebSessionModel) (auth.WebSession, error) {
	if len(model.TokenDigest) != len(auth.Digest{}) || len(model.CSRFTokenDigest) != len(auth.Digest{}) {
		return auth.WebSession{}, fmt.Errorf("map persistence model to web session: %w", auth.ErrInvalidWebSession)
	}
	entity := auth.WebSession{
		ID: model.ID, UserID: model.UserID, Status: auth.SessionStatus(model.Status), SessionVersion: model.SessionVersion,
		LastSeenAt: model.LastSeenAt, IdleExpiresAt: model.IdleExpiresAt, AbsoluteExpiresAt: model.AbsoluteExpiresAt,
		RevokedAt: model.RevokedAt, RevokeReason: model.RevokeReason, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	copy(entity.TokenDigest[:], model.TokenDigest)
	copy(entity.CSRFTokenDigest[:], model.CSRFTokenDigest)
	if err := entity.Validate(); err != nil {
		return auth.WebSession{}, fmt.Errorf("map persistence model to web session: %w", err)
	}
	return entity, nil
}
