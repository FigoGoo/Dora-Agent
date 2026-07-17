package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const userMessageRuntimeIntegrationDSNEnv = "DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN"

// TestUserMessageRuntimePostgreSQLLifecycle 在显式专用 DSN 上验证 Session 入队、takeover、回执重放、终态事务与 Workspace。
// 所有调用都运行在最外层测试事务中并最终回滚；普通单测没有 DSN 时不会接触任何数据库。
func TestUserMessageRuntimePostgreSQLLifecycle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(userMessageRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL 生命周期探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 User Message Runtime PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 User Message Runtime PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	if err := client.VerifyUserMessageRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("User Message Runtime Schema 未就绪: %v", err)
	}

	outer := client.db.WithContext(ctx).Begin()
	if outer.Error != nil {
		t.Fatalf("开启 User Message Runtime 外层事务失败: %v", outer.Error)
	}
	defer func() {
		if rollbackErr := outer.Rollback().Error; rollbackErr != nil {
			t.Errorf("回滚 User Message Runtime 外层事务失败: %v", rollbackErr)
		}
	}()
	testClient := &Client{db: outer}

	key := bytes.Repeat([]byte{0x42}, 32)
	contentProtector, err := contentcrypto.NewAES256GCMProtector(key, "user-message-integration-v1")
	if err != nil {
		t.Fatalf("创建 User Message Runtime 内容保护器失败: %v", err)
	}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtector(key, "user-message-integration-v1")
	if err != nil {
		t.Fatalf("创建 User Message Runtime Skill Snapshot 保护器失败: %v", err)
	}
	sessionRepository, err := NewSessionRepository(testClient)
	if err != nil {
		t.Fatalf("创建 Session Repository 失败: %v", err)
	}
	sessionService, err := session.NewServiceWithSkillSnapshotAndUserMessageRuntime(
		sessionRepository, idgen.UUIDv7{}, clock.System{}, contentProtector, snapshotProtector,
		skill.DefaultLimitsProfileV1(), usermessageruntime.ApprovedSessionProfile(),
	)
	if err != nil {
		t.Fatalf("创建 User Message Runtime Session Service 失败: %v", err)
	}

	ids := idgen.UUIDv7{}
	requestID := mustUserMessageRuntimeIntegrationID(t, ids)
	commandID := mustUserMessageRuntimeIntegrationID(t, ids)
	projectID := mustUserMessageRuntimeIntegrationID(t, ids)
	userID := mustUserMessageRuntimeIntegrationID(t, ids)
	prompt := "请帮我制作一张夏日新品发布海报"
	requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
		projectID, userID, prompt, session.SkillSnapshotKindEmpty,
	)
	if err != nil || !promptPresent {
		t.Fatalf("计算 User Message Runtime 测试摘要失败: present=%v err=%v", promptPresent, err)
	}
	ensureResult, err := sessionService.EnsureProjectSession(ctx, session.EnsureCommand{
		SchemaVersion: session.EnsureCommandSchemaVersionV1,
		RequestID:     requestID, CommandID: commandID, RequestDigest: requestDigest,
		ProjectID: projectID, OwnerUserID: userID, CreationSource: session.CreationSourceQuickCreate,
		InitialPrompt: prompt, PromptDigest: promptDigest, SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt: time.Now().UTC(),
	})
	if err != nil || ensureResult.InputID == nil || ensureResult.MessageID == nil {
		t.Fatalf("User Message Runtime Session 入队失败: result=%+v err=%v", ensureResult, err)
	}

	runtimeRepository, err := NewUserMessageRuntimeRepository(testClient, contentProtector, ids)
	if err != nil {
		t.Fatalf("创建 User Message Runtime Repository 失败: %v", err)
	}
	claimV1, err := runtimeRepository.ClaimNext(ctx, "user-message-integration-owner-v1", time.Now(), 30*time.Second)
	if err != nil || claimV1 == nil || claimV1.Context.InputID != *ensureResult.InputID {
		t.Fatalf("首次 Claim 失败: claim=%+v err=%v", claimV1, err)
	}
	expireUserMessageRuntimeIntegrationLease(t, outer, *claimV1)
	claimV2, err := runtimeRepository.ClaimNext(ctx, "user-message-integration-owner-v2", time.Now(), 30*time.Second)
	if err != nil || claimV2 == nil {
		t.Fatalf("created Run 过期 Lease takeover Claim 失败: claim=%+v err=%v", claimV2, err)
	}
	if claimV2.RunID != claimV1.RunID || claimV2.FenceToken <= claimV1.FenceToken || claimV2.Attempts != 2 {
		t.Fatalf("created Run takeover 未复用身份或推进 Fence/Attempts: first=%+v second=%+v", claimV1, claimV2)
	}
	assertUserMessageRuntimeIntegrationRunState(t, outer, claimV2.RunID, "created", false)
	if err := runtimeRepository.MarkRunning(ctx, *claimV1, time.Now()); !errors.Is(err, usermessageruntime.ErrFenceLost) {
		t.Fatalf("created Run takeover 后旧 Fence 仍可 MarkRunning: %v", err)
	}
	if err := runtimeRepository.MarkRunning(ctx, *claimV2, time.Now()); err != nil {
		t.Fatalf("created Run takeover 后 MarkRunning 失败: %v", err)
	}
	assertUserMessageRuntimeIntegrationRunState(t, outer, claimV2.RunID, "running", true)

	expireUserMessageRuntimeIntegrationLease(t, outer, *claimV2)
	claimV3, err := runtimeRepository.ClaimNext(ctx, "user-message-integration-owner-v3", time.Now(), 30*time.Second)
	if err != nil || claimV3 == nil {
		t.Fatalf("running Run 过期 Lease takeover Claim 失败: claim=%+v err=%v", claimV3, err)
	}
	if claimV3.RunID != claimV2.RunID || claimV3.FenceToken <= claimV2.FenceToken || claimV3.Attempts != 3 {
		t.Fatalf("running Run takeover 未复用身份或推进 Fence/Attempts: second=%+v third=%+v", claimV2, claimV3)
	}
	assertUserMessageRuntimeIntegrationRunState(t, outer, claimV3.RunID, "recovery_pending", true)
	if err := runtimeRepository.MarkRunning(ctx, *claimV2, time.Now()); !errors.Is(err, usermessageruntime.ErrFenceLost) {
		t.Fatalf("running Run takeover 后旧 Fence 仍可 MarkRunning: %v", err)
	}
	if err := runtimeRepository.MarkRunning(ctx, *claimV3, time.Now()); err != nil {
		t.Fatalf("recovery_pending Run takeover 后 MarkRunning 失败: %v", err)
	}
	assertUserMessageRuntimeIntegrationRunState(t, outer, claimV3.RunID, "running", true)

	preModelCard := usermessageruntime.NewDirectResponse(*claimV3)
	preModelOutput := usermessageruntime.Output{DirectResponse: &preModelCard}
	if err := runtimeRepository.FreezeOutput(ctx, *claimV3, preModelOutput, time.Now()); err != nil {
		t.Fatalf("预冻结 Output Receipt 失败: %v", err)
	}
	if err := runtimeRepository.Complete(ctx, *claimV3, preModelOutput, time.Now()); !errors.Is(err, usermessageruntime.ErrOutputContract) {
		t.Fatalf("缺失 completed Model Receipt 仍提交成功终态: %v", err)
	}

	countingModel := &countingUserMessageRuntimeIntegrationModel{base: chatmodel.NewUserMessageFake()}
	receiptModel, err := usermessageruntime.NewReceiptModel(countingModel, runtimeRepository)
	if err != nil {
		t.Fatalf("创建 User Message Receipt Model 失败: %v", err)
	}
	directAgent, err := chatmodelagent.NewDirectResponse(ctx, receiptModel)
	if err != nil {
		t.Fatalf("创建 Direct Response Agent 失败: %v", err)
	}
	runner, err := usermessageruntime.NewEinoRunner(ctx, directAgent)
	if err != nil {
		t.Fatalf("创建 User Message Eino Runner 失败: %v", err)
	}
	output, err := runner.Run(ctx, *claimV3)
	if err != nil || output.DirectResponse == nil {
		t.Fatalf("首次 Direct Response 执行失败: output=%+v err=%v", output, err)
	}
	replayed, err := runner.Run(ctx, *claimV3)
	if err != nil || replayed.DirectResponse == nil || !reflect.DeepEqual(replayed.DirectResponse, output.DirectResponse) {
		t.Fatalf("Model Receipt 重放漂移: first=%+v replay=%+v err=%v", output, replayed, err)
	}
	if calls := countingModel.calls.Load(); calls != 1 {
		t.Fatalf("同 Fence Model Receipt 重放重复调用 Fake: calls=%d", calls)
	}
	if err := runtimeRepository.FreezeOutput(ctx, *claimV3, output, time.Now()); err != nil {
		t.Fatalf("冻结 Output Receipt 失败: %v", err)
	}
	setUserMessageRuntimeIntegrationLeaseRemaining(t, outer, *claimV3, 500*time.Millisecond)
	delayedRepository, err := NewUserMessageRuntimeRepository(testClient, delayedUserMessageRuntimeProtector{
		delegate: contentProtector,
		delay:    750 * time.Millisecond,
	}, ids)
	if err != nil {
		t.Fatalf("创建延迟 User Message Runtime Repository 失败: %v", err)
	}
	if err := delayedRepository.Complete(ctx, *claimV3, output, time.Now()); !errors.Is(err, usermessageruntime.ErrFenceLost) {
		t.Fatalf("Lease 在终态事务中到期后 Complete err=%v want fence lost", err)
	}
	assertUserMessageRuntimeIntegrationTerminalRollback(t, outer, *claimV3)

	claimV4, err := runtimeRepository.ClaimNext(ctx, "user-message-integration-owner-v4", time.Now(), 30*time.Second)
	if err != nil || claimV4 == nil {
		t.Fatalf("冻结 Output 后过期 Lease takeover Claim 失败: claim=%+v err=%v", claimV4, err)
	}
	if claimV4.RunID != claimV3.RunID || claimV4.FenceToken <= claimV3.FenceToken || claimV4.Attempts != 4 {
		t.Fatalf("冻结 Output takeover 身份或 Fence/Attempts 漂移: third=%+v fourth=%+v", claimV3, claimV4)
	}
	assertUserMessageRuntimeIntegrationRunState(t, outer, claimV4.RunID, "recovery_pending", true)
	if err := runtimeRepository.MarkRunning(ctx, *claimV4, time.Now()); err != nil {
		t.Fatalf("冻结 Output takeover 后 MarkRunning 失败: %v", err)
	}
	frozen, err := runtimeRepository.LoadFrozenOutput(ctx, *claimV4)
	if err != nil || frozen.Output == nil || !reflect.DeepEqual(frozen.Output.DirectResponse, output.DirectResponse) {
		t.Fatalf("takeover 未 receipt-first 重放原 Output: frozen=%+v err=%v", frozen, err)
	}
	if err := runtimeRepository.Complete(ctx, *claimV4, *frozen.Output, time.Now()); err != nil {
		t.Fatalf("提交 User Message 终态事务失败: %v", err)
	}

	workspaceRepository, err := NewWorkspaceRepository(testClient)
	if err != nil {
		t.Fatalf("创建 Workspace Repository 失败: %v", err)
	}
	workspaceService, err := workspace.NewService(
		workspaceRepository, contentProtector, workspace.SnapshotLimits{MaxMessages: 10, MaxInputs: 10}, 64*1024,
	)
	if err != nil {
		t.Fatalf("创建 Workspace Service 失败: %v", err)
	}
	snapshotRequestID := mustUserMessageRuntimeIntegrationID(t, ids)
	snapshot, err := workspaceService.LoadSnapshot(ctx, workspace.Identity{
		UserID: userID, ProjectID: projectID, SessionID: ensureResult.SessionID,
	}, snapshotRequestID)
	if err != nil {
		t.Fatalf("读取 User Message Runtime Workspace 失败: %v", err)
	}
	if snapshot.SchemaVersion != workspace.SnapshotSchemaVersionV3 || snapshot.LatestTurnOutput == nil ||
		snapshot.LatestTurnOutput.SchemaVersion != usermessageruntime.DirectResponseCardSchemaVersion ||
		snapshot.LatestTurnOutput.RunID != claimV4.RunID || snapshot.LatestTurnOutput.InputID != *ensureResult.InputID ||
		snapshot.LatestTurnOutput.Status != "completed" || snapshot.EventHighWatermark != 3 ||
		len(snapshot.Inputs) != 1 || snapshot.Inputs[0].Status != string(session.InputStatusResolved) {
		t.Fatalf("Workspace V3 投影不完整: %+v", snapshot)
	}

	var facts struct {
		Turns         int64 `gorm:"column:turns"`
		Runs          int64 `gorm:"column:runs"`
		ModelReceipts int64 `gorm:"column:model_receipts"`
		Outputs       int64 `gorm:"column:outputs"`
		Projections   int64 `gorm:"column:projections"`
		Events        int64 `gorm:"column:events"`
	}
	if err := outer.Raw(`
		SELECT COUNT(DISTINCT turn_record.turn_id) AS turns,
		       COUNT(DISTINCT run_record.run_id) AS runs,
		       COUNT(DISTINCT model_receipt.model_call_id) AS model_receipts,
		       COUNT(DISTINCT output_receipt.output_id) AS outputs,
		       COUNT(DISTINCT projection.session_id) AS projections,
		       COUNT(DISTINCT event_record.event_id) AS events
		FROM agent.session_user_message_turn AS turn_record
		JOIN agent.session_user_message_run AS run_record ON run_record.turn_id = turn_record.turn_id
		JOIN agent.session_user_message_model_receipt AS model_receipt ON model_receipt.turn_id = turn_record.turn_id
		JOIN agent.session_user_message_output_receipt AS output_receipt ON output_receipt.turn_id = turn_record.turn_id
		JOIN agent.session_user_message_output_projection AS projection ON projection.turn_id = turn_record.turn_id
		JOIN agent.session_event_log AS event_record ON event_record.aggregate_id = turn_record.turn_id
		WHERE turn_record.session_id = ?
		  AND turn_record.status = 'completed' AND run_record.status = 'completed'
		  AND model_receipt.status = 'completed' AND output_receipt.status = 'completed'
		  AND event_record.event_type = 'session.turn.completed'`, ensureResult.SessionID).Scan(&facts).Error; err != nil {
		t.Fatalf("读取 User Message Runtime 唯一性事实失败: %v", err)
	}
	if facts.Turns != 1 || facts.Runs != 1 || facts.ModelReceipts != 1 || facts.Outputs != 1 ||
		facts.Projections != 1 || facts.Events != 1 {
		t.Fatalf("User Message Runtime 终态事实不唯一: %+v", facts)
	}
}

// TestUserMessageRuntimeTakeoverVsCompletePostgreSQL 验证 Complete 持有规范锁序跨过 Lease 到期时，
// takeover 只会跳过被锁 Input；旧 Fence 的终态事务回滚后，新 Fence 可安全接管且不会出现 40P01。
func TestUserMessageRuntimeTakeoverVsCompletePostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(userMessageRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL takeover-vs-Complete 探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 6, MaxIdleConns: 3,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 takeover-vs-Complete PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 takeover-vs-Complete PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	if err := client.VerifyUserMessageRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("User Message Runtime Schema 未就绪: %v", err)
	}

	key := bytes.Repeat([]byte{0x43}, 32)
	contentProtector, err := contentcrypto.NewAES256GCMProtector(key, "user-message-lock-order-v1")
	if err != nil {
		t.Fatalf("创建 lock-order 内容保护器失败: %v", err)
	}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtector(key, "user-message-lock-order-v1")
	if err != nil {
		t.Fatalf("创建 lock-order Skill Snapshot 保护器失败: %v", err)
	}
	sessionRepository, err := NewSessionRepository(client)
	if err != nil {
		t.Fatalf("创建 lock-order Session Repository 失败: %v", err)
	}
	sessionService, err := session.NewServiceWithSkillSnapshotAndUserMessageRuntime(
		sessionRepository, idgen.UUIDv7{}, clock.System{}, contentProtector, snapshotProtector,
		skill.DefaultLimitsProfileV1(), usermessageruntime.ApprovedSessionProfile(),
	)
	if err != nil {
		t.Fatalf("创建 lock-order Session Service 失败: %v", err)
	}

	ids := idgen.UUIDv7{}
	requestID := mustUserMessageRuntimeIntegrationID(t, ids)
	commandID := mustUserMessageRuntimeIntegrationID(t, ids)
	projectID := mustUserMessageRuntimeIntegrationID(t, ids)
	userID := mustUserMessageRuntimeIntegrationID(t, ids)
	prompt := "验证 takeover 与 Complete 的全局锁序"
	requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
		projectID, userID, prompt, session.SkillSnapshotKindEmpty,
	)
	if err != nil || !promptPresent {
		t.Fatalf("计算 lock-order 请求摘要失败: present=%v err=%v", promptPresent, err)
	}
	ensureResult, err := sessionService.EnsureProjectSession(ctx, session.EnsureCommand{
		SchemaVersion: session.EnsureCommandSchemaVersionV1,
		RequestID:     requestID, CommandID: commandID, RequestDigest: requestDigest,
		ProjectID: projectID, OwnerUserID: userID, CreationSource: session.CreationSourceQuickCreate,
		InitialPrompt: prompt, PromptDigest: promptDigest, SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt: time.Now().UTC(),
	})
	if err != nil || ensureResult.InputID == nil {
		t.Fatalf("创建 lock-order Session 事实失败: result=%+v err=%v", ensureResult, err)
	}

	runtimeRepository, err := NewUserMessageRuntimeRepository(client, contentProtector, ids)
	if err != nil {
		t.Fatalf("创建 lock-order Runtime Repository 失败: %v", err)
	}
	claim, err := runtimeRepository.ClaimNext(ctx, "lock-order-complete-owner", time.Now(), 30*time.Second)
	if err != nil || claim == nil || claim.Context.InputID != *ensureResult.InputID {
		t.Fatalf("lock-order 首次 Claim 失败: claim=%+v err=%v", claim, err)
	}
	if err := runtimeRepository.MarkRunning(ctx, *claim, time.Now()); err != nil {
		t.Fatalf("lock-order MarkRunning 失败: %v", err)
	}
	receiptModel, err := usermessageruntime.NewReceiptModel(chatmodel.NewUserMessageFake(), runtimeRepository)
	if err != nil {
		t.Fatalf("创建 lock-order Receipt Model 失败: %v", err)
	}
	directAgent, err := chatmodelagent.NewDirectResponse(ctx, receiptModel)
	if err != nil {
		t.Fatalf("创建 lock-order Direct Response Agent 失败: %v", err)
	}
	runner, err := usermessageruntime.NewEinoRunner(ctx, directAgent)
	if err != nil {
		t.Fatalf("创建 lock-order Runner 失败: %v", err)
	}
	output, err := runner.Run(ctx, *claim)
	if err != nil || output.DirectResponse == nil {
		t.Fatalf("执行 lock-order 模型失败: output=%+v err=%v", output, err)
	}
	if err := runtimeRepository.FreezeOutput(ctx, *claim, output, time.Now()); err != nil {
		t.Fatalf("冻结 lock-order Output 失败: %v", err)
	}
	setUserMessageRuntimeIntegrationLeaseRemaining(t, client.db, *claim, 300*time.Millisecond)

	completeEntered := make(chan struct{}, 1)
	delayedRepository, err := NewUserMessageRuntimeRepository(client, delayedUserMessageRuntimeProtector{
		delegate: contentProtector,
		delay:    650 * time.Millisecond,
		entered:  completeEntered,
	}, ids)
	if err != nil {
		t.Fatalf("创建 lock-order 延迟 Repository 失败: %v", err)
	}
	completeResult := make(chan error, 1)
	go func() {
		completeResult <- delayedRepository.Complete(ctx, *claim, output, time.Now())
	}()
	select {
	case <-completeEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("Complete 未在超时前持有 Input/Lease/Turn/Run/Output 锁")
	}
	time.Sleep(350 * time.Millisecond)

	takeoverAttemptCtx, takeoverAttemptCancel := context.WithTimeout(ctx, time.Second)
	takeoverAttemptStarted := time.Now()
	blockedClaim, takeoverAttemptErr := runtimeRepository.ClaimNext(
		takeoverAttemptCtx, "lock-order-takeover-owner", time.Now(), 30*time.Second,
	)
	takeoverAttemptCancel()
	if takeoverAttemptErr != nil || blockedClaim != nil {
		t.Fatalf("Complete 持锁时 takeover 应跳过并重试: claim=%+v err=%v", blockedClaim, takeoverAttemptErr)
	}
	if elapsed := time.Since(takeoverAttemptStarted); elapsed >= time.Second {
		t.Fatalf("Complete 持锁时 takeover 未通过 SKIP LOCKED 及时返回: elapsed=%s", elapsed)
	}

	select {
	case completeErr := <-completeResult:
		if !errors.Is(completeErr, usermessageruntime.ErrFenceLost) {
			t.Fatalf("Lease 到期的 Complete err=%v want fence lost（不得为 40P01/persistence）", completeErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("takeover-vs-Complete 并发事务超时，疑似死锁")
	}

	takeover, err := runtimeRepository.ClaimNext(ctx, "lock-order-takeover-owner", time.Now(), 30*time.Second)
	if err != nil || takeover == nil {
		t.Fatalf("旧 Complete 回滚后 takeover 失败: claim=%+v err=%v", takeover, err)
	}
	if takeover.RunID != claim.RunID || takeover.FenceToken <= claim.FenceToken {
		t.Fatalf("takeover 未复用 Run 或推进 Fence: old=%+v new=%+v", claim, takeover)
	}
	if err := runtimeRepository.MarkRunning(ctx, *takeover, time.Now()); err != nil {
		t.Fatalf("takeover MarkRunning 失败: %v", err)
	}
	frozen, err := runtimeRepository.LoadFrozenOutput(ctx, *takeover)
	if err != nil || frozen.Output == nil {
		t.Fatalf("takeover receipt-first 读取冻结 Output 失败: frozen=%+v err=%v", frozen, err)
	}
	if err := runtimeRepository.Complete(ctx, *takeover, *frozen.Output, time.Now()); err != nil {
		t.Fatalf("takeover 合法 Fence 提交失败: %v", err)
	}
}

type delayedUserMessageRuntimeProtector struct {
	delegate userMessageContentProtector
	delay    time.Duration
	entered  chan<- struct{}
}

type countingUserMessageRuntimeIntegrationModel struct {
	base  model.BaseChatModel
	calls atomic.Int64
}

func (counting *countingUserMessageRuntimeIntegrationModel) Generate(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.Message, error) {
	counting.calls.Add(1)
	return counting.base.Generate(ctx, messages, options...)
}

func (counting *countingUserMessageRuntimeIntegrationModel) Stream(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	counting.calls.Add(1)
	return counting.base.Stream(ctx, messages, options...)
}

func (protector delayedUserMessageRuntimeProtector) Protect(
	ctx context.Context,
	plaintext []byte,
) (session.ProtectedContent, error) {
	return protector.delegate.Protect(ctx, plaintext)
}

func (protector delayedUserMessageRuntimeProtector) Open(
	ctx context.Context,
	protected session.ProtectedContent,
	digest string,
) ([]byte, error) {
	if protector.entered != nil {
		select {
		case protector.entered <- struct{}{}:
		default:
		}
	}
	timer := time.NewTimer(protector.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return protector.delegate.Open(ctx, protected, digest)
	}
}

func expireUserMessageRuntimeIntegrationLease(t *testing.T, db *gorm.DB, claim usermessageruntime.Claim) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		input := tx.Exec(`
			UPDATE agent.session_input
			SET lease_until = clock_timestamp() - INTERVAL '1 second'
			WHERE id = ? AND lease_owner = ? AND fence_token = ?`,
			claim.Context.InputID, claim.Owner, claim.FenceToken)
		if input.Error != nil || input.RowsAffected != 1 {
			return fmt.Errorf("expire input lease provenance: rows=%d: %w", input.RowsAffected, input.Error)
		}
		lane := tx.Exec(`
			UPDATE agent.session_runtime_lease
			SET lease_until = clock_timestamp() - INTERVAL '1 second'
			WHERE session_id = ? AND lease_owner = ? AND fence_token = ?`,
			claim.Context.SessionID, claim.Owner, claim.FenceToken)
		if lane.Error != nil || lane.RowsAffected != 1 {
			return fmt.Errorf("expire runtime lease: rows=%d: %w", lane.RowsAffected, lane.Error)
		}
		return nil
	}); err != nil {
		t.Fatalf("制造 User Message Runtime 过期 Lease 失败: %v", err)
	}
}

func setUserMessageRuntimeIntegrationLeaseRemaining(
	t *testing.T,
	db *gorm.DB,
	claim usermessageruntime.Claim,
	remaining time.Duration,
) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		input := tx.Exec(`
			UPDATE agent.session_input
			SET lease_until = clock_timestamp() + (? * INTERVAL '1 microsecond')
			WHERE id = ? AND lease_owner = ? AND fence_token = ?`,
			remaining.Microseconds(), claim.Context.InputID, claim.Owner, claim.FenceToken)
		if input.Error != nil || input.RowsAffected != 1 {
			return fmt.Errorf("shorten input lease provenance: rows=%d: %w", input.RowsAffected, input.Error)
		}
		lane := tx.Exec(`
			UPDATE agent.session_runtime_lease
			SET lease_until = clock_timestamp() + (? * INTERVAL '1 microsecond')
			WHERE session_id = ? AND lease_owner = ? AND fence_token = ?`,
			remaining.Microseconds(), claim.Context.SessionID, claim.Owner, claim.FenceToken)
		if lane.Error != nil || lane.RowsAffected != 1 {
			return fmt.Errorf("shorten runtime lease: rows=%d: %w", lane.RowsAffected, lane.Error)
		}
		return nil
	}); err != nil {
		t.Fatalf("缩短 User Message Runtime Lease 失败: %v", err)
	}
}

func assertUserMessageRuntimeIntegrationRunState(
	t *testing.T,
	db *gorm.DB,
	runID string,
	wantStatus string,
	wantStarted bool,
) {
	t.Helper()
	var state struct {
		Status    string     `gorm:"column:status"`
		StartedAt *time.Time `gorm:"column:started_at"`
	}
	if err := db.Raw(`SELECT status, started_at FROM agent.session_user_message_run WHERE run_id = ?`, runID).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取 User Message Run 状态失败: %v", err)
	}
	if state.Status != wantStatus || (state.StartedAt != nil) != wantStarted {
		t.Fatalf("User Message Run 状态=%+v want status=%s started=%v", state, wantStatus, wantStarted)
	}
}

func assertUserMessageRuntimeIntegrationTerminalRollback(
	t *testing.T,
	db *gorm.DB,
	claim usermessageruntime.Claim,
) {
	t.Helper()
	var state struct {
		InputStatus      string `gorm:"column:input_status"`
		TurnStatus       string `gorm:"column:turn_status"`
		RunStatus        string `gorm:"column:run_status"`
		ProjectionCount  int64  `gorm:"column:projection_count"`
		TerminalEvents   int64  `gorm:"column:terminal_events"`
		FrozenOutputRows int64  `gorm:"column:frozen_output_rows"`
	}
	if err := db.Raw(`
		SELECT input_record.status AS input_status,
		       turn_record.status AS turn_status,
		       run_record.status AS run_status,
		       (SELECT COUNT(*) FROM agent.session_user_message_output_projection WHERE turn_id = ?) AS projection_count,
		       (SELECT COUNT(*) FROM agent.session_event_log WHERE event_id = ?) AS terminal_events,
		       (SELECT COUNT(*) FROM agent.session_user_message_output_receipt WHERE output_id = ? AND status = 'completed') AS frozen_output_rows
		FROM agent.session_input AS input_record
		JOIN agent.session_user_message_turn AS turn_record ON turn_record.input_id = input_record.id
		JOIN agent.session_user_message_run AS run_record ON run_record.input_id = input_record.id
		WHERE input_record.id = ?`,
		claim.Context.TurnID, claim.TerminalEventID, claim.OutputID, claim.Context.InputID).Scan(&state).Error; err != nil {
		t.Fatalf("读取 Lease 到期 Complete 回滚事实失败: %v", err)
	}
	if state.InputStatus != "running" || state.TurnStatus != "running" || state.RunStatus != "running" ||
		state.ProjectionCount != 0 || state.TerminalEvents != 0 || state.FrozenOutputRows != 1 {
		t.Fatalf("Lease 到期 Complete 产生非零终态写: %+v", state)
	}
}

func mustUserMessageRuntimeIntegrationID(t *testing.T, generator idgen.UUIDv7) string {
	t.Helper()
	value, err := generator.New()
	if err != nil {
		t.Fatalf("生成 User Message Runtime 测试 UUIDv7 失败: %v", err)
	}
	return value
}
