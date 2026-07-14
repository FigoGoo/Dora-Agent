// Package workspace 提供 Agent Workspace Snapshot 与持久 EventLog Tail 的只读用例。
package workspace

import (
	"context"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

// Identity 是通过 Business HMAC 断言得到的最小 User/Project/Session 三重读取绑定。
type Identity struct {
	// UserID 是已认证 Business Principal UUIDv7。
	UserID string
	// ProjectID 是 Business owned ready Binding 对应 Project UUIDv7。
	ProjectID string
	// SessionID 是此次读取允许访问的 Agent Session UUIDv7。
	SessionID string
}

// SnapshotLimits 描述 Repository 必须使用 limit+1 检测的集合边界。
type SnapshotLimits struct {
	// MaxMessages 是完整 Snapshot 允许的最大 Message 数。
	MaxMessages int
	// MaxInputs 是完整 Snapshot 允许的最大 Input 数。
	MaxInputs int
}

// SnapshotRecord 是一次只读一致性事务返回的内部加密投影，不得直接暴露给 HTTP。
type SnapshotRecord struct {
	// Session 是经过 User+Session JOIN 校验的会话事实。
	Session SessionRecord
	// Messages 是稳定排序的受保护消息集合。
	Messages []MessageRecord
	// Inputs 是稳定排序的输入集合。
	Inputs []InputRecord
	// EventHighWatermark 是与 Session/集合来自同一 Snapshot 的 Event 最大序号。
	EventHighWatermark int64
	// MinAvailableSeq 是同一 Snapshot 内在线可重放的最小 Event 序号。
	MinAvailableSeq int64
}

// SessionRecord 是 Repository→Service 的会话只读记录。
type SessionRecord struct {
	// ID 是 Agent Session UUIDv7。
	ID string
	// ProjectID 是关联 Business Project UUIDv7。
	ProjectID string
	// UserID 是关联 Business User UUIDv7，仅用于授权交叉校验。
	UserID string
	// Status 是 active 或 archived 生命周期状态。
	Status string
	// Version 是会话聚合版本。
	Version int64
	// CreatedAt 是会话创建 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 是会话最近更新 UTC 时间。
	UpdatedAt time.Time
}

// MessageRecord 是 Repository→Service 的受保护 Message 记录。
type MessageRecord struct {
	// ID 是 Message UUIDv7。
	ID string
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// Seq 是会话内 Message 单调序号。
	Seq int64
	// Role 是受控消息角色。
	Role string
	// Content 是 DRAE v1 Envelope 与明确 KeyVersion。
	Content session.ProtectedContent
	// ContentDigest 是持久化的明文 SHA-256 摘要。
	ContentDigest string
	// CreatedAt 是 Message 创建 UTC 时间。
	CreatedAt time.Time
}

// InputRecord 是 Repository→Service 的 Session Input 只读记录。
type InputRecord struct {
	// ID 是 Input UUIDv7。
	ID string
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// MessageID 是关联 Message UUIDv7；W0 user_message 必须存在。
	MessageID *string
	// SourceType 是可信输入来源类型。
	SourceType string
	// Status 是 Input 当前持久化状态。
	Status string
	// EnqueueSeq 是 Session Lane 入队序号。
	EnqueueSeq int64
	// AvailableAt 是最早可领取 UTC 时间。
	AvailableAt time.Time
	// CreatedAt 是 Input 创建 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 是 Input 最近更新 UTC 时间。
	UpdatedAt time.Time
}

// EventRecord 是 PostgreSQL EventLog 的内部完整记录；Source 字段不会进入前端 Envelope。
type EventRecord struct {
	// EventID 是 Event UUIDv7。
	EventID string
	// SessionID 是所属 Session UUIDv7。
	SessionID string
	// Seq 是会话内 Event 单调序号。
	Seq int64
	// EventType 是冻结白名单事件名。
	EventType string
	// SchemaVersion 是持久 Payload 版本。
	SchemaVersion string
	// AggregateType 是事件关联的权威聚合类型。
	AggregateType string
	// AggregateID 是事件关联聚合 UUIDv7。
	AggregateID string
	// AggregateVersion 是事件观察到的聚合版本。
	AggregateVersion int64
	// Payload 是 PostgreSQL JSONB 的原始字节，只能由强类型 Mapper 解码。
	Payload []byte
	// CreatedAt 是事件冻结 UTC 时间。
	CreatedAt time.Time
}

// EventBatchRecord 是一次 PostgreSQL 补读返回的边界与有界 Event 集合。
type EventBatchRecord struct {
	// LastSeq 是读取事务观察到的 Event 高水位。
	LastSeq int64
	// MinAvailableSeq 是在线可重放最小序号。
	MinAvailableSeq int64
	// Events 是严格按 Seq 升序的最多 BatchSize 条记录。
	Events []EventRecord
}

// Repository 定义 Workspace 消费的固定三查询 Snapshot 与有界 Event 批量读取。
type Repository interface {
	// LoadSnapshot 在 READ ONLY, REPEATABLE READ 事务中按 User+Session 加载固定三次集合查询。
	LoadSnapshot(ctx context.Context, identity Identity, limits SnapshotLimits) (SnapshotRecord, error)
	// LoadEventBatch 校验 User/Project/Session 后从 PostgreSQL 真源读取 seq>cursor 的有界连续候选集。
	LoadEventBatch(ctx context.Context, identity Identity, cursor int64, limit int) (EventBatchRecord, error)
}

// ContentDecryptor 是 Workspace 消费的最小只读正文解密接口。
type ContentDecryptor interface {
	// Open 必须完成 DRAE、Keyring、AEAD、UTF-8、长度和 Digest 全部校验。
	Open(ctx context.Context, protected session.ProtectedContent, contentDigest string) ([]byte, error)
}
