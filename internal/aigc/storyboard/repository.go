package storyboard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type DomainEvent struct {
	ID               string         `json:"id"`
	CommandID        string         `json:"command_id,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key,omitempty"`
	StoryboardID     string         `json:"storyboard_id"`
	SessionID        string         `json:"session_id"`
	AggregateVersion int            `json:"aggregate_version"`
	Type             string         `json:"type"`
	Actor            string         `json:"actor,omitempty"`
	Source           string         `json:"source,omitempty"`
	Payload          map[string]any `json:"payload,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

type AggregateRepository interface {
	CreateAggregate(ctx context.Context, aggregate StoryboardAggregate) error
	GetAggregate(ctx context.Context, storyboardID string) (StoryboardAggregate, error)
	GetAggregateBySession(ctx context.Context, sessionID string) (StoryboardAggregate, error)
	SaveAggregate(ctx context.Context, aggregate StoryboardAggregate, expectedVersion int, event DomainEvent) error
	ListDomainEvents(ctx context.Context, storyboardID string, afterVersion int) ([]DomainEvent, error)
}

// MemoryAggregateRepository is useful for graph unit tests and local demos. It
// applies the same aggregate-version CAS contract as the Postgres repository.
type MemoryAggregateRepository struct {
	mu         sync.RWMutex
	aggregates map[string]StoryboardAggregate
	events     map[string][]DomainEvent
}

func NewMemoryAggregateRepository() *MemoryAggregateRepository {
	return &MemoryAggregateRepository{
		aggregates: map[string]StoryboardAggregate{},
		events:     map[string][]DomainEvent{},
	}
}

func (r *MemoryAggregateRepository) CreateAggregate(_ context.Context, aggregate StoryboardAggregate) error {
	if err := validateAggregateIdentity(aggregate); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.aggregates[aggregate.ID]; exists {
		return fmt.Errorf("storyboard aggregate already exists: %s", aggregate.ID)
	}
	r.aggregates[aggregate.ID] = aggregate.Clone()
	return nil
}

func (r *MemoryAggregateRepository) GetAggregate(_ context.Context, storyboardID string) (StoryboardAggregate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	aggregate, ok := r.aggregates[strings.TrimSpace(storyboardID)]
	if !ok {
		return StoryboardAggregate{}, fmt.Errorf("%w: %s", ErrAggregateNotFound, storyboardID)
	}
	return aggregate.Clone(), nil
}

func (r *MemoryAggregateRepository) GetAggregateBySession(_ context.Context, sessionID string) (StoryboardAggregate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found *StoryboardAggregate
	for _, aggregate := range r.aggregates {
		if aggregate.SessionID != strings.TrimSpace(sessionID) {
			continue
		}
		if found == nil || aggregate.UpdatedAt.After(found.UpdatedAt) {
			clone := aggregate.Clone()
			found = &clone
		}
	}
	if found == nil {
		return StoryboardAggregate{}, fmt.Errorf("%w: session=%s", ErrAggregateNotFound, sessionID)
	}
	return *found, nil
}

func (r *MemoryAggregateRepository) SaveAggregate(_ context.Context, aggregate StoryboardAggregate, expectedVersion int, event DomainEvent) error {
	if err := validateAggregateIdentity(aggregate); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.aggregates[aggregate.ID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAggregateNotFound, aggregate.ID)
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: current=%d expected=%d", ErrVersionConflict, current.Version, expectedVersion)
	}
	if aggregate.Version != expectedVersion+1 {
		return fmt.Errorf("%w: next=%d expected=%d", ErrVersionConflict, aggregate.Version, expectedVersion+1)
	}
	if event.CommandID != "" {
		for _, existing := range r.events[aggregate.ID] {
			if existing.CommandID == event.CommandID {
				return nil
			}
		}
	}
	event = normalizeDomainEvent(event, aggregate)
	r.aggregates[aggregate.ID] = aggregate.Clone()
	r.events[aggregate.ID] = append(r.events[aggregate.ID], event)
	return nil
}

func (r *MemoryAggregateRepository) ListDomainEvents(_ context.Context, storyboardID string, afterVersion int) ([]DomainEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.aggregates[storyboardID]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrAggregateNotFound, storyboardID)
	}
	result := make([]DomainEvent, 0)
	for _, event := range r.events[storyboardID] {
		if event.AggregateVersion > afterVersion {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AggregateVersion < result[j].AggregateVersion })
	return result, nil
}

func validateAggregateIdentity(aggregate StoryboardAggregate) error {
	if strings.TrimSpace(aggregate.ID) == "" {
		return fmt.Errorf("storyboard aggregate id is required")
	}
	if strings.TrimSpace(aggregate.SessionID) == "" {
		return fmt.Errorf("storyboard aggregate session id is required")
	}
	return nil
}

func normalizeDomainEvent(event DomainEvent, aggregate StoryboardAggregate) DomainEvent {
	event.StoryboardID = aggregate.ID
	event.SessionID = aggregate.SessionID
	event.AggregateVersion = aggregate.Version
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s:event:%d", aggregate.ID, aggregate.Version)
	}
	if event.Type == "" {
		event.Type = "storyboard.changed"
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event
}

var _ AggregateRepository = (*MemoryAggregateRepository)(nil)

// IgnoreDuplicateCommand lets repository adapters signal that a previously
// committed idempotent command was replayed.
var ErrDuplicateCommand = errors.New("storyboard command already applied")
