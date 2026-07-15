# W2 Smoke Context 与 Scenario Registry 契约 v1

> 状态：Draft / awaiting owner approval
>
> 契约版本：`w2.smoke.context_registry.v1`
>
> 治理 Gate：`W2-S0-G0`
>
> 更新日期：2026-07-15
>
> 实现状态：`implementation_unlocked=false`
>
> 信任根状态：`candidate_unactivated`
>
> 关联文档：[W2-ADR-009](../cross-module/w2-adr-009-structured-smoke-harness-v1.md)、[全功能冒烟工程设计](full-function-smoke-engineering-design.md)、[SMK-004A shadow baseline](approvals/w2-s0-g0/smk-004a-shadow-baseline-v1.json)、[W2-S0-G0 审批清单](approvals/w2-s0-g0/approval-manifest.json)

## 1. 目的与规范用语

本文冻结 W2-S0 候选 Harness 的 Shell→Harness Run Context、Scenario Registry、typed step、Evidence 和安全校验边界，使后续实现可以由机器拒绝未知字段、断言漂移、目录逃逸、生产环境误连和 canonical 状态膨胀。

本文中的 `MUST` 是获批后实现的强制要求，`SHOULD` 是默认要求，偏离时必须记录风险与替代保障。当前文档仍是 Draft；`MUST` 不代表相关代码已经存在或已经批准。

## 2. 契约对象与版本

| 对象 | `schema_version` | Owner | 生命周期 |
| --- | --- | --- | --- |
| Run Context | `w2_smoke_run_context.v1` | Shell Orchestrator 生成，Harness 只读 | 单次 Attempt；结束即删除 |
| Authority Projection Index | `w2_smoke_authority_projection_index.v1` | 各 Module 提供事实，Shell 当前汇总脱敏 | 单次 Attempt；进入 Evidence 前重验 |
| Scenario Registry | `w2_smoke_scenario_registry.v1` | Test Owner | 随代码版本化、严格解析 |
| Scenario | `w2_smoke_scenario.v1` | 场景 Owner | 随 Registry 版本化 |
| Attempt Manifest | `w2_smoke_attempt_manifest.v1` | Harness | `pending -> passed|failed`，发布后不可变 |
| Shadow Baseline | `w2_s0_g0_smk_004a_shadow_baseline.v1` | Integration/Test Owner 候选 | Gate 绑定，修改需重审 |

任何未知 major 版本必须失败关闭。兼容新增不得依赖宽松未知字段；需要扩展时发布新 Schema，并提供旧 Context/Registry 的显式读取或失效策略。

## 3. 通用编码规则

### 3.1 JSON/YAML 严格性

- UTF-8，无 BOM；
- JSON 顶层必须是单个对象，拒绝重复 Key、未知字段、尾随值、`NaN`/`Infinity`；
- YAML 只允许 JSON-compatible scalar、array、object，拒绝重复 Key、Anchor、Alias、自定义 Tag、Merge Key 和多文档；
- ID、枚举和路径先做严格语法校验，不做隐式大小写、Unicode 或路径归一化；
- 集合在 Schema 指定排序时必须已经排序，Validator 不得静默排序后接受；
- 摘要统一为 `sha256:<64 lowercase hex>`，覆盖原始文件字节；
- 时间统一为 UTC RFC3339，运行期 ID 使用 UUIDv7；
- 行为重要的整数必须位于 JavaScript safe integer 范围，禁止浮点表达计数、版本或序号。

### 3.2 仓库路径

仓库路径统一使用 `/`，必须是规范化相对路径。禁止：

- 空路径、绝对路径、反斜杠、`.`、`..`、重复分隔符；
- NUL、控制字符和非 UTF-8；
- 符号链接、Git submodule、特殊文件和目录外跳转；
- 运行时把 Registry 中的字符串解释成任意模块、脚本或命令路径。

Git 绑定的文档/JSON 必须是 `100644 blob`；现有可执行 Shell 的 source ref 可以是显式冻结的 `100755 blob`。

## 4. Run Context v1

### 4.1 文件与权限

- 由受信 Shell 在本次 Run 私有目录内原子写入；
- 私有目录权限 `0700`，Context 文件权限 `0600`；
- 打开时使用 no-follow 语义并核对 owner、mode、普通文件、link count 和最终实路径仍位于 run root；
- Context 和 credential ref 目标不得位于仓库、Artifact 输出目录或系统公共临时目录的共享位置；
- Harness 读取后不得复制原文；正常、失败、取消和超时退出都必须删除；
- 删除失败、权限过宽、owner 不符或文件在读取前后发生替换，Attempt 必须失败。

### 4.2 顶层字段

获批后的 `w2_smoke_run_context.v1` 至少固定以下字段；实现不得用无类型扩展 Map 绕过 Schema：

| 字段 | 类型 | 规则 |
| --- | --- | --- |
| `schema_version` | string | 精确为 `w2_smoke_run_context.v1` |
| `smoke_run_id` | UUIDv7 string | 同一 closure run 稳定 |
| `attempt_id` | UUIDv7 string | 每次尝试唯一；重试不得复用 |
| `fixture_namespace` | string | 仅含小写 ASCII、数字、`-`；不可复用 |
| `environment` | enum | 首切只允许 `local_deterministic` |
| `source_digest_sha256` | digest | 绑定本次被测 source closure |
| `endpoints` | ordered array | 只允许已登记 service 与 loopback URL |
| `resource_refs` | ordered array | 已公开资源 ID；不得含业务正文 |
| `runtime_bindings` | ordered array | component、binary/build digest |
| `migration_bindings` | ordered array | Module 与 migration head |
| `version_bindings` | ordered array | Definition/Graph/Adapter/Freeze 等版本 |
| `credential_refs` | ordered array | 一次性凭据文件引用，不含凭据原文 |
| `authority_projection_refs` | ordered array | Shell 产出的脱敏只读投影引用 |
| `deadline_policy_ref` | string | 必须来自编译期 Policy Registry |
| `created_at` | UTC RFC3339 | 由 Shell Clock 产生 |
| `expires_at` | UTC RFC3339 | 严格晚于 `created_at`，过期即拒绝 |

### 4.3 Endpoint

Endpoint 元素字段固定为：

| 字段 | 规则 |
| --- | --- |
| `service` | `business_http`、`frontend_http` 或后续 Schema 明确增加的白名单值 |
| `url` | `http://127.0.0.1:<port>` 或 `http://[::1]:<port>`；禁止 hostname、userinfo、query、fragment |
| `health_path` | 固定白名单路径；不能由 Scenario 自由指定 |

首切 Harness 只通过 Business BFF 和真实 Frontend 访问 Agent 数据；不得拿到 Agent 内部身份断言、Agent 直连签名或数据库 DSN。`agent_direct_access_denied` 继续由既有权威路径验证，而不是向 Harness 发放更高权限。

### 4.4 Resource ref

Resource ref 是 `{kind,id}` 的严格对象：

- `kind` 首切只允许 `project`、`session`、`input`、`user_fixture`；
- `id` 对资源使用 UUID；用户 fixture 可以使用 Registry 中的稳定 fixture key；
- 不得包含 Prompt、Message、邮箱、Cookie、CSRF、内部签名、数据库行快照或临时 URL；
- 同一 `kind` 是否允许多个值由 profile Schema 明确规定，禁止 last-write-wins。

### 4.5 Runtime、Migration 与版本绑定

`runtime_bindings` 元素为 `{component,sha256}`，component 白名单为：

- `business_service`；
- `agent_service`；
- `business_worker`；
- `frontend_bundle`。

profile 必须声明 required component exact-set。SMK-004A API 首切不因未来场景需要而虚构 Worker 已运行；UI profile 必须额外绑定真实 frontend bundle。

`migration_bindings` 元素为 `{module,head}`，module 只允许 `business`、`agent`、`worker`；是否必需由 profile exact-set 决定。`head` 必须是可验证的版本化 Migration 标识，不能只写“latest”。

`version_bindings` 元素为 `{kind,key,version,digest_sha256}`。`kind` 白名单至少预留 `definition`、`graph`、`adapter`、`freeze`、`evidence_schema`；未适用项应从 required exact-set 中明确排除，不能写空值冒充绑定。

### 4.6 Credential ref

Credential ref 元素为：

```json
{
  "kind": "business_session_cookie",
  "path": "<run-private-relative-path>",
  "sha256": "sha256:<64-lowercase-hex>",
  "expires_at": "<UTC-RFC3339>"
}
```

规则：

- `kind` 必须来自 profile 的最小白名单；
- `path` 必须位于 run root，目标为 `0600` 普通文件且禁止 symlink；
- 摘要只用于检测替换，不得进入最终 Evidence；
- Credential 内容只能由对应 Driver 在内存中读取，不写日志、timeline、trace attachment 或失败消息；
- Context、credential 和浏览器 storage state 均必须在退出时删除；
- Scenario 无权请求额外 credential kind。

### 4.7 Authority projection ref

在专用只读 Evidence Role 建立前，首切使用：

```json
{
  "module": "agent",
  "projection_kind": "workspace_transport_redacted",
  "path": "<run-private-relative-path>",
  "sha256": "sha256:<64-lowercase-hex>",
  "schema_version": "<strict-projection-schema>",
  "produced_at": "<UTC-RFC3339>"
}
```

- Projection 由当前受信 Shell 调用现有权威查询后脱敏生成；
- Harness 只读，不能拥有任何数据库 DSN 或写权限；
- Projection Schema 必须逐字段白名单，不包含完整 Prompt/Message、Secret、签名、SQL 或无关个人信息；
- Harness 必须重验 digest、mode、path containment、Run/Attempt 绑定和过期时间；
- 后续改为 Module-owned read-only Evidence API/Role 时必须发布版本化迁移，不能静默替换该信任边界。

## 5. Scenario Registry v1

### 5.1 Registry 顶层

`w2_smoke_scenario_registry.v1` 顶层字段固定为：

| 字段 | 类型 | 规则 |
| --- | --- | --- |
| `schema_version` | string | 精确版本 |
| `registry_version` | string | 语义版本，不允许 mutable `latest` |
| `source_digest_sha256` | digest | 绑定所有 Scenario、Registry 和 Schema 输入 |
| `scenarios` | ordered array | 按 `slice_id` 排序、ID exact-set 唯一 |

Registry 不接受任意 metadata Map、动态 plugin、模块路径、环境变量插值或 CLI 字符串。

### 5.2 Scenario 字段

每个 `w2_smoke_scenario.v1` 固定包含：

| 字段 | 规则 |
| --- | --- |
| `slice_id` | `SMK-NNN` 或已登记 derived slice ID |
| `canonical_smoke_id` | 精确一个 `SMK-NNN` |
| `scenario_kind` | `canonical` 或 `derived_slice` |
| `requirement_ids` | 排序 exact-set；允许为空时必须有明确理由字段，首切不为空 |
| `required_slice_ids` | 排序 exact-set、无环、无自身引用 |
| `owner_role` | Registry 已知 role；不授予仓库权限 |
| `baseline_ref` | 可选；使用时绑定路径、Schema 和 SHA |
| `profiles` | 至少一个，profile key 唯一且排序 |

规则：

- canonical 的 `canonical_smoke_id` 必须等于自身 ID；
- derived slice 只能映射一个 canonical，且 canonical 不得引用自身；
- `required_slice_ids` 必须在全 Registry 上验证存在性、无环和无重复；
- Gate reopened 或任何 required Evidence stale 时，closure 必须 stale；
- 不允许根据文件名猜测 scenario kind 或状态贡献。

### 5.3 Profile 字段

Profile 固定包含：

| 字段 | 规则 |
| --- | --- |
| `profile` | 稳定小写 key；首切为 `api`、`ui` |
| `mode` | 首切精确为 `shadow_parity` |
| `contributes_to_status` | 首切精确为 `false` |
| `required_context_fields` | 编译期已知 field key exact-set |
| `required_runtime_components` | component exact-set |
| `required_migration_modules` | Module exact-set |
| `steps` | 有序 typed step 数组 |
| `assertion_ids` | 排序 exact-set，必须与 baseline 相同 |
| `evidence_collectors` | 排序白名单 key exact-set |
| `deadline_policy_ref` | 编译期 Policy Registry key |

`contributes_to_status=false` 的 profile 即使通过，也只能生成 shadow 结果。Harness 不得通过 CLI flag、环境变量、重试或 Evidence 后处理把它提升为 canonical contribution。

### 5.4 Typed step

Step 是严格 tagged union：

| `step_type` | 必填 payload | 允许职责 |
| --- | --- | --- |
| `driver` | `driver_key`、严格参数 DTO | 正式 API、真实 UI 或受控基础设施动作 |
| `assertion` | `assertion_id`、严格输入 ref | 对公开结果或脱敏权威投影断言 |
| `wait` | `policy_ref`、`observation_key` | 轮询公开 Read API/只读投影 |
| `collect` | `collector_key` | 采集白名单脱敏 Evidence |

所有 `driver_key`、`assertion_id`、`observation_key`、`collector_key` 和 `policy_ref` 必须在编译期 Registry 注册并具有唯一 Owner。未知 key 失败关闭。

Scenario/Step 中明确禁止：

- Shell、SQL、JavaScript/TypeScript 源码或动态表达式；
- 任意模块/文件路径加载；
- 任意 HTTP method、Header 或 URL；
- 直接设置成功状态、写数据库、写 Redis/etcd；
- 从旧 Evidence 复制 assertion boolean；
- 固定 `sleep` 代替权威状态等待；
- 在参数中携带 Cookie、CSRF、Token、Prompt、Message 或 Secret。

### 5.5 Assertion 与 canonical closure

- 每个 assertion ID 在 Registry 中只有一个稳定语义、输入 Schema 和 Owner；
- profile 的 assertion IDs 必须排序、无重复、与绑定 baseline exact-set 一致；
- API/UI profile 不得重复认领同一 assertion，除非新版本契约显式定义合并语义；首切 26 项互不重叠；
- assertion 只能由本次 Attempt 的观察与 projection 计算，不得硬编码 `true`；
- `skipped`、`missing`、`stale`、未知或非 boolean 结果均不算通过；
- 数值断言保留类型和值，例如 `concurrent_requests=100`，不能压成 boolean；
- canonical closure 只能在 required slice 与 version binding exact-set 完整时产生。

## 6. SMK-004A 首切 exact-set

机器基线是未来 Harness runtime 的唯一 exact-set 来源，本文只解释语义；Harness 必须读取并验证该基线，不能在运行代码中维护另一套可漂移列表。未激活的治理校验器可以独立固定同一 reviewed exact-set，作为拒绝 baseline 被单方篡改的 anti-tamper 候选；它不是 Harness 运行时的第二业务真源。

### 6.1 API shadow profile

固定 14 项：

1. `agent_direct_access_denied`
2. `agent_restart_hit`
3. `events_cross_owner_not_found`
4. `retention_old_events_pruned`
5. `retention_server_cursor_expired_reset`
6. `retention_window_advanced`
7. `snapshot_after_restart`
8. `sse_after_restart`
9. `sse_cursor_reset`
10. `sse_replay_and_ready`
11. `workspace_cross_owner_not_found`
12. `workspace_empty_arrays`
13. `workspace_owner_safe_not_found`
14. `workspace_snapshot`

### 6.2 UI shadow profile

固定 12 项：

1. `browser_controlled_disconnect`
2. `browser_cross_owner_agent_blocked`
3. `browser_cross_owner_not_found`
4. `browser_resource_facts_not_disclosed`
5. `browser_retention_no_stale_event_replayed`
6. `browser_retention_reset_received`
7. `browser_retention_reset_without_id`
8. `browser_retention_same_session_recovery`
9. `browser_retention_snapshot_reloaded`
10. `browser_retention_snapshot_retained`
11. `browser_same_session_recovery`
12. `browser_ui`

### 6.3 Canonical-only

固定 8 项，不得由两个 shadow profile 贡献：

1. `agent_unique_facts`
2. `blank_negative_side_effects`
3. `business_prompt_cleared`
4. `concurrent_requests`
5. `idempotency_conflict`
6. `idempotent_replay`
7. `logout_revoked`
8. `logout_workspace_denied`

上述 26 + 8 恰好对应当前 W0.5 canonical Evidence v3 的 34 项断言边界，但不表示首切已执行或已覆盖 canonical closure。

## 7. Evidence v1

### 7.1 Attempt 状态机

```text
pending -> passed
pending -> failed
```

`passed`、`failed` 为 append-only terminal。重试必须创建新 `attempt_id` 和新 fixture namespace，并通过 `previous_attempt_id` 关联；不得修改或删除第一次失败 manifest。

### 7.2 Attempt Manifest 必备字段

| 字段 | 规则 |
| --- | --- |
| `schema_version` | `w2_smoke_attempt_manifest.v1` |
| `smoke_run_id` / `attempt_id` | 与 Context 一致 |
| `slice_id` / `canonical_smoke_id` / `profile` | 与 Registry 一致 |
| `mode` / `contributes_to_status` | 与 baseline 一致 |
| `status` | `pending`、`passed`、`failed` |
| `source_digest_sha256` | 与 Context/Registry 一致 |
| `runtime_bindings` / `migration_bindings` / `version_bindings` | exact-set |
| `assertion_results` | 与 profile assertion exact-set 一致 |
| `file_checksums` | 所有发布文件 path/SHA exact-set |
| `started_at` / `finished_at` | UTC 时间 |
| `previous_attempt_id` | 首次为空；重试必填 |
| `failure_code` | failed 必填，passed 禁止 |

### 7.3 发布过程

1. 在权限 `0700` 的 Attempt 临时目录生成 `pending`；
2. 关闭仍会追加的 Runtime/Driver 输出；
3. 删除 Context、credential、browser storage state、完整 Prompt/Message 和未扫描原始日志；
4. 对剩余文件执行 Secret/内容扫描；
5. 计算逐文件 SHA 和 exact-set；
6. 写入 terminal manifest；
7. fsync 文件与目录；
8. 同文件系统原子 rename 到 Artifact 目录；
9. 任何一步失败都不得发布 passed；仅已完成脱敏闭环的 failed bundle 可保留。

Context、credential digest、Cookie、CSRF、Token、密码、完整 Prompt/Message、内部身份断言、数据库 DSN 和签名 URL不得出现在 terminal Evidence。

### 7.4 Stale

以下任一变化使旧 Evidence 不可用于 closure：

- source digest、Scenario/Registry/Schema 或 assertion exact-set；
- Runtime binary 或 frontend bundle digest；
- Migration head；
- Definition、Graph、Adapter、Freeze 或 Evidence Schema version/digest；
- required Gate reopened；
- baseline source path、Git mode、line count 或 raw SHA 漂移。

stale 是 closure 计算结果，不得改写历史 Attempt 的 terminal status。

## 8. 安全与 Module 边界

- Harness 不是生产 Runtime，不注册 etcd，不提供生产 HTTP/RPC；
- `business/`、`agent/`、`worker/` 各自拥有数据与 Migration，Harness 不 import 任何 Go `internal` 包；
- Harness 不持有跨数据库写账号，不直写 PostgreSQL、Redis 或 etcd；
- 当前首切只消费 Shell 脱敏投影，不能把 `dora_admin` 的临时存在描述成 Harness 权限；
- 生产配置、生产域名、非 loopback endpoint、真实用户账号或共享 Secret 一经发现立即失败；
- Fixture/Adapter 控制面后续只能在 local-smoke profile、私有网络和一次性 Run Token 下存在，生产构建不注册；
- 日志和 Evidence 禁止完整 Prompt、Message、素材、Provider Payload、Cookie、Token、签名 URL 与个人敏感信息；
- 高基数资源 ID可以进入受限 Evidence，但不得成为 Metric Label；
- UI 必须走真实请求，禁止 route interception、mock response、LocalStorage 权威状态和 capability 注入。

## 9. 失败关闭测试矩阵

后续 Harness Kernel 至少必须证明以下输入被拒绝：

| 类别 | 最小反例 |
| --- | --- |
| Context | mode 不是 0600、symlink、目录逃逸、过期、production hostname、未知 endpoint |
| JSON/YAML | 重复 Key、未知字段、尾随值、Anchor/Alias、Merge Key、多文档 |
| Registry | 重复 slice/profile/assertion、缺 baseline、derived 多 canonical、required slice 环 |
| Step | 未知 Driver/Assertion/Collector、内嵌 SQL/Shell、动态模块路径、任意 Header |
| Canonical | 把 shadow `contributes_to_status` 改为 true、把 canonical-only 断言塞入 shadow |
| Source | path、Git mode、line count、raw SHA 任一漂移 |
| Evidence | assertion 缺失/skipped/stale、第一次失败被覆盖、未扫描文件、checksum 漂移 |
| Secret | Context/Cookie/CSRF/Token/Prompt/DSN 出现在 terminal bundle |

## 10. 审批与实现门禁

本契约、W2-ADR-009 和 shadow baseline 的原始字节 SHA 由 `w2_s0_g0_approval_manifest.v1` 绑定。当前清单固定：

- `status=awaiting_owner_approval`；
- `implementation_unlocked=false`；
- `trust_root_status=candidate_unactivated`；
- 七方 `required_owner_roles` exact-set；
- 十项排序 `activation_blockers` exact-set：
  - `BASE_OWNED_WORKFLOW_NOT_ACTIVE`；
  - `FORK_CANARY_NOT_PASSED`；
  - `OWNER_AUTHORITY_NOT_ACTIVE`；
  - `RULESET_SOURCE_AND_NO_BYPASS_NOT_PROVEN`；
  - `SAME_REPO_CANARY_NOT_PASSED`；
  - `SEMANTIC_PATH_POLICY_NOT_ACTIVE`；
  - `TRUST_ROOT_REKEY_HANDOFF_NOT_FROZEN`；
  - `TRUST_ROOT_RELEASE_NOT_INSTALLED`；
  - `VALIDATOR_BUILD_CLOSURE_NOT_FROZEN`；
  - `WORKFLOW_DIGEST_AND_ACTION_SHA_NOT_FROZEN`；
- `smoke/**`、`test-adapters/**`、`deploy/local-smoke/**` 禁止出现。

三个禁止根是 canonical-path lock，不是全仓语义分类器；其他路径的 Harness 规避仍由 `SEMANTIC_PATH_POLICY_NOT_ACTIVE` 阻断正式激活。候选 Git-object 校验还必须拒绝 `.github/smoke-governance/**` 以及 `.github/workflows/w2-smoke-governance*.yml|yaml` 提前出现。

当前批次不创建 workflow、release、active pointer 或 Ruleset，本地 `go test` 与手动 Git-object 入口都不能声称远程 required check 已生效。正式激活必须先冻结 base-owned workflow 原始摘要、Action 全长 commit SHA、Validator 源码/构建输入 exact-set 和 generation handoff，再由现有外部治理合并 Bootstrap PR，配置 source-locked/no-bypass Ruleset 并完成同仓/fork canary。

正式批准不能通过修改本 Draft、自报 Owner 或填入普通 URL完成。当前 v1 清单不提供任何可自报的 approval field；必须先建立受信 Owner identity/review authority，再由后续版本化治理迁移绑定 authority request，由 App 在最终 head/merge SHA 核验七个不同受信 actor。Unlock PR 不得同时带入 Harness 实现。

## 11. 当前状态

本文是 **Draft / awaiting owner approval / candidate_unactivated**。它没有创建 Context、Registry、Harness、Evidence Role 或 CI 作业，没有 workflow/Ruleset/required check，没有执行 SMK-004A shadow parity，也没有更改 `SMK-004`、W0.5 canonical Evidence 或任何生产能力状态。
