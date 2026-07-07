package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

// --- minimal fakes implementing the generation.Worker dependencies ---

type fakeJobStore struct {
	job generation.GenerationJob
}

func (s *fakeJobStore) Get(_ context.Context, _ string) (generation.GenerationJob, error) {
	return s.job, nil
}

func (s *fakeJobStore) UpdateStatus(_ context.Context, _ string, status string, update generation.StatusUpdate) (generation.GenerationJob, error) {
	s.job.Status = status
	if update.ResultAssetIDs != nil {
		s.job.ResultAssetIDs = update.ResultAssetIDs
	}
	if update.Result != nil {
		s.job.Result = update.Result
	}
	return s.job, nil
}

type onceQueue struct {
	payload generation.QueuePayload
	done    bool
}

func (q *onceQueue) Dequeue(_ context.Context, _ time.Duration) (generation.QueuePayload, bool, error) {
	if q.done {
		return generation.QueuePayload{}, false, nil
	}
	q.done = true
	return q.payload, true, nil
}

type recordingAssetStore struct {
	saved map[string]asset.Asset
}

func (s *recordingAssetStore) Save(_ context.Context, a asset.Asset) (asset.Asset, error) {
	if s.saved == nil {
		s.saved = map[string]asset.Asset{}
	}
	s.saved[a.ID] = a
	return a, nil
}

func (s *recordingAssetStore) Get(_ context.Context, id string) (asset.Asset, error) {
	return s.saved[id], nil
}

// TestWorkerDrivesDemoHandlerToSucceeded proves the ② async job state machine walks
// queued→running→succeeded with the demo media handler: the job is marked succeeded and
// a fixed asset is persisted with the job's id. (Storyboard binding is covered separately
// in the generation package's worker tests.)
func TestWorkerDrivesDemoHandlerToSucceeded(t *testing.T) {
	assets := &recordingAssetStore{}
	store := &fakeJobStore{job: generation.GenerationJob{
		ID:         "job-1",
		SessionID:  "s1",
		Provider:   generation.ProviderImage2,
		TargetType: "key_element",
		TargetID:   "ke-1",
		Status:     generation.StatusQueued,
	}}
	handler := NewDemoMediaJobHandler(DemoMediaJobHandlerConfig{
		Assets:   assets,
		Provider: generation.ProviderImage2,
		Kind:     asset.KindImage,
		MIMEType: "image/png",
		URLs:     []string{"/works/doraigc-aigc-cultural-tourism.png"},
	})
	worker := generation.NewWorker(generation.WorkerConfig{
		Store:    store,
		Queue:    &onceQueue{payload: generation.QueuePayload{JobID: "job-1"}},
		Assets:   assets,
		Handlers: map[string]generation.JobHandler{generation.ProviderImage2: handler},
		NewID:    func() string { return "evt" },
	})

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}
	if !processed {
		t.Fatal("RunOnce did not process a job")
	}
	if store.job.Status != generation.StatusSucceeded {
		t.Fatalf("job status = %q, want succeeded", store.job.Status)
	}
	if len(store.job.ResultAssetIDs) != 1 || store.job.ResultAssetIDs[0] != "demo-asset:job-1" {
		t.Fatalf("result asset ids = %v", store.job.ResultAssetIDs)
	}
	got, ok := assets.saved["demo-asset:job-1"]
	if !ok {
		t.Fatalf("demo asset was not persisted")
	}
	if got.Kind != asset.KindImage || got.URL != "/works/doraigc-aigc-cultural-tourism.png" {
		t.Fatalf("persisted asset mismatch: %+v", got)
	}
}
