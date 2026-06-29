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
		&businesscore.ToolPricingPolicy{
			ID: "tprice_contract", PricingPolicyID: "tool_price_contract", ToolName: "tool_contract", ToolType: "builtin",
			ChargeMode: "per_call", BillingUnit: "call", UnitPoints: 9, FreeQuota: 2, MinChargePoints: 1,
			Status: "active", EffectiveAt: now.Add(-time.Hour), ChangedByAdminID: &adminID, MetadataJSON: emptyObject,
			CreatedAt: now, UpdatedAt: now,
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
	if found.PricingPolicyID != "tool_price_contract" || found.UnitPoints != 9 || found.FreeQuota != 2 || found.MinChargePoints != 1 {
		t.Fatalf("pricing fields should be exposed for admin tool management: %#v", found)
	}
	if found.RiskLevel != "medium" || !found.RequiresConfirmation || found.TimeoutMS != 30000 {
		t.Fatalf("policy fields should be exposed for admin tool management: %#v", found)
	}

	disabled, err := app.SetToolStatus(t.Context(), admin.AdminAuth{AdminID: adminID}, "tool_contract", "builtin", "disabled")
	if err != nil {
		t.Fatalf("disable tool: %v", err)
	}
	if disabled.Status != "disabled" || disabled.RiskLevel != "medium" || disabled.TimeoutMS != 30000 {
		t.Fatalf("disabled admin dto should keep configured policy fields: %#v", disabled)
	}

	preview, err := app.ImpactPreview(t.Context(), admin.AdminAuth{AdminID: adminID}, "tool_contract", "builtin")
	if err != nil {
		t.Fatalf("impact preview: %v", err)
	}
	if preview["tool_key"] != "tool_contract:builtin" || preview["impact_count"] != int64(1) {
		t.Fatalf("openapi preview fields mismatch: %#v", preview)
	}
	affectedSkills, ok := preview["affected_skills"].([]AffectedSkillDTO)
	if !ok || len(affectedSkills) != 1 || affectedSkills[0].SkillName != "Tool Contract Skill" || affectedSkills[0].Status != "published" {
		t.Fatalf("affected skill summaries missing: %#v", preview["affected_skills"])
	}
}

func TestRegisterToolCreatesMetadataPolicyAndPricing(t *testing.T) {
	app := newToolPolicyContractApp(t)
	adminID := "adm_root"
	registered, err := app.RegisterTool(t.Context(), RegisterToolInput{
		Auth:                 admin.AdminAuth{AdminID: adminID},
		ToolName:             "storyboard_extract",
		ToolType:             "builtin",
		DisplayName:          "分镜提取",
		Description:          "从剧本文本中提取镜头、人物和场景信息，适合视频类 Skill 生成故事板前调用。",
		Version:              "1.0.0",
		InputSchemaJSON:      `{"type":"object","properties":{"script":{"type":"string"}}}`,
		OutputSchemaJSON:     `{"type":"object","properties":{"shots":{"type":"array"}}}`,
		Allowed:              true,
		RiskLevel:            "medium",
		RequiresConfirmation: true,
		TimeoutMS:            45000,
		RetryPolicy:          map[string]string{"max_retries": "1"},
		CancelPolicy:         map[string]string{"cancelable": "true"},
		ChargeMode:           "per_call",
		BillingUnit:          "call",
		UnitPoints:           5,
		FreeQuota:            1,
		MinChargePoints:      1,
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}
	if registered.ToolKey != "storyboard_extract:builtin" || registered.Description == "" {
		t.Fatalf("registered tool metadata mismatch: %#v", registered)
	}
	if !registered.Allowed || registered.RiskLevel != "medium" || !registered.RequiresConfirmation || registered.TimeoutMS != 45000 {
		t.Fatalf("registered tool policy mismatch: %#v", registered)
	}
	if registered.ChargeMode != "per_call" || registered.UnitPoints != 5 || registered.FreeQuota != 1 || registered.MinChargePoints != 1 {
		t.Fatalf("registered tool pricing mismatch: %#v", registered)
	}
	if registered.PricingPolicyID == "" {
		t.Fatalf("pricing policy id should be generated: %#v", registered)
	}

	var changeCount int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.ToolPolicyChangeRecord{}).
		Where("tool_name = ? AND tool_type = ? AND change_type = ?", "storyboard_extract", "builtin", "tool.register").
		Count(&changeCount).Error; err != nil {
		t.Fatalf("count change records: %v", err)
	}
	if changeCount != 1 {
		t.Fatalf("expected one register change record, got %d", changeCount)
	}
}
