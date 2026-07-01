package foundation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRouterDecisionFixturesValidate(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(repoRoot(t), "tests/fixtures/contracts/router/*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("expected router fixtures")
	}
	for _, path := range fixtures {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var fixture struct {
				Expected struct {
					RouterDecision RouterDecision `json:"router_decision"`
				} `json:"expected"`
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if err := ValidateRouterDecision(fixture.Expected.RouterDecision); err != nil {
				t.Fatalf("fixture violates RouterDecision contract: %v", err)
			}
		})
	}
}

func TestRouterDecisionMustNotBeSafeToExecute(t *testing.T) {
	decision := RouterDecision{
		SchemaVersion:                  SchemaVersionRouterDecision,
		Decision:                       RouterDecisionSelectSkill,
		Confidence:                     0.9,
		ReasonCode:                     "matched",
		SafeToExecute:                  true,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                map[string]any{},
		MissingFields:                  []string{},
		CandidateSkills:                []CandidateSkill{},
		MarketplaceCandidates:          []MarketplaceCandidate{},
	}
	if err := ValidateRouterDecision(decision); err == nil {
		t.Fatalf("safe_to_execute=true must be rejected")
	}
}
