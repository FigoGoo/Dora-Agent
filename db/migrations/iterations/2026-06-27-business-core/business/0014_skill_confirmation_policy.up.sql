-- Dora business service migration 0014
-- Owner: 业务微服务后端工程师
-- Scope: Persist Skill runtime confirmation policy required by M3 Agent execution.
-- Lock risk: metadata-only nullable/default JSONB add for local baseline.

ALTER TABLE skill_versions
  ADD COLUMN IF NOT EXISTS confirmation_policy_json jsonb NOT NULL DEFAULT '{"requires_confirmation":false}'::jsonb;
