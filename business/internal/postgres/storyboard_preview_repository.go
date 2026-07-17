package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const storyboardPreviewPlanningContextSQL = `
SELECT
    project_record.id AS project_id,
    project_record.version AS project_version,
    project_record.title AS project_title,
    creation_spec_record.id AS creation_spec_id,
    creation_spec_record.project_id AS creation_spec_project_id,
    creation_spec_record.user_id AS creation_spec_user_id,
    creation_spec_record.status AS creation_spec_status,
    creation_spec_record.version AS creation_spec_version,
    creation_spec_record.schema_version AS creation_spec_schema_version,
    creation_spec_record.content_json AS creation_spec_content_json,
    creation_spec_record.content_digest AS creation_spec_content_digest,
    creation_spec_record.source_tool_call_id AS creation_spec_source_tool_call_id,
    creation_spec_record.source_prompt_version AS creation_spec_source_prompt_version,
    creation_spec_record.source_validator_version AS creation_spec_source_validator_version,
    creation_spec_record.created_at AS creation_spec_created_at,
    creation_spec_record.updated_at AS creation_spec_updated_at
FROM business.project AS project_record
JOIN business.creation_spec AS creation_spec_record
  ON creation_spec_record.project_id = project_record.id
 AND creation_spec_record.user_id = project_record.owner_user_id
WHERE project_record.id = ?
  AND project_record.owner_user_id = ?
  AND project_record.lifecycle_status = 'active'
  AND creation_spec_record.id = ?
  AND creation_spec_record.status = 'draft'
  AND creation_spec_record.schema_version = 'creation_spec.draft.v1'`

// StoryboardPreviewRepository 使用 GORM 和 Business PostgreSQL 实现联合上下文、Draft 与命令回执边界。
type StoryboardPreviewRepository struct {
	db *gorm.DB
}

var _ storyboardpreview.Repository = (*StoryboardPreviewRepository)(nil)

// NewStoryboardPreviewRepository 从 Business PostgreSQL Client 创建 Repository；Client 缺失时失败关闭。
func NewStoryboardPreviewRepository(client *Client) (*StoryboardPreviewRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create storyboard preview repository: postgres client is nil")
	}
	return &StoryboardPreviewRepository{db: client.db}, nil
}

// storyboardPreviewPlanningContextRecord 是单次 JOIN 查询的专用读取 DTO。
type storyboardPreviewPlanningContextRecord struct {
	// ProjectID 是 Owner 校验后的 Project 标识。
	ProjectID string `gorm:"column:project_id"`
	// ProjectVersion 是保存时必须复核的 Project 乐观版本。
	ProjectVersion int64 `gorm:"column:project_version"`
	// ProjectTitle 是允许返回给 Agent 的安全项目标题。
	ProjectTitle string `gorm:"column:project_title"`
	// CreationSpecID 是指定的 CreationSpec Draft 标识。
	CreationSpecID string `gorm:"column:creation_spec_id"`
	// CreationSpecProjectID 是 CreationSpec 冻结的 Project 逻辑标识。
	CreationSpecProjectID string `gorm:"column:creation_spec_project_id"`
	// CreationSpecUserID 是 CreationSpec 冻结的 Owner 逻辑标识。
	CreationSpecUserID string `gorm:"column:creation_spec_user_id"`
	// CreationSpecStatus 是 CreationSpec Draft 状态。
	CreationSpecStatus string `gorm:"column:creation_spec_status"`
	// CreationSpecVersion 是 CreationSpec Draft 版本。
	CreationSpecVersion int64 `gorm:"column:creation_spec_version"`
	// CreationSpecSchemaVersion 是 CreationSpec 内容契约版本。
	CreationSpecSchemaVersion string `gorm:"column:creation_spec_schema_version"`
	// CreationSpecContentJSON 是严格 CreationSpec 内容 JSON。
	CreationSpecContentJSON jsonbValue `gorm:"column:creation_spec_content_json"`
	// CreationSpecContentDigest 是 CreationSpec Canonical Content 摘要。
	CreationSpecContentDigest []byte `gorm:"column:creation_spec_content_digest"`
	// CreationSpecSourceToolCallID 是产生 CreationSpec 的 Tool Call 标识。
	CreationSpecSourceToolCallID string `gorm:"column:creation_spec_source_tool_call_id"`
	// CreationSpecSourcePromptVersion 是产生 CreationSpec 的 Prompt 版本。
	CreationSpecSourcePromptVersion string `gorm:"column:creation_spec_source_prompt_version"`
	// CreationSpecSourceValidatorVersion 是产生 CreationSpec 的 Validator 版本。
	CreationSpecSourceValidatorVersion string `gorm:"column:creation_spec_source_validator_version"`
	// CreationSpecCreatedAt 是 CreationSpec Draft 创建时间。
	CreationSpecCreatedAt time.Time `gorm:"column:creation_spec_created_at"`
	// CreationSpecUpdatedAt 是 CreationSpec Draft 更新时间。
	CreationSpecUpdatedAt time.Time `gorm:"column:creation_spec_updated_at"`
}

// FindPlanningContext 用一次 JOIN 同时校验 Project Owner、生命周期和指定 CreationSpec Draft。
// 不存在、跨 Owner、归档或非 Draft 统一隐藏为 ErrNotFound，避免资源枚举。
func (repository *StoryboardPreviewRepository) FindPlanningContext(ctx context.Context, query storyboardpreview.ContextQuery) (storyboardpreview.PlanningContext, error) {
	if ctx == nil {
		return storyboardpreview.PlanningContext{}, storyboardpreview.ErrInvalidInput
	}
	return loadStoryboardPlanningContext(repository.db.WithContext(ctx), query, false)
}

// SaveDraft 以 command_id 争夺 first-write-wins 创建权，并在同一事务复核 Project 与 CreationSpec 快照。
// 同 command/digest 返回首次 Draft；异 digest、版本漂移或候选 phase/DAG 失效均不产生第二份业务事实。
func (repository *StoryboardPreviewRepository) SaveDraft(ctx context.Context, aggregate storyboardpreview.SaveAggregate) (storyboardpreview.SaveResult, error) {
	if ctx == nil {
		return storyboardpreview.SaveResult{}, storyboardpreview.ErrInvalidInput
	}
	draftModel, receiptModel, err := storyboardPreviewModelsFromAggregate(aggregate)
	if err != nil {
		return storyboardpreview.SaveResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return storyboardpreview.SaveResult{}, err
	}

	var result storyboardpreview.SaveResult
	err = repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		inserted := transactionDB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "command_id"}}, DoNothing: true,
		}).Create(&receiptModel)
		if inserted.Error != nil {
			return fmt.Errorf("insert storyboard preview command receipt: %w", inserted.Error)
		}
		if inserted.RowsAffected == 0 {
			existing, loadErr := loadStoryboardPreviewAggregate(transactionDB, receiptModel.CommandID)
			if loadErr != nil {
				return loadErr
			}
			// command_id 已由首次写入绑定；不同摘要不得覆盖，也不得创建第二份 Draft。
			if existing.Receipt.RequestDigest != aggregate.Receipt.RequestDigest {
				return storyboardpreview.ErrIdempotencyConflict
			}
			result = storyboardpreview.SaveResult{
				Disposition: storyboardpreview.CommandDispositionReplayed, Draft: existing.Draft,
			}
			return nil
		}

		planningContext, contextErr := loadStoryboardPlanningContext(transactionDB, storyboardpreview.ContextQuery{
			UserID: aggregate.Draft.UserID, ProjectID: aggregate.Draft.ProjectID,
			CreationSpecRef: aggregate.Draft.CreationSpecRef,
		}, true)
		if contextErr != nil {
			return contextErr
		}
		if planningContext.ProjectVersion != aggregate.Receipt.ExpectedProjectVersion {
			// Project 语义已变化时回滚先写入的命令回执，要求 Agent 以新上下文创建新命令。
			return storyboardpreview.ErrProjectVersionConflict
		}
		actualReference := storyboardpreview.CreationSpecRef{
			ID: planningContext.CreationSpec.ID, Version: planningContext.CreationSpec.Version,
			ContentDigest: planningContext.CreationSpec.ContentDigest,
		}
		if actualReference != aggregate.Draft.CreationSpecRef {
			// 已授权 CreationSpec 的版本或摘要漂移不能静默替换生成输入。
			return storyboardpreview.ErrCreationSpecVersionConflict
		}
		if err := storyboardpreview.ValidateAgainstCreationSpec(aggregate.Draft.Content, planningContext.CreationSpec.Content); err != nil {
			return err
		}
		if err := transactionDB.Create(&draftModel).Error; err != nil {
			return fmt.Errorf("insert storyboard preview draft: %w", err)
		}
		result = storyboardpreview.SaveResult{
			Disposition: storyboardpreview.CommandDispositionCreated, Draft: aggregate.Draft,
		}
		return nil
	})
	if err != nil {
		return storyboardpreview.SaveResult{}, mapStoryboardPreviewRepositoryError(err)
	}
	return result, nil
}

// QueryCommand 按原 command_id、user、project 查询首次回执，再比较摘要并恢复 Draft。
// 查询次数固定为至多两次，跨 Owner 或 Project 统一返回 not_found。
func (repository *StoryboardPreviewRepository) QueryCommand(ctx context.Context, query storyboardpreview.QueryCommand) (storyboardpreview.QueryResult, error) {
	if ctx == nil {
		return storyboardpreview.QueryResult{}, storyboardpreview.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return storyboardpreview.QueryResult{}, err
	}
	var receiptModel storyboardPreviewCommandReceiptModel
	err := repository.db.WithContext(ctx).
		Where("command_id = ? AND user_id = ? AND project_id = ?", query.CommandID, query.UserID, query.ProjectID).
		Take(&receiptModel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storyboardpreview.QueryResult{Status: storyboardpreview.QueryStatusNotFound}, nil
	}
	if err != nil {
		return storyboardpreview.QueryResult{}, mapStoryboardPreviewRepositoryError(err)
	}
	receipt, err := storyboardPreviewReceiptEntity(receiptModel)
	if err != nil {
		return storyboardpreview.QueryResult{}, storyboardpreview.ErrPersistence
	}
	if receipt.RequestDigest != query.RequestDigest {
		return storyboardpreview.QueryResult{Status: storyboardpreview.QueryStatusConflict}, nil
	}
	var draftModel storyboardPreviewDraftModel
	if err := repository.db.WithContext(ctx).Where("id = ?", receipt.StoryboardPreviewID).Take(&draftModel).Error; err != nil {
		return storyboardpreview.QueryResult{}, mapStoryboardPreviewRepositoryError(err)
	}
	draft, err := storyboardPreviewDraftEntity(draftModel)
	if err != nil {
		return storyboardpreview.QueryResult{}, storyboardpreview.ErrPersistence
	}
	if err := storyboardpreview.ValidateAggregate(storyboardpreview.SaveAggregate{Draft: draft, Receipt: receipt}); err != nil {
		return storyboardpreview.QueryResult{}, storyboardpreview.ErrPersistence
	}
	return storyboardpreview.QueryResult{Status: storyboardpreview.QueryStatusCompleted, Draft: &draft}, nil
}

// loadStoryboardPlanningContext 执行固定一次 JOIN；保存事务可选择 FOR SHARE 锁定两条业务基线记录。
func loadStoryboardPlanningContext(db *gorm.DB, query storyboardpreview.ContextQuery, lock bool) (storyboardpreview.PlanningContext, error) {
	statement := storyboardPreviewPlanningContextSQL
	if lock {
		statement += " FOR SHARE OF project_record, creation_spec_record"
	}
	var record storyboardPreviewPlanningContextRecord
	queryResult := db.Raw(statement, query.ProjectID, query.UserID, query.CreationSpecRef.ID).Scan(&record)
	if queryResult.Error != nil {
		return storyboardpreview.PlanningContext{}, mapStoryboardPreviewRepositoryError(queryResult.Error)
	}
	if queryResult.RowsAffected != 1 {
		return storyboardpreview.PlanningContext{}, storyboardpreview.ErrNotFound
	}
	creationSpecModel := creationSpecModel{
		ID: record.CreationSpecID, ProjectID: record.CreationSpecProjectID, UserID: record.CreationSpecUserID,
		Status: record.CreationSpecStatus, Version: record.CreationSpecVersion,
		SchemaVersion: record.CreationSpecSchemaVersion, ContentJSON: record.CreationSpecContentJSON,
		ContentDigest: record.CreationSpecContentDigest, SourceToolCallID: record.CreationSpecSourceToolCallID,
		SourcePromptVersion:    record.CreationSpecSourcePromptVersion,
		SourceValidatorVersion: record.CreationSpecSourceValidatorVersion,
		CreatedAt:              record.CreationSpecCreatedAt, UpdatedAt: record.CreationSpecUpdatedAt,
	}
	creationSpecDraft, err := creationSpecDraftEntity(creationSpecModel)
	if err != nil {
		return storyboardpreview.PlanningContext{}, storyboardpreview.ErrPersistence
	}
	creationSpecDigest, err := storyboardpreview.DigestFromBytes(creationSpecDraft.ContentDigest.Bytes())
	if err != nil {
		return storyboardpreview.PlanningContext{}, storyboardpreview.ErrPersistence
	}
	result := storyboardpreview.PlanningContext{
		ProjectID: record.ProjectID, ProjectVersion: record.ProjectVersion, ProjectTitle: record.ProjectTitle,
		CreationSpec: storyboardpreview.CreationSpecSnapshot{
			ID: creationSpecDraft.ID, ProjectID: creationSpecDraft.ProjectID, UserID: creationSpecDraft.UserID,
			Status: creationSpecDraft.Status, Version: creationSpecDraft.Version,
			SchemaVersion: creationSpecDraft.SchemaVersion, Content: creationSpecDraft.Content,
			ContentDigest: creationSpecDigest,
		},
	}
	if err := storyboardpreview.ValidatePlanningContext(result); err != nil {
		return storyboardpreview.PlanningContext{}, storyboardpreview.ErrPersistence
	}
	return result, nil
}

// loadStoryboardPreviewAggregate 用固定两次主键查询恢复首次写入的不变聚合。
func loadStoryboardPreviewAggregate(db *gorm.DB, commandID string) (storyboardpreview.SaveAggregate, error) {
	var receiptModel storyboardPreviewCommandReceiptModel
	if err := db.Where("command_id = ?", commandID).Take(&receiptModel).Error; err != nil {
		return storyboardpreview.SaveAggregate{}, fmt.Errorf("read storyboard preview command receipt: %w", err)
	}
	receipt, err := storyboardPreviewReceiptEntity(receiptModel)
	if err != nil {
		return storyboardpreview.SaveAggregate{}, storyboardpreview.ErrPersistence
	}
	var draftModel storyboardPreviewDraftModel
	if err := db.Where("id = ?", receipt.StoryboardPreviewID).Take(&draftModel).Error; err != nil {
		return storyboardpreview.SaveAggregate{}, fmt.Errorf("read storyboard preview draft: %w", err)
	}
	draft, err := storyboardPreviewDraftEntity(draftModel)
	if err != nil {
		return storyboardpreview.SaveAggregate{}, storyboardpreview.ErrPersistence
	}
	aggregate := storyboardpreview.SaveAggregate{Draft: draft, Receipt: receipt}
	if err := storyboardpreview.ValidateAggregate(aggregate); err != nil {
		return storyboardpreview.SaveAggregate{}, storyboardpreview.ErrPersistence
	}
	return aggregate, nil
}

// mapStoryboardPreviewRepositoryError 保留稳定领域与 Context 语义，其余数据库细节收敛为 ErrPersistence。
func mapStoryboardPreviewRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, storyboardpreview.ErrInvalidInput) || errors.Is(err, storyboardpreview.ErrNotFound) ||
		errors.Is(err, storyboardpreview.ErrProjectVersionConflict) ||
		errors.Is(err, storyboardpreview.ErrCreationSpecVersionConflict) ||
		errors.Is(err, storyboardpreview.ErrIdempotencyConflict) || errors.Is(err, storyboardpreview.ErrPersistence) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w", storyboardpreview.ErrPersistence)
}
