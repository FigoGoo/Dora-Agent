# V2 通用 `user_message` Runtime 最小 Profile 设计

> 文档状态：Approved for Development Preview（方案 A）/ 不授权生产实现
>
> Profile：`user_message.runtime.v2preview1`
>
> 设计日期：2026-07-16；产品行为批准：2026-07-17
>
> Owner：Agent Runtime；Business 继续拥有 Project/QuickCreate，前端继续只经 Business 同源入口访问 Workspace
>
> 实现门禁：产品 Owner 已依据“继续推进开发”批准第 4.1 节方案 A；`B0`、B1、B2、本地 Fake B3 与 B4 已关闭。该完成状态不扩权到真实 Provider、Tool-enabled Profile、共享或生产环境；本 Profile 始终不得在共享/生产环境启用。

关联文档：[唯一开发计划](../../requirements/full-function-smoke-development-plan.md)、[Development Preview 批准清单](approvals/user_message_runtime_v2preview1/approval_manifest.json)、[专用契约 Corpus](../../../agent/tests/contract/testdata/v2_user_message_runtime_preview1/manifest.json)、[Session Lane Runtime 草案](session-lane-runtime-contract-v1.md)、[Ingress/Command Receipt 草案](session-lane-ingress-command-contract-v1.md)、[PostgreSQL 物理设计草案](session-lane-postgresql-design-v1.md)、[Immutable Turn Context 决策包](immutable-turn-context-decision-review-v1.md)、[Session Event Marker 设计](session-event-foundation-marker-v1.md)。

## 1. 目的与本批结论

V2 首项只解决一个已经存在的用户阻断：Preview 关闭时，非空 QuickCreate 已经可靠创建 `user_message/pending` Input，但 Agent 没有通用 Processor，Input 会永久停留在 `pending`。

本 Profile 的目标是让 QuickCreate V1/V2 的首个非空 `user_message`：

1. 保留原 `source_type/source_id/message_id/input_id` provenance；
2. 经同一 Session 严格 HOL、PostgreSQL Lease/Fence 与真实 Eino ChatModelAgent 执行；
3. 冻结稳定 Turn、Run、Model/Output Receipt；
4. 形成可由 Snapshot/SSE/硬刷新恢复的 `resolved/dead/recovery_pending` 结果；
5. Agent 重启、投影失败、响应丢失和旧 Fence 写入均不重复模型或副作用；
6. 不影响已经通过的 `plan_creation_spec.v1preview1` canonical Smoke。

本设计是完整生产 Session Lane 的窄开发 Profile，不关闭 `SMK-017/020`、Immutable Turn Context 十项 P0 或 P1 发布门禁。

## 2. 当前事实

### 2.1 已经具备

- Frontend 在 Preview 关闭时把非空 `initial_prompt` 交给 QuickCreate；无 Skill 使用 `project_quick_create.v1`，有 Skill 使用 `project_quick_create.v2`。
- Business 以 Outbox/Dispatcher 调用 Agent `EnsureProjectSessionV1/V2`，并具备响应未知后的原命令查询恢复。
- Agent Ensure 在一个事务中创建 Session、Skill Snapshot、Runtime Lease、加密 User Message、`user_message/pending` Input、`session.created`、`session.input.accepted` 和 Command Receipt。
- Business Bootstrap、Agent Workspace Snapshot/SSE 与前端七种 Input 状态展示已经存在。
- V1 Preview 已验证 HOL、Lease/Fence、稳定身份、Runner、模型/Tool Receipt、投影恢复、重启和真实 PostgreSQL/Redis/etcd/Chromium 形态。

### 2.2 唯一执行断点

- Preview `ClaimNext` 必须关联 `creation_spec_preview_run` 和 Preview Tool Receipt，因此永远不会领取 `user_message`。
- Preview Processor 只在 `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true` 时启动。
- 当前 Eino Runner 要求结构化 Preview Intent，`FakeRouter` 只允许 `plan_creation_spec`，不能消费自由文本。
- `session_message`、Workspace DTO 和前端 Message parser 当前只允许 `role=user`；通用 Assistant/Tool 历史尚未批准。
- 现有 Preview Model Receipt 的 `pending` 不能区分“尚未发送”和“已发送但未冻结”，不能用于真实 Provider。

## 3. Profile 范围

### 3.1 本批包含

- 只处理 Ensure V1/V2 创建的首个非空 QuickCreate `user_message`；
- 同时覆盖无 Skill 与有 Skill 的 Session；
- 新旧符合严格 pristine 条件的 pending Input；
- 单个无 Tool 的 ChatModelAgent Turn 或第 4.1 节批准的唯一首轮行为；
- 本地确定性 FakeModel；真实模型只完成接口与 `model_unknown` 状态设计，默认不启用；
- 通用 Turn Output 投影、终态/恢复事件、Snapshot/SSE/前端恢复；
- PostgreSQL Scanner 兜底丢失 Wake；Redis 仍只作可选延迟优化。

### 3.2 本批不包含

- Workspace 后续 Chat POST、多轮自由对话、Assistant/Tool Message History；
- `analyze_materials`、`plan_storyboard`、`write_prompts` 或其他新 Graph Tool；
- 计费、Approval、Operation/Batch/Job、Worker、媒体 Provider；
- Cancel/Resume/Preempt、完整 Checkpoint、Summary、Retention 或 Marker rollout；
- 把已有 `user_message` 改成 `creation_spec_preview`，或为它补造 Preview Run/Receipt；
- 将 Business `recent_run_status` 改成 Agent 终态真源。

## 4. 已批准产品与 Router 决策

### 4.1 首轮用户可见行为（方案 A 已批准）

2026-07-17，产品 Owner 以“继续推进开发”确认继续当前推荐路径，本 Profile 据此只批准方案 A。实现不能按环境静默切换为其他方案：

| 方案 | 行为 | 范围与代价 | 建议 |
|---|---|---|---|
| A | 无 Tool ChatModelAgent 返回稳定“需求已接收，可继续选择流程规划”的安全 Direct Response Card，Input 正常 `resolved` | 最小关闭通用 Lane/Turn/Run/Receipt；不把未结构化文本偷渡到 Preview Tool | **已批准为 `v2preview1`** |
| B | Router 将自由文本转换为严格 CreationSpec Intent，并自动调用 `plan_creation_spec` | 需要新的 unstructured→typed 契约、通用 Tool Receipt/Turn Context，且不能复用 Preview 表；范围接近下一 V2 Tool 批 | 后续 `v2preview2` |
| C | 仅把 Input 标记失败/死亡 | 技术上结束 pending，但没有可用 Agent 行为 | 拒绝 |

方案 A 的 Direct Response 只是一张通用 Turn Output Card，不写 Assistant Message History，也不声称已执行任何 Graph Tool。

### 4.2 Router exact-set

`v2preview1` 的 Router 决策只允许：

```text
direct_response
failed
model_unknown
```

- Executable Tool Registry 固定为空集；目录中的六 Tool 不等于当前 Turn 可执行授权。
- FakeModel 必须输出固定版本的 Direct Response DTO，不复制完整 Prompt，不生成 ToolCall。
- 未知输出、一个或多个 ToolCall、未知 schema、超限正文全部失败关闭。
- 最大模型调用一次；模型层是唯一 retry/failover Owner，Runner 不叠加重试。
- 方案 B 获批前，任何自动 `plan_creation_spec` ToolCall 都是契约错误。

## 5. 可信入口与稳定身份

### 5.1 输入资格

只接受同时满足以下条件的首 Input：

- Session active、未归档，且 Input `source_type=user_message`；
- Message/Session/Input/Ensure Receipt 的 User、Project、Session、Source 和摘要逐值一致；
- `enqueue_seq=1`，同 Session 没有更早非终态 Input；
- Input 为 `pending/attempts=0`，Claim provenance 为空、Fence 为 0；
- `session_runtime_lease` idle，且 legacy cohort 未被其他 Runtime 领取；
- `session.input.accepted` 仍在可验证在线窗口；缺失、已裁剪或不一致时 fail-closed；
- 非空 Message 经当前 Keyring 解密并重算 content digest 成功。

不符合资格的行进入脱敏 blocker 清单，不能重置、跳过、改型或伪造终态。

### 5.2 稳定 ID

- `input_id/message_id/source_id` 永远复用 Ensure 已提交事实；
- `turn_id` 在新 Ensure 事务或 legacy apply 事务中一次冻结；
- `run_id` 在首次成功 Claim 时创建，retry/takeover/restart 复用；
- `output_id/terminal_event_id/model_call_id` 在 Turn/Context 创建时预分配；
- 技术重试不生成新 Turn、Run、Model Call 或 Event ID。

## 6. 最小不可变 Turn Context

`v2preview1` 只冻结本批必需字段，不冒充完整 58 字段生产 Context：

```text
schema_version = user_message.turn_context.v2preview1
session_id / input_id / message_id / turn_id
user_id / project_id
message_cutoff_seq
message_content_digest
skill_snapshot_ref / skill_snapshot_digest
prompt_ref = user_message.direct_response@v1
prompt_digest
tool_registry_ref = user_message.empty_tools@v1
tool_registry_digest
runtime_policy_ref / runtime_policy_digest
model_route_ref = local.fake.user_message@v1
model_route_digest
budget_ref / budget_digest
access_scope_ref / access_scope_digest
context_digest
```

Context 与 Turn 同事务创建，append-only；Claim、retry、takeover 和 restart 只能加载并重算原版本，不读取“当前最新配置”。本 Profile 的模型历史只包含 cutoff 内唯一 User Message，禁止读取未来 Message/Event 或拼接 Preview Context。

## 7. 状态机

### 7.1 Input / Turn / Run

本开发 Profile 复用当前已落库 Input 状态，不提前引入完整生产 `quarantined`：

```text
Input: pending -> claimed -> running
       running -> resolved | dead | retry_wait | recovery_pending
       retry_wait -> claimed
       recovery_pending -> claimed（仅权威恢复收敛后）

Turn:  created -> running -> completed | failed

Run:   created -> running -> completed | failed | recovery_pending
```

`recovery_pending` 始终是 Session HOL，不能放行后序。完整生产是否改名为 `quarantined` 由后续不兼容 Profile 决定；代码不得同时解释两套名称。

### 7.2 Model Receipt

```text
reserved -> completed | failed
reserved -> dispatched -> completed | failed | model_unknown
model_unknown -> completed | failed（仅 Provider 按稳定请求键查询得到权威结果）
```

- 本地 FakeModel 只走 `reserved -> completed|failed`，没有外部 unknown outcome。
- 真实 Provider 调用前必须冻结 `provider_request_key + request_digest + model_route_ref`，随后原子标记 `dispatched`。
- `dispatched` 后进程失联且无冻结响应时进入 `model_unknown`；不可查询 Provider 禁止自动重发或 Failover。
- 只有权威证明未发送，才允许在原调用预算内复用同一个稳定请求键重试。
- 真实模型 Adapter 没有稳定请求查询能力时，本 Profile 保持禁用。

### 7.3 Output Receipt

```text
open -> completed | failed
```

Output Receipt 冻结完整 Direct Response 或稳定 Failure DTO 的密文、semantic digest、schema version 和 projection key。冻结后任何投影失败只重放同一 Output，不重新运行 Router/Model。

## 8. PostgreSQL 与单 Processor 拓扑

### 8.1 Forward-only 物理对象

候选新对象使用通用命名，不出现 `creation_spec_preview`：

- `session_user_message_turn`
- `session_user_message_turn_context`
- `session_user_message_run`
- `session_user_message_model_receipt`
- `session_user_message_output_receipt`
- `session_user_message_upgrade_ledger`

所有业务引用使用逻辑 UUID，不创建物理 FK；表、字段、CHECK、索引和中文 COMMENT 必须在 Migration 中显式给出。Receipt/Context/已完成 Ledger append-only，禁止 UPDATE/DELETE；状态行只允许版本化 CAS。

### 8.2 统一 HOL Processor

V2 不能再运行“Preview Processor + UserMessage Processor”两个按类型过滤的独立 Claim 循环。必须抽出一个 Session Lane Claim Owner：

```text
PostgreSQL scanner / optional wake
  -> 选择每个 Session 最小非终态 Input
  -> 同一事务领取 session_runtime_lease + Fence
  -> 按 source_type 分派 user_message 或 creation_spec_preview handler
  -> handler 冻结结果
  -> 同一 owner/fence 终态提交并释放 Lane
```

Scanner 不能先筛选 Source，否则会越过更早 HOL。`session_runtime_lease` 是唯一 TTL 真源；Input owner/fence 只作 Claim provenance。所有时间条件使用 PostgreSQL 时间，旧 Fence 写入必须零增量。

## 9. 终态、事件与前端投影

### 9.1 通用安全 DTO

Direct Response Card：

```text
schema_version = session.turn.direct_response.card.v1
turn_id / run_id / input_id
status = completed
message_code = creation_request_received
summary = 固定服务端中文文案
available_actions = [open_toolbox]
```

Failure Card：

```text
schema_version = session.turn.failure.card.v1
turn_id / run_id / input_id
status = failed | recovery_pending
error_code
retryable
summary = 稳定脱敏文案
```

事件和 Evidence 不包含原 Prompt、密文、Provider Payload、内部错误栈或 Secret。

### 9.2 Event exact-set

```text
session.turn.completed
session.turn.failed
session.turn.recovery_pending
```

- completed/failed 与 Output Receipt、Run/Turn/Input 终态、Projection 和 Lease release 在同一 Agent PostgreSQL 事务提交；
- recovery_pending 只表示 HOL 仍被权威恢复阻塞，不是终态；
- Event 使用预分配稳定 ID 和固定 projection index，重放零增量；
- Snapshot 提供 nullable `latest_turn_output`，SSE 与硬刷新共用同一严格 parser/reducer；
- 未知 schema/type/status 整包失败关闭，不能降级成成功。

本 Profile 不新增 Assistant Message。后续多轮 Chat 必须另行扩展 `session_message role=user|assistant|tool`、ToolCall/ToolResult 因果配对和 History 摘要，不能把 Card 文案反写为 Message。

## 10. 终态事务与恢复

唯一合法提交顺序：

1. 验证当前 Session/Input/Turn/Run owner、Fence、version 和未过期 Lease；
2. 读取并重验冻结 Model/Output Receipt；
3. 写或重放通用 Projection；
4. 追加稳定 Event；
5. CAS Run、Turn、Input 为对应终态；
6. 仅由当前 owner/fence 释放 Session Lease；
7. 一次提交。

确定性映射：

| 权威结论 | Input | Turn | Run |
|---|---|---|---|
| Direct Response 已冻结 | `resolved` | `completed` | `completed` |
| 确定失败且 Failure Receipt 已冻结 | `dead` | `failed` | `failed` |
| 模型结果 unknown | `recovery_pending` | `running` | `recovery_pending` |

读取/投影临时失败不消耗模型执行预算。已有冻结 Output 时，重启只修复投影；无冻结且明确未发送时才允许有界执行重试；任何 unknown 一直阻塞 HOL。

## 11. Migration 与启用顺序

1. **Preflight**：Processor 关闭；只输出安全计数与 blocker exact-set。
2. **Expand**：新表/列 nullable，现有 Ensure/Preview Writer 不受影响。
3. **兼容 Writer**：仅在 Profile 开关与 local 环境同时开启时，新 Ensure 非空 Prompt 同事务创建 Turn/Context；共享/生产默认关闭。
4. **Legacy Helper**：按 `absent -> prepared -> applied -> verified` Ledger 分批冻结 eligible Input 的 Turn/Context；不创建 Run。
5. **Verify/Contract**：真实 PostgreSQL 核对 anti-join、摘要、唯一约束、append-only、索引与 Down guard。
6. **Unified Lane**：旧 Preview Processor 排空并停止后，才启用统一 Claim/Source dispatcher；不得双 Processor 重叠。
7. **Frontend/Event**：严格 DTO、Snapshot/SSE reducer 与浏览器门禁通过后，才把 Capability Ready 置 true。

建议配置：

```text
DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=false
DORA_AGENT_USER_MESSAGE_RUNTIME_PROFILE=user_message.runtime.v2preview1
```

启用必须同时满足 `DORA_ENV=local`、loopback、本地专用数据库、Profile exact match、Migration generation 和 legacy blocker 为零；生产/共享环境失败关闭。

## 12. 验收矩阵

| ID | 场景 | 必须证明 |
|---|---|---|
| `V2-UM-001` | QuickCreate V1 非空 Prompt | 原 User Message/Input provenance 不变，pending→running→resolved，唯一 Turn/Run/Receipt/Event |
| `V2-UM-002` | QuickCreate V2 + Published Skill | 冻结原 Skill Snapshot；Skill 不能扩展空 Tool Registry |
| `V2-UM-003` | 空 Prompt | 不创建 Message/Input/Turn/Run/Receipt/Event |
| `V2-UM-004` | 同命令 100 并发/重放 | 只有一组事实和一次 FakeModel 调用 |
| `V2-UM-005` | 同 Session 两 Input | 严格 HOL；后序执行计数为零；跨 Session 可并行 |
| `V2-UM-006` | Claim/模型前/输出冻结后/投影前崩溃 | 重启复用原 Input/Turn/Run；冻结输出不重调 |
| `V2-UM-007` | 双实例与 Lease Takeover | 只有当前 Fence 可写；旧 Fence 全部零增量 |
| `V2-UM-008` | 模型 dispatch 后断链 | `model_unknown/recovery_pending`；不可查询时模型调用增量为零 |
| `V2-UM-009` | Redis Wake 丢失 | PostgreSQL Scanner 在固定 deadline 内领取同一 HOL |
| `V2-UM-010` | SSE 断线、硬刷新、Agent 重启 | 恢复同一 Direct Response/Failure Projection，不重复 Event |
| `V2-UM-011` | Preview 混合 Lane | 更早 user_message 未终结时 Preview 仍 409 零增量；终结后 Preview 可入队 |
| `V2-UM-012` | 禁止副作用 | Approval、Billing、Operation/Batch/Job、Worker、Business Draft 增量均为零 |
| `V2-UM-013` | V1 回归 | `make plan-spec-preview-smoke` 与三 Module/前端门禁继续通过 |

### 12.1 当前实现与剩余门禁（2026-07-17）

- `B1`：已关闭，批准清单状态为 `completed_legacy_helper_verified`。Migration 008/009、兼容 Ensure Writer、eligible-only Legacy Helper、分批 Ledger apply/verify、崩溃恢复与真实 PostgreSQL Repository Contract 均已完成；canonical Evidence 在专用 `dora_agent_test` 验证 Ledger `verified / generation=1 / version=3`、原始身份与 provenance 保持不变。
- `B2`：统一 Source Dispatcher 已在 Source 过滤前选择 Session 最小非终态 HOL，Scanner、Lease/Fence、Takeover、Processor mutual exclusion 与 Drain 已完成；Redis Wake 尚未接线，当前只以 PostgreSQL Scanner 保证正确性。
- `B3`：空 Tool Registry、无 Tool Eino ChatModelAgent、本地确定性 FakeModel、Model/Output Receipt 与冻结后重放已完成。本地 Fake 只实现 `reserved -> completed|failed`；`dispatched/model_unknown` 是后续真实 Provider 的门禁设计，不是本批已实现状态，也不得据此启用真实模型。
- `B4`：已关闭。Workspace Snapshot/SSE、严格 Card Contract/Reducer、Direct Response/Failure UI、刷新恢复、canonical 编排、真实 Chromium 用例和 shell 契约门禁均已完成；最终源码 Run `20260716T202111Z-58305` 已发布可保留 Trial Evidence。

因此方案 A 的本地 Fake Development Preview 批次 `B0`～`B4` 已关闭。`V2-UM-008` 仅在后续另行批准真实 Provider 时执行；本地 Fake 验收不得伪造 `model_unknown` 或把它标为已实现。Redis Wake 仍只是相对 PostgreSQL Scanner 的可选延迟优化，P1/生产门禁继续后置。

Canonical V2 Smoke 命令固定为 `make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example user-message-runtime-smoke`。它必须使用真实 PostgreSQL、Redis、etcd、Business/Agent/Vite 与 Chromium，强制 `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false`、`DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true`，且不能以 Docker socket/Compose readiness 代替宿主机 `15432/16379/12379` 直连。Evidence 只保存安全 ID、摘要、状态、计数和布尔断言，Schema 固定 `user_message_runtime.trial_evidence.v1`，权限固定 `0600`。

最终源码 Run `20260716T202111Z-58305` 已在 PostgreSQL `16.4` 的专用 `dora_agent_test`、Redis、etcd、宿主机 Business/Agent/Vite 与真实 Chromium 上通过。Evidence 位于 `.local/smoke/user-message-runtime-trial-evidence.json`，权限为 `0600`，source digest 为 `sha256:7b11d556defb379de05a04ff4e9b808618b784abc0244b3e75ceaeb9044d79ad`，文件 SHA-256 为 `sha256:7d2b1c0d0db69695e4eea2f54e1ffcacb6f01294fcf116f3ce8c1c312f363ab1`，32 项断言逐项为 `true`；其中包括 Legacy Ledger `verified / generation=1 / version=3`、既有身份与 provenance 保持、exact/continuous etcd instance provenance、关闭后的 lease/prefix 清理、唯一 Turn/Run/Model Receipt/Output Receipt/Projection/terminal Event、Model Receipt execution fence 与终态 Run fence 一致、CreationSpec Preview 零运行、刷新同一 Turn/Run/Input、无 Assistant Message、源码零变化和进程端口清理。该本地 Trial Evidence 不提交仓库，也不等于 P1 发布 Evidence。

## 13. 批次与签字

实施顺序：

1. `B0` 本文与首轮产品行为批准（**已关闭：方案 A，2026-07-17**）；
2. `B1` Migration、兼容 Ensure Writer、legacy Helper 与真实 PostgreSQL 证据（**已关闭：`completed_legacy_helper_verified`，Run `20260716T202111Z-58305`**）；
3. `B2` 统一 Lane Claim/Source Dispatcher、Fence、Scanner、Drain（**已完成；Redis Wake 未接线，Scanner 为正确性兜底**）；
4. `B3` 无 Tool ChatModelAgent、FakeModel、Model/Output Receipt 与终态事务（**本地 Fake 范围已完成；真实 Provider unknown 路径仍关闭**）；
5. `B4` Workspace Snapshot/SSE、前端 Direct Response/Failure Card 与 canonical Smoke（**已关闭；最终源码 Run `20260716T202111Z-58305` passed**）。

签字清单：

- [x] 产品：第 4.1 节方案 A，Approved for Development Preview（2026-07-17）；
- [x] Agent Runtime：统一 HOL、空 Tool Router、本地 Fake Receipt/恢复与 Eino 边界；
- [x] Agent PostgreSQL/Data：Migration、Writer、锁顺序、Fence、append-only、Down guard 与 Legacy Helper/既有 eligible cohort Ledger 已实现并由真实 PostgreSQL 验证；
- [x] Business/跨 Module：确认 QuickCreate/Ensure V1/V2 入口复用且 Business 无新增终态真源；
- [x] 前端/A2UI：通用 Card、Snapshot/SSE、未知版本和恢复语义；
- [ ] 安全/运维：local-only、Scanner/Drain 与 Development Preview canonical Evidence 已完成；真实 Provider unknown、Redis Wake/告警及 P1/生产 Evidence 仍待后续；
- [x] 测试：真实 PG required-mode、Legacy Upgrade/锁序并发探针与最终源码 Run `20260716T202111Z-58305` Chromium canonical Smoke/Evidence 已通过。

当前结论：**方案 A 已获 Development Preview 产品批准，`B0`、B1、B2、本地 Fake B3 与 B4 已关闭；B1 状态为 `completed_legacy_helper_verified`。最终源码 Run `20260716T202111Z-58305` 已通过真实 PostgreSQL/Redis/etcd/Business/Agent/Vite/Chromium canonical Trial。该进度不改变 Immutable Turn Context 全局 `awaiting_owner_approval`、`implementation_unlocked=false`，也不把 `production_authorized` 改为 true。本批准只授权空 Executable Tool Registry、单次无 Tool ChatModelAgent 与 Direct Response/Failure 投影；`analyze_materials`、真实 Provider 及其他 Tool-enabled Runtime 必须使用后续独立 Profile 重新评审。**
