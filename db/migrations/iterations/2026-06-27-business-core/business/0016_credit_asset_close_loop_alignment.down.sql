DROP TABLE IF EXISTS generated_asset_object_slots;

DROP INDEX IF EXISTS ux_upload_intents_object_key;
ALTER TABLE upload_intents
  DROP COLUMN IF EXISTS object_key,
  DROP COLUMN IF EXISTS bucket;

DROP INDEX IF EXISTS ux_asset_storage_objects_object_key;
ALTER TABLE asset_storage_objects
  DROP COLUMN IF EXISTS etag,
  DROP COLUMN IF EXISTS object_key;

ALTER TABLE redeem_code_batches
  DROP COLUMN IF EXISTS credit_expires_at;

DROP TABLE IF EXISTS credit_freeze_batch_items;

DROP INDEX IF EXISTS idx_credit_estimates_safety;
ALTER TABLE credit_estimates
  DROP COLUMN IF EXISTS safety_evidence_digest,
  DROP COLUMN IF EXISTS safety_evidence_id;
