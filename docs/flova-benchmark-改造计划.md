# Flova 对标 · Dora-Agent 改造计划（只提计划，暂不动代码）

> 来源：用 Flova.ai 实跑两支片（①水蜜桃「商品宣传短片」竖屏电商；②北京平谷「人文纪录短片」横屏文旅），
> 全程记录交互/接口/产物，逆向其流程与协议，对照 Dora 现状。
> 原始研究材料：`~/Desktop/桃子宣传片/flova运行记录/`（商品片）与 `flova运行记录-平谷文旅/`（文旅片）。
>
> **核心判断：Dora 的骨架已经和 Flova 高度同构，这是一份"补缺口 + 接线"的计划，不是重写。**

---

## 0. 结论先行

Dora 已具备的对标能力（无需新建，验证过 file 引用）：

| 能力 | Dora 现状 | 位置 |
|---|---|---|
| 结构化事件信封 | `SSEEvent{ID,SessionID,RunID,Seq,Event,Payload,CreatedAt}` | `internal/aigc/a2ui/events.go` |
| 确认卡点 + 建议芯片 | `InterruptRequestPayload{CheckpointID,SpecVersion,StoryboardVersion,Actions[]ActionSchema{Key,Label,Description,Schema}}` | `a2ui/events.go` |
| Skill=阶段+工具+依赖+pause | `SkillPlan.Stages[]SkillStage{Key,Title,Goal,ToolKeys,DependsOn,PauseAfter,Instruction}`，解析 `<name>/<description>/<planner>` | `skill/parser.go` |
| Final Spec（含模型偏好+文档形态） | `FinalVideoSpec{...,ModelPreference,Markdown,Version,Status(draft/reviewing/confirmed)}` | `spec/models.go` |
| 异步 job（幂等+状态版本） | `GenerationJob{IdempotencyKey,StatusVersion,Provider,TargetType,TargetID,ResultAssetIDs,MaxRetries}` | `generation/models.go` |
| Handler 注册表 | `Worker.Handlers map[string]JobHandler`，按 Provider 派发 | `generation/worker.go` |
| storyboard 乐观锁 | base_version / ErrVersionConflict(409) | `storyboard/` |

→ Flova 展示的 A2UI 事件流、interrupt/resume、Skill 编排、spec 文档、异步生成，Dora **概念上都有**。

**真正的缺口**集中在三处（Flova 两支片的差异恰好把它们暴露出来）：
1. **模型栈不能按 Skill 切换**：Provider 是写死枚举，商品片用 Nano Banana+Seedance、文旅片用 GPT Image 2+Happy Horse+MiniMax，Dora 无此抽象。
2. **流水线拓扑固定**：`mediagraph` 单一路径，无"首帧/关键帧"可选步骤（文旅片走 元素→**首帧**→视频），无"视频/音频串行 vs 并行"编排。
3. **素材接地是硬约束而非 Skill 开关**：商品片强制上传、文旅片从零生成——Dora 需要 `require_user_media` 之类的 Skill 级开关。

---

## 1. 逐维度差距表（Dora 现状 → Flova 展示 → 改造动作）

### D1. 事件协议 A2UI
- 现状：`SSEEvent` 单信封，`Event` 为字符串枚举（chat.delta/message、a2ui.*、storyboard.snapshot/patch、tool.progress、job.status、error）。单条 SSE 流。
- Flova：`/chat` 只回 `stream_chat_id`，另起 `stream_chat/info` 拉 NDJSON；信封 `{id,type,data,timestamp}`；`message_type` 枚举承载富内容；`notification` 承载媒体生成生命周期。
- 差距：Dora 事件语义足够；缺 (a) 提交/消费流解耦（断线重连/多端 attach）；(b) 媒体生成"生命周期通知"事件（见 D6）。
- 动作（P1）：新增可选的"按 run_id/stream_id 重新 attach 流"端点；`Event` 增补 `media.prompt_draft` / `media.generation_started` / `resource.updated` 三类（对齐 Flova notification）。**不改现有信封结构**。

### D2. Skill 模型
- 现状：`SkillStage{Key,Title,Goal,ToolKeys,DependsOn,PauseAfter,Instruction}`，planner 用编号列表 + `**tool**` + `depends_on:[...]` + `pause_after:true`。
- Flova skill.md：同构（`<planner>` 内阶段+工具+依赖+"何时暂停"）；**额外**在 Skill 语义里表达了：模型偏好、"用户已提供素材不重复生成"、"可选关键帧步骤"。
- 差距：`SkillStage` 无 ①模型栈选择 ②`require_user_media` ③流水线拓扑（是否插首帧、视频/音频并行）。
- 动作（P0，见 §2 数据结构）：给 `SkillStage`/`SkillPlan` 增字段，**保持向后兼容**（omitempty，老 Skill 不写即默认）。

### D3. 发消息/流式端点
- 现状：`server/router.go` 下 `/api/aigc/*`，SSE 流式。
- Flova：`POST /chat`(ack) + `POST /stream_chat/info`(NDJSON 流)。
- 差距：非阻塞点；Dora 单端点可用。可选把"提交"与"取流"拆开以支持重连。
- 动作（P2）：可选，非必须。

### D4. interrupt / resume
- 现状：`InterruptRequestPayload` 已含 checkpoint/版本/`Actions`；router 有 CheckpointMapping + 幂等一次性 resume；resume 带 spec/storyboard 版本。
- Flova：`/chat` 字段 `is_step_mode/is_paused/is_resume/resume_message_id`；message 带 `checkpoint_id`。
- 差距：Dora 已很完整。缺"建议芯片由 LLM 预生成"这层——Dora 有 `Actions` 结构但需确认是否由 skill/agent 主动填充候选项。
- 动作（P1）：让每个 `pause_after` 阶段的 agent 产出 2–3 个 `ActionSchema` 候选（对齐 Flova "确认/调整X/其它"）。纯 prompt/中间件层，不改协议。

### D5. Final Video Spec
- 现状：`FinalVideoSpec` 字段与 Flova **几乎一一对应**，含 `ModelPreference` + `Markdown`（文档形态）+ Version/Status。
- Flova：「文档」标签下 `Final_Video_Spec.md` 可编辑，Model Preference 写死模型选择。
- 差距：几乎无。确认前端是否暴露"文档"视图（可编辑 spec.Markdown）。`ModelPreference` 目前是字符串，建议结构化（见 D2/§2）。
- 动作（P2）：`ModelPreference` 从自由文本升级为结构化 `{image,video,audio}` 模型键；前端补"文档"可编辑视图。

### D6. 媒体生成事件粒度
- 现状：`GenerationJob` + `Worker` + `JobWakeupEvent`（worker 完成回推 session）。事件层有 `EventJobStatus`。
- Flova：`notification` 广播 `media_prompt_draft_update`(含 model_type/tool_type/reference_media_items) → `media_prompt_draft_generation_started` → `resource_update`；前端据此画进度百分比。
- 差距：Dora 目前偏"最终结果"；缺"生成计划草案 + 开始 + 单资源就绪"的细粒度广播。
- 动作（P1）：dispatcher 派发时发 `media.prompt_draft`（含 provider/target/参考资产），worker 起跑发 `media.generation_started`，完成发 `resource.updated`（单 asset 失效信号，前端只重取该 asset）。复用现有 `a2ui.broker`。

### D7. 素材上传
- 现状（已坐实）：`asset/tos_uploader.go` 的 `TOSUploader` 用 `PutObjectV2` **经业务后端中转**上传 TOS（`Uploader.Upload(UploadInput)→UploadResult`）。**无预签名直传、无 sha256 秒传、无上传即分析（FaceDetection/Transcode）。**
- Flova：三段式 `upload_ticket_v2`(sha256 秒传) → PUT GCS → `upload_complete`；上传即触发 FaceDetection/Transcode；每图产"多模态分析卡"。
- 差距：Dora 大文件经后端中转，浪费带宽；无去重；无上传即预处理/分析。
- 动作（P1）：`Uploader` 接口增预签名直传路径（TOS 支持 presign PUT）：后端只发票据+登记元数据，client 直传；入库时按 hash 秒传去重；可选触发"素材视觉分析"产物（用途/构图/光线，喂给 storyboard_designer）。

### D8. 装配 / 时间线 / video_assembler
- 现状（已坐实，修正先前判断）：**`VideoAssemblerTool` 已存在并注册**（`tools/pipeline_tools.go:17,24,89`；`agent/deepseek.go:338`），但是 **demo 占位**——`InvokableRun` 只回 `pipelineToolResult(..., "assembly_plan_ready", ...)`，无真实三轨装配、无 timeline 产物、无导出。另 `tools/write_prompt.go` 也在（对应 Flova"编写提示词"步骤）。
- Flova：`video_assembler` 把 Shot_*_video + Audio_* 铺三轨；时间线三轨（视频/人声/音乐）；导出 视频/PR工程/全部 + 无水印。
- 差距：工具骨架有，**实现是空的**；无 timeline 数据结构/视图；无导出。
- 动作（P1）：把 `VideoAssemblerTool` 从占位实现为真实装配——按 storyboard 顺序把 Shot_*_video 铺视频轨、Audio_VO/BGM 铺人声/音乐轨，产出 timeline 实体；前端补时间线视图 + 导出（视频/PR/全部）。

### D9. 音频 / 音色
- 现状（已坐实）：`generation/handlers/` 只有 `demo_audio.go`（占位，`provider: audio`）。**无真实 TTS、无音色库检索、无 BGM(text_to_instrumental)、无 MiniMax/服务商能力判断。**
- Flova：VO 走"音色库按标签检索选型"（Sweet_Lady/Lyrical Voice），且有"中文需换 MiniMax 服务商"的能力判断；BGM 用 text_to_instrumental；VO+BGM 可并行。
- 差距：音频整条是 demo；无音色选型、无 BGM。
- 动作（P2）：新增真实 TTS handler（`minimax`）+"按 spec 标签检索音色库"；新增 BGM(text_to_instrumental) handler；provider 选择支持语言/能力判断（如中文→MiniMax）。VO+BGM 并行由 §2 的 `Stage.Parallel` 承载。

---

## 2. P0 数据结构改动（唯一需要先定的"契约"，仍不写实现）

给 Skill 增加"题材参数化"能力——**全部 omitempty，向后兼容**：

```go
// skill/parser.go — SkillPlan 增加
type SkillPlan struct {
    // ...现有字段...
    RequireUserMedia bool          `json:"require_user_media,omitempty"` // 商品片=true, 文旅片=false
    ModelStack       ModelStack    `json:"model_stack,omitempty"`        // 该 Skill 默认模型栈
}
type ModelStack struct {
    Image     string `json:"image,omitempty"`      // e.g. "nano_banana" / "gpt_image_2"
    Video     string `json:"video,omitempty"`      // e.g. "seedance" / "happy_horse"
    Audio     string `json:"audio,omitempty"`      // e.g. "minimax"
    FirstFrame bool  `json:"first_frame,omitempty"` // 是否插入 元素→首帧→视频 这一层
}

// skill/parser.go — SkillStage 增加
type SkillStage struct {
    // ...现有字段...
    Parallel  []string `json:"parallel,omitempty"`  // 与哪些 stage 并行(如 video||audio)
    Optional  bool     `json:"optional,omitempty"`  // 路由器可跳过(如首帧仅在 first_frame=true 时)
}
```

对应地：
- `generation`：`Provider` 常量枚举 → 保留，但 handler 注册表按 `ModelStack` 的键选 handler（新增 `gpt_image_2` / `happy_horse` / `minimax` handler）；`TargetType` 增 `shot_first_frame`。
- `mediagraph/generator.go`：把"元素→视频"的固定路径改为读 `SkillPlan.ModelStack.FirstFrame` 决定是否插入首帧节点；读 `Stage.Parallel` 决定视频/音频并行。
- `spec.ModelPreference`：由自由文本升级为 `ModelStack`（或引用之）。

> 这个 §2 是整份计划的"地基契约"：一旦 `SkillPlan` 能声明{是否强制素材、模型栈、是否首帧、并行关系}，D6/D8/D9 都能挂上去，且**换题材=写一个新 Skill.md，不碰 runner/工具/协议**——这正是 Flova 两支片验证的结论。

---

## 3. 优先级路线（建议顺序）

**P0（先立契约，1 步）**
- §2：`SkillPlan/SkillStage` 增字段 + `ModelStack`；`ModelPreference` 结构化。仅类型 + 解析 + 测试，不接线。

**P1（补齐 Flova 已验证的高价值能力）**
1. mediagraph 读 `ModelStack.FirstFrame` 插入"首帧"可选节点；读 `Stage.Parallel` 支持视频/音频并行。
2. 媒体生成细粒度事件：`media.prompt_draft`/`media.generation_started`/`resource.updated`（复用 broker）。
3. `require_user_media` 开关落地：true 时 storyboard 前强制绑定用户素材、不凭空生成；false 时直接生成。
4. `video_assembler` 工具 + 时间线三轨 + 导出。
5. asset 预签名直传 + hash 秒传 +（可选）上传即多模态分析。
6. 每个 `pause_after` 阶段 agent 产 2–3 个 `ActionSchema` 候选芯片。

**P2（锦上添花）**
- 提交/取流解耦端点（重连/多端）；spec「文档」可编辑视图；音色库检索选型 + BGM 生成 + 服务商能力判断；逐消息 credits_info。

---

## 4. 落地节奏（沿用仓库 TDD 约定）

按 `docs/superpowers/plans/` 的逐条 TDD：每条先写失败测试再实现。P0 的类型/解析改动最适合先做（纯单元测试，无外部依赖）。P1 各项独立成 PR，互不阻塞（1→依赖 P0；2/3/4/5/6 可并行）。

> D7/D8/D9 已坐实（见上）：D8 的 `VideoAssemblerTool` 与 `write_prompt` 工具已存在但为占位实现，改造是"填实现"而非"新建"；D7 上传经后端中转、D9 音频为 demo，均需补真实能力。P0 契约不受这三项影响，可先行。

## 5. 复盘：Dora 与 Flova 的骨架差距其实很小
两支片验证下来，Dora 已同构的部分（事件信封 / interrupt+Actions / SkillPlan 阶段-工具-依赖-pause / FinalVideoSpec 含 ModelPreference+Markdown / 异步 job 幂等+status_version / Handler 注册表 / storyboard 乐观锁 / video_assembler+write_prompt 工具骨架）覆盖了 Flova 交互的绝大多数。**缺的不是架构，是三类"可参数化能力"没接线 + 几个工具是占位。** 本计划 P0 把"Skill 参数化模型栈/素材接地/流水线拓扑"的契约立起来，其余按 P1/P2 填实现即可。
