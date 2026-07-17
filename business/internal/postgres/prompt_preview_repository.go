package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const promptPreviewGenerationContextSQL = `
SELECT
    project_record.id AS project_id,
    project_record.version AS project_version,
    project_record.title AS project_title,
    storyboard_record.id AS storyboard_id,
    storyboard_record.project_id AS storyboard_project_id,
    storyboard_record.user_id AS storyboard_user_id,
    storyboard_record.creation_spec_id AS storyboard_creation_spec_id,
    storyboard_record.creation_spec_version AS storyboard_creation_spec_version,
    storyboard_record.creation_spec_content_digest AS storyboard_creation_spec_content_digest,
    storyboard_record.status AS storyboard_status,
    storyboard_record.version AS storyboard_version,
    storyboard_record.schema_version AS storyboard_schema_version,
    storyboard_record.content_json AS storyboard_content_json,
    storyboard_record.content_digest AS storyboard_content_digest,
    storyboard_record.source_tool_call_id AS storyboard_source_tool_call_id,
    storyboard_record.source_prompt_version AS storyboard_source_prompt_version,
    storyboard_record.source_validator_version AS storyboard_source_validator_version,
    storyboard_record.created_at AS storyboard_created_at,
    storyboard_record.updated_at AS storyboard_updated_at
FROM business.project AS project_record
JOIN business.storyboard_preview_draft AS storyboard_record
  ON storyboard_record.project_id = project_record.id
 AND storyboard_record.user_id = project_record.owner_user_id
WHERE project_record.id = ?
  AND project_record.owner_user_id = ?
  AND project_record.lifecycle_status = 'active'
  AND storyboard_record.id = ?
  AND storyboard_record.status = 'draft'
  AND storyboard_record.schema_version = 'storyboard.preview.draft.v1'`

// PromptPreviewRepository 使用 GORM 和 Business PostgreSQL 实现联合上下文、Draft 与命令回执边界。
type PromptPreviewRepository struct {
	db *gorm.DB
}

var _ promptpreview.Repository = (*PromptPreviewRepository)(nil)

// NewPromptPreviewRepository 从 Business PostgreSQL Client 创建 Repository；Client 缺失时失败关闭。
func NewPromptPreviewRepository(client *Client) (*PromptPreviewRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create prompt preview repository: postgres client is nil")
	}
	return &PromptPreviewRepository{db: client.db}, nil
}

// promptPreviewGenerationContextRecord 是单次 JOIN 查询的专用读取 DTO。
type promptPreviewGenerationContextRecord struct {
	// ProjectID 是 Owner 校验后的 Project 标识。
	ProjectID string `gorm:"column:project_id"`
	// ProjectVersion 是保存时必须复核的 Project 乐观版本。
	ProjectVersion int64 `gorm:"column:project_version"`
	// ProjectTitle 是允许返回给 Agent 的安全项目标题。
	ProjectTitle string `gorm:"column:project_title"`
	// StoryboardID 是指定的 Storyboard Preview Draft 标识。
	StoryboardID string `gorm:"column:storyboard_id"`
	// StoryboardProjectID 是 Storyboard 冻结的 Project 逻辑标识。
	StoryboardProjectID string `gorm:"column:storyboard_project_id"`
	// StoryboardUserID 是 Storyboard 冻结的 Owner 逻辑标识。
	StoryboardUserID string `gorm:"column:storyboard_user_id"`
	// StoryboardCreationSpecID 是 Storyboard 来源 CreationSpec 逻辑标识。
	StoryboardCreationSpecID string `gorm:"column:storyboard_creation_spec_id"`
	// StoryboardCreationSpecVersion 是 Storyboard 来源 CreationSpec 版本。
	StoryboardCreationSpecVersion int64 `gorm:"column:storyboard_creation_spec_version"`
	// StoryboardCreationSpecContentDigest 是 Storyboard 来源 CreationSpec 摘要。
	StoryboardCreationSpecContentDigest []byte `gorm:"column:storyboard_creation_spec_content_digest"`
	// StoryboardStatus 是 Storyboard Preview Draft 状态。
	StoryboardStatus string `gorm:"column:storyboard_status"`
	// StoryboardVersion 是 Storyboard Preview Draft 版本。
	StoryboardVersion int64 `gorm:"column:storyboard_version"`
	// StoryboardSchemaVersion 是 Storyboard Preview 内容契约版本。
	StoryboardSchemaVersion string `gorm:"column:storyboard_schema_version"`
	// StoryboardContentJSON 是严格 Storyboard Preview 内容 JSON。
	StoryboardContentJSON jsonbValue `gorm:"column:storyboard_content_json"`
	// StoryboardContentDigest 是 Storyboard Preview Canonical Content 摘要。
	StoryboardContentDigest []byte `gorm:"column:storyboard_content_digest"`
	// StoryboardSourceToolCallID 是产生 Storyboard Preview 的 Tool Call 标识。
	StoryboardSourceToolCallID string `gorm:"column:storyboard_source_tool_call_id"`
	// StoryboardSourcePromptVersion 是产生 Storyboard Preview 的 Prompt 版本。
	StoryboardSourcePromptVersion string `gorm:"column:storyboard_source_prompt_version"`
	// StoryboardSourceValidatorVersion 是产生 Storyboard Preview 的 Validator 版本。
	StoryboardSourceValidatorVersion string `gorm:"column:storyboard_source_validator_version"`
	// StoryboardCreatedAt 是 Storyboard Preview Draft 创建时间。
	StoryboardCreatedAt time.Time `gorm:"column:storyboard_created_at"`
	// StoryboardUpdatedAt 是 Storyboard Preview Draft 更新时间。
	StoryboardUpdatedAt time.Time `gorm:"column:storyboard_updated_at"`
}

// FindGenerationContext 用一次 JOIN 同时校验 Project Owner、生命周期和指定 Storyboard Preview Draft。
// 不存在、跨 Owner、归档或非 Draft 统一隐藏为 ErrNotFound，避免资源枚举。
func (repository *PromptPreviewRepository) FindGenerationContext(ctx context.Context, query promptpreview.ContextQuery) (promptpreview.GenerationContext, error) {
	if ctx == nil {
		return promptpreview.GenerationContext{}, promptpreview.ErrInvalidInput
	}
	return loadPromptGenerationContext(repository.db.WithContext(ctx), query, false)
}

// SaveDraft 以 command_id 争夺 first-write-wins 创建权，并在同一事务复核 Project 与 Storyboard Preview 快照。
// 同 command/digest 返回首次 Draft；异 digest、版本漂移或目标全集失效均不产生第二份业务事实。
func (repository *PromptPreviewRepository) SaveDraft(ctx context.Context, aggregate promptpreview.SaveAggregate) (promptpreview.SaveResult, error) {
	if ctx == nil {
		return promptpreview.SaveResult{}, promptpreview.ErrInvalidInput
	}
	draftModel, receiptModel, err := promptPreviewModelsFromAggregate(aggregate)
	if err != nil {
		return promptpreview.SaveResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return promptpreview.SaveResult{}, err
	}

	var result promptpreview.SaveResult
	err = repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		inserted := transactionDB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "command_id"}}, DoNothing: true,
		}).Create(&receiptModel)
		if inserted.Error != nil {
			return fmt.Errorf("insert prompt preview command receipt: %w", inserted.Error)
		}
		if inserted.RowsAffected == 0 {
			existing, loadErr := loadPromptPreviewAggregate(transactionDB, receiptModel.CommandID)
			if loadErr != nil {
				return loadErr
			}
			// command_id 已由首次写入绑定；不同摘要不得覆盖，也不得创建第二份 Draft。
			if existing.Receipt.RequestDigest != aggregate.Receipt.RequestDigest {
				return promptpreview.ErrIdempotencyConflict
			}
			result = promptpreview.SaveResult{
				Disposition: promptpreview.CommandDispositionReplayed, Draft: existing.Draft,
			}
			return nil
		}

		generationContext, contextErr := loadPromptGenerationContext(transactionDB, promptpreview.ContextQuery{
			UserID: aggregate.Draft.UserID, ProjectID: aggregate.Draft.ProjectID,
			StoryboardPreviewRef: aggregate.Draft.StoryboardPreviewRef,
		}, true)
		if contextErr != nil {
			return contextErr
		}
		if generationContext.ProjectVersion != aggregate.Receipt.ExpectedProjectVersion {
			// Project 语义已变化时回滚先写入的命令回执，要求 Agent 以新上下文创建新命令。
			return promptpreview.ErrProjectVersionConflict
		}
		actualReference := promptpreview.StoryboardPreviewRef{
			ID: generationContext.Storyboard.ID, Version: generationContext.Storyboard.Version,
			ContentDigest: generationContext.Storyboard.ContentDigest.Hex(),
		}
		if actualReference != aggregate.Draft.StoryboardPreviewRef {
			// 已授权 Storyboard Preview 的版本或摘要漂移不能静默替换生成输入。
			return promptpreview.ErrStoryboardVersionConflict
		}
		if err := promptpreview.ValidateContentAgainstStoryboard(aggregate.Draft.Content, generationContext.Storyboard); err != nil {
			return err
		}
		if err := transactionDB.Create(&draftModel).Error; err != nil {
			return fmt.Errorf("insert prompt preview draft: %w", err)
		}
		result = promptpreview.SaveResult{
			Disposition: promptpreview.CommandDispositionCreated, Draft: aggregate.Draft,
		}
		return nil
	})
	if err != nil {
		return promptpreview.SaveResult{}, mapPromptPreviewRepositoryError(err)
	}
	return result, nil
}

// QueryCommand 按原 command_id、user、project 查询首次回执，再比较摘要并恢复 Draft。
// 查询次数固定为至多两次，跨 Owner 或 Project 统一返回 not_found。
func (repository *PromptPreviewRepository) QueryCommand(ctx context.Context, query promptpreview.QueryCommand) (promptpreview.QueryResult, error) {
	if ctx == nil {
		return promptpreview.QueryResult{}, promptpreview.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return promptpreview.QueryResult{}, err
	}
	var receiptModel promptPreviewCommandReceiptModel
	err := repository.db.WithContext(ctx).
		Where("command_id = ? AND user_id = ? AND project_id = ?", query.CommandID, query.UserID, query.ProjectID).
		Take(&receiptModel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return promptpreview.QueryResult{Status: promptpreview.QueryStatusNotFound}, nil
	}
	if err != nil {
		return promptpreview.QueryResult{}, mapPromptPreviewRepositoryError(err)
	}
	receipt, err := promptPreviewReceiptEntity(receiptModel)
	if err != nil {
		return promptpreview.QueryResult{}, promptpreview.ErrPersistence
	}
	if receipt.RequestDigest != query.RequestDigest {
		return promptpreview.QueryResult{Status: promptpreview.QueryStatusConflict}, nil
	}
	var draftModel promptPreviewDraftModel
	if err := repository.db.WithContext(ctx).Where("id = ?", receipt.PromptPreviewID).Take(&draftModel).Error; err != nil {
		return promptpreview.QueryResult{}, mapPromptPreviewRepositoryError(err)
	}
	draft, err := promptPreviewDraftEntity(draftModel)
	if err != nil {
		return promptpreview.QueryResult{}, promptpreview.ErrPersistence
	}
	if err := promptpreview.ValidateAggregate(promptpreview.SaveAggregate{Draft: draft, Receipt: receipt}); err != nil {
		return promptpreview.QueryResult{}, promptpreview.ErrPersistence
	}
	return promptpreview.QueryResult{Status: promptpreview.QueryStatusCompleted, Draft: &draft}, nil
}

// loadPromptGenerationContext 执行固定一次 JOIN；保存事务可选择 FOR SHARE 锁定两条业务基线记录。
func loadPromptGenerationContext(db *gorm.DB, query promptpreview.ContextQuery, lock bool) (promptpreview.GenerationContext, error) {
	statement := promptPreviewGenerationContextSQL
	if lock {
		statement += " FOR SHARE OF project_record, storyboard_record"
	}
	var record promptPreviewGenerationContextRecord
	queryResult := db.Raw(statement, query.ProjectID, query.UserID, query.StoryboardPreviewRef.ID).Scan(&record)
	if queryResult.Error != nil {
		return promptpreview.GenerationContext{}, mapPromptPreviewRepositoryError(queryResult.Error)
	}
	if queryResult.RowsAffected != 1 {
		return promptpreview.GenerationContext{}, promptpreview.ErrNotFound
	}
	storyboardModel := storyboardPreviewDraftModel{
		ID: record.StoryboardID, ProjectID: record.StoryboardProjectID, UserID: record.StoryboardUserID,
		CreationSpecID: record.StoryboardCreationSpecID, CreationSpecVersion: record.StoryboardCreationSpecVersion,
		CreationSpecContentDigest: record.StoryboardCreationSpecContentDigest,
		Status:                    record.StoryboardStatus, Version: record.StoryboardVersion,
		SchemaVersion: record.StoryboardSchemaVersion, ContentJSON: record.StoryboardContentJSON,
		ContentDigest: record.StoryboardContentDigest, SourceToolCallID: record.StoryboardSourceToolCallID,
		SourcePromptVersion:    record.StoryboardSourcePromptVersion,
		SourceValidatorVersion: record.StoryboardSourceValidatorVersion,
		CreatedAt:              record.StoryboardCreatedAt, UpdatedAt: record.StoryboardUpdatedAt,
	}
	storyboardDraft, err := storyboardPreviewDraftEntity(storyboardModel)
	if err != nil {
		return promptpreview.GenerationContext{}, promptpreview.ErrPersistence
	}
	storyboardDigest, err := promptpreview.DigestFromBytes(storyboardDraft.ContentDigest.Bytes())
	if err != nil {
		return promptpreview.GenerationContext{}, promptpreview.ErrPersistence
	}
	result := promptpreview.GenerationContext{
		ProjectID: record.ProjectID, ProjectVersion: record.ProjectVersion, ProjectTitle: record.ProjectTitle,
		Storyboard: promptpreview.StoryboardSnapshot{
			ID: storyboardDraft.ID, ProjectID: storyboardDraft.ProjectID, UserID: storyboardDraft.UserID,
			Status: storyboardDraft.Status, Version: storyboardDraft.Version,
			SchemaVersion: storyboardDraft.SchemaVersion, Content: storyboardDraft.Content,
			ContentDigest: storyboardDigest,
		},
	}
	if err := promptpreview.ValidateGenerationContext(result); err != nil {
		return promptpreview.GenerationContext{}, promptpreview.ErrPersistence
	}
	return result, nil
}

// loadPromptPreviewAggregate 用固定两次主键查询恢复首次写入的不变聚合。
func loadPromptPreviewAggregate(db *gorm.DB, commandID string) (promptpreview.SaveAggregate, error) {
	var receiptModel promptPreviewCommandReceiptModel
	if err := db.Where("command_id = ?", commandID).Take(&receiptModel).Error; err != nil {
		return promptpreview.SaveAggregate{}, fmt.Errorf("read prompt preview command receipt: %w", err)
	}
	receipt, err := promptPreviewReceiptEntity(receiptModel)
	if err != nil {
		return promptpreview.SaveAggregate{}, promptpreview.ErrPersistence
	}
	var draftModel promptPreviewDraftModel
	if err := db.Where("id = ?", receipt.PromptPreviewID).Take(&draftModel).Error; err != nil {
		return promptpreview.SaveAggregate{}, fmt.Errorf("read prompt preview draft: %w", err)
	}
	draft, err := promptPreviewDraftEntity(draftModel)
	if err != nil {
		return promptpreview.SaveAggregate{}, promptpreview.ErrPersistence
	}
	aggregate := promptpreview.SaveAggregate{Draft: draft, Receipt: receipt}
	if err := promptpreview.ValidateAggregate(aggregate); err != nil {
		return promptpreview.SaveAggregate{}, promptpreview.ErrPersistence
	}
	return aggregate, nil
}

// mapPromptPreviewRepositoryError 保留稳定领域与 Context 语义，其余数据库细节收敛为 ErrPersistence。
func mapPromptPreviewRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, promptpreview.ErrInvalidInput) || errors.Is(err, promptpreview.ErrNotFound) ||
		errors.Is(err, promptpreview.ErrProjectVersionConflict) ||
		errors.Is(err, promptpreview.ErrStoryboardVersionConflict) ||
		errors.Is(err, promptpreview.ErrIdempotencyConflict) || errors.Is(err, promptpreview.ErrPersistence) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w", promptpreview.ErrPersistence)
}
