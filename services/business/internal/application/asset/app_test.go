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

func TestAssetOperatorColumnsAreFilledFromUserAuth(t *testing.T) {
	app := newAssetTestApp(t)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}

	created, err := app.CreateUploadIntent(t.Context(), CreateUploadIntentInput{
		Auth: auth, Meta: assetMeta("trace-asset-operator", "idem-upload-operator"),
		ProjectID: "prj_active_1001", AssetType: "image", Filename: "operator.png",
		ContentType: "image/png", SizeBytes: 128, Checksum: "sha256:operator-upload",
		MetadataText: "operator upload", SafetyEvidence: assetSafetyEvidence("trace-asset-operator"),
	})
	if err != nil {
		t.Fatalf("create upload intent: %v", err)
	}
	requireAssetOperatorColumns(t, app, "assets", "id = ?", auth.UserID, auth.UserID, created.AssetID)
	requireAssetOperatorColumns(t, app, "upload_intents", "upload_intent_id = ?", auth.UserID, auth.UserID, created.UploadIntentID)

	if _, err := app.ConfirmUploadIntent(t.Context(), ConfirmUploadInput{
		Auth: auth, Meta: assetMeta("trace-asset-operator", "idem-confirm-operator"),
		UploadIntentID: created.UploadIntentID, Etag: "uploaded-operator-etag", SizeBytes: 128,
		ContentType: "image/png", Checksum: "sha256:operator-upload-confirmed",
	}); err != nil {
		t.Fatalf("confirm upload intent: %v", err)
	}
	requireAssetOperatorColumns(t, app, "assets", "id = ?", auth.UserID, auth.UserID, created.AssetID)
	requireAssetOperatorColumns(t, app, "asset_storage_objects", "asset_id = ?", auth.UserID, auth.UserID, created.AssetID)
	requireAssetOperatorColumns(t, app, "upload_intents", "upload_intent_id = ?", auth.UserID, auth.UserID, created.UploadIntentID)

	aborted, err := app.CreateUploadIntent(t.Context(), CreateUploadIntentInput{
		Auth: auth, Meta: assetMeta("trace-asset-abort", "idem-upload-abort-operator"),
		ProjectID: "prj_active_1001", AssetType: "image", Filename: "abort.png",
		ContentType: "image/png", SizeBytes: 128, Checksum: "sha256:operator-abort",
		MetadataText: "operator abort", SafetyEvidence: assetSafetyEvidence("trace-asset-abort"),
	})
	if err != nil {
		t.Fatalf("create abort upload intent: %v", err)
	}
	if _, err := app.AbortUploadIntent(t.Context(), auth, assetMeta("trace-asset-abort", "idem-abort-operator"), aborted.UploadIntentID); err != nil {
		t.Fatalf("abort upload intent: %v", err)
	}
	requireAssetOperatorColumns(t, app, "assets", "id = ?", auth.UserID, auth.UserID, aborted.AssetID)
	requireAssetOperatorColumns(t, app, "upload_intents", "upload_intent_id = ?", auth.UserID, auth.UserID, aborted.UploadIntentID)

	if _, err := app.PrepareGeneratedAssetObjects(t.Context(), auth, assetMeta("trace-asset-slot", "idem-slot-operator"), "prj_active_1001", "sess_operator", "run_operator", []GeneratedObjectInput{{
		ArtifactID: "art_operator", ResourceType: "image", Filename: "generated.png",
		ContentType: "image/png", SizeBytes: 128, Checksum: "sha256:operator-slot",
		MetadataSummary: map[string]string{"display_name": "operator slot"},
	}}); err != nil {
		t.Fatalf("prepare generated asset object: %v", err)
	}
	requireAssetOperatorColumns(t, app, "generated_asset_object_slots", "run_id = ? AND artifact_id = ?", auth.UserID, auth.UserID, "run_operator", "art_operator")
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

func requireAssetOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
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
	if testString(row.CreatedBy) != wantCreatedBy || testString(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, testString(row.CreatedBy), testString(row.UpdatedBy))
	}
}

func testString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
