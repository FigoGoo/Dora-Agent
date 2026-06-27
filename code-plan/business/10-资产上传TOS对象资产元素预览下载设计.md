# 10-资产上传 TOS 对象资产元素预览下载设计

状态：production-design-ready  
owner：业务微服务后端工程师  
更新时间：2026-06-27  
适用范围：上传素材、TOS object key、上传意图、资产事实、最终资产元素、资产权限、预览和下载授权  
相关代码路径：`services/business/internal/application/asset/**`、`services/business/internal/domain/asset/**`、`services/business/internal/infra/tos/**`

## 产品事实源

- `docs/product/资产与创作过程保存产品系统设计.md`
- `docs/product/prd/08-资产素材与创作过程PRD.md`
- `docs/product/内容安全治理产品系统设计.md`
- `docs/product/prd/10-内容安全治理PRD.md`

## 目标

业务服务保存用户可见资产事实和最终资产元素，签发前端直传 TOS 的短期上传凭证，并提供资产预览/下载授权。Agent 只保存资产引用，不签发 TOS 凭证，不保存长期媒体 URL。

## 非目标

- 不在业务服务保存 Agent 黑板临时内容、run event 或工具调用原始过程。
- 不让前端或 Agent 接触 TOS AK/SK。
- 不在资产详情返回长期私有下载签名或私有 object key。
- 不提供任意自定义资产元素类型编辑页面。

## 需求映射矩阵

| 产品条目 | 业务解释 | 业务产出 | 【Agent开发】依赖 |
| --- | --- | --- | --- |
| 上传素材 | 业务签发上传意图，前端直传 TOS 后确认 | `upload_intents`、`assets`、`asset_storage_objects` | 上传元数据安全评估可由 Agent 提供 `SafetyEvidenceDTO` |
| 资产库列表/详情 | 当前空间和项目内查看资产摘要 | `/api/assets/**`、`AssetDetailDTO` | Agent 仅保存 asset ref |
| 资产元素 | 最终资产元素只能使用平台内置类型 | `asset_elements`、`asset_element_types` | Agent 输出元素前调用 `ListAssetElementTypes` |
| 预览/下载 | 每次访问重新校验权限并签发短期访问 | `CreateSignedPreviewAccess`、`CreateSignedDownloadAccess` | Agent 事件不得携带长期 URL |
| 元素类型变更审计 | 内置字典发布或变更要可审计 | `asset_element_type_change_records`、`business_audit_logs` | Agent 只读取 active 类型 |

## 数据库表

| 表 | 字段 | 索引和约束 |
| --- | --- | --- |
| `assets` | `asset_id`、`space_id`、`project_id`、`owner_user_id`、`asset_type`、`source_type`、`status`、`storage_object_key`、`mime_type`、`size_bytes` | `(space_id,owner_user_id,project_id,status,created_at)` |
| `asset_elements` | `element_id`、`asset_id`、`element_type`、`element_payload`、`display_order` | `(asset_id,display_order)` |
| `asset_storage_objects` | `object_id`、`asset_id`、`tos_bucket`、`tos_key`、`checksum`、`visibility` | `tos_key` 唯一 |
| `upload_intents` | `upload_intent_id`、`asset_id`、`object_key`、`status`、`expires_at` | `upload_intent_id` 唯一 |
| `asset_element_types` | `element_type`、`display_name`、`status`、`schema_hint` | `element_type` 唯一 |
| `asset_element_type_change_records` | `change_id`、`schema_version`、`changed_count`、`operator_type`、`trace_id` | `(schema_version,created_at)` |

## 详细数据库表设计

### `assets`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `asset_id` | varchar(64) | 是 | 生成 | pk/unique | 资产 ID |
| `space_id` | varchar(64) | 是 |  | idx composite | 空间 ID |
| `project_id` | varchar(64) | 否 | null | idx composite | 所属项目 |
| `owner_user_id` | varchar(64) | 是 |  | idx composite | 所有者 |
| `asset_type` | varchar(32) | 是 |  | idx | `image`、`music`、`video`、`file` |
| `source_type` | varchar(32) | 是 |  | idx | `generated`、`uploaded`、`imported` |
| `status` | varchar(32) | 是 | `pending` | idx composite | `pending`、`available`、`failed`、`deleted` |
| `storage_object_key` | varchar(512) | 否 | null | idx | 主对象 key |
| `display_name` | varchar(160) | 否 | null | idx | 展示名 |
| `mime_type` | varchar(120) | 是 |  | idx | MIME |
| `size_bytes` | bigint | 是 | 0 |  | 文件大小 |
| `checksum` | varchar(128) | 否 | null | idx | 内容校验 |
| `metadata_summary` | jsonb | 是 | `{}` |  | 非敏感摘要 |
| `source_session_id` | varchar(64) | 否 | null | idx | Agent session |
| `source_run_id` | varchar(64) | 否 | null | idx | Agent run |
| `source_artifact_id` | varchar(64) | 否 | null | idx | Agent artifact |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

列表索引：`(space_id, owner_user_id, project_id, status, created_at desc)`。

### `asset_storage_objects`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `object_id` | varchar(64) | 是 | 生成 | pk/unique | 对象记录 ID |
| `asset_id` | varchar(64) | 是 |  | idx | 资产 ID |
| `tos_bucket` | varchar(120) | 是 |  | idx | TOS bucket |
| `tos_key` | varchar(512) | 是 |  | unique | TOS object key |
| `public_url` | varchar(1024) | 否 | null |  | 公开桶 URL，必要时保存 |
| `checksum` | varchar(128) | 否 | null | idx | 校验 |
| `visibility` | varchar(32) | 是 | `private` | idx | `private`、`public_snapshot` |
| `content_type` | varchar(120) | 是 |  |  | MIME |
| `size_bytes` | bigint | 是 | 0 |  | 大小 |
| `status` | varchar(32) | 是 | `pending` | idx | `pending`、`available`、`deleted` |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

### `upload_intents`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `upload_intent_id` | varchar(64) | 是 | 生成 | pk/unique | 上传意图 ID |
| `asset_id` | varchar(64) | 是 |  | idx | 预创建资产 ID |
| `space_id` | varchar(64) | 是 |  | idx | 空间 |
| `project_id` | varchar(64) | 否 | null | idx | 项目 |
| `owner_user_id` | varchar(64) | 是 |  | idx | 上传用户 |
| `object_key` | varchar(512) | 是 |  | unique | 后端生成 object key |
| `filename` | varchar(255) | 是 |  |  | 原始文件名脱敏处理 |
| `content_type` | varchar(120) | 是 |  |  | MIME |
| `size_bytes` | bigint | 是 |  |  | 预期大小 |
| `checksum` | varchar(128) | 否 | null | idx | 前端提供校验 |
| `status` | varchar(32) | 是 | `created` | idx | `created`、`confirmed`、`aborted`、`expired` |
| `safety_evidence_id` | varchar(64) | 否 | null | idx | 元数据安全证据 |
| `idempotency_key` | varchar(128) | 是 |  | unique | 创建意图幂等键 |
| `expires_at` | timestamptz | 是 |  | idx | 上传凭证过期 |
| `confirmed_at` | timestamptz | 否 | null |  | 确认时间 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

### `asset_elements`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `element_id` | varchar(64) | 是 | 生成 | pk/unique | 元素 ID |
| `asset_id` | varchar(64) | 是 |  | idx composite | 资产 ID |
| `element_type` | varchar(64) | 是 |  | idx | 内置元素类型 |
| `element_payload` | jsonb | 是 | `{}` |  | 元素内容，禁止原始敏感 Prompt |
| `display_order` | int | 是 | 0 | idx composite | 展示顺序 |
| `source_tool_call_id` | varchar(64) | 否 | null | idx | Agent Tool 调用引用 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

### `asset_element_types`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `element_type` | varchar(64) | 是 |  | pk/unique | 元素类型 key |
| `display_name` | varchar(120) | 是 |  | idx | 展示名 |
| `resource_type` | varchar(32) | 是 |  | idx | image/music/video/file |
| `status` | varchar(32) | 是 | `active` | idx | `active`、`disabled` |
| `schema_hint` | jsonb | 是 | `{}` |  | 前端渲染和校验提示 |
| `sort_order` | int | 是 | 0 | idx | 展示排序 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

### `asset_access_logs`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `access_log_id` | varchar(64) | 是 | 生成 | pk/unique | 访问日志 ID |
| `asset_id` | varchar(64) | 是 |  | idx | 资产 ID |
| `access_type` | varchar(32) | 是 |  | idx | `preview`、`download` |
| `operator_user_id` | varchar(64) | 是 |  | idx | 操作者 |
| `space_id` | varchar(64) | 是 |  | idx | 空间 |
| `result` | varchar(32) | 是 |  | idx | `allowed`、`denied` |
| `denied_reason` | varchar(128) | 否 | null |  | 拒绝原因 |
| `trace_id` | varchar(128) | 是 |  | idx | 链路追踪 |
| `created_at` | timestamptz | 是 | now() | idx | 访问时间 |

预览按配置写访问日志，下载必须写访问日志；日志不保存签名 URL。

### `asset_element_type_change_records`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `change_id` | varchar(64) | 是 | 生成 | pk/unique | 元素类型变更记录 ID |
| `schema_version` | varchar(64) | 是 |  | idx composite | 本次内置元素 schema 版本 |
| `changed_count` | int | 是 | 0 |  | 新增、启用、停用或 schema 变化数量 |
| `change_summary` | jsonb | 是 | `{}` |  | 变更摘要，不含前端实现细节 |
| `operator_type` | varchar(32) | 是 | `system` | idx | `system`、`platform_admin` |
| `operator_id` | varchar(64) | 否 | null | idx | 维护人；seed 同步为空 |
| `idempotency_key` | varchar(128) | 是 |  | unique | 同步幂等键 |
| `trace_id` | varchar(128) | 是 |  | idx | 链路追踪 |
| `created_at` | timestamptz | 是 | now() | idx composite | 变更时间 |

内置资产元素类型通过 migration/seed 或平台维护命令同步，不提供独立页面编辑任意元素类型。每次发布或变更必须写 `asset_element_type_change_records`，并写入 `business_audit_logs` 的 `asset_element_type.publish` 或 `asset_element_type.change`。

## 业务能力接口清单

| 能力 | 调用方 | 接口形态 | 核心模型 | 幂等 | 审计 |
| --- | --- | --- | --- | --- | --- |
| 资产列表 | 用户端 | HTTP `GET /api/assets` | `Asset` | 否 | 否 |
| 创建 TOS 直传上传意图 | 用户端、Agent 间接 | HTTP `POST /api/assets/upload-intents`；RPC `CreateUploadIntent` | `UploadIntent`、`Asset` | 是 | 是 |
| 确认上传完成 | 用户端 | HTTP `POST /api/assets/upload-intents/:id/confirm`；RPC `ConfirmUploadedAsset` | `AssetStorageObject` | 是 | 是 |
| 放弃上传意图 | 用户端 | HTTP `POST /api/assets/upload-intents/:id/abort` | `UploadIntent.status` | 是 | 是 |
| 资产预览/下载授权 | 用户端 | HTTP `GET /api/assets/:asset_id/access` | `AssetStorageObject` | 否 | 仅下载可审计 |
| 批量资产引用权限校验 | Agent | RPC `BatchCheckAssetAccess` | `Asset` | 否 | 否 |
| 内置资产元素类型 | 用户端、管理端、Agent | HTTP `GET /api/asset-element-types`、`GET /api/admin/asset-element-types`；RPC `PlatformDictionaryService.ListAssetElementTypes` | `AssetElementType` | 否 | 否 |
| 内置资产元素类型同步 | migration/seed、平台维护命令 | 内部函数 `SyncAssetElementTypes`，不提供独立编辑页面 | `AssetElementType`、`AssetElementTypeChangeRecord` | 是 | 是 |

## HTTP API 设计

| Method | Path | 鉴权 | Request DTO | Response DTO | 页面状态 |
| --- | --- | --- | --- | --- | --- |
| GET | `/api/assets` | user | `ListAssetsRequest` | `PageResult<AssetCardDTO>` | `loading`、`empty`、`filtered_empty` |
| GET | `/api/assets/:asset_id` | user | path `asset_id` | `AssetDetailDTO` | `loading`、`permission_denied` |
| POST | `/api/assets/upload-intents` | user | `CreateUploadIntentRequest` + `Idempotency-Key` | `UploadIntentDTO` | `uploading`、`blocked` |
| POST | `/api/assets/upload-intents/:upload_intent_id/confirm` | user | `ConfirmUploadedAssetRequest` + `Idempotency-Key` | `AssetDetailDTO` | `saving`、`success`、`save_failed` |
| POST | `/api/assets/upload-intents/:upload_intent_id/abort` | user | `AbortUploadIntentRequest` + `Idempotency-Key` | `UploadIntentDTO` | `cancelled` |
| GET | `/api/assets/:asset_id/access` | user | `GetAssetAccessRequest` | `AssetAccessDTO` | `ready`、`permission_denied` |
| GET | `/api/asset-element-types` | user | empty | `AssetElementTypeListDTO` | `loading`、`success` |
| GET | `/api/admin/asset-element-types` | admin | empty | `AssetElementTypeListDTO` | `loading`、`success` |

## DTO 设计

| DTO | 字段 |
| --- | --- |
| `ListAssetsRequest` | `project_id` 可选、`asset_type`、`source_type`、`status`、`keyword`、`PaginationRequest` |
| `CreateUploadIntentRequest` | `project_id`、`asset_type`、`filename`、`content_type`、`size_bytes`、`checksum`、`metadata_text` 可选、`safety_evidence` |
| `UploadIntentDTO` | `upload_intent_id`、`asset_id`、`bucket`、`object_key`、`upload_url`、`upload_headers`、`expires_at`、`max_size_bytes`、`content_type`、`public_url_after_confirm` 可选 |
| `ConfirmUploadedAssetRequest` | `object_key`、`etag`、`size_bytes`、`content_type`、`checksum` |
| `AbortUploadIntentRequest` | `reason` 可选 |
| `AssetCardDTO` | `asset_id`、`project_id`、`asset_type`、`source_type`、`status`、`preview_url`、`mime_type`、`size_bytes`、`created_at` |
| `AssetDetailDTO` | `asset`、`elements[]`、`project_summary`、`source_session_id`、`source_run_id`、`access_actions[]` |
| `AssetAccessDTO` | `asset_id`、`access_type` preview/download、`public_url`、`expires_at`、`content_type`、`filename` |
| `BatchAssetAccessResultDTO` | `asset_id`、`allowed`、`asset_type`、`display_name`、`preview_summary`、`denied_reason` |
| `AssetElementTypeDTO` | `element_type`、`display_name`、`status`、`schema_hint` |

## RPC 设计

### AssetService.CreateUploadIntent

请求字段：`project_id`、`asset_type`、`filename`、`content_type`、`size_bytes`、`checksum`、`metadata_text`、`safety_evidence`、`idempotency_key`。创建前校验项目 `attach_asset` 权限和安全证据。

响应字段：`upload_intent_id`、`asset_id`、`bucket`、`object_key`、`upload_url`、`upload_headers`、`expires_at`、`max_size_bytes`、`content_type`。

### AssetService.ConfirmUploadedAsset

请求字段：`upload_intent_id`、`etag`、`size_bytes`、`content_type`、`checksum`、`idempotency_key`。响应：`asset_id`、`status=available`、资产摘要。

### AssetService.BatchCheckAssetAccess

请求字段：`asset_ids[]`、`project_id`、`access_purpose=reference/preview/download`、`auth_context`。响应：每个资产 allowed、asset_type、display_name、preview_summary。

### PlatformDictionaryService.ListAssetElementTypes

调用方：Agent Skill 输出元素校验、黑板渲染前置、用户端元素类型渲染。

请求字段：`auth_context`、`request_meta`、`page_size=50`、`schema_version` 可选。响应字段：`element_types[]`、`schema_version`。

`element_types[]` 每项包含：`element_type`、`display_name`、`resource_type`、`status`、`schema_hint`、`sort_order`。业务服务只返回 `status=active` 的元素类型给 Agent。

## Application 函数

```go
type AssetApp interface {
    CreateUploadIntent(ctx context.Context, in CreateUploadIntentInput) (UploadIntentDTO, error)
    ConfirmUploadedAsset(ctx context.Context, in ConfirmUploadedAssetInput) (AssetDTO, error)
    AbortUploadIntent(ctx context.Context, in AbortUploadIntentInput) error
    BatchCheckAssetAccess(ctx context.Context, in BatchCheckAssetAccessInput) ([]AssetAccessResult, error)
    ListAssets(ctx context.Context, in ListAssetsInput) (Page[AssetListItemDTO], error)
    GetAssetDetail(ctx context.Context, in GetAssetDetailInput) (AssetDetailDTO, error)
    CreateSignedPreviewAccess(ctx context.Context, in AssetAccessInput) (SignedAccessDTO, error)
    CreateSignedDownloadAccess(ctx context.Context, in AssetAccessInput) (SignedAccessDTO, error)
    ListAssetElementTypes(ctx context.Context, in ListAssetElementTypesInput) (AssetElementTypeListDTO, error)
    SyncAssetElementTypes(ctx context.Context, in SyncAssetElementTypesInput) (AssetElementTypeSyncResultDTO, error)
}
```

## 上传约束

| 类型 | 格式 | 单文件上限 |
| --- | --- | --- |
| 图片 | jpg、jpeg、png、webp | 20MB |
| 音频 | mp3、wav、m4a | 100MB |
| 视频 | mp4、mov、webm | 500MB |
| 文档 | txt、md、pdf、doc、docx | 50MB |

上传失败不创建可用资产，不扣积分。

## 业务规则

- object key 必须由业务服务生成，遵守 TOS 规范。
- 前端直传使用短期凭证，不接触 AK/SK。
- 上传文本元数据需要安全证据。
- `upload_intent` 未确认前资产不可用。
- 资产预览/下载必须重新校验权限。
- 被移出企业后不能访问企业空间资产。
- 最终资产元素只使用平台内置 `asset_element_types`。
- 内置资产元素类型的发布、启用、停用和 schema 变化必须写变更记录和审计；审计不保存前端组件源码或私有配置。
- `SyncAssetElementTypes` 使用 `schema_version + idempotency_key` 幂等，重复同步同一版本返回同一结果。

## 事务设计

| 事务 | 原子写入 | 回滚条件 |
| --- | --- | --- |
| 创建上传意图 | `assets(status=pending)`、`upload_intents`、幂等记录、审计 | 项目无权限、安全证据无效、文件参数非法 |
| 确认上传 | `upload_intents.status=confirmed`、`asset_storage_objects`、`assets.status=available`、幂等记录、审计 | object_key 不匹配、etag/size/checksum 校验失败 |
| 放弃上传 | `upload_intents.status=aborted`、`assets.status=deleted`、幂等记录、审计 | 上传意图不可见、状态已 confirmed |
| 预览/下载授权 | `asset_access_logs` 可选/必填、短期签名响应 | 权限失败、资产不可用、TOS 签名失败 |
| 元素类型同步 | `asset_element_types` 批量 upsert、`asset_element_type_change_records`、审计、幂等记录 | schema_version 冲突、元素 key 非法 |

## 【Agent开发】需要提供的能力与参数

| 【Agent开发】场景 | 业务 RPC | Agent 必传参数 | 业务服务返回 | Agent 行为 |
| --- | --- | --- | --- | --- |
| 引用已有资产 | `BatchCheckAssetAccess` | `asset_ids[]`、`project_id`、`access_purpose=reference` | 每个资产 allowed 和摘要 | 有拒绝则阻断引用 |
| 上传素材元数据安全评估 | 业务 API 调用上传前可要求 Agent 安全能力 | `scene=asset_upload_metadata`、文本摘要 | `safety_evidence` | 业务只接受 passed 证据 |
| 生成资产保存 | 见 11 | `asset_elements[]`、产物引用 | `asset_id`、元素摘要 | Agent 保存 asset_ref，不保存长期 URL |
| 预览/下载 | 业务 API，不经 Agent | 无 | signed/public URL | Agent 事件不得携带长期媒体 URL |

## 测试

- 创建上传意图校验项目权限、归档项目、文件大小、MIME。
- 确认上传时校验 object key、etag、size、checksum。
- 未确认上传不出现在资产列表。
- 被移出企业后资产访问拒绝。
- 批量权限校验避免逐条查询。
- Agent 事件中不出现长期 TOS URL。
- 内置资产元素类型同步写 `asset_element_type_change_records` 和 `business_audit_logs`；重复同步不产生重复变更。
