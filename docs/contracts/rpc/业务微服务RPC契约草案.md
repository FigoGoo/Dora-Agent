# 业务微服务 RPC 契约草案

状态：draft
owner：文档与契约责任域；业务服务责任域确认业务语义；Agent 服务责任域提出调用需求
更新时间：2026-06-28
适用范围：智能体微服务 -> 业务系统微服务，业务 API 适配层 -> 业务系统微服务

## 成熟度复核

当前成熟度：draft，不升 `active`。  
使用方式：可作为 RPC 服务边界、通用 DTO、业务规则和错误码方向输入；字段级 request/response 以 `api/thrift/business_agent_service.thrift` 为准，索引见 `docs/contracts/字段级契约索引.md`。

已补齐项：当前 Thrift 方法索引、方法级 timeout/retry、幂等冲突语义、错误码覆盖和 contract fixture 对应关系已在本文冻结。

未冻结项：运行时配置、自动化 contract test 执行证据和服务级验收报告尚未固化，因此本文仍保持 `draft`。

## 字段级事实源

- Thrift IDL：`api/thrift/business_agent_service.thrift`
- RPC fixture：`tests/contract/fixtures/business-rpc/**`
- 业务 RPC 实现：`services/business/internal/transport/rpc/**`
- Agent RPC client：`services/agent/internal/infra/rpc/**`

新增或变更字段时，先更新 Thrift IDL 和 fixture，再同步本文档的业务规则、错误码和成熟度状态。

## 契约原则

- RPC 方法表达业务能力，不表达数据库表操作。
- 写操作必须包含 `idempotency_key`。
- 需要人工确认或高风险的写操作必须支持 preview / confirm 或等价确认语义。
- 权限、事务、业务错误码和审计由业务微服务最终解释。
- 列表查询默认 `page_size = 10`，最大值建议 `50`，排序语义必须显式定义。
- 平台字典读取可显式声明更高默认值；`ListAssetElementTypes` 是内置字典例外，缺省 `page_size = 50`，最大值 `100`。
- 不在 RPC 中返回 ORM 对象、数据库表结构或未脱敏敏感信息。

## 通用 DTO

### AuthContext

| 字段 | 类型 | 必填 | 说明 | 兼容规则 |
| --- | --- | --- | --- | --- |
| actor_user_id | string | 是 | 当前操作者用户 ID | 新增字段向后兼容 |
| login_identity_type | enum | 是 | `personal` / `enterprise_member` / `admin` | 新增枚举需兼容未知值 |
| space_id | string | 否 | 当前空间 ID | 匿名公开读可为空 |
| enterprise_id | string | 否 | 企业空间 ID | 个人空间为空 |
| enterprise_role | enum | 否 | `owner` / `member` | 第一版不扩展企业管理员 |
| admin_id | string | 否 | 平台管理员 ID | 仅后台 RPC |

### RequestMeta

| 字段 | 类型 | 必填 | 说明 | 兼容规则 |
| --- | --- | --- | --- | --- |
| request_id | string | 是 | 请求 ID | 不变 |
| trace_id | string | 是 | 链路追踪 ID | 不变 |
| idempotency_key | string | 写操作必填 | 幂等键 | 读操作可为空 |
| source | enum | 是 | `agent` / `web` / `admin` / `test` | 新增枚举兼容 |

### SafetyEvidenceDTO

业务写入统一使用 `safety_evidence` 对象。字段名以本 DTO 为准；Agent 内部可保留自己的评估记录，但对 RPC/API 只输出以下脱敏摘要。

| 字段 | 类型 | 必填 | 说明 | 兼容规则 |
| --- | --- | --- | --- | --- |
| safety_evidence_id | string | 是 | 安全评估证据 ID | 全局唯一 |
| scene | enum | 是 | `generation` / `asset_upload_metadata` / `work_share` | 新增场景需兼容未知值 |
| result | enum | 是 | `passed` / `blocked` / `failed` | 业务写入只接受 `passed` |
| target_type | enum | 是 | `prompt` / `asset_metadata` / `work_share_text` | 新增类型需兼容 |
| target_ref_id | string | 否 | 被评估对象引用，例如 artifact 或 work draft | 不作为权限依据 |
| evaluated_object_digest | string | 是 | 被评估内容摘要 | 不保存原文 |
| policy_version | string | 是 | 安全策略版本 | 用于审计 |
| evidence_version | string | 是 | 证据结构版本 | 用于兼容 |
| evaluated_at | datetime | 是 | 评估时间 | ISO8601 |
| expires_at | datetime | 否 | 证据过期时间 | 过期按 invalid 处理 |
| source_session_id | string | 否 | Agent session 引用 | 不作为业务事实 |
| source_run_id | string | 否 | Agent run 引用 | 不作为业务事实 |
| source_artifact_id | string | 否 | Agent artifact 引用 | 不作为业务事实 |
| trace_id | string | 是 | 链路追踪 | 可用于客服问题编号 |
| user_visible_reason | string | 否 | 用户可见提示 | 不包含策略细节 |

禁止在 RPC 中传递：系统 Prompt、完整组装 Prompt、供应商原始响应、模型推理链路、内部评分、命中规则细节、绕过线索、API Key 和私有对象存储 URL。

### ProjectAccessPurpose

`ProjectService.CheckProjectAccess` 使用 `access_purpose` 表达访问目的。

| 枚举 | 说明 | `archived` 项目 |
| --- | --- | --- |
| view | 查看项目、历史会话、资产、黑板和作品 | 允许 |
| continue_creation | 创建 run、追加消息、重试或继续生成 | 拒绝 |
| attach_asset | 上传或绑定新资产到项目 | 拒绝 |
| commit_asset | 保存生成资产到项目 | 拒绝 |
| create_work | 从项目继续创建新作品 | 拒绝 |

`archived` 项目拒绝创作类目的时返回 `PROJECT_ARCHIVED`，响应 detail 建议包含 `project_status=archived`、`creative_allowed=false`、`allowed_actions=["view"]`。

### UploadIntentDTO

| 字段 | 类型 | 必填 | 说明 | 兼容规则 |
| --- | --- | --- | --- | --- |
| upload_intent_id | string | 是 | 上传意图 ID | 全局唯一 |
| asset_id | string | 是 | 上传确认后创建或激活的业务资产 ID | 不变 |
| bucket | string | 是 | TOS bucket | 不变 |
| object_key | string | 是 | 后端生成的 TOS object key | 遵守 TOS 对象存储规范 |
| upload_url | string | 是 | 前端直传地址 | 过期后不可用 |
| upload_headers | map<string,string> | 是 | 前端上传必须携带的签名头 | 新增 header 兼容 |
| expires_at | datetime | 是 | 上传凭证过期时间 | 过期需重新创建 intent |
| max_size_bytes | int64 | 是 | 最大文件大小 | 不变 |
| content_type | string | 是 | 允许上传 MIME | 不变 |
| object_public_url_after_confirm | string | 否 | 确认后业务可返回的 TOS 公共 URL | 不作为确认前可用资产 |

## 当前 Thrift 方法索引

字段级 request / response 只以 `api/thrift/business_agent_service.thrift` 为准。本文只维护调用语义、执行策略、错误码和 fixture 映射。

| 服务 | 当前方法 | 类型 | fixture 域 |
| --- | --- | --- | --- |
| AccountSpaceService | ResolveCurrentSpaceContext, ResolveAuthContextFromToken | 快速读 | accountspace |
| EnterpriseService | PreviewTransferOwner, ConfirmTransferOwner | 预览 / 确认写 | enterprise |
| AdminService | CreateAdmin, DisableAdmin | 幂等后台写 | admin |
| UserAdminService | PreviewSetUserStatus, ConfirmSetUserStatus | 预览 / 确认写 | admin |
| ProjectService | CheckProjectAccess, CreateProject, UpdateProjectTitle | 快速读 / 幂等写 | project |
| ProjectAssetService | AttachAssetToProject | 幂等写 | project |
| AssetService | CreateUploadIntent, ConfirmUploadedAsset, BatchCheckAssetAccess, PrepareGeneratedAssetObjects | 快速读 / TOS 与资产写 | asset |
| CreditService | EstimateGenerationCredits, EstimateToolCredits, FreezeCredits, ChargeToolUsageCredits, ReleaseFrozenCredits | 估算读 / 幂等积分写 | credit, tool |
| AssetCreditCommitService | CommitGeneratedAssetAndCharge | 资产提交与扣费写 | asset, credit |
| SkillCatalogService | ListRoutableSkills, GetPublishedSkillSpec, GetReviewCandidateSkillSpec, SaveSkillTestResult | 配置读 / 幂等审核写 | skill |
| ToolCapabilityService | CheckToolExecutionPolicy | 快速读 | tool |
| ModelConfigService | ListAvailableGenerationModels, ResolveDefaultModel, ResolveGenerationModelSnapshot | 配置读 | model |
| PlatformDictionaryService | ListAssetElementTypes | 配置读 | asset |
| WorkService | CreateWork | 幂等作品写 | work |
| WorkShareService | PreviewShareWork, ConfirmShareWork | 预览 / 确认写 | work |
| FeaturedWorkAdminService | PreviewTakeDownWork, ConfirmTakeDownWork | 预览 / 确认写 | work |
| PublicContentService | ListPublicWorks, GetPublicWork | 公开读 | work |
| NotificationService | CreateNotification, ListNotifications, GetUnreadCount, MarkNotificationRead, MarkAllNotificationsRead | 通知读 / 幂等通知写 | notification |

## 方法级执行策略

timeout 是业务 RPC handler 的契约上限，不包含客户端排队时间。调用方可以设置更短的用户体验超时，但不得超过本表语义后继续隐藏重试。

| 策略类 | 方法 | timeout | retry | 幂等要求 | 主要错误码 |
| --- | --- | --- | --- | --- | --- |
| 快速读 | ResolveCurrentSpaceContext, ResolveAuthContextFromToken, CheckProjectAccess, BatchCheckAssetAccess, CheckToolExecutionPolicy, ListAssetElementTypes, ListPublicWorks, GetPublicWork, ListNotifications, GetUnreadCount | 800ms | transport timeout 可退避重试 1 次 | 不要求 `idempotency_key` | UNAUTHENTICATED, PERMISSION_DENIED, CROSS_SPACE_DENIED, RESOURCE_NOT_FOUND, RATE_LIMITED, UPSTREAM_TIMEOUT |
| 配置读 | ListRoutableSkills, GetPublishedSkillSpec, GetReviewCandidateSkillSpec, ListAvailableGenerationModels, ResolveDefaultModel, ResolveGenerationModelSnapshot | 1000ms | transport timeout 可退避重试 1 次 | 不要求 `idempotency_key` | PERMISSION_DENIED, RESOURCE_NOT_FOUND, STATE_CONFLICT, RATE_LIMITED, UPSTREAM_TIMEOUT |
| 预览与估算 | PreviewTransferOwner, PreviewSetUserStatus, PreviewShareWork, PreviewTakeDownWork, EstimateGenerationCredits, EstimateToolCredits | 1500ms | 默认不自动重试；用户或 Agent 可重新发起预览 / 估算 | 不要求 `idempotency_key`；如果服务生成确认 token，重复预览必须返回新 token 或同语义 token | PERMISSION_DENIED, ENTERPRISE_ROLE_REQUIRED, RESOURCE_NOT_FOUND, STATE_CONFLICT, PROJECT_ARCHIVED, CREDIT_INSUFFICIENT, SAFETY_EVIDENCE_INVALID |
| 普通幂等写 | ConfirmTransferOwner, CreateAdmin, DisableAdmin, ConfirmSetUserStatus, CreateProject, UpdateProjectTitle, AttachAssetToProject, FreezeCredits, ChargeToolUsageCredits, ReleaseFrozenCredits, SaveSkillTestResult, CreateWork, ConfirmShareWork, ConfirmTakeDownWork, CreateNotification, MarkNotificationRead, MarkAllNotificationsRead | 2000ms | 仅在 `idempotency_key` 存在且请求 hash 一致时允许 transport timeout 后退避重试 1 次 | 必填 `request_meta.idempotency_key`，同 key 不同 hash 返回 `IDEMPOTENCY_CONFLICT` | INVALID_ARGUMENT, UNAUTHENTICATED, PERMISSION_DENIED, CROSS_SPACE_DENIED, RESOURCE_NOT_FOUND, STATE_CONFLICT, PROJECT_ARCHIVED, IDEMPOTENCY_CONFLICT, SAFETY_EVIDENCE_REQUIRED, SAFETY_EVIDENCE_INVALID |
| TOS 与资产提交写 | CreateUploadIntent, ConfirmUploadedAsset, PrepareGeneratedAssetObjects, CommitGeneratedAssetAndCharge | 5000ms | 仅在 `idempotency_key` 存在且对象 key、证据摘要和扣费语义一致时允许重试 1 次 | 必填 `request_meta.idempotency_key`；request hash 必须覆盖 object key、asset/work 引用、积分金额和 `safety_evidence` 摘要 | INVALID_ARGUMENT, PERMISSION_DENIED, RESOURCE_NOT_FOUND, PROJECT_ARCHIVED, CREDIT_INSUFFICIENT, IDEMPOTENCY_CONFLICT, SAFETY_EVIDENCE_REQUIRED, SAFETY_EVIDENCE_INVALID, SAFETY_BLOCKED, UPSTREAM_TIMEOUT |

## 幂等和冲突规则

- 写方法必须记录 `idempotency_key`、`request_hash`、`trace_id`、业务结果引用和状态。
- 同一 `idempotency_key`、同一 `request_hash` 重放时返回第一次成功结果或当前已确认结果，不重复扣费、不重复创建业务事实。
- 同一 `idempotency_key`、不同 `request_hash` 必须返回 `IDEMPOTENCY_CONFLICT`，不得尝试合并请求。
- `request_hash` 必须包含权限主体、目标资源、写入字段、确认 token、积分金额、TOS object key、安全证据 ID 和被评估对象摘要。
- 预览方法不产生最终业务事实；确认方法必须校验 preview token、权限上下文、资源版本和安全证据仍然有效。
- 超时后的重试只能发生在调用方无法确认服务端是否已完成时；服务端必须以幂等记录作为唯一判定依据。

## 错误码覆盖

| 方法域 | 必须覆盖的错误码 |
| --- | --- |
| accountspace | UNAUTHENTICATED, PERMISSION_DENIED, CROSS_SPACE_DENIED |
| enterprise | ENTERPRISE_ROLE_REQUIRED, RESOURCE_NOT_FOUND, STATE_CONFLICT, IDEMPOTENCY_CONFLICT |
| admin | PERMISSION_DENIED, RESOURCE_NOT_FOUND, STATE_CONFLICT, IDEMPOTENCY_CONFLICT |
| project | CROSS_SPACE_DENIED, RESOURCE_NOT_FOUND, PROJECT_ARCHIVED, IDEMPOTENCY_CONFLICT |
| asset | RESOURCE_NOT_FOUND, PROJECT_ARCHIVED, SAFETY_EVIDENCE_REQUIRED, SAFETY_EVIDENCE_INVALID, UPSTREAM_TIMEOUT, IDEMPOTENCY_CONFLICT |
| credit | CREDIT_INSUFFICIENT, STATE_CONFLICT, UPSTREAM_TIMEOUT, IDEMPOTENCY_CONFLICT |
| skill | PERMISSION_DENIED, RESOURCE_NOT_FOUND, STATE_CONFLICT, IDEMPOTENCY_CONFLICT |
| tool | PERMISSION_DENIED, STATE_CONFLICT, RATE_LIMITED |
| model | RESOURCE_NOT_FOUND, STATE_CONFLICT, UPSTREAM_TIMEOUT |
| work | PROJECT_ARCHIVED, SAFETY_EVIDENCE_REQUIRED, SAFETY_EVIDENCE_INVALID, SAFETY_BLOCKED, IDEMPOTENCY_CONFLICT |
| notification | RESOURCE_NOT_FOUND, STATE_CONFLICT, IDEMPOTENCY_CONFLICT |

## Fixture 映射

| fixture 域 | 路径 | 覆盖范围 |
| --- | --- | --- |
| accountspace | `tests/contract/fixtures/business-rpc/accountspace/scenarios.json` | ResolveCurrentSpaceContext, ResolveAuthContextFromToken |
| enterprise | `tests/contract/fixtures/business-rpc/enterprise/scenarios.json` | PreviewTransferOwner, ConfirmTransferOwner |
| admin | `tests/contract/fixtures/business-rpc/admin/scenarios.json` | CreateAdmin, DisableAdmin, PreviewSetUserStatus, ConfirmSetUserStatus |
| project | `tests/contract/fixtures/business-rpc/project/scenarios.json`, `tests/contract/fixtures/business-rpc/project/write_scenarios.json` | CheckProjectAccess, CreateProject, UpdateProjectTitle, AttachAssetToProject |
| asset | `tests/contract/fixtures/business-rpc/asset/scenarios.json`, `tests/contract/fixtures/business-rpc/asset/upload_scenarios.json`, `tests/contract/fixtures/business-rpc/prepare_generated_asset_objects_success.json`, `tests/contract/fixtures/business-rpc/commit_generated_asset_and_charge_success.json`, `tests/contract/fixtures/business-rpc/list_asset_element_types_success.json` | CreateUploadIntent, ConfirmUploadedAsset, BatchCheckAssetAccess, PrepareGeneratedAssetObjects, CommitGeneratedAssetAndCharge, ListAssetElementTypes |
| credit | `tests/contract/fixtures/business-rpc/credit/scenarios.json`, `tests/contract/fixtures/business-rpc/estimate_generation_with_tool_items_success.json`, `tests/contract/fixtures/business-rpc/estimate_generation_business_error_credit_insufficient.json`, `tests/contract/fixtures/business-rpc/freeze_credits_success.json`, `tests/contract/fixtures/business-rpc/freeze_credits_idempotency_conflict.json` | EstimateGenerationCredits, FreezeCredits, ReleaseFrozenCredits, CommitGeneratedAssetAndCharge |
| tool | `tests/contract/fixtures/business-rpc/tool/scenarios.json`, `tests/contract/fixtures/business-rpc/estimate_tool_credits_success.json`, `tests/contract/fixtures/business-rpc/charge_tool_usage_success.json`, `tests/contract/fixtures/business-rpc/charge_tool_usage_duplicate_conflict.json` | CheckToolExecutionPolicy, EstimateToolCredits, ChargeToolUsageCredits |
| skill | `tests/contract/fixtures/business-rpc/skill/scenarios.json`, `tests/contract/fixtures/business-rpc/list_routable_skills_version_compat.json`, `tests/contract/fixtures/business-rpc/get_review_candidate_skill_spec_success.json`, `tests/contract/fixtures/business-rpc/save_skill_test_result_success.json` | ListRoutableSkills, GetPublishedSkillSpec, GetReviewCandidateSkillSpec, SaveSkillTestResult |
| model | `tests/contract/fixtures/business-rpc/model/scenarios.json`, `tests/contract/fixtures/business-rpc/resolve_default_model_timeout.json`, `tests/contract/fixtures/business-rpc/resolve_generation_model_snapshot_success.json` | ListAvailableGenerationModels, ResolveDefaultModel, ResolveGenerationModelSnapshot |
| work | `tests/contract/fixtures/business-rpc/work/scenarios.json` | CreateWork, PreviewShareWork, ConfirmShareWork, PreviewTakeDownWork, ConfirmTakeDownWork, ListPublicWorks, GetPublicWork |
| notification | `tests/contract/fixtures/business-rpc/notification/scenarios.json` | CreateNotification, ListNotifications, GetUnreadCount, MarkNotificationRead, MarkAllNotificationsRead |

## 关键业务规则

### 项目归档

- `ProjectService.CheckProjectAccess(access_purpose=view)` 对 `archived` 项目允许读取。
- `continue_creation`、`attach_asset`、`commit_asset`、`create_work` 对 `archived` 项目必须拒绝。
- Agent 创建 session/run、resume、retry、confirm 和保存生成资产前都必须先按访问目的校验项目。
- 运行中发现项目归档时，Agent 不再发起新 Tool，释放未结算冻结积分，并发出归档阻断 AG-UI 事件。

### 内容安全证据

- `CommitGeneratedAssetAndCharge`、`CreateUploadIntent` / `ConfirmUploadedAsset` 的文本元数据、`ShareWork` / `PreviewShareWork` 必须接收 `safety_evidence`。
- 业务只接受 `safety_evidence.result=passed`，并校验摘要、过期时间、场景和当前写入对象匹配。
- 业务可保存脱敏证据摘要到 `content_safety_evidences`，并在资产或公开快照上冗余 `safety_evidence_id`、`safety_policy_version`、`safety_checked_at`、`safety_text_digest`。
- 写入幂等的 `request_hash` 必须包含 `safety_evidence_id` 和 `evaluated_object_digest`；同一幂等键更换证据或摘要返回 `IDEMPOTENCY_CONFLICT`。

### 公开媒体访问

- TOS 使用公共桶，公开详情返回 `public_media_url` 或公开媒体引用，URL 来自公开快照媒体前缀。
- 公开作品必须使用 `public/works` 快照媒体前缀，不直接返回源业务资产 object key。
- 取消分享或后台下架后，公开快照变为不可访问；第一版不做 CDN 缓存失效，已泄露或已缓存 CDN URL 不承诺即时失效。
- 已下架作品重新公开时必须生成新的公开快照媒体 object key，不能复用旧 key。
- 私有业务资产的预览和下载只通过登录态 Asset API 返回授权后的 TOS 公共 URL；管理员后台不直接下载用户私有源资产。

### TOS object key 和上传签名

- object key 必须由业务服务生成，并遵守 [TOS 对象存储规范](../../standards/TOS对象存储规范.md)。
- 前端直传使用 `CreateUploadIntent` 返回的短期上传凭证，不接触 TOS AK/SK。
- `CreateUploadIntent` 必须校验登录态、空间、项目权限、项目归档状态、文件 MIME、大小和安全证据。
- `ConfirmUploadedAsset` 必须校验 `object_key`、`etag`、`size_bytes`、`content_type`、`checksum` 和 upload intent 是否一致。
- 未确认上传不得进入可用资产列表；放弃或过期上传由清理任务删除。

## 错误码

| 错误码 | 类型 | 含义 | 是否可重试 | 前端/Agent 行为 |
| --- | --- | --- | --- | --- |
| UNAUTHENTICATED | 权限 | 未登录或 token 无效 | 否 | 前端弹登录；Agent 终止 run |
| PERMISSION_DENIED | 权限 | 无资源权限 | 否 | 展示权限不足 |
| CROSS_SPACE_DENIED | 权限 | 跨空间访问 | 否 | 停止操作并提示切换空间 |
| ENTERPRISE_ROLE_REQUIRED | 业务 | 需要企业 owner 权限 | 否 | 禁用操作 |
| RESOURCE_NOT_FOUND | 业务 | 资源不存在或不可见 | 否 | 展示不存在 |
| STATE_CONFLICT | 业务 | 状态不允许操作 | 否 | 刷新资源状态 |
| PROJECT_ARCHIVED | 业务 | 项目已归档，不允许继续创作 | 否 | 工作台切只读，禁用创作入口 |
| IDEMPOTENCY_CONFLICT | 幂等 | 同 key 参数不一致 | 否 | 记录错误并阻断重试 |
| CREDIT_INSUFFICIENT | 业务 | 积分不足 | 否 | 展示充值或兑换入口 |
| REDEEM_CODE_INVALID | 业务 | 兑换码无效 | 否 | 展示错误 |
| SAFETY_EVIDENCE_REQUIRED | 业务 | 缺少内容安全证据 | 否 | Agent 重新执行安全评估 |
| SAFETY_EVIDENCE_INVALID | 业务 | 安全证据过期、摘要不匹配或场景不匹配 | 否 | 重新安全评估 |
| SAFETY_BLOCKED | 业务 | 内容安全评估不通过 | 否 | 展示安全阻断提示 |
| RATE_LIMITED | 系统 | 限流 | 是 | 退避重试 |
| UPSTREAM_TIMEOUT | 系统 | 上游超时 | 是 | 幂等场景可重试 |

## contract test

- 正常路径：空间解析、项目创建、Skill 池查询、积分冻结、资产保存并扣费、公开作品读取。
- 业务错误：余额不足、项目归档、Skill 未发布、Tool 停用、安全证据缺失或失效。
- 权限错误：跨空间访问、企业 member 操作 owner 功能、后台访问私有创作内容。
- 超时：模型配置解析、资产保存、TOS 上传签名和上传确认。
- 幂等：重复创建项目、重复冻结、重复扣费、重复分享、重复下架和重复通知标记。
- 版本兼容：新增字段前后端忽略未知字段。

## 待确认

- 模型 API Key 加密和轮换策略。
- TOS 已接入 CDN，域名为 `https://tos.doraigc.com`；第一版不做 CDN 缓存失效。
