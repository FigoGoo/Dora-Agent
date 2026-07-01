# Active 事实源入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 核心重构 Contract-first active 化契约事实源导航

## 当前阶段

本目录承接 `docs/review/aigc-agent-refactor/2026-07-01/` 已通过的 review 结论，并按 Contract-first 方式建立 active 契约事实源。

当前状态：

```text
M0 / PR-1 active 最小契约已冻结
PR-2 Agent Runtime Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-3 Tool/Credit/Asset Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-4 Marketplace Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-5 E2E Fixtures + Fake Provider + Release Gates 已冻结，本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 已完成，真实浏览器、前端联动和完整测试环境 HTTP 服务 E2E 待 CI / 测试环境 gate
PR-1 到 PR-5 active 拆分已完成
PR-0 开发准备与 CI Gate 已启动
M1-M6 业务代码开发按 PR-1 到 PR-5 顺序受控推进
```

PR-1 active 范围：

```text
Contract index
  -> StateEnumRegistry
  -> common schemas
  -> RouterDecision schemas
  -> AG-UI envelope
  -> 第一批 router / AG-UI fixtures
```

## 事实源分层

| 层级 | 作用 | 字段事实源 |
| --- | --- | --- |
| review 文档 | 设计依据和裁决来源 | 否 |
| active PRD / 技术设计 | 用户流程、架构、边界和验收解释 | 否 |
| Thrift / OpenAPI / JSON Schema / SQL / fixture | 字段、状态、请求响应和验收样例 | 是 |
| 代码实现 | 事实源消费者 | 否 |

## 当前入口

| 类型 | 路径 |
| --- | --- |
| 契约索引 | `docs/active/contracts/index.md` |
| PR 路线图 | `docs/active/contracts/pr-roadmap.md` |
| PR-1 拆分 | `docs/active/contracts/pr-1-contract-index-state-router-agui.md` |
| PR-2 拆分 | `docs/active/contracts/pr-2-agent-runtime-contracts.md` |
| PR-3 拆分 | `docs/active/contracts/pr-3-tool-credit-asset-contracts.md` |
| PR-4 拆分 | `docs/active/contracts/pr-4-marketplace-contracts.md` |
| PR-5 拆分 | `docs/active/contracts/pr-5-e2e-fixtures-release-gates.md` |
| PR-0 CI Gate | `docs/active/technical/pr-0-development-ci-gate.md` |
| 字段命名规范 | `docs/active/contracts/field-naming-standard.md` |
| 状态枚举说明 | `docs/active/contracts/state-enum-registry.md` |
| 数据所有权 | `docs/active/contracts/data-ownership.md` |
| 兼容规则 | `docs/active/contracts/compatibility-rules.md` |
| JSON Schema | `api/schemas/**` |
| AG-UI Schema | `api/agui/**` |
| Contract Fixtures | `tests/fixtures/contracts/**` |
| E2E / Fake Provider | `tests/e2e/**`、`tests/fixtures/e2e/**` |
| Release Governance | `docs/active/technical/release-governance.md` |

## 使用规则

1. 研发不得直接按 review 文档字段编码。
2. 字段、状态、事件和 fixture 以本目录索引指向的 canonical file 为准。
3. 新增字段必须同步 schema、fixture 和契约索引。
4. 写 RPC、扣费、冻结、释放、确认类能力必须有幂等键和 trace。
5. AG-UI 事件必须支持同一 run 内 `seq` 单调递增和 `dedupe_key` 去重。
6. 真实 provider 流量必须等待 PR-5 fake provider、service-level E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、真实浏览器 / 前端联动 / 完整测试环境 HTTP 服务 E2E 和 release gate 在测试环境通过。
7. 本地开发默认先运行 `make active-contract-gate`；提交前运行 `make pr0-ci-gate`。
