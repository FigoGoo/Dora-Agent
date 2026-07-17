package postgres

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
)

// creationSpecModel 是 Business CreationSpec Draft 的 GORM 持久化模型。
// 该模型只映射版本化 Migration 已创建的表，不承担领域校验或对外 DTO 职责。
type creationSpecModel struct {
	// ID 是应用生成的 CreationSpec UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是所属 Project 逻辑标识，不创建数据库物理外键。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// UserID 是创建时冻结的 Project Owner 逻辑标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// Status 在开发预览中固定为 draft。
	Status string `gorm:"column:status"`
	// Version 是 Draft 资源版本，首次创建固定为一。
	Version int64 `gorm:"column:version"`
	// SchemaVersion 是结构化内容契约版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// ContentJSON 是通过确定性 Validator 与 Business 复核的严格 JSON。
	ContentJSON jsonbValue `gorm:"column:content_json;type:jsonb"`
	// ContentDigest 是 ContentJSON 冻结编码的 SHA-256 摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是来源 Prompt 冻结版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是来源确定性 Validator 冻结版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// CreatedAt 是 Draft 首次创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Draft 最近更新 UTC 时间；V1 首次创建时与 CreatedAt 相同。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 CreationSpec Draft 权威表名，禁止 GORM 推导或 AutoMigrate。
func (creationSpecModel) TableName() string { return "business.creation_spec" }

// creationSpecCommandReceiptModel 是保存命令 first-write-wins 回执的 GORM 持久化模型。
type creationSpecCommandReceiptModel struct {
	// ID 是应用生成的命令回执 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// CommandID 是 Agent 提供且全局唯一的保存命令 UUIDv7。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// RequestDigest 冻结保存命令完整语义，用于同键异义冲突。
	RequestDigest []byte `gorm:"column:request_digest"`
	// UserID 是可信调用用户逻辑标识。
	UserID string `gorm:"column:user_id;type:uuid"`
	// ProjectID 是目标 Project 逻辑标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// ExpectedProjectVersion 是命令冻结的 Project 乐观版本。
	ExpectedProjectVersion int64 `gorm:"column:expected_project_version"`
	// SourceToolCallID 是来源 Tool Call UUIDv7。
	SourceToolCallID string `gorm:"column:source_tool_call_id;type:uuid"`
	// SourcePromptVersion 是命令冻结的 Prompt 版本。
	SourcePromptVersion string `gorm:"column:source_prompt_version"`
	// SourceValidatorVersion 是命令冻结的 Validator 版本。
	SourceValidatorVersion string `gorm:"column:source_validator_version"`
	// CreationSpecID 是首次命令创建的 Draft 逻辑标识。
	CreationSpecID string `gorm:"column:creation_spec_id;type:uuid"`
	// ResultVersion 是首次响应冻结的 Draft 版本。
	ResultVersion int64 `gorm:"column:result_version"`
	// ResultStatus 是首次响应冻结的 Draft 状态。
	ResultStatus string `gorm:"column:result_status"`
	// ResultContentDigest 是首次响应冻结的内容摘要。
	ResultContentDigest []byte `gorm:"column:result_content_digest"`
	// CreatedAt 是命令首次提交 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 CreationSpec 保存命令回执权威表名。
func (creationSpecCommandReceiptModel) TableName() string {
	return "business.creation_spec_command_receipt"
}

// creationSpecModelsFromAggregate 将已校验领域聚合显式映射为两个持久化 Model。
// 映射会重新生成 Canonical JSON，防止调用方把非规范 JSON 字节带入 jsonb。
func creationSpecModelsFromAggregate(aggregate creationspec.SaveAggregate) (creationSpecModel, creationSpecCommandReceiptModel, error) {
	if err := creationspec.ValidateAggregate(aggregate); err != nil {
		return creationSpecModel{}, creationSpecCommandReceiptModel{}, err
	}
	contentJSON, err := aggregate.Draft.Content.CanonicalJSON()
	if err != nil {
		return creationSpecModel{}, creationSpecCommandReceiptModel{}, err
	}
	draft := aggregate.Draft
	receipt := aggregate.Receipt
	return creationSpecModel{
			ID: draft.ID, ProjectID: draft.ProjectID, UserID: draft.UserID, Status: draft.Status,
			Version: draft.Version, SchemaVersion: draft.SchemaVersion,
			ContentJSON: append(jsonbValue(nil), contentJSON...), ContentDigest: draft.ContentDigest.Bytes(),
			SourceToolCallID: draft.SourceToolCallID, SourcePromptVersion: draft.SourcePromptVersion,
			SourceValidatorVersion: draft.SourceValidatorVersion, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
		}, creationSpecCommandReceiptModel{
			ID: receipt.ID, CommandID: receipt.CommandID, RequestDigest: receipt.RequestDigest.Bytes(),
			UserID: receipt.UserID, ProjectID: receipt.ProjectID, ExpectedProjectVersion: receipt.ExpectedProjectVersion,
			SourceToolCallID: receipt.SourceToolCallID, SourcePromptVersion: receipt.SourcePromptVersion,
			SourceValidatorVersion: receipt.SourceValidatorVersion, CreationSpecID: receipt.CreationSpecID,
			ResultVersion: receipt.ResultVersion, ResultStatus: receipt.ResultStatus,
			ResultContentDigest: receipt.ResultContentDigest.Bytes(), CreatedAt: receipt.CreatedAt,
		}, nil
}

// creationSpecDraftEntity 从持久化 Model 严格恢复领域 Draft；数据库不变量损坏统一返回稳定持久化错误。
func creationSpecDraftEntity(model creationSpecModel) (creationspec.Draft, error) {
	content, err := creationspec.ParseContentJSON(model.ContentJSON)
	if err != nil {
		return creationspec.Draft{}, creationspec.ErrPersistence
	}
	digest, err := creationspec.DigestFromBytes(model.ContentDigest)
	if err != nil {
		return creationspec.Draft{}, creationspec.ErrPersistence
	}
	draft := creationspec.Draft{
		ID: model.ID, ProjectID: model.ProjectID, UserID: model.UserID, Status: model.Status,
		Version: model.Version, SchemaVersion: model.SchemaVersion, Content: content, ContentDigest: digest,
		SourceToolCallID: model.SourceToolCallID, SourcePromptVersion: model.SourcePromptVersion,
		SourceValidatorVersion: model.SourceValidatorVersion, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if err := creationspec.ValidateDraft(draft); err != nil {
		return creationspec.Draft{}, fmt.Errorf("%w: invalid creation spec draft record", creationspec.ErrPersistence)
	}
	return draft, nil
}

// creationSpecReceiptEntity 从持久化 Model 严格恢复领域命令回执。
func creationSpecReceiptEntity(model creationSpecCommandReceiptModel) (creationspec.CommandReceipt, error) {
	requestDigest, err := creationspec.DigestFromBytes(model.RequestDigest)
	if err != nil {
		return creationspec.CommandReceipt{}, creationspec.ErrPersistence
	}
	resultDigest, err := creationspec.DigestFromBytes(model.ResultContentDigest)
	if err != nil {
		return creationspec.CommandReceipt{}, creationspec.ErrPersistence
	}
	return creationspec.CommandReceipt{
		ID: model.ID, CommandID: model.CommandID, RequestDigest: requestDigest,
		UserID: model.UserID, ProjectID: model.ProjectID, ExpectedProjectVersion: model.ExpectedProjectVersion,
		SourceToolCallID: model.SourceToolCallID, SourcePromptVersion: model.SourcePromptVersion,
		SourceValidatorVersion: model.SourceValidatorVersion, CreationSpecID: model.CreationSpecID,
		ResultVersion: model.ResultVersion, ResultStatus: model.ResultStatus,
		ResultContentDigest: resultDigest, CreatedAt: model.CreatedAt,
	}, nil
}
