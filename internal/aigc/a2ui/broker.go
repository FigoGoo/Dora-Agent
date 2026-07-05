package a2ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type EventPublisher interface {
	Publish(ctx context.Context, event SSEEvent) error
}

type EventSubscriber interface {
	Subscribe(ctx context.Context, sessionID string) (<-chan SSEEvent, func())
}

type EventBroker interface {
	EventPublisher
	EventSubscriber
}

type MemoryBroker struct {
	mu          sync.RWMutex
	bufferSize  int
	subscribers map[string]map[chan SSEEvent]struct{}
}

func NewMemoryBroker(bufferSize int) *MemoryBroker {
	if bufferSize <= 0 {
		bufferSize = 16
	}
	return &MemoryBroker{
		bufferSize:  bufferSize,
		subscribers: map[string]map[chan SSEEvent]struct{}{},
	}
}

func (b *MemoryBroker) Publish(ctx context.Context, event SSEEvent) error {
	if b == nil {
		return fmt.Errorf("a2ui event broker is nil")
	}
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}

	b.mu.RLock()
	targets := make([]chan SSEEvent, 0, len(b.subscribers[sessionID]))
	for ch := range b.subscribers[sessionID] {
		targets = append(targets, ch)
	}
	b.mu.RUnlock()

	for _, ch := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- event:
		default:
		}
	}
	return nil
}

func (b *MemoryBroker) Subscribe(ctx context.Context, sessionID string) (<-chan SSEEvent, func()) {
	ch := make(chan SSEEvent, b.bufferSize)
	sessionID = strings.TrimSpace(sessionID)

	b.mu.Lock()
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[chan SSEEvent]struct{}{}
	}
	b.subscribers[sessionID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers[sessionID], ch)
			if len(b.subscribers[sessionID]) == 0 {
				delete(b.subscribers, sessionID)
			}
			b.mu.Unlock()
			close(ch)
		})
	}

	go func() {
		<-ctx.Done()
		unsubscribe()
	}()

	return ch, unsubscribe
}
