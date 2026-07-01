package businesscore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr4"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestPR4BusinessMarketplaceRepositoryWithActiveMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_pr4")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-marketplace-contracts/business")
	testdb.RequireNoForeignKeys(t, db.DB)
	for _, table := range []string{
		"skill_packages",
		"skill_versions",
		"skill_pricing_policies",
		"marketplace_listings",
		"skill_installations",
		"skill_usage_records",
		"skill_settlement_records",
		"skill_refund_cases",
		"skill_review_records",
	} {
		if !testdb.TableExists(t, db.DB, table) {
			t.Fatalf("PR-4 active business migration table %s missing", table)
		}
	}
	if testdb.TableExists(t, db.DB, "tool_plans") || testdb.TableExists(t, db.DB, "tool_tasks") {
		t.Fatal("business marketplace database must not contain agent tool plan or task tables")
	}

	repo := businesscore.New(db.DB)

	var publishFixture struct {
		SkillPackage  pr4.SkillPackage       `json:"skill_package"`
		SkillVersion  pr4.SkillVersion       `json:"skill_version"`
		PricingPolicy pr4.SkillPricingPolicy `json:"pricing_policy"`
		Listing       pr4.MarketplaceListing `json:"listing"`
	}
	readPR4BusinessFixture(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &publishFixture)
	if err := repo.SaveCreatorPublishFlowV1(t.Context(), publishFixture.SkillPackage, publishFixture.SkillVersion, publishFixture.PricingPolicy, publishFixture.Listing); err != nil {
		t.Fatalf("save creator publish flow: %v", err)
	}
	if err := repo.SaveCreatorPublishFlowV1(t.Context(), publishFixture.SkillPackage, publishFixture.SkillVersion, publishFixture.PricingPolicy, publishFixture.Listing); err != nil {
		t.Fatalf("save creator publish flow idempotently: %v", err)
	}

	var personalInstallFixture struct {
		Request      pr4.InstallSkillRequest `json:"request"`
		Installation pr4.SkillInstallation   `json:"installation"`
	}
	readPR4BusinessFixture(t, "tests/fixtures/contracts/marketplace/user_install_latest_personal.json", &personalInstallFixture)
	personalInstallation, err := repo.InstallPersonalLatestSkillV1(t.Context(), personalInstallFixture.Request, personalInstallFixture.Installation)
	if err != nil {
		t.Fatalf("install personal latest skill: %v", err)
	}
	if !reflect.DeepEqual(personalInstallation, personalInstallFixture.Installation) {
		t.Fatalf("unexpected personal installation\nwant: %#v\ngot:  %#v", personalInstallFixture.Installation, personalInstallation)
	}
	replayedPersonalInstallation, err := repo.InstallPersonalLatestSkillV1(t.Context(), personalInstallFixture.Request, personalInstallFixture.Installation)
	if err != nil {
		t.Fatalf("install personal latest skill idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedPersonalInstallation, personalInstallation) {
		t.Fatalf("unexpected replayed personal installation: %#v", replayedPersonalInstallation)
	}

	var enterpriseFixture struct {
		InitialInstallation      pr4.SkillInstallation               `json:"initial_installation"`
		UpgradeRequest           pr4.UpgradeSkillInstallationRequest `json:"upgrade_request"`
		InstallationAfterUpgrade pr4.SkillInstallation               `json:"installation_after_upgrade"`
		HistoricalRunRule        pr4.HistoricalRunRule               `json:"historical_run_rule"`
	}
	readPR4BusinessFixture(t, "tests/fixtures/contracts/marketplace/enterprise_install_pinned_upgrade.json", &enterpriseFixture)
	initialEnterprise, err := repo.SaveSkillInstallationSnapshotV1(t.Context(), enterpriseFixture.InitialInstallation, "ent_001:listing_city_tourism_creator_001:install")
	if err != nil {
		t.Fatalf("save enterprise pinned installation: %v", err)
	}
	if !reflect.DeepEqual(initialEnterprise, enterpriseFixture.InitialInstallation) {
		t.Fatalf("unexpected enterprise initial installation\nwant: %#v\ngot:  %#v", enterpriseFixture.InitialInstallation, initialEnterprise)
	}
	upgradedEnterprise, err := repo.UpgradeSkillInstallationV1(t.Context(), enterpriseFixture.UpgradeRequest, enterpriseFixture.InstallationAfterUpgrade, enterpriseFixture.HistoricalRunRule)
	if err != nil {
		t.Fatalf("upgrade enterprise pinned installation: %v", err)
	}
	if err := pr4.ValidateEnterprisePinnedUpgrade(initialEnterprise, enterpriseFixture.UpgradeRequest, upgradedEnterprise, enterpriseFixture.HistoricalRunRule); err != nil {
		t.Fatalf("enterprise upgrade contract: %v", err)
	}
	if !reflect.DeepEqual(upgradedEnterprise, enterpriseFixture.InstallationAfterUpgrade) {
		t.Fatalf("unexpected enterprise upgraded installation\nwant: %#v\ngot:  %#v", enterpriseFixture.InstallationAfterUpgrade, upgradedEnterprise)
	}
	replayedEnterpriseUpgrade, err := repo.UpgradeSkillInstallationV1(t.Context(), enterpriseFixture.UpgradeRequest, enterpriseFixture.InstallationAfterUpgrade, enterpriseFixture.HistoricalRunRule)
	if err != nil {
		t.Fatalf("upgrade enterprise pinned installation idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedEnterpriseUpgrade, upgradedEnterprise) {
		t.Fatalf("unexpected replayed enterprise upgrade: %#v", replayedEnterpriseUpgrade)
	}

	var chargeFixture struct {
		Sequence         []string             `json:"sequence"`
		UsageAfterCreate pr4.SkillUsageRecord `json:"usage_after_create"`
		UsageAfterCharge pr4.SkillUsageRecord `json:"usage_after_charge"`
		Settlement       pr4.SkillSettlement  `json:"settlement"`
	}
	readPR4BusinessFixture(t, "tests/fixtures/contracts/billing/skill_usage_precreate_confirm_charge.json", &chargeFixture)
	usageAfterCreate, err := repo.CreateSkillUsageRecordV1(t.Context(), chargeFixture.UsageAfterCreate, "run_city_tourism_paid_001:listing_city_tourism_creator_001:v1")
	if err != nil {
		t.Fatalf("create skill usage record: %v", err)
	}
	if !reflect.DeepEqual(usageAfterCreate, chargeFixture.UsageAfterCreate) {
		t.Fatalf("unexpected usage after create\nwant: %#v\ngot:  %#v", chargeFixture.UsageAfterCreate, usageAfterCreate)
	}
	replayedUsageCreate, err := repo.CreateSkillUsageRecordV1(t.Context(), chargeFixture.UsageAfterCreate, "run_city_tourism_paid_001:listing_city_tourism_creator_001:v1")
	if err != nil {
		t.Fatalf("create skill usage record idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedUsageCreate, usageAfterCreate) {
		t.Fatalf("unexpected replayed usage after create: %#v", replayedUsageCreate)
	}
	frozenAt := time.Date(2026, 7, 1, 5, 12, 0, 0, time.UTC)
	frozenUsage, err := repo.FreezeSkillUsageRecordV1(t.Context(), usageAfterCreate.UsageID, usageAfterCreate.SkillUsageDigest, *chargeFixture.UsageAfterCharge.CreditHoldID, frozenAt)
	if err != nil {
		t.Fatalf("freeze skill usage record: %v", err)
	}
	if frozenUsage.UsageStatus != "running" || frozenUsage.ChargeStatus != "frozen" || frozenUsage.CreditHoldID == nil || *frozenUsage.CreditHoldID != *chargeFixture.UsageAfterCharge.CreditHoldID {
		t.Fatalf("unexpected frozen skill usage: %#v", frozenUsage)
	}
	replayedFrozenUsage, err := repo.FreezeSkillUsageRecordV1(t.Context(), usageAfterCreate.UsageID, usageAfterCreate.SkillUsageDigest, *chargeFixture.UsageAfterCharge.CreditHoldID, frozenAt)
	if err != nil {
		t.Fatalf("freeze skill usage record idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedFrozenUsage, frozenUsage) {
		t.Fatalf("unexpected replayed frozen usage: %#v", replayedFrozenUsage)
	}
	usageAfterCharge, settlement, err := repo.CommitSkillUsageAndSettleV1(t.Context(), chargeFixture.UsageAfterCharge, chargeFixture.Settlement)
	if err != nil {
		t.Fatalf("commit skill usage and settle: %v", err)
	}
	if err := pr4.ValidateSkillUsagePrecreateConfirmCharge(chargeFixture.Sequence, usageAfterCreate, usageAfterCharge, settlement); err != nil {
		t.Fatalf("skill usage charge contract: %v", err)
	}
	if !reflect.DeepEqual(usageAfterCharge, chargeFixture.UsageAfterCharge) || !reflect.DeepEqual(settlement, chargeFixture.Settlement) {
		t.Fatalf("unexpected charged usage or settlement\nusage=%#v\nsettlement=%#v", usageAfterCharge, settlement)
	}
	replayedCharge, replayedSettlement, err := repo.CommitSkillUsageAndSettleV1(t.Context(), chargeFixture.UsageAfterCharge, chargeFixture.Settlement)
	if err != nil {
		t.Fatalf("commit skill usage and settle idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedCharge, usageAfterCharge) || !reflect.DeepEqual(replayedSettlement, settlement) {
		t.Fatalf("unexpected replayed charged usage or settlement\nusage=%#v\nsettlement=%#v", replayedCharge, replayedSettlement)
	}

	var refundFixture struct {
		UsageBeforeRefund      pr4.SkillUsageRecord `json:"usage_before_refund"`
		UsageAfterRefund       pr4.SkillUsageRecord `json:"usage_after_refund"`
		SettlementAfterReverse pr4.SkillSettlement  `json:"settlement_after_reverse"`
	}
	readPR4BusinessFixture(t, "tests/fixtures/contracts/billing/skill_usage_refund_reversal.json", &refundFixture)
	usageBeforeRefund, err := repo.MarkSkillUsageRefundPendingV1(t.Context(), refundFixture.UsageBeforeRefund)
	if err != nil {
		t.Fatalf("mark skill usage refund pending: %v", err)
	}
	if !reflect.DeepEqual(usageBeforeRefund, refundFixture.UsageBeforeRefund) {
		t.Fatalf("unexpected usage before refund\nwant: %#v\ngot:  %#v", refundFixture.UsageBeforeRefund, usageBeforeRefund)
	}
	usageAfterRefund, settlementAfterReverse, err := repo.ReverseSkillUsageRefundV1(t.Context(), refundFixture.UsageAfterRefund, refundFixture.SettlementAfterReverse)
	if err != nil {
		t.Fatalf("reverse skill usage refund: %v", err)
	}
	if err := pr4.ValidateSkillUsageRefundReversal(usageBeforeRefund, usageAfterRefund, settlementAfterReverse); err != nil {
		t.Fatalf("skill usage refund reversal contract: %v", err)
	}
	if !reflect.DeepEqual(usageAfterRefund, refundFixture.UsageAfterRefund) || !reflect.DeepEqual(settlementAfterReverse, refundFixture.SettlementAfterReverse) {
		t.Fatalf("unexpected refunded usage or reversed settlement\nusage=%#v\nsettlement=%#v", usageAfterRefund, settlementAfterReverse)
	}
	replayedRefund, replayedReverseSettlement, err := repo.ReverseSkillUsageRefundV1(t.Context(), refundFixture.UsageAfterRefund, refundFixture.SettlementAfterReverse)
	if err != nil {
		t.Fatalf("reverse skill usage refund idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedRefund, usageAfterRefund) || !reflect.DeepEqual(replayedReverseSettlement, settlementAfterReverse) {
		t.Fatalf("unexpected replayed refund reversal\nusage=%#v\nsettlement=%#v", replayedRefund, replayedReverseSettlement)
	}

	var installationCount int64
	if err := db.DB.Model(&businesscore.PR4SkillInstallationRecord{}).Count(&installationCount).Error; err != nil {
		t.Fatalf("count skill installations: %v", err)
	}
	if installationCount != 2 {
		t.Fatalf("expected 2 skill installations, got %d", installationCount)
	}
	var usageCount int64
	if err := db.DB.Model(&businesscore.PR4SkillUsageRecord{}).Count(&usageCount).Error; err != nil {
		t.Fatalf("count skill usage records: %v", err)
	}
	if usageCount != 1 {
		t.Fatalf("expected 1 skill usage record, got %d", usageCount)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func readPR4BusinessFixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}
