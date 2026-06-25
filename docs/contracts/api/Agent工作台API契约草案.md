# Agent 工作台 API 契约草案

状态：draft
owner：主控 Codex 汇总维护；Go Eino 智能体微服务架构工程师和前端开发工程师确认
更新时间：2026-06-25
适用范围：前端 -> 智能体微服务，统一 Agent 工作台

## API 清单

| Method | Path | 描述 | 鉴权 | 状态 |
| --- | --- | --- | --- | --- |
| POST | `/api/agent/sessions` | 在项目下创建会话 | 登录态 | planned |
| GET | `/api/agent/sessions/:session_id` | 查询会话快照 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs` | 发送用户输入并创建 run | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/events` | SSE 实时事件 | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/event-replay` | 按 sequence 补偿事件 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/interrupts/:interrupt_id/confirm` | 确认人工中断 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/interrupts/:interrupt_id/reject` | 拒绝人工中断 | 登录态 + 项目权限 | planned |
| POST | `/api/agent/runs/:run_id/cancel` | 取消 run | 登录态 + 项目权限 | planned |
| GET | `/api/agent/runs/:run_id/snapshot` | 查询 run 快照 | 登录态 + 项目权限 | planned |

## 通用请求头

| 字段 | 位置 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| Authorization | header | string | 是 | 登录 token |
| X-Trace-Id | header | string | 是 | 链路追踪 |
| Idempotency-Key | header | string | 写操作必填 | 幂等键 |
| Last-Event-ID | header | string | SSE 重连可选 | 最近消费事件 |

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
| user_message | body | object | 是 | 用户输入，结构待细化 |
| model_selection | body | object | 否 | 用户选择模型 |
| referenced_asset_ids | body | array | 否 | 引用业务资产 |

响应：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| run_id | string | 是 | Agent run ID |
| session_id | string | 是 | 会话 ID |
| stream_url | string | 是 | SSE URL |
| status | string | 是 | `pending` / `running` |

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
- interrupt：收到确认事件后锁定模型和关键输入。
- resume：优先事件补偿，失败后 snapshot 恢复。
- error：保留已有消息和资产引用，展示错误卡。

## 待确认

- 用户输入 DTO 是否支持文本、上传文件、素材引用、控件输入分开建模。
- SSE URL 是否独立鉴权 token。
- run 并发策略：同一 session 是否允许多个 active run。
