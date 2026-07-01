# PR-4 Marketplace Contracts

状态：active  
PR 状态：active / contract frozen  
owner：Business Skill Marketplace / Business Credit / Agent Runtime / 前端 / 管理端 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：Skill 市场、创作者发布后台、用户市场前台、安装升级、SkillUsageRecord、结算、治理和数据隔离  
来源设计源：`06-M5-开放Skill市场与两段积分结算.md`、`10-开放市场风控与数据隔离设计.md`、`07-M6-前后台体验与端到端验收.md`

## 目标

PR-4 冻结开放 Skill 市场完整闭环：创作者提交、审核、上架、用户发现、安装、使用、Skill 使用费确认、usage record 状态机、结算、退款、治理和数据隔离。

## 非目标

- 不修改 PR-1 AG-UI envelope。
- 不修改 PR-3 Tool 生成费扣费规则。
- 不做真实收益出账，只冻结结算 hold 和 settlement ledger 契约。

## Canonical Files

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| User API | `api/openapi/business-api.yaml` | 用户市场、安装、使用入口 |
| Creator API | `api/openapi/creator-api.yaml` | 创作者发布后台 |
| Admin API | `api/openapi/admin-api.yaml` | 审核、治理、风控 |
| Marketplace RPC | `api/thrift/business_skill_marketplace_service.thrift` | Skill、listing、installation、usage |
| Settlement RPC | `api/thrift/business_settlement_service.thrift` | 结算 hold、释放、退款 |
| Skill Schema | `api/schemas/skill/skill-package.v1.schema.json` | Skill 包结构 |
| SkillVersion Schema | `api/schemas/skill/skill-version.v1.schema.json` | 内容版本状态机 |
| Listing Schema | `api/schemas/skill/marketplace-listing.v1.schema.json` | 市场上架状态机 |
| Installation Schema | `api/schemas/skill/skill-installation.v1.schema.json` | 个人/企业安装策略 |
| Usage Schema | `api/schemas/skill/skill-usage-record.v1.schema.json` | SkillUsageRecord |
| Pricing Schema | `api/schemas/skill/skill-pricing-policy.v1.schema.json` | 使用费策略 |
| Settlement Schema | `api/schemas/settlement/skill-settlement.v1.schema.json` | 结算和退款 |
| Business Migration | `db/migrations/iterations/2026-07-01-marketplace-contracts/business/**` | 业务表 |
| Fixtures | `tests/fixtures/contracts/marketplace/**`、`tests/fixtures/contracts/billing/**` | 合同样例 |
| Validator | `tests/contract/validate_pr4_contracts.py` | PR-4 contract gate |

## P0 必拆项

| P0 | PR-4 active 冻结要求 |
| --- | --- |
| SkillUsageRecord 创建时机和状态流转 | `EstimateSkillUsageCredits -> CreateSkillUsageRecord(status=confirmation_required) -> Freeze -> Running -> ValueDelivered -> CommitAndSettle` |
| Skill 使用费确认与 Tool 生成费确认 | Skill 使用费默认先确认；Tool 生成费必须等 Board approved 和 ToolPlan 生成后确认 |
| 创作者端 Skill 发布后台 | 冻结 Creator API、页面状态、审核状态、错误摘要和验收 fixture |
| 用户端 Skill 市场前台 | 冻结市场首页、详情、安装、工作台候选、已安装 Skill 使用路径 |
| installation 版本策略 | 个人默认 latest_published；企业默认 pinned；升级必须确认；历史 run 使用 snapshot |
| Generic Creation Graph | 不进入市场、不收费；PR-4 不得将 L0 fallback 当成 marketplace listing |

## SkillUsageRecord 状态机

创建时机：

```text
用户选择付费市场 Skill
  -> EstimateSkillUsageCredits
  -> CreateSkillUsageRecord(status=confirmation_required, charge_status=not_frozen)
  -> cost_disclosure.skill_usage.presented
  -> 用户确认 skill_usage_digest
  -> FreezeSkillUsageCredits
  -> usage_status=running, charge_status=frozen
  -> Graph reaches value_delivered_stage
  -> CommitSkillUsageAndSettle
  -> usage_status=value_delivered, charge_status=charged, settlement_status=hold
```

独立枚举：

```text
usage_status: confirmation_required | confirmation_declined | cancelled | expired | frozen | running | value_delivered | charged | settlement_pending | released | refund_pending | refunded | refund_rejected | failed
charge_status: not_frozen | frozen | charged | released | failed
refund_status: none | refund_requested | refund_reviewing | refund_approved | refund_rejected | refund_reversed
settlement_status: pending_hold | eligible | settling | settled | reversed | frozen | failed
```

## Marketplace API 范围

| API | 使用方 | 说明 |
| --- | --- | --- |
| `GET /api/marketplace/skills` | 用户端 | 市场首页列表 |
| `GET /api/marketplace/skills/{listing_id}` | 用户端 | Skill 详情 |
| `POST /api/marketplace/installations` | 用户端 | 安装 Skill |
| `POST /api/marketplace/installations/{installation_id}/upgrade` | 用户端 | 安装版本升级 |
| `GET /api/marketplace/my-skills` | 用户端 | 已安装 Skill |
| `POST /api/creator/skills` | 创作者端 | 创建 Skill 草稿 |
| `POST /api/creator/skills/{skill_id}/submit` | 创作者端 | 提交审核 |
| `POST /api/admin/skill-reviews/{review_id}/approve` | 管理端 | 审核通过 |
| `POST /api/admin/listings/{listing_id}/suspend` | 管理端 | 暂停 listing |

## RPC 范围

| RPC | Owner | 幂等键 | 说明 |
| --- | --- | --- | --- |
| `EstimateSkillUsageCredits` | Business Credit | `run_id + listing_id + pricing_policy_digest` | Skill 使用费估算 |
| `CreateSkillUsageRecord` | Business Marketplace | `run_id + listing_id + skill_version + pricing_policy_digest` | 预创建 usage record |
| `FreezeSkillUsageCredits` | Business Credit | `usage_id + skill_usage_digest` | 冻结 Skill 使用费 |
| `CommitSkillUsageAndSettle` | Business Credit / Settlement | `usage_id + value_delivered_digest` | 扣费并进入结算 hold |
| `ReleaseSkillUsageFreeze` | Business Credit | `usage_id` | 取消或失败释放 |
| `InstallSkill` | Business Marketplace | `account_id + listing_id + target_scope` | 安装 Skill |
| `UpgradeSkillInstallation` | Business Marketplace | `installation_id + target_version` | 升级安装版本 |

## 数据隔离

创作者 API 不得返回用户私有创作数据。允许返回：

```text
usage_count
revenue_hold_amount
refund_count
failure_code_summary
review_status
listing_status
settlement_status
```

禁止返回：

```text
user prompt
private board content
generated assets
workspace conversation
raw provider payload
```

## Redis 使用

| 类型 | Key / Stream | 用途 | 约束 |
| --- | --- | --- | --- |
| cache | `marketplace:listings:{query_hash}` | 市场列表缓存 | listing 变更主动失效 |
| cache | `skill:installation:{account_id}:{skill_id}` | 安装查询缓存 | 安装/升级后失效 |
| stream | `skill.usage.created` | usage record 创建事件 | 下游结算/分析异步消费 |
| stream | `skill.usage.value_delivered` | 交付达成事件 | 扣费结算幂等 |
| stream | `skill.listing.changed` | listing 上下架变更 | 清缓存、通知 |

## Fixture 要求

| Fixture | 必须覆盖 |
| --- | --- |
| `marketplace/creator_submit_approve_publish.json` | 创作者发布闭环 |
| `marketplace/user_install_latest_personal.json` | 个人 latest 安装 |
| `marketplace/enterprise_install_pinned_upgrade.json` | 企业 pinned 和升级确认 |
| `billing/skill_usage_precreate_confirm_charge.json` | usage record 预创建、冻结、扣费 |
| `billing/skill_usage_refund_reversal.json` | 退款和结算 reverse |
| `marketplace/data_visibility_creator_safe.json` | 创作者不可见用户私有数据 |

## Done Gate

- [x] SkillVersion / MarketplaceListing 双状态机冻结。
- [x] SkillUsageRecord 预创建、usage/charge/refund 独立状态冻结。
- [x] SkillInstallation 版本策略和升级规则冻结。
- [x] Creator Portal / User Marketplace / Admin Governance API fixture 通过。
- [x] 两阶段 CostDisclosure 与 PR-3 Tool 生成费确认不冲突。
- [x] 数据隔离 fixture 证明创作者 API 不返回用户私有创作数据。
- [x] `python3 tests/contract/validate_pr4_contracts.py` 通过。
- [x] 真实 PostgreSQL migration dry-run 和 down-test 已由 Business repository integration test 覆盖，并会被 PR-0 `go test ./services/... ./internal/...` 执行。
- [x] Business Marketplace 应用层和用户端 Marketplace HTTP 主路径已接入。
- [ ] 真实 Marketplace RPC adapter、创作者/用户/管理端页面和结算出账治理接入。
