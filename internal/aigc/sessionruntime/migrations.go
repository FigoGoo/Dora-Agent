package sessionruntime

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	"gorm.io/gorm"
)

// MigrationStatements allows this package to be installed independently from
// the legacy session store while the application is migrated incrementally.
var MigrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS aigc_session_input_counters (
		session_id VARCHAR(128) PRIMARY KEY,
		next_seq BIGINT NOT NULL DEFAULT 0,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS aigc_session_inputs (
		input_id VARCHAR(256) PRIMARY KEY,
		session_id VARCHAR(128) NOT NULL,
		input_type VARCHAR(64) NOT NULL,
		source_id VARCHAR(256) NOT NULL,
		event_id VARCHAR(128) NOT NULL,
		payload_json JSONB NOT NULL,
		priority INTEGER NOT NULL,
		enqueue_seq BIGINT NOT NULL,
		context_message_seq BIGINT NOT NULL DEFAULT 0,
		status VARCHAR(32) NOT NULL,
		turn_id VARCHAR(128),
		claim_owner VARCHAR(128),
		claim_fence BIGINT NOT NULL DEFAULT 0,
		lease_until TIMESTAMPTZ,
		attempts INTEGER NOT NULL DEFAULT 0,
		available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		error_code VARCHAR(128),
		error_message TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMPTZ,
		UNIQUE (session_id, input_type, source_id),
		UNIQUE (session_id, enqueue_seq)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_session_inputs_claim
		ON aigc_session_inputs (session_id, status, priority DESC, enqueue_seq ASC, available_at)`,
	`ALTER TABLE aigc_session_inputs
		ADD COLUMN IF NOT EXISTS context_message_seq BIGINT NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS aigc_session_runtime_leases (
		session_id VARCHAR(128) PRIMARY KEY,
		owner_id VARCHAR(128) NOT NULL,
		fence_token BIGINT NOT NULL CHECK (fence_token > 0),
		lease_until TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS aigc_session_turn_runs (
		turn_id VARCHAR(128) PRIMARY KEY,
		input_id VARCHAR(256) NOT NULL UNIQUE,
		session_id VARCHAR(128) NOT NULL,
		runner_run_id VARCHAR(128) NOT NULL,
		parent_turn_id VARCHAR(128),
		claim_fence BIGINT NOT NULL,
		kind VARCHAR(64) NOT NULL,
		status VARCHAR(32) NOT NULL,
		runner_checkpoint_id VARCHAR(256),
		attempt INTEGER NOT NULL DEFAULT 0,
		context_message_seq BIGINT NOT NULL DEFAULT 0,
		context_seq_frozen BOOLEAN NOT NULL DEFAULT FALSE,
		output_payload_json JSONB,
		output_digest VARCHAR(128),
		error_code VARCHAR(128),
		error_message TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		committed_at TIMESTAMPTZ
	)`,
	`ALTER TABLE aigc_session_turn_runs
		ADD COLUMN IF NOT EXISTS output_payload_json JSONB`,
	`ALTER TABLE aigc_session_turn_runs
		ADD COLUMN IF NOT EXISTS context_message_seq BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE aigc_session_turn_runs
		ADD COLUMN IF NOT EXISTS context_seq_frozen BOOLEAN NOT NULL DEFAULT FALSE`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_session_turn_runs_session
		ON aigc_session_turn_runs (session_id, status)`,
	`CREATE TABLE IF NOT EXISTS aigc_approval_continuations (
		approval_id VARCHAR(128) NOT NULL,
		decision_version INTEGER NOT NULL,
		session_id VARCHAR(128) NOT NULL,
		executor VARCHAR(64) NOT NULL,
		execution_epoch BIGINT NOT NULL CHECK (execution_epoch > 0),
		status VARCHAR(32) NOT NULL,
		lease_owner VARCHAR(128),
		lease_until TIMESTAMPTZ,
		error_code VARCHAR(128),
		error_message TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		applied_at TIMESTAMPTZ,
		PRIMARY KEY (approval_id, decision_version)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_approval_continuations_claim
		ON aigc_approval_continuations (status, executor, lease_until)`,
	`CREATE TABLE IF NOT EXISTS aigc_approval_command_ledger (
		approval_id VARCHAR(128) NOT NULL,
		decision_version INTEGER NOT NULL,
		command_kind VARCHAR(128) NOT NULL,
		execution_epoch BIGINT NOT NULL,
		idempotency_key VARCHAR(256) NOT NULL UNIQUE,
		command_payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		result_payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (approval_id, decision_version, command_kind)
	)`,
}

func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("postgres session runtime store db is required")
	}
	for index, statement := range MigrationStatements {
		if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate session runtime statement %d: %w", index+1, err)
		}
	}
	// Terminal runtime failures are appended atomically with the dead input or
	// turn transition, so the durable event log is part of this store's minimum
	// production schema even when migrations are invoked package-by-package.
	return events.Migrate(ctx, db)
}
