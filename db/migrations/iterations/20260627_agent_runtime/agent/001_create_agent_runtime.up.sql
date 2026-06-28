CREATE TABLE agent_sessions (
  id varchar(64) PRIMARY KEY,
  tenant_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  user_id varchar(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  title varchar(128) NOT NULL DEFAULT '',
  last_run_id varchar(64) NOT NULL DEFAULT '',
  last_event_sequence bigint NOT NULL DEFAULT 0,
  snapshot_summary jsonb NOT NULL DEFAULT '{}',
  idempotency_key varchar(160) NOT NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE UNIQUE INDEX ux_agent_sessions_idempotency ON agent_sessions(idempotency_key);
CREATE INDEX idx_agent_sessions_space_project_user_updated ON agent_sessions(space_id, project_id, user_id, updated_at DESC);
CREATE INDEX idx_agent_sessions_deleted ON agent_sessions(deleted_at);

CREATE TABLE agent_runs (
  id varchar(64) PRIMARY KEY,
  session_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  user_id varchar(64) NOT NULL,
  turn_no bigint NOT NULL,
  status varchar(32) NOT NULL,
  input_summary jsonb NOT NULL DEFAULT '{}',
  skill_selection jsonb NOT NULL DEFAULT '{}',
  model_selection_snapshot jsonb NOT NULL DEFAULT '{}',
  runtime_config_version varchar(64) NOT NULL,
  idempotency_key varchar(160) NOT NULL,
  error_code varchar(64) NOT NULL DEFAULT '',
  error_message varchar(512) NOT NULL DEFAULT '',
  trace_id varchar(128) NOT NULL,
  started_at timestamptz NULL,
  completed_at timestamptz NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE UNIQUE INDEX ux_agent_runs_idempotency ON agent_runs(idempotency_key);
CREATE INDEX idx_agent_runs_session_created ON agent_runs(session_id, created_at DESC);
CREATE INDEX idx_agent_runs_project_status_updated ON agent_runs(project_id, status, updated_at DESC);

CREATE TABLE agent_messages (
  id varchar(64) PRIMARY KEY,
  session_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  role varchar(32) NOT NULL,
  content_type varchar(32) NOT NULL,
  content text NOT NULL DEFAULT '',
  content_summary jsonb NOT NULL DEFAULT '{}',
  sequence bigint NOT NULL,
  safety_status varchar(32) NOT NULL DEFAULT 'unchecked',
  metadata jsonb NOT NULL DEFAULT '{}',
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE UNIQUE INDEX ux_agent_messages_session_sequence ON agent_messages(session_id, sequence);
CREATE INDEX idx_agent_messages_run_sequence ON agent_messages(run_id, sequence);

CREATE TABLE agent_events (
  event_id varchar(64) PRIMARY KEY,
  type varchar(96) NOT NULL,
  session_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  actor_user_id varchar(64) NOT NULL,
  sequence bigint NOT NULL,
  component varchar(64) NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}',
  payload_schema_version varchar(32) NOT NULL,
  visibility varchar(32) NOT NULL DEFAULT 'frontend',
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX ux_agent_events_run_sequence ON agent_events(run_id, sequence);
CREATE INDEX idx_agent_events_run_created ON agent_events(run_id, created_at);
CREATE INDEX idx_agent_events_type ON agent_events(type);

CREATE TABLE agent_tool_calls (
  id varchar(64) PRIMARY KEY,
  run_id varchar(64) NOT NULL,
  task_id varchar(64) NOT NULL DEFAULT '',
  tool_name varchar(96) NOT NULL,
  tool_type varchar(48) NOT NULL,
  risk_level varchar(32) NOT NULL DEFAULT 'low',
  status varchar(32) NOT NULL,
  input_summary jsonb NOT NULL DEFAULT '{}',
  output_summary jsonb NOT NULL DEFAULT '{}',
  idempotency_key varchar(160) NOT NULL DEFAULT '',
  timeout_ms integer NOT NULL DEFAULT 0,
  retry_count integer NOT NULL DEFAULT 0,
  error_code varchar(64) NOT NULL DEFAULT '',
  latency_ms bigint NOT NULL DEFAULT 0,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_tool_calls_run_tool ON agent_tool_calls(run_id, tool_name);
CREATE INDEX idx_agent_tool_calls_status_updated ON agent_tool_calls(status, updated_at DESC);
CREATE INDEX idx_agent_tool_calls_idempotency ON agent_tool_calls(idempotency_key);

CREATE TABLE agent_tasks (
  id varchar(64) PRIMARY KEY,
  run_id varchar(64) NOT NULL,
  task_type varchar(48) NOT NULL,
  resource_type varchar(32) NOT NULL DEFAULT '',
  status varchar(32) NOT NULL,
  progress_percent integer NOT NULL DEFAULT 0,
  progress_detail jsonb NOT NULL DEFAULT '{}',
  cancel_requested boolean NOT NULL DEFAULT false,
  external_task_ref varchar(256) NOT NULL DEFAULT '',
  error_code varchar(64) NOT NULL DEFAULT '',
  started_at timestamptz NULL,
  completed_at timestamptz NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_tasks_run_status ON agent_tasks(run_id, status);
CREATE INDEX idx_agent_tasks_status_updated ON agent_tasks(status, updated_at DESC);

CREATE TABLE agent_interrupts (
  id varchar(64) PRIMARY KEY,
  run_id varchar(64) NOT NULL,
  interrupt_type varchar(48) NOT NULL,
  status varchar(32) NOT NULL,
  reason varchar(128) NOT NULL DEFAULT '',
  confirmation_payload jsonb NOT NULL DEFAULT '{}',
  allowed_actions jsonb NOT NULL DEFAULT '[]',
  resume_context jsonb NOT NULL DEFAULT '{}',
  idempotency_key varchar(160) NOT NULL DEFAULT '',
  expires_at timestamptz NOT NULL,
  resolved_at timestamptz NULL,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_interrupts_run_status ON agent_interrupts(run_id, status);
CREATE INDEX idx_agent_interrupts_expires_status ON agent_interrupts(expires_at, status);
CREATE INDEX idx_agent_interrupts_idempotency ON agent_interrupts(id, idempotency_key);

CREATE TABLE agent_artifacts (
  id varchar(64) PRIMARY KEY,
  session_id varchar(64) NOT NULL,
  project_id varchar(64) NOT NULL,
  run_id varchar(64) NOT NULL,
  artifact_type varchar(48) NOT NULL,
  status varchar(32) NOT NULL,
  element_type varchar(48) NOT NULL DEFAULT '',
  content jsonb NOT NULL DEFAULT '{}',
  business_ref_id varchar(64) NOT NULL DEFAULT '',
  visibility varchar(32) NOT NULL DEFAULT 'private',
  version integer NOT NULL DEFAULT 1,
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_artifacts_session_type_updated ON agent_artifacts(session_id, artifact_type, updated_at DESC);
CREATE INDEX idx_agent_artifacts_project_type_updated ON agent_artifacts(project_id, artifact_type, updated_at DESC);

CREATE TABLE agent_safety_evaluations (
  safety_evidence_id varchar(64) PRIMARY KEY,
  scene varchar(48) NOT NULL,
  target_type varchar(48) NOT NULL,
  target_ref_id varchar(64) NOT NULL DEFAULT '',
  evaluated_object_digest varchar(128) NOT NULL,
  policy_version varchar(64) NOT NULL,
  evidence_version varchar(32) NOT NULL,
  result varchar(32) NOT NULL,
  user_visible_reason varchar(512) NOT NULL DEFAULT '',
  source_session_id varchar(64) NOT NULL DEFAULT '',
  source_run_id varchar(64) NOT NULL DEFAULT '',
  source_artifact_id varchar(64) NOT NULL DEFAULT '',
  trace_id varchar(128) NOT NULL,
  evaluated_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_safety_source_run_target ON agent_safety_evaluations(source_run_id, target_type);
CREATE INDEX idx_agent_safety_expires ON agent_safety_evaluations(expires_at);

CREATE TABLE agent_memories (
  id varchar(64) PRIMARY KEY,
  user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  memory_type varchar(48) NOT NULL,
  scope varchar(48) NOT NULL,
  content_summary jsonb NOT NULL DEFAULT '{}',
  authorized boolean NOT NULL DEFAULT false,
  expires_at timestamptz NULL,
  source_session_id varchar(64) NOT NULL DEFAULT '',
  trace_id varchar(128) NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz NULL
);
CREATE INDEX idx_agent_memories_user_scope_updated ON agent_memories(user_id, scope, updated_at DESC);
CREATE INDEX idx_agent_memories_expires ON agent_memories(expires_at);

CREATE TABLE agent_runtime_configs (
  config_key varchar(96) NOT NULL,
  version varchar(64) NOT NULL,
  status varchar(32) NOT NULL,
  owner varchar(64) NOT NULL,
  content jsonb NOT NULL DEFAULT '{}',
  safe_config_refs jsonb NOT NULL DEFAULT '[]',
  activated_at timestamptz NULL,
  deprecated_at timestamptz NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (config_key, version)
);
CREATE INDEX idx_agent_runtime_configs_status ON agent_runtime_configs(status);
