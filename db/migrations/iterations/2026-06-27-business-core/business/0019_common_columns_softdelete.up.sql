-- Dora business service migration 0019
-- Owner: 业务微服务后端工程师 (W1 批A · INFRA-1)
-- Scope: 业务库公共列基线 —— 为可变业务实体/配置/状态/关联表补 created_by/updated_by/deleted_at，建立统一软删基线。
-- 约束: 数据建模规范(公共列基线: created_by/updated_by/deleted_at) / 迭代SQL脚本规范(禁外键、说明锁风险、不改历史脚本) / 数据主权(仅业务库)。
-- 影响表: 53 张可变业务表。append-only 审计/流水表(9 张)见 0020；idempotency_records 自成体系(有 tenant/expires，不软删)不纳入。
-- 锁风险: ADD COLUMN(可空、无默认值) 仅取短 ACCESS EXCLUSIVE 锁、不重写表；本地空库为 create-time，无数据量风险。
-- 幂等: ADD COLUMN IF NOT EXISTS；business_users/works/assets 既有 deleted_at 自动跳过，可重复执行。
-- 备注: 隔离键沿用现有 space_id/enterprise_id，本迭代不追加 tenant_id(避免与 space_id 冗余)。created_by/updated_by 本批仅加列(可空)，operator 回填留后续子切片。

DO $$
DECLARE
  t text;
  tables text[] := ARRAY[
    'business_users','auth_sessions','business_spaces','enterprises','enterprise_members','enterprise_invites',
    'platform_admins','platform_admin_bootstraps','platform_admin_sessions',
    'projects','project_assets','project_works',
    'model_providers','model_provider_credentials','models','model_prices','default_models',
    'tool_definitions','tool_policies','tool_pricing_policies','tool_whitelist_rules',
    'skills','skill_versions','skill_tool_bindings','skill_output_element_schemas','skill_test_cases','skill_test_runs','skill_review_records',
    'credit_accounts','credit_batches','credit_estimates','credit_estimate_items','credit_freezes','credit_tool_charge_batches','credit_tool_charge_items','redeem_code_batches','redeem_codes',
    'assets','asset_storage_objects','upload_intents','asset_elements','asset_element_types',
    'asset_commit_batches','asset_commit_items',
    'works','work_assets','work_public_snapshots','work_likes','work_categories',
    'notifications','notification_create_failures',
    'credit_freeze_batch_items','generated_asset_object_slots'
  ];
BEGIN
  FOREACH t IN ARRAY tables LOOP
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS created_by varchar(64)', t);
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS updated_by varchar(64)', t);
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS deleted_at timestamptz', t);
  END LOOP;
END $$;
