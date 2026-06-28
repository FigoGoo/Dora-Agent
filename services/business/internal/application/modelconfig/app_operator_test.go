package modelconfig

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestModelConfigOperatorColumnsAreFilledFromAdminAuth(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_modelconfig_operator")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	app := New(businesscore.New(db.DB))
	auth := admin.AdminAuth{AdminID: "adm_root"}

	if _, err := app.SaveProvider(t.Context(), SaveProviderInput{
		Auth: auth, ProviderID: "mp_operator", ProviderCode: "operator-provider", DisplayName: "Operator Provider",
		ProviderType: "openai_compatible", Status: activeStatus, BaseURL: "http://localhost:18081", Config: map[string]any{"timeout_ms": 45000},
	}); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	requireOperatorColumns(t, app, "model_providers", "id = ?", "adm_root", "adm_root", "mp_operator")

	if _, err := app.SetProviderStatus(t.Context(), auth, "mp_operator", "disabled"); err != nil {
		t.Fatalf("set provider status: %v", err)
	}
	requireOperatorColumns(t, app, "model_providers", "id = ?", "adm_root", "adm_root", "mp_operator")

	if _, err := app.SaveModel(t.Context(), SaveModelInput{
		Auth: auth, ModelID: "mdl_operator_image", ProviderID: "mp_seed", ModelCode: "operator-image",
		DisplayName: "Operator Image Model", ResourceType: "image", Status: activeStatus, CapabilityTags: []string{"image_generation"},
		RouteConfig: map[string]any{"route": "local"}, CredentialID: "mpc_seed",
	}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	requireOperatorColumns(t, app, "models", "id = ?", "adm_root", "adm_root", "mdl_operator_image")

	if _, err := app.SetModelStatus(t.Context(), auth, "mdl_operator_image", "disabled"); err != nil {
		t.Fatalf("set model status: %v", err)
	}
	requireOperatorColumns(t, app, "models", "id = ?", "adm_root", "adm_root", "mdl_operator_image")

	if _, err := app.SetDefaultModel(t.Context(), auth, "image", "mdl_seed_image", "price_model_image_seed"); err != nil {
		t.Fatalf("set default model: %v", err)
	}
	requireOperatorColumns(t, app, "default_models", "resource_type = ? AND scope = ? AND status = ?", "adm_root", "adm_root", "image", "global", activeStatus)
	requireOperatorColumns(t, app, "default_models", "id = ?", "", "adm_root", "dm_seed_image")
}

func requireOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
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
	if value(row.CreatedBy) != wantCreatedBy || value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
