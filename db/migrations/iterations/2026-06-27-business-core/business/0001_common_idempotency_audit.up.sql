-- Dora business service migration 0001
-- Owner: 业务微服务后端工程师
-- Scope: common idempotency and audit tables.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS idempotency_records (
  id varchar(64) PRIMARY KEY,
  tenant_id varchar(64) NOT NULL,
  space_id varchar(64),
  idempotency_key varchar(128) NOT NULL,
  request_hash varchar(128) NOT NULL,
  scope varchar(128) NOT NULL,
  actor_user_id varchar(64) NOT NULL,
  enterprise_id varchar(64),
  result_ref_type varchar(64),
  result_ref_id varchar(128),
  status varchar(32) NOT NULL DEFAULT 'processing',
  error_code varchar(64),
  locked_until timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, scope, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_records_tenant_space
  ON idempotency_records (tenant_id, space_id);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_actor_created
  ON idempotency_records (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_request_hash
  ON idempotency_records (request_hash);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_result_ref
  ON idempotency_records (result_ref_type, result_ref_id);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_error_code
  ON idempotency_records (error_code);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_status_locked
  ON idempotency_records (status, locked_until);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_expires
  ON idempotency_records (expires_at);

CREATE TABLE IF NOT EXISTS business_audit_logs (
  audit_id varchar(64) PRIMARY KEY,
  trace_id varchar(128) NOT NULL,
  operator_type varchar(32) NOT NULL,
  operator_id varchar(64),
  tenant_id varchar(64) NOT NULL,
  space_id varchar(64),
  business_action varchar(128) NOT NULL,
  resource_type varchar(64) NOT NULL,
  resource_id varchar(64),
  before_status varchar(64),
  after_status varchar(64),
  reason varchar(512),
  result varchar(32) NOT NULL,
  error_code varchar(64),
  metadata_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_audit_logs_trace
  ON business_audit_logs (trace_id);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_operator_created
  ON business_audit_logs (operator_type, operator_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_tenant_space
  ON business_audit_logs (tenant_id, space_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_resource
  ON business_audit_logs (resource_type, resource_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_action_result
  ON business_audit_logs (business_action, result, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_business_audit_logs_error_code
  ON business_audit_logs (error_code);
