-- Dora business service migration 0012
-- Owner: 业务微服务后端工程师
-- Scope: notifications and notification creation failures.
-- Lock risk: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS notifications (
  id varchar(64) PRIMARY KEY,
  notification_no varchar(64) NOT NULL UNIQUE,
  recipient_user_id varchar(64) NOT NULL,
  recipient_space_id varchar(64),
  recipient_enterprise_id varchar(64),
  notification_type varchar(64) NOT NULL,
  title varchar(160) NOT NULL,
  body varchar(2048) NOT NULL,
  jump_type varchar(64) NOT NULL,
  jump_target_id varchar(64),
  jump_payload_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  source_type varchar(64) NOT NULL,
  source_id varchar(64),
  status varchar(32) NOT NULL DEFAULT 'unread',
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_status_created
  ON notifications (recipient_user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_source
  ON notifications (source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_notifications_space_created
  ON notifications (recipient_space_id, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_create_failures (
  id varchar(64) PRIMARY KEY,
  source_type varchar(64) NOT NULL,
  source_id varchar(64) NOT NULL,
  recipient_user_id varchar(64),
  failure_code varchar(64) NOT NULL,
  failure_summary varchar(512),
  payload_digest varchar(128),
  trace_id varchar(128),
  retry_count integer NOT NULL DEFAULT 0,
  next_retry_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notification_create_failures_source
  ON notification_create_failures (source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_notification_create_failures_retry
  ON notification_create_failures (next_retry_at, retry_count);
