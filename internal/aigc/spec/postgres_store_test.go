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

func TestPostgresStoreFencesCreationSpecReviewingRevisions(t *testing.T) {
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
	sessionID := fmt.Sprintf("session-fence-%d", suffix)
	specID := fmt.Sprintf("spec-fence-%d", suffix)
	v2, err := store.Save(ctx, FinalVideoSpec{ID: specID, SessionID: sessionID, Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("fence-v2-%d", suffix), Title: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	v3, err := store.Save(ctx, FinalVideoSpec{ID: specID, SessionID: sessionID, Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("fence-v3-%d", suffix), Title: "v3"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.GetRevision(ctx, specID, v2.Version)
	if err != nil || old.Status != StatusSuperseded {
		t.Fatalf("old=%+v err=%v", old, err)
	}
	latest, err := store.GetLatestReviewingBySession(ctx, sessionID)
	if err != nil || latest.Version != v3.Version {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	if _, err := store.DecideRevision(ctx, specID, v2.Version, true); err == nil {
		t.Fatal("superseded v2 must not be approved")
	}
	if _, err := store.DecideRevision(ctx, specID, v3.Version, true); err != nil {
		t.Fatal(err)
	}
	confirmed, err := store.GetConfirmedBySession(ctx, sessionID)
	if err != nil || confirmed.Version != v3.Version {
		t.Fatalf("confirmed=%+v err=%v", confirmed, err)
	}
}
