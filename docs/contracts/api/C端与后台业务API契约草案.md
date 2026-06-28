# C 端与后台业务 API 契约草案

状态：draft
owner：文档与契约责任域；业务服务责任域和前端责任域确认
更新时间：2026-06-28
适用范围：Dora-Agent Web 前端 -> 业务 API 适配层

## 成熟度复核

当前成熟度：draft，不升 `active`。  
使用方式：可作为业务 API 领域拆分、路由和通用规则输入；字段级 route、request/response 和 schema 以 `api/openapi/business-api.yaml` 为准，索引见 `docs/contracts/字段级契约索引.md`。

已补齐项：公开作品分类、标签来源、固定排序字段、后台高风险操作 preview/confirm 规则已冻结。

未冻结项：业务 API 本地服务级执行报告尚未固化，因此本文仍保持 `draft`。

## 字段级事实源

- OpenAPI：`api/openapi/business-api.yaml`
- HTTP contract fixture：`tests/contract/fixtures/business-api/**`
- HTTP 实现：`services/business/internal/transport/http/**`

新增或变更字段时，先更新 OpenAPI 和 fixture，再同步本文档的业务规则、异常口径和成熟度状态。

## API 领域

| 领域 | 首批 API | 鉴权 | 说明 |
| --- | --- | --- | --- |
| Auth / Space | 登录、注册、企业登录、身份切换、当前空间 | 部分公开 | 当前空间决定项目、积分、Skill、资产上下文 |
| Public | 首页公开作品、精选作品、公开作品详情、公开 Skill 摘要 | 公开 | 不返回私有字段 |
| Project | 项目列表、创建、详情、项目资产/会话/作品 | 登录 | 项目是资产和创作过程归属容器 |
| Asset | 上传登记、资产列表、预览、下载、引用权限 | 登录 | 资产事实归业务服务 |
| Works | 我的作品、创建/编辑、分享/取消分享、点赞 | 登录/公开读 | 公开分享基于快照 |
| Enterprise | 企业概览、成员、邀请、积分 | 企业登录 | owner 和 member 权限不同 |
| Admin | 用户、模型、Tool、Skill、积分、作品、审计 | 管理员 | 独立后台登录态 |

## 公开作品列表

- `GET /api/public/works` 的字段级事实源是 `api/openapi/business-api.yaml` 的 `listPublicWorks`。
- 查询参数支持 `category`、`tag`、`resource_type` 和 `page_size`。
- `category` 来源于作品分享快照的 `category` 字段；第一版不单独维护分类字典表。
- `tag` 来源于作品分享快照 `snapshot_payload.tags`；标签由分享 preview/confirm 时的 `tags` 写入。
- `resource_type` 来源于公开快照或媒体摘要中的资源类型，用于图片、视频、文档等公开内容筛选。
- 排序固定为 `published_at DESC, public_work_id ASC`；第一版不开放用户可选排序字段。
- 响应项使用 `PublicWorkSummaryDTO`，只返回公开快照字段，不返回源资产 object key、私有作品字段、Prompt、黑板、会话或积分信息。

## 后台高风险操作

- 后台高风险写操作必须拆分为 preview 和 confirm；preview 只返回影响范围、确认摘要、`preview_token` 和过期时间，不改变业务事实。
- confirm 必须携带 `preview_token`、`AdminReason` 和 `Idempotency-Key`；服务端必须校验 preview token、管理员身份、操作对象、原因摘要和幂等冲突。
- 用户状态变更使用 `POST /api/admin/users/{user_id}/status/preview` 和 `POST /api/admin/users/{user_id}/status/confirm`。
- 公开作品下架使用 `POST /api/admin/works/public/{public_work_id}/take-down/preview` 和 `POST /api/admin/works/public/{public_work_id}/take-down/confirm`。
- confirm 成功后必须写入后台审计；失败时只返回业务错误码，不泄露内部实现、策略细节或数据库结构。

## 通用规则

- 用户端工程目录固定为 `frontend/`，承载公开页、登录态 C 端、项目、工作台、资产、作品和企业空间。
- 管理端工程目录固定为 `admin_frontend/`，承载独立后台登录态和 `/api/admin/**`。
- 两端 cookie/token 命名空间隔离；后台请求 `RequestMeta.source=admin`，必须包含 `admin_id`、操作原因和审计字段。
- 未登录公开读 API 不返回用户私有资产、会话、黑板、提示词、积分、模型成本。
- 需要登录的动作返回 `UNAUTHENTICATED`，前端弹 LoginModal。
- 列表默认 `page_size = 10`，最大值建议 `50`。
- 写操作必须透传 `Idempotency-Key`。
- 后台高风险写操作需要确认或二次确认语义。
- 后台不消费 AG-UI，不代替用户进入空间，不查看私有会话、黑板、提示词、私有素材或积分明细正文。

## 首批路由契约

| Method | Path | 描述 | 状态 |
| --- | --- | --- | --- |
| GET | `/api/public/home` | 首页公开作品、公开 Skill、登录后最近项目摘要 | contracted |
| GET | `/api/public/works` | 精选作品列表 | contracted |
| GET | `/api/public/works/:public_work_id` | 公开作品详情 | contracted |
| POST | `/api/auth/login` | 登录 | contracted |
| POST | `/api/auth/register` | 注册 | contracted |
| GET | `/api/account/current-space` | 当前空间 | contracted |
| POST | `/api/account/switch-identity` | 身份切换 | contracted |
| GET | `/api/projects` | 项目列表 | contracted |
| POST | `/api/projects` | 创建项目 | contracted |
| GET | `/api/projects/:project_id` | 项目详情 | contracted |
| POST | `/api/projects/:project_id/archive` | 归档项目 | contracted |
| GET | `/api/assets` | 资产列表 | contracted |
| POST | `/api/assets/upload-intents` | 创建 TOS 直传上传意图和上传签名 | contracted |
| POST | `/api/assets/upload-intents/:upload_intent_id/confirm` | 确认 TOS 直传完成并创建资产 | contracted |
| POST | `/api/assets/upload-intents/:upload_intent_id/abort` | 放弃上传意图并等待清理 | contracted |
| GET | `/api/assets/:asset_id/access` | 登录态资产访问信息，返回业务授权后的 TOS 公共 URL | contracted |
| GET | `/api/works` | 我的作品 | contracted |
| POST | `/api/works` | 创建作品 | contracted |
| POST | `/api/works/:work_id/share` | 分享作品 | contracted |
| POST | `/api/works/:work_id/unshare` | 取消分享 | contracted |
| POST | `/api/public/works/:public_work_id/like` | 点赞公开作品 | contracted |
| GET | `/api/enterprise/summary` | 企业概览 | contracted |
| GET | `/api/enterprise/members` | 成员列表 | contracted |
| POST | `/api/enterprise/invites` | 邀请成员 | contracted |
| GET | `/api/admin/users` | 后台用户列表 | contracted |
| POST | `/api/admin/users/:user_id/disable` | 禁用用户 | contracted |
| GET | `/api/admin/audit-logs` | 审计日志 | contracted |

## 前端状态

- loading：Skeleton 或表格占位。
- empty：公开页、项目、作品、企业成员和后台列表均需空态。
- login_required：保留触发意图，展示 LoginModal。
- permission_denied：展示权限不足，不自动切换身份。
- error：展示可重试错误，不泄露后端堆栈。

## 项目归档

- 项目 `archived` 后仍可在项目列表、项目详情、历史会话、资产、黑板和作品中只读查看。
- 项目 `archived` 后禁止新建 Agent run、继续生成、上传或绑定新资产到该项目、保存生成资产、从该项目继续创建新作品。
- 如第一版提供恢复能力，恢复必须由显式业务 API 完成；未提供恢复能力时只能新建项目重新创作。
- 前端收到 `PROJECT_ARCHIVED` 或项目详情 `creative_allowed=false` 时，显示 `项目已归档，无法继续创作。`

## 内容安全证据

- 创建上传意图或确认上传中的文本元数据、保存生成资产、分享作品必须携带 `safety_evidence`。
- `safety_evidence.result` 必须为 `passed`，过期、摘要不匹配或场景不匹配返回 `SAFETY_EVIDENCE_INVALID`。
- 前端只展示用户可见提示，不展示策略细节、内部评分、完整 Prompt、推理链路或供应商原始响应。

## TOS 直传

- TOS 使用公共桶，object key 和上传签名必须由业务服务生成。
- `POST /api/assets/upload-intents` 校验登录态、空间、项目权限、项目归档状态、文件 MIME、大小和安全证据后，返回 `upload_intent_id`、`asset_id`、`object_key`、`upload_url`、`upload_headers`、`expires_at`、`max_size_bytes`、`content_type`。
- 前端使用上传签名直接上传到 TOS，不接触 TOS AK/SK，不自造 object key。
- 上传完成后，前端调用 confirm API 提交 `object_key`、`etag`、`size_bytes`、`content_type`、`checksum`；业务服务校验一致后创建或激活资产。
- object key 必须遵守 [TOS 对象存储规范](../../standards/TOS对象存储规范.md)。

上传意图响应字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| upload_intent_id | string | 是 | 上传意图 ID |
| asset_id | string | 是 | 上传成功后创建或激活的资产 ID |
| bucket | string | 是 | TOS bucket |
| object_key | string | 是 | 后端生成的 TOS object key |
| upload_url | string | 是 | 前端直传地址 |
| upload_headers | object | 是 | 前端上传必须携带的签名头 |
| expires_at | datetime | 是 | 上传凭证过期时间 |
| max_size_bytes | int64 | 是 | 最大文件大小 |
| content_type | string | 是 | 允许上传的 MIME |
| public_url_after_confirm | string | 否 | 确认后可展示的 TOS 公共 URL |

## 公开媒体访问

- 公开作品详情返回公开快照和 `public_media_url` / `public_media_refs`，URL 来自 TOS 公共桶对象。
- 公开作品必须使用 `public/works` 快照媒体前缀，不直接返回源业务资产 object key。
- 未登录访客可看 Shared 公开快照媒体预览和复制公开链接；点赞、创作、作品中心、资产库继续返回 `UNAUTHENTICATED`。
- 取消分享或后台下架后，公开列表移除，详情页展示不可访问；第一版不做 CDN 缓存失效，已泄露或已缓存 CDN URL 不承诺即时失效；重新公开必须换 object key；源资产和私有作品不删除。

## 后续证据

- 需要补充业务 API 服务级执行报告，覆盖公开作品筛选、公开详情、点赞、分享 preview/confirm、后台用户状态 preview/confirm 和公开作品下架 preview/confirm。
- TOS 已接入 CDN，域名为 `https://tos.doraigc.com`；第一版不做 CDN 缓存失效。
