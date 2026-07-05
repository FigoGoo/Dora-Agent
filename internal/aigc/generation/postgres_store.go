package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("generation job not found")

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

type jobRecord struct {
	ID             string `gorm:"primaryKey;size:128"`
	SessionID      string `gorm:"size:128;index"`
	StoryboardID   string `gorm:"size:128;index"`
	ToolCallID     string `gorm:"size:128;index"`
	IdempotencyKey string `gorm:"size:256;uniqueIndex"`
	Provider       string `gorm:"size:64;index"`
	TargetType     string `gorm:"size:64;index"`
	TargetID       string `gorm:"size:128;index"`
	Status         string `gorm:"size:64;index"`
	RetryCount     int
	MaxRetries     int
	StatusVersion  int
	Payload        []byte `gorm:"type:jsonb"`
	Result         []byte `gorm:"type:jsonb"`
	ResultAssetIDs []byte `gorm:"type:jsonb"`
	ErrorCode      string `gorm:"size:128"`
	ErrorMessage   string `gorm:"type:text"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (jobRecord) TableName() string {
	return "aigc_generation_jobs"
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres generation job store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&jobRecord{}); err != nil {
		return fmt.Errorf("migrate generation job table: %w", err)
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, job GenerationJob) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres generation job store db is required")
	}
	rec, err := toRecord(job)
	if err != nil {
		return GenerationJob{}, err
	}
	if rec.StatusVersion <= 0 {
		rec.StatusVersion = 1
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		var existing jobRecord
		err := s.db.WithContext(ctx).First(&existing, "id = ?", rec.ID).Error
		if err == nil && !existing.CreatedAt.IsZero() {
			rec.CreatedAt = existing.CreatedAt
		} else {
			rec.CreatedAt = now
		}
	}
	rec.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&rec).Error; err != nil {
		return GenerationJob{}, fmt.Errorf("save generation job %s: %w", rec.ID, err)
	}
	return fromRecord(rec)
}

func (s *PostgresStore) Get(ctx context.Context, jobID string) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres generation job store db is required")
	}
	var rec jobRecord
	if err := s.db.WithContext(ctx).First(&rec, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return GenerationJob{}, fmt.Errorf("%w: %s", ErrNotFound, jobID)
		}
		return GenerationJob{}, fmt.Errorf("get generation job %s: %w", jobID, err)
	}
	return fromRecord(rec)
}

func (s *PostgresStore) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres generation job store db is required")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	var rec jobRecord
	if err := s.db.WithContext(ctx).First(&rec, "idempotency_key = ?", idempotencyKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return GenerationJob{}, fmt.Errorf("%w: idempotency_key=%s", ErrNotFound, idempotencyKey)
		}
		return GenerationJob{}, fmt.Errorf("get generation job by idempotency key %s: %w", idempotencyKey, err)
	}
	return fromRecord(rec)
}

func (s *PostgresStore) ListBySession(ctx context.Context, sessionID string) ([]GenerationJob, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres generation job store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	var records []jobRecord
	if err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("updated_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list generation jobs for session %s: %w", sessionID, err)
	}
	out := make([]GenerationJob, 0, len(records))
	for _, rec := range records {
		job, err := fromRecord(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, jobID string, status string, update StatusUpdate) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres generation job store db is required")
	}
	status = NormalizeStatus(status)
	if status == "" {
		return GenerationJob{}, fmt.Errorf("generation job status is invalid")
	}
	var out GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec jobRecord
		if err := tx.First(&rec, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrNotFound, jobID)
			}
			return fmt.Errorf("get generation job %s: %w", jobID, err)
		}
		rec.Status = status
		rec.StatusVersion++
		rec.UpdatedAt = time.Now()
		rec.ErrorCode = strings.TrimSpace(update.ErrorCode)
		rec.ErrorMessage = strings.TrimSpace(update.ErrorMessage)
		if update.ResultAssetIDs != nil {
			raw, err := json.Marshal(update.ResultAssetIDs)
			if err != nil {
				return fmt.Errorf("marshal generation job result assets: %w", err)
			}
			rec.ResultAssetIDs = raw
		}
		if update.Result != nil {
			raw, err := json.Marshal(update.Result)
			if err != nil {
				return fmt.Errorf("marshal generation job result: %w", err)
			}
			rec.Result = raw
		}
		if err := tx.Save(&rec).Error; err != nil {
			return fmt.Errorf("update generation job %s status: %w", jobID, err)
		}
		var err error
		out, err = fromRecord(rec)
		return err
	})
	if err != nil {
		return GenerationJob{}, err
	}
	return out, nil
}

func toRecord(job GenerationJob) (jobRecord, error) {
	job.ID = strings.TrimSpace(job.ID)
	job.SessionID = strings.TrimSpace(job.SessionID)
	job.IdempotencyKey = strings.TrimSpace(job.IdempotencyKey)
	job.Status = NormalizeStatus(job.Status)
	if job.ID == "" {
		return jobRecord{}, fmt.Errorf("generation job id is required")
	}
	if job.SessionID == "" {
		return jobRecord{}, fmt.Errorf("session id is required")
	}
	if job.IdempotencyKey == "" {
		return jobRecord{}, fmt.Errorf("idempotency key is required")
	}
	if job.Status == "" {
		job.Status = StatusQueued
	}
	payload, err := json.Marshal(job.Payload)
	if err != nil {
		return jobRecord{}, fmt.Errorf("marshal generation job payload: %w", err)
	}
	result, err := json.Marshal(job.Result)
	if err != nil {
		return jobRecord{}, fmt.Errorf("marshal generation job result: %w", err)
	}
	resultAssetIDs, err := json.Marshal(job.ResultAssetIDs)
	if err != nil {
		return jobRecord{}, fmt.Errorf("marshal generation job result assets: %w", err)
	}
	return jobRecord{
		ID:             job.ID,
		SessionID:      job.SessionID,
		StoryboardID:   strings.TrimSpace(job.StoryboardID),
		ToolCallID:     strings.TrimSpace(job.ToolCallID),
		IdempotencyKey: job.IdempotencyKey,
		Provider:       strings.TrimSpace(job.Provider),
		TargetType:     strings.TrimSpace(job.TargetType),
		TargetID:       strings.TrimSpace(job.TargetID),
		Status:         job.Status,
		RetryCount:     job.RetryCount,
		MaxRetries:     job.MaxRetries,
		StatusVersion:  job.StatusVersion,
		Payload:        payload,
		Result:         result,
		ResultAssetIDs: resultAssetIDs,
		ErrorCode:      strings.TrimSpace(job.ErrorCode),
		ErrorMessage:   strings.TrimSpace(job.ErrorMessage),
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
	}, nil
}

func fromRecord(rec jobRecord) (GenerationJob, error) {
	var payload map[string]any
	if len(rec.Payload) > 0 {
		if err := json.Unmarshal(rec.Payload, &payload); err != nil {
			return GenerationJob{}, fmt.Errorf("unmarshal generation job payload: %w", err)
		}
	}
	var result map[string]any
	if len(rec.Result) > 0 {
		if err := json.Unmarshal(rec.Result, &result); err != nil {
			return GenerationJob{}, fmt.Errorf("unmarshal generation job result: %w", err)
		}
	}
	var resultAssetIDs []string
	if len(rec.ResultAssetIDs) > 0 {
		if err := json.Unmarshal(rec.ResultAssetIDs, &resultAssetIDs); err != nil {
			return GenerationJob{}, fmt.Errorf("unmarshal generation job result assets: %w", err)
		}
	}
	return GenerationJob{
		ID:             rec.ID,
		SessionID:      rec.SessionID,
		StoryboardID:   rec.StoryboardID,
		ToolCallID:     rec.ToolCallID,
		IdempotencyKey: rec.IdempotencyKey,
		Provider:       rec.Provider,
		TargetType:     rec.TargetType,
		TargetID:       rec.TargetID,
		Status:         rec.Status,
		RetryCount:     rec.RetryCount,
		MaxRetries:     rec.MaxRetries,
		StatusVersion:  rec.StatusVersion,
		Payload:        payload,
		Result:         result,
		ResultAssetIDs: resultAssetIDs,
		ErrorCode:      rec.ErrorCode,
		ErrorMessage:   rec.ErrorMessage,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}, nil
}
