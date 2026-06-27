package work

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestShareWorkPreviewConfirmCreatesSanitizedPublicSnapshot(t *testing.T) {
	app := newWorkTestApp(t, nil)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}

	created, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-create", "idem-work-create"),
		ProjectID: "prj_active_1001", Title: "Private work", Description: "private desc",
		AssetIDs: []string{"ast_generated_1001"}, CoverAssetID: "ast_generated_1001", Category: "image", Tags: []string{"seed"},
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}

	preview, err := app.PreviewShareWork(t.Context(), PreviewShareWorkInput{
		Auth: auth, WorkID: created.Work.WorkID, PublicTitle: "Public title", PublicDescription: "Public desc", Tags: []string{"safe"},
		SafetyEvidence: workShareEvidence("trace-work-share", "Public title", "Public desc", []string{"safe"}),
	})
	if err != nil {
		t.Fatalf("preview share: %v", err)
	}
	shared, err := app.ConfirmShareWork(t.Context(), ConfirmShareWorkInput{
		Auth: auth, Meta: workMeta("trace-work-share", "idem-work-share-confirm"), WorkID: created.Work.WorkID, PreviewToken: preview.PreviewToken,
	})
	if err != nil {
		t.Fatalf("confirm share: %v", err)
	}
	if shared.PublicWorkID == "" || !strings.Contains(shared.ShareURL, "/share/") {
		t.Fatalf("unexpected share result: %#v", shared)
	}

	publicDetail, err := app.GetPublicWork(t.Context(), GetPublicWorkInput{PublicWorkID: shared.PublicWorkID})
	if err != nil {
		t.Fatalf("get public work: %v", err)
	}
	payload, _ := json.Marshal(publicDetail)
	for _, forbidden := range []string{"project_id", "session_id", "blackboard", "prompt", "credit", "model_cost", "private/generated"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("public detail leaked private field %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(string(payload), "/public/works/") {
		t.Fatalf("public detail did not use public snapshot media prefix: %s", payload)
	}

	_, err = app.UnshareWork(t.Context(), UnshareWorkInput{Auth: auth, Meta: workMeta("trace-work-unshare", "idem-work-unshare"), WorkID: created.Work.WorkID, Reason: "owner request"})
	if err != nil {
		t.Fatalf("unshare work: %v", err)
	}
	_, err = app.GetPublicWork(t.Context(), GetPublicWorkInput{PublicWorkID: shared.PublicWorkID})
	if codeOf(err) != bizerrors.CodeResourceNotFound {
		t.Fatalf("expected public work unavailable after unshare, got %v", err)
	}
}

func TestTakeDownDoesNotRollbackWhenNotificationFailsAndRecordsCompensation(t *testing.T) {
	notifier := failingNotifier{err: bizerrors.New(bizerrors.CodeInternal, "notification storage unavailable")}
	app := newWorkTestApp(t, notifier)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}
	adminAuth := AdminAuth{AdminID: "adm_1001"}

	created, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-create-2", "idem-work-create-2"),
		ProjectID: "prj_active_1001", Title: "Moderated work", AssetIDs: []string{"ast_generated_1001"}, CoverAssetID: "ast_generated_1001",
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	preview, err := app.PreviewShareWork(t.Context(), PreviewShareWorkInput{
		Auth: auth, WorkID: created.Work.WorkID, PublicTitle: "Moderated public", Tags: []string{"safe"},
		SafetyEvidence: workShareEvidence("trace-work-share-2", "Moderated public", "", []string{"safe"}),
	})
	if err != nil {
		t.Fatalf("preview share: %v", err)
	}
	shared, err := app.ConfirmShareWork(t.Context(), ConfirmShareWorkInput{Auth: auth, Meta: workMeta("trace-work-share-2", "idem-work-share-2"), WorkID: created.Work.WorkID, PreviewToken: preview.PreviewToken})
	if err != nil {
		t.Fatalf("confirm share: %v", err)
	}
	takedownPreview, err := app.PreviewTakeDownWork(t.Context(), PreviewTakeDownWorkInput{
		Auth: adminAuth, PublicWorkID: shared.PublicWorkID, Reason: "policy risk", NotifyAuthor: true,
	})
	if err != nil {
		t.Fatalf("preview takedown: %v", err)
	}
	takenDown, err := app.ConfirmTakeDownWork(t.Context(), ConfirmTakeDownWorkInput{
		Auth: adminAuth, Meta: adminMeta("trace-takedown", "idem-takedown"), PublicWorkID: shared.PublicWorkID,
		PreviewToken: takedownPreview.PreviewToken, Reason: "policy risk", NotifyAuthor: true,
	})
	if err != nil {
		t.Fatalf("confirm takedown should not rollback on notification failure: %v", err)
	}
	if takenDown.Status != "taken_down" {
		t.Fatalf("expected taken_down status, got %#v", takenDown)
	}
	if takenDown.NotificationStatus != "failed" {
		t.Fatalf("expected failed notification status, got %#v", takenDown)
	}
	if got := app.CountNotificationFailuresForTest(t.Context(), shared.PublicWorkID); got != 1 {
		t.Fatalf("expected one notification compensation failure, got %d", got)
	}
	_, err = app.GetPublicWork(t.Context(), GetPublicWorkInput{PublicWorkID: shared.PublicWorkID})
	if codeOf(err) != bizerrors.CodeResourceNotFound {
		t.Fatalf("expected public work hidden after takedown, got %v", err)
	}
	_, err = app.PreviewShareWork(t.Context(), PreviewShareWorkInput{
		Auth: auth, WorkID: created.Work.WorkID, PublicTitle: "Direct reshare", SafetyEvidence: workShareEvidence("trace-reshare", "Direct reshare", "", nil),
	})
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected taken_down direct share conflict, got %v", err)
	}
}

type failingNotifier struct {
	err error
}

func (n failingNotifier) CreateNotification(ctx Context, in NotificationInput) (notification.NotificationDTO, error) {
	return notification.NotificationDTO{}, n.err
}

func newWorkTestApp(t *testing.T, notifier NotificationCreator) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_work_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB), Options{
		PublicWebBaseURL: "http://localhost:3000", TOSBaseURL: "http://localhost/tos", Env: "local", Notification: notifier,
	})
}

func workMeta(traceID, idem string) accountspace.RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test"}
}

func adminMeta(traceID, idem string) accountspace.RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test", RequestHash: "hash-" + idem}
}

func workShareEvidence(traceID, title, description string, tags []string) *businessagent.SafetyEvidenceDTO {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	return &businessagent.SafetyEvidenceDTO{
		SafetyEvidenceId: "safe_work_share_" + strings.ReplaceAll(traceID, "-", "_"),
		Scene:            "work_share", Result_: "passed", TargetType: "work_share_text",
		EvaluatedObjectDigest: ShareTextDigest(title, description, tags),
		PolicyVersion:         "local-m5", EvidenceVersion: "2026-06-28",
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
