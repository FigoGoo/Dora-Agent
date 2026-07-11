package spec

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreFencesCreationSpecReviewingRevisions(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(WithMemoryClock(func() time.Time {
		now = now.Add(time.Second)
		return now
	}))

	v1, err := store.Save(ctx, FinalVideoSpec{
		ID: "spec-1", SessionID: "session-1", Status: StatusReviewing,
		IdempotencyKey: "plan-v1", Title: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := store.Save(ctx, FinalVideoSpec{
		ID: "spec-1", SessionID: "session-1", Status: StatusReviewing,
		IdempotencyKey: "plan-v2", Title: "v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.GetRevision(ctx, v1.ID, v1.Version)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != StatusSuperseded {
		t.Fatalf("v1 status=%s", old.Status)
	}
	latest, err := store.GetLatestReviewingBySession(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != v2.Version || latest.Title != "v2" {
		t.Fatalf("latest reviewing=%+v", latest)
	}
	if _, err := store.DecideRevision(ctx, v1.ID, v1.Version, true); err == nil {
		t.Fatal("superseded v1 must not be approved")
	}

	// Replaying an older create request must return its persisted result without
	// making that old result reviewable again.
	replay, err := store.Save(ctx, FinalVideoSpec{ID: "ignored", SessionID: "session-1", Status: StatusReviewing, IdempotencyKey: "plan-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Version != v1.Version || replay.Status != StatusSuperseded {
		t.Fatalf("replayed v1=%+v", replay)
	}
	latest, err = store.GetLatestReviewingBySession(ctx, "session-1")
	if err != nil || latest.Version != v2.Version {
		t.Fatalf("latest after replay=%+v err=%v", latest, err)
	}

	if _, err := store.DecideRevision(ctx, v2.ID, v2.Version, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetLatestReviewingBySession(ctx, "session-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no reviewing candidate, got %v", err)
	}
	v3, err := store.Save(ctx, FinalVideoSpec{ID: "spec-1", SessionID: "session-1", Status: StatusReviewing, IdempotencyKey: "plan-v3", Title: "v3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRevision(ctx, v3.ID, v3.Version, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRevision(ctx, v2.ID, v2.Version, true); err == nil {
		t.Fatal("approving v2 after v3 must not roll the confirmed spec back")
	}
	confirmed, err := store.GetConfirmedBySession(ctx, "session-1")
	if err != nil || confirmed.Version != v3.Version {
		t.Fatalf("confirmed=%+v err=%v", confirmed, err)
	}
}
