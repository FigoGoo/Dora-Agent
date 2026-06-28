-- Dora business service migration 0010
-- Owner: 业务微服务后端工程师
-- Scope: generated asset commit batches and charged commit items.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS asset_commit_batches (
  id varchar(64) PRIMARY KEY,
  commit_id varchar(64) NOT NULL UNIQUE,
  project_id varchar(64) NOT NULL,
  session_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  freeze_id varchar(64) NOT NULL,
  estimate_id varchar(64),
  actor_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  safety_evidence_id varchar(64) NOT NULL,
  safety_evidence_digest varchar(128) NOT NULL,
  charged_points bigint NOT NULL DEFAULT 0,
  released_points bigint NOT NULL DEFAULT 0,
  commit_status varchar(32) NOT NULL DEFAULT 'committed',
  ledger_ref varchar(64),
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (actor_user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_asset_commit_batches_run
  ON asset_commit_batches (run_id, commit_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_asset_commit_batches_project
  ON asset_commit_batches (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_asset_commit_batches_freeze
  ON asset_commit_batches (freeze_id, commit_status);

CREATE TABLE IF NOT EXISTS asset_commit_items (
  id varchar(64) PRIMARY KEY,
  commit_id varchar(64) NOT NULL,
  artifact_id varchar(64) NOT NULL,
  asset_id varchar(64) NOT NULL,
  resource_type varchar(64) NOT NULL,
  element_type varchar(64) NOT NULL,
  estimate_item_id varchar(64),
  tool_name varchar(128),
  tool_type varchar(64),
  charge_quantity bigint,
  charged_points bigint NOT NULL DEFAULT 0,
  content_uri_digest varchar(128),
  artifact_summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'committed',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (artifact_id),
  UNIQUE (estimate_item_id)
);

CREATE INDEX IF NOT EXISTS idx_asset_commit_items_commit
  ON asset_commit_items (commit_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_commit_items_asset
  ON asset_commit_items (asset_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_commit_items_tool
  ON asset_commit_items (tool_name, tool_type, created_at DESC);
