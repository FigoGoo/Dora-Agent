# Phase A Spike：执行计划 runtime 可行性 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 证实/证伪 §6.3 首要技术风险——8 要素执行计划（最小子集：元信息+节点+依赖边+卡点）能否被可靠执行：并发 DAG 调度 → 挂起 → 新实例恢复 → 幂等跑完；非法计划被拒。并对"Eino 动态构图"与"自写调度器"两条路径出对比结论。

**背景（主线回退，见终版 §6.7）：** Agent 注册面将切换为原子词汇 + 提交计划元能力；执行计划是新主线的执行载体。本 spike 的代码不是弃件——schema 与调度器直接作为 Phase C 计划 runtime 的起点（TDD 测试即验收）。

**路径预判（spike 要证实或推翻）：**
- **B（自写调度器，主路径）**：durable 底座的 Operation/Batch/Job 本质就是"无依赖边的扁平计划执行器"（状态机/挂起唤醒/幂等/恢复全齐）——计划 runtime = 同一模式 + 依赖边 + 非媒体节点。节点状态在 store，活计划修订=改未执行节点数据，无结构耦合问题。
- **A（Eino 动态构图，证实/证伪点）**：mediagraph 先例证明"重启后重新 Compile 同构图再 resume"可行（generator.go:132-191），动态计划同理（同一计划 JSON→同构图）。硬点：**活计划修订**（§3.6 ②）后图结构变化，旧 checkpoint 与新图不匹配——预判 A 表达修订的代价过高，仅作证伪记录，不阻塞 B。

**Files:**
- Modify: `internal/aigc/orchestration/plan_node.go`（PlanNode 增 ID/DependsOn/Gate——8 要素最小子集）
- Create: `internal/aigc/orchestration/plan.go`（ExecutionPlan + Validate 五查最小版：结构三查）
- Create: `internal/aigc/orchestration/plan_runtime.go`（内存计划实例 store + 调度器）
- Test: `internal/aigc/orchestration/plan_test.go`、`plan_runtime_test.go`

---

### Task 1: ExecutionPlan schema + 结构校验（环/未知工具引用/悬空依赖）

- [ ] Step 1: 失败测试 `plan_test.go`——合法计划过校验；含环计划、依赖不存在节点、节点 Validate 失败三类各被拒
- [ ] Step 2: 实现 `ExecutionPlan{PlanID, Source(template|dynamic), Summary, Nodes []PlanStep}`；`PlanStep{ID, Node PlanNode, DependsOn []string, Gate string(""|"user_confirm")}`；`Validate()` 做拓扑检查
- [ ] Step 3: 绿 + commit

### Task 2: 调度器——并发执行 + 卡点挂起 + 状态落 store

- [ ] Step 1: 失败测试——菱形 DAG（A→B,C→D）用注入的假执行器跑到底，B/C 并发（以执行器记录的重叠或调度轮次断言）；带 Gate 的节点执行**前**挂起，实例状态=suspended(waiting_user)，已完成节点状态正确
- [ ] Step 2: 实现 `PlanRun` 状态机（draft→running⇄suspended→succeeded/failed/cancelled；节点 pending→running→succeeded/failed/skipped）+ 内存 store（接口化，字段风格对齐 generation workflow store）+ 调度循环（就绪集=依赖全 succeeded 且未执行）
- [ ] Step 3: 绿 + commit

### Task 3: 恢复 + 幂等（spike 核心判据)

- [ ] Step 1: 失败测试——挂起后**丢弃调度器实例**，从 store 新建实例 resume(decision)：已执行节点不重跑（执行器调用计数不变），Gate 节点执行，计划跑到 succeeded；对同一 resume 重复调用幂等（同结果、无重复执行）
- [ ] Step 2: 实现 resume 路径（gate 决策落节点记录 + 一次性）
- [ ] Step 3: 绿 + commit

### Task 4: 活计划修订（§3.6 ② 最小验证）

- [ ] Step 1: 失败测试——挂起中修订：裁掉一个未执行节点（置 skipped）+ 追加一个新节点（续编）；resume 后按新结构跑完；已执行节点不受影响
- [ ] Step 2: 实现 `ReviseNodes`（仅允许动未执行节点，动已执行报错）
- [ ] Step 3: 绿 + commit

### Task 5: Eino A 路径证实/证伪 + 结论记录

- [ ] Step 1: 测试——两个不同计划 JSON 动态构两张 Eino 图（Lambda 节点包工具执行器）compile 均成功；interrupt 挂起后以**同一计划重建的图** resume 成功（mediagraph 模式复刻）
- [ ] Step 2: 尝试表达 Task 4 的"修订后恢复"：结构变化后旧 checkpoint resume——记录实际行为（预判失败或需整套自定义 state 迁移）
- [ ] Step 3: 在本文档追加《Spike 结论》：A/B 对比表（挂起恢复/活计划修订/与 jobs 底座接缝/代码量）+ 判定；commit

### Task 6: 回归 + 收尾

- [ ] `go build ./... && go vet ./... && go test ./...` 全绿；勾选本计划；结论同步终版 §6.3
