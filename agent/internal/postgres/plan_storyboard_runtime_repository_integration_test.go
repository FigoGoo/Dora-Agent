package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const planStoryboardRuntimeIntegrationDSNEnv = "DORA_AGENT_TEST_POSTGRES_DSN"

// TestPlanStoryboardRuntimePostgreSQLLifecycle 在真实 PostgreSQL 上验证并发幂等、全 Source HOL、
// Lease/Fence takeover、分层 Receipt、prepared Unknown Recovery、投影恢复与事务回滚。
func TestPlanStoryboardRuntimePostgreSQLLifecycle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(planStoryboardRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("DORA_AGENT_TEST_POSTGRES_DSN 未设置，跳过真实 PostgreSQL 生命周期测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 24, MaxIdleConns: 8,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Plan Storyboard Runtime PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 Plan Storyboard Runtime PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	ensurePlanStoryboardRuntimeMigration(t, client.db)
	if err := client.VerifyPlanStoryboardRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Plan Storyboard Runtime Schema 未就绪: %v", err)
	}
	quiescePlanStoryboardIntegrationRows(t, client.db)
	assertPlanStoryboardSchemaIsolation(t, client.db)

	ids := idgen.UUIDv7{}
	keyVersion := "plan-storyboard-integration-v1"
	protector, err := contentcrypto.NewAES256GCMProtector(bytes.Repeat([]byte{0x73}, 32), keyVersion)
	if err != nil {
		t.Fatalf("创建 Plan Storyboard 内容保护器失败: %v", err)
	}
	repository, err := NewPlanStoryboardRuntimeRepository(client, protector, ids)
	if err != nil {
		t.Fatalf("创建 Plan Storyboard Repository 失败: %v", err)
	}

	userID := mustPlanStoryboardIntegrationID(t, ids)
	projectID := mustPlanStoryboardIntegrationID(t, ids)
	sessionID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, sessionID, userID, projectID)
	duration := 30
	intent := planstoryboard.Intent{
		SchemaVersion:         planstoryboard.IntentSchemaVersion,
		PlanningInstruction:   "为夏日品牌短片规划可审核的单场景故事板",
		TargetDurationSeconds: &duration,
	}
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	creationRef := planstoryboard.CreationSpecRef{
		ID: mustPlanStoryboardIntegrationID(t, ids), Version: 1, ContentDigest: strings.Repeat("c", 64),
	}
	command := planstoryboardruntime.EnqueueCommand{
		RequestID: mustPlanStoryboardIntegrationID(t, ids), SessionID: sessionID,
		UserID: userID, ProjectID: projectID, IdempotencyKey: mustPlanStoryboardIntegrationID(t, ids),
		CreationSpecRef: creationRef, IntentJSON: intentJSON,
		AccessScopeRef: "plan-storyboard.preview.access-scope@v1", AccessScopeDigest: strings.Repeat("a", 64),
		IntentKeyVersion: keyVersion,
	}

	type enqueueOutcome struct {
		result planstoryboardruntime.EnqueueResult
		err    error
	}
	enqueueOutcomes := make([]enqueueOutcome, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		enqueueOutcomes[index].result, enqueueOutcomes[index].err = repository.Enqueue(ctx, command, time.Now())
	})
	var first planstoryboardruntime.EnqueueResult
	firstWrites := 0
	for index, outcome := range enqueueOutcomes {
		if outcome.err != nil {
			t.Fatalf("并发入队[%d]失败: %v", index, outcome.err)
		}
		if index == 0 {
			first = outcome.result
		}
		if outcome.result.InputID != first.InputID || outcome.result.TurnID != first.TurnID ||
			outcome.result.RunID != first.RunID || outcome.result.ToolCallID != first.ToolCallID ||
			outcome.result.BusinessCommandID != first.BusinessCommandID ||
			outcome.result.RouterModelCallID != first.RouterModelCallID ||
			outcome.result.GraphModelCallID != first.GraphModelCallID ||
			outcome.result.AcceptedEventID != first.AcceptedEventID || outcome.result.TerminalEventID != first.TerminalEventID {
			t.Fatalf("并发幂等未重放同一稳定身份: first=%+v outcome=%+v", first, outcome.result)
		}
		if !outcome.result.Replayed {
			firstWrites++
		}
	}
	if firstWrites != 1 {
		t.Fatalf("并发入队首写次数=%d want=1", firstWrites)
	}
	assertPlanStoryboardEnqueueFacts(t, client.db, command, first)
	assertPlanStoryboardGuardRejects(t, client.db, "plan_storyboard_context_guard_"+strings.ReplaceAll(first.InputID, "-", ""),
		"UPDATE agent.plan_storyboard_preview_turn_context SET profile = profile WHERE turn_id = ?", first.TurnID)

	replayCommand := command
	replayCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	failClosedRepository, err := NewPlanStoryboardRuntimeRepository(client, failingPlanStoryboardProtector{}, failingPlanStoryboardIDs{})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := failClosedRepository.Enqueue(ctx, replayCommand, time.Now())
	if err != nil || !replayed.Replayed || replayed.InputID != first.InputID {
		t.Fatalf("同义重放不应依赖 KMS 或随机源: result=%+v err=%v", replayed, err)
	}
	conflict := replayCommand
	conflictIntent := intent
	conflictIntent.PlanningInstruction = "不同的故事板语义"
	conflict.IntentJSON, _ = json.Marshal(conflictIntent)
	if _, err := repository.Enqueue(ctx, conflict, time.Now()); !errors.Is(err, planstoryboardruntime.ErrIdempotencyConflict) {
		t.Fatalf("异义 Intent 幂等重放未被拒绝: %v", err)
	}
	conflict = replayCommand
	conflict.CreationSpecRef.ContentDigest = strings.Repeat("d", 64)
	if _, err := repository.Enqueue(ctx, conflict, time.Now()); !errors.Is(err, planstoryboardruntime.ErrIdempotencyConflict) {
		t.Fatalf("异义 CreationSpecRef 幂等重放未被拒绝: %v", err)
	}

	type claimOutcome struct {
		claim *planstoryboardruntime.Claim
		err   error
	}
	claimOutcomes := make([]claimOutcome, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		claimOutcomes[index].claim, claimOutcomes[index].err = repository.ClaimNext(
			ctx, fmt.Sprintf("plan-storyboard-owner-%02d", index), time.Now(), 30*time.Second,
		)
	})
	var claimV1 *planstoryboardruntime.Claim
	for index, outcome := range claimOutcomes {
		if outcome.err != nil {
			t.Fatalf("并发 Claim[%d]失败: %v", index, outcome.err)
		}
		if outcome.claim != nil {
			if claimV1 != nil {
				t.Fatalf("并发 Claim 重复分派: first=%+v second=%+v", claimV1, outcome.claim)
			}
			claimV1 = outcome.claim
		}
	}
	if claimV1 == nil || planstoryboardruntime.ValidateClaim(*claimV1) != nil {
		t.Fatalf("并发 Claim 没有产生唯一合法结果: %+v", claimV1)
	}
	expirePlanStoryboardLease(t, client.db, *claimV1)
	claimV2, err := repository.ClaimNext(ctx, "plan-storyboard-owner-takeover", time.Now(), 30*time.Second)
	if err != nil || claimV2 == nil || claimV2.Context.RunID != claimV1.Context.RunID ||
		claimV2.FenceToken <= claimV1.FenceToken || claimV2.Attempts != claimV1.Attempts+1 {
		t.Fatalf("Lease takeover 未保持身份或推进 Fence/Attempts: v1=%+v v2=%+v err=%v", claimV1, claimV2, err)
	}
	if err := repository.MarkRunning(ctx, *claimV1, time.Now()); !errors.Is(err, planstoryboardruntime.ErrFenceLost) {
		t.Fatalf("旧 Fence 仍可写 Run: %v", err)
	}
	if err := repository.MarkRunning(ctx, *claimV2, time.Now()); err != nil {
		t.Fatalf("当前 Fence MarkRunning 失败: %v", err)
	}

	routerIdentity := planStoryboardModelIdentity(*claimV2, first.RouterModelCallID, planstoryboardruntime.ModelCallRouter)
	routerDigest := strings.Repeat("b", 64)
	type modelOutcome struct {
		snapshot planstoryboardruntime.ModelReceiptSnapshot
		execute  bool
		err      error
	}
	modelOutcomes := make([]modelOutcome, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		modelOutcomes[index].snapshot, modelOutcomes[index].execute, modelOutcomes[index].err =
			repository.ReplayOrReserveModel(ctx, routerIdentity, routerDigest)
	})
	executions := 0
	for index, outcome := range modelOutcomes {
		if outcome.err != nil || outcome.snapshot.Stage != planstoryboardruntime.ModelReceiptReserved {
			t.Fatalf("并发 Router Receipt[%d]异常: snapshot=%+v execute=%v err=%T/%v", index,
				outcome.snapshot, outcome.execute, outcome.err, outcome.err)
		}
		if outcome.execute {
			executions++
		}
	}
	if executions != 1 {
		t.Fatalf("Router Model 执行权次数=%d want=1", executions)
	}
	routerResponse := &schema.Message{Role: schema.Assistant, Content: "frozen router response"}
	if err := repository.FreezeModelCompleted(ctx, routerIdentity, routerDigest, routerResponse); err != nil {
		t.Fatalf("Router Receipt 冻结失败: %v", err)
	}
	replayedModel, execute, err := repository.ReplayOrReserveModel(ctx, routerIdentity, routerDigest)
	if err != nil || execute || replayedModel.Stage != planstoryboardruntime.ModelReceiptCompleted ||
		replayedModel.Response == nil || replayedModel.Response.Content != routerResponse.Content {
		t.Fatalf("Router Receipt 未原样重放: snapshot=%+v execute=%v err=%v", replayedModel, execute, err)
	}
	graphIdentity := planStoryboardModelIdentity(*claimV2, first.GraphModelCallID, planstoryboardruntime.ModelCallGraphPlanning)
	graphDigest := strings.Repeat("e", 64)
	graphSnapshot, graphExecute, err := repository.ReplayOrReserveModel(ctx, graphIdentity, graphDigest)
	if err != nil || !graphExecute || graphSnapshot.Stage != planstoryboardruntime.ModelReceiptReserved {
		t.Fatalf("Graph Model Receipt reserve 失败: snapshot=%+v execute=%v err=%v", graphSnapshot, graphExecute, err)
	}
	if err := repository.FreezeModelFailed(ctx, graphIdentity, graphDigest, "LOCAL_FAKE_MODEL_EXECUTION_FAILED"); err != nil {
		t.Fatalf("Graph Model Receipt failed freeze 失败: %v", err)
	}

	toolIdentity := planStoryboardToolIdentity(*claimV2)
	toolDigest := planStoryboardToolRequestDigest(claimV2.Context)
	type toolOutcome struct {
		snapshot planstoryboardruntime.ToolReceiptSnapshot
		execute  bool
		err      error
	}
	toolOutcomes := make([]toolOutcome, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		toolOutcomes[index].snapshot, toolOutcomes[index].execute, toolOutcomes[index].err =
			repository.ReplayOrOpenTool(ctx, toolIdentity, toolDigest)
	})
	executions = 0
	for index, outcome := range toolOutcomes {
		if outcome.err != nil || outcome.snapshot.Stage != planstoryboardruntime.ToolReceiptOpen {
			t.Fatalf("并发 Tool Receipt[%d]异常: %+v", index, outcome)
		}
		if outcome.execute {
			executions++
		}
	}
	if executions != 1 {
		t.Fatalf("Tool 执行权次数=%d want=1", executions)
	}

	preparedCommand := planStoryboardPreparedCommand(t, *claimV2)
	journal, err := planstoryboardruntime.NewCommandJournal(repository)
	if err != nil {
		t.Fatal(err)
	}
	runtimeCtx := turncontext.WithPlanStoryboardRuntime(ctx, planstoryboardruntime.RuntimeContextFromClaim(*claimV2))
	if err := journal.PrepareCommand(runtimeCtx, preparedCommand); err != nil {
		t.Fatalf("prepared Command 冻结失败: %v", err)
	}
	preparedSnapshot, err := repository.LoadToolReceipt(ctx, *claimV2)
	if err != nil || preparedSnapshot.Stage != planstoryboardruntime.ToolReceiptBusinessPrepared ||
		preparedSnapshot.PreparedCommand == nil || preparedSnapshot.PreparedCommand.RequestDigest != preparedCommand.RequestDigest ||
		preparedSnapshot.PreparedCommand.TrustedContext.BusinessCommandID != first.BusinessCommandID ||
		preparedSnapshot.PreparedCommandDigest == "" || preparedSnapshot.ContentDigest == "" {
		t.Fatalf("prepared Receipt 未完整重建: snapshot=%+v err=%v", preparedSnapshot, err)
	}
	if err := repository.MarkToolBusinessUnknown(ctx, toolIdentity, toolDigest); err != nil {
		t.Fatalf("prepared -> business_unknown 失败: %v", err)
	}
	if err := repository.DeferRecovery(ctx, *claimV2, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("Unknown Recovery defer 失败: %v", err)
	}
	claimV3, err := repository.ClaimNext(ctx, "plan-storyboard-owner-recovery", time.Now(), 30*time.Second)
	if err != nil || claimV3 == nil || claimV3.Attempts != claimV2.Attempts || claimV3.FenceToken <= claimV2.FenceToken {
		t.Fatalf("Recovery Claim 不应增加 Attempts: v2=%+v v3=%+v err=%v", claimV2, claimV3, err)
	}
	if err := repository.MarkRunning(ctx, *claimV3, time.Now()); err != nil {
		t.Fatalf("Recovery MarkRunning 失败: %v", err)
	}
	toolIdentity = planStoryboardToolIdentity(*claimV3)
	unknownSnapshot, execute, err := repository.ReplayOrOpenTool(ctx, toolIdentity, toolDigest)
	if err != nil || execute || unknownSnapshot.Stage != planstoryboardruntime.ToolReceiptBusinessUnknown ||
		unknownSnapshot.Recovery == nil || unknownSnapshot.PreparedCommand == nil {
		t.Fatalf("更高 Fence 未重建 Unknown Recovery: snapshot=%+v execute=%v err=%v", unknownSnapshot, execute, err)
	}
	recovery := *unknownSnapshot.Recovery
	type reserveOutcome struct {
		recovery planstoryboard.RecoveryDeferred
		execute  bool
		err      error
	}
	reserveOutcomes := make([]reserveOutcome, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		reserveOutcomes[index].recovery, reserveOutcomes[index].execute, reserveOutcomes[index].err =
			repository.ReserveToolCommandResend(ctx, toolIdentity, toolDigest, recovery)
	})
	reservations := 0
	for index, outcome := range reserveOutcomes {
		if outcome.err != nil || outcome.recovery.ResendAttempts != 1 || !outcome.recovery.ResendExhausted {
			t.Fatalf("并发重发预留[%d]异常: %+v", index, outcome)
		}
		if outcome.execute {
			reservations++
		}
	}
	if reservations != 1 {
		t.Fatalf("并发重发预留次数=%d want=1", reservations)
	}

	result := planStoryboardCompletedResult(t, ids, preparedCommand)
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultDigest := digestPlanStoryboardBytes(resultJSON)
	freezeErrors := make([]error, 16)
	runPlanStoryboardConcurrent(16, func(index int) {
		freezeErrors[index] = repository.FreezeToolResult(
			ctx, toolIdentity, toolDigest, planstoryboardruntime.ToolReceiptCompleted, resultJSON, resultDigest,
		)
	})
	for index, freezeErr := range freezeErrors {
		if freezeErr != nil {
			t.Fatalf("并发 Tool Result freeze[%d]失败: %v", index, freezeErr)
		}
	}
	if err := repository.DeferProjection(ctx, *claimV3, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("终态 Receipt 投影 defer 失败: %v", err)
	}
	claimV4, err := repository.ClaimNext(ctx, "plan-storyboard-owner-projection", time.Now(), 30*time.Second)
	if err != nil || claimV4 == nil || claimV4.Attempts != claimV3.Attempts || claimV4.FenceToken <= claimV3.FenceToken {
		t.Fatalf("投影恢复 Claim 不应增加 Attempts: v3=%+v v4=%+v err=%v", claimV3, claimV4, err)
	}
	if err := repository.MarkRunning(ctx, *claimV4, time.Now()); err != nil {
		t.Fatalf("投影恢复 MarkRunning 失败: %v", err)
	}
	terminalSnapshot, err := repository.LoadToolReceipt(ctx, *claimV4)
	if err != nil || terminalSnapshot.Stage != planstoryboardruntime.ToolReceiptCompleted ||
		terminalSnapshot.PreparedCommand == nil || !bytes.Equal(terminalSnapshot.ResultJSON, resultJSON) ||
		terminalSnapshot.PreparedCommand.RequestDigest != preparedCommand.RequestDigest || terminalSnapshot.ResultDigest != resultDigest {
		t.Fatalf("终态 Receipt 未连同 prepared Command 原样重放: snapshot=%+v err=%v", terminalSnapshot, err)
	}
	if err := repository.CompleteToolResult(ctx, *claimV4, result, time.Now()); err != nil {
		t.Fatalf("终态 Event/Input/Run 原子收尾失败: %v", err)
	}
	assertPlanStoryboardTerminalFacts(t, client.db, first, command.RequestID, resultDigest)
	workspaceRepository, err := NewWorkspaceRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	workspaceService, err := workspace.NewService(
		workspaceRepository, protector, workspace.SnapshotLimits{MaxMessages: 10, MaxInputs: 10}, 64*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	workspaceIdentity := workspace.Identity{
		UserID: userID, ProjectID: projectID, SessionID: sessionID,
	}
	hardRefresh, err := workspaceService.LoadSnapshot(ctx, workspaceIdentity, mustPlanStoryboardIntegrationID(t, ids))
	if err != nil || hardRefresh.SchemaVersion != workspace.SnapshotSchemaVersionV3 ||
		hardRefresh.PlanStoryboardPreview == nil || hardRefresh.PlanStoryboardPreview.Status != "completed" ||
		hardRefresh.PlanStoryboardPreview.InputID != first.InputID ||
		hardRefresh.PlanStoryboardPreview.TurnID != first.TurnID ||
		hardRefresh.PlanStoryboardPreview.RunID != first.RunID ||
		hardRefresh.PlanStoryboardPreview.ToolCallID != first.ToolCallID ||
		hardRefresh.PlanStoryboardPreview.StoryboardPreviewID != result.ResourceRef.StoryboardPreviewID {
		t.Fatalf("Workspace 硬刷新未恢复同一 Storyboard Card: snapshot=%+v err=%v", hardRefresh, err)
	}
	var terminalEvent sessionEventLogModel
	if err := client.db.Where("event_id = ?", first.TerminalEventID).Take(&terminalEvent).Error; err != nil {
		t.Fatalf("读取 Storyboard terminal Event seq 失败: %v", err)
	}
	replay, err := workspaceService.LoadEventBatch(ctx, workspaceIdentity, terminalEvent.Seq-1, 10)
	if err != nil || len(replay.Events) != 1 ||
		replay.Events[0].Seq != terminalEvent.Seq ||
		replay.Events[0].Event != string(event.TypePlanStoryboardPreviewCompleted) {
		t.Fatalf("Workspace SSE 未按冻结 Context 补读 Storyboard terminal Card: batch=%+v err=%v", replay, err)
	}

	rollbackSessionID := mustPlanStoryboardIntegrationID(t, ids)
	rollbackProjectID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, rollbackSessionID, userID, rollbackProjectID)
	rollbackIDs := make([]string, 9)
	for index := range rollbackIDs {
		rollbackIDs[index] = mustPlanStoryboardIntegrationID(t, ids)
	}
	rollbackIDs[7] = first.AcceptedEventID // 事务最后一个写入制造全局 Event PK 冲突。
	rollbackRepository, err := NewPlanStoryboardRuntimeRepository(client, protector, &slicePlanStoryboardIDs{values: rollbackIDs})
	if err != nil {
		t.Fatal(err)
	}
	rollbackCommand := command
	rollbackCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	rollbackCommand.SessionID = rollbackSessionID
	rollbackCommand.ProjectID = rollbackProjectID
	rollbackCommand.IdempotencyKey = mustPlanStoryboardIntegrationID(t, ids)
	if _, err := rollbackRepository.Enqueue(ctx, rollbackCommand, time.Now()); !errors.Is(err, planstoryboardruntime.ErrPersistence) {
		t.Fatalf("末端 Event 冲突未折叠为 Persistence: %v", err)
	}
	assertPlanStoryboardEnqueueRolledBack(t, client.db, rollbackSessionID, rollbackIDs)

	blockedSessionID := mustPlanStoryboardIntegrationID(t, ids)
	blockedProjectID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, blockedSessionID, userID, blockedProjectID)
	seedPlanStoryboardBlockedHead(t, client.db, blockedSessionID, ids)
	blockedCommand := command
	blockedCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	blockedCommand.SessionID = blockedSessionID
	blockedCommand.ProjectID = blockedProjectID
	blockedCommand.IdempotencyKey = mustPlanStoryboardIntegrationID(t, ids)
	if _, err := repository.Enqueue(ctx, blockedCommand, time.Now()); !errors.Is(err, planstoryboardruntime.ErrSessionLaneBlocked) {
		t.Fatalf("非本 Profile 未终态 HOL 未阻断入队: %v", err)
	}

	queuedSessionID := mustPlanStoryboardIntegrationID(t, ids)
	queuedProjectID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, queuedSessionID, userID, queuedProjectID)
	queuedCommand := command
	queuedCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	queuedCommand.SessionID = queuedSessionID
	queuedCommand.ProjectID = queuedProjectID
	queuedCommand.IdempotencyKey = mustPlanStoryboardIntegrationID(t, ids)
	queuedFirst, err := repository.Enqueue(ctx, queuedCommand, time.Now())
	if err != nil {
		t.Fatalf("同 Profile 队列首写失败: %v", err)
	}
	queuedCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	queuedCommand.IdempotencyKey = mustPlanStoryboardIntegrationID(t, ids)
	queuedSecond, err := repository.Enqueue(ctx, queuedCommand, time.Now())
	if err != nil || queuedSecond.InputID == queuedFirst.InputID {
		t.Fatalf("同 Profile 非终态后继未入队: first=%+v second=%+v err=%v", queuedFirst, queuedSecond, err)
	}
}

func ensurePlanStoryboardRuntimeMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	encoded, err := os.ReadFile("../../migrations/20260717001100_add_plan_storyboard_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 Plan Storyboard Runtime Migration 失败: %v", err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(0x5053424d32)).Error; err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw("SELECT to_regclass('agent.plan_storyboard_preview_run') IS NOT NULL").Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			// 本地测试库可能已应用过早期 011 草案；对齐最终 Migration 拥有的
			// recovery_pending exact-set，不改动任何业务行。
			var supportsRecovery bool
			if err := tx.Raw(`SELECT position('recovery_pending' in pg_get_constraintdef(oid)) > 0
				FROM pg_constraint
				WHERE conrelid = 'agent.session_input'::regclass AND conname = 'ck_session_input__status'`).
				Scan(&supportsRecovery).Error; err != nil {
				return err
			}
			if supportsRecovery {
				return nil
			}
			if err := tx.Exec("ALTER TABLE agent.session_input DROP CONSTRAINT ck_session_input__status").Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE agent.session_input ADD CONSTRAINT ck_session_input__status CHECK (
				status IN ('pending', 'claimed', 'running', 'retry_wait', 'recovery_pending', 'resolved', 'dead'))`).Error
		}
		return tx.Exec(string(encoded)).Error
	})
	if err != nil {
		t.Fatalf("应用 Plan Storyboard Runtime Migration 失败: %v", err)
	}
}

// quiescePlanStoryboardIntegrationRows 只处理专用 dora_agent_test 中上次中断测试留下的非终态行，
// 避免全 Source Scanner 在新用例开始时合法领取旧 Session。不删除 append-only Context/Receipt/Event。
func quiescePlanStoryboardIntegrationRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`UPDATE agent.session_runtime_lease AS lease
		 SET lease_owner = NULL, lease_until = NULL, version = version + 1, updated_at = clock_timestamp()
		 WHERE EXISTS (
		   SELECT 1 FROM agent.session_input AS input_record
		   WHERE input_record.session_id = lease.session_id
		     AND input_record.source_type = 'plan_storyboard_preview'
		     AND input_record.status IN ('pending','claimed','running','retry_wait','recovery_pending')
		 )`,
		`UPDATE agent.session_input
		 SET status = 'resolved', lease_owner = NULL, lease_until = NULL, updated_at = clock_timestamp()
		 WHERE source_type = 'plan_storyboard_preview'
		   AND status IN ('pending','claimed','running','retry_wait','recovery_pending')`,
		`UPDATE agent.plan_storyboard_preview_run
		 SET status = 'completed', started_at = COALESCE(started_at, clock_timestamp()),
		     completed_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
		 WHERE status IN ('created','running','recovery_pending')`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("收敛上次 Plan Storyboard 集成测试残留行失败: %v", err)
		}
	}
}

func assertPlanStoryboardSchemaIsolation(t *testing.T, db *gorm.DB) {
	t.Helper()
	var foreignKeys int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_constraint AS constraint_record
		JOIN pg_class AS relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'agent' AND constraint_record.contype = 'f'
		  AND relation.relname IN (
		    'plan_storyboard_preview_run', 'plan_storyboard_preview_turn_context',
		    'plan_storyboard_preview_model_receipt', 'plan_storyboard_preview_tool_receipt'
		  )`).Scan(&foreignKeys).Error; err != nil || foreignKeys != 0 {
		t.Fatalf("Plan Storyboard Runtime 隔离表禁止物理外键: count=%d err=%v", foreignKeys, err)
	}
}

func seedPlanStoryboardSession(t *testing.T, db *gorm.DB, sessionID, userID, projectID string) {
	t.Helper()
	now := time.Now().UTC()
	for _, record := range []any{
		&sessionModel{ID: sessionID, ProjectID: projectID, UserID: userID, Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now},
		&sessionSkillSnapshotModel{
			SessionID: sessionID, SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			SnapshotKind: string(session.SkillSnapshotKindEmpty), SkillCount: 0,
			SnapshotDigest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefs: "[]", CreatedAt: now,
		},
		&sessionSequenceCounterModel{SessionID: sessionID, LastMessageSeq: 0, LastInputEnqueueSeq: 0, UpdatedAt: now},
		&sessionEventCounterModel{SessionID: sessionID, LastSeq: 0, MinAvailableSeq: 1, UpdatedAt: now},
		&sessionRuntimeLeaseModel{SessionID: sessionID, FenceToken: 0, Version: 1, UpdatedAt: now},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed Plan Storyboard Session 失败: %v", err)
		}
	}
}

func seedPlanStoryboardBlockedHead(t *testing.T, db *gorm.DB, sessionID string, ids idgen.UUIDv7) {
	t.Helper()
	now := time.Now().UTC()
	input := sessionInputModel{
		ID: mustPlanStoryboardIntegrationID(t, ids), SessionID: sessionID,
		SourceType: string(session.InputSourceTypeUserMessage), SourceID: mustPlanStoryboardIntegrationID(t, ids),
		Status: string(session.InputStatusPending), EnqueueSeq: 1, Attempts: 0,
		AvailableAt: now, FenceToken: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&input).Error; err != nil {
		t.Fatalf("seed 非本 Profile HOL 失败: %v", err)
	}
	update := db.Model(&sessionSequenceCounterModel{}).Where("session_id = ? AND last_input_enqueue_seq = 0", sessionID).
		Updates(map[string]any{"last_input_enqueue_seq": 1, "updated_at": now})
	if update.Error != nil || update.RowsAffected != 1 {
		t.Fatalf("seed 非本 Profile HOL counter 失败: rows=%d err=%v", update.RowsAffected, update.Error)
	}
}

func assertPlanStoryboardEnqueueFacts(
	t *testing.T,
	db *gorm.DB,
	command planstoryboardruntime.EnqueueCommand,
	result planstoryboardruntime.EnqueueResult,
) {
	t.Helper()
	var input sessionInputModel
	if err := db.Where("id = ?", result.InputID).Take(&input).Error; err != nil || input.MessageID != nil ||
		input.SourceType != planstoryboardruntime.SourceType || input.SourceID != command.RequestID {
		t.Fatalf("typed Intent 被伪装为 Message 或 source 漂移: input=%+v err=%v", input, err)
	}
	var messageCount int64
	if err := db.Model(&sessionMessageModel{}).Where("session_id = ?", command.SessionID).Count(&messageCount).Error; err != nil || messageCount != 0 {
		t.Fatalf("Plan Storyboard 入队不应创建 session_message: count=%d err=%v", messageCount, err)
	}
	for table, predicate := range map[string]string{
		"agent.plan_storyboard_preview_run":          "input_id = ?",
		"agent.plan_storyboard_preview_turn_context": "input_id = ?",
		"agent.plan_storyboard_preview_tool_receipt": "input_id = ?",
	} {
		var count int64
		if err := db.Table(table).Where(predicate, result.InputID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("并发入队表 %s 行数=%d err=%v", table, count, err)
		}
	}
	var acceptedCount int64
	if err := db.Model(&sessionEventLogModel{}).
		Where("session_id = ? AND event_type = 'plan_storyboard.preview.accepted'", command.SessionID).
		Count(&acceptedCount).Error; err != nil || acceptedCount != 1 {
		t.Fatalf("并发入队 accepted Event 数量=%d err=%v", acceptedCount, err)
	}
	var stored turncontext.PlanStoryboardTurnContext
	if err := db.Raw(`SELECT request_id, creation_spec_id, creation_spec_version,
		creation_spec_content_digest, business_command_id, router_model_call_id,
		graph_model_call_id, context_digest
		FROM agent.plan_storyboard_preview_turn_context WHERE turn_id = ?`, result.TurnID).Scan(&stored).Error; err != nil ||
		stored.RequestID != command.RequestID || stored.CreationSpecID != command.CreationSpecRef.ID ||
		stored.CreationSpecVersion != command.CreationSpecRef.Version ||
		stored.CreationSpecContentDigest != command.CreationSpecRef.ContentDigest ||
		stored.BusinessCommandID != result.BusinessCommandID || stored.RouterModelCallID != result.RouterModelCallID ||
		stored.GraphModelCallID != result.GraphModelCallID || stored.ContextDigest == "" {
		t.Fatalf("Turn Context 未冻结 exact-set: context=%+v err=%v", stored, err)
	}
}

func expirePlanStoryboardLease(t *testing.T, db *gorm.DB, claim planstoryboardruntime.Claim) {
	t.Helper()
	for _, item := range []struct {
		statement string
		id        string
	}{
		{"UPDATE agent.session_runtime_lease SET lease_until = clock_timestamp() - INTERVAL '1 second' WHERE session_id = ? AND fence_token = ?", claim.Context.SessionID},
		{"UPDATE agent.session_input SET lease_until = clock_timestamp() - INTERVAL '1 second' WHERE id = ? AND fence_token = ?", claim.Context.InputID},
	} {
		result := db.Exec(item.statement, item.id, claim.FenceToken)
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatalf("过期 Plan Storyboard Lease 失败: rows=%d err=%v", result.RowsAffected, result.Error)
		}
	}
}

func planStoryboardModelIdentity(
	claim planstoryboardruntime.Claim,
	modelCallID string,
	kind planstoryboardruntime.ModelCallKind,
) planstoryboardruntime.ModelReceiptIdentity {
	return planstoryboardruntime.ModelReceiptIdentity{
		Owner: claim.Owner, FenceToken: claim.FenceToken, SessionID: claim.Context.SessionID,
		InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ModelCallID: modelCallID, CallKind: kind,
	}
}

func planStoryboardToolIdentity(claim planstoryboardruntime.Claim) planstoryboardruntime.ToolReceiptIdentity {
	return planstoryboardruntime.ToolReceiptIdentity{
		Owner: claim.Owner, FenceToken: claim.FenceToken, SessionID: claim.Context.SessionID,
		InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ToolCallID: claim.Context.ToolCallID, BusinessCommandID: claim.Context.BusinessCommandID,
	}
}

func planStoryboardPreparedCommand(t *testing.T, claim planstoryboardruntime.Claim) planstoryboard.DraftCommand {
	t.Helper()
	content := planstoryboard.Content{
		Title: "夏日品牌短片", Summary: "单场景开发预览故事板。",
		Sections: []planstoryboard.Section{{Key: "section_1", Title: "主体", Objective: "建立夏日氛围"}},
		Elements: []planstoryboard.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene", Title: "开场",
			NarrativePurpose: "建立氛围", DurationSeconds: 30, SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}},
		Slots: []planstoryboard.Slot{},
	}
	trusted := planstoryboardruntime.CoreContextFromRuntime(planstoryboardruntime.RuntimeContextFromClaim(claim))
	command := planstoryboard.DraftCommand{
		TrustedContext: trusted,
		DomainContext: planstoryboard.PlanningContext{
			ProjectID: trusted.ProjectID, ProjectVersion: 3,
			CreationSpec: planstoryboard.CreationSpecResource{
				ID: trusted.CreationSpecRef.ID, ProjectID: trusted.ProjectID, Version: trusted.CreationSpecRef.Version,
				Status: "draft", ContentDigest: trusted.CreationSpecRef.ContentDigest,
			},
		},
		Content: content,
	}
	var err error
	command.RequestDigest, err = planstoryboard.SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算 Business Save 摘要失败: %v", err)
	}
	return command
}

func planStoryboardCompletedResult(
	t *testing.T,
	ids idgen.UUIDv7,
	command planstoryboard.DraftCommand,
) planstoryboard.Result {
	t.Helper()
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil {
		t.Fatal(err)
	}
	previewID := mustPlanStoryboardIntegrationID(t, ids)
	ref := &planstoryboard.ResourceRef{
		StoryboardPreviewID: previewID, Version: 1, Digest: contentDigest, Status: "draft",
		CreationSpecRef: command.TrustedContext.CreationSpecRef,
	}
	return planstoryboard.Result{
		SchemaVersion: planstoryboard.ResultSchemaVersion, Status: "completed",
		ResultCode: planstoryboard.ResultCodeCompleted, ResourceRef: ref,
		InvocationRef: planstoryboard.InvocationRef{
			ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID,
		},
		Card: &planstoryboard.Card{
			SchemaVersion: planstoryboard.CardSchemaVersion, StoryboardPreviewID: previewID,
			ProjectID: command.TrustedContext.ProjectID, CreationSpecRef: command.TrustedContext.CreationSpecRef,
			Version: 1, Status: "draft", ContentDigest: contentDigest,
			Title: command.Content.Title, Summary: command.Content.Summary,
			Sections: command.Content.Sections, Elements: command.Content.Elements, Slots: command.Content.Slots,
			UpdatedAt: time.Now().UTC(),
		},
	}
}

func assertPlanStoryboardTerminalFacts(
	t *testing.T,
	db *gorm.DB,
	result planstoryboardruntime.EnqueueResult,
	requestID string,
	resultDigest string,
) {
	t.Helper()
	var input sessionInputModel
	if err := db.Where("id = ?", result.InputID).Take(&input).Error; err != nil || input.Status != "resolved" ||
		input.LeaseOwner != nil || input.LeaseUntil != nil {
		t.Fatalf("Input 终态或 Lease release 错误: input=%+v err=%v", input, err)
	}
	var run planStoryboardPreviewRunModel
	if err := db.Where("input_id = ?", result.InputID).Take(&run).Error; err != nil || run.Status != "completed" || run.CompletedAt == nil {
		t.Fatalf("Run 终态错误: run=%+v err=%v", run, err)
	}
	var terminal sessionEventLogModel
	if err := db.Where("event_id = ?", result.TerminalEventID).Take(&terminal).Error; err != nil ||
		terminal.EventType != "plan_storyboard.preview.completed" || terminal.SourceID != requestID {
		t.Fatalf("terminal Event 未冻结首次 RequestID/结果: event=%+v err=%v", terminal, err)
	}
	var eventCard event.PlanStoryboardPreviewCardPayload
	if err := json.Unmarshal([]byte(terminal.Payload), &eventCard); err != nil ||
		eventCard.Status != "completed" || eventCard.ResultCode != planstoryboard.ResultCodeCompleted ||
		eventCard.InputID != result.InputID || eventCard.TurnID != result.TurnID ||
		eventCard.RunID != result.RunID || eventCard.ToolCallID != result.ToolCallID ||
		eventCard.FailureKind != "" || eventCard.Retryable != nil {
		t.Fatalf("terminal Event Card 语义漂移: card=%+v err=%v", eventCard, err)
	}
	var eventFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(terminal.Payload), &eventFields); err != nil || len(eventFields) != 18 {
		t.Fatalf("terminal completed Card 非 exact-set: fields=%v err=%v payload=%s", eventFields, err, terminal.Payload)
	}
	for _, forbidden := range []string{"invocation_ref", "business_command_id", "prompt", "intent", "provider", "access_scope"} {
		if _, exists := eventFields[forbidden]; exists || strings.Contains(terminal.Payload, forbidden) {
			t.Fatalf("terminal Event Card 泄漏 %s: %s", forbidden, terminal.Payload)
		}
	}
	var receipt planStoryboardPreviewToolReceiptModel
	if err := db.Where("tool_call_id = ?", result.ToolCallID).Take(&receipt).Error; err != nil ||
		receipt.ResultDigest == nil || *receipt.ResultDigest != resultDigest {
		t.Fatalf("terminal Receipt 未冻结 canonical 结果摘要: receipt=%+v err=%v", receipt, err)
	}
	var terminalCount int64
	if err := db.Model(&sessionEventLogModel{}).
		Where("aggregate_id = ? AND event_type IN ?", result.InputID, []string{
			"plan_storyboard.preview.completed", "plan_storyboard.preview.failed", "plan_storyboard.preview.runtime_failed",
		}).Count(&terminalCount).Error; err != nil || terminalCount != 1 {
		t.Fatalf("互斥终态 Event 数量=%d err=%v", terminalCount, err)
	}
}

func assertPlanStoryboardEnqueueRolledBack(t *testing.T, db *gorm.DB, sessionID string, ids []string) {
	t.Helper()
	for table, predicate := range map[string]string{
		"agent.session_input":                        "id = ?",
		"agent.plan_storyboard_preview_run":          "input_id = ?",
		"agent.plan_storyboard_preview_turn_context": "input_id = ?",
		"agent.plan_storyboard_preview_tool_receipt": "input_id = ?",
	} {
		var count int64
		if err := db.Table(table).Where(predicate, ids[0]).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("末端 Event 冲突未回滚 %s: count=%d err=%v", table, count, err)
		}
	}
	var sequence sessionSequenceCounterModel
	var eventCounter sessionEventCounterModel
	if err := db.Where("session_id = ?", sessionID).Take(&sequence).Error; err != nil || sequence.LastInputEnqueueSeq != 0 {
		t.Fatalf("入队回滚未恢复 Input Counter: counter=%+v err=%v", sequence, err)
	}
	if err := db.Where("session_id = ?", sessionID).Take(&eventCounter).Error; err != nil || eventCounter.LastSeq != 0 {
		t.Fatalf("入队回滚未恢复 Event Counter: counter=%+v err=%v", eventCounter, err)
	}
}

func assertPlanStoryboardGuardRejects(t *testing.T, db *gorm.DB, savepoint, statement string, arguments ...any) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启 Guard 探针事务失败: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SAVEPOINT " + savepoint).Error; err != nil {
		t.Fatalf("创建 %s savepoint 失败: %v", savepoint, err)
	}
	if err := tx.Exec(statement, arguments...).Error; err == nil {
		t.Fatalf("数据库 Guard 未拒绝变更: %s", statement)
	}
	if err := tx.Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error; err != nil {
		t.Fatalf("恢复 %s savepoint 失败: %v", savepoint, err)
	}
}

func runPlanStoryboardConcurrent(count int, task func(int)) {
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer group.Done()
			<-start
			task(index)
		}(index)
	}
	close(start)
	group.Wait()
}

func mustPlanStoryboardIntegrationID(t *testing.T, ids idgen.UUIDv7) string {
	t.Helper()
	value, err := ids.New()
	if err != nil {
		t.Fatalf("生成 Plan Storyboard UUIDv7 失败: %v", err)
	}
	return value
}

type failingPlanStoryboardProtector struct{}

func (failingPlanStoryboardProtector) Protect(context.Context, []byte) (session.ProtectedContent, error) {
	return session.ProtectedContent{}, fmt.Errorf("protector must not be called")
}

func (failingPlanStoryboardProtector) Open(context.Context, session.ProtectedContent, string) ([]byte, error) {
	return nil, fmt.Errorf("protector must not be called")
}

type failingPlanStoryboardIDs struct{}

func (failingPlanStoryboardIDs) New() (string, error) {
	return "", fmt.Errorf("id generator must not be called")
}

type slicePlanStoryboardIDs struct {
	mu     sync.Mutex
	values []string
	index  int
}

func (generator *slicePlanStoryboardIDs) New() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	if generator.index >= len(generator.values) {
		return "", fmt.Errorf("test id generator exhausted")
	}
	value := generator.values[generator.index]
	generator.index++
	return value, nil
}
