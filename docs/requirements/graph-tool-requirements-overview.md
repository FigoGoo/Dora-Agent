# Dora Graph Tool 功能需求总览

> 文档状态：需求基线稿
>
> 版本：v0.1
>
> 更新日期：2026-07-14
>
> 关联文档：[共通业务规则与验收基线](common-requirements-baseline.md)、[用户端需求总览](user-requirements-overview.md)、[管理端需求总览](admin-requirements-overview.md)、[服务端需求总览](server-requirements-overview.md)

## 1. 文档目的与结论

本文定义 Dora v1 必须提供的用户可发现、可选择、可调用和可验收的 Graph Tool 产品能力，补齐此前只有 Graph Tool 共通运行规则、但没有逐项功能需求和用户使用入口的问题。

v1 工具箱必须至少包含以下六个 Graph Tool，显示名称和默认顺序与产品原型一致：

1. 流程规划。
2. 素材分析。
3. 故事板设计。
4. 媒体生成。
5. 提示词写法。
6. 视频剪辑。

仅在 Agent Registry 中存在内部 Tool、或只能由模型隐式调用，不能视为用户已经获得 Graph Tool 功能。六个工具必须同时满足目录可见、能力可理解、入口可操作、运行可追踪、结果可复用和费用可核对。

本文是需求基线，不提前固定 Graph State、Node/Branch、DTO、RPC、Event、Job Payload 或数据表；这些内容按既定决策在详细设计阶段确定。

## 2. 术语与边界

| 对象 | 需求定义 |
| --- | --- |
| Graph Tool Definition | 平台维护的高层能力定义，包含稳定 `tool_key`、显示名称、用途、版本、输入输出说明、前置条件、权限、计费说明和可用状态。 |
| Graph Tool Run | 用户显式选择或主 Agent 自动选择某个已发布 Graph Tool 版本后产生的一次可追踪运行。 |
| 公开 Tool | Skill 通过 `@Tool` 引用的受控原子能力；不等于本文六个面向完整创作场景的 Graph Tool。 |
| UI Command | 用户对确定对象执行的局部命令，例如修改某个 Prompt、绑定某个资产或重生成某个槽位；它可以触发 Graph Tool，但不是新的 Graph Tool Definition。 |
| 内部 Node/Tool | Graph Tool 内部实现节点或 Provider 适配器，不直接出现在用户工具箱，也不能由 Skill 或模型绕过 Graph Tool 边界调用。 |

Graph Tool 由唯一主 Agent 使用，可以由内部 Graph/Workflow 编排模型、确定性校验、领域命令和异步任务，但不能实现为子 Agent、AgentAsTool 或用户可上传代码。

## 3. 用户使用入口与完整流程

### 3.1 使用入口

用户必须可以通过以下方式使用 Graph Tool：

1. **工具箱显式选择**：在 Project 工作台打开“工具”，看到六个工具，选择后通过结构化表单或 A2UI 卡补齐输入并执行。
2. **自然语言调用**：用户在 Chat 中表达目标，由唯一主 Agent 在权限和预算内选择合适 Graph Tool，并明确展示实际调用的工具。
3. **Skill 驱动调用**：已发布 Skill 可以声明允许使用的 Graph Tool 和调用规则；运行时仍由平台 Registry、权限、预算和 Session Skill Snapshot 约束。
4. **工作台上下文操作**：故事板、Prompt、资产或时间线上的操作可以携带确定目标进入对应 Graph Tool，不要求用户重新描述已有上下文。

用户在工具箱显式选择工具时，客户端必须提交稳定 `requested_tool_key`。主 Agent 可以询问缺失信息，但不得静默替换为另一个 Graph Tool；无法满足前置条件时必须说明缺失项并停止，或在用户确认后启动所需的前置工具。

### 3.2 工具箱交互

工具箱至少需要提供：

- 图标、中文名称、简短说明、当前状态和是否满足前置条件。
- 预计积分范围、主要计费来源、预计耗时和是否产生外部副作用。
- 需要读取的 Project、素材、故事板、Prompt 和资产范围。
- 直接使用入口、使用示例、最近运行和结果入口。
- 工具不可用时的稳定原因，例如权限不足、余额不足、前置结果缺失、版本暂停或平台容量受限。

已发布但前置条件暂不满足的工具默认显示为禁用并解释原因，不能无提示消失；因权限或安全策略不能向用户披露的工具可以隐藏。

### 3.3 建议创作链路

六个工具既能按完整链路协作，也能在满足前置条件时独立使用：

```mermaid
flowchart LR
    A["用户目标与已有素材"] --> B["素材分析"]
    A --> C["流程规划"]
    B --> C
    C --> D["故事板设计"]
    D --> E["提示词写法"]
    E --> F["媒体生成"]
    F --> G["视频剪辑"]
    G --> H["预览或最终交付物"]
    F --> E
    D --> C
```

链路不是强制向导：例如用户可以在独立模式下直接进行提示词写作、媒体生成或视频剪辑。工具必须明确当前使用的是“独立模式”还是“Project/故事板模式”，避免把独立结果自动覆盖到故事板。

## 4. v1 Graph Tool 目录

| 默认顺序 | `tool_key` | 用户显示名称 | 用户目标 | 主要结果 | 执行形态 |
| --- | --- | --- | --- | --- | --- |
| 1 | `plan_creation_spec` | 流程规划 | 把创作目标整理成可执行流程、约束和阶段计划 | 版本化 Creation Spec、阶段计划、依赖、风险和预算范围 | 有界同步或短异步，结果需确认 |
| 2 | `analyze_materials` | 素材分析 | 理解文字、图片、PDF、音频和视频素材并判断可用性 | 版本化分析报告、引用、标签、关键片段、质量与风险项 | 有界同步或异步分析 |
| 3 | `plan_storyboard` | 故事板设计 | 根据已确认目标创建或整体重规划动态故事板 | Pending Storyboard Revision、模块、元素、依赖、Prompt/Asset Slot | 有界同步或短异步，结果需确认 |
| 4 | `generate_media` | 媒体生成 | 生成图片、视频、音频等候选资产 | Operation/Batch/Job、候选资产和费用汇总 | 异步 |
| 5 | `write_prompts` | 提示词写法 | 独立创建、改写或批量补齐模型提示词 | 版本化 Prompt 候选、适配说明和影响范围 | 有界同步或短异步，结果可确认 |
| 6 | `assemble_output` | 视频剪辑 | 编排时间线并生成预览或最终视频 | 版本化剪辑方案、预览和最终交付资产 | 计划同步，渲染异步 |

显示名称属于用户产品术语；稳定 `tool_key` 用于版本、运行、审计和 Skill 引用，不随中文名称调整而变化。

## 5. 六个 Graph Tool 功能需求

### 5.1 流程规划 `plan_creation_spec`

**用户输入**至少支持创作目标、内容类型、受众、语言、时长、画幅、风格、交付形式、时间要求、预算偏好和可选素材分析引用。

**输出**至少包含：

- 创作标题、目标、受众和验收结果描述。
- 创作阶段、阶段顺序、依赖关系及每阶段所需输入。
- 视觉、叙事、声音、语言、时长、画幅和模型偏好。
- 预计使用的后续 Graph Tool、生成数量范围、积分范围和风险提示。
- 仍需用户补充或正式确认的事项。

新规划或结构性修订必须形成新的候选版本；用户确认前不能替换当前有效 Creation Spec，也不能自动启动媒体生成。全局约束变化必须明确标记故事板是否需要整体重规划。

### 5.2 素材分析 `analyze_materials`

**用户输入**至少支持从资产中心或当前 Project 选择文字、图片、PDF、音频和视频，指定分析目标、重点和输出语言。

**输出**根据媒体类型至少包含：

- 来源资产和版本引用、内容摘要、主题、人物/主体、场景和风格标签。
- 图片/PDF 的版面或视觉要素，音视频的关键时间段、语音摘要和镜头信息。
- 分辨率、时长、清晰度、完整性、可编辑性和格式适配结论。
- 版权、隐私、敏感内容、低质量或无法解析的风险项。
- 对流程规划、故事板、Prompt 和媒体复用的建议。

分析必须引用真实素材证据；不能读取二进制内容时必须返回 `partial` 和明确缺失项，不得仅凭文件名或 MIME 猜测内容。分析不会修改、删除、公开或自动绑定原素材。

### 5.3 故事板设计 `plan_storyboard`

**前置条件**为已确认 Creation Spec；素材分析可以缺省，但缺少会影响质量时必须提示。

**输出**至少包含：

- 版本化动态 Storyboard Revision。
- 可适配短视频、长视频、图片集、音乐或其他场景的模块与元素，不固定写死分镜数量。
- 元素顺序、时长、画面/声音内容、依赖 DAG、Prompt Slot、Asset Slot 和交付约束。
- 新增、修改、复用、归档目标以及可能失效的已有资产。
- 需要用户确认的完整候选快照和版本差异。

故事板设计负责定义“需要什么 Prompt”和“需要什么资产”，不负责把最终 Prompt 文本作为不可见内部步骤完成。初始 Prompt 可以为空或为草案，正式创建、优化和批量补齐由“提示词写法”Graph Tool 完成。Pending Revision 未确认前不得替换 Active Revision 或派发付费媒体任务。

### 5.4 媒体生成 `generate_media`

媒体生成必须支持两种用户模式：

1. **故事板模式**：根据 Active Storyboard、已确认 Prompt、依赖和当前 Binding 生成下一批或用户选择范围内的候选资产。
2. **独立模式**：用户直接选择图片、视频或音频类型，填写 Prompt、模型偏好、数量和输出规格；结果进入当前 Project 与资产中心，但不自动绑定故事板。

正式派发前必须显示预计积分范围、生成数量、模型/能力类型和可能的等待时间。每个可计费生成项开始前按冻结配置扣费；Graph Tool 终态只汇总去重后的明细。

故事板模式和独立模式统一遵循：Graph 的确定性 Node 先调用 Business `PrepareGeneration`，由 Business 原子校验、创建元素/资产占位并直接扣费；只有返回 `charged + preparation_id + element_ids/asset_ids + ledger_entry_ids` 后才能创建 Operation/Batch/Job 并派发 Worker。该扣费不是 Reservation，失败、取消或未绑定不退款。

异步运行必须创建可取消、可恢复、可追踪的 Operation/Batch/Job。部分成功时分别展示成功资产、失败项、已扣积分和可执行的重新生成入口；技术重试不得重复扣费，用户重新生成属于新执行。

Worker 完成后只发布持久化 `generation.batch.*` 终态事件；Agent Inbox 幂等生成 `BatchContinuationResult` 并唤醒 Session Lane。Worker 不直接调用 Agent、不生成 A2UI；故事板/资产写入通过 Business Finalize RPC 完成，Binding 可选。

### 5.5 提示词写法 `write_prompts`

提示词写法是独立 Graph Tool，不得只作为故事板 Graph 内不可见的内部模型节点。

必须支持：

- **独立模式**：根据用户目标创建或改写单条 Prompt，并提供适用模型/媒体类型、变量、负向约束和使用说明。
- **故事板模式**：对指定目标、模块或所有缺失/过期 Prompt Slot 批量生成候选 Prompt。
- 保留用户锁定的 Prompt，不得被批量任务覆盖。
- 精确返回请求范围内的 Prompt，缺失、重复或多余目标必须失败关闭或明确部分失败。
- 保存 Prompt 版本、来源、适用模型、创建者、用户编辑和确认状态。
- Prompt 变更后确定性标记依赖它的未开始任务或旧结果为待重新评估，不直接删除旧资产。

调用该工具可以产生模型积分扣费，但不会自动调用媒体 Provider。用户确认、保存或复制已有结果不重复扣费。

### 5.6 视频剪辑 `assemble_output`

视频剪辑必须支持从 Active Storyboard 或用户选择的独立资产创建时间线，至少覆盖：

- 片段选择、顺序、入出点、裁剪、画幅适配和速度。
- 转场、字幕、标题、贴图和基础视觉层。
- 背景音乐、配音、原声、音量、淡入淡出和基础混音。
- 目标时长、分辨率、帧率、编码格式和预览/最终导出模式。
- 缺失素材、格式不兼容、版权风险和过期依赖检查。

剪辑方案必须形成版本化、可预览的 Assembly Plan。用户修改计划不会覆盖已经交付的旧成片；预览或最终渲染使用冻结计划创建异步任务。当前仅生成 JSON manifest 或占位媒体不能作为“视频剪辑”生产需求验收通过。

## 6. 共通运行、状态与结果要求

### 6.1 Definition 与版本

- 六个 `tool_key` 必须在生产 Graph Tool Registry 中各自存在至少一个已发布 Definition Version；灰度期间可以并存多个已发布版本，但对一个确定用户和 Run 必须唯一解析到一个有效版本。
- Definition 改输入输出、前置条件、权限、计费引用或副作用时形成新版本；运行开始后冻结版本。
- 新运行不能使用已暂停、已下架或不兼容版本；运行中的已冻结版本按安全策略完成、取消或进入核对，不能静默切换。
- Skill 只能声明使用平台已发布 Graph Tool，不得上传 Graph、Node、Prompt 模板代码或扩大工具权限。

### 6.2 Run 与用户投影

- 每次实际调用创建唯一 Graph Tool Run，并关联 User、Project、Session、Turn、可选 Skill Invocation、Definition Version 和业务幂等键。
- 同一 Session 的状态写入遵循 Session Lane 串行和版本校验；过期 Agent/Worker 结果不能覆盖新版本。
- 同步工具超过平台交互时限时必须转为持久化异步运行，不能维持易丢失的请求栈。
- 用户至少看到工具名称、阶段、总体状态、进度摘要、已用积分、结果入口、失败原因、取消和允许的重试动作；内部 Node、Reasoning、Provider Payload 和密钥不得展示。
- `accepted` 仅表示异步任务已可靠受理；不能显示为“已完成”。刷新、断线和服务重启后必须恢复同一运行。

### 6.3 确认与副作用

- Creation Spec 和 Storyboard 候选启用、批量媒体生成、最终视频导出以及超过用户预算的执行使用正式 Approval 或等价的确定性确认，不接受自然语言“确认”绕过。
- 用户拒绝候选时保留版本和审计，但不执行尚未开始的后续副作用。
- Graph Tool 不得通过内部 Node 绕过用户权限、内容安全、资产授权、积分预算或 Provider 配额。

## 7. 计费与 Skill 收益归属

六个工具统一遵循[共通计费真值表](common-requirements-baseline.md#32-计费真值表)：

- 浏览工具、打开表单、读取 Schema、校验前置条件、查看已有结果和汇总已有费用不扣费。
- 流程规划、素材分析、故事板设计和提示词写法在每次实际模型执行开始前按模型计费配置扣除积分。
- 媒体生成和视频剪辑按实际启动的模型、媒体、公开 Tool 或渲染执行逐项扣费。
- 一个 Graph Tool 包含多次执行时，终态汇总等于去重后的扣费明细，不追加 Graph Tool 完成费。
- 用户从工具箱直接调用平台原生 Graph Tool 时不产生 Skill 发布者收益；Graph Tool 明确归属于有效 Skill Invocation 时，合格成功明细按冻结收益规则归属该 Skill 发布者。
- 失败、取消或部分成功不退还已正常发生的扣费；平台重复扣费、错扣或权威事实证明外部执行未开始时通过账务冲正纠错。

## 8. 管理与运营要求

管理端必须支持：

- 查看和管理六个 Graph Tool 的 Definition、版本、状态、默认顺序、可见范围和兼容性。
- 配置每个版本允许的模型、Provider、公开 Tool、权限、预算、并发、超时和计费配置引用。
- 单工具测试、灰度发布、暂停、恢复、下架和紧急 Kill Switch。
- 查询 Run、阶段、成功率、部分失败率、取消率、延迟、积分、Provider 成本和异常原因。
- 定位受版本变更影响的 Skill、Project 和未终态运行。
- 对暂停、改价、权限变化、人工取消、重放和敏感数据查看保留完整审计。

管理员不得在运行请求中临时覆盖已发布版本、用户身份、最终价格或用户权限。

## 9. Graph Tool 专项非功能要求

| 维度 | v1 验收要求 |
| --- | --- |
| 目录可用性 | 工具目录核心 API 月可用性不低于 `99.9%`，六个已发布工具不得因单个 Provider 故障从目录无提示消失。 |
| 目录性能 | 工具列表和详情在正常负载下 `P95 ≤ 500ms`；打开工具后 `P95 ≤ 1s` 返回表单 Schema、前置状态和费用说明。 |
| 运行反馈 | 用户提交后 `200ms` 内出现本地反馈，持久化接受后 `P95 ≤ 2s` 返回首个运行状态。 |
| 幂等 | 同一 Graph Tool 业务幂等键并发或重放 `100` 次，只产生一个 Run、一组业务副作用和一次对应扣费。 |
| 恢复 | 同步请求转异步、浏览器断线或服务重启后继续使用持久化 Run；Redis 唤醒丢失时 `P95 ≤ 30s` 恢复处理。 |
| 容量 | 每个 Graph Tool 分别配置模型、Provider、Worker 和单用户并发硬上限，不允许一个媒体批次耗尽所有规划类工具容量。 |
| 可观测性 | 每个 Run 可按 `tool_key/version/run_id/project_id/session_id/operation_id` 追踪；普通日志不保存完整素材、Prompt、Provider Payload 或 Reasoning。 |

其余指标遵循[共通 v1 量化非功能基线](common-requirements-baseline.md#6-v1-量化非功能基线)。

## 10. 可执行验收用例

以下用例继承[共通 Evidence 要求](common-requirements-baseline.md#7-可执行验收规范)：

| ID | Given / When | Then |
| --- | --- | --- |
| GTL-CAT-001 | Given 普通用户打开 Project 工具箱；When 工具目录加载成功 | 按默认顺序显示流程规划、素材分析、故事板设计、媒体生成、提示词写法、视频剪辑六项，名称与稳定 `tool_key` 一一对应。 |
| GTL-CAT-002 | Given 某已发布工具前置条件不满足；When 用户查看工具箱 | 工具保持可发现但禁用并说明缺失项；安全策略要求隐藏的工具除外。 |
| GTL-USE-001 | Given 用户显式选择提示词写法；When 提交输入 | 请求冻结 `write_prompts`，Agent 只能补问或调用该工具，不能静默改为故事板设计或媒体生成。 |
| GTL-USE-002 | Given 用户通过自然语言或 Skill 发起调用；When Agent 选择 Graph Tool | UI 展示实际工具名称、版本、输入摘要、状态和费用，运行关联正确 Turn 和可选 Skill Invocation。 |
| GTL-VER-001 | Given Tool 发布新版本或暂停旧版本；When 新旧 Run 并存 | 新运行按发布状态选择版本，旧运行保持冻结版本或安全终止，历史结果和账单不被改写。 |
| GTL-IDEM-001 | Given 同一 Run 幂等键并发提交 100 次；When 执行结束 | 只存在一个 Run、一组副作用和一份去重费用汇总。 |
| GTL-BILL-001 | Given 一个 Graph Tool 内发生多次模型或 Provider 执行；When 进入终态 | 每项开始前按冻结配置扣费，Tool 汇总等于去重明细之和，没有完成费。 |
| GTL-EARN-001 | Given 同一 Graph Tool 分别由工具箱直接调用和有效 Skill Invocation 调用；When 成功结算 | 直接调用不产生发布者收益，Skill 归因调用只按合格成功明细生成一份收益。 |
| GTL-PLAN-001 | Given 用户提供创作目标；When 执行流程规划 | 产生完整候选 Creation Spec、阶段、依赖、预算范围和待确认项；确认前不启动媒体生成。 |
| GTL-ANALYZE-001 | Given 用户选择文字、图片、PDF、音频和视频素材；When 执行素材分析 | 输出带资产/时间段引用的真实内容分析；不能读取的内容显示 `partial`，不根据文件名臆测。 |
| GTL-STORY-001 | Given 已确认 Creation Spec；When 执行故事板设计 | 产生 Pending Revision、动态模块/元素、依赖和 Prompt/Asset Slot；未确认前不替换 Active 或派发生成。 |
| GTL-PROMPT-001 | Given Storyboard 存在一组缺失 Prompt Slot 且部分 Prompt 被用户锁定；When 批量执行提示词写法 | 精确生成未锁定请求集合，保留锁定内容，保存版本且不调用媒体 Provider。 |
| GTL-PROMPT-002 | Given 用户没有 Storyboard；When 从工具箱独立使用提示词写法 | 可以创建并保存独立 Prompt 结果，不强制先创建 Storyboard。 |
| GTL-MEDIA-001 | Given 用户没有 Storyboard；When 在独立模式生成媒体 | 创建异步 Run 和候选资产，结果进入 Project/资产中心但不自动绑定故事板。 |
| GTL-MEDIA-002 | Given Active Storyboard、确认 Prompt 和依赖已就绪；When 在故事板模式生成媒体 | 只为符合版本和依赖的目标创建 Job，迟到结果不能覆盖新 Prompt 或新 Revision。 |
| GTL-MEDIA-003 | Given Business `PrepareGeneration` 未返回 charged 或缺少元素/资产/账本 ID；When Graph 尝试派发 | 不创建 Worker Job、不调用 Provider；Unknown Outcome 使用原幂等键查询，不重复扣费。 |
| GTL-MEDIA-004 | Given Worker Batch terminal event 重复或乱序投递；When Agent Inbox 消费 | 同一 result version 只创建一个 Continuation，旧版本不回退 Card，不恢复旧 Graph。 |
| GTL-EDIT-001 | Given 用户选择多个视频、音频、图片和字幕；When 创建视频剪辑方案 | 生成可预览的版本化时间线，包含顺序、时长、转场、字幕、音频和导出设置。 |
| GTL-EDIT-002 | Given 剪辑计划已确认且依赖齐全；When 导出预览或最终视频 | 使用冻结计划异步渲染真实媒体资产；仅返回 JSON manifest 或占位视频不得通过。 |
| GTL-ASYNC-001 | Given 媒体生成或视频剪辑已返回 `accepted`；When 刷新、断线或服务重启 | 恢复同一 Run/Operation/Batch 状态，未终态不显示完成且不重复扣费。 |
| GTL-CANCEL-001 | Given Graph Tool 部分执行已扣费；When 用户取消 | 未开始项停止且不扣费，已开始项尽力取消并保留正常扣费，终态汇总可核对。 |
| GTL-SEC-001 | Given 用户无资产权限或输入包含未授权内容；When 调用任一 Graph Tool | 在读取或副作用前失败关闭，不泄露资产、Prompt、Provider Payload、内部 Node 或密钥。 |
| GTL-ADM-001 | Given 管理员暂停某 Tool 版本并执行重复操作；When 查询新运行和审计 | 新运行失败关闭，合法在途运行按冻结策略处理，操作幂等且审计完整。 |

## 11. 当前设计与目标需求差距

当前 `master` 尚无可运行的服务端实现。下表“历史设计状态”专指 `main` 分支及现有 AIGC 详细设计记录的迁移起点，不能视为当前分支能力，更不能直接视为本文需求已经实现。

| 目标需求 | 历史设计状态 | 必须补齐 |
| --- | --- | --- |
| 六个用户可见工具 | 历史 Runner Registry 固定五个 Tool | 新增独立 `write_prompts`，Registry、权限、状态和计费纳入同一治理。 |
| 用户工具箱和显式调用 | 历史实现重点是 Agent 内部隐式选择 | 增加目录、详情、结构化输入、`requested_tool_key`、运行与结果入口。 |
| 素材真实内容分析 | 历史实现主要分析文件名、MIME、URL 和可信 metadata | 接入图片、PDF、音频、视频内容抽取，并提供证据引用与部分失败。 |
| 提示词写法独立可用 | 历史 Prompt 生成隐藏在 `plan_storyboard`/`generate_media` 内部 | 拆出独立 Graph Tool，同时保留确定性 Prompt 编辑命令边界。 |
| 媒体生成独立模式 | 历史实现主要推进 Active Storyboard | 支持无 Storyboard 的直接生成，结果只进入 Project/资产中心。 |
| 生产级视频剪辑 | 历史 Assembly 可以仅产生 manifest 或占位结果 | 实现版本化时间线、预览和真实渲染/转码交付。 |

详细设计和实现验收不得以“历史分支已有五个 Tool 名称相近”替代上述差距关闭证明。

## 12. 详细设计阶段门禁

进入实现前，六个 Graph Tool 必须各自具有 `docs/design/agent/graphtool/<tool_key>-design.md`，至少包含：

- 用户场景、输入输出 Schema、权限和计费点。
- Mermaid 流程图、稳定 Node 清单/类型、Edge/Branch 和终止条件。
- Graph State 与独立业务状态机，不得用 Graph 执行状态代替领域状态。
- 同步/异步边界、Operation/Batch/Job、Approval、Cancel 和 Continuation。
- 幂等键、Receipt、Checkpoint、Unknown Outcome、重试和恢复策略。
- 模型/Provider/Tool/Token/时间/并发/积分预算。
- 数据所有权、跨 Module DTO/Event/Job 契约和兼容升级矩阵。
- 单元、契约、故障注入、恢复、计费和用户验收用例。
- 明确审核结论和与本文需求 ID 的追踪关系。

六份独立设计草案及共同契约目录已收录在 [Graph Tool 详细设计索引](../design/agent/graphtool/README.md)。当前均为 Draft / 待评审；缺少设计或评审未通过时，对应 Graph Tool 都不得开始实现或宣称生产可用。

## 13. 待确认参数

以下参数不影响六个 Graph Tool 的必备范围，但需要在交互或详细设计阶段确定：

1. 六个工具的最终图标、说明文案、示例和表单字段布局。
2. 素材分析支持的具体格式、单文件大小、时长、页数和批量上限。
3. 独立媒体生成首期开放的媒体类型、模型、数量、分辨率和时长上限。
4. 视频剪辑首期转场、字幕、音频、编码、分辨率和最长时间线上限。
5. 每个工具默认预算、同步转异步阈值、超时、并发和审批阈值。
6. Graph Tool 是否允许用户收藏、固定顺序或展示最近使用；v1 默认顺序仍以本文为准。
