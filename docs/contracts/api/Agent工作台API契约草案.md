# Agent 工作台 API 契约

状态：active
owner：文档与契约责任域；Agent 服务责任域和前端责任域确认
更新时间：2026-06-28
适用范围：前端 -> 智能体微服务，统一 Agent 工作台

## 成熟度复核

当前成熟度：active。
使用方式：作为 Agent 工作台 API 路由方向和错误语义当前事实源；字段级 route、request/response 和 schema 以 `api/openapi/agent-workbench.yaml` 为准，索引见 `docs/contracts/字段级契约索引.md`。

已补齐项：SSE 鉴权、`Last-Event-ID`、event replay 分页、snapshot fallback 和同 session run 并发策略已在本文冻结。

运行证据：`tests/reports/m3-technical-baseline-report.md` 已记录 Agent API token 鉴权、SSE `Last-Event-ID` replay、`GetRun`、`ListMessages`、`ReplayEvents`、`Snapshot` 和 `CancelRun` 权限校验通过；`tests/reports/m6-service-acceptance-report.md` 已记录 HTTP / AG-UI / 服务级验收通过。

## 字段级事实源

- OpenAPI：`api/openapi/agent-workbench.yaml`
- Agent API 实现：`services/agent/internal/api/http/**`
- AG-UI schema：`api/agui/agent-workbench-events.schema.json`
- AG-UI fixture：`tests/agent/agui/fixtures/**`

新增或变更字段时，先更新 OpenAPI、AG-UI schema 或 fixture，再同步本文档的错误语义、补偿规则和成熟度状态。

## API 清单

| Method | Path | 描述 | 鉴权 | 状态 |
| --- | --- | --- | --- | --- |
| POST | `/api/agent/sessions` | 在项目下创建会话 | 登录态 | planned |
| GET | `/api/agent/sessions/:session_id` | 查询会话快照 | 登录态 + 项目权限 | planned |
| GET | `/api/agent/sessions/:session_id/messages` | 分页查询会话消息 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs` | 发送用户输入并创建 run | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id` | 查询 run 状态 | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/stream` | SSE 实时事件 | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/events` | 按 sequence 补偿事件 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs/:run_id/messages` | 追加用户输入 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs/:run_id/interrupts/:interrupt_id/accept` | 确认人工中断 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs/:run_id/interrupts/:interrupt_id/reject` | 拒绝人工中断 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs/:run_id/cancel` | 取消 run | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/snapshot` | 查询 run 快照 | 登录态 + 项目权限 | planned |

## 通用请求头

| 字段 | 位置 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| Authorization | header | string | 是 | 登录 token |
| X-Trace-Id | header | string | 是 | 链路追踪 |
| Last-Event-ID | header | string | SSE 重连可选 | 最近成功消费的 AG-UI `event_id`，仅用于 `/stream` |

## 创建会话

- Method：POST
- Path：`/api/agent/sessions`
- 描述：在业务项目下创建 Agent 会话，Agent 只保存 `project_id` 引用，项目权限由业务 RPC 校验。
- 归档规则：创建前调用 `ProjectService.CheckProjectAccess(access_purpose=continue_creation)`；项目为 `archived` 时返回 `409 PROJECT_ARCHIVED`，不创建 session。

请求：

| 字段 | 位置 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| project_id | body | string | 是 | 业务项目 ID |
| initial_title | body | string | 否 | 会话标题 |

响应：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| session_id | string | 是 | Agent 会话 ID |
| project_id | string | 是 | 业务项目 ID |
| status | string | 是 | `active` |

## 创建 run

创建 run 前调用 `ProjectService.CheckProjectAccess(access_purpose=continue_creation)`。项目为 `archived` 时返回 `409 PROJECT_ARCHIVED`，不创建 run，不启动 SSE，不冻结积分。

请求：

| 字段 | 位置 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| session_id | body | string | 是 | Agent 会话 ID |
| project_id | body | string | 是 | 业务项目 ID |
| user_input | body | object | 是 | 用户输入 DTO，字段级以 OpenAPI `UserInputDTO` 为准 |
| model_selection | body | object | 否 | 用户选择模型 |
| referenced_assets | body | array | 否 | 引用业务资产 DTO |
| control_inputs | body | array | 否 | 前端控件输入 DTO |

响应：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| run_id | string | 是 | Agent run ID |
| session_id | string | 是 | 会话 ID |
| stream_url | string | 是 | SSE URL |
| snapshot_version | string | 是 | 创建 run 时的快照版本 |
| status | string | 是 | `pending` / `running` |

## Run 并发策略

- 同一 `session_id` 同一时间只允许一个 active run。
- active run 包括 `pending`、`running`、`waiting_confirmation`、`waiting_input`、`resuming` 和 `cancelling`。
- 当 session 已有 active run 时，`POST /api/agent/runs` 必须返回 `409 RUN_STATE_CONFLICT`，不得隐式取消旧 run 或创建并行 run。
- `completed`、`failed`、`cancelled` 或 `expired` 后可以创建新 run；新 run 必须继承同一 `project_id` 并重新做项目权限校验。
- `POST /api/agent/runs/:run_id/messages` 只能用于当前 active run 的追加输入或恢复，不创建新 run。
- `cancel`、`accept`、`reject` 不要求客户端携带 `Idempotency-Key`；需要防重的运行时动作由 Agent 应用层按 run、interrupt、action 和确认摘要等业务字段生成内部幂等键。

## SSE 和事件补偿

### 打开 SSE

- API：`GET /api/agent/runs/:run_id/stream`
- 鉴权：使用 Agent API Bearer 登录态，不单独定义长期 stream token。
- 重连：前端可携带 `Last-Event-ID` header，值为最近成功消费的 AG-UI `event_id`。
- 服务端必须校验 run、session、project 权限；权限失效返回 `PERMISSION_DENIED` 或 `PROJECT_ARCHIVED`，不继续推送事件。
- SSE 只推送 AG-UI schema 中定义的事件；未知事件只允许通过兼容策略被前端忽略，不允许作为业务逻辑依赖。

### 事件补偿

- API：`GET /api/agent/runs/:run_id/events?after_sequence={sequence}&page_size={size}`
- 默认分页：`page_size=10`。
- 最大分页：`page_size=100`。
- 返回：`events`、`next_after_sequence`、`snapshot_required`。
- 正常补偿返回 `sequence > after_sequence` 的连续事件；前端按 `event_id` 去重、按 `sequence` 合并。
- 如果 `after_sequence` 超出事件保留窗口、事件缺口不可补、`Last-Event-ID` 无法映射到当前 run，返回 `snapshot_required=true`。

### Snapshot fallback

- API：`GET /api/agent/runs/:run_id/snapshot`
- snapshot 至少包含 session、run、messages、assets、blackboard、tasks、`last_event_sequence` 和只读原因。
- run 处于 `waiting_confirmation` 且存在 required interrupt 时，snapshot 必须包含 `interrupt`：`interrupt_id`、`confirmation_id`、`type`、`status`、`reason`、`title`、`summary`、`risks`、`points`、`actions`、`payload_digest`、脱敏后的 `confirmation_payload`、`expires_at`、`trace_id`；不得返回模型快照、安全证据、预估详情、供应商运行引用、Prompt 或密钥引用。
- 前端收到 `snapshot_required=true` 后必须先查询 snapshot，用 snapshot 覆盖本地工作台状态，再从 `last_event_sequence` 继续补偿或重开 SSE。
- snapshot 只能包含 Agent Runtime 数据和业务引用，不得包含业务事实副本、私有 TOS 签名、系统 Prompt、供应商原始响应或推理链路。

## 项目归档阻断

| 场景 | API 行为 | 前端行为 |
| --- | --- | --- |
| 创建 session 时项目已归档 | `409 PROJECT_ARCHIVED` | 回到只读工作台或项目详情 |
| 创建 run 时项目已归档 | `409 PROJECT_ARCHIVED` | 禁用 Composer、重试和继续生成 |
| confirm / resume / retry 时项目归档 | `409 PROJECT_ARCHIVED` 或 AG-UI `project.archived.blocked` | 保留历史，展示只读 Banner |
| run 已在执行中途项目归档 | 停止新 Tool，释放未结算冻结积分，发送取消事件 | 展示已取消原因和释放结果 |
| GET session / run snapshot | 允许只读查询 | 用于历史恢复和查看 |

响应 detail 建议：

```json
{
  "code": "PROJECT_ARCHIVED",
  "message": "项目已归档，无法继续创作。",
  "project_status": "archived",
  "creative_allowed": false,
  "allowed_actions": ["view"]
}
```

## 错误码

| 错误码 | HTTP 状态码 | 含义 | 前端处理 |
| --- | --- | --- | --- |
| UNAUTHENTICATED | 401 | 未登录 | 弹 LoginModal |
| PERMISSION_DENIED | 403 | 无项目权限 | 展示权限不足 |
| PROJECT_NOT_FOUND | 404 | 项目不存在或不可见 | 返回项目列表 |
| PROJECT_ARCHIVED | 409 | 项目已归档，不能继续创作 | 工作台切只读，禁用创作入口 |
| RUN_STATE_CONFLICT | 409 | run 状态冲突 | 刷新 run 快照 |
| INTERRUPT_EXPIRED | 409 | 中断已过期 | 重新发起 |
| VALIDATION_FAILED | 400 | 参数错误 | 表单提示 |
| INTERNAL_ERROR | 500 | 系统错误 | 展示重试 |

## 前端使用说明

- loading：创建 session/run 时保持三栏骨架。
- streaming：SSE 事件驱动 MessageStream、ToolStatus、PreviewStage。
- interrupt：收到 `confirmation.required` / snapshot `interrupt` 后恢复确认面板并锁定模型和关键输入；用户拒绝时当前 run 取消，修改参数后创建新 run 重新预估。
- resume：优先事件补偿，失败后 snapshot 恢复。
- error：保留已有消息和资产引用，展示错误卡。

## 运行证据

- Agent API 与 SSE 证据：见 `tests/reports/m3-technical-baseline-report.md` 和 `tests/reports/m6-service-acceptance-report.md`。
- 字段级事实源验证入口：`api/openapi/agent-workbench.yaml`，由 M0-M6 门禁持续扫描。
- 新增或调整 API 时必须同步 OpenAPI、服务测试和本文档。
