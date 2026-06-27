package credit

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

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
