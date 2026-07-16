package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	"gorm.io/gorm"
)

// UserRepository 使用 GORM 实现用户认证事实的原子写入与单次 JOIN 读取。
type UserRepository struct {
	// db 只在 Repository 内部使用，不向业务或 Transport 层暴露。
	db *gorm.DB
}

var _ user.Repository = (*UserRepository)(nil)

// authenticationRecordRow 承接账户、邮箱身份与密码凭据单次 JOIN 结果，字段别名防止同名覆盖。
type authenticationRecordRow struct {
	// AccountID 用户账户标识。
	AccountID string `gorm:"column:account_id"`
	// AccountDisplayName 用户安全展示名。
	AccountDisplayName string `gorm:"column:account_display_name"`
	// AccountUserType 用户类型。
	AccountUserType string `gorm:"column:account_user_type"`
	// AccountStatus 用户状态，本查询只会返回 active。
	AccountStatus string `gorm:"column:account_status"`
	// AccountVersion 用户聚合版本。
	AccountVersion int64 `gorm:"column:account_version"`
	// AccountCreatedAt 用户创建时间。
	AccountCreatedAt time.Time `gorm:"column:account_created_at"`
	// AccountUpdatedAt 用户更新时间。
	AccountUpdatedAt time.Time `gorm:"column:account_updated_at"`
	// IdentityID 登录身份标识。
	IdentityID string `gorm:"column:identity_id"`
	// IdentityUserID 登录身份所属用户标识。
	IdentityUserID string `gorm:"column:identity_user_id"`
	// IdentityType 登录身份类型。
	IdentityType string `gorm:"column:identity_type"`
	// IdentityNormalizedIdentifier 规范化邮箱。
	IdentityNormalizedIdentifier string `gorm:"column:identity_normalized_identifier"`
	// IdentityVerified 登录身份已验证标记。
	IdentityVerified bool `gorm:"column:identity_verified"`
	// IdentityVerifiedAt 登录身份验证时间。
	IdentityVerifiedAt *time.Time `gorm:"column:identity_verified_at"`
	// IdentityCreatedAt 登录身份创建时间。
	IdentityCreatedAt time.Time `gorm:"column:identity_created_at"`
	// IdentityUpdatedAt 登录身份更新时间。
	IdentityUpdatedAt time.Time `gorm:"column:identity_updated_at"`
	// CredentialID 密码凭据标识。
	CredentialID string `gorm:"column:credential_id"`
	// CredentialUserID 密码凭据所属用户标识。
	CredentialUserID string `gorm:"column:credential_user_id"`
	// CredentialAlgorithm 密码摘要算法。
	CredentialAlgorithm string `gorm:"column:credential_algorithm"`
	// CredentialMemoryKiB Argon2id 内存参数。
	CredentialMemoryKiB int32 `gorm:"column:credential_memory_kib"`
	// CredentialIterations Argon2id 迭代参数。
	CredentialIterations int32 `gorm:"column:credential_iterations"`
	// CredentialParallelism Argon2id 并行参数。
	CredentialParallelism int16 `gorm:"column:credential_parallelism"`
	// CredentialSalt 密码随机盐，仅在认证用例内使用。
	CredentialSalt []byte `gorm:"column:credential_salt"`
	// CredentialPasswordHash 密码不可逆摘要，不得进入 DTO 或日志。
	CredentialPasswordHash []byte `gorm:"column:credential_password_hash"`
	// CredentialVersion 密码凭据版本。
	CredentialVersion int64 `gorm:"column:credential_version"`
	// CredentialPasswordChangedAt 密码最近修改时间。
	CredentialPasswordChangedAt time.Time `gorm:"column:credential_password_changed_at"`
	// CredentialCreatedAt 密码凭据创建时间。
	CredentialCreatedAt time.Time `gorm:"column:credential_created_at"`
	// CredentialUpdatedAt 密码凭据更新时间。
	CredentialUpdatedAt time.Time `gorm:"column:credential_updated_at"`
}

// NewUserRepository 从 Business PostgreSQL Client 创建用户 Repository；Client 未初始化时失败关闭。
func NewUserRepository(client *Client) (*UserRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create user repository: postgres client is nil")
	}
	return &UserRepository{db: client.db}, nil
}

// CreateAuthenticationRecord 在单一本地事务中依次写入账户、登录身份和密码凭据；任一失败都回滚全部事实。
func (r *UserRepository) CreateAuthenticationRecord(ctx context.Context, record user.AuthenticationRecord) error {
	// 项目禁止物理外键，因此必须在进入事务前显式校验三类事实的逻辑关联，避免创建孤儿凭据。
	if record.Account.ID == "" || record.Identity.UserID != record.Account.ID || record.Credential.UserID != record.Account.ID {
		return user.ErrInvalidAuthenticationRecord
	}
	accountModel, err := userAccountModelFromEntity(record.Account)
	if err != nil {
		return user.ErrPersistence
	}
	identityModel, err := userLoginIdentityModelFromEntity(record.Identity)
	if err != nil {
		return user.ErrPersistence
	}
	credentialModel, err := userPasswordCredentialModelFromEntity(record.Credential)
	if err != nil {
		return user.ErrPersistence
	}
	// 显式顺序维护逻辑关联完整性，不依赖项目禁止的数据库物理外键与级联。
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := transactionDB.Create(&accountModel).Error; err != nil {
			return err
		}
		if err := transactionDB.Create(&identityModel).Error; err != nil {
			return err
		}
		return transactionDB.Create(&credentialModel).Error
	})
	return mapUserRepositoryError(err)
}

// FindAuthenticationRecord 使用单次显式字段 JOIN 读取已验证邮箱、active 账户和 Argon2id 凭据；未命中统一返回 ErrUserNotFound。
func (r *UserRepository) FindAuthenticationRecord(ctx context.Context, identityType user.IdentityType, normalizedIdentifier string) (user.AuthenticationRecord, error) {
	var row authenticationRecordRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			account.id AS account_id,
			account.display_name AS account_display_name,
			account.user_type AS account_user_type,
			account.status AS account_status,
			account.version AS account_version,
			account.created_at AS account_created_at,
			account.updated_at AS account_updated_at,
			identity.id AS identity_id,
			identity.user_id AS identity_user_id,
			identity.identity_type AS identity_type,
			identity.normalized_identifier AS identity_normalized_identifier,
			identity.verified AS identity_verified,
			identity.verified_at AS identity_verified_at,
			identity.created_at AS identity_created_at,
			identity.updated_at AS identity_updated_at,
			credential.id AS credential_id,
			credential.user_id AS credential_user_id,
			credential.algorithm AS credential_algorithm,
			credential.memory_kib AS credential_memory_kib,
			credential.iterations AS credential_iterations,
			credential.parallelism AS credential_parallelism,
			credential.salt AS credential_salt,
			credential.password_hash AS credential_password_hash,
			credential.credential_version AS credential_version,
			credential.password_changed_at AS credential_password_changed_at,
			credential.created_at AS credential_created_at,
			credential.updated_at AS credential_updated_at
		FROM business.user_login_identity AS identity
		JOIN business.user_account AS account ON account.id = identity.user_id
		JOIN business.user_password_credential AS credential ON credential.user_id = account.id
		WHERE identity.identity_type = ?
		  AND identity.normalized_identifier = ?
		  AND identity.verified = TRUE
		  AND account.status = 'active'
		LIMIT 1`, string(identityType), normalizedIdentifier).Scan(&row).Error
	if err != nil {
		return user.AuthenticationRecord{}, mapUserRepositoryError(err)
	}
	if row.AccountID == "" {
		return user.AuthenticationRecord{}, user.ErrUserNotFound
	}
	record := user.AuthenticationRecord{
		Account:    user.Account{ID: row.AccountID, DisplayName: row.AccountDisplayName, Type: user.Type(row.AccountUserType), Status: user.Status(row.AccountStatus), Version: row.AccountVersion, CreatedAt: row.AccountCreatedAt, UpdatedAt: row.AccountUpdatedAt},
		Identity:   user.LoginIdentity{ID: row.IdentityID, UserID: row.IdentityUserID, Type: user.IdentityType(row.IdentityType), NormalizedIdentifier: row.IdentityNormalizedIdentifier, Verified: row.IdentityVerified, VerifiedAt: row.IdentityVerifiedAt, CreatedAt: row.IdentityCreatedAt, UpdatedAt: row.IdentityUpdatedAt},
		Credential: user.PasswordCredential{ID: row.CredentialID, UserID: row.CredentialUserID, Algorithm: row.CredentialAlgorithm, MemoryKiB: row.CredentialMemoryKiB, Iterations: row.CredentialIterations, Parallelism: row.CredentialParallelism, Salt: append([]byte(nil), row.CredentialSalt...), PasswordHash: append([]byte(nil), row.CredentialPasswordHash...), CredentialVersion: row.CredentialVersion, PasswordChangedAt: row.CredentialPasswordChangedAt, CreatedAt: row.CredentialCreatedAt, UpdatedAt: row.CredentialUpdatedAt},
	}
	if err := record.Account.Validate(); err != nil {
		return user.AuthenticationRecord{}, user.ErrPersistence
	}
	if err := record.Identity.Validate(); err != nil {
		return user.AuthenticationRecord{}, user.ErrPersistence
	}
	if err := record.Credential.Validate(); err != nil {
		return user.AuthenticationRecord{}, user.ErrPersistence
	}
	if record.Account.ID != record.Identity.UserID || record.Account.ID != record.Credential.UserID {
		return user.AuthenticationRecord{}, user.ErrPersistence
	}
	return record, nil
}

// mapUserRepositoryError 保留 Context 取消/超时，并将其他数据库错误收敛为不泄漏 SQL 参数的稳定错误。
func mapUserRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return user.ErrPersistence
	}
}
