package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const analyzeMaterialsRuntimeIntegrationDSNEnv = "DORA_ANALYZE_MATERIALS_RUNTIME_POSTGRES_DSN"

// TestAnalyzeMaterialsRuntimePostgreSQLLifecycle 在显式专用 DSN 上验证无 Message 入队、HOL/Fence、分层回执和冻结投影恢复。
func TestAnalyzeMaterialsRuntimePostgreSQLLifecycle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(analyzeMaterialsRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_ANALYZE_MATERIALS_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL 生命周期探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Analyze Materials Runtime PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 Analyze Materials Runtime PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	if err := client.VerifyAnalyzeMaterialsRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Analyze Materials Runtime Schema 未就绪: %v", err)
	}

	outer := client.db.WithContext(ctx).Begin()
	if outer.Error != nil {
		t.Fatalf("开启 Analyze Materials Runtime 外层事务失败: %v", outer.Error)
	}
	defer func() {
		if rollbackErr := outer.Rollback().Error; rollbackErr != nil {
			t.Errorf("回滚 Analyze Materials Runtime 外层事务失败: %v", rollbackErr)
		}
	}()
	testClient := &Client{db: outer}
	ids := idgen.UUIDv7{}
	keyVersion := "analyze-materials-integration-v1"
	protector, err := contentcrypto.NewAES256GCMProtector(bytes.Repeat([]byte{0x5a}, 32), keyVersion)
	if err != nil {
		t.Fatalf("创建 Analyze Materials 内容保护器失败: %v", err)
	}
	repository, err := NewAnalyzeMaterialsRuntimeRepository(testClient, protector, ids)
	if err != nil {
		t.Fatalf("创建 Analyze Materials Repository 失败: %v", err)
	}

	userID := mustAnalyzeMaterialsIntegrationID(t, ids)
	projectID := mustAnalyzeMaterialsIntegrationID(t, ids)
	sessionID := mustAnalyzeMaterialsIntegrationID(t, ids)
	seedAnalyzeMaterialsSession(t, outer, sessionID, userID, projectID)
	assetID := mustAnalyzeMaterialsIntegrationID(t, ids)
	intent := analyzematerials.Intent{
		SchemaVersion: analyzematerials.IntentSchemaVersion, AssetIDs: []string{assetID},
		AnalysisGoal: "识别素材主题和可复用元素", FocusDimensions: []string{"content", "visual"},
		OutputLanguage: "zh-CN", ExpectedAssets: []analyzematerials.ExpectedAsset{{AssetID: assetID, AssetVersion: 1}},
	}
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("编码 Analyze Materials Intent 失败: %v", err)
	}
	command := analyzematerialsruntime.EnqueueCommand{
		RequestID: mustAnalyzeMaterialsIntegrationID(t, ids), SessionID: sessionID, UserID: userID,
		ProjectID: projectID, IdempotencyKey: mustAnalyzeMaterialsIntegrationID(t, ids), IntentJSON: intentJSON,
		AccessScopeRef: "material-analysis.preview.access-scope@v1", AccessScopeDigest: strings.Repeat("a", 64),
		IntentKeyVersion: keyVersion,
	}
	first, err := repository.Enqueue(ctx, command, time.Now())
	if err != nil {
		t.Fatalf("首次 Analyze Materials 入队失败: %v", err)
	}
	if first.Replayed || !canonicalAnalyzeMaterialsUUIDv7(first.InputID) || !canonicalAnalyzeMaterialsUUIDv7(first.TerminalEventID) {
		t.Fatalf("首次入队未返回稳定身份: %+v", first)
	}
	assertAnalyzeMaterialsNoMessageInput(t, outer, first.InputID, command.RequestID)
	assertAnalyzeMaterialsGuardRejects(t, outer, "context_guard",
		"UPDATE agent.analyze_materials_preview_turn_context SET profile = profile WHERE turn_id = ?", first.TurnID)

	replayCommand := command
	replayCommand.RequestID = mustAnalyzeMaterialsIntegrationID(t, ids)
	replayed, err := repository.Enqueue(ctx, replayCommand, time.Now())
	if err != nil || !replayed.Replayed || replayed.InputID != first.InputID || replayed.AcceptedEventID != first.AcceptedEventID {
		t.Fatalf("同义幂等重放未返回首写身份: first=%+v replay=%+v err=%v", first, replayed, err)
	}
	failClosedReplayRepository, err := NewAnalyzeMaterialsRuntimeRepository(testClient, failingAnalyzeMaterialsProtector{}, failingAnalyzeMaterialsIDs{})
	if err != nil {
		t.Fatalf("创建 fail-closed replay Repository 失败: %v", err)
	}
	replayedWithoutKMS, err := failClosedReplayRepository.Enqueue(ctx, replayCommand, time.Now())
	if err != nil || !replayedWithoutKMS.Replayed || replayedWithoutKMS.InputID != first.InputID {
		t.Fatalf("既有同义回放不应依赖 KMS 或随机源: result=%+v err=%v", replayedWithoutKMS, err)
	}
	var acceptedCount int64
	if err := outer.Model(&sessionEventLogModel{}).
		Where("session_id = ? AND event_type = 'analyze_materials.preview.accepted'", sessionID).
		Count(&acceptedCount).Error; err != nil || acceptedCount != 1 {
		t.Fatalf("同义重放创建了重复 accepted Event: count=%d err=%v", acceptedCount, err)
	}
	var acceptedSourceID string
	if err := outer.Model(&sessionEventLogModel{}).Select("source_id").
		Where("event_id = ?", first.AcceptedEventID).Scan(&acceptedSourceID).Error; err != nil || acceptedSourceID != command.RequestID {
		t.Fatalf("accepted Event 未冻结首次 RequestID: source=%q err=%v", acceptedSourceID, err)
	}
	conflictCommand := replayCommand
	conflictIntent := intent
	conflictIntent.AnalysisGoal = "生成不同语义"
	conflictCommand.IntentJSON, _ = json.Marshal(conflictIntent)
	if _, err := repository.Enqueue(ctx, conflictCommand, time.Now()); !errors.Is(err, analyzematerialsruntime.ErrIdempotencyConflict) {
		t.Fatalf("异义幂等重放未被拒绝: %v", err)
	}

	claimV1, err := repository.ClaimNext(ctx, "analyze-materials-owner-v1", time.Now(), 30*time.Second)
	if err != nil || claimV1 == nil || analyzematerialsruntime.ValidateClaim(*claimV1) != nil {
		t.Fatalf("首次全 Source HOL Claim 无效: claim=%+v err=%v", claimV1, err)
	}
	expireAnalyzeMaterialsLease(t, outer, *claimV1)
	claimV2, err := repository.ClaimNext(ctx, "analyze-materials-owner-v2", time.Now(), 30*time.Second)
	if err != nil || claimV2 == nil || claimV2.Context.RunID != claimV1.Context.RunID ||
		claimV2.FenceToken <= claimV1.FenceToken || claimV2.Attempts != 2 {
		t.Fatalf("Lease takeover 未复用身份或推进 Fence/Attempts: first=%+v second=%+v err=%v", claimV1, claimV2, err)
	}
	staleModelIdentity := analyzematerialsruntime.ModelReceiptIdentity{
		Owner: claimV1.Owner, FenceToken: claimV1.FenceToken, SessionID: sessionID,
		InputID: first.InputID, TurnID: first.TurnID, RunID: first.RunID,
		ModelCallID: first.RouterModelCallID, CallKind: analyzematerialsruntime.ModelCallRouter,
	}
	if _, _, err := repository.ReplayOrReserveModel(ctx, staleModelIdentity, strings.Repeat("b", 64)); !errors.Is(err, analyzematerialsruntime.ErrFenceLost) {
		t.Fatalf("旧 Fence 仍可创建 Model Receipt: %v", err)
	}
	if err := repository.MarkRunning(ctx, *claimV1, time.Now()); !errors.Is(err, analyzematerialsruntime.ErrFenceLost) {
		t.Fatalf("旧 Fence 仍可推进 Run: %v", err)
	}
	if err := repository.MarkRunning(ctx, *claimV2, time.Now()); err != nil {
		t.Fatalf("当前 Fence MarkRunning 失败: %v", err)
	}

	routerIdentity := analyzematerialsruntime.ModelReceiptIdentity{
		Owner: claimV2.Owner, FenceToken: claimV2.FenceToken, SessionID: sessionID,
		InputID: first.InputID, TurnID: first.TurnID, RunID: first.RunID,
		ModelCallID: first.RouterModelCallID, CallKind: analyzematerialsruntime.ModelCallRouter,
	}
	routerRequestDigest := strings.Repeat("b", 64)
	snapshot, execute, err := repository.ReplayOrReserveModel(ctx, routerIdentity, routerRequestDigest)
	if err != nil || !execute || snapshot.Stage != analyzematerialsruntime.ModelReceiptReserved {
		t.Fatalf("Router Model Receipt 首次 reserve 失败: snapshot=%+v execute=%v err=%v", snapshot, execute, err)
	}
	response := &schema.Message{Role: schema.Assistant, Content: "router response"}
	if err := repository.FreezeModelCompleted(ctx, routerIdentity, routerRequestDigest, response); err != nil {
		t.Fatalf("Router Model Receipt freeze 失败: %v", err)
	}
	assertAnalyzeMaterialsGuardRejects(t, outer, "model_guard",
		"UPDATE agent.analyze_materials_preview_model_receipt SET status = status WHERE model_call_id = ?", first.RouterModelCallID)
	snapshot, execute, err = repository.ReplayOrReserveModel(ctx, routerIdentity, routerRequestDigest)
	if err != nil || execute || snapshot.Stage != analyzematerialsruntime.ModelReceiptCompleted ||
		snapshot.Response == nil || snapshot.Response.Content != response.Content {
		t.Fatalf("Router Model Receipt 未原样重放: snapshot=%+v execute=%v err=%v", snapshot, execute, err)
	}
	graphIdentity := analyzematerialsruntime.ModelReceiptIdentity{
		Owner: claimV2.Owner, FenceToken: claimV2.FenceToken, SessionID: sessionID,
		InputID: first.InputID, TurnID: first.TurnID, RunID: first.RunID,
		ModelCallID: first.GraphModelCallID, CallKind: analyzematerialsruntime.ModelCallGraphAnalysis,
	}
	graphRequestDigest := strings.Repeat("c", 64)
	graphSnapshot, graphExecute, err := repository.ReplayOrReserveModel(ctx, graphIdentity, graphRequestDigest)
	if err != nil || !graphExecute || graphSnapshot.Stage != analyzematerialsruntime.ModelReceiptReserved {
		t.Fatalf("Graph Model Receipt 首次 reserve 失败: snapshot=%+v execute=%v err=%v", graphSnapshot, graphExecute, err)
	}
	if err := repository.FreezeModelFailed(ctx, graphIdentity, graphRequestDigest, "LOCAL_FAKE_MODEL_EXECUTION_FAILED"); err != nil {
		t.Fatalf("Graph Model Receipt failed freeze 失败: %v", err)
	}
	graphSnapshot, graphExecute, err = repository.ReplayOrReserveModel(ctx, graphIdentity, graphRequestDigest)
	if err != nil || graphExecute || graphSnapshot.Stage != analyzematerialsruntime.ModelReceiptFailed ||
		graphSnapshot.ErrorCode != "LOCAL_FAKE_MODEL_EXECUTION_FAILED" {
		t.Fatalf("Graph Model failed Receipt 未稳定重放: snapshot=%+v execute=%v err=%v", graphSnapshot, graphExecute, err)
	}

	toolIdentity := analyzematerialsruntime.ToolReceiptIdentity{
		Owner: claimV2.Owner, FenceToken: claimV2.FenceToken, SessionID: sessionID,
		InputID: first.InputID, TurnID: first.TurnID, RunID: first.RunID, ToolCallID: first.ToolCallID,
	}
	toolRequestDigest := analyzeMaterialsToolRequestDigest(claimV2.Context)
	toolSnapshot, execute, err := repository.ReplayOrOpenTool(ctx, toolIdentity, toolRequestDigest)
	if err != nil || !execute || toolSnapshot.Stage != analyzematerialsruntime.ToolReceiptOpen {
		t.Fatalf("Tool Receipt 首次执行权获取失败: snapshot=%+v execute=%v err=%v", toolSnapshot, execute, err)
	}
	retryable := false
	toolResult := analyzematerials.Result{
		SchemaVersion: analyzematerials.ResultSchemaVersion, Status: "failed",
		ResultCode:    analyzematerials.ResultCodeDependencyNotReady,
		InvocationRef: analyzematerials.InvocationRef{ToolCallID: first.ToolCallID},
		Summary:       "素材证据尚不足以生成可信分析", Retryable: &retryable,
	}
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		t.Fatalf("编码 Tool Result 失败: %v", err)
	}
	resultDigest := digestAnalyzeMaterialsBytes(resultJSON)
	if err := repository.FreezeToolResult(ctx, toolIdentity, toolRequestDigest, analyzematerialsruntime.ToolReceiptFailed, resultJSON, resultDigest); err != nil {
		t.Fatalf("冻结 Tool failed Result 失败: %v", err)
	}
	assertAnalyzeMaterialsGuardRejects(t, outer, "tool_guard",
		"UPDATE agent.analyze_materials_preview_tool_receipt SET status = status WHERE tool_call_id = ?", first.ToolCallID)
	if err := repository.DeferProjection(ctx, *claimV2, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("冻结后投影 defer 失败: %v", err)
	}
	claimV3, err := repository.ClaimNext(ctx, "analyze-materials-owner-v3", time.Now(), 30*time.Second)
	if err != nil || claimV3 == nil || claimV3.Attempts != claimV2.Attempts || claimV3.FenceToken <= claimV2.FenceToken {
		t.Fatalf("投影恢复 Claim 不应增加执行 Attempts: second=%+v third=%+v err=%v", claimV2, claimV3, err)
	}
	if err := repository.MarkRunning(ctx, *claimV3, time.Now()); err != nil {
		t.Fatalf("投影恢复 MarkRunning 失败: %v", err)
	}
	toolIdentity.Owner, toolIdentity.FenceToken = claimV3.Owner, claimV3.FenceToken
	toolSnapshot, execute, err = repository.ReplayOrOpenTool(ctx, toolIdentity, toolRequestDigest)
	if err != nil || execute || toolSnapshot.Stage != analyzematerialsruntime.ToolReceiptFailed ||
		!bytes.Equal(toolSnapshot.ResultJSON, resultJSON) || toolSnapshot.ResultDigest != resultDigest {
		t.Fatalf("更高 Fence 未原样重放 frozen Tool Result: snapshot=%+v execute=%v err=%v", toolSnapshot, execute, err)
	}
	if err := repository.CompleteToolResult(ctx, *claimV3, toolResult, time.Now()); err != nil {
		t.Fatalf("Tool failed Result 终态投影失败: %v", err)
	}
	assertAnalyzeMaterialsTerminalFacts(t, outer, first.InputID, "resolved", "completed", "tool_failed", resultDigest)
	assertAnalyzeMaterialsGuardRejects(t, outer, "projection_guard",
		"UPDATE agent.analyze_materials_preview_projection SET status = status WHERE source_input_id = ?", first.InputID)
	var terminalSourceID string
	if err := outer.Model(&sessionEventLogModel{}).Select("source_id").Where("event_id = ?", first.TerminalEventID).
		Scan(&terminalSourceID).Error; err != nil || terminalSourceID != command.RequestID {
		t.Fatalf("terminal Event 未复用首次 RequestID: source=%q err=%v", terminalSourceID, err)
	}

	var modelReceiptCount int64
	if err := outer.Model(&analyzeMaterialsPreviewModelReceiptModel{}).Count(&modelReceiptCount).Error; err != nil || modelReceiptCount != 2 {
		t.Fatalf("旧 Fence 或重放产生了额外 Model Receipt: count=%d err=%v", modelReceiptCount, err)
	}

	runtimeFailureSessionID := mustAnalyzeMaterialsIntegrationID(t, ids)
	runtimeFailureProjectID := mustAnalyzeMaterialsIntegrationID(t, ids)
	seedAnalyzeMaterialsSession(t, outer, runtimeFailureSessionID, userID, runtimeFailureProjectID)
	runtimeFailureCommand := command
	runtimeFailureCommand.RequestID = mustAnalyzeMaterialsIntegrationID(t, ids)
	runtimeFailureCommand.SessionID = runtimeFailureSessionID
	runtimeFailureCommand.ProjectID = runtimeFailureProjectID
	runtimeFailureCommand.IdempotencyKey = mustAnalyzeMaterialsIntegrationID(t, ids)
	runtimeFailureRun, err := repository.Enqueue(ctx, runtimeFailureCommand, time.Now())
	if err != nil {
		t.Fatalf("Runtime Failure 用例入队失败: %v", err)
	}
	runtimeFailureClaim, err := repository.ClaimNext(ctx, "analyze-materials-runtime-failure-owner", time.Now(), 30*time.Second)
	if err != nil || runtimeFailureClaim == nil {
		t.Fatalf("Runtime Failure 用例 Claim 失败: claim=%+v err=%v", runtimeFailureClaim, err)
	}
	failure := analyzematerialsruntime.RuntimeFailure{
		SchemaVersion: "analyze_materials.preview.runtime_failure.v1", InputID: runtimeFailureRun.InputID,
		TurnID: runtimeFailureRun.TurnID, RunID: runtimeFailureRun.RunID, Code: "ANALYZE_MATERIALS_RUNTIME_FAILED",
		Summary: "素材分析运行时暂时无法完成", Retryable: false,
	}
	if err := repository.CompleteRuntimeFailure(ctx, *runtimeFailureClaim, failure, time.Now()); err != nil {
		t.Fatalf("claimed/created 直接 Runtime Failure 收尾失败: %v", err)
	}
	var runtimeProjection analyzeMaterialsPreviewProjectionModel
	if err := outer.Where("source_input_id = ?", runtimeFailureRun.InputID).Take(&runtimeProjection).Error; err != nil ||
		runtimeProjection.OutcomeKind != "runtime_failed" || runtimeProjection.Status != "failed" {
		t.Fatalf("Runtime Failure 投影未与 Tool failed 分离: projection=%+v err=%v", runtimeProjection, err)
	}
	var runtimeInput sessionInputModel
	var runtimeRun analyzeMaterialsPreviewRunModel
	if err := outer.Where("id = ?", runtimeFailureRun.InputID).Take(&runtimeInput).Error; err != nil || runtimeInput.Status != "dead" {
		t.Fatalf("Runtime Failure Input 未进入 dead: input=%+v err=%v", runtimeInput, err)
	}
	if err := outer.Where("input_id = ?", runtimeFailureRun.InputID).Take(&runtimeRun).Error; err != nil || runtimeRun.Status != "failed" {
		t.Fatalf("Runtime Failure Run 未进入 failed: run=%+v err=%v", runtimeRun, err)
	}

	blockedSessionID := mustAnalyzeMaterialsIntegrationID(t, ids)
	blockedProjectID := mustAnalyzeMaterialsIntegrationID(t, ids)
	seedAnalyzeMaterialsSession(t, outer, blockedSessionID, userID, blockedProjectID)
	seedAnalyzeMaterialsBlockedHead(t, outer, blockedSessionID, ids)
	blockedCommand := command
	blockedCommand.RequestID = mustAnalyzeMaterialsIntegrationID(t, ids)
	blockedCommand.SessionID = blockedSessionID
	blockedCommand.ProjectID = blockedProjectID
	blockedCommand.IdempotencyKey = mustAnalyzeMaterialsIntegrationID(t, ids)
	if _, err := repository.Enqueue(ctx, blockedCommand, time.Now()); !errors.Is(err, analyzematerialsruntime.ErrSessionLaneBlocked) {
		t.Fatalf("非本 Profile 未终态 HOL 未阻断入队: %v", err)
	}
	var blockedInputCount int64
	if err := outer.Model(&sessionInputModel{}).Where("session_id = ?", blockedSessionID).Count(&blockedInputCount).Error; err != nil || blockedInputCount != 1 {
		t.Fatalf("Lane blocked 事务产生了增量 Input: count=%d err=%v", blockedInputCount, err)
	}

	queuedSessionID := mustAnalyzeMaterialsIntegrationID(t, ids)
	queuedProjectID := mustAnalyzeMaterialsIntegrationID(t, ids)
	seedAnalyzeMaterialsSession(t, outer, queuedSessionID, userID, queuedProjectID)
	queuedCommand := command
	queuedCommand.RequestID = mustAnalyzeMaterialsIntegrationID(t, ids)
	queuedCommand.SessionID = queuedSessionID
	queuedCommand.ProjectID = queuedProjectID
	queuedFirst, err := repository.Enqueue(ctx, queuedCommand, time.Now())
	if err != nil {
		t.Fatalf("复合幂等键未允许跨 Session 使用: %v", err)
	}
	queuedCommand.RequestID = mustAnalyzeMaterialsIntegrationID(t, ids)
	queuedCommand.IdempotencyKey = mustAnalyzeMaterialsIntegrationID(t, ids)
	queuedSecond, err := repository.Enqueue(ctx, queuedCommand, time.Now())
	if err != nil || queuedSecond.InputID == queuedFirst.InputID {
		t.Fatalf("同 Profile 非终态后继未能继续入队: first=%+v second=%+v err=%v", queuedFirst, queuedSecond, err)
	}
}

type failingAnalyzeMaterialsProtector struct{}

func (failingAnalyzeMaterialsProtector) Protect(context.Context, []byte) (session.ProtectedContent, error) {
	return session.ProtectedContent{}, fmt.Errorf("protector must not be called")
}

func (failingAnalyzeMaterialsProtector) Open(context.Context, session.ProtectedContent, string) ([]byte, error) {
	return nil, fmt.Errorf("protector must not be called")
}

type failingAnalyzeMaterialsIDs struct{}

func (failingAnalyzeMaterialsIDs) New() (string, error) {
	return "", fmt.Errorf("id generator must not be called")
}

func seedAnalyzeMaterialsSession(t *testing.T, db *gorm.DB, sessionID, userID, projectID string) {
	t.Helper()
	now := time.Now().UTC()
	for _, record := range []any{
		&sessionModel{ID: sessionID, ProjectID: projectID, UserID: userID, Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now},
		&sessionSequenceCounterModel{SessionID: sessionID, LastMessageSeq: 0, LastInputEnqueueSeq: 0, UpdatedAt: now},
		&sessionEventCounterModel{SessionID: sessionID, LastSeq: 0, MinAvailableSeq: 1, UpdatedAt: now},
		&sessionRuntimeLeaseModel{SessionID: sessionID, FenceToken: 0, Version: 1, UpdatedAt: now},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed Analyze Materials Session 失败: %v", err)
		}
	}
}

func seedAnalyzeMaterialsBlockedHead(t *testing.T, db *gorm.DB, sessionID string, ids idgen.UUIDv7) {
	t.Helper()
	now := time.Now().UTC()
	input := sessionInputModel{
		ID: mustAnalyzeMaterialsIntegrationID(t, ids), SessionID: sessionID,
		SourceType: string(session.InputSourceTypeUserMessage), SourceID: mustAnalyzeMaterialsIntegrationID(t, ids),
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

func mustAnalyzeMaterialsIntegrationID(t *testing.T, ids idgen.UUIDv7) string {
	t.Helper()
	value, err := ids.New()
	if err != nil {
		t.Fatalf("生成 Analyze Materials UUIDv7 失败: %v", err)
	}
	return value
}

func assertAnalyzeMaterialsNoMessageInput(t *testing.T, db *gorm.DB, inputID, requestID string) {
	t.Helper()
	var input sessionInputModel
	if err := db.Where("id = ?", inputID).Take(&input).Error; err != nil {
		t.Fatalf("读取 Analyze Materials Input 失败: %v", err)
	}
	if input.MessageID != nil || input.SourceType != analyzematerialsruntime.SourceType || input.SourceID != requestID {
		t.Fatalf("typed Intent 被伪装成 Message 或 source 漂移: %+v", input)
	}
	var messageCount int64
	if err := db.Model(&sessionMessageModel{}).Where("session_id = ?", input.SessionID).Count(&messageCount).Error; err != nil || messageCount != 0 {
		t.Fatalf("Analyze Materials 入队不应创建 session_message: count=%d err=%v", messageCount, err)
	}
}

func expireAnalyzeMaterialsLease(t *testing.T, db *gorm.DB, claim analyzematerialsruntime.Claim) {
	t.Helper()
	for _, statement := range []string{
		"UPDATE agent.session_runtime_lease SET lease_until = clock_timestamp() - INTERVAL '1 second' WHERE session_id = ? AND fence_token = ?",
		"UPDATE agent.session_input SET lease_until = clock_timestamp() - INTERVAL '1 second' WHERE id = ? AND fence_token = ?",
	} {
		identifier := claim.Context.SessionID
		if strings.Contains(statement, "session_input") {
			identifier = claim.Context.InputID
		}
		result := db.Exec(statement, identifier, claim.FenceToken)
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatalf("过期 Analyze Materials Lease 失败: rows=%d err=%v", result.RowsAffected, result.Error)
		}
	}
}

func assertAnalyzeMaterialsTerminalFacts(
	t *testing.T,
	db *gorm.DB,
	inputID, inputStatus, runStatus, outcomeKind, resultDigest string,
) {
	t.Helper()
	var input sessionInputModel
	if err := db.Where("id = ?", inputID).Take(&input).Error; err != nil || input.Status != inputStatus || input.LeaseOwner != nil || input.LeaseUntil != nil {
		t.Fatalf("Input 终态或 Lease release 错误: input=%+v err=%v", input, err)
	}
	var run analyzeMaterialsPreviewRunModel
	if err := db.Where("input_id = ?", inputID).Take(&run).Error; err != nil || run.Status != runStatus || run.CompletedAt == nil {
		t.Fatalf("Run 终态错误: run=%+v err=%v", run, err)
	}
	var projection analyzeMaterialsPreviewProjectionModel
	if err := db.Where("source_input_id = ?", inputID).Take(&projection).Error; err != nil ||
		projection.OutcomeKind != outcomeKind || projection.ResultDigest != resultDigest {
		t.Fatalf("Projection 首写事实错误: projection=%+v err=%v", projection, err)
	}
}

func assertAnalyzeMaterialsGuardRejects(t *testing.T, db *gorm.DB, savepoint, statement string, arguments ...any) {
	t.Helper()
	if err := db.Exec("SAVEPOINT " + savepoint).Error; err != nil {
		t.Fatalf("创建 %s savepoint 失败: %v", savepoint, err)
	}
	if err := db.Exec(statement, arguments...).Error; err == nil {
		t.Fatalf("数据库 Guard 未拒绝变更: %s", statement)
	}
	if err := db.Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error; err != nil {
		t.Fatalf("恢复 %s savepoint 失败: %v", savepoint, err)
	}
}
