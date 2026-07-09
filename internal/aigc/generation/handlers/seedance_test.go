package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

func TestSeedanceJobHandlerGeneratesAndPersistsAsset(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks":
			var req struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.Model != "doubao-seedance-2-0-fast-260128" {
				t.Fatalf("model = %q", req.Model)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-1"}`))
		case "/tasks/cgt-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-1","status":"succeeded","output":{"video_url":"` + server.URL + `/video.mp4"}}`))
		case "/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	assets := &fakeAssetStore{}
	uploader := &fakeUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-video-1/seedance-shot-1.mp4",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-video-1/seedance-shot-1.mp4",
			SizeBytes: 8,
		},
	}
	handler := NewSeedanceJobHandler(SeedanceJobHandlerConfig{
		APIKey:          "test-seedance-key",
		Endpoint:        server.URL + "/tasks",
		Assets:          assets,
		AssetUploader:   uploader,
		NewID:           sequentialIDs("asset-video-1"),
		Now:             func() time.Time { return time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC) },
		PollInterval:    time.Millisecond,
		MaxPollAttempts: 1,
	})

	result, err := handler.Handle(context.Background(), generation.GenerationJob{
		ID:             "job-video-1",
		SessionID:      "s1",
		IdempotencyKey: "idem-video-1",
		Provider:       generation.ProviderSeedance,
		TargetType:     generation.TargetShot,
		TargetID:       "shot-1",
		Status:         generation.StatusRunning,
		Payload: map[string]any{
			"prompt":           "竹林中苏寂拔剑",
			"filename_prefix":  "seedance-shot",
			"user_id":          "u1",
			"ratio":            "16:9",
			"duration_seconds": float64(5),
			"resolution":       "720p",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(result.AssetIDs) != 1 || result.AssetIDs[0] != "asset-video-1" {
		t.Fatalf("asset ids = %#v", result.AssetIDs)
	}
	if result.Result["provider_task_id"] != "cgt-1" {
		t.Fatalf("handler result = %#v", result.Result)
	}
	if _, ok := result.Result["provider_video_url"]; ok {
		t.Fatalf("handler result should not expose provider video url: %#v", result.Result)
	}
	if updates, ok := result.Result["storyboard_updates"].([]tools.StoryboardUpdateHint); !ok || len(updates) != 1 || updates[0].Field != "video_asset_id" {
		t.Fatalf("storyboard updates = %#v", result.Result["storyboard_updates"])
	}
	if _, ok := result.Result["render_events"]; ok {
		t.Fatalf("handler result should not include render events: %#v", result.Result)
	}
	if string(uploader.body) != "mp4bytes" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if assets.saved.ID != "asset-video-1" || assets.saved.Kind != asset.KindVideo || assets.saved.SessionID != "s1" {
		t.Fatalf("saved asset = %#v", assets.saved)
	}
}

func TestSeedanceJobHandlerUsesBoundAssetsAsReferences(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks":
			var req struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url,omitempty"`
				} `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if len(req.Content) != 2 || req.Content[1].Type != "image_url" || req.Content[1].ImageURL.URL != "https://tos.doraigc.com/ref/suji.png" {
				t.Fatalf("content references = %#v", req.Content)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-ref-1"}`))
		case "/tasks/cgt-ref-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-ref-1","status":"succeeded","output":{"video_url":"` + server.URL + `/video.mp4"}}`))
		case "/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	assets := &fakeAssetStore{
		records: map[string]asset.Asset{
			"asset-ref-1": {ID: "asset-ref-1", Kind: asset.KindImage, URL: "https://tos.doraigc.com/ref/suji.png"},
		},
	}
	uploader := &fakeUploader{result: asset.UploadResult{Provider: asset.StorageProviderTOS, URL: "https://tos.doraigc.com/video.mp4", SizeBytes: 8}}
	handler := NewSeedanceJobHandler(SeedanceJobHandlerConfig{
		APIKey:          "test-seedance-key",
		Endpoint:        server.URL + "/tasks",
		Assets:          assets,
		AssetUploader:   uploader,
		NewID:           sequentialIDs("asset-video-1"),
		PollInterval:    time.Millisecond,
		MaxPollAttempts: 1,
	})

	if _, err := handler.Handle(context.Background(), generation.GenerationJob{
		ID:             "job-video-ref",
		SessionID:      "s1",
		IdempotencyKey: "idem-video-ref",
		Provider:       generation.ProviderSeedance,
		TargetType:     generation.TargetShot,
		TargetID:       "shot-1",
		Status:         generation.StatusRunning,
		Payload: map[string]any{
			"prompt":         "参考苏寂角色图生成镜头",
			"bind_asset_ids": []string{"asset-ref-1"},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}
