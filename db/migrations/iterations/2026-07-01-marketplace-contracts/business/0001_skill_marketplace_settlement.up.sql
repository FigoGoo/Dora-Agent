CREATE TABLE IF NOT EXISTS skill_packages (
  skill_id TEXT PRIMARY KEY,
  creator_user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  visibility TEXT NOT NULL,
  current_version TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_packages_creator
  ON skill_packages (creator_user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_versions (
  skill_version_id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  status TEXT NOT NULL,
  runtime_spec_digest TEXT NOT NULL,
  pricing_policy_digest TEXT NOT NULL,
  submitted_at TIMESTAMPTZ,
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_skill_versions_status
  ON skill_versions (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_pricing_policies (
  pricing_policy_id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  skill_version TEXT NOT NULL,
  pricing_model TEXT NOT NULL,
  usage_credits BIGINT NOT NULL,
  value_delivered_stage TEXT NOT NULL,
  pricing_policy_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS marketplace_listings (
  listing_id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  skill_version_id TEXT NOT NULL,
  status TEXT NOT NULL,
  pricing_policy_digest TEXT NOT NULL,
  published_by TEXT NOT NULL,
  listed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketplace_listings_status
  ON marketplace_listings (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_installations (
  installation_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  account_scope TEXT NOT NULL,
  listing_id TEXT NOT NULL,
  skill_id TEXT NOT NULL,
  installed_version TEXT NOT NULL,
  version_strategy TEXT NOT NULL,
  status TEXT NOT NULL,
  upgrade_status TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (account_id, account_scope, skill_id),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_skill_installations_account
  ON skill_installations (account_id, account_scope, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_usage_records (
  usage_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  listing_id TEXT NOT NULL,
  skill_id TEXT NOT NULL,
  skill_version TEXT NOT NULL,
  pricing_policy_digest TEXT NOT NULL,
  skill_usage_digest TEXT NOT NULL,
  usage_status TEXT NOT NULL,
  charge_status TEXT NOT NULL,
  refund_status TEXT NOT NULL,
  settlement_status TEXT NOT NULL,
  estimated_credits BIGINT NOT NULL,
  credit_hold_id TEXT,
  idempotency_key TEXT NOT NULL,
  value_delivered_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_skill_usage_records_run
  ON skill_usage_records (run_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_skill_usage_records_listing
  ON skill_usage_records (listing_id, usage_status, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_settlement_records (
  settlement_id TEXT PRIMARY KEY,
  usage_id TEXT NOT NULL,
  creator_user_id TEXT NOT NULL,
  status TEXT NOT NULL,
  gross_credits BIGINT NOT NULL,
  platform_fee_credits BIGINT NOT NULL,
  creator_credits BIGINT NOT NULL,
  hold_until TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_settlement_creator
  ON skill_settlement_records (creator_user_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_settlement_payout_records (
  payout_id TEXT PRIMARY KEY,
  settlement_id TEXT NOT NULL,
  creator_user_id TEXT NOT NULL,
  action TEXT NOT NULL,
  status_before TEXT NOT NULL,
  status_after TEXT NOT NULL,
  payout_reference TEXT NOT NULL DEFAULT '',
  reason_code TEXT NOT NULL,
  operator_admin_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_skill_settlement_payout_settlement
  ON skill_settlement_payout_records (settlement_id, created_at DESC);

CREATE TABLE IF NOT EXISTS skill_refund_cases (
  refund_case_id TEXT PRIMARY KEY,
  usage_id TEXT NOT NULL,
  settlement_id TEXT,
  status TEXT NOT NULL,
  reason_code TEXT NOT NULL,
  refund_digest TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_refund_cases_usage
  ON skill_refund_cases (usage_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_review_records (
  review_id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  skill_version_id TEXT NOT NULL,
  status TEXT NOT NULL,
  reviewer_id TEXT,
  decision_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_review_records_status
  ON skill_review_records (status, updated_at DESC);
