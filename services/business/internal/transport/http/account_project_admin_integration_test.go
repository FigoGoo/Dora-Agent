package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc"
)

func TestAccountProjectBusinessHTTPAndRPCIdentityProject(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_account-project")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	projectApp := project.New(repo, guard, auditWriter)
	router := NewRouter(RouterOptions{AccountSpace: accountApp, Admin: adminApp, Project: projectApp})

	userToken := loginUser(t, router, "user1001@dora.local", "local-user-change-me")
	current := requestJSON(t, router, http.MethodGet, "/api/account/current-space", userToken, "", nil)
	if current["data"].(map[string]any)["space_id"] != "sp_personal_1001" {
		t.Fatalf("unexpected current-space response: %#v", current)
	}

	seedProject := requestJSON(t, router, http.MethodGet, "/api/projects/prj_active_1001", userToken, "", nil)
	baseUpdatedAt := seedProject["data"].(map[string]any)["updated_at"].(string)
	updated := requestJSON(t, router, http.MethodPatch, "/api/projects/prj_active_1001", userToken, "idem-project-update", map[string]any{
		"title": "Account Project Updated Project", "cover_asset_id": "ast_generated_1001", "base_updated_at": baseUpdatedAt,
	})
	if updated["data"].(map[string]any)["cover_asset_id"] != "ast_generated_1001" {
		t.Fatalf("project update did not keep project cover: %#v", updated)
	}
	stale := requestRaw(t, router, http.MethodPatch, "/api/projects/prj_active_1001", userToken, "idem-project-update-stale", map[string]any{
		"title": "stale update", "base_updated_at": baseUpdatedAt,
	})
	if stale.Code != http.StatusConflict || stale.CodeValue() != string(bizerrors.CodeStateConflict) {
		t.Fatalf("expected stale base_updated_at conflict, status=%d body=%#v", stale.Code, stale.Body)
	}
	currentProject := requestJSON(t, router, http.MethodGet, "/api/projects/prj_active_1001", userToken, "", nil)
	currentBase := currentProject["data"].(map[string]any)["updated_at"].(string)
	invalidCover := requestRaw(t, router, http.MethodPatch, "/api/projects/prj_active_1001", userToken, "idem-project-update-cover-denied", map[string]any{
		"cover_asset_id": "ast_other_space_1002", "base_updated_at": currentBase,
	})
	if invalidCover.Code != http.StatusForbidden || invalidCover.CodeValue() != string(bizerrors.CodePermissionDenied) {
		t.Fatalf("expected cover asset permission denied, status=%d body=%#v", invalidCover.Code, invalidCover.Body)
	}

	enterprise := requestJSON(t, router, http.MethodPost, "/api/enterprise/register", userToken, "idem-enterprise-create", map[string]any{
		"enterprise_name": "Account Project Enterprise", "owner_display_name": "Owner", "contact_email": "owner@dora.local",
	})
	enterpriseID := enterprise["data"].(map[string]any)["enterprise_id"].(string)
	switched := requestJSON(t, router, http.MethodPost, "/api/account/switch-identity", userToken, "idem-enterprise-switch", map[string]any{
		"target_identity_type": "enterprise_member", "target_enterprise_id": enterpriseID,
	})
	if switched["data"].(map[string]any)["login_identity_type"] != "enterprise_member" {
		t.Fatalf("switch identity response = %#v", switched)
	}
	members := requestJSON(t, router, http.MethodGet, "/api/enterprise/members", userToken, "", nil)
	if members["data"].(map[string]any)["total"].(float64) < 1 {
		t.Fatalf("enterprise owner member missing: %#v", members)
	}
	requestJSON(t, router, http.MethodPost, "/api/account/switch-identity", userToken, "idem-personal-switch-back", map[string]any{
		"target_identity_type": "personal",
	})

	created := requestJSON(t, router, http.MethodPost, "/api/projects", userToken, "idem-project-create", map[string]any{"title": "Account Project Project"})
	projectID := created["data"].(map[string]any)["project_id"].(string)
	archived := requestJSON(t, router, http.MethodPost, "/api/projects/"+projectID+"/archive", userToken, "idem-project-archive", map[string]any{"reason": "done"})
	if archived["data"].(map[string]any)["status"] != "archived" {
		t.Fatalf("archive response did not mark archived: %#v", archived)
	}

	handler := rpc.NewHandler(accountApp, projectApp)
	spaceResp, err := handler.ResolveCurrentSpaceContext(t.Context(), &businessagent.ResolveCurrentSpaceContextRequest{
		AuthContext:     &businessagent.AuthContext{ActorUserId: "usr_1001", LoginIdentityType: businessagent.LoginIdentityType_PERSONAL, SpaceId: ptr("sp_personal_1001")},
		RequestMeta:     &businessagent.RequestMeta{RequestId: "req-rpc", TraceId: "trace-rpc", Source: "test"},
		ExpectedSpaceId: ptr("sp_personal_1001"),
	})
	if err != nil {
		t.Fatalf("resolve current space rpc: %v", err)
	}
	if spaceResp.SpaceId != "sp_personal_1001" || spaceResp.CreditAccountId != "ca_personal_1001" {
		t.Fatalf("unexpected space rpc response: %#v", spaceResp)
	}
	_, err = handler.CheckProjectAccess(t.Context(), &businessagent.CheckProjectAccessRequest{
		AuthContext:   &businessagent.AuthContext{ActorUserId: "usr_1001", LoginIdentityType: businessagent.LoginIdentityType_PERSONAL, SpaceId: ptr("sp_personal_1001")},
		RequestMeta:   &businessagent.RequestMeta{RequestId: "req-rpc", TraceId: "trace-rpc", Source: "test"},
		ProjectId:     projectID,
		AccessPurpose: businessagent.ProjectAccessPurpose_CONTINUE_CREATION,
	})
	if codeOf(err) != bizerrors.CodeProjectArchived {
		t.Fatalf("expected PROJECT_ARCHIVED for archived continue_creation, got %v", err)
	}
	viewResp, err := handler.CheckProjectAccess(t.Context(), &businessagent.CheckProjectAccessRequest{
		AuthContext:   &businessagent.AuthContext{ActorUserId: "usr_1001", LoginIdentityType: businessagent.LoginIdentityType_PERSONAL, SpaceId: ptr("sp_personal_1001")},
		RequestMeta:   &businessagent.RequestMeta{RequestId: "req-rpc", TraceId: "trace-rpc", Source: "test"},
		ProjectId:     projectID,
		AccessPurpose: businessagent.ProjectAccessPurpose_VIEW,
	})
	if err != nil || !viewResp.Allowed || viewResp.CreativeAllowed {
		t.Fatalf("archived view should be allowed read-only, resp=%#v err=%v", viewResp, err)
	}

	_, err = handler.ResolveCurrentSpaceContext(t.Context(), &businessagent.ResolveCurrentSpaceContextRequest{
		AuthContext:     &businessagent.AuthContext{ActorUserId: "usr_1001", LoginIdentityType: businessagent.LoginIdentityType_PERSONAL, SpaceId: ptr("sp_personal_1001")},
		RequestMeta:     &businessagent.RequestMeta{RequestId: "req-rpc", TraceId: "trace-rpc", Source: "test"},
		ExpectedSpaceId: ptr("sp_personal_1002"),
	})
	if codeOf(err) != bizerrors.CodeCrossSpaceDenied {
		t.Fatalf("expected CROSS_SPACE_DENIED for wrong expected space, got %v", err)
	}

	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	rotated := requestJSON(t, router, http.MethodPost, "/api/admin/auth/rotate-password", adminToken, "idem-admin-rotate", map[string]any{
		"current_password": "local-admin-change-me", "new_password": "local-admin-change-me-rotated", "reason": "account forced rotation",
	})
	adminToken = rotated["data"].(map[string]any)["access_token"].(string)
	preview := requestJSON(t, router, http.MethodPost, "/api/admin/users/usr_1001/status/preview", adminToken, "", map[string]any{
		"target_status": "disabled", "reason": "account-project disable user",
	})
	previewToken := preview["data"].(map[string]any)["preview_token"].(string)
	disabled := requestJSON(t, router, http.MethodPost, "/api/admin/users/usr_1001/status/confirm", adminToken, "idem-user-disable", map[string]any{
		"target_status": "disabled", "preview_token": previewToken, "reason": "account-project disable user",
	})
	if disabled["data"].(map[string]any)["status"] != "disabled" {
		t.Fatalf("user disable response = %#v", disabled)
	}
	_, err = handler.ResolveCurrentSpaceContext(t.Context(), &businessagent.ResolveCurrentSpaceContextRequest{
		AuthContext:     &businessagent.AuthContext{ActorUserId: "usr_1001", LoginIdentityType: businessagent.LoginIdentityType_PERSONAL, SpaceId: ptr("sp_personal_1001")},
		RequestMeta:     &businessagent.RequestMeta{RequestId: "req-rpc-disabled", TraceId: "trace-rpc", Source: "test"},
		ExpectedSpaceId: ptr("sp_personal_1001"),
	})
	if codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected PERMISSION_DENIED for disabled user RPC, got %v", err)
	}
}

func loginUser(t *testing.T, router http.Handler, account, password string) string {
	t.Helper()
	resp := requestJSON(t, router, http.MethodPost, "/api/auth/login", "", "idem-login", map[string]any{"login_type": "personal", "account": account, "password": password})
	token := resp["data"].(map[string]any)["access_token"].(string)
	if token == "" {
		t.Fatalf("login did not return access token: %#v", resp)
	}
	return token
}

func loginAdmin(t *testing.T, router http.Handler, account, password string) string {
	t.Helper()
	resp := requestJSON(t, router, http.MethodPost, "/api/admin/auth/login", "", "", map[string]any{"account": account, "password": password})
	token := resp["data"].(map[string]any)["access_token"].(string)
	if token == "" {
		t.Fatalf("admin login did not return access token: %#v", resp)
	}
	return token
}

type rawResponse struct {
	Code int
	Body map[string]any
}

func (r rawResponse) CodeValue() string {
	if value, ok := r.Body["code"].(string); ok {
		return value
	}
	if errBody, ok := r.Body["error"].(map[string]any); ok {
		if value, ok := errBody["code"].(string); ok {
			return value
		}
	}
	return ""
}

func requestRaw(t *testing.T, router http.Handler, method, path, token, idem string, body any) rawResponse {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-Id", "trace-account-project")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode raw response: %v body=%s", err, rec.Body.String())
	}
	return rawResponse{Code: rec.Code, Body: out}
}

func requestJSON(t *testing.T, router http.Handler, method, path, token, idem string, body any) map[string]any {
	t.Helper()
	resp := requestRaw(t, router, method, path, token, idem, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%#v", method, path, resp.Code, resp.Body)
	}
	return resp.Body
}

func ptr(value string) *string {
	return &value
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
