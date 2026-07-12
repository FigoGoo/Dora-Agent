# 第 1 步：推进映射数据化 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `server/runtime_processor.go` 里审批续作的硬编码 next-capability 映射（两个平行 switch）抽成编排模板数据；模板 schema 节点类型预留 `capability|atomic` 两态（v1 只用前者）。终版 §6.1 第 1 步，验收标准 = **语义不变的重构，零行为变化**。

**Architecture:** 新建 `internal/aigc/orchestration` 包作为编排库第一块砖：`PlanNode`（节点 schema，两态 Kind）+ `ContinuationRule` 决策表（纯数据，可 JSON 序列化，未来 DB 化零改动）+ `DecideApprovalContinuation` 纯查表函数。`runtime_processor.go` 两个映射函数薄壳化：守卫（approved/terminalNoop）与谓词求值（productionComplete）留在 server 保持各自原有错误策略，switch 结构删除换查表。既有 `runtime_processor_test.go:310-456` 七案例×2 + 端到端 3 案例**逐字不动**作黄金测试。

**Tech Stack:** Go 1.26，仅标准库 + 既有内部包。

**取证依据（2026-07-12 核实）：**
- 映射产生：`runtime_processor.go:264` `approvalContinuationNextStageInstruction`（LLM 文字指示）+ `:287` `approvalContinuationNextCapabilityDirective`（机器指令），同键不同产物的平行 switch。
- 注入：`runtime_processor.go:146-154` ApprovalContinuationResult 分支拼 SystemMessage。
- 强制执行：`agent/next_capability.go` middleware（Parse→合成稳定 ToolCall→repeat guard），白名单 `validateNextCapabilityDirective:208` 只认 plan_storyboard/generate_media/assemble_output——atomic 节点即使误入 directive 也会被拒，天然安全。
- 谓词 fast-path 等价性：`approvalContinuationProductionComplete:333` 首查 artifactType×commandKind 匹配，不匹配立即 `(false, nil)` 不解码——所以薄壳把谓词求值从 case 内**前置**到查表前，对 creation_spec_revision/unknown 类型无行为差异（不解码、不出错）。两壳错误策略差异必须保留：instruction 壳吞谓词错（`:274` `productionComplete, _ :=`）、terminalErr 时返回固定文本；directive 壳传播两类错误。
- 黄金测试：`runtime_processor_test.go` `TestApprovalContinuationNextStageInstruction`（7 案例含逐字中文文本）、`TestApprovalContinuationNextCapabilityDirective`（7 案例含 version 分派）、`TestFinalCandidateApprovalDeterministicallyStartsAssembly`、`TestCandidateApprovalDoesNotAssembleBeforeEveryCandidateIsResolved`、`TestApprovalProductionCompletionFailsClosedOnMalformedAggregate`。
- capability keys：`capability/intents.go:12-17` 五常量 + `AgentToolKeys` 切片。

**决策表全量语义（从旧 switch 逐分支提取，一行不多一行不少）：**

| artifact_type | when（命名谓词） | node (kind=capability) | instruction |
|---|---|---|---|
| creation_spec_revision | artifact_version_gt_1 | plan_storyboard `{"mode":"replan","preserve_approved_assets":true}` | 「必须调用 plan_storyboard；禁止再次调用 plan_creation_spec」 |
| creation_spec_revision | （兜底） | plan_storyboard `{"mode":"create"}` | 同上一行（instruction 不分 version） |
| storyboard_revision | production_complete | assemble_output `{"mode":"preview","output_type":"video"}` | 「…全部生产槽位已激活；必须调用 assemble_output…」 |
| storyboard_revision | （兜底） | generate_media `{"phase":"auto_next","policy":"all_eligible"}` | 「禁止再次调用 plan_storyboard；必须调用 generate_media…」 |
| candidate_asset | production_complete | assemble_output 同上 | 同 storyboard_revision+complete 行 |
| candidate_asset | （兜底） | generate_media 同上 | 「必须继续调用 generate_media…仅当没有更多 eligible…」 |
| （未匹配类型） | - | 无 node | 「该 artifact_type 没有定义确定性下一阶段…」 |

匹配语义：同 artifact_type 内按声明顺序取第一条 when 满足的行；when 为空 = 恒真兜底。

---

### Task 1: PlanNode schema（编排模板节点，两态 Kind）

**Files:**
- Create: `internal/aigc/orchestration/plan_node.go`
- Test: `internal/aigc/orchestration/plan_node_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestPlanNodeValidate(t *testing.T) {
	valid := PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next"}`)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid capability node: %v", err)
	}

	unknownKind := valid
	unknownKind.Kind = "weird"
	if err := unknownKind.Validate(); err == nil {
		t.Fatal("unknown kind must be rejected")
	}

	unknownCapability := valid
	unknownCapability.ToolKey = "not_a_capability"
	if err := unknownCapability.Validate(); err == nil {
		t.Fatal("capability node must reference a registered capability tool")
	}

	badArguments := valid
	badArguments.Arguments = json.RawMessage(`{`)
	if err := badArguments.Validate(); err == nil {
		t.Fatal("invalid JSON arguments must be rejected")
	}

	// atomic 为预留态：kind 合法、tool_key 非空即可（原子词汇表校验属 L2，
	// v1 无注册表可查；directive 白名单在 agent/next_capability.go 兜底）。
	atomic := PlanNode{Kind: NodeKindAtomic, ToolKey: "image_generate", Arguments: json.RawMessage(`{}`)}
	if err := atomic.Validate(); err != nil {
		t.Fatalf("reserved atomic node must pass schema validation: %v", err)
	}
	emptyAtomic := atomic
	emptyAtomic.ToolKey = " "
	if err := emptyAtomic.Validate(); err == nil {
		t.Fatal("atomic node must still name a tool")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run TestPlanNodeValidate -v`
Expected: FAIL（包不存在 / 未定义符号）

- [ ] **Step 3: 最小实现**

```go
// Package orchestration 是编排库的进程内起点：模板节点 schema 与
// 审批续作决策表（终版设计 §3.5/§6.1 第 1 步）。表为纯数据、可 JSON
// 序列化；评估点谓词按设计立场留在 Go 代码，数据只引用谓词名。
package orchestration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

type NodeKind string

const (
	NodeKindCapability NodeKind = "capability"
	// NodeKindAtomic 为 L2 原子词汇预留（终版 §6.1 第 1 步）；v1 模板
	// 不使用，schema 认识它以保证未来模板数据无需迁移。
	NodeKindAtomic NodeKind = "atomic"
)

// PlanNode 是编排模板里的一个可执行节点：调用哪个工具、用什么参数。
type PlanNode struct {
	Kind      NodeKind        `json:"kind"`
	ToolKey   string          `json:"tool_key"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (n PlanNode) Validate() error {
	toolKey := strings.TrimSpace(n.ToolKey)
	if toolKey == "" {
		return fmt.Errorf("plan node tool_key is required")
	}
	if len(n.Arguments) > 0 && !json.Valid(n.Arguments) {
		return fmt.Errorf("plan node arguments must be valid JSON")
	}
	switch n.Kind {
	case NodeKindCapability:
		if !slices.Contains(capability.AgentToolKeys, toolKey) {
			return fmt.Errorf("plan node capability %q is not registered", toolKey)
		}
		return nil
	case NodeKindAtomic:
		return nil
	default:
		return fmt.Errorf("plan node kind %q is not supported", n.Kind)
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/orchestration -run TestPlanNodeValidate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "feat(orchestration): 编排模板节点 schema——kind 两态 capability|atomic（后者预留）"
```

---

### Task 2: 审批续作决策表 + 纯查表 Decide

**Files:**
- Create: `internal/aigc/orchestration/approval_continuation.go`
- Test: `internal/aigc/orchestration/approval_continuation_test.go`

- [ ] **Step 1: 写失败测试（覆盖全表七行 + 匹配顺序 + 表自身合法性）**

```go
package orchestration

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestApprovalContinuationRulesAreValid(t *testing.T) {
	if err := ValidateApprovalContinuationRules(); err != nil {
		t.Fatalf("shipped rules must be valid: %v", err)
	}
}

func TestDecideApprovalContinuation(t *testing.T) {
	tests := []struct {
		name         string
		artifactType string
		guards       map[string]bool
		wantTool     string
		wantArgs     string
		wantInstr    string // 判包含（逐字全文由 server 黄金测试锁定）
	}{
		{
			name: "spec v1", artifactType: "creation_spec_revision",
			guards:   map[string]bool{},
			wantTool: capability.PlanStoryboardToolKey, wantArgs: `{"mode":"create"}`,
			wantInstr: "必须调用 plan_storyboard；禁止再次调用 plan_creation_spec",
		},
		{
			name: "spec revision", artifactType: "creation_spec_revision",
			guards:   map[string]bool{GuardArtifactVersionGt1: true},
			wantTool: capability.PlanStoryboardToolKey, wantArgs: `{"mode":"replan","preserve_approved_assets":true}`,
			wantInstr: "必须调用 plan_storyboard；禁止再次调用 plan_creation_spec",
		},
		{
			name: "storyboard not complete", artifactType: "storyboard_revision",
			guards:   map[string]bool{},
			wantTool: capability.GenerateMediaToolKey, wantArgs: `{"phase":"auto_next","policy":"all_eligible"}`,
			wantInstr: "禁止再次调用 plan_storyboard",
		},
		{
			name: "storyboard complete", artifactType: "storyboard_revision",
			guards:   map[string]bool{GuardProductionComplete: true},
			wantTool: capability.AssembleOutputToolKey, wantArgs: `{"mode":"preview","output_type":"video"}`,
			wantInstr: "全部生产槽位已激活",
		},
		{
			name: "candidate not complete", artifactType: "candidate_asset",
			guards:   map[string]bool{},
			wantTool: capability.GenerateMediaToolKey, wantArgs: `{"phase":"auto_next","policy":"all_eligible"}`,
			wantInstr: "必须继续调用 generate_media",
		},
		{
			name: "candidate complete", artifactType: "candidate_asset",
			guards:   map[string]bool{GuardProductionComplete: true},
			wantTool: capability.AssembleOutputToolKey, wantArgs: `{"mode":"preview","output_type":"video"}`,
			wantInstr: "全部生产槽位已激活",
		},
		{
			name: "unknown type", artifactType: "unknown",
			guards:    map[string]bool{GuardProductionComplete: true},
			wantInstr: "没有定义确定性下一阶段",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := DecideApprovalContinuation(ApprovalContinuationInput{
				ArtifactType: test.artifactType, Guards: test.guards,
			})
			if !strings.Contains(decision.Instruction, test.wantInstr) {
				t.Fatalf("instruction %q must contain %q", decision.Instruction, test.wantInstr)
			}
			if test.wantTool == "" {
				if decision.Node != nil {
					t.Fatalf("unexpected node %+v", decision.Node)
				}
				return
			}
			if decision.Node == nil {
				t.Fatal("expected a next node")
			}
			if decision.Node.Kind != NodeKindCapability || decision.Node.ToolKey != test.wantTool || string(decision.Node.Arguments) != test.wantArgs {
				t.Fatalf("node = %+v", decision.Node)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run "TestApprovalContinuationRulesAreValid|TestDecideApprovalContinuation" -v`
Expected: FAIL（未定义符号）

- [ ] **Step 3: 最小实现（表数据从旧 switch 逐字迁移，instruction 用 Sprintf 展开 ToolKey 常量保持与旧代码同源）**

```go
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
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.PlanStoryboardToolKey, Arguments: json.RawMessage(`{"mode":"create"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：必须调用 %s；禁止再次调用 %s。", capability.PlanStoryboardToolKey, capability.PlanCreationSpecToolKey),
	},
	{
		ArtifactType: "storyboard_revision", When: GuardProductionComplete,
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.AssembleOutputToolKey, Arguments: json.RawMessage(`{"mode":"preview","output_type":"video"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：冻结的 Storyboard 已无候选待审且全部生产槽位已激活；必须调用 %s，参数固定为 {\"mode\":\"preview\",\"output_type\":\"video\"}，由该 Capability 先校验装配依赖再派发本地预览。", capability.AssembleOutputToolKey),
	},
	{
		ArtifactType: "storyboard_revision",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next","policy":"all_eligible"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：禁止再次调用 %s；必须调用 %s，参数固定为 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}。", capability.PlanStoryboardToolKey, capability.GenerateMediaToolKey),
	},
	{
		ArtifactType: "candidate_asset", When: GuardProductionComplete,
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.AssembleOutputToolKey, Arguments: json.RawMessage(`{"mode":"preview","output_type":"video"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：冻结的 Storyboard 已无候选待审且全部生产槽位已激活；必须调用 %s，参数固定为 {\"mode\":\"preview\",\"output_type\":\"video\"}，由该 Capability 先校验装配依赖再派发本地预览。", capability.AssembleOutputToolKey),
	},
	{
		ArtifactType: "candidate_asset",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next","policy":"all_eligible"}`)},
		Instruction: fmt.Sprintf("确定性下一阶段：必须继续调用 %s，参数使用 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}；仅当没有更多 eligible 媒体阶段且当前装配依赖已满足时，才调用 %s。", capability.GenerateMediaToolKey, capability.AssembleOutputToolKey),
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
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/orchestration -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "feat(orchestration): 审批续作决策表——旧 switch 逐分支迁移为纯数据规则（含表自校验）"
```

---

### Task 3: runtime_processor 薄壳化（黄金测试逐字不动）

**Files:**
- Modify: `internal/aigc/server/runtime_processor.go`（`approvalContinuationNextStageInstruction:264` 与 `approvalContinuationNextCapabilityDirective:287` 两函数体；import 增 `orchestration`）
- Test: 无新测试——`runtime_processor_test.go` 既有黄金测试**一行不改**

- [ ] **Step 1: 改写 instruction 壳（守卫与吞错策略保持逐行为等价）**

```go
// approvalContinuationNextStageInstruction maps an applied approval result to
// the only deterministic next capability. Non-approved and unknown artifacts
// deliberately remain non-prescriptive so a terminal receipt cannot force an
// invalid workflow transition.
func approvalContinuationNextStageInstruction(value sessionruntime.ApprovalContinuationResult) string {
	terminalNoop, terminalErr := approvalContinuationIsTerminalNoop(value)
	if !strings.EqualFold(strings.TrimSpace(value.EffectiveStatus), string(approval.StatusApproved)) || terminalNoop || terminalErr != nil {
		return "本次审批未形成可推进的已应用状态，不强制推进到下一个 Capability；准确解释结果并按当前状态决定停止、等待用户输入或重新规划。"
	}
	// 谓词错误在文字指示通道历来按 false 继续（机器指令通道才传播），
	// 保持原策略。谓词自身对不相关 artifact_type 走 fast-path 不解码。
	productionComplete, _ := approvalContinuationProductionComplete(value)
	return orchestration.DecideApprovalContinuation(orchestration.ApprovalContinuationInput{
		ArtifactType: strings.TrimSpace(value.ArtifactType),
		Guards: map[string]bool{
			orchestration.GuardArtifactVersionGt1: value.ArtifactVersion > 1,
			orchestration.GuardProductionComplete: productionComplete,
		},
	}).Instruction
}
```

- [ ] **Step 2: 改写 directive 壳（错误传播策略保持逐行为等价；SourceID 运行时身份留在 server）**

```go
func approvalContinuationNextCapabilityDirective(value sessionruntime.ApprovalContinuationResult) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(value.EffectiveStatus), string(approval.StatusApproved)) {
		return "", nil
	}
	terminalNoop, err := approvalContinuationIsTerminalNoop(value)
	if err != nil {
		return "", err
	}
	if terminalNoop {
		return "", nil
	}
	productionComplete, completeErr := approvalContinuationProductionComplete(value)
	if completeErr != nil {
		return "", completeErr
	}
	decision := orchestration.DecideApprovalContinuation(orchestration.ApprovalContinuationInput{
		ArtifactType: strings.TrimSpace(value.ArtifactType),
		Guards: map[string]bool{
			orchestration.GuardArtifactVersionGt1: value.ArtifactVersion > 1,
			orchestration.GuardProductionComplete: productionComplete,
		},
	})
	if decision.Node == nil {
		return "", nil
	}
	return agentcontrol.EncodeNextCapabilityDirective(agentcontrol.NextCapabilityDirective{
		Version:  agentcontrol.NextCapabilityDirectiveVersion,
		SourceID: fmt.Sprintf("approval:%s:%d:%d", strings.TrimSpace(value.ApprovalID), value.DecisionVersion, value.ExecutionEpoch),
		Tool:     decision.Node.ToolKey, Arguments: decision.Node.Arguments,
	})
}
```

注意：directive 壳把 productionComplete 求值从旧代码的 case 内前置到查表前。等价性依据（header 已取证）：谓词首行按 artifactType×commandKind 分流，creation_spec_revision/unknown 类型立即 `(false, nil)`，不解码不产错——新旧唯一可能出错的输入集合相同（storyboard_revision/candidate_asset × 匹配 commandKind × malformed aggregate），且都传播。

- [ ] **Step 3: 删除旧 switch 与随之孤立的代码**

删除两个函数体内原 switch 全部分支。`approvalContinuationProductionComplete`、`approvalContinuationIsTerminalNoop` 保留在 server（谓词=评估点=代码）。检查无其他孤立符号：

Run: `go vet ./internal/aigc/server`

- [ ] **Step 4: 黄金测试全绿（一行未改的前提下）**

Run: `git diff --stat internal/aigc/server/runtime_processor_test.go`（必须为空输出）
Run: `go test ./internal/aigc/server -run "TestApprovalContinuation|TestFinalCandidate|TestCandidateApproval|TestApprovalProduction" -v`
Expected: 全部 PASS

- [ ] **Step 5: 包级回归 + Commit**

Run: `go test ./internal/aigc/server ./internal/aigc/orchestration ./internal/aigc/agent`
Expected: 全部 PASS

```bash
git add internal/aigc/server/runtime_processor.go
git commit -m "refactor(server): 审批续作映射薄壳化——switch 移除，查 orchestration 决策表（黄金测试零改动全绿）"
```

---

### Task 4: 全量回归与收尾

**Files:** 计划文件本身（勾选+执行记录）

- [ ] **Step 1: 全仓构建/测试/vet**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -30`
Expected: 全部 PASS（本地 Postgres/Redis 已起则含 DB 路径）

- [ ] **Step 2: DB 相关包强制真跑**

Run: `go test ./internal/aigc/server ./internal/aigc/sessionruntime ./internal/aigc/capabilityruntime -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 勾选计划 + 写执行记录 + 收尾提交**

```bash
git add docs/superpowers/plans/2026-07-12-step1-continuation-mapping-data.md
git commit -m "chore: 第1步推进映射数据化完成——零行为变化（黄金测试锁定）"
```

---

## 计划自审记录

- 覆盖检查：终版 §6.1 第 1 步两项要求——映射抽成模板数据 ✓（Task 2 决策表七行 = 旧 switch 全分支）、schema 预留 capability|atomic ✓（Task 1 NodeKind 两态 + atomic 校验语义）；零行为变化 ✓（Task 3 黄金测试逐字不动 + 谓词 fast-path 等价性论证）。
- 边界外：表进 DB（随编排库/沉淀机制后置）、Agent 接管推进（第 3 步）、instruction 文本模板化渲染（无需求）。
- 已知取舍：instruction 文本在数据行内以 Sprintf 展开 ToolKey 常量构造——字符串"来源"仍是代码但"结构"已数据化；黄金测试锁逐字文本，ToolKey 改名会同步失败暴露。atomic 节点 v1 仅 schema 级校验（kind 合法+tool_key 非空），词汇表级校验属 L2。
