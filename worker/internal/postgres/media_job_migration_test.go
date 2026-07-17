package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestMediaJobMigrationContract 静态锁定 Worker Owner、共享恢复摘要、中文注释和无物理外键口径。
func TestMediaJobMigrationContract(t *testing.T) {
	t.Parallel()
	upBytes, err := os.ReadFile("../../migrations/20260717000100_create_media_preview_runtime_receipts.up.sql")
	if err != nil {
		t.Fatalf("读取 Worker media runtime Up Migration 失败: %v", err)
	}
	downBytes, err := os.ReadFile("../../migrations/20260717000100_create_media_preview_runtime_receipts.down.sql")
	if err != nil {
		t.Fatalf("读取 Worker media runtime Down Migration 失败: %v", err)
	}
	upSQL, downSQL := string(upBytes), string(downBytes)
	for _, fragment := range []string{
		"CREATE TABLE worker.media_preview_attempts",
		"CREATE TABLE worker.media_preview_artifact_receipts",
		"CREATE TABLE worker.media_preview_finalization_observations",
		"claim_request_id uuid NOT NULL UNIQUE",
		"attempt_id uuid NOT NULL UNIQUE",
		"CONSTRAINT media_preview_finalization_identity_unique UNIQUE (command_id, request_digest)",
		"'claim_pending', 'claim_unknown', 'running', 'artifact_ready', 'finalize_unknown'",
		"'reconciling', 'terminal_unknown', 'retry_scheduled', 'completed', 'failed'",
		"COMMENT ON TABLE worker.media_preview_attempts IS",
		"COMMENT ON TABLE worker.media_preview_artifact_receipts IS",
		"COMMENT ON TABLE worker.media_preview_finalization_observations IS",
		"COMMENT ON COLUMN worker.media_preview_attempts.terminal_result_digest IS",
		"COMMENT ON COLUMN worker.media_preview_attempts.finalize_error_code IS",
		"COMMENT ON COLUMN worker.media_preview_artifact_receipts.content_digest IS",
		"COMMENT ON COLUMN worker.media_preview_finalization_observations.media_kind IS",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("Worker media runtime Up Migration 缺少契约片段 %q", fragment)
		}
	}
	upperUp := strings.ToUpper(upSQL)
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, "REFERENCES ") ||
		strings.Contains(upperUp, "ON DELETE CASCADE") || strings.Contains(upperUp, "ON UPDATE CASCADE") {
		t.Fatal("Worker media runtime Migration 禁止创建物理外键或数据库级联")
	}
	if strings.Contains(strings.ToLower(upSQL), "object_key") || strings.Contains(strings.ToLower(upSQL), "object_root") {
		t.Fatal("Worker 恢复表禁止持久化 Object Key 或对象根")
	}
	for _, fragment := range []string{
		"DROP TABLE IF EXISTS worker.media_preview_finalization_observations",
		"DROP TABLE IF EXISTS worker.media_preview_artifact_receipts",
		"DROP TABLE IF EXISTS worker.media_preview_attempts",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("Worker media runtime Down Migration 缺少契约片段 %q", fragment)
		}
	}
}
