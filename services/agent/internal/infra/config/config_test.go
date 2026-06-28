package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if len(cfg.ToolAllowlist) != 2 || cfg.ToolAllowlist[0] != "image" || cfg.ToolAllowlist[1] != "video" {
		t.Fatalf("unexpected allowlist: %#v", cfg.ToolAllowlist)
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

func unsetAgentEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_ENV", "APP_NAME", "LOG_LEVEL", "AGENT_DATABASE_URL", "AGENT_HTTP_ADDR",
		"AGENT_SERVICE_NAME", "BUSINESS_SERVICE_NAME", "KITEX_REGISTRY", "KITEX_TIMEOUT_MS",
		"AGENT_SSE_ENABLED", "AGENT_WS_ENABLED", "AGENT_SSE_HEARTBEAT_SECONDS",
		"AGENT_EVENT_REPLAY_PAGE_SIZE", "AGENT_EVENT_REPLAY_MAX_PAGE_SIZE", "AGENT_CONFIG_SOURCE",
		"AGENT_DEFAULT_CONFIG_VERSION", "AGENT_TOOL_ALLOWLIST", "AGENT_MEMORY_ENABLED",
		"AGENT_TOOL_DEFAULT_TIMEOUT_MS", "AGENT_SAFETY_POLICY_VERSION", "ETCD_ENDPOINTS", "ETCD_NAMESPACE",
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

func writeEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
}
