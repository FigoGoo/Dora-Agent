package config

import "testing"

// TestLoadValidConfig 验证完整本地配置可以通过启动校验。
func TestLoadValidConfig(t *testing.T) {
	setValidAgentConfig(t)

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载合法配置失败: %v", err)
	}
	if cfg.Service.Name != serviceName {
		t.Fatalf("服务名不一致: got %q", cfg.Service.Name)
	}
}

// TestLoadRejectsLoopbackAdvertisedAddress 验证服务注册不会发布回环地址。
func TestLoadRejectsLoopbackAdvertisedAddress(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "localhost:18082")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望回环注册地址被拒绝")
	}
}

// TestLoadRejectsInvalidTimeout 验证解析失败的时长不会静默退回无界行为。
func TestLoadRejectsInvalidTimeout(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_HTTP_READ_TIMEOUT", "invalid")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法 HTTP 时长被拒绝")
	}
}

// TestLoadRejectsInvalidBusinessRPCBudget 验证单次 RPC 超时不能超过 Agent 启动总预算。
func TestLoadRejectsInvalidBusinessRPCBudget(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_BUSINESS_RPC_REQUEST_TIMEOUT", "20s")
	t.Setenv("AGENT_BUSINESS_RPC_STARTUP_TIMEOUT", "10s")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法 Business RPC 启动预算被拒绝")
	}
}

// TestLoadRejectsInvalidContentKey 验证 Base64 非法或解码后不是 32 字节时启动失败且错误不回显 Secret。
func TestLoadRejectsInvalidContentKey(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_CONTENT_KEY_BASE64", "not-a-base64-secret")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法 Session 内容密钥被拒绝")
	}
}

// TestLoadRejectsInvalidSessionRPCAuthSecret 验证服务认证 Secret 缺失、非法或长度不符时失败关闭。
func TestLoadRejectsInvalidSessionRPCAuthSecret(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "not-a-base64-secret")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望非法 Session RPC 服务认证 Secret 被拒绝")
	}
}

// TestLoadRejectsOversizedContentKeyVersion 验证持久化 key_version 在启动阶段遵守 varchar(64) 边界。
func TestLoadRejectsOversizedContentKeyVersion(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_CONTENT_KEY_VERSION", "01234567890123456789012345678901234567890123456789012345678901234")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望超过 64 字节的内容密钥版本被拒绝")
	}
}

// TestLoadRejectsLoopbackRPCAdvertisedAddress 验证 Session RPC 不会向 etcd 发布回环地址。
func TestLoadRejectsLoopbackRPCAdvertisedAddress(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")

	if _, err := Load("test"); err == nil {
		t.Fatal("期望 Session RPC 回环注册地址被拒绝")
	}
}

// TestLoadAcceptsPairedPreviousKeys 验证身份与内容 previous key 只有成对提供且版本互异时进入轮换窗口。
func TestLoadAcceptsPairedPreviousKeys(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_CONTENT_PREVIOUS_KEY_VERSION", "content-previous-v0")
	t.Setenv("AGENT_CONTENT_PREVIOUS_KEY_BASE64", "YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk=")
	t.Setenv("AGENT_HTTP_ASSERTION_PREVIOUS_KEY_VERSION", "test-2026-06-z")
	t.Setenv("AGENT_HTTP_ASSERTION_PREVIOUS_SECRET_BASE64", "YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk=")

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("成对 previous key 未通过: %v", err)
	}
	if len(cfg.ContentProtection.PreviousKey) != 32 || len(cfg.HTTPIdentity.PreviousSecret) != 32 {
		t.Fatalf("previous key 未冻结到配置")
	}
}

// TestLoadRejectsHalfPreviousKeyPair 验证任一 previous 版本与 Secret 缺半都失败关闭。
func TestLoadRejectsHalfPreviousKeyPair(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_HTTP_ASSERTION_PREVIOUS_KEY_VERSION", "test-2026-06-z")
	if _, err := Load("test"); err == nil {
		t.Fatal("缺少 previous assertion secret 被接受")
	}
}

// TestLoadRejectsUnsafeWorkspaceAndSSELimits 验证 Snapshot 集合与逐帧超时不能突破 transport 安全边界。
func TestLoadRejectsUnsafeWorkspaceAndSSELimits(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("AGENT_WORKSPACE_MAX_MESSAGES", "101")
	if _, err := Load("test"); err == nil {
		t.Fatal("过大 Workspace Message 上限被接受")
	}

	setValidAgentConfig(t)
	t.Setenv("AGENT_WORKSPACE_MAX_MESSAGES", "100")
	t.Setenv("AGENT_SSE_HEARTBEAT_INTERVAL", "5s")
	t.Setenv("AGENT_SSE_FRAME_WRITE_TIMEOUT", "5s")
	if _, err := Load("test"); err == nil {
		t.Fatal("Frame Timeout 不小于 Heartbeat 被接受")
	}
}

// setValidAgentConfig 写入不访问网络的完整测试配置；密钥仅是测试 Fixture，不进入日志或生产模板。
func setValidAgentConfig(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_INSTANCE_ID", "agent-test-1")
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "host.docker.internal:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19082")
	t.Setenv("AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("AGENT_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/agent?sslmode=disable")
	t.Setenv("AGENT_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("AGENT_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("AGENT_CONTENT_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("AGENT_CONTENT_KEY_VERSION", "test-key-v1")
	t.Setenv("AGENT_HTTP_ASSERTION_ACTIVE_KEY_VERSION", "test-2026-07-a")
	t.Setenv("AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
}
