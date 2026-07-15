# W2 P4 Owner 决策收口包 v1

> 状态：Review Packet / Awaiting Owner Decisions
>
> 审计日期：2026-07-16
>
> 事实基线：`codex/w2-r04-consumption-audit@59b8b2b5`
>
> 结论边界：本文只把分散的 ADR、Review Freeze、Billing、Approval/Consumption 与 Structured Smoke 未决项整理成可逐项签核的决策包。本文不是 Owner 批准、不是机器 Review Freeze、不是 trust root，也不授权生产 Go、SQL Migration、IDL、生成代码、Graph、Runner、Billing、Approval、A2UI 或 Harness 实现。

## 1. P4 目标与非目标

P4 当前只做四件事：

1. 把 `W2-ADR-001/002/003/004/005/008/010/011` 与 `W2-R00/R01/R02/R03/R04` 的待决问题缩减为明确选项；
2. 给出首切计费授权模式、Activation Approval/Consumption 字段映射和生产 projection Owner 的推荐候选；
3. 单独列出 `W2-ADR-009` 的七方批准、版本化 trust root 与原子 unlock 前置；
4. 固定批准后的最短实施顺序，使 Owner 可以看清每项批准真正解锁什么、仍不解锁什么。

本文明确不做：

- 不手工把 machine manifest 的 `status`、`freeze`、`implementation_unlocked` 或 blocker 改成已关闭；
- 不把 test-only corpus、本地测试、raw Git membership 或文档签字栏解释为正式批准；
- 不创建 `smoke/**`、`test-adapters/**`、`deploy/local-smoke/**`；
- 不创建 W2 生产 Runner/Graph/Approval/Billing/A2UI 的 Go、SQL、IDL 或生成代码；
- 不修改或评价工作树中的用户自有 workflow 改动。

## 2. 绑定的当前事实

### 2.1 Review Freeze R00～R04

本次审计绑定 [`w2-review-freeze-manifest.json`](../agent/approvals/w2-review-freeze-manifest.json) 原始字节 SHA-256：

```text
a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e
```

| Gate | 当前状态 | manifest 当前 required roles | Candidate | 当前 blocker code exact-set |
| --- | --- | --- | --- | --- |
| `W2-R00` | `expansion_frozen`，`freeze=null` | Agent、Business、Finance、Product、Security | 无 | `W2_R00_BILLING_REVIEW_PENDING`、`W2_R00_OWNER_APPROVAL_MISSING` |
| `W2-R01` | `expansion_frozen`，`freeze=null` | Agent、Business、Finance、Operations、Security | 1 个 partial candidate；87 向量不能代表完整 Gate | `W2_R01_ADR_REVIEW_PENDING`、`W2_R01_CARD_AND_REGISTRY_SCOPE_PENDING`、`W2_R01_CORPUS_SCOPE_INCOMPLETE`、`W2_R01_OWNER_APPROVAL_MISSING`、`W2_R01_OWNER_SCOPE_PENDING`、`W2_R01_SLOT_REGISTRY_ORDINAL_PENDING`、`W2_R01_VALIDATOR_BUILD_CLOSURE_PENDING`、`W2_R01_VERSION_POLICY_PENDING` |
| `W2-R02` | `expansion_frozen`，`freeze=null` | Agent、Data、Operations、Security | 5 个 child manifest，机械小计 298 向量/58 个唯一 target tests；无 aggregate candidate | `W2_R02_AGGREGATE_MANIFEST_MISSING`、`W2_R02_OWNER_APPROVAL_MISSING` |
| `W2-R03` | `expansion_frozen`，`freeze=null` | Agent、Business、Finance、Frontend、Product、Security、Test | 无 | `W2_R03_CHILD_CORPUS_PENDING`、`W2_R03_OWNER_APPROVAL_MISSING` |
| `W2-R04` | `expansion_frozen`，`freeze=null` | Agent、Business、Finance、Operations、Product、Security、Test | 1 个 activation Consumption partial candidate；111 向量不能代表完整 Gate | `W2_R04_FULL_GATE_BASELINE_MISSING`、`W2_R04_OWNER_APPROVAL_MISSING`、`W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING` |

上表中的 `required_owner_roles` 是当前 manifest 输入，不代表角色集合已获批准。尤其：

- `R01-D06` 要求先关闭 R01 范围决定，再从责任反推最终 Owner exact-set；当前集合可能缺少 Frontend、Product 或 Test；
- R00 的 `BILL-OPEN-005` 没有登记 Owner，且 Provider、时钟、Registry 与测试关闭需要的 Operations/Test/Integration 责任尚未形成最终映射；
- R03 当前没有 Operations，R04 当前没有 Frontend；是否补充必须由语义范围与 projection 责任决定，不能由本文直接改写 manifest；
- `integration_owner` 可以组织跨 Gate 收口，但不能代替任何语义 Owner。

R00 的 12 个位置式 Billing open item 已由 [`w2-r00-owner-decision-matrix-v1.md`](../agent/w2-r00-owner-decision-matrix-v1.md) 去重为 `R00-D01`～`R00-D14`，原始字节 SHA-256 为 `0a0a0968a136c4c21d054a4c75e0f7996850c9885a758d8af3bc2364246abb30`。该矩阵将 `BILL-OPEN-005` 拆为责任分配与 Price Config↔Model Config exact mapping，并明确 `R00-D05/D07/D08/D09/D11/D13` 仍是 `candidate_incomplete_not_ballot_ready`、`R00-D01` 仍是 `scope_derivation_pending`。因此当前不得生成 `DR-W2-R00-v1`，不得为未登记 Owner 预填角色，也不得把设计中的候选向量表登记为 R00 candidate。

[`CPR-W2-R00-v1.json`](../agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json) 将全部 14 项 readiness 基线与六项 incomplete candidate 仍缺少的输入/证据要求固定为只读准备清单，原始字节 SHA-256 为 `6d9cd4a033d19c127fcfec04e975abdcb047247dcaa56c9fd381068b6977c836`。它把真实前置与跨 Gate 对齐分开，不提交 Policy 数值、Provider 能力、ModelReceipt 字段、时钟阈值、slot ordinal、Owner role 或 ballot option；两个独立 stdlib-only validator/guard 共同绑定完整 items 语义摘要、live R00 零 candidate/null freeze/reopen 与源码 exact-set。该 CPR 不是 `DR-W2-R00-v1`、candidate manifest、Owner request/approval、build/trust closure 或实现解锁。

[`DR-W2-R01-v1.json`](../agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json) 将 `R01-D01`～`R01-D04` 与 `GOV-D01`～`GOV-D06` 的推荐候选、`R01-D05` 的 `candidate_incomplete_not_ballot_ready` 和 `R01-D06` 的 `scope_derivation_pending` 固定为严格待决请求，原始字节 SHA-256 为 `676c4f83a1e7570c5ac41e3d0ffc8556fb936b0b363b93a6c7b79b2da7552018`。D05/D06 只允许补充版本化候选或在范围关闭后反推角色，不提供接受推荐选项。两个独立 stdlib-only validator/guard package 交叉固定源码 exact-set，从两份 corpus 派生 87 个 vector ID、固定 4 个目标测试名，并与 live Gate、SHA-bound manifest 交叉核对；请求仍只绑定 `partial_candidate`，不产生 compile attestation、完整 Gate baseline、Owner approval、build/trust closure 或实现解锁。

R02 的分散待决项已由 [`w2-r02-owner-decision-matrix-v1.md`](../agent/w2-r02-owner-decision-matrix-v1.md) 去重为 `R02-D01`～`R02-D19`，并映射既有 `PG-D01`～`PG-D08`、`TC-P01`～`TC-P10`、Owner 候选和最低关闭证据。该矩阵只关闭稳定引用缺口；全部决定仍为 `awaiting_owner_decision`，不构成 aggregate candidate、Owner approval 或生产解锁。

[`DR-W2-R02-v1.json`](../agent/approvals/w2-r02-owner-decision-requests/DR-W2-R02-v1.json) 将 19 项推荐候选固定为严格待决请求，原始字节 SHA-256 为 `4b6356f9d6b4da7adf348c2207135e2cebd8c972349f84c67ade274f6d274fe9`。它只允许 `accept_recommendation/reject_keep_blocked` 两种请求选项，Owner role 集显式标记为 provisional，并固定 `implementation_unlocked=false`；Schema 不包含 selected option、approval、Review、actor 或 commit 自报字段。该请求不是 approval summary、Owner authority request、aggregate manifest 或 Formal Freeze 输入。

R03 的分散 Approval、Decision、Continuation、Consumption、child Receipt 与双向 Query 待决项已由 [`w2-r03-owner-decision-matrix-v1.md`](../agent/w2-r03-owner-decision-matrix-v1.md) 去重为 `R03-D01`～`R03-D14`。该矩阵显式区分 Decision key、Continuation SourceID、child ToolReceipt key、Agent-local Consumption key、Business `tr:` command key 与领域 backstop，也区分 Business→Agent Consumption Authority Query 和 Agent→Business CreationSpec Decision Query；全部决定仍为 `awaiting_owner_decision`。

[`DR-W2-R03-v1.json`](../agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json) 将 14 项推荐候选固定为严格待决请求，原始字节 SHA-256 为 `d0e229c8b2fbaaee21b67a87155d6f9607f08e581d36419ae3833ae65b2d7c6d`。互相交叉守卫的两个独立 stdlib-only validator package 机械绑定 validator source exact-set、矩阵 §3、当前 Gate、provisional role 和 blocker exact-set，并要求 R03 保持 `candidate_evidence=[]`；请求仍不记录选择、批准、平台 Review 或实现解锁，也不补齐 child Corpus、生产 Authority Query、数据库 Evidence、aggregate 或 build/trust closure。

R04 的 headless scope、Intent、Candidate、Billing、Graph、Activation、Business RPC、child Receipt 与发布待决项已由 [`w2-r04-owner-decision-matrix-v1.md`](../agent/w2-r04-owner-decision-matrix-v1.md) 去重为 `R04-D01`～`R04-D20`。该矩阵把 111 条 Activation Consumption 向量继续限定为 partial candidate，并将 headless R04 与 R08/A2UI/C1/R09 分开；全部决定仍为 `awaiting_owner_decision`。

[`DR-W2-R04-v1.json`](../agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json) 将 20 项推荐候选固定为严格待决请求，原始字节 SHA-256 为 `d8806af1289aff1b8a790bdbf861c97a7c348f70aa83213760bdc28b318cd0e7`。同一 validator/guard 对从 SHA-bound candidate manifest 派生并交叉核验 4 个 fixture、111 条唯一向量、11 个唯一目标测试和 `candidate_unactivated` 状态；这只证明请求绑定当前 partial candidate，不是 compile attestation、完整 Gate baseline、Owner approval、aggregate 或 Formal Freeze。

### 2.2 ADR-009 / Structured Smoke

本次审计绑定 [`W2-S0-G0 approval manifest`](../testing/approvals/w2-s0-g0/approval-manifest.json) 原始字节 SHA-256：

```text
d9946b916618bfbb007173578059855f399c41003160c5d1a4f80bad2ca1afe9
```

当前机器事实为：

- `status=awaiting_owner_approval`；
- `trust_root_status=candidate_unactivated`；
- `implementation_unlocked=false`；
- 七方 exact-set 为 `agent_owner`、`business_owner`、`frontend_owner`、`operations_owner`、`security_owner`、`test_owner`、`worker_owner`；
- 禁止目录为 `deploy/local-smoke/**`、`smoke/**`、`test-adapters/**`；
- 十项 blocker 全部开放：`BASE_OWNED_WORKFLOW_NOT_ACTIVE`、`FORK_CANARY_NOT_PASSED`、`OWNER_AUTHORITY_NOT_ACTIVE`、`RULESET_SOURCE_AND_NO_BYPASS_NOT_PROVEN`、`SAME_REPO_CANARY_NOT_PASSED`、`SEMANTIC_PATH_POLICY_NOT_ACTIVE`、`TRUST_ROOT_REKEY_HANDOFF_NOT_FROZEN`、`TRUST_ROOT_RELEASE_NOT_INSTALLED`、`VALIDATOR_BUILD_CLOSURE_NOT_FROZEN`、`WORKFLOW_DIGEST_AND_ACTION_SHA_NOT_FROZEN`。

当前 manifest 只绑定 ADR、shadow baseline 和 Context Registry；没有绑定 [`full-function-smoke-engineering-design.md`](../testing/full-function-smoke-engineering-design.md) 或 [`w2-smoke-governance-trust-root-release-v1.md`](../testing/w2-smoke-governance-trust-root-release-v1.md)。两者仍是评审输入，不能反向改写已经绑定的 v1 字节。

### 2.3 Structured Smoke baseline 已陈旧

[`smk-004a-shadow-baseline-v1.json`](../testing/approvals/w2-s0-g0/smk-004a-shadow-baseline-v1.json) 绑定的 API 源为：

```text
scripts/smoke-w0-transport.sh
line_count=4457
sha256=17d4797c240bc4e1e9036d941773304c5e399340d7cfe57d689a4c7e34581cf1
```

本次只读核验的当前源为：

```text
scripts/smoke-w0-transport.sh
line_count=4816
sha256=c673d57d36fabd6fb0eae05b9890a6c8fd7a95f0c877e9b2aa1e06531ba397ed
```

UI 源仍与 baseline 一致。API 源漂移意味着 v1 不能再声明当前 parity/closure；Test Owner 必须决定版本化 baseline refresh 的 exact-set，并由七方重新审阅。禁止静默修改已绑定 v1。

## 3. 八项 ADR 的决策 ballot

本节“推荐接受”只是建议的 ballot 默认项，只有正式 Owner authority 在同一候选提交上验证通过后才成为 Accepted。

### 3.1 ADR-001：生产写模型和 `not_committed`

**推荐接受：**

- 生产写模型采用不可变 `logical_tool_execution` 根、追加式 `execution_segment`、通用 `execution_ref_slot` 与追加式 `execution_ref_observation`；
- canonical Receipt 是 segment 封口时生成的不可变兼容 projection，不是唯一物理写模型；
- observation 只承担内部 transport audit，不进入 canonical Receipt、digest 或 result refs；
- slot 只允许 `prepared/resolved`；`prepared` 绝对阻塞 terminal/seal；
- 普通 Query `not_committed` 只追加 observation、slot 保持 `prepared`，允许原 command identity 重发；
- 只有不可变权威终态或显式 terminal no-effect authority 才能 resolve；后者 resolve 后禁止重发。

**批准前必须补齐：**

- canonical slot identity、observation identity、Receipt/segment scope 与 production table/state mapping；
- Business Decision/Billing `authority_outcome` 的权威 envelope 和 exact projection；
- R01 中“Business Decision negative authority 可 resolve”与上述普通 Query 语义的统一规则。

### 3.2 ADR-002：摘要域分离

**推荐接受：**

- `intent_digest` 只覆盖稳定意图；
- `receipt_digest` 只覆盖不可变因果回执；
- `effect_request_digest` 只覆盖副作用请求；
- `projection_payload_digest` 只校验最终 exact JSON 完整性，不参与幂等、授权或 authority 等价判断。

**批准前必须补齐：**

- `request_semantic_digest`、`tool_receipt_digest`、`parent_tool_receipt_digest`、`result_digest` 到新摘要域的逐字段 old→new mapping；
- schema/version 升级规则、正反向 golden vectors 和跨 Module 兼容矩阵；
- exact-version 与“同主版本忽略未知可选字段”只能保留一个正式口径。当前推荐 R01 v1 exact-version、未知字段失败关闭。

### 3.3 ADR-003：Activation command identity

**推荐接受：** Activation 的公开 Command/Query 只暴露 `tr:<child_receipt_id>:<ref_slot>:v1`；`approval_id + decision_version` 不再构成第二套公开幂等身份。Approval version 必须拆成 presented/resulting；Business 领域唯一约束 `(candidate_id, decision_id, decision_action)` 只作同一业务事实的 backstop，不能成为第二个公开 command key。

**批准前必须补齐：** R01 `approval_consumption` slot 的 Receipt/segment scope 与 ordinal、R03/R04 child Receipt identity、Business Command Receipt/IDL、同键重放/异键同资源冲突和 Unknown Outcome Query。当前只记录推荐候选，不得据此创建 Activation Handler 或 Migration。

### 3.4 ADR-004：Consumption authority Query

**推荐接受：** v1 由 Business 通过服务认证的 Agent Query 校验 Approval/Decision/Consumption 权威事实；同一信任域内不提前引入签名信封与 Key Rotation。离线消费或跨信任域出现后再以新版本评审签名方案，不能把 test-only unsigned Core 当作生产授权。

**批准前必须补齐：** 双向 RPC 依赖、调用身份与 scope、Query exact schema、NOT_FOUND/late commit、超时/熔断/重放、权限审计，以及 child 文档中签名/Key Rotation 目标语义的统一 disposition。

### 3.5 ADR-005：首切计费授权唯一模式

**推荐接受 `authorization_mode=preauthorized`：**

- Business 拥有不可变、版本化、低额的 `billing_policy_ref/version/cap`；
- Agent Tool Definition 只 Pin 精确 policy ref/version/digest；
- Business 在 `PrepareBillableExecution` 内以本地权威事实重新校验后原子扣费；
- Charge commit 是正式执行开始边界；Finalize 只绑定 terminal ModelReceipt，不再次扣费；
- 已真实发生的模型失败、超时、取消、结果未采用或 Candidate 校验失败不自动退款；账务差错仅在 W4 通过 append-only reversal 处理；
- W2 v1 primary `model_call_ordinal=0`，correction 禁用；
- `preauthorized` 严禁创建 billable Approval/Decision/Consumption Core，Candidate Activation Approval 保持独立。

**若任一必要 Owner 拒绝：** 不允许运行时 A/B 或 Feature Flag 切换；应修订为 `full_approval`，先独立冻结 billable Approval/Decision/Consumption/Query、quote/cap digest、过期/single-use 和认证 Query，再重新审核 R00/R03 与 Graph topology。

**仍需按 `R00-D01`～`R00-D14` 关闭 `BILL-OPEN-001`～`BILL-OPEN-012`：** primary ordinal、授权模式、currency、policy cap/scope/暂停、Price Config↔Model Config、Provider 幂等/Query、ModelReceipt、Skill attribution、时钟、W4 reversal 边界、Prepare/Finalize ref slots 与 `authority_outcome` 映射。`R00-D06` 必须先冻结 `BILL-OPEN-005` 的责任分配；`R00-D05/D07/D08/D09/D11/D13` 补齐 exact candidate 前不得接受推荐。

### 3.6 ADR-008：契约 evaluator 进入生产入口

**推荐接受：** 相应 Gate Approved 后，把 canonicalizer、validator 和状态迁移放入生产包；契约测试直接调用生产入口；跨 Module 只共享 IDL/golden vectors，不共享 `internal` Go 包，也不保留平行 evaluator。

**范围决定：** ADR-008 不应阻塞只实现 lane/lease/fence 且不提交 resolved/Receipt 的 W2-A1；它是 W2-A2 生产 execution/ref/projection 的进入门禁。Owner 必须显式接受该 A1 carve-out，或把 ADR-008 加入 A1 Gate，不能保持含糊。

### 3.7 ADR-010：Feature Builder 与 bootstrap 边界

**推荐接受：** 每个纵切提供显式 Feature Builder，返回 Handler/Runner、生命周期与 readiness；顶层 bootstrap 只校验配置、排序启动、失败回滚和反向关闭。

**范围决定：** 与 ADR-008 相同，当前长期计划把它放在 W2-A2。Owner 必须明确 A1 的最小 lane feature 装配是否也受其约束；无论选择哪一项，都必须有独立构建与关闭顺序测试。

### 3.8 ADR-011：Activation 与 Billing 字段命名

**推荐接受以下 exact mapping：**

| 语义 | 字段 | 值 |
| --- | --- | --- |
| Activation Approval 类型 | `approval_type` | `candidate_activation` |
| Runner route | `action_type` | `candidate_activation` |
| 用户决定 | `decision_action` | `approve` 或 `reject` |
| 仅 approved 后的 Consumption 动作 | `consumption_action` | `activate` |
| Activation effect registry | `effect_kind` | `creation_spec_activation` |
| Activation scope（如保留） | `scope` | `creation_spec_activation`，但与 `effect_kind` 职责独立 |
| Activation ref slot | slot name | `approval_consumption` |
| 计费 effect | `effect_kind` | `billable_execution` |

`preauthorized` 下不得存在 billable Core。若 ADR-005 选择 `full_approval`，必须独立冻结 billable approval/action/decision/consumption/scope/ref-slot/domain identity 与 quote/cap digest，禁止从 activation mapping 推导。

ADR-011 批准前还必须关闭 `R01-D05`：`approval_consumption` 的 Receipt/segment scope、Owner、slot ordinal 唯一域，以及 R01/R03/R04/child Receipt 的一致映射。

## 4. 生产 Authority 与 Projection Owner

本表冻结的是建议责任边界，不宣称对应生产实现已经存在。

| 事实或 projection | 推荐权威 Owner | 读取/投影边界 |
| --- | --- | --- |
| Logical execution、segment、ref slot、observation、Receipt projection | Agent | Agent PostgreSQL 是生产真源；Business/Frontend 不复制成第二真源 |
| Approval、Decision、Consumption、Continuation 与认证 Query | Agent | Agent 生成不可变对象并提供受认证 Query；A2UI 不是 authority |
| Billing policy、Price Config、余额、Charge、Ledger、Charge Receipt、Billing command receipt/outbox | Business | Business PostgreSQL 是唯一计费真源；Agent 只保存权威引用 |
| ModelReceipt、provider request key、dispatch marker、模型终态 | Agent | terminal ModelReceipt 冻结后再请求 Business Finalize |
| CreationSpec Candidate、业务 Decision receipt/outbox | Business | Agent 通过版本化 RPC/Query，不直连 Business 数据库 |
| Tool Definition、Prompt/Model Config Pin、Runtime budget | Agent | 启动 Registry 与 Turn 冻结上下文；用户/Skill/模型不得改写 |
| A2UI Event/Card/Action/refresh projection | Agent + Frontend 契约，R08 决定展示生命周期 | R01 只保留最小 Approval 因果引用；`card_id`、revision 与 UI 恢复由 R08 冻结 |
| test-only corpus、shadow baseline、candidate evidence | Test/Governance 评审资产 | 没有生产 Owner；不得被运行时读取为 authority |

跨 Module 一律通过版本化 HTTP、Thrift/Kitex RPC、Event/Job 或明确数据库契约；不得跨 Module import `internal`，不得直连对方数据库。

## 5. 必须先消除的跨文档冲突

| ID | 冲突/缺口 | P4 关闭动作 |
| --- | --- | --- |
| `P4-C01` | 普通 Query `not_committed` 是否可 resolve 的口径冲突 | 采用 ADR-001 ballot；只允许 immutable terminal no-effect authority resolve，普通 Query 保持 prepared |
| `P4-C02` | ADR-001 目标模型缺少 canonical slot/observation identity 映射 | 增加字段/状态/唯一约束/Receipt scope 映射后再审 |
| `P4-C03` | ADR-002 没有 old→new digest 字段表 | 生成版本化 mapping 与 golden vectors |
| `P4-C04` | exact-version 与 Catalog 同主版本兼容策略冲突 | R01 v1 推荐 exact-version；另一策略只能作为新版本迁移 |
| `P4-C05` | `waiting_user.card_id` 的 R01/R08 Owner 冲突 | R01 仅冻结 Approval 因果引用；Card 生命周期交 R08，形成字段责任表 |
| `P4-C06` | R01 代表性 Tool key 与正式六 Tool Registry exact-set 不一致 | Owner 选择“机制+六 key”或缩小 Gate；当前 partial 不得冒充全量 |
| `P4-C07` | R02 现有 5 个 child manifest 只能机械小计为 298 向量/58 个唯一 target tests；upgrade 未公开 fixture/vector exact-set，且没有全 Gate aggregate | 按 [R02 Owner 决策矩阵](../agent/w2-r02-owner-decision-matrix-v1.md) 关闭语义、ADR scope、Owner exact-set 与 build/trust closure 后，版本化补齐 upgrade 并生成一个全 Gate aggregate manifest |
| `P4-C08` | ADR-008/010 对 A1/A2 的适用范围含糊 | 明确接受 A1 carve-out 或追加 A1 Gate，不得靠实现猜测 |
| `P4-C09` | R02 corpus 包含 approval/batch 来源，A1 仅支持 `user_message` | 冻结“corpus 保留、A1 production unavailable”，不提前扩实现 |
| `P4-C10` | R02 Runtime/Ingress/PostgreSQL/legacy/Marker/Turn Context 的位置式 P0 引用会漂移 | 已以 `R02-D01`～`R02-D19` 建立稳定编号、Owner 候选、关闭证据和源 crosswalk；语义决定、最终 Owner exact-set 与正式 review 仍未完成 |
| `P4-C11` | R00 Owner/Projection 映射不完整 | 按 `R00-D01`～`R00-D14` 先冻结 `BILL-OPEN-005` 责任分配，再补齐 Policy/Price/Provider/ModelReceipt/时钟/slot exact candidate 并反推最终 Owner exact-set |
| `P4-C12` | Structured Smoke v1 baseline API 源摘要已漂移 | 版本化 refresh、更新 binding、七方重新审批；不改写 v1 |
| `P4-C13` | R03/R04 的 Activation 仍混有 `approval_id + decision_version` 与 `tr:` 两套公开 identity | 裁决 ADR-003，统一公开 command key、presented/resulting version、领域唯一 backstop 与 child Receipt identity |
| `P4-C14` | R03/R04 的 unsigned Consumption Core、认证 Query 与 child 签名/Key Rotation 目标口径冲突 | 裁决 ADR-004；v1 认证 Query 或新版本签名方案只能保留一个正式口径，并冻结 RPC/权限/审计边界 |

### 5.1 P4 ballot 机械依赖映射

本表只建立追踪关系，不代表对应 ADR、Gate 或 stable decision 已被接受。R00/R02/R03/R04 的 stable decision record 只关闭引用漂移，不替代 Owner 结论、aggregate manifest 或正式批准；R01 待决请求也只固定当前 partial candidate 输入。

| P4 缺口 | ADR 依赖 | Gate | Stable decision / open item | 进入 Owner review 前置 |
| --- | --- | --- | --- | --- |
| `P4-C01` | ADR-001 | R00/R01/R04 | `R00-D14`、`R01-D02`、`R04-D10/D13/D14/D19` | 冻结普通 Query 与 terminal no-effect authority 的唯一 resolve 规则 |
| `P4-C02` | ADR-001 | R01/R02 | `R01-D05`、`R02-D09` | 冻结 root/segment/slot/observation identity、Receipt scope 与原子 terminal 拓扑 |
| `P4-C03` | ADR-002 | R01/R02 | `R01-D01`、`R02-D19` | 提供 old→new 摘要字段表、domain/version 与 golden vectors |
| `P4-C04` | ADR-002 | R01 | `R01-D01` | 在 exact-version 与同主版本兼容中选择唯一 v1 口径 |
| `P4-C05` | R08 contract ballot | R01/R03/R04/R08 | `R01-D03`、`R03-D13`、`R04-D01/D20` | 冻结 Approval 因果引用、headless exclusion 与 Card 展示生命周期字段责任表 |
| `P4-C06` | Tool Registry ballot | R01 | `R01-D04` | 选择“机制+六 key”或版本化缩小 Gate |
| `P4-C07` | ADR-001/002/008/010 | R02 | `R02-D01`～`D19`、`PG-D01`～`D08`、`TC-P01`～`P10` | 全部语义 disposition、upgrade fixture 口径、build/trust closure 后才生成 aggregate |
| `P4-C08` | ADR-008/010 | R02 | `R02-D19` | 冻结 A1 carve-out/追加 Gate 与 Feature lifecycle 要求 |
| `P4-C09` | R02 A1 scope | R02 | `R02-D03`、`R02-D19` | 冻结 Approval/Batch corpus 保留但 production unavailable |
| `P4-C10` | R02 stable reference | R02 | `R02-D01`～`D19` | 稳定 ID 已建立；逐项 Owner ballot、最终 role exact-set 仍待完成 |
| `P4-C11` | ADR-005 | R00/R04 | `R00-D01`～`D14`、`R04-D08`～`D15` | 先关闭 `R00-D06` 责任分配和六项 incomplete candidate，再逐项裁决计费/Provider/时钟/Registry/Test 责任 |
| `P4-C12` | ADR-009 | W2-S0 | 无；七方独立轮次 | Test Owner 冻结 refresh exact-set，版本化 baseline 后七方重审 |
| `P4-C13` | ADR-003 | R01/R03/R04 | `R01-D05`、`R03-D04`～`R03-D06`、`R03-D08`、`R03-D11`、`R04-D16`、`R04-D18`、`R04-D19` | 冻结唯一 `tr:` identity、version 语义、Business backstop 与 child Receipt |
| `P4-C14` | ADR-004 | R03/R04 | `R01-D02`、`R02-D18`、`R03-D03`、`R03-D10`、`R03-D11`、`R04-D17`～`R04-D19` | 冻结 v1 认证 Query 或新版本签名方案，以及双向 RPC/权限/审计 exact-set |

## 6. 最短批准与实施顺序

以下 lane 可以分别接受 Owner 决策，但每条 lane 只能在自己的全部 Gate Approved 后实施：

| 顺序 | 决策/批准 lane | 批准后最多解锁 | 仍禁止 |
| --- | --- | --- | --- |
| 1 | ADR-001/002；按 R02 决策矩阵关闭 ADR-008/010 的 A1 scope、`R02-D01`～`D19`、aggregate manifest 与 Owner authority | `W2-A1` Session Lane Kernel：真实 PostgreSQL claim/lease/fence/HOL/recovery；`TurnExecutor` 仅作端口 | resolved、Receipt、Approval、Graph、Billing、A2UI |
| 2 | ADR-005 + R00，推荐 `preauthorized`，关闭 12 个 Billing open item | `W2-B0a` Business Prepare/Get/Finalize 最小计费 | Graph、Candidate、Activation Core；若选 full approval，未批 R03 billable Core 前仍不解锁 |
| 3 | ADR-009 七方路线选择、版本化 baseline、Candidate-Preparation/Bootstrap/Activation/Unlock | 仅在最终 compound unlock 后允许后续独立 Harness PR | Candidate/Bootstrap/Unlock PR 均不得夹带 Harness 或业务实现 |
| 4 | R01/R02 + ADR-001/002/008/010 | `W2-A2` execution/ref/Receipt projection | Approval、Consumption、A2UI、业务 Graph |
| 5 | ADR-003/004/011 + R03/R04 及共享 R00～R02；其余 Graph ADR | headless `plan_creation_spec`、Candidate、Activation、Continuation | A2UI 仍需 R08；其他五 Tool 仍 unavailable |

不能为了“更快”把多个 Gate 写成一次笼统批准。每项批准必须绑定 exact contract manifest、向量/测试 exact-set、当前 head 与有效 Owner exact-set。

## 7. ADR-009 路线决定与 unlock 原子性

strict v1 要求权威仓库位于 Enterprise-controlled GitHub Organization，并依赖 Enterprise Auditor、Merge Queue、七个独立 actor、双管理员、私有 Owner App、独立 Ruleset Auditor、无 bypass Ruleset、外部 Secret Manager/HSM、CAS authority ledger、双故障域 witness、不可变制品库与确定性 builder。

本次只读核验的当前 origin 是公开个人用户仓库，不满足 strict v1 的组织拓扑。因此七方必须二选一：

1. 指定或迁移到满足 strict v1 的 Enterprise Org 权威仓库；或
2. 启动新的 v2 trust-root 设计，显式重审无法满足的 Enterprise Auditor/Merge Queue 假设。

不允许把当前拓扑静默解释为满足 v1。

真正 unlock 必须保持以下分离状态和 PR：

```text
candidate_unactivated
  -> Candidate-Preparation（仍为 candidate）
  -> Bootstrap PR merged：installed_locked
  -> 外部 App/Ruleset 激活 + same-repo/fork canary：active_locked
  -> 独立 Unlock PR + authority CAS event + 双 witness：approved/unlocked
```

原子约束：

- Candidate-Preparation 不得创建 `.github/**` trust root 或 Harness；
- Bootstrap 不得修改 prepared checker/plan，不得夹带 Unlock/Harness；
- activation 开始前，除 Owner Authority 与两类 canary 外的其余七项 blocker 必须已经有效；三项只能由一次 dual-witness finalization 原子关闭；
- Unlock payload 只能包含 unlock payload、request 与 manifest-v2 request projection；
- 七个不同 actor 必须审阅最终 head；
- 合并、authority chain CAS `unlock` event 与两个 witness checkpoints 三者共同成立，才代表正式 approved/unlocked；仓库 manifest-v2 只是 request projection；
- 任一语义漂移或 blocker reopen 都必须推进 epoch、烧毁旧 unlock，并由七方重新批准。

## 8. 正式决策记录与状态迁移

每个 ADR/Gate 的结构化记录至少绑定：

- 决策 ID、选定选项、拒绝选项与适用范围；
- 受影响 Module、契约、字段、状态机、向量、测试和 Migration；
- 当前候选完整 commit SHA 与 contract manifest SHA-256；
- 最终 `required_owner_roles` exact-set 及 role→责任→受保护平台身份映射；
- 平台返回的 reviewer numeric ID、review ID/state、review commit SHA 和时间；
- 被替代记录与 reopen/epoch 引用。

状态迁移必须失败关闭：

1. 先完成语义决定和文档冲突消除；
2. 再生成版本化 contract/corpus/build manifest，不改写旧批准字节；
3. Owner exact-set 从最终范围推导，不允许“以后再补 Owner”；
4. 受保护 base trust root、Ruleset、当前 head freshness 和 no-bypass authority 生效；
5. 由机器验证全部有效平台 Review；
6. 只有对应 Gate 的 versioned manifest 正式进入 Approved/unlocked，Canonical 才能把实现阶段改为 `已批准待实现`。

文档签字栏、自报 JSON、任意 URL、commit ancestor、本地测试和手工编辑状态均不能完成上述迁移。

## 9. Owner 待办清单

### 9.1 第一轮：语义 ballot

- [ ] Agent / Business / Security / Test：接受或拒绝 ADR-001 写模型与 `not_committed` 规则；
- [ ] Agent / Business / Security / Test：接受或拒绝 ADR-002 摘要域及 exact-version 口径；
- [ ] Agent / Business / Security / Test：接受或拒绝 ADR-003 的唯一 `tr:` Activation identity；
- [ ] Agent / Business / Operations / Security / Test：接受或拒绝 ADR-004 的 v1 认证 Authority Query；
- [ ] Product / Finance / Security / Business / Agent：接受 `preauthorized`，或明确转入完整 `full_approval` 设计；
- [ ] Agent / Operations：裁决 ADR-008/010 对 A1 的 carve-out；
- [ ] Agent / Business / Product / Finance / Security / Test：接受或修订 ADR-011 Activation mapping；
- [ ] R00/R01/R02/R03/R04 语义 Owner：关闭 `P4-C01`～`P4-C14` 并推导最终 role exact-set；分别按 `R00-D01`～`D14`、`R01-D01`～`D06`、`R02-D01`～`D19`、`R03-D01`～`D14`、`R04-D01`～`D20` 逐项 ballot，不得笼统批准；R00 incomplete 项不得提前接受。

### 9.2 第二轮：可验证候选

- [ ] 为 ADR-001/002 增加生产模型 mapping、摘要迁移表与 golden vectors；
- [x] 为 R00 待决项分配 `R00-D01`～`R00-D14`，拆分 `BILL-OPEN-005` 责任与 mapping，并显式标记六项 incomplete candidate；
- [x] 为 R00 六项 incomplete candidate 建立严格 CPR 输入/证据清单及双 validator/guard；readiness、零 candidate、零 ballot 与生产阻断保持不变；
- [x] 为 R01 建立严格的 `awaiting_owner_decision` 请求及互相交叉守卫的 stdlib-only validator/guard，只绑定当前 1 fixture/87 vectors/4 tests partial candidate；
- [x] 为 R02 待决项分配 `R02-D01`～`R02-D19`，映射 Owner 候选、最低关闭证据和源位置；
- [x] 为 R02 建立严格的 `awaiting_owner_decision` 请求及 validator，禁止选择/批准/Review 身份字段和实现解锁；
- [x] 为 R03 待决项分配 `R03-D01`～`R03-D14`，区分六类 identity、双向 Query、child/failed-after 与 Owner exact-set；
- [x] 为 R04 待决项分配 `R04-D01`～`R04-D20`，映射 PLAN/Billing/P4、headless/R08 边界与完整 Gate 缺口；
- [x] 为 R03/R04 建立严格的 `awaiting_owner_decision` 请求及互相交叉守卫的 stdlib-only validator/guard；R03 保持零 candidate，R04 只绑定当前 partial candidate，均禁止选择、批准、Review 身份与实现解锁；
- [ ] 取得 R02 全部稳定决定及既有 `PG-Dxx/TC-Pxx` 的结构化结论，补齐 upgrade exact-set 并生成 aggregate manifest；
- [ ] 补齐 `R00-D05/D07/D08/D09/D11/D13` exact candidate，逐项关闭 `R00-D01`～`D14`，再生成 R00 canonical manifest 和 exact vectors；
- [ ] 取得 R03/R04 全部 stable decision 的结构化结论，冻结 R01/R03/R04 slot/ordinal/owner/query mapping 与 child exact-set；
- [ ] 激活 GOV-D01～D06 对应的 base-owned trust root、build closure 和真实 PR authority。

### 9.3 ADR-009 独立轮次

- [ ] 七方选择 strict v1 Enterprise 路线或正式立项 v2；
- [ ] Test Owner 冻结 26 shadow + 8 canonical-only 的保留/迁移口径；
- [ ] 版本化刷新已陈旧的 API shadow baseline，并重新绑定工程设计、trust root/erratum；
- [ ] 依次完成 Candidate-Preparation、Bootstrap、外部 activation/canary、独立 Unlock；
- [ ] 只有 compound unlock 完成后，再以独立 PR 创建获准的 Harness 精确子范围。

## 10. 当前 P4 结论

截至本次审计：

- 八项 ADR 均只有推荐候选，没有 Accepted 记录；
- R00～R04 均为 `expansion_frozen`，没有 formal freeze 或 Owner approval；
- ADR-009 为 `awaiting_owner_approval / candidate_unactivated / implementation_unlocked=false`；
- 推荐首切计费模式是 `preauthorized`，但尚未被选择；
- Activation mapping 与 production projection Owner 已形成可评审候选，但尚未冻结；
- R00 已建立 14 项稳定决策和六项候选准备 CPR；六项实际候选仍未提交且不具备 ballot 条件，故没有生成 R00 待决请求、candidate manifest 或 candidate evidence；
- R01 已形成覆盖六项语义与六项治理决定的严格待决请求；它只绑定当前 87 条 partial candidate，不是完整 baseline、批准或解锁；
- R02 已有稳定 Owner 决策矩阵，但 19 项决定、既有 PG/TC 决定、aggregate manifest 与 Owner approval 均未关闭；
- R02 待决请求已机械绑定矩阵、当前 Gate manifest 和 validator；它没有批准能力，不能推进 `expansion_frozen`；
- R03/R04 已分别建立 14 项和 20 项稳定 Owner 决策矩阵，但语义、child/full-gate manifest、最终 Owner exact-set 与批准均未关闭；
- 当前唯一允许继续的工作是 Owner 决策、版本化契约/manifest 准备与 authority 路线收口；生产实现和 Harness 继续失败关闭。
