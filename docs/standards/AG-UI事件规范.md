# AG-UI 事件规范

状态：active  
owner：文档与契约责任域
更新时间：2026-07-01
适用范围：智能体微服务到前端的实时事件协议  

## 事件类型

字段级 envelope schema 以 `api/agui/agent-workbench-events.schema.json` 为准。事件 payload schema 以 `api/agui/events/**` 为准。

当前 PR-1 canonical 事件族：

- `creative.guide.presented`
- `creative.router.decided`
- `cost_disclosure.skill_usage.presented`
- `confirmation.required`

后续阶段继续补齐：

- `agent.run.started`
- `agent.run.completed`
- `agent.run.failed`
- `agent.run.cancelled`
- `agent.thinking.started`
- `agent.thinking.delta`
- `agent.thinking.completed`
- `agent.message.delta`
- `agent.message.completed`
- `tool.call.started`
- `tool.call.progress`
- `tool.call.completed`
- `tool.call.failed`
- `generation.progress`
- `generation.artifact.completed`
- `confirmation.required`
- `confirmation.accepted`
- `confirmation.rejected`
- `resume.accepted`
- `workspace.assets.updated`
- `workspace.blackboard.updated`
- `process.snapshot.saved`
- `project.archived.blocked`

新增事件必须先文档化。

## payload

统一事件结构：

```json
{
  "event_id": "evt_xxx",
  "event_type": "creative.router.decided",
  "schema_version": "agui.event.v1",
  "payload_schema_version": "creative.router.decided.v1",
  "project_id": "proj_xxx",
  "session_id": "sess_xxx",
  "run_id": "run_xxx",
  "seq": 12,
  "created_at": "2026-07-01T00:00:00Z",
  "dedupe_key": "run_xxx:creative.router.decided:12",
  "payload": {}
}
```

payload 内容按事件类型定义，避免泄露 Eino 内部实现细节。

## event_id

- 全局唯一。
- 用于前端幂等去重。
- 重放事件时 event_id 不变。

## session_id / run_id

- session_id 标识会话。
- run_id 标识一次 Agent 运行。
- 前端按 session_id 聚合历史，按 run_id 展示运行状态。

## created_at

- 使用服务端时间。
- 用于展示和排障，不作为唯一排序依据。

## 顺序

- 同一 run 内 sequence 单调递增。
- 同一 run 内 `seq` 单调递增。
- 前端按 `seq` 合并消息增量和状态事件。

## 幂等

- 前端遇到重复 event_id 必须忽略。
- 前端遇到重复 `dedupe_key` 必须忽略。
- 服务端重放历史事件时不得改变事件语义。

## 断线重连

- 支持 last_event_id 或 run_id + sequence 续传。
- 无法续传时前端应调用 HTTP API 查询 run 当前状态。

## 错误事件

- `agent.run.failed` 表示运行失败。
- Tool、RPC、模型或权限错误应在 payload 中说明 error_code、message、recoverable、trace_id。

## 未知事件兼容

- 前端遇到未知事件类型应记录并忽略，不应崩溃。
- 服务端新增事件必须保持旧前端可降级。

## 前端展示规则

- `agent.message.delta` 增量渲染，`agent.message.completed` 固化消息。
- `tool.call.started` 展示调用中，`tool.call.completed` 或 `tool.call.failed` 展示结果。
- `confirmation.required` 展示人工确认操作；Agent 内部 `interrupt` 只作为状态记录和 API 语义，不作为 canonical AG-UI 事件。
- `agent.run.completed` 展示完成状态。
- `agent.run.failed` 展示可理解错误和重试入口。
