# PR-2 Agent Runtime / Board / Graph 基础实现

状态：active
owner：Agent Runtime / 文档与契约责任域 / 测试与验收责任域
更新时间：2026-07-01
适用范围：PR-2 字段级契约在 Go 运行时的 Board、Patch、Snapshot、GraphPlan、Generic Creation Graph、Eino Graph runner、AG-UI payload 基础校验、Agent DB 持久化、Board/Graph HTTP API、Redis stream/cache/lock 原语和部署 wiring
相关代码路径：`internal/contracts/boardgraph/**`、`services/agent/internal/runtime/creation/**`、`services/agent/internal/runtime/eino/**`、`services/agent/internal/infra/repository/board_graph.go`、`services/agent/internal/api/http/workbench_handlers.go`、`services/agent/internal/events/stream/**`、`services/agent/internal/infra/config/**`、`services/agent/cmd/agent/main.go`
相关契约：`docs/active/contracts/pr-2-agent-runtime-contracts.md`、`api/schemas/board/**`、`api/schemas/graph/**`、`api/agui/events/board.*`、`api/agui/events/graph.*`

## 背景

PR-2 已冻结 Agent Runtime 的 Board / Storyboard / GraphPlan 字段级契约。进入真实 Agent Runtime 服务实现前，需要先提供 Go 运行时可复用的契约包，避免后续 DB、Redis、Eino Graph 和 AG-UI 事件各自手写字段。

## 目标

- 提供 CreativeBoard、CreativeElement、BoardPatch、BoardSnapshot Go 类型和校验。
- 提供 GraphTemplate、GraphPlan、GraphCheckpoint、Generic Creation Graph L0 fallback Go 类型和校验。
- 提供 Board / Graph AG-UI payload 类型和校验，并复用 PR-1 AG-UI Envelope。
- 使用 active fixture 回归测试 Board 创建、Patch replay、Board approval、Generic GraphPlan 和 interrupt resume。

## 非目标

- 不实现 ToolPlan、积分冻结、资产提交和 Marketplace。
- 不接真实 provider。
- 不在本 PR 默认强制启用真实 Redis 运行环境；真实环境连接由部署配置和 PR-5 gate 控制。
- 不改写旧工作台事件兼容层。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Board | `internal/contracts/boardgraph/board.go` | Board、Element、Patch、Snapshot 类型和状态校验 |
| Graph | `internal/contracts/boardgraph/graph.go` | Generic L0 Graph、GraphTemplate、GraphPlan、Checkpoint 类型和校验 |
| AG-UI | `internal/contracts/boardgraph/agui.go` | Board / Graph payload 类型，复用 PR-1 Envelope |
| Runtime | `services/agent/internal/runtime/creation/**`、`services/agent/internal/runtime/skillgraph/**` | Generic Creation Graph L0 fallback 与 Published Skill Graph 的确定性执行内核、Board 草稿和 approval gate |
| Eino Graph | `services/agent/internal/runtime/eino/**` | 使用 Eino `compose.Graph` 编译 Generic Creation Graph runner 和 Skill Graph runner；M2 对齐 `brief_parser -> clarifier -> creative_direction -> board_writer -> skill_recommendation`，M3 通过 `skill_graph_compile` 输出 Skill GraphPlan |
| Repository | `services/agent/internal/domain/model/board_graph.go`、`services/agent/internal/infra/repository/board_graph.go` | PR-2 active 表 record、AgentRun/GraphPlan/Board/Element/Event 持久化、Board approval 幂等更新、Workbench BoardGraph 状态保存 |
| HTTP/API | `services/agent/internal/application/workbench/app.go`、`services/agent/internal/api/http/workbench_handlers.go` | Board snapshot 查询、after-state BoardPatch、Board approve、GraphPlan 查询，按 project access 做权限校验 |
| Stream/Cache/Lock | `services/agent/internal/events/stream/agui_runtime.go` | `agent:run:{run_id}:events`、run/board snapshot cache、turn lock 的内存和 Redis 实现 |
| Runtime Redis Wiring | `services/agent/internal/infra/config/**`、`services/agent/cmd/agent/main.go`、`.env.example` | `AGENT_RUNTIME_REDIS_MODE=memory|redis`，启动时注入 AG-UI event bus、snapshot cache 和 turn lock |
| Tests | `internal/contracts/boardgraph/*_test.go` | 读取 PR-2 active fixture 做漂移防护 |

## 开发注意事项

1. Generic Creation Graph 固定为平台内置 L0 fallback，不允许绑定 Marketplace listing，不允许收费。
2. Board approved 只表示 PR-3 ToolPlan 可以生成，不得在 PR-2 合并 Tool 生成费确认。
3. BoardPatch 必须按 `base_version -> target_version` 单步递增并携带 `idempotency_key`。
4. AG-UI 事件必须继续使用 PR-1 Envelope、`payload_schema_version` 和 `dedupe_key` 规则。
5. Redis stream、cache 和 lock 必须使用 active 契约中的 key 命名；DB 仍是 replay 和 snapshot 的事实源。
6. `/boards/{board_id}/patches` 的通用 patch handler 只接受 after-state payload：`patch.payload.board_after`、`patch.payload.elements_after`，可选 `changed_element_ids`；`approve_board` 必须走 `/boards/{board_id}/approve`。

## Done Gate

- [x] `internal/contracts/boardgraph` 包存在。
- [x] Board 创建 fixture 校验通过。
- [x] Board patch replay fixture 校验通过。
- [x] Board approval fixture 校验通过，且不会跳过 PR-3 ToolPlan gate。
- [x] Generic Creation Graph L0 fallback fixture 校验通过。
- [x] Graph interrupt resume fixture 校验通过。
- [x] PR-2 AG-UI payload 可使用 PR-1 Envelope 构造并校验。
- [x] Generic Creation Graph L0 fallback runtime 内核可生成 GraphPlan、Board、Snapshot 和 PR-1 AG-UI 事件。
- [x] Board approval gate 可生成 `approve_board` patch，且不会绕过 PR-3 ToolPlan 估算和确认。
- [x] Agent Runtime DB repository 可持久化 Generic Creation 状态、replay run events、读取 Board snapshot / GraphPlan。
- [x] Board / Graph HTTP API 可查询 PR-2 active 数据，并可幂等批准 Board 推进 run 到 PR-3 planning。
- [x] PR-2 active run 可通过 `/api/agent/runs/{run_id}/events` 回放 `agent_run_events`，Board approve 会追加 `board.patch.applied` 和 `board.snapshot.updated`。
- [x] Redis stream/cache/lock key 规范和内存/Redis 实现已提供，支持本地测试与后续真实环境注入。
- [x] 通用 BoardPatch handler 已接入 after-state patch，应用成功后追加 `board.patch.applied` 和 `board.snapshot.updated`。
- [x] Eino Generic Creation Graph runner 已接入，节点顺序与 PR-2 GraphTemplate 对齐，并复用确定性 runtime 输出 PR-2 契约对象。
- [x] Agent Runtime Redis wiring 已接入配置和 main；默认 memory，`redis` 模式启动时 ping 后注入 AG-UI event bus、snapshot cache 和 turn lock。
- [x] M1 `normal/select_skill` 工作台入口已接入 M2 Generic Creation Graph，免费系统 Skill / 通用创作会创建 Board、GraphPlan、PR-2 AG-UI 事件，并停在 Board review / approve gate；不触发 Tool、积分估算或冻结。
- [x] M3 `select_skill` 免费系统 Skill 已接入 Published SkillRuntimeSpec 加载、Eino SkillGraphRunner、Skill GraphPlan、Board 和 Snapshot 生成；付费 Skill 仍停在 Skill 使用费确认门前，不触发 Tool、积分估算或冻结。

## 验证命令

```bash
go test ./internal/contracts/boardgraph
go test ./services/agent/internal/runtime/creation
go test ./services/agent/internal/runtime/eino
go test ./services/agent/internal/infra/repository
go test ./services/agent/internal/api/http
go test ./services/agent/internal/events/stream
go test ./services/agent/internal/infra/config
make active-contract-gate
make development-ci-gate
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/boardgraph
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph
active contract gate passed
PR-0 CI gate passed
```

2026-07-01 已新增 PR-2 Runtime Redis wiring 并执行：

```bash
go test ./services/agent/internal/infra/config ./services/agent/internal/application/workbench ./services/agent/internal/api/http ./services/agent/cmd/agent
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
?  github.com/FigoGoo/Dora-Agent/services/agent/cmd/agent [no test files]
```

2026-07-01 PR-2 Runtime Redis wiring 收口后执行：

```bash
go test ./services/agent/internal/runtime/eino ./services/agent/internal/runtime/creation ./services/agent/internal/events/stream ./services/agent/internal/infra/config ./services/agent/internal/infra/repository ./services/agent/internal/application/workbench ./services/agent/internal/api/http ./services/agent/cmd/agent
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
?  github.com/FigoGoo/Dora-Agent/services/agent/cmd/agent [no test files]
active contract gate passed
PR-0 CI gate passed
```

2026-07-01 已新增 M1 -> M2 工作台入口闭环并执行：

```bash
go test ./services/agent/internal/application/workbench -run 'TestM2NormalRunRoutesAndCreatesBoardWithoutToolOrCredit|TestM1EntryGuideRunEmitsGuideOnly|TestM1AmbiguousInputClarifiesWithoutGeneration'
go test ./services/agent/internal/application/workbench ./services/agent/internal/infra/repository ./services/agent/internal/api/http
npm test -- --run src/features/agent/agui.test.js
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
frontend AG-UI reducer tests passed
active contract gate passed
PR-0 CI gate passed
```

2026-07-01 已新增 PR-2 runtime 内核并执行：

```bash
go test ./services/agent/internal/runtime/creation
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation
```

2026-07-01 已新增 PR-2 Agent DB repository 并执行：

```bash
go test ./services/agent/internal/infra/repository
go test ./services/agent/internal/runtime/creation
make active-contract-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation
active contract gate passed
```

2026-07-01 已新增 PR-2 Board/Graph HTTP API 并执行：

```bash
go test ./services/agent/internal/api/http
go test ./services/agent/internal/application/workbench
go test ./services/agent/internal/infra/repository
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
```

2026-07-01 已新增 PR-2 AG-UI replay fallback 与 Redis stream/cache/lock 原语并执行：

```bash
go test ./services/agent/internal/api/http
go test ./services/agent/internal/infra/repository
go test ./services/agent/internal/events/stream
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream
```

2026-07-01 已新增 PR-2 after-state BoardPatch handler 并执行：

```bash
go test ./services/agent/internal/api/http
go test ./services/agent/internal/infra/repository
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
```

2026-07-01 已新增 PR-2 Eino Generic Creation Graph runner 并执行：

```bash
go test ./services/agent/internal/runtime/eino
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino
```

2026-07-01 PR-2 Eino runner 收口后执行：

```bash
go test ./services/agent/internal/runtime/eino ./services/agent/internal/runtime/creation ./services/agent/internal/infra/repository ./services/agent/internal/api/http ./services/agent/internal/events/stream
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream
active contract gate passed
PR-0 CI gate passed
```

2026-07-01 已新增 M3 Skill Graph runtime 并执行：

```bash
go test ./services/agent/internal/runtime/skillgraph ./services/agent/internal/runtime/eino ./services/agent/internal/application/workbench
go test ./services/agent/internal/runtime/skillgraph ./services/agent/internal/runtime/eino ./services/agent/internal/runtime/creation ./services/agent/internal/infra/repository ./services/agent/internal/api/http ./services/agent/internal/application/workbench
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skillgraph
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http
active contract gate passed
PR-0 CI gate passed
```
