package planstoryboardruntime

import "testing"

func TestApprovedPinsAreIndependentExactArtifactAnchors(t *testing.T) {
	if err := ValidateApprovedArtifacts(); err != nil {
		t.Fatal(err)
	}
	pins := ApprovedPins()
	if pins.ToolDefinitionDigest != approvedToolDefinitionDigest || pins.PromptDigest != approvedPromptDigest ||
		pins.ValidatorDigest != approvedValidatorDigest || pins.DAGValidatorDigest != approvedDAGValidatorDigest {
		t.Fatalf("approved pins 未使用独立审批锚点: %+v", pins)
	}
}
