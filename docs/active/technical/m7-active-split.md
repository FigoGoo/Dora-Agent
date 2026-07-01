# M7 Active 文档拆分与发布治理

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：AIGC Agent 核心重构从 review 到 active 的 Contract-first 拆分、契约冻结、PR 批次、发布治理  
来源 review：`docs/review/aigc-agent-refactor/2026-07-01/08-M7-契约拆分迁移发布与运营治理.md`

## 裁决

```text
M7 active 文档拆分
  -> M0 active 契约冻结
  -> PR-1 Contract Index + StateEnum + RouterDecision + AG-UI Envelope
  -> PR-2 Agent Runtime Contracts
  -> PR-3 Tool/Credit/Asset Contracts
  -> PR-4 Marketplace Contracts
  -> PR-5 E2E Fixtures + Fake Provider + Release Gates
```

## 非目标

1. 不在 M7 active 拆分阶段实现业务代码。
2. 不让研发直接读取 review 文档编码。
3. 不在未冻结契约前生成最终 Go / TypeScript 领域类型。
4. 不把 Markdown 示例作为字段级事实源。

## 字段级事实源

| 类型 | Canonical 路径 | 当前批次 |
| --- | --- | --- |
| 契约索引 | `docs/active/contracts/index.md` | PR-1 |
| 状态枚举 | `api/schemas/common/state-enum-registry.schema.json` | PR-1 |
| Router JSON Schema | `api/schemas/router/**` | PR-1 |
| AG-UI Envelope / Payload | `api/agui/**` | PR-1 |
| Contract Fixture | `tests/fixtures/contracts/**` | PR-1 起持续补齐 |
| Agent Runtime Schema | `api/schemas/board/**`、`api/schemas/graph/**` | PR-2 |
| Tool / Credit / Asset Schema | `api/schemas/tool/**`、`api/thrift/business_credit_service.thrift`、`api/thrift/business_asset_service.thrift` | PR-3 |
| Marketplace Schema / RPC / API | `api/schemas/skill/**`、`api/openapi/creator-api.yaml`、`api/openapi/admin-api.yaml`、`api/thrift/business_skill_marketplace_service.thrift` | PR-4 |
| SQL Migration | `db/migrations/**` | PR-2 起按域补齐 |
| E2E / Fake Provider / Release Gate | `tests/e2e/**`、`tests/fixtures/e2e/**`、`docs/active/technical/release-governance.md` | PR-5 |

## Done Gate

| Gate | 条件 |
| --- | --- |
| M7 Active Split Done | active 入口、契约索引、PR 路线图、M0 契约冻结文档存在 |
| M0 Contract Freeze Done | PR-1 schema、AG-UI、fixture 校验通过，状态枚举和字段规范冻结 |
| PR-1 Done | Contract Index、StateEnum、RouterDecision、AG-UI Envelope、第一批 fixtures 完成 |
| PR-2 Done | Board、GraphPlan、Agent Runtime migration 和 fixtures 完成 |
| PR-3 Done | ToolPlan、Credit、Asset RPC/schema/fixture 完成 |
| PR-4 Done | Marketplace、SkillUsageRecord、installation、settlement、creator/user/admin API 完成 |
| PR-5 Done | E2E fixtures、fake provider、release gate、rollback gate、本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke、测试环境 HTTP 服务 E2E 自动化入口和报告模板完成；完整测试环境执行与 `status: passed` 报告待测试环境 gate |

## 推进规则

1. 后续 PR 必须引用本文件和 `docs/active/contracts/index.md`。
2. 每个 PR 只能扩大契约事实源，不得直接进入业务实现。
3. 涉及字段变更时，必须同步 schema、fixture、索引和校验脚本。
4. M1-M6 分阶段开发必须等待 M0 active 契约冻结完成。
