-- Dora business service migration 0017
-- Owner: 业务微服务后端工程师
-- Scope: M4 redeem code account_type and bind target contract alignment.
-- Lock risk: additive columns and indexes; no database-level foreign keys.

ALTER TABLE redeem_code_batches
  ADD COLUMN IF NOT EXISTS account_type varchar(32),
  ADD COLUMN IF NOT EXISTS bind_target_type varchar(32),
  ADD COLUMN IF NOT EXISTS bind_target_id varchar(64),
  ADD COLUMN IF NOT EXISTS reason varchar(512);

UPDATE redeem_code_batches
SET
  account_type = CASE
    WHEN target_type IN ('enterprise') THEN 'enterprise'
    WHEN target_type IN ('personal', 'personal_user', 'user') THEN 'personal'
    WHEN target_enterprise_id IS NOT NULL THEN 'enterprise'
    ELSE 'personal'
  END,
  bind_target_type = CASE
    WHEN target_type IN ('personal_user', 'user') THEN 'user'
    WHEN target_type IN ('enterprise') THEN 'enterprise'
    WHEN target_type IN ('none', '') THEN 'none'
    WHEN channel_code IS NOT NULL AND channel_code <> '' THEN 'channel'
    ELSE 'none'
  END,
  bind_target_id = CASE
    WHEN target_type IN ('personal_user', 'user') THEN target_user_id
    WHEN target_type IN ('enterprise') THEN target_enterprise_id
    ELSE NULL
  END
WHERE account_type IS NULL OR bind_target_type IS NULL;

ALTER TABLE redeem_code_batches
  ALTER COLUMN account_type SET DEFAULT 'personal',
  ALTER COLUMN bind_target_type SET DEFAULT 'none';

UPDATE redeem_code_batches
SET account_type = 'personal'
WHERE account_type IS NULL;

UPDATE redeem_code_batches
SET bind_target_type = 'none'
WHERE bind_target_type IS NULL;

ALTER TABLE redeem_code_batches
  ALTER COLUMN account_type SET NOT NULL,
  ALTER COLUMN bind_target_type SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_account_bind
  ON redeem_code_batches (account_type, bind_target_type, bind_target_id, status);

CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_channel
  ON redeem_code_batches (channel_code, status)
  WHERE channel_code IS NOT NULL;
