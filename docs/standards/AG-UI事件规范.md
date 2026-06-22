# AG-UI 事件规范

状态：active  
owner：主控 Codex 汇总维护  
适用范围：智能体微服务到前端的实时事件协议  

## 事件类型

建议基础事件：

- `agent.started`
- `message.started`
- `message.delta`
- `message.completed`
- `tool.call`
- `tool.result`
- `graph.node.started`
- `graph.node.completed`
- `interrupt.required`
- `resume.accepted`
- `agent.completed`
- `agent.failed`

新增事件必须先文档化。

## payload

统一事件结构：

```json
{
  "event_id": "evt_xxx",
  "type": "message.delta",
  "session_id": "sess_xxx",
  "run_id": "run_xxx",
  "sequence": 12,
  "timestamp": "2026-06-22T08:00:00Z",
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

## timestamp

- 使用服务端时间。
- 用于展示和排障，不作为唯一排序依据。

## 顺序

- 同一 run 内 sequence 单调递增。
- 前端按 sequence 合并 message.delta 和状态事件。

## 幂等

- 前端遇到重复 event_id 必须忽略。
- 服务端重放历史事件时不得改变事件语义。

## 断线重连

- 支持 last_event_id 或 run_id + sequence 续传。
- 无法续传时前端应调用 HTTP API 查询 run 当前状态。

## 错误事件

- `agent.failed` 表示运行失败。
- Tool、RPC、模型或权限错误应在 payload 中说明 error_code、message、recoverable、trace_id。

## 未知事件兼容

- 前端遇到未知事件类型应记录并忽略，不应崩溃。
- 服务端新增事件必须保持旧前端可降级。

## 前端展示规则

- `message.delta` 增量渲染，`message.completed` 固化消息。
- `tool.call` 展示调用中，`tool.result` 展示成功或失败。
- `interrupt.required` 展示人工确认操作。
- `agent.completed` 展示完成状态。
- `agent.failed` 展示可理解错误和重试入口。
