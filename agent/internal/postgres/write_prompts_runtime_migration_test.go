package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestWritePromptsRuntimeMigrationContract 静态锁定 M3 隔离表、全 Source 扩展、分层回执与安全回滚口径。
func TestWritePromptsRuntimeMigrationContract(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("../../migrations/20260717001200_add_write_prompts_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("读取 write prompts runtime Up Migration 失败: %v", err)
	}
	down, err := os.ReadFile("../../migrations/20260717001200_add_write_prompts_runtime_v2preview1.down.sql")
	if err != nil {
		t.Fatalf("读取 write prompts runtime Down Migration 失败: %v", err)
	}
	upSQL, downSQL := string(up), string(down)
	for _, fragment := range []string{
		"source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview', 'plan_storyboard_preview', 'write_prompts_preview')",
		"'write_prompts.preview.accepted'",
		"'write_prompts.preview.completed'",
		"'write_prompts.preview.failed'",
		"'write_prompts.preview.runtime_failed'",
		"CREATE TABLE agent.write_prompts_preview_run",
		"business_command_id uuid NOT NULL",
		"UNIQUE (session_id, idempotency_key)",
		"CREATE TABLE agent.write_prompts_preview_turn_context",
		"profile = 'write_prompts.runtime.v2preview1'",
		"schema_version = 'write_prompts.turn_context.v2preview1'",
		"storyboard_preview_id uuid NOT NULL",
		"storyboard_preview_version bigint NOT NULL",
		"storyboard_preview_content_digest char(64) NOT NULL",
		"exact_set_validator_digest char(64) NOT NULL",
		"prompt_model_route_digest char(64) NOT NULL",
		"CREATE TABLE agent.write_prompts_preview_model_receipt",
		"call_kind IN ('router', 'graph_prompt')",
		"CREATE TABLE agent.write_prompts_preview_tool_receipt",
		"status IN ('open', 'business_prepared', 'business_unknown', 'completed', 'failed')",
		"command_ciphertext bytea NULL",
		"business_request_digest char(64) NULL",
		"resend_attempts integer NOT NULL DEFAULT 0",
		"resend_limit integer NOT NULL DEFAULT 0",
		"trg_write_prompts_preview_turn_context__immutable",
		"trg_write_prompts_preview_model_receipt__guard",
		"trg_write_prompts_preview_tool_receipt__guard",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("Up Migration 缺少契约片段 %q", fragment)
		}
	}
	upperUp := strings.ToUpper(upSQL)
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, "REFERENCES ") {
		t.Fatal("write prompts runtime Migration 禁止创建物理外键")
	}
	if strings.Contains(upSQL, "CREATE TABLE agent.session_message") || strings.Contains(upSQL, "ALTER TABLE agent.session_message") {
		t.Fatal("typed write prompts intent 不得创建或修改 session_message")
	}
	for _, fragment := range []string{
		"write prompts runtime preview contains durable data; rollback is unsafe",
		"WHERE source_type = 'write_prompts_preview'",
		"DROP TABLE IF EXISTS agent.write_prompts_preview_tool_receipt",
		"DROP TABLE IF EXISTS agent.write_prompts_preview_turn_context",
		"source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview', 'plan_storyboard_preview')",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("Down Migration 缺少安全片段 %q", fragment)
		}
	}
}
