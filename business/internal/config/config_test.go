package config

import (
	"os"
	"path/filepath"
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

// TestLoadMediaRuntimeRequiresUnifiedProfileLoopbackAndSafeRoot 验证媒体 Profile 依附基础 Profile 且对象根失败关闭。
func TestLoadMediaRuntimeRequiresUnifiedProfileLoopbackAndSafeRoot(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-media-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19081")
	t.Setenv("BUSINESS_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_LISTEN_ADDR", "127.0.0.1:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)
	t.Setenv("DORA_BUSINESS_MEDIA_RUNTIME_PROFILE", MediaRuntimeProfile)
	t.Setenv("DORA_BUSINESS_MEDIA_OBJECT_ROOT", root)

	cfg, err := Load("test")
	if err != nil || !cfg.MediaRuntime.Enabled() || cfg.MediaRuntime.ObjectRoot != root {
		t.Fatalf("加载合法媒体 Runtime 配置=%+v error=%v", cfg.MediaRuntime, err)
	}
	for _, directory := range []string{"staging", "objects"} {
		if _, err := os.Stat(filepath.Join(root, directory)); !os.IsNotExist(err) {
			t.Fatalf("Config Load 不得创建对象子目录 %q: %v", directory, err)
		}
	}

	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", "")
	if _, err := Load("test"); err == nil {
		t.Fatal("媒体 Profile 未依附统一基础 Profile 时被接受")
	}
	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)
	t.Setenv("DORA_BUSINESS_MEDIA_RUNTIME_PROFILE", "media.runtime.v4")
	if _, err := Load("test"); err == nil {
		t.Fatal("未知媒体 Profile 被接受")
	}
	t.Setenv("DORA_BUSINESS_MEDIA_RUNTIME_PROFILE", MediaRuntimeProfile)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("test"); err == nil {
		t.Fatal("宽权限媒体对象根被接受")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(t.TempDir(), "media-root-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DORA_BUSINESS_MEDIA_OBJECT_ROOT", rootLink)
	if _, err := Load("test"); err == nil {
		t.Fatal("符号链接媒体对象根被接受")
	}
}

// TestLoadDisabledMediaRuntimeHasZeroObjectRootRequirement 验证空 Profile 不检查也不创建媒体对象根。
func TestLoadDisabledMediaRuntimeHasZeroObjectRootRequirement(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-no-media-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_MEDIA_OBJECT_ROOT", "/path/that/must/not/exist")
	cfg, err := Load("test")
	if err != nil || cfg.MediaRuntime.Enabled() {
		t.Fatalf("关闭媒体 Profile 时不应要求对象根: config=%+v error=%v", cfg.MediaRuntime, err)
	}
}

// TestLoadValidConfig 验证完整本地配置可以通过启动校验。
func TestLoadValidConfig(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_LISTEN_ADDR", "127.0.0.1:19081")
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
		cfg.AgentHTTP.AssertionKeyVersion != "agent-http-test-v1" || cfg.AgentHTTP.AssertionTTL != 30*time.Second ||
		cfg.AgentHTTP.PlanSpecPreviewEnabled || cfg.AgentHTTP.AnalyzeMaterialsRuntimeEnabled ||
		cfg.AgentHTTP.PlanStoryboardRuntimeEnabled || cfg.AgentHTTP.WritePromptsRuntimeEnabled ||
		cfg.AgentHTTP.WritePromptsRuntimeProfile != "write_prompts.runtime.v2preview1" ||
		cfg.AgentHTTP.PreviewMaxRequestBodyBytes != 16384 {
		t.Fatalf("Agent HTTP 默认值不一致: base_url=%q timeout=%s kid=%q ttl=%s",
			cfg.AgentHTTP.BaseURL, cfg.AgentHTTP.RequestTimeout, cfg.AgentHTTP.AssertionKeyVersion, cfg.AgentHTTP.AssertionTTL)
	}
	if cfg.AssetAnalysisPreview.Enabled {
		t.Fatal("Asset Analysis Preview 默认门禁意外开启")
	}
	if cfg.RuntimeProfile != "" {
		t.Fatalf("统一 Runtime Profile 默认必须关闭: %q", cfg.RuntimeProfile)
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

// TestLoadMVPAllToolsRuntimeProfile 验证统一 Profile 精确派生全部已实现能力，并拒绝未知、冲突与非本地配置。
func TestLoadMVPAllToolsRuntimeProfile(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-mvp-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19081")
	t.Setenv("BUSINESS_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_LISTEN_ADDR", "127.0.0.1:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载统一本地 Runtime Profile 失败: %v", err)
	}
	if cfg.RuntimeProfile != MVPAllToolsRuntimeProfile ||
		!cfg.AgentHTTP.PlanSpecPreviewEnabled || !cfg.AgentHTTP.AnalyzeMaterialsRuntimeEnabled ||
		!cfg.AgentHTTP.PlanStoryboardRuntimeEnabled || !cfg.AgentHTTP.WritePromptsRuntimeEnabled ||
		!cfg.AssetAnalysisPreview.Enabled {
		t.Fatalf("统一 Runtime Profile 能力派生不完整: %+v asset=%+v", cfg.AgentHTTP, cfg.AssetAnalysisPreview)
	}

	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("统一 Runtime Profile 与隔离开关同时启用被接受")
	}
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "false")
	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", "mvp_all_tools.runtime.v2")
	if _, err := Load("test"); err == nil {
		t.Fatal("未知统一 Runtime Profile 被接受")
	}
	t.Setenv("DORA_BUSINESS_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)
	t.Setenv("DORA_ENV", "staging")
	if _, err := Load("test"); err == nil {
		t.Fatal("非本地环境启用统一 Runtime Profile 被接受")
	}
}

// TestLoadPlanStoryboardRuntimeRequiresLocalExclusiveGate 验证 Storyboard Preview RPC/BFF 门禁默认关闭且只允许本地互斥开启。
func TestLoadPlanStoryboardRuntimeRequiresLocalExclusiveGate(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19081")
	t.Setenv("BUSINESS_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_LISTEN_ADDR", "127.0.0.1:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AgentHTTP.PlanStoryboardRuntimeEnabled {
		t.Fatalf("本地 Storyboard Runtime 配置=%+v error=%v", cfg.AgentHTTP, err)
	}

	for _, testCase := range []struct{ key, value string }{
		{key: "BUSINESS_HTTP_ADDR", value: ":18081"},
		{key: "BUSINESS_RPC_LISTEN_ADDR", value: "0.0.0.0:19081"},
		{key: "BUSINESS_AGENT_HTTP_BASE_URL", value: "http://agent.internal:18082"},
		{key: "BUSINESS_DATABASE_URL", value: "postgres://user:password@database:5432/business?sslmode=disable"},
		{key: "BUSINESS_REDIS_ADDR", value: "redis:6379"},
		{key: "BUSINESS_ETCD_ENDPOINTS", value: "etcd:2379"},
		{key: "BUSINESS_ADVERTISED_ADDRESS", value: "127.0.0.1:18082"},
		{key: "BUSINESS_RPC_ADVERTISED_ADDRESS", value: "127.0.0.1:19082"},
	} {
		t.Setenv(testCase.key, testCase.value)
		if _, err := Load("test"); err == nil {
			t.Fatalf("Storyboard Runtime 接受非 loopback 配置: %s=%s", testCase.key, testCase.value)
		}
		switch testCase.key {
		case "BUSINESS_HTTP_ADDR":
			t.Setenv(testCase.key, "127.0.0.1:18081")
		case "BUSINESS_RPC_LISTEN_ADDR":
			t.Setenv(testCase.key, "127.0.0.1:19081")
		case "BUSINESS_AGENT_HTTP_BASE_URL":
			t.Setenv(testCase.key, "http://127.0.0.1:18082")
		case "BUSINESS_DATABASE_URL":
			t.Setenv(testCase.key, "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
		case "BUSINESS_REDIS_ADDR":
			t.Setenv(testCase.key, "127.0.0.1:6379")
		case "BUSINESS_ETCD_ENDPOINTS":
			t.Setenv(testCase.key, "127.0.0.1:2379")
		case "BUSINESS_ADVERTISED_ADDRESS":
			t.Setenv(testCase.key, "127.0.0.1:18081")
		case "BUSINESS_RPC_ADVERTISED_ADDRESS":
			t.Setenv(testCase.key, "127.0.0.1:19081")
		}
	}

	for _, conflict := range []string{"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED", "DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED"} {
		t.Setenv(conflict, "true")
		if _, err := Load("test"); err == nil {
			t.Fatalf("Storyboard Runtime 与 %s 同时启用被接受", conflict)
		}
		t.Setenv(conflict, "false")
	}

	for _, environment := range []string{"test", "staging", "production"} {
		t.Setenv("DORA_ENV", environment)
		if environment == "production" {
			t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "true")
			t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "https://agent.internal")
		}
		if _, err := Load("test"); err == nil {
			t.Fatalf("%s 环境开启 Storyboard Runtime 被接受", environment)
		}
	}

	t.Setenv("DORA_ENV", "local")
	t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "false")
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Storyboard Runtime boolean 被接受")
	}
}

// TestLoadWritePromptsRuntimeRequiresExactLocalExclusiveProfile 验证 Prompt Preview 默认关闭，且只允许 exact Profile 在精确回环本地环境互斥开启。
func TestLoadWritePromptsRuntimeRequiresExactLocalExclusiveProfile(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19081")
	t.Setenv("BUSINESS_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("BUSINESS_RPC_LISTEN_ADDR", "127.0.0.1:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AgentHTTP.WritePromptsRuntimeEnabled ||
		cfg.AgentHTTP.WritePromptsRuntimeProfile != "write_prompts.runtime.v2preview1" {
		t.Fatalf("本地 Prompt Runtime 配置=%+v error=%v", cfg.AgentHTTP, err)
	}

	t.Setenv("DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_PROFILE", "write_prompts.runtime.v2")
	if _, err := Load("test"); err == nil {
		t.Fatal("未知 Prompt Runtime Profile 被接受")
	}
	t.Setenv("DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_PROFILE", "write_prompts.runtime.v2preview1")
	for _, conflict := range []string{
		"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED", "DORA_BUSINESS_PLAN_STORYBOARD_RUNTIME_ENABLED", "DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED",
	} {
		t.Setenv(conflict, "true")
		if _, err := Load("test"); err == nil {
			t.Fatalf("Prompt Runtime 与 %s 同时启用被接受", conflict)
		}
		t.Setenv(conflict, "false")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://agent.internal:18082")
	if _, err := Load("test"); err == nil {
		t.Fatal("Prompt Runtime 接受非回环 Agent HTTP")
	}
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("DORA_ENV", "staging")
	if _, err := Load("test"); err == nil {
		t.Fatal("staging 环境开启 Prompt Runtime 被接受")
	}
	t.Setenv("DORA_ENV", "local")
	t.Setenv("DORA_BUSINESS_WRITE_PROMPTS_RUNTIME_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Prompt Runtime boolean 被接受")
	}
}

// TestLoadAnalyzeMaterialsRuntimeRequiresLocalEvidenceGate 验证素材分析 Runtime 默认关闭、仅本地启用并依赖独立 Evidence Preview 门禁。
func TestLoadAnalyzeMaterialsRuntimeRequiresLocalEvidenceGate(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("缺少 Evidence Preview 门禁时素材分析 Runtime 被接受")
	}
	t.Setenv("DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AgentHTTP.AnalyzeMaterialsRuntimeEnabled {
		t.Fatalf("本地素材分析 Runtime 配置=%+v error=%v", cfg.AgentHTTP, err)
	}
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("两个 Preview 写 Runtime 同时启用被接受")
	}
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "false")
	t.Setenv("DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法素材分析 Runtime 布尔值被接受")
	}
	t.Setenv("DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
	for _, environment := range []string{"test", "staging", "production"} {
		t.Setenv("DORA_ENV", environment)
		if environment == "production" {
			t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "true")
			t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "https://agent.internal")
		}
		if _, err := Load("test"); err == nil {
			t.Fatalf("%s 环境开启素材分析 Runtime 被接受", environment)
		}
	}
}

// TestLoadAssetAnalysisPreviewLocalOnly 验证独立门禁默认关闭、只接受合法布尔值且仅 local 可开启。
func TestLoadAssetAnalysisPreviewLocalOnly(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AssetAnalysisPreview.Enabled {
		t.Fatalf("本地显式开启配置=%+v error=%v", cfg.AssetAnalysisPreview, err)
	}
	t.Setenv("DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Asset Analysis Preview 布尔值被接受")
	}
	t.Setenv("DORA_BUSINESS_ASSET_ANALYSIS_PREVIEW_ENABLED", "true")
	for _, environment := range []string{"test", "staging", "production"} {
		t.Setenv("DORA_ENV", environment)
		if environment == "production" {
			t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "true")
			t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "https://agent.internal")
		}
		if _, err := Load("test"); err == nil {
			t.Fatalf("%s 环境开启 Asset Analysis Preview 被接受", environment)
		}
	}
}

// TestLoadCreationSpecPreviewRequiresExplicitDevelopmentFlag 验证 Preview 默认关闭、显式本地开启及生产禁用。
func TestLoadCreationSpecPreviewRequiresExplicitDevelopmentFlag(t *testing.T) {
	setValidAuthSecret(t)
	t.Setenv("BUSINESS_INSTANCE_ID", "business-test-1")
	t.Setenv("BUSINESS_ADVERTISED_ADDRESS", "host.docker.internal:18081")
	t.Setenv("BUSINESS_RPC_ADVERTISED_ADDRESS", "host.docker.internal:19081")
	t.Setenv("BUSINESS_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/business?sslmode=disable")
	t.Setenv("BUSINESS_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("BUSINESS_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AgentHTTP.PlanSpecPreviewEnabled {
		t.Fatalf("explicit local Preview config=%+v error=%v", cfg.AgentHTTP, err)
	}
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("invalid Preview boolean was accepted")
	}
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	t.Setenv("DORA_ENV", "production")
	t.Setenv("BUSINESS_AUTH_COOKIE_SECURE", "true")
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "https://agent.internal")
	if _, err := Load("test"); err == nil {
		t.Fatal("production enabled CreationSpec Preview was accepted")
	}
	for _, environment := range []string{"staging", "shared", "prod"} {
		t.Setenv("DORA_ENV", environment)
		if _, err := Load("test"); err == nil {
			t.Fatalf("%s environment enabled CreationSpec Preview was accepted", environment)
		}
	}
	t.Setenv("DORA_ENV", "local")
	t.Setenv("BUSINESS_AGENT_HTTP_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("BUSINESS_AGENT_HTTP_PREVIEW_MAX_REQUEST_BODY_BYTES", "100")
	if _, err := Load("test"); err == nil {
		t.Fatal("unsafe Preview request body limit was accepted")
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
