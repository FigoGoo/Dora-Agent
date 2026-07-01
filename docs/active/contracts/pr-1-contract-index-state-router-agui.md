# PR-1 Contract Index + StateEnum + RouterDecision + AG-UI Envelope

状态：active  
PR 状态：frozen  
owner：文档与契约责任域 / Agent Runtime / 前端 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：M0 active 最小契约冻结、RouterDecision、AG-UI Envelope、第一批 fixture  
来源设计源：`docs/review/aigc-agent-refactor/2026-07-01/01-M0-协议冻结与微服务基线.md`、`02-M1-CreativeGuide与ChatModelRouter.md`

## 目标

PR-1 冻结所有后续 PR 共享的最小契约骨架：字段命名、状态枚举、digest、错误结构、RouterDecision、CreativeGuideOutput、SkillCatalogSummary、AG-UI Envelope 和第一批 router / AG-UI fixture。

## 已冻结产物

| 类型 | Canonical File | 状态 |
| --- | --- | --- |
| Contract Index | `docs/active/contracts/index.md` | frozen |
| M0 Freeze | `docs/active/contracts/m0-contract-freeze.md` | frozen |
| StateEnumRegistry | `api/schemas/common/state-enum-registry.schema.json` | frozen |
| Digest | `api/schemas/common/digest.schema.json` | frozen |
| IDs | `api/schemas/common/ids.schema.json` | frozen |
| Error | `api/schemas/common/error.schema.json` | frozen |
| RouterDecision | `api/schemas/router/router-decision.v1.schema.json` | frozen |
| CreativeGuideOutput | `api/schemas/router/creative-guide-output.v1.schema.json` | frozen |
| SkillCatalogSummary | `api/schemas/router/skill-catalog-summary.v1.schema.json` | frozen |
| AG-UI Envelope | `api/agui/agent-workbench-events.schema.json` | frozen |
| AG-UI Event Payloads | `api/agui/events/**` | frozen |
| Router Fixtures | `tests/fixtures/contracts/router/**` | frozen |
| AG-UI Fixtures | `tests/fixtures/contracts/agui/**` | frozen |

## 状态枚举冻结

PR-1 已冻结以下状态枚举集合：

```text
RunStatus
BoardStatus
GraphPlanStatus
ToolPlanStatus
ToolTaskStatus
SkillVersionStatus
MarketplaceListingStatus
SkillUsageStatus
SkillUsageChargeStatus
SkillUsageRefundStatus
SettlementStatus
InstallationStatus
InstallationUpgradeStatus
RefundCaseStatus
```

## AG-UI Envelope 冻结

AG-UI 事件必须使用统一 envelope：

```text
event_id
event_type
run_id
seq
created_at
dedupe_key
payload
trace_id
schema_version
```

同一 `run_id` 内 `seq` 必须单调递增；前端必须用 `dedupe_key` 去重。

## Router Fixture 覆盖

| Fixture | 覆盖场景 |
| --- | --- |
| `select_system_city_tourism.json` | 默认系统 Skill 命中 |
| `clarify_generic_promo_video.json` | 意图不完整，进入澄清 |
| `marketplace_candidate_not_installed.json` | 市场候选但未安装 |
| `paid_marketplace_explicit_select.json` | 付费市场 Skill 显式选择 |
| `invalid_skill_guarded_to_clarify.json` | 不可用 Skill Guard |

## Done Gate

- [x] Contract Index 存在并指向 canonical files。
- [x] StateEnumRegistry 拆出 usage、charge、refund 独立枚举。
- [x] RouterDecision schema 和 fixtures 存在。
- [x] AG-UI Envelope 和 payload schema 存在。
- [x] 付费市场 Skill 使用费确认 fixture 存在。
- [x] `python3 tests/contract/validate_pr1_contracts.py` 通过。

## 后续影响

PR-2 到 PR-5 不得新增与 PR-1 冲突的状态、字段命名、AG-UI envelope 或 digest 规则。确需扩展时必须保持向后兼容，并同步 `docs/active/contracts/index.md`。
