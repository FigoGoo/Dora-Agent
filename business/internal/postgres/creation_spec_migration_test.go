package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCreationSpecPreviewMigrationContract 验证 V1 Draft/Receipt 表、中文 COMMENT 与无物理外键门禁。
func TestCreationSpecPreviewMigrationContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve CreationSpec migration test path")
	}
	migrationPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "20260716000100_create_creation_spec_preview.up.sql")
	contentBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read CreationSpec migration: %v", err)
	}
	content := string(contentBytes)
	upper := strings.ToUpper(content)
	for _, prohibited := range []string{"FOREIGN KEY", " REFERENCES ", " ON DELETE ", " ON UPDATE CASCADE"} {
		if strings.Contains(upper, prohibited) {
			t.Fatalf("CreationSpec migration contains prohibited physical relationship clause %q", prohibited)
		}
	}
	expected := map[string][]string{
		"creation_spec": {
			"id", "project_id", "user_id", "status", "version", "schema_version", "content_json",
			"content_digest", "source_tool_call_id", "source_prompt_version", "source_validator_version", "created_at", "updated_at",
		},
		"creation_spec_command_receipt": {
			"id", "command_id", "request_digest", "user_id", "project_id", "expected_project_version",
			"source_tool_call_id", "source_prompt_version", "source_validator_version", "creation_spec_id",
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
}
