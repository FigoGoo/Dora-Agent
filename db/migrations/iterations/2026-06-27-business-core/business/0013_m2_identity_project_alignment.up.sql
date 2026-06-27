-- Dora business service migration 0013
-- Owner: 业务微服务后端工程师
-- Scope: M2 identity, admin and project semantic alignment.
-- Lock risk: local development baseline; additive columns and indexes only.

ALTER TABLE business_users
  ADD COLUMN IF NOT EXISTS email_hash varchar(128),
  ADD COLUMN IF NOT EXISTS phone_hash varchar(128),
  ADD COLUMN IF NOT EXISTS disabled_reason varchar(512);

CREATE UNIQUE INDEX IF NOT EXISTS idx_business_users_email_hash_unique
  ON business_users (email_hash)
  WHERE email_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_business_users_phone_hash_unique
  ON business_users (phone_hash)
  WHERE phone_hash IS NOT NULL;

ALTER TABLE auth_sessions
  ADD COLUMN IF NOT EXISTS session_token_hash varchar(128),
  ADD COLUMN IF NOT EXISTS csrf_token_hash varchar(128),
  ADD COLUMN IF NOT EXISTS enterprise_role varchar(32);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_sessions_session_token_hash
  ON auth_sessions (session_token_hash)
  WHERE session_token_hash IS NOT NULL;

ALTER TABLE enterprise_members
  ADD COLUMN IF NOT EXISTS removed_at timestamptz,
  ADD COLUMN IF NOT EXISTS removed_by varchar(64),
  ADD COLUMN IF NOT EXISTS remove_reason varchar(512);

ALTER TABLE enterprise_invites
  ADD COLUMN IF NOT EXISTS target_email_hash varchar(128),
  ADD COLUMN IF NOT EXISTS target_phone_hash varchar(128),
  ADD COLUMN IF NOT EXISTS invite_token_hash varchar(128),
  ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;

CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_invites_token_hash
  ON enterprise_invites (invite_token_hash)
  WHERE invite_token_hash IS NOT NULL;

ALTER TABLE platform_admins
  ADD COLUMN IF NOT EXISTS created_by varchar(64),
  ADD COLUMN IF NOT EXISTS disabled_by varchar(64),
  ADD COLUMN IF NOT EXISTS disabled_reason varchar(512);

ALTER TABLE platform_admin_sessions
  ADD COLUMN IF NOT EXISTS csrf_token_hash varchar(128);

ALTER TABLE admin_login_attempts
  ADD COLUMN IF NOT EXISTS account_hash varchar(128);

CREATE INDEX IF NOT EXISTS idx_admin_login_attempts_account_hash_created
  ON admin_login_attempts (account_hash, created_at DESC);

ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS creative_allowed boolean NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS archive_reason varchar(512),
  ADD COLUMN IF NOT EXISTS archived_by varchar(64),
  ADD COLUMN IF NOT EXISTS last_activity_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_projects_space_owner_status_activity
  ON projects (space_id, owner_user_id, status, last_activity_at DESC);

ALTER TABLE project_assets
  ADD COLUMN IF NOT EXISTS source_session_id varchar(64),
  ADD COLUMN IF NOT EXISTS source_run_id varchar(64),
  ADD COLUMN IF NOT EXISTS source_artifact_id varchar(64),
  ADD COLUMN IF NOT EXISTS source_type varchar(32) NOT NULL DEFAULT 'generated',
  ADD COLUMN IF NOT EXISTS display_order int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS attached_by varchar(64);

CREATE INDEX IF NOT EXISTS idx_project_assets_source_run
  ON project_assets (source_run_id)
  WHERE source_run_id IS NOT NULL;

ALTER TABLE project_works
  ADD COLUMN IF NOT EXISTS created_from_asset_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS created_by varchar(64);

UPDATE auth_sessions
SET session_token_hash = session_token_digest
WHERE session_token_hash IS NULL AND session_token_digest IS NOT NULL;

UPDATE auth_sessions
SET enterprise_role = current_enterprise_role
WHERE enterprise_role IS NULL AND current_enterprise_role IS NOT NULL;

UPDATE enterprise_invites
SET invite_token_hash = invite_code_digest
WHERE invite_token_hash IS NULL AND invite_code_digest IS NOT NULL;

UPDATE projects
SET creative_allowed = (status = 'active' AND creative_status <> 'locked'),
    last_activity_at = COALESCE(last_opened_at, updated_at, created_at)
WHERE last_activity_at IS NOT NULL;

UPDATE project_assets
SET attached_by = attached_by_user_id
WHERE attached_by IS NULL AND attached_by_user_id IS NOT NULL;
