package events

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEventIDValidationMatchesPostgresColumnLimit(t *testing.T) {
	if MaxEventIDLength != 128 || !strings.Contains(strings.Join(MigrationStatements, "\n"), "event_id VARCHAR(128)") {
		t.Fatalf("event id validation/migration drift: max=%d", MaxEventIDLength)
	}
	store := NewMemoryStore()
	_, err := store.AppendSessionEventOnce(context.Background(), SessionEvent{
		SessionID: "session-1", EventID: strings.Repeat("e", MaxEventIDLength+1), EventType: "a2ui.action",
		ProducerKind: ProducerDomainProjector, SourceKey: "long-event-id", Payload: []byte(`{}`),
	})
	if err == nil || !strings.Contains(err.Error(), "cannot exceed 128") {
		t.Fatalf("overlong event id error = %v", err)
	}
}

func TestMemoryStoreAppendOnceAndTail(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)
	store := NewMemoryStoreWithClock(func() time.Time { return fixed })
	event := SessionEvent{
		SessionID: "session-1", EventID: "event-1", EventType: "a2ui.action",
		ProducerKind: ProducerDomainProjector, SourceKey: "domain:job-1:projection:1",
		Payload: []byte(`{"value":1}`),
	}
	first, err := store.AppendSessionEventOnce(ctx, event)
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if !first.Appended || first.Event.Seq != 1 || !first.Event.CreatedAt.Equal(fixed) {
		t.Fatalf("first append = %#v", first)
	}
	// JSON formatting is not part of the idempotency identity.
	event.Payload = []byte("{\n  \"value\": 1\n}")
	retry, err := store.AppendSessionEventOnce(ctx, event)
	if err != nil {
		t.Fatalf("retry append: %v", err)
	}
	if retry.Appended || retry.Event.Seq != 1 {
		t.Fatalf("retry append = %#v", retry)
	}
	event.EventID = "event-conflict"
	if _, err := store.AppendSessionEventOnce(ctx, event); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("source collision error = %v", err)
	}
	second := event
	second.EventID = "event-2"
	second.SourceKey = "domain:job-2:projection:1"
	if result, err := store.AppendSessionEventOnce(ctx, second); err != nil || result.Event.Seq != 2 {
		t.Fatalf("second append = %#v, %v", result, err)
	}
	rows, err := store.Tail(ctx, "session-1", TailOptions{AfterSeq: 1, Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].EventID != "event-2" {
		t.Fatalf("tail = %#v, %v", rows, err)
	}
	seq, err := store.CurrentSeq(ctx, "session-1")
	if err != nil || seq != 2 {
		t.Fatalf("current seq = %d, %v", seq, err)
	}
}

func TestTailRelayUsesDurableTailAfterMissedNotification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemoryStore()
	hub := NewMemoryNotificationHub()
	relay, err := NewTailRelay(TailRelayConfig{Store: store, Notifications: hub, PollInterval: 10 * time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	items := relay.Subscribe(ctx, "session-1", 0)
	// Append without notifying. Polling must still deliver it.
	_, err = store.AppendSessionEventOnce(ctx, SessionEvent{
		SessionID: "session-1", EventID: "event-1", EventType: "a2ui.action",
		ProducerKind: ProducerAgentAction, SourceKey: "turn:turn-1:event:1", Payload: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	select {
	case item := <-items:
		if item.Err != nil || item.Event.EventID != "event-1" {
			t.Fatalf("relay item = %#v", item)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay event")
	}
}

func TestTailRelayDoesNotAdvanceCursorWhenConsumerFails(t *testing.T) {
	store := NewMemoryStore()
	_, _ = store.AppendSessionEventOnce(context.Background(), SessionEvent{
		SessionID: "session-1", EventID: "event-1", EventType: "error",
		ProducerKind: ProducerSessionRuntime, SourceKey: "turn:turn-1:error", Payload: []byte(`{}`),
	})
	relay, _ := NewTailRelay(TailRelayConfig{Store: store, PollInterval: time.Hour})
	want := errors.New("flush failed")
	err := relay.Relay(context.Background(), "session-1", 0, func(SessionEvent) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("relay error = %v", err)
	}
	rows, _ := store.Tail(context.Background(), "session-1", TailOptions{AfterSeq: 0})
	if len(rows) != 1 {
		t.Fatalf("durable row disappeared: %#v", rows)
	}
}
