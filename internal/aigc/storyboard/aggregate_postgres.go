package storyboard

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

type storyboardAggregateRecord struct {
	ID        string `gorm:"primaryKey;size:128"`
	SessionID string `gorm:"size:128;index"`
	Version   int    `gorm:"index"`
	Status    string `gorm:"size:64;index"`
	Snapshot  []byte `gorm:"type:jsonb;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time `gorm:"index"`
}

func (storyboardAggregateRecord) TableName() string {
	return "aigc_storyboard_aggregates"
}

type storyboardDomainEventRecord struct {
	ID               string `gorm:"primaryKey;size:160"`
	CommandID        string `gorm:"size:160;uniqueIndex:uidx_storyboard_command,priority:2"`
	IdempotencyKey   string `gorm:"size:160;index"`
	StoryboardID     string `gorm:"size:128;index:idx_storyboard_version,priority:1;uniqueIndex:uidx_storyboard_command,priority:1"`
	SessionID        string `gorm:"size:128;index"`
	AggregateVersion int    `gorm:"index:idx_storyboard_version,priority:2"`
	Type             string `gorm:"size:128;index"`
	Actor            string `gorm:"size:128"`
	Source           string `gorm:"size:64"`
	Payload          []byte `gorm:"type:jsonb"`
	CreatedAt        time.Time
}

func (storyboardDomainEventRecord) TableName() string {
	return "aigc_storyboard_domain_events"
}

func (s *PostgresStore) CreateAggregate(ctx context.Context, aggregate StoryboardAggregate) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres storyboard store db is required")
	}
	if err := validateAggregateIdentity(aggregate); err != nil {
		return err
	}
	record, err := aggregateToRecord(aggregate)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("create storyboard aggregate %s: %w", aggregate.ID, err)
	}
	return nil
}

func (s *PostgresStore) GetAggregate(ctx context.Context, storyboardID string) (StoryboardAggregate, error) {
	if s == nil || s.db == nil {
		return StoryboardAggregate{}, fmt.Errorf("postgres storyboard store db is required")
	}
	var record storyboardAggregateRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(storyboardID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return StoryboardAggregate{}, fmt.Errorf("%w: %s", ErrAggregateNotFound, storyboardID)
		}
		return StoryboardAggregate{}, fmt.Errorf("get storyboard aggregate %s: %w", storyboardID, err)
	}
	return recordToAggregate(record)
}

func (s *PostgresStore) GetAggregateBySession(ctx context.Context, sessionID string) (StoryboardAggregate, error) {
	if s == nil || s.db == nil {
		return StoryboardAggregate{}, fmt.Errorf("postgres storyboard store db is required")
	}
	var record storyboardAggregateRecord
	err := s.db.WithContext(ctx).
		Where("session_id = ?", strings.TrimSpace(sessionID)).
		Order("updated_at DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return StoryboardAggregate{}, fmt.Errorf("%w: session=%s", ErrAggregateNotFound, sessionID)
		}
		return StoryboardAggregate{}, fmt.Errorf("get storyboard aggregate by session %s: %w", sessionID, err)
	}
	return recordToAggregate(record)
}

func (s *PostgresStore) SaveAggregate(ctx context.Context, aggregate StoryboardAggregate, expectedVersion int, event DomainEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres storyboard store db is required")
	}
	if err := validateAggregateIdentity(aggregate); err != nil {
		return err
	}
	if aggregate.Version != expectedVersion+1 {
		return fmt.Errorf("%w: next=%d expected=%d", ErrVersionConflict, aggregate.Version, expectedVersion+1)
	}
	record, err := aggregateToRecord(aggregate)
	if err != nil {
		return err
	}
	event = normalizeDomainEvent(event, aggregate)
	eventRecord, err := domainEventToRecord(event)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current storyboardAggregateRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", aggregate.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrAggregateNotFound, aggregate.ID)
			}
			return fmt.Errorf("lock storyboard aggregate %s: %w", aggregate.ID, err)
		}
		if current.Version != expectedVersion {
			return fmt.Errorf("%w: current=%d expected=%d", ErrVersionConflict, current.Version, expectedVersion)
		}
		result := tx.Model(&storyboardAggregateRecord{}).
			Where("id = ? AND version = ?", aggregate.ID, expectedVersion).
			Updates(map[string]any{
				"session_id": aggregate.SessionID,
				"version":    aggregate.Version,
				"status":     aggregate.Status,
				"snapshot":   record.Snapshot,
				"updated_at": record.UpdatedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("save storyboard aggregate %s: %w", aggregate.ID, result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: storyboard aggregate %s", ErrVersionConflict, aggregate.ID)
		}
		if err := tx.Create(&eventRecord).Error; err != nil {
			return fmt.Errorf("create storyboard domain event %s: %w", event.ID, err)
		}
		return nil
	})
}

func (s *PostgresStore) ListDomainEvents(ctx context.Context, storyboardID string, afterVersion int) ([]DomainEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres storyboard store db is required")
	}
	var records []storyboardDomainEventRecord
	if err := s.db.WithContext(ctx).
		Where("storyboard_id = ? AND aggregate_version > ?", strings.TrimSpace(storyboardID), afterVersion).
		Order("aggregate_version ASC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list storyboard domain events: %w", err)
	}
	events := make([]DomainEvent, 0, len(records))
	for _, record := range records {
		event, err := recordToDomainEvent(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func aggregateToRecord(aggregate StoryboardAggregate) (storyboardAggregateRecord, error) {
	snapshot, err := json.Marshal(aggregate)
	if err != nil {
		return storyboardAggregateRecord{}, fmt.Errorf("marshal storyboard aggregate %s: %w", aggregate.ID, err)
	}
	return storyboardAggregateRecord{
		ID:        aggregate.ID,
		SessionID: aggregate.SessionID,
		Version:   aggregate.Version,
		Status:    aggregate.Status,
		Snapshot:  snapshot,
		CreatedAt: aggregate.CreatedAt,
		UpdatedAt: aggregate.UpdatedAt,
	}, nil
}

func recordToAggregate(record storyboardAggregateRecord) (StoryboardAggregate, error) {
	var aggregate StoryboardAggregate
	if err := json.Unmarshal(record.Snapshot, &aggregate); err != nil {
		return StoryboardAggregate{}, fmt.Errorf("unmarshal storyboard aggregate %s: %w", record.ID, err)
	}
	return aggregate, nil
}

func domainEventToRecord(event DomainEvent) (storyboardDomainEventRecord, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return storyboardDomainEventRecord{}, fmt.Errorf("marshal storyboard event %s payload: %w", event.ID, err)
	}
	return storyboardDomainEventRecord{
		ID:               event.ID,
		CommandID:        event.CommandID,
		IdempotencyKey:   event.IdempotencyKey,
		StoryboardID:     event.StoryboardID,
		SessionID:        event.SessionID,
		AggregateVersion: event.AggregateVersion,
		Type:             event.Type,
		Actor:            event.Actor,
		Source:           event.Source,
		Payload:          payload,
		CreatedAt:        event.CreatedAt,
	}, nil
}

func recordToDomainEvent(record storyboardDomainEventRecord) (DomainEvent, error) {
	event := DomainEvent{
		ID:               record.ID,
		CommandID:        record.CommandID,
		IdempotencyKey:   record.IdempotencyKey,
		StoryboardID:     record.StoryboardID,
		SessionID:        record.SessionID,
		AggregateVersion: record.AggregateVersion,
		Type:             record.Type,
		Actor:            record.Actor,
		Source:           record.Source,
		CreatedAt:        record.CreatedAt,
	}
	if len(record.Payload) > 0 {
		if err := json.Unmarshal(record.Payload, &event.Payload); err != nil {
			return DomainEvent{}, fmt.Errorf("unmarshal storyboard event %s payload: %w", record.ID, err)
		}
	}
	return event, nil
}

var _ AggregateRepository = (*PostgresStore)(nil)
