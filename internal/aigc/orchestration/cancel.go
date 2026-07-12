package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrCancelReasonInvalid          = errors.New("plan run cancellation reason invalid")
	ErrRunCancellationConflict      = errors.New("plan run cancellation conflict")
	ErrRunCancellationTerminal      = errors.New("terminal plan run cannot be cancelled")
	ErrCancellationPending          = errors.New("plan run cancellation is pending")
	ErrRunCancellationBusy          = errors.New("plan run has an active execution claim")
	ErrBatchCancellationUnavailable = errors.New("generation batch cancellation is unavailable")
	ErrBatchCancellationInvalid     = errors.New("generation batch cancellation target is invalid")
	ErrCancellationDependency       = ErrBatchCancellationUnavailable
)

// BatchCanceller records a durable, idempotent cancellation intent for an
// already-dispatched generation batch.
type BatchCancelRequest struct {
	BatchID   string
	SessionID string
	UserID    string
	PlanRunID string
	NodeID    string
}

type BatchCanceller interface {
	CancelBatch(ctx context.Context, request BatchCancelRequest) error
}

// Cancel transitions any non-terminal run to cancelled. If the run is waiting
// for generation jobs, the durable provider-side cancel intent is recorded
// before the local run transition.
func (s *Scheduler) Cancel(ctx context.Context, runID, reason string) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	runID = strings.TrimSpace(runID)
	reason = strings.TrimSpace(reason)
	if runID == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	if reason == "" || len(reason) > 1024 {
		return PlanRun{}, ErrCancelReasonInvalid
	}
	release, err := s.acquireRunGate(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	defer release()

	current, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	if !current.CancelRequested && !(current.Status == RunStatusSuspended && current.SuspendReason == SuspendWaitingJobs) {
		cancelCtx, cancelImmediate := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
		current, err = s.cancelImmediately(cancelCtx, current, reason)
		cancelImmediate()
		if !errors.Is(err, errCancellationNeedsBatchIntent) {
			return current, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	intentCtx, cancelIntent := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	current, err = s.persistCancelIntent(intentCtx, current, reason)
	cancelIntent()
	if err != nil {
		return current, err
	}
	return s.continueCancellation(ctx, current)
}

func (s *Scheduler) cancelImmediately(ctx context.Context, current PlanRun, reason string) (PlanRun, error) {
	for range maxCASRetries {
		if current.Status == RunStatusSuspended && current.SuspendReason == SuspendWaitingJobs {
			return current, errCancellationNeedsBatchIntent
		}
		if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
			return current, terminalErr
		}
		if current.CancelRequested {
			if current.CancelReason != reason {
				return current, ErrRunCancellationConflict
			}
			return current, ErrCancellationPending
		}
		if hasActiveExecutionClaim(current) {
			return current, ErrRunCancellationBusy
		}
		cancelled, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			if next.Status == RunStatusSuspended && next.SuspendReason == SuspendWaitingJobs {
				return errCancellationNeedsBatchIntent
			}
			if handled, terminalErr := cancellationTerminalResult(*next, reason); handled {
				if terminalErr != nil {
					return terminalErr
				}
				return errCancellationAlreadyApplied
			}
			if next.CancelRequested {
				if next.CancelReason != reason {
					return ErrRunCancellationConflict
				}
				return ErrCancellationPending
			}
			if hasActiveExecutionClaim(*next) {
				return ErrRunCancellationBusy
			}
			return applyCancellation(next, reason)
		})
		if err == nil {
			return cancelled, nil
		}
		if !errors.Is(err, ErrRunVersionConflict) && !errors.Is(err, errCancellationAlreadyApplied) {
			return current, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	return current, fmt.Errorf("%w: immediate cancellation exceeded retry limit", ErrRunVersionConflict)
}

func (s *Scheduler) continueCancellation(ctx context.Context, current PlanRun) (PlanRun, error) {
	if current.Status == RunStatusCancelled {
		return current, nil
	}
	if !current.CancelRequested || strings.TrimSpace(current.CancelReason) == "" {
		return current, ErrRunCancellationConflict
	}
	if current.CancelBatchID != "" {
		if isNilInterface(s.batchCanceller) {
			return current, ErrCancellationDependency
		}
		if err := s.batchCanceller.CancelBatch(ctx, cancellationBatchRequest(current)); err != nil {
			return current, fmt.Errorf("cancel generation batch: %w", err)
		}
	}
	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	defer cancelFinalize()
	return s.finalizeCancellation(finalizeCtx, current, current.CancelReason)
}

func (s *Scheduler) persistCancelIntent(ctx context.Context, current PlanRun, reason string) (PlanRun, error) {
	for range maxCASRetries {
		if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
			return current, terminalErr
		}
		if current.CancelRequested {
			if current.CancelReason != reason {
				return current, ErrRunCancellationConflict
			}
			return current, nil
		}
		if hasActiveExecutionClaim(current) {
			return current, ErrRunCancellationBusy
		}
		batchID := ""
		cancelNodeID := ""
		if current.Status == RunStatusSuspended && current.SuspendReason == SuspendWaitingJobs {
			var err error
			batchID, err = activeWaitingBatch(current)
			if err != nil {
				return current, err
			}
			if isNilInterface(s.batchCanceller) {
				return current, ErrBatchCancellationUnavailable
			}
			cancelNodeID = current.SuspendedNodeID
		}
		requested, mutateErr := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			if handled, terminalErr := cancellationTerminalResult(*next, reason); handled {
				if terminalErr != nil {
					return terminalErr
				}
				return errCancellationAlreadyApplied
			}
			if next.CancelRequested {
				if next.CancelReason != reason {
					return ErrRunCancellationConflict
				}
				return errCancellationAlreadyApplied
			}
			if hasActiveExecutionClaim(*next) {
				return ErrRunCancellationBusy
			}
			if batchID != "" {
				freshBatchID, err := activeWaitingBatch(*next)
				if err != nil {
					return err
				}
				if freshBatchID != batchID {
					return ErrRunVersionConflict
				}
			}
			next.CancelRequested = true
			next.CancelReason = reason
			next.CancelBatchID = batchID
			next.CancelNodeID = cancelNodeID
			return nil
		})
		if mutateErr == nil {
			return requested, nil
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) && !errors.Is(mutateErr, errCancellationAlreadyApplied) {
			return current, mutateErr
		}
		var err error
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
		if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
			return current, terminalErr
		}
	}
	return current, fmt.Errorf("%w: cancellation intent exceeded retry limit", ErrRunVersionConflict)
}

func cancellationBatchRequest(run PlanRun) BatchCancelRequest {
	return BatchCancelRequest{
		BatchID: run.CancelBatchID, SessionID: run.SessionID, UserID: run.UserID,
		PlanRunID: run.ID, NodeID: run.CancelNodeID,
	}
}

func (s *Scheduler) finalizeCancellation(ctx context.Context, current PlanRun, reason string) (PlanRun, error) {
	for range maxCASRetries {
		if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
			return current, terminalErr
		}
		if !current.CancelRequested || current.CancelReason != reason {
			return current, ErrRunCancellationConflict
		}
		cancelled, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			if handled, terminalErr := cancellationTerminalResult(*next, reason); handled {
				if terminalErr != nil {
					return terminalErr
				}
				return errCancellationAlreadyApplied
			}
			if !next.CancelRequested || next.CancelReason != reason {
				return ErrRunCancellationConflict
			}
			return applyCancellation(next, reason)
		})
		if err == nil {
			return cancelled, nil
		}
		if !errors.Is(err, ErrRunVersionConflict) && !errors.Is(err, errCancellationAlreadyApplied) {
			return current, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	return current, fmt.Errorf("%w: cancellation finalize exceeded retry limit", ErrRunVersionConflict)
}

var errCancellationAlreadyApplied = errors.New("plan run cancellation already applied")
var errCancellationNeedsBatchIntent = errors.New("plan run cancellation requires batch intent")

func cancellationTerminalResult(run PlanRun, reason string) (bool, error) {
	switch run.Status {
	case RunStatusCancelled:
		if run.CancelReason != reason {
			return true, ErrRunCancellationConflict
		}
		return true, nil
	case RunStatusSucceeded, RunStatusPartialSucceeded, RunStatusFailed:
		return true, ErrRunCancellationTerminal
	default:
		return false, nil
	}
}

func activeWaitingBatch(run PlanRun) (string, error) {
	node := run.Nodes[run.SuspendedNodeID]
	if node == nil || node.Status != NodeStatusRunning || node.Suspension == nil || node.Suspension.Reason != SuspendWaitingJobs {
		return "", ErrBatchCancellationInvalid
	}
	batchID, _ := node.Suspension.Payload["batch_id"].(string)
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return "", ErrBatchCancellationInvalid
	}
	receiptBatchID, _ := node.Outputs["batch_id"].(string)
	if strings.TrimSpace(receiptBatchID) != batchID {
		return "", ErrBatchCancellationInvalid
	}
	operationID, _ := node.Outputs["operation_id"].(string)
	jobIDs, _ := node.Outputs["job_ids"].([]any)
	if strings.TrimSpace(operationID) == "" || len(jobIDs) == 0 {
		return "", ErrBatchCancellationInvalid
	}
	return batchID, nil
}

func applyCancellation(run *PlanRun, reason string) error {
	if err := ValidateRunTransition(run.Status, RunStatusCancelled); err != nil {
		return err
	}
	for _, node := range run.Nodes {
		if node == nil {
			continue
		}
		if node.Status == NodeStatusPending || node.Status == NodeStatusRunning {
			node.Status = NodeStatusSkipped
			node.SkipReason = SkipReasonCancelled
			node.Fail = nil
		}
		node.Suspension = nil
		clearExecutionClaim(node)
	}
	run.Status = RunStatusCancelled
	run.CancelRequested = true
	run.CancelReason = reason
	run.SuspendReason = ""
	run.SuspendedNodeID = ""
	run.PreviewRequired = false
	return nil
}
