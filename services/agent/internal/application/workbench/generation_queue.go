package workbench

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type GenerationJob struct {
	RunID          string         `json:"run_id"`
	InterruptID    string         `json:"interrupt_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	TraceID        string         `json:"trace_id"`
	Auth           AuthContextDTO `json:"auth"`
	EnqueuedAt     time.Time      `json:"enqueued_at"`
	QueueToken     string         `json:"-"`
}

type GenerationJobQueue interface {
	EnqueueGenerationJob(ctx context.Context, job GenerationJob) error
	DequeueGenerationJob(ctx context.Context) (GenerationJob, error)
	CompleteGenerationJob(ctx context.Context, job GenerationJob) error
	RequeueInflightGenerationJobs(ctx context.Context) (int, error)
}

type MemoryGenerationJobQueue struct {
	ch chan GenerationJob
}

func NewMemoryGenerationJobQueue(buffer int) *MemoryGenerationJobQueue {
	if buffer <= 0 {
		buffer = 1
	}
	return &MemoryGenerationJobQueue{ch: make(chan GenerationJob, buffer)}
}

func (q *MemoryGenerationJobQueue) EnqueueGenerationJob(ctx context.Context, job GenerationJob) error {
	select {
	case q.ch <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *MemoryGenerationJobQueue) DequeueGenerationJob(ctx context.Context) (GenerationJob, error) {
	select {
	case job := <-q.ch:
		return job, nil
	case <-ctx.Done():
		return GenerationJob{}, ctx.Err()
	}
}

func (q *MemoryGenerationJobQueue) CompleteGenerationJob(ctx context.Context, job GenerationJob) error {
	return nil
}

func (q *MemoryGenerationJobQueue) RequeueInflightGenerationJobs(ctx context.Context) (int, error) {
	return 0, nil
}

func (q *MemoryGenerationJobQueue) Len() int {
	return len(q.ch)
}

func validateGenerationJob(job GenerationJob) error {
	if job.RunID == "" || job.InterruptID == "" || job.IdempotencyKey == "" {
		return errors.New("run_id, interrupt_id and idempotency_key are required")
	}
	if job.TraceID == "" {
		return errors.New("trace_id is required")
	}
	if job.Auth.ActorUserID == "" || job.Auth.SpaceID == "" {
		return fmt.Errorf("generation job auth is incomplete")
	}
	return nil
}
