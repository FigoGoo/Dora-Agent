package pr5

import (
	"reflect"
	"testing"
)

func TestFakeProviderArtifacts(t *testing.T) {
	var manifest FakeProviderManifest
	var scenarios ProviderScenarios
	readFixture(t, "tests/e2e/fake-provider/fake_provider_manifest.json", &manifest)
	readFixture(t, "tests/e2e/fake-provider/provider_scenarios.json", &scenarios)

	if err := ValidateFakeProviderArtifacts(manifest, scenarios); err != nil {
		t.Fatalf("fake provider artifacts violate PR-5 contract: %v", err)
	}
}

func TestFakeProviderRequiresAllBehaviors(t *testing.T) {
	var manifest FakeProviderManifest
	var scenarios ProviderScenarios
	readFixture(t, "tests/e2e/fake-provider/fake_provider_manifest.json", &manifest)
	readFixture(t, "tests/e2e/fake-provider/provider_scenarios.json", &scenarios)

	manifest.BehaviorContracts = manifest.BehaviorContracts[:len(manifest.BehaviorContracts)-1]
	if err := ValidateFakeProviderArtifacts(manifest, scenarios); err == nil {
		t.Fatalf("fake provider gate must require all PR-5 behaviors")
	}
}

func TestFakeProviderScenariosAreExecutableAndIdempotent(t *testing.T) {
	var scenarios ProviderScenarios
	readFixture(t, "tests/e2e/fake-provider/provider_scenarios.json", &scenarios)

	for _, scenario := range scenarios.Scenarios {
		t.Run(scenario.CaseID, func(t *testing.T) {
			first, err := SimulateFakeProviderScenario(scenario)
			if err != nil {
				t.Fatalf("simulate first execution: %v", err)
			}
			second, err := SimulateFakeProviderScenario(scenario)
			if err != nil {
				t.Fatalf("simulate replay execution: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("fake provider replay must be idempotent\nfirst:  %#v\nsecond: %#v", first, second)
			}
			if first.ProviderID != scenario.ProviderID || first.BehaviorID != scenario.BehaviorID {
				t.Fatalf("execution identity mismatch: %#v", first)
			}
		})
	}
}
