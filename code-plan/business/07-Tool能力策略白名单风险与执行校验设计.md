# 07-Tool能力策略白名单风险与执行校验设计

状态：archived
owner：业务服务责任域
更新时间：2026-06-28
适用范围：平台 Tool 定义、Tool 策略、白名单、风险等级、超时重试取消策略、Agent 执行前校验  
相关代码路径：`services/business/internal/application/toolpolicy/**`、`services/business/internal/domain/toolpolicy/**`

## 目标

业务服务保存平台开放 Tool 的业务策略，Agent 执行 Tool 前必须调用业务服务校验可用性、风险等级、超时、重试、取消和是否需要人工确认。

## 非目标

- 不在业务服务执行 Tool 逻辑，不保存 Agent Tool 原始执行 payload。
- 不提供用户自定义外部 Tool 接入。
- 不允许 Agent 绕过 `CheckToolExecutionPolicy` 执行高风险或 disabled Tool。

## 需求映射矩阵

| 产品条目 | 业务解释 | 业务产出 | 【Agent开发】依赖 |
| --- | --- | --- | --- |
| 平台开放 Tool | 只有 enabled 且满足白名单的 Tool 可绑定和执行 | `tool_definitions`、`tool_policies`、`tool_whitelist_rules` | Tool 执行前调用 `CheckToolExecutionPolicy` |
| 高风险确认 | 高风险 Tool 返回 `requires_confirmation=true` | `ToolExecutionPolicyResponse` | Agent 创建 interrupt 并等待用户确认 |
| Tool 计价策略 | 独立计费 Tool 由业务返回 charge mode 和 pricing policy | `tool_pricing_policies` | Agent 预估/扣费只传数量，不传单价 |
| 后台 Tool 管理 | 管理端启停、策略、白名单和计价变更 | `/api/admin/tools/**`、`tool_policy_change_records` | Agent 按短 TTL 或缓存失效读取新策略 |

## 数据库表

| 表 | 字段 | 索引和约束 |
| --- | --- | --- |
| `tool_definitions` | `tool_id`、`tool_key`、`tool_type`、`display_name`、`status` | `tool_key` 唯一 |
| `tool_policies` | `tool_id`、`risk_level`、`timeout_ms`、`retry_policy`、`cancel_policy`、`scope_type` | `tool_id` 索引 |
| `tool_pricing_policies` | `pricing_policy_id`、`tool_id`、`charge_mode`、`billing_unit`、`unit_points`、`status` | `(tool_id,status,effective_at)` |
| `tool_whitelist_rules` | `rule_id`、`tool_id`、`target_type`、`target_id`、`space_type`、`status` | `(tool_id,target_type,target_id)` |

## 详细数据库表设计

### `tool_definitions`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `tool_id` | varchar(64) | 是 | 生成 | pk/unique | Tool ID |
| `tool_key` | varchar(120) | 是 |  | unique | 稳定 Tool key |
| `tool_type` | varchar(32) | 是 |  | idx | `generation`、`asset`、`business`、`utility` |
| `display_name` | varchar(120) | 是 |  | idx | 前端展示名称 |
| `description` | varchar(512) | 否 | null |  | Tool 说明 |
| `status` | varchar(32) | 是 | `enabled` | idx | `enabled`、`disabled` |
| `builtin` | boolean | 是 | true |  | 生产范围只允许内置 Tool；开放外部 Tool 必须先更新安全、白名单、计费和审计契约 |
| `created_by` | varchar(64) | 否 | null | idx | 管理员 |
| `updated_by` | varchar(64) | 否 | null | idx | 管理员 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

### `tool_policies`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `policy_id` | varchar(64) | 是 | 生成 | pk/unique | 策略 ID |
| `tool_id` | varchar(64) | 是 |  | unique | Tool ID |
| `risk_level` | varchar(32) | 是 | `low` | idx | `low`、`medium`、`high` |
| `requires_confirmation` | boolean | 是 | false | idx | 是否需要人工确认 |
| `timeout_ms` | int | 是 | 30000 |  | Tool 执行超时 |
| `retry_policy` | jsonb | 是 | `{}` |  | `max_attempts`、`backoff_ms`、`retryable_errors[]` |
| `cancel_policy` | jsonb | 是 | `{}` |  | `cancelable`、`keep_completed_items` |
| `scope_type` | varchar(32) | 是 | `all` | idx | `all`、`personal`、`enterprise`、`system` |
| `updated_by` | varchar(64) | 是 |  | idx | 管理员 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

业务规则：`risk_level=high` 时 `requires_confirmation` 必须为 true；非幂等 business Tool 的 `retry_policy.max_attempts` 必须为 0。

### `tool_pricing_policies`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `pricing_policy_id` | varchar(64) | 是 | 生成 | pk/unique | Tool 计价策略 ID |
| `tool_id` | varchar(64) | 是 |  | idx composite | Tool ID |
| `charge_mode` | varchar(32) | 是 | `no_charge` | idx | `no_charge`、`model_generation`、`tool_usage`、`business_value` |
| `billing_unit` | varchar(32) | 否 | null |  | `call`、`item`、`second`、`page`、`mb`、`token`、`asset` |
| `unit_points` | numeric(18,6) | 是 | 0 |  | 每单位积分 |
| `min_points` | bigint | 是 | 0 |  | 最小扣费积分 |
| `max_points` | bigint | 否 | null |  | 单次最大扣费上限 |
| `free_quota` | numeric(18,6) | 是 | 0 |  | 单次免费额度 |
| `rounding_mode` | varchar(32) | 是 | `ceil` |  | `ceil`、`floor`、`round` |
| `effective_at` | timestamptz | 是 | now() | idx composite | 生效时间 |
| `status` | varchar(32) | 是 | `active` | idx composite | `active`、`disabled` |
| `created_by` | varchar(64) | 是 |  | idx | 管理员 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

同一 Tool 同一时间只能有一个 active 计价策略，由 application 校验。`model_generation` 表示该 Tool 的用户扣费由模型价格快照承担，不再叠加 Tool 自身调用费。

### `tool_whitelist_rules`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `rule_id` | varchar(64) | 是 | 生成 | pk/unique | 白名单规则 ID |
| `tool_id` | varchar(64) | 是 |  | unique composite/idx | Tool ID |
| `target_type` | varchar(32) | 是 |  | unique composite/idx | `space`、`enterprise`、`user`、`skill`、`plan` |
| `target_id` | varchar(64) | 是 |  | unique composite/idx | 目标 ID，通配用 `*` |
| `space_type` | varchar(32) | 否 | null | idx | `personal`、`enterprise` |
| `status` | varchar(32) | 是 | `active` | idx | `active`、`disabled` |
| `reason` | varchar(512) | 否 | null |  | 后台原因摘要 |
| `created_by` | varchar(64) | 是 |  | idx | 管理员 |
| `updated_by` | varchar(64) | 是 |  | idx | 管理员 |
| `created_at` | timestamptz | 是 | now() | idx | 创建时间 |
| `updated_at` | timestamptz | 是 | now() |  | 更新时间 |

唯一约束：`(tool_id,target_type,target_id,space_type)`。Agent 校验时按精确目标优先，再匹配 `target_id='*'`。

### `tool_policy_change_records`

| 字段 | 类型 | 必填 | 默认值 | 索引/约束 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `change_id` | varchar(64) | 是 | 生成 | pk/unique | 变更记录 ID |
| `tool_id` | varchar(64) | 是 |  | idx | Tool ID |
| `change_type` | varchar(32) | 是 |  | idx | `policy`、`pricing`、`status`、`whitelist` |
| `before_snapshot` | jsonb | 是 | `{}` |  | 变更前摘要 |
| `after_snapshot` | jsonb | 是 | `{}` |  | 变更后摘要 |
| `affected_skill_count` | int | 是 | 0 |  | 影响 Skill 数 |
| `changed_by` | varchar(64) | 是 |  | idx | 管理员 |
| `reason` | varchar(512) | 是 |  |  | 变更原因 |
| `trace_id` | varchar(128) | 是 |  | idx | 链路追踪 |
| `created_at` | timestamptz | 是 | now() | idx | 变更时间 |

## 业务能力接口清单

| 能力 | 调用方 | 接口形态 | 核心模型 | 幂等 | 审计 |
| --- | --- | --- | --- | --- | --- |
| Skill Builder 可绑定 Tool 列表 | 用户端 | HTTP `GET /api/tools/bindable` | `ToolDefinition`、`ToolPolicy` | 否 | 否 |
| Tool 执行策略校验 | Agent | RPC `CheckToolExecutionPolicy` | `ToolPolicy`、`ToolWhitelistRule` | 否 | 否 |
| 后台 Tool 列表 | 管理端 | HTTP `GET /api/admin/tools` | `AdminToolDTO` | 否 | 否 |
| 修改 Tool 策略 | 管理端 | HTTP `PATCH /api/admin/tools/:tool_key/policy` | `ToolPolicy` | 是 | 是 |
| 修改 Tool 计价策略 | 管理端 | HTTP `PATCH /api/admin/tools/:tool_key/pricing-policy` | `ToolPricingPolicy` | 是 | 是 |
| 启停 Tool | 管理端 | HTTP `POST /api/admin/tools/:tool_key/status` | `ToolDefinition.status` | 是 | 是 |
| 修改白名单 | 管理端 | HTTP `PUT /api/admin/tools/:tool_key/whitelist` | `ToolWhitelistRule` | 是 | 是 |
| Tool 影响范围预览 | 管理端 | HTTP `POST /api/admin/tools/:tool_key/impact-preview` | `ToolImpactPreviewDTO` | 否 | 否 |

## HTTP API 设计

| Method | Path | 鉴权 | Request DTO | Response DTO | 页面状态 |
| --- | --- | --- | --- | --- | --- |
| GET | `/api/tools/bindable` | user | `ListBindableToolsRequest` | `PageResult<BindableToolDTO>` | `loading`、`empty`、`permission_denied` |
| GET | `/api/admin/tools` | admin | `ListAdminToolsRequest` | `PageResult<AdminToolDTO>` | `loading`、`empty` |
| POST | `/api/admin/tools/:tool_key/impact-preview` | admin | `ToolImpactPreviewRequest` | `ToolImpactPreviewDTO` | `confirming` |
| PATCH | `/api/admin/tools/:tool_key/policy` | admin | `UpdateToolPolicyRequest` + `Idempotency-Key` | `AdminToolDTO` | `confirming`、`success` |
| PATCH | `/api/admin/tools/:tool_key/pricing-policy` | admin | `UpdateToolPricingPolicyRequest` + `Idempotency-Key` | `AdminToolDTO` | `confirming`、`success` |
| POST | `/api/admin/tools/:tool_key/status` | admin | `SetToolStatusRequest` + `Idempotency-Key` | `AdminToolDTO` | `enabled`、`disabled` |
| PUT | `/api/admin/tools/:tool_key/whitelist` | admin | `ReplaceToolWhitelistRequest` + `Idempotency-Key` | `AdminToolDTO` | `success` |

## DTO 设计

| DTO | 字段 |
| --- | --- |
| `ListBindableToolsRequest` | `scope_type` personal/enterprise/system、`skill_id` 可选、`resource_type` 可选、`PaginationRequest` |
| `BindableToolDTO` | `tool_key`、`display_name`、`tool_type`、`risk_level`、`requires_confirmation`、`status`、`description`、`charge_mode` |
| `ListAdminToolsRequest` | `keyword`、`tool_type`、`status`、`risk_level`、`PaginationRequest` |
| `AdminToolDTO` | `tool_id`、`tool_key`、`display_name`、`tool_type`、`status`、`policy`、`pricing_policy`、`whitelist_summary`、`updated_at` |
| `ToolImpactPreviewRequest` | `target_policy_change` 或 `target_status`、`reason` |
| `ToolImpactPreviewDTO` | `preview_token`、`affected_skill_count`、`affected_published_skill_count`、`risk_summary`、`expires_at` |
| `UpdateToolPolicyRequest` | `risk_level`、`timeout_ms`、`retry_policy`、`cancel_policy`、`scope_type`、`preview_token` 可选、`reason` |
| `UpdateToolPricingPolicyRequest` | `charge_mode`、`billing_unit`、`unit_points`、`min_points`、`max_points`、`free_quota`、`rounding_mode`、`effective_at`、`reason` |
| `SetToolStatusRequest` | `target_status`、`preview_token` 可选、`reason` |
| `ReplaceToolWhitelistRequest` | `rules[]`，每项含 `target_type`、`target_id`、`space_type`、`status`、`reason` |

## RPC 设计

### ToolCapabilityService.CheckToolExecutionPolicy

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `tool_name` | string | 是 | Agent 侧 Tool 名称；业务侧映射到 `tool_definitions.tool_key` |
| `tool_type` | string | 是 | Agent 侧 Tool 类型 |
| `project_id` | string | 是 | 当前项目，用于归档和空间校验 |
| `skill_id` | string | 否 | Skill 执行时传 |
| `risk_context` | map<string,string> | 否 | 生成、业务写入、文件处理等风险上下文 |
| `auth_context` | AuthContext | 是 | 当前用户、空间和身份 |
| `request_meta` | RequestMeta | 是 | trace 和 source |

响应：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `allowed` | bool | 是否可执行 |
| `tool_name` | string | Agent Tool 名称 |
| `display_name` | string | 前端可展示名称 |
| `risk_level` | enum | low / medium / high |
| `requires_confirmation` | bool | 是否必须确认 |
| `charge_mode` | enum | `no_charge` / `model_generation` / `tool_usage` / `business_value` |
| `pricing_policy_id` | string | Tool 独立计费策略；`no_charge` 可为空 |
| `timeout_ms` | int64 | 调用超时 |
| `retry_policy` | object | 最大次数、退避、仅幂等 |
| `cancel_policy` | object | 是否可取消、已完成是否保留 |
| `denied_reason` | string | 不可执行原因 |

## Application 函数

```go
type ToolPolicyApp interface {
    CheckToolExecutionPolicy(ctx context.Context, in CheckToolPolicyInput) (ToolExecutionPolicy, error)
    ListBindableTools(ctx context.Context, in ListBindableToolsInput) (Page[BindableToolDTO], error)
    ListAdminTools(ctx context.Context, in ListAdminToolsInput) (Page[AdminToolDTO], error)
    PreviewToolImpact(ctx context.Context, in ToolImpactPreviewInput) (ToolImpactPreviewDTO, error)
    UpdateToolPolicy(ctx context.Context, in UpdateToolPolicyInput) (ToolPolicyDTO, error)
    UpdateToolPricingPolicy(ctx context.Context, in UpdateToolPricingPolicyInput) (ToolPricingPolicyDTO, error)
    SetToolStatus(ctx context.Context, in SetToolStatusInput) (AdminToolDTO, error)
    ReplaceToolWhitelist(ctx context.Context, in ReplaceToolWhitelistInput) (AdminToolDTO, error)
}
```

## 业务规则

- 未开放或 disabled Tool 不能绑定、发布或执行。
- 高风险 Tool 必须人工确认。
- 业务写入、扣费、外部副作用 Tool 默认不自动重试。
- 只有幂等读或明确幂等写允许 Agent 重试。
- Tool 白名单按企业、空间类型、用户等级或套餐过滤。
- 内容安全治理不是 Skill 可绑定 Tool，由 Agent 固定前置执行。
- Tool 积分不由 Agent 自行计算，必须由业务 `CreditService` 根据 Tool 计价策略和模型价格快照计算。
- `model_generation` 类 Tool 不叠加 Tool 调用费，按模型生成价格计费。
- `no_charge` 类 Tool 不单独扣积分，例如安全前置、资产引用校验、视觉理解和普通编排辅助。
- `tool_usage` / `business_value` 类 Tool 才按 `tool_pricing_policies` 独立计费，并进入积分预估、冻结、扣减和流水明细。
- `tool_usage` / `business_value` 的 `billing_unit`、`unit_points` 必填且 `unit_points > 0`；`no_charge` / `model_generation` 的 `unit_points` 必须为 0。
- 计价策略变更采用新增生效记录方式，不覆盖历史记录；预估生成后使用当时返回的 `pricing_policy_id`，后续策略变更不影响已冻结 run。

## 事务设计

| 事务 | 原子写入 | 回滚条件 |
| --- | --- | --- |
| 修改 Tool 策略 | `tool_policies`、`tool_policy_change_records`、审计、幂等记录 | Tool 不存在、preview 失效、审计失败 |
| 修改 Tool 计价策略 | 新增 `tool_pricing_policies`、停用旧 active 策略、变更记录、审计、幂等记录 | charge mode 和 billing unit 不匹配、unit_points 非法 |
| 启停 Tool | `tool_definitions.status`、变更记录、审计、幂等记录 | 停用影响 Published Skill 且未确认 |
| 修改白名单 | 批量 upsert `tool_whitelist_rules`、变更记录、审计、幂等记录 | 规则重复、目标类型非法 |

## 日志和审计

| 动作 | `business_action` | 审计内容 |
| --- | --- | --- |
| 修改 Tool 策略 | `tool.policy.update` | tool_key、risk_level、timeout_ms、retry_policy、cancel_policy、scope_type、原因 |
| 修改 Tool 计价策略 | `tool.pricing_policy.update` | tool_key、charge_mode、billing_unit、unit_points、free_quota、effective_at、原因 |
| 启停 Tool | `tool.status.set` | tool_key、before_status、after_status、affected_skill_count、原因 |
| 修改白名单 | `tool.whitelist.replace` | tool_key、规则数量、目标类型摘要、原因 |
| 影响范围预览 | 不写审计，写 info 日志 | tool_key、affected_skill_count、trace_id |

审计不得保存 Tool 内部实现代码、供应商密钥、用户私有参数或完整执行 payload。日志必须包含 `trace_id`、`admin_id`、`tool_key`、`business_action`、`result`。

## 【Agent开发】需要提供的能力与参数

| 【Agent开发】场景 | 业务 RPC | Agent 必传参数 | 业务服务返回 | Agent 行为 |
| --- | --- | --- | --- | --- |
| Skill 执行 Tool 前 | `CheckToolExecutionPolicy` | `tool_name`、`tool_type`、`project_id`、`skill_id`、`auth_context` | 风险、超时、重试、确认策略 | 不可执行则阻断并输出 Tool 不可用 |
| 无 Skill 直接 Tool 意图 | `CheckToolExecutionPolicy` | `tool_name`、`tool_type`、`project_id`、`risk_context.scene=direct_tool` | 同上 | 满足策略才允许直接执行 |
| 高风险 Tool | `CheckToolExecutionPolicy` | 同上 | `requires_confirmation=true` | 创建 interrupt，确认后执行 |
| Tool 执行日志关联 | 所有 Tool 业务 RPC | `trace_id`、`tool_call_id`、`idempotency_key` | 业务错误码 | 映射为 `tool.call.failed` 或 `tool.result` |
| Tool 积分预估 | `EstimateToolCredits` 或 `EstimateGenerationCredits.tool_usage_items[]` | `tool_name`、`tool_type`、`billing_unit`、`quantity` | `estimate_item_id`、`pricing_policy_id`、`estimate_points` | 后续扣费必须引用 `estimate_item_id` |

## 测试

- disabled Tool 拒绝执行。
- 企业白名单不匹配返回 `PERMISSION_DENIED`。
- high risk 必须 `requires_confirmation=true`。
- 非幂等高风险 Tool `retry_policy.max_attempts=0`。
- Agent contract test 覆盖 Tool 停用、权限不足、超时策略。
- 管理端修改计价策略后，新的预估使用新 `pricing_policy_id`，旧冻结继续按旧明细结算。
