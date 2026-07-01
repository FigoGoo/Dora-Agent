# Contract-first PR 路线图

状态：active  
owner：文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：M7 active 拆分后的 PR 批次、交付物、依赖、禁止事项和 Done Gate  
来源设计源：`docs/review/aigc-agent-refactor/2026-07-01/`

## 当前裁决

本轮 review 已完成并通过，Contract-first active 拆分已完成。当前已从 PR-0 / PR-1 基础实现进入 M1 业务代码开发，M1 Creative Guide / ChatModel Router 基础闭环已实现并保持在真实 provider gate 之前。

```text
PR-1 Contract Index + StateEnum + RouterDecision + AG-UI Envelope
  -> PR-2 Agent Runtime Contracts
  -> PR-3 Tool/Credit/Asset Contracts
  -> PR-4 Marketplace Contracts
  -> PR-5 E2E Fixtures + Fake Provider + Release Gates
```

## PR 状态总览

| PR | 状态 | 独立文档 | 目标 |
| --- | --- | --- | --- |
| PR-1 | active / frozen | [`pr-1-contract-index-state-router-agui.md`](./pr-1-contract-index-state-router-agui.md) | 冻结 Contract Index、StateEnum、RouterDecision、AG-UI Envelope 和第一批 fixture |
| PR-2 | active / contract frozen | [`pr-2-agent-runtime-contracts.md`](./pr-2-agent-runtime-contracts.md) | Agent Runtime、CreativeBoard、GraphPlan、Generic Creation Graph、agent migration 和 validator 已冻结；本地真实 PostgreSQL dry-run / down-test 已完成 |
| PR-3 | active / contract frozen | [`pr-3-tool-credit-asset-contracts.md`](./pr-3-tool-credit-asset-contracts.md) | ToolPlan、ToolTask、Credit freeze/commit/release、Asset commit、provider async、RPC、migration 和 validator 已冻结；本地真实 PostgreSQL dry-run / down-test 已完成 |
| PR-4 | active / contract frozen | [`pr-4-marketplace-contracts.md`](./pr-4-marketplace-contracts.md) | Marketplace、SkillUsageRecord、Installation、Settlement、Creator/User/Admin API、migration 和 validator 已冻结；本地真实 PostgreSQL dry-run / down-test 已完成 |
| PR-5 | active / release gate frozen | [`pr-5-e2e-fixtures-release-gates.md`](./pr-5-e2e-fixtures-release-gates.md) | E2E fixtures、Fake Provider、release gates、rollback、观测、轻量 validator、本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 已冻结；真实浏览器、前端联动和完整测试环境 HTTP 服务 E2E 待 CI / 测试环境 gate |

## 全局拆分规则

1. 每个 PR 必须先冻结字段级事实源，再允许对应实现开发。
2. 每个 PR 必须同步 `docs/active/contracts/index.md`、schema、fixture、validator 和 Done Gate。
3. 涉及 RPC、API、AG-UI、SQL、Redis key、Redis event、状态机和错误码时，必须在该 PR 的 active 契约中声明。
4. review 文档只作为设计来源，不能直接作为字段级编码依据。
5. M1-M6 业务代码开发必须等待对应 PR 的 active 契约、migration、fixture 和 release gate 完成。
6. 所有确认、冻结、扣费、释放、资产提交和安装升级操作必须有幂等键、trace_id 和审计口径。
7. 所有长任务必须支持并发安全、重放、恢复和重复事件去重。

## 依赖关系

| 依赖 | 说明 |
| --- | --- |
| PR-2 依赖 PR-1 | Board / Graph / Generic Creation Graph 必须复用 RouterDecision、AG-UI Envelope 和 StateEnumRegistry |
| PR-3 依赖 PR-2 | ToolPlan 必须绑定 `board_id`、`board_version`、`graph_plan_id` 和 digest |
| PR-4 依赖 PR-1 / PR-2 / PR-3 | Skill 使用费确认、installation、marketplace usage 必须复用状态枚举、Graph gate、两阶段 CostDisclosure |
| PR-5 依赖 PR-1 / PR-2 / PR-3 / PR-4 | E2E 必须覆盖 AG-UI、Board、Graph、Tool/Credit/Asset、Marketplace 和发布治理 |

## 全局禁止事项

| 禁止项 | 原因 |
| --- | --- |
| 直接按 review 文档写业务代码 | review 文档不是字段级事实源 |
| 在 PR-2 前实现 Tool 生成扣费 | ToolPlan 依赖 Board / GraphPlan digest |
| 在 PR-3 前实现 Marketplace usage 扣费结算 | Marketplace 结算依赖 Credit freeze/commit/release 契约 |
| 在 PR-4 前开发创作者后台或用户市场前台 | 页面字段和操作流必须先由 Marketplace API / RPC / SQL fixture 冻结 |
| 在 PR-5 真实 E2E gate 前发布真实 provider 流量 | release gate、fake provider、回滚和观测需要在真实测试环境复核 |

## 验证矩阵

| PR | 最小验证命令 | 结果要求 |
| --- | --- | --- |
| PR-1 | `python3 tests/contract/validate_pr1_contracts.py` | JSON、Router fixture、AG-UI fixture 通过 |
| PR-2 | `python3 tests/contract/validate_pr2_contracts.py` | Board replay、GraphPlan digest、agent migration static guard 通过 |
| PR-3 | `python3 tests/contract/validate_pr3_contracts.py`、`go test ./services/agent/internal/infra/repository ./services/business/internal/infra/repository/businesscore` | ToolPlan digest、freeze/commit/release、asset partial success、agent/business migration up/down 通过 |
| PR-4 | `python3 tests/contract/validate_pr4_contracts.py`、`go test ./services/business/internal/infra/repository/businesscore` | Marketplace API/RPC、usage record、installation、settlement fixture、business migration up/down 通过 |
| PR-5 | `python3 tests/contract/validate_pr5_e2e_gates.py`、`go test ./services/business/internal/e2e/pr5`、`go test ./services/agent/internal/e2e/pr5` | fake provider、E2E fixture、release/rollback gate、本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 通过 |

## M0 冻结关系

M0 active 最小契约冻结由 PR-1 承接，详见：

- [`m0-contract-freeze.md`](./m0-contract-freeze.md)
- [`pr-1-contract-index-state-router-agui.md`](./pr-1-contract-index-state-router-agui.md)

PR-1 到 PR-5 的 active 冻结已按本路线图完成。PR-2 / PR-3 / PR-4 本地真实 PostgreSQL dry-run / down-test 已完成；PR-5 本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 已完成。进入真实流量和 ready 标记前，仍必须完成 PR-5 真实浏览器、前端联动和完整测试环境 HTTP 服务 E2E 执行。

开发阶段从 PR-0 开始，入口见 [`../technical/pr-0-development-ci-gate.md`](../technical/pr-0-development-ci-gate.md)。M1 Router 实现入口见 [`../technical/router.md`](../technical/router.md)。PR-0 只建立 CI gate 和本地验证入口，不实现业务逻辑。
