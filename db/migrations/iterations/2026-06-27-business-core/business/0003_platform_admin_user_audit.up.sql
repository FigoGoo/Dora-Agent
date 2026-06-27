-- Dora business service migration 0003
-- Owner: 业务微服务后端工程师
-- Scope: platform admin bootstrap, login, sessions and login attempts.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS platform_admins (
  id varchar(64) PRIMARY KEY,
  admin_account varchar(128) NOT NULL UNIQUE,
  password_hash varchar(255) NOT NULL,
  display_name varchar(128) NOT NULL,
  role varchar(32) NOT NULL DEFAULT 'super_admin',
  status varchar(32) NOT NULL DEFAULT 'active',
  must_rotate_password boolean NOT NULL DEFAULT true,
  last_login_at timestamptz,
  password_rotated_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_platform_admins_status
  ON platform_admins (status, created_at DESC);

CREATE TABLE IF NOT EXISTS platform_admin_bootstraps (
  id varchar(64) PRIMARY KEY,
  admin_id varchar(64) NOT NULL,
  bootstrap_account varchar(128) NOT NULL,
  initialized_by varchar(64) NOT NULL DEFAULT 'system_seed',
  credential_secret_ref varchar(255),
  status varchar(32) NOT NULL DEFAULT 'initialized',
  initialized_at timestamptz NOT NULL DEFAULT now(),
  rotated_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (bootstrap_account)
);

CREATE INDEX IF NOT EXISTS idx_platform_admin_bootstraps_status
  ON platform_admin_bootstraps (status, initialized_at DESC);

CREATE TABLE IF NOT EXISTS platform_admin_sessions (
  id varchar(64) PRIMARY KEY,
  admin_id varchar(64) NOT NULL,
  session_token_digest varchar(128) NOT NULL UNIQUE,
  status varchar(32) NOT NULL DEFAULT 'active',
  client_ip_digest varchar(128),
  user_agent_digest varchar(128),
  expires_at timestamptz NOT NULL,
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_platform_admin_sessions_admin_status
  ON platform_admin_sessions (admin_id, status, expires_at DESC);

CREATE TABLE IF NOT EXISTS admin_login_attempts (
  id varchar(64) PRIMARY KEY,
  admin_account varchar(128) NOT NULL,
  result varchar(32) NOT NULL,
  failure_reason varchar(64),
  client_ip_digest varchar(128),
  user_agent_digest varchar(128),
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_login_attempts_account_created
  ON admin_login_attempts (admin_account, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_login_attempts_ip_created
  ON admin_login_attempts (client_ip_digest, created_at DESC);
