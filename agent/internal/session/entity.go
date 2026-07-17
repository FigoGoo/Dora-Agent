// Package session 定义 Agent Session、Message、Input、Command Receipt 的领域契约与创建用例。
package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
)

// Status 表示 Session 聚合生命周期状态，不承载 Runtime 忙闲状态。
type Status string

const (
	// StatusActive 表示 Session 可以接收后续持久化输入。
	StatusActive Status = "active"
	// StatusArchived 表示 Session 已归档，不再接收新输入。
	StatusArchived Status = "archived"
)

// SkillSnapshotKind 表示 Session 创建时冻结的 Skill 快照类型。
type SkillSnapshotKind string

const (
	// SkillSnapshotKindEmpty 表示 W0 显式冻结空 Skill 集合，而不是省略冻结事实。
	SkillSnapshotKindEmpty SkillSnapshotKind = "empty"
	// SkillSnapshotKindPublishedRefs 表示 W1 冻结一个或多个不可变 Published Skill 引用与加密 Runtime Content。
	SkillSnapshotKindPublishedRefs SkillSnapshotKind = "published_refs"
)

// MessageRole 表示持久化会话消息的受控角色。
type MessageRole string

const (
	// MessageRoleUser 表示由真实用户输入产生的消息。
	MessageRoleUser MessageRole = "user"
)

// InputSourceType 表示持久化 Session Input 的可信来源类型。
type InputSourceType string

const (
	// InputSourceTypeUserMessage 表示输入由用户消息产生。
	InputSourceTypeUserMessage InputSourceType = "user_message"
	// InputSourceTypeCreationSpecPreview 表示输入由已认证 Preview POST 的严格结构化 Intent 产生。
	InputSourceTypeCreationSpecPreview InputSourceType = "creation_spec_preview"
	// InputSourceTypeAnalyzeMaterialsPreview 表示输入由已认证素材分析 Preview POST 的严格 Intent 产生。
	InputSourceTypeAnalyzeMaterialsPreview InputSourceType = "analyze_materials_preview"
	// InputSourceTypePlanStoryboardPreview 表示输入由已认证 Storyboard Preview POST 的严格 Intent 产生。
	InputSourceTypePlanStoryboardPreview InputSourceType = "plan_storyboard_preview"
	// InputSourceTypeWritePromptsPreview 表示输入由已认证 Prompt Preview POST 的严格 Intent 产生。
	InputSourceTypeWritePromptsPreview InputSourceType = "write_prompts_preview"
	// InputSourceTypeGenerateMediaPreviewRequest 表示 generate_media typed ingress。
	InputSourceTypeGenerateMediaPreviewRequest InputSourceType = "generate_media_preview_request"
	// InputSourceTypeAssembleOutputPreviewRequest 表示 assemble_output typed ingress。
	InputSourceTypeAssembleOutputPreviewRequest InputSourceType = "assemble_output_preview_request"
	// InputSourceTypeMediaJobPreviewTerminal 表示 Worker 终态经 AppendOnce Bridge 返回 Session Lane。
	InputSourceTypeMediaJobPreviewTerminal InputSourceType = "media_job_preview_terminal"
)

// Valid 只接受当前持久化 Schema 已批准的 Input Source exact-set。
func (source InputSourceType) Valid() bool {
	switch source {
	case InputSourceTypeUserMessage, InputSourceTypeCreationSpecPreview, InputSourceTypeAnalyzeMaterialsPreview,
		InputSourceTypePlanStoryboardPreview, InputSourceTypeWritePromptsPreview,
		InputSourceTypeGenerateMediaPreviewRequest, InputSourceTypeAssembleOutputPreviewRequest,
		InputSourceTypeMediaJobPreviewTerminal:
		return true
	default:
		return false
	}
}

// MarshalJSON 拒绝把未知 Input Source 写入事件、快照或持久化 JSON。
func (source InputSourceType) MarshalJSON() ([]byte, error) {
	if !source.Valid() {
		return nil, fmt.Errorf("marshal session input source type: invalid value")
	}
	return json.Marshal(string(source))
}

// UnmarshalJSON 把 JSON 字符串严格映射到已批准的 Input Source exact-set。
func (source *InputSourceType) UnmarshalJSON(encoded []byte) error {
	if source == nil {
		return fmt.Errorf("unmarshal session input source type: nil destination")
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return fmt.Errorf("unmarshal session input source type: %w", err)
	}
	parsed := InputSourceType(value)
	if !parsed.Valid() {
		return fmt.Errorf("unmarshal session input source type: invalid value")
	}
	*source = parsed
	return nil
}

// InputStatus 表示 Session Input 的持久化处理状态。
type InputStatus string

const (
	// InputStatusPending 表示输入已可靠写入 PostgreSQL，等待后续 Runner 领取。
	InputStatusPending InputStatus = "pending"
	// InputStatusClaimed 表示输入已被具有有效 Lease/Fence 的 Processor 领取。
	InputStatusClaimed InputStatus = "claimed"
	// InputStatusRunning 表示 Runner 已开始处理输入。
	InputStatusRunning InputStatus = "running"
	// InputStatusRetryWait 表示可重试失败后的有界等待状态。
	InputStatusRetryWait InputStatus = "retry_wait"
	// InputStatusRecoveryPending 表示外部命令结果未知或终态投影尚待恢复，仍阻塞 Session HOL。
	InputStatusRecoveryPending InputStatus = "recovery_pending"
	// InputStatusResolved 表示输入已经产生并提交冻结终态。
	InputStatusResolved InputStatus = "resolved"
	// InputStatusDead 表示输入达到重试上限或遇到不可恢复失败。
	InputStatusDead InputStatus = "dead"
)

// CommandTypeEnsureProjectSessionV1 是 W0 Business→Agent 建会话命令类型。
const CommandTypeEnsureProjectSessionV1 = "ensure_project_session_v1"

// CommandTypeEnsureProjectSessionV2 是 W1 Business→Agent 携带冻结 Skill Snapshot 的建会话命令类型。
const CommandTypeEnsureProjectSessionV2 = "ensure_project_session_v2"

// EnsureCommandSchemaVersionV1 是 W0 EnsureProjectSession RPC 请求的冻结契约版本。
const EnsureCommandSchemaVersionV1 = "ensure_project_session.v1"

// CreationSourceQuickCreate 表示 Session 初始化由 Business Quick Create 用户意图触发。
const CreationSourceQuickCreate = "quick_create"

// ResultVersionV1 是 Ensure 命令冻结回执结构版本。
const ResultVersionV1 = 1

// ResultVersionV2 是携带 Skill Snapshot 摘要与数量的 Ensure v2 冻结回执结构版本。
const ResultVersionV2 = 2

// SkillSnapshotSchemaVersionV1 是 Agent 持久化 Header 与 Item 共同遵循的快照 Schema 版本。
const SkillSnapshotSchemaVersionV1 = "session_skill_snapshot.v1"

// EmptySkillSnapshotDigest 是规范化空 JSON 数组 `[]` 的 SHA-256 摘要。
const EmptySkillSnapshotDigest = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"

// Session 是 Agent 会话聚合根；同一 W0 Project 只能对应一个默认 Session。
type Session struct {
	// ID 是 Agent 应用生成的 Session UUIDv7。
	ID string
	// ProjectID 是 Business Project 的逻辑引用。
	ProjectID string
	// UserID 是 Business 已认证 User 的逻辑引用。
	UserID string
	// Status 是 Session 生命周期状态。
	Status Status
	// Version 是聚合乐观锁版本。
	Version int64
	// CreatedAt 是 Session 创建 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 是 Session 最近更新 UTC 时间。
	UpdatedAt time.Time
	// ArchivedAt 是 Session 归档 UTC 时间；活动状态为空。
	ArchivedAt *time.Time
}

// SkillSnapshot 是 Session 创建时不可变冻结的 Skill Snapshot Header。
type SkillSnapshot struct {
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// SchemaVersion 是快照持久化 Schema 版本；W0 回填与 W1 新写入均固定为 v1。
	SchemaVersion string
	// Kind 是快照类型；W0 必须为 empty。
	Kind SkillSnapshotKind
	// SkillCount 是冻结 Item 数量；empty 为 0，published_refs 必须大于 0。
	SkillCount int
	// Digest 是规范化快照内容摘要。
	Digest string
	// PublishedSnapshotRefsJSON 是稳定 JSON 数组；W0 固定为 `[]`。
	PublishedSnapshotRefsJSON string
	// CreatedAt 是快照冻结 UTC 时间。
	CreatedAt time.Time
}

// SkillSnapshotItem 是 Session 创建时冻结的单个 Published Skill 持久化事实。
// RuntimeContent 只允许保存使用 Agent Skill Snapshot 专用 purpose/AAD 生成的 AEAD Envelope，不保存明文。
type SkillSnapshotItem struct {
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// LoadOrder 是 Business 冻结的稠密加载顺序，从 1 开始。
	LoadOrder int
	// Priority 是 Business 冻结的非负优先级，Agent 不重新排序。
	Priority int
	// Namespace 是 system 或 user 稳定 token。
	Namespace string
	// SkillID 是 Business Skill UUIDv7 逻辑引用。
	SkillID string
	// PublisherUserID 是 Business 发布者 User UUIDv7 逻辑引用。
	PublisherUserID string
	// PublishedSnapshotID 是不可变 Published Snapshot UUIDv7 逻辑引用。
	PublishedSnapshotID string
	// PublicationRevision 是从 1 开始的冻结发布修订号。
	PublicationRevision int64
	// DefinitionSchemaVersion 是完整 Published Definition 的 Schema 版本。
	DefinitionSchemaVersion string
	// ContentDigest 是完整 Published Definition 的不透明 SHA-256 摘要。
	ContentDigest string
	// RuntimeContentSchemaVersion 是加密 Runtime Content 的 Schema 版本。
	RuntimeContentSchemaVersion string
	// RuntimeContentDigest 是 Runtime Content canonical 明文摘要，解密后必须重验。
	RuntimeContentDigest string
	// RuntimeContent 是带独立密钥版本的专用 AAD AEAD Envelope。
	RuntimeContent ProtectedContent
	// AllowedGraphToolKeysJSON 是冻结声明数组的稳定 JSON，不代表 Tool 已注册或可执行。
	AllowedGraphToolKeysJSON string
	// PublicToolRefsJSON 是冻结 Public Tool 引用数组；W1 必须为 `[]`。
	PublicToolRefsJSON string
	// PermissionSnapshotDigest 是 Business 权限快照的不透明 SHA-256 摘要。
	PermissionSnapshotDigest string
	// RuntimePolicyRef 是冻结的 Runtime Policy 版本引用。
	RuntimePolicyRef string
	// GovernanceEpoch 是冻结时治理纪元。
	GovernanceEpoch int64
	// PublishedAtUnixMS 是 Published Snapshot 发布时间的 Unix 毫秒原值。
	PublishedAtUnixMS int64
	// CreatedAt 是 Item 冻结 UTC 时间。
	CreatedAt time.Time
}

// StoredSkillSnapshot 是 Repository 至多两条 SQL 读取出的 Header 与有界 Item 集合。
// 该结构仍含密文，只有 Session Service 可以通过专用保护器解密并重验全部摘要。
type StoredSkillSnapshot struct {
	// Header 是不可变 Snapshot Header。
	Header SkillSnapshot
	// Items 按 load_order 升序排列，数量不得超过读取上限。
	Items []SkillSnapshotItem
}

// SequenceCounter 是 Message 和 Input 各自独立的会话级单调序号边界。
type SequenceCounter struct {
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// LastMessageSeq 是最近已提交的 Message 序号。
	LastMessageSeq int64
	// LastInputEnqueueSeq 是最近已提交的 Input 入队序号。
	LastInputEnqueueSeq int64
	// UpdatedAt 是计数器最近更新 UTC 时间。
	UpdatedAt time.Time
}

// ProtectedContent 是消息正文经 Agent 受控加密设施处理后的持久化值。
// Ciphertext 必须是版本化、自描述的 AEAD Envelope；算法、Nonce 和认证标签封装在 Envelope 内，KeyVersion 单独保存。
type ProtectedContent struct {
	// Ciphertext 是版本化、自描述 AEAD Envelope，内部必须包含算法标识、Nonce、密文和认证标签；禁止使用裸密文。
	Ciphertext []byte
	// KeyVersion 是 Envelope 使用的解密密钥版本引用，不包含密钥材料，也不替代 Envelope 内的算法和 Nonce。
	KeyVersion string
}

// Message 是会话追加式消息；正文只以受保护密文持久化。
type Message struct {
	// ID 是 Message UUIDv7。
	ID string
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// Seq 是会话内单调 Message 序号。
	Seq int64
	// Role 是受控消息角色。
	Role MessageRole
	// Content 是版本化自描述 AEAD Envelope 及独立密钥版本引用。
	Content ProtectedContent
	// ContentDigest 是规范化正文 SHA-256 摘要。
	ContentDigest string
	// SourceKind 是稳定消息来源类型。
	SourceKind string
	// SourceID 是来源命令 UUIDv7。
	SourceID string
	// CreatedAt 是消息创建 UTC 时间。
	CreatedAt time.Time
}

// Input 是先持久化后唤醒的 Session Lane 输入；W0 只创建 pending。
type Input struct {
	// ID 是 Input UUIDv7，同一业务输入技术重试不得替换。
	ID string
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// SourceType 是可信输入来源类型。
	SourceType InputSourceType
	// SourceID 是来源命令 UUIDv7，用于去重。
	SourceID string
	// MessageID 是用户输入关联 Message UUIDv7。
	MessageID string
	// Status 是持久化处理状态。
	Status InputStatus
	// EnqueueSeq 是会话内 Head-of-Line 入队序号。
	EnqueueSeq int64
	// Attempts 是已开始处理次数；W0 固定为 0。
	Attempts int
	// AvailableAt 是最早可领取 UTC 时间。
	AvailableAt time.Time
	// LeaseOwner 是输入领取实例；W0 为空。
	LeaseOwner *string
	// LeaseUntil 是输入领取到期时间；W0 为空。
	LeaseUntil *time.Time
	// FenceToken 是输入 Claim Fence；W0 为 0。
	FenceToken int64
	// CreatedAt 是输入创建 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 是输入最近更新 UTC 时间。
	UpdatedAt time.Time
}

// RuntimeLease 是 Session Lane 串行执行的独立 Lease/Fence 事实。
type RuntimeLease struct {
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// LeaseOwner 是当前处理实例；W0 初始化为空。
	LeaseOwner *string
	// LeaseUntil 是当前租约到期时间；W0 初始化为空。
	LeaseUntil *time.Time
	// FenceToken 是每次成功领取后单调递增的 Fence；W0 为 0。
	FenceToken int64
	// Version 是租约记录乐观锁版本。
	Version int64
	// UpdatedAt 是租约记录最近更新 UTC 时间。
	UpdatedAt time.Time
}

// CommandReceipt 是 Ensure 命令的 first-write-wins 冻结结果，不保存 Prompt 正文。
type CommandReceipt struct {
	// CommandID 是 Business 稳定命令 UUIDv7。
	CommandID string
	// CommandType 是稳定命令类型。
	CommandType string
	// RequestDigest 是 Agent 独立重算的语义摘要。
	RequestDigest string
	// SessionID 是冻结 Session 结果 UUIDv7。
	SessionID string
	// MessageID 是可选首 Message UUIDv7；空 Prompt 时为空。
	MessageID *string
	// InputID 是可选首 Input UUIDv7；空 Prompt 时为空。
	InputID *string
	// ResultVersion 是冻结结果结构版本。
	ResultVersion int
	// SkillSnapshotDigest 是命令冻结的 Snapshot set digest；V1 为规范空集合摘要。
	SkillSnapshotDigest string
	// SkillCount 是冻结 Snapshot Item 数量；V1 固定为 0。
	SkillCount int
	// CompletedAt 是本地事务成功使用的冻结 UTC 时间。
	CompletedAt time.Time
}

// EnsurePlan 是 Repository 在一个 Agent 本地事务内提交的 V1/V2 完整创建计划。
// 计划中的 Snapshot Items 已在事务前加密，Message/Input 必须同时存在或同时为空，Events 按固定投影顺序批量写入。
type EnsurePlan struct {
	// Session 是待创建 Session 聚合。
	Session Session
	// SkillSnapshot 是必须原子冻结的显式空快照。
	SkillSnapshot SkillSnapshot
	// SkillSnapshotItems 是一次批量写入的已加密 Snapshot Items；空 Snapshot 时为空切片。
	SkillSnapshotItems []SkillSnapshotItem
	// SequenceCounter 是 Message/Input 初始序号事实。
	SequenceCounter SequenceCounter
	// RuntimeLease 是 Session Lane 初始空租约事实。
	RuntimeLease RuntimeLease
	// Message 是非空 Prompt 对应首条用户消息。
	Message *Message
	// Input 是非空 Prompt 对应 pending 输入。
	Input *Input
	// Receipt 是与所有领域事实同事务提交的命令回执。
	Receipt CommandReceipt
	// UserMessageRuntime 是显式本地 Profile 开启时与首 Input 同事务冻结的最小 Turn/Context；空 Prompt 或关闭时为空。
	UserMessageRuntime *UserMessageRuntimePlan
	// Events 是严格有类型、无敏感正文的 EventLog 投影。
	Events []event.Record
}
