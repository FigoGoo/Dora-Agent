# 06-AG-UI事件生产SSE补偿与Payload设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：内部事件、AG-UI 事件、SSE、event store、事件补偿、snapshot、前端消费边界
相关代码路径：`services/agent/internal/events/**`
相关契约：`api/agui/agent-workbench-events.schema.json`、`docs/standards/AG-UI事件规范.md`

## 文档目标

- 定义 Agent 内部事件到 AG-UI 事件的映射。
- 设计事件 payload、sequence、event_id、SSE 和补偿查询。
- 设计未知事件兼容和断线恢复。
- 禁止泄露 Eino 内部节点、系统 Prompt、模型推理链路、供应商响应、密钥和内部成本。

## 事件范围

- `agent.run.*`
- `agent.message.*`
- `agent.thinking.*`
- `agent.skill.*`
- `platform.tags.updated`
- `chat.controls.*`
- `safety.prompt.*`
- `credits.*`
- `confirmation.*`
- `tool.call.*`
- `generation.*`
- `asset.save.*`
- `workspace.*`
- `process.snapshot.saved`
- `project.archived.blocked`

## 逐事件 Payload 必须覆盖

| 事件 | payload 必填字段 |
| --- | --- |
| `agent.run.started` | `run_status`、`project_id`、`session_id`、`started_at`。 |
| `agent.message.delta` | `message_id`、`role`、`text_delta`、`content_type`。 |
| `agent.skill.selected` | `skill_id`、`skill_name`、`skill_scope`、`skill_version`、`matched_reason`。 |
| `chat.controls.requested` | `controls[]`，每项含 `control_id`、`type`、`label`、`options`、`required`、`validation`。 |
| `chat.controls.locked` | `locked_fields[]`、`locked_reason`、`confirmation_id`。 |
| `safety.prompt.evaluating` | `scene`、`target_type`、`target_ref_id`、`checked_target`。 |
| `safety.prompt.evaluated` | `safety_status=passed`、`safety_evidence_id`、`policy_version`、`expires_at`。 |
| `safety.prompt.blocked` | `safety_status=blocked/failed`、`user_message`、`retryable`、`support_trace_id`。 |
| `credits.estimated` | `estimate_points`、`available_points`、`expires_soon_points`、`account_type`、`pricing_snapshot_id`。 |
| `confirmation.required` | `confirmation_id`、`interrupt_id`、`title`、`summary`、`risks[]`、`points`、`expires_at`、`actions[]`。 |
| `credits.frozen` | `freeze_id`、`frozen_points`、`expires_at`。 |
| `tool.call.started` | `tool_call_id`、`tool_name`、`tool_type`、`risk_level`、`timeout_ms`。 |
| `generation.progress` | `task_id`、`resource_type`、`status`、`progress`、`partial_completed`。 |
| `generation.artifact.completed` | `artifact_id`、`resource_type`、`name`、`metadata_summary`、`elements_summary`。 |
| `asset.save.completed` | `asset_id`、`artifact_id`、`resource_type`、`save_status`、`elements[]`、`downloadable`。 |
| `asset.save.failed` | `artifact_id`、`error_code`、`user_message`、`released_points`、`retryable`。 |
| `workspace.blackboard.updated` | `mode`、`elements[]`、`storyline[]`、`active_node_id`、`version`。 |
| `project.archived.blocked` | `project_status=archived`、`creative_allowed=false`、`read_only_reason`、`allowed_actions[]`、`user_message`。 |
| `agent.run.failed` | `error_type`、`error_code`、`user_message`、`retryable`、`support_trace_id`。 |

## 顺序、补偿和快照要求

- 同一 `run_id` 内 `sequence` 单调递增，写入 `agent_events` 时用 `(run_id, sequence)` 唯一约束防重复。
- `event_id` 全局唯一，重放时保持不变。
- SSE 支持 `Last-Event-ID`；HTTP 补偿支持 `run_id + after_sequence`。
- 生产级实现的事件表保留完整 run 事件，前端补偿分页默认 10、上限 100。
- 前端发现 sequence 缺口后暂停增量合并，调用补偿 API；补偿失败使用 snapshot。
- unknown event 必须被前端忽略并记录 debug，不能让页面崩溃。
- snapshot schema 必须包含 `session`、`run`、`messages[]`、`tasks[]`、`assets[]`、`blackboard`、`last_event_sequence`、`readonly_reason`。

## 前端 reducer 测试数据

AG-UI reducer 测试 fixture 必须为以下场景提供最小事件序列：

- 正常文本对话。
- 安全阻断。
- 积分不足。
- 确认后生成成功并扣费。
- 生成部分完成后取消。
- 资产保存失败并释放积分。
- 项目运行中归档。
- SSE 重连补偿。
- unknown event 兼容。

## 公共事件结构

所有 SSE 和补偿 API 返回事件统一使用 `type` 字段。兼容旧前端时可额外返回 `event_type`，但新实现只能以 `type` 为准。

```json
{
  "event_id": "evt_01JZ...",
  "type": "generation.progress",
  "session_id": "ses_01JZ...",
  "run_id": "run_01JZ...",
  "project_id": "prj_01JZ...",
  "space_id": "spc_01JZ...",
  "actor_user_id": "usr_01JZ...",
  "sequence": 12,
  "timestamp": "2026-06-27T12:00:00Z",
  "component": "agent-runtime",
  "trace_id": "trc_01JZ...",
  "payload": {}
}
```

公共规则：

- `sequence` 在同一 `run_id` 内从 1 开始单调递增，不能跳号提交。
- `payload` 不允许包含系统 Prompt、完整用户隐私原文、供应商原始响应、API Key、内部成本价和模型密钥引用。
- 用户可见失败事件必须包含 `user_message` 和 `support_trace_id`。
- 前端 unknown event 只记录 debug，不改变当前 reducer 状态。

## AG-UI Schema 文件计划

正式 schema 落点：`api/agui/agent-workbench-events.schema.json`，测试 fixture 落点：`tests/agent/agui/fixtures/*.json`。

```json
{
  "$id": "https://dora.local/schemas/agui/agent-workbench-events.schema.json",
  "type": "object",
  "required": ["event_id", "type", "session_id", "run_id", "project_id", "space_id", "actor_user_id", "sequence", "timestamp", "trace_id", "payload"],
  "properties": {
    "event_id": { "type": "string", "pattern": "^evt_[A-Za-z0-9_\\-]+$" },
    "type": { "type": "string" },
    "session_id": { "type": "string" },
    "run_id": { "type": "string" },
    "project_id": { "type": "string" },
    "space_id": { "type": "string" },
    "actor_user_id": { "type": "string" },
    "sequence": { "type": "integer", "minimum": 1 },
    "timestamp": { "type": "string", "format": "date-time" },
    "component": { "type": "string" },
    "trace_id": { "type": "string" },
    "payload": { "type": "object" }
  },
  "additionalProperties": false
}
```

逐事件 payload schema 以 `$defs` 维护，命名规则为 `Payload_<event type with dot replaced by underscore>`，例如 `Payload_generation_progress`。新增事件必须同时提交：schema、最小 fixture、reducer 单测和 Agent publisher 单测。

## Fixture 最小集

| 文件 | 覆盖 |
| --- | --- |
| `normal_generation_success.json` | 安全通过、积分确认、冻结、生成、保存、扣费、完成。 |
| `safety_blocked.json` | 安全阻断后不预估、不冻结。 |
| `credit_insufficient.json` | 积分不足终止。 |
| `confirmation_rejected.json` | 用户拒绝确认。 |
| `asset_save_failed_release.json` | 资产保存失败并释放冻结。 |
| `project_archived_running.json` | 运行中归档阻断。 |
| `sse_replay_gap.json` | sequence 缺口和补偿。 |
| `unknown_event_ignored.json` | unknown event 不破坏 reducer。 |

## Event Publisher 函数

包路径：`services/agent/internal/events`。

| 函数 | 入参 | 出参 | 说明 |
| --- | --- | --- | --- |
| `PublishAGUIEvent(ctx, command)` | `run_id`、`type`、`payload`、`trace_id`、`idempotency_key` | `AGUIEvent` | 在同一事务内分配 sequence、保存事件，再异步推送 SSE。 |
| `BuildEventPayload(ctx, domainEvent)` | 领域事件 | `map[string]any` | 把 Eino callback、Tool、RPC 结果映射为协议 payload。 |
| `ReplayEventsAfterSequence(ctx, runID, afterSequence, pageSize)` | `run_id`、`after_sequence`、`page_size` | `events[]`、`next_after_sequence` | 用于断线补偿，默认 10 条，上限 100 条。 |
| `BuildSnapshotFallback(ctx, runID)` | `run_id` | `snapshot` | 当前端补偿窗口失败或 sequence 缺口不可补时返回。 |

写入规则：

- `(run_id, sequence)` 唯一，`event_id` 全局唯一。
- 同一 `idempotency_key` 重试必须返回相同 `event_id` 和 `sequence`。
- SSE 推送失败不回滚已写入事件；前端通过补偿 API 获取。

## SSE 与补偿 API

| 接口 | 设计 |
| --- | --- |
| `GET /api/agent/runs/:run_id/stream` | 登录态或一次性 `stream_token` 鉴权；校验 `ProjectService.CheckProjectAccess(view)`；支持 `Last-Event-ID`。 |
| `GET /api/agent/runs/:run_id/events?after_sequence=&page_size=` | 补偿事件查询；`page_size` 默认 10，上限 100；只返回当前用户可访问 run。 |
| `GET /api/agent/runs/:run_id/snapshot` | 补偿失败时拉取快照；返回 `last_event_sequence` 和 `readonly_reason`。 |
| heartbeat | SSE 每 15 秒发送注释 heartbeat，不写入 `agent_events`。 |
| 关闭条件 | run 进入 `completed`、`failed`、`cancelled` 后发送终态事件并主动关闭。 |

## 日志和指标

| 输出点 | 字段 |
| --- | --- |
| 事件写入 | `event_id`、`type`、`run_id`、`sequence`、`trace_id`、`payload_size`。 |
| SSE 连接 | `run_id`、`actor_user_id`、`last_event_id`、`client_ip_digest`、`connected_ms`。 |
| 补偿查询 | `run_id`、`after_sequence`、`page_size`、`returned_count`、`snapshot_required`。 |
| reducer 测试 fixture | 每类主链路保存 JSON fixture，命名 `agui_<scenario>_events.json`。 |

## 业务开发对齐点

- 业务 RPC 错误如何映射为用户可理解事件。
- 积分、资产、项目归档等业务结果需要返回哪些展示摘要。
- 业务侧 trace_id 是否和 Agent trace_id 保持贯通。

## 【业务开发】需要提供的能力与参数

| 事件来源 | 业务能力 | 业务返回参数 |
| --- | --- | --- |
| `project.archived.blocked` | `ProjectService.CheckProjectAccess` | `project_status`、`creative_allowed`、`allowed_actions`、`user_message`。 |
| `credits.*` | `CreditService.*` | `estimate_points`、`available_points`、`freeze_id`、`charged_points`、`released_points`、`expires_soon_points`。 |
| `asset.save.*` | `AssetCreditCommitService.CommitGeneratedAssetAndCharge` | `asset_id`、`asset_summary`、`elements[]`、`charged_points`、`released_points`、错误详情。 |
| `tool.call.failed` | 所有业务 RPC | `error_code`、`user_message`、`retryable`、`trace_id`、`error_type`。 |
| `workspace.assets.updated` | `AssetService.BatchCheckAssetAccess` | `assets[]`，每项含 `asset_id`、`resource_type`、`display_name`、`preview_available`、`permission_status`。 |
