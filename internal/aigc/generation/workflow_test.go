package generation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"
)

func TestCommandCreateIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(WithMemoryIDGenerator(sequentialIDs("evt-1", "evt-2", "evt-3")))
	commands := NewCommandService(CommandServiceConfig{Store: store, NewID: sequentialIDs("generated-op", "generated-batch")})
	command := testWorkflowCommand("op-1", "batch-1", []GenerationJob{
		{ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true},
		{ID: "job-2", IdempotencyKey: "job-key-2", Provider: "mock", Required: false},
	})

	aggregate, created, err := commands.Create(ctx, command)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created || aggregate.Batch.RequiredJobs != 1 || aggregate.Batch.OptionalJobs != 1 {
		t.Fatalf("aggregate = %#v created=%v", aggregate, created)
	}
	events, err := store.ListOutbox(ctx, OutboxPending, 0)
	if err != nil {
		t.Fatalf("ListOutbox() error = %v", err)
	}
	if len(events) != 3 { // operation.accepted + two job.dispatch
		t.Fatalf("outbox count = %d, events = %#v", len(events), events)
	}

	again, created, err := commands.Create(ctx, command)
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if created || again.Operation.ID != aggregate.Operation.ID {
		t.Fatalf("second aggregate = %#v created=%v", again, created)
	}
	events, _ = store.ListOutbox(ctx, OutboxPending, 0)
	if len(events) != 3 {
		t.Fatalf("idempotent create duplicated outbox: %#v", events)
	}
}

func TestMemoryWorkflowCreateRollsBackAggregateAndOutboxTogether(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(WithMemoryIDGenerator(func() string { return "duplicate-outbox-id" }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-atomic", "batch-atomic", []GenerationJob{{
		ID: "job-atomic", IdempotencyKey: "job-key-atomic", Provider: "mock", Required: true,
	}}))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	if _, err := store.GetOperation(ctx, "op-atomic"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOperation() after rollback error = %v", err)
	}
	events, err := store.ListOutbox(ctx, OutboxPending, 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("outbox after rollback = %#v err=%v", events, err)
	}
}

func TestJobLifecycleTransitionsCommitImmutableOutboxSnapshots(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-progress", "batch-progress", []GenerationJob{{
		ID: "job-progress", IdempotencyKey: "job-key-progress", Provider: "mock", MediaKind: "image", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}

	transitions := []struct {
		status string
		phase  string
	}{
		{StatusRunning, PhaseProviderSubmit},
		{StatusWaitingProvider, PhaseProviderPoll},
		{StatusRunning, PhaseProviderPoll},
		{StatusFinalizing, PhaseArtifactFinalize},
		{StatusRetryWait, PhaseArtifactFinalize},
		{StatusFinalizing, PhaseArtifactFinalize},
		{StatusSucceeded, PhaseArtifactFinalize},
	}
	for _, transition := range transitions {
		job, getErr := store.GetJob(ctx, "job-progress")
		if getErr != nil {
			t.Fatal(getErr)
		}
		_, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.Status, current.Phase = transition.status, transition.phase
			if transition.status == StatusSucceeded {
				current.ResultAssetIDs = []string{"asset-1"}
			}
			return nil, nil
		})
		if err != nil {
			t.Fatalf("transition to %s/%s: %v", transition.status, transition.phase, err)
		}
	}

	events, err := store.ListOutbox(ctx, OutboxPending, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := make(map[int]OutboxEvent)
	for _, event := range events {
		if event.EventType != EventJobLifecycleChanged {
			continue
		}
		version, versionErr := boundedInteger(event.Payload["status_version"], 0)
		if versionErr != nil {
			t.Fatalf("status version: %v", versionErr)
		}
		snapshots[version] = event
		if event.AggregateVersion != version || event.IdempotencyKey != fmt.Sprintf("job:job-progress:lifecycle:%d", version) {
			t.Fatalf("lifecycle identity mismatch: %+v", event)
		}
	}
	if len(snapshots) != len(transitions) {
		t.Fatalf("lifecycle snapshots=%d events=%+v", len(snapshots), events)
	}
	first := snapshots[2]
	if first.Payload["status"] != StatusRunning || first.Payload["phase"] != PhaseProviderSubmit {
		t.Fatalf("running snapshot changed after later mutations: %+v", first.Payload)
	}
	last := snapshots[8]
	assetIDs, _ := last.Payload["result_asset_ids"].([]any)
	if last.Payload["status"] != StatusSucceeded || len(assetIDs) != 1 || assetIDs[0] != "asset-1" {
		t.Fatalf("terminal snapshot=%+v", last.Payload)
	}
}

func TestBatchTransactionAddsLifecycleEventForQueuedCancellation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-cancel-progress", "batch-cancel-progress", []GenerationJob{{
		ID: "job-cancel-progress", IdempotencyKey: "job-key-cancel-progress", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCommandService(CommandServiceConfig{Store: store}).CancelBatch(ctx, "batch-cancel-progress"); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListOutbox(ctx, OutboxPending, 0)
	found := false
	for _, event := range events {
		if event.EventType == EventJobLifecycleChanged && event.JobID == "job-cancel-progress" {
			found = event.Payload["status"] == StatusCancelled && event.AggregateType == "job" && event.AggregateVersion == 2
		}
	}
	if !found {
		t.Fatalf("queued cancellation lifecycle event missing: %+v", events)
	}
}

func TestSpecializedTerminalJobEventIsEnrichedWithoutDuplicateLifecycleEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-terminal-progress", "batch-terminal-progress", []GenerationJob{{
		ID: "job-terminal-progress", IdempotencyKey: "job-key-terminal-progress", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	job := advanceJob(t, store, "job-terminal-progress", StatusRunning, nil)
	_, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFailed
		current.ErrorCode = "provider_failed"
		return []OutboxEvent{{IdempotencyKey: "job:job-terminal-progress:failed", EventType: "job.failed", Destination: DestinationSessionSignal}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListOutbox(ctx, OutboxPending, 0)
	terminalEvents := 0
	for _, event := range events {
		if event.JobID != job.ID || event.Payload["status"] != StatusFailed {
			continue
		}
		terminalEvents++
		statusVersion, versionErr := boundedInteger(event.Payload["status_version"], 0)
		if event.EventType != "job.failed" || event.AggregateVersion != 3 || versionErr != nil || statusVersion != 3 {
			t.Fatalf("terminal event not enriched: %+v", event)
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("terminal lifecycle events=%d all=%+v", terminalEvents, events)
	}
}

func TestBatchBarrierWaitsForCompensationAndEmitsOneNetCostSignal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{
		{ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true},
		{ID: "job-2", IdempotencyKey: "job-key-2", Provider: "mock", Required: true},
	}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	advanceJob(t, store, "job-1", StatusFinalizing, nil)
	advanceJob(t, store, "job-1", StatusSucceeded, func(job *GenerationJob) {
		job.BillingTransactionID = "charge-1"
		job.ChargedPoints = 100
		job.NetChargedPoints = 100
		job.CompensationStatus = CompensationNotRequired
	})
	advanceJob(t, store, "job-2", StatusRunning, nil)
	advanceJob(t, store, "job-2", StatusFailed, func(job *GenerationJob) {
		job.BillingTransactionID = "charge-2"
		job.ChargedPoints = 50
		job.NetChargedPoints = 50
		job.CompensationStatus = CompensationPending
		job.ErrorCode = ErrorBindingConflictAfterCharge
	})

	barrier := NewBatchBarrier(store)
	result, err := barrier.TryFinalize(ctx, "batch-1")
	if err != nil {
		t.Fatalf("TryFinalize(pending) error = %v", err)
	}
	if result.Terminal {
		t.Fatalf("pending compensation reached terminal: %#v", result)
	}
	batch, _ := store.GetBatch(ctx, "batch-1")
	if batch.Status != BatchStatusFinalizing {
		t.Fatalf("batch status = %q", batch.Status)
	}

	job, _ := store.GetJob(ctx, "job-2")
	_, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.CompensationStatus = CompensationCompleted
		current.CompensatedPoints = 50
		current.NetChargedPoints = 0
		current.RefundTransactionID = "refund-2"
		return nil, nil
	})
	if err != nil {
		t.Fatalf("complete compensation error = %v", err)
	}
	result, err = barrier.TryFinalize(ctx, "batch-1")
	if err != nil {
		t.Fatalf("TryFinalize(completed) error = %v", err)
	}
	if !result.Terminal || !result.EventCreated || result.Payload.Status != BatchStatusFailed {
		t.Fatalf("result = %#v", result)
	}
	if result.Payload.Cost.GrossChargedPoints != 150 || result.Payload.Cost.RefundedPoints != 50 || result.Payload.Cost.NetChargedPoints != 100 {
		t.Fatalf("cost = %#v", result.Payload.Cost)
	}
	operation, operationErr := store.GetOperation(ctx, "op-1")
	if operationErr != nil || operation.Result["batch_id"] != "batch-1" || operation.Result["status"] != BatchStatusFailed {
		t.Fatalf("durable operation result = %#v err=%v", operation.Result, operationErr)
	}
	second, err := barrier.TryFinalize(ctx, "batch-1")
	if err != nil || !second.Terminal || second.EventCreated {
		t.Fatalf("second result = %#v err=%v", second, err)
	}
	events, _ := store.ListOutbox(ctx, OutboxPending, 0)
	terminal := 0
	for _, event := range events {
		if event.IdempotencyKey == "batch:batch-1:terminal" {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal outbox count = %d", terminal)
	}
}

func TestCancelIntentCancelsQueuedJobsAndBarrierStaysCancelled(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{
		{ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true},
		{ID: "job-2", IdempotencyKey: "job-key-2", Provider: "mock", Required: true},
	}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	commands := NewCommandService(CommandServiceConfig{Store: store})
	aggregate, err := commands.CancelBatch(ctx, "batch-1")
	if err != nil {
		t.Fatalf("CancelBatch() error = %v", err)
	}
	if !aggregate.Batch.CancelRequested || aggregate.Batch.Status != BatchStatusCancelling {
		t.Fatalf("cancelled aggregate = %#v", aggregate.Batch)
	}
	for _, job := range aggregate.Jobs {
		if job.Status != StatusCancelled || !job.CancelRequested {
			t.Fatalf("cancelled job = %#v", job)
		}
	}
	result, err := NewBatchBarrier(store).TryFinalize(ctx, "batch-1")
	if err != nil {
		t.Fatalf("TryFinalize() error = %v", err)
	}
	if result.Payload.Status != BatchStatusCancelled {
		t.Fatalf("payload = %#v", result.Payload)
	}
}

func TestLifecycleWorkerSubmitsOnceThenPollsAndFinalizes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	provider := &scriptedProvider{
		submit: ProviderResponse{State: ProviderStateAccepted, TaskID: "task-1", Status: "queued"},
		poll:   ProviderResponse{State: ProviderStateCompleted, TaskID: "task-1", Status: "done", Result: ProviderResult{AssetIDs: []string{"asset-1"}}},
	}
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	worker := NewLifecycleWorker(LifecycleWorkerConfig{
		Store:     store,
		Providers: map[string]ProviderAdapter{"mock": provider},
		Finalizer: NewFinalizationEngine(FinalizationEngineConfig{Store: store}),
		Clock:     func() time.Time { return now },
	})
	job, err := worker.Process(ctx, "job-1")
	if err != nil {
		t.Fatalf("Process(submit) error = %v", err)
	}
	if job.Status != StatusWaitingProvider || job.ProviderTaskID != "task-1" {
		t.Fatalf("submitted job = %#v", job)
	}
	now = now.Add(2 * time.Second)
	job, err = worker.Process(ctx, "job-1")
	if err != nil {
		t.Fatalf("Process(poll) error = %v", err)
	}
	if job.Status != StatusSucceeded || job.ResultDisposition != DispositionBoundCandidate {
		t.Fatalf("final job = %#v", job)
	}
	if provider.submits != 1 || provider.polls != 1 {
		t.Fatalf("provider calls submits=%d polls=%d", provider.submits, provider.polls)
	}
	batch, _ := store.GetBatch(ctx, "batch-1")
	if batch.Status != BatchStatusCompleted {
		t.Fatalf("batch = %#v", batch)
	}
}

func TestLifecycleWorkerRejectsAcceptedResponseWithoutTaskID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	provider := &scriptedProvider{submit: ProviderResponse{State: ProviderStateAccepted}}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"mock": provider}})
	job, err := worker.Process(ctx, "job-1")
	if err == nil {
		t.Fatal("Process() error is nil")
	}
	if job.Status != StatusFailed || job.ErrorCode != "provider_protocol_error" || provider.submits != 1 {
		t.Fatalf("job = %#v submits=%d", job, provider.submits)
	}
	assertOutboxKey(t, store, "job:job-1:batch-finalize-requested:terminal")
}

func TestLifecycleWorkerPersistsAndEnforcesProviderPollBudget(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-poll-budget", "batch-poll-budget", []GenerationJob{{
		ID: "job-poll-budget", IdempotencyKey: "job-poll-budget", Provider: "mock", Required: true, MaxAttempts: 4, MaxProviderPollAttempts: 2,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		submit: ProviderResponse{State: ProviderStateAccepted, TaskID: "task-poll-budget", Status: "queued", RetryAfter: time.Second},
		poll:   ProviderResponse{State: ProviderStatePending, TaskID: "task-poll-budget", Status: "running", RetryAfter: time.Second},
	}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"mock": provider}, Clock: func() time.Time { return now }})
	job, err := worker.Process(ctx, "job-poll-budget")
	if err != nil || job.ProviderPollAttempts != 0 || job.RetryCount != 0 {
		t.Fatalf("submit job = %+v err=%v", job, err)
	}

	now = now.Add(2 * time.Second)
	job, err = worker.Process(ctx, job.ID)
	if err != nil || job.Status != StatusWaitingProvider || job.ProviderPollAttempts != 1 || job.RetryCount != 0 {
		t.Fatalf("first poll job = %+v err=%v", job, err)
	}

	// A fresh worker observes the persisted count; a process restart cannot
	// reset the successful-pending poll budget.
	now = now.Add(2 * time.Second)
	worker = NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"mock": provider}, Clock: func() time.Time { return now }})
	job, err = worker.Process(ctx, job.ID)
	if err == nil || job.Status != StatusFailed || job.ErrorCode != "provider_poll_exhausted" || job.ProviderPollAttempts != 2 || job.RetryCount != 0 {
		t.Fatalf("exhausted poll job = %+v err=%v", job, err)
	}
	if provider.polls != 2 {
		t.Fatalf("provider polls = %d, want 2", provider.polls)
	}
}

func TestLifecycleWorkerMapsProviderCancellationToCancelled(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-provider-cancelled", "batch-provider-cancelled", []GenerationJob{{
		ID: "job-provider-cancelled", IdempotencyKey: "job-provider-cancelled", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		submit: ProviderResponse{State: ProviderStateAccepted, TaskID: "task-provider-cancelled", Status: "queued", RetryAfter: time.Second},
		poll:   ProviderResponse{State: ProviderStateCancelled, TaskID: "task-provider-cancelled", Status: "cancelled"},
	}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"mock": provider}, Clock: func() time.Time { return now }})
	job, err := worker.Process(ctx, "job-provider-cancelled")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	job, err = worker.Process(ctx, job.ID)
	if err != nil || job.Status != StatusCancelled || job.ErrorCode != "provider_cancelled" {
		t.Fatalf("cancelled job = %+v err=%v", job, err)
	}
	assertOutboxKey(t, store, "job:job-provider-cancelled:batch-finalize-requested:terminal")
}

func TestLifecycleWorkerFinalizingCancellationSettlesWithoutProviderCancel(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	advanceJob(t, store, "job-1", StatusFinalizing, func(job *GenerationJob) {
		job.ProviderTaskID = "task-completed"
		job.ResultAssetIDs = []string{"asset-1"}
	})
	if _, err := NewCommandService(CommandServiceConfig{Store: store}).CancelBatch(ctx, "batch-1"); err != nil {
		t.Fatalf("CancelBatch() error = %v", err)
	}
	provider := &scriptedProvider{}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"mock": provider}})
	job, err := worker.Process(ctx, "job-1")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if job.Status != StatusCancelled || provider.cancels != 0 {
		t.Fatalf("job = %#v provider cancels=%d", job, provider.cancels)
	}
	batch, err := store.GetBatch(ctx, "batch-1")
	if err != nil || batch.Status != BatchStatusCancelled {
		t.Fatalf("batch = %#v err=%v", batch, err)
	}
}

func TestFinalizationConflictAfterChargeCompensatesAndUnblocksBarrier(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	token := BindingToken{StoryboardID: "sb-1", TargetID: "shot-1", AssetSlot: "keyframe", TargetRevision: 1, PromptRevision: 1, GenerationEpoch: 1, InputFingerprint: "fp-1"}
	command := testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true, BindingToken: token,
	}})
	_, _, err := store.CreateWorkflow(ctx, command)
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	guard := &sequenceBindingGuard{checks: []BindingCheck{{TargetExists: true, Matches: true}, {TargetExists: true, Matches: false}}}
	billing := &fakeBillingGateway{}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Bindings: guard, Billing: billing})
	job, err := engine.Finalize(ctx, "job-1", ProviderResult{AssetIDs: []string{"asset-1"}, ActualPoints: 80})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if job.Status != StatusFailed || job.CompensationStatus != CompensationPending || job.ResultDisposition != DispositionSuperseded {
		t.Fatalf("job = %#v", job)
	}
	batch, _ := store.GetBatch(ctx, "batch-1")
	if batch.Status != BatchStatusFinalizing {
		t.Fatalf("batch before refund = %#v", batch)
	}
	compensation := NewCompensationService(CompensationServiceConfig{Store: store, Billing: billing})
	job, err = compensation.Run(ctx, "job-1")
	if err != nil {
		t.Fatalf("Compensation.Run() error = %v", err)
	}
	if job.CompensationStatus != CompensationCompleted || job.NetChargedPoints != 0 {
		t.Fatalf("compensated job = %#v", job)
	}
	batch, _ = store.GetBatch(ctx, "batch-1")
	if batch.Status != BatchStatusFailed || batch.Cost.NetChargedPoints != 0 || batch.Cost.RefundedPoints != 80 {
		t.Fatalf("batch after refund = %#v", batch)
	}
	if billing.chargeKey != "generation:charge:job-1" || billing.refundKey != "generation:refund:job-1:charge-1" {
		t.Fatalf("billing keys charge=%q refund=%q", billing.chargeKey, billing.refundKey)
	}
	assertOutboxKey(t, store, "job:job-1:batch-finalize-requested:compensation-settled")
}

func TestFinalizationCommitFailurePreservesJobIdentity(t *testing.T) {
	tests := []struct {
		name       string
		retryable  bool
		wantStatus string
	}{
		{name: "retryable", retryable: true, wantStatus: StatusRetryWait},
		{name: "non-retryable", retryable: false, wantStatus: StatusFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			jobID := "job-commit-failure-" + test.name
			_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-"+test.name, "batch-"+test.name, []GenerationJob{{
				ID: jobID, IdempotencyKey: jobID, Provider: "mock", Required: true, MaxAttempts: 3,
			}}))
			if err != nil {
				t.Fatal(err)
			}
			advanceJob(t, store, jobID, StatusRunning, nil)
			sentinel := errors.New("domain commit unavailable")
			commitErr := NewExecutionError(ErrorStageBinding, ErrorFinalizeFailed, test.retryable, sentinel)
			engine := NewFinalizationEngine(FinalizationEngineConfig{
				Store: store, Committer: failingFinalizationCommitter{err: commitErr},
			})
			job, err := engine.Finalize(ctx, jobID, ProviderResult{AssetIDs: []string{"asset-1"}})
			if !errors.Is(err, sentinel) {
				t.Fatalf("Finalize() error = %v, want original commit error", err)
			}
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("Finalize() masked commit error with not found: %v", err)
			}
			if job.ID != jobID || job.Status != test.wantStatus {
				t.Fatalf("returned job = %+v, want id=%s status=%s", job, jobID, test.wantStatus)
			}
			persisted, getErr := store.GetJob(ctx, jobID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if persisted.ID != jobID || persisted.Status != test.wantStatus || persisted.ErrorStage != ErrorStageBinding || persisted.ErrorCode != ErrorFinalizeFailed {
				t.Fatalf("persisted job = %+v", persisted)
			}
			if persisted.LeaseOwner != "" || persisted.LeaseUntil != nil || persisted.Retryable != test.retryable {
				t.Fatalf("persisted retry/lease state = %+v", persisted)
			}
		})
	}
}

func TestManualCompensationSettlementWritesDurableFinalizeRequest(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-manual-refund", "batch-manual-refund", []GenerationJob{{
		ID: "job-manual-refund", IdempotencyKey: "job-manual-refund", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, store, "job-manual-refund", StatusRunning, nil)
	advanceJob(t, store, "job-manual-refund", StatusFailed, func(job *GenerationJob) {
		job.BillingTransactionID = "charge-manual"
		job.BillingStatus = BillingCharged
		job.ChargedPoints = 20
		job.NetChargedPoints = 20
		job.CompensationStatus = CompensationPending
		job.ErrorCode = "compensation_failed"
		job.Retryable = false
	})
	service := NewCompensationService(CompensationServiceConfig{Store: store})
	job, err := service.ManualFinalize(ctx, "job-manual-refund", 20, "refund-manual")
	if err != nil || job.CompensationStatus != CompensationManualFinal || job.NetChargedPoints != 0 {
		t.Fatalf("manual settlement job = %+v err=%v", job, err)
	}
	assertOutboxKey(t, store, "job:job-manual-refund:batch-finalize-requested:compensation-settled")
}

func TestFinalizationRecoveryReusesFrozenProviderUsageReceipt(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	_, _, err := baseStore.CreateWorkflow(ctx, testWorkflowCommand("op-usage", "batch-usage", []GenerationJob{{ID: "job-usage", IdempotencyKey: "job-usage-key", Provider: "mock", Required: true}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, baseStore, "job-usage", StatusRunning, nil)
	store := &failRecordChargeStore{WorkflowStore: baseStore, failOnce: true}
	billing := &strictIdempotentBilling{}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Billing: billing})
	result := ProviderResult{AssetIDs: []string{"asset-usage"}, ActualPoints: 80, CostBreakdown: map[string]int64{"provider": 80}}
	if _, err := engine.Finalize(ctx, "job-usage", result); err == nil {
		t.Fatal("expected simulated recordCharge crash")
	}
	persisted, err := baseStore.GetJob(ctx, "job-usage")
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.ProviderUsageRecorded || persisted.ProviderActualPoints != 80 || persisted.ChargedPoints != 0 {
		t.Fatalf("provider receipt was not frozen before charge: %+v", persisted)
	}
	recovered, err := engine.Finalize(ctx, "job-usage", providerResultFromJob(persisted))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusSucceeded || recovered.ChargedPoints != 80 || billing.calls != 2 {
		t.Fatalf("recovered job=%+v billing=%+v", recovered, billing)
	}
}

func TestFinalizationRetryReusesFrozenSettlementQuote(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-quote", "batch-quote", []GenerationJob{{ID: "job-quote", IdempotencyKey: "job-quote-key", Provider: "mock", MediaKind: "image", Required: true, MaxAttempts: 3}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, store, "job-quote", StatusRunning, nil)
	calculator := &mutableCostCalculator{points: 12}
	billing := &failOnceChargeBilling{fail: true}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Billing: billing, Calculator: calculator})
	if _, err := engine.Finalize(ctx, "job-quote", ProviderResult{AssetIDs: []string{"asset-quote"}}); err == nil {
		t.Fatal("first transient billing attempt unexpectedly succeeded")
	}
	persisted, err := store.GetJob(ctx, "job-quote")
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.SettlementQuoteRecorded || persisted.SettlementPoints != 12 {
		t.Fatalf("settlement quote was not frozen: %+v", persisted)
	}
	calculator.points = 99
	recovered, err := engine.Finalize(ctx, "job-quote", providerResultFromJob(persisted))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusSucceeded || recovered.ChargedPoints != 12 || calculator.calls != 1 || billing.lastPoints != 12 {
		t.Fatalf("recovered=%+v calculator=%+v billing=%+v", recovered, calculator, billing)
	}
}

func TestFinalizationBindingCheckErrorAfterChargeRequestsCompensation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	engine := NewFinalizationEngine(FinalizationEngineConfig{
		Store: store, Billing: &fakeBillingGateway{},
		Bindings: &sequenceBindingGuard{checks: []BindingCheck{{TargetExists: true, Matches: true}}},
	})
	job, err := engine.Finalize(ctx, "job-1", ProviderResult{AssetIDs: []string{"asset-1"}, ActualPoints: 12})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if job.Status != StatusFailed || job.CompensationStatus != CompensationPending || job.BillingTransactionID == "" {
		t.Fatalf("job = %#v", job)
	}
}

func TestBillingRejectionIsTerminalAndDoesNotRequestCompensation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	billing := &rejectingBillingGateway{}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Billing: billing})
	job, err := engine.Finalize(ctx, "job-1", ProviderResult{AssetIDs: []string{"asset-1"}, ActualPoints: 10})
	if err == nil {
		t.Fatal("Finalize() error is nil")
	}
	if job.Status != StatusFailed || job.ErrorCode != ErrorBillingRejected || job.CompensationStatus != CompensationNotRequired {
		t.Fatalf("job = %#v", job)
	}
	batch, getErr := store.GetBatch(ctx, "batch-1")
	if getErr != nil || batch.Status != BatchStatusFailed {
		t.Fatalf("batch = %#v err=%v", batch, getErr)
	}
}

func TestSupersededBeforeChargeFailsWithoutBilling(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	token := BindingToken{StoryboardID: "sb-1", TargetID: "shot-1", AssetSlot: "video", TargetRevision: 1, PromptRevision: 2, GenerationEpoch: 3, InputFingerprint: "old"}
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true, BindingToken: token,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	billing := &fakeBillingGateway{}
	engine := NewFinalizationEngine(FinalizationEngineConfig{
		Store: store, Billing: billing,
		Bindings: &sequenceBindingGuard{checks: []BindingCheck{{TargetExists: true, Matches: false}}},
	})
	job, err := engine.Finalize(ctx, "job-1", ProviderResult{AssetIDs: []string{"asset-1"}, ActualPoints: 10})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if job.Status != StatusFailed || job.ErrorCode != ErrorResultSuperseded || job.ResultDisposition != DispositionSuperseded {
		t.Fatalf("job = %#v", job)
	}
	if billing.chargeKey != "" {
		t.Fatalf("superseded result was charged with key %q", billing.chargeKey)
	}
}

func TestCancelRacingSuccessfulChargeRecordsRefundAndEndsBatchCancelled(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	commands := NewCommandService(CommandServiceConfig{Store: store})
	billing := &callbackBillingGateway{onCharge: func() {
		if _, cancelErr := commands.CancelBatch(ctx, "batch-1"); cancelErr != nil {
			t.Errorf("CancelBatch() error = %v", cancelErr)
		}
	}}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Billing: billing})
	job, err := engine.Finalize(ctx, "job-1", ProviderResult{AssetIDs: []string{"asset-1"}, ActualPoints: 25})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if job.Status != StatusCancelled || job.CompensationStatus != CompensationPending || job.BillingTransactionID != "charge-race" {
		t.Fatalf("job after charge/cancel race = %#v", job)
	}
	compensation := NewCompensationService(CompensationServiceConfig{Store: store, Billing: billing})
	if _, err := compensation.Run(ctx, job.ID); err != nil {
		t.Fatalf("Compensation.Run() error = %v", err)
	}
	batch, err := store.GetBatch(ctx, "batch-1")
	if err != nil || batch.Status != BatchStatusCancelled || batch.Cost.NetChargedPoints != 0 {
		t.Fatalf("batch = %#v err=%v", batch, err)
	}
}

func TestCompensationFailureEmitsDurableRetryAndThenCompletes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	advanceJob(t, store, "job-1", StatusRunning, nil)
	advanceJob(t, store, "job-1", StatusFailed, func(job *GenerationJob) {
		job.BillingTransactionID = "charge-1"
		job.BillingStatus = BillingCharged
		job.ChargedPoints = 20
		job.NetChargedPoints = 20
		job.CompensationStatus = CompensationPending
	})
	balance := int64(980)
	billing := &flakyRefundGateway{remainingFailures: 1, balanceAfter: &balance}
	service := NewCompensationService(CompensationServiceConfig{Store: store, Billing: billing, Clock: func() time.Time { return now }})
	job, err := service.Run(ctx, "job-1")
	if err == nil {
		t.Fatal("Compensation.Run(first) error is nil")
	}
	if job.CompensationStatus != CompensationRetryWait || job.RetryCount != 1 || !job.NextRunAt.Equal(now.Add(time.Second)) {
		t.Fatalf("retry job = %#v", job)
	}
	events, _ := store.ListOutbox(ctx, OutboxPending, 0)
	foundRetry := false
	for _, event := range events {
		if event.IdempotencyKey == "job:job-1:compensation-retry:1" && event.AvailableAt.Equal(now.Add(time.Second)) {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatalf("compensation retry event missing: %#v", events)
	}
	now = now.Add(2 * time.Second)
	job, err = service.Run(ctx, "job-1")
	if err != nil {
		t.Fatalf("Compensation.Run(second) error = %v", err)
	}
	if job.CompensationStatus != CompensationCompleted || job.NetChargedPoints != 0 || job.BalanceAfter == nil || *job.BalanceAfter != balance {
		t.Fatalf("completed compensation job = %#v", job)
	}
}

func TestRecoverySchedulerOnlyWakesDueOrExpiredLeases(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-1", "batch-1", []GenerationJob{{
		ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	leaseUntil := now.Add(10 * time.Second)
	job := advanceJob(t, store, "job-1", StatusRunning, func(job *GenerationJob) {
		job.LeaseOwner = "worker-1"
		job.LeaseUntil = &leaseUntil
	})
	queue := &capturingDispatchQueue{}
	scheduler := NewRecoveryScheduler(store, queue, func() time.Time { return now })
	count, err := scheduler.EnqueueDue(ctx, 10)
	if err != nil || count != 0 {
		t.Fatalf("EnqueueDue(active lease) count=%d err=%v", count, err)
	}
	now = leaseUntil.Add(time.Millisecond)
	count, err = scheduler.EnqueueDue(ctx, 10)
	if err != nil || count != 1 || len(queue.payloads) != 1 {
		t.Fatalf("EnqueueDue(expired lease) count=%d payloads=%#v err=%v", count, queue.payloads, err)
	}
	wantKey := fmt.Sprintf("job:job-1:wake:%d", job.StatusVersion)
	if queue.payloads[0].JobID != job.ID || queue.payloads[0].IdempotencyKey != wantKey {
		t.Fatalf("wake payload = %#v want key %q", queue.payloads[0], wantKey)
	}
}

func TestOutboxDispatcherDoesNotBlockLaterRowsAfterOnePublishFailure(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("operation-outbox", "batch-outbox", []GenerationJob{
		{ID: "job-outbox-1", IdempotencyKey: "job-outbox-key-1", Provider: ProviderImage2},
		{ID: "job-outbox-2", IdempotencyKey: "job-outbox-key-2", Provider: ProviderImage2},
	}))
	if err != nil {
		t.Fatal(err)
	}
	publisher := &selectiveOutboxPublisher{failType: EventOperationAccepted}
	dispatcher := NewOutboxDispatcher(store, publisher)
	published, err := dispatcher.DispatchPending(ctx, 100)
	if err == nil || published != 2 {
		t.Fatalf("published=%d err=%v events=%v", published, err, publisher.seen)
	}
	pending, err := store.ListOutbox(ctx, OutboxPending, 100)
	if err != nil || len(pending) != 1 || pending[0].EventType != EventOperationAccepted {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestTerminalJobDurablyRetriesBarrierAfterCrashAndOutboxReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	baseStore := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := baseStore.CreateWorkflow(ctx, testWorkflowCommand("op-durable-finalize", "batch-durable-finalize", []GenerationJob{{
		ID: "job-durable-finalize", IdempotencyKey: "job-durable-finalize", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, baseStore, "job-durable-finalize", StatusRunning, nil)

	// Simulate the process-local Barrier attempt failing after the terminal Job
	// transaction committed. Finalize must still succeed because the same Job
	// transaction owns a durable finalize-requested Outbox row.
	flakyStore := &failTransactBatchOnceStore{WorkflowStore: baseStore, remainingFailures: 1}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: flakyStore, Clock: func() time.Time { return now }})
	job, err := engine.Finalize(ctx, "job-durable-finalize", ProviderResult{AssetIDs: []string{"asset-durable-finalize"}})
	if err != nil || job.Status != StatusSucceeded {
		t.Fatalf("Finalize() job = %+v err=%v", job, err)
	}
	batch, _ := baseStore.GetBatch(ctx, "batch-durable-finalize")
	if batch.Status != BatchStatusWaitingJobs {
		t.Fatalf("batch finalized despite injected Barrier failure: %+v", batch)
	}
	assertOutboxKey(t, baseStore, "job:job-durable-finalize:batch-finalize-requested:terminal")

	// Simulate a consumer crash/ACK loss after applying the Barrier. The relay
	// retries the same stable event, while the Barrier emits one terminal event.
	publisher := &replayingBarrierPublisher{barrier: NewBatchBarrier(baseStore, func() time.Time { return now }), failAfterFirstApply: true}
	dispatcher := NewOutboxDispatcher(baseStore, publisher, func() time.Time { return now })
	if _, err := dispatcher.DispatchPending(ctx, 100); err == nil {
		t.Fatal("first dispatch should report the simulated ACK loss")
	}
	batch, _ = baseStore.GetBatch(ctx, "batch-durable-finalize")
	if batch.Status != BatchStatusCompleted {
		t.Fatalf("replayed Barrier did not finalize batch: %+v", batch)
	}

	now = now.Add(2 * time.Second)
	if _, err := dispatcher.DispatchPending(ctx, 100); err != nil {
		t.Fatalf("second dispatch error = %v", err)
	}
	if publisher.finalizeCalls != 2 {
		t.Fatalf("finalize-requested publish calls = %d, want 2", publisher.finalizeCalls)
	}
	events, err := baseStore.ListOutbox(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	terminalEvents := 0
	for _, event := range events {
		if event.IdempotencyKey == "batch:batch-durable-finalize:terminal" {
			terminalEvents++
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("terminal outbox count = %d, events=%+v", terminalEvents, events)
	}
}

func TestBatchFinalizeRequestedOutboxNeverDeadLetters(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 11, 0, 0, 0, time.UTC)
	baseStore := NewMemoryStore(WithMemoryClock(func() time.Time { return now }))
	_, _, err := baseStore.CreateWorkflow(ctx, testWorkflowCommand("op-finalize-retry", "batch-finalize-retry", []GenerationJob{{
		ID: "job-finalize-retry", IdempotencyKey: "job-finalize-retry", Provider: "mock", Required: true,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, baseStore, "job-finalize-retry", StatusRunning, nil)
	flakyStore := &failTransactBatchOnceStore{WorkflowStore: baseStore, remainingFailures: 1}
	if _, err := NewFinalizationEngine(FinalizationEngineConfig{Store: flakyStore, Clock: func() time.Time { return now }}).Finalize(ctx, "job-finalize-retry", ProviderResult{AssetIDs: []string{"asset-finalize-retry"}}); err != nil {
		t.Fatal(err)
	}

	publisher := OutboxPublisherFunc(func(_ context.Context, event OutboxEvent) error {
		if event.EventType == EventBatchFinalizeRequested {
			return errors.New("barrier database unavailable")
		}
		return nil
	})
	dispatcher := NewOutboxDispatcher(baseStore, publisher, func() time.Time { return now })
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := dispatcher.DispatchPending(ctx, 100); err == nil {
			t.Fatalf("DispatchPending(attempt %d) error is nil", attempt+1)
		}
		now = now.Add(time.Hour)
	}

	events, err := baseStore.ListOutbox(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.IdempotencyKey != "job:job-finalize-retry:batch-finalize-requested:terminal" {
			continue
		}
		if event.Status != OutboxPending || event.Attempts != 12 {
			t.Fatalf("finalize-requested event = %+v", event)
		}
		return
	}
	t.Fatal("finalize-requested event missing")
}

func TestProviderPollCountDoesNotConsumeTransientFailureRetries(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("operation-long-poll", "batch-long-poll", []GenerationJob{{ID: "job-long-poll", IdempotencyKey: "job-long-poll-key", Provider: "long-poll", MaxAttempts: 4}}))
	if err != nil {
		t.Fatal(err)
	}
	job, _ := store.GetJob(ctx, "job-long-poll")
	job, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRunning
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusWaitingProvider
		current.Phase = PhaseProviderPoll
		current.ProviderTaskID = "provider-task-1"
		current.Attempt = 20
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{Store: store, Providers: map[string]ProviderAdapter{"long-poll": retryablePollFailure{}}, Clock: time.Now})
	updated, err := worker.Process(ctx, job.ID)
	if err == nil || updated.Status != StatusRetryWait || updated.RetryCount != 1 {
		t.Fatalf("long-poll retry job = %+v, err=%v", updated, err)
	}
}

func TestFinalizationRecoveryCompensatesPreviouslyChargedStaleResult(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-charged-recovery", "batch-charged-recovery", []GenerationJob{{ID: "job-charged-recovery", IdempotencyKey: "job-charged-recovery", Provider: "mock", Required: true}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, store, "job-charged-recovery", StatusRunning, nil)
	advanceJob(t, store, "job-charged-recovery", StatusFinalizing, func(job *GenerationJob) {
		job.ResultAssetIDs = []string{"asset-1"}
		job.BillingTransactionID, job.BillingStatus = "charge-1", BillingCharged
		job.ChargedPoints, job.NetChargedPoints = 12, 12
	})
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Bindings: &sequenceBindingGuard{checks: []BindingCheck{{TargetExists: true, Matches: false}}}})
	job, err := engine.Finalize(ctx, "job-charged-recovery", ProviderResult{AssetIDs: []string{"asset-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusFailed || job.CompensationStatus != CompensationPending || job.ErrorCode != ErrorBindingConflictAfterCharge {
		t.Fatalf("charged stale recovery = %+v", job)
	}
}

func TestCommittedReceiptWinsLateCancelAndCompletesBatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-commit-receipt", "batch-commit-receipt", []GenerationJob{{ID: "job-commit-receipt", IdempotencyKey: "job-commit-receipt", Provider: "mock", Required: true}}))
	if err != nil {
		t.Fatal(err)
	}
	advanceJob(t, store, "job-commit-receipt", StatusRunning, nil)
	advanceJob(t, store, "job-commit-receipt", StatusFinalizing, func(job *GenerationJob) { job.ResultAssetIDs = []string{"asset-1"} })
	if _, err := NewCommandService(CommandServiceConfig{Store: store}).CancelBatch(ctx, "batch-commit-receipt"); err != nil {
		t.Fatal(err)
	}
	engine := NewFinalizationEngine(FinalizationEngineConfig{Store: store, Inspector: fixedCommitInspector(true)})
	job, err := engine.Finalize(ctx, "job-commit-receipt", ProviderResult{AssetIDs: []string{"asset-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("job = %+v", job)
	}
	batch, _ := store.GetBatch(ctx, "batch-commit-receipt")
	if batch.Status != BatchStatusCompleted {
		t.Fatalf("batch = %+v", batch)
	}
}

func TestProviderFailureQuarantinesRecoveredPartialReceipt(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.CreateWorkflow(ctx, testWorkflowCommand("op-partial", "batch-partial", []GenerationJob{{ID: "job-partial", IdempotencyKey: "job-partial", Provider: "partial", Required: true}}))
	if err != nil {
		t.Fatal(err)
	}
	discarder := &capturingDiscarder{}
	worker := NewLifecycleWorker(LifecycleWorkerConfig{
		Store: store, Providers: map[string]ProviderAdapter{"partial": permanentProviderFailure{}},
		ResultRecovery: fixedResultRecovery{result: ProviderResult{AssetIDs: []string{"asset-partial"}, Payload: map[string]any{"receipt_complete": false}}},
		Finalizer:      NewFinalizationEngine(FinalizationEngineConfig{Store: store, Discarder: discarder}),
	})
	job, err := worker.Process(ctx, "job-partial")
	if err == nil || job.Status != StatusFailed {
		t.Fatalf("job = %+v err=%v", job, err)
	}
	if len(discarder.assets) != 1 || discarder.assets[0] != "asset-partial" {
		t.Fatalf("discarded assets = %#v", discarder.assets)
	}
}

func testWorkflowCommand(operationID, batchID string, jobs []GenerationJob) CreateWorkflowCommand {
	return CreateWorkflowCommand{
		Operation: GenerationOperation{ID: operationID, SessionID: "session-1", UserID: "user-1", StageRunID: "stage-1", ToolCallID: "tool-1", IdempotencyKey: "operation-key-" + operationID},
		Batch:     GenerationBatch{ID: batchID, CompletionPolicy: CompletionAllRequired, WakePolicy: WakeOnTerminal, DeliveryPolicy: DeliveryPolicy{BindingMode: BindingModeCandidate, ApprovalPolicy: ApprovalReviewRequired, ChargePolicy: ChargePostpaidNoReservation}},
		Jobs:      jobs,
	}
}

func advanceJob(t *testing.T, store WorkflowStore, jobID, status string, mutate func(*GenerationJob)) GenerationJob {
	t.Helper()
	job, err := store.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJob(%s) error = %v", jobID, err)
	}
	updated, err := store.MutateJob(context.Background(), jobID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = status
		if mutate != nil {
			mutate(current)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("MutateJob(%s -> %s) error = %v", jobID, status, err)
	}
	return updated
}

func assertOutboxKey(t *testing.T, store WorkflowStore, key string) {
	t.Helper()
	events, err := store.ListOutbox(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListOutbox() error = %v", err)
	}
	for _, event := range events {
		if event.IdempotencyKey == key {
			return
		}
	}
	t.Fatalf("outbox key %q missing: %+v", key, events)
}

type scriptedProvider struct {
	submit  ProviderResponse
	poll    ProviderResponse
	submits int
	polls   int
	cancels int
}

type failTransactBatchOnceStore struct {
	WorkflowStore
	remainingFailures int
}

func (s *failTransactBatchOnceStore) TransactBatch(ctx context.Context, batchID string, transaction BatchTransaction) (WorkflowAggregate, error) {
	if s.remainingFailures > 0 {
		s.remainingFailures--
		return WorkflowAggregate{}, errors.New("injected batch barrier outage")
	}
	return s.WorkflowStore.TransactBatch(ctx, batchID, transaction)
}

type replayingBarrierPublisher struct {
	barrier             *BatchBarrier
	failAfterFirstApply bool
	finalizeCalls       int
}

func (p *replayingBarrierPublisher) PublishOutbox(ctx context.Context, event OutboxEvent) error {
	if event.EventType != EventBatchFinalizeRequested {
		return nil
	}
	p.finalizeCalls++
	batchID, _ := event.Payload["batch_id"].(string)
	if _, err := p.barrier.TryFinalize(ctx, batchID); err != nil {
		return err
	}
	if p.failAfterFirstApply {
		p.failAfterFirstApply = false
		return errors.New("simulated consumer crash before ACK")
	}
	return nil
}

type retryablePollFailure struct{}

type fixedCommitInspector bool

type failingFinalizationCommitter struct{ err error }

func (c failingFinalizationCommitter) Commit(context.Context, FinalizationCommit) error {
	return c.err
}

func (i fixedCommitInspector) IsCommitted(context.Context, GenerationJob, []string) (bool, error) {
	return bool(i), nil
}

type fixedResultRecovery struct{ result ProviderResult }

func (r fixedResultRecovery) RecoverProviderResult(context.Context, GenerationJob) (ProviderResult, bool, error) {
	return r.result, false, nil
}

type permanentProviderFailure struct{}

func (permanentProviderFailure) Submit(context.Context, GenerationJob) (ProviderResponse, error) {
	return ProviderResponse{}, NewExecutionError(ErrorStageProvider, "permanent_failure", false, errors.New("permanent provider failure"))
}
func (permanentProviderFailure) Poll(context.Context, GenerationJob) (ProviderResponse, error) {
	return ProviderResponse{}, errors.New("unexpected poll")
}
func (permanentProviderFailure) Cancel(context.Context, GenerationJob) (ProviderCancelResult, error) {
	return ProviderCancelResult{}, errors.New("unexpected cancel")
}

type capturingDiscarder struct{ assets []string }

func (d *capturingDiscarder) Discard(_ context.Context, _ GenerationJob, assetIDs []string, _ string) error {
	d.assets = append(d.assets, assetIDs...)
	return nil
}

func (retryablePollFailure) Submit(context.Context, GenerationJob) (ProviderResponse, error) {
	return ProviderResponse{}, errors.New("unexpected submit")
}

func (retryablePollFailure) Poll(context.Context, GenerationJob) (ProviderResponse, error) {
	return ProviderResponse{}, NewExecutionError(ErrorStageProvider, "temporary_poll_failure", true, errors.New("temporary poll outage"))
}

func (retryablePollFailure) Cancel(context.Context, GenerationJob) (ProviderCancelResult, error) {
	return ProviderCancelResult{}, errors.New("unexpected cancel")
}

func (p *scriptedProvider) Submit(context.Context, GenerationJob) (ProviderResponse, error) {
	p.submits++
	return p.submit, nil
}

func (p *scriptedProvider) Poll(context.Context, GenerationJob) (ProviderResponse, error) {
	p.polls++
	return p.poll, nil
}

func (p *scriptedProvider) Cancel(context.Context, GenerationJob) (ProviderCancelResult, error) {
	p.cancels++
	return ProviderCancelResult{Confirmed: true}, nil
}

type sequenceBindingGuard struct {
	checks []BindingCheck
}

func (g *sequenceBindingGuard) Check(context.Context, BindingToken) (BindingCheck, error) {
	if len(g.checks) == 0 {
		return BindingCheck{}, errors.New("no binding check scripted")
	}
	result := g.checks[0]
	g.checks = g.checks[1:]
	return result, nil
}

type fakeBillingGateway struct {
	chargeKey string
	refundKey string
}

type rejectingBillingGateway struct{}

func (*rejectingBillingGateway) Charge(context.Context, ChargeRequest) (ChargeResult, error) {
	return ChargeResult{}, NewExecutionError(ErrorStageBilling, ErrorBillingRejected, false, errors.New("insufficient balance"))
}

func (*rejectingBillingGateway) Refund(context.Context, RefundRequest) (RefundResult, error) {
	return RefundResult{}, errors.New("unexpected refund")
}

type callbackBillingGateway struct {
	onCharge func()
}

type flakyRefundGateway struct {
	remainingFailures int
	balanceAfter      *int64
}

func (*flakyRefundGateway) Charge(context.Context, ChargeRequest) (ChargeResult, error) {
	return ChargeResult{}, errors.New("unexpected charge")
}

func (b *flakyRefundGateway) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
	if b.remainingFailures > 0 {
		b.remainingFailures--
		return RefundResult{}, errors.New("temporary refund outage")
	}
	return RefundResult{TransactionID: "refund-retry", RefundedPoints: request.Points, BalanceAfter: b.balanceAfter}, nil
}

type capturingDispatchQueue struct {
	payloads []QueuePayload
}

type selectiveOutboxPublisher struct {
	failType string
	seen     []string
}

func (p *selectiveOutboxPublisher) PublishOutbox(_ context.Context, event OutboxEvent) error {
	p.seen = append(p.seen, event.EventType)
	if event.EventType == p.failType {
		return errors.New("projection unavailable")
	}
	return nil
}

func (q *capturingDispatchQueue) Enqueue(_ context.Context, payload QueuePayload) error {
	q.payloads = append(q.payloads, payload)
	return nil
}

func (b *callbackBillingGateway) Charge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
	if b.onCharge != nil {
		b.onCharge()
	}
	return ChargeResult{TransactionID: "charge-race", ChargedPoints: request.Points}, nil
}

func (*callbackBillingGateway) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
	return RefundResult{TransactionID: "refund-race", RefundedPoints: request.Points}, nil
}

func (b *fakeBillingGateway) Charge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
	b.chargeKey = request.IdempotencyKey
	return ChargeResult{TransactionID: "charge-1", ChargedPoints: request.Points, Breakdown: request.Breakdown}, nil
}

func (b *fakeBillingGateway) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
	b.refundKey = request.IdempotencyKey
	return RefundResult{TransactionID: "refund-1", RefundedPoints: request.Points}, nil
}

type failRecordChargeStore struct {
	WorkflowStore
	failOnce bool
}

func (s *failRecordChargeStore) MutateJob(ctx context.Context, jobID string, expectedVersion int, mutation JobMutation) (GenerationJob, error) {
	current, err := s.WorkflowStore.GetJob(ctx, jobID)
	if err != nil {
		return GenerationJob{}, err
	}
	preview := cloneJob(current)
	if _, err := mutation(&preview); err != nil {
		return GenerationJob{}, err
	}
	if s.failOnce && current.BillingStatus == BillingCharging && preview.BillingStatus == BillingCharged {
		s.failOnce = false
		return GenerationJob{}, errors.New("simulated crash before charge receipt commit")
	}
	return s.WorkflowStore.MutateJob(ctx, jobID, expectedVersion, mutation)
}

type strictIdempotentBilling struct {
	calls     int
	key       string
	points    int64
	breakdown map[string]int64
}

type mutableCostCalculator struct {
	points int64
	calls  int
}

func (c *mutableCostCalculator) Calculate(context.Context, GenerationJob, ProviderResult) (int64, map[string]int64, error) {
	c.calls++
	return c.points, map[string]int64{"configured": c.points}, nil
}

type failOnceChargeBilling struct {
	fail       bool
	lastPoints int64
}

func (b *failOnceChargeBilling) Charge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
	b.lastPoints = request.Points
	if b.fail {
		b.fail = false
		return ChargeResult{}, errors.New("temporary billing outage")
	}
	return ChargeResult{TransactionID: "charge-quote", ChargedPoints: request.Points, Breakdown: request.Breakdown}, nil
}

func (*failOnceChargeBilling) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
	return RefundResult{TransactionID: "refund-quote", RefundedPoints: request.Points}, nil
}

func (b *strictIdempotentBilling) Charge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
	b.calls++
	if b.calls == 1 {
		b.key, b.points, b.breakdown = request.IdempotencyKey, request.Points, cloneInt64Map(request.Breakdown)
	} else if b.key != request.IdempotencyKey || b.points != request.Points || !maps.Equal(b.breakdown, request.Breakdown) {
		return ChargeResult{}, errors.New("billing idempotency conflict")
	}
	return ChargeResult{TransactionID: "charge-usage", ChargedPoints: request.Points, Breakdown: cloneInt64Map(request.Breakdown)}, nil
}

func (*strictIdempotentBilling) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
	return RefundResult{TransactionID: "refund-usage", RefundedPoints: request.Points}, nil
}

func sequentialIDs(ids ...string) func() string {
	index := 0
	return func() string {
		if len(ids) == 0 {
			return "id"
		}
		if index >= len(ids) {
			return ids[len(ids)-1] + "-extra"
		}
		value := ids[index]
		index++
		return value
	}
}

func TestStateMachineRejectsTerminalRegression(t *testing.T) {
	if err := ValidateJobTransition(StatusSucceeded, StatusRunning); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ValidateJobTransition() error = %v", err)
	}
	if err := ValidateBatchTransition(BatchStatusCancelled, BatchStatusCompleted); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ValidateBatchTransition() error = %v", err)
	}
}

func TestRetryBackoffIsBounded(t *testing.T) {
	if retryBackoff(100) != 128*time.Second {
		t.Fatalf("retryBackoff(100) = %s", retryBackoff(100))
	}
}
