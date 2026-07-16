# Agent Graph Tool 设计索引

> 状态：M0 设计草案已齐备，待跨角色评审
>
> 更新日期：2026-07-14
>
> 注意：本索引不替代六份独立设计文档；任何一份未通过评审，对应 Tool 均不得实现。

## 1. 设计基线

- [AIGC 跨 Module 契约目录](../../cross-module/aigc-contract-catalog.md)：数据 Owner、Business RPC、Agent→Worker Job Contract、Event、Approval、计费、幂等和 unknown-outcome 共同基线。
- [Agent Runner 与 PostgreSQL Session Lane v1 设计评审](../runner-session-lane-review-v1.md)：GraphToolResult/Receipt、严格 HOL、Lease/Fence、Turn/Run、Approval Continuation、Checkpoint 与 A2UI/Event 的 W2 运行底座草案。
- [Agent A2UI Event / Card / Action v1 契约评审](../a2ui-event-action-contract-v1.md)：W2 最小组件/Action 白名单、Card Revision、防重放、错误信封、SSE/REST 恢复和无 Mock/fallback 门禁草案。
- [Agent Eino / DeepSeek 依赖锁定评审 v1](../eino-dependency-lock-review-v1.md)：只批准 Agent Module 的精确版本、经典 Message/DAG 兼容与独立构建，不批准 Runtime。
- [`plan_creation_spec` W2-R04 开工差距评审](plan_creation_spec-w2-r04-gap-review.md)：记录首个同步 Tool 的十类 P0、待签核逐模型计费方案、Runtime 批次和 `SMK-009` 最小闭环；当前仍不通过实现门禁。
- [Graph Tool 功能需求总览](../../../requirements/graph-tool-requirements-overview.md)：六个用户可见工具的产品能力、状态、计费和验收要求。
- [全功能冒烟开发推进计划](../../../requirements/full-function-smoke-development-plan.md)：M0～M5、SMK-P0 和开发门禁。
- [全功能冒烟工程设计](../../testing/full-function-smoke-engineering-design.md)：Fixture、可控 Adapter、故障注入和 Evidence Bundle。
- [`main` 分支迁移资产清单](../../migration/main-branch-aigc-asset-inventory.md)：旧单体代码的选择性复用、重写和废弃边界。
- [AIGC ChatModelAgent 历史/目标设计](../../../aigc-chatmodelagent-demo-design.md)、[Tool/Storyboard 历史/目标设计](../../../aigc-tool-storyboard-design.md)、[Worker 历史/目标设计](../../../aigc-worker-design.md)：仅作迁移参考，不能覆盖当前独立设计。
- **评审对案**：[Dora AIGC 创作体系设计终版 v1](../../../superpowers/specs/2026-07-11-aigc-system-design-final.md) + [GraphTool/node 详设](../../../superpowers/specs/2026-07-13-aigc-graphtool-node-detail.md)——主张「4 模态路由 GraphTool + node 词汇 + 动态四档编排 + 场景 skill 注册匹配」，与本索引六 Tool 功能阶段式分工为竞争方案。**两方案分歧已压缩为 [W2 两方案冲突清单 v1](w2-design-conflict-review-v1.md)（5 硬冲突 + 6 必答问题 + 同频底座 + 融合建议）**；W2 评审须逐题裁决后，方可冻结 Tool 白名单。

## 2. 六个独立设计

| 展示顺序 | Tool Key | 设计文档 | 当前状态 | 实现依赖 |
|---|---|---|---|---|
| 1 | `plan_creation_spec` | [流程规划设计](plan_creation_spec-design.md) | Draft / 待评审 | 跨 Module 契约、同步模型计费 |
| 2 | `analyze_materials` | [素材分析设计](analyze_materials-design.md) | Draft / 待评审 | 素材提取输入、同步模型计费 |
| 3 | `plan_storyboard` | [故事板设计](plan_storyboard-design.md) | Draft / 待评审 | 激活 CreationSpec、稳定 Element/Slot |
| 4 | `generate_media` | [媒体生成设计](generate_media-design.md) | Draft / 待评审 | ready Prompt、Preparation、Job Contract、Worker |
| 5 | `write_prompts` | [提示词写法设计](write_prompts-design.md) | Draft / 待评审 | 激活 Storyboard 或独立 Prompt 上下文 |
| 6 | `assemble_output` | [视频剪辑与装配设计](assemble_output-design.md) | Draft / 待评审 | ready Asset、激活 AssemblyPlan、真实渲染 Worker |

产品展示顺序不等于实现顺序。建议实现顺序为：

```text
plan_creation_spec
  → analyze_materials
  → plan_storyboard
  → write_prompts
  → generate_media
  → assemble_output
```

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

## 4. 评审顺序

1. 三 Module 共同评审跨 Module 契约目录，先冻结 Owner、状态、幂等、Approval Consumption、Job Contract 和错误码；
2. 产品/Business/Agent/财务/安全评审四个同步模型 Tool；
3. 加入 Worker/运维评审 `generate_media` 与 `assemble_output`；
4. 测试负责人逐份确认契约、故障注入和 SMK-P0；
5. 六份文档的复选项全部关闭，并把状态改为 Approved 后，才允许进入 M1/M2 对应实现。

## 5. 当前未关闭的 M0 项目

- 六份设计尚未完成跨角色评审，当前仍是实现硬门禁；
- AIGC 契约目录已起草，但支付回调、管理治理及完整 HTTP/A2UI 字段目录仍需在 P0-04 补齐；
- Fixture、Fake/Sandbox Adapter、测试账号和冒烟证据格式已有独立草案，仍需跨角色评审和实现；
- 历史 `main` 的顶层资产与 29 个 `internal/aigc` 一级包已完成包级归类；具体迁移 PR 仍需逐文件复核。
