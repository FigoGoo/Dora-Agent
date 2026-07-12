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
	ErrBatchCancellationUnavailable = errors.New("generation batch cancellation is unavailable")
	ErrBatchCancellationInvalid     = errors.New("generation batch cancellation target is invalid")
)

// BatchCanceller records a durable, idempotent cancellation intent for an
// already-dispatched generation batch.
type BatchCanceller interface {
	CancelBatch(ctx context.Context, batchID string) error
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
	intentCtx, cancelIntent := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	current, err = s.persistCancelIntent(intentCtx, current, reason)
	cancelIntent()
	if err != nil {
		return current, err
	}
	if current.Status == RunStatusCancelled {
		return current, nil
	}
	if current.CancelBatchID != "" {
		if isNilInterface(s.batchCanceller) {
			return current, ErrBatchCancellationUnavailable
		}
		if err := s.batchCanceller.CancelBatch(ctx, current.CancelBatchID); err != nil {
			return current, fmt.Errorf("cancel generation batch: %w", err)
		}
	}
	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	defer cancelFinalize()
	return s.finalizeCancellation(finalizeCtx, current, reason)
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
			for _, node := range next.Nodes {
				if node != nil {
					clearExecutionClaim(node)
				}
			}
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
