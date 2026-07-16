package postgres

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

// TestProjectSkillBindingMigrationStaticContract 静态验证新迁移不修改已发布文件、不含物理外键且所有新表/列有中文 COMMENT。
func TestProjectSkillBindingMigrationStaticContract(t *testing.T) {
	upPath, _ := projectSkillBindingMigrationPaths(t)
	contentBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)
	upper := strings.ToUpper(content)
	for _, prohibited := range []string{"FOREIGN KEY", "REFERENCES ", " ON DELETE ", " ON UPDATE CASCADE"} {
		if strings.Contains(upper, prohibited) {
			t.Fatalf("Project Skill Binding migration contains prohibited database relation token %q", prohibited)
		}
	}
	expectedColumns := map[string][]string{
		"project_skill_binding_set": {
			"project_id", "owner_user_id", "schema_version", "set_version", "selection_digest", "enabled_count", "created_at", "updated_at",
		},
		"project_skill_binding": {
			"id", "project_id", "skill_id", "namespace", "priority", "status", "source", "enabled_by_user_id", "enabled_at",
			"disabled_by_user_id", "disabled_at", "version", "created_at", "updated_at",
		},
		"project_skill_binding_command_receipt": {
			"id", "actor_user_id", "project_id", "command_type", "key_digest", "semantic_digest", "result_set_version",
			"result_selection_digest", "result_enabled_count", "created_at",
		},
		"project_skill_binding_audit": {
			"id", "project_id", "binding_id", "skill_id", "binding_set_version", "action", "from_status", "to_status", "source",
			"actor_user_id", "command_receipt_id", "reason_code", "occurred_at",
		},
		"project_session_skill_resolution": {
			"id", "command_id", "project_id", "owner_user_id", "binding_set_version", "binding_selection_digest", "snapshot_schema_version",
			"snapshot_kind", "skill_count", "snapshot_set_digest", "runtime_policy_ref", "resolved_at",
		},
		"project_session_skill_resolution_item": {
			"resolution_id", "project_id", "command_id", "load_order", "priority", "namespace", "binding_id", "binding_version",
			"skill_id", "publisher_user_id", "published_snapshot_id", "publication_revision", "definition_schema_version", "content_digest",
			"runtime_content_schema_version", "runtime_content_digest", "allowed_graph_tool_keys", "public_tool_refs", "permission_snapshot_digest",
			"runtime_policy_ref", "governance_epoch", "published_at_unix_ms", "created_at",
		},
	}
	for table, columns := range expectedColumns {
		if !strings.Contains(content, "COMMENT ON TABLE business."+table+" IS '") {
			t.Fatalf("table %s missing Chinese COMMENT", table)
		}
		for _, column := range columns {
			if !strings.Contains(content, "COMMENT ON COLUMN business."+table+"."+column+" IS '") {
				t.Fatalf("column %s.%s missing Chinese COMMENT", table, column)
			}
		}
	}
	for table, columns := range map[string][]string{
		"skill":                    {"governance_epoch"},
		"project_creation_receipt": {"request_schema_version", "skill_snapshot_digest", "skill_count", "binding_set_version", "resolution_id"},
		"project_session_binding":  {"request_schema_version", "skill_snapshot_digest", "skill_count", "binding_set_version", "resolution_id"},
		"project_session_outbox":   {"skill_snapshot_digest", "skill_count", "binding_set_version", "resolution_id"},
	} {
		for _, column := range columns {
			if !strings.Contains(content, "COMMENT ON COLUMN business."+table+"."+column+" IS '") {
				t.Fatalf("expanded column %s.%s missing Chinese COMMENT", table, column)
			}
		}
	}
}

// TestProjectSkillBindingMigrationPostgreSQLContract 使用真实 PostgreSQL 验证中文 COMMENT、无外键、fail-safe Down 与 v1 delivered envelope 兼容。
func TestProjectSkillBindingMigrationPostgreSQLContract(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	var foreignKeyCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_constraint constraint_record
		JOIN pg_namespace namespace_record ON namespace_record.oid = constraint_record.connamespace
		WHERE namespace_record.nspname = 'business' AND constraint_record.contype = 'f'`).Scan(&foreignKeyCount).Error; err != nil {
		t.Fatal(err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("business schema contains %d physical foreign keys", foreignKeyCount)
	}
	var missingCommentCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_class table_record
		JOIN pg_namespace namespace_record ON namespace_record.oid = table_record.relnamespace
		JOIN pg_attribute column_record ON column_record.attrelid = table_record.oid
		WHERE namespace_record.nspname = 'business'
		  AND table_record.relkind = 'r'
		  AND column_record.attnum > 0
		  AND NOT column_record.attisdropped
		  AND (
			obj_description(table_record.oid, 'pg_class') IS NULL OR
			col_description(table_record.oid, column_record.attnum) IS NULL OR
			col_description(table_record.oid, column_record.attnum) !~ '[一-龥]'
		  )`).Scan(&missingCommentCount).Error; err != nil {
		t.Fatal(err)
	}
	if missingCommentCount != 0 {
		t.Fatalf("business schema has %d table/column rows without Chinese comments", missingCommentCount)
	}

	// Expand Migration 必须保留 W0 的原约束语义：v1 delivered 行允许在本地清理事务前短暂保留原 Prompt 密文。
	v1 := newRepositoryTestAggregate(t)
	if _, err := repository.CreateQuick(context.Background(), v1); err != nil {
		t.Fatal(err)
	}
	deliveredAt := v1.Outbox.CreatedAt.Add(time.Minute)
	if err := db.Model(&projectSessionOutboxModel{}).Where("id = ?", v1.Outbox.ID).
		Updates(map[string]any{"status": string(project.OutboxStatusDelivered), "delivered_at": deliveredAt, "updated_at": deliveredAt}).Error; err != nil {
		t.Fatalf("v1 delivered row retaining ciphertext must remain valid after expand: %v", err)
	}
	var retained projectSessionOutboxModel
	if err := db.Where("id = ?", v1.Outbox.ID).Take(&retained).Error; err != nil || len(retained.PayloadCiphertext) == 0 {
		t.Fatalf("v1 delivered compatibility fixture lost ciphertext: outbox=%+v err=%v", retained, err)
	}

	// 任一 Binding/V2 事实存在时 Down 必须在第一个 DO block 失败，不能删除审计或伪装成 empty Snapshot。
	if err := db.Create(&projectSkillBindingSetModel{
		ProjectID: newRepositoryTestUUIDv7(t), OwnerUserID: newRepositoryTestUUIDv7(t),
		SchemaVersion: "project_skill_binding_set.v1", SetVersion: 1,
		SelectionDigest: make([]byte, 32), EnabledCount: 0, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, downPath := projectSkillBindingMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(downSQL)).Error; err == nil {
		t.Fatal("expected fail-safe down to reject existing binding facts")
	}
	var retainedBindingSetCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM business.project_skill_binding_set`).Scan(&retainedBindingSetCount).Error; err != nil || retainedBindingSetCount != 1 {
		t.Fatalf("failed down removed binding facts: count=%d err=%v", retainedBindingSetCount, err)
	}
}

// TestProjectSkillBindingMigrationDownWithoutV2Data 验证没有任何 v2/Binding 数据且治理纪元未变化时可安全恢复 W0 schema。
func TestProjectSkillBindingMigrationDownWithoutV2Data(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	_, downPath := projectSkillBindingMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(downSQL)).Error; err != nil {
		t.Fatalf("safe down without v2 facts failed: %v", err)
	}
	var tableExists bool
	if err := db.Raw(`SELECT to_regclass('business.project_skill_binding_set') IS NOT NULL`).Scan(&tableExists).Error; err != nil {
		t.Fatal(err)
	}
	if tableExists {
		t.Fatal("safe down retained project_skill_binding_set")
	}
	var governanceColumnCount int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'business' AND table_name = 'skill' AND column_name = 'governance_epoch'`).Scan(&governanceColumnCount).Error; err != nil {
		t.Fatal(err)
	}
	if governanceColumnCount != 0 {
		t.Fatal("safe down retained skill.governance_epoch")
	}
}

// projectSkillBindingMigrationPaths 返回本测试文件相对的固定 Up/Down 路径，不扫描或修改已发布 Migration。
func projectSkillBindingMigrationPaths(t *testing.T) (string, string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Project Skill Binding migration test path")
	}
	directory := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	return filepath.Join(directory, "20260714000500_create_project_skill_binding_producer.up.sql"),
		filepath.Join(directory, "20260714000500_create_project_skill_binding_producer.down.sql")
}
