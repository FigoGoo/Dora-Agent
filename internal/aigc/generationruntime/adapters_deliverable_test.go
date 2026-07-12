package generationruntime

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

func TestBindingCheckSessionDeliverableSkipsStoryboard(t *testing.T) {
	// Repository 故意为空：deliverable 路径若触碰 storyboard 会直接报
	// "storyboard repository is required"，本测试是物理隔离证明。
	guard := BindingGuard{StoryboardBindingAdapter{}}
	token := generation.BindingToken{
		TargetKind:       generation.TargetKindSessionDeliverable,
		TargetID:         "deliverable:img-1",
		AssetSlot:        "primary",
		InputFingerprint: "fp",
	}
	check, err := guard.Check(context.Background(), token)
	if err != nil {
		t.Fatalf("deliverable check must not touch storyboard store: %v", err)
	}
	if !check.TargetExists || !check.Matches {
		t.Fatalf("deliverable target must always exist and match, got %+v", check)
	}
}

func TestCommitSessionDeliverableOnlyPublishesAssets(t *testing.T) {
	assets := &memoryAssets{values: map[string]asset.Asset{
		"asset-1": {ID: "asset-1", SessionID: "sess-1", Availability: asset.AvailabilityPendingBilling},
	}}
	// Repository/Commands/Approvals 全部为 nil：deliverable 落库若触碰
	// 任何 storyboard/审批依赖会直接报错或 panic，本测试是物理隔离证明。
	adapter := StoryboardBindingAdapter{Assets: assets, Events: failingEventPublisher{}}
	job := generation.GenerationJob{
		ID: "job-1", SessionID: "sess-1",
		DeliveryPolicy: generation.DeliveryPolicy{
			BindingMode:    generation.BindingModeActive,
			ApprovalPolicy: generation.ApprovalAutoApprove,
			ChargePolicy:   generation.ChargePostpaidNoReservation,
		},
		BindingToken: generation.BindingToken{
			TargetKind:       generation.TargetKindSessionDeliverable,
			TargetID:         "deliverable:img-1",
			AssetSlot:        "primary",
			InputFingerprint: "fp",
		},
		ResultAssetIDs: []string{"asset-1"},
	}

	err := adapter.Commit(context.Background(), generation.FinalizationCommit{
		Job: job, AssetIDs: []string{"asset-1"},
		BindingToken:   job.BindingToken,
		BindingMode:    generation.BindingModeActive,
		ApprovalPolicy: generation.ApprovalAutoApprove,
	})
	if err != nil {
		t.Fatalf("deliverable commit must succeed without storyboard/approval stores: %v", err)
	}

	stored, err := assets.Get(context.Background(), "asset-1")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if stored.Availability != asset.AvailabilityAvailable {
		t.Fatalf("asset must be available after commit, got %s", stored.Availability)
	}
	if stored.Metadata["target_kind"] != generation.TargetKindSessionDeliverable {
		t.Fatalf("deliverable asset must be tagged, metadata=%v", stored.Metadata)
	}

	committed, err := adapter.IsCommitted(context.Background(), job, []string{"asset-1"})
	if err != nil || !committed {
		t.Fatalf("IsCommitted must be true after deliverable commit: %v %v", committed, err)
	}

	// outbox 对 finalized job 调 PublishFinalized：deliverable 投影为
	// deliverables surface 事件；发布失败必须传播（outbox 重试语义），
	// 本测试的 Events 必失败，因此这里断言报错。仍不得触碰 storyboard
	// （Repository nil，触碰即 panic）。
	if err := adapter.PublishFinalized(context.Background(), job); err == nil {
		t.Fatal("PublishFinalized must propagate publish failures for outbox retry")
	}
}

type recordingEventPublisher struct{ events []a2ui.SSEEvent }

func (p *recordingEventPublisher) Publish(_ context.Context, event a2ui.SSEEvent) error {
	p.events = append(p.events, event)
	return nil
}

func TestPublishFinalizedEmitsDeliverablesSurface(t *testing.T) {
	assets := &memoryAssets{values: map[string]asset.Asset{
		"asset-1": {ID: "asset-1", SessionID: "sess-1", Availability: asset.AvailabilityAvailable, Kind: "image", MIMEType: "image/png", URL: "https://cdn/x.png"},
	}}
	publisher := &recordingEventPublisher{}
	adapter := StoryboardBindingAdapter{Assets: assets, Events: publisher}
	job := generation.GenerationJob{
		ID: "job-1", SessionID: "sess-1", ResultAssetIDs: []string{"asset-1"},
		BindingToken: generation.BindingToken{TargetKind: generation.TargetKindSessionDeliverable, TargetID: "deliverable:img-1", AssetSlot: "primary", InputFingerprint: "fp"},
	}
	if err := adapter.PublishFinalized(context.Background(), job); err != nil {
		t.Fatalf("PublishFinalized: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected one deliverables event, got %d", len(publisher.events))
	}
	if publisher.events[0].SessionID != "sess-1" {
		t.Fatalf("event session = %s", publisher.events[0].SessionID)
	}
	envelope, ok := publisher.events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 {
		t.Fatalf("payload = %#v", publisher.events[0].Payload)
	}
	action := envelope.Actions[0]
	if action.Type != a2ui.ActionUpdateCard || action.Surface != "deliverables" || action.Target == nil || action.Target.CardID != "deliverables" {
		t.Fatalf("action = %#v", action)
	}
	payload, ok := action.Payload.(map[string]any)
	if !ok {
		t.Fatalf("action payload = %#v", action.Payload)
	}
	views, ok := payload["assets"].([]map[string]any)
	if !ok || len(views) != 1 {
		t.Fatalf("assets view = %#v", payload["assets"])
	}
	view := views[0]
	if view["id"] != "asset-1" || view["url"] != "https://cdn/x.png" || view["kind"] != "image" || view["mime_type"] != "image/png" || view["target_id"] != "deliverable:img-1" {
		t.Fatalf("view = %#v", view)
	}
}

func TestFinalizeSessionDeliverableEndToEnd(t *testing.T) {
	ctx := context.Background()
	store := generation.NewMemoryStore()
	assets := &memoryAssets{values: map[string]asset.Asset{
		"asset-1": {ID: "asset-1", SessionID: "session-1", Availability: asset.AvailabilityPendingBilling},
	}}
	// Repository/Approvals/Specs 全部为 nil：Finalize 全链任何一步触碰
	// storyboard/审批即 panic 或报错——物理隔离证明。
	adapter := StoryboardBindingAdapter{Assets: assets, Events: failingEventPublisher{}}
	policy := generation.DeliveryPolicy{
		BindingMode:    generation.BindingModeActive,
		ApprovalPolicy: generation.ApprovalAutoApprove,
		ChargePolicy:   generation.ChargePostpaidNoReservation,
	}
	token := generation.BindingToken{
		TargetKind:       generation.TargetKindSessionDeliverable,
		TargetID:         "deliverable:img-1",
		AssetSlot:        "primary",
		InputFingerprint: "fp",
	}
	_, _, err := store.CreateWorkflow(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: "op-1", SessionID: "session-1", UserID: "user-1", StageRunID: "stage-1", ToolCallID: "tool-1", IdempotencyKey: "op-key-1"},
		Batch:     generation.GenerationBatch{ID: "batch-1", CompletionPolicy: generation.CompletionAllRequired, WakePolicy: generation.WakeOnTerminal, DeliveryPolicy: policy},
		Jobs: []generation.GenerationJob{{
			ID: "job-1", IdempotencyKey: "job-key-1", Provider: "mock", Required: true,
			BindingToken: token, DeliveryPolicy: policy,
		}},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow() error = %v", err)
	}
	queued, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if _, err := store.MutateJob(ctx, "job-1", queued.StatusVersion, func(current *generation.GenerationJob) ([]generation.OutboxEvent, error) {
		current.Status = generation.StatusRunning
		return nil, nil
	}); err != nil {
		t.Fatalf("advance to running: %v", err)
	}

	engine := generation.NewFinalizationEngine(generation.FinalizationEngineConfig{
		Store:     store,
		Bindings:  BindingGuard{adapter},
		Committer: adapter,
		Inspector: adapter,
	})
	job, err := engine.Finalize(ctx, "job-1", generation.ProviderResult{AssetIDs: []string{"asset-1"}, Status: "done"})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if job.Status != generation.StatusSucceeded {
		t.Fatalf("job status = %s, want %s (job=%#v)", job.Status, generation.StatusSucceeded, job)
	}
	if job.ResultDisposition != generation.DispositionBoundActive {
		t.Fatalf("disposition = %s, want %s", job.ResultDisposition, generation.DispositionBoundActive)
	}
	stored, err := assets.Get(ctx, "asset-1")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if stored.Availability != asset.AvailabilityAvailable {
		t.Fatalf("asset availability = %s, want available", stored.Availability)
	}
}
