# Media Runtime V3 Preview 跨 Module 契约

> 状态：**Approved for Development Preview / local-only**
>
> 契约集合：`business.media_asset.preview.v1`、`agent.media_job.preview.v1`、`media_job.preview.terminal.v1`
>
> 适用 Profile：[`media.runtime.v3preview1`](../agent/media-runtime-v3-preview-design.md)
>
> HTTP 路径与六工具组合：[MVP 六工具媒体扩展 V1](../agent/mvp-six-tools-media-extension-v1-design.md)

本文只冻结跑通一个 PNG Job 和一个 MP4 Job 所需的公共契约。未知主版本失败关闭；不得把字段复制为三个 Module 的共享 `internal` Go 类型。

## 1. Business Preview Asset 契约

### 1.1 Prepare DTO

`PrepareMediaAssetPreviewRequestV1` 精确字段：

| 字段 | 类型 / 规则 |
|---|---|
| `schema_version` | 固定 `media_asset.prepare.preview.v1` |
| `request_id`、`command_id`、`operation_id` | UUIDv7；command 为 first-write-wins 键 |
| `request_digest` | 64 位 lowercase SHA-256 |
| `user_id`、`project_id` | 可信 UUIDv7；Business 复核 Owner |
| `tool_key` | `generate_media` 或 `assemble_output` |
| `scope_digest` | 64 位 lowercase SHA-256 |
| `output_profile` | `png_640x360.v1` 或 `mp4_h264_640x360_2s.v1`，必须与 Tool 匹配 |
| `prompt_source` | generate 必填：`prompt_preview_id/version/content_digest/target_local_key` |
| `image_asset_source` | assemble 必填：`asset_id/version/content_digest` |

两种 source 必须恰好一个；禁止 Prompt 正文、路径、URL、Provider、价格和编码参数。

`PrepareMediaAssetPreviewResponseV1`：

```text
schema_version=media_asset.prepare.result.preview.v1
request_id, command_id, disposition(created|replayed), preparation_id
asset_ref{asset_id,version=1,status=reserved,media_kind,mime_type}
source_ref{source_type,source_id,source_version,source_digest,target_local_key?,target_digest?}
output_profile, staging_object_key, source_object_key?
created_at
```

generate 的 `target_local_key/target_digest` 必填，后者是选中 Prompt Entry 的规范 SHA-256；assemble 中二者缺省。`source_object_key` 只在 assemble 返回；所有 Object Key 是 Business 生成的相对 key，不是 URL/绝对路径/凭证，禁止进入 Tool Result、Event 或日志。

`source_ref.source_type` 精确值为：generate 使用 `prompt_preview`，assemble 使用 `image_asset`。三个 Module 不得引入 `media_preview_asset`、`asset` 等别名。

### 1.2 Finalize DTO

`FinalizeMediaAssetPreviewRequestV1`：

```text
schema_version=media_asset.finalize.preview.v1
request_id, command_id, request_digest, preparation_id
operation_id, batch_id, job_id, attempt_id, fence
terminal_status=ready|failed
output{content_digest,size_bytes,mime_type,width,height,duration_ms?,codec?,pixel_format?}?
error_code?
```

联合约束：`ready` 必须有 output 且无 error；`failed` 必须有白名单 error_code 且无 output。PNG 必须是 `image/png,640x360`；MP4 必须是 `video/mp4,640x360,duration_ms=2000±100,codec=h264,pixel_format=yuv420p`。

`FinalizeMediaAssetPreviewResponseV1` 返回 `disposition=created|replayed`、权威 Asset Ref、finalization receipt ID、content digest/size/mime 和完成时间。同 `command_id + request_digest` 重放；同 command 异 digest 冲突。响应未知使用以下 Query，禁止换 command：

- `QueryMediaAssetPreparationPreviewV1(command_id, request_digest, user_id, project_id)`；
- `QueryMediaAssetFinalizationPreviewV1(command_id, request_digest, preparation_id)`。

Query 只返回 `not_found|completed|conflict` 严格联合；`not_found` 仅表示当前无权威提交事实。

### 1.3 Range HTTP

```text
GET|HEAD /api/v1/projects/:project_id/media-preview-assets/:asset_id/content
```

要求已有登录、Project Owner、Asset `ready`；不存在、未 ready、越权统一 `404`。响应固定 `Content-Type`、`Content-Length`、`ETag`、`Accept-Ranges: bytes`、`Cache-Control: private, no-store`；只支持无 Range 或单一 `bytes=start-end`，合法范围返回 `206 + Content-Range`，非法/多 Range 返回 `416`。路由不接收 Object Key/路径，不使用静态目录或目录索引。

## 2. Agent 持久化消费契约 `agent.media_job.preview.v1`

Agent 是表/视图/函数的唯一 Migration Owner。Worker 角色只能 `SELECT agent.media_job_preview_v1_claimable` 及 `EXECUTE` 下列函数；`PUBLIC` 权限全部撤销。函数使用 `SECURITY DEFINER` 时必须固定安全 `search_path`、参数化 SQL、服务端时间、TTL/批量上限，并拒绝未知状态/版本。

```sql
CREATE VIEW agent.media_job_preview_v1_claimable AS
SELECT job_id, available_at, priority
-- 仅投影到期 pending/retry_wait，或 lease_expires_at 已过期的 running/reconciling；不暴露 payload。

agent.media_job_preview_v1_claim(
  p_job_id uuid, p_worker_id text, p_attempt_id uuid,
  p_claim_request_id uuid, p_lease_ttl_ms integer
) RETURNS TABLE (
  schema_version text, job_id uuid, batch_id uuid, operation_id uuid,
  session_id uuid, user_id uuid, project_id uuid, job_type text,
  definition_version text, scope_digest text, output_profile text,
  source_ref jsonb, target jsonb, artifact_request_digest text,
  attempt_id uuid, fence bigint, lease_expires_at timestamptz,
  created_at timestamptz, deadline_at timestamptz
)

agent.media_job_preview_v1_renew(
  p_job_id uuid, p_worker_id text, p_attempt_id uuid,
  p_fence bigint, p_lease_ttl_ms integer
) RETURNS TABLE (status text, lease_expires_at timestamptz)

agent.media_job_preview_v1_schedule_retry(
  p_job_id uuid, p_worker_id text, p_attempt_id uuid, p_fence bigint,
  p_retry_delay_ms integer, p_error_code text
) RETURNS TABLE (status text, available_at timestamptz)

agent.media_job_preview_v1_mark_reconciling(
  p_job_id uuid, p_worker_id text, p_attempt_id uuid, p_fence bigint,
  p_reason_code text
) RETURNS TABLE (status text)

agent.media_job_preview_v1_commit_terminal(
  p_job_id uuid, p_worker_id text, p_attempt_id uuid, p_fence bigint,
  p_terminal_event_id uuid, p_terminal_status text,
  p_result_schema_version text, p_result_digest text, p_result jsonb
) RETURNS TABLE (job_status text, batch_status text, operation_status text, terminal_event_id uuid)

agent.media_job_preview_v1_get(
  p_job_id uuid
) RETURNS TABLE (job_status text, attempt_id uuid, fence bigint,
                 lease_owner text, lease_expires_at timestamptz,
                 result_schema_version text, result_digest text,
                 terminal_event_id uuid)
```

`claim` 使用短事务与 `FOR UPDATE SKIP LOCKED`/CAS；首次成功把 `fence+1`，同 `claim_request_id` 重放同一 Attempt/Fence，异请求只能在 Job 处于到期待领状态，或 `running/reconciling` 的旧 Lease 已按数据库时钟过期后接管。View 中过期 Lease 的 `available_at` 投影为 `lease_expires_at`，因此其他 Worker 可发现但不能在 Lease 有效期内接管。接管 `reconciling` 后，Worker 必须先从共享 Worker Receipt 找到旧 Finalize command/digest 并 Query；`completed` 复用权威 Asset，`not_found` 才允许以新 Fence继续，禁止直接创建第二个 Finalize 命令。renew/retry/reconciling/terminal 都必须匹配当前 `worker_id + attempt_id + fence` 与允许的状态，`RowsAffected=0` 等价 `LEASE_LOST`。`commit_terminal` 同事务更新 Job→Batch→Operation 并 AppendOnce 写 Terminal Outbox；同 event/digest 重放，异义冲突。Worker 不获得普通 Agent 表 DML 权限。

### 2.1 `MediaJobEnvelopeV1`

```text
schema_version=agent.media_job.preview.v1
job_id, batch_id, operation_id, session_id, user_id, project_id
job_type=generate_png|assemble_mp4
definition_version, scope_digest, output_profile
source_ref{source_type,source_id,source_version,source_digest,target_local_key?,target_digest?,source_object_key?}
target{asset_id,asset_version=1,preparation_id,staging_object_key}
artifact_request_digest
attempt_id, fence, lease_expires_at
created_at, deadline_at
```

严格规则：generate 必须有 `target_local_key/target_digest` 且无 `source_object_key`；assemble 必须有 Business 生成的 PNG Object Key 且无 target 字段。`source_ref` 与 `target` JSONB 使用上述封闭字段集合，拒绝未知字段。Envelope 不含 Prompt、negative constraints、路径根、绝对路径、URL、Secret、费用、Approval、ffmpeg 参数或任意 metadata。

### 2.2 Terminal Result

成功 Result 固定为：

```text
schema_version=media_job.preview.result.v1
status=succeeded
asset_ref{asset_id,version=1,status=ready,media_kind,mime_type,content_digest,size_bytes}
finalization_receipt_id
```

失败 Result 固定为 `status=failed,error_code`，不含 stderr/堆栈/路径。`terminal_status` 只允许 `succeeded|failed`；一 Job Batch 不存在 partial/cancelled。

## 3. Terminal Outbox → Session Lane

Terminal Outbox Payload 固定 `media_job.preview.terminal.v1`：

```text
event_id, session_id, operation_id, batch_id, job_id
tool_key, terminal_status, result_schema_version, result_digest, result
occurred_at
```

Agent Bridge 以 `(session_id, source_type=media_job_preview_terminal, source_id=event_id)` AppendOnce 入 `session_input`，然后标记 Outbox delivered。终态 Processor 校验 DTO/digest 后确定性投影，不调用 ChatModel/Graph/Business 写接口。Outbox 重投、Bridge 崩溃、Redis 丢失或 SSE 重连都不得产生第二个 Input/Event。

## 4. Business 本地对象与权限边界

- 三个 Runtime 必须显式启用同一 local-only Profile；内部地址只允许 loopback；
- Business 建立 `0700` 根与固定 `staging/objects` 子目录；key 格式由 Business 生成并白名单解析。Worker 只从本地安全配置取得根，不从 Job 取得根；
- staging 文件为普通文件、`0600`、禁止 symlink/hardlink/path traversal；Finalize 复核 key、inode 类型、摘要、大小、magic 和当前 Fence 后原子 rename；
- Business Asset 表只保存相对 key；Range Handler 逐资源鉴权后打开，不暴露目录；
- Preview 没有服务身份认证/TLS，loopback 只是开发隔离，不满足生产安全。生产实现必须使用新版本契约、认证 ObjectStore/TOS 和独立安全评审。

## 5. 稳定错误码、兼容与回滚

最小错误码：`FEATURE_DISABLED`、`INVALID_ARGUMENT`、`NOT_FOUND`、`VERSION_CONFLICT`、`IDEMPOTENCY_CONFLICT`、`DEPENDENCY_NOT_READY`、`UNSUPPORTED_PROFILE`、`LEASE_LOST`、`FENCE_STALE`、`ARTIFACT_INVALID`、`FFMPEG_UNAVAILABLE`、`EXECUTION_TIMEOUT`、`UNKNOWN_OUTCOME`、`INTERNAL`。只有有权威证据证明未开始/未提交的瞬时错误可 `retryable=true`。

Preview 契约只允许追加可选字段；改变 Owner、状态、幂等键、函数语义、Job Payload 或文件规则必须发布新版本。回滚先关 Profile 和 Drain，再撤消费者，最后才允许 Down Migration；不得让旧 Worker 调用已删除函数，也不得物理删除尚被页面引用的 ready Asset。

## 6. 契约验收

- Business/Agent/Worker 分别做 DTO golden、未知字段/版本拒绝和 producer-consumer parity；
- PostgreSQL 实库验证 View/Function 权限、CAS、Lease/Fence、Terminal Outbox 原子性、无物理外键与中文 COMMENT；
- Prepare/Finalize/Claim/Terminal 每个响应丢失点均按原键查询收敛；
- Job/Outbox/日志扫描不得出现 Prompt 正文、绝对路径、URL、Secret 或 ffmpeg 参数；
- Range、路径穿越、symlink、旧 Fence、同键异义和多 Worker 竞争均有负向测试。

当前结论：**Approved for Development Preview。** 本契约不升级 `aigc.contract.v1alpha1` 完整生产 Draft，也不授权生产 `AGT-JOB-V1`、正式 `PrepareGeneration/FinalizeGeneration` 或静态 Catalog availability。
