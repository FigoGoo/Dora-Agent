package skillcatalog

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

// SKILL-2 FP3：GetPublishedSkillSpec 装配输出元素结构(按 display_order 排序)，
// 使 agent 能拿到 Skill 完整定义、按结构组织草稿/最终产物。
func TestGetPublishedSkillSpecIncludesOutputElements(t *testing.T) {
	app := newSkillTestApp(t)
	db := app.repo.DB().WithContext(t.Context())
	now := time.Now().UTC()
	ptr := func(s string) *string { return &s }

	skill := &businesscore.Skill{
		ID: "skl_fp3", SkillKey: "fp3", SkillName: "FP3", SkillScope: "public", Status: "published",
		PublishedVersionID: ptr("skv_fp3"), RouteHintsJSON: emptyJSON, CreatedAt: now, UpdatedAt: now,
	}
	sv := &businesscore.SkillVersion{
		ID: "skv_fp3", SkillID: "skl_fp3", Version: "1.0.0", Status: "published",
		SkillSpecJSON: emptyJSON, InputSchemaJSON: emptyJSON, OutputSchemaJSON: emptyJSON,
		MemoryPolicyJSON: emptyJSON, ConfirmationPolicyJSON: emptyJSON, CreatedAt: now, UpdatedAt: now,
	}
	lyrics := &businesscore.SkillOutputElementSchema{
		ID: "soe_fp3_lyrics", SkillID: "skl_fp3", VersionID: "skv_fp3", ElementType: "skill2.lyrics", ElementName: "歌词",
		SchemaJSON: emptyJSON, Required: true, DisplayOrder: 2, DisplaySlot: "asset_detail", UseDraft: false, UseFinal: true, CreatedAt: now,
	}
	title := &businesscore.SkillOutputElementSchema{
		ID: "soe_fp3_title", SkillID: "skl_fp3", VersionID: "skv_fp3", ElementType: "skill2.title", ElementName: "标题",
		SchemaJSON: emptyJSON, Required: true, DisplayOrder: 1, DisplaySlot: "blackboard", UseDraft: true, UseFinal: true, Editable: true, CreatedAt: now,
	}
	for _, row := range []any{skill, sv, lyrics, title} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	auth := accountspace.AuthContext{UserID: "usr_fp3", LoginIdentityType: "personal"}
	spec, err := app.GetPublishedSkillSpec(t.Context(), auth, "skl_fp3", "")
	if err != nil {
		t.Fatalf("get published skill spec: %v", err)
	}
	if len(spec.OutputElements) != 2 {
		t.Fatalf("应含 2 个输出元素，got %d", len(spec.OutputElements))
	}
	// display_order 升序：title(1) 在 lyrics(2) 之前。
	if spec.OutputElements[0].ElementType != "skill2.title" || !spec.OutputElements[0].UseDraft || !spec.OutputElements[0].Editable {
		t.Fatalf("第一个应为 title 且草稿态/可编辑: %#v", spec.OutputElements[0])
	}
	if spec.OutputElements[1].ElementType != "skill2.lyrics" || spec.OutputElements[1].UseDraft || !spec.OutputElements[1].UseFinal {
		t.Fatalf("第二个应为 lyrics 且仅最终态: %#v", spec.OutputElements[1])
	}
}
