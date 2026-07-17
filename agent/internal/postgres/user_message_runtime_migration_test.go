package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"gorm.io/gorm"
)

func TestUserMessageRuntimePreviewMigrationContract(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("../../migrations/20260717000800_add_user_message_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 user_message runtime Up Migration 失败: %v", err)
	}
	down, err := os.ReadFile("../../migrations/20260717000800_add_user_message_runtime_v2preview1.down.sql")
	if err != nil {
		t.Fatalf("读取 user_message runtime Down Migration 失败: %v", err)
	}
	guardUp, err := os.ReadFile("../../migrations/20260717000900_guard_user_message_legacy_upgrade.up.sql")
	if err != nil {
		t.Fatalf("读取 legacy upgrade Guard Up Migration 失败: %v", err)
	}
	guardDown, err := os.ReadFile("../../migrations/20260717000900_guard_user_message_legacy_upgrade.down.sql")
	if err != nil {
		t.Fatalf("读取 legacy upgrade Guard Down Migration 失败: %v", err)
	}
	upSQL := string(up)
	for _, table := range requiredUserMessageRuntimeTables() {
		if !strings.Contains(upSQL, "CREATE TABLE agent."+table) {
			t.Fatalf("Up Migration 缺少表 %s", table)
		}
		if !strings.Contains(upSQL, "COMMENT ON TABLE agent."+table+" IS '") {
			t.Fatalf("Up Migration 缺少表中文注释 %s", table)
		}
	}
	for _, required := range []string{
		"user_message.turn_context.v2preview1",
		"session.turn.completed",
		"session.turn.failed",
		"session.turn.recovery_pending",
		"CREATE TRIGGER trg_session_user_message_turn_context__immutable",
		"CREATE FUNCTION agent.guard_user_message_model_receipt_mutation()",
		"CREATE FUNCTION agent.guard_user_message_output_receipt_mutation()",
		"CREATE TRIGGER trg_session_user_message_model_receipt__guard",
		"CREATE TRIGGER trg_session_user_message_output_receipt__guard",
		"session_user_message_output_projection",
		"execution_fence",
		"recovery_event_id",
		"terminal_event_id",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("Up Migration 缺少契约 %s", required)
		}
	}
	if strings.Contains(strings.ToUpper(upSQL), "FOREIGN KEY") || strings.Contains(upSQL, "REFERENCES agent.") {
		t.Fatal("user_message runtime Migration 禁止物理外键")
	}
	for _, required := range []string{
		"user message runtime preview contains durable data; rollback is unsafe",
		"DROP TABLE IF EXISTS agent.session_user_message_output_projection",
		"DROP TABLE IF EXISTS agent.session_user_message_turn",
		"DROP FUNCTION IF EXISTS agent.guard_user_message_output_receipt_mutation()",
		"DROP FUNCTION IF EXISTS agent.guard_user_message_model_receipt_mutation()",
		"DROP FUNCTION IF EXISTS agent.reject_user_message_context_mutation()",
	} {
		if !strings.Contains(string(down), required) {
			t.Fatalf("Down Migration 缺少保护或清理 %s", required)
		}
	}
	for _, required := range []string{
		"upgrade_generation bigint NOT NULL DEFAULT 1",
		"version bigint NOT NULL DEFAULT 1",
		"idx_session_user_message_upgrade_ledger__stage_input",
		"trg_session_user_message_upgrade_ledger__guard",
		"OLD.stage = 'prepared' AND NEW.stage = 'applied'",
		"OLD.stage = 'applied' AND NEW.stage = 'verified'",
		"trg_session_command_receipt__immutable",
		"trg_session_message__immutable",
	} {
		if !strings.Contains(string(guardUp), required) {
			t.Fatalf("009 Up Migration 缺少契约 %s", required)
		}
	}
	for _, required := range []string{
		"user message upgrade ledger contains durable data; rollback is unsafe",
		"DROP TRIGGER IF EXISTS trg_session_command_receipt__immutable",
		"DROP TRIGGER IF EXISTS trg_session_message__immutable",
		"DROP COLUMN IF EXISTS version",
		"DROP COLUMN IF EXISTS upgrade_generation",
	} {
		if !strings.Contains(string(guardDown), required) {
			t.Fatalf("009 Down Migration 缺少保护或清理 %s", required)
		}
	}
}

// TestUserMessageLegacyUpgradeGuardsPostgreSQL 证明 Ledger 只能单向推进，Receipt 和 Ledger identity 均不可漂移。
func TestUserMessageLegacyUpgradeGuardsPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(userMessageRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL Legacy Guard 探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("连接 Legacy Guard PostgreSQL 失败: %v", err)
	}
	defer client.Close()
	outer := client.db.WithContext(ctx).Begin()
	if outer.Error != nil {
		t.Fatal(outer.Error)
	}
	defer outer.Rollback()
	ids := idgen.UUIDv7{}
	inputID := mustUserMessageRuntimeIntegrationID(t, ids)
	sessionID := mustUserMessageRuntimeIntegrationID(t, ids)
	turnID := mustUserMessageRuntimeIntegrationID(t, ids)
	if result := outer.Exec(`INSERT INTO agent.session_user_message_upgrade_ledger
		(input_id,session_id,stage,turn_id,context_digest,upgrade_generation,version,created_at,updated_at)
		VALUES (?,?,'prepared',?,?,1,1,clock_timestamp(),clock_timestamp())`, inputID, sessionID, turnID, strings.Repeat("a", 64)); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 prepared Ledger 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_upgrade_ledger SET stage='verified',version=2,updated_at=clock_timestamp()+interval '1 second' WHERE input_id=?`, inputID)
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_upgrade_ledger SET turn_id=?,stage='applied',version=2,updated_at=clock_timestamp()+interval '1 second' WHERE input_id=?`, mustUserMessageRuntimeIntegrationID(t, ids), inputID)
	if result := outer.Exec(`UPDATE agent.session_user_message_upgrade_ledger
		SET stage='applied',version=2,updated_at=clock_timestamp()+interval '1 second' WHERE input_id=?`, inputID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("prepared->applied 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if result := outer.Exec(`UPDATE agent.session_user_message_upgrade_ledger
		SET stage='verified',version=3,updated_at=clock_timestamp()+interval '2 seconds' WHERE input_id=?`, inputID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("applied->verified 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`DELETE FROM agent.session_user_message_upgrade_ledger WHERE input_id=?`, inputID)

	commandID := mustUserMessageRuntimeIntegrationID(t, ids)
	if result := outer.Exec(`INSERT INTO agent.session_command_receipt
		(command_id,command_type,request_digest,session_id,message_id,input_id,result_version,skill_snapshot_digest,skill_count,completed_at)
		VALUES (?,'ensure_project_session_v1',?,?,NULL,NULL,1,?,0,clock_timestamp())`,
		commandID, strings.Repeat("b", 64), sessionID, session.EmptySkillSnapshotDigest); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 Receipt Guard fixture 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_command_receipt SET request_digest=? WHERE command_id=?`, strings.Repeat("c", 64), commandID)

	messageID := mustUserMessageRuntimeIntegrationID(t, ids)
	envelope, err := session.BuildEnvelopeV1(session.EnvelopeAlgorithmAES256GCM, make([]byte, 12), make([]byte, 17))
	if err != nil {
		t.Fatal(err)
	}
	if result := outer.Exec(`INSERT INTO agent.session_message
		(id,session_id,message_seq,role,content_ciphertext,content_key_version,content_digest,source_kind,source_id,created_at)
		VALUES (?,?,1,'user',?,'legacy-guard-v1',?,'ensure_project_session',?,clock_timestamp())`,
		messageID, sessionID, envelope, strings.Repeat("d", 64), commandID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 Message Guard fixture 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_message SET content_digest=? WHERE id=?`, strings.Repeat("e", 64), messageID)
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`DELETE FROM agent.session_message WHERE id=?`, messageID)
}

// TestUserMessageRuntimeReceiptGuardsPostgreSQL 在真实 PostgreSQL 上证明首写身份不可改，且只允许一次合法终态转换。
func TestUserMessageRuntimeReceiptGuardsPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(userMessageRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL Receipt Guard 探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Receipt Guard PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 Receipt Guard PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifyUserMessageRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("User Message Runtime Schema 未就绪: %v", err)
	}

	outer := client.db.WithContext(ctx).Begin()
	if outer.Error != nil {
		t.Fatalf("开启 Receipt Guard 外层事务失败: %v", outer.Error)
	}
	defer func() {
		if rollbackErr := outer.Rollback().Error; rollbackErr != nil {
			t.Errorf("回滚 Receipt Guard 外层事务失败: %v", rollbackErr)
		}
	}()

	ids := idgen.UUIDv7{}
	modelCallID := mustUserMessageRuntimeIntegrationID(t, ids)
	modelRunID := mustUserMessageRuntimeIntegrationID(t, ids)
	modelTurnID := mustUserMessageRuntimeIntegrationID(t, ids)
	modelInputID := mustUserMessageRuntimeIntegrationID(t, ids)
	requestDigest := strings.Repeat("a", 64)
	if result := outer.Exec(`
		INSERT INTO agent.session_user_message_model_receipt
		    (model_call_id, run_id, turn_id, input_id, request_digest, execution_fence, status, created_at)
		VALUES (?, ?, ?, ?, ?, 7, 'reserved', clock_timestamp())`,
		modelCallID, modelRunID, modelTurnID, modelInputID, requestDigest); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 reserved Model Receipt 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_model_receipt SET request_digest = ? WHERE model_call_id = ?`,
		strings.Repeat("b", 64), modelCallID)
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_model_receipt SET run_id = ? WHERE model_call_id = ?`,
		mustUserMessageRuntimeIntegrationID(t, ids), modelCallID)
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_model_receipt SET execution_fence = 7 WHERE model_call_id = ?`,
		modelCallID)
	if result := outer.Exec(`
		UPDATE agent.session_user_message_model_receipt
		SET execution_fence = 8
		WHERE model_call_id = ? AND status = 'reserved' AND execution_fence = 7`,
		modelCallID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("合法推进 Model Receipt execution_fence 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if result := outer.Exec(`
		UPDATE agent.session_user_message_model_receipt
		SET status = 'completed', response_ciphertext = ?, response_key_version = ?,
		    response_digest = ?, completed_at = clock_timestamp()
		WHERE model_call_id = ?`,
		[]byte{0x01}, "receipt-guard-v1", strings.Repeat("c", 64), modelCallID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("合法冻结 Model Receipt 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_model_receipt SET response_digest = ? WHERE model_call_id = ?`,
		strings.Repeat("d", 64), modelCallID)

	outputID := mustUserMessageRuntimeIntegrationID(t, ids)
	outputRunID := mustUserMessageRuntimeIntegrationID(t, ids)
	outputTurnID := mustUserMessageRuntimeIntegrationID(t, ids)
	outputInputID := mustUserMessageRuntimeIntegrationID(t, ids)
	projectionKey := "session:" + mustUserMessageRuntimeIntegrationID(t, ids) + ":turn:" + outputTurnID
	if result := outer.Exec(`
		INSERT INTO agent.session_user_message_output_receipt
		    (output_id, run_id, turn_id, input_id, projection_key, schema_version, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'session.turn.direct_response.card.v1', 'open', clock_timestamp())`,
		outputID, outputRunID, outputTurnID, outputInputID, projectionKey); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 open Output Receipt 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_output_receipt SET projection_key = ? WHERE output_id = ?`,
		projectionKey+":other", outputID)
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_output_receipt SET schema_version = 'session.turn.failure.card.v1' WHERE output_id = ?`,
		outputID)
	if result := outer.Exec(`
		UPDATE agent.session_user_message_output_receipt
		SET status = 'failed', schema_version = 'session.turn.failure.card.v1',
		    result_ciphertext = ?, result_key_version = ?, result_digest = ?,
		    error_code = 'USER_MESSAGE_RUNTIME_PROCESSING_FAILED', completed_at = clock_timestamp()
		WHERE output_id = ?`,
		[]byte{0x02}, "receipt-guard-v1", strings.Repeat("e", 64), outputID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("合法冻结 Output Receipt 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	expectUserMessageRuntimeReceiptMutationRejected(t, outer,
		`UPDATE agent.session_user_message_output_receipt SET result_digest = ? WHERE output_id = ?`,
		strings.Repeat("f", 64), outputID)
}

var errUserMessageRuntimeReceiptMutationUnexpectedlyAccepted = errors.New("receipt mutation unexpectedly accepted")

func expectUserMessageRuntimeReceiptMutationRejected(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	err := db.Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(query, args...)
		if result.Error == nil {
			return errUserMessageRuntimeReceiptMutationUnexpectedlyAccepted
		}
		return result.Error
	})
	if errors.Is(err, errUserMessageRuntimeReceiptMutationUnexpectedlyAccepted) {
		t.Fatalf("Receipt Guard 接受了非法 UPDATE: %s", query)
	}
	if err == nil {
		t.Fatalf("Receipt Guard 非法 UPDATE 未返回错误: %s", query)
	}
}
