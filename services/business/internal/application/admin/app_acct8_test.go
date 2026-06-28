package admin

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func newAdminTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_admin_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
}

// ACCT-8 管理员越权红线：平台管理通道(GetUserSummary)只读用户平台级元数据，
// 即便目标用户拥有业务空间归属(个人/企业空间、企业成员)，也不得展开归属明细。
// 固化"管理员不得以 admin 身份跨入业务空间归属"，防止未来误把业务数据 join 进管理通道。
func TestAdminUserDetailDoesNotExposeBusinessOwnership(t *testing.T) {
	app := newAdminTestApp(t)
	// 种子 adm_root 默认 must_rotate；本测试只验证归属红线，置否以通过 requireAdmin。
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	auth := AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session"}

	// usr_1001 在 seed 中拥有 sp_personal_1001 / sp_enterprise_1001 与 ent_1001 owner 归属。
	detail, err := app.GetUserSummary(t.Context(), auth, "usr_1001")
	if err != nil {
		t.Fatalf("get user summary: %v", err)
	}
	if detail.Summary.UserID != "usr_1001" {
		t.Fatalf("应取到目标用户平台元数据，got=%#v", detail.Summary)
	}
	if len(detail.Spaces) != 0 || len(detail.EnterpriseMemberships) != 0 || len(detail.RecentAuditRefs) != 0 {
		t.Fatalf("ACCT-8 越权红线：admin 通道不得暴露业务空间归属明细，got spaces=%d members=%d audit=%d",
			len(detail.Spaces), len(detail.EnterpriseMemberships), len(detail.RecentAuditRefs))
	}
}
