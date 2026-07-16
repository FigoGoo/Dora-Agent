# W0 身份与工作台契约 v1

> 状态：Frozen / 用户已批准实现
>
> 版本：`w0.identity-workspace.v1`
>
> 冻结日期：2026-07-14
>
> 覆盖：`SMK-001` 身份会话子场景、`SMK-002`、`SMK-003`、`SMK-004` 的基础契约
>
> 非目标：完整管理员 Data Scope RBAC、Eino Runner/Graph Tool、Worker、计费、支付、生产 Gateway 实现

## 1. 冻结依据

用户已确认按[全功能冒烟开发推进计划](../../requirements/full-function-smoke-development-plan.md)中的 `W0-DEC-001`～`W0-DEC-011` 执行。本文件是 W0 首批实现的统一契约源；三份详细评审包继续提供设计理由、测试和风险，不再允许各实现线自行选择相反语义：

- [Business 鉴权/Project 评审包](../business/auth-project-foundation-review.md)
- [Agent Session/Event 评审包](../agent/session-event-foundation-review.md)
- [SMK-001～004 垂直切片评审包](../testing/smk-001-004-vertical-slice-review.md)

## 2. Owner 与一致性

| 事实 | Owner | 冻结边界 |
| --- | --- | --- |
| User、Credential、Web Session、Project、QuickCreate Receipt、Project→Session Binding、Business Outbox | Business | 同一 QuickCreate 本地事务原子提交 |
| Session、显式空 Skill Snapshot、Message、Input、Command Receipt、EventLog | Agent | 同一 Ensure 命令本地事务原子提交 |
| 浏览器认证 Cookie | Business Auth | 原始值只出现在浏览器、同源 Gateway 和 Business Auth Resolver |
| Agent 身份上下文 | Gateway/Agent | Agent 只接收短期、签名、Audience/Method/Path 绑定的内部断言 |
| SSE Cursor 与 Event | Agent PostgreSQL | Redis/进程通知只降低 Tail 延迟 |
| Storyboard、Asset | Business | Agent Event 只携带版本化刷新引用，不成为业务真源 |

跨 Business/Agent 不承诺分布式原子事务。成功语义固定为：Business 原子接受并返回 Project，Agent 按稳定 Command 最终恰好一次建立 Session/可选 Input；Unknown Outcome 先查询原 Command Receipt。

## 3. Auth HTTP v1

统一前缀为 `/api/v1`，JSON 字段使用 `snake_case`。浏览器凭据只使用 HttpOnly Opaque Session Cookie，前端不得持久化 Access/Refresh Token。

### 3.1 查询当前会话

```text
GET /api/v1/auth/session
```

成功 `200`：

```json
{
  "status": "authenticated",
  "principal": {
    "id": "uuidv7",
    "display_name": "string",
    "email": "masked-or-safe-string",
    "account_status": "active",
    "roles": [],
    "capabilities": []
  },
  "csrf_token": "memory-only-token",
  "session_expires_at": "RFC3339"
}
```

无有效会话返回 `401 UNAUTHENTICATED`；Business 不可用返回 `5xx`，不得伪装成匿名。

### 3.2 登录

```text
POST /api/v1/auth/session
Content-Type: application/json
```

请求只包含 `email`、`password`。成功 `200`，返回与查询会话相同的安全 DTO，并通过 `Set-Cookie` 建立/轮换 Session。用户不存在、密码错误、账号不可用统一返回 `AUTH_INVALID_CREDENTIALS`；详细原因只进入受控审计。

### 3.3 退出

```text
DELETE /api/v1/auth/session
X-CSRF-Token: <session-bound-token>
```

首次和重复退出均返回 `204`，撤销服务端 Session 并清理 Cookie。会话过期、绝对期限、空闲期限、并发设备数和限流阈值必须来自版本化配置，不写死在业务代码。

## 4. QuickCreate HTTP v1

### 4.1 创建命令

```text
POST /api/v1/projects:quick-create
Idempotency-Key: <stable-user-intent-key>
X-CSRF-Token: <session-bound-token>
Content-Type: application/json
```

请求：

```json
{
  "initial_prompt": "optional string"
}
```

规则：

1. Principal 只来自认证 Context；请求不得出现 `user_id`；
2. Idempotency Key 绑定 User 和一次用户意图，技术重试保持不变；
3. Business 对 Prompt 做 Unicode 空白规范化并计算 Canonical Digest；
4. 同 Key 同 Digest 重放原 Project，同 Key 异 Digest 返回 `409 IDEMPOTENCY_CONFLICT`；
5. 空白 Prompt 编码为 `null`，不创建 Agent Message/Input/Turn；
6. W0 Session 创建时固定冻结显式空 Skill Snapshot；
7. W0 Project 标题固定为“未命名项目”，不从 Prompt 截取，也不接受客户端标题字段。

QuickCreate 幂等 `semantic_digest` 与 Agent `request_digest` 相互独立。前者按固定顺序对以下 UTF-8 紧凑 JSON 计算 SHA-256：

```json
{"schema_version":"project_quick_create.v1","prompt_present":true,"prompt_digest":"<lowercase-sha256-or-empty>"}
```

该摘要必须由 Business 领域根据规范化 Prompt 自行计算，不能由 Handler、测试 Fixture 或调用方注入。

固定向量使用 Prompt ` e\u0301 `（NFC 后为 ` é `）：`prompt_digest=273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df`，QuickCreate `semantic_digest=dbf7920d5641b2ed5a2564b8d09228e89bbdcd2281085d1fe2c7f59d221457e4`。

首次接受返回 `201`，同键重放返回 `200`。DTO 相同：

```json
{
  "project_id": "uuidv7",
  "session_id": null,
  "input_id": null,
  "creation_status": "provisioning",
  "workspace_ref": "/projects/<project_id>/workspace",
  "request_id": "uuidv7"
}
```

当 Agent Receipt 已投影完成时，重放或 Bootstrap 可以返回 `creation_status=ready` 及权威 `session_id`、可选 `input_id`。HTTP 首次响应不在 Business 事务内等待 Agent RPC。

非空首 Prompt 在 Business Outbox 中只以 AES-256-GCM 密文暂存。Agent Receipt 确认后 Outbox 进入 `delivered`，清除 algorithm/key version/nonce/ciphertext 并写 `payload_cleared_at`；为核对幂等语义可保留不可逆 `payload_digest/request_digest`。未交付状态不得提前清除密文，空 Prompt 不得伪造清除时间。

### 4.2 Project Bootstrap

```text
GET /api/v1/projects/{project_id}/bootstrap
```

返回当前用户有权访问的 Project 状态、`creation_status`、可空 Session/Input 引用和 Business 资源 Snapshot 引用。`provisioning` 是正常可恢复状态；永久失败使用稳定错误码和安全说明，不返回 Outbox Payload 或内部 RPC 错误。

## 5. Business→Agent RPC v1

Agent 拥有 IDL Source，目标路径：

```text
agent/api/thrift/session/v1/session.thrift
```

冻结方法：

```text
EnsureProjectSessionV1
QueryProjectSessionCommandV1
```

Ensure 请求至少包含：`schema_version`、`request_id`、`command_id`、`request_digest`、`project_id`、`owner_user_id`、`creation_source=quick_create`、`skill_snapshot_mode=empty`、可选 `initial_prompt`、`prompt_digest`、`requested_at`。

Agent 必须独立规范化请求并重算 Digest，不能只相信 Business 提供值。首次处理由 Agent 生成 Session/Message/Input ID；同 Command 同 Digest 返回冻结 Receipt，同 Command 异 Digest 返回 Conflict。

### 5.1 Ensure Canonical Digest

`request_digest` 使用以下固定字段顺序的 UTF-8 紧凑 JSON 计算 SHA-256，并编码为小写十六进制：

```json
{"schema_version":"ensure_project_session.v1","project_id":"<lowercase-uuidv7>","owner_user_id":"<lowercase-uuidv7>","creation_source":"quick_create","prompt_present":true,"prompt_digest":"<lowercase-sha256-or-empty>","skill_snapshot_mode":"empty"}
```

Business 与 Agent 必须各自实现并通过同一固定向量。Prompt 先校验 UTF-8 并执行 Unicode NFC，规范化后上限为 65,536 UTF-8 字节；全文均为 Unicode 空白时编码为 `prompt_present=false`、`prompt_digest=""`，非空正文保留首尾空白并对规范化字节计算 `prompt_digest`。`command_id`、`request_id`、`requested_at`、Trace 字段和密文元数据不进入业务语义摘要。

跨 Module 固定向量：`project_id=019f0000-0000-7000-8000-0000000000ab`、`owner_user_id=019f0000-0000-7000-8000-0000000000cd`、Prompt ` e\u0301 ` 时，`request_digest=35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4`。

Query 必须包含 `command_id + expected_request_digest`，返回 `not_found/completed/conflict` 和安全 Receipt。Dispatcher 的 Unknown Outcome 顺序固定为 Query → completed 则重放 → confirmed not_found 才以原 Command 重试。

## 6. Agent Session/Event v1

### 6.1 Input 与 Lease

Input 状态固定为：

```text
pending → claimed → running → resolved
                    ↘ retry_wait → claimed
claimed/running → dead
```

W0 首批只创建 `pending`，不宣称 Runner 已执行。Session Runtime Lease/Fence 是独立权威事实，不能只依赖 Input 行状态。

Agent Message 的受保护正文使用 W0 二进制 AEAD Envelope v1，固定布局如下：

```text
byte 0..3   magic = 0x44 0x52 0x41 0x45（ASCII `DRAE`）
byte 4      version = 1
byte 5      algorithm = 1（AES-256-GCM）
byte 6      nonce_length = 12
byte 7..18  nonce（12 bytes）
byte 19..   ciphertext || authentication_tag（tag 固定 16 bytes，ciphertext 至少 1 byte）
```

因此 v1 Envelope 最短为 36 bytes；`key_version` 独立保存且不能为空。Service、Repository 与数据库 CHECK 都必须拒绝裸正文、裸密文、未知版本/算法、错误 Nonce 长度或缺少 Key Version 的内容。结构 Builder/Validator 属于本批冻结实现；真实加解密与密钥适配器在 Transport 接线批次实现，解密端仍必须执行 AEAD authentication tag 校验，结构校验不能替代认证校验。

### 6.2 序号

- Message 使用每 Session 单调 `message_seq`；
- Input 使用每 Session 单调 `enqueue_seq`；
- Event 使用每 Session 单调 `seq`；
- Event Counter 保存 `last_seq/min_available_seq`；
- Event AppendOnce 唯一语义包含 `source_kind/source_id/event_type/projection_index`。

### 6.3 W0 Event

最小事件：`session.created`、`session.input.accepted`。`stream.ready`、`stream.reset` 是连接控制事件，不落 EventLog、不占 Seq，也不设置 SSE `id`。

## 7. Workspace 与 SSE HTTP v1

候选路径在实现 Transport 时按本节冻结：

```text
GET /api/v1/agent/sessions/{session_id}/workspace
GET /api/v1/agent/sessions/{session_id}/events?after_seq=<last-confirmed-seq>
```

Workspace Snapshot 必须返回 `event_high_watermark`。SSE 使用 `max(valid after_seq, valid Last-Event-ID)`，先查询 `seq > cursor ORDER BY seq`，成功补读后进入通知 + 周期 Poll。

Cursor 小于 `min_available_seq` 时发送无 `id` 的 `stream.reset`，包含 `snapshot_required/min_available_seq/latest_seq`，随后关闭连接。前端连接器收到 Reset 后停止当前自动重连，完成完整 Snapshot 后显式创建新连接。

## 8. 错误 Envelope

所有 HTTP 失败统一为：

```json
{
  "error": {
    "code": "STABLE_CODE",
    "message": "安全中文信息",
    "request_id": "uuidv7",
    "retryable": false,
    "details": {}
  }
}
```

错误中不得出现 SQL、堆栈、Cookie、Prompt、Outbox Payload、内部 RPC 地址或 Secret。

## 9. 首批实现范围

本轮允许并行实现：

- Business/Agent Migration、Entity、Repository、事务与幂等单元测试；
- 前端 Auth/QuickCreate Client、Auth 状态和 `stream.reset` 生命周期测试；
- 现有 Module 独立构建与 Migration 静态门禁。

以下进入下一批：

- Argon2id 登录 UseCase、Cookie/CSRF Handler 和 Local Smoke Seeder；
- Gateway/Business Auth Resolver 与内部身份断言；
- Agent Kitex IDL/Server、Business Dispatcher；
- Project/Session HTTP、SSE Tail 和页面接线；
- Runner/Turn、Graph Tool、Worker 和费用。

实现线不得因为本文件 Frozen 而把“下一批”能力描述成当前已实现。
