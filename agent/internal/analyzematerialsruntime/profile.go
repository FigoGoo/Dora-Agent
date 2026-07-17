// Package analyzematerialsruntime 实现 analyze_materials.runtime.v2preview1 的本地可恢复执行核心。
package analyzematerialsruntime

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	// Profile 是本地素材分析 Runtime 的唯一批准标识。
	Profile = "analyze_materials.runtime.v2preview1"
	// SourceType 是专用 Session Input 来源，不得伪装成 user_message。
	SourceType = "analyze_materials_preview"
	// ToolRegistryRef 是恰好包含 analyze_materials 的可执行 Registry 引用。
	ToolRegistryRef = "analyze_materials.preview_tools@v1"
	// ToolDefinitionRef 是冻结的开发预览 Tool Definition 引用。
	ToolDefinitionRef = "analyze_materials.v2preview1"
	// PromptRef 是 Graph 内部主 Prompt 引用。
	PromptRef = "graph_tool.analyze_materials.preview.v1"
	// ValidatorRef 是 Graph 候选 Validator 引用。
	ValidatorRef = "analyze_materials.preview.validator.v1"
	// EvidencePolicyRef 是 text/image Evidence Policy 引用。
	EvidencePolicyRef = "analyze_materials.preview.evidence-policy.v1"
	// RouterModelRouteRef 是外层唯一 ToolCall 本地模型路由。
	RouterModelRouteRef = "local.fake.analyze_materials.router@v1"
	// AnalysisModelRouteRef 是 Graph 内候选生成本地模型路由。
	AnalysisModelRouteRef = "local.fake.analyze_materials.analysis@v1"
	// RuntimePolicyRef 是 receipt-first、ReturnDirectly 的运行策略引用。
	RuntimePolicyRef = "analyze_materials.runtime_policy@v1"
	// BudgetRef 是一次 Router、一次 Tool、一次 Graph Model 的预算引用。
	BudgetRef = "analyze_materials.local_preview_budget@v1"

	toolRegistryCanonical  = `{"schema_version":"analyze_materials.tool_registry.v1","tools":[{"tool_key":"analyze_materials","definition":"analyze_materials.v2preview1","intent_schema":"analyze_materials.preview.intent.v1","result_schema":"analyze_materials.preview.result.v1","return_directly":true}]}`
	runtimePolicyCanonical = `{"schema_version":"analyze_materials.runtime_policy.v1","profile":"analyze_materials.runtime.v2preview1","source_type":"analyze_materials_preview","local_only":true,"receipt_first":true,"return_directly":true}`
	routerRouteCanonical   = `{"schema_version":"analyze_materials.model_route.v1","call_kind":"router","route":"local.fake.analyze_materials.router@v1","provider":"local_fake"}`
	analysisRouteCanonical = `{"schema_version":"analyze_materials.model_route.v1","call_kind":"graph_analysis","route":"local.fake.analyze_materials.analysis@v1","provider":"local_fake"}`
	budgetCanonical        = `{"schema_version":"analyze_materials.budget.v1","max_router_model_calls":1,"max_tool_calls":1,"max_graph_model_calls":1,"retry":0,"failover":0}`
)

// ProfilePins 是入队时必须逐项冻结且启动时 exact-match 的不可变引用与摘要。
type ProfilePins struct {
	ToolRegistryRef          string
	ToolRegistryDigest       string
	ToolDefinitionRef        string
	ToolDefinitionDigest     string
	PromptRef                string
	PromptDigest             string
	ValidatorRef             string
	ValidatorDigest          string
	EvidencePolicyRef        string
	EvidencePolicyDigest     string
	RouterModelRouteRef      string
	RouterModelRouteDigest   string
	AnalysisModelRouteRef    string
	AnalysisModelRouteDigest string
	RuntimePolicyRef         string
	RuntimePolicyDigest      string
	BudgetRef                string
	BudgetDigest             string
}

// ApprovedPins 返回由代码常量唯一导出的开发预览 pins。
func ApprovedPins() ProfilePins {
	return ProfilePins{
		ToolRegistryRef: ToolRegistryRef, ToolRegistryDigest: digestText(toolRegistryCanonical),
		ToolDefinitionRef: ToolDefinitionRef, ToolDefinitionDigest: digestText(ToolDefinitionRef),
		PromptRef: PromptRef, PromptDigest: digestText(PromptRef),
		ValidatorRef: ValidatorRef, ValidatorDigest: digestText(ValidatorRef),
		EvidencePolicyRef: EvidencePolicyRef, EvidencePolicyDigest: digestText(EvidencePolicyRef),
		RouterModelRouteRef: RouterModelRouteRef, RouterModelRouteDigest: digestText(routerRouteCanonical),
		AnalysisModelRouteRef: AnalysisModelRouteRef, AnalysisModelRouteDigest: digestText(analysisRouteCanonical),
		RuntimePolicyRef: RuntimePolicyRef, RuntimePolicyDigest: digestText(runtimePolicyCanonical),
		BudgetRef: BudgetRef, BudgetDigest: digestText(budgetCanonical),
	}
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
