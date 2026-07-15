# W1-F Public Market Binding v1

> 状态：Implemented / Verified
>
> 设计日期：2026-07-14
>
> 冻结日期：2026-07-14
>
> 验证日期：2026-07-14；`make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w1-smoke` 通过
>
> 覆盖范围：公开 Skill 跨发布者 QuickCreate v2、权限证明、发布者身份冻结、治理 TOCTOU、前端市场预选与登录恢复
>
> 依赖：[W1-E Skill Market 公开读取 v1](./w1-skill-market-read-v1.md)、[Project Skill Binding 与 Session Snapshot Producer 契约 v1](../cross-module/project-skill-binding-contract-v1.md)、[Agent Session Skill Snapshot v2 设计评审](../agent/session-skill-snapshot-v2-review.md)

## 1. 结论与完成定义

W1-F 不新增“安装”、收藏或已有 Project 的 Binding Replace API。它扩展现有显式 `project_quick_create.v2`：消费者从公开 Market 详情选择一个当前公开 Skill，登录后回到首页创作器，最终仍由用户显式提交 QuickCreate；Business 在同一创建事务内根据 Skill Owner 与 Project Owner 的关系选择权限 basis，并重新校验 current Published Snapshot 与治理状态。

完成必须同时满足：

1. Owner 使用自己的 Published + Active Skill 时，继续生成完全相同的 `project_skill_permission_snapshot.v1 + owner_private` Permission Canonical、digest、Snapshot digest 和 Agent request digest；
2. 消费者使用其他 Publisher 的 Published + Active Skill 时，生成 `project_skill_permission_snapshot.v2 + public_market` Permission Canonical，Project Owner/Subject 保持消费者，Skill Owner/Publisher 保持创建者；
3. 客户端仍只提交 `enabled_skill_ids`，不得提交 owner、publisher、basis、policy ref、snapshot ID、revision、digest、governance epoch 或 Runtime Policy；
4. Market 读取与 QuickCreate 之间发生 suspend/offline 或指针/Canonical 损坏时，QuickCreate 事务读取当前权威状态并失败关闭，九类创建事实零部分写入；合法再次发布则冻结提交时的新 current Snapshot 并成功，绝不沿用页面读取时的旧 Snapshot；
5. 成功创建的 Session Snapshot 冻结精确 Published Snapshot、Publisher、Permission digest 与 governance epoch；后续重新发布或治理变化不改写旧 Session；
6. Agent wire、IDL 与持久化 Schema 不变。Agent 继续只校验独立 `publisher_user_id` UUID 和 opaque permission digest，不重做 Business 权限判定；
7. 前端公开详情提供“使用此 Skill 创作”，匿名用户完成登录后恢复同一内存预选，登录用户直接回到首页创作器；只有点击“开始创作”才生成稳定 QuickCreate 意图和 Idempotency-Key；
8. 真实 PostgreSQL、Business、Agent 与 Chromium 冒烟证明跨发布者创建成功、身份没有串位、治理竞态零部分写入、旧 Session 不漂移。

## 2. 明确排除

本批不实现：

- 给已有 Project 追加、替换或移除 Skill；当前一个 Project 只有一个默认 Session，Binding Replace 路由继续不注册；
- 收藏、安装、最近使用、调用统计或 Market 点击/曝光记录；
- 团队、企业、共享 Project、代理代绑、管理员代绑或系统 namespace；
- Publisher 账户停用联动、版权许可模型、商业授权、付费、分成或退款；
- 公共 Tool、可执行 Graph Registry、Skill Invocation 或真实模型运行；
- 历史 Session Kill Switch、在线 epoch 回查或治理事件消费；
- 通过 URL、LocalStorage、SessionStorage 或 Cookie 保存可直接执行的 QuickCreate 意图；
- 把查看详情、完成登录或进入首页记作“已使用”。只有后续真实 Skill Invocation 才能形成最近使用事实。

## 3. 信任边界与权限版本闭集

### 3.1 唯一可信输入

可信输入仍只来自：

- Auth Resolver 得到的当前消费者 Principal；
- 同一 Business 事务中新建 Project 的 Owner；
- PostgreSQL 中的 Binding、Skill Owner、current Published pointer、不可变 Published Snapshot、来源 Revision 与治理状态；
- 固定常量和启动时已验证的 Snapshot limits、Runtime Policy 与本地加密器。

公开 Market DTO 只是选择提示，不是授权票据。浏览器提交的唯一 Skill 相关输入是规范小写 UUIDv7 `enabled_skill_ids`。

### 3.2 Permission schema 与 basis 闭集

`project_skill_permission_snapshot.v1` 已冻结为 owner-private，不能在同一 schema 内扩枚举。W1-F 采用两个不可交叉的版本对；本文是 Project Skill Binding 契约的 W1-F addendum，并在冻结时明确取代其 `PSB-DEC-003/PSB-BLK-001` 的“跨 Owner 永久不可用”结论，其他 owner-private、事务、Snapshot 与 Agent 契约保持有效：

```text
project_skill_permission_snapshot.v1 + owner_private
project_skill_permission_snapshot.v2 + public_market
```

决策表：

| 条件 | schema | basis | policy_ref | Publisher |
| --- | --- | --- | --- |
| `project_owner_user_id == skill_owner_user_id` | `project_skill_permission_snapshot.v1` | `owner_private` | `project-skill-permission:owner-private:v1` | Skill Owner |
| `project_owner_user_id != skill_owner_user_id` 且 Skill current published + active | `project_skill_permission_snapshot.v2` | `public_market` | `project-skill-permission:public-market:v1` | Skill Owner |
| 其他任一状态 | 无 | 无 allow proof | 无 | 整个 QuickCreate 失败关闭 |

两个 basis 的公共约束：

```text
decision = allow
subject_user_id = project_owner_user_id = 当前消费者
namespace = user
allowed_actions = ["session_snapshot"]
skill_owner_user_id = publisher_user_id = Skill 权威 Owner
published_snapshot_id = 同一事务冻结的 current Published Snapshot
```

Reviewer 的 `published_by_user_id` 只是批准审计人，绝不能作为 Publisher 或跨用户授权主体。

## 4. Public Market Permission Canonical

### 4.1 字段与校验

Canonical 字段顺序继续固定为：

```text
schema_version
decision
basis
subject_user_id
project_id
project_owner_user_id
binding_id
binding_version
binding_set_version
namespace
skill_id
skill_owner_user_id
published_snapshot_id
allowed_actions
policy_ref
```

校验器必须按 basis 做成对校验，禁止接受交叉组合：

- v1 只能搭配 `owner_private + owner-private:v1`，且 Skill Owner 等于 Project Owner；
- v2 只能搭配 `public_market + public-market:v1`，且 Skill Owner 不等于 Project Owner；
- Subject 必须始终等于 Project Owner；
- 其他字段规则、UUIDv7、版本和 action exact-set 不变。

### 4.2 Public Market Golden Vector

```json
{"schema_version":"project_skill_permission_snapshot.v2","decision":"allow","basis":"public_market","subject_user_id":"019f0000-0000-7000-8000-0000000000cd","project_id":"019f0000-0000-7000-8000-0000000000ab","project_owner_user_id":"019f0000-0000-7000-8000-0000000000cd","binding_id":"019f0000-0000-7000-8000-000000000104","binding_version":1,"binding_set_version":1,"namespace":"user","skill_id":"019f0000-0000-7000-8000-000000000101","skill_owner_user_id":"019f0000-0000-7000-8000-0000000000ef","published_snapshot_id":"019f0000-0000-7000-8000-000000000103","allowed_actions":["session_snapshot"],"policy_ref":"project-skill-permission:public-market:v1"}
```

```text
permission_snapshot_digest
  = 7c398d2febe3e22cd81d467079d61731bad9179cadaaf15f2c1223bbf9d38351
```

Owner-private 既有 golden vector `785ae395...` 必须继续逐字节不变。

## 5. Snapshot 与跨 Module 契约

### 5.1 Public Market Snapshot Vector

使用上节 Permission digest 后，metadata Canonical 为：

```json
[{"load_order":1,"priority":100,"namespace":"user","skill_id":"019f0000-0000-7000-8000-000000000101","publisher_user_id":"019f0000-0000-7000-8000-0000000000ef","published_snapshot_id":"019f0000-0000-7000-8000-000000000103","publication_revision":2,"definition_schema_version":"skill_definition.v1","content_digest":"dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513","runtime_content_schema_version":"skill_runtime_content.v1","runtime_content_digest":"d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168","allowed_graph_tool_keys":["write_prompts"],"public_tool_refs":[],"permission_snapshot_digest":"7c398d2febe3e22cd81d467079d61731bad9179cadaaf15f2c1223bbf9d38351","runtime_policy_ref":"skill-runtime-policy:v1","governance_epoch":3,"published_at_unix_ms":1784011500123}]
```

```text
snapshot_set_digest
  = 92b9eed06ade6add5828922fe9ddbc63053e1234d866c0cd189d55abf49115f4
```

对应既有 Prompt fixture 的 Ensure v2 semantic Canonical：

```json
{"schema_version":"ensure_project_session.v2","project_id":"019f0000-0000-7000-8000-0000000000ab","owner_user_id":"019f0000-0000-7000-8000-0000000000cd","creation_source":"quick_create","prompt_present":true,"prompt_digest":"273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df","skill_snapshot_schema_version":"session_skill_snapshot.v1","skill_snapshot_kind":"published_refs","skill_count":1,"skill_snapshot_digest":"92b9eed06ade6add5828922fe9ddbc63053e1234d866c0cd189d55abf49115f4"}
```

```text
ensure_project_session_v2_request_digest
  = 1352201431cd11586f5c5814827e63ed84a4a584be3556827102a63e5575485b
```

Binding selection 与 QuickCreate v2 semantic 只覆盖 Skill ID 集合和 Prompt，因此同一 fixture 继续使用既有 `selection_digest=0eafe12d...` 与 `quick_create_semantic_digest=3d2bc7c4...`。Permission basis 由事务内权威所有权关系决定，不接受客户端输入。

### 5.2 Agent 不变结论

Agent 当前契约已经：

- 独立校验 `publisher_user_id` 为规范 UUIDv7，不要求它等于 `owner_user_id`；
- 将 `permission_snapshot_digest` 视为 Business-owned opaque SHA-256，只校验格式并纳入 Snapshot digest；
- 将 Session Snapshot 写为不可变 Header/Item。

因此本批不修改 Thrift、generated Kitex、Agent Migration 或 Agent Runtime。Business 必须用现有字段发送 Skill Owner 作为 Publisher，不能为规避评审新增第二套 wire 字段。

## 6. Business 事务与 Repository

现有 QuickCreate v2 顺序保持：Receipt Claim → Project → Binding Set/Bindings/Audit → 集合读取 → 权限/Canonical/limits → Resolution → 加密 Outbox → Session Binding/Receipt。

本批只改变集合读取后的资格决策：

1. Repository 继续一次 JOIN 读取全部 enabled Binding、Project、Skill、current Published Snapshot 和 Source Revision，并使用 `FOR SHARE` 锁定 current pointer；
2. 不再要求 `skill.owner_user_id == project.owner_user_id`；改为把两者都恢复到 typed DTO；
3. 同一 SQL 逻辑关联 Publisher `user_account`，`publisher_user_id` 明确取 Publisher Account/`skill.owner_user_id`，不得取 `published.published_by_user_id`；账户状态不作为资格条件，disabled/cancelled 仍继承 W1-E 当前语义；
4. Resolver 对每一项独立选择 v1 owner-private 或 v2 public-market，混合集合允许存在两种权限版本；
5. 任一项不存在、未发布、治理非 active、指针/revision/source/digest 损坏、Tool/limits 不可用时，整个事务回滚；
6. 不复制可确定派生的 permission schema/basis/policy ref：Resolution Header 冻结 Project Owner，Item 冻结 Binding、Skill、Publisher、Snapshot、Permission digest 与 epoch；审计 verifier 以 Header Owner 与 Item Publisher 的等值关系重建 v1 owner-private 或 v2 public-market Canonical，并逐字节核对 digest；不新增语义含糊的安装表或冗余权限列。

### 6.1 错误语义

| 条件 | HTTP / code | retryable |
| --- | --- | --- |
| 不存在、未发布、指针缺行或无可用公共发布 | `409 PROJECT_SKILL_UNAVAILABLE` | false |
| Owner 自用或跨 Owner 的不存在、未发布、suspended/offline 或不可公开 | `409 PROJECT_SKILL_UNAVAILABLE` | false |
| Canonical/digest/Runtime 事实损坏 | 既有稳定 Snapshot/Runtime 错误 | 按既有契约 |
| 同 Idempotency-Key 异义 | `409 IDEMPOTENCY_CONFLICT` | false |

现有 HTTP 边界把 Skill、治理与公共 Tool 不可用统一映射为 `PROJECT_SKILL_UNAVAILABLE`；本批保持该 existence-safe 行为。错误不得暴露 Owner 邮箱、Reviewer、快照、revision、digest、账户状态或治理原因。

## 7. 治理、发布与幂等

- Market 页面展示的 DTO 可能在点击后立即过期；QuickCreate 事务永远重新读取当前 Published pointer 与 governance status；
- suspend/offline 与 QuickCreate 通过 `skill` 行锁和事务顺序串行化：QuickCreate 的集合读取使用 `FOR SHARE`，治理更新取得排他锁；先取得锁并提交的一方定义线性化顺序，治理提交返回后才开始的 QuickCreate 必须失败；
- 成功 QuickCreate 后，Session 冻结当时 Published Snapshot 与 epoch。随后 suspend/offline 或再次发布不改写旧 Session；
- 同键已成功的重放返回冻结 Project/Receipt，不重新解析最新 Skill；
- 首次事务因治理或不可用失败时 Receipt 同事务回滚，后续由用户发起的新意图使用新 Idempotency-Key；前端不得自动把稳定 409 当作技术重试；
- mixed owner/private + public market 集合任一项失败时全量回滚，禁止只丢弃跨发布者项。

## 8. 前端用户旅程

### 8.1 登录用户

```text
打开 /skills/:skill_id
→ 点击“使用此 Skill 创作”
→ 内存保存安全 Market 选择投影
→ 导航到首页创作器
→ 展示“市场 Skill”预选，可显式移除
→ 用户填写 Prompt 并点击“开始创作”
→ 冻结 prompt + sorted skill IDs + 新 Idempotency-Key
→ 提交 project_quick_create.v2
```

### 8.2 匿名用户

```text
打开公开详情
→ 点击“登录后使用”
→ 内存保存待恢复 Market 选择
→ 打开登录 Modal
→ 登录成功后导航首页并恢复预选
→ 不自动创建 Project
→ 用户显式点击开始创作后才冻结意图并提交
```

登录失败、取消或 Auth Service unavailable 均不发 QuickCreate。待恢复预选由 App 级一次性内存状态持有，只包含 `skill_id + safe display projection`，不包含 Idempotency-Key、权限、快照或治理字段；首次成功 Principal 恢复后原子消费并导航首页。登录失败只在同一 Modal 生命周期保留，取消/退出来源流程、logout、Principal 变化或 authority epoch 变化必须清空；页面刷新允许安全丢失。登录完成但用户未点“开始创作”前，QuickCreate HTTP 调用数和九类创建事实增量都必须为零。

### 8.3 Owner Picker 共存

现有 Owner Picker 继续从 Owner API 读取自己的 Skill。Market 预选以独立安全 chip 展示，并加入 Picker 清理算法的受保护 ID 集合，避免 Owner 列表中不存在该跨发布者 ID 时被误删。用户可以：

- 直接移除 Market 预选；
- 在总数不超过 16 的前提下再选择自己的 Published + Active Skill；
- 达到上限后按既有规则失败关闭。

显示名称、Publisher 和摘要只用于本次页面提示；提交仍只有 Skill ID，最终权威校验在 Business。

## 9. API、Migration 与兼容性

- HTTP 仍为 `POST /api/v1/projects:quick-create` 的显式 v2 variant；不新增 Binding API；
- 请求/响应 DTO、CSRF、Session、Idempotency-Key 与错误 Envelope 不变；
- Thrift/Kitex、Agent DTO 与 Session Snapshot Schema 不变；
- Agent PostgreSQL Schema 不变；Business 新增 metadata-only 前向 Migration `20260714000900_document_public_market_binding`；
- Up 只修正 005 中已不再准确的两个数据库 COMMENT：`publisher_user_id` 是冻结 Skill Owner，`permission_snapshot_digest` 可以来自 v1 owner-private 或 v2 public-market Canonical；不得修改 005；
- Down 必须在同一事务中先以 `SHARE MODE` 按写入顺序锁定 Resolution Header/Item 表，阻断 QuickCreate 的 `ROW EXCLUSIVE` 写入，再检查 `header.owner_user_id <> item.publisher_user_id`；存在 public-market 历史时以 SQLSTATE `55000` `RAISE EXCEPTION`，只有纯 owner-private 数据时才允许恢复旧 COMMENT，异常不得改变新注释；
- PostgreSQL 审计重建测试必须由 Header/Item 恢复 schema、basis、policy ref 和完整 Permission Canonical，并核对持久化 digest；
- 已有 owner-private 数据、digest 与 Evidence 必须不变；
- W1-E 公开 GET 仍匿名、`no-store`、白名单 DTO，不因可使用而增加权限字段或内部引用。

## 10. 测试与 Evidence

### 10.1 Business 单元与 PostgreSQL

- owner-private golden vector 逐字节回归；
- public-market Permission、Snapshot 与 Ensure request 三组 golden vector；
- basis/policy ref 交叉组合、Subject/Project Owner 错配、public-market 同 Owner、owner-private 跨 Owner 全部拒绝；
- 同一集合混合自有与市场 Skill，稳定按 Skill ID 排序并逐项选择 basis；
- Repository 证明 Publisher 取 Skill Owner 而不是 Reviewer；
- active cross-owner 成功，draft-only/不存在/指针损坏失败；
- Owner 自用与跨 Owner suspend/offline 都保持统一 existence-safe unavailable，并核对 Project、Receipt、Binding Set、Binding、Binding Audit、Resolution、Resolution Item、Session Binding、Outbox 九类事实零增量；
- 成功后再次发布/治理不改变既有 Resolution 与 Agent Snapshot；
- 100 并发同键仍只创建一套事实，同键异义仍冲突；
- SQL 数固定、无按 Skill N+1。

### 10.2 前端

- 详情登录态按钮与匿名按钮文案、回调安全投影；
- 登录成功恢复 Market 预选但不自动提交；登录失败/取消不提交；
- Market chip 展示/移除，Owner Picker 加载不会误删受保护 ID；
- Market + Owner 混合选择排序、16 项上限和重复 ID；
- 点击开始后请求 exact `project_quick_create.v2`、稳定 Idempotency-Key 与 CSRF；
- 稳定 409 进入 failed，不用原 key 自动重试；5xx/网络错误才允许同意图重试；
- 路由切换、退出和卸载取消旧请求，不跨账号保留选择。

### 10.3 真实冒烟

新增独立 `w1.skill-market-binding.smoke.evidence.v1`，精确七项布尔断言，所有值必须为 boolean `true`，不得增加或遗漏 key：

```text
public_market_quickcreate
public_market_permission_identity_separation
public_market_publisher_snapshot_frozen
public_market_governance_toctou_closed
public_market_mixed_binding_atomicity
public_market_login_preselection_recovered
public_market_idempotency_frozen_replay
```

同次 W1 运行继续固定 foundation canonical `47/42` 与 governance `5`。W1-E Market sidecar 升级为 `w1.skill-market.smoke.evidence.v2`，仍精确六项且全部为 boolean `true`：

```text
skill_market_public_read
skill_market_safe_projection
skill_market_keyset_pagination
skill_market_governance_visibility
skill_market_cursor_fail_closed
skill_market_stale_selection_fail_closed
```

前五项保持 v1 语义；第六项证明 active 详情读取后发生 suspend/offline 时，陈旧选择不能形成九类创建事实。历史 v1 artifact 保留但新运行不再产出 `v1 passed`；不得继续使用语义已经失真的 `skill_market_cross_owner_use_blocked`，也不得通过替换成 suspended fixture 伪装原 active 拒绝语义仍成立。

四份 Evidence 发布门禁固定为：

| Evidence | Schema | assertions exact-set |
| --- | --- | --- |
| Foundation | `w1.skill-foundation.smoke.evidence.v3` | 47 项，其中 5 项固定数值、42 项布尔 true |
| Governance | `w1.skill-governance.smoke.evidence.v2` | 9 项布尔 true |
| Market | `w1.skill-market.smoke.evidence.v2` | 6 项布尔 true |
| Public Market Binding | `w1.skill-market-binding.smoke.evidence.v1` | 7 项布尔 true |

四份都必须校验顶层字段和 assertions 完整闭集，共用相同 `run_id/source_digest_sha256`，全部通过后才原子发布 `passed`；三个 sidecar 不得扩容 Foundation canonical。

`public_market_governance_toctou_closed` 必须由真实数据库锁竞争直接证明：未提交的治理写持有排他行锁时，QuickCreate 的共享锁必须等待；治理提交后请求观察新状态、返回稳定 409 且九类事实零增量。顺序型“active 详情读取 → suspend 已提交 → 陈旧选择提交”继续由 Market v2 的 `skill_market_stale_selection_fail_closed` 证明，二者不得相互冒充。

`public_market_mixed_binding_atomicity` 必须使用同一 Consumer 的真实 Published+Active owner-private Skill 和另一 Publisher 的真实 public-market Skill：有效集合成功冻结 v1/v2 两种 Permission；public-market 项治理失效后，同两个真实 ID 的新意图整体失败且九类事实零增量。

真实 `@w1-real-review` 与首个 shell checkpoint coordinator 共用 180 秒预算；coordinator 不得施加更短的 30 秒截止，并且只在 Playwright PID 退出、checkpoint 契约错误或预算耗尽时失败。`before_login`、`before_submit` 各精确出现一次，checkpoint/ACK 原子写入且权限为 `0600`。

真实链路至少执行：

1. Creator 创建并由 Reviewer 发布 Skill；
2. Consumer 匿名打开 Market 详情，点击使用并登录；
3. 登录后首页恢复同一 Skill 预选但数据库无 Project 新增；
4. Consumer 显式提交 QuickCreate v2 并进入 ready Workspace；
5. 核对 Project/Subject=Consumer、Publisher/Skill Owner=Creator、public-market permission digest 与 Business/Agent Snapshot digest；
6. Creator 修改 Draft 或再次发布后，旧 Consumer Session Snapshot 不变；
7. 重新读取 active 详情后由 Governor suspend，再用陈旧选择提交，精确失败且全事实计数不变；
8. resume 后新意图成功，offline 后新意图失败；
9. Evidence 扫描不得包含 Cookie、CSRF、密码、邮箱、Prompt、Definition、Snapshot 明文、幂等原文或治理原因。

## 11. 发布、回滚与观测

门禁与发布顺序：

```text
USR-SKILL-006 / SRV-SKILL-004 / SMK-006B 需求与追踪矩阵
→ Project Skill Binding cross-module addendum
→ 本文 Frozen / Approved
→ Business 双版本 Permission Canonical + Migration + Repository/Resolver
→ Business/Agent 契约回归与真实 PostgreSQL
→ 前端 Market 预选与登录恢复
→ Chromium + 新 Evidence sidecar
→ 更新实现事实与验收状态
```

本批需要先执行 009 COMMENT Migration。部署先上兼容的 Business，再上前端 CTA。回滚必须先隐藏 CTA，并停止、drain 所有可产生 QuickCreate v2 Resolution 的后端写入；随后执行 009 Down，只有 Down 成功后才允许回滚 Business 扩权。Down 会在同一事务获取 Header/Item `SHARE` 表锁并重新检查历史，因此外部预检查只用于运维提示，不是安全边界；SQLSTATE `55000` 表示存在 public-market 历史，必须中止后续 binary 回滚。W1-F 不新增专用 Feature Flag，运维可使用既有 Project Binding v2 总门禁或流量摘除完成停止写入；后端先行是向后兼容的能力开放，前端后行避免 CTA 早于服务端。

日志只允许 request/project/session/skill/publisher 的 ID、basis 枚举、结果与稳定错误码；禁止记录 Permission Canonical、digest 全文、Prompt、Market DTO、Cookie、CSRF 或底层 SQL。

## 12. 评审清单

- [x] Permission schema、basis 与 policy ref 成对且为闭集；
- [x] Owner-private golden vector 完全不变；
- [x] Project Owner/Subject 与 Skill Owner/Publisher 明确分离；
- [x] Reviewer 不会被投影成 Publisher；
- [x] 浏览器不能提交权限、快照、治理或 Runtime 可信字段；
- [x] Governance TOCTOU 在 QuickCreate 同一事务重新验证；
- [x] 任一 Skill 失败时整个 mixed 集合零部分写入；
- [x] 旧 Session 不因治理或再次发布被改写；
- [x] Agent wire、IDL、Schema 与 opaque permission 语义不变；Business Header/Item 可重建并核验权限审计；
- [x] 登录恢复不自动创建 Project、不跨账号保存意图；
- [x] “使用”未被误记为收藏、安装或最近调用；
- [x] Evidence 版本不会继续宣称已经被新能力取代的跨 Owner 拒绝事实。
