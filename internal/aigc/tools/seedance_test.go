package tools

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
)

func TestSeedanceGenerateToolPollsAndUploadsAsset(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/contents/generations/tasks":
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-seedance-key" {
				t.Fatalf("authorization header = %q", got)
			}
			var req struct {
				Model   string `json:"model"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.Model != DefaultSeedanceModel {
				t.Fatalf("model = %q", req.Model)
			}
			if len(req.Content) != 1 || req.Content[0].Type != "text" {
				t.Fatalf("content = %#v", req.Content)
			}
			text := req.Content[0].Text
			for _, want := range []string{"竹林中苏寂拔剑", "--ratio 16:9", "--dur 5", "--resolution 720p"} {
				if !strings.Contains(text, want) {
					t.Fatalf("prompt text %q does not contain %q", text, want)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-1"}`))
		case "/api/v3/contents/generations/tasks/cgt-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-1","status":"succeeded","output":{"video_url":"` + server.URL + `/videos/cgt-1.mp4"}}`))
		case "/videos/cgt-1.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	assets := &fakeSeedanceAssetStore{}
	uploader := &fakeSeedanceUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-video-1/seedance-shot-1.mp4",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-video-1/seedance-shot-1.mp4",
			SizeBytes: 8,
		},
	}
	tool := NewSeedanceGenerateTool(SeedanceToolConfig{
		APIKey:          "test-seedance-key",
		Endpoint:        server.URL + "/api/v3/contents/generations/tasks",
		Assets:          assets,
		AssetUploader:   uploader,
		NewID:           sequentialToolIDs("asset-video-1"),
		Now:             func() time.Time { return time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC) },
		PollInterval:    time.Millisecond,
		MaxPollAttempts: 1,
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"user_id":"u1",
		"target_type":"shot",
		"target_id":"shot-1",
		"filename_prefix":"seedance-shot",
		"prompt":"竹林中苏寂拔剑",
		"ratio":"16:9",
		"duration_seconds":5,
		"resolution":"720p"
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var result ToolResultEnvelope[SeedanceGenerateResult]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != ToolStatusOK {
		t.Fatalf("status = %q", result.Status)
	}
	if result.Data.ProviderTaskID != "cgt-1" || result.Data.AssetID != "asset-video-1" {
		t.Fatalf("result data = %#v", result.Data)
	}
	if strings.Contains(out, "provider_video_url") || strings.Contains(out, server.URL+"/videos/cgt-1.mp4") {
		t.Fatalf("tool result leaked provider video url: %s", out)
	}
	if len(result.ArtifactIDs) != 1 || result.ArtifactIDs[0] != "asset-video-1" {
		t.Fatalf("artifact ids = %#v", result.ArtifactIDs)
	}
	if result.Data.URL != uploader.result.URL {
		t.Fatalf("url = %q", result.Data.URL)
	}
	if len(result.Data.Assets) != 1 || result.Data.Assets[0].Kind != asset.KindVideo || result.Data.Assets[0].AssetID != "asset-video-1" {
		t.Fatalf("assets = %#v", result.Data.Assets)
	}
	if len(result.Data.StoryboardUpdates) != 1 || result.Data.StoryboardUpdates[0].Field != "video_asset_id" {
		t.Fatalf("storyboard updates = %#v", result.Data.StoryboardUpdates)
	}
	if len(result.Data.RenderEvents) == 0 {
		t.Fatalf("render events are missing")
	}
	if string(uploader.body) != "mp4bytes" || uploader.seen.MIMEType != "video/mp4" {
		t.Fatalf("upload = %#v body=%q", uploader.seen, string(uploader.body))
	}
	if assets.saved.ID != "asset-video-1" || assets.saved.Kind != asset.KindVideo || assets.saved.SessionID != "s1" {
		t.Fatalf("saved asset = %#v", assets.saved)
	}
}

type fakeSeedanceAssetStore struct {
	saved asset.Asset
}

func (s *fakeSeedanceAssetStore) Save(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.saved = record
	return record, nil
}

type fakeSeedanceUploader struct {
	result asset.UploadResult
	body   []byte
	seen   asset.UploadInput
}

func (u *fakeSeedanceUploader) Upload(_ context.Context, input asset.UploadInput) (asset.UploadResult, error) {
	u.seen = input
	u.body, _ = io.ReadAll(input.Content)
	return u.result, nil
}
