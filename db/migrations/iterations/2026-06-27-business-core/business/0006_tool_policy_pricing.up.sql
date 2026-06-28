-- Dora business service migration 0006
-- Owner: 业务微服务后端工程师
-- Scope: Tool definitions, execution policy, pricing and whitelist.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS tool_definitions (
  id varchar(64) PRIMARY KEY,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  display_name varchar(128) NOT NULL,
  description varchar(1024),
  status varchar(32) NOT NULL DEFAULT 'active',
  version varchar(64) NOT NULL DEFAULT '1.0.0',
  input_schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tool_name, tool_type, version)
);

CREATE INDEX IF NOT EXISTS idx_tool_definitions_status
  ON tool_definitions (status, tool_type, tool_name);

CREATE TABLE IF NOT EXISTS tool_policies (
  id varchar(64) PRIMARY KEY,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  policy_scope varchar(32) NOT NULL DEFAULT 'global',
  allowed boolean NOT NULL DEFAULT true,
  risk_level varchar(32) NOT NULL DEFAULT 'low',
  requires_confirmation boolean NOT NULL DEFAULT false,
  timeout_ms integer NOT NULL DEFAULT 30000,
  retry_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  cancel_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'active',
  effective_at timestamptz NOT NULL DEFAULT now(),
  expired_at timestamptz,
  changed_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_policies_lookup
  ON tool_policies (tool_name, tool_type, policy_scope, status);
CREATE INDEX IF NOT EXISTS idx_tool_policies_risk
  ON tool_policies (risk_level, requires_confirmation, status);

CREATE TABLE IF NOT EXISTS tool_pricing_policies (
  id varchar(64) PRIMARY KEY,
  pricing_policy_id varchar(64) NOT NULL UNIQUE,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  charge_mode varchar(32) NOT NULL,
  billing_unit varchar(32) NOT NULL,
  unit_points numeric(20,6) NOT NULL DEFAULT 0,
  free_quota integer NOT NULL DEFAULT 0,
  min_charge_points bigint NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'active',
  effective_at timestamptz NOT NULL DEFAULT now(),
  expired_at timestamptz,
  changed_by_admin_id varchar(64),
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_pricing_policies_lookup
  ON tool_pricing_policies (tool_name, tool_type, status, effective_at DESC);
CREATE INDEX IF NOT EXISTS idx_tool_pricing_policies_charge_mode
  ON tool_pricing_policies (charge_mode, status);

CREATE TABLE IF NOT EXISTS tool_whitelist_rules (
  id varchar(64) PRIMARY KEY,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  scope_type varchar(32) NOT NULL,
  scope_id varchar(64) NOT NULL,
  allowed boolean NOT NULL DEFAULT true,
  reason varchar(512),
  status varchar(32) NOT NULL DEFAULT 'active',
  changed_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tool_name, tool_type, scope_type, scope_id)
);

CREATE INDEX IF NOT EXISTS idx_tool_whitelist_rules_scope
  ON tool_whitelist_rules (scope_type, scope_id, status);

CREATE TABLE IF NOT EXISTS tool_policy_change_records (
  id varchar(64) PRIMARY KEY,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  change_type varchar(64) NOT NULL,
  before_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  after_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  changed_by_admin_id varchar(64),
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_policy_change_records_tool_created
  ON tool_policy_change_records (tool_name, tool_type, created_at DESC);
