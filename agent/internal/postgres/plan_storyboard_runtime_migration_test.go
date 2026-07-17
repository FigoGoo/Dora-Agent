package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestPlanStoryboardRuntimeMigrationContract 静态锁定 M2 隔离表、全 Source 扩展、prepared 恢复与安全回滚口径。
func TestPlanStoryboardRuntimeMigrationContract(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("../../migrations/20260717001100_add_plan_storyboard_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 plan storyboard runtime Up Migration 失败: %v", err)
	}
	down, err := os.ReadFile("../../migrations/20260717001100_add_plan_storyboard_runtime_v2preview1.down.sql")
	if err != nil {
		t.Fatalf("读取 plan storyboard runtime Down Migration 失败: %v", err)
	}
	upSQL, downSQL := string(up), string(down)
	for _, fragment := range []string{
		"status IN ('pending', 'claimed', 'running', 'retry_wait', 'recovery_pending', 'resolved', 'dead')",
		"source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview', 'plan_storyboard_preview')",
		"'plan_storyboard.preview.accepted'",
		"'plan_storyboard.preview.completed'",
		"'plan_storyboard.preview.failed'",
		"'plan_storyboard.preview.runtime_failed'",
		"CREATE TABLE agent.plan_storyboard_preview_run",
		"business_command_id uuid NOT NULL",
		"UNIQUE (session_id, idempotency_key)",
		"CREATE TABLE agent.plan_storyboard_preview_turn_context",
		"profile = 'plan_storyboard.runtime.v2preview1'",
		"schema_version = 'plan_storyboard.turn_context.v2preview1'",
		"creation_spec_id uuid NOT NULL",
		"creation_spec_version bigint NOT NULL",
		"creation_spec_content_digest char(64) NOT NULL",
		"candidate_schema_ref varchar(128) NOT NULL",
		"dag_validator_digest char(64) NOT NULL",
		"planning_model_route_digest char(64) NOT NULL",
		"CREATE TABLE agent.plan_storyboard_preview_model_receipt",
		"call_kind IN ('router', 'graph_planning')",
		"CREATE TABLE agent.plan_storyboard_preview_tool_receipt",
		"status IN ('open', 'business_prepared', 'business_unknown', 'completed', 'failed')",
		"command_ciphertext bytea NULL",
		"expected_project_version bigint NULL",
		"business_request_digest char(64) NULL",
		"resend_attempts integer NOT NULL DEFAULT 0",
		"resend_limit integer NOT NULL DEFAULT 0",
		"trg_plan_storyboard_preview_turn_context__immutable",
		"trg_plan_storyboard_preview_model_receipt__guard",
		"trg_plan_storyboard_preview_tool_receipt__guard",
		"COMMENT ON TABLE agent.plan_storyboard_preview_run IS",
		"COMMENT ON COLUMN agent.plan_storyboard_preview_tool_receipt.command_ciphertext IS",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("Up Migration 缺少契约片段 %q", fragment)
		}
	}
	upperUp := strings.ToUpper(upSQL)
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, "REFERENCES ") {
		t.Fatal("plan storyboard runtime Migration 禁止创建物理外键")
	}
	if strings.Contains(upSQL, "CREATE TABLE agent.session_message") || strings.Contains(upSQL, "ALTER TABLE agent.session_message") {
		t.Fatal("typed plan storyboard intent 不得创建或修改 session_message")
	}
	for _, fragment := range []string{
		"plan storyboard runtime preview contains durable data; rollback is unsafe",
		"WHERE source_type = 'plan_storyboard_preview'",
		"WHERE event_type IN (",
		"DROP TABLE IF EXISTS agent.plan_storyboard_preview_tool_receipt",
		"DROP TABLE IF EXISTS agent.plan_storyboard_preview_turn_context",
		"source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview')",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("Down Migration 缺少安全片段 %q", fragment)
		}
	}
}
