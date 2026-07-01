# 字段级事实源索引

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 核心重构 Contract-first active 化契约事实源索引  
来源 review：`docs/review/aigc-agent-refactor/2026-07-01/`

## 原则

```text
review 文档 = 设计依据
active PRD / 技术设计 = 解释依据
Thrift / OpenAPI / JSON Schema / SQL / fixture = 字段级事实源
代码实现 = 事实源消费者
```

每条契约必须回答：

1. 字段定义在哪里。
2. 谁拥有它。
3. 谁可以写。
4. 谁可以读。
5. 哪些 fixture 覆盖它。
6. 它来自哪个 review 裁决。

## PR-1 契约索引

| Contract | Version | Owner | Canonical File | Writers | Readers | Fixture Path | Source Review | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| StateEnumRegistry | v1 | 文档与契约责任域 | `api/schemas/common/state-enum-registry.schema.json` | 文档与契约 | Agent / Business / Frontend / Test | `tests/fixtures/contracts/**` | `01-M0` | active |
| Digest | v1 | 文档与契约责任域 | `api/schemas/common/digest.schema.json` | 文档与契约 | Agent / Business / Frontend / Test | `tests/fixtures/contracts/**` | `01-M0` | active |
| Error | v1 | 文档与契约责任域 | `api/schemas/common/error.schema.json` | 文档与契约 | Agent / Business / Frontend / Test | `tests/fixtures/contracts/**` | `01-M0` | active |
| RouterDecision | v1 | Agent Runtime | `api/schemas/router/router-decision.v1.schema.json` | Agent Runtime | Frontend / Business RPC mocks / Test | `tests/fixtures/contracts/router/` | `02-M1` | active |
| CreativeGuideOutput | v1 | Agent Runtime | `api/schemas/router/creative-guide-output.v1.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/router/` | `02-M1` | active |
| SkillCatalogSummary | v1 | Business Skill Catalog | `api/schemas/router/skill-catalog-summary.v1.schema.json` | Business Service | Agent Runtime / Test | `tests/fixtures/contracts/router/` | `02-M1` | active |
| AGUIEventEnvelope | v1 | Agent Runtime + Frontend | `api/agui/agent-workbench-events.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/agui/` | `01-M0` / `03-M2` | active |
| creative.guide.presented | v1 | Agent Runtime + Frontend | `api/agui/events/creative.guide.presented.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/agui/` | `02-M1` | active |
| creative.router.decided | v1 | Agent Runtime + Frontend | `api/agui/events/creative.router.decided.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/agui/` | `02-M1` | active |
| cost_disclosure.skill_usage.presented | v1 | Agent Runtime + Business Credit | `api/agui/events/cost_disclosure.skill_usage.presented.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/agui/` | `05-M4` / `06-M5` | active |
| confirmation.required | v1 | Agent Runtime + Frontend | `api/agui/events/confirmation.required.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/agui/` | `01-M0` / `05-M4` | active |
| JSONSchemaContractValidator | v1 | 文档与契约责任域 / 测试与验收 | `tests/contract/validate_json_schema_contracts.py`、`requirements/contract-gates.txt` | Test | CI / Agent / Business / Frontend | `api/schemas/**`、`api/agui/**`、`tests/fixtures/contracts/**` | `01-M0` / `08-M7` | active |

## PR-2 契约索引

| Contract | Version | Owner | Canonical File | Writers | Readers | Fixture Path | Source Review | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Agent Workbench OpenAPI | v1 | Agent Runtime | `api/openapi/agent-workbench.yaml` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/`、`tests/fixtures/contracts/graph/` | `02-M1` / `03-M2` | active |
| CreativeBoard | v1 | Agent Runtime | `api/schemas/board/creative-board.v1.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/create_city_tourism_board.json` | `03-M2` | active |
| CreativeElement | v1 | Agent Runtime | `api/schemas/board/creative-element.v1.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/create_city_tourism_board.json` | `03-M2` | active |
| BoardPatch | v1 | Agent Runtime | `api/schemas/board/board-patch.v1.schema.json` | Agent Runtime / Frontend | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/board/patch_replay_storyboard.json` | `03-M2` | active |
| BoardSnapshot | v1 | Agent Runtime | `api/schemas/board/board-snapshot.v1.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/patch_replay_storyboard.json` | `03-M2` | active |
| GraphTemplate | v1 | Agent Runtime | `api/schemas/graph/graph-template.v1.schema.json` | Agent Runtime | Agent Runtime / Test | `tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | `04-M3` | active |
| GraphPlan | v1 | Agent Runtime | `api/schemas/graph/graph-plan.v1.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | `04-M3` | active |
| GraphCheckpoint | v1 | Agent Runtime | `api/schemas/graph/graph-checkpoint.v1.schema.json` | Agent Runtime | Agent Runtime / Test | `tests/fixtures/contracts/graph/interrupt_resume_checkpoint.json` | `04-M3` | active |
| GenericCreationGraph | v1 | Agent Runtime | `api/schemas/graph/generic-creation-graph.v1.schema.json` | Agent Runtime | Router / Test | `tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | `04-M3` / `07-M6` | active |
| board.patch.applied | v1 | Agent Runtime + Frontend | `api/agui/events/board.patch.applied.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/patch_replay_storyboard.json` | `03-M2` | active |
| board.snapshot.updated | v1 | Agent Runtime + Frontend | `api/agui/events/board.snapshot.updated.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/board/patch_replay_storyboard.json` | `03-M2` | active |
| graph.plan.created | v1 | Agent Runtime + Frontend | `api/agui/events/graph.plan.created.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | `04-M3` | active |
| graph.node.updated | v1 | Agent Runtime + Frontend | `api/agui/events/graph.node.updated.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | `04-M3` | active |
| Agent Runtime Migration | v1 | Agent Runtime | `db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/**` | Agent Runtime | Agent Runtime / Test | `tests/contract/validate_board_graph_contracts.py` | `03-M2` / `04-M3` | active |

## PR-3 契约索引

| Contract | Version | Owner | Canonical File | Writers | Readers | Fixture Path | Source Review | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ToolPlan | v1 | Agent Runtime | `api/schemas/tool/tool-plan.v1.schema.json` | Agent Runtime | Frontend / Business Credit / Test | `tests/fixtures/contracts/toolplan/city_video_toolplan.json` | `05-M4` | active |
| ToolTask | v1 | Agent Runtime / Tool Runtime | `api/schemas/tool/tool-task.v1.schema.json` | Agent Runtime / Worker | Frontend / Test | `tests/fixtures/contracts/tool/provider_async_resume.json` | `05-M4` | active |
| ToolResult | v1 | Agent Runtime / Tool Runtime | `api/schemas/tool/tool-result.v1.schema.json` | Worker | Business Asset / Test | `tests/fixtures/contracts/asset/partial_commit_success.json` | `05-M4` | active |
| CreditFreeze | v1 | Business Credit | `api/schemas/credit/credit-freeze.v1.schema.json` | Business Credit | Agent Runtime / Test | `tests/fixtures/contracts/credit/**` | `05-M4` | active |
| GeneratedAsset | v1 | Business Asset | `api/schemas/asset/generated-asset.v1.schema.json` | Business Asset | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/asset/partial_commit_success.json` | `05-M4` | active |
| cost_disclosure.generation.presented | v1 | Agent Runtime + Business Credit | `api/agui/events/cost_disclosure.generation.presented.schema.json` | Agent Runtime | Frontend / Test | `tests/fixtures/contracts/toolplan/city_video_toolplan.json` | `05-M4` | active |
| tool.task.updated | v1 | Agent Runtime + Frontend | `api/agui/events/tool.task.updated.schema.json` | Agent Runtime / Worker | Frontend / Test | `tests/fixtures/contracts/tool/provider_async_resume.json` | `05-M4` | active |
| asset.commit.updated | v1 | Agent Runtime + Frontend | `api/agui/events/asset.commit.updated.schema.json` | Agent Runtime / Business Asset | Frontend / Test | `tests/fixtures/contracts/asset/partial_commit_success.json` | `05-M4` | active |
| BusinessCreditService | v1 | Business Credit | `api/thrift/business_credit_service.thrift` | Business Credit | Agent Runtime / Test | `tests/fixtures/contracts/credit/**` | `05-M4` | active |
| BusinessAssetService | v1 | Business Asset | `api/thrift/business_asset_service.thrift` | Business Asset | Agent Runtime / Test | `tests/fixtures/contracts/asset/**` | `05-M4` | active |
| BusinessToolService | v1 | Business Tool | `api/thrift/business_tool_service.thrift` | Business Tool | Agent Runtime / Test | `tests/fixtures/contracts/toolplan/**` | `05-M4` | active |
| Tool/Credit/Asset Migration | v1 | Agent Runtime / Business Service | `db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/**` | Agent Runtime / Business Service | Test | `tests/contract/validate_tool_asset_contracts.py` | `05-M4` | active |

## PR-4 契约索引

| Contract | Version | Owner | Canonical File | Writers | Readers | Fixture Path | Source Review | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Business Marketplace API | v1 | Business Skill Marketplace | `api/openapi/business-api.yaml` | Business Service | Frontend / Test | `tests/fixtures/contracts/marketplace/**` | `06-M5` / `07-M6` | active |
| Creator Skill API | v1 | Business Skill Marketplace | `api/openapi/creator-api.yaml` | Business Service | Creator Portal / Test | `tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json` | `06-M5` / `07-M6` | active |
| Admin Skill Governance API | v1 | Admin Governance | `api/openapi/admin-api.yaml` | Admin Service | Admin Frontend / Test | `tests/fixtures/contracts/marketplace/**`、`tests/fixtures/contracts/billing/**` | `06-M5` / `10` | active |
| SkillPackage | v1 | Business Skill Marketplace | `api/schemas/skill/skill-package.v1.schema.json` | Business Service | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json` | `06-M5` | active |
| SkillVersion | v1 | Business Skill Marketplace | `api/schemas/skill/skill-version.v1.schema.json` | Business Service | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json` | `06-M5` | active |
| SkillPricingPolicy | v1 | Business Skill Marketplace / Credit | `api/schemas/skill/skill-pricing-policy.v1.schema.json` | Business Service | Agent Runtime / Business Credit / Test | `tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json` | `06-M5` | active |
| MarketplaceListing | v1 | Business Skill Marketplace | `api/schemas/skill/marketplace-listing.v1.schema.json` | Business Service / Admin | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json` | `06-M5` | active |
| SkillInstallation | v1 | Business Skill Marketplace | `api/schemas/skill/skill-installation.v1.schema.json` | Business Service | Agent Runtime / Frontend / Test | `tests/fixtures/contracts/marketplace/user_install_latest_personal.json`、`tests/fixtures/contracts/marketplace/enterprise_install_pinned_upgrade.json` | `06-M5` | active |
| SkillUsageRecord | v1 | Business Skill Marketplace / Credit | `api/schemas/skill/skill-usage-record.v1.schema.json` | Business Service | Agent Runtime / Admin / Test | `tests/fixtures/contracts/billing/skill_usage_precreate_confirm_charge.json` | `06-M5` | active |
| SkillSettlement | v1 | Business Settlement | `api/schemas/settlement/skill-settlement.v1.schema.json` | Business Settlement | Creator / Admin / Test | `tests/fixtures/contracts/billing/**` | `06-M5` / `10` | active |
| BusinessSkillMarketplaceService | v1 | Business Skill Marketplace | `api/thrift/business_skill_marketplace_service.thrift` | Business Service | Agent Runtime / Test | `tests/fixtures/contracts/marketplace/**`、`tests/fixtures/contracts/billing/**` | `06-M5` | active |
| BusinessSettlementService | v1 | Business Settlement | `api/thrift/business_settlement_service.thrift` | Business Settlement | Business Marketplace / Admin / Test | `tests/fixtures/contracts/billing/**` | `06-M5` / `10` | active |
| Marketplace Migration | v1 | Business Service | `db/migrations/iterations/2026-07-01-marketplace-contracts/business/**` | Business Service | Test | `tests/contract/validate_skill_market_contracts.py` | `06-M5` / `10` | active |

## PR-5 契约索引

| Contract | Version | Owner | Canonical File | Writers | Readers | Fixture Path | Source Review | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| FakeProviderManifest | v1 | 测试与验收 / Tool Runtime | `tests/e2e/fake-provider/fake_provider_manifest.json` | Test | Agent / Tool Runtime / Test | `tests/e2e/fake-provider/provider_scenarios.json` | `07-M6` / `08-M7` | active |
| FakeProviderScenarios | v1 | 测试与验收 / Tool Runtime | `tests/e2e/fake-provider/provider_scenarios.json` | Test | Agent / Tool Runtime / Test | `tests/e2e/fake-provider/**` | `07-M6` / `08-M7` | active |
| AgentWorkspaceE2ESuite | v1 | 测试与验收 / Agent Runtime / 前端 | `tests/e2e/agent-workspace/scenarios.json` | Test | Agent / Frontend / Test | `tests/fixtures/e2e/agent-workspace/**` | `07-M6` | active |
| SkillMarketplaceE2ESuite | v1 | 测试与验收 / Business Service / 前端 | `tests/e2e/skill-marketplace/scenarios.json` | Test | Business / Frontend / Test | `tests/fixtures/e2e/skill-marketplace/**` | `06-M5` / `07-M6` | active |
| AdminGovernanceE2ESuite | v1 | 测试与验收 / 管理端 / Business Service | `tests/e2e/admin-governance/scenarios.json` | Test | Admin / Business / Test | `tests/fixtures/e2e/admin-governance/**` | `07-M6` / `10` | active |
| PR5ServiceLevelE2E | v1 | 测试与验收 / Business Service | `services/business/internal/e2e/release/service_e2e_test.go` | Test | CI / Agent / Business / Release | `tests/fixtures/contracts/**`、`tests/fixtures/e2e/**` | `07-M6` / `08-M7` | active |
| PR5AgentHTTPRedisE2E | v1 | 测试与验收 / Agent Runtime | `services/agent/internal/e2e/release/agent_http_redis_e2e_test.go` | Test | CI / Agent / Release | `tests/fixtures/contracts/**`、`tests/fixtures/e2e/**` | `07-M6` / `08-M7` | active |
| PR5AgentProcessSmoke | v1 | 测试与验收 / Agent Runtime | `services/agent/internal/e2e/release/agent_process_smoke_test.go` | Test | CI / Agent / Release | `db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/**` | `08-M7` | active |
| PR5BusinessProcessSmoke | v1 | 测试与验收 / Business Service | `services/business/internal/e2e/release/business_process_smoke_test.go` | Test | CI / Business / Release | `db/migrations/iterations/2026-06-27-business-core/business/**` | `08-M7` | active |
| PR5FullHTTPServiceSmoke | v1 | 测试与验收 / Agent Runtime / Business Service | `services/agent/internal/e2e/release/full_http_service_smoke_test.go`、`scripts/validate-release-full-http-smoke.sh` | Test | CI / Agent / Business / Release | `db/migrations/iterations/2026-06-27-business-core/business/**`、`db/migrations/iterations/20260627_agent_runtime/agent/**`、`tests/business/seed/business_core_seed.sql` | `08-M7` | active |
| PR5BrowserSmoke | v1 | 测试与验收 / 前端 / 管理端 | `scripts/validate-release-browser-smoke.sh`、`tests/e2e/browser/release-frontend-browser-smoke.mjs` | Test | CI / Frontend / Admin / Release | `frontend/**`、`admin_frontend/**` | `07-M6` / `08-M7` | active |
| RedisTestHarness | v1 | 测试与验收 / 工程基础设施 | `internal/testredis/redis.go` | Test | CI / Agent / Business | `services/agent/internal/e2e/release/**` | `08-M7` | active |
| ReleaseGovernance | v1 | 运维发布 / 文档与契约 / 测试验收 | `docs/active/technical/release-governance.md` | 运维发布 / 文档与契约 | Agent / Business / Frontend / Test | `tests/contract/validate_release_e2e_gates.py` | `08-M7` | active |
| PR5ReleaseGateValidator | v1 | 测试与验收 | `tests/contract/validate_release_e2e_gates.py` | Test | CI / Agent / Business / Frontend | `tests/e2e/**`、`tests/fixtures/e2e/**` | `08-M7` | active |

## P0 Contract Delta

| P0 | Canonical Contract | Active 口径 |
| --- | --- | --- |
| SkillUsageRecord 创建时机和状态流转 | PR-4：`api/schemas/skill/skill-usage-record.v1.schema.json`、`api/thrift/business_skill_marketplace_service.thrift`、business migration、billing fixture | `EstimateSkillUsageCredits -> CreateSkillUsageRecord(status=confirmation_required) -> Freeze -> Running -> ValueDelivered -> CommitAndSettle` |
| Skill 使用费确认与 Tool 生成费确认的两阶段 CostDisclosure | PR-1 / PR-3 / PR-4：AG-UI 事件和 billing/tool fixtures | `cost_disclosure.skill_usage.presented` 发生在付费 Skill Graph 执行前；`cost_disclosure.generation.presented` 已由 PR-3 冻结，发生在 Board approved 且 ToolPlan 完成预估后 |
| 创作者端 Skill 发布后台页面和验收 | PR-4：`api/openapi/creator-api.yaml`、Creator DTO、E2E fixture | 创作者后台只看草稿、审核、listing、分析、结算和脱敏错误摘要 |
| 用户端 Skill 市场前台页面和安装/使用路径 | PR-4：business API、Marketplace DTO、installation fixture | 市场首页、详情页、安装、workspace 候选抽屉、已安装页和工作台使用路径分开验收 |
| Skill installation 版本策略和升级规则 | PR-4：SkillInstallation schema、business RPC、SQL、fixture | 个人默认 `latest_published`；企业默认 `pinned`；升级需确认；历史 run 永远按 snapshot 恢复 |
| Generic Creation Graph 平台内置 L0 fallback 正式 spec | PR-2：`api/schemas/graph/generic-creation-graph.v1.schema.json`、`tests/fixtures/contracts/graph/generic_creation_graph_plan.json` | 不进市场，Skill 使用费 0，只做 brief、澄清、方向、提示词草稿和 Skill 推荐，不默认调用生成 Tool |

## 后续 PR 拆分

| PR | 独立文档 | 产物 | Done Gate |
| --- | --- | --- | --- |
| PR-1 | [`pr-1-contract-index-state-router-agui.md`](./pr-1-contract-index-state-router-agui.md) | Contract index、common schema、router schema、AG-UI envelope、第一批 fixtures | schema validate、RouterDecision fixture、AG-UI envelope fixture |
| PR-2 | [`pr-2-agent-runtime-contracts.md`](./pr-2-agent-runtime-contracts.md) | Agent Runtime contracts、Board、Graph、Agent DB migration | Board patch / replay fixture、GraphPlan digest fixture、agent migration dry-run |
| PR-3 | [`pr-3-tool-credit-asset-contracts.md`](./pr-3-tool-credit-asset-contracts.md) | Tool / Credit / Asset contracts | ToolPlan digest、freeze/commit/release 幂等、partial success fixture |
| PR-4 | [`pr-4-marketplace-contracts.md`](./pr-4-marketplace-contracts.md) | Marketplace contracts | SkillVersion / Listing 双状态机、SkillUsageRecord 预创建、settlement hold、data visibility |
| PR-5 | [`pr-5-e2e-fixtures-release-gates.md`](./pr-5-e2e-fixtures-release-gates.md) | E2E fixtures、fake provider、M7 gates、service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke、测试环境 HTTP 服务 E2E 自动化入口和报告模板 | 默认 Skill、付费市场 Skill、企业安装、partial failure、listing suspended、refund、worker replay、Redis AG-UI replay E2E、Agent / Business main 启动 smoke、双服务 HTTP smoke、用户端 / 管理端 browser smoke、测试环境 HTTP E2E |
