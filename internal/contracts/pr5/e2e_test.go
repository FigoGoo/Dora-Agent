package pr5

import (
	"os"
	"path/filepath"
	"testing"
)

func TestE2ESuiteIndexes(t *testing.T) {
	suites := make([]E2ESuite, 0, 3)
	for _, relativePath := range []string{
		"tests/e2e/agent-workspace/scenarios.json",
		"tests/e2e/skill-marketplace/scenarios.json",
		"tests/e2e/admin-governance/scenarios.json",
	} {
		var suite E2ESuite
		readFixture(t, relativePath, &suite)
		suites = append(suites, suite)
	}
	if err := ValidateE2ESuiteIndexes(suites); err != nil {
		t.Fatalf("e2e suite indexes violate PR-5 contract: %v", err)
	}
}

func TestE2EFixtures(t *testing.T) {
	for relativePath := range RequiredE2EFixtures {
		t.Run(relativePath, func(t *testing.T) {
			var fixture E2EFixture
			readFixture(t, filepath.Join("tests/fixtures/e2e", relativePath), &fixture)
			if err := ValidateE2EFixture(relativePath, fixture); err != nil {
				t.Fatalf("fixture violates PR-5 contract: %v", err)
			}
			for _, reference := range fixture.ContractReferences {
				if _, err := os.Stat(filepath.Join(repoRoot(t), reference.Path)); err != nil {
					t.Fatalf("missing contract reference %s: %v", reference.Path, err)
				}
			}
		})
	}
}

func TestE2EFixtureRequiresFixtureGate(t *testing.T) {
	var fixture E2EFixture
	relativePath := "skill-marketplace/paid_marketplace_skill_usage.json"
	readFixture(t, filepath.Join("tests/fixtures/e2e", relativePath), &fixture)

	fixture.ReleaseGates = []string{"Contract Gate"}
	if err := ValidateE2EFixture(relativePath, fixture); err == nil {
		t.Fatalf("PR-5 fixtures must include Fixture Gate")
	}
}

func TestReleaseGovernanceText(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs/active/technical/release-governance.md"))
	if err != nil {
		t.Fatalf("read release governance: %v", err)
	}
	if err := ValidateReleaseGovernanceText(string(data)); err != nil {
		t.Fatalf("release governance violates PR-5 contract: %v", err)
	}
}
