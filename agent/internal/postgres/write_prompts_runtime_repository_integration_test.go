package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/FigoGoo/Dora-Agent/agent/internal/writepromptsruntime"
	einotool "github.com/cloudwego/eino/components/tool"
	"gorm.io/gorm"
)

// TestWritePromptsRuntimePostgreSQLSmoke 在真实 PostgreSQL 验证 Migration、typed ingress、HOL Claim、
// 双层 Model/Tool Receipt、completed 原子终态，以及 session.workspace.v4 与 EventLog 的同源投影。
func TestWritePromptsRuntimePostgreSQLSmoke(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(planStoryboardRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("DORA_AGENT_TEST_POSTGRES_DSN 未设置，跳过真实 PostgreSQL 冒烟测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 8, MaxIdleConns: 4,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Write Prompts Runtime PostgreSQL 失败: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	ensurePlanStoryboardRuntimeMigration(t, client.db)
	ensureWritePromptsRuntimeMigration(t, client.db)
	if err := client.VerifyWritePromptsRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Write Prompts Runtime Schema 未就绪: %v", err)
	}
	quiesceWritePromptsIntegrationRows(t, client.db)

	ids := idgen.UUIDv7{}
	userID := mustPlanStoryboardIntegrationID(t, ids)
	projectID := mustPlanStoryboardIntegrationID(t, ids)
	sessionID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, sessionID, userID, projectID)
	protector, err := contentcrypto.NewAES256GCMProtector(bytes.Repeat([]byte{0x77}, 32), "write-prompts-integration-v1")
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewWritePromptsRuntimeRepository(client, protector, ids)
	if err != nil {
		t.Fatal(err)
	}
	intentJSON, err := json.Marshal(writeprompts.Intent{
		SchemaVersion:      writeprompts.IntentSchemaVersion,
		WritingInstruction: "为 Storyboard 全部 Slot 生成提示词",
		OutputLanguage:     "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	command := writepromptsruntime.EnqueueCommand{
		RequestID: mustPlanStoryboardIntegrationID(t, ids), SessionID: sessionID,
		UserID: userID, ProjectID: projectID, IdempotencyKey: mustPlanStoryboardIntegrationID(t, ids),
		StoryboardPreviewRef: writeprompts.StoryboardPreviewRef{
			ID: mustPlanStoryboardIntegrationID(t, ids), Version: 1, ContentDigest: strings.Repeat("b", 64),
		},
		IntentJSON: intentJSON, AccessScopeRef: "write-prompts.preview.access-scope@v1",
		AccessScopeDigest: strings.Repeat("a", 64), IntentKeyVersion: "write-prompts-integration-v1",
	}
	enqueued, err := repository.Enqueue(ctx, command, time.Now())
	if err != nil || enqueued.Replayed {
		t.Fatalf("Write Prompts 首次入队失败: result=%+v err=%v", enqueued, err)
	}
	replayed, err := repository.Enqueue(ctx, command, time.Now())
	if err != nil || !replayed.Replayed || replayed.InputID != enqueued.InputID || replayed.RunID != enqueued.RunID {
		t.Fatalf("Write Prompts 同义入队未稳定重放: first=%+v replay=%+v err=%v", enqueued, replayed, err)
	}
	business := &writePromptsIntegrationBusiness{
		promptPreviewID: mustPlanStoryboardIntegrationID(t, ids),
		generation: writeprompts.GenerationContext{
			ProjectID: projectID, ProjectVersion: 1, ProjectTitle: "集成测试项目",
			Storyboard: writeprompts.StoryboardResource{
				ID: command.StoryboardPreviewRef.ID, ProjectID: projectID, Version: 1, Status: "draft",
				ContentDigest: command.StoryboardPreviewRef.ContentDigest,
				Content: writeprompts.StoryboardContent{
					SchemaVersion: "storyboard.preview.draft.v1", Title: "单场景故事板", Summary: "验证提示词完整纵切",
					Elements: []writeprompts.StoryboardElement{{
						Key: "element_1", Order: 1, Title: "开场镜头", NarrativePurpose: "建立品牌氛围",
					}},
					Slots: []writeprompts.StoryboardSlot{{
						Key: "slot_1", ElementKey: "element_1", SlotType: "image", Purpose: "生成开场画面", Required: true,
					}},
				},
			},
		},
	}
	journal, err := writepromptsruntime.NewCommandJournal(repository)
	if err != nil {
		t.Fatal(err)
	}
	promptModel, err := writepromptsruntime.NewReceiptModel(
		writepromptsruntime.NewFakePromptModel(), repository, writepromptsruntime.ModelCallGraphPrompt,
	)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := writeprompts.Compile(ctx, promptModel, business, business, journal, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	coreTool, err := writeprompts.NewTool(graph, writepromptsruntime.ResolveCoreContext)
	if err != nil {
		t.Fatal(err)
	}
	receiptTool, err := writepromptsruntime.NewReceiptTool(ctx, coreTool, repository)
	if err != nil {
		t.Fatal(err)
	}
	routerModel, err := writepromptsruntime.NewReceiptModel(
		writepromptsruntime.NewFakeRouter(), repository, writepromptsruntime.ModelCallRouter,
	)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := chatmodelagent.NewWritePrompts(ctx, routerModel, []einotool.BaseTool{receiptTool})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := writepromptsruntime.NewEinoRunner(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := writepromptsruntime.NewRecoveryCoordinator(business, repository, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	processor, err := writepromptsruntime.NewProcessor(
		repository, runner, recovery, clock.System{}, "write-prompts-integration-owner",
		writepromptsruntime.ProcessorConfig{
			Concurrency: 1, PollInterval: 10 * time.Millisecond, LeaseDuration: 10 * time.Second,
			HeartbeatInterval: time.Second, RetryDelay: 10 * time.Millisecond, RecoveryDelay: 10 * time.Millisecond,
			ProjectionDelay: 10 * time.Millisecond, MaxAttempts: 3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = processor.Stop(context.Background()) }()
	processor.Wake()

	workspaceRepository, err := NewWorkspaceRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	workspaceService, err := workspace.NewService(
		workspaceRepository, protector, workspace.SnapshotLimits{MaxMessages: 10, MaxInputs: 10}, 256*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot workspace.Snapshot
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = workspaceService.LoadSnapshot(ctx, workspace.Identity{
			UserID: userID, ProjectID: projectID, SessionID: sessionID,
		}, command.RequestID)
		if err == nil && snapshot.WritePromptsPreview != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || snapshot.SchemaVersion != workspace.SnapshotSchemaVersionV4 ||
		snapshot.WritePromptsPreview == nil || snapshot.WritePromptsPreview.InputID != enqueued.InputID ||
		snapshot.WritePromptsPreview.ResultCode != writeprompts.ResultCodeCompleted ||
		snapshot.WritePromptsPreview.PromptPreviewID != business.promptPreviewID ||
		snapshot.WritePromptsPreview.TargetCount != 1 || len(snapshot.WritePromptsPreview.Prompts) != 1 || len(snapshot.Inputs) != 1 ||
		snapshot.Inputs[0].MessageID != nil {
		t.Fatalf("Write Prompts Workspace v4 投影失败: snapshot=%+v err=%v", snapshot, err)
	}
	if business.saves != 1 {
		t.Fatalf("Write Prompts Business Save 次数=%d want=1", business.saves)
	}

	// 单独停止 Processor 后手工模拟：旧进程已预留 Model Receipt，但在冻结结果前崩溃。
	// 新 owner 获得更高 Fence 后只能观察 reserved 并失败关闭，不得再次获得 execute 权限。
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := processor.Stop(stopCtx); err != nil {
		t.Fatalf("停止 Write Prompts Processor 失败: %v", err)
	}
	takeoverSessionID := mustPlanStoryboardIntegrationID(t, ids)
	takeoverProjectID := mustPlanStoryboardIntegrationID(t, ids)
	seedPlanStoryboardSession(t, client.db, takeoverSessionID, userID, takeoverProjectID)
	takeoverCommand := command
	takeoverCommand.RequestID = mustPlanStoryboardIntegrationID(t, ids)
	takeoverCommand.SessionID = takeoverSessionID
	takeoverCommand.ProjectID = takeoverProjectID
	takeoverCommand.IdempotencyKey = mustPlanStoryboardIntegrationID(t, ids)
	takeoverCommand.StoryboardPreviewRef.ID = mustPlanStoryboardIntegrationID(t, ids)
	takeoverResult, err := repository.Enqueue(ctx, takeoverCommand, time.Now())
	if err != nil || takeoverResult.Replayed {
		t.Fatalf("入队 Model Receipt takeover 样本失败: result=%+v err=%v", takeoverResult, err)
	}
	firstClaim, err := repository.ClaimNext(ctx, "write-prompts-crashed-owner", time.Now(), 10*time.Second)
	if err != nil || firstClaim == nil || firstClaim.Context.InputID != takeoverResult.InputID {
		t.Fatalf("首次领取 Model Receipt takeover 样本失败: claim=%+v err=%v", firstClaim, err)
	}
	requestDigest := strings.Repeat("c", 64)
	firstIdentity := writePromptsModelIdentity(*firstClaim, firstClaim.Context.GraphModelCallID, writepromptsruntime.ModelCallGraphPrompt)
	reserved, execute, err := repository.ReplayOrReserveModel(ctx, firstIdentity, requestDigest)
	if err != nil || reserved.Stage != writepromptsruntime.ModelReceiptReserved || !execute {
		t.Fatalf("首次 Model Receipt 预留失败: receipt=%+v execute=%v err=%v", reserved, execute, err)
	}
	expireWritePromptsLease(t, client.db, *firstClaim)
	secondClaim, err := repository.ClaimNext(ctx, "write-prompts-takeover-owner", time.Now(), 10*time.Second)
	if err != nil || secondClaim == nil || secondClaim.Context.InputID != firstClaim.Context.InputID ||
		secondClaim.FenceToken <= firstClaim.FenceToken {
		t.Fatalf("更高 Fence takeover 领取失败: first=%+v second=%+v err=%v", firstClaim, secondClaim, err)
	}
	secondIdentity := writePromptsModelIdentity(*secondClaim, secondClaim.Context.GraphModelCallID, writepromptsruntime.ModelCallGraphPrompt)
	reserved, execute, err = repository.ReplayOrReserveModel(ctx, secondIdentity, requestDigest)
	if err != nil || reserved.Stage != writepromptsruntime.ModelReceiptReserved || execute {
		t.Fatalf("更高 Fence 重放不得再执行 Model: receipt=%+v execute=%v err=%v", reserved, execute, err)
	}
	var persistedFence int64
	if err := client.db.Raw(`SELECT execution_fence FROM agent.write_prompts_preview_model_receipt WHERE model_call_id = ?`,
		firstIdentity.ModelCallID).Scan(&persistedFence).Error; err != nil || persistedFence != firstClaim.FenceToken {
		t.Fatalf("reserved Model Receipt Fence 被 takeover 篡改: got=%d want=%d err=%v", persistedFence, firstClaim.FenceToken, err)
	}
}

type writePromptsIntegrationBusiness struct {
	generation      writeprompts.GenerationContext
	promptPreviewID string
	saves           int
}

func (business *writePromptsIntegrationBusiness) GetPromptGenerationContext(
	_ context.Context,
	trusted writeprompts.TrustedContext,
) (writeprompts.GenerationContext, error) {
	if trusted.ProjectID != business.generation.ProjectID || trusted.StoryboardPreviewRef.ID != business.generation.Storyboard.ID {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessNotFound
	}
	return business.generation, nil
}

func (business *writePromptsIntegrationBusiness) SavePromptPreviewDraft(
	_ context.Context,
	command writeprompts.DraftCommand,
) (writeprompts.SaveDisposition, writeprompts.Resource, error) {
	business.saves++
	digest, err := writeprompts.ContentDigest(command.Content)
	if err != nil {
		return "", writeprompts.Resource{}, err
	}
	resource := writeprompts.Resource{
		PromptPreviewID: business.promptPreviewID, ProjectID: command.TrustedContext.ProjectID,
		StoryboardPreviewRef: command.TrustedContext.StoryboardPreviewRef, Version: 1, Status: "draft",
		ContentDigest: digest, ExactTargetSetDigest: command.ExactTargetSetDigest, Content: command.Content,
	}
	if writeprompts.ValidateResourceForCommand(resource, command) != nil {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessTechnical
	}
	return writeprompts.SaveDispositionCreated, resource, nil
}

func (*writePromptsIntegrationBusiness) QueryPromptPreviewCommand(
	context.Context,
	writeprompts.DraftCommand,
) (string, *writeprompts.Resource, error) {
	return "not_found", nil, nil
}

func ensureWritePromptsRuntimeMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	encoded, err := os.ReadFile("../../migrations/20260717001200_add_write_prompts_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 Write Prompts Runtime Migration 失败: %v", err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(0x5750524d33)).Error; err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw("SELECT to_regclass('agent.write_prompts_preview_run') IS NOT NULL").Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			return nil
		}
		return tx.Exec(string(encoded)).Error
	})
	if err != nil {
		t.Fatalf("应用 Write Prompts Runtime Migration 失败: %v", err)
	}
}

func quiesceWritePromptsIntegrationRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`UPDATE agent.session_runtime_lease AS lease
		 SET lease_owner = NULL, lease_until = NULL, version = version + 1, updated_at = clock_timestamp()
		 WHERE EXISTS (
		   SELECT 1 FROM agent.session_input AS input_record
		   WHERE input_record.session_id = lease.session_id
		     AND input_record.source_type = 'write_prompts_preview'
		     AND input_record.status IN ('pending','claimed','running','retry_wait','recovery_pending')
		 )`,
		`UPDATE agent.session_input SET status = 'resolved', lease_owner = NULL, lease_until = NULL, updated_at = clock_timestamp()
		 WHERE source_type = 'write_prompts_preview'
		   AND status IN ('pending','claimed','running','retry_wait','recovery_pending')`,
		`UPDATE agent.write_prompts_preview_run
		 SET status = 'completed', started_at = COALESCE(started_at, clock_timestamp()),
		     completed_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
		 WHERE status IN ('created','running','recovery_pending')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("收敛上次 Write Prompts 集成测试残留行失败: %v", err)
		}
	}
}

func expireWritePromptsLease(t *testing.T, db *gorm.DB, claim writepromptsruntime.Claim) {
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
			t.Fatalf("过期 Write Prompts Lease 失败: rows=%d err=%v", result.RowsAffected, result.Error)
		}
	}
}

func writePromptsModelIdentity(
	claim writepromptsruntime.Claim,
	modelCallID string,
	kind writepromptsruntime.ModelCallKind,
) writepromptsruntime.ModelReceiptIdentity {
	return writepromptsruntime.ModelReceiptIdentity{
		Owner: claim.Owner, FenceToken: claim.FenceToken, SessionID: claim.Context.SessionID,
		InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ModelCallID: modelCallID, CallKind: kind,
	}
}
