package artifact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPostgresStore(db *gorm.DB) *PostgresStore { return &PostgresStore{db: db, now: time.Now} }

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("artifact postgres db is required")
	}
	return s.db.WithContext(ctx).AutoMigrate(&Revision{}, &ReviewCommandReceipt{})
}

func (s *PostgresStore) CreateRevision(ctx context.Context, revision Revision) (CreateResult, error) {
	if s == nil || s.db == nil {
		return CreateResult{}, fmt.Errorf("artifact postgres db is required")
	}
	revision = normalizeCreateRequest(revision)
	if err := revision.Validate(); err != nil {
		return CreateResult{}, err
	}
	var result CreateResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockArtifactScope(tx, revision.SessionID, revision.Kind); err != nil {
			return err
		}
		var existing Revision
		if err := tx.First(&existing, "idempotency_key = ?", revision.IdempotencyKey).Error; err == nil {
			if !sameCreateRequest(existing, revision) {
				return fmt.Errorf("%w: idempotency_key=%s", ErrIdempotencyConflict, revision.IdempotencyKey)
			}
			result.Revision = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if revision.Version == 0 {
			var version int
			if err := tx.Model(&Revision{}).Where("session_id = ? AND kind = ?", revision.SessionID, revision.Kind).Select("COALESCE(MAX(version), 0)").Scan(&version).Error; err != nil {
				return err
			}
			revision.Version = version + 1
		}
		now := s.now().UTC()
		revision.CreatedAt, revision.UpdatedAt = now, now
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		result = CreateResult{Revision: revision, Created: true}
		return nil
	})
	if err != nil {
		// A concurrent creator may have won the unique idempotency-key race and
		// aborted this transaction. Read the winner outside the failed transaction
		// and apply the same immutable request validation.
		if existing, lookupErr := s.GetByIdempotencyKey(ctx, revision.IdempotencyKey); lookupErr == nil {
			if !sameCreateRequest(existing, revision) {
				return CreateResult{}, fmt.Errorf("%w: idempotency_key=%s", ErrIdempotencyConflict, revision.IdempotencyKey)
			}
			return CreateResult{Revision: existing}, nil
		}
		return CreateResult{}, fmt.Errorf("create artifact revision: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Revision, error) {
	var revision Revision
	if s == nil || s.db == nil {
		return revision, fmt.Errorf("artifact postgres db is required")
	}
	if err := s.db.WithContext(ctx).First(&revision, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Revision{}, ErrNotFound
		}
		return Revision{}, fmt.Errorf("get artifact: %w", err)
	}
	return revision, nil
}

func (s *PostgresStore) GetByIdempotencyKey(ctx context.Context, key string) (Revision, error) {
	var revision Revision
	if s == nil || s.db == nil {
		return revision, fmt.Errorf("artifact postgres db is required")
	}
	if err := s.db.WithContext(ctx).First(&revision, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Revision{}, ErrNotFound
		}
		return Revision{}, fmt.Errorf("get artifact by idempotency key: %w", err)
	}
	return revision, nil
}

func (s *PostgresStore) GetLatest(ctx context.Context, sessionID, kind string) (Revision, error) {
	var revision Revision
	if s == nil || s.db == nil {
		return revision, fmt.Errorf("artifact postgres db is required")
	}
	err := s.db.WithContext(ctx).Where("session_id = ? AND kind = ?", strings.TrimSpace(sessionID), strings.TrimSpace(kind)).Order("version DESC").First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, fmt.Errorf("get latest artifact: %w", err)
	}
	return revision, nil
}

func (s *PostgresStore) ApplyReview(ctx context.Context, command ReviewCommand) (ReviewResult, error) {
	if s == nil || s.db == nil {
		return ReviewResult{}, fmt.Errorf("artifact postgres db is required")
	}
	command = command.normalize()
	if err := command.Validate(); err != nil {
		return ReviewResult{}, err
	}
	var result ReviewResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockArtifactScope(tx, command.SessionID, command.ArtifactKind); err != nil {
			return err
		}
		if receipt, found, err := findReviewReceipt(tx, command.IdempotencyKey); err != nil {
			return err
		} else if found {
			if !sameReviewCommand(receipt, command) {
				return fmt.Errorf("%w: artifact review command key=%s", ErrIdempotencyConflict, command.IdempotencyKey)
			}
			result = ReviewResult{Revision: receipt.Result, Receipt: receipt}
			return nil
		}

		var revision Revision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&revision, "id = ?", command.ArtifactID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		// A concurrent caller with the same key may have committed while this
		// transaction waited on the artifact row lock. Re-read the receipt after
		// acquiring that lock before inspecting the mutable lifecycle state.
		if receipt, found, err := findReviewReceipt(tx, command.IdempotencyKey); err != nil {
			return err
		} else if found {
			if !sameReviewCommand(receipt, command) {
				return fmt.Errorf("%w: artifact review command key=%s", ErrIdempotencyConflict, command.IdempotencyKey)
			}
			result = ReviewResult{Revision: receipt.Result, Receipt: receipt}
			return nil
		}
		if revision.SessionID != command.SessionID || revision.Kind != command.ArtifactKind || revision.Version != command.ArtifactVersion {
			return fmt.Errorf("%w: artifact review target changed", ErrIdempotencyConflict)
		}
		if revision.Status != command.ExpectedStatus {
			return fmt.Errorf("%w: status=%s", ErrNotReviewable, revision.Status)
		}
		if command.RequireLatest {
			var latest Revision
			err := tx.Where("session_id = ? AND kind = ?", command.SessionID, command.ArtifactKind).Order("version DESC").First(&latest).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStale
			}
			if err != nil {
				return err
			}
			if latest.ID != revision.ID || latest.Version != revision.Version {
				return fmt.Errorf("%w: latest=%s/v%d target=%s/v%d", ErrStale, latest.ID, latest.Version, revision.ID, revision.Version)
			}
		}
		now := s.now().UTC()
		if command.Decision == ReviewDecisionApprove {
			if err := tx.Model(&Revision{}).
				Where("session_id = ? AND kind = ? AND status = ? AND id <> ?", revision.SessionID, revision.Kind, StatusActive, revision.ID).
				Updates(map[string]any{"status": StatusSuperseded, "updated_at": now}).Error; err != nil {
				return err
			}
			revision.Status, revision.ActivatedAt = StatusActive, &now
		} else {
			revision.Status = StatusRejected
		}
		revision.UpdatedAt = now
		if err := tx.Save(&revision).Error; err != nil {
			return err
		}
		receipt := ReviewCommandReceipt{
			IdempotencyKey: command.IdempotencyKey, SessionID: revision.SessionID, ArtifactID: revision.ID, ArtifactKind: revision.Kind,
			ArtifactVersion: revision.Version, ExpectedStatus: command.ExpectedStatus, Decision: command.Decision,
			RequireLatest: command.RequireLatest, Result: revision, CreatedAt: now,
		}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		result = ReviewResult{Revision: revision, Receipt: receipt, Applied: true}
		return nil
	})
	if err == nil {
		return result, nil
	}
	// A same-key concurrent transaction can win the receipt insert after this
	// transaction performed its first lookup. Its unique-key error rolls back
	// all local artifact mutations; the committed winner is then authoritative.
	if receipt, lookupErr := s.GetReviewReceipt(ctx, command.IdempotencyKey); lookupErr == nil {
		if !sameReviewCommand(receipt, command) {
			return ReviewResult{}, fmt.Errorf("%w: artifact review command key=%s", ErrIdempotencyConflict, command.IdempotencyKey)
		}
		return ReviewResult{Revision: receipt.Result, Receipt: receipt}, nil
	}
	return ReviewResult{}, fmt.Errorf("apply artifact review command: %w", err)
}

func (s *PostgresStore) GetReviewReceipt(ctx context.Context, idempotencyKey string) (ReviewCommandReceipt, error) {
	if s == nil || s.db == nil {
		return ReviewCommandReceipt{}, fmt.Errorf("artifact postgres db is required")
	}
	receipt, found, err := findReviewReceipt(s.db.WithContext(ctx), strings.TrimSpace(idempotencyKey))
	if err != nil {
		return ReviewCommandReceipt{}, fmt.Errorf("get artifact review receipt: %w", err)
	}
	if !found {
		return ReviewCommandReceipt{}, ErrNotFound
	}
	return receipt, nil
}

func findReviewReceipt(db *gorm.DB, idempotencyKey string) (ReviewCommandReceipt, bool, error) {
	var receipt ReviewCommandReceipt
	err := db.First(&receipt, "idempotency_key = ?", strings.TrimSpace(idempotencyKey)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ReviewCommandReceipt{}, false, nil
	}
	return receipt, err == nil, err
}

func lockArtifactScope(tx *gorm.DB, sessionID, kind string) error {
	key := "aigc-artifact:" + strings.TrimSpace(sessionID) + ":" + strings.TrimSpace(kind)
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("lock artifact scope: %w", err)
	}
	return nil
}

func (s *PostgresStore) Activate(ctx context.Context, id string, expectedVersion int) (Revision, error) {
	return s.transition(ctx, id, expectedVersion, StatusActive)
}

func (s *PostgresStore) Reject(ctx context.Context, id string, expectedVersion int) (Revision, error) {
	return s.transition(ctx, id, expectedVersion, StatusRejected)
}

func (s *PostgresStore) transition(ctx context.Context, id string, expectedVersion int, status string) (Revision, error) {
	var revision Revision
	if s == nil || s.db == nil {
		return revision, fmt.Errorf("artifact postgres db is required")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&revision, "id = ?", strings.TrimSpace(id)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if expectedVersion > 0 && revision.Version != expectedVersion {
			return fmt.Errorf("artifact version conflict: current=%d expected=%d", revision.Version, expectedVersion)
		}
		now := s.now().UTC()
		if status == StatusActive {
			if err := tx.Model(&Revision{}).Where("session_id = ? AND kind = ? AND status = ? AND id <> ?", revision.SessionID, revision.Kind, StatusActive, revision.ID).Updates(map[string]any{"status": StatusSuperseded, "updated_at": now}).Error; err != nil {
				return err
			}
			revision.ActivatedAt = &now
		}
		revision.Status, revision.UpdatedAt = status, now
		return tx.Save(&revision).Error
	})
	if err != nil {
		return Revision{}, fmt.Errorf("transition artifact: %w", err)
	}
	return revision, nil
}

var _ Store = (*PostgresStore)(nil)
