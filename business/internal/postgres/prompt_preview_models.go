package postgres

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
)

// promptPreviewDraftModel 是隔离 Prompt Development Preview Draft 的 GORM 持久化模型。
// 它只映射版本化 Migration，不承担领域校验，也不是生产 PromptArtifact 或 PromptRevision Model。
type promptPreviewDraftModel struct {
	// ID 是 Business 应用生成的 Preview Draft 根 UUIDv7。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是所属 Project 逻辑标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// UserID 是创建时冻结的 Project Owner 逻辑标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// StoryboardPreviewID 是生成该 Draft 的 Storyboard Preview 逻辑标识。
	StoryboardPreviewID string `gorm:"column:storyboard_preview_id;type:uuid"`
	// StoryboardPreviewVersion 是生成时冻结的 Storyboard Preview 版本。
	StoryboardPreviewVersion int64 `gorm:"column:storyboard_preview_version"`
	// StoryboardPreviewContentDigest 是生成时冻结的 Storyboard Preview 内容摘要。
	StoryboardPreviewContentDigest []byte `gorm:"column:storyboard_preview_content_digest"`
	// Status 在 Development Preview 固定为 draft。
	Status string `gorm:"column:status"`
	// Version 在不可变 Preview Draft 固定为一。
	Version int64 `gorm:"column:version"`
	// SchemaVersion 是 Prompt Preview JSON 契约版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// ContentJSON 是严格校验后的 Prompt 全集 JSON。
	ContentJSON jsonbValue `gorm:"column:content_json;type:jsonb"`
	// ContentDigest 是 Content Canonical JSON 的 SHA-256。
	ContentDigest []byte `gorm:"column:content_digest"`
	// ExactTargetSetDigest 是 Agent 冻结且由保存请求绑定的目标全集摘要。
	ExactTargetSetDigest []byte `gorm:"column:exact_target_set_digest"`
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是来源 Prompt 冻结版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是来源候选 Validator 冻结版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// SourceExactSetValidatorVersion 是来源目标全集 Validator 冻结版本。
	SourceExactSetValidatorVersion string `gorm:"column:source_exact_set_validator_version"`
	// CreatedAt 是 Preview Draft 创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Preview Draft 更新时间；不可变 Draft 中与 CreatedAt 相同。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回隔离 Prompt Preview Draft 权威表名。
func (promptPreviewDraftModel) TableName() string { return "business.prompt_preview_draft" }

// promptPreviewCommandReceiptModel 是 Prompt Preview 保存命令 first-write-wins 回执模型。
type promptPreviewCommandReceiptModel struct {
	// ID 是 Business 应用生成的命令回执 UUIDv7。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// CommandID 是 Agent 提供且全局唯一的保存命令 UUIDv7。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// RequestDigest 冻结保存命令完整语义。
	RequestDigest []byte `gorm:"column:request_digest"`
	// UserID 是可信调用用户逻辑标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// ProjectID 是目标 Project 逻辑标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// ExpectedProjectVersion 是命令冻结的 Project 乐观版本。
	ExpectedProjectVersion int64 `gorm:"column:expected_project_version"`
	// StoryboardPreviewID 是命令冻结的 Storyboard Preview 逻辑标识。
	StoryboardPreviewID string `gorm:"column:storyboard_preview_id;type:uuid"`
	// ExpectedStoryboardPreviewVersion 是命令冻结的 Storyboard Preview 版本。
	ExpectedStoryboardPreviewVersion int64 `gorm:"column:expected_storyboard_preview_version"`
	// ExpectedStoryboardPreviewContentDigest 是命令冻结的 Storyboard Preview 内容摘要。
	ExpectedStoryboardPreviewContentDigest []byte `gorm:"column:expected_storyboard_preview_content_digest"`
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是命令冻结的 Prompt 版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是命令冻结的候选 Validator 版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// SourceExactSetValidatorVersion 是命令冻结的目标全集 Validator 版本。
	SourceExactSetValidatorVersion string `gorm:"column:source_exact_set_validator_version"`
	// ExactTargetSetDigest 是命令冻结的目标全集摘要。
	ExactTargetSetDigest []byte `gorm:"column:exact_target_set_digest"`
	// PromptPreviewID 是首次命令创建的 Preview Draft 根标识。
	PromptPreviewID string `gorm:"column:prompt_preview_id;type:uuid"`
	// ResultVersion 是首次响应冻结的 Draft 版本。
	ResultVersion int64 `gorm:"column:result_version"`
	// ResultStatus 是首次响应冻结的 Draft 状态。
	ResultStatus string `gorm:"column:result_status"`
	// ResultContentDigest 是首次响应冻结的 Prompt 内容摘要。
	ResultContentDigest []byte `gorm:"column:result_content_digest"`
	// CreatedAt 是命令首次提交 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Prompt Preview 命令回执权威表名。
func (promptPreviewCommandReceiptModel) TableName() string {
	return "business.prompt_preview_command_receipt"
}

// promptPreviewModelsFromAggregate 将已校验领域聚合显式映射为两个持久化 Model。
// Content 在映射时重新生成 Canonical JSON，禁止调用方写入非规范 JSON 字节。
func promptPreviewModelsFromAggregate(aggregate promptpreview.SaveAggregate) (promptPreviewDraftModel, promptPreviewCommandReceiptModel, error) {
	if err := promptpreview.ValidateAggregate(aggregate); err != nil {
		return promptPreviewDraftModel{}, promptPreviewCommandReceiptModel{}, err
	}
	contentJSON, err := aggregate.Draft.Content.CanonicalJSON()
	if err != nil {
		return promptPreviewDraftModel{}, promptPreviewCommandReceiptModel{}, err
	}
	draft := aggregate.Draft
	receipt := aggregate.Receipt
	storyboardDigest, err := promptpreview.ParseDigest(draft.StoryboardPreviewRef.ContentDigest)
	if err != nil {
		return promptPreviewDraftModel{}, promptPreviewCommandReceiptModel{}, err
	}
	return promptPreviewDraftModel{
			ID: draft.ID, ProjectID: draft.ProjectID, UserID: draft.UserID,
			StoryboardPreviewID: draft.StoryboardPreviewRef.ID, StoryboardPreviewVersion: draft.StoryboardPreviewRef.Version,
			StoryboardPreviewContentDigest: storyboardDigest.Bytes(),
			Status:                         draft.Status, Version: draft.Version, SchemaVersion: draft.SchemaVersion,
			ContentJSON: append(jsonbValue(nil), contentJSON...), ContentDigest: draft.ContentDigest.Bytes(),
			ExactTargetSetDigest: draft.ExactTargetSetDigest.Bytes(), SourceToolCallID: draft.SourceToolCallID,
			SourcePromptVersion: draft.SourcePromptVersion, SourceValidatorVersion: draft.SourceValidatorVersion,
			SourceExactSetValidatorVersion: draft.SourceExactSetValidatorVersion,
			CreatedAt:                      draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
		}, promptPreviewCommandReceiptModel{
			ID: receipt.ID, CommandID: receipt.CommandID, RequestDigest: receipt.RequestDigest.Bytes(),
			UserID: receipt.UserID, ProjectID: receipt.ProjectID, ExpectedProjectVersion: receipt.ExpectedProjectVersion,
			StoryboardPreviewID:                    receipt.StoryboardPreviewRef.ID,
			ExpectedStoryboardPreviewVersion:       receipt.StoryboardPreviewRef.Version,
			ExpectedStoryboardPreviewContentDigest: storyboardDigest.Bytes(),
			SourceToolCallID:                       receipt.SourceToolCallID, SourcePromptVersion: receipt.SourcePromptVersion,
			SourceValidatorVersion:         receipt.SourceValidatorVersion,
			SourceExactSetValidatorVersion: receipt.SourceExactSetValidatorVersion,
			ExactTargetSetDigest:           receipt.ExactTargetSetDigest.Bytes(), PromptPreviewID: receipt.PromptPreviewID,
			ResultVersion: receipt.ResultVersion, ResultStatus: receipt.ResultStatus,
			ResultContentDigest: receipt.ResultContentDigest.Bytes(), CreatedAt: receipt.CreatedAt,
		}, nil
}

// promptPreviewDraftEntity 从持久化 Model 严格恢复领域 Draft。
func promptPreviewDraftEntity(model promptPreviewDraftModel) (promptpreview.Draft, error) {
	content, err := promptpreview.ParseContentJSON(model.ContentJSON)
	if err != nil {
		return promptpreview.Draft{}, promptpreview.ErrPersistence
	}
	storyboardDigest, err := promptpreview.DigestFromBytes(model.StoryboardPreviewContentDigest)
	if err != nil {
		return promptpreview.Draft{}, promptpreview.ErrPersistence
	}
	contentDigest, err := promptpreview.DigestFromBytes(model.ContentDigest)
	if err != nil {
		return promptpreview.Draft{}, promptpreview.ErrPersistence
	}
	exactTargetSetDigest, err := promptpreview.DigestFromBytes(model.ExactTargetSetDigest)
	if err != nil {
		return promptpreview.Draft{}, promptpreview.ErrPersistence
	}
	draft := promptpreview.Draft{
		ID: model.ID, ProjectID: model.ProjectID, UserID: model.UserID,
		StoryboardPreviewRef: promptpreview.StoryboardPreviewRef{
			ID: model.StoryboardPreviewID, Version: model.StoryboardPreviewVersion,
			ContentDigest: storyboardDigest.Hex(),
		},
		Status: model.Status, Version: model.Version, SchemaVersion: model.SchemaVersion,
		Content: content, ContentDigest: contentDigest, ExactTargetSetDigest: exactTargetSetDigest,
		SourceToolCallID: model.SourceToolCallID, SourcePromptVersion: model.SourcePromptVersion,
		SourceValidatorVersion:         model.SourceValidatorVersion,
		SourceExactSetValidatorVersion: model.SourceExactSetValidatorVersion,
		CreatedAt:                      model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := promptpreview.ValidateDraft(draft); err != nil {
		return promptpreview.Draft{}, fmt.Errorf("%w: invalid prompt preview draft record", promptpreview.ErrPersistence)
	}
	return draft, nil
}

// promptPreviewReceiptEntity 从持久化 Model 严格恢复领域命令回执。
func promptPreviewReceiptEntity(model promptPreviewCommandReceiptModel) (promptpreview.CommandReceipt, error) {
	requestDigest, err := promptpreview.DigestFromBytes(model.RequestDigest)
	if err != nil {
		return promptpreview.CommandReceipt{}, promptpreview.ErrPersistence
	}
	storyboardDigest, err := promptpreview.DigestFromBytes(model.ExpectedStoryboardPreviewContentDigest)
	if err != nil {
		return promptpreview.CommandReceipt{}, promptpreview.ErrPersistence
	}
	exactTargetSetDigest, err := promptpreview.DigestFromBytes(model.ExactTargetSetDigest)
	if err != nil {
		return promptpreview.CommandReceipt{}, promptpreview.ErrPersistence
	}
	resultDigest, err := promptpreview.DigestFromBytes(model.ResultContentDigest)
	if err != nil {
		return promptpreview.CommandReceipt{}, promptpreview.ErrPersistence
	}
	return promptpreview.CommandReceipt{
		ID: model.ID, CommandID: model.CommandID, RequestDigest: requestDigest,
		UserID: model.UserID, ProjectID: model.ProjectID, ExpectedProjectVersion: model.ExpectedProjectVersion,
		StoryboardPreviewRef: promptpreview.StoryboardPreviewRef{
			ID: model.StoryboardPreviewID, Version: model.ExpectedStoryboardPreviewVersion,
			ContentDigest: storyboardDigest.Hex(),
		},
		SourceToolCallID: model.SourceToolCallID, SourcePromptVersion: model.SourcePromptVersion,
		SourceValidatorVersion:         model.SourceValidatorVersion,
		SourceExactSetValidatorVersion: model.SourceExactSetValidatorVersion,
		ExactTargetSetDigest:           exactTargetSetDigest, PromptPreviewID: model.PromptPreviewID,
		ResultVersion: model.ResultVersion, ResultStatus: model.ResultStatus,
		ResultContentDigest: resultDigest, CreatedAt: model.CreatedAt,
	}, nil
}
