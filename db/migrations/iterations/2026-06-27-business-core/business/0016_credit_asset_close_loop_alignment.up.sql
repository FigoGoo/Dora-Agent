-- Dora business service migration 0016
-- Owner: 业务微服务后端工程师
-- Scope: Credit freeze batch allocation, asset object keys and generated asset slots.
-- Lock risk: additive local baseline alignment; no database-level foreign keys.

ALTER TABLE credit_estimates
  ADD COLUMN IF NOT EXISTS safety_evidence_id varchar(64),
  ADD COLUMN IF NOT EXISTS safety_evidence_digest varchar(128);

CREATE INDEX IF NOT EXISTS idx_credit_estimates_safety
  ON credit_estimates (safety_evidence_id)
  WHERE safety_evidence_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS credit_freeze_batch_items (
  id varchar(64) PRIMARY KEY,
  freeze_id varchar(64) NOT NULL,
  account_id varchar(64) NOT NULL,
  batch_id varchar(64) NOT NULL,
  frozen_points bigint NOT NULL,
  charged_points bigint NOT NULL DEFAULT 0,
  released_points bigint NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'frozen',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (freeze_id, batch_id)
);

CREATE INDEX IF NOT EXISTS idx_credit_freeze_batch_items_freeze
  ON credit_freeze_batch_items (freeze_id, status);

CREATE INDEX IF NOT EXISTS idx_credit_freeze_batch_items_batch
  ON credit_freeze_batch_items (batch_id, status);

ALTER TABLE redeem_code_batches
  ADD COLUMN IF NOT EXISTS credit_expires_at timestamptz;

ALTER TABLE asset_storage_objects
  ADD COLUMN IF NOT EXISTS object_key varchar(1024),
  ADD COLUMN IF NOT EXISTS etag varchar(256);

CREATE UNIQUE INDEX IF NOT EXISTS ux_asset_storage_objects_object_key
  ON asset_storage_objects (object_key)
  WHERE object_key IS NOT NULL;

ALTER TABLE upload_intents
  ADD COLUMN IF NOT EXISTS bucket varchar(128),
  ADD COLUMN IF NOT EXISTS object_key varchar(1024);

CREATE UNIQUE INDEX IF NOT EXISTS ux_upload_intents_object_key
  ON upload_intents (object_key)
  WHERE object_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS generated_asset_object_slots (
  id varchar(64) PRIMARY KEY,
  slot_id varchar(64) NOT NULL UNIQUE,
  project_id varchar(64) NOT NULL,
  session_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  artifact_id varchar(64) NOT NULL,
  resource_type varchar(64) NOT NULL,
  bucket varchar(128) NOT NULL,
  object_key varchar(1024) NOT NULL,
  object_key_digest varchar(128) NOT NULL,
  content_type varchar(128) NOT NULL,
  size_bytes bigint NOT NULL,
  checksum varchar(128),
  etag varchar(256),
  status varchar(32) NOT NULL DEFAULT 'created',
  idempotency_key varchar(128) NOT NULL,
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz NOT NULL,
  created_by_user_id varchar(64) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (run_id, artifact_id),
  UNIQUE (object_key),
  UNIQUE (created_by_user_id, idempotency_key, artifact_id)
);

CREATE INDEX IF NOT EXISTS idx_generated_asset_object_slots_run
  ON generated_asset_object_slots (run_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_generated_asset_object_slots_project
  ON generated_asset_object_slots (project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_generated_asset_object_slots_expires
  ON generated_asset_object_slots (status, expires_at);
