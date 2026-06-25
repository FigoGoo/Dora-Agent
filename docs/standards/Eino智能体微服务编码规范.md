# Eino 智能体微服务编码规范

状态：active  
owner：主控 Codex 汇总维护  
更新时间：2026-06-25
适用范围：`/services/agent/**` 和相关 Agent Runtime 代码  

## Eino ADK

- 使用 Eino 前先确认当前依赖版本和已有示例。
- 不确定 API 时不臆造，先查依赖、官方文档或仓库示例。

## Agent

- Agent 用于开放式推理、多工具选择和自然语言规划。
- Agent 不直接写业务数据库，业务写操作通过 RPC Tool。
- Agent 能力必须优先配置化，具体要求见 `docs/standards/智能体配置化规范.md`。

## Graph

- Graph 用于节点和分支明确的流程。
- 每个节点的输入、输出、事件和错误路径必须可测试。

## Workflow

- Workflow 用于步骤固定、输入输出稳定、偏确定性的任务。
- Workflow 状态变化要能映射到 Agent 运行记录。

## Tool

- Tool 封装外部能力、RPC client、媒体生成、文件处理或检索。
- Tool 输入输出 DTO 必须清晰，错误必须分类。
- Tool 输入输出 DTO 按领域和业务场景划分，不复用业务大对象或 ORM 对象。
- 列表型 Tool 输出必须分页，默认 10 条每页。

## Skill

- Skill 保存可复用方法、提示词策略或领域操作流程。
- Skill 不替代 RPC 契约或业务规则。

## Middleware

- Middleware 用于日志、trace、鉴权上下文、限流、重试、观测等横切逻辑。
- Middleware 不应改变业务语义。

## Retriever

- Retriever 用于知识库、历史消息、产物或文档检索。
- 检索结果要标注来源和可用范围。

## Memory

- Memory 用于短期上下文、长期偏好、会话摘要或用户授权记忆。
- 记忆不得保存未授权敏感业务事实。

## Callback

- Callback 用于观测模型、工具、节点、错误和 token 生命周期。
- Callback 输出应关联 session_id、run_id、trace_id。

## Interrupt / Resume

- 需要人工确认、审批、补充输入或安全拦截时使用 Interrupt / Resume。
- Resume 必须校验状态、权限和幂等。

## TurnLoop

- TurnLoop 负责多轮执行、追加输入、工具后继续推理、中断恢复和长任务状态。
- 长任务状态必须持久化。

## AG-UI

- 内部事件统一映射到文档化 AG-UI 事件。
- 必须包含 event_id、session_id、run_id、timestamp、sequence。

## RPC client

- RPC client 只调用业务服务契约，不绕过业务服务访问数据库。
- 超时、重试、幂等、权限上下文和错误码必须按契约处理。
- 避免在 `for` 循环中逐条调用业务 RPC 查询；需要多对象数据时优先提出批量 RPC 或分页查询契约。
- 调用列表 RPC 时必须传递分页参数，默认 10 条每页。

## Agent domain DB

- 只保存 Agent Runtime 数据。
- 不保存业务事实，不复制业务主数据。
- Agent 领域库建表一律不添加数据库级外键约束，也不通过关联键表达表关联；通过业务流程、幂等键、必要索引、应用层校验和测试维护一致性。
- Agent 领域库列表查询必须分页，默认 10 条每页。

## event stream

- 事件流支持顺序、幂等、断线重连和失败补偿。
- message.delta 等增量事件必须能被前端稳定合并。

## observability

- 记录 session、run、turn、tool_call、rpc、node、error 和 latency。
- 指标和日志用于排查 Agent 行为和性能。

## testing

- 覆盖 Agent 行为、Graph/Workflow 节点、Tool、RPC mock、事件流、Interrupt / Resume、TurnLoop 和 Agent DB。
