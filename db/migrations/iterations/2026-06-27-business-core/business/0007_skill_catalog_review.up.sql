-- Dora business service migration 0007
-- Owner: 业务微服务后端工程师
-- Scope: Skill catalog, versions, test cases, test runs and review records.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS skills (
  id varchar(64) PRIMARY KEY,
  skill_key varchar(128) NOT NULL UNIQUE,
  skill_name varchar(128) NOT NULL,
  skill_scope varchar(32) NOT NULL DEFAULT 'public',
  owner_user_id varchar(64),
  enterprise_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'draft',
  published_version_id varchar(64),
  route_hints_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by_user_id varchar(64),
  created_by_admin_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skills_scope_status
  ON skills (skill_scope, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_skills_enterprise_status
  ON skills (enterprise_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS skill_versions (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version varchar(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'draft',
  skill_spec_json jsonb NOT NULL,
  input_schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  memory_policy_json jsonb NOT NULL DEFAULT '{"enabled":true}'::jsonb,
  changelog varchar(1024),
  submitted_by_user_id varchar(64),
  reviewed_by_admin_id varchar(64),
  submitted_at timestamptz,
  reviewed_at timestamptz,
  published_at timestamptz,
  rolled_back_from_version_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_skill_versions_skill_status
  ON skill_versions (skill_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_versions_review
  ON skill_versions (status, submitted_at DESC);

CREATE TABLE IF NOT EXISTS skill_tool_bindings (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version_id varchar(64) NOT NULL,
  tool_name varchar(128) NOT NULL,
  tool_type varchar(64) NOT NULL,
  required boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (version_id, tool_name, tool_type)
);

CREATE INDEX IF NOT EXISTS idx_skill_tool_bindings_tool
  ON skill_tool_bindings (tool_name, tool_type);

CREATE TABLE IF NOT EXISTS skill_output_element_schemas (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version_id varchar(64) NOT NULL,
  element_type varchar(64) NOT NULL,
  schema_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  required boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (version_id, element_type)
);

CREATE INDEX IF NOT EXISTS idx_skill_output_element_schemas_type
  ON skill_output_element_schemas (element_type);

CREATE TABLE IF NOT EXISTS skill_test_cases (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version_id varchar(64) NOT NULL,
  case_name varchar(128) NOT NULL,
  test_input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  expected_elements_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by_user_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skill_test_cases_version_status
  ON skill_test_cases (version_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS skill_test_runs (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version_id varchar(64) NOT NULL,
  test_case_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'created',
  execution_mode varchar(32) NOT NULL DEFAULT 'sandbox',
  input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  actual_elements_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  safety_evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  error_code varchar(64),
  error_summary varchar(512),
  agent_trace_id varchar(128),
  started_at timestamptz,
  finished_at timestamptz,
  created_by_user_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skill_test_runs_version_status
  ON skill_test_runs (version_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_test_runs_trace
  ON skill_test_runs (agent_trace_id);

CREATE TABLE IF NOT EXISTS skill_review_records (
  id varchar(64) PRIMARY KEY,
  skill_id varchar(64) NOT NULL,
  version_id varchar(64) NOT NULL,
  review_action varchar(32) NOT NULL,
  review_status varchar(32) NOT NULL,
  review_comment varchar(1024),
  reviewed_by_admin_id varchar(64) NOT NULL,
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skill_review_records_version_created
  ON skill_review_records (version_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_review_records_status
  ON skill_review_records (review_status, created_at DESC);
