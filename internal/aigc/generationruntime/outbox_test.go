package generationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

type retryableRefundFailure struct{}

type capturingSessionRuntime struct {
	input sessionruntime.SessionInput
}

type capturingA2UIEvents struct{ events []a2ui.SSEEvent }

func (p *capturingA2UIEvents) Publish(_ context.Context, event a2ui.SSEEvent) error {
	p.events = append(p.events, event)
	return nil
}

func (r *capturingSessionRuntime) Enqueue(_ context.Context, _ string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	r.input = input
	return sessionruntime.EnqueueResult{}, nil
}

func (*capturingSessionRuntime) EnsureSession(context.Context, string) (bool, error) {
	return true, nil
}

func (retryableRefundFailure) Charge(context.Context, generation.ChargeRequest) (generation.ChargeResult, error) {
	return generation.ChargeResult{}, errors.New("unexpected charge")
}

func (retryableRefundFailure) Refund(context.Context, generation.RefundRequest) (generation.RefundResult, error) {
	return generation.RefundResult{}, errors.New("temporary refund outage")
}

func TestCompensationOutboxAcknowledgesPersistedRetrySchedule(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store := generation.NewMemoryStore(generation.WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: "operation-1", SessionID: "session-1", IdempotencyKey: "operation-key-1"},
		Batch: generation.GenerationBatch{ID: "batch-1", CompletionPolicy: generation.CompletionAllRequired, WakePolicy: generation.WakeOnTerminal,
			DeliveryPolicy: generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired, ChargePolicy: generation.ChargePostpaidNoReservation}},
		Jobs: []generation.GenerationJob{{ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := store.GetJob(ctx, "job-1")
	job, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusFailed
		current.BillingTransactionID = "charge-1"
		current.BillingStatus = generation.BillingCharged
		current.ChargedPoints = 25
		current.NetChargedPoints = 25
		current.CompensationStatus = generation.CompensationPending
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	compensation := generation.NewCompensationService(generation.CompensationServiceConfig{Store: store, Billing: retryableRefundFailure{}, Clock: func() time.Time { return now }})
	publisher := SessionSignalPublisher{Compensation: compensation}
	err = publisher.PublishOutbox(ctx, generation.OutboxEvent{Destination: generation.DestinationBilling, Payload: map[string]any{"job_id": job.ID}})
	if err != nil {
		t.Fatalf("persisted retry must acknowledge the consumed outbox request: %v", err)
	}
	updated, err := store.GetJob(ctx, job.ID)
	if err != nil || updated.CompensationStatus != generation.CompensationRetryWait || !updated.NextRunAt.After(now) {
		t.Fatalf("updated compensation job = %+v, err=%v", updated, err)
	}
}

func TestBatchContinuationCarriesTrustedTerminalResultAndCost(t *testing.T) {
	runtime := &capturingSessionRuntime{}
	publisher := SessionSignalPublisher{Runtime: runtime}
	payload := map[string]any{
		"session_id": "session-1", "operation_id": "operation-1", "batch_id": "batch-1", "batch_version": 7,
		"status": generation.BatchStatusCompleted, "needs_agent_explanation": true,
		"cost": map[string]any{"gross_charged_points": 12, "refunded_points": 2, "net_charged_points": 10},
		"jobs": []map[string]any{{"job_id": "job-1", "status": generation.StatusSucceeded, "result_asset_ids": []string{"asset-1"}}},
	}
	if err := publisher.PublishOutbox(context.Background(), generation.OutboxEvent{ID: "event-1", EventType: "batch.completed", Destination: generation.DestinationSessionSignal, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	input, ok := runtime.input.(sessionruntime.BatchContinuationResult)
	if !ok || len(input.Result) == 0 {
		t.Fatalf("batch continuation = %#v", runtime.input)
	}
	var trusted map[string]any
	if err := json.Unmarshal(input.Result, &trusted); err != nil {
		t.Fatal(err)
	}
	cost, _ := trusted["cost"].(map[string]any)
	jobs, _ := trusted["jobs"].([]any)
	if cost["net_charged_points"] != float64(10) || len(jobs) != 1 {
		t.Fatalf("trusted result = %#v", trusted)
	}
}

func TestLifecycleProjectionUsesDurableSnapshotAndUpdatesPublicStage(t *testing.T) {
	ctx := context.Background()
	store := generation.NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: "operation-progress", SessionID: "session-progress", StageRunID: "stage-progress", ToolCallID: "call-progress", IdempotencyKey: "operation-key", Kind: "generate_media"},
		Batch: generation.GenerationBatch{ID: "batch-progress", CompletionPolicy: generation.CompletionAllRequired, WakePolicy: generation.WakeOnTerminal,
			DeliveryPolicy: generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired, ChargePolicy: generation.ChargePostpaidNoReservation}},
		Jobs: []generation.GenerationJob{{ID: "job-progress", IdempotencyKey: "job-key", Provider: "mock", MediaKind: "image", TargetID: "shot-1", AssetSlot: "keyframe", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := store.GetJob(ctx, "job-progress")
	job, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusRunning
		current.Phase = generation.PhaseProviderSubmit
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListOutbox(ctx, generation.OutboxPending, 0)
	var runningEvent generation.OutboxEvent
	var acceptedEvent generation.OutboxEvent
	for _, event := range events {
		if event.EventType == generation.EventJobLifecycleChanged {
			runningEvent = event
		}
		if event.EventType == generation.EventOperationAccepted {
			acceptedEvent = event
		}
	}
	if runningEvent.ID == "" || acceptedEvent.ID == "" {
		t.Fatalf("running lifecycle event missing: %+v", events)
	}
	// Advance the aggregate before dispatch. The projection must still replay
	// the immutable running snapshot instead of collapsing to the latest state.
	_, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusFailed
		current.ErrorCode = "later_failure"
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generation.NewBatchBarrier(store).TryFinalize(ctx, "batch-progress"); err != nil {
		t.Fatal(err)
	}
	captured := &capturingA2UIEvents{}
	publisher := SessionSignalPublisher{Store: store, Events: captured, Now: func() time.Time { return time.Unix(100, 0) }}
	if err := publisher.PublishOutbox(ctx, runningEvent); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishOutbox(ctx, runningEvent); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishOutbox(ctx, acceptedEvent); err != nil {
		t.Fatal(err)
	}
	if len(captured.events) != 3 || captured.events[0].ID != runningEvent.ID+":projection" || captured.events[1].ID != captured.events[0].ID {
		t.Fatalf("projection replay ids=%+v", captured.events)
	}
	envelope, ok := captured.events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 2 {
		t.Fatalf("envelope=%#v", captured.events[0].Payload)
	}
	jobPayload := envelope.Actions[0].Payload.(map[string]any)["tool_run"].(map[string]any)
	if envelope.Actions[0].Target.CardID != "job:job-progress" || jobPayload["status"] != generation.StatusRunning || jobPayload["status_version"] != 2 || jobPayload["error_code"] != "" {
		t.Fatalf("job projection=%+v action=%+v", jobPayload, envelope.Actions[0])
	}
	stagePayload := envelope.Actions[1].Payload.(map[string]any)["tool_run"].(map[string]any)
	nodes := stagePayload["nodes"].([]map[string]any)
	if envelope.Actions[1].Target.CardID != "tool_run:call-progress" || stagePayload["stage_run_id"] != "stage-progress" || len(nodes) != 1 || nodes[0]["status"] != generation.StatusRunning || nodes[0]["status_version"] != 2 {
		t.Fatalf("stage projection=%+v action=%+v", stagePayload, envelope.Actions[1])
	}
	acceptedEnvelope := captured.events[2].Payload.(a2ui.ActionEnvelope)
	operationView := acceptedEnvelope.Actions[0].Payload.(map[string]any)["operation"].(map[string]any)
	acceptedStage := acceptedEnvelope.Actions[1].Payload.(map[string]any)["tool_run"].(map[string]any)
	if operationView["status"] != generation.BatchStatusWaitingJobs || operationView["status_version"] != 1 || acceptedStage["status"] != generation.BatchStatusWaitingJobs || acceptedStage["status_version"] != 1 {
		t.Fatalf("accepted operation=%+v stage=%+v", operationView, acceptedStage)
	}
}
