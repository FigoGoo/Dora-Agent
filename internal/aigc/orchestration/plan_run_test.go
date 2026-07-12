package orchestration

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type mutableOutput struct {
	Labels map[string]string `json:"labels"`
	Items  []string          `json:"items"`
}

type cyclicOutput struct {
	Self *cyclicOutput `json:"self"`
}

func TestRunStatusTransitions(t *testing.T) {
	legal := [][2]string{
		{RunStatusDraft, RunStatusRunning},
		{RunStatusDraft, RunStatusSuspended},
		{RunStatusRunning, RunStatusSuspended},
		{RunStatusSuspended, RunStatusRunning},
		{RunStatusRunning, RunStatusSucceeded},
		{RunStatusRunning, RunStatusPartialSucceeded},
		{RunStatusRunning, RunStatusFailed},
		{RunStatusDraft, RunStatusCancelled},
		{RunStatusRunning, RunStatusCancelled},
		{RunStatusSuspended, RunStatusCancelled},
	}
	for _, pair := range legal {
		if err := ValidateRunTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s -> %s: %v", pair[0], pair[1], err)
		}
	}
	for _, status := range []string{
		RunStatusDraft,
		RunStatusRunning,
		RunStatusSuspended,
		RunStatusSucceeded,
		RunStatusPartialSucceeded,
		RunStatusFailed,
		RunStatusCancelled,
	} {
		if err := ValidateRunTransition(status, status); err != nil {
			t.Fatalf("%s -> %s must be idempotent: %v", status, status, err)
		}
	}
	for _, pair := range [][2]string{
		{RunStatusSucceeded, RunStatusRunning},
		{RunStatusPartialSucceeded, RunStatusRunning},
		{RunStatusFailed, RunStatusRunning},
		{RunStatusCancelled, RunStatusRunning},
		{RunStatusDraft, RunStatusSucceeded},
		{RunStatusSuspended, RunStatusSucceeded},
		{"unknown", RunStatusRunning},
		{RunStatusRunning, "unknown"},
	} {
		if err := ValidateRunTransition(pair[0], pair[1]); err == nil {
			t.Fatalf("%s -> %s must fail", pair[0], pair[1])
		}
	}
}

func TestMemoryRunStoreCASAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	original := PlanRun{
		ID: "run-1", SessionID: "session-1", UserID: "user-1",
		Plan: validPlan(), Status: RunStatusDraft,
		Nodes: map[string]*NodeRun{
			"prompt": {
				StepID: "prompt", Status: NodeStatusPending, SuspensionGeneration: 7,
				Outputs: map[string]any{"nested": map[string]any{"value": "stored"}},
				Fail:    &vocabulary.Failure{Code: "original"},
				Suspension: &vocabulary.Suspension{
					Reason:  SuspendWaitingUser,
					Payload: map[string]any{"options": []any{"yes", "no"}},
				},
			},
		},
	}
	created, err := store.CreateRun(ctx, original)
	if err != nil || created.Version != 1 {
		t.Fatalf("create: version=%d err=%v", created.Version, err)
	}

	// Create 的入参与返回值都不能与存储值共享任意嵌套可变数据。
	original.Plan.Steps[0].Params["target_desc"] = "changed input"
	original.Nodes["prompt"].Outputs["nested"].(map[string]any)["value"] = "changed input"
	created.Plan.Steps[0].Params["target_desc"] = "changed result"
	created.Nodes["prompt"].Fail.Code = "changed result"
	created.Nodes["prompt"].Suspension.Payload["options"].([]any)[0] = "changed result"
	got, err := store.GetRun(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan.Steps[0].Params["target_desc"] != "雨中柴犬" {
		t.Fatal("create aliases plan input or result")
	}
	if got.Nodes["prompt"].Outputs["nested"].(map[string]any)["value"] != "stored" || got.Nodes["prompt"].Fail.Code != "original" {
		t.Fatal("create aliases nested node input or result")
	}
	if got.Nodes["prompt"].Suspension.Payload["options"].([]any)[0] != "yes" {
		t.Fatal("create aliases nested suspension payload")
	}
	if got.Nodes["prompt"].SuspensionGeneration != 7 {
		t.Fatalf("suspension generation=%d", got.Nodes["prompt"].SuspensionGeneration)
	}

	updated, err := store.MutateRun(ctx, created.ID, 1, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil || updated.Version != 2 {
		t.Fatalf("mutate: version=%d err=%v", updated.Version, err)
	}
	if _, err := store.MutateRun(ctx, created.ID, 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err := store.MutateRun(ctx, created.ID, 2, func(run *PlanRun) error {
		run.Nodes["leak"] = &NodeRun{StepID: "leak"}
		return errors.New("abort")
	}); err == nil {
		t.Fatal("callback failure must abort")
	}
	if _, err := store.MutateRun(ctx, created.ID, 2, func(run *PlanRun) error {
		run.Status = RunStatusDraft
		run.Nodes["transition-leak"] = &NodeRun{StepID: "transition-leak"}
		return nil
	}); err == nil {
		t.Fatal("invalid status transition must abort")
	}
	got, _ = store.GetRun(ctx, created.ID)
	if got.Version != 2 {
		t.Fatalf("aborted mutations changed version to %d", got.Version)
	}
	if _, exists := got.Nodes["leak"]; exists {
		t.Fatal("aborted callback mutation leaked")
	}
	if _, exists := got.Nodes["transition-leak"]; exists {
		t.Fatal("aborted transition mutation leaked")
	}

	// Get 和 Mutate 返回值也必须是深拷贝。
	got.Nodes["external"] = &NodeRun{StepID: "external"}
	got.Plan.Steps[0].DependsOn = append(got.Plan.Steps[0].DependsOn, "external")
	updated.Nodes["updated-external"] = &NodeRun{StepID: "updated-external"}
	again, _ := store.GetRun(ctx, created.ID)
	if _, exists := again.Nodes["external"]; exists {
		t.Fatal("read result aliases store")
	}
	if _, exists := again.Nodes["updated-external"]; exists {
		t.Fatal("mutate result aliases store")
	}
	if len(again.Plan.Steps[0].DependsOn) != 0 {
		t.Fatal("read result aliases nested plan slices")
	}
}

func TestMemoryRunStoreConcurrentCAS(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	created, err := store.CreateRun(ctx, PlanRun{ID: "run-cas", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
				run.Status = RunStatusRunning
				return nil
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrRunVersionConflict):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected mutate error: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 15 {
		t.Fatalf("successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
}

func TestMemoryRunStoreTimedMutationUsesInjectedClock(t *testing.T) {
	wantNow := time.Unix(1_700_000_000, 123)
	store := NewMemoryRunStore(WithMemoryRunStoreClock(func() time.Time { return wantNow }))
	created, err := store.CreateRun(context.Background(), PlanRun{ID: "run-timed", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	var gotNow time.Time
	updated, err := store.MutateRunAtAuthoritativeNow(context.Background(), created.ID, created.Version, func(run *PlanRun, now time.Time) error {
		gotNow = now
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil || updated.Version != 2 || !gotNow.Equal(wantNow) {
		t.Fatalf("updated=%+v now=%v err=%v", updated, gotNow, err)
	}
}

func TestMemoryRunStoreDuplicateAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	if _, err := store.CreateRun(ctx, PlanRun{ID: "run-1", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRun(ctx, PlanRun{ID: "run-1", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}}); err == nil {
		t.Fatal("duplicate run id must fail")
	}
	if _, err := store.GetRun(ctx, "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if _, err := store.MutateRun(ctx, "missing", 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("mutate missing: %v", err)
	}
}

func TestMemoryRunStoreMutationDoesNotRetainCallbackValues(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	created, err := store.CreateRun(ctx, PlanRun{ID: "run-callback", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}

	external := map[string]any{"items": []any{"stored"}}
	if _, err := store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		run.Nodes["node"] = &NodeRun{
			StepID:  "node",
			Status:  NodeStatusSucceeded,
			Outputs: map[string]any{"payload": external},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	external["added"] = true
	external["items"].([]any)[0] = "changed"
	got, err := store.GetRun(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload := got.Nodes["node"].Outputs["payload"].(map[string]any)
	if _, exists := payload["added"]; exists || payload["items"].([]any)[0] != "stored" {
		t.Fatal("callback-owned values alias stored run")
	}
}

func TestMemoryRunStoreClonesStructFieldsThroughStorageFormat(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	output := mutableOutput{
		Labels: map[string]string{"quality": "original"},
		Items:  []string{"original"},
	}
	created, err := store.CreateRun(ctx, PlanRun{
		ID: "run-struct", Status: RunStatusDraft,
		Nodes: map[string]*NodeRun{
			"node": {StepID: "node", Status: NodeStatusSucceeded, Outputs: map[string]any{"value": output}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	output.Labels["quality"] = "changed"
	output.Items[0] = "changed"
	got, err := store.GetRun(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := got.Nodes["node"].Outputs["value"].(map[string]any)
	if !ok {
		t.Fatalf("stored output type = %T, want JSON object", got.Nodes["node"].Outputs["value"])
	}
	labels := value["labels"].(map[string]any)
	items := value["items"].([]any)
	if labels["quality"] != "original" || items[0] != "original" {
		t.Fatal("mutable struct fields alias stored run")
	}
}

func TestMemoryRunStoreRejectsUnserializableValuesAtomically(t *testing.T) {
	ctx := context.Background()
	for name, value := range map[string]any{
		"function": func() {},
		"channel":  make(chan struct{}),
	} {
		t.Run("create_"+name, func(t *testing.T) {
			store := NewMemoryRunStore()
			_, err := store.CreateRun(ctx, PlanRun{
				ID: "run-invalid", Status: RunStatusDraft,
				Nodes: map[string]*NodeRun{"node": {Outputs: map[string]any{"invalid": value}}},
			})
			if !errors.Is(err, ErrRunNotSerializable) {
				t.Fatalf("create error = %v, want ErrRunNotSerializable", err)
			}
			if _, err := store.GetRun(ctx, "run-invalid"); !errors.Is(err, ErrRunNotFound) {
				t.Fatalf("failed create left a record: %v", err)
			}
		})
	}

	store := NewMemoryRunStore()
	created, err := store.CreateRun(ctx, PlanRun{ID: "run-mutate-invalid", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		run.Nodes["invalid"] = &NodeRun{Outputs: map[string]any{"fn": func() {}}}
		return nil
	})
	if !errors.Is(err, ErrRunNotSerializable) {
		t.Fatalf("mutate error = %v, want ErrRunNotSerializable", err)
	}
	got, err := store.GetRun(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != created.Version || got.Status != RunStatusDraft || len(got.Nodes) != 0 {
		t.Fatalf("failed mutation leaked: version=%d status=%s nodes=%v", got.Version, got.Status, got.Nodes)
	}
}

func TestMemoryRunStoreRejectsCyclicValues(t *testing.T) {
	const helperEnv = "DORA_TEST_CYCLIC_PLAN_RUN"
	if os.Getenv(helperEnv) == "1" {
		cyclicMap := map[string]any{}
		cyclicMap["self"] = cyclicMap
		cyclicPointer := &cyclicOutput{}
		cyclicPointer.Self = cyclicPointer
		for name, value := range map[string]any{"map": cyclicMap, "pointer": cyclicPointer} {
			store := NewMemoryRunStore()
			_, err := store.CreateRun(context.Background(), PlanRun{
				ID: "run-cyclic-" + name, Status: RunStatusDraft,
				Nodes: map[string]*NodeRun{"node": {Outputs: map[string]any{"value": value}}},
			})
			if !errors.Is(err, ErrRunNotSerializable) {
				t.Fatalf("%s create error = %v, want ErrRunNotSerializable", name, err)
			}
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemoryRunStoreRejectsCyclicValues$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("cyclic value handling exceeded timeout: %v", ctx.Err())
		}
		t.Fatalf("cyclic value helper failed: %v", err)
	}
}
