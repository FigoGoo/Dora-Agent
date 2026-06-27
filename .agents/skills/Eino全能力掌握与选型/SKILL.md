---
name: Eino全能力掌握与选型
description: 用于智能体微服务中选择 Eino ADK、Agent、Graph、Workflow、Tool、Skill、Memory、Callback、Interrupt、TurnLoop 等能力。
---

# Eino全能力掌握与选型

## 目标

避免把 Eino 简化为单一 Agent API，按任务复杂度选择合适的 Eino 能力组合。

## 使用场景

- 设计或评审智能体微服务架构。
- 新增 Agent、Graph、Workflow、Tool、Skill、Retriever、Memory、Callback、Interrupt / Resume 或 TurnLoop 能力。
- 需要解释为什么选择某种 Eino 编排方式。

## 输入

- 产品目标和 Agent 能力清单。
- 任务是否确定性、是否需要多轮、是否需要外部工具、是否需要知识检索。
- AG-UI、RPC、Agent 领域数据和测试要求。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 识别任务性质：对话型、确定性多步骤、长任务、检索增强、工具密集、需要人工确认。
2. 选择 Agent：开放式推理、自然语言规划、多工具调用、需要模型动态决策时使用。
3. 选择 Graph：流程节点和分支清晰、需要可视化状态和节点级事件时使用。
4. 选择 Workflow：业务步骤固定、输入输出稳定、追求确定性执行时使用。
5. 选择 Tool：Agent 需要调用外部能力、RPC、文件、搜索、媒体生成或业务服务时使用。
6. 选择 Skill：可复用的提示词、方法、领域能力或操作规程需要被 Agent 调用时使用。
7. 选择 Retriever：需要从知识库、历史产物、文档或业务可读数据中检索上下文时使用。
8. 选择 Memory：需要跨轮保存偏好、上下文摘要、长期或短期记忆时使用。
9. 选择 Callback：需要观察模型、工具、节点、错误、token 或事件生命周期时使用。
10. 选择 Interrupt / Resume：需要人工确认、审批、补充输入或安全拦截时使用。
11. 选择 TurnLoop：需要多轮执行、追加用户输入、工具后继续推理、长任务状态管理时使用。
12. 评估 Multi-Agent：只有角色边界明确、需要并行或专长分工时使用。
13. 设计事件流、可观测性和测试策略。

## 输出

- Eino 能力选型表。
- Agent / Graph / Workflow / Tool / Skill / Retriever / Memory / Callback / Interrupt / TurnLoop 使用理由。
- 事件流与 AG-UI 映射建议。
- 可观测性和测试策略。

## 检查表

- [ ] 是否覆盖 Eino ADK、Agent、Graph、Workflow、Tool、Skill、Middleware、Retriever、Memory、Callback、Interrupt / Resume、TurnLoop、Multi-Agent。
- [ ] 是否说明不用某些能力的理由。
- [ ] 是否避免伪造未确认的 Eino API。
- [ ] 是否考虑事件流、可观测性和测试。
- [ ] 是否保持业务事实只能通过 RPC 产生。

## 注意事项

- 不确定 Eino API 时先查当前仓库依赖、官方文档或已有示例。
- 选型文档写能力边界，不写未经验证的具体 API 调用。
- Graph 和 Workflow 适合确定性流程，Agent 适合动态推理，不要混用概念。
