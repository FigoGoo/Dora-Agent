# 业务微服务 RPC 契约草案

状态：draft
owner：主控 Codex 汇总维护；业务微服务后端工程师确认业务语义；Go Eino 智能体微服务架构工程师提出调用需求
更新时间：2026-06-25
适用范围：智能体微服务 -> 业务系统微服务，业务 API 适配层 -> 业务系统微服务

## 契约原则

- RPC 方法表达业务能力，不表达数据库表操作。
- 写操作必须包含 `idempotency_key`。
- 需要人工确认或高风险的写操作必须支持 preview / confirm 或等价确认语义。
- 权限、事务、业务错误码和审计由业务微服务最终解释。
- 列表查询默认 `page_size = 10`，最大值建议 `50`，排序语义必须显式定义。
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

## 首批服务清单

| 服务名 | 方法 | 调用方 | 类型 | 幂等 | 说明 |
| --- | --- | --- | --- | --- | --- |
| AccountSpaceService | ResolveCurrentSpaceContext | Agent / API | 读 | 否 | 解析当前空间、积分账户、Skill 池上下文 |
| AccountSpaceService | ListAvailableSpaces | API | 读 | 否 | 身份切换候选空间 |
| EnterpriseService | CreateEnterprise | API | 写 | 是 | 创建企业空间 |
| EnterpriseService | CreateMemberInvite | API | 写 | 是 | 邀请企业成员 |
| EnterpriseService | PreviewRemoveMember / ConfirmRemoveMember | API | 写 | 是 | 移除成员，owner 不可被移除 |
| EnterpriseService | PreviewTransferOwner / ConfirmTransferOwner | API | 写 | 是 | 拥有者转让 |
| ProjectService | CreateProject | Agent / API | 写 | 是 | 创建创作项目 |
| ProjectService | GetProject | Agent / API | 读 | 否 | 项目详情和权限 |
| ProjectService | ListProjects | API | 读 | 否 | 当前空间项目列表 |
| ProjectService | CheckProjectAccess | Agent / API | 读 | 否 | 工作台项目权限校验 |
| SkillCatalogService | ListRoutableSkills | Agent | 读 | 否 | 当前空间 Published Skill 池 |
| SkillCatalogService | GetPublishedSkillSpec | Agent | 读 | 否 | Agent 路由和执行规格 |
| SkillReviewService | PreviewReviewSkill / ConfirmReviewSkill | Admin API | 写 | 是 | Skill 审核通过或拒绝 |
| ToolCapabilityService | CheckToolExecutionPolicy | Agent | 读 | 否 | Tool 可用性、风险、确认要求 |
| ModelConfigService | ListAvailableGenerationModels | Agent / API | 读 | 否 | 用户可选模型 |
| ModelConfigService | ResolveDefaultModel | Agent / API | 读 | 否 | 默认模型 |
| CreditService | EstimateGenerationCredits | Agent | 读 | 否 | 积分预估 |
| CreditService | FreezeCredits | Agent | 写 | 是 | 冻结积分 |
| CreditService | ReleaseFrozenCredits | Agent | 写 | 是 | 释放冻结积分 |
| AssetCreditCommitService | CommitGeneratedAssetAndCharge | Agent | 写 | 是 | 保存生成资产并扣费 |
| AssetService | BatchCheckAssetAccess | Agent / API | 读 | 否 | 素材引用权限 |
| AssetService | CreateUploadIntent | API | 写 | 是 | 创建 TOS 直传上传意图、object key 和上传签名 |
| AssetService | ConfirmUploadedAsset | API | 写 | 是 | 校验 TOS 直传结果并创建或激活资产 |
| AssetService | AbortUploadIntent | API | 写 | 是 | 放弃上传意图，后续清理未确认对象 |
| AssetService | GetAssetAccess | API | 读 | 否 | 登录态资产访问信息，返回业务授权后的 TOS 公共 URL |
| WorkService | CreateWork / UpdateWork | API | 写 | 是 | 个人作品管理 |
| WorkShareService | ShareWork / UnshareWork | API | 写 | 是 | 公开快照 |
| WorkLikeService | LikeWork / UnlikeWork | API | 写 | 是 | 公开作品点赞 |
| PublicContentService | ListPublicWorks / GetPublicWork | Public API | 读 | 否 | 未登录公开读，返回公开快照媒体 URL |
| NotificationService | CreateNotification | Business | 写 | 是 | Skill 审核等通知 |
| AdminAuditService | AppendAuditLog / QueryAuditLogs | Admin API | 写/读 | 写是 | 审计日志 |

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

- object key 必须由业务后端生成，并遵守 [TOS 对象存储规范](../../standards/TOS对象存储规范.md)。
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
- 超时：外部供应商连通性测试、资产保存、TOS 上传签名和上传确认。
- 幂等：重复创建项目、重复冻结、重复扣费、重复点赞。
- 版本兼容：新增字段前后端忽略未知字段。

## 待确认

- 模型 API Key 加密和轮换策略。
- TOS 已接入 CDN，域名为 `https://tos.doraigc.com`；第一版不做 CDN 缓存失效。
