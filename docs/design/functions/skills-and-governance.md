# Skill、审核、治理与市场功能现状

> 文档状态：Current Implementation / 只描述当前工作树
>
> 适用 Runtime：`business/cmd/business-service`、`agent/cmd/agent-service`
>
> 更新日期：2026-07-17

## 现状

Business 已实现用户 Skill 的完整最小管理纵切：Owner 创建结构化草稿、读取自己的 Skill、用 Strong ETag 全量替换草稿、提交精确草稿审核；持有 `skill.review` 的 Reviewer 可以读取待审队列和冻结详情，并批准发布；持有 `skill.govern` 的 Governor 可以暂停、恢复或永久下架已发布 Skill；匿名用户可以读取当前公开市场列表和详情。

Skill 的 Draft、审核、Published Snapshot 和治理状态彼此分离。草稿修订和 Published Snapshot 都是不可变事实，聚合根只移动当前指针。审核批准事务原子写入审核终态、Published Snapshot、当前发布指针、幂等回执和审计记录。

Quick Create v2 可以选择当前可用的 Published Skill。自有 Skill 使用 `owner_private` 权限证明；其他发布者在公开市场中的 Skill 使用 `public_market` 权限证明。Business 在创建 Project 的事务内重新校验当前发布指针和治理状态，Agent 只冻结该次 Session 的不可变 Skill Snapshot，后续发布或治理变化不回写旧 Session。

## 边界

当前只支持 `user` namespace 的结构化 Skill。以下能力没有实现：

- 系统 Skill、企业/团队共享 Skill 和组织级授权；
- Skill 安装、收藏、评分、购买、收益和计费；
- 已有 Project 的 Skill Binding 替换接口；
- Owner 撤回审核或 Reviewer 拒绝审核的公开写入口；当前 Reviewer 决定只支持批准并发布；
- `offline` Skill 的恢复；下架是当前治理状态机的终点；
- 市场搜索、复杂筛选、推荐、封面资产上传和评论；
- 由市场接口直接执行 Tool 或把市场定义当作 Agent 可执行注册表。

角色分配存在 Business 权威表和运维写入方式，但没有面向普通用户的角色管理 API。用户类型、前端按钮或请求 Header 都不会隐式授予 Reviewer/Governor 能力。

## 流程

### Owner 草稿与发布

1. 登录 Owner 以幂等键创建 Skill 和首个不可变内容修订。
2. Owner 读取详情中的 `draft_etag`，用 `If-Match` 全量替换草稿；服务端通过当前草稿指针 CAS 防止丢失更新。
3. Owner 使用新的幂等键和同一 `If-Match` 提交审核，审核记录冻结该内容修订和摘要。
4. Reviewer 每次请求都从当前 Session Principal 读取动态解析的 `skill.review`，再读取 `reviewing` 队列或冻结详情。
5. Reviewer 提交 `approved` 决定、Strong Review ETag 和幂等键；事务内再次锁定并验证 Reviewer 的 active assignment，然后原子发布。

### 治理与公开市场

1. Governor 每次请求动态校验 `skill.govern`。
2. 管理详情返回当前 Published Snapshot、治理状态、治理纪元和 Strong Governance ETag。
3. 治理命令按 ETag 和当前状态执行 `suspend`、`resume` 或 `offline`，并原子写回执与追加审计。
4. 匿名市场查询只返回“存在 current Published Snapshot 且治理状态为 `active`”的 Skill；不可公开的详情统一为 404。
5. Quick Create v2 在提交时重新读取同一权威条件，市场页面先前读到的结果不构成授权。

### Session Snapshot

1. Business 按 `enabled_skill_ids` 排序和去重要求解析当前 Published Snapshot。
2. 每项冻结 Publisher、权限 basis、治理纪元、Runtime Policy、允许的 Graph Tool Key、内容摘要和加密 Runtime Content。
3. Business 将 Binding、Resolution 和 Session Bootstrap v2 Outbox 与 Project 同事务提交。
4. Agent 验证 Canonical 与摘要后，保存不可变 Snapshot Header/Item；旧 Session 不追随当前 Skill 变化。

## 接口与状态

### Owner 与管理接口

| 方法与路径 | 当前行为 | 认证要求 |
| --- | --- | --- |
| `POST /api/v1/skills` | 创建 Skill 与首个不可变 Draft Revision | Session + CSRF + `Idempotency-Key` |
| `GET /api/v1/skills?scope=mine` | Keyset 分页读取当前 Owner 的 Skill；`scope=mine` 必填 | Session |
| `GET /api/v1/skills/:skill_id` | 读取当前 Draft、Published 摘要、审核和允许动作 | Session + Owner |
| `PUT /api/v1/skills/:skill_id/draft` | 用 `If-Match` 全量替换草稿并追加 Revision | Session + CSRF |
| `POST /api/v1/skills/:skill_id/reviews` | 冻结当前草稿并提交审核 | Session + CSRF + `If-Match` + `Idempotency-Key` |
| `GET /api/v1/admin/skill-reviews?status=reviewing` | 读取 `reviewing` 队列；`status=reviewing` 必填 | Session + `skill.review` |
| `GET /api/v1/admin/skill-reviews/:review_id` | 读取提交时冻结内容和当前发布对照 | Session + `skill.review` |
| `POST /api/v1/admin/skill-reviews/:review_id/decisions` | 当前只接受批准并发布 | Session + CSRF + `skill.review` + ETag + 幂等键 |
| `GET /api/v1/admin/skill-governance?status={active\|suspended\|offline}` | 按一个必填治理状态读取已发布 Skill | Session + `skill.govern` |
| `GET /api/v1/admin/skill-governance/:skill_id` | 读取当前治理详情和 Strong ETag | Session + `skill.govern` |
| `POST /api/v1/admin/skill-governance/:skill_id/decisions` | 执行 suspend/resume/offline | Session + CSRF + `skill.govern` + ETag + 幂等键 |

### 匿名市场接口

| 方法与路径 | 当前行为 |
| --- | --- |
| `GET /api/v1/skill-market` | newest-first Keyset 分页公开列表 |
| `GET /api/v1/skill-market/:skill_id` | 公开详情白名单投影 |

### 当前状态集合

| 状态轴 | 当前代码接受的值 |
| --- | --- |
| 审核 | `reviewing`、`approved`、`rejected`、`withdrawn`；当前公开决定只会写 `approved` |
| 治理 | `active`、`suspended`、`offline` |
| 治理动作 | `suspend`、`resume`、`offline` |
| Session Skill Snapshot | `empty`、`published_refs` |
| 权限 basis | `owner_private`、`public_market` |

市场 DTO 只返回公开白名单字段，不返回 Draft、Runtime Guidance、内部 Snapshot ID、Digest、治理纪元、审核、审计或管理 ETag。

## 数据 Owner

| 数据 | Owner | 当前规则 |
| --- | --- | --- |
| Skill 聚合、Draft Revision、Review Submission、Published Snapshot | Business PostgreSQL | Business 是唯一写入者 |
| Reviewer/Governor 角色分配与能力解析 | Business PostgreSQL | 每次 Session 解析和写事务内重新验证 |
| 治理状态、治理纪元、命令回执和审计 | Business PostgreSQL | 与状态迁移同事务提交 |
| Project Skill Binding、Resolution、权限证明 | Business PostgreSQL | Quick Create v2 创建时冻结 |
| Session Skill Snapshot Header/Item | Agent PostgreSQL | 只接受 Business Session Bootstrap v2 的冻结载荷 |

Agent 不读取 Business 当前 Skill 表，也不会在 Session 运行时重新解析最新发布版本。公开市场是只读产品投影，不是跨 Module 执行契约。

## 错误与安全

- Owner 资源不存在和不属于当前用户统一为 not found，避免枚举他人 Skill。
- Draft、Review 和 Governance 分别使用不可混用的 Strong ETag；过期 ETag 返回冲突，不做 last-write-wins。
- 创建、提交审核、批准发布和治理迁移使用 first-write-wins 幂等回执；同键异义返回 `IDEMPOTENCY_CONFLICT`。
- Reviewer 和 Governor 能力完全分离；Repository 写事务会重新锁定并验证 active 账户和角色 assignment，防止请求鉴权后的撤权竞态。
- Governance action、reason code 和 approval reference 都使用闭集或严格格式；客户端不能直接设置治理状态或治理纪元。
- 市场只读取当前发布指针和 `active` 治理状态，不从缓存、前端选择或旧链接推断可见性。
- Skill Runtime Content 在跨 Module Outbox 和 Agent Snapshot 中加密保存；普通市场和管理列表不返回执行正文。
- 日志和错误响应不输出完整 Skill Runtime Content、密文、摘要原始输入、SQL 或内部权限结构。

## 测试

当前仓库包含：

- Skill Definition Canonical、服务、审核、治理、市场和 Project Binding 单元测试；
- Business Skill、Review、Governance、Market、Project Binding PostgreSQL Repository 集成测试；
- Agent Session Skill Snapshot Canonical、加密、RPC、Repository 和旧 Session 兼容测试；
- HTTP Handler 的 Session、CSRF、ETag、幂等、能力隔离和匿名市场负向测试；
- `make w1-smoke` / `make w1-browser-smoke`：Owner 创建、审核发布、治理、公开市场、跨发布者 Quick Create 和 Session Snapshot 纵切；
- `make test-smoke-contracts`：W1 Smoke 模式和文档单一口径约束。

这些入口验证的是当前仓库提供的行为；是否通过应以具体提交上的实际执行结果为准。

## 生产差距

当前没有组织/企业共享、系统 Skill、安装收藏、交易计费、市场搜索推荐、封面资产、申诉流程和完整角色管理控制台。Reviewer 公开写入口只有批准发布，尚未提供拒绝、撤回和多级审核工作流。

Session 已能冻结 Skill Runtime Snapshot，但这不等于任意 Skill 可以动态注册或执行新的公共 Tool。当前可执行 Graph Tool 仍由 Agent 代码和显式 Runtime Profile 控制；市场声明和 Skill 文本不能绕过服务端 Registry。跨环境生产部署仍需要独立的密钥管理、服务身份、审计留存和治理运营能力。
