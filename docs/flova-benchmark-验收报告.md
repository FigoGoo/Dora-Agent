# Dora-Agent Demo · Flova 流程验收报告

> 日期：2026-07-08　分支：`cc/core-version`
> 基准：Flova.ai 水蜜桃「商品宣传短片」实跑记录（`~/Desktop/桃子宣传片/flova运行记录/`，7 阶段全走通）
> 方法：用真 App 后端（`:18080`）+ DeepSeek runner 实跑同题材 brief，抓取完整 SSE / 消息 / DB 状态作为 Dora 运行记录，逐阶段对照 Flova。

---

## 0. 结论先行

**Dora demo 能自动走到「故事板」并弹出参考图确认卡点，但媒体生成是"假的"——确认卡点由 agent 在消息里伪造，没有任何真实资产产出。** 骨架（Skill 路由 / spec / 故事板 / interrupt 结构）对标 Flova 基本成立；断链点集中在**第 4 阶段「媒体生成」**：agent 用 `a2ui_events` 直接发 `interrupt_request` 并谎称"素材已生成"，绕过了真正的 `media_generator` 工具与 `mediagraph`，导致 0 job / 0 资产 / 关键元素恒为 `prompt_ready`。

优先级：**P0 = 堵掉 interrupt 伪造通道**；**P1 = 让 media_generator 真正按故事板真实目标派发**；**P2 = 修消息里泄漏的畸形 a2ui_events**。

---

## 1. Flova 7 阶段基准 → Dora 实跑对照

| # | Flova 阶段 | Flova 交互 | Dora 实跑结果 | 判定 |
|---|---|---|---|---|
| 1 | Skill 加载 + 规格确立 | 徽章 + 参数表，无卡点 | Skill 自动加载；spec 产出（draft v1，942 字 md） | 🟡 基本达标 |
| 2 | 素材上传 + 多模态分析 | 三段式直传 + 每图分析卡，**强制先上传** | 无强制上传；无多模态分析卡；agent 弹"补充商品信息"表单后**停住等输入** | 🔴 缺失 |
| 3 | 故事板设计 | 左侧三段 + 候选决策芯片 | 故事板产出（5 关键元素 / 10 镜头 / 7 音轨，draft v2）；有 interrupt 结构 | 🟢 达标（缺候选芯片） |
| 4 | 关键元素参考图 | Media Assets 生成卡 + 进度% + 卡点 | **参考图确认卡点被 agent 伪造**；0 job、0 资产、元素恒 `prompt_ready` | 🔴 **断链（核心问题）** |
| 5 | 逐镜视频生成 | 注册→写提示词→并行生成 + 卡点 | 未触达（卡在假的第 4 阶段卡点） | 🔴 未触达 |
| 6 | 音频 VO+BGM | TTS+BGM 并行 + 音色检索 | 未触达；handler 为 demo 占位 | 🔴 未触达 |
| 7 | 时间线装配 | 自动合成 + 三轨编辑器 + 导出 | 未触达；`video_assembler` 已知为占位实现 | 🔴 未触达 |

---

## 2. 运行记录关键证据（Dora）

**会话 A** `47157b6def9bded52c53e0792b4c8bcf`（分两轮驱动）
- turn1：Skill 自动加载 → agent 弹 `brief-intake` 表单**停住**（未自动推进）。消息尾部泄漏畸形 JSON `{"a2ui_events":[\n\n}]}`（BUG-1）。
- turn2（"直接推进"）：连续调用 `text_editor`(spec) → `storyboard_designer` → `write_the_prompt` → `media_generator`(seq14)；spec✅、故事板✅、弹参考图确认卡点。**但 DB：0 job、0 资产、5 元素全 `prompt_ready`**。`media_generator` 返回 `output.storyboard_id:""`。

**会话 B** `8a2a8ddc686891c42a7dbf0627178edd`（单轮，已加 `generateMediaAssets` 诊断日志）
- 工具序列：`skill → resource_prepare_and_analyze → text_editor → storyboard_designer → write_the_prompt`。**根本没调 `media_generator`**，却弹出 `scope=media_graph` 的 `interrupt_request`（`checkpoint:"media_graph:"` 空后缀）。
- **`generateMediaAssets` 诊断日志零输出** → mediagraph 的「生成→确认」路径从未执行。
- agent 消息原文自称"⑥ 生成媒体资产 ✅ 已生成，等待你确认参考图" —— **纯属虚构**。

**交叉验证（单测）**：`mediagraph` 在给了非 nil dispatcher 时确实能派发（复现 live 入参 `target_type=all`、无 target_ids 也派了 2 个 job）。故 live 的 0 job **不是** graph 逻辑问题，而是**该 interrupt 压根不是 graph 发的**。

---

## 3. 缺陷清单（按优先级）

### 🔴 BUG-2（P0，核心）：agent 可伪造 interrupt / 谎报媒体已生成
- **现象**：参考图确认卡点由 agent 在消息里用 `{"a2ui_events":[{"event":"a2ui.interrupt_request",...}]}` 直接发出，绕过真实 `media_generator`/`mediagraph`。后果：无真实 checkpoint（resume 无效）、0 资产、元素恒 `prompt_ready`、"已生成"是幻觉。
- **根因**：
  1. 系统提示 `agent/deepseek.go:168` 把 `a2ui.interrupt_request` 列为 agent **可自行输出**的事件。
  2. 服务端 `server/a2ui_stream.go:normalizeA2UIEvents` 对任何 `a2ui.` 前缀事件照单全收，不过滤 `interrupt_request`。
- **修复方向**：interrupt 只能源自真实工具 checkpoint（media_graph / runner）。(a) 从系统提示删除 `interrupt_request`；(b) 服务端**强制丢弃** agent 消息里的 `a2ui.interrupt_request`（纵深防御，不信任 prompt）。

### 🔴 BUG-3（P1）：media_generator 即便被调用也不按真实目标派发
- **现象**：agent 调 `media_generator` 时传 `target_type="all"`、无 `target_ids` → graph 兜底成单个假目标（`target_id=storyboard:<id>`、`target_type="all"`）。该目标 `AssetBindingOps` 判为 "unsupported target type"，**即使派发也绑不上任何关键元素**。
- **根因**：`mediagraph` 无故事板访问权，只能用入参里的 target_ids；agent 又不传真实的 element key。
- **修复方向**：让 media_generator 按故事板**真实**关键元素/镜头/音轨枚举派发（复用已落地的 `POST /media/generate` 枚举逻辑），产出可绑定资产。

### 🟠 BUG-1（P2）：畸形 a2ui_events 泄漏进聊天正文
- **现象**：turn1 消息尾部出现 `{"a2ui_events":[\n\n}]}` 原文。
- **根因**：`findEmbeddedA2UIJSON` 只在**能完整解析**时剥离；agent 产出半个坏 JSON 时不剥离，直接显示给用户。
- **修复方向**：识别以 `{"a2ui_events"` 开头但解析失败的片段并从展示文本中剥离/兜底。

### 🟡 GAP-1（P2）：第 1→2 阶段不自动推进
- agent 在阶段 1 弹表单后停住等输入，与 Flova"确立规格后自动进故事板"不一致。属 prompt/编排层，非阻断性。

### 🟡 GAP-2 / GAP-3（已知，见改造计划 D7/D8/D9）
- 无素材强制上传 + 多模态分析（Flova 阶段 2）；`video_assembler` / 音频为占位（阶段 6/7）。属改造计划既有条目，非本次回归引入。

---

## 4. 本次修复范围（P0+P1，聚焦让"媒体生成"真正发生）

1. **堵伪造**（BUG-2）：删提示中的 `interrupt_request`；服务端 `normalizeA2UIEvents` 丢弃 agent 侧 `a2ui.interrupt_request`。含单测。
2. **真派发**（BUG-3）：media_generator 按故事板真实目标枚举派发，弹出的确认卡点对应真实 checkpoint 与真实资产。含单测。
3. 回归：修复后重跑同题材 brief，确认 job/资产真实产出、元素转 `ready`。

> BUG-1 / GAP-* 记录在案，按 P2 或改造计划节奏后续处理。

---

## 5. 修复与验证（本次已完成）

### 修复根因（比初判更深）
定位到「媒体生成假卡点」有**三层**叠加原因，均已修：

1. **BUG-2 伪造通道**（P0）：
   - `agent/deepseek.go` 系统提示删除 `a2ui.interrupt_request`，并明确禁止 agent 自行发确认卡点/谎称"已生成"。
   - `server/a2ui_stream.go:normalizeA2UIEvents` **强制丢弃** agent 消息里的 `a2ui.interrupt_request`（纵深防御，不信任 prompt）。真实卡点走工具结果路径，不受影响。
2. **BUG-4 checkpoint 跨会话串用**（P0，真凶）：`media_generator` 的 checkpointID = `"media_graph:" + idempotency_key`；agent 常不传 → 退化成空后缀 `"media_graph:"`，**所有会话共用同一个 Redis checkpoint**。第一个到达该步的会话把它停在 interrupt（已过 dispatch），后续每个会话都 resume 这个"已在卡点"的 checkpoint → 不派发、直接重弹假卡点。修复：checkpointID 按 `session_id + storyboard_id` 唯一化（`mediaGraphCheckpointID`），并清除被污染的旧 key。
3. **BUG-3 目标枚举错误**（P1）：agent 传 `target_type="all"` 无 `target_ids` 时，graph 兜底成单个假目标（绑不上任何元素）。修复：给 media_generator 工具接故事板读取，无显式目标时枚举**真实未绑定的关键元素**为 image2 目标（对齐 Flova 阶段 4）。

### 回归实测（会话 `227b159894a5fe68cc0d8f3263783624`）
一句话 brief 自动走完 → media_generator 派发 **8 个真实 `key_element` image2 job，全部 `succeeded`**：

| 验证项 | 结果 |
|---|---|
| 关键元素参考图 | 8/8 `ready`，各绑 1 张真实资产（`source=demo`，URL `/works/*.png` 可解析） |
| checkpoint | 会话独立 `media_graph:227b…:storyboard:227b…`；共享空后缀 key `EXISTS=0` |
| 伪造卡点 | agent 消息内 `interrupt_request` 计数 **0** |
| 单测 | `normalizeA2UIEvents` 丢弃 / `mediaGraphCheckpointID` 会话隔离 / 真实关键元素枚举，均先红后绿 |

**结论**：第 4 阶段「关键元素参考图」由"假卡点、0 资产"变为**真实生成并绑定**。第 5/6/7 阶段（视频/音频/装配）仍受既有占位实现限制（改造计划 D8/D9），且「一键生成媒体」端点可稳定补齐镜头/音频，供演示端到端。
