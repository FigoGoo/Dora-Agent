package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

func TestAdminPaginationQueryAcceptsOpenAPIAndLegacyParams(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		wantLimit  int
		wantOffset int
	}{
		{name: "openapi page size and token", target: "/api/admin/users?page_size=20&page_token=40", wantLimit: 20, wantOffset: 40},
		{name: "legacy limit and offset", target: "/api/admin/users?limit=30&offset=60", wantLimit: 30, wantOffset: 60},
		{name: "legacy limit wins while page token supplies offset", target: "/api/admin/users?page_size=20&limit=50&page_token=70", wantLimit: 50, wantOffset: 70},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(nethttp.MethodGet, tc.target, nil)

			if got := adminPageLimit(ctx, 10); got != tc.wantLimit {
				t.Fatalf("limit=%d want=%d", got, tc.wantLimit)
			}
			if got := adminPageOffset(ctx); got != tc.wantOffset {
				t.Fatalf("offset=%d want=%d", got, tc.wantOffset)
			}
		})
	}
}

func TestAdminAuthenticatedRequestReturnsRenewedSessionExpiryHeader(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_admin_session_header")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	if err := repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	adminApp := admin.New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	router := NewRouter(RouterOptions{Admin: adminApp})
	token := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")

	req := httptest.NewRequest(nethttp.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", rec.Code, rec.Body.String())
	}
	rawExpiry := rec.Header().Get("X-Admin-Session-Expires-At")
	if rawExpiry == "" {
		t.Fatalf("expected renewed admin session expiry header")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, rawExpiry)
	if err != nil {
		t.Fatalf("parse expiry header %q: %v", rawExpiry, err)
	}
	if time.Until(expiresAt) < 6*24*time.Hour {
		t.Fatalf("renewed admin session expiry is too soon: %s", expiresAt)
	}
}

func TestAdminSkillReviewRequestAcceptsOpenAPIDecisionFields(t *testing.T) {
	req := adminSkillReviewRequest{Decision: "reject", Reason: "证据不足"}

	if got := req.normalizedAction(); got != "reject" {
		t.Fatalf("normalized action=%q", got)
	}
	if got := req.normalizedComment(); got != "证据不足" {
		t.Fatalf("normalized comment=%q", got)
	}
}
