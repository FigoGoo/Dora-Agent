---
name: TurnLoop多轮执行设计
description: 用于设计多轮 Agent 执行循环、中断、恢复、追加用户输入和长任务状态。
---

# TurnLoop多轮执行设计

## 目标

定义 Agent 在单轮、多轮、工具调用、人工确认、中断恢复和长任务中的执行循环。

## 使用场景

- Agent 需要多轮对话或追加用户输入。
- 工具调用后需要继续推理。
- 流程需要 interrupt、resume、preempt。
- 长任务需要状态持久化和事件输出。

## 输入

- Agent 能力清单和产品流程。
- Agent 会话、运行、消息、任务和中断数据模型。
- AG-UI 事件协议和 RPC 契约。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 定义 TurnLoop 职责：接收输入、推进推理、执行工具、输出事件、保存状态、处理暂停和恢复。
2. 单轮执行：用户输入进入 run，Agent 推理，可能调用 Tool，产生消息和完成事件。
3. 多轮执行：保留 session 上下文，追加用户输入，生成新的 turn 或 run。
4. 用户输入追加：校验 session、run 状态和权限，保存消息后继续执行。
5. 工具调用后继续推理：保存 tool.call.started、tool.call.completed 或 tool.call.failed，必要时再次调用模型。
6. interrupt：保存中断原因、待确认动作、风险说明、过期时间和恢复 token。
7. resume：校验中断状态、用户确认、补充输入和幂等键，继续执行。
8. preempt：支持取消、替换或抢占长任务，保存原因并输出事件。
9. 长任务状态：维护 pending、running、waiting_confirmation、resuming、completed、failed、cancelled。
10. 会话状态：区分 session 生命周期和 run 生命周期。
11. 事件输出：每次状态变化都输出可幂等的 AG-UI 事件。
12. 失败恢复：可恢复错误保存恢复点，不可恢复错误输出 agent.run.failed。
13. 测试场景：覆盖单轮、多轮、工具后继续、中断、恢复、抢占、断线重连和失败恢复。

## 输出

- TurnLoop 职责。
- 单轮和多轮执行流程。
- 用户输入追加规则。
- 工具调用后继续推理规则。
- interrupt / resume / preempt 规则。
- 长任务状态和会话状态。
- 事件输出。
- 失败恢复。
- 测试场景。

## 检查表

- [ ] 是否定义 session、run、turn 的关系。
- [ ] 是否保存中断和恢复所需状态。
- [ ] 是否定义抢占和取消。
- [ ] 是否所有状态变化都有事件。
- [ ] 是否支持断线后补偿。
- [ ] 是否区分可恢复和不可恢复失败。

## 注意事项

- TurnLoop 不直接改变业务事实，业务操作通过 RPC Tool 完成。
- resume 必须幂等，避免重复提交业务写操作。
- 长任务不能只依赖内存状态。
