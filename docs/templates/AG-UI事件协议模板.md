# AG-UI 事件协议模板

状态：draft  
owner：文档与契约责任域
更新时间：YYYY-MM-DD  
适用范围：智能体微服务 -> 前端  

## 事件背景

说明事件服务的用户流程、Agent 能力和前端展示目标。

## 传输方式

- SSE：
- WebSocket：
- HTTP 补偿查询：

## 事件类型

| 事件类型 | 触发时机 | 生产方 | 消费方行为 |
| --- | --- | --- | --- |
| agent.run.started | Agent 运行开始 | 智能体微服务 | 展示运行中 |
| agent.message.delta | 消息增量 | 智能体微服务 | 增量渲染 |
| agent.message.completed | 消息完成 | 智能体微服务 | 固化消息 |
| tool.call.started | Tool 调用开始 | 智能体微服务 | 展示调用中 |
| tool.call.completed | Tool 调用完成 | 智能体微服务 | 展示结果 |
| tool.call.failed | Tool 调用失败 | 智能体微服务 | 展示失败状态 |
| generation.progress | 生成进度 | 智能体微服务 | 更新进度 |
| confirmation.required | 需要人工确认 | 智能体微服务 | 展示确认 UI |
| resume.accepted | 恢复已接受 | 智能体微服务 | 展示恢复中 |
| workspace.assets.updated | 资产更新 | 智能体微服务 | 更新工作区资产 |
| process.snapshot.saved | 快照保存 | 智能体微服务 | 标记可恢复点 |
| agent.run.completed | Agent 完成 | 智能体微服务 | 展示完成 |
| agent.run.failed | Agent 失败 | 智能体微服务 | 展示错误 |

## 事件顺序

- 同一 run 内 sequence 单调递增。
- 前端按 event_id 去重，按 sequence 合并。

## payload schema

```json
{
  "event_id": "evt_xxx",
  "type": "agent.message.delta",
  "session_id": "sess_xxx",
  "run_id": "run_xxx",
  "sequence": 1,
  "timestamp": "2026-06-22T08:00:00Z",
  "payload": {}
}
```

## 错误事件

- error_code：
- message：
- recoverable：
- trace_id：
- suggested_action：

## 中断事件

- interrupt_id：
- reason：
- confirmation_payload：
- allowed_actions：
- expires_at：

## 恢复事件

- resume_id：
- interrupt_id：
- accepted_at：
- next_state：

## Tool 调用事件

- tool_call_id：
- tool_name：
- input_summary：
- output_summary：
- status：
- latency_ms：

## 消息增量事件

- message_id：
- role：
- delta：
- index：
- is_final：

## 前端渲染规则

- loading：
- streaming：
- tool running：
- interrupt：
- resume：
- completed：
- failed：

## 兼容性

- 未知事件处理：
- 新增字段策略：
- 废弃字段策略：
- 断线重连策略：
