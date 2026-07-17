package config

import (
	"os"
	"testing"
)

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

// TestLoadMediaRuntimeValid 验证完整 local/loopback/Profile 和预算配置能够显式启用媒体 Runtime。
func TestLoadMediaRuntimeValid(t *testing.T) {
	setValidMediaEnvironment(t)
	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载媒体 Runtime 配置失败: %v", err)
	}
	if !cfg.MediaRuntime.Enabled() || cfg.MediaRuntime.Profile != MediaRuntimeProfileV3Preview1 {
		t.Fatalf("媒体 Runtime 未启用: %+v", cfg.MediaRuntime)
	}
}

// TestLoadMediaRuntimeRejectsUnknownProfile 验证未知非空 Profile 不会静默降级为关闭状态。
func TestLoadMediaRuntimeRejectsUnknownProfile(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("DORA_WORKER_MEDIA_RUNTIME_PROFILE", "media.runtime.future")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望未知媒体 Profile 被拒绝")
	}
}

// TestLoadMediaRuntimeRejectsNonLoopback 验证媒体 Runtime 不允许 Agent 或 Business 指向非 loopback 地址。
func TestLoadMediaRuntimeRejectsNonLoopback(t *testing.T) {
	setValidMediaEnvironment(t)
	t.Setenv("DORA_WORKER_BUSINESS_BASE_URL", "http://192.0.2.10:8080")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非 loopback Business 地址被拒绝")
	}
	setValidMediaEnvironment(t)
	t.Setenv("DORA_WORKER_AGENT_CONSUMER_DSN", "postgres://user:password@192.0.2.10:5432/agent?sslmode=disable")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非 loopback Agent DSN 被拒绝")
	}
}

// TestLoadMediaRuntimeRejectsUnsafeBudget 验证响应、stderr 和重试预算非法时启动失败关闭。
func TestLoadMediaRuntimeRejectsUnsafeBudget(t *testing.T) {
	setValidMediaEnvironment(t)
	t.Setenv("DORA_WORKER_MEDIA_STDERR_LIMIT_BYTES", "999999")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望过大的 stderr 预算被拒绝")
	}
}

// TestLoadMediaRuntimeDisabledHasZeroExtraRequirements 验证 Profile 关闭时不要求媒体专用 DSN、URL 或二进制。
func TestLoadMediaRuntimeDisabledHasZeroExtraRequirements(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("DORA_WORKER_BUSINESS_BASE_URL", "not-a-url")
	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("关闭媒体 Profile 时不应校验未启用依赖: %v", err)
	}
	if cfg.MediaRuntime.Enabled() {
		t.Fatal("空 Profile 不应启用媒体 Runtime")
	}
}

func setValidMediaEnvironment(t *testing.T) {
	t.Helper()
	setValidEnvironment(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("设置测试对象根权限失败: %v", err)
	}
	t.Setenv("DORA_ENV", "local")
	t.Setenv("WORKER_HTTP_ADDR", "127.0.0.1:18083")
	t.Setenv("DORA_WORKER_MEDIA_RUNTIME_PROFILE", MediaRuntimeProfileV3Preview1)
	t.Setenv("DORA_WORKER_MEDIA_OBJECT_ROOT", root)
	t.Setenv("DORA_WORKER_AGENT_CONSUMER_DSN", "postgres://agent_worker:password@127.0.0.1:5432/agent?sslmode=disable")
	t.Setenv("DORA_WORKER_BUSINESS_BASE_URL", "http://127.0.0.1:18081")
	t.Setenv("DORA_WORKER_FFMPEG_PATH", "/usr/local/bin/ffmpeg-real")
	t.Setenv("DORA_WORKER_FFPROBE_PATH", "/usr/local/bin/ffprobe-real")
}
