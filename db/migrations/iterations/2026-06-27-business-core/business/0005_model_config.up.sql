-- Dora business service migration 0005
-- Owner: 业务微服务后端工程师
-- Scope: model providers, credentials, model catalog, prices and defaults.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS model_providers (
  id varchar(64) PRIMARY KEY,
  provider_code varchar(64) NOT NULL UNIQUE,
  display_name varchar(128) NOT NULL,
  provider_type varchar(32) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  base_url varchar(512),
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_model_providers_status
  ON model_providers (status, display_name);

CREATE TABLE IF NOT EXISTS model_provider_credentials (
  id varchar(64) PRIMARY KEY,
  provider_id varchar(64) NOT NULL,
  credential_name varchar(128) NOT NULL,
  secret_ref varchar(255) NOT NULL,
  encrypted_payload_digest varchar(128),
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider_id, credential_name)
);

CREATE INDEX IF NOT EXISTS idx_model_provider_credentials_provider_status
  ON model_provider_credentials (provider_id, status);

CREATE TABLE IF NOT EXISTS models (
  id varchar(64) PRIMARY KEY,
  provider_id varchar(64) NOT NULL,
  model_code varchar(128) NOT NULL,
  display_name varchar(128) NOT NULL,
  resource_type varchar(32) NOT NULL,
  capability_tags jsonb NOT NULL DEFAULT '[]'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'active',
  credential_id varchar(64),
  route_config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider_id, model_code)
);

CREATE INDEX IF NOT EXISTS idx_models_resource_status
  ON models (resource_type, status, display_name);

CREATE TABLE IF NOT EXISTS model_prices (
  id varchar(64) PRIMARY KEY,
  pricing_snapshot_id varchar(64) NOT NULL UNIQUE,
  model_id varchar(64) NOT NULL,
  resource_type varchar(32) NOT NULL,
  billing_unit varchar(32) NOT NULL,
  unit_points numeric(20,6) NOT NULL,
  min_charge_points bigint NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'active',
  effective_at timestamptz NOT NULL,
  expired_at timestamptz,
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_model_prices_model_effective
  ON model_prices (model_id, status, effective_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_prices_resource_status
  ON model_prices (resource_type, status, effective_at DESC);

CREATE TABLE IF NOT EXISTS default_models (
  id varchar(64) PRIMARY KEY,
  resource_type varchar(32) NOT NULL,
  model_id varchar(64) NOT NULL,
  pricing_snapshot_id varchar(64) NOT NULL,
  scope varchar(32) NOT NULL DEFAULT 'global',
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (resource_type, scope, status)
);

CREATE INDEX IF NOT EXISTS idx_default_models_resource
  ON default_models (resource_type, status);

CREATE TABLE IF NOT EXISTS model_connectivity_tests (
  id varchar(64) PRIMARY KEY,
  provider_id varchar(64) NOT NULL,
  model_id varchar(64),
  status varchar(32) NOT NULL,
  latency_ms integer,
  error_code varchar(64),
  error_message_digest varchar(128),
  tested_by_admin_id varchar(64),
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_model_connectivity_tests_provider_created
  ON model_connectivity_tests (provider_id, created_at DESC);
