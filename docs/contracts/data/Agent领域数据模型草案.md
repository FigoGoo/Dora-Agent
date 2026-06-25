# Agent 领域数据模型草案

状态：draft
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-25
适用范围：智能体微服务 Agent Runtime 数据库

## 领域边界

Agent 领域数据库只保存 Agent Runtime 数据，包括会话、运行、消息、事件、Tool 调用、任务、中断、草稿产物、记忆和配置。不保存项目、积分、最终资产、作品、企业成员、业务权限、订单、支付等业务事实。业务事实必须通过 RPC 调用业务微服务产生。

## 表清单

| 表名 | 用途 | 数据范围和清洗口径 |
| --- | --- | --- |
| agent_sessions | 工作台会话 | 按用户和项目查询，归档后按保留策略清理 |
| agent_runs | 单次 Agent 执行 | 保存 run 状态、项目引用、模型快照摘要 |
| agent_messages | 用户和 Agent 消息 | 保存可展示消息，不保存系统 Prompt |
| agent_events | AG-UI 事件存储 | 支持 SSE 补偿和 sequence 查询 |
| agent_tool_calls | Tool 调用记录 | 保存公开摘要、状态、耗时、错误码 |
| agent_tasks | 长任务 | 保存生成、保存、恢复等任务状态 |
| agent_interrupts | 人工确认 | 保存确认状态、过期时间和公开 payload |
| agent_artifacts | 草稿产物和黑板 | 保存黑板、分镜、脚本、提示词、业务资产引用 |
| agent_safety_evaluations | 内容安全评估记录 | 保存脱敏评估摘要、摘要哈希、策略版本和可排障字段 |
| agent_memories | 会话摘要和授权偏好 | 不保存业务事实和敏感信息 |
| agent_runtime_configs | 运行配置快照 | 保存 Agent Runtime 配置版本 |

## 通用字段

所有表建议包含：

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| id | string | 是 | pk | 主键 |
| user_id | string | 是 | index | 普通字段，不是外键 |
| space_id | string | 是 | index | 当前空间引用 |
| project_id | string | 否 | index | 业务项目引用 |
| trace_id | string | 是 | index | 链路追踪 |
| created_at | datetime | 是 | index | 创建时间 |
| updated_at | datetime | 是 | 否 | 更新时间 |
| deleted_at | datetime | 否 | index | 软删除 |

## 状态机

| 对象 | 状态 | 可迁移到 | 触发条件 |
| --- | --- | --- | --- |
| session | active | archived | 用户归档，或根据业务项目归档同步 Runtime 只读标记 |
| run | pending | running | 开始执行 |
| run | running | interrupted | 需要人工确认 |
| run | interrupted | running | 确认通过并恢复 |
| run | interrupted | cancelled | 用户拒绝或过期 |
| run | running | completed | 执行完成 |
| run | running | failed | 执行失败 |
| run | running | cancelled | 用户取消 |
| task | pending | running | 任务开始 |
| task | running | completed | 任务完成 |
| task | running | failed | 任务失败 |
| interrupt | required | accepted | 用户确认 |
| interrupt | required | rejected | 用户拒绝 |
| interrupt | required | expired | 超时 |

## 索引

| 表 | 索引字段 | 用途 |
| --- | --- | --- |
| agent_sessions | user_id, space_id, project_id, updated_at | 项目下会话列表 |
| agent_runs | session_id, status, created_at | 会话 run 列表 |
| agent_events | run_id, sequence | SSE 补偿 |
| agent_events | event_id | 幂等去重 |
| agent_messages | session_id, created_at | 消息恢复 |
| agent_tool_calls | run_id, status | Tool 状态 |
| agent_interrupts | run_id, status, expires_at | 确认恢复 |
| agent_artifacts | session_id, project_id, artifact_type | 黑板和草稿读取 |
| agent_safety_evaluations | safety_evidence_id | 证据幂等和排障 |
| agent_safety_evaluations | source_run_id, source_artifact_id | 运行和产物追踪 |

## CRUD

- Create：写入必须幂等，`agent_events` 使用 `event_id` 去重。
- Read：列表查询分页，默认 10，避免逐条关联查询。
- Update：状态迁移必须校验当前状态。
- Delete / Archive：优先软删除或归档。
- 幂等规则：run、event、interrupt、task 均要支持重复请求幂等处理。

## 和业务数据库的边界

- 不保存业务事实：积分余额、积分流水、最终资产、作品、企业成员、业务权限。
- 不复制业务主数据：项目标题、作品公开状态等通过业务 API/RPC 查询。
- 业务写操作通过 RPC：资产保存、扣费、项目创建、作品分享都走业务微服务。
- 测试验证方式：Agent DB 和业务 DB 分开断言。

## 项目归档边界

- 项目 `archived` 是业务事实，Agent 创建 session/run、resume、retry、confirm 和保存生成资产前必须通过 RPC 校验。
- Agent 可以把 session 标记为 `archived` 表示 Runtime 只读，但不能把它作为项目真实状态来源。
- `archived` 项目允许读取历史 session、run、message、event、snapshot；不允许创建新 run 或继续生成。
- 运行中发现项目归档时，Agent 停止发起新 Tool，释放未结算冻结积分，并写入取消状态和 AG-UI 事件。

## 内容安全证据边界

- `agent_safety_evaluations` 保存 `safety_evidence_id`、`scene`、`result`、`target_type`、`evaluated_object_digest`、`policy_version`、`evidence_version`、`evaluated_at`、`expires_at`、`trace_id`、`source_session_id`、`source_run_id`、`source_artifact_id`。
- Agent 内部可保存 `evaluator_config_version`、`model_snapshot_ref`、`prompt_assembly_ref`、`latency_ms`、`attempt_count`、`error_class` 和脱敏摘要，用于排障。
- 不保存系统 Prompt、完整组装 Prompt、供应商原始响应、推理链路、内部评分、命中规则细节、绕过提示和 API Key。
- 写业务 RPC 时只传 `safety_evidence` 脱敏摘要，业务侧二次校验证据和写入对象是否匹配。

## TOS 与公开媒体边界

- Agent 不签发 TOS 上传凭证，不生成公开快照，不保存 TOS AK/SK 或上传签名。
- `agent_artifacts` 的最终资产引用只保存 `artifact_type=asset_ref`、`status=final_ref`、`business_ref_id=asset_id`、`source_run_id` 和脱敏展示摘要。
- TOS object key 和公共 URL 属于业务资产事实，由业务微服务保存和授权返回。
- 前端预览、下载和公开访问必须通过业务 API 获取授权后的 TOS 公共 URL，不能从 Agent 事件中读取长期媒体 URL。

## 待确认

- 数据保留周期。
- `agent_memories` 第一版是否启用。
- run 并发策略和 session 归档策略。
