# Approval Continuation 跨对象证据契约 v1

> 状态：Test-only Executable Draft / 未 Approved
>
> 适用范围：W2-R03 的 Receipt → Approval → Decision → Tool Pin → Immutable Turn Context 因果链
>
> 生产门禁：本文件和 Corpus 不授权创建 Approval Store、Continuation Runner、Migration、Repository、Graph 或 HTTP/IDL。

## 1. 目的与边界

[`runner-session-lane-review-v1.md`](./runner-session-lane-review-v1.md) 已定义 Approval 状态机与 Continuation 保护字段，[`immutable-turn-context-design-v1.md`](./immutable-turn-context-design-v1.md) 已定义 58 字段 Turn Context，但此前两组 Corpus 只分别证明对象内部结构，没有证明同一次受信审批续跑中的跨对象因果。

本契约增加一条独立、测试专用正向链：

```text
frozen waiting_user ToolReceipt
  → immutable pending Approval binding
  → approve Decision Receipt candidate / stable SourceID
  → design-bound plan_creation_spec Graph Tool Pin
  → approval_continuation Turn Context
```

本批只证明引用、身份、版本和摘要逐值相等以及失败关闭，不证明：

- Approval/Decision/Consumption 的生产表、事务、CAS 或 Outbox；
- child ToolReceipt、Consumption Receipt、Business `Decide*` RPC 或副作用一次性；
- Lane Claim、Runner、Graph 编译、Model/Tool 实际执行；
- A2UI Action Handler、Card Projection、浏览器恢复；
- PostgreSQL、Redis、etcd、Migration 或容量性能。

因此 `SMK-009/017/033` 均不得标记通过，六个 Graph Tool 仍保持 `unavailable / DESIGN_REVIEW_PENDING`。

## 2. 权威对象与摘要域

| 对象 | Owner | schema | 摘要域 |
|---|---|---|---|
| frozen parent ToolReceipt snapshot | 测试专用 | `tool_receipt_snapshot.v1` | `dora.tool_receipt_snapshot.v1` |
| parent ToolReceipt Owner record | `agent.tool_receipt` candidate | `tool_receipt_owner_record.v1` | `dora.tool_receipt_owner_record.v1` |
| Approval immutable binding | `agent.approval_store` | `approval_binding.v1` | `dora.approval_binding.v1` |
| Decision Receipt | `agent.approval_store` | `approval_decision_receipt.v1` | `dora.approval_decision_receipt.v1` |
| Graph Tool Pin | `agent.tool_registry` | `graph_tool_pin.v1` | `dora.graph_tool_pin.v1` |
| Turn Context | Agent Turn Context Owner candidate | `session_turn_context.v1` | `dora.session_turn_context.v1` |
| 联合证据投影 | 测试专用 | `approval_continuation_cross_object_evidence.v1` | `dora.approval_continuation_cross_object_evidence.v1` |

所有带前缀的摘要只接受小写 `sha256:<64hex>`。桥接到 Turn Context 的裸摘要时，只允许严格移除固定 `sha256:` 前缀；禁止 `Trim`、大小写归一化、其他算法或宽松解析。

`tool_receipt_snapshot.v1` 摘要只证明现有 test-only Receipt evaluator 生成了完整 frozen snapshot，不冒充数据库行摘要或业务审计摘要。Context 只桥接独立 `tool_receipt_owner_record.v1` 的摘要；该 Owner record 再绑定 Receipt ID/version/write state 与 snapshot digest。Owner record 仍是候选 canonical，不决定最终表或生产摘要域。

`approval_binding.v1` 表示 Approval 创建时由 `waiting_user` Receipt 冻结并由 Decision CAS 呈交的不可变 `pending` binding。`approval_version` 是呈交版本；Decision Receipt 单独保存 `resulting_state=approved` 与 `resulting_approval_version=presented+1`。未来状态不得倒灌进 parent Receipt。它不是生产 Approval 物理行，也不决定最终 DDL。

Decision Receipt 本批只证明 Agent Owner candidate、actor scope、CAS 版本、action、SourceID 和摘要绑定，不包含密码学签名/Key Version，也不得宣称 Owner 已签字。

## 3. 唯一正向 fixture

fixture ID 固定为 `coe.approval_continuation.plan_creation_spec`，复用现有 `turn.approval.continuation` 的 58 字段 Context 模板和 `message_set.tool_pair`，但使用独立 R03 Owner records 替换以下引用：

- `authority_*` 与 Input binding 指向 Decision Receipt；
- `parent_tool_receipt_*` 指向候选 Owner record；Owner record 再绑定完整重建并通过现有 Receipt evaluator 的 frozen `waiting_user` snapshot；
- `approval_*` 指向 Approval immutable binding；
- `pinned_tool_*` 指向 `plan_creation_spec.v1alpha1 / plan_creation_spec_graph_v1` Pin；
- `resolved_refs` 由完整 Context canonical 重新生成并做 exact-set 校验。

`plan_creation_spec` 的 Owner/ref、Definition、Intent Schema、Result Schema 和 Graph Key 由测试内不可从 fixture 自行派生的固定 Registry tuple 校验，并逐字段引用 [`graphtool/plan_creation_spec-design.md`](./graphtool/plan_creation_spec-design.md)。六 Tool exact-set 同当前静态 Catalog 固定为 `plan_creation_spec/analyze_materials/plan_storyboard/generate_media/write_prompts/assemble_output`。该 Graph Tool 仍为 Draft；Corpus 中存在 Pin 只表示设计候选身份，不表示 Executable Registry 已激活或 Owner 已审批。

## 4. 必须成立的逐值链

1. Context parent Receipt ref 解出的 ID 等于 Receipt ID；Context parent Receipt digest 等于 Owner-record digest；Owner record 的 ID/version/state/snapshot digest 再与完整 frozen snapshot 逐值相等。
2. Context `parent_request_semantic_digest` 等于原 Receipt 请求摘要。
3. Receipt 的 Session、原 Turn/Run/ToolCall 与 Approval binding 逐值相等。
4. Receipt `approval_ref` 的 ID、呈交版本、digest、Card ID 等于 `pending` Approval binding。
5. Decision 的 Approval ID、呈交/结果版本、`resulting_state=approved`、actor user/project、Card revision 和 Approval binding digest 逐值相等。
6. Decision `ContinuationSourceID` 只能是 `approval-decision:{approval_id}:{decision_id}`。
7. Context Authority ID/digest 与 Decision Receipt 相等；Input ID/source digest 与其稳定 Continuation Input 相等。
8. Approval Tool Pin Owner/ref/digest 与 Tool Pin record 相等。
9. Receipt Tool key、Definition、Intent Schema、Result Schema 与 Tool Pin 相等；Graph Key 固定为 `plan_creation_spec_graph_v1`。
10. Context Receipt、Approval、Decision、Pin 的 `resolved_refs` 均恰好出现一次，完整集合不得缺失、额外或替换。
11. 局部绑定通过后，仍必须调用现有完整 Turn Context evaluator；不能用局部断言替代 58 字段、Message Set、cutoff、条件组和 Context golden 校验。

## 5. 稳定失败优先级

```text
SCHEMA
→ RECEIPT_INTRINSIC
→ IDENTITY
→ PARENT_REQUEST_DIGEST
→ APPROVAL
→ DECISION / CONTINUATION_SOURCE
→ TOOL_PIN
→ PARENT_RECEIPT_CONTEXT_BINDING
→ RESOLVED_REFS
→ TURN_CONTEXT
```

同一向量同时出现多个错误时，只返回最高优先级稳定原因；相邻层的 multi-error 表驱动测试固定该顺序。`legacy_ensure_receipt_attestation` Authority 类型不能替代 Approval Decision Authority，必须返回 `LEGACY_AUTHORITY_FORBIDDEN`。完整的合法 legacy Owner record 跨 Corpus 验证留待后续证据批次，本批不宣称已覆盖。

## 6. Test-only Corpus

机器真源位于：

- `agent/tests/contract/testdata/w2_r03_cross_object/manifest.json`
- `agent/tests/contract/testdata/w2_r03_cross_object/approval_continuation_cross_object_evidence_v1.json`
- `agent/tests/contract/approval_continuation_cross_object_evidence_v1_corpus_test.go`
- `agent/tests/contract/approval_continuation_parent_receipt_facade_v1_test.go`

Manifest 冻结 1 个 fixture、20 条向量和 13 个目标测试，并校验文件 SHA、向量 exact-set 及 AST Test 清单。向量包括 3 条合法路径与 17 条失败关闭路径，覆盖 Receipt ref/Owner-record/snapshot/request、Approval ID/version/digest、原身份、pending 创建状态、approve Decision/Source、受信 Registry Tool Pin 五字段 sensitivity、resolved refs、legacy 替换和多错误优先级。Trace、attempt、processor、read time 等运维元数据不得改变任一语义摘要。

R03 根测试不再编译或调用 R01 的内部 DTO/状态函数。facade 只把完整 parent Receipt fixture 与原 Graph Result corpus raw bytes交给 `w2r01.EvaluateApprovalContinuationParentReceiptV1`，并消费 Receipt identity、Tool Pin tuple、Approval ref 与 R01 重算的 snapshot digest 最小投影；失败只返回稳定错误码。R03 manifest、Corpus raw bytes和13个目标测试保持字节不变，package 迁移不得被误解为 R03 候选语义变更。

## 7. 后续解锁条件

本批完成后，`TC-P07` 只可把 `receipt_approval_tool_pin_binding` 从“完全缺失”推进为“已有 test-only candidate evidence”。以下条件仍未满足：

- Business Authorization 与 Agent Approval 的跨 Module exact-version 契约；
- 跨用户/项目、资源版本、权限/价格/政策再验证；
- Approval Decision/Expiry/Cancel 的真实 PostgreSQL 原子事务；
- approved Consumption Receipt 一次性与同键重放/异义冲突；
- child Receipt、Business `Decide*` 请求及 unknown-outcome 恢复；
- 产品、Business、Agent、前端、安全、财务和测试 Owner 签字。

全部 Owner 签字和真实 PostgreSQL/跨 Module 证据完成前，生产实现门禁保持关闭。
