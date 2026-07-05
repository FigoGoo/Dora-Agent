package a2ui

import (
	"context"
	"testing"
	"time"
)

func TestMemoryBrokerPublishesToSessionSubscribers(t *testing.T) {
	broker := NewMemoryBroker(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	event := SSEEvent{
		ID:        "evt-1",
		SessionID: "s1",
		Event:     EventJobStatus,
		Payload:   map[string]any{"job_id": "job-1", "status": "succeeded"},
		CreatedAt: time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC),
	}
	if err := broker.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case got := <-ch:
		if got.ID != "evt-1" || got.Event != EventJobStatus {
			t.Fatalf("event = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMemoryBrokerDoesNotCrossSessions(t *testing.T) {
	broker := NewMemoryBroker(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	if err := broker.Publish(ctx, SSEEvent{ID: "evt-2", SessionID: "s2", Event: EventJobStatus}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case got := <-ch:
		t.Fatalf("unexpected event = %#v", got)
	case <-time.After(20 * time.Millisecond):
	}
}
