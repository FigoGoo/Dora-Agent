package workspace

import "time"

const (
	// SnapshotSchemaVersionV1 是 Agent Workspace Snapshot 的冻结版本。
	SnapshotSchemaVersionV1 = "session.workspace.v1"
	// EventEnvelopeSchemaVersionV1 是前端持久事件 Envelope 的冻结版本。
	EventEnvelopeSchemaVersionV1 = "workspace.event.v1"
	// StreamControlSchemaVersionV1 是 Ready/Reset 控制帧的冻结版本。
	StreamControlSchemaVersionV1 = "workspace.stream-control.v1"
)

// Snapshot 是授权后返回给同源 BFF 的完整 Workspace HTTP DTO。
type Snapshot struct {
	// SchemaVersion 固定为 session.workspace.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是 Business 身份断言携带的内部请求 UUIDv7。
	RequestID string `json:"request_id"`
	// Session 是不含 UserID 和内部引用的会话投影。
	Session SessionDTO `json:"session"`
	// Messages 是完成认证解密的有序消息数组，空集合编码为 []。
	Messages []MessageDTO `json:"messages"`
	// Inputs 是不含 Lease/Fence/Attempts/Source 的有序输入数组，空集合编码为 []。
	Inputs []InputDTO `json:"inputs"`
	// EventHighWatermark 是与 Snapshot 同一事务观察到的 Event 最大 Seq。
	EventHighWatermark int64 `json:"event_high_watermark"`
	// MinAvailableSeq 是当前在线可重放的最小 Event Seq。
	MinAvailableSeq int64 `json:"min_available_seq"`
}

// SessionDTO 是 Workspace 对外安全会话投影。
type SessionDTO struct {
	// ID 是 Agent Session UUIDv7。
	ID string `json:"id"`
	// ProjectID 是关联 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Status 是 active 或 archived。
	Status string `json:"status"`
	// Version 是 Session 聚合版本。
	Version int64 `json:"version"`
	// CreatedAt 是 Session 创建 UTC 时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 是 Session 最近更新 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// MessageDTO 是完成授权解密后的前端安全 Message 投影。
type MessageDTO struct {
	// ID 是 Message UUIDv7。
	ID string `json:"id"`
	// MessageSeq 是会话内消息序号。
	MessageSeq int64 `json:"message_seq"`
	// Role 是受控角色；W0 只允许 user。
	Role string `json:"role"`
	// Content 是经过 AEAD 与 Digest 双重校验的 UTF-8 明文。
	Content string `json:"content"`
	// CreatedAt 是 Message 创建 UTC 时间。
	CreatedAt time.Time `json:"created_at"`
}

// InputDTO 是隐藏内部处理字段后的 Session Input 投影。
type InputDTO struct {
	// ID 是 Input UUIDv7。
	ID string `json:"id"`
	// MessageID 是关联 Message UUIDv7。
	MessageID string `json:"message_id"`
	// SourceType 是可信来源类型。
	SourceType string `json:"source_type"`
	// Status 是当前持久化处理状态。
	Status string `json:"status"`
	// EnqueueSeq 是 Session Lane 入队序号。
	EnqueueSeq int64 `json:"enqueue_seq"`
	// AvailableAt 是最早可领取 UTC 时间。
	AvailableAt time.Time `json:"available_at"`
	// CreatedAt 是 Input 创建 UTC 时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 是 Input 最近更新 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectedEvent 是已经强类型校验并编码完成的单条 SSE 持久事件。
type ProjectedEvent struct {
	// Seq 同时用作 SSE id 和 JSON seq。
	Seq int64
	// Event 同时用作 SSE event 和 JSON event。
	Event string
	// Data 是冻结 workspace.event.v1 JSON，不包含内部 Source 字段。
	Data []byte
}

// EventBatch 是 Service 校验后的有界连续 Event 集合及读取边界。
type EventBatch struct {
	// LatestSeq 是此次读取观察到的 Event 高水位。
	LatestSeq int64
	// MinAvailableSeq 是在线可重放最小序号。
	MinAvailableSeq int64
	// Events 是严格连续且已安全投影的持久事件。
	Events []ProjectedEvent
}

// StreamReady 是补读追平后发送且不推进 Cursor 的控制帧。
type StreamReady struct {
	// SchemaVersion 固定为 workspace.stream-control.v1。
	SchemaVersion string `json:"schema_version"`
	// Event 固定为 stream.ready。
	Event string `json:"event"`
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// Cursor 是服务端已经成功 Flush 的最后持久 Event Seq。
	Cursor int64 `json:"cursor"`
	// MinAvailableSeq 是当前在线可重放最小序号。
	MinAvailableSeq int64 `json:"min_available_seq"`
	// LatestSeq 是 Ready 时观察到的 Event 高水位。
	LatestSeq int64 `json:"latest_seq"`
}

// StreamReset 是投影无法安全恢复时发送且不推进 Cursor 的控制帧。
type StreamReset struct {
	// SchemaVersion 固定为 workspace.stream-control.v1。
	SchemaVersion string `json:"schema_version"`
	// Event 固定为 stream.reset。
	Event string `json:"event"`
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// Reason 是 cursor_expired、event_gap 或 projection_invalid。
	Reason string `json:"reason"`
	// SnapshotRequired 固定为 true，要求前端完整回源。
	SnapshotRequired bool `json:"snapshot_required"`
	// MinAvailableSeq 是当前在线可重放最小序号。
	MinAvailableSeq int64 `json:"min_available_seq"`
	// LatestSeq 是 Reset 时观察到的 Event 高水位。
	LatestSeq int64 `json:"latest_seq"`
}
