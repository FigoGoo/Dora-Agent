-- Dora business service migration 0013 rollback

DROP INDEX IF EXISTS idx_project_assets_source_run;
DROP INDEX IF EXISTS idx_projects_space_owner_status_activity;
DROP INDEX IF EXISTS idx_admin_login_attempts_account_hash_created;
DROP INDEX IF EXISTS idx_enterprise_invites_token_hash;
DROP INDEX IF EXISTS idx_auth_sessions_session_token_hash;
DROP INDEX IF EXISTS idx_business_users_phone_hash_unique;
DROP INDEX IF EXISTS idx_business_users_email_hash_unique;

ALTER TABLE project_works
  DROP COLUMN IF EXISTS created_by,
  DROP COLUMN IF EXISTS created_from_asset_ids;

ALTER TABLE project_assets
  DROP COLUMN IF EXISTS attached_by,
  DROP COLUMN IF EXISTS display_order,
  DROP COLUMN IF EXISTS source_type,
  DROP COLUMN IF EXISTS source_artifact_id,
  DROP COLUMN IF EXISTS source_run_id,
  DROP COLUMN IF EXISTS source_session_id;

ALTER TABLE projects
  DROP COLUMN IF EXISTS last_activity_at,
  DROP COLUMN IF EXISTS archived_by,
  DROP COLUMN IF EXISTS archive_reason,
  DROP COLUMN IF EXISTS creative_allowed;

ALTER TABLE admin_login_attempts
  DROP COLUMN IF EXISTS account_hash;

ALTER TABLE platform_admin_sessions
  DROP COLUMN IF EXISTS csrf_token_hash;

ALTER TABLE platform_admins
  DROP COLUMN IF EXISTS disabled_reason,
  DROP COLUMN IF EXISTS disabled_by,
  DROP COLUMN IF EXISTS created_by;

ALTER TABLE enterprise_invites
  DROP COLUMN IF EXISTS cancelled_at,
  DROP COLUMN IF EXISTS invite_token_hash,
  DROP COLUMN IF EXISTS target_phone_hash,
  DROP COLUMN IF EXISTS target_email_hash;

ALTER TABLE enterprise_members
  DROP COLUMN IF EXISTS remove_reason,
  DROP COLUMN IF EXISTS removed_by,
  DROP COLUMN IF EXISTS removed_at;

ALTER TABLE auth_sessions
  DROP COLUMN IF EXISTS enterprise_role,
  DROP COLUMN IF EXISTS csrf_token_hash,
  DROP COLUMN IF EXISTS session_token_hash;

ALTER TABLE business_users
  DROP COLUMN IF EXISTS disabled_reason,
  DROP COLUMN IF EXISTS phone_hash,
  DROP COLUMN IF EXISTS email_hash;
