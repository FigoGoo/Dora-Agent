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

	creatorAnalytics, err := app.GetCreatorSkillUsageAnalytics(t.Context(), accountspace.AuthContext{UserID: publishFixture.SkillPackage.CreatorUserID})
	if err != nil {
		t.Fatalf("creator analytics: %v", err)
	}
	if creatorAnalytics.UsageCount != 1 || creatorAnalytics.RevenueHoldAmount != int64(committed.Settlement.CreatorCredits) || creatorAnalytics.RefundCount != 0 {
		t.Fatalf("unexpected creator analytics: %#v", creatorAnalytics)
	}
	if len(creatorAnalytics.FailureCodeSummary) != 0 {
		t.Fatalf("creator analytics must not expose private failure details: %#v", creatorAnalytics)
	}
}

func TestMarketplaceAppCreatorDraftSubmitLifecycle(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_marketplace_creator_app")
	testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-marketplace-contracts/business")
	repo := businesscore.New(db.DB)
	app := New(repo)
	app.now = func() time.Time { return time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC) }

	auth := accountspace.AuthContext{UserID: "user_creator_http_001", SpaceID: "sp_creator_http_001", LoginIdentityType: accountspace.IdentityPersonal}
	created, err := app.CreateCreatorSkillDraft(t.Context(), CreateCreatorSkillDraftInput{
		Auth:        auth,
		Meta:        accountspace.RequestMeta{IdempotencyKey: "creator-skill-draft-001"},
		Name:        "文旅脚本策划",
		Description: "把城市卖点拆成 Storyboard 和提示词。",
	})
	if err != nil {
		t.Fatalf("create creator draft: %v", err)
	}
	if created.Skill.VersionStatus != "draft" || created.Skill.ReviewStatus != "not_submitted" || created.Skill.ListingStatus != "not_listed" {
		t.Fatalf("unexpected draft skill: %#v", created.Skill)
	}
	replayed, err := app.CreateCreatorSkillDraft(t.Context(), CreateCreatorSkillDraftInput{
		Auth:        auth,
		Meta:        accountspace.RequestMeta{IdempotencyKey: "creator-skill-draft-001"},
		Name:        "不同标题应按幂等键返回原草稿",
		Description: "不同描述",
	})
	if err != nil {
		t.Fatalf("replay creator draft: %v", err)
	}
	if replayed.Skill.SkillID != created.Skill.SkillID || replayed.Skill.Name != created.Skill.Name {
		t.Fatalf("unexpected replayed draft: %#v", replayed.Skill)
	}

	submitted, err := app.SubmitCreatorSkillVersion(t.Context(), SubmitCreatorSkillVersionInput{
		Auth:    auth,
		Meta:    accountspace.RequestMeta{IdempotencyKey: "creator-skill-submit-001"},
		SkillID: created.Skill.SkillID,
		Version: created.Skill.Version,
	})
	if err != nil {
		t.Fatalf("submit creator skill: %v", err)
	}
	if submitted.SkillVersion.VersionStatus != "submitted" || submitted.SkillVersion.ReviewStatus != "submitted" || submitted.SkillVersion.SubmittedAt == nil {
		t.Fatalf("unexpected submitted skill: %#v", submitted.SkillVersion)
	}

	list, err := app.ListCreatorListings(t.Context(), ListCreatorListingsInput{Auth: auth, Limit: 10})
	if err != nil {
		t.Fatalf("list creator listings: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].SkillID != created.Skill.SkillID || list.Items[0].Description == "" {
		t.Fatalf("unexpected creator listings: %#v", list)
	}
}

func TestMarketplaceAppAdminGovernanceLifecycle(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_marketplace_admin_app")
	testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-marketplace-contracts/business")
	repo := businesscore.New(db.DB)
	app := New(repo)
	app.now = func() time.Time { return time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC) }

	creator := accountspace.AuthContext{UserID: "user_creator_admin_001", SpaceID: "sp_creator_admin_001", LoginIdentityType: accountspace.IdentityPersonal}
	draft, err := app.CreateCreatorSkillDraft(t.Context(), CreateCreatorSkillDraftInput{
		Auth:        creator,
		Meta:        accountspace.RequestMeta{IdempotencyKey: "creator-skill-draft-admin-001"},
		Name:        "审核治理 Skill",
		Description: "用于验证 PR-4 管理端审核治理闭环。",
	})
	if err != nil {
		t.Fatalf("create creator draft for admin: %v", err)
	}
	submitted, err := app.SubmitCreatorSkillVersion(t.Context(), SubmitCreatorSkillVersionInput{
		Auth: creator, Meta: accountspace.RequestMeta{IdempotencyKey: "creator-skill-submit-admin-001"},
		SkillID: draft.Skill.SkillID, Version: draft.Skill.Version,
	})
	if err != nil {
		t.Fatalf("submit creator skill for admin: %v", err)
	}

	reviews, err := app.ListAdminSkillReviews(t.Context(), ListAdminSkillReviewsInput{AdminID: "admin_001", Status: "submitted", Limit: 10})
	if err != nil {
		t.Fatalf("list admin skill reviews: %v", err)
	}
	if len(reviews.Items) != 1 || reviews.Items[0].ReviewID != submitted.SkillVersion.ReviewID {
		t.Fatalf("unexpected admin review list: %#v", reviews)
	}
	approved, err := app.ApproveSkillReview(t.Context(), ApproveSkillReviewInput{
		AdminID: "admin_001", ReviewID: submitted.SkillVersion.ReviewID, Reason: "内容和定价通过",
	})
	if err != nil {
		t.Fatalf("approve skill review: %v", err)
	}
	if approved.SkillVersion.Status != "approved" || approved.SkillVersion.VersionStatus != "published" || approved.Listing.Status != "listed" {
		t.Fatalf("unexpected approved review: %#v", approved)
	}
	listings, err := app.ListAdminMarketplaceListings(t.Context(), ListAdminMarketplaceListingsInput{AdminID: "admin_001", Limit: 10})
	if err != nil {
		t.Fatalf("list admin marketplace listings: %v", err)
	}
	if len(listings.Items) != 1 || listings.Items[0].ListingID != approved.Listing.ListingID {
		t.Fatalf("unexpected admin listing list: %#v", listings)
	}
	suspended, err := app.SuspendMarketplaceListing(t.Context(), SuspendMarketplaceListingInput{
		AdminID: "admin_001", ListingID: approved.Listing.ListingID, ReasonCode: "policy_risk",
	})
	if err != nil {
		t.Fatalf("suspend marketplace listing: %v", err)
	}
	if suspended.Listing.Status != "suspended" {
		t.Fatalf("listing should be suspended: %#v", suspended)
	}

	var publishFixture struct {
		SkillPackage  pr4.SkillPackage       `json:"skill_package"`
		SkillVersion  pr4.SkillVersion       `json:"skill_version"`
		PricingPolicy pr4.SkillPricingPolicy `json:"pricing_policy"`
		Listing       pr4.MarketplaceListing `json:"listing"`
	}
	readMarketplaceFixture(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &publishFixture)
	if err := repo.SaveCreatorPublishFlowV1(t.Context(), publishFixture.SkillPackage, publishFixture.SkillVersion, publishFixture.PricingPolicy, publishFixture.Listing); err != nil {
		t.Fatalf("seed published marketplace skill: %v", err)
	}
	buyer := accountspace.AuthContext{UserID: "user_buyer_admin_001", SpaceID: "acct_buyer_admin_001", LoginIdentityType: accountspace.IdentityPersonal}
	estimate, err := app.EstimateSkillUsageCredits(t.Context(), EstimateSkillUsageCreditsInput{
		Auth: buyer, RunID: "run_refund_admin_001", ListingID: publishFixture.Listing.ListingID,
	})
	if err != nil {
		t.Fatalf("estimate refund usage: %v", err)
	}
	createdUsage, err := app.CreateSkillUsageRecord(t.Context(), CreateSkillUsageRecordInput{
		Auth: buyer, Meta: accountspace.RequestMeta{IdempotencyKey: "run_refund_admin_001:listing:v1"},
		RunID: "run_refund_admin_001", ListingID: publishFixture.Listing.ListingID,
		PricingPolicyDigest: estimate.PricingPolicyDigest, SkillUsageDigest: estimate.SkillUsageDigest,
		EstimatedCredits: estimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create refund usage: %v", err)
	}
	if _, err := app.FreezeSkillUsageCredits(t.Context(), FreezeSkillUsageCreditsInput{
		Auth: buyer, UsageID: createdUsage.Usage.UsageID, SkillUsageDigest: estimate.SkillUsageDigest,
	}); err != nil {
		t.Fatalf("freeze refund usage: %v", err)
	}
	committed, err := app.CommitSkillUsageAndSettle(t.Context(), CommitSkillUsageAndSettleInput{Auth: buyer, UsageID: createdUsage.Usage.UsageID})
	if err != nil {
		t.Fatalf("commit refund usage: %v", err)
	}
	beforeRefund := pr4.SkillUsageRecord{
		SchemaVersion:       pr4.SchemaVersionSkillUsageRecord,
		UsageID:             committed.Usage.UsageID,
		RunID:               committed.Usage.RunID,
		ListingID:           committed.Usage.ListingID,
		SkillID:             committed.Usage.SkillID,
		SkillVersion:        committed.Usage.SkillVersion,
		PricingPolicyDigest: committed.Usage.PricingPolicyDigest,
		SkillUsageDigest:    committed.Usage.SkillUsageDigest,
		UsageStatus:         "refund_pending",
		ChargeStatus:        "charged",
		RefundStatus:        "refund_requested",
		SettlementStatus:    "pending_hold",
		EstimatedCredits:    committed.Usage.EstimatedCredits,
		CreditHoldID:        committed.Usage.CreditHoldID,
		ValueDeliveredAt:    committed.Usage.ValueDeliveredAt,
		CreatedAt:           committed.Usage.CreatedAt,
		UpdatedAt:           app.now().UTC().Add(5 * time.Minute),
	}
	if _, err := repo.MarkSkillUsageRefundPendingV1(t.Context(), beforeRefund); err != nil {
		t.Fatalf("mark refund pending: %v", err)
	}
	settlementID := committed.Settlement.SettlementID
	if err := repo.DB().Create(&businesscore.PR4SkillRefundCaseRecord{
		RefundCaseID: "refund_case_admin_001",
		UsageID:      committed.Usage.UsageID,
		SettlementID: &settlementID,
		Status:       "refund_requested",
		ReasonCode:   "delivery_mismatch",
		RefundDigest: "sha256:2929292929292929292929292929292929292929292929292929292929292929",
		CreatedBy:    buyer.UserID,
		CreatedAt:    app.now().UTC(),
		UpdatedAt:    app.now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create refund case: %v", err)
	}
	refunds, err := app.ListAdminRefundCases(t.Context(), ListAdminRefundCasesInput{AdminID: "admin_001", Status: "refund_requested", Limit: 10})
	if err != nil {
		t.Fatalf("list admin refund cases: %v", err)
	}
	if len(refunds.Items) != 1 || refunds.Items[0].RefundCaseID != "refund_case_admin_001" || refunds.Items[0].SkillName == "" {
		t.Fatalf("unexpected refund case list: %#v", refunds)
	}
	settlements, err := app.ListAdminSettlements(t.Context(), ListAdminSettlementsInput{AdminID: "admin_001", Status: "pending_hold", Limit: 10})
	if err != nil {
		t.Fatalf("list admin settlements: %v", err)
	}
	if len(settlements.Items) == 0 || settlements.Items[0].SettlementID == "" {
		t.Fatalf("unexpected settlement list: %#v", settlements)
	}
	refunded, err := app.ApproveSkillUsageRefund(t.Context(), ApproveSkillUsageRefundInput{
		AdminID: "admin_001", RefundCaseID: "refund_case_admin_001",
	})
	if err != nil {
		t.Fatalf("approve skill usage refund: %v", err)
	}
	if refunded.Usage.UsageStatus != "refunded" || refunded.Usage.RefundStatus != "refund_reversed" || refunded.Settlement.Status != "reversed" {
		t.Fatalf("unexpected refund reversal: %#v", refunded)
	}

	payoutEstimate, err := app.EstimateSkillUsageCredits(t.Context(), EstimateSkillUsageCreditsInput{
		Auth: buyer, RunID: "run_payout_admin_001", ListingID: publishFixture.Listing.ListingID,
	})
	if err != nil {
		t.Fatalf("estimate payout usage: %v", err)
	}
	payoutUsage, err := app.CreateSkillUsageRecord(t.Context(), CreateSkillUsageRecordInput{
		Auth: buyer, Meta: accountspace.RequestMeta{IdempotencyKey: "run_payout_admin_001:listing:v1"},
		RunID: "run_payout_admin_001", ListingID: publishFixture.Listing.ListingID,
		PricingPolicyDigest: payoutEstimate.PricingPolicyDigest, SkillUsageDigest: payoutEstimate.SkillUsageDigest,
		EstimatedCredits: payoutEstimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create payout usage: %v", err)
	}
	if _, err := app.FreezeSkillUsageCredits(t.Context(), FreezeSkillUsageCreditsInput{
		Auth: buyer, UsageID: payoutUsage.Usage.UsageID, SkillUsageDigest: payoutEstimate.SkillUsageDigest,
	}); err != nil {
		t.Fatalf("freeze payout usage: %v", err)
	}
	payoutCommitted, err := app.CommitSkillUsageAndSettle(t.Context(), CommitSkillUsageAndSettleInput{Auth: buyer, UsageID: payoutUsage.Usage.UsageID})
	if err != nil {
		t.Fatalf("commit payout usage: %v", err)
	}
	if _, err := app.ReleaseSkillSettlementHold(t.Context(), ReleaseSkillSettlementHoldInput{
		AdminID: "admin_001", Meta: accountspace.RequestMeta{IdempotencyKey: "settlement-release-admin-early"},
		SettlementID: payoutCommitted.Settlement.SettlementID, ReasonCode: "hold_period_completed",
	}); err == nil {
		t.Fatalf("release hold should wait until hold_until")
	}

	app.now = func() time.Time { return payoutCommitted.Settlement.HoldUntil.Add(time.Minute) }
	released, err := app.ReleaseSkillSettlementHold(t.Context(), ReleaseSkillSettlementHoldInput{
		AdminID: "admin_001", Meta: accountspace.RequestMeta{IdempotencyKey: "settlement-release-admin-001"},
		SettlementID: payoutCommitted.Settlement.SettlementID, ReasonCode: "hold_period_completed",
	})
	if err != nil {
		t.Fatalf("release settlement hold: %v", err)
	}
	if released.Settlement.Status != "eligible" || released.Payout.Action != "release_hold" || released.Payout.StatusAfter != "eligible" {
		t.Fatalf("unexpected released settlement: %#v", released)
	}
	replayedRelease, err := app.ReleaseSkillSettlementHold(t.Context(), ReleaseSkillSettlementHoldInput{
		AdminID: "admin_001", Meta: accountspace.RequestMeta{IdempotencyKey: "settlement-release-admin-001"},
		SettlementID: payoutCommitted.Settlement.SettlementID, ReasonCode: "hold_period_completed",
	})
	if err != nil {
		t.Fatalf("replay release settlement hold: %v", err)
	}
	if replayedRelease.Payout.PayoutID != released.Payout.PayoutID || replayedRelease.Settlement.Status != "eligible" {
		t.Fatalf("unexpected replayed release: %#v", replayedRelease)
	}
	confirmed, err := app.ConfirmSkillSettlementPayout(t.Context(), ConfirmSkillSettlementPayoutInput{
		AdminID: "admin_001", Meta: accountspace.RequestMeta{IdempotencyKey: "settlement-payout-admin-001"},
		SettlementID: payoutCommitted.Settlement.SettlementID, PayoutReference: "manual-ledger-20260701-001", ReasonCode: "manual_payout_confirmed",
	})
	if err != nil {
		t.Fatalf("confirm settlement payout: %v", err)
	}
	if confirmed.Settlement.Status != "settled" || confirmed.Payout.Action != "confirm_payout" || confirmed.Payout.PayoutReference == "" {
		t.Fatalf("unexpected confirmed payout: %#v", confirmed)
	}
	var payoutUsageRecord businesscore.PR4SkillUsageRecord
	if err := repo.DB().Where("usage_id = ?", payoutUsage.Usage.UsageID).First(&payoutUsageRecord).Error; err != nil {
		t.Fatalf("load payout usage record: %v", err)
	}
	if payoutUsageRecord.SettlementStatus != "settled" {
		t.Fatalf("usage settlement status should follow settlement payout, got %#v", payoutUsageRecord)
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
