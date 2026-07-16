package config

import "testing"

func setValidEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("WORKER_INSTANCE_ID", "worker-test-1")
	t.Setenv("WORKER_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/worker?sslmode=disable")
	t.Setenv("WORKER_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("WORKER_ETCD_ENDPOINTS", "127.0.0.1:2379")
}

// TestLoadValidConfig 验证默认租约时间关系满足 Worker 约束。
func TestLoadValidConfig(t *testing.T) {
	setValidEnvironment(t)
	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载合法配置失败: %v", err)
	}
	if cfg.Service.Name != serviceName {
		t.Fatalf("服务名不一致: got %q", cfg.Service.Name)
	}
}

// TestLoadRejectsUnsafeHeartbeat 验证 Heartbeat 不得超过 Lease TTL 的三分之一。
func TestLoadRejectsUnsafeHeartbeat(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("WORKER_LEASE_TTL", "30s")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL", "11s")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望不安全的 Heartbeat 配置被拒绝")
	}
}

// TestLoadRejectsInvalidConcurrency 验证非法整数不会被静默接受。
func TestLoadRejectsInvalidConcurrency(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("WORKER_CONCURRENCY", "zero")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法并发数被拒绝")
	}
}
