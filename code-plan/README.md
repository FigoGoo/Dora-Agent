# 第一阶段服务端开发设计归档入口

状态：archived  
owner：文档与契约责任域
更新时间：2026-06-28  
适用范围：`services/agent/**`、`services/business/**`、`api/thrift/**`、`api/openapi/**`、`api/agui/**`、`db/migrations/iterations/**`、`tests/agent/**`、`tests/business/**`、`tests/contract/**`、`tests/e2e/**`  
相关代码路径：`services/agent/**`、`services/business/**`、`api/**`、`db/migrations/iterations/**`、`tests/**`  
相关设计契约：`docs/product/**`、`docs/product/prd/**`、`docs/design/**`、`docs/architecture/**`、`docs/standards/**`、`code-plan/agent/**`、`code-plan/business/**`、`code-plan/tests/**`

## 归档说明

第一阶段服务端开发已经完成。本目录保留第一阶段服务端设计、契约映射、测试设计和历史验收口径，仅用于追溯，不再作为后续迭代的当前事实源。

后续功能迭代、前端开发、发布上线或契约变更，必须先从 `docs/current/README.md` 进入，并更新 `docs/technical/**`、`docs/contracts/**`、`docs/standards/**`、`docs/product/**` 或 `docs/test/**` 中的 active 文档。

## 目标

本目录记录 Dora-Agent 第一阶段服务端开发的工程设计基线。它把 `docs/product/**` 和 `docs/design/**` 中已经 Done 的产品设计转成 Agent 微服务、业务微服务、跨服务契约、数据库、配置和测试设计。

当前不再在本目录新增阶段计划、任务拆分或迭代要求。

## 历史范围

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

本节仅用于追溯第一阶段服务端开发期间的历史读取顺序。新任务不要按本顺序读取，必须改读 `docs/current/README.md`。

1. `docs/product/AIGC智能体产品设计索引.md` 和 `docs/product/prd/**`。
2. `docs/product/工程交付输入清单产品系统设计.md`。
3. `docs/product/**` 中各产品系统设计。
4. `docs/design/**` 中页面、A2UI、AG-UI 和设计 token 文档。
5. `docs/architecture/**`。
6. `docs/standards/**`。
7. `code-plan/agent/**`。
8. `code-plan/business/**`。
9. `code-plan/tests/**`。

## 交付入口

| 入口 | 责任域 | 用途 |
| --- | --- | --- |
| [agent/README.md](./agent/README.md) | Agent 服务责任域 | Agent 微服务架构、Eino Runtime、Agent API、AG-UI、RPC client、Agent DB、运行时测试。 |
| [business/README.md](./business/README.md) | 业务服务责任域 | 业务领域、Kitex RPC、HTTP API、业务 DB、事务、权限、审计和业务测试。 |
| [tests/README.md](./tests/README.md) | 测试与验收责任域 | 非前端范围内的服务级、契约级、AG-UI、RPC、Agent DB、业务 DB 和跨服务验收。 |

## 归档使用规则

- 新任务默认不读取本目录。
- 需要追溯第一阶段服务端设计时，可按 `agent/README.md`、`business/README.md`、`tests/README.md` 定位历史设计。
- 如果历史设计仍然有效，应迁移或摘录到 `docs/technical/**`、`docs/contracts/**`、`docs/standards/**` 或 `docs/test/**` 的 active 文档。
- 如果历史设计已经失效，应标记替代文档或移入 `docs/archive/**`。

## 历史交付口径

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

- RPC / OpenAPI / AG-UI schema 与当前契约字段不一致。
- fixture 缺少正常、权限、业务错误、幂等冲突、超时或版本兼容场景。
- Agent 侧复制业务主数据，或业务侧保存 Agent session/run/message/event。
- 写操作缺少 `idempotency_key`、request hash 或审计字段。
- migration 出现数据库级外键或引用约束关键字。
- AG-UI 事件缺少 `event_id`、`sequence`、`trace_id` 或补偿路径。
- 测试报告把未执行项报告为通过。
