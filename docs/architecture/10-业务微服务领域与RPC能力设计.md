# 业务微服务领域与 RPC 能力设计

状态：draft
owner：业务微服务后端工程师
更新时间：2026-06-25
适用范围：业务系统微服务领域边界、Kitex RPC server 能力清单、Agent 调用需求和契约前置映射
相关代码路径：`services/business/**`、`api/thrift/business/**`、`tests/contract/**`、`tests/business/**`
相关契约：后续由主控 Codex 汇总到 `docs/contracts/**`；本文件只提出 RPC 需求清单，不作为正式契约

## 背景

PRD 套件已经定义 Dora-Agent 第一版的账户空间、平台后台、模型、Tool、Skill、积分、资产、通知和作品中心能力。本轮补充项目 Project 作为创作容器。业务系统微服务需要在正式契约和代码开发前明确业务事实、权限、事务、RPC server 能力和测试映射，避免 Agent 服务或前端绕过业务边界。

当前 PRD 套件已确认为 `product_status：Done`，本文可作为业务微服务契约、Go 代码实现、migration 和 contract test 的工程输入；正式字段仍以 `docs/contracts/**` 为准。

## 目标

- 明确业务微服务负责的业务域和不负责的 Agent Runtime 边界。
- 输出业务后端工程前置需求映射矩阵。
- 提出 Kitex RPC server 能力清单，方法表达业务能力，不表达数据库表 CRUD。
- 标出 Go Eino 智能体侧需要调用的 RPC 能力。
- 明确项目、资产、作品和 Agent Runtime 之间的业务边界。
- 标出必须支持 preview / confirm 的业务能力。
- 为后续 Thrift DTO、错误码、权限上下文、幂等键、审计字段和 contract test 提供输入。

## 非目标

- 不实现 Go 代码、Kitex handler、GORM repository 或 migration。
- 不定义最终 Thrift 字段编号和完整 DTO。
- 不生产 AG-UI 事件，不设计 Eino Graph、Workflow、Tool 执行编排。
- 不保存 Agent 会话、run、message、event、Tool 调用历史、黑板草稿或 Agent 记忆。
- 不允许业务 RPC 暴露业务数据库表 CRUD。

## 关键约束

- 业务微服务拥有业务规则最终解释权，负责业务数据库、事务、权限、错误码和审计。
- 智能体微服务只能通过 RPC 调用业务能力，不能直接访问业务数据库。
- 写操作必须有 `idempotency_key`，重复请求返回同一业务结果或稳定的幂等冲突错误。
- 需要人工确认的高风险、扣费、业务事实写入能力必须支持 preview / confirm。
- 权限上下文必须随请求传入，业务服务最终校验用户、租户、空间、企业角色和资源权限。
- 项目是业务事实，项目标题、状态、封面、归档和项目资产 / 作品关系由业务服务维护。
- 公开内容读取允许无登录态，但只能返回公开快照和公开配置，不返回用户私有数据。
- 列表查询必须分页，默认 `page_size=10`；建议契约阶段确认最大 `page_size=50`。
- DTO 按领域和业务场景拆分，不复用 ORM 对象、domain 大对象或跨场景通用大对象。
- 审计日志不得保存 API Key 明文、完整兑换码列表、用户私密创作内容、系统提示词、模型内部推理链路或供应商原始响应。

## 产品设计读取结果

| PRD | 业务后端重点 | 工程解释 |
| --- | --- | --- |
| 00 系统概要 | 当前空间决定 Skill 池、积分账户、资产归属和历史可见范围 | 业务服务需要提供空间上下文、权限判定、业务事实读写和积分资产事务能力 |
| 01 账户身份企业与空间 | 个人空间、企业空间、邀请、移除、拥有者转让 | 账户空间是所有业务资源隔离和权限校验的事实源 |
| 02 平台后台 | 平台管理员、用户管理、运营配置、审计日志 | 平台后台使用独立管理员身份和审计，不越权查看用户私有创作内容 |
| 03 模型 | 供应商 API Key、模型类型、默认模型、用户积分单价 | 业务服务保存模型配置和价格，Agent 只按可用模型和价格结果执行 |
| 04 Tool | 平台开放 Tool、白名单、风险、超时、重试、取消 | 业务服务保存 Tool 策略，Agent 执行前通过 RPC 校验可用性和风险 |
| 05 Skill | 系统、企业、个人 Skill 的创建、测试、审核、版本 | 业务服务保存 Skill 生命周期、审核和可路由发布状态，Agent 只加载 Published Skill |
| 06 工作台 | 生成前安全评估、积分预估确认、资产保存和扣费 | Agent 编排过程，业务服务提供空间、Skill、模型、积分、资产 RPC |
| 07 积分 | 发放、兑换码、有效期、预估、冻结、扣减、释放 | 积分账户和流水是业务事实，必须用事务和幂等保证一致性 |
| 08 资产 | 上传素材、生成资产、最终资产元素、预览下载、权限 | 业务服务保存用户可见资产事实和最终资产元素，Agent 保存草稿和过程 |
| 产品补充 项目与资产归属 | 项目聚合会话、资产、黑板和作品 | 业务服务保存项目事实、项目资产关系和项目权限，Agent 只保存 project_id |
| 09 AG-UI | 事件和组件由 Agent 到前端承接 | 业务服务不生产 AG-UI，只返回可映射成事件的业务结果 |
| 10 内容安全 | 生成前提示词安全评估，失败不冻结不生成 | 安全评估由 Agent 固定执行；业务写入可要求携带安全通过证据 |
| 11 通知 | Skill 审核结果站内信、已读未读 | 通知是结果触达，不是业务事实来源；写入失败不回滚审核结果 |
| 12 作品 | 个人作品、公开快照、分享、点赞、下架 | 作品引用资产，公开展示必须使用快照，不能放开私有资产权限 |
| 公开访问补充 | 首页公开作品、精选作品、公开作品详情可匿名访问 | 公开读接口不创建用户上下文；需要登录的动作返回 `UNAUTHENTICATED` |

## 业务后端前置需求映射矩阵

| 产品设计条目 | 工程解释 | 产出物 | owner | 契约/测试 |
| --- | --- | --- | --- | --- |
| 当前空间决定 Skill、积分、资产、历史 | 空间上下文必须成为业务 RPC 的权限入口 | SpaceContext DTO、ResolveSpaceContext RPC | 业务微服务后端工程师 | 空间权限 contract test、跨空间拒绝集成测试 |
| 项目承载创作资产和过程 | 项目是业务容器；会话和 run 在 Agent DB，项目事实和资产归属在业务 DB | ProjectService、ProjectAsset 关系、项目权限校验 | 业务微服务后端工程师 | 项目权限、项目资产、跨空间拒绝 contract test |
| 个人注册后拥有个人空间 | 注册必须原子创建用户、个人空间和个人积分账户 | AccountSpaceService、注册事务 | 业务微服务后端工程师 | 注册幂等、默认空间、默认积分账户测试 |
| 企业注册、邀请、移除、转让 | 企业成员和角色是业务服务事实源 | EnterpriseService、preview/confirm 写操作 | 业务微服务后端工程师 | 成员权限、转让原子性、重复邀请测试 |
| 平台管理员独立后台 | 管理员身份和普通用户身份隔离 | AdminService、AdminAuditLog | 业务微服务后端工程师 | 后台登录态隔离、审计脱敏测试 |
| 用户禁用启用需确认和原因 | 禁用影响登录和企业空间访问，不删除公开作品 | UserAdminService、SetUserStatus preview/confirm | 业务微服务后端工程师 | 禁用确认、禁用后登录拒绝、审计测试 |
| 模型配置和用户积分单价 | 业务服务保存模型目录、默认模型和价格 | ModelConfigService | 业务微服务后端工程师 | 模型停用、默认模型保护、价格缺失测试 |
| Tool 白名单和风险等级 | Agent 调用前必须由业务服务校验 Tool 策略 | ToolCapabilityService | 业务微服务后端工程师 | Tool 停用、白名单、风险确认测试 |
| Skill 创建、审核、发布、回滚 | Skill 发布状态决定 Agent 可路由能力 | SkillCatalogService、SkillReviewService | 业务微服务后端工程师 | 生命周期、审核通知、版本回滚测试 |
| 生成前积分预估和确认 | 积分预估是 preview，冻结是 confirm 后写入 | CreditService Estimate/Freeze | 业务微服务后端工程师 | 余额不足、0 单价、幂等冻结 contract test |
| 资产保存成功后才扣费 | 资产保存和冻结扣减必须在业务事务中保证顺序 | AssetCreditCommitService | 业务微服务后端工程师 | 保存失败不扣费、部分完成扣费释放测试 |
| 上传素材校验和资产权限 | 上传登记和预览下载由业务服务控制 | AssetService、SignedAccessService | 业务微服务后端工程师 | 类型大小、无权限引用、签名访问测试 |
| 站内信审核通知 | 通知写入失败不影响审核事实，但需可补偿 | NotificationService、通知幂等键 | 业务微服务后端工程师 | 通知幂等、已读未读、跳转权限测试 |
| 分享作品前生成公开快照 | 公开快照隔离私有资产和源创作过程 | WorkService、FeaturedWorkService | 业务微服务后端工程师 | 分享安全证据、公开快照、取消分享测试 |
| 首页公开内容免登录 | 首页公开作品来自公开快照，不能读取用户私有作品或资产权限 | PublicContentService / FeaturedWorkService | 业务微服务后端工程师 | 匿名访问、隐私字段缺失、下架不可见测试 |
| 点赞需要登录且同一用户一次 | 点赞是公开作品交互事实，需要唯一约束和幂等 | WorkLikeService | 业务微服务后端工程师 | 未登录拒绝、重复点赞、取消点赞测试 |
| 平台下架公开作品 | 高风险运营写操作，需要确认和审计 | FeaturedWorkAdminService preview/confirm | 业务微服务后端工程师 | 下架确认、公开链接不可访问、审计测试 |

## 业务域划分

| 业务域 | 业务事实 | 不包含 |
| --- | --- | --- |
| 账户与空间域 | 用户、个人空间、企业、企业成员、企业邀请、当前空间上下文 | Agent 会话、前端本地状态 |
| 平台后台与审计域 | 平台管理员、用户状态、后台操作审计、敏感信息脱敏摘要 | 用户私有创作内容、代登录 |
| 模型配置域 | 供应商、加密凭证、模型目录、类型、默认模型、内部成本、用户积分单价 | 模型推理链路、供应商原始响应全文 |
| Tool 能力策略域 | 平台内置 Tool、启停、白名单、风险等级、超时、重试、取消策略 | Tool 实际执行过程、任意 HTTP Tool |
| Skill 目录与审核域 | Skill 元数据、版本、输出元素声明、Tool 绑定、审核记录、发布状态 | Eino 编排执行、Agent Runtime 运行记录 |
| 项目域 | 项目标题、封面、状态、归档、项目资产关系、项目作品关系、项目权限 | Agent 会话、run、消息、事件、黑板快照 |
| 积分域 | 积分账户、批次、冻结、扣减、释放、流水、兑换码 | 支付、订单、发票、退款 |
| 资产域 | 用户可见资产事实、最终资产元素、上传素材登记、预览下载权限 | 黑板草稿、run 事件、Tool 调用历史 |
| 通知域 | 站内信、未读状态、关联对象跳转信息 | 外部短信、邮件、IM、营销推送 |
| 作品域 | 作品、作品资产引用、公开快照、分享状态、点赞、下架 | 评论、收藏、关注、排行榜、推荐算法 |
| 公开内容域 | 首页公开作品、精选作品列表、公开作品详情、公开 Skill 摘要 | 私有资产、个人项目、个人作品中心、用户画像 |

## 通用 RPC 上下文建议

所有业务 RPC 请求建议携带以下通用上下文，正式字段由后续 Thrift 契约确定。

| DTO | 字段建议 | 说明 |
| --- | --- | --- |
| AuthContext | `actor_user_id`、`admin_id`、`login_identity_type`、`tenant_id`、`space_id`、`enterprise_id`、`enterprise_role` | 普通用户和平台管理员身份必须互斥或明确区分 |
| RequestMeta | `trace_id`、`request_source`、`idempotency_key`、`client_request_id` | 写操作必须有幂等键 |
| AuditContext | `operator_id`、`operator_type`、`business_action`、`resource_type`、`resource_id`、`reason` | 后台和高风险写操作必须记录原因或摘要 |
| Pagination | `page_token` 或 `page`、`page_size`、`sort_by`、`sort_direction` | 默认 `page_size=10`，建议最大 50 |

## RPC server 能力清单

以下是业务微服务 Kitex server 的能力需求清单，不是最终契约。

| 服务 | 方法建议 | 调用方 | 读写 | preview/confirm | 幂等键 | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| AccountSpaceService | `ResolveCurrentSpaceContext` | Agent、前端 API 适配层 | 读 | 否 | 否 | 返回当前身份、空间、企业角色、可用业务上下文 |
| AccountSpaceService | `ListAvailableSpaces` | 前端 API 适配层 | 读 | 否 | 否 | 身份切换列表，分页默认 10 |
| AccountSpaceService | `RegisterPersonalAccount` | 前端 API 适配层 | 写 | 否 | 是 | 注册后创建个人空间和个人积分账户 |
| EnterpriseService | `CreateEnterprise` | 前端 API 适配层 | 写 | 可选 | 是 | 创建企业、企业空间、拥有者成员、企业积分账户 |
| EnterpriseService | `CreateMemberInvite` | 前端 API 适配层 | 写 | 否 | 是 | 仅企业拥有者邀请，支持未注册或已注册用户 |
| EnterpriseService | `AcceptMemberInvite` | 前端 API 适配层 | 写 | 否 | 是 | 一个用户第一版最多加入一个企业 |
| EnterpriseService | `PreviewRemoveMember` / `ConfirmRemoveMember` | 前端 API 适配层 | 写 | 是 | confirm 必填 | 移除非拥有者成员，确认影响 |
| EnterpriseService | `PreviewTransferOwner` / `ConfirmTransferOwner` | 前端 API 适配层 | 写 | 是 | confirm 必填 | 转让给当前企业成员，原拥有者变成员 |
| ProjectService | `CreateProject` | 前端 API 适配层、Agent | 写 | 否 | 是 | 当前空间下创建项目，默认归属创建者 |
| ProjectService | `GetProject`、`ListProjects` | 前端 API 适配层、Agent | 读 | 否 | 否 | 企业空间第一版只返回本人项目，分页默认 10 |
| ProjectService | `UpdateProjectTitle`、`ArchiveProject`、`RestoreProject` | 前端 API 适配层 | 写 | 归档可选确认 | 是 | 不删除资产和作品，审计状态变化 |
| ProjectService | `CheckProjectAccess` | Agent | 读 | 否 | 否 | 创建会话、run、恢复会话前校验项目访问 |
| ProjectAssetService | `AttachAssetToProject`、`ListProjectAssets` | Agent、前端 API 适配层 | 读写 | 否 | 写必填 | 资产归属项目，保留来源 session/run/artifact 摘要 |
| ProjectWorkService | `ListProjectWorks` | 前端 API 适配层 | 读 | 否 | 否 | 项目详情展示由项目资产创建的作品 |
| AdminService | `CreateAdmin`、`DisableAdmin`、`ListAdmins` | 平台后台 API 适配层 | 读写 | 停用需确认 | 写必填 | 平台管理员不做 RBAC，停用自身需阻止或特殊确认 |
| UserAdminService | `ListUsers`、`GetUserSummary` | 平台后台 API 适配层 | 读 | 否 | 否 | 不返回私有资产、会话、黑板、提示词、积分明细正文 |
| UserAdminService | `PreviewSetUserStatus` / `ConfirmSetUserStatus` | 平台后台 API 适配层 | 写 | 是 | confirm 必填 | 启用或禁用用户，必须记录原因 |
| ModelConfigService | `ListAvailableGenerationModels` | Agent | 读 | 否 | 否 | 只返回用户可见模型名称、类型、价格摘要 |
| ModelConfigService | `ResolveDefaultModel` | Agent | 读 | 否 | 否 | 按图片、音乐、视频返回默认模型 |
| ModelConfigService | `UpsertProvider`、`TestProviderConnectivity`、`UpsertModel`、`SetDefaultModel`、`SetModelStatus` | 平台后台 API 适配层 | 读写 | 默认模型停用需确认 | 写必填 | API Key 加密保存，展示和审计均脱敏 |
| ToolCapabilityService | `ListBindableTools` | Skill Builder API 适配层 | 读 | 否 | 否 | 按创建者、空间、企业、套餐和白名单过滤 |
| ToolCapabilityService | `CheckToolExecutionPolicy` | Agent | 读 | 否 | 否 | 执行前返回启停、权限、风险、超时、重试和是否需确认 |
| ToolCapabilityService | `UpdateToolPolicy` | 平台后台 API 适配层 | 写 | 风险或白名单变更需确认 | 是 | 变更可能影响已发布 Skill，需审计 |
| SkillCatalogService | `ListRoutableSkills`、`GetPublishedSkillSpec` | Agent | 读 | 否 | 否 | 只返回 Published 且当前空间可用的 Skill |
| SkillCatalogService | `CreateDraftSkill`、`UpdateDraftSkill`、`SubmitSkillForReview` | 前端 API 适配层 | 写 | 否 | 是 | 个人和企业 Skill 进入审核，系统 Skill 可发布 |
| SkillReviewService | `PreviewReviewSkill` / `ConfirmReviewSkill` | 平台后台 API 适配层 | 写 | 是 | confirm 必填 | 审核通过或拒绝，通知创建者 |
| SkillCatalogService | `PublishSystemSkill`、`DeprecateSkill`、`RollbackSkillVersion` | 平台后台 API 适配层 | 写 | 废弃和回滚需确认 | 是 | 历史版本不覆盖，回滚生成新草稿或新版本 |
| CreditService | `GetCreditBalance`、`ListCreditLedger`、`ListMemberConsumption` | Agent、前端 API 适配层 | 读 | 否 | 否 | 企业成员只能看本人企业空间消耗 |
| CreditService | `EstimateGenerationCredits` | Agent | 读 | 是，作为 preview | 否 | 按模型价格、数量或秒数预估，0 单价也返回确认数据 |
| CreditService | `FreezeCredits` | Agent | 写 | confirm | 是 | 用户确认后冻结，余额不足不冻结 |
| CreditService | `ReleaseFrozenCredits` | Agent | 写 | 否 | 是 | 失败、取消、超时、保存失败释放，不延长有效期 |
| AssetCreditCommitService | `CommitGeneratedAssetAndCharge` | Agent | 写 | 否，已在冻结前确认 | 是 | 在一个业务事务内保存最终资产并扣减对应冻结积分 |
| CreditAdminService | `GrantCredits`、`CreateRedeemCodes`、`DisableRedeemCode`、`ExportRedeemCodes` | 平台后台 API 适配层 | 写 | 发放、导出需确认 | 是 | 导出审计不保存完整兑换码列表 |
| CreditRedeemService | `RedeemCode` | 前端 API 适配层 | 写 | 否 | 是 | 一次性兑换，个人码和企业码按当前空间校验 |
| AssetService | `RegisterUploadAsset` | 前端 API 适配层、Agent | 写 | 否 | 是 | 上传校验通过后登记用户可见资产 |
| AssetService | `BatchCheckAssetAccess` | Agent | 读 | 否 | 否 | 批量校验引用资产权限，避免循环逐条 RPC |
| AssetService | `ListAssets`、`GetAssetDetail`、`CreateSignedPreviewAccess`、`CreateSignedDownloadAccess` | 前端 API 适配层 | 读 | 否 | 否 | 分页默认 10，签名链接短期有效 |
| NotificationService | `CreateNotification`、`ListNotifications`、`MarkNotificationRead`、`MarkAllNotificationsRead`、`GetUnreadCount` | 前端 API 适配层、后台流程 | 读写 | 否 | 写必填 | 审核通知幂等，跳转时重新校验权限 |
| WorkService | `CreateWork`、`UpdateWork`、`ListMyWorks`、`GetMyWorkDetail` | 前端 API 适配层 | 读写 | 否 | 写必填 | 作品引用已保存资产，企业身份只看本人作品 |
| WorkShareService | `PreviewShareWork` / `ConfirmShareWork` | 前端 API 适配层 | 写 | 是 | confirm 必填 | 分享前要求安全通过证据并生成公开快照 |
| WorkShareService | `UnshareWork` | 前端 API 适配层 | 写 | 可选 | 是 | 取消分享后公开链接不可访问 |
| PublicContentService | `ListHomeFeaturedWorks`、`ListPublicSkillSummaries` | 前端公开 API 适配层 | 读 | 否 | 否 | 首页公开内容，允许无登录态，只返回公开摘要 |
| FeaturedWorkService | `ListFeaturedWorks`、`GetFeaturedWorkDetail` | 前端公开 API 适配层 | 读 | 否 | 否 | 免登录只读公开快照，分页默认 10 |
| WorkLikeService | `LikeWork`、`UnlikeWork` | 前端 API 适配层 | 写 | 否 | 是 | 登录用户对同一作品只能点赞一次 |
| FeaturedWorkAdminService | `PreviewTakeDownWork` / `ConfirmTakeDownWork` | 平台后台 API 适配层 | 写 | 是 | confirm 必填 | 下架不删除源资产，必须审计 |
| AuditLogService | `ListAuditLogs` | 平台后台 API 适配层 | 读 | 否 | 否 | 按操作人、模块、类型、时间筛选，分页默认 10 |

## Go Eino 智能体侧需要调用的 RPC

| Agent 场景 | 业务 RPC 能力 | 是否写业务事实 | 是否必须人工确认 |
| --- | --- | --- | --- |
| 进入工作台并确定上下文 | `ResolveCurrentSpaceContext` | 否 | 否 |
| 创建或校验当前项目 | `CreateProject`、`CheckProjectAccess`、`GetProject` | 创建项目时写 | 创建项目无需确认；归档不由 Agent 发起 |
| 加载可路由 Skill 池 | `ListRoutableSkills`、`GetPublishedSkillSpec` | 否 | 否 |
| 校验 Tool 可执行性 | `CheckToolExecutionPolicy` | 否 | 高风险 Tool 需由 Agent 发起确认 |
| 获取可选或默认模型 | `ListAvailableGenerationModels`、`ResolveDefaultModel` | 否 | 否 |
| 生成前积分预估 | `EstimateGenerationCredits` | 否 | 作为 preview 结果展示 |
| 用户确认后冻结积分 | `FreezeCredits` | 是 | 是 |
| 生成失败、取消、保存失败释放 | `ReleaseFrozenCredits` | 是 | 否 |
| 生成成功后保存资产并扣费 | `CommitGeneratedAssetAndCharge` | 是 | 前置冻结已确认，本 RPC 只做事务提交 |
| 绑定资产到项目 | `AttachAssetToProject` 或资产保存事务内项目绑定 | 是 | 否 |
| 引用已有资产 | `BatchCheckAssetAccess` | 否 | 否 |
| 需要业务写入 Tool | 后续具体业务 RPC 的 preview/confirm 方法 | 是 | 是 |

## 必须 preview / confirm 的能力

| 能力 | preview 输出 | confirm 输入 | 原因 |
| --- | --- | --- | --- |
| 企业成员移除 | 被移除成员、影响空间、权限变化 | preview_token、idempotency_key、原因 | 影响企业访问权限 |
| 企业拥有者转让 | 新旧角色变化、影响提示 | preview_token、idempotency_key | 高影响企业权限变更 |
| 用户账号禁用 | 登录影响、公开作品保留说明 | preview_token、idempotency_key、原因 | 影响用户登录和企业身份 |
| 默认模型停用或切换 | 受影响模型类型和默认策略 | preview_token、idempotency_key | 可能阻断生成 |
| Tool 风险、白名单、启停变更 | 受影响 Skill 和空间摘要 | preview_token、idempotency_key | 影响已发布 Skill 和 Agent 执行 |
| Skill 审核通过或拒绝 | 状态变化、通知对象、审核意见 | preview_token、idempotency_key | 影响 Agent 路由和创建者通知 |
| 积分冻结 | 预计消耗、可用余额、即将过期积分 | 用户确认、idempotency_key | 扣费前必须人工确认 |
| 平台积分发放 | 目标账户、数量、过期时间、原因 | preview_token、idempotency_key | 运营资金等价业务事实 |
| 兑换码批量导出 | 导出数量、风险提示 | preview_token、idempotency_key | 高风险敏感导出 |
| 作品分享 | 公开字段、公开媒体引用、隐私不展示说明 | preview_token、idempotency_key、安全通过证据 | 私有内容变公开 |
| 项目归档 | 影响项目是否出现在默认最近项目和是否允许继续创作 | idempotency_key、可选原因 | 影响用户组织创作内容 |
| 公开作品下架 | 下架影响、公开链接不可访问说明 | preview_token、idempotency_key、原因 | 运营风险处理 |

## 错误码分类建议

| 分类 | 示例错误码 | 说明 |
| --- | --- | --- |
| 参数错误 | `INVALID_ARGUMENT`、`MISSING_REQUIRED_FIELD` | 请求 DTO 缺字段、字段非法 |
| 认证错误 | `UNAUTHENTICATED` | 缺少登录态或管理员态 |
| 权限错误 | `PERMISSION_DENIED`、`CROSS_SPACE_DENIED`、`ENTERPRISE_ROLE_REQUIRED` | 业务服务最终权限拒绝 |
| 资源错误 | `RESOURCE_NOT_FOUND`、`RESOURCE_UNAVAILABLE` | 资源不存在、停用、不可访问 |
| 状态冲突 | `STATE_CONFLICT`、`DEFAULT_MODEL_REQUIRED`、`SKILL_REVIEW_REQUIRED` | 当前状态不允许操作 |
| 幂等冲突 | `IDEMPOTENCY_CONFLICT` | 同一幂等键请求参数不一致 |
| 业务额度 | `CREDIT_INSUFFICIENT`、`CREDIT_FREEZE_NOT_FOUND`、`REDEEM_CODE_INVALID` | 积分、冻结、兑换码类错误 |
| 安全阻断 | `SAFETY_BLOCKED`、`SAFETY_EVIDENCE_REQUIRED` | 缺少安全通过证据或内容不合规 |
| 外部依赖 | `EXTERNAL_DEPENDENCY_FAILED`、`PROVIDER_CONNECTIVITY_FAILED` | 供应商、TOS 或其他外部依赖失败 |
| 超时 | `TIMEOUT` | RPC、DB 或外部依赖超时 |
| 系统错误 | `INTERNAL_ERROR` | 未分类内部错误，对外不暴露细节 |

## 审计字段建议

写操作审计至少记录：

| 字段 | 说明 |
| --- | --- |
| `audit_id` | 审计记录 ID |
| `trace_id` | 链路追踪 |
| `operator_type` | user、enterprise_owner、platform_admin、system |
| `operator_id` | 操作者 ID，登录失败可为空或脱敏摘要 |
| `tenant_id` / `space_id` | 业务隔离上下文 |
| `business_action` | 业务动作，例如 `credit.grant`、`skill.review.approve` |
| `resource_type` / `resource_id` | 被操作资源 |
| `idempotency_key` | 写操作幂等键 |
| `before_status` / `after_status` | 状态变化摘要，不能包含敏感明文 |
| `reason` | 管理员原因或用户确认摘要 |
| `result` | success、failed、blocked |
| `error_code` | 失败时记录稳定错误码 |
| `created_at` | 操作时间 |

不得记录 API Key 明文、完整兑换码列表、用户私密创作内容、系统提示词、模型内部推理链路、供应商原始响应全文。

## 测试与验收映射

| 范围 | contract test | 集成测试 |
| --- | --- | --- |
| 账户空间 | AuthContext、SpaceContext、跨空间错误码 | 注册默认空间、企业邀请、移除、转让 |
| 项目 | 项目创建、列表分页、权限错误、资产绑定 DTO | 项目创建、归档、跨空间拒绝、项目资产列表 |
| 平台后台 | 管理员态、审计字段、脱敏响应 | 用户禁用启用、审计查询 |
| 模型 | 可用模型列表、默认模型、价格字段 | 默认模型停用保护、价格缺失不可选 |
| Tool | 执行策略 DTO、风险等级、白名单错误码 | Tool 停用、企业白名单变更影响 |
| Skill | 生命周期状态、审核错误码、通知触发 | 个人/企业 Skill 审核、系统 Skill 发布 |
| 积分 | 预估、冻结、扣减、释放、幂等冲突 | 余额不足、0 单价、部分成功扣费释放 |
| 资产 | 批量权限校验、资产保存响应、签名访问 | 上传校验、保存失败不扣费、无权限引用 |
| 通知 | 列表分页、已读未读、跳转资源摘要 | 审核通知幂等、关联 Skill 权限校验 |
| 作品 | 公开快照 DTO、点赞幂等、下架错误码 | 分享、取消分享、下架、免登录访问 |
| 公开内容 | 首页公开作品 DTO、匿名访问、隐私字段缺失 | 首页公开作品、精选作品、登录弹窗触发 |

## 冲突、缺口或待确认事项

- 当前 PRD 套件已确认 Done；后续由主控 Codex 确认契约、开发计划和 工程角色需求映射矩阵。
- 平台后台是直接调用业务服务，还是通过独立后台 API 适配层调用业务 RPC，需要主控和前端/Agent 架构确认。
- 内容安全评估由 Agent 执行，但业务写操作是否统一要求 `safety_result_id`、`safety_passed_at` 和摘要字段，需要在 RPC 契约中确定。
- 项目封面、项目标题自动生成和归档后是否允许继续创作需要产品确认。
- 模型供应商连通性测试由业务服务直接执行还是调用 Agent/Tool 侧能力，需要在实现前确认密钥边界和网络出口。
- `page_size` 最大值建议为 50，但需要契约阶段统一。
- 作品分类、资产元素类型第一版是内置字典，是否需要后台变更能力当前 PRD 明确不做，后续不要提前实现。
