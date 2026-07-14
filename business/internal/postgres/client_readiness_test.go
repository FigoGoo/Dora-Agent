package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newReadinessTestClient 创建由 sqlmock 驱动的 PostgreSQL Client，用于验证 Ready 失败关闭路径。
func newReadinessTestClient(t *testing.T) (*Client, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create readiness sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open readiness gorm database: %v", err)
	}
	return &Client{db: db}, mock
}

func TestVerifySchemaRejectsMissingRequiredTable(t *testing.T) {
	client, mock := newReadinessTestClient(t)
	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(len(requiredBusinessTables) - 1))

	err := client.VerifySchema(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "missing required tables") {
		t.Fatalf("expected missing required table readiness error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet readiness SQL expectations: %v", err)
	}
}

func TestVerifySchemaRejectsMissingRequiredAuthColumn(t *testing.T) {
	client, mock := newReadinessTestClient(t)
	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`pg_catalog\.pg_tables`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(len(requiredBusinessTables)))
	mock.ExpectQuery(`information_schema\.columns`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err := client.VerifySchema(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "missing required auth or RBAC columns") {
		t.Fatalf("expected missing auth/RBAC column readiness error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet auth column readiness SQL expectations: %v", err)
	}
}

func TestVerifySchemaRejectsAuthorizationOrphan(t *testing.T) {
	client, mock := newReadinessTestClient(t)
	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`pg_catalog\.pg_tables`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(len(requiredBusinessTables)))
	mock.ExpectQuery(`information_schema\.columns`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`FROM business\.user_role_assignment AS assignment`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := client.VerifySchema(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "orphan or unknown assignments") {
		t.Fatalf("expected authorization integrity readiness error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet authorization readiness SQL expectations: %v", err)
	}
}
