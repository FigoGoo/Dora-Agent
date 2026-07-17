// Package planstoryboardruntime 实现 plan_storyboard.runtime.v2preview1 的本地可恢复执行核心。
package planstoryboardruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
)

const (
	// Profile 是本地 Storyboard Preview Runtime 的唯一批准标识。
	Profile = "plan_storyboard.runtime.v2preview1"
	// SourceType 是专用 Session Input 来源，不得伪装成 user_message。
	SourceType = "plan_storyboard_preview"
	// ToolRegistryRef 是恰好包含 plan_storyboard 的可执行 Registry 引用。
	ToolRegistryRef = "plan_storyboard.preview_tools@v1"
	// ToolDefinitionRef 是冻结的开发预览 Tool Definition 引用。
	ToolDefinitionRef = "plan_storyboard.v2preview1"
	// PromptRef 是 Graph 内部主 Prompt 引用。
	PromptRef = "graph_tool.plan_storyboard.preview.v1"
	// ValidatorRef 是 Graph 候选 Validator 引用。
	ValidatorRef = "plan_storyboard.preview.validator.v1"
	// DAGValidatorRef 是 Graph 依赖图 Validator 引用。
	DAGValidatorRef = "plan_storyboard.preview.dag-validator.v1"
	// RouterModelRouteRef 是外层唯一 ToolCall 本地模型路由。
	RouterModelRouteRef = "local.fake.plan_storyboard.router@v1"
	// PlanningModelRouteRef 是 Graph 内候选生成本地模型路由。
	PlanningModelRouteRef = "local.fake.plan_storyboard.planning@v1"
	// RuntimePolicyRef 是 receipt-first、ReturnDirectly 的运行策略引用。
	RuntimePolicyRef = "plan_storyboard.runtime_policy@v1"
	// BudgetRef 是一次 Router、一次 Tool、一次 Graph Model 的预算引用。
	BudgetRef = "plan_storyboard.local_preview_budget@v1"
	// BusinessResendLimit 是 prepared 命令在权威 not_found 后的最大自动重发次数。
	BusinessResendLimit = 1

	approvedToolDefinitionDigest = "c9160b4e45e67e18d4d0df926bf9c901780af73006ae5e9ac4c0705816d2c6a6"
	approvedPromptDigest         = "ad67c45314180374bf35b621786406ae237b0333c0bb12b63cd9706ae641d446"
	approvedValidatorDigest      = "b827fd37ffd3c9d069e20162d51c907f2dd902d029dcafed452905778d5f7be8"
	approvedDAGValidatorDigest   = "a59c4fd281970ddabbfb607fbae2e5e1baf6907e887a10124c2b7875639dc63c"

	toolRegistryCanonicalPrefix = `{"schema_version":"plan_storyboard.tool_registry.v1","tools":[{"tool_key":"plan_storyboard","definition":"plan_storyboard.v2preview1","definition_digest":"`
	toolRegistryCanonicalSuffix = `","intent_schema":"plan_storyboard.preview.intent.v1","result_schema":"plan_storyboard.preview.result.v1","return_directly":true}]}`
	runtimePolicyCanonical      = `{"schema_version":"plan_storyboard.runtime_policy.v1","profile":"plan_storyboard.runtime.v2preview1","source_type":"plan_storyboard_preview","local_only":true,"receipt_first":true,"return_directly":true}`
	routerRouteCanonical        = `{"schema_version":"plan_storyboard.model_route.v1","call_kind":"router","route":"local.fake.plan_storyboard.router@v1","provider":"local_fake"}`
	planningRouteCanonical      = `{"schema_version":"plan_storyboard.model_route.v1","call_kind":"graph_planning","route":"local.fake.plan_storyboard.planning@v1","provider":"local_fake"}`
	budgetCanonical             = `{"schema_version":"plan_storyboard.budget.v1","max_router_model_calls":1,"max_tool_calls":1,"max_graph_model_calls":1,"business_resend_limit":1,"retry":0,"failover":0}`
)

// ProfilePins 是入队时必须逐项冻结且启动时 exact-match 的不可变引用与摘要。
type ProfilePins struct {
	// ToolRegistryRef 是单 Tool Registry 引用。
	ToolRegistryRef string
	// ToolRegistryDigest 是单 Tool Registry 摘要。
	ToolRegistryDigest string
	// ToolDefinitionRef 是 Tool Definition 引用。
	ToolDefinitionRef string
	// ToolDefinitionDigest 是 Tool Definition 摘要。
	ToolDefinitionDigest string
	// PromptRef 是 Graph Prompt 引用。
	PromptRef string
	// PromptDigest 是 Graph Prompt 摘要。
	PromptDigest string
	// ValidatorRef 是 Candidate Validator 引用。
	ValidatorRef string
	// ValidatorDigest 是 Candidate Validator 摘要。
	ValidatorDigest string
	// DAGValidatorRef 是 DAG Validator 引用。
	DAGValidatorRef string
	// DAGValidatorDigest 是 DAG Validator 摘要。
	DAGValidatorDigest string
	// RouterModelRouteRef 是 Router Fake Model 路由引用。
	RouterModelRouteRef string
	// RouterModelRouteDigest 是 Router Fake Model 路由摘要。
	RouterModelRouteDigest string
	// PlanningModelRouteRef 是 Graph Planning Fake Model 路由引用。
	PlanningModelRouteRef string
	// PlanningModelRouteDigest 是 Graph Planning Fake Model 路由摘要。
	PlanningModelRouteDigest string
	// RuntimePolicyRef 是 Runtime 策略引用。
	RuntimePolicyRef string
	// RuntimePolicyDigest 是 Runtime 策略摘要。
	RuntimePolicyDigest string
	// BudgetRef 是本地固定预算引用。
	BudgetRef string
	// BudgetDigest 是本地固定预算摘要。
	BudgetDigest string
}

// ApprovedPins 返回由代码常量唯一导出的开发预览 pins。
func ApprovedPins() ProfilePins {
	return ProfilePins{
		ToolRegistryRef: ToolRegistryRef, ToolRegistryDigest: digestText(toolRegistryCanonicalPrefix + approvedToolDefinitionDigest + toolRegistryCanonicalSuffix),
		ToolDefinitionRef: ToolDefinitionRef, ToolDefinitionDigest: approvedToolDefinitionDigest,
		PromptRef: PromptRef, PromptDigest: approvedPromptDigest,
		ValidatorRef: ValidatorRef, ValidatorDigest: approvedValidatorDigest,
		DAGValidatorRef: DAGValidatorRef, DAGValidatorDigest: approvedDAGValidatorDigest,
		RouterModelRouteRef: RouterModelRouteRef, RouterModelRouteDigest: digestText(routerRouteCanonical),
		PlanningModelRouteRef: PlanningModelRouteRef, PlanningModelRouteDigest: digestText(planningRouteCanonical),
		RuntimePolicyRef: RuntimePolicyRef, RuntimePolicyDigest: digestText(runtimePolicyCanonical),
		BudgetRef: BudgetRef, BudgetDigest: digestText(budgetCanonical),
	}
}

// ValidateApprovedArtifacts 在启动期把当前真实工件与独立审批锚点逐项 exact-match。
// 修改工件时必须显式升级版本并更新审批摘要，不能在同 Profile 下自动批准漂移。
func ValidateApprovedArtifacts() error {
	if ToolDefinitionRef != planstoryboard.ToolDefinitionVersion || PromptRef != planstoryboard.PromptVersion ||
		ValidatorRef != planstoryboard.ValidatorVersion || DAGValidatorRef != planstoryboard.DAGValidatorVersion {
		return fmt.Errorf("validate plan storyboard approved artifacts: version reference mismatch")
	}
	checks := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "tool definition", actual: planstoryboard.ToolDefinitionDigest(), expected: approvedToolDefinitionDigest},
		{name: "prompt", actual: planstoryboard.PromptArtifactDigest(), expected: approvedPromptDigest},
		{name: "validator", actual: planstoryboard.ValidatorArtifactDigest(), expected: approvedValidatorDigest},
		{name: "DAG validator", actual: planstoryboard.DAGValidatorArtifactDigest(), expected: approvedDAGValidatorDigest},
	}
	for _, check := range checks {
		if check.actual != check.expected {
			return fmt.Errorf("validate plan storyboard approved artifacts: %s digest mismatch", check.name)
		}
	}
	return nil
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
