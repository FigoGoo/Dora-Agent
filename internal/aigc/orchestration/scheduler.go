package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

const (
	defaultMaxParallel   = 4
	defaultCommitTimeout = 5 * time.Second
	maxCASRetries        = 8
)

type SchedulerConfig struct {
	Store         RunStore
	Vocabulary    *vocabulary.Registry
	MaxParallel   int
	JobBudget     int
	CommitTimeout time.Duration
	NewID         func() string
}

type Scheduler struct {
	store         RunStore
	vocabulary    *vocabulary.Registry
	maxParallel   int
	jobBudget     int
	commitTimeout time.Duration
	newID         func() string
	gateMu        sync.Mutex
	gates         map[string]*runGate
}

type runGate struct {
	token chan struct{}
	refs  int
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
	commitTimeout := cfg.CommitTimeout
	if commitTimeout <= 0 {
		commitTimeout = defaultCommitTimeout
	}
	return &Scheduler{
		store: cfg.Store, vocabulary: cfg.Vocabulary, maxParallel: maxParallel,
		jobBudget: cfg.JobBudget, commitTimeout: commitTimeout, newID: cfg.NewID,
		gates: make(map[string]*runGate),
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
	initialStatus := RunStatusRunning
	suspendReason := ""
	previewRequired := false
	resumeKey := ""
	if plan.ExceedsJobBudget(s.jobBudget) {
		initialStatus = RunStatusSuspended
		suspendReason = SuspendWaitingUser
		previewRequired = true
		resumeKey = fmt.Sprintf("%s:preview:resume", runID)
	}
	created, err := s.store.CreateRun(ctx, PlanRun{
		ID: runID, SessionID: sessionID, UserID: userID, Plan: plan,
		Status: initialStatus, SuspendReason: suspendReason,
		PreviewRequired: previewRequired, ResumeKey: resumeKey, Nodes: nodes,
	})
	if err != nil {
		return PlanRun{}, err
	}
	if created.Status == RunStatusSuspended {
		return created, nil
	}
	return s.Advance(ctx, created.ID)
}

func (s *Scheduler) Advance(ctx context.Context, runID string) (PlanRun, error) {
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
	return s.advance(ctx, runID)
}

func (s *Scheduler) advance(ctx context.Context, runID string) (PlanRun, error) {
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
			return s.finalize(ctx, run)
		}

		outcomes := s.executeReady(ctx, run, ready)
		executionErr := ctx.Err()
		persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
		merged, mergeErr := s.mergeOutcomes(persistCtx, run, outcomes)
		cancelPersist()
		if executionErr == nil {
			executionErr = ctx.Err()
		}
		if mergeErr != nil {
			if executionErr != nil {
				return merged, errors.Join(executionErr, mergeErr)
			}
			var resolutionErr *inputResolutionError
			if errors.As(mergeErr, &resolutionErr) && merged.ID != "" {
				finalized, advanceErr := s.advance(ctx, merged.ID)
				if advanceErr != nil {
					return finalized, errors.Join(mergeErr, advanceErr)
				}
				return finalized, mergeErr
			}
			return merged, mergeErr
		}
		if executionErr != nil {
			return merged, executionErr
		}
		if merged.Status == RunStatusSuspended {
			return merged, nil
		}
	}
}

func (s *Scheduler) acquireRunGate(ctx context.Context, runID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.gateMu.Lock()
	gate := s.gates[runID]
	if gate == nil {
		gate = &runGate{token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		s.gates[runID] = gate
	}
	gate.refs++
	s.gateMu.Unlock()

	select {
	case <-ctx.Done():
		s.releaseRunGate(runID, gate, false)
		return nil, ctx.Err()
	case <-gate.token:
		if err := ctx.Err(); err != nil {
			gate.token <- struct{}{}
			s.releaseRunGate(runID, gate, false)
			return nil, err
		}
		return func() { s.releaseRunGate(runID, gate, true) }, nil
	}
}

func (s *Scheduler) releaseRunGate(runID string, gate *runGate, held bool) {
	if held {
		gate.token <- struct{}{}
	}
	s.gateMu.Lock()
	gate.refs--
	if gate.refs == 0 && s.gates[runID] == gate {
		delete(s.gates, runID)
	}
	s.gateMu.Unlock()
}

type nodeOutcome struct {
	step       PlanStep
	attempt    int
	invoked    bool
	result     vocabulary.Result
	toolErr    error
	resolveErr error
}

type inputResolutionError struct{ error }

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

func (s *Scheduler) executeReady(ctx context.Context, run PlanRun, ready []PlanStep) []nodeOutcome {
	outcomes := make([]nodeOutcome, len(ready))
	for index, step := range ready {
		outcomes[index] = nodeOutcome{step: step, attempt: run.Nodes[step.ID].Attempt + 1}
	}
	type task struct {
		index int
		step  PlanStep
	}
	tasks := make(chan task, len(ready))
	for index, step := range ready {
		tasks <- task{index: index, step: step}
	}
	close(tasks)
	workerCount := min(s.maxParallel, len(ready))
	var wait sync.WaitGroup
	for range workerCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case next, ok := <-tasks:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					executeOutcome(ctx, s.vocabulary, run, next.step, &outcomes[next.index])
				}
			}
		}()
	}
	wait.Wait()
	return outcomes
}

func executeOutcome(ctx context.Context, registry *vocabulary.Registry, run PlanRun, step PlanStep, outcome *nodeOutcome) {
	outcome.invoked = true
	inputs, err := resolveInputs(step.Params, run.Nodes)
	if err != nil {
		outcome.resolveErr = err
		return
	}
	if ctx.Err() != nil {
		outcome.invoked = false
		return
	}
	tool, ok := registry.Get(step.Tool)
	if !ok {
		outcome.resolveErr = fmt.Errorf("tool %q is no longer registered", step.Tool)
		return
	}
	outcome.result, outcome.toolErr = tool.Run(ctx, vocabulary.Call{
		SessionID: run.SessionID, UserID: run.UserID, PlanRunID: run.ID,
		NodeID: step.ID, Attempt: outcome.attempt,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, step.ID, outcome.attempt),
		Inputs:         inputs,
	})
	if outcome.toolErr == nil {
		outcome.result = normalizeToolResult(outcome.result)
	}
}

func normalizeToolResult(result vocabulary.Result) vocabulary.Result {
	invalid := result.Fail != nil && (result.Suspension != nil || result.Outputs != nil)
	if result.Suspension != nil && !knownSuspensionReason(result.Suspension.Reason) {
		invalid = true
	}
	if !invalid {
		return result
	}
	return vocabulary.Result{Fail: &vocabulary.Failure{
		Code: "invalid_tool_result", Message: "tool returned conflicting fields or an unknown suspension reason",
	}}
}

func knownSuspensionReason(reason string) bool {
	switch reason {
	case SuspendWaitingUser, SuspendWaitingAgent, SuspendWaitingJobs:
		return true
	default:
		return false
	}
}

func (s *Scheduler) mergeOutcomes(ctx context.Context, run PlanRun, outcomes []nodeOutcome) (PlanRun, error) {
	if !hasCommittableOutcome(outcomes) {
		return run, firstToolError(outcomes)
	}
	current := run
	for range maxCASRetries {
		merged, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			for _, outcome := range outcomes {
				if !outcome.invoked || outcome.toolErr != nil {
					continue
				}
				node := next.Nodes[outcome.step.ID]
				if node == nil {
					return fmt.Errorf("node %q is missing", outcome.step.ID)
				}
				if node.Status != NodeStatusPending || node.Attempt+1 != outcome.attempt {
					continue
				}
				applyOutcome(node, outcome)
				if node.Suspension == nil && outcome.step.Evaluate && node.Status == NodeStatusSucceeded {
					node.Suspension = &vocabulary.Suspension{Reason: SuspendWaitingAgent}
				}
				if node.Suspension != nil {
					node.ResumeKey = fmt.Sprintf("%s:%s:%d:resume", next.ID, outcome.step.ID, outcome.attempt)
					node.Resumed = false
					node.ResumeDecision = nil
				}
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
					node.ResumeKey = ""
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
					return merged, &inputResolutionError{error: outcome.resolveErr}
				}
			}
			return merged, firstToolError(outcomes)
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			return current, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
		if isTerminalRun(current.Status) || current.Status == RunStatusSuspended {
			return current, firstToolError(outcomes)
		}
	}
	return current, fmt.Errorf("%w: merge outcomes exceeded retry limit", ErrRunVersionConflict)
}

func hasCommittableOutcome(outcomes []nodeOutcome) bool {
	for _, outcome := range outcomes {
		if outcome.invoked && outcome.toolErr == nil {
			return true
		}
	}
	return false
}

func firstToolError(outcomes []nodeOutcome) error {
	for _, outcome := range outcomes {
		if outcome.invoked && outcome.toolErr != nil {
			return outcome.toolErr
		}
	}
	return nil
}

func applyOutcome(node *NodeRun, outcome nodeOutcome) {
	node.Attempt = outcome.attempt
	node.Outputs = nil
	node.Fail = nil
	node.Suspension = nil
	switch {
	case outcome.resolveErr != nil:
		node.Status = NodeStatusFailed
		node.Fail = &vocabulary.Failure{Code: "input_resolution_error", Message: outcome.resolveErr.Error()}
	case outcome.result.Fail != nil:
		node.Status = NodeStatusFailed
		node.Fail = outcome.result.Fail
	case outcome.result.Suspension != nil:
		node.Status = NodeStatusRunning
		node.Outputs = outcome.result.Outputs
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
		if len(readySteps(current)) > 0 {
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

func isTerminalRun(status string) bool {
	switch status {
	case RunStatusSucceeded, RunStatusPartialSucceeded, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}
