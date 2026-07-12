package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

const (
	defaultMaxParallel = 4
	maxCASRetries      = 8
)

type SchedulerConfig struct {
	Store       RunStore
	Vocabulary  *vocabulary.Registry
	MaxParallel int
	JobBudget   int
	NewID       func() string
}

type Scheduler struct {
	store       RunStore
	vocabulary  *vocabulary.Registry
	maxParallel int
	jobBudget   int
	newID       func() string
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Store == nil {
		return nil, errors.New("scheduler run store is required")
	}
	if cfg.Vocabulary == nil {
		return nil, errors.New("scheduler vocabulary is required")
	}
	if cfg.NewID == nil {
		return nil, errors.New("scheduler id generator is required")
	}
	maxParallel := cfg.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}
	return &Scheduler{
		store: cfg.Store, vocabulary: cfg.Vocabulary, maxParallel: maxParallel,
		jobBudget: cfg.JobBudget, newID: cfg.NewID,
	}, nil
}

func (s *Scheduler) Submit(ctx context.Context, sessionID, userID string, plan ExecutionPlan) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(userID) == "" {
		return PlanRun{}, errors.New("scheduler session id and user id are required")
	}
	if err := plan.Validate(s.vocabulary, s.jobBudget); err != nil {
		return PlanRun{}, err
	}
	if plan.SuccessPolicy != "" && plan.SuccessPolicy != "all_required" {
		return PlanRun{}, fmt.Errorf("%w: unsupported success policy %q", ErrPlanInvalid, plan.SuccessPolicy)
	}
	runID := strings.TrimSpace(s.newID())
	if runID == "" {
		return PlanRun{}, errors.New("scheduler generated an empty run id")
	}
	nodes := make(map[string]*NodeRun, len(plan.Steps))
	for _, step := range plan.Steps {
		nodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
	}
	created, err := s.store.CreateRun(ctx, PlanRun{
		ID: runID, SessionID: sessionID, UserID: userID, Plan: plan,
		Status: RunStatusDraft, Nodes: nodes,
	})
	if err != nil {
		return PlanRun{}, err
	}
	if plan.ExceedsJobBudget(s.jobBudget) {
		return s.store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
			run.Status = RunStatusSuspended
			run.SuspendReason = SuspendWaitingUser
			run.PreviewRequired = true
			return nil
		})
	}
	running, err := s.store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil {
		return PlanRun{}, err
	}
	return s.Advance(ctx, running.ID)
}

func (s *Scheduler) Advance(ctx context.Context, runID string) (PlanRun, error) {
	if ctx == nil {
		return PlanRun{}, errors.New("scheduler context is required")
	}
	if strings.TrimSpace(runID) == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	markConflicts := 0
	for {
		run, err := s.store.GetRun(ctx, runID)
		if err != nil {
			return PlanRun{}, err
		}
		if isTerminalRun(run.Status) || run.Status == RunStatusSuspended {
			return run, nil
		}
		if run.Status != RunStatusRunning {
			return run, fmt.Errorf("cannot advance plan run in status %q", run.Status)
		}

		ready := readySteps(run)
		if len(ready) == 0 {
			if hasRunningNode(run) {
				return run, nil
			}
			return s.finalize(ctx, run)
		}

		marked, err := s.markRunning(ctx, run, ready)
		if errors.Is(err, ErrRunVersionConflict) {
			markConflicts++
			if markConflicts >= maxCASRetries {
				return run, fmt.Errorf("%w: mark ready nodes exceeded retry limit", ErrRunVersionConflict)
			}
			continue
		}
		if err != nil {
			return PlanRun{}, err
		}
		markConflicts = 0
		outcomes := s.executeReady(ctx, marked, ready)
		merged, mergeErr := s.mergeOutcomes(ctx, marked, outcomes)
		if mergeErr != nil {
			if merged.ID != "" {
				finalized, advanceErr := s.Advance(ctx, merged.ID)
				if advanceErr != nil {
					return finalized, errors.Join(mergeErr, advanceErr)
				}
				return finalized, mergeErr
			}
			return merged, mergeErr
		}
		if merged.Status == RunStatusSuspended {
			return merged, nil
		}
	}
}

type nodeOutcome struct {
	step       PlanStep
	attempt    int
	result     vocabulary.Result
	toolErr    error
	resolveErr error
}

func readySteps(run PlanRun) []PlanStep {
	ready := make([]PlanStep, 0)
	for _, step := range run.Plan.Steps {
		node := run.Nodes[step.ID]
		if node == nil || node.Status != NodeStatusPending {
			continue
		}
		readyNow := true
		for _, dependency := range step.DependsOn {
			dependencyRun := run.Nodes[dependency]
			if dependencyRun == nil || (dependencyRun.Status != NodeStatusSucceeded && dependencyRun.Status != NodeStatusSkipped) {
				readyNow = false
				break
			}
		}
		if readyNow {
			ready = append(ready, step)
		}
	}
	return ready
}

func (s *Scheduler) markRunning(ctx context.Context, run PlanRun, ready []PlanStep) (PlanRun, error) {
	return s.store.MutateRun(ctx, run.ID, run.Version, func(next *PlanRun) error {
		for _, step := range ready {
			node := next.Nodes[step.ID]
			if node == nil || node.Status != NodeStatusPending {
				return fmt.Errorf("node %q is no longer pending", step.ID)
			}
			node.Status = NodeStatusRunning
			node.Attempt++
		}
		return nil
	})
}

func (s *Scheduler) executeReady(ctx context.Context, run PlanRun, ready []PlanStep) []nodeOutcome {
	outcomes := make([]nodeOutcome, len(ready))
	semaphore := make(chan struct{}, s.maxParallel)
	var wait sync.WaitGroup
	for index, step := range ready {
		index, step := index, step
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			node := run.Nodes[step.ID]
			outcome := nodeOutcome{step: step, attempt: node.Attempt}
			inputs, err := resolveInputs(step.Params, run.Nodes)
			if err != nil {
				outcome.resolveErr = err
				outcomes[index] = outcome
				return
			}
			tool, ok := s.vocabulary.Get(step.Tool)
			if !ok {
				outcome.resolveErr = fmt.Errorf("tool %q is no longer registered", step.Tool)
				outcomes[index] = outcome
				return
			}
			outcome.result, outcome.toolErr = tool.Run(ctx, vocabulary.Call{
				SessionID: run.SessionID, UserID: run.UserID, PlanRunID: run.ID,
				NodeID: step.ID, Attempt: node.Attempt,
				IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, step.ID, node.Attempt),
				Inputs:         inputs,
			})
			outcomes[index] = outcome
		}()
	}
	wait.Wait()
	return outcomes
}

func (s *Scheduler) mergeOutcomes(ctx context.Context, run PlanRun, outcomes []nodeOutcome) (PlanRun, error) {
	current := run
	for range maxCASRetries {
		merged, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			for _, outcome := range outcomes {
				node := next.Nodes[outcome.step.ID]
				if node == nil {
					return fmt.Errorf("node %q is missing", outcome.step.ID)
				}
				if node.Status != NodeStatusRunning || node.Attempt != outcome.attempt {
					continue
				}
				applyOutcome(node, outcome)
			}
			suspendedNodeIDs := make([]string, 0, 1)
			for _, outcome := range outcomes {
				node := next.Nodes[outcome.step.ID]
				if node != nil && node.Suspension != nil {
					suspendedNodeIDs = append(suspendedNodeIDs, outcome.step.ID)
				}
			}
			if len(suspendedNodeIDs) > 1 {
				for _, nodeID := range suspendedNodeIDs {
					node := next.Nodes[nodeID]
					node.Status = NodeStatusFailed
					node.Suspension = nil
					node.Fail = &vocabulary.Failure{
						Code: "multiple_suspensions", Message: "a ready wave produced multiple suspension points",
					}
				}
				for _, node := range next.Nodes {
					if node != nil && node.Status == NodeStatusPending {
						node.Status = NodeStatusSkipped
					}
				}
				next.Status = RunStatusFailed
				next.SuspendReason = ""
				next.SuspendedNodeID = ""
			} else if len(suspendedNodeIDs) == 1 {
				nodeID := suspendedNodeIDs[0]
				next.Status = RunStatusSuspended
				next.SuspendReason = next.Nodes[nodeID].Suspension.Reason
				next.SuspendedNodeID = nodeID
			}
			return nil
		})
		if err == nil {
			for _, outcome := range outcomes {
				if outcome.resolveErr != nil {
					return merged, outcome.resolveErr
				}
			}
			return merged, nil
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			return PlanRun{}, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	return current, fmt.Errorf("%w: merge outcomes exceeded retry limit", ErrRunVersionConflict)
}

func applyOutcome(node *NodeRun, outcome nodeOutcome) {
	node.Outputs = nil
	node.Fail = nil
	node.Suspension = nil
	switch {
	case outcome.resolveErr != nil:
		node.Status = NodeStatusFailed
		node.Fail = &vocabulary.Failure{Code: "input_resolution_error", Message: outcome.resolveErr.Error()}
	case outcome.toolErr != nil:
		node.Status = NodeStatusFailed
		node.Fail = &vocabulary.Failure{Code: "tool_error", Message: outcome.toolErr.Error(), Retryable: true}
	case outcome.result.Fail != nil:
		node.Status = NodeStatusFailed
		node.Fail = outcome.result.Fail
	case outcome.result.Suspension != nil:
		node.Status = NodeStatusRunning
		node.Suspension = outcome.result.Suspension
	default:
		node.Status = NodeStatusSucceeded
		node.Outputs = outcome.result.Outputs
	}
}

func (s *Scheduler) finalize(ctx context.Context, run PlanRun) (PlanRun, error) {
	current := run
	for range maxCASRetries {
		finalized, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			for _, step := range next.Plan.Steps {
				node := next.Nodes[step.ID]
				if node != nil && node.Status == NodeStatusPending {
					node.Status = NodeStatusSkipped
				}
			}
			next.Status = terminalStatus(*next)
			return nil
		})
		if err == nil {
			return finalized, nil
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			return PlanRun{}, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
		if isTerminalRun(current.Status) || current.Status == RunStatusSuspended {
			return current, nil
		}
		if hasRunningNode(current) || len(readySteps(current)) > 0 {
			return current, nil
		}
	}
	return current, fmt.Errorf("%w: finalize exceeded retry limit", ErrRunVersionConflict)
}

func terminalStatus(run PlanRun) string {
	partial := false
	for _, step := range run.Plan.Steps {
		node := run.Nodes[step.ID]
		if node == nil || (step.Required && node.Status != NodeStatusSucceeded) {
			return RunStatusFailed
		}
		if !step.Required && node.Status != NodeStatusSucceeded {
			partial = true
		}
	}
	if partial {
		return RunStatusPartialSucceeded
	}
	return RunStatusSucceeded
}

func resolveInputs(params map[string]any, nodes map[string]*NodeRun) (map[string]any, error) {
	if params == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(params))
	for key, value := range params {
		next, err := resolveValue(value, nodes)
		if err != nil {
			return nil, fmt.Errorf("resolve input %q: %w", key, err)
		}
		resolved[key] = next
	}
	return resolved, nil
}

func resolveValue(value any, nodes map[string]*NodeRun) (any, error) {
	switch typed := value.(type) {
	case string:
		stepID, outputKey, reference := parseOutputReference(typed)
		if !reference {
			return typed, nil
		}
		node := nodes[stepID]
		if node == nil || (node.Status != NodeStatusSucceeded && node.Status != NodeStatusSkipped) {
			return nil, fmt.Errorf("upstream node %q has not succeeded or been skipped", stepID)
		}
		output, ok := node.Outputs[outputKey]
		if !ok {
			return nil, fmt.Errorf("upstream node %q has no output %q", stepID, outputKey)
		}
		return copyJSONValue(output), nil
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			value, err := resolveValue(item, nodes)
			if err != nil {
				return nil, err
			}
			resolved[index] = value
		}
		return resolved, nil
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, item := range typed {
			value, err := resolveValue(item, nodes)
			if err != nil {
				return nil, err
			}
			resolved[key] = value
		}
		return resolved, nil
	default:
		return typed, nil
	}
}

func copyJSONValue(value any) any {
	switch typed := value.(type) {
	case []any:
		copied := make([]any, len(typed))
		for index, item := range typed {
			copied[index] = copyJSONValue(item)
		}
		return copied
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, item := range typed {
			copied[key] = copyJSONValue(item)
		}
		return copied
	default:
		return typed
	}
}

func parseOutputReference(value string) (stepID, outputKey string, ok bool) {
	if !strings.HasPrefix(value, "$") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "$"), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func hasRunningNode(run PlanRun) bool {
	for _, node := range run.Nodes {
		if node != nil && node.Status == NodeStatusRunning {
			return true
		}
	}
	return false
}

func isTerminalRun(status string) bool {
	switch status {
	case RunStatusSucceeded, RunStatusPartialSucceeded, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}
