-- Dora business service migration 0022
-- Owner: 业务微服务后端工程师 (B0 Credit Lot)
-- Scope: 为现有 credit_batches 补齐 B0 CreditLot 语义字段，保持现有表名和调用链兼容。
-- 约束: 无数据库级外键；现有 total_points / remaining_points 继续作为兼容字段。
-- 锁风险: ADD COLUMN + 本地基线回填；当前本地测试库为空或小数据量，线上执行前需按实际数据量评估回填窗口。

ALTER TABLE credit_batches
  ADD COLUMN IF NOT EXISTS original_points bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS available_points bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS frozen_points bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS consumed_points bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS expired_points bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS granted_at timestamptz,
  ADD COLUMN IF NOT EXISTS expiry_policy_json jsonb NOT NULL DEFAULT '{"type":"never_expire"}'::jsonb,
  ADD COLUMN IF NOT EXISTS spend_scope_json jsonb NOT NULL DEFAULT '["tool_generation","skill_usage"]'::jsonb,
  ADD COLUMN IF NOT EXISTS settlement_eligible boolean NOT NULL DEFAULT false;

UPDATE credit_batches
SET
  original_points = CASE WHEN original_points = 0 THEN total_points ELSE original_points END,
  available_points = CASE WHEN available_points = 0 THEN remaining_points ELSE available_points END,
  frozen_points = COALESCE((
    SELECT SUM(GREATEST(item.frozen_points - item.charged_points - item.released_points, 0))
    FROM credit_freeze_batch_items item
    WHERE item.batch_id = credit_batches.id
  ), frozen_points),
  consumed_points = COALESCE((
    SELECT SUM(item.charged_points)
    FROM credit_freeze_batch_items item
    WHERE item.batch_id = credit_batches.id
  ), consumed_points),
  granted_at = COALESCE(granted_at, created_at),
  expiry_policy_json = CASE
    WHEN expires_at IS NULL THEN '{"type":"never_expire"}'::jsonb
    ELSE jsonb_build_object(
      'type', 'fixed_date',
      'expires_at', to_char(expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
    )
  END,
  spend_scope_json = CASE
    WHEN jsonb_typeof(spend_scope_json) = 'array' AND jsonb_array_length(spend_scope_json) > 0 THEN spend_scope_json
    ELSE '["tool_generation","skill_usage"]'::jsonb
  END,
  settlement_eligible = CASE
    WHEN source_type IN ('recharge_package', 'redeem_code', 'admin_grant', 'seed') THEN true
    ELSE settlement_eligible
  END
WHERE granted_at IS NULL
   OR original_points = 0
   OR available_points = 0
   OR consumed_points = 0
   OR expiry_policy_json = '{"type":"never_expire"}'::jsonb
   OR spend_scope_json = '[]'::jsonb;

ALTER TABLE credit_batches
  ALTER COLUMN granted_at SET DEFAULT now(),
  ALTER COLUMN granted_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_credit_batches_lot_account_status_expiry
  ON credit_batches (account_id, status, expires_at NULLS LAST, granted_at ASC);
