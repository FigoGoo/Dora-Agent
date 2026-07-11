package generation

import (
	"context"
	"fmt"
	"time"
)

// RecoveryScheduler makes queue delivery a wake-up optimization rather than a
// source of truth. It re-enqueues due jobs and expired leases; duplicate queue
// messages are safe because LifecycleWorker claims by StatusVersion and lease.
type RecoveryScheduler struct {
	store WorkflowStore
	queue DispatchQueue
	clock func() time.Time
}

func NewRecoveryScheduler(store WorkflowStore, queue DispatchQueue, clock ...func() time.Time) *RecoveryScheduler {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &RecoveryScheduler{store: store, queue: queue, clock: now}
}

func (s *RecoveryScheduler) EnqueueDue(ctx context.Context, limit int) (int, error) {
	if s == nil || s.store == nil || s.queue == nil {
		return 0, fmt.Errorf("recovery scheduler store and queue are required")
	}
	jobs, err := s.store.ListRunnableJobs(ctx, s.clock(), limit)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, job := range jobs {
		wakeKey := fmt.Sprintf("job:%s:wake:%d", job.ID, job.StatusVersion)
		if err := s.queue.Enqueue(ctx, QueuePayload{JobID: job.ID, IdempotencyKey: wakeKey, EnqueuedAt: s.clock()}); err != nil {
			return enqueued, err
		}
		enqueued++
	}
	return enqueued, nil
}
