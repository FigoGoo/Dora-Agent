package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func TestMemoryRunStoreEnforcesSingleActiveRunPerSession(t *testing.T) {
	store := NewMemoryRunStore()
	ctx := context.Background()
	first, err := store.CreateRun(ctx, PlanRun{ID: "active-1", SessionID: "session", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CreateRun(ctx, PlanRun{ID: "active-2", SessionID: "session", Status: RunStatusRunning, Nodes: map[string]*NodeRun{}})
	if !errors.Is(err, ErrSessionActiveRun) {
		t.Fatalf("second active create error = %v, want ErrSessionActiveRun", err)
	}
	var conflict *SessionActiveRunError
	if !errors.As(err, &conflict) || conflict.ActiveRunID != first.ID {
		t.Fatalf("active conflict = %#v", conflict)
	}
	active, err := store.GetActiveRun(ctx, "session")
	if err != nil || active.ID != first.ID {
		t.Fatalf("GetActiveRun() = %+v, %v", active, err)
	}

	if _, err := store.MutateRun(ctx, first.ID, first.Version, func(run *PlanRun) error {
		run.Status = RunStatusCancelled
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRun(ctx, PlanRun{ID: "active-3", SessionID: "session", Status: RunStatusSuspended, Nodes: map[string]*NodeRun{}}); err != nil {
		t.Fatalf("terminal run did not release session slot: %v", err)
	}
}

func TestMemoryRunStoreGetActiveRunFailsClosedOnCorruptionAndClonesNumbers(t *testing.T) {
	store := NewMemoryRunStore()
	store.runs["one"] = PlanRun{ID: "one", SessionID: "session", Status: RunStatusRunning, Version: 1, ResumeDecision: map[string]any{"n": json.Number("9007199254740993")}}
	active, err := store.GetActiveRun(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	active.ResumeDecision["n"] = json.Number("1")
	again, err := store.GetActiveRun(context.Background(), "session")
	if err != nil || again.ResumeDecision["n"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("deep cloned active run = %+v, %v", again, err)
	}
	store.runs["two"] = PlanRun{ID: "two", SessionID: "session", Status: RunStatusSuspended, Version: 1}
	if _, err := store.GetActiveRun(context.Background(), "session"); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("corrupt active set error = %v", err)
	}
	if _, err := store.GetActiveRun(context.Background(), "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing active error = %v", err)
	}
}

func TestMemoryRunStoreRejectsIdentityMutationAtomically(t *testing.T) {
	store := NewMemoryRunStore()
	created, err := store.CreateRun(context.Background(), PlanRun{ID: "identity", SessionID: "session", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, timed := range []bool{false, true} {
		var mutateErr error
		if timed {
			_, mutateErr = store.MutateRunAtAuthoritativeNow(context.Background(), created.ID, created.Version, func(run *PlanRun, _ time.Time) error {
				run.ID = "changed"
				return nil
			})
		} else {
			_, mutateErr = store.MutateRun(context.Background(), created.ID, created.Version, func(run *PlanRun) error {
				run.ID = "changed"
				return nil
			})
		}
		if !errors.Is(mutateErr, ErrRunRecordCorrupt) {
			t.Fatalf("timed=%v mutation error = %v", timed, mutateErr)
		}
		stored, getErr := store.GetRun(context.Background(), created.ID)
		if getErr != nil || stored.ID != created.ID || stored.Version != created.Version {
			t.Fatalf("timed=%v stored=%+v err=%v", timed, stored, getErr)
		}
	}
}

type createCommitThenErrorStore struct {
	RunStore
	err   error
	fired atomic.Bool
	read  error
}

func (s *createCommitThenErrorStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	created, err := s.RunStore.CreateRun(ctx, run)
	if err != nil {
		return created, err
	}
	if s.fired.CompareAndSwap(false, true) {
		return PlanRun{}, s.err
	}
	return created, nil
}

func (s *createCommitThenErrorStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	if s.fired.Load() && s.read != nil {
		readErr := s.read
		s.read = nil
		return PlanRun{}, readErr
	}
	return s.RunStore.GetRun(ctx, id)
}

func TestSchedulerRecoversCreateCommitThenError(t *testing.T) {
	ambiguous := errors.New("create acknowledgement lost")
	base := NewMemoryRunStore()
	store := &createCommitThenErrorStore{RunStore: base, err: ambiguous}
	var calls atomic.Int32
	tool := schedulerTool{key: "once", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return "ambiguous-create" })
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, err := scheduler.Submit(context.Background(), "session", "user", activeTestPlan("plan", "once"))
	if err != nil || run.ID != "ambiguous-create" || run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("Submit() = %+v, calls=%d, err=%v", run, calls.Load(), err)
	}
}

func TestSchedulerReturnsJoinedErrorWhenAmbiguousCreateReadFailsThenReplayRecovers(t *testing.T) {
	ambiguous := errors.New("create acknowledgement lost")
	readErr := errors.New("authoritative read unavailable")
	base := NewMemoryRunStore()
	store := &createCommitThenErrorStore{RunStore: base, err: ambiguous, read: readErr}
	var calls atomic.Int32
	tool := schedulerTool{key: "once", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	registry := schedulerRegistry(t, tool)
	ids := []string{"ambiguous", "retry"}
	var next atomic.Int32
	cfg := schedulerConfigForTest(store, registry, func() string { return ids[next.Add(1)-1] })
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := activeTestPlan("same", "once")
	if _, err := scheduler.Submit(context.Background(), "session", "user", plan); !errors.Is(err, ambiguous) || !errors.Is(err, readErr) {
		t.Fatalf("first Submit() error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("tool calls after uncertain read = %d", calls.Load())
	}
	recovered, err := scheduler.Submit(context.Background(), "session", "user", plan)
	if err != nil || recovered.ID != "ambiguous" || recovered.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("replayed Submit() = %+v, calls=%d, err=%v", recovered, calls.Load(), err)
	}
}

func TestSchedulerConcurrentSubmitsEnforceSessionSlot(t *testing.T) {
	for _, samePlan := range []bool{false, true} {
		t.Run(fmt.Sprintf("same_plan_%v", samePlan), func(t *testing.T) {
			store := NewMemoryRunStore()
			started := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var calls atomic.Int32
			tool := schedulerTool{key: "block", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return vocabulary.Result{}, nil
			}}
			registry := schedulerRegistry(t, tool)
			firstCfg := schedulerConfigForTest(store, registry, func() string { return "run-first" })
			secondCfg := schedulerConfigForTest(store, registry, func() string { return "run-second" })
			first, _ := NewScheduler(firstCfg)
			second, _ := NewScheduler(secondCfg)
			firstPlan := activeTestPlan("same", "block")
			secondPlan := activeTestPlan("different", "block")
			if samePlan {
				secondPlan = firstPlan
			}
			type result struct {
				run PlanRun
				err error
			}
			firstResult := make(chan result, 1)
			go func() {
				run, submitErr := first.Submit(context.Background(), "session", "user", firstPlan)
				firstResult <- result{run: run, err: submitErr}
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("first tool did not start")
			}
			secondRun, secondErr := second.Submit(context.Background(), "session", "user", secondPlan)
			if samePlan {
				if secondErr != nil || secondRun.ID != "run-first" {
					t.Fatalf("identical replay = %+v, %v", secondRun, secondErr)
				}
			} else if !errors.Is(secondErr, ErrSessionActiveRun) {
				t.Fatalf("different plan error = %v", secondErr)
			}
			releaseOnce.Do(func() { close(release) })
			completed := <-firstResult
			if completed.err != nil || completed.run.Status != RunStatusSucceeded || calls.Load() != 1 {
				t.Fatalf("first result=%+v calls=%d", completed, calls.Load())
			}
		})
	}
}

func TestSchedulerTerminalRunReleasesSessionForNewSubmit(t *testing.T) {
	store := NewMemoryRunStore()
	tool := schedulerTool{key: "done"}
	registry := schedulerRegistry(t, tool)
	ids := []string{"first", "second"}
	var next atomic.Int32
	cfg := schedulerConfigForTest(store, registry, func() string { return ids[next.Add(1)-1] })
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := scheduler.Submit(context.Background(), "session", "user", activeTestPlan("one", "done"))
	if err != nil || first.Status != RunStatusSucceeded {
		t.Fatalf("first Submit() = %+v, %v", first, err)
	}
	second, err := scheduler.Submit(context.Background(), "session", "user", activeTestPlan("two", "done"))
	if err != nil || second.ID != "second" || second.Status != RunStatusSucceeded {
		t.Fatalf("second Submit() = %+v, %v", second, err)
	}
}

func TestSchedulerSuspendedRunRetainsSessionSlot(t *testing.T) {
	store := NewMemoryRunStore()
	tool := schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
	}}
	registry := schedulerRegistry(t, tool)
	firstCfg := schedulerConfigForTest(store, registry, func() string { return "suspended" })
	secondCfg := schedulerConfigForTest(store, registry, func() string { return "blocked" })
	first, _ := NewScheduler(firstCfg)
	second, _ := NewScheduler(secondCfg)
	suspended, err := first.Submit(context.Background(), "session", "user", activeTestPlan("one", "pause"))
	if err != nil || suspended.Status != RunStatusSuspended {
		t.Fatalf("first Submit() = %+v, %v", suspended, err)
	}
	if _, err := second.Submit(context.Background(), "session", "user", activeTestPlan("two", "pause")); !errors.Is(err, ErrSessionActiveRun) {
		t.Fatalf("different submit error = %v", err)
	}
}

func TestSameSubmitRequestUsesCanonicalPlanNumbers(t *testing.T) {
	requested := PlanRun{
		SessionID: "session", UserID: "user", PreviewRequired: true, ResumeDecisionSchema: "approved_bool_v1",
		Plan: activeTestPlan("canonical", "tool"),
	}
	authoritative, err := clonePlanRun(requested)
	if err != nil {
		t.Fatal(err)
	}
	authoritative.Plan.Steps[0].Params = map[string]any{"large": json.Number("9007199254740993")}
	if !sameSubmitRequest(authoritative, requested) {
		t.Fatal("equivalent canonical plans did not match")
	}
	authoritative.Plan.Steps[0].Params["large"] = json.Number("9007199254740992")
	if sameSubmitRequest(authoritative, requested) {
		t.Fatal("adjacent large integers collided")
	}
	authoritative = requested
	authoritative.UserID = "other-user"
	if sameSubmitRequest(authoritative, requested) {
		t.Fatal("different user matched submit request")
	}
}

func activeTestPlan(id, tool string) ExecutionPlan {
	return ExecutionPlan{
		PlanID: id, Source: "dynamic", Summary: id, Direction: "image",
		Steps: []PlanStep{{ID: "step", Tool: tool, Params: map[string]any{"large": json.Number("9007199254740993")}, Required: true}},
	}
}
