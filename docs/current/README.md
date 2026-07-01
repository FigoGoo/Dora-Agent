# 当前文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 核心重构当前事实源导航

## 当前事实源状态

旧项目内产品、技术、契约、测试、设计和阶段计划文档已从当前仓库文档事实源中清理，不再作为当前 Agent 核心重构事实源。

如需追溯本次重构之前的历史设计，只能通过 git 历史查询；当前任务读取链路不再保留本地历史设计副本。

当前重构依据：

- 用户提供的外部设计文档：`/Users/figo/Downloads/AIGC_Creation_Agent v1_1.md`
- 唯一 review 设计文档：`docs/review/aigc-agent-refactor/2026-07-01/`
- Contract-first active 事实源入口：`docs/active/README.md`

当前裁决：

```text
review 已完成并通过
P0 文档补丁已完成
已进入 M7 active 契约拆分
M0 / PR-1 active 最小契约已冻结
PR-2 Agent Runtime Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-3 Tool/Credit/Asset Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-4 Marketplace Contracts 字段级契约已冻结，本地真实 PostgreSQL dry-run / down-test 已完成，远端 CI gate 待 PR 运行确认
PR-5 E2E Fixtures + Fake Provider + Release Gates 已冻结，本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke 已完成，测试环境 HTTP 服务 E2E 自动化入口已完成，完整测试环境执行与报告归档待测试环境 gate
PR-1 到 PR-5 active 拆分已完成
已进入 PR-0 开发准备与 CI Gate
PR-1 到 PR-5 共享契约运行时基础实现已完成并纳入 PR-0 gate
M1 Creative Guide / ChatModel Router 基础业务闭环已实现：显式 run_intent 入口支持 entry_guide、capability_question、normal、select_skill；不触发 Tool、积分或真实 provider
M2 Generic Creation Graph L0 fallback 已实现：generic_creation 路径创建 Board、GraphPlan、Snapshot 和 PR-2 AG-UI 事件，并停在 Board review / approve gate
M3 Published Skill Graph 已实现：免费系统 Skill 的 select_skill 路径加载 Published SkillRuntimeSpec，经 Eino SkillGraphRunner 生成 Skill GraphPlan / Board / Snapshot；付费 Skill 仍停在 Skill 使用费确认门前
M4 ToolPlan / Tool Generation CostDisclosure 已实现：Board approved 后生成 ToolPlan、披露 Tool 生成费、创建 tool_generation_confirmation；用户确认后按 tool_plan_digest 冻结积分，执行生成、ToolTask、资产提交、扣费和 PR-3 AG-UI 事件闭环
M5 Marketplace / SkillUsage 已继续推进：Business Marketplace 应用层、用户端 Marketplace HTTP 主路径、Creator Portal HTTP 主路径、Admin Governance HTTP 主路径、真实 Marketplace RPC adapter、用户端 Skill 市场前台安装/使用路径、创作者 Skill 草稿/提交审核后台、管理端 Skill 审核 / listing 暂停 / 退款反转 / settlement hold 查看 / 内部出账确认页面、个人 latest 安装、SkillUsageRecord 预创建、确认后冻结、交付扣费、冻结释放、settlement hold 和内部 settlement ledger 出账治理已完成；外部真实打款通道仍待后续阶段
M1-M6 业务代码开发按 PR-1 到 PR-5 顺序受控启动，真实 provider 流量必须等待完整测试环境 HTTP 服务 E2E 执行与报告归档 gate
```

## 当前可读取文档

| 类型 | 路径 | 用途 |
| --- | --- | --- |
| 文档规范 | `docs/standards/文档规范.md` | 新文档状态、路径、owner 和归档规则 |
| 开发流程 | `docs/standards/开发流程规范.md` | 新功能开发前置约束 |
| 编码规范 | `docs/standards/编码规范.md` | 代码、契约和测试同步约束 |
| Review 入口 | `docs/review/README.md` | 已通过 review 设计源导航 |
| Review 设计源 | `docs/review/aigc-agent-refactor/2026-07-01/` | 本轮已通过 review 的设计依据 |
| Active 事实源 | `docs/active/README.md` | M7 active 拆分和字段级契约冻结入口 |
| 契约索引 | `docs/active/contracts/index.md` | 字段级事实源索引 |
| PR-0 CI Gate | `docs/active/technical/pr-0-development-ci-gate.md` | 开发准备、CI、本地验证和后续开发准入 |
| 模板 | `docs/templates/README.md` | 新产品、技术、契约和测试文档模板 |
| 历史清理 | `docs/archive/README.md` | 历史设计副本清理记录 |

## 新文档规则

- 新产品、技术、契约、SQL、测试和前端设计文档必须重新创建。
- 新文档状态先使用 `draft` 或 `review`；确认可作为事实源后再改为 `active`。
- 字段级事实源必须落在 Thrift、OpenAPI、AG-UI JSON Schema、migration 和 fixture。
- 当前已完成 M7 active 契约拆分；`docs/active/**`、`api/schemas/**`、`api/agui/**`、`api/openapi/**`、`api/thrift/**`、`db/migrations/**`、`tests/fixtures/contracts/**`、`tests/e2e/**`、`tests/fixtures/e2e/**` 中的 PR-1 / PR-2 / PR-3 / PR-4 / PR-5 范围作为当前字段级事实源。
- M1 业务实现入口为 `docs/active/technical/router.md`，字段级事实源仍以 `api/openapi/agent-workbench.yaml`、`api/schemas/router/**`、`api/agui/events/creative.*.schema.json` 和 contract fixtures 为准。
- M2 / M3 Agent Runtime 实现入口为 `docs/active/technical/creative-board.md`、`docs/active/technical/eino-graph-runtime.md` 和 `docs/active/technical/pr-2-agent-runtime-board-graph.md`，字段级事实源仍以 `api/schemas/board/**`、`api/schemas/graph/**`、`api/agui/events/board.*`、`api/agui/events/graph.*` 和 contract fixtures 为准。
- M4 Tool / Credit / Asset 实现入口为 `docs/active/technical/pr-3-tool-credit-asset.md`，字段级事实源仍以 `api/schemas/tool/**`、`api/schemas/credit/**`、`api/schemas/asset/**`、`api/agui/events/cost_disclosure.generation.presented.schema.json`、`api/agui/events/tool.task.updated.schema.json`、`api/agui/events/asset.commit.updated.schema.json` 和 PR-3 contract fixtures 为准。
- M5 Marketplace / SkillUsage 实现入口为 `docs/active/technical/pr-4-marketplace-skill-usage-settlement.md`，字段级事实源仍以 `api/openapi/business-api.yaml`、`api/thrift/business_skill_marketplace_service.thrift`、`api/schemas/skill/**`、`api/schemas/settlement/**`、`api/agui/events/cost_disclosure.skill_usage.presented.schema.json` 和 PR-4 contract fixtures 为准。
- M1-M6 业务代码开发按 PR-0 / PR-1 / PR-2 / PR-3 / PR-4 / PR-5 顺序推进；真实 provider 流量必须等待 PR-5 本地双服务 HTTP smoke、完整测试环境 HTTP 服务 E2E 执行与报告归档 gate。
- 本次重构之前的历史设计副本已清理，避免搜索命中旧事实源。
- 如确需复用旧结论，必须从 git 历史中明确迁移来源，并写入本次重构下的新 active 裁决。
