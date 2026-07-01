# 技术文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 核心重构技术文档导航

## 当前状态

旧技术文档已从当前仓库文档事实源清理，避免污染本轮 Agent 核心重构。

本轮 review 已完成并通过，已完成 M7 active 契约拆分。技术设计仍按 Contract-first 执行：M0 / PR-1 active 最小契约已冻结，PR-2 Agent Runtime Contracts、PR-3 Tool/Credit/Asset Contracts、PR-4 Marketplace Contracts 字段级契约已冻结，PR-5 E2E Fixtures + Fake Provider + Release Gates 已冻结。

当前已通过 review 的分阶段设计文档：

- `docs/review/aigc-agent-refactor/2026-07-01/`

该目录作为本次 active 拆分来源，不直接作为研发编码事实源；字段级事实源以 `docs/active/**`、`api/schemas/**`、`api/agui/**`、`api/openapi/**`、`api/thrift/**`、`db/migrations/**`、`tests/fixtures/**` 和 `tests/e2e/**` 为准。

## 建议新建位置

- 架构设计：`docs/technical/architecture/`
- 后端设计：`docs/technical/backend/features/`
- 前端设计：`docs/technical/frontend/features/`
- 功能迭代：`docs/technical/iterations/`
- CI/CD：`docs/technical/cicd/`

## 使用规则

- 新文档必须标注状态、owner、更新时间、适用范围、相关代码路径和相关契约。
- 涉及 RPC、API、AG-UI、数据模型或 SQL 时，先同步 `docs/contracts/**`。
- 本次重构之前的历史设计副本不在当前读取链路中；如需追溯只能通过 git 历史查询。
