package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var postgresRunTestSequence atomic.Int64

func openPostgresRunStore(t *testing.T) (*PostgresRunStore, *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresRunStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store, db
}

func postgresRunID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-plan-run-%d-%d", time.Now().UnixNano(), postgresRunTestSequence.Add(1))
}

func postgresStorePlan() ExecutionPlan {
	return ExecutionPlan{
		PlanID: "persisted-plan", Source: "dynamic", Summary: "persist run", Direction: "image",
		Steps: []PlanStep{{ID: "first", Tool: "persist-test", Required: true}},
	}
}

func postgresStoreRun(t *testing.T, id string) PlanRun {
	t.Helper()
	return PlanRun{
		ID: id, SessionID: "session-1", UserID: "user-1", Plan: postgresStorePlan(), Status: RunStatusDraft,
		ResumeDecision: map[string]any{"large": json.Number("9007199254740993")},
		Nodes: map[string]*NodeRun{
			"first": {
				StepID: "first", Status: NodeStatusPending, ResumeDecision: map[string]any{},
				ResumeKey: "persisted-resume", SuspensionGeneration: 7,
				GuardApproval: &GuardApprovalReceipt{Fingerprint: "guard-fingerprint", Attempt: 2, Status: guardApprovalPending},
			},
		},
	}
}

func TestPostgresRunStoreRoundTripAndCAS(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })

	created, err := store.CreateRun(ctx, postgresStoreRun(t, id))
	if err != nil || created.Version != 1 || created.Status != RunStatusDraft {
		t.Fatalf("CreateRun() = %+v, %v", created, err)
	}
	large, ok := created.ResumeDecision["large"].(json.Number)
	if !ok || large.String() != "9007199254740993" {
		t.Fatalf("large number = %#v (%T)", created.ResumeDecision["large"], created.ResumeDecision["large"])
	}
	created.Nodes["first"].ResumeKey = "caller-mutated"
	stored, err := store.GetRun(ctx, id)
	if err != nil || stored.Nodes["first"].ResumeKey != "persisted-resume" || stored.Nodes["first"].SuspensionGeneration != 7 || stored.Nodes["first"].GuardApproval == nil {
		t.Fatalf("GetRun() = %+v, %v", stored, err)
	}

	updated, err := store.MutateRun(ctx, id, 1, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		run.Nodes["first"].Outputs = map[string]any{"large": json.Number("9223372036854775807")}
		return nil
	})
	if err != nil || updated.Version != 2 || updated.Status != RunStatusRunning {
		t.Fatalf("MutateRun() = %+v, %v", updated, err)
	}
	if _, err := store.MutateRun(ctx, id, 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale mutation error = %v", err)
	}
	if _, err := store.CreateRun(ctx, postgresStoreRun(t, id)); !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("duplicate create error = %v", err)
	}
	if _, err := store.GetRun(ctx, postgresRunID(t)); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
}

func TestPostgresRunStoreMutationRollbackAndConcurrentCAS(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	created, err := store.CreateRun(ctx, postgresStoreRun(t, id))
	if err != nil {
		t.Fatal(err)
	}

	callbackErr := errors.New("callback failed")
	if _, err := store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
		run.SessionID = "must-rollback"
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("callback error = %v", err)
	}
	if _, err := store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusSucceeded
		return nil
	}); err == nil {
		t.Fatal("invalid transition unexpectedly succeeded")
	}
	if _, err := store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		run.SessionID = "must-also-rollback"
		run.Nodes["first"].Outputs = map[string]any{"invalid": make(chan struct{})}
		return nil
	}); !errors.Is(err, ErrRunNotSerializable) {
		t.Fatalf("serialization error = %v", err)
	}
	afterRollback, err := store.GetRun(ctx, id)
	if err != nil || afterRollback.Version != 1 || afterRollback.SessionID != "session-1" || afterRollback.Status != RunStatusDraft {
		t.Fatalf("after rollback = %+v, %v", afterRollback, err)
	}

	var successes atomic.Int32
	var conflicts atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, mutateErr := store.MutateRun(ctx, id, 1, func(run *PlanRun) error {
				run.Status = RunStatusRunning
				return nil
			})
			switch {
			case mutateErr == nil:
				successes.Add(1)
			case errors.Is(mutateErr, ErrRunVersionConflict):
				conflicts.Add(1)
			default:
				t.Errorf("concurrent mutation error = %v", mutateErr)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 7 {
		t.Fatalf("successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
}

func TestPostgresRunStoreRejectsMetadataPayloadDrift(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	if _, err := store.CreateRun(ctx, postgresStoreRun(t, id)); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&planRunRecord{}).Where("id = ?", id).Update("status", RunStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRun(ctx, id); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("drift error = %v", err)
	}
}

func TestPostgresRunStoreAutoMigrateRejectsNilDB(t *testing.T) {
	if err := NewPostgresRunStore(nil).AutoMigrate(context.Background()); err == nil {
		t.Fatal("AutoMigrate(nil DB) unexpectedly succeeded")
	}
	var db *gorm.DB
	if err := NewPostgresRunStore(db).AutoMigrate(context.Background()); err == nil {
		t.Fatal("AutoMigrate(typed nil DB) unexpectedly succeeded")
	}
}

func TestSchedulerRecoversSuspendedRunFromPostgres(t *testing.T) {
	store, db := openPostgresRunStore(t)
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	var interactionCalls atomic.Int32
	var downstreamCalls atomic.Int32
	registry := schedulerRegistry(t,
		schedulerTool{key: "ask", category: "interaction", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			interactionCalls.Add(1)
			return vocabulary.Result{Outputs: map[string]any{"prompt": "persisted"}, Suspension: &vocabulary.Suspension{Reason: SuspendWaitingUser, Payload: map[string]any{"question": "continue?"}}}, nil
		}},
		schedulerTool{key: "after", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
			downstreamCalls.Add(1)
			return vocabulary.Result{Outputs: map[string]any{"done": true}}, nil
		}},
	)
	firstCfg := schedulerConfigForTest(store, registry, func() string { return id })
	first, err := NewScheduler(firstCfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := first.Submit(context.Background(), "session-recovery", "user-recovery", ExecutionPlan{
		PlanID: "recovery", Source: "dynamic", Summary: "recover", Direction: "image",
		Steps: []PlanStep{{ID: "ask", Tool: "ask", Required: true}, {ID: "after", Tool: "after", DependsOn: []string{"ask"}, Required: true}},
	})
	if err != nil || suspended.Status != RunStatusSuspended || suspended.Nodes["ask"].ResumeKey == "" {
		t.Fatalf("Submit() = %+v, %v", suspended, err)
	}
	resumeKey := suspended.Nodes["ask"].ResumeKey

	secondCfg := schedulerConfigForTest(store, registry, func() string { return "unused" })
	second, err := NewScheduler(secondCfg)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := second.Resume(context.Background(), id, resumeKey, map[string]any{"approved": true})
	if err != nil || completed.Status != RunStatusSucceeded || interactionCalls.Load() != 1 || downstreamCalls.Load() != 1 {
		t.Fatalf("Resume() = %+v, %v interaction=%d downstream=%d", completed, err, interactionCalls.Load(), downstreamCalls.Load())
	}
	persisted, err := store.GetRun(context.Background(), id)
	if err != nil || persisted.Nodes["ask"].ResumeKey != resumeKey || persisted.Nodes["ask"].SuspensionGeneration != 1 || !persisted.Nodes["ask"].Resumed {
		t.Fatalf("persisted = %+v, %v", persisted, err)
	}
}

func TestSchedulerPostgresLeaseUsesDatabaseClockDespiteProcessSkew(t *testing.T) {
	store, db := openPostgresRunStore(t)
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	tool := schedulerTool{key: "block", run: func(ctx context.Context, _ vocabulary.Call) (vocabulary.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return vocabulary.Result{Outputs: map[string]any{"done": true}}, nil
		case <-ctx.Done():
			return vocabulary.Result{}, ctx.Err()
		}
	}}
	registry := schedulerRegistry(t, tool)
	firstCfg := schedulerConfigForTest(store, registry, func() string { return id })
	firstCfg.Now = func() time.Time { return time.Now().Add(-24 * time.Hour) }
	firstCfg.LeaseTTL = 10 * time.Second
	firstCfg.HeartbeatInterval = 20 * time.Millisecond
	first, err := NewScheduler(firstCfg)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, submitErr := first.Submit(context.Background(), "clock-session", "clock-user", ExecutionPlan{
			PlanID: "clock", Source: "dynamic", Summary: "clock", Direction: "image",
			Steps: []PlanStep{{ID: "block", Tool: "block", Required: true}},
		})
		result <- submitErr
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first scheduler did not start tool")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		persisted, getErr := store.GetRun(context.Background(), id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if persisted.Version >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first scheduler did not persist a heartbeat")
		}
		time.Sleep(10 * time.Millisecond)
	}

	secondCfg := schedulerConfigForTest(store, registry, func() string { return "unused" })
	secondCfg.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	secondCfg.LeaseTTL = 10 * time.Second
	secondCfg.HeartbeatInterval = 20 * time.Millisecond
	second, err := NewScheduler(secondCfg)
	if err != nil {
		t.Fatal(err)
	}
	advanceCtx, cancelAdvance := context.WithTimeout(context.Background(), time.Second)
	defer cancelAdvance()
	observed, err := second.Advance(advanceCtx, id)
	if err != nil || observed.Nodes["block"].ExecutionEpoch != 1 || calls.Load() != 1 {
		t.Fatalf("skewed Advance() = %+v, %v calls=%d", observed, err, calls.Load())
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("first Submit() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first scheduler did not finish")
	}
}

func TestPostgresHeartbeatCannotRenewLeaseThatExpiresWhileWaitingForRowLock(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	now, err := store.AuthoritativeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaseUntil := now.Add(800 * time.Millisecond)
	run := postgresStoreRun(t, id)
	run.Status = RunStatusRunning
	run.Nodes["first"] = &NodeRun{
		StepID: "first", Status: NodeStatusRunning, Attempt: 1, ExecutionEpoch: 1,
		ExecutionOwner: "owner-a", ExecutionToken: "owner-a:token-a", LeaseUntil: &leaseUntil,
		ResumeDecision: map[string]any{},
	}
	created, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}

	lockTx := lockPostgresPlanRun(t, db, id)
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, schedulerTool{key: "persist-test"}), func() string { return "unused" })
	cfg.OwnerID = "owner-a"
	cfg.LeaseTTL = 3 * time.Second
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claim := executionClaim{StepID: "first", Attempt: 1, Epoch: 1, Owner: "owner-a", Token: "owner-a:token-a"}
	renewed := make(chan error, 1)
	go func() { renewed <- scheduler.renewClaims(ctx, id, []executionClaim{claim}) }()
	assertStillBlocked(t, renewed)
	waitForPostgresTimeAfter(t, store, leaseUntil.Add(100*time.Millisecond))
	if err := lockTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-renewed:
		if !errors.Is(err, ErrExecutionClaimLost) {
			t.Fatalf("renewClaims() error = %v, want ErrExecutionClaimLost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renewClaims did not return after releasing row lock")
	}
	persisted, err := store.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Version != created.Version || !persisted.Nodes["first"].LeaseUntil.Equal(leaseUntil) {
		t.Fatalf("expired heartbeat changed run: version=%d lease=%v", persisted.Version, persisted.Nodes["first"].LeaseUntil)
	}
}

func TestPostgresClaimLeaseStartsAfterWaitingForRowLock(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	run := postgresStoreRun(t, id)
	run.Status = RunStatusRunning
	run.Nodes["first"] = &NodeRun{StepID: "first", Status: NodeStatusPending, ResumeDecision: map[string]any{}}
	created, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	beforeWait, err := store.AuthoritativeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}

	lockTx := lockPostgresPlanRun(t, db, id)
	leaseTTL := time.Second
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, schedulerTool{key: "persist-test"}), func() string { return "unused" })
	cfg.LeaseTTL = leaseTTL
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		run    PlanRun
		claims []executionClaim
		err    error
	}
	result := make(chan claimResult, 1)
	go func() {
		claimed, claims, claimErr := scheduler.claimReady(ctx, created)
		result <- claimResult{run: claimed, claims: claims, err: claimErr}
	}()
	assertStillBlocked(t, result)
	waitForPostgresTimeAfter(t, store, beforeWait.Add(leaseTTL+300*time.Millisecond))
	if err := lockTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	var got claimResult
	select {
	case got = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("claimReady did not return after releasing row lock")
	}
	if got.err != nil || len(got.claims) != 1 {
		t.Fatalf("claimReady() run=%+v claims=%+v err=%v", got.run, got.claims, got.err)
	}
	afterClaim, err := store.AuthoritativeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lease := got.run.Nodes["first"].LeaseUntil
	if lease == nil || !lease.After(afterClaim.Add(700*time.Millisecond)) {
		t.Fatalf("lease=%v database_now=%v; lease did not start after row lock", lease, afterClaim)
	}
}

func TestPostgresReclaimChecksExpiryAfterWaitingForRowLock(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	now, err := store.AuthoritativeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldLease := now.Add(800 * time.Millisecond)
	run := postgresStoreRun(t, id)
	run.Status = RunStatusRunning
	run.Nodes["first"] = &NodeRun{
		StepID: "first", Status: NodeStatusRunning, Attempt: 1, ExecutionEpoch: 1,
		ExecutionOwner: "owner-old", ExecutionToken: "owner-old:token-old", LeaseUntil: &oldLease,
		ResumeDecision: map[string]any{},
	}
	created, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}

	lockTx := lockPostgresPlanRun(t, db, id)
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, schedulerTool{key: "persist-test"}), func() string { return "unused" })
	cfg.OwnerID = "owner-new"
	cfg.LeaseTTL = time.Second
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	type reclaimResult struct {
		run    PlanRun
		claims []executionClaim
		err    error
	}
	result := make(chan reclaimResult, 1)
	go func() {
		claimed, claims, claimErr := scheduler.claimReady(ctx, created)
		result <- reclaimResult{run: claimed, claims: claims, err: claimErr}
	}()
	assertStillBlocked(t, result)
	waitForPostgresTimeAfter(t, store, oldLease.Add(100*time.Millisecond))
	if err := lockTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || len(got.claims) != 1 {
			t.Fatalf("reclaim run=%+v claims=%+v err=%v", got.run, got.claims, got.err)
		}
		node := got.run.Nodes["first"]
		if node.ExecutionOwner != "owner-new" || node.ExecutionEpoch != 2 || node.Attempt != 1 {
			t.Fatalf("reclaimed node = %+v", node)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not return after releasing row lock")
	}
}

func lockPostgresPlanRun(t *testing.T, db *gorm.DB, id string) *gorm.DB {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	var record planRunRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return tx
}

func waitForPostgresTimeAfter(t *testing.T, store *PostgresRunStore, target time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		now, err := store.AuthoritativeNow(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if now.After(target) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("database clock did not pass %v", target)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func assertStillBlocked[T any](t *testing.T, result <-chan T) {
	t.Helper()
	select {
	case got := <-result:
		t.Fatalf("operation returned before row lock release: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
}
