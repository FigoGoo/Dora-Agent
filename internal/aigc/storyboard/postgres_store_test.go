package storyboard

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStoreAppliesPatchWithVersionEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("local postgres is not available: %v", err)
	}

	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	suffix := time.Now().UnixNano()
	board := Storyboard{
		ID:        fmt.Sprintf("storyboard-%d", suffix),
		SessionID: fmt.Sprintf("session-%d", suffix),
		SpecID:    "spec-1",
		Version:   1,
		Status:    StatusDraft,
		KeyElements: []KeyElement{
			{Key: "suji", Type: "character", Name: "苏寂", AssetIDs: []string{"asset-1"}, Status: "planned"},
		},
		Shots: []Shot{
			{ShotID: "shot-1", Index: 1, SceneDescription: "竹林归隐", Status: "planned"},
		},
	}
	if err := store.SaveSnapshot(ctx, board); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}
	latest, err := store.GetLatestBySession(ctx, board.SessionID)
	if err != nil {
		t.Fatalf("GetLatestBySession() error = %v", err)
	}
	if latest.ID != board.ID || latest.Version != 1 {
		t.Fatalf("latest storyboard = %#v", latest)
	}

	patched, event, err := store.ApplyPatch(ctx, PatchRequest{
		EventID:      fmt.Sprintf("event-%d", suffix),
		SessionID:    board.SessionID,
		StoryboardID: board.ID,
		BaseVersion:  1,
		Source:       "user",
		Ops: []patch.JSONPatchOp{
			{Op: "replace", Path: "/key_elements/0/status", Value: "asset_required"},
			{Op: "replace", Path: "/shots/0/prompt", Value: "35mm film, cold bamboo forest"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if patched.Version != 2 || patched.KeyElements[0].Status != "asset_required" || patched.Shots[0].Prompt == "" {
		t.Fatalf("unexpected patched storyboard: %#v", patched)
	}
	if event.NextVersion != 2 || event.BaseVersion != 1 || event.Source != "user" {
		t.Fatalf("unexpected patch event: %#v", event)
	}

	patched, _, err = store.ApplyPatch(ctx, PatchRequest{
		EventID:      fmt.Sprintf("event-%d-array", suffix),
		SessionID:    board.SessionID,
		StoryboardID: board.ID,
		BaseVersion:  2,
		Source:       "user",
		Ops: []patch.JSONPatchOp{
			{Op: "add", Path: "/shots/1", Value: map[string]any{"shot_id": "shot-2", "index": 2, "scene_description": "雨夜拔剑"}},
			{Op: "remove", Path: "/key_elements/0/asset_ids/0"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatch(array) error = %v", err)
	}
	if patched.Version != 3 || len(patched.Shots) != 2 || patched.Shots[1].ShotID != "shot-2" {
		t.Fatalf("unexpected array patched storyboard: %#v", patched)
	}

	_, _, err = store.ApplyPatch(ctx, PatchRequest{
		EventID:      fmt.Sprintf("event-%d-conflict", suffix),
		SessionID:    board.SessionID,
		StoryboardID: board.ID,
		BaseVersion:  1,
		Source:       "user",
		Ops:          []patch.JSONPatchOp{{Op: "replace", Path: "/shots/0/status", Value: StatusConfirmed}},
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("ApplyPatch(conflict) error = %v, want ErrVersionConflict", err)
	}
}
