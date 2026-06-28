package asset

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestCreateUploadIntentRequiresRealSafetyEvidence(t *testing.T) {
	app := newAssetTestApp(t)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}
	_, err := app.CreateUploadIntent(t.Context(), CreateUploadIntentInput{
		Auth: auth, Meta: assetMeta("trace-asset", "idem-upload-missing-safety"),
		ProjectID: "prj_active_1001", AssetType: "image", Filename: "upload.png",
		ContentType: "image/png", SizeBytes: 128, Checksum: "sha256:upload",
		SafetyEvidence: &businessagent.SafetyEvidenceDTO{SafetyEvidenceId: "safe_only_id"},
	})
	if codeOf(err) != bizerrors.CodeSafetyEvidenceInvalid {
		t.Fatalf("expected SAFETY_EVIDENCE_INVALID for incomplete evidence, got %v", err)
	}

	out, err := app.CreateUploadIntent(t.Context(), CreateUploadIntentInput{
		Auth: auth, Meta: assetMeta("trace-asset", "idem-upload-valid-safety"),
		ProjectID: "prj_active_1001", AssetType: "image", Filename: "upload.png",
		ContentType: "image/png", SizeBytes: 128, Checksum: "sha256:upload",
		MetadataText: "safe upload", SafetyEvidence: assetSafetyEvidence("trace-asset"),
	})
	if err != nil {
		t.Fatalf("create upload intent with real safety evidence: %v", err)
	}
	if out.UploadIntentID == "" || out.ObjectKey == "" {
		t.Fatalf("upload intent missing ids: %#v", out)
	}
}

func newAssetTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_asset_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB), TOSOptions{Env: "local", Bucket: "dora-local", BaseURL: "http://localhost/tos"})
}

func assetMeta(traceID, idem string) accountspace.RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test"}
}

func assetSafetyEvidence(traceID string) *businessagent.SafetyEvidenceDTO {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	return &businessagent.SafetyEvidenceDTO{
		SafetyEvidenceId: "safe_asset_upload", Scene: "asset_upload_metadata", Result_: "passed", TargetType: "asset_metadata",
		EvaluatedObjectDigest: "sha256:asset-metadata", PolicyVersion: "test-policy", EvidenceVersion: "2026-06-28",
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAt: &expires, TraceId: traceID,
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
