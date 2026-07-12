# Plan 1/3：原子词汇层 + 执行计划 Runtime 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 b677904 终版设计的地基两块——原子工具统一契约（§1.1）与执行计划 runtime（§3.5/§3.6），验收标准 = 程序化提交的 8 要素计划能跑完「备提示词 → 派发生成 → waiting_jobs 挂起 → 批次唤醒 → 终态」全链，含 waiting_user 卡点、新实例恢复、幂等重放、活计划修订。

**Architecture:** 新建 `internal/aigc/vocabulary`（工具契约+注册表，词汇层）；扩展 `internal/aigc/orchestration`（8 要素计划 schema + 五查校验 + PlanRun 状态机 + 自写调度器）。**技术路径判定：自写调度器**——由设计自身导出：§3.6 的三条状态机语义（挂起统一/活计划修订/挂起即释放）要求节点状态可独立持久、可在挂起中修订未执行部分、挂起后零运行时驻留；任何"编译期图+checkpoint"形态的执行框架都与"活计划修订"存在图-快照结构耦合冲突，故计划不经 Eino graph 承载（Eino 仅继续服务于单工具内部实现）。Task 4-6 的恢复/幂等/修订测试就是 §6.3 可行性判据——**若证伪则停止执行，回设计重议路径**。

**Tech Stack:** Go 1.26，仅标准库 + 被改造的既有内部包。

**方案基准与改造纲领（立足点声明）：**
- **方案基准 = `b677904` 版终版设计（五块 delta 表），是本计划唯一的形态权威**；后续任何合流路线、任何既有实现的形态（含五能力 Capability 线）都不构成设计约束。改造代码本来就是要做的事——判定标准只有一条：是否符合 b677904 的设计形态。
- 现有代码按设计眼光三分类：
  - **改造对象**：`generation/` 异步链与 `asset/` 存储——它们是 §4 异步生成中心与 §5 资产模型的实现载体（§6.1 delta 表原文认领"generation/ 全链+wakeup 最成熟"），但形态须按设计改：幂等键结构改为"计划实例+节点+目标+派发序号"（§1.4）、新增派发时预注册 generating 态（§5）、批次终结对接计划挂起/唤醒（§4 delta）。BindingToken 的 target-中立两态即 §5 绑定目标多态的落地，沿用。
  - **不作依赖**：五能力 Capability Graph / capabilityruntime——词汇层与计划 runtime 对其零依赖；其去留（降级为编排库深模板或废弃）是 Plan 2 按 §2.2 处置的事，本计划不迁就。
  - **废弃**：`mediagraph/`（§6.1 原文："作废重构，节点序被吸收"——其节点序由编排库模板承载，Plan 3 落地时移除）；`docs/superpowers/plans/2026-07-12-phase-a-plan-runtime-spike.md`（判据被本计划 Task 4-6 吸收）。
- 本计划覆盖 delta 表：原始 Tool（契约 + 验收链所需词汇 3 个 + 护栏切面机制）、动态编排（校验器 + runtime）、异步任务（计划挂起/唤醒对接）。
- Plan 2 覆盖：Agent 注册面改造、M1/M2/M3、评估点续编闭环、认知类其余 5 个重写、五能力按 §2.2 处置。
- Plan 3 覆盖：编排库存储+目录注入、6 模板数据化、绑定表建模+actor 审计、前端计划视图、mediagraph 移除。

**设计定案（写死，任务间类型一致性的唯一来源）：**

- 工具契约（§1.1 逐条对应）：三要素+入参出参概要=`Descriptor`；`Run(ctx, Call) → Result | error`；error=基础设施故障（可重试），`Result.Fail`=业务失败（决策输入，不是异常）；`Result.Suspension`=工具声明挂起（交互工具/派发工具用）；无状态、互不调用、不发事件、写幂等（幂等键经 `Call.IdempotencyKey` 注入）。
- 参数引用语法：`Params` 值若为字符串且形如 `"$<stepID>.<outputKey>"` 则为上游引用，调度器在节点就绪时解析；其余为字面值。参数槽（`<PROMPT>` 类）必须在提交前实例化完毕，校验器把含 `<` `>` 包裹的裸槽值拒为"槽未实例化"。
- 卡点=计划节点引用交互工具（`request_confirmation`），无独立 Gate 语法——"一切执行皆计划"（§3.4）。
- 评估点=`PlanStep.Evaluate: true`：该节点 succeeded 后计划转 `suspended(waiting_agent)`，等 Agent 续编（Plan 2 闭环；本计划以测试注入续编决策）。
- 批量展开：`PlanStep.Expand` 字段预留，v1 校验对非空 Expand 拒绝（"暂未支持"）；六图的批量场景全部由 `dispatch_generation` 的 targets 数组承载。
- 预算五查：v1 以生成 job 数阈值（默认 20）计，超阈值不拒绝 → `PlanRun.PreviewRequired=true` 并直接进 `suspended(waiting_user)`（计划预览 ⏸ 的 runtime 承载；Agent 侧"深编排首提"触发在 Plan 2）。
- 权限五查与保障五查：v1 最小版=写域白名单表（入口×写域）+ R1 切面可织入检查（媒体执行类节点必须能织入护栏链）；`reserve_credits`/`check_permission` 空实现占位（§1.5 拍板）。
- `compliance_check`：规则表起步——硬拦（禁区词，拒绝不可翻案）/软拦（线索词，挂起授权）；本计划落切面机制+最小规则表，完整规则运营后置。

---

## 类型定稿（先读此节再看任务）

```go
// ── internal/aigc/vocabulary/contract.go ──
type ParamSpec struct {
	Type     string `json:"type"` // string|int|bool|array|object|ref
	Desc     string `json:"desc"`
	Required bool   `json:"required,omitempty"`
}

type Descriptor struct {
	Key         string               `json:"tool_key"`
	Name        string               `json:"tool_name"`
	Description string               `json:"tool_description"`
	Category    string               `json:"category"` // cognition|media|data|guard|interaction
	Inputs      map[string]ParamSpec `json:"inputs,omitempty"`
	Outputs     map[string]ParamSpec `json:"outputs,omitempty"`
}

type Call struct {
	SessionID      string
	UserID         string
	PlanRunID      string
	NodeID         string
	Attempt        int
	IdempotencyKey string // runtime 注入：plan_run+node+attempt 组合
	Inputs         map[string]any
}

type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type Suspension struct {
	Reason  string         `json:"reason"` // waiting_user | waiting_jobs
	Payload map[string]any `json:"payload,omitempty"`
}

type Result struct {
	Outputs    map[string]any `json:"outputs,omitempty"`
	Fail       *Failure       `json:"fail,omitempty"`
	Suspension *Suspension    `json:"suspension,omitempty"`
}

type Tool interface {
	Descriptor() Descriptor
	Run(ctx context.Context, call Call) (Result, error)
}

// ── internal/aigc/orchestration/plan.go ──
type ExpandSpec struct {
	Over string `json:"over,omitempty"` // 预留
}

type PlanStep struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Params    map[string]any `json:"params,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
	Required  bool           `json:"required"`
	Evaluate  bool           `json:"evaluate,omitempty"`
	Expand    *ExpandSpec    `json:"expand,omitempty"`
}

type ExecutionPlan struct {
	PlanID        string     `json:"plan_id"`
	Source        string     `json:"source"` // template:<key> | dynamic
	Summary       string     `json:"summary"`
	Direction     string     `json:"direction"` // image|video|music|audio|mixed
	Steps         []PlanStep `json:"steps"`
	EstimatedJobs int        `json:"estimated_jobs,omitempty"`
	SuccessPolicy string     `json:"success_policy,omitempty"` // "" = all_required
}

// ── internal/aigc/orchestration/plan_run.go ──
const (
	RunStatusDraft            = "draft"
	RunStatusRunning          = "running"
	RunStatusSuspended        = "suspended"
	RunStatusSucceeded        = "succeeded"
	RunStatusPartialSucceeded = "partial_succeeded"
	RunStatusFailed           = "failed"
	RunStatusCancelled        = "cancelled"

	SuspendWaitingUser  = "waiting_user"
	SuspendWaitingAgent = "waiting_agent"
	SuspendWaitingJobs  = "waiting_jobs"

	NodeStatusPending   = "pending"
	NodeStatusRunning   = "running"
	NodeStatusSucceeded = "succeeded"
	NodeStatusFailed    = "failed"
	NodeStatusSkipped   = "skipped"
)

type NodeRun struct {
	StepID    string                `json:"step_id"`
	Status    string                `json:"status"`
	Attempt   int                   `json:"attempt"`
	Outputs   map[string]any        `json:"outputs,omitempty"`
	Fail      *vocabulary.Failure   `json:"fail,omitempty"`
	Suspension *vocabulary.Suspension `json:"suspension,omitempty"`
	ResumeKey string                `json:"resume_key,omitempty"` // 挂起节点一次性恢复键
	Resumed   bool                  `json:"resumed,omitempty"`
}

type PlanRun struct {
	ID              string              `json:"id"`
	SessionID       string              `json:"session_id"`
	UserID          string              `json:"user_id"`
	Plan            ExecutionPlan       `json:"plan"`
	Status          string              `json:"status"`
	SuspendReason   string              `json:"suspend_reason,omitempty"`
	SuspendedNodeID string              `json:"suspended_node_id,omitempty"`
	PreviewRequired bool                `json:"preview_required,omitempty"`
	Nodes           map[string]*NodeRun `json:"nodes"`
	Version         int                 `json:"version"` // 乐观锁
}

type RunStore interface {
	CreateRun(ctx context.Context, run PlanRun) (PlanRun, error)
	GetRun(ctx context.Context, id string) (PlanRun, error)
	// MutateRun 对齐 generation workflow store 的 MutateJob 风格：
	// 版本不符返回 ErrRunVersionConflict。
	MutateRun(ctx context.Context, id string, version int, fn func(*PlanRun) error) (PlanRun, error)
}

// ── internal/aigc/orchestration/scheduler.go ──
type SchedulerConfig struct {
	Store       RunStore
	Vocabulary  *vocabulary.Registry
	MaxParallel int // 默认 4
	JobBudget   int // 预算五查阈值，默认 20
	NewID       func() string
}

type Scheduler struct{ /* 上述字段 */ }

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error)
// Submit：五查 → 预算超限置 PreviewRequired+waiting_user → 否则首轮 Advance
func (s *Scheduler) Submit(ctx context.Context, sessionID, userID string, plan ExecutionPlan) (PlanRun, error)
// Advance：调度循环——并发执行就绪节点直至挂起或终态
func (s *Scheduler) Advance(ctx context.Context, runID string) (PlanRun, error)
// Resume：waiting_user/waiting_agent 恢复；resumeKey 一次性幂等（重复调用返回同一结果）
func (s *Scheduler) Resume(ctx context.Context, runID, resumeKey string, decision map[string]any) (PlanRun, error)
// CompleteJobsWait：waiting_jobs 唤醒（批次终结回调）
func (s *Scheduler) CompleteJobsWait(ctx context.Context, runID, nodeID string, outcome JobsOutcome) (PlanRun, error)
// Revise：活计划修订——skip 未执行节点/追加节点；动已执行节点报错
func (s *Scheduler) Revise(ctx context.Context, runID string, revision PlanRevision) (PlanRun, error)

type JobsOutcome struct {
	BatchID   string         `json:"batch_id"`
	Status    string         `json:"status"` // completed|partial_failed|failed|cancelled
	Summary   map[string]any `json:"summary,omitempty"`
}

type PlanRevision struct {
	SkipStepIDs []string   `json:"skip_step_ids,omitempty"`
	AppendSteps []PlanStep `json:"append_steps,omitempty"`
}
```

哨兵错误（orchestration 包）：`ErrPlanInvalid`、`ErrRunVersionConflict`、`ErrRunNotFound`、`ErrResumeKeyMismatch`、`ErrReviseExecutedStep`。

---

### Task 1: vocabulary 包——工具契约 + 注册表

**Files:**
- Create: `internal/aigc/vocabulary/contract.go`（类型定稿节的契约部分）
- Create: `internal/aigc/vocabulary/registry.go`
- Test: `internal/aigc/vocabulary/registry_test.go`

- [ ] **Step 1: 写失败测试**

```go
package vocabulary

import (
	"context"
	"strings"
	"testing"
)

type echoTool struct{ key string }

func (t echoTool) Descriptor() Descriptor {
	return Descriptor{Key: t.key, Name: "回声", Description: "测试工具", Category: "cognition",
		Inputs:  map[string]ParamSpec{"text": {Type: "string", Desc: "输入", Required: true}},
		Outputs: map[string]ParamSpec{"text": {Type: "string", Desc: "原样返回"}},
	}
}

func (t echoTool) Run(_ context.Context, call Call) (Result, error) {
	return Result{Outputs: map[string]any{"text": call.Inputs["text"]}}, nil
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(echoTool{key: "echo"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(echoTool{key: "echo"}); err == nil {
		t.Fatal("duplicate key must be rejected")
	}
	if err := registry.Register(echoTool{key: " "}); err == nil {
		t.Fatal("empty key must be rejected")
	}
	tool, ok := registry.Get("echo")
	if !ok || tool.Descriptor().Name != "回声" {
		t.Fatalf("lookup failed: %v %v", ok, tool)
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("missing tool must not resolve")
	}
	catalog := registry.CatalogText()
	if !strings.Contains(catalog, "echo") || !strings.Contains(catalog, "回声") {
		t.Fatalf("catalog must list three-elements: %s", catalog)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/vocabulary -run TestRegistry -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现**

`contract.go` 按「类型定稿」节原文。`registry.go`：

```go
package vocabulary

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 是原子工具词汇表（§1 唯一固定词汇表）。注册面只在装配根
// 调用；运行期只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(tool Tool) error {
	descriptor := tool.Descriptor()
	key := strings.TrimSpace(descriptor.Key)
	if key == "" {
		return fmt.Errorf("vocabulary tool key is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[key]; exists {
		return fmt.Errorf("vocabulary tool %q already registered", key)
	}
	r.tools[key] = tool
	return nil
}

func (r *Registry) Get(key string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[strings.TrimSpace(key)]
	return tool, ok
}

func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// CatalogText 生成注入 Agent 上下文的词汇目录（三要素+入参出参概要）。
func (r *Registry) CatalogText() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		d := r.tools[key].Descriptor()
		builder.WriteString(fmt.Sprintf("- %s（%s，%s）：%s", d.Key, d.Name, d.Category, d.Description))
		if len(d.Inputs) > 0 {
			names := make([]string, 0, len(d.Inputs))
			for name, spec := range d.Inputs {
				if spec.Required {
					name += "*"
				}
				names = append(names, name)
			}
			sort.Strings(names)
			builder.WriteString(" 入参:" + strings.Join(names, ","))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/vocabulary -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/vocabulary/
git commit -m "feat(vocabulary): 原子工具统一契约与注册表（三要素/Run契约/fail与挂起为一等结果）"
```

---

### Task 2: ExecutionPlan schema + 五查校验器

**Files:**
- Create: `internal/aigc/orchestration/plan.go`（类型定稿节的 plan 部分 + Validate）
- Test: `internal/aigc/orchestration/plan_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func testVocabulary(t *testing.T) *vocabulary.Registry {
	t.Helper()
	registry := vocabulary.NewRegistry()
	for _, tool := range []vocabulary.Tool{
		staticTool{key: "write_media_prompt", inputs: map[string]vocabulary.ParamSpec{
			"target_desc": {Type: "string", Required: true},
		}},
		staticTool{key: "dispatch_generation", inputs: map[string]vocabulary.ParamSpec{
			"targets": {Type: "array", Required: true},
		}},
		staticTool{key: "request_confirmation", inputs: map[string]vocabulary.ParamSpec{
			"question": {Type: "string", Required: true},
		}},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

// staticTool 是校验/调度测试共用的可配置假工具。
type staticTool struct {
	key     string
	inputs  map[string]vocabulary.ParamSpec
	run     func(call vocabulary.Call) (vocabulary.Result, error)
}

func (t staticTool) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{Key: t.key, Name: t.key, Description: "test", Category: "cognition", Inputs: t.inputs}
}

func (t staticTool) Run(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	if t.run != nil {
		return t.run(call)
	}
	return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
}

func validPlan() ExecutionPlan {
	return ExecutionPlan{
		PlanID: "plan-1", Source: "dynamic", Summary: "一张图", Direction: "image",
		Steps: []PlanStep{
			{ID: "prompt", Tool: "write_media_prompt", Params: map[string]any{"target_desc": "雨中柴犬"}, Required: true},
			{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "提示词可以吗"}, DependsOn: []string{"prompt"}, Required: true},
			{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "$prompt.prompt"}}}, DependsOn: []string{"confirm"}, Required: true},
		},
		EstimatedJobs: 1,
	}
}

func TestPlanValidate(t *testing.T) {
	registry := testVocabulary(t)
	if err := validPlan().Validate(registry, 20); err != nil {
		t.Fatalf("valid plan: %v", err)
	}

	cyclic := validPlan()
	cyclic.Steps[0].DependsOn = []string{"generate"} // prompt→generate→confirm→prompt 成环
	if err := cyclic.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle must be rejected: %v", err)
	}

	unknownTool := validPlan()
	unknownTool.Steps[0].Tool = "no_such_tool"
	if err := unknownTool.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unknown tool must be rejected: %v", err)
	}

	danglingDep := validPlan()
	danglingDep.Steps[1].DependsOn = []string{"ghost"}
	if err := danglingDep.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("dangling dependency must be rejected: %v", err)
	}

	missingRequired := validPlan()
	delete(missingRequired.Steps[0].Params, "target_desc")
	if err := missingRequired.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "required param") {
		t.Fatalf("missing required param must be rejected: %v", err)
	}

	rawSlot := validPlan()
	rawSlot.Steps[0].Params["target_desc"] = "<PROMPT>"
	if err := rawSlot.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "slot") {
		t.Fatalf("uninstantiated slot must be rejected: %v", err)
	}

	expand := validPlan()
	expand.Steps[2].Expand = &ExpandSpec{Over: "targets"}
	if err := expand.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("expand is reserved and must be rejected in v1: %v", err)
	}

	dupID := validPlan()
	dupID.Steps[1].ID = "prompt"
	if err := dupID.Validate(registry, 20); err == nil {
		t.Fatal("duplicate step id must be rejected")
	}

	refUnknown := validPlan()
	refUnknown.Steps[2].Params["targets"] = "$ghost.prompt"
	if err := refUnknown.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("reference to unknown step must be rejected: %v", err)
	}
}

func TestPlanBudgetCheck(t *testing.T) {
	registry := testVocabulary(t)
	plan := validPlan()
	plan.EstimatedJobs = 25
	// 预算超限不是校验错误——由 Submit 转预览，Validate 仅回报超限事实。
	if !plan.ExceedsJobBudget(20) {
		t.Fatal("estimated 25 must exceed budget 20")
	}
	if err := plan.Validate(registry, 20); err != nil {
		t.Fatalf("over-budget plan is still structurally valid: %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run "TestPlanValidate|TestPlanBudget" -v`
Expected: FAIL（未定义符号）

- [ ] **Step 3: 实现 plan.go**

类型按「类型定稿」节。校验核心：

```go
// Validate 是五查的结构/参数/预留三查（权限与保障切面查在调度器执行面，
// 预算查回报事实由 Submit 决策）。结构化错误供修复循环（M3）使用。
func (p ExecutionPlan) Validate(registry *vocabulary.Registry, jobBudget int) error {
	if strings.TrimSpace(p.PlanID) == "" || len(p.Steps) == 0 {
		return fmt.Errorf("%w: plan_id and steps are required", ErrPlanInvalid)
	}
	stepIDs := map[string]int{}
	for index, step := range p.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("%w: step %d id is required", ErrPlanInvalid, index)
		}
		if _, dup := stepIDs[id]; dup {
			return fmt.Errorf("%w: duplicate step id %q", ErrPlanInvalid, id)
		}
		stepIDs[id] = index
	}
	for _, step := range p.Steps {
		if step.Expand != nil {
			return fmt.Errorf("%w: step %s expand is reserved and not supported in v1", ErrPlanInvalid, step.ID)
		}
		tool, ok := registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("%w: step %s tool %q is not registered", ErrPlanInvalid, step.ID, step.Tool)
		}
		descriptor := tool.Descriptor()
		for name, spec := range descriptor.Inputs {
			if !spec.Required {
				continue
			}
			if _, bound := step.Params[name]; !bound {
				return fmt.Errorf("%w: step %s required param %q is not bound", ErrPlanInvalid, step.ID, name)
			}
		}
		for name, value := range step.Params {
			if err := validateParamValue(step.ID, name, value, stepIDs); err != nil {
				return err
			}
		}
		for _, dep := range step.DependsOn {
			if _, ok := stepIDs[dep]; !ok {
				return fmt.Errorf("%w: step %s depends on unknown step %q", ErrPlanInvalid, step.ID, dep)
			}
		}
	}
	if err := detectCycle(p.Steps); err != nil {
		return err
	}
	return nil
}

func validateParamValue(stepID, name string, value any, stepIDs map[string]int) error {
	text, isText := value.(string)
	if isText {
		if strings.HasPrefix(text, "$") {
			ref := strings.SplitN(strings.TrimPrefix(text, "$"), ".", 2)
			if len(ref) != 2 || strings.TrimSpace(ref[0]) == "" || strings.TrimSpace(ref[1]) == "" {
				return fmt.Errorf("%w: step %s param %q has malformed reference %q", ErrPlanInvalid, stepID, name, text)
			}
			if _, ok := stepIDs[ref[0]]; !ok {
				return fmt.Errorf("%w: step %s param %q reference to unknown step %q", ErrPlanInvalid, stepID, name, ref[0])
			}
			return nil
		}
		if strings.Contains(text, "<") && strings.Contains(text, ">") {
			return fmt.Errorf("%w: step %s param %q slot is not instantiated: %q", ErrPlanInvalid, stepID, name, text)
		}
		return nil
	}
	// 数组/对象内层引用递归检查
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if err := validateParamValue(stepID, name, item, stepIDs); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range typed {
			if err := validateParamValue(stepID, name, item, stepIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

func detectCycle(steps []PlanStep) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	adjacency := map[string][]string{}
	for _, step := range steps {
		adjacency[step.ID] = step.DependsOn
	}
	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range adjacency[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("%w: dependency cycle through %q", ErrPlanInvalid, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for _, step := range steps {
		if color[step.ID] == white {
			if err := visit(step.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p ExecutionPlan) ExceedsJobBudget(budget int) bool {
	return budget > 0 && p.EstimatedJobs > budget
}
```

- [ ] **Step 4: 跑测试确认通过 + Commit**

Run: `go test ./internal/aigc/orchestration -run TestPlan -v`

```bash
git add internal/aigc/orchestration/plan.go internal/aigc/orchestration/plan_test.go
git commit -m "feat(orchestration): 8要素执行计划 schema + 结构/参数/预留校验（结构化错误供修复循环）"
```

---

### Task 3: PlanRun 状态机 + 内存 store

**Files:**
- Create: `internal/aigc/orchestration/plan_run.go`（类型定稿 + 状态转移表 + 内存 store）
- Test: `internal/aigc/orchestration/plan_run_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"context"
	"errors"
	"testing"
)

func TestRunStatusTransitions(t *testing.T) {
	legal := [][2]string{
		{RunStatusDraft, RunStatusRunning}, {RunStatusRunning, RunStatusSuspended},
		{RunStatusSuspended, RunStatusRunning}, {RunStatusRunning, RunStatusSucceeded},
		{RunStatusRunning, RunStatusPartialSucceeded}, {RunStatusRunning, RunStatusFailed},
		{RunStatusDraft, RunStatusCancelled}, {RunStatusRunning, RunStatusCancelled}, {RunStatusSuspended, RunStatusCancelled},
	}
	for _, pair := range legal {
		if err := ValidateRunTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s->%s must be legal: %v", pair[0], pair[1], err)
		}
	}
	illegal := [][2]string{
		{RunStatusSucceeded, RunStatusRunning}, {RunStatusFailed, RunStatusRunning},
		{RunStatusCancelled, RunStatusRunning}, {RunStatusDraft, RunStatusSucceeded},
	}
	for _, pair := range illegal {
		if err := ValidateRunTransition(pair[0], pair[1]); err == nil {
			t.Fatalf("%s->%s must be illegal", pair[0], pair[1])
		}
	}
}

func TestMemoryRunStoreOptimisticLock(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	created, err := store.CreateRun(ctx, PlanRun{ID: "run-1", SessionID: "s1", Plan: validPlan(), Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 {
		t.Fatalf("version = %d", created.Version)
	}
	mutated, err := store.MutateRun(ctx, "run-1", created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil || mutated.Version != 2 || mutated.Status != RunStatusRunning {
		t.Fatalf("mutate: %v %+v", err, mutated)
	}
	if _, err := store.MutateRun(ctx, "run-1", created.Version, func(run *PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale version must conflict: %v", err)
	}
	// Mutate 内的状态转移必须走状态机校验。
	if _, err := store.MutateRun(ctx, "run-1", mutated.Version, func(run *PlanRun) error {
		run.Status = RunStatusDraft
		return nil
	}); err == nil {
		t.Fatal("running->draft must be rejected by transition validation")
	}
	// 深拷贝隔离：读出的 run 修改不得影响 store。
	got, _ := store.GetRun(ctx, "run-1")
	got.Nodes["x"] = &NodeRun{StepID: "x"}
	again, _ := store.GetRun(ctx, "run-1")
	if _, leaked := again.Nodes["x"]; leaked {
		t.Fatal("store must deep-copy runs")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run "TestRunStatus|TestMemoryRunStore" -v`

- [ ] **Step 3: 实现 plan_run.go**

```go
var runTransitions = map[string]map[string]struct{}{
	RunStatusDraft:     {RunStatusRunning: {}, RunStatusSuspended: {}, RunStatusCancelled: {}},
	RunStatusRunning:   {RunStatusSuspended: {}, RunStatusSucceeded: {}, RunStatusPartialSucceeded: {}, RunStatusFailed: {}, RunStatusCancelled: {}},
	RunStatusSuspended: {RunStatusRunning: {}, RunStatusCancelled: {}, RunStatusFailed: {}},
}

func ValidateRunTransition(from, to string) error {
	if from == to {
		return nil
	}
	if _, ok := runTransitions[from][to]; !ok {
		return fmt.Errorf("invalid plan run transition %q -> %q", from, to)
	}
	return nil
}

type MemoryRunStore struct {
	mu   sync.Mutex
	runs map[string]PlanRun
}

func NewMemoryRunStore() *MemoryRunStore { return &MemoryRunStore{runs: map[string]PlanRun{}} }

func (s *MemoryRunStore) CreateRun(_ context.Context, run PlanRun) (PlanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.ID]; exists {
		return PlanRun{}, fmt.Errorf("plan run %s already exists", run.ID)
	}
	run.Version = 1
	if run.Nodes == nil {
		run.Nodes = map[string]*NodeRun{}
	}
	s.runs[run.ID] = deepCopyRun(run)
	return deepCopyRun(run), nil
}

func (s *MemoryRunStore) GetRun(_ context.Context, id string) (PlanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return PlanRun{}, ErrRunNotFound
	}
	return deepCopyRun(run), nil
}

func (s *MemoryRunStore) MutateRun(_ context.Context, id string, version int, fn func(*PlanRun) error) (PlanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.runs[id]
	if !ok {
		return PlanRun{}, ErrRunNotFound
	}
	if current.Version != version {
		return PlanRun{}, fmt.Errorf("%w: have %d want %d", ErrRunVersionConflict, current.Version, version)
	}
	next := deepCopyRun(current)
	before := next.Status
	if err := fn(&next); err != nil {
		return PlanRun{}, err
	}
	if err := ValidateRunTransition(before, next.Status); err != nil {
		return PlanRun{}, err
	}
	next.Version = current.Version + 1
	s.runs[id] = deepCopyRun(next)
	return deepCopyRun(next), nil
}

func deepCopyRun(run PlanRun) PlanRun {
	// 经 JSON 往返做防御性深拷贝（run 全字段可序列化——Postgres store 也依赖这一点）。
	raw, _ := json.Marshal(run)
	var copied PlanRun
	_ = json.Unmarshal(raw, &copied)
	copied.Version = run.Version
	return copied
}
```

- [ ] **Step 4: 绿 + Commit**

```bash
git add internal/aigc/orchestration/plan_run.go internal/aigc/orchestration/plan_run_test.go
git commit -m "feat(orchestration): 计划实例状态机（挂起统一/终态映射）+ 乐观锁内存 store"
```

---

### Task 4: 调度器核心——并发就绪集执行 + 终态归纳 + 幂等

**Files:**
- Create: `internal/aigc/orchestration/scheduler.go`
- Test: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// countingTool 记录调用次数与并发峰值。
type countingTool struct {
	key      string
	calls    *atomic.Int64
	inFlight *atomic.Int64
	peak     *atomic.Int64
	outputs  map[string]any
	fail     *vocabulary.Failure
}

func (t countingTool) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{Key: t.key, Name: t.key, Description: "test", Category: "cognition"}
}

func (t countingTool) Run(_ context.Context, _ vocabulary.Call) (vocabulary.Result, error) {
	t.calls.Add(1)
	current := t.inFlight.Add(1)
	for {
		peak := t.peak.Load()
		if current <= peak || t.peak.CompareAndSwap(peak, current) {
			break
		}
	}
	defer t.inFlight.Add(-1)
	if t.fail != nil {
		return vocabulary.Result{Fail: t.fail}, nil
	}
	outputs := t.outputs
	if outputs == nil {
		outputs = map[string]any{"ok": true}
	}
	return vocabulary.Result{Outputs: outputs}, nil
}

func diamondPlan() ExecutionPlan {
	return ExecutionPlan{
		PlanID: "diamond", Source: "dynamic", Summary: "菱形", Direction: "image",
		Steps: []PlanStep{
			{ID: "a", Tool: "step_a", Required: true},
			{ID: "b", Tool: "step_bc", DependsOn: []string{"a"}, Required: true},
			{ID: "c", Tool: "step_bc", DependsOn: []string{"a"}, Required: true},
			{ID: "d", Tool: "step_d", DependsOn: []string{"b", "c"}, Required: true},
		},
	}
}

func newSchedulerForTest(t *testing.T, registry *vocabulary.Registry) (*Scheduler, *MemoryRunStore) {
	t.Helper()
	store := NewMemoryRunStore()
	idCounter := 0
	var mu sync.Mutex
	scheduler, err := NewScheduler(SchedulerConfig{
		Store: store, Vocabulary: registry, MaxParallel: 4, JobBudget: 20,
		NewID: func() string { mu.Lock(); defer mu.Unlock(); idCounter++; return fmt.Sprintf("id-%d", idCounter) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return scheduler, store
}

func TestSchedulerRunsDiamondConcurrently(t *testing.T) {
	ctx := context.Background()
	var calls, inFlight, peak atomic.Int64
	registry := vocabulary.NewRegistry()
	_ = registry.Register(countingTool{key: "step_a", calls: &calls, inFlight: &inFlight, peak: &peak})
	_ = registry.Register(countingTool{key: "step_bc", calls: &calls, inFlight: &inFlight, peak: &peak})
	_ = registry.Register(countingTool{key: "step_d", calls: &calls, inFlight: &inFlight, peak: &peak})
	scheduler, _ := newSchedulerForTest(t, registry)

	run, err := scheduler.Submit(ctx, "s1", "u1", diamondPlan())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSucceeded {
		t.Fatalf("run = %+v", run)
	}
	if calls.Load() != 4 {
		t.Fatalf("4 nodes executed once each, got %d", calls.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("b/c must run concurrently, peak = %d", peak.Load())
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if run.Nodes[id].Status != NodeStatusSucceeded {
			t.Fatalf("node %s = %+v", id, run.Nodes[id])
		}
	}
}

func TestSchedulerRequiredFailureFailsRun(t *testing.T) {
	ctx := context.Background()
	var calls, inFlight, peak atomic.Int64
	registry := vocabulary.NewRegistry()
	_ = registry.Register(countingTool{key: "step_a", calls: &calls, inFlight: &inFlight, peak: &peak,
		fail: &vocabulary.Failure{Code: "boom", Message: "业务失败"}})
	_ = registry.Register(countingTool{key: "step_bc", calls: &calls, inFlight: &inFlight, peak: &peak})
	_ = registry.Register(countingTool{key: "step_d", calls: &calls, inFlight: &inFlight, peak: &peak})
	scheduler, _ := newSchedulerForTest(t, registry)

	run, err := scheduler.Submit(ctx, "s1", "u1", diamondPlan())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusFailed {
		t.Fatalf("required node failure must fail run: %+v", run)
	}
	if run.Nodes["a"].Fail == nil || run.Nodes["a"].Fail.Code != "boom" {
		t.Fatalf("fail must be recorded as decision input: %+v", run.Nodes["a"])
	}
	if run.Nodes["d"].Status != NodeStatusPending {
		t.Fatalf("downstream must not run: %+v", run.Nodes["d"])
	}
}

func TestSchedulerOptionalFailureIsPartialSuccess(t *testing.T) {
	ctx := context.Background()
	var calls, inFlight, peak atomic.Int64
	registry := vocabulary.NewRegistry()
	_ = registry.Register(countingTool{key: "ok_tool", calls: &calls, inFlight: &inFlight, peak: &peak})
	_ = registry.Register(countingTool{key: "flaky_tool", calls: &calls, inFlight: &inFlight, peak: &peak,
		fail: &vocabulary.Failure{Code: "soft", Message: "可选失败"}})
	scheduler, _ := newSchedulerForTest(t, registry)

	plan := ExecutionPlan{PlanID: "partial", Source: "dynamic", Summary: "部分", Direction: "image",
		Steps: []PlanStep{
			{ID: "main", Tool: "ok_tool", Required: true},
			{ID: "extra", Tool: "flaky_tool", Required: false},
		}}
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusPartialSucceeded {
		t.Fatalf("optional failure => partial_succeeded, got %+v", run)
	}
}

func TestSchedulerResolvesUpstreamReferences(t *testing.T) {
	ctx := context.Background()
	registry := vocabulary.NewRegistry()
	var got atomic.Value
	_ = registry.Register(staticTool{key: "producer", run: func(_ vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Outputs: map[string]any{"prompt": "雨中柴犬"}}, nil
	}})
	_ = registry.Register(staticTool{key: "consumer",
		inputs: map[string]vocabulary.ParamSpec{"text": {Type: "string", Required: true}},
		run: func(call vocabulary.Call) (vocabulary.Result, error) {
			got.Store(call.Inputs["text"])
			return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
		}})
	scheduler, _ := newSchedulerForTest(t, registry)

	plan := ExecutionPlan{PlanID: "refs", Source: "dynamic", Summary: "引用", Direction: "image",
		Steps: []PlanStep{
			{ID: "p", Tool: "producer", Required: true},
			{ID: "c", Tool: "consumer", Params: map[string]any{"text": "$p.prompt"}, DependsOn: []string{"p"}, Required: true},
		}}
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil || run.Status != RunStatusSucceeded {
		t.Fatalf("run: %v %+v", err, run)
	}
	if got.Load() != "雨中柴犬" {
		t.Fatalf("upstream reference must resolve, got %v", got.Load())
	}
}

func TestSchedulerBudgetOverflowGoesToPreview(t *testing.T) {
	ctx := context.Background()
	registry := testVocabulary(t)
	scheduler, _ := newSchedulerForTest(t, registry)
	plan := validPlan()
	plan.EstimatedJobs = 25
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || !run.PreviewRequired {
		t.Fatalf("over-budget must suspend to preview: %+v", run)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run TestScheduler -v`

- [ ] **Step 3: 实现 scheduler.go 核心**

```go
package orchestration

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type Scheduler struct {
	store       RunStore
	vocabulary  *vocabulary.Registry
	maxParallel int
	jobBudget   int
	newID       func() string
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Store == nil || cfg.Vocabulary == nil {
		return nil, fmt.Errorf("scheduler store and vocabulary are required")
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 4
	}
	if cfg.JobBudget <= 0 {
		cfg.JobBudget = 20
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultRunID
	}
	return &Scheduler{store: cfg.Store, vocabulary: cfg.Vocabulary, maxParallel: cfg.MaxParallel, jobBudget: cfg.JobBudget, newID: cfg.NewID}, nil
}

func (s *Scheduler) Submit(ctx context.Context, sessionID, userID string, plan ExecutionPlan) (PlanRun, error) {
	if err := plan.Validate(s.vocabulary, s.jobBudget); err != nil {
		return PlanRun{}, err
	}
	run := PlanRun{
		ID: s.newID(), SessionID: sessionID, UserID: userID, Plan: plan,
		Status: RunStatusDraft, Nodes: map[string]*NodeRun{},
	}
	for _, step := range plan.Steps {
		run.Nodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
	}
	created, err := s.store.CreateRun(ctx, run)
	if err != nil {
		return PlanRun{}, err
	}
	if plan.ExceedsJobBudget(s.jobBudget) {
		return s.store.MutateRun(ctx, created.ID, created.Version, func(current *PlanRun) error {
			current.Status = RunStatusSuspended
			current.SuspendReason = SuspendWaitingUser
			current.PreviewRequired = true
			return nil
		})
	}
	if _, err := s.store.MutateRun(ctx, created.ID, created.Version, func(current *PlanRun) error {
		current.Status = RunStatusRunning
		return nil
	}); err != nil {
		return PlanRun{}, err
	}
	return s.Advance(ctx, created.ID)
}

// Advance 调度循环：每轮取就绪集并发执行，节点结果落 store 后进下一轮；
// 遇挂起或无就绪节点时归纳终态退出。挂起即释放（§3.6 ③）：本函数返回
// 后不占任何运行时资源。
func (s *Scheduler) Advance(ctx context.Context, runID string) (PlanRun, error) {
	for {
		run, err := s.store.GetRun(ctx, runID)
		if err != nil {
			return PlanRun{}, err
		}
		if run.Status != RunStatusRunning {
			return run, nil
		}
		ready := readySteps(run)
		if len(ready) == 0 {
			return s.concludeIfDone(ctx, run)
		}
		if len(ready) > s.maxParallel {
			ready = ready[:s.maxParallel]
		}
		type nodeOutcome struct {
			stepID string
			result vocabulary.Result
			err    error
		}
		outcomes := make([]nodeOutcome, len(ready))
		var wg sync.WaitGroup
		for index, step := range ready {
			wg.Add(1)
			go func(slot int, step PlanStep) {
				defer wg.Done()
				result, runErr := s.executeStep(ctx, run, step)
				outcomes[slot] = nodeOutcome{stepID: step.ID, result: result, err: runErr}
			}(index, step)
		}
		wg.Wait()
		updated, err := s.store.MutateRun(ctx, run.ID, run.Version, func(current *PlanRun) error {
			for _, outcome := range outcomes {
				node := current.Nodes[outcome.stepID]
				node.Attempt++
				switch {
				case outcome.err != nil:
					// 基础设施错误：v1 直接记失败（工具级重试属工具实现，
					// 认知类重试 ≤2 在 Plan 2 的工具实现里落）。
					node.Status = NodeStatusFailed
					node.Fail = &vocabulary.Failure{Code: "infrastructure", Message: outcome.err.Error(), Retryable: true}
				case outcome.result.Fail != nil:
					node.Status = NodeStatusFailed
					node.Fail = outcome.result.Fail
				case outcome.result.Suspension != nil:
					node.Status = NodeStatusRunning
					node.Suspension = outcome.result.Suspension
					node.ResumeKey = "resume:" + current.ID + ":" + node.StepID + ":" + fmt.Sprint(node.Attempt)
					current.Status = RunStatusSuspended
					current.SuspendReason = outcome.result.Suspension.Reason
					current.SuspendedNodeID = node.StepID
				default:
					node.Status = NodeStatusSucceeded
					node.Outputs = outcome.result.Outputs
				}
			}
			return nil
		})
		if err != nil {
			return PlanRun{}, err
		}
		if updated.Status != RunStatusRunning {
			return updated, nil
		}
		// 评估点：本轮有 Evaluate 节点成功 → 挂起 waiting_agent。
		if evaluated := firstEvaluatedNode(updated, outcomesToIDs(outcomes)); evaluated != "" {
			return s.store.MutateRun(ctx, updated.ID, updated.Version, func(current *PlanRun) error {
				current.Status = RunStatusSuspended
				current.SuspendReason = SuspendWaitingAgent
				current.SuspendedNodeID = evaluated
				node := current.Nodes[evaluated]
				node.ResumeKey = "resume:" + current.ID + ":" + evaluated + ":" + fmt.Sprint(node.Attempt)
				return nil
			})
		}
	}
}

func readySteps(run PlanRun) []PlanStep {
	steps := make([]PlanStep, 0)
	for _, step := range run.Plan.Steps {
		node := run.Nodes[step.ID]
		if node == nil || node.Status != NodeStatusPending {
			continue
		}
		blocked := false
		for _, dep := range step.DependsOn {
			depNode := run.Nodes[dep]
			if depNode == nil || (depNode.Status != NodeStatusSucceeded && depNode.Status != NodeStatusSkipped) {
				blocked = true
				break
			}
		}
		if !blocked {
			steps = append(steps, step)
		}
	}
	return steps
}

func (s *Scheduler) executeStep(ctx context.Context, run PlanRun, step PlanStep) (vocabulary.Result, error) {
	tool, ok := s.vocabulary.Get(step.Tool)
	if !ok {
		return vocabulary.Result{}, fmt.Errorf("tool %q disappeared from vocabulary", step.Tool)
	}
	inputs, err := resolveParams(run, step.Params)
	if err != nil {
		return vocabulary.Result{}, err
	}
	node := run.Nodes[step.ID]
	call := vocabulary.Call{
		SessionID: run.SessionID, UserID: run.UserID, PlanRunID: run.ID, NodeID: step.ID,
		Attempt:        node.Attempt + 1,
		IdempotencyKey: fmt.Sprintf("plan:%s:%s:%d", run.ID, step.ID, node.Attempt+1),
		Inputs:         inputs,
	}
	return tool.Run(ctx, call)
}

func resolveParams(run PlanRun, params map[string]any) (map[string]any, error) {
	resolved := map[string]any{}
	for name, value := range params {
		item, err := resolveValue(run, value)
		if err != nil {
			return nil, fmt.Errorf("param %s: %w", name, err)
		}
		resolved[name] = item
	}
	return resolved, nil
}

func resolveValue(run PlanRun, value any) (any, error) {
	switch typed := value.(type) {
	case string:
		if !strings.HasPrefix(typed, "$") {
			return typed, nil
		}
		ref := strings.SplitN(strings.TrimPrefix(typed, "$"), ".", 2)
		node := run.Nodes[ref[0]]
		if node == nil || node.Status != NodeStatusSucceeded {
			return nil, fmt.Errorf("reference %q upstream not succeeded", typed)
		}
		output, ok := node.Outputs[ref[1]]
		if !ok {
			return nil, fmt.Errorf("reference %q output missing", typed)
		}
		return output, nil
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			resolved, err := resolveValue(run, item)
			if err != nil {
				return nil, err
			}
			items[index] = resolved
		}
		return items, nil
	case map[string]any:
		items := map[string]any{}
		for key, item := range typed {
			resolved, err := resolveValue(run, item)
			if err != nil {
				return nil, err
			}
			items[key] = resolved
		}
		return items, nil
	default:
		return value, nil
	}
}

// concludeIfDone 终态归纳（§3.6 终态映射）。
func (s *Scheduler) concludeIfDone(ctx context.Context, run PlanRun) (PlanRun, error) {
	requiredFailed, anyFailedOrSkipped, allDone := false, false, true
	for _, step := range run.Plan.Steps {
		node := run.Nodes[step.ID]
		switch node.Status {
		case NodeStatusSucceeded:
		case NodeStatusSkipped:
			anyFailedOrSkipped = true
		case NodeStatusFailed:
			anyFailedOrSkipped = true
			if step.Required {
				requiredFailed = true
			}
		default:
			allDone = false
		}
	}
	target := ""
	switch {
	case requiredFailed:
		target = RunStatusFailed
	case allDone && anyFailedOrSkipped:
		target = RunStatusPartialSucceeded
	case allDone:
		target = RunStatusSucceeded
	default:
		// 未完成但无就绪节点且未挂起 = 必需依赖失败导致下游饿死 → failed。
		target = RunStatusFailed
	}
	return s.store.MutateRun(ctx, run.ID, run.Version, func(current *PlanRun) error {
		current.Status = target
		current.SuspendReason = ""
		current.SuspendedNodeID = ""
		return nil
	})
}

func firstEvaluatedNode(run PlanRun, executedIDs map[string]struct{}) string {
	for _, step := range run.Plan.Steps {
		if !step.Evaluate {
			continue
		}
		if _, executed := executedIDs[step.ID]; !executed {
			continue
		}
		if run.Nodes[step.ID].Status == NodeStatusSucceeded {
			return step.ID
		}
	}
	return ""
}
```

（`outcomesToIDs` 为 5 行辅助：收集本轮 stepID 集合。`defaultRunID` 用 `crypto/rand` 十六进制。）

- [ ] **Step 4: 绿（含 -race）+ Commit**

Run: `go test ./internal/aigc/orchestration -run TestScheduler -race -v`

```bash
git add internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): 计划调度器——并发就绪集执行/上游引用解析/终态归纳/预算转预览"
```

---

### Task 5: 挂起/恢复 + 幂等（§6.3 可行性判据一）

**Files:**
- Modify: `internal/aigc/orchestration/scheduler.go`（Resume 实现）
- Test: `internal/aigc/orchestration/scheduler_resume_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// suspendingTool 首次执行声明挂起；恢复后由调度器直接采纳决策为输出。
func confirmTool() vocabulary.Tool {
	return staticTool{key: "request_confirmation",
		inputs: map[string]vocabulary.ParamSpec{"question": {Type: "string", Required: true}},
		run: func(call vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{
				Reason:  SuspendWaitingUser,
				Payload: map[string]any{"question": call.Inputs["question"]},
			}}, nil
		}}
}

func TestSchedulerSuspendResumeAcrossInstances(t *testing.T) {
	ctx := context.Background()
	var afterCalls atomic.Int64
	registry := vocabulary.NewRegistry()
	_ = registry.Register(staticTool{key: "before", run: func(_ vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Outputs: map[string]any{"prompt": "雨中柴犬"}}, nil
	}})
	_ = registry.Register(confirmTool())
	_ = registry.Register(staticTool{key: "after",
		inputs: map[string]vocabulary.ParamSpec{"text": {Type: "string", Required: true}},
		run: func(call vocabulary.Call) (vocabulary.Result, error) {
			afterCalls.Add(1)
			return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
		}})

	plan := ExecutionPlan{PlanID: "with-gate", Source: "dynamic", Summary: "带卡点", Direction: "image",
		Steps: []PlanStep{
			{ID: "prep", Tool: "before", Required: true},
			{ID: "gate", Tool: "request_confirmation", Params: map[string]any{"question": "可以吗"}, DependsOn: []string{"prep"}, Required: true},
			{ID: "final", Tool: "after", Params: map[string]any{"text": "$prep.prompt"}, DependsOn: []string{"gate"}, Required: true},
		}}

	scheduler, store := newSchedulerForTest(t, registry)
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || run.SuspendedNodeID != "gate" {
		t.Fatalf("must suspend at gate: %+v", run)
	}
	if run.Nodes["gate"].Suspension.Payload["question"] != "可以吗" {
		t.Fatalf("suspension payload lost: %+v", run.Nodes["gate"])
	}
	if afterCalls.Load() != 0 {
		t.Fatal("downstream must not run before resume")
	}
	resumeKey := run.Nodes["gate"].ResumeKey
	if resumeKey == "" {
		t.Fatal("suspended node must carry resume key")
	}

	// 挂起即释放的证明：丢弃原调度器，从同一 store 新建实例恢复。
	fresh, err := NewScheduler(SchedulerConfig{Store: store, Vocabulary: registry})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fresh.Resume(ctx, run.ID, resumeKey, map[string]any{"decision": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != RunStatusSucceeded {
		t.Fatalf("resumed run must complete: %+v", resumed)
	}
	if afterCalls.Load() != 1 {
		t.Fatalf("downstream must run exactly once, got %d", afterCalls.Load())
	}
	if resumed.Nodes["gate"].Outputs["decision"] != "approved" {
		t.Fatalf("decision must be node output: %+v", resumed.Nodes["gate"])
	}

	// 恢复幂等：同 resumeKey 重复调用返回同一结果、不重跑下游。
	again, err := fresh.Resume(ctx, run.ID, resumeKey, map[string]any{"decision": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != RunStatusSucceeded || afterCalls.Load() != 1 {
		t.Fatalf("resume must be idempotent: %+v calls=%d", again, afterCalls.Load())
	}

	// 错误 resumeKey 拒绝。
	if _, err := fresh.Resume(ctx, run.ID, "resume:bogus", map[string]any{}); !errors.Is(err, ErrResumeKeyMismatch) {
		t.Fatalf("bogus resume key must be rejected: %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run TestSchedulerSuspendResume -v`

- [ ] **Step 3: 实现 Resume**

```go
// Resume 恢复 waiting_user / waiting_agent 挂起。一次性幂等：已用
// resumeKey 的重复调用直接返回当前 run（不再推进也不报错）。
func (s *Scheduler) Resume(ctx context.Context, runID, resumeKey string, decision map[string]any) (PlanRun, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	node := findNodeByResumeKey(run, resumeKey)
	if node == nil {
		return PlanRun{}, fmt.Errorf("%w: %s", ErrResumeKeyMismatch, resumeKey)
	}
	if node.Resumed {
		return run, nil // 幂等重放
	}
	if run.Status != RunStatusSuspended || run.SuspendedNodeID != node.StepID {
		return PlanRun{}, fmt.Errorf("%w: run is not suspended on node %s", ErrResumeKeyMismatch, node.StepID)
	}
	updated, err := s.store.MutateRun(ctx, run.ID, run.Version, func(current *PlanRun) error {
		target := current.Nodes[node.StepID]
		target.Resumed = true
		target.Status = NodeStatusSucceeded
		if target.Outputs == nil {
			target.Outputs = map[string]any{}
		}
		for key, value := range decision {
			target.Outputs[key] = value
		}
		target.Suspension = nil
		current.Status = RunStatusRunning
		current.SuspendReason = ""
		current.SuspendedNodeID = ""
		return nil
	})
	if err != nil {
		return PlanRun{}, err
	}
	return s.Advance(ctx, updated.ID)
}

func findNodeByResumeKey(run PlanRun, resumeKey string) *NodeRun {
	if strings.TrimSpace(resumeKey) == "" {
		return nil
	}
	for _, node := range run.Nodes {
		if node.ResumeKey == resumeKey {
			return node
		}
	}
	return nil
}
```

- [ ] **Step 4: 绿（-race）+ Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "feat(orchestration): 卡点挂起/跨实例恢复/一次性幂等 resume（挂起即释放的物理证明）"
```

---

### Task 6: 活计划修订（§6.3 可行性判据二）

**Files:**
- Modify: `internal/aigc/orchestration/scheduler.go`（Revise 实现）
- Test: `internal/aigc/orchestration/scheduler_revise_test.go`

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func TestReviseSuspendedRunSkipAndAppend(t *testing.T) {
	ctx := context.Background()
	registry := vocabulary.NewRegistry()
	_ = registry.Register(staticTool{key: "before", run: func(_ vocabulary.Call) (vocabulary.Result, error) {
		return vocabulary.Result{Outputs: map[string]any{"prompt": "v1"}}, nil
	}})
	_ = registry.Register(confirmTool())
	_ = registry.Register(staticTool{key: "old_tail"})
	_ = registry.Register(staticTool{key: "new_tail"})

	plan := ExecutionPlan{PlanID: "revisable", Source: "dynamic", Summary: "可修订", Direction: "image",
		Steps: []PlanStep{
			{ID: "prep", Tool: "before", Required: true},
			{ID: "gate", Tool: "request_confirmation", Params: map[string]any{"question": "继续?"}, DependsOn: []string{"prep"}, Required: true},
			{ID: "tail", Tool: "old_tail", DependsOn: []string{"gate"}, Required: true},
		}}
	scheduler, _ := newSchedulerForTest(t, registry)
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil || run.Status != RunStatusSuspended {
		t.Fatalf("setup: %v %+v", err, run)
	}

	// 修订：裁掉 tail（置 skipped），追加 new_tail 接在 gate 后。
	revised, err := scheduler.Revise(ctx, run.ID, PlanRevision{
		SkipStepIDs: []string{"tail"},
		AppendSteps: []PlanStep{{ID: "tail2", Tool: "new_tail", DependsOn: []string{"gate"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Nodes["tail"].Status != NodeStatusSkipped {
		t.Fatalf("skipped node: %+v", revised.Nodes["tail"])
	}
	if _, ok := revised.Nodes["tail2"]; !ok {
		t.Fatal("appended node must exist")
	}

	// 修订不改变挂起态；resume 后按新结构跑完。
	resumed, err := scheduler.Resume(ctx, run.ID, revised.Nodes["gate"].ResumeKey, map[string]any{"decision": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != RunStatusPartialSucceeded { // 存在 skipped → partial
		t.Fatalf("resumed: %+v", resumed)
	}
	if resumed.Nodes["tail2"].Status != NodeStatusSucceeded {
		t.Fatalf("appended node must run: %+v", resumed.Nodes["tail2"])
	}

	// 动已执行节点必须报错。
	if _, err := scheduler.Revise(ctx, run.ID, PlanRevision{SkipStepIDs: []string{"prep"}}); !errors.Is(err, ErrReviseExecutedStep) {
		t.Fatalf("revising executed step must fail: %v", err)
	}

	// 追加引用未知依赖必须报错（复用计划校验）。
	if _, err := scheduler.Revise(ctx, run.ID, PlanRevision{
		AppendSteps: []PlanStep{{ID: "bad", Tool: "new_tail", DependsOn: []string{"ghost"}, Required: true}},
	}); err == nil {
		t.Fatal("appending step with unknown dependency must fail")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run TestRevise -v`

- [ ] **Step 3: 实现 Revise**

```go
// Revise 活计划修订（§3.6 ②）：续编=追加；修订=裁未执行（置 skipped）；
// 已执行永不回滚。terminal 态运行拒绝修订。
func (s *Scheduler) Revise(ctx context.Context, runID string, revision PlanRevision) (PlanRun, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	if run.Status != RunStatusRunning && run.Status != RunStatusSuspended && run.Status != RunStatusDraft {
		return PlanRun{}, fmt.Errorf("plan run %s is terminal and cannot be revised", runID)
	}
	return s.store.MutateRun(ctx, run.ID, run.Version, func(current *PlanRun) error {
		for _, skipID := range revision.SkipStepIDs {
			node, ok := current.Nodes[skipID]
			if !ok {
				return fmt.Errorf("%w: skip target %q not found", ErrPlanInvalid, skipID)
			}
			if node.Status != NodeStatusPending {
				return fmt.Errorf("%w: %s is %s", ErrReviseExecutedStep, skipID, node.Status)
			}
			node.Status = NodeStatusSkipped
		}
		for _, step := range revision.AppendSteps {
			if _, exists := current.Nodes[step.ID]; exists {
				return fmt.Errorf("%w: appended step id %q already exists", ErrPlanInvalid, step.ID)
			}
			current.Plan.Steps = append(current.Plan.Steps, step)
			current.Nodes[step.ID] = &NodeRun{StepID: step.ID, Status: NodeStatusPending}
		}
		// 修订后的整计划重过校验（引用/环/工具存在）。
		if err := current.Plan.Validate(s.vocabulary, 0); err != nil {
			return err
		}
		return nil
	})
}
```

- [ ] **Step 4: 绿 + 判据结论**

Run: `go test ./internal/aigc/orchestration -race`
Expected: 全部 PASS。**至此 §6.3 可行性判据（跨实例恢复/幂等/活修订）全部有测试背书——自写调度器路径证实**；若本任务或 Task 5 无法达成，停止执行并回终版 §6.3 重议。

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "feat(orchestration): 活计划修订——裁未执行置skipped/追加续编/已执行不可动（§6.3 判据闭环）"
```

---

### Task 7: 词汇 `write_media_prompt`（认知类第一个，统一契约改造）

**Files:**
- Create: `internal/aigc/vocabulary/tools/write_media_prompt.go`
- Test: `internal/aigc/vocabulary/tools/write_media_prompt_test.go`

依据：§1.2——"任何生成派发前为目标备提示词；模板按目标类型分发；不选模型不发起生成"。现有实现参考 `internal/aigc/tools/write_prompt.go`（ChatModel 调用形态）与 `mediagraph` 的 writeGenerationPrompts 节点；本任务把「调一次 ChatModel 产一个提示词」装进统一契约，ChatModel 以接口注入。

- [ ] **Step 1: 写失败测试**

```go
package tools

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type scriptedChat struct{ reply string }

func (c scriptedChat) Complete(_ context.Context, prompt string) (string, error) {
	return c.reply, nil
}

func TestWriteMediaPromptTool(t *testing.T) {
	tool := NewWriteMediaPrompt(scriptedChat{reply: "一只柴犬在雨中撑伞，电影感光效"})
	descriptor := tool.Descriptor()
	if descriptor.Key != "write_media_prompt" || descriptor.Category != "cognition" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	result, err := tool.Run(context.Background(), vocabulary.Call{
		SessionID: "s1", IdempotencyKey: "k1",
		Inputs: map[string]any{"target_desc": "雨中柴犬", "media_kind": "image"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fail != nil || result.Suspension != nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Outputs["prompt"] != "一只柴犬在雨中撑伞，电影感光效" {
		t.Fatalf("outputs = %+v", result.Outputs)
	}

	missing, err := tool.Run(context.Background(), vocabulary.Call{Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Fail == nil || missing.Fail.Code != "invalid_inputs" {
		t.Fatalf("missing inputs must be a business fail (decision input): %+v", missing)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/vocabulary/tools -run TestWriteMediaPrompt -v`

- [ ] **Step 3: 实现**

```go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// ChatCompleter 是认知类工具对模型栈的最小依赖（Provider 适配器在
// 工具层之下，§1.3）。
type ChatCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

type writeMediaPrompt struct{ chat ChatCompleter }

func NewWriteMediaPrompt(chat ChatCompleter) vocabulary.Tool { return writeMediaPrompt{chat: chat} }

func (t writeMediaPrompt) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{
		Key: "write_media_prompt", Name: "提示词编写", Category: "cognition",
		Description: "任何生成派发前为目标备提示词；按 media_kind 分发提示词模板；不选模型不发起生成。",
		Inputs: map[string]vocabulary.ParamSpec{
			"target_desc": {Type: "string", Desc: "生成目标的自然语言描述", Required: true},
			"media_kind":  {Type: "string", Desc: "image|video|music|audio", Required: true},
			"style_hints": {Type: "string", Desc: "风格约束（可选）"},
		},
		Outputs: map[string]vocabulary.ParamSpec{
			"prompt": {Type: "string", Desc: "可直接派发的生成提示词"},
		},
	}
}

func (t writeMediaPrompt) Run(ctx context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	targetDesc, _ := call.Inputs["target_desc"].(string)
	mediaKind, _ := call.Inputs["media_kind"].(string)
	if strings.TrimSpace(targetDesc) == "" || strings.TrimSpace(mediaKind) == "" {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "invalid_inputs", Message: "target_desc 与 media_kind 必填"}}, nil
	}
	styleHints, _ := call.Inputs["style_hints"].(string)
	instruction := promptTemplateFor(mediaKind, targetDesc, styleHints)
	reply, err := t.chat.Complete(ctx, instruction)
	if err != nil {
		return vocabulary.Result{}, fmt.Errorf("write_media_prompt chat: %w", err)
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "empty_completion", Message: "模型未产出提示词", Retryable: true}}, nil
	}
	return vocabulary.Result{Outputs: map[string]any{"prompt": reply}}, nil
}

func promptTemplateFor(mediaKind, targetDesc, styleHints string) string {
	base := map[string]string{
		"image": "为以下图像生成目标撰写一条中文生成提示词（构图/主体/光效/风格要素齐备），只输出提示词本身：",
		"video": "为以下视频镜头撰写一条中文生成提示词（主体/动作/镜头运动/氛围），只输出提示词本身：",
		"music": "为以下音乐需求撰写一条生成提示词（风格/情绪/节奏/配器），只输出提示词本身：",
		"audio": "为以下配音需求撰写朗读文本与语气说明，只输出内容本身：",
	}[strings.ToLower(strings.TrimSpace(mediaKind))]
	if base == "" {
		base = "为以下生成目标撰写一条中文生成提示词，只输出提示词本身："
	}
	text := base + targetDesc
	if strings.TrimSpace(styleHints) != "" {
		text += "。风格约束：" + styleHints
	}
	return text
}
```

- [ ] **Step 4: 绿 + Commit**

```bash
git add internal/aigc/vocabulary/tools/
git commit -m "feat(vocabulary): write_media_prompt 认知工具——统一契约首例（模板按模态分发/不选模型不派发）"
```

---

### Task 8: 词汇 `request_confirmation`（交互类，卡点载体）

**Files:**
- Create: `internal/aigc/vocabulary/tools/request_confirmation.go`
- Test: `internal/aigc/vocabulary/tools/request_confirmation_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tools

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func TestRequestConfirmationSuspends(t *testing.T) {
	tool := NewRequestConfirmation()
	result, err := tool.Run(context.Background(), vocabulary.Call{
		PlanRunID: "run-1", NodeID: "gate",
		Inputs: map[string]any{
			"question": "提示词可以吗",
			"options":  []any{"确认", "调整"},
			"preview":  map[string]any{"prompt": "雨中柴犬"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Suspension == nil || result.Suspension.Reason != "waiting_user" {
		t.Fatalf("must declare waiting_user suspension: %+v", result)
	}
	if result.Suspension.Payload["question"] != "提示词可以吗" {
		t.Fatalf("payload = %+v", result.Suspension.Payload)
	}
	empty, err := tool.Run(context.Background(), vocabulary.Call{Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Fail == nil {
		t.Fatalf("empty question must fail: %+v", empty)
	}
}
```

- [ ] **Step 2: 失败确认**

Run: `go test ./internal/aigc/vocabulary/tools -run TestRequestConfirmation -v`

- [ ] **Step 3: 实现**

```go
package tools

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type requestConfirmation struct{}

func NewRequestConfirmation() vocabulary.Tool { return requestConfirmation{} }

func (requestConfirmation) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{
		Key: "request_confirmation", Name: "确认卡点", Category: "interaction",
		Description: "封闭选项确认卡：计划在此节点挂起等待用户决定（复用 interrupt 链路，决定经 resume 注入）。",
		Inputs: map[string]vocabulary.ParamSpec{
			"question": {Type: "string", Desc: "向用户提出的问题", Required: true},
			"options":  {Type: "array", Desc: "封闭选项（默认 确认/取消）"},
			"preview":  {Type: "object", Desc: "随卡展示的预览内容"},
		},
		Outputs: map[string]vocabulary.ParamSpec{
			"decision": {Type: "string", Desc: "用户选择（resume 注入）"},
		},
	}
}

func (requestConfirmation) Run(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	question, _ := call.Inputs["question"].(string)
	if strings.TrimSpace(question) == "" {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "invalid_inputs", Message: "question 必填"}}, nil
	}
	payload := map[string]any{"question": question}
	if options, ok := call.Inputs["options"]; ok {
		payload["options"] = options
	}
	if preview, ok := call.Inputs["preview"]; ok {
		payload["preview"] = preview
	}
	return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: "waiting_user", Payload: payload}}, nil
}
```

- [ ] **Step 4: 绿 + Commit**

```bash
git add internal/aigc/vocabulary/tools/
git commit -m "feat(vocabulary): request_confirmation 交互工具——卡点=计划节点（一切执行皆计划）"
```

---

### Task 9: 词汇 `dispatch_generation`（数据类，异步派发 + 预注册）

**Files:**
- Create: `internal/aigc/vocabulary/tools/dispatch_generation.go`
- Test: `internal/aigc/vocabulary/tools/dispatch_generation_test.go`

依据：§1.4——"批量建 job 入队；同时预注册资产记录（generating 态）；幂等键=计划实例+节点+目标+派发序号 attempt"。工具的接口与语义完全由设计定义；实现载体是被改造的 `generation/` 异步链（建 job 入队走其命令服务与队列——这是 §4 异步中心的机制层）与 `asset/` 存储（预注册占位）。绑定目标多态按 §5 两态：`session_deliverable`（默认）| `storyboard_slot`（带 storyboard_id 的 target），token 结构即 §5 的落地形态。

- [ ] **Step 1: 写失败测试**

```go
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type memoryAssetStore struct{ saved []asset.Asset }

func (s *memoryAssetStore) Save(_ context.Context, value asset.Asset) (asset.Asset, error) {
	s.saved = append(s.saved, value)
	return value, nil
}

func TestDispatchGenerationCreatesBatchAndPreregistersAssets(t *testing.T) {
	ctx := context.Background()
	store := generation.NewMemoryStore()
	assets := &memoryAssetStore{}
	idCounter := 0
	tool := NewDispatchGeneration(DispatchGenerationConfig{
		Commands: generation.NewCommandService(generation.CommandServiceConfig{Store: store}),
		Assets:   assets,
		NewID:    func() string { idCounter++; return fmt.Sprintf("id-%d", idCounter) },
	})

	result, err := tool.Run(ctx, vocabulary.Call{
		SessionID: "s1", UserID: "u1", PlanRunID: "run-1", NodeID: "generate", Attempt: 1,
		IdempotencyKey: "plan:run-1:generate:1",
		Inputs: map[string]any{
			"media_kind": "image",
			"targets": []any{
				map[string]any{"prompt": "雨中柴犬 全身"},
				map[string]any{"prompt": "雨中柴犬 特写"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fail != nil {
		t.Fatalf("fail = %+v", result.Fail)
	}
	// 派发即挂起 waiting_jobs，payload 带批次引用。
	if result.Suspension == nil || result.Suspension.Reason != "waiting_jobs" {
		t.Fatalf("dispatch must suspend on jobs: %+v", result)
	}
	batchID, _ := result.Suspension.Payload["batch_id"].(string)
	if batchID == "" {
		t.Fatalf("payload must carry batch_id: %+v", result.Suspension.Payload)
	}

	jobs, err := store.ListBySession(ctx, "s1")
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs: %v %d", err, len(jobs))
	}
	seenKeys := map[string]struct{}{}
	for _, job := range jobs {
		if job.Provider != generation.ProviderImage2 {
			t.Fatalf("provider = %s", job.Provider)
		}
		if job.BindingToken.NormalizedKind() != generation.TargetKindSessionDeliverable {
			t.Fatalf("token = %+v", job.BindingToken)
		}
		if !strings.Contains(job.IdempotencyKey, "plan:run-1:generate:") || !strings.HasSuffix(job.IdempotencyKey, ":attempt:1") {
			t.Fatalf("idempotency key must be plan+node+target+attempt: %s", job.IdempotencyKey)
		}
		seenKeys[job.IdempotencyKey] = struct{}{}
	}
	if len(seenKeys) != 2 {
		t.Fatal("per-target idempotency keys must differ")
	}
	// 预注册：每 job 一条 generating 态资产占位。
	if len(assets.saved) != 2 {
		t.Fatalf("preregistered assets = %d", len(assets.saved))
	}
	for _, saved := range assets.saved {
		if saved.Availability != asset.AvailabilityGenerating || saved.SourceJobID == "" {
			t.Fatalf("preregistered asset = %+v", saved)
		}
	}

	// 未知 media_kind 是业务失败（决策输入）。
	bad, err := tool.Run(ctx, vocabulary.Call{SessionID: "s1", IdempotencyKey: "k2",
		Inputs: map[string]any{"media_kind": "hologram", "targets": []any{map[string]any{"prompt": "x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Fail == nil || bad.Fail.Code != "unknown_media_kind" {
		t.Fatalf("bad = %+v", bad)
	}
}
```

**前置小步（同属本任务）**：`asset/models.go` 现有可用态只有 pending_billing/available/quarantined/deleted（取证核实），预注册需要**新增**常量——在 :21 常量块加 `AvailabilityGenerating = "generating"`，并在 `NormalizeAvailability` 的 switch 中加对应分支（不影响任何既有态的归一化）；`go test ./internal/aigc/asset` 回归。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/vocabulary/tools -run TestDispatchGeneration -v`

- [ ] **Step 3: 实现**

```go
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type AssetPreRegistrar interface {
	Save(ctx context.Context, value asset.Asset) (asset.Asset, error)
}

type DispatchGenerationConfig struct {
	Commands *generation.CommandService
	Assets   AssetPreRegistrar
	NewID    func() string
}

type dispatchGeneration struct{ cfg DispatchGenerationConfig }

func NewDispatchGeneration(cfg DispatchGenerationConfig) vocabulary.Tool {
	return dispatchGeneration{cfg: cfg}
}

func (t dispatchGeneration) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{
		Key: "dispatch_generation", Name: "生成派发", Category: "data",
		Description: "批量建 job 入队并预注册 generating 态资产占位；计划在此挂起等待批次终结唤醒。幂等键=计划实例+节点+目标+派发序号。",
		Inputs: map[string]vocabulary.ParamSpec{
			"media_kind": {Type: "string", Desc: "image|video|music|audio", Required: true},
			"targets":    {Type: "array", Desc: "生成目标数组：{prompt, storyboard_id?, target_id?, asset_slot?}", Required: true},
			"ratio":      {Type: "string", Desc: "宽高比（可选）"},
		},
		Outputs: map[string]vocabulary.ParamSpec{
			"batch_id":     {Type: "string", Desc: "批次引用"},
			"operation_id": {Type: "string", Desc: "操作引用"},
			"job_count":    {Type: "int", Desc: "派发数量"},
		},
	}
}

func (t dispatchGeneration) Run(ctx context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	mediaKind, _ := call.Inputs["media_kind"].(string)
	mediaKind = strings.ToLower(strings.TrimSpace(mediaKind))
	provider := providerForKind(mediaKind)
	if provider == "" {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "unknown_media_kind", Message: "media_kind 必须是 image|video|music|audio"}}, nil
	}
	rawTargets, _ := call.Inputs["targets"].([]any)
	if len(rawTargets) == 0 {
		return vocabulary.Result{Fail: &vocabulary.Failure{Code: "invalid_inputs", Message: "targets 不能为空"}}, nil
	}
	ratio, _ := call.Inputs["ratio"].(string)
	policy := generation.DeliveryPolicy{BindingMode: generation.BindingModeActive, ApprovalPolicy: generation.ApprovalAutoApprove, ChargePolicy: generation.ChargePostpaidNoReservation}
	operationID, batchID := t.cfg.NewID(), t.cfg.NewID()
	jobs := make([]generation.GenerationJob, 0, len(rawTargets))
	for index, rawTarget := range rawTargets {
		target, _ := rawTarget.(map[string]any)
		prompt, _ := target["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			return vocabulary.Result{Fail: &vocabulary.Failure{Code: "invalid_inputs", Message: fmt.Sprintf("targets[%d].prompt 必填", index)}}, nil
		}
		token := bindingTokenFor(target, t.cfg.NewID)
		digest := sha256.Sum256([]byte(strings.Join([]string{prompt, mediaKind, ratio, strconv.Itoa(index)}, "\x00")))
		token.InputFingerprint = hex.EncodeToString(digest[:])
		payload := map[string]any{"prompt": prompt, "media_kind": mediaKind, "user_id": call.UserID}
		if strings.TrimSpace(ratio) != "" {
			payload["ratio"] = ratio
		}
		jobID := t.cfg.NewID()
		jobs = append(jobs, generation.GenerationJob{
			ID: jobID, SessionID: call.SessionID, UserID: call.UserID,
			// §1.4：幂等键 = 计划实例+节点+目标+派发序号（用户调整重派递增 Attempt 不消耗重试预算）。
			IdempotencyKey: fmt.Sprintf("plan:%s:%s:target:%d:attempt:%d", call.PlanRunID, call.NodeID, index, call.Attempt),
			Provider:       provider, MediaKind: mediaKind,
			TargetType: token.NormalizedKind(), TargetID: token.TargetID, AssetSlot: token.AssetSlot,
			StoryboardID: token.StoryboardID, Required: true,
			BindingToken: token, DeliveryPolicy: policy, MaxAttempts: 4,
			Payload: payload,
		})
		if t.cfg.Assets != nil {
			if _, err := t.cfg.Assets.Save(ctx, asset.Asset{
				ID: t.cfg.NewID(), SessionID: call.SessionID, UserID: call.UserID,
				SourceJobID: jobID, Kind: mediaKind, Source: "generation",
				Availability: asset.AvailabilityGenerating,
				Metadata:     map[string]any{"plan_run_id": call.PlanRunID, "node_id": call.NodeID, "target_kind": token.NormalizedKind()},
			}); err != nil {
				return vocabulary.Result{}, fmt.Errorf("preregister asset: %w", err)
			}
		}
	}
	workflow, _, err := t.cfg.Commands.Create(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: operationID, SessionID: call.SessionID, UserID: call.UserID, IdempotencyKey: call.IdempotencyKey, Kind: "plan_dispatch", Status: generation.OperationStatusAccepted, BatchID: batchID},
		Batch:     generation.GenerationBatch{ID: batchID, SessionID: call.SessionID, UserID: call.UserID, OperationID: operationID, Kind: "plan_dispatch", CompletionPolicy: generation.CompletionAllowPartial, WakePolicy: generation.WakeOnTerminal, DeliveryPolicy: policy},
		Jobs:      jobs,
	})
	if err != nil {
		return vocabulary.Result{}, fmt.Errorf("create generation workflow: %w", err)
	}
	return vocabulary.Result{
		Outputs: map[string]any{"batch_id": workflow.Batch.ID, "operation_id": workflow.Operation.ID, "job_count": len(workflow.Jobs)},
		Suspension: &vocabulary.Suspension{Reason: "waiting_jobs", Payload: map[string]any{
			"batch_id": workflow.Batch.ID, "operation_id": workflow.Operation.ID, "job_count": len(workflow.Jobs),
		}},
	}, nil
}

func bindingTokenFor(target map[string]any, newID func() string) generation.BindingToken {
	storyboardID, _ := target["storyboard_id"].(string)
	targetID, _ := target["target_id"].(string)
	assetSlot, _ := target["asset_slot"].(string)
	if strings.TrimSpace(storyboardID) != "" {
		return generation.BindingToken{StoryboardID: storyboardID, TargetID: targetID, AssetSlot: valueOr(assetSlot, "primary")}
	}
	if strings.TrimSpace(targetID) == "" {
		targetID = "deliverable:" + newID()
	}
	return generation.BindingToken{TargetKind: generation.TargetKindSessionDeliverable, TargetID: targetID, AssetSlot: valueOr(assetSlot, "primary"), TargetRevision: 1}
}

func providerForKind(kind string) string {
	switch kind {
	case "video":
		return generation.ProviderSeedance
	case "audio", "music":
		return generation.ProviderAudio
	case "image":
		return generation.ProviderImage2
	default:
		return ""
	}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
```

- [ ] **Step 4: 绿 + Commit**

```bash
git add internal/aigc/vocabulary/tools/
git commit -m "feat(vocabulary): dispatch_generation 数据工具——批量派发+预注册generating资产+计划级幂等键+waiting_jobs挂起"
```

---

### Task 10: waiting_jobs 唤醒——`CompleteJobsWait` + 与真实 barrier 的端到端

**Files:**
- Modify: `internal/aigc/orchestration/scheduler.go`（CompleteJobsWait）
- Test: `internal/aigc/orchestration/jobs_wait_test.go`

- [ ] **Step 1: 写失败测试（调度层单测 + 与 generation memory store/barrier 的集成）**

```go
package orchestration

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func dispatchLikeTool(batchID string) vocabulary.Tool {
	return staticTool{key: "dispatch_generation",
		inputs: map[string]vocabulary.ParamSpec{"targets": {Type: "array", Required: true}},
		run: func(_ vocabulary.Call) (vocabulary.Result, error) {
			return vocabulary.Result{
				Outputs:    map[string]any{"batch_id": batchID},
				Suspension: &vocabulary.Suspension{Reason: SuspendWaitingJobs, Payload: map[string]any{"batch_id": batchID}},
			}, nil
		}}
}

func TestCompleteJobsWaitResumesPlan(t *testing.T) {
	ctx := context.Background()
	registry := vocabulary.NewRegistry()
	_ = registry.Register(dispatchLikeTool("batch-1"))
	_ = registry.Register(staticTool{key: "after_jobs"})
	scheduler, store := newSchedulerForTest(t, registry)

	plan := ExecutionPlan{PlanID: "jobs-plan", Source: "dynamic", Summary: "派发", Direction: "image",
		Steps: []PlanStep{
			{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "x"}}}, Required: true},
			{ID: "wrap", Tool: "after_jobs", DependsOn: []string{"generate"}, Required: true},
		}}
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingJobs || run.SuspendedNodeID != "generate" {
		t.Fatalf("must suspend on jobs: %+v", run)
	}

	// 唤醒：批次完成（新实例，模拟进程重启后由 wakeup 通道触达）。
	fresh, _ := NewScheduler(SchedulerConfig{Store: store, Vocabulary: registry})
	woken, err := fresh.CompleteJobsWait(ctx, run.ID, "generate", JobsOutcome{BatchID: "batch-1", Status: "completed", Summary: map[string]any{"succeeded": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if woken.Status != RunStatusSucceeded {
		t.Fatalf("woken run: %+v", woken)
	}
	if woken.Nodes["generate"].Outputs["batch_status"] != "completed" {
		t.Fatalf("batch outcome must merge into node outputs: %+v", woken.Nodes["generate"])
	}

	// 幂等：重复唤醒不改变结果。
	again, err := fresh.CompleteJobsWait(ctx, run.ID, "generate", JobsOutcome{BatchID: "batch-1", Status: "completed"})
	if err != nil || again.Status != RunStatusSucceeded {
		t.Fatalf("duplicate wakeup must be idempotent: %v %+v", err, again)
	}

	// 批次失败 → 节点失败 → 计划失败（required）。
	run2, _ := scheduler.Submit(ctx, "s1", "u1", ExecutionPlan{PlanID: "jobs-fail", Source: "dynamic", Summary: "派发失败", Direction: "image",
		Steps: []PlanStep{{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "x"}}}, Required: true}}})
	failed, err := fresh.CompleteJobsWait(ctx, run2.ID, "generate", JobsOutcome{BatchID: "batch-1", Status: "failed"})
	if err != nil || failed.Status != RunStatusFailed {
		t.Fatalf("failed batch must fail plan: %v %+v", err, failed)
	}
}
```

- [ ] **Step 2: 失败确认**

Run: `go test ./internal/aigc/orchestration -run TestCompleteJobsWait -v`

- [ ] **Step 3: 实现**

```go
// CompleteJobsWait 是 waiting_jobs 的唤醒入口（批次终结回调对号入座，
// §2.3 R2）。幂等：节点已离开挂起态时重复唤醒直接返回当前 run。
func (s *Scheduler) CompleteJobsWait(ctx context.Context, runID, nodeID string, outcome JobsOutcome) (PlanRun, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	node, ok := run.Nodes[nodeID]
	if !ok {
		return PlanRun{}, fmt.Errorf("%w: node %s", ErrRunNotFound, nodeID)
	}
	if node.Suspension == nil || run.Status != RunStatusSuspended || run.SuspendedNodeID != nodeID {
		return run, nil // 已被唤醒过：幂等返回
	}
	batchFailed := outcome.Status == "failed" || outcome.Status == "cancelled"
	updated, err := s.store.MutateRun(ctx, run.ID, run.Version, func(current *PlanRun) error {
		target := current.Nodes[nodeID]
		if target.Outputs == nil {
			target.Outputs = map[string]any{}
		}
		target.Outputs["batch_status"] = outcome.Status
		for key, value := range outcome.Summary {
			target.Outputs[key] = value
		}
		target.Suspension = nil
		if batchFailed {
			target.Status = NodeStatusFailed
			target.Fail = &vocabulary.Failure{Code: "batch_" + outcome.Status, Message: "生成批次未成功"}
		} else {
			target.Status = NodeStatusSucceeded
		}
		current.Status = RunStatusRunning
		current.SuspendReason = ""
		current.SuspendedNodeID = ""
		return nil
	})
	if err != nil {
		return PlanRun{}, err
	}
	return s.Advance(ctx, updated.ID)
}
```

- [ ] **Step 4: 绿（-race）+ Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "feat(orchestration): waiting_jobs 唤醒对号入座——批次终结驱动计划续跑（幂等）"
```

---

### Task 11: R1 前置切面 + `compliance_check` 最小规则表

**Files:**
- Create: `internal/aigc/orchestration/guard_chain.go`
- Create: `internal/aigc/vocabulary/tools/compliance_check.go`
- Test: `internal/aigc/orchestration/guard_chain_test.go`、`internal/aigc/vocabulary/tools/compliance_check_test.go`

依据：§2.3 R1——媒体执行/派发节点自动织入前置 `check_permission → compliance_check → reserve_credits`，切面只由 runtime 织入、不进词汇表不作计划节点；§1.5——compliance 两级（硬拦拒绝不可翻案/软拦挂起授权）、permission/credits v1 空实现占位。

- [ ] **Step 1: 写失败测试（compliance 规则表）**

```go
package tools

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

func TestComplianceCheckTwoLevels(t *testing.T) {
	tool := NewComplianceCheck(ComplianceRules{
		HardBlock: []string{"血腥斩首"},
		SoftFlag:  []string{"真人肖像"},
	})
	clean, err := tool.Run(context.Background(), vocabulary.Call{Inputs: map[string]any{"content": "雨中柴犬"}})
	if err != nil || clean.Fail != nil || clean.Suspension != nil {
		t.Fatalf("clean content must pass: %v %+v", err, clean)
	}
	hard, err := tool.Run(context.Background(), vocabulary.Call{Inputs: map[string]any{"content": "血腥斩首场面"}})
	if err != nil {
		t.Fatal(err)
	}
	if hard.Fail == nil || hard.Fail.Code != "compliance_hard_block" || hard.Fail.Retryable {
		t.Fatalf("hard block must be a non-retryable fail: %+v", hard)
	}
	soft, err := tool.Run(context.Background(), vocabulary.Call{Inputs: map[string]any{"content": "真人肖像 写实"}})
	if err != nil {
		t.Fatal(err)
	}
	if soft.Suspension == nil || soft.Suspension.Reason != "waiting_user" {
		t.Fatalf("soft flag must suspend for authorization: %+v", soft)
	}
}
```

（guard_chain_test.go）

```go
package orchestration

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
	vocabtools "github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary/tools"
)

func TestGuardChainWeavesBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	registry := vocabulary.NewRegistry()
	_ = registry.Register(dispatchLikeTool("batch-1"))
	scheduler, _ := newSchedulerForTest(t, registry)
	scheduler.SetGuardChain(NewGuardChain(vocabtools.NewComplianceCheck(vocabtools.ComplianceRules{HardBlock: []string{"血腥斩首"}})))

	// 干净内容：切面放行，节点照常挂起 waiting_jobs。
	clean, err := scheduler.Submit(ctx, "s1", "u1", ExecutionPlan{PlanID: "g1", Source: "dynamic", Summary: "干净", Direction: "image",
		Steps: []PlanStep{{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "雨中柴犬"}}}, Required: true}}})
	if err != nil || clean.SuspendReason != SuspendWaitingJobs {
		t.Fatalf("clean: %v %+v", err, clean)
	}

	// 硬拦内容：节点失败（不可翻案），不触达工具本体。
	blocked, err := scheduler.Submit(ctx, "s1", "u1", ExecutionPlan{PlanID: "g2", Source: "dynamic", Summary: "硬拦", Direction: "image",
		Steps: []PlanStep{{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "血腥斩首场面"}}}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != RunStatusFailed || blocked.Nodes["generate"].Fail == nil || blocked.Nodes["generate"].Fail.Code != "compliance_hard_block" {
		t.Fatalf("hard block must fail node before tool runs: %+v", blocked)
	}
}
```

- [ ] **Step 2: 失败确认**

Run: `go test ./internal/aigc/vocabulary/tools -run TestCompliance -v && go test ./internal/aigc/orchestration -run TestGuardChain -v`

- [ ] **Step 3: 实现**

compliance_check.go：

```go
package tools

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type ComplianceRules struct {
	HardBlock []string
	SoftFlag  []string
}

type complianceCheck struct{ rules ComplianceRules }

func NewComplianceCheck(rules ComplianceRules) vocabulary.Tool { return complianceCheck{rules: rules} }

func (complianceCheck) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{
		Key: "compliance_check", Name: "合规预检", Category: "guard",
		Description: "两级规则表：硬拦直接拒绝不可授权翻案；软拦挂起等发起用户授权调整方向。切面专用，不进编排词汇。",
		Inputs: map[string]vocabulary.ParamSpec{
			"content": {Type: "string", Desc: "待检文本（提示词/文案）", Required: true},
		},
	}
}

func (t complianceCheck) Run(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	content, _ := call.Inputs["content"].(string)
	for _, word := range t.rules.HardBlock {
		if word != "" && strings.Contains(content, word) {
			return vocabulary.Result{Fail: &vocabulary.Failure{Code: "compliance_hard_block", Message: "内容命中禁区规则：" + word}}, nil
		}
	}
	for _, word := range t.rules.SoftFlag {
		if word != "" && strings.Contains(content, word) {
			return vocabulary.Result{Suspension: &vocabulary.Suspension{Reason: "waiting_user",
				Payload: map[string]any{"kind": "compliance_soft_flag", "hint": word, "content": content}}}, nil
		}
	}
	return vocabulary.Result{Outputs: map[string]any{"passed": true}}, nil
}
```

guard_chain.go（调度器织入面）：

```go
package orchestration

import (
	"context"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// GuardChain 是 R1 前置切面：只对派发/媒体执行类节点织入，由 runtime
// 持有，不进词汇注册表、不可作计划节点。v1 链 = compliance（permission/
// credits 空实现占位，接入时追加到 guards 即可）。
type GuardChain struct{ guards []vocabulary.Tool }

func NewGuardChain(guards ...vocabulary.Tool) *GuardChain { return &GuardChain{guards: guards} }

// guardedTools 列出需要织入前置护栏的词汇（媒体执行/派发类）。
var guardedTools = map[string]struct{}{"dispatch_generation": {}}

func (c *GuardChain) Applies(toolKey string) bool {
	_, ok := guardedTools[toolKey]
	return ok
}

// Check 提取节点输入里的文本面（prompt 等）依次过护栏；任一 Fail/
// Suspension 即短路返回。
func (c *GuardChain) Check(ctx context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	if c == nil || len(c.guards) == 0 {
		return vocabulary.Result{}, nil
	}
	content := collectTextInputs(call.Inputs)
	for _, guard := range c.guards {
		result, err := guard.Run(ctx, vocabulary.Call{
			SessionID: call.SessionID, UserID: call.UserID, PlanRunID: call.PlanRunID,
			NodeID: call.NodeID, IdempotencyKey: call.IdempotencyKey + ":guard:" + guard.Descriptor().Key,
			Inputs: map[string]any{"content": content},
		})
		if err != nil {
			return vocabulary.Result{}, fmt.Errorf("guard %s: %w", guard.Descriptor().Key, err)
		}
		if result.Fail != nil || result.Suspension != nil {
			return result, nil
		}
	}
	return vocabulary.Result{}, nil
}

func collectTextInputs(inputs map[string]any) string {
	var builder strings.Builder
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			builder.WriteString(typed)
			builder.WriteString("\n")
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(inputs)
	return builder.String()
}
```

调度器接线（`executeStep` 顶部，工具调用前）：

```go
	// Scheduler 增字段 guardChain *GuardChain 与 SetGuardChain 方法。
	if s.guardChain != nil && s.guardChain.Applies(step.Tool) {
		guardResult, guardErr := s.guardChain.Check(ctx, call)
		if guardErr != nil {
			return vocabulary.Result{}, guardErr
		}
		if guardResult.Fail != nil || guardResult.Suspension != nil {
			return guardResult, nil
		}
	}
```

（注意：`executeStep` 中 `call` 的构造须移到护栏检查之前。）

- [ ] **Step 4: 绿 + Commit**

```bash
git add internal/aigc/orchestration/ internal/aigc/vocabulary/tools/
git commit -m "feat(orchestration): R1 前置切面织入 + compliance 两级规则表（硬拦不可翻案/软拦挂起授权）"
```

---

### Task 12: 端到端验收——「备提示词→确认→派发→唤醒→终态」全链

**Files:**
- Test: `internal/aigc/orchestration/e2e_plan_test.go`

本任务是本计划的验收测试：真词汇（Task 7/8/9 实现）+ 真 generation memory store + 真 BatchBarrier，模拟 worker 完成 job 后经 barrier 终结批次，唤醒计划跑到终态。

- [ ] **Step 1: 写失败测试**

```go
package orchestration

import (
	"context"
	"fmt"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
	vocabtools "github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary/tools"
)

type e2eChat struct{}

func (e2eChat) Complete(_ context.Context, _ string) (string, error) {
	return "一只柴犬在雨中撑伞，电影感光效", nil
}

type e2eAssets struct{ saved []asset.Asset }

func (s *e2eAssets) Save(_ context.Context, value asset.Asset) (asset.Asset, error) {
	s.saved = append(s.saved, value)
	return value, nil
}

func TestEndToEndImagePlan(t *testing.T) {
	ctx := context.Background()
	generationStore := generation.NewMemoryStore()
	assets := &e2eAssets{}
	idCounter := 0
	newID := func() string { idCounter++; return fmt.Sprintf("id-%d", idCounter) }

	registry := vocabulary.NewRegistry()
	if err := registry.Register(vocabtools.NewWriteMediaPrompt(e2eChat{})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(vocabtools.NewRequestConfirmation()); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(vocabtools.NewDispatchGeneration(vocabtools.DispatchGenerationConfig{
		Commands: generation.NewCommandService(generation.CommandServiceConfig{Store: generationStore}),
		Assets:   assets, NewID: newID,
	})); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryRunStore()
	scheduler, err := NewScheduler(SchedulerConfig{Store: store, Vocabulary: registry, NewID: newID})
	if err != nil {
		t.Fatal(err)
	}

	plan := ExecutionPlan{
		PlanID: "quick-image", Source: "template:image_creation", Summary: "一张雨中柴犬", Direction: "image",
		Steps: []PlanStep{
			{ID: "prompt", Tool: "write_media_prompt", Params: map[string]any{"target_desc": "雨中柴犬", "media_kind": "image"}, Required: true},
			{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "提示词可以吗", "preview": map[string]any{"prompt": "$prompt.prompt"}}, DependsOn: []string{"prompt"}, Required: true},
			{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"media_kind": "image", "targets": []any{map[string]any{"prompt": "$prompt.prompt"}}}, DependsOn: []string{"confirm"}, Required: true},
		},
		EstimatedJobs: 1,
	}

	// 1) 提交 → 卡点挂起。
	run, err := scheduler.Submit(ctx, "s1", "u1", plan)
	if err != nil || run.SuspendReason != SuspendWaitingUser {
		t.Fatalf("submit: %v %+v", err, run)
	}
	// 卡点预览里的上游引用已解析为真值。
	if run.Nodes["confirm"].Suspension.Payload["preview"].(map[string]any)["prompt"] != "一只柴犬在雨中撑伞，电影感光效" {
		t.Fatalf("preview must carry resolved prompt: %+v", run.Nodes["confirm"].Suspension.Payload)
	}

	// 2) 用户确认 → 派发 → waiting_jobs。
	run, err = scheduler.Resume(ctx, run.ID, run.Nodes["confirm"].ResumeKey, map[string]any{"decision": "approved"})
	if err != nil || run.SuspendReason != SuspendWaitingJobs {
		t.Fatalf("after confirm: %v %+v", err, run)
	}
	if len(assets.saved) != 1 || assets.saved[0].Availability != asset.AvailabilityGenerating {
		t.Fatalf("preregistered assets: %+v", assets.saved)
	}
	batchID, _ := run.Nodes["generate"].Suspension.Payload["batch_id"].(string)

	// 3) 模拟 worker：job 直接置 succeeded，经真实 barrier 终结批次。
	jobs, err := generationStore.ListBySession(ctx, "s1")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %v %d", err, len(jobs))
	}
	job := jobs[0]
	if _, err := generationStore.MutateJob(ctx, job.ID, job.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusRunning
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	running, _ := generationStore.GetJob(ctx, job.ID)
	if _, err := generationStore.MutateJob(ctx, job.ID, running.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusSucceeded
		current.ResultAssetIDs = []string{"asset-out-1"}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	barrierResult, err := generation.NewBatchBarrier(generationStore).TryFinalize(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}

	// 4) 批次终结 → 唤醒计划 → 终态。
	final, err := scheduler.CompleteJobsWait(ctx, run.ID, "generate", JobsOutcome{
		BatchID: batchID, Status: barrierResult.Payload.Status,
	})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != RunStatusSucceeded {
		t.Fatalf("final = %+v", final)
	}
}
```

（`barrierResult.Payload.Status` 的字段路径按 `generation/barrier.go` 实际返回结构为准——取证已确认 `TryFinalize` 返回含 `Payload.Status`（workflow_test.go:298-304 用法）；批次完成态常量若与 `"completed"` 字符串不同，以 `generation.BatchStatusCompleted` 判断映射 JobsOutcome.Status。）

- [ ] **Step 2: 跑测试（此时多数环节已实现，失败点应集中在字段对齐）**

Run: `go test ./internal/aigc/orchestration -run TestEndToEnd -v`

- [ ] **Step 3: 修字段对齐直至绿；再跑 -race**

Run: `go test ./internal/aigc/orchestration -race`

- [ ] **Step 4: Commit**

```bash
git add internal/aigc/orchestration/
git commit -m "test(orchestration): 端到端验收——备提示词→确认→派发→真实barrier终结→唤醒→终态全链绿"
```

---

### Task 13: Postgres RunStore + 全量回归收尾

**Files:**
- Create: `internal/aigc/orchestration/postgres_run_store.go`
- Test: `internal/aigc/orchestration/postgres_run_store_test.go`

- [ ] **Step 1: 写失败测试（本地 Postgres 不可用时 skip，对齐仓库测试约定）**

```go
package orchestration

import (
	"context"
	"errors"
	"testing"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func openPostgresRunStore(t *testing.T) *PostgresRunStore {
	t.Helper()
	cfg := aigcconfig.LoadFromEnv().Normalize()
	db, err := aigcstorage.OpenAgentPostgres(cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	store := NewPostgresRunStore(db)
	if err := store.AutoMigrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPostgresRunStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openPostgresRunStore(t)
	run := PlanRun{ID: "pg-run-" + defaultRunID(), SessionID: "s1", UserID: "u1",
		Plan: validPlan(), Status: RunStatusDraft, Nodes: map[string]*NodeRun{"prompt": {StepID: "prompt", Status: NodeStatusPending}}}
	created, err := store.CreateRun(ctx, run)
	if err != nil || created.Version != 1 {
		t.Fatalf("create: %v %+v", err, created)
	}
	mutated, err := store.MutateRun(ctx, run.ID, 1, func(current *PlanRun) error {
		current.Status = RunStatusRunning
		current.Nodes["prompt"].Status = NodeStatusRunning
		return nil
	})
	if err != nil || mutated.Version != 2 {
		t.Fatalf("mutate: %v %+v", err, mutated)
	}
	if _, err := store.MutateRun(ctx, run.ID, 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale mutate: %v", err)
	}
	loaded, err := store.GetRun(ctx, run.ID)
	if err != nil || loaded.Status != RunStatusRunning || loaded.Nodes["prompt"].Status != NodeStatusRunning {
		t.Fatalf("load: %v %+v", err, loaded)
	}
}
```

- [ ] **Step 2: 失败确认（编译失败）**

- [ ] **Step 3: 实现（单表 JSONB：plan 与 nodes 全量 JSON 列 + version 整数列做 CAS；GORM 风格对齐 `storyboard/aggregate_postgres.go` 的单聚合表模式）**

```go
package orchestration

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type planRunRecord struct {
	ID        string `gorm:"primaryKey;column:id"`
	SessionID string `gorm:"column:session_id;index"`
	Payload   []byte `gorm:"column:payload;type:jsonb"`
	Version   int    `gorm:"column:version"`
}

func (planRunRecord) TableName() string { return "aigc_plan_runs" }

type PostgresRunStore struct{ db *gorm.DB }

func NewPostgresRunStore(db *gorm.DB) *PostgresRunStore { return &PostgresRunStore{db: db} }

func (s *PostgresRunStore) AutoMigrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&planRunRecord{})
}

func (s *PostgresRunStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	run.Version = 1
	payload, err := json.Marshal(run)
	if err != nil {
		return PlanRun{}, err
	}
	record := planRunRecord{ID: run.ID, SessionID: run.SessionID, Payload: payload, Version: 1}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
		return PlanRun{}, err
	}
	return s.GetRun(ctx, run.ID)
}

func (s *PostgresRunStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	var record planRunRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return PlanRun{}, ErrRunNotFound
		}
		return PlanRun{}, err
	}
	var run PlanRun
	if err := json.Unmarshal(record.Payload, &run); err != nil {
		return PlanRun{}, err
	}
	run.Version = record.Version
	return run, nil
}

func (s *PostgresRunStore) MutateRun(ctx context.Context, id string, version int, fn func(*PlanRun) error) (PlanRun, error) {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return PlanRun{}, err
	}
	if run.Version != version {
		return PlanRun{}, fmt.Errorf("%w: have %d want %d", ErrRunVersionConflict, run.Version, version)
	}
	before := run.Status
	if err := fn(&run); err != nil {
		return PlanRun{}, err
	}
	if err := ValidateRunTransition(before, run.Status); err != nil {
		return PlanRun{}, err
	}
	run.Version = version + 1
	payload, err := json.Marshal(run)
	if err != nil {
		return PlanRun{}, err
	}
	result := s.db.WithContext(ctx).Model(&planRunRecord{}).
		Where("id = ? AND version = ?", id, version).
		Updates(map[string]any{"payload": payload, "version": version + 1})
	if result.Error != nil {
		return PlanRun{}, result.Error
	}
	if result.RowsAffected == 0 {
		return PlanRun{}, fmt.Errorf("%w: concurrent update", ErrRunVersionConflict)
	}
	return run, nil
}
```

- [ ] **Step 4: 全量回归**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -8`
Run: `go test ./internal/aigc/orchestration ./internal/aigc/vocabulary/... -count=1 -race`
Expected: 全部 PASS（Postgres 测试在基础设施缺失时 skip）

- [ ] **Step 5: 勾选计划 + 执行记录 + 收尾提交**

```bash
git add -A docs/superpowers/plans/2026-07-12-plan1-vocabulary-and-plan-runtime.md internal/
git commit -m "chore: Plan 1 完成——原子词汇契约+执行计划runtime全链绿（§6.3 可行性证实）"
```

---

## 计划自审记录

- **Spec 覆盖**（对 b677904 §6.1 delta 表）：原始 Tool 契约 ✓（Task 1）+ 认知首例 ✓（Task 7）+ 交互 ✓（Task 8）+ 数据派发 ✓（Task 9）+ 护栏切面与 compliance ✓（Task 11，permission/credits 空占位按拍板）；动态编排之校验器 ✓（Task 2 五查的结构/参数/预留三查 + 预算查在 Submit）+ runtime ✓（Task 3-6/10）；异步任务对接 ✓（Task 9/10/12）。**不在本计划**（Plan 2/3 承接，见头部范围裁定）：Agent 注册面/M1-M3/评估点续编闭环/认知其余 5 个/编排库存储/模板数据化/绑定表建模/前端。
- **占位符扫描**：无 TBD/TODO；asset generating 常量经取证确认**不存在**，已改为 Task 9 的确定性新增步骤；barrier 返回路径 `.Payload.Status` 经取证确认存在（barrier.go:44）。
- **类型一致性**：全部任务共用「类型定稿」节；`staticTool`/`confirmTool`/`dispatchLikeTool`/`newSchedulerForTest` 等测试辅助在首次出现的任务中定义，后续任务同包复用；`Scheduler.SetGuardChain` 在 Task 11 引入并在该任务内完成接线说明。
- **风险与 stop 条件**：Task 5/6 是 §6.3 可行性判据，失败即停回设计；Task 12 端到端若暴露 barrier/store 接缝问题，按事实修正字段映射而非绕过。
