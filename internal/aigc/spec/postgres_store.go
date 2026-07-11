package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound           = errors.New("final video spec not found")
	ErrNotLatestReviewing = errors.New("final video spec revision is not the latest reviewing revision")
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) WithTx(tx *gorm.DB) *PostgresStore {
	return &PostgresStore{db: tx}
}

type finalVideoSpecRecord struct {
	RecordID        string `gorm:"primaryKey;size:192"`
	ID              string `gorm:"size:128;uniqueIndex:uidx_aigc_spec_id_version,priority:1"`
	SessionID       string `gorm:"size:128;index"`
	Version         int    `gorm:"index;uniqueIndex:uidx_aigc_spec_id_version,priority:2"`
	Status          string `gorm:"size:64;index"`
	IdempotencyKey  string `gorm:"size:256;uniqueIndex:uidx_aigc_spec_idempotency,where:idempotency_key <> ''"`
	Title           string `gorm:"size:512"`
	VideoType       string `gorm:"size:256"`
	TargetAudience  string `gorm:"size:512"`
	OutputLanguage  string `gorm:"size:128"`
	DurationSeconds int
	AspectRatio     string `gorm:"size:64"`
	NarrativeDriver string `gorm:"size:256"`
	VisualStyle     string `gorm:"type:text"`
	SoundStyle      string `gorm:"type:text"`
	ModelPreference string `gorm:"type:text"`
	Markdown        string `gorm:"type:text"`
	Fields          []byte `gorm:"type:jsonb"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (finalVideoSpecRecord) TableName() string {
	return "aigc_final_video_spec_revisions"
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres final video spec store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&finalVideoSpecRecord{}); err != nil {
		return fmt.Errorf("migrate final video spec table: %w", err)
	}
	// Older builds declared a non-partial unique index. PostgreSQL treats every
	// empty string as the same value, which prevented normal version creation
	// when callers did not provide an idempotency key.
	const legacyIndex = "idx_aigc_final_video_spec_revisions_idempotency_key"
	if s.db.Migrator().HasIndex(&finalVideoSpecRecord{}, legacyIndex) {
		if err := s.db.WithContext(ctx).Migrator().DropIndex(&finalVideoSpecRecord{}, legacyIndex); err != nil {
			return fmt.Errorf("drop legacy final video spec idempotency index: %w", err)
		}
	}
	// Reconcile legacy duplicate reviewing rows before installing the database
	// invariant. The newest candidate remains reviewable; all older approvals
	// will resolve as stale.
	const reconcileReviewing = `
WITH ranked AS (
    SELECT record_id,
           ROW_NUMBER() OVER (
               PARTITION BY session_id
               ORDER BY created_at DESC, version DESC, record_id DESC
           ) AS candidate_rank
    FROM aigc_final_video_spec_revisions
    WHERE status = 'reviewing'
)
UPDATE aigc_final_video_spec_revisions AS target
SET status = ?, updated_at = ?
FROM ranked
WHERE target.record_id = ranked.record_id AND ranked.candidate_rank > 1`
	if err := s.db.WithContext(ctx).Exec(reconcileReviewing, StatusSuperseded, time.Now().UTC()).Error; err != nil {
		return fmt.Errorf("reconcile duplicate reviewing final video specs: %w", err)
	}
	const reviewingIndex = `
CREATE UNIQUE INDEX IF NOT EXISTS uidx_aigc_spec_reviewing_session
ON aigc_final_video_spec_revisions (session_id)
WHERE status = 'reviewing'`
	if err := s.db.WithContext(ctx).Exec(reviewingIndex).Error; err != nil {
		return fmt.Errorf("create final video spec reviewing fence: %w", err)
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, spec FinalVideoSpec) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	if key := strings.TrimSpace(spec.IdempotencyKey); key != "" {
		var existing finalVideoSpecRecord
		err := s.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&existing).Error
		if err == nil {
			return recordToSpec(existing)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("find final video spec by idempotency key: %w", err)
		}
	}
	record, err := specToRecord(spec)
	if err != nil {
		return FinalVideoSpec{}, err
	}
	if record.Status == "" {
		record.Status = StatusDraft
	}
	var saved finalVideoSpecRecord
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize revision creation per session. Besides protecting the single
		// reviewing row, this makes version allocation deterministic for the
		// normal one-spec-id-per-session lifecycle.
		lockKey := "aigc-final-video-spec:" + record.SessionID
		if lockErr := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", lockKey).Error; lockErr != nil {
			return fmt.Errorf("lock final video spec session: %w", lockErr)
		}
		if record.IdempotencyKey != "" {
			var existing finalVideoSpecRecord
			lookupErr := tx.Where("idempotency_key = ?", record.IdempotencyKey).First(&existing).Error
			if lookupErr == nil {
				saved = existing
				return nil
			}
			if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find final video spec by idempotency key: %w", lookupErr)
			}
		}
		if record.Version <= 0 {
			nextVersion, versionErr := s.WithTx(tx).nextVersion(ctx, record.ID)
			if versionErr != nil {
				return versionErr
			}
			record.Version = nextVersion
		}
		record.RecordID = fmt.Sprintf("%s:v%d", record.ID, record.Version)
		now := time.Now().UTC()
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		record.UpdatedAt = now
		if record.Status == StatusReviewing {
			if updateErr := tx.Model(&finalVideoSpecRecord{}).
				Where("session_id = ? AND status = ?", record.SessionID, StatusReviewing).
				Updates(map[string]any{"status": StatusSuperseded, "updated_at": now}).Error; updateErr != nil {
				return fmt.Errorf("supersede previous reviewing final video spec: %w", updateErr)
			}
		}
		if createErr := tx.Create(&record).Error; createErr != nil {
			return createErr
		}
		saved = record
		return nil
	})
	if err != nil {
		if record.IdempotencyKey != "" {
			var existing finalVideoSpecRecord
			if lookupErr := s.db.WithContext(ctx).Where("idempotency_key = ?", record.IdempotencyKey).First(&existing).Error; lookupErr == nil {
				return recordToSpec(existing)
			}
		}
		return FinalVideoSpec{}, fmt.Errorf("save final video spec %s: %w", record.ID, err)
	}
	return recordToSpec(saved)
}

func (s *PostgresStore) Get(ctx context.Context, specID string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(specID)).Order("version DESC").First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: %s", ErrNotFound, specID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get final video spec %s: %w", specID, err)
	}
	return recordToSpec(record)
}

func (s *PostgresStore) GetByIdempotencyKey(ctx context.Context, key string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).Where("idempotency_key = ?", strings.TrimSpace(key)).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: idempotency_key=%s", ErrNotFound, key)
		}
		return FinalVideoSpec{}, fmt.Errorf("get final video spec by idempotency key: %w", err)
	}
	return recordToSpec(record)
}

func (s *PostgresStore) GetLatestBySession(ctx context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return FinalVideoSpec{}, fmt.Errorf("session id is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at DESC, version DESC, record_id DESC").
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: session=%s", ErrNotFound, sessionID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get latest final video spec for session %s: %w", sessionID, err)
	}
	return recordToSpec(record)
}

// GetLatestReviewingBySession returns the only currently reviewable candidate.
// The partial unique index enforces this cardinality in PostgreSQL.
func (s *PostgresStore) GetLatestReviewingBySession(ctx context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return FinalVideoSpec{}, fmt.Errorf("session id is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND status = ?", sessionID, StatusReviewing).
		Order("created_at DESC, version DESC, record_id DESC").
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: reviewing session=%s", ErrNotFound, sessionID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get reviewing final video spec for session %s: %w", sessionID, err)
	}
	return recordToSpec(record)
}

func (s *PostgresStore) GetConfirmedBySession(ctx context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).Where("session_id = ? AND status = ?", strings.TrimSpace(sessionID), StatusConfirmed).Order("created_at DESC, version DESC, record_id DESC").First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: confirmed session=%s", ErrNotFound, sessionID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get confirmed final video spec for session %s: %w", sessionID, err)
	}
	return recordToSpec(record)
}

// GetRevision reads one immutable spec revision instead of silently resolving
// to the latest version. Approval version checks use this method.
func (s *PostgresStore) GetRevision(ctx context.Context, specID string, version int) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).Where("id = ? AND version = ?", strings.TrimSpace(specID), version).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: %s version=%d", ErrNotFound, specID, version)
		}
		return FinalVideoSpec{}, fmt.Errorf("get final video spec revision: %w", err)
	}
	return recordToSpec(record)
}

// DecideRevision performs the deterministic approval command. Approving one
// revision supersedes the previously confirmed revision in the same session.
func (s *PostgresStore) DecideRevision(ctx context.Context, specID string, version int, approved bool) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND version = ?", strings.TrimSpace(specID), version).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s version=%d", ErrNotFound, specID, version)
			}
			return err
		}
		status := StatusRejected
		if approved {
			status = StatusConfirmed
		}
		if record.Status == status {
			return nil
		}
		if record.Status != StatusReviewing {
			return fmt.Errorf("final video spec revision cannot transition from %s", record.Status)
		}
		var latest finalVideoSpecRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND status = ?", record.SessionID, StatusReviewing).
			Order("created_at DESC, version DESC, record_id DESC").
			First(&latest).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotLatestReviewing
			}
			return err
		}
		if latest.RecordID != record.RecordID {
			return fmt.Errorf("%w: current=%s version=%d latest=%s version=%d", ErrNotLatestReviewing, record.ID, record.Version, latest.ID, latest.Version)
		}
		now := time.Now().UTC()
		if approved {
			if err := tx.Model(&finalVideoSpecRecord{}).
				Where("session_id = ? AND status = ? AND record_id <> ?", record.SessionID, StatusConfirmed, record.RecordID).
				Updates(map[string]any{"status": StatusSuperseded, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		record.Status = status
		record.UpdatedAt = now
		return tx.Save(&record).Error
	})
	if err != nil {
		return FinalVideoSpec{}, fmt.Errorf("decide final video spec revision: %w", err)
	}
	return recordToSpec(record)
}

func (s *PostgresStore) nextVersion(ctx context.Context, specID string) (int, error) {
	var version int
	err := s.db.WithContext(ctx).
		Model(&finalVideoSpecRecord{}).
		Where("id = ?", specID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error
	if err != nil {
		return 0, fmt.Errorf("load final video spec version %s: %w", specID, err)
	}
	return version + 1, nil
}

func specToRecord(spec FinalVideoSpec) (finalVideoSpecRecord, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.SessionID = strings.TrimSpace(spec.SessionID)
	if spec.ID == "" {
		return finalVideoSpecRecord{}, fmt.Errorf("final video spec id is required")
	}
	if spec.SessionID == "" {
		return finalVideoSpecRecord{}, fmt.Errorf("session id is required")
	}
	fields, err := json.Marshal(spec.Fields)
	if err != nil {
		return finalVideoSpecRecord{}, fmt.Errorf("marshal final video spec fields: %w", err)
	}
	return finalVideoSpecRecord{
		RecordID:        fmt.Sprintf("%s:v%d", spec.ID, spec.Version),
		ID:              spec.ID,
		SessionID:       spec.SessionID,
		Version:         spec.Version,
		Status:          strings.TrimSpace(spec.Status),
		IdempotencyKey:  strings.TrimSpace(spec.IdempotencyKey),
		Title:           strings.TrimSpace(spec.Title),
		VideoType:       strings.TrimSpace(spec.VideoType),
		TargetAudience:  strings.TrimSpace(spec.TargetAudience),
		OutputLanguage:  strings.TrimSpace(spec.OutputLanguage),
		DurationSeconds: spec.DurationSeconds,
		AspectRatio:     strings.TrimSpace(spec.AspectRatio),
		NarrativeDriver: strings.TrimSpace(spec.NarrativeDriver),
		VisualStyle:     strings.TrimSpace(spec.VisualStyle),
		SoundStyle:      strings.TrimSpace(spec.SoundStyle),
		ModelPreference: strings.TrimSpace(spec.ModelPreference),
		Markdown:        strings.TrimSpace(spec.Markdown),
		Fields:          fields,
		CreatedAt:       spec.CreatedAt,
		UpdatedAt:       spec.UpdatedAt,
	}, nil
}

func recordToSpec(record finalVideoSpecRecord) (FinalVideoSpec, error) {
	spec := FinalVideoSpec{
		ID:              record.ID,
		SessionID:       record.SessionID,
		Version:         record.Version,
		Status:          record.Status,
		IdempotencyKey:  record.IdempotencyKey,
		Title:           record.Title,
		VideoType:       record.VideoType,
		TargetAudience:  record.TargetAudience,
		OutputLanguage:  record.OutputLanguage,
		DurationSeconds: record.DurationSeconds,
		AspectRatio:     record.AspectRatio,
		NarrativeDriver: record.NarrativeDriver,
		VisualStyle:     record.VisualStyle,
		SoundStyle:      record.SoundStyle,
		ModelPreference: record.ModelPreference,
		Markdown:        record.Markdown,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
	if len(record.Fields) > 0 {
		if err := json.Unmarshal(record.Fields, &spec.Fields); err != nil {
			return FinalVideoSpec{}, fmt.Errorf("unmarshal final video spec fields: %w", err)
		}
	}
	return spec, nil
}
