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

func TestImage2GenerateToolUsesChange2ProDefaultEndpointAndPersistsB64JSON(t *testing.T) {
	client := &http.Client{Transport: image2RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.String(); got != DefaultImage2Endpoint {
			t.Fatalf("provider URL = %q, want %q", got, DefaultImage2Endpoint)
		}
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-image2-key" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		if got := req.Header.Get("Idempotency-Key"); got != "job-1" {
			t.Fatalf("idempotency key = %q, want job-1", got)
		}

		var body image2APIRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if body.Model != DefaultImage2Model || body.Prompt != "A cat" || body.N != 1 || body.Size != DefaultImage2Size {
			t.Fatalf("provider request = %#v", body)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"created":1783088529,
				"data":[{"b64_json":"iVBORw0KGgo=","revised_prompt":"A cat"}]
			}`)),
			Request: req,
		}, nil
	})}
	assets := &fakeImageAssetStore{}
	uploader := &fakeImageAssetUploader{result: asset.UploadResult{
		Provider:  asset.StorageProviderTOS,
		Bucket:    "dora-public",
		ObjectKey: "aigc/sessions/s1/assets/image/image2-1.png",
		URL:       "https://tos.example/image2-1.png",
		SizeBytes: 8,
	}}
	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:        "test-image2-key",
		HTTPClient:    client,
		Assets:        assets,
		AssetUploader: uploader,
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"prompt":"A cat","source_job_id":"job-1"}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if string(uploader.body) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if uploader.seen.MIMEType != "image/png" {
		t.Fatalf("upload MIME type = %q, want image/png", uploader.seen.MIMEType)
	}
	if assets.saved.ID == "" || assets.saved.URL != uploader.result.URL {
		t.Fatalf("saved asset = %#v", assets.saved)
	}
	if strings.Contains(out, "b64_json") || strings.Contains(out, "iVBOR") {
		t.Fatalf("tool result leaked provider image bytes: %s", out)
	}
}

func TestImage2GenerateToolSendsRequestAndRedactsProviderPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-image2-key" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}

		var req image2APIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != DefaultImage2Model || req.Prompt != "A cat" || req.N != 1 || req.Size != DefaultImage2Size {
			t.Fatalf("unexpected request: %#v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1783088529,
			"data":[{
				"b64_json":"iVBORw0KG=",
				"url":"http://127.0.0.1:3001/images/demo.png",
				"revised_prompt":"A cat"
			}],
			"usage":{
				"input_tokens":13,
				"output_tokens":1650,
				"total_tokens":1663,
				"input_tokens_details":{"text_tokens":13,"image_tokens":0,"cached_tokens":0},
				"output_tokens_details":{"text_tokens":0,"image_tokens":1650,"reasoning_tokens":0}
			}
		}`))
	}))
	defer server.Close()

	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:   "test-image2-key",
		Endpoint: server.URL,
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"prompt":"A cat"}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var result ToolResultEnvelope[Image2GenerateResult]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != ToolStatusOK {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	if strings.Contains(out, "b64_json") || strings.Contains(out, "data_url") {
		t.Fatalf("tool result leaked generated image bytes: %s", out)
	}
	if len(result.Data.Assets) != 1 {
		t.Fatalf("asset count = %d", len(result.Data.Assets))
	}
	image := result.Data.Assets[0]
	if image.AssetID != "" || image.URL != "" {
		t.Fatalf("non-persisted image should not expose provider artifact: %#v", image)
	}
	if image.Kind != asset.KindImage || image.Status != "generated_not_persisted" {
		t.Fatalf("asset business info = %#v", image)
	}
	if strings.Contains(out, "render_events") {
		t.Fatalf("tool result should not include render events: %s", out)
	}
}

func TestImage2GenerateToolAcceptsEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"/9j/demo"}]}`))
	}))
	defer server.Close()

	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:   "test-image2-key",
		Endpoint: server.URL,
	})
	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate",
		"payload":{"prompt":"portrait","n":2,"size":"1536x1024"}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var result ToolResultEnvelope[Image2GenerateResult]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.RequestID != "req-1" || result.IdempotencyKey != "idem-1" {
		t.Fatalf("envelope metadata was not preserved: %#v", result)
	}
	if strings.Contains(out, "b64_json") || strings.Contains(out, "data_url") {
		t.Fatalf("tool result leaked generated image bytes: %s", out)
	}
	if len(result.Data.Assets) != 1 || result.Data.Assets[0].Kind != asset.KindImage {
		t.Fatalf("asset business info = %#v", result.Data.Assets)
	}
}

func TestImage2GenerateToolUploadsAssetWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1783088529,
			"data":[{
				"b64_json":"iVBORw0KGgo=",
				"url":"http://provider/images/demo.png",
				"revised_prompt":"A cat"
			}]
		}`))
	}))
	defer server.Close()

	assets := &fakeImageAssetStore{}
	uploader := &fakeImageAssetUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-1/image2-1.png",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/image2-1.png",
			SizeBytes: 8,
		},
	}
	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:        "test-image2-key",
		Endpoint:      server.URL,
		Assets:        assets,
		AssetUploader: uploader,
		NewID:         sequentialToolIDs("asset-1"),
		Now:           func() time.Time { return time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC) },
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"user_id":"u1","target_type":"shot","target_id":"shot-1","prompt":"A cat"}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var result ToolResultEnvelope[Image2GenerateResult]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if strings.Contains(out, "b64_json") || strings.Contains(out, "data_url") || strings.Contains(out, "provider_url") {
		t.Fatalf("tool result leaked provider payload: %s", out)
	}
	if len(result.ArtifactIDs) != 1 || result.ArtifactIDs[0] != "asset-1" {
		t.Fatalf("artifact ids = %#v", result.ArtifactIDs)
	}
	if len(result.Data.Assets) != 1 {
		t.Fatalf("asset count = %d", len(result.Data.Assets))
	}
	image := result.Data.Assets[0]
	if image.AssetID != "asset-1" || image.URL != uploader.result.URL {
		t.Fatalf("image = %#v", image)
	}
	if image.Kind != asset.KindImage || image.Status != "generated" {
		t.Fatalf("asset business info = %#v", image)
	}
	if len(result.Data.StoryboardUpdates) != 1 || result.Data.StoryboardUpdates[0].AssetIDs[0] != "asset-1" {
		t.Fatalf("storyboard updates = %#v", result.Data.StoryboardUpdates)
	}
	if strings.Contains(out, "render_events") {
		t.Fatalf("tool result should not include render events: %s", out)
	}
	if string(uploader.body) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if assets.saved.ID != "asset-1" || assets.saved.SessionID != "s1" || assets.saved.Kind != asset.KindImage {
		t.Fatalf("saved asset = %#v", assets.saved)
	}
}

func TestImage2GenerateToolDownloadsProviderURLWhenB64Missing(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nprovider-bytes"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1783088529,
			"data":[{
				"url":"` + server.URL + `/generated.png",
				"revised_prompt":"A cat"
			}]
		}`))
	}))
	defer server.Close()

	assets := &fakeImageAssetStore{}
	uploader := &fakeImageAssetUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-1/image2-1.png",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/image2-1.png",
			SizeBytes: 26,
		},
	}
	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:        "test-image2-key",
		Endpoint:      server.URL,
		Assets:        assets,
		AssetUploader: uploader,
		NewID:         sequentialToolIDs("asset-1"),
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"target_type":"shot","target_id":"shot-1","prompt":"A cat"}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if string(uploader.body) != "\x89PNG\r\n\x1a\nprovider-bytes" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if uploader.seen.ContentLength != int64(len(uploader.body)) || uploader.seen.MIMEType != "image/png" {
		t.Fatalf("upload input = %#v", uploader.seen)
	}
	var result ToolResultEnvelope[Image2GenerateResult]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Data.Assets) != 1 || result.Data.Assets[0].AssetID != "asset-1" {
		t.Fatalf("assets = %#v", result.Data.Assets)
	}
}

func TestImage2GenerateToolRejectsEmptyProviderImage(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "image/png")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1783088529,
			"data":[{
				"url":"` + server.URL + `/empty.png",
				"revised_prompt":"A cat"
			}]
		}`))
	}))
	defer server.Close()

	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:        "test-image2-key",
		Endpoint:      server.URL,
		Assets:        &fakeImageAssetStore{},
		AssetUploader: &fakeImageAssetUploader{},
		NewID:         sequentialToolIDs("asset-1"),
	})

	_, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"prompt":"A cat"}
	}`)
	if err == nil || !strings.Contains(err.Error(), "empty image") {
		t.Fatalf("error = %v, want empty image error", err)
	}
}

func TestImage2GenerateToolRejectsProviderResponseWithoutImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783088529,"data":[]}`))
	}))
	defer server.Close()

	tool := NewImage2GenerateTool(Image2ToolConfig{
		APIKey:   "test-image2-key",
		Endpoint: server.URL,
	})
	_, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"prompt":"A cat"}
	}`)
	if err == nil || !strings.Contains(err.Error(), "did not include images") {
		t.Fatalf("error = %v, want missing images error", err)
	}
}

func TestImage2GenerateToolRequiresAPIKey(t *testing.T) {
	tool := NewImage2GenerateTool(Image2ToolConfig{})
	if _, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"generate_image",
		"payload":{"prompt":"A cat"}
	}`); err == nil {
		t.Fatalf("expected missing api key error")
	}
}

func TestImage2GenerateToolRejectsDirectPayload(t *testing.T) {
	tool := NewImage2GenerateTool(Image2ToolConfig{})
	if _, err := tool.InvokableRun(context.Background(), `{"prompt":"A cat"}`); err == nil {
		t.Fatal("expected direct payload to be rejected")
	}
}

type fakeImageAssetStore struct {
	saved asset.Asset
}

func (s *fakeImageAssetStore) Save(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.saved = record
	return record, nil
}

type fakeImageAssetUploader struct {
	result asset.UploadResult
	body   []byte
	seen   asset.UploadInput
}

func (u *fakeImageAssetUploader) Upload(_ context.Context, input asset.UploadInput) (asset.UploadResult, error) {
	u.seen = input
	u.body, _ = io.ReadAll(input.Content)
	return u.result, nil
}

func sequentialToolIDs(ids ...string) func() string {
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

type image2RoundTripFunc func(*http.Request) (*http.Response, error)

func (fn image2RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
