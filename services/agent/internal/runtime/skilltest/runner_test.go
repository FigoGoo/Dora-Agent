package skilltest

import "testing"

func TestRunnerRequiresThreeCasesSafetyAndElements(t *testing.T) {
	runner := NewRunner()
	if got := runner.Run(Input{Cases: []Case{{CaseID: "one"}}, SafetyResult: "passed"}); got.Status != "rejected" {
		t.Fatalf("expected rejected for less than three cases, got %#v", got)
	}
	cases := []Case{
		{CaseID: "one", ExpectedElements: []string{"image.primary"}},
		{CaseID: "two", ExpectedElements: []string{"text.caption"}},
		{CaseID: "three", ExpectedElements: []string{"metadata.generation"}},
	}
	if got := runner.Run(Input{Cases: cases, SafetyResult: "blocked", ActualElements: []string{"image.primary", "text.caption", "metadata.generation"}}); got.Status != "blocked" {
		t.Fatalf("expected blocked for safety, got %#v", got)
	}
	if got := runner.Run(Input{Cases: cases, SafetyResult: "passed", ActualElements: []string{"image.primary"}}); got.Status != "failed" || len(got.Missing) == 0 {
		t.Fatalf("expected missing output element failure, got %#v", got)
	}
	if got := runner.Run(Input{Cases: cases, SafetyResult: "passed", ActualElements: []string{"image.primary", "text.caption", "metadata.generation"}}); got.Status != "passed" {
		t.Fatalf("expected passed, got %#v", got)
	}
}

func TestRunnerValidatesDictionaryStageAndRenderHints(t *testing.T) {
	runner := NewRunner()
	cases := []Case{
		{CaseID: "one", ExpectedElements: []string{"image.primary"}},
		{CaseID: "two", ExpectedElements: []string{"text.caption"}},
		{CaseID: "three", ExpectedElements: []string{"metadata.generation"}},
	}
	specs := []ElementTypeSpec{
		{ElementType: "image.primary", UsageStage: "draft_final", DraftEnabled: true, FinalEnabled: true, RenderHint: "image"},
		{ElementType: "text.caption", UsageStage: "draft_final", DraftEnabled: true, FinalEnabled: true, RenderHint: "caption"},
		{ElementType: "metadata.generation", UsageStage: "final", DraftEnabled: false, FinalEnabled: true, RenderHint: "metadata"},
	}
	ok := runner.Run(Input{
		Cases: cases, SafetyResult: "passed", Stage: "final", ElementTypes: specs,
		ActualElementDetails: []ActualElement{
			{ElementType: "image.primary", UsageStage: "final", RenderHint: "image"},
			{ElementType: "text.caption", UsageStage: "final", RenderHint: "caption"},
			{ElementType: "metadata.generation", UsageStage: "final", RenderHint: "metadata"},
		},
	})
	if ok.Status != "passed" {
		t.Fatalf("expected dictionary-valid output, got %#v", ok)
	}
	invalid := runner.Run(Input{
		Cases: cases, SafetyResult: "passed", Stage: "final", ElementTypes: specs,
		ActualElementDetails: []ActualElement{
			{ElementType: "image.primary", UsageStage: "final", RenderHint: "image"},
			{ElementType: "text.caption", UsageStage: "final", RenderHint: "caption"},
			{ElementType: "metadata.generation", UsageStage: "final", RenderHint: "metadata"},
			{ElementType: "unknown.type", UsageStage: "final", RenderHint: "raw"},
		},
	})
	if invalid.Status != "failed" || invalid.Reason != "invalid_element_types" || len(invalid.InvalidTypes) == 0 {
		t.Fatalf("expected invalid type failure, got %#v", invalid)
	}
	stage := runner.Run(Input{
		Cases: cases, SafetyResult: "passed", Stage: "draft", ElementTypes: specs,
		ActualElementDetails: []ActualElement{
			{ElementType: "image.primary", UsageStage: "draft", RenderHint: "image"},
			{ElementType: "text.caption", UsageStage: "draft", RenderHint: "caption"},
			{ElementType: "metadata.generation", UsageStage: "draft", RenderHint: "metadata"},
		},
	})
	if stage.Status != "failed" || stage.Reason != "stage_violations" || len(stage.StageViolations) == 0 {
		t.Fatalf("expected stage violation, got %#v", stage)
	}
	render := runner.Run(Input{
		Cases: cases, SafetyResult: "passed", Stage: "final", ElementTypes: specs,
		ActualElementDetails: []ActualElement{
			{ElementType: "image.primary", UsageStage: "final", RenderHint: "image"},
			{ElementType: "text.caption", UsageStage: "final", RenderHint: "wrong"},
			{ElementType: "metadata.generation", UsageStage: "final", RenderHint: "metadata"},
		},
	})
	if render.Status != "failed" || render.Reason != "unrenderable_hints" || len(render.UnrenderableHints) == 0 {
		t.Fatalf("expected render hint violation, got %#v", render)
	}
}
