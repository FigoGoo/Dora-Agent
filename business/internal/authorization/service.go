package authorization

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Clock 为角色分配生命周期冻结一次 UTC 时间。
type Clock interface {
	// Now 返回当前时间，Service 会立即转为 UTC。
	Now() time.Time
}

// IDGenerator 为首次角色授予生成 UUIDv7。
type IDGenerator interface {
	// New 返回新 UUIDv7；失败时不得进入 Repository。
	New() (string, error)
}

// Service 实现最小 Reviewer 角色解析、授予和单向撤销。
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

var _ Resolver = (*Service)(nil)

// NewService 校验依赖并创建 Authorization Service；依赖缺失会阻止 Runtime 或 CLI 启动。
func NewService(repository Repository, clock Clock, ids IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || ids == nil {
		return nil, fmt.Errorf("create authorization service: dependency is nil")
	}
	return &Service{repository: repository, clock: clock, ids: ids}, nil
}

// Resolve 动态解析一次 active 账户的闭集角色，并确定性投影 capability；未知角色失败关闭。
func (service *Service) Resolve(ctx context.Context, userID string) (Projection, error) {
	if !isUUIDv7(userID) {
		return Projection{}, ErrSubjectInactive
	}
	resolution, err := service.repository.ResolveActiveRoles(ctx, userID)
	if err != nil {
		return Projection{}, normalizeRepositoryError(err)
	}
	if !resolution.SubjectActive {
		return Projection{}, ErrSubjectInactive
	}
	roleSet := make(map[string]struct{}, len(resolution.Roles))
	capabilitySet := make(map[string]struct{}, len(resolution.Roles))
	for _, role := range resolution.Roles {
		switch role {
		case RoleSkillReviewer:
			roleSet[string(role)] = struct{}{}
			capabilitySet[string(CapabilitySkillReview)] = struct{}{}
		default:
			// 数据库 CHECK 之外仍在领域层拒绝未知角色，避免损坏数据被静默当成低权限。
			return Projection{}, ErrUnavailable
		}
	}
	projection := Projection{Roles: sortedKeys(roleSet), Capabilities: sortedKeys(capabilitySet)}
	return projection, nil
}

// Grant 校验独立 actor、审批引用和闭集角色，生成 UUIDv7 后由 Repository 原子授予或同义重放。
func (service *Service) Grant(ctx context.Context, command GrantCommand) (MutationResult, error) {
	if !validGrantCommand(command) {
		return MutationResult{}, ErrInvalidCommand
	}
	id, err := service.ids.New()
	if err != nil || !isUUIDv7(id) {
		return MutationResult{}, ErrUnavailable
	}
	now := service.clock.Now().UTC()
	assignment := Assignment{
		ID: id, UserID: command.TargetUserID, Role: command.Role, Status: StatusActive, Version: 1,
		AssignedByUserID: command.ActorUserID, AssignmentReasonCode: command.ReasonCode,
		ApprovalReference: command.ApprovalReference, AssignedAt: now, UpdatedAt: now,
	}
	if err := assignment.Validate(); err != nil {
		return MutationResult{}, ErrInvalidCommand
	}
	result, err := service.repository.Grant(ctx, assignment)
	if err != nil {
		return MutationResult{}, normalizeRepositoryError(err)
	}
	if err := result.Assignment.Validate(); err != nil || result.Assignment.Status != StatusActive {
		return MutationResult{}, ErrUnavailable
	}
	return result, nil
}

// Revoke 校验 assignment/version/审批引用并由 Repository 在统一锁序事务内执行单向 CAS 撤权。
func (service *Service) Revoke(ctx context.Context, command RevokeCommand) (MutationResult, error) {
	if !validRevokeCommand(command) {
		return MutationResult{}, ErrInvalidCommand
	}
	result, err := service.repository.Revoke(ctx, command, service.clock.Now().UTC())
	if err != nil {
		return MutationResult{}, normalizeRepositoryError(err)
	}
	if err := result.Assignment.Validate(); err != nil || result.Assignment.Status != StatusRevoked {
		return MutationResult{}, ErrUnavailable
	}
	return result, nil
}

// validGrantCommand 校验 Grant 的 UUIDv7、actor 分离和稳定文本边界。
func validGrantCommand(command GrantCommand) bool {
	return isUUIDv7(command.TargetUserID) && isUUIDv7(command.ActorUserID) &&
		command.TargetUserID != command.ActorUserID && command.Role == RoleSkillReviewer &&
		validStableValue(command.ReasonCode, 128) && validStableValue(command.ApprovalReference, 160)
}

// validRevokeCommand 校验 Revoke 的 assignment、版本、actor 分离和稳定文本边界。
func validRevokeCommand(command RevokeCommand) bool {
	return isUUIDv7(command.AssignmentID) && isUUIDv7(command.TargetUserID) && isUUIDv7(command.ActorUserID) &&
		command.TargetUserID != command.ActorUserID && command.Role == RoleSkillReviewer && command.ExpectedVersion >= 1 &&
		validStableValue(command.ReasonCode, 128) && validStableValue(command.ApprovalReference, 160)
}

// normalizeRepositoryError 保留取消、超时和稳定授权错误，其他错误不暴露底层数据库细节。
func normalizeRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrSubjectInactive):
		return ErrSubjectInactive
	case errors.Is(err, ErrAssignmentNotFound):
		return ErrAssignmentNotFound
	case errors.Is(err, ErrAssignmentConflict):
		return ErrAssignmentConflict
	default:
		return ErrUnavailable
	}
}

// sortedKeys 把集合转换为非 nil、字典序稳定的前端安全数组。
func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
