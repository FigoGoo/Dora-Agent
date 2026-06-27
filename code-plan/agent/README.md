# Eino 智能体微服务生产级系统设计开发文档

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：`services/agent/**`、Agent API、Eino Runtime、AG-UI 事件生产、RPC client、Agent Runtime 数据库、Agent 侧测试
相关代码路径：`services/agent/**`、`api/thrift/**`、`api/openapi/**`、`db/migrations/iterations/**`、`tests/agent/**`、`tests/contract/**`
相关设计契约：`code-plan/README.md`、`code-plan/agent/**`、`code-plan/business/**`、`code-plan/tests/**`、`docs/product/**`、`docs/architecture/**`、`docs/standards/**`
后续实现落点：`api/thrift/**`、`api/openapi/**`、`api/agui/**`、`db/migrations/iterations/**`、`tests/agent/**`、`tests/contract/**`

## 说明

本目录是 Eino 智能体微服务的正式生产级系统设计开发文档，用于和业务开发、测试对齐范围、顺序和职责边界。当前文档体系已经包含架构、模型、函数出入参、RPC/AG-UI 边界、流程闭环、日志和测试设计。现有临时代码、临时 schema、fixture 或 migration 如与本目录不一致，以本目录设计为准，在对应功能切片内同步修正。

本次开发不涉及前端开发和部署上线文档；Agent 侧只负责定义和生产 Agent API、AG-UI 事件、SSE 补偿和 schema/fixture，不实现前端 reducer、页面或浏览器 UI 自动化。

每份详细设计包含：

- 架构设计。
- Eino 模块选型。
- Go package 与目录设计。
- model / DTO / schema 设计，且 model 字段使用中文注释。
- 函数功能、入参、出参、错误码和幂等设计。
- 设计模式与实现策略。
- 职责内流程闭环。
- 业务流程图，必须用 Mermaid 表达 Agent、业务服务、前端、外部模型或 Tool 的完整闭环。
- 代码逻辑图，必须用 Mermaid 表达 Handler、Application、Runtime、Eino、Repository、RPC client 和事件发布的调用顺序。
- 日志、trace、指标和脱敏规则。
- 测试设计与验收标准。

## 开发顺序

| 顺序 | 文档 | 目标 |
| --- | --- | --- |
| 00 | [事实源需求映射与开发顺序](./00-事实源需求映射与开发顺序.md) | 汇总产品、设计、架构、契约和规范事实源，定义开发顺序和跨域对齐点。 |
| 01 | [智能体微服务总体架构与包结构设计](./01-智能体微服务总体架构与包结构设计.md) | 定义 Agent 服务边界、分层架构、Go package、模块依赖和代码落点。 |
| 02 | [Eino模块选型与统一Agent运行时设计](./02-Eino模块选型与统一Agent运行时设计.md) | 定义 Eino Agent、Graph、Workflow、Tool、Skill、Retriever、Memory、Callback、Interrupt/Resume 的使用方式。 |
| 03 | [Agent领域模型状态机与数据库设计](./03-Agent领域模型状态机与数据库设计.md) | 定义 Agent Runtime model、表、索引、状态机、Repository 和 CRUD。 |
| 04 | [Agent API会话运行与函数出入参设计](./04-AgentAPI会话运行与函数出入参设计.md) | 定义 session、run、message、confirm、cancel、snapshot API 和函数出入参。 |
| 05 | [TurnLoop中断恢复取消与长任务设计](./05-TurnLoop中断恢复取消与长任务设计.md) | 定义多轮执行、追加输入、中断、恢复、取消、长任务和状态落库闭环。 |
| 06 | [AG-UI事件生产SSE补偿与Payload设计](./06-AGUI事件生产SSE补偿与Payload设计.md) | 定义内部事件、AG-UI payload、SSE、事件补偿、快照恢复和前端消费边界。 |
| 07 | [RPC客户端业务能力调用与DTO映射设计](./07-RPC客户端业务能力调用与DTO映射设计.md) | 定义 Agent 调用业务微服务的 RPC client、DTO 映射、超时、重试、幂等和错误码。 |
| 08 | [Skill路由Tool执行模型选择与安全治理设计](./08-Skill路由Tool执行模型选择与安全治理设计.md) | 定义 Skill 池加载、意图识别、Tool 策略、模型选择锁定和内容安全评估。 |
| 09 | [积分确认冻结生成资产保存扣费释放闭环设计](./09-积分确认冻结生成资产保存扣费释放闭环设计.md) | 定义生成类任务从预估到资产保存、扣费或释放的 Agent 侧流程闭环。 |
| 10 | [黑板资产引用快照记忆与会话恢复设计](./10-黑板资产引用快照记忆与会话恢复设计.md) | 定义黑板、过程资产元素、资产引用、快照、Memory 和会话恢复。 |
| 11 | [日志观测错误处理配置化与测试验收设计](./11-日志观测错误处理配置化与测试验收设计.md) | 定义日志、trace、指标、配置化、错误分类和测试验收策略。 |
| 12 | [Skill测试运行输出元素校验与安全证据设计](./12-Skill测试运行输出元素校验与安全证据设计.md) | 定义 Skill 发布前测试运行、输出元素校验、测试隔离和安全证据复用边界。 |
| 13 | [模型Tool适配器任务执行取消与供应商错误设计](./13-模型Tool适配器任务执行取消与供应商错误设计.md) | 定义图片、音乐、视频模型 Tool 适配器、任务提交/查询/取消、超时和供应商错误分类。 |
| 14 | [生产级闭眼开发门禁与任务切片验收设计](./14-生产级闭眼开发门禁与任务切片验收设计.md) | 定义进入生产级开发的契约冻结、任务切片、阻断门禁和验收矩阵。 |

## 跨服务边界

- 智能体微服务只保存 Agent Runtime 数据，不保存积分账户、项目事实、业务资产事实、作品公开状态、企业成员关系等业务事实。
- 业务事实读写必须通过业务微服务 RPC。
- AG-UI 是智能体微服务到前端的实时事件边界。
- Agent API 是前端工作台到智能体微服务的 HTTP/SSE 边界。
- Agent 数据库建表不添加数据库级外键约束，不写外键或引用约束关键字。
- 列表查询默认 `page_size=10`，需要定义上限并避免逐条循环查询。

## 对齐方式

每份详细设计都必须显式包含 `【业务开发】需要提供的能力与参数` 章节。该章节至少写清：

- 业务服务名和方法名。
- 调用时机。
- 请求参数，包含 `auth_context`、`request_meta`、业务参数、分页参数、幂等键和安全证据。
- 响应参数，包含业务结果、展示摘要、错误详情、状态、可重试性和 trace。
- 超时、重试、幂等、权限、审计和版本兼容要求。
- Agent 侧使用方式：只读、写入、preview / confirm、事件展示、DB 引用或错误映射。

通用参数约定：

| 参数 | 提供方 | 必填 | 说明 |
| --- | --- | --- | --- |
| `auth_context.actor_user_id` | 业务开发确认 | 是 | 当前操作者用户 ID。 |
| `auth_context.login_identity_type` | 业务开发确认 | 是 | Thrift 枚举值固定为 `PERSONAL`、`ENTERPRISE_MEMBER`、`ADMIN`。 |
| `auth_context.space_id` | 业务开发确认 | 登录态业务必填 | 当前个人空间或企业空间。 |
| `auth_context.enterprise_id` | 业务开发确认 | 企业空间必填 | 当前企业 ID。 |
| `auth_context.enterprise_role` | 业务开发确认 | 企业空间必填 | `owner` 或 `member`。 |
| `request_meta.trace_id` | Agent 传入，业务透传 | 是 | 全链路 trace。 |
| `request_meta.idempotency_key` | Agent 生成，业务校验 | 写操作必填 | 重复请求返回同一结果或幂等冲突。 |
| `request_meta.source` | Agent 传入 | 是 | Agent RPC 调用固定为 `agent_service`。 |
| `pagination.page_size` | Agent 传入，业务限制 | 列表必填 | 默认 10，最大值由业务契约声明。 |
| `safety_evidence` | Agent 生成，业务校验 | 积分预估、生成资产保存、公开/资产写入场景必填 | 只传脱敏证据摘要，不传原文和内部策略。 |

业务开发重点确认：

- `07` 中业务 RPC 能力、DTO、错误码、幂等和权限上下文是否符合业务语义。
- `08` 和 `13` 中 `ResolveGenerationModelSnapshot`、模型 Tool 适配、供应商产物上传到业务签发 TOS object key 是否与业务模型配置和 TOS 策略一致。
- `09` 中积分、对象准备、资产保存和扣费释放闭环是否与业务事务边界一致。
- `03` 中 Agent DB 是否没有复制业务事实。
- `04` 和 `05` 中项目归档、权限校验、确认恢复和取消策略是否需要业务侧额外能力。
- `12` 中 Skill 测试运行是否由 Agent 提供 runtime，业务侧只保存测试结果和审核状态。
- `13` 中模型 Tool 任务状态、取消、供应商错误是否需要业务侧模型配置或任务记录配合。
- `14` 中契约冻结、测试夹具、事务语义和测试账号是否满足闭眼开发门禁。

后续前端接入需要保留的契约点，本次不实现前端：

- `04` 中 Agent API 是否覆盖工作台交互。
- `06` 中 AG-UI 事件类型、payload、sequence、补偿和快照是否满足后续 reducer。
- `08` 中模型选择、输入控件锁定、Skill/Tool Tag 是否满足工作台体验。
- `10` 中黑板、资产引用和恢复快照是否满足 A2UI 渲染。

测试重点确认：

- 每份详细设计对应实现时，测试需要覆盖函数级、状态机、RPC 测试夹具、AG-UI 顺序、断线补偿、Agent DB 和边界违规检查。
