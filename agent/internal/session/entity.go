// Package session 定义 Agent Session、Message、Input、Command Receipt 的领域契约与创建用例。
package session

import (
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
)

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
	// InputStatusResolved 表示输入已经产生并提交冻结终态。
	InputStatusResolved InputStatus = "resolved"
	// InputStatusDead 表示输入达到重试上限或遇到不可恢复失败。
	InputStatusDead InputStatus = "dead"
)

// CommandTypeEnsureProjectSessionV1 是 W0 Business→Agent 建会话命令类型。
const CommandTypeEnsureProjectSessionV1 = "ensure_project_session_v1"

// EnsureCommandSchemaVersionV1 是 W0 EnsureProjectSession RPC 请求的冻结契约版本。
const EnsureCommandSchemaVersionV1 = "ensure_project_session.v1"

// CreationSourceQuickCreate 表示 Session 初始化由 Business Quick Create 用户意图触发。
const CreationSourceQuickCreate = "quick_create"

// ResultVersionV1 是 Ensure 命令冻结回执结构版本。
const ResultVersionV1 = 1

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

// SkillSnapshot 是 Session 创建时不可变冻结的 Skill 引用集合。
type SkillSnapshot struct {
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// Kind 是快照类型；W0 必须为 empty。
	Kind SkillSnapshotKind
	// Digest 是规范化快照内容摘要。
	Digest string
	// PublishedSnapshotRefsJSON 是稳定 JSON 数组；W0 固定为 `[]`。
	PublishedSnapshotRefsJSON string
	// CreatedAt 是快照冻结 UTC 时间。
	CreatedAt time.Time
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
	// CompletedAt 是本地事务成功使用的冻结 UTC 时间。
	CompletedAt time.Time
}

// EnsurePlan 是 Repository 在一个 Agent 本地事务内提交的完整创建计划。
// 计划中的 Message/Input 必须同时存在或同时为空，Events 必须按固定投影顺序批量写入。
type EnsurePlan struct {
	// Session 是待创建 Session 聚合。
	Session Session
	// SkillSnapshot 是必须原子冻结的显式空快照。
	SkillSnapshot SkillSnapshot
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
	// Events 是严格有类型、无敏感正文的 EventLog 投影。
	Events []event.Record
}
