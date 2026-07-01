package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	marketplaceapi "github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessskillmarketplace"
	marketplaceapp "github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestBusinessSkillMarketplaceRPCUsageLifecycle(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_marketplace_rpc")
	testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-marketplace-contracts/business")
	repo := businesscore.New(db.DB)
	app := marketplaceapp.New(repo)

	var fixture struct {
		SkillPackage  skillmarket.SkillPackage       `json:"skill_package"`
		SkillVersion  skillmarket.SkillVersion       `json:"skill_version"`
		PricingPolicy skillmarket.SkillPricingPolicy `json:"pricing_policy"`
		Listing       skillmarket.MarketplaceListing `json:"listing"`
	}
	readMarketplaceRPCFixture(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &fixture)
	if err := repo.SaveCreatorPublishFlowV1(t.Context(), fixture.SkillPackage, fixture.SkillVersion, fixture.PricingPolicy, fixture.Listing); err != nil {
		t.Fatalf("seed publish fixture: %v", err)
	}

	handler := NewHandler(nil, nil, app)
	auth := marketplaceRPCAuth()
	pageSize := int32(10)
	list, err := handler.ListMarketplaceSkills(t.Context(), &marketplaceapi.ListMarketplaceSkillsRequest{
		AuthContext: auth,
		RequestMeta: marketplaceRPCMeta("marketplace-rpc-list"),
		PageSize:    &pageSize,
	})
	if err != nil {
		t.Fatalf("list marketplace skills: %v", err)
	}
	if len(list.Listings) != 1 || list.Listings[0].ListingId != fixture.Listing.ListingID {
		t.Fatalf("unexpected marketplace listings: %#v", list.Listings)
	}

	detail, err := handler.GetMarketplaceSkill(t.Context(), &marketplaceapi.GetMarketplaceSkillRequest{
		AuthContext: auth,
		RequestMeta: marketplaceRPCMeta("marketplace-rpc-detail"),
		ListingId:   fixture.Listing.ListingID,
	})
	if err != nil {
		t.Fatalf("get marketplace skill: %v", err)
	}
	if detail.Listing.Status != marketplaceapi.MarketplaceListingStatus_LISTED {
		t.Fatalf("unexpected listing detail: %#v", detail.Listing)
	}

	installed, err := handler.InstallSkill(t.Context(), &marketplaceapi.InstallSkillRequest{
		AuthContext: auth,
		RequestMeta: marketplaceRPCMeta("marketplace-rpc-install"),
		ListingId:   fixture.Listing.ListingID,
		TargetScope: marketplaceapi.InstallationScope_PERSONAL,
	})
	if err != nil {
		t.Fatalf("install marketplace skill: %v", err)
	}
	if installed.Installation.AccountScope != marketplaceapi.InstallationScope_PERSONAL || installed.Installation.VersionStrategy != skillmarket.VersionStrategyLatestPublished {
		t.Fatalf("unexpected installation: %#v", installed.Installation)
	}

	estimate, err := handler.EstimateSkillUsageCredits(t.Context(), &marketplaceapi.EstimateSkillUsageCreditsRequest{
		AuthContext:         auth,
		RequestMeta:         marketplaceRPCMeta("marketplace-rpc-estimate"),
		RunId:               "run_city_tourism_paid_rpc_001",
		ListingId:           fixture.Listing.ListingID,
		PricingPolicyDigest: fixture.PricingPolicy.PricingPolicyDigest,
	})
	if err != nil {
		t.Fatalf("estimate skill usage: %v", err)
	}
	if estimate.EstimatedCredits != int64(fixture.PricingPolicy.UsageCredits) || estimate.SkillUsageDigest == "" {
		t.Fatalf("unexpected estimate: %#v", estimate)
	}

	created, err := handler.CreateSkillUsageRecord(t.Context(), &marketplaceapi.CreateSkillUsageRecordRequest{
		AuthContext:         auth,
		RequestMeta:         marketplaceRPCMeta("marketplace-rpc-create"),
		RunId:               "run_city_tourism_paid_rpc_001",
		ListingId:           fixture.Listing.ListingID,
		SkillId:             fixture.SkillPackage.SkillID,
		SkillVersion:        fixture.SkillVersion.Version,
		PricingPolicyDigest: estimate.PricingPolicyDigest,
		SkillUsageDigest:    estimate.SkillUsageDigest,
		EstimatedCredits:    estimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create skill usage record: %v", err)
	}
	if created.Usage.UsageStatus != "confirmation_required" || created.Usage.ChargeStatus != "not_frozen" {
		t.Fatalf("usage must be precreated before freeze: %#v", created.Usage)
	}

	frozen, err := handler.FreezeSkillUsageCredits(t.Context(), &marketplaceapi.FreezeSkillUsageCreditsRequest{
		AuthContext:      auth,
		RequestMeta:      marketplaceRPCMeta("marketplace-rpc-freeze"),
		UsageId:          created.Usage.UsageId,
		SkillUsageDigest: estimate.SkillUsageDigest,
	})
	if err != nil {
		t.Fatalf("freeze skill usage credits: %v", err)
	}
	if frozen.Usage.UsageStatus != "running" || frozen.Usage.ChargeStatus != "frozen" || frozen.Usage.CreditHoldId == nil {
		t.Fatalf("usage must be running/frozen after confirmation: %#v", frozen.Usage)
	}

	committed, err := handler.CommitSkillUsageAndSettle(t.Context(), &marketplaceapi.CommitSkillUsageAndSettleRequest{
		AuthContext:          auth,
		RequestMeta:          marketplaceRPCMeta("marketplace-rpc-commit"),
		UsageId:              created.Usage.UsageId,
		ValueDeliveredDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("commit skill usage and settle: %v", err)
	}
	if committed.Usage.UsageStatus != "value_delivered" || committed.Usage.ChargeStatus != "charged" || committed.SettlementId == "" {
		t.Fatalf("unexpected committed usage: %#v", committed)
	}

	releaseEstimate, releaseCreated := createFrozenMarketplaceRPCUsage(t, handler, auth, fixture, "run_city_tourism_paid_rpc_release_001")
	released, err := handler.ReleaseSkillUsageFreeze(t.Context(), &marketplaceapi.ReleaseSkillUsageFreezeRequest{
		AuthContext:   auth,
		RequestMeta:   marketplaceRPCMeta("marketplace-rpc-release"),
		UsageId:       releaseCreated.Usage.UsageId,
		ReleaseReason: "graph_failed_before_value_delivered",
	})
	if err != nil {
		t.Fatalf("release skill usage freeze: %v", err)
	}
	if released.Usage.SkillUsageDigest != releaseEstimate.SkillUsageDigest || released.Usage.UsageStatus != "released" || released.Usage.ChargeStatus != "released" {
		t.Fatalf("unexpected released usage: %#v", released.Usage)
	}
}

func createFrozenMarketplaceRPCUsage(t *testing.T, handler *Handler, auth *businessagent.AuthContext, fixture struct {
	SkillPackage  skillmarket.SkillPackage       `json:"skill_package"`
	SkillVersion  skillmarket.SkillVersion       `json:"skill_version"`
	PricingPolicy skillmarket.SkillPricingPolicy `json:"pricing_policy"`
	Listing       skillmarket.MarketplaceListing `json:"listing"`
}, runID string) (*marketplaceapi.EstimateSkillUsageCreditsResponse, *marketplaceapi.CreateSkillUsageRecordResponse) {
	t.Helper()
	estimate, err := handler.EstimateSkillUsageCredits(t.Context(), &marketplaceapi.EstimateSkillUsageCreditsRequest{
		AuthContext:         auth,
		RequestMeta:         marketplaceRPCMeta(runID + ":estimate"),
		RunId:               runID,
		ListingId:           fixture.Listing.ListingID,
		PricingPolicyDigest: fixture.PricingPolicy.PricingPolicyDigest,
	})
	if err != nil {
		t.Fatalf("estimate release skill usage: %v", err)
	}
	created, err := handler.CreateSkillUsageRecord(t.Context(), &marketplaceapi.CreateSkillUsageRecordRequest{
		AuthContext:         auth,
		RequestMeta:         marketplaceRPCMeta(runID + ":create"),
		RunId:               runID,
		ListingId:           fixture.Listing.ListingID,
		SkillId:             fixture.SkillPackage.SkillID,
		SkillVersion:        fixture.SkillVersion.Version,
		PricingPolicyDigest: estimate.PricingPolicyDigest,
		SkillUsageDigest:    estimate.SkillUsageDigest,
		EstimatedCredits:    estimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create release skill usage: %v", err)
	}
	if _, err := handler.FreezeSkillUsageCredits(t.Context(), &marketplaceapi.FreezeSkillUsageCreditsRequest{
		AuthContext:      auth,
		RequestMeta:      marketplaceRPCMeta(runID + ":freeze"),
		UsageId:          created.Usage.UsageId,
		SkillUsageDigest: estimate.SkillUsageDigest,
	}); err != nil {
		t.Fatalf("freeze release skill usage: %v", err)
	}
	return estimate, created
}

func marketplaceRPCAuth() *businessagent.AuthContext {
	return &businessagent.AuthContext{
		ActorUserId:       "user_buyer_001",
		LoginIdentityType: businessagent.LoginIdentityType_PERSONAL,
		SpaceId:           optionalString("acct_personal_001"),
	}
}

func marketplaceRPCMeta(idempotencyKey string) *businessagent.RequestMeta {
	return &businessagent.RequestMeta{
		RequestId:      "req_" + idempotencyKey,
		TraceId:        "trace_" + idempotencyKey,
		IdempotencyKey: optionalString(idempotencyKey),
		Source:         "rpc_test",
	}
}

func readMarketplaceRPCFixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}
