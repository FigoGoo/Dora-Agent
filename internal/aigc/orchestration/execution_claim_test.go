package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type claimClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClaimClock(now time.Time) *claimClock { return &claimClock{now: now} }
func (c *claimClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *claimClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}

func claimScheduler(t *testing.T, store RunStore, owner string, clock *claimClock, heartbeat time.Duration, tool schedulerTool) *Scheduler {
	t.Helper()
	var token atomic.Int64
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, tool), MaxParallel: 1,
		CommitTimeout: time.Second, NewID: func() string { return owner + "-run" },
		OwnerID: owner, LeaseTTL: 30 * time.Second, HeartbeatInterval: heartbeat,
		Now: clock.Now, NewToken: func() string { return fmt.Sprintf("token-%d", token.Add(1)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func oneClaimStep(tool string) ExecutionPlan {
	return ExecutionPlan{PlanID: "claim", Source: "dynamic", Summary: "claim", Direction: "image", Steps: []PlanStep{{ID: "effect", Tool: tool, Required: true}}}
}

type advanceResult struct {
	run PlanRun
	err error
}

func advanceAsync(s *Scheduler, runID string) <-chan advanceResult {
	done := make(chan advanceResult, 1)
	go func() {
		run, err := s.Advance(context.Background(), runID)
		done <- advanceResult{run: run, err: err}
	}()
	return done
}

func TestExecutionClaimContractFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	node := NodeRun{
		ExecutionOwner: "owner-a",
		ExecutionToken: "owner-a:token-1",
		LeaseUntil:     &now,
	}
	cfg := SchedulerConfig{
		OwnerID:  "owner-a",
		LeaseTTL: 30 * time.Second,
		Now:      func() time.Time { return now },
		NewToken: func() string { return "token-1" },
	}
	if node.ExecutionOwner == "" || node.ExecutionToken == "" || node.LeaseUntil == nil || cfg.OwnerID == "" {
		t.Fatal("execution claim contract was not populated")
	}
}

func TestExecutionClaimPreventsConcurrentSchedulersAndRevision(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan vocabulary.Call, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		started <- call
		<-release
		return vocabulary.Result{Outputs: map[string]any{"owner": "first"}}, nil
	}}
	first := claimScheduler(t, store, "owner-a", clock, time.Hour, tool)
	second := claimScheduler(t, store, "owner-b", clock, time.Hour, tool)
	run := createPendingSchedulerRun(t, store, "claim-exclusive", oneClaimStep("work"))

	firstDone := advanceAsync(first, run.ID)
	<-started
	claimed, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := claimed.Nodes["effect"]
	if node.Status != NodeStatusRunning || node.ExecutionOwner != "owner-a" || node.ExecutionToken == "" || node.LeaseUntil == nil || node.Attempt != 1 {
		t.Fatalf("claim=%+v", node)
	}
	secondRun, err := second.Advance(context.Background(), run.ID)
	if err != nil || secondRun.Version != claimed.Version || calls.Load() != 1 || secondRun.Status != RunStatusRunning {
		t.Fatalf("second=%+v calls=%d err=%v", secondRun, calls.Load(), err)
	}
	beforeRevision := secondRun.Version
	_, err = second.Revise(context.Background(), run.ID, PlanRevision{SkipStepIDs: []string{"effect"}})
	if !errors.Is(err, ErrReviseExecutedStep) {
		t.Fatalf("revise err=%v", err)
	}
	afterRevision, _ := store.GetRun(context.Background(), run.ID)
	if afterRevision.Version != beforeRevision || afterRevision.Nodes["effect"].Status != NodeStatusRunning {
		t.Fatalf("revision mutated claim: %+v", afterRevision)
	}
	close(release)
	result := <-firstDone
	if result.err != nil || result.run.Status != RunStatusSucceeded || result.run.Nodes["effect"].ExecutionToken != "" || result.run.Nodes["effect"].LeaseUntil != nil {
		t.Fatalf("result=%+v err=%v", result.run, result.err)
	}
}

func TestExecutionClaimExpiredReclaimPreservesAttemptAndFencesLateResult(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	firstStarted := make(chan vocabulary.Call, 1)
	firstRelease := make(chan struct{})
	first := claimScheduler(t, store, "owner-a", clock, time.Hour, schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		firstStarted <- call
		<-firstRelease
		return vocabulary.Result{Outputs: map[string]any{"winner": "stale"}}, nil
	}})
	secondCalls := make(chan vocabulary.Call, 1)
	second := claimScheduler(t, store, "owner-b", clock, time.Hour, schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		secondCalls <- call
		return vocabulary.Result{Outputs: map[string]any{"winner": "fresh"}}, nil
	}})
	run := createPendingSchedulerRun(t, store, "claim-reclaim", oneClaimStep("work"))
	firstDone := advanceAsync(first, run.ID)
	firstCall := <-firstStarted
	clock.Advance(31 * time.Second)
	secondRun, err := second.Advance(context.Background(), run.ID)
	if err != nil || secondRun.Status != RunStatusSucceeded {
		t.Fatalf("second=%+v err=%v", secondRun, err)
	}
	secondCall := <-secondCalls
	if firstCall.Attempt != 1 || secondCall.Attempt != 1 || firstCall.IdempotencyKey != secondCall.IdempotencyKey {
		t.Fatalf("first=%+v second=%+v", firstCall, secondCall)
	}
	version := secondRun.Version
	close(firstRelease)
	late := <-firstDone
	if late.err != nil || late.run.Version != version || late.run.Nodes["effect"].Outputs["winner"] != "fresh" {
		t.Fatalf("late=%+v err=%v", late.run, late.err)
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != version || stored.Nodes["effect"].Outputs["winner"] != "fresh" {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestExecutionClaimHeartbeatPreventsExpiredTakeover(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return vocabulary.Result{}, nil
	}}
	first := claimScheduler(t, store, "owner-a", clock, time.Millisecond, tool)
	second := claimScheduler(t, store, "owner-b", clock, time.Hour, tool)
	run := createPendingSchedulerRun(t, store, "claim-heartbeat", oneClaimStep("work"))
	done := advanceAsync(first, run.ID)
	<-started
	initial, _ := store.GetRun(context.Background(), run.ID)
	clock.Advance(31 * time.Second)
	deadline := time.After(time.Second)
	for {
		current, _ := store.GetRun(context.Background(), run.ID)
		if current.Nodes["effect"].LeaseUntil.After(*initial.Nodes["effect"].LeaseUntil) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("heartbeat did not renew lease")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	contender, err := second.Advance(context.Background(), run.ID)
	if err != nil || contender.Status != RunStatusRunning || calls.Load() != 1 {
		t.Fatalf("contender=%+v calls=%d err=%v", contender, calls.Load(), err)
	}
	close(release)
	if result := <-done; result.err != nil || result.run.Status != RunStatusSucceeded {
		t.Fatalf("result=%+v err=%v", result.run, result.err)
	}
}

func TestExecutionClaimToolErrorAndCancellationReleaseForSameKey(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(context.Context, vocabulary.Call) (vocabulary.Result, error)
		want error
	}{
		{name: "tool_error", want: errors.New("offline")},
		{name: "cancel", want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryRunStore()
			clock := newClaimClock(time.Unix(1_700_000_000, 0))
			var callsMu sync.Mutex
			var calls []vocabulary.Call
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			toolErr := test.want
			tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
				callsMu.Lock()
				calls = append(calls, call)
				callsMu.Unlock()
				if test.name == "cancel" {
					cancel()
				}
				return vocabulary.Result{}, toolErr
			}}
			scheduler := claimScheduler(t, store, "owner", clock, time.Hour, tool)
			run := createPendingSchedulerRun(t, store, "claim-release-"+test.name, oneClaimStep("work"))
			got, err := scheduler.Advance(ctx, run.ID)
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v", err)
			}
			node := got.Nodes["effect"]
			if node.Status != NodeStatusPending || node.Attempt != 1 || node.ExecutionOwner != "" || node.ExecutionToken != "" || node.LeaseUntil != nil {
				t.Fatalf("released=%+v", node)
			}
			_, _ = scheduler.Advance(context.Background(), run.ID)
			callsMu.Lock()
			defer callsMu.Unlock()
			if len(calls) != 2 || calls[0].IdempotencyKey != calls[1].IdempotencyKey {
				t.Fatalf("calls=%+v", calls)
			}
		})
	}
}

func TestExecutionClaimSuspensionClearsClaim(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	scheduler := claimScheduler(t, store, "owner", clock, time.Hour, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
	}})
	run := createPendingSchedulerRun(t, store, "claim-suspend", oneClaimStep("work"))
	got, err := scheduler.Advance(context.Background(), run.ID)
	node := got.Nodes["effect"]
	if err != nil || got.Status != RunStatusSuspended || node.Status != NodeStatusRunning || node.ExecutionOwner != "" || node.ExecutionToken != "" || node.LeaseUntil != nil {
		t.Fatalf("run=%+v node=%+v err=%v", got, node, err)
	}
}

func TestExecutionClaimRejectsEmptyTokenAtomically(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, schedulerTool{key: "work"}), MaxParallel: 1,
		NewID: func() string { return "empty-token" }, OwnerID: "owner", LeaseTTL: time.Second,
		Now: clock.Now, NewToken: func() string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createPendingSchedulerRun(t, store, "claim-empty-token", oneClaimStep("work"))
	got, err := scheduler.Advance(context.Background(), run.ID)
	if err == nil || got.Version != run.Version {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != run.Version || stored.Nodes["effect"].Status != NodeStatusPending {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestExecutionClaimStaleHeartbeatMergeAndReleaseAreReadOnly(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	lease := clock.Now().Add(time.Minute)
	plan := oneClaimStep("work")
	created, err := store.CreateRun(context.Background(), PlanRun{
		ID: "claim-stale", SessionID: "s", UserID: "u", Plan: plan, Status: RunStatusRunning,
		Nodes: map[string]*NodeRun{"effect": {
			StepID: "effect", Status: NodeStatusRunning, Attempt: 1,
			ExecutionOwner: "owner-b", ExecutionToken: "owner-b:fresh", LeaseUntil: &lease,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduler := claimScheduler(t, store, "owner-a", clock, time.Hour, schedulerTool{key: "work"})
	stale := executionClaim{StepID: "effect", Attempt: 1, Owner: "owner-a", Token: "owner-a:stale"}
	if err := scheduler.renewClaims(context.Background(), created.ID, []executionClaim{stale}); err != nil {
		t.Fatal(err)
	}
	released, err := scheduler.releaseClaims(context.Background(), created.ID, []executionClaim{stale})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := scheduler.mergeOutcomes(context.Background(), released, []nodeOutcome{{
		step: plan.Steps[0], claim: stale, invoked: true,
		result: vocabulary.Result{Outputs: map[string]any{"winner": "stale"}},
	}, {
		step: plan.Steps[0], claim: stale, invoked: true, toolErr: errors.New("stale tool error"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetRun(context.Background(), created.ID)
	if released.Version != created.Version || merged.Version != created.Version || stored.Version != created.Version || stored.Nodes["effect"].ExecutionToken != "owner-b:fresh" || stored.Nodes["effect"].Outputs != nil {
		t.Fatalf("released=%+v merged=%+v stored=%+v", released, merged, stored)
	}
}

func TestExecutionClaimRetriesRealCASConflictBeforeCallingTool(t *testing.T) {
	base := NewMemoryRunStore()
	store := &conflictOnceStore{RunStore: base, conflict: 1}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	var calls atomic.Int32
	scheduler := claimScheduler(t, store, "owner", clock, time.Hour, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}})
	run := createPendingSchedulerRun(t, store, "claim-cas", oneClaimStep("work"))
	got, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || got.Status != RunStatusSucceeded || calls.Load() != 1 || got.Nodes["effect"].Attempt != 1 {
		t.Fatalf("run=%+v calls=%d err=%v", got, calls.Load(), err)
	}
}
