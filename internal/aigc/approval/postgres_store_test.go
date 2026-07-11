package approval

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestMigrationOwnsApprovalDecisionContinuationOutboxAndImmutability(t *testing.T) {
	joined := strings.Join(MigrationStatements, "\n")
	for _, required := range []string{
		"aigc_approvals", "aigc_approval_decisions", "aigc_approval_continuations",
		"aigc_outbox_events", "aigc_candidate_approval_batches", "review_mode", "durable_fallback",
		"trg_aigc_approval_review_mode_immutable",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
}

func TestPostgresDecisionTransactionAndFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("local postgres is not available: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	suffix := time.Now().UnixNano()
	id := fmt.Sprintf("approval-pg-%d", suffix)
	requested := testApproval(id, ReviewModeInterrupt)
	created, err := store.Create(ctx, requested)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = store.BindInterruptMapping(ctx, MappingCommand{
		ApprovalID: id, ExpectedExecutionEpoch: 1,
		CheckpointMappingID: "mapping-" + id, MappingEpoch: 1,
	})
	if err != nil {
		t.Fatalf("BindInterruptMapping() error = %v", err)
	}
	decision, err := store.Decide(ctx, DecideCommand{
		ApprovalID: id, IdempotencyKey: "decision:" + id,
		Decision: DecisionApprove, CurrentBinding: created.Approval.Binding,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Approval.Status != StatusApproved || decision.Continuation.Executor != sessionruntime.ContinuationExecutorRunnerResume || decision.Outbox.EventType != EventSessionInputRequested {
		t.Fatalf("decision transaction result = %#v", decision)
	}
	loadedDecision, err := store.GetDecision(ctx, id, 1)
	if err != nil || loadedDecision.CommandKind != "promote_revision" {
		t.Fatalf("GetDecision() = %#v, err=%v", loadedDecision, err)
	}
	loadedContinuation, err := store.GetContinuation(ctx, id, 1)
	if err != nil || loadedContinuation.Executor != sessionruntime.ContinuationExecutorRunnerResume {
		t.Fatalf("GetContinuation() = %#v, err=%v", loadedContinuation, err)
	}

	fallback, err := store.SwitchToDurableFallback(ctx, FallbackCommand{
		ApprovalID: id, ExpectedExecutionMode: ExecutionModeInterrupt,
		ExpectedExecutionEpoch: 1, ExpectedDecisionVersion: 1,
	})
	if err != nil {
		t.Fatalf("SwitchToDurableFallback() error = %v", err)
	}
	if fallback.Approval.ReviewMode != ReviewModeInterrupt || fallback.Approval.ExecutionMode != ExecutionModeDurableFallback || fallback.Continuation == nil || fallback.Continuation.Executor != sessionruntime.ContinuationExecutorDeterministic || fallback.Outbox.EventType != EventApprovalContinuationRequested {
		t.Fatalf("fallback result = %#v", fallback)
	}
	ackAt := time.Now().UTC()
	if err := store.MarkOutboxPublished(ctx, fallback.Outbox.ID, ackAt); err != nil {
		t.Fatalf("MarkOutboxPublished() error = %v", err)
	}
	if err := store.MarkOutboxPublished(ctx, fallback.Outbox.ID, ackAt.Add(time.Hour)); err != nil {
		t.Fatalf("duplicate MarkOutboxPublished() error = %v", err)
	}
	published, err := store.ListOutbox(ctx, OutboxStatusPublished, 1000)
	if err != nil {
		t.Fatalf("ListOutbox(published) error = %v", err)
	}
	foundPublished := false
	for _, event := range published {
		if event.ID == fallback.Outbox.ID {
			foundPublished = event.PublishedAt != nil && event.PublishedAt.Equal(ackAt)
			break
		}
	}
	if !foundPublished {
		t.Fatalf("published fallback outbox %s not found", fallback.Outbox.ID)
	}

	// The database trigger enforces ReviewMode immutability even outside Store.
	if err := db.WithContext(ctx).Exec("UPDATE aigc_approvals SET review_mode = ? WHERE id = ?", ReviewModeDurable, id).Error; err == nil {
		t.Fatal("expected direct review_mode update to be rejected")
	}
}
