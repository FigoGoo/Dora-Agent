# Agent 领域数据模型草案

状态：draft
owner：Agent 服务责任域
更新时间：2026-06-28
适用范围：智能体微服务 Agent Runtime 数据库

## 成熟度复核

当前成熟度：draft，不升 `active`。  
使用方式：可作为 Agent DB 边界、表清单、字段类型、状态机、索引、保留周期和测试策略输入；实际 SQL 以 `db/migrations/iterations/20260627_agent_runtime/agent/001_create_agent_runtime.up.sql` 为当前落脚。

已补齐项：字段类型、唯一约束、状态迁移约束、数据保留周期、`agent_memories` 启用策略、run 并发策略和 session 归档策略已在本文冻结。

未冻结项：migration 本地执行证据、查询性能报告和服务级验收报告尚未固化，因此本文仍保持 `draft`。

## 领域边界

Agent 领域数据库只保存 Agent Runtime 数据，包括会话、运行、消息、事件、Tool 调用、任务、中断、草稿产物、记忆和配置。不保存项目、积分、最终资产、作品、企业成员、业务权限、订单、支付等业务事实。业务事实必须通过 RPC 调用业务微服务产生。

## 表清单

| 表名 | 用途 | 数据范围和清洗口径 |
| --- | --- | --- |
| agent_sessions | 工作台会话 | 按用户和项目查询，归档后按保留策略清理 |
| agent_runs | 单次 Agent 执行 | 保存 run 状态、项目引用、模型快照摘要 |
| agent_messages | 用户和 Agent 消息 | 保存可展示消息，不保存系统 Prompt |
| agent_events | AG-UI 事件存储 | 支持 SSE 补偿和 sequence 查询 |
| agent_tool_calls | Tool 调用记录 | 保存公开摘要、状态、耗时、错误码 |
| agent_tasks | 长任务 | 保存生成、保存、恢复等任务状态 |
| agent_interrupts | 人工确认 | 保存确认状态、过期时间和公开 payload |
| agent_artifacts | 草稿产物和黑板 | 保存黑板、分镜、脚本、提示词、业务资产引用 |
| agent_safety_evaluations | 内容安全评估记录 | 保存脱敏评估摘要、摘要哈希、策略版本和可排障字段 |
| agent_memories | 会话摘要和授权偏好 | 不保存业务事实和敏感信息 |
| agent_runtime_configs | 运行配置快照 | 保存 Agent Runtime 配置版本 |

## 通用字段

所有表建议包含：

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| id | string | 是 | pk | 主键 |
| user_id | string | 是 | index | 普通字段，不是外键 |
| space_id | string | 是 | index | 当前空间引用 |
| project_id | string | 否 | index | 业务项目引用 |
| trace_id | string | 是 | index | 链路追踪 |
| created_at | datetime | 是 | index | 创建时间 |
| updated_at | datetime | 是 | 否 | 更新时间 |
| deleted_at | datetime | 否 | index | 软删除 |

## 字段级模型

字段类型以 PostgreSQL migration 为准；以下只列核心字段、唯一约束和边界含义，避免在 Markdown 中复制完整 SQL。

| 表 | 核心字段和类型 | 唯一约束 | 边界说明 |
| --- | --- | --- | --- |
| agent_sessions | `id varchar(64)`、`tenant_id varchar(64)`、`space_id varchar(64)`、`project_id varchar(64)`、`user_id varchar(64)`、`status varchar(32)`、`title varchar(128)`、`last_run_id varchar(64)`、`last_event_sequence bigint`、`snapshot_summary jsonb`、`idempotency_key varchar(160)` | `id` 主键；`idempotency_key` 唯一 | 只保存项目引用和运行摘要，不保存项目标题事实。 |
| agent_runs | `id varchar(64)`、`session_id varchar(64)`、`project_id varchar(64)`、`space_id varchar(64)`、`user_id varchar(64)`、`turn_no bigint`、`status varchar(32)`、`input_summary jsonb`、`skill_selection jsonb`、`model_selection_snapshot jsonb`、`runtime_config_version varchar(64)`、`idempotency_key varchar(160)`、`error_code varchar(64)` | `id` 主键；`idempotency_key` 唯一 | 保存 Agent 执行状态和非敏感模型快照；不保存业务扣费事实。 |
| agent_messages | `id varchar(64)`、`session_id varchar(64)`、`run_id varchar(64)`、`role varchar(32)`、`content_type varchar(32)`、`content text`、`content_summary jsonb`、`sequence bigint`、`safety_status varchar(32)` | `id` 主键；`session_id, sequence` 唯一 | 保存可展示消息；不得保存系统 Prompt 或完整推理链路。 |
| agent_events | `event_id varchar(64)`、`type varchar(96)`、`session_id varchar(64)`、`run_id varchar(64)`、`project_id varchar(64)`、`space_id varchar(64)`、`actor_user_id varchar(64)`、`sequence bigint`、`component varchar(64)`、`payload jsonb`、`payload_schema_version varchar(32)` | `event_id` 主键；`run_id, sequence` 唯一 | AG-UI replay 事实源；payload 只含前端可消费字段。 |
| agent_tool_calls | `id varchar(64)`、`run_id varchar(64)`、`task_id varchar(64)`、`tool_name varchar(96)`、`tool_type varchar(48)`、`risk_level varchar(32)`、`status varchar(32)`、`input_summary jsonb`、`output_summary jsonb`、`timeout_ms integer`、`retry_count integer` | `id` 主键 | 保存公开摘要和状态，不保存完整原始参数、供应商原始响应或密钥。 |
| agent_tasks | `id varchar(64)`、`run_id varchar(64)`、`task_type varchar(48)`、`resource_type varchar(32)`、`status varchar(32)`、`progress_percent integer`、`progress_detail jsonb`、`cancel_requested boolean`、`external_task_ref varchar(256)` | `id` 主键 | 保存长任务状态；外部任务引用不得成为业务资产事实。 |
| agent_interrupts | `id varchar(64)`、`run_id varchar(64)`、`interrupt_type varchar(48)`、`status varchar(32)`、`reason varchar(128)`、`confirmation_payload jsonb`、`allowed_actions jsonb`、`resume_context jsonb`、`expires_at timestamptz` | `id` 主键 | Agent 内部 interruption 事实源，对前端映射为 `confirmation.*` 事件。 |
| agent_artifacts | `id varchar(64)`、`session_id varchar(64)`、`project_id varchar(64)`、`run_id varchar(64)`、`artifact_type varchar(48)`、`status varchar(32)`、`element_type varchar(48)`、`content jsonb`、`business_ref_id varchar(64)`、`visibility varchar(32)`、`version integer` | `id` 主键 | 草稿产物可保存摘要；最终业务资产只保存 `business_ref_id=asset_id`。 |
| agent_safety_evaluations | `safety_evidence_id varchar(64)`、`scene varchar(48)`、`target_type varchar(48)`、`evaluated_object_digest varchar(128)`、`policy_version varchar(64)`、`evidence_version varchar(32)`、`result varchar(32)`、`expires_at timestamptz` | `safety_evidence_id` 主键 | 保存脱敏安全证据；不得保存策略细节、评分和完整 Prompt。 |
| agent_memories | `id varchar(64)`、`user_id varchar(64)`、`space_id varchar(64)`、`memory_type varchar(48)`、`scope varchar(48)`、`content_summary jsonb`、`authorized boolean`、`expires_at timestamptz` | `id` 主键 | 第一版启用，但只允许授权摘要记忆，不保存业务事实。 |
| agent_runtime_configs | `config_key varchar(96)`、`version varchar(64)`、`status varchar(32)`、`owner varchar(64)`、`content jsonb`、`safe_config_refs jsonb` | `config_key, version` 联合主键 | 保存运行配置快照，不保存密钥明文。 |

## 状态机

| 对象 | 状态 | 可迁移到 | 触发条件 |
| --- | --- | --- | --- |
| session | active | archived, expired | 用户归档、清理策略触发，或根据业务项目归档同步 Runtime 只读标记 |
| session | archived | active | 用户恢复会话前通过业务项目权限校验 |
| run | pending | running | 开始执行 |
| run | running | waiting_confirmation, waiting_input, failed, cancelled, completed | 需要人工确认、等待追加输入、失败、取消或完成 |
| run | waiting_confirmation | resuming, cancelled, failed | 用户确认、拒绝、确认过期或权限失效 |
| run | waiting_input | resuming, cancelled, failed | 追加输入、用户取消或权限失效 |
| run | resuming | running, failed, cancelled | 恢复成功、恢复失败或取消 |
| run | running | completed | 执行完成 |
| run | running | failed | 执行失败 |
| run | running | cancelled | 用户取消 |
| task | pending | running | 任务开始 |
| task | running | cancel_requested, partial, completed, failed, timeout, cancelled | 请求取消、部分完成、完成、失败、超时或取消 |
| task | cancel_requested | cancelled, completed, failed | 取消成功、已完成不可取消或取消失败 |
| interrupt | required | accepted, rejected, expired | 用户确认、拒绝或超时 |
| interrupt | accepted | resolved | run 已恢复并写入确认结果 |
| interrupt | rejected | resolved | run 已取消或分支已终止 |
| interrupt | expired | resolved | 过期处理完成 |

## Run 并发和 Session 归档

- 同一 `session_id` 同一时间只允许一个 active run。
- active run 包括 `pending`、`running`、`waiting_confirmation`、`waiting_input`、`resuming` 和 `cancelling`。
- 新建 run 前必须查询同 session active run；存在时返回 `RUN_STATE_CONFLICT`，不得隐式并发执行。
- `agent_sessions.status=archived` 是 Agent Runtime 只读标记，不代表业务项目真实归档状态。
- 项目归档事实只来自业务 RPC；Agent session 归档或恢复前必须重新校验项目权限。
- archived session 允许读取历史消息、事件和 snapshot；不允许创建 run、追加输入、确认中断或重试。

## 索引

| 表 | 索引 / 约束 | 用途 |
| --- | --- | --- |
| agent_sessions | `ux_agent_sessions_idempotency`、`idx_agent_sessions_space_project_user_updated`、`idx_agent_sessions_deleted` | 创建幂等、项目下会话列表、软删除清理 |
| agent_runs | `ux_agent_runs_idempotency`、`idx_agent_runs_session_created`、`idx_agent_runs_project_status_updated` | 创建幂等、会话 run 列表、状态查询 |
| agent_messages | `ux_agent_messages_session_sequence`、`idx_agent_messages_run_sequence` | 消息恢复和 run 内消息读取 |
| agent_events | `ux_agent_events_run_sequence`、`idx_agent_events_run_created`、`idx_agent_events_type` | SSE 补偿、Last-Event-ID 映射和类型排障 |
| agent_tool_calls | `idx_agent_tool_calls_run_tool`、`idx_agent_tool_calls_status_updated`、`idx_agent_tool_calls_idempotency` | Tool 状态、重试和幂等查询 |
| agent_tasks | `idx_agent_tasks_run_status`、`idx_agent_tasks_status_updated` | 长任务状态推进和清理 |
| agent_interrupts | `idx_agent_interrupts_run_status`、`idx_agent_interrupts_expires_status`、`idx_agent_interrupts_idempotency` | 确认恢复、过期扫描和幂等处理 |
| agent_artifacts | `idx_agent_artifacts_session_type_updated`、`idx_agent_artifacts_project_type_updated` | 黑板、草稿和项目内产物读取 |
| agent_safety_evaluations | `idx_agent_safety_source_run_target`、`idx_agent_safety_expires` | 安全证据排障和过期扫描 |
| agent_memories | `idx_agent_memories_user_scope_updated`、`idx_agent_memories_expires` | 授权记忆读取和过期清理 |
| agent_runtime_configs | `idx_agent_runtime_configs_status` | 运行配置激活态查询 |

## CRUD

- Create：写入必须幂等，`agent_events` 使用 `event_id` 去重。
- Read：列表查询分页，默认 10，避免逐条关联查询。
- Update：状态迁移必须校验当前状态。
- Delete / Archive：优先软删除或归档。
- 幂等规则：run、event、interrupt、task 均要支持重复请求幂等处理。

## 数据保留周期

| 数据 | 保留策略 |
| --- | --- |
| session / message / artifact snapshot | 默认保留 180 天；用户删除或合规清理时软删除。 |
| run / task / interrupt / tool_call | 默认保留 180 天；排障期后可归档或脱敏。 |
| AG-UI event | terminal run 后保留 30 天用于 replay；超过窗口必须走 snapshot fallback。 |
| safety evaluation | 至少保留到 `expires_at + 30 天`；最长 180 天，之后只保留审计所需摘要。 |
| memory | 第一版启用；仅 `authorized=true` 可用于后续 run，必须设置 `expires_at` 或按 180 天清理。 |
| runtime config | 长期保留 active/deprecated 版本，用于回放和审计。 |

## Memory 启用策略

- `agent_memories` 第一版建表并启用，但默认只写 session summary memory。
- user / space preference memory 必须有显式授权，`authorized=false` 的记录不得参与检索。
- memory 只保存 `content_summary`，不得保存业务事实、完整 Prompt、私有素材正文、供应商响应或用户敏感明文。
- 用户撤销授权后，新 run 不得检索对应 user / space memory；历史记录按保留周期清理或脱敏。

## 和业务数据库的边界

- 不保存业务事实：积分余额、积分流水、最终资产、作品、企业成员、业务权限。
- 不复制业务主数据：项目标题、作品公开状态等通过业务 API/RPC 查询。
- 业务写操作通过 RPC：资产保存、扣费、项目创建、作品分享都走业务微服务。
- 测试验证方式：Agent DB 和业务 DB 分开断言。

## 项目归档边界

- 项目 `archived` 是业务事实，Agent 创建 session/run、resume、retry、confirm 和保存生成资产前必须通过 RPC 校验。
- Agent 可以把 session 标记为 `archived` 表示 Runtime 只读，但不能把它作为项目真实状态来源。
- `archived` 项目允许读取历史 session、run、message、event、snapshot；不允许创建新 run 或继续生成。
- 运行中发现项目归档时，Agent 停止发起新 Tool，释放未结算冻结积分，并写入取消状态和 AG-UI 事件。

## 内容安全证据边界

- `agent_safety_evaluations` 保存 `safety_evidence_id`、`scene`、`result`、`target_type`、`evaluated_object_digest`、`policy_version`、`evidence_version`、`evaluated_at`、`expires_at`、`trace_id`、`source_session_id`、`source_run_id`、`source_artifact_id`。
- Agent 内部可保存 `evaluator_config_version`、`model_snapshot_ref`、`prompt_assembly_ref`、`latency_ms`、`attempt_count`、`error_class` 和脱敏摘要，用于排障。
- 不保存系统 Prompt、完整组装 Prompt、供应商原始响应、推理链路、内部评分、命中规则细节、绕过提示和 API Key。
- 写业务 RPC 时只传 `safety_evidence` 脱敏摘要，业务侧二次校验证据和写入对象是否匹配。

## TOS 与公开媒体边界

- Agent 不签发 TOS 上传凭证，不生成公开快照，不保存 TOS AK/SK 或上传签名。
- `agent_artifacts` 的最终资产引用只保存 `artifact_type=asset_ref`、`status=final_ref`、`business_ref_id=asset_id`、`source_run_id` 和脱敏展示摘要。
- TOS object key 和公共 URL 属于业务资产事实，由业务微服务保存和授权返回。
- 前端预览、下载和公开访问必须通过业务 API 获取授权后的 TOS 公共 URL，不能从 Agent 事件中读取长期媒体 URL。

## 后续证据

- 需要补充 migration 本地 up/down 执行证据。
- 需要补充 Agent DB 查询性能和 query count 报告。
- 需要补充 Agent DB / 业务 DB 边界扫描报告。
