package events

import "context"

// Store is the durable source of truth consumed by SSE relays. Notifications
// are intentionally outside this interface: they are only best-effort wakeups.
type Store interface {
	AppendSessionEventOnce(ctx context.Context, event SessionEvent) (AppendResult, error)
	GetByEventID(ctx context.Context, eventID string) (SessionEvent, error)
	Tail(ctx context.Context, sessionID string, options TailOptions) ([]SessionEvent, error)
	CurrentSeq(ctx context.Context, sessionID string) (int64, error)
}

// AppendOnce is a short alias useful to producers.
func AppendOnce(ctx context.Context, store Store, event SessionEvent) (AppendResult, error) {
	return store.AppendSessionEventOnce(ctx, event)
}
