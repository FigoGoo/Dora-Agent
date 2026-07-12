package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

var (
	ErrResumeKeyMismatch       = errors.New("plan run resume key mismatch")
	ErrResumeReasonUnsupported = errors.New("plan run resume reason unsupported")
	ErrResumeDecisionConflict  = errors.New("plan run resume decision output conflict")
	ErrJobsWaitMismatch        = errors.New("plan run jobs wait mismatch")
	ErrJobsOutcomeInvalid      = errors.New("plan run jobs outcome invalid")
	ErrJobsOutcomeConflict     = errors.New("plan run jobs outcome conflict")
)

const jobsOutcomeReceiptKey = "jobs_outcome_receipt"

type JobsOutcome struct {
	BatchID string         `json:"batch_id"`
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
}

type jobsOutcomeIdentity struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
}

type resumeTarget struct {
	run  bool
	node *NodeRun
}

func (s *Scheduler) CompleteJobsWait(ctx context.Context, runID, nodeID string, outcome JobsOutcome) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(nodeID) == "" {
		return PlanRun{}, errors.New("plan run id and node id are required")
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
	identity, receipt, err := normalizeJobsOutcome(outcome)
	if err != nil {
		return current, err
	}
	if replayed, replayErr, handled := matchJobsOutcomeReceipt(current, nodeID, identity); handled {
		if replayErr != nil {
			return current, replayErr
		}
		return s.continueJobsRun(ctx, replayed)
	}
	if err := validateJobsWait(current, nodeID, identity.BatchID); err != nil {
		return current, err
	}
	summary, err := cloneResumeDecision(outcome.Summary)
	if err != nil {
		return current, fmt.Errorf("%w: summary: %v", ErrJobsOutcomeInvalid, err)
	}

	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	defer cancelCommit()
	for range maxCASRetries {
		committed, mutateErr := s.store.MutateRun(commitCtx, current.ID, current.Version, func(next *PlanRun) error {
			if _, replayErr, handled := matchJobsOutcomeReceipt(*next, nodeID, identity); handled {
				if replayErr != nil {
					return replayErr
				}
				return errResumeAlreadyApplied
			}
			if err := validateJobsWait(*next, nodeID, identity.BatchID); err != nil {
				return err
			}
			return applyJobsOutcome(next, nodeID, identity.Status, receipt, summary)
		})
		if mutateErr == nil {
			return s.continueJobsRun(ctx, committed)
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) && !errors.Is(mutateErr, errResumeAlreadyApplied) {
			if callerErr := ctx.Err(); callerErr != nil {
				return current, errors.Join(callerErr, mutateErr)
			}
			return current, mutateErr
		}
		current, err = s.store.GetRun(commitCtx, current.ID)
		if err != nil {
			if callerErr := ctx.Err(); callerErr != nil {
				return current, errors.Join(callerErr, err)
			}
			return current, err
		}
		if replayed, replayErr, handled := matchJobsOutcomeReceipt(current, nodeID, identity); handled {
			if replayErr != nil {
				return current, replayErr
			}
			return s.continueJobsRun(ctx, replayed)
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) {
			return current, mutateErr
		}
	}
	return current, fmt.Errorf("%w: jobs outcome exceeded retry limit", ErrRunVersionConflict)
}

func normalizeJobsOutcome(outcome JobsOutcome) (jobsOutcomeIdentity, string, error) {
	identity := jobsOutcomeIdentity{BatchID: strings.TrimSpace(outcome.BatchID), Status: strings.TrimSpace(outcome.Status)}
	if identity.BatchID == "" {
		return jobsOutcomeIdentity{}, "", fmt.Errorf("%w: batch_id is required", ErrJobsOutcomeInvalid)
	}
	switch identity.Status {
	case generation.BatchStatusCompleted, generation.BatchStatusPartialFailed, generation.BatchStatusFailed, generation.BatchStatusCancelled:
	default:
		return jobsOutcomeIdentity{}, "", fmt.Errorf("%w: unsupported status %q", ErrJobsOutcomeInvalid, identity.Status)
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return jobsOutcomeIdentity{}, "", fmt.Errorf("%w: marshal receipt: %v", ErrJobsOutcomeInvalid, err)
	}
	return identity, string(raw), nil
}

func validateJobsWait(run PlanRun, nodeID, batchID string) error {
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingJobs || run.SuspendedNodeID != nodeID {
		return fmt.Errorf("%w: run %q node %q is not the active jobs wait", ErrJobsWaitMismatch, run.ID, nodeID)
	}
	node := run.Nodes[nodeID]
	if node == nil || node.Status != NodeStatusRunning || node.Suspension == nil || node.Suspension.Reason != SuspendWaitingJobs {
		return fmt.Errorf("%w: run %q node %q has no jobs suspension", ErrJobsWaitMismatch, run.ID, nodeID)
	}
	waitingBatch, _ := node.Suspension.Payload["batch_id"].(string)
	if strings.TrimSpace(waitingBatch) == "" || strings.TrimSpace(waitingBatch) != batchID {
		return fmt.Errorf("%w: run %q node %q waits for batch %q, got %q", ErrJobsWaitMismatch, run.ID, nodeID, waitingBatch, batchID)
	}
	return nil
}

func matchJobsOutcomeReceipt(run PlanRun, nodeID string, wanted jobsOutcomeIdentity) (PlanRun, error, bool) {
	node := run.Nodes[nodeID]
	if node == nil || node.Outputs == nil {
		return run, nil, false
	}
	receipt, ok := node.Outputs[jobsOutcomeReceiptKey].(string)
	if !ok || strings.TrimSpace(receipt) == "" {
		return run, nil, false
	}
	var existing jobsOutcomeIdentity
	if err := decodeSingleJSONValue([]byte(receipt), &existing); err != nil {
		return run, fmt.Errorf("%w: malformed stored receipt", ErrJobsOutcomeConflict), true
	}
	if existing.BatchID != wanted.BatchID {
		return run, fmt.Errorf("%w: run %q node %q completed batch %q, got %q", ErrJobsWaitMismatch, run.ID, nodeID, existing.BatchID, wanted.BatchID), true
	}
	if existing.Status != wanted.Status {
		return run, fmt.Errorf("%w: batch %q stored status %q, got %q", ErrJobsOutcomeConflict, wanted.BatchID, existing.Status, wanted.Status), true
	}
	return run, nil, true
}

func applyJobsOutcome(run *PlanRun, nodeID, status, receipt string, summary map[string]any) error {
	node := run.Nodes[nodeID]
	step, ok := findPlanStep(run.Plan.Steps, nodeID)
	if node == nil || !ok {
		return fmt.Errorf("%w: run %q node %q is missing", ErrJobsWaitMismatch, run.ID, nodeID)
	}
	if node.Outputs == nil {
		node.Outputs = make(map[string]any)
	}
	node.Outputs[jobsOutcomeReceiptKey] = receipt
	if len(summary) > 0 {
		node.Outputs["jobs_summary"] = summary
	}
	node.Suspension = nil
	node.Resumed = true
	node.Fail = nil
	switch status {
	case generation.BatchStatusCompleted:
		node.Status = NodeStatusSucceeded
	case generation.BatchStatusPartialFailed:
		if step.Required && (run.Plan.SuccessPolicy == "" || run.Plan.SuccessPolicy == "all_required") {
			node.Status = NodeStatusFailed
			node.Fail = &vocabulary.Failure{Code: "generation_partial_failed", Message: "required generation batch partially failed"}
		} else {
			node.Status = NodeStatusSucceeded
		}
	case generation.BatchStatusFailed:
		node.Status = NodeStatusFailed
		node.Fail = &vocabulary.Failure{Code: "generation_failed", Message: "generation batch failed"}
	case generation.BatchStatusCancelled:
		node.Status = NodeStatusFailed
		node.Fail = &vocabulary.Failure{Code: "generation_cancelled", Message: "generation batch was cancelled"}
	default:
		return fmt.Errorf("%w: unsupported status %q", ErrJobsOutcomeInvalid, status)
	}
	run.Status = RunStatusRunning
	run.SuspendReason = ""
	run.SuspendedNodeID = ""
	return nil
}

func (s *Scheduler) continueJobsRun(ctx context.Context, current PlanRun) (PlanRun, error) {
	if current.Status != RunStatusRunning {
		return current, nil
	}
	if err := ctx.Err(); err != nil {
		return current, err
	}
	advanced, err := s.advance(ctx, current.ID)
	if err != nil && advanced.ID == "" {
		return current, err
	}
	return advanced, err
}

func (s *Scheduler) Resume(ctx context.Context, runID, resumeKey string, decision map[string]any) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	if strings.TrimSpace(runID) == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	release, err := s.acquireRunGate(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	defer release()
	return s.resume(ctx, runID, resumeKey, decision)
}

func (s *Scheduler) resume(ctx context.Context, runID, resumeKey string, decision map[string]any) (PlanRun, error) {
	current, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	target, matched := findResumeTarget(&current, resumeKey)
	if !matched {
		return current, resumeKeyMismatch(current.ID, resumeKey)
	}
	if targetResumed(target, &current) {
		return s.continueResumedRun(ctx, current, resumeKey)
	}

	clonedDecision, err := cloneResumeDecision(decision)
	if err != nil {
		return current, err
	}
	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
	defer cancelCommit()

	var committed PlanRun
	for range maxCASRetries {
		committed, err = s.store.MutateRun(commitCtx, current.ID, current.Version, func(next *PlanRun) error {
			freshTarget, ok := findResumeTarget(next, resumeKey)
			if !ok {
				return resumeKeyMismatch(next.ID, resumeKey)
			}
			if targetResumed(freshTarget, next) {
				return errResumeAlreadyApplied
			}
			return applyResume(next, freshTarget, resumeKey, clonedDecision)
		})
		if err == nil {
			break
		}
		if errors.Is(err, errResumeAlreadyApplied) {
			current, err = s.store.GetRun(commitCtx, current.ID)
			if err != nil {
				return PlanRun{}, err
			}
			return s.continueResumedRun(ctx, current, resumeKey)
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			if callerErr := ctx.Err(); callerErr != nil {
				return current, errors.Join(callerErr, err)
			}
			return current, err
		}
		current, err = s.store.GetRun(commitCtx, current.ID)
		if err != nil {
			if callerErr := ctx.Err(); callerErr != nil {
				return PlanRun{}, errors.Join(callerErr, err)
			}
			return PlanRun{}, err
		}
		target, matched = findResumeTarget(&current, resumeKey)
		if !matched {
			return current, resumeKeyMismatch(current.ID, resumeKey)
		}
		if targetResumed(target, &current) {
			return s.continueResumedRun(ctx, current, resumeKey)
		}
	}
	if err != nil {
		return current, fmt.Errorf("%w: resume exceeded retry limit", ErrRunVersionConflict)
	}
	return s.continueResumedRun(ctx, committed, resumeKey)
}

var errResumeAlreadyApplied = errors.New("plan run resume receipt already applied")

func (s *Scheduler) continueResumedRun(ctx context.Context, current PlanRun, resumeKey string) (PlanRun, error) {
	switch {
	case current.Status == RunStatusRunning:
		if err := ctx.Err(); err != nil {
			return current, err
		}
		advanced, err := s.advance(ctx, current.ID)
		if err != nil && advanced.ID == "" {
			return current, err
		}
		return advanced, err
	case current.Status == RunStatusSuspended || isTerminalRun(current.Status):
		return current, nil
	default:
		return current, resumeKeyMismatch(current.ID, resumeKey)
	}
}

func findResumeTarget(run *PlanRun, resumeKey string) (resumeTarget, bool) {
	if resumeKey == "" {
		return resumeTarget{}, false
	}
	if run.ResumeKey == resumeKey {
		return resumeTarget{run: true}, true
	}
	for _, step := range run.Plan.Steps {
		node := run.Nodes[step.ID]
		if node != nil && node.ResumeKey == resumeKey {
			return resumeTarget{node: node}, true
		}
	}
	return resumeTarget{}, false
}

func targetResumed(target resumeTarget, run *PlanRun) bool {
	if target.run {
		return run.Resumed
	}
	return target.node != nil && target.node.Resumed
}

func applyResume(run *PlanRun, target resumeTarget, resumeKey string, decision map[string]any) error {
	if target.run {
		if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || !run.PreviewRequired || run.SuspendedNodeID != "" {
			return resumeKeyMismatch(run.ID, resumeKey)
		}
		run.ResumeDecision = decision
		run.Resumed = true
		run.PreviewRequired = false
		run.SuspendReason = ""
		run.SuspendedNodeID = ""
		run.Status = RunStatusRunning
		return nil
	}
	if run.Status != RunStatusSuspended || run.SuspendedNodeID == "" || target.node == nil || target.node.StepID != run.SuspendedNodeID {
		return resumeKeyMismatch(run.ID, resumeKey)
	}
	if _, exists := target.node.Outputs["resume_decision"]; exists {
		return fmt.Errorf("%w: run %q key %q", ErrResumeDecisionConflict, run.ID, resumeKey)
	}
	switch run.SuspendReason {
	case SuspendWaitingUser:
		if target.node.Status != NodeStatusRunning {
			return resumeKeyMismatch(run.ID, resumeKey)
		}
		target.node.Status = NodeStatusSucceeded
	case SuspendWaitingAgent:
		if target.node.Status != NodeStatusSucceeded {
			return resumeKeyMismatch(run.ID, resumeKey)
		}
	case SuspendWaitingJobs:
		return fmt.Errorf("%w: run %q key %q reason %q", ErrResumeReasonUnsupported, run.ID, resumeKey, run.SuspendReason)
	default:
		return fmt.Errorf("%w: run %q key %q reason %q", ErrResumeReasonUnsupported, run.ID, resumeKey, run.SuspendReason)
	}
	target.node.ResumeDecision = decision
	if target.node.Outputs == nil {
		target.node.Outputs = make(map[string]any)
	}
	target.node.Outputs["resume_decision"] = decision
	target.node.Suspension = nil
	target.node.Resumed = true
	run.Status = RunStatusRunning
	run.SuspendReason = ""
	run.SuspendedNodeID = ""
	return nil
}

func cloneResumeDecision(decision map[string]any) (map[string]any, error) {
	if decision == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(decision)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal resume decision: %v", ErrRunNotSerializable, err)
	}
	var cloned map[string]any
	if err := decodeSingleJSONValue(data, &cloned); err != nil {
		return nil, fmt.Errorf("%w: unmarshal resume decision: %v", ErrRunNotSerializable, err)
	}
	return cloned, nil
}

func resumeKeyMismatch(runID, resumeKey string) error {
	return fmt.Errorf("%w: run %q key %q", ErrResumeKeyMismatch, runID, resumeKey)
}
