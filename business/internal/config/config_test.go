package config

import (
	"testing"
	"time"
)

// setValidAuthSecret 为配置测试注入仅存在于进程内的 32-byte Base64 CSRF 密钥。
func setValidAuthSecret(t *testing.T) {
	t.Helper()
	t.Setenv("BUSINESS_AUTH_CSRF_SECRET_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("BUSINESS_PROJECT_PROMPT_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("BUSINESS_PROJECT_PROMPT_KEY_VERSION", "project-prompt-test-v1")
	t.Setenv("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=")
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION", "agent-http-test-v1")
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
}

// TestLoadValidConfig 验证完整本地配置可以通过启动校验。
func TestLoadValidConfig(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载合法配置失败: %v", err)
	}
	if cfg.Service.Name != serviceName {
		t.Fatalf("服务名不一致: got %q", cfg.Service.Name)
	}
	if cfg.Auth.MaxConcurrentSessions != 5 || cfg.Auth.LoginRateLimitMaxAttempts != 10 ||
		cfg.Auth.LoginRateLimitWindow.String() != "15m0s" || cfg.Auth.LoginRateLimitTimeout.String() != "500ms" {
		t.Fatalf("鉴权安全策略默认值不一致: %+v", cfg.Auth)
	}
	if cfg.AgentHTTP.BaseURL != "http://127.0.0.1:18082" || cfg.AgentHTTP.RequestTimeout != 5*time.Second ||
		cfg.AgentHTTP.AssertionKeyVersion != "agent-http-test-v1" || cfg.AgentHTTP.AssertionTTL != 30*time.Second {
		t.Fatalf("Agent HTTP 默认值不一致: base_url=%q timeout=%s kid=%q ttl=%s",
			cfg.AgentHTTP.BaseURL, cfg.AgentHTTP.RequestTimeout, cfg.AgentHTTP.AssertionKeyVersion, cfg.AgentHTTP.AssertionTTL)
	}
	if cfg.Skill.MaxRequestBodyBytes != 524288 {
		t.Fatalf("Skill Builder 请求体默认上限不一致: %+v", cfg.Skill)
	}
	if cfg.Project.SkillSnapshotV2Enabled || cfg.Project.AgentSessionV2CapabilityConfirmed ||
		cfg.Project.SkillSnapshotLimitsProfile != "session_skill_snapshot_limits.v1" ||
		cfg.Project.SkillSnapshotLimits.MaxItems != 16 {
		t.Fatalf("QuickCreate v2 默认门禁或 limits 漂移: %+v", cfg.Project)
	}
}

func TestLoadRequiresAgentCapabilityAndSafeSkillSnapshotLimitsForV2(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_PROJECT_SKILL_SNAPSHOT_V2_ENABLED", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("未确认 Agent v2 capability 时开启新流量未失败关闭")
	}
	t.Setenv("BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED", "true")
	if _, err := Load("test"); err != nil {
		t.Fatalf("已确认 capability 的默认 v2 limits 未通过: %v", err)
	}
	t.Setenv("BUSINESS_SKILL_SNAPSHOT_MAX_ITEMS", "33")
	if _, err := Load("test"); err == nil {
		t.Fatal("超过协议 ceiling 的 Business Producer limits 被接受")
	}
}

func TestLoadValidatesProjectPromptPreviousKeyPair(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")

	t.Setenv("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_VERSION", "project-prompt-test-v0")
	if _, err := Load("test"); err == nil {
		t.Fatal("只配置 previous KeyVersion 未失败关闭")
	}
	t.Setenv("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_BASE64", "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=")
	if _, err := Load("test"); err != nil {
		t.Fatalf("合法 previous key pair 未通过: %v", err)
	}
	t.Setenv("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if _, err := Load("test"); err == nil {
		t.Fatal("previous KeyVersion 复用 active 根密钥未失败关闭")
	}
}

func TestLoadRejectsUnsafeSkillRequestBodyLimit(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_SKILL_MAX_REQUEST_BODY_BYTES", "1048577")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望超过上限的 Skill Builder 请求体配置被拒绝")
	}
}

// TestLoadRejectsLoopbackAdvertisedAddress 验证服务注册不会发布回环地址。
func TestLoadRejectsLoopbackAdvertisedAddress(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望回环注册地址被拒绝")
	}
}

// TestLoadRejectsLoopbackRPCAdvertisedAddress 验证 Foundation RPC 不会向 etcd 发布回环地址。
func TestLoadRejectsLoopbackRPCAdvertisedAddress(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望 Foundation RPC 回环注册地址被拒绝")
	}
}

// TestLoadRejectsInvalidTimeout 验证解析失败的时长不会静默退回无界行为。
func TestLoadRejectsInvalidTimeout(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_HTTP_READ_TIMEOUT", "not-a-duration")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法 HTTP 时长被拒绝")
	}
}

func TestLoadRejectsUnsafeAuthCookieAndSecretConfig(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_AUTH_COOKIE_SAME_SITE", "none")
	t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "false")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望 SameSite=None 且 Secure=false 被拒绝")
	}

	t.Setenv("BUSINESS_AUTH_COOKIE_SAME_SITE", "lax")
	t.Setenv("BUSINESS_AUTH_CSRF_SECRET_BASE64", "dG9vLXNob3J0")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望过短 CSRF HMAC 密钥被拒绝")
	}
}

func TestLoadRejectsAuthIdleTTLBeyondAbsoluteTTL(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_AUTH_SESSION_IDLE_TTL", "48h")
	t.Setenv("BUSINESS_AUTH_SESSION_ABSOLUTE_TTL", "24h")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望空闲有效期超过绝对有效期被拒绝")
	}
}

func TestLoadRejectsUnsafeProjectProtectionAndLeaseConfig(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("BUSINESS_PROJECT_PROMPT_KEY_BASE64", "dG9vLXNob3J0")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非 32 字节 Project Prompt AES 密钥被拒绝")
	}

	t.Setenv("BUSINESS_PROJECT_PROMPT_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "dG9vLXNob3J0")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非 32 字节 Agent Session RPC HMAC 密钥被拒绝")
	}

	t.Setenv("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=")
	t.Setenv("BUSINESS_PROJECT_DISPATCH_LEASE_DURATION", "9s")
	t.Setenv("BUSINESS_AGENT_RPC_REQUEST_TIMEOUT", "3s")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望不能覆盖 Unknown Outcome 最坏路径的派发租约被拒绝")
	}
}

func TestLoadRejectsUnsafeAgentHTTPConfig(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")

	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082/path")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望带业务 Path 的 Agent HTTP Base URL 被拒绝")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64", "dG9vLXNob3J0")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望非 32 字节 Agent HTTP HMAC 密钥被拒绝")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION", "Uppercase-v1")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望与 Agent Verifier 不一致的非小写 kid 被拒绝")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION", "agent-http-test-v1")
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_TTL", "61s")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望超过协议硬上限的 Agent HTTP 断言 TTL 被拒绝")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_ASSERTION_TTL", "30s")
	t.Setenv("DORA_ENV", "production")
	t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("期望生产环境拒绝明文 Agent HTTP Base URL")
	}
}
