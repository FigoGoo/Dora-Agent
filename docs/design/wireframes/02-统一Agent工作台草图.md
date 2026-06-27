# 统一 Agent 工作台草图

状态：archived
owner：产品与需求责任域
更新时间：2026-06-28
适用范围：统一 Agent 创作工作台 `/app/workspace`、项目上下文、会话恢复、聊天区域 AG-UI 组件、故事板、预览区和资产 / 黑板工作区
相关代码路径：用户端 `frontend/**`；管理端 `admin_frontend/**`
相关产品文档：[项目与资产归属产品系统设计](../../product/项目与资产归属产品系统设计.md)
相关契约：[统一 Agent 工作台 AG-UI 事件协议草案](../../contracts/ag-ui/统一Agent工作台AGUI事件协议草案.md)、[Agent 工作台 API 契约草案](../../contracts/api/Agent工作台API契约草案.md)、[业务微服务 RPC 契约草案](../../contracts/rpc/业务微服务RPC契约草案.md)

## 目标用户

- 个人用户：使用个人 Skill、个人积分和个人资产创作。
- 企业成员 / 企业拥有者：在企业空间中使用企业积分、企业 Skill 和本人企业空间资产创作。

## 主任务

在当前项目下，用一个 Agent 完成输入、意图识别、Skill 路由、模型选择、内容安全评估、积分确认、生成、资产保存、黑板更新和会话恢复。

## 结构线框

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ 顶部项目工具条：Dora-Agent / 项目标题 / 素材 / 项目 / 剪辑 / 导出 / 积分    │
├──────────────────────┬────────────────────────────┬──────────────────────────┤
│ StoryboardPanel      │ PreviewStage               │ ChatPanel                │
│ Header：故事板        │ Header：预览                │ Header：对话             │
│ 关键元素 / 分镜列表   │ 模型 / 尺寸 Chip            │ MessageList              │
│ ┌ Element_Ronin ┐    │                            │ Dora 回复                │
│ │ 描述 + 缩略图  │    │     当前元素生成预览          │ MediaAssetsCard          │
│ └───────────────┘    │     角色 / 场景 / 分镜       │ 结果表格                 │
│ ┌ Element_Assassin ┐ │                            │ 下一步提示               │
│ └───────────────┘    │                            ├──────────────────────────┤
│ ┌ Mountain_Road ┐   │                            │ Composer：输入消息       │
│ └───────────────┘   │                            │ [+] [模型] [Skill] [资产库]│
└──────────────────────┴────────────────────────────┴──────────────────────────┘
```

## 页面区域

| 区域 | 作用 |
| --- | --- |
| 顶部项目工具条 | 当前项目、素材、项目、剪辑、导出、积分和用户状态。 |
| StoryboardPanel | 左侧故事板，承载关键元素、分镜、脚本、提示词、素材缩略图和黑板结构化内容。 |
| PreviewStage | 中央预览区，承载当前元素 / 分镜的生成预览、模型、尺寸和切换控制。 |
| ChatPanel | 右侧对话面板，承载消息流、Thinking、Tag、确认卡、生成进度、错误卡和输入框。 |
| MediaAssetsCard | 出现在 ChatPanel 消息流中，展示本轮生成资产摘要和完成状态。 |
| Composer | 固定在 ChatPanel 底部，包含输入、模型、Skill、资产库和发送入口。 |
| 会话恢复条 | 断线或历史进入时展示恢复中、已恢复或恢复失败。 |

## 关键组件

| 组件 | 用途 |
| --- | --- |
| AgentWorkspace | 工作台页面骨架。 |
| ProjectContextBar | 展示当前项目、当前空间、返回项目详情和新会话入口。 |
| StoryboardPanel / StoryboardCard | 展示关键元素、分镜、脚本、提示词、素材缩略图和生成状态。 |
| PreviewStage | 展示当前选中元素或分镜的预览、模型和尺寸信息。 |
| ChatMessage / MessageStream | 用户和 Agent 文本消息，支持增量渲染。 |
| ThinkingTypewriter | 只展示可公开处理状态。 |
| PlatformTags | Skill、Tool、Model、Risk、Status 等短 Tag。 |
| CreditEstimate / ConfirmationPanel | 扣费、高风险和业务写入确认。 |
| ToolStatus / GenerationProgress | Tool 状态、生成进度、部分完成、取消和失败。 |
| MediaAssetsCard | 展示生成资产摘要、缩略图、完成状态和跳转入口。 |
| Composer | 文本、上传、素材引用、模型选择和参数入口。 |
| RestoreBanner | 重连、补偿、快照恢复状态。 |

## 状态覆盖

| 状态 | 工作台表现 |
| --- | --- |
| loading | 初始化项目、身份、Skill、模型、积分、会话时显示工作台骨架。 |
| empty | 新会话无消息时展示输入入口和少量场景建议。 |
| error | AG-UI、API、RPC 或生成失败时展示错误卡和可恢复动作。 |
| success | 生成完成、资产保存、扣费闭环完成后展示产物摘要。 |
| streaming | `message.delta` 增量渲染，布局不跳动。 |
| interrupt | `interrupt.required` 或确认事件出现时展示确认面板，锁定关键输入。 |
| resume | 断线重连后按 event_id 去重、sequence 补偿或快照恢复。 |
| archived_readonly | 项目归档后展示只读 Banner，禁用 Composer、确认、重试、继续生成和保存新资产。 |

## AG-UI / API / RPC 依赖

| 依赖 | 说明 |
| --- | --- |
| AG-UI | 消息流、Thinking、Skill/Tool Tag、安全评估、项目归档阻断、积分估算、确认、Tool 状态、生成进度、资产保存、黑板更新、run 完成或失败。 |
| API | 创建项目下会话和 run、发送用户输入、确认、取消、重试、会话恢复、事件补偿和快照查询。 |
| RPC | 当前空间解析、项目权限、Skill 池、模型可用性、Tool 权限、积分预估/冻结/扣减/释放、资产保存。 |

## 待确认项

- AG-UI 正式字段使用 `type`，第一版兼容读取 `event_type`。
- 工作台事件必须携带 `project_id`，用于前端确认当前项目上下文和归档只读态。
- 断线补偿窗口、快照结构和恢复失败文案需要契约确认。
- 可取消 Tool 和可重试 Tool 的范围需要后端契约确认。
- 0 积分生成是否仍展示同样确认面板，需产品口径确认。

## 边界声明

工作台不保存项目、积分、资产、企业、作品等业务事实；业务事实由业务微服务通过 API / RPC 维护。前端只消费契约化事件和字段，不自行解释 Eino 内部事件。
