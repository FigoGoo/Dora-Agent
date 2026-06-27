# 03-Agent领域模型状态机与数据库设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：Agent Runtime model、数据库表、Repository、CRUD、状态机、索引和数据保留
相关代码路径：`services/agent/internal/domain/**`、`services/agent/internal/infra/repository/**`、`db/migrations/iterations/**`
相关契约：`docs/architecture/02-Agent领域数据模型设计.md`、`db/migrations/iterations/20260627_agent_runtime/agent/001_create_agent_runtime.up.sql`

## 文档目标

- 定义 Agent DB 只保存 Agent Runtime 数据。
- 设计所有 Agent model、表、状态机和查询路径。
- 为 migration、GORM model、Repository 和测试提供实现依据。
- 所有 Go model 字段必须使用中文注释说明字段含义。

## 模型范围

- `AgentSession`
- `AgentRun`
- `AgentMessage`
- `AgentEvent`
- `AgentToolCall`
- `AgentTask`
- `AgentInterrupt`
- `AgentArtifact`
- `AgentSafetyEvaluation`
- `AgentMemory`
- `AgentRuntimeConfig`

## Go Model 设计示例

所有最终 Go model 字段必须使用中文注释。以下为实现时的字段基线，详细字段以本文件字段表和 migration 为准：

```go
// AgentRun 表示一次 Agent 执行，保存 Runtime 状态，不保存业务事实。
type AgentRun struct {
    ID                     string         `gorm:"primaryKey"` // Run 唯一标识。
    SessionID              string         `gorm:"index"`      // 所属会话 ID，普通字段，不是外键。
    ProjectID              string         `gorm:"index"`      // 业务项目引用，只用于权限校验和恢复。
    TenantID               string         `gorm:"index"`      // 租户或平台范围标识。
    SpaceID                string         `gorm:"index"`      // 当前个人空间或企业空间。
    UserID                 string         `gorm:"index"`      // 当前操作用户。
    TurnNo                 int64          `gorm:"index"`      // 会话内轮次。
    Status                 string         `gorm:"index"`      // pending/running/waiting_confirmation/resuming/completed/failed/cancelled。
    InputSummary           datatypes.JSON // 用户输入、素材引用和控件输入的脱敏摘要。
    SkillSelection         datatypes.JSON // Skill 路由结果和公开摘要。
    ModelSelectionSnapshot datatypes.JSON // 模型选择快照，只包含展示名和计价快照引用。
    RuntimeConfigVersion   string         `gorm:"index"` // Agent Runtime 配置版本。
    IdempotencyKey         string         `gorm:"uniqueIndex"` // 创建 run 幂等键。
    ErrorCode              string         `gorm:"index"` // 失败错误码。
    ErrorMessage           string         // 用户可理解错误摘要。
    TraceID                string         `gorm:"index"` // 链路追踪 ID。
    StartedAt              *time.Time     // 开始执行时间。
    CompletedAt            *time.Time     // 结束时间。
    CreatedAt              time.Time      // 创建时间。
    UpdatedAt              time.Time      // 更新时间。
    DeletedAt              gorm.DeletedAt `gorm:"index"` // 软删除时间。
}
```

## 状态机

| 对象 | 状态 | 可迁移到 | 触发函数 |
| --- | --- | --- | --- |
| session | `active` | `archived`、`expired` | `ArchiveSession`、清理任务 |
| session | `archived` | `active` | `RestoreSession`，需业务权限校验 |
| run | `pending` | `running` | `StartRun` |
| run | `running` | `waiting_confirmation`、`completed`、`failed`、`cancelled` | `CreateInterrupt`、`CompleteRun`、`FailRun`、`CancelRun` |
| run | `waiting_confirmation` | `resuming`、`cancelled`、`failed` | `AcceptInterrupt`、`RejectInterrupt`、`ExpireInterrupt` |
| run | `resuming` | `running`、`failed` | `ResumeTurnLoop` |
| task | `pending` | `running`、`cancelled` | `StartTask`、`CancelTask` |
| task | `running` | `completed`、`failed`、`cancelled` | `CompleteTask`、`FailTask`、`CancelTask` |
| interrupt | `required` | `accepted`、`rejected`、`expired` | `AcceptInterrupt`、`RejectInterrupt`、`ExpireInterrupt` |
| interrupt | `accepted` | `resolved` | `ConsumeResumeContext` |

## Agent DB 实现要求

| 主题 | 要求 |
| --- | --- |
| 数据边界 | 只保存 Agent Runtime 数据和业务引用 ID，不保存业务事实。 |
| 审计字段 | 每张表包含 `created_at`、`updated_at`；需要软删除的表包含 `deleted_at`。 |
| 外键约束 | SQL 不添加 `FOREIGN KEY` 和 `REFERENCES`，跨表一致性由 Repository、状态机和测试保证。 |
| 幂等 | 创建 session/run、interrupt 处理、事件写入、资产提交引用都必须有幂等键或唯一约束。 |
| 分页 | 列表查询默认 10 条，上限由 API 或 Repository 常量限制。 |
| 批量 | 资产引用、事件补偿、任务查询必须支持批量读取，避免循环逐条查库。 |
| 保留 | 事件和 snapshot 在生产级实现中按 run 完整保留；清理策略按配置执行，不删除业务事实。 |

## SQL 字段类型基线

迁移脚本使用 PostgreSQL。所有 ID 字段在生产级实现中使用 `varchar(64)`，时间使用 `timestamptz`，结构化摘要使用 `jsonb`，不创建数据库级外键。

| 表 | 字段与类型 | 约束和索引 |
| --- | --- | --- |
| `agent_sessions` | `id varchar(64) pk`、`tenant_id varchar(64) not null`、`space_id varchar(64) not null`、`project_id varchar(64) not null`、`user_id varchar(64) not null`、`status varchar(32) not null default 'active'`、`title varchar(128) not null default ''`、`last_run_id varchar(64) not null default ''`、`last_event_sequence bigint not null default 0`、`snapshot_summary jsonb not null default '{}'`、`idempotency_key varchar(160) not null`、`trace_id varchar(128) not null`、`created_at/updated_at timestamptz not null`、`deleted_at timestamptz null` | `unique(idempotency_key)`、`idx_agent_sessions_space_project_user_updated(space_id, project_id, user_id, updated_at desc)`、`idx_agent_sessions_deleted(deleted_at)` |
| `agent_runs` | `id varchar(64) pk`、`session_id varchar(64) not null`、`project_id varchar(64) not null`、`space_id varchar(64) not null`、`user_id varchar(64) not null`、`turn_no bigint not null`、`status varchar(32) not null`、`input_summary jsonb not null default '{}'`、`skill_selection jsonb not null default '{}'`、`model_selection_snapshot jsonb not null default '{}'`、`runtime_config_version varchar(64) not null`、`idempotency_key varchar(160) not null`、`error_code varchar(64) not null default ''`、`error_message varchar(512) not null default ''`、`trace_id varchar(128) not null`、`started_at/completed_at timestamptz null`、审计字段 | `unique(idempotency_key)`、`idx_agent_runs_session_created(session_id, created_at desc)`、`idx_agent_runs_project_status_updated(project_id, status, updated_at desc)` |
| `agent_messages` | `id varchar(64) pk`、`session_id varchar(64) not null`、`run_id varchar(64) not null`、`role varchar(32) not null`、`content_type varchar(32) not null`、`content text not null default ''`、`content_summary jsonb not null default '{}'`、`sequence bigint not null`、`safety_status varchar(32) not null default 'unchecked'`、`metadata jsonb not null default '{}'`、`trace_id varchar(128) not null`、审计字段 | `unique(session_id, sequence)`、`idx_agent_messages_run(run_id, sequence)` |
| `agent_events` | `event_id varchar(64) pk`、`type varchar(96) not null`、`session_id/run_id/project_id/space_id/actor_user_id varchar(64) not null`、`sequence bigint not null`、`component varchar(64) not null`、`payload jsonb not null default '{}'`、`payload_schema_version varchar(32) not null`、`visibility varchar(32) not null default 'frontend'`、`trace_id varchar(128) not null`、`created_at timestamptz not null` | `unique(run_id, sequence)`、`idx_agent_events_run_created(run_id, created_at)`、`idx_agent_events_type(type)` |
| `agent_tool_calls` | `id varchar(64) pk`、`run_id varchar(64) not null`、`task_id varchar(64) not null default ''`、`tool_name varchar(96) not null`、`tool_type varchar(48) not null`、`risk_level varchar(32) not null default 'low'`、`status varchar(32) not null`、`input_summary/output_summary jsonb not null default '{}'`、`idempotency_key varchar(160) not null default ''`、`timeout_ms integer not null default 0`、`retry_count integer not null default 0`、`error_code varchar(64) not null default ''`、`latency_ms bigint not null default 0`、`trace_id varchar(128) not null`、审计字段 | `idx_agent_tool_calls_run_tool(run_id, tool_name)`、`idx_agent_tool_calls_status_updated(status, updated_at desc)`、`idx_agent_tool_calls_idempotency(idempotency_key)` |
| `agent_tasks` | `id varchar(64) pk`、`run_id varchar(64) not null`、`task_type varchar(48) not null`、`resource_type varchar(32) not null default ''`、`status varchar(32) not null`、`progress_percent integer not null default 0`、`progress_detail jsonb not null default '{}'`、`cancel_requested boolean not null default false`、`external_task_ref varchar(256) not null default ''`、`error_code varchar(64) not null default ''`、`started_at/completed_at timestamptz null`、`trace_id varchar(128) not null`、审计字段 | `idx_agent_tasks_run_status(run_id, status)`、`idx_agent_tasks_status_updated(status, updated_at desc)` |
| `agent_interrupts` | `id varchar(64) pk`、`run_id varchar(64) not null`、`interrupt_type varchar(48) not null`、`status varchar(32) not null`、`reason varchar(128) not null default ''`、`confirmation_payload jsonb not null default '{}'`、`allowed_actions jsonb not null default '[]'`、`resume_context jsonb not null default '{}'`、`idempotency_key varchar(160) not null default ''`、`expires_at timestamptz not null`、`resolved_at timestamptz null`、`trace_id varchar(128) not null`、审计字段 | `idx_agent_interrupts_run_status(run_id, status)`、`idx_agent_interrupts_expires_status(expires_at, status)`、`idx_agent_interrupts_idempotency(id, idempotency_key)` |
| `agent_artifacts` | `id varchar(64) pk`、`session_id/project_id/run_id varchar(64) not null`、`artifact_type varchar(48) not null`、`status varchar(32) not null`、`element_type varchar(48) not null default ''`、`content jsonb not null default '{}'`、`business_ref_id varchar(64) not null default ''`、`visibility varchar(32) not null default 'private'`、`version integer not null default 1`、`trace_id varchar(128) not null`、审计字段 | `idx_agent_artifacts_session_type_updated(session_id, artifact_type, updated_at desc)`、`idx_agent_artifacts_project_type_updated(project_id, artifact_type, updated_at desc)` |
| `agent_safety_evaluations` | `safety_evidence_id varchar(64) pk`、`scene varchar(48) not null`、`target_type varchar(48) not null`、`target_ref_id varchar(64) not null default ''`、`evaluated_object_digest varchar(128) not null`、`policy_version varchar(64) not null`、`evidence_version varchar(32) not null`、`result varchar(32) not null`、`user_visible_reason varchar(512) not null default ''`、`source_session_id/source_run_id/source_artifact_id varchar(64) not null default ''`、`trace_id varchar(128) not null`、`evaluated_at/expires_at timestamptz not null`、审计字段 | `unique(safety_evidence_id)`、`idx_agent_safety_source_run_target(source_run_id, target_type)`、`idx_agent_safety_expires(expires_at)` |
| `agent_memories` | `id varchar(64) pk`、`user_id varchar(64) not null`、`space_id varchar(64) not null`、`memory_type varchar(48) not null`、`scope varchar(48) not null`、`content_summary jsonb not null default '{}'`、`authorized boolean not null default false`、`expires_at timestamptz null`、`source_session_id varchar(64) not null default ''`、`trace_id varchar(128) not null`、审计字段 | `idx_agent_memories_user_scope_updated(user_id, scope, updated_at desc)`、`idx_agent_memories_expires(expires_at)` |
| `agent_runtime_configs` | `config_key varchar(96) not null`、`version varchar(64) not null`、`status varchar(32) not null`、`owner varchar(64) not null`、`content jsonb not null default '{}'`、`safe_config_refs jsonb not null default '[]'`、`activated_at/deprecated_at timestamptz null`、`created_at/updated_at timestamptz not null` | `primary key(config_key, version)`、`idx_agent_runtime_configs_status(status)` |

## 字段级设计必须覆盖

字段设计按以下粒度实现 GORM model 和 migration：

| 表 / Model | 必须字段组 | 唯一约束 / 索引 | 业务边界 |
| --- | --- | --- | --- |
| `agent_sessions` / `AgentSession` | `id`、`tenant_id`、`space_id`、`project_id`、`user_id`、`status`、`title`、`last_run_id`、`last_event_sequence`、`snapshot_summary`、审计字段 | `(space_id, project_id, user_id, updated_at)`、`idempotency_key` 唯一 | 只保存项目引用和会话恢复摘要，不保存项目标题事实来源。 |
| `agent_runs` / `AgentRun` | `id`、`session_id`、`project_id`、`turn_no`、`status`、`input_summary`、`skill_selection`、`model_selection_snapshot`、`runtime_config_version`、`idempotency_key`、错误字段、时间字段 | `(session_id, created_at)`、`(project_id, status, updated_at)`、`idempotency_key` 唯一 | 模型快照只存用户可见名和计价快照引用，不存供应商密钥。 |
| `agent_messages` / `AgentMessage` | `id`、`session_id`、`run_id`、`role`、`content_type`、`content`、`content_summary`、`sequence`、`safety_status`、`metadata` | `(session_id, sequence)` | 不保存系统 Prompt、内部推理链路。 |
| `agent_events` / `AgentEvent` | `event_id`、`type`、`session_id`、`run_id`、`project_id`、`space_id`、`sequence`、`component`、`payload`、`payload_schema_version`、`visibility`、`trace_id` | `event_id` 唯一、`(run_id, sequence)` 唯一 | payload 只存前端可展示字段。 |
| `agent_tool_calls` / `AgentToolCall` | `id`、`run_id`、`task_id`、`tool_name`、`tool_type`、`risk_level`、`status`、`input_summary`、`output_summary`、`idempotency_key`、`timeout_ms`、`retry_count`、错误和耗时字段 | `(run_id, tool_name)`、`(status, updated_at)` | 只存脱敏摘要，不存供应商原始响应全文。 |
| `agent_tasks` / `AgentTask` | `id`、`run_id`、`task_type`、`resource_type`、`status`、`progress_percent`、`progress_detail`、`cancel_requested`、`external_task_ref`、错误和时间字段 | `(run_id, status)`、`(status, updated_at)` | 外部任务引用不作为业务事实。 |
| `agent_interrupts` / `AgentInterrupt` | `id`、`run_id`、`interrupt_type`、`status`、`reason`、`confirmation_payload`、`allowed_actions`、`resume_context`、`idempotency_key`、`expires_at`、`resolved_at` | `(run_id, status)`、`(expires_at, status)`、`(interrupt_id, idempotency_key)` | 恢复上下文不能包含密钥和业务库内部字段。 |
| `agent_artifacts` / `AgentArtifact` | `id`、`session_id`、`project_id`、`run_id`、`artifact_type`、`status`、`element_type`、`content`、`business_ref_id`、`visibility`、`version` | `(session_id, artifact_type, updated_at)`、`(project_id, artifact_type, updated_at)` | `business_ref_id` 只是业务资产引用，不是资产事实。 |
| `agent_safety_evaluations` / `AgentSafetyEvaluation` | `safety_evidence_id`、`scene`、`target_type`、`target_ref_id`、`evaluated_object_digest`、`policy_version`、`evidence_version`、`result`、`user_visible_reason`、`evaluated_at`、`expires_at`、来源字段 | `safety_evidence_id` 唯一、`(source_run_id, target_type)` | 不存完整文本、内部评分、命中规则和供应商响应。 |
| `agent_memories` / `AgentMemory` | `id`、`user_id`、`space_id`、`memory_type`、`scope`、`content_summary`、`authorized`、`expires_at`、`source_session_id` | `(user_id, scope, updated_at)` | 生产级实现只允许会话摘要和用户授权偏好。 |
| `agent_runtime_configs` / `AgentRuntimeConfig` | `config_key`、`version`、`status`、`owner`、`content`、`safe_config_refs`、`activated_at`、`deprecated_at` | `(config_key, version)` 唯一 | 密钥只存安全配置引用。 |

## Repository 接口必须覆盖

| Repository | 核心函数 | 入参 | 出参 |
| --- | --- | --- | --- |
| `SessionRepository` | `CreateSession`、`GetSessionForUpdate`、`ListSessionsByProject`、`UpdateSnapshot` | `ctx`、session model、分页、`project_id` | session、分页列表、错误 |
| `RunRepository` | `CreateRun`、`UpdateRunStatus`、`GetActiveRunBySession`、`GetRunSnapshot` | `ctx`、run model、状态迁移条件 | run、状态迁移结果 |
| `EventRepository` | `AppendEvent`、`ListEventsAfterSequence`、`GetEventByID` | `ctx`、event、`run_id`、`after_sequence`、分页 | 事件列表、是否超过补偿窗口 |
| `InterruptRepository` | `CreateInterrupt`、`ResolveInterrupt`、`GetRequiredInterrupt` | `ctx`、interrupt、`idempotency_key` | interrupt、幂等结果 |
| `ArtifactRepository` | `UpsertBlackboard`、`AppendArtifactRef`、`ListArtifactsForSnapshot` | `ctx`、artifact、`session_id`、`project_id` | artifact 列表 |
| `SafetyRepository` | `CreateSafetyEvidence`、`GetSafetyEvidence`、`MarkExpired` | `ctx`、evidence、`safety_evidence_id` | evidence |

## Migration 文件计划

- `db/migrations/iterations/<date>_agent_runtime/agent/001_create_agent_runtime.up.sql`
- `db/migrations/iterations/<date>_agent_runtime/agent/001_create_agent_runtime.down.sql`
- SQL 必须通过检查：无 `FOREIGN KEY`、无 `REFERENCES`、列表查询索引覆盖默认分页路径。

## 业务开发对齐点

- Agent 表只保存 `project_id`、`asset_id`、`work_id` 等普通引用。
- Agent 表不能保存项目标题、积分余额、最终资产事实、作品公开状态、企业成员关系。
- 需要业务服务提供权限校验和摘要查询，而不是 Agent 复制业务字段。

## 【业务开发】需要提供的能力与参数

| 业务能力 | 参数 | Agent 用途 |
| --- | --- | --- |
| 项目权限校验 | `auth_context`、`project_id`、`access_purpose` | Agent 写入 `project_id` 前后只做引用保存，权限事实由业务确认。 |
| 资产摘要批量查询 | `auth_context`、`asset_ids[]`、`project_id`、`purpose`、分页可选 | Agent 快照和黑板只保存资产引用，需要展示时使用业务摘要。 |
| Skill runtime spec 查询 | `auth_context`、`skill_id`、`version` | Agent `skill_selection` 只保存运行时快照引用和公开摘要。 |
| 模型计价快照查询 | `auth_context`、`model_id`、`resource_type` | Agent `model_selection_snapshot` 只保存计价快照引用。 |
