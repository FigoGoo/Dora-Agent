package events

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu        sync.RWMutex
	now       func() time.Time
	counters  map[string]int64
	byEvent   map[string]SessionEvent
	bySource  map[string]SessionEvent
	bySession map[string][]SessionEvent
}

func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithClock(time.Now)
}

func NewMemoryStoreWithClock(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{
		now:       now,
		counters:  make(map[string]int64),
		byEvent:   make(map[string]SessionEvent),
		bySource:  make(map[string]SessionEvent),
		bySession: make(map[string][]SessionEvent),
	}
}

func (s *MemoryStore) AppendSessionEventOnce(_ context.Context, event SessionEvent) (AppendResult, error) {
	if s == nil {
		return AppendResult{}, fmt.Errorf("memory event store is required")
	}
	event, err := normalizeEvent(event)
	if err != nil {
		return AppendResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byEvent[event.EventID]; ok {
		if !sameIdentity(existing, event) {
			return AppendResult{}, fmt.Errorf("%w: event_id=%s", ErrIdempotencyConflict, event.EventID)
		}
		return AppendResult{Event: cloneEvent(existing)}, nil
	}
	key := sourceIdentity(event)
	if existing, ok := s.bySource[key]; ok {
		if !sameIdentity(existing, event) {
			return AppendResult{}, fmt.Errorf("%w: source_key=%s", ErrIdempotencyConflict, event.SourceKey)
		}
		return AppendResult{Event: cloneEvent(existing)}, nil
	}
	s.counters[event.SessionID]++
	event.Seq = s.counters[event.SessionID]
	event.CreatedAt = s.now().UTC()
	s.byEvent[event.EventID] = cloneEvent(event)
	s.bySource[key] = cloneEvent(event)
	s.bySession[event.SessionID] = append(s.bySession[event.SessionID], cloneEvent(event))
	return AppendResult{Event: cloneEvent(event), Appended: true}, nil
}

func (s *MemoryStore) GetByEventID(_ context.Context, eventID string) (SessionEvent, error) {
	if s == nil {
		return SessionEvent{}, fmt.Errorf("memory event store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, ok := s.byEvent[strings.TrimSpace(eventID)]
	if !ok {
		return SessionEvent{}, fmt.Errorf("%w: event_id=%s", ErrEventNotFound, eventID)
	}
	return cloneEvent(event), nil
}

func (s *MemoryStore) Tail(_ context.Context, sessionID string, options TailOptions) ([]SessionEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("memory event store is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	limit := normalizeTailLimit(options.Limit)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.bySession[sessionID]
	start := sort.Search(len(rows), func(i int) bool { return rows[i].Seq > options.AfterSeq })
	end := min(len(rows), start+limit)
	out := make([]SessionEvent, 0, end-start)
	for _, row := range rows[start:end] {
		out = append(out, cloneEvent(row))
	}
	return out, nil
}

func (s *MemoryStore) CurrentSeq(_ context.Context, sessionID string) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("memory event store is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, fmt.Errorf("session id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.counters[sessionID], nil
}

func cloneEvent(event SessionEvent) SessionEvent {
	event.Payload = append(event.Payload[:0:0], event.Payload...)
	return event
}

func normalizeTailLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
