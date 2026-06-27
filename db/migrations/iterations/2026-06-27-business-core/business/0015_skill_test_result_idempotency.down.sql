-- Dora business service migration 0015 rollback

DROP INDEX IF EXISTS idx_skill_test_runs_request_hash;
DROP INDEX IF EXISTS ux_skill_test_runs_idempotency_key;

ALTER TABLE skill_test_runs
  DROP COLUMN IF EXISTS request_hash,
  DROP COLUMN IF EXISTS idempotency_key;
