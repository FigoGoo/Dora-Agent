CREATE TABLE IF NOT EXISTS credit_holds (
  credit_hold_id TEXT PRIMARY KEY,
  credit_account_id TEXT NOT NULL,
  credit_account_scope TEXT NOT NULL,
  run_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  tool_plan_id TEXT NOT NULL,
  tool_plan_digest TEXT NOT NULL,
  status TEXT NOT NULL,
  frozen_credits BIGINT NOT NULL,
  committed_credits BIGINT NOT NULL DEFAULT 0,
  released_credits BIGINT NOT NULL DEFAULT 0,
  idempotency_key TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_credit_holds_run
  ON credit_holds (run_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS credit_ledger_entries (
  ledger_entry_id TEXT PRIMARY KEY,
  credit_hold_id TEXT NOT NULL,
  credit_account_id TEXT NOT NULL,
  entry_type TEXT NOT NULL,
  credits BIGINT NOT NULL,
  reason TEXT NOT NULL,
  digest TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_credit_ledger_hold
  ON credit_ledger_entries (credit_hold_id, created_at DESC);

CREATE TABLE IF NOT EXISTS tool_pricing_snapshots (
  pricing_snapshot_id TEXT PRIMARY KEY,
  tool_id TEXT NOT NULL,
  tool_version TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  unit_credits BIGINT NOT NULL,
  pricing_digest TEXT NOT NULL,
  effective_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tool_pricing_lookup
  ON tool_pricing_snapshots (tool_id, tool_version, resource_type, effective_at DESC);

CREATE TABLE IF NOT EXISTS generated_assets (
  asset_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  tool_task_id TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  status TEXT NOT NULL,
  tos_object_key TEXT NOT NULL,
  preview_url TEXT,
  asset_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_generated_assets_project
  ON generated_assets (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS asset_commit_records (
  commit_record_id TEXT PRIMARY KEY,
  tool_task_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  status TEXT NOT NULL,
  tool_result_digest TEXT NOT NULL,
  committed_asset_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  failed_asset_count BIGINT NOT NULL DEFAULT 0,
  commit_digest TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_asset_commit_records_task
  ON asset_commit_records (tool_task_id, created_at DESC);
