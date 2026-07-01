# PR-3 Tool / Credit / Asset Contracts

状态：active  
PR 状态：active / contract frozen  
owner：Agent Runtime / Business Credit / Business Asset / Tool Runtime / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：ToolPlan、ToolTask、两阶段 Tool 生成费确认、积分冻结扣减释放、资产提交、provider async、Redis worker  
来源设计源：`05-M4-Preflight与ToolRuntime资产扣费.md`、`03-M2-CreativeBoard与AGUI事件闭环.md`

## 目标

PR-3 冻结从 Board approved 到 ToolPlan 估算、Tool 生成费确认、积分冻结、任务执行、资产提交、扣费或释放的完整契约。

## 非目标

- 不处理 Skill 使用费结算。
- 不处理 Skill 市场安装、创作者后台、listing 治理。
- 不接真实线上 provider 流量。

## Canonical Files

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Tool Schema | `api/schemas/tool/tool-plan.v1.schema.json` | ToolPlan 和 digest |
| ToolTask Schema | `api/schemas/tool/tool-task.v1.schema.json` | provider 执行任务 |
| ToolResult Schema | `api/schemas/tool/tool-result.v1.schema.json` | 生成结果 |
| Credit Schema | `api/schemas/credit/credit-freeze.v1.schema.json` | freeze / commit / release 结构 |
| Asset Schema | `api/schemas/asset/generated-asset.v1.schema.json` | 资产提交结构 |
| AG-UI Payload | `api/agui/events/cost_disclosure.generation.presented.schema.json` | Tool 生成费披露 |
| AG-UI Payload | `api/agui/events/tool.*.schema.json` | Tool task 状态流 |
| Credit RPC | `api/thrift/business_credit_service.thrift` | 积分估算、冻结、扣减、释放 |
| Asset RPC | `api/thrift/business_asset_service.thrift` | 资产提交、查询 |
| Tool RPC | `api/thrift/business_tool_service.thrift` | Tool 能力目录和价格 |
| Agent Migration | `db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/agent/**` | ToolPlan / ToolTask 表 |
| Business Migration | `db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/business/**` | 业务 credit / asset / tool 表 |
| Fixtures | `tests/fixtures/contracts/toolplan/**`、`tests/fixtures/contracts/credit/**`、`tests/fixtures/contracts/asset/**` | 合同样例 |
| Validator | `tests/contract/validate_pr3_contracts.py` | PR-3 contract gate |

## 主链路

```text
Board approved
  -> Build ToolPlan(board_id, board_version, graph_plan_id)
  -> EstimateToolCredits
  -> cost_disclosure.generation.presented(tool_plan_digest)
  -> confirmation.required(tool_generation)
  -> 用户确认 tool_plan_digest
  -> FreezeCredits(idempotency_key)
  -> Dispatch ToolTask
  -> Provider callback / polling
  -> CommitGeneratedAssets
  -> CommitCredits
  -> Board asset linked
```

失败链路：

```text
FreezeCredits succeeded
  -> ToolTask failed / Asset commit failed
  -> ReleaseCredits(idempotency_key)
  -> tool.failed AG-UI event
```

## RPC 契约范围

| RPC | Owner | 幂等键 | 说明 |
| --- | --- | --- | --- |
| `EstimateToolCredits` | Business Credit | `tool_plan_digest` | 只估算，不冻结 |
| `FreezeCredits` | Business Credit | `run_id + tool_plan_digest + account_id` | 冻结 Tool 生成费 |
| `CommitCredits` | Business Credit | `credit_hold_id` | 资产提交成功后扣减 |
| `ReleaseCredits` | Business Credit | `credit_hold_id` | 失败或取消释放 |
| `CommitGeneratedAssets` | Business Asset | `tool_task_id + asset_digest` | 提交 TOS 和资产元数据 |
| `GetToolPricing` | Business Tool | `tool_id + tool_version` | 返回价格策略 |

## 数据模型范围

| 表 | Owner | 写入方 | 用途 |
| --- | --- | --- | --- |
| `tool_plans` | Agent Runtime | Agent Runtime | ToolPlan digest、board_version、估算 |
| `tool_tasks` | Agent Runtime / Tool Runtime | Agent Runtime / Worker | provider 执行状态 |
| `credit_holds` | Business Credit | Business Service | 冻结记录 |
| `credit_ledger_entries` | Business Credit | Business Service | 扣减、释放、退款流水 |
| `generated_assets` | Business Asset | Business Service | 资产元数据 |
| `asset_commit_records` | Business Asset | Business Service | 资产提交幂等和 partial success |

## Redis 使用

| 类型 | Key / Stream | 用途 | 约束 |
| --- | --- | --- | --- |
| cache | `tool:pricing:{tool_id}:{version}` | Tool 价格缓存 | TTL 5 分钟，价格变更主动失效 |
| stream | `tool:task:requested` | worker 消费任务 | 至少一次投递，任务幂等 |
| stream | `tool:task:completed` | provider 完成事件 | 按 `tool_task_id` 去重 |
| lock | `lock:credit:hold:{idempotency_key}` | 防并发重复冻结 | 失败必须可重试 |
| cache | `asset:commit:{asset_digest}` | 资产提交去重 | DB 为事实源 |

## 两阶段 CostDisclosure 约束

Tool 生成费确认不得替代 Skill 使用费确认。默认顺序必须是：

```text
Skill 使用费确认
  -> Graph / Board 交付
  -> Board approved
  -> ToolPlan 估算
  -> Tool 生成费确认
```

combined confirmation 只允许在 ToolPlan 已存在且 Skill usage 尚未确认时走例外路径。后端必须先 `FreezeSkillUsageCredits`，再 `FreezeCredits`；第二步失败必须释放第一步冻结。

## Fixture 要求

| Fixture | 必须覆盖 |
| --- | --- |
| `toolplan/city_video_toolplan.json` | ToolPlan digest 绑定 board_version |
| `credit/freeze_commit_success.json` | freeze -> commit |
| `credit/freeze_release_on_tool_failure.json` | freeze -> release |
| `asset/partial_commit_success.json` | 部分资产成功、失败资产不扣费 |
| `tool/provider_async_resume.json` | worker 重启后任务恢复 |

## Done Gate

- [x] ToolPlan schema 冻结并绑定 `board_id`、`board_version`、`graph_plan_id`。
- [x] Tool 生成费 AG-UI payload 冻结。
- [x] Credit freeze / commit / release RPC 和 SQL fixture 冻结。
- [x] Asset commit partial success 契约冻结。
- [x] Redis worker 恢复、重复事件去重和 provider async policy fixture 明确。
- [x] `python3 tests/contract/validate_pr3_contracts.py` 通过。
- [x] 真实 PostgreSQL migration dry-run 和 down-test 已由 Agent / Business repository integration tests 覆盖，并会被 PR-0 `go test ./services/... ./internal/...` 执行。
- [ ] 真实 Business RPC adapter、Redis worker 和 provider fake/real runtime 接入。
