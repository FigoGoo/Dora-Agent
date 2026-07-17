# Dora 功能优先开发与试跑计划

> 状态：Active / 唯一开发排期口径
>
> 更新日期：2026-07-17
>
> 路径保留说明：文件名沿用 `full-function-smoke-development-plan.md` 以保持历史链接稳定；本文已从“所有生产门禁前置”调整为“主要功能优先、生产细节后置”。

关联文档：[用户端需求总览](user-requirements-overview.md)、[管理端需求总览](admin-requirements-overview.md)、[服务端需求总览](server-requirements-overview.md)、[Graph Tool 功能需求总览](graph-tool-requirements-overview.md)、[共通业务规则与验收基线](common-requirements-baseline.md)、[Graph Tool 设计索引](../design/agent/graphtool/README.md)、[全功能冒烟工程设计](../design/testing/full-function-smoke-engineering-design.md)。

## 1. 本文是什么

本文是仓库唯一的开发顺序、阶段状态和“下一步”口径：

- 需求总览回答“最终要有什么”；
- 各领域设计回答“边界和契约是什么”；
- 本文回答“现在先做什么、何时可启动试跑、哪些事项后置”；
- 冒烟工程设计回答“如何形成上线级 Evidence”，不再决定功能何时可以开始开发；
- README 只同步本文的当前阶段摘要，不另建第二套排期。

历史 `M0～M5`、`W0～W6` 只保留为 Evidence 和 Commit 的追溯标签，不再作为新的开发排序依据。

## 2. 推进原则

1. **先形成可用纵切，再扩功能宽度，最后补生产加固。** 每个阶段都必须能启动、能操作、能观察结果。
2. **设计与实现同批收敛。** 已有 Graph Tool 允许在同一开发批次先冻结本批最小设计，再实现和测试；未进入本批的生产能力不阻塞开发预览。
3. **只保留不可返工的前置约束。** 数据 Owner、跨 Module 类型契约、Migration、鉴权/Secret 边界、持久化输入、幂等回执、Session 串行与基本 Fence 不得后置。
4. **明确区分开发预览、试跑和生产可用。** 预览通过不等于计费、公开发布、完整容灾或上线门禁通过。
5. **一条纵切一个验收命令。** 测试数量、设计复选项和文档页数不能替代真实启动与用户路径。

## 3. 当前基线

| 范围 | 当前工作树事实 | 下一阶段门槛 |
|---|---|---|
| Business | CreationSpec、真实 Owner-only 项目、最小文本素材、Storyboard/Prompt Preview、媒体 Preview Asset、内部 Prepare/Query/Finalize、Owner 内容读取与同源 BFF 已在统一主链跑通 | 完整上传、通用 Asset/Evidence、生产服务认证、RBAC 与计费后置 |
| Agent | 普通消息和六个 Graph Tool 已由一个共享 ChatModelAgent、一个 Coordinator、可信 Context 路由与全来源 HOL 装配；媒体 Operation/Batch/Job、Terminal Outbox/Bridge 已跑通 | 保持静态 Catalog `unavailable`；重启恢复、Fence takeover、unknown outcome 和生产模型门禁单独补齐 |
| Worker | Job Claim/Lease/Heartbeat、Business Finalize、确定性 PNG 与固定 argv 的 H.264/yuv420p/2s/faststart MP4 已在真实 Worker 主链完成 | 生产 Provider、跨主机对象存储、完整恢复矩阵、压力与容灾后置 |
| 前端 | 真实项目列表、文本素材、六 Tool 统一入口、Workspace V5、媒体 SSE、PNG/MP4 控件与同源播放已由真实 Chromium 主链验证 | 保持前端全量测试/构建为独立门禁，继续补阻断性错误恢复和可访问性 |
| 工程 | 2026-07-17 `make trial-basic` 已通过三 Module、统一基础/媒体 Profile、六 Tool、Worker PNG/MP4、`200/206/416`、Workspace V5 刷新恢复、受控清理与源码零漂移；Evidence 为 `.local/smoke/trial-basic.json`、权限 `0600` | 五条 isolated smoke、三 Module/前端全量门禁及重启/Fence/故障注入继续独立执行 |

结论：V1 已完成开发预览退出条件。`20260716T100936Z-92447` Trial Evidence 证明真实 PostgreSQL/Redis/etcd、Business/Agent Runtime、Chromium、Agent 重启、幂等重放、durable-command 有界恢复与 blocked-lane 零增量闭环；三 Module 的 `test/race/vet/build` 和前端 `test/build` 同轮通过。该结论只表示 V1 Preview 完成，不表示完整 Graph Tool、计费、Approval、Worker、35 条 SMK-P0 或生产发布完成。

V2 已按顺序完成 `analyze_materials.v2preview1` Tool Core、本地 exact Evidence Adapter、`user_message.runtime.v2preview1` 方案 A，以及独立 [`analyze_materials.runtime.v2preview1`](../design/agent/analyze-materials-runtime-v2-design.md)、[`plan_storyboard.runtime.v2preview1`](../design/agent/plan-storyboard-runtime-v2-design.md) 和 [`write_prompts.runtime.v2preview1`](../design/agent/write-prompts-runtime-v2-design.md) 的 `M0`～`M4` Development Preview。V3 本地媒体纵切也已接入同一 Profile。2026-07-17 的 `make trial-basic` 已从真实登录和项目开始，经过文本素材与六个 Graph Tool，由 Worker 产生可解码 PNG 和可播放 MP4，并在 Workspace V5 硬刷新后恢复；通过后发布 `.local/smoke/trial-basic.json`（`0600`）。生产静态 Catalog 仍未注册。

## 4. 新阶段总览

```mermaid
flowchart LR
    V0["V0 基础链\n已完成"] --> V1["V1 首条可用纵切\n已完成"]
    V1 --> V2["V2 同步主要功能\n已完成"]
    V2 --> V3["V3 异步媒体闭环\n本地 MVP 已完成"]
    V3 --> T1["T1 联调与试跑\n快速主链已通过"]
    T1 --> P1["P1 生产加固"]
```

| 阶段 | 目标 | 退出条件 | 状态 |
|---|---|---|---|
| V0 基础链 | 身份、Project、Skill、Session、Snapshot/SSE、三 Module 可独立运行 | W0/W0.5/W1 现有门禁通过 | 已完成 |
| V1 首条可用纵切 | `Session Input → Runner → plan_creation_spec → Business Draft → Event/SSE → Card` | 本地一条命令启动；真实页面提交目标并看到持久化 CreationSpec Draft；刷新可恢复 | **已完成（Trial Evidence passed）** |
| V2 同步主要功能 | 先以方案 A 关闭通用 `user_message` Direct Response Lane，再分别批准 Tool-enabled Profile；随后把同步能力合入统一本地 Profile，并接入真实素材入口 | 非空 QuickCreate 首输入可恢复终结；四个同步 Tool 可在同一进程、同一 Workspace 依次执行；模型不可用时错误可观察 | **已完成（统一主链已通过）** |
| V3 异步媒体闭环 | `generate_media`、`assemble_output`、Operation/Batch/Job、Worker、Finalize | 本地确定性 PNG 与固定参数真 MP4 全链可运行、可受保护读取/播放 | **本地 MVP 已完成（Trial Evidence passed）** |
| T1 联调与试跑 | 串起创作主流程，修复阻断性体验/一致性问题 | 快速 MVP 主链可重复；更广错误/恢复清单另行收口 | **快速主链已通过；完整联调/恢复门禁继续进行** |
| P1 生产加固 | 计费、正式 Approval、完整 RBAC、沙箱、故障注入、性能、容灾、Provider 与支付上线门禁 | 发布清单和完整回归通过 | 后置 |

## 5. V1：首条可用纵切

### 5.1 用户路径

```text
用户从开启 Preview 的 QuickCreate 创建空 Input Lane
→ 首页目标只在前端进程内一次性预填 Workspace 表单
→ 用户确认结构化创作目标
→ 提交创作目标
→ Agent 先持久化输入并由 Session Lane 领取
→ Eino Runner 驱动唯一 ChatModelAgent / plan_creation_spec Graph
→ FakeModel 生成严格结构化候选，独立 Validator 校验
→ Business 以稳定幂等键保存 CreationSpec Draft
→ Agent 冻结 Tool Result，追加安全 Event/A2UI 投影
→ 前端展示 CreationSpec Card；SSE 重连和硬刷新恢复同一结果
```

### 5.2 V1 必须实现

- Business-owned `CreationSpec Draft` 聚合、前向 Migration、Repository、Service 和版本化 Kitex RPC；
- Agent-owned Input/Turn/Run/ToolReceipt/Projection 最小持久化事实，以及 Session HOL、Lease/Fence 和稳定 ID；
- 唯一 `ChatModelAgent`、Eino Runner、启动时 Compile 的 `plan_creation_spec` DAG；
- 默认确定性 FakeModel，保证 CI/本地不需要付费 Key；模型候选与确定性 Validator/Command 分离；
- 同键同义重放原 Draft/Result，同键异义冲突；Business 写入结果未知时先按原键查询；权威 `not_found` 只有在 Agent 已加密持久化完整规范命令并持有重发预算时，才允许有界重发同一 `command_id + request_digest`，禁止换键；
- 同义入队重放先只读返回冻结 Input，不重新调用内容保护器、随机 ID 或时钟；
- Preview Processor 只消费 `creation_spec_preview`；已有非终态 `user_message` 的旧 Session 返回不可重试 `409 SESSION_LANE_BLOCKED` 且事务零增量，禁止跳 HOL、原地 adopt 或伪造终态；
- 本地 Preview 开启时 QuickCreate 的首页目标不先写成 `user_message`，而以 `initial_prompt=null` 创建空 Lane，仅通过前端进程内一次性交接预填表单；用户确认后才持久化 Preview Intent；
- Business BFF 同源入口、Agent 内部身份校验、CSRF/Owner 校验；
- 最小 CreationSpec Card、EventLog/SSE reducer、Snapshot 恢复和安全未知版本降级；
- 单元、真实 PostgreSQL 契约、模块独立构建和一条浏览器主路径。

### 5.3 V1 明确后置

以下内容不阻塞 V1 开发预览，但在 P1 前不得宣称生产可用：

- 积分估价、扣费、收益归因、退款/冲正和财务对账；
- 模型执行 Approval、CreationSpec 激活 Approval、过期/拒绝/Continuation 完整状态机；
- 真实 DeepSeek 默认启用、模型 Failover、全量 Prompt 评测；
- Correction 二次模型调用、所有 unknown-outcome 故障注入矩阵；
- 完整 A2UI 组件白名单、Card Action、可访问性和性能指标；
- 全量管理员 RBAC、数据范围、导出和敏感字段治理；
- Worker、媒体 Provider、支付、公开发布和生产网络沙箱。

V1 产出的 CreationSpec 固定为 `draft`，不能冒充 `active`，也不能触发媒体任务。静态生产 Tool Catalog 在 V1 完成前继续 `unavailable`；开发预览入口必须显式标记 `preview`。

当前实现已补齐完整命令恢复账本：`PrepareCommand` 加密持久化严格规范的完整 Draft Command、payload digest 与 key version，并冻结每条 Receipt 的独立重发上限；恢复只能先查询原键，只有 Business 权威返回 `not_found` 才以当前 Fence CAS 领取一次预算并重发同一 `command_id + request_digest`。技术查询失败不消耗预算，最终权威 `not_found` 才进入可观察的 `business_resend_exhausted`，Input 保持 `recovery_pending` 并继续阻塞后继 HOL；禁止无限查询、重新调用模型或生成新命令键。Repository/Graph 测试与真实 PostgreSQL 的断链、重启、Fence 和预算耗尽探针均已通过。

### 5.4 V1 验收

V1 只有同时满足以下条件才完成：

1. 空库 Migration Up 后 Business/Agent Ready；
2. 浏览器在 Preview 开关下以 `initial_prompt=null` 创建空 Lane，经进程内一次性交接预填目标并由用户确认；随后经 Business 同源 API 提交，HTTP 只入队，不直调 Graph；
3. Processor 经 Session Lane 和 Runner 完成执行；
4. Business PostgreSQL 存在唯一 Draft，Agent PostgreSQL 存在唯一 Input/Run/Receipt/Event；
5. 同一幂等键重放不新增 Draft、模型调用或 Event，异义请求返回稳定冲突；
6. 页面显示阶段、交付物、约束和验收条件；
7. SSE 断开重连、页面硬刷新和 Agent 重启后读取同一 Draft/摘要；
8. Save 请求在送达前断链时，Agent 从加密持久化命令恢复，并在固定预算内以原 `command_id + request_digest` 重发后收敛；预算耗尽可观察且不换键；
9. 不存在扣费、Approval、Operation/Batch/Job 增量；
10. `GOWORK=off` 的 Business/Agent test、race、vet、build 与前端 test/build 通过；
11. `make plan-spec-preview-smoke` 在干净本地基础设施上可重复执行并输出脱敏 Trial Evidence。

此外必须有真实 PostgreSQL 负向证据：`EnsureProjectSessionV1/V2(nonempty initial_prompt) → Preview POST` 返回 `409 SESSION_LANE_BLOCKED`，原 Message/Input/Receipt/Event 与全部 counter 零变化且同一 Input 不出现第二个 accepted Event。该负向证据只保证不损坏旧 Session，不代表通用 `user_message` Runtime 已完成。

以上条件已全部闭环：本轮 canonical Smoke 使用当前工作树二进制连接真实 PostgreSQL、Redis、etcd，并由真实 Chromium 经 Business 同源入口完成提交、SSE、断线恢复、Agent 重启与硬刷新；正式 Graph/Repository 故障探针验证技术查询不耗预算、权威 `not_found` 后原键有界重发和预算耗尽 HOL。脱敏 Evidence 位于 `.local/smoke/plan-spec-preview-trial-evidence.json`，Schema 为 `plan_spec_preview.trial_evidence.v1`、状态为 `passed`、权限为 `0600`，Run ID 为 `20260716T100936Z-92447`。三 Module 的 `GOWORK=off test/race/vet/build`、前端 401 项测试与生产构建也已通过，因此 V1 标记为完成；该 Trial Evidence 不得被解释为 P1 生产 Evidence。

## 6. V2：同步主要功能

V2 按用户调整后的实现顺序推进，不以基础设施类别横切：

1. **已完成 Tool Core**：`analyze_materials.v2preview1` 先以未注册 Agent Tool Core 闭合 strict Intent、text/image Evidence、Graph、Fake Model、Validator 与 Result，包级 test/race/vet 已通过；
2. **已完成 Evidence Adapter 与最小文本入口**：`BIZ-PREVIEW-004` 的 text/image PostgreSQL 真值、Foundation RPC 与 Agent Mapper保持只读；另以 Owner-only `POST/GET .../text-materials` 创建不可变 text Asset/Evidence，未扩上传、编辑、删除或通用摄取；
3. **Runtime 集成的本地 Fake Profile 已完成**：`user_message.runtime.v2preview1` 方案 A 已经正式 Session Lane 产出可恢复 Direct Response/Failure Card；Executable Tool Registry 固定为空，禁止执行 `analyze_materials` 或 `plan_creation_spec`。新 Ensure Writer/Migration、Legacy Helper/Ledger、统一 HOL/Scanner/Fence、local Fake Receipt、Snapshot/SSE/UI 与最终源码 Run `20260716T202111Z-58305` canonical Evidence 均已完成；
4. **独立 Tool-enabled Profile 已完成**：[`analyze_materials.runtime.v2preview1`](../design/agent/analyze-materials-runtime-v2-design.md) 已完成 M0～M4；只读已持久化 text/image Evidence，不替代旧输入，不公开 Asset ID 文本表单，不写 MaterialAnalysis，生产 Catalog 继续不可用；
5. **独立 Storyboard Profile 已完成**：[`plan_storyboard.runtime.v2preview1`](../design/agent/plan-storyboard-runtime-v2-design.md) M0～M4 已完成；可信 CreationSpec Draft Ref、隔离 Business JSON Draft/Receipt、Foundation RPC、默认不注册 Tool Core/Adapter、typed ingress、HOL/Fence、两层 Receipt、Business BFF、Workspace v3 Snapshot/SSE/Card、表单、硬刷新与 Agent 重启恢复已闭合，生产稳定 Element/Slot、Revision、激活和 Approval 仍未获批；
6. **`write_prompts` M0～M4 已完成**：[`write_prompts.runtime.v2preview1`](../design/agent/write-prompts-runtime-v2-design.md) 已冻结只消费 Storyboard Preview 全部 local Slot 的 exact-set，并完成默认不注册的 15 Node Tool Core、双 Validator、隔离 Business Draft/Receipt、Foundation RPC、Agent Adapter、Runtime/BFF/Workspace/SSE/Card/Form 和 canonical Trial；standalone 作为后续独立 Profile；
7. **统一 Profile 基础装配已完成**：[`mvp_all_tools.runtime.v1preview1`](../design/agent/mvp-all-tools-runtime-v1-design.md) 已实现一个共享 ChatModelAgent、四个同步 Tool Registry、普通消息分支与一个 Coordinator Scanner；再由[六工具媒体扩展](../design/agent/mvp-six-tools-media-extension-v1-design.md)追加两个媒体 Tool，统一主链已通过 `make trial-basic`；
8. **真实基础入口已完成**：Owner-only 项目列表、最小文本素材创建/列表/Picker 与 `analyze_materials` 提交已通过 Business/Frontend 全量和真实 PostgreSQL 验证；
9. **生产模型能力明确后置**：`plan_creation_spec` 的真实 DeepSeek 可选配置、Prompt Registry 和评测基线不属于 local-only MVP 或 `trial-basic` 通过范围；
10. **统一前端交互已完成**：四个同步 Tool 已使用同一类 Tool Card/Resource Card 交互，并由媒体 Card 扩展承接两个异步 Tool。

每个 Tool 仍采用“本批最小设计冻结 → 实现 → 页面主路径 → 失败路径”节奏，不等待其余 Tool 的完整生产评审。

## 7. V3：异步媒体闭环

已按最小可运行顺序完成本地 MVP：

1. 按 [`media.runtime.v3preview1`](../design/agent/media-runtime-v3-preview-design.md) 与[六工具媒体扩展](../design/agent/mvp-six-tools-media-extension-v1-design.md)实现 Agent Operation/Batch/Job/Terminal Outbox、同一主 Agent 入口及 `agent.media_job.preview.v1` 数据库消费契约；
2. Worker Claim/Lease/Fence/Heartbeat，以及 Go `image/png` 确定性 PNG 与固定白名单 `ffmpeg` MP4；
3. Business 本地 Preview Asset `Prepare/Finalize/Query/Range`；本 Profile 固定零计费、零 Approval、零 TOS；
4. `generate_media` 返回 `accepted`，Worker 终态经 Outbox/Inbox 进入 Session Lane；
5. `assemble_output` 先输出真实可下载的测试媒体，再扩真实 Provider；
6. 高层 Tool Card 更新，不向前端泄露底层 Job 列表。

### 7.1 快速 MVP 验收与完整门禁

`make trial-basic` 是快速开发反馈：它在已启动的本地 PostgreSQL、Redis、etcd 上重置三个专用测试数据库，从当前工作树构建并启动 Business、Agent、Worker、Vite 与 Chromium，验证统一基础/媒体 Profile、登录、项目、文本素材、六 Tool、两个 Worker 终态、PNG/MP4、Owner 保护的 `200/206/416` 内容读取、Workspace V5 硬刷新恢复、受控清理和源码零漂移。只有全部断言为真才发布 `.local/smoke/trial-basic.json`，文件权限固定为 `0600`。

该命令不串行运行五条 standalone isolated smoke，也不替代三 Module 的全量 `verify/test/vet/race/build`、前端 `test/build`、Runtime 重启恢复、Fence takeover、unknown-outcome/故障注入、生产 Provider、计费或 Approval 门禁。前者用于快速证明“基本功能可跑通”，后者继续按变更范围和 P1 发布计划独立执行；不得把某一侧通过改写成另一侧通过。

## 8. T1 与 P1 的边界

T1 处理“试跑时阻塞用户完成任务”的问题：主流程断点、错误提示、恢复、资源刷新、基本性能和数据一致性。

P1 处理“上线后必须长期可靠、安全、可运营”的问题：

- 正式计费、Approval、支付和收益；
- 完整 RBAC、审计、保留/删除、Secret/KMS；
- 版本化升级 Business→Agent assertion，把规范 `Idempotency-Key` 和正文 SHA-256 绑定到签名，并在静态绑定校验后消费 Nonce；当前未绑定正文的断言只允许双端 local-only Preview；
- Provider 沙箱/生产适配、限流、熔断、Failover；
- 全故障注入、并发压测、容量、灾备、升级/回滚；
- 完整 A2UI 协议治理、可访问性、浏览器矩阵；
- 135 个验收 ID 的完整追踪和发布 Evidence。

生产细节可以后置，但不能被删除；所有后置项必须留在本节或对应需求追踪矩阵中。

## 9. 多 Agent 分工规则

历史 Analyze Materials 纵切按四条轨道收口；共享 IDL、公共 DTO、文档状态与最终 Smoke 只由主 Agent 合并：

| 轨道 | Owner | 文件边界 | 交付 | 当前状态 |
|---|---|---|---|---|
| V2-AM-C | 主 Agent | 唯一计划、跨 Module 集成、Workspace/SSE、Smoke 与审批状态 | 冻结契约、合并冲突、直接端口联调 | 已完成；Run `20260716T215049Z-39824` passed |
| V2-AM-P | PostgreSQL Agent | Agent Migration/Repository/required-PG 测试 | typed Input、HOL/Fence、Context/Receipt/Projection | 已完成；Migration 001→010 与生命周期通过 |
| V2-AM-R | Runtime/UI Agent | 单 Tool Eino Runtime、Fake Model、Frontend Parser/Reducer/Card | Router→Tool→ReturnDirectly 与只读恢复 UI | 已完成；45 个前端测试文件、450 项用例与构建通过 |
| V2-AM-S | Smoke/Approval Agent | Go localsmoke Helper、Chromium E2E、Corpus/Manifest | 直接 PG/Redis/etcd 证据与严格审批语料 | 已完成；22 向量 Corpus、22/22 Trial 断言通过 |

`write_prompts` 当前按相同的共享文件纪律分轨：主 Agent 独占唯一计划、Profile 设计、审批清单和最终集成；Validator/Artifact Agent 只实现严格 JSON、Scope/exact-set 与摘要；Test Agent 只新增 Tool Core 测试；Review Agent 只读复核。M2 开始后，Business Agent 只负责隔离 Draft/Receipt，Agent Adapter Agent 只负责 Foundation RPC Mapper；共享 IDL/生成代码、Composition Root、Workspace/SSE 与 canonical Smoke 仍由主 Agent 统一接线。

V3 已按目录 Owner 完成并行收口：Agent 轨负责媒体 Graph/Job/Terminal/Workspace；Business 轨负责 Asset/HTTP/Range/BFF；Worker 轨负责 Claim/Artifact/Finalize；主轨负责前端 Workspace V5、根配置、跨 Module parity、唯一计划与 `make trial-basic`。任何后续轨道仍不得复制另一 Module 的 `internal` 类型。

共享 IDL、公共 DTO 和现有 Composition Root 只由主 Agent 合并；各开发 Agent 不修改对方目录，避免并行覆盖。

## 10. 文档统一规则

1. 本文阶段状态是唯一排期真源；其他文档不得写另一套“下一步顺序”。
2. Graph Tool 独立设计可以保留完整生产目标，但必须在顶部标明当前获批的开发 Profile 和生产差距。
3. “已实现”必须有当前工作树代码、Migration 和测试；历史 `main` 资产只能标记为迁移参考。
4. “预览通过”“试跑通过”“生产可用”是三个不同结论，禁止混写。
5. 阶段状态变化时同步更新本文、README 当前状态和 Graph Tool 设计索引；测试工程文档只同步对应 Evidence 入口。
6. 新的 P0 架构风险可以阻塞当前阶段；纯生产细节进入 P1，不得重新反向阻塞已经批准的功能预览。

## 11. 旧口径映射

| 旧口径 | 新口径 |
|---|---|
| W0/W0.5/W1 | V0 已完成基础链与历史 Evidence |
| W2 Agent Runtime/Graph Tool | 拆为 V1 首条纵切 + V2 同步功能宽度 |
| W3 Worker/媒体 | V3 异步媒体闭环 |
| W4 全功能串联 | T1 联调与试跑 |
| W5 管理/支付/安全 | P1 生产加固 |
| W6 完整发布门禁 | P1 末期发布清单，不再阻塞 V1～T1 功能开发 |

## 12. 当前下一步

基本功能主链已跑通，下一步不再扩张 MVP 功能面，而是保持快反馈并分层补质量：

1. 保持 `make trial-basic` 可重复通过，任何影响登录、项目、文本素材、六 Tool、Worker、受保护媒体或 Workspace V5 的改动先修复该主链；
2. `plan-spec-preview-smoke`、`user-message-runtime-smoke`、`analyze-materials-runtime-smoke`、`plan-storyboard-runtime-smoke`、`write-prompts-runtime-smoke` 五条 isolated canonical 回归继续保留并独立运行，不伪装成 `trial-basic` 的内含步骤；
3. 三 Module 的全量 `verify/test/vet/race/build`、前端 `test/build` 与跨 Module 契约检查按变更范围独立执行并留证，不用快速 Evidence 代替；
4. T1 优先补会阻断试用的错误提示和恢复；Runtime 重启、Fence takeover、unknown outcome、故障注入、压力、容灾进入完整质量/恢复门禁；
5. 真实 Provider、生产 Catalog、计费、Approval/TOS、完整上传、通用 Asset/Evidence 与复杂 Batch 继续后置 P1，未经新设计评审不得从 local-only Preview 外溢。
