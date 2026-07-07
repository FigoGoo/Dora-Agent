# Skill Router Demo · 交付记录

日期：2026-07-08　分支：`cc/core-version`　对标：Flova.ai

本次在真 App 里落地了一版 demo，围绕 Flova 对标改造计划里两条核心流程——**③ Skill 路由内部** 与 **② 异步 job 状态机**——把「首页写想法 → 自动选 Skill → 分析/spec/故事板 → 生成媒体 → 文档展示」这条链打通，媒体生成用固定占位资产模拟，无需真调 provider。

相关文档：
- 设计规格：`docs/superpowers/specs/2026-07-07-skill-router-demo-design.md`
- 实施计划：`docs/superpowers/plans/2026-07-07-skill-router-demo.md`
- 对标改造总计划：`docs/flova-benchmark-改造计划.md`

---

## 1. 交付范围

| # | 能力 | 状态 |
|---|------|------|
| ③ | **Skill 路由内部** —— 用户未手动选 Skill 时，模型按 brief × Skill 库描述自动选中一个、绑定、注入上下文、透出理由，无匹配回落默认 | ✅ 已实现并真机验收 |
| ② | **异步 job 状态机** —— 生成任务 `queued→running→succeeded`、绑资产、发事件；无 provider key 时用固定媒体让状态机真正走到 succeeded | ✅ 代码经确定性集成测试证明；固定媒体已接线 |
| — | **文档展示** —— 工作区「文档」tab 只读渲染 `Final_Video_Spec.md` 与绑定的 `skill.md` | ✅ 已实现 |
| — | **首页直达** —— 首页输入框「开始创作」建会话 + 带 brief 进工作区自动发首条（对齐 Flova 首页直写生成） | ✅ 已实现 |
| — | **工具 session_id 兜底** —— 消除每个业务工具首次调用因缺 session_id 而失败重试 | ✅ 已修复并真机验收 |

---

## 2. 提交清单（`cc/core-version`，计划 `f10395e` 之后）

**Slice ① Skill Router + Slice ② 文档展示（后端）**
- `313a418` feat(skill): SkillSelector 接口 + LLM 实现（哨兵错误 + 边界测试）
- `a9bace0` feat(a2ui): `skill.selected` 事件 + payload
- `4080e1c` feat(server): streamMessage 未绑会话自动路由 Skill + 兜底
- `88f8b99` chore(main): 装配 SkillSelector / Publisher
- `4fe75e2` feat(server): 只读端点 `GET /spec`、`GET /skill`
- `c135b0e` refactor(server): 端点审查修复（复用 ensureSession、具名响应）
- `a16dd16` chore(main): 注入 spec store

**前端**
- `450f732` feat(frontend): `skill.selected` 自动选择提示条
- `2ec72a5` feat(frontend): 只读「文档」tab（spec.md / skill.md）
- `1abe547` feat(frontend): 首页 brief → 工作区、自动发首条

**② job 状态机 / 工具鲁棒性**
- `67356da` fix(tools): 工具从上下文兜底解析 session_id（text_editor / storyboard_designer / write_prompt / media_generator）
- `7a5e572` feat(generation): 固定媒体 demo handler（图/视频/音频）
- `0dbda44` test(generation): 确定性 worker + demo-handler 集成测试

---

## 3. 关键实现点

### ③ Skill 路由内部
- `internal/aigc/skill/selector.go`：`SkillSelector` 接口 + `llmSkillSelector`（复用 DeepSeek，返回严格 JSON `{skill_id, reason}`，校验 `skill_id ∈ 候选`，越界/坏 JSON 报哨兵错误）。
- `internal/aigc/server/skill_router.go`：`listEnabledSkillOptions`（`ListEnabled` + `ParseSkill` 组候选）、`resolveSkillSelection`（候选 0 跳过 / 1 直选 / 出错回落 `DefaultSkillID`）、`emitSkillSelected`（经 a2ui broker 广播）。
- 接线在 `streamMessage`：`SkillID==""` 且 Router 已配置 → 选中 → 写回 session → 发事件。**关键不变量**：选中在 `cfg.SessionValues(sessionRecord)` 之前完成，故**当轮**即通过 `turncontext.SessionValues`（`aigc.session.id` / `aigc.session.skill_id`）注入 agent，Skill 立刻生效（有断言锁定）。
- Agent 层零改动。

### ② 异步 job 状态机 + 固定媒体
- 媒体图 `mediagraph/generator.go` 经 `dispatcher.Dispatch` 把任务派进 `generation` 异步流水线；worker 取任务 → handler → `succeeded` → `syncStoryboardAssets` 绑到关键元素/镜头 → 发 `job.status`/`storyboard.patch` 事件。
- `internal/aigc/generation/handlers/demo_media.go`：通用 `DemoMediaJobHandler`（按 Kind 参数化 image/video/audio），造带**固定 URL** 的 asset、返回 AssetID。`cmd/aigc-agent/main.go` 在无对应 provider key 时自动挂 demo handler：
  - image2 → `works/*.png`（文旅/电商/产品等，按 target 轮换）
  - seedance → `frontend/public/demo/demo-shot.mp4`
  - audio → `demo-narration.mp3` / `demo-music.mp3`
- **证明**：`worker_integration_test.go` 用真 handler + fake store 断言 `queued→running→succeeded` + 资产持久化；`demo_media_test.go` 断言造对资产、URL 轮换、空配置报错。

### 工具 session_id 兜底（`67356da`）
- 业务工具的 `session_id` 之前只从模型参数取，DeepSeek 首次调用常漏填 → 每个工具白失败一次（`session_id is required`）再重试。
- `internal/aigc/tools/session_context.go`：`sessionIDFromContext(ctx)` 读 `adk.GetSessionValue(ctx, "aigc.session.id")`（turncontext 已注入）；工具改为 `firstNonEmpty(invocation.SessionID, payload.SessionID, sessionIDFromContext(ctx))`。附防漂移守卫测试（常量须等于 `turncontext.SessionIDValueKey`）。
- 真机验收：修复后两轮完整运行，`session_id is required` 由每工具一次降为 **0**。

### 文档展示 / 首页直达
- 后端 `internal/aigc/server/documents.go`：`GET /spec`（`Specs.GetLatestBySession`，404/500 区分）、`GET /skill`（当前绑定 skill 原文，未绑 `{bound:false}`；有意展示实际绑定即使已禁用）。
- 前端 `AigcWorkspacePage.jsx`：`skill.selected` 提示条；故事板/文档视图切换，`<pre>` 只读渲染。
- 首页 `LandingPage.jsx`：「开始创作」建新会话 + 暂存 brief（`dora:aigc:pending_brief`）+ 开 `/workspace?session_id=`；工作区挂载后 ref 守卫下自动发一次首条。空输入维持原登录引导。

---

## 4. 本地运行 / 体验

```bash
docker compose -f docker-compose.local.yml up -d          # Postgres + Redis
go build -o /tmp/aigc-agent ./cmd/aigc-agent && /tmp/aigc-agent   # 后端 :18080（需 DORA_DEEPSEEK_API_KEY）
cd frontend && npm run dev                                # 前端 :3200，代理 /api → :18080
```

> 说明：本地反复重启建议用**显式编译的二进制**（`go build -o` 后直接运行），避免 `go run` 多次重启后产生僵尸子进程、进程管理混乱。

**种 demo Skill**（Router 需要候选才触发）：`POST /api/aigc/skills`，body `{"content":"<name>…</name><description>…</description><planner>1. … **text_editor**\n…</planner>"}`。已内置示例：商品宣传短片 / 人文纪录短片 / 活动快剪集锦。

**体验路径**：首页 http://localhost:3200 → 大输入框写想法 → 发送 → 新标签页进工作区、自动发首条 → 顶部「🧭 已为你自动选择 Skill」 → 分析/spec/故事板 → 生成媒体（出固定图/视频/音频）→ 左侧「文档」tab 看 spec.md / skill.md。

---

## 5. 已知 live 摩擦（非本次代码问题）

真机端到端到「生成媒体」这步的可靠性，受几处外部因素影响，均**非本次交付代码**：

1. **模型随机性**：DeepSeek 每轮是否调用 `media_generator`、派发几个 job 不稳定（实测有时派 1–4 个、有时 0 个）。
2. **DeepSeek TLS 抖动**：偶发 `tls: bad record MAC` 打断 chat stream，重试即可。
3. **偶发畸形工具 JSON**：DeepSeek 生成坏字符，tool-exception 中间件让其重试。

一旦 job 真被派发，状态机就能走完、出固定媒体、绑上故事板（代码已证明）。

---

## 6. 后续可做

- **一键生成媒体按钮**：故事板 UI 直接派发 job，绕开「靠模型每轮自觉调用 media_generator」的随机性，让 demo 体验稳定。
- **改造计划 P0**（见 `docs/flova-benchmark-改造计划.md`）：给 `SkillPlan/SkillStage` 加 `ModelStack`（模型栈按 Skill 切换）/ `RequireUserMedia`（素材接地开关）/ 流水线拓扑声明（是否插首帧、视频/音频并行）。
