# W2-R00 Owner 决策矩阵 v1

> 状态：Proposed / Mixed Ballot Readiness
>
> 评审 Gate：`W2-R00`
>
> 事实基线：`codex/w2-r04-consumption-audit@553782fb`
>
> 结论边界：本文把 Graph Execution Billing 的 12 个位置式未决项拆成稳定决策、责任候选和关闭证据。本文不是 Accepted Decision、不是 Owner approval、不是 canonical contract manifest 或 Formal Review Freeze，也不授权生产 Thrift、生成代码、Migration、Repository、Handler、Billing Client、ModelReceipt、Graph 或 Smoke 实现。

## 1. 目的与状态词汇

[`graph-execution-billing-contract-v1.md`](../cross-module/graph-execution-billing-contract-v1.md) 第 17 节已有 `BILL-OPEN-001`～`BILL-OPEN-012`，但这些行不能直接等同于可接受的 Owner ballot：部分行尚无具体值、Schema、Provider capability 或 Registry ordinal，`BILL-OPEN-005` 还明确标记为 Owner 未登记且不得预填。

本文将它们去重为 `R00-D01`～`R00-D14`：

- `R00-D01` 负责在范围稳定后反推最终 Owner exact-set；
- `R00-D02`～`R00-D05` 映射 `BILL-OPEN-001`～`BILL-OPEN-004`；
- `R00-D06/R00-D07` 将 `BILL-OPEN-005` 拆为责任分配与 Price Config↔Model Config exact mapping；
- `R00-D08`～`R00-D14` 映射 `BILL-OPEN-006`～`BILL-OPEN-012`。

本文使用三种决策状态：

```text
awaiting_owner_decision
candidate_incomplete_not_ballot_ready
scope_derivation_pending
```

共同状态固定为：

```text
approval_status=not_requested
implementation_status=prohibited
evidence_status=candidate_only
```

`awaiting_owner_decision` 只表示推荐候选已足以让对应 Owner 接受、拒绝或提交版本化替代方案；它不表示选择已经发生。`candidate_incomplete_not_ballot_ready` 禁止使用 `accept_recommendation` 之类选项包装不完整候选。`scope_derivation_pending` 只能在上游范围决定关闭后再进入 ballot。

## 2. 当前机器事实与候选边界

当前来源摘要如下：

| 来源 | 原始字节 SHA-256 | 用途 |
| --- | --- | --- |
| [`graph-execution-billing-contract-v1.md`](../cross-module/graph-execution-billing-contract-v1.md) | `0380efa16bd8595618f1a3e9f0d6a014344e6d567a781150091472f004769b74` | R00 Billing Draft 与 `BILL-OPEN-*` 真源 |
| [`w2-review-freeze-manifest.json`](./approvals/w2-review-freeze-manifest.json) | `a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e` | 当前 Gate 失败关闭事实 |

R00 当前仍为：

- `status=expansion_frozen`；
- `freeze=null`、`reopen_exception=null`；
- `candidate_evidence=[]`；设计文档中的候选向量表不是已登记 candidate；
- 当前 `required_owner_roles=[agent_owner,business_owner,finance_owner,product_owner,security_owner]`，该数组不是最终 exact-set；
- blocker exact-set 为 `W2_R00_BILLING_REVIEW_PENDING`、`W2_R00_OWNER_APPROVAL_MISSING`。

因此本文不得创建 R00 canonical manifest、IDL manifest、vector exact-set、target-test exact-set 或 Owner approval。R00 直接阻塞 `W2-B0a` 与 `W2-B1`；两个生产 Gate 都继续保持关闭。

## 3. Stable Owner 决策

以下 Owner 均为 provisional 候选角色。Integration Owner 只组织跨 Gate 串行收口，不能替代任何语义 Owner。

### 3.1 Gate 范围、授权、币种与 Policy

| 决策 ID | 来源 | 当前状态 | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `R00-D01` | P4-C11 / Gate | `scope_derivation_pending` | R00 最终范围与 `required_owner_roles` exact-set | 先关闭 `R00-D02`～`R00-D14`，再从计费、Provider、时钟、Registry、测试和跨 Gate projection 的实际责任反推角色；当前数组不得视为冻结 | Agent、Business、Finance、Operations、Product、Security、Test | role→责任→受保护平台身份、缺席/替补规则、最终 exact-set 与同一 head Review |
| `R00-D02` | `BILL-OPEN-001` | `awaiting_owner_decision` | primary model ordinal 与 correction | W2 v1 只允许 `model_call_ordinal=0`，correction 禁用；不得沿用旧 Node 表中的 ordinal 1 | Agent、Business、Finance、Test | R04 Node/摘要/唯一键/向量同步，ordinal 0 与 correction 拒绝测试 |
| `R00-D03` | `BILL-OPEN-002` | `awaiting_owner_decision` | 首切唯一授权模式 | 推荐低额、不可变、版本化 `preauthorized`；一个 Tool Definition 只绑定一种模式。若拒绝，保持阻塞并先设计独立 `full_approval` billable Core | Agent、Business、Finance、Product、Security | ADR-005 disposition、scope/cap、模式互斥、拒绝路径与 Graph topology exact-set |
| `R00-D04` | `BILL-OPEN-003` | `awaiting_owner_decision` | 积分 currency 稳定代码 | 候选 `DORA_POINT`，进入 Price、Charge、Ledger 与 Receipt 摘要并禁止客户端覆盖 | Business、Finance | currency registry、迁移/兼容口径、DTO/IDL/golden 与非法代码测试 |
| `R00-D05` | `BILL-OPEN-004` | `candidate_incomplete_not_ballot_ready` | Policy cap 数值、作用域、生命周期与紧急暂停 | 保留 Business-owned immutable/versioned/published 低额 Policy 方向；在 cap 数值、计价单位、User/Project/Tool/Model scope、生效/失效和 emergency pause 精确规则形成前不得发起接受 ballot | Business、Finance、Product、Security、Test | exact values、状态机、权限/审计、边界值、暂停 race、版本 Pin 与真实 PG 测试 |

### 3.2 Price/Model、Provider、ModelReceipt 与 attribution

| 决策 ID | 来源 | 当前状态 | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `R00-D06` | `BILL-OPEN-005` / P4-C11 | `awaiting_owner_decision` | Price Config↔Model Config mapping 的责任分配 | 只提出责任候选：Business 持有 Price Config 与可计价 mapping authority，Agent 持有 Model Config/route Pin，Finance 审核价格，Security/Test 关闭篡改与回归；源契约的 Owner 在本决定被接受前继续保持“未登记、不得预填” | Agent、Business、Finance、Operations、Security、Test | 逐责任 RACI、仓库与服务边界、authority Query/审计 Owner、最终 role exact-set；不得由 Integration Owner 代签 |
| `R00-D07` | `BILL-OPEN-005` | `candidate_incomplete_not_ballot_ready` | Price Config↔Model Config exact mapping | 候选使用 Business 显式 mapping，并冻结 price/model/tool definition/version/digest；`R00-D06` 未关闭前本项不能进入 ballot | Agent、Business、Finance、Operations、Security、Test | mapping key/字段 exact-set、one-to-one 或版本化多对一规则、retire/rollback、Pin mismatch、摘要与跨 RPC golden |
| `R00-D08` | `BILL-OPEN-006` | `candidate_incomplete_not_ballot_ready` | Provider request identity、idempotency 与权威 Query 能力 | 在 Model Sandbox 专项评审后按 Provider 冻结真实 request key/idempotency/query/NOT_FOUND 语义；无 Query 时 post-dispatch unknown 只能 quarantine，禁止盲调 | Agent、Operations、Security、Test | Provider capability matrix、原 key Query、late result、response lost、无 Query quarantine、凭据/日志脱敏 |
| `R00-D09` | `BILL-OPEN-007` | `candidate_incomplete_not_ballot_ready` | terminal ModelReceipt schema、canonical digest 与 Finalize binding | 与 R01/R02 联合冻结 immutable terminal ModelReceipt；Finalize 只绑定其 ref/digest/outcome，不再次扣费或改写 Receipt | Agent、Business、Security、Test | 字段/状态 exact-set、canonical/domain/version、Provider identity、terminal-before-Finalize、unknown 禁止 Finalize |
| `R00-D10` | `BILL-OPEN-008` | `awaiting_owner_decision` | W2 最小 invocation / Skill attribution | 只保存不可变、版本化引用与 direct-platform zero-earning disposition；收益计算和账本延后 W4 | Agent、Business、Finance、Product、Security | attribution 字段表、Tool/Skill/Publisher Pin、直调零收益向量、隐私/保留与 W4 forward mapping |

### 3.3 时钟、冲正、Registry slot 与 authority projection

| 决策 ID | 来源 | 当前状态 | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `R00-D11` | `BILL-OPEN-009` | `candidate_incomplete_not_ballot_ready` | 跨服务时钟证据、监控与允许偏差 | 因果 ref/Receipt/version 为权威，时钟只作附加告警；在 clock source、偏差阈值、告警与恢复规则量化前不得 ballot | Agent、Business、Operations、Test | DB/provider clock source、skew 注入、阈值/告警、时间倒退与不影响幂等/authority 的证明 |
| `R00-D12` | `BILL-OPEN-010` | `awaiting_owner_decision` | W2 不退款与 W4 reversal 边界 | known failure/cancel/validator reject 不退款；重复扣费、错扣或可证明未执行等账务差错只由 W4 append-only reversal 修复，W2 不提供自动 reversal | Business、Finance、Product、Security、Test | 错误分类、W2 禁止接口/路径、原 Charge 引用、W4 RBAC/双审设计与 no-reversal 测试 |
| `R00-D13` | `BILL-OPEN-011` | `candidate_incomplete_not_ballot_ready` | Prepare/Finalize ref-slot 名称、scope 与正整数 ordinal | 候选 `charge.primary/charge_finalization.primary`，分别使用不同 `tr:` key；两个 `slot_ordinal` 必须由完整 Registry 分配不同正整数，且与 `model_call_ordinal=0` 分离。在 exact ordinals 与唯一域冻结前不得 ballot | Agent、Business、Finance、Security、Test | R01/R04 Registry、Receipt/segment scope、两个正整数、同 slot/key 与 ordinal 0/重复拒绝向量 |
| `R00-D14` | `BILL-OPEN-012` | `awaiting_owner_decision` | Billing Authority Ref 的 terminal outcome projection | CHARGED=`committed`、terminal NOT_CHARGED=`not_committed`；Get NOT_FOUND 保持 `prepared` 且不可 resolve，避免把未知结果伪装成零副作用 | Agent、Business、Finance、Security、Test | R01 `authority_outcome` mapping、Business Query envelope、late commit/NOT_FOUND、failed-after 与 projection replay 向量 |

## 4. 原未决项到稳定 ID 的规范 crosswalk

| 原 ID | Stable IDs | 说明 |
| --- | --- | --- |
| `BILL-OPEN-001` | `R00-D02` | ordinal/correction |
| `BILL-OPEN-002` | `R00-D03` | 唯一授权模式 |
| `BILL-OPEN-003` | `R00-D04` | currency |
| `BILL-OPEN-004` | `R00-D05` | Policy exact values/lifecycle |
| `BILL-OPEN-005` | `R00-D06`、`R00-D07` | 先责任分配，再冻结 mapping |
| `BILL-OPEN-006` | `R00-D08` | Provider capability |
| `BILL-OPEN-007` | `R00-D09` | ModelReceipt |
| `BILL-OPEN-008` | `R00-D10` | attribution |
| `BILL-OPEN-009` | `R00-D11` | clock evidence |
| `BILL-OPEN-010` | `R00-D12` | reversal boundary |
| `BILL-OPEN-011` | `R00-D13` | slots/ordinals |
| `BILL-OPEN-012` | `R00-D14` | authority projection |
| P4-C11 / final role set | `R00-D01` | 全部语义范围关闭后反推 |

R04 的 `R04-D08`～`R04-D15` 只描述 `plan_creation_spec` Graph 侧对 Billing 的依赖，不能替代 R00 决策或 R00 Owner。R01 的 `R01-D02/D05` 与 R04 的 `R04-D10` 是 `R00-D13/D14` 的跨 Gate 前置，但不能单独把 R00 改为 Approved。

## 5. Ballot 与待决请求顺序

建议顺序：

1. 先决定 `R00-D03`，选择唯一授权模式；拒绝 `preauthorized` 时先补完整 `full_approval` 子契约；
2. 决定 `R00-D06`，只冻结 Price/Model mapping 的责任分配；
3. 补齐 `R00-D05/D07/D08/D09/D11/D13` 的 exact values、Schema、capability 与 ordinal，使其从 `candidate_incomplete_not_ballot_ready` 进入 `awaiting_owner_decision`；
4. 逐项决定 `R00-D02`～`R00-D14`，并同步 R01/R02/R04 与 ADR-005；
5. 最后由 `R00-D01` 反推最终 Owner exact-set；
6. 只有以上决定与关闭证据齐全后，才生成 R00 canonical manifest、IDL/vector/test exact-set，并进入受信 Owner approval。

当前不得生成 `DR-W2-R00-v1`：严格待决请求不能给 `candidate_incomplete_not_ballot_ready` 项提供“接受推荐”能力，也不能为 `BILL-OPEN-005` 伪造 Owner。后续请求必须逐项保留 readiness、绑定本矩阵与 live Gate，并继续固定 `implementation_unlocked=false`。

## 6. 当前结论

本文只关闭“R00 未决项缺少稳定、去重、可追踪 ID”的治理缺口。`R00-D02/D03/D04/D06/D10/D12/D14` 仍为 `awaiting_owner_decision`；`R00-D05/D07/D08/D09/D11/D13` 仍为 `candidate_incomplete_not_ballot_ready`；`R00-D01` 仍为 `scope_derivation_pending`。R00 继续是 `expansion_frozen`、零 candidate、零批准，生产 Billing 与 `W2-B0a/W2-B1` 均未解锁。
