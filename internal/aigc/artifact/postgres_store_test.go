package artifact

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresReviewReceiptReplayDoesNotRollbackNewerArtifact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("local postgres is not available: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UnixNano()
	sessionID := fmt.Sprintf("artifact-review-session-%d", suffix)
	first, err := store.CreateRevision(ctx, Revision{
		ID: fmt.Sprintf("artifact-review-a-%d", suffix), SessionID: sessionID, Kind: KindExportResult,
		Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("artifact-review-create-a-%d", suffix),
		Content: map[string]any{"url": "a.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	commandA := ReviewCommand{
		IdempotencyKey: fmt.Sprintf("artifact-review-approve-a-%d", suffix), SessionID: sessionID,
		ArtifactID: first.Revision.ID, ArtifactKind: KindExportResult, ArtifactVersion: first.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	}
	if result, err := store.ApplyReview(ctx, commandA); err != nil || !result.Applied {
		t.Fatalf("first review=%+v err=%v", result, err)
	}
	second, err := store.CreateRevision(ctx, Revision{
		ID: fmt.Sprintf("artifact-review-b-%d", suffix), SessionID: sessionID, Kind: KindExportResult,
		Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("artifact-review-create-b-%d", suffix),
		Content: map[string]any{"url": "b.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyReview(ctx, ReviewCommand{
		IdempotencyKey: fmt.Sprintf("artifact-review-approve-b-%d", suffix), SessionID: sessionID,
		ArtifactID: second.Revision.ID, ArtifactKind: KindExportResult, ArtifactVersion: second.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	}); err != nil {
		t.Fatal(err)
	}
	if replay, err := store.ApplyReview(ctx, commandA); err != nil || replay.Applied {
		t.Fatalf("receipt replay=%+v err=%v", replay, err)
	}
	old, _ := store.Get(ctx, first.Revision.ID)
	latest, _ := store.Get(ctx, second.Revision.ID)
	if old.Status != StatusSuperseded || latest.Status != StatusActive {
		t.Fatalf("receipt replay rolled back latest: old=%s latest=%s", old.Status, latest.Status)
	}
	receipt, err := store.GetReviewReceipt(ctx, commandA.IdempotencyKey)
	if err != nil || receipt.Result.ID != first.Revision.ID || receipt.Result.Status != StatusActive {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	conflict := commandA
	conflict.ArtifactID, conflict.ArtifactVersion = second.Revision.ID, second.Revision.Version
	if _, err := store.ApplyReview(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same-key different request error=%v", err)
	}

	staleFirst, err := store.CreateRevision(ctx, Revision{
		ID: fmt.Sprintf("artifact-review-stale-a-%d", suffix), SessionID: sessionID, Kind: KindExportResult,
		Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("artifact-review-create-stale-a-%d", suffix),
		Content: map[string]any{"url": "stale-a.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	staleLatest, err := store.CreateRevision(ctx, Revision{
		ID: fmt.Sprintf("artifact-review-stale-b-%d", suffix), SessionID: sessionID, Kind: KindExportResult,
		Status: StatusReviewing, IdempotencyKey: fmt.Sprintf("artifact-review-create-stale-b-%d", suffix),
		Content: map[string]any{"url": "stale-b.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	staleKey := fmt.Sprintf("artifact-review-approve-stale-a-%d", suffix)
	_, err = store.ApplyReview(ctx, ReviewCommand{
		IdempotencyKey: staleKey, SessionID: sessionID, ArtifactID: staleFirst.Revision.ID,
		ArtifactKind: KindExportResult, ArtifactVersion: staleFirst.Revision.Version,
		ExpectedStatus: StatusReviewing, Decision: ReviewDecisionApprove, RequireLatest: true,
	})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("stale postgres review error=%v", err)
	}
	staleOld, _ := store.Get(ctx, staleFirst.Revision.ID)
	staleCurrent, _ := store.Get(ctx, staleLatest.Revision.ID)
	if staleOld.Status != StatusReviewing || staleCurrent.Status != StatusReviewing {
		t.Fatalf("stale postgres review mutated state: old=%s latest=%s", staleOld.Status, staleCurrent.Status)
	}
	if _, err := store.GetReviewReceipt(ctx, staleKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale postgres review persisted receipt: %v", err)
	}
}
