package pr4

import "testing"

func TestCreatorSubmitApprovePublishFixture(t *testing.T) {
	var fixture struct {
		SkillPackage  SkillPackage       `json:"skill_package"`
		SkillVersion  SkillVersion       `json:"skill_version"`
		PricingPolicy SkillPricingPolicy `json:"pricing_policy"`
		Listing       MarketplaceListing `json:"listing"`
	}
	readFixture(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &fixture)

	if err := ValidateCreatorPublishFlow(fixture.SkillPackage, fixture.SkillVersion, fixture.PricingPolicy, fixture.Listing); err != nil {
		t.Fatalf("fixture violates creator publish contract: %v", err)
	}
}

func TestUserInstallLatestPersonalFixture(t *testing.T) {
	var fixture struct {
		Request      InstallSkillRequest `json:"request"`
		Installation SkillInstallation   `json:"installation"`
	}
	readFixture(t, "tests/fixtures/contracts/marketplace/user_install_latest_personal.json", &fixture)

	if err := ValidatePersonalLatestInstall(fixture.Request, fixture.Installation); err != nil {
		t.Fatalf("fixture violates personal install contract: %v", err)
	}
}

func TestEnterpriseInstallPinnedUpgradeFixture(t *testing.T) {
	var fixture struct {
		InitialInstallation      SkillInstallation               `json:"initial_installation"`
		UpgradeRequest           UpgradeSkillInstallationRequest `json:"upgrade_request"`
		InstallationAfterUpgrade SkillInstallation               `json:"installation_after_upgrade"`
		HistoricalRunRule        HistoricalRunRule               `json:"historical_run_rule"`
	}
	readFixture(t, "tests/fixtures/contracts/marketplace/enterprise_install_pinned_upgrade.json", &fixture)

	if err := ValidateEnterprisePinnedUpgrade(fixture.InitialInstallation, fixture.UpgradeRequest, fixture.InstallationAfterUpgrade, fixture.HistoricalRunRule); err != nil {
		t.Fatalf("fixture violates enterprise upgrade contract: %v", err)
	}
}

func TestEnterpriseUpgradeRequiresConfirmation(t *testing.T) {
	var fixture struct {
		InitialInstallation      SkillInstallation               `json:"initial_installation"`
		UpgradeRequest           UpgradeSkillInstallationRequest `json:"upgrade_request"`
		InstallationAfterUpgrade SkillInstallation               `json:"installation_after_upgrade"`
		HistoricalRunRule        HistoricalRunRule               `json:"historical_run_rule"`
	}
	readFixture(t, "tests/fixtures/contracts/marketplace/enterprise_install_pinned_upgrade.json", &fixture)

	fixture.UpgradeRequest.Confirmed = false
	if err := ValidateEnterprisePinnedUpgrade(fixture.InitialInstallation, fixture.UpgradeRequest, fixture.InstallationAfterUpgrade, fixture.HistoricalRunRule); err == nil {
		t.Fatalf("enterprise pinned upgrade must require confirmation")
	}
}
