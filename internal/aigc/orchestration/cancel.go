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
	if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
		return current, terminalErr
	}

	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	defer cancelCommit()
	cancelledBatches := make(map[string]struct{})
	for range maxCASRetries {
		if err := s.cancelActiveBatch(ctx, current, cancelledBatches); err != nil {
			return current, err
		}
		cancelled, mutateErr := s.store.MutateRun(commitCtx, current.ID, current.Version, func(next *PlanRun) error {
			if handled, terminalErr := cancellationTerminalResult(*next, reason); handled {
				if terminalErr != nil {
					return terminalErr
				}
				return errCancellationAlreadyApplied
			}
			return applyCancellation(next, reason)
		})
		if mutateErr == nil {
			return cancelled, nil
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) && !errors.Is(mutateErr, errCancellationAlreadyApplied) {
			return current, mutateErr
		}
		current, err = s.store.GetRun(commitCtx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
		if handled, terminalErr := cancellationTerminalResult(current, reason); handled {
			return current, terminalErr
		}
	}
	return current, fmt.Errorf("%w: cancellation exceeded retry limit", ErrRunVersionConflict)
}

func (s *Scheduler) cancelActiveBatch(ctx context.Context, run PlanRun, cancelled map[string]struct{}) error {
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingJobs {
		return nil
	}
	batchID, err := activeWaitingBatch(run)
	if err != nil {
		return err
	}
	if _, ok := cancelled[batchID]; ok {
		return nil
	}
	if isNilInterface(s.batchCanceller) {
		return ErrBatchCancellationUnavailable
	}
	if err := s.batchCanceller.CancelBatch(ctx, batchID); err != nil {
		return fmt.Errorf("cancel generation batch: %w", err)
	}
	cancelled[batchID] = struct{}{}
	return nil
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
	run.CancelReason = reason
	run.SuspendReason = ""
	run.SuspendedNodeID = ""
	run.PreviewRequired = false
	return nil
}
