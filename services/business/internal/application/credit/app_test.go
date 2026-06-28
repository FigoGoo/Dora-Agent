package credit

import (
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
