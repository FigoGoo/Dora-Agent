# W1-D Skill 治理能力与管理处置 v1

> 状态：Frozen / Approved
>
> 设计日期：2026-07-14
>
> 冻结日期：2026-07-14
>
> 覆盖范围：`skill_governor` 动态 RBAC、已发布 Skill 暂停/恢复/下架、管理只读投影、强并发与审计闭环
>
> 依赖：[W1 Skill 与 Tool 入口基础契约 v1](../cross-module/w1-skill-tool-entry-contract-v1.md)、[W1-C2 Reviewer RBAC 与 Skill 管理审核 v1](w1-reviewer-rbac-admin-review-v1.md)

## 1. 结论与完成定义

W1-D 实现公开 Skill 市场开放前必须具备的最小真实治理纵切：Business 权威角色分配把 `skill_governor` 动态解析为 `skill.govern`；持有该 capability 的治理人员可以读取已发布 Skill 的当前治理投影，并使用受 CSRF、Strong ETag、幂等、事务内二次授权和追加审计保护的命令执行暂停、恢复或永久下架。

完成必须同时满足：

1. `skill.review` 与 `skill.govern` 完全分离；Reviewer、企业用户类型、前端状态、Header、Body 和环境变量都不能隐含治理权限；
2. 同一 Cookie 对应的 `skill_governor` assignment 被撤销后，下一次请求立即失去 `skill.govern`；
3. 暂停和下架提交后，新 Project Skill Binding 与 QuickCreate v2 立即失败关闭，不能继续解析为可用 Skill；
4. 治理写事务先锁定并复核 active governor account/assignment，再锁 Skill、校验 Strong ETag 和状态迁移，最后原子写回执、状态、治理纪元和审计；
5. 重复、并发、超时重试最多产生一个治理迁移事实，任一步失败不留下半完成状态；
6. `offline` 是本契约中的终态，不能通过恢复、重新发布或直接复用 Reviewer 批准入口重新上线。

本设计关闭的是 W1 公开读取前的“可暂停、可恢复、可下架”安全门禁，不表示完整 `ADM-RBAC-001`、`ADM-SKILL-001` 或 `ADM-SKILL-002` 已通过。公开 Skill 市场仍需独立 `Skill Market Read v1` 设计和验收，不能因本文存在而自动注册；市场必须消费同一 `published + active` 权威条件，并在联合门禁中另行验证暂停/下架后的公开读取和缓存失效。

## 2. 明确排除

本批不实现：

- 驳回审核、Owner 撤回审核、治理申诉、双人复核、数据范围授权或通用管理员后台；
- `offline -> active/suspended`、治理回滚、用户可见版本历史或产品版本回滚；
- 修改 Owner 草稿、替创建者重写 Skill、改变发布快照正文或切换历史发布快照；
- Tool、Graph Tool、Runner、既有 Session 或在途 Operation 的 Kill Switch；
- 收藏、举报、市场排序、推荐、Publisher Profile、公开调用指标和完整市场读取；
- 治理历史分页、导出、高敏明文查看、自由文本原因或附件；
- 把 `governance_epoch`、内部快照 ID、审计记录或管理 ETag 暴露给普通用户和公开市场；
- 把本地 Seeder 或直接 SQL 更新当作生产治理能力。

Owner 当前仍只看到既有 `governance_status`。暂停/下架原因的 Owner 安全投影、申诉和通知属于后续纵切；本批只保证管理审计中原因与审批引用可追溯，因此不得宣称完整满足“创建者查看原因”。

## 3. 状态机与作用范围

### 3.1 合法迁移

治理状态与内容 `draft/published`、审核 `reviewing/approved/...` 保持独立。只允许：

```text
active --suspend--> suspended
suspended --resume--> active
active --offline--> offline
suspended --offline--> offline
```

禁止：

- `active -> active`、`suspended -> suspended`、`offline -> offline`；
- `active -> resume`、`suspended -> suspend`；
- 任意 `offline -> active/suspended`；
- 未发布 Skill 的暂停、恢复或下架。

管理投影的 `allowed_actions[]` 使用固定顺序：

- `active`：`["suspend", "offline"]`；
- `suspended`：`["resume", "offline"]`；
- `offline`：`[]`。

### 3.2 对新旧运行的影响

首次批准发布仍要求治理状态为 `active`。治理迁移不修改 `current_published_snapshot_id`，不可变发布内容继续保留用于审计；每次成功迁移必须令 `governance_epoch += 1` 并令 Skill 聚合 `version += 1`。

状态影响固定为：

- `active`：允许新的 Project Skill Binding；后续公开市场只允许读取该状态；
- `suspended`：暂时阻止新的 Project Skill Binding 和公开读取，恢复后可再次用于新绑定；
- `offline`：永久阻止新的 Project Skill Binding、公开读取和再次批准发布；
- 已经成功创建并冻结 Skill Snapshot 的既有 Session 不在本批强制中断，继续遵循其冻结契约；对在途运行的强制取消必须等待 Runner/Kill Switch 独立设计。

Business 的 Project Skill Binding 解析必须继续以 `current_published_snapshot_id`、当前发布修订和 `governance_status=active` 为同一权威条件，不能只相信客户端保存的 Skill ID 或旧查询结果。

## 4. 身份、角色与 capability

### 4.1 固定闭集

Authorization 角色与能力扩展为：

| Role | Capability |
| --- | --- |
| `skill_reviewer` | `skill.review` |
| `skill_governor` | `skill.govern` |

一个账户可以分别获得两个 active assignment，Resolver 按字典序稳定去重投影。角色分配仍只保存 `role_key`，不保存任意 capability 字符串。新增角色必须修改代码闭集、Migration CHECK、Readiness 和测试。

`skill_reviewer` 不能调用治理 API；`skill_governor` 不能因治理角色读取审核队列或批准发布。前端同时拥有两个 capability 时可以显示两个入口，但权限仍由服务端分别判断。

### 4.2 Grant、Revoke 与动态解析

沿用 `business-role-admin` 和 Authorization Service/Repository：

- CLI 的 `grant|revoke` 允许闭集角色 `skill_reviewer|skill_governor`，仍要求 target、独立 active actor、稳定 reason 和外部 approval reference；
- active partial unique 继续使用 `(user_id, role_key)`，同一用户可同时拥有两个不同角色；
- Session Login 与每次 Resolve 都从数据库动态解析，不把 capability 固化到 Session、JWT、Redis、浏览器存储或进程缓存；
- 未知角色、畸形 assignment 和数据库错误失败关闭，不能降级为空 capability；
- Runtime Readiness 的角色闭集、orphan 检查和 CLI 输出同步扩展。

治理写事务与 Revoke 使用统一 `account -> assignment` 锁序。撤权先提交时，治理命令不能获得 active assignment；治理命令先锁定时，撤权等待本次迁移完整提交或回滚。

## 5. 持久化与前向 Migration

新增独立前向 Migration `20260714000700_create_skill_governance`，不得修改 `00400`、`00500` 或 `00600`。

### 5.1 角色约束

Migration 替换 `business.user_role_assignment` 的角色 CHECK，使其只允许 `skill_reviewer|skill_governor`，并更新中文 COMMENT。现有 append-only Trigger、active partial unique 和 Resolver 索引保持不变。

### 5.2 Skill 命令回执

复用 `business.skill_command_receipt`，新增：

- `command_type='governance_transition'`；
- `response_governance_epoch bigint NULL`，只要求新治理命令非空且 `>= 2`；
- 治理回执的 `scope_id/result_skill_id` 固定为 Skill ID；
- `result_published_snapshot_id/response_published_snapshot_id` 固定为命令执行时的 current published snapshot；
- `response_draft_revision_id` 保存命令执行时当前草稿引用，只用于满足既有冻结回执结构，不进入治理 HTTP DTO；
- `response_governance_status`、`response_governance_epoch` 和 `created_at` 冻结首次安全响应。

数据库 CHECK 必须把回执拆成互斥的完整分支：

- 只有 `governance_transition` 允许且必须具有 `response_governance_epoch >= 2`；既有 create、submit 和 approve 必须保持 epoch 为 null；
- 治理回执必须满足 `scope_id=result_skill_id`、`result_published_snapshot_id=response_published_snapshot_id` 且两个引用均非空；
- 治理回执的 result content/review 引用和全部 response review 字段必须为空，`request_id` 必须非空；
- `response_governance_status` 只允许 `active|suspended|offline`，其他命令不能伪装为治理回执。

既有 create、submit 和 approve 的原约束保持不变，保证前向兼容。原始 Idempotency-Key 仍只保存 SHA-256 digest。

### 5.3 追加治理审计

复用 append-only `business.skill_governance_audit`，允许新动作：

- `governance_suspended`：`active -> suspended`；
- `governance_resumed`：`suspended -> active`；
- `governance_offlined`：`active|suspended -> offline`。

前向新增可空字段：

- `actor_role_key varchar(64)`；
- `governance_epoch bigint`；
- `approval_reference varchar(160)`；
- `source_address inet`；
- `command_receipt_id uuid`。

约束必须区分历史批准记录与新治理记录：

- `review_approved_and_published` 保持既有 review transition 语义，兼容旧行新增字段为空；
- 新治理动作要求 `review_submission_id IS NULL`、`actor_role_key='skill_governor'`、`safe_reason_code` 非空、`governance_epoch >= 2`、`approval_reference` 非空、`source_address` 非空、`request_id` 非空、`command_receipt_id` 非空；
- transition CHECK 精确固定 `governance_suspended: active -> suspended`、`governance_resumed: suspended -> active`、`governance_offlined: active|suspended -> offline`，禁止 action 与前后状态错配；
- `safe_reason_code` 只能取第 6.4 节 action 对应闭集；`approval_reference` 只能取同节 ASCII 工单格式；
- 不保存完整 Skill 正文、Cookie、CSRF、原始幂等键、邮箱、自由文本或数据库错误。

新增 `command_receipt_id` 非空 partial unique index，强制每个治理回执至多关联一条审计；历史批准审计保持该字段为空。因为项目禁止物理外键，Readiness 使用固定 COUNT JOIN 检查治理审计引用的回执存在，并精确核对 actor、Skill、request ID、response status/epoch 与时间；任一 orphan 或错配都 not ready。

`source_address` 只取 HTTP TCP 连接的规范 IPv4/IPv6 peer address：从 `RemoteAddr` 去除端口后用 `netip.ParseAddr` 解析并 `Unmap`，拒绝 zone、非法值和来自 `Forwarded`/`X-Forwarded-For`/`X-Real-IP` 的覆盖。当前 Gin 明确禁用可信代理，因此该字段表示直连 peer；未来如需记录代理前的用户地址，必须先独立冻结可信代理 CIDR、Header 清洗和多跳规则。管理读取默认不返回该字段，后续高敏审计查询必须脱敏并另写访问审计；保留周期沿用不可变治理审计，不在本批提供删除入口。

既有 immutable Trigger 继续禁止 UPDATE/DELETE。Skill 表已有 `governance_status`、`governance_epoch`、`version` 和 `updated_at`，本批不新增可变治理镜像列。

### 5.4 管理列表索引

新增：

```sql
CREATE INDEX idx_skill_published_snapshot__published_id
    ON business.skill_published_snapshot (published_at DESC, id DESC);
```

该索引用于治理列表按当前发布时间 keyset 扫描，也为后续市场读取复用。007 固定使用与现有 Business Migration 一致的普通 `CREATE INDEX`，在业务流量开放前的 Migration 窗口执行并记录锁耗时；本批不引入 Runner 尚未声明支持的 `CREATE INDEX CONCURRENTLY` 或 Runtime 临时建索引。

Migration、表和列继续零物理外键、零级联、零物理删除。Down 只供空库/local 契约测试，禁止生产执行和 `CASCADE`。Down 在恢复只允许 `skill_reviewer` 的角色 CHECK 或删除治理列前，必须显式检查并拒绝以下任一事实：`skill_governor` assignment、governance receipt、governance audit、非 `active` Skill 或 `governance_epoch <> 1`；不能依赖后续 DDL 偶然失败并留下 dirty Migration。

## 6. 管理 HTTP v1

所有路由同源、`Cache-Control: no-store`，使用 Cookie Session；Principal 只能来自 Auth 私有 Context。

| 方法 | 路径 | 认证与语义 |
| --- | --- | --- |
| `GET` | `/api/v1/admin/skill-governance?status=active&cursor=...` | Session + 动态 `skill.govern`；已发布 Skill keyset 列表 |
| `GET` | `/api/v1/admin/skill-governance/:skill_id` | Session + 动态 `skill.govern`；当前发布与治理详情 |
| `POST` | `/api/v1/admin/skill-governance/:skill_id/decisions` | Session + 动态 `skill.govern` + CSRF + Strong `If-Match` + `Idempotency-Key` |

非法 UUID、重复或未知 Query、未知 JSON 字段、尾随 JSON、缺失 Header 必须在写事务前拒绝。Actor、角色、状态、治理纪元、快照、操作时间和 ETag 都不能由 Body 或自定义 Header 覆盖。

POST Handler 在调用 Service 前按第 5.3 节从 `RemoteAddr` 解析规范 peer address；失败返回 `400 INVALID_REQUEST`，且不能进入治理事务。Source address 不进入 Semantic Digest，同义重放从首次审计事实恢复原地址，不用重试请求的新网络地址覆盖。

### 6.1 列表

`status` 必须精确出现一次，只允许 `active|suspended|offline`；cursor 可省略或精确出现一次。固定页大小 20，排序为 `(published_at DESC, published_snapshot_id DESC)`，Repository 使用 `LIMIT 21` 和一次 JOIN current pointer 查询。

opaque cursor `skill_governance_queue_cursor.v1` 只包含：

- `status`；
- `published_at_unix_nano`；
- `published_snapshot_id`。

Cursor 使用无填充 Base64URL、最大 1024 字节，严格 JSON 解码并拒绝未知字段、尾随 JSON、非法时间和非 UUIDv7。cursor status 与 Query 不一致时返回非法请求。

该列表是管理工作队列，不是审计级快照。分页期间再次发布会使 active 项按新 snapshot 移到更前位置，治理迁移会使项目离开当前 status 集合或进入另一个集合；客户端必须跨页按 `skill_id` 去重，切换 status 或主动刷新时丢弃旧 cursor 并从第一页重建。允许并发变化造成一次扫描跳项，但不允许同一响应重复 ID、返回与 Query 不同的当前 status，或用 offset/total 冒充一致快照。

```json
{
  "items": [{
    "skill_id": "uuidv7",
    "name": "...",
    "summary": "...",
    "category": "...",
    "published_at": "RFC3339Nano",
    "governance_status": "active",
    "governance_epoch": 1,
    "allowed_actions": ["suspend", "offline"]
  }],
  "next_cursor": null,
  "request_id": "uuidv7"
}
```

名称、摘要和分类必须来自 `current_published_snapshot_id` 指向的不可变 Definition，不得来自当前草稿。列表不返回完整 Definition、Owner 身份、内部发布修订、快照 ID、digest、审核信息、原因、审批引用或 ETag。

### 6.2 详情

详情一次 JOIN 查询 Skill 与 current Published Snapshot，并使用同一 Mapper：

- Skill 必须存在 current published pointer；
- snapshot.skill_id、publication revision、schema、Canonical Definition 和 digest 必须与聚合一致；
- 存储损坏返回 `503 SKILL_PERSISTENCE_UNAVAILABLE`，不能伪装 404；
- 合法 UUID 但不存在或从未发布统一返回 `404 SKILL_GOVERNANCE_NOT_FOUND`；
- `active/suspended/offline` 均可被持权 Governor 读取。

```json
{
  "skill": {
    "skill_id": "uuidv7",
    "definition": { "schema_version": "skill_definition.v1" },
    "published_at": "RFC3339Nano",
    "governance_status": "active",
    "governance_epoch": 1,
    "governance_etag": "\"opaque\"",
    "allowed_actions": ["suspend", "offline"]
  },
  "request_id": "uuidv7"
}
```

管理详情允许读取当前发布的完整结构化 Definition，用于判断处置对象，但不能读取当前草稿、历史快照、审核原因、Publisher 私密身份或 Tool/计费的未冻结内部事实。Body `governance_etag` 与 HTTP `ETag` 必须完全一致。

### 6.3 Strong Governance ETag

ETag 使用 `skill_governance_etag.v1` 域分离，对以下规范字段计算 SHA-256/Base64URL 并编码为单个 strong quoted tag：

- Skill ID；
- current published snapshot ID；
- governance status；
- governance epoch。

Owner 后续编辑草稿不会使治理 ETag 失效；再次批准发布、暂停、恢复或下架都会改变至少一个 ETag 输入。POST 只接受一个完全匹配的 strong tag，拒绝 `*`、`W/`、逗号列表、重复 Header、空值和非规范空白。客户端必须把 ETag 当 opaque 字符串。

### 6.4 治理决定

请求 Body 固定为：

```json
{
  "action": "suspend",
  "reason_code": "content_safety",
  "approval_reference": "TICKET-123"
}
```

`action` 只允许 `suspend|resume|offline`。`reason_code` 使用 action 对应闭集：

- suspend：`content_safety`、`copyright_risk`、`privacy_risk`、`fraud_or_abuse`、`tool_dependency_risk`、`policy_violation`、`incident_containment`；
- resume：`risk_cleared`、`appeal_approved`、`incident_resolved`、`dependency_restored`、`policy_remediated`；
- offline：`content_safety`、`copyright_risk`、`privacy_risk`、`fraud_or_abuse`、`tool_dependency_risk`、`policy_violation`、`owner_request`、`repeated_violation`。

`approval_reference` 必须是长度不超过 160 的规范 ASCII，并完全匹配 `^[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}$`；服务端不 trim、不改写大小写，格式外输入直接拒绝。暂停、恢复和下架都要求闭集原因与外部审批引用，避免自由正文、换行或恢复无审计旁路。

合法 Skill ID 但不存在或从未发布时，Detail 和 Decision 都统一返回 `404 SKILL_GOVERNANCE_NOT_FOUND`；只有已发布 Skill 的 ETag、当前状态或 action 前置不匹配才返回 `409 SKILL_GOVERNANCE_CONFLICT`。

首次成功和同义重放统一返回 `200`：

```json
{
  "skill": {
    "skill_id": "uuidv7",
    "governance_status": "suspended",
    "governance_epoch": 2,
    "transitioned_at": "RFC3339Nano",
    "governance_etag": "\"opaque\"",
    "allowed_actions": ["resume", "offline"]
  },
  "request_id": "uuidv7"
}
```

HTTP Response `ETag` 与 Body 新 ETag 完全一致。响应不回显 reason、approval reference、内部 snapshot ID、聚合 version 或首次持久化 request ID。

## 7. 幂等、事务与并发

### 7.1 幂等语义

回执唯一作用域为 `(governor_user_id, governance_transition, skill_id, key_digest)`。Semantic Digest 使用版本化 schema，精确覆盖：

- Skill ID；
- action；
- reason code；
- approval reference；
- 原样 strong If-Match。

同一 actor、同 Key、同语义返回首次冻结的状态、epoch、时间和 ETag，不追加审计；同 Key 不同语义返回 `409 IDEMPOTENCY_CONFLICT`。不同 Governor 使用相同原始 Key 不属于重放，并由 Skill 锁和 ETag 收敛为至多一个状态迁移。

### 7.2 事务锁序

首次命令或重放都在同一事务中：

1. `FOR UPDATE` 锁定并验证 Governor active `user_account`；
2. `FOR UPDATE` 锁定并验证该用户 active `skill_governor` assignment；
3. 按 actor/command/skill/key 查询既有回执；存在则校验 Semantic Digest，读取冻结响应并返回，不重验当前 Skill 状态；
4. 不存在回执时 `FOR UPDATE` 锁定 Skill，并在同一固定读取中取得 current Published Snapshot；核对 snapshot ID、skill ID、聚合/快照 publication revision、schema、Canonical Definition 和 digest，悬空或错配返回持久化不可用；
5. 校验治理状态、governance epoch、action 合法迁移，重算并常量时间比较 Strong ETag；
6. 生成非空回执 ID、审计 ID 和 UTC 时间，CAS 更新 `governance_status`、`governance_epoch+1`、`version+1`、`updated_at`；
7. 插入命令回执与一条治理审计；
8. 从冻结结果构造响应并提交。

事务内禁止调用 Agent、Redis、etcd、对象存储、消息系统或其他外部服务。任一步失败必须回滚 Skill、回执和审计。

### 7.3 并发边界

- 治理与治理：同一 Skill 行锁串行；旧 ETag 的不同命令返回 conflict；
- 治理与批准发布：二者最终锁定同一 Skill。治理先提交时批准因非 active 失败；批准先提交时治理读取并作用于新 current published pointer；
- 治理与 Owner 草稿写：草稿变更不会单独使治理 ETag 失效，但 Skill 行锁保证聚合更新不丢失；
- 治理与角色撤销/账户禁用：统一 account → assignment 锁序决定最终授权线性化；
- 请求取消、超时或响应丢失属于 unknown outcome，客户端只能使用原 Key、原 ETag 和原语义显式重试，禁止自动换 Key。

## 8. 错误、安全与日志

稳定错误映射：

| HTTP | Code | 语义 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | Query、UUID、JSON、Header 或 ETag 格式非法 |
| 400 | `IDEMPOTENCY_KEY_INVALID` | 幂等键非法 |
| 401 | `UNAUTHENTICATED` | 会话无效 |
| 403 | `SKILL_GOVERNANCE_CAPABILITY_REQUIRED` | 当前权威角色不含 `skill.govern` |
| 403 | `CSRF_INVALID` | 写请求 CSRF 失败 |
| 404 | `SKILL_GOVERNANCE_NOT_FOUND` | 合法 Skill 不存在或从未发布 |
| 409 | `SKILL_GOVERNANCE_CONFLICT` | ETag、当前状态、action 或发布前置冲突 |
| 409 | `IDEMPOTENCY_CONFLICT` | 同 Key 不同语义 |
| 503 | `SKILL_PERSISTENCE_UNAVAILABLE` | 权威存储、schema、Canonical JSON 或 digest 不可用 |

所有成功和失败响应都 `Cache-Control: no-store` 并包含本次服务端 UUIDv7 request ID。Request ID 生成失败使用保留 emergency ID 并返回 503；客户端不能覆盖 request ID。

结构化安全日志只允许 route、action、actor ID、skill ID、request ID、result 和稳定 error code。不能记录 Cookie、CSRF、原始 Idempotency-Key、If-Match、完整 Definition、reason 原文、approval reference、邮箱、SQL、DSN 或底层错误。

GET capability denied、POST capability denied 和 CSRF denied 由真实拦截层记录安全审计日志；持久化治理审计只在首次成功事务写入，重放和失败不追加。

## 9. 前端管理路径

新增受保护路由：

- `/admin/skills/governance`：治理列表；
- `/admin/skills/governance/:skill_id`：治理详情。

路由只接受规范小写 UUIDv7。非法详情路径显示受保护的无效治理路由，不能回落 Home、市场或发起 API。导航、页面和按钮只认 Principal exact capability `skill.govern`；只有 `skill.review` 的用户无治理导航且治理 API 调用数为零。

页面必须覆盖 loading、empty、error/retry、分页、403、404、409、终态、撤权重新解析和 unknown outcome。详情完整只读展示 current published Definition，按钮严格来自 `allowed_actions[]`。`offline` 不显示恢复按钮。

治理命令复用内存命令账本模式，绑定 authority epoch、Skill ID、action、原 Idempotency-Key 与 ETag。401/403、退出登录、同用户新 Session、角色/capability 变化和 hard refresh 必须先清理不再可安全重放的意图。409 保留详情上下文、刷新权威状态并废弃旧命令；`IDEMPOTENCY_CONFLICT` 禁止自动换 Key。

前端 DTO parser 使用 exact keys，拒绝额外字段、重复 ID、非法状态/action/时间/UUID/ETag、null 数组和跨字段不变量。不得使用 LocalStorage、生产 Mock、Header capability 注入或 `/api/aigc/**`。

## 10. 本地种子与真实冒烟

扩展 build-tagged local Reviewer Seeder，新增与 Creator、Reviewer、provisioner 两两不同的 Governor 测试账户。Reviewer 只获得 `skill_reviewer`，Governor 只获得 `skill_governor`；两个角色继续通过正式 Authorization Service 独立授予，并使用不同 assignment ID、role-specific reason 和 approval reference。生产 Runtime 不读取 Seeder 环境变量决定 capability。

真实 Chromium/HTTP 冒烟至少按顺序验证：

1. Creator 创建并提交 Skill，Reviewer 通过真实 Reviewer API 批准发布；
2. Creator 使用该 Skill 的真实 QuickCreate v2 成功；
3. Governor 登录，通过真实治理详情取得 ETag 并暂停，重复同义请求只产生一次迁移和一条审计；
4. Creator 使用同一 Skill 创建新 Project 被治理失败关闭；既有已创建 Session 不被本测试伪造为已取消；
5. Governor 恢复后，新 QuickCreate v2 再次成功；
6. Governor 永久下架后，新 QuickCreate 与治理恢复失败关闭；Creator 随后更新草稿并提交一个新的 Review，Reviewer 读取该新 Review 的 ETag 后尝试批准，必须因 Skill 已 offline 被拒绝，并核对 Published Snapshot 数量、current pointer 和 publication revision 均未变化；
7. Creator 与只有 `skill.review` 的 Reviewer 调用治理 API 均返回 403；
8. 撤销 `skill_governor` 后，同一 Cookie 下一次请求失去治理能力。

测试必须核对数据库：状态、epoch、聚合 version、回执唯一性、审计唯一性、current published pointer 未改变，以及失败/重放不增加事实。日志和 Evidence 不得包含凭据、Cookie、CSRF、原始幂等键、ETag、reason 或 approval reference。

本批 Evidence 至少新增：

- `skill_governor_rbac=true`；
- `skill_governor_revocation=true`；
- `skill_governance_idempotency=true`；
- `skill_governance_quickcreate_gate=true`；
- `skill_governance_offline_terminal=true`。

## 11. Migration、测试与实现顺序

### 11.1 必测项

- Migration：空库、006→007 升级、重复初始化失败安全、中文 COMMENT、零物理 FK、约束、索引、local Down；
- Authorization：双角色解析、排序去重、未知角色失败、Grant/Revoke/重授予、Readiness；
- Service：状态矩阵、allowed actions、ETag、cursor、幂等 digest、非法输入和 repository error 收敛；
- Repository：一次 SQL list/detail、current pointer 完整性、四条合法迁移、全部非法迁移、同义重放、并发 100 请求；
- 事务集成：治理/撤权、治理/账户禁用、治理/批准发布的两种提交顺序；
- HTTP：Session、capability、CSRF、强 ETag、严格 JSON/Query、安全错误 Envelope 和 no-store；
- 前端：路由/capability guard、exact parser、状态页、命令账本、撤权收敛和无生产 Mock；
- 回归：Owner Skill、Reviewer 发布、Project Skill Binding、QuickCreate v2、Workspace Snapshot/SSE 和 W1 canonical。

### 11.2 实现顺序

1. 交叉评审本文并关闭全部 P0/P1，状态改为 Frozen / Approved；
2. 007 Migration、Authorization 双角色闭集、CLI 与 Readiness；
3. Governance Domain/Service、Repository 读取和事务写入；
4. HTTP Handler、Server 注册和安全审计；
5. 前端治理列表/详情与 capability 门禁；
6. Seeder、真实治理冒烟和 Evidence；
7. 在治理门禁证据通过后，单独冻结并实现 `Skill Market Read v1`。

## 12. 评审清单

- [ ] `skill.review` 与 `skill.govern` 无角色、路由或 Service 隐式继承；
- [ ] 状态机仅含四条合法边，offline 终态不能恢复；
- [ ] Strong ETag 不受草稿编辑影响，但发布指针、状态或 epoch 改变必失效；
- [ ] 回执可在后续状态变化后重放首次冻结响应；
- [ ] account → assignment → receipt → skill 锁序与撤权、批准发布无死锁或越权窗口；
- [ ] 暂停/下架后新 Binding 失败，既有 Session 行为没有被夸大；
- [ ] 审计包含 actor、role、目标、前后状态、epoch、reason、approval reference、request ID 和时间；
- [ ] 列表/详情只读 current published snapshot，不读取当前草稿；
- [ ] 管理端无 capability 时不发 API，服务端仍独立 403；
- [ ] 文档没有把本纵切夸大为完整 RBAC、完整 Skill 治理或公开市场完成。
