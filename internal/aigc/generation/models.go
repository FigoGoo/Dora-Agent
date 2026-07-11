package generation

import (
	"fmt"
	"strings"
	"time"
)

const (
	StatusQueued          = "queued"
	StatusRunning         = "running"
	StatusWaitingProvider = "waiting_provider"
	StatusFinalizing      = "finalizing"
	StatusRetryWait       = "retry_wait"
	StatusSucceeded       = "succeeded"
	StatusFailed          = "failed"
	StatusCancelled       = "cancelled"

	OperationStatusAccepted      = "accepted"
	OperationStatusWaitingJobs   = "waiting_jobs"
	OperationStatusCompleted     = "completed"
	OperationStatusPartialFailed = "partial_failed"
	OperationStatusFailed        = "failed"
	OperationStatusCancelled     = "cancelled"

	BatchStatusWaitingJobs   = "waiting_jobs"
	BatchStatusFinalizing    = "finalizing"
	BatchStatusCancelling    = "cancelling"
	BatchStatusCompleted     = "completed"
	BatchStatusPartialFailed = "partial_failed"
	BatchStatusFailed        = "failed"
	BatchStatusCancelled     = "cancelled"

	CompletionAllRequired  = "all_required"
	CompletionAllowPartial = "allow_partial"
	CompletionMinSuccess   = "min_success"

	WakeOnTerminal = "on_terminal"
	WakeOnFailure  = "on_failure"
	WakeNever      = "never"

	BindingModeCandidate = "candidate"
	BindingModeActive    = "active"

	ApprovalReviewRequired = "review_required"
	ApprovalAutoApprove    = "auto_approve"

	ChargePostpaidNoReservation = "postpaid_no_reservation"
	ChargeReserveThenSettle     = "reserve_then_settle"

	PhaseProviderSubmit   = "provider_submit"
	PhaseProviderPoll     = "provider_poll"
	PhaseArtifactFinalize = "artifact_finalize"
	PhaseBillingCharge    = "billing_charge"
	PhaseCompensation     = "compensation"

	DispositionBoundCandidate = "bound_candidate"
	DispositionBoundActive    = "bound_active"
	DispositionSuperseded     = "superseded"
	DispositionOrphaned       = "orphaned"

	BillingNotStarted = "not_started"
	BillingCharging   = "charging"
	BillingCharged    = "charged"
	BillingFailed     = "failed"

	CompensationNotRequired = "not_required"
	CompensationPending     = "pending"
	CompensationRunning     = "running"
	CompensationRetryWait   = "retry_wait"
	CompensationCompleted   = "completed"
	CompensationManualFinal = "manual_final"

	ProviderImage2   = "image2"
	ProviderSeedance = "seedance"
	ProviderAudio    = "audio"
	ProviderAssembly = "assembly"

	TargetKeyElement = "key_element"
	TargetShot       = "shot"
	TargetAudioLayer = "audio_layer"

	// DefaultProviderMaxPollAttempts bounds provider poll calls, including calls
	// that keep returning a non-terminal state. The counter is persisted on the
	// job so a worker restart cannot reset the budget.
	DefaultProviderMaxPollAttempts = 120
)

// GenerationOperation is the durable, user-visible unit initiated by one tool
// or UI command. An operation currently owns exactly one batch.
type GenerationOperation struct {
	ID                 string         `json:"id"`
	SessionID          string         `json:"session_id"`
	UserID             string         `json:"user_id,omitempty"`
	WorkflowRunID      string         `json:"workflow_run_id,omitempty"`
	StageRunID         string         `json:"stage_run_id,omitempty"`
	ToolCallID         string         `json:"tool_call_id,omitempty"`
	IdempotencyKey     string         `json:"idempotency_key"`
	RequestFingerprint string         `json:"request_fingerprint,omitempty"`
	Kind               string         `json:"kind,omitempty"`
	Status             string         `json:"status"`
	BatchID            string         `json:"batch_id"`
	Result             map[string]any `json:"result,omitempty"`
	ErrorCode          string         `json:"error_code,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	Version            int            `json:"version"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	TerminalAt         *time.Time     `json:"terminal_at,omitempty"`
}

// DeliveryPolicy is frozen when a batch/job is created. Workers must never
// infer these values from a provider response.
type DeliveryPolicy struct {
	BindingMode    string `json:"binding_mode"`
	ApprovalPolicy string `json:"approval_policy"`
	ChargePolicy   string `json:"charge_policy"`
}

func (p DeliveryPolicy) Normalize() DeliveryPolicy {
	if p.BindingMode == "" {
		p.BindingMode = BindingModeCandidate
	}
	if p.ApprovalPolicy == "" {
		p.ApprovalPolicy = ApprovalReviewRequired
	}
	if p.ChargePolicy == "" {
		p.ChargePolicy = ChargePostpaidNoReservation
	}
	return p
}

func (p DeliveryPolicy) Validate() error {
	p = p.Normalize()
	if p.BindingMode != BindingModeCandidate && p.BindingMode != BindingModeActive {
		return fmt.Errorf("invalid binding mode %q", p.BindingMode)
	}
	if p.ApprovalPolicy != ApprovalReviewRequired && p.ApprovalPolicy != ApprovalAutoApprove {
		return fmt.Errorf("invalid approval policy %q", p.ApprovalPolicy)
	}
	if p.ChargePolicy != ChargePostpaidNoReservation && p.ChargePolicy != ChargeReserveThenSettle {
		return fmt.Errorf("invalid charge policy %q", p.ChargePolicy)
	}
	if p.ApprovalPolicy == ApprovalReviewRequired && p.BindingMode != BindingModeCandidate {
		return fmt.Errorf("review-required delivery must create a candidate binding")
	}
	return nil
}

// BindingToken identifies the exact storyboard target/prompt/slot revision for
// which a generation result is valid. Global storyboard version is deliberately
// excluded because unrelated edits must not supersede a valid result.
type BindingToken struct {
	StoryboardID     string `json:"storyboard_id"`
	TargetID         string `json:"target_id"`
	AssetSlot        string `json:"asset_slot"`
	TargetRevision   int    `json:"target_revision"`
	PromptRevision   int    `json:"prompt_revision"`
	GenerationEpoch  int    `json:"generation_epoch"`
	SpecVersion      int    `json:"spec_version,omitempty"`
	AggregateVersion int    `json:"aggregate_version,omitempty"`
	InputFingerprint string `json:"input_fingerprint"`
}

func (t BindingToken) Validate() error {
	if strings.TrimSpace(t.StoryboardID) == "" || strings.TrimSpace(t.TargetID) == "" || strings.TrimSpace(t.AssetSlot) == "" {
		return fmt.Errorf("binding token storyboard, target and asset slot are required")
	}
	if t.TargetRevision < 0 || t.PromptRevision < 0 || t.GenerationEpoch < 0 || t.SpecVersion < 0 || t.AggregateVersion < 0 {
		return fmt.Errorf("binding token revisions cannot be negative")
	}
	if strings.TrimSpace(t.InputFingerprint) == "" {
		return fmt.Errorf("binding token input fingerprint is required")
	}
	return nil
}

func (t BindingToken) Equal(other BindingToken) bool {
	return strings.TrimSpace(t.StoryboardID) == strings.TrimSpace(other.StoryboardID) &&
		strings.TrimSpace(t.TargetID) == strings.TrimSpace(other.TargetID) &&
		strings.TrimSpace(t.AssetSlot) == strings.TrimSpace(other.AssetSlot) &&
		t.TargetRevision == other.TargetRevision &&
		t.PromptRevision == other.PromptRevision &&
		t.GenerationEpoch == other.GenerationEpoch &&
		t.SpecVersion == other.SpecVersion &&
		t.AggregateVersion == other.AggregateVersion &&
		strings.TrimSpace(t.InputFingerprint) == strings.TrimSpace(other.InputFingerprint)
}

type CostSummary struct {
	GrossChargedPoints int64            `json:"gross_charged_points"`
	RefundedPoints     int64            `json:"refunded_points"`
	NetChargedPoints   int64            `json:"net_charged_points"`
	Breakdown          map[string]int64 `json:"breakdown,omitempty"`
	BalanceAfter       *int64           `json:"balance_after,omitempty"`
}

// GenerationBatch is the barrier aggregate for a group of jobs.
type GenerationBatch struct {
	ID                        string         `json:"id"`
	SessionID                 string         `json:"session_id"`
	UserID                    string         `json:"user_id,omitempty"`
	WorkflowRunID             string         `json:"workflow_run_id,omitempty"`
	StageRunID                string         `json:"stage_run_id,omitempty"`
	OperationID               string         `json:"operation_id"`
	ToolCallID                string         `json:"tool_call_id,omitempty"`
	Kind                      string         `json:"kind,omitempty"`
	Status                    string         `json:"status"`
	CompletionPolicy          string         `json:"completion_policy"`
	MinSuccess                int            `json:"min_success,omitempty"`
	WakePolicy                string         `json:"wake_policy,omitempty"`
	DeliveryPolicy            DeliveryPolicy `json:"delivery_policy"`
	RequiredJobs              int            `json:"required_jobs"`
	OptionalJobs              int            `json:"optional_jobs"`
	SucceededJobs             int            `json:"succeeded_jobs"`
	FailedJobs                int            `json:"failed_jobs"`
	CancelledJobs             int            `json:"cancelled_jobs"`
	ExpectedSpecVersion       int            `json:"expected_spec_version,omitempty"`
	ExpectedStoryboardVersion int            `json:"expected_storyboard_version,omitempty"`
	ResultStoryboardVersion   int            `json:"result_storyboard_version,omitempty"`
	Cost                      CostSummary    `json:"cost"`
	CancelRequested           bool           `json:"cancel_requested"`
	CancelRequestedAt         *time.Time     `json:"cancel_requested_at,omitempty"`
	ErrorCode                 string         `json:"error_code,omitempty"`
	ErrorMessage              string         `json:"error_message,omitempty"`
	Version                   int            `json:"version"`
	CreatedAt                 time.Time      `json:"created_at"`
	UpdatedAt                 time.Time      `json:"updated_at"`
	TerminalAt                *time.Time     `json:"terminal_at,omitempty"`
}

type GenerationJob struct {
	ID                          string           `json:"id"`
	BatchID                     string           `json:"batch_id,omitempty"`
	OperationID                 string           `json:"operation_id,omitempty"`
	SessionID                   string           `json:"session_id"`
	UserID                      string           `json:"user_id,omitempty"`
	WorkflowRunID               string           `json:"workflow_run_id,omitempty"`
	StageRunID                  string           `json:"stage_run_id,omitempty"`
	StoryboardID                string           `json:"storyboard_id,omitempty"`
	ToolCallID                  string           `json:"tool_call_id,omitempty"`
	IdempotencyKey              string           `json:"idempotency_key"`
	Provider                    string           `json:"provider,omitempty"`
	MediaKind                   string           `json:"media_kind,omitempty"`
	TargetType                  string           `json:"target_type,omitempty"`
	TargetID                    string           `json:"target_id,omitempty"`
	AssetSlot                   string           `json:"asset_slot,omitempty"`
	VariantKey                  string           `json:"variant_key,omitempty"`
	Required                    bool             `json:"required"`
	StoryboardVersionAtDispatch int              `json:"storyboard_version_at_dispatch,omitempty"`
	BindingToken                BindingToken     `json:"binding_token"`
	DeliveryPolicy              DeliveryPolicy   `json:"delivery_policy"`
	Status                      string           `json:"status"`
	Phase                       string           `json:"phase,omitempty"`
	RetryCount                  int              `json:"retry_count"`
	MaxRetries                  int              `json:"max_retries"`
	Attempt                     int              `json:"attempt"`
	MaxAttempts                 int              `json:"max_attempts"`
	ProviderPollAttempts        int              `json:"provider_poll_attempts"`
	MaxProviderPollAttempts     int              `json:"max_provider_poll_attempts"`
	NextRunAt                   time.Time        `json:"next_run_at,omitempty"`
	LeaseOwner                  string           `json:"lease_owner,omitempty"`
	LeaseUntil                  *time.Time       `json:"lease_until,omitempty"`
	StatusVersion               int              `json:"status_version"`
	ProviderTaskID              string           `json:"provider_task_id,omitempty"`
	ProviderRequestID           string           `json:"provider_request_id,omitempty"`
	ProviderStatus              string           `json:"provider_status,omitempty"`
	Payload                     map[string]any   `json:"payload,omitempty"`
	Result                      map[string]any   `json:"result,omitempty"`
	ResultAssetIDs              []string         `json:"result_asset_ids,omitempty"`
	ResultDisposition           string           `json:"result_disposition,omitempty"`
	ProviderUsageRecorded       bool             `json:"provider_usage_recorded,omitempty"`
	ProviderUsageReported       bool             `json:"provider_usage_reported,omitempty"`
	ProviderActualPoints        int64            `json:"provider_actual_points,omitempty"`
	ProviderCostBreakdown       map[string]int64 `json:"provider_cost_breakdown,omitempty"`
	SettlementQuoteRecorded     bool             `json:"settlement_quote_recorded,omitempty"`
	SettlementPoints            int64            `json:"settlement_points,omitempty"`
	SettlementBreakdown         map[string]int64 `json:"settlement_breakdown,omitempty"`
	ChargedPoints               int64            `json:"charged_points,omitempty"`
	CompensatedPoints           int64            `json:"compensated_points,omitempty"`
	NetChargedPoints            int64            `json:"net_charged_points,omitempty"`
	CostBreakdown               map[string]int64 `json:"cost_breakdown,omitempty"`
	BillingStatus               string           `json:"billing_status,omitempty"`
	BillingTransactionID        string           `json:"billing_transaction_id,omitempty"`
	BillingIdempotencyKey       string           `json:"billing_idempotency_key,omitempty"`
	CompensationStatus          string           `json:"compensation_status,omitempty"`
	CompensationEventID         string           `json:"compensation_event_id,omitempty"`
	RefundTransactionID         string           `json:"refund_transaction_id,omitempty"`
	BalanceAfter                *int64           `json:"balance_after,omitempty"`
	CancelRequested             bool             `json:"cancel_requested"`
	ErrorStage                  string           `json:"error_stage,omitempty"`
	ErrorCode                   string           `json:"error_code,omitempty"`
	ErrorMessage                string           `json:"error_message,omitempty"`
	Retryable                   bool             `json:"retryable"`
	CreatedAt                   time.Time        `json:"created_at"`
	UpdatedAt                   time.Time        `json:"updated_at"`
	StartedAt                   *time.Time       `json:"started_at,omitempty"`
	TerminalAt                  *time.Time       `json:"terminal_at,omitempty"`
}

type StatusUpdate struct {
	ResultAssetIDs    []string
	Result            map[string]any
	Phase             string
	ProviderTaskID    string
	ProviderStatus    string
	ResultDisposition string
	ErrorStage        string
	ErrorCode         string
	ErrorMessage      string
}

func NormalizeStatus(status string) string {
	switch status {
	case StatusQueued, StatusRunning, StatusWaitingProvider, StatusFinalizing, StatusRetryWait, StatusSucceeded, StatusFailed, StatusCancelled:
		return status
	default:
		return ""
	}
}

func IsTerminalJobStatus(status string) bool {
	return status == StatusSucceeded || status == StatusFailed || status == StatusCancelled
}

func IsTerminalBatchStatus(status string) bool {
	return status == BatchStatusCompleted || status == BatchStatusPartialFailed || status == BatchStatusFailed || status == BatchStatusCancelled
}

func IsTerminalOperationStatus(status string) bool {
	return status == OperationStatusCompleted || status == OperationStatusPartialFailed || status == OperationStatusFailed || status == OperationStatusCancelled
}

func normalizedMaxProviderPollAttempts(value int) int {
	if value <= 0 {
		return DefaultProviderMaxPollAttempts
	}
	return value
}
