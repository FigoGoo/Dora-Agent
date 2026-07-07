package generation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestWorkerRunOnceMarksSucceeded(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	queue := &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: queue,
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(_ context.Context, job GenerationJob) (HandlerResult, error) {
				if job.ID != "job-1" || job.Status != StatusRunning {
					t.Fatalf("handler job = %#v", job)
				}
				return HandlerResult{AssetIDs: []string{"asset-1"}, Result: map[string]any{"asset_id": "asset-1"}}, nil
			}),
		},
	})

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("RunOnce() did not process a job")
	}
	got := store.jobs["job-1"]
	if got.Status != StatusSucceeded || got.ResultAssetIDs[0] != "asset-1" {
		t.Fatalf("job = %#v", got)
	}
	if len(store.statuses) != 2 || store.statuses[0] != StatusRunning || store.statuses[1] != StatusSucceeded {
		t.Fatalf("statuses = %#v", store.statuses)
	}
}

func TestWorkerRunOncePublishesJobStatusEvents(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	events := &fakeEventPublisher{}
	worker := NewWorker(WorkerConfig{
		Store:  store,
		Queue:  &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}},
		Events: events,
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{AssetIDs: []string{"asset-1"}}, nil
			}),
		},
		NewID: sequentialWorkerIDs("evt-running", "evt-succeeded"),
	})

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("RunOnce() did not process a job")
	}
	if len(events.events) != 2 {
		t.Fatalf("event count = %d, events = %#v", len(events.events), events.events)
	}
	if events.events[0].Event != EventJobStatus || events.events[0].ID != "evt-running" {
		t.Fatalf("running event = %#v", events.events[0])
	}
	if payload, ok := events.events[1].Payload.(JobStatusPayload); !ok || payload.Status != StatusSucceeded || payload.ResultAssetIDs[0] != "asset-1" {
		t.Fatalf("succeeded event payload = %#v", events.events[1].Payload)
	}
}

func TestWorkerRunOncePatchesStoryboardForResultAssets(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				StoryboardID:   "storyboard-1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				TargetType:     TargetKeyElement,
				TargetID:       "suji",
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	storyboards := &fakeStoryboardSyncStore{
		board: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   7,
			KeyElements: []storyboard.KeyElement{
				{Key: "suji", Name: "苏寂"},
			},
		},
	}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}},
		Assets: &fakeAssetLookup{
			records: map[string]asset.Asset{"asset-1": {ID: "asset-1", SessionID: "s1", Kind: asset.KindImage}},
		},
		Storyboards: storyboards,
		NewID:       sequentialWorkerIDs("patch-event-1"),
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{AssetIDs: []string{"asset-1"}}, nil
			}),
		},
	})

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("RunOnce() did not process a job")
	}
	if storyboards.seenPatch.EventID != "patch-event-1" || storyboards.seenPatch.Source != "worker" {
		t.Fatalf("patch request = %#v", storyboards.seenPatch)
	}
	if storyboards.seenPatch.BaseVersion != 7 || len(storyboards.seenPatch.Ops) != 2 {
		t.Fatalf("patch request = %#v", storyboards.seenPatch)
	}
	if storyboards.seenPatch.Ops[0].Path != "/key_elements/0/asset_ids" ||
		storyboards.seenPatch.Ops[1].Path != "/key_elements/0/status" ||
		storyboards.seenPatch.Ops[1].Value != storyboard.StatusReady {
		t.Fatalf("patch ops = %#v", storyboards.seenPatch.Ops)
	}
}

func TestWorkerRunOnceRetriesStoryboardBindOnVersionConflict(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				StoryboardID:   "storyboard-1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				TargetType:     TargetKeyElement,
				TargetID:       "suji",
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	storyboards := &fakeStoryboardSyncStore{
		board: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   7,
			KeyElements: []storyboard.KeyElement{
				{Key: "suji", Name: "苏寂"},
			},
		},
		conflictsBeforeSuccess: 3,
	}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}},
		Assets: &fakeAssetLookup{
			records: map[string]asset.Asset{"asset-1": {ID: "asset-1", SessionID: "s1", Kind: asset.KindImage}},
		},
		Storyboards: storyboards,
		NewID:       sequentialWorkerIDs("patch-event-1"),
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{AssetIDs: []string{"asset-1"}}, nil
			}),
		},
	})

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("RunOnce() did not process a job")
	}
	// 3 conflicts then success = 4 ApplyPatch attempts.
	if storyboards.applyCalls != 4 {
		t.Fatalf("ApplyPatch calls = %d, want 4 (3 conflicts + 1 success)", storyboards.applyCalls)
	}
	// The winning patch must have been recomputed against the freshly-read version.
	if storyboards.seenPatch.BaseVersion != 7 || len(storyboards.seenPatch.Ops) != 2 {
		t.Fatalf("final patch request = %#v", storyboards.seenPatch)
	}
}

func TestWorkerRunOncePublishesStoryboardPatchEvent(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				StoryboardID:   "storyboard-1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				TargetType:     TargetKeyElement,
				TargetID:       "suji",
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	events := &fakeEventPublisher{}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}},
		Assets: &fakeAssetLookup{
			records: map[string]asset.Asset{"asset-1": {ID: "asset-1", SessionID: "s1", Kind: asset.KindImage}},
		},
		Storyboards: &fakeStoryboardSyncStore{
			board: storyboard.Storyboard{
				ID:        "storyboard-1",
				SessionID: "s1",
				Version:   7,
				KeyElements: []storyboard.KeyElement{
					{Key: "suji", Name: "苏寂"},
				},
			},
		},
		Events: events,
		NewID:  sequentialWorkerIDs("evt-running", "evt-succeeded", "patch-event-1", "evt-patch"),
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{AssetIDs: []string{"asset-1"}}, nil
			}),
		},
	})

	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	var patchEvent *WorkerEvent
	for i := range events.events {
		if events.events[i].Event == EventStoryboardPatch {
			patchEvent = &events.events[i]
		}
	}
	if patchEvent == nil {
		t.Fatalf("missing storyboard patch event: %#v", events.events)
	}
	payload, ok := patchEvent.Payload.(StoryboardPatchPayload)
	if !ok {
		t.Fatalf("patch payload = %#v", patchEvent.Payload)
	}
	if payload.StoryboardID != "storyboard-1" || payload.NextVersion != 8 || len(payload.Ops) != 2 {
		t.Fatalf("patch payload = %#v", payload)
	}
	if payload.Ops[1].Path != "/key_elements/0/status" || payload.Ops[1].Value != storyboard.StatusReady {
		t.Fatalf("patch payload = %#v", payload)
	}
}

func TestWorkerRunOnceMarksFailed(t *testing.T) {
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	queue := &fakeJobQueue{payload: QueuePayload{JobID: "job-1"}}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: queue,
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{}, errors.New("provider failed")
			}),
		},
	})

	processed, err := worker.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error is nil")
	}
	if !processed {
		t.Fatal("RunOnce() did not process a job")
	}
	got := store.jobs["job-1"]
	if got.Status != StatusFailed || got.ErrorMessage != "provider failed" {
		t.Fatalf("job = %#v", got)
	}
}

func TestWorkerRunContinuesAfterJobFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-fail": {
				ID:             "job-fail",
				SessionID:      "s1",
				IdempotencyKey: "idem-fail",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
			"job-ok": {
				ID:             "job-ok",
				SessionID:      "s1",
				IdempotencyKey: "idem-ok",
				Provider:       ProviderAudio,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	queue := &scriptedJobQueue{
		payloads: []QueuePayload{{JobID: "job-fail"}, {JobID: "job-ok"}},
		onEmpty:  cancel,
	}
	worker := NewWorker(WorkerConfig{
		Store: store,
		Queue: queue,
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{}, errors.New("provider failed")
			}),
			ProviderAudio: JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
				return HandlerResult{AssetIDs: []string{"asset-ok"}}, nil
			}),
		},
	})

	err := worker.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if store.jobs["job-fail"].Status != StatusFailed {
		t.Fatalf("failed job = %#v", store.jobs["job-fail"])
	}
	if store.jobs["job-ok"].Status != StatusSucceeded {
		t.Fatalf("succeeded job = %#v", store.jobs["job-ok"])
	}
}

func TestWorkerRunProcessesJobsConcurrently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeJobStore{
		jobs: map[string]GenerationJob{
			"job-1": {
				ID:             "job-1",
				SessionID:      "s1",
				IdempotencyKey: "idem-1",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
			"job-2": {
				ID:             "job-2",
				SessionID:      "s1",
				IdempotencyKey: "idem-2",
				Provider:       ProviderImage2,
				Status:         StatusQueued,
				StatusVersion:  1,
			},
		},
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	worker := NewWorker(WorkerConfig{
		Store:       store,
		Queue:       &scriptedJobQueue{payloads: []QueuePayload{{JobID: "job-1"}, {JobID: "job-2"}}},
		Concurrency: 2,
		Handlers: map[string]JobHandler{
			ProviderImage2: JobHandlerFunc(func(_ context.Context, job GenerationJob) (HandlerResult, error) {
				started <- job.ID
				<-release
				return HandlerResult{AssetIDs: []string{"asset-" + job.ID}}, nil
			}),
		},
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()

	first := waitForStartedJob(t, started)
	second := waitForStartedJob(t, started)
	if first == second {
		t.Fatalf("same job started twice: %q", first)
	}

	close(release)
	waitForJobStatus(t, store, "job-1", StatusSucceeded)
	waitForJobStatus(t, store, "job-2", StatusSucceeded)
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

type fakeJobStore struct {
	mu       sync.Mutex
	jobs     map[string]GenerationJob
	statuses []string
}

func (s *fakeJobStore) Get(_ context.Context, jobID string) (GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return GenerationJob{}, ErrNotFound
	}
	return job, nil
}

func (s *fakeJobStore) UpdateStatus(_ context.Context, jobID string, status string, update StatusUpdate) (GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return GenerationJob{}, ErrNotFound
	}
	job.Status = status
	job.StatusVersion++
	job.ResultAssetIDs = update.ResultAssetIDs
	job.Result = update.Result
	job.ErrorCode = update.ErrorCode
	job.ErrorMessage = update.ErrorMessage
	s.jobs[jobID] = job
	s.statuses = append(s.statuses, status)
	return job, nil
}

type fakeJobQueue struct {
	payload QueuePayload
	empty   bool
}

func (q *fakeJobQueue) Dequeue(context.Context, time.Duration) (QueuePayload, bool, error) {
	if q.empty {
		return QueuePayload{}, false, nil
	}
	q.empty = true
	return q.payload, true, nil
}

type scriptedJobQueue struct {
	mu       sync.Mutex
	payloads []QueuePayload
	onEmpty  func()
}

func (q *scriptedJobQueue) Dequeue(context.Context, time.Duration) (QueuePayload, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.payloads) == 0 {
		if q.onEmpty != nil {
			q.onEmpty()
		}
		return QueuePayload{}, false, nil
	}
	payload := q.payloads[0]
	q.payloads = q.payloads[1:]
	return payload, true, nil
}

func waitForStartedJob(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case jobID := <-started:
		return jobID
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for concurrent job start")
		return ""
	}
}

func waitForJobStatus(t *testing.T, store *fakeJobStore, jobID string, status string) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for job %s status %s; job = %#v", jobID, status, store.jobs[jobID])
		case <-tick.C:
			store.mu.Lock()
			got := store.jobs[jobID]
			store.mu.Unlock()
			if got.Status == status {
				return
			}
		}
	}
}

type fakeAssetLookup struct {
	records map[string]asset.Asset
}

func (s *fakeAssetLookup) Get(_ context.Context, assetID string) (asset.Asset, error) {
	record, ok := s.records[assetID]
	if !ok {
		return asset.Asset{}, asset.ErrNotFound
	}
	return record, nil
}

type fakeStoryboardSyncStore struct {
	board     storyboard.Storyboard
	seenPatch storyboard.PatchRequest
	// conflictsBeforeSuccess makes the first N ApplyPatch calls fail with a version
	// conflict, simulating concurrent workers racing on the same storyboard's
	// optimistic lock. applyCalls records how many ApplyPatch calls were made.
	conflictsBeforeSuccess int
	applyCalls             int
}

func (s *fakeStoryboardSyncStore) Get(_ context.Context, storyboardID string) (storyboard.Storyboard, error) {
	if s.board.ID != storyboardID {
		return storyboard.Storyboard{}, storyboard.ErrNotFound
	}
	return s.board, nil
}

func (s *fakeStoryboardSyncStore) ApplyPatch(_ context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error) {
	s.applyCalls++
	if s.applyCalls <= s.conflictsBeforeSuccess {
		return storyboard.Storyboard{}, storyboard.EventRecord{}, storyboard.ErrVersionConflict
	}
	s.seenPatch = req
	s.board.Version = req.BaseVersion + 1
	return s.board, storyboard.EventRecord{NextVersion: s.board.Version}, nil
}

func sequentialWorkerIDs(ids ...string) func() string {
	i := 0
	return func() string {
		if i >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[i]
		i++
		return id
	}
}

type fakeEventPublisher struct {
	events []WorkerEvent
}

func (p *fakeEventPublisher) Publish(_ context.Context, event WorkerEvent) error {
	p.events = append(p.events, event)
	return nil
}
