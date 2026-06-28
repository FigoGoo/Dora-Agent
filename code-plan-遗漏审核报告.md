# code-plan 逻辑遗漏审核报告

> 审核对象：`code-plan/`（agent 15 份 + business 17 份实现设计文档，初审时在 `codex/follow-up-development`，现已并入 `cc-yr`）
> 基准源：`docs/product/prd/**`、`docs/product/**`、`docs/architecture/**`、`docs/standards/**`、`docs/contracts/**`
> 方法：6 路并行审核，每域跨 agent+business 两侧，对照产品 PRD / 架构 / 规范 / 契约 + 承重墙/接缝不变量 + 跨服务接缝一致性
> 日期：2026-06-27　审核方：Claude Code（cc-yr）
> 说明：本报告基于设计文档静态审核，行号引用以审核时分支为准；逐条请对最新文档复核。**总评：计划质量高、覆盖全，遗漏集中在物理落盘链、安全 fail-closed 旁路、确认恢复事件、Skill 完整定义、模型选择契约、企业空间归属、数据底座、接缝字段命名。**
> ⚠️ 注：现 `services/agent`、`services/business` 已有初版实现，本报告是**设计层**遗漏；落到初版代码后，每条需对照实现复核「是否已修 / 是否同样缺 / 是否引入新偏差」。

## 统计概览

| 严重度 | 数量 | 性质 |
|---|---|---|
| 🔴 高 | 10 | 致 bug / 阻断开发 / 架构洞 / 越权红线 |
| ⚠️ 接缝硬冲突 | 7 | 不修则 codegen/联调当场崩（契约冻结前必清） |
| 🟡 中 | 22 | 成体系缺口，非阻断但需补 |
| 🔵 低 | 11 | 收尾项 / 显式化防回归 |
| 📌 待确认边界 | 1 | 范围问题，需主控/产品裁决 |

类别图例：需求=PRD 功能点漏设计；不变量=承重墙未落实；接缝=agent↔business 不一致；边缘case=异常/边界路径漏；越权红线=权限边界缺；矛盾=文档间冲突。

---

## 🔴 高优先遗漏

- **GEN-1　生成资产「下载产物→上传 TOS→产出 object_key」整段无主**（不变量/边缘case）。`CommitGeneratedAssetAndCharge` 假定 TOS 对象「事务前已可验证」，但无文档说明 generated 资产 TOS 对象由谁/何时产出；`CreateUploadIntent` 只服务用户直传，agent 13 只产 artifacts 不下载上传。model-infra「raw 先落库→解析→下载/解码→上传 TOS→入库」掉进接缝，铁律②「先存」物理实现缺失。→ agent 13 + business 10。
- **GEN-3　`additional_input` 恢复后无强制重评安全**（安全 fail-closed 旁路）。补充输入携带新提示词，resume 链路只 `CheckProjectAccess`，`EvaluateSafety` 仅首轮。可注入未评估提示词进生成。→ agent 05 + 09。依据：内容安全设计 L68。
- **GEN-2　`EstimateGenerationCredits.safety_evidence` 契约冲突**（接缝，见 SEAM-2）。business 09 含、agent 07+business 15 不含。→ 二选一冻结。
- **TURN-1　`resume.accepted`/`confirmation.accepted` 事件全链路缺失**（矛盾）。5 份基准源都要求 resume 成功发此事件，code-plan 05/06 零落地；前端确认面板 accepted 态、恢复刷新依赖它。→ agent 05(emit) + 06(payload)。依据：AG-UI规范:20、TurnLoop规范:33、arch01:155。
- **TURN-2　标准基础事件 vs 生产事件命名未对账，`interrupt.required` 映射悬空**（矛盾）。arch01 挂起的待确认项未处置。→ agent 06 新增「事件命名对齐」节。
- **SKILL-1　Skill「人工确认规则」未持久化、未进 runtime spec**（需求）。PRD 列 P0 必填，`skill_versions`/`GetPublishedSkillSpec` 不存不返回，运行期退化为 Tool 级确认。→ business 08 + agent 08。
- **SKILL-2/3　输出元素「草稿/最终双态」+ 14 类内置字典 seed 双缺位**（需求）。element schema 只到 type/required；字典表建了无种子内容（上线即空）；SQL 清单缺 `asset_element_types`。→ business 08/10/14 + agent 12。
- **SKILL-4　用户选「非默认模型」无对应业务 RPC**（接缝，见 SEAM-4）。agent 要 `ResolveModelForRun(model_id)`，business 06 只有 list+default；agent 自身三处命名不一致。→ business 06 + agent 07/08/03。
- **ACCT-1/2　个人 Skill 在企业空间的归属与可用性规则全缺**（需求/不变量）。(a)「产物归企业+扣企业积分」未成文；(b)「绑定 Tool 不符企业白名单→该 Skill 不可用」未提。→ business 03/05/08 + agent 08。依据：账户体系 L119-127、PRD01 L151-152/L296。
- **WORK-1　企业作品分享/取消分享缺「创建者本人+仍具企业身份」鉴权**（越权红线）。business 12 只判 `owner`，被移出企业的原作者仍可管理企业作品。→ business 12。依据：PRD12 权限规则。
- **INFRA-1　业务库全库缺软删/统一审计公共列基线**（不变量）。business 全 14 文档 0 命中 `deleted_at`，逐表各写各的。→ business 02 立全表公共列+软删语义 + business 14 Done Gate。依据：数据建模规范 L61。

---

## ⚠️ 接缝硬冲突（契约冻结前必清）

| ID | 字段/能力 | agent 侧 | business 侧 | 处置 |
|---|---|---|---|---|
| SEAM-1 | 积分账户标识**四名混用** | `credit_account_type` | `_scope`(枚举)/`_id`(主键)/`_ref` | 统一命名，澄清要枚举还是主键 |
| SEAM-2 | `EstimateGenerationCredits.safety_evidence` | 无 | 有 | 二选一冻结 |
| SEAM-3 | `user_status` | 想读字段 | 用 `PERMISSION_DENIED` 错误码 | 裁决返字段 vs 返错误码 |
| SEAM-4 | 模型快照解析 RPC | `ResolveModelForRun`/`ResolveModelSnapshot` | 仅 list+default | 补 RPC 或声明复用 + 统一命名 |
| SEAM-5 | `SaveSkillTestResult` 服务归属 | `SkillReviewService` | thrift 在 `SkillCatalogService` | 统一服务名 |
| SEAM-6 | `final_elements[]`/`asset_refs[]` | `list<map<string,string>>`；`asset_refs` | 强类型 `GeneratedAssetElementInput`；`committed_assets[]` | 统一强类型+返回名 |
| SEAM-7 | 元素类型 RPC 项类型 | 需结构化 `schema_hint` | jsonb `schema_hint` | thrift `list<map<string,string>>` 容不下，改具名 struct(`schema_hint`,`sort_order:i32`) |

附：`skill_scope_keys[]`(business 输出)↔`skill_scope_filter`(agent 入参)无映射；`ResolveCurrentSpace`(agent 01) vs `ResolveCurrentSpaceContext`(余处)笔误——一并对齐。

---

## 🟡 中优先遗漏

| ID | 遗漏点 | 应落 |
|---|---|---|
| GEN-4 | safety evidence 在「长任务→commit」窗口过期只有硬失败、无重评续跑路径 | agent 09 |
| GEN-5 | 评估的是路由后 prompt、发模型的是最终组装 prompt，digest 一致性无断言（评 A 发 B） | agent 08+13 |
| GEN-6 | worker/进程重启恢复执行的安全姿态空白 | agent 05/13 |
| GEN-8 | 资产元素必填缺失在 commit 端的冻结分额释放口径不闭合 | business 11+agent 09 |
| SKILL-5 | 「测试通过≠审核通过」无断言/状态机守卫 | business 08 |
| SKILL-6 | 模型显示名可重名、靠 model_id 选——未显式声明（防误加唯一约束/按名匹配） | business 06 |
| SKILL-7 | 用户选模型后到预估之间被停用的运行期分支缺处理 | agent 08+business 06 |
| SKILL-8 | `model_generation` 类 Tool「不叠加费」agent 侧未显式拦截 | agent 08/13 |
| TURN-3 | `additional_input` 过期落点 `waiting_expired` 是 run 状态机非法态 | agent 05 或 03 |
| TURN-4 | 「timestamp 仅展示、不作排序」06 公共规则未复述 | agent 06 |
| TURN-5 | 06 payload 契约表缺一批事件行（`message.completed` 固化/`credits.charged/released`/`tool.call.completed/progress`） | agent 06 |
| TURN-6 | Composer 锁定时机未锚定（`required` 锁 vs `accepted` 锁，PRD 与契约草案有差异未裁决） | agent 06/05 |
| TURN-7 | 「改模型→先取消确认再重估」状态回退未成闭环 | agent 05 |
| TURN-8 | snapshot 的 `interrupt` 字段在 agent 04 `SnapshotResponse` 缺失（断线恰在待确认态恢复不出确认面板） | agent 04 对齐 10 |
| INFRA-2 | `agent_safety_evaluations` 是第 11 表、超 10 表白名单，business 14 边界测试名单未同步 | agent 03+business 14 |
| INFRA-3 | 业务侧无指标定义（counter/histogram/gauge 全缺） | business 02/14 |
| INFRA-4 | 结构化日志字段集不完整（agent 11 只 5 字段） | agent 11+business 02 |
| INFRA-5 | 无 OTel/traceparent 跨服务 trace 串联机制 | agent 11+business 02 |
| INFRA-6 | 业务错误分类未对齐 7 类（塌成 auth/business/system 三桶） | business 02 |
| INFRA-8 | 配置化「策略/提示词/模型参/Skill 白名单进配置」agent 侧无系统落点 | agent 11/03 |
| INFRA-11 | business 状态机未要求「流转矩阵+非法跳转测试」成文 | business 各域+14 |
| INFRA-12 | `idempotency_records` 缺 `tenant_id`/`space_id` 隔离 | business 02 |
| WORK-2 | 后台下架未触达作者站内信（`notify_author` 落空、13 无下架通知类型） | business 12+13 |
| WORK-3 | 作品分类未校验「内置单选+active 字典」 | business 12 |
| WORK-4 | 最后一个 active 管理员未守护，叠加 seed 静默复活→锁死/误重置风险 | business 04 |
| WORK-5 | 后台 7 模块（系统Skill/模型/Tool/积分发放/兑换码/审核队列/资产元素类型）owner 归属未声明 | business 04+各域 |
| WORK-6 | 下架后「重新编辑→再分享」状态流转缺（作者可能永久锁死 taken_down） | business 12 |
| WORK-7 | `AdminUserDetailDTO.spaces[]` 字段边界过粗（未逐字段白名单） | business 04 |
| ACCT-3 | 企业积分入口可见性分级（owner 看全/member 看本人）未成文 | business 03+09 |
| ACCT-4 | 成员「运行中被移出企业」的 active run 处置未成文 | business 03+agent 05 |
| ACCT-5 | agent 侧缺「run/session.space_id 与当前空间一致性」自检闸 | agent 04+00 |

---

## 🔵 低优先遗漏

| ID | 遗漏点 | 应落 |
|---|---|---|
| GEN-7 | `EstimateGenerationCredits` 无幂等键，确认前重复预估产生孤儿 estimate | business 09+agent 09 |
| SKILL-9 | 二级兜底「不推销建 Skill」未显式成规则/测试 | agent 08 |
| SKILL-10 | `tool_bindings[]` 未标注「需运行期逐个复检白名单」 | agent 08 |
| TURN-9 | `agent.thinking.*` 可见性/降级约束（visibility=public、不泄推理链）缺 | agent 06 |
| TURN-10 | `payload_schema_version` 与「已知事件新增字段」旧前端降级读法未定义 | agent 06 |
| TURN-11 | 契约源 `contracts/data` 仍用旧 `interrupted` 命名，与 code-plan 03 冲突（回改源） | contracts/data |
| INFRA-7 | agent 错误分类同样非 7 类（无 category 维度） | agent 11 |
| INFRA-9 | agent 11 自述「定义配置化边界」但正文只有 .env 表 | agent 11 |
| INFRA-10 | 四层配置覆盖链未落地为加载器契约，etcd 非敏感层缺 | business 01/15+agent 11 |
| INFRA-13 | 审计表不可变性（append-only/禁 UPDATE/DELETE）未声明 | business 02 |
| WORK-8 | 管理员「不能直接改用户密码」红线未在 04 显式成文 | business 04 |
| WORK-9 | 审计 `business_action` 枚举未对齐 PRD 11 模块清单 | business 04+各域 |
| WORK-10 | 点赞 `like_count` 并发一致性/不可为负/防刷未明确 | business 12 |
| ACCT-6 | 未登录访客边界在 03/05 未承接 | business 03/05 |
| ACCT-7 | 登录弹窗承接保留意图（未登录→登录回原意图）03 无字段/流程 | business 03 |
| ACCT-8 | 平台管理员越权红线本域未对称成文（交叉引用 04） | business 03 |
| INFRA-14 | `ListAssetElementTypes` page_size 上限 50 而非默认 10，fixture 未标显式差异 | agent 07/14 |

---

## 📌 待确认边界问题

- **BND-1**：model-infra 引擎前置不变量「MV 必须有封面」「DeepSeek 默认不进作品库」在生成闭环五份文档未体现。需确认是否属本计划范围；若属，应在 agent 13 补；若超范围可忽略。建议向主控/产品裁决。

---

## 按 code-plan 文档归位索引

**agent/**：03→INFRA-2,TURN-3｜04→TURN-8,ACCT-5｜05→TURN-1/3/7,GEN-3/6,ACCT-4｜06→TURN-1/2/4/5/6/9/10｜07→SEAM-1~7,SKILL-4｜08→SKILL-1/4/5/7/8/9/10,GEN-3/5,ACCT-1/2｜09→GEN-1/3/4/7/8｜11→INFRA-4/5/7/8/9｜12→SKILL-2,SEAM-7｜13→GEN-1/5/6,BND-1｜14→INFRA-14

**business/**：02→INFRA-1/3/4/5/6/12/13｜03→ACCT-1/2/3/4/6/7/8,SEAM-1/3｜04→WORK-4/5/7/8/9｜06→SKILL-4/6/7,SEAM-4｜08→SKILL-1/2/5,SEAM-5｜09→GEN-2/7,ACCT-3,SEAM-2｜10→GEN-1,SKILL-3,SEAM-7｜11→GEN-1/4/8,SEAM-2/6｜12→WORK-1/2/3/6/10｜13→WORK-2｜14→INFRA-1/2/11/14｜15→SEAM-2,INFRA-10

---

> 处置顺序：先清 ⚠️ 接缝硬冲突（7 条，契约冻结前必须）→ 再补 🔴 高优先（GEN-1 落盘链、GEN-3 安全旁路、TURN-1 resume 事件、SKILL-1/2/3 Skill 定义、ACCT-1/2 与 WORK-1 越权）→ 中低优先按域随切片认领。
