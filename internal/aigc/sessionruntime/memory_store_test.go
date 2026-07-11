package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func TestMemoryStoreInputPriorityIdempotencyAndFencing(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)}
	store := NewMemoryStoreWithClock(clock.read)
	batch := NewBatchContinuationResult("batch-1", 1, "event-batch")
	user := NewUserMessage("message-1", "event-message")
	resume := NewResumeRequested("approval-1", 1, "event-resume")
	resume.MappingID, resume.MappingEpoch = "mapping-1", 1
	for _, input := range []SessionInput{batch, resume, user} {
		result, err := store.EnqueueInput(ctx, "session-1", input)
		if err != nil || !result.Enqueued {
			t.Fatalf("enqueue %#v = %#v, %v", input, result, err)
		}
	}
	if sessions, err := store.ListRunnableSessions(ctx, 10); err != nil || len(sessions) != 1 || sessions[0] != "session-1" {
		t.Fatalf("runnable sessions = %#v, %v", sessions, err)
	}
	retry, err := store.EnqueueInput(ctx, "session-1", user)
	if err != nil || retry.Enqueued || retry.Input.EnqueueSeq != 3 {
		t.Fatalf("idempotent enqueue = %#v, %v", retry, err)
	}
	lease, err := store.AcquireLease(ctx, "session-1", "owner-1", time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute, MaxAttempts: 3})
	if err != nil || claimed.InputType != InputTypeUserMessage {
		t.Fatalf("first claim = %#v, %v", claimed, err)
	}
	clock.advance(2 * time.Minute)
	newLease, err := store.AcquireLease(ctx, "session-1", "owner-2", time.Minute)
	if err != nil || newLease.FenceToken != lease.FenceToken+1 {
		t.Fatalf("takeover lease = %#v, %v", newLease, err)
	}
	if _, err := store.ResolveInput(ctx, lease.Fence(), claimed.InputID); !errors.Is(err, ErrFenceRejected) {
		t.Fatalf("stale owner resolve error = %v", err)
	}
	if count, err := store.RecoverExpiredInputs(ctx, newLease.Fence()); err != nil || count != 1 {
		t.Fatalf("recover inputs = %d, %v", count, err)
	}
}

func TestMemoryStoreGenericInterruptResumeIdempotencyAndDecode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	data := json.RawMessage(`{"selection":{"shot":3},"confirmed":true}`)
	input := NewInterruptResumeRequested(
		"mapping-generic", 7, "checkpoint-generic", "interrupt-generic",
		"continue with the selected shot", data, "event-generic-resume",
	)

	first, err := store.EnqueueInput(ctx, "session-generic", input)
	if err != nil || !first.Enqueued {
		t.Fatalf("enqueue generic resume = %#v, %v", first, err)
	}
	if first.Input.InputID != "checkpoint:mapping-generic:resume:7" || first.Input.SourceID != "mapping-generic:7" {
		t.Fatalf("generic resume identity = %#v", first.Input)
	}
	decoded, err := DecodeInput(first.Input)
	if err != nil {
		t.Fatalf("decode generic resume: %v", err)
	}
	resume, ok := decoded.(ResumeRequested)
	if !ok {
		t.Fatalf("decoded input type = %T", decoded)
	}
	if resume.InputID != input.InputID || resume.EventID != input.EventID ||
		resume.MappingID != input.MappingID || resume.MappingEpoch != input.MappingEpoch ||
		resume.CheckpointID != input.CheckpointID || resume.InterruptID != input.InterruptID ||
		resume.Content != input.Content || !reflect.DeepEqual(resume.Data, input.Data) {
		t.Fatalf("decoded generic resume = %#v, want %#v", resume, input)
	}

	replay, err := store.EnqueueInput(ctx, "session-generic", input)
	if err != nil || replay.Enqueued || replay.Input.InputID != first.Input.InputID {
		t.Fatalf("exact generic replay = %#v, %v", replay, err)
	}
	conflict := NewInterruptResumeRequested(
		"mapping-generic", 7, "checkpoint-generic", "interrupt-generic",
		"continue with the selected shot", json.RawMessage(`{"selection":{"shot":4},"confirmed":true}`), "event-generic-resume",
	)
	if _, err := store.EnqueueInput(ctx, "session-generic", conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed generic resume data error = %v", err)
	}
}

func TestMemoryStoreApprovalContinuationResultIsStableAndDecodable(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	input := NewApprovalContinuationResult("approval-continued", 2, 3, "")
	input.RequestedDecision = "approve"
	input.EffectiveStatus = "approved"
	input.ArtifactType = "storyboard_revision"
	input.ArtifactID = "revision-2"
	input.ArtifactVersion = 2
	input.StoryboardID = "board-1"
	input.StoryboardVersion = 7
	input.CommandKind = "PromoteStoryboardRevision"
	input.CommandResult = json.RawMessage(`{"status":"active"}`)
	bounded, err := WithContextMessageSeq(input, 11)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(ApprovalContinuationResult)

	first, err := store.EnqueueInput(ctx, "session-approval", input)
	if err != nil || !first.Enqueued || first.Input.InputType != InputTypeApprovalContinuation {
		t.Fatalf("enqueue approval continuation=%+v err=%v", first, err)
	}
	if first.Input.InputID != "approval:approval-continued:continuation-result:2" || first.Input.SourceID != "approval-continued:2" || first.Input.ContextMessageSeq != 11 {
		t.Fatalf("approval continuation identity=%+v", first.Input)
	}
	decoded, err := DecodeInput(first.Input)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.(ApprovalContinuationResult)
	if !ok || !reflect.DeepEqual(result, input) {
		t.Fatalf("decoded approval continuation=%#v, want=%#v", decoded, input)
	}
	replay, err := store.EnqueueInput(ctx, "session-approval", input)
	if err != nil || replay.Enqueued || replay.Input.EnqueueSeq != first.Input.EnqueueSeq {
		t.Fatalf("approval continuation replay=%+v err=%v", replay, err)
	}
	changed := input
	changed.CommandResult = json.RawMessage(`{"status":"different"}`)
	if _, err := store.EnqueueInput(ctx, "session-approval", changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed approval continuation error=%v", err)
	}
}

func TestMemoryStoreStableTurnRetryAndCommit(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)}
	store := NewMemoryStoreWithClock(clock.read)
	input := NewUserMessage("message-1", "event-1")
	bounded, err := WithContextMessageSeq(input, 7)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(UserMessage)
	_, _ = store.EnqueueInput(ctx, "session-1", input)
	lease, _ := store.AcquireLease(ctx, "session-1", "owner-1", time.Hour)
	claimed, _ := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	turn, created, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil || !created || turn.TurnID != StableTurnID("session-1", input.InputID) {
		t.Fatalf("create turn = %#v, %t, %v", turn, created, err)
	}
	if claimed.ContextMessageSeq != 7 || turn.ContextMessageSeq != 7 || !turn.ContextSeqFrozen {
		t.Fatalf("message boundary input=%d turn=%d frozen=%t", claimed.ContextMessageSeq, turn.ContextMessageSeq, turn.ContextSeqFrozen)
	}
	if _, created, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{}); err != nil || created {
		t.Fatalf("reuse turn created=%t err=%v", created, err)
	}
	turn, _ = store.BeginTurn(ctx, lease.Fence(), turn.TurnID)
	retryAt := clock.read().Add(time.Minute)
	if _, err := store.SaveTurnCheckpoint(ctx, lease.Fence(), turn.TurnID, "checkpoint-1"); err != nil {
		t.Fatalf("save retry checkpoint: %v", err)
	}
	if _, err := store.RetryTurnAt(ctx, lease.Fence(), turn.TurnID, retryAt, Failure{Code: "transient"}); err != nil {
		t.Fatalf("retry turn: %v", err)
	}
	if _, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour}); !errors.Is(err, ErrNoInputAvailable) {
		t.Fatalf("early retry claim error = %v", err)
	}
	clock.advance(time.Minute)
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil {
		t.Fatalf("retry claim: %v", err)
	}
	reused, created, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{TurnID: "different"})
	if err != nil || created || reused.TurnID != turn.TurnID {
		t.Fatalf("stable retry turn = %#v, %t, %v", reused, created, err)
	}
	if reused.RunnerCheckpointID != "checkpoint-1" {
		t.Fatalf("retry checkpoint = %q", reused.RunnerCheckpointID)
	}
	_, _ = store.BeginTurn(ctx, lease.Fence(), reused.TurnID)
	committed, err := store.CommitTurn(ctx, lease.Fence(), reused.TurnID, "digest-1")
	if err != nil || committed.Status != TurnStatusCommitted {
		t.Fatalf("commit turn = %#v, %v", committed, err)
	}
	// Commit is idempotent even though the input claim was atomically cleared.
	if again, err := store.CommitTurn(ctx, lease.Fence(), reused.TurnID, "digest-1"); err != nil || again.Status != TurnStatusCommitted {
		t.Fatalf("repeat commit = %#v, %v", again, err)
	}
	record, _ := store.GetInput(ctx, input.InputID)
	if record.Status != InputStatusResolved || record.ResolvedAt == nil {
		t.Fatalf("input after commit = %#v", record)
	}
}

func TestMemoryStoreRetryingHeadBlocksLaterHigherPriorityInput(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)}
	store := NewMemoryStoreWithClock(clock.read)
	first := NewBatchContinuationResult("batch-head", 1, "event-head")
	if _, err := store.EnqueueInput(ctx, "session-head", first); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-head", "owner-head", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour, MaxAttempts: 3})
	if err != nil || claimed.InputID != first.InputID {
		t.Fatalf("first claim = %+v, err=%v", claimed, err)
	}
	turn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	retryAt := clock.read().Add(time.Minute)
	if _, err := store.RetryTurnAt(ctx, lease.Fence(), turn.TurnID, retryAt, Failure{Code: "projection_failed"}); err != nil {
		t.Fatal(err)
	}

	// This user message has a higher priority than the batch input, but it was
	// enqueued after the batch had started and therefore cannot bypass it.
	later := NewUserMessage("message-later", "event-later")
	if _, err := store.EnqueueInput(ctx, "session-head", later); err != nil {
		t.Fatal(err)
	}
	if sessions, err := store.ListRunnableSessions(ctx, 10); err != nil || len(sessions) != 0 {
		t.Fatalf("backoff head should hide later runnable input: sessions=%v err=%v", sessions, err)
	}
	if _, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour, MaxAttempts: 3}); !errors.Is(err, ErrNoInputAvailable) {
		t.Fatalf("later input bypassed retrying head: %v", err)
	}

	clock.advance(time.Minute)
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour, MaxAttempts: 3})
	if err != nil || claimed.InputID != first.InputID {
		t.Fatalf("retry claim = %+v, err=%v", claimed, err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTurn(ctx, lease.Fence(), turn.TurnID, "head-committed"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour, MaxAttempts: 3})
	if err != nil || claimed.InputID != later.InputID {
		t.Fatalf("later claim after head terminal = %+v, err=%v", claimed, err)
	}
}

func TestMemoryStoreTurnOutputReceiptSurvivesRetry(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)}
	store := NewMemoryStoreWithClock(clock.read)
	input := NewUserMessage("message-output", "event-output")
	if _, err := store.EnqueueInput(ctx, "session-output", input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-output", "owner-output", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, []byte(`{"a":1}`), "digest-output"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("save output before begin error = %v", err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"b":2,"a":1}`)
	saved, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, payload, "digest-output")
	if err != nil || !jsonEqual(saved.OutputPayload, json.RawMessage(`{"a":1,"b":2}`)) || saved.OutputDigest != "digest-output" {
		t.Fatalf("save output receipt = %#v, %v", saved, err)
	}
	payload[2] = 'z'
	replayed, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, []byte(`{"a":1,"b":2}`), "digest-output")
	if err != nil || !replayed.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("replay output receipt = %#v, %v", replayed, err)
	}
	replayed.OutputPayload[0] = '['
	stored, err := store.GetTurn(ctx, turn.TurnID)
	if err != nil || !jsonEqual(stored.OutputPayload, json.RawMessage(`{"a":1,"b":2}`)) {
		t.Fatalf("cloned output receipt = %#v, %v", stored, err)
	}
	if _, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, []byte(`{"a":2,"b":2}`), "digest-output"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed output payload error = %v", err)
	}
	if _, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, []byte(`{"a":1,"b":2}`), "digest-other"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed output digest error = %v", err)
	}
	if _, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, nil, "digest-output"); err == nil {
		t.Fatal("empty output payload unexpectedly accepted")
	}
	if _, err := store.SaveTurnOutput(ctx, lease.Fence(), turn.TurnID, []byte(`not-json`), "digest-output"); err == nil {
		t.Fatal("invalid output payload unexpectedly accepted")
	}

	if _, err := store.RetryTurn(ctx, lease.Fence(), turn.TurnID, Failure{Code: "publish_failed"}); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	reused, created, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil || created || !jsonEqual(reused.OutputPayload, json.RawMessage(`{"a":1,"b":2}`)) || reused.OutputDigest != "digest-output" {
		t.Fatalf("retry reused output receipt = %#v, created=%t, err=%v", reused, created, err)
	}
	if replayed, err = store.SaveTurnOutput(ctx, lease.Fence(), reused.TurnID, json.RawMessage(`{"b":2,"a":1}`), "digest-output"); err != nil || replayed.Status != TurnStatusRetryWait {
		t.Fatalf("retry-wait output replay = %#v, %v", replayed, err)
	}
	reused, err = store.BeginTurn(ctx, lease.Fence(), reused.TurnID)
	if err != nil || !jsonEqual(reused.OutputPayload, json.RawMessage(`{"a":1,"b":2}`)) || reused.OutputDigest != "digest-output" {
		t.Fatalf("retry began with output receipt = %#v, %v", reused, err)
	}
}

func TestMemoryStoreTurnContextBoundaryFreezesZeroAcrossRetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	input := NewBatchContinuationResult("batch-empty-context", 1, "event-empty-context")
	if _, err := store.EnqueueInput(ctx, "session-empty-context", input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-empty-context", "owner-empty-context", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	turn, err = store.BeginTurn(ctx, lease.Fence(), turn.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := store.FreezeTurnContextMessageSeq(ctx, lease.Fence(), turn.TurnID, 0)
	if err != nil || !frozen.ContextSeqFrozen || frozen.ContextMessageSeq != 0 {
		t.Fatalf("freeze zero boundary = %+v, err=%v", frozen, err)
	}
	if _, err := store.RetryTurn(ctx, lease.Fence(), turn.TurnID, Failure{Code: "retry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.FreezeTurnContextMessageSeq(ctx, lease.Fence(), turn.TurnID, 9)
	if err != nil || !replayed.ContextSeqFrozen || replayed.ContextMessageSeq != 0 {
		t.Fatalf("zero boundary changed on retry = %+v, err=%v", replayed, err)
	}
}

func TestMemoryStoreBatchBoundaryUsesTerminalUserPredecessorsOnly(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	batch := NewBatchContinuationResult("batch-context", 1, "event-batch-context")
	if _, err := store.EnqueueInput(ctx, "session-context", batch); err != nil {
		t.Fatal(err)
	}
	predecessor := NewUserMessage("message-predecessor", "event-predecessor")
	bounded, err := WithContextMessageSeq(predecessor, 7)
	if err != nil {
		t.Fatal(err)
	}
	predecessor = bounded.(UserMessage)
	if _, err := store.EnqueueInput(ctx, "session-context", predecessor); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-context", "owner-context", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// The later-enqueued user input overtakes the never-started low-priority
	// batch and becomes a real terminal predecessor.
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil || claimed.InputID != predecessor.InputID {
		t.Fatalf("predecessor claim = %+v, err=%v", claimed, err)
	}
	userTurn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), userTurn.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTurn(ctx, lease.Fence(), userTurn.TurnID, "user-committed"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Hour})
	if err != nil || claimed.InputID != batch.InputID {
		t.Fatalf("batch claim = %+v, err=%v", claimed, err)
	}
	batchTurn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), batchTurn.TurnID); err != nil {
		t.Fatal(err)
	}
	later := NewUserMessage("message-later-context", "event-later-context")
	bounded, err = WithContextMessageSeq(later, 9)
	if err != nil {
		t.Fatal(err)
	}
	later = bounded.(UserMessage)
	if _, err := store.EnqueueInput(ctx, "session-context", later); err != nil {
		t.Fatal(err)
	}
	frozen, err := store.FreezeTurnContextFromTerminalUserInputs(ctx, lease.Fence(), batchTurn.TurnID)
	if err != nil || !frozen.ContextSeqFrozen || frozen.ContextMessageSeq != 7 {
		t.Fatalf("batch predecessor boundary = %+v, err=%v", frozen, err)
	}
}

func TestMemoryStoreContinuationFallbackAndCommandLedger(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)}
	store := NewMemoryStoreWithClock(clock.read)
	continuation, created, err := store.RequestContinuation(ctx, ApprovalContinuation{
		ApprovalID: "approval-1", DecisionVersion: 1, SessionID: "session-1",
		Executor: ContinuationExecutorRunnerResume, ExecutionEpoch: 1,
	})
	if err != nil || !created {
		t.Fatalf("request continuation = %#v, %t, %v", continuation, created, err)
	}
	claim := ContinuationClaim{ApprovalID: "approval-1", DecisionVersion: 1, Executor: ContinuationExecutorRunnerResume, ExecutionEpoch: 1, LeaseOwner: "worker-1"}
	if _, err := store.ClaimContinuation(ctx, claim, time.Minute); err != nil {
		t.Fatalf("claim continuation: %v", err)
	}
	if _, err := store.FallbackContinuation(ctx, "approval-1", 1, 1); !errors.Is(err, ErrContinuationClaimed) {
		t.Fatalf("fallback with active claim error = %v", err)
	}
	clock.advance(2 * time.Minute)
	fallback, err := store.FallbackContinuation(ctx, "approval-1", 1, 1)
	if err != nil || fallback.Executor != ContinuationExecutorDeterministic || fallback.ExecutionEpoch != 2 {
		t.Fatalf("fallback = %#v, %v", fallback, err)
	}
	claim = ContinuationClaim{ApprovalID: "approval-1", DecisionVersion: 1, Executor: fallback.Executor, ExecutionEpoch: fallback.ExecutionEpoch, LeaseOwner: "worker-2"}
	_, _ = store.ClaimContinuation(ctx, claim, time.Minute)
	applied, err := store.ApplyContinuation(ctx, claim, []ApprovalCommandLedger{{
		CommandKind: "promote_storyboard", IdempotencyKey: "approval:approval-1:decision:1:promote",
		CommandPayload: []byte(`{"revision":2}`), ResultPayload: []byte(`{"promoted":true}`),
	}})
	if err != nil || applied.Status != ContinuationStatusApplied {
		t.Fatalf("apply continuation = %#v, %v", applied, err)
	}
	command, err := store.GetCommand(ctx, "approval-1", 1, "promote_storyboard")
	if err != nil || command.ExecutionEpoch != 2 {
		t.Fatalf("command ledger = %#v, %v", command, err)
	}
}

func TestFailedContinuationCanBeReclaimed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _, err := store.RequestContinuation(ctx, ApprovalContinuation{ApprovalID: "approval-retry", DecisionVersion: 1, SessionID: "session-1", Executor: ContinuationExecutorDeterministic, ExecutionEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := ContinuationClaim{ApprovalID: "approval-retry", DecisionVersion: 1, Executor: ContinuationExecutorDeterministic, ExecutionEpoch: 1, LeaseOwner: "worker-1"}
	if _, err := store.ClaimContinuation(ctx, first, time.Minute); err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailContinuation(ctx, first, Failure{Code: "temporary", Message: "database unavailable"})
	if err != nil || failed.Status != ContinuationStatusFailed {
		t.Fatalf("failed continuation = %+v, err=%v", failed, err)
	}
	second := first
	second.LeaseOwner = "worker-2"
	reclaimed, err := store.ClaimContinuation(ctx, second, time.Minute)
	if err != nil || reclaimed.Status != ContinuationStatusClaimed || reclaimed.ErrorCode != "" || reclaimed.ErrorMessage != "" {
		t.Fatalf("reclaimed continuation = %+v, err=%v", reclaimed, err)
	}
}

func TestManagerProcessesDurableInput(t *testing.T) {
	store := NewMemoryStore()
	processed := make(chan string, 1)
	manager, err := NewManager(ManagerConfig{
		Store: store, OwnerID: "manager-1", LeaseTTL: 500 * time.Millisecond,
		RenewInterval: 100 * time.Millisecond, ClaimTTL: 500 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		Processor: ProcessorFunc(func(_ context.Context, input SessionInput, _ SessionTurnRun, _ Fence) (TurnResult, error) {
			processed <- input.InputIdentity().InputID
			return TurnResult{Outcome: TurnOutcomeCommit, OutputDigest: "ok"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := manager.Enqueue(context.Background(), "session-1", NewUserMessage("message-1", "event-1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := manager.StartSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if started, err := manager.EnsureSession(context.Background(), "session-1"); err != nil || started {
		t.Fatalf("duplicate ensure started=%t err=%v", started, err)
	}
	select {
	case inputID := <-processed:
		if inputID != "message:message-1" {
			t.Fatalf("processed input id = %s", inputID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for processor")
	}
	deadline := time.Now().Add(time.Second)
	for {
		record, _ := store.GetInput(context.Background(), "message:message-1")
		if record.Status == InputStatusResolved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("input was not resolved: %#v", record)
		}
		time.Sleep(5 * time.Millisecond)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("stop manager: %v", err)
	}
	if started, err := manager.EnsureSession(context.Background(), "session-2"); started || !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("ensure after stop started=%t err=%v", started, err)
	}
}

func TestManagerDeadTurnCreatesOneDurableTerminalEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	processorCalls := 0
	manager, err := NewManager(ManagerConfig{
		Store: store, Processor: ProcessorFunc(func(context.Context, SessionInput, SessionTurnRun, Fence) (TurnResult, error) {
			processorCalls++
			return TurnResult{}, errors.New("provider unavailable")
		}),
		OwnerID: "manager-dead", LeaseTTL: time.Hour, RenewInterval: time.Minute, ClaimTTL: time.Hour,
		MaxAttempts: 2, RetryBackoff: func(int, error) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := NewUserMessage("message-dead", "event-dead")
	if _, err := store.EnqueueInput(ctx, "session-dead", input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-dead", "manager-dead", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		progressed, drainErr := manager.drain(ctx, lease.Fence())
		if drainErr != nil || !progressed {
			t.Fatalf("drain %d progressed=%t err=%v", attempt+1, progressed, drainErr)
		}
	}
	record, err := store.GetInput(ctx, input.InputID)
	if err != nil || record.Status != InputStatusDead || processorCalls != 2 {
		t.Fatalf("input=%+v calls=%d err=%v", record, processorCalls, err)
	}
	if len(store.terminalEvents) != 1 {
		t.Fatalf("terminal events=%+v", store.terminalEvents)
	}
	for _, event := range store.terminalEvents {
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if event.EventType != "a2ui.error" || payload["retryable"] != false || payload["input_id"] != input.InputID {
			t.Fatalf("terminal event=%+v payload=%+v", event, payload)
		}
	}
}

func TestManagerFrozenOutputSurvivesAttemptBudgetAndCompletes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	processorCalls := 0
	manager, err := NewManager(ManagerConfig{
		Store: store, Processor: ProcessorFunc(func(ctx context.Context, _ SessionInput, turn SessionTurnRun, fence Fence) (TurnResult, error) {
			processorCalls++
			if processorCalls == 1 {
				if _, err := store.SaveTurnOutput(ctx, fence, turn.TurnID, json.RawMessage(`{"version":1}`), "frozen-digest"); err != nil {
					return TurnResult{}, err
				}
				return TurnResult{}, errors.New("event projection unavailable")
			}
			return TurnResult{Outcome: TurnOutcomeCommit, OutputDigest: "frozen-digest"}, nil
		}),
		OwnerID: "manager-frozen", LeaseTTL: time.Hour, RenewInterval: time.Minute, ClaimTTL: time.Hour,
		MaxAttempts: 1, RetryBackoff: func(int, error) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := NewUserMessage("message-frozen", "event-frozen")
	if _, err := store.EnqueueInput(ctx, "session-frozen", input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-frozen", "manager-frozen", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		progressed, drainErr := manager.drain(ctx, lease.Fence())
		if drainErr != nil || !progressed {
			t.Fatalf("drain %d progressed=%t err=%v", attempt+1, progressed, drainErr)
		}
	}
	record, err := store.GetInput(ctx, input.InputID)
	if err != nil || record.Status != InputStatusResolved || record.Attempts != 2 || processorCalls != 2 {
		t.Fatalf("input=%+v calls=%d err=%v", record, processorCalls, err)
	}
	turn, err := store.GetTurn(ctx, record.TurnID)
	if err != nil || turn.Status != TurnStatusCommitted || turn.OutputDigest != "frozen-digest" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if len(store.terminalEvents) != 0 {
		t.Fatalf("frozen output was dead-lettered: %+v", store.terminalEvents)
	}
}

func TestExhaustedRetryOutcomeCreatesTerminalEventWithoutRerunningProcessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	processorCalls := 0
	manager, err := NewManager(ManagerConfig{
		Store: store, Processor: ProcessorFunc(func(context.Context, SessionInput, SessionTurnRun, Fence) (TurnResult, error) {
			processorCalls++
			return TurnResult{Outcome: TurnOutcomeRetry, RetryAt: time.Now(), Failure: Failure{Code: "dependency_busy"}}, nil
		}),
		OwnerID: "manager-outcome-retry", LeaseTTL: time.Hour, RenewInterval: time.Minute, ClaimTTL: time.Hour,
		MaxAttempts: 1, RetryBackoff: func(int, error) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := NewUserMessage("message-outcome-retry", "event-outcome-retry")
	if _, err := store.EnqueueInput(ctx, "session-outcome-retry", input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, "session-outcome-retry", "manager-outcome-retry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if progressed, err := manager.drain(ctx, lease.Fence()); err != nil || !progressed {
		t.Fatalf("first drain progressed=%t err=%v", progressed, err)
	}
	if progressed, err := manager.drain(ctx, lease.Fence()); err != nil || progressed {
		t.Fatalf("exhaustion drain progressed=%t err=%v", progressed, err)
	}
	record, err := store.GetInput(ctx, input.InputID)
	if err != nil || record.Status != InputStatusDead || processorCalls != 1 || len(store.terminalEvents) != 1 {
		t.Fatalf("input=%+v calls=%d events=%+v err=%v", record, processorCalls, store.terminalEvents, err)
	}
}

func TestManagerDiscoveryRecoversInputWithoutWake(t *testing.T) {
	store := NewMemoryStore()
	processed := make(chan struct{}, 1)
	manager, err := NewManager(ManagerConfig{
		Store: store, OwnerID: "manager-discovery", LeaseTTL: time.Second,
		RenewInterval: 100 * time.Millisecond, ClaimTTL: time.Second, PollInterval: 10 * time.Millisecond, IdleTimeout: time.Second,
		Processor: ProcessorFunc(func(context.Context, SessionInput, SessionTurnRun, Fence) (TurnResult, error) {
			processed <- struct{}{}
			return TurnResult{Outcome: TurnOutcomeCommit}, nil
		}),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	// Bypass Manager.Enqueue to simulate an Outbox consumer commit followed by
	// a lost wakeup or a process restart.
	if _, err := store.EnqueueInput(context.Background(), "session-discovery", NewUserMessage("message-discovery", "event-discovery")); err != nil {
		t.Fatalf("persist input: %v", err)
	}
	if err := manager.Discover(context.Background()); err != nil {
		t.Fatalf("discover: %v", err)
	}
	select {
	case <-processed:
	case <-time.After(time.Second):
		t.Fatal("discovered input was not processed")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("stop manager: %v", err)
	}
}
