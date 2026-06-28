package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestM5WorkPublicAndNotificationHTTP(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_m5_http")
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
			"policy_version":          "local-m5", "evidence_version": "2026-06-28", "evaluated_at": time.Now().UTC().Format(time.RFC3339Nano),
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
