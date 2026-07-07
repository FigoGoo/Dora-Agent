# Skill Router + 文档展示 · Demo 设计

日期：2026-07-07　范围：demo 维度（真 App 里跑通），两个 slice：**① Skill Router（无选择时自动选）** + **② 文档展示（文档 tab，只读）**。借鉴对象：Flova。

## 1. 目标与范围

**Slice ①**：在真 App 里补上"**用户没手动选 Skill 时，模型自动选一个**"这条链——对齐 Flova 首页"直接写想法生成"那条路径，也填 Dora `docs/flova-benchmark-改造计划.md` 里 Agent 编排层的核心缺口。

**Slice ②**：在工作区加「文档」tab，**只读**展示这个项目的 `Final_Video_Spec.md`（做什么）与绑定的 `skill.md`（怎么做）——对齐 Flova 的「文档」标签，让 demo 体验完整。Dora 现在这两份数据都生成/入库了（text_editor 写 spec、SkillRecord 存 skill 原文），只是没暴露展示。

**行为**
- 会话**已绑 Skill**（用户点了卡片，`sessionRecord.SkillID != ""`）→ 维持现状，Router 不介入。
- 会话**未绑 Skill**（`SkillID == ""`）→ 首条消息时，Router 用 brief × 各 Skill 描述选中一个 → 写回 `sessionRecord.SkillID` → 发 `skill.selected` 事件（含理由）→ 照原流程继续（`SessionValues` 自然带上该 Skill）。

**非目标（本 demo 明确不做）**
- 不新增生成 handler；生成/视频/旁白/音乐**全用现有默认**（image2 / seedance / demo audio）。
- 不做 `require_user_media / first_frame / model_stack` 等 Skill 参数化流程自适应（后续阶段）。
- 文档**只读**——不做在线编辑 / 「另存到我的 Skill」（后续）。
- 不改 `bindSkillToSession` 手动路径。

## 2. 组件与边界

### 2.1 SkillSelector（新，隔离可单测）
位置：`internal/aigc/skill/selector.go`

```go
type SkillOption struct { ID, Name, Description string }

type SkillSelection struct {
    SkillID string
    Reason  string   // 为什么选它（透传给前端）
    Fallback bool    // 是否走了兜底
}

type SkillSelector interface {
    Select(ctx context.Context, brief string, options []SkillOption) (SkillSelection, error)
}
```

**LLM 实现** `llmSkillSelector`：
- 依赖一个最小聊天模型接口（复用现有 DeepSeek chat model；以接口注入，测试可 fake）。
- Prompt：给出 brief + 候选 `[{id,name,description}]`，要求返回**严格 JSON** `{"skill_id": "...", "reason": "..."}`，只能从候选 id 里选。
- 解析：JSON 解析 + 校验 `skill_id ∈ 候选`。解析失败/越界 → 返回 error（交由兜底）。

**边界**：只做"选哪个"，不做绑定、不发事件、不碰 session。输入 brief+候选，输出选择。

### 2.2 兜底策略（在调用方，见 2.3）
- 候选为 0 → 不选，维持无 Skill（现状行为，Agent 走通用）。
- 候选为 1 → 直接选它（不调用 LLM，省一次调用），`Reason="库中唯一 Skill"`。
- LLM 出错/解析失败/越界 → 选**默认 Skill**（`Config.DefaultSkillID`，未配则取候选第一个），`Fallback=true`，`Reason="未能匹配，回落默认"`。

### 2.3 接线：streamMessage（改，最小侵入）
`internal/aigc/server/router.go` · `streamMessage`，在拿到 `sessionRecord` 与 `content` 之后、构造 `AgentInvokeRequest` 之前插入：

```
if sessionRecord.SkillID == "" && cfg.Skills != nil && cfg.SkillSelector != nil {
    options := listEnabledSkillOptions(ctx, cfg.Skills)      // 只取 Enabled
    if len(options) > 0 {
        sel := resolveSkillSelection(ctx, cfg, content, options) // 含 2.2 兜底
        sessionRecord.SkillID = sel.SkillID
        sessionRecord.UpdatedAt = cfg.Now()
        _ = cfg.Store.SaveSession(ctx, sessionRecord)           // 持久化绑定
        emitSkillSelected(cfg, sessionID, runID, sel, options)  // 发事件
    }
}
```
- 之后 `cfg.SessionValues(sessionRecord)` 已带新 `SkillID`，Agent 侧 `adkskill` 中间件照常注入该 Skill 上下文——**无需改 agent 层**。
- `resumeAgent` 不改（resume 时 Skill 已绑定）。
- `listEnabledSkillOptions`：`cfg.Skills.ListEnabled(ctx)`（已存在）→ 逐条 `ParseSkill(record.Content)` 取 `plan.Name / plan.Description` 组成 `SkillOption{record.ID, name, description}`。解析失败的记录跳过。

### 2.4 事件：skill.selected（新）
`internal/aigc/a2ui/events.go` 增一个事件常量与 payload：
```go
const EventSkillSelected = "skill.selected"

type SkillSelectedPayload struct {
    SkillID   string `json:"skill_id"`
    SkillName string `json:"skill_name"`
    Reason    string `json:"reason"`
    Fallback  bool   `json:"fallback,omitempty"`
}
```
在 SSE 流开始处（`streamAgentEvents` 之前或首帧）发出，让前端在对话区显示"已为你选择：〈skill〉（因为…）"。

### 2.5 Config 依赖注入
`server.Config` 增：
```go
SkillSelector  skill.SkillSelector  // 可注入 fake
DefaultSkillID string
```
`cmd/aigc-agent/main.go` 组装真实 `llmSkillSelector`（复用 DeepSeek chat model）。测试传 fake。

### 2.6 前端（最小）
`frontend/src/features/aigc/AigcWorkspacePage.jsx`：监听 `skill.selected` 事件 → 在对话流顶部渲染一条提示条"🧭 已为你自动选择 Skill：〈名称〉——〈理由〉"。手动选路径不变。

## 2B. Slice ② 文档展示（文档 tab，只读）

### 2B.1 后端（两个 GET，复用现成 store 方法）
`internal/aigc/server/router.go` 增两条只读路由：
- `GET /api/aigc/sessions/:session_id/spec`
  → `cfg.Specs.GetLatestBySession(ctx, sessionID)`（已存在）→ 返回 `FinalVideoSpec`（含 `Markdown` = Final_Video_Spec.md 原文 + 结构化字段）。无 spec → 404 或空对象（前端显示"尚未生成"）。
- `GET /api/aigc/sessions/:session_id/skill`
  → 取 `sessionRecord.SkillID`；空 → 返回 `{bound:false}`。非空 → `cfg.Skills.Get(ctx, skillID)`（已存在）→ 返回 `{ id, name, content }`，`content` = `SkillRecord.Content`（skill.md 原文）。
- 依赖：`server.Config` 已有 `Specs`（text_editor 用的同一 store 接口）与 `Skills`；如 Config 未持有 `Specs` 读接口则补一个窄接口 `FinalVideoSpecReader{ GetLatestBySession }`。

### 2B.2 前端（文档 tab，只读渲染）
`AigcWorkspacePage.jsx`：在故事板面板旁加一个「文档」tab（或与故事板并列的视图切换）：
- 左侧列表：`Final_Video_Spec.md`、`skill.md`。
- 右侧：选中项渲染其 markdown（只读）。spec 用 `FinalVideoSpec.Markdown`；skill 用 `content`。
- 进入工作区或收到 `final_video_spec` 相关事件后拉取一次；resume/spec 更新后可重拉。
- 无编辑、无「另存到我的 Skill」（本 demo 只读）。

### 2B.3 边界
两个端点纯只读、无副作用；与 Slice ① 正交（Router 改的是 `streamMessage` 写路径，文档是独立读路径）。skill.md 展示的是"当前绑定的那份"——若本轮是 Router 自动选中的，展示的就是被选中 Skill 的原文，天然联动。

## 3. 数据流

```
用户提交 brief（会话未绑 Skill）
  → streamMessage 取 sessionRecord(SkillID="")
  → listEnabledSkillOptions(cfg.Skills)
  → SkillSelector.Select(brief, options)  [1个直选 / LLM分类 / 出错兜底]
  → sessionRecord.SkillID = 选中；SaveSession（持久化）
  → 发 skill.selected 事件（前端提示）
  → cfg.SessionValues(sessionRecord) 带上 Skill
  → Invoker.Invoke → adkskill 注入该 Skill 上下文 → 照常流式
```

## 4. 错误处理
- 选择器出错 → 兜底默认，绝不阻塞创作流（记录日志 + `Fallback=true`）。
- SaveSession 失败 → 记录日志；本次仍以选中的 SkillID 继续本轮（下次进入会重选/或已持久化）。语义：绑定尽力而为，不因持久化失败中断对话。
- 候选为 0 → 跳过 Router，走现状（无 Skill）。

## 5. 测试（TDD，先红后绿）

**SkillSelector 单测** `skill/selector_test.go`（fake chat model，无真实 LLM；只测纯 LLM 分类，不含 0/1 候选与兜底——那些属调用方）：
- 多条 brief → 期望 skill_id（fake 返回对应 JSON，验证解析+校验）。
- LLM 返回非法 JSON / 越界 id → 返回 error。

**兜底与接线集成测** `server/*_test.go`（fake SkillSelector + 现有 fake store；`resolveSkillSelection` 兜底在此覆盖）：
- 会话无 SkillID + 有 ≥2 候选 → 断言：`Select` 被调用、`SaveSession` 后 `SkillID` 被设置、发出 `skill.selected` 事件、Invoker 被调用。
- 会话已有 SkillID → 断言 SkillSelector **未被调用**、行为与现状一致。
- 候选=1 → **不调用** `Select`、直选唯一项、`Reason="库中唯一 Skill"`。
- 候选=0 → Router 跳过、无 skill.selected 事件、正常继续。
- `Select` 返回 error → 兜底默认 SkillID、`Fallback=true` 事件。

**Slice ② 文档端点测** `server/*_test.go`（fake spec/skill store）：
- `GET /sessions/:id/spec`：有 spec → 返回含 `Markdown`；无 → 404 / 空。
- `GET /sessions/:id/skill`：已绑 → 返回 `{id,name,content}`；未绑 → `{bound:false}`。
- 前端：文档 tab 渲染 spec.Markdown 与 skill.content（组件测，mock 两个接口）。

## 6. 验收（真 App 体验）
1. 起后端（Postgres/Redis/DeepSeek key）+ 前端，种 2–3 份带清晰描述的 demo Skill。
2. 不点任何 Skill，直接输入"帮我做个水蜜桃电商宣传片" → 顶部出现"已为你选择：商品宣传短片（因为…）"，流程照该 Skill 走。
3. 新会话输入"做条北京平谷文旅短片" → 自动选中偏纪录/文旅的 Skill。
4. 手动点某 Skill 再输入 → 不触发 Router，用你选的。
5. 打开「文档」tab → 看到 `Final_Video_Spec.md`（做什么）与 `skill.md`（怎么做）两份只读文档；Router 自动选中的 Skill，其 skill.md 就是这里展示的那份。
