package artifact

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestMemoryStoreVersionsIdempotencyAndActivation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first, err := store.CreateRevision(ctx, Revision{ID: "a1", SessionID: "s1", Kind: KindMaterialAnalysis, IdempotencyKey: "k1", Content: map[string]any{"summary": "one"}})
	if err != nil || first.Revision.Version != 1 || !first.Created {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := store.CreateRevision(ctx, Revision{ID: "a1", SessionID: "s1", Kind: KindMaterialAnalysis, IdempotencyKey: "k1", Content: map[string]any{"summary": "one"}})
	if err != nil || replay.Created || replay.Revision.ID != "a1" {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	for name, conflict := range map[string]Revision{
		"id":      {ID: "different", SessionID: "s1", Kind: KindMaterialAnalysis, IdempotencyKey: "k1", Content: map[string]any{"summary": "one"}},
		"session": {ID: "a1", SessionID: "other", Kind: KindMaterialAnalysis, IdempotencyKey: "k1", Content: map[string]any{"summary": "one"}},
		"kind":    {ID: "a1", SessionID: "s1", Kind: KindAssemblyPlan, IdempotencyKey: "k1", Content: map[string]any{"summary": "one"}},
		"content": {ID: "a1", SessionID: "s1", Kind: KindMaterialAnalysis, IdempotencyKey: "k1", Content: map[string]any{"summary": "changed"}},
	} {
		t.Run("conflict_"+name, func(t *testing.T) {
			if _, err := store.CreateRevision(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	second, err := store.CreateRevision(ctx, Revision{ID: "a2", SessionID: "s1", Kind: KindMaterialAnalysis, IdempotencyKey: "k2", Content: map[string]any{"summary": "two"}})
	if err != nil || second.Revision.Version != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := store.Activate(ctx, "a1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(ctx, "a2", 2); err != nil {
		t.Fatal(err)
	}
	old, _ := store.Get(ctx, "a1")
	if old.Status != StatusSuperseded {
		t.Fatalf("old status=%s", old.Status)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestMemoryStoreReviewReceiptIsFirstWriteAndReplayDoesNotRollbackLatest(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first, err := store.CreateRevision(ctx, Revision{
		ID: "export-a", SessionID: "s1", Kind: KindExportResult, Status: StatusReviewing,
		IdempotencyKey: "create-export-a", Content: map[string]any{"url": "a.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	commandA := ReviewCommand{
		IdempotencyKey: "approve-export-a", SessionID: "s1", ArtifactID: first.Revision.ID,
		ArtifactKind: KindExportResult, ArtifactVersion: first.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	}

	const callers = 8
	results := make(chan ReviewResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, applyErr := store.ApplyReview(ctx, commandA)
			results <- result
			errs <- applyErr
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for applyErr := range errs {
		if applyErr != nil {
			t.Fatal(applyErr)
		}
	}
	applied := 0
	for result := range results {
		if result.Applied {
			applied++
		}
		if result.Revision.ID != first.Revision.ID || result.Revision.Status != StatusActive {
			t.Fatalf("review result=%+v", result)
		}
	}
	if applied != 1 {
		t.Fatalf("first-write applied count=%d, want 1", applied)
	}

	second, err := store.CreateRevision(ctx, Revision{
		ID: "export-b", SessionID: "s1", Kind: KindExportResult, Status: StatusReviewing,
		IdempotencyKey: "create-export-b", Content: map[string]any{"url": "b.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyReview(ctx, ReviewCommand{
		IdempotencyKey: "approve-export-b", SessionID: "s1", ArtifactID: second.Revision.ID,
		ArtifactKind: KindExportResult, ArtifactVersion: second.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	}); err != nil {
		t.Fatal(err)
	}
	if replay, err := store.ApplyReview(ctx, commandA); err != nil || replay.Applied || replay.Revision.Status != StatusActive {
		t.Fatalf("receipt replay=%+v err=%v", replay, err)
	}
	current, _ := store.Get(ctx, second.Revision.ID)
	old, _ := store.Get(ctx, first.Revision.ID)
	if current.Status != StatusActive || old.Status != StatusSuperseded {
		t.Fatalf("replay rolled back latest: old=%s current=%s", old.Status, current.Status)
	}
	conflict := commandA
	conflict.ArtifactID = second.Revision.ID
	conflict.ArtifactVersion = second.Revision.Version
	if _, err := store.ApplyReview(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same-key different request error=%v", err)
	}
}

func TestMemoryStoreReviewRequiresLatestRevision(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first, _ := store.CreateRevision(ctx, Revision{
		ID: "stale-export-a", SessionID: "s1", Kind: KindExportResult, Status: StatusReviewing,
		IdempotencyKey: "create-stale-a", Content: map[string]any{"url": "a.mp4"},
	})
	second, _ := store.CreateRevision(ctx, Revision{
		ID: "stale-export-b", SessionID: "s1", Kind: KindExportResult, Status: StatusReviewing,
		IdempotencyKey: "create-stale-b", Content: map[string]any{"url": "b.mp4"},
	})
	_, err := store.ApplyReview(ctx, ReviewCommand{
		IdempotencyKey: "approve-stale-a", SessionID: "s1", ArtifactID: first.Revision.ID,
		ArtifactKind: KindExportResult, ArtifactVersion: first.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("stale review error=%v", err)
	}
	old, _ := store.Get(ctx, first.Revision.ID)
	latest, _ := store.Get(ctx, second.Revision.ID)
	if old.Status != StatusReviewing || latest.Status != StatusReviewing {
		t.Fatalf("stale command mutated artifacts: old=%s latest=%s", old.Status, latest.Status)
	}
	if _, err := store.GetReviewReceipt(ctx, "approve-stale-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale command persisted receipt: %v", err)
	}
}
