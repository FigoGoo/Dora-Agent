package asset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("asset not found")

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) WithTx(tx *gorm.DB) *PostgresStore { return &PostgresStore{db: tx} }
func (s *PostgresStore) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

type record struct {
	ID              string `gorm:"primaryKey;size:128"`
	SessionID       string `gorm:"size:128;index"`
	UserID          string `gorm:"size:128;index"`
	SourceJobID     string `gorm:"size:128;index;uniqueIndex:idx_aigc_assets_source_job_output,priority:1,where:source_job_id <> ''"`
	OutputIndex     int    `gorm:"uniqueIndex:idx_aigc_assets_source_job_output,priority:2,where:source_job_id <> ''"`
	Kind            string `gorm:"size:64;index"`
	Source          string `gorm:"size:64;index"`
	Availability    string `gorm:"size:64;index"`
	MIMEType        string `gorm:"size:256"`
	Filename        string `gorm:"size:512"`
	SizeBytes       int64
	ContentHash     string `gorm:"size:128;index"`
	StorageProvider string `gorm:"size:64"`
	Bucket          string `gorm:"size:256"`
	ObjectKey       string `gorm:"size:1024;index"`
	URL             string `gorm:"type:text"`
	Metadata        []byte `gorm:"type:jsonb"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (record) TableName() string {
	return "aigc_assets"
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres asset store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&record{}); err != nil {
		return fmt.Errorf("migrate asset table: %w", err)
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, asset Asset) (Asset, error) {
	if s == nil || s.db == nil {
		return Asset{}, fmt.Errorf("postgres asset store db is required")
	}
	rec, err := toRecord(asset)
	if err != nil {
		return Asset{}, err
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		var existing record
		err := s.db.WithContext(ctx).First(&existing, "id = ?", rec.ID).Error
		if err == nil && !existing.CreatedAt.IsZero() {
			rec.CreatedAt = existing.CreatedAt
		} else {
			rec.CreatedAt = now
		}
	}
	rec.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&rec).Error; err != nil {
		return Asset{}, fmt.Errorf("save asset %s: %w", rec.ID, err)
	}
	return fromRecord(rec)
}

func (s *PostgresStore) Get(ctx context.Context, assetID string) (Asset, error) {
	if s == nil || s.db == nil {
		return Asset{}, fmt.Errorf("postgres asset store db is required")
	}
	assetID = strings.TrimSpace(assetID)
	var rec record
	if err := s.db.WithContext(ctx).First(&rec, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, fmt.Errorf("%w: %s", ErrNotFound, assetID)
		}
		return Asset{}, fmt.Errorf("get asset %s: %w", assetID, err)
	}
	return fromRecord(rec)
}

func (s *PostgresStore) ListBySession(ctx context.Context, sessionID string) ([]Asset, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres asset store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	var records []record
	if err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list assets for session %s: %w", sessionID, err)
	}
	out := make([]Asset, 0, len(records))
	for _, rec := range records {
		asset, err := fromRecord(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (s *PostgresStore) ListBySourceJob(ctx context.Context, sourceJobID string) ([]Asset, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres asset store db is required")
	}
	sourceJobID = strings.TrimSpace(sourceJobID)
	if sourceJobID == "" {
		return nil, fmt.Errorf("source job id is required")
	}
	var records []record
	if err := s.db.WithContext(ctx).Where("source_job_id = ?", sourceJobID).Order("output_index ASC, id ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list assets for source job %s: %w", sourceJobID, err)
	}
	out := make([]Asset, 0, len(records))
	for _, rec := range records {
		item, err := fromRecord(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func toRecord(asset Asset) (record, error) {
	asset.ID = strings.TrimSpace(asset.ID)
	asset.SessionID = strings.TrimSpace(asset.SessionID)
	asset.UserID = strings.TrimSpace(asset.UserID)
	asset.SourceJobID = strings.TrimSpace(asset.SourceJobID)
	asset.Kind = NormalizeKind(asset.Kind)
	asset.Source = strings.TrimSpace(asset.Source)
	asset.Availability = NormalizeAvailability(asset.Availability)
	if asset.ID == "" {
		return record{}, fmt.Errorf("asset id is required")
	}
	if asset.Kind == "" {
		return record{}, fmt.Errorf("asset kind is required")
	}
	if asset.Source == "" {
		asset.Source = SourceUpload
	}
	if asset.Availability == "" {
		asset.Availability = AvailabilityAvailable
	}
	metadata, err := json.Marshal(asset.Metadata)
	if err != nil {
		return record{}, fmt.Errorf("marshal asset metadata: %w", err)
	}
	return record{
		ID:              asset.ID,
		SessionID:       asset.SessionID,
		UserID:          asset.UserID,
		SourceJobID:     asset.SourceJobID,
		OutputIndex:     asset.OutputIndex,
		Kind:            asset.Kind,
		Source:          asset.Source,
		Availability:    asset.Availability,
		MIMEType:        strings.TrimSpace(asset.MIMEType),
		Filename:        strings.TrimSpace(asset.Filename),
		SizeBytes:       asset.SizeBytes,
		ContentHash:     strings.TrimSpace(asset.ContentHash),
		StorageProvider: strings.TrimSpace(asset.StorageProvider),
		Bucket:          strings.TrimSpace(asset.Bucket),
		ObjectKey:       strings.TrimSpace(asset.ObjectKey),
		URL:             strings.TrimSpace(asset.URL),
		Metadata:        metadata,
		CreatedAt:       asset.CreatedAt,
		UpdatedAt:       asset.UpdatedAt,
	}, nil
}

func fromRecord(rec record) (Asset, error) {
	var metadata map[string]any
	if len(rec.Metadata) > 0 {
		if err := json.Unmarshal(rec.Metadata, &metadata); err != nil {
			return Asset{}, fmt.Errorf("unmarshal asset metadata: %w", err)
		}
	}
	return Asset{
		ID:              rec.ID,
		SessionID:       rec.SessionID,
		UserID:          rec.UserID,
		SourceJobID:     rec.SourceJobID,
		OutputIndex:     rec.OutputIndex,
		Kind:            rec.Kind,
		Source:          rec.Source,
		Availability:    valueOrDefault(rec.Availability, AvailabilityAvailable),
		MIMEType:        rec.MIMEType,
		Filename:        rec.Filename,
		SizeBytes:       rec.SizeBytes,
		ContentHash:     rec.ContentHash,
		StorageProvider: rec.StorageProvider,
		Bucket:          rec.Bucket,
		ObjectKey:       rec.ObjectKey,
		URL:             rec.URL,
		Metadata:        metadata,
		CreatedAt:       rec.CreatedAt,
		UpdatedAt:       rec.UpdatedAt,
	}, nil
}

func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
