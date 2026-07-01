# M0 Active 契约冻结

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：M0 最小契约冻结、PR-1 字段级事实源、后续 PR 依赖入口  
来源 review：`docs/review/aigc-agent-refactor/2026-07-01/01-M0-协议冻结与微服务基线.md`

## 冻结范围

M0 / PR-1 active 最小契约已冻结，当前锁定以下字段事实源：

| Contract | Canonical File | 状态 |
| --- | --- | --- |
| Contract Index | `docs/active/contracts/index.md` | active |
| StateEnumRegistry | `api/schemas/common/state-enum-registry.schema.json` | active |
| Digest | `api/schemas/common/digest.schema.json` | active |
| Error | `api/schemas/common/error.schema.json` | active |
| RouterDecision | `api/schemas/router/router-decision.v1.schema.json` | active |
| CreativeGuideOutput | `api/schemas/router/creative-guide-output.v1.schema.json` | active |
| SkillCatalogSummary | `api/schemas/router/skill-catalog-summary.v1.schema.json` | active |
| AGUIEventEnvelope | `api/agui/agent-workbench-events.schema.json` | active |
| AG-UI Event Payloads | `api/agui/events/**` | active |
| Router Fixtures | `tests/fixtures/contracts/router/**` | active |
| AG-UI Fixtures | `tests/fixtures/contracts/agui/**` | active |

## 状态枚举冻结

PR-1 状态枚举冻结以下集合：

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

## 两阶段 CostDisclosure 冻结

默认费用确认链路：

```text
cost_disclosure.skill_usage.presented
  -> confirmation.required(skill_usage)
  -> confirm skill_usage_digest
  -> FreezeSkillUsageCredits
  -> Graph delivers Board / Storyboard
  -> CommitSkillUsageAndSettle
  -> Board approved
  -> ToolPlan estimate
  -> cost_disclosure.generation.presented
  -> confirmation.required(tool_generation)
  -> confirm tool_plan_digest
  -> FreezeCredits
```

combined confirmation 只允许在 ToolPlan 已存在且 Skill usage 尚未确认时发生。后端必须按：

```text
FreezeSkillUsageCredits -> FreezeCredits
```

顺序执行；第二步失败必须释放第一步冻结。

## 验收命令

当前 M0 / PR-1 使用 active contract gate 校验，包含 PR-1 到 PR-5 轻量 contract validator 和正式 JSON Schema fixture validator：

```bash
python3 tests/contract/validate_foundation_contracts.py
python3 tests/contract/validate_json_schema_contracts.py
make active-contract-gate
```

## Done Gate

- [x] PR-1 契约索引存在。
- [x] StateEnumRegistry 拆出 charge/refund 独立枚举。
- [x] RouterDecision schema 存在。
- [x] AG-UI Envelope 使用 `event_type`、`seq`、`created_at`、`dedupe_key`。
- [x] Skill 使用费 CostDisclosure payload schema 存在。
- [x] Router fixture 和 AG-UI fixture 存在。
- [x] PR-1 轻量 contract validator 已接入。
- [x] 用户 review 已完成，M0 / PR-1 active 最小契约可作为后续 PR 拆分基线。
- [x] PR-2 / PR-3 / PR-4 本地真实 PostgreSQL dry-run / down-test 已完成。
- [x] PR-5 本地 service-level PostgreSQL E2E 已完成。
- [x] PR-5 本地 Agent HTTP router + Redis container E2E 已完成。
- [x] PR-5 本地 Agent 独立进程 HTTP smoke 已完成。
- [x] PR-5 本地 Business 独立进程 HTTP smoke 已完成。
- [x] PR-5 本地 Agent + Business 双服务 HTTP smoke 已完成。
- [x] 正式 JSON Schema validator 接入 CI。
- [x] PR-5 本地真实浏览器前端联动 smoke 执行完成。
- [x] PR-5 测试环境 HTTP 服务 E2E 自动化入口和报告模板完成。
- [ ] PR-5 完整测试环境 HTTP 服务 E2E 环境执行与 `status: passed` 报告归档完成前，不进入对应 M1-M6 后续真实流量发布。
