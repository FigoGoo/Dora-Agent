package generation

import "testing"

func TestBindingTokenTargetKindValidation(t *testing.T) {
	base := BindingToken{TargetID: "deliverable:img-1", AssetSlot: "primary", InputFingerprint: "fp"}

	deliverable := base
	deliverable.TargetKind = TargetKindSessionDeliverable
	if err := deliverable.Validate(); err != nil {
		t.Fatalf("session deliverable token must not require storyboard id: %v", err)
	}

	legacyEmptyKind := base // TargetKind 为空 = 默认 storyboard_slot，仍要求 StoryboardID
	if err := legacyEmptyKind.Validate(); err == nil {
		t.Fatal("legacy empty-kind token must still require storyboard id")
	}

	slot := base
	slot.TargetKind = TargetKindStoryboardSlot
	if err := slot.Validate(); err == nil {
		t.Fatal("storyboard_slot token must require storyboard id")
	}

	unknown := base
	unknown.TargetKind = "weird"
	if err := unknown.Validate(); err == nil {
		t.Fatal("unknown target kind must be rejected")
	}
}

func TestBindingTokenEqualNormalizesKind(t *testing.T) {
	a := BindingToken{StoryboardID: "sb", TargetID: "e1", AssetSlot: "s", InputFingerprint: "fp"}
	b := a
	b.TargetKind = TargetKindStoryboardSlot
	if !a.Equal(b) {
		t.Fatal("empty kind and explicit storyboard_slot must compare equal")
	}
	c := a
	c.TargetKind = TargetKindSessionDeliverable
	if a.Equal(c) {
		t.Fatal("different target kinds must not compare equal")
	}
}
