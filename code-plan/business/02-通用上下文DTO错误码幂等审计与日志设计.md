# 02-通用上下文 DTO 错误码幂等审计与日志设计

状态：production-design-ready  
owner：业务微服务后端工程师  
更新时间：2026-06-27  
适用范围：业务服务所有 RPC、DTO、错误、幂等、审计、日志和测试  
相关代码路径：`api/thrift/common/**`、`api/thrift/business/**`、`services/business/internal/domain/common/**`、`services/business/internal/pkg/errors/**`、`services/business/internal/infra/idempotency/**`

## 目标

提供所有业务域复用的 RPC DTO、错误码、幂等、审计和日志规范。后续 03～13 文档不得重新定义一套冲突的上下文字段。

## 通用 Thrift DTO

### AuthContext

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `actor_user_id` | string | 私有用户请求必填 | 当前操作者用户 ID |
| `login_identity_type` | enum | 是 | Thrift `LoginIdentityType`：`PERSONAL`、`ENTERPRISE_MEMBER`、`ADMIN`。匿名公开 HTTP 不进入 Agent RPC `AuthContext` |
| `space_id` | string | 私有资源必填 | 当前空间 ID |
| `enterprise_id` | string | 企业身份必填 | 当前企业 ID |
| `enterprise_role` | enum | 企业身份必填 | `owner`、`member` |
| `admin_id` | string | 后台请求必填 | 平台管理员 ID |

### RequestMeta

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `request_id` | string | 是 | 单次 RPC 请求 ID |
| `trace_id` | string | 是 | 链路追踪 ID |
| `source` | string | 是 | Thrift 为 required string；稳定值：`agent_service`、`web`、`admin`、`public`、`test`。兼容读取历史值 `agent` |
| `idempotency_key` | string | 写操作必填 | 幂等键 |

客户端请求编号不是 Thrift `RequestMeta` 字段；HTTP 或 Agent 本地如需要保留该编号，只能通过 `X-Client-Request-ID` 进入日志和 request hash 忽略列表，不得写入 `RequestMeta` 或跨服务 RPC 契约。

### PaginationRequest / PaginationResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | int32 | 否 | 页码模式，从 1 开始 |
| `page_token` | string | 否 | 游标模式，和 `page` 二选一 |
| `page_size` | int32 | 否 | 默认 10，最大 50 |
| `sort_by` | string | 否 | 允许字段白名单 |
| `sort_direction` | enum | 否 | `asc`、`desc` |
| `next_page_token` | string | 响应可选 | 下一页游标 |
| `has_more` | bool | 响应必填 | 是否还有更多 |
| `total` | int64 | 响应可选 | 仅后台强需求时返回 |

### SafetyEvidenceDTO

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `safety_evidence_id` | string | 是 | Agent 安全评估证据 ID |
| `scene` | enum | 是 | `generation`、`asset_upload_metadata`、`work_share`、`skill_test` |
| `result` | enum | 是 | 业务写入只接受 `passed` |
| `target_type` | enum | 是 | `prompt`、`asset_metadata`、`work_share_text`、`skill_test_prompt` |
| `target_ref_id` | string | 否 | Agent artifact、work draft 或上传意图引用 |
| `evaluated_object_digest` | string | 是 | 被评估内容摘要，不是原文 |
| `policy_version` | string | 是 | 安全策略版本 |
| `evidence_version` | string | 是 | 证据结构版本 |
| `evaluated_at` | datetime | 是 | 评估完成时间 |
| `expires_at` | datetime | 否 | 证据过期时间 |
| `source_session_id` | string | 否 | Agent session 引用 |
| `source_run_id` | string | 否 | Agent run 引用 |
| `source_artifact_id` | string | 否 | Agent artifact 引用 |
| `trace_id` | string | 是 | 追踪 ID |

业务服务不得接收或保存系统 Prompt、完整生成 Prompt、Skill 测试组装 Prompt、模型推理链路、安全规则命中细节、供应商原始响应、API Key。

安全证据场景约束：

| `scene` | `target_type` | 适用业务写入 | 过期规则 |
| --- | --- | --- | --- |
| `generation` | `prompt` | `EstimateGenerationCredits`、`CommitGeneratedAssetAndCharge` | 必须未过期；推荐 TTL 30 分钟。 |
| `asset_upload_metadata` | `asset_metadata` | `CreateUploadIntent`、`ConfirmUploadedAsset` 的文本元数据 | 必须未过期；推荐 TTL 30 分钟。 |
| `work_share` | `work_share_text` | `PreviewShareWork`、`ShareWork` | 必须未过期；推荐 TTL 30 分钟。 |
| `skill_test` | `skill_test_prompt` | `SaveSkillTestResult.safety_evidence_json` | 必须未过期；推荐 TTL 30 分钟；仅作为审核测试证据，不代表 Skill 可绕过发布后安全评估。 |

`result` 的合法值为 `passed`、`blocked`、`failed`，业务写入和 Skill 测试成功结果只接受 `passed`。历史 fixture 或旧实现中的 `pass` 必须拒绝并迁移为 `passed`。

## 通用 HTTP API DTO

HTTP API DTO 是前端展示契约，不能直接复用 ORM model，也不能把 Agent Runtime DTO 原样透出。每个业务领域必须按场景定义 request / response；通用 wrapper 只解决错误、trace、分页和列表结构。

### ApiResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | string | 是 | 成功固定为 `OK`，失败为稳定业务错误码 |
| `message` | string | 是 | 成功为 `ok` 或业务成功摘要，失败为用户可理解摘要 |
| `data` | object/null | 成功时按接口必填 | 具体场景响应 DTO；OpenAPI 必须使用字段级 DTO，不允许任意 object |
| `error` | ApiError | 失败时必填 | 稳定错误 |
| `trace_id` | string | 是 | 链路追踪 ID |

### ApiError

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | string | 是 | 与 `BusinessError.code` 一致 |
| `message` | string | 是 | 用户可理解摘要 |
| `retryable` | bool | 是 | 前端是否可展示重试 |
| `detail` | object | 否 | 非敏感辅助字段，例如 `field`、`min`、`max` |

### PageResult

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | list | 是 | 当前页数据 |
| `page` | int32 | 页码模式必填 | 从 1 开始 |
| `page_size` | int32 | 是 | 默认 10，最大 50 |
| `total` | int64 | 后台列表可选 | 用户端默认不强制返回总数 |
| `next_page_token` | string | 游标模式可选 | 下一页游标 |
| `has_more` | bool | 是 | 是否还有下一页 |

### HTTP Header 约定

| Header | 必填 | 场景 | 说明 |
| --- | --- | --- | --- |
| `X-Request-ID` | 否 | 全部 | 缺失时服务端生成 |
| `X-Trace-ID` | 否 | 全部 | 缺失时服务端生成，响应必须返回 |
| `Idempotency-Key` | 写操作必填 | POST / PATCH / DELETE | 进入 `RequestMeta.idempotency_key` |
| `X-Admin-Reason` | 后台高风险写必填 | 管理端 | 进入审计 `reason`，脱敏保存 |
| `X-Client-Request-ID` | 否 | 前端或 Agent 本地调试 | 只进入 HTTP access log 和幂等 hash 忽略列表，不进入 Thrift `RequestMeta` |

## 前端状态映射

| BusinessError.code | HTTP status | 用户端状态 | 管理端状态 |
| --- | --- | --- | --- |
| `UNAUTHENTICATED` | 401 | `login_required` | 后台登录失效，跳转 `/admin/login` |
| `PERMISSION_DENIED`、`CROSS_SPACE_DENIED` | 403 | `permission_denied` | `permission_denied` |
| `RESOURCE_NOT_FOUND` | 404 | `empty` 或详情不可见 | 详情不可见 |
| `STATE_CONFLICT` | 409 | `error` 或 `archived_readonly` | `confirming` 后失败 |
| `PROJECT_ARCHIVED` | 409 | `archived_readonly` | 不适用 |
| `CREDIT_INSUFFICIENT` | 409 | `insufficient` | 不适用 |
| `REDEEM_CODE_INVALID`、`REDEEM_CODE_EXPIRED`、`REDEEM_CODE_USED`、`REDEEM_CODE_TARGET_MISMATCH` | 400/403/409 | `redeem_failed` | 后台批次状态不可用 |
| `SAFETY_EVIDENCE_REQUIRED`、`SAFETY_EVIDENCE_INVALID` | 400 | `blocked` | 审核或保存失败 |
| `RATE_LIMITED` | 429 | `error` 可重试 | `error` 可重试 |
| `UPSTREAM_TIMEOUT` | 504 | `timeout` 可重试 | `testing` -> `failed` |

## 统一错误码

| 错误码 | HTTP 映射 | Kitex 分类 | 是否可重试 | 说明 |
| --- | --- | --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | business | 否 | 参数非法 |
| `MISSING_REQUIRED_FIELD` | 400 | business | 否 | 必填字段缺失 |
| `UNAUTHENTICATED` | 401 | auth | 否 | 缺少登录态 |
| `PERMISSION_DENIED` | 403 | auth | 否 | 无资源权限 |
| `CROSS_SPACE_DENIED` | 403 | auth | 否 | 跨空间访问 |
| `ENTERPRISE_ROLE_REQUIRED` | 403 | business | 否 | 需要企业 owner |
| `RESOURCE_NOT_FOUND` | 404 | business | 否 | 资源不存在或不可见 |
| `RESOURCE_UNAVAILABLE` | 409 | business | 否 | 资源停用或不可用 |
| `STATE_CONFLICT` | 409 | business | 否 | 当前状态不允许 |
| `PROJECT_ARCHIVED` | 409 | business | 否 | 项目归档，不允许创作写入 |
| `IDEMPOTENCY_CONFLICT` | 409 | business | 否 | 同 key 请求摘要不一致 |
| `CREDIT_INSUFFICIENT` | 409 | business | 否 | 积分不足 |
| `CREDIT_FREEZE_NOT_FOUND` | 404 | business | 否 | 冻结记录不存在、不可见或状态不匹配 |
| `CREDIT_ESTIMATE_EXCEEDED` | 409 | business | 否 | Tool 实际计费数量超过确认过的预估明细 |
| `TOOL_PRICING_POLICY_MISSING` | 409 | business | 否 | Tool 需要计费但缺少可用计价策略 |
| `REDEEM_CODE_INVALID` | 400 | business | 否 | 兑换码不存在、格式非法或 hash 不匹配 |
| `REDEEM_CODE_EXPIRED` | 409 | business | 否 | 兑换码已过期 |
| `REDEEM_CODE_USED` | 409 | business | 否 | 兑换码已被兑换 |
| `REDEEM_CODE_TARGET_MISMATCH` | 403 | business | 否 | 兑换码绑定目标和当前用户、企业或渠道不匹配 |
| `SAFETY_EVIDENCE_REQUIRED` | 400 | business | 否 | 缺少安全证据 |
| `SAFETY_EVIDENCE_INVALID` | 400 | business | 否 | 安全证据过期、摘要不匹配或场景不匹配 |
| `ASSET_ELEMENT_INVALID` | 400 | business | 否 | 资产元素类型非法、必填元素缺失或 payload 不符合 schema |
| `ASSET_OBJECT_PREPARE_FAILED` | 502 | system | 是 | 生成产物对象槽或短期上传授权创建失败 |
| `ASSET_SAVE_FAILED` | 502 | system | 是 | 对象存储或资产保存依赖失败 |
| `RATE_LIMITED` | 429 | system | 是 | 限流 |
| `UPSTREAM_TIMEOUT` | 504 | system | 是 | 上游依赖超时 |
| `INTERNAL_ERROR` | 500 | system | 否 | 未分类内部错误 |

## BusinessError DTO

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | string | 是 | 稳定错误码 |
| `message` | string | 是 | 用户可理解摘要 |
| `detail` | map<string,string> | 否 | 非敏感辅助信息 |
| `retryable` | bool | 是 | 是否可重试 |
| `trace_id` | string | 是 | 支持排障 |

## 幂等记录模型

表：`idempotency_records`

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `tenant_id` | varchar(64) | 是 | uniq(tenant,scope,key) | 租户 ID；个人空间用 `space_id`，平台级后台操作用 `platform` |
| `space_id` | varchar(64) | 否 | idx | 业务空间；后台平台操作可为空 |
| `idempotency_key` | varchar(128) | 是 | uniq(tenant,scope,key) | 调用方提供 |
| `scope` | varchar(128) | 是 | uniq(tenant,scope,key) | 业务域，例如 `credit.freeze` |
| `request_hash` | varchar(128) | 是 | idx | 规范化请求摘要 |
| `result_ref_type` | varchar(64) | 否 |  | 结果资源类型 |
| `result_ref_id` | varchar(128) | 否 | idx | 结果资源 ID |
| `status` | varchar(32) | 是 | idx | `processing`、`succeeded`、`failed` |
| `error_code` | varchar(64) | 否 | idx | 失败错误码 |
| `expires_at` | timestamptz | 是 | idx | 过期清理时间 |
| `created_at` | timestamptz | 是 | idx | 创建时间 |
| `updated_at` | timestamptz | 是 |  | 更新时间 |

唯一约束：`(tenant_id, scope, idempotency_key)`。无数据库级外键；`result_ref_id` 只作为普通引用。

## 业务表公共字段基线

除纯字典 seed 表和明确说明的 append-only 日志表外，业务表默认包含以下公共字段；各领域文档如省略，应在 migration 和 GORM model 中补齐，不得在实现时再发明不同命名。

| 字段 | 类型 | 必填 | 适用范围 | 说明 |
| --- | --- | --- | --- | --- |
| `tenant_id` | varchar(64) | 私有业务表必填 | 用户/企业空间私有数据、幂等、审计 | 个人空间使用 `space_id`，企业空间使用企业 `space_id`，平台后台全局表可用 `platform` |
| `space_id` | varchar(64) | 空间资源必填 | 项目、资产、作品、积分、Skill、通知 | 当前业务空间 ID |
| `created_by` | varchar(64) | 写入型业务表必填 | 用户或管理员创建的资源 | 创建人 ID；系统 seed 可为 `system` |
| `updated_by` | varchar(64) | 可更新业务表必填 | 可编辑资源 | 最近更新人 ID |
| `created_at` | timestamptz | 是 | 全部业务表 | 创建时间 |
| `updated_at` | timestamptz | 可更新表必填 | 非 append-only 表 | 更新时间 |
| `deleted_at` | timestamptz | 可软删表必填 | 用户可删除/隐藏资源 | 软删除时间；状态机型资源如 `archived/taken_down` 不等价于删除 |

例外规则：

- append-only 表如 `business_audit_logs`、`credit_ledger_entries`、`work_moderation_records` 不更新历史行，可不含 `updated_at` 和 `deleted_at`。
- 强业务状态表可以用 `status` 表达业务生命周期，但用户删除或后台清理仍应使用 `deleted_at` 或独立删除记录，不能把业务状态和删除状态混在一起。
- 所有公共字段都是普通列和索引，不创建数据库级外键。

## 幂等函数设计

```go
type IdempotencyGuard interface {
    Begin(ctx context.Context, tenantID string, spaceID string, scope string, key string, requestHash string) (Decision, error)
    Succeed(ctx context.Context, record IdempotencyRecord, result ResultRef) error
    Fail(ctx context.Context, record IdempotencyRecord, errCode string) error
}

type Decision struct {
    Mode string // proceed, replay, conflict, processing
    Record IdempotencyRecord
    ReplayResult *ResultRef
}
```

处理规则：

- `proceed`：第一次请求，进入业务事务。
- `replay`：同 key 同 hash 已成功，返回同一业务结果。
- `conflict`：同 key 不同 hash，返回 `IDEMPOTENCY_CONFLICT`。
- `processing`：同 key 正在处理，返回 `STATE_CONFLICT` 或等待策略。

## Request Hash 规则

所有写 RPC 和写 HTTP API 必须在进入 application 前生成规范化 request hash。hash 用于幂等冲突判断，不用于安全签名。

规范化规则：

| 规则 | 说明 |
| --- | --- |
| JSON canonical | 请求体按字段名升序序列化，忽略空白和字段顺序差异。 |
| 忽略字段 | 忽略 `request_id`、`trace_id`、HTTP `X-Client-Request-ID`、时间戳类观测字段。 |
| 保留字段 | 保留业务参数、目标资源、操作者、空间、`safety_evidence_id`、`evaluated_object_digest`、金额和状态目标。 |
| 数字格式 | 金额、积分和数量转为十进制字符串，避免浮点序列化差异。 |
| 列表顺序 | 有业务顺序的列表保留顺序；无顺序集合先按 ID 排序。 |
| hash 算法 | `sha256(canonical_json)`，保存 hex 字符串。 |

各写操作 scope：

| Scope | 关键字段 |
| --- | --- |
| `auth.register` | `email_hash`、`phone_hash`、`display_name`、`invite_token_hash` |
| `account.switch_identity` | `actor_user_id`、`target_identity_type`、`target_enterprise_id` |
| `enterprise.create` | `actor_user_id`、`enterprise_name_normalized` |
| `enterprise.invite` | `enterprise_id`、`target_email_hash`、`target_phone_hash` |
| `enterprise.remove_member` | `enterprise_id`、`member_id`、`reason` |
| `enterprise.transfer_owner` | `enterprise_id`、`target_member_id`、`preview_token` |
| `admin.create` | `admin_account`、`operator_admin_id` |
| `admin.password.rotate` | `admin_id`、`new_password_digest`、`must_rotate_password_before` |
| `admin.user_status` | `target_user_id`、`target_status`、`preview_token`、`reason` |
| `project.create` | `space_id`、`owner_user_id`、`title`、`source` |
| `project.archive` / `project.restore` | `project_id`、`target_status`、`reason` |
| `model.provider.upsert` | `provider_id`、`provider_key`、`key_fingerprint`、`status` |
| `model.upsert` | `model_id`、`provider_id`、`internal_model_name`、`pricing_snapshot` |
| `tool.policy.update` | `tool_name`、`risk_level`、`timeout_ms`、`retry_policy`、`cancel_policy` |
| `skill.draft.upsert` | `skill_id`、`version_id`、`content_snapshot_digest` |
| `skill.review.confirm` | `review_id`、`result`、`comment`、`reviewer_admin_id` |
| `credit.freeze` | `estimate_id`、`points`、`run_id`、`account_id` |
| `credit.release` | `freeze_id`、`release_points`、`reason`、`run_id` |
| `credit.redeem` | `code_hash`、`account_id`、`redeemed_by`、`target_account_type`、`redeem_channel` |
| `asset.element_type.sync` | `schema_version`、`changed_element_types[]`、`operator_type` |
| `asset.upload_intent` | `project_id`、`filename_digest`、`content_type`、`size_bytes`、`checksum`、`safety_evidence_id` |
| `asset.upload_confirm` | `upload_intent_id`、`object_key`、`etag`、`size_bytes`、`checksum` |
| `asset.commit_charge` | `project_id`、`freeze_id`、`session_id`、`run_id`、`artifact_ids[]`、`safety_evidence_id` |
| `work.create` / `work.update` | `work_id` 可选、`project_id`、`title`、`asset_ids[]`、`cover_asset_id` |
| `work.share` | `work_id`、`public_title`、`public_description`、`tags[]`、`safety_evidence_id` |
| `work.like` | `public_work_id`、`user_id`、`target_status` |
| `notification.create` | `recipient_user_id`、`type`、`related_resource_type`、`related_resource_id` |

所有 scope 的 request hash 必须把 `tenant_id`、`space_id`、`actor_user_id/admin_id` 纳入 canonical JSON；公开匿名接口没有幂等写入时不创建幂等记录。

## 审计日志模型

表：`business_audit_logs`

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| `audit_id` | varchar(64) | 是 | pk | 审计 ID |
| `trace_id` | varchar(128) | 是 | idx | 链路追踪 |
| `operator_type` | varchar(32) | 是 | idx | `user`、`enterprise_owner`、`platform_admin`、`system` |
| `operator_id` | varchar(64) | 否 | idx | 操作者 ID |
| `tenant_id` | varchar(64) | 是 | idx | 租户 |
| `space_id` | varchar(64) | 否 | idx | 空间 |
| `business_action` | varchar(128) | 是 | idx | 业务动作 |
| `resource_type` | varchar(64) | 是 | idx | 资源类型 |
| `resource_id` | varchar(64) | 否 | idx | 资源 ID |
| `before_status` | varchar(64) | 否 |  | 前状态 |
| `after_status` | varchar(64) | 否 |  | 后状态 |
| `reason` | varchar(512) | 否 |  | 操作原因，脱敏 |
| `result` | varchar(32) | 是 | idx | `success`、`failed`、`blocked` |
| `error_code` | varchar(64) | 否 | idx | 失败错误码 |
| `metadata_summary` | jsonb | 否 |  | 非敏感摘要 |
| `created_at` | timestamptz | 是 | idx | 创建时间 |

## 日志输出点

| 位置 | level | 字段 |
| --- | --- | --- |
| RPC 入站 | info | `trace_id`、`rpc_method`、`source`、`operator_id` |
| 权限拒绝 | warn | `business_action`、`resource_type`、`error_code` |
| 幂等冲突 | warn | `scope`、`idempotency_key`、`request_hash` |
| 事务提交成功 | info | `business_action`、`resource_id`、`latency_ms` |
| 外部依赖失败 | error | `dependency`、`error_code`、`retryable` |
| panic recover | error | `trace_id`、`rpc_method`、stack 摘要 |

## 【Agent开发】需要提供的通用参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `AuthContext.actor_user_id` | string | 是 | 当前用户，不能仅信任前端原样透传 |
| `AuthContext.login_identity_type` | enum | 是 | `PERSONAL`、`ENTERPRISE_MEMBER`、`ADMIN`，后台 Skill 测试结果回写使用 `ADMIN` |
| `AuthContext.space_id` | string | 是 | 当前空间 |
| `RequestMeta.trace_id` | string | 是 | Agent run 全链路 trace |
| `RequestMeta.source` | string | 是 | Agent RPC 固定传 `agent_service`；业务服务兼容读取历史值 `agent` |
| `RequestMeta.idempotency_key` | string | 写操作必填 | run/interrupt/tool 维度稳定生成 |
| `SafetyEvidenceDTO` | object | 生成资产保存、上传元数据、作品分享前必填 | 安全评估脱敏证据 |

## 测试要求

- 错误码映射 contract test 覆盖正常、业务错误、权限错误、系统错误。
- 幂等测试覆盖 replay、conflict、processing。
- 幂等测试必须覆盖相同 `idempotency_key` 在不同 `tenant_id/space_id` 下互不影响，同一 `tenant_id/scope/key` 参数变化返回 `IDEMPOTENCY_CONFLICT`。
- 审计测试验证高风险写操作审计与业务状态同事务或有补偿记录。
- 日志脱敏测试禁止 API Key、完整兑换码、系统 Prompt、供应商原始响应、私密素材内容。
- 分页 DTO 测试验证默认 10、最大 50、非法 page_size 返回 `INVALID_ARGUMENT`。
