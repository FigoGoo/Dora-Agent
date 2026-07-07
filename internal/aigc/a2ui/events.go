package a2ui

import (
	"time"

	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

const (
	EventChatDelta          = "chat.delta"
	EventChatMessage        = "chat.message"
	EventBeginRendering     = "a2ui.begin_rendering"
	EventSurfaceUpdate      = "a2ui.surface_update"
	EventDataModelUpdate    = "a2ui.data_model_update"
	EventDeleteSurface      = "a2ui.delete_surface"
	EventInterruptRequest   = "a2ui.interrupt_request"
	EventStoryboardSnapshot = "storyboard.snapshot"
	EventStoryboardPatch    = "storyboard.patch"
	EventToolProgress       = "tool.progress"
	EventJobStatus          = "job.status"
	EventSkillSelected      = "skill.selected"
	EventError              = "error"
)

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

type StoryboardPatchPayload struct {
	StoryboardID string                  `json:"storyboard_id"`
	BaseVersion  int                     `json:"base_version"`
	NextVersion  int                     `json:"next_version"`
	Ops          []aigctools.JSONPatchOp `json:"ops"`
	Source       string                  `json:"source"`
	ToolCallID   string                  `json:"tool_call_id,omitempty"`
}

type InterruptRequestPayload struct {
	CheckpointID      string         `json:"checkpoint_id"`
	InterruptID       string         `json:"interrupt_id"`
	SpecVersion       int            `json:"spec_version,omitempty"`
	StoryboardVersion int            `json:"storyboard_version,omitempty"`
	Title             string         `json:"title,omitempty"`
	Message           string         `json:"message,omitempty"`
	Actions           []ActionSchema `json:"actions,omitempty"`
}

// SkillSelectedPayload 随 skill.selected 事件发给前端，表示 Router 在用户未手动选 Skill 时自动选中了一个。
type SkillSelectedPayload struct {
	SkillID   string `json:"skill_id"`
	SkillName string `json:"skill_name"`
	Reason    string `json:"reason"`
	Fallback  bool   `json:"fallback,omitempty"`
}

type ActionSchema struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}
