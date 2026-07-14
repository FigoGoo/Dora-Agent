// Package authorization 定义 Business Skill 审核与治理 RBAC 的领域边界。
package authorization

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	// ErrInvalidCommand 表示角色授予或撤销命令不满足 UUIDv7、角色、原因或审批引用约束。
	ErrInvalidCommand = errors.New("invalid authorization command")
	// ErrSubjectInactive 表示目标用户不存在或不是 active，不能产生可信权限投影。
	ErrSubjectInactive = errors.New("authorization subject is inactive")
	// ErrAssignmentNotFound 表示指定角色分配不存在。
	ErrAssignmentNotFound = errors.New("authorization assignment not found")
	// ErrAssignmentConflict 表示角色分配已存在但语义不同，或撤权版本已经变化。
	ErrAssignmentConflict = errors.New("authorization assignment conflict")
	// ErrUnavailable 表示权威角色存储不可用或包含无法安全解释的事实。
	ErrUnavailable = errors.New("authorization unavailable")
)

const (
	// RoleSkillReviewer 是允许持久化的 Skill 审核角色。
	RoleSkillReviewer RoleKey = "skill_reviewer"
	// RoleSkillGovernor 是允许持久化的 Skill 治理角色。
	RoleSkillGovernor RoleKey = "skill_governor"
	// CapabilitySkillReview 是 skill_reviewer 投影的审核能力。
	CapabilitySkillReview CapabilityKey = "skill.review"
	// CapabilitySkillGovern 是 skill_governor 投影的治理能力。
	CapabilitySkillGovern CapabilityKey = "skill.govern"
	// StatusActive 表示角色分配当前生效。
	StatusActive AssignmentStatus = "active"
	// StatusRevoked 表示角色分配已经单向撤销。
	StatusRevoked AssignmentStatus = "revoked"
)

// RoleKey 是 Business 代码闭集维护的角色稳定键，不能从 Header、Body 或环境变量扩展。
type RoleKey string

// CapabilityKey 是由受控角色映射产生的能力稳定键，数据库不直接保存该值。
type CapabilityKey string

// AssignmentStatus 是角色分配单向生命周期状态。
type AssignmentStatus string

// Projection 是 active 用户在一次权威解析时得到的稳定非空数组投影。
type Projection struct {
	// Roles 是去重并按字典序稳定排序的角色键。
	Roles []string
	// Capabilities 是去重并按字典序稳定排序的能力键。
	Capabilities []string
}

// RoleResolution 是 Repository 单次集合查询返回的 active 账户与角色事实。
type RoleResolution struct {
	// SubjectActive 表示查询锚定的用户账户存在且为 active。
	SubjectActive bool
	// Roles 是数据库中 active assignment 的角色稳定键。
	Roles []RoleKey
}

// Assignment 是角色授予与单向撤销的权威领域事实。
type Assignment struct {
	// ID 是应用生成的角色分配 UUIDv7。
	ID string
	// UserID 是被授予角色的用户 UUIDv7。
	UserID string
	// Role 是闭集角色键。
	Role RoleKey
	// Status 是 active 或 revoked。
	Status AssignmentStatus
	// Version 是角色分配 CAS 版本。
	Version int64
	// AssignedByUserID 是执行授予的独立 active 操作人。
	AssignedByUserID string
	// AssignmentReasonCode 是不含自由文本的稳定授予原因。
	AssignmentReasonCode string
	// ApprovalReference 是授予对应的外部审批稳定引用。
	ApprovalReference string
	// AssignedAt 是角色首次授予时间。
	AssignedAt time.Time
	// RevokedByUserID 是执行撤权的独立 active 操作人，仅 revoked 时存在。
	RevokedByUserID *string
	// RevokeReasonCode 是稳定撤权原因，仅 revoked 时存在。
	RevokeReasonCode *string
	// RevocationApprovalReference 是撤权外部审批稳定引用，仅 revoked 时存在。
	RevocationApprovalReference *string
	// RevokedAt 是角色撤销时间，仅 revoked 时存在。
	RevokedAt *time.Time
	// UpdatedAt 是角色分配最近状态时间。
	UpdatedAt time.Time
}

// Validate 校验角色分配的 UUIDv7、闭集角色、审计字段和单向状态不变量。
func (assignment Assignment) Validate() error {
	if !isUUIDv7(assignment.ID) || !isUUIDv7(assignment.UserID) || !isUUIDv7(assignment.AssignedByUserID) ||
		assignment.UserID == assignment.AssignedByUserID || !validRole(assignment.Role) ||
		assignment.Version < 1 || !validStableValue(assignment.AssignmentReasonCode, 128) ||
		!validStableValue(assignment.ApprovalReference, 160) || assignment.AssignedAt.IsZero() ||
		assignment.UpdatedAt.Before(assignment.AssignedAt) {
		return ErrInvalidCommand
	}
	if assignment.Status == StatusActive {
		if assignment.RevokedByUserID != nil || assignment.RevokeReasonCode != nil ||
			assignment.RevocationApprovalReference != nil || assignment.RevokedAt != nil {
			return ErrInvalidCommand
		}
		return nil
	}
	if assignment.Status != StatusRevoked || assignment.RevokedByUserID == nil || assignment.RevokeReasonCode == nil ||
		assignment.RevocationApprovalReference == nil || assignment.RevokedAt == nil ||
		!isUUIDv7(*assignment.RevokedByUserID) || *assignment.RevokedByUserID == assignment.UserID ||
		!validStableValue(*assignment.RevokeReasonCode, 128) ||
		!validStableValue(*assignment.RevocationApprovalReference, 160) || assignment.RevokedAt.Before(assignment.AssignedAt) {
		return ErrInvalidCommand
	}
	return nil
}

// validRole 只接受代码与 Migration 共同冻结的 Skill 审核、治理角色闭集。
func validRole(role RoleKey) bool {
	return role == RoleSkillReviewer || role == RoleSkillGovernor
}

// GrantCommand 是受控 Role Admin 或 local Seeder 提交的授予语义。
type GrantCommand struct {
	// TargetUserID 是被授予角色的 active 用户 UUIDv7。
	TargetUserID string
	// ActorUserID 是与目标不同的 active 操作人 UUIDv7。
	ActorUserID string
	// Role 是闭集角色键。
	Role RoleKey
	// ReasonCode 是稳定授予原因。
	ReasonCode string
	// ApprovalReference 是授予对应的外部审批引用。
	ApprovalReference string
}

// RevokeCommand 是带 assignment 与预期版本围栏的撤权语义。
type RevokeCommand struct {
	// AssignmentID 是待撤销角色分配 UUIDv7。
	AssignmentID string
	// TargetUserID 是被撤权用户 UUIDv7。
	TargetUserID string
	// ActorUserID 是与目标不同的 active 操作人 UUIDv7。
	ActorUserID string
	// Role 是待撤销闭集角色键。
	Role RoleKey
	// ExpectedVersion 是调用方观察到的 active assignment 版本。
	ExpectedVersion int64
	// ReasonCode 是稳定撤权原因。
	ReasonCode string
	// ApprovalReference 是撤权对应的外部审批引用。
	ApprovalReference string
}

// MutationResult 是 Grant/Revoke 首次写入或同义重放的安全结果。
type MutationResult struct {
	// Assignment 是写入或重放后的权威角色分配。
	Assignment Assignment
	// IdempotentReplay 表示没有创建或修改新事实。
	IdempotentReplay bool
}

// Repository 定义角色解析、授予和撤销使用的最小持久化能力。
type Repository interface {
	// ResolveActiveRoles 用一次集合查询读取 active 账户及其 active roles。
	ResolveActiveRoles(ctx context.Context, userID string) (RoleResolution, error)
	// Grant 在固定锁序事务内创建角色分配或返回同义 active 分配。
	Grant(ctx context.Context, assignment Assignment) (MutationResult, error)
	// Revoke 在固定锁序事务内 CAS 撤销角色分配或返回同义 revoked 分配。
	Revoke(ctx context.Context, command RevokeCommand, revokedAt time.Time) (MutationResult, error)
}

// Resolver 是 Auth Service 每次登录与 Session Resolve 使用的动态授权边界。
type Resolver interface {
	// Resolve 返回 active 用户的稳定角色与能力投影；未知或畸形事实失败关闭。
	Resolve(ctx context.Context, userID string) (Projection, error)
}

// isUUIDv7 校验标识必须是应用侧使用的规范小写 UUIDv7。
func isUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// validStableValue 只接受首尾无空白且不含控制字符的短稳定代码或审批引用。
func validStableValue(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
