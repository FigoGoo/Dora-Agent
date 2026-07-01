CREATE TABLE IF NOT EXISTS tool_plans (
  tool_plan_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  board_id TEXT NOT NULL,
  board_version BIGINT NOT NULL,
  graph_plan_id TEXT NOT NULL,
  status TEXT NOT NULL,
  items JSONB NOT NULL,
  estimated_credits BIGINT NOT NULL,
  currency TEXT NOT NULL,
  confirmation_required BOOLEAN NOT NULL DEFAULT TRUE,
  expires_at TIMESTAMPTZ,
  tool_plan_digest TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tool_plans_run
  ON tool_plans (run_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_tool_plans_board
  ON tool_plans (board_id, board_version);

CREATE TABLE IF NOT EXISTS tool_tasks (
  tool_task_id TEXT PRIMARY KEY,
  tool_plan_id TEXT NOT NULL,
  tool_plan_item_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  status TEXT NOT NULL,
  progress BIGINT NOT NULL DEFAULT 0,
  provider_policy JSONB NOT NULL,
  idempotency_key TEXT NOT NULL,
  input_digest TEXT NOT NULL,
  output_digest TEXT,
  error_code TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tool_plan_id, tool_plan_item_id),
  UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_tool_tasks_plan_status
  ON tool_tasks (tool_plan_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_tool_tasks_run
  ON tool_tasks (run_id, updated_at DESC);
