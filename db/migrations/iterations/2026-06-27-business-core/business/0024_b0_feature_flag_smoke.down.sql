-- Dora business service migration 0024 (down)
-- 回滚 B0 Feature Flag / Smoke 表。

DROP INDEX IF EXISTS idx_smoke_test_steps_run_status;
DROP TABLE IF EXISTS smoke_test_steps;

DROP INDEX IF EXISTS idx_smoke_test_runs_suite_status;
DROP TABLE IF EXISTS smoke_test_runs;

DROP INDEX IF EXISTS idx_fake_provider_tasks_provider_status;
DROP TABLE IF EXISTS fake_provider_tasks;

DROP TABLE IF EXISTS test_seed_runs;
DROP TABLE IF EXISTS system_feature_flags;
