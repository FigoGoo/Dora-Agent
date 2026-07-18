# 素材与素材分析功能现状

> 文档状态：Current Implementation / 只描述当前工作树
>
> 适用 Runtime：`business/cmd/business-service`、`agent/cmd/agent-service`
>
> 更新日期：2026-07-17

## 现状

当前用户可在自己拥有的 Project 内创建不可变文本素材，并读取最近创建的文本素材列表。文本素材创建会在一个 Business PostgreSQL 事务中写入 `asset_analysis_preview_assets` 的 ready text Asset 和一条完整 `text_segment` Evidence，因此可以直接作为 `analyze_materials.v2preview1` 的输入。

Agent 已实现独立的 Analyze Materials Preview 入队、Session Lane 处理、Business Foundation RPC 读取、确定性结果校验、Receipt、Event 和 Workspace Card 投影。请求按 Asset ID 和可选期望版本读取 Business 已持久化的 Evidence；Business 在一次有界集合查询中完成 Project Owner 授权、Asset exact-set 和 Evidence exact-set 校验。

当前用户可写入口只有文本素材。图片素材和视觉/安全 Evidence 结构已存在，仓库的本地 Smoke Fixture 可以显式写入测试事实，但没有面向用户的图片上传或自动提取接口。

## 边界

当前素材功能明确是本地开发预览，不是通用素材库。未实现：

- 图片、PDF、音频、视频上传和文件对象存储入口；
- OCR、视觉模型、安全审核、音视频转写或后台提取 Worker；
- Evidence 写 API、编辑、重新提取、版本升级、删除和 redaction 管理入口；
- 素材分页搜索、标签、目录、跨 Project 复用和批量管理；
- 持久化 MaterialAnalysis 业务聚合、候选审核、Approval 或计费；
- 生产服务身份认证和 TLS 保护的素材分析 RPC。

`asset_analysis_preview_assets` 与媒体产出使用的 `media_preview_asset` 是两个当前 Preview 聚合，不能混称为已经统一的生产 Asset 模型。

## 流程

### 创建文本素材

1. 登录用户在 Project 路径提交 Session、CSRF、规范 UUIDv7 `Idempotency-Key` 和严格 JSON 正文。
2. Business 校验正文是 NFC、1 至 2000 个 Unicode 字符，且不含不允许的控制字符。
3. `Idempotency-Key` 直接成为 `asset_id`，Business 生成唯一 `evidence_id`。
4. Repository 在一个事务内重新校验 Project Owner，写入 ready text Asset 和完整 text-range Evidence。
5. 同一 Asset ID、Owner、Project 和正文重放返回原事实；同一 ID 绑定不同语义返回冲突。

### 分析素材

1. 客户端向 Session 的 Analyze Materials Preview 入口提交严格目标集合。
2. Business BFF 校验 Session、CSRF 和 Session 对 Project 的 ready Binding，签发一次性内部身份断言并代理到 Agent。
3. Agent 在 PostgreSQL 中 AppendOnce 写入结构化 Input/Run Context，并唤醒共享 Session Lane。
4. Processor 依次执行固定 Runtime：读取 Business Foundation RPC、验证 Asset/Evidence exact-set、运行当前 Preview 的确定性分析逻辑、冻结 Model/Tool Receipt 和安全 Card。
5. Agent 在同一权威状态上写 terminal Event；Workspace Snapshot 和 SSE 投影同一张最新 Analyze Materials Card。

Business RPC 只返回已经持久化的 Evidence，不从缺失内容臆造描述，也不会在读请求中启动提取任务。

## 接口与状态

### 用户 HTTP 接口

| 方法与路径 | 当前行为 | 认证要求 |
| --- | --- | --- |
| `POST /api/v1/projects/:project_id/text-materials` | 创建或幂等重放不可变文本素材 | Session + CSRF + UUIDv7 `Idempotency-Key` |
| `GET /api/v1/projects/:project_id/text-materials` | 返回最近最多 100 条完整文本素材；当前拒绝所有 Query | Session + Project Owner |
| `POST /api/v1/agent/sessions/:session_id/analyze-materials-previews` | 入队独立素材分析 Preview | Session + CSRF + 对应 Project Session Binding |

### 跨 Module RPC

`BatchGetAssetAnalysisInputsPreviewV1` 是当前 Foundation RPC 中的只读 Development Preview 方法。单次最多读取 8 个 Asset、32 条 Evidence；返回完整有序集合和 Snapshot Token。它不是生产 `BatchGetAssetAnalysisInputs`。

### 当前类型与状态

| 对象 | 当前值 |
| --- | --- |
| Asset 媒体类型 | `text`、`image`；用户创建入口只写 `text` |
| Preview Asset 状态 | 固定 `ready` |
| Evidence kind | `text_segment`、`visual_description`、`safety_label` |
| Evidence availability | `ready`、`missing`、`failed`、`redacted`、`unsupported` |
| Locator | `text_range`、`image_whole`、`image_region` |
| Analyze Materials Event | `accepted`、`completed`、`partial`、`failed`、`runtime_failed` 对应的版本化事件类型 |

文本素材版本当前固定为 1，创建后不可编辑。Analyze Materials 请求允许携带 `expected_asset_version`；Business 完成授权后再报告版本冲突。

## 数据 Owner

| 数据 | Owner | 当前规则 |
| --- | --- | --- |
| Project、素材 Asset Snapshot、Evidence | Business PostgreSQL | Business 唯一写入和授权 Owner |
| 文本素材正文 | Business Evidence | 当前 Owner 可通过 Project 文本素材列表读取 |
| Analyze Materials Input、Run、Turn Context、Model/Tool Receipt、Projection | Agent PostgreSQL | Agent Session Lane 和 Preview Runtime 写入 |
| Workspace Card 与 EventLog | Agent PostgreSQL | 从同一终态事实确定性投影 |

Agent 只通过版本化 Foundation RPC 读取 Business Evidence，不连接 Business 数据库。Business 不保存 Agent 的分析运行状态。

## 错误与安全

- 文本素材 Owner 只来自认证 Principal；请求体不能提交 `owner_user_id`、`project_id` 或 Evidence 字段。
- Project 不存在和不属于当前用户统一返回 `PROJECT_NOT_FOUND`。
- 文本创建严格拒绝未知字段、重复 JSON Key、trailing JSON、非法 UTF-8/Surrogate 和非法控制字符。
- 同一 `asset_id` 只允许同语义重放；正文或归属变化返回 `IDEMPOTENCY_CONFLICT`，不会覆盖旧 Evidence。
- Evidence 是不可变事实，数据库 Trigger 拒绝更新或删除 Preview Evidence。
- Batch RPC 先校验 Project Owner，再读取 Asset；不存在、越权和不可用统一为 `NOT_FOUND`，授权后才区分 `VERSION_CONFLICT`。
- RPC 和 Runtime 使用 exact-set、数量上限、稳定排序和摘要复核；未知 Schema、重复 Asset、Evidence 缺项或多项都失败关闭。
- Preview RPC 只允许本地显式开关，当前没有独立服务身份和 TLS，不能暴露为公网接口。
- 普通日志、Event 和安全 Card 不输出 DSN、SQL、内部堆栈或未批准的素材正文集合。

## 测试

当前仓库包含：

- Business `textmaterial`、`assetanalysis` Service/Entity 和 HTTP Handler 单元测试；
- Text Material、Asset Analysis Migration/Repository 的 PostgreSQL 集成测试；
- Foundation RPC Producer、Agent RPC Mapper、严格 DTO 和摘要测试；
- Analyze Materials Runtime 的入队、HOL、Lease/Fence、Receipt、Projection、Event 和 Workspace Reducer 测试；
- 本地 Fixture Seeder 的 deterministic、reset、verify 和越权/损坏负向测试；
- `make analyze-materials-runtime-smoke`：独立素材分析纵切；
- `make test-analyze-materials-runtime-smoke`：Smoke 脚本结构和边界测试；
- `make trial-basic`：完整 MVP 流程中包含文本素材创建与 Analyze Materials 步骤。

这些入口不代表所有生产素材类型已实现；是否通过应以具体提交实际运行结果为准。

## 生产差距

当前最大差距是素材摄取和 Evidence 生产链：用户只能手工创建短文本，图片事实只能由测试 Fixture 提供，没有真实上传、对象存储、提取 Provider、异步任务、版本治理或删除/redaction 流程。

Preview Asset 表与媒体产出 Asset 表尚未统一，生产化前需要明确统一 Asset 聚合、版本、对象存储、安全扫描、保留删除和跨 Project 授权口径。当前 Foundation RPC 缺少生产服务身份、TLS 和网络最小权限，且 Analyze Materials 结果只是 Agent 安全 Card，不是 Business-owned MaterialAnalysis 业务版本。
