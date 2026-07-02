-- Dora business service migration 0001
-- Owner: 运维治理责任域 / 业务微服务后端工程师
-- Scope: M7 release governance data foundation.
-- Lock risk: create-time only for new governance tables.

CREATE TABLE IF NOT EXISTS release_batches (
  batch_id TEXT PRIMARY KEY,
  batch_name TEXT NOT NULL,
  status TEXT NOT NULL,
  release_scope TEXT NOT NULL,
  feature_flags_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  gates_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  rollback_plan JSONB NOT NULL DEFAULT '{}'::jsonb,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_by TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_release_batches_status_started
  ON release_batches (status, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_release_batches_scope_updated
  ON release_batches (release_scope, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_release_batches_trace
  ON release_batches (trace_id);

CREATE TABLE IF NOT EXISTS migration_jobs (
  job_id TEXT PRIMARY KEY,
  release_batch_id TEXT NOT NULL,
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  dry_run_result JSONB NOT NULL DEFAULT '{}'::jsonb,
  validation_result JSONB NOT NULL DEFAULT '{}'::jsonb,
  idempotency_key TEXT NOT NULL,
  requested_by TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_migration_jobs_batch_status
  ON migration_jobs (release_batch_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_migration_jobs_type_status
  ON migration_jobs (job_type, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_migration_jobs_trace
  ON migration_jobs (trace_id);

CREATE TABLE IF NOT EXISTS contract_fixture_runs (
  run_id TEXT PRIMARY KEY,
  release_batch_id TEXT NOT NULL,
  fixture_version TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  diff_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  contract_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
  idempotency_key TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_contract_fixture_runs_batch_status
  ON contract_fixture_runs (release_batch_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_contract_fixture_runs_target_version
  ON contract_fixture_runs (target, fixture_version, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_contract_fixture_runs_trace
  ON contract_fixture_runs (trace_id);

CREATE TABLE IF NOT EXISTS runtime_health_metrics (
  metric_id TEXT PRIMARY KEY,
  release_batch_id TEXT NOT NULL,
  metric_type TEXT NOT NULL,
  window_start TIMESTAMPTZ NOT NULL,
  window_end TIMESTAMPTZ NOT NULL,
  value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  alert_status TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (metric_type, window_start, window_end)
);

CREATE INDEX IF NOT EXISTS idx_runtime_health_metrics_batch_type
  ON runtime_health_metrics (release_batch_id, metric_type, window_start DESC);
CREATE INDEX IF NOT EXISTS idx_runtime_health_metrics_alert
  ON runtime_health_metrics (alert_status, window_start DESC);
CREATE INDEX IF NOT EXISTS idx_runtime_health_metrics_trace
  ON runtime_health_metrics (trace_id);

CREATE TABLE IF NOT EXISTS operational_incidents (
  incident_id TEXT PRIMARY KEY,
  release_batch_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  impact_summary TEXT NOT NULL,
  resolution_summary TEXT NOT NULL DEFAULT '',
  trace_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
  detected_at TIMESTAMPTZ NOT NULL,
  resolved_at TIMESTAMPTZ,
  created_by TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_operational_incidents_batch_status
  ON operational_incidents (release_batch_id, status, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_operational_incidents_severity_status
  ON operational_incidents (severity, status, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_operational_incidents_trace
  ON operational_incidents (trace_id);
