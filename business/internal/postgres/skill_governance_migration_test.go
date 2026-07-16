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

func TestSkillGovernanceMigrationHasForwardOnlyContract(t *testing.T) {
	upPath, downPath := skillGovernanceMigrationPaths(t)
	upBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read Skill Governance up migration: %v", err)
	}
	downBytes, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Skill Governance down migration: %v", err)
	}
	up := string(upBytes)
	down := string(downBytes)
	upperUp := strings.ToUpper(up)
	upperDown := strings.ToUpper(down)
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, " REFERENCES ") ||
		strings.Contains(upperUp, " ON DELETE ") || strings.Contains(upperUp, " ON UPDATE CASCADE") {
		t.Fatal("Skill Governance migration contains a prohibited physical foreign key or database cascade")
	}
	if strings.Contains(upperDown, " CASCADE") {
		t.Fatal("Skill Governance down migration contains prohibited CASCADE")
	}
	for _, fragment := range []string{
		"role_key IN ('skill_reviewer', 'skill_governor')",
		"ADD COLUMN response_governance_epoch bigint NULL",
		"ADD COLUMN actor_role_key varchar(64) NULL",
		"ADD COLUMN governance_epoch bigint NULL",
		"ADD COLUMN approval_reference varchar(160) NULL",
		"ADD COLUMN source_address inet NULL",
		"ADD COLUMN command_receipt_id uuid NULL",
		"CREATE UNIQUE INDEX uq_skill_governance_audit__command_receipt",
		"CREATE INDEX idx_skill_published_snapshot__published_id",
		"command_type = 'governance_transition'",
		"action = 'governance_suspended'",
		"action = 'governance_resumed'",
		"action = 'governance_offlined'",
	} {
		if !strings.Contains(up, fragment) {
			t.Fatalf("Skill Governance up migration missing %q", fragment)
		}
	}
	for _, column := range []string{
		"actor_role_key", "governance_epoch", "approval_reference", "source_address", "command_receipt_id",
	} {
		if !strings.Contains(up, "COMMENT ON COLUMN business.skill_governance_audit."+column+" IS '") {
			t.Fatalf("skill_governance_audit.%s is missing Chinese COMMENT", column)
		}
	}
	if !strings.Contains(up, "COMMENT ON COLUMN business.skill_command_receipt.response_governance_epoch IS '") {
		t.Fatal("skill_command_receipt.response_governance_epoch is missing Chinese COMMENT")
	}
	for _, guard := range []string{
		"role_key = 'skill_governor'", "command_type = 'governance_transition'",
		"governance_status <> 'active'", "governance_epoch <> 1", "USING ERRCODE = '55000'",
	} {
		if !strings.Contains(down, guard) {
			t.Fatalf("Skill Governance down migration missing fact guard %q", guard)
		}
	}
}

// TestSkillGovernanceMigrationPostgreSQLContract 使用真实 PostgreSQL 验证 006→007、local Down、重复执行和 Schema 契约。
func TestSkillGovernanceMigrationPostgreSQLContract(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	upPath, downPath := skillGovernanceMigrationPaths(t)
	upSQL, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read Skill Governance up migration: %v", err)
	}
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Skill Governance down migration: %v", err)
	}

	assertSkillGovernanceSchemaPresent(t, db)
	if err := db.Exec(string(downSQL)).Error; err != nil {
		t.Fatalf("apply local Skill Governance down migration: %v", err)
	}
	assertSkillGovernanceSchemaAbsent(t, db)
	if err := db.Exec(string(upSQL)).Error; err != nil {
		t.Fatalf("upgrade Business schema from 006 to 007: %v", err)
	}
	assertSkillGovernanceSchemaPresent(t, db)
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("006 to 007 schema is not ready: %v", err)
	}
	if err := db.Exec(string(upSQL)).Error; err == nil {
		t.Fatal("duplicate Skill Governance migration initialization unexpectedly succeeded")
	}
	assertSkillGovernanceSchemaPresent(t, db)
}

// TestSkillGovernanceMigrationPostgreSQLRejectsIncompleteFacts 验证 PostgreSQL CHECK 不会把 NULL 当作合法治理分支。
func TestSkillGovernanceMigrationPostgreSQLRejectsIncompleteFacts(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	now := time.Date(2026, 7, 14, 13, 30, 0, 0, time.UTC)

	if err := db.Exec(`
		INSERT INTO business.skill_command_receipt (
			id, actor_user_id, command_type, scope_id, key_digest, semantic_digest,
			result_skill_id, result_content_revision_id, result_review_submission_id,
			result_published_snapshot_id, response_draft_revision_id, response_published_snapshot_id,
			response_review_submission_id, response_review_status, response_review_reason_code,
			response_review_updated_at, response_governance_status, created_at, request_id,
			response_governance_epoch
		) VALUES (?, ?, 'governance_transition', ?, decode(repeat('31', 32), 'hex'), decode(repeat('32', 32), 'hex'),
			?, NULL, NULL, ?, ?, ?, NULL, NULL, NULL, NULL, 'suspended', ?, ?, NULL)`,
		"019f0000-0000-7000-8000-0000000000d1",
		"019f0000-0000-7000-8000-0000000000d2",
		"019f0000-0000-7000-8000-0000000000d3",
		"019f0000-0000-7000-8000-0000000000d3",
		"019f0000-0000-7000-8000-0000000000d4",
		"019f0000-0000-7000-8000-0000000000d5",
		"019f0000-0000-7000-8000-0000000000d4",
		now,
		"019f0000-0000-7000-8000-0000000000d6",
	).Error; err == nil {
		t.Fatal("governance receipt with NULL response epoch unexpectedly satisfied CHECK")
	}

	for _, testCase := range []struct {
		name       string
		actorRole  any
		reasonCode any
		epoch      any
	}{
		{name: "NULL actor role", actorRole: nil, reasonCode: "content_safety", epoch: int64(2)},
		{name: "NULL reason", actorRole: "skill_governor", reasonCode: nil, epoch: int64(2)},
		{name: "NULL epoch", actorRole: "skill_governor", reasonCode: "content_safety", epoch: nil},
		{name: "unknown reason", actorRole: "skill_governor", reasonCode: "free_text", epoch: int64(2)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := db.Exec(`
				INSERT INTO business.skill_governance_audit (
					id, skill_id, review_submission_id, action, from_status, to_status,
					safe_reason_code, actor_user_id, occurred_at, request_id, actor_role_key,
					governance_epoch, approval_reference, source_address, command_receipt_id
				) VALUES (?, ?, NULL, 'governance_suspended', 'active', 'suspended',
					?, ?, ?, ?, ?, ?, 'TICKET-123', '127.0.0.1', ?)`,
				"019f0000-0000-7000-8000-0000000000e1",
				"019f0000-0000-7000-8000-0000000000e2",
				testCase.reasonCode,
				"019f0000-0000-7000-8000-0000000000e3",
				now,
				"019f0000-0000-7000-8000-0000000000e4",
				testCase.actorRole,
				testCase.epoch,
				"019f0000-0000-7000-8000-0000000000e5",
			).Error; err == nil {
				t.Fatal("incomplete governance audit unexpectedly satisfied CHECK")
			}
		})
	}
}

// TestSkillGovernanceMigrationPostgreSQLDownRejectsGovernorFact 验证任一 Governor assignment 都阻止破坏性 local Down。
func TestSkillGovernanceMigrationPostgreSQLDownRejectsGovernorFact(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	_, downPath := skillGovernanceMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Skill Governance down migration: %v", err)
	}
	now := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	for _, account := range []struct {
		id   string
		name string
	}{
		{"019f0000-0000-7000-8000-0000000000a1", "Governor"},
		{"019f0000-0000-7000-8000-0000000000a2", "Provisioner"},
	} {
		if err := db.Exec(`
			INSERT INTO business.user_account (id, display_name, user_type, status, version, created_at, updated_at)
			VALUES (?, ?, 'personal', 'active', 1, ?, ?)`, account.id, account.name, now, now).Error; err != nil {
			t.Fatalf("seed Governor rollback account: %v", err)
		}
	}
	if err := db.Exec(`
		INSERT INTO business.user_role_assignment (
			id, user_id, role_key, status, version, assigned_by_user_id,
			assignment_reason_code, approval_reference, assigned_at, updated_at
		) VALUES (?, ?, 'skill_governor', 'active', 1, ?, 'governor_onboarding', 'DEPLOY-701', ?, ?)`,
		"019f0000-0000-7000-8000-0000000000a3",
		"019f0000-0000-7000-8000-0000000000a1",
		"019f0000-0000-7000-8000-0000000000a2", now, now,
	).Error; err != nil {
		t.Fatalf("seed Governor rollback fact: %v", err)
	}
	if err := db.Exec(string(downSQL)).Error; err == nil || !strings.Contains(err.Error(), "cannot rollback Skill governance migration") {
		t.Fatalf("Skill Governance down did not reject Governor fact: %v", err)
	}
	assertSkillGovernanceSchemaPresent(t, db)
}

// TestSkillGovernanceReadinessPostgreSQLRejectsOrphanReceipt 验证治理回执缺少审计时 Runtime 不进入 Ready。
func TestSkillGovernanceReadinessPostgreSQLRejectsOrphanReceipt(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	seedGovernanceReceipt(t, db, "019f0000-0000-7000-8000-0000000000b1")
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err == nil ||
		!strings.Contains(err.Error(), "orphan or mismatched receipt audits") {
		t.Fatalf("orphan governance receipt did not block readiness: %v", err)
	}
}

// TestSkillGovernanceReadinessPostgreSQLAcceptsMatchingFacts 验证一对字段完全一致的治理回执与审计允许 Runtime Ready。
func TestSkillGovernanceReadinessPostgreSQLAcceptsMatchingFacts(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	receiptID := "019f0000-0000-7000-8000-0000000000b1"
	seedGovernanceReceipt(t, db, receiptID)
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO business.skill_governance_audit (
			id, skill_id, review_submission_id, action, from_status, to_status,
			safe_reason_code, actor_user_id, occurred_at, request_id, actor_role_key,
			governance_epoch, approval_reference, source_address, command_receipt_id
		) VALUES (?, ?, NULL, 'governance_suspended', 'active', 'suspended',
			'content_safety', ?, ?, ?, 'skill_governor', 2, 'TICKET-123', '127.0.0.1', ?)`,
		"019f0000-0000-7000-8000-0000000000b8",
		"019f0000-0000-7000-8000-0000000000b3",
		"019f0000-0000-7000-8000-0000000000b2",
		now,
		"019f0000-0000-7000-8000-0000000000b7",
		receiptID,
	).Error; err != nil {
		t.Fatalf("seed matching governance audit: %v", err)
	}
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("matching governance receipt/audit facts did not pass readiness: %v", err)
	}
}

// TestSkillGovernanceReadinessPostgreSQLRejectsMismatchedAudit 验证治理审计与回执 actor 错配时 Runtime 不进入 Ready。
func TestSkillGovernanceReadinessPostgreSQLRejectsMismatchedAudit(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	receiptID := "019f0000-0000-7000-8000-0000000000c1"
	seedGovernanceReceipt(t, db, receiptID)
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO business.skill_governance_audit (
			id, skill_id, review_submission_id, action, from_status, to_status,
			safe_reason_code, actor_user_id, occurred_at, request_id, actor_role_key,
			governance_epoch, approval_reference, source_address, command_receipt_id
		) VALUES (?, ?, NULL, 'governance_suspended', 'active', 'suspended',
			'content_safety', ?, ?, ?, 'skill_governor', 2, 'TICKET-123', '127.0.0.1', ?)`,
		"019f0000-0000-7000-8000-0000000000c2",
		"019f0000-0000-7000-8000-0000000000b3",
		"019f0000-0000-7000-8000-0000000000c3",
		now, "019f0000-0000-7000-8000-0000000000b7", receiptID,
	).Error; err != nil {
		t.Fatalf("seed mismatched governance audit: %v", err)
	}
	if err := (&Client{db: db}).VerifySchema(context.Background(), 5*time.Second); err == nil ||
		!strings.Contains(err.Error(), "orphan or mismatched receipt audits") {
		t.Fatalf("mismatched governance audit did not block readiness: %v", err)
	}
}

// seedGovernanceReceipt 插入满足数据库分支约束的治理回执，供 Readiness 双向完整性测试复用。
func seedGovernanceReceipt(t *testing.T, db *gorm.DB, receiptID string) {
	t.Helper()
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO business.skill_command_receipt (
			id, actor_user_id, command_type, scope_id, key_digest, semantic_digest,
			result_skill_id, result_content_revision_id, result_review_submission_id,
			result_published_snapshot_id, response_draft_revision_id, response_published_snapshot_id,
			response_review_submission_id, response_review_status, response_review_reason_code,
			response_review_updated_at, response_governance_status, created_at, request_id,
			response_governance_epoch
		) VALUES (?, ?, 'governance_transition', ?, decode(repeat('11', 32), 'hex'), decode(repeat('22', 32), 'hex'),
			?, NULL, NULL, ?, ?, ?, NULL, NULL, NULL, NULL, 'suspended', ?, ?, 2)`,
		receiptID,
		"019f0000-0000-7000-8000-0000000000b2",
		"019f0000-0000-7000-8000-0000000000b3",
		"019f0000-0000-7000-8000-0000000000b3",
		"019f0000-0000-7000-8000-0000000000b4",
		"019f0000-0000-7000-8000-0000000000b5",
		"019f0000-0000-7000-8000-0000000000b4",
		now,
		"019f0000-0000-7000-8000-0000000000b7",
	).Error; err != nil {
		t.Fatalf("seed governance receipt: %v", err)
	}
}

// assertSkillGovernanceSchemaPresent 验证 007 字段、约束、索引、中文 COMMENT 与零物理外键契约。
func assertSkillGovernanceSchemaPresent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var requiredColumnCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'business'
		  AND (
		    (table_name = 'skill_command_receipt' AND column_name = 'response_governance_epoch') OR
		    (table_name = 'skill_governance_audit' AND column_name IN (
		      'actor_role_key', 'governance_epoch', 'approval_reference', 'source_address', 'command_receipt_id'
		    ))
		  )`).Scan(&requiredColumnCount).Error; err != nil || requiredColumnCount != 6 {
		t.Fatalf("Skill Governance columns are incomplete: count=%d err=%v", requiredColumnCount, err)
	}
	var requiredObjectCount int64
	if err := db.Raw(`
		SELECT
		  (SELECT COUNT(*) FROM pg_constraint constraint_record
		   JOIN pg_class relation ON relation.oid = constraint_record.conrelid
		   JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
		   WHERE namespace_record.nspname = 'business'
		     AND constraint_record.conname IN (
		       'ck_skill_command_receipt__governance_branch',
		       'ck_skill_governance_audit__payload',
		       'ck_skill_governance_audit__transition'
		     )) +
		  (SELECT COUNT(*) FROM pg_indexes
		   WHERE schemaname = 'business'
		     AND indexname IN (
		       'uq_skill_governance_audit__command_receipt',
		       'idx_skill_published_snapshot__published_id'
		     ))`).Scan(&requiredObjectCount).Error; err != nil || requiredObjectCount != 5 {
		t.Fatalf("Skill Governance constraints or indexes are incomplete: count=%d err=%v", requiredObjectCount, err)
	}
	var roleConstraint string
	if err := db.Raw(`
		SELECT pg_get_constraintdef(constraint_record.oid)
		FROM pg_constraint constraint_record
		JOIN pg_class relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
		WHERE namespace_record.nspname = 'business'
		  AND relation.relname = 'user_role_assignment'
		  AND constraint_record.conname = 'ck_user_role_assignment__role'`).Scan(&roleConstraint).Error; err != nil ||
		!strings.Contains(roleConstraint, "skill_reviewer") || !strings.Contains(roleConstraint, "skill_governor") {
		t.Fatalf("Skill Governance role constraint does not include both closed roles: definition=%q err=%v", roleConstraint, err)
	}
	var invalidContractCount int64
	if err := db.Raw(`
		SELECT
		  (SELECT COUNT(*)
		   FROM pg_constraint constraint_record
		   JOIN pg_class relation ON relation.oid = constraint_record.conrelid
		   JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
		   WHERE namespace_record.nspname = 'business' AND constraint_record.contype = 'f') +
		  (SELECT COUNT(*)
		   FROM pg_class relation
		   JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
		   JOIN pg_attribute attribute_record ON attribute_record.attrelid = relation.oid
		   WHERE namespace_record.nspname = 'business'
		     AND relation.relname IN ('skill_command_receipt', 'skill_governance_audit')
		     AND attribute_record.attname IN (
		       'response_governance_epoch', 'actor_role_key', 'governance_epoch',
		       'approval_reference', 'source_address', 'command_receipt_id'
		     )
		     AND COALESCE(col_description(relation.oid, attribute_record.attnum), '') !~ '[一-龥]')`).
		Scan(&invalidContractCount).Error; err != nil || invalidContractCount != 0 {
		t.Fatalf("Skill Governance schema violates foreign-key/comment contract: count=%d err=%v", invalidContractCount, err)
	}
}

// assertSkillGovernanceSchemaAbsent 验证 local Down 只移除 007 新增对象并恢复 Reviewer 单角色约束。
func assertSkillGovernanceSchemaAbsent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var remainingCount int64
	if err := db.Raw(`
		SELECT
		  (SELECT COUNT(*)
		   FROM information_schema.columns
		   WHERE table_schema = 'business'
		     AND (
		       (table_name = 'skill_command_receipt' AND column_name = 'response_governance_epoch') OR
		       (table_name = 'skill_governance_audit' AND column_name IN (
		         'actor_role_key', 'governance_epoch', 'approval_reference', 'source_address', 'command_receipt_id'
		       ))
		     )) +
		  (SELECT COUNT(*) FROM pg_indexes
		   WHERE schemaname = 'business'
		     AND indexname IN (
		       'uq_skill_governance_audit__command_receipt',
		       'idx_skill_published_snapshot__published_id'
		     ))`).Scan(&remainingCount).Error; err != nil || remainingCount != 0 {
		t.Fatalf("Skill Governance down migration retained 007 objects: count=%d err=%v", remainingCount, err)
	}
	var roleConstraint string
	if err := db.Raw(`
		SELECT pg_get_constraintdef(constraint_record.oid)
		FROM pg_constraint constraint_record
		JOIN pg_class relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace namespace_record ON namespace_record.oid = relation.relnamespace
		WHERE namespace_record.nspname = 'business'
		  AND relation.relname = 'user_role_assignment'
		  AND constraint_record.conname = 'ck_user_role_assignment__role'`).Scan(&roleConstraint).Error; err != nil ||
		!strings.Contains(roleConstraint, "skill_reviewer") || strings.Contains(roleConstraint, "skill_governor") {
		t.Fatalf("Skill Governance down did not restore Reviewer role constraint: definition=%q err=%v", roleConstraint, err)
	}
}

// skillGovernanceMigrationPaths 返回本测试文件相对的固定 007 Up/Down 路径。
func skillGovernanceMigrationPaths(t *testing.T) (string, string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Skill Governance migration test path")
	}
	directory := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	return filepath.Join(directory, "20260714000700_create_skill_governance.up.sql"),
		filepath.Join(directory, "20260714000700_create_skill_governance.down.sql")
}
