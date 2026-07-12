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

Plan 2 的入口条件：本计划 Task 4A/4B 的 execution fencing 与活计划修订 Gate、Task 8 的新实例恢复测试、Task 9 的端到端测试全部通过。若 execution claim 不能同时满足“执行前可见、过期可接管、迟到结果不可提交”，或 Task 4B 仍不能保证“已执行不回滚”，停止并回到 07-11 规格 §6.3 重议技术路径。

## 1. 文件结构

| 文件 | 责任 |
| --- | --- |
| `internal/aigc/orchestration/plan_run.go` | `PlanRun/NodeRun`、revision skip provenance、精确 JSON 数字 round-trip、状态转移、RunStore 接口、内存 Store |
| `internal/aigc/orchestration/scheduler.go` | DAG 就绪集、并发执行、参数引用解析、含用户接受修订缺口的终态归纳 |
| `internal/aigc/orchestration/execution_claim.go` | Node execution claim、lease/heartbeat、token fencing 与过期接管 |
| `internal/aigc/orchestration/resume.go` | waiting_user/waiting_agent/waiting_jobs 的一次性恢复 |
| `internal/aigc/orchestration/revision.go` | 活计划裁剪和追加，只允许修改未执行节点；跨实例 CAS 幂等与 canonical step conflict |
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
		{RunStatusDraft, RunStatusSuspended}, // 计划预览直接提交为 suspended(waiting_user)
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
	CommitTimeout time.Duration // <=0 默认 5s，限制 receipt 持久化活性
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

`Submit` 顺序固定为：校验 config/input 与 `plan.Validate` -> 初始化全部 NodeRun 为 pending -> **直接创建最终提交初态**。超预算时 Create `suspended(waiting_user)+PreviewRequired@v1` 并返回；普通计划 Create `running@v1` 后进入 `Advance`。Scheduler 不持久化 draft 中转，避免初态 mutation 失败遗留不可推进的 draft；Task 1 保留 `draft -> suspended` 作为状态机合法边，但 Scheduler 不依赖它。

`Advance` 每轮读取快照，只选择“自身 pending 且全部依赖 succeeded/skipped”的节点。ready 节点仅在当轮局部工作集内视为 executing，不在调用 Tool 前持久化 running；固定 worker pool 大小为 `min(MaxParallel, len(ready))`，取消后不再领取/调用未开始节点。Tool 结果可提交时，才按计划顺序以单次 CAS 将节点从 pending 原子落为 succeeded/failed/suspension；`Tool.Run` Go error 属基础设施错误，节点保持 pending，同 wave 其他 receipt 仍尽力以 `context.WithTimeout(context.WithoutCancel(ctx), CommitTimeout)` 落库（默认 5s，测试用短配置），然后返回 error。进程崩溃或 merge 失败时 durable 节点仍 pending，下一次 `Advance` 用相同 `Attempt`/`IdempotencyKey`（`planRunID:stepID:attempt`）重放，正确性依赖原子 Tool 的幂等契约；这是明确的 at-least-once crash window，不宣称 exactly-once，Task 8 前不引入 lease/owner。

同一 Scheduler 实例内，`Advance` 以可取消、引用计数的 per-run gate 串行化同一个 `PlanRun`，避免同实例并发入口重复调用 Tool；不同 run 使用不同 gate，可各自达到 `MaxParallel`（该上限按 active PlanRun 计算，不是进程全局上限）。gate 在 holder 与全部 waiter refs 归零后安全删除，等待者取消不泄漏引用。跨 Scheduler 实例/跨进程仍可能在 crash window 内以同一幂等键重放，Task 8 持久 Store 接入前不宣称 exactly-once。

Tool Result 在持久化前 fail closed 校验：Suspension reason 仅允许 `waiting_user|waiting_agent|waiting_jobs`；Fail 与 Suspension 互斥；Fail 不得同时携带 Outputs；Suspension 可以携带 Outputs（如 `batch_id`）。同一 ready wave 多个 Suspension 是非法歧义恢复点，相关节点以 `multiple_suspensions` 失败、Run 直接 failed、未执行节点 skipped。

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

Resume 先匹配 Run 级预览 receipt，再按 Plan Step 顺序查找 NodeRun ResumeKey。首次恢复才要求 Run 为 suspended 且命中当前挂起载体，并在同一个 CAS 回调内冻结 decision、清空 Suspension、置 `Resumed=true`、执行 Run `suspended->running`；`Outputs["resume_decision"]` 是保留命名空间，Tool 已占用时原子拒绝，nil decision 冻结为空 map。一次性只表示 decision/receipt 不重复应用，不表示执行推进只尝试一次：命中 `Resumed=true` receipt 后，若 Run 仍为 running，必须在当前 per-run gate 内调用私有 `advance` 继续收敛；只有 Run 已 terminal 或再次 suspended（包括下游新卡点）才只读返回当前权威快照。caller cancel、下游基础设施错误或 commit ACK 丢失后，fresh context 使用同一 key 重放必须继续 running receipt，且不得改写已冻结 decision 或增加 receipt Version。单 Scheduler 的 per-run gate 防止本地重复推进；多个 Scheduler 实例可对同一 running receipt 产生 at-least-once Tool 调用，Tool 必须使用稳定 IdempotencyKey 吸收重复业务效果，CAS 重读后所有调用返回同一 terminal 权威 Run。

- [ ] **Step 6: 运行 GREEN 与 race**

Run: `go test -race ./internal/aigc/orchestration -run 'TestSchedulerSuspends|TestResume|TestEvaluate' -v`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/aigc/orchestration/resume.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): add unified plan suspension and resume"
```

---

### Task 4A: Durable Node Execution Claim / Lease / Fencing

**Why this Gate exists:** Task 4 初次质量审查证明“结果提交式”Scheduler 无法与跨实例活计划修订共存：Tool 已开始执行时 durable Node 仍是 pending，另一个实例可以把它裁掉。实例内 gate 不能解决跨进程竞态。本任务先让“已开始执行”成为 Store 中可判定、可恢复、带 fencing token 的事实，再允许活计划修订通过 Gate。

**Files:**
- Create: `internal/aigc/orchestration/execution_claim.go`
- Create: `internal/aigc/orchestration/execution_claim_test.go`
- Modify: `internal/aigc/orchestration/plan_run.go`
- Modify: `internal/aigc/orchestration/scheduler.go`
- Modify: `internal/aigc/orchestration/scheduler_test.go`

- [ ] **Step 1: 写跨实例 claim 失败测试**

```go
func TestExecutionClaimPreventsConcurrentSchedulersFromRunningSameNode(t *testing.T) {
	store := NewMemoryRunStore()
	clock := newFakeClock(time.Unix(1_700_000_000, 0))
	tool := newBlockingIdempotentTool()
	first := newClaimingScheduler(t, store, "owner-a", clock.Now, tool)
	second := newClaimingScheduler(t, store, "owner-b", clock.Now, tool)
	run := createPendingRun(t, store, oneStepPlan("effect"))

	firstDone := runAdvanceAsync(first, run.ID)
	tool.WaitStarted(t)
	current, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := current.Nodes["effect"]
	if node.Status != NodeStatusRunning || node.ExecutionOwner != "owner-a" || node.ExecutionToken == "" || node.LeaseUntil == nil {
		t.Fatalf("claim=%+v", node)
	}
	secondRun, err := second.Advance(context.Background(), run.ID)
	if err != nil || secondRun.Status != RunStatusRunning || tool.Invocations() != 1 {
		t.Fatalf("second=%+v calls=%d err=%v", secondRun, tool.Invocations(), err)
	}
	tool.Release()
	if result := <-firstDone; result.err != nil || result.run.Status != RunStatusSucceeded {
		t.Fatalf("first=%+v err=%v", result.run, result.err)
	}
}
```

- [ ] **Step 2: 写 lease 过期接管与迟到结果 fencing 测试**

第一个 Scheduler claim 后模拟进程退出；fake clock 推进超过 LeaseTTL。第二个 Scheduler 必须保留原 Attempt 和 IdempotencyKey、写入新 token 后接管。第一个 token 的迟到 outcome 必须成为 no-op，不增加 Version、不覆盖第二个 token 的权威结果。

- [ ] **Step 3: 写 heartbeat 与主动释放测试**

长 Tool 每 `LeaseTTL/3` 续租；clock 未超过最新 lease 时其他实例不能接管。Tool 返回 infrastructure error、caller cancel 或未开始的 claimed node 必须原子退回 pending，清 owner/token/lease，但保留 Attempt，使下一次 claim 使用同一 idempotency key。

- [ ] **Step 4: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestExecutionClaim' -v`

Expected: 编译失败，缺少 execution claim 字段、Scheduler owner/lease 配置或 claim 实现。

- [ ] **Step 5: 实现 claim 类型与配置**

```go
type NodeRun struct {
	// existing fields...
	ExecutionEpoch int64      `json:"execution_epoch,omitempty"`
	ExecutionOwner string     `json:"execution_owner,omitempty"`
	ExecutionToken string     `json:"execution_token,omitempty"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
}

type SchedulerConfig struct {
	// existing fields...
	OwnerID  string
	LeaseTTL time.Duration
	Now      func() time.Time
	NewToken func() string
}
```

`OwnerID`、`Now`、`NewToken` 必填；LeaseTTL 默认 30 秒。每次 claim acquisition 都把 ExecutionEpoch 单调 +1（首次 0 -> 1）；pending 首次 claim 还把 pending -> running、Attempt 0 -> 1，tool error/cancel/heartbeat release 后的 pending 重 claim 与过期 running 接管都保持 Attempt 不变。ExecutionEpoch 已为 `math.MaxInt64` 时必须以可 `errors.Is` 的 fence exhausted 错误原子失败，不得溢出、增加 Version 或调用 Tool。Tool Call 使用 NodeRun 中已冻结的 Attempt。heartbeat、merge、release 必须同时匹配 owner/token/attempt/epoch；heartbeat 还必须确认旧 lease 在单次权威 `Now()` 读数下尚未过期，过期 heartbeat 不得复活 claim。

`Now` 的强契约是：所有 Scheduler 实例必须使用同一权威时钟。Task 4A 只以 Memory Store + fake clock 完成 fencing Gate，不宣称已解决生产 clock skew；Task 8 的 Postgres 装配必须注入 `SELECT now()` 等价语义的共享 DB clock，禁止以各进程 `time.Now` 作为生产 Scheduler clock。若该共享时钟未接入，Task 8 恢复 Gate 不得通过。

- [ ] **Step 6: 实现 claim wave、heartbeat 与 fenced merge**

```go
type executionClaim struct {
	StepID  string
	Attempt int
	Owner   string
	Token   string
}

func (s *Scheduler) claimReady(ctx context.Context, run PlanRun) (PlanRun, []executionClaim, error)
func (s *Scheduler) renewClaims(ctx context.Context, runID string, claims []executionClaim) error
func (s *Scheduler) releaseClaims(ctx context.Context, runID string, claims []executionClaim) error
```

每轮最多 claim `MaxParallel` 个 ready node。所有 outcome merge 必须同时匹配 `Status=running + ExecutionOwner + ExecutionToken + Attempt + ExecutionEpoch`；不匹配是迟到结果，直接 no-op。成功/业务失败/Suspension 清 claim 字段；基础设施失败/取消把同 epoch claim 退回 pending。Heartbeat 使用有界 CommitTimeout context；首次 Store 错误、commit timeout 或 claim lost 必须取消 wave context、停止领取未开始任务并尽力释放同 epoch claim。停止时等待 goroutine 退出，不泄漏 ticker。

- [ ] **Step 7: 写 token mismatch 与 claim 清理测试**

同一 Node 的旧 token outcome、错误 owner heartbeat、重复 release 都必须成为 no-op，不增加 Version；正常 terminal outcome 后 owner/token/lease 全部为空。该步骤只验证 claim/fencing 自身，不依赖 Task 4B 的 Revise API。

- [ ] **Step 8: 运行 GREEN 与 claim Gate**

Run:

```bash
go test -count=50 -race ./internal/aigc/orchestration -run 'TestExecutionClaim'
go test -race ./internal/aigc/orchestration
go test ./...
go vet ./...
```

Expected: 全部 PASS；无双执行、无 stale overwrite、无 heartbeat/gate goroutine 泄漏。

- [ ] **Step 9: 提交**

```bash
git add internal/aigc/orchestration/execution_claim.go internal/aigc/orchestration/execution_claim_test.go internal/aigc/orchestration/plan_run.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): fence durable node execution"
```

---

### Task 4B: 活计划修订 Gate 收口

**Files:**
- Modify: `internal/aigc/orchestration/revision.go`（`8049c4c` 已有实现未通过 Gate，本任务负责修正并复审）
- Modify: `internal/aigc/orchestration/revision_test.go`
- Modify: `internal/aigc/orchestration/plan_run.go`
- Modify: `internal/aigc/orchestration/scheduler.go`
- Modify: `internal/aigc/orchestration/scheduler_test.go`
- Modify: `docs/superpowers/plans/2026-07-12-aigc-dynamic-plan-runtime-v1.md`

- [x] **Step 1: 写未执行节点裁剪和追加测试**

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

- [x] **Step 2: 写已执行节点不可修改和冲突原子回滚测试**

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

- [x] **Step 2A: 写跨实例 Advance vs Revise 竞态测试**

一个 Scheduler claim 并阻塞在 Tool，另一个 Scheduler 调 `Revise(...SkipStepIDs:[claimedStep])`，必须 `errors.Is(err, ErrReviseExecutedStep)` 且 Run Version/Plan 不变；Tool 释放后原结果正常提交。随后用新 Scheduler 只依赖 Store 对 replacement revision 执行 `Advance` 至终态，断言旧 revision-skipped required 节点不会把成功替代路径误判为 failed。

- [x] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestRevise' -v`

Expected: Task 4 初版 API 已存在；Task 4B RED 编译失败于缺少 `NodeRun.SkipReason` / `SkipReasonRevision`，实现前 replacement 终态与大整数精度契约也不成立。

- [x] **Step 4: 实现修订 API**

```go
var ErrReviseExecutedStep = errors.New("cannot revise an executed plan step")

type PlanRevision struct {
	SkipStepIDs []string `json:"skip_step_ids,omitempty"`
	AppendSteps []PlanStep `json:"append_steps,omitempty"`
}

func (s *Scheduler) Revise(ctx context.Context, runID string, revision PlanRevision) (PlanRun, error)
```

在单次 `MutateRun` 回调内构造候选 Plan 和 Nodes，验证：只能 skip pending；claimed/running 节点必须以 `ErrReviseExecutedStep` 拒绝；追加 ID 不重复；依赖可指向现有或同批追加节点；整份候选 Plan 必须通过 Validate。任何错误不得增加 Version。

Revision 裁掉 required step 后，该 step 的 `NodeRun` 必须记录 `SkipReason="revision"`。终态归纳把 revision-skipped required step 视为明确缺口：只要其余 required replacement 路径成功，Run 为 `partial_succeeded`，不能误判 failed；dependency/error 导致的 skipped 仍按原失败规则处理。

`clonePlanRevision` 与 `clonePlanRun` 都使用 `json.Decoder.UseNumber()` 保留 `map[string]any` 中的大整数；canonical step equality 必须区分 `9007199254740992` 与 `9007199254740993`，序列化错误用 `%w` 保留底层 JSON error chain。只修 revision clone 不够，因为 RunStore round-trip 可能更早把 Plan Params 精度压成 float64。

- [x] **Step 5: 运行 GREEN 和 race**

Run: `go test -race ./internal/aigc/orchestration -run 'TestRevise|TestExecutionClaimPreventsRevise' -v`

Expected: PASS。

- [x] **Step 6: 提交并执行可行性 Gate 1**

Run: `go test -race ./internal/aigc/orchestration`

稳定性 Gate：`go test -count=50 -race ./internal/aigc/orchestration -run 'TestRevise|TestExecutionClaim'`

Expected: PASS。必须额外证明：跨 Scheduler 的 Tool 已 claim 后不能被 Revise；replacement DAG Advance 至终态为 `partial_succeeded`；新 Scheduler 只依赖 Store 即可继续 revised Run；大整数不同定义不被误判幂等。若任一失败，Gate 不通过并停止计划。

```bash
git add docs/superpowers/plans/2026-07-12-aigc-dynamic-plan-runtime-v1.md internal/aigc/orchestration/plan_run.go internal/aigc/orchestration/revision.go internal/aigc/orchestration/revision_test.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "fix(orchestration): close live revision feasibility gate"
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

- [x] **Step 1: 写现有 Workflow 创建映射测试**

使用 generation fake store，断言 `GenerationDispatchRequest` 映射为：target=`session_deliverable`；Operation/Batch/Job 同一创建调用；request fingerprint 包含 PlanRunID、NodeID、target 和 Attempt；返回真实 OperationID/BatchID/JobIDs。

- [x] **Step 2: 写 waiting_jobs 完成测试**

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

- [x] **Step 3: 写 BatchID/NodeID 错配测试**

错误 BatchID 或非 suspended node 必须返回哨兵错误，且不改变 Run Version。

- [x] **Step 4: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestGenerationBridge|TestCompleteJobsWait' -v`

Expected: 编译失败，缺少 bridge 或 `CompleteJobsWait`。

- [x] **Step 5: 实现 outcome 和恢复 API**

```go
type JobsOutcome struct {
	BatchID string `json:"batch_id"`
	Status string `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
}

func (s *Scheduler) CompleteJobsWait(ctx context.Context, runID, nodeID string, outcome JobsOutcome) (PlanRun, error)
```

`completed` 把节点置 succeeded；`partial_failed` 根据 step.Required 和 SuccessPolicy 进入 succeeded 或 failed；`failed/cancelled` 写入 `vocabulary.Failure`。首次恢复前必须核对 suspension payload 中的 batch_id，并把 canonical outcome JSON 与摘要写入 NodeRun Outputs。Run 已离开 suspended 后，同一 node_id + batch_id + canonical outcome 的重放直接返回当前快照；同一 batch_id 携带不同 outcome 返回 `ErrJobsOutcomeConflict`，不得改写 receipt。

- [x] **Step 6: 运行 GREEN 和 generation 回归**

Run: `go test -race ./internal/aigc/orchestration -run 'TestGenerationBridge|TestCompleteJobsWait' -v && go test ./internal/aigc/generation ./internal/aigc/generationruntime`

Expected: PASS。

- [x] **Step 7: 提交**

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

- [x] **Step 1: 写硬拦、软拦和通过测试**

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

- [x] **Step 2: 写非媒体工具不织入和媒体工具必织入测试**

Scheduler 根据 Descriptor.Category 判断，仅 `media` 节点执行 GuardChain；data/interaction/cognition 不重复织入。媒体节点在 guard fail/suspend 时不得调用 Tool.Run。

- [x] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/vocabulary ./internal/aigc/orchestration -run 'TestGuardChain|TestSchedulerGuards' -v`

Expected: 编译失败，缺少 GuardChain。

- [x] **Step 4: 实现固定顺序切面**

切面顺序固定：permission -> compliance -> reserve credits。v1 permission 只验证 SessionID/UserID 非空；reserve credits 返回通过；compliance 只使用构造时注入的最小规则表，不从 Skill 或模型动态改写。

- [x] **Step 5: 运行 GREEN 和 race**

Run: `go test -race ./internal/aigc/vocabulary ./internal/aigc/orchestration -run 'TestGuardChain|TestSchedulerGuards' -v`

Expected: PASS。

- [x] **Step 6: 提交**

```bash
git add internal/aigc/vocabulary/guard_chain.go internal/aigc/vocabulary/guard_chain_test.go internal/aigc/orchestration/scheduler.go internal/aigc/orchestration/scheduler_test.go
git commit -m "feat(orchestration): weave media guard chain"
```

---

### Task 8: PostgreSQL RunStore 与跨实例恢复

**Files:**
- Create: `internal/aigc/orchestration/postgres_run_store.go`
- Create: `internal/aigc/orchestration/postgres_run_store_test.go`

- [x] **Step 1: 写本地 Postgres 可跳过的 CAS 测试**

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

- [x] **Step 2: 写“新 Scheduler 实例恢复同一 suspended Run”测试**

第一个 Scheduler Submit 到 waiting_user 后丢弃；第二个 Scheduler 只共享 Postgres Store 和 Vocabulary，以持久化 ResumeKey 恢复并完成。断言交互节点不重跑、下游只跑一次。

- [x] **Step 3: 运行 RED**

Run: `go test ./internal/aigc/orchestration -run 'TestPostgresRunStore|TestSchedulerRecovers' -v`

Expected: 编译失败，缺少 Postgres Store。

- [x] **Step 4: 实现单表 JSONB 聚合 Store**

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

- [x] **Step 5: 运行 GREEN、包回归和可行性 Gate 2**

Run: `go test -race ./internal/aigc/orchestration`

Expected: PASS。若新实例不能只依赖 Store 恢复，则停止，不进入 Agent 改造。`cmd/aigc-agent` 的 AutoMigrate 和 Scheduler 生产装配由 Plan 2 与用户消息入口同批完成，避免本阶段构造一个无消费者、且缺少生产 PromptWriter 的半接线对象。

- [x] **Step 6: 提交**

```bash
git add internal/aigc/orchestration/postgres_run_store.go internal/aigc/orchestration/postgres_run_store_test.go
git commit -m "feat(orchestration): persist dynamic plan runs"
```

---

### Task 9: Runtime 端到端验收

**Files:**
- Create: `internal/aigc/integration/dynamic_plan_runtime_test.go`
- Modify: `docs/superpowers/plans/2026-07-12-aigc-dynamic-plan-runtime-v1.md`

- [x] **Step 1: 写全链测试**

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

- [x] **Step 2: 写失败/部分成功验收**

覆盖 `failed` Batch 使 required dispatch 节点失败；`partial_failed` 配合 optional dispatch 使 Run `partial_succeeded`；任何路径都不能遗留 running 节点。

- [x] **Step 3: 运行端到端验收测试**

Run: `go test ./internal/aigc/integration -run 'TestDynamicPlanRuntime' -v`

Expected: PASS。如果首次失败，只允许失败于 Task 1-8 已定义契约之间的字段映射或状态归纳；测试本身是验收，不为了制造 RED 增加重复需求。

- [x] **Step 4: 只修正接线，不新增抽象**

允许修改本计划已创建文件中的字段映射和构造器；不得在验收阶段引入第二套 Scheduler、第二套 generation 状态或绕过 RunStore 的进程内快捷路径。

- [x] **Step 5: 运行 GREEN、race 和全量回归**

Run:

```bash
go test -race ./internal/aigc/orchestration ./internal/aigc/vocabulary ./internal/aigc/integration
go test ./...
cd frontend && npm test -- --run && npm run build
```

Expected: 全部退出码 0。Postgres/Redis 不可用的测试按仓库约定明确 skip；必须在执行记录中列出 skip 情况，不能把 skip 说成集成基础设施已验证。

- [x] **Step 6: 更新计划执行记录**

在本文末尾追加实际 commit、命令、测试计数、skip 和偏差。不得提前勾选未执行任务。

- [x] **Step 7: 提交**

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
- **停止条件：** Task 4A/4B 或 Task 8 的可行性 Gate 失败即停止，不用进程内旁路掩盖跨实例 claim、不可修订 checkpoint 或迟到结果 fencing 问题。
- **完整性扫描：** 没有未决占位或模糊实现项；Plan 2/3 是明确排除范围，不是本计划中的未实现步骤。

## 4. 执行记录

- **Task 4A（`f26f4be`..`462d7ac`）：** durable execution claim/lease/fencing 已完成并通过双审；Tool 开始执行前先持久化 `running` claim，迟到结果受 epoch/token fence 约束。
- **Task 4B RED：** 新增终态/大整数/new Scheduler/claim-race 测试后，`go test ./internal/aigc/orchestration -run 'TestReviseTerminalStatus|TestRevisePreservesLarge|TestReviseSerialization|TestReviseReplacement|TestExecutionClaimPreventsRevise' -v` 按预期编译失败，缺少 `NodeRun.SkipReason` 与 `SkipReasonRevision`。
- **Task 4B GREEN/Gate：** `go test -race ./internal/aigc/orchestration -run 'TestRevise|TestExecutionClaimPreventsRevise' -v`、`go test -count=50 -race ./internal/aigc/orchestration -run 'TestRevise|TestExecutionClaim'`、`go test -race ./internal/aigc/orchestration`、`go test ./...`、`go vet ./...` 全部退出码 0。
- **Task 4B 语义结论：** claimed/running 节点不可裁；revision 裁剪具有 `SkipReason="revision"` provenance；replacement 成功和全 required revision-skipped 均归纳为 `partial_succeeded`；新 Scheduler 仅共享 Store/Registry 即可从原 preview receipt 恢复；相邻大整数 canonical definition 不再误判幂等。
- **Task 4B 偏差：** 初版计划的 RED 文案仍写“缺少 Revise API”，但 `8049c4c` 已提供 API；本次 RED 改为针对质量审查发现的 provenance、终态和精度缺口。后续 Task 尚未执行，不在此处勾选。
- **Task 1（`b8f4423`、`ac934b4`、`5fd4b04`）：** 建立 PlanRun 状态机与 MemoryRunStore，并补齐存储隔离和预算预览挂起边界。
- **Task 2（`cb1b578`、`1a675ac`、`32d0a99`、`cd5625f`）：** 实现动态 DAG Scheduler、输入引用解析、可恢复执行与有界提交。
- **Task 3（`4ae1c6d`、`4d29e38`、`5bc25b8`）：** 实现 `waiting_user`/`waiting_agent` 一次性 Resume，并收敛 receipt 重放与并发恢复。
- **Task 4A（`f21caa9`、`f26f4be`、`df735d2`、`f8aee54`、`462d7ac`）：** 加入 durable claim/lease/fencing、heartbeat 丢失处理和每次 claim 的 epoch 推进。
- **Task 4B（`8049c4c`、`74ab3b5`）：** 实现并收口活计划修订 Gate；详细 RED/GREEN 与偏差保留在上方原记录。
- **Task 5（`ee1b8c6`、`1ad8402`、`a2a4f15`）：** 加入三个真实验收原子工具，并使无效 provider receipt 保持可重试。
- **Task 6（`6d0ec9d`、`44999fd`）：** 将 dispatch 接入现有 generation aggregate，并完成 `waiting_jobs` terminal outcome、canonical receipt 与边界加固。
- **Task 7（`e0ce788`、`e9337a7`、`d482f2d`、`41410fc`、`9e187c4`）：** 织入媒体 Guard Chain，处理 typed nil、approval continuation、身份绑定与 suspension key 隔离。
- **Task 8（`61a83dd`、`9c43b8d`、`ffb3700`）：** 实现 PostgreSQL 聚合 RunStore、事务内权威时间采样与 timed lease mutation 原子性。
- **Task 9（本提交）：** 新增真实 Scheduler + MemoryRunStore + 三个真实 vocabulary Tool + GenerationBridge/MemoryStore 的端到端验收。首次运行 3 个 acceptance 中 2 PASS、1 FAIL：optional dispatch 的 `partial_failed` 被错误归纳为 `succeeded`；保留该验收作为 RED，最小修正 Batch outcome 映射为 optional Node failed，使无 required 下游的 Run 归纳为 `partial_succeeded`。已有“optional dispatch 后接 required 下游”单测同步明确为 Run `failed`，未新增 Scheduler、generation 状态或内存旁路。
- **Task 9 GREEN：** `go test ./internal/aigc/integration -run 'TestDynamicPlanRuntime' -v` 为 3 PASS；`go test -race ./internal/aigc/orchestration ./internal/aigc/vocabulary ./internal/aigc/integration`、`go test ./...`、`go vet ./...` 均退出码 0。另以 `go test -json -count=1 ./...` 统计 737 个测试 PASS、0 个测试 SKIP；本次 Postgres/Redis 路径未 skip。
- **前端验证：** 初次 `npm test -- --run` 因隔离 worktree 缺少 `vitest` 未进入测试；执行 `npm install`（112 packages，0 vulnerabilities）后重跑，2 个文件共 73 个测试 PASS。`npm run build` 成功，Vite 转换 91 modules。
- **Task 9 偏差：** 原计划 Files 只列验收测试与本文；验收 RED 证明 Task 6 状态归纳不符合“optional partial => partial_succeeded”，因此按 Step 4 允许范围最小修改 `orchestration/resume.go` 并校准既有 `scheduler_test.go`。没有伪造 Task 1-8 的历史 RED 命令或计数；上方 commits 来自本分支 `git log`。
- **记录时序：** 原 Task 4B 条目中的“后续 Task 尚未执行”是该 Task 完成时的点时记录；按保留历史记录要求未覆盖，后续实际状态以上方 Task 5-9 条目为准。
