# PR-3 Tool / Credit / Asset 基础实现

状态：active  
owner：Agent Runtime / Business Credit / Business Asset / Tool Runtime / 文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：PR-3 字段级契约在 Go 运行时的 ToolPlan、ToolTask、CreditFreeze、GeneratedAsset、AG-UI payload、Agent / Business repository 基础实现  
相关代码路径：`internal/contracts/pr3/**`、`services/agent/internal/infra/repository/**`、`services/business/internal/infra/repository/businesscore/**`  
相关契约：`docs/active/contracts/pr-3-tool-credit-asset-contracts.md`、`api/schemas/tool/**`、`api/schemas/credit/**`、`api/schemas/asset/**`、`api/agui/events/cost_disclosure.generation.presented.schema.json`、`api/agui/events/tool.task.updated.schema.json`、`api/agui/events/asset.commit.updated.schema.json`

## 背景

PR-3 已冻结 Board approved 后的 ToolPlan 估算、Tool 生成费确认、积分冻结、任务执行、资产提交、扣费或释放契约。进入真实服务实现前，需要先提供 Go 运行时可复用的契约包，让 Agent Runtime 与业务服务 adapter 共享状态和字段口径。

## 目标

- 提供 ToolPlan、ToolTask、ToolResult Go 类型和校验。
- 提供 FreezeCreditsRequest、CreditFreeze 以及 freeze / commit / release 状态机校验。
- 提供 GeneratedAsset、AssetCommitResponse 和部分提交计费守卫。
- 提供 Tool / Credit / Asset AG-UI payload 校验，并复用 PR-1 AG-UI Envelope。
- 使用 active fixture 回归测试 ToolPlan、Credit、Asset partial commit、provider async resume。

## 非目标

- 不处理 Skill 使用费和结算。
- 不处理 Marketplace 安装、创作者后台和 listing 治理。
- 不接真实线上 provider 流量。
- 不在本 PR 交付真实 Business RPC adapter、Redis worker、provider fake/real runtime。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Tool | `internal/contracts/pr3/tool.go` | ToolPlan、ToolTask、ToolResult 类型和状态校验 |
| Credit | `internal/contracts/pr3/credit.go` | 冻结、扣减、释放状态机校验 |
| Asset | `internal/contracts/pr3/asset.go` | GeneratedAsset、部分提交和失败资产不扣费守卫 |
| Worker | `internal/contracts/pr3/worker.go` | provider async resume 和 Redis stream 事件校验 |
| AG-UI | `internal/contracts/pr3/agui.go` | Tool 生成费、ToolTask、AssetCommit payload 类型 |
| Tests | `internal/contracts/pr3/*_test.go` | 读取 PR-3 active fixture 做漂移防护 |
| Agent Repository | `services/agent/internal/infra/repository/tool_credit_asset.go` | ToolPlan / ToolTask 保存、查询和 provider async resume 幂等更新 |
| Agent Workbench | `services/agent/internal/application/workbench/app_m4_toolplan.go`、`services/agent/internal/application/workbench/app.go` | Board approve 后创建 ToolPlan、Tool 生成费 CostDisclosure、tool_generation_confirmation、ToolTask 和资产提交 AG-UI 闭环 |
| Business Repository | `services/business/internal/infra/repository/businesscore/pr3_tool_credit_asset.go` | Credit freeze / commit / release、Asset partial commit 事务与幂等 |
| Migration Tests | `services/agent/internal/infra/repository/tool_credit_asset_integration_test.go`、`services/business/internal/infra/repository/businesscore/pr3_tool_credit_asset_integration_test.go` | PR-3 agent / business migration up/down 和无外键边界验证 |

## 开发注意事项

1. Tool 生成费确认不得替代 Skill 使用费确认；默认仍是两阶段 CostDisclosure。
2. ToolPlan 只能在 Board approved 后生成，并必须绑定 `board_id`、`board_version`、`graph_plan_id` 和 `tool_plan_digest`。
3. 积分冻结、扣减、释放由业务服务负责，Agent Runtime 只能通过 RPC 契约调用。
4. provider async stream 至少一次投递，必须按 `tool_task_id` 和 `dedupe_key` 去重。
5. 部分资产提交时失败资产不得计费，只能对 committed assets 扣减。

## Done Gate

- [x] `internal/contracts/pr3` 包存在。
- [x] ToolPlan fixture 校验通过，且要求 approved board。
- [x] Credit freeze -> commit fixture 校验通过。
- [x] Credit freeze -> release on failure fixture 校验通过。
- [x] Asset partial commit fixture 校验通过，失败资产不扣费。
- [x] provider async resume fixture 校验通过。
- [x] PR-3 AG-UI payload 可使用 PR-1 Envelope 构造并校验。
- [x] PR-3 Agent / Business PostgreSQL migration dry-run 和 down-test 已由 repository integration tests 覆盖。
- [x] ToolPlan / ToolTask repository 已支持幂等保存、查询和 provider completed event 重放。
- [x] Credit / Asset repository 已支持冻结、扣减、释放、partial commit 和幂等重放。
- [x] Agent Workbench 已支持 Board approved -> ToolPlan -> `cost_disclosure.generation.presented` -> `tool_generation_confirmation` -> FreezeCredits -> ToolTask -> AssetCommit -> `tool.task.updated` / `asset.commit.updated` 的本地闭环。
- [ ] 后续 PR 接入真实 Business RPC adapter、Redis worker 和 provider fake/real 实现。

## 验证命令

```bash
go test ./internal/contracts/pr3
go test ./services/agent/internal/application/workbench -run TestM4BoardApproveCreatesToolPlanAndConfirmationThenCommitsAsset
go test ./services/agent/internal/application/workbench
go test ./services/agent/internal/api/http
go test ./internal/contracts/pr3 ./services/agent/internal/infra/repository ./services/business/internal/infra/repository/businesscore
make active-contract-gate
make pr0-ci-gate
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/pr3
go test ./services/agent/internal/application/workbench -run TestM4BoardApproveCreatesToolPlanAndConfirmationThenCommitsAsset -count=1 -v
go test ./services/agent/internal/application/workbench -count=1
go test ./services/agent/internal/api/http -count=1
go test ./services/agent/internal/infra/repository
go test ./services/business/internal/infra/repository/businesscore
go test ./internal/contracts/pr3 ./services/agent/internal/infra/repository ./services/business/internal/infra/repository/businesscore
make active-contract-gate
make pr0-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/pr3
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
active contract gate passed
PR-0 CI gate passed
```
