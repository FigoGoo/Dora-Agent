package a2ui

import (
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
)

const (
	EventChatDelta         = "chat.delta"
	EventChatMessage       = "chat.message"
	EventReady             = "a2ui.ready"
	EventAction            = "a2ui.action"
	EventInterruptRequest  = "a2ui.interrupt_request"
	EventInterruptResolved = "a2ui.interrupt_resolved"
	EventToolProgress      = "tool.progress"
	EventError             = "a2ui.error"
)

// SSEEvent 是前后端事件流的统一外层格式，Payload 承载具体协议内容。
type SSEEvent struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	RunID        string    `json:"run_id,omitempty"`
	Seq          int64     `json:"seq"`
	Event        string    `json:"event"`
	SurfaceID    string    `json:"surface_id,omitempty"`
	DataModelKey string    `json:"data_model_key,omitempty"`
	Payload      any       `json:"payload,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// StoryboardPatchPayload 描述故事板版本化 JSON Patch，供前端同步左侧故事板。
type StoryboardPatchPayload struct {
	StoryboardID string              `json:"storyboard_id"`
	BaseVersion  int                 `json:"base_version"`
	NextVersion  int                 `json:"next_version"`
	Ops          []patch.JSONPatchOp `json:"ops"`
	Source       string              `json:"source"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
}

// InterruptRequestPayload 描述 Agent 暂停等待用户确认时的交互请求。
type InterruptRequestPayload struct {
	CheckpointID      string         `json:"checkpoint_id"`
	InterruptID       string         `json:"interrupt_id"`
	SpecVersion       int            `json:"spec_version,omitempty"`
	StoryboardVersion int            `json:"storyboard_version,omitempty"`
	Title             string         `json:"title,omitempty"`
	Message           string         `json:"message,omitempty"`
	Actions           []ActionSchema `json:"actions,omitempty"`
}

// ActionSchema 描述一次可恢复动作的输入 schema，用于确认/修改类 interrupt。
type ActionSchema struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}
