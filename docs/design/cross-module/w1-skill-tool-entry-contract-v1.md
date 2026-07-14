# W1 Skill 与 Tool 入口基础契约 v1

> 状态：Frozen / W1 Foundation 实现基线
>
> 冻结日期：2026-07-14
>
> 覆盖范围：`SMK-005`、`SMK-006` 的基础纵切，以及 `SMK-007`、`SMK-008` 的契约准备
>
> 重要限制：本文不批准任何 Graph Tool、Runner、Tool Run、计费或模型执行实现。

## 1. 冻结结论

W1 按两个互不混淆的层次推进：

1. 本批允许实现 Business Skill 草稿、审核提交、不可变发布快照、Owner 投影，以及 Agent Session Skill Snapshot 的前向兼容基础。
2. 六个 Graph Tool 的独立设计和 `aigc.contract.v1alpha1` 仍处于 Draft；在它们通过评审前，只能冻结目录 DTO 和不可用原因，禁止创建占位 Graph、假 Run、假版本或返回伪造的可执行状态。

因此，本契约存在不表示 `SMK-005`～`SMK-008` 已通过：

- `SMK-005` 只有在结构化 Builder、后端字段校验和未授权 `@Tool` 失败关闭均有真实证据后才可通过；
- `SMK-006` 只有在真实发布、旧/新 Session 快照隔离和前端无版本明细均有证据后才可通过；
- `SMK-007` 在六个生产 Graph 尚未审核和编译前只能验证 exact-set、顺序和不可用原因，不能宣称“六个可执行版本”通过；
- `SMK-008` 必须等待持久化 Input/Turn、唯一 Runner 和 `write_prompts` Graph 实现，当前不得以保存前端选择代替 Run 冻结。

## 2. Owner 与禁止边界

| 事实 | Owner | 其他 Module 的访问方式 |
| --- | --- | --- |
| Skill 聚合、内容修订、审核提交、发布快照、治理状态 | Business | 版本化 HTTP/RPC |
| 公共 `@Tool` Definition、权限、状态与计费策略 | 待独立契约冻结；当前不得假设为 Agent Graph Tool | 当前只允许空引用集合 |
| Project Enabled Skill Binding | Business | Session 创建时由 Business 解析 |
| Session Skill Snapshot | Agent | Agent Repository；前端只读安全投影 |
| 六个 Graph Tool Definition 与可执行 Registry | Agent | 设计审核通过后由 Agent 启动时编译和发布 |
| Tool Run、Receipt、Approval 与 EventLog | Agent | 设计审核通过后的 Agent HTTP/SSE |

强制约束：

- 公共 `@Tool` 与六个 Agent-facing Graph Tool 是两类资产，不得共用同一个字符串引用冒充相同契约；
- Business、Agent 不得互相引用 `internal` 包，也不得直连对方数据库；
- 客户端、Skill 内容和模型都不能提交可信权限、价格、版本、治理结论或预算；
- 企业用户类型不等于 Reviewer 或管理员；Reviewer 最小入口只允许按 [W1-C2 Reviewer RBAC 与 Skill 管理审核 v1](../business/w1-reviewer-rbac-admin-review-v1.md) 解析正式 `skill.review`，治理仍等待独立 `skill.govern` 设计；
- 本批不得修改已发布的 W0 Migration，只能使用前向 Migration。

## 3. Skill 产品投影与内部状态

### 3.1 用户可见内容状态

用户端只显示：

- `draft`：不存在当前发布快照；
- `published`：存在当前发布快照。

当已发布 Skill 继续编辑草稿时，仍显示 `published`，并返回 `has_unpublished_changes=true`。用户端不得展示 publication revision、版本列表、版本差异、版本切换或版本回滚。

### 3.2 审核与治理

审核状态独立为：

```text
reviewing -> approved
reviewing -> rejected
reviewing -> withdrawn
```

治理可用性独立为：

```text
active <-> suspended
active/suspended -> offline
```

审核和治理状态不能扩展成第三种 Skill 内容状态。没有正式 `skill.review` 或 `skill.govern` capability 的 HTTP Principal 不得执行审核或治理动作。

## 4. 结构化 SkillDefinitionV1

Skill 核心定义必须使用强类型 DTO；禁止使用一个大文本字段或 `map[string]any` 代替稳定字段。

### 4.1 顶层字段

| 字段 | 约束 |
| --- | --- |
| `name` | 必填；NFC UTF-8；去除首尾空白后非空 |
| `summary`、`category`、`tags[]` | 分离保存；标签去重并使用稳定顺序 |
| `input_description`、`output_description` | 分离保存，不接受客户端价格或权限声明 |
| `invocation_rules` | 多 Skill 选择规则，不能扩大 Tool 或资源权限 |
| `plan_creation_spec` | `CapabilityGuidanceV1` |
| `analyze_materials` | `CapabilityGuidanceV1` |
| `plan_storyboard` | `CapabilityGuidanceV1` |
| `generate_media` | `CapabilityGuidanceV1` |
| `write_prompts` | `CapabilityGuidanceV1` |
| `assemble_output` | `CapabilityGuidanceV1` |
| `examples[]`、`starter_prompts[]` | 分离结构化列表 |
| `market_listing` | 封面、详情、版权与用户可见说明；不得携带 Secret |
| `public_tool_refs[]` | 稳定公共 Tool 引用；当前实现基线只允许空数组 |

`market_listing` v1 固定为：

- `cover_asset_id`：可空的 Business Asset 稳定引用，缺省在 JSON 中固定编码为 `null`；当前未接入 Asset 时只能为 `null`，任意非空引用必须失败关闭；
- `detail`：市场详情说明；
- `copyright_notice`：版权声明；
- `user_notice`：用户可见使用说明。

`tags`、`examples`、`starter_prompts`、`public_tool_refs` 四个集合字段必须显式存在并编码为 JSON 数组；禁止省略或编码为 `null`，允许使用确定的空数组 `[]`。其中 `public_tool_refs` 在 W1 只能是 `[]`。`examples[]` 固定为 `{input, output}`，不得扩展为可执行脚本；`starter_prompts[]` 只保存普通用户可见字符串。六个 `CapabilityGuidanceV1` 在 JSON 中使用上述六个稳定 key 作为顶层字段，不包入可由客户端任意扩展的 map。

`CapabilityGuidanceV1` 使用互斥结构：

```json
{
  "applicability": "enabled",
  "guidance": "...",
  "not_applicable_reason": ""
}
```

或：

```json
{
  "applicability": "not_applicable",
  "guidance": "",
  "not_applicable_reason": "..."
}
```

六个能力字段必须逐项存在。`enabled` 必须有 guidance，`not_applicable` 必须有 reason；两类正文不得同时存在。

### 4.2 Canonical Digest

- 先完成 UTF-8、NFC、长度、枚举、数组去重和稳定排序；
- 使用固定字段顺序的 `skill_definition.v1` Canonical JSON；
- 摘要为 SHA-256 小写十六进制；
- Digest 覆盖全部语义字段和规范化公共 Tool 引用；
- ID、时间、请求 ID、幂等键、内部 revision 和显示派生字段不进入内容摘要。

Business 与 Agent 后续共享摘要测试向量，不共享 Go 类型。

## 5. Business 持久化契约

W1 Foundation 允许新增以下 Business-owned 表，全部使用逻辑外键且禁止物理外键和数据库级联：

1. `business.skill`：聚合根、Owner、当前草稿修订、当前发布快照、治理状态、CAS version；
2. `business.skill_content_revision`：不可变结构化内容修订；已写入记录禁止 UPDATE；
3. `business.skill_review_submission`：冻结提交时的 revision、digest、审核状态和安全错误码；
4. `business.skill_published_snapshot`：不可变发布事实，引用精确内容修订并保存 publication schema/digest；
5. `business.skill_command_receipt`：保存幂等键摘要、语义摘要和安全响应；不保存原始幂等键；
6. `business.skill_governance_audit`：append-only 审核/治理审计；不保存完整 Skill 正文。

公共 Tool Definition、Tool Version、发布 Tool 引用和 `project_skill_binding` 必须在各自契约冻结后用独立前向 Migration 增加，本批不得预建语义不明的表。

### 5.1 写事务

- 创建：原子创建 Skill、首个不可变内容修订和命令回执；
- 更新草稿：按 opaque ETag/CAS 锁定聚合，追加新内容修订，再切换 current draft 指针；
- 提交审核：冻结精确内容修订和 digest；后续编辑只产生新草稿，不改变已提交内容；
- 审核通过并发布：由受信 Reviewer 应用服务在一个事务内锁定聚合、重新校验提交摘要和 Tool 资格、写发布快照、CAS 切换 current publication、更新 Review 和追加审计；
- 发布事务任一步失败时全部回滚，旧发布快照必须继续可用。

Reviewer HTTP 只允许在 [W1-C2 Reviewer RBAC 与 Skill 管理审核 v1](../business/w1-reviewer-rbac-admin-review-v1.md) 的角色 Assignment、动态 Session Resolver、事务内 account/assignment 复核、Strong ETag 和专用审计回执全部接线后注册。该批准只覆盖待审列表、冻结详情和批准发布；驳回、暂停、恢复、下架以及全部治理 HTTP 继续保持未注册。

## 6. Business HTTP v1

本批允许冻结并实现以下 Owner API：

| 方法 | 路径 | 认证与语义 |
| --- | --- | --- |
| `POST` | `/api/v1/skills` | 登录、CSRF、`Idempotency-Key`；创建草稿 |
| `GET` | `/api/v1/skills?scope=mine&cursor=...` | 登录；只返回当前 Owner 投影 |
| `GET` | `/api/v1/skills/:skill_id` | 登录；Owner-safe 404；返回草稿/当前发布摘要 |
| `PUT` | `/api/v1/skills/:skill_id/draft` | 登录、CSRF、`If-Match`；全量替换草稿 |
| `POST` | `/api/v1/skills/:skill_id/reviews` | 登录、CSRF、`Idempotency-Key`、`If-Match`；提交精确草稿审核 |

W1-C2 额外批准以下 Reviewer 管理 API，精确 DTO、cursor、幂等和错误语义以其独立设计为准：

| 方法 | 路径 | 认证与语义 |
| --- | --- | --- |
| `GET` | `/api/v1/admin/skill-reviews?status=reviewing&cursor=...` | Session + 动态 `skill.review`；待审 keyset 列表 |
| `GET` | `/api/v1/admin/skill-reviews/:review_id` | Session + 动态 `skill.review`；提交冻结定义与当前发布对照 |
| `POST` | `/api/v1/admin/skill-reviews/:review_id/decisions` | Session + 动态 `skill.review` + CSRF + Strong `If-Match` + `Idempotency-Key`；本批只接受 approved |

公开 Skill 市场和详情只有在真实发布与治理 capability 接线后注册：

- `GET /api/v1/skill-market`
- `GET /api/v1/skill-market/:skill_id`

所有响应必须使用 DTO，不返回内部 revision 列表、digest、GORM Model、权限快照或审核内部记录。Owner DTO 至少包含：

- `skill_id`、结构化当前草稿；
- `content_status`；
- `has_unpublished_changes`；
- `review_status`、安全原因与更新时间；
- `governance_status`；
- `allowed_actions[]`；
- opaque `draft_etag`。

冻结的 JSON Envelope 为：

- Create、Detail、Update：`{skill, request_id}`；
- List：`{items, next_cursor, request_id}`；
- Submit Review：`{skill, review_id, request_id}`。

`SkillDefinitionV1.schema_version` 必须为 `skill_definition.v1`。`review_status` 在尚未提交时为 `null`，否则只允许 `reviewing/approved/rejected/withdrawn`；`review_reason_code` 与 `review_updated_at` 同步可空。`governance_status` 只允许 `active/suspended/offline`。本批 `allowed_actions` 只允许按固定顺序返回 `edit_draft`、`submit_review`；无未发布修改或当前草稿正在审核时不得返回 `submit_review`。`draft_etag` 是已经带双引号的 opaque HTTP ETag，前端必须原样放入 Draft Replace 和 Submit Review 的 `If-Match`，不得解析内部 revision。Submit Review 的幂等语义绑定 `skill_id + If-Match`：首次新 Key 的 ETag 已过期时返回草稿冲突；既有同 Key、同 ETag 即使当前草稿后来变化也必须优先重放首次冻结响应；同 Key、不同 ETag 返回幂等冲突。

写 API 使用严格 JSON 解码、Body 上限和字段级错误。Submit Review Body 只允许真正的空 Body 或严格空对象 `{}`；顶层 `null`、数组、标量、未知字段和尾随 JSON 都必须在进入应用服务前拒绝。跨 Owner 的缺失与越权统一返回 `404 SKILL_NOT_FOUND`。

## 7. 幂等与错误码

Create、Submit、Review Decision 和 Governance 命令遵循：

- 相同作用域、相同 Key Digest、相同 Semantic Digest：重放原安全响应；
- 相同作用域、相同 Key Digest、不同 Semantic Digest：`409 IDEMPOTENCY_CONFLICT`；
- 并发 100 次同义请求最多产生一个业务事实；
- 原始幂等键、Cookie、CSRF、完整 Skill 正文和 Tool 凭据不得进入日志或 Evidence。

稳定错误码至少包括：

- `SKILL_INVALID_DEFINITION`
- `SKILL_NOT_FOUND`
- `SKILL_DRAFT_CONFLICT`
- `SKILL_REVIEW_CONFLICT`
- `SKILL_TOOL_REFERENCE_UNAVAILABLE`
- `SKILL_TOOL_REFERENCE_FORBIDDEN`
- `SKILL_TOOL_REFERENCE_SUSPENDED`
- `SKILL_REVIEW_CAPABILITY_REQUIRED`
- `SKILL_GOVERNANCE_CAPABILITY_REQUIRED`
- `IDEMPOTENCY_CONFLICT`
- `SKILL_PERSISTENCE_UNAVAILABLE`

字段错误使用稳定 `field/code/message`，不得返回 SQL、内部地址、堆栈或权限策略原文。

## 8. Session Skill Snapshot v2 方向

现有 `ensure_project_session.v1` 和 `snapshot_kind=empty` 语义保持不变。非空快照必须使用新的跨 Module v2 契约，禁止给 v1 请求或数据库 CHECK 静默增加第二语义。

`PublishedSkillSnapshotRefV1` 至少包含：

- `skill_id`
- `published_snapshot_id`
- `definition_schema_version`
- `content_digest`
- 内部 `publication_revision`
- `display_name`
- `invocation_rules`
- 六个结构化能力字段
- `allowed_graph_tool_keys[]`
- `public_tool_refs[]`
- `permission_snapshot_digest`
- `runtime_policy_ref`
- `governance_epoch`
- `published_at_unix_ms`

Business 必须按稳定 `load_order + skill_id` 排序并计算 `snapshot_set_digest`。Agent 在同一 Session 创建事务中保存：

- `snapshot_kind=published_refs`；
- Header 中的规范化轻量 refs，以及 Item 表中加密保存的永久可解析 Runtime Content；
- `snapshot_set_digest`；
- 冻结时间。

已有 Session 不补入任何 Skill ref，不改变 empty digest，也不跟随新发布变化；前向 expand Migration 只允许机械回填 `schema_version` 和 `skill_count=0`。旧 Session 始终读取原 empty snapshot；新 Session 读取创建时 Business 返回并由 Agent 冻结的当前发布快照。

Production Producer 已由 [Project Skill Binding 与 Session Snapshot Producer 契约 v1](./project-skill-binding-contract-v1.md) 条件冻结：Business 使用 Module 内部 typed `ResolveProjectSkillSnapshotsV1`，并在 QuickCreate v2 本地事务中冻结 Binding、Resolution 与加密 Outbox；W1 不注册 Agent→Business 当前态 Resolve RPC。正式实现仍必须遵守：

1. 上述契约中的 `project_skill_binding` 状态、排序、CAS、owner-private 权限和 Feature Flag 门禁；
2. [Agent Session Skill Snapshot v2 设计评审](../agent/session-skill-snapshot-v2-review.md)中的 Bootstrap v2 IDL、摘要向量和 unknown-outcome 查询；
3. 同一评审中的 Agent 前向/回滚 Migration 及新旧 Snapshot 兼容读取；
4. 同一 Project 第二 Session、跨 Owner/RBAC、system Skill、非空 Public Tool、计费和在线治理仍保持不可用。

## 9. 六 Tool 目录准备

Agent/BFF/前端的正式只读纵切已由 [Agent Tool Definition Catalog v1 设计评审](../agent/tool-definition-catalog-v1.md) 冻结：路径为 `GET /api/v1/agent/sessions/:session_id/tools`，内部 Scope 为 `agent.session.tools.read`，成功 Envelope 为 `tool_definition_catalog.v1`。该评审只批准静态不可用投影，不批准 Executable Registry 或 Graph 实现。

目录 exact-set 和产品顺序固定为：

1. `plan_creation_spec`
2. `analyze_materials`
3. `plan_storyboard`
4. `generate_media`
5. `write_prompts`
6. `assemble_output`

Graph 设计未通过前，目录 DTO 只能返回：

- 稳定 key、中文显示名和顺序；
- `availability=unavailable`；
- `reason_code=DESIGN_REVIEW_PENDING`；
- 不返回可执行 definition version、输入 Schema、价格或 Run 入口。

这类数据称为 `Definition Catalog Projection`，不能命名或实现为生产 `Executable Tool Registry`。前端不得用本地 Mock 把缺失项补成可执行。

## 10. 前端路由与失败关闭

冻结路由：

- `/skills`：公开市场，后端公开读接线前显示真实 unavailable/empty，不使用生产 Mock；
- `/skills/:skill_id`：公开详情；
- `/my/skills`：受保护的 Owner 列表；
- `/my/skills/new`：受保护 Builder；
- `/my/skills/:skill_id/edit`：受保护草稿编辑；
- `/skill`：兼容跳转到 `/skills`；
- Tool 目录位于 `/projects/:project_id/workspace`，不得创建脱离 Project/Session 的运行入口。

前端必须：

- 不展示版本号、版本选择或版本差异；
- 保存六个独立能力字段，不拼接成自由文本；
- 对未知状态、非法响应 Schema、目录缺项/重复/乱序失败关闭；
- 对 401 复用 Auth Session 失效流程，对 409 保留本地草稿并提示刷新；
- 不使用 LocalStorage 保存 Session Snapshot、Tool 选择或 Run 权威事实；
- 不复用历史 `/api/aigc/**` Demo API。

## 11. 本批验收与下一门禁

W1 Foundation 合并前至少验证：

- Business Migration 在 PostgreSQL 16 空库和 W0 升级路径均成功，所有表/字段有中文 COMMENT 且无物理外键；
- Skill Definition 六字段、互斥规则、Canonical Digest 和非法 `@Tool` 失败关闭；
- Owner-safe 404、CSRF、严格 JSON、ETag/CAS、并发幂等和发布失败保留旧快照；
- Agent 现有 empty Snapshot 行为完全回归；实现 v2 时另加新旧 Session 隔离契约测试；
- 前端生产路径不读取 `skillMocks.js`，不展示版本明细，状态和字段错误可恢复；
- W0.5 QuickCreate、Workspace Snapshot/SSE、跨 Owner 和 Chromium 冒烟继续通过。

进入 Graph/Runner 实现前必须同时满足：

1. 六份 Graph Tool 独立设计全部标记 Approved；
2. `aigc.contract.v1alpha1` 评审冻结为可实现版本；
3. 可执行 Registry、版本、Schema、权限、预算和计费引用完整；
4. `SMK-007/008` 的 Fixture、Run/Event/A2UI 和 Evidence 契约完成评审。
