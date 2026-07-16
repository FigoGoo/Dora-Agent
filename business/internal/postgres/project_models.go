package postgres

import (
	"bytes"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

// projectModel Project 持久化模型，只负责 business.project 表的字段映射。
type projectModel struct {
	// ID Project UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// OwnerUserID 项目所有者逻辑关联标识。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// Title 项目安全标题。
	Title string `gorm:"column:title"`
	// LifecycleStatus 项目生命周期稳定代码。
	LifecycleStatus string `gorm:"column:lifecycle_status"`
	// RecentRunStatus 最近运行摘要稳定代码。
	RecentRunStatus string `gorm:"column:recent_run_status"`
	// InitialPromptStatus 首提示词状态稳定代码。
	InitialPromptStatus string `gorm:"column:initial_prompt_status"`
	// Version Project 乐观并发版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt Project 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt Project 最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Project 权威表名，禁止 GORM 推导或 AutoMigrate。
func (projectModel) TableName() string { return "business.project" }

// projectCreationReceiptModel 快速创建回执持久化模型，冻结首次安全响应快照。
type projectCreationReceiptModel struct {
	// ID 创建回执 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// OwnerUserID 发起创建的可信用户标识。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// CommandType 幂等命令类型。
	CommandType string `gorm:"column:command_type"`
	// KeyDigest 原始幂等键 SHA-256 摘要。
	KeyDigest []byte `gorm:"column:key_digest"`
	// SemanticDigest 规范化业务语义 SHA-256 摘要。
	SemanticDigest []byte `gorm:"column:semantic_digest"`
	// ProjectID 首次创建的 Project 标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// LifecycleStatus 首次响应的项目生命周期快照。
	LifecycleStatus string `gorm:"column:lifecycle_status"`
	// RecentRunStatus 首次响应的最近运行摘要。
	RecentRunStatus string `gorm:"column:recent_run_status"`
	// SessionProvisioningStatus 首次响应的 Session 初始化状态。
	SessionProvisioningStatus string `gorm:"column:session_provisioning_status"`
	// InitialPromptStatus 首次响应的首提示词状态。
	InitialPromptStatus string `gorm:"column:initial_prompt_status"`
	// CreatedAt 首次创建命令提交时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回快速创建回执权威表名，唯一约束负责并发幂等收敛。
func (projectCreationReceiptModel) TableName() string {
	return "business.project_creation_receipt"
}

// projectSessionBindingModel Project 与 Agent 默认 Session 逻辑绑定的持久化模型。
type projectSessionBindingModel struct {
	// ID 绑定 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 所属 Business Project 标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// CommandID Agent 初始化命令标识。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// RequestDigest 按 ensure_project_session.v1 Canonical Schema 计算的请求摘要。
	RequestDigest []byte `gorm:"column:request_digest"`
	// AgentSessionID Agent 权威 Session 标识，尚未确认时为空。
	AgentSessionID *string `gorm:"column:agent_session_id;type:uuid"`
	// AgentInputID Agent 权威首 Input 标识，空提示词或尚未确认时为空。
	AgentInputID *string `gorm:"column:agent_input_id;type:uuid"`
	// ProvisioningStatus Session 初始化状态。
	ProvisioningStatus string `gorm:"column:provisioning_status"`
	// LastErrorCode 最近一次稳定错误代码。
	LastErrorCode *string `gorm:"column:last_error_code"`
	// Version 绑定并发版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 绑定创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 绑定最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Project 与 Agent Session 绑定的权威表名。
func (projectSessionBindingModel) TableName() string {
	return "business.project_session_binding"
}

// projectSessionOutboxModel Session 初始化 Outbox 持久化模型，只允许密文及非秘密加密元数据。
type projectSessionOutboxModel struct {
	// ID Outbox UUIDv7 主键，同时作为稳定 command_id。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// EventType Session 初始化事件类型。
	EventType string `gorm:"column:event_type"`
	// SchemaVersion 初始化负载契约版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// AggregateID 所属 Business Project 标识。
	AggregateID string `gorm:"column:aggregate_id;type:uuid"`
	// OwnerUserID 命令冻结的可信项目所有者标识。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// RequestDigest 按 ensure_project_session.v1 Canonical Schema 计算的请求摘要。
	RequestDigest []byte `gorm:"column:request_digest"`
	// HasInitialPrompt 表示命令是否携带首提示词密文。
	HasInitialPrompt bool `gorm:"column:has_initial_prompt"`
	// PayloadEncryptionAlgorithm 首提示词认证加密算法。
	PayloadEncryptionAlgorithm *string `gorm:"column:payload_encryption_algorithm"`
	// PayloadKeyVersion 外部密钥版本引用，不包含密钥材料。
	PayloadKeyVersion *string `gorm:"column:payload_key_version"`
	// PayloadNonce 首提示词认证加密随机数。
	PayloadNonce []byte `gorm:"column:payload_nonce"`
	// PayloadCiphertext 首提示词密文及认证标签。
	PayloadCiphertext []byte `gorm:"column:payload_ciphertext"`
	// PayloadDigest 规范化首提示词明文摘要。
	PayloadDigest []byte `gorm:"column:payload_digest"`
	// SkillSnapshotDigest 是 v2 Snapshot set 摘要；v1 数据库审计值固定 empty digest。
	SkillSnapshotDigest []byte `gorm:"column:skill_snapshot_digest"`
	// SkillCount 是 v2 Snapshot Item 数量；v1 固定零。
	SkillCount int32 `gorm:"column:skill_count"`
	// BindingSetVersion 是 v2 初始绑定集合版本；v1 为空。
	BindingSetVersion *int64 `gorm:"column:binding_set_version"`
	// ResolutionID 是 v2 不可变解析头逻辑引用；v1 为空。
	ResolutionID *string `gorm:"column:resolution_id;type:uuid"`
	// PayloadClearedAt Agent Receipt 确认后清除首提示词密文的时间。
	PayloadClearedAt *time.Time `gorm:"column:payload_cleared_at"`
	// Status Outbox 派发状态。
	Status string `gorm:"column:status"`
	// AvailableAt 允许 Dispatcher 领取的最早时间。
	AvailableAt time.Time `gorm:"column:available_at"`
	// LeaseOwner 当前短租约 Owner。
	LeaseOwner *string `gorm:"column:lease_owner"`
	// LeaseVersion 当前租约 Fencing 版本。
	LeaseVersion int64 `gorm:"column:lease_version"`
	// LeaseExpiresAt 当前短租约过期时间。
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
	// AttemptCount 已开始的派发尝试次数。
	AttemptCount int32 `gorm:"column:attempt_count"`
	// MaxAttempts 允许的最大派发尝试次数。
	MaxAttempts int32 `gorm:"column:max_attempts"`
	// DeliveredAt Agent 确认原命令回执的时间。
	DeliveredAt *time.Time `gorm:"column:delivered_at"`
	// CreatedAt Outbox 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt Outbox 最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Project Session Outbox 权威表名。
func (projectSessionOutboxModel) TableName() string {
	return "business.project_session_outbox"
}

// quickCreateModels 聚合一次事务写入所需的四个持久化模型，不对外暴露 GORM 类型。
type quickCreateModels struct {
	// Project 待创建 Project 的持久化模型。
	Project projectModel
	// Receipt 待占有幂等键的回执持久化模型。
	Receipt projectCreationReceiptModel
	// Binding 待创建 Session 绑定的持久化模型。
	Binding projectSessionBindingModel
	// Outbox 待创建 Session 初始化 Outbox 的持久化模型。
	Outbox projectSessionOutboxModel
}

// projectBootstrapReadDTO 承载 Project 与默认 Session 绑定的一次 JOIN 结果，不参与 GORM 持久化。
type projectBootstrapReadDTO struct {
	// ProjectID 当前用户拥有的 Project 标识。
	ProjectID string `gorm:"column:project_id"`
	// Title Project 的安全展示标题。
	Title string `gorm:"column:title"`
	// LifecycleStatus Project 生命周期状态。
	LifecycleStatus string `gorm:"column:lifecycle_status"`
	// RecentRunStatus Project 最近运行摘要。
	RecentRunStatus string `gorm:"column:recent_run_status"`
	// InitialPromptStatus Project 首提示词初始化状态。
	InitialPromptStatus string `gorm:"column:initial_prompt_status"`
	// ProvisioningStatus 默认 Session 初始化状态。
	ProvisioningStatus string `gorm:"column:provisioning_status"`
	// AgentSessionID 已确认的 Agent Session 标识。
	AgentSessionID *string `gorm:"column:agent_session_id"`
	// AgentInputID 已确认的 Agent 首 Input 标识。
	AgentInputID *string `gorm:"column:agent_input_id"`
	// LastErrorCode 永久失败时的稳定代码。
	LastErrorCode *string `gorm:"column:last_error_code"`
	// ProjectUpdatedAt Project 最近更新时间。
	ProjectUpdatedAt time.Time `gorm:"column:project_updated_at"`
	// BindingUpdatedAt Session 绑定最近更新时间。
	BindingUpdatedAt time.Time `gorm:"column:binding_updated_at"`
}

// agentSessionAccessReadDTO 承载 BFF 所有权 JOIN 的最小授权结果，不包含 Project 展示字段或密文。
type agentSessionAccessReadDTO struct {
	// ProjectID 是当前用户拥有且仍可读取的 Project UUIDv7。
	ProjectID string `gorm:"column:project_id"`
	// AgentSessionID 是 ready Binding 确认且与路由完全匹配的 Agent Session UUIDv7。
	AgentSessionID string `gorm:"column:agent_session_id"`
}

// projectBootstrapResult 显式映射一次 JOIN 结果，并拒绝数据库中的未知状态或非法 Agent 引用。
func projectBootstrapResult(record projectBootstrapReadDTO) (project.BootstrapResult, error) {
	updatedAt := record.ProjectUpdatedAt
	if record.BindingUpdatedAt.After(updatedAt) {
		updatedAt = record.BindingUpdatedAt
	}
	result := project.BootstrapResult{
		ProjectID: record.ProjectID, Title: record.Title,
		LifecycleStatus: project.LifecycleStatus(record.LifecycleStatus), RecentRunStatus: project.RecentRunStatus(record.RecentRunStatus),
		InitialPromptStatus: project.InitialPromptStatus(record.InitialPromptStatus), ProvisioningStatus: project.ProvisioningStatus(record.ProvisioningStatus),
		AgentSessionID: record.AgentSessionID, AgentInputID: record.AgentInputID, LastErrorCode: record.LastErrorCode, UpdatedAt: updatedAt,
	}
	if err := result.Validate(); err != nil {
		return project.BootstrapResult{}, fmt.Errorf("map project bootstrap read model: %w", err)
	}
	return result, nil
}

// quickCreateModelsFromAggregate 在事务开始前校验聚合并显式映射四类持久化模型。
func quickCreateModelsFromAggregate(aggregate project.QuickCreateAggregate) (quickCreateModels, error) {
	if err := aggregate.Validate(); err != nil {
		return quickCreateModels{}, fmt.Errorf("map quick create aggregate to persistence models: %w", err)
	}
	return quickCreateModels{
		Project: projectModelFromEntity(aggregate.Project),
		Receipt: projectCreationReceiptModelFromEntity(aggregate.Receipt),
		Binding: projectSessionBindingModelFromEntity(aggregate.Binding),
		Outbox:  projectSessionOutboxModelFromEntity(aggregate.Outbox),
	}, nil
}

// projectModelFromEntity 将已由聚合校验的 Project 显式映射为持久化模型。
func projectModelFromEntity(entity project.Project) projectModel {
	return projectModel{
		ID: entity.ID, OwnerUserID: entity.OwnerUserID, Title: entity.Title,
		LifecycleStatus: string(entity.LifecycleStatus), RecentRunStatus: string(entity.RecentRunStatus),
		InitialPromptStatus: string(entity.InitialPromptStatus), Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

// projectEntity 将持久化模型恢复为 Project，并复用快速创建初始约束之外的字段校验。
func projectEntity(model projectModel) (project.Project, error) {
	entity := project.Project{
		ID: model.ID, OwnerUserID: model.OwnerUserID, Title: model.Title,
		LifecycleStatus: project.LifecycleStatus(model.LifecycleStatus), RecentRunStatus: project.RecentRunStatus(model.RecentRunStatus),
		InitialPromptStatus: project.InitialPromptStatus(model.InitialPromptStatus), Version: model.Version,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if entity.ID == "" || entity.OwnerUserID == "" || entity.Version < 1 || entity.CreatedAt.IsZero() || entity.UpdatedAt.Before(entity.CreatedAt) {
		return project.Project{}, fmt.Errorf("map persistence model to project: %w", project.ErrInvalidQuickCreate)
	}
	return entity, nil
}

// projectCreationReceiptModelFromEntity 显式映射首次安全响应和两个摘要。
func projectCreationReceiptModelFromEntity(entity project.CreationReceipt) projectCreationReceiptModel {
	return projectCreationReceiptModel{
		ID: entity.ID, OwnerUserID: entity.OwnerUserID, CommandType: entity.CommandType,
		KeyDigest: append([]byte(nil), entity.KeyDigest[:]...), SemanticDigest: append([]byte(nil), entity.SemanticDigest[:]...),
		ProjectID: entity.ProjectID, LifecycleStatus: string(entity.LifecycleStatus), RecentRunStatus: string(entity.RecentRunStatus),
		SessionProvisioningStatus: string(entity.SessionProvisioningStatus), InitialPromptStatus: string(entity.InitialPromptStatus),
		CreatedAt: entity.CreatedAt,
	}
}

// projectCreationReceiptEntity 将数据库回执恢复为领域回执，用于幂等重放和语义冲突判断。
func projectCreationReceiptEntity(model projectCreationReceiptModel) (project.CreationReceipt, error) {
	if len(model.KeyDigest) != len(project.Digest{}) || len(model.SemanticDigest) != len(project.Digest{}) {
		return project.CreationReceipt{}, fmt.Errorf("map persistence model to project creation receipt: %w", project.ErrInvalidQuickCreate)
	}
	entity := project.CreationReceipt{
		ID: model.ID, OwnerUserID: model.OwnerUserID, CommandType: model.CommandType, ProjectID: model.ProjectID,
		LifecycleStatus: project.LifecycleStatus(model.LifecycleStatus), RecentRunStatus: project.RecentRunStatus(model.RecentRunStatus),
		SessionProvisioningStatus: project.ProvisioningStatus(model.SessionProvisioningStatus),
		InitialPromptStatus:       project.InitialPromptStatus(model.InitialPromptStatus), CreatedAt: model.CreatedAt,
	}
	copy(entity.KeyDigest[:], model.KeyDigest)
	copy(entity.SemanticDigest[:], model.SemanticDigest)
	return entity, nil
}

// projectSessionBindingModelFromEntity 显式映射 Project 与 Agent Session 的跨数据库逻辑绑定。
func projectSessionBindingModelFromEntity(entity project.SessionBinding) projectSessionBindingModel {
	return projectSessionBindingModel{
		ID: entity.ID, ProjectID: entity.ProjectID, CommandID: entity.CommandID,
		RequestDigest: append([]byte(nil), entity.RequestDigest[:]...), AgentSessionID: entity.AgentSessionID,
		AgentInputID: entity.AgentInputID, ProvisioningStatus: string(entity.ProvisioningStatus), LastErrorCode: entity.LastErrorCode,
		Version: entity.Version, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

// projectSessionOutboxModelFromEntity 显式映射 Outbox，并保证空 Prompt 不产生任何加密负载列。
func projectSessionOutboxModelFromEntity(entity project.SessionOutbox) projectSessionOutboxModel {
	emptySnapshotDigest := project.SHA256Digest([]byte("[]"))
	model := projectSessionOutboxModel{
		ID: entity.ID, EventType: entity.EventType, SchemaVersion: entity.SchemaVersion,
		AggregateID: entity.AggregateID, OwnerUserID: entity.OwnerUserID,
		RequestDigest: append([]byte(nil), entity.RequestDigest[:]...), HasInitialPrompt: entity.HasInitialPrompt,
		Status: string(entity.Status), AvailableAt: entity.AvailableAt, LeaseOwner: entity.LeaseOwner,
		LeaseVersion: entity.LeaseVersion, LeaseExpiresAt: entity.LeaseExpiresAt, AttemptCount: entity.AttemptCount,
		MaxAttempts: entity.MaxAttempts, DeliveredAt: entity.DeliveredAt, PayloadClearedAt: entity.PayloadClearedAt,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
	if entity.SchemaVersion == project.EnsureSessionSchemaVersionV2 {
		model.SkillSnapshotDigest = append([]byte(nil), entity.SkillSnapshotDigest[:]...)
		model.SkillCount = entity.SkillCount
		model.BindingSetVersion = entity.BindingSetVersion
		model.ResolutionID = entity.ResolutionID
	} else {
		model.SkillSnapshotDigest = append([]byte(nil), emptySnapshotDigest[:]...)
	}
	if entity.EncryptedPayload != nil {
		model.PayloadDigest = append([]byte(nil), entity.EncryptedPayload.PayloadDigest[:]...)
		if entity.PayloadClearedAt == nil {
			algorithm := entity.EncryptedPayload.Algorithm
			keyVersion := entity.EncryptedPayload.KeyVersion
			model.PayloadEncryptionAlgorithm = &algorithm
			model.PayloadKeyVersion = &keyVersion
			model.PayloadNonce = append([]byte(nil), entity.EncryptedPayload.Nonce...)
			model.PayloadCiphertext = append([]byte(nil), entity.EncryptedPayload.Ciphertext...)
		}
	}
	return model
}

// projectSessionOutboxEntity 将数据库 Outbox 深拷贝为领域实体，并拒绝摘要长度、状态或密文三态异常。
func projectSessionOutboxEntity(model projectSessionOutboxModel) (project.SessionOutbox, error) {
	if len(model.RequestDigest) != len(project.Digest{}) || len(model.SkillSnapshotDigest) != len(project.Digest{}) {
		return project.SessionOutbox{}, fmt.Errorf("map project session outbox: %w", project.ErrInvalidQuickCreate)
	}
	entity := project.SessionOutbox{
		ID: model.ID, EventType: model.EventType, SchemaVersion: model.SchemaVersion,
		AggregateID: model.AggregateID, OwnerUserID: model.OwnerUserID, HasInitialPrompt: model.HasInitialPrompt,
		PayloadClearedAt: model.PayloadClearedAt, Status: project.OutboxStatus(model.Status), AvailableAt: model.AvailableAt,
		LeaseOwner: model.LeaseOwner, LeaseVersion: model.LeaseVersion, LeaseExpiresAt: model.LeaseExpiresAt,
		AttemptCount: model.AttemptCount, MaxAttempts: model.MaxAttempts, DeliveredAt: model.DeliveredAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	copy(entity.RequestDigest[:], model.RequestDigest)
	if model.SchemaVersion == project.EnsureSessionSchemaVersionV2 {
		if len(model.PayloadDigest) != len(project.Digest{}) {
			return project.SessionOutbox{}, fmt.Errorf("map project v2 session outbox payload digest: %w", project.ErrInvalidQuickCreate)
		}
		copy(entity.SkillSnapshotDigest[:], model.SkillSnapshotDigest)
		entity.SkillCount = model.SkillCount
		entity.BindingSetVersion = model.BindingSetVersion
		entity.ResolutionID = model.ResolutionID
		payload := &project.EncryptedPayload{}
		copy(payload.PayloadDigest[:], model.PayloadDigest)
		if model.PayloadClearedAt == nil {
			if model.PayloadEncryptionAlgorithm == nil || model.PayloadKeyVersion == nil {
				return project.SessionOutbox{}, fmt.Errorf("map project v2 session outbox encryption metadata: %w", project.ErrInvalidQuickCreate)
			}
			payload.Algorithm = *model.PayloadEncryptionAlgorithm
			payload.KeyVersion = *model.PayloadKeyVersion
			payload.Nonce = append([]byte(nil), model.PayloadNonce...)
			payload.Ciphertext = append([]byte(nil), model.PayloadCiphertext...)
		}
		entity.EncryptedPayload = payload
	} else {
		emptySnapshotDigest := project.SHA256Digest([]byte("[]"))
		if !bytes.Equal(model.SkillSnapshotDigest, emptySnapshotDigest[:]) || model.SkillCount != 0 ||
			model.BindingSetVersion != nil || model.ResolutionID != nil {
			return project.SessionOutbox{}, fmt.Errorf("map project v1 session outbox empty snapshot metadata: %w", project.ErrInvalidQuickCreate)
		}
	}
	if model.SchemaVersion != project.EnsureSessionSchemaVersionV2 && model.HasInitialPrompt {
		if len(model.PayloadDigest) != len(project.Digest{}) {
			return project.SessionOutbox{}, fmt.Errorf("map project session outbox payload digest: %w", project.ErrInvalidQuickCreate)
		}
		payload := &project.EncryptedPayload{}
		copy(payload.PayloadDigest[:], model.PayloadDigest)
		if model.PayloadClearedAt == nil {
			if model.PayloadEncryptionAlgorithm == nil || model.PayloadKeyVersion == nil {
				return project.SessionOutbox{}, fmt.Errorf("map project session outbox encryption metadata: %w", project.ErrInvalidQuickCreate)
			}
			payload.Algorithm = *model.PayloadEncryptionAlgorithm
			payload.KeyVersion = *model.PayloadKeyVersion
			payload.Nonce = append([]byte(nil), model.PayloadNonce...)
			payload.Ciphertext = append([]byte(nil), model.PayloadCiphertext...)
		}
		entity.EncryptedPayload = payload
	}
	if err := entity.Validate(); err != nil {
		return project.SessionOutbox{}, fmt.Errorf("map project session outbox entity: %w", err)
	}
	return entity, nil
}
