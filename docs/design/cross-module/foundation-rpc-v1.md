# Foundation RPC v1 契约

> 文档状态：Frozen / v1 基础契约 + Development Preview 子集
>
> 契约标识：`foundation.rpc.v1`
>
> 评审日期：2026-07-14
>
> Preview 子集更新：2026-07-17
>
> IDL Owner：Business Module

## 1. 目的与边界

Foundation RPC v1 用于验证三个独立 Go Module 开始业务开发前必须成立的基础链路：单一 Thrift IDL、Kitex 生成代码、Business RPC Runtime、etcd 服务发现、Agent Client 超时、请求关联和优雅摘除。

本契约最初冻结的基础范围只包含无业务副作用的 `Probe`。`Probe` 不查询或修改用户、Project、Skill、积分、Asset、Session、Graph、Operation、Job 或 Worker Attempt，也不能作为调用方鉴权、业务授权、Liveness 或外部流量入口。

2026-07-16 起额外批准仅供 [`plan_creation_spec.v1preview1`](../agent/graphtool/plan_creation_spec-design.md#0-v1-开发预览设计冻结2026-07-16)、未注册的 [`analyze_materials.v2preview1`](../agent/graphtool/analyze_materials-design.md#0-v2-tool-core-开发预览设计冻结2026-07-16)、[`plan_storyboard.v2preview1`](../agent/plan-storyboard-runtime-v2-design.md) 与 [`write_prompts.v2preview1`](../agent/write-prompts-runtime-v2-design.md) 使用的 Development Preview 子集。该子集复用同一 Service 以缩短功能纵切，但不改变 `Probe` 的无副作用语义，也不代表 Foundation Service 已升级为通用业务服务。Preview 方法必须受 Business 显式开发开关保护；存在 Agent Runtime 路径的方法还必须受 Agent 开关保护，缺少开关或生产环境一律失败关闭。开发顺序以 [Dora 功能优先开发与试跑计划](../../requirements/full-function-smoke-development-plan.md) 为唯一口径。

[AIGC 跨 Module 契约目录](aigc-contract-catalog.md) 继续保持 `Draft / v1alpha1`。Foundation RPC v1 的冻结不代表其中的计费、Approval、Job、Event 或 Graph Tool 契约已经通过评审。

## 2. Owner 与生成规则

| 项目 | 决定 |
|---|---|
| IDL 源 | `business/api/thrift/foundation/v1/foundation.thrift` |
| IDL Owner | Business Module |
| Provider | `business/cmd/business-service` |
| Consumer | `agent/cmd/agent-service` |
| Worker | 本版本不调用；不能为满足联调虚构 RPC 服务 |
| 生成输出 | Business 与 Agent 各自在本 Module 保存生成代码 |
| 生成器 | Kitex `v0.16.2`、thriftgo `v0.4.5` |
| 服务名 | `dora.business.foundation.v1` |
| etcd Prefix | `/dora/services/dora.business.foundation.v1/` |

两个 Module 的生成代码来自同一 IDL，但使用各自 Module Path 生成，不互相 import，也不创建第四个共享 Go Module。生成代码禁止手工修改；`scripts/generate-foundation-rpc.sh` 是唯一生成入口。

## 3. RPC 定义

```text
BusinessFoundationServiceV1.Probe(FoundationProbeRequestV1)
  → FoundationProbeResponseV1
  throws FoundationServiceExceptionV1
```

Development Preview 额外方法：

```text
GetCreationSpecContextPreviewV1             # Owner/Project 只读校验
SaveCreationSpecDraftPreviewV1              # command_id first-write-wins Draft 写入
QueryCreationSpecDraftCommandPreviewV1      # 原 command_id/digest 权威查询
BatchGetAssetAnalysisInputsPreviewV1        # 本地 text/image Evidence exact-set 只读
GetStoryboardPlanningContextPreviewV1       # 可信 CreationSpec Draft Ref exact snapshot
SaveStoryboardDraftPreviewV1                # 隔离 JSON Draft first-write-wins 写入
QueryStoryboardDraftCommandPreviewV1        # 原 Storyboard command/digest 权威查询
GetPromptGenerationContextPreviewV1         # 可信 Storyboard Preview Ref 最小安全投影
SavePromptDraftPreviewV1                    # 全 Source Slot 隔离 Prompt Draft first-write-wins
QueryPromptDraftCommandPreviewV1            # 原 Prompt command/digest 权威查询
```

前三个 CreationSpec 方法使用 `creation_spec.preview.rpc.v1`，跨 Module 摘要固定向量为 [`creation_spec_preview_save_digest_v1.json`](testdata/creation_spec_preview_save_digest_v1.json)；Material Evidence 方法使用 `asset_analysis_inputs.preview.rpc.v1`，完整边界见 [`material-analysis-evidence-preview-v1.md`](material-analysis-evidence-preview-v1.md)；Storyboard 三方法使用 `storyboard.preview.rpc.v1`，只允许 [`plan_storyboard.runtime.v2preview1`](../agent/plan-storyboard-runtime-v2-design.md) 消费，保存摘要向量为 [`storyboard_preview_save_digest_v1.json`](testdata/storyboard_preview_save_digest_v1.json)；Prompt 三方法使用 `prompt.preview.rpc.v1`，只允许 [`write_prompts.runtime.v2preview1`](../agent/write-prompts-runtime-v2-design.md) 消费，保存摘要向量为 [`prompt_preview_save_digest_v1.json`](testdata/prompt_preview_save_digest_v1.json)。完整字段号、枚举和 DTO 以 Owner IDL 为准。它们不是生产 `BIZ-AIGC-*` 接口，不得被其他 Tool、Worker 或外部调用方复用。

### 3.1 请求

| 字段号 | 字段 | 约束 |
|---|---|---|
| 1 | `schema_version` | 必须精确等于 `foundation.rpc.v1` |
| 2 | `request_id` | 必须是调用方生成的 UUIDv7；同一次有限重试保持不变 |
| 3 | `caller_service` | 必填稳定服务名；最大 128 字节 |
| 4 | `caller_version` | 必填构建版本；最大 128 字节 |
| 5 | `sent_at_unix_ms` | UTC Unix 毫秒；必须大于零 |

### 3.2 响应

| 字段号 | 字段 | 约束 |
|---|---|---|
| 1 | `schema_version` | 固定为 `foundation.rpc.v1` |
| 2 | `request_id` | 必须原样回显请求 ID，用于跨服务证据关联 |
| 3 | `service_name` | 固定为 Business Runtime 稳定服务名 |
| 4 | `service_version` | Business 构建版本 |
| 5 | `environment` | Business 运行环境 |
| 6 | `instance_id` | 实际处理请求的 Business 实例 |
| 7 | `received_at_unix_ms` | Business 接收请求的 UTC Unix 毫秒时间 |
| 8 | `plan_storyboard_runtime_enabled`（optional） | Business 启动时冻结的本地 Storyboard Preview gate；新 Business 明确返回，旧关闭态响应可缺省；Agent 启用时必须存在并与自身 flag exact-match |
| 9 | `plan_storyboard_runtime_profile`（optional） | gate 开启时必须存在且固定为 `plan_storyboard.runtime.v2preview1`；新 Business 关闭时明确返回空串，旧关闭态响应可缺省 |
| 10 | `write_prompts_runtime_enabled`（optional） | Business 启动时冻结的本地 Prompt Preview gate；新 Business 明确返回，旧关闭态响应可缺省；Agent 启用时必须存在并与自身 flag exact-match |
| 11 | `write_prompts_runtime_profile`（optional） | gate 开启时必须存在且固定为 `write_prompts.runtime.v2preview1`；新 Business 关闭时明确返回空串，旧关闭态响应可缺省 |

Agent 在 Ready 前分别复核字段 8/9 与 10/11：对应 Runtime 自身开启时，该字段对必须同时存在、Business gate 为 true、环境为 `local` 且 Profile exact-match；自身关闭时允许旧 Business 同时缺省字段对，或新 Business 明确返回 `false + 空串`，只出现一个字段仍失败关闭。任一端单独开启或 Profile 漂移都属于永久契约错误并立即失败启动。响应不得包含主机内部路径、数据库信息、Secret、调用栈或业务数据。

### 3.3 稳定错误

| 错误码 | Retryable | 语义 |
|---|---|---|
| `INVALID_ARGUMENT` | false | 请求为空、版本错误、ID 非 UUIDv7、字符串越界或时间非法 |
| `INTERNAL` | false | 无法构造安全响应；外部只返回固定中文信息 |

etcd 暂无实例、连接失败、请求超时和连接中断属于 Client Transport Error，不伪装成业务错误。Agent 只在启动预算内有限重试只读 `Probe`；超时后保持未就绪并失败启动。

### 3.4 Development Preview 错误与幂等

| 错误码/状态 | Retryable | 语义 |
|---|---|---|
| `FEATURE_DISABLED` | false | 开关关闭；必须在进入 Preview Service/Repository 前失败关闭 |
| `PREVIEW_UNAVAILABLE` | true | 开关已开但 Preview Service 未接线或本地依赖不可用 |
| `NOT_FOUND` | false | Project 不存在、非 Owner 或不可写，统一隐藏资源存在性 |
| `PROJECT_VERSION_CONFLICT` | false | 加载上下文后 Project 版本已经变化 |
| `CREATION_SPEC_VERSION_CONFLICT` | false | Storyboard 保存或读取时 CreationSpec version/digest 已变化 |
| `VERSION_CONFLICT` | false | Prompt 保存或读取时 Storyboard Preview version/digest/status/schema 已变化 |
| `IDEMPOTENCY_CONFLICT` | false | 原 `command_id` 已绑定不同请求摘要 |
| `PERSISTENCE_UNAVAILABLE` | true | 存储或 Transport 结果不确定；Agent 只能按原 `command_id + request_digest` 查询 |
| `ASSET_ANALYSIS_VERSION_CONFLICT` | false | Preview Asset 已授权，但与请求的可选 expected version 不一致 |
| `ASSET_ANALYSIS_EVIDENCE_CONFLICT` | false | 已持久化 Evidence 违反判别联合、digest、locator、绑定或重复约束 |
| `LIMIT_EXCEEDED` | false | Preview 请求/响应超过冻结的 8 Asset / 32 Evidence 上限；不分页或截断 |

`SaveCreationSpecDraftPreviewV1` 的 `command_id` 是 Business first-write-wins 身份。同键同摘要返回首次 Draft，同键异摘要稳定冲突；写响应丢失后只允许调用 Query，不得换键盲写。

`SaveStoryboardDraftPreviewV1` 使用相同 first-write-wins 规则，但 request digest 额外绑定 CreationSpec ID/version/content digest 与严格 Storyboard JSON。其两张隔离 Preview 表不等于生产 Storyboard/Revision/Element/Slot；局部 key 不能作为生产资源 ID。

`SavePromptDraftPreviewV1` 继续使用相同 first-write-wins 规则，request digest 绑定 Storyboard Preview ID/version/content digest、双 Validator 版本、opaque `exact_target_set_digest` 与完整 Prompt JSON。Business 从权威 Storyboard 全部 Slot 重新派生目标并逐项复核可信字段；完整 scope digest 还包含 Agent Intent/Artifact/Policy pins，因此 Business 只把它作为 request digest 绑定的 opaque pin，不宣称独立重算。两张隔离 Prompt Preview 表不等于生产 PromptArtifact/Revision，也没有 ready/active/Approval 状态。

## 4. 服务发现契约

Business 在 HTTP 与 RPC Listener 都绑定成功后，用同一进程所有的 etcd 租约发布两个 Endpoint。Foundation RPC 记录使用以下 JSON：

```json
{
  "service": "dora.business.foundation.v1",
  "instance_id": "business-local-1",
  "address": "host.docker.internal:19081",
  "version": "dev",
  "registered_at": "2026-07-14T00:00:00Z"
}
```

- `address` 必须是其他实例可访问的 Advertised Address，拒绝回环和通配地址。
- `foundation-smoke` 的 Runtime 在宿主机启动；若模板使用 `host.docker.internal`，脚本会发布默认路由的非回环 IPv4，避免把容器访问宿主机的别名误用于宿主机自访问。
- 所有记录绑定租约；KeepAlive 中断必须使 Business 退出 Readiness。
- Agent Resolver 只解析固定 Prefix 和有类型 JSON，忽略格式错误记录；没有合法实例时返回发现错误。
- 进程退出先置为未就绪并撤销租约，再停止 RPC/HTTP。
- etcd 记录不是鉴权声明，也不保存业务配置。

## 5. Runtime 与安全约束

1. Business RPC 使用独立监听地址和显式 Read/Write、连接空闲、退出超时。
2. Agent Client 使用显式 Connect Timeout、RPC Timeout 和 Startup Probe Timeout；框架自动重试保持关闭。
3. `Probe` Handler 执行严格 DTO 校验，不访问 PostgreSQL、Redis、外部网络或其他业务 Service；Preview Handler 使用独立 Service/Repository 边界，不能改变 Probe 路径。Evidence Preview 只访问 PostgreSQL，并必须以一个集合 SQL 完成授权 Asset 与 Evidence exact-set 读取。
4. 普通日志只允许记录 Request ID、调用方服务、处理实例、状态和耗时，不记录完整 Payload。
5. 本地环境允许明文 Thrift；当前 Preview RPC 没有独立 Agent→Business caller authentication，只允许本地 Development Preview，且生产配置禁止启用。生产发布前必须迁移到正式业务 RPC Owner，并增加服务身份认证、传输 TLS 和网络最小权限。Foundation `Probe` 或 Preview 成功不能替代这些安全门禁。
6. Business 关闭对应 Preview 开关时，各 Preview Handler 必须在进入 Service/Repository 前返回 `FEATURE_DISABLED`；Agent 关闭 CreationSpec 开关时不得注册其写路由或启动 Processor。Evidence 与 Prompt Adapter 在当前批次没有 Runtime/Registry 接线；后续接线前必须增加独立 Agent 开关。

## 6. 兼容性

- 已发布字段号不得复用；新增字段只能使用新编号并保持旧消费者可读。
- 删除字段、改变字段语义、服务名、etcd Prefix 或错误语义必须发布新主版本。
- Consumer 必须拒绝未知 `schema_version`，不能静默回退。
- IDL、生成代码、Handler、Client Mapper 和契约测试必须处于同一变更。

## 7. 验收

- [x] Business、Agent、Worker Owner 与非目标明确；Worker 不注册虚假 RPC。
- [x] IDL Source、版本、字段号、服务名和发现 Prefix 冻结。
- [x] 读请求无业务副作用，允许由 Agent 在单一启动预算内有限重试。
- [x] Request ID、版本校验、错误码和日志脱敏明确。
- [x] 明文只限本地；生产认证/TLS 未完成时不得宣称生产安全。
- [x] Business/Agent 生成代码可重复且无差异。
- [x] Handler/Mapper/Resolver 契约测试通过。
- [x] 本地真实 etcd + Kitex Probe 与优雅摘除冒烟通过。
- [x] CreationSpec Preview 的方法、摘要、幂等和 Unknown Outcome 查询边界已冻结。
- [x] Material Evidence Preview 的 text/image exact-set、单 SQL、授权折叠、版本与错误映射已冻结。
- [x] Material Evidence Preview 双 Module 生成代码、真实 PostgreSQL 与 Adapter 契约测试完成。
- [x] Storyboard Preview 的可信 CreationSpec Ref、隔离 JSON Draft、first-write-wins 与 Unknown Outcome 查询契约已冻结并生成双 Module DTO。
- [x] Storyboard Preview Business Repository/Handler、Agent Tool Core/Adapter、双 Module 契约及真实 PostgreSQL 并发 M1 测试完成；默认不注册且双端开关仍关闭。
- [x] Prompt Preview 的 Storyboard 最小投影、隔离 Draft/Receipt、first-write-wins、Unknown Outcome Query 与跨 Module 保存摘要已冻结并生成双 Module DTO。
- [x] Prompt Preview Business Repository/Handler、Agent Tool Core/Adapter、双 Module 契约及真实 PostgreSQL 并发 M2 测试完成；M3 Runtime/BFF/Workspace 已接线，Probe 以字段 10/11 强制双端 gate/Profile exact-match，静态生产 Catalog 仍未注册。
- [ ] Preview 生产化所需的独立 RPC Owner、服务身份认证、TLS 与网络最小权限后置到 P1；完成前只能标记开发预览。

评审结论：Foundation RPC v1 的 Probe 通过 M1.2 工程实现门禁；2026-07-17 起额外批准 `plan_creation_spec.v1preview1`、未注册 `analyze_materials.v2preview1`、`plan_storyboard.v2preview1` 与 `write_prompts.v2preview1` 的本地 Development Preview 子集。该结论不解除四个 Tool 的完整生产门禁、其他 Graph Tool 的独立门禁或生产安全门禁。
