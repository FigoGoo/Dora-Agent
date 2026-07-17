package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestMediaRuntimeMigrationContract 静态锁定 Agent Owner、Fence 函数、最小权限与安全回滚口径。
func TestMediaRuntimeMigrationContract(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("../../migrations/20260717001300_add_media_runtime_v3preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 media runtime Up Migration 失败: %v", err)
	}
	down, err := os.ReadFile("../../migrations/20260717001300_add_media_runtime_v3preview1.down.sql")
	if err != nil {
		t.Fatalf("读取 media runtime Down Migration 失败: %v", err)
	}
	upSQL, downSQL := string(up), string(down)
	for _, fragment := range []string{
		"'generate_media_preview_request'",
		"'assemble_output_preview_request'",
		"'media_job_preview_terminal'",
		"'media.preview.accepted'",
		"'media.preview.completed'",
		"'media.preview.failed'",
		"'media.preview.runtime_failed'",
		"CREATE FUNCTION agent.media_preview_v1_jsonb_object_key_count(p_value jsonb)",
		"agent.media_preview_v1_jsonb_object_key_count(source_ref)",
		"agent.media_preview_v1_jsonb_object_key_count(target)",
		"agent.media_preview_v1_jsonb_object_key_count(p_result)",
		"agent.media_preview_v1_jsonb_object_key_count(p_result->'asset_ref')",
		"REVOKE ALL ON FUNCTION agent.media_preview_v1_jsonb_object_key_count(jsonb) FROM PUBLIC",
		"CREATE TABLE agent.media_preview_operation",
		"CREATE TABLE agent.media_preview_batch",
		"CREATE TABLE agent.media_preview_job",
		"CREATE TABLE agent.media_preview_dispatch_outbox",
		"CREATE TABLE agent.media_preview_terminal_outbox",
		"CREATE VIEW agent.media_job_preview_v1_claimable",
		"job_record.lease_expires_at <= clock_timestamp()",
		"CREATE FUNCTION agent.media_job_preview_v1_claim(",
		"CREATE FUNCTION agent.media_job_preview_v1_renew(",
		"job_record.status IN ('running', 'reconciling')",
		"'renewed'::text",
		"CREATE FUNCTION agent.media_job_preview_v1_schedule_retry(",
		"CREATE FUNCTION agent.media_job_preview_v1_mark_reconciling(",
		"CREATE FUNCTION agent.media_job_preview_v1_commit_terminal(",
		"operation_record.planned_job_id = v_job.job_id",
		"v_job.job_type = 'generate_png' AND v_tool_key = 'generate_media'",
		"v_job.job_type = 'assemble_mp4' AND v_tool_key = 'assemble_output'",
		"p_result->'asset_ref'->'asset_id' = v_job.target->'asset_id'",
		"p_result->'asset_ref'->'version' = v_job.target->'asset_version'",
		"NOT COALESCE((",
		"p_result->'asset_ref'->>'media_kind' = 'image'",
		"p_result->'asset_ref'->>'mime_type' = 'image/png'",
		"p_result->'asset_ref'->>'media_kind' = 'video'",
		"p_result->'asset_ref'->>'mime_type' = 'video/mp4'",
		"CREATE FUNCTION agent.media_job_preview_v1_get(",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog, agent",
		"REVOKE ALL ON agent.media_job_preview_v1_claimable FROM PUBLIC",
		"GRANT SELECT ON agent.media_job_preview_v1_claimable TO dora_worker_app",
		"COMMENT ON TABLE agent.media_preview_operation IS",
		"COMMENT ON COLUMN agent.media_preview_request.intent_digest IS '规范化 Intent JSON 的 SHA-256 摘要'",
		"COMMENT ON COLUMN agent.media_preview_operation.preparation_command_id IS 'Business Prepare 首写生效命令的 UUIDv7'",
		"COMMENT ON COLUMN agent.media_preview_terminal_outbox.lane_input_id IS",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("Up Migration 缺少契约片段 %q", fragment)
		}
	}
	upperUp := strings.ToUpper(upSQL)
	if strings.Contains(upSQL, "jsonb_object_length") {
		t.Fatal("PostgreSQL 不提供 jsonb_object_length；对象键数量必须使用受锁定的 helper")
	}
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, "REFERENCES ") ||
		strings.Contains(upperUp, "ON DELETE CASCADE") {
		t.Fatal("media runtime Migration 禁止创建物理外键或数据库级联")
	}
	for _, fragment := range []string{
		"media runtime v3 preview contains durable data; rollback is unsafe",
		"DROP FUNCTION IF EXISTS agent.media_job_preview_v1_commit_terminal",
		"DROP VIEW IF EXISTS agent.media_job_preview_v1_claimable",
		"DROP TABLE IF EXISTS agent.media_preview_terminal_outbox",
		"DROP TABLE IF EXISTS agent.media_preview_operation",
		"DROP FUNCTION IF EXISTS agent.media_preview_v1_jsonb_object_key_count(jsonb)",
		"'media_job_preview_terminal'",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("Down Migration 缺少安全片段 %q", fragment)
		}
	}
}
