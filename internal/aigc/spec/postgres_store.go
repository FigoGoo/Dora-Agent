package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("final video spec not found")

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

type finalVideoSpecRecord struct {
	ID              string `gorm:"primaryKey;size:128"`
	SessionID       string `gorm:"size:128;index"`
	Version         int    `gorm:"index"`
	Status          string `gorm:"size:64;index"`
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
	return "aigc_final_video_specs"
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres final video spec store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&finalVideoSpecRecord{}); err != nil {
		return fmt.Errorf("migrate final video spec table: %w", err)
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, spec FinalVideoSpec) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	record, err := specToRecord(spec)
	if err != nil {
		return FinalVideoSpec{}, err
	}
	if record.Version <= 0 {
		nextVersion, err := s.nextVersion(ctx, record.ID)
		if err != nil {
			return FinalVideoSpec{}, err
		}
		record.Version = nextVersion
	}
	if record.Status == "" {
		record.Status = StatusDraft
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		var existing finalVideoSpecRecord
		err := s.db.WithContext(ctx).First(&existing, "id = ?", record.ID).Error
		if err == nil && !existing.CreatedAt.IsZero() {
			record.CreatedAt = existing.CreatedAt
		} else {
			record.CreatedAt = now
		}
	}
	record.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&record).Error; err != nil {
		return FinalVideoSpec{}, fmt.Errorf("save final video spec %s: %w", record.ID, err)
	}
	return recordToSpec(record)
}

func (s *PostgresStore) Get(ctx context.Context, specID string) (FinalVideoSpec, error) {
	if s == nil || s.db == nil {
		return FinalVideoSpec{}, fmt.Errorf("postgres final video spec store db is required")
	}
	var record finalVideoSpecRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(specID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: %s", ErrNotFound, specID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get final video spec %s: %w", specID, err)
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
		Order("updated_at DESC").
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FinalVideoSpec{}, fmt.Errorf("%w: session=%s", ErrNotFound, sessionID)
		}
		return FinalVideoSpec{}, fmt.Errorf("get latest final video spec for session %s: %w", sessionID, err)
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
		ID:              spec.ID,
		SessionID:       spec.SessionID,
		Version:         spec.Version,
		Status:          strings.TrimSpace(spec.Status),
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
