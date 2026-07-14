package postgres

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
)

// userRoleAssignmentModel 是 Business Reviewer 角色分配持久化模型，不创建数据库物理外键。
type userRoleAssignmentModel struct {
	// ID 是应用生成的 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// UserID 是被赋权用户逻辑关联标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// RoleKey 是闭集角色键。
	RoleKey string `gorm:"column:role_key"`
	// Status 是 active 或 revoked。
	Status string `gorm:"column:status"`
	// Version 是撤权 CAS 版本。
	Version int64 `gorm:"column:version"`
	// AssignedByUserID 是授予操作人逻辑关联标识。
	AssignedByUserID string `gorm:"column:assigned_by_user_id;type:uuid"`
	// AssignmentReasonCode 是稳定授予原因。
	AssignmentReasonCode string `gorm:"column:assignment_reason_code"`
	// ApprovalReference 是授予外部审批引用。
	ApprovalReference string `gorm:"column:approval_reference"`
	// AssignedAt 是 UTC 授予时间。
	AssignedAt time.Time `gorm:"column:assigned_at"`
	// RevokedByUserID 是可选撤权操作人逻辑关联标识。
	RevokedByUserID *string `gorm:"column:revoked_by_user_id;type:uuid"`
	// RevokeReasonCode 是可选稳定撤权原因。
	RevokeReasonCode *string `gorm:"column:revoke_reason_code"`
	// RevocationApprovalReference 是可选撤权外部审批引用。
	RevocationApprovalReference *string `gorm:"column:revocation_approval_reference"`
	// RevokedAt 是可选 UTC 撤权时间。
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	// UpdatedAt 是最近状态更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Business 角色分配权威表名。
func (userRoleAssignmentModel) TableName() string { return "business.user_role_assignment" }

// userRoleAssignmentModelFromEntity 校验并映射角色分配，不接受畸形审计事实。
func userRoleAssignmentModelFromEntity(entity authorization.Assignment) (userRoleAssignmentModel, error) {
	if err := entity.Validate(); err != nil {
		return userRoleAssignmentModel{}, fmt.Errorf("map authorization assignment to persistence model: %w", err)
	}
	return userRoleAssignmentModel{
		ID: entity.ID, UserID: entity.UserID, RoleKey: string(entity.Role), Status: string(entity.Status), Version: entity.Version,
		AssignedByUserID: entity.AssignedByUserID, AssignmentReasonCode: entity.AssignmentReasonCode,
		ApprovalReference: entity.ApprovalReference, AssignedAt: entity.AssignedAt.UTC(),
		RevokedByUserID: cloneAuthorizationStringPointer(entity.RevokedByUserID), RevokeReasonCode: cloneAuthorizationStringPointer(entity.RevokeReasonCode),
		RevocationApprovalReference: cloneAuthorizationStringPointer(entity.RevocationApprovalReference),
		RevokedAt:                   cloneTimePointer(entity.RevokedAt), UpdatedAt: entity.UpdatedAt.UTC(),
	}, nil
}

// userRoleAssignmentEntity 将持久化记录恢复为领域事实并再次验证全部状态不变量。
func userRoleAssignmentEntity(model userRoleAssignmentModel) (authorization.Assignment, error) {
	entity := authorization.Assignment{
		ID: model.ID, UserID: model.UserID, Role: authorization.RoleKey(model.RoleKey),
		Status: authorization.AssignmentStatus(model.Status), Version: model.Version,
		AssignedByUserID: model.AssignedByUserID, AssignmentReasonCode: model.AssignmentReasonCode,
		ApprovalReference: model.ApprovalReference, AssignedAt: model.AssignedAt,
		RevokedByUserID: cloneAuthorizationStringPointer(model.RevokedByUserID), RevokeReasonCode: cloneAuthorizationStringPointer(model.RevokeReasonCode),
		RevocationApprovalReference: cloneAuthorizationStringPointer(model.RevocationApprovalReference),
		RevokedAt:                   cloneTimePointer(model.RevokedAt), UpdatedAt: model.UpdatedAt,
	}
	if err := entity.Validate(); err != nil {
		return authorization.Assignment{}, fmt.Errorf("map persistence model to authorization assignment: %w", err)
	}
	return entity, nil
}

// cloneAuthorizationStringPointer 深拷贝授权领域的可选字符串，避免与其他持久化模型形成隐式依赖。
func cloneAuthorizationStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// cloneTimePointer 深拷贝可选时间，避免持久化模型与领域对象共享可变指针。
func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
