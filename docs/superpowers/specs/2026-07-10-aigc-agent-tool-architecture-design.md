# AIGC 创作 Agent 工具体系设计（Graph Tool × AGUI）

> 日期：2026-07-10　状态：设计稿（未实现）
> 范围：**通用 AIGC 创作 Agent 平台**的工具体系——脱离任何具体产品实现，适配"故事视频 / MV / 商品片 / 纪录片"等任意创作场景。
> 依据：Flova.ai 实测拆解（对话实录 + 接口逆向记录）+ 本次设计讨论定案。

---

## 0. 设计目标与第一性原则

**范式：工具固定，编排动态。** 一批预定义原子能力不变；"这次调哪些、什么顺序、串并行、走哪条内部路径"由 Agent 按需求 + 输入 + Skill 每次动态决定。

五条第一性原则（全文一切细节由此推出）：

| # | 原则 | 理由 |
|---|------|------|
| P1 | **注册给 LLM 的工具越少越精准** | 每个工具 schema 都进上下文：工具多 → token/延迟线性涨、决策面大 → 选错率高、首 token 慢。 |
| P2 | **注册面只有 Graph Tool** | 用户必须能感知"流程、工具、状态"；只有流程型工具能持续对外广播节点状态。 |
| P3 | **业务逻辑与能力逻辑分离，业务不靠 Agent 自觉** | 扣费/权限/写表这类"必须发生"的事不能依赖 LLM 记得调；业务混进能力工具则边界不清、逻辑臃肿。 |
| P4 | **前端零业务，一切交互走 AGUI 事件面** | 前端是共享状态树的纯投影；不存在"后端做了事前端不知道"。 |
| P5 | **新增能力优先加参数/内部路径，证明不行才允许加工具** | 守住 P1 的注册面；能力膨胀吸收进胖工具内部。 |

---

## 1. 分层总览

```
┌─────────────────────────────────────────────┐
│ 前端（纯状态投影 · 零业务逻辑）                  │
└──────────────┬───────────────▲──────────────┘
        SSE 事件流 ↓        ↑ POST RunInput
┌─────────────────────────────────────────────┐
│ AGUI 事件面（前后端唯一契约）                    │
│ RUN_* / TEXT_* / TOOL_CALL_* / STEP_* /       │
│ STATE_SNAPSHOT + STATE_DELTA / CUSTOM         │
└──────────────┬──────────────────────────────┘
┌──────────────┴──────────────────────────────┐
│ 运行时层                                      │
│  Agent（LLM 决策）── 只注册 Graph Tool          │
│  Graph Tool 运行时 ── 节点状态机 · 双线结构      │
└──────────────┬──────────────────────────────┘
┌──────────────┴──────────────────────────────┐
│ 原子工具层                                    │
│  ① LLM 调用类（可注册 / 可作节点）              │
│  ② 业务类（只能作节点 · 禁止注册给 Agent）       │
└─────────────────────────────────────────────┘
```

---

## 2. 三类工具与注册约束

**tool = 方法。** 可以作为 flow 中的一个 node，也可以单独调用；flow 本身也可以封装成 tool（Graph Tool）单独调用。

| 类 | 名称 | 内容 | 注册给 Agent？ | 可作 Graph 节点？ |
|---|------|------|:---:|:---:|
| ① | LLM 调用类工具 | 能力封装：各模态生成、写提示词（LLM 节点）、视觉理解、检索 | **否**（P2 无例外：能力一律经 Graph Tool 暴露） | ✅ |
| ② | 业务类工具 | 扣费、权限校验、上传 OSS、写表、资产绑定、回执 | **禁止**（无法保证 Agent 一定调用；必须确定性执行） | ✅（仅此用途） |
| ③ | Graph Tool | 流程即工具；节点可含 ①②；有状态、可并行、可暂停 | **唯一注册面** | ✅（图可嵌套为节点，v1 不启用） |

**硬约束：**
- 业务类工具**只能**编入 Graph Tool 的节点，任何路径都不允许直挂 Agent。
- 业务逻辑变更只改业务工具本身；Graph 按节点状态推进，图结构不用动。
- 能力类工具产出重负载（图/视频字节、base64、长日志）**绝不**返回给 Agent——节点内部完成"生成 → 传 OSS → 写表"，只向上返回 `resource_id` / URL / 业务摘要。

---

## 3. Graph Tool 规范

### 3.1 定义与构成

Graph Tool = 一段封装成工具的有向流程。三副面孔：对 Agent 是一次普通工具调用（薄 schema 入参 + 摘要出参）；对运行时是一张节点 DAG；对用户是一条可感知的进度流。

一个 Graph Tool 由六部分**声明式定义**（是数据，不是代码——改图不发版）：

| 部分 | 内容 |
|---|---|
| 元数据 | key · 给 LLM 看的 description（Agent 据此决定何时调用）· 版本 |
| 入参 schema | Agent 可见的薄参数：目标引用、媒体种类、意图备注。**不暴露内部路径** |
| 节点集 | 每个节点：类型 · 输入/输出 · fanout · 重试/超时 · 业务切面声明 |
| 依赖边 | 节点间 DAG（无环）；调度就绪判定的唯一依据 |
| 暂停点 | 哪些节点后暂停 · 候选 actions · 触发条件（skill 可在允许范围内覆盖开/关） |
| 输出映射 | 图终态 → 给 Agent 的摘要 envelope 各字段的来源 |

定义示例（media_generate 精简版）：

```yaml
graph_tool: media_generate
description: 为蓝图中的目标生成媒体资产（图/视频/音频），必要时发起确认卡点
params:                        # Agent 只看到这一层
  targets: 蓝图目标引用[]       # element_id / shot_id / layer_id
  kinds:   [image | video | audio]
  note:    自由文本意图
nodes:
  write_prompt: { type: llm,        in: [targets, blueprint, spec], out: prompts }
  generate:     { type: capability, in: [prompts, refs], out: outputs,
                  fanout: per_target, path: auto,
                  requires: [permission, billing, persist, bind] }  # 业务切面
  confirm:      { type: pause, when: "kinds 含 image",
                  actions: [confirm_reference, revise_reference] }
edges: write_prompt → generate → confirm
output: { asset_ids: generate.outputs, summary: auto }
```

### 3.2 节点契约（统一接口）

所有节点实现同一契约，runtime 才能统一调度、统一发事件：

```
run(ctx, inputs) → outputs         # 正常完成
                 | pause(payload)  # 请求暂停（仅 pause 型节点允许）
                 | fail(error)     # 重试耗尽后失败
```

- **ctx 由 runtime 注入**：session、蓝图/规格版本、幂等键前缀、取消信号、事件发射器。节点**不直接发事件**，一律经 runtime 统一发——这是 P4"迁移=事件"的实现保证。
- **inputs / outputs 只传引用**：`resource_id` / `asset_id` / 结构化小对象；媒体字节永不进图状态。
- **节点属性**：`type`(llm | capability | business | pause) · `retry`(次数/退避/是否允许调参重试) · `timeout` · `fanout`(per_target 展开) · `requires`(业务切面声明)。
- **fanout 实例是最小状态单元**：每个实例独立走状态机，STEP 事件带实例索引，前端按组渲染。

### 3.3 双线结构与业务切面

每个 Graph Tool 逻辑上分两条线：**能力线**（LLM 节点 + 能力节点，做事）与**业务线**（权限/扣费/写表/绑定/回执，保障）。

**裁决：业务线不显式画进每张图，而是声明式切面织入。** 能力节点声明 `requires: [permission, billing, persist, bind]`，runtime 在其前后自动织入业务节点：

```
前置：权限校验 → 预扣费
本体：能力节点执行（生成 → 产 resource）
后置：传 OSS · 写表 → 绑定资产 → 回执（CUSTOM billing.charged）
失败：自动回冲预扣（回冲也发回执）
```

选切面而非显式节点的三个理由：
1. 图定义只画能力线，简洁可读；
2. **业务逻辑变更只改业务工具与织入规则，所有图自动生效**——"业务变了只动 tool，图不用改"；
3. 织入的业务节点与能力节点走同一状态机、同样发 STEP/回执事件，用户照样可感知。

约束不变：业务工具仍只能存在于图内（禁止注册给 Agent）；织入顺序固定，属物理层，skill 不可改写。

示意（media_generate 的一次图片生成，织入后）：

```
能力线：写提示词(LLM) ──→ 生成 ×N(并行) ──→ 汇合/质检
织入线：[权限 → 预扣]生成前 · [传OSS·写表 → 绑定 → 回执]生成后
```

### 3.4 节点状态机与 checkpoint

```
pending → running → succeeded
                  ↘ failed   （技术失败：节点内自动重试，重试耗尽才 failed）
                  ↘ paused   （等用户确认：checkpoint 落盘，可 resume）
```

- **paused 是一等状态**：确认卡点发生在图中途。进入 paused 时 checkpoint 持久化；resume 幂等且一次性（重复提交返回同一结果）；resume 载荷带蓝图/规格版本做冲突检测。
- **每次状态迁移必然产出事件**（STEP_* 或 STATE_DELTA），见 §8。
- **checkpoint 内容**：`{ graph_key, tool_call_id, 入参, 各节点/实例的状态与产物引用, 蓝图&规格版本, 已预扣费记录, 事件 seq }`。
- **resume 校验**：一次性 + 版本——若蓝图版本已变，不硬续跑，转确认卡问用户"蓝图已变更：继续 / 重新评估"。
- **图终态**：`succeeded / partial_succeeded / failed / paused`；partial_succeeded = fanout 部分实例失败但满足合并策略下限。

### 3.5 调度语义（状态驱动推进）

runtime 是一个纯状态机推进器，不含业务判断：

- **就绪判定**：节点 pending 且全部依赖 succeeded → 入执行队列。
- **fanout / 合并**：per_target 展开 N 个实例并行执行；合并策略由节点声明——`all`（全部成功才通过）或 `quorum`（达到比例即通过，失败实例列入确认卡由用户处置）。
- **暂停**：pause 节点触发 → 整图挂起 → checkpoint（§3.4）→ 本轮 run 正常收尾（§8.3）。
- **推进即事件**：所有调度与迁移由 runtime 统一广播，节点无权私发。

### 3.6 并行语义

- 串/并行**由依赖关系决定**，不是主观选择：节点间无输入输出依赖 → 可并行；有依赖 → 必串行。
- 经验规则：**基本只有"生成、分析资产"类任务会并行**（多个独立镜头、多张元素图、旁白与 BGM）。
- **设计异味**：出现"强关联的多个节点还想并行"，说明图切分错了，回炉。

### 3.7 失败语义

| 失败类型 | 处理 | 用户感知 |
|---|---|---|
| 技术失败（超时/模型抖动/坏输出） | 节点内自动重试（可调参重发），不打扰用户 | STEP 事件里可见 retry 计数 |
| 合规拦截（提示词踩红线） | **绝不静默改写绕过**。节点转 paused，告知被拦原因 + 建议调整方向，等用户授权后按合规重写规则重试 | 确认卡点 |
| 单节点失败 | 不扩散：无依赖的兄弟节点继续跑；依赖它的下游在依赖点停下问用户 | 局部红点，全局不倒 |

### 3.8 输出契约（给 Agent 的摘要）

```json
{
  "status": "ok | paused | partial | failed",
  "summary": "已生成 4 张元素参考图，进入参考图确认",
  "asset_ids": ["…"],
  "next_confirmation_id": "…（paused 时）",
  "receipts": [{ "type": "billing", "credits": -20 }],
  "blueprint_version": 13
}
```

绝不含字节、base64、provider 原始负载、长日志——上下文卫生是 P1 的一部分。

### 3.9 粒度约束

- 一个 Graph Tool 对应**一个用户可感知的阶段性成果**（一版蓝图 / 一批资产 / 一个成片）。
- 流程不过长：节点数收敛（能力线建议 ≤5 个节点）；过长说明该拆成两次 Agent 编排。
- 加能力优先加**内部路径/参数**；只有当新能力的产物类型、权限边界、用户感知形态都不同于现有工具时，才允许新增 Graph Tool。

### 3.10 胖工具内部路径选择（两级，不占 Agent 决策预算）

1. **Skill 声明层**：本题材用什么模型栈、是否插首帧、对口型场景必须走音频驱动路径——写在 skill 文档里。
2. **节点运行时兜底**：按输入判断——有参考图→图生图；有驱动音频→对口型；只有文字→文生路径。

Agent 只说"给这些目标生成图/视频/音频"，不选路径。

### 3.11 一次执行的完整事件序列（示例，对照 §8）

"为 4 个关键元素生成参考图"从调用到暂停的全程：

```
TOOL_CALL_START media_generate                  ← Agent 发起
STEP_STARTED  write_prompt
STEP_FINISHED write_prompt
STEP_STARTED  generate[e1..e4]                  ← fanout 4 实例并行
  CUSTOM billing.charged（预扣 −20，回执）
  CUSTOM media.generation_started ×4
  STATE_DELTA /jobs/{id}/progress …             ← 各实例进度交错
  CUSTOM media.resource.updated ×4
  STATE_DELTA /blueprint/elements/{i}/asset_ids ← 逐个绑定回填
STEP_FINISHED generate（合并策略 all 通过）
STEP_STARTED  confirm → 节点 pause · checkpoint 落盘
TOOL_CALL_END media_generate（status=paused）
TOOL_CALL_START ui.request_confirmation（确认 / 调整 候选）
RUN_FINISHED                                    ← 本轮收尾，等待用户
--- 用户点"确认" → 新 RunInput(toolResult) ---
新 run：resume → confirm succeeded → 图 succeeded
TOOL_CALL_RESULT 摘要 envelope 回给 Agent → Agent 总结陈述
```

---

## 4. 注册工具清单（v1 = 5 个 Graph Tool）

| 工具 | 职责 | 写权限 | 内部节点概要 |
|---|---|---|---|
| `plan` | 写蓝图与规格：Final Spec（类型/画幅/时长/风格/模型偏好）、蓝图（关键元素/镜头列表/音频层） | 蓝图/规格 | 解析意图(LLM) → 写文档 → 版本落盘 → 回执 |
| `media_generate` | **唯一资产写权限**。生成一切媒体；内部路径：文生图 / 图生图 / 首帧生视频 / 多模态生视频 / 插帧 / 音频驱动对口型 / 文生器乐 / 歌词生歌 / TTS 旁白 / 超分 | ★ 资产 | 写提示词(LLM) → 权限/扣费 → 派发生成 ×N(并行) → 传OSS·写表 → 绑定 → (可选 paused 确认) |
| `analyze` | 处理已有素材：关键帧提取、音轨分离、BPM/歌词逐行时间戳、角色/场景/镜头语言识别，产出结构化分析供下游引用 | 分析产物（只追加，不触资产） | 拉取素材 → 多模态分析(LLM/模型) → 结构化落盘 → 回执 |
| `assemble` | 按蓝图时间线拼合资产：铺轨、对齐时间戳、静音视频轨、混音；导出多形态（视频/工程/全部） | 组装（不产新媒体） | 校验齐备 → 拼时间线 → 渲染导出 → 回执 |
| `search` | 外部检索与库内选型：网络搜索、素材库/音色库按标签检索（探索型任务） | 只读 | 检索 → 排序摘要 |

**查询不设工具。** 资产状态/版本/绑定这类信息通过两条免费通道获得：
- Agent 侧：运行时每轮把共享状态摘要作**瞬态上下文**注入（不持久化、不进摘要）；
- 前端侧：本来就持有共享状态树（AGUI STATE），直接读。
若未来出现"深查询"需求（如跨项目检索），再按 P5 评估是否加工具。

**Skill 加载是运行时机制**（中间件在会话上激活 skill 文档），不占注册面。

### 预工具（Prefab Graph Tool）

平台/用户可把常用参数预置的 Graph Tool 存为"预工具"，在输入框 **@ 直接触发**——跳过 Agent 决策直接入图执行，但走同一套节点状态机与 AGUI 事件面。用户无需知道内部怎么执行。这使体系不限于当前产品的固定场景。

---

## 5. 数据模型：三层绑定

```
blueprint（蓝图）            asset（资产）               resource（资源）
element_id / shot_id  ──→   asset_id                ──→  resource_id @ 对象存储
audio_layer_id              · 多版本 variants[]           · 原始文件字节
· 带 version（乐观锁）        · favorite（首选版本）        · 上传三段式：
                            · source: user | generated     ticket(sha256秒传)→PUT→complete
```

- **工具互不调用**。Agent（或图内的绑定节点）是唯一调度枢纽：上一步产出的 `resource_id/asset_id` 由绑定关系承载，下一步从绑定关系取参考输入。
- **每个镜头/元素是独立资产单元**：可单点重生成（参考原关键帧/音频保持一致）、可一次产 2~3 个 variant 供选、替换只动该单元不重置时间线。
- **幂等（三处，键设计防两类事故）**：
  - 生成任务：`graph:{session}:{blueprint}@{version}:{node}:{target}:{kind}:{attempt}` —— **必须含蓝图版本与尝试序号**：同版本重复派发被去重；重生成 / 出 variant 递增 attempt。若键里没有版本/attempt，重生成会撞旧 job 的幂等键、永远拿回老结果。
  - checkpoint 以**单次调用**为界：`graph:{session}:{tool_call_id}` —— 再次调用同一 Graph Tool 必产新 checkpoint。**绝不能**按"会话+蓝图"甚至全局复用：否则第二次调用会 resume 到旧断点的"已在卡点"状态、静默跳过全部执行。
  - job 回调：按 `job_id + status_version` 幂等。

---

## 6. 权限模型（双层）

**物理层（系统硬编码，Skill 改不了、Agent 绕不过）：**

| 规则 | 说明 |
|---|---|
| 写资产权限唯一 | 只有 `media_generate` 能创建资产/加 variant/换 favorite/删除（删除需用户明确指令，永不自动删） |
| 用户上传只读 | 用户上传的原始文件**永久保留**，任何工具不得覆盖或删除 |
| 已确认资产保护 | 未经授权不得替换用户已确认的资产；重生成产出**并列新版本**，不覆盖旧版 |
| 分析/检索不触资产 | analyze/search 不得创建、修改、删除任何资产与用户素材；analyze 仅可**追加**结构化分析产物（独立的分析文档域） |

**行为层（Agent 调度规范，Skill 可在范围内微调）：**
不擅自重做已确认内容；合规拦截停下来问；全局性变更先问范围、给影响评估（哪些可复用/重做成本），授权后**最小化返工**并更新 Spec 作新基准。

---

## 7. Skill 与编排的分工

**Skill 定骨架（做什么），Agent 填血肉（怎么做）：**

| 层面 | Skill 定 | Agent 定 |
|---|:---:|:---:|
| 阶段顺序 / 依赖关系（如 2→1; 3→1,2） | ✅ | |
| 暂停卡点的位置 | ✅ | |
| 每阶段用哪个 Graph Tool（大类） | ✅ | |
| 模型栈偏好 / 提示词写法规则 | ✅ | |
| 内容决策（节奏/情绪/色调/参数） | | ✅ |
| 用户素材处置（有→绑定复用；无→生成） | | ✅ |
| 串/并行判断、工具内部路径兜底 | | ✅ |
| 异常与中途改需求处理 | | ✅ |
| 要不要加载 skill、加载哪个 | | ✅ |
| 卡点上开放什么操作（精改/跳过/回退/部分重做） | | ✅ |

**Skill 生命周期：**
- 双副本：注册表里的**原始模板** + 项目内的**可编辑副本**。编辑只改项目副本，**下一条消息起生效**；已完成步骤不回滚，从当前位置按新流程继续；"重新加载 skill"用模板覆盖项目编辑（需警示）。
- 选择：一次只激活一个。按"调用规则"字段匹配；明显偏向→直选并说明理由；模糊→出候选让用户裁决；不属于任何 skill→单步执行。需要多能力时以一个 skill 为主框架，Agent 参考其他 skill 文档补充判断，**不硬拼两个**。
- 无 skill 也能跑：Agent 按通用依赖规则（蓝图先于生成、参考图先于镜头、合成最后）推导流程，但缺领域经验、卡点判断粗——skill 的价值即"把有经验的制片人判断固化成数据"。

---

## 8. AGUI 交互契约（前后端唯一契约）

### 8.1 共享状态树

前端一切渲染 = 此树的投影：

```json
{
  "spec":      { "version": 3, "markdown": "…", "model_preference": {…} },
  "blueprint": { "version": 12, "elements": […], "shots": […], "audio_layers": […] },
  "assets":    { "<asset_id>": { "kind": "image|video|audio", "status": "…",
                  "url": "…", "variants": […], "favorite": "…", "source": "user|generated" } },
  "jobs":      { "<job_id>": { "node": "…", "progress": 0.6, "status": "running" } },
  "credits":   { "balance": 1448, "ledger": […] },
  "run":       { "active_steps": […], "paused": { "checkpoint_id": "…", "actions": […] } }
}
```

### 8.2 事件映射表（逐交互）

| 用户看到的 | AGUI 事件 | 载荷要点 |
|---|---|---|
| 一轮对话开始/结束/出错 | `RUN_STARTED / RUN_FINISHED / RUN_ERROR` | runId · threadId |
| Agent 打字机回复 | `TEXT_MESSAGE_START / CONTENT / END` | messageId · delta |
| "正在调用 xx 工具"卡片 | `TOOL_CALL_START / ARGS / END` | toolCallId · name=某 Graph Tool |
| 流程进度逐节点推进 | `STEP_STARTED / STEP_FINISHED` | stepName=节点 key（能力/业务节点同一套） |
| 生成进度% / 资产就绪 / 蓝图长出 | `STATE_DELTA`（JSON Patch） | `/jobs/x/progress`、`/blueprint/shots/3/video_asset_id`，**带版本** |
| 首屏 / 断线重连恢复 | `STATE_SNAPSHOT` | 共享状态树全量 |
| 扣费回执（如 −5） | `CUSTOM billing.charged` | credits · nodeId · 余额 |
| 生成三段生命周期 | `CUSTOM media.prompt_draft / generation_started / resource.updated` | 模型/参考资产/单资产失效信号（前端只重取该资产） |
| 确认卡点卡片 | `TOOL_CALL_START ui.request_confirmation` | title · message · actions[]候选芯片 · 资产引用 · checkpoint_id · 版本 |
| 用户点选（确认/调整/跳过/回退） | RunInput 的 `toolResult` | action_key + checkpoint_id + 版本（幂等 + 冲突检测） |

### 8.3 确认卡点时序（human-in-the-loop）

```
节点 paused + checkpoint 落盘（checkpoint_id 绑定本次 tool_call_id）
  → TOOL_CALL_END：本次 Graph Tool 调用以 status=paused 收尾（返回 checkpoint_id）
  → TOOL_CALL_START ui.request_confirmation（携 actions[] 候选）
  → RUN_FINISHED（本轮 run 结束，停在"等待用户"）
  → 前端渲染确认卡（候选芯片，支持自由文本"其它"）
  → 用户点选 → 新 RunInput 携 toolResult（checkpoint_id + action_key + 版本）
  → 新 run：校验幂等一次性 + 版本冲突 → resume 续跑图
  → STEP / STATE 事件继续播
```

> 关键：paused **不会挂着一个永不结束的 run**——本轮 run 正常收尾，恢复是一次新 run。这样断线、多端、超时都不需要特殊处理。

确认卡不是特殊协议——就是一次普通"前端工具"调用往返，协议面不膨胀。

### 8.4 四条硬规则

1. **前端零业务**：只应用 SNAPSHOT + DELTA，永不自行推断状态。
2. **节点状态迁移 = 事件，一一对应**：不存在后端做了事而前端不知道。这是"用户感知流程/工具/状态"的技术兑现。
3. **确认卡点走前端工具调用**：paused 即 `ui.request_confirmation`，点选即 toolResult，resume 幂等一次性。
4. **业务节点也发回执**：扣费/权限/绑定对 Agent 不可见（禁止注册），但对**用户可见**（CUSTOM 回执）——审计与信任感来源。

### 8.5 工程细节

- 并行节点的 STEP 事件交错到达，前端按 stepId 分组渲染。
- 断线重连：`STATE_SNAPSHOT` 兜底全量 + 事件 seq 续传。
- 提交/消费流解耦：发消息只回执 run 标识，事件流独立拉取，支持多端 attach。
- **异步任务跨 run**：媒体生成的寿命可超出单次 run。job 完成后的 `media.resource.updated` / STATE_DELTA 发到**会话级事件流**（不绑定 run）；同时写入 wakeup 记录，Agent 下一轮经状态注入自动看到结果，无需用户复述。
- **用户直接编辑不经 Agent**：蓝图 / Spec / skill.md 的手动编辑走独立数据接口（PATCH + base_version 乐观锁），服务端受理后广播 STATE_DELTA 同步所有端；Agent 下一轮经状态注入感知变更。

---

## 9. 端到端旅程示例（一句话 → 音乐 MV）

| 步骤 | 谁在做 | AGUI 上发生什么 |
|---|---|---|
| 用户："做一首暗恋中文流行歌 + 竖屏 MV" | Agent 判断复杂项目 → 匹配激活"音乐 MV" skill | RUN_STARTED · TOOL_CALL(skill 加载卡) |
| 建规格 | `plan`（spec） | STEP_* · STATE_DELTA(/spec) · 文档面板出现 Spec |
| 问视觉风格 | 图内 paused | ui.request_confirmation（4 个风格候选芯片，分页） |
| 设计蓝图 | `plan`（blueprint） | STATE_DELTA 逐条长出元素/镜头/音轨 |
| 分析歌曲 | `analyze` | 结构化时间戳落盘（BPM/歌词逐行 start_ms/end_ms） |
| 元素参考图 | `media_generate`（并行 ×N） | media.prompt_draft → started → resource.updated ×N · billing.charged · 绑定 DELTA |
| 确认参考图 | 图内 paused | ui.request_confirmation（确认 / 调整） |
| 关键帧 → 镜头视频 | `media_generate`（演唱镜头走对口型路径、叙事镜头走多模态路径；与音频生成并行） | STEP 交错 · 进度% DELTA |
| 合成 | `assemble` | 拼轨对齐 · RUN_FINISHED · 导出入口 |

---

## 10. 非目标（v1 不做）

- 多 skill 同时激活混排（只允许"一主框架 + 参考"）。
- Graph 嵌套 Graph（图作为节点）。
- 深查询工具（跨项目/复杂检索）——状态注入不够用时再议。
- 多人实时协同编辑同一蓝图。

---

## 附：与 Flova 实测的对应关系（依据清单）

| 本设计条目 | Flova 实证 |
|---|---|
| Graph Tool 节点吐进度 | 生歌流程逐条"资产配置完成→生成素材完成"进度卡 |
| 业务回执 | "Media Assets 已完成 **−5**" 扣费挂在节点上 |
| 注册面少而胖 | LLM 只见 ~7 个胖工具，生成十几条路径全在 media_generator 内部 |
| 权限物理层硬编码 | "上传只读、查询不可写、不覆盖已确认——skill 改不了 agent 绕不过"（Flova 原话） |
| paused/checkpoint/resume | 参考图确认发生在图中途，message 带 checkpoint_id 可任意点 resume |
| 并行仅生成/分析 | "并行取决于依赖——独立镜头/元素图/VO+BGM 同时生成"（原话） |
| skill 双副本与编辑语义 | 项目内 skill.md 可编辑、下条消息生效、已完成不回滚、重载覆盖编辑（原话） |
| 单镜头独立重生成 + variant | "只重做 Shot_3，参考原关键帧+音频，一次 2~3 个变体"（原话） |
| 合规不绕过 | "改写绕过等于欺骗你，必须告知等授权"（原话） |
