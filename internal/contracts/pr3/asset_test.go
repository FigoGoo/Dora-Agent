package pr3

import "testing"

func TestPartialAssetCommitSuccessFixture(t *testing.T) {
	var fixture struct {
		ToolResult     ToolResult          `json:"tool_result"`
		CommitResponse AssetCommitResponse `json:"commit_response"`
		BillingRule    AssetBillingRule    `json:"billing_rule"`
	}
	readFixture(t, "tests/fixtures/contracts/asset/partial_commit_success.json", &fixture)

	if err := ValidatePartialAssetCommit(fixture.ToolResult, fixture.CommitResponse, fixture.BillingRule); err != nil {
		t.Fatalf("fixture violates partial asset commit contract: %v", err)
	}
}

func TestPartialAssetCommitRejectsChargingFailedAssets(t *testing.T) {
	var fixture struct {
		ToolResult     ToolResult          `json:"tool_result"`
		CommitResponse AssetCommitResponse `json:"commit_response"`
		BillingRule    AssetBillingRule    `json:"billing_rule"`
	}
	readFixture(t, "tests/fixtures/contracts/asset/partial_commit_success.json", &fixture)

	fixture.BillingRule.FailedAssetsMustNotBeCharged = false
	if err := ValidatePartialAssetCommit(fixture.ToolResult, fixture.CommitResponse, fixture.BillingRule); err == nil {
		t.Fatalf("failed assets must not be charged")
	}
}
