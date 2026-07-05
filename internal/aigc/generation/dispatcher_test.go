package generation

import (
	"context"
	"testing"
)

func TestDispatcherCreatesQueuedJobAndEnqueues(t *testing.T) {
	store := newFakeDispatchStore()
	queue := &fakeDispatchQueue{}
	dispatcher := NewDispatcher(DispatcherConfig{Store: store, Queue: queue})

	job, queued, err := dispatcher.Dispatch(context.Background(), GenerationJob{
		ID:             "job-1",
		SessionID:      "s1",
		IdempotencyKey: "idem-1",
		Provider:       ProviderImage2,
		TargetType:     TargetKeyElement,
		TargetID:       "suji",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !queued {
		t.Fatal("Dispatch() did not enqueue a new job")
	}
	if job.Status != StatusQueued || job.StatusVersion != 1 {
		t.Fatalf("job = %#v", job)
	}
	if queue.payload.JobID != "job-1" || queue.payload.IdempotencyKey != "idem-1" {
		t.Fatalf("queue payload = %#v", queue.payload)
	}
}

func TestDispatcherReturnsExistingJobByIdempotencyKey(t *testing.T) {
	store := newFakeDispatchStore()
	store.jobsByKey["idem-1"] = GenerationJob{
		ID:             "job-existing",
		SessionID:      "s1",
		IdempotencyKey: "idem-1",
		Status:         StatusQueued,
	}
	queue := &fakeDispatchQueue{}
	dispatcher := NewDispatcher(DispatcherConfig{Store: store, Queue: queue})

	job, queued, err := dispatcher.Dispatch(context.Background(), GenerationJob{
		ID:             "job-1",
		SessionID:      "s1",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if queued {
		t.Fatal("Dispatch() enqueued an existing job")
	}
	if job.ID != "job-existing" || queue.called {
		t.Fatalf("job = %#v queue.called=%v", job, queue.called)
	}
}

type fakeDispatchStore struct {
	jobs      map[string]GenerationJob
	jobsByKey map[string]GenerationJob
}

func newFakeDispatchStore() *fakeDispatchStore {
	return &fakeDispatchStore{
		jobs:      map[string]GenerationJob{},
		jobsByKey: map[string]GenerationJob{},
	}
}

func (s *fakeDispatchStore) Save(_ context.Context, job GenerationJob) (GenerationJob, error) {
	if job.StatusVersion <= 0 {
		job.StatusVersion = 1
	}
	s.jobs[job.ID] = job
	s.jobsByKey[job.IdempotencyKey] = job
	return job, nil
}

func (s *fakeDispatchStore) GetByIdempotencyKey(_ context.Context, idempotencyKey string) (GenerationJob, error) {
	job, ok := s.jobsByKey[idempotencyKey]
	if !ok {
		return GenerationJob{}, ErrNotFound
	}
	return job, nil
}

type fakeDispatchQueue struct {
	payload QueuePayload
	called  bool
}

func (q *fakeDispatchQueue) Enqueue(_ context.Context, payload QueuePayload) error {
	q.payload = payload
	q.called = true
	return nil
}
