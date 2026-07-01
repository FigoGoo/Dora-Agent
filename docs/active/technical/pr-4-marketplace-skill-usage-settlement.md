# PR-4 Marketplace / SkillUsage / Settlement 基础实现

状态：active  
owner：Business Skill Marketplace / Business Credit / Agent Runtime / 前端 / 管理端 / 文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：PR-4 字段级契约在 Go 运行时的 Skill 发布、市场上架、安装升级、SkillUsageRecord、结算、退款、数据隔离、Business repository、应用层、用户端 Marketplace HTTP 主路径、真实 Marketplace RPC adapter 和用户端 Skill 市场前台实现
相关代码路径：`internal/contracts/pr4/**`、`services/business/internal/application/marketplace/**`、`services/business/internal/infra/repository/businesscore/**`、`services/business/internal/transport/http/**`、`services/business/internal/transport/rpc/**`、`kitex_gen/dora/api/businessskillmarketplace/**`、`frontend/src/lib/api/marketplace.js`、`frontend/src/features/skills/**`
相关契约：`docs/active/contracts/pr-4-marketplace-contracts.md`、`api/schemas/skill/**`、`api/schemas/settlement/**`、`api/agui/events/cost_disclosure.skill_usage.presented.schema.json`

## 背景

PR-4 已冻结开放 Skill 市场闭环。实现前需要先把 SkillUsageRecord 创建时机、usage / charge / refund / settlement 独立状态、安装版本策略和创作者数据隔离做成共享 Go 契约，避免后续页面、RPC 调用和结算逻辑产生事实源分叉。

## 目标

- 提供 SkillPackage、SkillVersion、SkillPricingPolicy、MarketplaceListing Go 类型和校验。
- 提供 SkillInstallation、个人 latest 安装、企业 pinned 升级确认和历史 run snapshot 校验。
- 提供 SkillUsageRecord、SkillSettlement、预创建/冻结/扣费/退款反转状态机校验。
- 提供 Skill 使用费 CostDisclosure AG-UI payload 校验，并复用 PR-1 AG-UI Envelope。
- 提供创作者 API 数据隔离 fixture 校验。

## 非目标

- 不修改 PR-1 AG-UI Envelope。
- 不修改 PR-3 Tool 生成费扣费规则。
- 不做真实收益出账。
- 不实现创作者后台页面或管理端页面。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Marketplace | `internal/contracts/pr4/marketplace.go` | Skill 包、版本、定价、listing、installation 类型和校验 |
| Billing | `internal/contracts/pr4/billing.go` | SkillUsageRecord、SkillSettlement 和状态流转校验 |
| AG-UI | `internal/contracts/pr4/agui.go` | Skill 使用费 CostDisclosure payload |
| Visibility | `internal/contracts/pr4/visibility.go` | 创作者 API 安全摘要和私有字段泄露守卫 |
| Tests | `internal/contracts/pr4/*_test.go` | 读取 PR-4 active fixture 做漂移防护 |
| Business Repository | `services/business/internal/infra/repository/businesscore/pr4_marketplace.go` | 发布、安装、升级、usage 预创建、扣费结算、退款反转事务与幂等 |
| Business Application | `services/business/internal/application/marketplace/app.go` | Marketplace 列表/详情、安装、升级、usage 估算、预创建、冻结、交付扣费和 settlement hold |
| User HTTP | `services/business/internal/transport/http/handlers_work_notification_marketplace.go` | `/api/marketplace/skills`、详情、安装、升级、已安装列表 |
| Business RPC | `services/business/internal/transport/rpc/handlers_marketplace.go`、`kitex_gen/dora/api/businessskillmarketplace/**` | `BusinessSkillMarketplaceService` 查询、安装、升级、usage 估算、预创建、冻结、扣费结算和释放冻结 |
| User Frontend | `frontend/src/lib/api/marketplace.js`、`frontend/src/features/skills/SkillsPage.jsx` | 用户端市场列表、搜索筛选、安装登录门、个人安装和已安装后使用入口 |
| Migration Tests | `services/business/internal/infra/repository/businesscore/pr4_marketplace_integration_test.go` | PR-4 business migration up/down、无外键、fixture 状态机验证 |

## 开发注意事项

1. 付费市场 Skill 必须先 `CreateSkillUsageRecord(status=confirmation_required, charge_status=not_frozen)`，再展示 Skill 使用费确认。
2. Skill 使用费确认和 Tool 生成费确认是两阶段 CostDisclosure；PR-4 不得替代 PR-3 Tool 生成费确认。
3. 个人安装默认 `latest_published`；企业安装默认 `pinned`，升级必须确认。
4. 历史 run 必须使用启动时的 skill version snapshot 恢复。
5. 创作者 API 只能返回聚合摘要，不得返回用户 prompt、私有 board、生成资产、对话或 provider 原始 payload。

## Done Gate

- [x] `internal/contracts/pr4` 包存在。
- [x] 创作者提交、审核、发布 fixture 校验通过。
- [x] 个人 latest 安装 fixture 校验通过。
- [x] 企业 pinned 升级确认和历史 snapshot fixture 校验通过。
- [x] SkillUsageRecord 预创建、冻结、交付扣费 fixture 校验通过。
- [x] 退款反转和 settlement reverse fixture 校验通过。
- [x] 创作者数据隔离 fixture 校验通过。
- [x] Skill 使用费 AG-UI payload 可使用 PR-1 Envelope 构造并校验。
- [x] PR-4 Business PostgreSQL migration dry-run 和 down-test 已由 repository integration test 覆盖。
- [x] Marketplace / SkillUsage / Settlement repository 已支持发布、安装、升级、预创建、扣费结算、退款反转和幂等重放。
- [x] Marketplace 应用层已支持用户发现、详情、个人 latest 安装、已安装列表、usage 预创建、确认后冻结、交付扣费和 settlement hold。
- [x] 用户端 Marketplace HTTP 主路径已接入 `business-api.yaml` 已冻结路径。
- [x] 真实 Marketplace RPC adapter 已接入 `BusinessSkillMarketplaceService` 并覆盖 usage 创建、冻结、扣费和释放冻结。
- [x] 用户端 Skill 市场前台已接入市场列表、安装登录门、个人安装和已安装后使用入口。
- [ ] 后续 PR 接入创作者后台、管理端页面和结算出账治理。

## 验证命令

```bash
go test ./internal/contracts/pr4
go test ./services/business/internal/application/marketplace
go test ./internal/contracts/pr4 ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/transport/rpc
go test ./services/business/internal/transport/http
make active-contract-gate
make pr0-ci-gate
npm test --prefix frontend
npm run build --prefix frontend
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/pr4
go test ./services/business/internal/application/marketplace
go test ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/transport/rpc
go test ./services/business/internal/transport/http
go test ./internal/contracts/pr4 ./services/business/internal/infra/repository/businesscore
make active-contract-gate
make pr0-ci-gate
```

2026-07-01 Marketplace RPC adapter 接入后已执行：

```bash
go test ./services/business/internal/application/marketplace ./services/business/internal/infra/repository/businesscore ./services/business/internal/transport/rpc ./services/business/internal/bootstrap
```

2026-07-01 用户端 Skill 市场前台接入后已执行：

```bash
npm test --prefix frontend
npm run build --prefix frontend
make active-contract-gate
make pr0-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/pr4
ok github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
ok github.com/FigoGoo/Dora-Agent/services/business/internal/transport/http
ok github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc
ok github.com/FigoGoo/Dora-Agent/services/business/internal/bootstrap
active contract gate passed
PR-0 CI gate passed
```
