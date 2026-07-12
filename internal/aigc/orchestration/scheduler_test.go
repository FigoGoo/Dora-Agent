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

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
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
	cfg := schedulerConfigForTest(store, registry, func() string { return "run-1" })
	cfg.MaxParallel = maxParallel
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

var schedulerTestSequence atomic.Int64

func schedulerConfigForTest(store RunStore, registry *vocabulary.Registry, newID func() string) SchedulerConfig {
	owner := fmt.Sprintf("test-owner-%d", schedulerTestSequence.Add(1))
	return SchedulerConfig{
		Store: store, Vocabulary: registry, NewID: newID, OwnerID: owner,
		Now: time.Now, NewToken: func() string { return fmt.Sprintf("token-%d", schedulerTestSequence.Add(1)) },
	}
}

func createPendingSchedulerRun(t *testing.T, store RunStore, id string, plan ExecutionPlan) PlanRun {
	t.Helper()
	nodes := make(map[string]*NodeRun, len(plan.Steps))
	for _, step := range plan.Steps {
		nodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
	}
	run, err := store.CreateRun(context.Background(), PlanRun{
		ID: id, SessionID: "s1", UserID: "u1", Plan: plan, Status: RunStatusRunning, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
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

func TestSchedulerSerializesConcurrentAdvanceForSameRun(t *testing.T) {
	var calls atomic.Int32
	var active atomic.Int32
	var peak atomic.Int32
	started := make(chan struct{}, 64)
	release := make(chan struct{})
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		started <- struct{}{}
		<-release
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	steps := make([]PlanStep, 4)
	for index := range steps {
		steps[index] = PlanStep{ID: fmt.Sprintf("n%d", index), Tool: "work", Required: true}
	}
	plan := ExecutionPlan{PlanID: "same-run", Source: "dynamic", Summary: "same-run", Direction: "image", Steps: steps}
	store := NewMemoryRunStore()
	createPendingSchedulerRun(t, store, "same-run", plan)
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, tool), 2)
	const advances = 8
	startAdvance := make(chan struct{})
	results := make(chan error, advances)
	for range advances {
		go func() {
			<-startAdvance
			run, err := scheduler.Advance(context.Background(), "same-run")
			if err == nil && run.Status != RunStatusSucceeded {
				err = fmt.Errorf("status=%s", run.Status)
			}
			results <- err
		}()
	}
	close(startAdvance)
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("first advance did not reach configured parallelism")
		}
	}
	close(release)
	for range advances {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 || peak.Load() > 2 {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
}

func TestSchedulerRunGateWaitCanBeCancelled(t *testing.T) {
	started := make(chan struct{}, 1)
	releaseTool := make(chan struct{})
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		started <- struct{}{}
		<-releaseTool
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	plan := ExecutionPlan{PlanID: "gate-cancel", Source: "dynamic", Summary: "gate-cancel", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}}}
	store := NewMemoryRunStore()
	createPendingSchedulerRun(t, store, "gate-cancel", plan)
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, tool), 1)
	holderDone := make(chan error, 1)
	go func() {
		_, err := scheduler.Advance(context.Background(), "gate-cancel")
		holderDone <- err
	}()
	<-started
	waitCtx, cancelWait := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := scheduler.Advance(waitCtx, "gate-cancel")
		waiterDone <- err
	}()
	cancelWait()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter err=%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled gate waiter did not return")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	close(releaseTool)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	scheduler.gateMu.Lock()
	defer scheduler.gateMu.Unlock()
	if len(scheduler.gates) != 0 {
		t.Fatalf("gate entries leaked: %d", len(scheduler.gates))
	}
}

func TestSchedulerRunGateAllowsDifferentRunsToAdvanceConcurrently(t *testing.T) {
	started := make(chan string, 2)
	releaseTool := make(chan struct{})
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		started <- call.PlanRunID
		<-releaseTool
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	plan := ExecutionPlan{PlanID: "different-runs", Source: "dynamic", Summary: "different-runs", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}}}
	store := NewMemoryRunStore()
	createPendingSchedulerRun(t, store, "run-a", plan)
	createPendingSchedulerRun(t, store, "run-b", plan)
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, tool), 1)
	results := make(chan error, 2)
	for _, runID := range []string{"run-a", "run-b"} {
		go func() {
			run, err := scheduler.Advance(context.Background(), runID)
			if err == nil && run.Status != RunStatusSucceeded {
				err = fmt.Errorf("run %s status=%s", runID, run.Status)
			}
			results <- err
		}()
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case runID := <-started:
			seen[runID] = true
		case <-time.After(500 * time.Millisecond):
			close(releaseTool)
			t.Fatal("different runs were serialized")
		}
	}
	close(releaseTool)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if !seen["run-a"] || !seen["run-b"] {
		t.Fatalf("seen=%v", seen)
	}
	scheduler.gateMu.Lock()
	defer scheduler.gateMu.Unlock()
	if len(scheduler.gates) != 0 {
		t.Fatalf("gate entries leaked: %d", len(scheduler.gates))
	}
}

func TestSchedulerCancellationStopsUnstartedToolsAndPersistsCompletedResults(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		callNumber := calls.Add(1)
		if callNumber <= 2 {
			started <- struct{}{}
			<-release
		}
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	steps := make([]PlanStep, 10)
	for index := range steps {
		steps[index] = PlanStep{ID: fmt.Sprintf("n%d", index), Tool: "work", Required: true}
	}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 2)
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		run PlanRun
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		run, err := scheduler.Submit(ctx, "s1", "u1", ExecutionPlan{
			PlanID: "cancel", Source: "dynamic", Summary: "cancel", Direction: "image", Steps: steps,
		})
		done <- outcome{run: run, err: err}
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	close(release)
	result := <-done
	if !errors.Is(result.err, context.Canceled) || calls.Load() != 2 {
		t.Fatalf("calls=%d err=%v", calls.Load(), result.err)
	}
	if result.run.Status != RunStatusRunning {
		t.Fatalf("status=%s", result.run.Status)
	}
	var succeeded, pending int
	for id, node := range result.run.Nodes {
		switch node.Status {
		case NodeStatusSucceeded:
			succeeded++
		case NodeStatusPending:
			pending++
		case NodeStatusRunning:
			t.Fatalf("node %s left running", id)
		default:
			t.Fatalf("node %s status=%s", id, node.Status)
		}
	}
	if succeeded != 2 || pending != 8 {
		t.Fatalf("succeeded=%d pending=%d", succeeded, pending)
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
	cfg := schedulerConfigForTest(NewMemoryRunStore(), schedulerRegistry(t, tool), func() string { return "preview-run" })
	cfg.MaxParallel, cfg.JobBudget = 1, 2
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "preview", Source: "dynamic", Summary: "preview", Direction: "image", EstimatedJobs: 3,
		Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil || run.Status != RunStatusSuspended || run.Version != 1 || run.SuspendReason != SuspendWaitingUser || !run.PreviewRequired || calls.Load() != 0 {
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

func TestSchedulerToolInfrastructureErrorRemainsPendingAndRetriesSameKey(t *testing.T) {
	toolErr := errors.New("provider offline")
	var flakyCalls atomic.Int32
	var siblingCalls atomic.Int32
	var keysMu sync.Mutex
	var flakyKeys []string
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		if call.NodeID == "sibling" {
			siblingCalls.Add(1)
			return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
		}
		attempt := flakyCalls.Add(1)
		keysMu.Lock()
		flakyKeys = append(flakyKeys, call.IdempotencyKey)
		keysMu.Unlock()
		if attempt == 1 {
			return vocabulary.Result{}, toolErr
		}
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 2)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "retry-tool", Source: "dynamic", Summary: "retry-tool", Direction: "image", Steps: []PlanStep{
			{ID: "a", Tool: "work", Required: true}, {ID: "sibling", Tool: "work", Required: true},
		},
	})
	if !errors.Is(err, toolErr) || run.Status != RunStatusRunning {
		t.Fatalf("status=%s err=%v", run.Status, err)
	}
	node := run.Nodes["a"]
	if node.Status != NodeStatusPending || node.Attempt != 1 || node.Fail != nil || node.ExecutionToken != "" {
		t.Fatalf("node=%+v", node)
	}
	if run.Nodes["sibling"].Status != NodeStatusSucceeded || siblingCalls.Load() != 1 {
		t.Fatalf("sibling=%+v calls=%d", run.Nodes["sibling"], siblingCalls.Load())
	}
	recovered, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded || flakyCalls.Load() != 2 || siblingCalls.Load() != 1 {
		t.Fatalf("status=%s flaky=%d sibling=%d err=%v", recovered.Status, flakyCalls.Load(), siblingCalls.Load(), err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(flakyKeys) != 2 || flakyKeys[0] != "run-1:a:1" || flakyKeys[1] != flakyKeys[0] {
		t.Fatalf("keys=%v", flakyKeys)
	}
}

func TestSchedulerInvalidToolResultsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result vocabulary.Result
	}{
		{name: "empty_suspension_reason", result: vocabulary.Result{Suspension: &vocabulary.Suspension{}}},
		{name: "unknown_suspension_reason", result: vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: "waiting_magic"}}},
		{name: "fail_and_suspension", result: vocabulary.Result{
			Fail: &vocabulary.Failure{Code: "original"}, Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser},
		}},
		{name: "fail_and_outputs", result: vocabulary.Result{
			Fail: &vocabulary.Failure{Code: "original"}, Outputs: map[string]any{"value": true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
				return tc.result, nil
			}}
			run, err := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
				PlanID: tc.name, Source: "dynamic", Summary: tc.name, Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			node := run.Nodes["a"]
			if run.Status != RunStatusFailed || node.Status != NodeStatusFailed || node.Fail == nil || node.Fail.Code != "invalid_tool_result" || node.Outputs != nil || node.Suspension != nil {
				t.Fatalf("run status=%s node=%+v", run.Status, node)
			}
		})
	}
}

func TestSchedulerSuspendsAndStopsDownstream(t *testing.T) {
	var downstream atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{
				Outputs:    map[string]any{"batch_id": "batch-1"},
				Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser, Payload: map[string]any{"question": "continue?"}},
			}, nil
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
	if err != nil || run.Status != RunStatusSuspended || run.SuspendedNodeID != "pause" || run.Nodes["pause"].Suspension == nil || run.Nodes["pause"].Outputs["batch_id"] != "batch-1" || downstream.Load() != 0 {
		t.Fatalf("run=%+v downstream=%d err=%v", run, downstream.Load(), err)
	}
	if run.Nodes["pause"].ResumeKey != "run-1:pause:1:resume" {
		t.Fatalf("resume key=%q", run.Nodes["pause"].ResumeKey)
	}
	version := run.Version
	again, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || again.Version != version || downstream.Load() != 0 {
		t.Fatalf("suspended rerun: version=%d/%d downstream=%d err=%v", version, again.Version, downstream.Load(), err)
	}
}

func TestResumeWaitingUserIsOneShotAndPreservesDecision(t *testing.T) {
	var downstream atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Outputs: map[string]any{"tool": "kept"}, Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstream.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "resume", Source: "dynamic", Summary: "resume", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true},
			{ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["pause"].ResumeKey
	if key == "" {
		t.Fatal("resume key missing")
	}
	for _, invalid := range []string{"", "wrong"} {
		before := suspended.Version
		got, resumeErr := scheduler.Resume(context.Background(), suspended.ID, invalid, nil)
		if !errors.Is(resumeErr, ErrResumeKeyMismatch) || got.Version != before || !strings.Contains(resumeErr.Error(), suspended.ID) || !strings.Contains(resumeErr.Error(), invalid) {
			t.Fatalf("invalid key %q: run=%+v err=%v", invalid, got, resumeErr)
		}
	}
	decision := map[string]any{"choice": map[string]any{"approved": true}, "items": []any{"a"}}
	resumed, err := scheduler.Resume(context.Background(), suspended.ID, key, decision)
	if err != nil || resumed.Status != RunStatusSucceeded || downstream.Load() != 1 {
		t.Fatalf("resume: run=%+v downstream=%d err=%v", resumed, downstream.Load(), err)
	}
	node := resumed.Nodes["pause"]
	if node.Status != NodeStatusSucceeded || !node.Resumed || node.Suspension != nil || node.Outputs["tool"] != "kept" || !reflect.DeepEqual(node.Outputs["resume_decision"], decision) || !reflect.DeepEqual(node.ResumeDecision, decision) {
		t.Fatalf("node=%+v", node)
	}
	decision["choice"].(map[string]any)["approved"] = false
	decision["items"].([]any)[0] = "changed"
	stored, err := scheduler.store.GetRun(context.Background(), suspended.ID)
	if err != nil || stored.Nodes["pause"].ResumeDecision["choice"].(map[string]any)["approved"] != true || stored.Nodes["pause"].ResumeDecision["items"].([]any)[0] != "a" {
		t.Fatalf("decision alias leaked: run=%+v err=%v", stored, err)
	}
	version := stored.Version
	replayed, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"choice": "different"})
	if err != nil || replayed.Version != version || downstream.Load() != 1 {
		t.Fatalf("replay: version=%d/%d downstream=%d err=%v", replayed.Version, version, downstream.Load(), err)
	}
}

func TestResumeUnserializableDecisionIsAtomic(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
	}})
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "bad-decision", Source: "dynamic", Summary: "bad-decision", Direction: "image", Steps: []PlanStep{{ID: "pause", Tool: "pause", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["pause"].ResumeKey, map[string]any{"bad": make(chan struct{})})
	if !errors.Is(err, ErrRunNotSerializable) || got.Version != suspended.Version {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	stored, _ := scheduler.store.GetRun(context.Background(), suspended.ID)
	if stored.Version != suspended.Version || stored.Status != RunStatusSuspended || stored.Nodes["pause"].Resumed || stored.Nodes["pause"].ResumeDecision != nil {
		t.Fatalf("mutation leaked: %+v", stored)
	}
}

func TestEvaluateSuspendsWaitingAgentWithoutRerunningTool(t *testing.T) {
	var evaluateCalls atomic.Int32
	var sinkCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "evaluate", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			evaluateCalls.Add(1)
			return vocabulary.Result{Outputs: map[string]any{"score": 0.9}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			sinkCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "evaluate", Source: "dynamic", Summary: "evaluate", Direction: "image", Steps: []PlanStep{
			{ID: "judge", Tool: "evaluate", Evaluate: true, Required: true},
			{ID: "sink", Tool: "sink", DependsOn: []string{"judge"}, Required: true},
		},
	})
	judge := suspended.Nodes["judge"]
	score, scoreOK := judge.Outputs["score"].(json.Number)
	if err != nil || suspended.Status != RunStatusSuspended || suspended.SuspendReason != SuspendWaitingAgent || judge.Status != NodeStatusSucceeded || !scoreOK || score.String() != "0.9" || judge.Suspension == nil || judge.ResumeKey == "" || evaluateCalls.Load() != 1 || sinkCalls.Load() != 0 {
		t.Fatalf("run=%+v judge=%+v evaluate=%d sink=%d err=%v", suspended, judge, evaluateCalls.Load(), sinkCalls.Load(), err)
	}
	resumed, err := scheduler.Resume(context.Background(), suspended.ID, judge.ResumeKey, map[string]any{"continue": true})
	if err != nil || resumed.Status != RunStatusSucceeded || evaluateCalls.Load() != 1 || sinkCalls.Load() != 1 {
		t.Fatalf("run=%+v evaluate=%d sink=%d err=%v", resumed, evaluateCalls.Load(), sinkCalls.Load(), err)
	}
}

func TestBudgetPreviewResumeStartsExecutionAndReplaysTerminalReceipt(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	cfg := schedulerConfigForTest(NewMemoryRunStore(), schedulerRegistry(t, tool), func() string { return "preview-resume" })
	cfg.JobBudget = 1
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "preview", Source: "dynamic", Summary: "preview", Direction: "image", EstimatedJobs: 2, Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}},
	})
	if err != nil || suspended.ResumeKey != "preview-resume:preview:resume" || suspended.Resumed || calls.Load() != 0 {
		t.Fatalf("run=%+v calls=%d err=%v", suspended, calls.Load(), err)
	}
	decision := map[string]any{"approved": true}
	resumed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.ResumeKey, decision)
	if err != nil || resumed.Status != RunStatusSucceeded || !resumed.Resumed || resumed.PreviewRequired || !reflect.DeepEqual(resumed.ResumeDecision, decision) || calls.Load() != 1 {
		t.Fatalf("run=%+v calls=%d err=%v", resumed, calls.Load(), err)
	}
	replayed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.ResumeKey, map[string]any{"approved": false})
	if err != nil || replayed.Version != resumed.Version || calls.Load() != 1 {
		t.Fatalf("replay=%+v calls=%d err=%v", replayed, calls.Load(), err)
	}
}

func TestResumeRejectsWaitingJobs(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "jobs", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs}}, nil
	}})
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "jobs", Source: "dynamic", Summary: "jobs", Direction: "image", Steps: []PlanStep{{ID: "jobs", Tool: "jobs", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["jobs"].ResumeKey, nil)
	if !errors.Is(err, ErrResumeReasonUnsupported) || got.Version != suspended.Version || !strings.Contains(err.Error(), suspended.ID) || !strings.Contains(err.Error(), suspended.Nodes["jobs"].ResumeKey) {
		t.Fatalf("run=%+v err=%v", got, err)
	}
}

func TestResumeOldReceiptDoesNotCrossNewSuspension(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "pause", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser, Payload: map[string]any{"node": call.NodeID}}}, nil
	}})
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	first, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "two-pauses", Source: "dynamic", Summary: "two-pauses", Direction: "image", Steps: []PlanStep{
			{ID: "first", Tool: "pause", Required: true}, {ID: "second", Tool: "pause", DependsOn: []string{"first"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldKey := first.Nodes["first"].ResumeKey
	second, err := scheduler.Resume(context.Background(), first.ID, oldKey, nil)
	if err != nil || second.Status != RunStatusSuspended || second.SuspendedNodeID != "second" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	version := second.Version
	replayed, err := scheduler.Resume(context.Background(), first.ID, oldKey, map[string]any{"different": true})
	if err != nil || replayed.Version != version || replayed.SuspendedNodeID != "second" || replayed.Nodes["second"].Resumed {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
}

func TestResumeConcurrentSameKeyAdvancesDownstreamOnce(t *testing.T) {
	var sinkCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			sinkCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "concurrent-resume", Source: "dynamic", Summary: "concurrent-resume", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["pause"].ResumeKey
	start := make(chan struct{})
	type result struct {
		run PlanRun
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			run, resumeErr := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": true})
			results <- result{run: run, err: resumeErr}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.run.Status != RunStatusSucceeded || second.run.Status != RunStatusSucceeded || first.run.Version != second.run.Version || sinkCalls.Load() != 1 {
		t.Fatalf("first=%+v/%v second=%+v/%v sink=%d", first.run, first.err, second.run, second.err, sinkCalls.Load())
	}
}

func TestResumeRunGateWaiterCanCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			started <- struct{}{}
			<-release
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "gate-cancel", Source: "dynamic", Summary: "gate-cancel", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["pause"].ResumeKey
	firstDone := make(chan error, 1)
	go func() {
		_, resumeErr := scheduler.Resume(context.Background(), suspended.ID, key, nil)
		firstDone <- resumeErr
	}()
	<-started
	waitCtx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, resumeErr := scheduler.Resume(waitCtx, suspended.ID, key, nil)
		waitDone <- resumeErr
	}()
	cancel()
	select {
	case waitErr := <-waitDone:
		if !errors.Is(waitErr, context.Canceled) {
			t.Fatalf("wait error=%v", waitErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled resume remained blocked on run gate")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestResumeCASConflictRereadsWithoutRepeatingDownstream(t *testing.T) {
	base := NewMemoryRunStore()
	var sinkCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			sinkCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	creator := schedulerForTest(t, base, registry, 1)
	suspended, err := creator.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "resume-conflict", Source: "dynamic", Summary: "resume-conflict", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &advancingConflictStore{RunStore: base}
	scheduler := schedulerForTest(t, store, registry, 1)
	resumed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["pause"].ResumeKey, nil)
	if err != nil || resumed.Status != RunStatusSucceeded || resumed.UserID != "concurrent-update" || sinkCalls.Load() != 1 {
		t.Fatalf("run=%+v sink=%d err=%v", resumed, sinkCalls.Load(), err)
	}
}

type resumeCommitStore struct {
	RunStore
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (s *resumeCommitStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.block.Load() {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return PlanRun{}, ctx.Err()
		case <-s.release:
		}
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func TestResumeCallerCancelStillCommitsReceipt(t *testing.T) {
	base := NewMemoryRunStore()
	store := &resumeCommitStore{RunStore: base, entered: make(chan struct{}, 1), release: make(chan struct{})}
	var sinkEffects atomic.Int32
	var sinkKeysMu sync.Mutex
	var sinkKeys []string
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			sinkEffects.Add(1)
			sinkKeysMu.Lock()
			sinkKeys = append(sinkKeys, call.IdempotencyKey)
			sinkKeysMu.Unlock()
			return vocabulary.Result{}, nil
		}},
	)
	cfg := schedulerConfigForTest(store, registry, func() string { return "cancel-receipt" })
	cfg.MaxParallel, cfg.CommitTimeout = 1, time.Second
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cancel-receipt", Source: "dynamic", Summary: "cancel-receipt", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		run PlanRun
		err error
	}
	done := make(chan result, 1)
	go func() {
		run, resumeErr := scheduler.Resume(ctx, suspended.ID, suspended.Nodes["pause"].ResumeKey, map[string]any{"approved": true})
		done <- result{run: run, err: resumeErr}
	}()
	<-store.entered
	cancel()
	close(store.release)
	got := <-done
	if !errors.Is(got.err, context.Canceled) || !got.run.Nodes["pause"].Resumed {
		t.Fatalf("run=%+v err=%v", got.run, got.err)
	}
	stored, err := base.GetRun(context.Background(), suspended.ID)
	if err != nil || !stored.Nodes["pause"].Resumed || stored.Status != RunStatusRunning {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	store.block.Store(false)
	recovered, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["pause"].ResumeKey, map[string]any{"approved": false})
	if err != nil || recovered.Status != RunStatusSucceeded || sinkEffects.Load() != 1 {
		t.Fatalf("recovered=%+v effects=%d err=%v", recovered, sinkEffects.Load(), err)
	}
	sinkKeysMu.Lock()
	defer sinkKeysMu.Unlock()
	if len(sinkKeys) != 1 || sinkKeys[0] != "cancel-receipt:sink:1" {
		t.Fatalf("sink keys=%v", sinkKeys)
	}
}

func TestResumeAppliedReceiptContinuesAfterDownstreamInfrastructureError(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	var sinkCalls atomic.Int32
	var keysMu sync.Mutex
	var keys []string
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			attempt := sinkCalls.Add(1)
			keysMu.Lock()
			keys = append(keys, call.IdempotencyKey)
			keysMu.Unlock()
			if attempt == 1 {
				return vocabulary.Result{}, providerErr
			}
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "resume-retry", Source: "dynamic", Summary: "resume-retry", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["pause"].ResumeKey
	decision := map[string]any{"approved": true}
	failed, err := scheduler.Resume(context.Background(), suspended.ID, key, decision)
	if !errors.Is(err, providerErr) || failed.Status != RunStatusRunning || !failed.Nodes["pause"].Resumed {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	receiptVersion := failed.Version
	recovered, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": false})
	if err != nil || recovered.Status != RunStatusSucceeded || sinkCalls.Load() != 2 {
		t.Fatalf("recovered=%+v calls=%d err=%v", recovered, sinkCalls.Load(), err)
	}
	if !reflect.DeepEqual(recovered.Nodes["pause"].ResumeDecision, decision) || recovered.Version <= receiptVersion {
		t.Fatalf("receipt changed or run did not advance: %+v", recovered.Nodes["pause"])
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 2 || keys[0] != "run-1:sink:1" || keys[1] != keys[0] {
		t.Fatalf("keys=%v", keys)
	}
}

type commitThenErrorStore struct {
	RunStore
	enabled atomic.Bool
	fired   atomic.Bool
	err     error
}

func (s *commitThenErrorStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	committed, err := s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
	if err != nil {
		return committed, err
	}
	if s.enabled.Load() && s.fired.CompareAndSwap(false, true) {
		return PlanRun{}, s.err
	}
	return committed, nil
}

func TestResumeReplaysReceiptAfterCommitReturnedAmbiguousError(t *testing.T) {
	ambiguousErr := errors.New("commit acknowledgement lost")
	base := NewMemoryRunStore()
	store := &commitThenErrorStore{RunStore: base, err: ambiguousErr}
	var sinkCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			sinkCalls.Add(1)
			return vocabulary.Result{}, nil
		}},
	)
	scheduler := schedulerForTest(t, store, registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "ambiguous-resume", Source: "dynamic", Summary: "ambiguous-resume", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.enabled.Store(true)
	key := suspended.Nodes["pause"].ResumeKey
	first, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": true})
	if !errors.Is(err, ambiguousErr) || first.Status != RunStatusSuspended {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	stored, err := base.GetRun(context.Background(), suspended.ID)
	if err != nil || stored.Status != RunStatusRunning || !stored.Nodes["pause"].Resumed {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	recovered, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": false})
	if err != nil || recovered.Status != RunStatusSucceeded || sinkCalls.Load() != 1 {
		t.Fatalf("recovered=%+v calls=%d err=%v", recovered, sinkCalls.Load(), err)
	}
}

type resumeReadBarrierStore struct {
	RunStore
	enabled atomic.Bool
	reads   atomic.Int32
	ready   chan struct{}
	release chan struct{}
}

func (s *resumeReadBarrierStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	run, err := s.RunStore.GetRun(ctx, id)
	if err != nil || !s.enabled.Load() {
		return run, err
	}
	read := s.reads.Add(1)
	if read <= 2 {
		if read == 2 {
			close(s.ready)
		}
		select {
		case <-ctx.Done():
			return PlanRun{}, ctx.Err()
		case <-s.release:
		}
	}
	return run, nil
}

func TestResumeAcrossSchedulersConvergesFromSameSuspendedSnapshot(t *testing.T) {
	base := NewMemoryRunStore()
	store := &resumeReadBarrierStore{RunStore: base, ready: make(chan struct{}), release: make(chan struct{})}
	var businessEffects atomic.Int32
	var invocations atomic.Int32
	var seen sync.Map
	var keysMu sync.Mutex
	var keys []string
	sinkStarted := make(chan struct{}, 2)
	sinkRelease := make(chan struct{})
	var sinkReleaseOnce sync.Once
	releaseSink := func() { sinkReleaseOnce.Do(func() { close(sinkRelease) }) }
	defer releaseSink()
	registry := schedulerRegistry(t,
		schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
		}},
		schedulerTool{key: "sink", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
			invocations.Add(1)
			keysMu.Lock()
			keys = append(keys, call.IdempotencyKey)
			keysMu.Unlock()
			if _, loaded := seen.LoadOrStore(call.IdempotencyKey, struct{}{}); !loaded {
				businessEffects.Add(1)
			}
			sinkStarted <- struct{}{}
			<-sinkRelease
			return vocabulary.Result{}, nil
		}},
	)
	creator := schedulerForTest(t, store, registry, 1)
	suspended, err := creator.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cross-scheduler", Source: "dynamic", Summary: "cross-scheduler", Direction: "image", Steps: []PlanStep{
			{ID: "pause", Tool: "pause", Required: true}, {ID: "sink", Tool: "sink", DependsOn: []string{"pause"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := schedulerForTest(t, store, registry, 1)
	second := schedulerForTest(t, store, registry, 1)
	store.enabled.Store(true)
	key := suspended.Nodes["pause"].ResumeKey
	type result struct {
		run PlanRun
		err error
	}
	results := make(chan result, 2)
	for _, scheduler := range []*Scheduler{first, second} {
		go func(s *Scheduler) {
			run, resumeErr := s.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": true})
			results <- result{run: run, err: resumeErr}
		}(scheduler)
	}
	select {
	case <-store.ready:
		close(store.release)
	case <-time.After(2 * time.Second):
		close(store.release)
		t.Fatal("both schedulers did not read the suspended snapshot")
	}
	select {
	case <-sinkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("claimed scheduler did not invoke sink")
	}
	releaseSink()
	a, b := <-results, <-results
	authoritative, getErr := base.GetRun(context.Background(), suspended.ID)
	wantVersion := suspended.Version + 4 // receipt + sink claim + sink merge + terminal finalize
	keysMu.Lock()
	defer keysMu.Unlock()
	validSnapshot := func(run PlanRun) bool { return run.Status == RunStatusRunning || run.Status == RunStatusSucceeded }
	if a.err != nil || b.err != nil || getErr != nil || !validSnapshot(a.run) || !validSnapshot(b.run) || (a.run.Status != RunStatusSucceeded && b.run.Status != RunStatusSucceeded) || authoritative.Status != RunStatusSucceeded || authoritative.Version != wantVersion || invocations.Load() != 1 || len(keys) != 1 || businessEffects.Load() != 1 {
		t.Fatalf("a=%+v/%v b=%+v/%v authoritative=%+v/%v wantVersion=%d invocations=%d keys=%v effects=%d", a.run, a.err, b.run, b.err, authoritative, getErr, wantVersion, invocations.Load(), keys, businessEffects.Load())
	}
}

func TestSchedulerTerminalAuthorityDiscardsLateWaveToolError(t *testing.T) {
	store := NewMemoryRunStore()
	plan := ExecutionPlan{
		PlanID: "terminal-authority", Source: "dynamic", Summary: "terminal-authority", Direction: "image",
		Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}},
	}
	created := createPendingSchedulerRun(t, store, "terminal-authority-run", plan)
	goodStarted := make(chan struct{}, 1)
	goodRelease := make(chan struct{})
	goodRegistry := schedulerRegistry(t, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		goodStarted <- struct{}{}
		<-goodRelease
		return vocabulary.Result{}, nil
	}})
	var badCalls atomic.Int32
	badRegistry := schedulerRegistry(t, schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		badCalls.Add(1)
		return vocabulary.Result{}, errors.New("must not run")
	}})
	goodScheduler := schedulerForTest(t, store, goodRegistry, 1)
	badScheduler := schedulerForTest(t, store, badRegistry, 1)
	type result struct {
		run PlanRun
		err error
	}
	goodDone := make(chan result, 1)
	go func() {
		run, err := goodScheduler.Advance(context.Background(), created.ID)
		goodDone <- result{run: run, err: err}
	}()
	select {
	case <-goodStarted:
	case <-time.After(2 * time.Second):
		close(goodRelease)
		t.Fatal("authoritative scheduler did not start")
	}
	contender, err := badScheduler.Advance(context.Background(), created.ID)
	if err != nil || contender.Status != RunStatusRunning || badCalls.Load() != 0 {
		close(goodRelease)
		t.Fatalf("contender=%+v calls=%d err=%v", contender, badCalls.Load(), err)
	}
	close(goodRelease)
	good := <-goodDone
	if good.err != nil || good.run.Status != RunStatusSucceeded {
		t.Fatalf("authoritative scheduler: run=%+v err=%v", good.run, good.err)
	}
}

func TestSchedulerMergeOutcomesDoesNotCommitAlreadyAppliedWave(t *testing.T) {
	store := NewMemoryRunStore()
	plan := ExecutionPlan{
		PlanID: "already-applied", Source: "dynamic", Summary: "already-applied", Direction: "image",
		Steps: []PlanStep{{ID: "work", Tool: "work", Required: true}},
	}
	stale := createPendingSchedulerRun(t, store, "already-applied-run", plan)
	authoritative, err := store.MutateRun(context.Background(), stale.ID, stale.Version, func(run *PlanRun) error {
		run.Nodes["work"].Status = NodeStatusSucceeded
		run.Nodes["work"].Attempt = 1
		run.Nodes["work"].Outputs = map[string]any{"ok": true}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, schedulerTool{key: "work"}), 1)
	merged, err := scheduler.mergeOutcomes(context.Background(), stale, []nodeOutcome{{
		step: plan.Steps[0], claim: executionClaim{StepID: "work", Attempt: 1, Owner: "stale", Token: "stale:token"},
		invoked: true, result: vocabulary.Result{Outputs: map[string]any{"ok": true}},
	}})
	if err != nil || merged.Version != authoritative.Version {
		t.Fatalf("merged=%+v authoritative=%+v err=%v", merged, authoritative, err)
	}
	stored, err := store.GetRun(context.Background(), stale.ID)
	if err != nil || stored.Version != authoritative.Version {
		t.Fatalf("stored=%+v authoritative=%+v err=%v", stored, authoritative, err)
	}
}

func TestResumeRejectsReservedOutputConflictWithoutMutation(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Outputs: map[string]any{"resume_decision": "tool-owned"}, Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
	}})
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "reserved-output", Source: "dynamic", Summary: "reserved-output", Direction: "image", Steps: []PlanStep{{ID: "pause", Tool: "pause", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := suspended.Nodes["pause"].ResumeKey
	got, err := scheduler.Resume(context.Background(), suspended.ID, key, map[string]any{"approved": true})
	if !errors.Is(err, ErrResumeDecisionConflict) || got.Version != suspended.Version || !strings.Contains(err.Error(), suspended.ID) || !strings.Contains(err.Error(), key) {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	stored, _ := scheduler.store.GetRun(context.Background(), suspended.ID)
	if stored.Version != suspended.Version || stored.Nodes["pause"].Outputs["resume_decision"] != "tool-owned" || stored.Nodes["pause"].Resumed {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestResumeNilDecisionFreezesEmptyMap(t *testing.T) {
	registry := schedulerRegistry(t, schedulerTool{key: "pause", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser}}, nil
	}})
	scheduler := schedulerForTest(t, NewMemoryRunStore(), registry, 1)
	suspended, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "nil-decision", Source: "dynamic", Summary: "nil-decision", Direction: "image", Steps: []PlanStep{{ID: "pause", Tool: "pause", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := scheduler.Resume(context.Background(), suspended.ID, suspended.Nodes["pause"].ResumeKey, nil)
	if err != nil || resumed.Status != RunStatusSucceeded || resumed.Nodes["pause"].ResumeDecision == nil || len(resumed.Nodes["pause"].ResumeDecision) != 0 {
		t.Fatalf("run=%+v err=%v", resumed, err)
	}
	output, exists := resumed.Nodes["pause"].Outputs["resume_decision"]
	if !exists || !reflect.DeepEqual(output, map[string]any{}) {
		t.Fatalf("output=%#v exists=%v", output, exists)
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

type failFirstMutationStore struct {
	RunStore
	err    error
	failed atomic.Bool
}

type blockingMutationStore struct {
	RunStore
	block   atomic.Bool
	release chan struct{}
}

func (s *blockingMutationStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.block.Load() {
		select {
		case <-ctx.Done():
			return PlanRun{}, ctx.Err()
		case <-s.release:
			return PlanRun{}, errors.New("blocking store released without receipt timeout")
		}
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func TestSchedulerReceiptCommitTimeoutBoundsCancelledSubmit(t *testing.T) {
	store := &blockingMutationStore{RunStore: NewMemoryRunStore(), release: make(chan struct{})}
	started := make(chan vocabulary.Call, 1)
	releaseTool := make(chan struct{})
	var blockOnce sync.Once
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		started <- call
		<-releaseTool
		blockOnce.Do(func() { store.block.Store(true) })
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return "timeout-run" })
	cfg.MaxParallel, cfg.CommitTimeout = 1, 50*time.Millisecond
	cfg.Now, cfg.LeaseTTL, cfg.HeartbeatInterval = clock.Now, time.Second, time.Hour
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		run PlanRun
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		run, err := scheduler.Submit(ctx, "s1", "u1", ExecutionPlan{
			PlanID: "timeout", Source: "dynamic", Summary: "timeout", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
		})
		done <- outcome{run: run, err: err}
	}()
	firstCall := <-started
	cancel()
	close(releaseTool)
	var result outcome
	select {
	case result = <-done:
	case <-time.After(500 * time.Millisecond):
		close(store.release)
		result = <-done
		t.Fatalf("receipt persist exceeded bound: %v", result.err)
	}
	if !errors.Is(result.err, context.Canceled) || !errors.Is(result.err, context.DeadlineExceeded) || result.run.ID != "timeout-run" {
		t.Fatalf("run=%+v err=%v", result.run, result.err)
	}
	node := result.run.Nodes["a"]
	if result.run.Status != RunStatusRunning || node.Status != NodeStatusRunning || node.Attempt != 1 || node.ExecutionToken == "" {
		t.Fatalf("run=%+v node=%+v", result.run, node)
	}
	store.block.Store(false)
	clock.Advance(2 * time.Second)
	recovered, err := scheduler.Advance(context.Background(), result.run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded {
		t.Fatalf("status=%s err=%v", recovered.Status, err)
	}
	if firstCall.IdempotencyKey != "timeout-run:a:1" {
		t.Fatalf("first key=%s", firstCall.IdempotencyKey)
	}
}

func (s *failFirstMutationStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.failed.CompareAndSwap(false, true) {
		return PlanRun{}, s.err
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func TestSchedulerSubmitMergeFailureLeavesRecoverableRunningRun(t *testing.T) {
	mergeErr := errors.New("store unavailable")
	store := &failFirstMutationStore{RunStore: NewMemoryRunStore(), err: mergeErr}
	var calls atomic.Int32
	var keysMu sync.Mutex
	var keys []string
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		keysMu.Lock()
		keys = append(keys, call.IdempotencyKey)
		keysMu.Unlock()
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, tool), 1)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "recoverable", Source: "dynamic", Summary: "recoverable", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if !errors.Is(err, mergeErr) || run.ID != "run-1" || calls.Load() != 0 {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	stored, getErr := store.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Status != RunStatusRunning || stored.Version != 1 || stored.Nodes["a"].Status != NodeStatusPending || stored.Nodes["a"].Attempt != 0 {
		t.Fatalf("stored=%+v node=%+v", stored, stored.Nodes["a"])
	}
	recovered, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("status=%s calls=%d err=%v", recovered.Status, calls.Load(), err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 1 || keys[0] != "run-1:a:1" {
		t.Fatalf("keys=%v", keys)
	}
}

func TestSchedulerUnserializableReceiptRemainsPendingAndRecoverable(t *testing.T) {
	for _, kind := range []string{"outputs", "suspension"} {
		t.Run(kind, func(t *testing.T) {
			var invalid atomic.Bool
			invalid.Store(true)
			var keysMu sync.Mutex
			var keys []string
			tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
				keysMu.Lock()
				keys = append(keys, call.IdempotencyKey)
				keysMu.Unlock()
				if !invalid.Load() {
					return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
				}
				if kind == "outputs" {
					return vocabulary.Result{Outputs: map[string]any{"bad": make(chan struct{})}}, nil
				}
				return vocabulary.Result{Suspension: &vocabulary.Suspension{
					Reason: SuspendWaitingUser, Payload: map[string]any{"bad": make(chan struct{})},
				}}, nil
			}}
			scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 1)
			run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
				PlanID: "bad-receipt", Source: "dynamic", Summary: "bad-receipt", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
			})
			if !errors.Is(err, ErrRunNotSerializable) || run.ID != "run-1" {
				t.Fatalf("run=%+v err=%v", run, err)
			}
			node := run.Nodes["a"]
			if run.Status != RunStatusRunning || node.Status != NodeStatusPending || node.Attempt != 1 || node.Outputs != nil || node.Suspension != nil || node.ExecutionToken != "" {
				t.Fatalf("run=%+v node=%+v", run, node)
			}
			invalid.Store(false)
			recovered, err := scheduler.Advance(context.Background(), run.ID)
			if err != nil || recovered.Status != RunStatusSucceeded {
				t.Fatalf("status=%s err=%v", recovered.Status, err)
			}
			keysMu.Lock()
			defer keysMu.Unlock()
			if len(keys) != 2 || keys[0] != "run-1:a:1" || keys[1] != keys[0] {
				t.Fatalf("keys=%v", keys)
			}
		})
	}
}

type advancingConflictRangeStore struct {
	RunStore
	mutation  atomic.Int32
	conflicts int32
}

type advancingConflictStore struct {
	RunStore
	conflicted atomic.Bool
}

func (s *advancingConflictStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if s.conflicted.CompareAndSwap(false, true) {
		if _, err := s.RunStore.MutateRun(ctx, id, expectedVersion, func(run *PlanRun) error {
			run.UserID = "concurrent-update"
			return nil
		}); err != nil {
			return PlanRun{}, err
		}
		return PlanRun{}, ErrRunVersionConflict
	}
	return s.RunStore.MutateRun(ctx, id, expectedVersion, mutate)
}

func (s *advancingConflictRangeStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	mutation := s.mutation.Add(1)
	if mutation <= s.conflicts {
		if _, err := s.RunStore.MutateRun(ctx, id, expectedVersion, func(run *PlanRun) error {
			run.Plan.Summary += fmt.Sprintf("|conflict-%d", mutation)
			return nil
		}); err != nil {
			return PlanRun{}, err
		}
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
	store := &conflictOnceStore{RunStore: NewMemoryRunStore(), conflict: 1}
	run, err := schedulerForTest(t, store, schedulerRegistry(t, tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cas", Source: "dynamic", Summary: "cas", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil || run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("status=%s calls=%d err=%v", run.Status, calls.Load(), err)
	}
}

func TestSchedulerCASConflictRereadsAndPreservesConcurrentUpdate(t *testing.T) {
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	store := &advancingConflictStore{RunStore: NewMemoryRunStore()}
	run, err := schedulerForTest(t, store, schedulerRegistry(t, tool), 1).Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cas-reread", Source: "dynamic", Summary: "cas-reread", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if err != nil || run.Status != RunStatusSucceeded || run.UserID != "concurrent-update" || calls.Load() != 1 {
		t.Fatalf("status=%s user=%s calls=%d err=%v", run.Status, run.UserID, calls.Load(), err)
	}
}

func TestSchedulerStopsAfterCASConflictLimit(t *testing.T) {
	var calls atomic.Int32
	var keysMu sync.Mutex
	var keys []string
	tool := schedulerTool{key: "work", run: func(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		keysMu.Lock()
		keys = append(keys, call.IdempotencyKey)
		keysMu.Unlock()
		return vocabulary.Result{}, nil
	}}
	store := &advancingConflictRangeStore{RunStore: NewMemoryRunStore(), conflicts: maxCASRetries}
	scheduler := schedulerForTest(t, store, schedulerRegistry(t, tool), 1)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", ExecutionPlan{
		PlanID: "cas-limit", Source: "dynamic", Summary: "cas-limit", Direction: "image", Steps: []PlanStep{{ID: "a", Tool: "work", Required: true}},
	})
	if !errors.Is(err, ErrRunVersionConflict) || calls.Load() != 0 || run.Nodes["a"].Status != NodeStatusPending || run.Nodes["a"].Attempt != 0 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	if run.Version != 1+maxCASRetries {
		t.Fatalf("version=%d", run.Version)
	}
	for conflict := 1; conflict <= maxCASRetries; conflict++ {
		if !strings.Contains(run.Plan.Summary, fmt.Sprintf("|conflict-%d", conflict)) {
			t.Fatalf("missing conflict update %d in %q", conflict, run.Plan.Summary)
		}
	}
	recovered, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || recovered.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("recovered=%s calls=%d err=%v", recovered.Status, calls.Load(), err)
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 1 || keys[0] != "run-1:a:1" {
		t.Fatalf("keys=%v", keys)
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
	cfg := schedulerConfigForTest(NewMemoryRunStore(), registry, func() string { return "missing-ref" })
	cfg.MaxParallel = 1
	scheduler, err := NewScheduler(cfg)
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
	validConfig := schedulerConfigForTest(NewMemoryRunStore(), registry, newID)
	nilStore := validConfig
	nilStore.Store = nil
	nilVocabulary := validConfig
	nilVocabulary.Vocabulary = nil
	nilNewID := validConfig
	nilNewID.NewID = nil
	nilOwner := validConfig
	nilOwner.OwnerID = ""
	nilClock := validConfig
	nilClock.Now = nil
	nilToken := validConfig
	nilToken.NewToken = nil
	for name, cfg := range map[string]SchedulerConfig{
		"nil_store": nilStore, "nil_vocabulary": nilVocabulary, "nil_new_id": nilNewID,
		"nil_owner": nilOwner, "nil_clock": nilClock, "nil_token": nilToken,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewScheduler(cfg); err == nil {
				t.Fatal("expected config error")
			}
		})
	}
	scheduler, err := NewScheduler(validConfig)
	if err != nil {
		t.Fatal(err)
	}
	if scheduler.maxParallel != defaultMaxParallel || scheduler.jobBudget != 0 || scheduler.commitTimeout != defaultCommitTimeout || scheduler.leaseTTL != defaultLeaseTTL || scheduler.heartbeatInterval != defaultLeaseTTL/3 {
		t.Fatalf("defaults: maxParallel=%d jobBudget=%d commitTimeout=%s", scheduler.maxParallel, scheduler.jobBudget, scheduler.commitTimeout)
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
	emptyIDConfig := schedulerConfigForTest(NewMemoryRunStore(), registry, func() string { return "" })
	emptyID, err := NewScheduler(emptyIDConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emptyID.Submit(context.Background(), "session", "user", valid); err == nil {
		t.Fatal("empty generated run id must fail")
	}
}

func runWaitingForBatch(t *testing.T, required bool) (*Scheduler, PlanRun) {
	t.Helper()
	dispatch := schedulerTool{key: "dispatch", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{
			Outputs:    map[string]any{"operation_id": "operation-1", "batch_id": "batch-1", "job_ids": []string{"job-1"}},
			Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": "batch-1", "job_ids": []string{"job-1"}}},
		}, nil
	}}
	after := schedulerTool{key: "after"}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, dispatch, after), 1)
	run, err := scheduler.Submit(context.Background(), "session-1", "user-1", ExecutionPlan{
		PlanID: "jobs-wait", Source: "dynamic", Summary: "jobs wait", Direction: "image",
		Steps: []PlanStep{
			{ID: "dispatch", Tool: "dispatch", Required: required},
			{ID: "after", Tool: "after", DependsOn: []string{"dispatch"}, Required: true},
		},
	})
	if err != nil || run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingJobs {
		t.Fatalf("suspended=%+v err=%v", run, err)
	}
	return scheduler, run
}

func TestCompleteJobsWaitResumesExactlyOnce(t *testing.T) {
	scheduler, suspended := runWaitingForBatch(t, true)
	completed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, suspended.SuspendedNodeID, JobsOutcome{
		BatchID: "batch-1", Status: generation.BatchStatusCompleted, Summary: map[string]any{"assets": []any{"asset-1"}},
	})
	if err != nil || completed.Status != RunStatusSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	node := completed.Nodes["dispatch"]
	if node.Status != NodeStatusSucceeded || node.Suspension != nil || node.Outputs["jobs_outcome_receipt"] != `{"batch_id":"batch-1","status":"completed"}` || !reflect.DeepEqual(node.Outputs["jobs_summary"], map[string]any{"assets": []any{"asset-1"}}) {
		t.Fatalf("completed node=%+v", node)
	}

	replayed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, suspended.SuspendedNodeID, JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusCompleted})
	if err != nil || replayed.Version != completed.Version {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	if !reflect.DeepEqual(replayed.Nodes["dispatch"].Outputs["jobs_summary"], node.Outputs["jobs_summary"]) {
		t.Fatalf("replay rewrote summary: %+v", replayed.Nodes["dispatch"].Outputs)
	}
}

func TestCompleteJobsWaitRejectsMismatchesWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		nodeID  string
		outcome JobsOutcome
		wantErr error
	}{
		{name: "batch", nodeID: "dispatch", outcome: JobsOutcome{BatchID: "batch-other", Status: generation.BatchStatusCompleted}, wantErr: ErrJobsWaitMismatch},
		{name: "node", nodeID: "after", outcome: JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusCompleted}, wantErr: ErrJobsWaitMismatch},
		{name: "status", nodeID: "dispatch", outcome: JobsOutcome{BatchID: "batch-1", Status: "waiting_jobs"}, wantErr: ErrJobsOutcomeInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler, suspended := runWaitingForBatch(t, true)
			got, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, test.nodeID, test.outcome)
			if !errors.Is(err, test.wantErr) || got.Version != suspended.Version {
				t.Fatalf("run=%+v err=%v", got, err)
			}
			stored, getErr := scheduler.store.GetRun(context.Background(), suspended.ID)
			if getErr != nil || stored.Version != suspended.Version {
				t.Fatalf("stored=%+v err=%v", stored, getErr)
			}
		})
	}
}

func TestCompleteJobsWaitMapsTerminalBatchStatuses(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		required    bool
		wantRun     string
		wantNode    string
		failureCode string
	}{
		{name: "required partial", status: generation.BatchStatusPartialFailed, required: true, wantRun: RunStatusFailed, wantNode: NodeStatusFailed, failureCode: "generation_partial_failed"},
		{name: "optional partial", status: generation.BatchStatusPartialFailed, required: false, wantRun: RunStatusSucceeded, wantNode: NodeStatusSucceeded},
		{name: "failed", status: generation.BatchStatusFailed, required: true, wantRun: RunStatusFailed, wantNode: NodeStatusFailed, failureCode: "generation_failed"},
		{name: "cancelled", status: generation.BatchStatusCancelled, required: true, wantRun: RunStatusFailed, wantNode: NodeStatusFailed, failureCode: "generation_cancelled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler, suspended := runWaitingForBatch(t, test.required)
			completed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{BatchID: "batch-1", Status: test.status})
			if err != nil || completed.Status != test.wantRun || completed.Nodes["dispatch"].Status != test.wantNode {
				t.Fatalf("completed=%+v err=%v", completed, err)
			}
			failure := completed.Nodes["dispatch"].Fail
			if test.failureCode == "" && failure != nil || test.failureCode != "" && (failure == nil || failure.Code != test.failureCode) {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}

func TestCompleteJobsWaitRejectsConflictingReplay(t *testing.T) {
	scheduler, suspended := runWaitingForBatch(t, true)
	completed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	conflicted, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusFailed})
	if !errors.Is(err, ErrJobsOutcomeConflict) || conflicted.Version != completed.Version {
		t.Fatalf("conflicted=%+v err=%v", conflicted, err)
	}
}

func TestCompleteJobsWaitPreservesLargeSummaryNumbers(t *testing.T) {
	scheduler, suspended := runWaitingForBatch(t, true)
	completed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{
		BatchID: "batch-1", Status: generation.BatchStatusCompleted,
		Summary: map[string]any{"provider_sequence": int64(9007199254740993)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := completed.Nodes["dispatch"].Outputs["jobs_summary"].(map[string]any)["provider_sequence"].(json.Number)
	if !ok || got.String() != "9007199254740993" {
		t.Fatalf("provider_sequence lost precision: %#v", completed.Nodes["dispatch"].Outputs["jobs_summary"])
	}
}

func TestCompleteJobsWaitReplayIgnoresNonIdentitySummary(t *testing.T) {
	scheduler, suspended := runWaitingForBatch(t, true)
	completed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{
		BatchID: "batch-1", Status: generation.BatchStatusCompleted, Summary: map[string]any{"asset": "asset-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := scheduler.CompleteJobsWait(context.Background(), suspended.ID, "dispatch", JobsOutcome{
		BatchID: "batch-1", Status: generation.BatchStatusCompleted, Summary: map[string]any{"ignored": func() {}},
	})
	if err != nil || replayed.Version != completed.Version || !reflect.DeepEqual(replayed.Nodes["dispatch"].Outputs["jobs_summary"], completed.Nodes["dispatch"].Outputs["jobs_summary"]) {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}

type countingGenerationDispatcher struct{ calls atomic.Int32 }

func (d *countingGenerationDispatcher) Dispatch(context.Context, vocabulary.GenerationDispatchRequest) (vocabulary.GenerationDispatchResult, error) {
	d.calls.Add(1)
	return vocabulary.GenerationDispatchResult{BatchID: "must-not-dispatch"}, nil
}

func TestSchedulerTreatsInvalidGenerationTargetAsBusinessFailure(t *testing.T) {
	dispatcher := &countingGenerationDispatcher{}
	tool := vocabulary.NewDispatchGenerationTool(dispatcher)
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, tool), 1)
	run, err := scheduler.Submit(context.Background(), "session-1", "user-1", ExecutionPlan{
		PlanID: "invalid-generation", Source: "dynamic", Summary: "invalid", Direction: "image",
		Steps: []PlanStep{{ID: "dispatch", Tool: "dispatch_generation", Params: map[string]any{
			"targets": []any{map[string]any{"media_kind": "hologram"}},
		}, Required: true}},
	})
	if err != nil || run.Status != RunStatusFailed || run.Nodes["dispatch"].Status != NodeStatusFailed || run.Nodes["dispatch"].Fail == nil || run.Nodes["dispatch"].Fail.Code != "invalid_request" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if dispatcher.calls.Load() != 0 {
		t.Fatalf("dispatcher calls=%d", dispatcher.calls.Load())
	}
}

type jobsCompletionErrorStore struct {
	RunStore
	entered chan struct{}
	release chan struct{}
	err     error
}

func (s *jobsCompletionErrorStore) MutateRun(context.Context, string, int, func(*PlanRun) error) (PlanRun, error) {
	close(s.entered)
	<-s.release
	return PlanRun{}, s.err
}

func TestCompleteJobsWaitCancellationJoinsCommitErrorWithoutReceipt(t *testing.T) {
	baseScheduler, suspended := runWaitingForBatch(t, true)
	cause := errors.New("database write failed")
	store := &jobsCompletionErrorStore{RunStore: baseScheduler.store, entered: make(chan struct{}), release: make(chan struct{}), err: cause}
	scheduler := schedulerForTest(t, store, baseScheduler.vocabulary, 1)
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		run PlanRun
		err error
	}
	done := make(chan result, 1)
	go func() {
		run, err := scheduler.CompleteJobsWait(ctx, suspended.ID, "dispatch", JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusCompleted})
		done <- result{run: run, err: err}
	}()
	<-store.entered
	cancel()
	close(store.release)
	got := <-done
	if !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, cause) || got.run.Version != suspended.Version {
		t.Fatalf("run=%+v err=%v", got.run, got.err)
	}
	stored, err := baseScheduler.store.GetRun(context.Background(), suspended.ID)
	if err != nil || stored.Version != suspended.Version || stored.Nodes["dispatch"].Outputs[jobsOutcomeReceiptKey] != nil {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

type jobsConflictReloadErrorStore struct {
	RunStore
	gets    atomic.Int32
	entered chan struct{}
	release chan struct{}
	err     error
}

func (s *jobsConflictReloadErrorStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	if s.gets.Add(1) == 1 {
		return s.RunStore.GetRun(ctx, id)
	}
	return PlanRun{}, s.err
}

func (s *jobsConflictReloadErrorStore) MutateRun(context.Context, string, int, func(*PlanRun) error) (PlanRun, error) {
	close(s.entered)
	<-s.release
	return PlanRun{}, ErrRunVersionConflict
}

func TestCompleteJobsWaitCancellationJoinsConflictReloadError(t *testing.T) {
	baseScheduler, suspended := runWaitingForBatch(t, true)
	cause := errors.New("database read failed")
	store := &jobsConflictReloadErrorStore{RunStore: baseScheduler.store, entered: make(chan struct{}), release: make(chan struct{}), err: cause}
	scheduler := schedulerForTest(t, store, baseScheduler.vocabulary, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := scheduler.CompleteJobsWait(ctx, suspended.ID, "dispatch", JobsOutcome{BatchID: "batch-1", Status: generation.BatchStatusCompleted})
		done <- err
	}()
	<-store.entered
	cancel()
	close(store.release)
	err := <-done
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("err=%v", err)
	}
}
