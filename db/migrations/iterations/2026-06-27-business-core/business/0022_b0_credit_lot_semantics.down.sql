-- Dora business service migration 0022 (down)
-- 回滚 B0 CreditLot 兼容字段。保留 legacy total_points / remaining_points。

DROP INDEX IF EXISTS idx_credit_batches_lot_account_status_expiry;

ALTER TABLE credit_batches
  DROP COLUMN IF EXISTS original_points,
  DROP COLUMN IF EXISTS available_points,
  DROP COLUMN IF EXISTS frozen_points,
  DROP COLUMN IF EXISTS consumed_points,
  DROP COLUMN IF EXISTS expired_points,
  DROP COLUMN IF EXISTS granted_at,
  DROP COLUMN IF EXISTS expiry_policy_json,
  DROP COLUMN IF EXISTS spend_scope_json,
  DROP COLUMN IF EXISTS settlement_eligible;
