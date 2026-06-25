# AG-UI 与 A2UI 交互契约产品系统设计

状态：draft  
owner：产品体验设计师  
更新时间：2026-06-25  
适用范围：统一 Agent 到前端的组件样式、组件作用、事件类型、事件顺序、payload 和交互契约  
product_status：Draft

## 关联文档

- [统一 Agent 产品系统设计](./统一Agent产品系统设计.md)
- [Tool 边界产品系统设计](./Tool边界产品系统设计.md)
- [积分扣费产品系统设计](./积分扣费产品系统设计.md)
- [模型选择产品系统设计](./模型选择产品系统设计.md)
- [资产与创作过程保存产品系统设计](./资产与创作过程保存产品系统设计.md)
- [AG-UI事件规范](../standards/AG-UI事件规范.md)
- [AG-UI事件协议模板](../templates/AG-UI事件协议模板.md)

## 背景

统一 Agent 需要把对话、Tool 调用、生成进度、人工确认、积分预估、产物结果和错误状态展示给前端。产品已确认：AG-UI / A2UI 需要单独设计，不只定义事件类型，还要定义组件样式、组件作用和交互契约。

本文件记录产品侧设计范围，并给出第一版事件类型、顺序、payload、幂等、断线重连和兼容策略的产品口径。正式工程字段后续由 AG-UI 契约文档承接。

## 目标

- 定义 Agent 输出到前端需要哪些聊天区域组件和聊天外部组件。
- 定义每类组件的作用、展示状态和交互行为。
- 定义 AG-UI / A2UI 事件类型、顺序和 payload 的设计范围。
- 定义人工确认、Tool 状态、任务进度、产物展示和错误状态的交互边界。
- 确保前端不自行发明 Agent 事件字段。

## 设计原则

以下为第一版建议稿，待产品确认后再进入正式 AG-UI 契约设计。

- A2UI 组件服务于创作工作台，不做营销型展示。
- A2UI 组件样式必须符合站点主题样式，不单独创建独立视觉风格。
- A2UI 组件的颜色、字体、圆角、间距、阴影、密度、图标和状态样式必须继承站点设计系统。
- A2UI 组件分为聊天区域组件和聊天外部组件。
- 模型选择不作为独立 A2UI 组件，模型在用户聊天输入框内选择。
- 聊天外部组件只有一个外部工作区组件，用于承载资产视图或黑板视图。
- A2UI 需要支持思考状态的打印机输出，用于展示可公开的 Agent 处理状态。
- A2UI 需要支持平台能力 Tag 展示，用于展示 Skill、Tool 等平台内置能力。
- 组件应紧凑、可扫描，适合在对话流中反复出现。
- 组件状态必须稳定，不因进度、按钮、长模型名称或长文件名导致布局跳动。
- 生成类任务必须清楚展示模型名称、预计积分、确认状态、进度和产物结果。
- 提示词安全评估不通过时必须展示可理解阻止原因，并阻止进入积分冻结和生成执行。
- 高风险 Tool、扣费和业务写入必须用确认组件承接。
- 前端只能依赖已文档化事件，不把 Eino 内部事件当作前端业务字段。
- 未识别事件必须兼容忽略，并保留可排障日志。

## A2UI 组件范围

第一阶段组件分为聊天区域组件和聊天外部组件。

A2UI 组件只定义在统一 Agent 创作工作台中的结构、状态和交互职责。具体视觉表现必须跟随站点主题样式和后续前端设计系统。

### 聊天区域组件

聊天区域组件出现在对话流、聊天输入框或对话内确认区域。

| 组件 key | 组件 | 作用 | 关键状态 | 主要动作 |
| --- | --- | --- | --- | --- |
| `message.stream` | 消息流组件 | 展示 Agent 文本回复、增量输出和完成状态 | streaming、completed、failed | 停止生成、复制 |
| `thinking.typewriter` | 思考状态打印机组件 | 以打印机效果展示可公开的 Agent 处理状态 | typing、completed、collapsed、hidden | 折叠、展开 |
| `platform.tags` | 平台能力 Tag 组件 | 展示 Skill、Tool 等平台内置能力标签 | visible、active、disabled、hidden | 查看说明 |
| `credit.estimate` | 积分预估组件 | 展示预计消耗积分、可用积分和即将过期积分 | estimating、ready、insufficient、expired_notice | 查看明细 |
| `confirmation.panel` | 人工确认组件 | 展示扣费确认、高风险 Tool 确认和业务写入确认 | required、accepted、rejected、expired | 确认、取消 |
| `tool.status` | Tool 状态组件 | 展示 Tool 调用中、成功、失败、取消、超时 | running、succeeded、failed、cancelled、timeout | 重试、查看详情 |
| `generation.progress` | 生成任务进度组件 | 展示图片、音乐、视频生成任务进度 | queued、running、partial_completed、completed、failed、cancelled | 取消、查看已完成 |
| `error.notice` | 错误提示组件 | 展示用户错误、权限错误、Tool 错误、模型错误、RPC 错误和系统错误 | user_error、permission_denied、tool_error、model_error、rpc_error、system_error | 重试、返回修改 |
| `chat.input.controls` | 聊天输入控件组件 | 在聊天输入框内承载模型选择、单选、多选、输入框等控件 | idle、editing、locked、invalid | 选择、填写、提交 |

### 聊天输入控件类型

| 控件类型 | 用途 | 示例 |
| --- | --- | --- |
| 模型选择 | 在聊天输入框内选择图片、音乐、视频生成模型 | 当前对话使用哪个图片生成模型 |
| 单选 | 从互斥选项中选择一个 | 横版 / 竖版视频 |
| 多选 | 从多个选项中选择一个或多个 | 需要生成封面、脚本、BGM |
| 输入框 | 补充文本、提示词、商品卖点、脚本要求 | 商品标题、目标人群、视频时长 |
| 文件 / 素材选择 | 选择上传文件或已有资产作为输入 | 参考图、已有音乐、商品图片 |

### 平台能力 Tag 类型

平台能力 Tag 是可嵌入的 A2UI 基础展示能力，可出现在消息流、思考状态、Tool 状态、生成进度、黑板视图和资产视图中。

| Tag 类型 | 用途 | 示例 |
| --- | --- | --- |
| Skill Tag | 展示当前选择或正在执行的 Skill | Skill：MV 制作 |
| Tool Tag | 展示当前调用或即将调用的平台开放 Tool | Tool：图片生成 |
| Model Tag | 展示当前对话选择的生成模型名称 | 模型：图像生成 A |
| Risk Tag | 展示 Tool 或操作风险等级 | 高风险、需确认 |
| Status Tag | 展示平台能力当前状态 | 运行中、已完成、不可用 |

### 聊天外部组件

聊天外部组件第一版只有一个：外部工作区组件。外部工作区可以承载资产视图或黑板视图，但前端结构上仍是一个外部组件。

| 组件 key | 组件 | 作用 | 关键状态 | 主要动作 |
| --- | --- | --- | --- | --- |
| `workspace.panel` | 外部工作区组件 | 展示聊天外部的资产视图或黑板视图 | empty、assets、blackboard、updating、error | 预览、切换视图、选择素材、引用素材 |

外部工作区视图：

- 资产视图：展示音乐、图片、视频等素材和生成产物。
- 黑板视图：展示 Skill 产出的资产元素草稿、素材、提示词、脚本和故事线。
- 视频类 Skill 的黑板视图按分镜展示图片、脚本、提示词和生成状态。
- 外部工作区按资产元素类型渲染内容，不按音乐、MV、电商图等具体场景硬编码字段。

## 组件交互规则

- 模型选择只对当前对话生效，并在用户聊天输入框内完成。
- 思考状态打印机输出只展示可公开的处理状态，例如分析需求、整理素材、生成分镜、调用工具、等待模型返回。
- 思考状态打印机输出不得展示模型内部推理链路、系统提示词、密钥、平台内部配置或安全策略细节。
- 思考状态打印机输出可在任务完成后折叠，保留用户可理解的状态摘要。
- 平台能力 Tag 只展示可公开的展示名称、类型、状态和风险提示。
- 平台能力 Tag 不展示内部 ID、供应商、API Key、endpoint、内部模型标识、内部成本或平台配置细节。
- Skill、Tool 等平台能力 Tag 必须受当前空间、权限、平台白名单和可见范围约束。
- 无权限、未开放或不应暴露的平台能力不得以 Tag 形式展示给普通用户。
- 进入扣费确认后，聊天输入控件进入 `locked` 状态，不允许直接修改模型或生成参数。
- 用户需要修改模型时，必须取消当前确认并重新发起预估。
- 积分不足时，不进入确认和生成执行。
- 用户确认扣费后，先冻结积分，再进入生成任务。
- 用户取消生成后，不再发起新的 Tool 调用；已完成产物保留并扣费，未完成部分释放冻结积分。
- 高风险 Tool 和业务写入确认必须展示风险说明、影响范围和确认动作。
- 外部工作区的资产视图展示生成音乐、图片、视频等素材。
- 外部工作区的黑板视图按 Skill 故事线和输出元素结构展示素材、提示词、脚本、分镜和其他资产元素草稿。
- 第一版产物详情不提供独立“继续创作”快捷入口；继续创作只在会话中通过引用已创作资产完成，例如基于图片做视频、基于歌词生成歌曲、基于商品图生成广告视频。

## AG-UI 事件建议清单

正式事件命名、字段和兼容策略后续进入 AG-UI 契约文档。第一版产品建议如下：

| 事件类型 | 触发时机 | 前端消费组件 |
| --- | --- | --- |
| `agent.run.started` | Agent run 开始 | `message.stream` |
| `agent.thinking.started` | 可公开思考状态开始输出 | `thinking.typewriter` |
| `agent.thinking.delta` | 可公开思考状态增量输出 | `thinking.typewriter` |
| `agent.thinking.completed` | 可公开思考状态输出完成 | `thinking.typewriter` |
| `agent.message.delta` | Agent 文本增量输出 | `message.stream` |
| `agent.message.completed` | Agent 文本输出完成 | `message.stream` |
| `agent.skill.selected` | Agent 选择到 Skill | `message.stream` / `platform.tags` |
| `agent.skill.missing` | 没有匹配 Skill，使用文本模型对话或直接 Tool 意图判断 | `message.stream` |
| `platform.tags.updated` | Skill、Tool、模型、风险或状态 Tag 发生变化 | `platform.tags` |
| `chat.controls.requested` | 需要在聊天输入框展示单选、多选、输入框、素材选择或模型选择 | `chat.input.controls` |
| `chat.controls.locked` | 进入扣费确认后锁定聊天输入控件 | `chat.input.controls` |
| `safety.prompt.evaluated` | LLM 提示词安全评估完成 | `message.stream` / `error.notice` |
| `safety.prompt.blocked` | 提示词安全评估不通过 | `error.notice` |
| `workspace.assets.updated` | 资产视图中的音乐、图片、视频或资产元素发生变化 | `workspace.panel` |
| `workspace.blackboard.updated` | 黑板视图中的故事线、资产元素草稿、分镜、脚本、提示词或素材发生变化 | `workspace.panel` |
| `credits.estimated` | 积分预估完成 | `credit.estimate` |
| `credits.freeze.requested` | 用户确认后请求冻结积分 | `credit.estimate` |
| `credits.frozen` | 积分冻结成功 | `credit.estimate` |
| `credits.charged` | 已保存可用资产确认扣费 | `credit.estimate` |
| `credits.released` | 未完成部分释放冻结积分 | `credit.estimate` |
| `asset.save.started` | 资产开始保存 | `workspace.panel` |
| `asset.save.completed` | 资产保存成功并可展示 | `workspace.panel` |
| `asset.save.failed` | 资产保存失败 | `error.notice` / `workspace.panel` |
| `process.snapshot.saved` | 创作过程快照保存成功 | `message.stream` / `workspace.panel` |
| `confirmation.required` | 需要用户确认 | `confirmation.panel` |
| `confirmation.accepted` | 用户确认 | `confirmation.panel` |
| `confirmation.rejected` | 用户拒绝或取消 | `confirmation.panel` |
| `tool.call.started` | Tool 调用开始 | `tool.status` / `platform.tags` |
| `tool.call.progress` | Tool 调用进度变化 | `tool.status` / `platform.tags` |
| `tool.call.completed` | Tool 调用成功 | `tool.status` / `platform.tags` |
| `tool.call.failed` | Tool 调用失败 | `tool.status` / `error.notice` |
| `generation.progress` | 生成任务进度变化 | `generation.progress` |
| `generation.artifact.completed` | 单个产物完成 | `workspace.panel` |
| `agent.run.completed` | Agent run 成功完成 | `message.stream` / `workspace.panel` |
| `agent.run.failed` | Agent run 失败 | `error.notice` |
| `agent.run.cancelled` | Agent run 被取消 | `generation.progress` / `message.stream` |

## 公共 Payload 设计

每个事件包含以下公共字段，最终工程字段以 AG-UI 契约文档为准：

| 字段 | 说明 |
| --- | --- |
| `event_id` | 事件唯一标识，用于幂等 |
| `event_type` | 事件类型 |
| `sequence` | 同一 run 内单调递增序号 |
| `timestamp` | 事件产生时间 |
| `session_id` | 会话标识 |
| `run_id` | Agent run 标识 |
| `space_id` | 当前个人空间或企业空间标识 |
| `actor_user_id` | 当前操作用户标识 |
| `component` | 建议前端消费组件 key |
| `payload` | 事件业务载荷 |
| `trace_id` | 排障链路标识 |

事件业务载荷按事件组表达：

| 事件组 | 关键 payload | 说明 |
| --- | --- | --- |
| Agent 运行 | `run_status`、`started_at`、`completed_at`、`error_code`、`user_message`、`retryable` | 驱动运行开始、完成、失败和取消状态 |
| 思考状态 | `text_delta`、`display_level`、`visibility`、`collapsed` | 只展示可公开处理状态，`visibility` 固定为 public |
| 消息输出 | `role`、`text_delta`、`content`、`finish_reason` | 支持增量消息和完成消息 |
| Skill 路由 | `skill_name`、`skill_scope`、`matched`、`fallback_reason` | 只展示可公开 Skill 名称和路由结果 |
| 平台能力 Tag | `tags[]`，包含 `type`、`label`、`status`、`risk_level`、`visible` | 用于 Skill、Tool、Model、Risk、Status 展示 |
| 聊天输入控件 | `controls[]`，包含 `control_id`、`type`、`label`、`options`、`default_value`、`required`、`validation`、`locked_reason` | 支持模型选择、单选、多选、输入框、素材选择 |
| 提示词安全评估 | `safety_status`、`checked_target`、`user_message`、`retryable` | 展示通过、阻止、失败或超时，不暴露安全策略细节 |
| 积分 | `estimate_points`、`frozen_points`、`charged_points`、`released_points`、`account_type`、`expires_soon_points` | 展示预估、冻结、扣减、释放和即将过期积分 |
| 人工确认 | `confirmation_id`、`title`、`summary`、`risks[]`、`points`、`expires_at`、`actions[]` | 承接扣费、高风险 Tool 和业务写入确认 |
| Tool 调用 | `tool_name`、`tool_tag`、`status`、`progress`、`retryable`、`error_code`、`user_message` | 展示 Tool 运行、成功、失败、超时和取消 |
| 生成任务 | `resource_type`、`task_id`、`model_name`、`progress`、`status`、`partial_completed` | 支持图片、音乐、视频生成进度 |
| 产物完成 | `artifact_id`、`resource_type`、`name`、`preview_url`、`duration`、`metadata_summary`、`elements_summary` | 只返回前端可展示的产物摘要、元素摘要和引用 |
| 资产保存 | `asset_id`、`artifact_id`、`resource_type`、`save_status`、`preview_url`、`downloadable`、`elements[]`、`user_message` | 展示资产保存中、成功、失败和可展示资产元素 |
| 创作过程保存 | `session_id`、`run_id`、`snapshot_status`、`last_saved_sequence`、`user_message` | 展示创作过程保存和恢复状态 |
| 资产元素 | `element_id`、`element_type`、`label`、`value`、`ref_asset_id`、`required`、`editable`、`render_hint`、`visibility` | 前端按平台内置固定元素类型渲染，不按具体场景硬编码 |
| 外部工作区 | `mode`、`assets[]`、`elements[]`、`storyline[]`、`active_node_id` | `mode` 为 assets 或 blackboard |
| 错误提示 | `error_type`、`error_code`、`user_message`、`retryable`、`support_trace_id` | 用户可理解错误和排障标识 |

生成类事件 payload 至少需要表达：

- 资源类型：图片、音乐、视频。
- 思考状态打印机文本片段、展示级别和折叠状态。
- 平台能力 Tag 列表，例如 Skill、Tool、Model、Risk、Status。
- 平台能力 Tag 的展示名称、类型、状态、风险等级和可见性。
- Skill 输出元素结构和当前可展示资产元素摘要。
- 当前对话选择的模型名称。
- 提示词安全评估状态和用户可见提示。
- 生成参数摘要。
- 聊天输入控件类型、选项、默认值和锁定状态。
- 预计积分、冻结积分、已扣积分或释放积分。
- 任务状态和进度。
- 已完成产物列表。
- 外部工作区展示模式：资产视图或黑板视图。
- 黑板故事线节点，例如分镜、脚本、提示词、参考素材和生成状态。
- 错误类型、错误提示和可重试建议。

## 事件顺序建议

同一 `run_id` 内建议遵守以下顺序：

```text
agent.run.started
  -> agent.thinking.started / agent.thinking.delta / agent.thinking.completed
  -> agent.message.delta / agent.skill.selected / agent.skill.missing
  -> platform.tags.updated
  -> chat.controls.requested
  -> safety.prompt.evaluated / safety.prompt.blocked
  -> credits.estimated
  -> confirmation.required
  -> confirmation.accepted / confirmation.rejected
  -> chat.controls.locked
  -> credits.frozen
  -> tool.call.started
  -> platform.tags.updated
  -> tool.call.progress / generation.progress / workspace.blackboard.updated
  -> generation.artifact.completed
  -> asset.save.started / asset.save.completed / asset.save.failed
  -> workspace.assets.updated
  -> process.snapshot.saved
  -> credits.charged / credits.released
  -> agent.message.completed
  -> agent.run.completed / agent.run.failed / agent.run.cancelled
```

顺序规则：

- 同一 run 内 `sequence` 必须单调递增。
- 前端按 `sequence` 合并和渲染事件。
- `event_id` 必须全局唯一，重复事件前端可忽略。
- 断线重连同时支持 `Last-Event-ID` 和 `run_id + sequence` 补偿。
- 未知事件前端忽略展示，但保留排障日志。

## 承载与断线重连设计

第一版实时事件默认使用 SSE 承载，确认、取消、重试、补偿查询等用户动作通过 HTTP API 承接。WebSocket 不作为第一版必需能力，后续如果需要双向实时协作再扩展。

断线重连规则：

- 前端订阅某个 `run_id` 的事件流。
- SSE 断线后，前端优先携带 `Last-Event-ID` 重新订阅。
- 服务端根据 `Last-Event-ID` 从下一条事件继续推送。
- 如果浏览器或代理没有保留 `Last-Event-ID`，前端使用 `run_id + after_sequence` 通过补偿接口拉取缺失事件。
- 如果事件已经超过可补偿窗口，服务端返回当前 run 快照和不可补偿标识，前端进入 `reconnecting` 后刷新到最新可展示状态。
- 前端按 `event_id` 去重，按 `sequence` 排序合并。
- 如果发现 `sequence` 缺口，前端暂停增量渲染，进入 `reconnecting` 状态并触发补偿。
- 未知事件类型不阻断重连；前端忽略展示并记录排障日志。

## 用户流程

1. 用户在统一 Agent 对话框输入意图。
2. Agent 通过 A2UI 思考状态打印机输出可公开处理状态。
3. Agent 通过 AG-UI / A2UI 输出消息流。
4. 如果 Agent 选择 Skill、调用 Tool 或进入平台能力状态变化，前端展示对应平台能力 Tag。
5. 如果需要模型选择、单选、多选、输入框或素材选择，前端在聊天输入框内展示对应控件。
6. 生成类任务进入积分冻结前，前端接收提示词安全评估状态。
7. 如果提示词安全评估不通过，前端展示 blocked 状态和修改提示，不进入积分预估确认。
8. 如果需要扣费确认，前端在聊天区域展示积分预估和人工确认组件。
9. 如果调用 Tool，前端在聊天区域展示 Tool 状态组件。
10. 如果生成图片、音乐或视频，前端在聊天区域展示任务进度组件。
11. 如果 Skill 产出故事线、分镜、脚本、提示词或中间素材，前端在外部工作区的黑板视图展示。
12. 生成完成后，前端展示资产保存状态。
13. 资产保存成功后，前端在外部工作区的资产视图展示音乐、图片、视频等素材。
14. 创作过程保存成功后，用户后续可从会话历史恢复对话、黑板和资产视图。
15. 失败、取消或超时时，前端展示错误提示或取消状态。

## 业务规则

- AG-UI / A2UI 需要单独设计组件样式、组件作用和交互契约。
- A2UI 组件样式必须符合站点主题样式。
- A2UI 不允许脱离站点主题另起一套视觉风格。
- A2UI 的视觉 token、组件变体和状态样式由后续前端设计系统落地。
- A2UI 组件分为聊天区域组件和聊天外部组件。
- A2UI 需要支持思考状态的打印机输出。
- 思考状态打印机输出只能展示可公开的处理状态和进度摘要，不得展示模型内部推理链路。
- A2UI 需要支持 Skill、Tool 等平台内置能力的 Tag 展示。
- 平台能力 Tag 只能展示可公开信息，不得泄露内部配置、供应商信息、密钥或成本。
- 模型选择不作为独立 A2UI 组件，必须在用户聊天输入框内完成。
- 聊天输入框内支持模型选择、单选、多选、输入框、文件或素材选择等控件。
- 聊天外部组件第一版只有一个外部工作区组件，用于承载资产视图和黑板视图。
- AG-UI / A2UI 事件类型、顺序、payload、断线重连和兼容策略按本文第一版产品口径进入后续正式契约设计。
- 前端只能消费已文档化事件和 payload。
- 提示词安全评估事件只展示用户可理解状态，不暴露安全策略细节。
- 智能体微服务负责生产 AG-UI / A2UI 事件。
- 前端负责按组件契约消费和展示事件。
- 未定义事件必须前端兼容处理，不能导致页面崩溃。
- 人工确认、扣费确认、高风险 Tool 和业务写入确认必须有明确组件和交互契约。
- 事件必须支持幂等、顺序消费和断线重连补偿。
- 资产保存和创作过程保存必须有明确事件和展示状态。

## 非目标

- 本文档不定义最终工程字段，最终字段以后续 AG-UI 契约为准。
- 本文档不定义最终视觉稿和设计系统。
- 本文档不替代 AG-UI 契约文档。
- 本文档不替代前端组件实现文档。
- 本文档不把模型选择定义为聊天外部组件或独立 A2UI 输出组件。
- 本文档不为 A2UI 单独定义脱离站点主题的视觉风格。
- 本文档不允许通过思考状态打印机输出暴露模型内部推理链路、系统提示词或敏感配置。
- 本文档不允许通过平台能力 Tag 暴露内部 ID、供应商、密钥、endpoint、内部成本或平台配置细节。

## 验收标准

- [ ] 已列出第一阶段需要的 AG-UI / A2UI 组件范围。
- [ ] A2UI 组件样式必须符合站点主题样式。
- [ ] A2UI 组件不单独创建独立视觉风格。
- [ ] A2UI 组件已区分聊天区域组件和聊天外部组件。
- [ ] A2UI 支持思考状态打印机输出。
- [ ] 思考状态打印机输出只展示可公开处理状态，不暴露模型内部推理链路。
- [ ] A2UI 支持 Skill、Tool 等平台内置能力 Tag 展示。
- [ ] 平台能力 Tag 只展示可公开信息，不暴露内部配置、供应商、密钥或成本。
- [ ] 模型选择已明确在聊天输入框内完成，不作为独立 A2UI 组件。
- [ ] 聊天输入框支持单选、多选、输入框、模型选择和素材选择等控件。
- [ ] 聊天外部组件第一版只有一个外部工作区组件。
- [ ] 外部工作区支持资产视图和黑板视图。
- [ ] 每类组件有明确作用、关键状态和主要动作。
- [ ] 已给出 AG-UI / A2UI 事件类型建议清单。
- [ ] 已给出公共 payload 建议字段。
- [ ] 已给出事件顺序、幂等和断线重连设计。
- [ ] 断线重连同时支持 `Last-Event-ID` 和 `run_id + sequence` 补偿。
- [ ] 人工确认、Tool 状态、任务进度、积分预估、思考状态打印机、平台能力 Tag、聊天输入控件、资产视图和黑板视图都有组件承接。
- [ ] 提示词安全评估通过、阻止、失败和超时状态都有组件承接。
- [ ] 资产保存和创作过程保存都有事件和展示状态承接。
- [ ] 前端不得消费未文档化事件字段。

## 待确认事项

- 站点主题的具体设计 token、组件变体和状态样式，后续在前端设计系统中补充。

## Done Gate

- [ ] 组件范围确认。
- [ ] 组件作用确认。
- [ ] 交互契约范围确认。
- [ ] 后续 AG-UI 契约文档入口确认。
- [ ] 验收标准可测试。
- [ ] product_status 更新为 Done 后，才允许进入正式工程开发。
