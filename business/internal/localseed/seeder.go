// Package localseed 提供只允许 local 环境使用的正常认证账号 Fixture 写入能力。
package localseed

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/user"
	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB   = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltBytes   = 16
	argonHashBytes   = 32
)

var (
	// ErrLocalEnvironmentRequired 表示调用方尝试在非 local 环境准备已知测试账号。
	ErrLocalEnvironmentRequired = errors.New("local smoke seed requires local environment")
	// ErrInvalidFixture 表示本地邮箱、密码或展示名不满足正常认证实体边界。
	ErrInvalidFixture = errors.New("invalid local smoke user fixture")
	// ErrFixtureConflict 表示同邮箱已存在，但现有密码与本地 Fixture 不一致。
	ErrFixtureConflict = errors.New("local smoke user fixture conflicts with existing account")
	// ErrUnavailable 表示 ID、随机源、Argon2id 或持久化依赖不可用。
	ErrUnavailable = errors.New("local smoke seed unavailable")
)

// Repository 是本地 Seeder 消费的正常用户 Repository 边界，不绕过生产实体与事务校验。
type Repository interface {
	FindAuthenticationRecord(ctx context.Context, identityType user.IdentityType, normalizedIdentifier string) (user.AuthenticationRecord, error)
	CreateAuthenticationRecord(ctx context.Context, record user.AuthenticationRecord) error
}

// IDGenerator 为账户、登录身份和密码凭据生成 UUIDv7。
type IDGenerator interface {
	New() (string, error)
}

// Clock 冻结 Fixture 的 UTC 创建时间。
type Clock interface {
	Now() time.Time
}

// PasswordHasher 为本地已知密码生成随机盐 Argon2id 摘要。
type PasswordHasher interface {
	Hash(password string, random io.Reader) (salt []byte, passwordHash []byte, err error)
}

// PasswordVerifier 校验重复执行时已有账号仍绑定同一本地密码。
type PasswordVerifier interface {
	Verify(password string, credential user.PasswordCredential) (bool, error)
}

// Config 冻结 local 环境和单个正常登录 Fixture。
type Config struct {
	Environment string
	Email       string
	Password    string
	DisplayName string
}

// Result 返回可用于后续 Smoke 关联的非敏感用户标识与是否新建。
type Result struct {
	UserID  string
	Created bool
}

// Seeder 通过正式 User Repository 幂等准备一个 Argon2id 账号。
type Seeder struct {
	repository Repository
	ids        IDGenerator
	clock      Clock
	random     io.Reader
	hasher     PasswordHasher
	verifier   PasswordVerifier
	config     Config
}

// New 校验环境和依赖；任何非 local 环境都在打开数据库前失败关闭。
func New(
	repository Repository,
	ids IDGenerator,
	clock Clock,
	random io.Reader,
	hasher PasswordHasher,
	verifier PasswordVerifier,
	config Config,
) (*Seeder, error) {
	if strings.TrimSpace(config.Environment) != "local" {
		return nil, ErrLocalEnvironmentRequired
	}
	normalizedEmail, err := auth.NormalizeEmail(config.Email)
	if err != nil || len(config.Password) < 12 || len(config.Password) > 1024 || strings.TrimSpace(config.DisplayName) == "" {
		return nil, ErrInvalidFixture
	}
	if repository == nil || ids == nil || clock == nil || random == nil || hasher == nil || verifier == nil {
		return nil, ErrUnavailable
	}
	config.Email = normalizedEmail
	return &Seeder{
		repository: repository, ids: ids, clock: clock, random: random,
		hasher: hasher, verifier: verifier, config: config,
	}, nil
}

// Ensure 已存在同义账号时校验密码并重放；不存在时用正式三表事务创建，竞态失败后再查询收敛。
func (seeder *Seeder) Ensure(ctx context.Context) (Result, error) {
	existing, err := seeder.repository.FindAuthenticationRecord(ctx, user.IdentityTypeEmail, seeder.config.Email)
	if err == nil {
		return seeder.existingResult(existing)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Result{}, err
	}
	if !errors.Is(err, user.ErrUserNotFound) {
		return Result{}, ErrUnavailable
	}

	record, err := seeder.newRecord()
	if err != nil {
		return Result{}, err
	}
	if err := seeder.repository.CreateAuthenticationRecord(ctx, record); err == nil {
		return Result{UserID: record.Account.ID, Created: true}, nil
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Result{}, err
	}
	// 并发 Seeder 可能已经写入同一邮箱；再次走正常认证读取并核对密码，不覆盖现有账户。
	existing, err = seeder.repository.FindAuthenticationRecord(ctx, user.IdentityTypeEmail, seeder.config.Email)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, err
		}
		return Result{}, ErrUnavailable
	}
	return seeder.existingResult(existing)
}

// newRecord 生成三类逻辑关联事实；敏感密码与盐只存在于当前调用栈和返回实体。
func (seeder *Seeder) newRecord() (user.AuthenticationRecord, error) {
	ids := make([]string, 3)
	for index := range ids {
		id, err := seeder.ids.New()
		if err != nil {
			return user.AuthenticationRecord{}, ErrUnavailable
		}
		ids[index] = id
	}
	salt, passwordHash, err := seeder.hasher.Hash(seeder.config.Password, seeder.random)
	if err != nil {
		return user.AuthenticationRecord{}, ErrUnavailable
	}
	now := seeder.clock.Now().UTC()
	verifiedAt := now
	record := user.AuthenticationRecord{
		Account: user.Account{
			ID: ids[0], DisplayName: seeder.config.DisplayName, Type: user.TypePersonal,
			Status: user.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Identity: user.LoginIdentity{
			ID: ids[1], UserID: ids[0], Type: user.IdentityTypeEmail,
			NormalizedIdentifier: seeder.config.Email, Verified: true, VerifiedAt: &verifiedAt,
			CreatedAt: now, UpdatedAt: now,
		},
		Credential: user.PasswordCredential{
			ID: ids[2], UserID: ids[0], Algorithm: "argon2id",
			MemoryKiB: argonMemoryKiB, Iterations: argonIterations, Parallelism: argonParallelism,
			Salt: salt, PasswordHash: passwordHash, CredentialVersion: 1,
			PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := record.Account.Validate(); err != nil {
		return user.AuthenticationRecord{}, ErrInvalidFixture
	}
	if err := record.Identity.Validate(); err != nil {
		return user.AuthenticationRecord{}, ErrInvalidFixture
	}
	if err := record.Credential.Validate(); err != nil {
		return user.AuthenticationRecord{}, ErrUnavailable
	}
	return record, nil
}

// existingResult 只允许同一邮箱、active 账号和同一密码重放，避免 Seeder 改写环境中已有身份。
func (seeder *Seeder) existingResult(record user.AuthenticationRecord) (Result, error) {
	if record.Identity.NormalizedIdentifier != seeder.config.Email || record.Account.Status != user.StatusActive {
		return Result{}, ErrFixtureConflict
	}
	matched, err := seeder.verifier.Verify(seeder.config.Password, record.Credential)
	if err != nil {
		return Result{}, ErrUnavailable
	}
	if !matched {
		return Result{}, ErrFixtureConflict
	}
	return Result{UserID: record.Account.ID, Created: false}, nil
}

// Argon2idHasher 使用生产登录校验兼容的有界参数生成随机盐和 256-bit 摘要。
type Argon2idHasher struct{}

// Hash 从安全随机源读取 128-bit Salt，并执行 Argon2id。
func (Argon2idHasher) Hash(password string, random io.Reader) ([]byte, []byte, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, nil, err
	}
	passwordHash := argon2.IDKey(
		[]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, argonHashBytes,
	)
	return salt, passwordHash, nil
}
