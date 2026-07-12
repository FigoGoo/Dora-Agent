package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type planRunRecord struct {
	ID        string         `gorm:"primaryKey;size:128"`
	SessionID string         `gorm:"index;size:128;not null"`
	Status    string         `gorm:"index;size:32;not null"`
	Version   int            `gorm:"not null"`
	Payload   datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
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
	if err := s.db.WithContext(ctx).AutoMigrate(&planRunRecord{}); err != nil {
		return fmt.Errorf("migrate plan runs: %w", err)
	}
	return nil
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
	if err := ValidateRunTransition(run.Status, run.Status); err != nil {
		return PlanRun{}, err
	}
	run.Version = 1
	record, result, err := encodePlanRunRecord(run)
	if err != nil {
		return PlanRun{}, err
	}
	created := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if created.Error != nil {
		return PlanRun{}, fmt.Errorf("create plan run %q: %w", run.ID, created.Error)
	}
	if created.RowsAffected != 1 {
		return PlanRun{}, fmt.Errorf("%w: run %q", ErrRunAlreadyExists, run.ID)
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
		if err := ValidateRunTransition(current.Status, next.Status); err != nil {
			return err
		}
		next.Version = current.Version + 1
		updatedRecord, cloned, err := encodePlanRunRecord(next)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"session_id": updatedRecord.SessionID,
			"status":     updatedRecord.Status,
			"version":    updatedRecord.Version,
			"payload":    updatedRecord.Payload,
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
		return PlanRun{}, err
	}
	return result, nil
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
		ID: cloned.ID, SessionID: cloned.SessionID, Status: cloned.Status,
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
	if run.ID != record.ID || run.SessionID != record.SessionID || run.Status != record.Status || run.Version != record.Version {
		return PlanRun{}, fmt.Errorf("%w: run %q metadata does not match payload", ErrRunRecordCorrupt, record.ID)
	}
	cloned, err := clonePlanRun(run)
	if err != nil {
		return PlanRun{}, fmt.Errorf("%w: clone run %q: %v", ErrRunRecordCorrupt, record.ID, err)
	}
	return cloned, nil
}
