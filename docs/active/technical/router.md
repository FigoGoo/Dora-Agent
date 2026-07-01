# M1 Creative Guide / ChatModel Router 技术设计

状态：active  
owner：Agent 服务责任域 / 前端责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：M1 入口引导、能力问答、RouterDecision、Router Guard、AG-UI 消费和 Agent Runtime 快照  
字段级事实源：`api/openapi/agent-workbench.yaml`、`api/schemas/router/**`、`api/agui/events/creative.*.schema.json`、`tests/fixtures/contracts/router/**`

## 当前裁决

M1 基础业务闭环已实现。显式 `run_intent` 入口支持：

```text
entry_guide
capability_question
normal
select_skill
```

M1 只读取业务服务 Skill Catalog，只写 Agent Runtime 数据和 AG-UI 事件。M1 不调用生成模型列表、积分估算、积分冻结、Tool 执行、Board/Graph 生成和资产提交。

旧请求未传 `run_intent` 时继续兼容既有生成链路；M1 前端和新调用必须显式传 `run_intent`。

## 实现入口

| 层 | 路径 | 说明 |
| --- | --- | --- |
| HTTP API | `services/agent/internal/api/http/workbench_handlers.go` | 复用 `POST /api/agent/runs`、SSE replay 和 snapshot 路由 |
| Application | `services/agent/internal/application/workbench/app.go` | `CreateRun` 根据显式 `run_intent` 进入 M1 分支 |
| Creative Guide | `services/agent/internal/runtime/guide/creative_guide.go` | 根据 Skill Catalog 生成 suggestion chips、支持输出类型和默认动作 |
| ChatModel Router | `services/agent/internal/runtime/router/chatmodel_router.go` | 本地 structured router/fake provider 口径，输出 `RouterDecision.v1` |
| Router Guard | `services/agent/internal/runtime/router/chatmodel_router.go` | 校验 published、skill_source、entitlement、marketplace 候选和 safe_to_execute |
| Frontend reducer | `frontend/src/features/agent/agui.js` | 消费 `creative.guide.presented` / `creative.router.decided` 并按 event_id 去重 |
| Workspace UI | `frontend/src/features/landing/LandingPage.jsx` | 独立工作台渲染 guide chips 和 router banner |

## 状态与持久化

M1 显式 run 创建后状态为 `routing`。

| Decision | Run 结果状态 | 写入 |
| --- | --- | --- |
| `entry_guide` | `completed` | assistant message、`creative.guide.presented`、`agent.run.completed` |
| `capability_question` | `completed` | assistant message、guide event、completed event |
| `select_skill` | `completed` | `creative.router.decided`、`agent.skill.selected`、`agent.run.completed` |
| `generic_creation` | `completed` | `creative.router.decided`、`agent.skill.selected`、`agent.run.completed` |
| `clarify` | `waiting_input` | assistant message、`creative.router.decided` |
| `reject` | `failed` | assistant message、failed status |

RouterDecision 快照写入 `agent_runs.skill_selection`，至少包含：

```text
run_intent
router_decision
router_decision_digest
decision
skill_id
skill_version
skill_source
listing_id
confidence
requires_skill_usage_confirmation
entitlement_status
safe_to_execute=false
```

## AG-UI 事件

M1 使用以下事件：

```text
agent.run.started
creative.guide.presented
creative.router.decided
agent.skill.selected
agent.message.completed
agent.run.completed
agent.run.failed
```

前端消费规则：

1. 按 `sequence` 排序。
2. 按 `event_id` 幂等去重。
3. 未识别事件忽略，不阻断已知事件渲染。
4. `creative.guide.presented` 更新 guide chips。
5. `creative.router.decided` 更新 RouterDecision banner。

## Eino 使用说明

M1 保留 ChatModel Router 的结构化输出边界，当前本地实现作为 fake provider / deterministic router 使用，不触发真实 provider 流量。接入真实 Eino ChatModel 时必须保持：

1. 输出先校验 `RouterDecision.v1`。
2. `safe_to_execute` 固定为 `false`。
3. Router 失败降级为 `clarify` 或 `text_answer`。
4. Guard 不允许选择未 published 或不可见 Skill。
5. 真实 provider 流量必须等待 PR-5 完整测试环境 HTTP 服务 E2E gate。

## 验证

已执行：

```bash
go test ./services/agent/internal/application/workbench -run 'TestM1'
go test ./services/agent/internal/application/workbench ./services/agent/internal/runtime/router ./services/agent/internal/runtime/guide ./internal/contracts/pr1
python3 tests/contract/validate_pr1_contracts.py
npm --prefix frontend test -- src/features/agent/agui.test.js src/app/App.test.jsx
```

当前结果：通过。

## 遗留边界

1. M1 当前不接真实 ChatModel provider。
2. `waiting_input` 的追加输入恢复到 Router 的完整链路将在 M2/M3 交接时继续收口。
3. 用户端真实 API/SSE 接入仍受 PR-5 完整测试环境 HTTP 服务 E2E gate 控制。
