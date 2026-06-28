package toolpolicy

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestToolPolicyOperatorColumnsAreFilledFromAdminAuth(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_toolpolicy_operator")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	app := New(businesscore.New(db.DB))
	auth := admin.AdminAuth{AdminID: "adm_root"}

	if _, err := app.SetToolStatus(t.Context(), auth, "web_fetch", "browser", "disabled"); err != nil {
		t.Fatalf("set tool status: %v", err)
	}
	requireOperatorColumns(t, app, "tool_definitions", "tool_name = ? AND tool_type = ?", "adm_root", "web_fetch", "browser")

	allowed := true
	timeout := int32(45000)
	if _, err := app.UpdatePolicy(t.Context(), auth, "web_fetch", "browser", &allowed, "high", nil, timeout, nil, nil); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	requireOperatorColumns(t, app, "tool_policies", "tool_name = ? AND tool_type = ? AND policy_scope = ? AND status = ?", "adm_root", "web_fetch", "browser", "global", activeStatus)

	if _, err := app.UpdatePricing(t.Context(), auth, "web_fetch", "browser", "per_call", "call", 5, 0, 0); err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	requireOperatorColumns(t, app, "tool_pricing_policies", "tool_name = ? AND tool_type = ? AND status = ?", "adm_root", "web_fetch", "browser", activeStatus)
	requireOperatorColumns(t, app, "tool_pricing_policies", "tool_name = ? AND tool_type = ? AND status = ?", "adm_root", "web_fetch", "browser", "inactive")

	if _, err := app.SaveWhitelist(t.Context(), auth, "web_fetch", "browser", "space", "sp_personal_1001", false, "operator test"); err != nil {
		t.Fatalf("save whitelist: %v", err)
	}
	requireOperatorColumns(t, app, "tool_whitelist_rules", "tool_name = ? AND tool_type = ? AND scope_type = ? AND scope_id = ?", "adm_root", "web_fetch", "browser", "space", "sp_personal_1001")
}

func requireOperatorColumns(t *testing.T, app *App, table string, where string, wantUpdatedBy string, args ...any) {
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
	if row.UpdatedBy == nil || *row.UpdatedBy != wantUpdatedBy {
		t.Fatalf("unexpected updated_by in %s: created_by=%s updated_by=%s", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
	if row.CreatedBy != nil && *row.CreatedBy != wantUpdatedBy {
		t.Fatalf("unexpected created_by in %s: created_by=%s updated_by=%s", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
