package skillcatalog

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

var emptyJSON = datatypes.JSON([]byte("{}"))

func newSkillTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_skill_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo)
}

// ACCT-1b：绑定 Tool 被当前企业/空间白名单显式禁用(allowed=false)的 Skill 不可路由；
// 未被显式禁用的 Skill 正常路由，避免把"无规则=默认允许"误判为不可用。
func TestRoutableSkillsFiltersWhitelistDeniedTools(t *testing.T) {
	app := newSkillTestApp(t)
	db := app.repo.DB().WithContext(t.Context())
	entID := "ent_acct1b"
	now := time.Now().UTC()
	ptr := func(s string) *string { return &s }

	mkSkill := func(id, versionID string) *businesscore.Skill {
		return &businesscore.Skill{
			ID: id, SkillKey: id, SkillName: id, SkillScope: "public", Status: "published",
			PublishedVersionID: ptr(versionID), EnterpriseID: ptr(entID), RouteHintsJSON: emptyJSON,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	mkVersion := func(id, skillID string) *businesscore.SkillVersion {
		return &businesscore.SkillVersion{
			ID: id, SkillID: skillID, Version: "1.0.0", Status: "published",
			SkillSpecJSON: emptyJSON, InputSchemaJSON: emptyJSON, OutputSchemaJSON: emptyJSON,
			MemoryPolicyJSON: emptyJSON, ConfirmationPolicyJSON: emptyJSON,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	mkBinding := func(id, skillID, versionID, toolName string) *businesscore.SkillToolBinding {
		return &businesscore.SkillToolBinding{
			ID: id, SkillID: skillID, VersionID: versionID, ToolName: toolName, ToolType: "builtin",
			Required: true, CreatedAt: now,
		}
	}

	for _, row := range []any{
		mkSkill("skl_ok_acct1b", "skv_ok_acct1b"),
		mkSkill("skl_blocked_acct1b", "skv_blocked_acct1b"),
		mkVersion("skv_ok_acct1b", "skl_ok_acct1b"),
		mkVersion("skv_blocked_acct1b", "skl_blocked_acct1b"),
		mkBinding("stb_ok_acct1b", "skl_ok_acct1b", "skv_ok_acct1b", "tool_safe_acct1b"),
		mkBinding("stb_blocked_acct1b", "skl_blocked_acct1b", "skv_blocked_acct1b", "tool_banned_acct1b"),
		&businesscore.ToolWhitelistRule{
			ID: "twr_deny_acct1b", ToolName: "tool_banned_acct1b", ToolType: "builtin",
			ScopeType: "enterprise", ScopeID: entID, Allowed: false, Status: "active",
			CreatedAt: now, UpdatedAt: now,
		},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed fixture %T: %v", row, err)
		}
	}

	idSet := func(items []SkillSummaryDTO) map[string]bool {
		m := make(map[string]bool, len(items))
		for _, it := range items {
			m[it.SkillID] = true
		}
		return m
	}

	// 企业身份：绑定被企业白名单禁用 Tool 的 Skill 必须被过滤掉。
	entAuth := accountspace.AuthContext{UserID: "usr_acct1b", SpaceID: "sp_enterprise_acct1b", EnterpriseID: entID, LoginIdentityType: "enterprise"}
	entItems, _, err := app.ListRoutableSkills(t.Context(), entAuth, "", 50, "")
	if err != nil {
		t.Fatalf("enterprise list routable: %v", err)
	}
	entIDs := idSet(entItems)
	if !entIDs["skl_ok_acct1b"] {
		t.Fatalf("未被禁用的 Skill 应可路由，got=%#v", entIDs)
	}
	if entIDs["skl_blocked_acct1b"] {
		t.Fatalf("绑定被企业白名单禁用 Tool 的 Skill 不应路由(ACCT-1b)，got=%#v", entIDs)
	}

	// 个人身份(无企业/空间白名单)：仅"显式禁用"才拦，默认允许的 Skill 不得被误杀。
	personalAuth := accountspace.AuthContext{UserID: "usr_acct1b", LoginIdentityType: "personal"}
	personalItems, _, err := app.ListRoutableSkills(t.Context(), personalAuth, "", 50, "")
	if err != nil {
		t.Fatalf("personal list routable: %v", err)
	}
	if !idSet(personalItems)["skl_blocked_acct1b"] {
		t.Fatalf("无白名单作用域时不得误杀 Skill，got=%#v", idSet(personalItems))
	}
}
