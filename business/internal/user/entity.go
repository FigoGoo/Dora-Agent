// Package user 定义 Business 用户账户、登录身份和密码凭证的领域边界。
package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	// ErrInvalidAccount 表示用户账户字段不满足 UUIDv7、状态或版本不变量。
	ErrInvalidAccount = errors.New("invalid user account")
	// ErrInvalidLoginIdentity 表示登录身份字段不满足类型、规范化标识或验证状态不变量。
	ErrInvalidLoginIdentity = errors.New("invalid user login identity")
	// ErrInvalidPasswordCredential 表示密码凭证字段不满足 Argon2id 参数或敏感摘要不变量。
	ErrInvalidPasswordCredential = errors.New("invalid user password credential")
	// ErrUserNotFound 表示用户或对应认证事实不存在，调用方不得据此暴露账号枚举信息。
	ErrUserNotFound = errors.New("user not found")
	// ErrPersistence 表示用户事实读写失败，不包含 SQL、DSN 或凭据等底层细节。
	ErrPersistence = errors.New("user persistence unavailable")
	// ErrInvalidAuthenticationRecord 表示账户、登录身份与密码凭据的逻辑 User ID 关联不一致。
	ErrInvalidAuthenticationRecord = errors.New("invalid authentication record")
)

// Type 用户类型，区分个人用户和企业用户但不表达租户语义。
type Type string

const (
	// TypePersonal 表示个人用户。
	TypePersonal Type = "personal"
	// TypeEnterprise 表示企业用户。
	TypeEnterprise Type = "enterprise"
)

// Status 用户状态，决定账户是否允许建立或继续使用安全会话。
type Status string

const (
	// StatusActive 表示用户可以正常使用受保护能力。
	StatusActive Status = "active"
	// StatusDisabled 表示用户被禁用且不得继续通过鉴权。
	StatusDisabled Status = "disabled"
	// StatusCancelled 表示用户已进入注销后的不可恢复状态。
	StatusCancelled Status = "cancelled"
)

// Account 用户账户实体，保存安全展示名、类型、状态和乐观并发版本。
type Account struct {
	// ID 用户唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// DisplayName 用户安全展示名，不得包含密码、令牌或完整邮箱等敏感凭据。
	DisplayName string
	// Type 用户类型，不隐含组织或租户边界。
	Type Type
	// Status 用户状态，用于登录和受保护资源访问判断。
	Status Status
	// Version 用户聚合乐观并发版本，从 1 开始。
	Version int64
	// CreatedAt 用户账户创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 用户账户最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// Validate 校验用户账户持久化前后的稳定不变量；成功表示可安全映射到数据库，失败返回 ErrInvalidAccount。
func (a Account) Validate() error {
	if !isUUIDv7(a.ID) || strings.TrimSpace(a.DisplayName) == "" || utf8.RuneCountInString(a.DisplayName) > 160 ||
		(a.Type != TypePersonal && a.Type != TypeEnterprise) {
		return ErrInvalidAccount
	}
	for _, char := range a.DisplayName {
		if unicode.IsControl(char) {
			return ErrInvalidAccount
		}
	}
	if a.Status != StatusActive && a.Status != StatusDisabled && a.Status != StatusCancelled {
		return ErrInvalidAccount
	}
	if a.Version < 1 || a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return ErrInvalidAccount
	}
	return nil
}

// IdentityType 登录身份类型；W0 只冻结 Email，其他类型必须通过后续契约与 Migration 扩展。
type IdentityType string

const (
	// IdentityTypeEmail 表示邮箱登录身份。
	IdentityTypeEmail IdentityType = "email"
)

// LoginIdentity 用户登录身份实体，只保存规范化标识且不承载密码凭证。
type LoginIdentity struct {
	// ID 登录身份唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// UserID 所属用户标识，是 Business 内逻辑关联。
	UserID string
	// Type 登录身份类型，决定规范化和验证策略。
	Type IdentityType
	// NormalizedIdentifier 规范化后的邮箱，普通日志不得记录完整值。
	NormalizedIdentifier string
	// Verified 表示登录身份是否已完成所有权验证。
	Verified bool
	// VerifiedAt 登录身份验证时间，未验证时为空。
	VerifiedAt *time.Time
	// CreatedAt 登录身份创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 登录身份最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// Validate 校验登录身份及验证时间的一致性；成功表示可持久化，失败返回 ErrInvalidLoginIdentity。
func (i LoginIdentity) Validate() error {
	if !isUUIDv7(i.ID) || !isUUIDv7(i.UserID) || i.Type != IdentityTypeEmail {
		return ErrInvalidLoginIdentity
	}
	if i.NormalizedIdentifier == "" || len(i.NormalizedIdentifier) > 320 || i.CreatedAt.IsZero() || i.UpdatedAt.Before(i.CreatedAt) {
		return ErrInvalidLoginIdentity
	}
	// 验证标记与验证时间必须同时变化，避免生成无法解释的半验证身份。
	if i.Verified != (i.VerifiedAt != nil) {
		return ErrInvalidLoginIdentity
	}
	return nil
}

// PasswordCredential 用户密码凭证实体，仅保存 Argon2id 参数、盐和不可逆摘要。
type PasswordCredential struct {
	// ID 密码凭证唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// UserID 所属用户标识，是 Business 内逻辑关联。
	UserID string
	// Algorithm 密码摘要算法，W0 固定为 argon2id。
	Algorithm string
	// MemoryKiB Argon2id 单次计算使用内存，单位为 KiB。
	MemoryKiB int32
	// Iterations Argon2id 迭代次数。
	Iterations int32
	// Parallelism Argon2id 并行度。
	Parallelism int16
	// Salt 密码摘要随机盐，不得记录或返回给前端。
	Salt []byte
	// PasswordHash 不可逆密码摘要，不得记录或返回给前端。
	PasswordHash []byte
	// CredentialVersion 凭证版本，用于参数升级和会话撤销判断。
	CredentialVersion int64
	// PasswordChangedAt 最近一次密码变更的 UTC 时间。
	PasswordChangedAt time.Time
	// CreatedAt 密码凭证创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 密码凭证最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// Validate 校验密码凭证算法、参数和敏感摘要长度；成功表示可持久化，失败返回 ErrInvalidPasswordCredential。
func (c PasswordCredential) Validate() error {
	if !isUUIDv7(c.ID) || !isUUIDv7(c.UserID) || c.Algorithm != "argon2id" {
		return ErrInvalidPasswordCredential
	}
	if c.MemoryKiB <= 0 || c.Iterations <= 0 || c.Parallelism <= 0 || len(c.Salt) < 16 || len(c.PasswordHash) < 16 {
		return ErrInvalidPasswordCredential
	}
	if c.CredentialVersion < 1 || c.PasswordChangedAt.IsZero() || c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return ErrInvalidPasswordCredential
	}
	return nil
}

// AuthenticationRecord 登录认证查询记录，在用户领域内组合固定数量查询得到的账户、身份和密码凭证。
type AuthenticationRecord struct {
	// Account 登录身份所属用户账户。
	Account Account
	// Identity 已按规范化标识唯一命中的登录身份。
	Identity LoginIdentity
	// Credential 用户当前有效密码凭证。
	Credential PasswordCredential
}

// Repository 定义用户注册写入和登录认证读取所需的最小持久化能力。
type Repository interface {
	// CreateAuthenticationRecord 原子创建账户、登录身份和密码凭证；任一步失败都不得留下部分事实。
	CreateAuthenticationRecord(ctx context.Context, record AuthenticationRecord) error
	// FindAuthenticationRecord 按身份类型和规范化标识读取认证事实；不存在时返回 ErrUserNotFound。
	FindAuthenticationRecord(ctx context.Context, identityType IdentityType, normalizedIdentifier string) (AuthenticationRecord, error)
}

// isUUIDv7 校验标识是否为应用侧生成的 UUIDv7，避免其他 UUID 版本进入领域事实。
func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7
}
