package orchestration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

const (
	defaultMaxParallel   = 4
	defaultCommitTimeout = 5 * time.Second
	defaultLeaseTTL      = 30 * time.Second
	maxCASRetries        = 8
)

type SchedulerConfig struct {
	Store             RunStore
	Vocabulary        *vocabulary.Registry
	Guard             vocabulary.Guard
	MaxParallel       int
	JobBudget         int
	CommitTimeout     time.Duration
	NewID             func() string
	OwnerID           string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	// Now must use one authoritative clock across Scheduler instances. Production
	// persistence must inject a shared database clock, never process-local time.Now.
	Now      func() time.Time
	NewToken func() string
}

type Scheduler struct {
	store             RunStore
	vocabulary        *vocabulary.Registry
	guard             vocabulary.Guard
	maxParallel       int
	jobBudget         int
	commitTimeout     time.Duration
	newID             func() string
	ownerID           string
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	now               func() time.Time
	newToken          func() string
	gateMu            sync.Mutex
	gates             map[string]*runGate
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
	if strings.TrimSpace(cfg.OwnerID) == "" {
		return nil, errors.New("scheduler owner id is required")
	}
	if cfg.Now == nil {
		return nil, errors.New("scheduler clock is required")
	}
	if cfg.NewToken == nil {
		return nil, errors.New("scheduler execution token generator is required")
	}
	maxParallel := cfg.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}
	commitTimeout := cfg.CommitTimeout
	if commitTimeout <= 0 {
		commitTimeout = defaultCommitTimeout
	}
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTTL
	}
	heartbeatInterval := cfg.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = leaseTTL / 3
		if heartbeatInterval <= 0 {
			heartbeatInterval = time.Nanosecond
		}
	}
	guard := cfg.Guard
	if isNilGuard(guard) {
		guard = nil
	}
	return &Scheduler{
		store: cfg.Store, vocabulary: cfg.Vocabulary, guard: guard, maxParallel: maxParallel,
		jobBudget: cfg.JobBudget, commitTimeout: commitTimeout, newID: cfg.NewID,
		ownerID: strings.TrimSpace(cfg.OwnerID), leaseTTL: leaseTTL,
		heartbeatInterval: heartbeatInterval, now: cfg.Now, newToken: cfg.NewToken,
		gates: make(map[string]*runGate),
	}, nil
}

func isNilGuard(guard vocabulary.Guard) bool {
	if guard == nil {
		return true
	}
	value := reflect.ValueOf(guard)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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

		claimedRun, claims, claimErr := s.claimReady(ctx, run)
		if claimErr != nil {
			return claimedRun, claimErr
		}
		if len(claims) == 0 {
			if hasActiveExecutionClaim(claimedRun) {
				return claimedRun, nil
			}
			return s.finalize(ctx, claimedRun)
		}

		outcomes, heartbeatErr := s.executeClaims(ctx, claimedRun, claims)
		executionErr := ctx.Err()
		persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout)
		if heartbeatErr != nil {
			released, releaseErr := s.releaseClaims(persistCtx, claimedRun.ID, claims)
			cancelPersist()
			if executionErr == nil {
				executionErr = ctx.Err()
			}
			if released.ID == "" {
				released = claimedRun
			}
			return released, errors.Join(heartbeatErr, executionErr, releaseErr)
		}
		merged, mergeErr := s.mergeOutcomes(persistCtx, claimedRun, outcomes)
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
	claim      executionClaim
	invoked    bool
	result     vocabulary.Result
	toolErr    error
	resolveErr error
}

type inputResolutionError struct{ error }

var errNoOutcomeApplied = errors.New("no scheduler outcome applied")

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

func (s *Scheduler) executeClaims(ctx context.Context, run PlanRun, claims []executionClaim) ([]nodeOutcome, error) {
	waveCtx, cancelWave := context.WithCancel(ctx)
	defer cancelWave()
	outcomes := make([]nodeOutcome, len(claims))
	for index, claim := range claims {
		step, _ := findPlanStep(run.Plan.Steps, claim.StepID)
		outcomes[index] = nodeOutcome{step: step, claim: claim}
	}
	type task struct {
		index int
	}
	tasks := make(chan task, len(claims))
	for index := range claims {
		tasks <- task{index: index}
	}
	close(tasks)
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go s.heartbeatClaims(run.ID, claims, stopHeartbeat, cancelWave, heartbeatDone)
	workerCount := min(s.maxParallel, len(claims))
	var wait sync.WaitGroup
	for range workerCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				if waveCtx.Err() != nil {
					return
				}
				select {
				case <-waveCtx.Done():
					return
				case next, ok := <-tasks:
					if !ok {
						return
					}
					if waveCtx.Err() != nil {
						return
					}
					executeOutcome(waveCtx, s.vocabulary, s.guard, run, &outcomes[next.index])
				}
			}
		}()
	}
	wait.Wait()
	close(stopHeartbeat)
	return outcomes, <-heartbeatDone
}

func (s *Scheduler) heartbeatClaims(runID string, claims []executionClaim, stop <-chan struct{}, cancelWave context.CancelFunc, done chan<- error) {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), s.commitTimeout)
			err := s.renewClaims(ctx, runID, claims)
			cancel()
			if err != nil {
				cancelWave()
				done <- err
				return
			}
		}
	}
}

func executeOutcome(ctx context.Context, registry *vocabulary.Registry, guard vocabulary.Guard, run PlanRun, outcome *nodeOutcome) {
	outcome.invoked = true
	inputs, err := resolveInputs(outcome.step.Params, run.Nodes)
	if err != nil {
		outcome.resolveErr = err
		return
	}
	if ctx.Err() != nil {
		outcome.invoked = false
		return
	}
	tool, ok := registry.Get(outcome.step.Tool)
	if !ok {
		outcome.resolveErr = fmt.Errorf("tool %q is no longer registered", outcome.step.Tool)
		return
	}
	call := vocabulary.Call{
		SessionID: run.SessionID, UserID: run.UserID, PlanRunID: run.ID,
		NodeID: outcome.step.ID, Attempt: outcome.claim.Attempt,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, outcome.step.ID, outcome.claim.Attempt),
		Inputs:         inputs,
	}
	if guard != nil && tool.Descriptor().Category == "media" {
		outcome.result = normalizeToolResult(guard.Check(ctx, call))
		if outcome.result.Fail != nil || outcome.result.Suspension != nil {
			return
		}
	}
	if ctx.Err() != nil {
		outcome.invoked = false
		return
	}
	outcome.result, outcome.toolErr = tool.Run(ctx, call)
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
	current := run
	for range maxCASRetries {
		var appliedToolErr error
		var appliedResolveErr error
		merged, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			applied := 0
			for _, outcome := range outcomes {
				node := next.Nodes[outcome.step.ID]
				if node == nil {
					return fmt.Errorf("node %q is missing", outcome.step.ID)
				}
				if !claimMatches(node, outcome.claim) {
					continue
				}
				if !outcome.invoked || outcome.toolErr != nil {
					node.Status = NodeStatusPending
					clearExecutionClaim(node)
					if outcome.invoked && appliedToolErr == nil {
						appliedToolErr = outcome.toolErr
					}
					applied++
					continue
				}
				applyOutcome(node, outcome)
				clearExecutionClaim(node)
				if outcome.resolveErr != nil && appliedResolveErr == nil {
					appliedResolveErr = outcome.resolveErr
				}
				if node.Suspension == nil && outcome.step.Evaluate && node.Status == NodeStatusSucceeded {
					node.Suspension = &vocabulary.Suspension{Reason: SuspendWaitingAgent}
				}
				if node.Suspension != nil {
					node.ResumeKey = fmt.Sprintf("%s:%s:%d:resume", next.ID, outcome.step.ID, outcome.claim.Attempt)
					node.Resumed = false
					node.ResumeDecision = nil
				}
				applied++
			}
			if applied == 0 {
				return errNoOutcomeApplied
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
			if appliedResolveErr != nil {
				return merged, &inputResolutionError{error: appliedResolveErr}
			}
			return merged, appliedToolErr
		}
		if errors.Is(err, errNoOutcomeApplied) {
			current, err = s.store.GetRun(ctx, current.ID)
			if err != nil {
				return PlanRun{}, err
			}
			if isTerminalRun(current.Status) || current.Status == RunStatusSuspended {
				return current, nil
			}
			return current, nil
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			released, releaseErr := s.releaseClaims(ctx, current.ID, outcomeClaims(outcomes))
			if released.ID == "" {
				released = current
			}
			return released, errors.Join(err, releaseErr)
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, err
		}
		if isTerminalRun(current.Status) || current.Status == RunStatusSuspended {
			return current, nil
		}
	}
	return current, fmt.Errorf("%w: merge outcomes exceeded retry limit", ErrRunVersionConflict)
}

func outcomeClaims(outcomes []nodeOutcome) []executionClaim {
	claims := make([]executionClaim, len(outcomes))
	for index := range outcomes {
		claims[index] = outcomes[index].claim
	}
	return claims
}

func applyOutcome(node *NodeRun, outcome nodeOutcome) {
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
		if node == nil {
			return RunStatusFailed
		}
		if step.Required {
			switch {
			case node.Status == NodeStatusSucceeded:
				continue
			case node.Status == NodeStatusSkipped && node.SkipReason == SkipReasonRevision:
				partial = true
				continue
			default:
				return RunStatusFailed
			}
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
