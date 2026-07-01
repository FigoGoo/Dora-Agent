package toolasset

import "testing"

func TestCreditFreezeCommitSuccessFixture(t *testing.T) {
	var fixture struct {
		FreezeRequest   FreezeCreditsRequest `json:"freeze_request"`
		HoldAfterFreeze CreditFreeze         `json:"hold_after_freeze"`
		HoldAfterCommit CreditFreeze         `json:"hold_after_commit"`
	}
	readFixture(t, "tests/fixtures/contracts/credit/freeze_commit_success.json", &fixture)

	if err := ValidateFreezeCommitFlow(fixture.FreezeRequest, fixture.HoldAfterFreeze, fixture.HoldAfterCommit); err != nil {
		t.Fatalf("fixture violates freeze commit contract: %v", err)
	}
}

func TestCreditFreezeReleaseOnToolFailureFixture(t *testing.T) {
	var fixture struct {
		HoldAfterFreeze  CreditFreeze `json:"hold_after_freeze"`
		ToolTaskFailure  ToolTask     `json:"tool_task_failure"`
		HoldAfterRelease CreditFreeze `json:"hold_after_release"`
	}
	readFixture(t, "tests/fixtures/contracts/credit/freeze_release_on_tool_failure.json", &fixture)

	if err := ValidateFreezeReleaseOnFailure(fixture.HoldAfterFreeze, fixture.ToolTaskFailure, fixture.HoldAfterRelease); err != nil {
		t.Fatalf("fixture violates freeze release contract: %v", err)
	}
}

func TestCreditFreezeRejectsDoubleSpend(t *testing.T) {
	var fixture struct {
		HoldAfterCommit CreditFreeze `json:"hold_after_commit"`
	}
	readFixture(t, "tests/fixtures/contracts/credit/freeze_commit_success.json", &fixture)

	fixture.HoldAfterCommit.ReleasedCredits = 1
	if err := ValidateCreditFreeze(fixture.HoldAfterCommit); err == nil {
		t.Fatalf("credit hold must not allow committed and released credits above frozen amount")
	}
}
