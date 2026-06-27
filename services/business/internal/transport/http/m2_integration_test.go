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

func TestM2BusinessHTTPAndRPCIdentityProject(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_m2")
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

	created := requestJSON(t, router, http.MethodPost, "/api/projects", userToken, "idem-project-create", map[string]any{"title": "M2 Project"})
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

func requestJSON(t *testing.T, router http.Handler, method, path, token, idem string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-Id", "trace-m2")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	return out
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
