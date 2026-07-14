// Command business-role-admin 提供不注册 HTTP 的窄化生产角色授予与撤权命令。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/clock"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/business/internal/postgres"
)

// roleAdminInput 是从非敏感命令行参数解析的窄化 Grant/Revoke 输入。
type roleAdminInput struct {
	// Action 是 grant 或 revoke。
	Action string
	// TargetUserID 是被赋权或撤权用户 UUIDv7。
	TargetUserID string
	// ActorUserID 是可追责的独立 active 操作人 UUIDv7。
	ActorUserID string
	// Role 是固定 skill_reviewer。
	Role string
	// ReasonCode 是稳定原因代码。
	ReasonCode string
	// ApprovalReference 是外部审批或工单引用。
	ApprovalReference string
	// AssignmentID 是 revoke 必填的角色分配 UUIDv7。
	AssignmentID string
	// ExpectedVersion 是 revoke 必填的 active assignment 版本。
	ExpectedVersion int64
}

// roleAdminOutput 是成功时允许写入标准输出的非敏感稳定结果。
type roleAdminOutput struct {
	// Action 是本次 grant 或 revoke。
	Action string `json:"action"`
	// AssignmentID 是权威角色分配 UUIDv7。
	AssignmentID string `json:"assignment_id"`
	// TargetUserID 是被操作用户 UUIDv7。
	TargetUserID string `json:"target_user_id"`
	// Role 是固定 skill_reviewer。
	Role string `json:"role"`
	// Status 是操作后的 active 或 revoked。
	Status string `json:"status"`
	// Version 是操作后的 assignment 版本。
	Version int64 `json:"version"`
	// ApprovalReference 是本次外部审批引用。
	ApprovalReference string `json:"approval_reference"`
	// RequestID 是本次 CLI 执行生成的 UUIDv7。
	RequestID string `json:"request_id"`
}

// main 执行受控角色运维，并用固定错误避免 DSN 或数据库细节进入终端。
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Business 角色运维失败")
		os.Exit(1)
	}
}

// run 从环境读取专用 DSN、解析非秘密命令并调用正式 Authorization Service。
func run(args []string, output io.Writer) error {
	input, err := parseRoleAdminInput(args)
	if err != nil {
		return err
	}
	dsn := strings.TrimSpace(os.Getenv("DORA_ROLE_ADMIN_POSTGRES_DSN"))
	if dsn == "" {
		return errors.New("role admin postgres DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := postgres.Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	repository, err := postgres.NewAuthorizationRepository(client)
	if err != nil {
		return err
	}
	service, err := authorization.NewService(repository, clock.System{}, idgen.UUIDv7{})
	if err != nil {
		return err
	}
	var result authorization.MutationResult
	switch input.Action {
	case "grant":
		result, err = service.Grant(ctx, authorization.GrantCommand{
			TargetUserID: input.TargetUserID, ActorUserID: input.ActorUserID,
			Role: authorization.RoleKey(input.Role), ReasonCode: input.ReasonCode,
			ApprovalReference: input.ApprovalReference,
		})
	case "revoke":
		result, err = service.Revoke(ctx, authorization.RevokeCommand{
			AssignmentID: input.AssignmentID, TargetUserID: input.TargetUserID, ActorUserID: input.ActorUserID,
			Role: authorization.RoleKey(input.Role), ExpectedVersion: input.ExpectedVersion,
			ReasonCode: input.ReasonCode, ApprovalReference: input.ApprovalReference,
		})
	default:
		return authorization.ErrInvalidCommand
	}
	if err != nil {
		return err
	}
	requestID, err := (idgen.UUIDv7{}).New()
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(roleAdminOutput{
		Action: input.Action, AssignmentID: result.Assignment.ID, TargetUserID: result.Assignment.UserID,
		Role: string(result.Assignment.Role), Status: string(result.Assignment.Status), Version: result.Assignment.Version,
		ApprovalReference: input.ApprovalReference, RequestID: requestID,
	})
}

// parseRoleAdminInput 使用独立 FlagSet 严格解析非敏感输入；未知参数或动作特有字段不完整时失败。
func parseRoleAdminInput(args []string) (roleAdminInput, error) {
	flags := flag.NewFlagSet("business-role-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var input roleAdminInput
	flags.StringVar(&input.Action, "action", "", "grant or revoke")
	flags.StringVar(&input.TargetUserID, "target-user-id", "", "target user UUIDv7")
	flags.StringVar(&input.ActorUserID, "actor-user-id", "", "actor user UUIDv7")
	flags.StringVar(&input.Role, "role", "", "fixed role key")
	flags.StringVar(&input.ReasonCode, "reason", "", "stable reason code")
	flags.StringVar(&input.ApprovalReference, "approval-reference", "", "external approval reference")
	flags.StringVar(&input.AssignmentID, "assignment-id", "", "assignment UUIDv7 for revoke")
	flags.Int64Var(&input.ExpectedVersion, "expected-version", 0, "assignment version for revoke")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return roleAdminInput{}, authorization.ErrInvalidCommand
	}
	input.Action = strings.TrimSpace(input.Action)
	if input.Action != "grant" && input.Action != "revoke" {
		return roleAdminInput{}, authorization.ErrInvalidCommand
	}
	if input.Action == "grant" && (input.AssignmentID != "" || input.ExpectedVersion != 0) {
		return roleAdminInput{}, authorization.ErrInvalidCommand
	}
	if input.Action == "revoke" && (input.AssignmentID == "" || input.ExpectedVersion < 1) {
		return roleAdminInput{}, authorization.ErrInvalidCommand
	}
	if input.TargetUserID == "" || input.ActorUserID == "" || input.Role == "" || input.ReasonCode == "" || input.ApprovalReference == "" {
		return roleAdminInput{}, authorization.ErrInvalidCommand
	}
	return input, nil
}
