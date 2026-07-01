package skillmarket

import "testing"

func TestCreatorDataVisibilityFixture(t *testing.T) {
	var fixture DataVisibilityFixture
	readFixture(t, "tests/fixtures/contracts/marketplace/data_visibility_creator_safe.json", &fixture)

	if err := ValidateCreatorDataVisibility(fixture); err != nil {
		t.Fatalf("fixture violates creator data visibility contract: %v", err)
	}
}

func TestCreatorDataVisibilityRejectsPrivateLeak(t *testing.T) {
	var fixture DataVisibilityFixture
	readFixture(t, "tests/fixtures/contracts/marketplace/data_visibility_creator_safe.json", &fixture)

	fixture.CreatorAPIResponse["user_prompt"] = "private"
	if err := ValidateCreatorDataVisibility(fixture); err == nil {
		t.Fatalf("creator API must not leak user private fields")
	}
}
