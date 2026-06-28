package admin

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
)

func TestAdminBootstrapPopulatesOperatorColumns(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_admin_operator_bootstrap")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	repo := businesscore.New(db.DB)
	app := New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	passwordHash, err := security.HashPassword("Passw0rd!Bootstrap")
	if err != nil {
		t.Fatalf("hash bootstrap password: %v", err)
	}
	out, err := app.BootstrapInitialAdmin(t.Context(), BootstrapInput{Account: "root.operator@dora.local", PasswordHash: passwordHash, TraceID: "trace-admin-operator-bootstrap"})
	if err != nil {
		t.Fatalf("bootstrap initial admin: %v", err)
	}
	requireAdminOperatorColumns(t, app, "platform_admins", "id = ?", "system_seed", "system_seed", out.AdminID)
	requireAdminOperatorColumns(t, app, "platform_admin_bootstraps", "admin_id = ?", "system_seed", "system_seed", out.AdminID)
}

func TestAdminSessionAndAccountFlowsPopulateOperatorColumns(t *testing.T) {
	app := newAdminTestApp(t)
	rootAuth := AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session"}
	if err := app.repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", rootAuth.AdminID).Updates(map[string]any{"must_rotate_password": false, "updated_by": rootAuth.AdminID}).Error; err != nil {
		t.Fatalf("prep root admin: %v", err)
	}

	session, err := app.Login(t.Context(), AdminLoginInput{
		Account: "admin@dora.local", Password: "local-admin-change-me",
		Meta: RequestMeta{TraceID: "trace-admin-operator-login"},
	})
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	auth, err := app.AuthenticateToken(t.Context(), session.AccessToken)
	if err != nil {
		t.Fatalf("authenticate admin token: %v", err)
	}
	requireAdminOperatorColumns(t, app, "platform_admin_sessions", "id = ?", auth.AdminID, auth.AdminID, auth.SessionID)
	requireAdminUpdatedBy(t, app, "platform_admins", "id = ?", auth.AdminID, auth.AdminID)

	if err := app.Logout(t.Context(), auth, RequestMeta{TraceID: "trace-admin-operator-logout"}); err != nil {
		t.Fatalf("admin logout: %v", err)
	}
	requireAdminOperatorColumns(t, app, "platform_admin_sessions", "id = ?", auth.AdminID, auth.AdminID, auth.SessionID)

	created, err := app.CreateAdmin(t.Context(), CreateAdminInput{
		Auth: rootAuth, Account: "operator-created-admin@dora.local", InitialPassword: "Passw0rd!Created", Reason: "operator create",
		Meta: RequestMeta{TraceID: "trace-admin-operator-create", IdempotencyKey: "idem-admin-operator-create"},
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	requireAdminOperatorColumns(t, app, "platform_admins", "id = ?", rootAuth.AdminID, rootAuth.AdminID, created.AdminID)

	disabled, err := app.DisableAdmin(t.Context(), DisableAdminInput{
		Auth: rootAuth, AdminID: created.AdminID, Reason: "operator disable",
		Meta: RequestMeta{TraceID: "trace-admin-operator-disable", IdempotencyKey: "idem-admin-operator-disable"},
	})
	if err != nil {
		t.Fatalf("disable admin: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("admin not disabled: %#v", disabled)
	}
	requireAdminOperatorColumns(t, app, "platform_admins", "id = ?", rootAuth.AdminID, rootAuth.AdminID, created.AdminID)
}

func TestAdminRotateAndUserStatusPopulateOperatorColumns(t *testing.T) {
	app := newAdminTestApp(t)
	auth := AdminAuth{AdminID: "adm_root", Account: "admin@dora.local", SessionID: "admin-session", AccessToken: "token"}
	rotated, err := app.RotatePassword(t.Context(), RotatePasswordInput{
		Auth: auth, CurrentPassword: "local-admin-change-me", NewPassword: "Passw0rd!Rotated", Reason: "operator rotate",
		Meta: RequestMeta{TraceID: "trace-admin-operator-rotate", IdempotencyKey: "idem-admin-operator-rotate"},
	})
	if err != nil {
		t.Fatalf("rotate admin password: %v", err)
	}
	requireAdminUpdatedBy(t, app, "platform_admins", "id = ?", rotated.AdminID, rotated.AdminID)
	requireAdminUpdatedBy(t, app, "platform_admin_bootstraps", "admin_id = ?", rotated.AdminID, rotated.AdminID)

	preview, err := app.PreviewSetUserStatus(t.Context(), UserStatusInput{
		Auth: auth, UserID: "usr_1001", TargetStatus: "disabled", Reason: "operator status",
		Meta: RequestMeta{TraceID: "trace-admin-operator-user-preview", IdempotencyKey: "idem-admin-operator-user-preview"},
	})
	if err != nil {
		t.Fatalf("preview user status: %v", err)
	}
	if _, err := app.ConfirmSetUserStatus(t.Context(), UserStatusInput{
		Auth: auth, UserID: "usr_1001", TargetStatus: "disabled", Reason: "operator status", PreviewToken: preview.PreviewToken,
		Meta: RequestMeta{TraceID: "trace-admin-operator-user-status", IdempotencyKey: "idem-admin-operator-user-status"},
	}); err != nil {
		t.Fatalf("confirm user status: %v", err)
	}
	requireAdminUpdatedBy(t, app, "business_users", "id = ?", auth.AdminID, "usr_1001")
}

func requireAdminOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		CreatedBy *string `gorm:"column:created_by"`
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := app.repo.DB().Raw("SELECT created_by, updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
	if tx.Error != nil {
		t.Fatalf("query operator columns for %s: %v", table, tx.Error)
	}
	if tx.RowsAffected == 0 {
		t.Fatalf("expected row in %s where %s", table, where)
	}
	if stringValue(row.CreatedBy) != wantCreatedBy || stringValue(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, stringValue(row.CreatedBy), stringValue(row.UpdatedBy))
	}
}

func requireAdminUpdatedBy(t *testing.T, app *App, table string, where string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := app.repo.DB().Raw("SELECT updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
	if tx.Error != nil {
		t.Fatalf("query updated_by for %s: %v", table, tx.Error)
	}
	if tx.RowsAffected == 0 {
		t.Fatalf("expected row in %s where %s", table, where)
	}
	if stringValue(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected updated_by in %s: %q", table, stringValue(row.UpdatedBy))
	}
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
