package handlers

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

type fakeAssetSaver struct {
	saved []asset.Asset
}

func (f *fakeAssetSaver) Save(_ context.Context, a asset.Asset) (asset.Asset, error) {
	f.saved = append(f.saved, a)
	return a, nil
}

func TestDemoMediaJobHandlerPersistsFixedAsset(t *testing.T) {
	saver := &fakeAssetSaver{}
	h := NewDemoMediaJobHandler(DemoMediaJobHandlerConfig{
		Assets:   saver,
		Provider: generation.ProviderSeedance,
		Kind:     asset.KindVideo,
		MIMEType: "video/mp4",
		URLs:     []string{"/demo/demo-shot.mp4"},
	})

	res, err := h.Handle(context.Background(), generation.GenerationJob{
		ID:         "job-1",
		SessionID:  "s1",
		TargetType: "shot",
		TargetID:   "shot-3",
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "demo-asset:job-1" {
		t.Fatalf("AssetIDs = %v", res.AssetIDs)
	}
	if len(saver.saved) != 1 {
		t.Fatalf("expected 1 saved asset, got %d", len(saver.saved))
	}
	got := saver.saved[0]
	if got.Kind != asset.KindVideo || got.URL != "/demo/demo-shot.mp4" || got.SessionID != "s1" {
		t.Fatalf("saved asset mismatch: %+v", got)
	}
}

func TestDemoMediaJobHandlerRotatesURLsByTarget(t *testing.T) {
	h := NewDemoMediaJobHandler(DemoMediaJobHandlerConfig{
		Provider: generation.ProviderImage2,
		Kind:     asset.KindImage,
		URLs:     []string{"/a.png", "/b.png", "/c.png"},
	})
	// Same target → same URL (deterministic); some targets differ.
	if h.pickURL("k1") != h.pickURL("k1") {
		t.Fatalf("pickURL not deterministic for same target")
	}
	seen := map[string]bool{}
	for _, tid := range []string{"k1", "k2", "k3", "k4", "k5", "k6"} {
		seen[h.pickURL(tid)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected variety across targets, got %v", seen)
	}
}

func TestDemoMediaJobHandlerErrorsWithoutStoreOrURLs(t *testing.T) {
	noStore := NewDemoMediaJobHandler(DemoMediaJobHandlerConfig{URLs: []string{"/a.png"}})
	if _, err := noStore.Handle(context.Background(), generation.GenerationJob{ID: "j"}); err == nil {
		t.Fatalf("expected error when asset store is nil")
	}
	noURLs := NewDemoMediaJobHandler(DemoMediaJobHandlerConfig{Assets: &fakeAssetSaver{}})
	if _, err := noURLs.Handle(context.Background(), generation.GenerationJob{ID: "j"}); err == nil {
		t.Fatalf("expected error when no urls configured")
	}
}
