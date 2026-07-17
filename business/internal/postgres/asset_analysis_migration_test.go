package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAssetAnalysisPreviewMigrationContract 验证两表、中文表列注释、无物理外键与无生产 Seed。
func TestAssetAnalysisPreviewMigrationContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve AssetAnalysis migration test path")
	}
	migrationPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "20260716000200_create_asset_analysis_preview.up.sql")
	contentBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read AssetAnalysis migration: %v", err)
	}
	content := string(contentBytes)
	upper := strings.ToUpper(content)
	for _, prohibited := range []string{"FOREIGN KEY", " REFERENCES ", " ON DELETE ", "INSERT INTO"} {
		if strings.Contains(upper, prohibited) {
			t.Fatalf("AssetAnalysis migration contains prohibited clause %q", prohibited)
		}
	}
	expected := map[string][]string{
		"asset_analysis_preview_assets": {
			"id", "owner_user_id", "project_id", "asset_version", "media_type", "status", "created_at",
		},
		"asset_analysis_preview_evidence": {
			"id", "asset_id", "asset_version", "media_type", "evidence_kind", "availability", "reason_code",
			"content_digest", "extractor_schema_version", "extractor_version", "locator_kind", "text_start", "text_end",
			"text_source_length", "image_x", "image_y", "image_width", "image_height", "content", "created_at",
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
	if !strings.Contains(content, "trg_asset_analysis_preview_evidence_immutable") {
		t.Fatal("immutable Evidence trigger is missing")
	}
}
