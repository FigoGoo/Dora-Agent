// Package sessionruntime implements the durable input lane and fencing
// contracts that sit in front of an Eino TurnLoop/Runner.
package sessionruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInputNotFound        = errors.New("session input not found")
	ErrNoInputAvailable     = errors.New("no session input available")
	ErrLeaseHeld            = errors.New("session runtime lease is held by another owner")
	ErrFenceRejected        = errors.New("session runtime fence rejected")
	ErrInvalidTransition    = errors.New("invalid session runtime transition")
	ErrTurnNotFound         = errors.New("session turn run not found")
	ErrContinuationNotFound = errors.New("approval continuation not found")
	ErrContinuationClaimed  = errors.New("approval continuation is already claimed")
	ErrIdempotencyConflict  = errors.New("session runtime idempotency conflict")
	ErrManagerStopped       = errors.New("session runtime manager is stopped")
)

type InputType string

const (
	InputTypeUserMessage             InputType = "user_message"
	InputTypeResumeRequested         InputType = "resume_requested"
	InputTypeApprovalContinuation    InputType = "approval_continuation_result"
	InputTypeBatchContinuationResult InputType = "batch_continuation_result"
)

const (
	PriorityUserMessage             = 300
	PriorityResumeRequested         = 200
	PriorityApprovalContinuation    = 200
	PriorityBatchContinuationResult = 100
)

type InputStatus string

const (
	InputStatusPending   InputStatus = "pending"
	InputStatusClaimed   InputStatus = "claimed"
	InputStatusRunning   InputStatus = "running"
	InputStatusRetryWait InputStatus = "retry_wait"
	InputStatusResolved  InputStatus = "resolved"
	InputStatusDead      InputStatus = "dead"
)

type TurnStatus string

const (
	TurnStatusPrepared         TurnStatus = "prepared"
	TurnStatusRunning          TurnStatus = "running"
	TurnStatusWaitingInterrupt TurnStatus = "waiting_interrupt"
	TurnStatusCommitting       TurnStatus = "committing"
	TurnStatusCommitted        TurnStatus = "committed"
	TurnStatusRetryWait        TurnStatus = "retry_wait"
	TurnStatusDead             TurnStatus = "dead"
)

func isTerminalInputStatus(status InputStatus) bool {
	return status == InputStatusResolved || status == InputStatusDead
}

func isTerminalTurnStatus(status TurnStatus) bool {
	return status == TurnStatusWaitingInterrupt || status == TurnStatusCommitted || status == TurnStatusDead
}

type ContinuationExecutor string

const (
	ContinuationExecutorRunnerResume  ContinuationExecutor = "runner_resume"
	ContinuationExecutorDeterministic ContinuationExecutor = "deterministic_continuation"
)

type ContinuationStatus string

const (
	ContinuationStatusRequested ContinuationStatus = "requested"
	ContinuationStatusClaimed   ContinuationStatus = "claimed"
	ContinuationStatusApplied   ContinuationStatus = "applied"
	ContinuationStatusFailed    ContinuationStatus = "failed"
)

// SessionInput is deliberately closed to the durable TurnLoop inputs.
// Worker JobStatus and BatchTerminalSignal must not implement this interface.
type SessionInput interface {
	sessionInput()
	InputIdentity() InputIdentity
}

type InputIdentity struct {
	InputID  string
	EventID  string
	Type     InputType
	SourceID string
	Priority int
}

type UserMessage struct {
	InputID           string `json:"input_id"`
	EventID           string `json:"event_id"`
	MessageID         string `json:"message_id"`
	ContextMessageSeq int64  `json:"context_message_seq,omitempty"`
}

func (UserMessage) sessionInput() {}
func (input UserMessage) InputIdentity() InputIdentity {
	return InputIdentity{input.InputID, input.EventID, InputTypeUserMessage, input.MessageID, PriorityUserMessage}
}

type ResumeRequested struct {
	InputID         string          `json:"input_id"`
	EventID         string          `json:"event_id"`
	ApprovalID      string          `json:"approval_id"`
	DecisionVersion int             `json:"decision_version"`
	MappingID       string          `json:"mapping_id"`
	MappingEpoch    int64           `json:"mapping_epoch"`
	CheckpointID    string          `json:"checkpoint_id,omitempty"`
	InterruptID     string          `json:"interrupt_id,omitempty"`
	Content         string          `json:"content,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
}

func (ResumeRequested) sessionInput() {}
func (input ResumeRequested) InputIdentity() InputIdentity {
	if strings.TrimSpace(input.ApprovalID) == "" {
		mappingID := strings.TrimSpace(input.MappingID)
		inputID := fmt.Sprintf("checkpoint:%s:resume:%d", mappingID, input.MappingEpoch)
		source := fmt.Sprintf("%s:%d", mappingID, input.MappingEpoch)
		return InputIdentity{inputID, input.EventID, InputTypeResumeRequested, source, PriorityResumeRequested}
	}
	source := fmt.Sprintf("%s:%d", input.ApprovalID, input.DecisionVersion)
	return InputIdentity{input.InputID, input.EventID, InputTypeResumeRequested, source, PriorityResumeRequested}
}

// ApprovalContinuationResult is the trusted, durable wake-up emitted only
// after a deterministic approval command has been applied. It starts a fresh
// Agent turn; unlike ResumeRequested it never impersonates a user response or
// attempts to restore an interrupted Runner checkpoint.
type ApprovalContinuationResult struct {
	InputID           string          `json:"input_id"`
	EventID           string          `json:"event_id"`
	ApprovalID        string          `json:"approval_id"`
	DecisionVersion   int             `json:"decision_version"`
	ExecutionEpoch    int64           `json:"execution_epoch"`
	RequestedDecision string          `json:"requested_decision"`
	EffectiveStatus   string          `json:"effective_status"`
	ArtifactType      string          `json:"artifact_type,omitempty"`
	ArtifactID        string          `json:"artifact_id,omitempty"`
	ArtifactVersion   int             `json:"artifact_version,omitempty"`
	StoryboardID      string          `json:"storyboard_id,omitempty"`
	StoryboardVersion int             `json:"storyboard_version,omitempty"`
	CommandKind       string          `json:"command_kind,omitempty"`
	CommandResult     json.RawMessage `json:"command_result,omitempty"`
	// ContextMessageSeq freezes the inclusive chat-history boundary used by
	// the internal continuation turn.
	ContextMessageSeq int64 `json:"context_message_seq,omitempty"`
}

func (ApprovalContinuationResult) sessionInput() {}
func (input ApprovalContinuationResult) InputIdentity() InputIdentity {
	source := fmt.Sprintf("%s:%d", input.ApprovalID, input.DecisionVersion)
	return InputIdentity{input.InputID, input.EventID, InputTypeApprovalContinuation, source, PriorityApprovalContinuation}
}

type BatchContinuationResult struct {
	InputID               string `json:"input_id"`
	EventID               string `json:"event_id"`
	ResultVersion         int    `json:"result_version"`
	OperationID           string `json:"operation_id"`
	BatchID               string `json:"batch_id"`
	StageStatus           string `json:"stage_status"`
	ApprovalID            string `json:"approval_id,omitempty"`
	NeedsAgentExplanation bool   `json:"needs_agent_explanation"`
	// ContextMessageSeq freezes the inclusive upper bound of chat history used
	// if the durable continuation asks the Agent to explain the result.
	ContextMessageSeq int64 `json:"context_message_seq,omitempty"`
	// Result is the immutable PostBatchPayload emitted by the barrier. It is
	// trusted persisted context, not provider output reconstructed by the Agent.
	Result json.RawMessage `json:"result,omitempty"`
}

func (BatchContinuationResult) sessionInput() {}
func (input BatchContinuationResult) InputIdentity() InputIdentity {
	source := fmt.Sprintf("%s:%d", input.BatchID, input.ResultVersion)
	return InputIdentity{input.InputID, input.EventID, InputTypeBatchContinuationResult, source, PriorityBatchContinuationResult}
}

func NewUserMessage(messageID, eventID string) UserMessage {
	messageID = strings.TrimSpace(messageID)
	inputID := "message:" + messageID
	if strings.TrimSpace(eventID) == "" {
		eventID = inputID
	}
	return UserMessage{InputID: inputID, EventID: eventID, MessageID: messageID}
}

func NewResumeRequested(approvalID string, decisionVersion int, eventID string) ResumeRequested {
	approvalID = strings.TrimSpace(approvalID)
	inputID := fmt.Sprintf("approval:%s:resume:%d", approvalID, decisionVersion)
	if strings.TrimSpace(eventID) == "" {
		eventID = inputID
	}
	return ResumeRequested{InputID: inputID, EventID: eventID, ApprovalID: approvalID, DecisionVersion: decisionVersion}
}

func NewApprovalContinuationResult(approvalID string, decisionVersion int, executionEpoch int64, eventID string) ApprovalContinuationResult {
	approvalID = strings.TrimSpace(approvalID)
	inputID := fmt.Sprintf("approval:%s:continuation-result:%d", approvalID, decisionVersion)
	if strings.TrimSpace(eventID) == "" {
		eventID = inputID
	}
	return ApprovalContinuationResult{
		InputID: inputID, EventID: eventID, ApprovalID: approvalID,
		DecisionVersion: decisionVersion, ExecutionEpoch: executionEpoch,
	}
}

func NewInterruptResumeRequested(mappingID string, epoch int64, checkpointID, interruptID, content string, data json.RawMessage, eventID string) ResumeRequested {
	mappingID = strings.TrimSpace(mappingID)
	checkpointID = strings.TrimSpace(checkpointID)
	interruptID = strings.TrimSpace(interruptID)
	inputID := fmt.Sprintf("checkpoint:%s:resume:%d", mappingID, epoch)
	if strings.TrimSpace(eventID) == "" {
		eventID = inputID
	}
	return ResumeRequested{
		InputID: inputID, EventID: eventID,
		MappingID: mappingID, MappingEpoch: epoch,
		CheckpointID: checkpointID, InterruptID: interruptID,
		Content: content, Data: append(json.RawMessage(nil), data...),
	}
}

func NewBatchContinuationResult(batchID string, resultVersion int, eventID string) BatchContinuationResult {
	batchID = strings.TrimSpace(batchID)
	inputID := fmt.Sprintf("batch:%s:continuation:%d", batchID, resultVersion)
	if strings.TrimSpace(eventID) == "" {
		eventID = inputID
	}
	return BatchContinuationResult{InputID: inputID, EventID: eventID, BatchID: batchID, ResultVersion: resultVersion}
}

type SessionInputRecord struct {
	InputID    string          `json:"input_id" gorm:"primaryKey;size:256"`
	SessionID  string          `json:"session_id" gorm:"size:128;index"`
	InputType  InputType       `json:"input_type" gorm:"size:64;index"`
	SourceID   string          `json:"source_id" gorm:"size:256"`
	EventID    string          `json:"event_id" gorm:"size:128"`
	Payload    json.RawMessage `json:"payload" gorm:"type:jsonb;column:payload_json"`
	Priority   int             `json:"priority" gorm:"index"`
	EnqueueSeq int64           `json:"enqueue_seq"`
	// ContextMessageSeq is copied from the immutable typed input at enqueue.
	// It is intentionally independent of EnqueueSeq: the former addresses the
	// chat message log while the latter orders the durable input lane.
	ContextMessageSeq int64       `json:"context_message_seq" gorm:"column:context_message_seq"`
	Status            InputStatus `json:"status" gorm:"size:32;index"`
	TurnID            string      `json:"turn_id,omitempty" gorm:"size:128;index"`
	ClaimOwner        string      `json:"claim_owner,omitempty" gorm:"size:128"`
	ClaimFence        int64       `json:"claim_fence,omitempty"`
	LeaseUntil        *time.Time  `json:"lease_until,omitempty" gorm:"index"`
	Attempts          int         `json:"attempts"`
	AvailableAt       time.Time   `json:"available_at" gorm:"index"`
	ErrorCode         string      `json:"error_code,omitempty" gorm:"size:128"`
	ErrorMessage      string      `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
	ResolvedAt        *time.Time  `json:"resolved_at,omitempty"`
}

func (SessionInputRecord) TableName() string { return "aigc_session_inputs" }

type sessionInputCounter struct {
	SessionID string    `gorm:"primaryKey;size:128"`
	NextSeq   int64     `gorm:"column:next_seq"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (sessionInputCounter) TableName() string { return "aigc_session_input_counters" }

type SessionRuntimeLease struct {
	SessionID  string    `json:"session_id" gorm:"primaryKey;size:128"`
	OwnerID    string    `json:"owner_id" gorm:"size:128;index"`
	FenceToken int64     `json:"fence_token"`
	LeaseUntil time.Time `json:"lease_until" gorm:"index"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (SessionRuntimeLease) TableName() string { return "aigc_session_runtime_leases" }

type Fence struct {
	SessionID  string `json:"session_id"`
	OwnerID    string `json:"owner_id"`
	FenceToken int64  `json:"fence_token"`
}

func (lease SessionRuntimeLease) Fence() Fence {
	return Fence{SessionID: lease.SessionID, OwnerID: lease.OwnerID, FenceToken: lease.FenceToken}
}

type SessionTurnRun struct {
	TurnID             string          `json:"turn_id" gorm:"primaryKey;size:128"`
	InputID            string          `json:"input_id" gorm:"size:256;uniqueIndex"`
	SessionID          string          `json:"session_id" gorm:"size:128;index"`
	RunnerRunID        string          `json:"runner_run_id" gorm:"size:128;index"`
	ParentTurnID       string          `json:"parent_turn_id,omitempty" gorm:"size:128;index"`
	ClaimFence         int64           `json:"claim_fence"`
	Kind               InputType       `json:"kind" gorm:"size:64"`
	Status             TurnStatus      `json:"status" gorm:"size:32;index"`
	RunnerCheckpointID string          `json:"runner_checkpoint_id,omitempty" gorm:"size:256;index"`
	Attempt            int             `json:"attempt"`
	ContextMessageSeq  int64           `json:"context_message_seq" gorm:"column:context_message_seq"`
	ContextSeqFrozen   bool            `json:"context_seq_frozen" gorm:"column:context_seq_frozen"`
	OutputPayload      json.RawMessage `json:"output_payload,omitempty" gorm:"type:jsonb;column:output_payload_json"`
	OutputDigest       string          `json:"output_digest,omitempty" gorm:"size:128"`
	ErrorCode          string          `json:"error_code,omitempty" gorm:"size:128"`
	ErrorMessage       string          `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	CommittedAt        *time.Time      `json:"committed_at,omitempty"`
}

func (SessionTurnRun) TableName() string { return "aigc_session_turn_runs" }

type TurnSpec struct {
	TurnID             string
	RunnerRunID        string
	ParentTurnID       string
	RunnerCheckpointID string
}

type ApprovalContinuation struct {
	ApprovalID      string               `json:"approval_id" gorm:"primaryKey;size:128"`
	DecisionVersion int                  `json:"decision_version" gorm:"primaryKey;autoIncrement:false"`
	SessionID       string               `json:"session_id" gorm:"size:128;index"`
	Executor        ContinuationExecutor `json:"executor" gorm:"size:64;index"`
	ExecutionEpoch  int64                `json:"execution_epoch"`
	Status          ContinuationStatus   `json:"status" gorm:"size:32;index"`
	LeaseOwner      string               `json:"lease_owner,omitempty" gorm:"size:128"`
	LeaseUntil      *time.Time           `json:"lease_until,omitempty" gorm:"index"`
	ErrorCode       string               `json:"error_code,omitempty" gorm:"size:128"`
	ErrorMessage    string               `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	AppliedAt       *time.Time           `json:"applied_at,omitempty"`
}

func (ApprovalContinuation) TableName() string { return "aigc_approval_continuations" }

type ContinuationClaim struct {
	ApprovalID      string
	DecisionVersion int
	Executor        ContinuationExecutor
	ExecutionEpoch  int64
	LeaseOwner      string
}

type ApprovalCommandLedger struct {
	ApprovalID      string          `json:"approval_id" gorm:"primaryKey;size:128"`
	DecisionVersion int             `json:"decision_version" gorm:"primaryKey;autoIncrement:false"`
	CommandKind     string          `json:"command_kind" gorm:"primaryKey;size:128"`
	ExecutionEpoch  int64           `json:"execution_epoch"`
	IdempotencyKey  string          `json:"idempotency_key" gorm:"size:256;uniqueIndex"`
	CommandPayload  json.RawMessage `json:"command_payload" gorm:"type:jsonb;column:command_payload_json"`
	ResultPayload   json.RawMessage `json:"result_payload" gorm:"type:jsonb;column:result_payload_json"`
	CreatedAt       time.Time       `json:"created_at"`
}

func (ApprovalCommandLedger) TableName() string { return "aigc_approval_command_ledger" }

func StableTurnID(sessionID, inputID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(inputID)))
	return "turn_" + hex.EncodeToString(sum[:16])
}

func StableRunnerRunID(sessionID, inputID string) string {
	sum := sha256.Sum256([]byte("runner\x00" + strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(inputID)))
	return "run_" + hex.EncodeToString(sum[:16])
}

// WithContextMessageSeq binds an input that rebuilds chat history to an
// inclusive message-log boundary. Inputs that resume a Runner checkpoint do
// not rebuild history and are returned unchanged.
func WithContextMessageSeq(input SessionInput, seq int64) (SessionInput, error) {
	if seq < 0 {
		return nil, fmt.Errorf("context message sequence cannot be negative")
	}
	switch value := input.(type) {
	case UserMessage:
		value.ContextMessageSeq = seq
		return value, nil
	case *UserMessage:
		if value == nil {
			return nil, fmt.Errorf("user message input is required")
		}
		cloned := *value
		cloned.ContextMessageSeq = seq
		return cloned, nil
	case BatchContinuationResult:
		value.ContextMessageSeq = seq
		return value, nil
	case *BatchContinuationResult:
		if value == nil {
			return nil, fmt.Errorf("batch continuation input is required")
		}
		cloned := *value
		cloned.ContextMessageSeq = seq
		cloned.Result = append(json.RawMessage(nil), value.Result...)
		return cloned, nil
	case ApprovalContinuationResult:
		value.ContextMessageSeq = seq
		return value, nil
	case *ApprovalContinuationResult:
		if value == nil {
			return nil, fmt.Errorf("approval continuation input is required")
		}
		cloned := *value
		cloned.ContextMessageSeq = seq
		cloned.CommandResult = append(json.RawMessage(nil), value.CommandResult...)
		return cloned, nil
	default:
		return input, nil
	}
}

func inputContextMessageSeq(input SessionInput) int64 {
	switch value := input.(type) {
	case UserMessage:
		return value.ContextMessageSeq
	case *UserMessage:
		if value != nil {
			return value.ContextMessageSeq
		}
	case BatchContinuationResult:
		return value.ContextMessageSeq
	case *BatchContinuationResult:
		if value != nil {
			return value.ContextMessageSeq
		}
	case ApprovalContinuationResult:
		return value.ContextMessageSeq
	case *ApprovalContinuationResult:
		if value != nil {
			return value.ContextMessageSeq
		}
	}
	return 0
}

func encodeInput(input SessionInput) (InputIdentity, json.RawMessage, error) {
	if input == nil {
		return InputIdentity{}, nil, fmt.Errorf("session input is required")
	}
	switch value := input.(type) {
	case *UserMessage:
		if value == nil {
			return InputIdentity{}, nil, fmt.Errorf("user message input is required")
		}
	case *ResumeRequested:
		if value == nil {
			return InputIdentity{}, nil, fmt.Errorf("resume requested input is required")
		}
	case *BatchContinuationResult:
		if value == nil {
			return InputIdentity{}, nil, fmt.Errorf("batch continuation input is required")
		}
	case *ApprovalContinuationResult:
		if value == nil {
			return InputIdentity{}, nil, fmt.Errorf("approval continuation input is required")
		}
	}
	identity := input.InputIdentity()
	identity.InputID = strings.TrimSpace(identity.InputID)
	identity.EventID = strings.TrimSpace(identity.EventID)
	identity.SourceID = strings.TrimSpace(identity.SourceID)
	if identity.InputID == "" || identity.EventID == "" || identity.SourceID == "" {
		return InputIdentity{}, nil, fmt.Errorf("input id, event id, and source id are required")
	}
	if identity.Priority <= 0 {
		return InputIdentity{}, nil, fmt.Errorf("input priority must be positive")
	}
	if err := validateTypedInput(input); err != nil {
		return InputIdentity{}, nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return InputIdentity{}, nil, fmt.Errorf("marshal session input: %w", err)
	}
	return identity, raw, nil
}

func validateTypedInput(input SessionInput) error {
	switch value := input.(type) {
	case *UserMessage:
		if value == nil {
			return fmt.Errorf("user message input is required")
		}
		return validateTypedInput(*value)
	case *ResumeRequested:
		if value == nil {
			return fmt.Errorf("resume requested input is required")
		}
		return validateTypedInput(*value)
	case *BatchContinuationResult:
		if value == nil {
			return fmt.Errorf("batch continuation input is required")
		}
		return validateTypedInput(*value)
	case *ApprovalContinuationResult:
		if value == nil {
			return fmt.Errorf("approval continuation input is required")
		}
		return validateTypedInput(*value)
	case UserMessage:
		if strings.TrimSpace(value.MessageID) == "" {
			return fmt.Errorf("message id is required")
		}
		if value.ContextMessageSeq < 0 {
			return fmt.Errorf("context message sequence cannot be negative")
		}
	case ResumeRequested:
		if strings.TrimSpace(value.MappingID) == "" || value.MappingEpoch <= 0 {
			return fmt.Errorf("mapping id and positive mapping epoch are required")
		}
		if strings.TrimSpace(value.ApprovalID) != "" {
			if value.DecisionVersion <= 0 {
				return fmt.Errorf("approval id and positive decision version are required")
			}
			if strings.TrimSpace(value.CheckpointID) != "" || strings.TrimSpace(value.InterruptID) != "" || value.Content != "" || len(value.Data) != 0 {
				return fmt.Errorf("approval resume cannot include generic interrupt fields")
			}
			break
		}
		if value.DecisionVersion != 0 {
			return fmt.Errorf("generic interrupt resume cannot include approval fields")
		}
		if strings.TrimSpace(value.CheckpointID) == "" || strings.TrimSpace(value.InterruptID) == "" {
			return fmt.Errorf("checkpoint id and interrupt id are required")
		}
		if !json.Valid(value.Data) {
			return fmt.Errorf("generic interrupt resume data must be valid JSON")
		}
	case BatchContinuationResult:
		if strings.TrimSpace(value.BatchID) == "" || value.ResultVersion <= 0 {
			return fmt.Errorf("batch id and positive result version are required")
		}
		if value.ContextMessageSeq < 0 {
			return fmt.Errorf("context message sequence cannot be negative")
		}
	case ApprovalContinuationResult:
		if strings.TrimSpace(value.ApprovalID) == "" || value.DecisionVersion <= 0 || value.ExecutionEpoch <= 0 {
			return fmt.Errorf("approval id, positive decision version, and positive execution epoch are required")
		}
		if strings.TrimSpace(value.RequestedDecision) == "" || strings.TrimSpace(value.EffectiveStatus) == "" {
			return fmt.Errorf("requested decision and effective status are required")
		}
		if value.ContextMessageSeq < 0 {
			return fmt.Errorf("context message sequence cannot be negative")
		}
		if len(value.CommandResult) > 0 && !json.Valid(value.CommandResult) {
			return fmt.Errorf("approval command result must be valid JSON")
		}
	default:
		return fmt.Errorf("unsupported session input type %T", input)
	}
	return nil
}

func DecodeInput(record SessionInputRecord) (SessionInput, error) {
	switch record.InputType {
	case InputTypeUserMessage:
		var value UserMessage
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return nil, fmt.Errorf("decode user message input: %w", err)
		}
		return value, nil
	case InputTypeResumeRequested:
		var value ResumeRequested
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return nil, fmt.Errorf("decode resume requested input: %w", err)
		}
		return value, nil
	case InputTypeBatchContinuationResult:
		var value BatchContinuationResult
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return nil, fmt.Errorf("decode batch continuation input: %w", err)
		}
		return value, nil
	case InputTypeApprovalContinuation:
		var value ApprovalContinuationResult
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return nil, fmt.Errorf("decode approval continuation input: %w", err)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported input type %q", record.InputType)
	}
}
