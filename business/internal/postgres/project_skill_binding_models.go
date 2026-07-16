package postgres

import (
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
)

// projectSkillBindingSetModel 映射 Project 期望 Skill 集合聚合根；空集合也必须存在一行。
type projectSkillBindingSetModel struct {
	// ProjectID 是 Project 逻辑关联与本表主键。
	ProjectID string `gorm:"column:project_id;type:uuid;primaryKey"`
	// OwnerUserID 是冻结的可信 Project Owner。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// SchemaVersion 是绑定集合结构版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// SetVersion 是集合 CAS 版本。
	SetVersion int64 `gorm:"column:set_version"`
	// SelectionDigest 是启用绑定 Canonical 摘要。
	SelectionDigest []byte `gorm:"column:selection_digest"`
	// EnabledCount 是启用 Skill 数量。
	EnabledCount int `gorm:"column:enabled_count"`
	// CreatedAt 是集合创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是集合最近语义变更时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Project Skill Binding Set 权威表名。
func (projectSkillBindingSetModel) TableName() string { return "business.project_skill_binding_set" }

// projectSkillBindingModel 映射 Project 与 Skill 当前绑定行；W1 初始创建只写 enabled/user/100。
type projectSkillBindingModel struct {
	// ID 是 Binding UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是 Project 逻辑关联。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// SkillID 是 Skill 逻辑关联。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// Namespace 是 W1 固定 user。
	Namespace string `gorm:"column:namespace"`
	// Priority 是 W1 固定 100。
	Priority int `gorm:"column:priority"`
	// Status 是 enabled 或 disabled。
	Status string `gorm:"column:status"`
	// Source 是 quick_create 或 owner_replace。
	Source string `gorm:"column:source"`
	// EnabledByUserID 是最近启用 Owner。
	EnabledByUserID string `gorm:"column:enabled_by_user_id;type:uuid"`
	// EnabledAt 是最近启用时间。
	EnabledAt time.Time `gorm:"column:enabled_at"`
	// DisabledByUserID 是最近停用 Owner；启用时为空。
	DisabledByUserID *string `gorm:"column:disabled_by_user_id;type:uuid"`
	// DisabledAt 是最近停用时间；启用时为空。
	DisabledAt *time.Time `gorm:"column:disabled_at"`
	// Version 是绑定行 CAS 版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 是绑定创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是绑定最近变更时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Project Skill Binding 权威表名。
func (projectSkillBindingModel) TableName() string { return "business.project_skill_binding" }

// projectSkillBindingAuditModel 映射追加式 Binding 审计事实。
type projectSkillBindingAuditModel struct {
	// ID 是审计 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是所属 Project。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// BindingID 是关联 Binding。
	BindingID string `gorm:"column:binding_id;type:uuid"`
	// SkillID 是关联 Skill。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// BindingSetVersion 是动作后集合版本。
	BindingSetVersion int64 `gorm:"column:binding_set_version"`
	// Action 是稳定审计动作。
	Action string `gorm:"column:action"`
	// FromStatus 是动作前状态；首次启用为空。
	FromStatus *string `gorm:"column:from_status"`
	// ToStatus 是动作后状态。
	ToStatus string `gorm:"column:to_status"`
	// Source 是动作来源。
	Source string `gorm:"column:source"`
	// ActorUserID 是可信 Project Owner。
	ActorUserID string `gorm:"column:actor_user_id;type:uuid"`
	// CommandReceiptID 是同批项目创建回执。
	CommandReceiptID string `gorm:"column:command_receipt_id;type:uuid"`
	// ReasonCode 是可选安全原因代码。
	ReasonCode *string `gorm:"column:reason_code"`
	// OccurredAt 是动作时间。
	OccurredAt time.Time `gorm:"column:occurred_at"`
}

// TableName 返回 Project Skill Binding Audit 权威表名。
func (projectSkillBindingAuditModel) TableName() string {
	return "business.project_skill_binding_audit"
}

// projectSessionSkillResolutionModel 映射某个 Bootstrap command 冻结的不可变解析头。
type projectSessionSkillResolutionModel struct {
	// ID 是 Resolution UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// CommandID 是唯一 Bootstrap command_id。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// ProjectID 是 Project 逻辑关联。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// OwnerUserID 是冻结 Owner。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// BindingSetVersion 是集合版本。
	BindingSetVersion int64 `gorm:"column:binding_set_version"`
	// BindingSelectionDigest 是集合摘要。
	BindingSelectionDigest []byte `gorm:"column:binding_selection_digest"`
	// SnapshotSchemaVersion 是 Snapshot 结构版本。
	SnapshotSchemaVersion string `gorm:"column:snapshot_schema_version"`
	// SnapshotKind 是 empty 或 published_refs。
	SnapshotKind string `gorm:"column:snapshot_kind"`
	// SkillCount 是冻结 Skill 数量。
	SkillCount int `gorm:"column:skill_count"`
	// SnapshotSetDigest 是 metadata 集合摘要。
	SnapshotSetDigest []byte `gorm:"column:snapshot_set_digest"`
	// RuntimePolicyRef 是固定安全策略引用。
	RuntimePolicyRef string `gorm:"column:runtime_policy_ref"`
	// ResolvedAt 是解析冻结时间。
	ResolvedAt time.Time `gorm:"column:resolved_at"`
}

// TableName 返回 Project Session Skill Resolution 权威表名。
func (projectSessionSkillResolutionModel) TableName() string {
	return "business.project_session_skill_resolution"
}

// projectSessionSkillResolutionItemModel 映射 Resolution metadata；Runtime Content 明文不进入本表。
type projectSessionSkillResolutionItemModel struct {
	// ResolutionID 是所属解析头。
	ResolutionID string `gorm:"column:resolution_id;type:uuid;primaryKey"`
	// ProjectID 是所属 Project。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// CommandID 是所属 Bootstrap command_id。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// LoadOrder 是稠密加载顺序。
	LoadOrder int `gorm:"column:load_order;primaryKey"`
	// Priority 是冻结优先级。
	Priority int `gorm:"column:priority"`
	// Namespace 是冻结 namespace。
	Namespace string `gorm:"column:namespace"`
	// BindingID 是来源 Binding。
	BindingID string `gorm:"column:binding_id;type:uuid"`
	// BindingVersion 是来源 Binding 版本。
	BindingVersion int64 `gorm:"column:binding_version"`
	// SkillID 是冻结 Skill。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// PublisherUserID 是冻结 Skill Owner，public-market 场景允许与 Project Owner 不同。
	PublisherUserID string `gorm:"column:publisher_user_id;type:uuid"`
	// PublishedSnapshotID 是冻结发布快照。
	PublishedSnapshotID string `gorm:"column:published_snapshot_id;type:uuid"`
	// PublicationRevision 是冻结发布修订。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// DefinitionSchemaVersion 是完整定义版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// ContentDigest 是完整定义摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// RuntimeContentSchemaVersion 是运行时投影版本。
	RuntimeContentSchemaVersion string `gorm:"column:runtime_content_schema_version"`
	// RuntimeContentDigest 是运行时投影摘要。
	RuntimeContentDigest []byte `gorm:"column:runtime_content_digest"`
	// AllowedGraphToolKeys 是声明键 JSONB 数组。
	AllowedGraphToolKeys jsonbValue `gorm:"column:allowed_graph_tool_keys;type:jsonb"`
	// PublicToolRefs 是 W1 固定空 JSONB 数组。
	PublicToolRefs jsonbValue `gorm:"column:public_tool_refs;type:jsonb"`
	// PermissionSnapshotDigest 是 v1 owner-private 或 v2 public-market 权限摘要。
	PermissionSnapshotDigest []byte `gorm:"column:permission_snapshot_digest"`
	// RuntimePolicyRef 是固定安全策略引用。
	RuntimePolicyRef string `gorm:"column:runtime_policy_ref"`
	// GovernanceEpoch 是治理纪元。
	GovernanceEpoch int64 `gorm:"column:governance_epoch"`
	// PublishedAtUnixMS 是发布 Unix 毫秒。
	PublishedAtUnixMS int64 `gorm:"column:published_at_unix_ms"`
	// CreatedAt 是解析项创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Project Session Skill Resolution Item 权威表名。
func (projectSessionSkillResolutionItemModel) TableName() string {
	return "business.project_session_skill_resolution_item"
}

// projectCreationReceiptV2Model 映射显式 QuickCreate v2 冻结响应；与 v1 共享表但不改变 v1 Model。
type projectCreationReceiptV2Model struct {
	// ID 是首次 QuickCreate 回执 UUIDv7。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// OwnerUserID 是发起创建的可信 Owner。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// CommandType 与 v1 一致固定为 quick_create。
	CommandType string `gorm:"column:command_type"`
	// KeyDigest 是原始幂等键摘要。
	KeyDigest []byte `gorm:"column:key_digest"`
	// SemanticDigest 是 Prompt 与 Binding Set 的 v2 语义摘要。
	SemanticDigest []byte `gorm:"column:semantic_digest"`
	// ProjectID 是首次命令创建的 Project。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// LifecycleStatus 是首次安全响应的生命周期。
	LifecycleStatus string `gorm:"column:lifecycle_status"`
	// RecentRunStatus 是首次安全响应的运行摘要。
	RecentRunStatus string `gorm:"column:recent_run_status"`
	// SessionProvisioningStatus 是首次安全响应的初始化状态。
	SessionProvisioningStatus string `gorm:"column:session_provisioning_status"`
	// InitialPromptStatus 是首次安全响应的 Prompt 状态。
	InitialPromptStatus string `gorm:"column:initial_prompt_status"`
	// CreatedAt 是首次命令提交时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// RequestSchemaVersion 明确区分 v1 和显式 v2。
	RequestSchemaVersion string `gorm:"column:request_schema_version"`
	// SkillSnapshotDigest 是首次冻结 Snapshot 摘要。
	SkillSnapshotDigest []byte `gorm:"column:skill_snapshot_digest"`
	// SkillCount 是首次冻结 Skill 数量。
	SkillCount int `gorm:"column:skill_count"`
	// BindingSetVersion 是 v2 冻结集合版本。
	BindingSetVersion *int64 `gorm:"column:binding_set_version"`
	// ResolutionID 是 v2 冻结 Resolution 逻辑引用。
	ResolutionID *string `gorm:"column:resolution_id;type:uuid"`
}

// TableName 返回共享 Project Creation Receipt 权威表名。
func (projectCreationReceiptV2Model) TableName() string { return "business.project_creation_receipt" }

// projectSessionBindingV2Model 映射 v2 默认 Agent Session 初始化绑定。
type projectSessionBindingV2Model struct {
	// ID 是 Session Binding UUIDv7。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是所属 Business Project。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// CommandID 是 Agent Session 初始化 command_id。
	CommandID string `gorm:"column:command_id;type:uuid"`
	// RequestDigest 是 EnsureProjectSessionV2 语义摘要。
	RequestDigest []byte `gorm:"column:request_digest"`
	// AgentSessionID 是交付后 Agent 权威 Session 引用。
	AgentSessionID *string `gorm:"column:agent_session_id;type:uuid"`
	// AgentInputID 是交付后可选首 Input 引用。
	AgentInputID *string `gorm:"column:agent_input_id;type:uuid"`
	// ProvisioningStatus 是跨数据库最终一致初始化状态。
	ProvisioningStatus string `gorm:"column:provisioning_status"`
	// LastErrorCode 是最近稳定安全错误码。
	LastErrorCode *string `gorm:"column:last_error_code"`
	// Version 是 Session Binding CAS 版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 是绑定创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是绑定最近变更时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// RequestSchemaVersion 固定为 ensure_project_session.v2。
	RequestSchemaVersion string `gorm:"column:request_schema_version"`
	// SkillSnapshotDigest 是创建时冻结 Snapshot 摘要。
	SkillSnapshotDigest []byte `gorm:"column:skill_snapshot_digest"`
	// SkillCount 是创建时冻结 Skill 数量。
	SkillCount int `gorm:"column:skill_count"`
	// BindingSetVersion 是创建时冻结集合版本。
	BindingSetVersion *int64 `gorm:"column:binding_set_version"`
	// ResolutionID 是创建时冻结 Resolution 逻辑引用。
	ResolutionID *string `gorm:"column:resolution_id;type:uuid"`
}

// TableName 返回共享 Project Session Binding 权威表名。
func (projectSessionBindingV2Model) TableName() string { return "business.project_session_binding" }

// projectSessionOutboxV2Model 映射完整加密 Bootstrap v2 Outbox；不包含任何明文 Runtime Content 字段。
type projectSessionOutboxV2Model struct {
	// ID 是 Outbox UUIDv7，同时作为稳定 Agent command_id。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// EventType 与 v1 一致为 agent.session.ensure。
	EventType string `gorm:"column:event_type"`
	// SchemaVersion 固定为完整 Bootstrap payload v2。
	SchemaVersion string `gorm:"column:schema_version"`
	// AggregateID 是所属 Business Project。
	AggregateID string `gorm:"column:aggregate_id;type:uuid"`
	// OwnerUserID 是冻结的可信 Project Owner。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// RequestDigest 是 EnsureProjectSessionV2 语义摘要。
	RequestDigest []byte `gorm:"column:request_digest"`
	// HasInitialPrompt 表示加密 plaintext 是否包含非空首 Prompt。
	HasInitialPrompt bool `gorm:"column:has_initial_prompt"`
	// PayloadEncryptionAlgorithm 是用途隔离认证加密算法。
	PayloadEncryptionAlgorithm *string `gorm:"column:payload_encryption_algorithm"`
	// PayloadKeyVersion 是用途隔离密钥版本引用。
	PayloadKeyVersion *string `gorm:"column:payload_key_version"`
	// PayloadNonce 是单次认证加密随机数。
	PayloadNonce []byte `gorm:"column:payload_nonce"`
	// PayloadCiphertext 是完整 Bootstrap plaintext 密文及认证标签。
	PayloadCiphertext []byte `gorm:"column:payload_ciphertext"`
	// PayloadDigest 是完整 plaintext Canonical 摘要。
	PayloadDigest []byte `gorm:"column:payload_digest"`
	// PayloadClearedAt 是确认交付后清理密文材料时间。
	PayloadClearedAt *time.Time `gorm:"column:payload_cleared_at"`
	// Status 是 Outbox 权威派发状态。
	Status string `gorm:"column:status"`
	// AvailableAt 是允许领取的最早时间。
	AvailableAt time.Time `gorm:"column:available_at"`
	// LeaseOwner 是 processing 短租约 Owner。
	LeaseOwner *string `gorm:"column:lease_owner"`
	// LeaseVersion 是租约 Fencing 版本。
	LeaseVersion int64 `gorm:"column:lease_version"`
	// LeaseExpiresAt 是 processing 租约过期时间。
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
	// AttemptCount 是已开始派发尝试数。
	AttemptCount int32 `gorm:"column:attempt_count"`
	// MaxAttempts 是冻结有限尝试预算。
	MaxAttempts int32 `gorm:"column:max_attempts"`
	// DeliveredAt 是 Agent 回执确认时间。
	DeliveredAt *time.Time `gorm:"column:delivered_at"`
	// CreatedAt 是 Outbox 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Outbox 最近变更时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// SkillSnapshotDigest 是冻结 Snapshot 摘要。
	SkillSnapshotDigest []byte `gorm:"column:skill_snapshot_digest"`
	// SkillCount 是冻结 Skill 数量。
	SkillCount int `gorm:"column:skill_count"`
	// BindingSetVersion 是冻结 Binding Set 版本。
	BindingSetVersion *int64 `gorm:"column:binding_set_version"`
	// ResolutionID 是冻结 Resolution 逻辑引用。
	ResolutionID *string `gorm:"column:resolution_id;type:uuid"`
}

// TableName 返回共享 Project Session Outbox 权威表名。
func (projectSessionOutboxV2Model) TableName() string { return "business.project_session_outbox" }

// projectSkillPublishedReadRecord 是一次 JOIN 的数据库扫描形态；bytea 在映射时严格转换为固定 Digest。
type projectSkillPublishedReadRecord struct {
	// ProjectID 是所属 Project。
	ProjectID string `gorm:"column:project_id"`
	// ProjectOwnerUserID 是 Project 权威 Owner。
	ProjectOwnerUserID string `gorm:"column:project_owner_user_id"`
	// ProjectLifecycleStatus 是 Project 生命周期。
	ProjectLifecycleStatus string `gorm:"column:project_lifecycle_status"`
	// BindingID 是解析来源 Binding。
	BindingID string `gorm:"column:binding_id"`
	// BindingVersion 是 Binding 行版本。
	BindingVersion int64 `gorm:"column:binding_version"`
	// BindingStatus 是当前绑定状态。
	BindingStatus string `gorm:"column:binding_status"`
	// Namespace 是绑定 Skill namespace。
	Namespace string `gorm:"column:namespace"`
	// Priority 是绑定加载优先级。
	Priority int `gorm:"column:priority"`
	// SkillID 是被绑定 Skill。
	SkillID string `gorm:"column:skill_id"`
	// SkillOwnerUserID 是 Skill 权威 Owner。
	SkillOwnerUserID string `gorm:"column:skill_owner_user_id"`
	// PublisherUserID 是同一集合 SQL 关联到的 Publisher Account 标识。
	PublisherUserID string `gorm:"column:publisher_user_id"`
	// CurrentPublishedSnapshotID 是 Skill 当前发布指针。
	CurrentPublishedSnapshotID string `gorm:"column:current_published_snapshot_id"`
	// SkillPublicationRevision 是 Skill 当前发布序号。
	SkillPublicationRevision int64 `gorm:"column:skill_publication_revision"`
	// GovernanceStatus 是 Skill 治理状态。
	GovernanceStatus string `gorm:"column:governance_status"`
	// GovernanceEpoch 是 Skill 当前治理纪元。
	GovernanceEpoch int64 `gorm:"column:governance_epoch"`
	// PublishedSnapshotID 是 JOIN 发布快照标识。
	PublishedSnapshotID string `gorm:"column:published_snapshot_id"`
	// PublishedSkillID 是发布快照所属 Skill。
	PublishedSkillID string `gorm:"column:published_skill_id"`
	// SourceContentRevisionID 是发布来源修订。
	SourceContentRevisionID string `gorm:"column:source_content_revision_id"`
	// PublishedPublicationRevision 是发布快照修订序号。
	PublishedPublicationRevision int64 `gorm:"column:published_publication_revision"`
	// DefinitionSchemaVersion 是发布定义版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// DefinitionJSON 是发布定义 JSONB 投影。
	DefinitionJSON []byte `gorm:"column:definition_json"`
	// ContentDigest 是发布定义摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// PublishedByReviewerUserID 是执行批准发布的 Reviewer，仅用于与 Publisher 身份隔离。
	PublishedByReviewerUserID string `gorm:"column:published_by_reviewer_user_id"`
	// PublishedAt 是发布时刻。
	PublishedAt time.Time `gorm:"column:published_at"`
	// RevisionID 是来源不可变修订。
	RevisionID string `gorm:"column:revision_id"`
	// RevisionSkillID 是来源修订所属 Skill。
	RevisionSkillID string `gorm:"column:revision_skill_id"`
	// RevisionDefinitionSchemaVersion 是来源修订定义版本。
	RevisionDefinitionSchemaVersion string `gorm:"column:revision_definition_schema_version"`
	// RevisionDefinitionJSON 是来源修订 JSONB 投影。
	RevisionDefinitionJSON []byte `gorm:"column:revision_definition_json"`
	// RevisionContentDigest 是来源修订摘要。
	RevisionContentDigest []byte `gorm:"column:revision_content_digest"`
}

var _ projectskillbinding.QuickCreateV2Repository = (*ProjectRepository)(nil)
