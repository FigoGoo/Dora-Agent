-- Dora business service migration 0001
-- Owner: 业务微服务后端工程师
-- Scope: common idempotency and audit tables.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS idempotency_records (
  id varchar(64) PRIMARY KEY,
  idempotency_key varchar(128) NOT NULL,
  request_hash varchar(128) NOT NULL,
  scope varchar(64) NOT NULL,
  actor_user_id varchar(64) NOT NULL,
  space_id varchar(64),
  enterprise_id varchar(64),
  resource_type varchar(64),
  resource_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'processing',
  response_code varchar(64),
  response_body_digest varchar(128),
  response_body_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  locked_until timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (scope, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_records_actor_created
  ON idempotency_records (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_status_locked
  ON idempotency_records (status, locked_until);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_expires
  ON idempotency_records (expires_at);

CREATE TABLE IF NOT EXISTS business_audit_logs (
  id varchar(64) PRIMARY KEY,
  trace_id varchar(128) NOT NULL,
  request_id varchar(128) NOT NULL,
  idempotency_key varchar(128),
  source varchar(32) NOT NULL,
  actor_user_id varchar(64),
  admin_id varchar(64),
  login_identity_type varchar(32) NOT NULL,
  space_id varchar(64),
  enterprise_id varchar(64),
  enterprise_role varchar(64),
  action varchar(96) NOT NULL,
  resource_type varchar(64) NOT NULL,
  resource_id varchar(64),
  result varchar(32) NOT NULL,
  error_code varchar(64),
  before_snapshot_digest varchar(128),
  after_snapshot_digest varchar(128),
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  client_ip_digest varchar(128),
  user_agent_digest varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_audit_logs_actor_created
  ON business_audit_logs (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_admin_created
  ON business_audit_logs (admin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_resource
  ON business_audit_logs (resource_type, resource_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_trace
  ON business_audit_logs (trace_id);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_action_result
  ON business_audit_logs (action, result, created_at DESC);
