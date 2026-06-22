# Agent 领域数据建模规范

状态：active  
owner：主控 Codex 汇总维护  
适用范围：智能体微服务 Agent Runtime 数据模型  

## Agent DB 边界

Agent DB 只保存 Agent Runtime 数据：会话、运行、消息、事件、工具调用、任务、中断、产物、记忆和运行配置。

Agent DB 不保存订单、支付、审批结果、用户资产、业务交易、业务权限、业务主数据等业务事实。

## Agent 表设计

建议表清单：

- `agent_sessions`：会话。
- `agent_runs`：一次 Agent 运行。
- `agent_messages`：用户、Agent、系统消息。
- `agent_events`：AG-UI 或内部事件。
- `agent_tool_calls`：Tool 调用记录。
- `agent_tasks`：长任务状态。
- `agent_interrupts`：中断和恢复状态。
- `agent_artifacts`：生成产物元数据。
- `agent_memories`：授权记忆。
- `agent_runtime_configs`：运行配置。

`agent_runtime_configs` 用于保存可版本化的智能体配置，具体要求见 `docs/standards/智能体配置化规范.md`。

## CRUD 规范

- 写入必须幂等，避免重复事件、重复 Tool 调用记录和重复恢复。
- 查询按 session_id、run_id、task_id、user_id、tenant_id 优化。
- 删除优先软删除或按保留策略归档。

## 状态机

- session：active、archived、expired。
- run：pending、running、waiting_confirmation、resuming、completed、failed、cancelled。
- task：pending、running、completed、failed、cancelled。
- interrupt：required、accepted、rejected、expired、resolved。
- 状态迁移必须可测试，不允许任意跳转。

## 索引

- `agent_runs(session_id, created_at)`
- `agent_events(run_id, sequence)`
- `agent_tasks(status, updated_at)`
- `agent_interrupts(run_id, status)`
- `agent_tool_calls(run_id, tool_name)`

实际索引以查询路径和数据库类型为准。

## 审计字段

建议包含：id、tenant_id、user_id、trace_id、created_at、updated_at、created_by、updated_by、deleted_at。

## 数据保留

- 事件和日志按排障周期保留。
- 产物元数据按业务展示周期保留。
- 记忆按用户授权和隐私要求保留。
- 配置需要版本和审计。

## Agent DB 与业务 DB 边界

- Agent DB 不作为业务事实来源。
- 业务事实必须通过 RPC 调用业务微服务产生。
- 智能体微服务不能直接写业务数据库。

## 测试策略

- CRUD 测试。
- 状态机迁移测试。
- 幂等写测试。
- 事件补偿查询测试。
- 数据保留策略测试。
- Agent DB 与业务 DB 边界测试。
