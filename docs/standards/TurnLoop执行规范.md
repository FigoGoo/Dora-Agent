# TurnLoop 执行规范

状态：active  
owner：文档与契约责任域
更新时间：2026-06-30
适用范围：智能体微服务多轮执行循环  

## 执行轮次

- session 表示会话生命周期。
- run 表示一次 Agent 执行。
- turn 表示用户输入与 Agent 响应的轮次。
- 每轮执行必须关联 session_id 和 run_id。

## 用户输入

- 用户输入先校验权限、会话状态和内容格式。
- 追加输入必须保存为消息并触发新的执行推进。

## 工具调用

- Tool 调用前记录 tool.call.started 事件。
- Tool 返回后记录 tool.call.completed 或 tool.call.failed 事件。
- 工具结果可触发继续推理或结束运行。

## 中断

- 需要人工确认、补充输入、审批或风险拦截时进入 interrupt。
- interrupt 必须保存原因、待确认动作、过期时间和恢复上下文。

## 恢复

- resume 必须校验 run、interrupt、用户权限和内部幂等语义。
- 恢复后输出 resume.accepted 并继续执行。

## 抢占

- preempt 用于取消、替换或抢占长任务。
- 抢占必须保存原因并输出状态事件。

## 长任务

- 长任务状态必须持久化。
- 任务应支持查询、取消、失败恢复和最终结果获取。

## 状态持久化

- session、run、message、event、task、interrupt、tool_call 必须按需要持久化。
- 不依赖进程内存作为唯一状态来源。

## 事件输出

- 状态变化、消息增量、工具调用、中断、恢复、完成和失败都要输出 AG-UI 事件。
- 事件必须支持顺序、幂等和重放。

## 失败恢复

- 可恢复失败保存恢复点并提示用户操作。
- 不可恢复失败输出 agent.run.failed，并保留 trace_id、run_id 和错误分类。
