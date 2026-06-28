package assetdict

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

func newAssetdictTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_assetdict_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	repo := businesscore.New(db.DB)
	return New(repo)
}

// SKILL-2 FP1：字典上限读取——双态/编辑/引用上限来自 schema_json 内嵌字段，
// 未声明默认放开、显式 false 收紧、inactive 反映 Active=false、缺失项不出现在结果。
func TestElementTypeLimitsReflectsDictionaryCaps(t *testing.T) {
	app := newAssetdictTestApp(t)
	db := app.repo.DB().WithContext(t.Context())
	now := time.Now().UTC()
	mk := func(elementType, schema, status string) *businesscore.AssetElementType {
		return &businesscore.AssetElementType{
			ID: "aet_" + elementType, ElementType: elementType, DisplayName: elementType,
			SchemaVersion: "2026-06-28", SchemaJSON: datatypes.JSON([]byte(schema)), Status: status,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	for _, row := range []any{
		mk("skill2.fp1.full", "{}", "active"),
		mk("skill2.fp1.limited", `{"draft_enabled":false,"editable":false}`, "active"),
		mk("skill2.fp1.inactive", "{}", "inactive"),
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	limits, err := app.ElementTypeLimits(t.Context(), []string{
		"skill2.fp1.full", "skill2.fp1.limited", "skill2.fp1.inactive", "skill2.fp1.missing",
	})
	if err != nil {
		t.Fatalf("element type limits: %v", err)
	}

	full, ok := limits["skill2.fp1.full"]
	if !ok || !full.Active || !full.DraftEnabled || !full.FinalEnabled || !full.Editable || !full.Referable {
		t.Fatalf("默认无声明应全放开且 active，got=%#v ok=%v", full, ok)
	}
	limited := limits["skill2.fp1.limited"]
	if !limited.Active || limited.DraftEnabled || !limited.FinalEnabled || limited.Editable || !limited.Referable {
		t.Fatalf("显式 false 应收紧、未声明仍放开，got=%#v", limited)
	}
	inactive, ok := limits["skill2.fp1.inactive"]
	if !ok || inactive.Active {
		t.Fatalf("inactive 字典项 Active 应为 false，got=%#v ok=%v", inactive, ok)
	}
	if _, ok := limits["skill2.fp1.missing"]; ok {
		t.Fatalf("不存在的 element_type 不应出现在结果中")
	}
}
