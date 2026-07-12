// Package approval owns the durable business truth for human review. An
// Approval is independent from an Eino checkpoint: ReviewMode records the
// immutable source while ExecutionMode may be fenced over to a deterministic
// durable fallback.
package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusStale     Status = "stale"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

type ReviewMode string

const (
	ReviewModeInterrupt ReviewMode = "interrupt"
	ReviewModeDurable   ReviewMode = "durable"
)

type ExecutionMode string

const (
	ExecutionModeInterrupt       ExecutionMode = "interrupt"
	ExecutionModeDurable         ExecutionMode = "durable"
	ExecutionModeDurableFallback ExecutionMode = "durable_fallback"
)

const (
	DecisionApprove = "approved"
	DecisionReject  = "rejected"

	EventSessionInputRequested         = "session.input_requested"
	EventApprovalContinuationRequested = "approval.continuation_requested"
	EventApprovalFallbackEnabled       = "approval.fallback_enabled"
	DestinationSessionInputs           = "session.inputs"
	DestinationApprovalContinuations   = "approval.continuations"

	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
	OutboxStatusDead      = "dead"
)

var (
	ErrNotFound             = errors.New("approval not found")
	ErrAlreadyDecided       = errors.New("approval already decided")
	ErrVersionConflict      = errors.New("approval decision version conflict")
	ErrIdempotencyConflict  = errors.New("approval idempotency conflict")
	ErrInvalidTransition    = errors.New("invalid approval transition")
	ErrReviewModeImmutable  = errors.New("approval review mode is immutable")
	ErrFallbackFenced       = errors.New("approval fallback fence rejected")
	ErrContinuationBusy     = errors.New("approval continuation has an active claim")
	ErrPrimaryReviewPending = errors.New("a primary creation review is already pending for the session")
)

const pendingPrimaryReviewIndex = "idx_aigc_approvals_one_pending_primary_review_per_session"

// isPrimaryReviewApproval identifies the two system-owned review gates that
// must be serialized for a session. Candidate assets are intentionally not in
// this set: they are reviewed as one storyboard-level batch and may have many
// durable Approval rows underneath that single surface.
func isPrimaryReviewApproval(value Approval) bool {
	if value.Status != StatusPending {
		return false
	}
	switch strings.TrimSpace(value.ArtifactType) {
	case "creation_spec_revision", "storyboard_revision":
		return true
	default:
		return false
	}
}

// VersionBinding freezes the semantic artifact versions reviewed by the user.
// For a target-scoped approval, the target/prompt/epoch tuple is authoritative
// and unrelated aggregate-version changes do not invalidate the decision.
type VersionBinding struct {
	ArtifactID        string `json:"artifact_id"`
	ArtifactVersion   int    `json:"artifact_version"`
	StoryboardID      string `json:"storyboard_id,omitempty"`
	StoryboardVersion int    `json:"storyboard_version,omitempty"`
	TargetID          string `json:"target_id,omitempty"`
	TargetRevision    int    `json:"target_revision,omitempty"`
	PromptRevision    int    `json:"prompt_revision,omitempty"`
	GenerationEpoch   int    `json:"generation_epoch,omitempty"`
}

func (binding VersionBinding) Matches(current VersionBinding) bool {
	if binding.ArtifactID != current.ArtifactID || binding.ArtifactVersion != current.ArtifactVersion {
		return false
	}
	if binding.StoryboardID != current.StoryboardID {
		return false
	}
	if binding.TargetID != "" {
		return binding.TargetID == current.TargetID &&
			binding.TargetRevision == current.TargetRevision &&
			binding.PromptRevision == current.PromptRevision &&
			binding.GenerationEpoch == current.GenerationEpoch
	}
	return binding.StoryboardVersion == current.StoryboardVersion
}

type FrozenCommand struct {
	Kind           string          `json:"kind"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}

type Approval struct {
	ID                  string         `json:"id"`
	IdempotencyKey      string         `json:"idempotency_key"`
	TenantID            string         `json:"tenant_id,omitempty"`
	UserID              string         `json:"user_id,omitempty"`
	SessionID           string         `json:"session_id"`
	ArtifactType        string         `json:"artifact_type"`
	Binding             VersionBinding `json:"binding"`
	ReviewMode          ReviewMode     `json:"review_mode"`
	ExecutionMode       ExecutionMode  `json:"execution_mode"`
	ExecutionEpoch      int64          `json:"execution_epoch"`
	Status              Status         `json:"status"`
	DecisionVersion     int            `json:"decision_version"`
	ApproveCommand      FrozenCommand  `json:"approve_command"`
	RejectCommand       FrozenCommand  `json:"reject_command"`
	CheckpointMappingID string         `json:"checkpoint_mapping_id,omitempty"`
	MappingEpoch        int64          `json:"mapping_epoch,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	ExpiresAt           *time.Time     `json:"expires_at,omitempty"`
	DecidedAt           *time.Time     `json:"decided_at,omitempty"`
}

type ApprovalDecision struct {
	ApprovalID            string          `json:"approval_id"`
	DecisionVersion       int             `json:"decision_version"`
	IdempotencyKey        string          `json:"idempotency_key"`
	RequestedDecision     string          `json:"requested_decision"`
	EffectiveStatus       Status          `json:"effective_status"`
	ActorID               string          `json:"actor_id,omitempty"`
	Reason                string          `json:"reason,omitempty"`
	ObservedBinding       *VersionBinding `json:"observed_binding,omitempty"`
	CommandKind           string          `json:"command_kind,omitempty"`
	CommandIdempotencyKey string          `json:"command_idempotency_key,omitempty"`
	CommandPayload        json.RawMessage `json:"command_payload,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
}

type DecideCommand struct {
	ApprovalID              string         `json:"approval_id"`
	ExpectedDecisionVersion int            `json:"expected_decision_version"`
	IdempotencyKey          string         `json:"idempotency_key"`
	Decision                string         `json:"decision"`
	ActorID                 string         `json:"actor_id,omitempty"`
	Reason                  string         `json:"reason,omitempty"`
	CurrentBinding          VersionBinding `json:"current_binding"`
	Now                     time.Time      `json:"-"`
}

type CloseCommand struct {
	ApprovalID              string    `json:"approval_id"`
	ExpectedDecisionVersion int       `json:"expected_decision_version"`
	IdempotencyKey          string    `json:"idempotency_key"`
	Status                  Status    `json:"status"`
	ActorID                 string    `json:"actor_id,omitempty"`
	Reason                  string    `json:"reason,omitempty"`
	Now                     time.Time `json:"-"`
}

type MappingCommand struct {
	ApprovalID             string `json:"approval_id"`
	ExpectedExecutionEpoch int64  `json:"expected_execution_epoch"`
	CheckpointMappingID    string `json:"checkpoint_mapping_id"`
	MappingEpoch           int64  `json:"mapping_epoch"`
}

type FallbackCommand struct {
	ApprovalID              string        `json:"approval_id"`
	ExpectedExecutionMode   ExecutionMode `json:"expected_execution_mode"`
	ExpectedExecutionEpoch  int64         `json:"expected_execution_epoch"`
	ExpectedDecisionVersion int           `json:"expected_decision_version"`
	Now                     time.Time     `json:"-"`
}

type DecisionResult struct {
	Approval     Approval                            `json:"approval"`
	Decision     ApprovalDecision                    `json:"decision"`
	Continuation sessionruntime.ApprovalContinuation `json:"continuation"`
	Outbox       OutboxEvent                         `json:"outbox"`
	Created      bool                                `json:"created"`
}

type FallbackResult struct {
	Approval     Approval                             `json:"approval"`
	Continuation *sessionruntime.ApprovalContinuation `json:"continuation,omitempty"`
	Outbox       OutboxEvent                          `json:"outbox"`
	Switched     bool                                 `json:"switched"`
}

type ApprovalContinuationRequested struct {
	ApprovalID      string                              `json:"approval_id"`
	DecisionVersion int                                 `json:"decision_version"`
	Executor        sessionruntime.ContinuationExecutor `json:"executor"`
	ExecutionEpoch  int64                               `json:"execution_epoch"`
}

type OutboxEvent struct {
	ID               string          `json:"id" gorm:"primaryKey;size:256"`
	IdempotencyKey   string          `json:"idempotency_key" gorm:"size:256;uniqueIndex"`
	EventType        string          `json:"event_type" gorm:"size:128;index"`
	Destination      string          `json:"destination" gorm:"size:128;index"`
	AggregateType    string          `json:"aggregate_type" gorm:"size:64"`
	AggregateID      string          `json:"aggregate_id" gorm:"size:128;index"`
	AggregateVersion int             `json:"aggregate_version"`
	SessionID        string          `json:"session_id,omitempty" gorm:"size:128;index"`
	WorkflowRunID    string          `json:"workflow_run_id,omitempty" gorm:"size:128"`
	StageRunID       string          `json:"stage_run_id,omitempty" gorm:"size:128"`
	OperationID      string          `json:"operation_id,omitempty" gorm:"size:128"`
	ToolCallID       string          `json:"tool_call_id,omitempty" gorm:"size:128"`
	BatchID          string          `json:"batch_id,omitempty" gorm:"size:128"`
	JobID            string          `json:"job_id,omitempty" gorm:"size:128"`
	Payload          json.RawMessage `json:"payload" gorm:"column:payload_json;type:jsonb"`
	Status           string          `json:"status" gorm:"size:32;index"`
	Attempts         int             `json:"attempts"`
	AvailableAt      time.Time       `json:"available_at" gorm:"index"`
	CreatedAt        time.Time       `json:"created_at"`
	PublishedAt      *time.Time      `json:"published_at,omitempty"`
}

func (OutboxEvent) TableName() string { return "aigc_outbox_events" }

func normalizeApproval(value Approval, now time.Time) (Approval, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.IdempotencyKey = strings.TrimSpace(value.IdempotencyKey)
	value.TenantID = strings.TrimSpace(value.TenantID)
	value.UserID = strings.TrimSpace(value.UserID)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.ArtifactType = strings.TrimSpace(value.ArtifactType)
	value.Binding = normalizeBinding(value.Binding)
	if value.ID == "" || value.IdempotencyKey == "" || value.SessionID == "" || value.ArtifactType == "" {
		return Approval{}, fmt.Errorf("approval id, idempotency key, session id and artifact type are required")
	}
	if value.Binding.ArtifactID == "" || value.Binding.ArtifactVersion <= 0 {
		return Approval{}, fmt.Errorf("approval artifact id and positive version are required")
	}
	if err := validateReviewExecutionMode(value.ReviewMode, value.ExecutionMode); err != nil {
		return Approval{}, err
	}
	approve, err := normalizeFrozenCommand(value.ApproveCommand)
	if err != nil {
		return Approval{}, fmt.Errorf("approve command: %w", err)
	}
	reject, err := normalizeFrozenCommand(value.RejectCommand)
	if err != nil {
		return Approval{}, fmt.Errorf("reject command: %w", err)
	}
	if approve.Kind == reject.Kind || approve.IdempotencyKey == reject.IdempotencyKey {
		return Approval{}, fmt.Errorf("approve and reject commands require distinct kinds and idempotency keys")
	}
	value.ApproveCommand, value.RejectCommand = approve, reject
	value.Status = StatusPending
	value.DecisionVersion = 0
	value.ExecutionEpoch = 1
	value.CheckpointMappingID = ""
	value.MappingEpoch = 0
	value.DecidedAt = nil
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	value.CreatedAt, value.UpdatedAt = now, now
	if value.ExpiresAt != nil {
		expires := value.ExpiresAt.UTC()
		if !expires.After(now) {
			return Approval{}, fmt.Errorf("approval expiry must be in the future")
		}
		value.ExpiresAt = &expires
	}
	return value, nil
}

func normalizeBinding(value VersionBinding) VersionBinding {
	value.ArtifactID = strings.TrimSpace(value.ArtifactID)
	value.StoryboardID = strings.TrimSpace(value.StoryboardID)
	value.TargetID = strings.TrimSpace(value.TargetID)
	return value
}

func normalizeFrozenCommand(command FrozenCommand) (FrozenCommand, error) {
	command.Kind = strings.TrimSpace(command.Kind)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.Kind == "" || command.IdempotencyKey == "" {
		return FrozenCommand{}, fmt.Errorf("kind and idempotency key are required")
	}
	if len(command.Payload) == 0 {
		command.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(command.Payload) {
		return FrozenCommand{}, fmt.Errorf("payload must be valid JSON")
	}
	command.Payload = append(json.RawMessage(nil), command.Payload...)
	return command, nil
}

func validateReviewExecutionMode(review ReviewMode, execution ExecutionMode) error {
	switch review {
	case ReviewModeInterrupt:
		if execution != ExecutionModeInterrupt {
			return fmt.Errorf("interrupt review must start in interrupt execution mode")
		}
	case ReviewModeDurable:
		if execution != ExecutionModeDurable {
			return fmt.Errorf("durable review must start in durable execution mode")
		}
	default:
		return fmt.Errorf("unsupported review mode %q", review)
	}
	return nil
}

func sameApproval(a, b Approval) bool {
	return a.ID == b.ID && a.IdempotencyKey == b.IdempotencyKey && a.TenantID == b.TenantID &&
		a.UserID == b.UserID && a.SessionID == b.SessionID && a.ArtifactType == b.ArtifactType &&
		a.Binding == b.Binding && a.ReviewMode == b.ReviewMode && a.ExecutionMode == b.ExecutionMode &&
		sameFrozenCommand(a.ApproveCommand, b.ApproveCommand) && sameFrozenCommand(a.RejectCommand, b.RejectCommand) &&
		sameOptionalTime(a.ExpiresAt, b.ExpiresAt)
}

func sameFrozenCommand(a, b FrozenCommand) bool {
	return a.Kind == b.Kind && a.IdempotencyKey == b.IdempotencyKey && bytes.Equal(a.Payload, b.Payload)
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
