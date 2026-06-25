# 统一 Agent 工作台 AG-UI 事件协议草案

状态：draft
owner：主控 Codex 汇总维护；Go Eino 智能体微服务架构工程师负责生产语义；前端开发工程师负责消费语义
更新时间：2026-06-25
适用范围：智能体微服务 -> Dora-Agent Web 前端统一 Agent 工作台

## 事件背景

统一 Agent 工作台通过 SSE 接收 Agent 运行、消息增量、Skill/Tool 状态、人工确认、积分、资产保存、黑板更新和恢复事件。前端只消费协议内事件，不读取 Eino 内部事件。

## 传输方式

- SSE：第一版默认实时通道，路径待 Agent API 契约确认。
- WebSocket：第一版不作为默认方案。
- HTTP 补偿查询：支持 `run_id + after_sequence` 或 `last_event_id` 补偿。
- 快照恢复：补偿失败时查询 run/session snapshot。

## 公共 payload

```json
{
  "event_id": "evt_xxx",
  "type": "agent.run.started",
  "session_id": "sess_xxx",
  "run_id": "run_xxx",
  "project_id": "proj_xxx",
  "space_id": "space_xxx",
  "sequence": 1,
  "timestamp": "2026-06-25T10:00:00Z",
  "trace_id": "trace_xxx",
  "payload": {}
}
```

兼容口径：正式字段使用 `type`；第一版消费者可兼容读取 `event_type`，但协议文档和测试以 `type` 为准。

公共字段中的 `trace_id` 只用于排障。前端最多在错误详情中弱展示为“问题编号”，不得把内部 run 调度细节、Eino 节点名或供应商信息展示给用户。

## 事件类型

| 事件类型 | 触发时机 | 生产方 | 消费方行为 |
| --- | --- | --- | --- |
| agent.run.started | run 开始 | 智能体微服务 | 初始化运行状态 |
| agent.run.completed | run 完成 | 智能体微服务 | 展示完成、停止 loading |
| agent.run.failed | run 失败 | 智能体微服务 | 展示错误和可恢复动作 |
| agent.run.cancelled | run 被取消 | 智能体微服务 | 停止生成，保留已有内容 |
| project.archived.blocked | 项目归档阻断继续创作 | 智能体微服务 | 工作台切只读，禁用 Composer、确认、重试和继续生成 |
| agent.thinking.started | 公开处理状态开始 | 智能体微服务 | ThinkingTypewriter 开始展示 |
| agent.thinking.delta | 公开处理状态增量 | 智能体微服务 | ThinkingTypewriter 增量展示 |
| agent.thinking.completed | 公开处理状态结束 | 智能体微服务 | ThinkingTypewriter 弱化或折叠 |
| agent.message.delta | Agent 文本增量 | 智能体微服务 | MessageStream 增量渲染 |
| agent.message.completed | Agent 消息完成 | 智能体微服务 | 固化消息 |
| agent.skill.selected | 选中 Skill | 智能体微服务 | 展示 SkillTag |
| agent.skill.missing | 未匹配 Skill | 智能体微服务 | 展示文本兜底状态 |
| platform.tags.updated | Skill/Tool/Model/Risk/Status Tag 更新 | 智能体微服务 | 更新标签栏 |
| chat.controls.requested | 请求前端输入控件 | 智能体微服务 | 展示单选、多选、输入框或素材选择 |
| chat.controls.locked | 关键参数锁定 | 智能体微服务 | 锁定模型和关键参数 |
| safety.prompt.evaluating | 安全评估开始 | 智能体微服务 | 可展示“安全评估中”，不展示规则 |
| safety.prompt.evaluated | 安全评估通过 | 智能体微服务 | 进入积分预估 |
| safety.prompt.blocked | 安全评估阻断 | 智能体微服务 | 展示 blocked，停止生成 |
| credits.estimated | 积分预估完成 | 智能体微服务 | 展示 CreditEstimate |
| credits.frozen | 积分冻结成功 | 智能体微服务 | 进入生成 |
| credits.charged | 扣费成功 | 智能体微服务 | 更新积分展示 |
| credits.released | 冻结释放 | 智能体微服务 | 展示释放原因 |
| confirmation.required | 需要人工确认 | 智能体微服务 | 展示 ConfirmPanel |
| confirmation.accepted | 用户确认 | 智能体微服务 | 锁定输入，继续 run |
| confirmation.rejected | 用户拒绝 | 智能体微服务 | 停止对应操作 |
| tool.call.started | Tool 开始 | 智能体微服务 | 展示 ToolStatus |
| tool.call.progress | Tool 进度 | 智能体微服务 | 更新进度 |
| tool.call.completed | Tool 完成 | 智能体微服务 | 展示结果摘要 |
| tool.call.failed | Tool 失败 | 智能体微服务 | 展示可重试/失败状态 |
| generation.progress | 生成进度 | 智能体微服务 | 更新 GenerationProgress |
| generation.artifact.completed | 单个产物完成 | 智能体微服务 | 更新 PreviewStage |
| asset.save.started | 资产保存开始 | 智能体微服务 | 展示保存中 |
| asset.save.completed | 资产保存完成 | 智能体微服务 | 更新 MediaAssetsCard |
| asset.save.failed | 资产保存失败 | 智能体微服务 | 展示 save_failed 并触发释放 |
| workspace.assets.updated | 工作区资产更新 | 智能体微服务 | 更新资产视图 |
| workspace.blackboard.updated | 黑板更新 | 智能体微服务 | 更新 StoryboardPanel |
| process.snapshot.saved | 快照保存 | 智能体微服务 | 标记可恢复点 |

## 产品可见性规则

| 事件类别 | 用户可见组件 | 展示口径 |
| --- | --- | --- |
| `agent.run.*` | ChatPanel / 状态条 | 开始处理、完成、失败、已取消 |
| `agent.thinking.*` | ThinkingTypewriter | 只展示“正在分析素材”“正在生成分镜”“正在保存资产”等公开状态 |
| `safety.prompt.evaluated` | 默认静默或弱 Tag | 通过后继续积分预估 |
| `safety.prompt.blocked` | SafetyNotice / ErrorNotice | `内容不符合平台规则，请修改后重试。` |
| `project.archived.blocked` | ReadonlyBanner / ErrorNotice | `项目已归档，无法继续创作。` |
| `credits.*` | CreditEstimate / ConfirmPanel | 预计积分、余额、冻结、扣费或释放 |
| `confirmation.*` | ConfirmPanel | 扣费、高风险、业务写入必须等待用户确认 |
| `tool.call.*` | ToolStatus | 工具名称、状态、耗时和结果摘要 |
| `generation.*` | GenerationProgress / PreviewStage | 排队、生成、部分完成、完成、失败、取消 |
| `asset.save.*` | MediaAssetsCard / ErrorNotice | 保存中、保存成功、保存失败 |
| `workspace.*` | StoryboardPanel / PreviewStage | 更新故事板、黑板、资产缩略图和当前预览 |
| `process.snapshot.saved` | 默认静默 | 仅在恢复时体现“已恢复到最新状态” |

不得展示给用户：`event_id`、`sequence`、内部 run 调度细节、Eino 节点名、系统 Prompt、完整组装 Prompt、模型推理链路、供应商原始响应、API Key、内部成本、完整 Tool 原始参数、内容安全内部评分和命中规则细节。

## 关键 payload 约束

### safety.prompt.*

```json
{
  "safety_status": "evaluating|passed|blocked|failed",
  "safety_evidence_id": "safe_xxx",
  "result": "passed",
  "user_message": "内容不符合平台规则，请修改后重试。",
  "suggested_action": "edit_prompt",
  "retryable": true,
  "trace_id": "trace_xxx"
}
```

- `safety_evidence_id` 只在 `passed` 后作为业务写入证据引用。
- 阻断或失败事件只给用户可理解原因，不给业务写入，不进入积分预估、冻结或生成。
- payload 不包含策略细节、内部评分、完整 Prompt、供应商原始响应或推理链路。

### project.archived.blocked

```json
{
  "project_status": "archived",
  "creative_allowed": false,
  "read_only_reason": "project_archived",
  "allowed_actions": ["view"],
  "user_message": "项目已归档，无法继续创作。"
}
```

- 创建 session/run 前发现归档时优先由 Agent API 返回 `409 PROJECT_ARCHIVED`，不创建 run，也不产生 SSE。
- 已有 run 或运行中二次校验发现归档时，发送 `project.archived.blocked`，随后发送 `agent.run.cancelled`。
- 前端收到后禁用 Composer、模型选择、确认按钮、重试按钮和继续生成入口，保留历史、资产、黑板和作品只读。

## 事件顺序

建议主路径：

```text
agent.run.started
agent.thinking.started / agent.thinking.delta / agent.message.delta
agent.skill.selected / agent.skill.missing
chat.controls.requested
safety.prompt.evaluating
safety.prompt.evaluated | safety.prompt.blocked
credits.estimated
confirmation.required
confirmation.accepted | confirmation.rejected
credits.frozen
tool.call.started -> tool.call.progress -> tool.call.completed
generation.progress -> generation.artifact.completed
asset.save.started -> asset.save.completed
workspace.assets.updated / workspace.blackboard.updated
credits.charged | credits.released
agent.run.completed | agent.run.failed | agent.run.cancelled
```

归档阻断序列：

```text
project.archived.blocked
credits.released
agent.run.cancelled
process.snapshot.saved
```

## 前端渲染规则

- `event_id` 去重，重复事件忽略。
- 同一 `run_id` 内按 `sequence` 合并，发现缺口后触发补偿查询。
- 未知事件忽略并记录 debug 日志，不崩溃。
- Thinking 只展示公开处理状态，不展示模型内部推理链路。
- payload 不得包含 API Key、供应商原始密钥、内部成本、系统 Prompt、完整推理链路。
- 安全通过事件可静默处理；安全阻断、归档阻断和扣费失败必须展示用户可理解提示。
- 媒体预览和下载不得依赖 Agent 事件中的长期 URL，必须通过业务 API 获取授权后的 TOS 公共 URL。

## 待确认

- `confirmation.required` 与标准 `interrupt.required` 是否保留映射事件。
- 补偿查询 API 的路径和分页上限。
- 高频 progress 事件是否需要节流。
