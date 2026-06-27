# code-plan 逻辑遗漏审核报告

> 审核对象：`codex/follow-up-development` 分支 `code-plan/`（agent 15 份 + business 17 份实现设计文档）
> 基准源（同分支）：`docs/product/prd/**`、`docs/product/**`、`docs/architecture/**`、`docs/standards/**`、`docs/contracts/**`
> 方法：6 路并行审核，每域跨 agent+business 两侧，对照产品 PRD / 架构 / 规范 / 契约 + 承重墙/接缝不变量 + 跨服务接缝一致性
> 日期：2026-06-27　审核方：Claude Code（cc-yr）
> 说明：本报告基于设计文档静态审核，行号引用以审核时分支为准；逐条请对最新文档复核。**总评：计划质量高、覆盖全，遗漏集中在物理落盘链、安全 fail-closed 旁路、确认恢复事件、Skill 完整定义、模型选择契约、企业空间归属、数据底座、接缝字段命名。**

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

### GEN-1　生成资产「下载产物→上传 TOS→产出 object_key」整段无主
- 类别：不变量 / 边缘case
- 现状：`CommitGeneratedAssetAndCharge` 假定 TOS 对象「事务前已可验证」（business 11），但无任何文档说明 **generated 资产**的 TOS 对象由谁、何时产出。`CreateUploadIntent` 只服务用户直传；agent 13 `BuildArtifactsFromTaskResult` 只产 artifacts，不下载/解码/上传。`model-infra` 核心不变量「raw 先落库→解析→下载/解码→上传 TOS→入库」在双服务拆分后掉进接缝。这是铁律②「先存后扣」的「先存」物理实现缺失。
- 应落：agent 13（新增 artifact 落库管线）+ business 10（generated 资产 object_key 签发 RPC，或明确 Agent 服务端直传路径）
- 依据：model-infra 不变量；资产设计「object key 由后端生成」；business 11「TOS 对象应在事务前已可验证」却无生产者

### GEN-3　`additional_input`（补充输入）恢复后无强制重评安全
- 类别：不变量（安全 fail-closed 旁路）
- 现状：补充输入中断恢复后「补 UserInputDTO 后继续路由」，`ResumeInput.ExtraUserInput` 携带新提示词文本；但 resume 链路只列 `CheckProjectAccess`，`EvaluateSafety` 仅在首轮 StartTurn。用户可借此注入未评估的新提示词进入生成，破「Skill/任何路径不可绕过、引擎级」。
- 应落：agent 05（resume 含 ExtraUserInput 时强制回 EvaluateSafety）+ agent 09（主链路标补输入重评分支）
- 依据：内容安全设计 L68「Skill 不能绕过」；agent 05、agent 09 主链路

### GEN-2　`EstimateGenerationCredits.safety_evidence` 契约冲突（亦见接缝 SEAM-2）
- 类别：接缝 / 安全
- 现状：business 09 请求**含** `safety_evidence`；agent 07 struct + business 15 冻结表**都不含**。按 agent Thrift 实现则业务预估端拿不到证据，无法做「安全通过才预估」的服务端兜底。
- 应落：business 09 删字段对齐 Thrift，或 agent 07 加字段——二选一冻结
- 依据：business 09 vs agent 07 + business 15

### TURN-1　`resume.accepted` / `confirmation.accepted` 事件全链路缺失
- 类别：矛盾（与 5 份基准源冲突）
- 现状：resume 成功后必须发该事件作「确认已接受、恢复中」信号，但 agent 05 `ResumeTurn` 从不 emit，agent 06 事件范围/payload 表均无此事件（全 code-plan 零命中）。前端 `confirmation.panel` 的 accepted 态、`workspace.panel` 恢复刷新都依赖它。
- 应落：agent 05（emit 时机：`waiting_confirmation→resuming` 转换点）+ agent 06（payload 行）
- 依据：AG-UI事件规范:20；TurnLoop执行规范:33；architecture/01:155；architecture/00:155；design/07:65

### TURN-2　标准基础事件 vs 生产事件命名未对账，`interrupt.required` 映射悬空
- 类别：矛盾 / 需求
- 现状：arch 01 已挂起待确认项「confirmation.required 与标准 interrupt.required 是否保留映射」，code-plan 通篇未处置；`agent.started`/`message.delta`/`agent.completed` 等标准基础事件名与 `agent.run.started`/`agent.message.delta`/`agent.run.completed` 生产名的兼容口径无人裁决。协议层 SSOT 裂缝。
- 应落：agent 06（新增「事件命名与规范对齐」一节定稿）
- 依据：architecture/01:242；AG-UI事件规范:9-23 vs contracts/ag-ui 事件表

### SKILL-1　Skill「人工确认规则」未持久化、未进 runtime spec
- 类别：需求（Skill 完整定义缺件）
- 现状：PRD 把「人工确认规则（扣费/高风险/业务写入必确认）」列为 Skill 必填内容（P0），但 `skill_versions`/`content_snapshot` 无该字段，`PublishedSkillSpecDTO`/`GetPublishedSkillSpec` 不返回。运行期确认退化为 Tool 级 `CheckToolExecutionPolicy`，Skill 自声明确认意图丢失，审核也无据可审。
- 应落：business 08（`skill_versions` + `PublishedSkillSpecDTO`）+ agent 08（消费）
- 依据：PRD 05「人工确认 P0」；SkillBuilder 产品设计

### SKILL-2 / SKILL-3　输出元素「草稿/最终双态」+ 14 类内置字典 seed 双缺位
- 类别：需求（产品输出元素主线落空壳）
- 现状：(a) PRD 要求输出元素区分草稿态（黑板）/最终态（资产库），每元素声明可编辑/可引用/展示位置；但 `skill_output_element_schemas` 只有 `element_type/required/schema_json/display_order`，agent 12 校验也只到 type/required。(b) `asset_element_types` 表 + 同步机制都在（business 10），但无任何 code-plan 把 PRD 那 14 类（短文本/长文本/富文本/结构化对象/列表/图片引用/音频引用/视频引用/文件引用/提示词/分镜/时间线/标签组/参数组）落为 seed/migration；`第一版迭代SQL清单` 业务首批表也缺 `asset_element_types`，校验依赖的字典可能上线即空。
- 应落：business 08（element schema 增双态+属性）+ business 10/14（14 类 seed 进 migration）+ agent 12（校验算法）
- 依据：PRD 05 输出元素结构节 + 元素类型表（14 行）；SQL 清单缺表

### SKILL-4　用户选「非默认模型」无对应业务 RPC（亦见 SEAM-4）
- 类别：接缝 / Model 不变量
- 现状：agent 要 `ResolveModelForRun(resource_type, selected_model_id, auth_context)→ModelSnapshotDTO`，但 business 06 / thrift `ModelConfigService` 只有 `ListAvailableGenerationModels`（列表）+ `ResolveDefaultModel`（默认），**无按 model_id 解析单模型快照的 RPC**；agent 自身三处命名 `ResolveModelSnapshot`/`ResolveModelForRun`/「计价快照查询」不一致。用户选模型主路径无契约支撑。
- 应落：business 06（新增 `ResolveGenerationModelSnapshot(model_id,resource_type,auth_context)` 或显式声明复用 list 快照）+ agent 07/08/03（统一命名）
- 依据：agent 08、agent 03 L155；business 06 RPC 清单 + agent 07 thrift

### ACCT-1 / ACCT-2　个人 Skill 在企业空间的归属与可用性规则全缺
- 类别：需求 / 不变量（企业空间核心差异点）
- 现状：(a)「个人 Skill 在企业空间执行→产物归企业、扣企业积分」business 03/05 均未成文，会按 `space_id` 一刀切漏掉特殊路径。(b)「个人 Skill 绑定 Tool 不符合企业白名单→该 Skill 在企业空间不可用」business 03 + `ListRoutableSkills` 过滤 + agent 08 路由前置均未提。
- 应落：business 03（归属/可用规则）+ business 08（ListRoutableSkills 过滤）+ agent 08（路由）
- 依据：账户体系设计 L119-127、L126-127、L277；PRD 01 L151-152、L296

### WORK-1　企业作品分享/取消分享缺「创建者本人 + 仍具企业身份」鉴权
- 类别：越权红线
- 现状：business 12 `ShareWork`/`UnshareWork` 鉴权只写 `owner`，全文无「被移出企业后无权管理企业作品」「企业身份下仅创建者本人分享/取消分享」的落地（权限规则/事务/测试均缺）。被移出企业的原作者仍可能管理企业空间作品 → 隐私/治理事故。
- 应落：business 12（权限规则 + ShareWork/UnshareWork 事务前置 + 测试）
- 依据：PRD 12 权限规则；账户体系「企业拥有者不额外管理成员作品」

### INFRA-1　业务库全库缺软删 / 统一审计公共列基线
- 类别：不变量（数据建模）
- 现状：business 全 14 文档 0 命中 `deleted_at`；逐表各写各的审计列（有的有 created_by 有的没有）。projects 用 `archived_at`、works 用 `taken_down` 状态，但用户/成员/资产/Skill 等无统一软删列与「查询默认排除已删」约定。违反数据建模规范审计字段集 + 「删除优先软删」。
- 应落：business 02（全表公共审计列基线 + 软删语义）+ business 14 Done Gate 加一条
- 依据：Agent领域数据建模规范 L61；CRUD「删除优先软删除」

---

## ⚠️ 接缝硬冲突（契约冻结前必清，否则 codegen/联调当场崩）

| ID | 字段/能力 | agent 侧 | business 侧 | 处置 |
|---|---|---|---|---|
| SEAM-1 | 积分账户标识**四名混用** | `credit_account_type` | `_scope`(枚举) / `_id`(主键) / `_ref` | 统一命名，并澄清 agent 要枚举(scope)还是主键(id)——二者语义不同 |
| SEAM-2 | `EstimateGenerationCredits.safety_evidence` | 无（agent 07 + business 15） | 有（business 09） | 二选一冻结（同 GEN-2） |
| SEAM-3 | `user_status` | agent 07 想读字段判禁用 | business 用 `PERMISSION_DENIED` 错误码 | 裁决：返字段 vs 返错误码，统一口径 |
| SEAM-4 | 模型快照解析 RPC（同 SKILL-4） | `ResolveModelForRun`/`ResolveModelSnapshot` | 仅 list + default，无按 id 解析 | 补 RPC 或声明复用 list 快照 + 统一命名 |
| SEAM-5 | `SaveSkillTestResult` 服务归属 | agent 00 写 `SkillReviewService` | thrift 在 `SkillCatalogService` | 统一服务名（联调即错） |
| SEAM-6 | `final_elements[]` / `asset_refs[]` 类型与命名 | thrift `list<map<string,string>>`；`asset_refs` | business 11 强类型 `GeneratedAssetElementInput`；响应 `committed_assets[]` | 统一为一种强类型；返回名统一 |
| SEAM-7 | 元素类型 RPC 项类型 | 消费需 `metadata_schema`/结构化 | business 10 `AssetElementTypeDTO` 含 jsonb `schema_hint` | thrift `list<map<string,string>>` 容不下结构化 schema_hint，改具名 struct（含 `schema_hint`、`sort_order:i32`） |

附：`skill_scope_keys[]`(business 输出) ↔ `skill_scope_filter`(agent 入参) 无映射说明；`ResolveCurrentSpace`(agent 01) vs `ResolveCurrentSpaceContext`(余处) agent 内部笔误——一并对齐。

---

## 🟡 中优先遗漏

| ID | 遗漏点 | 类别 | 应落 | 依据 |
|---|---|---|---|---|
| GEN-4 | safety evidence 在「长任务生成→commit」窗口过期，只有硬失败、无重评续跑路径（与铁律②「已存应扣」张力） | 安全/铁律② | agent 09 | agent 08；business 11:210 |
| GEN-5 | 评估的是路由后 prompt，发模型的是 Skill 最终组装 prompt，二者 digest 一致性无断言（评 A 发 B） | 安全 | agent 08 + agent 13 | 内容安全设计 L38；PRD 10 |
| GEN-6 | worker/进程重启（Redis ZSET 调度）拉起后恢复执行的安全姿态空白（证据复用/失效无定义） | 安全/边缘case | agent 05 或 13 | CLAUDE.md ZSET 调度 |
| GEN-8 | 资产元素必填缺失（`ASSET_ELEMENT_INVALID`）在 commit 端的冻结分额释放口径与错误表不闭合 | 资产/铁律② | business 11 + agent 09 | 资产设计 L306 |
| SKILL-5 | 「测试通过≠审核通过」无断言/状态机守卫（防自动 Published） | Skill 不变量 | business 08 | PRD 05 |
| SKILL-6 | 模型显示名可重名、靠 model_id 选择——业务规则/测试未显式声明（防误加唯一约束/按名匹配） | Model 不变量 | business 06 | PRD 03 |
| SKILL-7 | 用户选模型后到预估之间被停用的运行期分支缺处理 | Model 不变量 | agent 08 + business 06 | PRD 03 异常 |
| SKILL-8 | `model_generation` 类 Tool「不叠加 Tool 调用费」agent 侧未显式拦截（防重复扣费断言） | Tool 不变量 | agent 08 或 13 | business 07 |
| TURN-3 | `additional_input` 过期落点 `waiting_expired` 是 run 状态机里的非法态（永远走不通的补偿路径） | 矛盾（自洽性） | agent 05 或 03 | code-plan 05:134 vs 03:69-71 |
| TURN-4 | 「timestamp 仅展示、不作排序依据」06 公共规则未复述（实现者易误用 timestamp 排序破坏 sequence 合并） | 不变量 | agent 06 | AG-UI事件规范:57-60 |
| TURN-5 | 06 payload 契约表缺一批用户可见 transition 事件行：`message.completed`(固化)、`credits.charged/released`、`tool.call.completed/progress` 等（glob 兜不住字段契约） | 需求/边缘 | agent 06 | AG-UI事件规范:90；contracts/ag-ui |
| TURN-6 | Composer 锁定时机未锚定（`confirmation.required` 锁 vs `confirmation.accepted` 锁，PRD 09 与契约草案本身有先后差异，code-plan 未裁决） | 边缘case | agent 06/05 | prd09:106 vs 契约设计:251 |
| TURN-7 | 「改模型→先取消确认再重估」状态回退未成闭环（解锁+作废 interrupt 回到可重估态的事件序列未串） | 需求 | agent 05 | 契约设计:122-123；prd06:118 |
| TURN-8 | snapshot 的 `interrupt` 字段在 agent 04 `SnapshotResponse`/OpenAPI 基线缺失（断线恰在 waiting_confirmation 时前端还原不出确认面板） | 矛盾（文档间） | agent 04 对齐 10 | code-plan 10:51 vs 04:63,128 |
| INFRA-2 | `agent_safety_evaluations` 是第 11 张表、超 10 表白名单，且 business 14 边界测试白名单未同步（"业务库不出现 Agent 表"会漏检） | 不变量（表白名单一致性） | agent 03 显式登记为扩展 + 回写标准；business 14 同步 | 数据建模规范:16-27；arch02:26-38 |
| INFRA-3 | 业务侧无指标/metrics 定义（counter/histogram/gauge 全 business 0 命中；缺 RPC 成功率/latency、扣费冻结失败率等） | 可观测 | business 02 或 14 | 后端技术栈:99-106 |
| INFRA-4 | 结构化日志字段集不完整（agent 11 只列 5 字段，缺 space_id/turn_id/tool_call_id/task_id/interrupt_id/event_id/rpc_method/latency_ms；business 同） | 可观测 | agent 11 + business 02 | 编码规范:39 |
| INFRA-5 | 无 OTel/traceparent 跨服务 trace 串联机制（trace_id 如何经 Kitex metadata/HTTP header 传播未定义） | 可观测 | agent 11 + business 02 | 后端技术栈:105 |
| INFRA-6 | 业务错误分类未对齐 7 类（user/permission/safety/credit/tool/asset/system），business 02 塌成 auth/business/system 三桶 | 错误处理 | business 02 | 编码规范:33 |
| INFRA-8 | 配置化的「策略/提示词/模型参/Skill 白名单进配置」agent 侧无系统落点（仅 env Tool allowlist + 一个 jsonb），缺配置项→config_key→最小验证集清单 | 配置化 | agent 11 或 03 | 智能体配置化规范:26-40,54-58 |
| INFRA-11 | business 状态机未要求「枚举+允许流转矩阵+拒绝任意跳转+可测试」成文（不像 agent 03 那样有非法迁移测试） | 状态机 | business 各域 + 14 Done Gate | 测试规范:9；数据建模规范:47 |
| INFRA-12 | `idempotency_records` 缺 `tenant_id`/`space_id` 隔离字段（唯一键仅 `(scope,key)`，跨租户可能命中/冲突） | 不变量（租户隔离） | business 02 | 安全规范:22-24 |
| WORK-2 | 后台下架未触达作者站内信：12 的 `notify_author` 落空，13 通知 type 无「作品下架」类型、无 CreateNotification 调用链 | 矛盾 | business 12 + 13 | 12 `notify_author`；PRD 12；13 |
| WORK-3 | 作品分类未校验「平台内置单选 + status=active 字典」（CreateWork/UpdateWork 透传任意 category 串，无校验/非法错误码） | 边缘case | business 12 | PRD 12 |
| WORK-4 | 最后一个 active 管理员未守护（只防"停自身"），叠加 seed 静默复活 → 后台锁死/意外重置风险 | 边缘case | business 04 | PRD 02 异常；04 L95 |
| WORK-5 | business 04 作为「平台后台」唯一 owner，未声明系统Skill/模型/Tool/积分发放/兑换码/审核队列/资产元素类型 7 个后台模块的入口归属与审计聚合点（后台 IA 无主） | 需求 | business 04（索引）+ 各域补后台段 | PRD 02；business README |
| WORK-6 | 下架后「重新编辑→再分享」状态流转缺（状态机画了 taken_down→private 但无 application 函数/事务，作者可能永久锁死） | 边缘case | business 12 | PRD 12 状态机 |
| WORK-7 | `AdminUserDetailDTO.spaces[]`/`enterprise_memberships[]` 字段边界过粗（仅注释"不含私有内容"，未逐字段白名单，易带出 project 列表/资产计数等） | 不变量 | business 04 | PRD 02 用户管理 |
| ACCT-3 | 企业积分入口可见性分级（owner 看账户/流水/账单/成员消耗，member 只看本人）未在 03 权限规则成文 | 越权红线 | business 03 + 09 | 账户体系 L165-166；PRD 01 L142 |
| ACCT-4 | 成员「运行中被移出企业」的 active run 处置未成文（下次 RPC 失败即停？释放冻结？arch-11 只说"后续查询体现"） | 边缘case | business 03 + agent 05 | arch-11 L128；PRD 01 L139 |
| ACCT-5 | agent 侧缺「run/session.space_id 与当前 auth_context.space_id 一致性」自检闸（切换后串用旧空间防护全靠 business 兜底） | 不变量/边缘case | agent 04 + 00 | 账户体系 L169,L274；PRD 01 L168-169 |

---

## 🔵 低优先遗漏

| ID | 遗漏点 | 应落 | 依据 |
|---|---|---|---|
| GEN-7 | `EstimateGenerationCredits` 无幂等键，确认前重复预估产生多条孤儿 estimate，回收/状态流转未定义 | business 09 + agent 09 | agent 07 |
| SKILL-9 | 二级兜底「不推销建 Skill」未显式成规则/测试（当前是"碰巧没写"，建议显式防回归） | agent 08 | 任务基准 |
| SKILL-10 | `GetPublishedSkillSpec` 返回的 `tool_bindings[]` 未标注「需运行期逐个复检白名单」（白名单变更影响已发布 Skill） | agent 08 | Tool 边界设计 |
| TURN-9 | `agent.thinking.*` 可见性/降级约束（`visibility=public`、不泄推理链）06 无 payload 行/约束 | agent 06 | contracts/ag-ui:50-51 |
| TURN-10 | `payload_schema_version` 与「已知事件新增字段」的旧前端降级读法未定义（只讲了未知 type 忽略） | agent 06 | AG-UI事件规范:83-84 |
| TURN-11 | 基准源 `contracts/data/Agent领域数据模型草案` 仍用旧 `interrupted` 命名，与 code-plan 03 的 `waiting_confirmation/resuming` 冲突（提示回改契约源） | 契约源 | contracts/data:49-51 |
| INFRA-7 | agent 错误分类同样非 7 类（agent 11 实为错误码→HTTP/AG-UI 映射表，无 category 维度） | agent 11 | 编码规范:33 |
| INFRA-9 | agent 11 自述「定义配置化边界」但正文只有 .env 表，未 restate `agent_runtime_configs` 治理（版本/状态/owner/回滚/解释器/密钥引用） | agent 11 | 配置化规范:42-52 |
| INFRA-10 | 四层配置覆盖链（默认→.env.example→.env.local→etcd→密钥）未落地为加载器契约；etcd 非敏感配置层缺 | business 01/15 + agent 11 | 本地开发规范:30-37 |
| INFRA-13 | 审计表不可变性（append-only/禁 UPDATE/DELETE）未显式声明 | business 02 | 安全规范审计 |
| WORK-8 | 管理员「不能直接改用户密码」红线未在 04 显式成文（无接口即隐性满足，但缺声明易被误加） | business 04 | PRD 02 |
| WORK-9 | 审计 `business_action` 枚举未对齐 PRD 11 模块清单（04 只列 admin/user 两类动作） | business 04 + 各域 | PRD 02 审计覆盖范围 |
| WORK-10 | 点赞 `like_count` 冗余与 `work_likes` 的并发一致性/不可为负/防刷措施未明确 | business 12 | PRD 12 |
| ACCT-6 | 未登录访客边界在 03/05 未承接（匿名只读、不产生空间/项目/积分、需登录返 UNAUTHENTICATED） | business 03/05 | arch-11 L49；arch-10 L65 |
| ACCT-7 | 登录弹窗承接保留意图（未登录→登录后回原意图）03 无字段/流程 | business 03 | PRD 12（跨域） |
| ACCT-8 | 平台管理员越权红线本域未对称成文（建议 03 交叉引用 04 管理员红线） | business 03 | 账户体系 L70,L36 |
| INFRA-14 | `ListAssetElementTypes` page_size 上限 50 而非默认 10，未在 agent 07/14 fixture 标显式差异 | agent 07/14 | business 15 |

---

## 📌 待确认边界问题

- **BND-1**：`model-infra` 引擎前置不变量「MV 必须有封面」「DeepSeek 默认不进作品库」在 code-plan 五份生成闭环文档中未体现（grep 命中均在 business 05/12 作品域与 agent 03 数据模型，非生成段）。需确认：这些是否属 Dora-Agent code-plan 当前范围？若属，应在 agent 13（封面前置/产物角色）补；若已判定超范围，可忽略。建议向主控/产品裁决。

---

## 按 code-plan 文档归位索引（便于逐文档认领）

**agent/**
- 03（领域模型/DB）：INFRA-2、TURN-3
- 04（API 会话运行）：TURN-8、ACCT-5
- 05（TurnLoop）：TURN-1、TURN-3、TURN-7、GEN-3、GEN-6、ACCT-4
- 06（AG-UI 事件）：TURN-1、TURN-2、TURN-4、TURN-5、TURN-6、TURN-9、TURN-10
- 07（RPC client/DTO）：SEAM-1~7、SKILL-4
- 08（Skill 路由/Tool/Model/安全）：SKILL-1、SKILL-4、SKILL-5、SKILL-7、SKILL-8、SKILL-9、SKILL-10、GEN-3、GEN-5、ACCT-1、ACCT-2
- 09（积分资产闭环）：GEN-1、GEN-3、GEN-4、GEN-7、GEN-8
- 11（日志/观测/配置/测试）：INFRA-4、INFRA-5、INFRA-7、INFRA-8、INFRA-9
- 12（Skill 测试/输出元素校验）：SKILL-2、SEAM-7
- 13（模型 Tool 适配器）：GEN-1、GEN-5、GEN-6、BND-1
- 14（闭眼门禁）：INFRA-14

**business/**
- 02（通用 DTO/错误码/幂等/审计/日志）：INFRA-1、INFRA-3、INFRA-4、INFRA-5、INFRA-6、INFRA-12、INFRA-13
- 03（账户/空间/企业/权限）：ACCT-1、ACCT-2、ACCT-3、ACCT-4、ACCT-6、ACCT-7、ACCT-8、SEAM-1、SEAM-3
- 04（后台/用户管理/审计）：WORK-4、WORK-5、WORK-7、WORK-8、WORK-9
- 06（模型供应商/目录/价格）：SKILL-4、SKILL-6、SKILL-7、SEAM-4
- 08（Skill 目录/版本/审核）：SKILL-1、SKILL-2、SKILL-5、SEAM-5
- 09（积分账户/兑换码/冻结扣减）：GEN-2、GEN-7、ACCT-3、SEAM-2
- 10（资产上传/TOS/元素/预览）：GEN-1、SKILL-3、SEAM-7
- 11（生成资产保存+扣费事务）：GEN-1、GEN-4、GEN-8、SEAM-2、SEAM-6
- 12（作品中心/公开/精选/点赞/下架）：WORK-1、WORK-2、WORK-3、WORK-6、WORK-10
- 13（站内信）：WORK-2
- 14（SQL/测试矩阵/验收）：INFRA-1、INFRA-2、INFRA-11、INFRA-14
- 15（闭眼门禁/Agent 对齐）：SEAM-2、INFRA-10

---

> 建议处置顺序：先清 ⚠️ 接缝硬冲突（7 条，契约冻结前必须，成本低）→ 再补 🔴 高优先（尤其 GEN-1 落盘链、GEN-3/安全旁路、TURN-1/resume 事件、SKILL-1·2·3/Skill 定义、ACCT-1·2 与 WORK-1/越权）→ 中低优先按域随实现切片认领。
