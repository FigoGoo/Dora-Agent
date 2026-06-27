# 08-Skill路由Tool执行模型选择与安全治理设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：Skill 池加载、意图识别、Skill 路由、Tool 执行、模型选择、内容安全评估
相关代码路径：`services/agent/internal/runtime/skill/**`、`services/agent/internal/runtime/tool/**`、`services/agent/internal/runtime/eino/**`
相关契约：`docs/product/prd/04-Tool边界与平台开放能力PRD.md`、`docs/product/prd/05-SkillBuilder与审核PRD.md`、`docs/product/prd/10-内容安全治理PRD.md`

## 文档目标

- 定义统一 Agent 如何按当前空间加载 Skill 池。
- 定义意图识别、Skill 路由、无 Skill 兜底和直接 Tool 意图。
- 定义 Tool 执行策略、模型选择锁定和内容安全治理。
- 明确 Skill 和 Tool 不承载业务规则最终解释权。

## 功能范围

- 当前空间 Skill 池。
- Published Skill 过滤。
- 用户个人 Skill 在企业空间使用限制。
- 无 Skill 文本兜底。
- 平台开放基础 Tool 直接调用。
- Tool 风险等级和确认。
- 模型选择当前对话生效。
- 确认后模型和参数锁定。
- LLM 提示词安全评估。

## Skill 路由流程

```text
ResolveCurrentSpaceContext
  -> ListRoutableSkills(page_size=10)
  -> Eino Router 判断 Skill / Tool / free_chat
  -> GetPublishedSkillSpec
  -> 合并 Skill confirmation_policy
  -> CheckToolExecutionPolicy
  -> ResolveDefaultModel 或使用本轮锁定模型
  -> ResolveGenerationModelSnapshot
  -> EvaluateSafety
  -> 进入 TurnLoop 积分和生成闭环
```

路由结果写入 `agent_runs.skill_selection`，字段包含 `skill_id`、`skill_version`、`skill_scope`、`matched_reason`、`fallback_reason`、`tool_refs_digest`、`execution_space_id`、`billing_credit_account_scope`。无 Skill 命中时进入 `free_chat` 或直接 Tool 意图，但仍必须经过安全评估和权限策略。

企业空间使用个人 Skill 的规则：

- Agent 只路由 `ListRoutableSkills` 返回的 Skill，不自行拼接系统、企业、个人作用域。
- 个人 Skill 在企业空间运行时，输出资产、作品草稿、积分预估和扣费均归当前企业空间；不得写回个人空间。
- 企业 Tool allowlist、风险等级和停用策略优先作用于所有 Skill，包括个人 Skill；个人 Skill 绑定的 Tool 如果不被企业空间允许，Agent 必须阻断或降级，而不能绕过业务策略。
- `skill_scope=personal` 只表示 Skill 归属，不表示运行事实归属；运行事实以当前 `space_id` 和业务返回的 `credit_account_scope` 为准。

## 核心函数设计

| 函数 | 入参 | 出参 | 使用的 Eino 模块 |
| --- | --- | --- | --- |
| `LoadRoutableSkillPool` | `auth_context`、`space_id`、`cursor` | `SkillRuntimeSpec[]`、`next_cursor` | `Retriever` 风格封装业务 Skill 索引，不直接持久化业务 Skill。 |
| `RouteSkillIntent` | `user_input`、`skill_pool`、`asset_refs[]` | `SkillRouteResult` | `Agent` / `Graph` router node。 |
| `BuildToolPlan` | `skill_spec`、`route_result`、`model_selection` | `ToolExecutionPlan` | `Graph` planning node。 |
| `ResolveModelForRun` | `resource_type`、`selected_model_id`、`auth_context` | `ModelSnapshotDTO` | 普通组件，结果传入 Tool node。 |
| `EvaluateSafety` | `scene`、`target_type`、`target_ref_id`、`text_digest` | `SafetyEvidenceDTO` | `Callback` 输出安全事件，必要时中断 Graph。 |
| `MergeConfirmationPolicy` | `skill_confirmation_policy`、`tool_policy`、`business_write_policy` | `ConfirmationDecision` | 普通组件；只能加强确认要求，不能削弱平台高风险策略。 |

## Tool 执行策略

| 条件 | Agent 行为 | AG-UI |
| --- | --- | --- |
| Tool 停用 | 不执行，run failed 或回退可用替代 Tool | `tool.call.failed` |
| 权限不足 | 不执行，错误映射为权限失败 | `agent.run.failed` |
| 高风险 Tool | 创建 `risk_confirmation` interrupt | `confirmation.required` |
| Skill 声明需确认 | 合并 Skill confirmation policy 与 Tool 策略，生成 `confirmation.required` | `confirmation.required` |
| 生成类 Tool | 走安全、预估、确认、冻结、生成、保存资产、扣费闭环 | `credits.*`、`generation.*`、`asset.save.*` |
| 业务写入 Tool | 只允许调用业务 preview / confirm RPC | `confirmation.required` |
| 非幂等失败 | 不自动重试，提示用户重新发起 | `tool.call.failed` |

## 模型选择锁定

- 用户选择模型后仅对当前 run 生效，写入 `agent_runs.model_selection`。
- 创建确认 interrupt 后，模型、数量、时长、尺寸、计价快照全部锁定。
- confirm payload 与锁定摘要不一致时返回 `IDEMPOTENCY_CONFLICT`，不冻结积分。
- 模型配置、供应商密钥和成本价由业务服务管理，Agent 只持有 `model_id`、`public_display_name`、`pricing_snapshot_id` 和运行所需非敏感参数。
- Agent 执行生成 Tool 前必须调用 `ResolveGenerationModelSnapshot`；如果模型、provider 或价格快照在确认后不可用，停止执行、释放冻结并输出用户可见失败。

## Skill 确认策略

`PublishedSkillSpecDTO.confirmation_policy` 是 Skill 运行时声明的确认要求。Agent 读取后与 Tool 风险策略、业务写入 preview/confirm 规则合并，合并结果遵循“只能更严格”的原则。

| 字段 | 说明 |
| --- | --- |
| `required_actions[]` | 需要确认的步骤，例如 `credit_freeze`、`high_risk_tool`、`business_write`、`external_publish`。 |
| `risk_summary` | 给确认面板展示的风险摘要，不包含内部策略细节。 |
| `min_confirm_level` | `none`、`standard`、`strong`；高风险 Tool 最低为 `strong`。 |
| `lock_fields[]` | 确认后锁定字段，例如 model、quantity、duration、public_title。 |
| `expires_in_seconds` | 确认有效期；不得超过平台中断最大过期时间。 |

合并规则：

- Skill 声明 `none` 不能关闭平台积分确认、高风险 Tool 确认、业务写入确认和内容安全。
- Tool 策略返回 `requires_confirmation=true` 时，最终结果必须要求确认。
- 业务写入 Tool 只能通过 preview / confirm RPC，不能由 Skill 直接调用写 RPC。
- `confirmation.accepted` 后锁定字段摘要进入 interrupt payload；后续 resume payload 不一致返回 `IDEMPOTENCY_CONFLICT`。

## 内容安全证据设计

安全证据按以下字段、生成逻辑和传递边界实现：

| 字段 | 说明 |
| --- | --- |
| `safety_evidence_id` | 全局唯一证据 ID。 |
| `scene` | `generation`、`asset_upload_metadata`、`work_share`、`skill_test`。 |
| `target_type` | `prompt`、`asset_metadata`、`work_share_text`、`skill_test_prompt`。 |
| `target_ref_id` | 被评估对象引用，例如 `run_id`、`artifact_id`、`upload_intent_id`、`work_candidate_id`。 |
| `evaluated_object_digest` | 被评估文本摘要哈希，不能保存原文。 |
| `policy_version` | 安全策略版本。 |
| `evidence_version` | 证据结构版本。 |
| `result` | `passed`、`blocked`、`failed`。 |
| `evaluated_at` | 评估完成时间。 |
| `expires_at` | 证据过期时间。 |
| `user_visible_reason` | 用户可见原因，不包含策略细节。 |
| `source_session_id` / `source_run_id` / `source_artifact_id` | Agent 侧追踪字段，不作为业务事实。 |
| `trace_id` | 排障链路。 |

行为规则：

- 安全评估不通过、失败或超时均阻断生成，不进入积分预估和冻结。
- RPC 写入只传 `safety_evidence` 脱敏摘要，不传完整 Prompt、系统 Prompt、内部评分、命中规则和供应商原始响应。
- AG-UI 通过 `safety.prompt.*` 事件展示评估中、通过、阻断、失败。
- 业务侧写入资产、上传素材文本元数据、作品分享公开快照时必须校验 `result=passed`、`scene`、`target_type`、摘要和过期时间。

## 上传素材和作品分享前安全评估边界

| 场景 | Agent 是否实现 | 边界说明 |
| --- | --- | --- |
| 工作台生成前提示词 | Agent 实现 | TurnLoop 固定步骤，不允许 Skill 绕过。 |
| Skill 测试样例提示词 | Agent 实现 | 详见 `12`，用于测试运行和输出元素校验。 |
| 上传素材标题/说明/标签 | Agent 不实现上传业务流程，只复用 Safety Evaluator DTO | 上传由业务 API 承接；业务开发提供同等安全评估能力并产出兼容 `safety_evidence`；Agent 不保存上传资产事实。 |
| 作品分享标题/简介/标签 | Agent 不实现作品分享业务流程，只复用 Safety Evaluator DTO | 作品和公开快照是业务事实；业务开发在分享前校验证据；Agent 不保存作品分享状态。 |

## Safety Evaluator 函数

| 函数 | 入参 | 出参 | 说明 |
| --- | --- | --- | --- |
| `EvaluateSafety` | `scene`、`target_type`、`target_ref_id`、`text`、`source_run_id`、`trace_id` | `SafetyEvidenceDTO` | 完成摘要哈希、策略评估、证据保存和事件输出。 |
| `BuildSafetyEvidence` | `evaluation_result`、`target_digest`、`policy_version` | `SafetyEvidenceDTO` | 不保存完整原文，不暴露策略命中细节。 |
| `HashSafetyTarget` | `normalized_text`、`salt_version` | `evaluated_object_digest` | 用统一归一化规则生成摘要，供业务写入校验。 |
| `ValidateEvidenceForCommit` | `safety_evidence`、`expected_scene`、`expected_digest` | `bool`、`reason` | Agent 提交资产前先做本地一致性检查，业务侧仍需二次校验。 |

## Skill 测试和模型 Tool 专项

- Skill 发布前测试运行、输出元素校验、测试隔离和测试结果回传放到 [12](./12-Skill测试运行输出元素校验与安全证据设计.md)。
- 图片、音乐、视频模型 Tool 的统一接口、任务提交/查询/取消、超时、供应商错误分类和部分完成处理放到 [13](./13-模型Tool适配器任务执行取消与供应商错误设计.md)。

## 测试

| 场景 | 验证 |
| --- | --- |
| Skill 命中 | `agent_runs.skill_selection` 保存命中信息、运行空间和计费账户作用域，AG-UI 输出 `agent.skill.selected`。 |
| 无 Skill 兜底 | 不调用不存在的 Tool，进入 free chat 或参数补充。 |
| Tool 停用 | 不执行供应商请求，错误映射稳定。 |
| 高风险 Tool | 必须生成 `risk_confirmation` interrupt。 |
| Skill 确认策略 | Skill 要求确认时生成 `confirmation.required`；Skill 不能绕过高风险 Tool 和积分确认。 |
| 模型锁定 | confirm 后修改模型或参数被拒绝。 |
| 确认后模型停用 | `ResolveGenerationModelSnapshot` 返回 `RESOURCE_UNAVAILABLE`，Agent 释放冻结并失败。 |
| 安全阻断 | 不进入积分预估，不冻结。 |
| 安全证据过期 | asset commit 前失败并释放冻结积分。 |
| 上传和分享 | Agent 不保存上传和作品事实，仅验证证据 DTO 兼容。 |

## 业务开发对齐点

- Skill runtime spec 由业务服务返回哪些字段。
- Tool 可用性、风险等级和白名单由业务服务如何表达。
- 模型可选列表、默认模型和计价快照如何返回。
- 内容安全证据是否需要业务服务二次校验。

## 【业务开发】需要提供的能力与参数

| 能力 | 参数 | Agent 用途 |
| --- | --- | --- |
| Skill 池分页查询 | `auth_context`、`scope_filter`、`page_size=10`、`cursor` | 只返回 Published Skill，供路由使用。 |
| Skill runtime spec | `skill_id`、`version`、`auth_context` | 返回意图、步骤、Tool refs、输出元素结构、Memory 设置、`confirmation_policy`。 |
| Tool 执行策略 | `tool_name`、`tool_type`、`auth_context`、`risk_context` | 返回是否可用、风险等级、是否确认、超时、重试和取消策略。 |
| 模型选择与计价快照 | `resource_type`、`model_id`、`auth_context` | 返回用户可见模型名、默认模型、`pricing_snapshot_id`、非敏感模型运行快照或不可用错误。 |
| 安全证据校验 | `safety_evidence`、业务写入对象摘要 | 业务侧确认 scene、target_type、digest、expires_at 是否匹配写入对象。 |
