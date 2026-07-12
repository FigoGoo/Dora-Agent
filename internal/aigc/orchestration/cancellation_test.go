package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type recordingBatchCanceller struct {
	mu      sync.Mutex
	batches []string
	err     error
}

type blockingBatchCanceller struct {
	started chan string
	release chan struct{}
	calls   atomic.Int32
}

func (c *blockingBatchCanceller) CancelBatch(ctx context.Context, request BatchCancelRequest) error {
	c.calls.Add(1)
	select {
	case c.started <- request.BatchID:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
		return nil
	}
}

type switchBatchOnConflictStore struct {
	RunStore
	switched atomic.Bool
}

type cancellationAckLossStore struct {
	RunStore
	mutations atomic.Int32
	err       error
}

type blockingCreateWorkflowStore struct {
	generation.WorkflowStore
	created chan generation.WorkflowAggregate
	release chan struct{}
}

func (s *blockingCreateWorkflowStore) CreateWorkflow(ctx context.Context, command generation.CreateWorkflowCommand) (generation.WorkflowAggregate, bool, error) {
	aggregate, created, err := s.WorkflowStore.CreateWorkflow(ctx, command)
	if err != nil {
		return aggregate, created, err
	}
	s.created <- aggregate
	select {
	case <-ctx.Done():
		return generation.WorkflowAggregate{}, false, ctx.Err()
	case <-s.release:
		return aggregate, created, nil
	}
}

func (s *cancellationAckLossStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.mutations.Add(1) == 2 {
		if _, err := s.RunStore.MutateRun(ctx, id, expectedVersion, mutate); err != nil {
			return PlanRun{}, err
		}
		return PlanRun{}, s.err
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (s *switchBatchOnConflictStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.switched.CompareAndSwap(false, true) {
		if _, err := s.RunStore.MutateRun(ctx, id, expectedVersion, func(run *PlanRun) error {
			run.Nodes[run.SuspendedNodeID].Suspension.Payload["batch_id"] = "batch-2"
			run.Nodes[run.SuspendedNodeID].Outputs["batch_id"] = "batch-2"
			return nil
		}); err != nil {
			return PlanRun{}, err
		}
		return PlanRun{}, ErrRunVersionConflict
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (c *recordingBatchCanceller) CancelBatch(_ context.Context, request BatchCancelRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, request.BatchID)
	return c.err
}

func (c *recordingBatchCanceller) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.batches...)
}

func TestPreviewDenialCancelsWithoutExecutingAndReplaysExactly(t *testing.T) {
	var calls atomic.Int32
	work := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	cfg := schedulerConfigForTest(NewMemoryRunStore(), schedulerRegistry(t, work), func() string { return "preview-denial" })
	cfg.JobBudget = 1
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "preview", Source: "dynamic", Summary: "preview", Direction: "image", EstimatedJobs: 2,
		Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}},
	})
	if err != nil || suspended.Status != RunStatusSuspended {
		t.Fatalf("run=%+v err=%v", suspended, err)
	}
	denied, err := scheduler.Resume(context.Background(), suspended.ID, suspended.ResumeKey, map[string]any{"approved": false})
	if err != nil || denied.Status != RunStatusCancelled || denied.CancelReason != CancelReasonPreviewRejected || calls.Load() != 0 {
		t.Fatalf("run=%+v calls=%d err=%v", denied, calls.Load(), err)
	}
	assertCancelledNodes(t, denied)
	version := denied.Version
	replayed, err := scheduler.Resume(context.Background(), denied.ID, suspended.ResumeKey, map[string]any{"approved": false})
	if err != nil || replayed.Version != version || calls.Load() != 0 {
		t.Fatalf("replay=%+v calls=%d err=%v", replayed, calls.Load(), err)
	}
	conflicted, err := scheduler.Resume(context.Background(), denied.ID, suspended.ResumeKey, map[string]any{"approved": true})
	if !errors.Is(err, ErrResumeDecisionConflict) || conflicted.Version != version || calls.Load() != 0 {
		t.Fatalf("conflict=%+v calls=%d err=%v", conflicted, calls.Load(), err)
	}
}

func TestRequestConfirmationDenialCancelsAndValidatesStrictDecision(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision map[string]any
	}{
		{name: "missing", decision: map[string]any{}},
		{name: "non_bool", decision: map[string]any{"approved": "no"}},
		{name: "extra", decision: map[string]any{"approved": false, "note": "x"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var downstream atomic.Int32
			scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t,
				vocabulary.NewRequestConfirmationTool(),
				schedulerTool{key: "dispatch", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
					downstream.Add(1)
					return vocabulary.Result{}, nil
				}},
			), 1)
			suspended, err := scheduler.Submit(context.Background(), "s1", "u1", confirmationPlan())
			if err != nil {
				t.Fatal(err)
			}
			got, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["confirm"].ResumeKey, test.decision)
			if !errors.Is(err, ErrConfirmationDecisionInvalid) || got.Version != suspended.Version || got.Status != RunStatusSuspended || downstream.Load() != 0 {
				t.Fatalf("run=%+v downstream=%d err=%v", got, downstream.Load(), err)
			}
		})
	}

	var downstream atomic.Int32
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t,
		vocabulary.NewRequestConfirmationTool(),
		schedulerTool{key: "dispatch", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstream.Add(1)
			return vocabulary.Result{}, nil
		}},
	), 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", confirmationPlan())
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["confirm"].ResumeKey
	denied, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": false})
	if err != nil || denied.Status != RunStatusCancelled || downstream.Load() != 0 || !denied.Nodes["confirm"].Resumed {
		t.Fatalf("run=%+v downstream=%d err=%v", denied, downstream.Load(), err)
	}
	version := denied.Version
	replayed, err := scheduler.Resume(context.Background(), denied.ID, key, map[string]any{"approved": false})
	if err != nil || replayed.Version != version {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	conflicted, err := scheduler.Resume(context.Background(), denied.ID, key, map[string]any{"approved": true})
	if !errors.Is(err, ErrResumeDecisionConflict) || conflicted.Version != version {
		t.Fatalf("conflict=%+v err=%v", conflicted, err)
	}
}

func TestRequestConfirmationApprovalStillAdvances(t *testing.T) {
	var downstream atomic.Int32
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t,
		vocabulary.NewRequestConfirmationTool(),
		schedulerTool{key: "dispatch", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstream.Add(1)
			return vocabulary.Result{}, nil
		}},
	), 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", confirmationPlan())
	if err != nil {
		t.Fatal(err)
	}
	completed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["confirm"].ResumeKey, map[string]any{"approved": true})
	if err != nil || completed.Status != RunStatusSucceeded || downstream.Load() != 1 {
		t.Fatalf("run=%+v downstream=%d err=%v", completed, downstream.Load(), err)
	}
}

func TestCustomWaitingUserDecisionRemainsOpenSchema(t *testing.T) {
	custom := schedulerTool{key: "custom", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{
			Reason: SuspendWaitingUser, Origin: "request_confirmation", DecisionSchema: "approved_bool_v1",
		}}, nil
	}}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, custom), 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "custom", Source: "dynamic", Summary: "custom", Direction: "image",
		Steps: []PlanStep{{ID: "custom", Tool: "custom", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Nodes["custom"].SuspensionOrigin != "custom" {
		t.Fatalf("origin=%q", suspended.Nodes["custom"].SuspensionOrigin)
	}
	completed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["custom"].ResumeKey, map[string]any{"choice": "keep"})
	if err != nil || completed.Status != RunStatusSucceeded {
		t.Fatalf("run=%+v err=%v", completed, err)
	}
}

func TestCancelIsIdempotentFencesClaimsAndFreezesReason(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, schedulerTool{key: "work"}), 1)
	created, err := store.CreateRun(context.Background(), PlanRun{
		ID: "cancel-running", SessionID: "s1", UserID: "u1", Status: RunStatusRunning,
		Plan:  ExecutionPlan{PlanID: "cancel", Source: "dynamic", Summary: "cancel", Direction: "image", Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}}},
		Nodes: map[string]*NodeRun{"work": {StepID: "work", Status: NodeStatusPending, Attempt: 1, ExecutionEpoch: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := scheduler.Cancel(context.Background(), created.ID, "user requested stop")
	if err != nil || cancelled.Status != RunStatusCancelled || cancelled.CancelReason != "user requested stop" {
		t.Fatalf("run=%+v err=%v", cancelled, err)
	}
	assertCancelledNodes(t, cancelled)
	version := cancelled.Version
	replayed, err := scheduler.Cancel(context.Background(), created.ID, "user requested stop")
	if err != nil || replayed.Version != version {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	conflicted, err := scheduler.Cancel(context.Background(), created.ID, "different")
	if !errors.Is(err, ErrRunCancellationConflict) || conflicted.Version != version {
		t.Fatalf("conflict=%+v err=%v", conflicted, err)
	}
	if err := scheduler.renewClaims(context.Background(), created.ID, []executionClaim{{StepID: "work", Attempt: 1, Epoch: 3, Owner: "old", Token: "old:token"}}); !errors.Is(err, ErrExecutionClaimLost) {
		t.Fatalf("renew stale claim err=%v", err)
	}
	late, err := scheduler.mergeOutcomes(context.Background(), created, []nodeOutcome{{
		step:  PlanStep{ID: "work", Tool: "work", Required: true},
		claim: executionClaim{StepID: "work", Attempt: 1, Epoch: 3, Owner: "old", Token: "old:token"}, invoked: true,
		result: vocabulary.Result{Outputs: map[string]any{"late": true}},
	}})
	if err != nil || late.Status != RunStatusCancelled || late.Version != version {
		t.Fatalf("late=%+v err=%v", late, err)
	}
}

func TestCancelRejectsOrdinaryActiveExecutionClaimWithoutMutation(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	work := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		started <- struct{}{}
		<-release
		return vocabulary.Result{}, nil
	}}
	store := NewMemoryRunStore()
	registry := schedulerRegistry(t, work)
	running := schedulerForTest(t, store, registry, 1)
	cancelling := schedulerForTest(t, store, registry, 1)
	done := make(chan PlanRun, 1)
	go func() {
		run, _ := running.Submit(context.Background(), "s", "u", ExecutionPlan{
			PlanID: "busy", Source: "dynamic", Summary: "busy", Direction: "image", Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}},
		})
		done <- run
	}()
	<-started
	claimed, err := store.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := cancelling.Cancel(context.Background(), claimed.ID, "stop")
	if !errors.Is(err, ErrRunCancellationBusy) || got.Version != claimed.Version || got.CancelRequested || got.Nodes["work"].ExecutionToken != claimed.Nodes["work"].ExecutionToken {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	close(release)
	if completed := <-done; completed.Status != RunStatusSucceeded {
		t.Fatalf("completed=%+v", completed)
	}
}

func TestCancelWaitsForRealGenerationDispatchReceiptBeforeOwningBatch(t *testing.T) {
	generationStore := &blockingCreateWorkflowStore{
		WorkflowStore: generation.NewMemoryStore(), created: make(chan generation.WorkflowAggregate, 1), release: make(chan struct{}),
	}
	var ids atomic.Int32
	commands := generation.NewCommandService(generation.CommandServiceConfig{Store: generationStore, NewID: func() string {
		return fmt.Sprintf("generation-%d", ids.Add(1))
	}})
	bridge := NewGenerationBridge(commands)
	dispatch := vocabulary.NewDispatchGenerationTool(bridge)
	runStore := NewMemoryRunStore()
	registry := schedulerRegistry(t, dispatch)
	runCfg := schedulerConfigForTest(runStore, registry, func() string { return "real-dispatch-run" })
	running, err := NewScheduler(runCfg)
	if err != nil {
		t.Fatal(err)
	}
	cancelCfg := schedulerConfigForTest(runStore, registry, func() string { return "unused" })
	cancelCfg.BatchCanceller = bridge
	cancelling, err := NewScheduler(cancelCfg)
	if err != nil {
		t.Fatal(err)
	}
	type submitResult struct {
		run PlanRun
		err error
	}
	done := make(chan submitResult, 1)
	go func() {
		run, submitErr := running.Submit(context.Background(), "session-1", "user-1", ExecutionPlan{
			PlanID: "dispatch", Source: "dynamic", Summary: "dispatch", Direction: "image",
			Steps: []PlanStep{{ID: "dispatch", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "rain"}}}, Required: true}},
		})
		done <- submitResult{run: run, err: submitErr}
	}()
	aggregate := <-generationStore.created
	claimed, err := runStore.GetRun(context.Background(), "real-dispatch-run")
	if err != nil {
		t.Fatal(err)
	}
	busy, err := cancelling.Cancel(context.Background(), claimed.ID, "stop")
	if !errors.Is(err, ErrRunCancellationBusy) || busy.Version != claimed.Version || busy.CancelRequested {
		t.Fatalf("busy=%+v err=%v", busy, err)
	}
	batch, err := generationStore.GetBatch(context.Background(), aggregate.Batch.ID)
	if err != nil || batch.CancelRequested {
		t.Fatalf("escaped batch before receipt=%+v err=%v", batch, err)
	}
	close(generationStore.release)
	submitted := <-done
	if submitted.err != nil || submitted.run.Status != RunStatusSuspended || submitted.run.Nodes["dispatch"].Outputs["batch_id"] != aggregate.Batch.ID {
		t.Fatalf("submitted=%+v err=%v", submitted.run, submitted.err)
	}
	cancelled, err := cancelling.Cancel(context.Background(), submitted.run.ID, "stop")
	if err != nil || cancelled.Status != RunStatusCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	batch, err = generationStore.GetBatch(context.Background(), aggregate.Batch.ID)
	if err != nil || !batch.CancelRequested {
		t.Fatalf("owned batch=%+v err=%v", batch, err)
	}
}

func TestCancelRefusesCompletedTerminalRuns(t *testing.T) {
	for _, status := range []string{RunStatusSucceeded, RunStatusPartialSucceeded, RunStatusFailed} {
		t.Run(status, func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, schedulerRegistry(t, schedulerTool{key: "work"}), 1)
			created, err := store.CreateRun(context.Background(), PlanRun{ID: "terminal-" + status, SessionID: "s", UserID: "u", Status: status, Plan: ExecutionPlan{PlanID: "p", Source: "dynamic", Summary: "p", Direction: "image"}, Nodes: map[string]*NodeRun{}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := scheduler.Cancel(context.Background(), created.ID, "stop")
			if !errors.Is(err, ErrRunCancellationTerminal) || got.Version != created.Version || got.Status != status {
				t.Fatalf("run=%+v err=%v", got, err)
			}
		})
	}
}

func TestCancelSupportsEveryNonTerminalStatus(t *testing.T) {
	for _, status := range []string{RunStatusDraft, RunStatusRunning, RunStatusSuspended} {
		t.Run(status, func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, schedulerRegistry(t, schedulerTool{key: "work"}), 1)
			node := &NodeRun{StepID: "work", Status: NodeStatusPending}
			run := PlanRun{
				ID: "nonterminal-" + status, SessionID: "s", UserID: "u", Status: status,
				Plan:  ExecutionPlan{PlanID: "p", Source: "dynamic", Summary: "p", Direction: "image", Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}}},
				Nodes: map[string]*NodeRun{"work": node},
			}
			if status == RunStatusSuspended {
				run.SuspendReason = SuspendWaitingUser
				run.SuspendedNodeID = "work"
				node.Status = NodeStatusRunning
				node.Suspension = &vocabulary.Suspension{Reason: SuspendWaitingUser}
			}
			created, err := store.CreateRun(context.Background(), run)
			if err != nil {
				t.Fatal(err)
			}
			cancelled, err := scheduler.Cancel(context.Background(), created.ID, "stop")
			if err != nil || cancelled.Status != RunStatusCancelled {
				t.Fatalf("run=%+v err=%v", cancelled, err)
			}
			assertCancelledNodes(t, cancelled)
		})
	}
}

func TestConcurrentCancelSameReasonConvergesToOneVersion(t *testing.T) {
	store := NewMemoryRunStore()
	registry := schedulerRegistry(t, schedulerTool{key: "work"})
	first := schedulerForTest(t, store, registry, 1)
	second := schedulerForTest(t, store, registry, 1)
	created, err := store.CreateRun(context.Background(), PlanRun{
		ID: "concurrent-cancel", SessionID: "s", UserID: "u", Status: RunStatusRunning,
		Plan:  ExecutionPlan{PlanID: "p", Source: "dynamic", Summary: "p", Direction: "image", Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}}},
		Nodes: map[string]*NodeRun{"work": {StepID: "work", Status: NodeStatusPending}},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan PlanRun, 2)
	errs := make(chan error, 2)
	for _, scheduler := range []*Scheduler{first, second} {
		go func(s *Scheduler) {
			<-start
			run, cancelErr := s.Cancel(context.Background(), created.ID, "same reason")
			results <- run
			errs <- cancelErr
		}(scheduler)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if run := <-results; run.Status != RunStatusCancelled || run.Version != created.Version+1 {
			t.Fatalf("run=%+v", run)
		}
	}
}

func TestCancelWaitingJobsFailsClosedAndCancelsBatchBeforeRun(t *testing.T) {
	newSuspended := func(t *testing.T, canceller BatchCanceller) (*Scheduler, PlanRun) {
		t.Helper()
		jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return waitingJobsResult("batch-1"), nil
		}}
		cfg := schedulerConfigForTest(NewMemoryRunStore(), schedulerRegistry(t, jobs), func() string { return "jobs-cancel" })
		cfg.BatchCanceller = canceller
		scheduler, err := NewScheduler(cfg)
		if err != nil {
			t.Fatal(err)
		}
		run, err := scheduler.Submit(context.Background(), "s", "u", ExecutionPlan{PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}}})
		if err != nil {
			t.Fatal(err)
		}
		return scheduler, run
	}

	without, suspended := newSuspended(t, nil)
	got, err := without.Cancel(context.Background(), suspended.ID, "stop")
	if !errors.Is(err, ErrBatchCancellationUnavailable) || got.Version != suspended.Version || got.Status != RunStatusSuspended {
		t.Fatalf("run=%+v err=%v", got, err)
	}

	cancelFailure := errors.New("generation store unavailable")
	failingCanceller := &recordingBatchCanceller{err: cancelFailure}
	failing, suspended := newSuspended(t, failingCanceller)
	got, err = failing.Cancel(context.Background(), suspended.ID, "stop")
	if !errors.Is(err, cancelFailure) || got.Version != suspended.Version+1 || got.Status != RunStatusSuspended || !got.CancelRequested || got.CancelBatchID != "batch-1" || got.CancelReason != "stop" {
		t.Fatalf("failed intent run=%+v err=%v", got, err)
	}
	intentVersion := got.Version
	unconfigured := schedulerForTest(t, failing.store, failing.vocabulary, 1)
	blockedCancel, err := unconfigured.Cancel(context.Background(), suspended.ID, "stop")
	if !errors.Is(err, ErrBatchCancellationUnavailable) || blockedCancel.Version != intentVersion {
		t.Fatalf("unconfigured retry=%+v err=%v", blockedCancel, err)
	}
	blockedResume, err := failing.Resume(context.Background(), suspended.ID, suspended.Nodes["jobs"].ResumeKey, map[string]any{"approved": true})
	if !errors.Is(err, ErrCancellationPending) || blockedResume.Version != intentVersion {
		t.Fatalf("resume=%+v err=%v", blockedResume, err)
	}
	blockedAdvance, err := failing.Advance(context.Background(), suspended.ID)
	if !errors.Is(err, cancelFailure) || blockedAdvance.Version != intentVersion {
		t.Fatalf("advance=%+v err=%v", blockedAdvance, err)
	}
	conflicted, err := failing.Cancel(context.Background(), suspended.ID, "different")
	if !errors.Is(err, ErrRunCancellationConflict) || conflicted.Version != intentVersion {
		t.Fatalf("conflict=%+v err=%v", conflicted, err)
	}
	failingCanceller.err = nil
	retried, err := failing.Cancel(context.Background(), suspended.ID, "stop")
	if err != nil || retried.Status != RunStatusCancelled || len(failingCanceller.calls()) != 3 {
		t.Fatalf("retry=%+v calls=%v err=%v", retried, failingCanceller.calls(), err)
	}

	canceller := &recordingBatchCanceller{}
	with, suspended := newSuspended(t, canceller)
	cancelled, err := with.Cancel(context.Background(), suspended.ID, "stop")
	if err != nil || cancelled.Status != RunStatusCancelled || len(canceller.calls()) != 1 || canceller.calls()[0] != "batch-1" {
		t.Fatalf("run=%+v calls=%v err=%v", cancelled, canceller.calls(), err)
	}
	version := cancelled.Version
	replayed, err := with.Cancel(context.Background(), suspended.ID, "stop")
	if err != nil || replayed.Version != version || len(canceller.calls()) != 1 {
		t.Fatalf("replay=%+v calls=%v err=%v", replayed, canceller.calls(), err)
	}
	late, err := with.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: "completed"})
	if err != nil || late.Version != version || late.Status != RunStatusCancelled {
		t.Fatalf("late=%+v err=%v", late, err)
	}
}

func TestCancelRejectsSuspensionWithoutMatchingNodeReceipt(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		result := waitingJobsResult("batch-1")
		result.Outputs["batch_id"] = "other-batch"
		return result, nil
	}}
	canceller := &recordingBatchCanceller{}
	cfg := schedulerConfigForTest(NewMemoryRunStore(), schedulerRegistry(t, jobs), func() string { return "receipt-mismatch" })
	cfg.BatchCanceller = canceller
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := scheduler.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scheduler.Cancel(context.Background(), suspended.ID, "stop")
	if !errors.Is(err, ErrBatchCancellationInvalid) || got.Version != suspended.Version || got.CancelRequested || len(canceller.calls()) != 0 {
		t.Fatalf("run=%+v calls=%v err=%v", got, canceller.calls(), err)
	}
}

func TestCancelIntentWinsCompleteJobsWaitAcrossSchedulers(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return waitingJobsResult("batch-1"), nil
	}}
	store := NewMemoryRunStore()
	registry := schedulerRegistry(t, jobs)
	base := schedulerForTest(t, store, registry, 1)
	suspended, err := base.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	canceller := &blockingBatchCanceller{started: make(chan string, 1), release: make(chan struct{})}
	cancelCfg := schedulerConfigForTest(store, registry, func() string { return "unused-cancel" })
	cancelCfg.BatchCanceller = canceller
	cancelling, err := NewScheduler(cancelCfg)
	if err != nil {
		t.Fatal(err)
	}
	completing := schedulerForTest(t, store, registry, 1)
	type cancelResult struct {
		run PlanRun
		err error
	}
	done := make(chan cancelResult, 1)
	go func() {
		run, cancelErr := cancelling.Cancel(context.Background(), suspended.ID, "user stop")
		done <- cancelResult{run: run, err: cancelErr}
	}()
	if batchID := <-canceller.started; batchID != "batch-1" {
		t.Fatalf("batch=%q", batchID)
	}
	intent, err := store.GetRun(context.Background(), suspended.ID)
	if err != nil || !intent.CancelRequested || intent.CancelReason != "user stop" || intent.CancelBatchID != "batch-1" || intent.Status != RunStatusSuspended {
		t.Fatalf("intent=%+v err=%v", intent, err)
	}
	revised, err := completing.Revise(context.Background(), suspended.ID, PlanRevision{SkipStepIDs: []string{"jobs"}})
	if !errors.Is(err, ErrCancellationPending) || revised.Version != intent.Version {
		t.Fatalf("revised=%+v err=%v", revised, err)
	}
	completed, err := completing.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: "cancelled"})
	if err != nil || completed.Status != RunStatusCancelled || completed.CancelReason != "user stop" {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	version := completed.Version
	replayed, err := completing.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: "cancelled"})
	if err != nil || replayed.Version != version || replayed.Status != RunStatusCancelled {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	close(canceller.release)
	result := <-done
	if result.err != nil || result.run.Status != RunStatusCancelled || result.run.Version != version || result.run.CancelReason != "user stop" {
		t.Fatalf("cancel=%+v err=%v", result.run, result.err)
	}
}

func TestAdvanceRecoversPersistedJobsCancellationIntent(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return waitingJobsResult("batch-1"), nil
	}}
	store := NewMemoryRunStore()
	registry := schedulerRegistry(t, jobs)
	failingCanceller := &recordingBatchCanceller{err: context.Canceled}
	firstCfg := schedulerConfigForTest(store, registry, func() string { return "recover-intent" })
	firstCfg.BatchCanceller = failingCanceller
	first, err := NewScheduler(firstCfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := first.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := first.Cancel(context.Background(), suspended.ID, "stop")
	if err == nil || !intent.CancelRequested || intent.Status != RunStatusSuspended {
		t.Fatalf("intent=%+v err=%v", intent, err)
	}

	withoutDependency := schedulerForTest(t, store, registry, 1)
	blocked, err := withoutDependency.Advance(context.Background(), suspended.ID)
	if !errors.Is(err, ErrCancellationDependency) || blocked.Version != intent.Version {
		t.Fatalf("blocked=%+v err=%v", blocked, err)
	}

	recoveryCanceller := &recordingBatchCanceller{}
	recoveryCfg := schedulerConfigForTest(store, registry, func() string { return "unused" })
	recoveryCfg.BatchCanceller = recoveryCanceller
	recovery, err := NewScheduler(recoveryCfg)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := recovery.Advance(context.Background(), suspended.ID)
	if err != nil || cancelled.Status != RunStatusCancelled || len(recoveryCanceller.calls()) != 1 || recoveryCanceller.calls()[0] != "batch-1" {
		t.Fatalf("cancelled=%+v calls=%v err=%v", cancelled, recoveryCanceller.calls(), err)
	}
}

func TestCancelIntentDominatesEveryTerminalJobsOutcome(t *testing.T) {
	for _, status := range []string{generation.BatchStatusCompleted, generation.BatchStatusFailed, generation.BatchStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
				return waitingJobsResult("batch-1"), nil
			}}
			store := NewMemoryRunStore()
			registry := schedulerRegistry(t, jobs)
			canceller := &recordingBatchCanceller{err: errors.New("provider unavailable")}
			cancelCfg := schedulerConfigForTest(store, registry, func() string { return "intent-" + status })
			cancelCfg.BatchCanceller = canceller
			cancelling, err := NewScheduler(cancelCfg)
			if err != nil {
				t.Fatal(err)
			}
			suspended, err := cancelling.Submit(context.Background(), "s", "u", ExecutionPlan{
				PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			intent, err := cancelling.Cancel(context.Background(), suspended.ID, "user stop")
			if err == nil || !intent.CancelRequested || intent.Status != RunStatusSuspended {
				t.Fatalf("intent=%+v err=%v", intent, err)
			}
			completing := schedulerForTest(t, store, registry, 1)
			completed, err := completing.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: status})
			if err != nil || completed.Status != RunStatusCancelled || completed.CancelReason != "user stop" {
				t.Fatalf("completed=%+v err=%v", completed, err)
			}
			version := completed.Version
			replayed, err := completing.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: status})
			if err != nil || replayed.Version != version {
				t.Fatalf("replayed=%+v err=%v", replayed, err)
			}
			otherStatus := generation.BatchStatusFailed
			if status == otherStatus {
				otherStatus = generation.BatchStatusCompleted
			}
			conflicted, err := completing.CompleteJobsWait(context.Background(), suspended.ID, "jobs", JobsOutcome{BatchID: "batch-1", Status: otherStatus})
			if !errors.Is(err, ErrJobsOutcomeConflict) || conflicted.Version != version {
				t.Fatalf("conflicted=%+v err=%v", conflicted, err)
			}
		})
	}
}

func TestCancelWaitingJobsRetriesCASWithoutRepeatingExternalIntent(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return waitingJobsResult("batch-1"), nil
	}}
	base := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, jobs), 1)
	suspended, err := base.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	canceller := &recordingBatchCanceller{}
	cfg := schedulerConfigForTest(&conflictOnceStore{RunStore: base.store, conflict: 1}, base.vocabulary, func() string { return "unused" })
	cfg.BatchCanceller = canceller
	recovering, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := recovering.Cancel(context.Background(), suspended.ID, "stop")
	if err != nil || cancelled.Status != RunStatusCancelled || len(canceller.calls()) != 1 {
		t.Fatalf("run=%+v calls=%v err=%v", cancelled, canceller.calls(), err)
	}
}

func TestCancelWaitingJobsBindsIntentToAuthoritativeBatchAfterConflict(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return waitingJobsResult("batch-1"), nil
	}}
	base := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, jobs), 1)
	suspended, err := base.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	canceller := &recordingBatchCanceller{}
	cfg := schedulerConfigForTest(&switchBatchOnConflictStore{RunStore: base.store}, base.vocabulary, func() string { return "unused" })
	cfg.BatchCanceller = canceller
	recovering, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := recovering.Cancel(context.Background(), suspended.ID, "stop")
	calls := canceller.calls()
	if err != nil || cancelled.Status != RunStatusCancelled || len(calls) != 1 || calls[0] != "batch-2" || cancelled.CancelBatchID != "batch-2" {
		t.Fatalf("run=%+v calls=%v err=%v", cancelled, calls, err)
	}
}

func TestCancelRetryConvergesAfterFinalizeAckLoss(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return waitingJobsResult("batch-1"), nil
	}}
	baseStore := NewMemoryRunStore()
	registry := schedulerRegistry(t, jobs)
	base := schedulerForTest(t, baseStore, registry, 1)
	suspended, err := base.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &cancellationAckLossStore{RunStore: baseStore, err: errors.New("commit acknowledgement lost")}
	canceller := &recordingBatchCanceller{}
	cfg := schedulerConfigForTest(store, registry, func() string { return "ack-loss" })
	cfg.BatchCanceller = canceller
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := scheduler.Cancel(context.Background(), suspended.ID, "stop")
	if !errors.Is(err, store.err) || first.Status != RunStatusSuspended || !first.CancelRequested {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	retried, err := scheduler.Cancel(context.Background(), suspended.ID, "stop")
	if err != nil || retried.Status != RunStatusCancelled || len(canceller.calls()) != 1 {
		t.Fatalf("retried=%+v calls=%v err=%v", retried, canceller.calls(), err)
	}
	conflicted, err := scheduler.Cancel(context.Background(), suspended.ID, "different")
	if !errors.Is(err, ErrRunCancellationConflict) || conflicted.Version != retried.Version {
		t.Fatalf("conflicted=%+v err=%v", conflicted, err)
	}
}

func confirmationPlan() ExecutionPlan {
	return ExecutionPlan{PlanID: "confirm", Source: "dynamic", Summary: "confirm", Direction: "image", Steps: []PlanStep{
		{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "continue?"}, Required: true},
		{ID: "dispatch", Tool: "dispatch", DependsOn: []string{"confirm"}, Required: true},
	}}
}

func waitingJobsResult(batchID string) vocabulary.Result {
	return vocabulary.Result{
		Outputs:    map[string]any{"operation_id": "operation-1", "batch_id": batchID, "job_ids": []any{"job-1"}},
		Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": batchID}},
	}
}

func assertCancelledNodes(t *testing.T, run PlanRun) {
	t.Helper()
	for id, node := range run.Nodes {
		if node.Status == NodeStatusPending || node.Status == NodeStatusRunning || node.ExecutionOwner != "" || node.ExecutionToken != "" || node.LeaseUntil != nil || node.Suspension != nil {
			t.Fatalf("node %q not cancelled cleanly: %+v", id, node)
		}
		if node.Status == NodeStatusSkipped && node.SkipReason != SkipReasonCancelled {
			t.Fatalf("node %q skip reason=%q", id, node.SkipReason)
		}
	}
}
