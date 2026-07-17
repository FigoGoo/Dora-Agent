package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestAnalyzeMaterialsRuntimeMigrationContract 静态锁定 M1 隔离表、无 Message 输入、Guard 与安全回滚口径。
func TestAnalyzeMaterialsRuntimeMigrationContract(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("../../migrations/20260717001000_add_analyze_materials_runtime_v2preview1.up.sql")
	if err != nil {
		t.Fatalf("read analyze materials runtime up migration: %v", err)
	}
	down, err := os.ReadFile("../../migrations/20260717001000_add_analyze_materials_runtime_v2preview1.down.sql")
	if err != nil {
		t.Fatalf("read analyze materials runtime down migration: %v", err)
	}
	upSQL, downSQL := string(up), string(down)
	for _, fragment := range []string{
		"source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview')",
		"CREATE TABLE agent.analyze_materials_preview_run",
		"request_id uuid NOT NULL",
		"UNIQUE (session_id, idempotency_key)",
		"CREATE TABLE agent.analyze_materials_preview_turn_context",
		"CREATE TABLE agent.analyze_materials_preview_model_receipt",
		"CREATE TABLE agent.analyze_materials_preview_tool_receipt",
		"CREATE TABLE agent.analyze_materials_preview_projection",
		"profile = 'analyze_materials.runtime.v2preview1'",
		"schema_version = 'analyze_materials.turn_context.v2preview1'",
		"call_kind IN ('router', 'graph_analysis')",
		"status IN ('open', 'completed', 'partial', 'failed')",
		"'tool_completed', 'tool_partial', 'tool_failed', 'runtime_failed'",
		"trg_analyze_materials_preview_turn_context__immutable",
		"trg_analyze_materials_preview_model_receipt__guard",
		"trg_analyze_materials_preview_tool_receipt__guard",
		"trg_analyze_materials_preview_projection__immutable",
		"COMMENT ON TABLE agent.analyze_materials_preview_run IS",
		"COMMENT ON COLUMN agent.analyze_materials_preview_projection.payload IS",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("up migration missing contract fragment %q", fragment)
		}
	}
	if strings.Contains(strings.ToUpper(upSQL), "FOREIGN KEY") || strings.Contains(strings.ToUpper(upSQL), "REFERENCES ") {
		t.Fatal("analyze materials runtime migration must not create physical foreign keys")
	}
	if strings.Contains(upSQL, "CREATE TABLE agent.session_message") || strings.Contains(upSQL, "ALTER TABLE agent.session_message") {
		t.Fatal("typed analyze materials intent must not create or alter session_message")
	}
	for _, fragment := range []string{
		"analyze materials runtime preview contains durable data; rollback is unsafe",
		"WHERE source_type = 'analyze_materials_preview'",
		"WHERE event_type IN (",
		"DROP TABLE IF EXISTS agent.analyze_materials_preview_turn_context",
		"source_type IN ('user_message', 'creation_spec_preview')",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("down migration missing safety fragment %q", fragment)
		}
	}
}

// TestRequiredAnalyzeMaterialsRuntimeTablesStable 确保启动 Verify 与 Migration 表集保持精确一致。
func TestRequiredAnalyzeMaterialsRuntimeTablesStable(t *testing.T) {
	t.Parallel()
	want := []string{
		"analyze_materials_preview_model_receipt",
		"analyze_materials_preview_projection",
		"analyze_materials_preview_run",
		"analyze_materials_preview_tool_receipt",
		"analyze_materials_preview_turn_context",
	}
	got := requiredAnalyzeMaterialsRuntimeTables()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("required analyze materials tables mismatch: got %v want %v", got, want)
	}
}
