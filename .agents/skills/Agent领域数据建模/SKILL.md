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

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

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
