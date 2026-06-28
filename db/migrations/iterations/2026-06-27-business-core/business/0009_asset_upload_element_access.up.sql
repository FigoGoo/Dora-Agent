-- Dora business service migration 0009
-- Owner: 业务微服务后端工程师
-- Scope: assets, TOS objects, upload intents, asset elements and access logs.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS assets (
  id varchar(64) PRIMARY KEY,
  asset_no varchar(64) NOT NULL UNIQUE,
  owner_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  enterprise_id varchar(64),
  project_id varchar(64),
  asset_type varchar(64) NOT NULL,
  title varchar(160),
  status varchar(32) NOT NULL DEFAULT 'active',
  visibility varchar(32) NOT NULL DEFAULT 'private',
  source_type varchar(64) NOT NULL,
  source_ref_id varchar(64),
  content_digest varchar(128),
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_assets_space_status_created
  ON assets (space_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_assets_project_status_created
  ON assets (project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_assets_owner_status_created
  ON assets (owner_user_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS asset_storage_objects (
  id varchar(64) PRIMARY KEY,
  asset_id varchar(64) NOT NULL,
  bucket varchar(128) NOT NULL,
  object_key_digest varchar(128) NOT NULL,
  object_uri varchar(1024) NOT NULL,
  mime_type varchar(128),
  size_bytes bigint,
  checksum varchar(128),
  storage_status varchar(32) NOT NULL DEFAULT 'available',
  preview_uri varchar(1024),
  download_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_storage_objects_asset
  ON asset_storage_objects (asset_id, storage_status);
CREATE INDEX IF NOT EXISTS idx_asset_storage_objects_key
  ON asset_storage_objects (object_key_digest);

CREATE TABLE IF NOT EXISTS upload_intents (
  id varchar(64) PRIMARY KEY,
  upload_intent_id varchar(64) NOT NULL UNIQUE,
  owner_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  project_id varchar(64),
  asset_type varchar(64) NOT NULL,
  object_key_digest varchar(128) NOT NULL,
  mime_type varchar(128),
  max_size_bytes bigint NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'created',
  expires_at timestamptz NOT NULL,
  confirmed_asset_id varchar(64),
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_upload_intents_space_status
  ON upload_intents (space_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_upload_intents_expires
  ON upload_intents (status, expires_at);

CREATE TABLE IF NOT EXISTS asset_elements (
  id varchar(64) PRIMARY KEY,
  asset_id varchar(64) NOT NULL,
  element_type varchar(64) NOT NULL,
  element_key varchar(128) NOT NULL,
  element_summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  preview_text varchar(2048),
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (asset_id, element_key)
);

CREATE INDEX IF NOT EXISTS idx_asset_elements_asset_type
  ON asset_elements (asset_id, element_type, status);

CREATE TABLE IF NOT EXISTS asset_element_types (
  id varchar(64) PRIMARY KEY,
  element_type varchar(64) NOT NULL UNIQUE,
  display_name varchar(128) NOT NULL,
  schema_version varchar(64) NOT NULL,
  schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'active',
  operator_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_element_types_status
  ON asset_element_types (status, element_type);

CREATE TABLE IF NOT EXISTS asset_access_logs (
  id varchar(64) PRIMARY KEY,
  asset_id varchar(64) NOT NULL,
  actor_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  project_id varchar(64),
  access_purpose varchar(64) NOT NULL,
  allowed boolean NOT NULL,
  deny_reason varchar(128),
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_access_logs_asset_created
  ON asset_access_logs (asset_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_asset_access_logs_actor_created
  ON asset_access_logs (actor_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS asset_element_type_change_records (
  id varchar(64) PRIMARY KEY,
  element_type varchar(64) NOT NULL,
  change_type varchar(64) NOT NULL,
  before_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  after_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  operator_id varchar(64),
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_element_type_change_records_type
  ON asset_element_type_change_records (element_type, created_at DESC);
