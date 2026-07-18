# 身份与项目功能现状

> 文档状态：Current Implementation / 只描述当前工作树
>
> 适用 Runtime：`business/cmd/business-service`、`agent/cmd/agent-service`
>
> 更新日期：2026-07-17

## 现状

当前代码已经形成一条可执行的身份与项目基础纵切：Business 提供邮箱密码登录、不透明 Cookie Session、当前会话查询和退出；登录用户可以快速创建 Project、分页读取自己的 Project、查询单个 Project 的初始化状态。Project 创建事务同时写入默认 Agent Session 的初始化 Binding 与加密 Outbox，后台 Dispatcher 再通过正式 Session RPC 创建或核对 Agent Session。

同一个快速创建路径支持两个严格变体：

- 未携带 `schema_version` 时按 W0 v1 创建，Session Skill Snapshot 固定为空集合；
- `schema_version=project_quick_create.v2` 时必须显式携带非 `null` 的 `enabled_skill_ids`，Business 在创建事务内冻结可用 Published Skill、权限证明和 Session Bootstrap v2 Outbox。

Project 首次创建不等待 Agent RPC，正常返回 `creation_status=provisioning`。Dispatcher 完成后，Project Bootstrap 或同义幂等重放可以读取到 `ready`、`session_id` 和可选的首 `input_id`。

## 边界

当前公开功能只包含登录、会话查询、退出、Project 快速创建、Project 列表和 Project Bootstrap。

以下能力没有公开实现：

- 用户注册、找回密码、修改密码、MFA 和第三方登录；
- Project 改名、归档、恢复、移入回收站和删除接口；
- 为已有 Project 新建第二个 Session；
- 为已有 Project 替换 Skill Binding；
- 面向管理员的用户、Session 或 Project 管理 API。

数据库和领域模型虽然定义了 `archived`、`trash`、`deleted` 等 Project 生命周期值，但当前 HTTP 没有迁移这些状态的写入口，不能把状态定义视为已完成的项目管理功能。

## 流程

### 登录与会话

1. 客户端提交邮箱和密码。
2. Business 规范化身份并执行 Redis 登录限流，再用 Argon2id 校验密码凭据。
3. 登录成功后，Business 创建服务端 Web Session，只持久化 Cookie Token 和 CSRF Token 的 SHA-256 摘要。
4. HTTP 响应写入 HttpOnly 会话 Cookie，并返回当前 Principal、CSRF Token 和会话到期时间。
5. 读请求由 Session 中间件解析可信 Principal；状态变更请求额外校验同一 Session 绑定的 `X-CSRF-Token`。
6. 退出会撤销服务端 Session 并清除 Cookie；缺少 Cookie、会话已不存在或重复退出保持幂等 `204`。

### 快速创建 Project

1. 客户端携带登录 Session、CSRF、规范 `Idempotency-Key` 和严格 JSON 请求。
2. Business 从可信 Principal 冻结 `owner_user_id`，不接受请求体覆盖用户、Project 或 Session 标识。
3. v1 在单事务写 Project、创建回执、Session Binding 和加密 Outbox；v2 还在同一事务冻结 Project Skill Binding、解析结果和加密 Skill Runtime Snapshot。
4. HTTP 首次提交返回 `201 + provisioning`；同键同义重放返回 `200`，同键异义返回冲突。
5. Business Dispatcher 从 PostgreSQL Outbox 领取命令，通过 Agent Session RPC 执行 Ensure；响应未知时先查询原命令结果，不创建第二个 Session。
6. Agent 原子写入 Session、可选首 Message/Input、Skill Snapshot、命令回执和基础事件；Business 收到匹配回执后把 Binding 标记为 `ready` 并清除 Outbox 敏感载荷。

## 接口与状态

### HTTP 接口

| 方法与路径 | 当前行为 | 认证要求 |
| --- | --- | --- |
| `GET /api/v1/auth/session` | 返回当前 Principal、CSRF Token 和会话到期时间 | 有效 Cookie Session |
| `POST /api/v1/auth/session` | 邮箱密码登录并创建 Web Session | 登录限流；无需已有 Session |
| `DELETE /api/v1/auth/session` | 幂等退出并清除 Cookie | 有 Session 时校验 CSRF |
| `POST /api/v1/projects:quick-create` | 创建 v1 或显式 v2 Project；首次不等待 Agent | Session + CSRF + `Idempotency-Key` |
| `GET /api/v1/projects` | 按 `updated_at DESC, id DESC` Keyset 分页读取当前用户的 `active/archived` Project | Session |
| `GET /api/v1/projects/:project_id/bootstrap` | 返回 Project 和默认 Session 初始化投影 | Session + Project Owner |

Quick Create v1 请求只允许 `initial_prompt`；v2 请求只允许 `schema_version`、`initial_prompt` 和非 `null` 的 `enabled_skill_ids`。两种请求均拒绝未知字段和 trailing JSON。

### 当前状态集合

| 对象 | 当前代码接受的状态 |
| --- | --- |
| Web Session | `active`、`revoked`、`expired` |
| Project 生命周期 | `active`、`archived`、`trash`、`deleted`；当前创建只写 `active` |
| Project 最近运行摘要 | `idle`、`queued`、`running`、`waiting_user`、`waiting_async`、`succeeded`、`partial_failed`、`failed`、`cancelled` |
| 首提示词初始化 | `absent`、`pending`、`accepted`、`failed` |
| Session Provisioning | `pending`、`reconciling`、`ready`、`blocked` |
| Project Session Outbox | `pending`、`processing`、`retry`、`delivered`、`dead` |
| Agent Session | `active`、`archived`；当前 Ensure 只创建 `active` |

## 数据 Owner

| 数据 | Owner | 当前访问方式 |
| --- | --- | --- |
| User、邮箱身份、密码凭据、Web Session、角色分配 | Business PostgreSQL | Business Repository；外部只经 Auth HTTP |
| Project、创建回执、默认 Session Binding、Session Outbox | Business PostgreSQL | Business HTTP 和内部 Dispatcher |
| Project Skill Binding、解析结果、权限摘要 | Business PostgreSQL | Quick Create v2 事务内生成 |
| Session、Message、Input、Session Skill Snapshot、Session 命令回执 | Agent PostgreSQL | Business 通过版本化 Session RPC；浏览器经同源 BFF 读取 |
| 登录限流 | Redis 非权威状态 | 只用于限流，不替代 Session PostgreSQL 真值 |
| 服务发现 | etcd 非业务权威 | Business 和 Agent 注册/解析 RPC Endpoint |

Business 不直写 Agent 表，Agent 不读取 Business 数据库，也不引用对方的 `internal` 包。

## 错误与安全

- Cookie 为服务端不透明随机值；数据库只保存摘要。CSRF Token 同样只保存摘要，并用常量时间比较校验。
- 写接口要求 Session 与 CSRF；Principal 只从认证 Context 取得，Body、Query 和自定义身份 Header 不能覆盖。
- 登录错误对外收敛，不能用响应区分邮箱不存在、密码错误或账户不可用。
- 登录限流由 Redis 执行；Redis 或 PostgreSQL 不可用时失败关闭，不伪装成匿名或登录失败。
- Project 不存在和不属于当前用户统一返回 `PROJECT_NOT_FOUND`，避免泄露资源存在性。
- Quick Create 使用 first-write-wins 幂等回执；同键异义返回 `IDEMPOTENCY_CONFLICT`。
- 首提示词和 Skill Runtime Snapshot 在 Business Outbox 中使用 AEAD 加密，Agent 确认后清除密文并保留清除时间。
- Business 到 Agent 的 HTTP 读取使用短期、绑定 method/target/scope/session/request/nonce 的内部身份断言；Session 创建使用版本化 RPC 和原命令查询恢复。
- 普通日志和错误 Envelope 不记录密码、Cookie、CSRF、首提示词、Skill Runtime 正文、DSN 或 SQL 原错。

## 测试

当前仓库包含以下验证入口：

- Business `auth`、`project`、`projectcreation`、`projectdispatch`、`projectdispatchv2`、HTTP Handler 和 PostgreSQL Repository 单元/集成测试；
- Agent `session`、Session RPC、Skill Snapshot、Workspace 投影和 PostgreSQL Repository 测试；
- `make w0-smoke`：身份、Quick Create、Session 初始化和基础数据库证据；
- `make w0-browser-smoke`：浏览器同源登录和 Project 基础纵切；
- `make w1-smoke` / `make w1-browser-smoke`：带 Skill Snapshot 的 Quick Create v2 纵切；
- `make test-local-smoke-seeders`：本地 Smoke Fixture 的确定性和边界测试。

这些命令是仓库提供的验收入口；某次提交是否通过仍以该提交实际执行结果为准。

## 生产差距

当前实现尚未覆盖完整账户生命周期、MFA、密码恢复、Session 运维治理、Project 生命周期写 API、多 Session Project 和已有 Project 的 Binding 更新。

本地配置使用静态注入的内容保护密钥；当前代码没有外部 KMS/HSM、自动轮换服务或跨区域密钥治理。Business 与 Agent 的本地联调可以使用明文 loopback 连接，不能据此宣称已经具备生产 TLS、服务网格身份或公网 Gateway 防护。角色分配通过受控运维路径维护，尚无完整管理员控制台。
