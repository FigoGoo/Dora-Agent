package spec

import (
	"context"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStoreSavesFinalVideoSpecVersions(t *testing.T) {
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
	sessionID := fmt.Sprintf("session-%d", suffix)
	specID := fmt.Sprintf("spec-%d", suffix)

	saved, err := store.Save(ctx, FinalVideoSpec{
		ID:              specID,
		SessionID:       sessionID,
		Status:          StatusDraft,
		Title:           "归隐·藏锋",
		VideoType:       "武侠短片",
		DurationSeconds: 120,
		Fields:          map[string]any{"model_preference": "Nano Banana Pro"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.Version != 1 || saved.Status != StatusDraft {
		t.Fatalf("saved spec = %#v", saved)
	}

	saved, err = store.Save(ctx, FinalVideoSpec{
		ID:              specID,
		SessionID:       sessionID,
		Status:          StatusReviewing,
		Title:           "归隐·藏锋（苏寂出山）",
		VideoType:       "武侠短片",
		DurationSeconds: 120,
		Markdown:        "# Final Video Spec\n\nVideo Title: 归隐·藏锋",
	})
	if err != nil {
		t.Fatalf("Save(update) error = %v", err)
	}
	if saved.Version != 2 || saved.Status != StatusReviewing || saved.Markdown == "" {
		t.Fatalf("updated spec = %#v", saved)
	}

	latest, err := store.GetLatestBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetLatestBySession() error = %v", err)
	}
	if latest.ID != specID || latest.Version != 2 || latest.Title != "归隐·藏锋（苏寂出山）" {
		t.Fatalf("latest spec = %#v", latest)
	}
}
