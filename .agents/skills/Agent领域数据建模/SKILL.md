---
name: Agent领域数据建模
description: 用于设计智能体微服务的 Agent 领域数据库表、CRUD、状态机、索引和测试策略。
---

# Agent领域数据建模

## 目标

为 Agent Runtime 建立独立数据模型，明确 Agent DB 和业务 DB 边界。

## 使用场景

- 设计 Agent 会话、运行、消息、事件、工具调用、任务、中断、产物、记忆和配置表。
- 需要新增 Agent 领域 CRUD。
- 需要验证 Agent 领域数据库和业务数据库边界。

## 输入

- Agent 架构、TurnLoop、AG-UI 事件和测试要求。
- 当前数据库技术栈和已有表结构。
- RPC 契约中涉及的业务能力。

## 执行流程

1. 定义 Agent 领域边界：只保存 Agent Runtime 数据。
2. 定义 Agent 数据库边界：不保存订单、支付、审批结果、用户资产、交易、权限、主数据等业务事实。
3. 设计表清单：agent_sessions、agent_runs、agent_messages、agent_events、agent_tool_calls、agent_tasks、agent_interrupts、agent_artifacts、agent_memories、agent_runtime_configs。
4. 为每张表定义用途、主键、状态字段、审计字段和保留策略；数据库创建一律不添加外键约束，也不通过关联键表达表关联。
5. CRUD 设计原则：写入幂等，读取按 session_id、run_id、task_id、user_id 等常用路径优化；列表查询分页，默认 10 条每页。
6. 查询效率：尽量避免在 `for` 循环中逐条查询关联数据，优先批量查询、`IN` 查询或聚合读取。
7. 索引建议：围绕 run 查询、事件补偿、任务状态、用户历史和清理任务建立索引。
8. 状态机：为 session、run、task、interrupt 定义状态和合法迁移。
9. 数据保留策略：区分运行日志、事件、产物、记忆和配置的保留周期。
10. 审计字段：created_at、updated_at、created_by、tenant_id、trace_id 等按项目约束选择。
11. 和业务数据库的边界：业务事实必须通过 RPC 调用业务微服务产生。
12. 测试策略：覆盖 CRUD、状态迁移、幂等写、索引查询、数据清理和边界违规检查。

## 输出

- Agent 领域边界。
- Agent 数据库边界。
- 表清单和字段建议。
- CRUD 设计原则。
- 索引建议。
- 状态机。
- 数据保留策略。
- 审计字段。
- 和业务数据库的边界。
- 测试策略。

## 检查表

- [ ] 是否覆盖所有核心 Agent Runtime 表。
- [ ] 是否禁止保存业务事实。
- [ ] 是否没有创建数据库级外键约束。
- [ ] 列表查询是否分页且默认 10 条每页。
- [ ] 是否避免 `for` 循环逐条查询数据库或 RPC。
- [ ] 是否定义状态机和合法迁移。
- [ ] 是否考虑事件补偿查询。
- [ ] 是否有数据保留和审计字段。
- [ ] 是否有 Agent DB 与业务 DB 的测试区分。

## 注意事项

- Agent 领域表只存 Agent Runtime 数据。
- 业务事实必须通过 RPC 调用业务微服务产生。
- 智能体微服务不能直接写业务数据库。
