-- Dora business service migration 0008
-- Owner: 业务微服务后端工程师
-- Scope: credit accounts, estimates, freezes, ledgers, Tool charges and redeem codes.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS credit_accounts (
  id varchar(64) PRIMARY KEY,
  account_type varchar(32) NOT NULL,
  owner_user_id varchar(64),
  enterprise_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'active',
  available_points bigint NOT NULL DEFAULT 0,
  frozen_points bigint NOT NULL DEFAULT 0,
  expires_soon_points bigint NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_type, owner_user_id, enterprise_id)
);

CREATE INDEX IF NOT EXISTS idx_credit_accounts_owner
  ON credit_accounts (owner_user_id, status);
CREATE INDEX IF NOT EXISTS idx_credit_accounts_enterprise
  ON credit_accounts (enterprise_id, status);

CREATE TABLE IF NOT EXISTS credit_batches (
  id varchar(64) PRIMARY KEY,
  account_id varchar(64) NOT NULL,
  batch_type varchar(32) NOT NULL,
  source_type varchar(64) NOT NULL,
  source_id varchar(64),
  total_points bigint NOT NULL,
  remaining_points bigint NOT NULL,
  expires_at timestamptz,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credit_batches_account_expires
  ON credit_batches (account_id, status, expires_at NULLS LAST);

CREATE TABLE IF NOT EXISTS credit_estimates (
  id varchar(64) PRIMARY KEY,
  estimate_id varchar(64) NOT NULL UNIQUE,
  account_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  resource_type varchar(64),
  model_id varchar(64),
  pricing_snapshot_id varchar(64),
  estimate_points bigint NOT NULL,
  available_points bigint NOT NULL,
  expires_soon_points bigint NOT NULL DEFAULT 0,
  account_type varchar(32) NOT NULL,
  insufficient boolean NOT NULL DEFAULT false,
  status varchar(32) NOT NULL DEFAULT 'estimated',
  expires_at timestamptz NOT NULL,
  created_by_user_id varchar(64) NOT NULL,
  trace_id varchar(128) NOT NULL,
  request_meta_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credit_estimates_account_status
  ON credit_estimates (account_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_credit_estimates_project
  ON credit_estimates (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS credit_estimate_items (
  id varchar(64) PRIMARY KEY,
  estimate_id varchar(64) NOT NULL,
  estimate_item_id varchar(64) NOT NULL UNIQUE,
  item_type varchar(64) NOT NULL,
  tool_name varchar(128),
  tool_type varchar(64),
  pricing_policy_id varchar(64),
  model_id varchar(64),
  resource_type varchar(64),
  billing_unit varchar(32),
  quantity numeric(20,6),
  unit_points numeric(20,6),
  estimate_points bigint NOT NULL,
  free_reason varchar(128),
  status varchar(32) NOT NULL DEFAULT 'estimated',
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credit_estimate_items_estimate
  ON credit_estimate_items (estimate_id, status);
CREATE INDEX IF NOT EXISTS idx_credit_estimate_items_tool
  ON credit_estimate_items (tool_name, tool_type, status);

CREATE TABLE IF NOT EXISTS credit_freezes (
  id varchar(64) PRIMARY KEY,
  freeze_id varchar(64) NOT NULL UNIQUE,
  estimate_id varchar(64) NOT NULL,
  account_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  confirmation_id varchar(64),
  frozen_points bigint NOT NULL,
  charged_points bigint NOT NULL DEFAULT 0,
  released_points bigint NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'frozen',
  expires_at timestamptz NOT NULL,
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_credit_freezes_estimate
  ON credit_freezes (estimate_id, status);
CREATE INDEX IF NOT EXISTS idx_credit_freezes_run
  ON credit_freezes (run_id, status);
CREATE INDEX IF NOT EXISTS idx_credit_freezes_expires
  ON credit_freezes (status, expires_at);

CREATE TABLE IF NOT EXISTS credit_ledger_entries (
  id varchar(64) PRIMARY KEY,
  account_id varchar(64) NOT NULL,
  entry_type varchar(32) NOT NULL,
  points_delta bigint NOT NULL,
  balance_after bigint NOT NULL,
  frozen_after bigint NOT NULL,
  source_type varchar(64) NOT NULL,
  source_id varchar(64) NOT NULL,
  batch_id varchar(64),
  project_id varchar(64),
  run_id varchar(64),
  trace_id varchar(128),
  idempotency_key varchar(128),
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credit_ledger_entries_account_created
  ON credit_ledger_entries (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_credit_ledger_entries_source
  ON credit_ledger_entries (source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_credit_ledger_entries_project
  ON credit_ledger_entries (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS credit_tool_charge_batches (
  id varchar(64) PRIMARY KEY,
  tool_charge_id varchar(64) NOT NULL UNIQUE,
  account_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  estimate_id varchar(64) NOT NULL,
  freeze_id varchar(64) NOT NULL,
  session_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  charged_points bigint NOT NULL DEFAULT 0,
  released_points bigint NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'charged',
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_credit_tool_charge_batches_run
  ON credit_tool_charge_batches (run_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_credit_tool_charge_batches_estimate
  ON credit_tool_charge_batches (estimate_id, status);

CREATE TABLE IF NOT EXISTS credit_tool_charge_items (
  id varchar(64) PRIMARY KEY,
  tool_charge_id varchar(64) NOT NULL,
  estimate_item_id varchar(64) NOT NULL,
  tool_call_id varchar(64) NOT NULL,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  billing_unit varchar(32) NOT NULL,
  actual_quantity numeric(20,6) NOT NULL,
  charged_points bigint NOT NULL,
  execution_status varchar(32) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'charged',
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (estimate_item_id),
  UNIQUE (tool_call_id)
);

CREATE INDEX IF NOT EXISTS idx_credit_tool_charge_items_charge
  ON credit_tool_charge_items (tool_charge_id, status);
CREATE INDEX IF NOT EXISTS idx_credit_tool_charge_items_tool
  ON credit_tool_charge_items (tool_name, tool_type, created_at DESC);

CREATE TABLE IF NOT EXISTS redeem_code_batches (
  id varchar(64) PRIMARY KEY,
  batch_no varchar(64) NOT NULL UNIQUE,
  target_type varchar(32) NOT NULL,
  target_user_id varchar(64),
  target_enterprise_id varchar(64),
  channel_code varchar(64),
  total_codes integer NOT NULL,
  points_per_code bigint NOT NULL,
  expires_at timestamptz,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_target
  ON redeem_code_batches (target_type, target_user_id, target_enterprise_id, status);

CREATE TABLE IF NOT EXISTS redeem_codes (
  id varchar(64) PRIMARY KEY,
  batch_id varchar(64) NOT NULL,
  code_digest varchar(128) NOT NULL UNIQUE,
  status varchar(32) NOT NULL DEFAULT 'unused',
  redeemed_by_user_id varchar(64),
  redeemed_enterprise_id varchar(64),
  redeemed_account_id varchar(64),
  redeemed_at timestamptz,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_redeem_codes_batch_status
  ON redeem_codes (batch_id, status);
CREATE INDEX IF NOT EXISTS idx_redeem_codes_redeemed_by
  ON redeem_codes (redeemed_by_user_id, redeemed_at DESC);

CREATE TABLE IF NOT EXISTS redeem_code_redemptions (
  id varchar(64) PRIMARY KEY,
  redeem_code_id varchar(64) NOT NULL,
  account_id varchar(64) NOT NULL,
  user_id varchar(64) NOT NULL,
  enterprise_id varchar(64),
  points bigint NOT NULL,
  status varchar(32) NOT NULL,
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (redeem_code_id),
  UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_redeem_code_redemptions_account
  ON redeem_code_redemptions (account_id, created_at DESC);
