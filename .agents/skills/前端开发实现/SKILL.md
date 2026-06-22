---
name: 前端开发实现
description: 用于指导前端页面、组件、状态管理、HTTP API、SSE/WebSocket、AG-UI 消费和测试实现。
---

# 前端开发实现

## 目标

让前端稳定消费 API 和 AG-UI 事件，完整呈现 Agent 生成、工具调用、人工确认和任务状态。

## 使用场景

- 实现页面、组件、状态管理或 API 集成。
- 消费 SSE / WebSocket AG-UI 事件。
- 展示 Agent 流式输出、Tool 调用和人工确认。

## 输入

- PRD、UI/UE 规范和验收标准。
- HTTP API 契约和 AG-UI 事件协议。
- 现有前端技术栈和测试约定。

## 执行流程

1. 页面开发：按用户流程组织入口、工作区、历史、预览和任务详情。
2. 组件开发：把输入、参数、流式消息、Tool 状态、确认弹窗、产物卡片拆成可复用组件。
3. 状态管理：区分 loading、empty、error、success、streaming、interrupt、resume。
4. HTTP API：只使用契约字段，处理鉴权、分页、状态码和错误码。
5. SSE / WebSocket：处理连接、重连、关闭、心跳和 last_event_id。
6. AG-UI 事件消费：按 event_id 和 sequence 幂等合并事件。
7. Agent 流式输出：增量渲染 message.delta，完成后固化 message.completed。
8. Tool 调用展示：展示 tool.call、tool.result、耗时、参数摘要和错误。
9. 人工确认：展示 interrupt.required，支持确认、拒绝、补充输入和 resume 状态。
10. 错误态、加载态、空态：每个核心页面和组件都要定义。
11. 测试：覆盖组件、状态 reducer、事件消费、API mock 和关键浏览器路径。

## 输出

- 页面开发说明。
- 组件和状态管理说明。
- HTTP API 与 SSE / WebSocket 集成说明。
- AG-UI 事件消费说明。
- Agent 流式输出、Tool 调用、人工确认和错误状态。
- 测试结果。

## 检查表

- [ ] 是否没有发明后端字段。
- [ ] 是否按 AG-UI 协议消费事件。
- [ ] 是否处理全部关键状态。
- [ ] 是否支持流式增量渲染。
- [ ] 是否展示 Tool 调用和结果。
- [ ] 是否覆盖人工确认和 resume。
- [ ] 是否有前端测试。

## 注意事项

- API 字段和事件字段不一致时报告主控 Codex。
- 前端不定义 RPC 契约。
- 不把未文档化事件写成长期逻辑。
