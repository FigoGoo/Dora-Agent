package events

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore { return &PostgresStore{db: db} }

// WithTx binds the store to a caller-owned transaction. Store methods may use
// nested savepoints, but their writes remain part of the outer transaction.
func (s *PostgresStore) WithTx(tx *gorm.DB) *PostgresStore { return &PostgresStore{db: tx} }

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres event store db is required")
	}
	return Migrate(ctx, s.db)
}

func (s *PostgresStore) AppendSessionEventOnce(ctx context.Context, event SessionEvent) (AppendResult, error) {
	if s == nil || s.db == nil {
		return AppendResult{}, fmt.Errorf("postgres event store db is required")
	}
	event, err := normalizeEvent(event)
	if err != nil {
		return AppendResult{}, err
	}
	var result AppendResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var innerErr error
		result, innerErr = appendSessionEventOnceTx(tx, event)
		return innerErr
	})
	if err != nil {
		return AppendResult{}, err
	}
	return result, nil
}

// AppendSessionEventOnceTx lets a domain projector commit its Inbox marker
// and all external projections in one caller-owned PostgreSQL transaction.
func (s *PostgresStore) AppendSessionEventOnceTx(ctx context.Context, tx *gorm.DB, event SessionEvent) (AppendResult, error) {
	if s == nil || s.db == nil {
		return AppendResult{}, fmt.Errorf("postgres event store db is required")
	}
	if tx == nil {
		return AppendResult{}, fmt.Errorf("postgres transaction is required")
	}
	event, err := normalizeEvent(event)
	if err != nil {
		return AppendResult{}, err
	}
	return appendSessionEventOnceTx(tx.WithContext(ctx), event)
}

func appendSessionEventOnceTx(tx *gorm.DB, event SessionEvent) (AppendResult, error) {
	var result AppendResult
	// The counter row is the per-session serialization point. Checking
	// idempotency after taking this lock prevents duplicate producers from
	// consuming a sequence number.
	if err := tx.Exec(`
			INSERT INTO aigc_session_event_counters (session_id, next_seq, updated_at)
			VALUES (?, 0, CURRENT_TIMESTAMP)
			ON CONFLICT (session_id) DO NOTHING`, event.SessionID).Error; err != nil {
		return AppendResult{}, fmt.Errorf("ensure session event counter: %w", err)
	}
	var nextSeq int64
	if err := tx.Raw(`
			SELECT next_seq FROM aigc_session_event_counters
			WHERE session_id = ? FOR UPDATE`, event.SessionID).Scan(&nextSeq).Error; err != nil {
		return AppendResult{}, fmt.Errorf("lock session event counter: %w", err)
	}

	var existing SessionEvent
	err := tx.Where("event_id = ?", event.EventID).First(&existing).Error
	if err == nil {
		if !sameIdentity(existing, event) {
			return AppendResult{}, fmt.Errorf("%w: event_id=%s", ErrIdempotencyConflict, event.EventID)
		}
		result.Event = existing
		return result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AppendResult{}, fmt.Errorf("find session event by event id: %w", err)
	}
	err = tx.Where(
		"session_id = ? AND producer_kind = ? AND source_key = ? AND projection_index = ?",
		event.SessionID, event.ProducerKind, event.SourceKey, event.ProjectionIndex,
	).First(&existing).Error
	if err == nil {
		if !sameIdentity(existing, event) {
			return AppendResult{}, fmt.Errorf("%w: source_key=%s", ErrIdempotencyConflict, event.SourceKey)
		}
		result.Event = existing
		return result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AppendResult{}, fmt.Errorf("find session event by source: %w", err)
	}

	event.Seq = nextSeq + 1
	if err := tx.Create(&event).Error; err != nil {
		return AppendResult{}, fmt.Errorf("%w: insert event %s: %v", ErrIdempotencyConflict, event.EventID, err)
	}
	if err := tx.Exec(`
			UPDATE aigc_session_event_counters
			SET next_seq = ?, updated_at = CURRENT_TIMESTAMP
			WHERE session_id = ?`, event.Seq, event.SessionID).Error; err != nil {
		return AppendResult{}, fmt.Errorf("advance session event counter: %w", err)
	}
	result = AppendResult{Event: event, Appended: true}
	return result, nil
}

func (s *PostgresStore) GetByEventID(ctx context.Context, eventID string) (SessionEvent, error) {
	if s == nil || s.db == nil {
		return SessionEvent{}, fmt.Errorf("postgres event store db is required")
	}
	var event SessionEvent
	err := s.db.WithContext(ctx).Where("event_id = ?", strings.TrimSpace(eventID)).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionEvent{}, fmt.Errorf("%w: event_id=%s", ErrEventNotFound, eventID)
	}
	if err != nil {
		return SessionEvent{}, fmt.Errorf("get event %s: %w", eventID, err)
	}
	return event, nil
}

func (s *PostgresStore) Tail(ctx context.Context, sessionID string, options TailOptions) ([]SessionEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres event store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	var rows []SessionEvent
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND seq > ?", sessionID, options.AfterSeq).
		Order("seq ASC").
		Limit(normalizeTailLimit(options.Limit)).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("tail session events for %s: %w", sessionID, err)
	}
	return rows, nil
}

func (s *PostgresStore) CurrentSeq(ctx context.Context, sessionID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("postgres event store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, fmt.Errorf("session id is required")
	}
	var counter sessionEventCounter
	err := s.db.WithContext(ctx).First(&counter, "session_id = ?", sessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get session event counter for %s: %w", sessionID, err)
	}
	return counter.NextSeq, nil
}
