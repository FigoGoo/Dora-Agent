# Agent Graph Tool 设计索引

> 状态：`mvp_all_tools.runtime.v1preview1 + media.runtime.v3preview1` 本地 Development Preview 已于 2026-07-17 经 `make trial-basic` 跑通六个 Tool；六 Tool 完整生产设计仍为 Draft / P1
>
> 更新日期：2026-07-17
>
> 注意：本索引不替代六份独立设计文档。下文“已完成”只表示 local-only Development Preview 在获批范围内通过，不表示生产 Catalog、真实 Provider、计费、Approval、完整恢复或发布门禁已完成。

## 1. 设计基线

- [AIGC 跨 Module 契约目录](../../cross-module/aigc-contract-catalog.md)：数据 Owner、Business RPC、Agent→Worker Job Contract、Event、Approval、计费、幂等和 unknown-outcome 共同基线。
- [Agent Runner 与 PostgreSQL Session Lane v1 设计评审](../runner-session-lane-review-v1.md)：GraphToolResult/Receipt、严格 HOL、Lease/Fence、Turn/Run、Approval Continuation、Checkpoint 与 A2UI/Event 的 W2 运行底座草案。
- [Agent A2UI Event / Card / Action v1 契约评审](../a2ui-event-action-contract-v1.md)：W2 最小组件/Action 白名单、Card Revision、防重放、错误信封、SSE/REST 恢复和无 Mock/fallback 门禁草案。
- [Agent Eino / DeepSeek 依赖锁定评审 v1](../eino-dependency-lock-review-v1.md)：只批准 Agent Module 的精确版本、经典 Message/DAG 兼容与独立构建，不批准 Runtime。
- [`plan_creation_spec` W2-R04 生产差距评审](plan_creation_spec-w2-r04-gap-review.md)：十类 P0 继续作为 P1 生产门禁，不再阻塞不含计费/Approval 的 V1 开发预览。
- [Graph Tool 功能需求总览](../../../requirements/graph-tool-requirements-overview.md)：六个用户可见工具的产品能力、状态、计费和验收要求。
- [功能优先开发与试跑计划](../../../requirements/full-function-smoke-development-plan.md)：唯一开发顺序、当前 V2 纵切、试跑和 P1 生产门禁口径。
- [本地 MVP 全工具运行时](../mvp-all-tools-runtime-v1-design.md)：一个主 ChatModelAgent、一个 Coordinator Scanner 与统一能力 Profile。
- [Media Runtime V3 Development Preview](../media-runtime-v3-preview-design.md)：确定性 PNG、固定 MP4、单 Job 与 Terminal Outbox 的 local-only 批准范围。
- [MVP 六工具媒体扩展](../mvp-six-tools-media-extension-v1-design.md)：冻结双 Profile、单主 Agent、浏览器入口、内部 Prepare/Finalize 路径与一键验收。
- [全功能冒烟工程设计](../../testing/full-function-smoke-engineering-design.md)：Fixture、可控 Adapter、故障注入和 Evidence Bundle。
- [`main` 分支迁移资产清单](../../migration/main-branch-aigc-asset-inventory.md)：旧单体代码的选择性复用、重写和废弃边界。
- [AIGC ChatModelAgent 历史/目标设计](../../../aigc-chatmodelagent-demo-design.md)、[Tool/Storyboard 历史/目标设计](../../../aigc-tool-storyboard-design.md)、[Worker 历史/目标设计](../../../aigc-worker-design.md)：仅作迁移参考，不能覆盖当前独立设计。

## 2. 六个独立设计

| 展示顺序 | Tool Key | 设计文档 | 当前状态 | 实现依赖 |
|---|---|---|---|---|
| 1 | `plan_creation_spec` | [流程规划设计](plan_creation_spec-design.md) | V1 Preview 已完成；生产 Draft | Runner→Business Draft→Card、恢复账本、真实 PG/Redis/etcd/Chromium canonical Smoke 与 Trial Evidence 已通过；计费/Approval 后置 P1 |
| 2 | `analyze_materials` | [素材分析设计](analyze_materials-design.md) | `analyze_materials.runtime.v2preview1` M0～M4 Development Preview 已完成；生产 Catalog 未注册；生产 Draft | 本地互斥 Profile 已验证 typed ingress、单 Tool Eino、Receipt、Snapshot/SSE/Card 与 Chromium；生产仍需摄取、MaterialAnalysis 持久化、认证/TLS、计费与 Approval |
| 3 | `plan_storyboard` | [故事板设计](plan_storyboard-design.md)、[最小 Runtime Profile](../plan-storyboard-runtime-v2-design.md) | `plan_storyboard.runtime.v2preview1` M0～M4 Development Preview 已完成；完整生产 Draft | 最终源码 Run `20260717T010209Z-81125` 已验证隔离 Business Draft/Receipt、Foundation RPC、默认不注册 Tool Core/Adapter、typed ingress/HOL/Fence/两层 Receipt、BFF、Workspace v3 Snapshot/SSE/Card、硬刷新与 Agent 重启；生产仍需稳定 Element/Slot、Active/Approval、修订/Diff、锁与绑定保护 |
| 4 | `generate_media` | [媒体生成设计](generate_media-design.md)、[Media Preview](../media-runtime-v3-preview-design.md)、[六工具扩展](../mvp-six-tools-media-extension-v1-design.md) | `media.runtime.v3preview1` Development Preview 已完成；完整生产 Draft | `make trial-basic` 已验证 Agent Job→Worker Claim/Finalize/Terminal、确定性真 PNG、受保护读取与 Workspace V5 刷新恢复；真实 Provider、计费、Approval 和异常恢复后置 P1 |
| 5 | `write_prompts` | [提示词写法设计](write_prompts-design.md)、[最小 Runtime Profile](../write-prompts-runtime-v2-design.md) | `write_prompts.runtime.v2preview1` M0～M4 Development Preview 已完成；完整生产 Draft | Run `20260717T043513Z-58302` 已验证 Runtime/BFF/Workspace/SSE/Card/Form、exact-set、硬刷新与 Agent 重连；standalone、生产 PromptArtifact/Revision、Active/Approval 后置 |
| 6 | `assemble_output` | [视频剪辑与装配设计](assemble_output-design.md)、[Media Preview](../media-runtime-v3-preview-design.md)、[六工具扩展](../mvp-six-tools-media-extension-v1-design.md) | `media.runtime.v3preview1` Development Preview 已完成；完整生产 Draft | `make trial-basic` 已验证固定 ffmpeg 产出 2 秒可播放 MP4、同源 Range `200/206/416`、Workspace V5 与硬刷新恢复；生产需 Active/Approval AssemblyPlan、真实 Provider 与完整恢复 |

产品展示顺序不等于实现顺序。本索引只记录 Profile 状态与目标依赖，不维护独立排期；开发顺序、当前阶段和“下一步”只以[功能优先开发与试跑计划](../../../requirements/full-function-smoke-development-plan.md)为准。

## 3. 已统一的设计决策

1. 只有一个 ChatModelAgent；六个 Tool 是高层 `compose.Graph`，不是子 Agent、DeepAgent 或 AgentAsTool。
2. Graph 启动时编译为无环 DAG，使用 `AllPredecessor`；模型候选后必须经过独立确定性 Validator。
3. 用户 Approval 结束当前 Graph，以新 Continuation Turn 继续；不保留跨分钟 Graph 栈。
4. 媒体/真实渲染在 Agent 原子派发后返回 `accepted`；Worker 终态通过 Terminal Outbox/Inbox 产生新 Continuation。
5. Business 拥有创作领域对象、Asset/Binding 和计费；Agent 拥有执行、Approval、Job、EventLog；Worker 只拥有私有 Attempt/Receipt。
6. Worker 只经 `AGT-JOB-V1` 的版本化视图/函数 Claim、续租和提交终态，不直写 Agent 普通表。
7. PostgreSQL 是权威；Redis 只唤醒/加速，etcd 只做服务注册发现。
8. 所有可计费执行先扣费；失败/取消默认不退款，unknown outcome 必须查权威 Receipt。
9. `plan_storyboard` 不生成最终 Prompt；`write_prompts` 是唯一正式 Prompt 生成入口。
10. `assemble_output` 的 preview/export 必须产生真实媒体文件，不能用 manifest 或 accepted 状态冒充成功。

## 4. 排期唯一引用

[功能优先开发与试跑计划](../../../requirements/full-function-smoke-development-plan.md)是唯一开发顺序和阶段状态真源。本索引只在阶段变化时同步各 Tool 获批 Profile；若与计划不一致，以计划为准。任何 Preview 通过都不得自动升级为生产 Approved。

## 5. 当前生产未关闭项

- `mvp_all_tools.runtime.v1preview1 + media.runtime.v3preview1` 已通过 2026-07-17 `make trial-basic` 本地真实浏览器主链：同一主 Agent 依次运行六个 Tool，Worker 完成 PNG/MP4，Workspace V5、受保护 Range 和页面刷新恢复均通过；该 Evidence 仅证明获批的 local-only happy path；
- 完整生产 Registry/Catalog、真实 Provider、计费、正式 Approval、异常恢复与服务重启恢复仍为 Draft / P1，六份 Tool 的完整生产范围均未通过生产门禁；
- AIGC 契约目录已起草，支付回调、管理治理及完整 HTTP/A2UI 字段目录后置到 P1 补齐；
- 本地确定性 Fixture、测试账号和 `trial-basic` Evidence 已实现；生产 Sandbox/Provider、故障注入、容灾与完整发布 Evidence 仍需跨角色评审和实现；
- 历史 `main` 的顶层资产与 29 个 `internal/aigc` 一级包已完成包级归类；具体迁移 PR 仍需逐文件复核。
