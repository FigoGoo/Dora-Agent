# Dora 项目开发计划（Canonical）

> 状态：Active / 项目唯一执行口径
>
> 更新日期：2026-07-16
>
> 当前实现事实基线（不含本计划文档提交）：`codex/w2-r04-consumption-audit@97ea5c73`

## 1. 文档职责与冲突处理

本文只回答三个问题：项目当前真实完成到哪里、下一批先做什么、哪些工作尚未获准开始。后续上下文必须先读取本文，再按本节路由到详细文档。

| 文档或证据 | 唯一职责 | 不得据此推断 |
| --- | --- | --- |
| 本文 | 当前状态、执行顺序、暂停项和上下文交接 | 代替领域设计或 Owner 批准 |
| [全功能冒烟开发推进计划](full-function-smoke-development-plan.md) | M0～M5、SMK-P0、需求覆盖和长期 backlog | 某项已经获准开工，或历史批次仍是当前优先级 |
| [全功能冒烟架构与推进审计](../design/cross-module/full-function-smoke-architecture-audit-2026-07-15.md) | 目标架构、阶段依赖、反模式和推荐方案 | 推荐方案已经 Approved 或已实现 |
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
| W2 Contract / Review Freeze corpus | `评审中` | 已有大量 test-only evaluator、raw Git object 和 GitHub reported commit 双读投影候选 | Owner authority、正式 trust root、生产 consumer、真实 builder / toolchain closure 和 Formal Freeze |
| W2 Structured Smoke Harness | `未解锁` | ADR-009 与工程设计仍为 Draft / awaiting owner approval / candidate_unactivated | 七方 Owner 批准、activation blockers 和受信解锁迁移 |
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

1. 收口 ADR-001/002/005/008/010/011 与 R00/R01/R02/R03/R04 的 Owner 结论。
2. 明确首切计费授权唯一模式、Approval/Consumption 字段映射和生产 projection owner。
3. 对 ADR-009 单独取得七方 Owner 批准；未批准前继续禁止 Harness 目录。
4. 将 test-only corpus、正式 Review Freeze、版本化 trust root 和生产实现四种状态分开记录。
5. 只有相应 Gate 显式 Approved 后，才把下面第 5 节对应阶段改为可实施。

## 5. W2 及后续长期顺序

下表统一长期排序；详细字段和退出条件以架构审计第 9 节及领域设计为准。

| 顺序 | 阶段 | 进入门禁 | 可交付结果 |
| --- | --- | --- | --- |
| 1 | W2 架构与契约决策 | 所需 Owner 明确批准 | 唯一写模型、摘要域、计费模式和 Action/Effect 映射 |
| 2 | Structured Smoke Harness parity | ADR-009 Approved / unlocked | 仅迁移既有 W0 API + Chromium parity，不宣称 W2 完成 |
| 3 | Agent Session Lane Kernel | R02 + ADR-001/002 Approved | PostgreSQL HOL、lease/fence、scanner、Redis lost-wake 恢复 |
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

> 按 `docs/requirements/project-development-plan.md` 接管项目，先核对 P0～P3 已完成事实和两个用户 workflow 改动，再进入 P4 W2 Owner 决策与解锁审计。先读取 AGENTS.md 和所需 Skill/规范；不得用生产代码、test-only corpus 或本地通过替代 Owner 批准。

交接时的本地事实：

- 分支：`codex/w2-r04-consumption-audit`
- W1 Governor 前端实现提交：`9da070b4`（契约/API）、`ea193502`（页面/资源路由）、`11beb86a`（权限导航集成）。
- W1 Governor 浏览器与 Governance Evidence v2 提交：`97ea5c73 test(smoke): 覆盖Skill治理浏览器链路`；实际 HEAD 以新上下文执行 `git rev-parse --short HEAD` 的结果为准。
- canonical 已连续通过 run `20260715T185511Z-69894` 与 `20260715T185627Z-75733`；后者 Governance v2 九项全真，四份 Evidence 共用同一 run/source/Business/Agent digest。
- 用户自有未提交改动：
  - `M .github/workflows/w2-contract-governance.yml`
  - `?? .github/workflows/w2-review-freeze-transition.yml`
- W1 Governor P0～P3 已完成；完整 `SMK-031` 仍为 `待实现`，不得由本子切片整体转绿。
- W2 Structured Smoke Harness 仍未解锁；下一窗口不得创建其受限目录。

本文在以下情况必须更新：当前纵切变化、真实状态变化、Owner 审批变化、门禁解锁、SMK 状态变化或交接基线提交变化。
