package capability

import "testing"

func TestGenerateMediaIntentTargetDispatch(t *testing.T) {
	legacy := GenerateMediaIntent{Phase: "auto_next", Policy: "all_eligible"}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy storyboard intent must stay valid: %v", err)
	}
	if legacy.NormalizedTarget() != MediaTargetStoryboard {
		t.Fatalf("empty target must normalize to storyboard, got %q", legacy.NormalizedTarget())
	}

	deliverable := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image", Prompt: "一只在雨里撑伞的柴犬"}
	if err := deliverable.Validate(); err != nil {
		t.Fatalf("deliverable intent must not require phase/policy: %v", err)
	}
	if deliverable.NormalizedCount() != 1 {
		t.Fatalf("count defaults to 1, got %d", deliverable.NormalizedCount())
	}

	for _, kind := range []string{"image", "video", "music", "audio"} {
		intent := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: kind, Prompt: "p"}
		if err := intent.Validate(); err != nil {
			t.Fatalf("media_kind %s must be valid: %v", kind, err)
		}
	}

	badKind := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "hologram", Prompt: "p"}
	if err := badKind.Validate(); err == nil {
		t.Fatal("unknown media_kind must be rejected")
	}
	noPrompt := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image"}
	if err := noPrompt.Validate(); err == nil {
		t.Fatal("deliverable intent requires prompt")
	}
	tooMany := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image", Prompt: "p", Count: 5}
	if err := tooMany.Validate(); err == nil {
		t.Fatal("count above 4 must be rejected")
	}
	unknownTarget := GenerateMediaIntent{Target: "weird", MediaKind: "image", Prompt: "p"}
	if err := unknownTarget.Validate(); err == nil {
		t.Fatal("unknown target must be rejected")
	}
}
