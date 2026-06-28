# 04-Agent API会话运行与函数出入参设计

状态：archived
owner：Agent 服务责任域
更新时间：2026-06-28
适用范围：Agent HTTP API、session、run、message、interrupt、cancel、snapshot、SSE 入口
相关代码路径：`services/agent/internal/api/http/**`、`services/agent/internal/application/**`
相关设计契约：`code-plan/agent/00-事实源需求映射与开发顺序.md`
后续实现落点：`api/openapi/agent-workbench.yaml`

## 文档目标

- 定义前端工作台调用 Agent 服务的 API 能力。
- 设计 API request/response DTO。
- 设计 Application Service 函数出入参。
- 明确鉴权、项目权限校验、幂等和错误码。

## API 范围

- 创建会话。
- 查询会话快照。
- 创建 run。
- 查询 run 状态。
- SSE 事件流。
- 事件补偿。
- 追加用户输入。
- 确认中断。
- 拒绝中断。
- 取消 run。
- 查询 run snapshot。

## HTTP 路由表

| Method | Path | Handler | 说明 |
| --- | --- | --- | --- |
| `POST` | `/api/agent/sessions` | `CreateSessionHandler` | 在项目下创建 Agent 会话。 |
| `GET` | `/api/agent/sessions/:session_id` | `GetSessionHandler` | 查询会话摘要和 snapshot。 |
| `GET` | `/api/agent/sessions/:session_id/messages` | `ListMessagesHandler` | 分页查询消息，默认 10。 |
| `POST` | `/api/agent/runs` | `CreateRunHandler` | 创建一次 Agent run。 |
| `GET` | `/api/agent/runs/:run_id` | `GetRunHandler` | 查询 run 状态。 |
| `GET` | `/api/agent/runs/:run_id/stream` | `OpenRunStreamHandler` | SSE 事件流。 |
| `GET` | `/api/agent/runs/:run_id/events` | `ReplayEventsHandler` | 按 `after_sequence` 或 `Last-Event-ID` 补偿。 |
| `POST` | `/api/agent/runs/:run_id/messages` | `AppendUserInputHandler` | 追加用户输入。 |
| `POST` | `/api/agent/runs/:run_id/interrupts/:interrupt_id/accept` | `AcceptInterruptHandler` | 确认中断。 |
| `POST` | `/api/agent/runs/:run_id/interrupts/:interrupt_id/reject` | `RejectInterruptHandler` | 拒绝中断。 |
| `POST` | `/api/agent/runs/:run_id/cancel` | `CancelRunHandler` | 取消 run。 |
| `GET` | `/api/agent/runs/:run_id/snapshot` | `GetRunSnapshotHandler` | 查询 run 恢复快照。 |

## DTO 设计必须覆盖

| DTO | 字段 |
| --- | --- |
| `CreateSessionRequest` | `project_id`、`initial_title`、`idempotency_key`。 |
| `CreateSessionResponse` | `session_id`、`project_id`、`status`、`snapshot`。 |
| `CreateRunRequest` | `session_id`、`project_id`、`user_input`、`model_selection`、`referenced_assets[]`、`control_inputs[]`、`idempotency_key`。 |
| `UserInputDTO` | `text`、`content_type`、`language`、`attachments[]`、`client_message_id`。 |
| `AssetReferenceDTO` | `asset_id`、`project_id`、`source`、`purpose`、`metadata_digest`。 |
| `ControlInputDTO` | `control_id`、`type`、`value`、`display_label`、`required`、`validation_digest`。 |
| `ModelSelectionDTO` | `resource_type`、`model_id`、`model_display_name`、`is_default`、`pricing_snapshot_id`。 |
| `CreateRunResponse` | `run_id`、`session_id`、`project_id`、`status`、`stream_url`、`snapshot_version`。 |
| `ConfirmInterruptRequest` | `interrupt_id`、`run_id`、`action`、`confirmed_payload_digest`、`idempotency_key`。 |
| `RejectInterruptRequest` | `interrupt_id`、`run_id`、`reason_code`、`idempotency_key`。 |
| `CancelRunRequest` | `run_id`、`cancel_reason`、`idempotency_key`。 |
| `SnapshotResponse` | `session`、`run`、`messages[]`、`assets[]`、`blackboard`、`tasks[]`、`last_event_sequence`、`readonly_reason`。 |

## OpenAPI 字段基线

正式 OpenAPI 文件落点：`api/openapi/agent-workbench.yaml`。所有字段采用 `snake_case`，时间使用 RFC3339 字符串，未知字段前端必须忽略。

```yaml
ErrorResponse:
  type: object
  required: [code, message, retryable, trace_id]
  properties:
    code: { type: string }
    message: { type: string }
    retryable: { type: boolean }
    trace_id: { type: string }
    details: { type: object, additionalProperties: true }

UserInputDTO:
  type: object
  required: [client_message_id, content_type, text]
  properties:
    client_message_id: { type: string, maxLength: 64 }
    content_type: { type: string, enum: [text, multimodal_text] }
    text: { type: string, maxLength: 8000 }
    language: { type: string, default: zh-CN }
    attachments:
      type: array
      items: { $ref: '#/components/schemas/InputAttachmentDTO' }

InputAttachmentDTO:
  type: object
  required: [attachment_id, attachment_type, source]
  properties:
    attachment_id: { type: string }
    attachment_type: { type: string, enum: [asset, upload_intent, external_url] }
    source: { type: string, enum: [project_asset, user_upload, public_reference] }
    metadata_digest: { type: string }

AssetReferenceDTO:
  type: object
  required: [asset_id, project_id, purpose]
  properties:
    asset_id: { type: string }
    project_id: { type: string }
    source: { type: string, enum: [project_asset, blackboard, user_selected] }
    purpose: { type: string, enum: [agent_input, view, reference] }
    metadata_digest: { type: string }

CreateRunRequest:
  type: object
  required: [session_id, project_id, user_input]
  properties:
    session_id: { type: string }
    project_id: { type: string }
    user_input: { $ref: '#/components/schemas/UserInputDTO' }
    model_selection: { $ref: '#/components/schemas/ModelSelectionDTO' }
    referenced_assets:
      type: array
      items: { $ref: '#/components/schemas/AssetReferenceDTO' }
    control_inputs:
      type: array
      items: { $ref: '#/components/schemas/ControlInputDTO' }

SnapshotResponse:
  type: object
  required: [session, run, messages, assets, blackboard, tasks, last_event_sequence]
  properties:
    session: { type: object, additionalProperties: true }
    run: { type: object, additionalProperties: true }
    messages: { type: array, items: { type: object, additionalProperties: true } }
    assets: { type: array, items: { type: object, additionalProperties: true } }
    blackboard: { type: object, additionalProperties: true }
    tasks: { type: array, items: { type: object, additionalProperties: true } }
    last_event_sequence: { type: integer, format: int64 }
    readonly_reason: { type: string, enum: ['', project_archived, permission_lost] }
```

HTTP 状态码固定映射：

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `VALIDATION_FAILED` | DTO 校验失败、缺少幂等头、非法枚举。 |
| 401 | `UNAUTHENTICATED` | 登录态缺失或失效。 |
| 403 | `PERMISSION_DENIED`、`CROSS_SPACE_DENIED` | 无项目、资产或空间权限。 |
| 404 | `RESOURCE_NOT_FOUND`、`PROJECT_NOT_FOUND` | 资源不存在或不可见。 |
| 409 | `PROJECT_ARCHIVED`、`RUN_STATE_CONFLICT`、`INTERRUPT_EXPIRED`、`IDEMPOTENCY_CONFLICT` | 状态冲突。 |
| 429 | `RATE_LIMITED` | 限流。 |
| 500 | `INTERNAL_ERROR` | 未分类系统错误。 |
| 504 | `UPSTREAM_TIMEOUT` | RPC 或外部供应商超时。 |

## Application 函数出入参

```go
// CreateRunCommand 是创建 run 的应用层命令。
type CreateRunCommand struct {
    AuthContext      AuthContextDTO      // 当前登录身份和空间上下文。
    ProjectID        string              // 业务项目 ID。
    SessionID        string              // Agent 会话 ID。
    UserInput        UserInputDTO        // 文本、附件和客户端消息 ID。
    ModelSelection   *ModelSelectionDTO  // 用户选择模型，可为空。
    ReferencedAssets []AssetReferenceDTO // 引用资产列表。
    ControlInputs    []ControlInputDTO   // 控件输入。
    IdempotencyKey   string              // 创建 run 幂等键。
    TraceID          string              // 链路追踪 ID。
}

// CreateRunResult 是创建 run 的返回结果。
type CreateRunResult struct {
    RunID           string // Run ID。
    SessionID       string // 会话 ID。
    ProjectID       string // 项目 ID。
    Status          string // 当前 run 状态。
    StreamURL       string // SSE 地址。
    SnapshotVersion string // 初始快照版本。
}
```

## run 并发策略

- 同一 session 在生产级实现中只允许一个 active run，active 状态包括 `pending`、`running`、`waiting_confirmation`、`resuming`。
- 新建 run 时若存在 active run，返回 `RUN_STATE_CONFLICT`，并返回当前 active run 摘要。
- 不同 session 可以并行 run，但都必须各自校验项目权限和当前空间。
- 用户追加输入时，如果当前 run 处于 `waiting_confirmation`，默认不修改原确认参数；用户需要先 reject/cancel，再创建新 run。

## API 细节设计

| 主题 | 设计要求 |
| --- | --- |
| SSE 鉴权 | 明确是否复用登录 token，是否签发短期 stream token；SSE 必须校验 `run_id`、`session_id`、`project_id` 权限。 |
| run 并发策略 | 生产级实现中同一 session 只允许一个 `running/waiting_confirmation/resuming` run，不同 session 可并行。 |
| 用户输入拆分 | 文本、素材引用、控件输入、模型选择必须分开建 DTO，不把所有内容塞进一个 JSON 字符串。 |
| HTTP 错误映射 | `UNAUTHENTICATED=401`、`PERMISSION_DENIED=403`、`PROJECT_NOT_FOUND=404`、`PROJECT_ARCHIVED=409`、`RUN_STATE_CONFLICT=409`、`INTERRUPT_EXPIRED=409`、`VALIDATION_FAILED=400`、`INTERNAL_ERROR=500`。 |
| 项目归档校验 | 创建 session/run、追加输入、confirm、reject、cancel、snapshot 的读写语义分开；创作类操作用 `continue_creation`，只读 snapshot 用 `view`。 |
| 幂等 | 创建 session、创建 run、confirm、reject、cancel 必须接收 `Idempotency-Key`。 |

## 通用请求头

| Header | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | 登录态 token，Agent HTTP middleware 必须通过 `AccountSpaceService.ResolveAuthContextFromToken` 解析为 `AuthContextDTO` 和 `SpaceContextDTO`。 |
| `Idempotency-Key` | 写操作必填 | 创建 session/run、confirm、reject、cancel 使用。 |
| `X-Request-ID` | 否 | 无则 Agent 生成 `trace_id`。 |
| `X-Space-Id` | 否 | 仅作为 `expected_space_id` 传给业务服务校验，不作为身份事实。 |
| `Last-Event-ID` | SSE 可选 | SSE 重连时用于定位补偿起点。 |

鉴权约束：Agent API 不得信任 `X-Actor-User-Id`、`X-Login-Identity-Type`、`X-Enterprise-Id`、`X-Enterprise-Role` 等客户端身份头；这些字段只能来自业务服务对 `Authorization` token 的解析结果。缺 token、token 失效、用户禁用、企业成员移除或 expected space 不匹配必须返回稳定 401/403，不得继续创建 session/run。

## Handler 设计

| Handler | 调用 Application | 前置校验 | 日志字段 |
| --- | --- | --- | --- |
| `CreateSessionHandler` | `SessionApplication.CreateSession` | 登录、`project_id`、幂等键 | `project_id`、`session_id`、`trace_id`。 |
| `CreateRunHandler` | `RunApplication.CreateRun` | 登录、session/project 一致、幂等键 | `session_id`、`run_id`、`client_message_id`。 |
| `OpenRunStreamHandler` | `EventApplication.OpenStream` | 登录、run 权限、Last-Event-ID | `run_id`、`last_event_id`、`connected_ms`。 |
| `AcceptInterruptHandler` | `RunApplication.AcceptInterrupt` | 登录、interrupt 状态、幂等键 | `interrupt_id`、`action`、`payload_digest`。 |
| `CancelRunHandler` | `RunApplication.CancelRun` | 登录、run 状态、幂等键 | `run_id`、`cancel_reason`。 |
| `GetRunSnapshotHandler` | `SnapshotApplication.GetRunSnapshot` | 登录、项目 `view` 权限 | `run_id`、`last_event_sequence`。 |

## API 单元测试和契约测试

| 场景 | 断言 |
| --- | --- |
| 创建 session 成功 | 调用 `CheckProjectAccess(continue_creation)`，返回 `session_id`。 |
| 归档项目创建 run | HTTP 409，错误码 `PROJECT_ARCHIVED`。 |
| 同 session 并发 run | HTTP 409，错误码 `RUN_STATE_CONFLICT`。 |
| 引用资产无权限 | HTTP 403 或逐项拒绝摘要，不启动 run。 |
| SSE 重连 | 根据 `Last-Event-ID` 返回缺失事件。 |
| confirm 重复提交 | 相同幂等键返回同一结果，不重复冻结。 |
| snapshot 只读 | archived 项目可 view，返回 `readonly_reason`。 |

## 业务开发对齐点

- 创建 session/run 前需要 `ProjectService.CheckProjectAccess`。
- confirm、cancel、resume 前是否需要再次校验项目状态和空间权限。
- 业务服务对 `PROJECT_ARCHIVED`、`PERMISSION_DENIED` 等错误码的返回格式。

## 【业务开发】需要提供的能力与参数

| API 场景 | 业务能力 | 必要参数 | 响应要求 |
| --- | --- | --- | --- |
| 创建 session | `ProjectService.CheckProjectAccess` | `auth_context`、`project_id`、`access_purpose=continue_creation`、`request_meta.trace_id` | `allowed`、`project_status`、`creative_allowed`、`allowed_actions`、错误码。 |
| 创建 run | `ProjectService.CheckProjectAccess`、`AssetService.BatchCheckAssetAccess` | `auth_context`、`project_id`、`asset_ids[]`、`purpose=agent_input` | 项目可创作；资产逐项返回 `allowed` 和展示摘要。 |
| confirm / resume | `ProjectService.CheckProjectAccess` | `auth_context`、`project_id`、`access_purpose=continue_creation`、`run_id` | 若归档，Agent 不恢复执行并发送归档阻断事件。 |
| snapshot 只读 | `ProjectService.CheckProjectAccess` | `auth_context`、`project_id`、`access_purpose=view` | archived 项目允许查看，返回只读原因。 |
| HTTP 错误展示 | 所有业务 RPC | 稳定 `error_code`、`user_message`、`retryable`、`trace_id` | Agent 映射为 HTTP 错误和 AG-UI 错误 payload。 |
