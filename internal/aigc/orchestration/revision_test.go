package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func TestReviseSkipsPendingAndAppendsSteps(t *testing.T) {
	store := NewMemoryRunStore()
	registry := testVocabulary(t)
	scheduler := schedulerForTest(t, store, registry, 1)
	plan := validPlan()
	run := createPendingSchedulerRun(t, store, "revise-basic", plan)

	revised, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{
		SkipStepIDs: []string{"generate"},
		AppendSteps: []PlanStep{{
			ID: "replacement", Tool: "dispatch_generation",
			Params:    map[string]any{"targets": []any{"$prompt.prompt"}},
			DependsOn: []string{"confirm"}, Required: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Nodes["generate"].Status != NodeStatusSkipped {
		t.Fatalf("generate=%s", revised.Nodes["generate"].Status)
	}
	if revised.Nodes["generate"].SkipReason != SkipReasonRevision {
		t.Fatalf("generate skip_reason=%q", revised.Nodes["generate"].SkipReason)
	}
	if node := revised.Nodes["replacement"]; node == nil || node.Status != NodeStatusPending || node.Attempt != 0 {
		t.Fatalf("replacement=%+v", node)
	}
	if revised.Version != run.Version+1 {
		t.Fatalf("version=%d want=%d", revised.Version, run.Version+1)
	}
	if err := revised.Plan.Validate(registry, scheduler.jobBudget); err != nil {
		t.Fatalf("revised plan invalid: %v", err)
	}
}

func TestReviseTerminalStatusTreatsRevisionSkippedRequiredAsAcceptedGap(t *testing.T) {
	tests := []struct {
		name  string
		steps []PlanStep
		nodes map[string]*NodeRun
		want  string
	}{
		{
			name: "replacement_succeeded",
			steps: []PlanStep{
				{ID: "old", Required: true},
				{ID: "replacement", Required: true},
			},
			nodes: map[string]*NodeRun{
				"old":         {StepID: "old", Status: NodeStatusSkipped, SkipReason: SkipReasonRevision},
				"replacement": {StepID: "replacement", Status: NodeStatusSucceeded},
			},
			want: RunStatusPartialSucceeded,
		},
		{
			name:  "all_required_revision_skipped",
			steps: []PlanStep{{ID: "a", Required: true}, {ID: "b", Required: true}},
			nodes: map[string]*NodeRun{
				"a": {StepID: "a", Status: NodeStatusSkipped, SkipReason: SkipReasonRevision},
				"b": {StepID: "b", Status: NodeStatusSkipped, SkipReason: SkipReasonRevision},
			},
			want: RunStatusPartialSucceeded,
		},
		{
			name:  "non_revision_required_skip_fails",
			steps: []PlanStep{{ID: "required", Required: true}},
			nodes: map[string]*NodeRun{
				"required": {StepID: "required", Status: NodeStatusSkipped},
			},
			want: RunStatusFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := terminalStatus(PlanRun{Plan: ExecutionPlan{Steps: test.steps}, Nodes: test.nodes})
			if got != test.want {
				t.Fatalf("status=%s want=%s", got, test.want)
			}
		})
	}
}

func TestReviseSchedulerDependencySkipHasNoRevisionReason(t *testing.T) {
	fail := schedulerTool{key: "fail", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "expected", Message: "expected"}}, nil
	}}
	sink := schedulerTool{key: "sink"}
	scheduler := schedulerForTest(t, NewMemoryRunStore(), schedulerRegistry(t, fail, sink), 1)
	run, err := scheduler.Submit(context.Background(), "s", "u", ExecutionPlan{
		PlanID: "dependency-skip", Source: "dynamic", Summary: "dependency", Direction: "image",
		Steps: []PlanStep{
			{ID: "failed", Tool: "fail", Required: true},
			{ID: "blocked", Tool: "sink", DependsOn: []string{"failed"}, Required: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusFailed || run.Nodes["blocked"].Status != NodeStatusSkipped || run.Nodes["blocked"].SkipReason != "" {
		t.Fatalf("run=%+v blocked=%+v", run, run.Nodes["blocked"])
	}
}

func TestRevisePreservesLargeJSONNumbersAndDetectsAdjacentDefinition(t *testing.T) {
	store := NewMemoryRunStore()
	registry := testVocabulary(t)
	scheduler := schedulerForTest(t, store, registry, 1)
	plan := validPlan()
	plan.Steps[0].Params["large"] = json.Number("9007199254740992")
	run := createPendingSchedulerRun(t, store, "revise-large-number", plan)
	storedNumber, ok := run.Plan.Steps[0].Params["large"].(json.Number)
	if !ok || storedNumber.String() != "9007199254740992" {
		t.Fatalf("stored number=%T(%v)", run.Plan.Steps[0].Params["large"], run.Plan.Steps[0].Params["large"])
	}

	different := plan.Steps[0]
	different.Params = map[string]any{
		"target_desc": "雨中柴犬",
		"large":       json.Number("9007199254740993"),
	}
	_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{AppendSteps: []PlanStep{different}})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("err=%v", err)
	}
	after, getErr := store.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if after.Version != run.Version || after.Plan.Steps[0].Params["large"].(json.Number).String() != "9007199254740992" {
		t.Fatalf("large number silently changed: %+v", after.Plan.Steps[0].Params)
	}
}

func TestReviseSerializationErrorsWrapUnderlyingJSONError(t *testing.T) {
	badStep := PlanStep{ID: "bad", Tool: "request_confirmation", Params: map[string]any{"question": "x", "bad": func() {}}}
	for name, clone := range map[string]func() error{
		"revision": func() error {
			_, err := clonePlanRevision(PlanRevision{AppendSteps: []PlanStep{badStep}})
			return err
		},
		"run": func() error {
			_, err := clonePlanRun(PlanRun{Plan: ExecutionPlan{Steps: []PlanStep{badStep}}})
			return err
		},
		"canonical_step": func() error {
			_, err := planStepsEqual(badStep, badStep)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := clone()
			var unsupported *json.UnsupportedTypeError
			if !errors.Is(err, ErrRunNotSerializable) || !errors.As(err, &unsupported) {
				t.Fatalf("error chain=%v", err)
			}
		})
	}
}

func TestReviseJSONDecoderRejectsTrailingValue(t *testing.T) {
	var target map[string]any
	err := decodeSingleJSONValue([]byte(`{"first":1} {"second":2}`), &target)
	if err == nil {
		t.Fatal("decoder accepted a trailing JSON value")
	}
}

func TestReviseRejectsExecutedStepAtomically(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	plan := validPlan()
	run := createPendingSchedulerRun(t, store, "revise-executed", plan)
	run, err := store.MutateRun(context.Background(), run.ID, run.Version, func(next *PlanRun) error {
		next.Nodes["prompt"].Status = NodeStatusSucceeded
		next.Nodes["prompt"].Attempt = 1
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = scheduler.Revise(context.Background(), run.ID, PlanRevision{
		SkipStepIDs: []string{"prompt"},
		AppendSteps: []PlanStep{{ID: "leak", Tool: "request_confirmation", Params: map[string]any{"question": "leak"}}},
	})
	if !errors.Is(err, ErrReviseExecutedStep) {
		t.Fatalf("err=%v", err)
	}
	after, getErr := store.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if after.Version != run.Version || after.Nodes["prompt"].Status != NodeStatusSucceeded || after.Nodes["leak"] != nil {
		t.Fatalf("mutation leaked: %+v", after)
	}
}

func TestReviseRejectsEveryNonPendingStepAndUnknownStepAtomically(t *testing.T) {
	for _, status := range []string{NodeStatusSucceeded, NodeStatusFailed, NodeStatusSkipped, NodeStatusRunning} {
		t.Run(status, func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
			run := createPendingSchedulerRun(t, store, "revise-status-"+status, validPlan())
			run, err := store.MutateRun(context.Background(), run.ID, run.Version, func(next *PlanRun) error {
				next.Nodes["confirm"].Status = status
				if status == NodeStatusRunning {
					next.Status = RunStatusSuspended
					next.SuspendReason = SuspendWaitingUser
					next.SuspendedNodeID = "confirm"
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = scheduler.Revise(context.Background(), run.ID, PlanRevision{
				SkipStepIDs: []string{"confirm"},
				AppendSteps: []PlanStep{{ID: "must-not-leak", Tool: "request_confirmation", Params: map[string]any{"question": "x"}}},
			})
			if !errors.Is(err, ErrReviseExecutedStep) || !containsAll(err.Error(), run.ID, "confirm", status) {
				t.Fatalf("err=%v", err)
			}
			after, _ := store.GetRun(context.Background(), run.ID)
			if after.Version != run.Version || after.Nodes["must-not-leak"] != nil {
				t.Fatalf("mutation leaked: version=%d nodes=%v", after.Version, after.Nodes)
			}
		})
	}

	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-unknown", validPlan())
	_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{SkipStepIDs: []string{"ghost"}})
	if !errors.Is(err, ErrPlanInvalid) || !containsAll(err.Error(), run.ID, "ghost") {
		t.Fatalf("err=%v", err)
	}
	after, _ := store.GetRun(context.Background(), run.ID)
	if after.Version != run.Version {
		t.Fatalf("version=%d want=%d", after.Version, run.Version)
	}
}

func TestReviseRejectsInvalidAppendAtomically(t *testing.T) {
	tests := []struct {
		name   string
		append []PlanStep
		want   error
	}{
		{name: "empty_id", append: []PlanStep{{Tool: "request_confirmation", Params: map[string]any{"question": "x"}}}, want: ErrPlanInvalid},
		{name: "duplicate_batch_id", append: []PlanStep{
			{ID: "dup", Tool: "request_confirmation", Params: map[string]any{"question": "x"}},
			{ID: "dup", Tool: "request_confirmation", Params: map[string]any{"question": "x"}},
		}, want: ErrRevisionConflict},
		{name: "different_existing_definition", append: []PlanStep{{ID: "prompt", Tool: "request_confirmation", Params: map[string]any{"question": "x"}}}, want: ErrRevisionConflict},
		{name: "unknown_tool", append: []PlanStep{{ID: "bad", Tool: "ghost"}}, want: ErrPlanInvalid},
		{name: "unknown_dependency", append: []PlanStep{{ID: "bad-dep", Tool: "request_confirmation", Params: map[string]any{"question": "x"}, DependsOn: []string{"ghost"}}}, want: ErrPlanInvalid},
		{name: "cycle", append: []PlanStep{
			{ID: "cycle-a", Tool: "request_confirmation", Params: map[string]any{"question": "x"}, DependsOn: []string{"cycle-b"}},
			{ID: "cycle-b", Tool: "request_confirmation", Params: map[string]any{"question": "x"}, DependsOn: []string{"cycle-a"}},
		}, want: ErrPlanInvalid},
		{name: "bad_reference", append: []PlanStep{{ID: "bad-ref", Tool: "request_confirmation", Params: map[string]any{"question": "$ghost.value"}}}, want: ErrPlanInvalid},
		{name: "raw_slot", append: []PlanStep{{ID: "slot", Tool: "request_confirmation", Params: map[string]any{"question": "<QUESTION>"}}}, want: ErrPlanInvalid},
		{name: "reserved_expand", append: []PlanStep{{ID: "expand", Tool: "request_confirmation", Params: map[string]any{"question": "x"}, Expand: &ExpandSpec{Over: "items"}}}, want: ErrPlanInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
			run := createPendingSchedulerRun(t, store, "revise-invalid-"+test.name, validPlan())
			_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{
				SkipStepIDs: []string{"generate"}, AppendSteps: test.append,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want errors.Is %v", err, test.want)
			}
			after, _ := store.GetRun(context.Background(), run.ID)
			if after.Version != run.Version || after.Nodes["generate"].Status != NodeStatusPending || len(after.Plan.Steps) != len(run.Plan.Steps) {
				t.Fatalf("invalid revision leaked: %+v", after)
			}
		})
	}
}

func TestReviseClonesNestedAppendParams(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-clone", validPlan())
	params := map[string]any{"question": "keep", "nested": map[string]any{"items": []any{"stored"}}}
	revision := PlanRevision{AppendSteps: []PlanStep{{ID: "clone", Tool: "request_confirmation", Params: params}}}
	revised, err := scheduler.Revise(context.Background(), run.ID, revision)
	if err != nil {
		t.Fatal(err)
	}
	params["question"] = "changed"
	params["nested"].(map[string]any)["items"].([]any)[0] = "changed"
	revision.AppendSteps[0].Params["added"] = true
	revised.Plan.Steps[len(revised.Plan.Steps)-1].Params["question"] = "changed result"

	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	step, _ := findPlanStep(stored.Plan.Steps, "clone")
	if step.Params["question"] != "keep" || step.Params["nested"].(map[string]any)["items"].([]any)[0] != "stored" {
		t.Fatalf("stored params aliased caller: %#v", step.Params)
	}
	if _, exists := step.Params["added"]; exists {
		t.Fatal("stored params retained revision alias")
	}
}

func TestReviseEmptyIsReadOnlyAndRejectsNonRevisableRuns(t *testing.T) {
	for _, status := range []string{RunStatusRunning, RunStatusSuspended} {
		t.Run("empty_"+status, func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
			run := createPendingSchedulerRun(t, store, "revise-empty-"+status, validPlan())
			if status == RunStatusSuspended {
				var err error
				run, err = store.MutateRun(context.Background(), run.ID, run.Version, func(next *PlanRun) error {
					next.Status = RunStatusSuspended
					next.SuspendReason = SuspendWaitingAgent
					next.SuspendedNodeID = "prompt"
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{})
			if err != nil || got.Version != run.Version {
				t.Fatalf("version=%d want=%d err=%v", got.Version, run.Version, err)
			}
		})
	}

	for _, status := range []string{RunStatusDraft, RunStatusSucceeded, RunStatusPartialSucceeded, RunStatusFailed, RunStatusCancelled} {
		t.Run("reject_"+status, func(t *testing.T) {
			store := NewMemoryRunStore()
			run, err := store.CreateRun(context.Background(), PlanRun{ID: "non-revisable-" + status, Status: status, Plan: validPlan(), Nodes: pendingNodes(validPlan())})
			if err != nil {
				t.Fatal(err)
			}
			scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
			_, err = scheduler.Revise(context.Background(), run.ID, PlanRevision{})
			if !errors.Is(err, ErrRunNotRevisable) {
				t.Fatalf("status=%s err=%v", status, err)
			}
		})
	}
}

func TestReviseSuspendedRunPreservesCheckpointAndBudgetPreview(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprintf("preview_%t", preview), func(t *testing.T) {
			store := NewMemoryRunStore()
			scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
			plan := validPlan()
			plan.EstimatedJobs = 99
			run := PlanRun{
				ID: "revise-suspended", SessionID: "s", UserID: "u", Plan: plan,
				Status: RunStatusSuspended, SuspendReason: SuspendWaitingUser,
				SuspendedNodeID: "", PreviewRequired: preview, ResumeKey: "receipt-key",
				Resumed: true, ResumeDecision: map[string]any{"choice": "keep"}, Nodes: pendingNodes(plan),
			}
			if !preview {
				run.SuspendedNodeID = "confirm"
				run.Nodes["confirm"].Status = NodeStatusRunning
				run.Nodes["confirm"].Attempt = 2
				run.Nodes["confirm"].ResumeKey = "node-key"
				run.Nodes["confirm"].Resumed = false
			}
			created, err := store.CreateRun(context.Background(), run)
			if err != nil {
				t.Fatal(err)
			}
			revised, err := scheduler.Revise(context.Background(), created.ID, PlanRevision{
				SkipStepIDs: []string{"generate"},
				AppendSteps: []PlanStep{{ID: "later", Tool: "request_confirmation", Params: map[string]any{"question": "later"}, DependsOn: []string{"confirm"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if revised.Status != created.Status || revised.SuspendReason != created.SuspendReason || revised.SuspendedNodeID != created.SuspendedNodeID ||
				revised.PreviewRequired != created.PreviewRequired || revised.ResumeKey != created.ResumeKey || revised.Resumed != created.Resumed ||
				!reflect.DeepEqual(revised.ResumeDecision, created.ResumeDecision) || revised.Plan.EstimatedJobs != 99 {
				t.Fatalf("suspension changed: before=%+v after=%+v", created, revised)
			}
			if !preview && (revised.Nodes["confirm"].Status != NodeStatusRunning || revised.Nodes["confirm"].ResumeKey != "node-key") {
				t.Fatalf("node checkpoint changed: %+v", revised.Nodes["confirm"])
			}
		})
	}
}

func TestReviseReplacementResumesOnNewSchedulerToPartialSucceeded(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	var calls atomic.Int32
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	registry := schedulerRegistry(t, tool)
	newScheduler := func(owner string) *Scheduler {
		t.Helper()
		var token atomic.Int64
		scheduler, err := NewScheduler(SchedulerConfig{
			Store: withDeterministicStoreClock(store, clock.Now), Vocabulary: registry, MaxParallel: 1,
			CommitTimeout: time.Second, NewID: func() string { return owner + "-run" },
			OwnerID: owner, LeaseTTL: 30 * time.Second, HeartbeatInterval: time.Hour,
			Now: clock.Now, NewToken: func() string { return fmt.Sprintf("%s-token-%d", owner, token.Add(1)) },
		})
		if err != nil {
			t.Fatal(err)
		}
		return scheduler
	}
	first := newScheduler("revision-owner")
	plan := ExecutionPlan{
		PlanID: "replacement-preview", Source: "dynamic", Summary: "replacement", Direction: "image",
		Steps: []PlanStep{{ID: "old", Tool: "work", Required: true}},
	}
	created, err := store.CreateRun(context.Background(), PlanRun{
		ID: "replacement-preview", SessionID: "s", UserID: "u", Plan: plan,
		Status: RunStatusSuspended, SuspendReason: SuspendWaitingUser,
		PreviewRequired: true, ResumeKey: "preview-receipt", Nodes: pendingNodes(plan),
	})
	if err != nil {
		t.Fatal(err)
	}
	revised, err := first.Revise(context.Background(), created.ID, PlanRevision{
		SkipStepIDs: []string{"old"},
		AppendSteps: []PlanStep{{ID: "replacement", Tool: "work", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Status != RunStatusSuspended || revised.ResumeKey != created.ResumeKey || !revised.PreviewRequired || calls.Load() != 0 {
		t.Fatalf("revision crossed checkpoint: %+v calls=%d", revised, calls.Load())
	}

	second := newScheduler("resume-owner")
	decision := map[string]any{"approved": true}
	completed, err := second.Resume(context.Background(), revised.ID, revised.ResumeKey, decision)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != RunStatusPartialSucceeded || calls.Load() != 1 {
		t.Fatalf("status=%s calls=%d run=%+v", completed.Status, calls.Load(), completed)
	}
	if old := completed.Nodes["old"]; old.Status != NodeStatusSkipped || old.SkipReason != SkipReasonRevision {
		t.Fatalf("old=%+v", old)
	}
	if replacement := completed.Nodes["replacement"]; replacement.Status != NodeStatusSucceeded || replacement.ExecutionToken != "" {
		t.Fatalf("replacement=%+v", replacement)
	}
	if !completed.Resumed || completed.PreviewRequired || !reflect.DeepEqual(completed.ResumeDecision, decision) {
		t.Fatalf("resume receipt lost: %+v", completed)
	}
}

func TestExecutionClaimPreventsReviseAfterToolStarted(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newClaimClock(time.Unix(1_700_000_000, 0))
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	tool := schedulerTool{key: "work", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		started <- struct{}{}
		<-release
		return vocabulary.Result{Outputs: map[string]any{"committed": true}}, nil
	}}
	executor := claimScheduler(t, store, "execute-owner", clock, time.Hour, tool)
	reviser := claimScheduler(t, store, "revise-owner", clock, time.Hour, tool)
	run := createPendingSchedulerRun(t, store, "claim-vs-revise", oneClaimStep("work"))
	done := advanceAsync(executor, run.ID)
	<-started
	claimed, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if node := claimed.Nodes["effect"]; node.Status != NodeStatusRunning || node.ExecutionToken == "" || node.ExecutionOwner != "execute-owner" {
		t.Fatalf("claim not durable: %+v", node)
	}
	planBefore := claimed.Plan
	_, err = reviser.Revise(context.Background(), run.ID, PlanRevision{SkipStepIDs: []string{"effect"}})
	if !errors.Is(err, ErrReviseExecutedStep) {
		t.Fatalf("err=%v", err)
	}
	after, getErr := store.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if after.Version != claimed.Version || !reflect.DeepEqual(after.Plan, planBefore) || after.Nodes["effect"].Status != NodeStatusRunning {
		t.Fatalf("revision changed claimed run: before=%+v after=%+v", claimed, after)
	}
	close(release)
	result := <-done
	if result.err != nil || result.run.Status != RunStatusSucceeded || result.run.Nodes["effect"].Status != NodeStatusSucceeded {
		t.Fatalf("result=%+v err=%v", result.run, result.err)
	}
}

func TestReviseAppendCanDependOnSameBatchAndDuplicateSkipsAreDeduplicated(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-batch-deps", validPlan())
	revised, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{
		SkipStepIDs: []string{"generate", "generate"},
		AppendSteps: []PlanStep{
			{ID: "batch-a", Tool: "request_confirmation", Params: map[string]any{"question": "a"}, DependsOn: []string{"confirm"}},
			{ID: "batch-b", Tool: "request_confirmation", Params: map[string]any{"question": "$batch-a.ok"}, DependsOn: []string{"batch-a"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Status != RunStatusRunning || revised.Version != run.Version+1 || revised.Nodes["batch-a"].Attempt != 0 || revised.Nodes["batch-b"].Status != NodeStatusPending {
		t.Fatalf("revised=%+v", revised)
	}
}

func TestReviseConcurrentCallsOnSameSchedulerCommitOnce(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-local-concurrent", validPlan())
	revision := PlanRevision{SkipStepIDs: []string{"generate"}, AppendSteps: []PlanStep{{
		ID: "replacement", Tool: "request_confirmation", Params: map[string]any{"question": "x"}, DependsOn: []string{"confirm"},
	}}}
	const callers = 12
	start := make(chan struct{})
	results := make(chan struct {
		run PlanRun
		err error
	}, callers)
	for range callers {
		go func() {
			<-start
			got, err := scheduler.Revise(context.Background(), run.ID, revision)
			results <- struct {
				run PlanRun
				err error
			}{got, err}
		}()
	}
	close(start)
	for range callers {
		result := <-results
		if result.err != nil || result.run.Version != run.Version+1 {
			t.Fatalf("version=%d err=%v", result.run.Version, result.err)
		}
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != run.Version+1 {
		t.Fatalf("stored version=%d want=%d", stored.Version, run.Version+1)
	}
}

type blockingRevisionStore struct {
	base    *MemoryRunStore
	entered chan struct{}
	release chan struct{}
	blocked atomic.Bool
	active  atomic.Int32
	peak    atomic.Int32
}

func (s *blockingRevisionStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	return s.base.CreateRun(ctx, run)
}

func (s *blockingRevisionStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	return s.base.GetRun(ctx, id)
}

func (s *blockingRevisionStore) GetActiveRun(ctx context.Context, sessionID string) (PlanRun, error) {
	return s.base.GetActiveRun(ctx, sessionID)
}

func (s *blockingRevisionStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for old := s.peak.Load(); active > old && !s.peak.CompareAndSwap(old, active); old = s.peak.Load() {
	}
	if s.blocked.CompareAndSwap(false, true) {
		s.entered <- struct{}{}
		select {
		case <-ctx.Done():
			return PlanRun{}, ctx.Err()
		case <-s.release:
		}
	}
	return s.base.MutateRun(ctx, id, expectedVersion, mutate)
}

func (s *blockingRevisionStore) MutateRunAtAuthoritativeNow(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun, time.Time) error) (PlanRun, error) {
	return s.base.MutateRunAtAuthoritativeNow(ctx, id, expectedVersion, mutate)
}

func TestReviseConcurrentCallsOnSameSchedulerAreGateSerialized(t *testing.T) {
	store := &blockingRevisionStore{
		base: NewMemoryRunStore(), entered: make(chan struct{}, 1), release: make(chan struct{}),
	}
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-local-gate", validPlan())
	results := make(chan error, 2)
	go func() {
		_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{AppendSteps: []PlanStep{{
			ID: "first", Tool: "request_confirmation", Params: map[string]any{"question": "first"},
		}}})
		results <- err
	}()
	<-store.entered
	go func() {
		_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{AppendSteps: []PlanStep{{
			ID: "second", Tool: "request_confirmation", Params: map[string]any{"question": "second"},
		}}})
		results <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		scheduler.gateMu.Lock()
		gate := scheduler.gates[run.ID]
		waiterObserved := gate != nil && gate.refs == 2
		scheduler.gateMu.Unlock()
		if waiterObserved {
			break
		}
		if time.Now().After(deadline) {
			close(store.release)
			t.Fatal("second Revise did not enter the run gate")
		}
		runtime.Gosched()
	}
	if store.peak.Load() != 1 {
		t.Fatalf("concurrent Store mutations=%d", store.peak.Load())
	}
	close(store.release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != run.Version+2 || stored.Nodes["first"] == nil || stored.Nodes["second"] == nil {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestReviseAcrossSchedulersSameRevisionIsIdempotent(t *testing.T) {
	store := NewMemoryRunStore()
	registry := testVocabulary(t)
	left := schedulerForTest(t, store, registry, 1)
	right := schedulerForTest(t, store, registry, 1)
	run := createPendingSchedulerRun(t, store, "revise-cross-same", validPlan())
	revision := PlanRevision{SkipStepIDs: []string{"generate"}, AppendSteps: []PlanStep{{
		ID: "cross", Tool: "request_confirmation", Params: map[string]any{"question": "same"}, DependsOn: []string{"confirm"},
	}}}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, scheduler := range []*Scheduler{left, right} {
		go func() {
			<-start
			got, err := scheduler.Revise(context.Background(), run.ID, revision)
			if err == nil && got.Version != run.Version+1 {
				err = fmt.Errorf("version=%d", got.Version)
			}
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != run.Version+1 {
		t.Fatalf("stored version=%d want=%d", stored.Version, run.Version+1)
	}
}

func TestReviseAcrossSchedulersDifferentDefinitionConflicts(t *testing.T) {
	store := NewMemoryRunStore()
	registry := testVocabulary(t)
	run := createPendingSchedulerRun(t, store, "revise-cross-different", validPlan())
	start := make(chan struct{})
	results := make(chan error, 2)
	definitions := []string{"left", "right"}
	for index, scheduler := range []*Scheduler{schedulerForTest(t, store, registry, 1), schedulerForTest(t, store, registry, 1)} {
		definition := definitions[index]
		go func() {
			<-start
			_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{AppendSteps: []PlanStep{{
				ID: "contended", Tool: "request_confirmation", Params: map[string]any{"question": definition},
			}}})
			results <- err
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	stored, _ := store.GetRun(context.Background(), run.ID)
	if stored.Version != run.Version+1 {
		t.Fatalf("version=%d want=%d", stored.Version, run.Version+1)
	}
}

func TestReviseDifferentDefinitionWinsConflictAfterOriginalRevisionApplied(t *testing.T) {
	store := NewMemoryRunStore()
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-applied-conflict", validPlan())
	first := PlanRevision{SkipStepIDs: []string{"generate"}, AppendSteps: []PlanStep{{
		ID: "replacement", Tool: "request_confirmation", Params: map[string]any{"question": "first"}, DependsOn: []string{"confirm"},
	}}}
	committed, err := scheduler.Revise(context.Background(), run.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.AppendSteps[0].Params = map[string]any{"question": "different"}
	_, err = scheduler.Revise(context.Background(), run.ID, second)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("err=%v", err)
	}
	after, _ := store.GetRun(context.Background(), run.ID)
	if after.Version != committed.Version {
		t.Fatalf("version=%d want=%d", after.Version, committed.Version)
	}
}

type revisionConflictStore struct {
	base      *MemoryRunStore
	remaining atomic.Int32
}

func (s *revisionConflictStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	return s.base.CreateRun(ctx, run)
}

func (s *revisionConflictStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	return s.base.GetRun(ctx, id)
}

func (s *revisionConflictStore) GetActiveRun(ctx context.Context, sessionID string) (PlanRun, error) {
	return s.base.GetActiveRun(ctx, sessionID)
}

func (s *revisionConflictStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	for {
		remaining := s.remaining.Load()
		if remaining <= 0 || !s.remaining.CompareAndSwap(remaining, remaining-1) {
			if remaining <= 0 {
				return s.base.MutateRun(ctx, id, expectedVersion, mutate)
			}
			continue
		}
		advanced, err := s.base.MutateRun(ctx, id, expectedVersion, func(run *PlanRun) error {
			if run.ResumeDecision == nil {
				run.ResumeDecision = make(map[string]any)
			}
			run.ResumeDecision["concurrent"] = remaining
			run.Plan.EstimatedJobs++
			return nil
		})
		if err != nil {
			return PlanRun{}, err
		}
		return advanced, fmt.Errorf("%w: injected real concurrent progress", ErrRunVersionConflict)
	}
}

func (s *revisionConflictStore) MutateRunAtAuthoritativeNow(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun, time.Time) error) (PlanRun, error) {
	return s.base.MutateRunAtAuthoritativeNow(ctx, id, expectedVersion, mutate)
}

func TestReviseRetriesCASAgainstLatestRunAndPreservesConcurrentFields(t *testing.T) {
	base := NewMemoryRunStore()
	store := &revisionConflictStore{base: base}
	store.remaining.Store(3)
	scheduler := schedulerForTest(t, store, testVocabulary(t), 1)
	run := createPendingSchedulerRun(t, store, "revise-cas-progress", validPlan())
	revised, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{AppendSteps: []PlanStep{{
		ID: "after-conflicts", Tool: "request_confirmation", Params: map[string]any{"question": "x"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	concurrent, ok := revised.ResumeDecision["concurrent"].(json.Number)
	if revised.Version != run.Version+4 || revised.Plan.EstimatedJobs != run.Plan.EstimatedJobs+3 || !ok || concurrent.String() != "1" {
		t.Fatalf("concurrent progress lost: %+v", revised)
	}
}

func TestReviseGateWaitCanBeCancelled(t *testing.T) {
	started := make(chan struct{}, 1)
	releaseTool := make(chan struct{})
	tool := schedulerTool{key: "blocking", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		started <- struct{}{}
		<-releaseTool
		return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
	}}
	registry := schedulerRegistry(t, tool)
	store := NewMemoryRunStore()
	plan := ExecutionPlan{PlanID: "revise-gate", Source: "dynamic", Summary: "gate", Direction: "image", Steps: []PlanStep{{ID: "hold", Tool: "blocking", Required: true}}}
	createPendingSchedulerRun(t, store, "revise-gate", plan)
	scheduler := schedulerForTest(t, store, registry, 1)
	holderDone := make(chan error, 1)
	go func() {
		_, err := scheduler.Advance(context.Background(), "revise-gate")
		holderDone <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := scheduler.Revise(ctx, "revise-gate", PlanRevision{})
		waiterDone <- err
	}()
	cancel()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled Revise gate waiter did not return")
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

func pendingNodes(plan ExecutionPlan) map[string]*NodeRun {
	nodes := make(map[string]*NodeRun, len(plan.Steps))
	for _, step := range plan.Steps {
		nodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
	}
	return nodes
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
