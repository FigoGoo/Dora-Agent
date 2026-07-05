package generation

import (
	"context"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStorePersistsJobTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}

	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	suffix := time.Now().Format("20060102150405.000000000")
	job := GenerationJob{
		ID:             "job-store-test-" + suffix,
		SessionID:      "session-job-store-test",
		StoryboardID:   "storyboard-1",
		ToolCallID:     "tool-call-1",
		IdempotencyKey: "idempotency-" + suffix,
		Provider:       ProviderImage2,
		TargetType:     TargetKeyElement,
		TargetID:       "suji",
		Status:         StatusQueued,
		MaxRetries:     2,
		Payload:        map[string]any{"prompt": "苏寂角色参考图"},
	}
	saved, err := store.Save(ctx, job)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.Status != StatusQueued || saved.StatusVersion != 1 {
		t.Fatalf("saved job = %#v", saved)
	}

	running, err := store.UpdateStatus(ctx, saved.ID, StatusRunning, StatusUpdate{})
	if err != nil {
		t.Fatalf("UpdateStatus(running) error = %v", err)
	}
	if running.Status != StatusRunning || running.StatusVersion != 2 {
		t.Fatalf("running job = %#v", running)
	}

	succeeded, err := store.UpdateStatus(ctx, saved.ID, StatusSucceeded, StatusUpdate{
		ResultAssetIDs: []string{"asset-1"},
		Result:         map[string]any{"asset_id": "asset-1"},
	})
	if err != nil {
		t.Fatalf("UpdateStatus(succeeded) error = %v", err)
	}
	if succeeded.Status != StatusSucceeded || succeeded.StatusVersion != 3 || succeeded.ResultAssetIDs[0] != "asset-1" {
		t.Fatalf("succeeded job = %#v", succeeded)
	}

	byKey, err := store.GetByIdempotencyKey(ctx, saved.IdempotencyKey)
	if err != nil {
		t.Fatalf("GetByIdempotencyKey() error = %v", err)
	}
	if byKey.ID != saved.ID || byKey.Payload["prompt"] != "苏寂角色参考图" {
		t.Fatalf("job by idempotency key = %#v", byKey)
	}

	list, err := store.ListBySession(ctx, saved.SessionID)
	if err != nil {
		t.Fatalf("ListBySession() error = %v", err)
	}
	if len(list) == 0 || list[0].ID != saved.ID {
		t.Fatalf("jobs by session = %#v", list)
	}
}
