package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
)

// DurableEventBroker preserves the existing publish/subscribe integration but
// makes SessionEventLog the source of truth. The in-memory broker and
// notification hub are latency hints only.
type DurableEventBroker struct {
	Store         events.Store
	Wake          a2ui.EventBroker
	Notifications *events.MemoryNotificationHub
	NewID         func() string
	Now           func() time.Time
}

func NewDurableEventBroker(store events.Store, wake a2ui.EventBroker, notifications *events.MemoryNotificationHub) *DurableEventBroker {
	if wake == nil {
		wake = a2ui.NewMemoryBroker(64)
	}
	if notifications == nil {
		notifications = events.NewMemoryNotificationHub()
	}
	return &DurableEventBroker{Store: store, Wake: wake, Notifications: notifications, NewID: randomID, Now: time.Now}
}

func (b *DurableEventBroker) Publish(ctx context.Context, event a2ui.SSEEvent) error {
	if b == nil || b.Store == nil {
		return fmt.Errorf("durable event store is required")
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = b.NewID()
	}
	payload, err := events.MarshalPayload(event.Payload)
	if err != nil {
		return err
	}
	sourceKey := strings.TrimSpace(event.ID)
	if strings.TrimSpace(event.RunID) != "" && event.Seq > 0 {
		sourceKey = fmt.Sprintf("turn:%s:event:%d", event.RunID, event.Seq)
	}
	result, err := b.Store.AppendSessionEventOnce(ctx, events.SessionEvent{
		SessionID: event.SessionID, EventID: event.ID, EventType: event.Event,
		ProducerKind: events.ProducerAgentAction, SourceKey: sourceKey,
		SurfaceID: event.SurfaceID, DataModelKey: event.DataModelKey, Payload: payload,
	})
	if err != nil {
		return err
	}
	stored := sessionEventAsSSE(result.Event)
	stored.RunID = event.RunID
	if b.Wake != nil {
		_ = b.Wake.Publish(ctx, stored)
	}
	if b.Notifications != nil {
		b.Notifications.Notify(event.SessionID)
	}
	return nil
}

func (b *DurableEventBroker) Subscribe(ctx context.Context, sessionID string) (<-chan a2ui.SSEEvent, func()) {
	if b == nil || b.Wake == nil {
		ch := make(chan a2ui.SSEEvent)
		close(ch)
		return ch, func() {}
	}
	return b.Wake.Subscribe(ctx, sessionID)
}

func sessionEventAsSSE(event events.SessionEvent) a2ui.SSEEvent {
	var payload any
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	return a2ui.SSEEvent{ID: event.EventID, SessionID: event.SessionID, Seq: event.Seq, Event: event.EventType, SurfaceID: event.SurfaceID, DataModelKey: event.DataModelKey, Payload: payload, CreatedAt: event.CreatedAt}
}

var _ a2ui.EventBroker = (*DurableEventBroker)(nil)
