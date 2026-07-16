# W2-R04 Owner 决策矩阵 v1

> 状态：Proposed / Awaiting Owner Decision
>
> 评审 Gate：`W2-R04`
>
> 事实基线：`codex/w2-r04-consumption-audit@c9fc5f40`
>
> 结论边界：本文为 `plan_creation_spec` 的 Intent、Candidate、Billing、Graph、Activation、Business RPC、child Receipt 与 headless 发布待决项提供稳定 ID、Owner 候选和关闭证据。本文不是 Accepted Decision、不是 Formal Review Freeze，也不授权生产 DTO、Migration、Repository、Runner、Graph、RPC、A2UI 或 Tool availability。

## 1. 目的与状态词汇

R04 当前只有 Activation Approval Consumption Core 的 partial candidate，无法稳定引用其余 Intent、Candidate、Billing、Graph、Business RPC 与 child Receipt 待决项。本文把这些问题去重为严格的 `R04-D01`～`R04-D20` exact-set。

本文所有决定初始状态统一为：

```text
decision_status=awaiting_owner_decision
approval_status=not_requested
implementation_status=prohibited
evidence_status=candidate_only
```

这些值不是新的机器 Schema。Owner 必须逐项接受、拒绝或提交版本化替代方案；不得以“其他 Graph/Billing 项”或一次笼统批准兜底。

## 2. 当前机器事实与证据边界

[`w2-review-freeze-manifest.json`](./approvals/w2-review-freeze-manifest.json) 当前原始字节 SHA-256 为：

```text
a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e
```

R04 当前仍为：

- `status=expansion_frozen`；
- `freeze=null`、`reopen_exception=null`；
- 当前 `required_owner_roles=[agent_owner,business_owner,finance_owner,operations_owner,product_owner,security_owner,test_owner]`，该数组不是已批准事实；
- blocker exact-set 为 `W2_R04_FULL_GATE_BASELINE_MISSING`、`W2_R04_OWNER_APPROVAL_MISSING`、`W2_R04_VALIDATOR_BUILD_CLOSURE_PENDING`。

唯一登记的 candidate evidence 是：

| Scope | Manifest 原始 SHA-256 | Fixture | 向量 | Target tests | Build closure |
| --- | --- | ---: | ---: | ---: | --- |
| `creation_spec_activation_consumption_core_candidate` | `6ad8c58dbeeaf514994fc3db8c8beca0192de6b2e52121201060f37a7c900ba0` | 4 | 111 | 11 | 两个 direct source、stdlib-only、`candidate_unactivated` |

该证据只覆盖 unsigned Activation Consumption semantic core 的 strict JSON、single-use、revalidation、replay/Query 和纯 evaluator；不证明真实 PostgreSQL 事务、认证 Query、Intent/Candidate、Billing、Graph、Business RPC 或 child Receipt。Billing 文档只有候选 vector ID，没有 R00/R04 executable Corpus；child Receipt 仍是 `Corpus Pending`。

## 3. Stable Owner 决策

以下“推荐候选”均未被接受。

### 3.1 Scope、Intent、Candidate 与 Graph

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R04-D01` | R04 scope、共享依赖与最终 Owner exact-set | R04 只冻结 headless `plan_creation_spec`；R08 独立门禁 A2UI/C1/R09；先关闭 R00～R03 与相关 ADR，再从范围反推最终角色 | Agent、Business、Finance、Operations、Product、Security、Test；Frontend 是否加入由 R08 边界决定 | headless/R08 文件与对象 exact-set、依赖 Gate 结构化状态、role→责任→受保护平台身份映射 |
| `R04-D02` | `PlanCreationSpecIntentV1` strict schema | 分离内容类型与交付形式；冻结 create/revise 判别联合、语言/时长/画幅/时间/预算偏好/素材引用的 required、enum、limit 与 canonical 规则 | Agent、Business、Product、Security、Test | 完整字段表、strict schema、交叉规则、边界/非法/版本兼容向量 |
| `R04-D03` | Proposal/Candidate 边界 | 模型只产 strict Proposal；确定性 Validator 注入 ID、Policy、权限与资源引用后生成 Candidate；Quote/积分估算是独立确定性投影 | Agent、Business、Finance、Product、Security、Test | Proposal/Candidate 字段责任表、unknown/null/重复/越权字段拒绝、Quote 独立性与 golden |
| `R04-D04` | Candidate 生命周期 | 保留 `reviewing/active/rejected/superseded` 基线；明确 revise/edit、旧 Approval、expired/cancelled。推荐 expired/cancelled 不伪造 Business reject，继续动作必须使用新授权 | Business、Agent、Product、Security、Test | 状态迁移表、CAS/version/outbox、旧授权失效、expired/cancelled/revise/edit 正反测试 |
| `R04-D05` | Graph Output 与 unknown | 冻结 terminal `waiting_user/completed/failed/cancelled`；unknown 不生成 Result，Receipt 保持 `open/recovery_pending`；另冻结 recovery-deferred/conflict-aborted 的非终态语义 | Agent、Operations、Security、Test | Result exact-set、status/field matrix、unknown/crash/stale-fence/replay 与禁止假 terminal 测试 |
| `R04-D06` | Typed State、execution refs 与 Checkpoint | 冻结完整 State 字段/类型/来源/敏感级别/单写 Owner；Approval、Receipt、Business 长期状态不进入 Graph State；Checkpoint 只保存最小技术恢复状态 | Agent、Operations、Security、Test | State/Checkpoint 字段表、脱敏、版本兼容、Owner 写点、恢复不刷新权威事实测试 |
| `R04-D07` | Graph topology、Node/Branch 与 typed fan-in | 重画 primary ordinal 0、无 correction 的 DAG；冻结全部 `AllPredecessor` typed fan-in、缺席分支、Merge/Join、恢复分支和唯一 END | Agent、Operations、Security、Test | 文档/实际 Node/Branch exact-set、启动 Compile、fan-in/默认失败/全部路径/无环与预算测试 |

### 3.2 Billing、ModelReceipt 与 Attribution

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R04-D08` | 首切 Billing authorization mode | 一个 Definition 只允许唯一版本化模式；推荐 `preauthorized`。若选 `full_approval`，先建立独立 billable Approval/Decision/Consumption/Query，不能复用 activation Core | Business、Agent、Finance、Product、Security | ADR-005 与 `BILL-OPEN-002` disposition、Policy/cap、两模式禁止混用与迁移结论 |
| `R04-D09` | Billing model ordinal 与 correction | primary `model_call_ordinal=0`；v1 correction 禁用；旧 Node 表 ordinal 1/correction 路径不得进入实现。未来开放 correction 必须新 ordinal/Charge/Review Freeze | Agent、Business、Finance、Test | `BILL-OPEN-001`/`PLAN-P0-05` 结论、Graph 删除旧路径、ordinal/call-count/golden 向量 |
| `R04-D10` | Billing Registry slot 与 authority projection | 保留 `charge.primary`、`charge_finalization.primary` 两个独立 ref slot 并分配不同正整数 `slot_ordinal`；CHARGED=`committed`、NOT_CHARGED=`not_committed`、NOT_FOUND=`prepared` | Agent、Business、Finance、Security、Test | `BILL-OPEN-011/012`、R01 Registry、slot/ordinal/ref schema exact-set、positive/negative/not-found 投影测试 |
| `R04-D11` | Currency、Policy、cap 与 Price/Model mapping | `DORA_POINT` 仅为候选；冻结 cap/scope/lifecycle/emergency pause，以及 Price Config↔Model Config 的显式 digest mapping；不得预填 `BILL-OPEN-005` Owner | Business、Finance、Product、Security；Owner exact-set 待补 | `BILL-OPEN-003/004/005` 结构化结论、配置 canonical、版本/生效/暂停/不匹配负向测试 |
| `R04-D12` | Invocation/Publisher attribution | W2 只保存 immutable Skill/Publisher/规则引用，平台直调明确 zero earning；收益账本、结算和回收留 W4 | Business、Agent、Finance、Product、Security | Attribution DTO/digest、Snapshot/Publisher/直调规则、Charge/Receipt 逐值绑定与 W4 exclusion |
| `R04-D13` | Business Billing RPC 与扣费原子性 | Prepare 在 Business 单事务直接扣费并返回 Charge Receipt；Get/Finalize 按原 key 查询/绑定 terminal ModelReceipt；Finalize 不二次扣费 | Business、Agent、Finance、Operations、Security、Test | 公共 IDL、Command Receipt/Outbox、真实 PG 原子/并发/response-lost、余额/Ledger/Receipt exact-once |
| `R04-D14` | Provider、dispatch marker、ModelReceipt 与 unknown | Adapter 前持久化 dispatch marker；marker 后只按原 provider key Query/quarantine，不盲重试；terminal ModelReceipt schema/digest 独立冻结 | Agent、Operations、Security、Test | Provider 能力评审、marker crash window、Query/NOT_FOUND late uncertainty、无 Query quarantine、terminal Receipt 测试 |
| `R04-D15` | 失败不退款与 W4 reversal | known model failure、cancel、Validator reject、Candidate 未采用均不退款；账务差错只允许 W4 append-only reversal，不做 W2 自动退款 | Business、Finance、Product、Security、Test | `BILL-OPEN-010`、failure/outcome matrix、reversal exclusion、Ledger 不变与审计测试 |

### 3.3 Activation、Consumption、Business 决策与发布

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R04-D16` | Activation Approval/Decision identity 与 mapping | 固定 `approval_type=candidate_activation`、Decision action `approve/reject`、approve-only Consumption action `activate`、`effect_kind=creation_spec_activation`；presented/resulting version 分离 | Agent、Business、Product、Security、Test | ADR-003/011 与 R03 disposition、跨字段映射表、版本/actor/scope/旧别名拒绝测试 |
| `R04-D17` | Approval Consumption authority、single-use 与认证 Query | v1 保持 unsigned Agent DB Core；只允许受认证服务 Record/Query；冻结 exact key→approval backstop、双唯一索引、PG 原子写、stored integrity/scope 与公共错误 | Agent、Business、Operations、Security、Test | ADR-004、R03 Query contract、生产 DDL/事务/认证/审计、111 向量迁移与真实并发/崩溃测试 |
| `R04-D18` | Continuation child Receipt、slot 与 recovery | 冻结 root/parent/current/causal、`approval-decision:{approval_id}:{decision_id}`、child key、`approval_decision/approval_consumption/business_decide`、Fence 与 late-commit/unknown 隔离 | Agent、Business、Operations、Security、Test | ADR-003/004/011、R01/R02/R03 disposition、child manifest/canonical/golden、approve/reject/root!=parent/failed-after 测试 |
| `R04-D19` | Business Candidate Save/Get/Decide/Query | 冻结版本化 IDL、CAS/version/digest、Outbox 和同 schema `committed/not_committed` authority；public command identity 统一为 `tr:<child_receipt_id>:<ref_slot>:v1`，领域 backstop 独立保留 | Business、Agent、Operations、Security、Test | `PLAN-P0-07`、ADR-001/003/004、BIZ-AIGC method exact-set、同键重放/异键同资源冲突、late-commit Query |
| `R04-D20` | R08/A2UI、Release、Evidence 与 aggregate manifest 的边界 | headless R04 aggregate 显式排除 R08 Card/Action/Read/Catalog；R08 另行门禁 C1/R09。完整 R04 aggregate 只绑定 headless 范围的 design/corpus/validator/build/trust 输入 | Agent、Business、Operations、Product、Security、Test；R08 另含 Frontend | headless/R08 exclusion exact-set、R04 aggregate/build trust attestation；R08 的 Resource Read/Catalog/Definition Pin/Evidence 由其独立 Gate 关闭 |

## 4. 源待决项到稳定 ID 的映射

### 4.1 `PLAN-P0-01`～`PLAN-P0-10`

| 源 ID | Stable IDs |
| --- | --- |
| `PLAN-P0-01` | `R04-D01` |
| `PLAN-P0-02` | `R04-D02` |
| `PLAN-P0-03` | `R04-D03` |
| `PLAN-P0-04` | `R04-D02`、`R04-D03`、`R04-D05`、`R04-D06` |
| `PLAN-P0-05` | `R04-D09` |
| `PLAN-P0-06` | `R04-D12` |
| `PLAN-P0-07` | `R04-D04`、`R04-D13`、`R04-D19` |
| `PLAN-P0-08` | `R04-D05`、`R04-D06` |
| `PLAN-P0-09` | `R04-D07` |
| `PLAN-P0-10` | `R04-D20` |

### 4.2 `BILL-OPEN-001`～`BILL-OPEN-012`

| 源 ID | Stable IDs |
| --- | --- |
| `BILL-OPEN-001` | `R04-D09` |
| `BILL-OPEN-002` | `R04-D08` |
| `BILL-OPEN-003/004/005` | `R04-D11` |
| `BILL-OPEN-006/007/009` | `R04-D13`、`R04-D14` |
| `BILL-OPEN-008` | `R04-D12` |
| `BILL-OPEN-010` | `R04-D15` |
| `BILL-OPEN-011/012` | `R04-D10` |

### 4.3 P4 conflicts 与 ADR

| P4/ADR | Stable IDs |
| --- | --- |
| `P4-C01` / ADR-001 | `R04-D10`、`R04-D13`、`R04-D14`、`R04-D19` |
| `P4-C05` / R08 | `R04-D01`、`R04-D20` |
| `P4-C11` / ADR-005 | `R04-D01`、`R04-D08`～`R04-D15` |
| `P4-C13` / ADR-003 | `R04-D16`、`R04-D18`、`R04-D19` |
| `P4-C14` / ADR-004 | `R04-D17`～`R04-D19` |
| ADR-011 | `R04-D16`～`R04-D19` |

## 5. 实施依赖与最大解锁范围

```text
ADR-001/002 + R02 decisions/aggregate
  -> W2-A1（仅 Lane/Lease/Fence/HOL）
  -> R01/R02 + ADR-008/010
  -> W2-A2（execution ref / Receipt projection）

ADR-005 + R00 decisions
  -> W2-B0a（Billing Prepare/Get/Finalize）

A2 + B0a + R01/R02 + R03 + ADR-003/004/011 + R04-D01..D19
  -> headless plan_creation_spec

R08 + R04-D20
  -> A2UI/Card/Action/Read/Catalog
  -> C1/R09
```

A1 单独绝不解锁 R04；它明确禁止 Graph、Receipt、Approval、Billing 和 A2UI。即使 `R04-D01`～`R04-D19` 被接受，headless 实施仍必须等待各依赖 Gate 的正式 Approved/unlocked，而不是只依赖本文。

## 6. 状态迁移与 ballot 顺序

R04 只有同时满足以下条件，才可进入 `awaiting_review`：

1. `R04-D01`～`R04-D20` 全部有结构化 disposition，且 R00～R03、ADR-001～005/008/010/011 无冲突；
2. Intent、Proposal/Candidate、Billing、Graph、Activation、child Receipt 与 Business RPC 均有 versioned exact-set 和 executable candidate evidence；
3. R04 aggregate candidate 绑定全部 design/corpus/validator/build/toolchain/trust-root 输入；
4. `candidate_unactivated` build closure 完成锁定 toolchain 的真实 compile attestation 与 base-owned trust root；
5. 最终 Owner exact-set 从已决定的 headless/R08 范围推导；
6. 当前候选仍明确 test-only，生产实现和 availability 保持禁止。

建议 ballot 顺序：

1. 先决定 `R04-D01`，固定 headless/R08 边界与依赖；
2. 决定 `R04-D08`～`R04-D15`，冻结 Billing 与 ModelReceipt；
3. 决定 `R04-D16`～`R04-D19`，冻结 Activation、Consumption、child 与 Business authority；
4. 决定 `R04-D02`～`R04-D07`，冻结 Intent/Candidate/Graph；
5. 最后决定 `R04-D20`，形成 aggregate、release 与 Evidence 边界。

## 7. 当前结论

本文只关闭“R04 待决项没有稳定 ID”的治理缺口。`R04-D01`～`R04-D20` 全部仍为 `awaiting_owner_decision`；当前 111 条向量仍只是 partial candidate；R04 仍为 `expansion_frozen`，headless Graph、A2UI、aggregate candidate、Owner authority 和生产实现均未解锁。
