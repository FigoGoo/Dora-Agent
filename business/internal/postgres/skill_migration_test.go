package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestW1SkillMigrationHasCommentsNoForeignKeysAndNoUnfrozenTables(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve skill migration test path")
	}
	migrationPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "20260714000400_create_w1_skill_foundation.up.sql")
	contentBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read W1 Skill migration: %v", err)
	}
	content := string(contentBytes)
	upper := strings.ToUpper(content)
	if strings.Contains(upper, "FOREIGN KEY") || strings.Contains(upper, " ON DELETE ") || strings.Contains(upper, " ON UPDATE CASCADE") {
		t.Fatal("W1 Skill migration contains a prohibited physical foreign key or database cascade")
	}
	for _, forbiddenTable := range []string{"public_tool_definition", "public_tool_version", "project_skill_binding"} {
		if strings.Contains(content, "CREATE TABLE business."+forbiddenTable) {
			t.Fatalf("W1 Skill migration pre-created unfrozen table %s", forbiddenTable)
		}
	}
	expectedColumns := map[string][]string{
		"skill": {
			"id", "owner_user_id", "current_draft_revision_id", "current_published_snapshot_id",
			"publication_revision", "governance_status", "version", "created_at", "updated_at",
		},
		"skill_content_revision": {
			"id", "skill_id", "revision_no", "definition_schema_version", "definition_json",
			"content_digest", "created_by_user_id", "created_at",
		},
		"skill_review_submission": {
			"id", "skill_id", "content_revision_id", "content_digest", "status", "safe_reason_code",
			"version", "submitted_by_user_id", "decided_by_user_id", "submitted_at", "decided_at", "updated_at",
		},
		"skill_published_snapshot": {
			"id", "skill_id", "source_content_revision_id", "review_submission_id", "publication_revision",
			"definition_schema_version", "definition_json", "content_digest", "published_by_user_id", "published_at",
		},
		"skill_command_receipt": {
			"id", "actor_user_id", "command_type", "scope_id", "key_digest", "semantic_digest",
			"result_skill_id", "result_content_revision_id", "result_review_submission_id", "result_published_snapshot_id",
			"response_draft_revision_id", "response_published_snapshot_id", "response_review_submission_id",
			"response_review_status", "response_review_reason_code", "response_review_updated_at",
			"response_governance_status", "created_at",
		},
		"skill_governance_audit": {
			"id", "skill_id", "review_submission_id", "action", "from_status", "to_status",
			"safe_reason_code", "actor_user_id", "occurred_at",
		},
	}
	for table, columns := range expectedColumns {
		if !strings.Contains(content, "COMMENT ON TABLE business."+table+" IS '") {
			t.Fatalf("table %s is missing Chinese COMMENT", table)
		}
		for _, column := range columns {
			marker := "COMMENT ON COLUMN business." + table + "." + column + " IS '"
			if !strings.Contains(content, marker) {
				t.Fatalf("column %s.%s is missing Chinese COMMENT", table, column)
			}
		}
	}
}
