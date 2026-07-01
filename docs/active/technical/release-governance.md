# Agent 核心重构发布治理

状态：active  
owner：运维发布责任域 / 文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：M7 active 拆分后的 E2E、Fake Provider、Feature Flag、灰度、观测、回滚和数据修复  
相关代码路径：`.github/**`、`docker-compose*.yml`、部署脚本、`configs/release/governance.json`、`tests/e2e/**`、`tests/fixtures/e2e/**`、`tests/contract/validate_release_governance_manifest.py`
相关契约：`docs/active/contracts/pr-roadmap.md`、`docs/active/contracts/pr-5-e2e-fixtures-release-gates.md`、`docs/active/contracts/index.md`

## 背景

Agent 核心重构横跨 Agent Runtime、业务微服务、前端、管理端、积分、Tool、Skill 市场和结算。review 文档已经完成，PR-1 到 PR-4 冻结字段级契约，PR-5 冻结发布前的端到端验收与治理 gate，并已完成本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke，以及 release HTTP 服务 E2E 自动化入口、本地地址执行和报告归档。

## 目标

- 用 fake provider 替代真实 provider 完成确定性 E2E。
- 用 release gate 阻断未验证的真实流量。
- 用 feature flag 支持分域灰度和快速回滚。
- 用观测指标定位 run、graph、tool、credit、marketplace 和 settlement 问题。
- 用回滚与数据修复规则处理未完成冻结、重复事件和失败任务。

## 非目标

- 不在本文新增业务字段。
- 不替代 PR-1 到 PR-4 的字段级契约。
- 不把人工点击验收作为唯一 release gate。
- 不允许未通过 fake provider gate 的路径接入真实 provider。

## 机器可校验事实源

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Release Governance Manifest | `configs/release/governance.json` | feature flag、release gate、观测指标、回滚步骤和数据修复场景的机器可校验事实源 |
| Validator | `tests/contract/validate_release_governance_manifest.py` | 校验 manifest、文档链接、Makefile target 和 active contract gate 接入 |
| Script | `scripts/validate-release-governance.sh` | 发布治理 gate 脚本入口 |
| Make Target | `make release-governance-gate` | 本地和 CI 可复用的发布治理 gate |

## 环境矩阵

| 环境 | 用途 | 部署方式 | 配置来源 | 数据库 | 访问控制 |
| --- | --- | --- | --- | --- | --- |
| dev | 契约与 fixture 本地验证 | 本地服务 / fake provider | `.env.local` 示例值 | 本地 PostgreSQL / Redis | 开发账号 |
| test | E2E、迁移 dry-run、回滚演练 | 单机或 CI compose | CI secret / 测试配置 | 测试 PostgreSQL / Redis | 测试账号和管理员账号 |
| prod | 灰度和正式流量 | CentOS 8 单机发布 | 生产 secret | 生产 PostgreSQL / Redis | 最小权限 |

## Feature Flag Gate

| Flag | 默认 | 控制范围 | 回滚动作 |
| --- | --- | --- | --- |
| `agent_runtime_v2` | off | 新 Router、Graph、Board、AG-UI replay | 切回旧入口，停止新 run 创建 |
| `tool_generation_v2` | off | ToolPlan、ToolTask、provider worker、资产提交 | 停止 `tool:task:requested` 消费，释放未扣费冻结 |
| `marketplace_v2` | off | Skill 市场、安装、SkillUsageRecord、结算 hold | 关闭新安装和付费 Skill 入口，保留历史 replay |
| `creator_portal_v2` | off | 创作者发布后台 | 关闭草稿提交和审核入口 |
| `admin_governance_v2` | off | Skill 审核、暂停、退款治理 | 关闭新增审批入口，保留只读查询 |

## 流水线

| 阶段 | 触发条件 | 命令 | 产物 | 阻断条件 |
| --- | --- | --- | --- | --- |
| Contract Gate | PR 或合并前 | `python3 tests/contract/validate_foundation_contracts.py && python3 tests/contract/validate_board_graph_contracts.py && python3 tests/contract/validate_tool_asset_contracts.py && python3 tests/contract/validate_skill_market_contracts.py` | 契约校验日志 | schema、OpenAPI、Thrift、migration static guard 或 fixture 任一失败 |
| Migration Gate | test 环境发布前 | PostgreSQL dry-run、down-test、数据兼容检查 | migration 报告 | PR-2 / PR-3 / PR-4 migration 无法重放 |
| Fixture Gate | E2E 前 | `python3 tests/contract/validate_release_e2e_gates.py` | PR-5 fixture 报告 | fake provider、E2E fixture 或 release gate 缺失 |
| Service E2E Gate | 发布前 | `go test ./services/business/internal/e2e/release` | 本地 PostgreSQL 服务级 E2E 报告 | PR-2 / PR-3 / PR-4 主路径串联失败 |
| Agent / Business HTTP Gate | 发布前 | `go test ./services/agent/internal/e2e/release ./services/business/internal/e2e/release` | Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 报告 | 健康检查、AG-UI replay、stream dedupe、cache、lock、Agent main 或 Business main 进程启动失败 |
| Local Full HTTP Service Gate | 发布前 | `make release-full-http-smoke` | 本地 Agent + Business 双服务 HTTP smoke 报告 | Business HTTP login、Business Kitex RPC、Agent HTTP run 或 AG-UI replay 失败 |
| Browser Smoke Gate | 发布前 | `make release-browser-smoke` | 本地前后台浏览器 smoke 报告 | 用户端 Skill 市场、创作者提交或管理端结算治理页面失败 |
| Fake Provider Gate | E2E | fake provider 场景套件 | 成功、失败、partial、超时报告 | 任一 required behavior 未覆盖或幂等失败 |
| Test Environment HTTP Service Gate | 真实流量前 | `make release-http-service-e2e` 或本地 harness | `tests/reports/release-http-service-e2e-report.md` 或 CI artifact 中的 HTTP E2E 报告 | health、ready、登录、Agent session/run、RouterDecision、AG-UI replay 失败，或报告不是 `status: passed` |
| Release Governance Manifest Gate | PR 或合并前 | `make release-governance-gate` | `configs/release/governance.json` 和校验日志 | feature flag 默认值、回滚动作、观测阈值、数据修复场景或文档链接缺失 |
| Build Gate | 发布前 | 前端、Agent、业务服务构建命令 | 构建产物 | 任一服务构建失败 |
| Release Gate | 灰度前 | 健康检查、关键 API/RPC/AG-UI smoke | 发布记录 | 指标、trace、日志、告警缺失 |

## 发布步骤

1. 确认 PR-1 到 PR-5 validator 全部通过。
2. 在 test 环境重放 PR-2 / PR-3 / PR-4 PostgreSQL dry-run 和 down-test。
3. 执行本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke，并在 test 环境以 fake provider 执行 PR-5 E2E 场景。
4. 在 test 环境注入 `RELEASE_BUSINESS_BASE_URL`、`RELEASE_AGENT_BASE_URL` 和测试账号，执行 `make release-http-service-e2e`，归档 `status: passed` 报告。
5. 执行 `make release-governance-gate`，确认发布治理 manifest、回滚、观测和数据修复规则仍与 active 文档一致。
6. 默认关闭所有新 feature flag 发布服务。
7. 先灰度 `agent_runtime_v2`，确认 run、router、graph、board 指标稳定。
8. 再灰度 `tool_generation_v2`，确认 provider worker、credit hold 和 asset commit 指标稳定。
9. 最后灰度 `marketplace_v2`、`creator_portal_v2`、`admin_governance_v2`。
10. 每个阶段保留回滚窗口和审计记录。

## Observability Gate

| 指标 | 说明 | 阻断条件 |
| --- | --- | --- |
| `agent_run_success_rate` | run 成功率 | 灰度下降超过阈值 |
| `router_decision_latency_ms` | Router 延迟 | P95 超阈值 |
| `board_patch_replay_error_count` | Board replay 错误 | 任一连续增长 |
| `graph_resume_failure_count` | Graph 恢复失败 | 任一连续增长 |
| `tool_task_success_rate` | Tool task 成功率 | 灰度下降超过阈值 |
| `credit_freeze_leak_count` | 未释放冻结数量 | 大于 0 必须阻断 |
| `skill_usage_charge_error_count` | Skill 使用费扣费错误 | 大于 0 必须阻断 |
| `marketplace_install_failure_count` | 安装失败 | 异常增长阻断 |
| `settlement_reverse_count` | 结算 reverse | 异常增长需人工复核 |

所有关键链路必须写入 `trace_id`，并能按 `run_id`、`tool_plan_id`、`usage_id`、`credit_hold_id`、`installation_id` 查询日志。

## Rollback Gate

回滚必须按以下顺序执行：

1. 关闭 `marketplace_v2`、`tool_generation_v2`、`agent_runtime_v2` 等新入口 flag。
2. 停止消费 `tool:task:requested`、`asset:commit:requested`、`credit:hold:release_requested` 等新 worker stream。
3. 释放所有未进入 `charged` 或 `committed` 的 credit hold。
4. 保留已生成资产、已扣费 ledger、SkillUsageRecord、Settlement，不做物理删除。
5. AG-UI replay 继续展示回滚前历史事件，重复事件通过 `dedupe_key` 去重。
6. 生成回滚审计记录，包含 operator、trace_id、reason、affected_run_ids 和 affected_hold_ids。

## 数据修复

| 场景 | 修复原则 |
| --- | --- |
| ToolTask terminal failure 后 hold 未释放 | 以 `credit_hold_id` 幂等释放，写入审计 |
| duplicate provider callback | 按 stream `dedupe_key` 忽略重复，不重复提交资产 |
| SkillUsageRecord charged 但 settlement 未创建 | 以 `usage_id` 补偿创建 pending_hold |
| settlement reversed 但 creator hold 可结算 | 冻结结算并人工复核 |
| listing suspended 后出现新 installation | 撤销 installation，保留审计，不影响历史 run |

## 验证清单

- [x] Contract Gate 已定义。
- [x] Migration Gate 已定义。
- [x] Fixture Gate 已定义。
- [x] Fake Provider Gate 已定义。
- [x] Test Environment HTTP Service Gate 已定义。
- [x] Feature Flag Gate 已定义。
- [x] Observability Gate 已定义。
- [x] Rollback Gate 已定义。
- [x] 数据修复规则已定义。
- [x] Release Governance Manifest Gate 已定义并接入 `make release-governance-gate`。

## 风险

| 风险 | 影响 | 缓解方式 |
| --- | --- | --- |
| migration gate 只在本地完成，尚未在 test 环境重放 | 字段或索引兼容性问题延后暴露 | 发布前在 test 环境重放 PR-2 / PR-3 / PR-4 dry-run 和 down-test |
| fake provider 与真实 provider 行为差异 | 灰度后长任务失败率上升 | 真实 provider 只能在 fake provider gate 通过后小流量灰度 |
| feature flag 边界不完整 | 回滚时仍有新 worker 消费 | flag 必须覆盖入口、worker 和后台操作 |
| credit hold 泄漏 | 用户积分被冻结 | `credit_freeze_leak_count > 0` 阻断发布并触发修复 |
