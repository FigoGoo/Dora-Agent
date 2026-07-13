package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	activeRunSessionIndex       = "idx_aigc_plan_runs_one_active_session"
	requestKeySessionIndex      = "idx_aigc_plan_runs_session_request_key"
	effectiveRequestKeySQL      = "COALESCE(NULLIF(request_key, ''), id)"
	planRunPrimaryKeyConstraint = "aigc_plan_runs_pkey"
	// Serialize every plan-run schema migration before any DDL. PostgreSQL can
	// otherwise deadlock concurrent AutoMigrate transactions on catalog locks.
	planRunMigrationAdvisoryKey int64 = 0x444f5241504c414e
)

type runAlreadyExistsConstraintError struct {
	runID string
	cause error
}

func (e *runAlreadyExistsConstraintError) Error() string {
	return fmt.Sprintf("%s: run %q", ErrRunAlreadyExists, e.runID)
}

func (e *runAlreadyExistsConstraintError) Unwrap() []error {
	return []error{ErrRunAlreadyExists, e.cause}
}

type submitRequestKeyExistsConstraintError struct{ cause error }

func (e *submitRequestKeyExistsConstraintError) Error() string {
	return ErrSubmitRequestKeyExists.Error()
}

func (e *submitRequestKeyExistsConstraintError) Unwrap() []error {
	return []error{ErrSubmitRequestKeyExists, e.cause}
}

type planRunRecord struct {
	ID                       string         `gorm:"primaryKey;size:128"`
	SessionID                string         `gorm:"index;size:128;not null"`
	RequestKey               string         `gorm:"size:128"`
	SubmitRequestFingerprint string         `gorm:"size:71"`
	Status                   string         `gorm:"index;size:32;not null"`
	Version                  int            `gorm:"not null"`
	Payload                  datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

func (planRunRecord) TableName() string { return "aigc_plan_runs" }

// PostgresRunStore persists each PlanRun as one locked JSONB aggregate. Nodes
// deliberately remain inside the aggregate so a run mutation has one CAS
// boundary.
type PostgresRunStore struct {
	db *gorm.DB
}

func NewPostgresRunStore(db *gorm.DB) *PostgresRunStore {
	return &PostgresRunStore{db: db}
}

func (s *PostgresRunStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("postgres run store db is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", planRunMigrationAdvisoryKey).Error; err != nil {
			return fmt.Errorf("lock plan run migrations: %w", err)
		}
		if err := tx.AutoMigrate(&planRunRecord{}); err != nil {
			return fmt.Errorf("migrate plan runs: %w", err)
		}
		if err := tx.Exec(`LOCK TABLE aigc_plan_runs IN ACCESS EXCLUSIVE MODE`).Error; err != nil {
			return fmt.Errorf("lock plan runs for identity migration: %w", err)
		}
		if err := tx.Exec(`UPDATE aigc_plan_runs SET request_key = id WHERE request_key IS NULL OR request_key = ''`).Error; err != nil {
			return fmt.Errorf("backfill plan run request keys: %w", err)
		}
		if err := tx.Exec(`UPDATE aigc_plan_runs
			SET submit_request_fingerprint = payload ->> 'submit_request_fingerprint'
			WHERE (submit_request_fingerprint IS NULL OR submit_request_fingerprint = '')
				AND COALESCE(payload ->> 'submit_request_fingerprint', '') <> ''`).Error; err != nil {
			return fmt.Errorf("promote plan run submit fingerprints: %w", err)
		}
		if err := tx.Exec(`ALTER TABLE aigc_plan_runs ALTER COLUMN request_key DROP NOT NULL`).Error; err != nil {
			return fmt.Errorf("allow rolling plan run request keys: %w", err)
		}
		if err := tx.Exec(`DROP INDEX IF EXISTS ` + requestKeySessionIndex).Error; err != nil {
			return fmt.Errorf("drop legacy plan run request key index: %w", err)
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX ` + requestKeySessionIndex + `
			ON aigc_plan_runs (session_id, (` + effectiveRequestKeySQL + `))`).Error; err != nil {
			return fmt.Errorf("create plan run request key index: %w", err)
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + activeRunSessionIndex + `
			ON aigc_plan_runs (session_id)
			WHERE status IN ('draft', 'running', 'suspended')`).Error; err != nil {
			return fmt.Errorf("create active plan run session index: %w", err)
		}
		return nil
	})
}

// AuthoritativeNow implements the Scheduler's optional shared-clock contract.
// PostgreSQL transaction timestamps provide one time basis across instances.
func (s *PostgresRunStore) AuthoritativeNow(ctx context.Context) (time.Time, error) {
	if s == nil || s.db == nil {
		return time.Time{}, errors.New("postgres run store db is required")
	}
	var now time.Time
	if err := s.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		return time.Time{}, fmt.Errorf("read postgres plan run clock: %w", err)
	}
	if now.IsZero() {
		return time.Time{}, errors.New("postgres plan run clock returned zero time")
	}
	return now, nil
}

func (s *PostgresRunStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	if s == nil || s.db == nil {
		return PlanRun{}, errors.New("postgres run store db is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	if strings.TrimSpace(run.ID) == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	if strings.TrimSpace(run.RequestKey) == "" {
		run.RequestKey = run.ID
	}
	if err := ensureInitialSubmitFingerprint(&run); err != nil {
		return PlanRun{}, err
	}
	if err := ValidateRunTransition(run.Status, run.Status); err != nil {
		return PlanRun{}, err
	}
	run.Version = 1
	record, result, err := encodePlanRunRecord(run)
	if err != nil {
		return PlanRun{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", run.SessionID).Error; err != nil {
			return fmt.Errorf("lock plan run session: %w", err)
		}
		var duplicate int64
		if err := tx.Model(&planRunRecord{}).Where("id = ?", run.ID).Count(&duplicate).Error; err != nil {
			return fmt.Errorf("check plan run %q: %w", run.ID, err)
		}
		if duplicate != 0 {
			return fmt.Errorf("%w: run %q", ErrRunAlreadyExists, run.ID)
		}
		var keyed planRunRecord
		keyQuery := tx.Select("id").Where("session_id = ? AND "+effectiveRequestKeySQL+" = ?", run.SessionID, run.RequestKey).Limit(1).Find(&keyed)
		if keyQuery.Error != nil {
			return fmt.Errorf("check plan run request key: %w", keyQuery.Error)
		}
		if keyQuery.RowsAffected != 0 {
			return fmt.Errorf("%w: request key %q", ErrSubmitRequestKeyExists, run.RequestKey)
		}
		if isActiveRunStatus(run.Status) {
			var active planRunRecord
			query := tx.Select("id").Where("session_id = ? AND status IN ?", run.SessionID, activeRunStatuses()).Limit(1).Find(&active)
			if query.Error != nil {
				return fmt.Errorf("check active plan run: %w", query.Error)
			}
			if query.RowsAffected != 0 {
				return &SessionActiveRunError{ActiveRunID: active.ID}
			}
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("create plan run %q: %w", run.ID, err)
		}
		return nil
	})
	if err != nil {
		return PlanRun{}, mapPlanRunConstraintError(err, run.ID)
	}
	return result, nil
}

func (s *PostgresRunStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	if s == nil || s.db == nil {
		return PlanRun{}, errors.New("postgres run store db is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	var record planRunRecord
	query := s.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&record)
	if query.Error != nil {
		return PlanRun{}, fmt.Errorf("get plan run %q: %w", id, query.Error)
	}
	if query.RowsAffected == 0 {
		return PlanRun{}, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	return decodePlanRunRecord(record)
}

func (s *PostgresRunStore) GetActiveRun(ctx context.Context, sessionID string) (PlanRun, error) {
	if s == nil || s.db == nil {
		return PlanRun{}, errors.New("postgres run store db is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	var records []planRunRecord
	query := s.db.WithContext(ctx).Where("session_id = ? AND status IN ?", sessionID, activeRunStatuses()).Limit(2).Find(&records)
	if query.Error != nil {
		return PlanRun{}, fmt.Errorf("get active plan run for session %q: %w", sessionID, query.Error)
	}
	if len(records) == 0 {
		return PlanRun{}, fmt.Errorf("%w: active run for session %q", ErrRunNotFound, sessionID)
	}
	if len(records) > 1 {
		return PlanRun{}, fmt.Errorf("%w: session %q has multiple active runs", ErrRunRecordCorrupt, sessionID)
	}
	return decodePlanRunRecord(records[0])
}

func (s *PostgresRunStore) GetRunByRequestKey(ctx context.Context, sessionID, requestKey string) (PlanRun, error) {
	if s == nil || s.db == nil {
		return PlanRun{}, errors.New("postgres run store db is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	var records []planRunRecord
	query := s.db.WithContext(ctx).
		Where("session_id = ? AND "+effectiveRequestKeySQL+" = ?", sessionID, requestKey).
		Limit(2).Find(&records)
	if query.Error != nil {
		return PlanRun{}, fmt.Errorf("get plan run by request key: %w", query.Error)
	}
	if len(records) == 0 {
		return PlanRun{}, fmt.Errorf("%w: request key %q", ErrRunNotFound, requestKey)
	}
	if len(records) > 1 {
		return PlanRun{}, fmt.Errorf("%w: duplicate request key for session %q", ErrRunRecordCorrupt, sessionID)
	}
	return decodePlanRunRecord(records[0])
}

func activeRunStatuses() []string {
	return []string{RunStatusDraft, RunStatusRunning, RunStatusSuspended}
}

func (s *PostgresRunStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if mutate == nil {
		return PlanRun{}, errors.New("plan run mutation callback is required")
	}
	return s.mutateRunLocked(ctx, id, expectedVersion, func(run *PlanRun, _ time.Time) error {
		return mutate(run)
	}, false)
}

// MutateRunAtAuthoritativeNow locks the aggregate before sampling PostgreSQL's
// wall clock. Lease eligibility and deadline calculation therefore share the
// same lock and time boundary as the persisted mutation.
func (s *PostgresRunStore) MutateRunAtAuthoritativeNow(
	ctx context.Context,
	id string,
	expectedVersion int,
	mutate func(*PlanRun, time.Time) error,
) (PlanRun, error) {
	if mutate == nil {
		return PlanRun{}, errors.New("plan run timed mutation callback is required")
	}
	return s.mutateRunLocked(ctx, id, expectedVersion, mutate, true)
}

func (s *PostgresRunStore) mutateRunLocked(
	ctx context.Context,
	id string,
	expectedVersion int,
	mutate func(*PlanRun, time.Time) error,
	readClock bool,
) (PlanRun, error) {
	if s == nil || s.db == nil {
		return PlanRun{}, errors.New("postgres run store db is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	var result PlanRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record planRunRecord
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Limit(1).Find(&record)
		if query.Error != nil {
			return fmt.Errorf("lock plan run %q: %w", id, query.Error)
		}
		if query.RowsAffected == 0 {
			return fmt.Errorf("%w: %q", ErrRunNotFound, id)
		}
		current, err := decodePlanRunRecord(record)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return fmt.Errorf("%w: run %q expected version %d, got %d", ErrRunVersionConflict, id, expectedVersion, current.Version)
		}
		var now time.Time
		if readClock {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return fmt.Errorf("read locked postgres plan run clock: %w", err)
			}
			if now.IsZero() {
				return errors.New("locked postgres plan run clock returned zero time")
			}
		}
		next, err := clonePlanRun(current)
		if err != nil {
			return err
		}
		if err := mutate(&next, now); err != nil {
			return err
		}
		if next.ID != current.ID {
			return fmt.Errorf("%w: mutation changed id from %q to %q", ErrRunRecordCorrupt, current.ID, next.ID)
		}
		if next.SessionID != current.SessionID {
			return fmt.Errorf("%w: mutation changed session id", ErrRunRecordCorrupt)
		}
		if next.UserID != current.UserID {
			return fmt.Errorf("%w: mutation changed user id", ErrRunRecordCorrupt)
		}
		if next.RequestKey != current.RequestKey {
			return fmt.Errorf("%w: mutation changed request key", ErrRunRecordCorrupt)
		}
		if next.SubmitRequestFingerprint != current.SubmitRequestFingerprint {
			return fmt.Errorf("%w: mutation changed submit request fingerprint", ErrRunRecordCorrupt)
		}
		if err := ValidateRunTransition(current.Status, next.Status); err != nil {
			return err
		}
		next.Version = current.Version + 1
		updatedRecord, cloned, err := encodePlanRunRecord(next)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"session_id":  updatedRecord.SessionID,
			"request_key": updatedRecord.RequestKey,
			"status":      updatedRecord.Status,
			"version":     updatedRecord.Version,
			"payload":     updatedRecord.Payload,
		}
		updated := tx.Model(&planRunRecord{}).Where("id = ?", id).Updates(updates)
		if updated.Error != nil {
			return fmt.Errorf("update plan run %q: %w", id, updated.Error)
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("%w: update run %q affected %d rows", ErrRunRecordCorrupt, id, updated.RowsAffected)
		}
		result = cloned
		return nil
	})
	if err != nil {
		return PlanRun{}, mapPlanRunConstraintError(err, id)
	}
	return result, nil
}

func mapPlanRunConstraintError(err error, runID string) error {
	var postgresErr *pgconn.PgError
	if !errors.As(err, &postgresErr) || postgresErr.Code != "23505" {
		return err
	}
	switch postgresErr.ConstraintName {
	case planRunPrimaryKeyConstraint:
		return &runAlreadyExistsConstraintError{runID: runID, cause: err}
	case activeRunSessionIndex:
		return &SessionActiveRunError{cause: err}
	case requestKeySessionIndex:
		return &submitRequestKeyExistsConstraintError{cause: err}
	default:
		return err
	}
}

func encodePlanRunRecord(run PlanRun) (planRunRecord, PlanRun, error) {
	cloned, err := clonePlanRun(run)
	if err != nil {
		return planRunRecord{}, PlanRun{}, err
	}
	payload, err := clonePlanRunJSON(cloned)
	if err != nil {
		return planRunRecord{}, PlanRun{}, err
	}
	return planRunRecord{
		ID: cloned.ID, SessionID: cloned.SessionID, RequestKey: cloned.RequestKey, SubmitRequestFingerprint: cloned.SubmitRequestFingerprint, Status: cloned.Status,
		Version: cloned.Version, Payload: datatypes.JSON(payload),
	}, cloned, nil
}

func clonePlanRunJSON(run PlanRun) ([]byte, error) {
	payload, err := json.Marshal(run)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal payload: %w", ErrRunNotSerializable, err)
	}
	return payload, nil
}

func decodePlanRunRecord(record planRunRecord) (PlanRun, error) {
	var run PlanRun
	if err := decodeSingleJSONValue(record.Payload, &run); err != nil {
		return PlanRun{}, fmt.Errorf("%w: decode run %q payload: %v", ErrRunRecordCorrupt, record.ID, err)
	}
	if run.RequestKey == "" {
		if record.RequestKey == "" {
			run.RequestKey = record.ID
		} else {
			run.RequestKey = record.RequestKey
		}
	} else if run.RequestKey != record.RequestKey {
		return PlanRun{}, fmt.Errorf("%w: run %q request key metadata does not match payload", ErrRunRecordCorrupt, record.ID)
	}
	if run.SubmitRequestFingerprint == "" {
		run.SubmitRequestFingerprint = record.SubmitRequestFingerprint
	} else if run.SubmitRequestFingerprint != record.SubmitRequestFingerprint {
		return PlanRun{}, fmt.Errorf("%w: run %q fingerprint metadata does not match payload", ErrRunRecordCorrupt, record.ID)
	}
	if run.ID != record.ID || run.SessionID != record.SessionID || run.Status != record.Status || run.Version != record.Version {
		return PlanRun{}, fmt.Errorf("%w: run %q metadata does not match payload", ErrRunRecordCorrupt, record.ID)
	}
	if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.RequestKey) == "" {
		return PlanRun{}, fmt.Errorf("%w: run %q is missing immutable submit metadata", ErrRunRecordCorrupt, record.ID)
	}
	cloned, err := clonePlanRun(run)
	if err != nil {
		return PlanRun{}, fmt.Errorf("%w: clone run %q: %v", ErrRunRecordCorrupt, record.ID, err)
	}
	return cloned, nil
}
