-- Dora business service migration 0014 rollback
-- Owner: 业务微服务后端工程师
-- Scope: Remove persisted Skill runtime confirmation policy.

ALTER TABLE skill_versions
  DROP COLUMN IF EXISTS confirmation_policy_json;
