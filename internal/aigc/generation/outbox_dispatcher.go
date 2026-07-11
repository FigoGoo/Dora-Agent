package generation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type OutboxPublisher interface {
	PublishOutbox(ctx context.Context, event OutboxEvent) error
}

type OutboxPublisherFunc func(ctx context.Context, event OutboxEvent) error

func (f OutboxPublisherFunc) PublishOutbox(ctx context.Context, event OutboxEvent) error {
	return f(ctx, event)
}

type OutboxDispatcher struct {
	store     WorkflowStore
	publisher OutboxPublisher
	clock     func() time.Time
}

func NewOutboxDispatcher(store WorkflowStore, publisher OutboxPublisher, clock ...func() time.Time) *OutboxDispatcher {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &OutboxDispatcher{store: store, publisher: publisher, clock: now}
}

// DispatchPending is an at-least-once relay. Stable OutboxEvent.IdempotencyKey
// is the consumer-side dedupe key; an ACK failure may redeliver the same event.
func (d *OutboxDispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	if d == nil || d.store == nil || d.publisher == nil {
		return 0, fmt.Errorf("outbox store and publisher are required")
	}
	events, err := d.store.ListOutbox(ctx, OutboxPending, 0)
	if err != nil {
		return 0, err
	}
	published := 0
	now := d.clock()
	var dispatchErr error
	due := 0
	for _, event := range events {
		if event.AvailableAt.After(now) {
			continue
		}
		if limit > 0 && due >= limit {
			break
		}
		due++
		if err := d.publisher.PublishOutbox(ctx, event); err != nil {
			dispatchErr = errors.Join(dispatchErr, fmt.Errorf("publish outbox %s: %w", event.ID, err))
			maxAttempts := 10
			if event.EventType == EventBatchFinalizeRequested {
				// A terminal Job has no future scheduler wake. Dead-lettering its
				// Barrier trigger can strand the Batch forever, so this internal,
				// idempotent event remains retryable until it is acknowledged.
				maxAttempts = 0
			}
			if markErr := d.store.MarkOutboxFailed(ctx, event.ID, d.clock(), maxAttempts); markErr != nil {
				dispatchErr = errors.Join(dispatchErr, fmt.Errorf("mark outbox %s failed: %w", event.ID, markErr))
			}
			continue
		}
		if err := d.store.MarkOutboxPublished(ctx, event.ID, d.clock()); err != nil {
			dispatchErr = errors.Join(dispatchErr, fmt.Errorf("mark outbox %s published: %w", event.ID, err))
			continue
		}
		published++
	}
	return published, dispatchErr
}

// QueueOutboxPublisher bridges media job dispatch events to the legacy queue;
// other destinations are delegated to Next.
type QueueOutboxPublisher struct {
	Queue DispatchQueue
	Next  OutboxPublisher
}

func (p QueueOutboxPublisher) PublishOutbox(ctx context.Context, event OutboxEvent) error {
	if event.Destination != DestinationMediaJobs {
		if p.Next == nil {
			return nil
		}
		return p.Next.PublishOutbox(ctx, event)
	}
	if p.Queue == nil {
		return fmt.Errorf("generation dispatch queue is required")
	}
	jobID, _ := event.Payload["job_id"].(string)
	idempotencyKey, _ := event.Payload["idempotency_key"].(string)
	idempotencyKey = valueOrDefault(idempotencyKey, event.IdempotencyKey)
	return p.Queue.Enqueue(ctx, QueuePayload{JobID: jobID, IdempotencyKey: idempotencyKey, EnqueuedAt: event.AvailableAt})
}
