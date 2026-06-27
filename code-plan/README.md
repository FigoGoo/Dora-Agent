# code-plan 正式开发设计总入口

状态：production-design-ready  
owner：主控 Codex 汇总维护  
更新时间：2026-06-27  
适用范围：`services/agent/**`、`services/business/**`、`api/thrift/**`、`api/openapi/**`、`api/agui/**`、`db/migrations/iterations/**`、`tests/agent/**`、`tests/business/**`、`tests/contract/**`、`tests/e2e/**`  
相关代码路径：`services/agent/**`、`services/business/**`、`api/**`、`db/migrations/iterations/**`、`tests/**`  
相关设计契约：`docs/product/**`、`docs/product/prd/**`、`docs/design/**`、`docs/architecture/**`、`docs/standards/**`、`code-plan/agent/**`、`code-plan/business/**`、`code-plan/tests/**`

## 目标

本目录是 Dora-Agent 本次正式开发的工程事实源。目标是在不依赖现有临时代码、临时 schema、临时 fixture、临时 OpenAPI 或临时 RPC 文件的前提下，把 `docs/product/**` 和 `docs/design/**` 中已经 Done 的产品设计转成可直接开发、可测试、可验收的 Agent 微服务、业务微服务、跨服务契约、数据库、配置和测试设计。

本次开发不涉及前端开发，不补部署上线文档。

## 当前范围

| 范围 | 本次是否覆盖 | 说明 |
| --- | --- | --- |
| Agent 微服务 | 是 | Go + Eino Runtime、Agent API、AG-UI 事件生产、RPC client、Agent Runtime DB、事件补偿、测试。 |
| 业务微服务 | 是 | Kitex RPC server、业务 HTTP API 设计、业务数据库、事务、权限、审计、积分资产闭环、测试。 |
| RPC 契约 | 是 | Agent 与业务服务之间的 Thrift service、DTO、错误码、幂等、超时、重试和 contract fixture 设计。 |
| AG-UI 契约 | 是 | Agent 到前端的事件生产契约、schema、fixture、SSE replay 和 snapshot 设计。前端 reducer 不在本次范围。 |
| OpenAPI / HTTP API | 是 | 作为后续前端和管理端接入的服务端 DTO 契约设计。本次不实现前端页面。 |
| 数据库和 SQL | 是 | Agent DB 与业务 DB 分开建模、migration、seed、索引、无外键约束和数据库验证。 |
| 测试验收 | 是 | 服务级、契约级、AG-UI schema/replay、Agent DB、业务 DB、跨服务主链路和错误路径验证。 |
| 前端开发 | 否 | 不创建 `code-plan/frontend/**`，不实现页面、组件、状态管理、A2UI reducer 或浏览器 UI 自动化。 |
| 部署上线文档 | 否 | 不补 CI/CD、CentOS 部署步骤、SLO、告警、生产运行手册、发布回滚手册。 |

## 事实源读取顺序

1. `docs/product/AIGC智能体产品设计索引.md` 和 `docs/product/prd/**`。
2. `docs/product/工程交付输入清单产品系统设计.md`。
3. `docs/product/**` 中各产品系统设计。
4. `docs/design/**` 中页面、A2UI、AG-UI 和设计 token 文档，只作为 Agent/业务契约输入；本次不进入前端开发。
5. `docs/architecture/**`。
6. `docs/standards/**`。
7. `code-plan/agent/**`。
8. `code-plan/business/**`。
9. `code-plan/tests/**`。

现有代码、临时 schema、临时 fixture、临时 seed、临时 OpenAPI 或临时 RPC 文件如与 `code-plan` 不一致，以 `code-plan` 为准，在对应功能切片内先同步契约和测试，再写实现。

## 交付入口

| 入口 | owner | 用途 |
| --- | --- | --- |
| [agent/README.md](./agent/README.md) | Go Eino 智能体微服务架构工程师 | Agent 微服务架构、Eino Runtime、Agent API、AG-UI、RPC client、Agent DB、运行时测试。 |
| [business/README.md](./business/README.md) | 业务微服务后端工程师 | 业务领域、Kitex RPC、HTTP API、业务 DB、事务、权限、审计和业务测试。 |
| [tests/README.md](./tests/README.md) | 浏览器、RPC 与数据库测试工程师 | 非前端范围内的服务级、契约级、AG-UI、RPC、Agent DB、业务 DB 和跨服务验收。 |

## 开发顺序

| 阶段 | 输入 | 主要产出 | Done 条件 |
| --- | --- | --- | --- |
| M0 契约冻结 | Agent 04/06/07、业务 02/15、测试 00 | Thrift、OpenAPI、AG-UI schema、错误码、分页、幂等、fixture 设计冻结 | 契约字段、错误码、幂等键和 fixture 场景在 Agent、业务、测试三侧一致。 |
| M1 基础设施 | Agent 01/03/11、业务 01/02/14 | 服务骨架、配置、日志、Agent DB、业务 DB、migration、seed | 配置键、日志 trace、SQL up/down、无外键约束和基础测试路径明确。 |
| M2 身份项目能力 | 业务 03/04/05、Agent 04/05/07 | 登录空间、企业、管理员、项目、权限、Agent session/run 基础 | session/run 创建前能解析空间、校验项目、区分权限错误和归档状态。 |
| M3 配置能力 | 业务 06/07/08、Agent 02/08/12/13 | 模型、Tool、Skill、Skill 测试、安全证据、模型 Tool 适配 | Agent 只路由 Published Skill，只执行允许 Tool，安全评估先于预估和生成。 |
| M4 积分资产闭环 | 业务 09/10/11、Agent 09/10/13 | 预估、确认、冻结、生成、TOS 对象槽、保存、扣费、释放 | 资产保存成功才扣费；失败、取消、归档能释放冻结；DB 事实边界正确。 |
| M5 公开触达和通知 | 业务 12/13、Agent 06/10 | 作品公开快照、点赞、下架、站内信、AG-UI 展示事件 | 公开内容不泄露隐私，通知和公开状态由业务服务维护。 |
| M6 服务级验收 | 测试 00、Agent 14、业务 15 | RPC contract、HTTP contract、AG-UI replay、DB 验证、跨服务主链路报告 | 非前端、非部署范围内的阻断缺陷清零，未执行项有原因和风险说明。 |

## 全局 Done Gate

- [x] 产品文档 `product_status: Done`，正式开发计划不依赖 `docs/project/**`。
- [x] 本次范围明确排除前端开发和部署上线文档。
- [x] Agent、业务、测试三类 `code-plan` 入口齐全。
- [x] Agent 不直接访问或写入业务数据库。
- [x] 业务服务不保存 Agent Runtime 数据。
- [x] RPC 写操作有幂等键、错误码、超时、重试和 contract fixture 设计。
- [x] AG-UI 事件生产有 canonical type、payload、sequence、replay、snapshot 和 fixture 设计。
- [x] HTTP API 作为服务端 DTO 契约设计落回业务领域文档，不要求本次前端实现。
- [x] Agent DB 与业务 DB 分开建模，migration 不使用数据库级外键。
- [x] 测试计划覆盖服务级主链路、契约、DB、错误、权限、幂等和事件补偿。
- [x] 部署上线、CI/CD、告警、SLO、生产运行手册不作为本轮文档 Done Gate。

## 阻断规则

以下问题会阻断对应功能切片进入联调或验收：

- RPC / OpenAPI / AG-UI schema 与 `code-plan` 字段不一致。
- fixture 缺少正常、权限、业务错误、幂等冲突、超时或版本兼容场景。
- Agent 侧复制业务主数据，或业务侧保存 Agent session/run/message/event。
- 写操作缺少 `idempotency_key`、request hash 或审计字段。
- migration 出现数据库级外键或引用约束关键字。
- AG-UI 事件缺少 `event_id`、`sequence`、`trace_id` 或补偿路径。
- 测试报告把未执行项报告为通过。
