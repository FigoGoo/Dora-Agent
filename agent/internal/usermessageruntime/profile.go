package usermessageruntime

import "github.com/FigoGoo/Dora-Agent/agent/internal/session"

const (
	// PromptRef 是固定安全响应 Prompt 的唯一版本引用。
	PromptRef = "user_message.direct_response@v1"
	// RuntimePolicyRef 是方案 A 无 Tool、无 Assistant History 策略引用。
	RuntimePolicyRef = "user_message.runtime_policy@v1"
	// BudgetRef 是单模型调用与 4KiB 输出上限的预算引用。
	BudgetRef = "user_message.single_model_call@v1"

	// 下列 canonical JSON 不经过 map 编码；字段顺序与字节内容是摘要契约的一部分。
	PromptCanonical              = `{"schema_version":"user_message.direct_response.prompt.v1","behavior":"fixed_safe_card","copy_user_message":false,"executable_tools":[]}`
	EmptyToolRegistryCanonical   = `{"schema_version":"user_message.tool_registry.v1","tools":[]}`
	RuntimePolicyCanonical       = `{"schema_version":"user_message.runtime_policy.v1","profile":"user_message.runtime.v2preview1","max_model_calls":1,"assistant_history":false}`
	LocalFakeModelRouteCanonical = `{"schema_version":"user_message.model_route.v1","route":"local.fake.user_message@v1","provider":"local_fake"}`
	BudgetCanonical              = `{"schema_version":"user_message.budget.v1","max_model_calls":1,"max_output_bytes":4096}`

	PromptDigest              = "328f92af1a3286d3c4e93670755cddf3b43d00c8899f062ef0ef9152e9235327"
	EmptyToolRegistryDigest   = "301a72a81332c6cc041e9778a354edb9d35e60578ae55620c2d9e5dfc5774068"
	RuntimePolicyDigest       = "4a1522948a9fd9a8e3fe1c2c1237feb7656792a2f5d3d9f6eb5b377878cbd49d"
	LocalFakeModelRouteDigest = "081cf3b399166b5fdb2e933dda07824b4cb20907335e303ba0852bf54ae0daef"
	BudgetDigest              = "bbd407f0179b4c38cc81024de32c632ee01e95b83b4745d357a798e4890882e0"
)

// ApprovedSessionProfile 返回 Ensure/Bootstrap 共用的唯一批准 Profile。
// 调用方若要保持 Writer 关闭，只能把返回值的 Enabled 显式改为 false；其余 pins 不得按环境漂移。
func ApprovedSessionProfile() session.UserMessageRuntimeProfile {
	return session.UserMessageRuntimeProfile{
		Enabled: true, Profile: Profile, ContextSchema: session.UserMessageContextSchemaV2Preview1,
		PromptRef: PromptRef, PromptDigest: PromptDigest,
		ToolRegistryRef: EmptyToolRegistryRef, ToolRegistryDigest: EmptyToolRegistryDigest,
		RuntimePolicyRef: RuntimePolicyRef, RuntimePolicyDigest: RuntimePolicyDigest,
		ModelRouteRef: LocalFakeModelRouteRef, ModelRouteDigest: LocalFakeModelRouteDigest,
		BudgetRef: BudgetRef, BudgetDigest: BudgetDigest,
	}
}
