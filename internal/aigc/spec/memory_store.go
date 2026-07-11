package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore mirrors the revision and approval fences of PostgresStore. It is
// primarily useful for deterministic domain and approval-runtime tests.
type MemoryStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	items map[string]FinalVideoSpec
	byKey map[string]string
}

type MemoryStoreOption func(*MemoryStore)

func WithMemoryClock(now func() time.Time) MemoryStoreOption {
	return func(store *MemoryStore) {
		if now != nil {
			store.now = now
		}
	}
}

func NewMemoryStore(options ...MemoryStoreOption) *MemoryStore {
	store := &MemoryStore{
		now:   time.Now,
		items: map[string]FinalVideoSpec{},
		byKey: map[string]string{},
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *MemoryStore) Save(_ context.Context, value FinalVideoSpec) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	value.ID = strings.TrimSpace(value.ID)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.IdempotencyKey = strings.TrimSpace(value.IdempotencyKey)
	if value.ID == "" {
		return FinalVideoSpec{}, fmt.Errorf("final video spec id is required")
	}
	if value.SessionID == "" {
		return FinalVideoSpec{}, fmt.Errorf("session id is required")
	}
	fields, err := cloneSpecFields(value.Fields)
	if err != nil {
		return FinalVideoSpec{}, fmt.Errorf("clone final video spec fields: %w", err)
	}
	value.Fields = fields

	s.mu.Lock()
	defer s.mu.Unlock()
	if recordKey, ok := s.byKey[value.IdempotencyKey]; value.IdempotencyKey != "" && ok {
		return cloneSpec(s.items[recordKey]), nil
	}
	if value.Version <= 0 {
		value.Version = s.nextVersionLocked(value.ID)
	}
	recordKey := specRevisionKey(value.ID, value.Version)
	if _, exists := s.items[recordKey]; exists {
		return FinalVideoSpec{}, fmt.Errorf("final video spec revision already exists: %s version=%d", value.ID, value.Version)
	}
	if value.Status == "" {
		value.Status = StatusDraft
	}
	now := s.now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	} else {
		value.CreatedAt = value.CreatedAt.UTC()
	}
	value.UpdatedAt = now
	if value.Status == StatusReviewing {
		for key, previous := range s.items {
			if previous.SessionID == value.SessionID && previous.Status == StatusReviewing {
				previous.Status = StatusSuperseded
				previous.UpdatedAt = now
				s.items[key] = previous
			}
		}
	}
	s.items[recordKey] = cloneSpec(value)
	if value.IdempotencyKey != "" {
		s.byKey[value.IdempotencyKey] = recordKey
	}
	return cloneSpec(value), nil
}

func (s *MemoryStore) Get(_ context.Context, specID string) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.filterLocked(func(value FinalVideoSpec) bool { return value.ID == strings.TrimSpace(specID) })
	return latestSpec(items, fmt.Sprintf("%s", specID))
}

func (s *MemoryStore) GetByIdempotencyKey(_ context.Context, key string) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	recordKey, ok := s.byKey[strings.TrimSpace(key)]
	if !ok {
		return FinalVideoSpec{}, fmt.Errorf("%w: idempotency_key=%s", ErrNotFound, key)
	}
	return cloneSpec(s.items[recordKey]), nil
}

func (s *MemoryStore) GetLatestBySession(_ context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID = strings.TrimSpace(sessionID)
	items := s.filterLocked(func(value FinalVideoSpec) bool { return value.SessionID == sessionID })
	return latestSpec(items, "session="+sessionID)
}

func (s *MemoryStore) GetLatestReviewingBySession(_ context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID = strings.TrimSpace(sessionID)
	items := s.filterLocked(func(value FinalVideoSpec) bool {
		return value.SessionID == sessionID && value.Status == StatusReviewing
	})
	return latestSpec(items, "reviewing session="+sessionID)
}

func (s *MemoryStore) GetConfirmedBySession(_ context.Context, sessionID string) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID = strings.TrimSpace(sessionID)
	items := s.filterLocked(func(value FinalVideoSpec) bool {
		return value.SessionID == sessionID && value.Status == StatusConfirmed
	})
	return latestSpec(items, "confirmed session="+sessionID)
}

func (s *MemoryStore) GetRevision(_ context.Context, specID string, version int) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.items[specRevisionKey(specID, version)]
	if !ok {
		return FinalVideoSpec{}, fmt.Errorf("%w: %s version=%d", ErrNotFound, specID, version)
	}
	return cloneSpec(value), nil
}

func (s *MemoryStore) DecideRevision(_ context.Context, specID string, version int, approved bool) (FinalVideoSpec, error) {
	if s == nil {
		return FinalVideoSpec{}, fmt.Errorf("memory final video spec store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordKey := specRevisionKey(specID, version)
	value, ok := s.items[recordKey]
	if !ok {
		return FinalVideoSpec{}, fmt.Errorf("%w: %s version=%d", ErrNotFound, specID, version)
	}
	targetStatus := StatusRejected
	if approved {
		targetStatus = StatusConfirmed
	}
	if value.Status == targetStatus {
		return cloneSpec(value), nil
	}
	if value.Status != StatusReviewing {
		return FinalVideoSpec{}, fmt.Errorf("final video spec revision cannot transition from %s", value.Status)
	}
	latest, ok := s.latestReviewingLocked(value.SessionID)
	if !ok || latest.ID != value.ID || latest.Version != value.Version {
		return FinalVideoSpec{}, fmt.Errorf("%w: current=%s version=%d", ErrNotLatestReviewing, value.ID, value.Version)
	}
	now := s.now().UTC()
	if approved {
		for key, previous := range s.items {
			if previous.SessionID == value.SessionID && previous.Status == StatusConfirmed && key != recordKey {
				previous.Status = StatusSuperseded
				previous.UpdatedAt = now
				s.items[key] = previous
			}
		}
	}
	value.Status = targetStatus
	value.UpdatedAt = now
	s.items[recordKey] = value
	return cloneSpec(value), nil
}

func (s *MemoryStore) nextVersionLocked(specID string) int {
	version := 0
	for _, value := range s.items {
		if value.ID == specID && value.Version > version {
			version = value.Version
		}
	}
	return version + 1
}

func (s *MemoryStore) latestReviewingLocked(sessionID string) (FinalVideoSpec, bool) {
	items := s.filterLocked(func(value FinalVideoSpec) bool {
		return value.SessionID == sessionID && value.Status == StatusReviewing
	})
	if len(items) == 0 {
		return FinalVideoSpec{}, false
	}
	sortSpecs(items)
	return items[0], true
}

func (s *MemoryStore) filterLocked(match func(FinalVideoSpec) bool) []FinalVideoSpec {
	items := make([]FinalVideoSpec, 0)
	for _, value := range s.items {
		if match(value) {
			items = append(items, cloneSpec(value))
		}
	}
	return items
}

func latestSpec(items []FinalVideoSpec, description string) (FinalVideoSpec, error) {
	if len(items) == 0 {
		return FinalVideoSpec{}, fmt.Errorf("%w: %s", ErrNotFound, description)
	}
	sortSpecs(items)
	return cloneSpec(items[0]), nil
}

func sortSpecs(items []FinalVideoSpec) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		if items[i].Version != items[j].Version {
			return items[i].Version > items[j].Version
		}
		return items[i].ID > items[j].ID
	})
}

func specRevisionKey(specID string, version int) string {
	return fmt.Sprintf("%s\x00%d", strings.TrimSpace(specID), version)
}

func cloneSpec(value FinalVideoSpec) FinalVideoSpec {
	fields, _ := cloneSpecFields(value.Fields)
	value.Fields = fields
	return value
}

func cloneSpecFields(fields map[string]any) (map[string]any, error) {
	if fields == nil {
		return nil, nil
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var cloned map[string]any
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}
