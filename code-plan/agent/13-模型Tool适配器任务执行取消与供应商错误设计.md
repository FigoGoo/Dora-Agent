# 13-模型Tool适配器任务执行取消与供应商错误设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：图片、音乐、视频模型 Tool 适配器，任务提交、查询、取消、超时、部分完成和供应商错误分类
相关代码路径：`services/agent/internal/runtime/tool/**`、`services/agent/internal/runtime/modeltool/**`
相关契约：`docs/product/prd/03-模型供应商模型选择与单价PRD.md`、`docs/product/prd/04-Tool边界与平台开放能力PRD.md`、`docs/product/prd/07-积分账户兑换码与扣费PRD.md`

## 文档目标

- 定义图片、音乐、视频生成 Tool 的统一接口。
- 支持同步返回和异步任务提交/查询模式。
- 定义取消、超时、部分完成和供应商错误分类。
- 明确模型供应商配置由业务服务管理，Agent 不保存 API Key。
- 定义 Tool 结果如何进入资产保存和扣费闭环。

## 模型 Tool 类型

| Tool | 用途 | 任务模式 |
| --- | --- | --- |
| `ImageGenerationTool` | 文生图、图生图、风格化 | 可同步或异步，统一适配为 task。 |
| `MusicGenerationTool` | 文生音乐、歌词生成歌曲、BGM | 多为异步 task。 |
| `VideoGenerationTool` | 文生视频、图生视频、分镜转视频 | 异步 task，必须支持查询和取消。 |
| `VisionUnderstandingTool` | 图片理解和素材分析 | 同步或短任务，不对用户单独计费。 |

## 统一接口必须覆盖

| 函数 | 入参 | 出参 |
| --- | --- | --- |
| `SubmitGenerationTask` | `tool_name`、`resource_type`、`model_snapshot`、`prompt`、`parameters`、`input_assets[]`、`timeout_ms`、`idempotency_key` | `task_id`、`external_task_ref`、`status`。 |
| `PollGenerationTask` | `task_id`、`external_task_ref`、`resource_type` | `status`、`progress`、`artifacts[]`、`provider_error`。 |
| `CancelGenerationTask` | `task_id`、`external_task_ref`、`cancel_reason`、`idempotency_key` | `cancel_status`、`completed_artifacts[]`。 |
| `NormalizeProviderError` | `provider`、`raw_error_code`、`raw_error_message_digest` | `error_type`、`error_code`、`retryable`、`user_message`。 |
| `BuildArtifactsFromTaskResult` | `task_result`、`skill_output_schema` | `artifacts[]`、`process_elements[]`、`missing_required[]`。 |
| `PersistProviderArtifactsToTOS` | `run_id`、`project_id`、`provider_artifacts[]`、`idempotency_key` | `artifacts[]`，含 `storage_object_ref`、`checksum`、`size_bytes`。 |

## Model Tool Adapter 架构

```text
services/agent/internal/runtime/modeltool/
  adapter.go
  registry.go
  task_runner.go
  error_mapper.go
  artifact_builder.go
  adapters/
    image_adapter.go
    music_adapter.go
    video_adapter.go
    vision_adapter.go
```

`Tool Executor` 依赖统一 `GenerationToolAdapter` 接口；具体供应商适配器只能出现在 `adapters/**`，不能向上暴露供应商原始响应和密钥字段。

```go
// GenerationToolAdapter 是图片、音乐、视频生成供应商的统一适配接口。
type GenerationToolAdapter interface {
    Submit(ctx context.Context, req SubmitGenerationTaskRequest) (SubmitGenerationTaskResult, error)
    Poll(ctx context.Context, req PollGenerationTaskRequest) (PollGenerationTaskResult, error)
    Cancel(ctx context.Context, req CancelGenerationTaskRequest) (CancelGenerationTaskResult, error)
}

// ModelSnapshotDTO 是业务服务返回给 Agent 的非敏感模型运行快照。
type ModelSnapshotDTO struct {
    ModelID           string
    ResourceType      string
    ProviderRef       string
    PublicDisplayName string
    PricingSnapshotID string
    TimeoutMS         int
    RetryPolicy       RetryPolicyDTO
    RuntimeParams     map[string]string
}
```

## 任务状态机

| 状态 | 说明 | 可流转到 |
| --- | --- | --- |
| `created` | Agent 创建本地 task | `submitted`、`failed`、`cancelled` |
| `submitted` | 已获得供应商任务引用 | `running`、`completed`、`failed`、`timeout`、`cancel_requested` |
| `running` | 查询到处理中 | `completed`、`partial_completed`、`failed`、`timeout`、`cancel_requested` |
| `cancel_requested` | 用户取消或系统抢占 | `cancelled`、`partial_completed`、`failed` |
| `partial_completed` | 有可保存产物但未全部完成 | `completed`、`cancelled`、`failed` |
| `completed` | 所有产物完成 | 终态 |
| `failed` | 不可恢复失败 | 终态 |
| `timeout` | 超过配置超时 | `cancelled`、`failed` |
| `cancelled` | 取消成功或本地停止轮询 | 终态 |

## 参数 schema

| 资源类型 | 参数 |
| --- | --- |
| `image` | `prompt`、`negative_prompt`、`size`、`style`、`seed`、`quantity`、`input_assets[]`。 |
| `music` | `prompt`、`lyrics`、`duration_seconds`、`vocal_mode`、`tempo`、`quantity`。 |
| `video` | `prompt`、`duration_seconds`、`resolution`、`fps`、`storyboard_refs[]`、`input_assets[]`。 |
| `vision` | `input_assets[]`、`question`、`output_schema`。 |

参数校验在提交供应商前完成；非法参数不消耗积分、不提交任务，返回 `provider_invalid_request` 的用户可修改提示。

## 供应商错误分类

| 错误类型 | 示例 | Agent 行为 |
| --- | --- | --- |
| `provider_auth_error` | API Key 无效、权限不足 | run failed，提示平台配置异常，不重试。 |
| `provider_rate_limited` | 限流 | 可退避重试，超过次数失败。 |
| `provider_timeout` | 任务查询超时 | 标记 task timeout，释放未完成冻结积分。 |
| `provider_invalid_request` | 参数不被供应商接受 | 用户可修改参数后重新发起。 |
| `provider_generation_failed` | 生成失败 | 失败，释放未完成冻结积分。 |
| `provider_partial_completed` | 部分产物完成 | 已完成进入资产保存，未完成释放。 |
| `provider_unavailable` | 供应商不可用 | 可重试或提示稍后再试。 |

## 取消和部分完成规则

- 用户取消后不再发起新的 Tool。
- 已完成且可保存的 artifact 继续进入资产保存和扣费闭环。
- 未完成、失败或保存失败的 artifact 释放冻结积分。
- 取消不回滚已经由业务服务确认保存和扣费的资产。
- 所有取消动作必须写入 `agent_tasks` 和 AG-UI `agent.run.cancelled` / `generation.progress`。

## 取消流程

```text
CancelRun
  -> 标记 agent_tasks.cancel_requested
  -> 调 adapter.Cancel
  -> 停止轮询未完成任务
  -> 已完成 artifact 进入 CommitGeneratedAssetAndCharge
  -> 未完成部分 ReleaseFrozenCredits
  -> 写 generation.progress / agent.run.cancelled
```

如果供应商不支持取消，Agent 停止本地轮询并将迟到供应商回调标记为 ignored；已经保存扣费的资产不回滚。

## 生成产物持久化到 TOS

供应商完成后返回的 URL、base64、临时文件句柄或 provider artifact ref 只是临时输入，不能作为业务资产事实。Agent 必须在调用 `CommitGeneratedAssetAndCharge` 前完成以下闭环：

```text
PollGenerationTask completed
  -> ExtractProviderArtifact
  -> StreamOrDecodeProviderArtifact
  -> ValidateArtifactFile(MIME/size/checksum/resource_type)
  -> BusinessGateway.PrepareGeneratedAssetObjects
  -> UploadArtifactToTOS(upload_url, upload_headers)
  -> HeadObjectOrUploadResult 校验 etag/checksum
  -> Build CommitArtifactDTO(storage_object_ref)
  -> CommitGeneratedAssetAndCharge
```

函数设计：

| 函数 | 入参 | 出参 | 说明 |
| --- | --- | --- | --- |
| `ExtractProviderArtifacts` | `PollGenerationTaskResult` | `ProviderArtifact[]` | 只提取必要的临时引用和展示摘要，不记录完整供应商响应。 |
| `StreamOrDecodeProviderArtifact` | `ProviderArtifact`、`max_size_bytes`、`context.Context` | `io.ReadCloser`、`content_type`、`size_bytes` | 支持 provider URL、base64 和 SDK 文件流；受 context 取消控制。 |
| `ValidateArtifactFile` | `resource_type`、`content_type`、`size_bytes`、`checksum` | `ValidationResult` | 不符合业务 MIME/大小限制时不上传、不扣费。 |
| `PrepareGeneratedAssetObjects` | `project_id`、`session_id`、`run_id`、`artifacts[]` | `upload_slots[]` | 通过业务 RPC 获取 object key 和短期上传授权。 |
| `UploadArtifactToTOS` | `upload_slot`、`artifact_stream`、`checksum` | `GeneratedStorageObjectRef` | 使用业务签发的单对象上传 URL，不接触 TOS AK/SK。 |

错误和补偿：

- `PrepareGeneratedAssetObjects` 返回 `PROJECT_ARCHIVED` 时，停止上传，调用 `ReleaseFrozenCredits`，发送 `project.archived.blocked`。
- 供应商文件下载失败或 checksum 不一致时，对该 artifact 标记 `failed`，未完成部分释放冻结；其他已上传成功 artifact 可按部分完成进入 commit。
- 上传授权过期时重新调用 `PrepareGeneratedAssetObjects` 必须使用同一幂等键；业务返回同一 object key 或明确 `IDEMPOTENCY_CONFLICT`。
- Agent 日志只记录 `artifact_id`、`object_key_digest`、`content_type`、`size_bytes`、`checksum_digest`、`trace_id`，不得记录完整 URL、签名 header 或供应商原始响应。

## 【业务开发】需要提供的能力与参数

| 能力 | 请求参数 | 响应参数 |
| --- | --- | --- |
| 模型运行配置读取 | `auth_context`、`model_id`、`resource_type`、`pricing_snapshot_id` | `model_snapshot`、`provider_ref`、`public_display_name`、`timeout_policy`；不返回 API Key 明文。 |
| Tool 执行策略 | `tool_name`、`tool_type=model_generation`、`auth_context` | `timeout_ms`、`retry_policy`、`cancel_policy`、`risk_level`。 |
| 资产保存与扣费 | `artifacts[]`、`final_elements[]`、`freeze_id`、`safety_evidence`、`idempotency_key` | `asset_refs[]`、`charged_points`、`released_points`。 |
| 生成产物对象准备 | `project_id`、`session_id`、`run_id`、`artifacts[]`、`idempotency_key` | `upload_slots[]`，每个 artifact 对应一个业务生成 object key 和短期上传授权。 |
| 供应商配置错误展示 | 模型或供应商不可用时业务错误 | `error_code`、`user_message`、`retryable=false`、`trace_id`。 |

## 日志、trace 和测试矩阵

日志字段：`task_id`、`tool_name`、`resource_type`、`model_id`、`provider_ref_digest`、`external_task_ref_digest`、`status`、`progress`、`error_code`、`retryable`、`trace_id`。不输出供应商原始请求、原始响应、API Key 和完整 Prompt。

| 场景 | 断言 |
| --- | --- |
| 同步成功 | 统一转换为 completed task 和 artifact。 |
| 异步成功 | submit、poll、completed 顺序完整。 |
| 用户取消 | 不再提交新任务，未完成释放积分。 |
| 供应商超时 | task timeout，错误可见，释放未完成部分。 |
| 部分完成 | 完成 artifact 可保存并扣费，未完成释放。 |
| 供应商鉴权错误 | `PROVIDER_CONFIG_MISSING` 或 `provider_auth_error`，不重试。 |
| 限流 | 按 retry policy 退避，超过次数失败。 |
| 参数非法 | 不冻结或释放冻结，用户可修改参数重试。 |
| 供应商产物落 TOS | provider URL/base64/SDK 流均能转换为 `storage_object_ref`；object key 由业务签发，日志不出现签名 URL。 |
| TOS 上传失败 | 不调用 commit；已冻结积分按失败或部分完成释放。 |
