# PR-5 E2E Fixtures + Fake Provider + Release Gates

状态：active  
PR 状态：active / release gate frozen  
owner：测试与验收责任域 / Agent Runtime / Business Service / 前端 / 管理端 / 运维发布责任域  
更新时间：2026-07-01  
适用范围：端到端验收、Fake Provider、发布治理、灰度、回滚、观测、数据修复和 release gate  
来源设计源：`07-M6-前后台体验与端到端验收.md`、`08-M7-契约拆分迁移发布与运营治理.md`

## 目标

PR-5 把 PR-2 到 PR-4 的字段级契约串成可回放、可验证、可发布、可回滚的端到端闭环。它不新增业务字段，只冻结测试事实源、fake provider 行为和 release gate。

当前裁决：

```text
PR-5 E2E fixture、fake provider 行为、发布治理、轻量 validator、本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent 独立进程 HTTP smoke 和 Business 独立进程 HTTP smoke 已冻结。
真实浏览器、前端联动和完整测试环境 HTTP 服务 E2E 仍是进入真实流量前的发布 gate。
```

## 非目标

- 不新增未在 PR-2 到 PR-4 冻结过的业务字段。
- 不绕过 fake provider 直接连真实 provider。
- 不以人工验收替代合同 fixture 和自动化 gate。

## Canonical Files

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Fake Provider | `tests/e2e/fake-provider/**` | 模型、图片、视频、音频、失败注入 |
| Workspace E2E | `tests/e2e/agent-workspace/**` | 工作台主路径 |
| Marketplace E2E | `tests/e2e/skill-marketplace/**` | 市场安装与付费 Skill |
| Admin E2E | `tests/e2e/admin-governance/**` | 审核、暂停、退款 |
| Release Governance | `docs/active/technical/release-governance.md` | feature flag、灰度、回滚、观测 |
| Fixtures | `tests/fixtures/e2e/**` | E2E 输入输出 |
| Validator | `tests/contract/validate_pr5_e2e_gates.py` | PR-5 gate |
| Service E2E | `services/business/internal/e2e/pr5/service_e2e_test.go` | 本地 PostgreSQL 串联 PR-2/PR-3/PR-4 主路径 |
| Agent HTTP/Redis E2E | `services/agent/internal/e2e/pr5/agent_http_redis_e2e_test.go` | `httptest` Agent HTTP router + testcontainers Redis |
| Agent Process Smoke | `services/agent/internal/e2e/pr5/agent_process_smoke_test.go` | 真实 `cmd/agent` 二进制 + Postgres + Redis + HTTP health/ready |
| Business Process Smoke | `services/business/internal/e2e/pr5/business_process_smoke_test.go` | 真实 `cmd/business` 二进制 + Postgres + Kitex + HTTP health/ready |

## E2E 场景矩阵

| 场景 | 依赖 PR | 必须验证 |
| --- | --- | --- |
| 城市文旅默认 Skill | PR-1 / PR-2 / PR-3 | Router 命中、Board、Storyboard、ToolPlan、资产提交 |
| Generic Creation Graph fallback | PR-1 / PR-2 | 未命中具体 Skill 时进入 L0，Skill 使用费为 0 |
| 付费市场 Skill 使用 | PR-1 / PR-2 / PR-3 / PR-4 | usage record 预创建、Skill 使用费确认、Tool 生成费确认 |
| 企业安装 pinned Skill | PR-4 | pinned 版本、升级确认、历史 run snapshot |
| Tool partial failure | PR-2 / PR-3 | 部分资产成功、失败任务释放冻结 |
| listing suspended | PR-4 | 暂停 listing 后不可新安装，历史 run 可恢复 |
| refund and settlement reverse | PR-4 | 退款审批、账务 reverse、结算 hold 释放 |
| replay after restart | PR-1 / PR-2 / PR-3 | AG-UI replay、worker 恢复、重复事件去重 |

## Fake Provider 策略

| Provider 行为 | 必须支持 |
| --- | --- |
| deterministic success | 同输入返回固定 digest |
| async pending | 模拟长任务轮询和 callback |
| partial success | 多资产任务部分成功 |
| transient failure | 可重试失败 |
| terminal failure | 不可重试失败并释放积分 |
| slow callback | 验证超时、恢复和幂等 |

## Release Gate

| Gate | 要求 |
| --- | --- |
| Contract Gate | PR-1 到 PR-4 validator 全部通过 |
| Migration Gate | agent / business migration dry-run、down-test、数据兼容检查通过 |
| Fixture Gate | contract fixture 和 E2E fixture 全部通过 |
| Fake Provider Gate | 所有 provider 成功、失败、partial、超时场景通过 |
| Feature Flag Gate | 新 Agent Runtime、Tool generation、Marketplace 三类 flag 可独立开关 |
| Observability Gate | run、graph、tool、credit、marketplace、settlement 指标和 trace 存在 |
| Rollback Gate | 可关闭新入口、停止 worker、释放未完成冻结、恢复旧路由 |

## 观测指标

| 指标 | 说明 |
| --- | --- |
| `agent_run_success_rate` | run 成功率 |
| `router_decision_latency_ms` | Router 延迟 |
| `board_patch_replay_error_count` | Board replay 错误 |
| `graph_resume_failure_count` | Graph 恢复失败 |
| `tool_task_success_rate` | Tool task 成功率 |
| `credit_freeze_leak_count` | 冻结未释放数量 |
| `skill_usage_charge_error_count` | Skill 使用费扣费错误 |
| `marketplace_install_failure_count` | 安装失败 |
| `settlement_reverse_count` | 结算 reverse |

## 回滚要求

回滚必须满足：

1. 关闭新 Router / Marketplace / Tool generation feature flag。
2. 停止消费 `tool:task:requested` 等新 worker stream。
3. 释放所有未进入 `charged` 的 credit hold。
4. 保留已生成资产和已扣费 ledger，不做物理删除。
5. AG-UI replay 可展示回滚前的历史事件。
6. 生成回滚审计记录。

## Done Gate

- [x] 城市文旅默认 Skill E2E fixture 通过。
- [x] Generic Creation Graph fallback E2E fixture 通过。
- [x] 付费市场 Skill E2E fixture 通过。
- [x] 企业安装 Skill E2E fixture 通过。
- [x] Tool partial failure E2E fixture 通过。
- [x] listing suspended E2E fixture 通过。
- [x] refund / settlement reverse E2E fixture 通过。
- [x] replay after restart E2E fixture 通过。
- [x] fake provider scenarios 可执行且幂等。
- [x] service-level PostgreSQL E2E 覆盖 ToolPlan、Credit、Asset、Marketplace、SkillUsage、Settlement、listing suspend 和 worker replay。
- [x] Agent HTTP router + Redis container E2E 覆盖健康检查、AG-UI Redis replay、stream dedupe、snapshot cache 和 turn lock。
- [x] Agent 独立进程 HTTP smoke 覆盖真实 `cmd/agent` 启动、PostgreSQL、Redis runtime、`/healthz` 和 `/readyz`。
- [x] Business 独立进程 HTTP smoke 覆盖真实 `cmd/business` 启动、PostgreSQL、Kitex、`/healthz` 和 `/readyz`。
- [x] release、rollback、feature flag、观测和数据修复 gate 明确。
- [x] `python3 tests/contract/validate_pr5_e2e_gates.py` 通过。
- [x] `go test ./services/business/internal/e2e/pr5` 通过。
- [x] `go test ./services/agent/internal/e2e/pr5` 通过。
- [ ] 真实浏览器、前端联动和完整测试环境 HTTP 服务 E2E 执行并归档测试报告。
