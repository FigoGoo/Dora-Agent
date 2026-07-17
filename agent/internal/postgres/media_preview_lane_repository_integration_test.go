package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"gorm.io/gorm"
)

// TestMediaRuntimePostgreSQLClaimNextMapsRequestColumns locks the real PostgreSQL
// request_record.* scan contract used by both generate_media and assemble_output.
func TestMediaRuntimePostgreSQLClaimNextMapsRequestColumns(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DORA_AGENT_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("DORA_AGENT_TEST_POSTGRES_DSN 未设置，跳过真实 PostgreSQL Claim 映射探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Media Runtime PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 Media Runtime PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Agent 基础 Schema 未就绪: %v", err)
	}
	if err := client.VerifyMediaRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Media Runtime Schema 未就绪: %v", err)
	}

	outer := client.db.WithContext(ctx).Begin()
	if outer.Error != nil {
		t.Fatalf("开启 Media Runtime 外层事务失败: %v", outer.Error)
	}
	defer func() {
		if rollbackErr := outer.Rollback().Error; rollbackErr != nil {
			t.Errorf("回滚 Media Runtime 外层事务失败: %v", rollbackErr)
		}
	}()
	// 专用测试库可能残留上次中断的媒体请求；只在最终回滚的事务内让它们退出本次候选集。
	if err := outer.Exec(`
		UPDATE agent.session_input
		SET status = 'resolved', updated_at = clock_timestamp()
		WHERE source_type IN (?, ?)
		  AND status IN ('pending','claimed','running','retry_wait','recovery_pending')`,
		mediapreviewruntime.GenerateSourceType, mediapreviewruntime.AssembleSourceType).Error; err != nil {
		t.Fatalf("隔离既有 Media Runtime 候选失败: %v", err)
	}

	testClient := &Client{db: outer}
	ids := idgen.UUIDv7{}
	repository, err := NewMediaRuntimeRepository(testClient, ids)
	if err != nil {
		t.Fatalf("创建 Media Runtime Repository 失败: %v", err)
	}
	userID := mustMediaRuntimeIntegrationID(t, ids)
	projectID := mustMediaRuntimeIntegrationID(t, ids)
	sessionID := mustMediaRuntimeIntegrationID(t, ids)
	seedMediaRuntimeSession(t, outer, sessionID, userID, projectID)
	promptPreviewID := mustMediaRuntimeIntegrationID(t, ids)
	intentJSON, err := mediapreview.CanonicalJSON(mediapreview.GenerateMediaIntent{
		SchemaVersion: mediapreview.GenerateMediaIntentVersion, PromptPreviewID: promptPreviewID,
		ExpectedPromptVersion: 1, ExpectedPromptContentDigest: strings.Repeat("a", 64),
		TargetLocalKey: "slot_1", OutputProfile: mediapreview.GenerateOutputProfile,
	})
	if err != nil {
		t.Fatalf("编码 generate_media Intent 失败: %v", err)
	}
	command := mediapreviewruntime.EnqueueCommand{
		EnqueueRequest: mediapreviewruntime.EnqueueRequest{
			RequestID: mustMediaRuntimeIntegrationID(t, ids), SessionID: sessionID,
			UserID: userID, ProjectID: projectID, IdempotencyKey: mustMediaRuntimeIntegrationID(t, ids),
			ToolKey: mediapreview.GenerateMediaToolKey, IntentJSON: intentJSON,
		},
		IntentSchemaVersion: mediapreview.GenerateMediaIntentVersion,
		IntentDigest:        mediaDigest(intentJSON), RequestDigest: strings.Repeat("b", 64),
		DeadlineAt: time.Now().UTC().Add(5 * time.Minute),
	}
	enqueued, err := repository.Enqueue(ctx, command)
	if err != nil {
		t.Fatalf("generate_media 入队失败: %v", err)
	}
	claim, err := repository.ClaimNext(ctx, mediapreviewruntime.GenerateSourceType, "media-claim-integration", time.Minute)
	if err != nil {
		t.Fatalf("generate_media ClaimNext 失败: %v", err)
	}
	if claim == nil {
		t.Fatal("generate_media 已是 Session HOL，但 ClaimNext 未返回请求")
	}
	if claim.RequestID != command.RequestID || claim.SessionID != sessionID || claim.InputID != enqueued.InputID ||
		claim.ToolCallID != enqueued.ToolCallID || claim.ToolKey != mediapreview.GenerateMediaToolKey || claim.Attempts != 1 || claim.FenceToken != 1 {
		t.Fatalf("request_record.* 映射或 Claim 身份漂移: claim=%+v enqueue=%+v", claim, enqueued)
	}
	if string(claim.IntentJSON) != string(intentJSON) || claim.IntentDigest != command.IntentDigest {
		t.Fatalf("generate_media JSONB Claim 未恢复 canonical Intent: got=%s want=%s", claim.IntentJSON, intentJSON)
	}

	assembleUserID := mustMediaRuntimeIntegrationID(t, ids)
	assembleProjectID := mustMediaRuntimeIntegrationID(t, ids)
	assembleSessionID := mustMediaRuntimeIntegrationID(t, ids)
	seedMediaRuntimeSession(t, outer, assembleSessionID, assembleUserID, assembleProjectID)
	assembleIntentJSON, err := mediapreview.CanonicalJSON(mediapreview.AssembleOutputIntent{
		SchemaVersion: mediapreview.AssembleOutputIntentVersion,
		SourceAssetID: mustMediaRuntimeIntegrationID(t, ids), ExpectedSourceVersion: 1,
		ExpectedSourceContentDigest: strings.Repeat("c", 64), OutputProfile: mediapreview.AssembleOutputProfile,
	})
	if err != nil {
		t.Fatalf("编码 assemble_output Intent 失败: %v", err)
	}
	assembleCommand := mediapreviewruntime.EnqueueCommand{
		EnqueueRequest: mediapreviewruntime.EnqueueRequest{
			RequestID: mustMediaRuntimeIntegrationID(t, ids), SessionID: assembleSessionID,
			UserID: assembleUserID, ProjectID: assembleProjectID, IdempotencyKey: mustMediaRuntimeIntegrationID(t, ids),
			ToolKey: mediapreview.AssembleOutputToolKey, IntentJSON: assembleIntentJSON,
		},
		IntentSchemaVersion: mediapreview.AssembleOutputIntentVersion,
		IntentDigest:        mediaDigest(assembleIntentJSON), RequestDigest: strings.Repeat("d", 64),
		DeadlineAt: time.Now().UTC().Add(5 * time.Minute),
	}
	assembleEnqueued, err := repository.Enqueue(ctx, assembleCommand)
	if err != nil {
		t.Fatalf("assemble_output 入队失败: %v", err)
	}
	assembleClaim, err := repository.ClaimNext(ctx, mediapreviewruntime.AssembleSourceType, "assemble-claim-integration", time.Minute)
	if err != nil {
		t.Fatalf("assemble_output ClaimNext 失败: %v", err)
	}
	if assembleClaim == nil || assembleClaim.RequestID != assembleCommand.RequestID ||
		assembleClaim.SessionID != assembleSessionID || assembleClaim.InputID != assembleEnqueued.InputID ||
		assembleClaim.ToolCallID != assembleEnqueued.ToolCallID || assembleClaim.ToolKey != mediapreview.AssembleOutputToolKey ||
		assembleClaim.Attempts != 1 || assembleClaim.FenceToken != 1 {
		t.Fatalf("assemble_output request_record 列映射或 Claim 身份漂移: claim=%+v enqueue=%+v", assembleClaim, assembleEnqueued)
	}
	if string(assembleClaim.IntentJSON) != string(assembleIntentJSON) || assembleClaim.IntentDigest != assembleCommand.IntentDigest {
		t.Fatalf("assemble_output JSONB Claim 未恢复 canonical Intent: got=%s want=%s", assembleClaim.IntentJSON, assembleIntentJSON)
	}
}

func seedMediaRuntimeSession(t *testing.T, db *gorm.DB, sessionID, userID, projectID string) {
	t.Helper()
	now := time.Now().UTC()
	for _, record := range []any{
		&sessionModel{ID: sessionID, ProjectID: projectID, UserID: userID, Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now},
		&sessionSequenceCounterModel{SessionID: sessionID, LastMessageSeq: 0, LastInputEnqueueSeq: 0, UpdatedAt: now},
		&sessionEventCounterModel{SessionID: sessionID, LastSeq: 0, MinAvailableSeq: 1, UpdatedAt: now},
		&sessionRuntimeLeaseModel{SessionID: sessionID, FenceToken: 0, Version: 1, UpdatedAt: now},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed Media Runtime Session 失败: %v", err)
		}
	}
}

func mustMediaRuntimeIntegrationID(t *testing.T, ids idgen.UUIDv7) string {
	t.Helper()
	value, err := ids.New()
	if err != nil {
		t.Fatalf("生成 Media Runtime UUIDv7 失败: %v", err)
	}
	return value
}
