package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
)

// TestMigratedSchemaContract 使用真实 PostgreSQL 验证 Worker Migration 的 Schema 契约。
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
		t.Fatalf("连接 Worker 契约测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("关闭 Worker 契约测试数据库失败: %v", err)
		}
	})
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("Worker Migration Schema 契约无效: %v", err)
	}
}
