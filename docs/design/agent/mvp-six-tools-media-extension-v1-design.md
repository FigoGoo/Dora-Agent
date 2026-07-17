# MVP 六工具媒体扩展 V1 设计

> 文档状态：**Approved for Development Preview / local-only**
>
> 基础 Profile：`mvp_all_tools.runtime.v1preview1`
>
> 媒体 Profile：`media.runtime.v3preview1`
>
> 实现状态：2026-07-17 `make trial-basic` 快速 MVP 主链已通过；完整质量/恢复门禁仍独立执行
>
> 依赖设计：[MVP All Tools Runtime V1](./mvp-all-tools-runtime-v1-design.md)、[Media Runtime V3 Preview](./media-runtime-v3-preview-design.md)、[Media Runtime V3 跨 Module 契约](../cross-module/media-runtime-v3-preview-contract.md)

本文只补齐“一个主 Agent 同时跑通六个 Graph Tool”所需的组合边界、入口与传输路径。媒体状态机、Job、Fence、文件校验和 Tool Graph 仍以依赖设计为准；本文不复制其领域状态机，也不授权生产开放。

## 1. 最小完成定义

同一登录用户、项目和 Agent Session 必须按顺序完成：

1. 普通消息；
2. `plan_creation_spec`；
3. 文本素材创建与 `analyze_materials`；
4. `plan_storyboard`；
5. `write_prompts`；
6. `generate_media` 产生可解码 PNG；
7. `assemble_output` 产生浏览器可播放 MP4；
8. 硬刷新后从 PostgreSQL Snapshot 恢复；
9. PNG/MP4 经同源 Owner 鉴权端点读取，并验证成功读取、合法 Range 与非法 Range 的 `200/206/416`；
10. 在已启动的本地 PostgreSQL、Redis、etcd 上重置专用测试库后，一条命令重复验收上述链路。

静态生产 Tool Catalog 仍不得把 `generate_media` 或 `assemble_output` 标记为 production available。页面只显示“本地开发预览”。

## 2. Profile 组合与失败关闭

四端使用以下精确环境变量：

| Runtime | 基础 Profile | 媒体 Profile |
|---|---|---|
| Agent | `DORA_AGENT_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1` | `DORA_AGENT_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1` |
| Business | `DORA_BUSINESS_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1` | `DORA_BUSINESS_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1` |
| Worker | 不适用 | `DORA_WORKER_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1` |
| Frontend | `VITE_DORA_RUNTIME_PROFILE=mvp_all_tools.runtime.v1preview1` | `VITE_DORA_MEDIA_RUNTIME_PROFILE=media.runtime.v3preview1` |

规则：

- 媒体 Profile 只有在 Agent、Business、Worker 三端精确同开且基础 Profile 在 Agent、Business 同开时才可启动；
- Frontend 只负责入口可见性，未知值或基础/媒体 Profile 不完整时失败关闭；
- 所有服务环境必须是 `local`，服务地址、Agent Consumer PostgreSQL 地址只允许 loopback；
- Business 和 Worker 必须指向同一个绝对、非符号链接、权限不宽于 `0700` 的本地对象根；
- Agent 启动探针必须验证 Business 基础能力和媒体能力；Worker readiness 必须验证对象根、Agent DB 函数、Business 内部端点以及 ffmpeg/ffprobe；
- 禁止以六个独立 boolean 代替 Profile，禁止媒体 Profile 静默降级为仅 PNG 或 Mock MP4。

有效能力精确为：普通消息加 `plan_creation_spec`、`analyze_materials`、`plan_storyboard`、`write_prompts`、`generate_media`、`assemble_output`。现有五条隔离 Profile 与 smoke 保持不变。

## 3. 单主 Agent 与 Session Lane

统一 Profile 仍只构造一个 `adk.ChatModelAgent`、一个 `mvpruntime.Coordinator` 和一个全局 PostgreSQL HOL。媒体扩展在同一 Agent Registry 追加精确两个 Tool；不得构造第二个媒体 ChatModelAgent 或第二个 Scanner。

新增两个可信输入来源：

- `generate_media_preview_request`
- `assemble_output_preview_request`

每个来源只接受 Business BFF 已绑定的 `user_id/project_id/session_id` 与严格结构化 Intent。对应 deterministic model dispatcher 只产生一个已冻结 Tool Call，不生成自然语言，不接收 Prompt 明文；Tool reduction 对该来源精确暴露一个 Tool。Processor 复用同一个 Coordinator 的 `ProcessNext` 轮转和全来源 HOL。

媒体 Job 终态来源固定为 `media_job_preview_terminal`。它由 Terminal Bridge AppendOnce 进入同一 Lane；Terminal Processor 不调用 ChatModel 或 Graph，只校验冻结 Result 并投影 Card。Bridge、Processor 和五个既有来源共用一个 Coordinator 生命周期。

## 4. 浏览器/BFF/Agent 入口

浏览器经 Business 同源 BFF 提交：

```text
POST /api/v1/agent/sessions/:session_id/generate-media-previews
POST /api/v1/agent/sessions/:session_id/assemble-output-previews
```

两者都要求既有登录 Session、CSRF、`Idempotency-Key` UUIDv7。Business 复用现有 Agent identity signer，绑定 Owner、Project、Session 和规范 target；Agent 验证签名、canonical target、deadline 与 Session binding 后入队。响应固定为 `202`：

```text
schema_version=media_preview.enqueue.v1
request_id, session_id, input_id, turn_id, run_id, tool_call_id
tool_key=generate_media|assemble_output
status=pending
replayed=true|false
```

Generate 请求 Body 精确为：

```text
schema_version=generate_media.preview.enqueue-request.v1
prompt_preview_ref{id,version=1,content_digest}
tool_intent{
  schema_version=generate_media.intent.v3preview1,
  prompt_preview_id,expected_prompt_version=1,
  expected_prompt_content_digest,target_local_key,
  output_profile=png_640x360.v1
}
```

两个 Prompt Ref 必须完全一致；Body 不接收 Project/User/Session、Prompt 正文或路径。

Assemble 请求 Body 精确为：

```text
schema_version=assemble_output.preview.enqueue-request.v1
source_asset_ref{id,version=1,content_digest}
tool_intent{
  schema_version=assemble_output.intent.v3preview1,
  source_asset_id,expected_source_version=1,
  expected_source_content_digest,
  output_profile=mp4_h264_640x360_2s.v1
}
```

两个 Asset Ref 必须完全一致；Body 不接收 Object Key、codec、duration、ffmpeg 参数或路径。

## 5. Business 内部媒体端点

Agent 和 Worker 通过同一个 loopback Business Base URL 使用严格 JSON；不扩展公开 Thrift IDL，避免把 local-only 契约误当生产 Foundation RPC。端点只在媒体 Profile 开启时注册：

```text
POST /internal/v1/media-preview-assets/prepare
POST /internal/v1/media-preview-assets/query-preparation
POST /internal/v1/media-preview-assets/finalize
POST /internal/v1/media-preview-assets/query-finalization
GET  /internal/v1/media-preview-assets/readiness
```

Prepare/Finalize/Query 的 Body 与响应精确使用跨 Module 契约命名的 V1 DTO，`Content-Type` 必须为 `application/json`，未知字段、重复 JSON key、尾随 token、未知 schema version 一律拒绝。`request_id` 只用于追踪；幂等与冲突仍由 `command_id + request_digest` 判定。

内部端点没有生产服务身份认证，只允许在 `SERVICE_ENV=local`、媒体 Profile 开启且监听/请求 RemoteAddr 为 loopback 时工作；否则返回 `404`。它们不得被浏览器调用，不返回绝对路径、URL、Prompt 或 Secret。

Readiness 成功只返回：

```text
schema_version=media_asset.readiness.preview.v1
profile=media.runtime.v3preview1
object_root_ready=true
prepare=true
finalize=true
```

对象内容仍只通过公开 Owner 鉴权端点读取：

```text
GET|HEAD /api/v1/projects/:project_id/media-preview-assets/:asset_id/content
```

## 6. Agent → Worker 与 Worker → Business

Agent 仍是 Operation/Batch/Job/Terminal Outbox Migration Owner。Worker 使用专用最小权限 Agent PostgreSQL DSN，只调用跨 Module 契约冻结的 View/Functions；不经 Redis 传输 Payload，不直写普通 Agent 表。

Worker 每次成功 Claim 后：

1. 严格解析 `MediaJobEnvelopeV1`；
2. 生成/验证 staging artifact；
3. 以原 preparation/job/attempt/fence 调用 Business Finalize；
4. Finalize 响应未知时按原键 Query；
5. 获得权威 ready Asset 后调用 `commit_terminal`；
6. terminal 响应未知时按 `media_job_preview_v1_get(job_id)` 收敛；
7. 只有证明未提交的瞬时失败才按原 Job 进入 retry；旧 Fence 立即停止。

Worker 自有数据库只保存 Attempt/Artifact Receipt/Finalize 查询事实的摘要和稳定 ID，不保存 Job Payload、Prompt、Object Key 根、完整路径、stderr 或命令参数。

## 7. 页面最小交互

正式 `ProjectWorkspacePage` 在两个 Profile 同开时追加：

- 从最新 completed Prompt Preview 中只列 `mediaKind=image` 目标；提交 Generate；
- 从当前 Workspace 的 completed media terminal Card 中选 ready PNG；提交 Assemble；
- accepted Card 显示 Operation/Asset ID 与等待状态；
- completed PNG Card 显示受保护图片；completed MP4 Card 使用 `<video controls>`；
- failed Card 显示白名单错误码和可否重试，不显示内部诊断；
- Snapshot、SSE、硬刷新和重连使用同一个严格 Parser/Reducer，不使用 localStorage 或 Mock 补结果。

媒体 Card Schema：

```text
schema_version=media_preview.card.v1
input_id,turn_id,run_id,tool_call_id
tool_key=generate_media|assemble_output
status=accepted|completed|failed
operation_id,batch_id,job_id?,asset_ref{id,version,status,media_kind,mime_type,content_digest?,size_bytes?}
result_code,updated_at,error_code?
content_url?
```

联合约束：accepted 不含 `job_id/content_digest/size/content_url/error`；completed 必须含 ready Asset、digest、size 和同源 `content_url`；failed 只含白名单 `error_code`，不含 content URL。终态 Card 以 `terminal_event_id` AppendOnce，不覆盖 accepted EventLog 历史。

## 8. 一键验收与回滚

`make trial-basic` 是快速 MVP 主链验收。它使用三个专用测试数据库、独立端口和对象根，从当前工作树构建并启动 Business、Agent、Worker、Vite 与 Chromium，在唯一基础 Profile 和媒体 Profile 下执行登录、项目、文本素材、六个 Tool、Worker PNG/MP4、受保护内容读取和 Workspace V5 硬刷新恢复。脚本还验证三端/Vite 受控清理和执行期间源码无变化；任一步失败均非零退出且不发布伪成功 Evidence。

2026-07-17 该命令已真实通过。成功 Evidence 固定为 `.local/smoke/trial-basic.json`、Schema `trial_basic.evidence.v1`、权限 `0600`，至少保存六 Tool Receipt、两个媒体终态 Job 的安全 ID/digest/size、浏览器 PNG 解码、MP4 播放就绪、同源 BFF、`200/206/416`、Workspace V5 恢复、清理和源码零漂移断言。

快速验收与完整质量/恢复门禁严格分离：`trial-basic` 不串行运行五条既有 canonical isolated smoke，不自动运行三 Module 全量测试或前端全量测试/build，也不证明 Runtime 重启恢复、Fence takeover、unknown outcome、故障注入、生产 Provider、计费或 Approval。isolated smoke、Module/Frontend 全量门禁和 P1 Evidence 必须按变更范围分别执行，任何文档不得把它们改写为 `trial-basic` 已内含或已通过。

回滚先关闭 Frontend 媒体入口，再停止新 Claim 并 Drain Worker，关闭三端媒体 Profile；保留 ready Asset 和 Receipt。基础统一 Profile 与五条隔离 Profile 不受影响。

## 9. 审核结论

本文批准上述 exact-set 的本地开发预览实现。任何真实 Provider、生产 Catalog availability、公共对象访问、任意媒体参数、多 Job Batch、计费、Approval/TOS、跨主机存储或非 loopback 部署，都必须发布新设计并重新评审。
