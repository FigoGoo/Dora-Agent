// Package writepromptsruntime 实现 write_prompts.runtime.v2preview1 的本地可恢复执行核心。
package writepromptsruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
)

const (
	// Profile 是本地 Prompt Preview Runtime 的唯一批准标识。
	Profile = "write_prompts.runtime.v2preview1"
	// SourceType 是专用 Session Input 来源，不得伪装成 user_message。
	SourceType = "write_prompts_preview"
	// ToolRegistryRef 是恰好包含 write_prompts 的可执行 Registry 引用。
	ToolRegistryRef = "write_prompts.preview_tools@v1"
	// ToolDefinitionRef 是冻结的开发预览 Tool Definition 引用。
	ToolDefinitionRef = "write_prompts.v2preview1"
	// PromptRef 是 Graph 内部主 Prompt 引用。
	PromptRef = "graph_tool.write_prompts.preview.v1"
	// ValidatorRef 是 Graph 候选 Validator 引用。
	ValidatorRef = "write_prompts.preview.validator.v1"
	// ExactSetValidatorRef 是 Graph 目标全集 Validator 引用。
	ExactSetValidatorRef = "write_prompts.preview.exact-set-validator.v1"
	// RouterModelRouteRef 是外层唯一 ToolCall 本地模型路由。
	RouterModelRouteRef = "local.fake.write_prompts.router@v1"
	// PromptModelRouteRef 是 Graph 内候选生成本地模型路由。
	PromptModelRouteRef = "local.fake.write_prompts.prompt@v1"
	// RuntimePolicyRef 是 receipt-first、ReturnDirectly 的运行策略引用。
	RuntimePolicyRef = "write_prompts.runtime_policy@v1"
	// BudgetRef 是一次 Router、一次 Tool、一次 Graph Model 的预算引用。
	BudgetRef = "write_prompts.local_preview_budget@v1"
	// BusinessResendLimit 是 prepared 命令在权威 not_found 后的最大自动重发次数。
	BusinessResendLimit = 1

	approvedToolDefinitionDigest    = "e7de5abe75d0196c8b19b5dfabbbc4ddbdf533d2c1ec53bbb9bc853c543933a9"
	approvedPromptDigest            = "2d7947ff7b868d4d35d1fff788e530775dbc805b582773e8de736341ea3638c9"
	approvedValidatorDigest         = "ac3ee7b72784f9ab7799e3d76001128fbf6403408f2cb4235c211b8ef0269ab0"
	approvedExactSetValidatorDigest = "e3ec53ca0f88f352e9bebc0d6ed06185063e07e8c42e2d650f021506eec8cc8d"

	toolRegistryCanonicalPrefix = `{"schema_version":"write_prompts.tool_registry.v1","tools":[{"tool_key":"write_prompts","definition":"write_prompts.v2preview1","definition_digest":"`
	toolRegistryCanonicalSuffix = `","intent_schema":"write_prompts.preview.intent.v1","result_schema":"write_prompts.preview.result.v1","return_directly":true}]}`
	runtimePolicyCanonical      = `{"schema_version":"write_prompts.runtime_policy.v1","profile":"write_prompts.runtime.v2preview1","source_type":"write_prompts_preview","local_only":true,"receipt_first":true,"return_directly":true,"max_targets":96,"default_output_language":"zh-CN","max_command_resends":1}`
	routerRouteCanonical        = `{"schema_version":"write_prompts.model_route.v1","call_kind":"router","route":"local.fake.write_prompts.router@v1","provider":"local_fake"}`
	promptRouteCanonical        = `{"schema_version":"write_prompts.model_route.v1","call_kind":"graph_prompt","route":"local.fake.write_prompts.prompt@v1","provider":"local_fake"}`
	budgetCanonical             = `{"schema_version":"write_prompts.budget.v1","max_router_model_calls":1,"max_tool_calls":1,"max_graph_model_calls":1,"business_resend_limit":1,"retry":0,"failover":0}`
)

// ApprovedPolicy 返回本 Profile 唯一允许的目标、语言与恢复预算。
// Bootstrap 从环境读取并校验同一组值，入队再把策略摘要冻结到 Turn Context。
func ApprovedPolicy() writeprompts.Policy {
	return writeprompts.Policy{
		Version: writeprompts.RuntimePolicyVersion, MaxTargets: 96,
		DefaultOutputLanguage: "zh-CN", MaxCommandResends: BusinessResendLimit,
	}
}

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
	// ExactSetValidatorRef 是 DAG Validator 引用。
	ExactSetValidatorRef string
	// ExactSetValidatorDigest 是 DAG Validator 摘要。
	ExactSetValidatorDigest string
	// RouterModelRouteRef 是 Router Fake Model 路由引用。
	RouterModelRouteRef string
	// RouterModelRouteDigest 是 Router Fake Model 路由摘要。
	RouterModelRouteDigest string
	// PromptModelRouteRef 是 Graph Prompt Fake Model 路由引用。
	PromptModelRouteRef string
	// PromptModelRouteDigest 是 Graph Prompt Fake Model 路由摘要。
	PromptModelRouteDigest string
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
		ExactSetValidatorRef: ExactSetValidatorRef, ExactSetValidatorDigest: approvedExactSetValidatorDigest,
		RouterModelRouteRef: RouterModelRouteRef, RouterModelRouteDigest: digestText(routerRouteCanonical),
		PromptModelRouteRef: PromptModelRouteRef, PromptModelRouteDigest: digestText(promptRouteCanonical),
		RuntimePolicyRef: RuntimePolicyRef, RuntimePolicyDigest: digestText(runtimePolicyCanonical),
		BudgetRef: BudgetRef, BudgetDigest: digestText(budgetCanonical),
	}
}

// ValidateApprovedArtifacts 在启动期把当前真实工件与独立审批锚点逐项 exact-match。
// 修改工件时必须显式升级版本并更新审批摘要，不能在同 Profile 下自动批准漂移。
func ValidateApprovedArtifacts() error {
	if ToolDefinitionRef != writeprompts.ToolDefinitionVersion || PromptRef != writeprompts.PromptVersion ||
		ValidatorRef != writeprompts.ValidatorVersion || ExactSetValidatorRef != writeprompts.ExactSetValidatorVersion {
		return fmt.Errorf("validate write prompts approved artifacts: version reference mismatch")
	}
	checks := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "tool definition", actual: writeprompts.ToolDefinitionDigest(), expected: approvedToolDefinitionDigest},
		{name: "prompt", actual: writeprompts.PromptArtifactDigest(), expected: approvedPromptDigest},
		{name: "validator", actual: writeprompts.ValidatorArtifactDigest(), expected: approvedValidatorDigest},
		{name: "exact-set validator", actual: writeprompts.ExactSetValidatorArtifactDigest(), expected: approvedExactSetValidatorDigest},
	}
	for _, check := range checks {
		if check.actual != check.expected {
			return fmt.Errorf("validate write prompts approved artifacts: %s digest mismatch", check.name)
		}
	}
	return nil
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
