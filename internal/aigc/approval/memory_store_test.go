package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

func TestInterruptDecisionAtomicallyCreatesContinuationAndSessionInputOutbox(t *testing.T) {
	now := time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)
	store := NewMemoryStoreWithClock(func() time.Time { return now })
	created, err := store.Create(context.Background(), testApproval("approval-interrupt", ReviewModeInterrupt))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.Created || created.Approval.Status != StatusPending || created.Approval.ExecutionEpoch != 1 {
		t.Fatalf("created approval = %#v", created)
	}
	_, err = store.BindInterruptMapping(context.Background(), MappingCommand{
		ApprovalID: "approval-interrupt", ExpectedExecutionEpoch: 1,
		CheckpointMappingID: "mapping-1", MappingEpoch: 4,
	})
	if err != nil {
		t.Fatalf("BindInterruptMapping() error = %v", err)
	}
	result, err := store.Decide(context.Background(), DecideCommand{
		ApprovalID: "approval-interrupt", ExpectedDecisionVersion: 0,
		IdempotencyKey: "decision-interrupt-1", Decision: DecisionApprove,
		ActorID: "user-1", CurrentBinding: created.Approval.Binding, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !result.Created || result.Approval.Status != StatusApproved || result.Approval.DecisionVersion != 1 {
		t.Fatalf("decision result = %#v", result)
	}
	if result.Decision.CommandKind != "promote_revision" || result.Decision.CommandIdempotencyKey == "" {
		t.Fatalf("frozen approve command = %#v", result.Decision)
	}
	if result.Continuation.Executor != sessionruntime.ContinuationExecutorRunnerResume || result.Continuation.ExecutionEpoch != 1 {
		t.Fatalf("continuation = %#v", result.Continuation)
	}
	if result.Outbox.EventType != EventSessionInputRequested || result.Outbox.Destination != DestinationSessionInputs {
		t.Fatalf("outbox = %#v", result.Outbox)
	}
	var resume sessionruntime.ResumeRequested
	if err := json.Unmarshal(result.Outbox.Payload, &resume); err != nil {
		t.Fatalf("decode resume payload: %v", err)
	}
	if resume.ApprovalID != "approval-interrupt" || resume.DecisionVersion != 1 || resume.MappingID != "mapping-1" || resume.MappingEpoch != 4 {
		t.Fatalf("resume payload = %#v", resume)
	}

	replayed, err := store.Decide(context.Background(), DecideCommand{
		ApprovalID: "approval-interrupt", ExpectedDecisionVersion: 0,
		IdempotencyKey: "decision-interrupt-1", Decision: DecisionApprove,
		ActorID: "user-1", CurrentBinding: created.Approval.Binding, Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("replayed Decide() error = %v", err)
	}
	if replayed.Created || replayed.Decision.DecisionVersion != 1 {
		t.Fatalf("replayed result = %#v", replayed)
	}
	outbox, err := store.ListOutbox(context.Background(), OutboxStatusPending, 100)
	if err != nil || len(outbox) != 1 {
		t.Fatalf("outbox after replay = %#v, err=%v", outbox, err)
	}
	_, err = store.Decide(context.Background(), DecideCommand{
		ApprovalID: "approval-interrupt", ExpectedDecisionVersion: 1,
		IdempotencyKey: "different-decision-key", Decision: DecisionReject,
		CurrentBinding: created.Approval.Binding,
	})
	if !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("second decision error = %v, want ErrAlreadyDecided", err)
	}
}

func TestInterruptDecisionWithoutMappingRollsBack(t *testing.T) {
	store := NewMemoryStore()
	created, err := store.Create(context.Background(), testApproval("approval-no-mapping", ReviewModeInterrupt))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = store.Decide(context.Background(), DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision-no-mapping",
		Decision: DecisionApprove, CurrentBinding: created.Approval.Binding,
	})
	if err == nil {
		t.Fatal("expected missing mapping decision to fail")
	}
	current, err := store.Get(context.Background(), created.Approval.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Status != StatusPending || current.DecisionVersion != 0 {
		t.Fatalf("approval changed after failed transaction: %#v", current)
	}
	if _, err := store.GetDecision(context.Background(), current.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDecision() error = %v, want ErrNotFound", err)
	}
	outbox, _ := store.ListOutbox(context.Background(), "", 100)
	if len(outbox) != 0 {
		t.Fatalf("failed decision created outbox: %#v", outbox)
	}
}

func TestDurableDecisionUsesDeterministicContinuationAndRejectCommand(t *testing.T) {
	store := NewMemoryStore()
	created, err := store.Create(context.Background(), testApproval("approval-durable", ReviewModeDurable))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	result, err := store.Decide(context.Background(), DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision-durable",
		Decision: DecisionReject, CurrentBinding: created.Approval.Binding,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.Approval.Status != StatusRejected || result.Decision.CommandKind != "reject_revision" {
		t.Fatalf("rejected decision = %#v", result)
	}
	if result.Continuation.Executor != sessionruntime.ContinuationExecutorDeterministic {
		t.Fatalf("continuation = %#v", result.Continuation)
	}
	if result.Outbox.EventType != EventApprovalContinuationRequested || result.Outbox.Destination != DestinationApprovalContinuations {
		t.Fatalf("outbox = %#v", result.Outbox)
	}
}

func TestVersionMismatchAndExpiryBecomeTerminalNoOpBranches(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		store := NewMemoryStore()
		created, err := store.Create(context.Background(), testApproval("approval-stale", ReviewModeDurable))
		if err != nil {
			t.Fatal(err)
		}
		current := created.Approval.Binding
		current.TargetRevision++
		result, err := store.Decide(context.Background(), DecideCommand{
			ApprovalID: created.Approval.ID, IdempotencyKey: "decision-stale",
			Decision: DecisionApprove, CurrentBinding: current,
		})
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if result.Approval.Status != StatusStale || result.Decision.CommandKind != "" {
			t.Fatalf("stale decision = %#v", result)
		}
	})

	t.Run("expired", func(t *testing.T) {
		now := time.Date(2026, 7, 11, 3, 0, 0, 0, time.UTC)
		store := NewMemoryStoreWithClock(func() time.Time { return now })
		approval := testApproval("approval-expired", ReviewModeDurable)
		expires := now.Add(time.Minute)
		approval.ExpiresAt = &expires
		created, err := store.Create(context.Background(), approval)
		if err != nil {
			t.Fatal(err)
		}
		result, err := store.Decide(context.Background(), DecideCommand{
			ApprovalID: created.Approval.ID, IdempotencyKey: "decision-expired",
			Decision: DecisionApprove, CurrentBinding: created.Approval.Binding, Now: expires,
		})
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if result.Approval.Status != StatusExpired || result.Decision.CommandKind != "" {
			t.Fatalf("expired decision = %#v", result)
		}
	})
}

func TestCloseSupportsStaleExpiredAndCancelled(t *testing.T) {
	for _, status := range []Status{StatusStale, StatusExpired, StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			store := NewMemoryStore()
			created, err := store.Create(context.Background(), testApproval("approval-close-"+string(status), ReviewModeDurable))
			if err != nil {
				t.Fatal(err)
			}
			result, err := store.Close(context.Background(), CloseCommand{
				ApprovalID: created.Approval.ID, IdempotencyKey: "close-" + string(status), Status: status,
			})
			if err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if result.Approval.Status != status || result.Decision.DecisionVersion != 1 || result.Decision.CommandKind != "" {
				t.Fatalf("close result = %#v", result)
			}
		})
	}
}

func TestFallbackBeforeAndAfterDecisionIsFencedAndKeepsReviewMode(t *testing.T) {
	t.Run("before decision", func(t *testing.T) {
		store := NewMemoryStore()
		created, err := store.Create(context.Background(), testApproval("approval-fallback-before", ReviewModeInterrupt))
		if err != nil {
			t.Fatal(err)
		}
		fallback, err := store.SwitchToDurableFallback(context.Background(), FallbackCommand{
			ApprovalID: created.Approval.ID, ExpectedExecutionMode: ExecutionModeInterrupt,
			ExpectedExecutionEpoch: 1, ExpectedDecisionVersion: 0,
		})
		if err != nil {
			t.Fatalf("SwitchToDurableFallback() error = %v", err)
		}
		if !fallback.Switched || fallback.Approval.ReviewMode != ReviewModeInterrupt || fallback.Approval.ExecutionMode != ExecutionModeDurableFallback || fallback.Approval.ExecutionEpoch != 2 || fallback.Continuation != nil {
			t.Fatalf("fallback result = %#v", fallback)
		}
		result, err := store.Decide(context.Background(), DecideCommand{
			ApprovalID: created.Approval.ID, IdempotencyKey: "fallback-before-decision",
			Decision: DecisionApprove, CurrentBinding: created.Approval.Binding,
		})
		if err != nil {
			t.Fatalf("Decide after fallback error = %v", err)
		}
		if result.Continuation.Executor != sessionruntime.ContinuationExecutorDeterministic || result.Continuation.ExecutionEpoch != 2 {
			t.Fatalf("fallback continuation = %#v", result.Continuation)
		}
	})

	t.Run("after decision", func(t *testing.T) {
		now := time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC)
		store := NewMemoryStoreWithClock(func() time.Time { return now })
		created, err := store.Create(context.Background(), testApproval("approval-fallback-after", ReviewModeInterrupt))
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.BindInterruptMapping(context.Background(), MappingCommand{
			ApprovalID: created.Approval.ID, ExpectedExecutionEpoch: 1, CheckpointMappingID: "mapping-after", MappingEpoch: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		decision, err := store.Decide(context.Background(), DecideCommand{
			ApprovalID: created.Approval.ID, IdempotencyKey: "decision-before-fallback",
			Decision: DecisionApprove, CurrentBinding: created.Approval.Binding, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Continuation.Executor != sessionruntime.ContinuationExecutorRunnerResume {
			t.Fatalf("initial continuation = %#v", decision.Continuation)
		}
		fallback, err := store.SwitchToDurableFallback(context.Background(), FallbackCommand{
			ApprovalID: created.Approval.ID, ExpectedExecutionMode: ExecutionModeInterrupt,
			ExpectedExecutionEpoch: 1, ExpectedDecisionVersion: 1, Now: now.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("SwitchToDurableFallback() error = %v", err)
		}
		if fallback.Approval.ReviewMode != ReviewModeInterrupt || fallback.Approval.ExecutionMode != ExecutionModeDurableFallback || fallback.Continuation == nil || fallback.Continuation.Executor != sessionruntime.ContinuationExecutorDeterministic || fallback.Continuation.ExecutionEpoch != 2 {
			t.Fatalf("fallback result = %#v", fallback)
		}
		if fallback.Outbox.EventType != EventApprovalContinuationRequested {
			t.Fatalf("fallback outbox = %#v", fallback.Outbox)
		}
		replay, err := store.SwitchToDurableFallback(context.Background(), FallbackCommand{
			ApprovalID: created.Approval.ID, ExpectedExecutionMode: ExecutionModeInterrupt,
			ExpectedExecutionEpoch: 1, ExpectedDecisionVersion: 1,
		})
		if err != nil || replay.Switched {
			t.Fatalf("fallback replay = %#v, err=%v", replay, err)
		}
		_, err = store.SwitchToDurableFallback(context.Background(), FallbackCommand{
			ApprovalID: created.Approval.ID, ExpectedExecutionMode: ExecutionModeInterrupt,
			ExpectedExecutionEpoch: 2, ExpectedDecisionVersion: 1,
		})
		if !errors.Is(err, ErrFallbackFenced) {
			t.Fatalf("stale fallback fence error = %v", err)
		}
	})
}

func TestFallbackRejectsActiveContinuationClaim(t *testing.T) {
	now := time.Date(2026, 7, 11, 5, 0, 0, 0, time.UTC)
	store := NewMemoryStoreWithClock(func() time.Time { return now })
	created, _ := store.Create(context.Background(), testApproval("approval-busy", ReviewModeInterrupt))
	_, _ = store.BindInterruptMapping(context.Background(), MappingCommand{
		ApprovalID: created.Approval.ID, ExpectedExecutionEpoch: 1, CheckpointMappingID: "mapping-busy", MappingEpoch: 1,
	})
	_, err := store.Decide(context.Background(), DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision-busy", Decision: DecisionApprove,
		CurrentBinding: created.Approval.Binding, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	key := continuationKey(created.Approval.ID, 1)
	continuation := store.continuations[key]
	lease := now.Add(time.Hour)
	continuation.Status = sessionruntime.ContinuationStatusClaimed
	continuation.LeaseUntil = &lease
	store.continuations[key] = continuation
	store.mu.Unlock()
	_, err = store.SwitchToDurableFallback(context.Background(), FallbackCommand{
		ApprovalID: created.Approval.ID, ExpectedExecutionMode: ExecutionModeInterrupt,
		ExpectedExecutionEpoch: 1, ExpectedDecisionVersion: 1, Now: now,
	})
	if !errors.Is(err, ErrContinuationBusy) {
		t.Fatalf("fallback error = %v, want ErrContinuationBusy", err)
	}
}

func TestCreateIsIdempotentAndFreezesCommandPayload(t *testing.T) {
	store := NewMemoryStore()
	requested := testApproval("approval-create-idempotent", ReviewModeDurable)
	created, err := store.Create(context.Background(), requested)
	if err != nil {
		t.Fatal(err)
	}
	requested.ApproveCommand.Payload[2] = 'X'
	loaded, err := store.Get(context.Background(), created.Approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(loaded.ApproveCommand.Payload) || string(loaded.ApproveCommand.Payload) != `{"revision_id":"revision-1"}` {
		t.Fatalf("stored frozen command was mutated: %s", loaded.ApproveCommand.Payload)
	}
	replayed, err := store.Create(context.Background(), testApproval("approval-create-idempotent", ReviewModeDurable))
	if err != nil || replayed.Created {
		t.Fatalf("Create replay = %#v, err=%v", replayed, err)
	}
	conflict := testApproval("approval-create-idempotent-other", ReviewModeDurable)
	conflict.IdempotencyKey = created.Approval.IdempotencyKey
	if _, err := store.Create(context.Background(), conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("create conflict error = %v", err)
	}
}

func TestCandidateApprovalBatchFreezesTargetsIdempotently(t *testing.T) {
	store := NewMemoryStore()
	requested := CandidateApprovalBatch{
		ID: "candidate-batch-1", IdempotencyKey: "confirm-storyboard-1",
		SessionID: "session-1", StoryboardID: "storyboard-1", ExpectedStoryboardVersion: 7,
		Decision: DecisionApprove, ActorID: "user-1",
		Targets: []CandidateApprovalBatchTarget{{ApprovalID: "approval-1", BindingID: "binding-1", ExpectedDecisionVersion: 0}},
	}
	created, err := store.CreateCandidateApprovalBatch(context.Background(), requested)
	if err != nil || !created.Created {
		t.Fatalf("create candidate batch=%+v err=%v", created, err)
	}
	requested.Targets[0].BindingID = "mutated-by-caller"
	loaded, err := store.GetCandidateApprovalBatchByKey(context.Background(), "confirm-storyboard-1")
	if err != nil || loaded.Targets[0].BindingID != "binding-1" {
		t.Fatalf("loaded candidate batch=%+v err=%v", loaded, err)
	}
	replayed, err := store.CreateCandidateApprovalBatch(context.Background(), loaded)
	if err != nil || replayed.Created {
		t.Fatalf("replay candidate batch=%+v err=%v", replayed, err)
	}
	changed := loaded
	changed.ExpectedStoryboardVersion++
	if _, err := store.CreateCandidateApprovalBatch(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed batch replay error=%v", err)
	}
	empty := loaded
	empty.ID, empty.IdempotencyKey, empty.Targets = "candidate-batch-empty", "confirm-empty", nil
	if _, err := store.CreateCandidateApprovalBatch(context.Background(), empty); err == nil {
		t.Fatal("empty candidate batch was accepted")
	}
}

func TestMarkOutboxPublishedIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)
	store := NewMemoryStoreWithClock(func() time.Time { return now })
	created, err := store.Create(context.Background(), testApproval("approval-outbox-ack", ReviewModeDurable))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := store.Decide(context.Background(), DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision-outbox-ack",
		Decision: DecisionApprove, CurrentBinding: created.Approval.Binding, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ackAt := now.Add(time.Minute)
	if err := store.MarkOutboxPublished(context.Background(), decision.Outbox.ID, ackAt); err != nil {
		t.Fatalf("MarkOutboxPublished() error = %v", err)
	}
	if err := store.MarkOutboxPublished(context.Background(), decision.Outbox.ID, ackAt.Add(time.Hour)); err != nil {
		t.Fatalf("duplicate MarkOutboxPublished() error = %v", err)
	}
	pending, _ := store.ListOutbox(context.Background(), OutboxStatusPending, 100)
	if len(pending) != 0 {
		t.Fatalf("pending outbox after ACK = %#v", pending)
	}
	published, _ := store.ListOutbox(context.Background(), OutboxStatusPublished, 100)
	if len(published) != 1 || published[0].PublishedAt == nil || !published[0].PublishedAt.Equal(ackAt) {
		t.Fatalf("published outbox = %#v", published)
	}
	if err := store.MarkOutboxPublished(context.Background(), "missing-outbox", ackAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ACK error = %v, want ErrNotFound", err)
	}
}

func TestTargetBindingIgnoresUnrelatedAggregateVersion(t *testing.T) {
	binding := testApproval("binding", ReviewModeDurable).Binding
	current := binding
	current.StoryboardVersion += 99
	if !binding.Matches(current) {
		t.Fatal("target-scoped binding should ignore unrelated storyboard aggregate version")
	}
	current.PromptRevision++
	if binding.Matches(current) {
		t.Fatal("prompt revision change must invalidate target-scoped approval")
	}
}

func testApproval(id string, reviewMode ReviewMode) Approval {
	executionMode := ExecutionModeDurable
	if reviewMode == ReviewModeInterrupt {
		executionMode = ExecutionModeInterrupt
	}
	return Approval{
		ID: id, IdempotencyKey: "create:" + id, TenantID: "tenant-1", UserID: "user-1", SessionID: "session-1",
		ArtifactType: "storyboard_revision",
		Binding: VersionBinding{
			ArtifactID: "revision-1", ArtifactVersion: 2, StoryboardID: "storyboard-1", StoryboardVersion: 7,
			TargetID: "element-1", TargetRevision: 3, PromptRevision: 4, GenerationEpoch: 1,
		},
		ReviewMode: reviewMode, ExecutionMode: executionMode,
		ApproveCommand: FrozenCommand{Kind: "promote_revision", IdempotencyKey: fmt.Sprintf("%s:approve", id), Payload: json.RawMessage(`{"revision_id":"revision-1"}`)},
		RejectCommand:  FrozenCommand{Kind: "reject_revision", IdempotencyKey: fmt.Sprintf("%s:reject", id), Payload: json.RawMessage(`{"revision_id":"revision-1"}`)},
	}
}
