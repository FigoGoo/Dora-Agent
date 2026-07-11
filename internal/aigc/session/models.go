package session

import (
	"errors"
	"time"
)

var (
	ErrCheckpointNotFound         = errors.New("checkpoint mapping not found")
	ErrMessageIdempotencyConflict = errors.New("message idempotency conflict")
)

const (
	CheckpointScopeRunner = "runner"

	CheckpointStatusPending       = "pending"
	CheckpointStatusResumeQueued  = "resume_queued"
	CheckpointStatusResuming      = "resuming"
	CheckpointStatusResumeApplied = "resume_applied"
	CheckpointStatusResumed       = "resumed"
	CheckpointStatusCancelled     = "cancelled"
	CheckpointStatusExpired       = "expired"
	CheckpointStatusStale         = "stale"
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
	// Limit is a soft message budget. ApplyMessageWindow never splits a RunID
	// group, so the result may exceed Limit when the newest/current run alone is
	// larger than the budget.
	Limit int
	// ThroughSeq is an inclusive cutoff for user-owned run groups. A group whose
	// user message is later than the cutoff is removed together with its output;
	// visible predecessor assistant/tool messages remain even when their
	// physical log Seq is higher than the cutoff.
	ThroughSeq *int64
	// CurrentMessageID, when set for a user turn, is ordered after all visible
	// predecessor outputs even if the user row was appended before those
	// outputs. It is also retained by the logical-window Limit.
	CurrentMessageID string
}

type CheckpointMapping struct {
	ID                 string    `json:"id" gorm:"primaryKey;size:128"`
	ApprovalID         string    `json:"approval_id,omitempty" gorm:"size:128;index;uniqueIndex:idx_aigc_checkpoint_approval_interrupt,priority:1,where:approval_id <> ''"`
	SessionID          string    `json:"session_id" gorm:"size:128;uniqueIndex:idx_aigc_checkpoint_session_interrupt,priority:1"`
	RunID              string    `json:"run_id" gorm:"size:128;index"`
	Scope              string    `json:"scope" gorm:"size:64;index"`
	ToolCallID         string    `json:"tool_call_id,omitempty" gorm:"size:128;index"`
	ToolKey            string    `json:"tool_key,omitempty" gorm:"size:128"`
	GraphName          string    `json:"graph_name,omitempty" gorm:"size:128"`
	NodePath           string    `json:"node_path,omitempty" gorm:"size:256"`
	RunnerCheckpointID string    `json:"runner_checkpoint_id,omitempty" gorm:"size:256;index"`
	GraphCheckpointID  string    `json:"graph_checkpoint_id,omitempty" gorm:"size:256;index"`
	InterruptID        string    `json:"interrupt_id" gorm:"size:256;uniqueIndex:idx_aigc_checkpoint_session_interrupt,priority:2;uniqueIndex:idx_aigc_checkpoint_approval_interrupt,priority:2,where:approval_id <> ''"`
	MappingEpoch       int64     `json:"mapping_epoch" gorm:"not null;default:1"`
	DecisionVersion    int       `json:"decision_version,omitempty"`
	StageKey           string    `json:"stage_key,omitempty" gorm:"size:128"`
	ArtifactID         string    `json:"artifact_id,omitempty" gorm:"size:128;index"`
	ArtifactVersion    int       `json:"artifact_version,omitempty"`
	SpecVersion        int       `json:"spec_version,omitempty"`
	StoryboardVersion  int       `json:"storyboard_version,omitempty"`
	TargetRevision     int       `json:"target_revision,omitempty"`
	PromptRevision     int       `json:"prompt_revision,omitempty"`
	GenerationEpoch    int       `json:"generation_epoch,omitempty"`
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
