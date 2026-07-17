package writepromptsruntime

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
)

func TestApprovedArtifactsExactMatch(t *testing.T) {
	t.Logf("tool=%s prompt=%s validator=%s exact=%s", writeprompts.ToolDefinitionDigest(), writeprompts.PromptArtifactDigest(), writeprompts.ValidatorArtifactDigest(), writeprompts.ExactSetValidatorArtifactDigest())
	if err := ValidateApprovedArtifacts(); err != nil {
		t.Fatal(err)
	}
}
