# Dora Agent

Dora Agent 是一个面向短视频 AIGC 创作的受信本地 Agent Demo。后端使用 Go、Eino ChatModelAgent、动态 Storyboard 和持久化生成工作流，前端使用 React 渲染对话、故事板、审批和任务状态。它尚未实现面向公网的真实登录鉴权、租户授权和接口限流，不能直接作为生产安全边界。

本地验收验证 Tool → Approval → Operation/Batch/Job → Worker → Finalization/Barrier → Durable Session/A2UI → React 的流程连通性，并保留 DeepSeek 推理与真实 Image2 图片生成。Image2 使用 `DORA_IMAGE2_API_KEY` 调用真实 provider，返回的 `b64_json` 解码后写入本地素材目录；Seedance 可使用确定性 MP4 占位，Audio 使用 Demo WAV，Assembly 使用 JSON manifest。`DORA_LOCAL_DEMO=true` 不会覆盖已配置的真实 Image2，也不要求真实视频/音频合成、视频拼接或转码。

本文只描述当前代码可以运行的行为。`docs/` 下三份详细设计仍包含后续目标，不应把其中的全域 Event Projector、独立 Stage Ledger、Redis Streams 或独立 PostBatchContinuation Graph 当成现状；但 Batch Barrier 已经持久化完整不可变 `PostBatchPayload`，并原样放入 durable `BatchContinuationResult.Result`。

## 当前实现状态

| 能力 | 状态 | 当前边界 |
| --- | --- | --- |
| 五个 Agent Tool | 已实现 | Runner fail-closed 固定注册且校验恰好五个：`analyze_materials`、`plan_creation_spec`、`plan_storyboard`、`generate_media`、`assemble_output`；不存在可切换的旧 Agent Registry 工厂 |
| Skill 渐进加载 | 已实现 | 每个 Agent run 都重新执行 `SkillBackend.List`；空列表不注入 Eino Skill 指令或 `skill` loader，运行中导入 Skill 后下一 turn 自动启用，无需重启 Runner；loader 的内部进度/错误不投影为用户 `tool_runs` 卡 |
| Capability Graph | 已实现 | 每个 Tool 编译为 `validate_request → execute_capability → validate_result` 的有界 Eino DAG；业务 Query/Command 仍在执行节点内调用 |
| 内部模型推理 | 已实现 | 所有 Capability 推理和缺失 Prompt 生成复用显式 Eino 子图 `prepare_messages → chat_model → decode_json`；Creation Spec 使用精确字段 schema，持久化前最多重试一次，失败不写 Revision/Approval |
| 动态 Storyboard | 已实现 | Aggregate/Active Revision/Pending Revision、动态 Module/Element、PromptSlot、AssetSlot、Dependency、CAS 和定向命令均已落库 |
| 审批与续作 | 已实现 | Spec、Storyboard 保留系统 chat Approval，同一 Session 仅一张可操作卡且按流程串行，系统卡存在时抑制模型候选预览；Candidate Asset 仍逐项创建 durable Approval，但素材预览与统一确认只在左侧 Storyboard，由相关 Job 全终态后一次冻结并提交候选批次。冻结命令、版本栅栏、Continuation 与 Command Ledger 继续作为逐项业务真相 |
| Operation/Batch/Job | 已实现 | PostgreSQL 是状态真相源，支持 Worker lease、重试、取消、Finalization、Batch Barrier 和 Generation Outbox；Job 终态/补偿结算与 `batch.finalize_requested` 同事务提交 |
| Provider | 部分实现 | 本地验收配置真实 `DORA_IMAGE2_API_KEY` 使用同步 Image2 Adapter；Seedance 可留空并使用确定性占位资产，Audio 和 Assembly 始终是 Demo Provider。真实 Seedance Key 仍可启用异步 Submit/Poll/Cancel |
| Billing | 部分实现 | 已实现 PostgreSQL 积分账户、生成前余额估算、幂等扣费/退款和 Batch 净费用；幂等键绑定不可变金额与业务引用，冲突重放会被拒绝；当前使用 postpaid，`reserve_then_settle` 仅有模型定义 |
| Session Runtime | 已实现 | UserMessage、generic/Approval ResumeRequested、ApprovalContinuationResult、BatchContinuationResult 均先写 Durable SessionInput；已启动输入独占 Session 队首，消息历史按 Turn 因果边界重建，跨实例 session lease/fence 驱动 Runner |
| Runner 重放收据 | 已实现 | 外层 Agent 模型原始输出按 `TurnID + model-call ordinal` first-write-wins 冻结；provider-output normalizer 只向下游提取一个顶层对象并做极有限闭合符修复，无 ToolCall 的最终响应仍不合法时最多重试一次 |
| A2UI/SSE | 部分实现 | 严格 A2UI 1.0/known-action parser 与整包 fail-closed 不变；外层 normalizer 不放宽协议，只清理单对象格式噪声；聊天区对一次 `generate_media`/`assemble_output` 只保留一张稳定高层 ToolRun 卡，终态通过 `refresh_resources` 刷新左侧资源；SessionEventLog/cursor 回放和 Tail Relay 已实现 |
| Stage/Event Projector | 待实现 | 没有独立持久化 Stage Ledger，也没有覆盖所有领域的 Outbox→Inbox→Projector；Operation/Batch/Job 仍是 Store/REST read model 的 durable truth，但不再分别投影为聊天卡 |

## 当前运行链路

```text
UserMessage
  → Durable SessionInput / Session lease
  → Eino Runner / ChatModelAgent
  → 五个 Agent Tool
  → bounded Capability Graph
      → Artifact / Spec / Dynamic Storyboard
      → 或 Operation / Batch / Job / Generation Outbox

Generation Outbox
  → Redis List（唤醒提示）
  → Lifecycle Worker / Provider / Finalization / Billing
  → Job 终态或补偿结算同事务写 batch.finalize_requested
  → Batch Barrier 唯一终态事件
  → Durable BatchContinuationResult
  → 按策略直接提交，或再运行一次 Agent 解释结果

Agent A2UI、Generation Outbox 投影、Capability/Approval Decision 直发
  → SessionEventLog
  → SSE replay/tail
  → React 工作台
```

## 核心目录

- `cmd/aigc-agent`：服务入口，装配数据库、Redis、Runner、Capability、Worker、Outbox relay 和 HTTP Router。
- `internal/aigc/agent`、`internal/aigc/modelreceipt`：DeepSeek ChatModelAgent、Eino Runner、Middleware 和外层 Agent 模型输出 receipt。
- `internal/aigc/capability`：五个 Agent Tool 的 Intent、可信上下文、结果协议和 bounded Graph。
- `internal/aigc/capabilityruntime`：Capability 的领域编排和内部 ChatModel 推理子图。
- `internal/aigc/storyboard`：动态 Storyboard Aggregate、Revision、Command、CAS 和 DomainEvent。
- `internal/aigc/approval`、`internal/aigc/approvalruntime`、`internal/aigc/artifact`：durable Approval、Continuation、版本化 Artifact 和审核命令 receipt。
- `internal/aigc/generation`：Operation/Batch/Job、Outbox、Redis List、Worker、Provider 生命周期、Finalization 和 Barrier。
- `internal/aigc/events`、`internal/aigc/sessionruntime`：SessionEventLog、SSE Tail Relay 和 Durable SessionInput。
- `internal/aigc/tools`：Provider 与部分旧 Tool 实现；旧 `internal/aigc/mediagraph` 代码只保留为隔离测试素材，生产入口、HTTP 路由和 Redis 队列均不再装配它。
- `frontend/src/features/aigc`：工作台页面、A2UI dispatcher 和 UI Command 调用。

## 五个 Agent Tool

Agent 只能填写 Tool Intent。`session_id`、`user_id`、`request_id`、`idempotency_key`、`tool_call_id` 和 `stage_run_id` 由服务端可信上下文注入或派生，不出现在 Agent-facing schema 中。

| Tool | 当前行为 |
| --- | --- |
| `analyze_materials` | 读取已归一化 Asset 的文件名、MIME、URL 和 metadata，经内部模型子图生成版本化 Material Analysis Artifact。当前不读取图片、音视频或 PDF 的实际内容，对这类输入返回 `partial` 和 `missing_inputs`。 |
| `plan_creation_spec` | 基于用户目标、已有 Spec 和 Material Analysis，按固定 Creation Spec schema 生成完整候选；JSON 解码或 schema 校验失败时只在持久化前重试一次。两次均失败则直接返回错误，不保存 reviewing Revision、不创建 Approval。 |
| `plan_storyboard` | 基于已确认 Spec 动态推理 Module、Element 数量、PromptSlot purpose、AssetSlot 和 Dependency；元素规划节点禁止生成 Prompt 文本。领域代码先规范化模型缺失/重复的 Module/Element ID 与 key、AssetSlot key，随后由独立内部 ChatModel 节点统一为所有 Provider-backed 槽生成可审核 Prompt（即使模型给出 `requires_prompt=false`），再保存 Pending Revision 并创建 durable Approval。遗漏、空值、重复或额外返回任一 Prompt 都使整次规划失败。已有 Artifact 只有明确 `ErrNotFound` 才视为不存在。只用于首次规划或整体 replan。 |
| `generate_media` | 对已批准的 Active Revision 再做一次缺失 Prompt 安全补齐；模型响应必须与请求的 `target_id/purpose` 集合一一对应，空值、部分、重复或额外项都拒绝。随后选择当前可生成目标，创建持久化 Operation/Batch/Job 并立即返回 `accepted`；Pending Revision 存在时拒绝启动。空选择只返回五种稳定 no-op：`waiting_candidate_approval`、`generation_jobs_in_flight`、`dependency_blocked`、`production_complete`、`no_targets_for_requested_phase`，并按调用幂等键冻结 receipt。 |
| `assemble_output` | `validate` 检查依赖；`plan` 保存版本化 Assembly Plan；依赖齐全时，`preview/export` 创建 Assembly Operation/Batch/Job。当前 Assembly Provider 输出 JSON manifest，不是真实视频渲染器。 |

`prepare_prompts`、Image2、Seedance、Audio、Assembly 都不是 Agent Tool。Prompt 推理位于 Capability 内部模型子图，Provider 仅由 Worker 调用。Runner 不接受外部 `ToolKeys` 覆盖，启动时要求 Registry 恰好等于固定五 Tool，缺失或多出任何 Tool 都 fail closed。

### Graph 边界

五个 Tool 在启动时分别编译下面的有限 DAG：

```text
START
  → validate_request
  → execute_capability
  → validate_result
  → END
```

- `validate_request` 校验可信 `session_id/request_id/idempotency_key`。
- `execute_capability` 调用类型化 Handler；当前领域查询、命令和 dispatch 是 Handler 内的 Go 调用，不是各自独立的 Eino Node。
- `validate_result` 只接受 `completed/accepted/waiting_user/partial/failed/cancelled`。

需要模型推理时，Handler 显式调用另一个已编译的内部 Eino Graph：

```text
prepare_messages → chat_model → decode_json
```

这条子图负责素材 metadata 分析、Spec/Storyboard 规划和 Prompt 生成，输出 JSON 后仍由领域代码校验和落库。`plan_creation_spec` 明确要求 `title/video_type/target_audience/output_language/duration_seconds/aspect_ratio/narrative_driver/visual_style/sound_style/model_preference/markdown/fields` 字段 schema；首次解码或校验失败后最多再调用一次内部模型，只有完整候选通过后才允许保存 Revision 和创建 Approval。`plan_storyboard` 明确分成“元素规划 → 唯一标识规范化 → 独立 Prompt ChatModel 节点 → 领域校验/保存 Pending Revision → Approval”；元素规划只声明 PromptSlot purpose，不能夹带 Prompt 文本。所有 Provider-backed 槽都必须有 Prompt，模型声明的 `requires_prompt=false` 不能绕过。

### Durable Turn 因果与重放边界

- User Message 与 Durable Input 在同一事务保存，并把该消息的 `ContextMessageSeq` 冻结到输入。一个输入一旦被执行过（`Attempts > 0`），就成为该 Session 的 head-of-line；即使进入 `retry_wait`，后续高优先级输入也不能越过。只有不存在已启动输入时，才按 UserMessage/Resume/Batch 的优先级和 `enqueue_seq` 选择尚未开始的输入。
- 消息重建按稳定 `RunID` 把一次 Turn 的 user/assistant/tool 记录视为一个因果组。`ThroughSeq` 排除的是整个后排用户组，而不是只排除 user 行；当前用户组在逻辑顺序中位于前序 Turn 输出之后。窗口 `Limit` 是分组、过滤和排序后的软消息预算，只能从尾部选择完整 Run，不能切开 ToolCall/Result 链；当前用户 Run 必须完整保留，因此单个 Run 可使结果超过 Limit。需要 Agent 解释的 Batch 在第一次成为队首时，冻结当时所有已终结 UserMessage 的最大边界；显式无前序消息用 `ContextSeqFrozen=true + ContextMessageSeq=0` 表示，重试不会扩大历史窗口。
- `aigc_model_output_receipts` 冻结外层 ChatModelAgent 的原始 Provider 响应，键为 durable `TurnID + model-call ordinal`；流式响应先完整拼接再冻结。外层 normalizer 随后只处理无 ToolCall 的最终文本：提取恰好一个顶层 JSON object，并最多修复一个可明确判定的不匹配闭合符；多个对象、DSML、歧义或仍无法通过严格 A2UI parser 的内容一律拒绝。Receipt 中保留未清理的原始输出，重放时再确定性归一化。Capability 内部子图不使用该 receipt。
- Approved `ApprovalContinuationResult` 携带服务端生成的受信 next-capability directive。Middleware 将它转换为一个稳定 ToolCall，并严格校验 Tool、参数、CallID 和 Result 配对；该 Capability 完成后，本 Turn 进入 explanation-only，模型再产生任何 ToolCall 都会被替换为确定性的严格 A2UI stop card。下一阶段只能由新的持久化事件开启新的 Turn。
- Runner 返回的完整 Agent 事件先冻结到 `session_turn_runs.output_payload_json`，再做权威 Message/Event 投影。投影或完成回执失败时重试同一 frozen output，不再次调用 Runner/模型；已有 frozen output 的 Turn 不受普通 `MaxAttempts` dead-letter 上限影响。已成功发送的实时 Tool 进度用 `ProgressPublished` 标记，权威重放只补未确认进度；Approval Resume 在冻结领域命令成功 Apply 前不会提前发布完成进度。
- 仍未实现把 Message、SessionEventLog、TurnRun、Input 和全部必要 Outbox 收敛为一个 effective-turn 最终事务。若尚无 frozen output 的输入耗尽重试，PostgreSQL 会在把 Input/Turn 置 `dead` 的同一事务追加稳定、脱敏的 `a2ui.error` SessionEvent；Tail Relay 再按 seq 可靠投递。

## 动态 Storyboard 与 UI Command

Storyboard 是单一版本化 Aggregate：

- `Version` 随每次领域变更递增；`PlanRevision` 只在完整 Pending Revision 被批准后推进。
- Active/Pending Revision 都保存动态 Module；Module 的语义类型、元素数量和媒体槽由模型按场景生成，不使用固定镜头/角色/音轨枚举。
- Element 使用稳定 `target_id`，包含可独立修订的 PromptSlot 和 AssetSlot。
- Dependency 决定生成前置条件和 Prompt/Asset 变更后的 stale 传播。
- 每个 Storyboard Command 持久化 canonical payload fingerprint；同一 `(storyboard_id, command_id)` 只有 payload 完全一致才可幂等重放，不同 payload 返回 idempotency conflict。数据库唯一约束按 Storyboard 复合分区，避免不同 Aggregate 的相同 CommandID 互相冲突。
- 整体 replan 的 Pending Revision 在审批期间允许旧 Active Revision 的 Job 完成；若 `preserve_approved_assets=true`，Promotion 的最终 CAS 会把这期间新激活且语义兼容的 Binding rebase 到候选 Revision，依赖签名已变化的槽仍标记 stale。
- Candidate 审核激活和 `auto_approve` 直接激活都会把下游依赖闭包标记 stale；Assembly 把 required 且 stale 的 Slot 视为缺失，拒绝 preview/export 派发。
- `ResolveGenerationInput(target_id, asset_slot)` 是派发、Worker Finalization token 重算和 Candidate 审批校验的单一语义源：统一解析 Dependency Asset、Prompt/Revision/Epoch 和 InputFingerprint。Prompt 选择只允许同一套 purpose 匹配规则，并且仅在元素恰有一个 PromptSlot 时使用该单一 Prompt fallback，不能由各调用方各自猜测。
- 普通目标的 BindingToken 校验 TargetRevision、PromptRevision、GenerationEpoch、SpecVersion 和 InputFingerprint；AggregateVersion 保持为 0，避免无关目标编辑误杀仍有效的结果。Assembly Token 额外冻结 AggregateVersion，Spec 或整个 Storyboard Aggregate 变化都会使旧 manifest 结果失效。
- TurnContext Middleware 读取动态 Aggregate，并把 Active/Pending 的紧凑状态注入下一轮 Agent。

左侧编辑不绕 Agent，直接调用定向 HTTP Command：

```text
PATCH  .../targets/:target_id/prompt
POST   .../targets/:target_id/regenerate
POST   .../targets/:target_id/assets/:asset_id/bind
POST   .../approvals/:approval_id/decision
POST   .../storyboards/:storyboard_id/candidate-approvals/decision
GET    .../generation-operations/:operation_id
POST   .../generation-operations/:operation_id/control
```

Prompt 更新、绑定和局部重生成都要求 expected version/revision；局部重生成会推进 GenerationEpoch，并在同一 Storyboard DomainEvent 中保存可信的 `RegenerationDispatchSnapshot`（Provider、媒体类型、用户、Spec/Storyboard 版本、估算费用、GenerationInput 和 Provider Payload），再据此创建 Operation/Batch/Job。HTTP 响应丢失后的同幂等重放先从该快照恢复，不能按已变化的 Storyboard 重新解释请求。槽位内“上传素材”会先 multipart 上传 `/api/aigc/assets`，再以精确 TargetID/AssetSlot 和版本栅栏直接绑定并激活 Asset；存在 Pending Revision 时禁用。动态 Storyboard 的 Active/Candidate 音频 Asset 与显式 A2UI `AudioPreview` 由浏览器原生 `<audio controls preload="metadata">` 试听。

Candidate Binding 继续带各自的 durable `approval_id`，但前端不会把这些 Approval 逐项渲染到 chat。左侧 Storyboard 只有在该 Storyboard 相关 Job 全部终态后才显示统一确认入口，并一次调用 `POST .../storyboards/:storyboard_id/candidate-approvals/decision`，携带 `decision=approved`、`expected_storyboard_version` 和稳定 `idempotency_key`。服务端首次请求冻结当时所有 pending Candidate 的精确 `(approval_id, binding_id, expected_decision_version)` 集合；随后逐项复用原 durable Decision/Continuation。部分失败返回逐项结果，使用同一幂等键重试只续作冻结批次，不会夹带点击后新产生的 Candidate。

上传与绑定是两个独立请求；绑定失败时，已上传的 available Asset 仍留在素材列表。当前 Demo 上传从 Session 记录取得 `user_id`，表单若提供不同值会返回 403；上传没有显式大小限制和幂等键，TOS 对象使用 `public-read` URL。Asset detail 要求 `session_id`、校验 Asset 属于该 Session 且 `availability=available`。这些仍不是登录用户授权；生产环境仍需补认证、限额、私有对象/签名 URL 和上传幂等。

高层 Capability ToolRun 卡通过 `operation_id` 对非终态任务提供取消，并对 `failed/partial_failed` 提供 `retry_failed`。取消作用于原 Batch；已终态 Batch 的取消是返回原 Aggregate 的 202 幂等 no-op。`retry_failed` 先按请求幂等键查找既有 Recovery Workflow；命中时直接返回冻结结果，不再读取当前余额、Provider 或 Storyboard。首次请求只选择 `status=failed + error_stage=provider + ProviderErrorRetryable=true`、仍语义有效且非 superseded/orphaned 的 Job，创建新的 `_recovery` Operation/Batch/Jobs。它会重新执行完整 Provider 流程，不是 Finalization-only 恢复。前端已把 `cancelling` 映射为“取消中”并视为 in-progress。UI Command 通常直接使用 HTTP 响应刷新当前页面。Capability 规划与 Spec/Storyboard Approval Decision 仍在事务后直写 SessionEventLog；Worker Finalization 的终态投影发送 `refresh_resources` 刷新左侧 Storyboard、Assets 和 Jobs，不发布素材预览或逐项 Candidate chat 卡。

## Durable Approval

Spec/Storyboard 审批与 Worker 产生的 Candidate Asset 审批共用 durable deterministic 决策内核，但用户入口不同：

```text
保存候选状态
  → 创建 Approval（版本绑定 + approve/reject 冻结命令）
  ├─ Spec/Storyboard：事务提交后发布 chat approval card
  │   → 同一 Session 只保留一张可操作卡，按 Spec → Storyboard 串行提交决定
  └─ Candidate Asset：不发布 chat approval card；Job 完成只刷新 Storyboard
      → 相关 Job 全终态后，左侧 Storyboard 一次提交 candidate approval batch
      → 服务端冻结精确 approval/binding/decision-version 集合
      → 逐项恢复原 durable Decision
  → durable ApprovalContinuation
  → 执行一次冻结领域命令
  → Command Ledger 记录命令和结果 receipt
```

Spec/Storyboard 的审核入口必须是服务端发布的权威 Approval Card：卡片 `data` 携带 `approval_id`，正文可用 Markdown 展示候选详情，交互则使用“确认/拒绝” `SingleChoice` 和卡片统一的“提交”控件（当前 A2UI 没有独立 Button 组件）。同一 Session 同一时刻最多存在一张可操作的 Spec/Storyboard Approval，二者按创作流程串行；新审批发布前必须关闭或取代旧审批。系统 Approval 已存在时，模型生成的候选预览卡必须被抑制，避免同一事实出现两个确认入口。前端只有识别到 `approval_id` 才调用 `POST .../approvals/:approval_id/decision`；聊天框输入、普通表单提交以及模型生成的 Markdown 都只会形成 UserMessage，不能推进 Approval。Agent 在 `waiting_user` 后可以提示用户使用权威审核卡，但不得输出“请回复确认/输入确认”或仿制审核表单；这种伪入口必须在模型输出发布前被拒绝并触发受限重试。

Candidate 批量入口当前只接受 `approved`。它不是把多个 Approval 合并成一个可变决定，而是一个持久化 saga：批次记录以 Storyboard version 和幂等键冻结精确目标，每个子决定使用稳定 child idempotency key。请求中途失败时返回逐项结果（未完整时 HTTP 207）；使用同一幂等键重试继续未完成项，不重新扫描新 Candidate。

正常五 Tool 审批使用 `review_mode=durable`、`execution_mode=durable`，不会发出 `a2ui.interrupt_request`，也不依赖 Runner checkpoint。平台同时支持生产 Runner interrupt：前端调用 `/api/aigc/sessions/:session_id/messages/resume` 时，Runtime 配置下 HTTP 只校验非 Approval-bound 的 Runner mapping，并以 `checkpoint:<mapping_id>:resume:<mapping_epoch>` 固定 `ResumeRequested` InputID；确认消息与 SessionInput 在 PostgreSQL 同事务提交，HTTP 不 claim checkpoint、也不直接调用 Runner。Approval-bound mapping 必须走 Approval Decision API。无 Runtime 的同步 Runner Resume 仅是测试兼容路径。

Checkpoint 路径严格区分执行状态：`resume_queued` 只表示 `ResumeRequested` 已排队、尚未调用外部 Runner/Graph；Session Runtime claim 后进入 `resuming`。若进程在外部调用期间中断，同一 InputID/TurnID 会从 `resuming` 做 at-least-once Runner replay，所有 Tool/Command 继续使用稳定幂等键。Runner 输出先冻结；Approval-bound Resume 必须先 Apply 对应 ApprovalContinuation，成功后才投影冻结输出。输出投影完成后进入 `resume_applied`，补齐最终回执后才是 `resumed` 并发布 `a2ui.interrupt_resolved`；对 `resume_applied` 的重放不得再次调用外部 Resume。

Approval Store 自身有 decision outbox/continuation 记录。Continuation claim 带 `LeaseOwner/LeaseUntil`：有效 lease 被其他执行者持有时，Session Runtime/Relay 延后到 lease 到期后再试，不消费普通错误次数，也不会提前投影成功或确认 outbox。真正执行失败的 Continuation 可从 `failed` 状态重新 claim；Relay 对单行错误隔离、指数退避，累计 10 次后标记 `dead`，并继续处理其他到期行。冻结领域命令提交后、Continuation receipt 写入前若崩溃，重试会检查 Spec 状态、Storyboard command receipt 或 Artifact review receipt，确认已提交后只补写 Command Ledger/Continuation applied，不会重复产生业务效果。

Approved 续作不再由模型自由决定下一步：Creation Spec v1 固定调用 `plan_storyboard(create)`，后续版本固定调用 `plan_storyboard(replan,preserve_approved_assets=true)`；Storyboard/Candidate 的冻结 command result 会携带审批后的 Aggregate。只有该 Aggregate 无待审 Candidate、且全部 Provider-backed/required 生产槽都有匹配的 active Binding 时，才固定调用 `assemble_output(preview,video)`；否则固定调用 `generate_media(auto_next,all_eligible)`。每个 Turn 恰好执行这一项，额外 ToolCall 被 stop card 阻断。

Artifact Store 的 `aigc_artifact_command_receipts` 把审核命令的 session、kind、artifact/version、预期 `reviewing` 状态、approve/reject、`require_latest` 和不可变结果快照 first-write-wins 地冻结，并与 Artifact 生命周期变更同事务提交。首次执行的 latest fence 会拒绝已被新版本取代的旧候选；如果旧导出 A 已激活但进程在补 Continuation ledger 前崩溃，之后新导出 B 激活，A 的重放会先命中 receipt 并只补外层 ledger，不会重新激活 A。当前主 `assemble_output(preview/export)` 仍由 Demo Worker 自动保存 manifest，不会自动创建 Export Result Approval；receipt-backed export review 是已实现的 Store/ApprovalRuntime 能力边界。

Decision A2UI 事件使用稳定 `approval_id + decision_version`，只投影该决定冻结的数据；可变 Storyboard 快照用带 Aggregate Version 的独立事件发布。Spec/Storyboard 初始审批卡和 Decision 结果仍由 HTTP/运行时在事务后直接发布。Candidate 单项不发布 chat 卡或单项 chat Decision；批量端点只发布最新 Storyboard 与批次摘要。这里仍不是通用 Event Projector。

## Operation / Batch / Job

- Operation 是一次 Tool 或 UI Command 发起的用户可见工作单元；当前一个 Operation 对应一个 Batch。
- Batch 冻结 CompletionPolicy、WakePolicy 和 DeliveryPolicy，并作为所有 Job 的持久化 Barrier。
- Job 保存 Provider、Payload、BindingToken（包括 SpecVersion；Assembly 还包括 AggregateVersion）、lease、状态版本、失败重试、持久化 Provider Poll 计数/上限、扣费、补偿和结果 Asset。
- 创建工作流、状态迁移和对应 Generation Outbox Event 在 PostgreSQL 事务内提交。
- 用户侧 Session Jobs 与 Operation detail API 会移除 Job Payload/Result、BindingToken、Provider task/request、lease、User/幂等键和账务交易 ID；失败 Job 也不返回 ResultAssetIDs，成功 Asset 通过 availability-filtered Asset API 获取。

Generation Outbox Dispatcher 将 `job.dispatch` 写入 Redis List，并把 operation/job/batch 信号投影到 SessionEventLog。Dispatcher 先扫描全部 pending Row，再只按 `AvailableAt` 处理到期项；普通单行失败会增加 Attempts、按指数退避推迟，累计 10 次后进入 `dead`，同时继续处理其他到期行，避免 poison row 头阻塞。内部幂等的 `batch.finalize_requested` 是例外：终态 Job 已没有后续 Scheduler 唤醒，它会持续退避重试直至确认，不能 dead-letter 后永久挂住 Batch。当前没有多实例 publisher claim lease，也没有 dead-row 管理/重放 API。Redis 使用 `RPUSH/BLPOP`，没有 Streams consumer group、ACK 或 reclaim；它只负责低延迟唤醒。Recovery Scheduler 按数据库中的 `NextRunAt` 和过期 lease 扫描并重新入队，所以 PostgreSQL 才是恢复真相源。

Lifecycle Worker 每次只推进一个可恢复阶段：Provider Submit/Poll、Artifact Finalize、Billing 或 Compensation。Provider 调用期间会续租，重复队列消息由 Job 状态版本和 lease 栅栏吸收。每次真实 Poll 前先持久增加 `ProviderPollAttempts`；默认总上限为 120，跨 Worker 重启不重置，耗尽后以 `provider_poll_exhausted` 失败。正常 accepted/pending 不增加失败 `RetryCount`，只有真实可重试错误消耗 Provider 失败预算。Provider 输入/配置、401/403 和大多数 4xx 属于永久错误，网络、存储、5xx 等默认可重试；Provider 未知状态属于协议错误并 fail closed。Billing 的账户不存在、余额不足、引用交易不存在、超额退款和幂等冲突属于永久业务错误，其余存储/基础设施错误可重试。Compensation 出错时先持久化 retry schedule/下一条 Outbox，再把原 generation outbox 标记 published；这不是 Redis List ACK。

### Provider 现状

| Provider | 当前实现 |
| --- | --- |
| Image2 | 本地验收必须配置 `DORA_IMAGE2_API_KEY`，使用真实同步 JobHandler 调用 `gpt-image-2`；`b64_json` 解码后通过当前 Uploader 持久化。代码仍保留无 Key + `DORA_LOCAL_DEMO=true` 的 PNG fallback，但它不属于本轮真实 Image2 验收口径。 |
| Seedance | 有 `DORA_SEEDANCE_API_KEY` 时使用真实 Submit/Poll/Cancel，ProviderTaskID 持久化后再轮询；无 Key 且 `DORA_LOCAL_DEMO=true` 时通过同步 Adapter 写入确定性的 MP4 占位 Asset，不调用真实视频生成或转码。真实 Provider 的 DELETE 202 只表示取消已受理，Poll 返回 `cancelled/canceled` 才收敛为 cancelled，未知状态按永久协议错误处理。 |
| Audio | 始终注册的 Demo Provider；生成确定性的两秒 WAV 占位 Asset，metadata 明确标记 `demo_placeholder`。 |
| Assembly | 始终注册的 Demo Provider；把 Assembly manifest 保存为 JSON Asset，不输出真实合成视频。 |

本地验收推荐显式设置 `DORA_LOCAL_ASSET_DIR=.local/aigc-assets`。真实 Image2 返回的 `b64_json` 解码结果、Seedance/Audio 占位和 Assembly manifest 都写入该目录；Router 通过 `DORA_LOCAL_ASSET_BASE_URL`（默认 `/api/aigc/local-assets`）静态提供文件，Asset URL 形如 `/api/aigc/local-assets/<object-key>`，因此不需要 TOS。验收必须配置 `DORA_IMAGE2_API_KEY`；`DORA_SEEDANCE_API_KEY` 可留空以启用本地占位。

### Finalization 与 Billing

Provider 结果先保存为 `pending_billing` Asset。Finalization 在任何外部扣费前把 Provider usage receipt（实际点数和费用明细）一次性冻结到 Job；崩溃恢复始终复用该 receipt，不重新读取可变 Provider 结果。随后校验 BindingToken，计算实际费用或默认点数，执行幂等扣费，再次校验 token，并将 Asset 设为 available 后创建 Candidate/Active Binding。普通目标重算当前 SpecVersion 和语义指纹；Assembly 使用版本化 Artifact 中冻结的 manifest，并同时要求计划派发时的 SpecVersion 与 AggregateVersion 未变化。需要审核的 Binding 同时创建 durable Approval，但不创建 chat 卡。成功 Job 的 generation outbox 可重试投影最新 Storyboard；投影失败不会把已成功结果误判为失败或触发退款。

Finalization 的领域 Commit 返回错误时，错误处理始终使用 Commit 前冻结的原 Job ID 重新读取 Job，并把原始 Commit error 传给 retry/permanent 分类；不会用零值 Job 覆盖身份，也不会用后续 `job not found` 掩盖根因。因此可重试错误进入 `retry_wait`，永久错误进入明确失败状态，不会无限卡在 `finalizing`。

当前会话创建时会为 `user_id` 初始化积分账户。派发前会校验 Provider 已注册、参数白名单/边界，并按图片输出数量等可信参数估算余额；除字段缺失/null 外，整数参数必须是 JSON number，任何字符串都不会通过 preflight。Operation RequestFingerprint 使用 Job 从 Batch 实际继承后的 DeliveryPolicy，并包含有效 Poll 上限，确保预检、执行和幂等重放对同一语义达成一致。这不是额度预占。默认计费为 image 12 点、video 120 点；Demo audio/music/voice/assembly 在服务入口配置为 0 点。扣费和退款使用独立幂等键；同一键若改变 kind、user、points、reference、operation/batch/job 或 breakdown 会返回 `billing idempotency conflict`。若扣费后取消或绑定失效，会进入 Compensation，Batch Barrier 等待补偿完成后再计算 gross/refund/net。

只有补偿已进入显式永久失败状态的 Job 才允许人工终结。受保护的管理端点为 `POST /api/aigc/admin/generation/jobs/:job_id/compensation/finalize`，要求 `AIGC_ADMIN_TOKEN` 对应的 Bearer Token，并在保存人工退款点数/交易 ID 的同一 Job mutation 中写入 settlement `batch.finalize_requested`；它不是普通用户 API，也不替代真实后台鉴权与审计。

当前生产路径只使用 `postpaid_no_reservation`。`reserve_then_settle` 常量和校验存在，但尚未接入余额预占。

## Batch 终态与 Session 续跑

单个 Job 终态不会直接续跑 Agent。所有 Job 终态 mutation（包括取消/失败）以及补偿完成/人工终结都会同事务写稳定幂等的 `batch.finalize_requested`；事务后的直接 Barrier 调用仅用于降低延迟，失败不回滚已提交 Job，由 Outbox 恢复并可安全重放。所有 Job 终态且已扣费 Job 的补偿处理完后，Batch Barrier 才会原子写入唯一 `batch.completed/partial_failed/failed/cancelled` Outbox Event，其中包含 Job 摘要和净费用。

正常 `generate_media` 与 `assemble_output` Batch 固定使用 `wake_policy=on_failure`：成功结果不再唤醒模型解释。Generation outbox 依据 durable Operation/Job 状态更新同一张高层 Capability ToolRun；Job Finalization 和 Batch 终态通过 `refresh_resources` 刷新左侧 Storyboard、Assets 和 Jobs。Candidate Approval 与素材预览都不进入 chat，统一确认入口由左侧 Storyboard 在相关 Job 全终态后开放。只有 partial/failed/cancelled 才创建需要 Agent 解释的 Turn。

当前后续链路是：

```text
Batch terminal Outbox
  → SessionSignalPublisher
  → Durable BatchContinuationResult
  → Session Runtime
      ├─ NeedsAgentExplanation=false：直接提交该输入
      └─ NeedsAgentExplanation=true：再次调用 Agent 做结果说明
```

Generation UI 不为 Operation、Job 或内部 Stage 创建独立聊天卡。所有 `generate_media` 调用归并更新 `tool_run:generate_media`，所有 `assemble_output` 调用归并更新 `tool_run:assemble_output`；每类能力在聊天区只有一张稳定高层卡。它展示用户能理解的总体状态和 Job 汇总，不展开逐 Job 节点，也不承载结果素材预览。卡片以 Batch/Operation 版本做单调更新，并保留 `operation_id` 供取消/失败重试。Operation/Batch/Job 的完整状态、`status_version` 和逐 Job 结果仍保存在 Store，并通过 REST read model 与左侧工作区读取；该 ToolRun 只是 A2UI 投影，不是独立持久化 Stage Aggregate/Ledger。若同一 Turn 中成功的 `generate_media`/`assemble_output` Tool Result 已投影到该权威卡，且没有其他公开 Tool 需要解释，模型生成的进度复述必须在发布和持久化前被抑制。

这里没有独立的 PostBatchContinuation Graph：不会在 Batch 之后统一写 ToolOperationResult、推进 Stage 或创建下一条 Approval。Candidate Approval 已在各 Job Finalization 时创建。Barrier 已把完整不可变 `PostBatchPayload`（Session/Workflow/Stage/Operation/ToolCall/Batch 关联、BatchVersion、终态、CostSummary、逐 Job 终态/Asset/错误码/费用、解释策略和创建时间）保存到 `Operation.Result` 和唯一 terminal outbox；`BatchContinuationResult.Result` 原样携带这份可信 payload，Agent 无需也不得从 Provider Payload 重建费用或结果。需要解释的 BatchContinuationResult 与 ApprovalContinuationResult 都以可信 `system` 内部事件启动新 Turn，不伪造 user message；无需解释的 Batch 输入直接 commit。

## A2UI 与 SSE

Agent 的普通回答和表单都必须输出纯 `ActionEnvelope`；后端不从自然语言、Markdown 表格或任意 Tool Result 猜测 UI：

```json
{
  "a2ui_version": "1.0",
  "actions": [
    {
      "type": "append_card",
      "surface": "chat",
      "card_id": "brief-intake",
      "card": {
        "root": "root",
        "components": [
          { "id": "root", "component": { "Card": { "children": ["product"] } } },
          { "id": "product", "component": { "TextInput": { "key": "product_name", "label": "产品名称/品类", "required": true } } }
        ]
      }
    }
  ]
}
```

后端和前端只接受 `a2ui_version="1.0"` 以及已知的 `append_card/update_card`；Envelope 中只要混入未知版本、未知 Action 或无效 Card，整包 fail closed，不执行其中任何部分。实时 SSE 和历史回放共用同一协议门禁。

严格 parser 本身不提取文本、不补字段，也不修复 JSON。仅 ChatModelAgent 外层 provider-output normalizer 可以从格式噪声中提取恰好一个顶层对象，并最多修复一个明确不匹配的闭合符；修复后的候选仍必须完整通过同一个严格 parser，否则进入最多一次模型纠错重试。

聊天 `append_card.card_id` 会在持久化前扩展为实例级唯一 ID。Generic A2UI 表单在提交前完整校验所有 `required` TextInput、SingleChoice、MultiChoice 和 FileUpload；通过后才归约成普通 UserMessage，同时保存 `ui_source.card_id`，然后进入 Durable SessionInput。

外部可见事件最终都写入 PostgreSQL `aigc_session_event_log`，按 session 分配单调 `seq`，并以稳定 source key AppendOnce。SSE 支持 `after_seq`、`Last-Event-ID`、历史回放和按序 Tail；`a2ui.ready` 只是连接元数据，不推进回放 cursor。前端丢弃不递增的 live seq；Storyboard 全量快照按 Aggregate Version 合并，Patch 只在 `base_version == current.version` 时应用，发现向前缺口会回源 REST。高层 Capability ToolRun 使用 Batch/Operation 版本门禁抵御重复或乱序投影；逐 Job `status_version` 留在 Store/read model，不形成聊天节点。终态 `refresh_resources` 使前端重新读取 Storyboard、Assets 和 Jobs。会话切换会增加 generation、Abort 旧 REST 请求并让旧 SSE/异步响应失去写回资格，避免上一会话覆盖当前工作台。实时与历史聊天卡的 `append_card/update_card` 共用同一 reducer，`action.payload` 与 patch 采用相同合并语义。真正 Runner Resume 成功后发布稳定的 `a2ui.interrupt_resolved`，前端据此关闭对应 interrupt surface。内存 NotificationHub 只是当前单进程的低延迟提示，Tail Relay 始终轮询数据库保证正确性。

Approval 决策请求按 `approval_id + decision + expected_decision_version` 冻结 idempotency key；成功或收到终态事件后，会话内记录 terminal tombstone。后续慢到的 hydration、历史回放或重复 append 不得复活已关闭的审核卡，提示文案也只依据实际冻结并提交的 decision。

事件来源并不完全统一：

- Agent 的完整 Turn 输出先冻结，再由权威投影使用稳定 source key 写 SessionEventLog；实时 Tool 进度只是可确认的低延迟提示。
- Generation 状态先写 Generation Outbox，再归并到单张高层 Capability ToolRun，并在终态投影 `refresh_resources` 与 BatchContinuationResult；Operation/Job 明细、素材预览和 Candidate Approval 不投影到 chat。
- Capability 规划的 Storyboard/Spec Approval 卡、Approval Decision 结果仍在领域事务提交后直接写 SessionEventLog；UI Command 还会直接使用 HTTP 响应。
- 当前 DurableEventBroker 没有按领域区分 producer kind，项目也没有通用 Inbox/Event Projector。

浏览器只执行 `/events/stream` 中的 `a2ui.ready`、`a2ui.action`、`a2ui.interrupt_request`、`a2ui.interrupt_resolved` 和 `a2ui.error`；错误事件使用命名空间，避免与 EventSource 的连接级 `error` 事件冲突。正常五 Tool 审批使用 `a2ui.action` approval card。A2UI 协议仍支持 Image/Video/Audio Preview，但生成结果的预览不放入聊天 ToolRun；终态 `refresh_resources` 后由左侧 Storyboard/素材区从公开 REST 视图加载图片、视频、音频或 Assembly manifest。用户侧 REST/SSE 使用公开视图：Storyboard 隐藏 Command ledger/fingerprint，Generation 隐藏 Provider Payload/Result、BindingToken、lease、内部 request/task ID、账务交易和原始错误正文，Approval Decision 响应隐藏冻结命令/Continuation/Outbox，Asset 只暴露当前 Session 中 `availability=available` 的记录。

## 设计文档

- [AIGC Tool 编排与动态故事板详细设计](docs/aigc-tool-storyboard-design.md)
- [AIGC ChatModelAgent Demo 详细设计](docs/aigc-chatmodelagent-demo-design.md)
- [AIGC Generation Worker 详细设计](docs/aigc-worker-design.md)

这些文档同时标注当前实现与后续目标；判断当前行为时以代码和测试为准。

## 本地运行

本地全流程需要 Go 1.26.3、Node.js/npm、PostgreSQL、Redis、DeepSeek API Key 和 Image2 API Key。仓库的 Docker Compose 可以启动 PostgreSQL/Redis；验收必须同时设置 `DORA_DEEPSEEK_API_KEY` 与 `DORA_IMAGE2_API_KEY`。Seedance 和 TOS 凭据可不设置，也不需要安装 ffmpeg。

```bash
cp .env.example .env.local
docker compose -f docker-compose.local.yml up -d
```

在 `.env.local` 中填写 DeepSeek 与 Image2 Key，并保留本地 Demo 配置：

```text
DORA_DEEPSEEK_API_KEY=...
DORA_IMAGE2_API_KEY=...
DORA_LOCAL_DEMO=true
DORA_LOCAL_ASSET_DIR=.local/aigc-assets
DORA_LOCAL_ASSET_BASE_URL=/api/aigc/local-assets
```

加载环境变量后启动后端：

```bash
set -a
source .env.local
set +a
# 本地验收保留真实 Image2，只关闭真实 Seedance 以使用视频占位链路。
unset DORA_SEEDANCE_API_KEY
go run ./cmd/aigc-agent
```

启动前端：

```bash
cd frontend
npm install
npm run dev
```

默认后端监听 `:18080`，前端监听 `:3200`。本地占位资产由后端的 `/api/aigc/local-assets/*` 路由提供；浏览器看到的是同源相对 URL。

### 本地全流程验收

1. 在前端创建 Session 并提交创作目标，处理仍保留在 chat 中的 Spec、Storyboard approval 卡。
2. 确认 `generate_media` 返回 `accepted` 后，聊天区只有一张稳定的 `generate_media` ToolRun 卡持续更新；不存在 Operation/Job/Stage 多卡或逐 Job 节点，终态会刷新左侧 Storyboard、Assets 和 Jobs。
3. 确认图片由真实 Image2 生成并持久化到 `/api/aigc/local-assets/`；Seedance 视频可为明确标记 `demo_placeholder` 的占位 Asset，音频为 Demo WAV，Assembly 为可读取的 JSON manifest。
4. 确认 Candidate 不产生逐项 chat approval 卡；相关 Job 全终态后，左侧 Storyboard 只提交一次“统一确认”，并能用相同幂等键恢复部分完成的 durable 决策。
5. 确认 Batch Barrier 只产生一次终态续作，页面刷新或 SSE 重连后仍可从 SessionEventLog 恢复状态；不要把 Seedance/Audio/Assembly 占位结果当作真实合成或转码能力验收。

若需要每次从空环境验收，可在停止服务后执行下面的清理命令。它会删除本机 Docker 数据卷和本地占位资产，不会保留历史数据：

```bash
docker compose -f docker-compose.local.yml down -v
rm -rf .local/aigc-assets
```

## 验证命令

```bash
go test ./internal/aigc/... -count=1
```

本地 Tool → Worker → A2UI 全链路可定向回归：

```bash
go test ./internal/aigc/integration -run TestLocalDemoFlowFromCapabilityThroughWorkerToA2UI -v -count=1
```

```bash
cd frontend
npm test
npm run build
```

```bash
git diff --check
```

## 开发约束

- Agent Registry 只暴露五个高层 Capability；Provider、Prompt、余额、权限和业务 CRUD 不得注册成 Agent Tool。
- Tool schema 只包含 Intent；身份、关联、版本和幂等字段必须来自可信 `context.Context`。
- Tool 返回领域结果及 Artifact/Operation/Batch 引用，不返回 UI Event、base64、Provider 临时 URL 或完整 Provider Payload。
- Capability 外层保持有限 DAG；新增模型推理应放在显式内部 Eino Graph 中，并在模型输出后执行领域校验。
- `plan_storyboard` 只做首次规划和整体 replan；局部 Prompt、绑定和重生成继续走定向 UI Command。
- 创建 Operation/Batch/Job 后立即返回 `accepted`，不得在 Agent Tool 内等待 Provider 完成。
- Provider Result 必须经过 BindingToken、Billing 和 Finalization；失效结果不得覆盖当前 Storyboard。
- Generation 状态变化必须同时写 Generation Outbox；不要把 Redis List 当作状态真相源。
- `job.succeeded` 的专用 Finalization 投影只刷新 Storyboard，不得重新加入 Candidate chat approval 卡；Spec/Storyboard 卡和对应 Decision 仍存在事务后直发窗口，不能把这些路径描述成全域 Event Projector。
- Generation chat 只允许每次 `generate_media`/`assemble_output` 一张高层 ToolRun；Operation/Batch/Job 明细保留在 Store/read model，终态用 `refresh_resources` 更新左侧，素材预览和 Candidate 统一审核不得回到聊天区。
- 同一 Session 的 Spec/Storyboard Approval 必须唯一且串行；存在系统 Approval 时抑制模型候选预览，不能出现并行确认入口。
- Candidate 统一确认必须冻结精确审批批次并逐项复用 durable Decision；幂等重试不得重新扫描并混入新 Candidate。
- Skill middleware 必须在每个 Agent run 查询 backend；空列表不得注入 Eino Skill 指令/loader，loader 的内部进度和错误不得进入用户 `tool_runs`。
- 不要把 `stage_run_id` 关联字段描述成独立 Stage Ledger，也不要把完整 `PostBatchPayload` 误写成独立 PostBatchContinuation Graph；前者已持久化并随 BatchContinuationResult 传递，后者尚不存在。
- 只有实际创建 Eino checkpoint mapping 并通过 Runner Resume 的流程才能使用 `a2ui.interrupt_request`；普通审批使用 durable Approval Card。
- Agent 必须输出 `a2ui_version + actions` 的纯 JSON；协议外文本会转换成错误事件。
- SessionEventLog 是 SSE 真相源；内存通知和 Redis 队列都只用于唤醒。
- 已启动的 Durable SessionInput 是当前 Session 的因果队首；不要让后续高优先级输入越过其 `retry_wait`。
- 外层 Agent 模型与完整 Turn 输出必须先写 first-write-wins receipt 再驱动 Tool/权威投影；不要把该保证扩大到 Capability 内部 ChatModel 子图，也不要把 frozen output 误写成 effective-turn 全事务提交。
- Artifact 审核重放必须先查不可变 ReviewCommandReceipt，再检查可变 lifecycle；旧版本 receipt 只能补 Continuation ledger，不能回滚较新的 Active Artifact。
- 无 frozen output 的 Turn 终态失败必须与稳定、脱敏 `a2ui.error` SessionEvent 原子落库；SSE 消费只在 Tail Relay write/flush 成功后推进 cursor。
