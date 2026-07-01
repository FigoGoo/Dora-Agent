package pr5

import (
	"errors"
	"fmt"
	"strings"
)

var RequiredReleaseGates = []string{
	"Contract Gate",
	"Migration Gate",
	"Fixture Gate",
	"Fake Provider Gate",
	"Feature Flag Gate",
	"Observability Gate",
	"Rollback Gate",
}

var RequiredFeatureFlags = []string{
	"agent_runtime_v2",
	"tool_generation_v2",
	"marketplace_v2",
}

var RequiredMetrics = []string{
	"agent_run_success_rate",
	"router_decision_latency_ms",
	"board_patch_replay_error_count",
	"graph_resume_failure_count",
	"tool_task_success_rate",
	"credit_freeze_leak_count",
	"skill_usage_charge_error_count",
	"marketplace_install_failure_count",
	"settlement_reverse_count",
}

var RequiredE2EFixtures = map[string]RequiredE2EFixture{
	"agent-workspace/city_tourism_default_skill.json": {
		CaseID:      "city_tourism_default_skill_e2e_v1",
		DependsOnPR: []string{"PR-1", "PR-2", "PR-3"},
	},
	"agent-workspace/generic_creation_graph_fallback.json": {
		CaseID:      "generic_creation_graph_fallback_e2e_v1",
		DependsOnPR: []string{"PR-1", "PR-2"},
	},
	"skill-marketplace/paid_marketplace_skill_usage.json": {
		CaseID:      "paid_marketplace_skill_usage_e2e_v1",
		DependsOnPR: []string{"PR-1", "PR-2", "PR-3", "PR-4"},
	},
	"skill-marketplace/enterprise_pinned_install_upgrade.json": {
		CaseID:      "enterprise_pinned_install_upgrade_e2e_v1",
		DependsOnPR: []string{"PR-4"},
	},
	"agent-workspace/tool_partial_failure_release.json": {
		CaseID:      "tool_partial_failure_release_e2e_v1",
		DependsOnPR: []string{"PR-2", "PR-3"},
	},
	"skill-marketplace/listing_suspended_guard.json": {
		CaseID:      "listing_suspended_guard_e2e_v1",
		DependsOnPR: []string{"PR-4"},
	},
	"admin-governance/refund_settlement_reverse.json": {
		CaseID:      "refund_settlement_reverse_e2e_v1",
		DependsOnPR: []string{"PR-4"},
	},
	"agent-workspace/replay_after_restart.json": {
		CaseID:      "replay_after_restart_e2e_v1",
		DependsOnPR: []string{"PR-1", "PR-2", "PR-3"},
	},
}

type RequiredE2EFixture struct {
	CaseID      string
	DependsOnPR []string
}

type E2ESuite struct {
	SchemaVersion string            `json:"schema_version"`
	SuiteID       string            `json:"suite_id"`
	Status        string            `json:"status"`
	Owner         string            `json:"owner"`
	UpdatedAt     string            `json:"updated_at"`
	Fixtures      []E2ESuiteFixture `json:"fixtures"`
	RequiredGates []string          `json:"required_gates"`
}

type E2ESuiteFixture struct {
	CaseID               string `json:"case_id"`
	FixturePath          string `json:"fixture_path"`
	Required             bool   `json:"required"`
	FakeProviderBehavior string `json:"fake_provider_behavior"`
}

type E2EFixture struct {
	CaseID                 string              `json:"case_id"`
	Status                 string              `json:"status"`
	Owner                  string              `json:"owner"`
	DependsOnPR            []string            `json:"depends_on_pr"`
	ContractReferences     []ContractReference `json:"contract_references"`
	FeatureFlags           map[string]bool     `json:"feature_flags"`
	Preconditions          []string            `json:"preconditions"`
	UserJourney            []string            `json:"user_journey"`
	ExpectedAGUIEventOrder []string            `json:"expected_agui_event_order"`
	ExpectedBusinessState  map[string]any      `json:"expected_business_state"`
	FakeProvider           E2EFakeProvider     `json:"fake_provider"`
	RedisExpectations      []string            `json:"redis_expectations"`
	ReleaseGates           []string            `json:"release_gates"`
}

type ContractReference struct {
	Path     string `json:"path"`
	Required bool   `json:"required"`
}

type E2EFakeProvider struct {
	ProviderID string `json:"provider_id"`
	BehaviorID string `json:"behavior_id"`
}

func ValidateE2ESuiteIndexes(suites []E2ESuite) error {
	if len(suites) == 0 {
		return errors.New("e2e suites are required")
	}
	indexed := make(map[string]struct{})
	for suiteIndex, suite := range suites {
		if suite.SchemaVersion != "e2e_suite.v1" || suite.Status != "active" {
			return fmt.Errorf("suite %d must be active e2e_suite.v1", suiteIndex+1)
		}
		if !contains(suite.RequiredGates, "Fixture Gate") {
			return fmt.Errorf("suite %q must include Fixture Gate", suite.SuiteID)
		}
		for fixtureIndex, fixture := range suite.Fixtures {
			if fixture.CaseID == "" || fixture.FixturePath == "" {
				return fmt.Errorf("suite %q fixture %d missing required fields", suite.SuiteID, fixtureIndex+1)
			}
			if !fixture.Required {
				return fmt.Errorf("suite %q fixture %q must be required", suite.SuiteID, fixture.CaseID)
			}
			if !IsRequiredFakeProviderBehavior(fixture.FakeProviderBehavior) {
				return fmt.Errorf("suite %q fixture %q has unknown behavior %q", suite.SuiteID, fixture.CaseID, fixture.FakeProviderBehavior)
			}
			indexed[strings.TrimPrefix(fixture.FixturePath, "tests/fixtures/e2e/")] = struct{}{}
		}
	}
	for relativePath := range RequiredE2EFixtures {
		if _, ok := indexed[relativePath]; !ok {
			return fmt.Errorf("e2e suite indexes missing fixture %q", relativePath)
		}
	}
	return nil
}

func ValidateE2EFixture(relativePath string, fixture E2EFixture) error {
	expected, ok := RequiredE2EFixtures[relativePath]
	if !ok {
		return fmt.Errorf("unexpected e2e fixture %q", relativePath)
	}
	if fixture.Status != "active" {
		return fmt.Errorf("%s must be active", relativePath)
	}
	if fixture.CaseID != expected.CaseID {
		return fmt.Errorf("%s case_id=%q, expected %q", relativePath, fixture.CaseID, expected.CaseID)
	}
	for _, requiredPR := range expected.DependsOnPR {
		if !contains(fixture.DependsOnPR, requiredPR) {
			return fmt.Errorf("%s missing dependency %s", relativePath, requiredPR)
		}
	}
	if len(fixture.UserJourney) == 0 || len(fixture.ExpectedBusinessState) == 0 {
		return fmt.Errorf("%s requires user_journey and expected_business_state", relativePath)
	}
	if len(fixture.ContractReferences) == 0 {
		return fmt.Errorf("%s requires contract_references", relativePath)
	}
	for index, reference := range fixture.ContractReferences {
		if reference.Path == "" || !reference.Required {
			return fmt.Errorf("%s contract reference %d must be required", relativePath, index+1)
		}
		if !strings.HasPrefix(reference.Path, "tests/fixtures/contracts/") {
			return fmt.Errorf("%s contract reference %q must point to tests/fixtures/contracts", relativePath, reference.Path)
		}
	}
	if fixture.FakeProvider.ProviderID == "" || !IsRequiredFakeProviderBehavior(fixture.FakeProvider.BehaviorID) {
		return fmt.Errorf("%s has invalid fake_provider", relativePath)
	}
	if !contains(fixture.ReleaseGates, "Fixture Gate") {
		return fmt.Errorf("%s missing Fixture Gate", relativePath)
	}
	return nil
}

func ValidateReleaseGovernanceText(text string) error {
	if text == "" {
		return errors.New("release governance text is required")
	}
	for _, token := range append(append([]string{}, RequiredReleaseGates...), append(RequiredFeatureFlags, RequiredMetrics...)...) {
		if !strings.Contains(text, token) {
			return fmt.Errorf("release governance missing %q", token)
		}
	}
	for _, token := range []string{"关闭", "停止消费", "释放所有未进入", "AG-UI replay", "dedupe_key"} {
		if !strings.Contains(text, token) {
			return fmt.Errorf("release governance missing rollback token %q", token)
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
