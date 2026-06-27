# Agent 领域数据模型设计

状态：archived
owner：Agent 服务责任域
更新时间：2026-06-28
适用范围：智能体微服务 Agent Runtime PostgreSQL 数据模型草案
相关代码路径：`/services/agent/**`、`/db/migrations/iterations/**`
相关契约：`docs/standards/Agent领域数据建模规范.md`、`docs/architecture/00-智能体微服务总体架构设计.md`、`docs/architecture/01-Eino能力选型与TurnLoop设计.md`

## 领域边界

Agent 领域数据库只保存 Agent Runtime 数据，包括会话、运行、消息、事件、Tool 调用、任务、中断、草稿产物、记忆和运行配置。

Agent 领域数据库不保存订单、支付、积分账户、积分流水、项目事实、资产事实、Skill 审核结果、企业成员关系、业务权限、作品分享状态、模型供应商密钥、业务主数据等业务事实。业务事实必须通过 RPC 调用业务微服务产生并维护。

## 数据库约束

- 使用 PostgreSQL + GORM + golang-migrate。
- 本阶段只做表设计草案，不创建 migration。
- 建表一律不添加数据库级外键约束，也不通过关联键表达表关联。
- `project_id`、`session_id`、`run_id`、`task_id`、`user_id`、`tenant_id`、`space_id` 等只作为普通字段、查询条件、审计字段或应用层一致性校验依据。
- 跨表一致性通过业务流程、幂等键、唯一约束、必要索引、application 校验和测试保证。
- 列表查询默认 `page_size=10`，必须定义上限；查询路径需要避免 `for` 循环逐条查询。

## 表清单

| 表名 | 用途 | 数据范围和清洗口径 |
| --- | --- | --- |
| `agent_sessions` | 工作台会话 | 按用户、空间和会话保留策略清理 |
| `agent_runs` | 一次 Agent 执行 | 按 session 和创建时间保留 |
| `agent_messages` | 用户、Agent、系统消息 | 按会话保留，敏感内容按策略脱敏或摘要化 |
| `agent_events` | 内部事件和 AG-UI 事件 | 按补偿窗口和排障周期保留 |
| `agent_tool_calls` | Tool 调用记录 | 按 run 和 tool 保留，脱敏输入输出摘要 |
| `agent_tasks` | 长任务和生成任务状态 | 按任务终态和保留周期清理 |
| `agent_interrupts` | 中断和恢复状态 | 按 run 和中断状态保留 |
| `agent_artifacts` | 黑板、草稿元素、过程快照、资产引用元数据 | 按 session/run 保留，不保存业务资产事实 |
| `agent_memories` | 用户授权记忆和会话摘要 | 按用户授权、隐私和保留策略清理 |
| `agent_runtime_configs` | Agent/Skill/Tool/Workflow/模型参数运行配置快照 | 按配置版本和审计要求保留 |

## 公共字段建议

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 业务主键，非外键 |
| `tenant_id` | string | 是 | 租户或平台空间标识 |
| `space_id` | string | 是 | 当前空间标识，普通字段 |
| `project_id` | string | 否 | 业务项目引用，普通字段，不保存项目事实 |
| `user_id` | string | 是 | 当前操作用户，普通字段 |
| `trace_id` | string | 是 | 链路追踪 |
| `created_at` | timestamptz | 是 | 创建时间 |
| `updated_at` | timestamptz | 是 | 更新时间 |
| `created_by` | string | 否 | 创建者 |
| `updated_by` | string | 否 | 更新者 |
| `deleted_at` | timestamptz | 否 | 软删除或归档标记 |

## 表字段草案

### agent_sessions

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 会话 ID |
| `tenant_id` | string | 是 | idx | 租户 |
| `space_id` | string | 是 | idx | 当前空间 |
| `project_id` | string | 是 | idx | 所属业务项目，普通字段 |
| `user_id` | string | 是 | idx | 会话创建者 |
| `status` | string | 是 | idx | `active`、`archived`、`expired` |
| `title` | string | 否 |  | 会话展示标题 |
| `last_run_id` | string | 否 | idx | 最近 run 标识，普通字段 |
| `last_event_sequence` | int64 | 是 |  | 最近事件序号 |
| `snapshot_summary` | jsonb | 否 |  | 会话恢复摘要 |
| `metadata` | jsonb | 否 |  | 非业务事实扩展 |

### agent_runs

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | run ID |
| `session_id` | string | 是 | idx | 所属会话，普通字段 |
| `project_id` | string | 是 | idx | 所属业务项目，普通字段 |
| `tenant_id` / `space_id` / `user_id` | string | 是 | idx | 运行上下文 |
| `turn_no` | int64 | 是 | idx | 会话内轮次 |
| `status` | string | 是 | idx | `pending`、`running`、`waiting_confirmation`、`resuming`、`completed`、`failed`、`cancelled` |
| `input_summary` | jsonb | 否 |  | 用户输入摘要和素材引用摘要 |
| `skill_selection` | jsonb | 否 |  | Skill 路由结果，不保存业务 Skill 事实 |
| `model_selection_snapshot` | jsonb | 否 |  | 用户可见模型名、模型类型、计价快照 ID |
| `runtime_config_version` | string | 否 | idx | Agent 配置版本 |
| `idempotency_key` | string | 是 | uniq | 创建 run 幂等键 |
| `error_code` | string | 否 | idx | 失败错误码 |
| `error_message` | string | 否 |  | 用户可理解错误摘要 |
| `started_at` | timestamptz | 否 |  | 开始时间 |
| `completed_at` | timestamptz | 否 |  | 完成时间 |

### agent_messages

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 消息 ID |
| `session_id` | string | 是 | idx | 会话 ID |
| `run_id` | string | 否 | idx | 关联 run，普通字段 |
| `role` | string | 是 | idx | `user`、`assistant`、`system`、`tool` |
| `content_type` | string | 是 |  | `text`、`json`、`asset_ref` 等 |
| `content` | text/jsonb | 否 |  | 用户可见内容或结构化内容 |
| `content_summary` | string | 否 |  | 摘要，供历史检索 |
| `sequence` | int64 | 是 | idx | 会话内消息顺序 |
| `safety_status` | string | 否 | idx | 安全评估状态摘要 |
| `metadata` | jsonb | 否 |  | 素材引用、展示状态等 |

### agent_events

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 事件记录 ID |
| `event_id` | string | 是 | uniq | 全局事件 ID，重放不变 |
| `project_id` | string | 是 | idx | 所属业务项目，普通字段 |
| `session_id` | string | 是 | idx | 会话 ID |
| `run_id` | string | 是 | idx | run ID |
| `sequence` | int64 | 是 | uniq(run_id, sequence) | run 内单调递增 |
| `event_type` | string | 是 | idx | AG-UI 事件类型 |
| `component` | string | 否 | idx | 建议消费组件 |
| `payload` | jsonb | 是 |  | 前端可展示 payload |
| `payload_schema_version` | string | 是 |  | 兼容版本 |
| `visibility` | string | 是 | idx | `public`、`internal` |
| `occurred_at` | timestamptz | 是 | idx | 事件发生时间 |

### agent_tool_calls

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | Tool call ID |
| `run_id` | string | 是 | idx | run ID |
| `task_id` | string | 否 | idx | 关联长任务 |
| `tool_name` | string | 是 | idx | Tool 名称 |
| `tool_type` | string | 是 | idx | 模型、文件、资产、业务 RPC 等 |
| `risk_level` | string | 是 | idx | `low`、`medium`、`high` |
| `status` | string | 是 | idx | `pending`、`running`、`succeeded`、`failed`、`timeout`、`cancelled` |
| `input_summary` | jsonb | 否 |  | 脱敏输入摘要 |
| `output_summary` | jsonb | 否 |  | 脱敏输出摘要 |
| `idempotency_key` | string | 否 | idx | RPC 写入或生成任务幂等 |
| `timeout_ms` | int64 | 否 |  | 调用超时 |
| `retry_count` | int | 是 |  | 已重试次数 |
| `error_code` | string | 否 | idx | 错误码 |
| `latency_ms` | int64 | 否 |  | 耗时 |

### agent_tasks

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 任务 ID |
| `run_id` | string | 是 | idx | run ID |
| `task_type` | string | 是 | idx | 图片、音乐、视频、资产保存等 |
| `status` | string | 是 | idx | `pending`、`running`、`completed`、`failed`、`cancelled` |
| `progress_percent` | int | 否 |  | 0 到 100 |
| `progress_detail` | jsonb | 否 |  | 队列、运行、部分完成等展示信息 |
| `cancel_requested` | bool | 是 | idx | 是否请求取消 |
| `started_at` | timestamptz | 否 |  | 开始时间 |
| `finished_at` | timestamptz | 否 |  | 结束时间 |
| `error_code` | string | 否 | idx | 错误码 |

### agent_interrupts

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 中断 ID |
| `run_id` | string | 是 | idx | run ID |
| `interrupt_type` | string | 是 | idx | `credit_confirmation`、`risk_confirmation`、`business_write`、`additional_input` |
| `status` | string | 是 | idx | `required`、`accepted`、`rejected`、`expired`、`resolved` |
| `reason` | string | 是 |  | 用户可理解原因 |
| `confirmation_payload` | jsonb | 是 |  | 确认面板 payload |
| `allowed_actions` | jsonb | 是 |  | `accept`、`reject`、`modify` 等 |
| `resume_context` | jsonb | 是 |  | 恢复上下文，不含业务密钥 |
| `idempotency_key` | string | 否 | idx | resume 幂等键 |
| `expires_at` | timestamptz | 否 | idx | 过期时间 |
| `resolved_at` | timestamptz | 否 |  | 解决时间 |

### agent_artifacts

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 产物记录 ID |
| `session_id` | string | 是 | idx | 会话 ID |
| `project_id` | string | 是 | idx | 所属业务项目，普通字段 |
| `run_id` | string | 否 | idx | run ID |
| `artifact_type` | string | 是 | idx | `blackboard`、`draft_element`、`asset_ref`、`snapshot` |
| `status` | string | 是 | idx | `draft`、`final_ref`、`failed`、`archived` |
| `element_type` | string | 否 | idx | 平台内置资产元素类型 |
| `content` | jsonb | 是 |  | 草稿内容、黑板结构或快照 |
| `business_ref_id` | string | 否 | idx | 业务资产 ID 或引用 ID，普通字段 |
| `visibility` | string | 是 | idx | `session_private`、`frontend_visible` |
| `version` | int | 是 |  | 版本号 |

### agent_memories

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 记忆 ID |
| `tenant_id` / `space_id` / `user_id` | string | 是 | idx | 授权范围 |
| `memory_type` | string | 是 | idx | `session_summary`、`user_preference` |
| `scope` | string | 是 | idx | `session`、`user`、`space` |
| `content_summary` | text | 是 |  | 摘要内容 |
| `source_session_id` | string | 否 | idx | 来源会话，普通字段 |
| `authorized` | bool | 是 | idx | 是否授权 |
| `expires_at` | timestamptz | 否 | idx | 过期时间 |

### agent_runtime_configs

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | pk | 配置 ID |
| `config_key` | string | 是 | idx | Agent、Tool、Skill、Workflow 配置 key |
| `version` | string | 是 | uniq(config_key, version) | 配置版本 |
| `status` | string | 是 | idx | `draft`、`active`、`deprecated` |
| `owner` | string | 是 |  | 配置 owner |
| `content` | jsonb | 是 |  | 非敏感配置内容 |
| `safe_config_refs` | jsonb | 否 |  | 安全配置 key 引用，不保存密钥 |
| `activated_at` | timestamptz | 否 | idx | 生效时间 |
| `deprecated_at` | timestamptz | 否 | idx | 废弃时间 |

## 状态机

| 对象 | 状态 | 可迁移到 | 触发条件 |
| --- | --- | --- | --- |
| session | active | archived、expired | 用户归档、保留期到期 |
| session | archived | active | 用户恢复，需权限校验 |
| run | pending | running | 开始执行 |
| run | running | waiting_confirmation、completed、failed、cancelled | 需要确认、成功、失败、取消 |
| run | waiting_confirmation | resuming、cancelled、failed | 确认、拒绝、过期 |
| run | resuming | running、failed | 恢复成功或失败 |
| task | pending | running、cancelled | 开始或取消 |
| task | running | completed、failed、cancelled | 完成、失败或取消 |
| interrupt | required | accepted、rejected、expired | 用户确认、拒绝或过期 |
| interrupt | accepted | resolved | 恢复上下文已消费 |

## 索引建议

| 表 | 索引字段 | 用途 |
| --- | --- | --- |
| `agent_sessions` | `(tenant_id, user_id, status, updated_at)` | 会话列表 |
| `agent_sessions` | `(space_id, user_id, updated_at)` | 当前空间历史 |
| `agent_sessions` | `(space_id, project_id, updated_at)` | 项目下会话列表 |
| `agent_runs` | `(session_id, created_at)` | 会话下 run 查询 |
| `agent_runs` | `(project_id, session_id, created_at)` | 项目下 run 查询 |
| `agent_runs` | `(status, updated_at)` | 异常和长任务巡检 |
| `agent_runs` | `(idempotency_key)` unique | run 创建幂等 |
| `agent_messages` | `(session_id, sequence)` | 消息恢复 |
| `agent_events` | `(run_id, sequence)` unique | 事件补偿和顺序 |
| `agent_events` | `(project_id, run_id, sequence)` | 项目上下文事件补偿 |
| `agent_events` | `(event_id)` unique | 事件幂等去重 |
| `agent_events` | `(run_id, event_type, occurred_at)` | 事件排障 |
| `agent_tool_calls` | `(run_id, tool_name)` | Tool 调用历史 |
| `agent_tool_calls` | `(status, updated_at)` | 超时任务巡检 |
| `agent_tasks` | `(status, updated_at)` | 长任务查询 |
| `agent_interrupts` | `(run_id, status)` | 当前中断查询 |
| `agent_interrupts` | `(expires_at, status)` | 过期扫描 |
| `agent_artifacts` | `(session_id, artifact_type, updated_at)` | 黑板和资产引用恢复 |
| `agent_artifacts` | `(project_id, artifact_type, updated_at)` | 项目黑板和资产引用聚合 |
| `agent_memories` | `(user_id, scope, updated_at)` | 授权记忆检索 |
| `agent_runtime_configs` | `(config_key, version)` unique | 配置版本 |

## CRUD 设计原则

- Create：所有写入携带 `trace_id` 和必要 `idempotency_key`，重复请求返回同一 Runtime 结果或明确幂等冲突。
- Read：会话、消息、事件、任务、产物引用列表必须分页，默认 `page_size=10`；事件补偿 API 后续可在契约中定义上限。
- Update：状态迁移必须通过 application 层校验，不允许任意跳转。
- Delete / Archive：优先软删除、归档或按保留策略清理，不物理删除仍处于补偿窗口的数据。
- Batch：恢复会话时批量读取消息、事件、黑板和任务，避免逐条查询。

## 幂等规则

| 场景 | 幂等字段 | 行为 |
| --- | --- | --- |
| 创建 session | `idempotency_key` | 重复返回同一 session |
| 创建 run | `idempotency_key` | 重复返回同一 run 和当前状态 |
| 写入事件 | `event_id`、`run_id + sequence` | 重复写忽略或返回已存在 |
| Tool 写业务 RPC | `idempotency_key` | 由业务服务返回同一结果或幂等冲突 |
| resume interrupt | `interrupt_id + idempotency_key` | 重复确认不重复执行业务写 |
| cancel task | `task_id + idempotency_key` | 重复取消返回当前取消状态 |

## 分页与查询路径

- 会话列表：按 `updated_at desc`，默认 10 条；项目详情内可按 `project_id` 过滤。
- 消息列表：按 `sequence asc`，默认 10 条。
- 事件补偿：按 `run_id + sequence asc`，默认 10 条；如果正式契约允许更大窗口，需写明上限。
- 任务列表：按 `updated_at desc`，默认 10 条。
- 黑板/产物引用：按 `updated_at desc` 或 `version desc`，默认 10 条。
- 记忆列表：按 `updated_at desc`，默认 10 条。

## 数据保留

| 数据 | 建议保留策略 |
| --- | --- |
| 事件 | 至少覆盖断线补偿窗口和排障周期；超过后保留快照摘要 |
| 消息 | 按用户会话保留策略；必要时生成摘要用于检索 |
| Tool 调用 | 保留脱敏摘要和错误信息，不保留供应商原始响应全文 |
| 黑板草稿 | 跟随 session 保留，用户归档或过期后清理 |
| 资产引用 | 只保留业务资产引用 ID 和摘要，业务资产由业务服务控制 |
| 记忆 | 按用户授权和隐私要求保留，可撤销和过期 |
| 配置 | 保留版本、owner、审计和回滚信息 |

## 和业务数据库的边界

- 不保存积分账户、积分批次、冻结、扣减、释放和流水事实。
- 不保存项目标题、封面、归档状态、项目权限和项目资产关系事实；只保存 `project_id` 普通引用。
- 不保存业务资产文件、最终资产元素、资产权限、预览下载授权事实。
- 不保存 Skill 审核状态最终事实，只保存运行时使用的配置快照或引用。
- 不保存企业成员关系、用户账号状态、平台管理员审计事实。
- 不保存作品、精选作品、点赞、公开快照和下架状态。
- 保存 `business_ref_id` 时仅作为普通引用和恢复上下文，不作为业务事实来源。

## 测试策略

- CRUD：每张表创建、读取、分页、更新、归档路径。
- 状态机：session、run、task、interrupt 合法迁移和非法迁移拒绝。
- 幂等：run 创建、事件写入、resume、cancel、Tool 调用记录重复请求。
- 索引查询：会话历史、事件补偿、任务巡检、interrupt 查询。
- 数据保留：事件过补偿窗口后的快照恢复，归档数据不影响新 run。
- 边界违规检查：schema 无数据库级外键约束；Agent 表不出现业务事实字段。
- 项目边界：Agent 表只保存 `project_id` 引用，项目事实必须通过业务 RPC 校验。
- 分页和批量：列表默认 10 条，恢复会话批量读取，不在循环中逐条查询。
- 脱敏：事件 payload、Tool 摘要和日志不包含密钥、系统提示词、供应商原始响应、内部成本。

## 验收标准

- 已覆盖核心 Agent Runtime 表清单。
- 已定义字段草案、状态机、索引、CRUD、分页、幂等、保留策略。
- 已明确不添加数据库级外键约束。
- 已明确 Agent DB 与业务 DB 边界。
- 已明确测试策略，不进入 migration 实现。

## 风险与待确认

- 字段类型和索引需要在实现阶段结合具体 PostgreSQL/GORM 约定进一步收敛。
- 事件补偿查询默认分页 10 是否满足高频流式输出，需要 API 契约确认 page_size 上限。
- 黑板草稿与业务最终资产元素的字段边界需要前端和业务微服务共同确认。
- `project_id` 在 AG-UI 公共字段、Agent API 和业务 Project RPC 中的命名与必填口径需要正式契约确认。
- `type` 与 `event_type` 字段命名需在 AG-UI 正式契约中统一。
