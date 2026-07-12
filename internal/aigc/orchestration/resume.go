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
		return current, ErrResumeKeyMismatch
	}
	if targetResumed(target, &current) {
		return current, nil
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
				return ErrResumeKeyMismatch
			}
			if targetResumed(freshTarget, next) {
				return nil
			}
			return applyResume(next, freshTarget, clonedDecision)
		})
		if err == nil {
			break
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
			return current, ErrResumeKeyMismatch
		}
		if targetResumed(target, &current) {
			return current, nil
		}
	}
	if err != nil {
		return current, fmt.Errorf("%w: resume exceeded retry limit", ErrRunVersionConflict)
	}
	if callerErr := ctx.Err(); callerErr != nil {
		return committed, callerErr
	}
	return s.advance(ctx, committed.ID)
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

func applyResume(run *PlanRun, target resumeTarget, decision map[string]any) error {
	if target.run {
		if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || !run.PreviewRequired || run.SuspendedNodeID != "" {
			return ErrResumeKeyMismatch
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
		return ErrResumeKeyMismatch
	}
	switch run.SuspendReason {
	case SuspendWaitingUser:
		if target.node.Status != NodeStatusRunning {
			return ErrResumeKeyMismatch
		}
		target.node.Status = NodeStatusSucceeded
	case SuspendWaitingAgent:
		if target.node.Status != NodeStatusSucceeded {
			return ErrResumeKeyMismatch
		}
	case SuspendWaitingJobs:
		return ErrResumeReasonUnsupported
	default:
		return ErrResumeReasonUnsupported
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
		return nil, nil
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
