//go:build localsmoke

// Package main 提供只允许 local 环境编译执行的 Creator、Reviewer、Governor 与 Provisioner 角色夹具。
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/clock"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/business/internal/localseed"
	"github.com/FigoGoo/Dora-Agent/business/internal/postgres"
)

const (
	localReviewerReason            = "local_smoke_fixture"
	localReviewerApprovalReference = "local-smoke-reviewer-fixture-v1"
	localGovernorReason            = "local_smoke_governance_fixture"
	localGovernorApprovalReference = "local-smoke-governor-fixture-v1"
)

// reviewerSeederOutput 是仅供 0700 临时目录消费的角色夹具结果；调用方不得把 reason 字段复制到 Evidence。
type reviewerSeederOutput struct {
	// AssignmentID 是正式 Authorization Repository 保存的角色分配 UUIDv7。
	AssignmentID string `json:"assignment_id"`
	// Role 是固定 skill_reviewer。
	Role string `json:"role"`
	// Reason 是固定 local_smoke_fixture。
	Reason string `json:"reason"`
	// GovernorAssignmentID 是正式 Authorization Repository 保存的独立治理角色分配 UUIDv7。
	GovernorAssignmentID string `json:"governor_assignment_id"`
	// GovernorRole 固定为 skill_governor。
	GovernorRole string `json:"governor_role"`
	// GovernorReason 是与 Reviewer 不同的稳定治理夹具原因。
	GovernorReason string `json:"governor_reason"`
	// CreatorUserID 是普通 Skill 创建者 UUIDv7。
	CreatorUserID string `json:"creator_user_id"`
	// ReviewerUserID 是获得正式 Reviewer assignment 的用户 UUIDv7。
	ReviewerUserID string `json:"reviewer_user_id"`
	// GovernorUserID 是只获得正式 Governor assignment 的用户 UUIDv7。
	GovernorUserID string `json:"governor_user_id"`
	// ProvisionerUserID 是与 Creator、Reviewer 分离的本地赋权 actor UUIDv7。
	ProvisionerUserID string `json:"provisioner_user_id"`
}

// smokeUserConfig 保存一个本地 Argon2id 用户夹具的环境输入。
type smokeUserConfig struct {
	// Email 是规范化前邮箱。
	Email string
	// Password 是仅当前进程使用的原始密码。
	Password string
	// DisplayName 是安全展示名。
	DisplayName string
}

// main 执行本地 Reviewer Seeder，并用固定错误阻止 DSN、密码或 SQL 细节进入终端。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "本地 Reviewer 账号与角色准备失败")
		os.Exit(1)
	}
}

// run 校验 build-tag 之外的环境与 DSN 边界，准备四个独立用户并通过正式 Authorization Service 分别授权。
func run() error {
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" {
		return localseed.ErrLocalEnvironmentRequired
	}
	dsn := strings.TrimSpace(os.Getenv("BUSINESS_DATABASE_URL"))
	if !localseed.IsSafeLocalBusinessDSN(dsn) {
		return errors.New("unsafe local Business DSN")
	}
	creator := smokeUserConfig{
		Email: os.Getenv("DORA_SMOKE_USER_EMAIL"), Password: os.Getenv("DORA_SMOKE_USER_PASSWORD"),
		DisplayName: os.Getenv("DORA_SMOKE_USER_DISPLAY_NAME"),
	}
	reviewer := smokeUserConfig{
		Email: os.Getenv("DORA_SMOKE_REVIEWER_EMAIL"), Password: os.Getenv("DORA_SMOKE_REVIEWER_PASSWORD"),
		DisplayName: os.Getenv("DORA_SMOKE_REVIEWER_DISPLAY_NAME"),
	}
	governor := smokeUserConfig{
		Email: os.Getenv("DORA_SMOKE_GOVERNOR_EMAIL"), Password: os.Getenv("DORA_SMOKE_GOVERNOR_PASSWORD"),
		DisplayName: os.Getenv("DORA_SMOKE_GOVERNOR_DISPLAY_NAME"),
	}
	provisioner := smokeUserConfig{
		Email: os.Getenv("DORA_SMOKE_PROVISIONER_EMAIL"), Password: os.Getenv("DORA_SMOKE_PROVISIONER_PASSWORD"),
		DisplayName: os.Getenv("DORA_SMOKE_PROVISIONER_DISPLAY_NAME"),
	}
	if !distinctSmokeUsers(creator, reviewer, governor, provisioner) {
		return localseed.ErrInvalidFixture
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := postgres.Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	userRepository, err := postgres.NewUserRepository(client)
	if err != nil {
		return err
	}
	creatorResult, err := ensureSmokeUser(ctx, userRepository, creator)
	if err != nil {
		return err
	}
	reviewerResult, err := ensureSmokeUser(ctx, userRepository, reviewer)
	if err != nil {
		return err
	}
	governorResult, err := ensureSmokeUser(ctx, userRepository, governor)
	if err != nil {
		return err
	}
	provisionerResult, err := ensureSmokeUser(ctx, userRepository, provisioner)
	if err != nil {
		return err
	}
	if !distinctSmokeUserIDs(creatorResult.UserID, reviewerResult.UserID, governorResult.UserID, provisionerResult.UserID) {
		return localseed.ErrFixtureConflict
	}
	authorizationRepository, err := postgres.NewAuthorizationRepository(client)
	if err != nil {
		return err
	}
	authorizationService, err := authorization.NewService(authorizationRepository, clock.System{}, idgen.UUIDv7{})
	if err != nil {
		return err
	}
	grant, err := authorizationService.Grant(ctx, authorization.GrantCommand{
		TargetUserID: reviewerResult.UserID, ActorUserID: provisionerResult.UserID,
		Role: authorization.RoleSkillReviewer, ReasonCode: localReviewerReason,
		ApprovalReference: localReviewerApprovalReference,
	})
	if err != nil {
		return err
	}
	governorGrant, err := authorizationService.Grant(ctx, authorization.GrantCommand{
		TargetUserID: governorResult.UserID, ActorUserID: provisionerResult.UserID,
		Role: authorization.RoleSkillGovernor, ReasonCode: localGovernorReason,
		ApprovalReference: localGovernorApprovalReference,
	})
	if err != nil {
		return err
	}
	if grant.Assignment.ID == governorGrant.Assignment.ID {
		return localseed.ErrFixtureConflict
	}
	return json.NewEncoder(os.Stdout).Encode(reviewerSeederOutput{
		AssignmentID: grant.Assignment.ID, Role: string(grant.Assignment.Role), Reason: localReviewerReason,
		GovernorAssignmentID: governorGrant.Assignment.ID, GovernorRole: string(governorGrant.Assignment.Role),
		GovernorReason: localGovernorReason, CreatorUserID: creatorResult.UserID, ReviewerUserID: reviewerResult.UserID,
		GovernorUserID: governorResult.UserID, ProvisionerUserID: provisionerResult.UserID,
	})
}

// ensureSmokeUser 复用正常 Argon2id 用户实体和 Repository 幂等准备一个本地身份。
func ensureSmokeUser(ctx context.Context, repository localseed.Repository, input smokeUserConfig) (localseed.Result, error) {
	seeder, err := localseed.New(
		repository, idgen.UUIDv7{}, clock.System{}, rand.Reader,
		localseed.Argon2idHasher{}, auth.Argon2idVerifier{},
		localseed.Config{
			Environment: "local", Email: input.Email, Password: input.Password, DisplayName: input.DisplayName,
		},
	)
	if err != nil {
		return localseed.Result{}, err
	}
	return seeder.Ensure(ctx)
}

// distinctSmokeUsers 在打开数据库前拒绝重复邮箱，数据库写入后仍会再次比较三个 User ID。
func distinctSmokeUsers(users ...smokeUserConfig) bool {
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		normalized, err := auth.NormalizeEmail(user.Email)
		if err != nil || user.Password == "" || strings.TrimSpace(user.DisplayName) == "" {
			return false
		}
		if _, exists := seen[normalized]; exists {
			return false
		}
		seen[normalized] = struct{}{}
	}
	return true
}

// distinctSmokeUserIDs 拒绝数据库中任意身份合并，防止规范化或历史夹具把职责边界折叠到同一账户。
func distinctSmokeUserIDs(userIDs ...string) bool {
	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			return false
		}
		if _, exists := seen[userID]; exists {
			return false
		}
		seen[userID] = struct{}{}
	}
	return true
}
