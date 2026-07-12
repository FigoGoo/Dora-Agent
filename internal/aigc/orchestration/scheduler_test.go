package orchestration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type schedulerTool struct {
	key    string
	inputs map[string]vocabulary.ParamSpec
	run    func(context.Context, vocabulary.Call) (vocabulary.Result, error)
}

func (t schedulerTool) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{Key: t.key, Name: t.key, Description: "scheduler test", Category: "cognition", Inputs: t.inputs}
}

func (t schedulerTool) Run(ctx context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	if t.run != nil {
		return t.run(ctx, call)
	}
	return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
}

func schedulerRegistry(t *testing.T, tools ...vocabulary.Tool) *vocabulary.Registry {
	t.Helper()
	registry := vocabulary.NewRegistry()
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func schedulerForTest(t *testing.T, store RunStore, registry *vocabulary.Registry, maxParallel int) *Scheduler {
	t.Helper()
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: registry, MaxParallel: maxParallel,
		NewID: func() string { return "run-1" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func TestSchedulerRunsDiamondDAGOnce(t *testing.T) {
	var calls atomic.Int32
	var active atomic.Int32
	var peak atomic.Int32
	var branchesDone atomic.Int32
	started := make(chan string, 2)
	release := make(chan struct{})
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		if call.NodeID == "b" || call.NodeID == "c" {
			current := active.Add(1)
			for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
			}
			started <- call.NodeID
			<-release
			active.Add(-1)
			branchesDone.Add(1)
		}
		if call.NodeID == "d" && branchesDone.Load() != 2 {
			return vocabulary.Result{}, errors.New("join ran before both branches completed")
		}
		return vocabulary.Result{Outputs: map[string]any{"node": call.NodeID}}, nil
	}}
	plan := ExecutionPlan{PlanID: "diamond", Source: "dynamic", Summary: "diamond", Direction: "image", Steps: []PlanStep{
		{ID: "a", Tool: "work", Required: true},
		{ID: "b", Tool: "work", DependsOn: []string{"a"}, Required: true},
		{ID: "c", Tool: "work", DependsOn: []string{"a"}, Required: true},
		{ID: "d", Tool: "work", DependsOn: []string{"b", "c"}, Required: true},
	}}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 2)
	type outcome struct {
		run PlanRun
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		run, err := scheduler.Submit(context.Background(), "s1", "u1", plan)
		done <- outcome{run: run, err: err}
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("diamond branches did not overlap")
		}
	}
	close(release)
	result := <-done
	if result.err != nil || result.run.Status != RunStatusSucceeded {
		t.Fatalf("status=%s err=%v", result.run.Status, result.err)
	}
	if !seen["b"] || !seen["c"] || calls.Load() != 4 || peak.Load() != 2 {
		t.Fatalf("seen=%v calls=%d peak=%d", seen, calls.Load(), peak.Load())
	}
}

func TestSchedulerHonorsMaxParallel(t *testing.T) {
	started := make(chan struct{}, 5)
	release := make(chan struct{}, 5)
	var active atomic.Int32
	var peak atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		started <- struct{}{}
		<-release
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	steps := make([]PlanStep, 5)
	for i := range steps {
		steps[i] = PlanStep{ID: fmt.Sprintf("n%d", i), Tool: "work", Required: true}
	}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 2)
	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
			PlanID: "parallel", Source: "dynamic", Summary: "parallel", Direction: "image", Steps: steps,
		})
		done <- err
	}()
	for _, wave := range []int{2, 2, 1} {
		for range wave {
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("ready node did not start")
			}
		}
		select {
		case <-started:
			t.Fatal("scheduler exceeded MaxParallel")
		default:
		}
		for range wave {
			release <- struct{}{}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 {
		t.Fatalf("peak=%d", peak.Load())
	}
}

func TestSchedulerResolvesRecursiveReferencesAndCopiesInputs(t *testing.T) {
	var received map[string]any
	source := schedulerTool{key: "source", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Outputs: map[string]any{
			"prompt": "rain", "meta": map[string]any{"labels": []any{"wet"}},
		}}, nil
	}}
	sink := schedulerTool{key: "sink", inputs: map[string]vocabulary.ParamSpec{"value": {Type: "object", Required: true}}, run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		received = call.Inputs
		call.Inputs["value"].(map[string]any)["meta"].(map[string]any)["labels"].([]any)[0] = "mutated"
		return vocabulary.Result{Outputs: map[string]any{"done": true}}, nil
	}}
	plan := ExecutionPlan{PlanID: "refs", Source: "dynamic", Summary: "refs", Direction: "image", Steps: []PlanStep{
		{ID: "a", Tool: "source", Required: true},
		{ID: "b", Tool: "sink", Params: map[string]any{"value": map[string]any{
			"items": []any{"$a.prompt", map[string]any{"copy": "$a.prompt"}},
			"meta":  "$a.meta", "literal": "cost $5",
		}}, DependsOn: []string{"a"}, Required: true},
	}}
	run, err := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, source, sink), 2).Submit(context.Background(), "s1", "u1", plan)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"items": []any{"rain", map[string]any{"copy": "rain"}}, "meta": map[string]any{"labels": []any{"mutated"}}, "literal": "cost $5"}
	if !reflect.DeepEqual(received["value"], want) {
		t.Fatalf("received=%#v", received)
	}
	labels := run.Nodes["a"].Outputs["meta"].(map[string]any)["labels"].([]any)
	if labels[0] != "wet" {
		t.Fatal("resolved input aliases upstream outputs")
	}
}

func TestSchedulerTreatsReferencedOutputStringsAsLiterals(t *testing.T) {
	var received any
	registry := schedulerRegistry(t,
		schedulerTool{key: "source", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			if call.NodeID == "a" {
				return vocabulary.Result{Outputs: map[string]any{"value": "$other.key"}}, nil
			}
			return vocabulary.Result{Outputs: map[string]any{"key": "rewritten"}}, nil
		}},
		schedulerTool{key: "sink", inputs: map[string]vocabulary.ParamSpec{"value": {Required: true}}, run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			received = call.Inputs["value"]
			return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
		}},
	)
	_, err := schedulerForTest(t, NewMemoryRunStore(), registry, 2).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "literal-output", Source: "dynamic", Summary: "literal-output", Direction: "image", Steps: []PlanStep{
			{ID: "a", Tool: "source", Required: true},
			{ID: "other", Tool: "source", Required: true},
			{ID: "sink", Tool: "sink", Params: map[string]any{"value": "$a.value"}, DependsOn: []string{"a", "other"}, Required: true},
		},
	})
	if err != nil || received != "$other.key" {
		t.Fatalf("received=%v err=%v", received, err)
	}
}

func TestSchedulerResolvesReferenceWithoutASCIIRestriction(t *testing.T) {
	var received any
	registry := schedulerRegistry(t,
		schedulerTool{key: "source", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Outputs: map[string]any{"提示词": "雨"}}, nil
		}},
		schedulerTool{key: "sink", inputs: map[string]vocabulary.ParamSpec{"value": {Required: true}}, run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			received = call.Inputs["value"]
			return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
		}},
	)
	_, err := schedulerForTest(t, NewMemoryRunStore(), registry, 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "unicode-ref", Source: "dynamic", Summary: "unicode-ref", Direction: "image", Steps: []PlanStep{
			{ID: "来源", Tool: "source", Required: true},
			{ID: "sink", Tool: "sink", Params: map[string]any{"value": "$来源.提示词"}, DependsOn: []string{"来源"}, Required: true},
		},
	})
	if err != nil || received != "雨" {
		t.Fatalf("received=%v err=%v", received, err)
	}
}

func TestSchedulerBuildsStableToolCall(t *testing.T) {
	var got vocabulary.Call
	tool := schedulerTool{key: "capture", inputs: map[string]vocabulary.ParamSpec{"value": {Type: "object", Required: true}}, run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		got = call
		call.Inputs["value"].(map[string]any)["changed"] = true
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	plan := ExecutionPlan{PlanID: "call", Source: "dynamic", Summary: "call", Direction: "image", Steps: []PlanStep{
		{ID: "node", Tool: "capture", Params: map[string]any{"value": map[string]any{"original": true}}, Required: true},
	}}
	run, err := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 1).Submit(context.Background(), "session", "user", plan)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "session" || got.UserID != "user" || got.PlanRunID != "run-1" || got.NodeID != "node" || got.Attempt != 1 || got.IdempotencyKey != "run-1:node:1" {
		t.Fatalf("call=%+v", got)
	}
	if run.Plan.Steps[0].Params["value"].(map[string]any)["changed"] != nil {
		t.Fatal("tool inputs alias stored plan params")
	}
}

func TestSchedulerTerminalOutcomes(t *testing.T) {
	failing := func(key string) schedulerTool {
		return schedulerTool{key: key, run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Fail: &vocabulary.Failure{Code: "rejected", Message: "no"}}, nil
		}}
	}
	succeed := schedulerTool{key: "succeed"}
	for _, tc := range []struct {
		name  string
		steps []PlanStep
		want  string
	}{
		{name: "all_success", steps: []PlanStep{{ID: "a", Tool: "succeed", Required: true}}, want: RunStatusSucceeded},
		{name: "required_failure", steps: []PlanStep{{ID: "a", Tool: "required_fail", Required: true}, {ID: "blocked", Tool: "succeed", DependsOn: []string{"a"}, Required: true}}, want: RunStatusFailed},
		{name: "optional_failure", steps: []PlanStep{{ID: "a", Tool: "succeed", Required: true}, {ID: "optional", Tool: "optional_fail", Required: false}}, want: RunStatusPartialSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, err := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, succeed, failing("required_fail"), failing("optional_fail")), 2).Submit(context.Background(), "s1", "u1", ExecutionPlan{
				PlanID: tc.name, Source: "dynamic", Summary: tc.name, Direction: "image", Steps: tc.steps,
			})
			if err != nil || run.Status != tc.want {
				t.Fatalf("status=%s want=%s err=%v", run.Status, tc.want, err)
			}
			for id, node := range run.Nodes {
				if node.Status == NodeStatusRunning {
					t.Fatalf("terminal run left node %s running", id)
				}
			}
		})
	}
}

func TestSchedulerBudgetPreviewDoesNotExecute(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: NewMemoryRunStore(), Vocabulary: schedulerRegistry(t, tool), MaxParallel: 1, JobBudget: 2,
		NewID: func() string { return "preview-run" },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "preview", Source: "dynamic", Summary: "preview", Direction: "image", EstimatedJobs: 3,
		Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil || run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || !run.PreviewRequired || calls.Load() != 0 {
		t.Fatalf("run=%+v calls=%d err=%v", run, calls.Load(), err)
	}
	version := run.Version
	again, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || again.Version != version || calls.Load() != 0 {
		t.Fatalf("suspended advance: version=%d want=%d calls=%d err=%v", again.Version, version, calls.Load(), err)
	}
}

func TestSchedulerTerminalAdvanceIsIdempotent(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 1)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "terminal", Source: "dynamic", Summary: "terminal", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || again.Version != run.Version || calls.Load() != 1 {
		t.Fatalf("before=%d after=%d calls=%d err=%v", run.Version, again.Version, calls.Load(), err)
	}
}

func TestSchedulerToolErrorAndFailureFailNodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool schedulerTool
	}{
		{name: "error", tool: schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{}, errors.New("offline")
		}}},
		{name: "failure", tool: schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Fail: &vocabulary.Failure{Code: "bad", Message: "bad input"}}, nil
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, err := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tc.tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
				PlanID: tc.name, Source: "dynamic", Summary: tc.name, Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
			})
			if err != nil || run.Status != RunStatusFailed || run.Nodes["a"].Status != NodeStatusFailed || run.Nodes["a"].Fail == nil {
				t.Fatalf("run=%+v err=%v", run, err)
			}
		})
	}
}

func TestSchedulerSuspensionStopsDownstream(t *testing.T) {
	var downstream atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser, Payload: map[string]any{"question": "continue?"}}}, nil
		}},
		schedulerTool{key: "next", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstream.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "pause", Source: "dynamic", Summary: "pause", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "next", Tool: "next", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil || run.Status != RunStatusSuspended || run.SuspendedNodeID != "pause" || run.Nodes["pause"].Suspension == nil || downstream.Load() != 0 {
		t.Fatalf("run=%+v downstream=%d err=%v", run, downstream.Load(), err)
	}
	version := run.Version
	again, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || again.Version != version || downstream.Load() != 0 {
		t.Fatalf("suspended rerun: version=%d/%d downstream=%d err=%v", version, again.Version, downstream.Load(), err)
	}
}

func TestSchedulerFailsClosedOnMultipleSuspensions(t *testing.T) {
	var downstream atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{
				Reason: SuspendWaitingUser, Payload: map[string]any{"node": call.NodeID},
			}}, nil
		}},
		schedulerTool{key: "next", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstream.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	run, err := schedulerForTest(t, NewMemoryRunStore(), registry, 2).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "ambiguous-pause", Source: "dynamic", Summary: "ambiguous-pause", Direction: "image", Steps: []PlanStep{
			{ID: "pause-a", Tool: "pause", Required: false},
			{ID: "pause-b", Tool: "pause", Required: false},
			{ID: "next", Tool: "next", DependsOn: []string{"pause-a", "pause-b"}, Required: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusFailed || run.SuspendReason != "" || run.SuspendedNodeID != "" || downstream.Load() != 0 {
		t.Fatalf("status=%s reason=%q node=%q downstream=%d", run.Status, run.SuspendReason, run.SuspendedNodeID, downstream.Load())
	}
	for _, id := range []string{"pause-a", "pause-b"} {
		node := run.Nodes[id]
		if node.Status != NodeStatusFailed || node.Fail == nil || node.Fail.Code != "multiple_suspensions" || node.Suspension != nil {
			t.Fatalf("node %s = %+v", id, node)
		}
	}
	for id, node := range run.Nodes {
		if node.Status == NodeStatusRunning {
			t.Fatalf("terminal run left node %s running", id)
		}
	}
}

type conflictOnceStore struct {
	RunStore
	mutation atomic.Int32
	conflict int32
}

type conflictRangeStore struct {
	RunStore
	mutation atomic.Int32
	from     int32
	through  int32
}

func (s *conflictRangeStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	mutation := s.mutation.Add(1)
	if mutation >= s.from && mutation <= s.through {
		return PlanRun{}, ErrRunVersionConflict
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (s *conflictOnceStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.mutation.Add(1) == s.conflict {
		return PlanRun{}, ErrRunVersionConflict
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func TestSchedulerCASConflictDoesNotRepeatTool(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	store := &conflictOnceStore{RunStore: NewMemoryRunStore(), conflict: 3}
	run, err := schedulerForTest(t, store, schedulerRegistry(t, tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cas", Source: "dynamic", Summary: "cas", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil || run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("status=%s calls=%d err=%v", run.Status, calls.Load(), err)
	}
}

func TestSchedulerStopsAfterCASConflictLimit(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	store := &conflictRangeStore{RunStore: NewMemoryRunStore(), from: 2, through: 10}
	_, err := schedulerForTest(t, store, schedulerRegistry(t, tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cas-limit", Source: "dynamic", Summary: "cas-limit", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if !errors.Is(err, ErrRunVersionConflict) || calls.Load() != 0 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestSchedulerMissingReferenceOutputDoesNotCallTool(t *testing.T) {
	var sinkCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "source", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Outputs: map[string]any{"other": true}}, nil
		}},
		schedulerTool{key: "sink", inputs: map[string]vocabulary.ParamSpec{"value": {Required: true}}, run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			sinkCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler, err := NewScheduler(SchedulerConfig{Store: NewMemoryRunStore(), Vocabulary: registry, MaxParallel: 1, NewID: func() string { return "missing-ref" }})
	if err != nil {
		t.Fatal(err)
	}
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "missing", Source: "dynamic", Summary: "missing", Direction: "image", Steps: []PlanStep{
			{ID: "a", Tool: "source", Required: true}, {ID: "b", Tool: "sink", Params: map[string]any{"value": "$a.missing"}, DependsOn: []string{"a"}, Required: true},
		},
	})
	if err == nil || run.Status != RunStatusFailed || sinkCalls.Load() != 0 {
		t.Fatalf("status=%s calls=%d err=%v", run.Status, sinkCalls.Load(), err)
	}
}

func TestSchedulerRejectsInvalidConfigAndInputs(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "work"})
	newID := func() string { return "run" }
	for name, cfg := range map[string]SchedulerConfig{
		"nil_store":      {Vocabulary: registry, NewID: newID},
		"nil_vocabulary": {Store: NewMemoryRunStore(), NewID: newID},
		"nil_new_id":     {Store: NewMemoryRunStore(), Vocabulary: registry},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewScheduler(cfg); err == nil {
				t.Fatal("expected config error")
			}
		})
	}
	scheduler, err := NewScheduler(SchedulerConfig{Store: NewMemoryRunStore(), Vocabulary: registry, NewID: newID})
	if err != nil {
		t.Fatal(err)
	}
	if scheduler.maxParallel != defaultMaxParallel || scheduler.jobBudget != 0 {
		t.Fatalf("defaults: maxParallel=%d jobBudget=%d", scheduler.maxParallel, scheduler.jobBudget)
	}
	valid := ExecutionPlan{PlanID: "valid", Source: "dynamic", Summary: "valid", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}}}
	for _, input := range [][2]string{{"", "user"}, {"session", ""}} {
		if _, err := scheduler.Submit(context.Background(), input[0], input[1], valid); err == nil {
			t.Fatalf("session/user %q/%q must fail", input[0], input[1])
		}
	}
	invalidPolicy := valid
	invalidPolicy.SuccessPolicy = "any"
	if _, err := scheduler.Submit(context.Background(), "session", "user", invalidPolicy); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("unsupported success policy: %v", err)
	}
	emptyID, err := NewScheduler(SchedulerConfig{Store: NewMemoryRunStore(), Vocabulary: registry, NewID: func() string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emptyID.Submit(context.Background(), "session", "user", valid); err == nil {
		t.Fatal("empty generated run id must fail")
	}
}
