package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&SessionRecord{}, &MessageRecord{}, &CheckpointMapping{}); err != nil {
		return fmt.Errorf("migrate session tables: %w", err)
	}
	return nil
}

func (s *PostgresStore) SaveSession(ctx context.Context, record SessionRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres store db is required")
	}
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return fmt.Errorf("session id is required")
	}
	if record.Status == "" {
		record.Status = "active"
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&record).Error; err != nil {
		return fmt.Errorf("save session %s: %w", record.ID, err)
	}
	return nil
}

func (s *PostgresStore) GetSession(ctx context.Context, sessionID string) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("postgres store db is required")
	}
	var record SessionRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", sessionID).Error; err != nil {
		return SessionRecord{}, fmt.Errorf("get session %s: %w", sessionID, err)
	}
	return record, nil
}

func (s *PostgresStore) AppendMessage(ctx context.Context, record MessageRecord) (MessageRecord, error) {
	if s == nil || s.db == nil {
		return MessageRecord{}, fmt.Errorf("postgres store db is required")
	}
	record.ID = strings.TrimSpace(record.ID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Role = strings.TrimSpace(record.Role)
	if record.ID == "" {
		return MessageRecord{}, fmt.Errorf("message id is required")
	}
	if record.SessionID == "" {
		return MessageRecord{}, fmt.Errorf("session id is required")
	}
	if record.Role == "" {
		return MessageRecord{}, fmt.Errorf("message role is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if record.Seq <= 0 {
			var maxSeq int64
			if err := tx.Model(&MessageRecord{}).
				Where("session_id = ?", record.SessionID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&maxSeq).Error; err != nil {
				return err
			}
			record.Seq = maxSeq + 1
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return MessageRecord{}, fmt.Errorf("append message %s: %w", record.ID, err)
	}
	return record, nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, sessionID string, window MessageWindow) ([]MessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres store db is required")
	}
	query := s.db.WithContext(ctx).Where("session_id = ?", sessionID)
	order := "seq ASC"
	if window.Limit > 0 {
		query = query.Order("seq DESC").Limit(window.Limit)
		order = ""
	}
	if order != "" {
		query = query.Order(order)
	}

	var records []MessageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list messages for session %s: %w", sessionID, err)
	}
	if window.Limit > 0 {
		slices.Reverse(records)
	}
	return records, nil
}

func (s *PostgresStore) SaveCheckpointMapping(ctx context.Context, record CheckpointMapping) (CheckpointMapping, error) {
	if s == nil || s.db == nil {
		return CheckpointMapping{}, fmt.Errorf("postgres store db is required")
	}
	record.ID = strings.TrimSpace(record.ID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.InterruptID = strings.TrimSpace(record.InterruptID)
	record.Scope = strings.TrimSpace(record.Scope)
	if record.ID == "" {
		return CheckpointMapping{}, fmt.Errorf("checkpoint mapping id is required")
	}
	if record.SessionID == "" {
		return CheckpointMapping{}, fmt.Errorf("session id is required")
	}
	if record.InterruptID == "" {
		return CheckpointMapping{}, fmt.Errorf("interrupt id is required")
	}
	if record.Scope == "" {
		record.Scope = CheckpointScopeRunner
	}
	if record.Status == "" {
		record.Status = CheckpointStatusPending
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&record).Error; err != nil {
		return CheckpointMapping{}, fmt.Errorf("save checkpoint mapping %s: %w", record.ID, err)
	}
	return record, nil
}

func (s *PostgresStore) GetCheckpointMapping(ctx context.Context, sessionID string, interruptID string) (CheckpointMapping, error) {
	if s == nil || s.db == nil {
		return CheckpointMapping{}, fmt.Errorf("postgres store db is required")
	}
	var record CheckpointMapping
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND interrupt_id = ?", strings.TrimSpace(sessionID), strings.TrimSpace(interruptID)).
		Order("updated_at DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckpointMapping{}, ErrCheckpointNotFound
		}
		return CheckpointMapping{}, fmt.Errorf("get checkpoint mapping: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) MarkCheckpointResumed(ctx context.Context, id string) (CheckpointMapping, error) {
	if s == nil || s.db == nil {
		return CheckpointMapping{}, fmt.Errorf("postgres store db is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return CheckpointMapping{}, fmt.Errorf("checkpoint mapping id is required")
	}
	var record CheckpointMapping
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&record, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCheckpointNotFound
			}
			return err
		}
		now := time.Now()
		record.Status = CheckpointStatusResumed
		record.ResumedAt = now
		record.UpdatedAt = now
		return tx.Save(&record).Error
	})
	if err != nil {
		return CheckpointMapping{}, fmt.Errorf("mark checkpoint resumed %s: %w", id, err)
	}
	return record, nil
}
