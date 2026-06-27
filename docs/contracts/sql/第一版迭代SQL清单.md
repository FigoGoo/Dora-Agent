# 第一版迭代 SQL 清单

状态：draft
owner：文档与契约责任域；业务服务责任域和 Agent 服务责任域按数据库边界维护
更新时间：2026-06-28
适用范围：Dora-Agent 第一版本地开发 SQL 迭代脚本计划

## 成熟度复核

当前成熟度：draft，不升 `active`。  
使用方式：可作为业务库和 Agent 库首批 migration 落脚、脚本分组、schema 策略和验证要求输入；实际脚本以 `db/migrations/iterations/**` 为准。

已补齐项：实际 migration 目录、PostgreSQL schema 策略、up/down 脚本落脚和 SQL lint 要求已在本文冻结。

未冻结项：本地 PostgreSQL up/down 执行证据、服务级验收报告和线上迁移操作记录尚未固化，因此本文仍保持 `draft`。

## SQL 规则

- 业务数据库和 Agent 领域数据库分目录维护。
- 第一版不拆 PostgreSQL `business` / `agent` schema，不执行 `CREATE SCHEMA`；本地开发使用默认 schema，通过 migration 目录、表名和服务访问边界区分业务库与 Agent 库。
- 不添加数据库级外键约束，不写 `FOREIGN KEY` / `REFERENCES`。
- 跨表一致性由事务、唯一约束、普通索引、幂等记录和测试保证。
- 列表查询必须有分页索引设计，默认 10 条每页。
- 每个 SQL 脚本必须包含 up/down 或说明不可回滚原因。

## 当前 Migration 落脚

```text
db/migrations/iterations/2026-06-27-business-core/
  business/
    0001_common_idempotency_audit.up.sql
    ...
    0018_work_notification_alignment.down.sql
db/migrations/iterations/20260627_agent_runtime/
  agent/
    001_create_agent_runtime.up.sql
    001_create_agent_runtime.down.sql
```

## 业务库迁移分组

| 脚本 | 覆盖表或能力 | 状态 |
| --- | --- | --- |
| `0001_common_idempotency_audit` | `idempotency_records`、`business_audit_logs` | implemented |
| `0002_account_auth_space_enterprise` | `business_users`、`auth_sessions`、`business_spaces`、`enterprises`、`enterprise_members`、`enterprise_invites` | implemented |
| `0003_platform_admin_user_audit` | `platform_admins`、`platform_admin_bootstraps`、`platform_admin_sessions`、`admin_login_attempts` | implemented |
| `0004_project` | `projects`、`project_assets`、`project_works` | implemented |
| `0005_model_config` | `model_providers`、`model_provider_credentials`、`models`、`model_prices`、`default_models`、`model_connectivity_tests` | implemented |
| `0006_tool_policy_pricing` | `tool_definitions`、`tool_policies`、`tool_pricing_policies`、`tool_whitelist_rules`、`tool_policy_change_records` | implemented |
| `0007_skill_catalog_review` | `skills`、`skill_versions`、`skill_tool_bindings`、`skill_output_element_schemas`、`skill_test_cases`、`skill_test_runs`、`skill_review_records` | implemented |
| `0008_credit_account_tool_charge_redeem` | `credit_accounts`、`credit_batches`、`credit_estimates`、`credit_estimate_items`、`credit_freezes`、`credit_ledger_entries`、`credit_tool_charge_batches`、`credit_tool_charge_items`、`redeem_code_batches`、`redeem_codes`、`redeem_code_redemptions` | implemented |
| `0009_asset_upload_element_access` | `assets`、`asset_storage_objects`、`upload_intents`、`asset_elements`、`asset_element_types`、`asset_access_logs`、`asset_element_type_change_records` | implemented |
| `0010_asset_commit_credit_close_loop` | `asset_commit_batches`、`asset_commit_items` | implemented |
| `0011_work_public_snapshot_like` | `works`、`work_assets`、`work_public_snapshots`、`work_likes`、`work_categories`、`work_moderation_records` | implemented |
| `0012_notification` | `notifications`、`notification_create_failures` | implemented |
| `0013_identity_project_alignment` | 账户、身份和项目对齐补丁 | implemented |
| `0014_skill_confirmation_policy` | Skill 确认策略补丁 | implemented |
| `0015_skill_test_result_idempotency` | Skill 测试结果幂等补丁 | implemented |
| `0016_credit_asset_close_loop_alignment` | `credit_freeze_batch_items`、`generated_asset_object_slots` | implemented |
| `0017_redeem_code_account_contract` | 兑换码账户契约补丁 | implemented |
| `0018_work_notification_alignment` | 作品和通知对齐补丁 | implemented |

## Agent 库首批表

| 表 | 责任域 | 来源契约 | 状态 |
| --- | --- | --- | --- |
| agent_sessions | Agent 服务 | Agent 数据模型 | implemented |
| agent_runs | Agent 服务 | Agent 数据模型 | implemented |
| agent_messages | Agent 服务 | Agent 数据模型 | implemented |
| agent_events | Agent 服务 | Agent 数据模型 | implemented |
| agent_tool_calls | Agent 服务 | Agent 数据模型 | implemented |
| agent_tasks | Agent 服务 | Agent 数据模型 | implemented |
| agent_interrupts | Agent 服务 | Agent 数据模型 | implemented |
| agent_artifacts | Agent 服务 | Agent 数据模型 | implemented |
| agent_safety_evaluations | Agent 服务 | Agent 数据模型 | implemented |
| agent_memories | Agent 服务 | Agent 数据模型 | implemented，功能按授权策略启用 |
| agent_runtime_configs | Agent 服务 | Agent 数据模型 | implemented |

## 验证要求

- SQL lint：禁止 `FOREIGN KEY` 和 `REFERENCES`。
- SQL lint：第一版禁止 `CREATE SCHEMA`。
- migration up/down 本地可执行。
- Agent DB 边界测试：不得出现积分、业务资产事实、企业成员、作品公开状态。
- 业务 DB 边界测试：不得出现 Agent run/message/event/tool_call 内部状态。

## 后续证据

- 需要补充本地 PostgreSQL 执行 `business` 和 `agent` up/down 的命令、输出和数据库版本。
- 需要补充 SQL lint 输出，证明无 `FOREIGN KEY`、`REFERENCES` 和 `CREATE SCHEMA`。
- 需要补充线上 CentOS 8 单机迁移操作记录。
