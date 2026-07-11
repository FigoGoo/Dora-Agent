package generationruntime

import (
	"context"
	"testing"

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
