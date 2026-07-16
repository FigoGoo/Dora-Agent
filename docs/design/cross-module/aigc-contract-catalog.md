# AIGC 跨 Module 契约目录

> 文档状态：Draft / 待 Business、Agent、Worker 三方评审
>
> 契约版本：`aigc.contract.v1alpha1`
>
> 设计日期：2026-07-14
>
> 实现门禁：本文通过评审并冻结为 `v1` 前，不得创建对应生产表、IDL、事件消费者或 Graph Tool 生产代码。

> Runner、PostgreSQL Session Lane、Receipt、Approval Continuation、Checkpoint 和 A2UI/Event 的运行契约详见 [Agent Runner 与 PostgreSQL Session Lane v1 设计评审](../agent/runner-session-lane-review-v1.md)。该文档同为 Draft，不能作为实现批准。

## 1. 目标与非目标

本文冻结六个 Agent-facing Graph Tool 跨越 `business/`、`agent/`、`worker/` 时的权威数据、公开契约、幂等边界、计费顺序、异步终态与恢复规则，是六份 Graph Tool 独立设计文档的共同契约基线。

本文目标：

- 让每类数据只有一个 Migration Owner 和一个权威来源；
- 让跨 Module 调用只依赖版本化 RPC、Event 或持久化消费契约；
- 让“已扣费但未派发”“响应丢失”“Worker 重复执行”等 unknown outcome 有唯一恢复路径；
- 让 Graph、业务状态机、Worker 执行状态和 A2UI 展示状态相互解耦；
- 为 M1～M4 实现及 M5 全功能冒烟提供可追踪的契约编号。

本文不负责：

- 定义页面布局、文案或视觉样式；
- 固定 Provider SDK、模型名、单价、超时秒数、并发数等运行参数；
- 用共享 Go 包跨越三个独立 Go Module；
- 把 Redis、Eino Checkpoint 或 A2UI EventLog 作为业务权威来源。

## 2. 不可变架构决策

### 2.1 Module 和运行时边界

| Module | 生产 Runtime | 职责 | 禁止事项 |
|---|---|---|---|
| Business | `business/cmd/business-service` | 用户、项目、Skill、创作领域对象、素材与资产、Storyboard、Prompt、Assembly Plan、计费、绑定 | 不执行 ChatModelAgent，不直接调用媒体 Provider，不引用其他 Module 的 `internal` 包 |
| Agent | `agent/cmd/agent-service` | Session/Turn/Run、唯一 ChatModelAgent、六个 Graph Tool、Approval、Operation/Batch/Job、Checkpoint、Receipt、A2UI EventLog | 不拥有创作领域最终数据，不执行长耗时 Provider 任务，不把自然语言确认当授权 |
| Worker | `worker/cmd/business-worker` | Claim、租约、Provider 调用、上传、私有 Attempt/Receipt、终态提交 | 不决策业务语义，不扣费，不生成 A2UI，不修改 Storyboard/Asset 权威表 |

根目录不作为生产 Go Module；根 `go.work` 仅用于本地联调。

### 2.2 权威数据与 Migration Owner

| 聚合或数据 | 权威来源 | Migration Owner | 其他 Module 的访问方式 |
|---|---|---|---|
| User、Project、Skill Draft/Published Snapshot | Business PostgreSQL | Business | Business RPC/Event |
| CreationSpec、MaterialAnalysis、Storyboard、PromptArtifact、AssemblyPlan | Business PostgreSQL | Business | Business RPC/Event；禁止直表写入 |
| Asset、Binding、积分流水、计费准备与结算回执 | Business PostgreSQL | Business | Business RPC/Event；禁止 Worker/Agent 自行扣费 |
| Session、Input、Turn、Run、ToolReceipt、ModelReceipt、Approval | Agent PostgreSQL | Agent | Agent HTTP/SSE；内部 Repository |
| Operation、Batch、Job、Dispatch Outbox、Inbox、Continuation Result | Agent PostgreSQL | Agent | Agent 内部 Repository；Worker 仅使用 `AGT-JOB-V1` 持久化消费契约 |
| WorkerAttempt、ProviderReceipt、UploadReceipt | Worker PostgreSQL | Worker | Worker 内部 Repository；必要信息通过终态提交进入 Agent/Business |
| A2UI EventLog、Card Revision | Agent PostgreSQL | Agent | Agent SSE；前端不反向写 EventLog |
| Redis Stream/List/缓存/租约提示 | 非权威 | 各使用方 | 只能唤醒、加速和削峰；必须能从 PostgreSQL 恢复 |
| etcd 注册发现 | 非业务权威 | 各生产 Runtime | 仅注册发现和配置定位，不存业务状态 |

### 2.3 Graph 与长生命周期状态

- 六个 Tool 均为启动时编译的 Eino `compose.Graph`；DAG 使用 `AllPredecessor` 触发模式。
- Graph State 只在一次调用内存在，不是跨分钟业务状态；所有业务状态由 PostgreSQL 聚合及版本号承载。
- 用户审批默认结束当前 Graph，结果为 `waiting_user`。审批完成后创建新的 Continuation Turn，以可信 `ApprovalContinuationResult` 重新进入 Tool。
- 媒体生成和真实渲染在原子派发完成后结束 Graph，结果为 `accepted`。Worker 终态通过 Inbox 形成新的 `BatchContinuationResult`，不恢复旧 Graph 栈。
- Eino Checkpoint 只用于同一短生命周期调用内的故障恢复；不得替代 Approval、Job、Outbox 或业务状态机。

### 2.4 当前实现事实与目标边界

截至 2026-07-14，Agent 仅具备 W0 Session/Input/Event 基础：`session_runtime_lease` 仍是空租约骨架；`session_input` 虽声明 `pending/claimed/running/retry_wait/resolved/dead`，生产路径只写入 `pending`，且 `source_type` 只允许 `user_message`。Turn、Run、ModelReceipt、ToolReceipt、Approval、Checkpoint、A2UI 后端、输入 Claim/处理器均尚未实现；`agent/go.mod` 也尚未引入 Eino。前端遗留 `/api/aigc/**` A2UI 资产没有匹配的当前后端，不能被当成目标契约或兼容承诺。

后续实现必须遵守：

- Eino 依赖通过独立依赖 PR 精确锁定 `github.com/cloudwego/eino v0.9.10` 与已审核 Adapter，并完成版本兼容验证，不能在业务实现 PR 中隐式追加；
- 现有 W0 Migration 不得回改，所有新表、字段、约束和索引只允许使用向前 Migration；
- Runner/Session Lane 设计、本文以及所有受影响 HTTP/RPC/Event/DTO 契约先评审，之后才能进入 Go、SQL 或 IDL 实现；
- 前端 A2UI 接入另行迁移到版本化 Event/Action 契约，不能让后端适配未冻结的遗留接口。

## 3. 通用契约约定

### 3.1 标识、时间与版本

- 所有新主键使用 UUIDv7；跨 Module 传输使用字符串格式 UUID。
- 所有时间使用 UTC，IDL 字段采用毫秒 Unix 时间或明确的 RFC 3339 时间戳，单个契约内不得混用。
- 每个 Envelope 必须包含 `schema_version`、`event_id/request_id`、`occurred_at`、`producer`。
- 每个可变资源必须包含 `resource_version`；内容型资源另含 `content_digest`。
- 请求中的 `user_id`、`project_id`、权限、预算授权和 Approval 不接受模型或前端自由文本赋值，必须由服务端可信上下文注入或复核。
- 不引入 Tenant 维度。

### 3.2 通用请求上下文 `TrustedCommandContextV1`

| 字段 | 来源 | 约束 |
|---|---|---|
| `user_id` | 已认证 HTTP/Session | 必填；不可由 Tool Intent 覆盖 |
| `project_id` | Session/资源解析 | 必填；必须校验用户访问权 |
| `session_id`、`turn_id`、`run_id` | Agent | 必填；用于审计和回放 |
| `graph_run_id`、`tool_call_id` | Agent | 必填；一次 Graph/Tool 唯一 |
| `tool_key`、`definition_version` | Tool Registry | 必填；必须匹配服务端 Registry |
| `skill_invocation_id` | Agent Skill 中间件 | 可选；存在时必须能追溯 Published Snapshot |
| `idempotency_key` | Agent 派生 | 必填；不得使用随机重试键 |
| `budget_authorization_id` | Agent 权威记录 | 可选；只接受服务端查询后的冻结快照 |
| `approval_id` | Agent 权威记录 | 可选；必须绑定用户、动作、资源版本、摘要、金额范围和有效期 |
| `deadline_at` | Agent 策略配置 | 必填；下游不得擅自延长 |

### 3.3 通用 Tool Result

Tool 对模型返回稳定、短小、可机读的 `GraphToolResultV1`，大对象通过 Resource 引用：

| 字段 | 说明 |
|---|---|
| `status` | `completed`、`accepted`、`waiting_user`、`partial`、`failed`、`cancelled` |
| `result_code` | 稳定业务结果码，不承载自由文本判断 |
| `summary` | 可展示摘要；不得包含密钥、内部栈或 Provider 原始响应 |
| `resource_refs[]` | `resource_type/resource_id/resource_version/content_digest` |
| `approval_ref` | 仅 `waiting_user` 返回；包含 Approval ID 和 Card ID |
| `operation_ref` | 仅异步受理返回；包含 Operation/Batch ID，不返回内部 Job 细节 |
| `receipt_ref` | Tool/Charge/Dispatch 回执引用 |
| `warnings[]` | 稳定警告码及可本地化参数 |
| `retryable` | 是否可在同一用户意图下安全重试；不等于立即自动重试 |

`GraphToolResultV1` 只表达一次 Tool 调用对模型可见的冻结语义结果，不是 ToolReceipt、Approval、Operation/Batch/Job、Turn/Run 或领域资源的状态机。六种状态必须遵守：

- `completed`：本次有界工作已完成，不能同时存在待决 Approval 或仍在执行的 Operation；
- `accepted`：异步派发已原子提交，必须含 `operation_ref` 和派发/受理回执，不能被解释为执行完成；
- `waiting_user`：必须含 `approval_ref`，对应 Approval 仍为 `pending`，不得同时含待执行动作的 `operation_ref`；
- `partial`：必须至少包含一个成功资源引用，并包含稳定 Warning/失败明细；异步资源终态仍从 Operation/Batch 查询；
- `failed`：必须含稳定 `result_code`，不得用历史资源引用伪装本次成功；
- `cancelled`：仅表示本次调用已取消，不承诺已回滚、退款或外部 Operation 已取消。

每个结果都必须先冻结到 ToolReceipt 并返回 `receipt_ref`。`accepted` 缺少 Operation、`waiting_user` 缺少 Approval、同时返回 Approval 与待执行 Operation、`completed` 仍引用 pending Approval/活动 Operation、`partial` 没有任何成功引用或告警，均为非法组合。完整固定向量、ToolReceipt first-write-wins 规则和恢复语义由 `runner-session-lane-review-v1.md` 冻结。

ToolReceipt 的 `request_semantic_digest` 必须在执行前冻结，只覆盖当时已存在的规范化 Intent、Tool Pin、可信范围、输入资源和策略/预算版本。Receipt 处于 `open` 时，本次执行产生或核对出的 PromptArtifact、Model、Approval、Consumption、Quote/Charge、Business Write、Operation 或 Resource 权威引用必须按稳定 `ref_slot` append-once 写入独立 `execution_refs`：同 slot 同 digest 重放原引用、异 digest 冲突，且不得回写请求摘要。`result_status/result_digest/result_refs` 只允许在 `open -> frozen` 时一次写入，`result_refs` 必须从既有 `execution_refs` 按结果白名单确定性投影，冻结时不得补做下游调用或临时造引用。

### 3.4 Agent 通用运行契约目录

| 编号 | 契约 | 权威来源 | 不可变边界 |
|---|---|---|---|
| `AGT-RUN-001` | PostgreSQL Session Lane | Agent PostgreSQL | 同 Session 串行、严格 HOL、Lease/Fence；Redis 只能唤醒 |
| `AGT-RUN-002` | Input/Turn/Run | Agent PostgreSQL | 稳定 ID；技术重试复用原 Turn/Run；等待审批不占活跃 Run |
| `AGT-RUN-003` | ModelReceipt | Agent PostgreSQL | 请求前落摘要、输出完全冻结后投影；unknown outcome 先查询，禁止盲重发 |
| `AGT-RUN-004` | ToolReceipt | Agent PostgreSQL | `(session_id, turn_id, tool_call_id)` first-write-wins；执行前冻结 `request_semantic_digest`；open 阶段按 slot append-only `execution_refs`；结果字段只在 `open -> frozen` 一次写且从 execution refs 投影 |
| `AGT-RUN-005` | Approval/Continuation | Agent PostgreSQL | 决策状态与消费事实分离；批准后创建受信系统 Input/Turn，由 Runner 确定性调用原 pinned Graph，不进入模型 ReAct |
| `AGT-RUN-006` | PostgreSQL Checkpoint | Agent PostgreSQL | 仅短期技术恢复；Fence 是 revision provenance，高 Fence 接管须 CAS 新 epoch；不是业务权威来源 |
| `AGT-RUN-007` | A2UI/EventLog | Agent PostgreSQL | 这里只冻结运行边界；W2-R08 仍未关闭，必须另交独立白名单/DTO/错误信封契约 |
| `AGT-RUN-008` | Budget/Cancel | Agent PostgreSQL | 冻结预算快照、持久化计数、父级 deadline/cancel 传播；取消不等于退款 |

## 4. Business RPC 契约目录

命名空间暂定为 `dora.business.aigc.v1`。下表冻结语义和幂等边界；正式 Thrift 字段在实现前由三方评审生成，不得直接共享 Go DTO。

| 编号 | 方法 | 调用方 | 核心语义 | 幂等键/版本 Guard | Unknown outcome 查询 |
|---|---|---|---|---|---|
| `BIZ-AIGC-001` | `GetGraphToolContext` | Agent | 按 Tool 和资源范围返回授权后的 Project、Published Skill Snapshot、领域版本摘要 | 读请求；必须带用户和项目 | 原请求可重试 |
| `BIZ-AIGC-002` | `BatchGetAssetAnalysisInputs` | Agent | 返回授权素材的元数据、已持久化提取文本/视觉/音频/视频摘要及缺失原因 | 资源版本列表 | 原请求可重试；不得让模型臆测缺失内容 |
| `BIZ-AIGC-003` | `PrepareBillableExecution` | Agent | 对同步模型/规则执行冻结用量口径并立即扣费，返回 Charge Receipt | `user_id + tool_call_id + execution_digest` | `GetBillableExecutionReceipt` |
| `BIZ-AIGC-004` | `GetBillableExecutionReceipt` | Agent | 查询同步执行扣费是否已提交 | 同上 | 本方法即权威查询 |
| `BIZ-AIGC-005` | `FinalizeBillableExecution` | Agent | 记录成功、失败、取消及收入确认资格；默认不退款 | Charge Receipt + terminal receipt | `GetBillableExecutionReceipt` |
| `BIZ-AIGC-006` | `SaveMaterialAnalysisCandidate` | Agent | 保存素材分析版本及证据范围 | `tool_call_id + input_digest`；项目版本 Guard | 按幂等键查询返回相同版本 |
| `BIZ-AIGC-007` | `SaveCreationSpecCandidate` | Agent | 保存 Creation Spec 候选，状态进入 `reviewing` | `tool_call_id + candidate_digest`；基线版本 Guard | 按幂等键查询 |
| `BIZ-AIGC-008` | [`DecideCreationSpecCandidate`](./creation-spec-candidate-decision-contract-v1.md) | Agent Continuation | `action=approve` 激活候选时必须携带不可变 Decision Receipt 与已认证 Agent 生成的 `ApprovalConsumptionReceiptV1` 并逐字段验证；`action=reject` 只接受不可变 Decision Receipt且禁止 Consumption Receipt | 复用 child `business_decide` prepared-slot key `tr:<child_receipt_id>:business_decide:v1` 并绑定 request digest；另校验 Decision identity/action、approve consumption 与候选版本/digest Guard | 按原幂等键与 request digest 查询；`not_found` 仍属 unknown，不授权重发 |
| `BIZ-AIGC-009` | `SaveStoryboardCandidate` | Agent | 保存 Storyboard Revision 候选、稳定 Element/Slot ID 和 Diff | `tool_call_id + candidate_digest`；Creation Spec/Storyboard 基线 Guard | 按幂等键查询 |
| `BIZ-AIGC-010` | `DecideStoryboardCandidate` | Agent Continuation | `action=approve` 激活 Storyboard 候选时必须携带不可变 Decision Receipt 与已认证 Agent 生成的 `ApprovalConsumptionReceiptV1` 并逐字段验证；`action=reject` 只接受不可变 Decision Receipt且禁止 Consumption Receipt | Decision identity + action；approve 另校验 consumption key/digest；候选版本/digest Guard | 按 Decision/Consumption/Revision 幂等键查询状态 |
| `BIZ-AIGC-011` | `SavePromptResults` | Agent | 原子保存独立 Prompt 或 Storyboard Prompt Revision 集合 | `tool_call_id + exact_target_set_digest`；各 Slot 版本 Guard | 按幂等键查询完整目标集合 |
| `BIZ-AIGC-012` | `DecidePromptResults` | Agent Continuation | `action=approve` 仅处理 Approval 冻结的 exact/partial target set，必须携带不可变 Decision Receipt 与已认证 Agent 生成的 `ApprovalConsumptionReceiptV1` 并逐字段验证；`action=reject` 只接受不可变 Decision Receipt且禁止 Consumption Receipt | Decision identity + action；approve 另校验 consumption key/digest；目标集合 digest Guard | 按 Decision/Consumption/目标集合幂等键查询完整 Prompt Revision 集合 |
| `BIZ-AIGC-013` | `SaveAssemblyPlanCandidate` | Agent | 保存可编辑 Timeline/Assembly Plan 候选 | `tool_call_id + plan_digest`；资产和基线版本 Guard | 按幂等键查询 |
| `BIZ-AIGC-014` | `DecideAssemblyPlanCandidate` | Agent Continuation | `action=approve` 激活 Assembly Plan 时必须携带不可变 Decision Receipt 与已认证 Agent 生成的 `ApprovalConsumptionReceiptV1` 并逐字段验证；`action=reject` 只接受不可变 Decision Receipt且禁止 Consumption Receipt | Decision identity + action；approve 另校验 consumption key/digest；计划版本/digest Guard | 按 Decision/Consumption/计划幂等键查询状态 |
| `BIZ-AIGC-015` | `QuoteGeneration` | Agent | 对固定目标、Provider 能力和输出规格给出价格/用量快照；不扣费 | `scope_digest`；短期 Quote Version | 原请求可重试 |
| `BIZ-AIGC-016` | `PrepareGeneration` | Agent | 校验正式 Approval，立即扣费，创建 Asset 占位和 Generation Binding Token | `operation_id + scope_digest`；Approval/Quote/资源版本 Guard | `GetGenerationPreparation` |
| `BIZ-AIGC-017` | `GetGenerationPreparation` | Agent/Worker | 查询是否已扣费、占位 Asset、Binding Token 和允许的执行参数 | Operation ID | 本方法即权威查询 |
| `BIZ-AIGC-018` | `FinalizeGeneration` | Worker | 用 Binding Token 将已上传对象绑定到 Asset；提交收入确认资格 | `job_id + attempt_fence + output_digest`；Token/Asset Version Guard | `GetGenerationFinalizationReceipt` |
| `BIZ-AIGC-019` | `GetGenerationFinalizationReceipt` | Worker/Agent | 查询终态绑定回执，解决超时和响应丢失 | Job ID + Attempt Fence | 本方法即权威查询 |

`BIZ-AIGC-008` 的链接文件与 70 条 Agent consumer-side Corpus 只固定 test-only command/query/authority canonical、approve/reject 联合类型和 unknown-outcome 约束；它仍是 Draft，不是公共 Thrift/DTO、认证 envelope、Business PostgreSQL 事务或 producer/consumer parity 证据，也不通过本目录的实现门禁。

共同约束：

- `Prepare*` 成功后发生超时，不得再次扣费；调用方先查 Receipt。
- `DecideCreationSpecCandidate/DecideStoryboardCandidate/DecidePromptResults/DecideAssemblyPlanCandidate` 必须显式携带 `action=approve/reject`，Business 不从是否存在 Consumption Receipt 猜动作。
- 所有 `Decide*(action=approve)` 必须同时接收不可变 Approval Decision Receipt 和已认证 Agent 生成的 `ApprovalConsumptionReceiptV1`，并逐字段比对 user/project、Approval/action、资源/目标 exact-set、版本/content digest、Tool Pin/Intent 以及适用的 Quote/金额范围；缺失、过期、异义或字段不符均失败关闭。
- 所有 `Decide*(action=reject)` 只验证不可变 reject Decision Receipt、资源/目标和版本/digest，不创建、不要求也不接受 ApprovalConsumptionReceipt；携带消费回执必须作为契约错误拒绝，禁止用 reject 消耗 Approval。
- 上述 approve/reject 约束与 `Prepare*` 的消费校验并列适用，不能以“未扣费”或“只是候选状态变化”为由跳过正式消费/决策边界。
- `Finalize*` 不因 Provider 失败自动退款；失败收入资格按 Business 规则记录。
- 仅当存在权威证据证明外部执行从未开始，才允许走人工或显式冲正流程；冲正不是普通重试分支。
- Business 响应只返回 Worker/Agent 完成当前职责所需字段，Provider 密钥只在运行时安全配置中解析。
- 所有写方法返回权威资源版本、内容摘要、事务回执 ID 和可审计时间。

## 5. Agent HTTP、SSE 与 Approval 契约

### 5.1 HTTP/SSE 能力

| 编号 | 能力 | 约束 |
|---|---|---|
| `AGT-HTTP-001` | Tool Catalog | W1-B2 静态不可用投影已由 `tool-definition-catalog-v1.md` 冻结：Session 级 GET、`agent.session.tools.read`、六个稳定 key/名称/顺序及 `DESIGN_REVIEW_PENDING`；Definition Catalog 与 Executable Registry 分离，Graph 审核前禁止返回执行 Schema、版本或入口 |
| `AGT-HTTP-002` | Session Input/Turn | 每个用户输入先持久化 Input/Turn，再启动 Run；重复请求返回原 Turn |
| `AGT-HTTP-003` | Run/Operation 查询 | 返回权威 Run/Operation/Batch 摘要，不从 SSE 缓存推断终态 |
| `AGT-HTTP-004` | Approval Decision | 只接受已认证用户对指定 Approval 的结构化 `action/approval_version/decision_id`；`decision_id` 是稳定幂等身份，CAS `approval_version` |
| `AGT-SSE-001` | A2UI Event Stream | 按 EventLog Cursor 重放；相同 Event ID 客户端幂等应用 |

### 5.2 Approval V1

Approval 是 Agent PostgreSQL 权威聚合，至少绑定：

- `approval_id/user_id/project_id/action_type`；
- `resource_type/resource_id/resource_version/content_digest`；
- 精确目标集合摘要、Quote/Charge 摘要和最大金额范围；
- `pending/approved/rejected/expired/cancelled` 决策状态及版本；`approval.requested` 只能作为事件名，不能作为持久化状态；
- 创建、到期、决策和失效时间；
- 来源 Card、原 Run/Turn/ToolReceipt/ToolCall；
- 一次性/可复用策略及最大允许消费次数。

`decision_id/decision_actor/action/card_revision/decision_digest` 属于不可变 Approval Decision Receipt；Expiry/Cancel 的 owner、来源和幂等键属于对应 Invalidation Receipt。Approval 决策聚合只保存当前状态、版本及 Receipt 引用，不复制这些幂等记录，更不保存消费时间或消费键。

以下内容不是 Approval：自然语言中的“确认”、模型 Tool 参数、前端隐藏字段、过期 Card Action、只存在于 Redis 的标记。

一个已批准的 Approval 只能覆盖其冻结摘要和金额范围。资源、目标、价格或计划发生变化时必须创建新 Approval。进入实际副作用前必须以 Approval 冻结策略为谓词 first-write-wins 写 Consumption Receipt；写入或重放校验失败时不得继续扣费或派发，也不得通过修改 Approval 行伪造消费成功。

Agent 准备使用一个 `approved` Approval 时先生成不可变 `ApprovalConsumptionReceiptV1`，至少包含 Approval ID/version、消费键、消费语义摘要、用户/项目、动作、资源和目标摘要、Quote/金额摘要、消费时间及 Agent 审计签名。Consumption Receipt 的稳定键为 `(approval_id, consumption_key)`，采用 first-write-wins：同键且同 `consumption_digest` 重放原回执，同键但不同 digest 返回 `APPROVAL_CONSUMPTION_CONFLICT`，不得覆盖。一次性策略还必须对 `approval_id` 施加“最多一条 Consumption Receipt”的数据库唯一约束，即使调用方换用另一个 `consumption_key` 也不能再次消费；可复用策略按 Approval 冻结的最大次数和作用域逐条 CAS 写 Receipt。消费时间、消费幂等键和消费计数均属于 Receipt/其聚合查询，不写回 Approval 决策聚合；消费事实不得把 Approval 的历史决策状态从 `approved` 改写成 `consumed`。Business 仅在已认证的 Agent RPC 调用中接受该回执，并逐字段比对 `Prepare*` 以及全部 `Decide*(action=approve)` 请求；来自前端、模型或未认证调用方的同名字段一律无效。`Decide*(action=reject)` 只验证不可变 reject Decision Receipt，并必须拒绝任何 Consumption Receipt。消费回执响应丢失时查询 Agent Approval 与 Consumption Receipt 权威记录，不再次消费；Receipt 已存在但下游结果未知时按同一业务幂等键恢复，不能创建第二条消费或第二个业务意图。

Approval 的状态迁移 owner 和幂等身份必须分离：用户 `approve/reject` 由已认证 Decision Handler 以 `(approval_id, decision_id)` 处理；`expired` 由 Agent Approval Expiry Scanner 以 `(approval_id, approval_version, expires_at)` 处理；`cancelled` 由 Agent Invalidation Processor 以 `(approval_id, source_event_id)` 处理。每种迁移都必须在单个 Agent PostgreSQL 事务中完成状态 CAS、不可变 Decision/Invalidation Receipt、稳定 SourceID 的 `ApprovalContinuationResult` Input 及 EventLog/Outbox；响应未知时按对应稳定键查询，不重复迁移。

## 6. Agent → Worker 持久化消费契约 `AGT-JOB-V1`

### 6.1 选择结论

Worker 不直接更新 Agent 业务表，也不依赖 Agent Go `internal` 包。Agent Migration 在 Agent PostgreSQL 中发布版本化只读视图与受控数据库函数，Worker 使用最小权限数据库角色调用：

- `agent_job_contract_v1_list_claimable(...)`：读取可 Claim Job ID；
- `agent_job_contract_v1_claim(job_id, worker_id, lease_until, request_id)`：CAS Claim 并返回冻结 Job Envelope；
- `agent_job_contract_v1_renew(job_id, attempt_id, fence, lease_until)`：续租；
- `agent_job_contract_v1_mark_started(job_id, attempt_id, fence, provider_request_digest)`：记录外部执行开始边界；
- `agent_job_contract_v1_schedule_retry(job_id, attempt_id, fence, retry_at, error_code)`：按策略安排重试；
- `agent_job_contract_v1_commit_terminal(job_id, attempt_id, fence, terminal_status, finalization_receipt)`：提交终态并在同一 Agent DB 事务写 Terminal Outbox；
- `agent_job_contract_v1_get(job_id)`：解决 Claim/终态响应丢失。

函数名可在 IDL/SQL 评审时调整，但“Agent Migration Owner + Worker 最小 EXECUTE/SELECT 权限 + CAS/Fence + 同事务 Terminal Outbox”不可改变。

### 6.2 Job Envelope 最小字段

| 字段组 | 内容 |
|---|---|
| 身份 | `schema_version/job_id/batch_id/operation_id/project_id/user_id` |
| 类型 | `job_type`：媒体生成、素材处理或真实渲染；不得让 Worker 从 Prompt 猜类型 |
| 输入 | Prompt/Asset/Plan 的权威引用、资源版本、内容摘要；大对象使用对象存储引用 |
| 执行 | Provider Capability、模型/规格约束、Binding Token、Deadline；不含密钥 |
| 幂等 | `provider_idempotency_key/upload_idempotency_key/finalize_idempotency_key` |
| 租约 | `attempt_id/fence/lease_until/max_attempt_policy_ref` |
| 取消 | `cancel_requested_at/cancel_version`；Worker 必须在 Provider 前和上传前复核 |
| 审计 | `created_at/dispatched_at/definition_version` |

### 6.3 Redis 唤醒

Agent Dispatch Outbox 可发布 Redis 唤醒 Envelope：

```text
schema_version, event_id, job_id, available_at
```

Redis 消息不得携带完整 Prompt、授权信息、价格或业务终态。消息丢失时 Worker 必须能通过 Agent PostgreSQL Claimable 视图扫描恢复；重复消息只触发同一个 CAS Claim。

## 7. Worker 执行与终态提交

Worker 在自己的 PostgreSQL 中持久化 `WorkerAttempt`、`ProviderReceipt`、`UploadReceipt`。推荐顺序：

1. 通过 `AGT-JOB-V1` Claim，取得 Attempt ID 和 Fence；
2. 在 Worker DB 创建/恢复 Attempt；
3. 调 Provider 前将稳定请求摘要和 Idempotency Key 落库，再调用 `mark_started`；
4. Provider 响应未知时先按 Provider 请求 ID/幂等键查询，禁止盲目重发；
5. 上传对象前校验取消和 Fence，上传后落 Upload Receipt；
6. 调 `FinalizeGeneration`，超时先查 Finalization Receipt；
7. 用当前 Fence 调 `commit_terminal`；Agent DB 在同一事务更新 Job/Batch Barrier 并写 Terminal Outbox；
8. `commit_terminal` 响应未知时用 `get(job_id)` 查询，不生成新的 Attempt。

Worker 不直接生成 A2UI。Terminal Outbox 经 Agent Inbox/Continuation Processor 写 A2UI EventLog，并产生 `BatchContinuationResult`。

## 8. Event 契约目录

事件采用 Producer PostgreSQL Outbox → Redis Stream 通知 → Consumer PostgreSQL Inbox 的 at-least-once 模式。Redis 不是权威来源；Producer Dispatcher 扫描 Outbox 重发，Consumer 以 `event_id` 去重。

| 编号 | 事件 | Producer | Consumer | 用途 |
|---|---|---|---|---|
| `EVT-BIZ-001` | `creation_spec.activated.v1` | Business | Agent | 使依赖旧 Spec 的 Continuation/卡片失效并刷新上下文 |
| `EVT-BIZ-002` | `storyboard.revision.activated.v1` | Business | Agent | 更新会话资源、Prompt/Generation 依赖版本 |
| `EVT-BIZ-003` | `asset.updated.v1` | Business | Agent | 刷新资源和就绪度；禁止从事件重建 Asset 全量真相 |
| `EVT-BIZ-004` | `points.ledger.changed.v1` | Business | Agent | 刷新余额展示；余额权威仍通过 Business 查询 |
| `EVT-AGT-001` | `generation.batch.completed.v1` | Agent Job Contract | Agent Continuation | 触发 Batch 完成回合和 A2UI 刷新 |
| `EVT-AGT-002` | `generation.batch.partial_failed.v1` | Agent Job Contract | Agent Continuation | 展示部分成功并允许固定失败目标重试 |
| `EVT-AGT-003` | `generation.batch.failed.v1` | Agent Job Contract | Agent Continuation | 展示失败；不自动退款 |
| `EVT-AGT-004` | `generation.batch.cancelled.v1` | Agent Job Contract | Agent Continuation | 展示取消及已发生费用事实 |

事件 Payload 只包含身份、版本、终态、摘要和权威资源引用；消费者需要详情时通过相应 RPC/Repository 查询。

## 9. 幂等、Fence 与 Unknown Outcome 矩阵

| 风险点 | 稳定键 | 权威判断 | 恢复动作 |
|---|---|---|---|
| Tool 重放 | `(session_id, turn_id, tool_call_id)`；阶段引用另以 `(tool_receipt_id, ref_slot)` | Agent ToolReceipt 的 `request_semantic_digest/execution_refs/result_digest/result_refs` | 同请求摘要恢复/回放；open 阶段同 slot 同 ref digest 重放、异 digest 冲突；结果仅从 execution refs 冻结投影，后生成引用不得改请求摘要 |
| 同步模型重复扣费 | `tool_call_id + execution_digest` | Business Charge Receipt | 查询 Receipt；成功则复用，不再扣费 |
| 模型响应丢失 | `model_receipt_id + request_digest` | Agent ModelReceipt + Provider 请求标识 | 查询 Provider；无法证明未执行时不得盲重发 |
| 候选重复保存 | `tool_call_id + candidate_digest` | Business 写入回执 | 返回相同 Resource Version |
| Approval 决策/失效重放 | Decision：`(approval_id, decision_id)`；Expiry：`(approval_id, approval_version, expires_at)`；Cancel：`(approval_id, source_event_id)` | Agent Decision/Invalidation Receipt + 稳定 Continuation SourceID | 同键同 digest 回放原状态/Input；异义冲突；单事务不重复迁移 |
| Approval 重复消费 | `(approval_id, consumption_key)`；一次性策略另以 `approval_id` 唯一 | Agent Consumption Receipt + Approval 冻结策略 | 同键同 digest 回放；同键异义冲突；一次性策略拒绝第二条 Receipt |
| Generation 重复扣费 | `operation_id + scope_digest` | Business Preparation Receipt | 查询准备结果，复用 Asset/Token |
| 已扣费未派发 | Operation/Preparation Receipt | Agent Operation + Dispatch Outbox | Recovery Scanner 补建或补投同一批 Job |
| Job 重复 Claim | Job ID + CAS | Agent Job Contract | 仅当前 Attempt/Fence 生效 |
| Provider 重复调用 | Provider Idempotency Key | Worker ProviderReceipt/Provider 查询 | 查询优先；只在证明未开始后重试 |
| 旧 Attempt 回写 | Job ID + Fence | Agent Job Contract | 拒绝并记录 stale result；不得覆盖新 Attempt |
| Finalize 响应丢失 | Job ID + Fence + Output Digest | Business Finalization Receipt | 查询后复用；不得重复绑定 |
| Terminal Event 重复 | Event ID | Agent Inbox | 幂等忽略，返回已处理状态 |
| SSE 重连 | EventLog Cursor/Event ID | Agent EventLog | 从 Cursor 重放，前端幂等应用 |

Agent Runner 边界内的模型、Tool、Business RPC、Approval Consumption、Dispatch 等任一副作用一旦出现 unknown outcome，必须立即把当前 Input 迁移为 `quarantined`、Run 迁移为或保持 `recovery_pending`，持续阻塞 HOL。`retry_wait` 只允许用于已有权威证据证明相关副作用未发送或未提交的瞬时技术失败；自动核对预算只控制查询频率、退避和升级人工处置，不得延迟隔离或把 unknown 变成普通重试。

## 10. 状态和错误码约束

### 10.1 业务状态与执行状态不得混用

- CreationSpec/Storyboard/Prompt/AssemblyPlan 的 `reviewing/active/rejected/superseded` 属于 Business。
- GraphToolResult 的 `completed/accepted/waiting_user/partial/failed/cancelled` 是一次 Tool 的冻结语义输出，不是任何持久化聚合状态。
- ToolReceipt 的写入阶段与其冻结的 `result_status` 分字段保存，不能复用 Approval/Operation 状态。
- Approval 的 `pending/approved/rejected/expired/cancelled` 属于 Agent；消费是不可变 Consumption Receipt，不是第六种决策状态。
- Operation/Batch/Job 的 `pending/running/completed/partial_failed/failed/cancelled` 属于 Agent。
- Turn/Run 的执行状态属于 Agent；`waiting_user` 会结束当前 Turn/Run，不能成为占用 Session Lane 的持久执行状态。
- WorkerAttempt 的 `claimed/calling_provider/uploading/finalizing/terminal` 属于 Worker。
- Graph Node 成功/失败只是一次调用控制流，不得直接替代以上状态。

### 10.2 跨 Module 错误码

错误必须分为：

- `INVALID_ARGUMENT`：Schema 或业务输入不合法，不重试；
- `PERMISSION_DENIED`：权限或资源范围不匹配，不重试；
- `VERSION_CONFLICT`：资源版本/digest/fence 冲突，重新读取后由用户或新 Turn 决策；
- `APPROVAL_REQUIRED/APPROVAL_INVALID`：创建或刷新正式 Approval；
- `INSUFFICIENT_POINTS`：不得调用 Provider；
- `DEPENDENCY_NOT_READY`：返回固定缺失依赖，允许用户补齐；
- `RATE_LIMITED/DEPENDENCY_UNAVAILABLE`：仅按配置策略有限重试；
- `UNKNOWN_OUTCOME`：必须走对应查询契约，禁止直接重放副作用；
- `STALE_ATTEMPT`：旧 Fence 结果隔离为 superseded/orphaned；
- `INTERNAL`：记录内部错误 ID，外部不泄漏栈和敏感信息。

## 11. 安全、日志与隐私

- Tool Intent、Prompt、A2UI Action 都不能授予权限、预算或 Approval。
- Provider/API 密钥来自 Runtime Secret，不进入 DB、Event、日志、Prompt 或 A2UI。
- 日志记录 ID、版本、摘要、耗时、状态和稳定错误码；原始用户内容按数据分类脱敏或只记录 digest。
- 素材和产物访问使用短期签名或服务端代理；不得把永久对象存储凭证传给 Agent Model。
- Worker 角色只获得 Agent Contract 函数的 `EXECUTE`、必要视图的 `SELECT` 和 Worker 自有库权限。
- 所有扣费、Approval、派发、取消、终态与冲正必须有不可变审计记录。

## 12. 实现顺序与兼容性

1. 三方评审本文，冻结 `v1` 字段、状态、错误码和 Owner；
2. 评审 `runner-session-lane-review-v1.md`，冻结 Session Lane、Input/Turn/Run、Receipt、Approval Continuation、Checkpoint、EventLog 和预算/取消规则；
3. 通过独立依赖 PR 引入并验证 Eino，再以向前 Migration 实现 Agent 运行基础；
4. Business 先实现读契约、候选写入、计费与 Generation Preparation/Finalization；
5. Agent 实现 Approval、Operation/Batch/Job、Outbox/Inbox、Job Contract V1；
6. Worker 实现 V1 Claim/Fence/Receipt/Finalize，旧消费者不得直写新表；
7. 六个 Graph Tool 按依赖顺序接入；
8. 前端接版本化 Tool Catalog、Approval 和 A2UI EventLog/SSE；
9. 运行单 Module 测试、三 Module 契约测试、unknown-outcome 故障注入和 35 条 SMK-P0 全功能冒烟。

兼容规则：

- 新增可选字段可保持同一主版本；删除字段、改变状态语义、幂等键或 Owner 必须升主版本；
- Consumer 必须拒绝未知主版本，忽略未知可选字段；
- 事件发布至少覆盖一个消费者迁移窗口；旧版本下线前必须有消费指标和积压清零证据；
- 不允许用“前端暂时不用”规避服务端契约兼容性。

## 13. 评审清单与当前结论

- [ ] Business 确认创作领域对象、计费、资产和 Binding 的 Migration Owner；
- [ ] Agent 确认 Approval、Operation/Batch/Job、EventLog 和 Continuation Owner；
- [ ] Agent Runner/Session Lane 评审冻结 GraphToolResult、Receipt、HOL、Lease/Fence、Checkpoint、A2UI、预算和取消语义；
- [ ] Agent/Business 契约测试确认四个 `Decide*`：`DecideCreationSpecCandidate` 已有 70 条 Agent consumer-side test-only 候选向量，但仍缺 Business producer parity 与 Owner Approved；其余三类也必须证明 approve 同时逐字段验证 Decision/Consumption Receipt，reject 只验证 Decision Receipt并拒绝 Consumption Receipt；
- [ ] ToolReceipt 契约测试确认 open execution ref slot 同义重放/异义冲突，结果字段只在 `open -> frozen` 从 execution refs 一次投影；
- [ ] Eino 独立依赖 PR 和兼容验证方案已评审；所有数据库变化均规划为向前 Migration；
- [ ] Worker 确认持久化消费契约、最小权限、Attempt/Receipt 边界；
- [ ] 三方冻结所有状态、错误码、幂等键、Fence 和 unknown-outcome 查询；
- [ ] 安全评审确认素材访问、Secret、日志脱敏与数据保留；
- [ ] 产品/财务确认扣费、收入确认、失败不退款和显式冲正规则；
- [ ] 运维确认 PostgreSQL、Redis Stream、etcd、扫描恢复与告警指标；
- [ ] 契约测试和故障注入用例已评审。

当前结论：**待评审，不通过实现门禁。** 本目录及 `runner-session-lane-review-v1.md` 均保持 Draft。六个 Graph Tool 设计可以基于草案继续细化，但生产代码、Migration、IDL 和跨 Module Consumer 必须等待对应契约冻结为 `v1`。
