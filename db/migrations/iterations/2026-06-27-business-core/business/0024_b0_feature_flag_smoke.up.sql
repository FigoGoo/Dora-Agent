-- Dora business service migration 0024
-- Owner: 业务微服务后端工程师 (B0 Smoke)
-- Scope: Feature Flag、测试 seed run、fake provider task 和 smoke suite 运行记录。
-- 约束: 无数据库级外键；Smoke 记录用于测试环境验收证据，不承载生产调度。
-- 锁风险: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS system_feature_flags (
  id varchar(64) PRIMARY KEY,
  flag_key varchar(128) NOT NULL UNIQUE,
  enabled boolean NOT NULL,
  default_enabled boolean NOT NULL,
  description varchar(256),
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS test_seed_runs (
  id varchar(64) PRIMARY KEY,
  seed_run_id varchar(64) NOT NULL UNIQUE,
  fixture_id varchar(128) NOT NULL,
  status varchar(32) NOT NULL,
  summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  trace_id varchar(128),
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fake_provider_tasks (
  id varchar(64) PRIMARY KEY,
  provider_task_id varchar(64) NOT NULL UNIQUE,
  provider_key varchar(128) NOT NULL,
  tool_id varchar(128) NOT NULL,
  scenario varchar(32) NOT NULL,
  latency_ms integer NOT NULL DEFAULT 0,
  artifact_uri varchar(512),
  status varchar(32) NOT NULL,
  result_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_fake_provider_tasks_provider_status
  ON fake_provider_tasks (provider_key, status, created_at DESC);

CREATE TABLE IF NOT EXISTS smoke_test_runs (
  id varchar(64) PRIMARY KEY,
  smoke_run_id varchar(64) NOT NULL UNIQUE,
  suite_key varchar(128) NOT NULL,
  status varchar(32) NOT NULL,
  started_at timestamptz NOT NULL,
  finished_at timestamptz,
  summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  trace_id varchar(128),
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_smoke_test_runs_suite_status
  ON smoke_test_runs (suite_key, status, created_at DESC);

CREATE TABLE IF NOT EXISTS smoke_test_steps (
  id varchar(64) PRIMARY KEY,
  smoke_run_id varchar(64) NOT NULL,
  step_key varchar(128) NOT NULL,
  status varchar(32) NOT NULL,
  evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  error_message varchar(512),
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (smoke_run_id, step_key)
);

CREATE INDEX IF NOT EXISTS idx_smoke_test_steps_run_status
  ON smoke_test_steps (smoke_run_id, status);
