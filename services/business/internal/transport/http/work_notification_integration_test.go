package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestWorkWorkPublicAndNotificationHTTP(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_work_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	projectApp := project.New(repo, guard, auditWriter)
	notificationApp := notification.New(repo, guard)
	skillApp := skillcatalog.New(repo)
	skillApp.SetNotificationService(notificationApp)
	workApp := work.New(repo, guard, auditWriter, work.Options{PublicWebBaseURL: "http://localhost:3000", TOSBaseURL: "http://localhost/tos", Env: "local", Notification: notificationApp})
	router := NewRouter(RouterOptions{
		AccountSpace: accountApp, Admin: adminApp, Project: projectApp, Skill: skillApp, Work: workApp, Notification: notificationApp,
	})

	publicList := requestJSON(t, router, http.MethodGet, "/api/public/works?page_size=10", "", "", nil)
	if len(publicList["data"].(map[string]any)["items"].([]any)) == 0 {
		t.Fatalf("expected anonymous public works: %#v", publicList)
	}
	publicDetail := requestJSON(t, router, http.MethodGet, "/api/public/works/seed-storyboard", "", "", nil)
	publicData := publicDetail["data"].(map[string]any)
	if _, leaked := publicData["project_id"]; leaked {
		t.Fatalf("public detail leaked project_id: %#v", publicData)
	}
	if got := publicData["public_work_id"].(string); got != "pubw_seed_storyboard" {
		t.Fatalf("unexpected seed public work id %q", got)
	}
	anonymousLike := requestRaw(t, router, http.MethodPost, "/api/public/works/pubw_seed_storyboard/like", "", "idem-anon-like", map[string]any{"request_hash": "hash-anon-like"})
	if anonymousLike.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous like should require login: status=%d body=%#v", anonymousLike.Code, anonymousLike.Body)
	}
	anonymousLikeError := anonymousLike.Body["error"].(map[string]any)
	anonymousLikeDetails := anonymousLikeError["details"].(map[string]any)
	if anonymousLikeError["code"] != "UNAUTHENTICATED" || anonymousLikeDetails["login_required"] != "true" {
		t.Fatalf("anonymous like should return login_required details: %#v", anonymousLike.Body)
	}
	if anonymousLikeDetails["return_to"] != "/api/public/works/pubw_seed_storyboard/like" {
		t.Fatalf("anonymous like should preserve return_to: %#v", anonymousLikeDetails)
	}
	if anonymousLikeDetails["pending_intent"] != "POST /api/public/works/:public_work_id/like" {
		t.Fatalf("anonymous like should preserve pending_intent: %#v", anonymousLikeDetails)
	}

	userToken := loginUser(t, router, "user1001@dora.local", "local-user-change-me")
	created := requestJSON(t, router, http.MethodPost, "/api/works", userToken, "idem-http-work-create", map[string]any{
		"project_id": "prj_active_1001", "title": "HTTP Work", "asset_ids": []string{"ast_generated_1001"},
		"cover_asset_id": "ast_generated_1001", "category": "storyboard", "tags": []string{"http"}, "request_hash": "hash-http-work-create",
	})
	workID := created["data"].(map[string]any)["work"].(map[string]any)["work_id"].(string)
	title := "HTTP Public Work"
	description := "HTTP public description"
	preview := requestJSON(t, router, http.MethodPost, "/api/works/"+workID+"/share/preview", userToken, "", map[string]any{
		"public_title": title, "public_description": description, "tags": []string{"http"},
		"safety_evidence": map[string]any{
			"safety_evidence_id": "safe_http_work_share", "scene": "work_share", "result": "passed", "target_type": "work_share_text",
			"evaluated_object_digest": work.ShareTextDigest(title, description, []string{"http"}),
			"policy_version":          "local-work", "evidence_version": "2026-06-28", "evaluated_at": time.Now().UTC().Format(time.RFC3339Nano),
			"expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), "trace_id": "trace-http-work-share",
		},
	})
	previewToken := preview["data"].(map[string]any)["preview_token"].(string)
	shared := requestJSON(t, router, http.MethodPost, "/api/works/"+workID+"/share/confirm", userToken, "idem-http-work-share", map[string]any{
		"preview_token": previewToken,
	})
	publicWorkID := shared["data"].(map[string]any)["public_work_id"].(string)
	requestJSON(t, router, http.MethodPost, "/api/public/works/"+publicWorkID+"/like", userToken, "idem-http-like", map[string]any{"request_hash": "hash-http-like"})

	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	takedownPreview := requestJSON(t, router, http.MethodPost, "/api/admin/works/public/"+publicWorkID+"/take-down/preview", adminToken, "", map[string]any{
		"reason": "policy risk", "notify_author": true,
	})
	takedownToken := takedownPreview["data"].(map[string]any)["preview_token"].(string)
	takenDown := requestJSON(t, router, http.MethodPost, "/api/admin/works/public/"+publicWorkID+"/take-down/confirm", adminToken, "idem-http-takedown", map[string]any{
		"preview_token": takedownToken, "reason": "policy risk", "notify_author": true,
	})
	if got := takenDown["data"].(map[string]any)["notification_status"]; got != "created" {
		t.Fatalf("expected created notification status, got %#v", takenDown)
	}
	hidden := requestRaw(t, router, http.MethodGet, "/api/public/works/"+publicWorkID, "", "", nil)
	if hidden.Code != http.StatusNotFound {
		t.Fatalf("taken down public work should be hidden: status=%d body=%#v", hidden.Code, hidden.Body)
	}
	notifications := requestJSON(t, router, http.MethodGet, "/api/notifications?read_status=unread&page_size=10", userToken, "", nil)
	items := notifications["data"].(map[string]any)["items"].([]any)
	found := false
	var notificationID string
	for _, item := range items {
		row := item.(map[string]any)
		if row["type"] == "work_public_taken_down" {
			found = true
			notificationID = row["notification_id"].(string)
		}
	}
	if !found {
		t.Fatalf("expected work takedown notification: %#v", notifications)
	}
	navigation := requestJSON(t, router, http.MethodGet, "/api/notifications/"+notificationID+"/navigation", userToken, "", nil)
	navData := navigation["data"].(map[string]any)
	if got := navData["target_resource_id"]; got != workID {
		t.Fatalf("expected navigation target_resource_id %q, got %#v", workID, navData)
	}
	if got := navData["target_route"]; got != "/works/"+workID {
		t.Fatalf("expected navigation target_route for work, got %#v", navData)
	}
	if _, stale := navData["target_id"]; stale {
		t.Fatalf("navigation response exposed stale target_id: %#v", navData)
	}
	requestJSON(t, router, http.MethodPost, "/api/notifications/"+notificationID+"/read", userToken, "idem-http-ntf-read", map[string]any{"request_hash": "hash-http-ntf-read"})
}

func TestWorkCreatorSkillPortalHTTP(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_creator_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() {
		srcErr, dbErr := migrator.Close()
		if srcErr != nil || dbErr != nil {
			t.Fatalf("close migrator source=%v database=%v", srcErr, dbErr)
		}
	})
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	testdb.ExecSQL(t, db.DB, `
DROP TABLE IF EXISTS skill_review_records;
DROP TABLE IF EXISTS skill_versions;
`)
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "db/migrations/iterations/2026-07-01-marketplace-contracts/business/0001_skill_marketplace_settlement.up.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	projectApp := project.New(repo, guard, auditWriter)
	marketplaceApp := marketplace.New(repo)
	router := NewRouter(RouterOptions{
		AccountSpace: accountApp, Admin: adminApp, Project: projectApp, Marketplace: marketplaceApp,
	})

	userToken := loginUser(t, router, "user1001@dora.local", "local-user-change-me")
	created := requestJSON(t, router, http.MethodPost, "/api/creator/skills", userToken, "idem-creator-skill-draft-http", map[string]any{
		"name": "文旅脚本策划", "description": "把城市卖点拆成 Storyboard 和提示词。", "request_hash": "hash-creator-skill-draft-http",
	})
	skill := created["data"].(map[string]any)["skill"].(map[string]any)
	if skill["version_status"] != "draft" || skill["review_status"] != "not_submitted" || skill["listing_status"] != "not_listed" {
		t.Fatalf("unexpected creator draft response: %#v", skill)
	}
	skillID := skill["skill_id"].(string)
	version := skill["version"].(string)

	submitted := requestJSON(t, router, http.MethodPost, "/api/creator/skills/"+skillID+"/versions/"+version+"/submit", userToken, "idem-creator-skill-submit-http", map[string]any{
		"request_hash": "hash-creator-skill-submit-http",
	})
	submittedVersion := submitted["data"].(map[string]any)["skill_version"].(map[string]any)
	if submittedVersion["version_status"] != "submitted" || submittedVersion["review_status"] != "submitted" {
		t.Fatalf("unexpected creator submit response: %#v", submittedVersion)
	}
	reviewID := submittedVersion["review_id"].(string)

	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	reviews := requestJSON(t, router, http.MethodGet, "/api/admin/marketplace/skill-reviews?status=submitted&page_size=10", adminToken, "", nil)
	reviewItems := reviews["data"].(map[string]any)["items"].([]any)
	if len(reviewItems) != 1 || reviewItems[0].(map[string]any)["review_id"] != reviewID {
		t.Fatalf("unexpected admin skill reviews: %#v", reviews)
	}
	approved := requestJSON(t, router, http.MethodPost, "/api/admin/skill-reviews/"+reviewID+"/approve", adminToken, "idem-admin-skill-review-approve-http", map[string]any{
		"reason": "审核通过", "request_hash": "hash-admin-skill-review-approve-http",
	})
	approvedData := approved["data"].(map[string]any)
	listing := approvedData["listing"].(map[string]any)
	if listing["status"] != "listed" {
		t.Fatalf("unexpected approved listing: %#v", approved)
	}
	listingID := listing["listing_id"].(string)

	buyer := accountspace.AuthContext{UserID: "user_buyer_http_001", SpaceID: "sp_buyer_http_001", LoginIdentityType: accountspace.IdentityPersonal}
	estimate, err := marketplaceApp.EstimateSkillUsageCredits(t.Context(), marketplace.EstimateSkillUsageCreditsInput{
		Auth: buyer, RunID: "run_refund_http_001", ListingID: listingID,
	})
	if err != nil {
		t.Fatalf("estimate http refund usage: %v", err)
	}
	createdUsage, err := marketplaceApp.CreateSkillUsageRecord(t.Context(), marketplace.CreateSkillUsageRecordInput{
		Auth: buyer, Meta: accountspace.RequestMeta{IdempotencyKey: "run_refund_http_001:listing:v1"},
		RunID: "run_refund_http_001", ListingID: listingID,
		PricingPolicyDigest: estimate.PricingPolicyDigest, SkillUsageDigest: estimate.SkillUsageDigest,
		EstimatedCredits: estimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create http refund usage: %v", err)
	}
	if _, err := marketplaceApp.FreezeSkillUsageCredits(t.Context(), marketplace.FreezeSkillUsageCreditsInput{
		Auth: buyer, UsageID: createdUsage.Usage.UsageID, SkillUsageDigest: estimate.SkillUsageDigest,
	}); err != nil {
		t.Fatalf("freeze http refund usage: %v", err)
	}
	committed, err := marketplaceApp.CommitSkillUsageAndSettle(t.Context(), marketplace.CommitSkillUsageAndSettleInput{Auth: buyer, UsageID: createdUsage.Usage.UsageID})
	if err != nil {
		t.Fatalf("commit http refund usage: %v", err)
	}
	beforeRefund := skillmarket.SkillUsageRecord{
		SchemaVersion:       skillmarket.SchemaVersionSkillUsageRecord,
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
		UpdatedAt:           time.Now().UTC(),
	}
	if _, err := repo.MarkSkillUsageRefundPendingV1(t.Context(), beforeRefund); err != nil {
		t.Fatalf("mark http refund pending: %v", err)
	}
	settlementID := committed.Settlement.SettlementID
	if err := repo.DB().Create(&businesscore.SkillRefundCaseRecord{
		RefundCaseID: "refund_case_http_001",
		UsageID:      committed.Usage.UsageID,
		SettlementID: &settlementID,
		Status:       "refund_requested",
		ReasonCode:   "delivery_mismatch",
		RefundDigest: "sha256:3030303030303030303030303030303030303030303030303030303030303030",
		CreatedBy:    buyer.UserID,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create http refund case: %v", err)
	}
	refunds := requestJSON(t, router, http.MethodGet, "/api/admin/marketplace/refund-cases?status=refund_requested&page_size=10", adminToken, "", nil)
	refundItems := refunds["data"].(map[string]any)["items"].([]any)
	if len(refundItems) != 1 || refundItems[0].(map[string]any)["refund_case_id"] != "refund_case_http_001" {
		t.Fatalf("unexpected admin refund cases: %#v", refunds)
	}
	settlements := requestJSON(t, router, http.MethodGet, "/api/admin/marketplace/settlements?status=pending_hold&page_size=10", adminToken, "", nil)
	if len(settlements["data"].(map[string]any)["items"].([]any)) == 0 {
		t.Fatalf("expected admin settlements: %#v", settlements)
	}
	refunded := requestJSON(t, router, http.MethodPost, "/api/admin/refund-cases/refund_case_http_001/approve", adminToken, "idem-admin-refund-approve-http", map[string]any{
		"request_hash": "hash-admin-refund-approve-http",
	})
	if refunded["data"].(map[string]any)["usage"].(map[string]any)["refund_status"] != "refund_reversed" {
		t.Fatalf("unexpected approved refund: %#v", refunded)
	}
	payoutEstimate, err := marketplaceApp.EstimateSkillUsageCredits(t.Context(), marketplace.EstimateSkillUsageCreditsInput{
		Auth: buyer, RunID: "run_payout_http_001", ListingID: listingID,
	})
	if err != nil {
		t.Fatalf("estimate http payout usage: %v", err)
	}
	payoutUsage, err := marketplaceApp.CreateSkillUsageRecord(t.Context(), marketplace.CreateSkillUsageRecordInput{
		Auth: buyer, Meta: accountspace.RequestMeta{IdempotencyKey: "run_payout_http_001:listing:v1"},
		RunID: "run_payout_http_001", ListingID: listingID,
		PricingPolicyDigest: payoutEstimate.PricingPolicyDigest, SkillUsageDigest: payoutEstimate.SkillUsageDigest,
		EstimatedCredits: payoutEstimate.EstimatedCredits,
	})
	if err != nil {
		t.Fatalf("create http payout usage: %v", err)
	}
	if _, err := marketplaceApp.FreezeSkillUsageCredits(t.Context(), marketplace.FreezeSkillUsageCreditsInput{
		Auth: buyer, UsageID: payoutUsage.Usage.UsageID, SkillUsageDigest: payoutEstimate.SkillUsageDigest,
	}); err != nil {
		t.Fatalf("freeze http payout usage: %v", err)
	}
	payoutCommitted, err := marketplaceApp.CommitSkillUsageAndSettle(t.Context(), marketplace.CommitSkillUsageAndSettleInput{Auth: buyer, UsageID: payoutUsage.Usage.UsageID})
	if err != nil {
		t.Fatalf("commit http payout usage: %v", err)
	}
	if err := repo.DB().Model(&businesscore.SkillSettlementRecord{}).
		Where("settlement_id = ?", payoutCommitted.Settlement.SettlementID).
		Update("hold_until", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire settlement hold: %v", err)
	}
	released := requestJSON(t, router, http.MethodPost, "/api/admin/settlements/"+payoutCommitted.Settlement.SettlementID+"/release-hold", adminToken, "idem-admin-settlement-release-http", map[string]any{
		"reason_code": "hold_period_completed", "request_hash": "hash-admin-settlement-release-http",
	})
	if released["data"].(map[string]any)["settlement"].(map[string]any)["status"] != "eligible" {
		t.Fatalf("unexpected released settlement: %#v", released)
	}
	confirmed := requestJSON(t, router, http.MethodPost, "/api/admin/settlements/"+payoutCommitted.Settlement.SettlementID+"/confirm-payout", adminToken, "idem-admin-settlement-payout-http", map[string]any{
		"payout_reference": "manual-ledger-http-001", "reason_code": "manual_payout_confirmed", "request_hash": "hash-admin-settlement-payout-http",
	})
	if confirmed["data"].(map[string]any)["settlement"].(map[string]any)["status"] != "settled" {
		t.Fatalf("unexpected confirmed settlement payout: %#v", confirmed)
	}
	adminListings := requestJSON(t, router, http.MethodGet, "/api/admin/marketplace/listings?status=listed&page_size=10", adminToken, "", nil)
	if len(adminListings["data"].(map[string]any)["items"].([]any)) == 0 {
		t.Fatalf("expected admin marketplace listings: %#v", adminListings)
	}
	suspended := requestJSON(t, router, http.MethodPost, "/api/admin/listings/"+listingID+"/suspend", adminToken, "idem-admin-listing-suspend-http", map[string]any{
		"reason_code": "policy_risk", "request_hash": "hash-admin-listing-suspend-http",
	})
	if suspended["data"].(map[string]any)["listing"].(map[string]any)["status"] != "suspended" {
		t.Fatalf("unexpected suspended listing: %#v", suspended)
	}

	listings := requestJSON(t, router, http.MethodGet, "/api/creator/listings?page_size=10", userToken, "", nil)
	items := listings["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["skill_id"] != skillID {
		t.Fatalf("unexpected creator listings: %#v", listings)
	}
	analytics := requestJSON(t, router, http.MethodGet, "/api/creator/analytics/skill-usage", userToken, "", nil)
	analyticsData := analytics["data"].(map[string]any)
	if analyticsData["usage_count"].(float64) != 2 || analyticsData["revenue_hold_amount"].(float64) != 0 || analyticsData["refund_count"].(float64) != 1 {
		t.Fatalf("unexpected creator analytics: %#v", analyticsData)
	}
	if analyticsData["failure_code_summary"].(map[string]any)["delivery_mismatch"].(float64) != 1 {
		t.Fatalf("creator analytics should expose only aggregate failure codes: %#v", analyticsData)
	}
	if _, leaked := analyticsData["raw_provider_payload"]; leaked {
		t.Fatalf("creator analytics leaked provider payload: %#v", analyticsData)
	}
}
