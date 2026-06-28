package admin

import (
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"gorm.io/gorm"
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

func TestAdminUserStatusChangeDoesNotMutateUserPassword(t *testing.T) {
	app := newAdminTestApp(t)
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	auth := AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session"}
	var before businesscore.User
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", "usr_1001").First(&before).Error; err != nil {
		t.Fatalf("load user before status change: %v", err)
	}
	preview, err := app.PreviewSetUserStatus(t.Context(), UserStatusInput{
		Auth: auth, UserID: "usr_1001", TargetStatus: "disabled", Reason: "security review",
		Meta: RequestMeta{TraceID: "trace-user-status-preview", IdempotencyKey: "idem-user-status-preview"},
	})
	if err != nil {
		t.Fatalf("preview set user status: %v", err)
	}
	_, err = app.ConfirmSetUserStatus(t.Context(), UserStatusInput{
		Auth: auth, UserID: "usr_1001", TargetStatus: "disabled", PreviewToken: preview.PreviewToken, Reason: "security review",
		Meta: RequestMeta{TraceID: "trace-user-status-confirm", IdempotencyKey: "idem-user-status-confirm"},
	})
	if err != nil {
		t.Fatalf("confirm set user status: %v", err)
	}
	var after businesscore.User
	if err := app.repo.DB().WithContext(t.Context()).Where("id = ?", "usr_1001").First(&after).Error; err != nil {
		t.Fatalf("load user after status change: %v", err)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Fatalf("WORK-8 redline violated: admin user status flow mutated user password hash")
	}
}

func TestDisableAdminCannotRemoveLastActiveAdmin(t *testing.T) {
	app := newAdminTestApp(t)
	err := app.repo.DB().WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		_, err := app.lockDisableTargetAdmin(tx, "adm_root")
		return err
	})
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected last active admin guard, got %v", err)
	}
}

func TestDisableAdminAllowsNonLastActiveAdmin(t *testing.T) {
	app := newAdminTestApp(t)
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	auth := AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session"}
	created, err := app.CreateAdmin(t.Context(), CreateAdminInput{
		Auth: auth, Account: "second.admin@dora.local", InitialPassword: "Passw0rd!Second", Reason: "test second admin",
		Meta: RequestMeta{TraceID: "trace-admin-create", IdempotencyKey: "idem-admin-create-second"},
	})
	if err != nil {
		t.Fatalf("create second admin: %v", err)
	}
	disabled, err := app.DisableAdmin(t.Context(), DisableAdminInput{
		Auth: auth, AdminID: created.AdminID, Reason: "test disable non-last",
		Meta: RequestMeta{TraceID: "trace-admin-disable", IdempotencyKey: "idem-admin-disable-second"},
	})
	if err != nil {
		t.Fatalf("disable non-last admin: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("admin not disabled: %#v", disabled)
	}
	var activeCount int64
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).Where("status = ?", accountspace.StatusActive).Count(&activeCount).Error; err != nil {
		t.Fatalf("count active admins: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active admin remaining, got %d", activeCount)
	}
}

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return businessErr.Code
	}
	return ""
}
