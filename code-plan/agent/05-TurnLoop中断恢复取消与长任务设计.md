# 05-TurnLoop中断恢复取消与长任务设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：多轮执行、追加输入、Interrupt/Resume、Preempt/Cancel、长任务、失败恢复
相关代码路径：`services/agent/internal/runtime/turnloop/**`、`services/agent/internal/application/run/**`
相关契约：`docs/standards/TurnLoop执行规范.md`、`docs/architecture/01-Eino能力选型与TurnLoop设计.md`

## 文档目标

- 定义 TurnLoop 在 Agent Runtime 中的职责。
- 定义 session、run、turn、task、interrupt 的关系。
- 定义中断、恢复、拒绝、取消、抢占和长任务状态闭环。
- 确保长任务状态不依赖进程内存。

## 流程范围

- 单轮执行。
- 多轮追加输入。
- Tool 调用后继续推理。
- 扣费确认中断。
- 高风险 Tool 确认中断。
- 业务写入确认中断。
- 补充输入中断。
- 用户确认恢复。
- 用户拒绝或取消。
- 长任务进度与部分完成。

## 项目归档与权限失效闭环

项目归档和权限失效必须作为 TurnLoop 主链路风险单独设计：

| 时机 | 必做校验 | Agent 行为 | 业务能力 |
| --- | --- | --- | --- |
| 创建 session | `CheckProjectAccess(continue_creation)` | 拒绝创建，返回 HTTP `409 PROJECT_ARCHIVED` | `ProjectService.CheckProjectAccess` |
| 创建 run | `CheckProjectAccess(continue_creation)`、资产引用权限 | 不创建 run、不启动 SSE、不冻结积分 | `ProjectService.CheckProjectAccess`、`AssetService.BatchCheckAssetAccess` |
| resume / confirm | `CheckProjectAccess(continue_creation)` | 不恢复执行，interrupt 标记失败或取消，输出只读事件 | `ProjectService.CheckProjectAccess` |
| asset commit 前 | `CheckProjectAccess(commit_asset)` | 不保存新资产，释放未结算冻结积分 | `ProjectService.CheckProjectAccess`、`CreditService.ReleaseFrozenCredits` |
| 运行中新 Tool 前 | `CheckProjectAccess(continue_creation)` 或缓存短 TTL 二次校验 | 停止发起新 Tool，输出 `project.archived.blocked` 和 `agent.run.cancelled` | `ProjectService.CheckProjectAccess` |
| snapshot / 历史恢复 | `CheckProjectAccess(view)` | 允许只读恢复，标记 `readonly_reason=project_archived` | `ProjectService.CheckProjectAccess` |

运行中归档闭环：

```text
发现项目 archived
  -> 停止发起新 Tool
  -> 标记当前 task cancel_requested
  -> 已保存资产保持业务结果
  -> 未结算冻结积分调用 ReleaseFrozenCredits
  -> 写入 agent_events: project.archived.blocked
  -> 写入 agent_events: agent.run.cancelled
  -> 保存 process.snapshot.saved
```

## TurnLoop 接口设计

包路径：`services/agent/internal/runtime/turnloop`。TurnLoop 只处理 Agent Runtime 状态推进，不直接调用业务数据库，所有业务事实读取和写入通过 `BusinessGateway` 完成。

```go
// TurnInput 是一次用户输入启动或追加运行的领域入参。
type TurnInput struct {
    SessionID       string
    RunID           string
    ProjectID       string
    UserInput       UserInputDTO
    AssetRefs       []AssetReferenceDTO
    ModelSelection  ModelSelectionDTO
    AuthContext     AuthContext
    RequestMeta     RequestMeta
    IdempotencyKey  string
}

// ResumeInput 是人工确认、拒绝或补充输入后恢复运行的入参。
type ResumeInput struct {
    InterruptID     string
    Action          string
    ConfirmationDTO ConfirmationInputDTO
    ExtraUserInput  *UserInputDTO
    AuthContext     AuthContext
    RequestMeta     RequestMeta
    IdempotencyKey  string
}

// PreemptInput 是取消、项目归档或权限失效触发抢占的入参。
type PreemptInput struct {
    RunID           string
    Reason          string
    AuthContext     AuthContext
    RequestMeta     RequestMeta
    IdempotencyKey  string
}
```

| 函数 | 入参 | 出参 | 主要职责 |
| --- | --- | --- | --- |
| `StartTurn(ctx, input)` | `TurnInput` | `RunSnapshot`、`error` | 创建或恢复 run 上下文，写入用户消息，进入 Eino Graph。 |
| `ResumeTurn(ctx, input)` | `ResumeInput` | `RunSnapshot`、`error` | 校验 interrupt 状态、项目权限和幂等键，恢复冻结、生成或业务写入步骤；`additional_input` 必须追加用户消息并重新安全评估。 |
| `CancelRun(ctx, input)` | `PreemptInput` | `CancelResult`、`error` | 停止新 Tool，取消可取消任务，释放未结算冻结积分，输出取消事件。 |
| `HandleLongTaskCallback(ctx, taskID, patch)` | `TaskProgressPatch` | `TaskSnapshot`、`error` | 处理异步任务进度、部分完成和终态转换。 |
| `BuildRunSnapshot(ctx, runID)` | `run_id` | `RunSnapshot` | 为补偿失败、恢复页面和只读查看构造快照。 |

## 单轮闭环

```text
StartTurn
  -> CheckProjectAccess(continue_creation)
  -> BatchCheckAssetAccess(reference)
  -> 保存 agent_messages(user)
  -> 发布 agent.run.started / agent.message.created
  -> Route Skill / Tool / Model
  -> EvaluateSafety
  -> EstimateGenerationCredits
  -> 创建 credit_confirmation interrupt
  -> 用户 confirm 后 ResumeTurn
  -> CheckProjectAccess(continue_creation)
  -> FreezeCredits
  -> ExecuteGenerationTasks
  -> CheckProjectAccess(commit_asset)
  -> CommitGeneratedAssetAndCharge
  -> 写入 process.snapshot.saved
  -> run completed
```

每一步必须同时完成三件事：更新领域状态、写入 `agent_events`、输出结构化日志。任一步失败时必须写入用户可见错误事件，并保证冻结积分、任务状态和 run 终态闭合。

## 中断类型

| 中断类型 | 触发时机 | 恢复动作 | 过期行为 |
| --- | --- | --- | --- |
| `credit_confirmation` | 积分预估完成后 | `confirm` 冻结积分并生成；`reject` 结束 run | 释放上下文，不冻结积分。 |
| `risk_confirmation` | 高风险 Tool 或业务写入前 | `confirm` 执行；`reject` 跳过或失败 | 标记 interrupt expired，run failed。 |
| `business_write` | 需要 preview / confirm 的业务写入前 | `confirm` 调业务 RPC；`reject` 不写入 | 不调用写 RPC。 |
| `additional_input` | 缺少必要参数或控件输入 | 补充 `UserInputDTO` 后追加消息、发布 `resume.accepted`、重新 `EvaluateSafety(scene=generation,target_type=prompt)`，通过后继续路由 | 标记 run `failed`，输出 `agent.run.failed`，不预估、不冻结。 |

## ResumeTurn 详细逻辑

```text
ResumeTurn
  -> 按 idempotency_key 校验重复恢复
  -> 读取 interrupt 并校验 status=waiting、未过期、归属当前 run
  -> CheckProjectAccess(continue_creation)
  -> action=reject: 写 confirmation.rejected，run cancelled
  -> action=confirm: 写 confirmation.accepted，校验 payload digest，进入冻结或业务写入
  -> action=additional_input:
       追加 agent_messages(user)
       写 resume.accepted
       写 safety.prompt.evaluating
       EvaluateSafety(scene=generation,target_type=prompt,target_ref_id=run_id)
       安全通过: 写 safety.prompt.evaluated，继续 Route Skill / Tool / Model
       安全阻断或失败: 写 safety.prompt.blocked，run failed
```

`additional_input` 产生的新文本不得复用旧 `SafetyEvidenceDTO`。安全证据摘要必须覆盖追加输入和重新组装后的生成提示词；安全阻断、评估失败或评估超时时均不得调用 `EstimateGenerationCredits`、`EstimateToolCredits`、`FreezeCredits` 或任何 Tool。

## 长任务状态更新

| 状态 | 进入条件 | 可流转到 | AG-UI 事件 |
| --- | --- | --- | --- |
| `pending` | task 已创建但未提交供应商 | `running`、`cancelled`、`failed` | `generation.progress` |
| `running` | 已提交供应商并获得外部引用 | `completed`、`partial_completed`、`failed`、`timeout`、`cancelled` | `tool.call.started`、`generation.progress` |
| `partial_completed` | 部分 artifact 可保存 | `completed`、`cancelled`、`failed` | `generation.artifact.completed`、`generation.progress` |
| `completed` | 所有预期 artifact 完成 | 终态 | `generation.artifact.completed` |
| `failed` | 供应商失败或参数错误 | 终态 | `tool.call.failed`、`agent.run.failed` |
| `timeout` | 超过策略时间 | `cancelled`、`failed` | `generation.progress`、`agent.run.failed` |
| `cancelled` | 用户取消或归档抢占 | 终态 | `agent.run.cancelled` |

## 失败补偿

| 失败点 | 补偿动作 | 事件 |
| --- | --- | --- |
| 安全失败或超时 | 不预估、不冻结、不生成 | `safety.prompt.blocked` 或 `safety.prompt.failed` |
| 积分不足 | 不创建确认或创建不可确认摘要 | `credits.insufficient`、`agent.run.failed` |
| 用户拒绝确认 | run 标记 cancelled，不冻结 | `confirmation.rejected`、`agent.run.cancelled` |
| 冻结失败 | run failed，不生成 | `credits.freeze.failed`、`agent.run.failed` |
| Tool 失败 | 可重试按策略重试；不可重试释放冻结 | `tool.call.failed`、`credits.released` |
| 部分完成 | 完成部分进入 asset commit，未完成部分释放 | `generation.partial.completed` |
| 资产保存失败 | 调 `ReleaseFrozenCredits` | `asset.save.failed`、`credits.released` |
| 追加输入安全阻断 | 不继续路由，不预估、不冻结、不生成 | `resume.accepted`、`safety.prompt.blocked`、`agent.run.failed` |
| 项目归档 | 停新 Tool，释放未结算冻结积分 | `project.archived.blocked`、`agent.run.cancelled` |

## 测试场景

TurnLoop 测试必须覆盖：正常生成、确认接受、确认拒绝、确认重复提交、追加输入安全通过、追加输入安全阻断、项目创建后归档、resume 前权限失效、Tool 超时、用户取消、部分完成、资产保存失败释放积分、SSE 断线后 snapshot 恢复。所有测试都需要断言 run、task、interrupt、event 四类状态同时闭合。

## 业务开发对齐点

- 运行中项目归档时如何释放未结算冻结积分。
- 已成功业务写入是否允许取消回滚。
- 业务写入是否需要 preview / confirm RPC。

## 【业务开发】需要提供的能力与参数

| 场景 | 业务能力 | 参数 |
| --- | --- | --- |
| 创作前权限校验 | `ProjectService.CheckProjectAccess` | `auth_context`、`project_id`、`access_purpose=continue_creation`、`trace_id`。 |
| 保存资产前权限校验 | `ProjectService.CheckProjectAccess` | `auth_context`、`project_id`、`access_purpose=commit_asset`、`run_id`。 |
| 运行中归档释放积分 | `CreditService.ReleaseFrozenCredits` | `freeze_id`、`release_points`、`reason=project_archived`、`run_id`、`idempotency_key`。 |
| 权限失效错误 | 所有业务 RPC | 稳定返回 `PERMISSION_DENIED`、`CROSS_SPACE_DENIED`、`PROJECT_ARCHIVED`、`STATE_CONFLICT` 和用户可见摘要。 |
