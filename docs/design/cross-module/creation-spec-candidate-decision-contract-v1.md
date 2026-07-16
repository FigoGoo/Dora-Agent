# CreationSpec Candidate Decision 公共契约候选 v1

> 状态：`Draft / Awaiting Agent + Business + Security Owner Approval / Test-only Corpus Bound`
>
> 适用范围：`plan_creation_spec` 的 Candidate Activation continuation 中，Agent 向 Business 请求 `DecideCreationSpecCandidate`，以及 write outcome 不明时查询同一业务 authority。
>
> 禁止事项：本文件不是 Approved 公共契约，不授权新增或修改 Business/Agent 公共 IDL、DTO、生成代码、Repository、Migration、Runtime、服务注册或部署配置。

## 1. 目的与边界

本候选只把下一阶段需要 owner 审核的最小机器语义固定为 test-only corpus：

1. approve/reject 是严格联合类型；
2. Decision 使用不可变 `decision_id + decision_digest`，并绑定 `presented_approval_version + resulting_approval_version`；
3. approve 必须绑定由 Agent authority 认证的 Approval Consumption，reject 必须完全不携带该绑定；
4. Business 返回同一 schema 下的 `committed` 或 `not_committed` terminal authority；
5. write response 丢失后只查询原 `idempotency_key + request_digest`；
6. `not_found` 仍是 unknown outcome，绝不构成重发授权；
7. corpus 固定严格 JSON、canonical digest、idempotency conflict 和稳定拒绝优先级。

以下内容仍明确不在本候选中：

- 公共 Thrift 方法名、字段编号与生成类型；
- Agent Consumption authority 最终采用受保护 DB authority query、签名 envelope，还是其他 owner 批准的认证方式；
- Business terminal negative authority 的表结构、事务边界和清理策略；
- `not_found_final` 或任何“无晚到提交”的线性化保证；
- Runner/Graph Node/Checkpoint/Interrupt 的生产实现。

因此，本候选不能作为“BIZ-AIGC-008 已 Approved”或“生产链路已存在”的证据。

## 2. 机器真源

当前唯一可执行候选位于：

- `agent/tests/contract/testdata/w2_r04_creation_spec_decision/creation_spec_candidate_decision_v1.json`
- `agent/tests/contract/testdata/w2_r04_creation_spec_decision/manifest.json`
- `agent/tests/contract/creation_spec_candidate_decision_v1_corpus_test.go`

它是 Agent consumer-side test-only evaluator，不是跨 Module producer/consumer parity 证明。Business owner 批准公共契约后，仍需让 Business producer 与 Agent consumer 消费同一 Approved schema/corpus，并补充 IDL/生成代码/事务实现证据。

Corpus fixture 额外保存非 wire 的 `approve_prepared_slot_authority` 与 `reject_prepared_slot_authority`。两者分别来自真实、不同的 child ToolReceipt：

```text
child_tool_receipt_id
ref_slot = business_decide
action = approve | reject
request_digest
idempotency_key
query_contract
```

它代表对应 child ToolReceipt 已原子 prepared 的受信槽。每个 Business command、Query 和 terminal authority 必须逐值复用自己槽中的 action/request digest/key/query；approve 与 reject 不共用 child receipt 或 key。`child_tool_receipt_id` 只用于 evaluator 校验 prepared key 公式，不作为独立字段进入 Business command semantic fields，也不得为同一 prepared command 新增第二个 Business key。

## 3. Command 严格联合类型

### 3.1 公共字段

`creation_spec_candidate_decision_command.v1` 的公共字段 exact-set 为：

```text
schema_version
method_key
query_contract
idempotency_key
action
principal
decision
candidate
tool_binding
```

候选 `method_key` 为 `decide_creation_spec_candidate.v1`。它只是 corpus key，不等价于已批准 RPC 名称。

`principal` 只包含：

```text
user_id
project_id
```

Business 必须使用认证调用上下文校验该 principal，不能信任请求自报身份。session、turn、run、attempt、lease、fence、trace 和 billing 不进入 Business semantic command。

### 3.2 Decision 绑定

`decision` exact-set 为：

```text
decision_receipt_id
decision_id
decision_digest
approval_id
presented_approval_version
resulting_approval_version
action
actor_user_id
actor_project_id
card_id
card_revision
```

本契约禁止出现或推导 `decision_version`。R03 Decision Receipt 没有该版本字段；并发与 replay 由不可变 Decision identity/digest、Approval 的 presented/resulting version 和 Business candidate guard 共同约束。

最小版本关系为：

```text
resulting_approval_version = presented_approval_version + 1
```

Decision action、actor user/project 必须与 command action 和认证 principal 一致。

### 3.3 Candidate guard

`candidate` exact-set 为：

```text
resource_type = creation_spec_candidate
candidate_id
expected_candidate_version
expected_candidate_digest
target_exact_set_digest
```

Business authority 必须证明 command guard 针对 `reviewing` candidate 的同一 `id/version/digest`。approve 的合法 committed after 为 `active`，reject 的合法 committed after 为 `rejected`。

### 3.4 Tool binding

`tool_binding` exact-set 为：

```text
tool_key
definition_version
intent_schema_version
result_schema_version
graph_key
tool_pin_owner
tool_pin_ref
tool_pin_digest
intent_digest
```

当前 fixture 只允许 `plan_creation_spec` 及其候选 pin/schema/graph exact binding。该绑定用于防止 continuation 使用另一个工具定义、intent 或 pin 激活相同 candidate。

### 3.5 approve/reject 联合

approve 顶层必须额外出现且只出现：

```text
consumption_authority_binding
```

reject 顶层必须完全不出现该字段；JSON `null` 不等价于“无”，同样拒绝。

`consumption_authority_binding` exact-set 为：

```text
owner
authority_ref
schema_version
authority_digest
receipt_id
receipt_version
consumption_key
consumption_digest
effect_kind
```

其候选含义是：Agent 受信 authority 已认证该 Approval Consumption，Business 接收的是该 authority 的不可变引用和 digest，而不是把 unsigned JSON core 当作认证凭证。

当前 corpus 用受信 fixture 对该引用做 exact match，只证明“生产 wire 必须认证”这一要求。它不替 Security owner 决定认证机制，也不声明当前 R04 unsigned core 已可跨服务使用。

## 4. Canonical digest 与 idempotency

Command semantic digest：

```text
SHA-256(
  UTF8("dora.creation_spec_candidate_decision_command.v1")
  || 0x00
  || canonical_json(command)
)
```

canonical JSON 使用无多余空白的 UTF-8 JSON；结构字段顺序由候选 DTO 固定。所有 command semantic 叶子，包括认证 consumption authority binding，均进入 digest。

Business command 不生成 idempotency key。它逐值复用 prepared `business_decide` slot 的冻结 key；evaluator 对该受信 slot 校验 R01 公式：

```text
tr:<child_tool_receipt_id>:business_decide:v1
```

约束：

- 同一 prepared authority 的 key、action、query contract 与 request digest 必须在首写前冻结；
- 同一 key、同一 request digest 是同一逻辑 command replay；
- 同一 key、不同 request digest 是 `BUSINESS_DECIDE_IDEMPOTENCY_CONFLICT`；
- 调用方不得为 unknown outcome 生成第二个 key；
- approve/reject 各自使用不同 child ToolReceipt 派生的 prepared key；command、Query 与 Business authority 的 `action/request_digest/idempotency_key/query_contract` 必须与对应 prepared slot 逐值相等；
- `child_tool_receipt_id` 是 Agent prepared-slot authority，不进入 Business command semantic fields；
- 查询只能携带原 key 和原 request digest；
- 更高 Agent fence、更多 attempt 或新 trace 不改变 Business key。

该公式仍需 Agent/Business owner 批准后才能成为公共契约。

## 5. Business terminal authority

`creation_spec_candidate_decision_authority.v1` 使用 envelope：

```text
schema_version
authority_digest
core
```

`authority_digest` 为：

```text
SHA-256(
  UTF8("dora.creation_spec_candidate_decision_authority.v1")
  || 0x00
  || canonical_json(core)
)
```

`core` 公共 exact-set：

```text
authority_id
authority_version
transaction_receipt_id
audited_at
idempotency_key
query_contract
request_digest
action
decision
candidate_before
outcome
```

### 5.1 committed

`outcome = committed` 时只出现 `committed`：

```text
candidate_after
consumption_authority_binding  # approve only
```

approve committed after 必须是同 candidate 的 `active` 新版本，并重复绑定相同 authenticated consumption authority。reject committed after 必须是同 candidate 的 `rejected` 新版本，并禁止 consumption binding。

### 5.2 not_committed

`outcome = not_committed` 时只出现：

```text
not_committed.reason
```

候选 reason exact-set：

```text
approval_invalid
idempotency_conflict
permission_denied
version_conflict
```

`not_committed` 是 Business terminal authority，不是 transport error，也不是普通未找到。它要求 Business owner 最终证明：该 negative authority 与 command outcome 在受审计事务语义下稳定、可 replay，且不会晚到转成 committed。当前 corpus 只测试 envelope 语义，没有提供该生产持久化证据。

## 6. Query 与 unknown outcome

Query 候选 exact-set：

```text
schema_version = creation_spec_candidate_decision_query.v1
query_contract = business.creation_spec_candidate_decision.query.v1
principal
idempotency_key
request_digest
action
decision_id
candidate_id
```

Query 是认证、只读、可重复的 authority lookup，不创建新 command，不接受第二个 idempotency key。

Query result 只允许三个状态：

| query_state | authority | 语义 | 可重发 write |
|---|---|---|---:|
| `found` | 必须存在 | 完整 terminal authority，内部 outcome 为 `committed` 或 `not_committed` | 否 |
| `not_found` | 必须不存在 | 当前未见 terminal authority，仍是 unknown outcome | 否 |
| `conflict` | 必须不存在 | key 已存在但 request/binding 冲突；隔离并人工处理 | 否 |

Transport timeout、unavailable、畸形 JSON 和 schema drift 不属于 query state，全部保持 unknown 或拒绝畸形证据。

恢复矩阵：

```text
write response valid authority
  -> 校验完整 binding -> resolve

write timeout / connection lost / invalid response
  -> query(original idempotency_key, original request_digest)

query found + valid committed/not_committed authority
  -> resolve exactly once

query found + wrong digest/action/Decision/candidate/consumption/schema
  -> reject authority, quarantine, no re-send

query conflict
  -> quarantine/manual, no re-send

query not_found / timeout / unavailable
  -> remain unknown/recovery_pending, no re-send

repeated not_found followed by found
  -> consume late terminal authority; earlier not_found never authorized re-send
```

## 7. 严格 JSON 与稳定优先级

所有 command/query/result/authority 使用：

- UTF-8；
- 禁止 duplicate key；
- 禁止未知字段；
- 禁止缺少必填字段；
- 禁止 `null` 代替缺失联合分支；
- 禁止 trailing JSON value；
- UUIDv7、positive safe integer、lowercase `sha256:<64hex>` 和 canonical UTC timestamp。

候选拒绝优先级：

```text
strict JSON / schema
  > authenticated identity
  > action / Decision
  > candidate guard
  > tool binding
  > approve/reject consumption union + authenticated authority
  > idempotency formula
  > command request digest
  > stored idempotency conflict
  > query/result schema and binding
  > terminal Business authority integrity
  > transport unknown observation
```

一旦存在完整有效的 terminal authority，其证据优先于并发观察到的 transport unknown；`not_found` 则始终不能升级成 negative authority。

Evaluator 必须把 `transport_unknown_observed` 作为实际求值输出的一部分；该观察不覆盖合法 `found` terminal authority，也不掩盖非法 authority。`ResendAllowed` 必须由 query-state policy 显式求值：未查询、timeout、`found`、`not_found`、`conflict` 均为 `false`，不得依赖 Go bool 零值。

Query result 与 authority 联合分支在 strict decode 前先做 nil/shape guard：缺少 QueryResult、`found` 缺 authority、`not_found/conflict` 偷带 authority、`committed/not_committed` 分支缺失或并存都稳定拒绝，不允许 evaluator 或 mutation helper panic。

## 8. Corpus 覆盖与 owner gate

当前 manifest exact-set 绑定 70 个 evaluator 实际消费的向量，覆盖：

- approve/reject command 与 terminal flow；
- malformed/unknown/duplicate/null/trailing/missing 严格 JSON；
- principal、Decision、Approval version、candidate、tool pin、consumption authority；
- approve/reject 双 prepared authority、冻结 action/request digest/key/query、second key、schema/query switch、command digest sensitivity、golden digest 和 idempotency conflict；
- committed/not_committed authority integrity；
- found/not_found/conflict、late found、authority replay、query timeout，以及 QueryResult/authority/terminal union 的 nil/非法 shape；
- 八组复合 mutation 的稳定优先级。

`case.kind` 只用于覆盖分类和 manifest exact-set，不进入 evaluator，也不得选择求值分支。Digest sensitivity 由 mutation 显式标记 probe，并在同步认证 principal/candidate/tool/consumption authority、重算 claimed digest 和 prepared request digest 后，走完整 command/prepared/digest 校验，再比较原始与变更后 digest。

进入任何生产实现前至少还需：

1. Agent、Business、Security owner 对字段、认证机制、idempotency 和 query semantics 的书面 Approved 结论；
2. Business 规范要求的公共 IDL/DTO/version/error/鉴权设计；
3. Business terminal authority 与 candidate mutation 的原子事务、持久化、审计和并发测试；
4. Agent/Business 对同一 Approved corpus 的 producer/consumer parity；
5. unknown outcome 故障注入，证明不会双写、不会把 `not_found` 当成未提交；
6. 生产冒烟前的真实服务注册、RPC、数据库与恢复链路证据。

在这些条件满足前，本文件和 corpus 必须继续标记为 `Draft / awaiting owner / test-only`。
