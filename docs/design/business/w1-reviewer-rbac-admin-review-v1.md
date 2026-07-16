# W1-C2 Reviewer RBAC 与 Skill 管理审核 v1

> 状态：Frozen / Approved
>
> 设计日期：2026-07-14
>
> 冻结日期：2026-07-14
>
> 覆盖范围：生产 Reviewer 身份解析、待审核队列、冻结详情、批准发布，以及真实双身份浏览器冒烟
>
> 依赖：[W1 Skill 与 Tool 入口基础契约 v1](../cross-module/w1-skill-tool-entry-contract-v1.md)

## 1. 结论与完成定义

W1-C2 只实现一个可上线验证的最小管理纵切：通过 Business-owned 运维命令写入的 `skill_reviewer` 角色在每次登录和 Session Resolve 时动态解析为 `skill.review`；持有该 capability 的 Reviewer 可以读取待审核队列、查看提交时冻结的完整 Skill 定义，并用受 CSRF、ETag、幂等和事务内二次授权保护的命令批准发布。

完成必须同时满足：

1. 普通用户、企业用户类型、只有角色名但没有权威 active assignment 的用户都不能获得 `skill.review`；
2. 同一 Cookie 在角色撤销后的下一次请求立即失去 capability；
3. 审核详情只读取 `skill_review_submission.content_revision_id` 指向的不可变修订，不能用 Owner 当前草稿代替；
4. 批准事务在写回执、快照、指针、审核终态和审计前依次锁定并验证 active Reviewer account 与 assignment；
5. 创建者提交、Reviewer 在管理端批准、创建者重新登录选择已发布 Skill 并执行 QuickCreate v2 的浏览器链路不使用 HTTP mock 或 capability 注入。

本设计不表示完整 `ADM-RBAC-001`、`ADM-SKILL-001` 或 `ADM-SKILL-002` 已通过。

## 2. 明确排除

本批不实现：

- 通用角色/权限目录、角色管理 HTTP、首个生产管理员 Bootstrap 或数据范围授权；本批只有窄化的离线角色运维命令；
- 驳回、撤回、暂停、恢复、下架、治理回滚或用户可见版本历史；
- `skill.govern`、Tool 安全检查、Tool 治理、计费、收益、导出或高敏明文查看；
- 公开 Skill 市场；
- 以本地 Seeder 证明生产赋权治理已经完成。

这些能力必须使用独立前向设计和 Migration。`skill.review` 不得隐含 `skill.govern`。

## 3. 身份、角色与 capability

### 3.1 固定映射

W1-C2 只批准以下闭集：

| Role | Capability |
| --- | --- |
| `skill_reviewer` | `skill.review` |

映射由 Business `authorization` 应用包冻结。数据库只保存角色分配，不保存可被运行时任意扩展的 capability 字符串。新增角色必须同时修改代码闭集、前向 Migration 约束、测试和设计，不能通过环境变量、Header、Body、`user_type=enterprise` 或前端状态临时赋权。

### 3.2 持久化模型

只新增前向 Migration `20260714000600_create_reviewer_rbac`，不得修改已发布 Migration。新增 `business.user_role_assignment`：

| 字段 | 约束 |
| --- | --- |
| `id` | 应用生成 UUIDv7，主键 |
| `user_id` | 被赋权用户 UUID，逻辑关联，无物理外键 |
| `role_key` | W1-C2 固定为 `skill_reviewer` |
| `status` | `active` 或 `revoked` |
| `version` | 从 1 开始，用于撤权 CAS |
| `assigned_by_user_id`、`assignment_reason_code`、`assigned_at` | 必填赋权审计字段 |
| `approval_reference` | 外部部署审批/工单稳定引用，Grant 必填且不得包含 Secret |
| `revoked_by_user_id`、`revoke_reason_code`、`revocation_approval_reference`、`revoked_at` | 仅 revoked 状态必填 |
| `updated_at` | UTC 最近状态时间 |

数据库必须有 `(user_id, role_key) WHERE status='active'` partial unique index 和 `(user_id, status, role_key, id)` Resolver 索引；CHECK 固定 role、version、`assigned_at <= updated_at` 以及 `revoked_at >= assigned_at`。active 时全部 revoke 字段必须为空，revoked 时 actor、reason、approval reference、time 必须同时非空。Trigger 禁止 DELETE，并只允许 `active(version=n) -> revoked(version=n+1)`，assigned 字段和 revoked 历史都不可改回。撤权保留旧行，重新授予创建新行。表和字段必须有中文 COMMENT，禁止物理外键、级联和物理删除。该表和其全部写入由 Business Module 拥有，禁止人工 SQL、Runtime HTTP 或其他 Module 直接修改。

### 3.3 动态解析与撤权

`authorization.Resolver.Resolve(ctx, userID)` 使用一次固定集合查询，以 active `user_account` 为锚读取 active assignments，稳定排序、去重后返回非 nil `roles[]` 和 `capabilities[]`：

- 无角色是正常结果 `[]/[]`；
- 账户不存在、非 active、未知角色、畸形行或数据库错误失败关闭；
- 0、1 条 active 以及 100 条含历史 revoked assignment 均为一次 SQL，不允许 N+1；
- 不写入 `auth_web_session`、JWT、Redis、浏览器存储或进程缓存。

Auth Service 在密码验证成功且创建 Session 前解析一次，使 Login Response 立即返回角色；每次 Cookie Session Resolve 在账户、期限、CSRF 绑定和 Touch CAS 校验后再次解析。Touch CAS 冲突重读时必须先重新验证 Session，再执行 authorization resolve，任何失败都不能返回旧 Principal。

登录记录中的账户失效沿用统一凭据失败语义；既有 Session 对应账户不存在或非 active 返回 `401 UNAUTHENTICATED`。只有数据库不可用、未知角色或畸形 assignment 才返回 `503 AUTH_UNAVAILABLE`，不能降级为空 capability。

### 3.4 权威 Grant/Revoke Writer

新增正常生产构建包含、但不注册 HTTP 的 `business/cmd/business-role-admin`。它只调用 Authorization Service/Repository，使用专用 `DORA_ROLE_ADMIN_POSTGRES_DSN`，接受明确的 `grant|revoke`、target user UUIDv7、actor user UUIDv7、固定 role、稳定 reason 和外部 approval reference；revoke 还必须携带 assignment UUIDv7 和 expected version。凭据不能通过命令行参数传入，输出只包含 action、assignment ID、target user、role、status、version、approval reference 和 request ID，不包含 DSN、邮箱或凭据。

生产信任根是部署平台对专用 DSN Secret 与审批引用的双重控制，不是数据库中预先存在的管理员角色。actor 必须是由正常身份流程创建的独立 active 操作人账户，只承担可追责身份，不能等于 target；首个 Grant 也遵循该规则，禁止 self-grant、system 假用户或手工 SQL。生产执行数据库身份拥有 `user_account` 的 SELECT 和仅用于 PostgreSQL `SELECT ... FOR UPDATE` 行锁所必需的 UPDATE 权限，以及 `user_role_assignment` 的 SELECT/INSERT/UPDATE 权限；CLI 不发出任何 `user_account` UPDATE，专用 DSN 只能由受审计部署任务持有。该身份不得拥有 DELETE、Migration 或其他业务表写权限。权限由部署环境准备，不由 Runtime 自动创建。CLI 在一个固定事务中：

1. 按 UUIDv7 稳定顺序 `FOR UPDATE` 锁定 active actor 和 target 账户，二者不能相同；
2. 校验 role 闭集和 reason；
3. Grant 同义语义精确覆盖 target、role、actor、reason、approval reference；active unique 命中同义则重放，不同义则 conflict；首次授予用应用生成 UUIDv7 插入，已 revoke 后重新授予必须使用新 ID；
4. Revoke 使用 `WHERE id/user_id/role_key/status='active'/version=?` CAS，写入 actor、reason、revocation approval reference、UTC 时间并递增 version；若记录已 revoked，只有 target/role/assignment、actor、reason 和 revocation approval reference 与首次完全一致时幂等重放，否则 conflict；
5. RowsAffected 或既有语义不匹配时失败关闭。

应用启动 Readiness 需要用固定 COUNT 查询检测 assignment 的 target、actor orphan 和未知角色；非零即 not ready。所有生产、local Seeder 和测试 writer 必须复用该 Service/Repository 边界。

## 4. Reviewer HTTP v1

所有路由同源、`Cache-Control: no-store`，Principal 只能来自私有 Auth Context。前端 capability 门禁只用于体验，服务端 Handler 和 Skill Service 都必须再次校验 `skill.review`。

| 方法 | 路径 | 认证与语义 |
| --- | --- | --- |
| `GET` | `/api/v1/admin/skill-reviews?status=reviewing&cursor=...` | Session + `skill.review`；只接受一个 status 和可选一个 cursor |
| `GET` | `/api/v1/admin/skill-reviews/:review_id` | Session + `skill.review`；读取提交冻结内容 |
| `POST` | `/api/v1/admin/skill-reviews/:review_id/decisions` | Session + `skill.review` + CSRF + `Idempotency-Key` + `If-Match`；严格 `{ "decision": "approved" }` |

非法 UUID、重复或未知 Query、未知 JSON 字段、尾随 JSON、缺失 Header 都必须在进入写事务前失败。Actor、角色、capability、审核状态、digest、definition 和发布时间不能从 Body 或自定义 Header 覆盖。

### 4.1 队列

`status=reviewing` 必须精确出现一次，cursor 可省略或精确出现一次。固定页大小 20，排序为最早提交优先 `(submitted_at ASC, review_id ASC)`。独立 opaque cursor `skill_review_queue_cursor.v1` 只包含 `status=reviewing`、`submitted_at_unix_nano` 和 `review_id`，cursor status 与 Query 不一致时失败。Repository 使用现有 `(status, submitted_at, id)` 索引和 `LIMIT 21`，一次 SQL 返回：

```json
{
  "items": [{
    "review_id": "uuidv7",
    "skill_id": "uuidv7",
    "name": "...",
    "summary": "...",
    "category": "...",
    "status": "reviewing",
    "submitted_at": "RFC3339Nano",
    "allowed_actions": ["approve_and_publish"]
  }],
  "next_cursor": null,
  "request_id": "uuidv7"
}
```

列表的 name、summary 和 category 必须来自 Review 冻结 content revision，而不是 current draft。列表不得返回完整 Definition、Owner 邮箱、内部 revision、digest、发布序号或审核策略。

### 4.2 冻结详情

详情一次集合查询 JOIN Review、Skill、被提交 Content Revision，并 LEFT JOIN current Published Snapshot。Mapper 必须分别重新解析 submitted/current Canonical Definition、重算 digest、核对 schema 和逻辑关联；存储 schema、Canonical JSON 或 digest 破坏统一告警并返回 `503 SKILL_PERSISTENCE_UNAVAILABLE`。当前发布只用于读取时对照，不提供字段 diff、历史列表、版本切换或 `ADM-SKILL-002` 完成声明。

```json
{
  "review": {
    "review_id": "uuidv7",
    "skill_id": "uuidv7",
    "owner_user_id": "uuidv7",
    "status": "reviewing",
    "submitted_at": "RFC3339Nano",
    "updated_at": "RFC3339Nano",
    "definition": { "schema_version": "skill_definition.v1" },
    "current_published": null,
    "comparison": {
      "has_current_published": false,
      "same_content": false
    },
    "review_etag": "\"opaque\"",
    "allowed_actions": ["approve_and_publish"]
  },
  "request_id": "uuidv7"
}
```

`current_published` 必须存在，只能是 `null` 或 `{published_snapshot_id,published_at,definition}`；`comparison.has_current_published` 必须等于非空性，`same_content` 只在两份权威 digest 相同时为 true，无当前发布时固定 false。`review_etag` 使用 `skill_review_etag.v1` 域分离以及 Review ID、status、version、冻结内容摘要计算 SHA-256/Base64URL，编码为单个 strong quoted ETag。Detail 的 `ETag` 响应头必须与 Body `review_etag` 完全相同。POST 只接受一个完全匹配的 strong tag，拒绝 `*`、`W/`、逗号列表、重复 Header 和非规范空白；Repository 锁定 Review 后重算并比较。客户端不能解析 ETag。Reviewer 可以读取合法 ID 的 `reviewing/approved/rejected/withdrawn` 详情；只有 reviewing 返回 `["approve_and_publish"]`，所有终态返回 `[]`，不存在才 404。批准事务仍需重新锁定和重验。

Skill Domain 新增统一的 Canonical Definition 总量上限 1 MiB，不低于旧 Binary 可接受的最大请求体，因此不会把 006 前合法不可变 revision 变成不可审核事实；Create/Update 在 normalize/canonicalize 阶段超过即返回 `400 SKILL_INVALID_DEFINITION`，Submit/Detail/Approve 使用同一常量校验既有记录。Detail 使用与 Canonical 相同的 `json.Encoder.SetEscapeHTML(false)` 有界编码器，在写响应 Header 前完成编码并断言不超过 3 MiB；该上限覆盖 submitted/current 两份最大定义与固定 Envelope，不得截断或回退到 Gin 默认 HTML 转义。测试必须包含大量 `<>&` 的最大合法 Definition，并证明可 create→submit→detail→approve；超限内容在 Owner 写入阶段失败。

### 4.3 批准决定

`POST .../decisions` 只接受 `approved`。首次成功和同义重放统一返回 `200`，避免前端根据状态码恢复不同模型：

```json
{
  "review": {
    "review_id": "uuidv7",
    "skill_id": "uuidv7",
    "status": "approved",
    "published_snapshot_id": "uuidv7",
    "decided_at": "RFC3339Nano",
    "allowed_actions": []
  },
  "request_id": "uuidv7"
}
```

决定回执作用域精确为 `(reviewer_user_id, approve_and_publish, review_id, key_digest)`；不同 Reviewer 使用相同原始 Key 不属于重放。Semantic Digest 使用版本化 schema，精确覆盖 review ID、`approved` 和原样 strong If-Match。重放发生在 active account/assignment 锁之后、当前 Review 状态和 ETag 重验之前，返回首次冻结的 review/skill/status/decided_at；HTTP request ID 始终是本次请求的新值。首次成功只产生一条 Audit，重放不追加 Audit。

同一内存生命周期内，请求超时、网络中断、HTTP 408/425/429/5xx、成功状态但响应丢失，或 2xx Body 严格解析失败，都属于 unknown outcome，必须保留原 Key 与原 ETag，并只允许用户显式原意图重试。`409 SKILL_REVIEW_CONFLICT` 表示旧 ETag 已不可重试：保留页面上下文、刷新权威详情并废弃旧命令；`409 IDEMPOTENCY_CONFLICT` 禁止自动换 Key，显示不可重试冲突。

命令账本是内存态而非权威持久化。hard refresh 后先 GET Detail：终态则清理本 Review 的旧意图并收敛；仍为 reviewing 时允许用户明确创建新的决定意图和新 Key，Review CAS 保证最多一个发布事实。不得把普通 retry、自动刷新或 malformed 2xx 偷换成新 Key。

## 5. 发布事务与撤权线性化

批准发布取消当前事务外 `FindReviewByID` 预读，使用专用 Reviewer Decision Result/Receipt mapper，不再复用会随 Owner 后续草稿变化的 `OwnerState` 或 `findFrozenSkillReceiptState`。事务按以下顺序执行，事务中不得调用外部服务：

1. 先用固定查询 `FOR UPDATE` 锁定并验证 Reviewer active `user_account`，再用第二条固定查询 `FOR UPDATE` 锁定 active `skill_reviewer` assignment；所有 Approval、Revoke 和账户禁用都遵循 account → assignment 的统一锁序，不依赖数据库执行计划；不存在立即返回 capability required，且不留下回执；
2. 按完整 actor/command/review/key 作用域查询既有回执；存在则核对 Semantic Digest 并直接读取专用冻结决定结果，不重验当前终态或 ETag；撤权后的同键 replay 因步骤 1 已先拒绝；
3. 不存在回执时锁定 Review 与 Skill；同一 Reviewer 的 assignment 行锁会串行化同 scope 请求，不同 Reviewer 由 Review 行锁与状态 CAS 收敛；
4. 重算 strong ETag 并与 If-Match 比较，校验 `reviewing`、治理 `active`、Review version 和内容引用；
5. 解析 Canonical Definition、重算 digest、核对 Review/Revision digest 和本批空 Tool 引用；
6. 此时拥有完整非空结果引用，插入决定回执、不可变 Published Snapshot，CAS 切换当前发布指针和 Review 终态；
7. 追加一条 Governance Audit 并冻结专用决定响应。

角色撤销使用同一 assignment 行的 CAS UPDATE；账户禁用/注销 writer 必须先锁同一 account 行。批准先锁定时撤权或禁用等待发布事务结束；撤权或禁用先提交时批准无法取得 active 账户与 assignment。任一步失败都回滚回执、快照、指针、Review 和 Audit，旧发布继续可用。GET 的授权线性化点是本次 Auth Middleware 的动态 Resolver；Handler 与 Service 对同一 Principal 的复核只是纵深防御。Decision 的最终授权线性化点是上述事务锁。

## 6. 审计、安全与错误

批准审计继续写 append-only `skill_governance_audit`，至少保存 skill、review、动作、前后状态、Reviewer、决定时间和首次 HTTP request ID；同一前向 Migration 为 006 前历史行兼容，向 `skill_command_receipt` 和 `skill_governance_audit` 新增可空 `request_id uuid`。新 HTTP 决定的 Command 在进入 Repository 前必须验证 request ID 为 UUIDv7，并在首次事务向 Receipt 与 Audit 写入同一值；重放保留首次持久化 request ID，但 HTTP Response 返回本次新 request ID。request ID 由服务端生成，不能从 Body 或自定义 Header 覆盖。结构化日志只允许 route、action、decision、actor ID、review ID、request ID 和稳定 error code，不得记录 Cookie、CSRF、原始 Idempotency-Key、Definition、邮箱、SQL 或底层错误。

未认证、capability denied 和 CSRF denied 由实际拦截请求的 Middleware/Handler 写结构化安全审计日志；持久化的通用管理员访问/数据范围审计属于后续完整 RBAC，因此本批不得把 `ADM-RBAC-001` 标为通过。

稳定错误映射：

| HTTP | Code | 语义 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | Query、UUID、JSON 或 If-Match 格式非法 |
| 400 | `IDEMPOTENCY_KEY_INVALID` | 幂等键非法 |
| 401 | `UNAUTHENTICATED` | 会话无效 |
| 403 | `SKILL_REVIEW_CAPABILITY_REQUIRED` | 当前权威角色不含 `skill.review` |
| 403 | `CSRF_INVALID` | 写请求 CSRF 失败 |
| 404 | `SKILL_REVIEW_NOT_FOUND` | 合法 Review 不存在 |
| 409 | `SKILL_REVIEW_CONFLICT` | 客户端 ETag、状态或治理前置冲突 |
| 409 | `IDEMPOTENCY_CONFLICT` | 同 Key 不同语义 |
| 503 | `SKILL_PERSISTENCE_UNAVAILABLE` | 权威存储、schema、Canonical JSON 或 digest 解析不可用 |

## 7. 前端与浏览器闭环

前端新增 exact `/admin/skills/reviews` 和只接受规范小写 UUIDv7 的 `/admin/skills/reviews/:review_id`。所有 `/admin/skills/reviews/...` 非法变体都映射到“受保护的无效管理路由”，不得回落 Home 或发起 Reviewer API。App Router 固定顺序是 auth bootstrapping/anonymous/unavailable → authenticated capability guard → page render。路由、导航和批准按钮只认 Principal `capabilities` 中精确的 `skill.review`；只有 `admin` role 没有 capability 仍拒绝。普通用户直达显示无权限且 Reviewer API 调用数为零。

Capability 在页面已打开期间被撤销、服务端返回 403 时必须立刻禁用决定、Abort 或用 generation 丢弃迟到请求，并且只触发一次 Session 重新解析；无 capability 收敛为 forbidden，解析 503 收敛为 auth unavailable，不得形成 403→bootstrap→自动重试循环。401、403、404 后不得保留可点击的 retry-decision；Detail 已为 approved 时清理本 Review 的 pending ledger。

页面必须覆盖 loading、empty、error/retry、分页、404、403、409、终态和未知写结果。详情完整只读展示冻结 Definition；409 保留上下文并要求刷新；未知写结果复用命令账本保存原 Idempotency-Key 与 ETag。

前端 DTO parser 使用 exact keys，拒绝额外字段；所有冻结字段必须存在，数组不得为 null，UUID 必须为规范 UUIDv7，时间必须为 RFC3339Nano，ETag 必须是 quoted strong opaque 值。List 只接受 `reviewing + [approve_and_publish]`；Detail 只接受 `reviewing -> [approve_and_publish]` 或 `approved/rejected/withdrawn -> []`，并严格校验 current published/comparison 不变量；Decision 必须包含规范 published snapshot ID 且只接受 `approved -> []`。重复 ID、未知 status/action、空 cursor、非法 Definition 或跨字段不变量失败关闭。`next_cursor` 字段必须存在且只能是 `null` 或非空 opaque string。

命令账本绑定 authority epoch，而不只绑定 user ID。epoch 是客户端每次成功 Login/Session Bootstrap 递增的内存 generation 与 principal ID、exact roles、exact capabilities 的组合；退出 authenticated、同用户新 Session、角色/capability 变化、403 re-bootstrap 或用户变化都必须先清账本，再允许新决定。

真实 Playwright 顺序固定为：

1. Creator 登录，确认无管理入口，创建并以 sentinel A 内容提交唯一 Skill；提交后继续保存 sentinel B 新草稿；
2. Creator 退出，Reviewer 登录，确认 Principal 含 `skill_reviewer` 和 `skill.review`；
3. Reviewer 从队列进入详情，必须看到 sentinel A 且看不到 sentinel B，并通过真实 HTTP 决定发布；
4. Reviewer 退出，Creator 重新登录，在真实 Picker 选择已发布 Skill；
5. Creator 发起 `project_quick_create.v2`，请求携带精确 `skill_id`，进入正式 Workspace。

真实链使用独立 `@w1-real-review` tag；Smoke 在启动 Chromium 前必须验证 Creator/Reviewer 四项凭据完整，缺失直接失败，禁止 skip 后通过。mock contract 用例可以保留，但不得进入 W1-C2 Evidence。全链必须在同一 browser context 顺序 logout/login，并断言 Creator/Reviewer Principal ID 不同、Reviewer Login exact roles/capabilities、Decision 的 CSRF/Idempotency-Key/If-Match/body/200、QuickCreate v2 的精确 skill ID/201/正式 Workspace、全部业务请求 same-origin 且没有 `/api/aigc/**`。

Evidence boolean 必须由 mandatory real tag、API 和数据库断言派生，禁止硬编码。Playwright 日志和 Evidence 不得包含登录凭据、Cookie、CSRF 或原始 Idempotency-Key。全链不得使用 `page.route`、本地 publisher、测试 Principal、伪造 Auth Response、Header/Body/env Runtime capability 作为通过证据；build-tagged Seeder 只允许准备正式 `user_role_assignment` 数据，权限仍由生产 Resolver 在每次请求解析。

## 8. 本地 Seeder 与运行边界

新增 build tag `localsmoke` 的 Reviewer Seeder，只允许 `DORA_ENV=local`，并严格接受唯一 DSN 形态：loopback、用户 `dora_business_app`、数据库 `/dora_business`、唯一 `sslmode=disable`，禁止 fragment、空密码、重复参数和 host/user/dbname query override。Creator、Reviewer 与 local provisioner 三个用户 ID 必须两两不同；Seeder 先复用正式密码用户创建逻辑，再以 provisioner 为 actor 调用正式 Authorization Service 幂等授予 `skill_reviewer`，reason 固定 `local_smoke_fixture`，approval reference 固定 `local-smoke-reviewer-fixture-v1`。成功只输出 assignment ID、role、reason 和三个非敏感 user ID。

Seeder 只准备测试数据。正式 Runtime 不注册赋权路由，也不读取环境变量决定 capability。W1-C2 实现必须删除旧 `local-smoke-skill-publisher` 源码和全部脚本调用；现有 W1 Smoke 的发布准备统一改为 Reviewer Seeder + 真实 Reviewer Login/List/Detail/Decision HTTP，不保留 capability 注入诊断旁路。

## 9. Migration、部署与回滚

`00600` 只做前向 CREATE/ALTER：创建 `user_role_assignment`，并向既有 Receipt/Audit 增加兼容可空 request ID。部署顺序固定为 Migration → 新 Binary；应用回滚只回滚 Binary 并保留前向兼容 Schema。生产禁止执行 destructive Down；Down 只供空库/local 契约测试，必须显式 DROP COLUMN/TABLE 且禁止 CASCADE。

Runtime `requiredBusinessTables` 和 required-column readiness 必须包含新表及两个 request ID 列。验证覆盖空库、005→006 升级、重复初始化失败安全、中文 COMMENT、零物理 FK、local Down 以及 assignment orphan/未知角色 not-ready。

## 10. 实现与验证顺序

1. Migration、Authorization Domain/Repository、离线 Role Admin CLI、动态 Auth Projection；
2. Auth 撤权即时生效和批准/撤权并发线性化测试；
3. Reviewer List/Detail/Decision Service、Repository 和 HTTP；
4. 本地 Reviewer Seeder，删除旧 publisher 并把所有 W1 Smoke 改为真实 Reviewer HTTP；
5. 前端 capability 门禁、队列、详情和批准交互；
6. 双身份 Playwright 与 Smoke/Evidence；
7. `GOWORK=off` Business test/race/vet/build/mod verify、前端全量 test/build/Chromium、PostgreSQL Migration/Repository 契约和 W1 Browser Smoke。

Evidence 至少包含 `reviewer_rbac=true`、`reviewer_revocation=true`、`browser_review_publish_quickcreate_v2=true`，并记录源码/二进制摘要。只有全部为真才可声明 W1-C2 完成。

## 11. 评审清单

- [ ] 角色来源、映射和失败关闭无 Header/Body/env 绕过；
- [ ] 同 Cookie 撤权后下一请求不再返回 capability；
- [ ] 批准与撤权、批准与账户禁用的两种提交顺序均有事务集成测试；
- [ ] Grant、Revoke、同义重放、重授予和 orphan Readiness 均走权威边界；
- [ ] Queue 和 Detail 分别固定一次 SQL；
- [ ] Detail 读取提交冻结 revision，并核对 Canonical digest；
- [ ] Decision 同时具备 CSRF、ETag、幂等和事务内角色复核；
- [ ] 普通用户无导航、无 API 请求且服务端返回 403；
- [ ] 浏览器链不依赖 mock、publisher 或 capability 注入；
- [ ] 文档没有把本切片夸大为完整管理员 RBAC 或 Skill 治理。
