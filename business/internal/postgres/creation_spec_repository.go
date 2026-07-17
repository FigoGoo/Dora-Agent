package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreationSpecRepository 使用 GORM 和 Business PostgreSQL 实现 Draft 与保存回执的权威持久化边界。
type CreationSpecRepository struct {
	// db 只在 Repository 内使用，领域与 Transport 层不会接收 GORM 对象。
	db *gorm.DB
}

var _ creationspec.Repository = (*CreationSpecRepository)(nil)

// NewCreationSpecRepository 从 Business PostgreSQL Client 创建 Repository；Client 缺失时失败关闭。
func NewCreationSpecRepository(client *Client) (*CreationSpecRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create creation spec repository: postgres client is nil")
	}
	return &CreationSpecRepository{db: client.db}, nil
}

// FindOwnedProject 用一次查询验证 owner、可写生命周期并返回最小安全 Project 上下文。
// 不存在、跨 owner、已归档或已删除统一隐藏为 ErrNotFound，避免资源枚举。
func (repository *CreationSpecRepository) FindOwnedProject(ctx context.Context, userID string, projectID string) (creationspec.ProjectContext, error) {
	if ctx == nil {
		return creationspec.ProjectContext{}, creationspec.ErrInvalidInput
	}
	var model projectModel
	err := repository.db.WithContext(ctx).
		Select("id", "version", "title").
		Where("id = ? AND owner_user_id = ? AND lifecycle_status = ?", projectID, userID, "active").
		Take(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return creationspec.ProjectContext{}, creationspec.ErrNotFound
		}
		return creationspec.ProjectContext{}, mapCreationSpecRepositoryError(err)
	}
	result := creationspec.ProjectContext{ProjectID: model.ID, Version: model.Version, Title: model.Title}
	if err := creationspec.ValidateProjectContext(result); err != nil {
		return creationspec.ProjectContext{}, creationspec.ErrPersistence
	}
	return result, nil
}

// SaveDraft 以 command_id 争夺 first-write-wins 创建权，并在同一事务保存回执与 Draft。
// 同 command/digest 返回首次 Draft，异 digest 冲突；首次命令还会锁定并复核 Project owner 与版本。
func (repository *CreationSpecRepository) SaveDraft(ctx context.Context, aggregate creationspec.SaveAggregate) (creationspec.SaveResult, error) {
	if ctx == nil {
		return creationspec.SaveResult{}, creationspec.ErrInvalidInput
	}
	draftModel, receiptModel, err := creationSpecModelsFromAggregate(aggregate)
	if err != nil {
		return creationspec.SaveResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return creationspec.SaveResult{}, err
	}

	var result creationspec.SaveResult
	err = repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		inserted := transactionDB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "command_id"}}, DoNothing: true,
		}).Create(&receiptModel)
		if inserted.Error != nil {
			return fmt.Errorf("insert creation spec command receipt: %w", inserted.Error)
		}
		if inserted.RowsAffected == 0 {
			existing, loadErr := loadCreationSpecAggregate(transactionDB, receiptModel.CommandID)
			if loadErr != nil {
				return loadErr
			}
			// command_id 已由首次写入绑定；不同摘要绝不能覆盖或创建第二份 Draft。
			if existing.Receipt.RequestDigest != aggregate.Receipt.RequestDigest {
				return creationspec.ErrIdempotencyConflict
			}
			result = creationspec.SaveResult{Disposition: creationspec.CommandDispositionReplayed, Draft: existing.Draft}
			return nil
		}

		var owned projectModel
		projectErr := transactionDB.Clauses(clause.Locking{Strength: "SHARE"}).
			Select("id", "version", "title").
			Where("id = ? AND owner_user_id = ? AND lifecycle_status = ?", aggregate.Draft.ProjectID, aggregate.Draft.UserID, "active").
			Take(&owned).Error
		if errors.Is(projectErr, gorm.ErrRecordNotFound) {
			return creationspec.ErrNotFound
		}
		if projectErr != nil {
			return fmt.Errorf("lock creation spec project: %w", projectErr)
		}
		if owned.Version != aggregate.Receipt.ExpectedProjectVersion {
			// 生成上下文已过期时回滚此前候选回执，使 Agent 只能用新业务语义重新发起新命令。
			return creationspec.ErrVersionConflict
		}
		if err := transactionDB.Create(&draftModel).Error; err != nil {
			return fmt.Errorf("insert creation spec draft: %w", err)
		}
		result = creationspec.SaveResult{Disposition: creationspec.CommandDispositionCreated, Draft: aggregate.Draft}
		return nil
	})
	if err != nil {
		return creationspec.SaveResult{}, mapCreationSpecRepositoryError(err)
	}
	return result, nil
}

// QueryCommand 按原 command_id、user、project 查询首次回执，再比较原请求摘要并恢复 Draft。
// 查询次数固定为至多两次且不随结果规模增长；跨 owner/project 统一返回 not_found。
func (repository *CreationSpecRepository) QueryCommand(ctx context.Context, query creationspec.QueryCommand) (creationspec.QueryResult, error) {
	if ctx == nil {
		return creationspec.QueryResult{}, creationspec.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return creationspec.QueryResult{}, err
	}
	var receiptModel creationSpecCommandReceiptModel
	err := repository.db.WithContext(ctx).
		Where("command_id = ? AND user_id = ? AND project_id = ?", query.CommandID, query.UserID, query.ProjectID).
		Take(&receiptModel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return creationspec.QueryResult{Status: creationspec.QueryStatusNotFound}, nil
	}
	if err != nil {
		return creationspec.QueryResult{}, mapCreationSpecRepositoryError(err)
	}
	receipt, err := creationSpecReceiptEntity(receiptModel)
	if err != nil {
		return creationspec.QueryResult{}, creationspec.ErrPersistence
	}
	if receipt.RequestDigest != query.RequestDigest {
		return creationspec.QueryResult{Status: creationspec.QueryStatusConflict}, nil
	}
	var draftModel creationSpecModel
	if err := repository.db.WithContext(ctx).Where("id = ?", receipt.CreationSpecID).Take(&draftModel).Error; err != nil {
		return creationspec.QueryResult{}, mapCreationSpecRepositoryError(err)
	}
	draft, err := creationSpecDraftEntity(draftModel)
	if err != nil {
		return creationspec.QueryResult{}, creationspec.ErrPersistence
	}
	if err := creationspec.ValidateAggregate(creationspec.SaveAggregate{Draft: draft, Receipt: receipt}); err != nil {
		return creationspec.QueryResult{}, creationspec.ErrPersistence
	}
	return creationspec.QueryResult{Status: creationspec.QueryStatusCompleted, Draft: &draft}, nil
}

// loadCreationSpecAggregate 用固定两次主键查询恢复首次写入；回执与 Draft 任一缺失都视为持久化不变量损坏。
func loadCreationSpecAggregate(db *gorm.DB, commandID string) (creationspec.SaveAggregate, error) {
	var receiptModel creationSpecCommandReceiptModel
	if err := db.Where("command_id = ?", commandID).Take(&receiptModel).Error; err != nil {
		return creationspec.SaveAggregate{}, fmt.Errorf("read creation spec command receipt: %w", err)
	}
	receipt, err := creationSpecReceiptEntity(receiptModel)
	if err != nil {
		return creationspec.SaveAggregate{}, creationspec.ErrPersistence
	}
	var draftModel creationSpecModel
	if err := db.Where("id = ?", receipt.CreationSpecID).Take(&draftModel).Error; err != nil {
		return creationspec.SaveAggregate{}, fmt.Errorf("read creation spec draft: %w", err)
	}
	draft, err := creationSpecDraftEntity(draftModel)
	if err != nil {
		return creationspec.SaveAggregate{}, creationspec.ErrPersistence
	}
	aggregate := creationspec.SaveAggregate{Draft: draft, Receipt: receipt}
	if err := creationspec.ValidateAggregate(aggregate); err != nil {
		return creationspec.SaveAggregate{}, creationspec.ErrPersistence
	}
	return aggregate, nil
}

// mapCreationSpecRepositoryError 保留稳定领域与 Context 语义，其余数据库细节统一收敛为 ErrPersistence。
func mapCreationSpecRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, creationspec.ErrInvalidInput) || errors.Is(err, creationspec.ErrNotFound) ||
		errors.Is(err, creationspec.ErrVersionConflict) || errors.Is(err, creationspec.ErrIdempotencyConflict) ||
		errors.Is(err, creationspec.ErrPersistence) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w", creationspec.ErrPersistence)
}
