-- Dora business service migration 0019 (down)
-- 回滚公共列基线。移除本迭代新增的 created_by/updated_by；deleted_at 仅移除本迭代新增的表。
-- 注意: business_users(0002)/assets(0009)/works(0011) 的 deleted_at 属历史已有列，本 down 不移除，避免破坏既有 schema。

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
  preexisting_deleted_at text[] := ARRAY['business_users','assets','works'];
BEGIN
  FOREACH t IN ARRAY tables LOOP
    EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS created_by', t);
    EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS updated_by', t);
    IF NOT (t = ANY(preexisting_deleted_at)) THEN
      EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS deleted_at', t);
    END IF;
  END LOOP;
END $$;
