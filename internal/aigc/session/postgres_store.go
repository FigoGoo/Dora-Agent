package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return appendMessageTx(tx, &record) })
	if err != nil {
		return MessageRecord{}, fmt.Errorf("append message %s: %w", record.ID, err)
	}
	return record, nil
}

// AppendMessageAndEnqueue commits the user-visible Message and its durable
// TurnLoop input in one PostgreSQL transaction.
func (s *PostgresStore) AppendMessageAndEnqueue(ctx context.Context, runtimeStore *sessionruntime.PostgresStore, record MessageRecord, input sessionruntime.SessionInput) (MessageRecord, sessionruntime.EnqueueResult, error) {
	if s == nil || s.db == nil || runtimeStore == nil {
		return MessageRecord{}, sessionruntime.EnqueueResult{}, fmt.Errorf("session and runtime postgres stores are required")
	}
	record.ID = strings.TrimSpace(record.ID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Role = strings.TrimSpace(record.Role)
	if record.ID == "" || record.SessionID == "" || record.Role == "" {
		return MessageRecord{}, sessionruntime.EnqueueResult{}, fmt.Errorf("message id, session id and role are required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	var enqueued sessionruntime.EnqueueResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := appendMessageTx(tx, &record); err != nil {
			return err
		}
		boundedInput, boundErr := sessionruntime.WithContextMessageSeq(input, record.Seq)
		if boundErr != nil {
			return boundErr
		}
		var err error
		enqueued, err = runtimeStore.WithTx(tx).EnqueueInput(ctx, record.SessionID, boundedInput)
		return err
	})
	if err != nil {
		return MessageRecord{}, sessionruntime.EnqueueResult{}, fmt.Errorf("append message and enqueue input: %w", err)
	}
	return record, enqueued, nil
}

func appendMessageTx(tx *gorm.DB, record *MessageRecord) error {
	var existing MessageRecord
	err := tx.First(&existing, "id = ?", record.ID).Error
	if err == nil {
		if !sameMessageRecord(existing, *record) {
			return fmt.Errorf("%w: %s", ErrMessageIdempotencyConflict, record.ID)
		}
		*record = existing
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Locking the session row serializes user ingress and Agent output sequence
	// allocation without introducing a second counter table.
	var owner SessionRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&owner, "id = ?", record.SessionID).Error; err != nil {
		return err
	}
	if record.Seq <= 0 {
		var maxSeq int64
		if err := tx.Model(&MessageRecord{}).Where("session_id = ?", record.SessionID).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
			return err
		}
		record.Seq = maxSeq + 1
	}
	return tx.Create(record).Error
}

func sameMessageRecord(left, right MessageRecord) bool {
	return left.ID == right.ID && left.SessionID == right.SessionID && left.RunID == right.RunID && left.Role == right.Role &&
		left.Content == right.Content && left.ToolCallID == right.ToolCallID && left.ToolName == right.ToolName &&
		bytes.Equal(left.MessageJSON, right.MessageJSON) && bytes.Equal(left.ToolCalls, right.ToolCalls)
}

func (s *PostgresStore) ListMessages(ctx context.Context, sessionID string, window MessageWindow) ([]MessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres store db is required")
	}
	var records []MessageRecord
	if err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("seq ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list messages for session %s: %w", sessionID, err)
	}
	return ApplyMessageWindow(records, window), nil
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
	if record.MappingEpoch <= 0 {
		record.MappingEpoch = 1
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	var result CheckpointMapping
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing CheckpointMapping
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? OR (session_id = ? AND interrupt_id = ?)", record.ID, record.SessionID, record.InterruptID).
			First(&existing).Error
		if lookup == nil {
			if !sameCheckpointIdentity(existing, record) {
				return fmt.Errorf("checkpoint mapping idempotency conflict")
			}
			result = existing
			return nil
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = record
		return nil
	})
	if err != nil {
		// Resolve a concurrent insert without ever overwriting a terminal mapping.
		var existing CheckpointMapping
		if lookupErr := s.db.WithContext(ctx).Where("id = ? OR (session_id = ? AND interrupt_id = ?)", record.ID, record.SessionID, record.InterruptID).First(&existing).Error; lookupErr == nil && sameCheckpointIdentity(existing, record) {
			return existing, nil
		}
		return CheckpointMapping{}, fmt.Errorf("save checkpoint mapping %s: %w", record.ID, err)
	}
	return result, nil
}

func sameCheckpointIdentity(left, right CheckpointMapping) bool {
	return left.ID == right.ID && left.SessionID == right.SessionID && left.InterruptID == right.InterruptID &&
		left.Scope == right.Scope && left.RunnerCheckpointID == right.RunnerCheckpointID && left.GraphCheckpointID == right.GraphCheckpointID
}

func (s *PostgresStore) GetCheckpointMappingByApproval(ctx context.Context, approvalID string) (CheckpointMapping, error) {
	if s == nil || s.db == nil {
		return CheckpointMapping{}, fmt.Errorf("postgres store db is required")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return CheckpointMapping{}, fmt.Errorf("approval id is required")
	}
	var record CheckpointMapping
	err := s.db.WithContext(ctx).
		Where("approval_id = ?", approvalID).
		Order("mapping_epoch DESC, updated_at DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckpointMapping{}, ErrCheckpointNotFound
		}
		return CheckpointMapping{}, fmt.Errorf("get checkpoint mapping by approval: %w", err)
	}
	return record, nil
}

// TransitionCheckpointMapping applies a fenced state transition. A stale worker cannot
// advance a mapping after a fallback or a newer mapping epoch has taken ownership.
func (s *PostgresStore) TransitionCheckpointMapping(ctx context.Context, id string, expectedStatus string, expectedEpoch int64, nextStatus string, decisionVersion int) (CheckpointMapping, error) {
	if s == nil || s.db == nil {
		return CheckpointMapping{}, fmt.Errorf("postgres store db is required")
	}
	id = strings.TrimSpace(id)
	nextStatus = strings.TrimSpace(nextStatus)
	if id == "" || nextStatus == "" || expectedEpoch <= 0 {
		return CheckpointMapping{}, fmt.Errorf("mapping id, epoch and next status are required")
	}
	now := time.Now()
	updates := map[string]any{
		"status":           nextStatus,
		"decision_version": decisionVersion,
		"updated_at":       now,
	}
	if nextStatus == CheckpointStatusResumed {
		updates["resumed_at"] = now
	}
	result := s.db.WithContext(ctx).Model(&CheckpointMapping{}).
		Where("id = ? AND status = ? AND mapping_epoch = ?", id, strings.TrimSpace(expectedStatus), expectedEpoch).
		Updates(updates)
	if result.Error != nil {
		return CheckpointMapping{}, fmt.Errorf("transition checkpoint mapping %s: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return CheckpointMapping{}, fmt.Errorf("%w: stale checkpoint mapping transition", ErrCheckpointNotFound)
	}
	var record CheckpointMapping
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		return CheckpointMapping{}, fmt.Errorf("reload checkpoint mapping %s: %w", id, err)
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
