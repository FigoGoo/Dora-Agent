-- Dora business service migration 0020 (down)
-- 移除 append-only 触发器与函数。

DO $$
DECLARE
  t text;
  tables text[] := ARRAY[
    'business_audit_logs','credit_ledger_entries','asset_access_logs','admin_login_attempts',
    'redeem_code_redemptions','work_moderation_records','skill_review_records','asset_element_type_change_records',
    'tool_policy_change_records','model_connectivity_tests'
  ];
BEGIN
  FOREACH t IN ARRAY tables LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS trg_append_only ON %I', t);
  END LOOP;
END $$;

DROP FUNCTION IF EXISTS business_forbid_mutation();
