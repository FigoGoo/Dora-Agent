package postgres

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"gorm.io/gorm"
)

// TestPublicMarketBindingMigrationStaticContract 验证 009 只修正文档且 Down 具有 public 历史保护。
func TestPublicMarketBindingMigrationStaticContract(t *testing.T) {
	upPath, downPath := publicMarketBindingMigrationPaths(t)
	upBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	combinedUpper := strings.ToUpper(string(upBytes) + string(downBytes))
	for _, prohibited := range []string{"FOREIGN KEY", "REFERENCES ", " ON DELETE ", " ON UPDATE ", " CASCADE"} {
		if strings.Contains(combinedUpper, prohibited) {
			t.Fatalf("Public Market Binding migration contains prohibited token %q", prohibited)
		}
	}
	up := string(upBytes)
	for _, expected := range []string{
		"COMMENT ON COLUMN business.project_session_skill_resolution_item.publisher_user_id",
		"冻结的 Skill 权威所有者（Publisher）用户标识",
		"COMMENT ON COLUMN business.project_session_skill_resolution_item.permission_snapshot_digest",
		"v1 owner-private 或 v2 public-market",
	} {
		if !strings.Contains(up, expected) {
			t.Fatalf("Public Market Binding up migration missing %q", expected)
		}
	}
	down := string(downBytes)
	for _, expected := range []string{
		"LOCK TABLE business.project_session_skill_resolution,",
		"business.project_session_skill_resolution_item IN SHARE MODE",
		"resolution_header.owner_user_id <> resolution_item.publisher_user_id",
		"cannot rollback public market binding documentation migration while public-market history exists",
		"USING ERRCODE = '55000'",
		"W1 owner-private 等于项目所有者",
	} {
		if !strings.Contains(down, expected) {
			t.Fatalf("Public Market Binding down migration missing %q", expected)
		}
	}
	lockIndex := strings.Index(down, "LOCK TABLE business.project_session_skill_resolution,")
	guardIndex := strings.Index(down, "IF EXISTS (")
	commentIndex := strings.Index(down, "COMMENT ON COLUMN business.project_session_skill_resolution_item.publisher_user_id")
	if lockIndex < 0 || guardIndex <= lockIndex || commentIndex <= guardIndex {
		t.Fatalf("Public Market Binding down must lock before guard and restore comments last: lock=%d guard=%d comment=%d", lockIndex, guardIndex, commentIndex)
	}
}

// TestPublicMarketBindingMigrationPostgreSQLPublicHistoryGuard 使用真实 PostgreSQL 证明 public-market 历史阻止错误回退注释。
func TestPublicMarketBindingMigrationPostgreSQLPublicHistoryGuard(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	publicSkill := seedPublishedProjectSkill(t, db)
	consumerUserID := newRepositoryTestUUIDv7(t)
	ensureProjectSkillBindingUserAccount(t, db, consumerUserID, "active")
	command := newProjectSkillBindingV2Command(t, consumerUserID, []string{publicSkill.Skill.ID}, "migration-public-history-key")
	if _, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{}); err != nil {
		t.Fatal(err)
	}
	_, downPath := publicMarketBindingMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(downSQL)).Error; err == nil || !strings.Contains(err.Error(), "cannot rollback public market binding documentation migration") {
		t.Fatalf("public-market history did not block 009 down: %v", err)
	}
	if comment := publicMarketBindingColumnComment(t, db, "publisher_user_id"); !strings.Contains(comment, "允许与项目所有者不同") {
		t.Fatalf("failed Down changed Publisher comment: %q", comment)
	}
	if comment := publicMarketBindingColumnComment(t, db, "permission_snapshot_digest"); !strings.Contains(comment, "v2 public-market") {
		t.Fatalf("failed Down changed Permission comment: %q", comment)
	}
}

// TestPublicMarketBindingMigrationPostgreSQLDownOwnerPrivateOnly 证明纯 owner-private 历史可以恢复 005 注释。
func TestPublicMarketBindingMigrationPostgreSQLDownOwnerPrivateOnly(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	ownerSkill := seedPublishedProjectSkill(t, db)
	command := newProjectSkillBindingV2Command(t, ownerSkill.Skill.OwnerUserID, []string{ownerSkill.Skill.ID}, "migration-owner-history-key")
	if _, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{}); err != nil {
		t.Fatal(err)
	}
	_, downPath := publicMarketBindingMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(downSQL)).Error; err != nil {
		t.Fatalf("owner-private-only 009 down failed: %v", err)
	}
	if comment := publicMarketBindingColumnComment(t, db, "publisher_user_id"); !strings.Contains(comment, "owner-private 等于项目所有者") {
		t.Fatalf("owner-private Down did not restore Publisher comment: %q", comment)
	}
}

// TestPublicMarketBindingMigrationPostgreSQLDownSerializesConcurrentWriter 证明 Down 先阻断 Resolution DML，随后观察已提交 public 历史并拒绝恢复旧注释。
func TestPublicMarketBindingMigrationPostgreSQLDownSerializesConcurrentWriter(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	publicSkill := seedPublishedProjectSkill(t, db)
	consumerUserID := newRepositoryTestUUIDv7(t)
	ensureProjectSkillBindingUserAccount(t, db, consumerUserID, "active")
	command := newProjectSkillBindingV2Command(t, consumerUserID, []string{publicSkill.Skill.ID}, "migration-concurrent-history-key")

	// 在外层事务中执行真实 QuickCreate：嵌套 savepoint 成功后，public-market 九类事实仍未对其他连接可见，
	// 但 Resolution 表已持有 ROW EXCLUSIVE，必须让 Down 的 SHARE 表锁等待到本事务提交。
	writer := db.Begin()
	if writer.Error != nil {
		t.Fatal(writer.Error)
	}
	writerFinished := false
	t.Cleanup(func() {
		if !writerFinished {
			_ = writer.Rollback().Error
		}
	})
	writerRepository := &ProjectRepository{db: writer}
	if _, err := writerRepository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{}); err != nil {
		t.Fatalf("create uncommitted public-market history: %v", err)
	}
	_, downPath := publicMarketBindingMigrationPaths(t)
	downSQL, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	downDone := make(chan error, 1)
	go func() {
		downDone <- db.Exec(string(downSQL)).Error
	}()
	waitForPublicMarketBindingMigrationLock(t, db, downDone)
	if err := writer.Commit().Error; err != nil {
		t.Fatal(err)
	}
	writerFinished = true
	select {
	case err := <-downDone:
		if err == nil || !strings.Contains(err.Error(), "cannot rollback public market binding documentation migration") {
			t.Fatalf("serialized Down did not reject committed public history: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serialized Down did not finish after concurrent writer committed")
	}
	if comment := publicMarketBindingColumnComment(t, db, "publisher_user_id"); !strings.Contains(comment, "允许与项目所有者不同") {
		t.Fatalf("serialized failed Down changed Publisher comment: %q", comment)
	}
}

// waitForPublicMarketBindingMigrationLock 等待 Down 的 SHARE 表锁真实阻塞在并发 Resolution 写事务上。
func waitForPublicMarketBindingMigrationLock(t *testing.T, db *gorm.DB, downDone <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-downDone:
			t.Fatalf("Public Market Binding Down finished before concurrent writer committed: %v", err)
		default:
		}
		var waitingCount int64
		if err := db.Raw(`
			SELECT COUNT(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%LOCK TABLE business.project_session_skill_resolution%'`).Scan(&waitingCount).Error; err != nil {
			t.Fatal(err)
		}
		if waitingCount > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Public Market Binding Down did not wait on the Resolution table lock")
}

// publicMarketBindingColumnComment 读取 009 涉及列的数据库 COMMENT。
func publicMarketBindingColumnComment(t *testing.T, db *gorm.DB, column string) string {
	t.Helper()
	var comment string
	if err := db.Raw(`
		SELECT col_description(table_record.oid, column_record.attnum)
		FROM pg_class AS table_record
		JOIN pg_namespace AS namespace_record ON namespace_record.oid = table_record.relnamespace
		JOIN pg_attribute AS column_record ON column_record.attrelid = table_record.oid
		WHERE namespace_record.nspname = 'business'
		  AND table_record.relname = 'project_session_skill_resolution_item'
		  AND column_record.attname = ?`, column).Scan(&comment).Error; err != nil {
		t.Fatal(err)
	}
	return comment
}

// publicMarketBindingMigrationPaths 返回 009 Up/Down 的固定路径，不扫描或修改已发布 Migration。
func publicMarketBindingMigrationPaths(t *testing.T) (string, string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Public Market Binding migration test path")
	}
	directory := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	return filepath.Join(directory, "20260714000900_document_public_market_binding.up.sql"),
		filepath.Join(directory, "20260714000900_document_public_market_binding.down.sql")
}
