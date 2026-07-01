package pr3

import "testing"

func TestCityVideoToolPlanFixture(t *testing.T) {
	var fixture struct {
		Precondition     ToolPlanPrecondition            `json:"precondition"`
		ToolPlan         ToolPlan                        `json:"tool_plan"`
		AGUIEventPayload GenerationCostDisclosurePayload `json:"agui_event_payload"`
	}
	readFixture(t, "tests/fixtures/contracts/toolplan/city_video_toolplan.json", &fixture)

	if err := ValidateToolPlanForApprovedBoard(fixture.Precondition, fixture.ToolPlan); err != nil {
		t.Fatalf("fixture violates ToolPlan contract: %v", err)
	}
	if err := ValidateGenerationCostDisclosurePayload(fixture.AGUIEventPayload); err != nil {
		t.Fatalf("fixture violates generation disclosure contract: %v", err)
	}
	if fixture.AGUIEventPayload.ToolPlanDigest != fixture.ToolPlan.ToolPlanDigest {
		t.Fatalf("AG-UI tool_plan_digest must match ToolPlan")
	}
}

func TestToolPlanRejectsUnapprovedBoard(t *testing.T) {
	var fixture struct {
		Precondition ToolPlanPrecondition `json:"precondition"`
		ToolPlan     ToolPlan             `json:"tool_plan"`
	}
	readFixture(t, "tests/fixtures/contracts/toolplan/city_video_toolplan.json", &fixture)

	fixture.Precondition.BoardStatus = "ready"
	if err := ValidateToolPlanForApprovedBoard(fixture.Precondition, fixture.ToolPlan); err == nil {
		t.Fatalf("ToolPlan must require approved board")
	}
}

func TestProviderAsyncResumeFixture(t *testing.T) {
	var fixture struct {
		ToolTaskBeforeRestart ToolTask                     `json:"tool_task_before_restart"`
		RedisStreamEvent      ToolTaskCompletedStreamEvent `json:"redis_stream_event"`
		ToolTaskAfterResume   ToolTask                     `json:"tool_task_after_resume"`
	}
	readFixture(t, "tests/fixtures/contracts/tool/provider_async_resume.json", &fixture)

	if err := ValidateProviderAsyncResume(fixture.ToolTaskBeforeRestart, fixture.RedisStreamEvent, fixture.ToolTaskAfterResume); err != nil {
		t.Fatalf("fixture violates provider async resume contract: %v", err)
	}
}
