package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

func TestImage2JobHandlerGeneratesAndPersistsAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-image2-key" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "job-1" {
			t.Fatalf("idempotency key = %q, want job-1", got)
		}
		if got := r.Header.Get("X-Client-Request-Id"); got != "job-1" {
			t.Fatalf("client request id = %q, want job-1", got)
		}
		var req struct {
			Prompt string `json:"prompt"`
			Model  string `json:"model"`
			N      int    `json:"n"`
			Size   string `json:"size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if req.Prompt != "苏寂角色参考图" || req.Model != tools.DefaultImage2Model || req.N != 1 {
			t.Fatalf("provider request = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1783088529,
			"data":[{
				"b64_json":"iVBORw0KGgo=",
				"url":"http://provider/images/suji.png",
				"revised_prompt":"苏寂角色参考图"
			}]
		}`))
	}))
	defer server.Close()

	assets := &fakeAssetStore{}
	uploader := &fakeUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-1/image2-suji-1.png",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/image2-suji-1.png",
			SizeBytes: 8,
		},
	}
	handler := NewImage2JobHandler(Image2JobHandlerConfig{
		APIKey:        "test-image2-key",
		Endpoint:      server.URL,
		Assets:        assets,
		AssetUploader: uploader,
		NewID:         sequentialIDs("asset-1"),
		Now:           func() time.Time { return time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC) },
	})

	result, err := handler.Handle(context.Background(), generation.GenerationJob{
		ID:             "job-1",
		SessionID:      "s1",
		IdempotencyKey: "idem-1",
		Provider:       generation.ProviderImage2,
		TargetType:     generation.TargetKeyElement,
		TargetID:       "suji",
		Status:         generation.StatusRunning,
		Payload: map[string]any{
			"prompt":          "苏寂角色参考图",
			"filename_prefix": "image2-suji",
			"user_id":         "u1",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(result.AssetIDs) != 1 || !strings.HasPrefix(result.AssetIDs[0], "image_") {
		t.Fatalf("asset ids = %#v", result.AssetIDs)
	}
	assetID := result.AssetIDs[0]
	if result.Result["asset_ids"].([]string)[0] != assetID {
		t.Fatalf("handler result = %#v", result.Result)
	}
	if _, ok := result.Result["images"]; ok {
		t.Fatalf("handler result should not include raw image list: %#v", result.Result)
	}
	if updates, ok := result.Result["storyboard_updates"].([]tools.StoryboardUpdateHint); !ok || len(updates) != 1 || updates[0].AssetIDs[0] != assetID {
		t.Fatalf("storyboard updates = %#v", result.Result["storyboard_updates"])
	}
	if _, ok := result.Result["render_events"]; ok {
		t.Fatalf("handler result should not include render events: %#v", result.Result)
	}
	if string(uploader.body) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if assets.saved.ID != assetID || assets.saved.SessionID != "s1" || assets.saved.UserID != "u1" {
		t.Fatalf("saved asset = %#v", assets.saved)
	}
}

type fakeAssetStore struct {
	saved   asset.Asset
	records map[string]asset.Asset
}

func (s *fakeAssetStore) Save(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.saved = record
	return record, nil
}

func (s *fakeAssetStore) Get(_ context.Context, assetID string) (asset.Asset, error) {
	if s.records != nil {
		if record, ok := s.records[assetID]; ok {
			return record, nil
		}
	}
	return asset.Asset{}, asset.ErrNotFound
}

type fakeUploader struct {
	result asset.UploadResult
	body   []byte
}

func (u *fakeUploader) Upload(_ context.Context, input asset.UploadInput) (asset.UploadResult, error) {
	u.body, _ = io.ReadAll(input.Content)
	return u.result, nil
}

func sequentialIDs(ids ...string) func() string {
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
