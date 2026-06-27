-- Dora business service migration 0015
-- Owner: 业务微服务后端工程师
-- Scope: Skill test result idempotency and request hash.
-- Lock risk: metadata-only columns and unique index for local M3 baseline.

ALTER TABLE skill_test_runs
  ADD COLUMN IF NOT EXISTS idempotency_key varchar(128),
  ADD COLUMN IF NOT EXISTS request_hash varchar(128);

CREATE UNIQUE INDEX IF NOT EXISTS ux_skill_test_runs_idempotency_key
  ON skill_test_runs (idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_skill_test_runs_request_hash
  ON skill_test_runs (request_hash);
