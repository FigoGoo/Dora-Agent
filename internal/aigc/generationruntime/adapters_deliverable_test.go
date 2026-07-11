package generationruntime

import (
	"context"
	"testing"

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

	committed, err := adapter.IsCommitted(context.Background(), job, []string{"asset-1"})
	if err != nil || !committed {
		t.Fatalf("IsCommitted must be true after deliverable commit: %v %v", committed, err)
	}

	// outbox 对 finalized job 调 PublishFinalized：deliverable 无 storyboard
	// 可投影，必须短路成功（Events 非 nil 且必失败 + Repository nil，
	// 不短路则报错或 panic）。
	if err := adapter.PublishFinalized(context.Background(), job); err != nil {
		t.Fatalf("PublishFinalized must skip storyboard projection for deliverable: %v", err)
	}
}
