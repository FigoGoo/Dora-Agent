package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type DispatchStore interface {
	Save(ctx context.Context, job GenerationJob) (GenerationJob, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (GenerationJob, error)
}

type DispatchQueue interface {
	Enqueue(ctx context.Context, payload QueuePayload) error
}

type DispatcherConfig struct {
	Store DispatchStore
	Queue DispatchQueue
}

type Dispatcher struct {
	cfg DispatcherConfig
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	return &Dispatcher{cfg: cfg}
}

func (d *Dispatcher) Dispatch(ctx context.Context, job GenerationJob) (GenerationJob, bool, error) {
	if d == nil || d.cfg.Store == nil {
		return GenerationJob{}, false, fmt.Errorf("generation dispatcher store is required")
	}
	if d.cfg.Queue == nil {
		return GenerationJob{}, false, fmt.Errorf("generation dispatcher queue is required")
	}
	job.ID = strings.TrimSpace(job.ID)
	job.SessionID = strings.TrimSpace(job.SessionID)
	job.IdempotencyKey = strings.TrimSpace(job.IdempotencyKey)
	if job.ID == "" {
		return GenerationJob{}, false, fmt.Errorf("generation job id is required")
	}
	if job.SessionID == "" {
		return GenerationJob{}, false, fmt.Errorf("session id is required")
	}
	if job.IdempotencyKey == "" {
		return GenerationJob{}, false, fmt.Errorf("idempotency key is required")
	}
	existing, err := d.cfg.Store.GetByIdempotencyKey(ctx, job.IdempotencyKey)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return GenerationJob{}, false, err
	}
	if job.Status == "" {
		job.Status = StatusQueued
	}
	if job.StatusVersion <= 0 {
		job.StatusVersion = 1
	}
	saved, err := d.cfg.Store.Save(ctx, job)
	if err != nil {
		return GenerationJob{}, false, err
	}
	if err := d.cfg.Queue.Enqueue(ctx, QueuePayload{
		JobID:          saved.ID,
		IdempotencyKey: saved.IdempotencyKey,
	}); err != nil {
		return GenerationJob{}, false, err
	}
	return saved, true, nil
}
