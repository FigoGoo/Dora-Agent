# 契约文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 核心重构契约文档导航

## 当前状态

旧 RPC、API、AG-UI、Agent 数据模型和 SQL 契约文档已从当前仓库文档事实源清理，避免污染本轮契约冻结。

本轮 review 已完成并通过，已完成 M7 active 契约拆分。PR-1 / PR-2 / PR-3 / PR-4 契约事实源和 PR-5 E2E / release gate 事实源已冻结，索引见：

- `docs/active/contracts/index.md`
- `docs/contracts/字段级契约索引.md`

PR-1 / PR-2 / PR-3 / PR-4 / PR-5 当前范围：

- `api/schemas/common/**`
- `api/schemas/router/**`
- `api/schemas/board/**`
- `api/schemas/graph/**`
- `api/schemas/tool/**`
- `api/schemas/credit/**`
- `api/schemas/asset/**`
- `api/schemas/skill/**`
- `api/schemas/settlement/**`
- `api/thrift/business_credit_service.thrift`
- `api/thrift/business_asset_service.thrift`
- `api/thrift/business_tool_service.thrift`
- `api/thrift/business_skill_marketplace_service.thrift`
- `api/thrift/business_settlement_service.thrift`
- `api/openapi/agent-workbench.yaml`
- `api/openapi/business-api.yaml`
- `api/openapi/creator-api.yaml`
- `api/openapi/admin-api.yaml`
- `api/agui/agent-workbench-events.schema.json`
- `api/agui/events/**`
- `db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/**`
- `db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/**`
- `db/migrations/iterations/2026-07-01-marketplace-contracts/business/**`
- `tests/fixtures/contracts/router/**`
- `tests/fixtures/contracts/agui/**`
- `tests/fixtures/contracts/board/**`
- `tests/fixtures/contracts/graph/**`
- `tests/fixtures/contracts/toolplan/**`
- `tests/fixtures/contracts/credit/**`
- `tests/fixtures/contracts/asset/**`
- `tests/fixtures/contracts/tool/**`
- `tests/fixtures/contracts/marketplace/**`
- `tests/fixtures/contracts/billing/**`
- `tests/e2e/fake-provider/**`
- `tests/e2e/agent-workspace/**`
- `tests/e2e/skill-marketplace/**`
- `tests/e2e/admin-governance/**`
- `tests/e2e/browser/**`
- `tests/fixtures/e2e/**`
- `docs/active/technical/release-governance.md`
- `tests/contract/validate_pr1_contracts.py`
- `tests/contract/validate_pr2_contracts.py`
- `tests/contract/validate_pr3_contracts.py`
- `tests/contract/validate_pr4_contracts.py`
- `tests/contract/validate_pr5_e2e_gates.py`

## 需要重建的契约

- 业务 RPC：`api/thrift/**`、`docs/active/contracts/index.md`
- Agent API：`api/openapi/**`、`docs/active/contracts/index.md`
- 业务 API：`api/openapi/**`、`docs/active/contracts/index.md`
- AG-UI：`api/agui/**`、`docs/active/contracts/index.md`
- JSON Schema：`api/schemas/**`、`docs/active/contracts/index.md`
- SQL 清单：`db/migrations/**`、`docs/active/contracts/index.md`
- Fixture：`tests/fixtures/contracts/**`

## 使用规则

- 字段级事实源必须由 Thrift、OpenAPI、AG-UI JSON Schema、migration 或 fixture 承接。
- 新增或变更契约后，必须同步测试入口和实现状态。
- 本次重构之前的旧契约不在当前读取链路中；如需追溯只能通过 git 历史查询，不能直接指导新实现。
