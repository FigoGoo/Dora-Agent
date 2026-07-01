# PR-4 Marketplace / SkillUsage / Settlement 基础实现

状态：active  
owner：Business Skill Marketplace / Business Credit / Agent Runtime / 前端 / 管理端 / 文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：PR-4 字段级契约在 Go 运行时的 Skill 发布、市场上架、安装升级、SkillUsageRecord、结算、退款、内部出账治理、数据隔离、Business repository、应用层、用户端 Marketplace HTTP 主路径、Creator Portal HTTP 主路径、Admin Governance HTTP 主路径、真实 Marketplace RPC adapter、用户端 Skill 市场前台、创作者发布后台和管理端治理页面实现
相关代码路径：`internal/contracts/skillmarket/**`、`services/business/internal/application/marketplace/**`、`services/business/internal/infra/repository/businesscore/**`、`services/business/internal/transport/http/**`、`services/business/internal/transport/rpc/**`、`kitex_gen/dora/api/businessskillmarketplace/**`、`frontend/src/lib/api/marketplace.js`、`frontend/src/lib/api/creator.js`、`frontend/src/features/skills/**`、`admin_frontend/src/features/resources/pageConfigs.jsx`、`admin_frontend/src/layout/AdminShell.jsx`
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
- 不接外部打款、支付供应商或真实资金出账；本阶段只做内部 settlement ledger 治理与人工确认记录。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Marketplace | `internal/contracts/skillmarket/marketplace.go` | Skill 包、版本、定价、listing、installation 类型和校验 |
| Billing | `internal/contracts/skillmarket/billing.go` | SkillUsageRecord、SkillSettlement 和状态流转校验 |
| AG-UI | `internal/contracts/skillmarket/agui.go` | Skill 使用费 CostDisclosure payload |
| Visibility | `internal/contracts/skillmarket/visibility.go` | 创作者 API 安全摘要和私有字段泄露守卫 |
| Tests | `internal/contracts/skillmarket/*_test.go` | 读取 PR-4 active fixture 做漂移防护 |
| Business Repository | `services/business/internal/infra/repository/businesscore/marketplace.go` | 发布、安装、升级、usage 预创建、扣费结算、退款反转事务与幂等 |
| Business Application | `services/business/internal/application/marketplace/app.go` | Marketplace 列表/详情、安装、升级、usage 估算、预创建、冻结、交付扣费和 settlement hold |
| User HTTP | `services/business/internal/transport/http/handlers_work_notification_marketplace.go` | `/api/marketplace/skills`、详情、安装、升级、已安装列表 |
| Creator HTTP | `services/business/internal/transport/http/handlers_work_notification_marketplace.go` | `/api/creator/skills` 草稿、提交审核、创作者 listing 和脱敏 usage analytics |
| Admin HTTP | `services/business/internal/transport/http/handlers_work_notification_marketplace.go` | `/api/admin/marketplace/*` 查询、`/api/admin/skill-reviews/*/approve`、`/api/admin/listings/*/suspend`、`/api/admin/refund-cases/*/approve`、`/api/admin/settlements/*/release-hold`、`/api/admin/settlements/*/confirm-payout` |
| Business RPC | `services/business/internal/transport/rpc/handlers_marketplace.go`、`kitex_gen/dora/api/businessskillmarketplace/**` | `BusinessSkillMarketplaceService` 查询、安装、升级、usage 估算、预创建、冻结、扣费结算和释放冻结 |
| User / Creator Frontend | `frontend/src/lib/api/marketplace.js`、`frontend/src/lib/api/creator.js`、`frontend/src/features/skills/SkillsPage.jsx` | 用户端市场列表、搜索筛选、安装登录门、个人安装、已安装后使用入口、创作者草稿和提交审核 |
| Admin Frontend | `admin_frontend/src/features/resources/pageConfigs.jsx`、`admin_frontend/src/layout/AdminShell.jsx` | 管理端 Skill 审核、市场 listing、退款仲裁、settlement hold 解除和内部出账确认 |
| Settlement Governance | `skill_settlement_payout_records` | 记录 release_hold / confirm_payout 动作、状态前后值、原因码、操作管理员、出账引用和幂等键 |
| Migration Tests | `services/business/internal/infra/repository/businesscore/marketplace_integration_test.go` | PR-4 business migration up/down、无外键、fixture 状态机验证 |

## 开发注意事项

1. 付费市场 Skill 必须先 `CreateSkillUsageRecord(status=confirmation_required, charge_status=not_frozen)`，再展示 Skill 使用费确认。
2. Skill 使用费确认和 Tool 生成费确认是两阶段 CostDisclosure；PR-4 不得替代 PR-3 Tool 生成费确认。
3. 个人安装默认 `latest_published`；企业安装默认 `pinned`，升级必须确认。
4. 历史 run 必须使用启动时的 skill version snapshot 恢复。
5. 创作者 API 只能返回聚合摘要，不得返回用户 prompt、私有 board、生成资产、对话或 provider 原始 payload。

## Done Gate

- [x] `internal/contracts/skillmarket` 包存在。
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
- [x] Creator Portal HTTP 主路径已接入 `creator-api.yaml` 已冻结路径。
- [x] 真实 Marketplace RPC adapter 已接入 `BusinessSkillMarketplaceService` 并覆盖 usage 创建、冻结、扣费和释放冻结。
- [x] 用户端 Skill 市场前台已接入市场列表、安装登录门、个人安装和已安装后使用入口。
- [x] 创作者 Skill 发布后台已接入草稿创建、提交审核、创作者 listing 和脱敏 analytics。
- [x] Admin Governance HTTP 主路径已接入 Skill 审核通过、listing 暂停、退款通过反转和 settlement hold 查询。
- [x] 管理端页面已接入 Skill 审核、Skill 市场、Skill 退款和 Skill 结算治理列表。
- [x] 内部 settlement 出账治理已接入：hold 到期解除、eligible 后人工确认出账、幂等重放、状态同步和治理记录落库。
- [ ] 后续 PR 接入外部真实打款通道和支付/财务供应商对账。

## 验证命令

```bash
go test ./internal/contracts/skillmarket
go test ./services/business/internal/application/marketplace
go test ./internal/contracts/skillmarket ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/transport/rpc
go test ./services/business/internal/transport/http
make active-contract-gate
make development-ci-gate
npm test --prefix frontend
npm run build --prefix frontend
npm test --prefix admin_frontend
npm run build --prefix admin_frontend
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/skillmarket
go test ./services/business/internal/application/marketplace
go test ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/transport/rpc
go test ./services/business/internal/transport/http
go test ./internal/contracts/skillmarket ./services/business/internal/infra/repository/businesscore
make active-contract-gate
make development-ci-gate
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
make development-ci-gate
```

2026-07-01 创作者 Skill 发布后台接入后已执行：

```bash
go test ./services/business/internal/application/marketplace ./services/business/internal/transport/http
npm test --prefix frontend
npm run build --prefix frontend
make active-contract-gate
make development-ci-gate
```

2026-07-01 管理端 Skill 治理和结算 hold 页面接入后已执行：

```bash
go test ./services/business/internal/application/marketplace
go test ./services/business/internal/transport/http
npm test --prefix admin_frontend
npm run build --prefix admin_frontend
make active-contract-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket
ok github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
ok github.com/FigoGoo/Dora-Agent/services/business/internal/transport/http
ok github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc
ok github.com/FigoGoo/Dora-Agent/services/business/internal/bootstrap
active contract gate passed
development CI gate passed
```

2026-07-01 内部 settlement 出账治理接入后已执行：

```bash
GOCACHE=/private/tmp/dora-go-cache go test -run '^$' ./services/business/internal/application/marketplace
GOCACHE=/private/tmp/dora-go-cache go test -run '^$' ./services/business/internal/transport/http
GOCACHE=/private/tmp/dora-go-cache go test -run '^$' ./services/business/internal/infra/repository/businesscore
python3 tests/contract/validate_skill_market_contracts.py
npm test --prefix admin_frontend
npm run build --prefix admin_frontend
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace [no tests to run]
ok github.com/FigoGoo/Dora-Agent/services/business/internal/transport/http [no tests to run]
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore [no tests to run]
pr4 contract validation ok
admin_frontend: 13 files / 41 tests passed
admin_frontend build passed
active contract gate passed
development-ci-gate: active contract、gofmt dry check 和不依赖 Docker 的 contracts 测试通过；全量 Go 测试被当前沙箱的 Docker socket 权限和本地端口监听权限拦截，需在具备 Docker/网络监听权限的 CI 或本地环境复跑
```
