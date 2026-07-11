package events

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// MigrationStatements is intentionally package-local in ownership so the
// session event log can be installed without changing the shared DB bootstrap.
var MigrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS aigc_session_event_counters (
		session_id VARCHAR(128) PRIMARY KEY,
		next_seq BIGINT NOT NULL DEFAULT 0,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS aigc_session_event_log (
		session_id VARCHAR(128) NOT NULL,
		seq BIGINT NOT NULL,
		event_id VARCHAR(128) NOT NULL,
		event_type VARCHAR(96) NOT NULL,
		producer_kind VARCHAR(64) NOT NULL,
		source_key VARCHAR(512) NOT NULL,
		projection_index INTEGER NOT NULL DEFAULT 0 CHECK (projection_index >= 0),
		surface_id VARCHAR(128),
		data_model_key VARCHAR(256),
		payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (session_id, seq),
		UNIQUE (event_id),
		UNIQUE (session_id, producer_kind, source_key, projection_index)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_session_event_log_tail
		ON aigc_session_event_log (session_id, seq)`,
}

func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("postgres event store db is required")
	}
	for index, statement := range MigrationStatements {
		if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate session event log statement %d: %w", index+1, err)
		}
	}
	return nil
}
