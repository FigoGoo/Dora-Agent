package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
	"github.com/jackc/pgx/v5/pgconn"
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
	sqlDB, err := db.DB()
	if err != nil {
		if closer, ok := db.ConnPool.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		t.Fatalf("load agent postgres pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close agent postgres pool: %v", err)
		}
	})
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
	if err != nil || created.Version != 1 || created.Status != RunStatusDraft || created.RequestKey != id || created.SubmitRequestFingerprint == "" {
		t.Fatalf("CreateRun() = %+v, %v", created, err)
	}
	byRequest, err := store.GetRunByRequestKey(ctx, "session-1", id)
	if err != nil || byRequest.ID != id {
		t.Fatalf("GetRunByRequestKey() = %+v, %v", byRequest, err)
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

func TestPostgresRunStoreEnforcesSingleActiveRunPerSession(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	sessionID := postgresRunID(t) + "-session"
	ids := []string{postgresRunID(t), postgresRunID(t), postgresRunID(t)}
	t.Cleanup(func() {
		db.WithContext(context.Background()).Where("session_id = ?", sessionID).Delete(&planRunRecord{})
	})
	runs := []PlanRun{
		{ID: ids[0], SessionID: sessionID, UserID: "user", Status: RunStatusRunning, Nodes: map[string]*NodeRun{}},
		{ID: ids[1], SessionID: sessionID, UserID: "user", Status: RunStatusSuspended, Nodes: map[string]*NodeRun{}},
	}
	start := make(chan struct{})
	type result struct {
		run PlanRun
		err error
	}
	results := make(chan result, len(runs))
	for _, run := range runs {
		run := run
		go func() {
			<-start
			created, err := store.CreateRun(ctx, run)
			results <- result{run: created, err: err}
		}()
	}
	close(start)
	var created PlanRun
	var successes, conflicts int
	for range runs {
		result := <-results
		if result.err == nil {
			successes++
			created = result.run
		} else if errors.Is(result.err, ErrSessionActiveRun) {
			conflicts++
		} else {
			t.Fatalf("CreateRun() error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	active, err := store.GetActiveRun(ctx, sessionID)
	if err != nil || active.ID != created.ID {
		t.Fatalf("GetActiveRun() = %+v, %v", active, err)
	}
	if _, err := store.MutateRun(ctx, created.ID, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusCancelled
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRun(ctx, PlanRun{ID: ids[2], SessionID: sessionID, UserID: "user", Status: RunStatusDraft, Nodes: map[string]*NodeRun{}}); err != nil {
		t.Fatalf("terminal status did not release session slot: %v", err)
	}
}

func TestPostgresSchedulersConvergeIdenticalConcurrentSubmit(t *testing.T) {
	store, db := openPostgresRunStore(t)
	sessionID := postgresRunID(t) + "-scheduler-session"
	t.Cleanup(func() {
		db.WithContext(context.Background()).Where("session_id = ?", sessionID).Delete(&planRunRecord{})
	})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	tool := schedulerTool{key: "block", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return vocabulary.Result{}, nil
	}}
	registry := schedulerRegistry(t, tool)
	leftCfg := schedulerConfigForTest(store, registry, func() string { return postgresRunID(t) })
	rightCfg := schedulerConfigForTest(store, registry, func() string { return postgresRunID(t) })
	left, _ := NewScheduler(leftCfg)
	right, _ := NewScheduler(rightCfg)
	plan := activeTestPlan("same", "block")
	type result struct {
		run PlanRun
		err error
	}
	leftResult := make(chan result, 1)
	go func() {
		run, err := left.SubmitWithKey(context.Background(), sessionID, "user", "postgres-message", plan)
		leftResult <- result{run: run, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first scheduler did not start tool")
	}
	rightRun, rightErr := right.SubmitWithKey(context.Background(), sessionID, "user", "postgres-message", plan)
	if rightErr != nil || rightRun.ID == "" {
		t.Fatalf("right Submit() = %+v, %v", rightRun, rightErr)
	}
	releaseOnce.Do(func() { close(release) })
	leftDone := <-leftResult
	if leftDone.err != nil || leftDone.run.ID != rightRun.ID || leftDone.run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("left=%+v right=%+v calls=%d", leftDone, rightRun, calls.Load())
	}
}

func TestPostgresSchedulerReplaysTerminalSubmitWithKey(t *testing.T) {
	store, db := openPostgresRunStore(t)
	sessionID := postgresRunID(t) + "-terminal-key-session"
	t.Cleanup(func() {
		db.WithContext(context.Background()).Where("session_id = ?", sessionID).Delete(&planRunRecord{})
	})
	var calls atomic.Int32
	tool := schedulerTool{key: "terminal-key", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		calls.Add(1)
		return vocabulary.Result{}, nil
	}}
	var ids atomic.Int32
	cfg := schedulerConfigForTest(store, schedulerRegistry(t, tool), func() string { return fmt.Sprintf("%s-%d", sessionID, ids.Add(1)) })
	scheduler, _ := NewScheduler(cfg)
	plan := activeTestPlan("terminal-key", "terminal-key")
	first, err := scheduler.SubmitWithKey(context.Background(), sessionID, "user", "message-terminal", plan)
	if err != nil || first.Status != RunStatusSucceeded {
		t.Fatalf("first SubmitWithKey() = %+v, %v", first, err)
	}
	replayed, err := scheduler.SubmitWithKey(context.Background(), sessionID, "user", "message-terminal", plan)
	if err != nil || replayed.ID != first.ID || calls.Load() != 1 {
		t.Fatalf("terminal replay = %+v calls=%d err=%v", replayed, calls.Load(), err)
	}
}

func TestPostgresSchedulersRejectDifferentConcurrentSubmit(t *testing.T) {
	store, db := openPostgresRunStore(t)
	sessionID := postgresRunID(t) + "-different-session"
	t.Cleanup(func() {
		db.WithContext(context.Background()).Where("session_id = ?", sessionID).Delete(&planRunRecord{})
	})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	tool := schedulerTool{key: "block-different", run: func(context.Context, vocabulary.Call) (vocabulary.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return vocabulary.Result{}, nil
	}}
	registry := schedulerRegistry(t, tool)
	leftCfg := schedulerConfigForTest(store, registry, func() string { return postgresRunID(t) })
	rightCfg := schedulerConfigForTest(store, registry, func() string { return postgresRunID(t) })
	left, _ := NewScheduler(leftCfg)
	right, _ := NewScheduler(rightCfg)
	type result struct {
		run PlanRun
		err error
	}
	leftResult := make(chan result, 1)
	go func() {
		run, err := left.Submit(context.Background(), sessionID, "user", activeTestPlan("left", "block-different"))
		leftResult <- result{run: run, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first scheduler did not start tool")
	}
	if _, err := right.Submit(context.Background(), sessionID, "user", activeTestPlan("right", "block-different")); !errors.Is(err, ErrSessionActiveRun) {
		t.Fatalf("right Submit() error = %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	leftDone := <-leftResult
	if leftDone.err != nil || leftDone.run.Status != RunStatusSucceeded || calls.Load() != 1 {
		t.Fatalf("left=%+v calls=%d", leftDone, calls.Load())
	}
	var count int64
	if err := db.Model(&planRunRecord{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("persisted runs=%d err=%v", count, err)
	}
}

func TestPostgresRunStoreRejectsIdentityMutation(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	sessionID := id + "-session"
	t.Cleanup(func() {
		db.WithContext(context.Background()).Where("session_id = ?", sessionID).Delete(&planRunRecord{})
	})
	created, err := store.CreateRun(ctx, PlanRun{ID: id, SessionID: sessionID, Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, timed := range []bool{false, true} {
		var mutateErr error
		if timed {
			_, mutateErr = store.MutateRunAtAuthoritativeNow(ctx, id, created.Version, func(run *PlanRun, _ time.Time) error {
				run.ID = "changed"
				return nil
			})
		} else {
			_, mutateErr = store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
				run.ID = "changed"
				return nil
			})
		}
		if !errors.Is(mutateErr, ErrRunRecordCorrupt) {
			t.Fatalf("timed=%v error=%v", timed, mutateErr)
		}
		stored, getErr := store.GetRun(ctx, id)
		if getErr != nil || stored.ID != id || stored.Version != created.Version {
			t.Fatalf("timed=%v stored=%+v err=%v", timed, stored, getErr)
		}
	}
	if _, err := store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
		run.RequestKey = "changed-key"
		return nil
	}); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("request key mutation error = %v", err)
	}
	if _, err := store.MutateRun(ctx, id, created.Version, func(run *PlanRun) error {
		run.SubmitRequestFingerprint = "changed-fingerprint"
		return nil
	}); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("fingerprint mutation error = %v", err)
	}
}

func TestPostgresRunStoreSessionMigrationEnforcesActiveSlotAtomically(t *testing.T) {
	for _, timed := range []bool{false, true} {
		t.Run(fmt.Sprintf("timed_%v", timed), func(t *testing.T) {
			store, db := openPostgresRunStore(t)
			prefix := postgresRunID(t)
			source, target, free := prefix+"-source", prefix+"-target", prefix+"-free"
			t.Cleanup(func() {
				db.WithContext(context.Background()).Where("session_id IN ?", []string{source, target, free}).Delete(&planRunRecord{})
			})
			occupied, err := store.CreateRun(context.Background(), PlanRun{ID: prefix + "-occupied", SessionID: target, Status: RunStatusRunning, Nodes: map[string]*NodeRun{}})
			if err != nil {
				t.Fatal(err)
			}
			moving, err := store.CreateRun(context.Background(), PlanRun{ID: prefix + "-moving", SessionID: source, Status: RunStatusRunning, Nodes: map[string]*NodeRun{}})
			if err != nil {
				t.Fatal(err)
			}
			_, mutateErr := mutateSessionForTest(store, timed, moving, target, RunStatusRunning)
			if !errors.Is(mutateErr, ErrSessionActiveRun) {
				t.Fatalf("occupied migration error = %v", mutateErr)
			}
			for _, before := range []PlanRun{occupied, moving} {
				after, getErr := store.GetRun(context.Background(), before.ID)
				if getErr != nil || !reflect.DeepEqual(after, before) {
					t.Fatalf("rollback run %s = %+v, %v", before.ID, after, getErr)
				}
			}
			migrated, err := mutateSessionForTest(store, timed, moving, free, RunStatusRunning)
			if err != nil || migrated.SessionID != free {
				t.Fatalf("free migration = %+v, %v", migrated, err)
			}
			terminal, err := mutateSessionForTest(store, timed, migrated, target, RunStatusCancelled)
			if err != nil || terminal.SessionID != target || terminal.Status != RunStatusCancelled {
				t.Fatalf("terminal migration = %+v, %v", terminal, err)
			}
		})
	}
}

func TestPostgresRunStoreConcurrentSameIDDifferentSessionsReturnsDuplicateSentinel(t *testing.T) {
	store, db := openPostgresRunStore(t)
	for iteration := range 10 {
		id := postgresRunID(t)
		t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
		const callers = 16
		start := make(chan struct{})
		results := make(chan error, callers)
		for caller := range callers {
			caller := caller
			go func() {
				<-start
				_, err := store.CreateRun(context.Background(), PlanRun{
					ID: id, SessionID: fmt.Sprintf("%s-session-%d-%d", id, iteration, caller), Status: RunStatusRunning, Nodes: map[string]*NodeRun{},
				})
				results <- err
			}()
		}
		close(start)
		var successes, duplicates int
		for range callers {
			err := <-results
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrRunAlreadyExists):
				duplicates++
			default:
				t.Fatalf("iteration %d unexpected CreateRun error = %v", iteration, err)
			}
		}
		if successes != 1 || duplicates != callers-1 {
			t.Fatalf("iteration %d successes=%d duplicates=%d", iteration, successes, duplicates)
		}
	}
}

func TestPostgresRunConstraintMappingPreservesCauseBehindDomainError(t *testing.T) {
	for _, test := range []struct {
		constraint string
		want       error
	}{
		{constraint: planRunPrimaryKeyConstraint, want: ErrRunAlreadyExists},
		{constraint: activeRunSessionIndex, want: ErrSessionActiveRun},
		{constraint: requestKeySessionIndex, want: ErrSubmitRequestKeyExists},
	} {
		raw := &pgconn.PgError{Code: "23505", ConstraintName: test.constraint, Message: "raw duplicate detail"}
		mapped := mapPlanRunConstraintError(raw, "run")
		if !errors.Is(mapped, test.want) {
			t.Fatalf("constraint %s mapped error = %v", test.constraint, mapped)
		}
		var retained *pgconn.PgError
		if !errors.As(mapped, &retained) || retained != raw {
			t.Fatalf("constraint %s lost pg cause", test.constraint)
		}
		if strings.Contains(mapped.Error(), raw.Message) {
			t.Fatalf("constraint %s leaked raw detail: %v", test.constraint, mapped)
		}
	}
}

func TestPostgresRunStoreAutoMigrateRejectsExistingDuplicateActiveSessions(t *testing.T) {
	_, db := openPostgresRunStore(t)
	ctx := context.Background()
	rollback := errors.New("rollback migration fixture")
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP INDEX IF EXISTS idx_aigc_plan_runs_one_active_session").Error; err != nil {
			return err
		}
		sessionID := postgresRunID(t) + "-duplicate-session"
		for index := range 2 {
			run := PlanRun{ID: fmt.Sprintf("%s-%d", sessionID, index), RequestKey: fmt.Sprintf("duplicate-%d", index), SessionID: sessionID, Status: RunStatusRunning, Version: 1, Nodes: map[string]*NodeRun{}}
			record, _, encodeErr := encodePlanRunRecord(run)
			if encodeErr != nil {
				return encodeErr
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		if migrateErr := NewPostgresRunStore(tx).AutoMigrate(ctx); migrateErr == nil {
			t.Fatal("AutoMigrate() unexpectedly accepted duplicate active sessions")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("fixture transaction error = %v", err)
	}
}

func TestPostgresRunStoreAutoMigrateBackfillsLegacyRequestKeyAndPayload(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	sessionID := id + "-legacy-session"
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	created, err := store.CreateRun(ctx, PlanRun{ID: id, RequestKey: "original-key", SessionID: sessionID, Status: RunStatusSucceeded, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP INDEX IF EXISTS idx_aigc_plan_runs_session_request_key").Error; err != nil {
			return err
		}
		if err := tx.Exec("ALTER TABLE aigc_plan_runs ALTER COLUMN request_key DROP NOT NULL").Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE aigc_plan_runs SET request_key = NULL, payload = payload - 'request_key' - 'submit_request_fingerprint' WHERE id = ?", created.ID).Error; err != nil {
			return err
		}
		return NewPostgresRunStore(tx).AutoMigrate(ctx)
	}); err != nil {
		t.Fatalf("legacy AutoMigrate() error = %v", err)
	}
	backfilled, err := store.GetRun(ctx, id)
	wantFingerprint, fingerprintErr := computeSubmitRequestFingerprint(backfilled.SessionID, backfilled.UserID, id, backfilled.Plan)
	if fingerprintErr != nil || err != nil || backfilled.RequestKey != id || backfilled.SubmitRequestFingerprint != wantFingerprint {
		t.Fatalf("backfilled run = %+v, get=%v fingerprint=%v", backfilled, err, fingerprintErr)
	}
}

func TestPostgresRunStorePersistsCancellationIntent(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })

	run := postgresStoreRun(t, id)
	run.Status = RunStatusSuspended
	run.SuspendReason = SuspendWaitingJobs
	run.SuspendedNodeID = "first"
	run.CancelRequested = true
	run.CancelReason = "user stop"
	run.CancelBatchID = "batch-1"
	run.CancelNodeID = "first"
	run.Nodes["first"].Status = NodeStatusRunning
	run.Nodes["first"].Suspension = &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": "batch-1"}}
	created, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetRun(ctx, created.ID)
	if err != nil || !stored.CancelRequested || stored.CancelReason != "user stop" || stored.CancelBatchID != "batch-1" || stored.CancelNodeID != "first" || stored.Status != RunStatusSuspended {
		t.Fatalf("stored=%+v err=%v", stored, err)
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

func TestPostgresSchedulerFailsClosedOnDurableCrossInstanceSuspensions(t *testing.T) {
	store, db := openPostgresRunStore(t)
	id := postgresRunID(t)
	t.Cleanup(func() {
		if err := db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}).Error; err != nil {
			t.Errorf("clean up plan run %q: %v", id, err)
		}
	})
	runCrossInstanceSuspensionConflict(t, store, id)
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

func TestPostgresRunStoreRejectsSubmitFingerprintPayloadDrift(t *testing.T) {
	store, db := openPostgresRunStore(t)
	ctx := context.Background()
	id := postgresRunID(t)
	t.Cleanup(func() { db.WithContext(context.Background()).Where("id = ?", id).Delete(&planRunRecord{}) })
	created, err := store.CreateRun(ctx, postgresStoreRun(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec("UPDATE aigc_plan_runs SET payload = payload - 'submit_request_fingerprint' WHERE id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRun(ctx, id); !errors.Is(err, ErrRunRecordCorrupt) {
		t.Fatalf("fingerprint drift error = %v", err)
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

func TestSchedulerWrappedPostgresRunStoreKeepsAtomicLeaseClock(t *testing.T) {
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

	wrapped := struct{ RunStore }{RunStore: store}
	cfg := schedulerConfigForTest(wrapped, schedulerRegistry(t, schedulerTool{key: "persist-test"}), func() string { return "unused" })
	cfg.Now = func() time.Time { return time.Now().Add(-24 * time.Hour) }
	cfg.LeaseTTL = time.Second
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claimed, claims, err := scheduler.claimReady(ctx, created)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claimReady() run=%+v claims=%+v err=%v", claimed, claims, err)
	}
	databaseNow, err := store.AuthoritativeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lease := claimed.Nodes["first"].LeaseUntil
	if lease == nil || !lease.After(databaseNow.Add(700*time.Millisecond)) {
		t.Fatalf("wrapped store used non-atomic fallback clock: lease=%v database_now=%v", lease, databaseNow)
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
