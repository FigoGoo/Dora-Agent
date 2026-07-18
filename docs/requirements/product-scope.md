# Dora 产品范围

> 状态：Active
>
> 本文是产品范围的唯一事实源，回答“做什么、暂不做什么”。完成进度、当前阶段和下一步只在[交付阶段与当前状态](delivery-status.md)维护。

## 范围原则

1. 先把端到端基本链路稳定跑通，再做生产化，最后扩展商业和运营能力。
2. 当前层只承诺本地 Development Preview；“已实现”不等于生产可用、生产 Catalog 已发布或完整验收通过。
3. P1 只补齐核心创作链生产必需能力，不借机扩展新业务域或引入无验收价值的抽象层。
4. P2 业务扩展必须建立在 P1 的权限、计费、可靠性和可观测基础上。

## MVP 基本功能范围：本地 Development Preview

| 功能域 | MVP 基线 | 明确边界 |
| --- | --- | --- |
| 身份与项目 | 真实登录 API、Owner-safe 项目列表、QuickCreate | 本地身份链可用；完整生产认证、RBAC 和安全加固不在当前层 |
| Skill 基础 | 已有 Skill Foundation、治理、公开市场读取和项目绑定的局部验证链 | 不代表完整创建、发布、审核、分发、收益生命周期已完成 |
| 素材与分析 | 不可变文本素材的创建、列表、选择，以及素材 Evidence/分析 Preview | 文件上传、通用媒体摄取、OCR/ASR 等不在当前层 |
| 创作流程 | 一个主 ChatModelAgent、一个 Coordinator/HOL；`plan_creation_spec`、`analyze_materials`、`plan_storyboard`、`generate_media`、`write_prompts`、`assemble_output` 六个 Graph Tool | 仅批准本地 Preview Profile；正式 Active Revision、Approval、生产 Tool Catalog 尚未完成 |
| 媒体与资产 | Business Prepare/Query/Finalize、Agent Operation/Batch/Job、Worker Claim/Lease/Heartbeat；可生成确定性 PNG 和固定 MP4，并支持 Owner 保护的内容读取 | 本地确定性媒体不是生产 Provider，单机文件也不是生产对象存储 |
| 工作台与事件 | Workspace V5、Snapshot/SSE、工具卡片、素材选择和硬刷新恢复 | 完整 A2UI 治理、动作安全和复杂异常恢复仍属生产化范围 |
| 本地运行 | PostgreSQL、Redis、etcd、三 Runtime、Vite、Chromium 可组成统一 MVP 主链 | 快速主链不自动覆盖全量测试、故障注入、重启/Fence 恢复和发布门禁 |

## P1：核心创作链生产化

P1 不增加新的商业场景，目标是让现有核心链能够安全、可靠、可运营地进入生产候选。

| 方向 | 必须交付的结果 |
| --- | --- |
| 身份与安全 | 生产认证与 RBAC、服务间认证、Secret 管理、TLS、输入/配额限制、内容与执行沙箱 |
| 素材与资产 | 文件上传和通用 Asset/Evidence、授权校验、生产对象存储、生命周期与清理策略 |
| 模型与媒体 | 生产 ChatModel/Provider Adapter、超时与配额、真实图片/视频生成、可追踪 Provider Receipt |
| 创作领域 | 稳定 Revision/Slot/Binding、Active 状态、正式 Approval、可验证的业务状态机和幂等契约 |
| Agent Runtime | 生产六 Tool Catalog、Checkpoint/Continuation、取消/重试、unknown-outcome 查询、重启恢复与 Fence takeover |
| 计费 | 核心执行的扣费、幂等账本、失败语义、预算与对账，不以 UI 展示代替权威交易事实 |
| 前端与 A2UI | 受控组件和 Action 白名单、版本兼容、错误恢复、可访问性及敏感信息边界 |
| 质量与运维 | 结构化观测、告警、审计、故障注入、性能/容量、备份恢复、发布与回滚 Evidence |

## P2：业务与商业扩展

P2 在 P1 门禁关闭后按业务价值单独立项，不阻塞基本创作链跑通。

| 方向 | 候选能力 |
| --- | --- |
| Skill 商业生命周期 | 完整创建、版本、提交审核、发布、下架、市场分发、作者收益与结算 |
| 支付与运营 | 充值、积分套餐、支付回调、账务运营、活动和商业报表 |
| 内容社区 | 精选作品、公开分享、点赞/收藏及内容治理 |
| 平台运营 | 公告、运营配置、完整管理控制台、审核与审计工作台 |
| 丰富素材能力 | 图片/音视频/PDF 等摄取，以及 OCR、ASR、解析、检索和批量处理 |

## 非目标与变更约束

- 当前不把本地 Fake/确定性 Provider、Preview Draft 或测试 Evidence 包装成生产能力。
- 当前不以新增微服务、通用工作流平台、动态子 Agent 或插件系统替代六个稳定 Graph Tool。
- P1 未关闭前，P2 能力不得进入 MVP 必经链或扩大跨 Module 契约面。
- 新范围必须先修改本文并给出可验收结果；仅有设计设想、占位 API 或 UI 不计入产品范围。
