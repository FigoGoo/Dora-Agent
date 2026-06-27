---
name: Eino智能体架构设计
description: 用于设计 Go + Eino 智能体微服务的整体架构、边界、事件流、RPC 调用和 Agent 领域数据。
---

# Eino智能体架构设计

## 目标

设计独立 Agent Runtime 微服务，清晰划分 Eino 编排、AG-UI 事件、RPC client、会话状态和 Agent 领域数据。

## 使用场景

- 新建或调整智能体微服务架构。
- 设计 Agent API、事件流、TurnLoop 或 Agent 领域表。
- 需要和业务微服务、前端协作确定边界。

## 输入

- 产品设计、Agent 能力清单和验收标准。
- RPC 契约、AG-UI 事件协议、Agent 数据模型规范。
- 当前服务目录、依赖、测试和部署约束。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 服务职责边界：确认智能体服务只负责 Agent Runtime，不直接处理业务事实。
2. Agent / Graph / Workflow / Tool / Skill 划分：说明每类能力负责什么、如何组合。
3. Agent API：定义会话、运行、消息、任务、产物、中断和恢复接口。
4. 事件流协议：定义内部事件、生命周期、顺序、幂等和错误事件。
5. AG-UI 映射：把 Agent、Message、Tool、Graph、Interrupt、Resume 事件映射到前端协议。
6. RPC 调用边界：所有业务事实变化通过 RPC client 调用业务服务。
7. Agent 会话状态：定义 session、run、turn、message、task 的状态关系。
8. Agent 运行状态：定义 pending、running、interrupting、resuming、completed、failed、cancelled 等状态。
9. Agent 领域数据模型：定义 Agent Runtime 表、索引、审计字段和保留策略。
10. 错误处理：区分模型错误、工具错误、RPC 错误、用户输入错误和系统错误。
11. 可观测性：定义日志、指标、trace、callback 和运行记录。
12. 测试策略：覆盖单元、集成、契约、事件流、恢复和数据库验证。

## 输出

- 智能体微服务架构说明。
- Agent / Graph / Workflow / Tool / Skill 划分。
- Agent API 和事件流协议。
- AG-UI 映射。
- RPC 调用边界。
- Agent 会话状态和运行状态。
- Agent 领域数据模型。
- 错误处理、可观测性和测试策略。

## 检查表

- [ ] 是否明确服务职责边界。
- [ ] 是否禁止智能体服务直接写业务库。
- [ ] 是否覆盖 AG-UI 映射。
- [ ] 是否覆盖 RPC client 和业务服务边界。
- [ ] 是否定义 Agent 领域数据。
- [ ] 是否包含错误处理、可观测性和测试策略。

## 注意事项

- 智能体服务可以有自己的数据库，但只保存 Agent Runtime 数据。
- 需要业务确认的操作必须走 RPC preview / confirm 或等价模式。
- 架构设计应先于实现。
