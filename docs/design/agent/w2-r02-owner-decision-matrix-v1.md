# W2-R02 Owner 决策矩阵 v1

> 状态：Proposed / Awaiting Owner Decision
>
> 评审 Gate：`W2-R02`
>
> 事实基线：`codex/w2-r04-consumption-audit@cf0a680f`
>
> 结论边界：本文为 R02 分散的 Runtime、Ingress、PostgreSQL、legacy、Marker 与 Turn Context 待决项提供稳定 ID、Owner 候选和关闭证据。本文不是 Accepted Decision、不是 Formal Review Freeze，也不授权生产 Migration、Repository、Processor、Scanner、Redis Wake、Runner、Checkpoint、Receipt、Graph、Approval、Billing、A2UI 或 Smoke 实现。

## 1. 目的与状态词汇

R02 当前的多个设计文档只使用“第 N 项 P0”或列表位置引用未决项。列表增删后位置会漂移，机器 manifest 无法稳定绑定 Owner 结论、关闭证据和 reopen 原因。本文把这些问题去重为 `R02-D01`～`R02-D19`，并保留已有 `PG-D01`～`PG-D08`、`TC-P01`～`TC-P10` 的原始身份。

本文所有决策初始状态统一为：

```text
decision_status=awaiting_owner_decision
approval_status=not_requested
implementation_status=prohibited
evidence_status=candidate_only
```

这些值只描述本文评审状态，不是新的机器 Schema。只有语义、Owner exact-set、关闭证据和 ADR disposition 全部固定后，才可准备 `awaiting_owner_approval` 候选；平台 authority 验证同一 head 上全部有效 Review 后，才可能进入 Formal Freeze/Approved。

## 2. 当前机器事实与候选证据

[`w2-review-freeze-manifest.json`](./approvals/w2-review-freeze-manifest.json) 当前原始字节 SHA-256 为：

```text
a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e
```

其中 R02 仍为：

- `status=expansion_frozen`；
- `freeze=null`；
- `candidate_evidence=[]`；
- 当前 `required_owner_roles=[agent_owner,data_owner,operations_owner,security_owner]`，该数组不是最终 exact-set；
- blocker exact-set 为 `W2_R02_AGGREGATE_MANIFEST_MISSING`、`W2_R02_OWNER_APPROVAL_MISSING`。

当前有五个彼此独立的 child manifest：

| 子域 | Manifest | 原始 SHA-256 | Corpus 文件 | 向量 | Published fixture IDs | Target tests |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| Lane state | [`w2_r02/manifest.json`](../../../agent/tests/contract/testdata/w2_r02/manifest.json) | `9a73a22c7e6eeef179a5c46978ad1dfc3b6af54d765fea24890d3302f2ea5f26` | 1 | 60 | 1 | 4 |
| Ingress | [`w2_r02_ingress/manifest.json`](../../../agent/tests/contract/testdata/w2_r02_ingress/manifest.json) | `5b955d31f2f718c49f67fe24891fe154714cc9f88521345409b329c064591938` | 1 | 42 | 1 | 4 |
| Legacy upgrade | [`w2_r02_upgrade/manifest.json`](../../../agent/tests/contract/testdata/w2_r02_upgrade/manifest.json) | `7e67554d2dff826058060cab6ff624a4cc0522808310302cb6402ab589f9b1ce` | 2 | 107 | 0 | 14 |
| Foundation Marker | [`w2_r02_marker/manifest.json`](../../../agent/tests/contract/testdata/w2_r02_marker/manifest.json) | `d557a97f2981c40c944b2c9fafb6afb92f57ac3e0e84b1c2b08479fcb3f65c14` | 1 | 19 | 3 | 9 |
| Message Set / Turn Context | [`w2_r02_turn_context/manifest.json`](../../../agent/tests/contract/testdata/w2_r02_turn_context/manifest.json) | `076ed67cbf22c3032fcf4686de9475e7a9cf968082cbafa98e4d11f5c98eaa13` | 2 | 70 | 8 | 27 |

机械小计是 5 个 child manifest、7 个 Corpus 文件、298 条向量、13 个已发布 fixture ID 和 58 个互不重复的 target-test 引用。它不是 R02 aggregate candidate，原因至少包括：

1. legacy upgrade manifest 没有 `fixture_ids` 或 `vector_ids`，无法形成全 Gate vector exact-set；
2. 五个 manifest 没有统一绑定 design source、validator source、build input、toolchain 与 trust root；
3. `R02-D01`～`R02-D19`、`PG-D01`～`PG-D08`、`TC-P01`～`TC-P10` 均未获得 Owner 结论；
4. R02 最终 Owner exact-set 尚未由批准范围推导；
5. GOV-D01～D06 的 base-owned authority 仍未激活。

因此本文不创建 aggregate manifest，也不向 Review Freeze manifest 写入 candidate evidence。

## 3. Stable Owner 决策

以下“推荐候选”均未被接受。Owner 需要逐项接受、拒绝或给出版本化替代方案，不能以一次笼统的“R02 同意”覆盖所有行。

### 3.1 Command、Ingress 与物理 Receipt

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R02-D01` | 哪些 Runtime command 必须有 durable Receipt、replay 与 Query | 冻结 `enqueue_input_v1`、十个 Lane 控制命令和 `resume_requested_v1` 的版本化 exact-set；全局 CommandID；同 ID/同摘要只读重放，异 type/digest 冲突；响应丢失只按原 ID Query | Agent、Data、Security、Test | 每类 request/result exact-set、跨 type 冲突、first-write-wins、response-lost Query、真实 PG/Handler 测试 |
| `R02-D02` | 失败命令是否持久化 Receipt | 只有已经形成不可变且可安全重放的命令结论才写 terminal failure Receipt；认证/scope 预检失败和完整事务回滚不写成功域 Receipt；需冻结稳定失败 exact-set | Agent、Data、Operations、Security、Test | 失败分类、重复失败、修复后重试、无半 Receipt、无旧结果泄漏、retention/audit 测试 |
| `R02-D03` | Source/Class/Authority/Turn/Resolution/Cancel Registry 由谁分类 | 保留当前 Source/Class 候选；Approval/Batch class 只能由版本化服务端 Policy 和受信 Authority 推导；模型、前端和 payload 不能选择；未在 A1 开放的类型显式 `production_unavailable` | Agent、Business、Product、Security、Test | Registry raw exact-set、全部非法组合、actor/scope、policy drift、payload override 负向测试 |
| `R02-D04` | 全局 Header、origin Result 与 alias 的唯一物理模型 | 保留唯一 `session_command_receipt` Header；每个 primary 一份 typed Enqueue Result；alias 只新增 Header 并指向 primary；禁止平行全局 Receipt 表和任意 JSON Result 逃逸 | Agent、Data、Operations、Security | Forward DDL、Ensure backfill、字段组 CHECK、全局 collision、alias 时间/完整性、Query anti-leak、无物理 FK、中文 COMMENT、Down guard |
| `R02-D05` | Enqueue 的事务、锁顺序和故障原子性 | 固定 Command lock → Receipt → Session → Sequence Counter → Event Counter；同事务写 Message/Input/Turn/Context/Marker/Result/Header；commit 后才发非权威 Wake；故障全回滚且不消耗序号 | Agent、Data、Operations、Test | 100 不同 Source、100 同 Source、跨 Session/双连接池、全部 crash point、backend terminate、commit response lost、`-race -count=3` |

### 3.2 Lane、恢复、Runner 与外部边界

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R02-D06` | HOL、Lease/Fence、Takeover 与数据库时间 | Head 是最小非终态 `enqueue_seq`；`retry_wait/quarantined` 不可跳过；`session_runtime_lease` 是唯一 TTL；所有 Owner 写校验 DB time、owner/fence/version；Takeover fence 精确 `+1` | Agent、Data、Operations、Security、Test | 100 Input 顺序、跨 Session 并发、双 Processor race、expiry boundary、全写路径 stale-fence、takeover/crash、Scanner 二次锁 Head |
| `R02-D07` | legacy Authority、升级资格与 Helper | 只升级严格证明的 pristine pending chat；Authority 固定 `derived_provenance_only + legacy_chat_only`；prepared Ledger 冻结计划；patch/Authority/Turn/Context/applied 同事务；Helper 不创建 Run | Agent、Data、Operations、Security、Test | Receipt/Message 不可变、Keyring/AAD、107 向量 exact-set、root/global anti-join、Helper crash/replay、真实 PG Up/Verify/Down |
| `R02-D08` | EventLog、Foundation Marker 与 Retention | 独立 append-only `session_event_marker` 永久保存 created/accepted 最小证据；EventLog 只承担可裁剪在线投影；Marker rollout 前 Retention disabled；Wake Marker 不能成为业务真源 | Agent、Data、Operations、Security、Test | Marker canonical、dual-write/backfill、不可变 Trigger、Retention CAS、Counter/partition/cleanup、race/crash、CLI Down、Evidence 脱敏 |
| `R02-D09` | terminal commit 和 Evidence 的唯一原子拓扑 | 归一化执行事实批准后，同一 Agent PG 事务冻结 Result/Failure/Cancel projection、写 Marker、CAS Run/Turn/Input 终态并释放 Lease；投影重放不得重执副作用；此项属于 A2 prerequisite | Agent、Data、Operations、Security、Test | 每个写点 crash、terminal/Marker 不可分离、投影重放零 Model/Tool/RPC、stale fence、Evidence exact-set |
| `R02-D10` | Redis Wake、Scanner、Readiness generation 与 Redis 全失效 | Redis 只作 hint；Scanner 从 PostgreSQL 恢复并复用 Claim；Foundation/Marker/Lane/Processor/ClaimAllowed 分层；generation 精确匹配；Redis 全失效仍满足 P95≤30s | Agent、Data、Operations、Security、Test | publish failure/loss/duplicate/early wake、Scanner race/Query Plan、P95 测量、generation drift、零早启 Claim/Wake |
| `R02-D11` | Eino Runner、Checkpoint 与 Drain 的生产适配 | Runner 只消费成功 Claim 的稳定 Run；Checkpoint 若进入后续阶段必须绑定 Session/Input/Run/fence/epoch；Drain 停止 Claim、继续 Heartbeat，只在 terminal 或 durable handoff 后 release；旧 fence Set/Delete 全拒绝 | Agent、Operations、Security、Test | event drain、cancel propagation、checkpoint epoch/CAS、stale fence、deadline/handoff、Feature close order、goroutine leak、restart/takeover |
| `R02-D12` | Cancel command、权限与不抢占 | Cancel Request 是 durable first-write-wins control fact，不释放 HOL；非 owned Head 先做 cancel-specific claim；unknown 不得 terminal；API 不接受调用方覆盖 user/project/fence | Agent、Frontend、Operations、Security、Test | 同义 replay/异义 conflict、scope/权限、目标 Input/Run/version、pending/retry/quarantine、response-lost Query、UI authority-only 结果 |
| `R02-D13` | HTTP/IDL、错误 Registry、权限与 Query 防枚举 | 单独冻结公开 schema/version 和错误 exact-set；内部恢复错误不冒充业务 Result；wrong scope/type/digest 返回 conflict 且结果字段全空 | Agent、Business、Frontend、Security、Test | OpenAPI/IDL/生成物、错误优先级、跨 scope/不存在等价防枚举、request ID/audit、前端负向测试 |
| `R02-D14` | R02 Evidence 与 `SMK-017/020` 通过条件 | 只接受真实 API + PG + Redis/Scanner + 可控 Adapter + 故障注入；Corpus、浏览器顺序和 Migration 005 fixture 不能替代；Evidence 绑定 source/binary/migration 且 secret-free | Agent、Frontend、Operations、Security、Test | response lost、Redis loss、restart、HOL call count、SSE/browser reconnect、Evidence exact-set/secret scan、连续重复运行 |
| `R02-D15` | `SMK-018` Worker 范围是否属于 R02 | 固定为 deferred/W3-owned；R02 只冻结 Lane 边界，不关闭 Worker Job lease、Provider unknown、Upload/Finalize | Agent、Worker、Operations、Security、Test | R02 manifest exclusion、W3 契约引用、R02 测试证明无 Worker claim/provider/finalize 生产路径 |

### 3.3 Turn Context、引用 Owner 与 A1 Gate

| 决策 ID | 决策问题 | 推荐候选 | Owner 候选 | 最低关闭证据 |
| --- | --- | --- | --- | --- |
| `R02-D16` | Turn/Context 创建、不可变与恢复读取边界 | Turn 与 Context 分表；需 Turn 的 Input 在 Ingress/legacy apply 同事务创建 Turn+Context；Claim/retry/takeover 只读原 Context；Context append-only、无 Update/Delete API；损坏时失败关闭并阻塞 HOL | Agent、Data、Operations、Security、Test | `TC-P01/P08/P09/P10` 结论、同 Input 单 Turn/Context、配置更新隔离、commit response lost、不可变 Trigger、legacy apply、takeover 字节级复用、真实 PG crash |
| `R02-D17` | 模型历史、Message Set 与 Summary | Agent-owned append-only `user/assistant/tool` 历史；System/Skill/Continuation 不伪装为 Message；v1 使用有界 full-array digest；prefix-chain 只能用新 schema/domain v2；Summary 是独立不可变 ref/digest | Agent、Data、Product、Security、Test | `TC-P02/P03` 结论、role exact-set、ToolCall/Result 因果、Message/Event cutoff、空/Unicode/256 边界 golden、Summary 算法/保留、PG Explain |
| `R02-D18` | Prompt/Tool/Runtime/Model/Budget/Access/Approval Ref 的发布、解析和保留 Owner | 各对象使用不可变、版本化 ref/digest；Business Authorization 是权限真源；Agent 保存 Access Snapshot；恢复不刷新预算或当前配置；目录 availability 不构成 Tool 执行授权 | Agent、Business、Finance、Operations、Product、Security、Test | `TC-P04～P07` 结论、逐对象 Owner 表、Prompt/Tool exact-set、预算/Provider unknown、权限/资源版本、Decision/Consumption、跨 Module Query/version 测试 |
| `R02-D19` | ADR-001/002/008/010 对 A1 的精确适用范围 | 建议：A1 不创建 ADR-001 execution root/ref/Receipt projection；Session Command request/result digest 保持独立域，不套用 ADR-002 Tool 摘要改名；ADR-008 的 A2 Receipt/canonical projection 要求对 A1 carve-out，但 A1 自身不得导入 test-only evaluator；ADR-010 的 Feature lifecycle/readiness 适用于 A1，建议加入 A1 Gate | Agent、Data、Operations、Security、Test；涉及 Context/Authority 时追加 Business/Product/Finance | ADR disposition 结构化记录、A1 file/object exact-set、Lane Feature Builder/readiness/rollback/close-order、禁止路径扫描、A1/A2 boundary tests |

## 4. 源待决项到稳定 ID 的映射

本节是位置序号到稳定 ID 的规范 crosswalk。源文档仍保持 Draft，不因被映射而关闭。

### 4.1 Runtime 第 6 节

| 源位置 | Stable IDs |
| --- | --- |
| Runtime 6.1：Enqueue/控制命令 Receipt 与真实 Repository | `R02-D01`、`R02-D05` |
| Runtime 6.2：Source/Turn/Resolution/Cancel Registry | `R02-D03`、`R02-D13` |
| Runtime 6.3：100 Input、跨 Session、双 Processor | `R02-D06` |
| Runtime 6.4：不可变、Retention、Context、SQL/Migration | `R02-D04`、`R02-D06`～`R02-D08`、`R02-D16`～`R02-D18` |
| Runtime 6.5：Runner/Cancel/Checkpoint wrapper | `R02-D11`、`R02-D12` |
| Runtime 6.6：Wake/Scanner/Readiness | `R02-D10` |
| Runtime 6.7：Cancel HTTP/IDL | `R02-D12`、`R02-D13` |
| Runtime 6.8：terminal Repository 原子性 | `R02-D09` |
| Runtime 6.9：SMK-017 Evidence | `R02-D14` |
| Runtime 6.10：SMK-018 Worker | `R02-D15` |

### 4.2 Ingress 第 7 节

| 源位置 | Stable IDs |
| --- | --- |
| Ingress 7.1：Lane/Resume Command Receipt | `R02-D01` |
| Ingress 7.2：失败命令 Receipt | `R02-D02` |
| Ingress 7.3：Receipt 物理形态 | `R02-D04` |
| Ingress 7.4：Approval/Batch Class/Authority | `R02-D03`、`R02-D18` |
| Ingress 7.5：alias 物理落点 | `R02-D04` |
| Ingress 7.6：Receipt/Event/Wake retention | `R02-D08` |
| Ingress 7.7：HTTP/IDL/错误/防枚举 | `R02-D13` |
| Ingress 7.8：Migration/Repository/Scanner/故障 | `R02-D05`、`R02-D06`、`R02-D10` |
| Ingress 7.9：Evidence Bundle | `R02-D09`、`R02-D14` |
| Ingress 7.10：Processor/Drain/Runner | `R02-D10`、`R02-D11` |

### 4.3 PostgreSQL、legacy 与 Turn Context

| 原决策/位置 | Stable IDs |
| --- | --- |
| `PG-D01/PG-D02/PG-D04`：Header/Result/alias/内部 CAS | `R02-D04`、`R02-D05` |
| `PG-D03`：独立 Marker | `R02-D08` |
| `PG-D05/PG-D08`：Lease 唯一 TTL、严格 HOL | `R02-D06` |
| `PG-D06/PG-D07`：eligible legacy、Turn 无 Run | `R02-D07`、`R02-D16` |
| PostgreSQL 第 10 节 P0 1～3 | `R02-D07`、`R02-D08`、`R02-D16`～`R02-D18` |
| PostgreSQL 第 10 节 P0 4～6 | `R02-D04`、`R02-D06`、`R02-D07`、`R02-D10`、`R02-D12` |
| PostgreSQL 第 10 节 P0 7～10 | `R02-D09`～`R02-D11`、`R02-D14` |
| legacy 第 11 节下一步 1～5 | `R02-D07`、`R02-D08`、`R02-D10`、`R02-D11`、`R02-D16`～`R02-D19` |
| `TC-P01/TC-P08/TC-P09/TC-P10` | `R02-D16`，并分别依赖 `R02-D08/R02-D10/R02-D14` |
| `TC-P02/TC-P03` | `R02-D17` |
| `TC-P04/TC-P05/TC-P06/TC-P07` | `R02-D18` |

[`runner-session-lane-review-v1.md`](./runner-session-lane-review-v1.md) 第 16.2 节还混有 R01、R03、R08、Provider、Approval、Budget 等外部 Gate 项。它们只在与 R02 相交时映射到 `R02-D03/D09/D11/D13/D18/D19`，不能被 R02 单独关闭。

## 5. ADR disposition 与 A1 精确边界

### 5.1 ADR-001

R02 的 Session Command Header/Result、Foundation Marker 和 Lane 状态不是 ADR-001 所说的 Tool execution/Receipt projection 写模型。推荐 disposition：

- A1 不创建 `logical_tool_execution/execution_segment/execution_ref_slot/execution_ref_observation`；
- `R02-D09` 的 terminal Receipt/projection 只作为 A2 前置语义保留，不能随 A1 实现；
- A1 不提交 `resolved`、不冻结 Tool/Model Receipt，也不以全量 Receipt snapshot 代替未来归一化执行事实。

### 5.2 ADR-002

`session_lane_ingress_request.v1` 已有独立的 Session Command request digest，Session Command Result 也有独立 result digest。推荐 disposition：

- 它们不等同于 Tool `intent_digest/receipt_digest/effect_request_digest/projection_payload_digest`；
- ADR-002 不按名称重命名或复用 Session Command digest；
- 若未来需要跨域关联，使用显式 ref/mapping，不让两个摘要域互相授权或参与对方幂等。

### 5.3 ADR-008

A1 必须使用生产 R02 state transition、DTO validator 和 Repository，不得导入 `agent/tests/**` 或 `_test.go` evaluator。推荐只对 ADR-008 的 A2 execution/Receipt canonicalizer 入口作 A1 carve-out，而不是允许 A1 保留平行测试实现。

### 5.4 ADR-010

Lane 的 Processor、Scanner、Readiness generation、Drain 和关闭顺序本身需要 Feature lifecycle。推荐把 ADR-010 明确加入 A1 Gate；若 Owner 拒绝，则 R02 必须冻结语义等价的最小 Lane Feature Builder、rollback、readiness 与 reverse close-order，不能继续直接膨胀顶层 bootstrap。

### 5.5 推荐的 A1 maximum scope

即使 R02 与相关 ADR 正式 Approved，A1 也最多允许：

- 只接收现有 `user_message` production source；Approval/Batch 保留 Corpus、production unavailable；
- 为 user message 创建/加载已批准的稳定 Turn 与最小不可变 Context；不产生 Assistant/Tool 历史或 Summary Runtime；
- `Claim/Renew/MarkRunning/Retry/Expire/Release`、严格 HOL、Lease/Fence、Takeover/Crash Recovery；
- PostgreSQL Scanner 与 Redis 丢失唤醒兜底；
- 只依赖窄 `TurnExecutor` 端口的 Processor；A1 测试 Adapter 不构成业务 Graph 或生产执行授权；
- 分层 Readiness、generation、Drain 与安全关闭。

A1 继续禁止：

- `resolved/dead` 业务终态提交和 terminal Receipt/projection；
- execution root/segment/ref/observation、ToolReceipt、ModelReceipt、Checkpoint；
- Approval/Decision/Consumption、Billing、Business Decide/Query；
- ChatModel/Graph/Tool availability、A2UI Schema/Card/Action；
- Worker Claim、Provider、Upload、Finalize；
- 宣称 `SMK-017/018/020` 通过。

最终 A1 scope 必须由 `R02-D19` 的 Owner 结论和 versioned manifest exact-set 冻结；本文推荐不能自行扩权。

## 6. Aggregate manifest 的创建前置

R02 aggregate manifest 只能在以下条件全部满足后创建为候选：

1. `R02-D01`～`R02-D19`、`PG-D01`～`PG-D08`、`TC-P01`～`TC-P10` 都有结构化 disposition；
2. ADR-001/002/008/010 的 R02/A1 范围有结构化决定；
3. legacy upgrade 以新版本 manifest 发布完整排序的 `fixture_ids/vector_ids`；不得静默扩写已审计 v1 字节；
4. 五个 child manifest 的 schema、raw digest、Corpus file digest、fixture/vector exact-set 和 target-test exact-set 可机械合并；
5. design source exact-set、validator source exact-set、构建输入、依赖、toolchain、运行环境和 workflow/action trust root 有完整 closure；
6. A1 allowed/prohibited object/path/test exact-set 与 A2/B0/B1/W3 exclusion 明确；
7. 最终 `required_owner_roles` 从已决定范围推导，且 role→责任→受保护平台身份映射完成；
8. GOV-D01～D06 生效，真实 PR 的 Review freshness、dismissal、head update 和 no-bypass 失败关闭。

候选 aggregate manifest 至少需要：

- `schema_version/gate/status/implementation_unlocked`；
- 当前 commit、所有 source/child manifest 的 path + raw SHA-256；
- 7 个 Corpus file、fixture/vector IDs 与总数 exact-set；
- 58 个 target tests 的排序 exact-set；
- validator direct source、transitive build input、dependency/toolchain/environment closure；
- `R02-Dxx/PG-Dxx/TC-Pxx/ADR` disposition refs；
- A1 allowed/prohibited scope 与 downstream exclusions；
- Owner exact-set、approval refs、reopen/blocker 与 supersedes 信息。

当前 child manifest 小计或本文都不能代替该 aggregate manifest。

## 7. 状态迁移

R02 当前保持 `expansion_frozen`。只有同时满足下列条件，才可进入 `awaiting_review`：

1. 本文全部 Stable Decision 有结构化 Owner 结论，跨文档无冲突；
2. ADR disposition 与 A1 scope exact-set 冻结；
3. child manifest 缺口和 aggregate manifest 完成，所有摘要/数量/exact-set 一致；
4. validator/build/toolchain/trust-root closure 形成候选；
5. 最终 Owner exact-set 已从范围推导，不存在“稍后追加 Owner”；
6. 当前状态仍明确 test-only，生产实现保持禁止。

进入 `review_frozen/approved` 还必须在同一候选 head 上取得全部角色的有效平台 Review，并由 base-owned authority 验证身份、角色、freshness、Ruleset 与 no-bypass。手工修改 manifest、文档勾选、自报 JSON、任意 URL、本地测试或 commit ancestor 都不能推进状态。

## 8. 建议 Owner ballot 顺序

1. 先决定 `R02-D19`，固定 ADR scope 与 A1 maximum scope；
2. 再决定 `R02-D16`～`R02-D18`，冻结 Turn Context、历史和所有 Ref Owner；
3. 决定 `R02-D01`～`R02-D08`，冻结 Command/Ingress/PG/legacy/Marker；
4. 决定 `R02-D06/D10/D11/D12`，冻结 HOL、Scanner、Runner/Drain 和 Cancel；
5. 将 `R02-D09` 明确保留为 A2 prerequisite，并冻结 `R02-D13` 的公开契约边界；
6. 决定 `R02-D14/D15` 的 Evidence 与 W3 exclusion；
7. 推导最终 Owner exact-set，补齐 child manifest 后生成 aggregate candidate；
8. 最后进入受保护平台 Review，不在本文内自报 Approved。

## 9. 当前结论

本文关闭的是“R02 待决项没有稳定 ID”的治理缺口，不关闭任何语义或实现 Gate。当前事实仍是：

- `R02-D01`～`R02-D19` 全部 `awaiting_owner_decision`；
- `PG-D01`～`PG-D08` 与 `TC-P01`～`TC-P10` 仍为推荐候选；
- 五个 child manifest 只能机械小计为 298 条向量/58 个目标测试，不能形成 aggregate candidate；
- R02 仍为 `expansion_frozen`，生产 A1/A2 均未解锁。
