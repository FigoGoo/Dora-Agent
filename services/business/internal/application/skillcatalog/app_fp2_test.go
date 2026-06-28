package skillcatalog

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

// SKILL-2 FP2：SaveSkill 持久化 per-skill 输出元素结构，并强制「类型须平台内置 active、
// 双态/编辑/引用不得超字典上限、至少一态」。
func TestSaveSkillPersistsAndValidatesOutputElements(t *testing.T) {
	app := newSkillTestApp(t)
	app.SetDictionary(assetdict.New(app.repo))
	db := app.repo.DB().WithContext(t.Context())
	now := time.Now().UTC()

	mkType := func(et, schema string) *businesscore.AssetElementType {
		return &businesscore.AssetElementType{
			ID: "aet_" + et, ElementType: et, DisplayName: et, SchemaVersion: "2026-06-28",
			SchemaJSON: datatypes.JSON([]byte(schema)), Status: "active", CreatedAt: now, UpdatedAt: now,
		}
	}
	// title 全开；lyrics 仅最终态(草稿禁、可编辑禁)。
	for _, row := range []any{
		mkType("skill2.title", "{}"),
		mkType("skill2.lyrics", `{"draft_enabled":false,"editable":false}`),
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed dict: %v", err)
		}
	}

	auth := accountspace.AuthContext{UserID: "usr_fp2", LoginIdentityType: "personal"}
	base := SaveSkillInput{Auth: auth, SkillName: "FP2 Skill", SkillScope: "personal", Version: "0.1.0"}

	// 1. 正常写入（含双态差异）。
	ok := base
	ok.SkillKey = "sk_fp2_ok"
	ok.OutputElements = []OutputElementInput{
		{ElementType: "skill2.title", ElementName: "标题", Required: true, UseDraft: true, UseFinal: true, Editable: true, DisplayOrder: 1, DisplaySlot: "asset_detail"},
		{ElementType: "skill2.lyrics", ElementName: "歌词", Required: true, UseDraft: false, UseFinal: true},
	}
	detail, err := app.SaveSkill(t.Context(), ok)
	if err != nil {
		t.Fatalf("save skill: %v", err)
	}
	var rows []businesscore.SkillOutputElementSchema
	if err := db.Where("skill_id = ?", detail.SkillID).Order("element_type ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load elements: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应写入 2 个输出元素，got %d", len(rows))
	}
	lyrics := rows[0] // element_type ASC → skill2.lyrics 在前
	if lyrics.ElementType != "skill2.lyrics" || lyrics.UseDraft || !lyrics.UseFinal || lyrics.Editable || lyrics.ElementName != "歌词" || lyrics.DisplaySlot != "blackboard" {
		t.Fatalf("lyrics 双态/默认槽位落库不符: %#v", lyrics)
	}
	title := rows[1]
	if !title.UseDraft || !title.UseFinal || !title.Editable || title.DisplaySlot != "asset_detail" {
		t.Fatalf("title 字段落库不符: %#v", title)
	}

	// 2. 越字典上限：lyrics editable=true 超字典(editable=false) → 拒绝。
	badEditable := base
	badEditable.SkillKey = "sk_fp2_bad_editable"
	badEditable.OutputElements = []OutputElementInput{{ElementType: "skill2.lyrics", ElementName: "歌词", UseFinal: true, Editable: true}}
	if _, err := app.SaveSkill(t.Context(), badEditable); err == nil {
		t.Fatal("editable 超字典上限应被拒绝")
	}

	// 3. 非平台内置类型 → 拒绝。
	badType := base
	badType.SkillKey = "sk_fp2_bad_type"
	badType.OutputElements = []OutputElementInput{{ElementType: "skill2.unknown", UseFinal: true}}
	if _, err := app.SaveSkill(t.Context(), badType); err == nil {
		t.Fatal("非平台内置类型应被拒绝")
	}

	// 4. 两态都关 → 拒绝。
	badStage := base
	badStage.SkillKey = "sk_fp2_bad_stage"
	badStage.OutputElements = []OutputElementInput{{ElementType: "skill2.title"}}
	if _, err := app.SaveSkill(t.Context(), badStage); err == nil {
		t.Fatal("草稿/最终两态都关应被拒绝")
	}
}
