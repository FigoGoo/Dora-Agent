package postgres

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMigratedSchemaContract 使用真实 PostgreSQL 验证 Agent Migration 的 Schema 契约。
func TestMigratedSchemaContract(t *testing.T) {
	dsn := os.Getenv("DORA_POSTGRES_CONTRACT_DSN")
	if dsn == "" {
		t.Skip("未设置 DORA_POSTGRES_CONTRACT_DSN，跳过真实 PostgreSQL 契约测试")
	}
	client, err := Open(context.Background(), config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Agent 契约测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("关闭 Agent 契约测试数据库失败: %v", err)
		}
	})
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("Agent Migration Schema 契约无效: %v", err)
	}
}

// TestVerifySchemaRejectsMissingW0Table 验证只有旧 Agent Schema 时 Readiness 会列出缺失 W0 表并失败关闭。
func TestVerifySchemaRejectsMissingW0Table(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 PostgreSQL Mock 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
			t.Errorf("Readiness SQL 不符合固定查询契约: %v", expectationErr)
		}
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("创建 GORM Mock 连接失败: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)")).
		WithArgs(schemaName).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	existingRows := sqlmock.NewRows([]string{"table_name"})
	for _, tableName := range requiredAgentTables() {
		if tableName != "session_event_log" {
			existingRows.AddRow(tableName)
		}
	}
	mock.ExpectQuery("SELECT relation\\.relname AS table_name.*FROM pg_class AS relation.*pg_namespace").
		WithArgs(schemaName).
		WillReturnRows(existingRows)
	mock.ExpectClose()

	client := &Client{db: db}
	err = client.VerifySchema(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "session_event_log") {
		t.Fatalf("缺少 W0 表时错误=%v，want 包含 session_event_log", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("缺表被错误映射为超时: %v", err)
	}
}
