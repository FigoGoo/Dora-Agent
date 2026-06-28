package testdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Database struct {
	URL string
	DB  *gorm.DB
	SQL *sql.DB
}

func StartPostgres(t *testing.T, databaseName string) *Database {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	ctr, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase(databaseName),
		tcpostgres.WithUsername("dora"),
		tcpostgres.WithPassword("dora_test_password"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Fatalf("terminate postgres container: %v", err)
		}
	})

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	return &Database{URL: url, DB: db, SQL: sqlDB}
}

func ApplyMigrations(t *testing.T, dbURL, migrationDir string) *migrate.Migrate {
	t.Helper()
	source := "file://" + filepath.Join(RepoRoot(t), migrationDir)
	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	driver, err := migratepostgres.WithInstance(sqlDB, &migratepostgres.Config{})
	if err != nil {
		t.Fatalf("create postgres migrate driver: %v", err)
	}
	migrator, err := migrate.NewWithDatabaseInstance(source, "postgres", driver)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("run migration up for %s: %v", migrationDir, err)
	}
	return migrator
}

func DownMigrations(t *testing.T, migrator *migrate.Migrate) {
	t.Helper()
	if err := migrator.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("run migration down: %v", err)
	}
	srcErr, dbErr := migrator.Close()
	if srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator source=%v database=%v", srcErr, dbErr)
	}
}

func MustReadSQL(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(RepoRoot(t), path))
	if err != nil {
		t.Fatalf("read sql file %s: %v", path, err)
	}
	return string(data)
}

func TableExists(t *testing.T, db *gorm.DB, table string) bool {
	t.Helper()
	var exists bool
	err := db.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)", table).Scan(&exists).Error
	if err != nil {
		t.Fatalf("check table exists %s: %v", table, err)
	}
	return exists
}

func CountTables(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var count int
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'schema_migrations'").Scan(&count).Error; err != nil {
		t.Fatalf("count public tables: %v", err)
	}
	return count
}

func RequireNoForeignKeys(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int
	err := db.Raw("SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema = 'public' AND constraint_type = concat('FOREIGN ', 'KEY')").Scan(&count).Error
	if err != nil {
		t.Fatalf("count foreign keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no foreign keys, found %d", count)
	}
}

func ExecSQL(t *testing.T, db *gorm.DB, sqlText string) {
	t.Helper()
	if err := db.Exec(sqlText).Error; err != nil {
		t.Fatalf("execute sql: %v", err)
	}
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repo root from %s: %v", root, err)
	}
	return root
}
