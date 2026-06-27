-- Dora business service migration 0002
-- Owner: 业务微服务后端工程师
-- Scope: user identity, sessions, personal spaces, enterprises and members.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS business_users (
  id varchar(64) PRIMARY KEY,
  account_no varchar(64) NOT NULL UNIQUE,
  email varchar(255),
  phone varchar(64),
  password_hash varchar(255),
  display_name varchar(128) NOT NULL,
  avatar_asset_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'active',
  default_space_id varchar(64),
  registered_source varchar(32) NOT NULL DEFAULT 'web',
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  UNIQUE (email),
  UNIQUE (phone)
);

CREATE INDEX IF NOT EXISTS idx_business_users_status_created
  ON business_users (status, created_at DESC);

CREATE TABLE IF NOT EXISTS auth_sessions (
  id varchar(64) PRIMARY KEY,
  user_id varchar(64) NOT NULL,
  login_identity_type varchar(32) NOT NULL,
  current_space_id varchar(64),
  current_enterprise_id varchar(64),
  current_enterprise_role varchar(64),
  session_token_digest varchar(128) NOT NULL UNIQUE,
  refresh_token_digest varchar(128),
  status varchar(32) NOT NULL DEFAULT 'active',
  client_ip_digest varchar(128),
  user_agent_digest varchar(128),
  expires_at timestamptz NOT NULL,
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_status
  ON auth_sessions (user_id, status, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_space
  ON auth_sessions (current_space_id, status);

CREATE TABLE IF NOT EXISTS business_spaces (
  id varchar(64) PRIMARY KEY,
  owner_user_id varchar(64) NOT NULL,
  space_type varchar(32) NOT NULL,
  enterprise_id varchar(64),
  display_name varchar(128) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  credit_account_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_user_id, space_type, enterprise_id)
);

CREATE INDEX IF NOT EXISTS idx_business_spaces_owner
  ON business_spaces (owner_user_id, status);
CREATE INDEX IF NOT EXISTS idx_business_spaces_enterprise
  ON business_spaces (enterprise_id, status);

CREATE TABLE IF NOT EXISTS enterprises (
  id varchar(64) PRIMARY KEY,
  enterprise_no varchar(64) NOT NULL UNIQUE,
  name varchar(160) NOT NULL,
  owner_user_id varchar(64) NOT NULL,
  default_space_id varchar(64),
  credit_account_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_enterprises_owner_status
  ON enterprises (owner_user_id, status);

CREATE TABLE IF NOT EXISTS enterprise_members (
  id varchar(64) PRIMARY KEY,
  enterprise_id varchar(64) NOT NULL,
  user_id varchar(64) NOT NULL,
  role varchar(32) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  joined_at timestamptz,
  invited_by_user_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (enterprise_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_enterprise_members_user
  ON enterprise_members (user_id, status);
CREATE INDEX IF NOT EXISTS idx_enterprise_members_enterprise_role
  ON enterprise_members (enterprise_id, role, status);

CREATE TABLE IF NOT EXISTS enterprise_invites (
  id varchar(64) PRIMARY KEY,
  enterprise_id varchar(64) NOT NULL,
  invitee_email varchar(255),
  invitee_phone varchar(64),
  role varchar(32) NOT NULL,
  invite_code_digest varchar(128) NOT NULL UNIQUE,
  status varchar(32) NOT NULL DEFAULT 'pending',
  invited_by_user_id varchar(64) NOT NULL,
  accepted_by_user_id varchar(64),
  expires_at timestamptz NOT NULL,
  accepted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_enterprise_invites_enterprise_status
  ON enterprise_invites (enterprise_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_enterprise_invites_email_status
  ON enterprise_invites (invitee_email, status);
