package credit

import (
	"testing"
	"time"
)

func TestCreditUserFlowsPopulateOperatorColumns(t *testing.T) {
	app := newCreditTestApp(t)
	auth := testAuth()
	traceID := "trace-credit-operator-user"
	estimate, err := app.EstimateGenerationCredits(t.Context(), EstimateGenerationInput{
		Auth: auth, Meta: testMeta(traceID, "idem-operator-estimate"), ProjectID: "prj_active_1001", ResourceType: "image",
		ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1,
		ToolUsageItems: []ToolUsageItem{{ToolName: "web_fetch", ToolType: "browser", BillingUnit: "call", Quantity: 1}},
		SafetyEvidence: testSafetyEvidence(traceID, "sess_operator", "run_operator"),
	})
	if err != nil {
		t.Fatalf("estimate credits: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_estimates", "estimate_id = ?", auth.UserID, auth.UserID, estimate.EstimateID)
	requireCreditOperatorColumns(t, app, "credit_estimate_items", "estimate_id = ? AND item_type = ?", auth.UserID, auth.UserID, estimate.EstimateID, "model_generation")
	requireCreditOperatorColumns(t, app, "credit_estimate_items", "estimate_id = ? AND item_type = ?", auth.UserID, auth.UserID, estimate.EstimateID, "tool_usage")

	freeze, err := app.FreezeCredits(t.Context(), FreezeInput{
		Auth: auth, Meta: testMeta(traceID, "idem-operator-freeze"), EstimateID: estimate.EstimateID,
		Points: estimate.EstimatePoints, RunID: "run_operator", ConfirmationID: "intr_operator", AccountID: estimate.CreditAccountID,
	})
	if err != nil {
		t.Fatalf("freeze credits: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_accounts", "id = ?", "", auth.UserID, estimate.CreditAccountID)
	requireCreditOperatorColumns(t, app, "credit_batches", "account_id = ? AND updated_by = ?", "", auth.UserID, estimate.CreditAccountID, auth.UserID)
	requireCreditOperatorColumns(t, app, "credit_freezes", "freeze_id = ?", auth.UserID, auth.UserID, freeze.FreezeID)
	requireCreditOperatorColumns(t, app, "credit_freeze_batch_items", "freeze_id = ?", auth.UserID, auth.UserID, freeze.FreezeID)

	var toolItemID string
	for _, item := range estimate.LineItems {
		if item.ItemType == "tool_usage" {
			toolItemID = item.EstimateItemID
			break
		}
	}
	if toolItemID == "" {
		t.Fatalf("expected tool usage estimate item")
	}
	charge, err := app.ChargeToolUsageCredits(t.Context(), ChargeToolInput{
		Auth: auth, Meta: testMeta(traceID, "idem-operator-tool-charge"), ProjectID: "prj_active_1001",
		EstimateID: estimate.EstimateID, FreezeID: freeze.FreezeID, SessionID: "sess_operator", RunID: "run_operator",
		ChargeItems: []ChargeItemInput{{
			EstimateItemID: toolItemID, ToolCallID: "tool_call_operator", ToolName: "web_fetch",
			ToolType: "browser", BillingUnit: "call", ActualQuantity: 1, ExecutionStatus: "success",
		}},
	})
	if err != nil {
		t.Fatalf("charge tool credits: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_tool_charge_batches", "tool_charge_id = ?", auth.UserID, auth.UserID, charge.ToolChargeID)
	requireCreditOperatorColumns(t, app, "credit_tool_charge_items", "tool_charge_id = ?", auth.UserID, auth.UserID, charge.ToolChargeID)
	requireCreditOperatorColumns(t, app, "credit_freezes", "freeze_id = ?", auth.UserID, auth.UserID, freeze.FreezeID)
	requireCreditOperatorColumns(t, app, "credit_freeze_batch_items", "freeze_id = ?", auth.UserID, auth.UserID, freeze.FreezeID)

	releaseTraceID := "trace-credit-operator-release"
	releaseEstimate, err := app.EstimateGenerationCredits(t.Context(), EstimateGenerationInput{
		Auth: auth, Meta: testMeta(releaseTraceID, "idem-operator-release-estimate"), ProjectID: "prj_active_1001", ResourceType: "image",
		ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1,
		SafetyEvidence: testSafetyEvidence(releaseTraceID, "sess_operator_release", "run_operator_release"),
	})
	if err != nil {
		t.Fatalf("estimate release credits: %v", err)
	}
	releaseFreeze, err := app.FreezeCredits(t.Context(), FreezeInput{
		Auth: auth, Meta: testMeta(releaseTraceID, "idem-operator-release-freeze"), EstimateID: releaseEstimate.EstimateID,
		Points: releaseEstimate.EstimatePoints, RunID: "run_operator_release", ConfirmationID: "intr_operator_release", AccountID: releaseEstimate.CreditAccountID,
	})
	if err != nil {
		t.Fatalf("freeze release credits: %v", err)
	}
	if _, err := app.ReleaseFrozenCredits(t.Context(), ReleaseInput{
		Auth: auth, Meta: testMeta(releaseTraceID, "idem-operator-release"), FreezeID: releaseFreeze.FreezeID,
		ReleasePoints: releaseFreeze.FrozenPoints, Reason: "operator_release", RunID: "run_operator_release",
	}); err != nil {
		t.Fatalf("release frozen credits: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_freezes", "freeze_id = ?", auth.UserID, auth.UserID, releaseFreeze.FreezeID)
	requireCreditOperatorColumns(t, app, "credit_freeze_batch_items", "freeze_id = ?", auth.UserID, auth.UserID, releaseFreeze.FreezeID)

	codeExpiresAt := time.Now().UTC().Add(2 * time.Hour)
	creditExpiresAt := time.Now().UTC().Add(48 * time.Hour)
	codes, err := app.CreateRedeemCodes(t.Context(), CreateCodesInput{
		Auth: adminAuth(), Meta: testMeta("trace-credit-operator-admin-codes", "idem-operator-create-codes"),
		Count: 1, Points: 100, CodeExpiresAt: codeExpiresAt, CreditExpiresAt: creditExpiresAt,
		AccountType: "personal", BindTargetType: "user", BindTargetID: auth.UserID, Channel: "operator-channel", Reason: "operator test",
	})
	if err != nil {
		t.Fatalf("create redeem codes: %v", err)
	}
	redeem, err := app.RedeemCode(t.Context(), RedeemInput{
		Auth: auth, Meta: testMeta("trace-credit-operator-redeem", "idem-operator-redeem"),
		Code: codes.Codes[0], TargetAccountType: "personal", RedeemChannel: "operator-channel",
	})
	if err != nil {
		t.Fatalf("redeem code: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_batches", "id = ?", auth.UserID, auth.UserID, redeem.CreditBatchID)
	requireCreditOperatorColumns(t, app, "redeem_codes", "redeemed_account_id = ?", "adm_root", auth.UserID, redeem.AccountID)
	requireCreditOperatorColumns(t, app, "credit_accounts", "id = ?", "", auth.UserID, redeem.AccountID)
}

func TestCreditAdminFlowsPopulateOperatorColumns(t *testing.T) {
	app := newCreditTestApp(t)
	auth := adminAuth()
	expiresAt := time.Now().UTC().Add(48 * time.Hour)

	grant, err := app.AdminGrantCredits(t.Context(), AdminGrantInput{
		Auth: auth, Meta: testMeta("trace-credit-operator-grant", "idem-operator-grant"),
		TargetType: "user", TargetID: "usr_1002", Points: 50, ExpiresAt: expiresAt, Reason: "operator grant",
	})
	if err != nil {
		t.Fatalf("admin grant credits: %v", err)
	}
	requireCreditOperatorColumns(t, app, "credit_batches", "id = ?", auth.AdminID, auth.AdminID, grant.BatchID)
	requireCreditOperatorColumns(t, app, "credit_accounts", "id = ?", "", auth.AdminID, grant.AccountID)

	codes, err := app.CreateRedeemCodes(t.Context(), CreateCodesInput{
		Auth: auth, Meta: testMeta("trace-credit-operator-codes", "idem-operator-codes"),
		Count: 2, Points: 25, CodeExpiresAt: expiresAt, CreditExpiresAt: expiresAt,
		AccountType: "personal", BindTargetType: "user", BindTargetID: "usr_1001", Channel: "admin-operator", Reason: "operator codes",
	})
	if err != nil {
		t.Fatalf("create redeem codes: %v", err)
	}
	requireCreditOperatorColumns(t, app, "redeem_code_batches", "id = ?", auth.AdminID, auth.AdminID, codes.BatchID)
	requireCreditOperatorColumns(t, app, "redeem_codes", "batch_id = ? AND status = ?", auth.AdminID, auth.AdminID, codes.BatchID, "unused")

	if _, err := app.DisableRedeemCodeBatch(t.Context(), auth, codes.BatchID); err != nil {
		t.Fatalf("disable redeem code batch: %v", err)
	}
	requireCreditOperatorColumns(t, app, "redeem_code_batches", "id = ?", auth.AdminID, auth.AdminID, codes.BatchID)
	requireCreditOperatorColumns(t, app, "redeem_codes", "batch_id = ? AND status = ?", auth.AdminID, auth.AdminID, codes.BatchID, "disabled")
}

func requireCreditOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		CreatedBy *string `gorm:"column:created_by"`
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := app.repo.DB().Raw("SELECT created_by, updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
	if tx.Error != nil {
		t.Fatalf("query operator columns for %s: %v", table, tx.Error)
	}
	if tx.RowsAffected == 0 {
		t.Fatalf("expected row in %s where %s", table, where)
	}
	if value(row.CreatedBy) != wantCreatedBy || value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
}
