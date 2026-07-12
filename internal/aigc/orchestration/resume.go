package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrResumeKeyMismatch       = errors.New("plan run resume key mismatch")
	ErrResumeReasonUnsupported = errors.New("plan run resume reason unsupported")
	ErrResumeDecisionConflict  = errors.New("plan run resume decision output conflict")
)

type resumeTarget struct {
	run  bool
	node *NodeRun
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
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, fmt.Errorf("%w: unmarshal resume decision: %v", ErrRunNotSerializable, err)
	}
	return cloned, nil
}

func resumeKeyMismatch(runID, resumeKey string) error {
	return fmt.Errorf("%w: run %q key %q", ErrResumeKeyMismatch, runID, resumeKey)
}
