package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ErrorStageProvider = "provider"
	ErrorStageArtifact = "artifact"
	ErrorStageBilling  = "billing"
	ErrorStageBinding  = "storyboard_bind"

	ErrorResultSuperseded           = "result_superseded"
	ErrorTargetOrphaned             = "target_orphaned"
	ErrorBindingConflictAfterCharge = "binding_conflict_after_charge"
	ErrorBillingRejected            = "billing_rejected"
	ErrorFinalizeFailed             = "finalize_failed"
)

type ProviderResult struct {
	TaskID        string           `json:"task_id,omitempty"`
	RequestID     string           `json:"request_id,omitempty"`
	Status        string           `json:"status,omitempty"`
	AssetIDs      []string         `json:"asset_ids,omitempty"`
	Payload       map[string]any   `json:"payload,omitempty"`
	UsageReported bool             `json:"usage_reported,omitempty"`
	ActualPoints  int64            `json:"actual_points,omitempty"`
	CostBreakdown map[string]int64 `json:"cost_breakdown,omitempty"`
}

type PendingArtifactStore interface {
	PersistPending(ctx context.Context, job GenerationJob, result ProviderResult) ([]string, error)
}

type BindingCheck struct {
	TargetExists bool
	Matches      bool
	Current      BindingToken
}

type BindingGuard interface {
	Check(ctx context.Context, token BindingToken) (BindingCheck, error)
}

type FinalizationCommit struct {
	Job            GenerationJob
	AssetIDs       []string
	BindingToken   BindingToken
	BindingMode    string
	ApprovalPolicy string
}

// FinalizationCommitter must atomically make assets available and create the
// candidate/active binding selected by the frozen delivery policy.
type FinalizationCommitter interface {
	Commit(ctx context.Context, input FinalizationCommit) error
}

// FinalizationCommitInspector detects the narrow crash window where the
// domain commit succeeded but the generation job transaction did not record
// its terminal state. A committed receipt must win over a later cancel/replan.
type FinalizationCommitInspector interface {
	IsCommitted(ctx context.Context, job GenerationJob, assetIDs []string) (bool, error)
}

// ResultDiscarder makes provider outputs that cannot be delivered explicitly
// unavailable. It prevents superseded, orphaned, cancelled or unpaid assets
// from remaining forever in pending_billing or leaking through asset queries.
type ResultDiscarder interface {
	Discard(ctx context.Context, job GenerationJob, assetIDs []string, disposition string) error
}

type ChargeRequest struct {
	UserID         string
	SessionID      string
	OperationID    string
	BatchID        string
	JobID          string
	IdempotencyKey string
	Points         int64
	Breakdown      map[string]int64
}

type ChargeResult struct {
	TransactionID string
	ChargedPoints int64
	Breakdown     map[string]int64
	BalanceAfter  *int64
}

type RefundRequest struct {
	UserID               string
	SessionID            string
	OperationID          string
	BatchID              string
	JobID                string
	BillingTransactionID string
	IdempotencyKey       string
	Points               int64
}

type RefundResult struct {
	TransactionID  string
	RefundedPoints int64
	BalanceAfter   *int64
}

type BillingGateway interface {
	Charge(ctx context.Context, request ChargeRequest) (ChargeResult, error)
	Refund(ctx context.Context, request RefundRequest) (RefundResult, error)
}

type CostCalculator interface {
	Calculate(ctx context.Context, job GenerationJob, result ProviderResult) (int64, map[string]int64, error)
}

type ExecutionError struct {
	Stage     string
	Code      string
	Retryable bool
	Err       error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *ExecutionError) Unwrap() error { return e.Err }

func NewExecutionError(stage, code string, retryable bool, err error) error {
	return &ExecutionError{Stage: stage, Code: code, Retryable: retryable, Err: err}
}

func classifyExecutionError(err error, defaultStage, defaultCode string) (stage, code string, retryable bool) {
	var execution *ExecutionError
	if errors.As(err, &execution) {
		return valueOrDefault(execution.Stage, defaultStage), valueOrDefault(execution.Code, defaultCode), execution.Retryable
	}
	return defaultStage, defaultCode, ProviderErrorRetryable(err)
}

type FinalizationEngineConfig struct {
	Store      WorkflowStore
	Artifacts  PendingArtifactStore
	Bindings   BindingGuard
	Committer  FinalizationCommitter
	Inspector  FinalizationCommitInspector
	Discarder  ResultDiscarder
	Billing    BillingGateway
	Calculator CostCalculator
	Barrier    *BatchBarrier
	Clock      func() time.Time
}

type FinalizationEngine struct {
	store      WorkflowStore
	artifacts  PendingArtifactStore
	bindings   BindingGuard
	committer  FinalizationCommitter
	inspector  FinalizationCommitInspector
	discarder  ResultDiscarder
	billing    BillingGateway
	calculator CostCalculator
	barrier    *BatchBarrier
	clock      func() time.Time
}

func NewFinalizationEngine(config FinalizationEngineConfig) *FinalizationEngine {
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Barrier == nil && config.Store != nil {
		config.Barrier = NewBatchBarrier(config.Store, config.Clock)
	}
	return &FinalizationEngine{
		store: config.Store, artifacts: config.Artifacts, bindings: config.Bindings,
		committer: config.Committer, inspector: config.Inspector, discarder: config.Discarder, billing: config.Billing, calculator: config.Calculator,
		barrier: config.Barrier, clock: config.Clock,
	}
}

// Finalize resumes safely from any finalization subphase. Artifact and billing
// idempotency keys ensure retries never regenerate media or double charge.
func (f *FinalizationEngine) Finalize(ctx context.Context, jobID string, result ProviderResult) (GenerationJob, error) {
	if f == nil || f.store == nil {
		return GenerationJob{}, fmt.Errorf("finalization store is required")
	}
	job, err := f.store.GetJob(ctx, jobID)
	if err != nil {
		return GenerationJob{}, err
	}
	if IsTerminalJobStatus(job.Status) {
		return job, nil
	}
	job, err = f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFinalizing
		current.Phase = PhaseArtifactFinalize
		current.ProviderTaskID = valueOrDefault(result.TaskID, current.ProviderTaskID)
		current.ProviderRequestID = valueOrDefault(result.RequestID, current.ProviderRequestID)
		current.ProviderStatus = valueOrDefault(result.Status, current.ProviderStatus)
		if result.Payload != nil {
			current.Result = cloneMap(result.Payload)
		}
		if !current.ProviderUsageRecorded {
			current.ProviderUsageRecorded = true
			current.ProviderUsageReported = providerUsageWasReported(result)
			current.ProviderActualPoints = result.ActualPoints
			current.ProviderCostBreakdown = cloneInt64Map(result.CostBreakdown)
		}
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	// The provider usage receipt is immutable and persisted before any external
	// billing call. Recovery therefore reuses the exact charge request even if
	// the process died after Charge succeeded but before recordCharge.
	result.ActualPoints = job.ProviderActualPoints
	result.CostBreakdown = cloneInt64Map(job.ProviderCostBreakdown)
	result.UsageReported = job.ProviderUsageReported
	points, breakdown, err := f.freezeSettlementQuote(ctx, job, result)
	if err != nil {
		return f.handleFinalizeError(ctx, job, err, ErrorStageBilling, ErrorFinalizeFailed)
	}
	job, err = f.store.GetJob(ctx, job.ID)
	if err != nil {
		return GenerationJob{}, err
	}
	receiptAssetIDs := append([]string(nil), job.ResultAssetIDs...)
	if len(receiptAssetIDs) == 0 {
		receiptAssetIDs = append(receiptAssetIDs, result.AssetIDs...)
	}
	if f.inspector != nil && len(receiptAssetIDs) > 0 {
		committed, inspectErr := f.inspector.IsCommitted(ctx, job, receiptAssetIDs)
		if inspectErr != nil {
			return f.handleFinalizeError(ctx, job, NewExecutionError(ErrorStageBinding, ErrorFinalizeFailed, true, inspectErr), ErrorStageBinding, ErrorFinalizeFailed)
		}
		if committed {
			return f.completeCommittedReceipt(ctx, job, receiptAssetIDs)
		}
	}

	if job.CancelRequested {
		return f.cancelBeforeCommit(ctx, job)
	}

	assetIDs := append([]string(nil), job.ResultAssetIDs...)
	if len(assetIDs) == 0 {
		if len(result.AssetIDs) > 0 {
			assetIDs = append([]string(nil), result.AssetIDs...)
		} else if f.artifacts != nil {
			assetIDs, err = f.artifacts.PersistPending(ctx, job, result)
			if err != nil {
				return f.handleFinalizeError(ctx, job, err, ErrorStageArtifact, ErrorFinalizeFailed)
			}
		}
		job, err = f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.ResultAssetIDs = append([]string(nil), assetIDs...)
			return nil, nil
		})
		if err != nil {
			return GenerationJob{}, err
		}
	}

	check, err := f.checkBinding(ctx, job.BindingToken)
	if err != nil {
		return f.handleFinalizeError(ctx, job, err, ErrorStageBinding, ErrorFinalizeFailed)
	}
	if !check.TargetExists {
		if strings.TrimSpace(job.BillingTransactionID) != "" {
			return f.failAfterCharge(ctx, job, check)
		}
		return f.failWithoutCharge(ctx, job, DispositionOrphaned, ErrorTargetOrphaned)
	}
	if !check.Matches {
		if strings.TrimSpace(job.BillingTransactionID) != "" {
			return f.failAfterCharge(ctx, job, check)
		}
		return f.failWithoutCharge(ctx, job, DispositionSuperseded, ErrorResultSuperseded)
	}

	charge := ChargeResult{ChargedPoints: points, Breakdown: cloneInt64Map(breakdown)}
	if points > 0 {
		if f.billing == nil {
			return f.handleFinalizeError(ctx, job, fmt.Errorf("billing gateway is required for non-zero charge"), ErrorStageBilling, ErrorFinalizeFailed)
		}
		key := valueOrDefault(job.BillingIdempotencyKey, "generation:charge:"+job.ID)
		job, err = f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.Phase = PhaseBillingCharge
			current.BillingStatus = BillingCharging
			current.BillingIdempotencyKey = key
			return nil, nil
		})
		if err != nil {
			return GenerationJob{}, err
		}
		charge, err = f.billing.Charge(ctx, ChargeRequest{
			UserID: job.UserID, SessionID: job.SessionID, OperationID: job.OperationID,
			BatchID: job.BatchID, JobID: job.ID, IdempotencyKey: key,
			Points: points, Breakdown: cloneInt64Map(breakdown),
		})
		if err != nil {
			stage, code, retryable := classifyExecutionError(err, ErrorStageBilling, ErrorBillingRejected)
			if retryable {
				return f.scheduleRetry(ctx, job, stage, code, err)
			}
			return f.failPermanent(ctx, job, stage, code, err)
		}
		if strings.TrimSpace(charge.TransactionID) == "" {
			return f.handleFinalizeError(ctx, job, fmt.Errorf("billing transaction id is required"), ErrorStageBilling, ErrorFinalizeFailed)
		}
		job, err = f.recordCharge(ctx, job, charge)
		if err != nil {
			return GenerationJob{}, err
		}
	}

	check, err = f.checkBinding(ctx, job.BindingToken)
	if err != nil {
		return f.handleFinalizeError(ctx, job, err, ErrorStageBinding, ErrorFinalizeFailed)
	}
	if !check.TargetExists || !check.Matches || job.CancelRequested {
		return f.failAfterCharge(ctx, job, check)
	}

	policy := job.DeliveryPolicy.Normalize()
	if err := policy.Validate(); err != nil {
		return f.handleFinalizeError(ctx, job, err, ErrorStageBinding, ErrorFinalizeFailed)
	}
	disposition := DispositionBoundCandidate
	if policy.BindingMode == BindingModeActive {
		disposition = DispositionBoundActive
	}
	beforeCommit := job
	updated, commitErr := f.store.MutateJob(ctx, beforeCommit.ID, beforeCommit.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		if current.CancelRequested {
			return nil, fmt.Errorf("%w: cancellation won finalization CAS", ErrVersionConflict)
		}
		if f.committer != nil {
			// MutateJob owns the generation job row/lock while this local domain
			// commit runs. Implementations must make Asset availability, token
			// revalidation and Candidate/Active binding one idempotent DB commit.
			if err := f.committer.Commit(ctx, FinalizationCommit{
				Job: cloneJob(*current), AssetIDs: append([]string(nil), assetIDs...), BindingToken: current.BindingToken,
				BindingMode: policy.BindingMode, ApprovalPolicy: policy.ApprovalPolicy,
			}); err != nil {
				return nil, err
			}
		}
		current.Status = StatusSucceeded
		current.Phase = PhaseArtifactFinalize
		current.ResultAssetIDs = append([]string(nil), assetIDs...)
		current.ResultDisposition = disposition
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		current.ErrorStage = ""
		current.ErrorCode = ""
		current.ErrorMessage = ""
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":succeeded",
			EventType:      "job.succeeded", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusSucceeded, "result_disposition": disposition},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if commitErr != nil {
		latest, getErr := f.store.GetJob(ctx, beforeCommit.ID)
		if getErr != nil {
			return beforeCommit, errors.Join(commitErr, fmt.Errorf("reload generation job %s after finalization commit failure: %w", beforeCommit.ID, getErr))
		}
		job = latest
		if latest.CancelRequested {
			if strings.TrimSpace(latest.BillingTransactionID) != "" {
				return f.failAfterCharge(ctx, latest, BindingCheck{TargetExists: true, Matches: false})
			}
			return f.cancelBeforeCommit(ctx, latest)
		}
		stage, code, retryable := classifyExecutionError(commitErr, ErrorStageBinding, ErrorFinalizeFailed)
		if retryable {
			return f.scheduleRetry(ctx, job, stage, code, commitErr)
		}
		if strings.TrimSpace(job.BillingTransactionID) != "" {
			return f.failAfterCharge(ctx, job, BindingCheck{TargetExists: true, Matches: false})
		}
		return f.failPermanent(ctx, job, stage, code, commitErr)
	}
	job = updated
	tryFinalizeBatchBestEffort(ctx, f.barrier, job.BatchID)
	return job, nil
}

func (f *FinalizationEngine) completeCommittedReceipt(ctx context.Context, job GenerationJob, assetIDs []string) (GenerationJob, error) {
	policy := job.DeliveryPolicy.Normalize()
	disposition := DispositionBoundCandidate
	if policy.BindingMode == BindingModeActive {
		disposition = DispositionBoundActive
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusSucceeded
		current.Phase = PhaseArtifactFinalize
		current.ResultAssetIDs = append([]string(nil), assetIDs...)
		current.ResultDisposition = disposition
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		current.ErrorStage = ""
		current.ErrorCode = ""
		current.ErrorMessage = ""
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":succeeded",
			EventType:      "job.succeeded", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusSucceeded, "result_disposition": disposition, "recovered_domain_commit": true},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, f.barrier, updated.BatchID)
	}
	return updated, err
}

func (f *FinalizationEngine) recordCharge(ctx context.Context, job GenerationJob, charge ChargeResult) (GenerationJob, error) {
	for attempt := 0; attempt < 4; attempt++ {
		updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			if current.BillingTransactionID != "" && current.BillingTransactionID != charge.TransactionID {
				return nil, fmt.Errorf("job %s already references a different billing transaction", current.ID)
			}
			current.BillingStatus = BillingCharged
			current.BillingTransactionID = charge.TransactionID
			current.ChargedPoints = charge.ChargedPoints
			current.NetChargedPoints = charge.ChargedPoints
			current.CostBreakdown = cloneInt64Map(charge.Breakdown)
			current.BalanceAfter = cloneInt64Pointer(charge.BalanceAfter)
			return nil, nil
		})
		if err == nil {
			return updated, nil
		}
		if !errors.Is(err, ErrVersionConflict) {
			return GenerationJob{}, err
		}
		job, err = f.store.GetJob(ctx, job.ID)
		if err != nil {
			return GenerationJob{}, err
		}
		if job.BillingTransactionID == charge.TransactionID {
			return job, nil
		}
	}
	return GenerationJob{}, fmt.Errorf("%w: could not record billing transaction for job %s", ErrVersionConflict, job.ID)
}

func (f *FinalizationEngine) checkBinding(ctx context.Context, token BindingToken) (BindingCheck, error) {
	if f.bindings == nil {
		return BindingCheck{TargetExists: true, Matches: true, Current: token}, nil
	}
	return f.bindings.Check(ctx, token)
}

func (f *FinalizationEngine) calculateCost(ctx context.Context, job GenerationJob, result ProviderResult) (int64, map[string]int64, error) {
	if f.calculator != nil {
		return f.calculator.Calculate(ctx, job, result)
	}
	return result.ActualPoints, cloneInt64Map(result.CostBreakdown), nil
}

func (f *FinalizationEngine) freezeSettlementQuote(ctx context.Context, job GenerationJob, result ProviderResult) (int64, map[string]int64, error) {
	if job.SettlementQuoteRecorded {
		return job.SettlementPoints, cloneInt64Map(job.SettlementBreakdown), nil
	}
	points, breakdown, err := f.calculateCost(ctx, job, result)
	if err != nil {
		return 0, nil, err
	}
	if points < 0 {
		return 0, nil, fmt.Errorf("settlement points cannot be negative")
	}
	for attempt := 0; attempt < 4; attempt++ {
		updated, mutateErr := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			if current.SettlementQuoteRecorded {
				return nil, nil
			}
			current.SettlementQuoteRecorded = true
			current.SettlementPoints = points
			current.SettlementBreakdown = cloneInt64Map(breakdown)
			return nil, nil
		})
		if mutateErr == nil {
			return updated.SettlementPoints, cloneInt64Map(updated.SettlementBreakdown), nil
		}
		if !errors.Is(mutateErr, ErrVersionConflict) {
			return 0, nil, mutateErr
		}
		job, err = f.store.GetJob(ctx, job.ID)
		if err != nil {
			return 0, nil, err
		}
		if job.SettlementQuoteRecorded {
			return job.SettlementPoints, cloneInt64Map(job.SettlementBreakdown), nil
		}
	}
	return 0, nil, fmt.Errorf("%w: could not freeze settlement quote for job %s", ErrVersionConflict, job.ID)
}

func providerUsageWasReported(result ProviderResult) bool {
	return result.UsageReported || result.ActualPoints != 0 || len(result.CostBreakdown) > 0
}

func (f *FinalizationEngine) failWithoutCharge(ctx context.Context, job GenerationJob, disposition, code string) (GenerationJob, error) {
	if err := f.discardResult(ctx, job, disposition); err != nil {
		return GenerationJob{}, err
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFailed
		current.ResultDisposition = disposition
		current.ErrorStage = ErrorStageBinding
		current.ErrorCode = code
		current.ErrorMessage = code
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":failed:" + code,
			EventType:      "job.failed", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusFailed, "error_code": code, "result_disposition": disposition},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, f.barrier, updated.BatchID)
	}
	return updated, err
}

func (f *FinalizationEngine) failAfterCharge(ctx context.Context, job GenerationJob, check BindingCheck) (GenerationJob, error) {
	disposition := DispositionSuperseded
	code := ErrorBindingConflictAfterCharge
	if !check.TargetExists {
		disposition = DispositionOrphaned
	}
	if err := f.discardResult(ctx, job, disposition); err != nil {
		return GenerationJob{}, err
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		if current.CancelRequested {
			current.Status = StatusCancelled
			current.ErrorCode = "cancelled_after_charge"
		} else {
			current.Status = StatusFailed
			current.ErrorCode = code
		}
		current.ErrorStage = ErrorStageBinding
		current.ErrorMessage = current.ErrorCode
		current.ResultDisposition = disposition
		current.CompensationStatus = CompensationPending
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		current.CompensationEventID = "compensation:" + current.ID
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":compensation-requested:" + current.BillingTransactionID,
			EventType:      EventBillingCompensationRequested, Destination: DestinationBilling,
			Payload: map[string]any{
				"job_id": current.ID, "billing_transaction_id": current.BillingTransactionID,
				"idempotency_key": refundIdempotencyKey(*current),
			},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, f.barrier, updated.BatchID)
	}
	return updated, err
}

func (f *FinalizationEngine) cancelBeforeCommit(ctx context.Context, job GenerationJob) (GenerationJob, error) {
	if strings.TrimSpace(job.BillingTransactionID) != "" {
		return f.failAfterCharge(ctx, job, BindingCheck{TargetExists: true, Matches: false})
	}
	if err := f.discardResult(ctx, job, "cancelled"); err != nil {
		return GenerationJob{}, err
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusCancelled
		current.ErrorCode = "cancelled_by_user"
		current.ErrorMessage = "generation was cancelled before finalization commit"
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":cancelled",
			EventType:      "job.cancelled", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusCancelled, "error_code": current.ErrorCode},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, f.barrier, updated.BatchID)
	}
	return updated, err
}

func (f *FinalizationEngine) scheduleRetry(ctx context.Context, job GenerationJob, stage, code string, cause error) (GenerationJob, error) {
	if job.MaxAttempts > 0 && job.RetryCount+1 >= job.MaxAttempts {
		if stage == ErrorStageBinding && strings.TrimSpace(job.BillingTransactionID) != "" {
			return f.failAfterCharge(ctx, job, BindingCheck{TargetExists: true, Matches: false})
		}
		return f.failPermanent(ctx, job, stage, code, cause)
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRetryWait
		current.ErrorStage = stage
		current.ErrorCode = code
		current.ErrorMessage = cause.Error()
		current.Retryable = true
		current.RetryCount++
		current.NextRunAt = f.clock().Add(retryBackoff(current.RetryCount))
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	return updated, cause
}

func (f *FinalizationEngine) failPermanent(ctx context.Context, job GenerationJob, stage, code string, cause error) (GenerationJob, error) {
	if strings.TrimSpace(job.BillingTransactionID) == "" {
		if err := f.discardResult(ctx, job, "failed"); err != nil {
			return GenerationJob{}, err
		}
	}
	updated, err := f.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFailed
		current.ErrorStage = stage
		current.ErrorCode = code
		current.ErrorMessage = cause.Error()
		current.Retryable = false
		if stage == ErrorStageBilling {
			current.BillingStatus = BillingFailed
		}
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":failed:" + code,
			EventType:      "job.failed", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusFailed, "error_code": code},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, f.barrier, updated.BatchID)
	}
	if err != nil {
		return GenerationJob{}, err
	}
	return updated, cause
}

func (f *FinalizationEngine) discardResult(ctx context.Context, job GenerationJob, disposition string) error {
	if f.discarder == nil || len(job.ResultAssetIDs) == 0 {
		return nil
	}
	return f.discarder.Discard(ctx, job, append([]string(nil), job.ResultAssetIDs...), disposition)
}

// DiscardUndelivered lets the provider phase quarantine a recovered partial
// receipt before marking a job failed/cancelled. It is idempotent because the
// domain discarder writes the same terminal availability/disposition again.
func (f *FinalizationEngine) DiscardUndelivered(ctx context.Context, job GenerationJob, disposition string) error {
	if f == nil {
		return nil
	}
	return f.discardResult(ctx, job, disposition)
}

func (f *FinalizationEngine) handleFinalizeError(ctx context.Context, job GenerationJob, cause error, stage, code string) (GenerationJob, error) {
	errorStage, errorCode, retryable := classifyExecutionError(cause, stage, code)
	if retryable {
		return f.scheduleRetry(ctx, job, errorStage, errorCode, cause)
	}
	if strings.TrimSpace(job.BillingTransactionID) != "" {
		return f.failAfterCharge(ctx, job, BindingCheck{TargetExists: true, Matches: false})
	}
	return f.failPermanent(ctx, job, errorStage, errorCode, cause)
}

type CompensationServiceConfig struct {
	Store   WorkflowStore
	Billing BillingGateway
	Barrier *BatchBarrier
	Clock   func() time.Time
}

type CompensationService struct {
	store   WorkflowStore
	billing BillingGateway
	barrier *BatchBarrier
	clock   func() time.Time
}

func NewCompensationService(config CompensationServiceConfig) *CompensationService {
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Barrier == nil && config.Store != nil {
		config.Barrier = NewBatchBarrier(config.Store, config.Clock)
	}
	return &CompensationService{store: config.Store, billing: config.Billing, barrier: config.Barrier, clock: config.Clock}
}

func (s *CompensationService) Run(ctx context.Context, jobID string) (GenerationJob, error) {
	if s == nil || s.store == nil || s.billing == nil {
		return GenerationJob{}, fmt.Errorf("compensation store and billing gateway are required")
	}
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return GenerationJob{}, err
	}
	status := normalizedCompensationStatus(job.CompensationStatus)
	if status == CompensationCompleted || status == CompensationManualFinal || status == CompensationNotRequired {
		return job, nil
	}
	if status == CompensationRunning && job.LeaseUntil != nil && job.LeaseUntil.After(s.clock()) {
		return job, NewExecutionError(ErrorStageBilling, "compensation_in_progress", true, fmt.Errorf("job %s compensation is already in progress", job.ID))
	}
	if status != CompensationPending && status != CompensationRetryWait && status != CompensationRunning {
		return GenerationJob{}, fmt.Errorf("job %s has invalid compensation status %q", job.ID, status)
	}
	if strings.TrimSpace(job.BillingTransactionID) == "" {
		return GenerationJob{}, fmt.Errorf("job %s has no billing transaction to compensate", job.ID)
	}
	claimID := "compensation:" + defaultID()
	job, err = s.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.CompensationStatus = CompensationRunning
		current.Phase = PhaseCompensation
		current.LeaseOwner = claimID
		until := s.clock().Add(5 * time.Minute)
		current.LeaseUntil = &until
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	refund, err := s.billing.Refund(ctx, RefundRequest{
		UserID: job.UserID, SessionID: job.SessionID, OperationID: job.OperationID,
		BatchID: job.BatchID, JobID: job.ID, BillingTransactionID: job.BillingTransactionID,
		IdempotencyKey: refundIdempotencyKey(job), Points: job.ChargedPoints,
	})
	if err == nil && strings.TrimSpace(refund.TransactionID) == "" {
		err = fmt.Errorf("billing refund transaction id is required")
	}
	if err == nil && (refund.RefundedPoints < 0 || refund.RefundedPoints > job.ChargedPoints) {
		err = fmt.Errorf("refunded points %d are outside charged range 0..%d", refund.RefundedPoints, job.ChargedPoints)
	}
	if err != nil {
		retryable := true
		var execution *ExecutionError
		if errors.As(err, &execution) {
			retryable = execution.Retryable
		}
		if !retryable {
			updated, updateErr := s.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
				current.CompensationStatus = CompensationPending
				current.Phase = PhaseCompensation
				current.ErrorStage = ErrorStageBilling
				current.ErrorCode = "compensation_failed"
				current.ErrorMessage = err.Error()
				current.Retryable = false
				current.NextRunAt = time.Time{}
				current.LeaseOwner = ""
				current.LeaseUntil = nil
				return []OutboxEvent{{
					IdempotencyKey: "job:" + current.ID + ":compensation-failed:" + current.BillingTransactionID,
					EventType:      EventBillingCompensationFailed,
					Destination:    DestinationSessionSignal,
					Payload: map[string]any{
						"job_id": current.ID, "billing_transaction_id": current.BillingTransactionID,
						"requires_manual_finalization": true,
					},
				}}, nil
			})
			if updateErr != nil {
				return GenerationJob{}, updateErr
			}
			return updated, err
		}
		updated, updateErr := s.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.CompensationStatus = CompensationRetryWait
			current.Phase = PhaseCompensation
			current.ErrorStage = ErrorStageBilling
			current.ErrorCode = "compensation_failed"
			current.ErrorMessage = err.Error()
			current.Retryable = true
			current.RetryCount++
			current.NextRunAt = s.clock().Add(retryBackoff(current.RetryCount))
			current.LeaseOwner = ""
			current.LeaseUntil = nil
			return []OutboxEvent{{
				IdempotencyKey: fmt.Sprintf("job:%s:compensation-retry:%d", current.ID, current.RetryCount),
				EventType:      EventBillingCompensationRequested,
				Destination:    DestinationBilling,
				AvailableAt:    current.NextRunAt,
				Payload: map[string]any{
					"job_id": current.ID, "billing_transaction_id": current.BillingTransactionID,
					"idempotency_key": refundIdempotencyKey(*current),
				},
			}}, nil
		})
		if updateErr != nil {
			return GenerationJob{}, updateErr
		}
		return updated, err
	}
	updated, err := s.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.CompensationStatus = CompensationCompleted
		current.CompensatedPoints = refund.RefundedPoints
		current.NetChargedPoints = current.ChargedPoints - refund.RefundedPoints
		current.RefundTransactionID = refund.TransactionID
		current.BalanceAfter = cloneInt64Pointer(refund.BalanceAfter)
		current.Retryable = false
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":compensation-completed:" + refund.TransactionID,
			EventType:      EventBillingCompensationCompleted, Destination: DestinationSessionSignal,
			Payload: map[string]any{"job_id": current.ID, "refund_transaction_id": refund.TransactionID, "refunded_points": refund.RefundedPoints},
		}, newBatchFinalizeRequestedEvent(*current, "compensation-settled")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, s.barrier, updated.BatchID)
	}
	return updated, err
}

// ManualFinalize is an explicit business decision after permanent compensation
// failure. It freezes the auditable net amount and unblocks the Batch Barrier.
func (s *CompensationService) ManualFinalize(ctx context.Context, jobID string, refundedPoints int64, refundTransactionID string) (GenerationJob, error) {
	if s == nil || s.store == nil {
		return GenerationJob{}, fmt.Errorf("compensation store is required")
	}
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return GenerationJob{}, err
	}
	if refundedPoints < 0 || refundedPoints > job.ChargedPoints {
		return GenerationJob{}, fmt.Errorf("manual refunded points must be between zero and charged points")
	}
	if normalizedCompensationStatus(job.CompensationStatus) == CompensationManualFinal {
		return job, nil
	}
	status := normalizedCompensationStatus(job.CompensationStatus)
	if status != CompensationPending || job.Retryable || job.ErrorCode != "compensation_failed" {
		return GenerationJob{}, fmt.Errorf("job %s compensation status %q cannot be manually finalized", job.ID, status)
	}
	refundTransactionID = strings.TrimSpace(refundTransactionID)
	if refundedPoints > 0 && refundTransactionID == "" {
		return GenerationJob{}, fmt.Errorf("manual refund transaction id is required when refunded points are non-zero")
	}
	updated, err := s.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.CompensationStatus = CompensationManualFinal
		current.CompensatedPoints = refundedPoints
		current.NetChargedPoints = current.ChargedPoints - refundedPoints
		current.RefundTransactionID = refundTransactionID
		current.Retryable = false
		current.NextRunAt = time.Time{}
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":compensation-manual-final",
			EventType:      EventBillingCompensationCompleted,
			Destination:    DestinationSessionSignal,
			Payload: map[string]any{
				"job_id": current.ID, "refund_transaction_id": refundTransactionID,
				"refunded_points": refundedPoints, "manual_final": true,
			},
		}, newBatchFinalizeRequestedEvent(*current, "compensation-settled")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, s.barrier, updated.BatchID)
	}
	return updated, err
}

func refundIdempotencyKey(job GenerationJob) string {
	return "generation:refund:" + job.ID + ":" + job.BillingTransactionID
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Second * time.Duration(1<<(attempt-1))
}
