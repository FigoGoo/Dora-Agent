# Dora 服务端需求总览

> 文档状态：需求基线稿
>
> 版本：v0.5
>
> 更新日期：2026-07-14
>
> 关联文档：[共通业务规则与验收基线](common-requirements-baseline.md)、[支付与积分充值需求总览](payment-requirements-overview.md)、[Graph Tool 功能需求总览](graph-tool-requirements-overview.md)、[用户端需求总览](user-requirements-overview.md)、[管理端需求总览](admin-requirements-overview.md)

## 1. 文档目的

本文定义 Dora 用户端和管理端所依赖的生产级服务端能力、领域概念、服务边界、业务状态、可靠性规则与验收口径。

本文用于：

1. 统一 Project、Skill、Skill 调用、Graph Tool、Agent、故事板、资产、支付、积分、发布者收益、精选作品和公告等概念。
2. 明确业务服务、Agent 服务、AIGC Worker 和平台基础能力的职责边界。
3. 为后续领域设计、服务拆分、接口设计、数据设计、测试和运营验收提供需求依据。
4. 继承现有三份 AIGC 设计文档中可用于生产版的领域原则，同时废弃其中仅适用于本地 Demo 的假设。

本文是服务端需求总览，不规定具体 Go 包、API 路径、DTO、数据库表或部署拓扑。Agent 实现必须另行遵循已审核的 [Agent Service 开发规范](../../.agents/skills/dora-server-development/reference/agent-development-standards.md) 和每个 Agent-facing Graph Tool 的独立设计文档。

## 2. 需求描述与服务端目标

Dora 服务端需要支持以下完整业务闭环：

```text
个人用户通过微信支付或支付宝购买消费积分
→ 服务端验签通知或主动查单确认支付，并唯一履约到账
→ 使用到账积分开始创作

个人用户快速创建 Project
→ 在 Project 中启用一个或多个 Skill
→ Agent 根据 Skill 调用规则选择实际 Skill
→ 记录 Skill Invocation，并按实际模型、Graph Tool、媒体和公开 Tool 可计费执行扣除积分
→ 用户显式选择或由 Agent/Skill 调用六个必备 Graph Tool，并使用公开 Tool 完成创作
→ 发布者获得可追溯收益
→ 用户可将作品及脱敏创作过程投稿为精选作品
→ 平台完成审核、运营、风控、结算和公告管理
```

服务端核心目标：

- 所有用户操作、Agent 运行、Skill 调用、异步任务和账务都可恢复、可追踪、可审计。
- 微信支付采用 Native 扫码，支付宝采用电脑网站支付；支付确认只信任验签通知或受信主动查单，且每笔成功订单只履约一次积分。
- 同一 Project 能够同时启用多个 Skill；只对实际开始的可计费执行扣费，并归因到对应 Skill Invocation 或平台原生 Graph Tool Run。
- Skill 通过结构化字段约束六个 Graph Tool、通过受控 `@Tool` 引用公开 Tool，不允许模型或用户绕过权限、版本和计费边界。
- 用户可以在工具箱显式选择 Graph Tool；服务端必须冻结用户选择的 `requested_tool_key`，不能由模型静默替换。
- Chat、A2UI、故事板和资产始终投影同一份持久化业务事实。
- 用户消费积分与发布者收益积分分账；可计费执行开始时直接扣费，不使用预占、不支持退款，并支持账务冲正、收益冲正和五折收益回收。
- 精选作品只公开作者确认并经平台审核的快照，不泄露私有创作数据。

## 3. 服务端用户与系统参与者

### 3.1 外部用户

| 用户 | 服务端需要支持的能力 |
| --- | --- |
| 游客 | 浏览公开 Skill、精选作品和当前有效全局公告。 |
| 个人用户 | 使用微信/支付宝购买消费积分，创建 Project、使用 Agent、显式选择 Graph Tool、启用 Skill、管理故事板和资产、消费积分、点赞和提交工单。 |
| Skill 创建者/发布者 | 创建结构化 Skill、引用公开 Tool、测试与发布、查看 Dashboard、获得收益积分并申请五折回收。 |

### 3.2 内部用户

- 平台管理员。
- Skill 与 Tool 审核人员。
- 市场和精选作品运营人员。
- 内容安全、版权和风控人员。
- 客服、财务、审计和系统运维人员。

### 3.3 系统参与者

- 用户端 Web 与管理端 Web。
- Agent Runtime。
- AIGC Worker。
- 模型、图片、视频、音频、剪辑和装配 Provider。
- 对象存储、支付、实名、打款及其他受控第三方服务。

## 4. 范围与服务端原则

### 4.1 本期范围

- 个人身份、个人发布者和内部管理员权限。
- Project、Session、Agent Turn、多 Skill 启用及 Skill 实际调用。
- 结构化 Skill 草稿/发布、审核、市场和 `@Tool` 引用。
- Chat/A2UI、动态故事板、资产和异步媒体生产。
- 微信支付、支付宝支付、积分商品、支付订单、积分履约、消费积分、发布者收益积分、结算、回收和打款。
- 精选作品公开快照、点赞和创作过程查看。
- 平台公告全局弹窗。
- 内容安全、风险控制、审计和生产可观测性。

### 4.2 基本原则

1. PostgreSQL 或等价的持久化业务存储是业务状态真源，队列和缓存不能成为唯一真源。
2. 所有实际运行、计费和公开展示引用不可变版本或快照，后续编辑不得静默改写历史事实。
3. Agent 只理解意图、选择 Skill 和 Graph Tool，不直接操作余额、权限、用户资产 CRUD、Provider 密钥或发布者收益。
4. 有界规划可以在 Agent/Graph 中执行，跨分钟的媒体任务必须交给 Worker，不维持常驻 Graph 调用栈。
5. Worker 只负责可靠完成异步任务和资产收尾，不选择 Skill、不决定下一 Agent 阶段、不生成 A2UI、不直接写发布者收益。
6. 用户、客户端和模型提供的价格、身份、权限、Tool 标识及最终费用均不是可信事实，必须由服务端上下文和快照确定。
7. 本期 Skill 只允许结构化配置及平台开放 Tool；不支持任意可执行代码。

## 5. 核心业务对象

### 5.1 对象关系

```text
User
├─ Project
│  ├─ Session
│  │  ├─ Agent Run / Turn
│  │  │  ├─ Skill Invocation（0..N）
│  │  │  │  └─ Graph Tool / Public Tool Run
│  │  │  ├─ Platform Native Graph Tool Run（0..N）
│  │  │  └─ A2UI Event
│  │  ├─ Enabled Skill Binding（0..N）
│  │  └─ Chat History
│  ├─ Storyboard
│  │  └─ Revision → Module/Element → Prompt Slot/Asset Slot → Asset Binding
│  ├─ Asset
│  └─ Featured Work Snapshot（可选）
│
├─ Points Account
│  └─ Charge / Tool Cost Summary / Reversal / Ledger
├─ Payment Order
│  ├─ Channel Attempt / Notification / Query
│  └─ Points Fulfillment → Recharge Ledger
│
└─ Publisher Profile（可选）
   ├─ Skill → Draft / Published Snapshot → Tool Reference
   └─ Earnings Account → Settlement / Withdrawal / Earnings Ledger

Generation Operation
└─ Batch
   └─ Job
      └─ Provider Receipt / Asset

Announcement
└─ Announcement Version → Display Rule / User Display Receipt
```

### 5.2 关键定义

| 对象 | 定义 |
| --- | --- |
| Project | 长期创作容器，是 Chat、故事板、资产、Skill 和费用的用户入口。 |
| Session | Project 内按序处理用户输入、Agent 结果和异步回流的持久化会话。 |
| Enabled Skill Binding | Project/Session 当前允许 Agent 选择的 Skill；Session 创建时冻结当时的 published snapshot。 |
| Skill Invocation | Agent 实际选择并开始执行一个 Skill 的业务调用，是调用归因、运行追踪和发布者收益归属的最小单位；用户积分按其内部实际可计费执行逐项扣除。 |
| Agent Turn | 一次持久化输入驱动的 Agent 处理过程，可以不调用 Skill，也可以调用一个或多个 Skill。 |
| Graph Tool Definition/Version | 平台维护的稳定高层能力定义及已发布版本，包含用户目录信息、输入输出、前置条件、权限、预算、计费引用和状态。 |
| Graph Tool/Public Tool Run | 用户直接调用或 Skill Invocation 内部执行的高层 Graph Tool，以及 Skill 调用的公开 Tool 运行记录；Graph Tool 终态汇总其内部可计费执行，不额外产生 Tool 完成费。 |
| Payment Order | 用户购买消费积分时冻结商品、金额、积分、渠道和用户的支付事实；浏览器回跳不能确认其成功。 |
| Points Fulfillment | 支付确认后按冻结商品快照向用户积分账户唯一入账的履约事实，与支付状态独立。 |
| Asset | 用户上传或系统生成的图片、视频、音频、文本、结构化内容和交付物。 |
| Featured Work Snapshot | 从私有 Project 派生、经作者确认与平台审核的不可变公开作品和创作过程快照。 |

## 6. 服务端能力总览

```text
服务端
├─ 接入、身份与权限
├─ 用户与发布者
├─ 快速创作与 Project
├─ Session、Chat 与 A2UI
├─ Skill 草稿、发布与市场
├─ 公开 Tool 目录与授权
├─ 多 Skill 启用与实际调用
├─ Agent Runtime
├─ Graph Tool、动态故事板与确定性命令
├─ AIGC Generation Worker
├─ 资产中心与对象存储
├─ 微信/支付宝支付、积分订单与计费
├─ 发布者收益积分、结算与回收
├─ 精选作品与点赞
├─ 平台公告全局弹窗
├─ 内容安全、版权与风控
├─ 管理端与数据支撑
└─ 可靠事件、可观测性与基础设施
```

## 7. 详细能力需求

### 7.1 接入、身份、权限与审计

- 提供用户端和管理端所需的受控访问能力。
- 支持个人账号、个人实名认证、个人发布者身份和内部管理员 RBAC。
- 对 Project、Session、Asset、Skill、收益、收益回收、精选作品等对象执行资源归属校验。
- 管理用户对 Tool、Connector 和第三方数据的授权，并支持撤销。
- 敏感操作必须二次校验、记录原因并写入不可篡改审计记录。
- 客户端或模型传入的 `user_id`、发布者、价格和权限不得覆盖服务端身份上下文。
- 对登录、创作、Skill 调用、Tool、上传、点赞、支付和管理端操作执行适当限流。

### 7.2 快速创作与 Project 初始化

#### 7.2.1 功能目的

保证“快速创作”无论是否填写首条提示词，都只创建一个可继续、可恢复的 Project，并且不会产生半成品运行或重复计费。

#### 7.2.2 创建要求

- 一次有效快速创建必须生成唯一 `project_id`，并创建默认 Session、空故事板/资产工作区和初始运行上下文。
- 首条提示词为可选值。
- 有非空提示词时，在同一业务提交中建立 Project、Session、首条用户消息、持久化输入和可靠唤醒事件；提交后异步启动 Agent。
- 空值或纯空白提示词只创建 Project、Session 和空工作台，不创建消息、Agent Turn、Skill Invocation 或积分扣费。
- Project 创建成功后，用户端可以立即凭 `project_id` 打开工作台，不必等待 Agent 执行完成。
- Project 记录所有者、创建来源、首提示词状态和初始启用 Skill 集合。

#### 7.2.3 幂等与异常

- 客户端每次创建使用稳定请求标识。
- 相同请求标识与相同语义的重试返回同一 `project_id`。
- 相同请求标识携带不同提示词或配置时必须返回语义冲突，不能覆盖原 Project。
- 业务提交成功但运行唤醒失败时，由可靠事件重新投递。
- 不允许出现“Project 不存在但已经执行/收费”“Project 已创建但首消息重复”或“重复点击创建多个 Project”。

### 7.3 Project、Session、Chat 与创作工作台

- Project 提供故事板、资产、Chat 历史、已启用 Skill、运行状态和费用摘要的统一权威读模型。
- Session 输入必须先持久化再执行；同一 Session 的业务输入按确定顺序处理。
- 右侧 Chat 支持流式文本、A2UI 表单/确认卡和高层 Skill/Graph Tool 状态。
- 左侧只展示故事板、资产、候选结果和必要生成进度，不在 Chat 中复制逐 Job 或逐资产内部卡片。
- 事件流必须有持久化序号，支持断线续传、历史回读和版本缺口回源。
- 刷新、服务重启和实例切换不能丢失已经接受的用户消息或已冻结 Agent 输出。
- Project 和 Session 默认只允许所有者访问，本期不支持团队共享或协同编辑。

### 7.4 结构化 Skill 定义

Skill 需要按字段保存，不能只存储为一段拼接文本。生产版 Skill 定义至少包含：

| 字段 | 服务端语义 |
| --- | --- |
| Skill 名称 | 公开名称及唯一业务标识。 |
| 简介、分类、标签、展示内容 | 市场检索、展示和审核元数据。 |
| 输入与输出 | 支持的输入类型、约束及预期输出。 |
| Skill 调用规则 | 多 Skill 同时启用时，供 Agent 判断何时选择本 Skill。 |
| 流程规划 | 执行顺序、依赖关系和与用户交互规则。 |
| 素材分析 | 对输入资产的分析、提取、整理和基础处理要求。 |
| 故事板设计 | 故事板元素、镜头、画面和镜头语言要求。 |
| 媒体生成 | 模型、参考资产、图片/视频/音频及输出设置要求。 |
| 提示词写法 | 图片、视频等 Provider Prompt 的编写规则与效果优化要求。 |
| 视频剪辑 | 裁剪、时间线、音视频对齐、转场、连贯性和导出要求。 |
| 示例与开场提示 | 用户可见示例及测试输入。 |
| Tool References | 从结构化字段中的 `@Tool` 引用解析出的稳定引用集合。 |
| Billing Reference | 运行时需要展示和冻结的平台模型、媒体、公开 Tool、Graph Tool 计费配置引用、预计积分范围和费用上限；发布者不得自行定价。 |

字段必填矩阵、长度、格式和“不适用”规则由平台配置，但服务端必须保留字段边界、变更历史和版本差异。

### 7.5 `@Tool` 引用与公开 Tool 目录

- 服务端提供发布者可见且可引用的公开 Tool 目录及能力说明。
- 前端输入 `@` 只是选择体验，服务端必须权威解析和校验。
- Tool 显示名称与稳定引用分离；保存时关联 Tool 标识、版本或明确的兼容版本策略及所需权限。
- Tool 改名不得改变既有 Skill 的执行语义。
- 未知、重名、无权限、已暂停或已废弃 Tool 引用必须被拒绝或阻止发布。
- Skill 发布快照冻结 Tool、权限、计费配置引用和安全策略；具体执行价格在可计费执行开始时按已发布计费配置版本冻结，运行时不得静默替换为未审核 Tool 或未发布价格。
- Tool 凭证由平台托管，不能写入 Skill、模型上下文、用户响应或普通日志。
- Tool 目录至少包含能力说明、输入输出、风险等级、权限、成本估算、并发/频率限制、状态和版本信息。
- 本期不允许用户通过 `@` 引用任意字符串、私有可执行文件或未经平台审核的 Tool。

### 7.6 Skill 草稿、发布、审核与市场

- Skill 产品状态只支持 `draft/published`，不提供版本明细、版本列表、版本切换或版本回滚。
- 支持保存草稿、自动保存、复制、测试、预览、提交审核、驳回、发布、再次发布、暂停、恢复和下架。
- 每个 Skill 保存一个可编辑草稿和一个当前发布快照；已发布 Skill 的编辑只修改草稿，再次审核通过后原子替换发布快照。
- 发布只对发布后创建的新 Session 生效；已有 Session 始终使用创建时冻结的 Session Skill Snapshot。
- 服务端保留内部 `publication_revision`、digest、发布时间和操作人用于 CAS、幂等和审计，但不得作为用户可见版本明细。
- 提交审核时冻结结构化字段、Tool 引用、权限、计费、测试结果和市场展示快照。
- 发布前校验至少覆盖字段完整性、有效执行能力、Tool 解析、最小权限、冲突/循环、测试结果、成本上限、Prompt Injection、隐私和内容安全。
- Skill 测试调用必须标记为测试，不得进入公开调用量或生成发布者收益积分；测试中实际开始的模型、媒体或公开 Tool 执行仍按测试账号适用的已发布计费配置扣除积分。
- Skill 市场提供公开详情、搜索、分类、标签、榜单、推荐、收藏、最近使用和发布者主页所需读模型。
- 用户端只按“草稿/已发布”分组。已有发布快照且同时存在草稿时仍属于“已发布”，可以标记“有未发布修改”；审核/暂停/下架作为独立治理信息展示。

建议状态：

```text
content: draft / published
governance: reviewing / rejected / suspended / offline
```

### 7.7 多 Skill 启用、选择与实际调用

#### 7.7.1 启用规则

- 用户可以在 Project/Session 中启用一个或多个已发布 Skill。
- Enabled Skill Binding 记录 Skill ID、启用人、启用时间、停用时间和来源；Session Skill Snapshot 记录内部 publication revision/digest。
- 启用、停用、展示、预加载或读取调用规则都不扣除积分。
- Skill 被暂停、下架、Tool 被禁用或用户权限失效后，新 Session/新调用必须 fail closed；已有 Session 按安全治理策略停止或继续冻结快照，不能静默切换内容或 Tool。

#### 7.7.2 Runtime 选择

- Agent 在 Session 创建时读取当前已启用且可执行的 Skill 发布快照并冻结；每轮只读取 Session Snapshot。
- 每个 Skill 的名称、调用规则和结构化执行内容必须保持可识别边界，不能把多个 Skill 无标识拼接成一段指令。
- Agent 只能选择当前已启用的 Skill；模型给出的 Skill 名称不是权威标识。
- Agent 实际选定 Skill 并开始业务执行时，创建唯一 Skill Invocation。
- 一个 Agent Turn 可以不调用 Skill，也可以调用一个或多个 Skill；同一项目可以多次调用相同或不同 Skill。
- Skill Invocation 记录 Project、Session、Run、Turn、Skill、Session Snapshot digest/内部 publication revision、调用序号、选择原因、计费配置与收益规则引用、Tool、权限和安全快照；内部 revision 不对用户展示为版本明细。

#### 7.7.3 调用边界

- 每个新的业务调用创建新的 Skill Invocation；用户积分按 Invocation 下实际开始的模型、媒体、公开 Tool 或 Graph Tool 可计费执行逐项扣除，不收取统一的 Invocation 完成费。
- 同一 Skill Invocation 内的模型重试、Tool 重试、Worker 回调、Checkpoint/Receipt 重放、Interrupt/Resume 和投影恢复必须复用原 Invocation 与原扣费结果，不能重复扣费或重复记收益。
- 多 Skill 冲突、循环调用、每轮最大 Skill 数、总调用次数和最大积分预算由服务端守卫。
- 达到调用或积分上限时，在新的可计费执行开始前进入正式 Approval 等待或安全终止，不能静默超额执行。

### 7.8 Agent Runtime

Agent 服务使用 Eino ADK 的 Agent 与 Runner 能力处理持久化输入，满足：

- v1 只使用一个主 `ChatModelAgent` 处理多轮意图、动态 Skill 和 Graph Tool 调用，不使用子 Agent、DeepAgent、AgentAsTool 或多 Agent 编排。
- 生产执行通过 `Runner` 管理事件、检查点和 Interrupt/Resume，不直接把裸 Agent 调用作为业务运行入口。
- 所有 Session Input 先持久化再唤醒；同一 Session 使用持久化 Lane、Lease 和 Fencing 串行处理，后续输入不得越过未决前序输入。
- Session 创建时冻结 Skill；每个新 Turn 冻结消息边界、Prompt、Tool Registry、预算和计费配置引用。技术重试和 Resume 不得混入后续消息或新发布 Skill。
- PostgreSQL 是 Run、Turn、Checkpoint、Model/Tool Receipt、Approval、Operation、Outbox 和 EventLog 权威来源；Redis 只缓存和唤醒。
- 模型和 Tool 完整输出先冻结 Receipt 再投影；投影失败、进程重启或 Resume 时重放冻结结果，不重新调用模型、Graph Tool 或产生新扣费。
- 对交互式多轮输入提供排队、安全取消、空闲和优雅结束能力；v1 默认保持 Session Head-of-Line，不允许后续输入无条件抢占已产生副作用的前序 Turn。
- 动态装载当前 Project 已启用 Skill，并根据调用规则选择实际 Skill。
- 使用 Skill、上下文注入、Tool 结果缩减、对话摘要和异常修复等 Middleware，保持长会话可运行。
- Retry 和 Failover 由模型调用层的单一 Owner 在同一 Turn 预算内有限执行；Graph、Runtime 和客户端不得叠加重试同一次模型或外部副作用。
- 高风险操作在副作用和扣费前使用 PostgreSQL 正式 Approval；自然语言中的“确认”不构成审批，重复决定、过期、Owner 或资源版本不匹配必须失败关闭。
- Cancel 必须传播到 Runner、Graph、模型、RPC 和可取消 Tool；取消前未开始的执行不扣费，已经开始的外部副作用继续收尾或核对，已发生扣费不退款。
- 模型输出不能自行确定用户、权限、可信 Tool、最终价格、账务结果或发布者收益。

v1 Agent-facing Graph Tool 白名单必须完整包含：

```text
plan_creation_spec     流程规划
analyze_materials      素材分析
plan_storyboard        故事板设计
generate_media         媒体生成
write_prompts          提示词写法
assemble_output        视频剪辑
```

- 六个 Tool 必须具有用户可见 Definition、工具箱入口和可执行版本；仅存在内部 Node 或名称相近的旧 Capability 不视为完成。
- 工具箱显式选择产生受信 `requested_tool_key`；Agent 可以补问缺失输入，但不能静默换 Tool。自然语言调用可以由 Agent 选择 Tool，但必须在输出中展示实际选择。
- `write_prompts` 是独立 Graph Tool；`plan_storyboard` 负责 Prompt Slot 与需求，不能只依靠隐藏内部 Prompt 节点代替用户可用的提示词写法能力。
- `generate_media` 和 `assemble_output` 同时支持故事板模式与无 Storyboard 的独立模式；独立结果不得自动覆盖或绑定 Active Storyboard。
- Skill 可以通过结构化字段指导这些 Graph Tool 及公开 Tool，但不得新增 Tool、扩权、提高预算、选择 Provider 或改变计费规则；Agent 不得直接暴露底层 Provider 或业务 CRUD。
- Graph Tool 只处理一次有界规划或派发；异步 Graph Tool 创建持久化 Operation/Batch/Job 后返回 `accepted`，等待用户和 Worker 结果通过持久化状态继续，不维持跨分钟调用栈。
- 每个 Graph Tool 使用稳定 Tool Key、版本化 Schema、严格 Intent/Result DTO、确定性 Validator、业务状态机、幂等键、硬预算和独立中文设计文档。
- 无需 Agent 解释的确定性成功结果只刷新工作台，不强制新增 Agent Turn。
- 六个 Tool 的用户场景、能力边界、输入输出、计费和专项验收统一以[Graph Tool 功能需求总览](graph-tool-requirements-overview.md)为准。

### 7.9 动态故事板、资产绑定与确定性命令

- 故事板按 Revision 管理，支持动态 Module、Element、Prompt Slot、Asset Slot 和依赖关系。
- 支持整体重新规划、Prompt 定向编辑、目标局部重新生成、上传资产绑定、候选确认、激活和版本回退。
- 用户明确指定目标的编辑、绑定、确认和局部重生成使用确定性领域命令，不要求 Agent 重新理解目标。
- 所有命令需要稳定目标标识、期望版本和幂等标识。
- 用户修改后，旧 Job 结果只能标记为 superseded/quarantine，不能覆盖较新的 Prompt、故事板或 Active Asset。
- 上游资产或镜头变更需要按确定依赖图传播 stale 状态，不能由 Agent 或 Worker 自由猜测影响范围。
- A2UI 是领域事实的用户投影，不是独立业务真源。
- A2UI 主要用于用户端创作页聊天框，后端和前端必须分别使用独立可扩展包目录。
- 所有 A2UI 组件以具备安全 Markdown 展示能力的 Card 为公共基类/组合结构；首期至少支持单选、多选、提交按钮、输入框、多图片、多视频、多音频、纵向步骤条、Tool Renderer 和 Status Renderer。
- 后端使用版本化 Envelope、组件/Action Registry、严格 Validator、Inbox/Projector 和 EventLog；未知版本、组件或 Action 失败关闭或降级为不可交互 Card。
- Worker 不生成 A2UI；Business/Worker 领域事件进入 Agent Inbox 后，Agent Projector 更新 Card 和 `refresh_resources`。

### 7.10 AIGC Worker 与媒体能力

- 异步 Graph Tool 创建持久化 Generation Operation、Batch、Job 和可靠事件后立即返回 `accepted`。
- Worker 使用 At-Least-Once + 幂等语义，从 PostgreSQL 权威状态 Claim Job；Redis 只负责低延迟唤醒，丢失唤醒时 PostgreSQL 轮询必须恢复。
- Claim 使用短事务、Lease 和递增 Fencing Token；Heartbeat、重试和终态提交必须同时校验旧状态、Lease Owner 和 Lease Version，过期 Worker 不得覆盖新 Owner 结果。
- Worker 负责 Provider Submit/Poll/Cancel、错误分类、有限重试与 Full Jitter、TOS 上传、可信执行回执、调用 Business Finalize 和 Batch Barrier；Worker Pool、Claim Batch 和下游并发必须有硬上限。
- 媒体能力覆盖图片、视频、音频、资产预处理、截帧、裁剪、时间线、音视频对齐、转场、装配和导出；具体开放能力由 Tool/Provider 配置决定。
- 每个生成执行在正式调用 Provider 前通过 Business 权威计费能力原子扣除冻结配置积分；扣费失败不得启动 Provider，扣费成功后以稳定账务幂等键恢复，不因 Job 后续失败或取消退款。
- Job 只有在 Provider、TOS 上传和 Business Finalize 完成后才成功；Storyboard Binding 按 `binding_mode` 可选。Graph Tool/Batch 终态只汇总已有扣费，不二次收费。
- Provider 已完成但后续收尾失败时，应复用已冻结结果和用量回执，不能无条件重跑 Provider。
- Provider 请求结果未知时进入 `reconciling` 并按原幂等键查询；自动重试耗尽或永久错误进入 `dead`，人工重放创建新 Job，不原地重置终态。
- 迟到、过期或目标版本不匹配的结果不得绑定为 Active Asset，并按安全规则隔离或清理。
- Worker 不选择 Skill、不决定下一 Agent 步骤、不生成 Chat/A2UI、不直接调用 Runner、不计算发布者收益积分。
- 用户取消只阻止未开始执行；已调用 Provider 或已扣费的执行继续安全收尾或核对，已发生扣费不退款。
- Worker 优雅退出时先停止 Claim，再在有界窗口内 Drain 并继续 Heartbeat；未完成 Job 留在可由新 Lease 接管的权威状态。
- Batch 全部 Job 进入业务终态后，Worker 只发布一次 `generation.batch.completed|partial_failed|failed|cancelled`；Agent Inbox 幂等转换为 `BatchContinuationResult` 并唤醒 Session Lane。

### 7.11 资产中心与对象存储

- 统一登记用户上传、AI 生成、候选、Active、已替代和最终交付物 Asset。
- Asset 保留所属用户、Project、Session、Skill Invocation、Operation、Job、媒体元数据、来源、版本、版权和安全状态。
- 对象默认私有，使用短期授权访问，不使用长期公开 URL 暴露用户资产。
- 支持上传校验、幂等、大小/类型限制、病毒与内容安全检查。
- 支持查询、预览、下载、复用、重命名、删除、回收站、引用保护和孤儿对象清理。
- 资产中心跨 Project 查询仍按个人用户隔离。
- 精选作品使用独立公开快照和公开派生资产，不直接暴露私有 Asset 地址。

### 7.12 积分、订单与 Skill 调用计费

#### 7.12.1 积分商品与充值支付

- 提供版本化积分商品，创建订单时冻结商品版本、人民币分值、基础积分、赠送积分、最终到账积分、活动规则、用户和渠道；金额使用整数分，积分使用 `bigint`，不得用浮点数或回调参数反推积分。
- v1 支持微信支付 APIv3 Native 扫码和支付宝电脑网站支付。微信二维码、支付宝同步回跳、前端轮询参数和客户端声明均不能确认支付成功。
- Payment Order、Channel Attempt、Notification、Points Fulfillment 和积分账本分别保留权威状态和因果关联；支付已成功但积分未到账时继续履约，不要求用户再次付款。
- 服务端对渠道通知验签/解密并校验商户身份、渠道、商户订单号、渠道交易号、币种、冻结金额和官方成功状态；通知缺失或请求结果未知时按原商户订单号主动查单。
- 创建订单、渠道交易号、Notification 和积分履约分别使用稳定幂等键；重复回调、查单、进程重启、扫描和人工恢复只能确认一次支付并写入一笔正常充值积分。
- 未支付订单可以关闭；平台不提供用户自助或客服普通充值退款。支付机构强制撤销等法定异常进入 `provider_exception`，保留原账本并走受控积分冻结、冲正或追偿流程。
- 支付渠道配置、证书/公钥轮换、回调、查询、关单、履约、对账和异常处置的完整要求以[支付与积分充值需求总览](payment-requirements-overview.md)为准。

#### 7.12.2 计费单位

- 计费单位是使用已发布平台计费配置的一次模型、媒体、公开 Tool 或 Graph Tool 内部可计费执行，不是固定的一次 Skill Invocation 完成费。
- 仅启用 Skill、加载配置、读取调用规则、展示 Graph Tool 或聚合已有费用都不扣积分。
- 每个可计费执行开始前冻结计费配置版本、用户积分、计费单位、模型/能力标识、Skill Invocation、预算和稳定账务幂等键。
- Graph Tool 终态汇总其下去重后的可计费执行；异步 Tool 返回 `accepted` 时费用可以尚未汇总完成。
- 同一执行的技术重试、Resume、Worker 回流、Checkpoint/Receipt 重放和投影恢复复用原扣费；用户主动重新生成创建新的计费执行。

#### 7.12.3 账单构成

服务端按冻结的平台配置透明拆分：

```text
单个 Graph Tool 用户积分
= Σ 去重后的模型/媒体/公开 Tool/生成执行积分

单个 Skill Invocation 用户积分
= Σ 归属于该 Invocation 的 Graph Tool 用户积分
```

计费配置由平台维护并版本化。发布者、用户、客户端和模型不能覆盖价格；Provider 实际成本可以作为平台经营数据，但不得直接作为客户端提交的可信用户积分。

#### 7.12.4 扣费、无退款与冲正

- 本期不使用积分预占。Business Service 在可计费执行正式开始前，以一个原子账务操作完成余额校验和积分扣除；扣费失败时不得调用模型、Provider 或公开 Tool。
- 扣费成功且外部执行已经正式开始后，即使失败、部分成功、取消、超时、结果被替代或用户未采用，也不退款。
- 执行开始前取消不扣费；开始后取消只停止后续可取消步骤，已发生扣费保持有效。
- 请求结果未知时不得使用新账务键盲目重试，必须先按原幂等键查询模型/Provider/Tool 回执和 Business 账本。
- 消费积分账本不可变。重复扣费、错扣或权威事实证明没有对应可计费执行时，通过唯一追加式冲正纠错；冲正不是产品退款。
- 充值订单和支付回调必须遵循本节支付规则；本期不提供普通充值退款。
- 完整场景处理遵循[共通计费真值表](common-requirements-baseline.md#32-计费真值表)。

### 7.13 发布者收益积分、结算与回收

- 用户消费积分账户与发布者收益积分账户在逻辑和账务上分离。
- 发布者收益积分由平台收益规则根据有效已发布 Skill Invocation、归属明确的已扣费明细和风险结果生成，不直接等同于用户总扣费或 Provider 成本。
- 收益规则版本在 Invocation 开始时冻结；合格收益先进入待结算，结算周期和风控通过后进入可用。
- 发布者收益积分至少支持待结算、可用、冻结、已回收和已冲正；冻结可以恢复为可用或通过追加式收益冲正结束。
- 平台按 `50%` 系数回收可用收益积分；不足最小结算单位的余数保留在收益账户。
- 发布者可以将可用收益积分转换为消费积分，或申请收益回收；两种操作必须使用不同幂等键和账本记录。
- 支持结算周期、最低回收积分、实名/收款校验、风控审核、财务审核、打款和对账。
- 发布者自测、自买、刷量、虚假用户和风险流量不得制造可回收收益。
- 收益、冲正、转换和回收账本不可变且能够关联到原始 Skill Invocation、Graph Tool 和适用扣费明细。

### 7.14 “我的 Skill”Dashboard

服务端为发布者提供总览和单 Skill/时间范围聚合，不提供用户可见版本筛选：

- 市场曝光、详情访问和启用次数。
- 实际 Skill Invocation、独立使用用户、成功、失败、取消和部分成功。
- 积分扣费、账务冲正和平均单次积分。
- 待结算、可用、冻结、冲正和已回收收益积分。
- 关联精选作品及浏览、点赞和 Skill 导流。
- Skill 发布、审核、暂停、下架和 Tool 风险状态。

统计规则：

- Invocation 是调用量真源，启用、规则读取和技术重试不得增加调用量。
- 聚合指标需要提供一致口径、统计周期和数据延迟说明。
- 发布者只能查看自己 Skill 的聚合和允许公开的脱敏失败信息，不能读取使用者完整 Prompt、Chat、私有 Asset 或身份信息。
- 风控剔除不删除原始事实，必须保留剔除原因和审计记录。

### 7.15 精选作品、公开创作过程与点赞

#### 7.15.1 作品快照

- 作者只能从自己有权访问且满足条件的 Project/交付物创建精选作品投稿。
- 发布时生成不可变 Featured Work Snapshot，包含作品、封面、标题、标签、作者公开资料、媒体信息、实际使用的 Skill 与 Session Snapshot 摘要、公开创作过程和版权声明。
- “使用的 Skill”从 Project 的实际 Skill Invocation 快照生成，作者不能伪造不存在的 Skill 调用。
- 作者提交前预览并明确确认公开内容；平台审核通过后才能公开。
- Project 后续修改不得静默改变已发布作品，更新需要创建新快照并重新审核。
- 支持作者撤回、平台暂停/下架、版权投诉和安全处置。

#### 7.15.2 公开创作过程

服务端需要生成专门的脱敏 Process Snapshot，可以包含：

- 作者同意公开的创作需求和 Prompt。
- 选定的故事板版本及关键镜头。
- 被选公开资产的演进和最终结果。
- 实际使用的 Skill、Session Skill Snapshot 摘要和高层 Graph Tool 步骤。

严禁直接公开：

- 完整私有 Chat、系统提示词和 Skill 隐藏指令。
- 模型隐藏推理过程。
- Tool 参数、密钥、Provider 请求和内部运行日志。
- 未授权、已删除、隔离或存在版权风险的 Asset。
- 内部积分交易标识、风控和管理端数据。

#### 7.15.3 点赞与无评论边界

- 点赞以用户与作品的唯一关系为真源，点赞和取消均幂等。
- 同一用户并发或重试点赞同一作品最多计一个有效点赞。
- 点赞计数可以异步投影，但必须能够从唯一关系修复。
- 支持异常点赞识别、冻结和从公开计数中剔除。
- 本期不创建 Comment 对象、评论入口、评论计数、回复、`@用户` 或评论通知能力。

建议状态：

```text
draft → submitted → reviewing → published/rejected
published → suspended/offline
```

### 7.16 平台公告全局弹窗

- 管理端维护公告草稿、版本、展示时间、失效时间、优先级、频率、发布和撤回状态。
- 用户端应用壳进入任意页面时查询当前有效公告并以全局弹窗展示。
- 多个公告按确定优先级和发布时间排队。
- 记录用户对公告版本的展示和关闭回执，用于“每版本一次”“每次登录”等频控。
- 公告正文更新需要形成新版本，才能按新版本重新展示。
- 已撤回或过期公告不得继续展示，缓存不能使撤回公告长期残留。
- 不生成站内信、个人定向消息、未读列表、红点或消息收件箱。
- Skill 审核、任务、收益、回收/打款和工单状态从对应业务读模型查询，不通过公告模拟定向通知。

建议状态：

```text
draft → scheduled → published → expired
published → withdrawn
```

### 7.17 内容安全、版权与风控

- 对 Skill 字段、`@Tool` 引用、用户输入、生成结果、精选作品及公开创作过程执行适当安全检查。
- 防止 Prompt Injection、结构化字段越界、Tool 越权、参数注入和敏感数据外泄。
- Tool 使用参数白名单、调用次数、成本、并发和频率上限。
- Provider 下载需要限制大小、类型、重定向和内网访问，防止 SSRF 和恶意文件。
- 处理个人信息、违法违规内容、未成年人风险、版权、商标、肖像、声音及其他权利投诉。
- 风控覆盖账号滥用、Skill 刷量、发布者自买、虚假点赞、支付盗刷、冲正滥用和异常收益回收。
- 管理端查看私有 Prompt、Chat 或公开过程原始来源属于高敏操作，必须最小权限和审计。

### 7.18 管理端与运营支撑

服务端提供以下管理能力的权威读写入口：

- 用户、发布者、实名认证、封禁和注销。
- Skill 审核、发布、暂停、恢复、下架和申诉。
- 公开 Tool、模型、Provider、路由、成本、权限和配额。
- Skill 市场分类、标签、榜单和推荐位。
- 精选作品审核、推荐、标签、点赞风控和下架。
- Agent/Worker 查询、受控取消、重试、恢复和补偿。
- 积分商品、微信/支付宝渠道、支付订单、通知/查单、积分履约、扣费、冲正、收益积分、结算、回收、打款和对账。
- 内容安全、风控、客服工单、公告、功能开关和数据报表。

所有人工处置必须进行资源权限校验，记录原因和审计，并通过领域状态机执行，不得直接修改不可变账本或伪造终态。

## 8. 服务职责边界

### 8.1 业务服务

负责：

- 用户、发布者、管理员身份与权限。
- Project、用户资源归属和用户可见 Session 注册信息。
- Skill 市场元数据、发布审核、启用绑定、平台计费和收益规则。
- Skill 草稿、当前发布快照和内部 publication revision；Storyboard、Revision、Element/Slot、Asset、Binding 及其领域事件。
- Graph Tool 用户目录、Definition 发布状态、可见范围和平台治理元数据；具体跨 Module 写入所有权在详细设计中确定。
- 公开 Tool 目录、权限与价格策略。
- 资产元数据与用户资产视图。
- 积分商品、微信/支付宝渠道配置、支付订单、通知、查单、积分履约、消费积分、发布者收益积分、结算、回收和打款。
- 精选作品、公开快照、点赞和全局公告。
- 管理端、客服、内容安全、风控和审计事实。

### 8.2 Agent 服务

负责：

- 维护 Agent Runtime Session、Input、Run、Turn、Checkpoint、Receipt、Approval 和 EventLog 权威状态，并运行 Eino Agent。
- 装载已启用 Skill 的不可变运行快照，理解调用规则并选择实际 Skill。
- Session 创建时冻结当前发布 Skill 快照，新发布只影响新 Session。
- 装载六个已发布 Graph Tool 的冻结 Registry 快照，执行用户显式选择或 Agent 选择的 Graph Tool。
- 建立 Agent Run/Turn、Skill Invocation 和 Graph Tool 的因果关联。
- 执行有界规划、派发和必要的人机确认。
- 产生 Chat/A2UI 领域输出和可靠投影事件。
- 消费 Business/Worker 事件到 Agent Inbox，并将 Worker terminal event 转换为 Durable BatchContinuationResult。
- 支持取消、重试、故障转移、检查点和恢复。

不负责：

- 直接修改消费积分或发布者收益积分；所有扣费、冲正和收益写入必须调用 Business 权威能力。
- 信任模型提供的用户、价格、权限或 Tool。
- 直接持有 Provider 密钥或执行长耗时媒体生成。
- 直接拥有或写入 Storyboard、Binding、Asset 和积分业务表。

### 8.3 AIGC Worker

负责：

- Operation/Batch/Job 的长耗时执行。
- Provider Submit/Poll/Cancel、重试、租约和并发控制。
- TOS 上传、媒体校验、Provider/上传/Finalize receipt，以及调用 Business `FinalizeGeneration` 保存 Asset metadata 和可选 Binding。
- Batch Barrier 和可靠 Worker terminal Outbox。

不负责：

- Skill 选择和下一 Agent 阶段决策。
- 用户 Chat/A2UI 生成或直接调用 Runner。
- Skill/模型定价、发布者收益积分规则或管理用户钱包。
- Storyboard/Binding/Asset/积分所有权、退款、A2UI 投影或 Session Lane 决策。

### 8.4 平台基础能力

负责：

- 业务真源数据库。
- Outbox/Inbox、可靠队列唤醒和恢复调度。
- EventLog、SSE 和游标回放。
- 私有对象存储和短期授权访问。
- 密钥管理、日志、指标、追踪、告警、备份与数据保留。

### 8.5 详细设计阶段门禁

本需求只确定业务职责，不提前固定具体表、Migration Owner、RPC 方法、Event Schema 或 Job Payload。开始对应功能实现前，详细设计必须补齐跨 Module 所有权、唯一写入方、读取方、事务/最终一致性、幂等键、版本兼容和回滚矩阵；支付还必须补齐渠道 Adapter、证书/公钥轮换、通知 Inbox、主动查单、关单、积分履约与渠道账单对账契约。

在设计评审完成前，任何 Module 不得默认直连其他 Module 数据库、跨 Schema JOIN、复用其他 Module 的 `internal` 包或复制其 GORM Model；Agent-facing Graph Tool 还必须先完成独立中文设计文档、流程图、Node 清单、Graph State 和业务状态机。

## 9. 可靠事件、一致性与幂等

- 业务状态与待发布领域事件必须在同一事务或等价原子边界中提交。
- 跨服务按至少一次投递设计，消费者使用 Inbox、稳定幂等标识和语义指纹去重。
- Project 创建、首消息、支付订单、渠道 Notification/Query、积分履约、Skill 启用、Skill Invocation、可计费执行扣费、Graph Tool 费用汇总、账务冲正、收益入账/回收/冲正、点赞、Storyboard Command、Job/Batch、Asset Binding 和公告回执分别具有明确幂等边界。
- 相同幂等标识但业务语义不同必须返回冲突，不能复用第一次结果覆盖新语义。
- SSE 只从持久化 EventLog 分配单调序号；队列、进程内 Channel 和通知不能充当事件真源。
- 读模型允许最终一致，但支付订单、积分履约、余额、充值/扣费/冲正账本、Graph Tool 费用汇总、发布者收益积分、Skill 发布快照、Tool 版本和权限快照必须可强一致验证与审计。
- 技术重试不能产生重复 Project、首消息、支付确认、充值积分、Provider 副作用、Asset、点赞、扣费、收益或公告展示回执。

关键事件族包括但不限于：

```text
project.created
session.input_accepted
payment.order_created / pending / paid / closed / payment_failed / reconciling / provider_exception
payment.notification_received / verified / rejected / applied
points.fulfillment_started / fulfilled / failed
skill.enabled / disabled
skill.invocation_started / succeeded / partial_failed / failed / cancelled
graph_tool.accepted / completed / partial_failed / failed / cancelled
billing.charge_succeeded / reversed / cost_summary_completed
publisher.earning_pending / available / frozen / recovered / reversed
generation.operation_accepted
generation.job_succeeded / dead / cancelled / reconciling
generation.batch_completed / partial_failed / failed / cancelled
asset.available / bound / superseded
featured_work.submitted / published / suspended / offline / liked / unliked
announcement.published / withdrawn / displayed / dismissed
```

## 10. 需求级状态机

| 对象/状态轴 | 权威状态 |
| --- | --- |
| Project 生命周期 | `active → archived/trash → active/deleted` |
| Project 最近运行摘要 | `idle/queued/running/waiting_user/waiting_async/succeeded/partial_failed/failed/cancelled` |
| Skill 内容 | `draft / published`，可同时存在草稿与当前发布快照；治理状态单独记录 `reviewing/rejected/suspended/offline` |
| Skill Invocation 执行 | `created → running → waiting_user/waiting_async → running/succeeded/partial_failed/failed/cancelled` |
| Skill Invocation 账务 | `uncharged → charging → charged/partially_charged`；仅账务差错可进入 `reversed` |
| Agent Run/Turn | `queued → running → waiting_user/waiting_async → running/succeeded/failed/cancelled` |
| Graph Tool Definition | `draft → testing → published → suspended/offline → deprecated`；`suspended → published/offline` |
| Graph Tool | `created → running → waiting_user/accepted/completed/partial_failed/failed/cancelled` |
| Approval | `pending → approved/rejected/expired/cancelled` |
| Generation Job | `pending/retry_wait → running → succeeded/dead/cancelled/reconciling`；`reconciling → succeeded/retry_wait/dead` |
| Generation Batch | `waiting_jobs → finalizing/reconciling/cancelling → completed/partial_failed/failed/cancelled` |
| Featured Work | `draft → submitted → reviewing → published/rejected`；`published ↔ suspended → offline` |
| Announcement | `draft → scheduled/published → expired/withdrawn` |
| Payment Order | `created → pending_payment/reconciling`；`pending_payment/reconciling → paid/closed/payment_failed/provider_exception`；仅强制异常允许 `paid → provider_exception` |
| Points Fulfillment | `unfulfilled → fulfilling → fulfilled/fulfillment_failed`；`fulfillment_failed → fulfilling` |
| Publisher Earnings | `pending → available/frozen/reversed`；`available → frozen/recovered/reversed`；`frozen → available/reversed` |
| Earnings Recovery | `submitted → risk_review → finance_review → paying → completed/rejected/pay_failed` |

用户端和管理端中文状态必须使用[共通状态投影](common-requirements-baseline.md#42-权威状态与用户投影)。执行状态与账务状态必须分离，避免把“运行失败”解释为“未扣费”或把“已扣费”解释为“运行成功”。终态记录不得原地重置；所有非法迁移必须被拒绝，并记录触发者、原因、版本和审计信息。

## 11. 可观测性与运营指标

### 11.1 全链路追踪

支持从以下任一对象查询完整因果链：

```text
User / Project / Session
→ Agent Run / Turn
→ Skill Invocation
→ Graph Tool / Public Tool Run
→ Operation / Batch / Job
→ Provider / Asset
→ Points Charge / Cost Summary / Reversal / Publisher Earnings

User → Payment Order → Channel Attempt / Notification / Query
→ Points Fulfillment → Recharge Ledger / Points Account
```

### 11.2 关键指标

- 快速创建成功率、重复创建拦截数、首 Turn 延迟。
- Session 积压、Agent 成功率、取消、重试、Failover 和等待用户时长。
- Skill 选择分布、实际调用数、逐项扣费、Graph Tool 汇总、重复扣费拦截，以及仅供审计的 Session Snapshot digest/内部 publication revision 使用分布。
- Provider 成本、错误率、Poll 时长、重试、Reconciling、Dead、隔离资产和 Batch 完成率。
- SSE 延迟、断线续传、事件积压和投影失败。
- 积分扣费差异、汇总差异、账务冲正、收益回收/冲正和对账不平。
- 分渠道支付创建/成功率、通知验签失败、主动查单恢复、未知结果、积分履约延迟/失败和支付对账差异。
- 精选作品提交、审核、曝光、点赞和异常点赞。
- 公告曝光、关闭、版本频控和撤回生效延迟。

用户可见错误必须脱敏并提供稳定错误码；内部保留可定位的 Trace、阶段和分类，但不得记录密钥、完整 Provider Payload 或私有内容。

## 12. 核心验收标准

以下用例继承[共通可执行验收规范与 Evidence 要求](common-requirements-baseline.md#7-可执行验收规范)，缺少规定证据时不得判定通过。

| ID | Given / When | Then |
| --- | --- | --- |
| SRV-CREATE-001 | Given 同一快速创建语义和请求标识；When 并发提交 100 次 | 只产生一个 Project、Session、首消息和 Agent Input，全部成功响应返回同一 `project_id`。 |
| SRV-CREATE-002 | Given 提示词为空或纯空白；When 创建 Project | 只创建 Project、Session 和空工作台，不产生消息、Turn、Invocation 或扣费。 |
| SRV-EVENT-001 | Given 业务提交成功但 Redis 唤醒失败；When PostgreSQL 恢复扫描运行 | 输入在 30 秒 P95 内重新进入处理，不丢失、不重复创建 Turn。 |
| SRV-SKILL-001 | Given Project 启用多个 Skill；When Agent 实际选择 0、1 或多个 Skill | 只为实际选择创建唯一 Invocation；启用、加载和规则读取不扣费。 |
| SRV-SKILL-002 | Given Skill 已发布且已有 Session；When 修改草稿并再次发布 | 已有 Session 始终使用原 Snapshot，新 Session 使用新发布快照；产品不出现版本明细，历史事实不被改写。 |
| SRV-SKILL-003 | Given Skill 具有 current published snapshot；When 匿名列表或详情读取 | 只有治理 active 且发布指针、逻辑关联、Canonical 与 digest 完整一致时返回白名单公开 DTO；暂停/下架统一不可见，损坏事实失败关闭且不泄漏草稿、运行 guidance 或内部治理字段。 |
| SRV-GTL-001 | Given 生产 Registry 启动或热加载；When 校验 Graph Tool Definition | 六个必备 `tool_key` 各有可执行已发布版本，Schema、权限、预算和计费引用完整；任一缺失或同一用户灰度解析不唯一时失败关闭并告警。 |
| SRV-GTL-002 | Given 用户显式选择 `write_prompts`；When Agent 处理输入 | 冻结所选 Tool 和版本，只允许补问或调用 `write_prompts`，不得调用媒体生成替代；Run 与事件可追踪。 |
| SRV-GTL-003 | Given 用户无 Storyboard；When 独立调用媒体生成或视频剪辑 | 创建平台原生 Graph Tool Run 和独立结果，不创建虚假 Skill Invocation，也不覆盖 Active Storyboard。 |
| SRV-BILL-001 | Given 余额充足且计费版本已冻结；When 可计费执行正式开始 | Business 原子扣费成功后才调用下游，账本关联完整因果链；Graph Tool 终态只汇总已有明细。 |
| SRV-BILL-002 | Given 执行已扣费；When 失败、部分成功、取消、超时或结果被替代 | 运行进入对应状态，已发生扣费不退款；失败或取消子项不生成收益，已成功合格子项正常结算，只有账务差错可产生唯一冲正。 |
| SRV-BILL-003 | Given 同一模型/Tool/Job 幂等键发生重试、恢复或重复事件；When 最终完成 | 只产生一笔正常扣费和一份收益归属；用户主动重新生成产生新记录。 |
| SRV-PAY-001 | Given 微信或支付宝通知签名有效且订单、商户身份、币种、金额和交易状态匹配；When 通知并发重放 100 次 | 只确认一次支付并只履约一笔冻结积分，Notification、渠道交易号和充值账本可追踪。 |
| SRV-PAY-002 | Given 支付通知签名错误、金额不符、渠道交易号冲突或客户端伪造成功；When 请求到达 | 不改变支付订单和积分账户，记录脱敏拒绝原因并按阈值告警。 |
| SRV-PAY-003 | Given 渠道下单超时、通知丢失或支付结果未知；When 扫描恢复 | 使用原商户订单号主动查单，5 分钟 P95 内恢复权威结果，不盲目创建第二笔渠道订单。 |
| SRV-PAY-004 | Given 已确认支付后进程在积分入账前后任一崩溃点退出；When Outbox/Inbox 或定时扫描恢复 | 按冻结商品快照补齐履约且最终只有一笔正常充值积分，用户无需再次支付。 |
| SRV-EARN-001 | Given 有效 Skill Invocation 产生合格收益；When 结算和五折回收 | 收益按冻结规则进入待结算/可用，按 50% 计算回收基数，余数保留且重复打款被拦截。 |
| SRV-AGT-001 | Given 同一 Session 有并发输入；When 多实例 Claim | 只有有效 Lease/Fence Owner 处理，按输入顺序串行，不混入后续 Turn 消息。 |
| SRV-AGT-002 | Given Model/Tool Receipt 已冻结；When 投影失败、进程重启或 Resume | 重放原输出和扣费结果，不再次调用模型、Graph Tool 或外部副作用。 |
| SRV-AGT-003 | Given Approval 待确认；When 重复决定、过期决定或资源版本变化 | 仅首个合法且版本匹配的正式决定生效，自然语言确认不能绕过 Approval。 |
| SRV-AGT-004 | Given 异步 Graph Tool 已返回 `accepted`；When 原 Agent Run 结束 | Operation/Batch/Job 继续由持久化状态推进，终态以可信 Continuation 回流，不恢复跨分钟 Graph 栈。 |
| SRV-WRK-001 | Given 同一 Job 被重复唤醒且 Worker 竞争；When 执行并提交终态 | 只有匹配 Lease Owner/Version 的提交生效，Provider、TOS 上传和 Business Finalize 最多一次；Worker 不调用扣费/退款。 |
| SRV-WRK-002 | Given Provider 结果未知；When Worker 恢复 | 进入 `reconciling` 并按原幂等键查询，不盲目重跑；无法自动恢复时进入 `dead`。 |
| SRV-WRK-003 | Given 用户编辑导致旧 Job 目标版本过期；When 旧结果回流 | 结果只能隔离或标记 superseded，不能覆盖新故事板、Prompt 或 Active Asset；已开始执行的正常扣费不退款。 |
| SRV-WRK-004 | Given Batch 已进入任一终态且 terminal event 重复投递；When Agent Inbox 消费 | 只创建一个 `(batch_id,result_version)` Continuation 并唤醒一次 Session Lane，不直接恢复旧 Graph。 |
| SRV-A2UI-001 | Given 服务端发布 Card、输入、媒体、步骤、Tool 或 Status 组件；When 前端实时接收或历史回放 | 已知组件按同一 Registry 渲染，Markdown 被安全清洗；未知版本/组件/Action 不执行交互并安全降级或失败关闭。 |
| SRV-READ-001 | Given 工作台已有持久化事件；When 刷新或 SSE 断线重连 | 3 秒 P95 内回放缺口并恢复 Chat/A2UI、故事板、资产和高层运行状态。 |
| SRV-PUBLIC-001 | Given 作者提交精选作品；When 审核发布 | 公开快照只能包含确认内容和实际使用 Skill，私有 Chat、隐藏指令、内部 Tool 参数和私有 Asset 不可访问。 |
| SRV-LIKE-001 | Given 同一用户对同一作品并发操作 100 次；When 点赞或取消 | 最多一个有效点赞关系，计数可从关系真源修复，系统不存在 Comment 对象。 |
| SRV-ADMIN-001 | Given 管理员执行取消、恢复、冲正、调账、下架或收益回收；When 请求被重放 | 权限、状态机、幂等和双人复核按规则生效，不重复产生财务或业务副作用，审计完整。 |

## 13. 非功能要求

服务端统一遵循[共通 v1 量化非功能基线](common-requirements-baseline.md#6-v1-量化非功能基线)，并满足：

| 维度 | 服务端验收要求 |
| --- | --- |
| 可用性 | 核心 API 月可用性 `≥99.9%`，支付、积分履约、扣费、收益账本和任务受理接口 `≥99.95%`。 |
| 延迟 | 不含外部 Provider 的读接口 `P95≤500ms`、写接口 `P95≤800ms`、Project 创建 `P95≤1s`、首 SSE 状态 `P95≤2s`。 |
| 恢复 | Redis 丢失唤醒时 Agent Input/Job 在 `P95≤30s` 恢复；PostgreSQL 权威数据 `RPO≤5min`、核心服务 `RTO≤30min`。 |
| 幂等 | 相同业务幂等键并发/重放 100 次，仅产生一个对应业务事实；支付场景至多一次支付确认和一笔正常充值积分，消费场景至多一次正常扣费和一份收益归属。 |
| 数据流 | SSE 缺口回放 `P95≤3s`；EventLog 按 Session 单调递增并支持至少 30 天在线回放。 |
| 生命周期 | Agent/Worker 收到退出信号后 5 秒内停止接收或 Claim，新旧 Owner 通过 Lease/Fence 安全接管；Drain 最长默认 60 秒。 |
| 财务 | 每日对账覆盖 100% 渠道支付、积分履约、扣费、冲正、收益和回收记录；不平在 5 分钟内告警并阻止重复履约或结算。 |
| 数据保护 | 普通日志 30 天、Trace 7 天、账务和审计至少 7 年；Secret、完整 Prompt、Tool/Provider Payload、Checkpoint 和 Reasoning 不进入普通日志/Trace。 |
| 容量 | 上线压测不低于批准峰值预测的 2 倍；Agent Lane、Graph、Worker Pool、Claim Batch、模型和 Provider 并发全部有界。 |
| 兼容性 | 已发布 DTO/Event/Job Payload/Skill Snapshot/Tool/Prompt 字段不得复用或静默改义；滚动发布期间新旧协议均可处理存量权威状态。 |

## 14. 与现有三份 AIGC 文档的关系

### 14.1 继承的设计原则

| 现有文档 | 生产需求继续采用的领域原则 |
| --- | --- |
| [AIGC ChatModelAgent Demo 详细设计](../aigc-chatmodelagent-demo-design.md) | 持久化 Session 输入、Runner、Chat/A2UI、Graph Tool、Interrupt/Resume、因果回执和事件续传。 |
| [AIGC Tool 编排与动态故事板详细设计](../aigc-tool-storyboard-design.md) | 动态 Storyboard Revision、稳定 Target、Prompt 局部编辑、候选资产、审核、依赖失效和迟到结果保护。 |
| [AIGC Generation Worker 详细设计](../aigc-worker-design.md) | 异步 Provider、TOS、Business Finalize、Batch Barrier、terminal Outbox、幂等、重试、取消和已扣积分引用。 |
| [AIGC A2UI 全栈详细设计](../aigc-a2ui-design.md) | Card 基类、交互组件、后端/前端独立包、协议、Inbox/Projector、SSE、安全和验收。 |

### 14.2 生产版新增或重做

- 真实用户认证、个人发布者、管理员 RBAC 和资源级授权。
- Project 模型与可选首提示词的幂等快速创建。
- 结构化多 Skill、`@Tool` 引用和实际 Skill Invocation。
- 六个用户可发现、可显式选择的 Graph Tool 目录与运行入口；新增独立 `write_prompts`，并把 `assemble_output` 补齐为生产级视频剪辑。
- Skill 市场、创建、测试、审核、草稿/发布和 Dashboard。
- 微信 Native、支付宝电脑网站支付、积分商品、服务端通知/查单、唯一积分履约和渠道对账。
- 按平台模型/能力配置对实际可计费执行直接扣费、Graph Tool 终态汇总、无产品退款、账务冲正、发布者收益积分和五折回收。
- 资产中心、私有对象存储和用户数据隔离。
- 精选作品、公开过程快照、点赞和无评论边界。
- 平台公告全局弹窗和展示频控。
- 内容安全、版权、风控、管理端和生产可观测性。

### 14.3 不进入生产版的 Demo 假设

- 受信本地身份、固定 Demo 用户和简单管理 Token。
- 用户资产长期公开 URL。
- 占位媒体 Provider 作为正式生产能力。
- 纯进程内状态或队列作为业务真源。
- 生成完成或交付后才统一后付费、由 Agent/Worker 自行决定价格、失败自动退款等 Demo 计费假设；生产版在可计费执行开始前由 Business 原子扣费。
- 用户直接接触 Provider、内部 Tool、完整 Payload 或内部错误日志。

三份设计文档已在 2026-07-14 统一为 Business 生成前直接扣费、无退款、Worker 不计费的目标合同；其中标为“迁移前实现”的 Reservation/Refund/Compensation 描述仅用于识别待删除代码，不得作为生产验收依据。

现有设计固定五个 Capability、把 Prompt 生成隐藏在故事板内部、素材只分析 metadata、媒体生成仅推进 Storyboard 或 Assembly 只生成 manifest/占位结果时，同样不能作为六个 Graph Tool 已完成的证据；差距以[Graph Tool 功能需求总览](graph-tool-requirements-overview.md#11-当前设计与目标需求差距)为准。

## 15. 待确认参数

以下参数不改变本文已确认的计费、无退款、五折回收和状态规则，但必须在详细产品规则和技术设计前确定：

1. Skill 各结构化字段的必填矩阵、长度限制和审核规则。
2. 公开 Tool 的版本兼容策略，以及 Tool 暂停后运行中 Skill 的处置方式。
3. 模型、媒体、公开 Tool 和 Graph Tool 的具体积分配置、计费单位、配置版本发布和灰度生效规则。
4. 发布者收益积分生成公式、结算周期、最低回收积分、积分兑付币种/法币面值、税费、打款渠道和收益转消费积分比例；五折回收系数已经确认。
5. 单个 Turn 最大 Skill/Graph Tool 调用数、Project 最大启用 Skill 数、模型/Tool/Token/时间/异步任务硬预算及默认积分上限。
6. 详细设计阶段的跨 Module Migration Owner、RPC/Event/Job Payload、幂等键和兼容升级矩阵。
7. 精选作品匿名访问范围、公开过程字段和审核时限。
8. 公告默认展示频率、连续弹窗上限和紧急公告关闭规则。
9. 首期正式支持的模型、图片、视频、音频、剪辑和装配 Provider 清单。
10. 六个 Graph Tool 的输入上限、同步转异步阈值、模型/Provider 路由、独立模式 DTO 和用户表单；六个工具的必备范围及稳定 `tool_key` 不再作为待定项。
11. 首批积分商品、订单有效期、充值限额、实名/风控阈值、发票规则、商户签约参数和支付机构强制异常处置；微信支付和支付宝支付的必备范围不再作为待定项。
