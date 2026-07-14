# Session Event Foundation 独立 Marker 设计 v1

> 文档状态：Agent-owned Review Ready 候选 / W2-R02 Event Marker，未 Approved
>
> 设计日期：2026-07-15
>
> Owner：Agent Service
>
> 适用范围：`session.created`、`session.input.accepted` 的永久证明、EventLog 在线保留、legacy Session Lane 升级前置条件
>
> 实现门禁：本文只收敛候选设计，不创建 Migration、GORM Model、Repository、Helper、Retention Job 或 Runtime 接线。跨 Agent、数据、安全、运维与测试评审 Approved 前，不得据此启用 Retention、legacy Helper、Processor、Scanner、Claim、Redis Wake 或 Run。

## 1. 结论

推荐新增独立、不可裁剪的 `agent.session_event_marker`，永久冻结 `session.created/session.input.accepted` 的最小证明；现有 `agent.session_event_log` 继续只承担 Workspace/SSE 在线投影和 Cursor Reset，可以按已审核保留窗口裁剪。

Marker 与 EventLog 的关系是“同一已提交业务事实的不同生命周期投影”，不是两套业务结果：

- EventLog 保存前端安全 Payload，按 Session 单调 Seq 提供在线重放；
- Marker 保存不含正文的强类型身份、Payload Digest 和 Semantic Digest，永久用于 Receipt 重放、legacy 升级和审计；
- 新 Writer 必须在同一 Agent PostgreSQL 事务中同时写 Event、Marker、领域事实和 Receipt；
- legacy Helper 只允许从仍可验证的 Event 与现有权威行派生 Marker，缺失或已裁剪时失败关闭；
- Marker 不替代 Receipt、Authority Attestation、Turn Context、Turn、Run、Checkpoint 或 EventLog。

本文达到 Agent-owned Review Ready 候选，只表示字段、摘要、事务、回填、Retention、Down 和测试边界足以进入跨角色评审；它仍未 Approved，也不关闭 [`session-lane-postgresql-design-v1.md`](./session-lane-postgresql-design-v1.md) 的其他 P0。

## 2. 当前事实与非目标

### 2.1 当前仓库事实

- Migration 004 已创建 `session_event_counter(last_seq,min_available_seq)` 与 `session_event_log`；当前 CHECK 允许 `min_available_seq=last_seq+1`。
- `SessionRepository.Ensure` 只在创建 Session 时原子写一个 `session.created`，非空 Prompt 再写一个 `session.input.accepted`；尚无通用 Append Repository。
- `WorkspaceRepository` 只以 `REPEATABLE READ` 读取 Snapshot、Counter 和 `seq>cursor` 的 Event 批次；尚无生产 Retention Repository 或 Job。
- W0.5 Smoke 的保留窗口注入器会在测试库删除 seq 1/2 并把水位推进到 3；这证明 EventLog 事件可能合法退出在线窗口。
- EventLog 当前没有 UPDATE/DELETE 不可变触发器，不能被直接当作永久 Authority。
- [`session-lane-ingress-command-contract-v1.md`](./session-lane-ingress-command-contract-v1.md) 已在 test-only Corpus 中把 `session.input.accepted` Marker 绑定 Input、Event Seq 与 Receipt，但没有生产表或 Writer。

### 2.2 非目标

本文不设计或批准：

- legacy Ensure Receipt 的数据库级不可变保护和 Authority Attestation；
- Turn Context、Turn、Run、升级 Ledger 或完整 Session Lane Migration；
- Event Archive/Cold Storage、管理员删除、用户数据生命周期或合规擦除流程；
- 新 Event Type、A2UI Payload、Redis Wake 或生产 Scanner；
- 由 SQL 猜测 Payload、生成 UUIDv7 或重建已裁剪历史。

## 3. 为什么不永久保留 EventLog

“让 `session.created/session.input.accepted` 永不删除”表面上少一张表，实际会把在线投影和永久审计耦合：

| 维度 | 永久保留 EventLog 关键行 | 独立 Marker |
|---|---|---|
| 生命周期 | 水位以下仍残留特殊行，EventLog 同时具有在线与永久两种语义 | EventLog 只解释在线窗口；Marker 永久 |
| 不可变 | 还需额外阻止 UPDATE，并为 Retention DELETE 开特殊通道 | Marker 统一拒绝 UPDATE/DELETE；EventLog 可按策略删除 |
| 存储 | 每个 Input 永久保留 JSONB Payload 与投影字段 | 每个 Input 只保留紧凑身份和 Digest |
| SSE/Reset | Retention 必须跳过 anchor，测试与查询需理解水位以下残留行 | 现有 `min_available_seq` 和 Reset 语义不变 |
| legacy 回填 | Event 存在不等于已受永久不可变保护 | Helper 验证后一次冻结 Marker，之后不依赖 Event 生命周期 |
| 未来 Ingress | Receipt 仍缺独立 Marker 引用 | 与 test-only `marker_id` 候选直接对齐 |

因此独立 Marker 是推荐方向。为了在 Marker 覆盖完成前防止证据继续丢失，生产 Retention 必须保持 disabled；未来 Retention Repository 也必须在删除关键 Event 前验证 exact Marker 已存在。这个临时门禁不等于把 EventLog 永久保留。

## 4. 表与不变量

### 4.1 候选表

候选表名固定为 `agent.session_event_marker`。不建立物理外键或数据库级联；所有 UUID 由应用生成并在应用/Helper 中验证为 canonical UUIDv7。

| 字段 | 类型 | 约束与语义 |
|---|---|---|
| `marker_id` | `uuid` | 主键；复用对应 Event 的 `event_id`，不再分配第二身份；Event 被裁剪后仍稳定 |
| `session_id` | `uuid` | Session 逻辑引用，不设置物理外键 |
| `marker_type` | `varchar(64)` | v1 仅允许 `session.created/session.input.accepted` |
| `schema_version` | `varchar(64)` | 固定 `session_event_marker.v1` |
| `event_schema_version` | `varchar(32)` | v1 固定 `session.event.v1` |
| `source_kind` | `varchar(64)` | v1 候选仅允许 `ensure_project_session/enqueue_input_v1`；后者仍受 Ingress 未 Approved 门禁 |
| `source_id` | `uuid` | 来源稳定幂等标识 |
| `projection_index` | `integer` | 同 Source 的固定投影位置，必须非负 |
| `aggregate_type` | `varchar(32)` | `session.created -> session`；`session.input.accepted -> session_input` |
| `aggregate_id` | `uuid` | Session 或 Input 逻辑引用 |
| `aggregate_version` | `bigint` | Marker 观察到的冻结聚合版本，必须大于 0 |
| `event_seq` | `bigint` | 原 Event 在 Session 内的 Seq，必须大于 0 |
| `payload_digest` | `char(64)` | 强类型 Event Payload canonical JSON 的 SHA-256 小写十六进制摘要 |
| `semantic_digest` | `char(64)` | 第 5.2 节域分离输入 `UTF8("dora.session_event_marker.v1") || 0x00 || canonical_json` 的 SHA-256 小写十六进制摘要 |
| `occurred_at_unix_us` | `bigint` | 原 Event `created_at` 转换的正 Unix 微秒整数，避免时区/文本格式漂移 |
| `recorded_at` | `timestamptz` | Marker 实际写入 PostgreSQL 的 UTC 时间；属于运维元数据，不进入 Semantic Digest |

### 4.2 CHECK 与唯一约束

候选 Migration 必须至少冻结：

- `PRIMARY KEY(marker_id)`；
- `UNIQUE(session_id,event_seq)`，同一 Event Seq 只能对应一个 Marker；
- `UNIQUE(session_id,source_kind,source_id,marker_type,projection_index)`，冻结 AppendOnce 来源；
- partial unique：每个 Session 恰好至多一个 `session.created` Marker；
- partial unique：每个 Input `aggregate_id` 恰好至多一个 `session.input.accepted` Marker；
- `schema_version/event_schema_version/marker_type/source_kind/aggregate_type` exact-set CHECK；
- `projection_index>=0`、`aggregate_version>0`、`event_seq>0`、`occurred_at_unix_us>0`；
- `payload_digest/semantic_digest` 必须匹配 `^[0-9a-f]{64}$`；
- 类型组合 CHECK：created 仅允许 `ensure_project_session + projection_index=0 + aggregate_type=session + aggregate_id=session_id`；accepted 仅允许 `ensure_project_session + projection_index=1` 或 `enqueue_input_v1 + projection_index=0`，且 `aggregate_type=session_input`；
- 表与全部字段具有中文 COMMENT，明确逻辑引用、摘要、时间单位和不可裁剪语义；
- 不含 `FOREIGN KEY`、`REFERENCES`、CASCADE 或 `tenant_id`。

Marker 是 append-only 审计事实。数据库必须使用 `BEFORE UPDATE OR DELETE` 触发器统一拒绝修改和删除，包括无变化 UPDATE；生产 Repository 只允许 INSERT/SELECT。

### 4.3 Counter 候选收紧

现有 CHECK 允许 `min_available_seq=last_seq+1`，可能在 Snapshot 从 `last_seq` 重连时形成永久 Reset 循环。独立 Marker 不改变在线窗口仍需至少保留一条 Event 的要求。

候选前向 Migration 应先 fail-closed 检查每个 Counter 都满足 `last_seq>=1` 且在线区间无中间洞，再将边界收紧为：

```text
1 <= min_available_seq <= last_seq
```

若真实历史存在 `last_seq=0`、`min_available_seq>last_seq` 或 `[min_available_seq,last_seq]` 不连续，Migration 必须拒绝并输出安全计数，不能自动补造 Event。

## 5. Semantic Digest

### 5.1 Payload Digest

应用先按 `event_schema_version + marker_type` 选择现有强类型 Payload DTO：

- `session.created` 使用 `event.SessionCreatedPayload`；
- `session.input.accepted` 使用 `event.SessionInputAcceptedPayload`。

必须严格拒绝未知字段、未知版本、未知类型、超长 Payload、身份/聚合不一致和 trailing JSON。通过后以稳定字段顺序重新编码 canonical JSON，再计算 `payload_digest=SHA-256(canonical_payload)`。禁止直接摘要 PostgreSQL `jsonb::text`、原始网络字节或任意 Map。

### 5.2 Marker Canonical DTO

`semantic_digest` 的输入固定为以下有序、无可选字段 DTO：

```text
schema_version
marker_id
session_id
marker_type
event_schema_version
source_kind
source_id
projection_index
aggregate_type
aggregate_id
aggregate_version
event_seq
payload_digest
occurred_at_unix_us
```

编码规则固定为 UTF-8、字段顺序如上、JSON 字符串使用标准转义、整数使用十进制、无浮点、无 Map、无额外空白。Marker Semantic Digest 必须使用固定域分离，不能直接对 canonical JSON 计算裸 SHA-256：

```text
semantic_digest = lowercase_hex(
  SHA256(
    UTF8("dora.session_event_marker.v1")
    || 0x00
    || canonical_json
  )
)
```

域字符串和单字节 `0x00` 分隔符都属于摘要输入，任何实现不得省略、改写大小写、追加终止符或替换为文本 `"0x00"`。该域分离避免相同 canonical JSON 字节在 Receipt、Authority、Event Payload 或其他摘要语义域中产生可互换 Digest。`recorded_at`、Helper run/attempt、Claim owner、Trace 和日志元数据不进入 canonical JSON，因此同一 Event 的首次写入、Helper 回填和技术重试得到相同 Semantic Digest。

Payload Digest 仍按第 5.1 节对强类型 canonical Payload 计算 SHA-256；只有 Marker `semantic_digest` 使用上述 `dora.session_event_marker.v1` 域分离。应用必须先校验固定长度小写十六进制，再使用 constant-time compare 比较 Digest；日志和 Evidence 只允许记录安全 ID、类型、计数、状态和 Digest，不记录 Payload、Message 密文或正文。

## 6. Compatible Dual-write

### 6.1 新 Session/Ensure

兼容 Writer 必须在当前 Ensure 短事务中原子完成：

1. Command advisory lock 与 Receipt first-write-wins；
2. Session、Skill Snapshot、Sequence Counter、Runtime Lease；
3. 可选 Message/Input；
4. Event Counter 与批量 Event；
5. 从同一强类型 Event 批量创建 exact Marker；
6. Command Receipt；
7. 一次提交，提交后才允许非权威通知。

Event 与 Marker 必须复用同一 `event_id/marker_id`、Session、Source、Projection、Aggregate、Seq 和发生时间。任一 Event/Marker/Receipt INSERT、唯一约束或摘要核对失败，整个事务回滚，不能消耗 Seq 或留下半个 Receipt。

同 CommandID 重放不新增 Event/Marker。Expand 兼容期内，旧 Receipt 缺 Marker 不能使现有 Foundation `/readyz` 失败或被悄悄伪造；新兼容 Writer 创建的 Receipt 必须始终原子带 Marker。旧 Writer 排空和 legacy 覆盖为零后，Contract 版本才将 Receipt→Marker 完整性提升为强制重放门禁。

### 6.2 未来 Enqueue

未来 `enqueue_input_v1` 仍必须遵守 [`session-lane-ingress-command-contract-v1.md`](./session-lane-ingress-command-contract-v1.md)：Marker 与 Input、可选 Message/Turn、Event/Projection 和 Receipt 同事务，Receipt 冻结 `marker_id`。本文不批准 Enqueue DTO、Turn 或生产 Repository，只冻结 Marker 必须可被该事务复用的物理边界。

## 7. Legacy Helper

### 7.1 Rooted 分类

Helper 必须从 `session_command_receipt + session` 聚合根做有界 LEFT JOIN 分类，不能从 EventLog 起表：

- 每个合法空 Prompt Ensure Receipt 期望一个 created Marker、零 accepted Marker；
- 每个合法非空 Prompt Ensure Receipt 期望一个 created Marker和一个与 Receipt Input 精确绑定的 accepted Marker；
- 同时批量核对 Message/Input、Session/Sequence/Event Counter、Skill Snapshot、Runtime Lease、Event、现有 Marker 和 Receipt；
- global orphan anti-join 覆盖 Marker 指向不存在 Session/Input 的行。

查询使用稳定 keyset 和唯一 tie-breaker，一批内固定数量 SQL/CTE；禁止按行 N+1。

### 7.2 允许回填

只有以下条件全部成立才允许插入 Marker：

- 原 Event 仍物理存在，且 `event_seq` 位于锁定 Counter 的 `[min_available_seq,last_seq]`；
- 在线区间连续，Event 不在高水位之后；
- Event 类型、Schema、Source、Projection、Aggregate、时间和强类型 Payload 全部合法；
- Event 与 Receipt、Session、Input、Message 逐值一致；
- 应用独立重算 Payload Digest 与 Semantic Digest；
- 同 Marker key 不存在，或已存在完全同义 Marker。

同键同 Digest 重放为零增量；同键异 Digest、不同 Marker 抢占同 Event Seq 或同 Input 出现多个 accepted Marker均为稳定 conflict，原事实不变。

### 7.3 Fail-closed

以下情况只产生脱敏 blocker，不插入、不覆盖、不修复 Event：

- created/accepted Event 缺失、已位于 `min_available_seq` 之前、位于 `last_seq` 之后或在线区间有洞；
- Payload 无法严格解码，或 Session/Input/Message/Receipt/Counter 绑定不一致；
- Receipt 不可验证、可选 Message/Input 组合损坏、Session 非预期状态；
- Marker key、Digest、类型或唯一关系冲突；
- 未知 Event/Schema/Source/Content 状态。

Helper 不得从 Session、Input 或 Receipt 猜造已裁剪 Event，不得推进/回退水位，不得重置 Input 状态、Attempt、Lease 或 Fence，也不得创建 Authority、Turn、Run、Wake。缺失 Event 继续映射 [`session-lane-legacy-upgrade-contract-v1.md`](./session-lane-legacy-upgrade-contract-v1.md) 的稳定 blocker。

## 8. 锁顺序与 Retention

### 8.1 全局锁顺序

所有涉及 Marker/Event Seq 的 Writer、Helper 和 Retention 必须遵守统一顺序，只能跳过不需要的前置锁，不能取得后置锁后再回头：

```text
Command advisory lock（如有）
  -> Receipt（如有）
  -> Session
  -> session_sequence_counter（如需 Message/Input Seq）
  -> session_event_counter
  -> session_event_log rows
  -> session_event_marker rows
```

新 Session 首次创建 Counter 时不存在并发 Retention；未来 Append、Helper 和 Retention 对已有 Session 必须锁定 `session_event_counter FOR UPDATE`。多 Session 批次按 canonical Session UUID 升序取得锁，或使用有界 `FOR UPDATE SKIP LOCKED`，不得无序持有多 Session 锁。

### 8.2 Helper 并发

Helper 每批在短事务内重新锁定并复核 Counter/Event/Marker；应用在事务外准备的候选不能直接提交。多 Helper 依赖唯一约束和同义 Digest 幂等，不依赖进程内互斥。批量 INSERT 后必须固定批量读回核对，检查 RowsAffected，异义冲突整体失败。

### 8.3 Retention 事务

生产 Retention 在 Marker rollout 完成前保持 disabled。启用后每个 Session 的一次裁剪必须在一个短事务中：

1. 锁定 `session_event_counter FOR UPDATE` 并读取 observed `last_seq/min_available_seq`；
2. 验证目标 `next_min_available_seq` 单调前进、`<=last_seq`，且当前在线区间无洞；
3. 对待删范围内的 `session.created/session.input.accepted` 验证 exact Marker 的 ID、Source、Aggregate、Seq、Payload Digest 和 Semantic Digest；
4. 任一关键 Event 无 Marker或 Marker 冲突时零删除、零水位推进；
5. 删除 `seq < next_min_available_seq` 的 Event；
6. 以 observed 水位作 CAS 更新 Counter，并检查 RowsAffected=1；
7. 一次提交。

Append 与 Retention 通过同一 Counter 行串行；Workspace 的 `REPEATABLE READ` 读事务只能看到裁剪前或裁剪后的自洽快照。Retention 不删除 Marker，且始终至少保留 `last_seq` 对应的最新 Event。

## 9. Rollout 与 Readiness

### 9.1 阶段

| 阶段 | 动作 | 必须保持关闭 |
|---|---|---|
| R0 Review | 本文跨角色审核 | Migration、Writer、Helper、Retention、Lane Runtime |
| R1 Expand | 新建 Marker 表/不可变触发器，收紧 Counter 候选约束；不回填 | Retention、Helper 写入、Processor/Scanner |
| R2 Compatible Writer | 新 Writer 对新 Ensure 双写；旧 Writer 仍可能产生 legacy gap | Retention、Lane Capability、Processor/Scanner |
| R3 Drain + Backfill | 排空旧 Writer，至少再执行一轮 rooted 分类与 Helper | Retention、Claim/Wake/Run |
| R4 Verify | blocker=0、eligible unmarked=0、orphan=0、真实 PG/race/crash Evidence Approved | Processor/Scanner 仍关闭 |
| R5 Contract | 新写入与 Receipt replay 强制 Marker；按独立 generation 启用 Retention capability | 其他 Session Lane P0 未关闭时仍禁止 Lane Runtime |

### 9.2 Readiness 分层

Marker rollout 不改变现有 Foundation Ready 含义：

- 旧 Writer 与旧 Schema 在 R1/R2 期间仍可服务现有 Ensure/Workspace；
- 新 compatible Writer 实例只有在 Marker Schema exact 且双写可用时才可接收新流量；
- legacy Marker blocker、Helper 未完成或 Retention disabled 不得使现有 Ensure/Workspace `/readyz` 失败；
- `MarkerCapabilityReady=true` 至少要求 Schema exact、兼容 Writer 全量、旧 Writer 已排空、分类完成、blocker=0、未标记 eligible=0、active Helper claim=0、真实 PG Evidence Approved、capability state=`ready` 且 generation>0；
- Retention 每次事务必须读取并匹配当前 Marker generation；stale generation 禁止删除；
- Marker Capability Ready 不等于 Lane Capability Ready。Receipt 不可变、Authority、Turn Context、Ledger、Turn/Run 和其他 PG 门禁未 Approved 时，Processor/Scanner/Claim/Wake/Run 继续为零。

## 10. Down Guard

Down 是部署与 SQL 的共同门禁：

1. 部署层先取得全局 Migration Fence，摘流并确认 compatible Writer、Helper、Retention、Processor、Scanner 全停；
2. Down SQL 第一条持久事实检查必须发生在任何 DROP TRIGGER、DROP TABLE 或 ALTER CONSTRAINT 前；
3. `session_event_marker` 任意行存在即拒绝 Down，包括 legacy backfill、new dual-write 和 terminal 审计事实；
4. 表为空且 Counter 满足旧 Schema 时，才允许删除 Marker 触发器/函数/表并恢复旧 Counter CHECK；
5. 拒绝后 Marker、Event、Counter、Schema 和业务数据必须原样；`golang-migrate` dirty/version 结果与恢复步骤必须由真实 CLI 测试冻结。

一旦 compatible Writer 或 Helper 已提交 Marker，该环境不得宣称本 Migration 可无损 Down。若产品需要删除 Marker或执行合规擦除，必须另有独立、Approved 的审计保留/销毁设计，不得把它伪装成普通 Schema rollback。

## 11. 真实 PostgreSQL 16 测试矩阵

以下测试必须使用真实 PostgreSQL 16；sqlmock、SQLite 和纯 Go Corpus 不能替代：

| ID | 类别 | 必须证明 |
|---|---|---|
| EM-PG-M01 | Up | 空库完整 Up；从真实 Migration 005 的 V1、V2、空 Prompt fixture Up；旧事实不漂移 |
| EM-PG-M02 | Schema | 中文 COMMENT、无物理 FK、exact CHECK、唯一/partial index、不可变 trigger、Counter 边界 |
| EM-PG-M03 | Preflight | `last_seq=0`、`min>last`、在线洞、orphan Counter/Event/Marker 均 fail-closed |
| EM-PG-W01 | Dual-write | 新非空 Ensure 原子产生 2 Event+2 Marker+Receipt；空 Prompt 产生 1 Event+1 Marker、零 accepted |
| EM-PG-W02 | 并发 | 同 Command 100 并发只产生一组稳定 ID/Digest；不同 Command/Source 冲突不产生重复 Marker |
| EM-PG-W03 | 回滚 | Event 后、Marker 批次中、Receipt 前注入失败均回滚 Session/Input/Event/Marker/Counter/Receipt 与 Seq |
| EM-PG-H01 | Backfill | V1/V2/empty legacy Event 完整时 exact 回填；重复运行零增量；双 Helper 无重复、无死锁 |
| EM-PG-H02 | Fail-closed | Event 缺失、`seq<min`、`seq>last`、Payload/Source/Aggregate/Receipt/Message/Input tamper 全部 blocker 且零 Marker |
| EM-PG-H03 | Conflict | 同 Marker ID 异 Digest、同 Input 双 accepted、同 Session 双 created、同 Seq 双 Marker均保留原事实并返回稳定冲突 |
| EM-PG-I01 | Immutability | Marker 无变化 UPDATE、变更 UPDATE 与 DELETE 均由数据库拒绝；原行可读且 Digest 不变 |
| EM-PG-R01 | Retention | 无 exact Marker 时 DELETE 与水位推进均为零；有 Marker 后可裁剪 Event但 Marker保留 |
| EM-PG-R02 | Counter | Append vs Retention、Helper vs Retention、双 Retention 并发无死锁/越界；RowsAffected/CAS 精确；始终保留最后 Event |
| EM-PG-R03 | Workspace | 裁剪前后 Snapshot/EventBatch 只观察自洽水位；过期 Cursor Reset，新高水位重连无永久 Reset 循环 |
| EM-PG-D01 | Down | 空 Marker 表 Down 成功；任意 Marker 在首个 DDL 前拒绝，拒绝后 Schema/数据不变 |
| EM-PG-D02 | CLI | 真实 `scripts/migrate.sh`/golang-migrate 冻结 Down 拒绝后的 dirty/version、修复与重试语义 |
| EM-PG-E01 | Evidence | Evidence 只含安全 ID、count、reason、generation、digest、耗时；不含 Payload、正文、密文、DSN 或 Secret |

测试还必须使用 `GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C agent test ./...` 验证 Agent Module 独立构建，并运行 `-race` 覆盖 Helper/Retention/Writer 的进程内协调代码。

## 12. 候选实现切片与评审门禁

Approved 后的最小实现应保持一个目的一个 Commit，并按以下路径落位；当前不得创建这些生产变更：

- 前向 SQL：`agent/migrations/<next>_create_session_event_marker.up.sql` 与 `.down.sql`；
- 强类型 Marker/摘要：`agent/internal/event/marker.go`；
- Session 创建计划：`agent/internal/session/entity.go`、`service.go`、`service_v2.go`；
- GORM Model/Mapper/事务：`agent/internal/postgres/session_model.go`、`session_mapper.go`、`session_repository.go`；
- Helper 消费接口与编排：`agent/internal/event/repository.go`、`marker_backfill.go`；
- PostgreSQL Helper/Retention：`agent/internal/postgres/session_event_marker_repository.go`；
- Schema readiness：`agent/internal/postgres/client.go`；
- 真实 PG 证据：独立 `session_event_marker_repository_contract_test.go`，并同步现有 Session/Client contract tests。

进入实现前仍需取得：

1. Agent Owner 对字段、Digest、事务和错误语义签字；
2. 数据评审对索引、批次、Explain、存储增长和 Counter 锁签字；
3. 安全评审对不可变、脱敏、Receipt/Authority 边界签字；
4. 运维评审对 rollout、旧 Writer drain、Migration Fence、Retention 与 Down/dirty 恢复签字；
5. 测试评审对真实 PG/race/crash/CLI matrix 签字。

当前审核结论：**独立 Marker 是 Agent-owned Review Ready 候选推荐；仍未 Approved，不得开始 Migration 或生产代码。**
