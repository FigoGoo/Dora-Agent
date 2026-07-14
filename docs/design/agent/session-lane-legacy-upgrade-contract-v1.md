# Session Lane legacy Authority 与升级分类可执行契约 v1

> 文档状态：Agent-owned Executable Draft / W2-R02，未 Approved
>
> 设计日期：2026-07-15
>
> 实现边界：本文、Go `_test.go` 与 JSON Corpus 只冻结候选语义，不提供生产 Migration、Repository、升级 Helper、Turn/Run、Processor、Scanner 或 Readiness 接线。

## 1. 目的与当前事实

本文补齐 [`session-lane-postgresql-design-v1.md`](./session-lane-postgresql-design-v1.md) 中 legacy 升级的三个 P0：

1. 旧 Ensure V1/V2 Receipt 只能证明派生 provenance，不能伪装历史 `authenticated_user`；
2. 升级必须从 Session 聚合根做 anti-join，并返回稳定、有序、完整的 blocker exact-set；
3. Foundation Ready、Lane Capability Ready、Processor Ready 和 Claim Allowed 必须分层。

截至 2026-07-15，生产事实仍只有 Migration 001～005 的 Foundation 表与 Ensure V1/V2 路径：

- `session/session_message/session_input/session_command_receipt/session_event_log` 等现有表已落地；
- 005 只给 Skill Snapshot Header/Item 增加不可变触发器，Receipt、Message、Input、Event 仍不是数据库级不可变事实；
- 没有 Authority Attestation、升级 Ledger/Helper、Turn/Run/Turn Context、Lane Capability Readiness、Processor、Scanner 或 Runner；
- `postgres.VerifySchema` 和全局 `/readyz` 仍只覆盖 Foundation；
- 当前生产 `session_input` 仍是 W0 字段，不能直接承载本文候选回填结果。

因此 legacy 升级能力本身最多标记为 `MODEL CONTRACT READY` 候选；真实 Migration 005 三类输入只达到独立的 `REAL PG PRECONDITION READY`。两者都不得标记 `UPGRADE READY`、`RUNTIME READY`，也不得宣称 `SMK-017/020` 已通过。

## 2. Corpus 与固定规模

可执行资产：

- `agent/tests/contract/session_lane_legacy_upgrade_v1_corpus_test.go`
- `agent/tests/contract/testdata/w2_r02_upgrade/legacy_authority_attestation_v1.json`
- `agent/tests/contract/testdata/w2_r02_upgrade/session_lane_upgrade_blocker_v1.json`
- `agent/tests/contract/testdata/w2_r02_upgrade/manifest.json`

Manifest 固定 107 条向量：

| 分组 | 数量 | 目的 |
|---|---:|---|
| Authority Attestation | 17 | canonical、摘要域、派生证据等级、Receipt 不可变、chat-only 能力和篡改拒绝 |
| Upgrade Blocker | 90 | preflight/verify、Input/Session root/global orphan、V1/V2/empty prompt、内容可读状态、Event 高低水位和回填后验证 |

Manifest 对文件 raw SHA-256、向量数、总数和十四个目标测试做 exact-set 校验。新增文件、改名、漏向量、摘要未更新或测试重命名都必须失败。

## 3. legacy Authority Attestation

### 3.1 证据等级

内部 Authority 类型固定：

```text
authority_kind = legacy_ensure_receipt_attestation
evidence_level = derived_provenance_only
execution_class = chat
capability_policy = legacy_chat_only
```

它只允许 legacy chat 回填，不进入公开 `enqueue_input_v1` 的 `authority_ref_types` exact-set，不证明历史请求当时完成了认证，也不能授权审批、扣费、外部写入或其他敏感 Tool。敏感动作必须取得当前、独立且可验证的授权事实。

### 3.2 Canonical 字段与摘要

字段顺序固定：

```text
schema_version
authority_kind
evidence_level
source_contract_version
source_command_id
source_command_type
source_request_digest
session_id
project_id
owner_user_id
message_id
input_id
content_digest
result_version
skill_snapshot_schema_version
skill_snapshot_kind
skill_snapshot_digest
skill_count
receipt_completed_at_unix_us
execution_class
capability_policy
```

摘要算法：

```text
lowerhex(SHA256(
  UTF8("dora.legacy_ensure_receipt_attestation.v1") || 0x00 || compact_canonical_json
))
```

`attestation_id/migration_run_id/attempt/attested_at`、Trace、Claim Owner、密文和明文不进入摘要。应用在 `prepared` Ledger 中冻结 UUIDv7 Attestation ID；重试复用该 ID，但 ID 本身不改变 Authority 语义摘要。

`receipt_completed_at_unix_us` 精确保留 PostgreSQL 微秒，不使用浮点数。Receipt `result_version` 必须与命令版本逐值一致：V1 为 `1`、V2 为 `2`。V1/V2、空 Skill 和 published Skill 各有一个固定 golden；任何 canonical 字段变化都必须改变 digest。

### 3.3 Receipt 不可变前置

仅凭当前 `session_command_receipt` 行不能生成长期可信 Attestation，因为当前 Schema 允许 UPDATE/DELETE。自动升级前必须先有经审核的数据库级不可变保护，并由真实 PostgreSQL 测试证明：

- Receipt Header/Result 更新和删除被拒绝；
- 旧 V1/V2 回执与 Message/Input/Session/Skill Snapshot 逐值绑定；
- 应用直接复用生产 `session.CalculateRequestDigest` / `skill.CanonicalEnsureProjectSessionV2` 重算 Ensure canonical request digest，由生产函数统一执行 UTF-8、NFC、Unicode blank、边界空格和 64 KiB 限制；
- 缺失、重复、冲突或不可变保护缺失返回稳定 blocker，Helper 零写入。

## 4. 两阶段升级分类

### 4.1 Preflight

Preflight 只判断现有 legacy 来源事实是否可规划，不要求尚未创建的 Attestation/Turn 已存在。输入必须同时满足：

- Session active 且版本属于已审核 legacy 集合；
- Input 为 `pending/attempts=0`，Input 自有 lease 为空、fence=0；
- Message、Ensure V1/V2 Receipt、Skill Snapshot、Sequence/Event Counter、`session.input.accepted` 与空 Runtime Lease 全部一致；
- Message 与 Skill Runtime Content 已由应用使用 active/previous Keyring 解密并重验摘要；
- Receipt 已受数据库级不可变保护；
- 没有 orphan、已有 Turn、已有 Run 或冲突 Ledger。

只有 `scope=full` 的无 blocker 结果可以返回 `eligible`，代表可以生成升级计划；组件 scope 的无 blocker 结果只能是 `diagnostic_clear`。行级结果不包含 Runtime Ready，`row_verified=false`。

### 4.2 Verify

Verify 在事务提交后重新做全量 anti-join，并额外要求：

- 恰好一个 `derived_provenance_only` Attestation 且 canonical digest 重算一致；
- 恰好一个稳定 UUIDv7 Turn，`chat/created/version=1`；
- `context_message_seq` 来自锁定的 `session_sequence_counter.last_message_seq`，不得使用 `enqueue_seq`；
- 该自动升级 cohort 的 Run anti-join 为零；
- Ledger、Input patch、Authority、Turn 与未来 Turn Context 逐值一致。

当前 Corpus 只冻结 Attestation/Turn/Run/Ledger 的最小计数和冲突语义。不可变 Turn Context 的完整 Schema 仍是下一批 P0；在其 Approved 前不得创建生产 Turn Migration 或 Helper。

### 4.3 Empty prompt

Ensure 空 Prompt 合法地没有 Message/Input/Attestation/Turn/Run/升级 Ledger，但仍必须从 Session root 验证：

- Ensure Receipt；
- Skill Snapshot Header/Item；
- Sequence/Event Counter 与 `session.created`；
- pristine Runtime Lease；
- 全局 orphan anti-join。

分类器不得从 `session_input` 起表，否则既会漏掉合法空 Prompt Session，也会漏掉孤儿 Message/Input/Event/Receipt。

## 5. Blocker exact-set

72 个 reason code 的唯一顺序由 Corpus `exact_sets.reason_codes` 冻结。分类器返回完整有序数组，不只返回 primary error，也不依赖 SQL planner、Go map 或字典序。

范围分为：

- `input_row`：Input、Message、Receipt、accepted Event、内容可读性和它们依赖的 Session Foundation；
- `session_rooted`：空 Prompt、Session 必备行、Receipt target 与 unclaimed rows；
- `global_orphan`：现有 Receipt/Message/Input/Event/Snapshot Header/Item/Sequence Counter/Event Counter/Runtime Lease，以及未来 Authority/Ledger/Turn/Run/Enqueue Result 指向不存在 Session；
- `full`：合并全部诊断；只有该 scope 能返回 `eligible/legal_no_input/verified`，其他 scope 无 blocker 时只返回 `diagnostic_clear`。

任何 blocker 都必须满足：

- 不创建或覆盖 Attestation/Turn/Run；
- 不重置 Input status/attempt/fence/lease；
- 不补猜 Receipt/Event/Counter；
- 不解密到日志或 Evidence；
- 不启用 Scanner/Processor，不发 Redis Wake，不执行 Claim。

## 6. 内容验证边界

Corpus 只冻结 `verified_active_key/verified_previous_key/unverified/unreadable_unknown_key/unreadable_auth_or_digest` 的分类结果，不把抽象状态冒充真实解密证据。

生产 Helper 必须调用正式 Message 与 Skill Snapshot Keyring：

- active/previous 明确 KeyVersion 可读且摘要一致才通过；
- unknown/revoked key、Envelope、Tag、Digest、Skill AAD 任一失败都进入 blocker；
- 不遍历试用其他 key，不顺手重加密历史密文；
- 一项 Skill 失败即整组失败，禁止返回部分明文；
- 日志、Ledger、Evidence 只存安全 ID、count、reason 和 digest。

## 7. Ledger、崩溃与重放

候选状态机：

```text
absent -> blocked
absent -> prepared -> applied -> verified
```

- `prepared` 冻结 facts/plan/Authority/Turn/Turn Context digest、Attestation ID、Turn ID 和 Message cutoff；
- Input patch、Authority insert、Turn/Context insert 与 `applied` 必须在同一 PostgreSQL 事务；
- Input patch 后或 Turn insert 后崩溃必须整体回滚，只保留先前已提交的 `prepared`；
- apply commit 响应丢失后查询原 Ledger/目标事实，同摘要重放零增量；
- 同 ledger key 异 plan/facts/target digest 返回 conflict，原事实不变；
- `blocked/verified` 终态，修复 blocker 后使用新 upgrade generation，不覆盖旧审计事实；
- Helper 永不创建 Run。

测试专用模型已固定事务内 crash、commit response lost、稳定身份、同义重放和异义冲突；这不替代真实 PostgreSQL 锁、唯一约束、并发和进程终止测试。

## 8. Readiness 分层

```text
FoundationReady
  -> LaneCapabilityReady
       -> ProcessorReady
            -> ClaimAllowed(generation match)
```

`FoundationReady` 保持现有服务含义。Legacy blocker、Helper 未完成或 Processor 关闭不得使现有 Ensure/Workspace `/readyz` 失败。

`LaneCapabilityReady=true` 必须同时满足：

- Foundation Ready；
- Lane Schema exact；
- 兼容 Writer 已部署且旧 Writer 已排空；
- 分类完成、blocker=0、eligible 未验证数=0、active helper claim=0；
- Activation Policy/Turn Context 依赖可解析；
- 真实 PostgreSQL upgrade/crash 证据审核通过；
- capability state=`ready` 且 generation>0。

`ProcessorReady` 还要求显式启用 Processor；`ClaimAllowed` 再要求 Claim 事务读取的 generation 与当前 generation 精确相等。当前仓库即使 Foundation Ready，也固定属于 Lane Capability Not Ready。

## 9. Down guard

Down 只允许 Expand-only pristine 状态。部署层必须先取得全局 Migration Fence 并摘流，确认 compatible Writer、Processor、Scanner 全停；SQL guard 不能用一次空表查询替代停写协议。任一以下事实存在都必须在任何 DROP/DELETE/ALTER 前拒绝：

- Enqueue Header、alias Receipt、origin Result；
- Authority Snapshot；
- legacy Turn、任意 Run；
- 新状态/expanded field/Turn Context 业务事实；
- 任意 upgrade Ledger，包括 terminal blocked/verified 审计行；
- 全局 Migration Fence 未持有，或 compatible Writer/Processor/Scanner 任一仍运行。

纯模型只冻结 guard truth table，不证明部署进程已停止，也不证明 golang-migrate metadata。当前 `scripts/migrate.sh` 直接调用 golang-migrate；Down SQL 抛错后的 dirty/version 结果、修复流程与可重试性必须用真实 CLI/数据库测试冻结。已经运行 Helper 并产生 Ledger/Authority/Turn/Context 的环境不能使用“无损 Down”描述，除非另有独立、Approved 的审计保留与精确逆迁移。

## 10. Event Retention P0

`session.created/session.input.accepted` 当前都位于带 `min_available_seq` 在线保留水位的 EventLog。若升级所需的 created/accepted Event 已被裁剪，Helper 不能猜测或补造历史事件；在线区间只要求 `[min_available_seq,last_seq]` 连续，水位前缀裁剪本身不是中间洞。

评审候选已推荐 [`session-event-foundation-marker-v1.md`](./session-event-foundation-marker-v1.md) 的独立、不可裁剪 Marker，EventLog 继续只承担在线投影与 Retention；该候选仍未 Approved/实现。在 Marker 覆盖完成前 Retention 必须关闭，created Event 被裁剪按 `SESSION_CREATED_EVENT_MISSING`、accepted Event 被裁剪按 `ACCEPTED_EVENT_MISSING` fail-closed；纯模型显式携带 `min_available_seq/last_seq`，但不替代真实 Marker/Retention SQL 证据。

## 11. 证据分层与下一步

| 层级 | 本批能证明 | 仍不能证明 |
|---|---|---|
| 纯模型 | 107 向量、canonical/digest、reason exact-set/order、rooted anti-join、Ledger crash 语义、Readiness/Down truth table | SQL、锁、真实密文、进程、Migration metadata 和部署 |
| 真实 PostgreSQL | `REAL PG PRECONDITION READY`：真实 005 已创建并逐值核对 V1/V2/empty 三类 cohort；待实现 forward Up、Helper/Verify、anti-join、唯一约束、批锁、crash/resume、Down 原子拒绝 | 生产 Processor/Scanner/Redis/Runner |
| 生产 Runtime | 待实现：Keyring/AAD、Readiness/generation gate、旧 Writer drain、零早启 Claim/Wake、脱敏 Evidence | 全功能业务链 |
| 全功能 Smoke | 待实现：真实 Session Lane/Provider unknown outcome/投影重试 | 完整回归和发布门禁 |

下一批顺序：

1. 评审并冻结 [`immutable Turn Context`](./immutable-turn-context-design-v1.md)、Activation Policy 与 Message-set digest；
2. 审核 [`session-event-foundation-marker-v1.md`](./session-event-foundation-marker-v1.md) 的独立 Marker/Retention 候选；
3. 评审 Receipt/Message/Event 不可变 DDL 和升级 Ledger 物理设计；
4. 才允许编写 forward-only Migration、Repository、Helper 与真实 PostgreSQL crash matrix；
5. 真实 PG 证据 Approved 后再接 Lane Capability Readiness、Processor/Scanner、Redis Wake 和 Eino Runner。
