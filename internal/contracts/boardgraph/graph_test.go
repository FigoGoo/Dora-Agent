package boardgraph

import "testing"

func TestGenericCreationGraphPlanFixture(t *testing.T) {
	var fixture struct {
		GenericCreationGraph GenericCreationGraph `json:"generic_creation_graph"`
		GraphTemplate        GraphTemplate        `json:"graph_template"`
		GraphPlan            GraphPlan            `json:"graph_plan"`
	}
	readFixture(t, "tests/fixtures/contracts/graph/generic_creation_graph_plan.json", &fixture)

	if err := ValidateGenericGraphFixture(fixture.GenericCreationGraph, fixture.GraphTemplate, fixture.GraphPlan); err != nil {
		t.Fatalf("fixture violates generic graph contract: %v", err)
	}
}

func TestInterruptResumeCheckpointFixture(t *testing.T) {
	var fixture struct {
		GraphPlanID string          `json:"graph_plan_id"`
		Checkpoint  GraphCheckpoint `json:"checkpoint"`
		Expected    struct {
			CheckpointStatusAfterResume string `json:"checkpoint_status_after_resume"`
		} `json:"expected"`
	}
	readFixture(t, "tests/fixtures/contracts/graph/interrupt_resume_checkpoint.json", &fixture)

	if fixture.Checkpoint.GraphPlanID != fixture.GraphPlanID {
		t.Fatalf("checkpoint graph_plan_id = %q, fixture graph_plan_id = %q", fixture.Checkpoint.GraphPlanID, fixture.GraphPlanID)
	}
	resumed, err := ResumeCheckpoint(fixture.Checkpoint)
	if err != nil {
		t.Fatalf("resume checkpoint: %v", err)
	}
	if resumed.Status != fixture.Expected.CheckpointStatusAfterResume {
		t.Fatalf("resumed status = %q", resumed.Status)
	}
}

func TestGenericCreationGraphRejectsMarketplaceBinding(t *testing.T) {
	var fixture struct {
		GenericCreationGraph GenericCreationGraph `json:"generic_creation_graph"`
	}
	readFixture(t, "tests/fixtures/contracts/graph/generic_creation_graph_plan.json", &fixture)

	listingID := "listing_001"
	fixture.GenericCreationGraph.MarketplaceListingID = &listingID
	if err := ValidateGenericCreationGraph(fixture.GenericCreationGraph); err == nil {
		t.Fatalf("generic L0 fallback must not bind marketplace_listing_id")
	}
}
