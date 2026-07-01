# PR-2 Agent Runtime Contracts

状态：active  
PR 状态：active / contract frozen  
owner：Agent Runtime / 文档与契约责任域 / 前端 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：CreativeBoard、BoardPatch、GraphTemplate、GraphPlan、Generic Creation Graph、Agent DB migration、AG-UI replay  
来源设计源：`02-M1-CreativeGuide与ChatModelRouter.md`、`03-M2-CreativeBoard与AGUI事件闭环.md`、`04-M3-SkillRuntimeSpec与EinoGraphPlan.md`、`07-M6-前后台体验与端到端验收.md`

## 目标

PR-2 把 Agent Runtime 的可执行契约拆清楚，让用户从创作意图进入 Board / Storyboard / GraphPlan 的主路径闭环，并把 Generic Creation Graph 正式内置为平台 L0 fallback。

## 非目标

- 不实现 Tool 生成、积分冻结、资产提交。
- 不实现 Marketplace 安装、创作者后台、结算。
- 不接真实 provider。

## Canonical Files

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Agent OpenAPI | `api/openapi/agent-workbench.yaml` | Run、message、board、graph、event replay API |
| Board Schema | `api/schemas/board/creative-board.v1.schema.json` | Board 基础字段 |
| Element Schema | `api/schemas/board/creative-element.v1.schema.json` | Board 元素字段 |
| Patch Schema | `api/schemas/board/board-patch.v1.schema.json` | patch 操作和幂等 |
| Snapshot Schema | `api/schemas/board/board-snapshot.v1.schema.json` | replay 快照 |
| GraphTemplate Schema | `api/schemas/graph/graph-template.v1.schema.json` | Eino Graph 模板 |
| GraphPlan Schema | `api/schemas/graph/graph-plan.v1.schema.json` | 运行期 GraphPlan |
| GraphCheckpoint Schema | `api/schemas/graph/graph-checkpoint.v1.schema.json` | 中断恢复 |
| Generic Graph Spec | `api/schemas/graph/generic-creation-graph.v1.schema.json` | 内置 L0 fallback |
| AG-UI Payloads | `api/agui/events/board.*.schema.json`、`api/agui/events/graph.*.schema.json` | Board / Graph 流式事件 |
| Agent Migration | `db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/**` | Agent Runtime 表 |
| Fixtures | `tests/fixtures/contracts/board/**`、`tests/fixtures/contracts/graph/**` | replay、digest、checkpoint |
| Validator | `tests/contract/validate_pr2_contracts.py` | PR-2 contract gate |

## API 契约范围

| API | 责任域 | 说明 |
| --- | --- | --- |
| `POST /api/agent/runs` | Agent Runtime | 创建 run，绑定 router decision |
| `POST /api/agent/runs/{run_id}/messages` | Agent Runtime | 追加用户输入，支持 turn loop |
| `GET /api/agent/runs/{run_id}` | Agent Runtime | 查询 run 当前状态 |
| `GET /api/agent/runs/{run_id}/events` | Agent Runtime | AG-UI replay |
| `GET /api/agent/boards/{board_id}` | Agent Runtime | 查询 Board snapshot |
| `POST /api/agent/boards/{board_id}/patches` | Agent Runtime | 应用 after-state BoardPatch，要求 idempotency_key，payload 必须携带 `board_after`、`elements_after` |
| `POST /api/agent/boards/{board_id}/approve` | Agent Runtime | 用户批准 Board，作为 PR-3 ToolPlan 前置 |
| `GET /api/agent/graphs/{graph_plan_id}` | Agent Runtime | 查询 GraphPlan |

## Agent DB 表范围

| 表 | Owner | 写入方 | 用途 |
| --- | --- | --- | --- |
| `agent_runs` | Agent Runtime | Agent Runtime | run 状态、router decision、trace |
| `agent_run_events` | Agent Runtime | Agent Runtime | AG-UI 事件持久化和 replay |
| `creative_boards` | Agent Runtime | Agent Runtime | Board 元数据和当前版本 |
| `creative_elements` | Agent Runtime | Agent Runtime | Board 元素 |
| `board_patches` | Agent Runtime | Agent Runtime | patch 历史、幂等、replay |
| `graph_templates` | Agent Runtime | Agent Runtime | 内置/系统 Graph 模板 |
| `graph_plans` | Agent Runtime | Agent Runtime | GraphPlan digest 和执行阶段 |
| `graph_checkpoints` | Agent Runtime | Agent Runtime | interrupt / resume checkpoint |

## Generic Creation Graph L0 Spec

Generic Creation Graph 必须作为平台内置 L0 fallback，不进入市场、不收 Skill 使用费。

固定能力：

1. 解析 brief。
2. 追问缺失信息。
3. 生成创作方向。
4. 生成 Storyboard / Board 草稿。
5. 推荐可安装 Skill。
6. 不默认调用 Tool 生成。

固定字段：

```text
generic_graph_id = "generic_creation_graph"
skill_level = "L0"
pricing_policy = free
marketplace_listing_id = null
usage_fee = 0
version_strategy = platform_builtin
```

## Redis 使用

| 类型 | Key / Stream | 用途 | 约束 |
| --- | --- | --- | --- |
| cache | `agent:run:{run_id}:snapshot` | run 快照缓存 | TTL 30 分钟，DB 为事实源 |
| cache | `agent:board:{board_id}:snapshot:{version}` | Board 快照缓存 | 写 patch 后失效 |
| stream | `agent:run:{run_id}:events` | AG-UI 流式事件 | `seq` 单调递增 |
| lock | `lock:agent:run:{run_id}:turn` | turn loop 并发锁 | 超时释放，失败可重试 |

## Fixture 要求

| Fixture | 必须覆盖 |
| --- | --- |
| `board/create_city_tourism_board.json` | run -> board 创建 |
| `board/patch_replay_storyboard.json` | patch replay 后 snapshot 一致 |
| `board/approve_board_for_toolplan.json` | Board approved 后可进入 PR-3 |
| `graph/generic_creation_graph_plan.json` | L0 fallback GraphPlan digest |
| `graph/interrupt_resume_checkpoint.json` | 中断恢复 |

## Done Gate

- [x] OpenAPI 覆盖 Run、Message、Board、Graph、Events。
- [x] Board / Element / Patch / Snapshot schema 冻结。
- [x] GraphTemplate / GraphPlan / Checkpoint schema 冻结。
- [x] Generic Creation Graph L0 fallback schema 和 seed fixture 冻结。
- [x] Agent migration 文件存在且静态 guard 通过，不含数据库级外键。
- [x] Board replay fixture 证明 snapshot 可恢复。
- [x] GraphPlan digest fixture 证明同输入同 digest。
- [x] `python3 tests/contract/validate_pr2_contracts.py` 通过。
- [x] 真实 PostgreSQL migration dry-run 和 down-test 已由 `services/agent/internal/infra/repository` 集成测试覆盖。
