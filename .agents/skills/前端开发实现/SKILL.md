---
name: 前端开发实现
description: 用于指导前端页面、组件、状态管理、HTTP API、SSE/WebSocket、AG-UI 消费和测试实现；开发前必须对齐产品/设计文档、后端接口契约、入参/出参字段、业务规则、错误和权限边界。
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
- HTTP API 契约、后端接口文档、字段级契约和 AG-UI 事件协议。
- 页面动作、默认筛选、分页排序、表单字段、业务规则、权限和错误处理要求。
- 现有前端技术栈和测试约定。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发页面、表单或 API hook 前，必须读取对应产品/设计文档和后端接口契约；前端不能只根据截图、fixture、mock 数据或已有组件猜业务逻辑和字段。
- HTTP API 字段以 `docs/contracts/字段级契约索引.md` 指向的 OpenAPI、Thrift、JSON Schema、migration 和 fixture 为准；涉及业务系统接口优先查 `api/openapi/**`，涉及 RPC 边界查 `api/thrift/**` 和 `docs/contracts/rpc/**`。
- 如果是管理端页面，额外核对 `/api/admin/**` 接口、后台页面设计和审计/权限要求；如果是用户端或 Agent 工作台，额外核对用户流程、AG-UI 事件和业务状态机。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 文档对齐：先确认当前 active 的产品、设计、技术和契约入口；缺失时补文档或标记阻塞，不直接编码。
2. 接口盘点：把页面动作映射到接口或事件，列出 method/path、query/path/body/header 入参、response 出参、分页排序、鉴权、幂等、审计原因、错误信封和 trace_id。
3. 业务规则：确认默认筛选、初始入参、状态枚举、字段可见性、权限、危险操作、空/错/加载/成功状态来自文档或契约。
4. 页面开发：按用户流程组织入口、工作区、历史、预览和任务详情。
5. 组件开发：把输入、参数、流式消息、Tool 状态、确认弹窗、产物卡片拆成可复用组件。
6. 状态管理：区分 loading、empty、error、success、streaming、interrupt、resume。
7. HTTP API：只使用契约字段，处理鉴权、分页、状态码、错误码和字段适配；接口不清时先查 handler、fixture 或契约责任域，不猜字段名。
8. SSE / WebSocket：处理连接、重连、关闭、心跳和 last_event_id。
9. AG-UI 事件消费：按 event_id 和 sequence 幂等合并事件。
10. Agent 流式输出：增量渲染 agent.message.delta，完成后固化 agent.message.completed。
11. Tool 调用展示：展示 tool.call.started、tool.call.completed 或 tool.call.failed、耗时、参数摘要和错误。
12. 人工确认：展示 confirmation.required，支持确认、拒绝、补充输入和 resume 状态。
13. 错误态、加载态、空态：每个核心页面和组件都要定义。
14. 测试：覆盖组件、状态 reducer、事件消费、API mock、接口入参断言和关键浏览器路径。

## 输出

- 页面开发说明。
- 组件和状态管理说明。
- 已对齐的接口清单：页面动作、接口/事件、入参、出参、默认值、错误和权限。
- HTTP API 与 SSE / WebSocket 集成说明。
- AG-UI 事件消费说明。
- Agent 流式输出、Tool 调用、人工确认和错误状态。
- 未确认字段、契约缺口或设计/接口冲突。
- 测试结果。

## 检查表

- [ ] 是否没有发明后端字段。
- [ ] 是否已读取当前产品/设计文档和后端接口契约，而不是只按截图、fixture 或 mock 数据开发。
- [ ] 是否把页面动作映射到明确接口或 AG-UI 事件。
- [ ] 是否核对 query/path/body/header 入参、response 出参、分页排序、状态枚举、默认值和错误信封。
- [ ] 是否核对鉴权、权限、幂等、审计原因、trace_id 和敏感信息边界。
- [ ] 是否在 API hook 或 adapter 测试中断言关键入参字段和错误解析。
- [ ] 是否按 AG-UI 协议消费事件。
- [ ] 是否处理全部关键状态。
- [ ] 是否支持流式增量渲染。
- [ ] 是否展示 Tool 调用和结果。
- [ ] 是否覆盖人工确认和 resume。
- [ ] 是否有前端测试。

## 注意事项

- API 字段和事件字段不一致时报告文档与契约责任域。
- 设计文档、接口文档和代码不一致时，先确认字段级事实源；不要用前端适配掩盖契约错误。
- fixture 和 mock 只能辅助测试，不能替代 OpenAPI、Thrift、AG-UI Schema 或 active 设计文档。
- 默认筛选、初始请求参数、状态枚举和表单字段必须可追溯到文档或契约。
- 前端不定义 RPC 契约。
- 不把未文档化事件写成长期逻辑。
