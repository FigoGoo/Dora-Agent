package modelconfig

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
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

	list, err := app.ListModels(t.Context(), auth, "", "image", "", 10, 0)
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
	filtered, err := app.ListModels(t.Context(), auth, found.ProviderID, "image", "", 10, 0)
	if err != nil {
		t.Fatalf("list models by provider: %v", err)
	}
	if len(filtered.Items) == 0 {
		t.Fatalf("expected models for provider %q", found.ProviderID)
	}
	for _, item := range filtered.Items {
		if item.ProviderID != found.ProviderID {
			t.Fatalf("provider filter leaked model: %#v", item)
		}
	}
}

func TestSetDefaultModelCreatesFreePricingSnapshotWhenMissing(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	auth := admin.AdminAuth{AdminID: "adm_root"}
	adminID := auth.AdminID

	rows := []any{
		&businesscore.ModelProvider{
			ID: "mp_contract_free_default", ProviderCode: "provider_contract_free_default", DisplayName: "Provider Free Default",
			ProviderType: "openai_compatible", Status: "active", ConfigJSON: datatypes.JSON([]byte("{}")),
			CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Model{
			ID: "mdl_contract_free_default", ProviderID: "mp_contract_free_default", ModelCode: "model_contract_free_default",
			DisplayName: "Model Free Default", ResourceType: "text", CapabilityTags: datatypes.JSON([]byte("[]")),
			Status: "active", RouteConfigJSON: datatypes.JSON([]byte("{}")), CreatedByAdminID: &adminID,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, row := range rows {
		if err := app.repo.DB().WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	out, err := app.SetDefaultModel(t.Context(), auth, "text", "mdl_contract_free_default", "")
	if err != nil {
		t.Fatalf("set default model without price: %v", err)
	}
	if out.PricingSnapshotID == "" {
		t.Fatalf("expected generated pricing snapshot id: %#v", out)
	}

	var price businesscore.ModelPrice
	if err := app.repo.DB().WithContext(t.Context()).
		Where("pricing_snapshot_id = ? AND model_id = ?", out.PricingSnapshotID, "mdl_contract_free_default").
		First(&price).Error; err != nil {
		t.Fatalf("generated price missing: %v", err)
	}
	if price.BillingUnit != "token" || price.UnitPoints != 0 || price.MinChargePoints != 0 {
		t.Fatalf("unexpected generated price: %#v", price)
	}
}

func TestSetDefaultModelCanBeRepeatedWithoutUniqueConflict(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	auth := admin.AdminAuth{AdminID: "adm_root"}
	adminID := auth.AdminID

	rows := []any{
		&businesscore.ModelProvider{
			ID: "mp_contract_repeat_default", ProviderCode: "provider_contract_repeat_default", DisplayName: "Provider Repeat Default",
			ProviderType: "openai_compatible", Status: "active", ConfigJSON: datatypes.JSON([]byte("{}")),
			CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Model{
			ID: "mdl_contract_repeat_default_a", ProviderID: "mp_contract_repeat_default", ModelCode: "model_repeat_a",
			DisplayName: "Model Repeat A", ResourceType: "text", CapabilityTags: datatypes.JSON([]byte("[]")),
			Status: "active", RouteConfigJSON: datatypes.JSON([]byte("{}")), CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Model{
			ID: "mdl_contract_repeat_default_b", ProviderID: "mp_contract_repeat_default", ModelCode: "model_repeat_b",
			DisplayName: "Model Repeat B", ResourceType: "text", CapabilityTags: datatypes.JSON([]byte("[]")),
			Status: "active", RouteConfigJSON: datatypes.JSON([]byte("{}")), CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, row := range rows {
		if err := app.repo.DB().WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	if _, err := app.SetDefaultModel(t.Context(), auth, "text", "mdl_contract_repeat_default_a", ""); err != nil {
		t.Fatalf("set default a: %v", err)
	}
	if _, err := app.SetDefaultModel(t.Context(), auth, "text", "mdl_contract_repeat_default_b", ""); err != nil {
		t.Fatalf("set default b: %v", err)
	}
	out, err := app.SetDefaultModel(t.Context(), auth, "text", "mdl_contract_repeat_default_a", "")
	if err != nil {
		t.Fatalf("set default a again: %v", err)
	}
	if out.ModelID != "mdl_contract_repeat_default_a" {
		t.Fatalf("default model=%q", out.ModelID)
	}

	var activeDefaultCount int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.DefaultModel{}).
		Where("resource_type = ? AND scope = ? AND status = ?", "text", "global", activeStatus).
		Count(&activeDefaultCount).Error; err != nil {
		t.Fatalf("count active defaults: %v", err)
	}
	if activeDefaultCount != 1 {
		t.Fatalf("active default count=%d", activeDefaultCount)
	}
}

func TestSetModelStatusRejectsDisablingDefaultModel(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	auth := admin.AdminAuth{AdminID: "adm_root"}
	adminID := auth.AdminID

	rows := []any{
		&businesscore.ModelProvider{
			ID: "mp_contract_default_guard", ProviderCode: "provider_contract_default_guard", DisplayName: "Provider Default Guard",
			ProviderType: "openai_compatible", Status: "active", ConfigJSON: datatypes.JSON([]byte("{}")),
			CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Model{
			ID: "mdl_contract_default_guard", ProviderID: "mp_contract_default_guard", ModelCode: "model_default_guard",
			DisplayName: "Model Default Guard", ResourceType: "image", CapabilityTags: datatypes.JSON([]byte("[]")),
			Status: "active", RouteConfigJSON: datatypes.JSON([]byte("{}")), CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, row := range rows {
		if err := app.repo.DB().WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
	if _, err := app.SetDefaultModel(t.Context(), auth, "image", "mdl_contract_default_guard", ""); err != nil {
		t.Fatalf("set default model: %v", err)
	}
	if _, err := app.SetModelStatus(t.Context(), auth, "mdl_contract_default_guard", "disabled"); err == nil {
		t.Fatal("expected disabling default model to fail")
	} else if got := bizerrors.FromError(err).Code; got != bizerrors.CodeStateConflict {
		t.Fatalf("error code=%s err=%v", got, err)
	}
}

func TestSaveModelCreatesAndRotatesPricingSnapshot(t *testing.T) {
	app := newModelConfigContractApp(t)
	now := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	auth := admin.AdminAuth{AdminID: "adm_root"}

	created, err := app.SaveModel(t.Context(), SaveModelInput{
		Auth: auth, ModelID: "mdl_contract_price", ProviderID: "mp_seed", ModelCode: "contract-price",
		DisplayName: "Contract Price Model", ResourceType: "image", Status: activeStatus,
		BillingUnit: "image", UnitPoints: 2.5, MinChargePoints: 2,
	})
	if err != nil {
		t.Fatalf("save model with price: %v", err)
	}
	if created.PricingSnapshotID == "" {
		t.Fatalf("expected pricing snapshot id: %#v", created)
	}

	updated, err := app.SaveModel(t.Context(), SaveModelInput{
		Auth: auth, ModelID: "mdl_contract_price", ProviderID: "mp_seed", ModelCode: "contract-price",
		DisplayName: "Contract Price Model", ResourceType: "image", Status: activeStatus,
		PricingSnapshotID: "price_contract_rotated", BillingUnit: "image", UnitPoints: 3, MinChargePoints: 3,
	})
	if err != nil {
		t.Fatalf("rotate model price: %v", err)
	}
	if updated.PricingSnapshotID != "price_contract_rotated" {
		t.Fatalf("pricing snapshot not rotated: %#v", updated)
	}

	var activeCount int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.ModelPrice{}).
		Where("model_id = ? AND resource_type = ? AND status = ?", "mdl_contract_price", "image", activeStatus).
		Count(&activeCount).Error; err != nil {
		t.Fatalf("count active prices: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active price count=%d", activeCount)
	}
}
