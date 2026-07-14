# `plan_creation_spec` W2-R04 开工差距评审

> 评审状态：Audit Complete / 实现门禁未通过
>
> 评审日期：2026-07-14
>
> 评审范围：`GTL-PLAN-001`、W2-R01～R04、W2-R08、`SMK-009`
>
> 不授权事项：本文不修改 `plan_creation_spec` 的 Draft 状态，不批准 Graph、Runner、Migration、RPC、前端 Action 或目录 availability。

## 1. 结论

现有 [`plan_creation_spec-design.md`](./plan_creation_spec-design.md) 已具备独立中文设计、Mermaid DAG、稳定 Node/Branch 清单、Typed State 草案，以及 ToolReceipt、Approval、CreationSpecRevision 三套分离状态机。但设计仍存在十类 P0 开工缺口；共享 Runner/Receipt/Approval/A2UI 契约也未 Approved，因此必须继续保持 `DESIGN_REVIEW_PENDING`。

推荐推进顺序不是直接写 Graph，而是：独立依赖锁定 → 共享 W2-R01/R02/R03/R08 冻结 → W2-R04 产品/计费/Business 契约冻结 → 纯 DTO/Validator/Graph Compile → Runtime 与真实全栈纵切。

## 2. P0 关闭矩阵

| ID | 当前缺口 | 必须冻结的关闭产物 | Owner |
|---|---|---|---|
| `PLAN-P0-01` | W2-R01/R02/R03/R08 和跨 Module 契约均为 Draft | Approved 的 Result/Receipt、Runner/Lane、Approval/Continuation、A2UI 契约版本与签核记录 | Agent、Business、前端、安全、运维、财务 |
| `PLAN-P0-02` | Intent 已有 `deliverable_type`，但内容类型与交付形式未拆分，且缺语言、时长、画幅、时间、预算偏好和素材分析引用 | 完整 `PlanCreationSpecIntentV1` 字段、枚举、required、上限和 create/revise 交叉规则 | 产品、Business、Agent |
| `PLAN-P0-03` | Candidate 缺标题、阶段输入、视听/语言/时长/画幅、后续 Tool、数量、风险和重规划影响 | 完整 Proposal/Candidate strict DTO；积分估算由确定性 Quote 投影，不由模型生成 | 产品、Business、Agent |
| `PLAN-P0-04` | 嵌套 DTO、精确数量/长度/精度上限和 canonicalization 未冻结 | Schema `additionalProperties=false`、完整类型表、固定边界向量和版本兼容矩阵 | 产品、Business、Agent、测试 |
| `PLAN-P0-05` | 当前只有 primary 前一次 Charge，correction 前无独立扣费，与“每次模型执行前扣费”冲突 | primary/correction 独立 ordinal、charge key、Prepare/Finalize/Query、汇总和 no-refund 规则；同步重画 Graph/Node/Recovery | 财务、Business、Agent |
| `PLAN-P0-06` | Skill Invocation/Publisher 收益归因未进入可信上下文和 execution digest | `InvocationAttributionV1`、冻结 Snapshot/Publisher/规则版本、Charge/Receipt 字段和直调零收益用例 | Business、Agent、财务 |
| `PLAN-P0-07` | `BIZ-AIGC-001/003/004/005/007/008` 仍是候选，Candidate expired/cancelled 语义未定 | 版本化 RPC DTO、幂等键、查询/确定性重放、Expected Version、Receipt、Outbox 和合法状态迁移 | Business、Agent、财务 |
| `PLAN-P0-08` | Graph State 缺完整字段类型/敏感级别/Checkpoint 策略；defer/abort 无具名输出 | 完整 `PlanCreationSpecStateV1` 与 `PlanCreationSpecGraphOutputV1`，区分 terminal/recovery_deferred/conflict_aborted | Agent、安全 |
| `PLAN-P0-09` | `AllPredecessor` 下多上游互斥汇合没有 typed fan-in 设计 | 每个汇合点的输入 Key/类型、缺席分支、Merge/Join 策略；必要时拆分复用 Node | Agent、测试 |
| `PLAN-P0-10` | SMK-009 的 Candidate Card、两类 Approval scope、Action 和完整 Resource Read 不可执行 | Approved A2UI Card/Action/Error、CreationSpec Read API、现有 Catalog v1 的兼容升级方案或经评审的新版本、Definition Pin 和 Golden 向量 | Agent、Business、前端、测试、安全 |

任何一项未关闭时都不得把 W2-R04 改为 Approved。

## 3. 待签核逐模型计费方案

为满足“每次实际模型执行开始前扣费”，推荐冻结：

1. 一次执行 Approval 可以覆盖 primary 与最多一次 correction 的版本化最大预算/纠错策略 envelope；
2. primary 和 correction 各自使用稳定 ordinal 与独立 charge key，在各自模型调用前分别 Prepare，并把实际 model request digest 绑定到对应 ordinal Charge；
3. correction 未发生则不扣 correction；两次执行均正常开始时，Tool 费用汇总等于两条去重明细之和；
4. 模型或 Validator 正常失败不退款，只按原 key Finalize；Provider/数据库 unknown outcome 只查询原权威回执，不换 key 重试；权威证据证明模型执行从未开始时走追加式冲正，不伪造普通退款；
5. 该推荐必须由财务、Business 和 Agent 共同签核后才能修改目标 Graph。

该方案比“一次预扣覆盖两次模型调用”更直接满足 `GTL-BILL-001`，并能把 ModelReceipt ordinal、ChargeReceipt 和 Skill 收益明细逐一对应。

## 4. Runtime 最短实现批次

| 批次 | 内容 | 前置 |
|---|---|---|
| B1 | Eino `v0.9.10`、DeepSeek Adapter 精确锁定与兼容测试 | 本依赖评审 |
| B2 | GraphToolResult、Tool Pin、可信上下文、Strict Schema 纯契约 | W2-R01 冻结 |
| B3 | Session Lane、HOL、Lease/Fence、Input/Turn/Run、Recovery | W2-R02 冻结 |
| B4 | ToolCallInput、Tool/Model Receipt、Execution Ref、Prompt Artifact | W2-R01/R02 冻结 |
| B5 | Model/Prompt Registry、单主 ChatModelAgent、Runner | B3/B4，且 Runtime 评审 Approved |
| B6 | Business CreationSpec 与计费 RPC/Migration/Outbox | W2-R01/R02/R03/R04、计费与 CreationSpec 状态机、跨 Module RPC/Outbox 和 Business Migration 设计全部 Approved；届时可与 Agent Runtime 批次并行 |
| B7 | Approval/Continuation/A2UI/BFF/前端 Renderer | W2-R03/R08 Approved |
| B8 | `plan_creation_spec` DTO、Validator、Graph 与启动 Compile | W2-R04 Approved，B2～B7 可用 |
| B9 | API、PostgreSQL、单一真实 Chromium `SMK-009` Evidence | B8 真实注册完成；先冻结正式 canary audience/Definition 发布与目录能力协商，不得使用隐藏测试端点或生产测试后门 |

## 5. `SMK-009` 最小真实闭环

首个全功能纵切固定为：

```text
Workspace → Tool Definition Pin → strict Intent → durable Input/Turn/Run/ToolReceipt
→ execution Approval → approved Continuation → primary Charge/Model/Validator
→ reviewing CreationSpec Candidate → activation Approval
→ approved Continuation → active CreationSpec → SSE/Snapshot/硬刷新恢复
```

验收必须同时证明：

- 执行批准前零扣费、零模型、零 Candidate、零媒体 Operation/Batch/Job；从 Candidate `reviewing` 到激活 Approval pending/rejected/expired/cancelled 期间，媒体 Operation/Batch/Job/Generation Ledger 始终零增量；即使激活成功，`plan_creation_spec` 也不派发媒体；
- primary 与可选 correction 各自最多一条 Charge/ModelReceipt，重放不重复；
- 单一真实 Chromium 可见 Candidate Card 必须展示阶段、依赖、模型规划范围、确定性 `estimated_points_range`、待确认项与验收条件，并明确区分执行 Approval 与激活 Approval scope；
- 激活前 Active CreationSpec 不变，激活后 Business Read API 返回同一版本/digest；
- 原 ToolReceipt 保持 frozen `waiting_user`，Continuation 使用新 Turn/Run 和子 Receipt，不回写 parent；
- 硬刷新、SSE 重连、Redis 通知丢失和 Agent 重启只重放权威状态；
- CSRF、跨 Owner、stale Card revision、异义 idempotency key 全部失败关闭且零副作用；
- 前端不访问遗留 `/api/aigc/**`，不使用 Mock、fallback 或 LocalStorage 推断成功。

### 5.1 A2UI Action 与恢复门禁

- `action_definition_id`、`action_id`、`decision_id` 三者分离；Definition exact-set/digest 与 target binding 必须冻结；
- 同 `action_id` 同义重放返回同一 ActionReceipt，异义请求冲突；HTTP 响应丢失按原 `action_id` 查询/重放并返回原 ActionReceipt；
- 跨 Card Definition 替换、stale revision、Reset 前旧 Card Action 均失败关闭且零副作用；
- W0.5 Workspace Snapshot 与独立 A2UI Snapshot 的高水位关系必须冻结；实时 Event、REST 回放与 Snapshot 共用同一 reducer；
- Card revision 必须连续，缺口触发 Reset；Reset 立即撤销旧 Action 写能力，完整回源后才能恢复交互；
- 未知 A2UI 版本、Event、Component、Action 安全降级为不可交互，不能保留旧 Card 的授权能力；
- Agent 重启或 Redis 通知丢失只从 PostgreSQL 权威记录恢复，不重跑 Graph、模型或 Action；
- Candidate Card 只携带稳定 Resource Ref；激活后通过版本化 Business Read API 和冻结的 `creation_spec` reload hint 读取同一版本/digest。

### 5.2 发布与 Evidence 门禁

- Catalog v1 继续严格返回 unavailable；先部署消费者并完成版本/能力协商，再通过正式 canary audience 运行真实 Definition，Evidence 通过后才按已评审协议切 availability；
- 生产 Bundle 不仅禁止 `/api/aigc/**`，还必须隔离遗留 `a2ui_version/append_card/update_card` 协议资产；
- `w2.plan-creation-spec.smoke.evidence.v1` 冻结 exact-set Schema，至少关联 RequestID、ActionID、ActionReceiptID、ToolReceiptID、ApprovalID、CreationSpecID 和 EventID；
- Evidence 只保存 ID/version/digest、稳定错误码、计数和布尔断言，禁止保存 Prompt、Candidate/Card/Form 原文、Cookie、CSRF、内部断言或模型响应。

## 6. 下一评审动作

1. 主线先关闭 W2-R01/R02/R03/R08 的共享未决策；
2. 产品冻结 Intent/Candidate DTO；
3. 财务与 Business 冻结逐模型计费、收益归因和 AIGC RPC；
4. Agent 据此重画 Graph 计费/恢复路径并补 typed fan-in、State/Output、错误矩阵和固定向量；
5. 六角色复核全部通过后，才允许把 `plan_creation_spec-design.md` 状态改为 Approved。
