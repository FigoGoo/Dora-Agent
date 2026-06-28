-- Dora business service migration 0004
-- Owner: 业务微服务后端工程师
-- Scope: projects, project assets and project works.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS projects (
  id varchar(64) PRIMARY KEY,
  project_no varchar(64) NOT NULL UNIQUE,
  owner_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  enterprise_id varchar(64),
  title varchar(160) NOT NULL,
  description varchar(1024),
  status varchar(32) NOT NULL DEFAULT 'active',
  creative_status varchar(32) NOT NULL DEFAULT 'editable',
  cover_asset_id varchar(64),
  last_opened_at timestamptz,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_projects_space_status_updated
  ON projects (space_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_projects_owner_status_updated
  ON projects (owner_user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_projects_enterprise_status
  ON projects (enterprise_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS project_assets (
  id varchar(64) PRIMARY KEY,
  project_id varchar(64) NOT NULL,
  asset_id varchar(64) NOT NULL,
  asset_role varchar(32) NOT NULL DEFAULT 'source',
  attached_by_user_id varchar(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, asset_id, asset_role)
);

CREATE INDEX IF NOT EXISTS idx_project_assets_project
  ON project_assets (project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_project_assets_asset
  ON project_assets (asset_id, status);

CREATE TABLE IF NOT EXISTS project_works (
  id varchar(64) PRIMARY KEY,
  project_id varchar(64) NOT NULL,
  work_id varchar(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, work_id)
);

CREATE INDEX IF NOT EXISTS idx_project_works_project
  ON project_works (project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_project_works_work
  ON project_works (work_id, status);
