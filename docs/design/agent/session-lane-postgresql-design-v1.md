# Session Lane PostgreSQL 物理设计与升级方案 v1

> 文档状态：Agent-owned Database Design Draft / W2-R02，未 Review Ready、未 Approved
>
> 设计日期：2026-07-14
>
> 适用范围：`enqueue_input_v1`、全局 Command Receipt、W0 legacy Input 升级、Turn/Run/Lease、严格 HOL 与真实 PostgreSQL 证据
>
> 实现门禁：本文仍是候选物理设计。第 10 节 P0 和跨角色评审关闭前，不得创建生产 Migration/Repository，不得启用 Processor，也不得宣称 `SMK-017/020` 已通过。

## 1. 目的与当前事实

本文承接：

- [`session-lane-runtime-contract-v1.md`](./session-lane-runtime-contract-v1.md) 的 60 条 Lane 状态向量；
- [`session-lane-ingress-command-contract-v1.md`](./session-lane-ingress-command-contract-v1.md) 的 42 条 Ingress/Receipt 向量；
- [`runner-session-lane-review-v1.md`](./runner-session-lane-review-v1.md) 的 Runner、恢复、Drain 和冒烟门禁。

当前生产 Schema/代码事实：

- `agent.session_command_receipt(command_id)` 已是 Session Command 域唯一全局 Header，Ensure V1/V2 共用同一主键和 advisory lock；
- Receipt 的 `skill_snapshot_digest/skill_count`、Message/Input 成对 CHECK 和 GORM 完整性校验仍是 Ensure 专用；
- `session_sequence_counter`、`session_event_counter`、`session_runtime_lease` 已存在；后者还没有 Claim/Heartbeat/Takeover Repository；
- `session_input` 只允许 `user_message`，生产 Repository 只创建 `pending/attempts=0/无 owner/无 lease/fence=0`；Schema 虽允许其他状态，但当前没有合法生产 Writer；
- `session_input` 自带 `lease_until`，若继续读取会与 `session_runtime_lease` 形成双 TTL 真源；
- 已有 `session.input.accepted` EventLog，可作为持久 Marker；仓库没有独立 Marker、Turn 或 Run 表；
- 当前 Claim partial index 只能发现 `pending/retry_wait`，不能证明每个 Session 的最早非终态 Head。

本批推荐物理方案，但不新增 `.sql`、GORM Model 或 Repository。

## 2. 已收敛的候选决策

| 编号 | 候选决策 | 原因 |
|---|---|---|
| PG-D01 | 保留 `session_command_receipt` 为唯一全局 Command Header；新增 Enqueue Result 子表 | 避免 Ensure/Enqueue 各自一张全局表导致同 CommandID 双成功，也避免继续向 Header 堆全部结果字段 |
| PG-D02 | alias 只新增 Header，指向首次 primary；结果子表只存一份 | 一个 Input 可以有多个 CommandID，但首次结果、版本和提交时间只有一份 |
| PG-D03 | `session.input.accepted` EventLog 就是 Ingress Corpus 的持久 Marker | 当前已有 EventLog/Counter/AppendOnce 约束，不再创造第二套投影真源 |
| PG-D04 | 外部 Enqueue DTO 不携带精确 `expected_session_version` | 100 个不同 Source 并发时，外部版本会让 99 个合法请求 stale；Session Version 只做事务内防御 CAS |
| PG-D05 | `session_runtime_lease` 是唯一 TTL 真源 | Input owner/fence 只保留 Claim provenance；Input `lease_until` 退役且禁止读取 |
| PG-D06 | 仅自动升级可严格证明的 legacy pending Input | claimed/running/retry/terminal 行缺权威 Run/Effect，重置会伪造恢复结论 |
| PG-D07 | legacy pending 回填 Turn，不创建 Run；Run 在首次正常执行 Claim 同事务创建 | Run 表示真实执行尝试，Migration 不能伪造从未发生的 Claim/执行；cancel-specific Claim-before-Run 保持 Run absent |
| PG-D08 | Head 是每 Session 最小非终态 `enqueue_seq` | retry_wait/quarantined 或 Lease 已持有时的 Head 都必须阻塞后序，Scanner 不能全局挑任意 due Input |

`expected_session_version`、`allocated_*_id`、`committed_tick` 和 `fail_at` 只属于测试模型。生产应用生成 UUIDv7，事务锁行读取当前版本，所有更新仍以 `WHERE version = observed_version` 和 `RowsAffected=1` fail-closed。

## 3. 全局 Receipt Header 与 Enqueue Result

### 3.1 Header 演进

保留表名和主键：

```text
agent.session_command_receipt
  command_id             uuid PK
  command_type           varchar(64)
  request_digest         char(64)
  session_id             uuid
  result_version         integer
  completed_at           timestamptz   -- 首次业务结果冻结时间
  receipt_kind           varchar(16)   -- primary | source_alias
  origin_command_id      uuid          -- primary 自指，alias 指向首次 primary
  recorded_at            timestamptz   -- 当前 Header 实际记录时间

  message_id/input_id/skill_snapshot_digest/skill_count
                         -- 仅 Ensure V1/V2 字段组
```

Forward Migration 对现有 Ensure 行回填：

- `receipt_kind=primary`；
- `origin_command_id=command_id`；
- `recorded_at=completed_at`；
- Ensure 原字段和值保持不变。

字段组 CHECK 候选：

- Ensure V1/V2 只能是 primary、`origin_command_id=command_id`；现有 Message/Input 配对和 Skill Snapshot 非空约束继续成立；
- `enqueue_input_v1` 才允许 source_alias；Header 中 Message/Input/Skill Snapshot 等 Ensure 专属字段必须全部为 NULL，不得写“空 Skill”哨兵；
- Enqueue Header 的 `result_version=1`，详细 Schema Version 只存 Result 子表；
- primary 必须 `origin_command_id=command_id` 且 `recorded_at=completed_at`；
- source_alias 必须 `origin_command_id<>command_id` 且 `recorded_at>=completed_at`；
- Tighten 阶段移除 Ensure 专属字段的 DEFAULT/NOT NULL，再用按 `command_type` 分组的 CHECK 收紧；现有 Ensure 值不得改写；
- 所有业务 Digest 为裸 64 位 lowercase hex。

GORM 不能继续用一个全非空 Ensure Model 写所有 Header：公共 Header Model 的 Ensure 专属列必须改为 nullable，Ensure Mapper/Validator 继续要求非空，Enqueue Mapper 继续要求全空；Result 使用独立 Model。该重构与 Tighten 必须同批验证。

### 3.2 Enqueue Result 子表

候选表：

```text
agent.session_enqueue_result
  origin_command_id      uuid PK
  result_schema_version  varchar(64)
  result_digest          char(64)
  session_id             uuid
  message_id             uuid NULL
  input_id               uuid NOT NULL UNIQUE
  turn_id                uuid NULL
  enqueue_seq            bigint NOT NULL
  input_version          bigint NOT NULL
  turn_version           bigint NOT NULL
  accepted_event_id      uuid NOT NULL UNIQUE
  result_committed_at    timestamptz NOT NULL
```

约束候选：

- `enqueue_seq/input_version > 0`；
- `turn_id IS NULL` 当且仅当 `turn_version=0`；
- `result_committed_at` 与 primary Header `completed_at` 逻辑相等；
- primary Header 必须 `recorded_at=completed_at=result_committed_at`；alias Header 必须复制 origin primary 的 `completed_at`，只有 `recorded_at` 使用 alias 写入时间；
- `result_digest` 只从固定 Result Schema、所有结果引用、初始版本和 `result_committed_at` 重算，禁止使用 alias `recorded_at`；
- 不设置数据库物理 FK；Repository、唯一约束和真实 PG Contract Test 校验所有逻辑引用。

一个 Source 首次创建写 primary Header + 一条 Result；后续同义 Source 新 CommandID 只写 source_alias Header。alias Header 复制 primary 的 `command_type/request_digest/session_id/completed_at`，但 `recorded_at` 记录 alias 成功时间。Query 先解析 alias 的 `origin_command_id`，再读取唯一 Result。

### 3.3 Query 和完整性

Query 必须先比较受信 scope、expected type 和 expected digest：

- query schema、CommandID、expected type（只允许 `enqueue_input_v1`）或 digest 格式非法：`SESSION_COMMAND_INVALID`；
- Header 不存在：`not_found`；
- Header 的 type、digest 或 Session scope 任一不匹配：`conflict`，且结果字段全空；Ingress Query 不把 type mismatch 改成 Ensure Query 的 version error；
- alias 必须指向 `receipt_kind=primary` 且自指的 origin Header；
- alias 与 primary 的 type/digest/session/completed_at 必须完全一致；
- type/digest/scope 完全匹配后，origin/result 缺失、Input/Message/Turn/Event 引用错位、从 Input 重算的 request digest 不同、Result digest 不同，均返回 `SESSION_COMMAND_RECEIPT_INTEGRITY_VIOLATION`；
- Query 不写表、不发 Wake、不调用 Runner。

## 4. Enqueue 事务与锁顺序

生产 Enqueue 统一使用以下顺序，所有相关 Repository 不得反向加锁：

1. transport 认证与 Session scope 预检在事务外完成；
2. `pg_advisory_xact_lock(hashtextextended(command_id, 0))`；
3. 查询全局 Receipt Header，命中时执行 replay/conflict；
4. `SELECT session ... FOR UPDATE`，确认 active；
5. `SELECT session_sequence_counter ... FOR UPDATE`；
6. `SELECT session_event_counter ... FOR UPDATE`；
7. 在 Session 锁保护下复查 `(session_id, source_type, source_id)`；
8. Source 同义时写 alias Header；异义时返回 Source conflict；
9. 首次 Source 分配 Message/Input/Event 连续序号，创建可选 Message、Input、可选 Turn 和 `session.input.accepted` Event；
10. 更新 Session Version，精确 CAS 且 `RowsAffected=1`；
11. 写 Enqueue Result，最后写 primary Header；
12. Commit 成功后才尝试 Redis Wake。

不同 CommandID 的同 Source 竞争由 Session 行锁串行化；不同 Session 没有共享 Counter/Session 行锁。事务内不执行 RPC、Redis、模型、Tool、Runner 或加密服务调用；Message Envelope 必须预先生成并在事务前验证。

## 5. `session_input` Expand 与 legacy 升级

### 5.1 候选字段

`session_input` 至少 Expand：

```text
source_contract_version
source_digest
execution_class
authority_ref_type
authority_ref_id
authority_ref_digest
content_digest
version
cancel_requested
cancel_version
cancel_command_id
cancel_request_digest
resolution_code
claim_owner
claim_fence
```

同时扩展：

- `session_message.source_kind` 接受新用户消息来源；候选使用 `source_kind=user_message/source_id=Input.source_id`，保持同 Session 来源唯一；
- `session_event_log.source_kind` 接受 `enqueue_input_v1`，`source_id=origin_command_id`，`projection_index=0`，aggregate 指向 Input 初始 Version；
- `session.input.accepted` payload 继续是无正文安全投影，Message Ciphertext/Prompt 不进入 Event/Receipt/Trace。

新 Ingress exact-set 使用 `authenticated_user/approval_decision/approval_invalidation/batch_terminal_event`。legacy 行不得伪装成历史已冻结的 `authenticated_user`，最小兼容映射候选为：

- `source_contract_version` 明确为 `legacy.ensure_project_session.v1` 或 `legacy.ensure_project_session.v2`；
- `source_digest=Ensure Receipt.request_digest`；
- `content_digest=Message.content_digest`；
- `execution_class=chat`；
- `authority_ref_type=ensure_project_session_receipt`（仅 legacy 内部允许）；
- `authority_ref_id=Ensure command_id`；
- `authority_ref_digest=Ensure request_digest`。

`ensure_project_session_receipt` 不进入新 Enqueue DTO/Policy exact-set。若安全评审拒绝该兼容类型，则必须新增不可变 Authority Snapshot，由应用生成 UUIDv7 和 canonical digest 后回填；现有数据不足以用 SQL 还原历史认证决策。

### 5.2 可自动升级集合

只有同时满足以下条件的 legacy Input 可自动升级：

- Session active；Input `pending/attempts=0/owner=NULL/lease_until=NULL/fence=0`；
- Source 为 `user_message`，Message 与 Input/Session 一致；
- SourceID 对应 Ensure V1/V2 Receipt，Receipt 的 Session/Message/Input 全匹配；
- Sequence Counter、Event Counter、`session.input.accepted` Event 无矛盾；Runtime Lease 行必须存在且严格为 `owner=NULL/until=NULL/fence=0/version=1`；
- 同 Session 的序号、Source 和 Message 唯一约束无缺口。

claimed/running/retry_wait/resolved/dead、archived Session pending、缺 Receipt/Message/Event、非空旧 Lease 等行一律进入 upgrade-block 清单，`Runtime Ready=false`。不得重置为 pending、清零 Fence、伪造 Run 或猜测 Effect State。

### 5.3 Turn 回填

PostgreSQL 16 不能直接可靠生成项目要求的 UUIDv7，Turn 回填由 runtime-disabled 的应用辅助命令分批执行：

- 锁定并再次校验 eligible Input；
- 应用生成 Turn UUIDv7；
- 创建 `turn_kind=chat/status=created/version=1`；
- 从锁定的 `session_sequence_counter.last_message_seq` 冻结 `context_message_seq` 和已评审的执行上下文引用；若本次 Enqueue 创建 Message，则先分配 Message Seq，再把更新后的值冻结为 cutoff；
- 幂等提交，支持中断重启；
- 不创建 Run。

Ingress 纯状态 Corpus 已同步改为 Message `context_message_seq` cutoff，不再把 Input `enqueue_seq` 映射到物理列。Turn 的 Skill/Prompt/Tool Registry/Budget Snapshot 引用与摘要仍是第 10 节 P0；未冻结前不得开始生产 Turn Migration。

## 6. Turn、Run、Lease 与 HOL 候选

### 6.1 Turn

```text
session_turn(
  turn_id uuid PK,
  session_id uuid,
  input_id uuid UNIQUE,
  turn_kind varchar,
  status varchar,
  context_message_seq bigint,
  version bigint,
  created_at/updated_at/terminal_at
)
```

Turn exact-set：`created/running/completed/failed/cancelled`。`deterministic_projection` Input 没有 Turn；chat、approval continuation、batch explanation 各有一个稳定 Turn。

### 6.2 Run

```text
session_run(
  run_id uuid PK,
  session_id uuid,
  input_id uuid UNIQUE,
  turn_id uuid UNIQUE,
  status varchar,
  version bigint,
  owner_fence bigint,
  recovered_from_fence bigint,
  effect_state varchar,
  created_at/updated_at/terminal_at
)
```

Run exact-set：`created/running/recovery_pending/completed/failed/cancelled`。一个 Input/Turn 只有一个稳定 Run；retry、进程重启和 takeover 复用该 Run。Run 仅在首次正常执行 Claim 同事务创建；cancel-specific `claim_head` 在 Claim 前尚无 Run 时不得补造 Run，后续 cancellation 仍保持 Run absent。terminal Run 必须有 resolved Effect，unknown 只能进入 recovery_pending/quarantine 对账，不能伪造重试安全。

### 6.3 Lease 与索引

- Runtime Lease 到期索引：`(lease_until, session_id) WHERE lease_owner IS NOT NULL`；
- Input HOL 索引：`(session_id, enqueue_seq, id) WHERE status IN ('pending','claimed','running','retry_wait','quarantined')`；
- durable pending 候选扫描：`(available_at, session_id, enqueue_seq, id) WHERE status='pending'`；
- retry scan：`(available_at, session_id, enqueue_seq, id) WHERE status='retry_wait'`；
- Run recovery：`(updated_at, session_id, run_id) WHERE status='recovery_pending'`；
- Active Turn/Run 可分别建立有界 partial index；
- 旧 `idx_session_input__claim` 在 Contract 阶段删除。

Scanner 只能发现候选 Session；Claim 事务必须重新锁定 Session Lease 和最小非终态 Head。不能把 pending/retry 候选直接当成可执行 Head，否则会越过 retry_wait/quarantined 或 Lease 已持有时的 Head。

## 7. Forward-only Migration 阶段

### A. Preflight

- Processor/Scanner 保持 disabled；
- 输出不含正文/密文/DSN 的分类 COUNT 与反连接审计；
- 确认 eligible pending、unsupported active、archived pending、孤儿引用和序号缺口；
- 任一未知活跃状态默认阻断，而不是自动修复。

### B. Expand

- 新列先 nullable；
- Header 増加 `receipt_kind/origin_command_id/recorded_at`；
- 创建 Enqueue Result、Turn、Run 和候选索引；
- 暂时保留 Ensure 专属列现有 DEFAULT/NOT NULL，Enqueue 仍 disabled，避免旧 Ensure Writer 中断；
- Expand 阶段只能添加显式允许 legacy NULL 的过渡 CHECK，或暂不添加最终字段组 CHECK；`NOT VALID` 仍会校验新增行，不能用它绕过旧 Ensure Writer 对新列写 NULL 的兼容窗口。最终 exact-group CHECK 只能在旧实例排空、二次回填完成后于 Contract 阶段启用；此时仍不能开放生产 Enqueue/Processor。

### C. 部署兼容 Writer 与应用辅助回填

- 先部署会为 Ensure Header 显式写 `primary/origin/recorded_at` 的兼容应用；
- 排空所有旧实例后，再次回填部署窗口内新增的 NULL Header 并证明数量为零；
- 分批锁定 eligible pending；
- 回填 Source/Authority/Content/Version；
- 生成 UUIDv7 Turn；
- 写可重复运行的进度/审计证据；
- 不创建 Run，不处理 upgrade-block 行。

### D. Verify

- 全量校验 anti-join、UUIDv7、Digest、Source、Counter、Event、Turn 一致性；
- eligible 非 projection pending 恰好一个 Turn且零 Run；
- 整个 upgrade-block exact-set 必须为空，或每一行都已凭权威 Evidence 完成人工迁移；claimed/running/retry_wait/resolved/dead、archived pending、异常 Lease 和孤儿引用都不能被“active=0”窄化后遗漏。

### E. Contract

- 只有兼容 Writer 全量生效且 NULL Header 为零后，才移除 Ensure 专属 DEFAULT/NOT NULL、设置新 Header 列 NOT NULL；
- 设置 NOT NULL，验证条件 CHECK；
- 收紧 source/class/authority/status 组合；
- 旧 `lease_owner/fence_token` 原位重命名为 `claim_owner/claim_fence` 并只作 provenance；eligible/人工迁移完成后 `lease_until` 必须全空并 DROP；
- Repository/Schema Contract 明确断言 Claim/Heartbeat/Takeover 从不读取 Input `lease_until`，唯一 TTL 只来自 `session_runtime_lease`；
- 替换 HOL/scan index；
- 同批更新 Entity、GORM Model/Mapper、Repository、`requiredAgentTables` 和 schema row invariant readiness。

### F. Runtime-disabled 验证与启用

- 从 Migration 005 的真实旧数据执行 Up；
- 通过第 8 节 PG contract/race/crash 证据；
- 跨角色批准后才 enable Ingress，再单独 enable Processor/Scanner。

Down Migration 只在不存在 Enqueue Header/alias/Result/Turn/Run/新状态时允许移除新增结构；存在任一新事实必须在首段显式拒绝。禁止 DROP 后静默丢失已提交事实。

## 8. 真实 PostgreSQL 测试矩阵

### 8.1 Migration/Schema

- `PG-M01`：从 W0/Ensure V1/V2 数据 Up，语义不漂移；无新数据 Down 可回退，有新事实 Down fail-safe；
- `PG-M02`：锁定全局 Header PK、字段组 CHECK、Source/Message/Input/Turn/Event 唯一约束和 partial index；
- `PG-M03`：Ensure V1/V2/Enqueue 相同 CommandID 只能一个全局 Winner。

### 8.2 并发

- `PG-C01`：使用 empty-prompt、三个 Counter 均为 0 的 Session；100 个不同 Source/Command 全部成功，Message/Input/Turn/Event/Receipt +100，三个序号各为无洞 `1..100`，Run=0。非零基线复用例必须断言 `(base, base+100]`；
- `PG-C02`：两个 Session 各 50，锁住 A 的 Counter 时 B 仍可提交；
- `PG-C03`：两个独立 Client/连接池各 50，证明不依赖进程内 Mutex；
- `PG-C04`：同 Source 100 个不同 CommandID 同义，1 created + 99 source_replayed，1 份业务事实 +100 Header；
- `PG-C05`：同 Source 异 digest/class/authority/content，只有 Winner，Loser Query not_found；
- `PG-C06`：同 CommandID 异 digest/Session/type，只能一个 Winner，冲突不泄漏。

并发测试不向外传 exact Session Version；Repository 锁行后读取 observed version 并执行内部 CAS。

### 8.3 Fault/Crash

- `PG-F01`：Sequence Counter 更新失败；
- `PG-F02`：Event Counter 更新失败；
- `PG-F03`：Message 插入失败；
- `PG-F04`：Input 插入失败；
- `PG-F05`：Turn 插入失败；
- `PG-F06`：Event 插入失败；
- `PG-F07`：Session Version CAS 失败或 `RowsAffected!=1`；
- `PG-F08`：Enqueue Result 插入失败；
- `PG-F09`：primary Header 插入失败；
- `PG-F10`：deferred COMMIT 失败；
- `PG-F01..F10` 每一项都必须断言所有表与三个 Counter 完整回滚，重试后序号无空洞；
- `PG-F11`：由 sentinel UUID/application_name 限定的测试 Trigger 卡住，再从第二连接 `pg_terminate_backend`，证明真实 backend crash 回滚；
- `PG-F12`：alias Header insert 和 alias COMMIT 作为两个必跑 subcase；失败均不影响 origin，Query alias not_found，重试只新增 alias。

测试故障注入只能安装在独立 `_test` 数据库临时 Trigger/Deferred Constraint 中；生产代码不得包含 failpoint。

### 8.4 Unknown Outcome

- `PG-U01`：Commit 成功但调用方丢弃响应，第二 Client Query completed；
- `PG-U02`：Commit 边界断链后 Query completed 时，同命令 replay 返回原结果且所有计数零增量；Query not_found 时，同命令 retry 只创建一次且断链事务没有部分事实；
- `PG-U03`：alias 响应丢失可 Query；错 scope/digest/type 不泄漏结果。

Canonical 入口复用 `scripts/check-database-contracts.sh agent`。当前三 Module required-mode 已解析 `go test -json`，逐项要求目标 Test 出现 top-level `pass` 且任一 Test/Subtest 均无 `skip`；目标重命名、`no tests to run` 和普通环境门禁 Skip 都会失败关闭。第 8 节新增真实 PG Test 后仍须纳入 PostgreSQL 16.4 CI，并发门禁执行 `-race -count=3`；当前尚无这些用例或真实 DSN Evidence。无 DSN 的普通 `go test ./...` 可沿用显式环境门禁 skip，不能把本机缺数据库误报为通过证据。

## 9. Evidence 与冒烟边界

Repository 证据候选：`w2.session-ingress.repository-precondition.evidence.v1`，至少记录：

- Git SHA、Go/PostgreSQL/Migration 版本；
- exact case IDs、100 并发与双连接池计数；
- 每个 crash point 的 before/after 事实摘要；
- Query 结论和零泄漏断言；
- `hol_execution_tested=false`；
- `scanner_tested=false`；
- `redis_tested=false`；
- `http_response_loss_tested=false`；
- `adapter_tested=false`；
- `drain_tested=false`；
- `browser_tested=false`；
- `smk_passed=false`。

Repository 测试只证明持久化顺序和事务原子性，不证明 Adapter 调用次数、HOL 执行、Redis lost-wake、Scanner `P95<=30s`、Drain、SSE/浏览器重连或 `SMK-017/020`。

## 10. 生产实现前仍需关闭的 P0

1. Turn 完整执行上下文：Prompt/Message cutoff、Skill Snapshot、Tool Registry、Runtime Policy、Budget/Approval 引用与摘要；同时补齐 cutoff/version 不可变绑定和系统 Turn cutoff 篡改拒绝测试；
2. legacy-only `ensure_project_session_receipt` Authority 类型的安全审核，或不可变 Authority Snapshot 替代方案；
3. unsupported legacy/archived pending 的运维隔离、处置和 Runtime Readiness 协议；
4. Header/Result DDL、字段组 CHECK、alias 时间和 Query 错误优先级的 Agent/安全/运维/数据联合评审；
5. Claim/Heartbeat/Takeover/Terminal SQL 的锁顺序、DB time、Fence/CAS、cancel-specific no-Run 例外与 RowsAffected 规范；
6. Run Effect State、权威 Receipt/Marker/Checkpoint 和 quarantine reconciliation 物理引用；
7. forward Migration、应用辅助回填命令、fail-safe Down 和真实 PG 升级演练；
8. durable pending/strict HOL Scanner 的有界发现结构、Query 计划和 Claim 二次校验；
9. 第 8 节全部 PG contract/race/crash/unknown-outcome 证据。

关闭这些 P0 前，下一步只能继续设计和测试基础设施，不得直接落生产 Runtime。
