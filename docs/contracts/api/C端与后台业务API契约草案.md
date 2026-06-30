# C 端与后台业务 API 契约

状态：active
owner：文档与契约责任域；业务服务责任域和前端责任域确认
更新时间：2026-06-29
适用范围：Dora-Agent Web 前端 -> 业务 API 适配层

## 成熟度复核

当前成熟度：active。
使用方式：作为业务 API 领域拆分、路由和通用规则当前事实源；字段级 route、request/response 和 schema 以 `api/openapi/business-api.yaml` 为准，索引见 `docs/contracts/字段级契约索引.md`。

已补齐项：公开作品分类、标签来源、固定排序字段、后台高风险操作 preview/confirm 规则已冻结。

运行证据：`tests/reports/m5-technical-baseline-report.md` 已记录业务 API fixture、OpenAPI / Gin route parity、公开作品、分享 preview/confirm、后台下架 preview/confirm 和通知能力通过；`tests/reports/m6-service-acceptance-report.md` 已记录 HTTP 服务级验收通过。

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
- confirm 必须在 JSON body 携带 `preview_token` 和 `reason`；服务端必须校验 preview token、管理员身份、操作对象、原因摘要，并在业务需要时由后端派生业务幂等键。
- 后台操作原因属于业务审计字段，不使用 HTTP header 传输；所有中文、多行原因都按 JSON body 原文传递。
- 用户状态变更使用 `POST /api/admin/users/{user_id}/status/preview` 和 `POST /api/admin/users/{user_id}/status/confirm`。
- 公开作品下架使用 `POST /api/admin/works/public/{public_work_id}/take-down/preview` 和 `POST /api/admin/works/public/{public_work_id}/take-down/confirm`。
- confirm 成功后必须写入后台审计；失败时只返回业务错误码，不泄露内部实现、策略细节或数据库结构。

## 后台 Skill 创建

- `POST /api/admin/skills/system` 的主输入是 `skill_markdown`，管理端 Skill 编辑器只提交 Skill 名称、标签和 Markdown 源码。
- Markdown 使用中文标签段落，例如 `<名称>`、`<说明>`、`<输入>`、`<计划>`、`<工具引用>`、`<AG-UI元素引用>`、`<生成偏好>`、`<提示词写法>` 和 `<结果输出>`。
- Tool 引用使用结构化标签，例如 `<tool id="image_generate:model_generation">图片生成</tool>`；文本展示只显示标签内名称，源码保存完整标签。
- AG-UI 引用使用结构化标签，例如 `<agui id="storyboard_panel">故事板面板</agui>`；对话框内/外分组由 Markdown 段落文本表达。
- `<输入>` 和 `<结果输出>` 都使用自然语言描述运行时意图，不要求管理员手写 JSON Schema；Agent 运行时根据意图向用户发起 AG-UI 填写、选择、上传、审阅或修改。
- 业务服务解析 `skill_markdown` 后派生 `skill_spec_json`、`route_hints`、`input_schema_json`、`output_schema_json`、Tool 绑定和默认 `memory_policy_json`；前端不得自行拼装这些运行时 JSON 字段。
- `input_schema_json.mode=agent_requested_inputs`，保存从 `<输入>` 推断出的 `input_intents`、偏好的 AG-UI 交互和循环补充策略；`output_schema_json.mode=agent_generated_outputs`，保存从 `<结果输出>` 推断出的 `output_intents`、产物类型、偏好的展示面板和用户可修改策略。
- `skill_key`、`version`、旧 `skill_spec_json` 等字段保留兼容，但新管理端默认不要求管理员手工填写。
- 系统 Skill 列表额外返回 `latest_version_id` 和 `active_test_case_count`，用于管理端判断发布前置条件；发布仍由后端强校验至少 3 个 active 测试用例。
- 审核提交、通过和拒绝必须同步 `skills.status` 与 `skill_versions.status`；拒绝新 Skill 时主状态回到 `draft`，拒绝已发布 Skill 的新版本时主状态保持 `published`。

## 后台 Tool 注册

- `POST /api/admin/tools` 只注册 Tool 元信息和默认治理策略，不创建运行时执行器。
- 管理端常规 Tool 管理页不暴露新增按钮，只展示已注册 Tool，并提供启停、策略、计费、白名单和影响预览。
- 注册字段必须包含 `tool_name`、`tool_type`、`display_name` 和 `description`；`description` 是给 Skill 作者选择 Tool 时阅读的作用说明。
- 注册时同时初始化全局执行策略和默认计费策略，包括 `allowed`、`risk_level`、`requires_confirmation`、`timeout_ms`、`charge_mode`、`billing_unit`、`unit_points`、`free_quota` 和 `min_charge_points`。
- `input_schema_json` 和 `output_schema_json` 保存 Tool 执行器的结构化输入/输出说明；为空时默认 `{}`，不得由前端臆造未被执行器支持的字段。
- 内置 Tool 的真实执行能力仍由后端运行时提供；管理端注册成功只代表 Skill 可以引用该 Tool，并不代表新增了新的代码级执行器。
- 管理端 Tool 列表展示真实治理配置；Tool 停用只影响运行时执行检查，不得把管理端策略展示改成 `disabled/0`。
- Tool 影响预览同时返回 `affected_skill_ids` 和 `affected_skills[]`，其中 `affected_skills` 至少包含 `skill_id`、`skill_name` 和 `status`，便于管理端展示中文对象说明。

## 后台模型配置

- 模型供应商和模型是联动配置：`GET /api/admin/models/providers` 返回供应商候选，管理端创建或编辑模型时必须从供应商候选中选择 `provider_id`，不要求管理员手工输入供应商 ID。
- 模型管理页应采用左侧供应商索引、右侧模型列表的联动布局；选择左侧供应商后，右侧模型列表通过 `provider_id` 查询参数刷新。
- `GET /api/admin/models` 支持 `provider_id`、`resource_type`、`status` 查询参数；从供应商详情或供应商列表进入模型管理时，应携带 `provider_id` 做默认筛选并同步左侧选中态。
- `AdminModelDTO` 返回 `provider_id` 和展示用 `provider_name`；表格展示使用供应商名称，仍保留 ID 便于审计和排障。
- 模型供应商负责密钥引用、基础 URL 和连通性测试；模型负责模型编码、资源类型、路由配置、默认模型和计费价格快照。
- 设为默认模型时 `pricing_snapshot_id` 可省略；后端优先绑定当前 active 价格快照，没有 active 价格快照时自动创建 0 积分默认价格快照；同一资源类型只维护一条 active 默认记录，重复设默认通过更新该记录完成。
- 默认模型不能直接停用；`POST /api/admin/models/{model_id}/status` 对 active 默认模型返回 `STATE_CONFLICT`，管理端也不展示停用按钮。

## 通用规则

- 用户端工程目录固定为 `frontend/`，承载公开页、登录态 C 端、项目、工作台、资产、作品和企业空间。
- 管理端工程目录固定为 `admin_frontend/`，承载独立后台登录态和 `/api/admin/**`。
- 两端 cookie/token 命名空间隔离；后台请求 `RequestMeta.source=admin`，必须包含 `admin_id`、操作原因和审计字段。
- 后台登录成功返回 `AdminSessionDTO.expires_at`；后台 session 为 7 天滑动窗口，任一有效 `/api/admin/**` 鉴权请求都会续期并通过 `X-Admin-Session-Expires-At` 响应头返回新的过期时间。
- 未登录公开读 API 不返回用户私有资产、会话、黑板、提示词、积分、模型成本。
- 需要登录的动作返回 `UNAUTHENTICATED`，前端弹 LoginModal。
- 列表默认 `page_size = 10`，最大值建议 `50`。
- HTTP 写操作不再要求客户端传 `Idempotency-Key` 或 `request_hash`；需要幂等的业务写由具体业务应用层基于业务字段、目标资源和操作者生成或校验内部业务幂等信息。
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

## 运行证据与后续事项

- 业务 API 服务级执行报告：见 `tests/reports/m5-technical-baseline-report.md` 和 `tests/reports/m6-service-acceptance-report.md`。
- Fixture 验证入口：`tests/contract/validate_fixtures.py`，由 `scripts/validate-m6.sh` 串行执行。
- TOS 已接入 CDN，域名为 `https://tos.doraigc.com`；第一版不做 CDN 缓存失效。
