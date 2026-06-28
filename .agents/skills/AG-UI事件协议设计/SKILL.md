---
name: AG-UI事件协议设计
description: 用于设计智能体微服务到前端的 AG-UI 事件协议和前端消费规则。
---

# AG-UI事件协议设计

## 目标

建立智能体微服务到前端的实时事件边界，让 Agent 输出、Tool 调用、Graph 节点、人工确认和错误都可展示、可恢复、可测试。

## 使用场景

- 新增或变更 AG-UI 事件。
- 设计 SSE / WebSocket 流式输出。
- 前端需要消费 Agent 实时状态。
- QA 需要验证事件顺序、payload 和断线重连。

## 输入

- Eino 内部事件、Agent 生命周期、Graph 节点事件和 Tool 调用事件。
- 前端展示需求和状态规范。
- 会话、运行、任务和中断状态模型。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 定义 AG-UI 事件边界：只承载前端需要展示或驱动交互的事件。
2. 映射 Eino 内部事件到 AG-UI：保留语义，不泄露内部实现细节。
3. 选择承载方式：SSE 适合单向流，WebSocket 适合双向实时交互；HTTP API 用于查询和补偿。
4. 定义事件类型：至少评估 agent.run.started、agent.message.delta、agent.message.completed、tool.call.started、tool.call.completed、tool.call.failed、generation.progress、confirmation.required、resume.accepted、workspace.assets.updated、agent.run.completed、agent.run.failed。
5. 定义事件 payload：包含 event_id、session_id、run_id、timestamp、type、payload、sequence。
6. 定义事件顺序：同一 run 内 sequence 单调递增，前端可按 sequence 合并。
7. 定义事件幂等：event_id 全局唯一，重复事件前端可忽略。
8. 设计断线重连：支持 last_event_id 或 run_id + sequence 补偿。
9. 设计错误事件：区分用户错误、工具错误、RPC 错误、模型错误和系统错误。
10. 设计人工确认事件：confirmation.required 说明确认项、风险、可选动作和过期时间。
11. 设计 Tool 调用事件、消息增量事件和任务完成事件。

## 输出

- AG-UI 事件边界。
- Eino 内部事件到 AG-UI 的映射。
- SSE / WebSocket 承载方式。
- 事件类型。
- 事件 payload。
- 事件顺序和幂等规则。
- 断线重连。
- 错误事件、人工确认事件、Tool 调用事件、消息增量事件、任务完成事件。

## 检查表

- [ ] 是否定义 event_id、session_id、run_id、timestamp、sequence。
- [ ] 是否覆盖建议事件类型。
- [ ] 是否说明 SSE / WebSocket 使用方式。
- [ ] 是否支持断线重连。
- [ ] 是否支持 unknown event 兼容。
- [ ] 是否覆盖前端渲染规则和测试点。

## 注意事项

- 前端不能把未文档化事件写死为业务逻辑。
- 智能体服务不能暴露过多 Eino 内部实现字段。
- 错误事件要能被用户理解，也要保留排障字段。
