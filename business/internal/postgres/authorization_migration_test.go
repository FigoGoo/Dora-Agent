package postgres

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestReviewerRBACMigrationHasForwardOnlyContract(t *testing.T) {
	migrationPath, _ := reviewerRBACMigrationPaths(t)
	contentBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read Reviewer RBAC migration: %v", err)
	}
	content := string(contentBytes)
	upper := strings.ToUpper(content)
	if strings.Contains(upper, "FOREIGN KEY") || strings.Contains(upper, " REFERENCES ") ||
		strings.Contains(upper, " ON DELETE ") || strings.Contains(upper, " ON UPDATE CASCADE") {
		t.Fatal("Reviewer RBAC migration contains a prohibited physical foreign key or database cascade")
	}
	for _, fragment := range []string{
		"CREATE TABLE business.user_role_assignment",
		"revocation_approval_reference varchar(160) NULL",
		"CREATE UNIQUE INDEX uq_user_role_assignment__active_user_role",
		"CREATE TRIGGER trg_user_role_assignment__append_only",
		"ADD COLUMN request_id uuid NULL",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("Reviewer RBAC migration missing %q", fragment)
		}
	}
	columns := []string{
		"id", "user_id", "role_key", "status", "version", "assigned_by_user_id", "assignment_reason_code",
		"approval_reference", "assigned_at", "revoked_by_user_id", "revoke_reason_code",
		"revocation_approval_reference", "revoked_at", "updated_at",
	}
	if !strings.Contains(content, "COMMENT ON TABLE business.user_role_assignment IS '") {
		t.Fatal("user_role_assignment is missing Chinese table COMMENT")
	}
	for _, column := range columns {
		if !strings.Contains(content, "COMMENT ON COLUMN business.user_role_assignment."+column+" IS '") {
			t.Fatalf("user_role_assignment.%s is missing Chinese COMMENT", column)
		}
	}
	for _, marker := range []string{
		"COMMENT ON COLUMN business.skill_command_receipt.request_id IS '",
		"COMMENT ON COLUMN business.skill_governance_audit.request_id IS '",
	} {
		if !strings.Contains(content, marker) {
			t.Fatalf("forward request_id column is missing COMMENT: %s", marker)
		}
	}
}

// TestReviewerRBACMigrationPostgreSQLContract 使用真实 PostgreSQL 显式覆盖空库、005→006、重复初始化失败安全和 local Down。
func TestReviewerRBACMigrationPostgreSQLContract(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	upPath, downPath := reviewerRBACMigrationPaths(t)
	upSQL, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read Reviewer RBAC up migration: %v", err)
	}
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Reviewer RBAC down migration: %v", err)
	}

	// openBusinessIntegrationRepository 已从空 business Schema 顺序应用全部 Up Migration。
	assertReviewerRBACSchemaPresent(t, db)
	if err := db.Exec(string(downSQL)).Error; err != nil {
		t.Fatalf("apply local Reviewer RBAC down migration: %v", err)
	}
	assertReviewerRBACSchemaAbsent(t, db)

	// Down 后的 Schema 等价于 005；单独执行 006 必须完成前向升级并恢复 Ready。
	if err := db.Exec(string(upSQL)).Error; err != nil {
		t.Fatalf("upgrade Business schema from 005 to 006: %v", err)
	}
	assertReviewerRBACSchemaPresent(t, db)
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("005 to 006 schema is not ready: %v", err)
	}

	// 版本化 Migration 被误重复执行时必须失败，且不能损坏已经完成的 006 Schema。
	if err := db.Exec(string(upSQL)).Error; err == nil {
		t.Fatal("duplicate Reviewer RBAC migration initialization unexpectedly succeeded")
	}
	assertReviewerRBACSchemaPresent(t, db)
}

// TestReviewerRBACReadinessPostgreSQLRejectsOrphan 使用真实无外键 Schema 验证孤儿 assignment 阻止 Ready。
func TestReviewerRBACReadinessPostgreSQLRejectsOrphan(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO business.user_role_assignment (
			id, user_id, role_key, status, version, assigned_by_user_id,
			assignment_reason_code, approval_reference, assigned_at, updated_at
		) VALUES (?, ?, 'skill_reviewer', 'active', 1, ?, 'corruption_probe', 'TEST-ORPHAN', ?, ?)`,
		"019f0000-0000-7000-8000-000000000091",
		"019f0000-0000-7000-8000-000000000092",
		"019f0000-0000-7000-8000-000000000093", now, now,
	).Error; err != nil {
		t.Fatalf("seed orphan Reviewer assignment: %v", err)
	}
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err == nil || !strings.Contains(err.Error(), "orphan or unknown assignments") {
		t.Fatalf("orphan Reviewer assignment did not block readiness: %v", err)
	}
}

// TestReviewerRBACReadinessPostgreSQLRejectsUnknownRole 模拟约束损坏后验证未知角色仍在 Runtime 层失败关闭。
func TestReviewerRBACReadinessPostgreSQLRejectsUnknownRole(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for _, user := range []struct {
		id   string
		name string
	}{
		{"019f0000-0000-7000-8000-000000000094", "Corrupted Reviewer"},
		{"019f0000-0000-7000-8000-000000000095", "Corruption Actor"},
	} {
		if err := db.Exec(`
			INSERT INTO business.user_account (id, display_name, user_type, status, version, created_at, updated_at)
			VALUES (?, ?, 'personal', 'active', 1, ?, ?)`, user.id, user.name, now, now).Error; err != nil {
			t.Fatalf("seed unknown-role account: %v", err)
		}
	}
	if err := db.Exec(`ALTER TABLE business.user_role_assignment DROP CONSTRAINT ck_user_role_assignment__role`).Error; err != nil {
		t.Fatalf("simulate damaged Reviewer role constraint: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO business.user_role_assignment (
			id, user_id, role_key, status, version, assigned_by_user_id,
			assignment_reason_code, approval_reference, assigned_at, updated_at
		) VALUES (?, ?, 'unexpected_role', 'active', 1, ?, 'corruption_probe', 'TEST-UNKNOWN-ROLE', ?, ?)`,
		"019f0000-0000-7000-8000-000000000096",
		"019f0000-0000-7000-8000-000000000094",
		"019f0000-0000-7000-8000-000000000095", now, now,
	).Error; err != nil {
		t.Fatalf("seed unknown Reviewer role: %v", err)
	}
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err == nil || !strings.Contains(err.Error(), "orphan or unknown assignments") {
		t.Fatalf("unknown Reviewer role did not block readiness: %v", err)
	}
}

// assertReviewerRBACSchemaPresent 验证 006 的表、前向列、中文 COMMENT 与零物理外键契约。
func assertReviewerRBACSchemaPresent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var tableExists bool
	if err := db.Raw(`SELECT to_regclass('business.user_role_assignment') IS NOT NULL`).Scan(&tableExists).Error; err != nil || !tableExists {
		t.Fatalf("Reviewer role assignment table is missing: exists=%t err=%v", tableExists, err)
	}
	var requestColumnCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'business'
		  AND table_name IN ('skill_command_receipt', 'skill_governance_audit')
		  AND column_name = 'request_id'`).Scan(&requestColumnCount).Error; err != nil || requestColumnCount != 2 {
		t.Fatalf("Reviewer request_id columns are incomplete: count=%d err=%v", requestColumnCount, err)
	}
	var invalidContractCount int64
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*)
			 FROM pg_constraint constraint_record
			 JOIN pg_class relation ON relation.oid = constraint_record.conrelid
			 JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
			 WHERE namespace_record.nspname = 'business'
			   AND relation.relname = 'user_role_assignment'
			   AND constraint_record.contype = 'f') +
			(SELECT COUNT(*)
			 FROM pg_class relation
			 JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
			 JOIN pg_attribute attribute_record ON attribute_record.attrelid = relation.oid
			 WHERE namespace_record.nspname = 'business'
			   AND relation.relname = 'user_role_assignment'
			   AND attribute_record.attnum > 0
			   AND NOT attribute_record.attisdropped
			   AND (COALESCE(obj_description(relation.oid, 'pg_class'), '') !~ '[一-龥]'
			     OR COALESCE(col_description(relation.oid, attribute_record.attnum), '') !~ '[一-龥]'))`).Scan(&invalidContractCount).Error; err != nil || invalidContractCount != 0 {
		t.Fatalf("Reviewer RBAC schema violates foreign-key/comment contract: count=%d err=%v", invalidContractCount, err)
	}
}

// assertReviewerRBACSchemaAbsent 验证 destructive Down 只显式移除 006 新增对象。
func assertReviewerRBACSchemaAbsent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var remainingCount int64
	if err := db.Raw(`
		SELECT
			(CASE WHEN to_regclass('business.user_role_assignment') IS NULL THEN 0 ELSE 1 END) +
			(SELECT COUNT(*)
			 FROM information_schema.columns
			 WHERE table_schema = 'business'
			   AND table_name IN ('skill_command_receipt', 'skill_governance_audit')
			   AND column_name = 'request_id')`).Scan(&remainingCount).Error; err != nil || remainingCount != 0 {
		t.Fatalf("Reviewer RBAC down migration retained 006 objects: count=%d err=%v", remainingCount, err)
	}
}

// reviewerRBACMigrationPaths 返回本测试文件相对的固定 006 Up/Down 路径。
func reviewerRBACMigrationPaths(t *testing.T) (string, string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve authorization migration test path")
	}
	directory := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	return filepath.Join(directory, "20260714000600_create_reviewer_rbac.up.sql"),
		filepath.Join(directory, "20260714000600_create_reviewer_rbac.down.sql")
}
