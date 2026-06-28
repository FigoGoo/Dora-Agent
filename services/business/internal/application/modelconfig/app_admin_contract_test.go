package modelconfig

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

func newModelConfigContractApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_model_contract")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	return New(businesscore.New(db.DB))
}

func TestAdminProviderDTOExposesOpenAPIAliases(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Now().UTC()
	auth := admin.AdminAuth{AdminID: "adm_root"}
	config := map[string]any{"secret_key_ref": "secret/model/provider"}

	created, err := app.SaveProvider(t.Context(), SaveProviderInput{
		Auth: auth, ProviderID: "mp_contract_alias", ProviderCode: "openai_contract_alias",
		DisplayName: "OpenAI Contract", ProviderType: "openai_compatible", Status: "active",
		Config: config,
	})
	if err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if created.ProviderName != "OpenAI Contract" {
		t.Fatalf("provider_name alias=%q", created.ProviderName)
	}
	if created.SecretRefStatus != "configured" {
		t.Fatalf("secret_ref_status=%q", created.SecretRefStatus)
	}
	if created.UpdatedAt.Before(now) {
		t.Fatalf("updated_at should be set, got %s", created.UpdatedAt)
	}
}

func TestSetDefaultModelInfersActivePricingSnapshotWhenOmitted(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Date(2026, 6, 29, 8, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	auth := admin.AdminAuth{AdminID: "adm_root"}

	adminID := auth.AdminID
	rows := []any{
		&businesscore.ModelProvider{
			ID: "mp_contract_default", ProviderCode: "provider_contract_default", DisplayName: "Provider Contract",
			ProviderType: "openai_compatible", Status: "active", ConfigJSON: datatypes.JSON([]byte("{}")),
			CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Model{
			ID: "mdl_contract_default", ProviderID: "mp_contract_default", ModelCode: "model_contract_default",
			DisplayName: "Model Contract", ResourceType: "image", CapabilityTags: datatypes.JSON([]byte("[]")),
			Status: "active", RouteConfigJSON: datatypes.JSON([]byte("{}")), CreatedByAdminID: &adminID,
			CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.ModelPrice{
			ID: "mprice_contract_default", PricingSnapshotID: "price_contract_default", ModelID: "mdl_contract_default",
			ResourceType: "image", BillingUnit: "image", UnitPoints: 1, MinChargePoints: 1,
			Status: "active", EffectiveAt: now.Add(-time.Hour), CreatedByAdminID: &adminID, CreatedAt: now,
		},
	}
	for _, row := range rows {
		if err := app.repo.DB().WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	out, err := app.SetDefaultModel(t.Context(), auth, "image", "mdl_contract_default", "")
	if err != nil {
		t.Fatalf("set default model: %v", err)
	}
	if out.PricingSnapshotID != "price_contract_default" {
		t.Fatalf("pricing_snapshot_id=%q", out.PricingSnapshotID)
	}

	list, err := app.ListModels(t.Context(), auth, "image", "", 10, 0)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	var found *ModelAdminDTO
	for i := range list.Items {
		if list.Items[i].ModelID == "mdl_contract_default" {
			found = &list.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("model missing from admin list: %#v", list.Items)
	}
	if !found.IsDefault || !found.DefaultForResource {
		t.Fatalf("default aliases not set: %#v", *found)
	}
}
