package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

func TestDemoVisualJobHandlerPersistsImageAndVideoPlaceholders(t *testing.T) {
	for _, test := range []struct {
		name, provider, mediaKind, wantKind, wantMIME string
	}{
		{name: "image", provider: generation.ProviderImage2, mediaKind: "image", wantKind: asset.KindImage, wantMIME: "image/png"},
		{name: "video", provider: generation.ProviderSeedance, mediaKind: "video", wantKind: asset.KindVideo, wantMIME: "video/mp4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &capturingDemoAssetStore{}
			uploader := &capturingDemoUploader{}
			handler := NewDemoVisualJobHandler(DemoVisualJobHandlerConfig{Assets: store, Uploader: uploader})
			result, err := handler.Handle(context.Background(), generation.GenerationJob{
				ID: "job-" + test.name, SessionID: "session", UserID: "user", Provider: test.provider, MediaKind: test.mediaKind,
				Payload: map[string]any{"prompt": "local demo"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.AssetIDs) != 1 || store.saved.Kind != test.wantKind || store.saved.MIMEType != test.wantMIME || len(uploader.body) == 0 {
				t.Fatalf("result=%+v asset=%+v uploaded=%d", result, store.saved, len(uploader.body))
			}
			if test.wantKind == asset.KindVideo && !bytes.Contains(uploader.body[:min(len(uploader.body), 32)], []byte("ftyp")) {
				t.Fatalf("demo video is not an MP4 header: %x", uploader.body[:min(len(uploader.body), 32)])
			}
		})
	}
}

type capturingDemoAssetStore struct{ saved asset.Asset }

func (s *capturingDemoAssetStore) Save(_ context.Context, value asset.Asset) (asset.Asset, error) {
	s.saved = value
	return value, nil
}

type capturingDemoUploader struct{ body []byte }

func (u *capturingDemoUploader) Upload(_ context.Context, input asset.UploadInput) (asset.UploadResult, error) {
	u.body = make([]byte, input.ContentLength)
	_, _ = input.Content.Read(u.body)
	return asset.UploadResult{Provider: asset.StorageProviderLocal, Bucket: "local", ObjectKey: input.ObjectKey, URL: "/local/" + input.ObjectKey, SizeBytes: int64(len(u.body))}, nil
}
