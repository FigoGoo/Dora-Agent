# W2 Graph Tool 两方案冲突清单 v1（评审议题输入）

> 状态：Draft / W2 评审议题输入
>
> 日期：2026-07-16
>
> 目的：把「六 Graph Tool 方案」与「创作体系对案」的分歧压缩为**必须裁决的 6 个问题**，避免评审在细节里打转。本文只陈述分歧与同频，不替评审做结论。
>
> 方案 A（六 Tool）：[设计索引](README.md) + 六份 `<tool_key>-design.md` + [跨 Module 契约目录](../../cross-module/aigc-contract-catalog.md) + [Runner/Session Lane 评审](../runner-session-lane-review-v1.md) + [Graph Tool 需求总览](../../../requirements/graph-tool-requirements-overview.md)。
>
> 方案 B（对案）：[Dora AIGC 创作体系设计终版 v1](../../../superpowers/specs/2026-07-11-aigc-system-design-final.md) + [GraphTool/node 详设](../../../superpowers/specs/2026-07-13-aigc-graphtool-node-detail.md)。

## 1. 两方案一句话速览

- **方案 A**：用户工具箱 = 六个流程阶段 Tool（流程规划/素材分析/故事板/提示词/媒体生成/剪辑装配），每个 Tool 是设计期写死的 Eino 无环 DAG；唯一主 Agent 逐 Turn 路由到单个 Tool；跨 Tool 靠 Resource Ref + 前置条件 + 逐阶段正式 Approval 衔接；无自动全流程编排。
- **方案 B**：用户工具面 = 四个模态产物 GraphTool（图/视频/音乐/音频，@直呼）+ 门控故事板；阶段（分析/提示词/装配）内化为 node 词汇不暴露；Agent 按动态四档编排（命中固化 → 计划层组合 → 兜底连线 → 沉淀），计划是数据（8 要素执行计划 + 五查 + 活计划修订）；场景 skill 注册匹配、命中定协作方式。

## 2. 硬冲突（评审必须二选一或给出融合规则）

### 冲突 1：用户工具面——功能阶段 vs 模态产物

| | 方案 A | 方案 B |
|---|---|---|
| 用户看到 | 六个**工序**，全部可显式选择（总览 §3.1；README §2） | 四个**产物方向**（主文档 §2.1 路由面）；分析/提示词/装配内化为 node（详设 §1）；故事板门控进入非直呼（主文档 §6.9#2） |
| 心智模型 | 用户按"制作工序"思考 | 用户按"我要什么东西"思考 |

直接对撞点：A 的 `write_prompts`/`analyze_materials`/`assemble_output`/`plan_storyboard` 都是用户工具；B 已明确裁决"装配不作为独立 GraphTool""加工不单设路由项"（主文档 §6.9#3、#5）。

**评审必答 Q1**：用户工具箱按工序切还是按产物切？（产品裁决；两者也可并存——产物为主入口、工序为高级入口——但需明确主次与引导策略）

### 冲突 2：编排哲学——固定 DAG + 逐 Turn 人推 vs 动态四档

- 方案 A：六 Tool 内部全部**设计期固定 DAG**（各设计 §4，模型不组装、不改连线，改流程=发新版本）；**没有总编排者**——主 Agent 每 Turn 选一个 Tool（总览 §3.1.2），链路"不是强制向导"（总览 §3.3）；**不存在**复杂度分流、模板命中、沉淀、执行计划概念；对"一句复杂需求（如'做个 30 秒 MV'）"**没有自动分解路径，且不允许一句话静默自动成片**（总览 §6.3 每阶段正式 Approval）。
- 方案 B：M1 需求矩阵 → 复杂度门 → M2 命中/skill 匹配 → M3 计划层组合/兜底连线 → 8 要素执行计划 → 活计划修订 → 沉淀（主文档 §2.4、§3）；一句话复杂需求是**核心场景**。

结构性事实：**A 的六 Tool ≈ B 的"档 1 固化流程"；B 的档 2/3/4 在 A 中整体不存在。**

**评审必答 Q2**：产品要不要支持"一句复杂需求 → Agent 自动组合编排（含审批卡点）"？要，则六 Tool-only 不满足需求，须补路由/计划层；不要，则 B 的动态层降为远期。

### 冲突 3：Skill——同词不同物

| | 方案 A 的 Skill | 方案 B 的场景 skill |
|---|---|---|
| 本体 | **平台核心产品对象**：用户结构化表单创建、审核、不可变发布、市场变现（用户需求总览 §2、§4；W1 已实现） | 编排知识层：复杂场景题材经验包（主文档 §6.10） |
| 选择域 | 用户先在 Project **启用**（≤16），AI 在**启用集内**选（用户需求总览 §2.2） | Agent 在**全注册库**按需求匹配（主文档 §2.1 匹配面） |
| 内容 | 可 `@` 引用平台 Tool（调用规则/流程包倾向） | 选骨架 + 题材化排布 + 注经验，**不定义连线**（主文档 §2.1 粒度防回潮） |
| 介入时机 | Published Snapshot 注入 Tool 可信上下文（契约 §3.2 `skill_invocation_id`） | **先匹配 skill → 命中定协作方式**，复杂度门兜底（主文档 §6.10 入口顺序） |

隐性对撞：B 的"skill 先于路由决定走哪条链"撞上 A 的路由铁律"用户显式选择冻结 `requested_tool_key`，不得静默替换"（总览 §3.1，GTL-USE-001）。

**评审必答 Q3**：(a) Skill 一词最终指什么？建议显式分层命名（如市场 Skill=UGC 产品对象；场景 skill=平台编排经验），否则文档层面持续歧义。(b) 用户显式选 Tool 与 skill/Agent 匹配的优先级顺序是什么？

### 冲突 4：计费——失败补偿退回 vs 默认不退款

同频的部分先说清：**两边都是"事前扣"**（A：`Prepare*` 立即扣费，BIZ-AIGC-003/016；B：预留腿在前，主文档 §1"扣费三条腿"）。真正冲突在**失败/取消之后**：

- 方案 A：**默认不退款**（契约共同约束；总览 §5.4 明确"该扣费不是 Reservation"），`reserved` 一词仅指 Asset 占位；退钱唯一窄路=权威证据证明外部执行从未开始 → 人工显式冲正。
- 方案 B：**补偿退回**是三腿之一（失败/取消走 billing.compensation 事件链，主文档 §0 理念 5、§1、§6.9#2）。

**评审必答 Q4**：生成失败/取消，用户积分退不退？（产品/财务裁决，技术两边都能实现；A 侧文档自身也标注 primary+correction 报价与退款规则待财务冻结——plan_creation_spec §11）

### 冲突 5：卡点恢复——同计划原地恢复 vs Graph 即弃新 Turn 重进

- 方案 A：审批一律**结束当前 Graph**（`waiting_user`），批准后新 Continuation Turn 复用原 `tool_call_id`、从 START 确定性重进 pinned Graph，"不保留跨分钟 Graph 栈"（契约 §2.3；Runner 评审 §2.2、AGT-RUN-005）；异步生成同理（`accepted` → Terminal Outbox → 新 Turn）。
- 方案 B：卡点=node 挂起态，PlanRun suspended → 唤醒 → **同一计划原地 resume**（主文档 §3.6；已有 orchestration 代码验证过该语义，现为历史参考）。

缓和事实：两边"等待不占会话通道"原则一致（B"挂起即释放" ≈ A"waiting_user 不占活跃 Run"）。冲突只在恢复载体：durable PlanRun vs 无状态 Graph + 业务状态机。

**评审必答 Q5**：W2 运行时以哪个恢复语义为正统？（工程裁决；若选 A 且后续要支持 B 的计划层，需定义"执行计划"如何映射到 Turn/Continuation——计划状态存 Business/Agent 聚合、每步仍走 A 的 Graph 生命周期，是一条已知可行路径）

## 3. 不对称差异（一方有、一方无；不需二选一，但需决定要不要）

- **B 有 A 无**：复杂度门 / M1 需求→产物矩阵 / 引导话术=模板输入要素清单 / 沉淀（档 4）/ 活计划修订（中途改需求→影响评估→最小返工）。A 最接近的等价物只有被动的 `DEPENDENCY_NOT_READY`。
- **A 有 B 无**（对案的欠账，评审时应直接认领而非辩护）：全套回执/幂等/unknown-outcome 纪律——ToolReceipt first-write-wins、Approval Decision/Consumption Receipt 逐字段验签、Lease/Fence、`quarantined` 隔离、错误码闭集、Evidence 门禁。B 在此颗粒度为空白。

## 4. 同频底座（评审可直接冻结为公共决策，不必争论)

单 ChatModelAgent；Tool=compose.Graph 非子 Agent/AgentAsTool；PostgreSQL 权威、Redis 只唤醒；先审批后副作用、自然语言"确认"无效；事前扣费；服务端稳定 Element/Slot ID（=B 的 element_id 锚点寻址）；Revision 不可变 + `preserve_element_ids`（=B 的 favorite 锁定/已确认产物保护）；单镜头重做不重跑全流程；部分成功分别展示（A Batch Barrier ≈ B SuccessPolicy）；`accepted` + 终态回流开新回合（≈ B waiting_jobs+唤醒）；provider 原始负载不进模型上下文；**故事板只定 Slot 不写 Prompt / 提示词独立生成 / 生成执行无 ChatModel**（= B 的分镜 node、提示词 node、生成 node 内核切分）。

## 5. 融合形态建议（非结论，供评审起点）

两方案是**互补错位**：A 的六 Tool 内部结构 ≈ B 词汇 node 的固化组合；A 缺需求侧路由与动态层，B 缺执行侧工程纪律。建议融合方向：

1. **A 的运行时契约（Receipt/Approval/幂等/Job Contract）做统一底座**——B 无异议且直接受益；
2. **A 的六 Tool 降级为"固化 GraphTool 库"的初始成员**（即 B 的档 1），保留其全部内部设计；
3. **B 的路由/匹配层（M1 需求矩阵 + 复杂度门 + 场景 skill 匹配 + 模态入口）盖在上面**，作为自然语言入口的编排前端；用户显式选 Tool 仍走 A 的 `requested_tool_key` 冻结路径（回答 Q3b 的一种方式：显式选择 > skill 匹配）；
4. **B 的档 2（计划层组合）以"执行计划=数据、每步=一次 A 式 Tool/Graph 调用、卡点=A 式 Approval Continuation"重新落地**，不复用旧 orchestration 代码（其语义已被 e2e 验证，作行为参考）；档 3/4（兜底连线/沉淀）列为后续版本；
5. **计费三腿之争按 Q4 财务裁决**，落谁的模型都不影响其余融合。

## 6. 评审必答问题汇总

- [ ] Q1 用户工具箱：按工序（六 Tool）还是按产物（四模态）切？可否并存、谁主谁次？
- [ ] Q2 要不要"一句复杂需求 → Agent 自动组合编排"？（决定动态层去留与优先级）
- [ ] Q3a Skill 命名分层：市场 Skill（UGC 产品）与场景 skill（编排经验）是否拆成两个词？
- [ ] Q3b 用户显式选 Tool vs skill/Agent 匹配的优先级？
- [ ] Q4 生成失败/取消退不退积分？（补偿腿 vs 默认不退款）
- [ ] Q5 W2 恢复语义正统：无状态 Graph+Continuation vs durable PlanRun？若前者，执行计划如何映射？
- [ ] Q6 若采纳融合形态（§5），六 Tool 与模态入口/动态层的实现先后顺序？
