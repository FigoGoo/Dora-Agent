package orchestration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type recordingBatchCanceller struct {
	mu      sync.Mutex
	batches []string
	err     error
}

type switchBatchOnConflictStore struct {
	RunStore
	switched atomic.Bool
}

func (s *switchBatchOnConflictStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.switched.CompareAndSwap(false, true) {
		if _, err := s.RunStore.MutateRun(ctx, id, expectedVersion, func(run *PlanRun) error {
			run.Nodes[run.SuspendedNodeID].Suspension.Payload["batch_id"] = "batch-2"
			return nil
		}); err != nil {
			return PlanRun{}, err
		}
		return PlanRun{}, ErrRunVersionConflict
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (c *recordingBatchCanceller) CancelBatch(_ context.Context, batchID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, batchID)
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
	lease := time.Now().Add(10 * time.Second)
	created, err := store.CreateRun(context.Background(), PlanRun{
		ID: "cancel-running", SessionID: "s1", UserID: "u1", Status: RunStatusRunning,
		Plan:  ExecutionPlan{PlanID: "cancel", Source: "dynamic", Summary: "cancel", Direction: "image", Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}}},
		Nodes: map[string]*NodeRun{"work": {StepID: "work", Status: NodeStatusRunning, Attempt: 1, ExecutionEpoch: 3, ExecutionOwner: "old", ExecutionToken: "old:token", LeaseUntil: &lease}},
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
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": "batch-1"}}}, nil
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
	if !errors.Is(err, cancelFailure) || got.Version != suspended.Version || got.Status != RunStatusSuspended {
		t.Fatalf("failed intent run=%+v err=%v", got, err)
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
	if !errors.Is(err, ErrJobsWaitMismatch) || late.Version != version || late.Status != RunStatusCancelled {
		t.Fatalf("late=%+v err=%v", late, err)
	}
}

func TestCancelWaitingJobsRetriesCASWithoutRepeatingExternalIntent(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": "batch-1"}}}, nil
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

func TestCancelWaitingJobsCancelsNewBatchAfterConcurrentProgress(t *testing.T) {
	jobs := schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": "batch-1"}}}, nil
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
	if err != nil || cancelled.Status != RunStatusCancelled || len(calls) != 2 || calls[0] != "batch-1" || calls[1] != "batch-2" {
		t.Fatalf("run=%+v calls=%v err=%v", cancelled, calls, err)
	}
}

func confirmationPlan() ExecutionPlan {
	return ExecutionPlan{PlanID: "confirm", Source: "dynamic", Summary: "confirm", Direction: "image", Steps: []PlanStep{
		{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "continue?"}, Required: true},
		{ID: "dispatch", Tool: "dispatch", DependsOn: []string{"confirm"}, Required: true},
	}}
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
