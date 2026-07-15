# Dora 项目开发计划（Canonical）

> 状态：Active / 项目唯一执行口径
>
> 更新日期：2026-07-16
>
> 当前实现事实基线（不含本计划文档提交）：`codex/w2-r04-consumption-audit@7860467b`

## 1. 文档职责与冲突处理

本文只回答三个问题：项目当前真实完成到哪里、下一批先做什么、哪些工作尚未获准开始。后续上下文必须先读取本文，再按本节路由到详细文档。

| 文档或证据 | 唯一职责 | 不得据此推断 |
| --- | --- | --- |
| 本文 | 当前状态、执行顺序、暂停项和上下文交接 | 代替领域设计或 Owner 批准 |
| [全功能冒烟开发推进计划](full-function-smoke-development-plan.md) | M0～M5、SMK-P0、需求覆盖和长期 backlog | 某项已经获准开工，或历史批次仍是当前优先级 |
| [全功能冒烟架构与推进审计](../design/cross-module/full-function-smoke-architecture-audit-2026-07-15.md) | 目标架构、阶段依赖、反模式和推荐方案 | 推荐方案已经 Approved 或已实现 |
| [W2 P4 Owner 决策收口包](../design/cross-module/w2-owner-decision-closure-v1.md) | ADR/R00～R04 ballot、Projection Owner、Structured Smoke trust root 路线和待签核清单 | 推荐候选已经 Accepted、Owner 已签字或生产实现已解锁 |
| [W2-R00 Owner 决策矩阵](../design/agent/w2-r00-owner-decision-matrix-v1.md) | R00-D01～D14、Billing open-item crosswalk、ballot readiness、Owner 候选与关闭证据 | incomplete candidate 已可接受、R00 请求/canonical manifest 已生成或 Billing 已解锁 |
| [W2-R00 候选准备请求](../design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json) | 14 项 readiness 基线、六项 incomplete candidate 的缺失输入/证据、前置/跨 Gate 对齐与失败关闭能力 | 任一实际候选值、Owner role/option、candidate evidence、R00 待决请求、批准或实现解锁已经形成 |
| [W2-R00 D09 ModelReceipt 候选输入](../design/agent/approvals/w2-r00-candidate-inputs/R00-D09-v1/candidate-input.json) | Provider-neutral terminal ModelReceipt exact schema、canonical/条件 presence、uniqueness/Finalize 纯契约向量；状态仅为 `prepared_unregistered` | 真实 Provider 能力、正式 IDL/PG 证据、Owner request/approval、candidate evidence、ballot readiness 或生产实现已经形成 |
| [W2-R01 Owner 待决请求](../design/agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json) | R01/GOV 十二项混合 readiness、当前 partial candidate、provisional roles、live Gate 与失败关闭摘要 | D05/D06 已可接受、任一选项已被选择、87 向量已成为完整 baseline、build/trust closure 已形成或 Gate 已前移 |
| [W2-R02 Owner 决策矩阵](../design/agent/w2-r02-owner-decision-matrix-v1.md) | R02-D01～D19、PG/TC crosswalk、Owner 候选、A1 边界与 aggregate 前置 | 任一语义已经决定、aggregate 已生成、R02 已冻结或生产 A1/A2 已解锁 |
| [W2-R02 Owner 待决请求](../design/agent/approvals/w2-r02-owner-decision-requests/DR-W2-R02-v1.json) | 19 项推荐候选、provisional Owner roles、失败关闭 blocker 与绑定摘要 | 任一选项已被 Owner 选择、approval summary/authority 已形成或 Gate 已前移 |
| [W2-R02 legacy-upgrade v2 child manifest](../../agent/tests/contract/testdata/w2_r02_upgrade_v2/manifest.json) | 从未修改的 v1 manifest/corpus 机械派生 3 fixtures/107 vectors/14 tests 排序 exact-set；状态仅为 `prepared_unregistered` | live candidate evidence/aggregate 已登记、Owner 已批准、R02 已冻结或 A1/A2 已解锁 |
| [W2-R03 Owner 待决请求](../design/agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json) | 14 项推荐候选、零 candidate Gate、provisional Owner roles 与失败关闭 blocker | 任一选项已被 Owner 选择、child/aggregate 已形成、authority 已激活或 Gate 已前移 |
| [W2-R04 Owner 待决请求](../design/agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json) | 20 项推荐候选、当前 partial candidate 绑定、provisional Owner roles 与失败关闭 blocker | partial candidate 已变成完整 baseline、任一选项已被选择、authority 已形成或 Gate 已前移 |
| 领域 Design / ADR / Review Freeze | 契约、状态机、Schema、Owner 决议和实现门禁 | 代码已经存在或 Smoke 已通过 |
| 当前代码、Migration、黑盒 Evidence | 已实现事实 | 尚未运行的目标能力 |
| `AGENTS.md` 与项目 Skills | 工程规范、验证和提交规则 | 产品范围或 Owner 业务决策 |

发生冲突时按以下规则处理：

1. “已经实现”只由当前代码、Migration 和可重复 Evidence 证明；文档目标不能覆盖实际代码。
2. “是否允许实现”由对应 Design / ADR / Owner 审批状态决定；本文不能解除门禁。
3. “下一步先做什么”以本文为准；旧计划中的“下一批”、Batch 编号和历史提交台账不再具有调度权。
4. 需求范围仍以六份需求总览和验收 ID 为准；本文只安排顺序，不删除需求。

## 2. 统一状态枚举

项目计划、评审和交接只使用以下状态：

| 状态 | 含义 |
| --- | --- |
| `已完成` | 生产路径已实现，并有与风险相称的自动化或黑盒证据 |
| `已批准待实现` | 对应设计已 Frozen / Approved，但代码或真实 Smoke 尚未完成 |
| `评审中` | 方案或 corpus 可评审，但未获得全部所需 Owner 批准 |
| `未解锁` | 明确受审批门禁禁止创建生产代码、Migration、IDL 或指定目录 |
| `延后` | 方向有效，但不在当前关键路径；不得抢占当前纵切 |
| `历史参考` | 只用于迁移或追溯，不描述当前分支能力 |

`test-only candidate`、`corpus closed claim`、`Expansion Frozen` 和 `Review Ready` 一律归入 `评审中`，不能写成 `已完成` 或 `已批准待实现`。

## 3. 2026-07-16 当前事实

| 范围 | 统一状态 | 当前事实 | 仍缺少 |
| --- | --- | --- | --- |
| 三 Module 基础 Runtime | `已完成` | Business、Agent、Worker 独立 Module、Migration、配置、健康检查、构建和本地基础设施已存在 | Worker 业务消费与 W2 Runtime |
| W0 / W0.5 身份、QuickCreate、Workspace Transport | `已完成` | 真实 Auth、Project、Agent Ensure、Snapshot/SSE、重启/Reset/跨 Owner Chromium Evidence 已通过 | Chat/A2UI/Storyboard/Asset 的 W2 业务投影 |
| W1 Skill Foundation / Reviewer / Snapshot v2 | `已完成` | Owner Builder、Reviewer 队列/详情/批准、Business→Agent 不可变 Skill Snapshot 和真实 Chromium 主链已通过 | 驳回、撤回和完整管理员 RBAC |
| W1 Skill Governance 后端 | `已完成` | `skill.govern`、列表/详情/决定 API、Strong ETag、幂等回执、审计和 `active → suspended → active → offline` 黑盒链已通过 | Owner 原因安全投影、申诉和在线角色管理 |
| W1 Market / Public Market Binding | `已完成` | 匿名列表/详情、跨 Publisher QuickCreate v2、治理可见性、TOCTOU 和冻结快照 Evidence 已通过 | 收藏、搜索、指标和已有 Project 追加 Skill |
| W1 Governor 前端纵切 | `已完成` | strict parser/API、三态列表、只读详情、内存命令账本、capability 路由、Creator/Reviewer 隔离和 Governor Chromium 暂停→恢复→永久下架→同 Cookie 撤权均已通过 | Tool/Graph Tool 治理、申诉、在线角色管理和完整 `SMK-031` |
| W2 Contract / Review Freeze corpus | `评审中` | 已完成 P4 跨 Module 审计与八项 ADR dependency map；R00 已形成 14 项稳定决策，并以严格 CPR 固定六项 incomplete candidate 的缺失输入/证据，但没有提交实际 candidate、Owner option 或 R00 请求。R01 已形成覆盖六项语义与六项治理决定的严格请求，只绑定当前 1 fixture/87 vectors/4 tests partial candidate；R02/R03/R04 分别保留 19/14/20 项严格待决请求。R02 legacy-upgrade v2 已从未修改的 v1 资产机械固定 3 fixtures/107 vectors/14 tests，但仅为 `prepared_unregistered`。R00 CPR、R01、R02 upgrade v2、R03/R04 使用独立交叉守卫的 stdlib-only validator/guard；R00/R03 的 Gate `candidate_evidence` 为空，R04 仍只绑定 4 fixture/111 vectors/11 tests 的 `partial_candidate / candidate_unactivated`；R00～R04 全部为 `expansion_frozen` | R00 六项 exact candidate、ADR-001/002/003/004/005/008/010/011 与 R00～R04 语义结论、R02 upgrade v2 评审/登记、R03 child/R04 full-gate exact-set、aggregate manifest、最终 Owner exact-set、正式 authority/build closure 与 Formal Freeze |
| W2 Structured Smoke Harness | `未解锁` | ADR-009 仍为 `awaiting_owner_approval / candidate_unactivated / implementation_unlocked=false`；七方、十项 blocker、三 PR 与外部激活顺序已明确；API shadow baseline 已相对当前源陈旧 | strict v1 Enterprise 路线或 v2 设计决定、版本化 baseline refresh、七方 Owner 批准、trust root activation 与 compound unlock |
| W2 Session Lane / Runner / Receipt / Approval / Billing / A2UI | `未解锁` | 仅有设计与 test-only corpus；生产 Runtime 尚不存在 | 对应 ADR/R00～R08 批准、Migration、Repository、Runner 和真实纵切 |
| 六个 Graph Tool | `未解锁` | 静态目录存在，六项均显式 unavailable；设计仍未全部通过实现门禁 | 当前 Tool 的独立批准、生产 Graph、计费、Approval 和 Evidence |
| Worker 媒体链 | `延后` | 基础 Runtime 已有，尚无 Job claim/lease/provider/finalize | W2 同步黄金纵切稳定后再进入 W3 |
| 支付、收益和产品宽度 | `延后` | 需求基线存在 | W2/W3 运行闭环后按 W4/W5 纵切实现 |

结论：W1 Governor 前端纵切及其真实浏览器证据已经收口。当前唯一队列进入 **P4 W2 Owner 决策与解锁**；在所需 ADR/R00～R08 和 Owner 门禁明确批准前，不以生产代码或横向 test-only corpus 代替决策。

## 4. 当前唯一执行队列

同一时刻只允许一个主纵切处于 `in_progress`。后续窗口不得跳过进入门禁，也不得把长计划中的并行建议理解为提前创建未批准目录。

### P0：上下文与工作树接管

状态：`已完成（2026-07-16）`

1. 读取根 `AGENTS.md`、本文及本批涉及的 Skill / Module 规范。
2. 确认分支和 HEAD；若基线已经前移，以实际提交为准并更新本文交接区。
3. 保留用户自有的 `.github/workflows/w2-contract-governance.yml` 修改和 `.github/workflows/w2-review-freeze-transition.yml` 新文件，不读取为已批准事实，不纳入其他提交。
4. 确认不存在上一窗口遗留的半成品 Governor 前端文件；若存在，先审计再继续，禁止盲目覆盖。
5. 运行与目标范围相称的基线测试，记录失败是否为既有问题。

退出标准：工作文件 Owner 清楚、基线可复现、没有把用户改动混入计划提交。

### P1：W1 Governor 前端功能闭环

状态：`已完成（2026-07-16）`

独立文件线可以并行，但共享路由和 App 集成只能由一个 Integration Owner 收口：

1. `frontend/src/features/skillGovernance/` 增加 exact-key DTO parser、fixtures 和对抗测试。
2. API Client 只调用 `/api/v1/admin/skill-governance` 正式接口，严格携带 Cookie、CSRF、Strong `If-Match` 与原 `Idempotency-Key`。
3. 列表覆盖 `active/suspended/offline` 筛选、loading、empty、error/retry、分页和跨页去重。
4. 详情只读展示 current published `SkillDefinitionV1`；动作按钮严格来自 `allowed_actions[]`，`offline` 不显示恢复入口。
5. 复用内存命令账本：unknown outcome 使用原 Key/ETag/语义重试；409 废弃旧命令并刷新权威详情；401/403、登出、同用户新 Session 和 capability 变化清理命令。
6. 接入 `/admin/skills/governance` 与 `/:skill_id`；非法 UUIDv7 零 API，`skill.govern` 与 `skill.review` 完全分离。
7. Creator/Reviewer 无治理导航且直达失败关闭；Governor 无 Reviewer 隐式权限。
8. 完成契约、API、组件、路由和 App 集成测试，再运行全量前端 test/build。

建议提交：`feat(frontend): 接入Skill治理管理页面`

完成事实：契约/API、页面和路由集成已分三次提交；前端 37 个测试文件、441 个测试和生产构建通过。

### P2：W1 Governor 真实浏览器冒烟

状态：`已完成（2026-07-16）`

1. 在现有 W1 真实 Runtime/Chromium 门禁中注入独立 Governor 测试身份，不增加生产测试后门。
2. 浏览器通过正式列表和详情找到已发布 Skill，依次执行暂停、恢复和永久下架。
3. 校验请求 Header、响应 Body/ETag、状态/epoch/allowed actions、终态按钮和页面只读 Definition。
4. 校验 Creator 与纯 Reviewer 的路由/API 隔离、Governor 撤权后的同 Cookie 重新解析，以及浏览器不访问 `/api/aigc/**`。
5. 从 PostgreSQL 只读派生迁移、回执、审计、current published pointer 和既有 Session 冻结事实；Evidence 不保存凭据、Cookie、CSRF、原幂等键、ETag、reason 或 approval reference。
6. 若扩展 Governance Evidence 字段，必须升级 Schema 版本并同步 exact-set verifier、README 和计划；不得静默扩容 v1。
7. 运行 `make ... w1-smoke`，连续通过后再更新状态。

建议提交：`test(smoke): 覆盖Skill治理浏览器链路`

完成事实：Governance Evidence 已显式升级为 `w1.skill-governance.smoke.evidence.v2`，九项 assertion exact-set 全真；canonical 连续通过 run `20260715T185511Z-69894` 与 `20260715T185627Z-75733`，两次均由同一 source digest 和当前 worktree Business/Agent 二进制生成。

### P3：W1 状态收口

状态：`已完成（2026-07-16）`

1. 更新 README、本计划和详细 SMK 计划中的当前事实。
2. 只把真实页面和 Chromium 已证明的子切片标记为完成；完整 `SMK-031` 仍等待 Tool/Graph Tool 治理，不得整体转绿。
3. 执行差异审计、Secret 扫描、相关 Module/前端门禁并分目的提交。

### P4：W2 Owner 决策与解锁

状态：`评审中 / 当前唯一执行队列 / 不允许用代码代替批准`

P1～P3 不依赖 W2 批准。它们完成后，W2 只推进以下决策工作：

1. 按 [W2 P4 Owner 决策收口包](../design/cross-module/w2-owner-decision-closure-v1.md) 取得 ADR-001/002/003/004/005/008/010/011 与 R00/R01/R02/R03/R04 的结构化 Owner 结论。
2. 对首切 `authorization_mode=preauthorized` 推荐候选作接受/拒绝决定；若拒绝，先设计并批准独立 billable Core，禁止运行时双模式。
3. 冻结 candidate activation 的 `approval_type/action_type/decision_action/consumption_action/effect_kind/ref slot` exact mapping，以及 Agent/Business/Frontend 的生产 projection Owner。
4. 关闭 `P4-C01`～`P4-C14`。R00 已建立 [`R00-D01`～`D14`](../design/agent/w2-r00-owner-decision-matrix-v1.md)，并以严格 [CPR](../design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json) 固定六项 incomplete candidate 的缺失输入/证据；CPR 不提交候选值且不得生成 R00 接受请求。当前仅 [`R00-D09`](../design/agent/approvals/w2-r00-candidate-inputs/R00-D09-v1/candidate-input.json) 增加 Provider-neutral、`prepared_unregistered` 的 terminal ModelReceipt 输入包；它不登记 live candidate，且真实 Provider、跨 Gate、IDL、PG 证据继续缺失。R01 的 [`R01-D01`～`D06` 与 `GOV-D01`～`D06`](../design/agent/w2-r01-owner-decision-matrix-v1.md) 已形成严格无批准能力的 [R01 请求](../design/agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json)，只绑定当前 partial candidate。R02/R03/R04 继续使用 [`R02-D01`～`D19`](../design/agent/w2-r02-owner-decision-matrix-v1.md)、[`R03-D01`～`D14`](../design/agent/w2-r03-owner-decision-matrix-v1.md)、[`R04-D01`～`D20`](../design/agent/w2-r04-owner-decision-matrix-v1.md) 及各自严格请求；R02 legacy-upgrade v2 已机械准备 3 fixtures/107 vectors/14 tests exact-set，但不登记 candidate evidence 也不创建 aggregate。Owner 仍须逐项裁决全部 stable decision 与既有 `PG-Dxx/TC-Pxx`，再审核/登记 R02 upgrade v2、补齐 R00 exact candidate、R03 child/R04 full-gate exact-set 和 aggregate manifest；ADR-003/004/011 还必须关闭 Activation identity、双向 Authority Query 与 slot mapping。最终 Owner exact-set 只能由批准后的范围反推。
5. 对 ADR-009 单独决定 strict v1 Enterprise 路线或新 v2；版本化刷新已陈旧 API shadow baseline，再依次完成 Candidate-Preparation、Bootstrap、外部 activation/canary 和独立 compound Unlock。
6. 将 test-only corpus、正式 Review Freeze、版本化 trust root 和生产实现四种状态分开记录。
7. 只有相应 Gate 显式 Approved 后，才把下面第 5 节对应阶段改为可实施。

2026-07-16 审计结论：八项 ADR 仍只有推荐候选；R00～R04 的 `freeze` 均为空。R00 已建立 14 项稳定 ID 和严格候选准备 CPR；D09 只有 `prepared_unregistered` 的 Provider-neutral 输入包，其余五项仍只有准备清单，六项均未形成 live candidate submission，且没有 R00 Owner 请求。R01 已形成十二项严格待决请求，但 87 条向量仍只是 `partial_candidate`，x/text/toolchain/trust root 未闭合。R02 legacy-upgrade v2 已机械固定既有 3 fixtures/107 vectors/14 tests，但状态仅为 `prepared_unregistered`；R02/R03/R04 的 19/14/20 项请求同样没有选择/批准能力；R00/R03 的 Gate `candidate_evidence` 为空，R04 的 111 条向量仍只是 `partial_candidate / candidate_unactivated`。aggregate、R03 child、R04 full-gate manifest 与 Owner approval 均未完成；ADR-003/004/011 的 identity/query/slot 依赖仍待裁决；ADR-009 的 `implementation_unlocked=false`，十项 activation blocker 全部开放。因此 P4 保持 `评审中`，生产实现与 Harness 继续失败关闭。

## 5. W2 及后续长期顺序

下表统一长期排序；详细字段和退出条件以架构审计第 9 节及领域设计为准。

| 顺序 | 阶段 | 进入门禁 | 可交付结果 |
| --- | --- | --- | --- |
| 1 | W2 架构与契约决策 | 所需 Owner 明确批准 | 唯一写模型、摘要域、计费模式和 Action/Effect 映射 |
| 2 | Structured Smoke Harness parity | ADR-009 Approved / unlocked | 仅迁移既有 W0 API + Chromium parity，不宣称 W2 完成 |
| 3 | Agent Session Lane Kernel | R02 + ADR-001/002 Approved；`R02-D19` 已冻结 ADR-008/010 对 A1 的适用范围并满足其结论 | PostgreSQL HOL、lease/fence、scanner、Redis lost-wake 恢复 |
| 4 | Agent execution/ref/projection | Lane 完成，R01/R02 + ADR-001/002/008/010 Approved | 生产写模型、Receipt projection、崩溃恢复 |
| 5 | Business 最小计费 | R00 + ADR-005 Approved | Prepare/Get/Finalize、唯一 Charge/Ledger/Receipt |
| 6 | Business Creation Spec Candidate | 整个 R04 Approved | Save/Get Candidate 与 unknown-outcome Query |
| 7 | `plan_creation_spec` Local Fake | 上述 Agent/Business 阶段完成，R03/R04 Approved | 首个 headless Graph、Approval/Continuation、零重复调用/扣费 |
| 8 | 正式 A2UI 与 Project Workspace | R08 + ADR-007 Approved | 版本化 A2UI、Action、Snapshot/SSE/刷新恢复 |
| 9 | 首条 Chromium 黄金链 | 阶段 7～8 完成 | `plan_creation_spec` 真实三 Runtime 本地确定性冒烟 |
| 10 | 真实 ChatModel 与同步 Tool 宽度 | 每个 Tool 独立 Approved | 逐个切换 availability，不批量开放 |
| 11 | W3 Worker 异步媒体 | Job/Event/Finalize 契约 Approved | Claim/Lease/Provider/Finalize/Continuation 与恢复 |
| 12 | W4/W5 账务、支付和产品宽度 | 黄金创作链稳定 | 钱包、充值、收益、公开作品、公告和完整管理治理 |
| 13 | 全量 Smoke / 发布加固 | 35 条 SMK-P0 真实可执行 | Local + Sandbox、回归、压测、安全、灰度和回滚 |

## 6. 当前暂停与禁止项

在本文后续更新前，以下规则失败关闭：

- 暂停继续扩展 W2 artifact source derivation、build-closure、SBOM、GitHub projection 等横向 test-only Batch；现有候选保留，不删除、不夸大。
- 现有 BuildInfo、调用方 JSON 或 raw Git membership 不能证明 artifact 确由受控源码、工具链和 builder 产生；相关工作只有在正式 trust/build closure 被重新排入关键路径后恢复。
- ADR-009 未解锁前禁止创建 `smoke/**`、`test-adapters/**`、`deploy/local-smoke/**`。
- 对应 W2 ADR/R00～R08 未 Approved 前，禁止创建生产 Runner/Graph/Approval/Billing/A2UI 的 Go、SQL Migration、IDL 或生成代码。
- 任一 Agent-facing Graph Tool 未满足独立中文设计与审核门禁前禁止实现。
- 不恢复旧 `/api/aigc/**`、旧根 Go Module、旧内存真源或跨 Module `internal` import。
- 不修改、不暂存、不提交用户自有的两个 W2 workflow 文件，除非用户在新上下文明确授权。

## 7. 通用完成定义

每个实现批次必须同时满足：

1. 当前事实、目标形态、test-only candidate 和 Approved 能力分开表述。
2. 契约 parser、业务状态、权限负向、幂等/unknown outcome 和资源身份均有测试。
3. 前端覆盖 loading、empty、error、permission denied、processing、terminal 和恢复路径。
4. 受影响 Go Module 在 `GOWORK=off` 下完成规范要求的 mod verify、vet、test、race 和 build；纯前端批次完成 `npm test` 与 `npm run build`。
5. 真实纵切使用正式 API、真实 Runtime 和 PostgreSQL；Fake 只允许位于已批准的外部 Adapter 边界。
6. Evidence exact-set、版本、run/source/binary/Migration 绑定和脱敏规则明确；失败运行不能发布 passed。
7. 一次提交只处理一个目的，使用 `<type>(<scope>): <中文摘要>`，不夹带用户或无关改动。
8. README、本文和详细计划只在真实交付后更新，不预写完成状态。

## 8. 下一上下文交接

新上下文建议使用以下开场指令：

> 按 `docs/requirements/project-development-plan.md` 接管项目，先核对 P0～P3 已完成事实和两个用户 workflow 改动，再读取 `docs/design/cross-module/w2-owner-decision-closure-v1.md`、R00～R04 的 Owner 决策矩阵与现有严格待决请求，继续 P4 Owner 语义 ballot、R00 incomplete candidate、aggregate 准备与 authority 路线收口。先读取 AGENTS.md 和所需 Skill/规范；不得用待决请求、生产代码、test-only corpus 或本地通过替代 Owner 批准。

交接时的本地事实：

- 分支：`codex/w2-r04-consumption-audit`
- W1 Governor 前端实现提交：`9da070b4`（契约/API）、`ea193502`（页面/资源路由）、`11beb86a`（权限导航集成）。
- W1 Governor 浏览器与 Governance Evidence v2 提交：`97ea5c73 test(smoke): 覆盖Skill治理浏览器链路`；实际 HEAD 以新上下文执行 `git rev-parse --short HEAD` 的结果为准。
- W2 P4 跨 Module 审计与 Owner 决策收口包：`2927486a docs(w2): 汇总Owner决策收口包`；它只整理 ballot、冲突和 unlock 条件，不代表任何批准。
- W2-R02 Owner 决策矩阵：`c450544f docs(agent): 建立R02 Owner决策矩阵`；它只稳定 `R02-D01`～`D19`、Owner 候选、关闭证据与 aggregate 前置，不代表任何语义结论或解锁。
- P4 ballot 依赖修复：`5f0ca38f docs(w2): 补齐P4 ballot依赖`；补入 ADR-003/004、`P4-C13/C14` 和 A1 的 `R02-D19` 门禁，不代表任一 ADR 已接受。
- R02 Owner 待决请求：`63999145 test(agent): 固定R02 Owner待决请求`；原始摘要为 `4b6356f9d6b4da7adf348c2207135e2cebd8c972349f84c67ade274f6d274fe9`，只固定候选输入和失败关闭 validator，不记录选择、批准或平台身份。
- R02 legacy-upgrade v2 完整集合：`7860467b test(agent): 固定R02升级语料完整集合`；manifest 原始摘要为 `3b11053b2a3d7de283f0f437f3afe9d161cbb316ae74e981754c0d012404d134`，只从未修改的 v1 资产机械派生 3 fixtures/107 vectors/14 tests 排序 exact-set，状态为 `prepared_unregistered/not_requested/prohibited`，不登记 live candidate、不生成 aggregate 或实现解锁。
- R03/R04 Owner 决策矩阵：`c9fc5f40 docs(agent): 建立R03 Owner决策矩阵`、`8d9cb205 docs(agent): 建立R04 Owner决策矩阵`；只稳定 14/20 项待决语义。
- R03/R04 Owner 待决请求：`1e6bf728 test(agent): 固定R03 R04待决请求`；原始摘要分别为 `d0e229c8b2fbaaee21b67a87155d6f9607f08e581d36419ae3833ae65b2d7c6d`、`d8806af1289aff1b8a790bdbf861c97a7c348f70aa83213760bdc28b318cd0e7`，共享互相交叉守卫的 stdlib-only validator/guard，只绑定失败关闭输入，不记录批准或实现解锁。
- R00 Owner 决策矩阵：`956ac483 docs(agent): 建立R00 Owner决策矩阵`；稳定 `R00-D01`～`D14` 并保留六项 incomplete readiness，原始摘要为 `0a0a0968a136c4c21d054a4c75e0f7996850c9885a758d8af3bc2364246abb30`，当前明确禁止生成 R00 请求。
- R00 矩阵测试：`3e9f7ecf test(agent): 固定R00待决矩阵`、`59b8b2b5 test(agent): 补强R00待决矩阵`；固定稳定 ID、12 项 crosswalk、逐项 readiness 与未登记 Owner 边界，不构成 Billing candidate。
- R00 候选准备契约：`9f9d4c77 test(agent): 固定R00候选准备契约`；CPR 原始摘要为 `6d9cd4a033d19c127fcfec04e975abdcb047247dcaa56c9fd381068b6977c836`，只固定六项缺失输入/证据、完整 readiness 和失败关闭 validator，不提交实际候选值、Owner 请求、candidate evidence 或实现解锁。
- R00-D09 ModelReceipt 候选输入：`8ea78c2b test(agent): 固定R00 D09模型回执候选`；candidate/vector 原始摘要分别为 `f4d4f16d41d567041ccde282d61ff6faee89e9266242b97052e1ffa736028bbd`、`4620fe379ee169790cf8268d2602c4ce1ea19dad68865716821f07657733216d`。双 stdlib-only validator/guard 固定 4+20+4+5 条 contract/state 向量、safe integer/ASCII canonical/Provider authority 配对与源码 exact-set；状态仍为 `prepared_unregistered/not_requested/prohibited`，不登记 live candidate 或解锁实现。
- R01 Owner 待决请求：`e658fbcc test(agent): 固定R01待决请求`；原始摘要为 `676c4f83a1e7570c5ac41e3d0ffc8556fb936b0b363b93a6c7b79b2da7552018`，D05/D06 分别保持 candidate incomplete/scope derivation，独立 validator/guard 从 corpus 派生并绑定 1 fixture/87 vectors/4 tests partial candidate，不记录批准、compile attestation 或实现解锁。
- Review Freeze clean-runner 可重复性维护：`e7aeb1fc test(agent): 规范化x/text模块信息夹具`；只在宿主 Go cache 进入隔离测试 fixture 时精确识别 51-byte minimal 与 191-byte official Origin 两种 `.info` 表示，并统一收敛为既有 51-byte canonical leaf。v1 validator、snapshot digest/size、15-leaf、12/2/1 purpose、Gate/manifest/claim 和 `expansion_frozen / partial_candidate` 状态均未改变；PR #1 clean runner 的 `review-freeze` 已通过，`module (agent)` 只剩 ADR-009/W2-S0-G0 的 `4816 want 4457` 既有失败关闭。
- canonical 已连续通过 run `20260715T185511Z-69894` 与 `20260715T185627Z-75733`；后者 Governance v2 九项全真，四份 Evidence 共用同一 run/source/Business/Agent digest。
- 用户自有未提交改动：
  - `M .github/workflows/w2-contract-governance.yml`
  - `?? .github/workflows/w2-review-freeze-transition.yml`
- W1 Governor P0～P3 已完成；完整 `SMK-031` 仍为 `待实现`，不得由本子切片整体转绿。
- W2 R00～R04 均为 `expansion_frozen`；八项 ADR 和 R00～R04 的稳定决定都没有 Owner 结论。R00 六项 candidate incomplete，只有无值、无 ballot 的 CPR 且无 Owner 请求；R02 upgrade v2 仅为 `prepared_unregistered`，不构成 live candidate 或 aggregate；R01/R02/R03/R04 四份严格待决请求不构成批准，仍分别缺 full-gate、aggregate、child、full-gate baseline 与 Owner approval；ADR-003/004 的 R03/R04 依赖未裁决；ADR-009 仍未解锁，API shadow baseline 已陈旧；下一窗口不得创建受限目录或生产 W2 实现。

本文在以下情况必须更新：当前纵切变化、真实状态变化、Owner 审批变化、门禁解锁、SMK 状态变化或交接基线提交变化。
