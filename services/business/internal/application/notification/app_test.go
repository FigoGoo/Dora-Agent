package notification

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestNotificationsAreUserScopedAndReadIdempotently(t *testing.T) {
	app := newNotificationTestApp(t)
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"}
	other := accountspace.AuthContext{UserID: "usr_1002", SpaceID: "sp_personal_1002", LoginIdentityType: "personal"}

	created, err := app.CreateNotification(t.Context(), CreateNotificationInput{
		RecipientUserID: "usr_1001", Type: "skill_review_approved", Title: "Skill approved", Summary: "Approved",
		RelatedResourceType: "skill", RelatedResourceID: "sk_seed_storyboard",
		NavigationHint: map[string]any{"target_route": "/skills/sk_seed_storyboard", "target_resource_id": "sk_seed_storyboard"},
		IdempotencyKey: "ntf:skill:approved:sk_seed_storyboard", TraceID: "trace-notification",
	})
	if err != nil {
		t.Fatalf("create notification: %v", err)
	}
	replayed, err := app.CreateNotification(t.Context(), CreateNotificationInput{
		RecipientUserID: "usr_1001", Type: "skill_review_approved", Title: "Skill approved", Summary: "Approved",
		RelatedResourceType: "skill", RelatedResourceID: "sk_seed_storyboard",
		NavigationHint: map[string]any{"target_route": "/skills/sk_seed_storyboard", "target_resource_id": "sk_seed_storyboard"},
		IdempotencyKey: "ntf:skill:approved:sk_seed_storyboard", TraceID: "trace-notification",
	})
	if err != nil || replayed.NotificationID != created.NotificationID {
		t.Fatalf("expected idempotent notification replay, got %#v err=%v", replayed, err)
	}
	list, err := app.ListNotifications(t.Context(), auth, ListInput{ReadStatus: "unread", Limit: 10})
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("list notifications: %#v err=%v", list, err)
	}
	count, err := app.GetUnreadCount(t.Context(), auth)
	if err != nil || count.UnreadCount == 0 {
		t.Fatalf("unread count: %#v err=%v", count, err)
	}
	_, err = app.MarkNotificationRead(t.Context(), other, notificationMeta("trace-read-other", "idem-read-other"), created.NotificationID)
	if codeOf(err) != bizerrors.CodeResourceNotFound {
		t.Fatalf("expected other user cannot read notification, got %v", err)
	}
	read, err := app.MarkNotificationRead(t.Context(), auth, notificationMeta("trace-read", "idem-read"), created.NotificationID)
	if err != nil || read.ReadAt == nil {
		t.Fatalf("mark read: %#v err=%v", read, err)
	}
	again, err := app.MarkNotificationRead(t.Context(), auth, notificationMeta("trace-read", "idem-read"), created.NotificationID)
	if err != nil || again.NotificationID != created.NotificationID {
		t.Fatalf("expected mark read replay: %#v err=%v", again, err)
	}
	allRead, err := app.MarkAllNotificationsRead(t.Context(), auth, notificationMeta("trace-read-all", "idem-read-all"), "")
	if err != nil || allRead.UnreadCount != 0 {
		t.Fatalf("mark all read: %#v err=%v", allRead, err)
	}
	nav, err := app.GetNotificationNavigation(t.Context(), auth, created.NotificationID)
	if err != nil || !nav.Allowed || nav.TargetResourceID != "sk_seed_storyboard" {
		t.Fatalf("navigation: %#v err=%v", nav, err)
	}
}

func newNotificationTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_notification_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour))
}

func notificationMeta(traceID, idem string) accountspace.RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test", RequestHash: "hash-" + idem}
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
