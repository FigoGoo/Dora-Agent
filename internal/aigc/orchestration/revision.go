package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrReviseExecutedStep = errors.New("cannot revise an executed plan step")
	ErrRunNotRevisable    = errors.New("plan run is not revisable")
	ErrRevisionConflict   = errors.New("plan revision conflicts with the current plan")
)

type PlanRevision struct {
	SkipStepIDs []string   `json:"skip_step_ids,omitempty"`
	AppendSteps []PlanStep `json:"append_steps,omitempty"`
}

// Revise commits a live-plan change without advancing or resuming the run.
func (s *Scheduler) Revise(ctx context.Context, runID string, revision PlanRevision) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	if strings.TrimSpace(runID) == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	cloned, err := clonePlanRevision(revision)
	if err != nil {
		return PlanRun{}, err
	}
	if err := validateAppendBatch(cloned.AppendSteps); err != nil {
		return PlanRun{}, err
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
	for range maxCASRetries {
		if current.CancelRequested {
			return current, ErrCancellationPending
		}
		if err := ensureRunRevisable(current); err != nil {
			return current, err
		}
		if err := ensureExistingAppendDefinitions(current.Plan.Steps, cloned.AppendSteps, current.ID); err != nil {
			return current, err
		}
		if revisionEmpty(cloned) || revisionAlreadyApplied(current, cloned) {
			return current, nil
		}

		revised, mutateErr := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			if next.CancelRequested {
				return ErrCancellationPending
			}
			if err := ensureRunRevisable(*next); err != nil {
				return err
			}
			candidatePlan := next.Plan
			candidatePlan.Steps = append([]PlanStep(nil), next.Plan.Steps...)
			candidateNodes := make(map[string]*NodeRun, len(next.Nodes)+len(cloned.AppendSteps))
			for id, node := range next.Nodes {
				candidateNodes[id] = node
			}

			if err := ensureExistingAppendDefinitions(candidatePlan.Steps, cloned.AppendSteps, next.ID); err != nil {
				return err
			}
			for _, stepID := range uniqueStrings(cloned.SkipStepIDs) {
				node, exists := candidateNodes[stepID]
				_, stepExists := findPlanStep(candidatePlan.Steps, stepID)
				if !exists || !stepExists {
					return fmt.Errorf("%w: run %q skip step %q does not exist", ErrPlanInvalid, next.ID, stepID)
				}
				if node.Status != NodeStatusPending {
					return fmt.Errorf("%w: run %q step %q is %s", ErrReviseExecutedStep, next.ID, stepID, node.Status)
				}
				node.Status = NodeStatusSkipped
				node.SkipReason = SkipReasonRevision
			}
			for _, step := range cloned.AppendSteps {
				if existing, exists := findPlanStep(candidatePlan.Steps, step.ID); exists {
					equal, err := planStepsEqual(existing, step)
					if err != nil {
						return err
					}
					if !equal {
						return fmt.Errorf("%w: run %q step id %q has a different definition", ErrRevisionConflict, next.ID, step.ID)
					}
					continue
				}
				candidatePlan.Steps = append(candidatePlan.Steps, step)
				candidateNodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
			}
			if err := candidatePlan.Validate(s.vocabulary, s.jobBudget); err != nil {
				return err
			}
			next.Plan = candidatePlan
			next.Nodes = candidateNodes
			return nil
		})
		if mutateErr == nil {
			return revised, nil
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) {
			return current, mutateErr
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	return current, fmt.Errorf("%w: revision exceeded retry limit", ErrRunVersionConflict)
}

func clonePlanRevision(revision PlanRevision) (PlanRevision, error) {
	data, err := json.Marshal(revision)
	if err != nil {
		return PlanRevision{}, fmt.Errorf("%w: marshal plan revision: %w", ErrRunNotSerializable, err)
	}
	var cloned PlanRevision
	if err := decodeSingleJSONValue(data, &cloned); err != nil {
		return PlanRevision{}, fmt.Errorf("%w: unmarshal plan revision: %w", ErrRunNotSerializable, err)
	}
	return cloned, nil
}

func validateAppendBatch(steps []PlanStep) error {
	seen := make(map[string]struct{}, len(steps))
	for index, step := range steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("%w: appended step %d id is required", ErrPlanInvalid, index)
		}
		if _, duplicate := seen[step.ID]; duplicate {
			return fmt.Errorf("%w: duplicate appended step id %q", ErrRevisionConflict, step.ID)
		}
		seen[step.ID] = struct{}{}
	}
	return nil
}

func ensureRunRevisable(run PlanRun) error {
	if run.Status != RunStatusRunning && run.Status != RunStatusSuspended {
		return fmt.Errorf("%w: run %q is %s", ErrRunNotRevisable, run.ID, run.Status)
	}
	return nil
}

func revisionEmpty(revision PlanRevision) bool {
	return len(revision.SkipStepIDs) == 0 && len(revision.AppendSteps) == 0
}

func revisionAlreadyApplied(run PlanRun, revision PlanRevision) bool {
	for _, stepID := range uniqueStrings(revision.SkipStepIDs) {
		node := run.Nodes[stepID]
		if node == nil || node.Status != NodeStatusSkipped || node.SkipReason != SkipReasonRevision {
			return false
		}
	}
	for _, appended := range revision.AppendSteps {
		existing, ok := findPlanStep(run.Plan.Steps, appended.ID)
		if !ok {
			return false
		}
		equal, err := planStepsEqual(existing, appended)
		if err != nil || !equal {
			return false
		}
	}
	return true
}

func ensureExistingAppendDefinitions(existingSteps, appendedSteps []PlanStep, runID string) error {
	for _, appended := range appendedSteps {
		existing, exists := findPlanStep(existingSteps, appended.ID)
		if !exists {
			continue
		}
		equal, err := planStepsEqual(existing, appended)
		if err != nil {
			return err
		}
		if !equal {
			return fmt.Errorf("%w: run %q step id %q has a different definition", ErrRevisionConflict, runID, appended.ID)
		}
	}
	return nil
}

func uniqueStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func findPlanStep(steps []PlanStep, id string) (PlanStep, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
	}
	return PlanStep{}, false
}

func planStepsEqual(left, right PlanStep) (bool, error) {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false, fmt.Errorf("%w: marshal existing step %q: %w", ErrRunNotSerializable, left.ID, err)
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return false, fmt.Errorf("%w: marshal appended step %q: %w", ErrRunNotSerializable, right.ID, err)
	}
	return bytes.Equal(leftJSON, rightJSON), nil
}
