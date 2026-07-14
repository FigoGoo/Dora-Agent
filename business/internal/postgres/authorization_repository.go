package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AuthorizationRepository 使用 GORM 实现一次动态解析和固定锁序的角色授予、撤销。
type AuthorizationRepository struct {
	// db 只在 Repository 内部使用，不向 Auth、CLI 或 HTTP 暴露。
	db *gorm.DB
}

var _ authorization.Repository = (*AuthorizationRepository)(nil)

// authorizationResolutionRow 承接 active 账户与去重角色 JSON 数组的一次集合查询。
type authorizationResolutionRow struct {
	// UserID 是 active 查询锚点用户。
	UserID string `gorm:"column:user_id"`
	// RoleKeysJSON 是按稳定顺序聚合的角色键数组。
	RoleKeysJSON []byte `gorm:"column:role_keys_json"`
}

// lockedAuthorizationUserRow 承接按 UUIDv7 排序锁定的 active actor/target 用户。
type lockedAuthorizationUserRow struct {
	// ID 是已锁定 active 用户 UUIDv7。
	ID string `gorm:"column:id"`
}

// NewAuthorizationRepository 从 Business PostgreSQL Client 创建授权 Repository；Client 未初始化时失败关闭。
func NewAuthorizationRepository(client *Client) (*AuthorizationRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create authorization repository: postgres client is nil")
	}
	return &AuthorizationRepository{db: client.db}, nil
}

// ResolveActiveRoles 使用一次 active-account 锚定的 LEFT JOIN 聚合查询返回稳定角色集合。
func (repository *AuthorizationRepository) ResolveActiveRoles(ctx context.Context, userID string) (authorization.RoleResolution, error) {
	var row authorizationResolutionRow
	result := repository.db.WithContext(ctx).Raw(`
		SELECT
			account.id AS user_id,
			COALESCE(
				jsonb_agg(DISTINCT assignment.role_key ORDER BY assignment.role_key)
					FILTER (WHERE assignment.id IS NOT NULL),
				'[]'::jsonb
			) AS role_keys_json
		FROM business.user_account AS account
		LEFT JOIN business.user_role_assignment AS assignment
		  ON assignment.user_id = account.id
		 AND assignment.status = 'active'
		WHERE account.id = ?
		  AND account.status = 'active'
		GROUP BY account.id`, userID).Scan(&row)
	if result.Error != nil {
		return authorization.RoleResolution{}, mapAuthorizationRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 || row.UserID != userID {
		return authorization.RoleResolution{SubjectActive: false, Roles: []authorization.RoleKey{}}, nil
	}
	var encodedRoles []string
	if len(row.RoleKeysJSON) == 0 || json.Unmarshal(row.RoleKeysJSON, &encodedRoles) != nil || encodedRoles == nil {
		return authorization.RoleResolution{}, authorization.ErrUnavailable
	}
	roles := make([]authorization.RoleKey, len(encodedRoles))
	for index, role := range encodedRoles {
		roles[index] = authorization.RoleKey(role)
	}
	return authorization.RoleResolution{SubjectActive: true, Roles: roles}, nil
}

// Grant 按 account 后 assignment 的统一锁序创建角色分配；active 同义事实安全重放。
func (repository *AuthorizationRepository) Grant(ctx context.Context, assignment authorization.Assignment) (authorization.MutationResult, error) {
	model, err := userRoleAssignmentModelFromEntity(assignment)
	if err != nil || assignment.Status != authorization.StatusActive {
		return authorization.MutationResult{}, authorization.ErrInvalidCommand
	}
	var result authorization.MutationResult
	err = repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveAuthorizationUsers(tx, assignment.AssignedByUserID, assignment.UserID); err != nil {
			return err
		}
		var existing userRoleAssignmentModel
		read := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND role_key = ? AND status = 'active'", assignment.UserID, assignment.Role).
			Take(&existing)
		switch {
		case read.Error == nil:
			entity, mapErr := userRoleAssignmentEntity(existing)
			if mapErr != nil {
				return mapErr
			}
			if !sameGrantSemantics(entity, assignment) {
				return authorization.ErrAssignmentConflict
			}
			result = authorization.MutationResult{Assignment: entity, IdempotentReplay: true}
			return nil
		case !errors.Is(read.Error, gorm.ErrRecordNotFound):
			return read.Error
		}
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&model).Error; err != nil {
			return err
		}
		result = authorization.MutationResult{Assignment: assignment, IdempotentReplay: false}
		return nil
	})
	if err != nil {
		return authorization.MutationResult{}, mapAuthorizationRepositoryError(err)
	}
	return result, nil
}

// Revoke 按 account 后 assignment 的统一锁序 CAS 撤权；已撤权且语义完全一致时安全重放。
func (repository *AuthorizationRepository) Revoke(ctx context.Context, command authorization.RevokeCommand, revokedAt time.Time) (authorization.MutationResult, error) {
	var result authorization.MutationResult
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveAuthorizationUsers(tx, command.ActorUserID, command.TargetUserID); err != nil {
			return err
		}
		var existing userRoleAssignmentModel
		read := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", command.AssignmentID).Take(&existing)
		if errors.Is(read.Error, gorm.ErrRecordNotFound) {
			return authorization.ErrAssignmentNotFound
		}
		if read.Error != nil {
			return read.Error
		}
		entity, err := userRoleAssignmentEntity(existing)
		if err != nil {
			return err
		}
		if entity.UserID != command.TargetUserID || entity.Role != command.Role {
			return authorization.ErrAssignmentConflict
		}
		if entity.Status == authorization.StatusRevoked {
			if !sameRevokeSemantics(entity, command) {
				return authorization.ErrAssignmentConflict
			}
			result = authorization.MutationResult{Assignment: entity, IdempotentReplay: true}
			return nil
		}
		if entity.Status != authorization.StatusActive || entity.Version != command.ExpectedVersion {
			return authorization.ErrAssignmentConflict
		}
		revokedAt = revokedAt.UTC()
		update := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&userRoleAssignmentModel{}).
			Where("id = ? AND user_id = ? AND role_key = ? AND status = 'active' AND version = ?",
				command.AssignmentID, command.TargetUserID, command.Role, command.ExpectedVersion).
			Updates(map[string]any{
				"status": authorization.StatusRevoked, "version": gorm.Expr("version + 1"),
				"revoked_by_user_id": command.ActorUserID, "revoke_reason_code": command.ReasonCode,
				"revocation_approval_reference": command.ApprovalReference,
				"revoked_at":                    revokedAt, "updated_at": revokedAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return authorization.ErrAssignmentConflict
		}
		actor := command.ActorUserID
		reason := command.ReasonCode
		approval := command.ApprovalReference
		entity.Status = authorization.StatusRevoked
		entity.Version++
		entity.RevokedByUserID = &actor
		entity.RevokeReasonCode = &reason
		entity.RevocationApprovalReference = &approval
		entity.RevokedAt = &revokedAt
		entity.UpdatedAt = revokedAt
		if err := entity.Validate(); err != nil {
			return err
		}
		result = authorization.MutationResult{Assignment: entity, IdempotentReplay: false}
		return nil
	})
	if err != nil {
		return authorization.MutationResult{}, mapAuthorizationRepositoryError(err)
	}
	return result, nil
}

// lockActiveAuthorizationUsers 按 UUIDv7 稳定顺序锁定独立 actor 与 target active 账户。
func lockActiveAuthorizationUsers(tx *gorm.DB, actorUserID string, targetUserID string) error {
	if actorUserID == targetUserID {
		return authorization.ErrInvalidCommand
	}
	ids := []string{actorUserID, targetUserID}
	sort.Strings(ids)
	var rows []lockedAuthorizationUserRow
	result := tx.Raw(`
		SELECT id
		FROM business.user_account
		WHERE id IN ?
		  AND status = 'active'
		ORDER BY id
		FOR UPDATE`, ids).Scan(&rows)
	if result.Error != nil {
		return result.Error
	}
	if len(rows) != 2 || rows[0].ID != ids[0] || rows[1].ID != ids[1] {
		return authorization.ErrSubjectInactive
	}
	return nil
}

// sameGrantSemantics 确保 active unique 命中时只有 actor、原因和审批引用完全一致才重放。
func sameGrantSemantics(existing authorization.Assignment, candidate authorization.Assignment) bool {
	return existing.Status == authorization.StatusActive && existing.UserID == candidate.UserID && existing.Role == candidate.Role &&
		existing.AssignedByUserID == candidate.AssignedByUserID && existing.AssignmentReasonCode == candidate.AssignmentReasonCode &&
		existing.ApprovalReference == candidate.ApprovalReference
}

// sameRevokeSemantics 确保 response 丢失后的撤权只按首次 actor、原因、审批引用和版本重放。
func sameRevokeSemantics(existing authorization.Assignment, command authorization.RevokeCommand) bool {
	return existing.Status == authorization.StatusRevoked && existing.Version == command.ExpectedVersion+1 &&
		existing.UserID == command.TargetUserID && existing.Role == command.Role &&
		existing.RevokedByUserID != nil && *existing.RevokedByUserID == command.ActorUserID &&
		existing.RevokeReasonCode != nil && *existing.RevokeReasonCode == command.ReasonCode &&
		existing.RevocationApprovalReference != nil && *existing.RevocationApprovalReference == command.ApprovalReference
}

// mapAuthorizationRepositoryError 保留稳定领域错误并收敛 PostgreSQL 细节。
func mapAuthorizationRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, authorization.ErrInvalidCommand):
		return authorization.ErrInvalidCommand
	case errors.Is(err, authorization.ErrSubjectInactive):
		return authorization.ErrSubjectInactive
	case errors.Is(err, authorization.ErrAssignmentNotFound):
		return authorization.ErrAssignmentNotFound
	case errors.Is(err, authorization.ErrAssignmentConflict):
		return authorization.ErrAssignmentConflict
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.ConstraintName == "uq_user_role_assignment__active_user_role" {
		return authorization.ErrAssignmentConflict
	}
	return authorization.ErrUnavailable
}
