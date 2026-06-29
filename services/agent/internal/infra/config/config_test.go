package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/configsource"
	"github.com/FigoGoo/Dora-Agent/services/internal/envconfig"
)

func TestLoadFromDefaultsAndLocalOverride(t *testing.T) {
	unsetAgentEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	local := filepath.Join(dir, ".env.local")
	writeEnv(t, example, `
APP_ENV=local
LOG_LEVEL=info
AGENT_DATABASE_URL=postgres://example
AGENT_HTTP_ADDR=0.0.0.0:18080
AGENT_SERVICE_NAME=dora.agent
BUSINESS_SERVICE_NAME=dora.business
BUSINESS_HOSTPORTS=127.0.0.1:19001,127.0.0.1:29001
KITEX_TIMEOUT_MS=3000
AGENT_EVENT_REPLAY_PAGE_SIZE=10
AGENT_EVENT_REPLAY_MAX_PAGE_SIZE=100
AGENT_MEMORY_ENABLED=true
AGENT_TOOL_DEFAULT_TIMEOUT_MS=120000
AGENT_SAFETY_POLICY_VERSION=local-v1
`)
	writeEnv(t, local, `
LOG_LEVEL=debug
AGENT_HTTP_ADDR=127.0.0.1:28080
AGENT_TOOL_ALLOWLIST=image,video
AGENT_GENERATION_QUEUE=redis
AGENT_GENERATION_REDIS_ADDR=127.0.0.1:6379
AGENT_GENERATION_REDIS_DB=2
AGENT_GENERATION_REDIS_LIST_KEY=dora:test:generation_jobs
AGENT_GENERATION_WORKERS=3
AGENT_GENERATION_RECOVERY_STALE_AFTER=30s
`)

	cfg, err := LoadFrom(example, local)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:28080" {
		t.Fatalf("expected .env.local override, got %s", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected log override, got %s", cfg.LogLevel)
	}
	if cfg.KitexTimeout != 3*time.Second || cfg.ToolDefaultTimeout != 120*time.Second {
		t.Fatalf("unexpected timeouts: kitex=%s tool=%s", cfg.KitexTimeout, cfg.ToolDefaultTimeout)
	}
	if len(cfg.BusinessHostPorts) != 2 || cfg.BusinessHostPorts[0] != "127.0.0.1:19001" || cfg.BusinessHostPorts[1] != "127.0.0.1:29001" {
		t.Fatalf("unexpected business hostports: %#v", cfg.BusinessHostPorts)
	}
	if len(cfg.ToolAllowlist) != 2 || cfg.ToolAllowlist[0] != "image" || cfg.ToolAllowlist[1] != "video" {
		t.Fatalf("unexpected allowlist: %#v", cfg.ToolAllowlist)
	}
	if cfg.GenerationQueue != "redis" || cfg.GenerationRedisAddress != "127.0.0.1:6379" || cfg.GenerationRedisDB != 2 {
		t.Fatalf("unexpected generation queue config: %#v", cfg)
	}
	if cfg.GenerationRedisListKey != "dora:test:generation_jobs" || cfg.GenerationWorkers != 3 || cfg.GenerationRecoveryAge != 30*time.Second {
		t.Fatalf("unexpected generation worker config: key=%s workers=%d stale=%s", cfg.GenerationRedisListKey, cfg.GenerationWorkers, cfg.GenerationRecoveryAge)
	}
}

func TestLoadFromMissingRequiredDatabaseURL(t *testing.T) {
	unsetAgentEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	writeEnv(t, example, "AGENT_HTTP_ADDR=0.0.0.0:18080\n")

	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected missing AGENT_DATABASE_URL error")
	}
}

func TestLoadFromInvalidNumberAndBoolean(t *testing.T) {
	unsetAgentEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	writeEnv(t, example, `
AGENT_DATABASE_URL=postgres://example
AGENT_HTTP_ADDR=0.0.0.0:18080
AGENT_SSE_ENABLED=maybe
`)
	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected invalid boolean error")
	}

	writeEnv(t, example, `
AGENT_DATABASE_URL=postgres://example
AGENT_HTTP_ADDR=0.0.0.0:18080
AGENT_EVENT_REPLAY_PAGE_SIZE=abc
`)
	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected invalid integer error")
	}
}

func TestLoadFromAgentConfigEtcdOverlayAndEnvFinal(t *testing.T) {
	unsetAgentEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	writeEnv(t, example, `
DORA_CONFIG_SOURCE=etcd
APP_ENV=local
LOG_LEVEL=info
AGENT_DATABASE_URL=postgres://example
AGENT_HTTP_ADDR=0.0.0.0:18080
AGENT_SERVICE_NAME=dora.agent
ETCD_ENDPOINTS=http://127.0.0.1:2379
ETCD_NAMESPACE=/dora/local
`)
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := loadFromWithEtcdLoader([]string{example}, func(_ context.Context, opts configsource.EtcdOptions) (envconfig.Values, error) {
		if opts.ServiceName != "dora.agent" {
			t.Fatalf("unexpected service name: %s", opts.ServiceName)
		}
		if contains(opts.AllowedKeys, "AGENT_DATABASE_URL") || contains(opts.AllowedKeys, "ETCD_ENDPOINTS") {
			t.Fatal("sensitive or bootstrap agent config must not be allowed from etcd")
		}
		if contains(opts.AllowedKeys, "AGENT_GENERATION_REDIS_PASSWORD") {
			t.Fatal("redis password must not be allowed from etcd")
		}
		return envconfig.Values{
			"LOG_LEVEL":                    "debug",
			"AGENT_EVENT_REPLAY_PAGE_SIZE": "20",
			"AGENT_TOOL_ALLOWLIST":         "image,video",
			"AGENT_GENERATION_QUEUE":       "redis",
		}, nil
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LogLevel != "error" {
		t.Fatalf("expected env to override etcd log level, got %s", cfg.LogLevel)
	}
	if cfg.EventReplayPageSize != 20 {
		t.Fatalf("expected etcd page size overlay, got %d", cfg.EventReplayPageSize)
	}
	if len(cfg.ToolAllowlist) != 2 || cfg.ToolAllowlist[0] != "image" || cfg.ToolAllowlist[1] != "video" {
		t.Fatalf("unexpected allowlist: %#v", cfg.ToolAllowlist)
	}
	if cfg.GenerationQueue != "redis" {
		t.Fatalf("expected etcd generation queue overlay, got %s", cfg.GenerationQueue)
	}
}

func unsetAgentEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"DORA_CONFIG_SOURCE", "DORA_CONFIG_ETCD_TIMEOUT",
		"APP_ENV", "APP_NAME", "LOG_LEVEL", "AGENT_DATABASE_URL", "AGENT_HTTP_ADDR",
		"AGENT_SERVICE_NAME", "BUSINESS_SERVICE_NAME", "BUSINESS_HOSTPORTS", "KITEX_REGISTRY", "KITEX_TIMEOUT_MS",
		"AGENT_SSE_ENABLED", "AGENT_WS_ENABLED", "AGENT_SSE_HEARTBEAT_SECONDS",
		"AGENT_EVENT_REPLAY_PAGE_SIZE", "AGENT_EVENT_REPLAY_MAX_PAGE_SIZE", "AGENT_CONFIG_SOURCE",
		"AGENT_DEFAULT_CONFIG_VERSION", "AGENT_TOOL_ALLOWLIST", "AGENT_MEMORY_ENABLED",
		"AGENT_TOOL_DEFAULT_TIMEOUT_MS", "AGENT_SAFETY_POLICY_VERSION", "AGENT_GENERATION_QUEUE",
		"AGENT_GENERATION_REDIS_ADDR", "AGENT_GENERATION_REDIS_PASSWORD", "AGENT_GENERATION_REDIS_DB",
		"AGENT_GENERATION_REDIS_LIST_KEY", "AGENT_GENERATION_WORKERS", "AGENT_GENERATION_RECOVERY_STALE_AFTER",
		"ETCD_ENDPOINTS", "ETCD_NAMESPACE",
	}
	for _, key := range keys {
		old, ok := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		key := key
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func writeEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
}
