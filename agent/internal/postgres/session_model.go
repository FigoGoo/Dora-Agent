package postgres

import "time"

// sessionModel 映射 agent.session，会话业务行为仅存在于 session Entity/Service。
type sessionModel struct {
	// ID 是 Session UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 是 Business Project 逻辑引用。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// UserID 是 Business User 逻辑引用。
	UserID string `gorm:"column:user_id;type:uuid"`
	// Status 是 Session 生命周期稳定状态码。
	Status string `gorm:"column:status"`
	// Version 是 Session 聚合乐观锁版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 是 Session 创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Session 最近更新 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// ArchivedAt 是可选 Session 归档 UTC 时间。
	ArchivedAt *time.Time `gorm:"column:archived_at"`
}

// TableName 返回 Session 的显式 Agent Schema 表名。
func (sessionModel) TableName() string { return "agent.session" }

// sessionSkillSnapshotModel 映射 Session 创建时不可变冻结的 Skill 快照。
type sessionSkillSnapshotModel struct {
	// SessionID 是 Session 逻辑引用和本表主键。
	SessionID string `gorm:"column:session_id;type:uuid;primaryKey"`
	// SchemaVersion 是快照 Header Schema 版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// SnapshotKind 是快照类型，W0 固定 empty。
	SnapshotKind string `gorm:"column:snapshot_kind"`
	// SkillCount 是不可变 Snapshot Item 数量。
	SkillCount int `gorm:"column:skill_count"`
	// SnapshotDigest 是规范化快照摘要。
	SnapshotDigest string `gorm:"column:snapshot_digest"`
	// PublishedSnapshotRefs 是冻结的 Published Skill 引用 JSON 数组。
	PublishedSnapshotRefs string `gorm:"column:published_snapshot_refs;type:jsonb"`
	// CreatedAt 是快照冻结 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Skill Snapshot 的显式 Agent Schema 表名。
func (sessionSkillSnapshotModel) TableName() string { return "agent.session_skill_snapshot" }

// sessionSkillSnapshotItemModel 映射不可变 Published Skill 元数据及 Agent 专用 AAD 加密 Runtime Content。
type sessionSkillSnapshotItemModel struct {
	// SessionID 是 Session 逻辑引用，与 LoadOrder 组成主键。
	SessionID string `gorm:"column:session_id;type:uuid;primaryKey"`
	// LoadOrder 是从 1 开始的稠密加载顺序。
	LoadOrder int `gorm:"column:load_order;primaryKey"`
	// Priority 是 Business 冻结的非负优先级。
	Priority int `gorm:"column:priority"`
	// Namespace 是 system 或 user 稳定 token。
	Namespace string `gorm:"column:namespace"`
	// SkillID 是 Business Skill 逻辑引用。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// PublisherUserID 是 Business 发布者 User 逻辑引用。
	PublisherUserID string `gorm:"column:publisher_user_id;type:uuid"`
	// PublishedSnapshotID 是不可变 Published Snapshot 逻辑引用。
	PublishedSnapshotID string `gorm:"column:published_snapshot_id;type:uuid"`
	// PublicationRevision 是冻结发布修订号。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// DefinitionSchemaVersion 是完整 Published Definition Schema 版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// ContentDigest 是完整 Published Definition 的不透明摘要。
	ContentDigest string `gorm:"column:content_digest"`
	// RuntimeContentSchemaVersion 是加密 Runtime Content Schema 版本。
	RuntimeContentSchemaVersion string `gorm:"column:runtime_content_schema_version"`
	// RuntimeContentDigest 是 Runtime Content canonical 明文摘要。
	RuntimeContentDigest string `gorm:"column:runtime_content_digest"`
	// RuntimeContentCiphertext 是专用 AAD 认证的 DRAE v1 Envelope。
	RuntimeContentCiphertext []byte `gorm:"column:runtime_content_ciphertext"`
	// RuntimeContentKeyVersion 是 Agent Skill Snapshot 专用密钥版本引用。
	RuntimeContentKeyVersion string `gorm:"column:runtime_content_key_version"`
	// AllowedGraphToolKeys 是冻结 Graph Tool 声明 JSON 数组。
	AllowedGraphToolKeys string `gorm:"column:allowed_graph_tool_keys;type:jsonb"`
	// PublicToolRefs 是 Public Tool 引用 JSON 数组；W1 必须为空。
	PublicToolRefs string `gorm:"column:public_tool_refs;type:jsonb"`
	// PermissionSnapshotDigest 是 Business 权限快照不透明摘要。
	PermissionSnapshotDigest string `gorm:"column:permission_snapshot_digest"`
	// RuntimePolicyRef 是冻结 Runtime Policy 引用。
	RuntimePolicyRef string `gorm:"column:runtime_policy_ref"`
	// GovernanceEpoch 是冻结时治理纪元。
	GovernanceEpoch int64 `gorm:"column:governance_epoch"`
	// PublishedAtUnixMS 是 Published Snapshot 发布时间 Unix 毫秒原值。
	PublishedAtUnixMS int64 `gorm:"column:published_at_unix_ms"`
	// CreatedAt 是 Item 冻结 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Session Skill Snapshot Item 的显式 Agent Schema 表名。
func (sessionSkillSnapshotItemModel) TableName() string {
	return "agent.session_skill_snapshot_item"
}

// sessionSequenceCounterModel 映射 Message 与 Input 的独立会话级序号计数器。
type sessionSequenceCounterModel struct {
	// SessionID 是 Session 逻辑引用和本表主键。
	SessionID string `gorm:"column:session_id;type:uuid;primaryKey"`
	// LastMessageSeq 是最近已提交 Message 序号。
	LastMessageSeq int64 `gorm:"column:last_message_seq"`
	// LastInputEnqueueSeq 是最近已提交 Input 入队序号。
	LastInputEnqueueSeq int64 `gorm:"column:last_input_enqueue_seq"`
	// UpdatedAt 是计数器最近更新 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Session Sequence Counter 的显式 Agent Schema 表名。
func (sessionSequenceCounterModel) TableName() string { return "agent.session_sequence_counter" }

// sessionMessageModel 映射追加式 Session Message，正文只保存密文。
type sessionMessageModel struct {
	// ID 是 Message UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SessionID 是 Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// MessageSeq 是会话内单调消息序号。
	MessageSeq int64 `gorm:"column:message_seq"`
	// Role 是受控消息角色。
	Role string `gorm:"column:role"`
	// ContentCiphertext 是包含算法、Nonce、密文和认证标签的版本化自描述 AEAD Envelope。
	ContentCiphertext []byte `gorm:"column:content_ciphertext"`
	// ContentKeyVersion 是密钥版本引用。
	ContentKeyVersion string `gorm:"column:content_key_version"`
	// ContentDigest 是规范化正文摘要。
	ContentDigest string `gorm:"column:content_digest"`
	// SourceKind 是稳定消息来源类型。
	SourceKind string `gorm:"column:source_kind"`
	// SourceID 是来源命令 UUIDv7。
	SourceID string `gorm:"column:source_id;type:uuid"`
	// CreatedAt 是消息创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Session Message 的显式 Agent Schema 表名。
func (sessionMessageModel) TableName() string { return "agent.session_message" }

// sessionInputModel 映射先持久化后处理的 Session Input 状态。
type sessionInputModel struct {
	// ID 是 Input UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SessionID 是 Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// SourceType 是可信输入来源类型。
	SourceType string `gorm:"column:source_type"`
	// SourceID 是来源稳定 UUIDv7。
	SourceID string `gorm:"column:source_id;type:uuid"`
	// MessageID 是可选 Message 逻辑引用。
	MessageID *string `gorm:"column:message_id;type:uuid"`
	// Status 是 Input 处理状态。
	Status string `gorm:"column:status"`
	// EnqueueSeq 是会话内 Head-of-Line 序号。
	EnqueueSeq int64 `gorm:"column:enqueue_seq"`
	// Attempts 是处理尝试次数。
	Attempts int `gorm:"column:attempts"`
	// AvailableAt 是最早可领取 UTC 时间。
	AvailableAt time.Time `gorm:"column:available_at"`
	// LeaseOwner 是可选领取实例标识。
	LeaseOwner *string `gorm:"column:lease_owner"`
	// LeaseUntil 是可选领取到期 UTC 时间。
	LeaseUntil *time.Time `gorm:"column:lease_until"`
	// FenceToken 是 Input Claim Fence。
	FenceToken int64 `gorm:"column:fence_token"`
	// CreatedAt 是 Input 创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 Input 最近更新 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Session Input 的显式 Agent Schema 表名。
func (sessionInputModel) TableName() string { return "agent.session_input" }

// sessionRuntimeLeaseModel 映射 Session Lane 的独立 Lease/Fence 事实。
type sessionRuntimeLeaseModel struct {
	// SessionID 是 Session 逻辑引用和本表主键。
	SessionID string `gorm:"column:session_id;type:uuid;primaryKey"`
	// LeaseOwner 是可选处理实例标识。
	LeaseOwner *string `gorm:"column:lease_owner"`
	// LeaseUntil 是可选租约到期 UTC 时间。
	LeaseUntil *time.Time `gorm:"column:lease_until"`
	// FenceToken 是 Session Lane Fence。
	FenceToken int64 `gorm:"column:fence_token"`
	// Version 是租约乐观锁版本。
	Version int64 `gorm:"column:version"`
	// UpdatedAt 是租约最近更新 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Session Runtime Lease 的显式 Agent Schema 表名。
func (sessionRuntimeLeaseModel) TableName() string { return "agent.session_runtime_lease" }

// sessionCommandReceiptModel 映射 Ensure 命令 first-write-wins 冻结回执。
type sessionCommandReceiptModel struct {
	// CommandID 是 Business 命令 UUIDv7 主键。
	CommandID string `gorm:"column:command_id;type:uuid;primaryKey"`
	// CommandType 是稳定命令类型。
	CommandType string `gorm:"column:command_type"`
	// RequestDigest 是 Agent 独立重算的语义摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// SessionID 是冻结 Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// MessageID 是可选首 Message 逻辑引用。
	MessageID *string `gorm:"column:message_id;type:uuid"`
	// InputID 是可选首 Input 逻辑引用。
	InputID *string `gorm:"column:input_id;type:uuid"`
	// ResultVersion 是冻结结果结构版本。
	ResultVersion int `gorm:"column:result_version"`
	// SkillSnapshotDigest 是冻结 Session Skill Snapshot set digest。
	SkillSnapshotDigest string `gorm:"column:skill_snapshot_digest"`
	// SkillCount 是冻结 Snapshot Item 数量。
	SkillCount int `gorm:"column:skill_count"`
	// CompletedAt 是事务冻结 UTC 时间。
	CompletedAt time.Time `gorm:"column:completed_at"`
}

// TableName 返回 Session Command Receipt 的显式 Agent Schema 表名。
func (sessionCommandReceiptModel) TableName() string { return "agent.session_command_receipt" }

// sessionEventCounterModel 映射 EventLog 会话级序号与在线保留水位。
type sessionEventCounterModel struct {
	// SessionID 是 Session 逻辑引用和本表主键。
	SessionID string `gorm:"column:session_id;type:uuid;primaryKey"`
	// LastSeq 是最近已提交事件序号。
	LastSeq int64 `gorm:"column:last_seq"`
	// MinAvailableSeq 是在线可重放最小序号。
	MinAvailableSeq int64 `gorm:"column:min_available_seq"`
	// UpdatedAt 是计数器最近更新 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Session Event Counter 的显式 Agent Schema 表名。
func (sessionEventCounterModel) TableName() string { return "agent.session_event_counter" }

// sessionEventLogModel 映射追加式安全前端投影事件。
type sessionEventLogModel struct {
	// EventID 是 Event UUIDv7 主键。
	EventID string `gorm:"column:event_id;type:uuid;primaryKey"`
	// SessionID 是 Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// Seq 是会话内单调事件序号。
	Seq int64 `gorm:"column:seq"`
	// EventType 是严格白名单事件类型。
	EventType string `gorm:"column:event_type"`
	// SchemaVersion 是事件载荷版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// SourceKind 是稳定事件来源类型。
	SourceKind string `gorm:"column:source_kind"`
	// SourceID 是来源命令 UUIDv7。
	SourceID string `gorm:"column:source_id;type:uuid"`
	// ProjectionIndex 是同一来源投影固定顺序索引。
	ProjectionIndex int `gorm:"column:projection_index"`
	// AggregateType 是关联聚合类型。
	AggregateType string `gorm:"column:aggregate_type"`
	// AggregateID 是关联聚合 UUIDv7。
	AggregateID string `gorm:"column:aggregate_id;type:uuid"`
	// AggregateVersion 是事件观察到的聚合版本。
	AggregateVersion int64 `gorm:"column:aggregate_version"`
	// Payload 是经有类型 DTO 编码的安全 JSON。
	Payload string `gorm:"column:payload;type:jsonb"`
	// CreatedAt 是事件创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Session Event Log 的显式 Agent Schema 表名。
func (sessionEventLogModel) TableName() string { return "agent.session_event_log" }
