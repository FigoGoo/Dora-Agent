# 页面、路由、组件、状态与 AG-UI 需求映射矩阵

状态：draft
owner：产品体验设计师
更新时间：2026-06-25
适用范围：Dora-Agent 第一版页面设计到前端组件、页面状态、AG-UI 消费点、API/RPC 依赖的需求映射
相关代码路径：`web/**`、`frontend/**`
相关 PRD：[PRD 文档索引](../product/prd/README.md)、[00-系统概要与功能大纲 PRD](../product/prd/00-系统概要与功能大纲PRD.md)、[01-账户身份企业与空间 PRD](../product/prd/01-账户身份企业与空间PRD.md)、[02-平台后台与运营管理 PRD](../product/prd/02-平台后台与运营管理PRD.md)、[03-模型供应商模型选择与单价 PRD](../product/prd/03-模型供应商模型选择与单价PRD.md)、[04-Tool 边界与平台开放能力 PRD](../product/prd/04-Tool边界与平台开放能力PRD.md)、[05-Skill Builder 与审核 PRD](../product/prd/05-SkillBuilder与审核PRD.md)、[06-统一 Agent 创作工作台 PRD](../product/prd/06-统一Agent创作工作台PRD.md)、[07-积分账户兑换码与扣费 PRD](../product/prd/07-积分账户兑换码与扣费PRD.md)、[08-资产素材与创作过程 PRD](../product/prd/08-资产素材与创作过程PRD.md)、[09-AG-UI 与 A2UI 交互 PRD](../product/prd/09-AG-UI与A2UI交互PRD.md)、[10-内容安全治理 PRD](../product/prd/10-内容安全治理PRD.md)、[11-站内信与通知 PRD](../product/prd/11-站内信与通知PRD.md)、[12-作品中心与精选作品 PRD](../product/prd/12-作品中心与精选作品PRD.md)
相关契约：[AG-UI 事件规范](../standards/AG-UI事件规范.md)；正式 API、RPC、AG-UI 契约待后续补充

## 使用说明

本矩阵把 PRD 条目映射到页面、路由草案、组件、状态和契约依赖，供后续前端开发工程师、Go Eino 智能体微服务架构工程师、业务微服务后端工程师做需求映射时引用。

注意：

- 本文件不定义最终字段，字段以 API、AG-UI 和 RPC 契约为准。
- AG-UI 当前有 `docs/standards/AG-UI事件规范.md` 的基础事件，也有 `09-AG-UI与A2UI交互PRD.md` 的扩展事件建议。正式事件命名需要由 AG-UI 契约统一。
- 非 Agent 实时页面默认不消费 AG-UI，主要依赖 API；涉及业务事实写入的动作需要 RPC 由业务微服务承接。

## 页面需求映射矩阵

| PRD 条目 | 页面 / 路由草案 | 前端组件 | 必备状态 | AG-UI 消费点 | API / RPC 依赖 | 待确认 |
| --- | --- | --- | --- | --- | --- | --- |
| 账户登录、个人空间 | `/login`、`/register` | AuthForm、RegisterForm、IdentitySwitch | loading、error、success | 无 | 登录、注册、当前身份 API | 账号字段、登录方式 |
| 企业登录、身份切换 | `/login`、`/app/account` | IdentityList、SpaceBadge、SwitchConfirm | loading、switching、permission_denied、success | 无 | 身份切换 API、企业身份 RPC | 切换后的缓存刷新策略 |
| 首页快速创作 | `/app` | CreationLanding、PromptComposer、SkillCard | loading、empty、error、success | 无；进入工作台后消费 | 最近会话、热门 Skill、积分摘要 API | 首页是否允许未登录试用 |
| 统一 Agent 工作台 | `/app/workspace` | AgentWorkspace、ChatMessage、ThinkingTypewriter、Composer、WorkspacePanel | loading、empty、streaming、interrupt、resume、error、success | `message.started`、`message.delta`、`message.completed`、`tool.call`、`tool.result`、`interrupt.required`、`resume.accepted`、`agent.completed`、`agent.failed`；PRD 建议扩展事件见下方 | Agent run API、确认/取消/重试 API、AG-UI SSE | 标准事件与 PRD 扩展事件命名统一 |
| 模型选择 | `/app/workspace` | ModelPicker、ComposerCapsule、ModelTag | editing、locked、unavailable、error | PRD 建议：`chat.controls.requested`、`chat.controls.locked`、`platform.tags.updated` | 可用模型 API、默认模型 API | 模型列表字段、显示名重复处理 |
| 内容安全阻断 | `/app/workspace`、`/app/works/:id/edit` | SafetyNotice、ErrorNotice、PromptEditor | loading、blocked、error、success | PRD 建议：`safety.prompt.evaluated`、`safety.prompt.blocked` | 安全评估能力、作品分享评估 API | 用户可见提示口径 |
| 积分预估和确认 | `/app/workspace`、`/app/credits` | CreditEstimate、ConfirmPanel、CreditOverview | estimating、insufficient、interrupt、success、error | `interrupt.required`；PRD 建议：`credits.estimated`、`credits.frozen`、`credits.charged`、`credits.released`、`confirmation.required` | 积分预估、冻结、扣减、释放 RPC/API | 0 积分确认文案 |
| Tool 调用和生成进度 | `/app/workspace` | ToolStatus、GenerationProgress、RiskBadge | loading、generating、partial_completed、cancelled、timeout、error、success | `tool.call`、`tool.result`；PRD 建议：`tool.call.started`、`tool.call.progress`、`tool.call.completed`、`tool.call.failed`、`generation.progress` | Tool 执行、取消、重试 API/RPC | 可重试 Tool 范围 |
| 资产和黑板 | `/app/workspace`、`/app/assets` | WorkspacePanel、AssetCard、BlackboardElement、StoryboardCard | loading、empty、saving、save_failed、resume、success | PRD 建议：`workspace.assets.updated`、`workspace.blackboard.updated`、`asset.save.started`、`asset.save.completed`、`asset.save.failed`、`process.snapshot.saved` | 资产 API、Agent 过程快照、业务资产 RPC | 资产元素类型配置来源 |
| 会话历史和恢复 | `/app/sessions`、`/app/workspace/sessions/:sessionId` | SessionCard、SessionDrawer、RestoreBanner | loading、empty、filtered_empty、reconnecting、resume、error | `resume.accepted`；缺失事件补偿和快照恢复 | 会话列表 API、事件补偿 API、快照 API | 补偿窗口和快照结构 |
| 资产库 | `/app/assets` | Upload、AssetCard、AssetDrawer、AssetPicker | loading、empty、uploading、blocked、error、success | 无；工作台引用时消费相关事件 | 上传、预览、下载、引用 API/RPC | 文件大小和 MIME 错误码 |
| 个人 Skill 管理 | `/app/skills`、`/app/skills/:id/edit` | SkillCard、SkillBuilder、ToolPicker、TestCaseList | loading、empty、draft、testing、pending_review、rejected、published、deprecated、error | 无；工作台执行 Skill 时通过 Tag 展示 | Skill CRUD、测试、提交审核 API/RPC | Skill 字段和版本规则 |
| 企业 Skill | `/app/enterprise/skills` | SkillTabs、SkillCard、PermissionState | loading、empty、permission_denied、draft、pending_review、rejected、published | 无；工作台执行企业 Skill 时通过 Tag 展示 | 企业 Skill API/RPC | 企业拥有者权限字段 |
| 通知中心 | `/app/notifications` | NotificationList、NotificationDetail、UnreadBadge | loading、empty、unread、read、error、success | 无 | 通知列表、已读、跳转 API | 通知类型枚举 |
| 积分中心 | `/app/credits`、`/app/enterprise/credits` | CreditOverview、ExpiringCreditNotice、RedeemForm、LedgerList | loading、empty、redeem_success、redeem_failed、insufficient、error | 无；工作台扣费过程消费 AG-UI | 积分账户、兑换码、流水 API/RPC | 企业成员可见范围 |
| 企业成员管理 | `/app/enterprise/members` | MemberTable、InviteDialog、RemoveConfirm | loading、empty、invite_pending、confirming、permission_denied、error、success | 无 | 企业邀请、移除成员 API/RPC | 邀请链接有效期字段 |
| 企业拥有者转让 | `/app/enterprise/owner-transfer` | MemberSelect、ImpactConfirm | loading、selecting_member、confirming、error、success | 无 | 拥有者转让 RPC/API | 转让后登录态刷新 |
| 个人作品中心 | `/app/works`、`/app/works/:id/edit` | GalleryGrid、WorkCard、WorkForm、ShareModal | loading、empty、private、shared、taken_down、blocked、error、success | 无 | 作品 CRUD、公开快照、分享、取消分享 API/RPC | 分类和标签来源 |
| 精选作品中心 | `/explore`、`/explore/:publicWorkId` | PublicShell、WorkCard、PublicDetail、LikeButton、ShareButton | loading、empty、filtered_empty、login_required、private、taken_down、error、success | 无 | 公开作品、点赞、分享链接 API | 公开媒体访问方式 |
| 平台后台用户管理 | `/admin/users` | UserTable、UserDetailDrawer、StatusConfirm | loading、empty、filtered_empty、confirming、permission_denied、error、success | 无 | 用户查询、启停、审计 API/RPC | 脱敏展示字段 |
| 平台后台 Skill 审核 | `/admin/skills/reviews` | ReviewQueue、ReviewDetail、ReviewActionPanel | loading、empty、pending_review、approved、rejected、error、success | 无 | 审核结果、站内信、审计 API/RPC | 审核意见为空展示 |
| 平台后台模型和 Tool | `/admin/models`、`/admin/models/providers`、`/admin/tools` | ModelTable、ProviderTable、ToolTable、RiskBadge、WhitelistEditor | loading、empty、testing、connected、failed、enabled、disabled、confirming、error | 无 | 模型供应商、模型、Tool 管理 API/RPC | Tool 白名单维度 |
| 平台后台积分和兑换码 | `/admin/credits/grants`、`/admin/credits/codes` | GrantForm、CodeTable、ExportConfirm | loading、empty、confirming、active、redeemed、expired、disabled、failed、success | 无 | 积分发放、兑换码、导出、审计 API/RPC | 导出结果交付方式 |
| 平台后台精选作品和审计 | `/admin/works/public`、`/admin/audit-logs` | PublicWorkTable、TakeDownConfirm、AuditTable | loading、empty、filtered_empty、shared、taken_down、confirming、error、success | 无 | 下架公开作品、审计查询 API/RPC | trace_id 展示规则 |

## AG-UI 组件消费矩阵

| 前端组件 | 当前标准事件 | PRD 09 建议事件 | 展示状态 | 关键约束 |
| --- | --- | --- | --- | --- |
| `message.stream` | `message.started`、`message.delta`、`message.completed`、`agent.completed`、`agent.failed` | `agent.run.started`、`agent.message.delta`、`agent.message.completed`、`agent.run.completed`、`agent.run.failed`、`agent.run.cancelled` | streaming、completed、failed、cancelled | 增量渲染，重复 event_id 幂等忽略。 |
| `thinking.typewriter` | 暂无专门标准事件，可从消息或 Tool 状态降级展示 | `agent.thinking.started`、`agent.thinking.delta`、`agent.thinking.completed` | typing、completed、collapsed | 只展示可公开处理状态，不展示内部推理链路。 |
| `platform.tags` | `tool.call`、`tool.result` 可降级驱动 Tool Tag | `agent.skill.selected`、`agent.skill.missing`、`platform.tags.updated` | visible、active、disabled | 只展示公开名称、状态、风险，不展示内部 ID、成本、密钥。 |
| `chat.input.controls` | `interrupt.required` 可承接补充输入 | `chat.controls.requested`、`chat.controls.locked` | idle、editing、locked、invalid | 模型选择在输入框内，确认后锁定。 |
| `credit.estimate` | `interrupt.required` 可承接确认前信息 | `credits.estimated`、`credits.frozen`、`credits.charged`、`credits.released` | estimating、ready、insufficient | 积分不足不进入确认和生成。 |
| `confirmation.panel` | `interrupt.required`、`resume.accepted` | `confirmation.required`、`confirmation.accepted`、`confirmation.rejected` | required、accepted、rejected、expired | 高风险、扣费、业务写入必须确认。 |
| `tool.status` | `tool.call`、`tool.result` | `tool.call.started`、`tool.call.progress`、`tool.call.completed`、`tool.call.failed` | running、succeeded、failed、timeout、cancelled | 失败展示可理解原因，不展示敏感参数。 |
| `generation.progress` | 可由 `tool.call` / `tool.result` 降级展示 | `generation.progress`、`generation.artifact.completed` | queued、running、partial_completed、completed、cancelled、failed | 取消后展示已完成与未完成释放状态。 |
| `workspace.panel` | `resume.accepted` 可触发恢复后刷新 | `workspace.assets.updated`、`workspace.blackboard.updated`、`asset.save.started`、`asset.save.completed`、`asset.save.failed`、`process.snapshot.saved` | empty、assets、blackboard、updating、error | 按资产元素类型渲染，不按具体场景硬编码字段。 |
| `error.notice` | `agent.failed`、`tool.result` | `safety.prompt.blocked`、`tool.call.failed`、`asset.save.failed`、`agent.run.failed` | user_error、permission_denied、tool_error、model_error、system_error、blocked | 用户可理解，不暴露供应商原始响应和内部策略。 |

## 状态覆盖矩阵

| 状态 | 覆盖页面 | 触发来源 | 恢复动作 |
| --- | --- | --- | --- |
| loading | 所有页面 | 页面、列表、详情、表单提交 | Skeleton、禁用提交、保留布局 |
| empty | 列表和库 | 无数据 | 开始创作、上传、创建、清除筛选 |
| error | 所有页面 | API、RPC、AG-UI、网络错误 | 重试、返回、展示 trace_id 摘要 |
| success | 表单、保存、兑换、分享 | API/RPC 成功 | Toast、结果区、刷新列表 |
| streaming | Agent 工作台 | `message.delta` 或 PRD 扩展消息事件 | 停止、等待完成、断线恢复 |
| interrupt | Agent 工作台 | `interrupt.required` 或确认事件 | 确认、取消、修改后继续 |
| resume | Agent 工作台 | `resume.accepted`、Last-Event-ID 或快照恢复 | 补齐事件、刷新快照、保持内容 |
| blocked | 工作台、作品分享、上传文本 | 内容安全评估不通过 | 修改文本后重新评估 |
| permission_denied | 企业、后台、私有详情 | 权限不足或身份不匹配 | 返回、切换身份、重新登录 |
| insufficient | 工作台、积分中心 | 积分不足 | 兑换、联系企业拥有者、取消生成 |

## 依赖工程子域

| 设计条目 | Go Eino 智能体微服务依赖 | 业务微服务依赖 | 前端依赖 | 测试依赖 |
| --- | --- | --- | --- | --- |
| Agent 工作台 | Agent run、AG-UI 事件、Tool 状态、Interrupt/Resume | 积分预估、资产保存、模型和 Tool 配置查询 | 工作台组件、SSE 消费、状态管理 | 事件顺序、断线重连、确认流程 |
| 企业空间 | 当前空间上下文传入 Agent | 企业身份、成员、积分、Skill 权限 | 企业页面和权限态 | 企业成员权限和资产不可见性 |
| 作品中心 | 无直接编排依赖 | 作品、公开快照、点赞、分享、下架 | 作品管理和公开页 | 免登录查看、隐私边界 |
| 平台后台 | Tool / Skill / 模型能力边界反馈 | 后台配置、用户管理、积分、审计 | 后台页面和表格表单 | 审计、脱敏、权限 |
| 通知中心 | Skill 审核结果可触发协作事件 | 站内信创建、已读未读 | 通知列表和跳转 | 通知幂等和权限跳转 |

## 冲突与待确认

- AG-UI 标准事件与 PRD 09 扩展事件命名需要在正式 AG-UI 契约中统一。
- `docs/contracts/` 目录当前不存在，API/RPC 依赖只能标记为待契约确认。
- 当前 PRD 套件仍为 Draft，对应页面只能作为设计草案和需求映射，不进入正式开发。
- 页面路由为草案，最终由前端工程师结合应用框架和路由规范确认。
- 任何页面字段、权限字段、状态枚举和 payload 都必须以后续契约为准。

## 验收标准

- [ ] 页面、路由、组件、状态和 AG-UI 消费点已形成矩阵。
- [ ] 覆盖 loading、empty、error、success、streaming、interrupt、resume。
- [ ] 明确非 Agent 实时页面不消费 AG-UI，主要依赖 API/RPC。
- [ ] 明确 AG-UI 标准事件与 PRD 扩展事件需要后续契约统一。
- [ ] 明确不发明后端字段，字段以 API、AG-UI、RPC 契约为准。
