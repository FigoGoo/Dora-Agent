package session

import (
	"errors"
	"time"
)

var ErrCheckpointNotFound = errors.New("checkpoint mapping not found")

const (
	CheckpointScopeRunner     = "runner"
	CheckpointScopeMediaGraph = "media_graph"

	CheckpointStatusPending = "pending"
	CheckpointStatusResumed = "resumed"
)

type SessionRecord struct {
	ID        string    `json:"id" gorm:"primaryKey;size:128"`
	UserID    string    `json:"user_id,omitempty" gorm:"size:128;index"`
	SkillID   string    `json:"skill_id,omitempty" gorm:"size:128;index"`
	Title     string    `json:"title,omitempty" gorm:"size:512"`
	Status    string    `json:"status" gorm:"size:64;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SessionRecord) TableName() string {
	return "aigc_sessions"
}

type MessageRecord struct {
	ID              string         `json:"id" gorm:"primaryKey;size:128"`
	SessionID       string         `json:"session_id" gorm:"size:128;index:idx_aigc_messages_session_seq,priority:1"`
	RunID           string         `json:"run_id,omitempty"`
	Role            string         `json:"role" gorm:"size:32"`
	Content         string         `json:"content,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty" gorm:"type:jsonb;serializer:json"`
	MessageJSON     []byte         `json:"message_json,omitempty" gorm:"type:jsonb"`
	ContentBlocks   []byte         `json:"content_blocks,omitempty"`
	ToolCalls       []byte         `json:"tool_calls,omitempty" gorm:"type:jsonb"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	ToolName        string         `json:"tool_name,omitempty"`
	ParentMessageID string         `json:"parent_message_id,omitempty"`
	Seq             int64          `json:"seq" gorm:"index:idx_aigc_messages_session_seq,priority:2"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (MessageRecord) TableName() string {
	return "aigc_messages"
}

type MessageWindow struct {
	Limit int
}

type CheckpointMapping struct {
	ID                 string    `json:"id" gorm:"primaryKey;size:128"`
	SessionID          string    `json:"session_id" gorm:"size:128;index:idx_aigc_checkpoint_session_interrupt,priority:1"`
	RunID              string    `json:"run_id" gorm:"size:128;index"`
	Scope              string    `json:"scope" gorm:"size:64;index"`
	ToolCallID         string    `json:"tool_call_id,omitempty" gorm:"size:128;index"`
	ToolKey            string    `json:"tool_key,omitempty" gorm:"size:128"`
	GraphName          string    `json:"graph_name,omitempty" gorm:"size:128"`
	NodePath           string    `json:"node_path,omitempty" gorm:"size:256"`
	RunnerCheckpointID string    `json:"runner_checkpoint_id,omitempty" gorm:"size:256;index"`
	GraphCheckpointID  string    `json:"graph_checkpoint_id,omitempty" gorm:"size:256;index"`
	InterruptID        string    `json:"interrupt_id" gorm:"size:256;index:idx_aigc_checkpoint_session_interrupt,priority:2"`
	StageKey           string    `json:"stage_key,omitempty" gorm:"size:128"`
	SpecVersion        int       `json:"spec_version,omitempty"`
	StoryboardVersion  int       `json:"storyboard_version,omitempty"`
	Status             string    `json:"status" gorm:"size:64;index"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	ResumedAt          time.Time `json:"resumed_at,omitempty"`
}

func (CheckpointMapping) TableName() string {
	return "aigc_checkpoint_mappings"
}

type JobWakeupEvent struct {
	SessionID         string   `json:"session_id"`
	JobID             string   `json:"job_id"`
	ToolCallID        string   `json:"tool_call_id,omitempty"`
	StageKey          string   `json:"stage_key,omitempty"`
	Status            string   `json:"status"`
	AssetIDs          []string `json:"asset_ids,omitempty"`
	ErrorCode         string   `json:"error_code,omitempty"`
	StoryboardVersion int      `json:"storyboard_version,omitempty"`
}
