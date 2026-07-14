// Package event 定义 Agent 会话事件日志的稳定领域契约与安全投影载荷。
package event

import (
	"encoding/json"
	"fmt"
	"time"
)

// Type 表示会话 EventLog 中允许持久化的稳定事件类型。
type Type string

const (
	// TypeSessionCreated 表示默认会话和显式 Skill 快照已经原子建立。
	TypeSessionCreated Type = "session.created"
	// TypeSessionInputAccepted 表示非空用户输入已经可靠写入 PostgreSQL，尚未声明 Runner 已执行。
	TypeSessionInputAccepted Type = "session.input.accepted"
)

// SchemaVersionV1 是 W0 会话事件投影的首个稳定 Schema 版本。
const SchemaVersionV1 = "session.event.v1"

// SourceKindEnsureProjectSession 表示事件由 Business 的建会话命令产生。
const SourceKindEnsureProjectSession = "ensure_project_session"

// AggregateType 表示事件关联的权威聚合类型。
type AggregateType string

const (
	// AggregateTypeSession 表示事件关联 Session 聚合。
	AggregateTypeSession AggregateType = "session"
	// AggregateTypeSessionInput 表示事件关联 Session Input 聚合。
	AggregateTypeSessionInput AggregateType = "session_input"
)

// Record 表示待追加到会话 EventLog 的安全版本化事件。
// PayloadJSON 只能由本包的有类型构造函数生成，禁止调用方写入任意 JSON 或敏感正文。
type Record struct {
	// EventID 是应用生成的事件 UUIDv7。
	EventID string
	// SessionID 是事件所属 Session UUIDv7。
	SessionID string
	// Seq 是会话内单调序号；创建计划中由 Repository 事务统一分配。
	Seq int64
	// Type 是严格白名单事件类型。
	Type Type
	// SchemaVersion 是投影载荷版本。
	SchemaVersion string
	// SourceKind 是稳定事件来源类型。
	SourceKind string
	// SourceID 是来源命令 UUIDv7，用于 AppendOnce 去重。
	SourceID string
	// ProjectionIndex 是同一来源下投影事件的固定顺序索引。
	ProjectionIndex int
	// AggregateType 是事件关联聚合的稳定类型。
	AggregateType AggregateType
	// AggregateID 是事件关联聚合 UUIDv7。
	AggregateID string
	// AggregateVersion 是事件观察到的聚合版本。
	AggregateVersion int64
	// PayloadJSON 是经有类型 DTO 编码的安全 JSON，不包含 Prompt 或内部执行状态。
	PayloadJSON []byte
	// CreatedAt 是事件冻结时间，使用 UTC。
	CreatedAt time.Time
}

// SessionCreatedPayload 是 session.created 的前端安全投影载荷。
type SessionCreatedPayload struct {
	// SessionID 是已创建 Session UUIDv7。
	SessionID string `json:"session_id"`
	// ProjectID 是关联 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Status 是会话生命周期状态。
	Status string `json:"status"`
	// Version 是会话聚合版本。
	Version int64 `json:"version"`
}

// SessionInputAcceptedPayload 是 session.input.accepted 的前端安全投影载荷。
type SessionInputAcceptedPayload struct {
	// SessionID 是输入所属 Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是已持久化 Input UUIDv7。
	InputID string `json:"input_id"`
	// MessageID 是关联的用户 Message UUIDv7。
	MessageID string `json:"message_id"`
	// EnqueueSeq 是 Input 在 Session 内的 Head-of-Line 序号。
	EnqueueSeq int64 `json:"enqueue_seq"`
	// Status 是 Input 当前状态；W0 固定为 pending。
	Status string `json:"status"`
}

// NewSessionCreated 创建不含敏感正文的 session.created 事件。
// 调用方必须提供已冻结的 Session 事实；编码失败时不返回部分事件。
func NewSessionCreated(eventID, sessionID, projectID, status, sourceID string, version int64, createdAt time.Time) (Record, error) {
	payload, err := json.Marshal(SessionCreatedPayload{
		SessionID: sessionID,
		ProjectID: projectID,
		Status:    status,
		Version:   version,
	})
	if err != nil {
		return Record{}, fmt.Errorf("encode session.created payload: %w", err)
	}
	return Record{
		EventID:          eventID,
		SessionID:        sessionID,
		Type:             TypeSessionCreated,
		SchemaVersion:    SchemaVersionV1,
		SourceKind:       SourceKindEnsureProjectSession,
		SourceID:         sourceID,
		ProjectionIndex:  0,
		AggregateType:    AggregateTypeSession,
		AggregateID:      sessionID,
		AggregateVersion: version,
		PayloadJSON:      payload,
		CreatedAt:        createdAt.UTC(),
	}, nil
}

// NewSessionInputAccepted 创建不含消息正文的 session.input.accepted 事件。
// 该事件仅表示 PostgreSQL 已接受输入，不得被解释为 Runner、Turn 或模型已经执行。
func NewSessionInputAccepted(eventID, sessionID, inputID, messageID, sourceID, status string, enqueueSeq int64, createdAt time.Time) (Record, error) {
	payload, err := json.Marshal(SessionInputAcceptedPayload{
		SessionID:  sessionID,
		InputID:    inputID,
		MessageID:  messageID,
		EnqueueSeq: enqueueSeq,
		Status:     status,
	})
	if err != nil {
		return Record{}, fmt.Errorf("encode session.input.accepted payload: %w", err)
	}
	return Record{
		EventID:          eventID,
		SessionID:        sessionID,
		Type:             TypeSessionInputAccepted,
		SchemaVersion:    SchemaVersionV1,
		SourceKind:       SourceKindEnsureProjectSession,
		SourceID:         sourceID,
		ProjectionIndex:  1,
		AggregateType:    AggregateTypeSessionInput,
		AggregateID:      inputID,
		AggregateVersion: 1,
		PayloadJSON:      payload,
		CreatedAt:        createdAt.UTC(),
	}, nil
}
