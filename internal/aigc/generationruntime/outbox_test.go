package generationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestLifecycleProjectionHidesJobCardsAndUsesOneStableCapabilityCard(t *testing.T) {
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
	events, _ = store.ListOutbox(ctx, generation.OutboxPending, 0)
	var terminalEvent generation.OutboxEvent
	for _, event := range events {
		if strings.HasPrefix(event.EventType, "batch.") && event.EventType != generation.EventBatchFinalizeRequested {
			terminalEvent = event
		}
	}
	if terminalEvent.ID == "" {
		t.Fatalf("terminal batch event missing: %+v", events)
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
	if err := publisher.PublishOutbox(ctx, terminalEvent); err != nil {
		t.Fatal(err)
	}
	// Replayed Job lifecycle rows are deliberately silent in chat. Only the
	// accepted and terminal Operation/Batch snapshots update the capability card.
	if len(captured.events) != 2 {
		t.Fatalf("projection events=%+v", captured.events)
	}
	acceptedEnvelope, ok := captured.events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(acceptedEnvelope.Actions) != 1 {
		t.Fatalf("accepted envelope=%#v", captured.events[0].Payload)
	}
	acceptedAction := acceptedEnvelope.Actions[0]
	acceptedPayload := acceptedAction.Payload.(map[string]any)
	acceptedView := acceptedPayload["tool_run"].(map[string]any)
	if acceptedAction.Target == nil || acceptedAction.Target.CardID != "tool_run:generate_media" || acceptedView["status"] != generation.BatchStatusWaitingJobs {
		t.Fatalf("accepted projection=%+v action=%+v", acceptedView, acceptedAction)
	}
	if _, exists := acceptedView["status_version"]; exists {
		t.Fatalf("session-stable capability card must not reuse per-operation status versions: %+v", acceptedView)
	}
	for _, forbidden := range []string{"job_id", "target_id", "asset_slot", "result_asset_ids", "nodes"} {
		if _, exists := acceptedView[forbidden]; exists {
			t.Fatalf("public capability card leaked %s: %+v", forbidden, acceptedView)
		}
	}
	if _, exists := acceptedPayload["refresh_resources"]; exists {
		t.Fatalf("non-terminal projection requested a resource refresh: %+v", acceptedPayload)
	}

	terminalEnvelope := captured.events[1].Payload.(a2ui.ActionEnvelope)
	terminalAction := terminalEnvelope.Actions[0]
	terminalPayload := terminalAction.Payload.(map[string]any)
	terminalView := terminalPayload["tool_run"].(map[string]any)
	if terminalAction.Target == nil || terminalAction.Target.CardID != acceptedAction.Target.CardID || terminalView["status"] != generation.BatchStatusFailed {
		t.Fatalf("terminal projection=%+v action=%+v", terminalView, terminalAction)
	}
	refresh, ok := terminalPayload["refresh_resources"].([]string)
	if !ok || len(refresh) != 3 || refresh[0] != "storyboard" || refresh[1] != "assets" || refresh[2] != "jobs" {
		t.Fatalf("terminal refresh hint=%#v", terminalPayload["refresh_resources"])
	}
}

func TestPublicCapabilityCardNormalizesWorkflowKinds(t *testing.T) {
	tests := []struct {
		kind     string
		wantCard string
		wantTool string
	}{
		{kind: "generate_media", wantCard: "tool_run:generate_media", wantTool: "generate_media"},
		{kind: "target_regeneration_recovery", wantCard: "tool_run:generate_media", wantTool: "generate_media"},
		{kind: "assemble_preview", wantCard: "tool_run:assemble_output", wantTool: "assemble_output"},
		{kind: "assemble_export_recovery", wantCard: "tool_run:assemble_output", wantTool: "assemble_output"},
	}
	for _, test := range tests {
		card, tool := publicCapabilityCard(test.kind)
		if card != test.wantCard || tool != test.wantTool {
			t.Fatalf("publicCapabilityCard(%q) = (%q, %q), want (%q, %q)", test.kind, card, tool, test.wantCard, test.wantTool)
		}
	}
	if card, tool := publicCapabilityCard("unknown"); card != "" || tool != "" {
		t.Fatalf("unknown workflow projected as (%q, %q)", card, tool)
	}
}
