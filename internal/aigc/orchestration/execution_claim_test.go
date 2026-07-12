package orchestration

import (
	"context"
	"errors"
	"fmt"
	"math"
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

type failHeartbeatStore struct {
	RunStore
	failNext atomic.Bool
	err      error
}

type queuedMutationErrorStore struct {
	RunStore
	mu     sync.Mutex
	errors []error
	failed chan struct{}
	once   sync.Once
}

func (s *queuedMutationErrorStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	s.mu.Lock()
	if len(s.errors) > 0 {
		err := s.errors[0]
		s.errors = s.errors[1:]
		s.mu.Unlock()
		s.once.Do(func() { close(s.failed) })
		return PlanRun{}, err
	}
	s.mu.Unlock()
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (s *queuedMutationErrorStore) failNext(errs ...error) {
	s.mu.Lock()
	s.errors = append(s.errors, errs...)
	s.mu.Unlock()
}

func (s *failHeartbeatStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.failNext.CompareAndSwap(true, false) {
		return PlanRun{}, s.err
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
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
	clock.Advance(20 * time.Second)
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
	clock.Advance(11 * time.Second)
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
	if err := scheduler.renewClaims(context.Background(), created.ID, []executionClaim{stale}); !errors.Is(err, ErrExecutionClaimLost) {
		t.Fatalf("renew err=%v", err)
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

func TestExecutionClaimExpiredHeartbeatCannotReviveLease(t *testing.T) {
	for _, order := range []string{"renew_then_reclaim", "reclaim_then_renew"} {
		t.Run(order, func(t *testing.T) {
			store := NewMemoryRunStore()
			clock := newClaimClock(time.Unix(1_700_000_000, 0))
			plan := oneClaimStep("work")
			lease := clock.Now().Add(30 * time.Second)
			created, err := store.CreateRun(context.Background(), PlanRun{
				ID: "expired-heartbeat-" + order, SessionID: "s", UserID: "u", Plan: plan, Status: RunStatusRunning,
				Nodes: map[string]*NodeRun{"effect": {
					StepID: "effect", Status: NodeStatusRunning, Attempt: 1, ExecutionEpoch: 1,
					ExecutionOwner: "owner-a", ExecutionToken: "owner-a:same", LeaseUntil: &lease,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			old := executionClaim{StepID: "effect", Attempt: 1, Epoch: 1, Owner: "owner-a", Token: "owner-a:same"}
			reclaimer := claimScheduler(t, store, "owner-b", clock, time.Hour, schedulerTool{key: "work"})
			clock.Advance(30 * time.Second)

			renew := func() {
				before, _ := store.GetRun(context.Background(), created.ID)
				if err := reclaimer.renewClaims(context.Background(), created.ID, []executionClaim{old}); !errors.Is(err, ErrExecutionClaimLost) {
					t.Fatalf("renew err=%v", err)
				}
				after, _ := store.GetRun(context.Background(), created.ID)
				if after.Version != before.Version {
					t.Fatalf("expired heartbeat changed version %d -> %d", before.Version, after.Version)
				}
			}
			reclaim := func() PlanRun {
				current, _ := store.GetRun(context.Background(), created.ID)
				claimed, claims, claimErr := reclaimer.claimReady(context.Background(), current)
				if claimErr != nil || len(claims) != 1 {
					t.Fatalf("claimed=%+v claims=%+v err=%v", claimed, claims, claimErr)
				}
				return claimed
			}
			var claimed PlanRun
			if order == "renew_then_reclaim" {
				renew()
				claimed = reclaim()
			} else {
				claimed = reclaim()
				renew()
			}
			node := claimed.Nodes["effect"]
			if node.ExecutionOwner != "owner-b" || node.ExecutionEpoch != 2 || node.Attempt != 1 {
				t.Fatalf("node=%+v", node)
			}
		})
	}

	t.Run("concurrent_barrier", func(t *testing.T) {
		store := NewMemoryRunStore()
		clock := newClaimClock(time.Unix(1_700_000_000, 0))
		plan := oneClaimStep("work")
		lease := clock.Now().Add(30 * time.Second)
		created, err := store.CreateRun(context.Background(), PlanRun{
			ID: "expired-heartbeat-concurrent", SessionID: "s", UserID: "u", Plan: plan, Status: RunStatusRunning,
			Nodes: map[string]*NodeRun{"effect": {
				StepID: "effect", Status: NodeStatusRunning, Attempt: 1, ExecutionEpoch: 1,
				ExecutionOwner: "owner-a", ExecutionToken: "owner-a:same", LeaseUntil: &lease,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		old := executionClaim{StepID: "effect", Attempt: 1, Epoch: 1, Owner: "owner-a", Token: "owner-a:same"}
		reclaimer := claimScheduler(t, store, "owner-b", clock, time.Hour, schedulerTool{key: "work"})
		clock.Advance(30 * time.Second)
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			err := reclaimer.renewClaims(context.Background(), created.ID, []executionClaim{old})
			if !errors.Is(err, ErrExecutionClaimLost) {
				errs <- fmt.Errorf("renew err=%v", err)
				return
			}
			errs <- nil
		}()
		go func() {
			<-start
			current, getErr := store.GetRun(context.Background(), created.ID)
			if getErr != nil {
				errs <- getErr
				return
			}
			_, claims, claimErr := reclaimer.claimReady(context.Background(), current)
			if claimErr != nil || len(claims) != 1 {
				errs <- fmt.Errorf("claims=%+v err=%v", claims, claimErr)
				return
			}
			errs <- nil
		}()
		close(start)
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		stored, _ := store.GetRun(context.Background(), created.ID)
		if stored.Version != created.Version+1 || stored.Nodes["effect"].ExecutionOwner != "owner-b" || stored.Nodes["effect"].ExecutionEpoch != 2 {
			t.Fatalf("stored=%+v", stored)
		}
	})
}

func TestExecutionClaimEpochFencesSameOwnerAndTokenCollision(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	firstStarted := make(chan struct{}, 1)
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	secondRelease := make(chan struct{})
	newScheduler := func(tool schedulerTool) *Scheduler {
		scheduler, err := NewScheduler(SchedulerConfig{
			Store: store, Vocabulary: schedulerRegistry(t, tool), MaxParallel: 1,
			CommitTimeout: time.Second, NewID: func() string { return "collision" },
			OwnerID: "same-owner", LeaseTTL: 30 * time.Second, HeartbeatInterval: time.Hour,
			Now: clock.Now, NewToken: func() string { return "same-token" },
		})
		if err != nil {
			t.Fatal(err)
		}
		return scheduler
	}
	first := newScheduler(schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		firstStarted <- struct{}{}
		<-firstRelease
		return vocabulary.Result{Outputs: map[string]any{"winner": "stale"}}, nil
	}})
	second := newScheduler(schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		secondStarted <- struct{}{}
		<-secondRelease
		return vocabulary.Result{Outputs: map[string]any{"winner": "fresh"}}, nil
	}})
	run := createPendingSchedulerRun(t, store, "claim-collision", oneClaimStep("work"))
	firstDone := advanceAsync(first, run.ID)
	<-firstStarted
	firstClaim, _ := store.GetRun(context.Background(), run.ID)
	if firstClaim.Nodes["effect"].ExecutionEpoch != 1 {
		t.Fatalf("first=%+v", firstClaim.Nodes["effect"])
	}
	clock.Advance(31 * time.Second)
	secondDone := advanceAsync(second, run.ID)
	<-secondStarted
	reclaimed, _ := store.GetRun(context.Background(), run.ID)
	old := executionClaim{
		StepID: "effect", Attempt: 1, Epoch: firstClaim.Nodes["effect"].ExecutionEpoch,
		Owner: firstClaim.Nodes["effect"].ExecutionOwner, Token: firstClaim.Nodes["effect"].ExecutionToken,
	}
	version := reclaimed.Version
	if err := first.renewClaims(context.Background(), run.ID, []executionClaim{old}); !errors.Is(err, ErrExecutionClaimLost) {
		t.Fatalf("old renew err=%v", err)
	}
	released, err := first.releaseClaims(context.Background(), run.ID, []executionClaim{old})
	if err != nil || released.Version != version {
		t.Fatalf("old release=%+v err=%v", released, err)
	}
	close(secondRelease)
	freshResult := <-secondDone
	fresh, err := freshResult.run, freshResult.err
	if err != nil || fresh.Status != RunStatusSucceeded || fresh.Nodes["effect"].ExecutionEpoch != 2 || fresh.Nodes["effect"].Outputs["winner"] != "fresh" {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
	version = fresh.Version
	close(firstRelease)
	stale := <-firstDone
	if stale.err != nil || stale.run.Version != version || stale.run.Nodes["effect"].Outputs["winner"] != "fresh" {
		t.Fatalf("stale=%+v err=%v", stale.run, stale.err)
	}
}

func TestExecutionClaimHeartbeatFailureCancelsWaveAndReleasesClaim(t *testing.T) {
	base := NewMemoryRunStore()
	heartbeatErr := errors.New("heartbeat store unavailable")
	store := &failHeartbeatStore{RunStore: base, err: heartbeatErr}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "block", run: func(ctx context.Context, _ vocabulary.Call) (vocabulary.Result, error) {
			firstCalls.Add(1)
			started <- struct{}{}
			<-ctx.Done()
			return vocabulary.Result{}, ctx.Err()
		}},
		schedulerTool{key: "later", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			secondCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: registry, MaxParallel: 1, CommitTimeout: 50 * time.Millisecond,
		NewID: func() string { return "heartbeat-failure" }, OwnerID: "owner", LeaseTTL: time.Second,
		HeartbeatInterval: time.Millisecond, Now: clock.Now, NewToken: func() string { return "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := ExecutionPlan{PlanID: "heartbeat-failure", Source: "dynamic", Summary: "heartbeat-failure", Direction: "image", Steps: []PlanStep{
		{ID: "first", Tool: "block", Required: true}, {ID: "second", Tool: "later", DependsOn: []string{"first"}, Required: true},
	}}
	run := createPendingSchedulerRun(t, store, "heartbeat-failure", plan)
	done := advanceAsync(scheduler, run.ID)
	<-started
	store.failNext.Store(true)
	result := <-done
	if !errors.Is(result.err, heartbeatErr) {
		t.Fatalf("err=%v", result.err)
	}
	if firstCalls.Load() != 1 || secondCalls.Load() != 0 {
		t.Fatalf("first=%d second=%d", firstCalls.Load(), secondCalls.Load())
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	firstNode := stored.Nodes["first"]
	if firstNode.Status != NodeStatusPending || firstNode.ExecutionToken != "" || firstNode.ExecutionEpoch != 1 || stored.Nodes["second"].Status != NodeStatusPending {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestExecutionClaimHeartbeatLossDiscardsSuccessAndRetriesSameKey(t *testing.T) {
	base := NewMemoryRunStore()
	heartbeatErr := errors.New("heartbeat failed")
	store := &queuedMutationErrorStore{RunStore: base, failed: make(chan struct{})}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	returnSuccess := make(chan struct{})
	var keysMu sync.Mutex
	var keys []string
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		started <- struct{}{}
		<-returnSuccess
		return vocabulary.Result{Outputs: map[string]any{"must": "discard"}}, nil
	}}
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, tool), MaxParallel: 1, CommitTimeout: 50 * time.Millisecond,
		NewID: func() string { return "heartbeat-discard" }, OwnerID: "owner", LeaseTTL: time.Second,
		HeartbeatInterval: time.Millisecond, Now: clock.Now, NewToken: func() string { return "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Record calls without making the first Tool honor wave cancellation.
	scheduler.vocabulary = schedulerRegistry(t, schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		keysMu.Lock()
		keys = append(keys, call.IdempotencyKey)
		keysMu.Unlock()
		started <- struct{}{}
		<-returnSuccess
		return vocabulary.Result{Outputs: map[string]any{"must": "discard"}}, nil
	}})
	run := createPendingSchedulerRun(t, store, "heartbeat-discard", oneClaimStep("work"))
	done := advanceAsync(scheduler, run.ID)
	<-started
	store.failNext(heartbeatErr)
	<-store.failed
	close(returnSuccess)
	result := <-done
	if !errors.Is(result.err, heartbeatErr) {
		t.Fatalf("err=%v", result.err)
	}
	node := result.run.Nodes["effect"]
	if node.Status != NodeStatusPending || node.ExecutionToken != "" || node.Outputs != nil {
		t.Fatalf("node=%+v", node)
	}
	retry := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		keysMu.Lock()
		keys = append(keys, call.IdempotencyKey)
		keysMu.Unlock()
		return vocabulary.Result{Outputs: map[string]any{"kept": true}}, nil
	}}
	scheduler.vocabulary = schedulerRegistry(t, retry)
	recovered, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestSchedulerGuardsApprovedMediaHeartbeatLossDiscardsOutcome(t *testing.T) {
	base := NewMemoryRunStore()
	heartbeatErr := errors.New("guarded media heartbeat failed")
	store := &failHeartbeatStore{RunStore: base, err: heartbeatErr}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	var keysMu sync.Mutex
	var keys []string
	tool := schedulerTool{key: "render", category: "media", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		attempt := calls.Add(1)
		keysMu.Lock()
		keys = append(keys, call.IdempotencyKey)
		keysMu.Unlock()
		if attempt == 1 {
			started <- struct{}{}
			<-releaseFirst
			return vocabulary.Result{Outputs: map[string]any{"asset": "stale"}}, nil
		}
		return vocabulary.Result{Outputs: map[string]any{"asset": "fresh"}}, nil
	}}
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, tool), Guard: vocabulary.NewGuardChain(vocabulary.GuardConfig{SoftTerms: []string{"soft-risk"}}),
		MaxParallel: 1, CommitTimeout: 50 * time.Millisecond, NewID: func() string { return "guard-heartbeat" },
		OwnerID: "owner", LeaseTTL: time.Second, HeartbeatInterval: time.Millisecond, Now: clock.Now, NewToken: func() string { return "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "guard-heartbeat", Source: "dynamic", Summary: "guard heartbeat", Direction: "image",
		Steps: []PlanStep{{ID: "render", Tool: "render", Params: map[string]any{"prompt": "soft-risk"}, Required: true}},
	})
	if err != nil || suspended.Status != RunStatusSuspended || calls.Load() != 0 {
		t.Fatalf("suspended=%+v calls=%d err=%v", suspended, calls.Load(), err)
	}
	type resumeResult struct {
		run PlanRun
		err error
	}
	done := make(chan resumeResult, 1)
	decision := map[string]any{"approved": true}
	go func() {
		run, resumeErr := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["render"].ResumeKey, decision)
		done <- resumeResult{run: run, err: resumeErr}
	}()
	<-started
	store.failNext.Store(true)
	time.Sleep(5 * time.Millisecond)
	close(releaseFirst)
	failed := <-done
	if !errors.Is(failed.err, heartbeatErr) {
		t.Fatalf("failed=%+v err=%v", failed.run, failed.err)
	}
	stored, err := base.GetRun(context.Background(), suspended.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := stored.Nodes["render"]
	if stored.Status != RunStatusRunning || node.Status != NodeStatusPending || node.ExecutionToken != "" || node.Outputs["asset"] != nil || !node.Resumed {
		t.Fatalf("stored=%+v node=%+v", stored, node)
	}
	recovered, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["render"].ResumeKey, decision)
	if err != nil || recovered.Status != RunStatusSucceeded || recovered.Nodes["render"].Outputs["asset"] != "fresh" || calls.Load() != 2 {
		t.Fatalf("recovered=%+v calls=%d err=%v", recovered, calls.Load(), err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 2 || keys[0] != "guard-heartbeat:render:1" || keys[1] != keys[0] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestExecutionClaimHeartbeatLossDiscardsWholeWave(t *testing.T) {
	base := NewMemoryRunStore()
	heartbeatErr := errors.New("heartbeat failed")
	store := &queuedMutationErrorStore{RunStore: base, failed: make(chan struct{})}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	firstDone := make(chan struct{}, 1)
	secondStarted := make(chan struct{}, 1)
	var keysMu sync.Mutex
	keys := make(map[string][]string)
	registry := schedulerRegistry(t,
		schedulerTool{key: "first", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			keysMu.Lock()
			keys[call.NodeID] = append(keys[call.NodeID], call.IdempotencyKey)
			keysMu.Unlock()
			firstDone <- struct{}{}
			return vocabulary.Result{Outputs: map[string]any{"must": "discard"}}, nil
		}},
		schedulerTool{key: "second", run: func(ctx context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			keysMu.Lock()
			keys[call.NodeID] = append(keys[call.NodeID], call.IdempotencyKey)
			keysMu.Unlock()
			secondStarted <- struct{}{}
			<-ctx.Done()
			return vocabulary.Result{}, ctx.Err()
		}},
	)
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: registry, MaxParallel: 2, CommitTimeout: 50 * time.Millisecond,
		NewID: func() string { return "heartbeat-wave" }, OwnerID: "owner", LeaseTTL: time.Second,
		HeartbeatInterval: time.Millisecond, Now: clock.Now, NewToken: func() string { return "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := ExecutionPlan{PlanID: "heartbeat-wave", Source: "dynamic", Summary: "heartbeat-wave", Direction: "image", Steps: []PlanStep{
		{ID: "first", Tool: "first", Required: true}, {ID: "second", Tool: "second", Required: true},
	}}
	run := createPendingSchedulerRun(t, store, "heartbeat-wave", plan)
	done := advanceAsync(scheduler, run.ID)
	<-firstDone
	<-secondStarted
	store.failNext(heartbeatErr)
	result := <-done
	if !errors.Is(result.err, heartbeatErr) {
		t.Fatalf("err=%v", result.err)
	}
	for _, id := range []string{"first", "second"} {
		node := result.run.Nodes[id]
		if node.Status != NodeStatusPending || node.ExecutionToken != "" || node.Outputs != nil {
			t.Fatalf("%s=%+v", id, node)
		}
	}
	retryRegistry := schedulerRegistry(t,
		schedulerTool{key: "first", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			keysMu.Lock()
			keys[call.NodeID] = append(keys[call.NodeID], call.IdempotencyKey)
			keysMu.Unlock()
			return vocabulary.Result{}, nil
		}},
		schedulerTool{key: "second", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			keysMu.Lock()
			keys[call.NodeID] = append(keys[call.NodeID], call.IdempotencyKey)
			keysMu.Unlock()
			return vocabulary.Result{}, nil
		}},
	)
	scheduler.vocabulary = retryRegistry
	recovered, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	for _, id := range []string{"first", "second"} {
		if len(keys[id]) != 2 || keys[id][0] != keys[id][1] {
			t.Fatalf("%s keys=%v", id, keys[id])
		}
	}
}

func TestExecutionClaimHeartbeatLossJoinsReleaseError(t *testing.T) {
	base := NewMemoryRunStore()
	heartbeatErr := errors.New("heartbeat failed")
	releaseErr := errors.New("release failed")
	store := &queuedMutationErrorStore{RunStore: base, failed: make(chan struct{})}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	returnSuccess := make(chan struct{})
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			started <- struct{}{}
			<-returnSuccess
			return vocabulary.Result{}, nil
		}}), MaxParallel: 1, CommitTimeout: 50 * time.Millisecond,
		NewID: func() string { return "heartbeat-release-error" }, OwnerID: "owner", LeaseTTL: time.Second,
		HeartbeatInterval: time.Millisecond, Now: clock.Now, NewToken: func() string { return "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createPendingSchedulerRun(t, store, "heartbeat-release-error", oneClaimStep("work"))
	done := advanceAsync(scheduler, run.ID)
	<-started
	store.failNext(heartbeatErr, releaseErr)
	<-store.failed
	close(returnSuccess)
	result := <-done
	if !errors.Is(result.err, heartbeatErr) || !errors.Is(result.err, releaseErr) {
		t.Fatalf("err=%v", result.err)
	}
	stored, _ := base.GetRun(context.Background(), run.ID)
	if stored.Nodes["effect"].Status != NodeStatusRunning || stored.Nodes["effect"].ExecutionToken == "" {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestExecutionClaimAdvancesEpochAfterReleaseWithTokenCollision(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: schedulerRegistry(t, schedulerTool{key: "work"}), MaxParallel: 1,
		NewID: func() string { return "epoch-release" }, OwnerID: "same-owner", LeaseTTL: time.Minute,
		HeartbeatInterval: time.Hour, Now: clock.Now, NewToken: func() string { return "same-token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createPendingSchedulerRun(t, store, "epoch-release", oneClaimStep("work"))
	firstRun, firstClaims, err := scheduler.claimReady(context.Background(), run)
	if err != nil || len(firstClaims) != 1 || firstClaims[0].Epoch != 1 {
		t.Fatalf("first=%+v claims=%+v err=%v", firstRun, firstClaims, err)
	}
	released, err := scheduler.releaseClaims(context.Background(), run.ID, firstClaims)
	if err != nil || released.Nodes["effect"].Status != NodeStatusPending {
		t.Fatalf("released=%+v err=%v", released, err)
	}
	secondRun, secondClaims, err := scheduler.claimReady(context.Background(), released)
	if err != nil || len(secondClaims) != 1 || secondClaims[0].Epoch != 2 || secondClaims[0].Token != firstClaims[0].Token {
		t.Fatalf("second=%+v claims=%+v err=%v", secondRun, secondClaims, err)
	}
	version := secondRun.Version
	if err := scheduler.renewClaims(context.Background(), run.ID, firstClaims); !errors.Is(err, ErrExecutionClaimLost) {
		t.Fatalf("stale renew err=%v", err)
	}
	staleRelease, err := scheduler.releaseClaims(context.Background(), run.ID, firstClaims)
	if err != nil || staleRelease.Version != version {
		t.Fatalf("stale release=%+v err=%v", staleRelease, err)
	}
	staleMerge, err := scheduler.mergeOutcomes(context.Background(), staleRelease, []nodeOutcome{{
		step: secondRun.Plan.Steps[0], claim: firstClaims[0], invoked: true,
		result: vocabulary.Result{Outputs: map[string]any{"winner": "stale"}},
	}})
	if err != nil || staleMerge.Version != version {
		t.Fatalf("stale merge=%+v err=%v", staleMerge, err)
	}
	fresh, err := scheduler.mergeOutcomes(context.Background(), staleMerge, []nodeOutcome{{
		step: secondRun.Plan.Steps[0], claim: secondClaims[0], invoked: true,
		result: vocabulary.Result{Outputs: map[string]any{"winner": "fresh"}},
	}})
	if err != nil || fresh.Nodes["effect"].Status != NodeStatusSucceeded || fresh.Nodes["effect"].Outputs["winner"] != "fresh" {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
}

func TestExecutionClaimRejectsExhaustedEpochAtomically(t *testing.T) {
	for _, status := range []string{NodeStatusPending, NodeStatusRunning} {
		t.Run(status, func(t *testing.T) {
			store := NewMemoryRunStore()
			clock := newClaimClock(time.Unix(1_700_000_000, 0))
			var calls atomic.Int32
			var tokenCalls atomic.Int32
			scheduler := claimScheduler(t, store, "owner", clock, time.Hour, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
				calls.Add(1)
				return vocabulary.Result{}, nil
			}})
			scheduler.newToken = func() string {
				tokenCalls.Add(1)
				return "must-not-be-used"
			}
			node := &NodeRun{StepID: "effect", Status: status, Attempt: 1, ExecutionEpoch: math.MaxInt64}
			if status == NodeStatusRunning {
				expired := clock.Now().Add(-time.Second)
				node.ExecutionOwner = "old-owner"
				node.ExecutionToken = "old-owner:token"
				node.LeaseUntil = &expired
			}
			created, err := store.CreateRun(context.Background(), PlanRun{
				ID: "epoch-exhausted-" + status, SessionID: "s", UserID: "u", Plan: oneClaimStep("work"),
				Status: RunStatusRunning, Nodes: map[string]*NodeRun{"effect": node},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := scheduler.Advance(context.Background(), created.ID)
			if !errors.Is(err, ErrExecutionFenceExhausted) || got.Version != created.Version || calls.Load() != 0 || tokenCalls.Load() != 0 {
				t.Fatalf("run=%+v calls=%d tokenCalls=%d err=%v", got, calls.Load(), tokenCalls.Load(), err)
			}
			stored, _ := store.GetRun(context.Background(), created.ID)
			if stored.Version != created.Version || stored.Nodes["effect"].ExecutionEpoch != math.MaxInt64 || stored.Nodes["effect"].Status != status {
				t.Fatalf("stored=%+v", stored)
			}
		})
	}
}
