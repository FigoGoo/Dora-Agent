-- Dora agent service migration 0001
-- Owner: 运维治理责任域 / Agent Runtime 工程师
-- Scope: M7 agent release audit and runtime health snapshots.
-- Lock risk: create-time only for new Agent Runtime governance tables.

CREATE TABLE IF NOT EXISTS agent_release_audits (
  audit_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  release_batch_id TEXT NOT NULL,
  feature_flags_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  trace_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (run_id, release_batch_id),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_agent_release_audits_run
  ON agent_release_audits (run_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_release_audits_batch
  ON agent_release_audits (release_batch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_release_audits_trace
  ON agent_release_audits (trace_id);

CREATE TABLE IF NOT EXISTS agent_runtime_health_snapshots (
  snapshot_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  graph_plan_id TEXT NOT NULL,
  release_batch_id TEXT NOT NULL,
  task_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  error_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  metric_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  captured_at TIMESTAMPTZ NOT NULL,
  idempotency_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_agent_runtime_health_snapshots_run
  ON agent_runtime_health_snapshots (run_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_health_snapshots_graph_plan
  ON agent_runtime_health_snapshots (graph_plan_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_health_snapshots_batch_status
  ON agent_runtime_health_snapshots (release_batch_id, status, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_health_snapshots_trace
  ON agent_runtime_health_snapshots (trace_id);
