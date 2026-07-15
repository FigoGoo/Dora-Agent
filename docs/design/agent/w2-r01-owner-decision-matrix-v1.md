# W2-R01 Owner 决策矩阵 v1

> 状态：Awaiting Owner Decision
>
> 适用范围：W2-R01 `GraphToolResultV1`、`ToolReceiptV1` 与 Review Freeze 准入
>
> 结论边界：本文只整理待裁决问题、推荐选项和验收证据，不代表任何 Owner 已批准，也不授权生产 DTO、Migration、Repository、RPC、Graph、Runner 或前端联调实现。

## 1. 目标

W2-R01 已有测试专用候选语料，但当前仍存在跨 Module 语义、Owner 范围和审批真实性缺口。继续堆叠向量无法替代这些决定。本矩阵把每个缺口收敛为一个可单独裁决、可被 Review Freeze 验证的决策项，并规定从 `expansion_frozen` 进入正式评审前必须具备的证据。

以下内容必须区分：

- `candidate evidence`：仓库中的设计文档、语料、测试和摘要；
- `owner decision`：受信身份在指定提交上作出的结构化结论；
- `formal approval`：仓库门禁验证身份、权限、提交绑定和完整角色集合后接受的批准。

在 Owner Authority 机制激活前，JSON 中自报的角色、任意 HTTPS URL、提交祖先关系或文档签名栏都只能是候选记录，不能证明正式批准。

## 2. R01 语义决策

| 决策 ID | 问题 | 推荐候选 | 备选与代价 | 必需 Owner | 关闭证据 |
| --- | --- | --- | --- | --- | --- |
| `R01-D01` | `GraphToolResultV1` 遇到未知同主版本可选字段时，是严格拒绝还是兼容忽略 | R01 v1 采用 exact-version、严格拒绝未知字段；未来兼容行为以新 schema/version 和独立迁移矩阵引入 | 同主版本忽略可选字段可降低升级摩擦，但会与当前 strict JSON、canonical digest 和冻结语料冲突 | Agent、Business、安全、测试 | ADR-001/002 统一结论；Catalog、设计文档、语料和测试无冲突 |
| `R01-D02` | `authority_outcome`、failed-after 和 Billing authority projection 是否纳入 R01 | 保留语义目标，但在 Business/Billing 权威 envelope、query contract 和失败优先级独立冻结前，不计入当前 R01 完整候选 | 从 R01 删除可缩小范围，但会失去跨边界 unknown-outcome 的失败关闭模型 | Agent、Business、财务、运维、安全 | 独立契约、Owner、字段 exact-set、正反向量、查询恢复和重放证明齐全 |
| `R01-D03` | `waiting_user.approval_ref.card_id` 由 R01 还是 R08/A2UI 冻结 | R01 仅冻结不可变 Approval 引用；Card ID 的展示生命周期与前端恢复语义交给 R08，R01 只保留双方共同确认的最小因果绑定 | R01 完整持有 Card 语义会扩大 Product/Frontend Owner 范围，并把展示修订耦合到 Receipt digest | Agent、产品、前端、安全 | R01/R08 字段责任表、Card revision/恢复策略和跨契约向量 |
| `R01-D04` | R01 是只冻结 Tool Registry 机制，还是同时冻结六个生产 Tool Key exact-set | 正式 R01 Gate 同时冻结 Registry 机制和六 Tool Key exact-set；当前代表性 slot policy 只能称 partial candidate | 只冻结机制可先推进底座，但不能证明每个 Tool 的 definition/version/ref-slot/side-effect policy 已受审 | Agent、Business、财务、运维、安全、测试 | 六 Tool Key、definition version、slot policy、authority owner、query contract、effect class exact-set |
| `R01-D05` | Approval Consumption slot 的规范名称、Owner、Receipt/segment scope 与 ordinal | 名称和 scope 必须在 R01、R03、R04 和 child Receipt 契约中一次性裁决；当前 Approval/Consumption 代表性 policy 分属不同 Receipt fixture 却同为 ordinal 1，只能保留为 partial candidate | 直接把 `consumption.primary` 写成生产事实，或在未明确 ordinal 是按 Definition、Receipt 还是 execution segment 唯一时自行改成 2，都会污染 digest、重放和 Registry pin | Agent、Business、安全、测试 | 跨文档字段表、唯一 slot 名、Receipt/segment identity、ordinal 唯一性、owner/query contract、迁移与兼容结论 |
| `R01-D06` | R01 `required_owner_roles` exact-set | 先完成 `R01-D02`～`R01-D04`，再由范围反推角色；当前角色数组不得视为已冻结 | 预先冻结角色会遗漏 Frontend/Product 或引入无责任边界的批准者 | 治理 Owner 与全部候选实现 Owner | role→责任→仓库身份映射及缺席/替补规则 |

## 3. Review Freeze 授权决策

| 决策 ID | 问题 | 推荐候选 | 禁止的弱证明 | 关闭证据 |
| --- | --- | --- | --- | --- |
| `GOV-D01` | 如何证明 reviewer 确实拥有某个 Owner 角色 | 在默认分支维护受保护的 `role -> GitHub numeric actor/team authority` 策略；门禁从平台 Review 事件/API 读取 actor、review state、review commit 和不可变 ID | Manifest 自报角色、显示名、邮箱、任意 URL、提交作者或普通评论 | 受保护 authority policy、最小权限读取、未知/停用身份拒绝测试 |
| `GOV-D02` | 批准如何绑定候选提交 | 每个必需角色的有效批准必须绑定当前 PR head；head 更新、review dismissed、request changes 或 authority policy 更新后自动失效 | 只要求 approval commit 是当前提交祖先，或复用旧 PR/旧版本批准 | review freshness 状态机、edited/synchronize/submitted/dismissed 对抗测试 |
| `GOV-D03` | 一个 actor 能否满足多个角色 | 默认一个 actor 只能满足一个 required role；确需兼任时在 authority policy 中显式声明且由治理 Owner 批准 | 验证器自动把一个用户扩散为所有角色 | 唯一匹配算法、兼任白名单和 exact-set 测试 |
| `GOV-D04` | 哪个检查真正阻止合并 | Ruleset 将可信 Review Freeze workflow 设为 required check，并要求分支最新、禁止直推、禁止绕过、只允许 merge commit | 仅在本地运行、仅靠 path filter、未设 required check 或允许管理员静默 bypass | 默认分支上的 workflow、Ruleset 导出/截图、真实 PR 正反演练 |
| `GOV-D05` | 验证器源码如何防止被同一 PR 降级 | workflow 与 Review Freeze verifier 从默认分支 trust root 执行；候选 manifest 绑定设计源和 validator source 摘要；正式候选迁移期间禁止修改 trust root | 从 PR head checkout 后直接执行脚本，或只冻结测试名称不冻结源码 | base/head Git-object 对抗测试、symlink/mode/tree 拒绝、源码摘要闭包 |
| `GOV-D06` | Go 测试包及构建输入能否影响冻结测试 | 优先把每个 Gate verifier 迁入独立、stdlib-only 且无未登记 embed 输入的包；否则冻结完整 transitive build-input closure，包括直接 package source、embed inputs、内部 Go 依赖源码、第三方模块、`go.mod/go.sum`、toolchain 与 workflow/action trust root，不得只绑定若干测试文件或 Module metadata | 允许未绑定的 `TestMain`、`init`、共享全局、新增测试文件、embed 文件、内部依赖源码、第三方依赖、toolchain/action 或 dependency replacement 参与正式 verifier 进程 | 独立 stdlib-only verifier，或完整 transitive build-input manifest；额外源码/embed/内部依赖、dependency replacement、toolchain/action 漂移均有失败测试 |

`GOV-D06` 当前只完成 D0-04A：共享 `contract_test` 包 11 个直接 `.go` 文件与 `agent/go.mod/go.sum` 已形成 exact-set/SHA 绑定，modern/legacy build constraint、Go 忽略/GOOS/GOARCH 平台选择文件名、package-level `TestMain`、`replace`、直接源码增删及已登记文件 mode/symlink 漂移已有失败测试。D0-04B 仍未完成：包内 `//go:embed`、`agent/internal/**` 传递源码、第三方依赖，以及受信 toolchain/workflow action digest 尚未进入闭包。因此当前进展不能证明完整 build closure，`GOV-D06` 仍是 formal Freeze blocker。

## 4. 状态迁移入口

W2-R01 只有同时满足以下条件，才可从 `expansion_frozen` 进入 `awaiting_review`：

1. 当前 corpus、manifest、设计文档和 Review Freeze candidate evidence 的文件摘要、向量数量及 exact-set 完全一致；
2. `design_sources`、`validator_sources` 与 `validator_build_sources` 均为非空、排序、无重复的仓库相对路径，并绑定原始文件 SHA-256；`validator_sources` 必须等于 verifier package 的直接 `.go` 文件 exact-set，`validator_build_sources` 至少等于对应 Module 的 `go.mod/go.sum` exact-set，且 `go.mod` 不含 `replace`；这只是 D0-04A，不能替代 `GOV-D06` 要求的独立 stdlib-only verifier 或完整 transitive build-input closure；
3. `R01-D01`～`R01-D06` 均有结构化结论，所有受影响文档不再互相矛盾；
4. `GOV-D01`～`GOV-D06` 已实现为默认分支 trust root，且真实 PR 的批准、撤销、提交更新、源码/依赖注入和越权场景均失败关闭；
5. `required_owner_roles` 由已裁决范围推导并形成 exact-set，不存在“以后再决定是否追加 Owner”的正式状态；
6. 当前候选仍明确标记为 test-only，未据此创建生产实现或宣称全功能冒烟通过。

进入 `review_frozen` 或 `approved` 还必须在同一候选提交上取得全部角色的有效平台 Review，并由机器门禁验证；人工复制审批链接或手工编辑 manifest 不得推进状态。

## 5. 决策记录格式

每项决定最终应生成结构化记录，至少绑定：

- 决策 ID、选定选项和拒绝选项；
- 受影响契约、Module、字段、状态机、向量和迁移；
- 当前候选 commit SHA 与 contract manifest SHA-256；
- 平台返回的 reviewer numeric ID、review ID、review state、review commit SHA；
- reviewer 对应的受保护 Owner role；
- 决策时间与被替代记录引用。

该记录是门禁输入，不是权限真源。身份与角色归属必须从默认分支受保护策略及代码托管平台事实验证。

## 6. 当前结论

截至本次更新时，R01 仍是 `expansion_frozen / partial_candidate`。Receipt replay 命令形状、Tool Key exact-set，以及 direct package source + Module metadata closure 已完成；完整 transitive build-input closure 仍未完成。`R01-D01`～`R01-D06` 与 `GOV-D01`～`GOV-D06` 未关闭、默认分支 trust root 和真实 PR/Ruleset 未激活前，仍不得进入 formal Review Freeze、生产 Graph Tool 或全功能冒烟纵切。D0-04B 优先迁入独立 stdlib-only verifier；若保留共享包，则必须补齐 embed、内部/第三方依赖、toolchain 与 workflow action 的完整闭包。
