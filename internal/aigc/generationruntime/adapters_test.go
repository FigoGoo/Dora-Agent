package generationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type memoryAssets struct{ values map[string]asset.Asset }

type sourceAssets struct{ *memoryAssets }

func (s sourceAssets) ListBySourceJob(context.Context, string) ([]asset.Asset, error) {
	return []asset.Asset{
		{ID: "asset-0", OutputIndex: 0, Availability: asset.AvailabilityPendingBilling},
		{ID: "asset-1", OutputIndex: 1, Availability: asset.AvailabilityPendingBilling},
	}, nil
}

type fixedConfirmedSpec struct{ version int }

type failingEventPublisher struct{}

type captureEventPublisher struct{ events []a2ui.SSEEvent }

func TestDefaultCostCalculatorDistinguishesReportedZeroUsage(t *testing.T) {
	calculator := DefaultCostCalculator{Points: map[string]int64{"image": 99}}
	job := generation.GenerationJob{MediaKind: "image"}
	points, _, err := calculator.Calculate(context.Background(), job, generation.ProviderResult{UsageReported: true, ActualPoints: 0})
	if err != nil || points != 0 {
		t.Fatalf("reported zero usage points=%d err=%v", points, err)
	}
	points, _, err = calculator.Calculate(context.Background(), job, generation.ProviderResult{})
	if err != nil || points != 99 {
		t.Fatalf("unreported usage fallback points=%d err=%v", points, err)
	}
}

func TestPendingAssetRecoveryReadsPersistedJSONNumberCount(t *testing.T) {
	store := PendingAssetStore{AssetStore: sourceAssets{memoryAssets: &memoryAssets{values: map[string]asset.Asset{}}}}
	result, complete, err := store.RecoverProviderResult(context.Background(), generation.GenerationJob{
		ID: "job-1", MediaKind: "image", Payload: map[string]any{"n": json.Number("2")},
	})
	if err != nil || !complete || len(result.AssetIDs) != 2 {
		t.Fatalf("result=%+v complete=%t err=%v", result, complete, err)
	}
}

func (failingEventPublisher) Publish(context.Context, a2ui.SSEEvent) error {
	return errors.New("event log unavailable")
}

func (p *captureEventPublisher) Publish(_ context.Context, event a2ui.SSEEvent) error {
	p.events = append(p.events, event)
	return nil
}

func (s fixedConfirmedSpec) GetConfirmedBySession(context.Context, string) (spec.FinalVideoSpec, error) {
	return spec.FinalVideoSpec{Version: s.version, Status: spec.StatusConfirmed}, nil
}

func (s *memoryAssets) Get(_ context.Context, id string) (asset.Asset, error) {
	value, ok := s.values[id]
	if !ok {
		return asset.Asset{}, fmt.Errorf("missing asset")
	}
	return value, nil
}
func (s *memoryAssets) Save(_ context.Context, value asset.Asset) (asset.Asset, error) {
	s.values[value.ID] = value
	return value, nil
}

func TestBindingAdapterMakesAssetAvailableAndCreatesCandidateApproval(t *testing.T) {
	ctx := context.Background()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := commands.Create(ctx, "board-1", "session-1")
	candidate := storyboard.StoryboardRevision{ID: "revision-1", DerivedFromSpecVersion: 1, Modules: []storyboard.StoryboardModule{{ID: "module-1", Key: "shots", SemanticType: "shot", Title: "镜头", PlannedCount: 1, Elements: []storyboard.StoryboardElement{{ID: "shot-1", Key: "shot-1", SemanticType: "shot", Title: "镜头 1", Revision: 1, PromptSlots: []storyboard.PromptSlot{{Purpose: "keyframe", Prompt: "prompt", Revision: 1, Status: storyboard.PromptStatusReady}}, AssetSlots: []storyboard.AssetSlot{{Key: "keyframe", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}}}}}}}
	aggregate, _, err := commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "promote", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	assets := &memoryAssets{values: map[string]asset.Asset{"asset-1": {ID: "asset-1", SessionID: "session-1", Availability: asset.AvailabilityPendingBilling}}}
	approvals := approval.NewMemoryStore()
	adapter := StoryboardBindingAdapter{Repository: repository, Commands: commands, Assets: assets, Approvals: approvals, Specs: fixedConfirmedSpec{version: 1}, Events: failingEventPublisher{}}
	currentToken, ok := currentBindingToken(aggregate, "shot_1", "keyframe")
	if !ok {
		t.Fatal("current binding token not found")
	}
	token := currentToken
	check, err := (BindingGuard{StoryboardBindingAdapter: adapter}).Check(ctx, token)
	if err != nil || !check.TargetExists || !check.Matches {
		t.Fatalf("check=%+v err=%v", check, err)
	}
	staleSpecAdapter := adapter
	staleSpecAdapter.Specs = fixedConfirmedSpec{version: 2}
	if staleCheck, staleErr := (BindingGuard{StoryboardBindingAdapter: staleSpecAdapter}).Check(ctx, token); staleErr != nil || staleCheck.Matches {
		t.Fatalf("spec fence check=%+v err=%v", staleCheck, staleErr)
	}
	assemblyToken := generation.BindingToken{StoryboardID: aggregate.ID, TargetID: "assembly:plan-1", AssetSlot: "video", TargetRevision: 1, SpecVersion: 1, AggregateVersion: aggregate.Version, InputFingerprint: "manifest-fingerprint"}
	if assemblyCheck, assemblyErr := adapter.Check(ctx, assemblyToken); assemblyErr != nil || !assemblyCheck.Matches {
		t.Fatalf("assembly check=%+v err=%v", assemblyCheck, assemblyErr)
	}
	err = adapter.Commit(ctx, generation.FinalizationCommit{Job: generation.GenerationJob{ID: "job-1", SessionID: "session-1", UserID: "user-1"}, AssetIDs: []string{"asset-1"}, BindingToken: token, BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired})
	if err != nil {
		t.Fatalf("domain commit must not fail with its projection: %v", err)
	}
	storedAsset, _ := assets.Get(ctx, "asset-1")
	if storedAsset.Availability != asset.AvailabilityAvailable {
		t.Fatalf("availability=%s", storedAsset.Availability)
	}
	updated, _ := repository.GetAggregate(ctx, aggregate.ID)
	if len(updated.Bindings) != 1 || updated.Bindings[0].State != storyboard.BindingStateCandidate {
		t.Fatalf("bindings=%+v", updated.Bindings)
	}
	review, err := approvals.Get(ctx, "approval:binding:job-1:0")
	if err != nil || review.ArtifactType != "candidate_asset" || review.ReviewMode != approval.ReviewModeDurable {
		t.Fatalf("approval=%+v err=%v", review, err)
	}
	projectedJob := generation.GenerationJob{ID: "job-1", SessionID: "session-1", UserID: "user-1", ResultAssetIDs: []string{"asset-1"}, BindingToken: token, DeliveryPolicy: generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired}}
	if err := adapter.PublishFinalized(ctx, projectedJob); err == nil {
		t.Fatal("durable projection should surface the event publication error for outbox retry")
	}
	capture := &captureEventPublisher{}
	adapter.Events = capture
	if err := adapter.PublishFinalized(ctx, projectedJob); err != nil {
		t.Fatal(err)
	}
	if len(capture.events) != 1 {
		t.Fatalf("candidate finalization projected %d events, want storyboard only", len(capture.events))
	}
	envelope, ok := capture.events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 || envelope.Actions[0].Surface != "storyboard" || envelope.Actions[0].Type == a2ui.ActionAppendCard {
		t.Fatalf("candidate finalization event = %#v", capture.events[0])
	}
}
