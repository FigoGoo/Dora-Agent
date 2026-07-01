package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/smoke"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestB0AdminFeatureFlagFakeProviderAndSmokeHTTP(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_b0_smoke_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	if err := repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	creditApp := credit.New(repo, guard, auditWriter)
	smokeApp := smoke.New(repo, creditApp)
	router := NewRouter(RouterOptions{AccountSpace: accountApp, Admin: adminApp, Credit: creditApp, Smoke: smokeApp})

	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	flags := requestJSON(t, router, http.MethodGet, "/api/admin/feature-flags", adminToken, "", nil)
	if flags["data"].(map[string]any)["total"].(float64) < 8 {
		t.Fatalf("expected B0 feature flags: %#v", flags)
	}
	updated := requestJSON(t, router, http.MethodPost, "/api/admin/feature-flags/paid_marketplace_skill.enabled", adminToken, "idem-b0-flag-update", map[string]any{
		"enabled": true,
	})
	if !updated["data"].(map[string]any)["enabled"].(bool) {
		t.Fatalf("feature flag update failed: %#v", updated)
	}
	tasks := requestJSON(t, router, http.MethodGet, "/api/admin/fake-provider/tasks", adminToken, "", nil)
	if tasks["data"].(map[string]any)["total"].(float64) < 4 {
		t.Fatalf("expected fake provider tasks: %#v", tasks)
	}

	seed := requestJSON(t, router, http.MethodPost, "/api/admin/smoke/seed", adminToken, "idem-b0-smoke-seed", map[string]any{})
	if seed["data"].(map[string]any)["status"] != "passed" {
		t.Fatalf("B0 smoke seed should pass: %#v", seed)
	}
	run := requestJSON(t, router, http.MethodPost, "/api/admin/smoke/runs", adminToken, "idem-b0-smoke-run", map[string]any{
		"suite_key": "b0_business_smoke",
	})
	runData := run["data"].(map[string]any)
	if runData["status"] != "passed" {
		t.Fatalf("B0 smoke suite should pass: %#v", run)
	}
	runID := runData["run_id"].(string)
	result := requestJSON(t, router, http.MethodGet, "/api/admin/smoke/runs/"+runID, adminToken, "", nil)
	if result["data"].(map[string]any)["status"] != "passed" || len(result["data"].(map[string]any)["steps"].([]any)) < 7 {
		t.Fatalf("B0 smoke result mismatch: %#v", result)
	}
}
