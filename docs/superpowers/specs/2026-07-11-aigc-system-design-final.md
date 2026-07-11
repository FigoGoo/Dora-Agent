# Dora AIGC 创作体系设计（终版 v1）

> 日期：2026-07-11　状态：设计定稿（实施计划另出）
> 本文聚合并取代三份中间文档：`2026-07-11-aigc-atomic-tools-v1.md`、`2026-07-11-aigc-orchestration-layer.md`、`2026-07-11-aigc-user-tools-orchestration.md`（原文件保留作过程存档，以本文为准）。
> 事实源：飞书草图（Tool 设计，原子/用户两级 + 两张流程图）、flova 拆解（**仅借鉴查漏，非对齐基准**）、07-10 工具体系设计稿（竞品逆向产物，同前）、现有代码（`internal/aigc/*`）、本轮逐项拍板记录。

---

## 0. 设计理念

1. **真正的 AIGC = 分析用户需求目的，然后动态编排。** 不预定义流程——固定流程就是 Dify（人画好工作流，LLM 只是节点）；Dora 里 LLM 是编排者，执行图是 Agent 每次按需求现场生成的产物。
2. **只有方向，没有场景。** 按四个模态方向（视频/图片/音乐/音频）组织能力；场景无穷且易变，一律由 Skill 以领域经验（软知识，非硬流程锁）承载。场景化的东西只允许从沉淀里长出来，不允许初始预置。
3. **原始 Tool 层是唯一固定物**——编排的全部词汇表；四方向 ×（生成/编辑/装配/理解）的完备性 = 编排自由度上限。能力进池由方向完备性决定，v1 实现与否只是排期。
4. **用户 Tool = 编排产物**：库里沉淀的编排模板（含用户 @ 直呼的预工具），不是 Agent 注册面；用户感知编排的进度流，不关心内部运转。
5. **业务保障以切面绑定在原始工具上**（不绑图）：图形状随便变，调到工具，权限/扣费/入库/绑定必然发生。
6. **层次原则**：原始工具层 = 创作域词汇表；编排域元操作（组计划/校验计划/沉淀计划）永不进入原子层。

---

## 1. 原始 Tool（原子层：创作词汇表）

### 1.1 通用契约

三要素（tool_key / tool_name / tool_description，面向编排层与开发者，永不直接面向最终用户）；`run(ctx, inputs) → outputs | fail`（ctx 由 runtime 注入）；无状态；输入输出只传引用/结构化小对象，媒体字节永不过手；工具互不调用（数据由编排层路由）；不发事件（runtime 统一广播）；写操作幂等（同键重放返回同一结果）。

### 1.2 认知类（6）——调一次 ChatModel，产一个认知产物

| tool_key | tool_name | 什么情况下调用 | 边界 |
|---|---|---|---|
| `draft_spec` | 规格起草 | 新创作项目确立/改方向重立规格 | 不落库不确认；产出含"需用户裁决项"清单 |
| `draft_script` | 脚本创作 | 需先产文本内容（故事/歌词/文案/解说词） | 不落库不分镜 |
| `draft_storyboard` | 故事板起草 | 把创作意图落成结构化蓝图（元素/输出单元/音频层） | 不落库不生成媒体 |
| `edit_document` | 文档修订 | 对既有文档提修改（含翻译/改写类变换） | 只产 JSON Patch+影响提示，不应用 |
| `write_media_prompt` | 提示词编写 | 任何生成派发前为目标备提示词 | 模板按目标类型分发（可扩展，skill 可注入规则）；不选模型不发起生成 |
| `understand_media` | 素材理解 | 上传/引用素材后需看懂才能规划复用 | 接口全模态，v1 实现 image；只读不产优化版 |

### 1.3 媒体执行类——四模态方向组织，每方向"生成→编辑→装配"三段齐

共同边界：执行工具产出 **provider 产物句柄（provider_ref，仅在异步中心 worker 进程内流转的临时引用）+ 摘要**；正式 `resource_id` 由落库链 upload_to_storage 物化产生。"媒体字节永不过手"指字节不进图状态/Agent 上下文/事件面——worker 进程内流转是唯一合法通道。绝不向上层返回字节/base64/provider 原始负载；不扣费不注册不绑定（切面做）。Provider 适配器在本层之下（模型栈配置，非工具）。

| 方向 | v1 实现（✅） | 留格子（▢） |
|---|---|---|
| 图片 | `text_to_image`（image2 已有）、`image_to_image`（image2 可覆盖） | `edit_image`（局部编辑，v1 以 i2i 近似）、`compose_image`（图文排版）、`super_resolution` |
| 视频 | `image_to_video`（seedance 已有） | `text_to_video`、`multimodal_to_video`（参考驱动为入参能力）、`audio_driven_video`、`images_to_video`、`video_interpolate`、`video_to_video`（风格化重绘——视频的编辑段）、`extract_keyframes` |
| 音乐 | `generate_music`（器乐；参考续写为入参能力） | `generate_song`（能力池必备，排期待定） |
| 音频 | `text_to_speech`（demo 占位；音色 v1 预设表；**输出含分段/句级时间戳**——字幕与对齐的数据源） | `generate_sound_effect`、`clone_voice`（音色资产）、`separate_audio_tracks` |
| 跨方向装配 | `assemble_media`（时间线类：排轨/对齐/混音/渲染；输出视频或音频参数化；须含 clip in/out、叠加轨 overlay；字幕作输入参数；单输入退化形态即格式转换） | （空间排版归 compose_image） |

### 1.4 数据类（3）——按数据域收拢，op 作参数

| tool_key | 收拢范围 | 关键约束 |
|---|---|---|
| `asset_store` | 入库(对象存储+resource)/注册/variant/favorite/绑定/软删 | **绑定写独立绑定表**；绑定目标多态（文档结构节点/计划产出槽/会话交付物）；软删是唯一删除入口、仅响应用户明确指令；重生成永不覆盖 |
| `document_store` | 定版/应用 patch/分析落盘 | 版本单调递增、结构 ID 定版生效；patch 走 base_version 乐观锁；分析域只追加 |
| `dispatch_generation` | 批量建 job 入队；**同时预注册资产记录（generating 态）**——前端即刻有占位 | 幂等键 = 计划实例+节点+目标+**派发序号 attempt**：任何重派（失败重试或用户调整）一律递增，杜绝同键吞掉新内容；失败重试上限用独立 retry 计数判断，用户调整不消耗重试预算 |

双宿主库函数（图节点/异步 worker）；用户直接操作入口仅两个（手动编辑文档 patch、前端删除按钮）。纪律：落库链幂等重放向前（每步幂等/断点重放/不回滚）；写权限按"入口×写域"白名单校验；写操作携带 actor 审计上下文。

### 1.5 护栏类（5）——fail 是决策输入不是异常

| tool_key | 职责 |
|---|---|
| `precheck_inputs` | 校验计划节点声明的输入引用真实就绪（资产 ready/文档版本存在）；**不含业务顺序规则**（那属 Skill 软知识+Agent 编排判断） |
| `check_permission` | 消耗动作前的权限/配额/provider 可用性（无副作用） |
| `compliance_check` | 合规预检，两级：**硬拦**（禁区类，直接拒绝，不可授权翻案）/**软拦**（风险线索，进 ⏸ 由发起用户授权调整方向后重写重试）；绝不静默改写绕过；v1 规则表起步 |
| `reserve_credits` / `settle_credits` | 预扣 / 结算回冲；**v1 空实现占位**（接计费不动图） |

### 1.6 交互类（2）

`request_confirmation`（封闭选项确认卡）/ `request_user_input`（开放收集卡：上传引导/偏好/自由输入）。

### 1.7 明确不设与非工具

不设：检索类整个（库内/外部检索，未来按新增大类）；进度通知卡（通知归事件广播，卡点=挂起等人）。
非工具（设施/机制）：异步生成中心、事件广播、Skill/Tool 中间件、意图识别（Agent 本体）、Provider 适配器、上传三段式（数据接口）、编排域元操作（库检索注入/计划校验/沉淀参数化）。

**合计：v1 实现 22，留格子 14，总盘子 36。**（经 17 路径对抗压测修订；教训在案：结论自洽 ≠ 路径可达。）

---

## 2. 用户 Tool（编排库初始模板）

### 2.1 定位

编排好的产物：编排库冷启动内容，基于原始工具词汇表编排而成，用户可直接使用。三要素同原始工具（description 即命中判定依据）。**不是 Agent 注册面。** 使用双通道：①需求命中（Agent 分析后取库绑参）②用户 **@ 直呼**——跳过的是"选哪个用户 Tool"的命中判定（强制命中）；实例化仍经 Agent（深浅裁决、参数补齐、缺参走收集卡点）。
**初始库 = 分类级（模态方向 × 创作/加工，平台中性）；沉淀库 = 可以具体**（场景化模板只能从使用沉淀里长出来）。

### 2.2 初始库（6 个 + 2 个待格子）

| tool_key | tool_name | tool_description（命中依据） |
|---|---|---|
| `image_creation` | 图片创作 | 从想法/文本/参考生成图片；单张/多张/一致性系列皆为参数 |
| `image_edit` | 图片加工 | 改已有图：换背景/改风格/去元素/局部重绘（v1 以 i2i 近似） |
| `video_creation` | 视频创作 | 从想法/文本/素材创作视频；**深浅是同一工具的两种编排**（直出 vs 规划），由 Agent 按需求定 |
| `video_edit` | 视频加工 | 给已有视频加字幕/配乐/配音/裁剪拼接 |
| `music_creation` | 音乐创作 | 按风格/情绪/时长生成音乐（歌待 generate_song 格子并入） |
| `audio_creation` | 音频创作 | 文本变语音（旁白/播报/多段拼接） |

（音乐加工、音频加工：依赖格子工具，随格子实现入库。）

**M1 需求分析框架 = 需求→产物矩阵**：行=用户带来物（只有想法/文本/图/音频/视频/平台产物），列=想拿走的产物方向。分析产物四要素：产出方向、带来物、编排深浅、约束偏好——直接充当计划元信息与 M2 命中输入。

### 2.3 通用执行规则 R1–R7（六图共享）

- **R1 切面**：媒体执行节点自动织入——前置 `check_permission → compliance_check → reserve_credits`；后置（worker 侧落库链）`upload_to_storage(物化 resource) → asset_store(置 ready/挂 variant) → bind_asset → settle_credits(失败回冲)`，每步幂等、断点重放向前。**切面与数据/护栏工具只由 runtime 织入，不进 Agent 编排词汇表、不可作计划节点**——杜绝双重织入。
- **R2 派发-挂起-唤醒**（一切媒体执行的形态）：`dispatch_generation`（批量 job + 预注册 generating 资产）→ 计划 suspended(waiting_jobs)、checkpoint、本轮 run 收尾 → 异步中心逐 job：worker→provider→落库链→逐个广播事件（前端逐个点亮）→ 批次终结 → wakeup → 计划恢复。**活性保障**：job 带执行超时（超时置 failed 计入批次终结）；批次看门狗周期核对 suspended(waiting_jobs) 计划的批次真实状态并补发唤醒（wakeup 崩溃丢失的兜底）；唤醒幂等。
- **R3 评估点 ◆ = 回 Agent 续编点**，两型：后置质量型（全成→续段｜部分失败→补派发 attempt+1，上限 2｜达上限→带缺口进 ⏸｜provider 合规拦→进 ⏸ 告知原因与调整方向，经用户授权改向后重写重试；硬性禁区不适用授权，直接终止该目标）；前置可行性型（可行→续｜歧义→request_user_input 澄清｜不可行→说明+替代，计划 cancelled）。
- **R4 卡点 ⏸ 三出口**：确认→续段｜调整第 k 项(带反馈)→重写重派→回本卡点｜中途改需求→影响评估→授权→活计划修订（已执行不回滚，被裁置 skipped）。
- **R5 认知类同步执行**（只有媒体执行经 R2）；失败语义：ChatModel 调用失败/坏输出自动重试 ≤2，耗尽 → 计划 failed 并告知原因；compliance 软拦 flag → 生成前过 ⏸，硬拦直接拒绝。
- **R6 深浅裁决与升级**：深浅由 Agent 定；浅结果不满足可原地升级，已产资产复用。
- **R7 卡点开关**：深编排 ⏸ 默认开，用户可声明关闭后续某类确认；**边界：合规 ⏸ 与涉用户素材 ⏸ 不可关**（权限性质非效率性质）。

### 2.4 六图（符号：⏸ 问用户｜◆ 回 Agent｜×N 批量并行｜∥ 分支并行）

**图的读法**：图中 `(有图)/(无图)` 类条件是**模板的实例化注记**，不是计划内分支——模板是编排知识，Agent 实例化时按实际输入消解条件，产出的执行计划是**无分支的纯 DAG**（§3.5 定案的落实）；图中 dispatch/document_store/护栏字样是机制可视化（R1/R2 自动发生），非 Agent 编排的节点。

**`video_creation` 浅编排**
```
S1 (有图时)understand_media → write_media_prompt(引用理解结果) → compliance(flag→⏸)
S2 (无图)护栏前置 → dispatch(t2i) → 【R2】→ ◆首帧质量
S3 write_media_prompt(视频动态) → 护栏前置 → dispatch(i2v) → 【R2】→ ◆成片质量
S4 交付：陈述+导出（绑定已由 R1 完成，目标=会话交付物）；不满意→续段 attempt+1 或 R6 升级
```

**`video_creation` 深编排**
```
D1 素材理解：(缺素材)request_user_input→挂起 | understand_media ×N → document_store(分析)
D2 规格：draft_spec(含 open_questions) → ⏸规格确认 → document_store(定版)
D3 脚本：draft_script → ⏸脚本确认 → document_store(定版)
D4 故事板：draft_storyboard → ⏸故事板确认 → document_store(定版,结构ID生效)
          → bind_asset(用户素材→元素；指定即绑定复用，不擅自重画)
D5 元素锚点图：precheck → write_media_prompt ×N → compliance ×N(flag→⏸) → 护栏(批量预扣)
          → dispatch(t2i/i2i ×N) → 【R2 逐张点亮】→ ◆锚点质量 → ⏸锚点图确认
D6 单元帧→单元视频（依赖 D5）：
   帧：precheck → write_media_prompt ×N(参考锚点，**默认不依赖前序帧、全并行**) → compliance ×N
       → dispatch(i2i ×N) → 【R2】→ ◆帧质量 → ⏸帧确认(默认开,R7 可关)
       （需强跨帧连续性时由 Agent 编排为分波次——依赖边表达，牺牲并行度换连贯）
   视频：write_media_prompt ×N(只写变化动态) → dispatch(i2v ×N) → 【R2】→ ◆视频质量
D7 音频（依赖 D4，∥ D5/D6）：旁白 write_media_prompt→dispatch(tts ×段)→【R2】→◆
                            配乐 write_media_prompt→dispatch(generate_music)→【R2】→◆
D8 装配交付（依赖 D6+D7 汇合）：precheck(齐备性;quorum:缺口→◆带缺口进⏸:跳过/重试/终止)
          → dispatch(assemble_media:时间线+字幕[script+tts 时间戳转换]) → 【R2】
          → 交付陈述+导出（无成片⏸；改动走续段）
并行结构：D5 内×N；D6 内×N；D6∥D7；D1→D2→D3→D4 串行。
```

**卡点成本账**（每个 ⏸ 的存在依据 = 挡住的下游返工成本 > 打断成本）：

| ⏸ | 错了废掉什么 | 强度 |
|---|---|---|
| 规格 | 全部下游 | 最强 |
| 脚本 | 分镜与全部生成 | 强 |
| 故事板 | 全部生成与装配 | 强 |
| 锚点图 | 一致性的根，全部单元 | 强 |
| 帧 | 仅该单元视频 | 最弱——默认开但可关可合并 |

浅编排零卡点（错误成本=一次重生成，⏸ 账立不住）；成片不设卡点（装配确定性、错误已在上游卡过、"行不行"只有看到成片才知道）。

**`image_creation`**
```
浅：S1 (有参考)understand_media → write_media_prompt ×N → compliance(flag→⏸)
    S2 护栏前置 → dispatch(t2i/i2i ×N) → 【R2】→ ◆图质量
    S3 交付；不满意→带反馈续段
深（一致性系列）：D1 (有参考)understand_media → document_store
    D2 锚点：write_media_prompt(特征写满) → compliance → 护栏 → dispatch → 【R2】
        → ◆锚点质量 → ⏸锚点确认（账：锚点错→整系列废）
    D3 扩展：write_media_prompt ×N(引用锚点) → compliance ×N → dispatch(i2i ×N) → 【R2】
        → ◆系列一致性（单张错只废单张，无⏸）
    D4 集合交付
```

**`image_edit`**
```
S1 understand_media(原图+改法) → ◆改法可行性（前置型：可行/澄清/不可行终止）
S2 write_media_prompt(原图特征+改法) → compliance → 护栏 → dispatch(i2i 参考原图)
   → 【R2】→ ◆改动效果
S3 交付新 variant（原图只读永不覆盖）；不满意→带反馈续段。全程无⏸
```

**`video_edit`**
```
S1 (未传视频)request_user_input→挂起 | 视频入资产(三段式,原件只读) | (没给文本)draft_script
S2 ⏸加工方案确认（动用户素材必过人；R7 不可关）
S3 按需：dispatch(tts)∥dispatch(generate_music) → 【R2】→ ◆音轨质量
S4 dispatch(assemble_media:原视频+新轨+字幕+clip/overlay) → 【R2】→ 交付新资产（无成片⏸）
```

**`music_creation`**
```
S1 write_media_prompt(风格/情绪/结构/时长) → compliance → 护栏
S2 dispatch(generate_music) → 【R2】→ ◆
S3 交付（无⏸）；不满意→带反馈 attempt+1
（generate_song 格子后并入：draft_script(歌词)→⏸歌词确认→dispatch(generate_song)）
```

**`audio_creation`**
```
S1 (需写稿)draft_script → ⏸稿件确认（用户自带稿跳过）| write_media_prompt ×段 → compliance
S2 护栏 → dispatch(tts ×段并行) → 【R2】→ ◆段质量
S3 多段→dispatch(assemble_media 拼接)→【R2】→交付；单段直接交付
```

---

## 3. 动态编排

### 3.1 主流程（方向性总纲）

```
用户需求 → Agent 分析需求目的（M1）
              ↓
       检索编排库（M2）
        ├─ 命中 → 取模板绑参 → 执行计划
        └─ 未命中 → LLM 从词汇表现场编排（M3）→ 执行计划
              ↓
       同一个 runtime 执行（不区分计划来源）
              ↓
       成功且有复用价值 → 沉淀回库（M6）
              ↑ 库越用越厚，永远有动态编排兜底（不是 Dify 的根本原因）
```

### 3.2 M1 需求分析（Agent 本体）

产物四要素（产出方向/带来物/深浅/约束偏好，见 §2.2 矩阵）。反问边界：**必问**——定位不了矩阵格子、提到素材没给；**不问用默认**——能被后续卡点兜住的细节偏好（原则：反问有打断成本，卡点能兜的不提前问）；完全无方向→引导话术介绍四方向能力。

### 3.3 M2 库命中

v1 库小：目录整体注入上下文，Agent 直接判（不建检索服务）。三档处置：明显命中→直取绑参并说明｜模糊两可→出候选让用户选（不替用户猜方向性决策）｜不命中→兜底动态编排。**"永不拒绝"指路由层永远有 M3 兜底（不存在"库里没有"式拒绝）；M3 编排本身可能失败并诚实告知（§3.4），失败是合法终点。** @ 直呼旁路跳过 M1/M2 的命中判定。

### 3.4 M3 动态编排机制

- **词汇表注入**：**可编排词汇表 = 认知 6 + 执行 6 + 交互 2 = 14 个**（三要素+入参出参概要）+ 6 模板目录 + 激活 skill + M1 四要素。数据 3 与护栏 5 属切面/机制层，**不注入、不可作计划节点**（R1）；留格子不注入（格子实现自动进入）。复用 turncontext 瞬态注入机制。
- **计划生成**：产出 8 要素计划（结构化输出）；**一切执行皆计划实例**（单节点也是），执行面零分叉；"提交计划"是编排域元能力（不进原子层）。
- **合法性校验**（runtime 提交时同步，五查）：结构（无环/工具存在且已实现/边指向真实节点）、参数（必填绑定/类型匹配/槽已实例化）、权限（入口×写域白名单）、保障（切面可织入）、预算（v1 以**生成 job 数阈值**计——credits 空实现期无金额可算，接计费后升级为金额；超阈值不拒绝→转预览 ⏸）。
- **修复循环**：结构化错误回 Agent → 修计划重提 → 上限 2 次 → 仍败则诚实告知编排不出+原因，不硬跑。
- **计划预览 ⏸**：预算超阈值或深编排首提 → 先给用户看"打算怎么做、预计消耗"；轻计划直接跑。

### 3.5 M4 执行计划（8 要素）

元信息（来源/需求摘要/方向）｜节点集（工具引用+参数绑定：字面值/上游引用/参数槽）｜依赖边（纯 DAG）｜批量展开｜卡点声明（问用户）｜评估点声明（回 Agent）｜预算预估｜成功判据。
**分支兑现（定案）：计划内不放 if/else，分支 = 评估点 + Agent 续编（分段编排）。** 理由：判断留给 LLM 才是"LLM 是编排者"的彻底版；计划保持纯 DAG；已知代价是多几次 LLM 调用，换判断质量与架构简单。

### 3.6 M5 runtime 执行：计划实例状态机

```
draft（Agent 编排中，未提交）→ running ⇄ suspended(waiting_user/agent/jobs)
draft / running / suspended → cancelled（用户拒绝或终止）
running → succeeded / partial_succeeded / failed
节点/展开实例：pending → running → succeeded / failed(重试耗尽) / skipped(修订被裁)
```
**计划预览 = suspended(waiting_user)**（受挂起统一模型管辖，checkpoint/唤醒同一套）；draft 仅指未提交。
① 挂起统一：一个 suspended + 三原因（waiting_user/waiting_agent/waiting_jobs），checkpoint/唤醒/展示一套逻辑。
② 活计划：续编=追加；修订=改未执行（裁掉置 skipped）；已执行永不回滚；终态=无未执行且无挂起。
③ 挂起即释放：不占运行时资源，当轮 run 正常收尾；三种唤醒对号（用户 action/Agent 续编/job 批次 wakeup——复用现有 wakeup）。
checkpoint/resume：挂起必 checkpoint（计划+节点状态+产物引用+文档版本快照）；resume 幂等一次性；恢复时版本冲突检测（变了不硬续，转确认）。
**终态映射**：succeeded=全部节点成功｜partial_succeeded=quorum 通过但存在 skipped/缺口（用户选择跳过后交付）｜failed=M3 修复耗尽/认知类重试耗尽/批次全败且用户放弃｜cancelled=预览拒绝/前置不可行终止/用户主动终止。
**并发约束（v1）**：同一会话同一时间**一个活动计划**（running/suspended）；活动计划挂起期间的新需求由 Agent 判断并入当前计划（续编）或排队；多计划并发调度列入 v2。

### 3.7 M6 沉淀（v1 简化）

动态编排成功后 Agent 标记"可复用候选"，**用户确认才入库**（不自动沉淀）；参数化机制后置。

---

## 4. 异步任务（异步生成中心）

- **定位**：共享基础设施（非工具非节点）；一切媒体执行的唯一执行环境（执行类工具唯一入口 = worker）。
- **执行形态**：R2 循环（见 §2.3）。job 幂等键 = 计划实例+节点+目标+attempt。
- **job 状态机**：queued → running → succeeded / failed / cancelled（既有代码）+ **执行超时**：running 超时置 failed 计入批次终结——活性保障，新增；回调按 job_id + status_version 幂等。
- **批次语义**：逐 job 完成即广播事件（前端逐个点亮）；**批次终结**（全部 job 终态）才 wakeup 计划恢复——批次结果交 ◆ 按 R3 处置（含 quorum 部分成功）。**批次看门狗**：周期扫描 suspended(waiting_jobs) 计划、核对批次真实状态、补发丢失的唤醒（服务崩溃窗口的兜底）；唤醒幂等。
- **与计划状态机衔接**：派发即计划 suspended(waiting_jobs)；job 寿命可跨 run；唤醒复用现有 wakeup 机制（会话级事件流，不绑 run）。
- 现有代码基础：`generation/` Dispatcher→Redis Queue→Worker（并发 4）+ wakeup——存量最成熟，delta 为批次终结判定与计划挂起对接。

---

## 5. 资产生成和绑定

- **三层模型**：文档结构（element/unit/audio_layer）→ asset（逻辑资产：variants[]/favorite/source）→ resource（物理文件@对象存储）。
- **绑定独立表**（拍板）：故事板文档只存创作结构（人改的），绑定/生成状态是系统事实（机器写的）——写路径分离不撞锁；前端状态树投影时合成两域。绑定目标多态：文档结构节点/计划产出槽/会话交付物。
- **结构变更与在途生成的协调**：文档改版期间在途 job 照常完成；绑定时若目标结构 ID 已不存在 → 产物归入**孤立产物区**并进 ⏸ 告知用户处置（重新挂载/丢弃）；计划恢复时版本冲突检测照旧。
- **落库链**（R1 后置，worker 侧）：upload_to_storage(物化 resource) → asset_store(置 ready/挂 variant) → bind_asset → settle_credits；**幂等重放向前**（每步幂等/断点重放继续完成/绝不回滚，孤儿 resource 靠 v2 对账）。
- **资产生命周期**：**派发时预注册**（registered/generating，前端占位）→ ready →（用户确认）confirmed；重生成/调整 = 并列 variant（新派发序号），**favorite 不自动切换**——变更 favorite 是用户显式动作，已确认资产的 favorite 锁定（"未经授权不得替换"的物理兑现）；用户上传原件永久只读；删除唯一入口 = 软删 + 用户明确指令。
- **权限物理化**：写权限按"入口×写域"白名单在计划校验时强制（skill 改不了、Agent 绕不过的物理层）。
- **审计**：一切写操作携带 actor 上下文（session/run/计划实例/节点/job_id）。

---

## 6. 实施范围与遗留

### 6.1 v1 实施范围（五块 delta）

| 块 | 现有基础 | 实现 delta |
|---|---|---|
| 原始 Tool | image2/seedance/demo audio；write_prompt/text_editor/storyboard_designer 可改造 | 认知 6 统一契约重写；assemble_media、generate_music 新写；数据 3 收拢+绑定表新建；护栏 5（compliance 新写、credits 空占位）；交互 2 复用 interrupt 链路 |
| 用户 Tool | mediagraph 预定义图（作废重构，节点序被吸收） | 编排库存储+目录注入；6 模板计划模板数据化 |
| 动态编排 | 无 | Agent 注册面改造、计划生成与校验器、评估点续编循环 |
| 异步任务 | `generation/` 全链 + wakeup（最成熟） | 批次终结判定、计划挂起/唤醒对接 |
| 资产生成和绑定 | asset store、TOS、patch 乐观锁 | 绑定表建模、落库链幂等重放改造、actor 审计 |

### 6.2 拍板记录（2026-07-11 全部按建议定案）

M1 反问边界=能被卡点兜住的不提前问；M3 修复循环上限=2 次；计划预览触发=预算超阈值或深编排首提；§3.6 计划状态机三条语义（挂起统一/活计划/挂起即释放）全部生效。**设计阶段无遗留待拍板项。**

### 6.3 已知未验证

全部编排图为设计初值，未经真实会话检验；6 条命中 description 区分度未压测；Eino compose 承载动态计划的技术可行性未验证（实施计划首个技术风险项）。

### 6.4 深度审查记录（2026-07-11 终审）

双通道审查（作者逐维核对 + 独立对抗评审）发现 20 项实质问题并全部修复入档。P0×4：异步链无活性保障（job 超时/批次看门狗补入）、生成字节→存储通道未定义（provider_ref 语义补入）、资产注册时机矛盾（派发时预注册定案）、用户调整重派被幂等吞掉（派发序号与重试预算分离）。P1×7：切面双重织入（可编排词汇表收敛 14）、结构变更绑定悬空（孤立产物区）、模板分支与纯 DAG 矛盾（实例化注记读法）、@直呼语义、认知类失败路径、D6 前序帧依赖、"永不拒绝"表述。P2×7 与 P3×2：预览状态归属、预算数据源、幂等键组分、favorite 归属、tts 时间戳、终态映射、合规授权主体、M5 编号、多计划并发约束。

### 6.5 明确不做（v1）

检索类工具；自动沉淀；跨项目深查询；图嵌套图；多 skill 混排；多计划并发调度（v1 单会话单活动计划）；多人协同编辑；分发域（发布到第三方平台）。
