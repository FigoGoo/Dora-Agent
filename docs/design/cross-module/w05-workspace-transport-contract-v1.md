# W0.5 Workspace Transport 契约 v1

> 状态：Frozen / W0.5 实现基线
>
> 版本：`w05.workspace-transport.v1`
>
> 冻结日期：2026-07-14
>
> 上游契约：[W0 身份与工作台契约 v1](w0-identity-workspace-contract-v1.md)

## 1. 范围与非目标

本文件只细化 W0 已冻结但尚未落地的同源 BFF 身份断言、Agent Workspace Snapshot、EventLog SSE Tail 和前端 Snapshot→SSE 恢复规则。与 W0 上游契约冲突时以上游契约为准。

W0.5 允许投影当前已存在的 Session、首 Message、首 Input、`session.created` 与 `session.input.accepted`；不得伪造 A2UI、Turn、Run、Graph Tool、Storyboard、Asset、模型或费用事实。Business 现有 Project Bootstrap 是本批 Business 权威读模型；Storyboard/Asset 专项 Snapshot 在对应业务表和事件设计冻结后以前向版本增加。

## 2. 冻结决定

| 决定 | 结论 |
| --- | --- |
| W05-DEC-001 | 浏览器正式 `/api/v1/agent/**` 始终进入 Business 同源 BFF，不由 Vite 或浏览器直连 Agent |
| W05-DEC-002 | BFF 只允许代理两个固定 GET 路径；Cookie、Authorization、CSRF 和浏览器传入的内部断言 Header 不转发 |
| W05-DEC-003 | Business 先按 Cookie 解析权威 Web Session，再按 owned ready Binding 取得 Project/Agent Session，最后签发一次性短期断言 |
| W05-DEC-004 | Agent 同时校验断言、Nonce、User/Project/Session 三重绑定；现有 Session RPC 服务 HMAC 不复用于用户级 HTTP 断言 |
| W05-DEC-005 | Snapshot 在 PostgreSQL `READ ONLY, REPEATABLE READ` 事务中冻结 Session 投影和 Event 高水位；任一 Message 解密失败则整个响应失败关闭 |
| W05-DEC-006 | SSE `id` 与 JSON `seq` 相同；只有持久 EventLog 行推进 Cursor，`stream.ready/reset` 和 Heartbeat 不占 Seq |
| W05-DEC-007 | PostgreSQL 是 Tail 真源；通知只降延迟，周期 Poll 必须能从最后确认 Cursor 恢复 |
| W05-DEC-008 | EventSource 传输失败后用同一 Workspace Snapshot 兼做身份与游标 Probe，不新增第三个 W0.5 HTTP 接口 |
| W05-DEC-009 | 前端正式状态机与历史 AIGC Demo 隔离，不读取 LocalStorage Session、不自动创建 Demo Session |

## 3. 同源 BFF 与身份断言

### 3.1 公开路径与代理边界

浏览器只访问 Business 同源入口：

```text
GET /api/v1/agent/sessions/{session_id}/workspace
GET /api/v1/agent/sessions/{session_id}/events?after_seq=<last-confirmed-seq>
```

BFF 固定执行：

1. 删除所有浏览器传入的 `X-Dora-Identity-*`、`Authorization` 和其他内部认证 Header；
2. 用 HttpOnly Cookie 调用 Business Auth Resolver；
3. 用 `principal_user_id + agent_session_id` 查询 Business Project/Session Binding，只有 owner、Project 可读、Binding=`ready` 且 Session 完全匹配才继续；
4. 只转发 `GET`、规范路径、规范化后的 `after_seq` 和必要的 `Accept`；不得转发 Cookie、CSRF、原始 Query、请求体或任意用户 Header；
5. 生成新 Request ID、Nonce 和短期断言；内部 HTTP Client 禁止自动业务重试；
6. JSON 响应有界复制；SSE 禁止代理缓冲并传播取消、Flush 与逐帧 Deadline。

不存在、非 owner、未 ready 或 Session 不匹配统一返回公共 `404 SESSION_NOT_FOUND`，避免枚举 Project/Session。

### 3.2 Wire Protocol

断言使用三个 Header：

```text
X-Dora-Identity-Assertion: <base64url-no-padding(canonical UTF-8)>
X-Dora-Identity-Key-Version: <kid>
X-Dora-Identity-Signature: <lowercase-hex HMAC-SHA256>
```

Canonical 固定 16 行，行间单个 LF，末尾无 LF：

```text
agent_http_identity_assertion.v1
dora-business-service
dora.agent.http.v1
<kid>
<request_id UUIDv7 lowercase>
GET
<canonical_target>
<principal_user_id UUIDv7 lowercase>
<web_session_id UUIDv7 lowercase>
<web_session_version canonical int64>
<project_id UUIDv7 lowercase>
<agent_session_id UUIDv7 lowercase>
<agent.session.workspace.read|agent.session.events.read>
<iat unix-ms>
<exp unix-ms>
<nonce base64url-no-padding 16 random bytes>
```

Workspace `canonical_target` 不含 Query。Events 由 BFF 先严格解析 `after_seq` 与 `Last-Event-ID`，取两者合法值的较大者，只向 Agent 转发：

```text
/api/v1/agent/sessions/<agent_session_id>/events?after_seq=<canonical-decimal>
```

签名为：

```text
lowercase_hex(HMAC-SHA256(key[kid], canonical_bytes))
```

Header `kid` 必须与 Canonical 第 4 行相同。断言最大解码长度 2048 bytes；全部枚举、UUID、整数、时间、Path、Scope 和 Base64URL 必须是唯一规范编码。

### 3.3 TTL、重放与轮换

- 默认签发 TTL 为 30 秒，协议硬上限 60 秒，最大时钟偏差 5 秒；
- Agent 校验 `exp > now`、`0 < exp-iat <= 60s`、`iat <= now+5s`，并把请求 Context Deadline 收紧到 `exp`；
- SSE 最长连接时间取配置值与断言剩余 TTL 的较小者，到期直接关闭，由浏览器经 BFF 获取新断言重连；
- 静态校验与 HMAC 通过后，Agent 用 Redis 一次性占有 Nonce：

```text
SET dora:agent:http-identity:v1:<kid>:<sha256(nonce)> 1 NX PX <exp+skew-now>
```

Nonce 重放统一认证失败；Redis 不可用失败关闭为 `503 IDENTITY_ASSERTION_UNAVAILABLE`，不得用单机内存替代多实例防重放。

HTTP 断言密钥独立于 Cookie CSRF Secret 和 Session RPC HMAC。密钥固定 32 bytes，`kid` 不可复用。轮换顺序为：Agent 先接受 old+new → Business 切 active kid → 等待 `maxTTL+skew` → 删除 old。Verifier 只按 `kid` 选键，不遍历试签。

### 3.4 固定向量

测试 Key 为十六进制 `000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f`，Canonical 为：

```text
agent_http_identity_assertion.v1
dora-business-service
dora.agent.http.v1
test-2026-07-a
019f0000-0000-7000-8000-000000000001
GET
/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42
019f0000-0000-7000-8000-000000000002
019f0000-0000-7000-8000-000000000003
7
019f0000-0000-7000-8000-000000000004
019f0000-0000-7000-8000-000000000005
agent.session.events.read
1784011500123
1784011530123
AAECAwQFBgcICQoLDA0ODw
```

期望签名：

```text
a7bd082fd06e94d0e09eff76608f240dde6390692c714c9e23fc4983c736c374
```

## 4. Agent Workspace Snapshot

成功 `200`：

```json
{
  "schema_version": "session.workspace.v1",
  "request_id": "uuidv7",
  "session": {
    "id": "uuidv7",
    "project_id": "uuidv7",
    "status": "active",
    "version": 1,
    "created_at": "RFC3339Nano",
    "updated_at": "RFC3339Nano"
  },
  "messages": [
    {
      "id": "uuidv7",
      "message_seq": 1,
      "role": "user",
      "content": "authorized plaintext",
      "created_at": "RFC3339Nano"
    }
  ],
  "inputs": [
    {
      "id": "uuidv7",
      "message_id": "uuidv7",
      "source_type": "user_message",
      "status": "pending",
      "enqueue_seq": 1,
      "available_at": "RFC3339Nano",
      "created_at": "RFC3339Nano",
      "updated_at": "RFC3339Nano"
    }
  ],
  "event_high_watermark": 2,
  "min_available_seq": 1
}
```

约束：

- 空 Prompt 必须返回 `messages=[]`、`inputs=[]`，不得伪造欢迎消息；
- 不返回 `user_id`、Digest、密文、Key Version、Source、Lease、Fence、Attempts、Receipt 或 Skill 内部引用；
- 所有数组不得为 `null`，序号不得超过 JavaScript safe integer；
- `event_high_watermark` 与响应投影来自同一只读一致性事务；
- active/archived Session 均允许 owner 读取，写能力另行校验 active；
- Session 不存在与 owner 不匹配统一 `404 SESSION_NOT_FOUND`。

### 4.1 Repository 一致性

`LoadSnapshot` 使用短 `READ ONLY, REPEATABLE READ` 事务，固定三次集合查询：

1. JOIN `session + session_skill_snapshot + session_event_counter`，条件同时包含 `session.id` 与 `session.user_id`；
2. Message 按 `message_seq ASC, id ASC` 有界批量读取；
3. Input 按 `enqueue_seq ASC, id ASC` 有界批量读取。

不得按 Message/Input 循环查询。每类读取使用 `limit+1` 检测超界，禁止静默截断完整 Snapshot；当前 W0 只会存在零或一条。

### 4.2 内容解密

只读 `ContentDecryptor.Open` 固定执行：

1. 校验 DRAE v1 magic/version/algorithm/Nonce/最短长度；
2. 按 `content_key_version` 选择 active 或 previous read key；
3. 拆出 12-byte Nonce 与 `ciphertext || tag`；
4. AES-GCM `Open(..., aad=nil)` 验证认证标签；
5. 校验明文 UTF-8、最大 65,536 bytes；
6. 重算 SHA-256 并与持久化 `content_digest` 常量时间比较。

任一步失败，整个 Snapshot 返回 `503 WORKSPACE_CONTENT_UNAVAILABLE`，不得跳过消息或返回占位正文。现有 `AGENT_CONTENT_KEY_*` 是 active key；W0.5 增加可选 previous key/version，只有二者同时提供才启用旧版本只读解密。

## 5. EventLog SSE

### 5.1 持久事件 Envelope

```text
id: 2
event: session.input.accepted
data: {"schema_version":"workspace.event.v1","payload_schema_version":"session.event.v1","event_id":"uuidv7","session_id":"uuidv7","project_id":"uuidv7","seq":2,"event":"session.input.accepted","occurred_at":"RFC3339Nano","aggregate_type":"session_input","aggregate_id":"uuidv7","aggregate_version":1,"payload":{"session_id":"uuidv7","input_id":"uuidv7","message_id":"uuidv7","enqueue_seq":1,"status":"pending"}}
```

- SSE `id`、JSON `seq` 和数据库 `seq` 完全相同；
- SSE `event`、JSON `event` 和数据库 `event_type` 完全相同；
- Mapper 只接受 `session.created/session.input.accepted` 与 `session.event.v1`，解码为强类型 Payload 后交叉校验 Session/Aggregate；
- 不返回 `source_kind/source_id/projection_index`，未知类型、版本、缺口或不一致触发 Reset 并记录高优先级告警。

### 5.2 Cursor

- `after_seq` 表示最后已确认 Seq，查询严格 `seq > cursor`；
- `after_seq` 与 `Last-Event-ID` 只接受唯一的规范非负十进制整数；重复参数、负数、前导零、溢出或任一非法值返回 `400 INVALID_CURSOR`；
- 两者都存在时取较大值；大于 `last_seq` 返回 `400 INVALID_CURSOR`；
- `cursor < min_available_seq` 时发送无 `id` 的 `stream.reset` 后关闭；
- 每批固定查询 `WHERE session_id=? AND seq>? ORDER BY seq ASC LIMIT ?`，并校验结果严格连续。

### 5.3 控制帧

成功补读后发送一次无 `id` 的 Ready：

```text
event: stream.ready
data: {"schema_version":"workspace.stream-control.v1","event":"stream.ready","session_id":"uuidv7","cursor":2,"min_available_seq":1,"latest_seq":2}
```

Cursor 过期或投影不可安全恢复时发送无 `id` 的 Reset 并关闭：

```text
event: stream.reset
data: {"schema_version":"workspace.stream-control.v1","event":"stream.reset","session_id":"uuidv7","reason":"cursor_expired","snapshot_required":true,"min_available_seq":21,"latest_seq":42}
```

Reset reason 白名单为 `cursor_expired/event_gap/projection_invalid`。Heartbeat 只使用 SSE Comment：

```text
: heartbeat 1784000000000
```

### 5.4 Tail 与资源边界

连接流程固定为：建立非权威通知订阅 → PostgreSQL 有界补读 → 每条 write+flush 成功后推进连接内 Cursor → Ready → 通知/周期 Poll/Heartbeat/连接期限循环。通知丢失必须由 Poll 补偿；单连接串行读写，不建立无界 Event Channel。

达到并发预算时在写 Header 前返回 `429 STREAM_RATE_LIMITED`。SSE Handler 必须覆盖普通 Server 的 30 秒 WriteTimeout，改用逐帧 Write Deadline；慢写、数据库瞬时失败、客户端取消或断言到期直接关闭，客户端从最后确认 Cursor 恢复。

建议默认配置：Batch=100、Poll=1s、Heartbeat=10s、MaxConnection=25s、FrameWriteTimeout=5s、MaxEvent=64KiB、单实例总连接=1000、每 User=5、每 Session=2。全部参数必须有正数和上限校验，Heartbeat 小于代理 Idle，Frame Timeout 小于 Heartbeat。

## 6. 前端正式状态机

顶层状态固定为：

| 状态 | 行为 |
| --- | --- |
| `loading` | 等待 Auth、Project Bootstrap 或 Agent Snapshot；新路由立即清空旧项目数据 |
| `ready` | 已验证 Project Bootstrap + Agent Snapshot；内部流状态为 `connecting/live/reconnecting` |
| `reset` | 保留同项目只读画面，禁用写操作，关闭旧流并完整回源 |
| `offline` | 已有 Snapshot 可只读保留；冷启动不得显示旧内容，提供显式重试 |
| `unauthorized` | 清空 Workspace、关闭流并进入登录流程 |
| `not_found` | 清空 Workspace，不自动创建 Session，显示统一不存在/不可访问 |

冷启动顺序：Auth authenticated → Business Project Bootstrap ready → Agent Workspace Snapshot → 校验 Project/Session → 原子提交 → 以 `event_high_watermark` 建 SSE。

每次路由变化、Reset、退出或 401 都执行：Generation+1、Abort 当前请求、关闭 SSE、清理 Timer；所有回调同时校验 `generation + project_id + session_id`。

前端 Cursor 只能在 DTO、事件强类型、序号连续性和 Reducer 全部成功后推进：

- `seq == cursor+1`：应用并推进；
- `seq <= cursor`：只有缓存的 `seq→event_id` 相同才丢弃；
- 同 Seq 异 Event ID 或 `seq > cursor+1`：进入 Reset；
- `stream.ready/reset` 与 Heartbeat 不推进 Cursor。

EventSource 传输失败后先调用 Agent Workspace Snapshot：401 进入 unauthorized，404 进入 not_found，`cursor < min_available_seq` 进入 reset，其余在有界退避内从原 Cursor 重连；耗尽预算进入 offline。不得在永久错误上无限重连。

正式页面稳定暴露 `data-workspace-state`、`data-stream-state`、`data-project-id`、`data-session-id` 和 `aria-busy`，供 Playwright 只断言用户可见状态与连接生命周期。

## 7. 错误映射

| HTTP | Code | 公开语义 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | Session ID 非法 |
| 400 | `INVALID_CURSOR` | Cursor 非法、溢出或超前 |
| 401 | `UNAUTHENTICATED` | Business Cookie 会话失效；前端清空 Auth/Workspace |
| 404 | `SESSION_NOT_FOUND` | Session 不存在、不属于用户或 Binding 不匹配 |
| 429 | `STREAM_RATE_LIMITED` | SSE 并发预算耗尽，有界等待 |
| 503 | `DEPENDENCY_UNAVAILABLE` | Business→Agent 内部身份或 Transport 失败；不得清除浏览器登录态 |
| 503 | `PERSISTENCE_UNAVAILABLE` | Agent PostgreSQL 暂不可用 |
| 503 | `WORKSPACE_CONTENT_UNAVAILABLE` | Key、AEAD、Digest 或 UTF-8 校验失败，不泄漏具体原因 |

Agent 内部断言无效使用 `401 INTERNAL_IDENTITY_INVALID`；BFF 将其映射为公共 `503 DEPENDENCY_UNAVAILABLE`。只有 Business Auth Resolver 确认浏览器会话无效时才返回公共 401。

## 8. 配置

Business 新增并启动校验：

```text
BUSINESS_AGENT_HTTP_BASE_URL
BUSINESS_AGENT_HTTP_REQUEST_TIMEOUT
BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION
BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64
BUSINESS_AGENT_HTTP_ASSERTION_TTL
```

Agent 新增并启动校验：

```text
AGENT_HTTP_ASSERTION_ACTIVE_KEY_VERSION
AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64
AGENT_HTTP_ASSERTION_PREVIOUS_KEY_VERSION
AGENT_HTTP_ASSERTION_PREVIOUS_SECRET_BASE64
AGENT_HTTP_ASSERTION_MAX_CLOCK_SKEW
AGENT_HTTP_ASSERTION_REPLAY_TIMEOUT
AGENT_CONTENT_PREVIOUS_KEY_VERSION
AGENT_CONTENT_PREVIOUS_KEY_BASE64
AGENT_WORKSPACE_MAX_MESSAGES
AGENT_WORKSPACE_MAX_INPUTS
AGENT_SSE_BATCH_SIZE
AGENT_SSE_POLL_INTERVAL
AGENT_SSE_HEARTBEAT_INTERVAL
AGENT_SSE_MAX_CONNECTION_DURATION
AGENT_SSE_FRAME_WRITE_TIMEOUT
AGENT_SSE_MAX_EVENT_BYTES
AGENT_SSE_MAX_CONNECTIONS
AGENT_SSE_MAX_CONNECTIONS_PER_USER
AGENT_SSE_MAX_CONNECTIONS_PER_SESSION
```

`DORA_ENV=production` 时 Agent Base URL 必须为 HTTPS。Secret 只来自环境或 Secret Manager，不进入配置输出、日志、Trace、HTTP/RPC DTO 或 Evidence。

## 9. 实现批次与门禁

文件互斥的并行实现线：

1. Agent：内容 Keyring/Open、Workspace Repository/Service、身份 Verifier/Replay Store、HTTP/SSE；
2. Business：Session Access Query、身份 Signer、白名单 BFF 与代理取消/Flush；
3. Frontend：Workspace Contract/Reducer/Hook、严格 SSE Cursor、页面状态与 Playwright；
4. Smoke：扩展 `w0-browser-smoke` 覆盖 Snapshot、刷新、重连、Reset、越权与退出。

必须通过：

- 三 Module 各自在 `GOWORK=off` 下的 shuffle/race/vet/build/mod verify/tidy；
- 真实 PostgreSQL Snapshot 高水位、固定查询次数、解密篡改与 Event Batch 契约测试；
- 身份固定向量、跨路径/用户/项目重放、100 并发同 Nonce 仅一次成功、Redis 故障和 old/new 轮换；
- SSE `id==seq`、补读、通知丢失、Heartbeat、Reset、断言到期、慢消费者与取消；
- 前端刷新、断线、Cursor 过期、401/404、旧连接隔离和退出；
- Evidence 不包含 Cookie、CSRF、断言、签名、密钥、Prompt 密文或完整用户敏感正文。

W0.5 全绿只关闭 `SMK-004A` 的 Session/Event 恢复底座；A2UI/Storyboard/Asset/Run 的完整 `SMK-004B` 必须等待对应 Owner 契约和实现，不得由本批误报完成。
