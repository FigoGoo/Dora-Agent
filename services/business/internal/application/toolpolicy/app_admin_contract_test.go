package toolpolicy

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

func newToolPolicyContractApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_tool_contract")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	return New(businesscore.New(db.DB))
}

func TestAdminToolDTOAndImpactPreviewExposeOpenAPIFields(t *testing.T) {
	app := newToolPolicyContractApp(t)
	now := time.Now().UTC()
	adminID := "adm_root"
	emptyObject := datatypes.JSON([]byte("{}"))
	rows := []any{
		&businesscore.ToolDefinition{
			ID: "td_contract", ToolName: "tool_contract", ToolType: "builtin", DisplayName: "Tool Contract",
			Status: "active", Version: "1.0.0", InputSchemaJSON: emptyObject, OutputSchemaJSON: emptyObject,
			CreatedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.ToolPolicy{
			ID: "tp_contract", ToolName: "tool_contract", ToolType: "builtin", PolicyScope: "global",
			Allowed: true, RiskLevel: "medium", RequiresConfirmation: true, TimeoutMS: 30000,
			RetryPolicyJSON: emptyObject, CancelPolicyJSON: emptyObject, Status: "active",
			EffectiveAt: now.Add(-time.Hour), ChangedByAdminID: &adminID, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.Skill{
			ID: "skl_tool_contract", SkillKey: "tool-contract", SkillName: "Tool Contract Skill",
			SkillScope: "public", Status: "published", RouteHintsJSON: emptyObject, CreatedByAdminID: &adminID,
			CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.SkillVersion{
			ID: "skv_tool_contract", SkillID: "skl_tool_contract", Version: "1.0.0", Status: "published",
			SkillSpecJSON: emptyObject, InputSchemaJSON: emptyObject, OutputSchemaJSON: emptyObject,
			MemoryPolicyJSON: emptyObject, ConfirmationPolicyJSON: emptyObject, CreatedAt: now, UpdatedAt: now,
		},
		&businesscore.SkillToolBinding{
			ID: "stb_tool_contract", SkillID: "skl_tool_contract", VersionID: "skv_tool_contract",
			ToolName: "tool_contract", ToolType: "builtin", Required: true, CreatedAt: now,
		},
	}
	for _, row := range rows {
		if err := app.repo.DB().WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	list, err := app.ListAdminTools(t.Context(), admin.AdminAuth{AdminID: adminID}, "active", 10, 0)
	if err != nil {
		t.Fatalf("list admin tools: %v", err)
	}
	var found *ToolDTO
	for i := range list.Items {
		if list.Items[i].ToolName == "tool_contract" {
			found = &list.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("tool missing from admin list: %#v", list.Items)
	}
	if found.ToolKey != "tool_contract:builtin" {
		t.Fatalf("tool_key=%q", found.ToolKey)
	}

	preview, err := app.ImpactPreview(t.Context(), admin.AdminAuth{AdminID: adminID}, "tool_contract", "builtin")
	if err != nil {
		t.Fatalf("impact preview: %v", err)
	}
	if preview["tool_key"] != "tool_contract:builtin" || preview["impact_count"] != int64(1) {
		t.Fatalf("openapi preview fields mismatch: %#v", preview)
	}
}
