package orchestration

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

// 命名谓词：评估点在模板数据里的引用形态。求值留在调用方 Go 代码
// （终版立场：编排=数据，评估点=代码）。
const (
	GuardArtifactVersionGt1 = "artifact_version_gt_1"
	GuardProductionComplete = "production_complete"
)

// ContinuationRule 是审批续作决策表的一行：artifact_type 命中后，
// 同类型内按声明顺序取第一条 When 满足的行；When 为空 = 恒真兜底。
type ContinuationRule struct {
	ArtifactType string   `json:"artifact_type"`
	When         string   `json:"when,omitempty"`
	Node         PlanNode `json:"node"`
	Instruction  string   `json:"instruction"`
}

type ApprovalContinuationInput struct {
	ArtifactType string
	Guards       map[string]bool
}

type ContinuationDecision struct {
	Instruction string
	Node        *PlanNode
}

// approvalContinuationRules 由 runtime_processor.go 旧 switch 逐分支
// 迁移（第 1 步数据化，零行为变化）；第 3 步 Agent 接管推进后，本表
// 降级为模板参考而非强制指令。
var approvalContinuationRules = []ContinuationRule{
	{
		ArtifactType: "creation_spec_revision", When: GuardArtifactVersionGt1,
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.PlanStoryboardToolKey, Arguments: json.RawMessage(`{"mode":"replan","preserve_approved_assets":true}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：必须调用 %s；禁止再次调用 %s。", capability.PlanStoryboardToolKey, capability.PlanCreationSpecToolKey),
	},
	{
		ArtifactType: "creation_spec_revision",
		Node:         PlanNode{Kind: NodeKindCapability, ToolKey: capability.PlanStoryboardToolKey, Arguments: json.RawMessage(`{"mode":"create"}`)},
		Instruction:  fmt.Sprintf("确定性下一阶段：必须调用 %s；禁止再次调用 %s。", capability.PlanStoryboardToolKey, capability.PlanCreationSpecToolKey),
	},
	{
		ArtifactType: "storyboard_revision", When: GuardProductionComplete,
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.AssembleOutputToolKey, Arguments: json.RawMessage(`{"mode":"preview","output_type":"video"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：冻结的 Storyboard 已无候选待审且全部生产槽位已激活；必须调用 %s，参数固定为 {\"mode\":\"preview\",\"output_type\":\"video\"}，由该 Capability 先校验装配依赖再派发本地预览。", capability.AssembleOutputToolKey),
	},
	{
		ArtifactType: "storyboard_revision",
		Node:         PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next","policy":"all_eligible"}`)},
		Instruction:  fmt.Sprintf("确定性下一阶段：禁止再次调用 %s；必须调用 %s，参数固定为 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}。", capability.PlanStoryboardToolKey, capability.GenerateMediaToolKey),
	},
	{
		ArtifactType: "candidate_asset", When: GuardProductionComplete,
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.AssembleOutputToolKey, Arguments: json.RawMessage(`{"mode":"preview","output_type":"video"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：冻结的 Storyboard 已无候选待审且全部生产槽位已激活；必须调用 %s，参数固定为 {\"mode\":\"preview\",\"output_type\":\"video\"}，由该 Capability 先校验装配依赖再派发本地预览。", capability.AssembleOutputToolKey),
	},
	{
		ArtifactType: "candidate_asset",
		Node:         PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next","policy":"all_eligible"}`)},
		Instruction:  fmt.Sprintf("确定性下一阶段：必须继续调用 %s，参数使用 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}；仅当没有更多 eligible 媒体阶段且当前装配依赖已满足时，才调用 %s。", capability.GenerateMediaToolKey, capability.AssembleOutputToolKey),
	},
}

const undefinedContinuationInstruction = "审批虽为 approved，但该 artifact_type 没有定义确定性下一阶段；只解释已持久化结果并重新读取当前状态，不要猜测或重复调用 Capability。"

// ValidateApprovalContinuationRules 供启动自检与测试：表中每个节点
// 必须通过 schema 校验，谓词名必须是已知谓词。
func ValidateApprovalContinuationRules() error {
	known := map[string]struct{}{GuardArtifactVersionGt1: {}, GuardProductionComplete: {}}
	for index, rule := range approvalContinuationRules {
		if strings.TrimSpace(rule.ArtifactType) == "" {
			return fmt.Errorf("rule %d: artifact_type is required", index)
		}
		if rule.When != "" {
			if _, ok := known[rule.When]; !ok {
				return fmt.Errorf("rule %d: unknown guard %q", index, rule.When)
			}
		}
		if err := rule.Node.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", index, err)
		}
		if strings.TrimSpace(rule.Instruction) == "" {
			return fmt.Errorf("rule %d: instruction is required", index)
		}
	}
	return nil
}

// DecideApprovalContinuation 纯查表：调用方负责守卫（approved/terminal
// noop）与谓词求值；未命中任何行时无 Node、给"未定义下一阶段"指示。
func DecideApprovalContinuation(input ApprovalContinuationInput) ContinuationDecision {
	artifactType := strings.TrimSpace(input.ArtifactType)
	for _, rule := range approvalContinuationRules {
		if rule.ArtifactType != artifactType {
			continue
		}
		if rule.When != "" && !input.Guards[rule.When] {
			continue
		}
		node := rule.Node
		return ContinuationDecision{Instruction: rule.Instruction, Node: &node}
	}
	return ContinuationDecision{Instruction: undefinedContinuationInstruction}
}
