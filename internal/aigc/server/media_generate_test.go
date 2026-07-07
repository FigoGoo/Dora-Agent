package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type fakeMediaDispatcher struct {
	jobs      []generation.GenerationJob
	created   map[string]bool // idempotency keys that already exist (reused)
	dispatchN int
}

func (d *fakeMediaDispatcher) Dispatch(_ context.Context, job generation.GenerationJob) (generation.GenerationJob, bool, error) {
	d.dispatchN++
	if job.Status == "" {
		job.Status = generation.StatusQueued
	}
	d.jobs = append(d.jobs, job)
	if d.created != nil && d.created[job.IdempotencyKey] {
		return job, false, nil
	}
	return job, true, nil
}

func TestGenerateSessionMediaDispatchesUnfilledTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	boards := &fakeStoryboardStore{
		latest: storyboard.Storyboard{
			ID:        "sb1",
			SessionID: "s1",
			Version:   3,
			KeyElements: []storyboard.KeyElement{
				{Key: "e1", Prompt: "p", Status: "prompt_ready"},                    // dispatch image2
				{Key: "e2", AssetIDs: []string{"a1"}, Status: "ready"},              // skip (bound)
			},
			Shots: []storyboard.Shot{
				{ShotID: "shot-1", Prompt: "p", Status: "prompt_ready"},             // dispatch seedance
				{ShotID: "shot-2", VideoAssetID: "v1", Status: "ready"},             // skip (bound)
			},
			AudioLayers: []storyboard.AudioLayer{
				{LayerID: "m1", Status: "planned"},                                  // dispatch audio
				{LayerID: "m2", AssetID: "au1", Status: "ready"},                    // skip (bound)
			},
		},
	}
	disp := &fakeMediaDispatcher{}
	router := NewRouter(Config{
		Store:           store,
		Storyboards:     boards,
		MediaDispatcher: disp,
		Invoker:         &fakeAgentInvoker{},
		NewID:           func() string { return "id" },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/media/generate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Dispatched int `json:"dispatched"`
		Reused     int `json:"reused"`
		Skipped    int `json:"skipped"`
		Jobs       []struct {
			JobID      string `json:"job_id"`
			Provider   string `json:"provider"`
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Created    bool   `json:"created"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Dispatched != 3 || got.Reused != 0 || got.Skipped != 3 {
		t.Fatalf("counts: dispatched=%d reused=%d skipped=%d", got.Dispatched, got.Reused, got.Skipped)
	}
	if len(got.Jobs) != 3 {
		t.Fatalf("jobs len = %d, want 3: %#v", len(got.Jobs), got.Jobs)
	}

	// Verify each dispatched job targets the right provider/target and carries the
	// storyboard id + a stable idempotency key.
	byTarget := map[string]generation.GenerationJob{}
	for _, j := range disp.jobs {
		byTarget[j.TargetType+"/"+j.TargetID] = j
	}
	e1 := byTarget["key_element/e1"]
	if e1.Provider != generation.ProviderImage2 || e1.StoryboardID != "sb1" ||
		e1.IdempotencyKey != "media:s1:key_element:e1" {
		t.Fatalf("e1 job = %#v", e1)
	}
	shot := byTarget["shot/shot-1"]
	if shot.Provider != generation.ProviderSeedance || shot.Payload["field"] != "video_asset_id" {
		t.Fatalf("shot job = %#v", shot)
	}
	if byTarget["audio_layer/m1"].Provider != generation.ProviderAudio {
		t.Fatalf("audio job = %#v", byTarget["audio_layer/m1"])
	}
}

func TestGenerateSessionMediaReturnsNotFoundWithoutStoryboard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	boards := &fakeStoryboardStore{getErr: storyboard.ErrNotFound}
	router := NewRouter(Config{
		Store:           store,
		Storyboards:     boards,
		MediaDispatcher: &fakeMediaDispatcher{},
		Invoker:         &fakeAgentInvoker{},
		NewID:           func() string { return "id" },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/media/generate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
