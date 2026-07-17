# `analyze_materials` Tool-enabled Runtime 最小 Profile 设计

> 文档状态：Approved for Development Preview / 不授权生产实现
>
> Profile：`analyze_materials.runtime.v2preview1`
>
> 设计与产品行为批准日期：2026-07-17
>
> Owner：Agent Runtime；Business 只提供已授权 text/image Evidence 只读快照
>
> 批准依据：用户要求继续按设计推进 Tool 开发与多 Agent 分工。本批准只覆盖本文 exact-set，不改变 Immutable Turn Context 全局门禁。

关联文档：[唯一开发计划](../../requirements/full-function-smoke-development-plan.md)、[`analyze_materials` Graph Tool 设计](graphtool/analyze_materials-design.md)、[`user_message.runtime.v2preview1` 方案 A](user-message-runtime-v2-design.md)、[Runner/Session Lane 评审](runner-session-lane-review-v1.md)、[Evidence Preview 跨 Module 契约](../cross-module/material-analysis-evidence-preview-v1.md)。

## 1. 本批目标与结论

本 Profile 只把已经实现、默认不注册的 `analyze_materials.v2preview1` Tool Core 接入一个本地可恢复执行面：

1. 用户在 Project Workspace 显式提交严格 `analyze_materials.preview.intent.v1`，不会从 QuickCreate 自由文本推断或改写意图；
2. HTTP 只做身份校验、幂等校验和 PostgreSQL 入队，模型、Graph 与 Business RPC 全部由后台 Processor 执行；
3. 每个 Turn 的 Executable Tool Registry 恰好为 `{analyze_materials}`，Tool Definition、Intent/Result Schema、Prompt、Validator、Evidence Policy、Model Route、Budget 与 Access Scope 全部在入队事务冻结；
4. 唯一主 `ChatModelAgent` 由真实 Eino ADK Runner 驱动，本地确定性 Router 只生成一个稳定 ToolCall，`analyze_materials` 配置 `ReturnDirectly`，不会追加第二次模型路由；
5. Router Model、Graph 内部 Model 和 Tool Result 分层 first-write-wins，重启、投影失败、响应丢失和 Lease Takeover 均重放原冻结事实；
6. Tool 只读 Business Evidence；结果是非权威 Development Preview Card，不创建 MaterialAnalysis Revision、CreationSpec、Approval、账单、Operation、Batch、Job 或 Worker 任务；
7. Profile 只允许本地专用数据库与 loopback 服务，和 `user_message.runtime.v2preview1`、`plan_creation_spec.v1preview1` 互斥启用。

该批准不让 `analyze_materials` 进入静态生产 Catalog。`tool-definition-catalog-v1.md` 中的 `DESIGN_REVIEW_PENDING` 保持不变；这里批准的是独立 Development Preview Executable Registry，不是生产 `v1alpha1` Definition。

## 2. 当前事实与隔离要求

### 2.1 已完成事实

- `agent/internal/graphtool/analyzematerials` 已有严格 Intent/Result、11 Node、2 Branch、无环 `AllPredecessor` Graph、独立 Candidate Validator 和 Eino InvokableTool 包装器。
- Business 已有 text/image Evidence 的 Owner、Migration、单 SQL Repository、Foundation RPC；Agent Adapter 一次无重试读取 exact-set 快照。
- 方案 A 已实现 PostgreSQL HOL、Lease/Fence、稳定 Turn/Run、Model/Output Receipt、Snapshot/SSE 与硬刷新恢复，但它的 Executable Tool Registry 被批准并实现为永久空集。
- CreationSpec Preview 有独立结构化输入和专用回执表，可作为工程形态参考；其 Business Draft 写入、命令恢复和表不得被本 Profile 复用。

### 2.2 必须隔离

- 不修改、重算或 supersede 已有 `user_message` 的 Source、Message、Input、Turn Context 或结果；
- 不把 `analyze_materials_preview` 伪装成 `user_message`、`creation_spec_preview`、Assistant Message 或系统 continuation；
- 不复用 `session_user_message_*`、`creation_spec_preview_*` 表或 Receipt；
- 不并行启动三个按 Source 过滤的 Processor。启用本 Profile 时其他两个 Runtime 必须关闭；
- 不使用生产 `agent_tool_receipt`、58 字段 Immutable Turn Context 或完整 Model unknown 状态名冒充已关闭门禁。

## 3. 用户意图与入口

### 3.1 唯一入口

```text
POST /api/v1/agent/sessions/:session_id/analyze-materials-previews
  -> Business BFF 重验 Project/Session Owner 与 CSRF
  -> POST /internal/v1/workspaces/sessions/:session_id/analyze-materials-previews
  -> Agent 验证内部身份 scope `analyze_materials.preview.write` + Idempotency-Key
  -> 单事务写 typed Intent 密文、Input、Turn Context、Run identity、open Receipt、accepted Event
  -> 202 pending
```

202 响应固定为 `analyze_materials.preview.enqueue.v1`，exact-set 为 `request_id/session_id/input_id/turn_id/run_id/tool_call_id/status=pending/replayed`；它只证明 typed Input 与全部稳定身份已经提交，不声明模型、Tool 或投影已完成。

请求正文 exact-set 复用 Tool Intent：

```json
{
  "schema_version": "analyze_materials.preview.intent.v1",
  "asset_ids": ["<uuidv7>"],
  "analysis_goal": "识别素材主题和可复用元素",
  "focus_dimensions": ["content", "visual"],
  "output_language": "zh-CN",
  "expected_assets": [{"asset_id": "<uuidv7>", "asset_version": 1}]
}
```

Tool Core 的通用 Schema 允许省略 `expected_assets`，但本 Runtime Profile 把它收紧为必填，并要求与 `asset_ids` exact-set，从入队时就冻结每个 Asset 版本。BFF 和 Agent 都严格拒绝重复字段、未知字段、尾随 JSON、非规范 UUIDv7、重复集合和超限正文。

### 3.2 Source 与身份

- `session_input.source_type = analyze_materials_preview`；
- `source_id = idempotency_key`；
- `session_input.message_id = NULL`；严格 Intent 密文、Key Version 和 digest 存在本 Profile 专用 append-only Turn Context，不创建或伪造 `session_message`；
- `input_id/turn_id/run_id/tool_call_id/router_model_call_id/graph_model_call_id/terminal_event_id` 首次入队一次生成，技术重试不换 ID；
- `request_id` 只关联当前 HTTP 调用；同义幂等重放返回原 `input_id`，异义重放为 `409 IDEMPOTENCY_CONFLICT`。

本批不支持自然语言 Chat POST，也不允许“选中 Tool 后自动使用旧 QuickCreate Prompt”。用户若希望新结构化意图替代旧输入，必须另行设计 consent/supersession ledger；当前只能创建新的显式 Input，并受 Session HOL 约束。

## 4. Router 与 Executable Tool Registry

### 4.1 Registry exact-set

```text
profile = analyze_materials.runtime.v2preview1
executable_tools = [analyze_materials]
tool_definition = analyze_materials.v2preview1
intent_schema = analyze_materials.preview.intent.v1
result_schema = analyze_materials.preview.result.v1
```

以下任一情况启动失败关闭：Tool 缺失、重复、额外 Tool、Info name/schema 不一致、Catalog 被误当授权、配置按环境静默改写 Registry。

### 4.2 Router exact-set

本地 Fake Router 只允许：

```text
typed intent -> exactly one analyze_materials ToolCall -> ReturnDirectly Tool Result
```

- Router 逐字复制已认证解密并重验摘要的 Intent JSON 到 Tool arguments；身份与 pins 只从可信 Turn Context 注入；
- ToolCall ID 必须等于入队冻结值；未知 Tool、零 ToolCall、多个 ToolCall、Assistant 自由文本或参数变化全部为契约错误；
- `ReturnDirectly={analyze_materials:true}`，因此 Router 只有一次 Model 调用；
- Graph 内部素材分析 Model 是第二个独立逻辑 Model 调用，不经过外层 Router Receipt；
- 两个本地 Fake Model 都不代表真实 Provider，不实现 `dispatched/model_unknown`。

## 5. 最小不可变 Turn Context

`analyze_materials.turn_context.v2preview1` 只冻结本批所需 exact-set：

```text
profile / context_schema_version
session_id / input_id / turn_id / run_id / tool_call_id
user_id / project_id / intent_ciphertext / intent_key_version / intent_digest
access_scope_ref / access_scope_digest
tool_registry_ref / tool_registry_digest
tool_definition_ref / tool_definition_digest
intent_schema_ref / result_schema_ref
prompt_ref / prompt_digest
validator_ref / validator_digest
evidence_policy_ref / evidence_policy_digest
router_model_route_ref / router_model_route_digest
analysis_model_route_ref / analysis_model_route_digest
runtime_policy_ref / runtime_policy_digest
budget_ref / budget_digest
context_digest
```

Context 与 Input/Run identity/open Tool Receipt 同事务创建并 append-only。入队事务另行预分配 Accepted Event ID；Run 只冻结一个 `terminal_event_id`，语义 completed/partial/failed 与 `runtime_failed` 互斥复用该稳定 ID，不创建第二个 Runtime Failure Event ID。Claim、retry、takeover 和 restart 只能认证解密原 Intent 并重算这些 pins，不能读取“当前最新”配置。

## 6. Receipt 与状态机

### 6.1 Input / Run

```text
Input: pending -> claimed -> running
       running -> resolved | dead | retry_wait
       retry_wait -> claimed

Run:   created -> running -> completed | failed
```

已冻结 Tool Result 的投影失败不进入 `dead`，而是释放当前 Lease 后按固定延迟回到可 Claim 状态，只重放投影。没有 external unknown outcome，本 Profile 不声明 `recovery_pending` 能力。

### 6.2 Model Receipt

Router 与 Graph Model 各有独立稳定 ModelCallID：

```text
reserved -> completed | failed
```

Model Receipt 请求摘要覆盖 `call_kind + model_call_id + context_digest + route_ref/digest + canonical messages`。首次合法 owner/fence 执行本地模型并冻结响应或稳定失败码；同 fence 重放不执行，更高 fence 只有在 `reserved` 且本地进程结果已明确丢失时才允许重做。真实 Provider 接入必须升级 Profile 与状态机。

### 6.3 Tool Receipt

```text
open -> completed | partial | failed
```

主键为稳定 `tool_call_id`。request digest 覆盖 `context_digest + tool definition/schema pins + canonical intent digest`。结果冻结前必须通过 `analyzematerials.ValidateResultForContext`；冻结内容为完整严格 Result 密文、result digest、status、result code 与 execution fence。

Tool Wrapper 的合法顺序：

1. 读取或创建同键 open Receipt 并校验 request digest；
2. 已冻结时认证解密、重验 digest/Schema/ToolCallID 后原样返回；
3. open 时执行 Tool Core；
4. first-write-wins 冻结完整 Result；
5. 重读并返回数据库首写结果。

Tool Core 返回的确定性 `failed` 仍是合法 Tool Result，Input 以 `resolved` 终结；只有密文损坏、Context/Receipt 冲突、Eino 契约破坏或执行重试耗尽才形成独立 Runtime Failure，Input 为 `dead`。Runtime Failure 不伪造素材分析结果。

## 7. PostgreSQL 对象与统一 HOL

前向 Migration 新增：

- `analyze_materials_preview_run`
- `analyze_materials_preview_turn_context`
- `analyze_materials_preview_model_receipt`
- `analyze_materials_preview_tool_receipt`
- `analyze_materials_preview_projection`

同时只向 `session_input.source_type` 和必要 Event aggregate/event exact-set 增加 `analyze_materials_preview`。所有跨对象引用使用逻辑 UUID，不创建物理 FK；表、列、CHECK、索引和中文 COMMENT 必须完整；Context、冻结 Receipt 和完成 Projection 需数据库级拒绝 UPDATE/DELETE。

Claim 必须先计算每个 Session 的全 Source 最小非终态 Input，再判断 head 是否属于本 Profile。禁止先筛 `analyze_materials_preview` 后越过更早 Input。由于当前尚未有一个可分派三种 Source 的公共统一 Processor，本 Profile 只允许与另外两个 Runtime 互斥启用；这不是生产统一 Lane 已完成的声明。

## 8. Projection、事件与前端

Snapshot nullable 字段固定为 `analyze_materials_preview`；事件 exact-set：

```text
analyze_materials.preview.accepted
analyze_materials.preview.completed
analyze_materials.preview.partial
analyze_materials.preview.failed
analyze_materials.preview.runtime_failed
```

`analyze_materials.preview.accepted` 使用不含 `message_id` 的独立 typed payload；不得复用当前要求用户 Message 的 `session.input.accepted` payload。

Card exact-set：

```text
schema_version = analyze_materials.preview.card.v1
input_id / turn_id / run_id / tool_call_id
status = completed | partial | failed
result_code
analysis / coverage / evidence_refs（仅 Tool completed/partial）
failure_kind = tool | runtime（仅 failed）
summary / retryable（仅 Tool failed 或 Runtime Failure）
```

Projection/Event/Run/Input 终态与当前 owner/fence 的 Lease release 在一个 Agent PostgreSQL 事务提交。Snapshot 与 SSE 共用严格 parser/reducer；未知 schema/status/字段失败关闭。Evidence 正文、用户原始 Prompt、模型原文、密文、内部错误、Provider metadata 和 Secret 不进入 Event 或浏览器。

本 Profile 的安全 Card 上限为 64 KiB；启用时 `AGENT_SSE_MAX_EVENT_BYTES` 必须至少为 131072，为 `workspace.event.v1` Envelope 的身份、聚合、时间与编码字段保留确定性余量，禁止持久化一个无法通过 SSE 重放的合法 Card。

首批前端入口允许用户从当前 Project 的已支持 text/image Preview Asset 中选择 1～8 个目标，填写目标、维度和语言后显式提交。若当前页面尚无权威 Preview Asset 列表，入口保持隐藏，canonical Smoke 可使用本地专用 seed；禁止用自由文本 Asset ID 输入冒充正式产品体验。

## 9. 配置与启用门禁

```text
DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE=analyze_materials.runtime.v2preview1
DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=false
```

启用必须同时满足：

- `DORA_ENV=local`；
- Business/Agent HTTP 和 RPC 监听为 loopback；
- PostgreSQL DSN 指向允许的本地专用数据库，禁止共享/生产库；
- Profile exact match、Migration generation match；
- `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false`；
- `DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false`；
- Business/Agent 双端门禁一致；
- Static Catalog 继续显示不可用，不以 Catalog 可见性替代本 Profile 授权。

Redis Wake 仍是可选延迟优化；PostgreSQL Scanner 是正确性路径。宿主机联调直接连接 Docker 暴露的 `127.0.0.1:15432/16379/12379`，不读取 Docker Socket，也不以 Compose readiness 代替端口和协议探针。

## 10. 明确禁止项

- 自动把 QuickCreate/user_message 路由为素材分析；
- 修改旧 Input provenance 或原地升级方案 A Context；
- 注册任何第二个 Tool、动态 Tool Search、Provider/CRUD Tool；
- Assistant/Tool Message History、Summary、Memory 或多轮 Chat；
- MaterialAnalysis/CreationSpec/Storyboard Business 写入；
- Approval、Billing、Operation、Batch、Job、Worker、媒体 Provider；
- PDF/音频/视频二进制分析或图片原图模型调用；当前只读已持久化 text/image Evidence；
- 真实 DeepSeek/外部 Provider、模型 Failover、unknown outcome 自动重发；
- Graph Checkpoint、Interrupt/Resume、子 Agent、DeepAgent、AgentAsTool、长驻 Graph；
- 共享/生产环境启用，或把本地 canonical Evidence 当 P1/生产 Evidence。

## 11. 批次与验收

| 批次 | 状态 | 交付 |
|---|---|---|
| `M0` | **已批准** | 本文、Profile/入口/Registry/Receipt/恢复/禁用项 exact-set |
| `M1` | **已完成** | Migration、Repository、显式 Enqueue、HOL/Fence/Receipt 合同与 required PostgreSQL 测试 |
| `M2` | **已完成（仅本地 Fake）** | 单 Tool ChatModelAgent、ReturnDirectly Router、Router/Graph Model Receipt、Tool Wrapper |
| `M3` | **已完成（安全投影）** | Business BFF、Snapshot/SSE/只读 Card；因无权威 Asset 列表，页面提交入口保持隐藏 |
| `M4` | **已完成（canonical Trial passed）** | 宿主机 Business/Agent/Vite 直连现有 `15432/16379/12379` + Chromium；不访问 Docker Socket/Compose |

最低验收：

1. 同 Idempotency-Key 并发/重放只有一组 typed Intent/Input/Context/Run/Receipt/Event；异义为 409，且不创建 `session_message`；
2. Registry 恰好一个 `analyze_materials`，Router 恰好一个稳定 ToolCall，ReturnDirectly 后无第二次 Router Model；
3. Graph 内部 Model 恰好一次；Router/Graph Model 与 Tool Result 都可冻结重放；
4. completed、partial、Tool failed、Runtime dead 四类投影严格区分；
5. Evidence 无权限/不存在统一为安全失败，RPC 原文不泄漏；
6. Session HOL、双实例 Fence Takeover、旧 Fence 零增量、Scanner 丢 Wake 恢复通过真实 PostgreSQL；
7. Tool Result 冻结后崩溃只补 Projection/Event，不重调 Router、Graph Model 或 Business RPC；
8. Business Draft、Approval、Billing、Operation/Batch/Job/Worker 表增量为零；
9. 方案 A 和 CreationSpec Preview 全量回归通过；
10. canonical Smoke 只保存安全 ID、摘要、计数和布尔断言，Evidence 权限 `0600`，并证明源码运行期间未变化与端口清理。

M4 最终源码 Trial 为 `20260716T215049Z-39824`：专用 `dora_business_test/dora_agent_test` 完成 Migration、typed enqueue、同义重放、异义 `409`、单 Tool 执行、两层 Model Receipt、Tool Receipt、Snapshot/SSE、只读 Card、硬刷新和静态 Catalog `unavailable`；22/22 Evidence 断言为真，文件权限 `0600`，source digest 为 `sha256:1f853003f9b21c8514a8178aa1e65986cecb5973f1ee07c840702388167a96a3`，Evidence SHA-256 为 `sha256:d7e5c0e4a475f2c918195e32114b7f53df83504eedb81982d6a8380571dcdff0`。该命令只探测宿主机端口并通过 Go Client 访问中间件，不检查或访问 Docker Socket、Compose、`psql`、`redis-cli`。

当前结论：**`M0`～`M4` 的 Development Preview 已完成，但不授权生产。`analyze_materials` 静态生产 Catalog 仍默认不注册且保持 `unavailable/DESIGN_REVIEW_PENDING`；任何批次完成都不得把 Immutable Turn Context 全局 `implementation_unlocked` 或 `production_authorized` 改为 true。**
