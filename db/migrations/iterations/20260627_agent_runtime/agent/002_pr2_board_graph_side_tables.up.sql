CREATE TABLE IF NOT EXISTS agent_run_events (
  event_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  seq BIGINT NOT NULL,
  event_type TEXT NOT NULL,
  payload_schema_version TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  payload_digest TEXT,
  payload JSONB NOT NULL,
  trace_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (run_id, seq),
  UNIQUE (run_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_agent_run_events_replay
  ON agent_run_events (run_id, seq);

CREATE TABLE IF NOT EXISTS creative_boards (
  board_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  graph_plan_id TEXT,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  version BIGINT NOT NULL,
  elements_count BIGINT NOT NULL DEFAULT 0,
  board_digest TEXT NOT NULL,
  approved_at TIMESTAMPTZ,
  approved_by TEXT,
  tool_plan_allowed BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_creative_boards_run
  ON creative_boards (run_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS creative_elements (
  element_id TEXT PRIMARY KEY,
  board_id TEXT NOT NULL,
  element_type TEXT NOT NULL,
  source TEXT NOT NULL,
  status TEXT NOT NULL,
  position JSONB NOT NULL,
  content JSONB NOT NULL,
  linked_asset_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  content_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_creative_elements_board
  ON creative_elements (board_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS board_patches (
  patch_id TEXT PRIMARY KEY,
  board_id TEXT NOT NULL,
  base_version BIGINT NOT NULL,
  target_version BIGINT NOT NULL,
  operation TEXT NOT NULL,
  actor TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload JSONB NOT NULL,
  patch_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (board_id, target_version),
  UNIQUE (board_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_board_patches_replay
  ON board_patches (board_id, target_version);

CREATE TABLE IF NOT EXISTS graph_templates (
  graph_template_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  graph_type TEXT NOT NULL,
  skill_level TEXT NOT NULL,
  entry_node TEXT NOT NULL,
  terminal_nodes JSONB NOT NULL,
  nodes JSONB NOT NULL,
  edges JSONB NOT NULL,
  template_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (graph_template_id, version)
);

CREATE TABLE IF NOT EXISTS graph_plans (
  graph_plan_id TEXT PRIMARY KEY,
  graph_template_id TEXT NOT NULL,
  graph_template_version TEXT NOT NULL,
  run_id TEXT NOT NULL,
  board_id TEXT NOT NULL,
  status TEXT NOT NULL,
  current_node TEXT,
  value_delivered_stage TEXT NOT NULL,
  nodes JSONB NOT NULL,
  edges JSONB NOT NULL,
  graph_plan_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_graph_plans_run
  ON graph_plans (run_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS graph_checkpoints (
  checkpoint_id TEXT PRIMARY KEY,
  graph_plan_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  checkpoint_type TEXT NOT NULL,
  status TEXT NOT NULL,
  state_digest TEXT NOT NULL,
  resumable BOOLEAN NOT NULL DEFAULT FALSE,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_graph_checkpoints_resume
  ON graph_checkpoints (run_id, status, expires_at);
