-- Dora business service migration 0018 rollback
-- Owner: 业务微服务后端工程师
-- Scope: Remove additive work and notification alignment columns and indexes.

DROP INDEX IF EXISTS uq_notification_failures_idempotency_key;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS error_code;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS related_resource_id;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS related_resource_type;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS type;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS recipient_user_id;
ALTER TABLE notification_create_failures DROP COLUMN IF EXISTS failure_id;

DROP INDEX IF EXISTS idx_notifications_user_read_created;
DROP INDEX IF EXISTS uq_notifications_idempotency_key;
DROP INDEX IF EXISTS uq_notifications_notification_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS trace_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE notifications DROP COLUMN IF EXISTS navigation_hint;
ALTER TABLE notifications DROP COLUMN IF EXISTS related_resource_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS related_resource_type;
ALTER TABLE notifications DROP COLUMN IF EXISTS summary;
ALTER TABLE notifications DROP COLUMN IF EXISTS type;
ALTER TABLE notifications DROP COLUMN IF EXISTS notification_id;

DROP INDEX IF EXISTS idx_work_moderation_records_public_created;
ALTER TABLE work_moderation_records DROP COLUMN IF EXISTS operator_admin_id;
ALTER TABLE work_moderation_records DROP COLUMN IF EXISTS public_work_id;
ALTER TABLE work_moderation_records DROP COLUMN IF EXISTS record_id;

DROP INDEX IF EXISTS uq_work_categories_category_key;
ALTER TABLE work_categories DROP COLUMN IF EXISTS category_key;

DROP INDEX IF EXISTS uq_work_likes_public_user;
ALTER TABLE work_likes DROP COLUMN IF EXISTS liked_at;
ALTER TABLE work_likes DROP COLUMN IF EXISTS work_id;
ALTER TABLE work_likes DROP COLUMN IF EXISTS public_work_id;
ALTER TABLE work_likes DROP COLUMN IF EXISTS like_id;

DROP INDEX IF EXISTS idx_work_public_snapshots_public_status;
DROP INDEX IF EXISTS uq_work_public_snapshots_public_slug;
DROP INDEX IF EXISTS uq_work_public_snapshots_public_work;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS safety_evidence_digest;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS safety_evidence_id;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS taken_down_reason;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS taken_down_by;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS published_by;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS resource_type;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS category;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS public_media_refs;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS snapshot_payload;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS public_url;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS public_slug;
ALTER TABLE work_public_snapshots DROP COLUMN IF EXISTS public_work_id;

DROP INDEX IF EXISTS uq_work_assets_work_asset;
ALTER TABLE work_assets DROP COLUMN IF EXISTS updated_at;
ALTER TABLE work_assets DROP COLUMN IF EXISTS display_order;
ALTER TABLE work_assets DROP COLUMN IF EXISTS role;
ALTER TABLE work_assets DROP COLUMN IF EXISTS work_asset_id;

DROP INDEX IF EXISTS idx_works_private_reset;
DROP INDEX IF EXISTS idx_works_owner_share_created;
ALTER TABLE works DROP COLUMN IF EXISTS private_reset_at;
ALTER TABLE works DROP COLUMN IF EXISTS last_moderation_record_id;
ALTER TABLE works DROP COLUMN IF EXISTS current_snapshot_id;
ALTER TABLE works DROP COLUMN IF EXISTS share_status;
ALTER TABLE works DROP COLUMN IF EXISTS tags;
ALTER TABLE works DROP COLUMN IF EXISTS category;
