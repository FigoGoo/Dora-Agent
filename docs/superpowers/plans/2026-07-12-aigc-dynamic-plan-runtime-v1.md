# AIGC Dynamic Plan Runtime V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以 07-11 终版规格为唯一产品设计基准，完成可持久、可挂起、可恢复、可修订的动态执行计划 Runtime，并跑通「提示词 -> 用户确认 -> 媒体派发 -> Batch 唤醒 -> 计划终态」全链。

**Architecture:** `internal/aigc/vocabulary` 定义无状态原子工具契约，`internal/aigc/orchestration` 持有 8 要素 `ExecutionPlan`、`PlanRun` 状态机和自写 DAG Scheduler。Scheduler 不用 Eino Compose 承载动态拓扑；Eino 继续用于单个认知工具内部模型调用。媒体节点通过适配器复用现有 `generation.CommandService`、Operation/Batch/Job/Outbox/Barrier，不复制异步基础设施。

**Tech Stack:** Go 1.26、Eino 0.9（仅单工具内部推理）、GORM/PostgreSQL、现有 generation workflow、标准库并发原语。

---

## 0. 设计基准与范围

### 唯一规格源

- `docs/superpowers/specs/2026-07-11-aigc-system-design-final.md`
- 以该文档 §6.7 为最终裁决：Agent 最终注册面是 14 个可编排原子词汇；五个 Capability 不删除，但降级为深模板实现素材。
- 现有 `sessionruntime`、Approval、Operation/Batch/Job、Outbox、Barrier、Finalization、RecoveryScheduler 均为底座，禁止在本计划重复实现。

### 当前已完成基线

- `5051269`、`5029c75`：`internal/aigc/vocabulary` 的 `Descriptor/Call/Result/Tool/Registry` 已实现并验证。
- `27c1d21`、`80a625b`：8 要素 `ExecutionPlan`、DAG/引用/参数槽/预算事实校验已实现并验证。
- target-neutral deliverable、轻模板、前端 deliverables surface 已存在，作为媒体派发落点复用。

### 三阶段路线

1. **本计划：Plan Runtime 地基。** 状态机、Scheduler、挂起恢复、活计划修订、原子验收工具、generation bridge、Postgres 恢复。
2. **后续 Plan 2：Agent 动态编排。** M1 需求矩阵、M2 模板命中、M3 计划生成/两次修复、14 词汇注册面、五 Capability 降级。
3. **后续 Plan 3：模板与产品投影。** 六个初始模板数据化、计划/节点 A2UI 投影、孤立产物区、actor 审计、旧 mediagraph 清理。

Plan 2 的入口条件：本计划 Task 8 的新实例恢复测试和 Task 9 的端到端测试全部通过。若 Task 4 或 Task 8 证明“挂起即释放”“活计划修订”或“仅依赖持久化 Store 恢复”无法在当前模型下可靠实现，停止并回到 07-11 规格 §6.3 重议技术路径。

## 1. 文件结构

| 文件 | 责任 |
| --- | --- |
| `internal/aigc/orchestration/plan_run.go` | `PlanRun/NodeRun`、状态转移、RunStore 接口、内存 Store |
| `internal/aigc/orchestration/scheduler.go` | DAG 就绪集、并发执行、参数引用解析、终态归纳 |
| `internal/aigc/orchestration/resume.go` | waiting_user/waiting_agent/waiting_jobs 的一次性恢复 |
| `internal/aigc/orchestration/revision.go` | 活计划裁剪和追加，只允许修改未执行节点 |
| `internal/aigc/orchestration/postgres_run_store.go` | 单行 JSONB 聚合、Version CAS、跨实例恢复 |
| `internal/aigc/orchestration/generation_bridge.go` | Plan node 与现有 generation workflow 的窄接口适配 |
| `internal/aigc/vocabulary/write_media_prompt.go` | 第一个认知原子工具，模型调用委托给窄接口 |
| `internal/aigc/vocabulary/request_confirmation.go` | 交互原子工具，返回 waiting_user Suspension |
| `internal/aigc/vocabulary/dispatch_generation.go` | 数据原子工具，创建 workflow 并返回 waiting_jobs Suspension |
| `internal/aigc/vocabulary/guard_chain.go` | 媒体节点执行前的 permission/compliance/credits 最小切面 |

---

### Task 1: PlanRun 状态机与内存 Store

**Files:**
- Create: `internal/aigc/orchestration/plan_run.go`
- Create: `internal/aigc/orchestration/plan_run_test.go`

- [ ] **Step 1: 写状态转移失败测试**

```go
func TestRunStatusTransitions(t *testing.T) {
	legal := [][2]string{
		{RunStatusDraft, RunStatusRunning},
		{RunStatusRunning, RunStatusSuspended},
		{RunStatusSuspended, RunStatusRunning},
		{RunStatusRunning, RunStatusSucceeded},
		{RunStatusRunning, RunStatusPartialSucceeded},
		{RunStatusRunning, RunStatusFailed},
		{RunStatusDraft, RunStatusCancelled},
		{RunStatusRunning, RunStatusCancelled},
		{RunStatusSuspended, RunStatusCancelled},
	}
	for _, pair := range legal {
		if err := ValidateRunTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s -> %s: %v", pair[0], pair[1], err)
		}
	}
	for _, pair := range [][2]string{
		{RunStatusSucceeded, RunStatusRunning},
		{RunStatusFailed, RunStatusRunning},
		{RunStatusCancelled, RunStatusRunning},
		{RunStatusDraft, RunStatusSucceeded},
	} {
		if err := ValidateRunTransition(pair[0], pair[1]); err == nil {
			t.Fatalf("%s -> %s must fail", pair[0], pair[1])
		}
	}
}
```

- [ ] **Step 2: 写 Store CAS、回调原子性和深拷贝失败测试**

```go
func TestMemoryRunStoreCASAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRunStore()
	created, err := store.CreateRun(ctx, PlanRun{
		ID: "run-1", SessionID: "session-1", UserID: "user-1",
		Plan: validPlan(), Status: RunStatusDraft, Nodes: map[string]*NodeRun{},
	})
	if err != nil || created.Version != 1 {
		t.Fatalf("create: version=%d err=%v", created.Version, err)
	}
	updated, err := store.MutateRun(ctx, created.ID, 1, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil || updated.Version != 2 {
		t.Fatalf("mutate: version=%d err=%v", updated.Version, err)
	}
	if _, err := store.MutateRun(ctx, created.ID, 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err := store.MutateRun(ctx, created.ID, 2, func(run *PlanRun) error {
		run.Nodes["leak"] = &NodeRun{StepID: "leak"}
		return errors.New("abort")
	}); err == nil {
		t.Fatal("callback failure must abort")
	}
	got, _ := store.GetRun(ctx, created.ID)
	if _, exists := got.Nodes["leak"]; exists {
		t.Fatal("aborted mutation leaked")
	}
	got.Nodes["external"] = &NodeRun{StepID: "external"}
	again, _ := store.GetRun(ctx, created.ID)
	if _, exists := again.Nodes["external"]; exists {
		t.Fatal("read result aliases store")
	}
}
```

- [ ] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestRunStatus|TestMemoryRunStore' -v`

Expected: 编译失败，缺少 `PlanRun`、`NewMemoryRunStore` 或状态常量。

- [ ] **Step 4: 实现最小状态模型**

```go
var (
	ErrRunNotFound        = errors.New("plan run not found")
	ErrRunVersionConflict = errors.New("plan run version conflict")
)

const (
	RunStatusDraft = "draft"
	RunStatusRunning = "running"
	RunStatusSuspended = "suspended"
	RunStatusSucceeded = "succeeded"
	RunStatusPartialSucceeded = "partial_succeeded"
	RunStatusFailed = "failed"
	RunStatusCancelled = "cancelled"
	SuspendWaitingUser = "waiting_user"
	SuspendWaitingAgent = "waiting_agent"
	SuspendWaitingJobs = "waiting_jobs"
	NodeStatusPending = "pending"
	NodeStatusRunning = "running"
	NodeStatusSucceeded = "succeeded"
	NodeStatusFailed = "failed"
	NodeStatusSkipped = "skipped"
)

type NodeRun struct {
	StepID string `json:"step_id"`
	Status string `json:"status"`
	Attempt int `json:"attempt"`
	Outputs map[string]any `json:"outputs,omitempty"`
	Fail *vocabulary.Failure `json:"fail,omitempty"`
	Suspension *vocabulary.Suspension `json:"suspension,omitempty"`
	ResumeKey string `json:"resume_key,omitempty"`
	Resumed bool `json:"resumed,omitempty"`
}

type PlanRun struct {
	ID string `json:"id"`
	SessionID string `json:"session_id"`
	UserID string `json:"user_id"`
	Plan ExecutionPlan `json:"plan"`
	Status string `json:"status"`
	SuspendReason string `json:"suspend_reason,omitempty"`
	SuspendedNodeID string `json:"suspended_node_id,omitempty"`
	PreviewRequired bool `json:"preview_required,omitempty"`
	Nodes map[string]*NodeRun `json:"nodes"`
	Version int `json:"version"`
}
```

`RunStore` 必须只暴露 `CreateRun`、`GetRun`、`MutateRun(id, expectedVersion, fn)`；内存实现持锁执行回调，先深拷贝、校验状态转移，成功后 Version `+1` 再替换。

- [ ] **Step 5: 运行 GREEN 和包回归**

Run: `go test ./internal/aigc/orchestration -run 'TestRunStatus|TestMemoryRunStore' -v && go test ./internal/aigc/orchestration`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/aigc/orchestration/plan_run.go internal/aigc/orchestration/plan_run_test.go
git commit -m "feat(orchestration): add durable plan run state model"
```

---

### Task 2: Scheduler 就绪集、并发上限和终态归纳

**Files:**
- Create: `internal/aigc/orchestration/scheduler.go`
- Create: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写菱形 DAG 并发测试**

```go
func TestSchedulerRunsDiamondDAGOnce(t *testing.T) {
	registry, calls, peak := schedulerVocabulary(t)
	scheduler := newSchedulerForTest(t, NewMemoryRunStore(), registry, 2)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", diamondPlan())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSucceeded {
		t.Fatalf("status=%s", run.Status)
	}
	if calls.Load() != 4 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if peak.Load() != 2 {
		t.Fatalf("peak=%d", peak.Load())
	}
}
```

测试工具在 `Run` 内用 channel barrier 保证兄弟节点确实重叠，避免仅凭调度时序猜测并发。

- [ ] **Step 2: 写上游引用解析与幂等 Advance 测试**

```go
func TestSchedulerResolvesUpstreamOutputAndDoesNotRerun(t *testing.T) {
	var received any
	registry := vocabulary.NewRegistry()
	mustRegister(t, registry, staticTool{key: "source", outputs: map[string]any{"prompt": "rain"}})
	mustRegister(t, registry, staticTool{key: "sink", run: func(call vocabulary.Call) (vocabulary.Result, error) {
		received = call.Inputs["value"]
		return vocabulary.Result{Outputs: map[string]any{"done": true}}, nil
	}})
	plan := ExecutionPlan{PlanID: "refs", Source: "dynamic", Summary: "refs", Direction: "image", Steps: []PlanStep{
		{ID: "a", Tool: "source", Required: true},
		{ID: "b", Tool: "sink", Params: map[string]any{"value": "$a.prompt"}, DependsOn: []string{"a"}, Required: true},
	}}
	scheduler := newSchedulerForTest(t, NewMemoryRunStore(), registry, 2)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", plan)
	if err != nil || received != "rain" {
		t.Fatalf("received=%v err=%v", received, err)
	}
	again, err := scheduler.Advance(context.Background(), run.ID)
	if err != nil || again.Version != run.Version {
		t.Fatalf("terminal replay mutated run: before=%d after=%d err=%v", run.Version, again.Version, err)
	}
}
```

- [ ] **Step 3: 写 required/optional 终态测试**

覆盖三条规则：required 节点失败且无法继续 -> `failed`；optional 节点失败但 required 全成功 -> `partial_succeeded`；所有节点成功 -> `succeeded`。

- [ ] **Step 4: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestScheduler' -v`

Expected: 编译失败，缺少 `Scheduler`。

- [ ] **Step 5: 实现 Scheduler 最小 API**

```go
type SchedulerConfig struct {
	Store RunStore
	Vocabulary *vocabulary.Registry
	MaxParallel int
	JobBudget int
	NewID func() string
}

type Scheduler struct {
	store RunStore
	vocabulary *vocabulary.Registry
	maxParallel int
	jobBudget int
	newID func() string
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error)
func (s *Scheduler) Submit(ctx context.Context, sessionID, userID string, plan ExecutionPlan) (PlanRun, error)
func (s *Scheduler) Advance(ctx context.Context, runID string) (PlanRun, error)
```

`Submit` 顺序固定为：`plan.Validate` -> 初始化全部 NodeRun 为 pending -> 创建 Run -> 预算判定 -> `draft->running` -> `Advance`。`Advance` 每轮读取快照，只选择“自身 pending 且全部依赖 succeeded/skipped”的节点；用容量为 `MaxParallel` 的 semaphore 并发执行；每个节点的幂等键固定为 `planRunID:stepID:attempt`。

- [ ] **Step 6: 运行 GREEN、race 和包回归**

Run: `go test -race ./internal/aigc/orchestration -run 'TestScheduler' -v && go test ./internal/aigc/orchestration`

Expected: PASS，无 data race。

- [ ] **Step 7: 提交**

```bash
git add internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): execute dynamic plan DAGs"
```

---

### Task 3: waiting_user / waiting_agent 统一挂起与一次性 Resume

**Files:**
- Create: `internal/aigc/orchestration/resume.go`
- Modify: `internal/aigc/orchestration/scheduler.go`
- Modify: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写交互挂起释放测试**

```go
func TestSchedulerSuspendsWithoutResidentExecution(t *testing.T) {
	registry := vocabulary.NewRegistry()
	mustRegister(t, registry, suspensionTool{key: "confirm", reason: SuspendWaitingUser})
	scheduler := newSchedulerForTest(t, NewMemoryRunStore(), registry, 1)
	run, err := scheduler.Submit(context.Background(), "s1", "u1", oneStepPlan("confirm"))
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSuspended || run.SuspendReason != SuspendWaitingUser || run.SuspendedNodeID == "" {
		t.Fatalf("run=%+v", run)
	}
	if run.Nodes[run.SuspendedNodeID].ResumeKey == "" {
		t.Fatal("resume key missing")
	}
}
```

- [ ] **Step 2: 写 Resume 幂等和 key 冲突测试**

```go
func TestResumeIsOneShotAndIdempotent(t *testing.T) {
	ctx := context.Background()
	scheduler := schedulerWithConfirmationThenSink(t)
	suspended, _ := scheduler.Submit(ctx, "s1", "u1", confirmationPlan())
	key := suspended.Nodes[suspended.SuspendedNodeID].ResumeKey
	resumed, err := scheduler.Resume(ctx, suspended.ID, key, map[string]any{"decision": "approved"})
	if err != nil || resumed.Status != RunStatusSucceeded {
		t.Fatalf("resume: status=%s err=%v", resumed.Status, err)
	}
	replayed, err := scheduler.Resume(ctx, suspended.ID, key, map[string]any{"decision": "approved"})
	if err != nil || replayed.Version != resumed.Version {
		t.Fatalf("replay changed run: %+v %v", replayed, err)
	}
	if _, err := scheduler.Resume(ctx, suspended.ID, "wrong", nil); !errors.Is(err, ErrResumeKeyMismatch) {
		t.Fatalf("wrong key: %v", err)
	}
}
```

- [ ] **Step 3: 写 Evaluate -> waiting_agent 测试**

节点成功且 `PlanStep.Evaluate=true` 时保存 outputs，然后 Run 进入 `suspended(waiting_agent)`；该节点不得重复执行。

- [ ] **Step 4: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestSchedulerSuspends|TestResume|TestEvaluate' -v`

Expected: 编译失败，缺少 `Resume` 或断言失败。

- [ ] **Step 5: 实现恢复协议**

```go
var ErrResumeKeyMismatch = errors.New("plan run resume key mismatch")

func (s *Scheduler) Resume(
	ctx context.Context,
	runID string,
	resumeKey string,
	decision map[string]any,
) (PlanRun, error)
```

Resume 先在全部 NodeRun 中查找 ResumeKey。若命中节点已 `Resumed=true`，必须直接返回当前快照，即使 Run 已经终态；这是 HTTP 重放的 receipt 路径。首次恢复才要求 Run 为 suspended 且命中 `SuspendedNodeID`，并在同一个 CAS 回调内把 decision 合并到节点 outputs、清空 Suspension、置 `Resumed=true`、执行 Run `suspended->running`。CAS 成功后才调用 `Advance`。

- [ ] **Step 6: 运行 GREEN 与 race**

Run: `go test -race ./internal/aigc/orchestration -run 'TestSchedulerSuspends|TestResume|TestEvaluate' -v`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/aigc/orchestration/resume.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): add unified plan suspension and resume"
```

---

### Task 4: 活计划修订

**Files:**
- Create: `internal/aigc/orchestration/revision.go`
- Create: `internal/aigc/orchestration/revision_test.go`

- [ ] **Step 1: 写未执行节点裁剪和追加测试**

```go
func TestReviseSkipsPendingAndAppendsSteps(t *testing.T) {
	ctx := context.Background()
	scheduler, suspended := suspendedThreeStepRun(t)
	revised, err := scheduler.Revise(ctx, suspended.ID, PlanRevision{
		SkipStepIDs: []string{"old_tail"},
		AppendSteps: []PlanStep{{ID: "new_tail", Tool: "sink", DependsOn: []string{"confirm"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Nodes["old_tail"].Status != NodeStatusSkipped {
		t.Fatalf("old_tail=%s", revised.Nodes["old_tail"].Status)
	}
	if revised.Nodes["new_tail"].Status != NodeStatusPending {
		t.Fatalf("new_tail=%s", revised.Nodes["new_tail"].Status)
	}
	if err := revised.Plan.Validate(scheduler.vocabulary, scheduler.jobBudget); err != nil {
		t.Fatalf("revised plan invalid: %v", err)
	}
}
```

- [ ] **Step 2: 写已执行节点不可修改和冲突原子回滚测试**

```go
func TestReviseRejectsExecutedStepAtomically(t *testing.T) {
	scheduler, run := runWithSucceededHead(t)
	before := run.Version
	_, err := scheduler.Revise(context.Background(), run.ID, PlanRevision{SkipStepIDs: []string{"head"}})
	if !errors.Is(err, ErrReviseExecutedStep) {
		t.Fatalf("err=%v", err)
	}
	after, _ := scheduler.store.GetRun(context.Background(), run.ID)
	if after.Version != before || after.Nodes["head"].Status != NodeStatusSucceeded {
		t.Fatalf("mutation leaked: %+v", after)
	}
}
```

- [ ] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestRevise' -v`

Expected: 编译失败，缺少 `PlanRevision`、`Revise`。

- [ ] **Step 4: 实现修订 API**

```go
var ErrReviseExecutedStep = errors.New("cannot revise an executed plan step")

type PlanRevision struct {
	SkipStepIDs []string `json:"skip_step_ids,omitempty"`
	AppendSteps []PlanStep `json:"append_steps,omitempty"`
}

func (s *Scheduler) Revise(ctx context.Context, runID string, revision PlanRevision) (PlanRun, error)
```

在单次 `MutateRun` 回调内构造候选 Plan 和 Nodes，验证：只能 skip pending；追加 ID 不重复；依赖可指向现有或同批追加节点；整份候选 Plan 必须通过 Validate。任何错误不得增加 Version。

- [ ] **Step 5: 运行 GREEN 和 race**

Run: `go test -race ./internal/aigc/orchestration -run 'TestRevise' -v`

Expected: PASS。

- [ ] **Step 6: 提交并执行可行性 Gate 1**

Run: `go test -race ./internal/aigc/orchestration`

Expected: PASS。若无法同时满足“已执行不回滚、挂起中可追加、CAS 冲突不泄漏”，停止计划。

```bash
git add internal/aigc/orchestration/revision.go internal/aigc/orchestration/revision_test.go
git commit -m "feat(orchestration): support live plan revision"
```

---

### Task 5: 三个验收原子工具

**Files:**
- Create: `internal/aigc/vocabulary/write_media_prompt.go`
- Create: `internal/aigc/vocabulary/request_confirmation.go`
- Create: `internal/aigc/vocabulary/dispatch_generation.go`
- Create: `internal/aigc/vocabulary/runtime_tools_test.go`

- [ ] **Step 1: 写 Descriptor 和行为失败测试**

```go
func TestRuntimeToolsExposeStableContracts(t *testing.T) {
	prompt := NewWriteMediaPromptTool(fakePromptWriter{prompt: "cinematic rain"})
	confirm := NewRequestConfirmationTool()
	dispatch := NewDispatchGenerationTool(fakeGenerationDispatcher{batchID: "batch-1"})

	if prompt.Descriptor().Key != "write_media_prompt" || confirm.Descriptor().Key != "request_confirmation" || dispatch.Descriptor().Key != "dispatch_generation" {
		t.Fatal("unexpected tool keys")
	}
	result, err := confirm.Run(context.Background(), Call{Inputs: map[string]any{"question": "continue?"}})
	if err != nil || result.Suspension == nil || result.Suspension.Reason != "waiting_user" {
		t.Fatalf("confirmation=%+v err=%v", result, err)
	}
	result, err = dispatch.Run(context.Background(), validDispatchCall())
	if err != nil || result.Suspension == nil || result.Suspension.Reason != "waiting_jobs" || result.Outputs["batch_id"] != "batch-1" {
		t.Fatalf("dispatch=%+v err=%v", result, err)
	}
}
```

- [ ] **Step 2: 写幂等键透传测试**

fake dispatcher 记录 `Call.PlanRunID/NodeID/Attempt/IdempotencyKey`，断言 dispatch request 的业务幂等基键由这四项组成；同一 Call 重放返回相同 batch_id。

- [ ] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/vocabulary -run 'TestRuntimeTools' -v`

Expected: 编译失败，缺少三个 constructor。

- [ ] **Step 4: 实现窄接口和工具**

```go
type PromptWriter interface {
	WritePrompt(ctx context.Context, inputs map[string]any) (string, error)
}

type GenerationDispatchRequest struct {
	SessionID string
	UserID string
	PlanRunID string
	NodeID string
	Attempt int
	IdempotencyKey string
	Inputs map[string]any
}

type GenerationDispatchResult struct {
	OperationID string
	BatchID string
	JobIDs []string
}

type GenerationDispatcher interface {
	Dispatch(ctx context.Context, request GenerationDispatchRequest) (GenerationDispatchResult, error)
}
```

工具不得依赖 `orchestration` 包，避免 vocabulary -> orchestration 环。业务拒绝放 `Result.Fail`，基础设施故障才返回 Go error。

- [ ] **Step 5: 运行 GREEN**

Run: `go test ./internal/aigc/vocabulary -run 'TestRuntimeTools' -v && go test ./internal/aigc/vocabulary`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/aigc/vocabulary/write_media_prompt.go internal/aigc/vocabulary/request_confirmation.go internal/aigc/vocabulary/dispatch_generation.go internal/aigc/vocabulary/runtime_tools_test.go
git commit -m "feat(vocabulary): add runtime acceptance tools"
```

---

### Task 6: Generation Bridge 与 waiting_jobs 唤醒

**Files:**
- Create: `internal/aigc/orchestration/generation_bridge.go`
- Create: `internal/aigc/orchestration/generation_bridge_test.go`
- Modify: `internal/aigc/orchestration/resume.go`
- Modify: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写现有 Workflow 创建映射测试**

使用 generation fake store，断言 `GenerationDispatchRequest` 映射为：target=`session_deliverable`；Operation/Batch/Job 同一创建调用；request fingerprint 包含 PlanRunID、NodeID、target 和 Attempt；返回真实 OperationID/BatchID/JobIDs。

- [ ] **Step 2: 写 waiting_jobs 完成测试**

```go
func TestCompleteJobsWaitResumesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	scheduler, suspended := runWaitingForBatch(t, "batch-1")
	completed, err := scheduler.CompleteJobsWait(ctx, suspended.ID, suspended.SuspendedNodeID, JobsOutcome{
		BatchID: "batch-1", Status: "completed", Summary: map[string]any{"assets": []any{"asset-1"}},
	})
	if err != nil || completed.Status != RunStatusSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	replayed, err := scheduler.CompleteJobsWait(ctx, suspended.ID, suspended.SuspendedNodeID, JobsOutcome{BatchID: "batch-1", Status: "completed"})
	if err != nil || replayed.Version != completed.Version {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
}
```

- [ ] **Step 3: 写 BatchID/NodeID 错配测试**

错误 BatchID 或非 suspended node 必须返回哨兵错误，且不改变 Run Version。

- [ ] **Step 4: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestGenerationBridge|TestCompleteJobsWait' -v`

Expected: 编译失败，缺少 bridge 或 `CompleteJobsWait`。

- [ ] **Step 5: 实现 outcome 和恢复 API**

```go
type JobsOutcome struct {
	BatchID string `json:"batch_id"`
	Status string `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
}

func (s *Scheduler) CompleteJobsWait(ctx context.Context, runID, nodeID string, outcome JobsOutcome) (PlanRun, error)
```

`completed` 把节点置 succeeded；`partial_failed` 根据 step.Required 和 SuccessPolicy 进入 succeeded 或 failed；`failed/cancelled` 写入 `vocabulary.Failure`。首次恢复前必须核对 suspension payload 中的 batch_id，并把 canonical outcome JSON 与摘要写入 NodeRun Outputs。Run 已离开 suspended 后，同一 node_id + batch_id + canonical outcome 的重放直接返回当前快照；同一 batch_id 携带不同 outcome 返回 `ErrJobsOutcomeConflict`，不得改写 receipt。

- [ ] **Step 6: 运行 GREEN 和 generation 回归**

Run: `go test -race ./internal/aigc/orchestration -run 'TestGenerationBridge|TestCompleteJobsWait' -v && go test ./internal/aigc/generation ./internal/aigc/generationruntime`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/aigc/orchestration/generation_bridge.go internal/aigc/orchestration/generation_bridge_test.go internal/aigc/orchestration/resume.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): bridge plan runs to generation batches"
```

---

### Task 7: R1 最小护栏切面

**Files:**
- Create: `internal/aigc/vocabulary/guard_chain.go`
- Create: `internal/aigc/vocabulary/guard_chain_test.go`
- Modify: `internal/aigc/orchestration/scheduler.go`
- Modify: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写硬拦、软拦和通过测试**

```go
func TestGuardChainComplianceDecisions(t *testing.T) {
	guard := NewGuardChain(GuardConfig{HardTerms: []string{"hard-ban"}, SoftTerms: []string{"soft-risk"}})
	if result := guard.Check(context.Background(), GuardCall{ToolCategory: "media", Inputs: map[string]any{"prompt": "hard-ban"}}); result.Fail == nil || result.Fail.Code != "compliance_hard_block" {
		t.Fatalf("hard=%+v", result)
	}
	if result := guard.Check(context.Background(), GuardCall{ToolCategory: "media", Inputs: map[string]any{"prompt": "soft-risk"}}); result.Suspension == nil || result.Suspension.Reason != "waiting_user" {
		t.Fatalf("soft=%+v", result)
	}
	if result := guard.Check(context.Background(), GuardCall{ToolCategory: "media", Inputs: map[string]any{"prompt": "safe"}}); result.Fail != nil || result.Suspension != nil {
		t.Fatalf("safe=%+v", result)
	}
}
```

- [ ] **Step 2: 写非媒体工具不织入和媒体工具必织入测试**

Scheduler 根据 Descriptor.Category 判断，仅 `media` 节点执行 GuardChain；data/interaction/cognition 不重复织入。媒体节点在 guard fail/suspend 时不得调用 Tool.Run。

- [ ] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/vocabulary ./internal/aigc/orchestration -run 'TestGuardChain|TestSchedulerGuards' -v`

Expected: 编译失败，缺少 GuardChain。

- [ ] **Step 4: 实现固定顺序切面**

切面顺序固定：permission -> compliance -> reserve credits。v1 permission 只验证 SessionID/UserID 非空；reserve credits 返回通过；compliance 只使用构造时注入的最小规则表，不从 Skill 或模型动态改写。

- [ ] **Step 5: 运行 GREEN 和 race**

Run: `go test -race ./internal/aigc/vocabulary ./internal/aigc/orchestration -run 'TestGuardChain|TestSchedulerGuards' -v`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/aigc/vocabulary/guard_chain.go internal/aigc/vocabulary/guard_chain_test.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): weave media guard chain"
```

---

### Task 8: PostgreSQL RunStore 与跨实例恢复

**Files:**
- Create: `internal/aigc/orchestration/postgres_run_store.go`
- Create: `internal/aigc/orchestration/postgres_run_store_test.go`

- [ ] **Step 1: 写本地 Postgres 可跳过的 CAS 测试**

```go
func TestPostgresRunStoreRoundTripAndCAS(t *testing.T) {
	db, err := storage.OpenAgentPostgres(context.Background(), config.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresRunStore(db)
	if err := store.AutoMigrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateRun(context.Background(), PlanRun{ID: uniqueID(t), SessionID: "s1", UserID: "u1", Plan: validPlan(), Status: RunStatusDraft, Nodes: map[string]*NodeRun{}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.MutateRun(context.Background(), created.ID, created.Version, func(run *PlanRun) error {
		run.Status = RunStatusRunning
		return nil
	})
	if err != nil || updated.Version != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if _, err := store.MutateRun(context.Background(), created.ID, 1, func(*PlanRun) error { return nil }); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("stale=%v", err)
	}
}
```

- [ ] **Step 2: 写“新 Scheduler 实例恢复同一 suspended Run”测试**

第一个 Scheduler Submit 到 waiting_user 后丢弃；第二个 Scheduler 只共享 Postgres Store 和 Vocabulary，以持久化 ResumeKey 恢复并完成。断言交互节点不重跑、下游只跑一次。

- [ ] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestPostgresRunStore|TestSchedulerRecovers' -v`

Expected: 编译失败，缺少 Postgres Store。

- [ ] **Step 4: 实现单表 JSONB 聚合 Store**

```go
type planRunRecord struct {
	ID string `gorm:"primaryKey;size:128"`
	SessionID string `gorm:"index;size:128;not null"`
	Status string `gorm:"index;size:32;not null"`
	Version int `gorm:"not null"`
	Payload datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

`MutateRun` 在事务内 `SELECT ... FOR UPDATE`，比较 expected Version，执行与内存 Store 相同的状态转移校验，然后写入完整 payload 和新 Version。禁止为 Nodes 单独拆表，本阶段需要聚合 CAS 而不是查询优化。

- [ ] **Step 5: 运行 GREEN、包回归和可行性 Gate 2**

Run: `go test -race ./internal/aigc/orchestration`

Expected: PASS。若新实例不能只依赖 Store 恢复，则停止，不进入 Agent 改造。`cmd/aigc-agent` 的 AutoMigrate 和 Scheduler 生产装配由 Plan 2 与用户消息入口同批完成，避免本阶段构造一个无消费者、且缺少生产 PromptWriter 的半接线对象。

- [ ] **Step 6: 提交**

```bash
git add internal/aigc/orchestration/postgres_run_store.go internal/aigc/orchestration/postgres_run_store_test.go
git commit -m "feat(orchestration): persist dynamic plan runs"
```

---

### Task 9: Runtime 端到端验收

**Files:**
- Create: `internal/aigc/integration/dynamic_plan_runtime_test.go`
- Modify: `docs/superpowers/plans/2026-07-12-aigc-dynamic-plan-runtime-v1.md`

- [ ] **Step 1: 写全链测试**

测试必须使用真实 Scheduler、MemoryRunStore、真实三个 vocabulary 工具和 generation bridge fake，计划如下：

```go
plan := orchestration.ExecutionPlan{
	PlanID: "acceptance-image", Source: "dynamic", Summary: "生成一张雨景图", Direction: "image",
	Steps: []orchestration.PlanStep{
		{ID: "prompt", Tool: "write_media_prompt", Params: map[string]any{"target_desc": "雨夜城市"}, Required: true},
		{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "使用该提示词生成？", "prompt": "$prompt.prompt"}, DependsOn: []string{"prompt"}, Required: true},
		{ID: "dispatch", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "$prompt.prompt", "target_type": "session_deliverable"}}}, DependsOn: []string{"confirm"}, Required: true},
	},
	EstimatedJobs: 1,
}
```

断言顺序：Submit -> waiting_user；Resume -> waiting_jobs；CompleteJobsWait -> succeeded。重复 Resume 和重复 CompleteJobsWait 都不改变 Version、Tool 调用次数或 BatchID。

- [ ] **Step 2: 写失败/部分成功验收**

覆盖 `failed` Batch 使 required dispatch 节点失败；`partial_failed` 配合 optional dispatch 使 Run `partial_succeeded`；任何路径都不能遗留 running 节点。

- [ ] **Step 3: 运行端到端验收测试**

Run: `go test ./internal/aigc/integration -run 'TestDynamicPlanRuntime' -v`

Expected: PASS。如果首次失败，只允许失败于 Task 1-8 已定义契约之间的字段映射或状态归纳；测试本身是验收，不为了制造 RED 增加重复需求。

- [ ] **Step 4: 只修正接线，不新增抽象**

允许修改本计划已创建文件中的字段映射和构造器；不得在验收阶段引入第二套 Scheduler、第二套 generation 状态或绕过 RunStore 的进程内快捷路径。

- [ ] **Step 5: 运行 GREEN、race 和全量回归**

Run:

```bash
go test -race ./internal/aigc/orchestration ./internal/aigc/vocabulary ./internal/aigc/integration
go test ./...
cd frontend && npm test -- --run && npm run build
```

Expected: 全部退出码 0。Postgres/Redis 不可用的测试按仓库约定明确 skip；必须在执行记录中列出 skip 情况，不能把 skip 说成集成基础设施已验证。

- [ ] **Step 6: 更新计划执行记录**

在本文末尾追加实际 commit、命令、测试计数、skip 和偏差。不得提前勾选未执行任务。

- [ ] **Step 7: 提交**

```bash
git add internal/aigc/integration/dynamic_plan_runtime_test.go docs/superpowers/plans/2026-07-12-aigc-dynamic-plan-runtime-v1.md
git commit -m "test(orchestration): verify dynamic plan runtime end to end"
```

---

## 2. Plan 2 入口契约

本计划完成后，下一份计划只能基于以下已验证 API：

```go
scheduler.Submit(ctx, sessionID, userID, plan)
scheduler.Advance(ctx, runID)
scheduler.Resume(ctx, runID, resumeKey, decision)
scheduler.CompleteJobsWait(ctx, runID, nodeID, outcome)
scheduler.Revise(ctx, runID, revision)
```

Plan 2 负责：

- TurnContext 只注入 Category 为 cognition/media/interaction 且已实现的词汇目录；data/guard 不进入 Agent 可编排词汇。
- M1 输出方向、带来物、深浅、约束偏好。
- M2 在六模板目录中明显命中/候选选择/不命中三档路由。
- M3 生成 `ExecutionPlan`，校验失败最多修复两次。
- Agent 只提交/修订计划，不直接形成第二条工具执行路径。
- 五 Capability 转成深模板内部适配，不再作为生产 Agent 固定注册面。

Plan 3 负责模板持久化、计划 A2UI 和资产/actor 产品化，不得反向改变本计划的 PlanRun 状态语义。

## 3. 自审记录

- **规格覆盖：** 本计划覆盖 07-11 §1.1 契约复用、§3.5 八要素计划、§3.6 状态机/挂起/修订/单活动计划的 Runtime 地基、§4 Batch 唤醒衔接和 §5 session_deliverable 生成落点。M1/M2/M3 Agent 行为及六模板产品化明确拆入 Plan 2/3。
- **当前状态：** 已完成的 vocabulary Registry 和 ExecutionPlan Validate 不重复实现；新任务从 `PlanRun` 开始。
- **类型一致性：** `waiting_user/waiting_agent/waiting_jobs` 只存在于 `PlanRun.SuspendReason` 与 `vocabulary.Suspension.Reason`；generation bridge 不自建第四种挂起。
- **正确性边界：** Postgres 为 Run 真源，Scheduler 不持有跨调用 goroutine；Redis/Worker 只通过 Batch terminal outcome 唤醒。
- **停止条件：** Task 4 或 Task 8 的可行性 Gate 失败即停止，不用隐藏驻留 goroutine、不可修订 checkpoint 或进程内恢复旁路绕过规格。
- **完整性扫描：** 没有未决占位或模糊实现项；Plan 2/3 是明确排除范围，不是本计划中的未实现步骤。

## 4. 执行记录

尚未执行。任务勾选、commit、测试输出和偏差只在实际执行后追加。
