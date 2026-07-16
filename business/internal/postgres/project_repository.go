package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectRepository 使用 GORM 和 Business PostgreSQL 实现 Project 快速创建的原子持久化边界。
type ProjectRepository struct {
	// db 是只允许本 Repository 使用的 GORM 连接；业务层不会接收该对象。
	db *gorm.DB
}

var _ project.Repository = (*ProjectRepository)(nil)
var _ project.DispatchRepository = (*ProjectRepository)(nil)
var _ project.AgentSessionAccessRepository = (*ProjectRepository)(nil)

// NewProjectRepository 从 Business PostgreSQL Client 创建 Repository；Client 未初始化时返回错误并阻止隐式降级。
func NewProjectRepository(client *Client) (*ProjectRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create project repository: postgres client is nil")
	}
	return &ProjectRepository{db: client.db}, nil
}

// CreateQuick 原子写入 Project、Receipt、Binding 和 Outbox；同键同摘要返回原回执，异摘要返回 ErrIdempotencyConflict。
func (r *ProjectRepository) CreateQuick(ctx context.Context, aggregate project.QuickCreateAggregate) (project.QuickCreateResult, error) {
	models, err := quickCreateModelsFromAggregate(aggregate)
	if err != nil {
		return project.QuickCreateResult{}, err
	}

	var result project.QuickCreateResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先用唯一幂等范围争夺创建权：未获得创建权的并发事务不会写入任何候选 Project，避免孤儿事实。
		insertReceipt := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "owner_user_id"}, {Name: "command_type"}, {Name: "key_digest"}},
				DoNothing: true,
			}).
			Create(&models.Receipt)
		if insertReceipt.Error != nil {
			return fmt.Errorf("insert project creation receipt: %w", insertReceipt.Error)
		}

		if insertReceipt.RowsAffected == 0 {
			var existing projectCreationReceiptModel
			if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
				Where("owner_user_id = ? AND command_type = ? AND key_digest = ?", models.Receipt.OwnerUserID, models.Receipt.CommandType, models.Receipt.KeyDigest).
				Take(&existing).Error; err != nil {
				return fmt.Errorf("read existing project creation receipt: %w", err)
			}

			// 唯一键相同但语义摘要不同表示客户端复用了幂等键，必须保留原 Project 且稳定返回冲突。
			if !bytes.Equal(existing.SemanticDigest, models.Receipt.SemanticDigest) {
				return project.ErrIdempotencyConflict
			}
			receipt, err := projectCreationReceiptEntity(existing)
			if err != nil {
				return err
			}
			result = project.ResultFromReceipt(receipt, true)
			return nil
		}

		// 获得幂等键创建权后，Project、绑定和加密 Outbox 必须与回执处于同一事务；任一步失败会回滚全部四类事实。
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := transactionDB.Create(&models.Project).Error; err != nil {
			return fmt.Errorf("insert project: %w", err)
		}
		if err := transactionDB.Create(&models.Binding).Error; err != nil {
			return fmt.Errorf("insert project session binding: %w", err)
		}
		if err := transactionDB.Create(&models.Outbox).Error; err != nil {
			return fmt.Errorf("insert project session outbox: %w", err)
		}

		result = project.ResultFromReceipt(aggregate.Receipt, false)
		return nil
	})
	if err != nil {
		return project.QuickCreateResult{}, mapProjectRepositoryError(err)
	}
	return result, nil
}

// FindOwnedByID 按 Project ID 和可信所有者做单次查询；不存在或所有者不匹配时统一返回 ErrProjectNotFound。
func (r *ProjectRepository) FindOwnedByID(ctx context.Context, projectID string, ownerUserID string) (project.Project, error) {
	var model projectModel
	if err := r.db.WithContext(ctx).
		Where("id = ? AND owner_user_id = ?", projectID, ownerUserID).
		Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return project.Project{}, project.ErrProjectNotFound
		}
		return project.Project{}, mapProjectRepositoryError(err)
	}
	entity, err := projectEntity(model)
	if err != nil {
		return project.Project{}, mapProjectRepositoryError(err)
	}
	return entity, nil
}

// FindBootstrapOwnedByID 通过一次显式 JOIN 读取 Project 与默认 Session 绑定，避免页面 Bootstrap 产生 N+1。
func (r *ProjectRepository) FindBootstrapOwnedByID(ctx context.Context, projectID string, ownerUserID string) (project.BootstrapResult, error) {
	var record projectBootstrapReadDTO
	err := r.db.WithContext(ctx).
		Table("business.project AS project").
		Select(`project.id AS project_id,
			project.title AS title,
			project.lifecycle_status AS lifecycle_status,
			project.recent_run_status AS recent_run_status,
			project.initial_prompt_status AS initial_prompt_status,
			binding.provisioning_status AS provisioning_status,
			binding.agent_session_id AS agent_session_id,
			binding.agent_input_id AS agent_input_id,
			binding.last_error_code AS last_error_code,
			project.updated_at AS project_updated_at,
			binding.updated_at AS binding_updated_at`).
		Joins("JOIN business.project_session_binding AS binding ON binding.project_id = project.id").
		Where("project.id = ? AND project.owner_user_id = ?", projectID, ownerUserID).
		Take(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 不存在与越权共享同一错误，避免按 Project ID 枚举其他用户资源。
			return project.BootstrapResult{}, project.ErrProjectNotFound
		}
		return project.BootstrapResult{}, mapProjectRepositoryError(err)
	}
	result, err := projectBootstrapResult(record)
	if err != nil {
		return project.BootstrapResult{}, mapProjectRepositoryError(err)
	}
	return result, nil
}

// FindReadyAgentSessionAccess 用一次显式 JOIN 同时核对 owner、Project 可读状态、ready Binding 与完整 Session ID。
func (r *ProjectRepository) FindReadyAgentSessionAccess(ctx context.Context, ownerUserID string, agentSessionID string) (project.AgentSessionAccess, error) {
	var record agentSessionAccessReadDTO
	err := r.db.WithContext(ctx).
		Table("business.project AS project").
		Select(`project.id AS project_id, binding.agent_session_id AS agent_session_id`).
		Joins("JOIN business.project_session_binding AS binding ON binding.project_id = project.id").
		Where(`project.owner_user_id = ?
			AND project.lifecycle_status IN ?
			AND binding.provisioning_status = ?
			AND binding.agent_session_id = ?`, ownerUserID,
			[]string{string(project.LifecycleStatusActive), string(project.LifecycleStatusArchived)},
			string(project.ProvisioningStatusReady), agentSessionID).
		Take(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 缺失、越权、未 ready 和 Project 已不可读统一收敛，避免枚举跨用户 Agent Session。
			return project.AgentSessionAccess{}, project.ErrAgentSessionNotFound
		}
		return project.AgentSessionAccess{}, mapProjectRepositoryError(err)
	}
	access := project.AgentSessionAccess{ProjectID: record.ProjectID, AgentSessionID: record.AgentSessionID}
	if err := access.Validate(); err != nil || access.AgentSessionID != agentSessionID {
		return project.AgentSessionAccess{}, mapProjectRepositoryError(project.ErrAgentSessionNotFound)
	}
	return access, nil
}

// ClaimNext 使用 PostgreSQL 行锁领取一个到期命令；过期 processing 可被新 Fence 接管，耗尽命令原子转为 dead。
func (r *ProjectRepository) ClaimNext(ctx context.Context, leaseOwner string, now time.Time, leaseUntil time.Time) (project.SessionOutbox, error) {
	if strings.TrimSpace(leaseOwner) == "" || len(leaseOwner) > 128 || now.IsZero() || !leaseUntil.After(now) {
		return project.SessionOutbox{}, project.ErrInvalidQuickCreate
	}
	var claimed project.SessionOutbox
	exhausted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model projectSessionOutboxModel
		err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("((status IN ? AND available_at <= ?) OR (status = ? AND lease_expires_at <= ?))",
				[]string{string(project.OutboxStatusPending), string(project.OutboxStatusRetry)}, now,
				string(project.OutboxStatusProcessing), now).
			Order("available_at ASC, id ASC").
			Take(&model).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select project session outbox claim: %w", err)
		}
		// retry 与过期 processing 都可能已经被 Agent 提交但响应未知；新 Lease 必须先 Query 原命令。
		recoveryRequired := model.Status == string(project.OutboxStatusRetry) || model.Status == string(project.OutboxStatusProcessing)

		if model.AttemptCount >= model.MaxAttempts {
			// 崩溃可能遗留最后一次 processing Lease；新 Owner 只负责收敛 dead，不再越过冻结尝试预算调用 Agent。
			if err := markDeadInTransaction(tx, model, "AGENT_SESSION_ATTEMPTS_EXCEEDED", now); err != nil {
				return err
			}
			exhausted = true
			return nil
		}

		update := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionOutboxModel{}).
			Where("id = ? AND status = ? AND lease_version = ?", model.ID, model.Status, model.LeaseVersion).
			Updates(map[string]any{
				"status": string(project.OutboxStatusProcessing), "lease_owner": leaseOwner,
				"lease_version": model.LeaseVersion + 1, "lease_expires_at": leaseUntil,
				"attempt_count": model.AttemptCount + 1, "updated_at": now,
			})
		if update.Error != nil {
			return fmt.Errorf("claim project session outbox: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}
		model.Status = string(project.OutboxStatusProcessing)
		model.LeaseOwner = stringValuePointer(leaseOwner)
		model.LeaseVersion++
		model.LeaseExpiresAt = timeValuePointer(leaseUntil)
		model.AttemptCount++
		model.UpdatedAt = now
		entity, err := projectSessionOutboxEntity(model)
		if err != nil {
			return err
		}
		entity.RecoveryRequired = recoveryRequired
		claimed = entity
		return nil
	})
	if err != nil {
		return project.SessionOutbox{}, mapProjectRepositoryError(err)
	}
	if exhausted || claimed.ID == "" {
		return project.SessionOutbox{}, project.ErrOutboxEmpty
	}
	return claimed, nil
}

// MarkDelivered 在同一事务内确认 Agent Receipt、更新绑定和 Project，并清除 Business 暂存 Prompt 密文。
func (r *ProjectRepository) MarkDelivered(ctx context.Context, outbox project.SessionOutbox, receipt project.EnsureSessionReceipt, deliveredAt time.Time) error {
	if err := outbox.Validate(); err != nil || outbox.Status != project.OutboxStatusProcessing || deliveredAt.IsZero() ||
		receipt.CommandID != outbox.ID || receipt.RequestDigest != outbox.RequestDigest || receipt.SessionID == "" {
		return project.ErrInvalidQuickCreate
	}
	if outbox.SchemaVersion == project.EnsureSessionSchemaVersionV2 {
		if receipt.SkillSnapshotDigest != outbox.SkillSnapshotDigest || receipt.SkillCount != outbox.SkillCount {
			return project.ErrInvalidQuickCreate
		}
	} else if receipt.SkillSnapshotDigest != (project.Digest{}) || receipt.SkillCount != 0 {
		return project.ErrInvalidQuickCreate
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		outboxUpdates := map[string]any{
			"status": string(project.OutboxStatusDelivered), "delivered_at": deliveredAt,
			"lease_owner": nil, "lease_expires_at": nil, "updated_at": deliveredAt,
		}
		if outbox.HasInitialPrompt || outbox.SchemaVersion == project.EnsureSessionSchemaVersionV2 {
			outboxUpdates["payload_encryption_algorithm"] = nil
			outboxUpdates["payload_key_version"] = nil
			outboxUpdates["payload_nonce"] = nil
			outboxUpdates["payload_ciphertext"] = nil
			outboxUpdates["payload_cleared_at"] = deliveredAt
		}
		updated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionOutboxModel{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_version = ?", outbox.ID, string(project.OutboxStatusProcessing), *outbox.LeaseOwner, outbox.LeaseVersion).
			Updates(outboxUpdates)
		if updated.Error != nil {
			return fmt.Errorf("mark project session outbox delivered: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}

		bindingWhere := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionBindingModel{}).
			Where("command_id = ? AND request_digest = ? AND provisioning_status IN ?", outbox.ID, outbox.RequestDigest[:],
				[]string{string(project.ProvisioningStatusPending), string(project.ProvisioningStatusReconciling)})
		if outbox.SchemaVersion == project.EnsureSessionSchemaVersionV2 {
			bindingWhere = bindingWhere.Where(
				"request_schema_version = ? AND skill_snapshot_digest = ? AND skill_count = ?",
				"ensure_project_session.v2", outbox.SkillSnapshotDigest[:], outbox.SkillCount,
			)
		}
		bindingUpdated := bindingWhere.
			Updates(map[string]any{
				"agent_session_id": receipt.SessionID, "agent_input_id": receipt.InputID,
				"provisioning_status": string(project.ProvisioningStatusReady), "last_error_code": nil,
				"version": gorm.Expr("version + 1"), "updated_at": deliveredAt,
			})
		if bindingUpdated.Error != nil {
			return fmt.Errorf("mark project session binding ready: %w", bindingUpdated.Error)
		}
		if bindingUpdated.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}

		if outbox.HasInitialPrompt {
			projectUpdated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectModel{}).
				Where("id = ? AND owner_user_id = ? AND initial_prompt_status = ?", outbox.AggregateID, outbox.OwnerUserID, string(project.InitialPromptStatusPending)).
				Updates(map[string]any{
					"initial_prompt_status": string(project.InitialPromptStatusAccepted),
					"version":               gorm.Expr("version + 1"), "updated_at": deliveredAt,
				})
			if projectUpdated.Error != nil {
				return fmt.Errorf("mark project initial prompt accepted: %w", projectUpdated.Error)
			}
			if projectUpdated.RowsAffected != 1 {
				return project.ErrOutboxLeaseLost
			}
		}
		return nil
	})
	return mapProjectRepositoryErrorOrNil(err)
}

// MarkRetry 以当前 Lease/Fence 释放命令并标记 Binding 正在核对，后续仍使用原 command_id。
func (r *ProjectRepository) MarkRetry(ctx context.Context, outbox project.SessionOutbox, availableAt time.Time, updatedAt time.Time) error {
	if err := outbox.Validate(); err != nil || outbox.Status != project.OutboxStatusProcessing || !availableAt.After(updatedAt) {
		return project.ErrInvalidQuickCreate
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionOutboxModel{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_version = ?", outbox.ID, string(project.OutboxStatusProcessing), *outbox.LeaseOwner, outbox.LeaseVersion).
			Updates(map[string]any{
				"status": string(project.OutboxStatusRetry), "available_at": availableAt,
				"lease_owner": nil, "lease_expires_at": nil, "updated_at": updatedAt,
			})
		if updated.Error != nil {
			return fmt.Errorf("mark project session outbox retry: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}
		bindingUpdated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionBindingModel{}).
			Where("command_id = ? AND provisioning_status IN ?", outbox.ID,
				[]string{string(project.ProvisioningStatusPending), string(project.ProvisioningStatusReconciling)}).
			Updates(map[string]any{
				"provisioning_status": string(project.ProvisioningStatusReconciling),
				"last_error_code":     "AGENT_SESSION_UNKNOWN_OUTCOME", "version": gorm.Expr("version + 1"), "updated_at": updatedAt,
			})
		if bindingUpdated.Error != nil {
			return fmt.Errorf("mark project session binding reconciling: %w", bindingUpdated.Error)
		}
		if bindingUpdated.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}
		return nil
	})
	return mapProjectRepositoryErrorOrNil(err)
}

// MarkDead 以稳定错误码终止当前 Lease，并原子阻止该 Project 继续伪装为可自动恢复状态。
func (r *ProjectRepository) MarkDead(ctx context.Context, outbox project.SessionOutbox, stableErrorCode string, updatedAt time.Time) error {
	if err := outbox.Validate(); err != nil || outbox.Status != project.OutboxStatusProcessing || !validStableErrorCode(stableErrorCode) || updatedAt.IsZero() {
		return project.ErrInvalidQuickCreate
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := projectSessionOutboxModel{ID: outbox.ID, Status: string(outbox.Status), LeaseOwner: outbox.LeaseOwner,
			LeaseVersion: outbox.LeaseVersion, AggregateID: outbox.AggregateID, HasInitialPrompt: outbox.HasInitialPrompt}
		return markDeadInTransaction(tx, model, stableErrorCode, updatedAt)
	})
	return mapProjectRepositoryErrorOrNil(err)
}

// markDeadInTransaction 在调用方事务内同时收敛 Outbox、Binding 与可选首 Prompt 状态。
func markDeadInTransaction(tx *gorm.DB, model projectSessionOutboxModel, stableErrorCode string, updatedAt time.Time) error {
	where := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionOutboxModel{}).
		Where("id = ? AND status = ? AND lease_version = ?", model.ID, model.Status, model.LeaseVersion)
	if model.Status == string(project.OutboxStatusProcessing) && model.LeaseOwner != nil {
		where = where.Where("lease_owner = ?", *model.LeaseOwner)
	}
	updated := where.Updates(map[string]any{
		"status": string(project.OutboxStatusDead), "lease_owner": nil, "lease_expires_at": nil, "updated_at": updatedAt,
	})
	if updated.Error != nil {
		return fmt.Errorf("mark project session outbox dead: %w", updated.Error)
	}
	if updated.RowsAffected != 1 {
		return project.ErrOutboxLeaseLost
	}
	bindingUpdated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectSessionBindingModel{}).
		Where("command_id = ? AND provisioning_status IN ?", model.ID,
			[]string{string(project.ProvisioningStatusPending), string(project.ProvisioningStatusReconciling)}).
		Updates(map[string]any{
			"provisioning_status": string(project.ProvisioningStatusBlocked), "last_error_code": stableErrorCode,
			"version": gorm.Expr("version + 1"), "updated_at": updatedAt,
		})
	if bindingUpdated.Error != nil {
		return fmt.Errorf("mark project session binding blocked: %w", bindingUpdated.Error)
	}
	if bindingUpdated.RowsAffected != 1 {
		return project.ErrOutboxLeaseLost
	}
	if model.HasInitialPrompt {
		projectUpdated := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&projectModel{}).
			Where("id = ? AND initial_prompt_status = ?", model.AggregateID, string(project.InitialPromptStatusPending)).
			Updates(map[string]any{
				"initial_prompt_status": string(project.InitialPromptStatusFailed),
				"version":               gorm.Expr("version + 1"), "updated_at": updatedAt,
			})
		if projectUpdated.Error != nil {
			return fmt.Errorf("mark project initial prompt failed: %w", projectUpdated.Error)
		}
		if projectUpdated.RowsAffected != 1 {
			return project.ErrOutboxLeaseLost
		}
	}
	return nil
}

// validStableErrorCode 只允许短小的 ASCII 大写代码进入数据库，阻止 RPC 原文、地址或用户内容落库。
func validStableErrorCode(code string) bool {
	if code == "" || len(code) > 128 {
		return false
	}
	for _, character := range code {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// stringValuePointer 返回字符串副本地址，避免局部复用改变模型中的 Lease Owner。
func stringValuePointer(value string) *string { return &value }

// timeValuePointer 返回时间副本地址，避免局部复用改变模型中的 Lease 期限。
func timeValuePointer(value time.Time) *time.Time { return &value }

// mapProjectRepositoryErrorOrNil 允许事务成功返回 nil，其余错误统一通过稳定边界映射。
func mapProjectRepositoryErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return mapProjectRepositoryError(err)
}

// mapProjectRepositoryError 将 PostgreSQL/GORM 原错收敛为稳定领域错误，同时保留调用取消、超时和既有业务错误语义。
func mapProjectRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, project.ErrIdempotencyConflict):
		return project.ErrIdempotencyConflict
	case errors.Is(err, project.ErrProjectNotFound):
		return project.ErrProjectNotFound
	case errors.Is(err, project.ErrOutboxEmpty):
		return project.ErrOutboxEmpty
	case errors.Is(err, project.ErrOutboxLeaseLost):
		return project.ErrOutboxLeaseLost
	default:
		// 未分类数据库错误可能包含 SQL、DSN、地址或敏感参数，只返回无底层 cause 的稳定边界错误。
		return project.ErrPersistence
	}
}
