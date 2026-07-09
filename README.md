# Dora Agent

Dora Agent 是一个面向 AIGC 视频创作的 Agent 工作台。后端使用 Go/Eino 驱动 Agent、工具、故事板和生成任务，前端使用 React 渲染对话、故事板、工具进度和 A2UI 交互卡片。

当前交互规范已经统一为 **A2UI Action 协议**：Agent 直接输出卡片新增或卡片更新指令，后端不再根据文本或工具结果猜测 UI。

## 核心目录

- `cmd/aigc-agent`：AIGC 服务入口，初始化数据库、Redis、Eino Runner、生成 worker 和 HTTP 路由。
- `internal/aigc/agent`：DeepSeek/Eino Agent 构建和运行时工具注册。
- `internal/aigc/a2ui`：A2UI Action 协议类型、Agent 协议提示词、SSE 事件和内存事件 broker。
- `internal/aigc/server`：HTTP API、SSE 流、A2UI action 转发、后台任务事件桥接。
- `internal/aigc/tools`：创作工具实现，例如 Image2、Seedance、WritePrompt。
- `internal/aigc/generation`：异步生成任务、worker 和 provider handler。
- `internal/aigc/storyboard`：故事板模型、patch、资产绑定。
- `frontend/src/features/aigc`：AIGC 工作台页面、A2UI action dispatcher 和组件渲染器。

## A2UI Action 协议

Agent 需要展示 UI 时，输出纯 JSON：

```json
{
  "a2ui_version": "1.0",
  "actions": [
    {
      "type": "append_card",
      "surface": "chat",
      "card_id": "brief-intake",
      "card": {
        "title": "补充产品信息",
        "submit_label": "提交",
        "root": "root",
        "components": [
          { "id": "root", "component": { "Card": { "children": ["product", "platform", "asset"] } } },
          { "id": "product", "component": { "TextInput": { "key": "product_name", "label": "产品名称/品类", "required": true } } },
          {
            "id": "platform",
            "component": {
              "MultiChoice": {
                "key": "platform",
                "label": "投放平台",
                "options": [
                  { "value": "douyin", "label": "抖音" },
                  { "value": "xiaohongshu", "label": "小红书" }
                ]
              }
            }
          },
          { "id": "asset", "component": { "FileUpload": { "key": "reference_file", "label": "上传参考图片", "accept": "image/*" } } }
        ]
      }
    }
  ]
}
```

支持的 action：

- `append_card`：新增一张消息卡。Agent 必须提供稳定的基础 `card_id`，后端会在发布和持久化前把它扩展成实例级唯一 `card_id`。
- `update_card`：更新已有卡、左侧故事板或工具状态。通过 `target.surface` 和 `target.card_id` 定位；需要精确更新某一张聊天卡时使用历史里的完整 `card_id`。

后端只负责识别纯 `ActionEnvelope` 并通过 SSE 发出 `a2ui.action`，不会再从自然语言、Markdown 表格、工具结果或历史协议中推断 UI。

结构化输出由 Eino ChatModel 层控制：DeepSeek ChatModel 启用 `ResponseFormatTypeJSONObject`，Runner 调用时通过 `adk.WithChatModelOptions` 传入 `MaxTokens`、`Temperature` 等模型参数。后端只做 A2UI 协议边界校验，不做模型输出重试纠偏，也不用工具包装去改写模型输出。

## card_id 约定

Agent 输出的 `append_card.card_id` 是基础业务名：

- 聊天卡：按业务命名，例如 `brief-intake`、`skill-selection`、`stage-confirmation`。
- 工具运行卡：使用 `tool_run:<tool_call_id>`；媒体生成统一聚合为 `tool_run:media_generator`。
- 故事板更新：使用 `storyboard` 或 `storyboard:<session_id>`。

后端识别到合法 `append_card` 后，会把聊天卡的基础 `card_id` 扩展为实例级唯一值，例如 `skill-selection:ca0cf5879e53eb347fe2d5affb6da507`，并把改写后的 ActionEnvelope 同时发布到 `/events/stream` 和写入历史消息。前端提交表单后按这个完整 `card_id` 删除对应卡实例，因此同一个对话里可以存在多张同类卡而不会互相覆盖。

同一个业务卡后续状态更新优先使用 `update_card`；如果确实要在聊天区新增一张新的同类卡，可以继续使用相同基础 `card_id` 的 `append_card`，后端会生成新的完整 `card_id`。

## 业务交互流程

### 1. 普通回答

普通说明也必须由 Agent 输出 `append_card`。卡片可以只包含一个 `Text` 或 `Markdown` 组件，但外层仍然是 `ActionEnvelope`，后端不会把普通文本自动包装成消息卡。

### 2. 信息收集表单

Agent 输出 `append_card`，card 内包含 `TextInput`、`SingleChoice`、`MultiChoice`、`FileUpload` 等组件。提交按钮统一显示为“提交”。前端不会发送 A2UI 提交事件，而是按组件类型把用户输入归约成普通用户消息：

- 单选：直接提交所选项文本。
- 多选：用 `、` 连接所选项文本。
- 文本输入：直接提交用户输入文本。
- 文件上传：提交上传后的 `file_id`，聊天框只显示缩略图或文件名，不直接显示 `file_id`。
- 分页表单：多段提交内容用换行符隔开。
- 选择器和文本输入混合：选择项文本在前，用 `、` 连接用户输入文本。

```json
{
  "content": "抖音、小红书、智能手表",
  "ui_source": {
    "type": "a2ui_submit",
    "card_id": "brief-intake:6f7a9b0c1d2e3f4a"
  }
}
```

后端会持久化这条普通用户消息和 `ui_source` 元数据；Eino history 只恢复普通 `content` 给 Agent，不把 `ui_source` 注入模型上下文。刷新历史时，前端根据 `ui_source.card_id` 移除已提交的表单卡实例。A2UI 只负责采集交互，不作为提交事件进入创作历史。

### 3. 故事板更新

异步生成 worker 内部仍然使用 `storyboard.patch` 领域事件。进入前端前，后端桥接为：

```json
{
  "a2ui_version": "1.0",
  "actions": [
    {
      "type": "update_card",
      "surface": "storyboard",
      "target": { "surface": "storyboard", "card_id": "storyboard:s1" },
      "payload": {
        "patch": {
          "ops": [
            { "op": "replace", "path": "/shots/0/scene_description", "value": "竹林夜雨" }
          ]
        }
      }
    }
  ]
}
```

前端按 JSON Patch 更新左侧故事板，不在聊天区新增重复卡。

### 4. 工具状态更新

后台任务状态进入前端前会桥接为 `update_card`，更新同一个工具运行卡：

```json
{
  "type": "update_card",
  "surface": "tool_runs",
  "target": { "surface": "tool_runs", "card_id": "tool_run:media_generator" },
  "payload": {
    "tool_run": {
      "tool_key": "media_generator",
      "display_name": "Media Assets",
      "status": "running",
      "nodes": []
    }
  }
}
```

### 5. 长表单分页

长表单建议拆成多张稳定卡：

- 第 1 页：`card_id=brief-page-1`
- 第 2 页：`card_id=brief-page-2`
- 汇总确认：`card_id=brief-review`

每页提交都按同一套组件归约规则生成普通用户消息；分页内容之间使用换行符隔开。Agent 根据这条消息决定下一张 `append_card` 或对当前卡 `update_card`。不要让后端保存“第几页”的硬编码业务状态。

## 运行方式

后端需要 PostgreSQL、Redis、DeepSeek API Key，以及可选的 Image2/Seedance/TOS 配置。

```bash
AGENT_HTTP_ADDR=:19080 \
DORA_DEEPSEEK_API_KEY=... \
DORA_IMAGE2_API_KEY=... \
DORA_SEEDANCE_API_KEY=... \
TOS_ENDPOINT=... \
TOS_BUCKET=... \
TOS_ACCESS_KEY_ID=... \
TOS_SECRET_ACCESS_KEY=... \
TOS_REGION=... \
TOS_BASE_URL=... \
/Users/figo/sdk/go1.26.3/bin/go run ./cmd/aigc-agent
```

前端：

```bash
cd frontend
npm install
npm run dev -- --host 127.0.0.1 --port 3200
```

## 验证命令

后端：

```bash
/Users/figo/sdk/go1.26.3/bin/go test ./internal/aigc/... -count=1
```

前端：

```bash
cd frontend
npm test
npm run build
```

代码格式检查：

```bash
git diff --check
```

## 开发约束

- 新增交互组件时，优先扩展 A2UI component 渲染器和协议类型，不改工具返回结构。
- 工具只返回业务结果，例如 asset、storyboard update、summary，不返回 UI 渲染事件，也不返回 `a2ui_type` / `a2ui_hint` 这类 UI 意图字段。
- Agent 调用工具时必须使用 `ToolInvocationEnvelope`：顶层包含 `session_id`、`request_id`、`idempotency_key`、`action`，业务字段全部放在 `payload`。
- Agent 需要 UI 时必须输出 `a2ui_version + actions` 的纯 JSON。
- 后端只透传 Agent 直出的 A2UI action；如果 Agent 输出普通文本、Markdown 表格或 `<details>` HTML，会返回协议错误事件。
- 后端不得根据自然语言关键词或工具结果猜测表单、选择列表或确认卡。
- 前端只执行 `/events/stream` 中的 `a2ui.ready`、`a2ui.action`、`a2ui.interrupt_request` 和 `error` 事件。
