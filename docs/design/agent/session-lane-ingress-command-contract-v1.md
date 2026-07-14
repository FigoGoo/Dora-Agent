# Session Lane Ingress 与 Command Receipt 可执行契约 v1

> 文档状态：Agent-owned Executable Draft / W2-R02 Ingress 子批次，未 Approved
>
> 设计日期：2026-07-14
>
> 适用范围：Agent Session Input 入队、全局 CommandID、Source first-write-wins、稳定 Turn 创建与命令查询
>
> 实现门禁：本文只冻结测试专用逻辑模型。跨角色评审通过前，不得据此修改生产 Migration、Repository、HTTP/IDL 或宣称 `SMK-017/020` 已通过。

## 1. 目的和当前事实

本文承接 [`session-lane-runtime-contract-v1.md`](./session-lane-runtime-contract-v1.md)，只关闭 `enqueue_input_v1` 的纯状态候选，不把全部 Lane 控制命令、权威 Evidence 或 PostgreSQL Repository 冒充为已完成。

当前生产事实：

- `agent.session_command_receipt.command_id` 已是 Agent Session Command 域全局主键，现有 `ensure_project_session_v1/v2` 通过同一表执行 first-write-wins；UUIDv7 由应用校验，数据库主键本身不验证版本位；
- `SessionRepository.Ensure` 已按 CommandID 事务级 advisory lock 串行化，Agent 会独立重算 Ensure 请求摘要；
- `session_input` 已有 `(session_id, source_type, source_id)` 和 `(session_id, enqueue_seq)` 唯一约束，但生产 `source_type` 只允许 `user_message`，没有 `source_digest/version`；现有 Receipt/Content 摘要持久字段使用裸 64 位 lowercase hex；
- W0 创建首个 `pending` Input 时尚不创建 Turn；Approval/Batch Continuation、通用 Enqueue API、Redis Wake、Scanner 与 Processor 均未实现；
- 现有 Command Receipt 的字段组和 CHECK 仍是 Ensure 专用，不能直接声称可承载 Runtime 命令。

因此，本批只新增 `_test.go` 和测试 Corpus，不修改生产 Go、SQL 或对外协议。

## 2. 固定逻辑决策

### 2.1 全局 CommandID 域

`command_id` 沿用现有 Agent Session Command 域全局 UUIDv7 命名空间：

- 不允许降为 `(session_id, command_id)`；
- 同 ID 跨 Session、跨 `command_type` 都是冲突；
- 本批 exact-set 只识别现有 `ensure_project_session_v1/v2` 和新增候选 `enqueue_input_v1`；
- transport `request_id`、TraceID、调用方时间不属于幂等身份；
- 持久 Receipt 只表示已提交，不创建 `pending/failed` Receipt。

Corpus 初始两条 Ensure Receipt 只作为跨 type 的全局 CommandID 碰撞哨兵，仅使用当前共有的 identity/request 字段；它们不是现有 PostgreSQL Ensure 行、Event 或结果字段的镜像。

同 ID、同 type、同 digest 返回原结果；同 ID 异 type 返回 version/type conflict；同 ID 同 type 异 digest 返回 semantic conflict。任何冲突都不得覆盖旧 Receipt 或泄漏旧结果引用。

### 2.2 Agent 独立重算请求摘要

`session_lane_ingress_request.v1` 的 canonical 语义字段为：

1. `schema_version`
2. `command_type`
3. `session_id`
4. `source_type`
5. `source_id`
6. `source_digest`
7. `execution_class`
8. `authority_ref_type`
9. `authority_ref_id`
10. `authority_ref_digest`
11. `message_present`
12. `content_digest`

Agent 按固定 JSON 字段顺序编码并计算 SHA-256，再以裸 64 位 lowercase hex 与调用方提交摘要做常量时间比对；本子契约所有业务 `*_digest` 都使用该裸 hex 形态，与现有 `char(64)`、CHECK 和 `validSHA256Hex` 保持一致。Corpus Manifest 的 `sha256:` 文件摘要标签属于 Manifest 格式，不得混入业务 Receipt。以下字段不得进入语义摘要：

- `request_id/trace_id/caller_time`；
- `command_id`；
- 数据库生成的 `message_id/input_id/turn_id/event_id`；
- `message_seq/enqueue_seq/event_seq`；
- 随机密文、数据库提交时间和响应 disposition。

调用方摘要只能用于比对，不能直接成为 Agent 权威摘要。

### 2.3 Source 与 Execution Class exact-set

Input Source 候选 exact-set：

- `user_message`
- `approval_continuation_result`
- `batch_continuation_result`

受信 Execution Class 候选 exact-set：

- `chat`
- `approval_continuation`
- `batch_explanation`
- `deterministic_projection`

固定映射：

| Source | 允许 Class | 测试逻辑 AuthorityRefType | 原子创建事实 |
|---|---|---|---|
| `user_message` | `chat` | `authenticated_user` | Message + Input + `turn_kind=chat` Turn |
| `approval_continuation_result` | `approval_continuation` | `approval_decision` / `approval_invalidation` | Input + `turn_kind=approval_continuation` Turn |
| `approval_continuation_result` | `deterministic_projection` | `approval_decision` / `approval_invalidation` | 仅 Input |
| `batch_continuation_result` | `batch_explanation` | `batch_terminal_event` | Input + `turn_kind=batch_explanation` Turn |
| `batch_continuation_result` | `deterministic_projection` | `batch_terminal_event` | 仅 Input |

测试逻辑的 AuthorityRefType exact-set 为 `authenticated_user/approval_decision/approval_invalidation/batch_terminal_event`，MarkerType exact-set 仅为 `session.input.accepted`。所有 Enqueue 都不得创建 Run。Approval/Batch 业务事实应选择哪种 Class，必须由受信 Owner 和版本化服务端 Policy 决定；这些 authority/marker fixture 只证明逻辑映射，不是已评审的 Actor DTO、鉴权 Policy 或物理 Event Registry。

创建 Turn 时冻结的是 Message 上下文 cutoff：在可选 Message 分配后读取当前 `last_message_seq` 写入 `context_message_seq`。它不是 Input `enqueue_seq`；无 Message 的连续 Approval/Batch Turn 可以共享同一个 Message cutoff。

### 2.4 Enqueue 单事务拓扑

首次成功 `enqueue_input_v1` 的一个 Agent PostgreSQL 短事务必须原子完成：

1. 在全局域判定 Command Receipt；
2. 锁定 Session 和 sequence counter，校验 Session scope/version；
3. 校验 `(session_id, source_type, source_id)` first-write-wins；
4. 分配连续 `enqueue_seq`，用户消息另分配 `message_seq`；
5. 创建 Input、可选 Message 和可选 Turn；
6. 写 EventLog 或 Projection/Wake Marker；
7. 冻结 Command Receipt 结果及 `result_digest`；
8. 提交后才发送非权威 Redis Wake。

任一写点失败必须整体回滚，不能消耗序号或留下半个 Receipt。入队事务不得调用 Redis、模型、Runner、Tool 或跨 Module RPC。

候选生产锁顺序必须统一为：Command advisory lock → Receipt → Session → sequence counter → event counter；具体 SQL、隔离级别和 GORM 形态仍待 Repository 评审。

### 2.5 Receipt-first 重放与 Source first-write-wins

完成 transport 认证和 scope 预检后，Repository 先按全局 CommandID 查询 Receipt。该外层预检不在纯状态 Corpus 内建模；Corpus 的 `trusted_authority` 只表示 Source/Authority 引用可信，不替代 HTTP/RPC 身份鉴别：

- 同 ID/type/digest 即使 Input 已推进，也返回首次冻结的 Message/Input/Turn/seq/result digest；
- 重放不得重新分配 ID、序号、Event 或 Wake Marker；
- Receipt 校验必须从其 Input 冻结的 Source/Class/Authority/Content 重新构造 canonical request，并与 `request_digest` 常量时间比对；
- Receipt 指向缺失或错 Session 的 Input/Turn/Message 时返回完整性错误，不能伪造 replay。

新 CommandID 命中同 `(session, source_type, source_id)` 时：

- `source_digest + execution_class + authority ref + content digest` 全部相同，复用首次冻结结果，并在同一事务写入指向原 Input 的 alias Receipt 后返回 `source_replayed`；
- 任一语义不同，返回 Source conflict，Counter 和所有事实保持不变。

alias Receipt 必须占用新的全局 CommandID；否则同一 CommandID 后续可能成功绑定另一 Source，破坏 first-write-wins。一个 Input 因此允许对应一条创建 Receipt 和多条语义相同的 alias Receipt，但不得创建第二个 Marker、Turn、序号或 Wake。alias 的表内字段组或结果子表物理形态仍待 Migration 评审。

### 2.6 Query 不创建事实

本子契约的 Command Query 只查询 `enqueue_input_v1`，必须携带 expected command type、expected digest 和受信 Session scope，返回 exact-set：

- `completed`：仅匹配 type/digest/scope 时返回冻结结果；
- `not_found`：不创建、不重试；
- `conflict`：摘要或 scope 不匹配，不返回旧 Receipt、Message/Input/Turn ID 或结果摘要。

alias Receipt 可被同一 Query 契约查询并返回首次冻结结果；Input/Turn 后续变为 running/resolved/completed 或 version 增长，不得改变 Receipt 的初始版本与 `result_digest`。

Query 不获取写锁，不发布 Wake，不触发 Enqueue 或 Runner。

### 2.7 Redis Wake 只是 Hint

Wake Envelope 最多携带 schema/version、wake ID、Session ID、最新已知 enqueue revision/seq、commit time 和 producer instance。业务正文、唯一权威状态和消费进度不得只存在于 Redis。

Redis publish 必须发生在数据库提交后；publish 失败不能回滚已接受 Input。重复、过期、早到 Wake 与 Scanner 竞争时，都必须回到同一 PostgreSQL Claim 事务。当前纯状态 Corpus 不执行 Redis、Scanner SQL 或 `P95 <= 30s` 测量。

## 3. 逻辑 Receipt 结果

`enqueue_input_v1` 成功 Receipt 至少冻结：

- `command_id/command_type/request_digest/session_id`；
- `result_schema_version/result_digest`；
- `input_id`、可选 `message_id/turn_id`；
- `enqueue_seq`、Input 初始 version、可选 Turn 初始 version；
- Event/Projection Marker 引用；
- 数据库 `committed_at`。

测试 Corpus 以单调 `committed_tick` 代替数据库时间，只用于验证结果摘要稳定性，不能据此定义生产时间类型或时钟来源。

响应 disposition 为 `created/replayed`，它不写回 Receipt 业务语义。由同 Source 不同 CommandID 命中原 Input 时写 alias Receipt 并返回 `source_replayed`；alias 继续冻结原创建 Receipt 的结果，不按当前 Input/Turn Version 重算。

测试模型中的 `expected_session_version`、`allocated_*_id`、`committed_tick`、`fail_at` 是内部 CAS、确定性分配和 crash-point 夹具，`trusted_authority` 是受信 Source fixture；它们都不是候选 HTTP/RPC DTO，禁止直接复制到外部协议。生产 Enqueue 在 Session 行锁内读取 observed version 并做内部 CAS，不能要求 100 个并发调用方共享同一精确版本。

## 4. 测试候选错误类

```text
SESSION_COMMAND_INVALID
SESSION_COMMAND_VERSION_CONFLICT
SESSION_COMMAND_CONFLICT
SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION
SESSION_INPUT_SOURCE_UNTRUSTED
SESSION_INPUT_SOURCE_CONFLICT
SESSION_INPUT_CLASSIFICATION_CONFLICT
SESSION_INPUT_STATE_CONFLICT
SESSION_INPUT_ENQUEUE_INVARIANT_VIOLATION
```

这些名称只属于当前测试 Corpus，不是已发布的 HTTP/IDL Error Registry。

## 5. PostgreSQL 候选边界（未冻结）

后续生产设计候选已进入 [`Session Lane PostgreSQL 物理设计与升级方案 v1`](./session-lane-postgresql-design-v1.md)，其中推荐保留全局 Receipt Header 并增加 origin Result 子表；在该文档 P0 和评审关闭前，以下边界仍未冻结：

- 保留现有全局 `session_command_receipt(command_id)` Header 真源，按 `command_type` 建字段组 CHECK 并保持 Ensure legacy 行可验证；禁止另建无法跨表拒绝 CommandID 碰撞的平行全局 Receipt；
- alias 行必须区分“首次结果提交时间”和“alias Receipt 记录时间”：冻结结果摘要只使用首次结果时间，审计列可另记 alias 创建时间，不能用后者重算原结果；
- `session_input` 增加 `source_digest/version`，明确 W0 legacy Input 的安全 backfill 或 claim 规则；
- Turn `PK(turn_id) + UNIQUE(input_id)`；Run `PK(run_id) + UNIQUE(input_id) + UNIQUE(turn_id)`；
- 严格 HOL partial index `(session_id, enqueue_seq, id) WHERE status IN (nonterminal)`；
- due scan、expired lease、recovery Run/open Receipt 的有界 partial index；
- `session_runtime_lease` 是唯一 TTL 真源，Input 上若保留 owner/fence 只作 provenance；
- 所有 CAS 写精确校验 expected version/fence，且 `RowsAffected=1`；
- 无数据库物理外键，跨表逻辑引用由事务、唯一约束、查询完整性和 Contract Test 共同保证。

Scanner 只能发现候选，真正 Claim/Takeover 复用同一 Repository 事务；不得先从全局 due Input 选择，否则会绕过某 Session 更早的 retry/quarantine HOL。

## 6. 集成和冒烟门禁

当前 `agent/tests/contract/testdata/w2_r02_ingress/session_lane_ingress_v1.json` 固定 42 条 exact-set 向量：14 条接受路径和 28 条拒绝路径，覆盖 alias Query、错 scope 不泄漏、第二 Session 独立序号、未知/过期 Session、四类 ID 冲突、五个首次创建事务 crash point与 alias Receipt 回滚。另有独立测试验证 Input/Turn 推进后 Command/Source Replay 仍返回冻结结果，以及 request/result digest 损坏后的完整性错误与 fail-closed。它们只消费测试专用模型。

当前纯状态契约不能关闭以下门禁：

- 100 个不同 Input 同 Session 并发入队、跨 Session 并发和双实例 race；
- counter/message/input/turn/event/receipt 各 crash point 的真实事务回滚；
- commit 成功但响应丢失后的真实 Query Receipt；
- Redis publish 失败、完全不可用、重复/过期/早到 Wake；
- Scanner 与 Redis 恢复并发 Claim，及 `P95 <= 30s`；
- Adapter 卡住 Head 时后序 Input 已持久化但执行计数为零；
- SSE/Event 顺序与 `enqueue_seq` 一致；
- 浏览器刷新/重连不重复展示，Cancel UI 只展示权威结果。

浏览器顺序不能替代数据库 HOL、Adapter 调用次数、Receipt/Marker 和故障注入证据。`SMK-017/020` 只有在生产 API、PostgreSQL、Redis、可控 Adapter 和黑盒脚本均完成后才可标记通过。

## 7. 仍未关闭

1. 10 个 Lane 控制命令及 `resume_requested_v1` 的 Command Receipt；
2. 失败命令是否保存 Receipt；
3. 现有 Receipt 表扩列、类型子表或 JSON Result 的物理选择；
4. Approval/Batch Execution Class 的真实资格 Policy 和 Authority/Actor DTO；
5. alias Receipt 在现有表扩列、全局基表 + 结果子表或其他物理方案中的落点；
6. Receipt/Event/Wake Marker 的保留、分区、清理和审计；
7. HTTP/IDL、权限、错误映射和查询防枚举策略；
8. 前向 Migration、真实 Repository、Scanner SQL 与并发/故障注入；
9. Receipt/Marker/Checkpoint 权威 Evidence Bundle；
10. Processor 生命周期、Drain 与 Eino Runner 装配。

## 8. 当前结论

本文只把 `enqueue_input_v1` 的全局 CommandID、独立摘要、Source/Class 映射、原子创建、重放和查询收敛为 Agent-owned Executable Draft。它不是生产 Schema，也不关闭 Session Lane Runtime、权威 Evidence、Redis/Scanner、Drain 或 `SMK-017/020`。
