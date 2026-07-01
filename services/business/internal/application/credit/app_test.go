package credit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestCreateRedeemCodesPersistsAccountTypeAndBindTarget(t *testing.T) {
	app := newCreditTestApp(t)
	codeExpiresAt := time.Now().UTC().Add(2 * time.Hour)
	creditExpiresAt := time.Now().UTC().Add(48 * time.Hour)
	out, err := app.CreateRedeemCodes(t.Context(), CreateCodesInput{
		Auth: adminAuth(), Meta: testMeta("trace-code-enterprise", "idem-code-enterprise"),
		Count: 2, Points: 100, CodeExpiresAt: codeExpiresAt, CreditExpiresAt: creditExpiresAt,
		AccountType: "enterprise", BindTargetType: "enterprise", BindTargetID: "ent_1001",
		Channel: "enterprise-campaign", Reason: "enterprise grant",
	})
	if err != nil {
		t.Fatalf("create enterprise redeem codes: %v", err)
	}
	var batch businesscore.RedeemCodeBatch
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", out.BatchID).First(&batch).Error; err != nil {
		t.Fatalf("load redeem code batch: %v", err)
	}
	if batch.AccountType != "enterprise" || batch.BindTargetType != "enterprise" || value(batch.BindTargetID) != "ent_1001" {
		t.Fatalf("redeem code batch did not persist account/bind target fields: %#v", batch)
	}
	if batch.TargetType != "enterprise" {
		t.Fatalf("target_type should remain bind target semantics, got %q", batch.TargetType)
	}
	if value(batch.ChannelCode) != "enterprise-campaign" || value(batch.Reason) != "enterprise grant" {
		t.Fatalf("channel/reason not persisted: %#v", batch)
	}
	if batch.CreditExpiresAt == nil || !batch.CreditExpiresAt.Equal(creditExpiresAt) {
		t.Fatalf("credit_expires_at not persisted, got %#v want %s", batch.CreditExpiresAt, creditExpiresAt)
	}
}

func TestCreateRedeemCodesHashIncludesContractFields(t *testing.T) {
	app := newCreditTestApp(t)
	codeExpiresAt := time.Now().UTC().Add(2 * time.Hour)
	creditExpiresAt := time.Now().UTC().Add(48 * time.Hour)
	base := CreateCodesInput{
		Auth: adminAuth(), Meta: testMeta("trace-code-hash", "idem-code-hash"),
		Count: 1, Points: 100, CodeExpiresAt: codeExpiresAt, CreditExpiresAt: creditExpiresAt,
		AccountType: "personal", BindTargetType: "user", BindTargetID: "usr_1001",
		Channel: "local", Reason: "first request",
	}
	if _, err := app.CreateRedeemCodes(t.Context(), base); err != nil {
		t.Fatalf("create first redeem code batch: %v", err)
	}
	base.AccountType = "enterprise"
	base.BindTargetType = "enterprise"
	base.BindTargetID = "ent_1001"
	base.Reason = "different request"
	_, err := app.CreateRedeemCodes(t.Context(), base)
	if codeOf(err) != bizerrors.CodeIdempotencyConflict {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT for changed contract fields, got %v", err)
	}
}

func TestRedeemCodeRejectsAccountTypeMismatchBeforeConsumingCode(t *testing.T) {
	app := newCreditTestApp(t)
	enterpriseAuth := AuthContext{
		UserID: "usr_1001", SpaceID: "sp_enterprise_1001", EnterpriseID: "ent_1001",
		EnterpriseRole: accountspace.RoleOwner, LoginIdentityType: "enterprise",
	}
	_, err := app.RedeemCode(t.Context(), RedeemInput{
		Auth: enterpriseAuth, Meta: testMeta("trace-redeem-mismatch", "idem-redeem-mismatch"),
		Code: "seed-user-code", TargetAccountType: "enterprise", RedeemChannel: "local",
	})
	if codeOf(err) != bizerrors.CodeRedeemCodeTargetMismatch {
		t.Fatalf("expected REDEEM_CODE_TARGET_MISMATCH, got %v", err)
	}
	var code businesscore.RedeemCode
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", "rc_user_1001").First(&code).Error; err != nil {
		t.Fatalf("load redeem code: %v", err)
	}
	if code.Status != "unused" {
		t.Fatalf("mismatched redeem consumed code, status=%s", code.Status)
	}
}

func TestRedeemCodeCreatesB0CreditLotMetadata(t *testing.T) {
	app := newCreditTestApp(t)
	redeem, err := app.RedeemCode(t.Context(), RedeemInput{
		Auth: testAuth(), Meta: testMeta("trace-b0-lot-redeem", "idem-b0-lot-redeem"),
		Code: "seed-user-code", TargetAccountType: "personal", RedeemChannel: "local",
	})
	if err != nil {
		t.Fatalf("redeem code: %v", err)
	}

	lot := loadB0CreditLot(t, app, redeem.CreditBatchID)
	if lot.OriginalPoints != redeem.RedeemedPoints ||
		lot.AvailablePoints != redeem.RedeemedPoints ||
		lot.FrozenPoints != 0 ||
		lot.ConsumedPoints != 0 ||
		lot.ExpiredPoints != 0 {
		t.Fatalf("redeemed lot point split mismatch: %#v redeem=%#v", lot, redeem)
	}
	if lot.GrantedAt.IsZero() {
		t.Fatal("redeemed lot granted_at is required")
	}
	var expiry struct {
		Type      string `json:"type"`
		ExpiresAt string `json:"expires_at"`
	}
	mustUnmarshalJSON(t, lot.ExpiryPolicyJSON, &expiry)
	if expiry.Type != "fixed_date" || expiry.ExpiresAt == "" {
		t.Fatalf("redeemed lot expiry policy mismatch: %s", lot.ExpiryPolicyJSON)
	}
	var scopes []string
	mustUnmarshalJSON(t, lot.SpendScopeJSON, &scopes)
	if !containsString(scopes, "tool_generation") || !containsString(scopes, "skill_usage") {
		t.Fatalf("redeemed lot spend_scope must cover tool_generation and skill_usage, got %#v", scopes)
	}
	if !lot.SettlementEligible {
		t.Fatal("SMOKE redeem lot should be settlement eligible for paid Skill smoke")
	}
}

func TestChargeToolUsageUpdatesB0CreditLotAllocationTotals(t *testing.T) {
	app := newCreditTestApp(t)
	auth := testAuth()
	estimate, err := app.EstimateToolCredits(t.Context(), EstimateToolInput{
		Auth: auth, Meta: testMeta("trace-b0-lot-charge", "idem-b0-lot-estimate"),
		ProjectID: "prj_active_1001",
		ToolUsageItems: []ToolUsageItem{{
			ToolName: "image_generate", ToolType: "model_generation", BillingUnit: "asset", Quantity: 3,
		}},
		SafetyEvidence: testSafetyEvidence("trace-b0-lot-charge", "sess_b0_lot_charge", "run_b0_lot_charge"),
	})
	if err != nil {
		t.Fatalf("estimate tool credits: %v", err)
	}
	freeze, err := app.FreezeCredits(t.Context(), FreezeInput{
		Auth: auth, Meta: testMeta("trace-b0-lot-charge", "idem-b0-lot-freeze"),
		EstimateID: estimate.EstimateID, Points: estimate.EstimatePoints, RunID: "run_b0_lot_charge", AccountID: estimate.CreditAccountID,
	})
	if err != nil {
		t.Fatalf("freeze credits: %v", err)
	}
	charge, err := app.ChargeToolUsageCredits(t.Context(), ChargeToolInput{
		Auth: auth, Meta: testMeta("trace-b0-lot-charge", "idem-b0-lot-charge"),
		ProjectID: "prj_active_1001", EstimateID: estimate.EstimateID, FreezeID: freeze.FreezeID,
		SessionID: "sess_b0_lot_charge", RunID: "run_b0_lot_charge",
		ChargeItems: []ChargeItemInput{{
			EstimateItemID:  estimate.LineItems[0].EstimateItemID,
			ToolCallID:      "tool_call_b0_lot_charge",
			ToolName:        "image_generate",
			ToolType:        "model_generation",
			BillingUnit:     "asset",
			ActualQuantity:  3,
			ExecutionStatus: "success",
		}},
	})
	if err != nil {
		t.Fatalf("charge tool usage: %v", err)
	}
	if charge.ChargedPoints != estimate.EstimatePoints {
		t.Fatalf("charged points mismatch, got %d want %d", charge.ChargedPoints, estimate.EstimatePoints)
	}

	lot := loadB0CreditLot(t, app, "cb_personal_1001_seed")
	if lot.OriginalPoints != 5000 ||
		lot.AvailablePoints != 5000-estimate.EstimatePoints ||
		lot.FrozenPoints != 0 ||
		lot.ConsumedPoints != estimate.EstimatePoints ||
		lot.ExpiredPoints != 0 {
		t.Fatalf("charged lot point split mismatch: %#v estimate=%#v", lot, estimate)
	}
	var freezeItem businesscore.CreditFreezeBatchItem
	if err := app.repo.DB().WithContext(t.Context()).
		Where("freeze_id = ? AND batch_id = ?", freeze.FreezeID, "cb_personal_1001_seed").
		First(&freezeItem).Error; err != nil {
		t.Fatalf("load freeze allocation: %v", err)
	}
	if freezeItem.ChargedPoints != estimate.EstimatePoints || freezeItem.Status != StatusCharged {
		t.Fatalf("freeze allocation should be charged, got %#v", freezeItem)
	}
}

func TestMockPayRechargeOrderCreatesRechargePackageCreditLot(t *testing.T) {
	app := newCreditTestApp(t)
	paidAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return paidAt }

	order, err := app.CreateRechargeOrder(t.Context(), CreateRechargeOrderInput{
		Auth: testAuth(), Meta: testMeta("trace-recharge-order", "idem-recharge-order-create"),
		PackageID: "pkg_1000_1m",
	})
	if err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	if order.PaymentStatus != "pending" || order.Points != 1000 || order.PriceCents != 9900 || order.CreditLotID != "" {
		t.Fatalf("unexpected pending recharge order: %#v", order)
	}

	paid, err := app.MockPayRechargeOrder(t.Context(), MockPayRechargeOrderInput{
		Auth: testAuth(), Meta: testMeta("trace-recharge-order", "idem-recharge-order-pay"),
		OrderID: order.OrderID, PaymentResult: "success", ProviderTransactionID: "mock_txn_recharge_1000_success",
	})
	if err != nil {
		t.Fatalf("mock pay recharge order: %v", err)
	}
	if paid.PaymentStatus != "paid" || paid.CreditLotID == "" {
		t.Fatalf("unexpected paid recharge order: %#v", paid)
	}

	lot := loadB0CreditLot(t, app, paid.CreditLotID)
	if lot.SourceType != "recharge_package" || lot.SourceID != paid.OrderID {
		t.Fatalf("recharge lot source mismatch: %#v order=%#v", lot, paid)
	}
	if lot.OriginalPoints != 1000 || lot.AvailablePoints != 1000 || lot.FrozenPoints != 0 || lot.ConsumedPoints != 0 || lot.ExpiredPoints != 0 {
		t.Fatalf("recharge lot points mismatch: %#v", lot)
	}
	if lot.ExpiresAt == nil || !lot.ExpiresAt.Equal(paidAt.AddDate(0, 1, 0)) {
		t.Fatalf("recharge lot expires_at mismatch: got %#v want %s", lot.ExpiresAt, paidAt.AddDate(0, 1, 0))
	}
	var expiry struct {
		Type     string `json:"type"`
		Duration string `json:"duration"`
	}
	mustUnmarshalJSON(t, lot.ExpiryPolicyJSON, &expiry)
	if expiry.Type != "relative_duration" || expiry.Duration != "P1M" {
		t.Fatalf("recharge lot expiry policy mismatch: %s", lot.ExpiryPolicyJSON)
	}
	if !lot.SettlementEligible {
		t.Fatal("recharge package lot must be settlement eligible")
	}
	var account businesscore.CreditAccount
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", paid.AccountID).First(&account).Error; err != nil {
		t.Fatalf("load credit account: %v", err)
	}
	if account.AvailablePoints != 6000 {
		t.Fatalf("available points should include recharge package, got %d", account.AvailablePoints)
	}
	if countRows(t, app.repo, &businesscore.CreditLedgerEntry{}, "entry_type = ? AND source_type = ? AND source_id = ?", "recharge", "recharge_order", paid.OrderID) != 1 {
		t.Fatal("expected one recharge ledger entry")
	}
}

func TestMockPayRechargeOrderReplayDoesNotDoubleGrant(t *testing.T) {
	app := newCreditTestApp(t)
	app.now = func() time.Time { return time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC) }
	order, err := app.CreateRechargeOrder(t.Context(), CreateRechargeOrderInput{
		Auth: testAuth(), Meta: testMeta("trace-recharge-replay", "idem-recharge-replay-create"),
		PackageID: "pkg_1000_1m",
	})
	if err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	input := MockPayRechargeOrderInput{
		Auth: testAuth(), Meta: testMeta("trace-recharge-replay", "idem-recharge-replay-pay"),
		OrderID: order.OrderID, PaymentResult: "success", ProviderTransactionID: "mock_txn_recharge_replay",
	}
	first, err := app.MockPayRechargeOrder(t.Context(), input)
	if err != nil {
		t.Fatalf("mock pay first: %v", err)
	}
	replay, err := app.MockPayRechargeOrder(t.Context(), input)
	if err != nil {
		t.Fatalf("mock pay replay: %v", err)
	}
	if replay.OrderID != first.OrderID || replay.CreditLotID != first.CreditLotID || replay.PaymentStatus != "paid" {
		t.Fatalf("mock payment replay returned different order, first=%#v replay=%#v", first, replay)
	}
	var lotCount int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.CreditBatch{}).
		Where("source_type = ? AND source_id = ?", "recharge_package", order.OrderID).
		Count(&lotCount).Error; err != nil {
		t.Fatalf("count recharge lots: %v", err)
	}
	if lotCount != 1 {
		t.Fatalf("mock payment replay created duplicate lots, count=%d", lotCount)
	}
	var account businesscore.CreditAccount
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", first.AccountID).First(&account).Error; err != nil {
		t.Fatalf("load credit account: %v", err)
	}
	if account.AvailablePoints != 6000 {
		t.Fatalf("mock payment replay double granted points, available=%d", account.AvailablePoints)
	}
}

func TestReleaseFrozenCreditsReplayReturnsOriginalResult(t *testing.T) {
	app := newCreditTestApp(t)
	auth := testAuth()
	meta := testMeta("trace-credit-release", "idem-estimate-release")
	safety := testSafetyEvidence("trace-credit-release", "sess_release", "run_release")
	estimate, err := app.EstimateGenerationCredits(t.Context(), EstimateGenerationInput{
		Auth: auth, Meta: meta, ProjectID: "prj_active_1001", ResourceType: "image",
		ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1, SafetyEvidence: safety,
	})
	if err != nil {
		t.Fatalf("estimate credits: %v", err)
	}
	freeze, err := app.FreezeCredits(t.Context(), FreezeInput{
		Auth: auth, Meta: testMeta("trace-credit-release", "idem-freeze-release"), EstimateID: estimate.EstimateID,
		Points: estimate.EstimatePoints, RunID: "run_release", ConfirmationID: "intr_release", AccountID: estimate.CreditAccountID,
	})
	if err != nil {
		t.Fatalf("freeze credits: %v", err)
	}
	first, err := app.ReleaseFrozenCredits(t.Context(), ReleaseInput{
		Auth: auth, Meta: testMeta("trace-credit-release", "idem-release-replay"), FreezeID: freeze.FreezeID,
		ReleasePoints: freeze.FrozenPoints, Reason: "test_release", RunID: "run_release",
	})
	if err != nil {
		t.Fatalf("release credits: %v", err)
	}
	replay, err := app.ReleaseFrozenCredits(t.Context(), ReleaseInput{
		Auth: auth, Meta: testMeta("trace-credit-release", "idem-release-replay"), FreezeID: freeze.FreezeID,
		ReleasePoints: freeze.FrozenPoints, Reason: "test_release", RunID: "run_release",
	})
	if err != nil {
		t.Fatalf("release replay: %v", err)
	}
	if replay.ReleasedPoints != first.ReleasedPoints || replay.ReleaseStatus != first.ReleaseStatus {
		t.Fatalf("replay returned different release result, first=%#v replay=%#v", first, replay)
	}
}

func TestAdminCreditMaintenanceExpireRefundAndReverse(t *testing.T) {
	app := newCreditTestApp(t)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	accountID := "ca_personal_1001"
	expiredAt := now.Add(-time.Hour)
	sourceID := "admin-maintenance-fixture"
	lotID := "cb_admin_maintenance_expire"
	batch := businesscore.CreditBatch{
		ID: lotID, AccountID: accountID, BatchType: "grant", SourceType: "admin_grant", SourceID: &sourceID,
		TotalPoints: 120, RemainingPoints: 120, OriginalPoints: 120, AvailablePoints: 120, GrantedAt: now.Add(-24 * time.Hour),
		ExpiresAt: &expiredAt, ExpiryPolicyJSON: creditExpiryPolicyJSON(&expiredAt), SpendScopeJSON: defaultSpendScopeJSON(),
		SettlementEligible: true, Status: StatusActive, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour),
	}
	if err := app.repo.DB().WithContext(t.Context()).Create(&batch).Error; err != nil {
		t.Fatalf("create expirable lot: %v", err)
	}
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.CreditAccount{}).
		Where("id = ?", accountID).Update("available_points", 5120).Error; err != nil {
		t.Fatalf("prepare account balance: %v", err)
	}

	expired, err := app.AdminExpireCreditLots(t.Context(), ExpireCreditLotsInput{
		Auth: adminAuth(), Meta: testMeta("trace-admin-expire-lot", "idem-admin-expire-lot"),
		LotID: lotID, Reason: "expire test lot",
	})
	if err != nil {
		t.Fatalf("expire credit lots: %v", err)
	}
	if expired.Points != 120 || expired.AffectedLots != 1 {
		t.Fatalf("unexpected expire result: %#v", expired)
	}
	var expiredLot businesscore.CreditBatch
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", lotID).First(&expiredLot).Error; err != nil {
		t.Fatalf("load expired lot: %v", err)
	}
	if expiredLot.Status != "expired" || expiredLot.AvailablePoints != 0 || expiredLot.ExpiredPoints != 120 {
		t.Fatalf("lot was not expired correctly: %#v", expiredLot)
	}
	var expireLedger businesscore.CreditLedgerEntry
	if err := app.repo.DB().WithContext(t.Context()).
		Where("entry_type = ? AND source_type = ? AND source_id = ?", "expire", "credit_lot", lotID).
		First(&expireLedger).Error; err != nil {
		t.Fatalf("load expire ledger: %v", err)
	}
	if expireLedger.PointsDelta != -120 {
		t.Fatalf("expire ledger delta mismatch: %#v", expireLedger)
	}

	reversed, err := app.AdminReverseCreditLedgerEntry(t.Context(), ReverseCreditLedgerEntryInput{
		Auth: adminAuth(), Meta: testMeta("trace-admin-reverse-ledger", "idem-admin-reverse-ledger"),
		LedgerEntryID: expireLedger.ID, Reason: "reverse expire test",
	})
	if err != nil {
		t.Fatalf("reverse credit ledger: %v", err)
	}
	if reversed.Points != 120 || reversed.ReversedLedgerEntryID != expireLedger.ID || reversed.LotID == "" {
		t.Fatalf("unexpected reverse result: %#v", reversed)
	}
	var reverseLedger businesscore.CreditLedgerEntry
	if err := app.repo.DB().WithContext(t.Context()).
		Where("entry_type = ? AND source_type = ? AND source_id = ?", "reverse", "ledger_reversal", expireLedger.ID).
		First(&reverseLedger).Error; err != nil {
		t.Fatalf("load reverse ledger: %v", err)
	}
	if reverseLedger.PointsDelta != 120 || value(reverseLedger.BatchID) != reversed.LotID {
		t.Fatalf("reverse ledger mismatch: %#v result=%#v", reverseLedger, reversed)
	}

	refunded, err := app.AdminRefundCredits(t.Context(), RefundCreditsInput{
		Auth: adminAuth(), Meta: testMeta("trace-admin-refund-credits", "idem-admin-refund-credits"),
		AccountID: accountID, OriginalLotID: reversed.LotID, Points: 30, Reason: "manual refund test",
	})
	if err != nil {
		t.Fatalf("refund credits: %v", err)
	}
	if refunded.Points != 30 || refunded.LotID != reversed.LotID {
		t.Fatalf("refund should restore unexpired original lot, got %#v", refunded)
	}
	var refundLot businesscore.CreditBatch
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", reversed.LotID).First(&refundLot).Error; err != nil {
		t.Fatalf("load refund lot: %v", err)
	}
	if refundLot.AvailablePoints != 150 || refundLot.Status != StatusActive {
		t.Fatalf("refund lot not restored: %#v", refundLot)
	}
	var account businesscore.CreditAccount
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", accountID).First(&account).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account.AvailablePoints != 5150 {
		t.Fatalf("account available points mismatch after maintenance, got %d", account.AvailablePoints)
	}
}

func TestEstimateGenerationCreditsIdempotencyReplayDoesNotCreateOrphanEstimate(t *testing.T) {
	app := newCreditTestApp(t)
	auth := testAuth()
	meta := testMeta("trace-estimate-replay", "idem-estimate-replay")
	input := EstimateGenerationInput{
		Auth: auth, Meta: meta, ProjectID: "prj_active_1001", ResourceType: "image",
		ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1,
		SafetyEvidence: testSafetyEvidence("trace-estimate-replay", "sess_estimate_replay", "run_estimate_replay"),
	}
	first, err := app.EstimateGenerationCredits(t.Context(), input)
	if err != nil {
		t.Fatalf("estimate first request: %v", err)
	}
	replay, err := app.EstimateGenerationCredits(t.Context(), input)
	if err != nil {
		t.Fatalf("estimate replay request: %v", err)
	}
	if replay.EstimateID != first.EstimateID || replay.EstimatePoints != first.EstimatePoints || len(replay.LineItems) != len(first.LineItems) {
		t.Fatalf("replay returned different estimate, first=%#v replay=%#v", first, replay)
	}
	var count int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.CreditEstimate{}).Where("project_id = ?", input.ProjectID).Count(&count).Error; err != nil {
		t.Fatalf("count estimates: %v", err)
	}
	if count != 1 {
		t.Fatalf("idempotent replay created orphan estimates, count=%d", count)
	}
}

func TestEstimateGenerationCreditsIdempotencyConflict(t *testing.T) {
	app := newCreditTestApp(t)
	auth := testAuth()
	input := EstimateGenerationInput{
		Auth: auth, Meta: testMeta("trace-estimate-conflict", "idem-estimate-conflict"), ProjectID: "prj_active_1001", ResourceType: "image",
		ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1,
		SafetyEvidence: testSafetyEvidence("trace-estimate-conflict", "sess_estimate_conflict", "run_estimate_conflict"),
	}
	if _, err := app.EstimateGenerationCredits(t.Context(), input); err != nil {
		t.Fatalf("estimate first request: %v", err)
	}
	input.Quantity = 2
	_, err := app.EstimateGenerationCredits(t.Context(), input)
	if codeOf(err) != bizerrors.CodeIdempotencyConflict {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT for changed estimate request, got %v", err)
	}
}

func newCreditTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_credit_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
}

func testAuth() AuthContext {
	return accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}
}

func adminAuth() admin.AdminAuth {
	return admin.AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session"}
}

func testMeta(traceID, idem string) RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test"}
}

func testSafetyEvidence(traceID, sessionID, runID string) *businessagent.SafetyEvidenceDTO {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	return &businessagent.SafetyEvidenceDTO{
		SafetyEvidenceId: "safe_" + runID, Scene: "generation", Result_: "passed", TargetType: "prompt",
		EvaluatedObjectDigest: "sha256:test-prompt-" + runID, PolicyVersion: "test-policy",
		EvidenceVersion: "2026-06-28", EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExpiresAt: &expires, SourceSessionId: &sessionID, SourceRunId: &runID, TraceId: traceID,
	}
}

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	if businessErr, ok := err.(*bizerrors.BusinessError); ok {
		return businessErr.Code
	}
	return ""
}

type b0CreditLotProjection struct {
	SourceType         string
	SourceID           string
	OriginalPoints     int64
	AvailablePoints    int64
	FrozenPoints       int64
	ConsumedPoints     int64
	ExpiredPoints      int64
	GrantedAt          time.Time
	ExpiresAt          *time.Time
	ExpiryPolicyJSON   string
	SpendScopeJSON     string
	SettlementEligible bool
}

func loadB0CreditLot(t *testing.T, app *App, batchID string) b0CreditLotProjection {
	t.Helper()
	var lot b0CreditLotProjection
	err := app.repo.DB().WithContext(t.Context()).Raw(`
		SELECT
			source_type,
			source_id,
			original_points,
			available_points,
			frozen_points,
			consumed_points,
			expired_points,
			granted_at,
			expires_at,
			expiry_policy_json::text AS expiry_policy_json,
			spend_scope_json::text AS spend_scope_json,
			settlement_eligible
		FROM credit_batches
		WHERE id = ?
	`, batchID).Scan(&lot).Error
	if err != nil {
		t.Fatalf("load B0 credit lot %s: %v", batchID, err)
	}
	return lot
}

func countRows(t *testing.T, repo *businesscore.Repository, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := repo.DB().WithContext(t.Context()).Model(model).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func mustUnmarshalJSON(t *testing.T, raw string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("invalid json %s: %v", raw, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
