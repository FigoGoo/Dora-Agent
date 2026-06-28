-- Dora business service migration 0020
-- Owner: 业务微服务后端工程师 (W1 批A · INFRA-13)
-- Scope: 审计/流水/日志表 DB 级 append-only —— 禁止行级 UPDATE/DELETE。
-- 约束: 安全规范(审计不可篡改/可取证) / 数据建模规范 / 禁外键。
-- 机制: BEFORE UPDATE OR DELETE 触发器 RAISE EXCEPTION，任何角色(含 superuser)都拦，不依赖 REVOKE 角色管理。
--       TRUNCATE 不在拦截范围(DDL，由权限控制)；INSERT 正常放行。
-- 影响表: 10 张纯 append-only 表(写入即终态)。skill_review_records 与 work_moderation_records 对称：审/评动作每次 INSERT 新记录、无 UPDATE。

CREATE OR REPLACE FUNCTION business_forbid_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'append-only table % does not allow %', TG_TABLE_NAME, TG_OP
    USING HINT = 'audit/ledger rows are immutable; insert a new row instead';
END;
$$ LANGUAGE plpgsql;

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
    EXECUTE format('CREATE TRIGGER trg_append_only BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION business_forbid_mutation()', t);
  END LOOP;
END $$;
