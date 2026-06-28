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
	"gorm.io/datatypes"
)

func TestShareWorkPreviewConfirmCreatesSanitizedPublicSnapshot(t *testing.T) {
	app := newWorkTestApp(t, nil)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}

	created, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-create", "idem-work-create"),
		ProjectID: "prj_active_1001", Title: "Private work", Description: "private desc",
		AssetIDs: []string{"ast_generated_1001"}, CoverAssetID: "ast_generated_1001", Category: "storyboard", Tags: []string{"seed"},
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

	liked, err := app.LikePublicWork(t.Context(), LikePublicWorkInput{Auth: auth, Meta: workMeta("trace-like", "idem-like"), PublicWorkID: shared.PublicWorkID})
	if err != nil || !liked.Liked || liked.LikeCount == 0 {
		t.Fatalf("like public work: %#v err=%v", liked, err)
	}
	likeReplay, err := app.LikePublicWork(t.Context(), LikePublicWorkInput{Auth: auth, Meta: workMeta("trace-like", "idem-like"), PublicWorkID: shared.PublicWorkID})
	if err != nil || !likeReplay.Liked || likeReplay.PublicWorkID != shared.PublicWorkID {
		t.Fatalf("expected like replay: %#v err=%v", likeReplay, err)
	}
	unliked, err := app.UnlikePublicWork(t.Context(), LikePublicWorkInput{Auth: auth, Meta: workMeta("trace-unlike", "idem-unlike"), PublicWorkID: shared.PublicWorkID})
	if err != nil || unliked.Liked {
		t.Fatalf("unlike public work: %#v err=%v", unliked, err)
	}
	unlikeReplay, err := app.UnlikePublicWork(t.Context(), LikePublicWorkInput{Auth: auth, Meta: workMeta("trace-unlike", "idem-unlike"), PublicWorkID: shared.PublicWorkID})
	if err != nil || unlikeReplay.Liked {
		t.Fatalf("expected unlike replay: %#v err=%v", unlikeReplay, err)
	}

	unshared, err := app.UnshareWork(t.Context(), UnshareWorkInput{Auth: auth, Meta: workMeta("trace-work-unshare", "idem-work-unshare"), WorkID: created.Work.WorkID, Reason: "owner request"})
	if err != nil {
		t.Fatalf("unshare work: %v", err)
	}
	replayedUnshare, err := app.UnshareWork(t.Context(), UnshareWorkInput{Auth: auth, Meta: workMeta("trace-work-unshare", "idem-work-unshare"), WorkID: created.Work.WorkID, Reason: "owner request"})
	if err != nil || replayedUnshare.Work.WorkID != unshared.Work.WorkID || replayedUnshare.Work.ShareStatus != StatusPrivate {
		t.Fatalf("expected unshare replay: %#v err=%v", replayedUnshare, err)
	}
	_, err = app.GetPublicWork(t.Context(), GetPublicWorkInput{PublicWorkID: shared.PublicWorkID})
	if codeOf(err) != bizerrors.CodeResourceNotFound {
		t.Fatalf("expected public work unavailable after unshare, got %v", err)
	}
}

func TestPublicWorkLikeInvariantsAreStable(t *testing.T) {
	app := newWorkTestApp(t, nil)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}
	created, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-create", "idem-like-invariant-create"),
		ProjectID: "prj_active_1001", Title: "Like invariant work", AssetIDs: []string{"ast_generated_1001"}, CoverAssetID: "ast_generated_1001",
		Category: "storyboard",
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	preview, err := app.PreviewShareWork(t.Context(), PreviewShareWorkInput{
		Auth: auth, WorkID: created.Work.WorkID, PublicTitle: "Like invariant public",
		SafetyEvidence: workShareEvidence("trace-like-invariant-share", "Like invariant public", "", nil),
	})
	if err != nil {
		t.Fatalf("preview share: %v", err)
	}
	shared, err := app.ConfirmShareWork(t.Context(), ConfirmShareWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-share", "idem-like-invariant-share"),
		WorkID: created.Work.WorkID, PreviewToken: preview.PreviewToken,
	})
	if err != nil {
		t.Fatalf("confirm share: %v", err)
	}

	unlikeFirst, err := app.UnlikePublicWork(t.Context(), LikePublicWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-unlike-first", "idem-like-invariant-unlike-first"), PublicWorkID: shared.PublicWorkID,
	})
	if err != nil || unlikeFirst.LikeCount != 0 {
		t.Fatalf("first unlike must keep count non-negative, got %#v err=%v", unlikeFirst, err)
	}
	liked, err := app.LikePublicWork(t.Context(), LikePublicWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-like", "idem-like-invariant-like"), PublicWorkID: shared.PublicWorkID,
	})
	if err != nil || !liked.Liked || liked.LikeCount != 1 {
		t.Fatalf("like must increment once, got %#v err=%v", liked, err)
	}
	likedAgain, err := app.LikePublicWork(t.Context(), LikePublicWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-like-again", "idem-like-invariant-like-again"), PublicWorkID: shared.PublicWorkID,
	})
	if err != nil || !likedAgain.Liked || likedAgain.LikeCount != 1 {
		t.Fatalf("duplicate like must not increment, got %#v err=%v", likedAgain, err)
	}
	unliked, err := app.UnlikePublicWork(t.Context(), LikePublicWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-unlike", "idem-like-invariant-unlike"), PublicWorkID: shared.PublicWorkID,
	})
	if err != nil || unliked.Liked || unliked.LikeCount != 0 {
		t.Fatalf("unlike must decrement once, got %#v err=%v", unliked, err)
	}
	unlikedAgain, err := app.UnlikePublicWork(t.Context(), LikePublicWorkInput{
		Auth: auth, Meta: workMeta("trace-like-invariant-unlike-again", "idem-like-invariant-unlike-again"), PublicWorkID: shared.PublicWorkID,
	})
	if err != nil || unlikedAgain.Liked || unlikedAgain.LikeCount != 0 {
		t.Fatalf("duplicate unlike must keep count at zero, got %#v err=%v", unlikedAgain, err)
	}
	var likeRows int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.WorkLike{}).
		Where("public_work_id = ? AND user_id = ?", shared.PublicWorkID, auth.UserID).Count(&likeRows).Error; err != nil {
		t.Fatalf("count work likes: %v", err)
	}
	if likeRows != 1 {
		t.Fatalf("expected one reaction row per public work/user, got %d", likeRows)
	}
}

func TestWorkCategoryMustUseActiveDictionary(t *testing.T) {
	app := newWorkTestApp(t, nil)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}

	_, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-bad-category", "idem-work-bad-category"),
		ProjectID: "prj_active_1001", Title: "Bad category work", AssetIDs: []string{"ast_generated_1001"},
		CoverAssetID: "ast_generated_1001", Category: "free_text",
	})
	if codeOf(err) != bizerrors.CodeInvalidArgument {
		t.Fatalf("expected invalid category create error, got %v", err)
	}

	created, err := app.CreateWork(t.Context(), CreateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-good-category", "idem-work-good-category"),
		ProjectID: "prj_active_1001", Title: "Good category work", AssetIDs: []string{"ast_generated_1001"},
		CoverAssetID: "ast_generated_1001", Category: "storyboard",
	})
	if err != nil {
		t.Fatalf("create valid category work: %v", err)
	}
	badUpdate := "free_text"
	_, err = app.UpdateWork(t.Context(), UpdateWorkInput{
		Auth: auth, Meta: workMeta("trace-work-update-bad-category", "idem-work-update-bad-category"),
		WorkID: created.Work.WorkID, Category: &badUpdate,
	})
	if codeOf(err) != bizerrors.CodeInvalidArgument {
		t.Fatalf("expected invalid category update error, got %v", err)
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
	replayedTakeDown, err := app.ConfirmTakeDownWork(t.Context(), ConfirmTakeDownWorkInput{
		Auth: adminAuth, Meta: adminMeta("trace-takedown", "idem-takedown"), PublicWorkID: shared.PublicWorkID,
		PreviewToken: takedownPreview.PreviewToken, Reason: "policy risk", NotifyAuthor: true,
	})
	if err != nil || replayedTakeDown.Status != "taken_down" || replayedTakeDown.NotificationStatus != "failed" {
		t.Fatalf("expected takedown replay: %#v err=%v", replayedTakeDown, err)
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
	_, err = app.UpdateWork(t.Context(), UpdateWorkInput{
		Auth: auth, Meta: workMeta("trace-empty-takedown-edit", "idem-empty-takedown-edit"), WorkID: created.Work.WorkID,
	})
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected empty update to keep taken_down work blocked, got %v", err)
	}
	sameTitle := "Moderated work"
	_, err = app.UpdateWork(t.Context(), UpdateWorkInput{
		Auth: auth, Meta: workMeta("trace-same-takedown-edit", "idem-same-takedown-edit"), WorkID: created.Work.WorkID, Title: &sameTitle,
	})
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected same-value update to keep taken_down work blocked, got %v", err)
	}
	editedTitle := "Moderated work edited"
	edited, err := app.UpdateWork(t.Context(), UpdateWorkInput{
		Auth: auth, Meta: workMeta("trace-real-takedown-edit", "idem-real-takedown-edit"), WorkID: created.Work.WorkID, Title: &editedTitle,
	})
	if err != nil {
		t.Fatalf("real edit after takedown should reset private: %v", err)
	}
	if edited.Work.ShareStatus != StatusPrivate || edited.ShareSummary["share_status"] != StatusPrivate {
		t.Fatalf("expected real edit to reset taken_down work to private, got %#v", edited)
	}
}

func TestEnterpriseRemovedMemberCannotManageWorks(t *testing.T) {
	app := newWorkTestApp(t, nil)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_enterprise_1001", EnterpriseID: "ent_1001", EnterpriseRole: "owner", LoginIdentityType: "enterprise_member"}
	now := time.Now().UTC()
	entID := "ent_1001"
	if err := app.repo.DB().WithContext(t.Context()).Create(&businesscore.Project{
		ID: "prj_enterprise_removed", ProjectNo: "P-ENT-REMOVED", OwnerUserID: auth.UserID, SpaceID: auth.SpaceID, EnterpriseID: &entID,
		Title: "Enterprise removed project", Status: "active", CreativeStatus: "editable", CreativeAllowed: true, LastActivityAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed enterprise project: %v", err)
	}
	if err := app.repo.DB().WithContext(t.Context()).Create(&businesscore.Work{
		ID: "wrk_enterprise_removed", WorkNo: "W-ENT-REMOVED", ProjectID: "prj_enterprise_removed", OwnerUserID: auth.UserID, SpaceID: auth.SpaceID,
		Title: "Enterprise removed work", TagsJSON: datatypes.JSON([]byte("[]")), ShareStatus: StatusPrivate, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed enterprise work: %v", err)
	}
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.EnterpriseMember{}).
		Where("enterprise_id = ? AND user_id = ?", auth.EnterpriseID, auth.UserID).
		Updates(map[string]any{"status": "removed", "updated_at": now}).Error; err != nil {
		t.Fatalf("remove enterprise member: %v", err)
	}

	if _, err := app.ListMyWorks(t.Context(), ListWorksInput{Auth: auth}); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected list denied for removed enterprise member, got %v", err)
	}
	if _, err := app.GetWorkDetail(t.Context(), auth, "wrk_enterprise_removed"); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected detail denied for removed enterprise member, got %v", err)
	}
	title := "blocked update"
	if _, err := app.UpdateWork(t.Context(), UpdateWorkInput{Auth: auth, Meta: workMeta("trace-ent-update", "idem-ent-update"), WorkID: "wrk_enterprise_removed", Title: &title}); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected update denied for removed enterprise member, got %v", err)
	}
	if _, err := app.PreviewShareWork(t.Context(), PreviewShareWorkInput{Auth: auth, WorkID: "wrk_enterprise_removed", PublicTitle: "blocked share", SafetyEvidence: workShareEvidence("trace-ent-share", "blocked share", "", nil)}); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected share denied for removed enterprise member, got %v", err)
	}
	if _, err := app.UnshareWork(t.Context(), UnshareWorkInput{Auth: auth, Meta: workMeta("trace-ent-unshare", "idem-ent-unshare"), WorkID: "wrk_enterprise_removed"}); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected unshare denied for removed enterprise member, got %v", err)
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
