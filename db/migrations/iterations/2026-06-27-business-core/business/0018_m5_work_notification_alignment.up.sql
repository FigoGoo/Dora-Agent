-- Dora business service migration 0018
-- Owner: 业务微服务后端工程师
-- Scope: M5 work public snapshot, likes and notifications code-plan alignment.
-- Lock risk: local-development additive columns and indexes; no database-level foreign keys.

ALTER TABLE works ADD COLUMN IF NOT EXISTS category varchar(64);
ALTER TABLE works ADD COLUMN IF NOT EXISTS tags jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE works ADD COLUMN IF NOT EXISTS share_status varchar(32) NOT NULL DEFAULT 'private';
ALTER TABLE works ADD COLUMN IF NOT EXISTS current_snapshot_id varchar(64);
ALTER TABLE works ADD COLUMN IF NOT EXISTS last_moderation_record_id varchar(64);
ALTER TABLE works ADD COLUMN IF NOT EXISTS private_reset_at timestamptz;

UPDATE works
SET share_status = CASE
  WHEN status IN ('public', 'shared') THEN 'shared'
  WHEN status = 'taken_down' THEN 'taken_down'
  ELSE 'private'
END
WHERE share_status = 'private' AND status IS NOT NULL;

UPDATE works
SET current_snapshot_id = latest_snapshot_id
WHERE current_snapshot_id IS NULL AND latest_snapshot_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_works_owner_share_created
  ON works (space_id, owner_user_id, share_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_works_private_reset
  ON works (private_reset_at);

ALTER TABLE work_assets ADD COLUMN IF NOT EXISTS work_asset_id varchar(64);
ALTER TABLE work_assets ADD COLUMN IF NOT EXISTS role varchar(32) NOT NULL DEFAULT 'content';
ALTER TABLE work_assets ADD COLUMN IF NOT EXISTS display_order integer NOT NULL DEFAULT 0;
ALTER TABLE work_assets ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

UPDATE work_assets SET work_asset_id = id WHERE work_asset_id IS NULL;
UPDATE work_assets SET role = asset_role WHERE asset_role IS NOT NULL;
UPDATE work_assets SET display_order = sort_order WHERE sort_order IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_work_assets_work_asset
  ON work_assets (work_id, asset_id);

ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS public_work_id varchar(64);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS public_slug varchar(160);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS public_url varchar(512);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS snapshot_payload jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS public_media_refs jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS category varchar(64);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS resource_type varchar(32);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS published_by varchar(64);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS taken_down_by varchar(64);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS taken_down_reason varchar(512);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS safety_evidence_id varchar(128);
ALTER TABLE work_public_snapshots ADD COLUMN IF NOT EXISTS safety_evidence_digest varchar(128);

UPDATE work_public_snapshots SET public_work_id = snapshot_id WHERE public_work_id IS NULL;
UPDATE work_public_snapshots SET public_slug = share_slug WHERE public_slug IS NULL AND share_slug IS NOT NULL;
UPDATE work_public_snapshots SET public_url = share_url WHERE public_url IS NULL AND share_url IS NOT NULL;
UPDATE work_public_snapshots SET snapshot_payload = snapshot_json WHERE snapshot_json IS NOT NULL AND snapshot_payload = '{}'::jsonb;
UPDATE work_public_snapshots SET published_by = published_by_user_id WHERE published_by IS NULL AND published_by_user_id IS NOT NULL;
UPDATE work_public_snapshots SET taken_down_by = taken_down_by_admin_id WHERE taken_down_by IS NULL AND taken_down_by_admin_id IS NOT NULL;
UPDATE work_public_snapshots SET taken_down_reason = take_down_reason WHERE taken_down_reason IS NULL AND take_down_reason IS NOT NULL;
UPDATE work_public_snapshots
SET status = CASE
  WHEN status = 'published' THEN 'active'
  WHEN status = 'private' THEN 'cancelled'
  ELSE status
END
WHERE status IN ('published', 'private');

ALTER TABLE work_public_snapshots ALTER COLUMN public_work_id SET NOT NULL;
ALTER TABLE work_public_snapshots ALTER COLUMN public_slug SET NOT NULL;
ALTER TABLE work_public_snapshots ALTER COLUMN public_url SET NOT NULL;
ALTER TABLE work_public_snapshots ALTER COLUMN published_by SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_work_public_snapshots_public_work
  ON work_public_snapshots (public_work_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_work_public_snapshots_public_slug
  ON work_public_snapshots (public_slug);
CREATE INDEX IF NOT EXISTS idx_work_public_snapshots_public_status
  ON work_public_snapshots (status, category, resource_type, published_at DESC);

ALTER TABLE work_likes ADD COLUMN IF NOT EXISTS like_id varchar(64);
ALTER TABLE work_likes ADD COLUMN IF NOT EXISTS public_work_id varchar(64);
ALTER TABLE work_likes ADD COLUMN IF NOT EXISTS work_id varchar(64);
ALTER TABLE work_likes ADD COLUMN IF NOT EXISTS liked_at timestamptz;

UPDATE work_likes SET like_id = id WHERE like_id IS NULL;
UPDATE work_likes wl
SET public_work_id = wps.public_work_id,
    work_id = wps.work_id
FROM work_public_snapshots wps
WHERE wl.snapshot_id = wps.snapshot_id AND wl.public_work_id IS NULL;
UPDATE work_likes SET liked_at = created_at WHERE liked_at IS NULL AND status = 'liked';

CREATE UNIQUE INDEX IF NOT EXISTS uq_work_likes_public_user
  ON work_likes (public_work_id, user_id);

ALTER TABLE work_categories ADD COLUMN IF NOT EXISTS category_key varchar(64);
UPDATE work_categories SET category_key = category_code WHERE category_key IS NULL AND category_code IS NOT NULL;
ALTER TABLE work_categories ALTER COLUMN category_key SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_work_categories_category_key
  ON work_categories (category_key);

ALTER TABLE work_moderation_records ADD COLUMN IF NOT EXISTS record_id varchar(64);
ALTER TABLE work_moderation_records ADD COLUMN IF NOT EXISTS public_work_id varchar(64);
ALTER TABLE work_moderation_records ADD COLUMN IF NOT EXISTS operator_admin_id varchar(64);

UPDATE work_moderation_records SET record_id = id WHERE record_id IS NULL;
UPDATE work_moderation_records wmr
SET public_work_id = wps.public_work_id
FROM work_public_snapshots wps
WHERE wmr.snapshot_id = wps.snapshot_id AND wmr.public_work_id IS NULL;
UPDATE work_moderation_records SET operator_admin_id = operated_by_admin_id WHERE operator_admin_id IS NULL AND operated_by_admin_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_work_moderation_records_public_created
  ON work_moderation_records (public_work_id, created_at DESC);

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS notification_id varchar(64);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS type varchar(64);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS summary varchar(512);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS related_resource_type varchar(64);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS related_resource_id varchar(64);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS navigation_hint jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS idempotency_key varchar(128);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS trace_id varchar(128);

UPDATE notifications SET notification_id = id WHERE notification_id IS NULL;
UPDATE notifications SET type = notification_type WHERE type IS NULL AND notification_type IS NOT NULL;
UPDATE notifications SET summary = left(body, 512) WHERE summary IS NULL AND body IS NOT NULL;
UPDATE notifications SET related_resource_type = jump_type WHERE related_resource_type IS NULL AND jump_type IS NOT NULL;
UPDATE notifications SET related_resource_id = jump_target_id WHERE related_resource_id IS NULL AND jump_target_id IS NOT NULL;
UPDATE notifications SET navigation_hint = jump_payload_json WHERE navigation_hint = '{}'::jsonb AND jump_payload_json IS NOT NULL;
UPDATE notifications SET idempotency_key = 'seed:' || id WHERE idempotency_key IS NULL;
UPDATE notifications SET trace_id = 'seed-notification' WHERE trace_id IS NULL;

ALTER TABLE notifications ALTER COLUMN notification_id SET NOT NULL;
ALTER TABLE notifications ALTER COLUMN type SET NOT NULL;
ALTER TABLE notifications ALTER COLUMN summary SET NOT NULL;
ALTER TABLE notifications ALTER COLUMN idempotency_key SET NOT NULL;
ALTER TABLE notifications ALTER COLUMN trace_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_notification_id
  ON notifications (notification_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_idempotency_key
  ON notifications (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_notifications_user_read_created
  ON notifications (recipient_user_id, read_at, created_at DESC);

ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS failure_id varchar(64);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS recipient_user_id varchar(64);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS type varchar(64);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS related_resource_type varchar(64);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS related_resource_id varchar(64);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS idempotency_key varchar(128);
ALTER TABLE notification_create_failures ADD COLUMN IF NOT EXISTS error_code varchar(64);

UPDATE notification_create_failures SET failure_id = id WHERE failure_id IS NULL;
UPDATE notification_create_failures SET type = source_type WHERE type IS NULL AND source_type IS NOT NULL;
UPDATE notification_create_failures SET related_resource_type = source_type WHERE related_resource_type IS NULL AND source_type IS NOT NULL;
UPDATE notification_create_failures SET related_resource_id = source_id WHERE related_resource_id IS NULL AND source_id IS NOT NULL;
UPDATE notification_create_failures SET idempotency_key = 'failure:' || id WHERE idempotency_key IS NULL;
UPDATE notification_create_failures SET error_code = failure_code WHERE error_code IS NULL AND failure_code IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_failures_idempotency_key
  ON notification_create_failures (idempotency_key);
