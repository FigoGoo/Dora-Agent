# W2-R03 Owner 决策矩阵 v1

> 状态：Proposed / Awaiting Owner Decision
>
> 评审 Gate：`W2-R03`
>
> 事实基线：`codex/w2-r04-consumption-audit@03153d622739`
>
> 结论边界：本文为 Approval、Decision、Consumption、Continuation、child ToolReceipt 与认证 Query 的分散待决项提供稳定 ID、Owner 候选和关闭证据。本文不是 Accepted Decision、不是 Formal Review Freeze，也不授权生产 Migration、Repository、Runner、Graph、HTTP/IDL、A2UI Action 或 Business 写入。

## 1. 目的与状态词汇

R03 当前的设计候选横跨 Receipt、Approval、Decision、Turn Context、Consumption、child Receipt 和 Business Decide/Query。它们仍以章节、列表位置或未冻结字段名互相引用，不能稳定绑定 Owner 结论。本文把这些问题去重为 `R03-D01`～`R03-D14`。

本文所有决定初始状态统一为：

```text
decision_status=awaiting_owner_decision
approval_status=not_requested
implementation_status=prohibited
evidence_status=candidate_only
```

这些值不是新的机器 Schema。Owner 必须逐项接受、拒绝或提交版本化替代方案；一次笼统的“R03 同意”不能关闭任何一项。

## 2. 当前机器事实与证据边界

[`w2-review-freeze-manifest.json`](./approvals/w2-review-freeze-manifest.json) 当前原始字节 SHA-256 为：

```text
a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e
```

R03 当前仍为：

- `status=expansion_frozen`；
- `freeze=null`；
- `candidate_evidence=[]`；
- 当前 `required_owner_roles=[agent_owner,business_owner,finance_owner,frontend_owner,product_owner,security_owner,test_owner]`，该数组不是最终 exact-set；
- blocker exact-set 为 `W2_R03_CHILD_CORPUS_PENDING`、`W2_R03_OWNER_APPROVAL_MISSING`。

已有的跨对象测试候选为：

| 候选 | Manifest 原始 SHA-256 | Fixture | 向量 | Target tests | 边界 |
| --- | --- | ---: | ---: | ---: | --- |
| Approval Continuation cross-object | `cee3d3c8422df61a1c6e3cceb6a9a714c66c6308e94e96d69ad180afa568a0bf` | 1 | 20 | 13 | 仅有 `plan_creation_spec` approve 正向链；只覆盖首次 `root=parent` |

该 manifest 尚未登记为 R03 `candidate_evidence`，也没有 `design_sources`、`validator_sources`、`validator_build_sources` 或 build closure。它仍位于共享 `contract_test` package，并依赖 R01 facade 与共享 Turn Context evaluator，不能成为 Formal Freeze candidate。

R04 的 Approval Consumption Core 虽有 4 个 fixture、111 条向量和 11 个目标测试，但它属于 R04 partial candidate，不能自动计入 R03。Continuation child ToolReceipt 仍是 `Corpus Pending`，尚无 manifest、fixture、vector 或 validator。

## 3. Stable Owner 决策

以下“推荐候选”均未被接受。

### 3.1 Gate 范围、Approval 与 Continuation 因果

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R03-D01` | R03 v1 的 Approval/Consumption 范围 | 只覆盖 `candidate_activation`；若 ADR-005 选择 `preauthorized`，R03 排除 billable Core；若选择 `full_approval`，另建 billable 子 Gate/新版本，不从 activation Core 推导 | Agent、Business、Product、Security、Test；`full_approval` 时追加 Finance | ADR-005/R00 disposition、effect exact-set、包含/排除对象与 production path exact-set |
| `R03-D02` | Approval 生命周期、版本与 Decision CAS | 区分 presented/resulting Approval version；状态只允许 `pending/approved/rejected/expired/cancelled`；Consumption 不把 Approval 改成 `consumed`；重复 Decision first-write-wins | Agent、Product、Security、Test | 真实 PostgreSQL CAS/Outbox、重复/异义 Decision、expiry/cancel race、版本冲突与重放证据 |
| `R03-D03` | Decision Receipt 身份、actor scope 与 authority | Agent PostgreSQL 保存不可变 Decision Receipt；`(approval_id,decision_id)` 是 Decision 身份；v1 使用受认证内部 Query，不虚构对象 `signature/key_version` | Agent、Business、Security、Test | ADR-004 disposition、Decision canonical/Query/权限/审计 exact-set、actor/scope/expiry 负向证据 |
| `R03-D04` | Continuation Source/Input/Turn/Run 映射 | SourceID 固定为 `approval-decision:{approval_id}:{decision_id}`；first-write-wins 映射唯一 Input/Turn/Run；系统输入不得伪装 User Message | Agent、Operations、Security、Test | R02 Source Registry/Ingress/Turn Context disposition、同源重放、异义映射、重启与 HOL 测试 |
| `R03-D05` | root、parent、current 与 causal 继承 | 四者显式分离；只有 origin parent 可 root self-ref；后续 parent 必须显式携带原 root；不得自动升级 Tool Pin、Intent、资源或 Context | Agent、Product、Security、Test | `root=parent` 与 `root!=parent` 正向证据、逐字段 inheritance sensitivity、缺 root/parent 漂移失败测试 |
| `R03-D06` | child key、request canonical/digest 与重放 | child key 固定为 `(session_id,continuation_turn_id,original_tool_call_id)`；同 key 同 digest 恢复/重放，异 digest 冲突；Decision 与新 causal IDs 进入 child request digest，Consumption、Business authority、Fence 与运维元数据不进入 | Agent、Security、Test | ADR-002 mapping、strict canonicalizer、golden、同键异义、stored integrity/scope 与运维元数据不变测试 |

### 3.2 child Receipt、Consumption 与跨 Module Authority

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R03-D07` | child 状态、Fence、HOL、取消与接管 | 只有最高 Fence 可写；unknown 进入 `open/recovery_pending`，Input `quarantined` 并阻塞 HOL；Decision 已确定后取消不能伪造 terminal；接管只 Query 原 authority | Agent、Operations、Security、Test | `R02-D06/D11/D12` 结论、真实 Lane/PG/Scanner、stale fence、crash/takeover、cancel/unknown 测试 |
| `R03-D08` | Activation 字段与 slot Registry | 保留 `approval_decision`、approve-only `approval_consumption`、`business_decide` 三个逻辑角色；slot ordinal、Receipt/segment scope、ref type/schema 与 Query contract 由 pinned Definition 联合冻结，不预填数字 | Agent、Business、Security、Test | ADR-011、`R01-D05` 与 R04 disposition、跨文档字段表、ordinal 唯一域、approve/reject slot exact-set |
| `R03-D09` | Consumption Core、single-use 与存储原子性 | Agent-owned unsigned DB Core；唯一键 `(approval_id,consumption_key)`；Core/index/child ref 同事务 first-write-wins；Approval 状态仍为 approved | Agent、Data、Operations、Security、Test | R04 证据正式归属决定、Forward DDL、唯一/不可变约束、并发/崩溃/response-lost Query、无半 Core/ref 测试 |
| `R03-D10` | Consumption Record/Query 的认证、查找优先级与冲突 | 仅受认证内部服务可调用；Record 先 exact key，再 approval-only backstop；先验证 stored integrity/scope，再比较 digest；Query expected binding 与 lookup key 分离；`NOT_FOUND` 不自动证明 effect 未发生 | Agent、Business、Operations、Security、Test | ADR-004、Business→Agent IDL、caller principal/audience/scope、错误/防枚举、late commit、超时/熔断与审计测试 |
| `R03-D11` | Business `DecideCreationSpecCandidate`/Query 与 authority envelope | 唯一公开 command key 为 `tr:<child_receipt_id>:<ref_slot>:v1`；发送前 prepared；positive/negative 使用同一 envelope 并分别投影 `committed/not_committed`；未排除 late commit 的 `NOT_FOUND` 保持 unresolved | Agent、Business、Operations、Security、Test | ADR-001/003/004、Business-owned IDL/Command Receipt/Query、领域 backstop、同键重放/异键同资源冲突、真实 unknown-outcome 测试 |
| `R03-D12` | child terminal Result、failed-after 与 Event/Marker 原子性 | 所有 side-effect slot resolved 后才 freeze；approve/reject/确定失败各自使用固定 ref exact-set；Result refs 只从 resolved refs 投影；unknown 永不冻结为假失败 | Agent、Business、Operations、Security、Test | `R01-D02`、`R02-D09` 结论、child Corpus、每写点 crash、Marker/terminal 原子性与 projection replay 零副作用测试 |
| `R03-D13` | Card/Action 与 Authority 边界 | R03 只冻结 Decision 对 `card_id/card_revision` 的不可变绑定与 stale guard；Card 生命周期、Reset、展示恢复归 R08；R01 只保留最小 Approval 因果引用 | Agent、Frontend、Product、Security、Test | `R01-D03`、R08 字段责任表、stale/replay/cross-card 负向证据、Decision 与 Card revision 因果向量 |
| `R03-D14` | 最终 Owner exact-set、candidate manifest 与状态迁移 | 先决定 `R03-D01`～`R03-D13`，再从责任反推角色与 role→受保护平台身份；当前数组不得视为冻结 | Governance 与全部实际语义 Owner | child fixture/vector/test exact-set、design/validator/build/trust closure、最终 role mapping、同一 head 平台 Review |

## 4. 源待决项到稳定 ID 的映射

| 源契约/问题 | Stable IDs |
| --- | --- |
| Approval Continuation 跨对象证据 §2～§5 | `R03-D02`～`R03-D06`、`R03-D13` |
| Approval Continuation 跨对象证据 §6～§7 | `R03-D03`～`R03-D06`、`R03-D14` |
| Approval Consumption Core §2～§6 | `R03-D02`、`R03-D03`、`R03-D09`、`R03-D10` |
| Approval Consumption Core §7～§8 | `R03-D08`～`R03-D12`、`R03-D14` |
| Continuation child Receipt §2～§5 | `R03-D03`～`R03-D06` |
| Continuation child Receipt §6～§11 | `R03-D07`～`R03-D12` |
| Continuation child Receipt §12～§13 | `R03-D01`、`R03-D07`～`R03-D14` |
| `P4-C13` / ADR-003 | `R03-D04`～`R03-D06`、`R03-D08`、`R03-D11` |
| `P4-C14` / ADR-004 | `R03-D03`、`R03-D10`、`R03-D11` |
| ADR-011 Activation mapping | `R03-D01`、`R03-D02`、`R03-D08`～`R03-D11` |

## 5. 身份域与 Query 方向

下列身份域必须分离，禁止互为 alias 或复用同一个“decision version”字段：

1. Decision key：`(approval_id,decision_id)`；
2. Continuation SourceID：`approval-decision:{approval_id}:{decision_id}`；
3. child ToolReceipt key：`(session_id,continuation_turn_id,original_tool_call_id)`；
4. Agent-local Consumption key：`(approval_id,consumption_key)`；
5. Business public command key：`tr:<child_receipt_id>:<ref_slot>:v1`；
6. Business 领域 backstop：Candidate/Decision/action 的版本化唯一组合。

两类 Query 也不能混合：

- `R03-D10` 是 Business→Agent 查询 Approval/Decision/Consumption authority；
- `R03-D11` 是 Agent→Business 查询 CreationSpec Decision authority。

二者必须分别冻结 caller principal、audience、scope、IDL、状态、late-commit 语义、Retry Owner 和审计。

## 6. ADR-004 的签名冲突

跨对象证据和 Consumption Core 明确只有 digest，没有对象 `signature/key_version`。child Receipt 文档一方面允许“DB ref + authenticated Query”与签名 envelope 二选一，另一方面又把签名格式、Key Version 和轮换列为必需条件，形成待决冲突。

推荐候选是：v1 对象/wire core 禁止 `signature/key_version`；mTLS/JWT 等服务凭据轮换仍必须冻结，但它不是对象签名；审计日志签名若保留，只能作为非授权运维证据，不能进入 Core 或 Business 等价判断。离线或跨信任域对象签名必须使用新 schema/version 和独立 key-rotation 评审。该推荐只有 ADR-004 与 `R03-D03/D10` 被 Owner 接受后才能成为正式口径。

## 7. 状态迁移与 ballot 顺序

R03 只有同时满足以下条件，才可进入 `awaiting_review`：

1. `R03-D01`～`R03-D14` 均有结构化 disposition，ADR-003/004/011 与共享 R00～R02 无冲突；
2. child Receipt 发布版本化 manifest、fixture/vector/test exact-set，approve/reject、`root!=parent`、failed-after 与 unknown 路径完整；
3. R03 aggregate candidate 绑定全部 design/validator/build/toolchain/trust-root 输入；
4. 最终 Owner exact-set 从已决定范围推导；
5. 当前范围仍明确 test-only，生产实现保持禁止。

建议 ballot 顺序：

1. 先决定 `R03-D01`，固定 activation-only 或 billable 条件；
2. 决定 `R03-D13`，固定 R01/R03/R08 Card 边界；
3. 决定 `R03-D02`～`R03-D06`，冻结 Approval→Decision→Continuation→child 因果；
4. 决定 `R03-D08`～`R03-D11`，冻结 slot、Consumption 与跨 Module authority；
5. 决定 `R03-D07/D12`，冻结 recovery 与 terminal；
6. 最后由 `R03-D14` 推导 Owner exact-set 和候选 manifest。

当前角色数组含 Finance、缺 Operations。若 R03 最终为 activation-only 且 Card 生命周期归 R08，Finance/Frontend 是否保留都需从实际责任反推；child unknown-outcome、恢复和 Query 若在 R03 范围内，Operations 是必要候选。本文不直接修改 manifest。

## 8. 当前结论

本文只关闭“R03 待决项没有稳定 ID”的治理缺口。`R03-D01`～`R03-D14` 全部仍为 `awaiting_owner_decision`；R03 仍是 `expansion_frozen`，child Corpus、aggregate candidate、Owner authority 和生产实现均未解锁。
