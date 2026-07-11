# 用户 Tool 执行级编排规范

> **⚠ 已并入终版** `2026-07-11-aigc-system-design-final.md`，本文件仅作过程存档，内容可能过期——以终版为准。

> 日期：2026-07-11　状态：`video_creation` 全图先行（粒度样板），其余 5 个待粒度确认后补
> 事实源：①飞书草图图二（异步中心/批次事件/更新故事板/落库的粒度）②flova 拆解 §7/§9——**仅作借鉴查漏，不作对齐基准** ③07-10 稿 §3.5 quorum、§3.7 失败语义、§3.11 事件序列 ④现有代码（mediagraph 节点序、generation job 状态机、wakeup）⑤编排层已拍板定案（8 要素计划、评估点续编、挂起统一、活计划、切面序列、幂等键）

## 通用执行规则（每个用户 Tool 共享，图内不重复画）

**R1 切面**：每个媒体执行节点自动织入——前置 `check_permission → compliance_check → reserve_credits`；后置（worker 侧落库链）`upload_to_storage → register_asset → bind_asset → settle_credits（失败回冲）`，每步幂等、断点重放向前。

**R2 派发-挂起-唤醒循环**（一切媒体执行节点的执行形态）：
```
dispatch_generation（批量建 job；幂等键=计划实例+节点+目标+attempt）
→ 计划 suspended(waiting_jobs)，checkpoint 落盘，本轮 run 正常收尾
→ 【异步中心逐 job】worker → provider → 落库链(R1 后置) → 逐个广播完成/失败事件（前端逐个点亮）
→ 批次终结（全部 job 到达终态）→ wakeup → 计划恢复 running
```

**R3 评估点 ◆ = 回 Agent 续编点**（不问用户），两型处置：

后置质量型（生成批次后）：
| 批次结果 | 处置 |
|---|---|
| 全部成功且无质量标记 | 直接续段 |
| 部分失败 | 失败项补派发（attempt+1），回 R2 循环；重试上限默认 2 |
| 达重试上限仍有缺口 | 带缺口进最近的 ⏸ 交用户处置（跳过该项/继续重试/终止） |
| 合规拦截（provider 拦） | 绝不静默改写——进 ⏸ 告知原因与调整方向，授权后按合规规则重写重试 |

前置可行性型（重动作执行前，如改图可行性）：
| 评估结果 | 处置 |
|---|---|
| 可行 | 续段 |
| 意图歧义 | request_user_input 澄清 → 挂起(waiting_user) |
| 不可行 | 说明原因+替代建议，计划终止（cancelled） |

**R4 卡点 ⏸ 统一三出口**（问用户；载体 request_confirmation）：
```
确认      → 段结束，Agent 续编下一段
调整第 k 项（带反馈）→ write_media_prompt(k, 反馈) → dispatch(attempt+1) → 回本卡点
中途改需求 → 回 Agent：edit_document 产影响评估 → ⏸授权 → 活计划修订（已执行不回滚，未执行重编，被裁节点置 skipped）
```

**R5 认知类节点同步执行**（不走异步中心——只有媒体执行类经 R2）；compliance_check 出 risk_flag → 生成前先过 ⏸ 合规确认。

**R6 深浅裁决与升级**：编排深度由 Agent 分析需求定；浅编排结果不满足时可原地升级为深编排，已产资产复用（活计划续段，不重开会话）。

**R7 卡点开关**：深编排各 ⏸ 默认开；用户可在任一卡点声明"后续 XX 不用再确认"，Agent 续编时按声明去掉对应 ⏸（记录该授权）。**边界：合规 ⏸ 与涉用户素材的 ⏸（如加工方案确认）不可关**——它们是权限性质，不是效率性质。

---

## `video_creation` 视频创作

### 浅编排（直出；命中"把这图做成视频/来段 XX 短视频"）

```
S1 准备（同步）：
   (用户给图) understand_media(图) ─┐
   write_media_prompt(目标=视频；无图时先出帧提示词) ─┴→ compliance_check（flag→⏸）
S2 首帧（仅无图时）：
   护栏前置 → dispatch(t2i) → 【R2 挂起唤醒】→ ◆首帧质量（R3）
S3 成片：
   write_media_prompt(视频动态，引用首帧) → 护栏前置 → dispatch(i2v)
   → 【R2】→ ◆成片质量（R3）
S4 交付：结果陈述 + 导出入口（绑定已由 R1 落库链完成——绑定目标多态：会话交付物）
   用户不满意 → 续段重做（variant，attempt+1）或 R6 升级深编排
```

### 深编排（规划；命中复杂/多单元/需确认的视频需求）

```
D1 素材理解（有素材才有本段）
   (缺素材而用户提到有) request_user_input(上传引导) → 挂起(waiting_user)
   understand_media ×N（同步并行）→ document_store(分析落盘)

D2 规格
   draft_spec(意图+分析+skill 约束) → 产出 open_questions[]
   → ⏸规格确认（open_questions 为候选项；R4 三出口）→ document_store(spec 定版)

D3 脚本
   draft_script(spec+分析) → ⏸脚本确认（R4）→ document_store(script 定版)

D4 故事板
   draft_storyboard(spec+script+分析) → ⏸故事板确认（R4）
   → document_store(定版，结构 ID 生效)
   → bind_asset(用户素材 → 对应元素；"指定为角色则直接绑定复用，不擅自重画")

D5 元素锚点图
   precheck(故事板定版+元素清单) → write_media_prompt ×N → compliance ×N(flag→⏸)
   → 护栏前置(批量预扣) → dispatch(t2i/i2i ×N) → 【R2 并行生成，前端逐张点亮】
   → ◆锚点质量(R3) → ⏸锚点图确认(R4：确认/调整第k张/改需求)

D6 单元帧 → 单元视频（依赖 D5）
   帧：precheck(锚点 ready) → write_media_prompt ×N(参考锚点+前序帧)
       → compliance ×N → dispatch(i2i ×N) → 【R2】→ ◆帧质量
       → ⏸帧确认（默认开，R7 可关）
   视频：write_media_prompt ×N(只写变化与动态) → dispatch(i2v ×N) → 【R2】→ ◆视频质量

D7 音频（依赖 D4，与 D5/D6 并行）
   旁白：write_media_prompt(文本规范化+语气) → dispatch(tts ×段) → 【R2】→ ◆
   配乐：write_media_prompt(风格/情绪/时长) → dispatch(generate_music) → 【R2】→ ◆

D8 装配交付（依赖 D6+D7 全部汇合）
   precheck(齐备性；quorum：单元缺口 → ◆带缺口进 ⏸：跳过该单元/重试/终止)
   → dispatch(assemble_media：时间线+字幕参数[script+tts 时间戳转换]) → 【R2】
   → 交付陈述 + 导出入口（无成片 ⏸；改动走续段）
```

**并行结构**：D5 内 ×N；D6 内 ×N；**D6 ∥ D7**；D1→D2→D3→D4 串行（数据依赖）。
**卡点数**：默认 5（规格/脚本/故事板/锚点/帧）。设计依据是每个 ⏸ 自己的账——**它挡住的下游返工成本必须大于打断成本**：
| ⏸ | 错了废掉什么 | 存在理由强度 |
|---|---|---|
| 规格 | 全部下游 | 最强 |
| 脚本 | 分镜与全部生成 | 强 |
| 故事板 | 全部生成与装配 | 强 |
| 锚点图 | 一致性的根，全部单元 | 强 |
| 帧 | 仅该单元的视频 | **最弱**——故默认开但 R7 可关、可被 Agent 合并 |
Agent 可按需求简单度在编排时合并相邻确认（如规格+脚本一卡）。
**有状态**：整个执行是一个计划实例；每次 ⏸/◆/waiting_jobs 挂起都 checkpoint；跨天续做、断线重连、多端均靠"挂起即释放+唤醒"。

### 自审记录（交付前对照）

- 草图图二逐框核对：素材收集(D1)、故事板 ChatModel→确认→落库(D4)、创建任务→异步中心→批次完成事件→前端渲染→更新故事板(R2+R1 后置)、镜头提示词→图片生成(D6)、聚合→生成视频(D8) ——**全部有落位**
- flova 借鉴查漏（非对齐认证）：其七阶段覆盖的关注点在本图均有自主对应设计，无漏想项
- 07-10 §3.11 事件序列对齐；差异一处：图内 interrupt → **计划级挂起**（挂起统一模型），属既定改造
- 现有代码差异（改造点）：mediagraph 预定义图 → 计划实例；其节点序（register→prompts→generate→confirm）被 D5/D6 段结构吸收
- 五特性：并行(D5/D6/D7)✓ 合并(D8 quorum)✓ 中断/恢复(⏸+挂起+checkpoint)✓ 逻辑判断(◆+R6 裁决)✓ 有状态(计划实例)✓

---

## `image_creation` 图片创作

```
浅（单张/几张）：
S1 (有参考图) understand_media → write_media_prompt ×N → compliance_check(flag→⏸)
S2 护栏前置 → dispatch(t2i/i2i ×N) → 【R2】→ ◆图质量(R3 后置型)
S3 交付：陈述（无⏸）；不满意 → 带反馈续段 attempt+1

深（一致性系列）：
D1 (有参考) understand_media → document_store(分析落盘)
D2 锚点：write_media_prompt(锚点，特征写满) → compliance_check → 护栏前置
   → dispatch(t2i/i2i) → 【R2】→ ◆锚点质量 → ⏸锚点确认（账：锚点错→整系列废，强）
D3 扩展：write_media_prompt ×N(引用锚点) → compliance_check ×N → dispatch(i2i ×N 并行)
   → 【R2】→ ◆系列一致性（单张错只废单张，不设⏸）
D4 集合交付
```

## `image_edit` 图片加工

```
S1 understand_media(原图+用户改法) → ◆改法可行性（R3 前置型：可行/澄清/不可行终止）
S2 write_media_prompt(原图特征+改法) → compliance_check → 护栏前置
   → dispatch(i2i 参考原图) → 【R2】→ ◆改动效果(R3 后置型)
S3 交付新 variant（原图只读永不覆盖）；不满意 → 带反馈续段
（全程无⏸：错误成本=单图重做；edit_image 格子实现后 S2 换真局部编辑）
```

## `video_edit` 视频加工

```
S1 (未传视频) request_user_input(上传引导) → 挂起(waiting_user)
   用户视频入资产（三段式上传，原件只读）
   (用户没给文本时) draft_script(字幕/旁白文本)
S2 ⏸加工方案确认（账：动用户素材+方案歧义空间大，必过人；R7 不可关）
S3 按需并行：dispatch(tts 配音) ∥ dispatch(generate_music 配乐) → 【R2】→ ◆音轨质量
S4 dispatch(assemble_media：原视频+新轨+字幕+clip/overlay) → 【R2】
   → 交付新资产（无成片⏸：装配确定性、错误已在上游卡过）
```

## `music_creation` 音乐创作

```
S1 write_media_prompt(风格/情绪/结构/时长) → compliance_check → 护栏前置
S2 dispatch(generate_music) → 【R2】→ ◆(R3 后置型)
S3 交付（无⏸）；不满意 → 带反馈 attempt+1
（generate_song 格子实现后并入：draft_script(歌词) → ⏸歌词确认（账：词错整首废，强）
 → dispatch(generate_song)）
```

## `audio_creation` 音频创作

```
S1 (需要我们写稿) draft_script → ⏸稿件确认（账：稿错全部段废，强；用户自带稿整段跳过）
   write_media_prompt ×段(文本规范化+语气标注) → compliance_check
S2 护栏前置 → dispatch(tts ×段并行) → 【R2】→ ◆段质量(R3 后置型)
S3 多段 → dispatch(assemble_media 拼接) → 【R2】→ 交付；单段直接交付
```

## 全集自审记录（2026-07-11 二轮审查）

一轮自审后用户追问触发二轮交叉审查，抓出并修正 4 处：①R3 评估点定义过窄（只覆盖批次质量）→ 扩为两型（后置质量/前置可行性）；②video_edit 缺"未传视频→request_user_input"分支 → 补；③交付段 bind_asset 冗余（R1 落库链已含绑定）→ 全集统一为"交付=陈述+导出"，video_creation S4 同步订正；④R7 可关卡点无边界 → 合规⏸与涉用户素材⏸不可关。
全集核对：六图仅用 v1 已实现工具；每 ⏸ 带成本账；每 ◆ 标注 R3 型别；五特性全部落位（并行/合并/中断恢复/逻辑判断/有状态）。
未验证：全部为设计初值，未经真实会话检验；命中区分度（M2）未压测。
