package generation

import (
	"context"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresWorkflowStoreTransactionAndBarrier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresWorkflowStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	suffix := time.Now().Format("20060102150405.000000000")
	command := testWorkflowCommand("workflow-op-"+suffix, "workflow-batch-"+suffix, []GenerationJob{{
		ID: "workflow-job-" + suffix, IdempotencyKey: "workflow-job-key-" + suffix,
		Provider: "mock", Required: true,
	}})
	command.Operation.IdempotencyKey = "workflow-operation-key-" + suffix
	aggregate, created, err := store.CreateWorkflow(ctx, command)
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	if !created || aggregate.Batch.Status != BatchStatusWaitingJobs || len(aggregate.Jobs) != 1 {
		t.Fatalf("aggregate = %#v created=%v", aggregate, created)
	}
	again, created, err := store.CreateWorkflow(ctx, command)
	if err != nil || created || again.Operation.ID != aggregate.Operation.ID {
		t.Fatalf("idempotent create aggregate=%#v created=%v err=%v", again, created, err)
	}
	job := advanceJob(t, store, aggregate.Jobs[0].ID, StatusRunning, nil)
	job = advanceJob(t, store, job.ID, StatusFinalizing, nil)
	advanceJob(t, store, job.ID, StatusSucceeded, func(current *GenerationJob) {
		current.ResultDisposition = DispositionBoundCandidate
		current.CompensationStatus = CompensationNotRequired
	})
	barrierResult, err := NewBatchBarrier(store).TryFinalize(ctx, aggregate.Batch.ID)
	if err != nil {
		t.Fatalf("TryFinalize() error = %v", err)
	}
	if !barrierResult.Terminal || barrierResult.Payload.Status != BatchStatusCompleted {
		t.Fatalf("barrier result = %#v", barrierResult)
	}
	batch, err := store.GetBatch(ctx, aggregate.Batch.ID)
	if err != nil || batch.Status != BatchStatusCompleted {
		t.Fatalf("batch = %#v err=%v", batch, err)
	}
	events, err := store.ListOutbox(ctx, OutboxPending, 0)
	if err != nil {
		t.Fatalf("ListOutbox() error = %v", err)
	}
	terminal := 0
	lifecycle := map[string]bool{}
	for _, event := range events {
		if event.IdempotencyKey == "batch:"+aggregate.Batch.ID+":terminal" {
			terminal++
		}
		if event.JobID == aggregate.Jobs[0].ID && event.EventType == EventJobLifecycleChanged {
			status, _ := event.Payload["status"].(string)
			lifecycle[status] = event.AggregateVersion > 1
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal outbox count = %d", terminal)
	}
	for _, status := range []string{StatusRunning, StatusFinalizing, StatusSucceeded} {
		if !lifecycle[status] {
			t.Fatalf("postgres lifecycle snapshot %q missing: %+v", status, events)
		}
	}
}
