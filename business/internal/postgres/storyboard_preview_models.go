package postgres

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

// storyboardPreviewDraftModel 是隔离 Storyboard Development Preview Draft 的 GORM 持久化模型。
// 它只映射版本化 Migration，不承担领域校验，也不是生产 Storyboard Revision Model。
type storyboardPreviewDraftModel struct {
	// ID 是 Business 应用生成的 Preview Draft 根 UUIDv7。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是所属 Project 逻辑标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// UserID 是创建时冻结的 Project Owner 逻辑标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// CreationSpecID 是生成该 Draft 的 CreationSpec 逻辑标识。
	CreationSpecID string `gorm:"column:creation_spec_id;type:uuid"`
	// CreationSpecVersion 是生成时冻结的 CreationSpec 版本。
	CreationSpecVersion int64 `gorm:"column:creation_spec_version"`
	// CreationSpecContentDigest 是生成时冻结的 CreationSpec 内容摘要。
	CreationSpecContentDigest []byte `gorm:"column:creation_spec_content_digest"`
	// Status 在 Development Preview 固定为 draft。
	Status string `gorm:"column:status"`
	// Version 在不可变 Preview Draft 固定为一。
	Version int64 `gorm:"column:version"`
	// SchemaVersion 是 Storyboard Preview JSON 契约版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// ContentJSON 是严格校验后的局部键、引用和依赖 DAG JSON。
	ContentJSON jsonbValue `gorm:"column:content_json;type:jsonb"`
	// ContentDigest 是 Content Canonical JSON 的 SHA-256。
	ContentDigest []byte `gorm:"column:content_digest"`
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是来源 Prompt 冻结版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是来源 Validator 冻结版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// CreatedAt 是 Preview Draft 创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Preview Draft 更新时间；不可变 Draft 中与 CreatedAt 相同。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回隔离 Storyboard Preview Draft 权威表名。
func (storyboardPreviewDraftModel) TableName() string {
	return "business.storyboard_preview_draft"
}

// storyboardPreviewCommandReceiptModel 是 Storyboard Preview 保存命令 first-write-wins 回执模型。
type storyboardPreviewCommandReceiptModel struct {
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
	// CreationSpecID 是命令冻结的 CreationSpec 逻辑标识。
	CreationSpecID string `gorm:"column:creation_spec_id;type:uuid"`
	// ExpectedCreationSpecVersion 是命令冻结的 CreationSpec 版本。
	ExpectedCreationSpecVersion int64 `gorm:"column:expected_creation_spec_version"`
	// ExpectedCreationSpecContentDigest 是命令冻结的 CreationSpec 内容摘要。
	ExpectedCreationSpecContentDigest []byte `gorm:"column:expected_creation_spec_content_digest"`
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是命令冻结的 Prompt 版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是命令冻结的 Validator 版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// StoryboardPreviewID 是首次命令创建的 Preview Draft 根标识。
	StoryboardPreviewID string `gorm:"column:storyboard_preview_id;type:uuid"`
	// ResultVersion 是首次响应冻结的 Draft 版本。
	ResultVersion int64 `gorm:"column:result_version"`
	// ResultStatus 是首次响应冻结的 Draft 状态。
	ResultStatus string `gorm:"column:result_status"`
	// ResultContentDigest 是首次响应冻结的 Storyboard 内容摘要。
	ResultContentDigest []byte `gorm:"column:result_content_digest"`
	// CreatedAt 是命令首次提交 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Storyboard Preview 命令回执权威表名。
func (storyboardPreviewCommandReceiptModel) TableName() string {
	return "business.storyboard_preview_command_receipt"
}

// storyboardPreviewModelsFromAggregate 将已校验领域聚合显式映射为两个持久化 Model。
// Content 在映射时重新生成 Canonical JSON，禁止调用方写入非规范 JSON 字节。
func storyboardPreviewModelsFromAggregate(aggregate storyboardpreview.SaveAggregate) (storyboardPreviewDraftModel, storyboardPreviewCommandReceiptModel, error) {
	if err := storyboardpreview.ValidateAggregate(aggregate); err != nil {
		return storyboardPreviewDraftModel{}, storyboardPreviewCommandReceiptModel{}, err
	}
	contentJSON, err := aggregate.Draft.Content.CanonicalJSON()
	if err != nil {
		return storyboardPreviewDraftModel{}, storyboardPreviewCommandReceiptModel{}, err
	}
	draft := aggregate.Draft
	receipt := aggregate.Receipt
	return storyboardPreviewDraftModel{
			ID: draft.ID, ProjectID: draft.ProjectID, UserID: draft.UserID,
			CreationSpecID: draft.CreationSpecRef.ID, CreationSpecVersion: draft.CreationSpecRef.Version,
			CreationSpecContentDigest: draft.CreationSpecRef.ContentDigest.Bytes(), Status: draft.Status,
			Version: draft.Version, SchemaVersion: draft.SchemaVersion,
			ContentJSON: append(jsonbValue(nil), contentJSON...), ContentDigest: draft.ContentDigest.Bytes(),
			SourceToolCallID: draft.SourceToolCallID, SourcePromptVersion: draft.SourcePromptVersion,
			SourceValidatorVersion: draft.SourceValidatorVersion, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
		}, storyboardPreviewCommandReceiptModel{
			ID: receipt.ID, CommandID: receipt.CommandID, RequestDigest: receipt.RequestDigest.Bytes(),
			UserID: receipt.UserID, ProjectID: receipt.ProjectID, ExpectedProjectVersion: receipt.ExpectedProjectVersion,
			CreationSpecID: receipt.CreationSpecRef.ID, ExpectedCreationSpecVersion: receipt.CreationSpecRef.Version,
			ExpectedCreationSpecContentDigest: receipt.CreationSpecRef.ContentDigest.Bytes(),
			SourceToolCallID:                  receipt.SourceToolCallID, SourcePromptVersion: receipt.SourcePromptVersion,
			SourceValidatorVersion: receipt.SourceValidatorVersion, StoryboardPreviewID: receipt.StoryboardPreviewID,
			ResultVersion: receipt.ResultVersion, ResultStatus: receipt.ResultStatus,
			ResultContentDigest: receipt.ResultContentDigest.Bytes(), CreatedAt: receipt.CreatedAt,
		}, nil
}

// storyboardPreviewDraftEntity 从持久化 Model 严格恢复领域 Draft。
func storyboardPreviewDraftEntity(model storyboardPreviewDraftModel) (storyboardpreview.Draft, error) {
	content, err := storyboardpreview.ParseContentJSON(model.ContentJSON)
	if err != nil {
		return storyboardpreview.Draft{}, storyboardpreview.ErrPersistence
	}
	creationSpecDigest, err := storyboardpreview.DigestFromBytes(model.CreationSpecContentDigest)
	if err != nil {
		return storyboardpreview.Draft{}, storyboardpreview.ErrPersistence
	}
	contentDigest, err := storyboardpreview.DigestFromBytes(model.ContentDigest)
	if err != nil {
		return storyboardpreview.Draft{}, storyboardpreview.ErrPersistence
	}
	draft := storyboardpreview.Draft{
		ID: model.ID, ProjectID: model.ProjectID, UserID: model.UserID,
		CreationSpecRef: storyboardpreview.CreationSpecRef{
			ID: model.CreationSpecID, Version: model.CreationSpecVersion, ContentDigest: creationSpecDigest,
		},
		Status: model.Status, Version: model.Version, SchemaVersion: model.SchemaVersion,
		Content: content, ContentDigest: contentDigest, SourceToolCallID: model.SourceToolCallID,
		SourcePromptVersion: model.SourcePromptVersion, SourceValidatorVersion: model.SourceValidatorVersion,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := storyboardpreview.ValidateDraft(draft); err != nil {
		return storyboardpreview.Draft{}, fmt.Errorf("%w: invalid storyboard preview draft record", storyboardpreview.ErrPersistence)
	}
	return draft, nil
}

// storyboardPreviewReceiptEntity 从持久化 Model 严格恢复领域命令回执。
func storyboardPreviewReceiptEntity(model storyboardPreviewCommandReceiptModel) (storyboardpreview.CommandReceipt, error) {
	requestDigest, err := storyboardpreview.DigestFromBytes(model.RequestDigest)
	if err != nil {
		return storyboardpreview.CommandReceipt{}, storyboardpreview.ErrPersistence
	}
	creationSpecDigest, err := storyboardpreview.DigestFromBytes(model.ExpectedCreationSpecContentDigest)
	if err != nil {
		return storyboardpreview.CommandReceipt{}, storyboardpreview.ErrPersistence
	}
	resultDigest, err := storyboardpreview.DigestFromBytes(model.ResultContentDigest)
	if err != nil {
		return storyboardpreview.CommandReceipt{}, storyboardpreview.ErrPersistence
	}
	return storyboardpreview.CommandReceipt{
		ID: model.ID, CommandID: model.CommandID, RequestDigest: requestDigest,
		UserID: model.UserID, ProjectID: model.ProjectID, ExpectedProjectVersion: model.ExpectedProjectVersion,
		CreationSpecRef: storyboardpreview.CreationSpecRef{
			ID: model.CreationSpecID, Version: model.ExpectedCreationSpecVersion, ContentDigest: creationSpecDigest,
		},
		SourceToolCallID: model.SourceToolCallID, SourcePromptVersion: model.SourcePromptVersion,
		SourceValidatorVersion: model.SourceValidatorVersion, StoryboardPreviewID: model.StoryboardPreviewID,
		ResultVersion: model.ResultVersion, ResultStatus: model.ResultStatus,
		ResultContentDigest: resultDigest, CreatedAt: model.CreatedAt,
	}, nil
}
