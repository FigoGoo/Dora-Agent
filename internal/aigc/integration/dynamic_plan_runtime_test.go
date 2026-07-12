package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/orchestration"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

const acceptancePrompt = "cinematic rainy city at night"

type acceptancePromptWriter struct {
	mu                 sync.Mutex
	calls              int
	targetDescriptions []string
}

func (w *acceptancePromptWriter) WritePrompt(_ context.Context, inputs map[string]any) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	target, _ := inputs["target_desc"].(string)
	w.targetDescriptions = append(w.targetDescriptions, target)
	return acceptancePrompt, nil
}

func (w *acceptancePromptWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func (w *acceptancePromptWriter) targetDescription(t *testing.T) string {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.targetDescriptions) != 1 {
		t.Fatalf("prompt writer inputs = %d, want 1", len(w.targetDescriptions))
	}
	return w.targetDescriptions[0]
}

type countingWorkflowStore struct {
	generation.WorkflowStore
	mu          sync.Mutex
	createCalls int
}

func (s *countingWorkflowStore) CreateWorkflow(ctx context.Context, command generation.CreateWorkflowCommand) (generation.WorkflowAggregate, bool, error) {
	s.mu.Lock()
	s.createCalls++
	s.mu.Unlock()
	return s.WorkflowStore.CreateWorkflow(ctx, command)
}

func (s *countingWorkflowStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createCalls
}

type dynamicRuntimeHarness struct {
	scheduler      *orchestration.Scheduler
	promptWriter   *acceptancePromptWriter
	workflowStore  *generation.MemoryStore
	countingStore  *countingWorkflowStore
	dispatchNeeded bool
}

type generationReceipt struct {
	operationID string
	batchID     string
	jobIDs      []string
}

func newDynamicRuntimeHarness(t *testing.T, runID string, dispatchRequired bool) *dynamicRuntimeHarness {
	t.Helper()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	promptWriter := &acceptancePromptWriter{}
	workflowStore := generation.NewMemoryStore(generation.WithMemoryClock(func() time.Time { return now }))
	countingStore := &countingWorkflowStore{WorkflowStore: workflowStore}
	var generationID atomic.Int64
	commands := generation.NewCommandService(generation.CommandServiceConfig{
		Store: countingStore,
		NewID: func() string {
			return fmt.Sprintf("%s-generation-%d", runID, generationID.Add(1))
		},
		Clock: func() time.Time { return now },
	})
	registry := vocabulary.NewRegistry()
	for _, tool := range []vocabulary.Tool{
		vocabulary.NewWriteMediaPromptTool(promptWriter),
		vocabulary.NewRequestConfirmationTool(),
		vocabulary.NewDispatchGenerationTool(orchestration.NewGenerationBridge(commands)),
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Descriptor().Key, err)
		}
	}
	var tokenID atomic.Int64
	scheduler, err := orchestration.NewScheduler(orchestration.SchedulerConfig{
		Store:       orchestration.NewMemoryRunStore(orchestration.WithMemoryRunStoreClock(func() time.Time { return now })),
		Vocabulary:  registry,
		MaxParallel: 1,
		JobBudget:   10,
		NewID:       func() string { return runID },
		OwnerID:     "acceptance-owner-" + runID,
		Now:         func() time.Time { return now },
		NewToken: func() string {
			return fmt.Sprintf("%s-token-%d", runID, tokenID.Add(1))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &dynamicRuntimeHarness{
		scheduler: scheduler, promptWriter: promptWriter, workflowStore: workflowStore,
		countingStore: countingStore, dispatchNeeded: dispatchRequired,
	}
}

func acceptancePlan(dispatchRequired bool) orchestration.ExecutionPlan {
	return orchestration.ExecutionPlan{
		PlanID: "acceptance-image", Source: "dynamic", Summary: "生成一张雨景图", Direction: "image",
		Steps: []orchestration.PlanStep{
			{ID: "prompt", Tool: "write_media_prompt", Params: map[string]any{"target_desc": "雨夜城市"}, Required: true},
			{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{
				"question": "使用该提示词生成？", "prompt": "$prompt.prompt",
			}, DependsOn: []string{"prompt"}, Required: true},
			{ID: "dispatch", Tool: "dispatch_generation", Params: map[string]any{
				"targets": []any{map[string]any{"prompt": "$prompt.prompt", "target_type": "session_deliverable"}},
			}, DependsOn: []string{"confirm"}, Required: dispatchRequired},
		},
		EstimatedJobs: 1,
	}
}

func (h *dynamicRuntimeHarness) submitAndResume(t *testing.T) (orchestration.PlanRun, generationReceipt) {
	t.Helper()
	ctx := context.Background()
	suspended, err := h.scheduler.Submit(ctx, "acceptance-session", "acceptance-user", acceptancePlan(h.dispatchNeeded))
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Status != orchestration.RunStatusSuspended || suspended.SuspendReason != orchestration.SuspendWaitingUser || suspended.SuspendedNodeID != "confirm" {
		t.Fatalf("Submit run = %+v", suspended)
	}
	if h.promptWriter.callCount() != 1 || h.promptWriter.targetDescription(t) != "雨夜城市" || h.countingStore.callCount() != 0 {
		t.Fatalf("calls after Submit: prompt=%d dispatch=%d", h.promptWriter.callCount(), h.countingStore.callCount())
	}
	confirm := suspended.Nodes["confirm"]
	if confirm == nil || confirm.Suspension == nil || confirm.Suspension.Payload["question"] != "使用该提示词生成？" {
		t.Fatalf("confirmation suspension = %+v", confirm)
	}
	resumeKey := confirm.ResumeKey
	waitingJobs, err := h.scheduler.Resume(ctx, suspended.ID, resumeKey, map[string]any{"approved": true})
	if err != nil {
		t.Fatal(err)
	}
	if waitingJobs.Status != orchestration.RunStatusSuspended || waitingJobs.SuspendReason != orchestration.SuspendWaitingJobs || waitingJobs.SuspendedNodeID != "dispatch" {
		t.Fatalf("Resume run = %+v", waitingJobs)
	}
	if h.promptWriter.callCount() != 1 || h.countingStore.callCount() != 1 {
		t.Fatalf("calls after Resume: prompt=%d dispatch=%d", h.promptWriter.callCount(), h.countingStore.callCount())
	}
	receipt := generationReceiptFromRun(t, waitingJobs)
	h.assertGenerationAggregate(t, waitingJobs, receipt)

	replayed, err := h.scheduler.Resume(ctx, suspended.ID, resumeKey, map[string]any{"approved": true})
	if err != nil {
		t.Fatal(err)
	}
	replayedReceipt := generationReceiptFromRun(t, replayed)
	if replayed.Version != waitingJobs.Version || replayed.Nodes["confirm"].Attempt != 1 || replayed.Nodes["dispatch"].Attempt != 1 || replayedReceipt.operationID != receipt.operationID || replayedReceipt.batchID != receipt.batchID || !slices.Equal(replayedReceipt.jobIDs, receipt.jobIDs) || h.promptWriter.callCount() != 1 || h.countingStore.callCount() != 1 {
		t.Fatalf("Resume replay mutated run=%+v prompt=%d dispatch=%d", replayed, h.promptWriter.callCount(), h.countingStore.callCount())
	}
	h.assertGenerationAggregate(t, replayed, replayedReceipt)
	return waitingJobs, receipt
}

func generationReceiptFromRun(t *testing.T, run orchestration.PlanRun) generationReceipt {
	t.Helper()
	dispatch := run.Nodes["dispatch"]
	if dispatch == nil || dispatch.Outputs == nil || dispatch.Suspension == nil {
		t.Fatalf("dispatch node has no durable receipt: %+v", dispatch)
	}
	operationID, operationOK := dispatch.Outputs["operation_id"].(string)
	batchID, batchOK := dispatch.Outputs["batch_id"].(string)
	jobIDs := stringArray(t, "dispatch outputs job_ids", dispatch.Outputs["job_ids"])
	waitingBatchID, waitingBatchOK := dispatch.Suspension.Payload["batch_id"].(string)
	waitingJobIDs := stringArray(t, "dispatch suspension job_ids", dispatch.Suspension.Payload["job_ids"])
	if !operationOK || operationID == "" || !batchOK || batchID == "" || len(jobIDs) == 0 || !waitingBatchOK || waitingBatchID != batchID || !slices.Equal(waitingJobIDs, jobIDs) {
		t.Fatalf("dispatch receipt outputs=%+v suspension=%+v", dispatch.Outputs, dispatch.Suspension.Payload)
	}
	return generationReceipt{operationID: operationID, batchID: batchID, jobIDs: jobIDs}
}

func stringArray(t *testing.T, label string, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %#v (%T), want JSON array", label, value, value)
	}
	result := make([]string, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok || text == "" {
			t.Fatalf("%s[%d] = %#v (%T), want non-empty string", label, index, item, item)
		}
		result[index] = text
	}
	return result
}

func (h *dynamicRuntimeHarness) assertGenerationAggregate(t *testing.T, run orchestration.PlanRun, receipt generationReceipt) {
	t.Helper()
	ctx := context.Background()
	operation, err := h.workflowStore.GetOperation(ctx, receipt.operationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.ID != receipt.operationID || operation.BatchID != receipt.batchID || operation.SessionID != run.SessionID || operation.UserID != run.UserID || operation.WorkflowRunID != run.ID || operation.StageRunID != "dispatch" || operation.ToolCallID != "" || operation.IdempotencyKey != run.ID+":dispatch:1" || operation.RequestFingerprint == "" || operation.Status != generation.OperationStatusWaitingJobs {
		t.Fatalf("generation operation = %+v", operation)
	}
	batch, err := h.workflowStore.GetBatch(ctx, receipt.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.ID != receipt.batchID || batch.OperationID != receipt.operationID || batch.SessionID != run.SessionID || batch.UserID != run.UserID || batch.WorkflowRunID != run.ID || batch.StageRunID != "dispatch" || batch.ToolCallID != "" || batch.Status != generation.BatchStatusWaitingJobs || batch.RequiredJobs != len(receipt.jobIDs) || batch.OptionalJobs != 0 {
		t.Fatalf("generation batch = %+v", batch)
	}
	jobs, err := h.workflowStore.ListJobsByBatch(ctx, receipt.batchID)
	if err != nil || len(jobs) != len(receipt.jobIDs) {
		t.Fatalf("generation jobs = %+v err=%v", jobs, err)
	}
	for index, jobID := range receipt.jobIDs {
		job, getErr := h.workflowStore.GetJob(ctx, jobID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		listed := jobs[index]
		planAttempt, planAttemptOK := job.Payload["plan_attempt"].(json.Number)
		if listed.ID != jobID || job.ID != jobID || job.BatchID != receipt.batchID || job.OperationID != receipt.operationID || job.SessionID != run.SessionID || job.UserID != run.UserID || job.WorkflowRunID != run.ID || job.StageRunID != "dispatch" || job.ToolCallID != "" || job.IdempotencyKey != run.ID+":dispatch:1:target:"+fmt.Sprint(index) || job.Status != generation.StatusQueued || job.Attempt != 0 || !planAttemptOK || planAttempt.String() != "1" || job.Payload["prompt"] != acceptancePrompt || job.Payload["target_type"] != generation.TargetKindSessionDeliverable || job.TargetType != generation.TargetKindSessionDeliverable || !job.Required || job.BindingToken.NormalizedKind() != generation.TargetKindSessionDeliverable || job.TargetID == "" || job.TargetID != job.BindingToken.TargetID || job.AssetSlot == "" || job.AssetSlot != job.BindingToken.AssetSlot || job.BindingToken.InputFingerprint == "" {
			t.Fatalf("generation job[%d] = %+v listed=%+v", index, job, listed)
		}
	}
}

func TestDynamicPlanRuntimeCompletesAndReplaysExactlyOnce(t *testing.T) {
	harness := newDynamicRuntimeHarness(t, "acceptance-completed", true)
	waitingJobs, receipt := harness.submitAndResume(t)
	completed, err := harness.scheduler.CompleteJobsWait(context.Background(), waitingJobs.ID, "dispatch", orchestration.JobsOutcome{
		BatchID: receipt.batchID, Status: generation.BatchStatusCompleted, Summary: map[string]any{"asset_ids": []any{"asset-1"}},
	})
	if err != nil || completed.Status != orchestration.RunStatusSucceeded {
		t.Fatalf("CompleteJobsWait run=%+v err=%v", completed, err)
	}
	assertTerminalNodes(t, completed)

	replayed, err := harness.scheduler.CompleteJobsWait(context.Background(), waitingJobs.ID, "dispatch", orchestration.JobsOutcome{
		BatchID: receipt.batchID, Status: generation.BatchStatusCompleted,
	})
	if err != nil || replayed.Version != completed.Version || replayed.Nodes["dispatch"].Outputs["batch_id"] != receipt.batchID || harness.promptWriter.callCount() != 1 || harness.countingStore.callCount() != 1 {
		t.Fatalf("jobs replay run=%+v err=%v prompt=%d dispatch=%d", replayed, err, harness.promptWriter.callCount(), harness.countingStore.callCount())
	}
	harness.assertGenerationAggregate(t, replayed, receipt)
}

func TestDynamicPlanRuntimeMapsFailedRequiredBatch(t *testing.T) {
	harness := newDynamicRuntimeHarness(t, "acceptance-failed", true)
	waitingJobs, receipt := harness.submitAndResume(t)
	failed, err := harness.scheduler.CompleteJobsWait(context.Background(), waitingJobs.ID, "dispatch", orchestration.JobsOutcome{
		BatchID: receipt.batchID, Status: generation.BatchStatusFailed,
	})
	if err != nil || failed.Status != orchestration.RunStatusFailed || failed.Nodes["dispatch"].Status != orchestration.NodeStatusFailed {
		t.Fatalf("failed run=%+v err=%v", failed, err)
	}
	assertTerminalNodes(t, failed)
}

func TestDynamicPlanRuntimeMapsPartialFailedOptionalBatch(t *testing.T) {
	harness := newDynamicRuntimeHarness(t, "acceptance-partial", false)
	waitingJobs, receipt := harness.submitAndResume(t)
	partial, err := harness.scheduler.CompleteJobsWait(context.Background(), waitingJobs.ID, "dispatch", orchestration.JobsOutcome{
		BatchID: receipt.batchID, Status: generation.BatchStatusPartialFailed,
	})
	if err != nil || partial.Status != orchestration.RunStatusPartialSucceeded || partial.Nodes["dispatch"].Status != orchestration.NodeStatusFailed {
		t.Fatalf("partial run=%+v err=%v", partial, err)
	}
	assertTerminalNodes(t, partial)
}

func assertTerminalNodes(t *testing.T, run orchestration.PlanRun) {
	t.Helper()
	for id, node := range run.Nodes {
		status := "<nil>"
		if node != nil {
			status = node.Status
		}
		switch status {
		case orchestration.NodeStatusSucceeded, orchestration.NodeStatusFailed, orchestration.NodeStatusSkipped:
			continue
		default:
			t.Fatalf("terminal run %q node %q has invalid status %q", run.Status, id, status)
		}
	}
}
