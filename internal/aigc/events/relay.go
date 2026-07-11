package events

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// NotificationSource accelerates a TailRelay. Correctness never depends on a
// notification being delivered; the relay always polls the durable Store.
type NotificationSource interface {
	Subscribe(ctx context.Context, sessionID string) (<-chan struct{}, func(), error)
}

type TailRelayConfig struct {
	Store         Store
	Notifications NotificationSource
	PollInterval  time.Duration
	BatchSize     int
}

type TailRelay struct{ config TailRelayConfig }

type RelayItem struct {
	Event SessionEvent
	Err   error
}

func NewTailRelay(config TailRelayConfig) (*TailRelay, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("session event store is required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	config.BatchSize = normalizeTailLimit(config.BatchSize)
	return &TailRelay{config: config}, nil
}

// Relay calls consume in sequence order. The cursor advances only after
// consume returns nil, matching the SSE write+flush acknowledgement boundary.
func (r *TailRelay) Relay(ctx context.Context, sessionID string, afterSeq int64, consume func(SessionEvent) error) error {
	if r == nil || r.config.Store == nil {
		return fmt.Errorf("tail relay is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || consume == nil {
		return fmt.Errorf("session id and consumer are required")
	}
	var notifications <-chan struct{}
	cancelNotifications := func() {}
	if r.config.Notifications != nil {
		var err error
		notifications, cancelNotifications, err = r.config.Notifications.Subscribe(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("subscribe session event notifications: %w", err)
		}
	}
	defer cancelNotifications()
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	cursor := afterSeq
	for {
		rows, err := r.config.Store.Tail(ctx, sessionID, TailOptions{AfterSeq: cursor, Limit: r.config.BatchSize})
		if err != nil {
			return err
		}
		for _, event := range rows {
			if err := consume(event); err != nil {
				return err
			}
			cursor = event.Seq
		}
		if len(rows) == r.config.BatchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-notifications:
		}
	}
}

func (r *TailRelay) Subscribe(ctx context.Context, sessionID string, afterSeq int64) <-chan RelayItem {
	items := make(chan RelayItem)
	go func() {
		defer close(items)
		err := r.Relay(ctx, sessionID, afterSeq, func(event SessionEvent) error {
			select {
			case items <- RelayItem{Event: event}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && ctx.Err() == nil {
			select {
			case items <- RelayItem{Err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return items
}

// MemoryNotificationHub is useful for single-process deployments and tests.
// It is intentionally lossy and therefore safe to replace with PostgreSQL
// LISTEN/NOTIFY without changing TailRelay semantics.
type MemoryNotificationHub struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[string]map[uint64]chan struct{}
}

func NewMemoryNotificationHub() *MemoryNotificationHub {
	return &MemoryNotificationHub{subscribers: make(map[string]map[uint64]chan struct{})}
}

func (h *MemoryNotificationHub) Notify(sessionID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subscribers[strings.TrimSpace(sessionID)] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *MemoryNotificationHub) Subscribe(ctx context.Context, sessionID string) (<-chan struct{}, func(), error) {
	if h == nil {
		return nil, nil, fmt.Errorf("memory notification hub is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, fmt.Errorf("session id is required")
	}
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	if h.subscribers[sessionID] == nil {
		h.subscribers[sessionID] = make(map[uint64]chan struct{})
	}
	ch := make(chan struct{}, 1)
	h.subscribers[sessionID][id] = ch
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[sessionID], id)
			if len(h.subscribers[sessionID]) == 0 {
				delete(h.subscribers, sessionID)
			}
			h.mu.Unlock()
		})
	}
	context.AfterFunc(ctx, cancel)
	return ch, cancel, nil
}
