package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	store.runs["one"] = PlanRun{ID: "one", RequestKey: "one", SubmitRequestFingerprint: "sha256:one", SessionID: "session", Status: RunStatusRunning, Version: 1, ResumeDecision: map[string]any{"n": json.Number("9007199254740993")}}
	active, err := store.GetActiveRun(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	active.ResumeDecision["n"] = json.Number("1")
	again, err := store.GetActiveRun(context.Background(), "session")
	if err != nil || again.ResumeDecision["n"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("deep cloned active run = %+v, %v", again, err)
	}
	store.runs["two"] = PlanRun{ID: "two", RequestKey: "two", SubmitRequestFingerprint: "sha256:two", SessionID: "session", Status: RunStatusSuspended, Version: 1}
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

func TestMemoryRunStoreRejectsRequestKeyMutationAtomically(t *testing.T) {
	store := NewMemoryRunStore()
	created, err := store.CreateRun(context.Background(), PlanRun{ID: "request-identity", RequestKey: "frozen-key", SessionID: "session", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MutateRun(context.Background(), created.ID, created.Version, func(run *PlanRun) error {
		run.RequestKey = "changed-key"
		return nil
	}); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("request key mutation error = %v", err)
	}
	stored, err := store.GetRunByRequestKey(context.Background(), "session", "frozen-key")
	if err != nil || stored.Version != created.Version || stored.RequestKey != created.RequestKey {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestMemoryRunStoreRejectsSubmitFingerprintMutationAtomically(t *testing.T) {
	store := NewMemoryRunStore()
	created, err := store.CreateRun(context.Background(), PlanRun{ID: "fingerprint-identity", RequestKey: "frozen-key", SessionID: "session", UserID: "user", Plan: activeTestPlan("fingerprint", "unused"), Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil || created.SubmitRequestFingerprint == "" {
		t.Fatalf("CreateRun() = %+v, %v", created, err)
	}
	if _, err := store.MutateRun(context.Background(), created.ID, created.Version, func(run *PlanRun) error {
		run.SubmitRequestFingerprint = "changed"
		return nil
	}); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("fingerprint mutation error = %v", err)
	}
	stored, err := store.GetRun(context.Background(), created.ID)
	if err != nil || stored.Version != created.Version || stored.SubmitRequestFingerprint != created.SubmitRequestFingerprint {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestMemoryRunStoreGetRejectsMissingSubmitIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		run  PlanRun
	}{
		{name: "request_key", run: PlanRun{ID: "missing-key", SubmitRequestFingerprint: "sha256:value", Status: RunStatusSucceeded}},
		{name: "fingerprint", run: PlanRun{ID: "missing-fingerprint", RequestKey: "request", Status: RunStatusSucceeded}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryRunStore()
			store.runs[test.run.ID] = test.run
			if _, err := store.GetRun(context.Background(), test.run.ID); !errors.Is(err, ErrRunRecordCorrupt) {
				t.Fatalf("GetRun() error = %v", err)
			}
		})
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
	if _, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "request-retry", plan); !errors.Is(err, ambiguous) || !errors.Is(err, readErr) {
		t.Fatalf("first Submit() error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("tool calls after uncertain read = %d", calls.Load())
	}
	recovered, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "request-retry", plan)
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
			if !errors.Is(secondErr, ErrSessionActiveRun) {
				t.Fatalf("different plan error = %v", secondErr)
			}
			if secondRun.ID != "" {
				t.Fatalf("independent Submit returned run %+v", secondRun)
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
	second, err := scheduler.Submit(context.Background(), "session", "user", activeTestPlan("one", "done"))
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
	if _, err := second.Submit(context.Background(), "session", "user", activeTestPlan("one", "pause")); !errors.Is(err, ErrSessionActiveRun) {
		t.Fatalf("same-plan independent submit error = %v", err)
	}
}

func TestSameSubmitRequestUsesCanonicalPlanNumbers(t *testing.T) {
	requested := PlanRun{
		RequestKey: "request-canonical", SessionID: "session", UserID: "user", PreviewRequired: true, ResumeDecisionSchema: "approved_bool_v1",
		Plan: activeTestPlan("canonical", "tool"),
	}
	if err := ensureInitialSubmitFingerprint(&requested); err != nil {
		t.Fatal(err)
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
	if !sameSubmitRequest(authoritative, requested) {
		t.Fatal("live plan mutation changed frozen submit identity")
	}
	otherUser := requested
	otherUser.UserID = "other-user"
	otherUser.SubmitRequestFingerprint = ""
	if err := ensureInitialSubmitFingerprint(&otherUser); err != nil {
		t.Fatal(err)
	}
	if sameSubmitRequest(otherUser, requested) {
		t.Fatal("different user matched submit request")
	}
	otherRequest := requested
	otherRequest.RequestKey = "other-request"
	otherRequest.SubmitRequestFingerprint = ""
	if err := ensureInitialSubmitFingerprint(&otherRequest); err != nil {
		t.Fatal(err)
	}
	if sameSubmitRequest(otherRequest, requested) {
		t.Fatal("different request key matched submit request")
	}
}

func TestSchedulerReplaysApprovedPreviewUsingImmutableRequestOnly(t *testing.T) {
	store := NewMemoryRunStore()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	tool := schedulerTool{key: "preview-work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return vocabulary.Result{}, nil
	}}
	registry := schedulerRegistry(t, tool)
	firstCfg := schedulerConfigForTest(store, registry, func() string { return "preview-active" })
	firstCfg.JobBudget = 1
	secondCfg := schedulerConfigForTest(store, registry, func() string { return "preview-replay" })
	secondCfg.JobBudget = 1
	first, _ := NewScheduler(firstCfg)
	second, _ := NewScheduler(secondCfg)
	plan := activeTestPlan("preview", "preview-work")
	plan.EstimatedJobs = 2
	suspended, err := first.SubmitWithKey(context.Background(), "session", "user", "preview-request", plan)
	if err != nil || !suspended.PreviewRequired {
		t.Fatalf("initial Submit() = %+v, %v", suspended, err)
	}
	type result struct {
		run PlanRun
		err error
	}
	resumeResult := make(chan result, 1)
	go func() {
		run, resumeErr := first.Resume(context.Background(), suspended.ID, suspended.ResumeKey, map[string]any{"approved": true})
		resumeResult <- result{run: run, err: resumeErr}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("approved preview did not start downstream tool")
	}
	authoritative, err := store.GetRun(context.Background(), suspended.ID)
	if err != nil || authoritative.PreviewRequired || authoritative.Status != RunStatusRunning {
		t.Fatalf("approved active run = %+v, %v", authoritative, err)
	}
	replayed, err := second.SubmitWithKey(context.Background(), "session", "user", "preview-request", plan)
	if err != nil || replayed.ID != suspended.ID {
		t.Fatalf("same-plan replay = %+v, %v", replayed, err)
	}
	different := plan
	different.PlanID = "different"
	if _, err := second.SubmitWithKey(context.Background(), "session", "user", "preview-request", different); !errors.Is(err, ErrSubmitRequestConflict) {
		t.Fatalf("same-key different-plan Submit() error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	completed := <-resumeResult
	if completed.err != nil || completed.run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("resume=%+v calls=%d", completed, calls.Load())
	}
}

func TestSchedulerSubmitWithKeyReplaysTerminalRunAndRejectsConflicts(t *testing.T) {
	store := NewMemoryRunStore()
	var calls atomic.Int32
	tool := schedulerTool{key: "keyed", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	var next atomic.Int32
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return fmt.Sprintf("keyed-%d", next.Add(1)) })
	scheduler, _ := NewScheduler(cfg)
	plan := activeTestPlan("keyed", "keyed")
	first, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-1", plan)
	if err != nil || first.Status != RunStatusSucceeded || first.RequestKey != "message-1" {
		t.Fatalf("first SubmitWithKey() = %+v, %v", first, err)
	}
	replayed, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-1", plan)
	if err != nil || replayed.ID != first.ID || calls.Load() != 1 {
		t.Fatalf("terminal replay = %+v calls=%d err=%v", replayed, calls.Load(), err)
	}
	changed := plan
	changed.PlanID = "changed"
	if _, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-1", changed); !errors.Is(err, ErrSubmitRequestConflict) {
		t.Fatalf("same-key changed-plan error = %v", err)
	}
	if _, err := scheduler.SubmitWithKey(context.Background(), "session", "other-user", "message-1", plan); !errors.Is(err, ErrSubmitRequestConflict) {
		t.Fatalf("same-key changed-user error = %v", err)
	}
	newRun, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-2", plan)
	if err != nil || newRun.ID != "keyed-5" || calls.Load() != 2 {
		t.Fatalf("new keyed request = %+v calls=%d err=%v", newRun, calls.Load(), err)
	}
}

func TestSchedulerSubmitWithKeyValidatesKey(t *testing.T) {
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, schedulerTool{key: "keyed"}), 1)
	for _, key := range []string{"", "   ", "contains space", strings.Repeat("x", 129), "bad/segment"} {
		if _, err := scheduler.SubmitWithKey(context.Background(), "session", "user", key, activeTestPlan("keyed", "keyed")); !errors.Is(err, ErrSubmitRequestKeyInvalid) {
			t.Fatalf("key %q error = %v", key, err)
		}
	}
	trimmed, err := scheduler.SubmitWithKey(context.Background(), "trim-session", "user", "  valid-key  ", activeTestPlan("keyed", "keyed"))
	if err != nil || trimmed.RequestKey != "valid-key" {
		t.Fatalf("trimmed key run = %+v, %v", trimmed, err)
	}
}

func TestMemoryRunStoreRequestKeyRoundTripAndLookup(t *testing.T) {
	store := NewMemoryRunStore()
	created, err := store.CreateRun(context.Background(), PlanRun{ID: "request-run", RequestKey: "request-key", SessionID: "session", Status: RunStatusSucceeded, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	byKey, err := store.GetRunByRequestKey(context.Background(), "session", "request-key")
	if err != nil || byKey.ID != created.ID || byKey.RequestKey != "request-key" {
		t.Fatalf("GetRunByRequestKey() = %+v, %v", byKey, err)
	}
}

func TestSchedulerSubmitWithKeyConvergesAcrossSchedulers(t *testing.T) {
	store := NewMemoryRunStore()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var calls atomic.Int32
	tool := schedulerTool{key: "keyed-block", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return vocabulary.Result{}, nil
	}}
	registry := schedulerRegistry(t, tool)
	leftCfg := schedulerConfigForTest(store, registry, func() string { return "key-left" })
	rightCfg := schedulerConfigForTest(store, registry, func() string { return "key-right" })
	left, _ := NewScheduler(leftCfg)
	right, _ := NewScheduler(rightCfg)
	plan := activeTestPlan("keyed-concurrent", "keyed-block")
	type result struct {
		run PlanRun
		err error
	}
	leftResult := make(chan result, 1)
	go func() {
		run, err := left.SubmitWithKey(context.Background(), "session", "user", "message-concurrent", plan)
		leftResult <- result{run: run, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first keyed tool did not start")
	}
	rightRun, rightErr := right.SubmitWithKey(context.Background(), "session", "user", "message-concurrent", plan)
	if rightErr != nil || rightRun.ID != "key-left" {
		t.Fatalf("right replay = %+v, %v", rightRun, rightErr)
	}
	once.Do(func() { close(release) })
	completed := <-leftResult
	if completed.err != nil || completed.run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("left=%+v calls=%d", completed, calls.Load())
	}
}

func TestSchedulerSubmitWithKeyIsScopedBySession(t *testing.T) {
	store := NewMemoryRunStore()
	tool := schedulerTool{key: "scoped"}
	var next atomic.Int32
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return fmt.Sprintf("scoped-%d", next.Add(1)) })
	scheduler, _ := NewScheduler(cfg)
	plan := activeTestPlan("scoped", "scoped")
	left, leftErr := scheduler.SubmitWithKey(context.Background(), "session-left", "user", "same-key", plan)
	right, rightErr := scheduler.SubmitWithKey(context.Background(), "session-right", "user", "same-key", plan)
	if leftErr != nil || rightErr != nil || left.ID == right.ID {
		t.Fatalf("left=%+v/%v right=%+v/%v", left, leftErr, right, rightErr)
	}
}

func TestSchedulerSubmitWithKeyReplaysOriginalRequestAfterRevision(t *testing.T) {
	store := NewMemoryRunStore()
	tool := schedulerTool{key: "fingerprint"}
	registry := schedulerRegistry(t, tool)
	var next atomic.Int32
	cfg := schedulerConfigForTest(store, registry, func() string { return fmt.Sprintf("fingerprint-%d", next.Add(1)) })
	cfg.JobBudget = 1
	scheduler, _ := NewScheduler(cfg)
	original := activeTestPlan("original", "fingerprint")
	original.EstimatedJobs = 2
	suspended, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-revised", original)
	if err != nil || suspended.SubmitRequestFingerprint == "" {
		t.Fatalf("initial SubmitWithKey() = %+v, %v", suspended, err)
	}
	revised, err := scheduler.Revise(context.Background(), suspended.ID, PlanRevision{AppendSteps: []PlanStep{{ID: "extra", Tool: "fingerprint", Required: true}}})
	if err != nil || len(revised.Plan.Steps) != 2 || revised.SubmitRequestFingerprint != suspended.SubmitRequestFingerprint {
		t.Fatalf("Revise() = %+v, %v", revised, err)
	}
	replayed, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-revised", original)
	if err != nil || replayed.ID != suspended.ID || replayed.Version != revised.Version {
		t.Fatalf("original replay = %+v, %v", replayed, err)
	}
	if _, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-revised", revised.Plan); !errors.Is(err, ErrSubmitRequestConflict) {
		t.Fatalf("live-plan replay error = %v", err)
	}
	completed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.ResumeKey, map[string]any{"approved": true})
	if err != nil || completed.Status != RunStatusSucceeded {
		t.Fatalf("Resume() = %+v, %v", completed, err)
	}
	terminalReplay, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-revised", original)
	if err != nil || terminalReplay.ID != suspended.ID || terminalReplay.Status != RunStatusSucceeded {
		t.Fatalf("terminal replay = %+v, %v", terminalReplay, err)
	}
}

func TestSubmitRequestFingerprintPreservesAdjacentLargeIntegers(t *testing.T) {
	left := activeTestPlan("large", "unused")
	right := activeTestPlan("large", "unused")
	left.Steps[0].Params["large"] = json.Number("9007199254740992")
	right.Steps[0].Params["large"] = json.Number("9007199254740993")
	leftFingerprint, leftErr := computeSubmitRequestFingerprint("session", "user", "request", left)
	rightFingerprint, rightErr := computeSubmitRequestFingerprint("session", "user", "request", right)
	if leftErr != nil || rightErr != nil || leftFingerprint == rightFingerprint {
		t.Fatalf("left=%q/%v right=%q/%v", leftFingerprint, leftErr, rightFingerprint, rightErr)
	}
}

func TestSchedulerOrdinarySubmitRejectsNewIDCollision(t *testing.T) {
	store := NewMemoryRunStore()
	var calls atomic.Int32
	tool := schedulerTool{key: "collision", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return "colliding-run" })
	scheduler, _ := NewScheduler(cfg)
	plan := activeTestPlan("collision", "collision")
	first, err := scheduler.Submit(context.Background(), "session", "user", plan)
	if err != nil || first.Status != RunStatusSucceeded {
		t.Fatalf("first Submit() = %+v, %v", first, err)
	}
	if _, err := scheduler.Submit(context.Background(), "session", "user", plan); !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("colliding Submit() error = %v", err)
	}
	stored, err := store.GetRun(context.Background(), first.ID)
	if err != nil || stored.Version != first.Version || calls.Load() != 1 {
		t.Fatalf("stored=%+v calls=%d err=%v", stored, calls.Load(), err)
	}
}

func TestSchedulerSubmitWithKeyReplaysDespiteNewIDCollision(t *testing.T) {
	store := NewMemoryRunStore()
	tool := schedulerTool{key: "key-collision"}
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return "same-run-id" })
	scheduler, _ := NewScheduler(cfg)
	plan := activeTestPlan("key-collision", "key-collision")
	first, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-collision", plan)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := scheduler.SubmitWithKey(context.Background(), "session", "user", "message-collision", plan)
	if err != nil || replayed.ID != first.ID {
		t.Fatalf("keyed collision replay = %+v, %v", replayed, err)
	}
}

func TestMemoryRunStoreSessionMigrationEnforcesActiveSlotAtomically(t *testing.T) {
	for _, timed := range []bool{false, true} {
		t.Run(fmt.Sprintf("timed_%v", timed), func(t *testing.T) {
			store := NewMemoryRunStore()
			occupied, err := store.CreateRun(context.Background(), PlanRun{ID: "occupied", SessionID: "target", Status: RunStatusRunning, Nodes: map[string]*NodeRun{}})
			if err != nil {
				t.Fatal(err)
			}
			moving, err := store.CreateRun(context.Background(), PlanRun{ID: "moving", SessionID: "source", Status: RunStatusRunning, Nodes: map[string]*NodeRun{}})
			if err != nil {
				t.Fatal(err)
			}
			_, mutateErr := mutateSessionForTest(store, timed, moving, "target", RunStatusRunning)
			if !errors.Is(mutateErr, ErrSessionActiveRun) {
				t.Fatalf("occupied migration error = %v", mutateErr)
			}
			for _, before := range []PlanRun{occupied, moving} {
				after, getErr := store.GetRun(context.Background(), before.ID)
				if getErr != nil || !reflect.DeepEqual(after, before) {
					t.Fatalf("rollback run %s = %+v, %v", before.ID, after, getErr)
				}
			}
			migrated, err := mutateSessionForTest(store, timed, moving, "free", RunStatusRunning)
			if err != nil || migrated.SessionID != "free" {
				t.Fatalf("free migration = %+v, %v", migrated, err)
			}
			byRequest, err := store.GetRunByRequestKey(context.Background(), "free", migrated.RequestKey)
			if err != nil || byRequest.ID != migrated.ID {
				t.Fatalf("migrated request index = %+v, %v", byRequest, err)
			}
			terminal, err := mutateSessionForTest(store, timed, migrated, "target", RunStatusCancelled)
			if err != nil || terminal.SessionID != "target" || terminal.Status != RunStatusCancelled {
				t.Fatalf("terminal migration = %+v, %v", terminal, err)
			}
		})
	}
}

func mutateSessionForTest(store RunStore, timed bool, run PlanRun, sessionID, status string) (PlanRun, error) {
	if timed {
		return store.MutateRunAtAuthoritativeNow(context.Background(), run.ID, run.Version, func(next *PlanRun, _ time.Time) error {
			next.SessionID = sessionID
			next.Status = status
			return nil
		})
	}
	return store.MutateRun(context.Background(), run.ID, run.Version, func(next *PlanRun) error {
		next.SessionID = sessionID
		next.Status = status
		return nil
	})
}

func activeTestPlan(id, tool string) ExecutionPlan {
	return ExecutionPlan{
		PlanID: id, Source: "dynamic", Summary: id, Direction: "image",
		Steps: []PlanStep{{ID: "step", Tool: tool, Params: map[string]any{"large": json.Number("9007199254740993")}, Required: true}},
	}
}
