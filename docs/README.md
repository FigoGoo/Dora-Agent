# Agent 核心重构分阶段设计文档索引

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：AIGC 创作 Agent 核心重构、开放生态 Skill 市场、Eino Agent Runtime、业务微服务、用户端、管理端和测试验收  
相关代码路径：`services/agent/**`、`services/business/**`、`api/**`、`frontend/**`、`admin_frontend/**`、`db/migrations/**`  
相关契约：`api/thrift/business_agent_service.thrift`、`api/openapi/agent-workbench.yaml`、`api/openapi/business-api.yaml`、`api/agui/agent-workbench-events.schema.json`  
来源文档：`/Users/figo/Downloads/AIGC_Creation_Agent v1_1.md`

## 背景

用户要求以外部设计文档 `AIGC_Creation_Agent v1_1.md` 为准，对 Dora-Agent 进行系统重构，并要求分阶段产出设计文档。当前 `docs/current/README.md` 已声明旧产品、技术、契约和测试文档归档，新一轮事实源必须从该外部设计文档重新整理。

本目录用于承载重构分阶段设计文档。当前 review 已完成并通过，本文档集作为 M7 active 拆分的设计依据；字段、状态、RPC/API/AG-UI、SQL 和 fixture 必须继续拆入对应 active 事实源。

## 当前审核结论

当前裁决：

```text
review 已完成并通过
P0 文档补丁已完成
已进入 M7 active 契约拆分
M0 / PR-1 active 最小契约已冻结
下一步按 PR-2 Agent Runtime Contracts -> PR-3 Tool/Credit/Asset Contracts -> PR-4 Marketplace Contracts -> PR-5 E2E Fixtures + Fake Provider + Release Gates 继续拆分
M1-M6 业务代码开发必须等待对应 active 契约、SQL、fixture 和发布 gate 冻结后启动
```

现阶段结论：

| 维度 | 结论 |
| --- | --- |
| 架构设计 | 通过 |
| 业务闭环 | 通过 |
| 开发可执行性 | 已进入 M7 active 契约拆分 |
| 直接编码成熟度 | 不允许直接按 review 文档编码 |
| 当前推进状态 | M0 / PR-1 active 最小契约已冻结；不进入 M1-M6 业务代码开发 |

已完成 P0 补丁清单：

1. Skill 使用费 usage record 的创建时机和状态机。
2. Skill 使用费确认与 Tool 生成费确认的时序关系。
3. 创作者端 Skill 发布后台页面和验收。
4. 用户端 Skill 市场前台页面和安装/使用路径。
5. Skill installation 的版本策略和升级规则。
6. Generic Creation Graph 作为平台内置 L0 fallback 的正式 spec。

## 分阶段文档

| 阶段 | 文档 | 闭环目标 |
| --- | --- | --- |
| 总览 | [`00-总体架构与阶段拆分.md`](00-总体架构与阶段拆分.md) | 定义微服务架构、阶段依赖、责任域和总体验收口径。 |
| M0 | [`01-M0-协议冻结与微服务基线.md`](01-M0-协议冻结与微服务基线.md) | 冻结 Router、Skill、Board、Graph、ToolPlan、AG-UI、RPC、数据模型最小协议。 |
| M1 | [`02-M1-CreativeGuide与ChatModelRouter.md`](02-M1-CreativeGuide与ChatModelRouter.md) | 用户入口动态引导、能力问答、ChatModel Router 和 Router Guard 闭环。 |
| M2 | [`03-M2-CreativeBoard与AGUI事件闭环.md`](03-M2-CreativeBoard与AGUI事件闭环.md) | Creative Board、元素、Patch、Snapshot、AG-UI replay 闭环。 |
| M3 | [`04-M3-SkillRuntimeSpec与EinoGraphPlan.md`](04-M3-SkillRuntimeSpec与EinoGraphPlan.md) | Skill Runtime Spec、Graph Template、Graph Plan、Eino Graph User Gate 闭环。 |
| M4 | [`05-M4-Preflight与ToolRuntime资产扣费.md`](05-M4-Preflight与ToolRuntime资产扣费.md) | ToolPlan、安全、积分预估、确认、冻结、生成、资产提交、扣费/释放闭环。 |
| M5 | [`06-M5-开放Skill市场与两段积分结算.md`](06-M5-开放Skill市场与两段积分结算.md) | 市场 Skill 发布、审核、上架、安装、使用费、结算、治理闭环。 |
| M6 | [`07-M6-前后台体验与端到端验收.md`](07-M6-前后台体验与端到端验收.md) | 用户端工作台、管理端运营审核、城市文旅 Skill E2E、性能并发和测试验收闭环。 |
| M7 | [`08-M7-契约拆分迁移发布与运营治理.md`](08-M7-契约拆分迁移发布与运营治理.md) | active 事实源拆分、迁移、灰度发布、观测、回滚和运营接管闭环。 |
| 专题 | [`09-Skill分层、Tool清单与场景Skill示例.md`](09-Skill分层、Tool清单与场景Skill示例.md) | 冻结 Skill Level、必要 Tool、Model Registry 和 10 个场景 Skill 样例。 |
| 专题 | [`10-开放市场风控与数据隔离设计.md`](10-开放市场风控与数据隔离设计.md) | 冻结创作者数据隔离、静态审核、风险熔断、退款结算和治理闭环。 |

## 阶段推进规则

1. M0 是所有后续阶段的前置条件；协议未冻结前不进入正式代码重构。
2. M1-M3 可以在 M0 审核后串行推进 Agent Runtime 主链路。
3. M4 依赖 M2 Board 和 M3 GraphPlan，必须先明确 ToolPlan digest、确认和幂等规则。
4. M5 可以和 M3/M4 部分并行，但 Skill 使用费 RPC、表设计和 AG-UI 事件必须与 M0/M4 对齐。
5. M5 必须使用双状态机：`SkillVersionStatus` 管内容版本，`MarketplaceListingStatus` 管市场上架；listing 暂停、恢复、移除不得修改已发布 SkillVersion。
6. M6 依赖 M1-M5 的 API、AG-UI 和业务状态；前端不得发明字段或绕过契约。
7. M7 依赖 M0-M6 审核通过，用于把 review 设计拆成 active 事实源、迁移清单、发布批次和运营治理。
8. `09` 和 `10` 是 M0-M7 的横向专题约束，审核通过后应拆入 active 的契约、数据、产品、前后端和测试事实源。
9. 每个阶段都必须形成独立业务闭环，不能只做孤立表或页面。
10. 用户已确认 review 完成；当前进入 M7 active 契约拆分，M0 / PR-1 active 最小契约已冻结，M1-M6 业务代码开发必须等待对应 PR 契约冻结。

## 状态流转口径

本轮用户 review 已完成并通过，状态流转如下：

1. `00` 到 `10` 已从 `review` 流转为 `active` 设计依据。
2. `docs/active/**` 承接 M7 active 拆分、M0 契约冻结和 PR 批次治理。
3. `api/schemas/**`、`api/agui/**`、`api/openapi/**`、`api/thrift/**`、`db/migrations/**` 和 `tests/fixtures/**` 承接字段级事实源。
4. 本目录不直接作为研发编码依据。

## 当前限制

- 本目录只承载已通过的设计依据，不直接修改代码或数据库。
- 本目录仍不作为字段级研发编码依据；字段级事实源必须落在 active 契约、schema、API、RPC、SQL 和 fixture。
- 本次重构之前的历史设计副本已从仓库文档事实源清理，不作为本目录设计依据。
- 具体 Eino API 调用以当前项目依赖版本和官方文档验证为准，本阶段文档只定义能力选型和边界。
