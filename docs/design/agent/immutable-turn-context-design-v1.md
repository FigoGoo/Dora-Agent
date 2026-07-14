# Agent Immutable Turn Context 最小物理契约 v1

> 文档状态：Draft / 未 Approved
>
> 设计日期：2026-07-15
>
> 适用范围：Agent Session Lane 的 Turn 身份、不可变执行上下文、Message Set、Snapshot 引用、legacy Input 升级和 Runner 恢复读取
>
> 实现门禁：本文是评审候选，不是已批准 Schema。第 12 节 P0 与第 13 节跨角色审核全部关闭前，禁止创建生产 Migration、GORM Model、Repository、Helper、HTTP/IDL 或 Runtime Writer；禁止把本文字段直接复制为生产实现，也不得据此宣称 `SMK-017/018/020` 已通过。

## 1. 目的、范围和当前事实

本文承接：

- [`runner-session-lane-review-v1.md`](./runner-session-lane-review-v1.md) 第 5.5 节 Turn 冻结边界；
- [`session-lane-postgresql-design-v1.md`](./session-lane-postgresql-design-v1.md) 的 Turn/Run、legacy 升级和第 10 节 P0；
- [`session-lane-ingress-command-contract-v1.md`](./session-lane-ingress-command-contract-v1.md) 的 Ingress 原子创建与 `context_message_seq` 候选；
- [`session-lane-legacy-upgrade-contract-v1.md`](./session-lane-legacy-upgrade-contract-v1.md) 的 prepared/applied/verified Ledger 与 Turn Context 门禁。
- [`immutable-turn-context-decision-review-v1.md`](./immutable-turn-context-decision-review-v1.md) 将第 12 节 10 项 P0 转为逐项推荐结论、Owner、证据和签字清单；该评审包仍未 Approved，不关闭本文任何门禁。

截至 2026-07-15，生产事实是：

- `agent.session`、`session_message`、`session_input`、`session_sequence_counter`、`session_runtime_lease`、Command Receipt 和 EventLog 已存在；
- `session_message.role` 只允许 `user`，生产路径没有 Assistant、Tool、System 或事件摘要的统一模型可见历史；
- `session_skill_snapshot` Header/Item 已有数据库级 UPDATE/DELETE 拒绝触发器，Message、Input、Receipt、Event 仍不是数据库级不可变事实；
- `session_sequence_counter` 只有 `last_message_seq/last_input_enqueue_seq`，没有 Message Set prefix digest；
- Turn、Run、Turn Context、Prompt Bundle Registry、Executable Tool Registry、Model Route、Budget Snapshot、Access Scope Snapshot、Activation Policy 和 legacy Upgrade Helper 均未实现；
- 现有 Ingress/legacy Corpus 只冻结 test-only 的稳定 Turn、`context_message_seq` 和最小计数关系，不能代替完整 Context Schema 或真实 PostgreSQL 证据。

本文只收敛最小生产物理契约候选和评审问题，不创建生产 Go 或 SQL。测试专用 Corpus 必须作为独立变更，且不得被生产代码导入。

## 2. 核心候选决策

| 编号 | 候选决策 | 理由 |
|---|---|---|
| TC-D01 | `session_turn` 与 `session_turn_context` 分表 | Turn 状态需要 CAS 更新；Context 必须 append-once。混在同一行会让不可变列保护、状态迁移和 Key Rotation 边界互相污染 |
| TC-D02 | Context 以 `turn_id` 为主键，一 Turn 恰好一个 Context | 同一 Input 技术重试、Lease takeover 和 Run 恢复必须复用同一冻结语义 |
| TC-D03 | `message_cutoff_seq` 只存在于 Context | 避免 Turn 与 Context 同时保存 cutoff 形成双真源；现有 test-only `context_message_seq` 在批准后再迁移语义 |
| TC-D04 | Message Set 和 Context 使用两个独立 domain-separated digest | Message 历史篡改与其他 Snapshot/Policy 篡改需要可定位，且不同 canonical 类型不能共享摘要域 |
| TC-D05 | Context 只存不可变引用、版本和 digest，不复制 Prompt、消息明文、权限明细或 Secret | 降低敏感数据扩散；Runtime 通过受控 Store 读取并逐层重算 |
| TC-D06 | 无物理外键、无数据库级 cascade | 遵循 Dora 多 Module/逻辑引用规范；由同事务检查、唯一约束、anti-join、Repository 读取校验和 Contract Test 保证完整性 |
| TC-D07 | Ingress 首次创建需 Turn 的 Input 时，同事务创建 Turn 和 Context | 禁止可 Claim 的 Turn 缺 Context；重放不能晚绑定当前配置 |
| TC-D08 | legacy Helper 在 prepared Ledger 冻结 ID/digest 后，同事务 patch Input、写 Authority、Turn、Context 和 applied Ledger | crash、响应丢失和重放不得形成半升级事实 |
| TC-D09 | Runtime 不提供 Update/Delete Context API | Context 修复只能阻断并走显式升级/人工处置，不能覆盖历史语义 |
| TC-D10 | Context 创建时点仍是 P0 | 文档同时存在“入队创建 Turn”与“Claim 后冻结 Snapshot”描述；第 12 节关闭前 TC-D07 只是推荐候选，不是批准结论 |

## 3. 对象边界

```text
Session
  └─ Input
       └─ Turn                    mutable status/version
            ├─ TurnContext        immutable, exactly one
            └─ Run                mutable execution/recovery state

TurnContext
  ├─ Message Set cutoff + digest
  ├─ Session Skill Snapshot ref/digest
  ├─ Prompt Bundle ref/digest
  ├─ Tool Registry ref/digest
  ├─ Runtime Policy ref/digest
  ├─ Model Route ref/digest
  ├─ Budget Snapshot ref/digest
  ├─ Access Scope ref/digest
  └─ optional Continuation/Approval binding
```

Turn 表只承载稳定身份和生命周期状态。Context 表只承载首次冻结的执行语义。Run 的 owner fence、attempt、checkpoint、recovery evidence、started/terminal 时间不进入 Context。

## 4. `agent.session_turn` 候选字段

| 字段 | 候选类型 | Null | 语义与约束 |
|---|---|---:|---|
| `turn_id` | `uuid` | 否 | 应用生成 UUIDv7；主键；创建后不可修改 |
| `session_id` | `uuid` | 否 | `agent.session.id` 逻辑引用；创建后不可修改 |
| `input_id` | `uuid` | 否 | `agent.session_input.id` 逻辑引用；全表唯一；创建后不可修改 |
| `turn_kind` | `varchar(32)` | 否 | 候选 exact-set：`chat/approval_continuation/batch_explanation`；创建后不可修改 |
| `status` | `varchar(24)` | 否 | 候选 exact-set：`created/running/completed/failed/cancelled` |
| `version` | `bigint` | 否 | 初始 `1`；每次状态 CAS `+1`，必须 `>=1` |
| `created_at` | `timestamptz` | 否 | 数据库事务使用的 UTC 创建时间 |
| `updated_at` | `timestamptz` | 否 | 最近一次合法状态 CAS 时间，`updated_at >= created_at` |
| `terminal_at` | `timestamptz` | 是 | 仅 terminal 状态非空，且 `terminal_at >= created_at` |

候选唯一键和 CHECK：

- `PRIMARY KEY(turn_id)`；
- `UNIQUE(input_id)`，保证一个需 Turn 的 Input 只有一个 Turn；
- `status IN (...)`、`turn_kind IN (...)`、`version >= 1`；
- `created/running -> terminal_at IS NULL`；`completed/failed/cancelled -> terminal_at IS NOT NULL`；
- 数据库触发器拒绝修改 `turn_id/session_id/input_id/turn_kind/created_at`，但不阻止 Repository 对状态、版本和终态时间做受 Fence 保护的合法 CAS。

`deterministic_projection` Input 不创建 Turn。是否需要新增 Turn kind 必须升级 Registry、canonical schema 和评审，不允许只扩 SQL CHECK。

## 5. `agent.session_turn_context` 候选字段

### 5.1 身份与来源

| 字段 | 候选类型 | Null | 进入 Context digest | 语义与约束 |
|---|---|---:|---:|---|
| `turn_id` | `uuid` | 否 | 是 | `session_turn.turn_id` 逻辑引用和本表主键 |
| `session_id` | `uuid` | 否 | 是 | 冗余绑定 Session，必须与 Turn/Input 同 Session |
| `input_id` | `uuid` | 否 | 是 | 冗余绑定 Input；全表唯一，必须与 Turn 相同 |
| `turn_kind` | `varchar(32)` | 否 | 是 | 冗余绑定已冻结分派类型，必须与 Turn 相同 |
| `schema_version` | `varchar(64)` | 否 | 是 | 候选固定 `session_turn_context.v1` |
| `input_source_digest` | `char(64)` | 否 | 是 | Input Source/Class/Authority/Content canonical digest；不得信任调用方原值 |
| `authority_ref_type` | `varchar(64)` | 否 | 是 | 候选来自受控 Authority Registry |
| `authority_ref_id` | `uuid` | 否 | 是 | 已认证用户决策、Approval Decision、Batch terminal event 或 legacy Attestation 的稳定引用 |
| `authority_ref_digest` | `char(64)` | 否 | 是 | 对应不可变 Authority canonical digest |

### 5.2 Message Set

| 字段 | 候选类型 | Null | 进入 Context digest | 语义与约束 |
|---|---|---:|---:|---|
| `message_cutoff_seq` | `bigint` | 否 | 是 | 锁定 Sequence Counter 后冻结的 Message 边界，必须 `>=0` |
| `message_count` | `integer` | 否 | 是 | 本 Turn 可见 Message Set 数量，必须 `>=0` |
| `message_set_schema_version` | `varchar(64)` | 否 | 是 | 候选固定 `session_message_set.v1` |
| `message_set_digest` | `char(64)` | 否 | 是 | 第 6 节 canonical 的 domain-separated SHA-256 |

在“全部 `session_message` 都可见且序号稠密”的最小候选中，`message_count = message_cutoff_seq`。一旦模型可见历史包含过滤、摘要、Assistant/Tool/System 或 Event Projection，该等式不再成立，必须先关闭第 12 节 P0，再确定最终 CHECK。

### 5.3 固定 Snapshot 和执行策略

| 字段 | 候选类型 | Null | 进入 Context digest | 语义与约束 |
|---|---|---:|---:|---|
| `skill_snapshot_schema_version` | `varchar(64)` | 否 | 是 | 当前候选 `session_skill_snapshot.v1` |
| `skill_snapshot_kind` | `varchar(32)` | 否 | 是 | `empty/published_refs` |
| `skill_count` | `integer` | 否 | 是 | `0..32`；与不可变 Header/Item 相符 |
| `skill_snapshot_digest` | `char(64)` | 否 | 是 | 复用 Session Skill Snapshot set digest，不重算另一套 Skill 语义 |
| `prompt_bundle_ref` | `varchar(255)` | 条件 | 是 | 模型 Turn 使用的不可变系统 Prompt/模板 Bundle 版本引用 |
| `prompt_bundle_digest` | `char(64)` | 条件 | 是 | Prompt Bundle canonical digest，不包含运行 Secret |
| `tool_registry_ref` | `varchar(255)` | 否 | 是 | Executable Tool Registry/Tool Catalog 不可变版本引用 |
| `tool_registry_digest` | `char(64)` | 否 | 是 | Registry 中允许的 Tool key、Definition/Schema/Policy exact-set digest |
| `runtime_policy_ref` | `varchar(255)` | 否 | 是 | Runner、Retry/Failover、History materialization 等策略 Bundle 引用 |
| `runtime_policy_digest` | `char(64)` | 否 | 是 | Runtime Policy canonical digest |
| `model_route_ref` | `varchar(255)` | 条件 | 是 | 模型 Turn 的路由、Provider capability 和 Failover 集合版本引用 |
| `model_route_digest` | `char(64)` | 条件 | 是 | Model Route canonical digest，不包含 Provider Secret |
| `budget_snapshot_ref` | `varchar(255)` | 否 | 是 | 不可变 Budget Snapshot 引用 |
| `budget_snapshot_digest` | `char(64)` | 否 | 是 | 迭代、调用、Attempt、Token、墙钟、费用、Operation 和收尾策略 digest |
| `access_scope_ref` | `varchar(255)` | 否 | 是 | 已认证 user/project/session/resource scope 的不可变快照引用 |
| `access_scope_digest` | `char(64)` | 否 | 是 | Access Scope canonical digest；不得只保存当前权限查询结果的口头结论 |

`chat/batch_explanation` 要求 Prompt 和 Model Route 两个字段组都完整；`approval_continuation` 不进入模型，候选要求这两组全空。最终 all-or-none CHECK 必须等 Turn kind、Prompt Owner 和 Model Route Owner 评审后冻结。

### 5.4 Continuation/Approval 条件组

| 字段 | 候选类型 | Null | 进入 Context digest | 语义与约束 |
|---|---|---:|---:|---|
| `continuation_present` | `boolean` | 否 | 是 | 普通模型 Turn 为 `false`；Approval Continuation 为 `true` |
| `parent_tool_receipt_id` | `uuid` | 条件 | 是 | 原 ToolReceipt 稳定引用 |
| `parent_request_semantic_digest` | `char(64)` | 条件 | 是 | 原请求语义摘要，Continuation 不得改写 |
| `approval_ref` | `varchar(255)` | 条件 | 是 | Decision/Invalidation/Consumption 组合的不可变引用 |
| `approval_digest` | `char(64)` | 条件 | 是 | 当前受信 Approval 上下文 digest |
| `pinned_tool_ref` | `varchar(255)` | 条件 | 是 | 原 Tool key、Definition/Schema 和 Graph Pin 引用 |
| `pinned_tool_digest` | `char(64)` | 条件 | 是 | 受保护 Pin canonical digest |

`continuation_present=false` 时其余字段必须全空；为 `true` 时必须全非空且 `turn_kind=approval_continuation`。Approval/Batch 分类 Policy 尚未 Approved，因此本组仍是 P0 候选。

### 5.5 封印和审计元数据

| 字段 | 候选类型 | Null | 进入 Context digest | 语义与约束 |
|---|---|---:|---:|---|
| `context_digest` | `char(64)` | 否 | 否 | 第 7 节 canonical 的最终 domain-separated SHA-256 结果 |
| `created_at` | `timestamptz` | 否 | 否 | 本地事务冻结时间，只用于审计，不改变执行语义 |

TraceID、RequestID、Processor instance、Lease owner/fence、密文、Key version、数据库提交响应和 Runtime 读取时间不得进入 Context digest。若需审计，应写独立 Run/Receipt/Event 元数据，不能让技术重试改变 Turn 语义。

## 6. Message Set canonical、cutoff 与 prefix-chain

### 6.1 cutoff 规则

1. Ingress/legacy Helper 锁定 Session 和 `session_sequence_counter`；
2. 若本次创建用户 Message，先分配并写入下一 `message_seq`，再更新 Counter；
3. 从锁定后的 `last_message_seq` 取得 `message_cutoff_seq`；
4. 系统 Continuation 不伪造 Message，因此可以与前一 Turn 共享 cutoff；
5. cutoff 绝不取 Input `enqueue_seq`、Event `seq`、当前最大行或未锁定缓存；
6. retry、takeover、resume 只读取原 Context，不重新读取最新 Counter；
7. cutoff 大于 Counter、Message 存在空洞、重复 Seq 或跨 Session 时 fail closed。

### 6.2 最小 Message leaf 候选

在当前只使用 `session_message` 的候选中，按 `message_seq ASC` 编码 exact-set leaf：

```json
{
  "message_id": "uuidv7",
  "message_seq": 1,
  "role": "user",
  "content_digest": "lowercase-sha256",
  "source_kind": "stable-token",
  "source_id": "uuidv7"
}
```

Message Set canonical object 的固定字段顺序候选为：

1. `schema_version`
2. `session_id`
3. `message_cutoff_seq`
4. `message_count`
5. `messages`

摘要域候选：

```text
SHA-256("dora.session_message_set.v1" || 0x00 || canonical_json)
```

数据库存储结果使用当前 Session Schema 一致的小写 64 位 hex，不带 `sha256:`。Message ciphertext、Key version、创建时间和数据库物理位置不进入摘要；读取正文时仍必须通过 AEAD 认证并重算 `content_digest`。

### 6.3 prefix-chain 未决

每次创建 Turn 都读取 `1..cutoff` 全量 Message 可形成正确 golden，但会扩大锁事务和历史读取成本。候选优化是：

```text
empty = SHA-256(domain || 0x00 || canonical empty set header)
next  = SHA-256(domain || 0x00 || previous_digest_bytes || canonical message leaf)
```

可能需要为 `session_sequence_counter` 增加当前链头，并为 Message 保存可验证的 prefix digest 或由应用重放验证。当前没有任何字段、回填、碰撞域、空集合 golden 或跨版本策略，且 legacy 行必须由应用 canonical 代码回算，不能用 SQL 拼 JSON 猜摘要。

因此 prefix-chain 是明确 P0：在选择“全量数组摘要”或“可增量链摘要”前，不得冻结生产字段和 Migration。

### 6.4 模型可见历史缺口

当前生产表只允许 User Message，而 Runner 设计要求后续模型读取版本化的 Assistant/Tool/System 或 Continuation 事件摘要。最终 Message Set 必须在下列方案中评审选择一个：

- 扩展 `session_message` 为完整、追加式、受控角色的模型可见历史；或
- 引入版本化 History Materialization Snapshot，并在 Context 额外冻结其引用、Event cutoff 和 digest。

在该选择前，本节 canonical 只能覆盖现有 User Message，不能宣称已支持正确的全功能多轮冒烟。

## 7. Turn Context canonical

Context canonical exact-set 的字段顺序候选为：

1. `schema_version`
2. `turn_id`
3. `session_id`
4. `input_id`
5. `turn_kind`
6. `input_source_digest`
7. `authority_ref_type`
8. `authority_ref_id`
9. `authority_ref_digest`
10. `message_set_schema_version`
11. `message_cutoff_seq`
12. `message_count`
13. `message_set_digest`
14. `skill_snapshot_schema_version`
15. `skill_snapshot_kind`
16. `skill_count`
17. `skill_snapshot_digest`
18. `prompt_bundle_ref`
19. `prompt_bundle_digest`
20. `tool_registry_ref`
21. `tool_registry_digest`
22. `runtime_policy_ref`
23. `runtime_policy_digest`
24. `model_route_ref`
25. `model_route_digest`
26. `budget_snapshot_ref`
27. `budget_snapshot_digest`
28. `access_scope_ref`
29. `access_scope_digest`
30. `continuation_present`
31. `parent_tool_receipt_id`
32. `parent_request_semantic_digest`
33. `approval_ref`
34. `approval_digest`
35. `pinned_tool_ref`
36. `pinned_tool_digest`

条件字段仍出现在 exact-set 中，缺失语义编码为 JSON `null`，不得通过省略字段制造另一种 canonical 形态。摘要域候选：

```text
SHA-256("dora.session_turn_context.v1" || 0x00 || canonical_json)
```

Context digest 绑定 Turn/Input/Session 身份，因此同一业务内容在不同 Turn 上会有不同 digest。`context_digest` 本身不进入 canonical，也不设 UNIQUE；每次读取必须使用共享 canonical 包重算并常量时间比较。

## 8. 逻辑 FK、唯一键、CHECK 与不可变性

### 8.1 逻辑引用

禁止创建物理 FK 和数据库 cascade。Repository 必须在同事务或一致快照中验证：

- Turn 的 Session/Input 存在、Input 属于相同 Session 且要求创建 Turn；
- Context 的 Turn/Input/Session/turn kind 逐值相同；
- cutoff 不超过锁定 Counter，Message exact-set 无空洞且全部属于该 Session；
- Skill Snapshot Header/Item 数量、种类和摘要一致；
- Authority、Prompt、Tool Registry、Policy、Route、Budget、Access Scope 和 Continuation 引用可解析、不可变且摘要一致；
- 任一 orphan、跨 Session、未知版本、缺失引用或摘要损坏均为完整失败，不返回部分 Context。

### 8.2 数据库约束候选

- 所有 digest 使用 `char(64)` 和小写十六进制 CHECK；
- 所有版本/ref token 非空、长度有界，不接受仅空白；
- `message_cutoff_seq >= 0`、`message_count >= 0`，最终 count/cutoff CHECK 等待历史模型冻结；
- `skill_count BETWEEN 0 AND 32`，empty/published_refs 字段组与现有 Skill Snapshot 规则一致；
- Prompt/Model Route 和 Continuation 使用严格 all-null/all-not-null 组 CHECK；
- `PRIMARY KEY(turn_id)`、`UNIQUE(input_id)`；不为 `context_digest` 建唯一约束；
- 每个新增表和列必须有中文 COMMENT；表名和约束名遵循 Agent Migration 规范。

### 8.3 不可变触发器

- `session_turn_context`：拒绝任何 UPDATE/DELETE，包括值未变化的 UPDATE；
- `session_turn`：拒绝身份、kind、created_at 变化；状态只能由受 Fence/Version 保护的 Repository CAS 修改；
- `session_message`：在任何 Context Writer 启用前补数据库级 UPDATE/DELETE 拒绝，否则只能检测篡改，不能保证被引用事实 append-only；
- Context 所引用的新 Snapshot/Registry 表也必须 append-only 或以 Published Revision 形成不可变版本；仅保存指向可变配置行的 ID 不满足本契约。

Key Rotation 不得原地改 Context 语义字段。Context 不存正文或密文；引用对象的密钥轮换、active/previous Keyring 和保留策略由对应 Store 设计负责。

## 9. Repository 契约与原子边界

### 9.1 写 API 候选

不提供独立的 `CreateTurnContext`。候选领域 API 形态是：

```text
Enqueue(ctx, EnqueuePlan{
  Message?, Input, Turn?, TurnContext?, Receipt, Marker/Event
}) -> EnqueueResult

ApplyLegacyUpgrade(ctx, UpgradeApplyPlan{
  PreparedLedger, InputPatch, Authority, Turn, TurnContext, AppliedLedger
})
```

不要求立即采用这些 Go 名称，但必须保持以下事务边界：

1. Command advisory lock/Receipt 判定；
2. 锁 Session、Sequence Counter，必要时锁 Event Counter；
3. 校验 Source/Authority/Session scope 和已预解析 Snapshot；
4. 追加可选 Message，分配 Message/cutoff/Input/Event 序号；
5. 计算或复核 Message Set 和 Context digest；
6. 同事务创建 Input、Turn、Context、Receipt、Marker/Event；
7. 任一写点失败整体回滚，不消耗序号；
8. 提交后才允许发送非权威 Redis Wake。

Snapshot/Policy 解析不能在持锁事务中调用跨 Module RPC。调用前可获取受信 Published Ref；事务内必须验证其不可变身份和期望 digest，不能在重试时重新选择“当前配置”。

### 9.2 读取 API 候选

```text
LoadTurnExecutionContext(ctx, turnID, maxMessages, maxSkills)
  -> TurnExecutionContext
```

读取必须：

- 使用固定、有界批量查询，不按 Message/Skill/Ref 形成 N+1；
- 返回领域 Entity/DTO，不把 GORM Model 暴露到 Repository 外；
- 加载 Turn/Context/Input/Session，批量加载 `seq <= cutoff` Message 和 Skill Snapshot；
- 重算 Message Set、Skill Snapshot 和 Context digest；
- 认证解密 Message/Skill 内容后重验 plaintext digest；
- 未知 schema/key/ref、数量超限、引用缺失、摘要不符时 fail closed，且不返回部分明文；
- retry、resume、takeover 始终读取同一 Context，不提供“刷新配置”参数。

### 9.3 状态与 Context 的提交关系

- 需 Turn 的 Input 进入可 Claim 状态前，Turn 和 Context 必须同时存在；
- `created -> running` 前必须完整加载并验证 Context；
- Run 创建/恢复不创建第二个 Context；
- Context 损坏时 Input 不得转 `retry_wait` 或越过 HOL，应进入设计批准后的 quarantine/运维阻断路径；
- Context 读取成功不代表 Authority/Approval 仍可消费，副作用边界仍需按冻结 Policy 做权威再验证。

## 10. Forward Up、Activation 和 legacy Helper

### 10.1 Preflight

- Processor/Scanner/新 Enqueue Writer 保持 disabled；
- 从 Session root 检查 orphan、legacy Input 状态、Message 序号、Receipt/Event 保留、Skill Snapshot 完整性和可读 Key version；
- 输出只有稳定原因码、COUNT、ID/digest 的受控证据，不输出正文、密文、DSN 或 Secret；
- 任一未知状态、Message 空洞、Receipt 可变、Authority 不足或 Snapshot 不可解析均阻断。

### 10.2 Expand

- 只新增向前 Migration，不修改 002–005；
- 创建 Turn/Context 候选表和必要索引/触发器；Supporting Message/Counter 扩列必须先 nullable；
- 新旧 Writer 共存期间不得加入会拒绝旧 Ensure Writer 的最终 NOT NULL/CHECK；
- Schema 存在不代表 Lane Capability Ready，Processor/Scanner 仍为零早启。

### 10.3 Compatible Writer 与 Helper

- 部署能识别新字段但保持新能力关闭的兼容 Writer，等待旧实例排空；
- legacy Helper 只处理完整证明 eligible 的 pending chat Input；
- prepared Ledger 先冻结 facts/plan/Authority/Turn/Context digest、UUIDv7 Turn ID 和 cutoff；
- patch Input、写 Authority/Turn/Context、Ledger `applied` 同事务；不创建 Run；
- crash/restart、commit response lost 和重放逐值核对原 Ledger，零增量或稳定冲突；
- Message prefix-chain 若被采用，legacy 回算只能复用共享 canonical 包。

### 10.4 Verify 与 Activation

- 对每个 Turn 做 Turn/Input/Context/Message/Snapshot anti-join 和摘要重算；
- legacy eligible 全部进入 verified，blocker=0，active helper claim=0；
- Foundation、Lane Capability、Processor 和 Claim generation 分层，只有正确 generation 才能 Claim；
- Prompt/Tool/Policy/Route/Budget/Access/Approval Owner 的 Published Ref 全部可解析；
- 真实 PostgreSQL upgrade/crash/concurrency 和脱敏 Evidence 审核通过后，才允许打开新 Enqueue，再打开 Processor/Scanner。

## 11. Fail-safe Down 风险

Down 只承诺 pristine expand-only 回退，不承诺在已有业务事实时无损回滚。

首个 DROP/ALTER 前必须同时满足：

- compatible Writer、Processor、Scanner 全部停止；
- 持有全局 Migration Fence；
- Turn、Context、Run、Authority Snapshot、Upgrade Ledger、Enqueue Result、alias Receipt、新状态和 expanded-field 业务事实全部为零；
- 没有 active helper claim，Readiness/Claim generation 已关闭；
- 没有依赖 Context 的 Checkpoint/Receipt/Event/审计事实。

任一条件不满足，Down 首段必须抛出稳定错误并保持 Migration version/dirty 状态不变。Context 已生成后 DROP 会永久丢失重试、恢复、授权和预算语义，不能通过删除 Context、重置 Input、清空 Fence 或重建当前配置伪装回滚。

Message 不可变 Trigger 只有在 Context 为零、没有依赖其 append-only 保证的事实时才允许撤除。真实 Down 必须用 golang-migrate CLI 和 PostgreSQL 事务测试，不以手工执行片段替代。

## 12. 生产编码前必须关闭的 10 项 P0

1. **冻结时点**：统一“入队事务创建 Turn/Context”与“Claim/Runner 启动时冻结 Snapshot”的冲突，并定义 backlog 等待期间配置更新对新旧 Turn 的影响。
2. **模型可见历史**：确定 User/Assistant/Tool/System/Continuation 的权威存储、role Registry、History Materialization、摘要与 Event cutoff；当前 user-only Message 不足以支持全功能多轮。
3. **Message Set 算法**：批准全量数组摘要或 prefix-chain、空集合 golden、leaf 字段、版本升级、Counter/Message 字段和 legacy 回填算法。
4. **Prompt Bundle 真源**：确定 system Prompt、模板、Skill 注入、Summarization 后临时 Context 的 immutable Published Ref、digest Owner 和保留策略。
5. **Executable Tool Registry 真源**：确定六 Tool 的 key、Definition/Schema、Graph Pin、runtime availability、Tool Reduction 和 per-Turn/per-Call Pin 边界。
6. **Runtime/Model/Budget 真源**：冻结 Runtime Policy、Model Route、Retry/Failover owner、Budget Snapshot canonical、默认值、deadline 起算点和恢复不刷新规则。
7. **Access/Approval 真源**：冻结 Access Scope、资源版本、Approval/Quote/Operation、Continuation parent receipt/pin 的引用结构、再验证时点和 Owner。
8. **被引用事实不可变性**：批准 Message/Receipt/Event/Authority 的数据库级不可变 DDL，以及 `session.created/session.input.accepted` Retention 或不可裁剪 Marker。
9. **Turn/Run 与 Activation**：批准 Turn/Run 状态、Context 损坏映射、quarantine/HOL、legacy Ledger/Helper、Capability generation、旧 Writer drain 和零早启协议。
10. **物理与证据审核**：批准字段长度/NULL/CHECK/索引/触发器、固定查询上限、Up/Down、真实 PostgreSQL crash/race/CLI migration 矩阵和脱敏 Evidence Bundle。

这 10 项任何一项未关闭，都不得创建生产 Turn/Context Migration 或 Repository。当前允许的下一步仅是设计评审和 test-only canonical Corpus；Corpus 也必须在独立变更中创建，不能与生产实现混合。

## 13. 审核角色与批准条件

| 角色 | 必须审核的内容 | 通过条件 |
|---|---|---|
| Agent Runtime Owner | Turn/Context/Run 边界、Runner 分派、retry/resume/takeover、Eino `*schema.Message` 历史 | 同一 Turn 技术恢复不漂移；系统 Continuation 不进入错误模型路径 |
| Agent PostgreSQL/Data Owner | 字段、CHECK、索引、锁顺序、逻辑 FK、触发器、Up/Down、查询上限 | 无 N+1、无物理 FK/cascade、真实 PG crash/race/down guard 方案可执行 |
| 安全/隐私 Owner | Prompt/Message/Intent 加密、AAD、Key Rotation、Access Scope、日志/Event/Checkpoint 脱敏和保留 | Context 不复制敏感正文；未知 key/ref fail closed；审计可追溯且最小化 |
| Business/Authorization Owner | User/Project/Resource Authority、Skill/Approval/Quote/Operation 引用和版本再验证 | 模型、前端和 legacy 来源不能授予或扩大权限；跨 Module 契约版本化 |
| 产品/财务 Owner | Budget/Billing、费用和 Operation 上限、取消/超时、Approval 消费语义 | 默认值、例外、费用 unknown-outcome 和人工恢复权限明确 |
| 运维/SRE Owner | Readiness generation、Writer drain、Helper、Scanner、告警、恢复、Migration Fence | Blocker 存在时 Foundation 可用但 Claim/Run 为零；可演练升级和回退 |
| 前端/A2UI Owner | Event/Continuation 显示、刷新/重连、未知类型、Context 损坏和阻断状态 | UI 不把 Event/模型文本当权威，不重复展示，不提供伪授权入口 |
| 跨 Module Contract Owner | Agent/Business/Worker DTO、Receipt、Event 和 IDL 边界 | 不引用其他 Module `internal`，所有跨模块引用都有 Owner、版本和查询契约 |

批准条件：第 12 节 10 项逐项有结论，以上角色完成与其范围匹配的审核，本文状态改为 Approved，并同步 Runner/PostgreSQL/Ingress/legacy 文档和跨 Module Catalog。任何单一团队的代码评审不能替代安全、授权、财务、运维或跨 Module 审核。

## 14. 测试矩阵

### 14.1 Canonical 单元测试与共享 Corpus

- Message Set 空集合、单条、多条、Unicode、顺序和 domain golden；
- Context 普通 chat、batch explanation、approval continuation golden；
- 每个语义字段逐项篡改必须改变 digest；
- `created_at/trace/request_id/ciphertext/key_version` 不得改变语义 digest；
- 条件字段缺失、额外字段、未知 schema/domain、uppercase/带前缀 digest fail closed；
- cutoff 取 Message Seq，不取 enqueue/event seq；后续 Message 不改变旧 Context；
- 同 Turn retry/resume/takeover 字节级复用；不同 Turn ID 不共享 Context digest；
- prefix-chain 若采用，必须与批准的 reference implementation/golden 一致并覆盖 legacy 回算。

### 14.2 Repository 与真实 PostgreSQL

- Up 后表、列、中文 COMMENT、唯一键、CHECK、索引、触发器和“无物理 FK” exact 核对；
- Message/Input/Turn/Context/Receipt/Marker/Event 每个写点故障全部回滚且不消耗序号；
- 同 Input 只能有一个 Turn/Context；Context/Message UPDATE/DELETE 包括 no-op 均被拒绝；
- Turn/Input/Context 跨 Session、kind 不同、orphan、cutoff 超 Counter、Message 空洞/重复/乱序均拒绝；
- Message plaintext/content digest、Message Set、Skill Header/Item 和 Context digest 任一损坏均拒绝且不返回部分明文；
- unknown active/previous key、未知 ref/version、超 `maxMessages/maxSkills` fail closed；
- 同 Session 100 个不同 Enqueue 无洞，跨 Session 并发不互相阻断；
- 同 Command/Source 100 次重放一个 Turn/Context，异摘要冲突不覆盖；
- commit 成功响应丢失后查询原 Receipt/Turn/Context 零增量；
- 真实锁等待、事务取消、deadline、连接断开、进程崩溃和双 Client race。

### 14.3 legacy 升级与 Migration

- 从真实 Migration 005 的 V1/V2/empty-prompt fixture 执行 Preflight/Expand/Helper/Verify；
- eligible、unsupported、archived、claimed/running/retry/terminal、Message 不可读、Event 被裁剪、global orphan exact-set；
- prepared 后、Input patch 后、Authority/Turn/Context insert 后、apply commit response lost 各 crash point；
- 同 Ledger 同摘要重放零增量，异摘要稳定冲突；Turn Context 与 Ledger cutoff/digest 逐值一致，Run=0；
- blocker 存在时 Lane/Processor/Claim Ready 为 false，Foundation 仍可服务；
- pristine Down 成功；每类 Header/Result/Authority/Ledger/Turn/Context/Run/新状态独立存在时首个 DDL 前拒绝；
- golang-migrate CLI 的 version/dirty、修复步骤和 Migration Fence 证据。

### 14.4 Runner 与全功能冒烟

- Prompt、Skill、Tool Registry、Runtime Policy、Model Route、Budget、Access Scope 热更新不改变旧 Turn；
- retry、Failover、checkpoint resume、Lease takeover 使用相同 Message cutoff 和 Context digest；
- `chat/batch_explanation` 进入唯一 ChatModelAgent，`approval_continuation` 跳过模型并调用原 pinned Graph；
- 后续 Message 已持久化但旧 HOL Run 的模型输入不包含它；
- Approval/资源/价格/权限变化按冻结 Policy 重新验证，不能覆盖原 Context；
- Context 或引用损坏时不调用模型、Tool、Business RPC，不释放 HOL，不打印正文；
- Redis Wake 丢失/重复/早到只回到 PostgreSQL Claim，不重建 Context；
- Event/SSE/浏览器刷新与重连不重复投影，不以浏览器顺序代替数据库证据；
- 日志、Trace、Metric、Event、Checkpoint 不出现 Prompt、消息明文、Secret 或完整敏感 Context。

## 15. 当前结论与下一步

当前结论：**Draft，未 Approved，不通过生产实现门禁。**

10 项 P0 的推荐选择、明确拒绝方案和逐角色签字入口已经集中到 [`immutable-turn-context-decision-review-v1.md`](./immutable-turn-context-decision-review-v1.md)。这只把讨论收敛为可审核决策，不表示任一 P0 已关闭。

推荐下一步顺序：

1. Agent Runtime、PostgreSQL 和安全先联合关闭冻结时点、模型可见历史与 Message Set/prefix-chain；
2. 各 Snapshot Owner 提供不可变 ref/digest/读取/保留契约；
3. Agent、Business、产品/财务冻结 Access/Approval/Budget；
4. 运维冻结 Activation generation、Helper、Migration Fence 和 Down guard；
5. 另行创建 test-only canonical Corpus 并完成跨语言 golden；
6. 所有审核通过并把本文改为 Approved 后，才拆分生产 Migration、Repository、Helper 和 Runtime PR。

在此之前，任何“先建空表以后补字段”、只保存一个 opaque Context JSON、在 Claim 时读取最新配置、或依赖当前 Message/Receipt 可变行的实现，都不能满足 immutable Turn Context。
