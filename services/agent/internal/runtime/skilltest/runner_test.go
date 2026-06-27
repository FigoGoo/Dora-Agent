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
