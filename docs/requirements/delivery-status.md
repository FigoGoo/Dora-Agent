# Dora 交付阶段与当前状态

> 状态：Active / 唯一阶段状态源
>
> 本文是“当前做到哪、还缺什么、下一步执行什么”的唯一事实源。产品边界见[产品范围](product-scope.md)，功能实现方式见[统一文档入口](../README.md)。其他需求、设计、README 和历史计划不得维护独立的当前阶段或下一步清单。

## 状态词

本文只使用以下四种状态，避免把 Preview 跑通误写为生产完成。

| 状态 | 含义 |
| --- | --- |
| 已跑通（Preview） | 当前工作树已有可执行链路和本地验收证据；仅对明确的 Development Preview 范围成立 |
| 进行中 | 已有部分实现，但本阶段门禁尚未全部关闭 |
| 待开始 | 已进入稳定产品范围，但当前阶段尚未实施 |
| 后置 | 不阻塞当前目标，达到前置门禁后再立项 |

## 阶段总览

| 阶段 | 状态 | 当前结论 | 退出条件 |
| --- | --- | --- | --- |
| D0 三模块与本地基础 | 已跑通（Preview） | Business、Agent、Worker 独立 Module；PostgreSQL、Redis、etcd 本地联调链可用 | 三 Module 独立构建，基础 Migration 与服务发现链可重复运行 |
| D1 同步创作基础 | 已跑通（Preview） | 登录、项目、文本素材、CreationSpec、素材分析、Storyboard、Prompt 与 Workspace 链路已接入统一 Runtime | 同一 Session Lane 可按 Owner 边界完成同步工具链并刷新恢复 |
| D2 六工具媒体 MVP | 已跑通（Preview） | `make trial-basic` 已跑通一个主 Agent、六 Tool、Worker PNG/MP4、受保护内容读取及 Workspace V5 | 快速主链全部断言通过并原子发布本地 Evidence |
| D3 已有功能验收收口 | 进行中 | 快速主链已绿；isolated profiles、Skill/W1、三 Module 全门禁、前端全门禁和关键恢复仍独立执行 | 下节“D3 关闭清单”全部通过，失败项有明确修复与回归证据 |
| P1 核心生产化 | 待开始 | 生产安全、Provider/Storage、正式领域状态、Catalog、计费、恢复和运维门禁尚未关闭 | [产品范围](product-scope.md)中的 P1 结果全部具备生产 Evidence |
| P2 业务扩展 | 后置 | Skill 商业生命周期、支付运营、社区和完整管理端不阻塞当前 MVP | P1 关闭后按业务价值重新排期 |

## 当前已验证的统一主链

`make trial-basic` 当前验证以下 local-only happy path：

1. 真实登录、Owner-safe 项目与 QuickCreate。
2. 不可变文本素材创建、列表、选择与素材分析。
3. 一个主 ChatModelAgent、一个 Coordinator/HOL 顺序运行六个 Graph Tool。
4. Business 持有 Preview Draft/Asset，Agent 持有 Operation/Batch/Job，Worker 完成 Claim/Lease/Heartbeat 和 Finalize。
5. 生成确定性 `640×360` PNG，以及 H.264、`yuv420p`、2 秒、faststart MP4。
6. 同源内容读取覆盖 `HEAD`、`200`、`206`、`416`，Workspace V5 可在硬刷新后恢复。

成功 Evidence 位于 `.local/smoke/trial-basic.json`，权限应为 `0600`。Evidence 只证明生成它的当前工作树通过该 Profile，不是生产认证，也不能替代其他门禁。

## D3 关闭清单与执行顺序

为了最快跑通所有已实现功能，按以下顺序收口；只修复阻断链路的问题，不在 D3 引入新框架或扩大产品范围。

| 顺序 | 门禁 | 命令 | 完成标准 |
| --- | --- | --- | --- |
| 1 | 统一 MVP 基线 | `make trial-basic` | 每次收口后主链保持全绿，Evidence 可验证且不含密钥/原始敏感输入 |
| 2 | 五个 isolated Profile | `make plan-spec-preview-smoke`、`make user-message-runtime-smoke`、`make analyze-materials-runtime-smoke`、`make plan-storyboard-runtime-smoke`、`make write-prompts-runtime-smoke` | 每个 Profile 独立运行并清理自身进程；不能用统一主链结果代替 |
| 3 | Workspace 与 Skill 基础 | `make w05-browser-smoke`、`make w1-smoke` | 身份/Workspace 运输链及已有 Skill Foundation/治理/市场链分别通过 |
| 4 | 三 Module 质量 | `make verify`、`make test`、`make vet`、`make race`、`make build` | 三 Module 在 `GOWORK=off` 语义下全部通过 |
| 5 | 前端质量 | `make check-frontend` | 前端单测与生产构建通过 |
| 6 | 文档单一事实源 | `make test-document-single-source` | 不再存在并列阶段状态源、过期入口或当前/目标口径混用 |
| 7 | 关键恢复补测 | 按[运行时与质量设计](../design/functions/runtime-and-quality.md)中的固定用例执行 | Runtime 重启、Fence takeover、unknown outcome 和最小故障注入有可重复证据 |

顺序 1～6 是“所有已实现基本功能跑通”的当前门槛；顺序 7 是进入 P1 前的可靠性门槛。真实生产 Provider、计费、正式 Approval、完整容灾和商业扩展不塞入 D3。

## 未关闭边界

- 六个 Tool 的生产静态 Catalog 仍为 Draft/不可用；Preview 通过不得自动升级为生产 Approved。
- `trial-basic` 不串行执行五个 isolated Profile，也不包含 Module 全量质量、前端全量质量或 W1 链路。
- 生产认证/RBAC、服务间安全、对象存储、真实模型与媒体 Provider、正式 Revision/Approval、计费尚未完成。
- Runtime 重启、Fence takeover、unknown outcome、故障注入、压力与灾备尚未形成完整生产矩阵。
- P2 的支付运营、Skill 商业生命周期、社区、公告和完整管理端不属于当前 D3 阻断项。

## 状态更新规则

1. 只有实际命令和权威数据断言通过后才能写“已跑通（Preview）”；代码存在、测试替身存在或页面可见都不算。
2. 任一 D3 门禁失败时保持“进行中”，记录问题到执行任务或缺陷，不在本文堆叠 Run ID、Hash 和逐次日志。
3. 阶段变化必须在同一改动中更新本文；其他文档只链接本文，不复制状态表。
4. Evidence 保存机器可验证事实；本文只保存稳定结论和门禁，不保存凭证、DSN、Cookie、用户原文或 Provider Payload。
5. 范围变化先更新 `product-scope.md`，设计变化更新对应功能域入口；两者不能隐式改变本文的阶段结论。
