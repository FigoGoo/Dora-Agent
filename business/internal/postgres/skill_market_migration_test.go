package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestSkillMarketMigrationHasKeysetIndexCommentAndSafeDown(t *testing.T) {
	upPath, downPath := skillMarketMigrationPaths(t)
	upBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read Skill Market up migration: %v", err)
	}
	downBytes, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Skill Market down migration: %v", err)
	}
	up := string(upBytes)
	down := string(downBytes)
	upperUp := strings.ToUpper(up)
	upperDown := strings.ToUpper(down)
	if strings.Contains(upperUp, "FOREIGN KEY") || strings.Contains(upperUp, " REFERENCES ") ||
		strings.Contains(upperUp, " ON DELETE ") || strings.Contains(upperUp, " ON UPDATE CASCADE") {
		t.Fatal("Skill Market migration contains a prohibited physical foreign key or database cascade")
	}
	if strings.Contains(upperDown, "CASCADE") {
		t.Fatal("Skill Market down migration contains prohibited CASCADE")
	}
	for _, fragment := range []string{
		"CREATE INDEX idx_skill_published_snapshot__published_skill_id",
		"ON business.skill_published_snapshot (published_at DESC, skill_id DESC)",
		"COMMENT ON INDEX business.idx_skill_published_snapshot__published_skill_id",
		"DROP INDEX business.idx_skill_published_snapshot__published_skill_id",
	} {
		if !strings.Contains(up+down, fragment) {
			t.Fatalf("Skill Market migration missing %q", fragment)
		}
	}
	if !strings.Contains(up, "IS '") || !strings.Contains(up, "公开市场") {
		t.Fatal("Skill Market index is missing a Chinese COMMENT")
	}
}

func TestSkillMarketMigrationPostgreSQLIndexContract(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	upPath, downPath := skillMarketMigrationPaths(t)
	upBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read Skill Market up migration: %v", err)
	}
	downBytes, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read Skill Market down migration: %v", err)
	}
	assertSkillMarketIndexPresent(t, db)
	if err := db.Exec(string(downBytes)).Error; err != nil {
		t.Fatalf("apply local Skill Market down migration: %v", err)
	}
	var remaining int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'business' AND indexname = 'idx_skill_published_snapshot__published_skill_id'`).Scan(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("Skill Market down retained index: count=%d err=%v", remaining, err)
	}
	if err := db.Exec(string(upBytes)).Error; err != nil {
		t.Fatalf("reapply Skill Market up migration: %v", err)
	}
	assertSkillMarketIndexPresent(t, db)
}

func assertSkillMarketIndexPresent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var result struct {
		Definition string `gorm:"column:definition"`
		Comment    string `gorm:"column:comment"`
	}
	if err := db.Raw(`
		SELECT pg_get_indexdef(index_record.indexrelid) AS definition,
		       obj_description(index_record.indexrelid, 'pg_class') AS comment
		FROM pg_index AS index_record
		JOIN pg_class AS relation ON relation.oid = index_record.indexrelid
		JOIN pg_namespace AS namespace_record ON namespace_record.oid = relation.relnamespace
		WHERE namespace_record.nspname = 'business'
		  AND relation.relname = 'idx_skill_published_snapshot__published_skill_id'`).Scan(&result).Error; err != nil ||
		!strings.Contains(result.Definition, "published_at DESC, skill_id DESC") || !strings.Contains(result.Comment, "公开市场") {
		t.Fatalf("Skill Market index contract mismatch: result=%+v err=%v", result, err)
	}
}

func skillMarketMigrationPaths(t *testing.T) (string, string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Skill Market migration test path")
	}
	directory := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	return filepath.Join(directory, "20260714000800_create_skill_market_read.up.sql"),
		filepath.Join(directory, "20260714000800_create_skill_market_read.down.sql")
}
