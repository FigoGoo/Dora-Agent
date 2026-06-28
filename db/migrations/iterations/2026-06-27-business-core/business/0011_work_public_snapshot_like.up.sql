-- Dora business service migration 0011
-- Owner: 业务微服务后端工程师
-- Scope: works, public snapshots, likes, categories and moderation.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS works (
  id varchar(64) PRIMARY KEY,
  work_no varchar(64) NOT NULL UNIQUE,
  project_id varchar(64) NOT NULL,
  owner_user_id varchar(64) NOT NULL,
  space_id varchar(64) NOT NULL,
  title varchar(160) NOT NULL,
  description varchar(2048),
  cover_asset_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'private',
  latest_snapshot_id varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_works_space_status_updated
  ON works (space_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_works_project_status
  ON works (project_id, status);

CREATE TABLE IF NOT EXISTS work_assets (
  id varchar(64) PRIMARY KEY,
  work_id varchar(64) NOT NULL,
  asset_id varchar(64) NOT NULL,
  asset_role varchar(32) NOT NULL DEFAULT 'content',
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (work_id, asset_id, asset_role)
);

CREATE INDEX IF NOT EXISTS idx_work_assets_work
  ON work_assets (work_id, sort_order);

CREATE TABLE IF NOT EXISTS work_public_snapshots (
  id varchar(64) PRIMARY KEY,
  snapshot_id varchar(64) NOT NULL UNIQUE,
  work_id varchar(64) NOT NULL,
  share_slug varchar(128) NOT NULL UNIQUE,
  title varchar(160) NOT NULL,
  description varchar(2048),
  cover_asset_id varchar(64),
  snapshot_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  share_url varchar(1024) NOT NULL,
  visibility varchar(32) NOT NULL DEFAULT 'public',
  status varchar(32) NOT NULL DEFAULT 'published',
  like_count bigint NOT NULL DEFAULT 0,
  published_by_user_id varchar(64) NOT NULL,
  published_at timestamptz NOT NULL,
  taken_down_by_admin_id varchar(64),
  taken_down_at timestamptz,
  take_down_reason varchar(512),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_work_public_snapshots_status_published
  ON work_public_snapshots (status, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_work_public_snapshots_work
  ON work_public_snapshots (work_id, status);

CREATE TABLE IF NOT EXISTS work_likes (
  id varchar(64) PRIMARY KEY,
  snapshot_id varchar(64) NOT NULL,
  user_id varchar(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'liked',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (snapshot_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_work_likes_user_created
  ON work_likes (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS work_categories (
  id varchar(64) PRIMARY KEY,
  category_code varchar(64) NOT NULL UNIQUE,
  display_name varchar(128) NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_work_categories_status_sort
  ON work_categories (status, sort_order);

CREATE TABLE IF NOT EXISTS work_moderation_records (
  id varchar(64) PRIMARY KEY,
  snapshot_id varchar(64) NOT NULL,
  action varchar(32) NOT NULL,
  reason varchar(512),
  before_status varchar(32),
  after_status varchar(32) NOT NULL,
  operated_by_admin_id varchar(64) NOT NULL,
  trace_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_work_moderation_records_snapshot_created
  ON work_moderation_records (snapshot_id, created_at DESC);
