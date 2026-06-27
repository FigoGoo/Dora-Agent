DROP INDEX IF EXISTS idx_redeem_code_batches_channel;
DROP INDEX IF EXISTS idx_redeem_code_batches_account_bind;

ALTER TABLE redeem_code_batches
  DROP COLUMN IF EXISTS reason,
  DROP COLUMN IF EXISTS bind_target_id,
  DROP COLUMN IF EXISTS bind_target_type,
  DROP COLUMN IF EXISTS account_type;
