package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPromptPreviewMigrationContract 验证隔离 Draft/Receipt 表、中文 COMMENT、状态约束和无物理外键门禁。
func TestPromptPreviewMigrationContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Prompt Preview migration path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "20260717000400_create_prompt_preview_draft.up.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Prompt Preview migration: %v", err)
	}
	content := string(raw)
	upper := strings.ToUpper(content)
	for _, prohibited := range []string{"FOREIGN KEY", " REFERENCES ", " ON DELETE ", " ON UPDATE CASCADE"} {
		if strings.Contains(upper, prohibited) {
			t.Fatalf("Prompt Preview migration contains prohibited physical relationship clause %q", prohibited)
		}
	}
	expected := map[string][]string{
		"prompt_preview_draft": {
			"id", "project_id", "user_id", "storyboard_preview_id", "storyboard_preview_version",
			"storyboard_preview_content_digest", "status", "version", "schema_version", "content_json",
			"content_digest", "exact_target_set_digest", "source_tool_call_id", "source_prompt_version",
			"source_validator_version", "source_exact_set_validator_version", "created_at", "updated_at",
		},
		"prompt_preview_command_receipt": {
			"id", "command_id", "request_digest", "user_id", "project_id", "expected_project_version",
			"storyboard_preview_id", "expected_storyboard_preview_version", "expected_storyboard_preview_content_digest",
			"source_tool_call_id", "source_prompt_version", "source_validator_version",
			"source_exact_set_validator_version", "exact_target_set_digest", "prompt_preview_id",
			"result_version", "result_status", "result_content_digest", "created_at",
		},
	}
	for table, columns := range expected {
		if !strings.Contains(content, "CREATE TABLE business."+table+" (") ||
			!strings.Contains(content, "COMMENT ON TABLE business."+table+" IS '") {
			t.Fatalf("table %s is missing DDL or Chinese COMMENT", table)
		}
		for _, column := range columns {
			if !strings.Contains(content, "COMMENT ON COLUMN business."+table+"."+column+" IS '") {
				t.Fatalf("column %s.%s is missing Chinese COMMENT", table, column)
			}
		}
	}
	if !strings.Contains(content, "CHECK (status = 'draft')") || strings.Contains(content, "'reviewing'") ||
		strings.Contains(content, "'ready'") || strings.Contains(content, "'active'") {
		t.Fatal("Prompt Preview migration does not preserve draft-only lifecycle")
	}
}
