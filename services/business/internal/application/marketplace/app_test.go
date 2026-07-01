package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr4"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestMarketplaceAppUserInstallAndSkillUsageLifecycle(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_marketplace_app")
	testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-marketplace-contracts/business")
	repo := businesscore.New(db.DB)
	app := New(repo)
	app.now = func() time.Time { return time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC) }

	var publishFixture struct {
		SkillPackage  pr4.SkillPackage       `json:"skill_package"`
		SkillVersion  pr4.SkillVersion       `json:"skill_version"`
		PricingPolicy pr4.SkillPricingPolicy `json:"pricing_policy"`
		Listing       pr4.MarketplaceListing `json:"listing"`
	}
	readMarketplaceFixture(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &publishFixture)
	if err := repo.SaveCreatorPublishFlowV1(t.Context(), publishFixture.SkillPackage, publishFixture.SkillVersion, publishFixture.PricingPolicy, publishFixture.Listing); err != nil {
		t.Fatalf("seed publish fixture: %v", err)
	}

	auth := accountspace.AuthContext{UserID: "user_buyer_001", SpaceID: "acct_personal_001", LoginIdentityType: accountspace.IdentityPersonal}
	list, err := app.ListMarketplaceSkills(t.Context(), ListMarketplaceSkillsInput{Auth: auth, Limit: 10})
	if err != nil {
		t.Fatalf("list marketplace skills: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ListingID != publishFixture.Listing.ListingID || list.Items[0].UsageCredits != publishFixture.PricingPolicy.UsageCredits {
		t.Fatalf("unexpected marketplace list: %#v", list)
	}

	detail, err := app.GetMarketplaceSkill(t.Context(), auth, publishFixture.Listing.ListingID)
	if err != nil {
		t.Fatalf("get marketplace skill: %v", err)
	}
	if detail.Listing.SkillName != publishFixture.SkillPackage.Name {
		t.Fatalf("unexpected skill detail: %#v", detail)
	}

	installed, err := app.InstallSkill(t.Context(), InstallSkillInput{
		Auth:        auth,
		Meta:        accountspace.RequestMeta{IdempotencyKey: "acct_personal_001:listing_city_tourism_creator_001:install"},
		ListingID:   publishFixture.Listing.ListingID,
		TargetScope: pr4.AccountScopePersonal,
	})
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	if installed.Installation.AccountScope != pr4.AccountScopePersonal || installed.Installation.VersionStrategy != pr4.VersionStrategyLatestPublished {
		t.Fatalf("unexpected installation: %#v", installed)
	}
	replayedInstall, err := app.InstallSkill(t.Context(), InstallSkillInput{
		Auth:        auth,
		Meta:        accountspace.RequestMeta{IdempotencyKey: "acct_personal_001:listing_city_tourism_creator_001:install"},
		ListingID:   publishFixture.Listing.ListingID,
		TargetScope: pr4.AccountScopePersonal,
	})
	if err != nil {
		t.Fatalf("replay install skill: %v", err)
	}
	if !replayedInstall.IdempotentReplay || replayedInstall.Installation.InstallationID != installed.Installation.InstallationID {
		t.Fatalf("unexpected replayed installation: %#v", replayedInstall)
	}

	mySkills, err := app.ListInstalledSkills(t.Context(), ListInstalledSkillsInput{Auth: auth})
	if err != nil {
		t.Fatalf("list installed skills: %v", err)
	}
	if len(mySkills.Items) != 1 || mySkills.Items[0].InstallationID != installed.Installation.InstallationID {
		t.Fatalf("unexpected installed skills: %#v", mySkills)
	}

	estimate, err := app.EstimateSkillUsageCredits(t.Context(), EstimateSkillUsageCreditsInput{
		Auth: auth, RunID: "run_city_tourism_paid_app_001", ListingID: publishFixture.Listing.ListingID,
	})
	if err != nil {
		t.Fatalf("estimate skill usage: %v", err)
	}
	if estimate.EstimatedCredits != publishFixture.PricingPolicy.UsageCredits || estimate.SkillUsageDigest == "" {
		t.Fatalf("unexpected estimate: %#v", estimate)
	}

	created, err := app.CreateSkillUsageRecord(t.Context(), CreateSkillUsageRecordInput{
		Auth:                auth,
		Meta:                accountspace.RequestMeta{IdempotencyKey: "run_city_tourism_paid_app_001:listing_city_tourism_creator_001:v1"},
		RunID:               "run_city_tourism_paid_app_001",
		ListingID:           publishFixture.Listing.ListingID,
		PricingPolicyDigest: estimate.PricingPolicyDigest,
		SkillUsageDigest:    estimate.SkillUsageDigest,
		EstimatedCredits:    estimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create skill usage record: %v", err)
	}
	if created.Usage.UsageStatus != "confirmation_required" || created.Usage.ChargeStatus != "not_frozen" {
		t.Fatalf("usage must be precreated before confirmation: %#v", created.Usage)
	}

	frozen, err := app.FreezeSkillUsageCredits(t.Context(), FreezeSkillUsageCreditsInput{
		Auth: auth, UsageID: created.Usage.UsageID, SkillUsageDigest: estimate.SkillUsageDigest,
	})
	if err != nil {
		t.Fatalf("freeze skill usage: %v", err)
	}
	if frozen.Usage.UsageStatus != "running" || frozen.Usage.ChargeStatus != "frozen" || frozen.Usage.CreditHoldID == nil {
		t.Fatalf("usage must be running/frozen after confirmation: %#v", frozen.Usage)
	}

	committed, err := app.CommitSkillUsageAndSettle(t.Context(), CommitSkillUsageAndSettleInput{Auth: auth, UsageID: created.Usage.UsageID})
	if err != nil {
		t.Fatalf("commit skill usage and settle: %v", err)
	}
	if committed.Usage.UsageStatus != "value_delivered" || committed.Usage.ChargeStatus != "charged" {
		t.Fatalf("usage must be charged after value delivered: %#v", committed.Usage)
	}
	if committed.Settlement.CreatorUserID != publishFixture.SkillPackage.CreatorUserID || committed.Settlement.GrossCredits != estimate.EstimatedCredits {
		t.Fatalf("unexpected settlement: %#v", committed.Settlement)
	}
}

func readMarketplaceFixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}
