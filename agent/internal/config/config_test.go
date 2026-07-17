package config

import (
	"testing"
	"time"
)

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
	if cfg.PlanSpecPreviewEnabled {
		t.Fatal("CreationSpec Preview 必须默认关闭")
	}
	if cfg.UserMessageRuntimeEnabled {
		t.Fatal("User Message Runtime 必须默认关闭")
	}
	if cfg.AnalyzeMaterialsRuntimeEnabled || cfg.AnalyzeMaterialsRuntime.Enabled {
		t.Fatal("Analyze Materials Runtime 必须默认关闭")
	}
	if cfg.PlanStoryboardRuntimeEnabled || cfg.PlanStoryboardRuntime.Enabled {
		t.Fatal("Plan Storyboard Runtime 必须默认关闭")
	}
	if cfg.WritePromptsRuntimeEnabled || cfg.WritePromptsRuntime.Enabled {
		t.Fatal("Write Prompts Runtime 必须默认关闭")
	}
	if cfg.UserMessageRuntime.Profile != "user_message.runtime.v2preview1" ||
		cfg.UserMessageRuntime.ProcessorConcurrency != 2 ||
		cfg.UserMessageRuntime.MaxOutputBytes != 4*1024 {
		t.Fatalf("User Message Runtime 默认 Profile/预算漂移: %+v", cfg.UserMessageRuntime)
	}
	if cfg.AnalyzeMaterialsRuntime.Profile != "analyze_materials.runtime.v2preview1" ||
		cfg.AnalyzeMaterialsRuntime.ProcessorConcurrency != 2 ||
		cfg.AnalyzeMaterialsRuntime.PollInterval != 500*time.Millisecond ||
		cfg.AnalyzeMaterialsRuntime.LeaseDuration != 30*time.Second ||
		cfg.AnalyzeMaterialsRuntime.HeartbeatInterval != 10*time.Second ||
		cfg.AnalyzeMaterialsRuntime.RetryDelay != time.Second ||
		cfg.AnalyzeMaterialsRuntime.RecoveryDelay != 2*time.Second ||
		cfg.AnalyzeMaterialsRuntime.MaxAttempts != 5 ||
		cfg.AnalyzeMaterialsRuntime.MaxOutputBytes != 64*1024 {
		t.Fatalf("Analyze Materials Runtime 默认 Profile/预算漂移: %+v", cfg.AnalyzeMaterialsRuntime)
	}
	if cfg.PlanStoryboardRuntime.Profile != "plan_storyboard.runtime.v2preview1" ||
		cfg.PlanStoryboardRuntime.ProcessorConcurrency != 2 ||
		cfg.PlanStoryboardRuntime.PollInterval != 500*time.Millisecond ||
		cfg.PlanStoryboardRuntime.LeaseDuration != 30*time.Second ||
		cfg.PlanStoryboardRuntime.HeartbeatInterval != 10*time.Second ||
		cfg.PlanStoryboardRuntime.RetryDelay != time.Second ||
		cfg.PlanStoryboardRuntime.RecoveryDelay != 2*time.Second ||
		cfg.PlanStoryboardRuntime.MaxAttempts != 5 || cfg.PlanStoryboardRuntime.MaxOutputBytes != 64*1024 ||
		cfg.PlanStoryboardRuntime.MaxBusinessResends != 1 {
		t.Fatalf("Plan Storyboard Runtime 默认 Profile/预算漂移: %+v", cfg.PlanStoryboardRuntime)
	}
	if cfg.WritePromptsRuntime.Profile != "write_prompts.runtime.v2preview1" ||
		cfg.WritePromptsRuntime.ProcessorConcurrency != 2 ||
		cfg.WritePromptsRuntime.PollInterval != 500*time.Millisecond ||
		cfg.WritePromptsRuntime.LeaseDuration != 30*time.Second ||
		cfg.WritePromptsRuntime.HeartbeatInterval != 10*time.Second ||
		cfg.WritePromptsRuntime.RetryDelay != time.Second ||
		cfg.WritePromptsRuntime.RecoveryDelay != 2*time.Second ||
		cfg.WritePromptsRuntime.MaxAttempts != 5 || cfg.WritePromptsRuntime.MaxOutputBytes != 128*1024 ||
		cfg.WritePromptsRuntime.MaxTargets != 96 || cfg.WritePromptsRuntime.DefaultOutputLanguage != "zh-CN" ||
		cfg.WritePromptsRuntime.MaxBusinessResends != 1 {
		t.Fatalf("Write Prompts Runtime 默认 Profile/Policy/预算漂移: %+v", cfg.WritePromptsRuntime)
	}
	if cfg.PlanSpecPreviewRuntime.MaxIterations != 4 || cfg.PlanSpecPreviewRuntime.ProcessorConcurrency != 2 ||
		cfg.PlanSpecPreviewRuntime.PollInterval != 500*time.Millisecond ||
		cfg.PlanSpecPreviewRuntime.LeaseDuration != 30*time.Second ||
		cfg.PlanSpecPreviewRuntime.HeartbeatInterval != 10*time.Second ||
		cfg.PlanSpecPreviewRuntime.RetryDelay != time.Second ||
		cfg.PlanSpecPreviewRuntime.RecoveryDelay != 2*time.Second || cfg.PlanSpecPreviewRuntime.MaxAttempts != 5 ||
		cfg.PlanSpecPreviewRuntime.MaxBusinessResends != 3 {
		t.Fatalf("CreationSpec Preview 默认 Runtime 预算漂移: %+v", cfg.PlanSpecPreviewRuntime)
	}
	if cfg.SkillSnapshotLimits.ProfileVersion != "session_skill_snapshot_limits.v1" ||
		cfg.SkillSnapshotLimits.MaxItems != 16 || cfg.SkillSnapshotLimits.MaxPublicToolRefsPerItem != 0 {
		t.Fatalf("Skill Snapshot 默认 limits 漂移: %+v", cfg.SkillSnapshotLimits)
	}
}

// TestLoadMVPAllToolsRuntimeProfile 验证统一 Profile 是唯一激活真源，并精确派生五开两关能力集合。
func TestLoadMVPAllToolsRuntimeProfile(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
	t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("加载统一 Profile 失败: %v", err)
	}
	capabilities := cfg.EffectiveRuntimeCapabilities()
	if !cfg.MVPAllToolsRuntimeEnabled() || !capabilities.UserMessage || !capabilities.PlanCreationSpec ||
		!capabilities.AnalyzeMaterials || !capabilities.PlanStoryboard || !capabilities.WritePrompts ||
		capabilities.GenerateMedia || capabilities.AssembleOutput {
		t.Fatalf("统一 Profile capability 漂移: profile=%q capabilities=%+v", cfg.RuntimeProfile, capabilities)
	}
	if cfg.PlanSpecPreviewEnabled || cfg.UserMessageRuntimeEnabled || cfg.AnalyzeMaterialsRuntimeEnabled ||
		cfg.PlanStoryboardRuntimeEnabled || cfg.WritePromptsRuntimeEnabled {
		t.Fatalf("统一 Profile 不得改写遗留开关: %+v", cfg)
	}
}

// TestLoadMVPAllToolsRuntimeProfileFailsClosed 验证未知值、双真源、远程依赖和非本地环境均不能启动。
func TestLoadMVPAllToolsRuntimeProfileFailsClosed(t *testing.T) {
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "unknown profile", key: "DORA_AGENT_RUNTIME_PROFILE", value: "mvp_all_tools.runtime.v2"},
		{name: "legacy flag conflict", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", value: "true"},
		{name: "non local", key: "DORA_ENV", value: "staging"},
		{name: "wildcard HTTP", key: "AGENT_HTTP_ADDR", value: ":18082"},
		{name: "remote PostgreSQL", key: "AGENT_DATABASE_URL", value: "postgres://user:password@database:5432/agent?sslmode=disable"},
		{name: "remote Redis", key: "AGENT_REDIS_ADDR", value: "redis:6379"},
		{name: "remote etcd", key: "AGENT_ETCD_ENDPOINTS", value: "etcd:2379"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setValidAgentConfig(t)
			t.Setenv("DORA_AGENT_RUNTIME_PROFILE", MVPAllToolsRuntimeProfile)
			t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
			t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
			t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")
			t.Setenv(testCase.key, testCase.value)
			if _, err := Load("test"); err == nil {
				t.Fatalf("统一 Profile 非法配置被接受: %s=%s", testCase.key, testCase.value)
			}
		})
	}
}

// TestLoadWritePromptsRuntimeRequiresLocalExclusiveApprovedProfile 验证 Prompt Runtime 只能按批准 Policy 本地互斥启用。
func TestLoadWritePromptsRuntimeRequiresLocalExclusiveApprovedProfile(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED", "true")
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
	t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")
	cfg, err := Load("test")
	if err != nil || !cfg.WritePromptsRuntimeEnabled || !cfg.WritePromptsRuntime.Enabled {
		t.Fatalf("显式本地 Write Prompts Runtime 配置=%+v error=%v", cfg.WritePromptsRuntime, err)
	}

	for _, testCase := range []struct{ key, value string }{
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_PROFILE", value: "write_prompts.runtime.v3"},
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_MAX_OUTPUT_BYTES", value: "131071"},
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_MAX_TARGETS", value: "95"},
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_DEFAULT_OUTPUT_LANGUAGE", value: "en-US"},
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_MAX_BUSINESS_RESENDS", value: "0"},
		{key: "DORA_AGENT_WRITE_PROMPTS_RUNTIME_PROCESSOR_CONCURRENCY", value: "33"},
		{key: "AGENT_SSE_MAX_EVENT_BYTES", value: "131072"},
		{key: "AGENT_HTTP_ADDR", value: ":18082"},
		{key: "AGENT_DATABASE_URL", value: "postgres://user:password@database:5432/agent?sslmode=disable"},
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED", "true")
		t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
		t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
		t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")
		t.Setenv(testCase.key, testCase.value)
		if _, err := Load("test"); err == nil {
			t.Fatalf("非法 Write Prompts Runtime 配置被接受: %s=%s", testCase.key, testCase.value)
		}
	}

	for _, conflict := range []string{
		"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED",
		"DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED",
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED", "true")
		t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
		t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
		t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")
		t.Setenv(conflict, "true")
		if _, err := Load("test"); err == nil {
			t.Fatalf("Write Prompts Runtime 与 %s 同时启用被接受", conflict)
		}
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_WRITE_PROMPTS_RUNTIME_ENABLED", "true")
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
	t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "262144")
	t.Setenv("DORA_ENV", "production")
	if _, err := Load("test"); err == nil {
		t.Fatal("production 环境启用 Write Prompts Runtime 被接受")
	}
}

// TestLoadPlanStoryboardRuntimeRequiresLocalExclusiveApprovedProfile 验证 Storyboard Runtime 只按冻结 Profile 在本地互斥启用。
func TestLoadPlanStoryboardRuntimeRequiresLocalExclusiveApprovedProfile(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "true")
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
	cfg, err := Load("test")
	if err != nil || !cfg.PlanStoryboardRuntimeEnabled || !cfg.PlanStoryboardRuntime.Enabled {
		t.Fatalf("显式本地 Plan Storyboard Runtime 配置=%+v error=%v", cfg.PlanStoryboardRuntime, err)
	}

	for _, testCase := range []struct{ key, value string }{
		{key: "DORA_AGENT_PLAN_STORYBOARD_RUNTIME_PROFILE", value: "plan_storyboard.runtime.v3"},
		{key: "DORA_AGENT_PLAN_STORYBOARD_RUNTIME_MAX_OUTPUT_BYTES", value: "65535"},
		{key: "DORA_AGENT_PLAN_STORYBOARD_RUNTIME_MAX_BUSINESS_RESENDS", value: "2"},
		{key: "DORA_AGENT_PLAN_STORYBOARD_RUNTIME_PROCESSOR_CONCURRENCY", value: "33"},
		{key: "AGENT_SSE_MAX_EVENT_BYTES", value: "65536"},
		{key: "AGENT_HTTP_ADDR", value: ":18082"},
		{key: "AGENT_RPC_LISTEN_ADDR", value: "0.0.0.0:19082"},
		{key: "AGENT_DATABASE_URL", value: "postgres://user:password@database:5432/agent?sslmode=disable"},
		{key: "AGENT_REDIS_ADDR", value: "redis:6379"},
		{key: "AGENT_ETCD_ENDPOINTS", value: "etcd:2379"},
		{key: "AGENT_ADVERTISED_ADDRESS", value: "127.0.0.1:18083"},
		{key: "AGENT_RPC_ADVERTISED_ADDRESS", value: "127.0.0.1:19083"},
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "true")
		t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
		t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
		t.Setenv(testCase.key, testCase.value)
		if _, err := Load("test"); err == nil {
			t.Fatalf("非法 Plan Storyboard Runtime 配置被接受: %s=%s", testCase.key, testCase.value)
		}
	}

	for _, conflict := range []string{
		"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED",
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "true")
		t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
		t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
		t.Setenv(conflict, "true")
		if _, err := Load("test"); err == nil {
			t.Fatalf("Plan Storyboard Runtime 与 %s 同时启用被接受", conflict)
		}
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "true")
	t.Setenv("AGENT_ADVERTISED_ADDRESS", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_ADVERTISED_ADDRESS", "127.0.0.1:19082")
	t.Setenv("DORA_ENV", "production")
	if _, err := Load("test"); err == nil {
		t.Fatal("production 环境启用 Plan Storyboard Runtime 被接受")
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Plan Storyboard Runtime boolean 被接受")
	}
}

// TestLoadAnalyzeMaterialsRuntimeRequiresLocalExclusiveApprovedProfile 验证素材分析 Runtime 只能本地、单独且按冻结预算启用。
func TestLoadAnalyzeMaterialsRuntimeRequiresLocalExclusiveApprovedProfile(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.AnalyzeMaterialsRuntimeEnabled || !cfg.AnalyzeMaterialsRuntime.Enabled {
		t.Fatalf("显式本地 Analyze Materials Runtime 配置=%+v error=%v", cfg.AnalyzeMaterialsRuntime, err)
	}

	for _, testCase := range []struct{ key, value string }{
		{key: "DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE", value: "analyze_materials.runtime.v3"},
		{key: "DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_MAX_OUTPUT_BYTES", value: "65535"},
		{key: "DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROCESSOR_CONCURRENCY", value: "33"},
		{key: "DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_HEARTBEAT_INTERVAL", value: "30s"},
		{key: "AGENT_SSE_MAX_EVENT_BYTES", value: "65536"},
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
		t.Setenv(testCase.key, testCase.value)
		if _, err := Load("test"); err == nil {
			t.Fatalf("非法 Analyze Materials Runtime 配置被接受: %s=%s", testCase.key, testCase.value)
		}
	}

	for _, conflict := range []string{"DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED"} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
		t.Setenv(conflict, "true")
		if _, err := Load("test"); err == nil {
			t.Fatalf("Analyze Materials Runtime 与 %s 同时启用被接受", conflict)
		}
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "true")
	t.Setenv("DORA_ENV", "production")
	if _, err := Load("test"); err == nil {
		t.Fatal("production 环境启用 Analyze Materials Runtime 被接受")
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Analyze Materials Runtime boolean 被接受")
	}
}

// TestLoadUserMessageRuntimeRequiresLocalExclusiveApprovedProfile 验证方案 A 只能本地、单 Processor、固定 Profile 启用。
func TestLoadUserMessageRuntimeRequiresLocalExclusiveApprovedProfile(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.UserMessageRuntimeEnabled {
		t.Fatalf("显式本地 User Message Runtime 配置=%+v error=%v", cfg.UserMessageRuntimeEnabled, err)
	}

	for _, testCase := range []struct{ key, value string }{
		{key: "DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE", value: "user_message.runtime.v3"},
		{key: "DORA_AGENT_USER_MESSAGE_MAX_OUTPUT_BYTES", value: "4097"},
	} {
		setValidAgentConfig(t)
		t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "true")
		t.Setenv(testCase.key, testCase.value)
		if _, err := Load("test"); err == nil {
			t.Fatalf("非法 User Message Runtime 配置被接受: %s=%s", testCase.key, testCase.value)
		}
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "true")
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	if _, err := Load("test"); err == nil {
		t.Fatal("两个 source-filtered Processor 同时启用被接受")
	}

	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "true")
	t.Setenv("DORA_ENV", "production")
	if _, err := Load("test"); err == nil {
		t.Fatal("production 环境启用 User Message Runtime 被接受")
	}
}

// TestLoadRejectsInvalidCreationSpecPreviewRuntimeBudget 验证 Preview 关闭时也不接受无界或自相矛盾的预算。
func TestLoadRejectsInvalidCreationSpecPreviewRuntimeBudget(t *testing.T) {
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "iterations", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_MAX_ITERATIONS", value: "1"},
		{name: "concurrency", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_PROCESSOR_CONCURRENCY", value: "33"},
		{name: "poll", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_POLL_INTERVAL", value: "1ms"},
		{name: "lease", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_LEASE_DURATION", value: "500ms"},
		{name: "heartbeat", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_HEARTBEAT_INTERVAL", value: "30s"},
		{name: "attempts", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_MAX_ATTEMPTS", value: "101"},
		{name: "business resends", key: "DORA_AGENT_PLAN_SPEC_PREVIEW_MAX_BUSINESS_RESENDS", value: "21"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setValidAgentConfig(t)
			t.Setenv(testCase.key, testCase.value)
			if _, err := Load("test"); err == nil {
				t.Fatalf("非法 Preview Runtime 预算被接受: %s=%s", testCase.key, testCase.value)
			}
		})
	}
}

// TestLoadCreationSpecPreviewRequiresExplicitDevelopmentFlag 验证 Agent Preview 默认关闭、显式本地开启及生产禁用。
func TestLoadCreationSpecPreviewRequiresExplicitDevelopmentFlag(t *testing.T) {
	setValidAgentConfig(t)
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	cfg, err := Load("test")
	if err != nil || !cfg.PlanSpecPreviewEnabled {
		t.Fatalf("显式本地 Preview 配置=%+v error=%v", cfg.PlanSpecPreviewEnabled, err)
	}

	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("非法 Preview boolean 被接受")
	}

	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "true")
	t.Setenv("DORA_ENV", "production")
	if _, err := Load("test"); err == nil {
		t.Fatal("production 环境启用 CreationSpec Preview 被接受")
	}
	for _, environment := range []string{"staging", "shared", "prod"} {
		t.Setenv("DORA_ENV", environment)
		if _, err := Load("test"); err == nil {
			t.Fatalf("%s 环境启用 CreationSpec Preview 被接受", environment)
		}
	}
}

// TestLoadRejectsUnsafeSkillSnapshotLimits 验证配置不能突破协议 ceiling、开启 Public Tool 或使用未知 profile。
func TestLoadRejectsUnsafeSkillSnapshotLimits(t *testing.T) {
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "unknown profile", key: "AGENT_SKILL_SNAPSHOT_LIMITS_PROFILE_VERSION", value: "session_skill_snapshot_limits.v2"},
		{name: "items over ceiling", key: "AGENT_SKILL_SNAPSHOT_MAX_ITEMS", value: "33"},
		{name: "public tools enabled", key: "AGENT_SKILL_SNAPSHOT_MAX_PUBLIC_TOOL_REFS_PER_ITEM", value: "1"},
		{name: "RPC over ceiling", key: "AGENT_SKILL_SNAPSHOT_MAX_RPC_REQUEST_BYTES", value: "4194305"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setValidAgentConfig(t)
			t.Setenv(testCase.key, testCase.value)
			if _, err := Load("test"); err == nil {
				t.Fatalf("非法 Skill Snapshot limit 被接受: %s=%s", testCase.key, testCase.value)
			}
		})
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
	t.Setenv("AGENT_HTTP_ADDR", "127.0.0.1:18082")
	t.Setenv("AGENT_RPC_LISTEN_ADDR", "127.0.0.1:19082")
	t.Setenv("AGENT_SESSION_RPC_AUTH_SECRET_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("AGENT_DATABASE_URL", "postgres://user:password@127.0.0.1:5432/agent?sslmode=disable")
	t.Setenv("AGENT_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("AGENT_ETCD_ENDPOINTS", "127.0.0.1:2379")
	t.Setenv("AGENT_CONTENT_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("AGENT_CONTENT_KEY_VERSION", "test-key-v1")
	t.Setenv("AGENT_HTTP_ASSERTION_ACTIVE_KEY_VERSION", "test-2026-07-a")
	t.Setenv("AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("DORA_AGENT_RUNTIME_PROFILE", "")
	t.Setenv("DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED", "false")
	t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED", "false")
	t.Setenv("DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE", "user_message.runtime.v2preview1")
	t.Setenv("DORA_AGENT_USER_MESSAGE_MAX_OUTPUT_BYTES", "4096")
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED", "false")
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE", "analyze_materials.runtime.v2preview1")
	t.Setenv("DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_MAX_OUTPUT_BYTES", "65536")
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_ENABLED", "false")
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_PROFILE", "plan_storyboard.runtime.v2preview1")
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_MAX_OUTPUT_BYTES", "65536")
	t.Setenv("DORA_AGENT_PLAN_STORYBOARD_RUNTIME_MAX_BUSINESS_RESENDS", "1")
	t.Setenv("AGENT_SSE_MAX_EVENT_BYTES", "131072")
}
