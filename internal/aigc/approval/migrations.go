package approval

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// MigrationStatements are package-owned and safe to run alongside the
// sessionruntime migration. The continuation table definition is intentionally
// identical to sessionruntime.ApprovalContinuation.
var MigrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS aigc_approvals (
		id VARCHAR(128) PRIMARY KEY,
		idempotency_key VARCHAR(256) NOT NULL UNIQUE,
		tenant_id VARCHAR(128),
		user_id VARCHAR(128),
		session_id VARCHAR(128) NOT NULL,
		artifact_type VARCHAR(128) NOT NULL,
		binding_json JSONB NOT NULL,
		review_mode VARCHAR(32) NOT NULL CHECK (review_mode IN ('interrupt', 'durable')),
		execution_mode VARCHAR(32) NOT NULL CHECK (execution_mode IN ('interrupt', 'durable', 'durable_fallback')),
		execution_epoch BIGINT NOT NULL CHECK (execution_epoch > 0),
		status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'stale', 'expired', 'cancelled')),
		decision_version INTEGER NOT NULL DEFAULT 0 CHECK (decision_version >= 0),
		approve_command_json JSONB NOT NULL,
		reject_command_json JSONB NOT NULL,
		checkpoint_mapping_id VARCHAR(256),
		mapping_epoch BIGINT NOT NULL DEFAULT 0 CHECK (mapping_epoch >= 0),
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMPTZ,
		decided_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_approvals_session_status
		ON aigc_approvals (session_id, status, created_at)`,
	`CREATE TABLE IF NOT EXISTS aigc_candidate_approval_batches (
		id VARCHAR(128) PRIMARY KEY,
		idempotency_key VARCHAR(256) NOT NULL UNIQUE,
		session_id VARCHAR(128) NOT NULL,
		storyboard_id VARCHAR(128) NOT NULL,
		expected_storyboard_version INTEGER NOT NULL CHECK (expected_storyboard_version > 0),
		decision VARCHAR(32) NOT NULL CHECK (decision = 'approved'),
		actor_id VARCHAR(128),
		reason TEXT,
		targets_json JSONB NOT NULL DEFAULT '[]'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_candidate_approval_batches_scope
		ON aigc_candidate_approval_batches (session_id, storyboard_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS aigc_approval_decisions (
		approval_id VARCHAR(128) NOT NULL REFERENCES aigc_approvals(id),
		decision_version INTEGER NOT NULL CHECK (decision_version > 0),
		idempotency_key VARCHAR(256) NOT NULL UNIQUE,
		requested_decision VARCHAR(32) NOT NULL,
		effective_status VARCHAR(32) NOT NULL CHECK (effective_status IN ('approved', 'rejected', 'stale', 'expired', 'cancelled')),
		actor_id VARCHAR(128),
		reason TEXT,
		observed_binding_json JSONB,
		command_kind VARCHAR(128),
		command_idempotency_key VARCHAR(256),
		command_payload_json JSONB,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (approval_id, decision_version)
	)`,
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
	`CREATE TABLE IF NOT EXISTS aigc_outbox_events (
		id VARCHAR(256) PRIMARY KEY,
		idempotency_key VARCHAR(256) NOT NULL UNIQUE,
		event_type VARCHAR(128) NOT NULL,
		destination VARCHAR(128) NOT NULL,
		aggregate_type VARCHAR(64) NOT NULL,
		aggregate_id VARCHAR(128) NOT NULL,
		aggregate_version INTEGER NOT NULL,
		session_id VARCHAR(128),
		workflow_run_id VARCHAR(128),
		stage_run_id VARCHAR(128),
		operation_id VARCHAR(128),
		tool_call_id VARCHAR(128),
		batch_id VARCHAR(128),
		job_id VARCHAR(128),
		payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		status VARCHAR(32) NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		published_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_aigc_outbox_events_relay
		ON aigc_outbox_events (status, available_at, created_at)`,
	`CREATE OR REPLACE FUNCTION aigc_reject_approval_review_mode_change()
	RETURNS trigger AS $$
	BEGIN
		IF OLD.review_mode IS DISTINCT FROM NEW.review_mode THEN
			RAISE EXCEPTION 'approval review_mode is immutable';
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_trigger WHERE tgname = 'trg_aigc_approval_review_mode_immutable'
		) THEN
			CREATE TRIGGER trg_aigc_approval_review_mode_immutable
			BEFORE UPDATE ON aigc_approvals
			FOR EACH ROW EXECUTE FUNCTION aigc_reject_approval_review_mode_change();
		END IF;
	END $$`,
}

func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("postgres approval store db is required")
	}
	for index, statement := range MigrationStatements {
		if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate approval statement %d: %w", index+1, err)
		}
	}
	return nil
}
