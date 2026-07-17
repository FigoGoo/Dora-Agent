package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMediaPreviewMigrationContract 验证三张 Preview 表、中文 COMMENT、约束、索引和无物理外键门禁。
func TestMediaPreviewMigrationContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Media Preview migration path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "20260717000500_create_media_preview_asset.up.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Media Preview migration: %v", err)
	}
	content := string(raw)
	upper := strings.ToUpper(content)
	for _, prohibited := range []string{"FOREIGN KEY", " REFERENCES ", " ON DELETE ", " ON UPDATE CASCADE", "INSERT INTO"} {
		if strings.Contains(upper, prohibited) {
			t.Fatalf("Media Preview migration contains prohibited clause %q", prohibited)
		}
	}
	expected := map[string][]string{
		"media_preview_asset": {
			"id", "owner_user_id", "project_id", "asset_version", "status", "media_kind", "mime_type",
			"output_profile", "source_type", "source_id", "source_version", "source_digest", "target_local_key",
			"target_digest", "object_key", "content_digest", "size_bytes", "width", "height", "duration_ms",
			"codec", "pixel_format", "finalized_job_id", "finalized_attempt_id", "finalized_fence", "error_code",
			"created_at", "finalized_at",
		},
		"media_preview_preparation_receipt": {
			"id", "request_id", "command_id", "request_digest", "operation_id", "owner_user_id", "project_id",
			"tool_key", "scope_digest", "output_profile", "source_type", "source_id", "source_version",
			"source_digest", "target_local_key", "target_digest", "source_object_key", "asset_id", "asset_version",
			"asset_status", "media_kind", "mime_type", "staging_object_key", "final_object_key", "created_at",
		},
		"media_preview_finalization_receipt": {
			"id", "request_id", "command_id", "request_digest", "preparation_id", "operation_id", "batch_id",
			"job_id", "attempt_id", "fence", "terminal_status", "asset_id", "asset_version", "asset_status",
			"media_kind", "mime_type", "content_digest", "size_bytes", "width", "height", "duration_ms", "codec",
			"pixel_format", "error_code", "completed_at",
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
	for _, required := range []string{
		"media_preview_preparation_command_unique", "media_preview_preparation_operation_unique",
		"media_preview_finalization_command_unique", "media_preview_finalization_preparation_unique",
		"media_preview_finalization_job_unique", "finalized_fence > 0", "fence > 0",
		"source_type = 'prompt_preview'", "source_type = 'image_asset'",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("Media Preview migration is missing contract fragment %q", required)
		}
	}
	if strings.Contains(content, "source_type = 'media_preview_asset'") {
		t.Fatal("Media Preview migration contains prohibited source_type alias")
	}
}
