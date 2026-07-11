package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStoreDurableTurnLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("migrate runtime store: %v", err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	store = store.WithTx(tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sessionID := "runtime-pg-session-" + suffix
	input := NewUserMessage("runtime-pg-message-"+suffix, "runtime-pg-event-"+suffix)
	if _, err := store.EnqueueInput(ctx, sessionID, input); err != nil {
		t.Fatalf("enqueue input: %v", err)
	}
	lease, err := store.AcquireLease(ctx, sessionID, "runtime-pg-owner-"+suffix, time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute})
	if err != nil {
		t.Fatalf("claim input: %v", err)
	}
	turn, created, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil || !created {
		t.Fatalf("create turn = %#v, %t, %v", turn, created, err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	committed, err := store.CommitTurn(ctx, lease.Fence(), turn.TurnID, "digest")
	if err != nil || committed.Status != TurnStatusCommitted {
		t.Fatalf("commit turn = %#v, %v", committed, err)
	}
	if _, err := store.CommitTurn(ctx, lease.Fence(), turn.TurnID, "digest"); err != nil {
		t.Fatalf("idempotent commit: %v", err)
	}
	record, err := store.GetInput(ctx, input.InputID)
	if err != nil || record.Status != InputStatusResolved {
		t.Fatalf("resolved input = %#v, %v", record, err)
	}
	continuation, created, err := store.RequestContinuation(ctx, ApprovalContinuation{
		ApprovalID: "runtime-pg-approval-" + suffix, DecisionVersion: 1, SessionID: sessionID,
		Executor: ContinuationExecutorDeterministic, ExecutionEpoch: 1,
	})
	if err != nil || !created {
		t.Fatalf("request continuation = %#v, %t, %v", continuation, created, err)
	}
	claim := ContinuationClaim{ApprovalID: continuation.ApprovalID, DecisionVersion: 1, Executor: continuation.Executor, ExecutionEpoch: 1, LeaseOwner: "continuation-owner-" + suffix}
	if _, err := store.ClaimContinuation(ctx, claim, time.Minute); err != nil {
		t.Fatalf("claim continuation: %v", err)
	}
	if _, err := store.ApplyContinuation(ctx, claim, []ApprovalCommandLedger{{
		CommandKind: "activate", IdempotencyKey: "runtime-pg-command-" + suffix,
		CommandPayload: []byte(`{"revision":1}`), ResultPayload: []byte(`{"active":true}`),
	}}); err != nil {
		t.Fatalf("apply continuation: %v", err)
	}
	if command, err := store.GetCommand(ctx, continuation.ApprovalID, 1, "activate"); err != nil || command.ExecutionEpoch != 1 {
		t.Fatalf("command ledger = %#v, %v", command, err)
	}
	if err := store.ReleaseLease(ctx, lease.Fence()); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	newLease, err := store.AcquireLease(ctx, sessionID, "runtime-pg-owner-2-"+suffix, time.Minute)
	if err != nil || newLease.FenceToken != lease.FenceToken+1 {
		t.Fatalf("lease takeover = %#v, %v", newLease, err)
	}
	if err := store.ValidateFence(ctx, lease.Fence()); err == nil {
		t.Fatal("old fence unexpectedly remained valid")
	}
}

func TestPostgresDeadTurnAtomicallyAppendsTerminalEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	store = store.WithTx(tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sessionID := "runtime-pg-dead-" + suffix
	input := NewUserMessage("runtime-pg-dead-message-"+suffix, "runtime-pg-dead-event-"+suffix)
	if _, err := store.EnqueueInput(ctx, sessionID, input); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, sessionID, "runtime-pg-dead-owner-"+suffix, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	dead, err := store.DeadTurn(ctx, lease.Fence(), turn.TurnID, Failure{Code: "processor_failed", Message: "private provider detail"})
	if err != nil || dead.Status != TurnStatusDead {
		t.Fatalf("dead=%+v err=%v", dead, err)
	}
	rows, err := events.NewPostgresStore(tx).Tail(ctx, sessionID, events.TailOptions{AfterSeq: 0, Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("events=%+v err=%v", rows, err)
	}
	if rows[0].EventType != "a2ui.error" || rows[0].SourceKey != "turn:"+turn.TurnID+":dead" || strings.Contains(string(rows[0].Payload), "private provider detail") {
		t.Fatalf("terminal event=%+v", rows[0])
	}
}

func TestPostgresRetryingHeadBlocksLaterHigherPriorityInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("migrate runtime store: %v", err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	store = store.WithTx(tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sessionID := "runtime-pg-head-" + suffix
	first := NewBatchContinuationResult("runtime-pg-batch-"+suffix, 1, "runtime-pg-head-event-"+suffix)
	if _, err := store.EnqueueInput(ctx, sessionID, first); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease(ctx, sessionID, "runtime-pg-head-owner-"+suffix, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := store.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	retryAt := time.Now().Add(150 * time.Millisecond)
	if _, err := store.RetryTurnAt(ctx, lease.Fence(), turn.TurnID, retryAt, Failure{Code: "projection_failed"}); err != nil {
		t.Fatal(err)
	}
	later := NewUserMessage("runtime-pg-later-"+suffix, "runtime-pg-later-event-"+suffix)
	if _, err := store.EnqueueInput(ctx, sessionID, later); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute, MaxAttempts: 3}); !errors.Is(err, ErrNoInputAvailable) {
		t.Fatalf("later input bypassed PostgreSQL retrying head: %v", err)
	}
	// CURRENT_TIMESTAMP is stable for the lifetime of this rollback-only test
	// transaction, so make the retry due explicitly instead of sleeping.
	if err := tx.Exec("UPDATE aigc_session_inputs SET available_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE input_id = ?", first.InputID).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute, MaxAttempts: 3})
	if err != nil || claimed.InputID != first.InputID {
		t.Fatalf("retry claim = %+v, err=%v", claimed, err)
	}
	if _, err := store.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTurn(ctx, lease.Fence(), turn.TurnID, "head-committed"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimNext(ctx, ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute, MaxAttempts: 3})
	if err != nil || claimed.InputID != later.InputID {
		t.Fatalf("later claim after terminal head = %+v, err=%v", claimed, err)
	}
}
