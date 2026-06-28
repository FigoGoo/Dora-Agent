package accountspace

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestAccountspaceLoginAndLogoutPopulateOperatorColumns(t *testing.T) {
	app := newAccountspaceOperatorTestApp(t)
	session, err := app.Login(t.Context(), LoginInput{
		LoginType: IdentityPersonal, Account: "user1001@dora.local", Password: "local-user-change-me",
		Meta: RequestMeta{TraceID: "trace-account-operator-login-personal"},
	})
	if err != nil {
		t.Fatalf("login personal account: %v", err)
	}
	auth, err := app.AuthenticateToken(t.Context(), session.AccessToken)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	requireAccountOperatorColumns(t, app, "auth_sessions", "id = ?", auth.UserID, auth.UserID, auth.SessionID)
	requireAccountUpdatedBy(t, app, "business_users", "id = ?", auth.UserID, auth.UserID)
	if err := app.Logout(t.Context(), auth, RequestMeta{TraceID: "trace-account-operator-logout-personal"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	requireAccountOperatorColumns(t, app, "auth_sessions", "id = ?", auth.UserID, auth.UserID, auth.SessionID)
}

func TestAccountspaceEnterpriseFlowsPopulateOperatorColumns(t *testing.T) {
	app := newAccountspaceOperatorTestApp(t)
	auth := AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: IdentityPersonal, SessionID: "sess_operator_account"}
	enterprise, err := app.CreateEnterprise(t.Context(), CreateEnterpriseInput{
		Auth: auth, EnterpriseName: "Operator Enterprise",
		Meta: RequestMeta{TraceID: "trace-account-operator-enterprise", IdempotencyKey: "idem-account-operator-enterprise"},
	})
	if err != nil {
		t.Fatalf("create enterprise: %v", err)
	}
	requireAccountOperatorColumns(t, app, "enterprises", "id = ?", auth.UserID, auth.UserID, enterprise.EnterpriseID)
	requireAccountOperatorColumns(t, app, "business_spaces", "id = ?", auth.UserID, auth.UserID, enterprise.SpaceID)
	requireAccountOperatorColumns(t, app, "credit_accounts", "enterprise_id = ?", auth.UserID, auth.UserID, enterprise.EnterpriseID)
	requireAccountOperatorColumns(t, app, "enterprise_members", "enterprise_id = ? AND user_id = ?", auth.UserID, auth.UserID, enterprise.EnterpriseID, auth.UserID)

	enterpriseAuth := AuthContext{UserID: auth.UserID, SpaceID: enterprise.SpaceID, EnterpriseID: enterprise.EnterpriseID, EnterpriseRole: RoleOwner, LoginIdentityType: IdentityEnterprise, SessionID: "sess_operator_account"}
	invite, err := app.CreateMemberInvite(t.Context(), InviteInput{
		Auth: enterpriseAuth, Email: "operator-invite@dora.local", ExpiresInDays: 3,
		Meta: RequestMeta{TraceID: "trace-account-operator-invite", IdempotencyKey: "idem-account-operator-invite"},
	})
	if err != nil {
		t.Fatalf("create member invite: %v", err)
	}
	requireAccountOperatorColumns(t, app, "enterprise_invites", "id = ?", auth.UserID, auth.UserID, invite.InviteID)
	insertEnterpriseMemberForOperatorTest(t, app, "ent_mem_operator_remove")

	removePreview, err := app.PreviewRemoveMember(t.Context(), RemoveMemberInput{Auth: testEnterpriseOwnerAuth(), MemberID: "ent_mem_operator_remove"})
	if err != nil {
		t.Fatalf("preview remove member: %v", err)
	}
	if _, err := app.ConfirmRemoveMember(t.Context(), RemoveMemberInput{
		Auth: testEnterpriseOwnerAuth(), MemberID: "ent_mem_operator_remove", Reason: "operator remove", PreviewToken: removePreview.PreviewToken,
		Meta: RequestMeta{TraceID: "trace-account-operator-remove", IdempotencyKey: "idem-account-operator-remove"},
	}); err != nil {
		t.Fatalf("confirm remove member: %v", err)
	}
	requireAccountUpdatedBy(t, app, "enterprise_members", "id = ?", "usr_1001", "ent_mem_operator_remove")
}

func TestAccountspaceSessionAndTransferPopulateOperatorColumns(t *testing.T) {
	app := newAccountspaceOperatorTestApp(t)
	session, err := app.Login(t.Context(), LoginInput{
		LoginType: IdentityEnterprise, Account: "user1001@dora.local", Password: "local-user-change-me", EnterpriseID: "ent_1001",
		Meta: RequestMeta{TraceID: "trace-account-operator-login", IdempotencyKey: "idem-account-operator-login"},
	})
	if err != nil {
		t.Fatalf("login enterprise: %v", err)
	}
	auth, err := app.AuthenticateToken(t.Context(), session.AccessToken)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	requireAccountOperatorColumns(t, app, "auth_sessions", "id = ?", auth.UserID, auth.UserID, auth.SessionID)
	requireAccountUpdatedBy(t, app, "business_users", "id = ?", auth.UserID, auth.UserID)

	if _, err := app.SwitchIdentity(t.Context(), SwitchIdentityInput{
		Auth: auth, TargetIdentityType: IdentityPersonal,
		Meta: RequestMeta{TraceID: "trace-account-operator-switch", IdempotencyKey: "idem-account-operator-switch"},
	}); err != nil {
		t.Fatalf("switch identity: %v", err)
	}
	requireAccountOperatorColumns(t, app, "auth_sessions", "id = ?", auth.UserID, auth.UserID, auth.SessionID)

	insertEnterpriseMemberForOperatorTest(t, app, "ent_mem_operator_transfer")
	preview, err := app.PreviewTransferOwner(t.Context(), TransferOwnerInput{Auth: testEnterpriseOwnerAuth(), TargetMemberID: "ent_mem_operator_transfer"})
	if err != nil {
		t.Fatalf("preview transfer owner: %v", err)
	}
	if _, err := app.ConfirmTransferOwner(t.Context(), TransferOwnerInput{
		Auth: testEnterpriseOwnerAuth(), TargetMemberID: "ent_mem_operator_transfer", Reason: "operator transfer", PreviewToken: preview.PreviewToken,
		Meta: RequestMeta{TraceID: "trace-account-operator-transfer", IdempotencyKey: "idem-account-operator-transfer"},
	}); err != nil {
		t.Fatalf("confirm transfer owner: %v", err)
	}
	requireAccountUpdatedBy(t, app, "enterprises", "id = ?", "usr_1001", "ent_1001")
	requireAccountUpdatedBy(t, app, "enterprise_members", "enterprise_id = ? AND user_id = ?", "usr_1001", "ent_1001", "usr_1001")
	requireAccountUpdatedBy(t, app, "enterprise_members", "id = ?", "usr_1001", "ent_mem_operator_transfer")

	if err := app.Logout(t.Context(), auth, RequestMeta{TraceID: "trace-account-operator-logout"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	requireAccountOperatorColumns(t, app, "auth_sessions", "id = ?", auth.UserID, auth.UserID, auth.SessionID)
}

func insertEnterpriseMemberForOperatorTest(t *testing.T, app *App, memberID string) {
	t.Helper()
	now := time.Now().UTC()
	member := businesscore.EnterpriseMember{
		ID: memberID, EnterpriseID: "ent_1001", UserID: "usr_1002", Role: RoleMember, Status: StatusActive,
		JoinedAt: &now, CreatedBy: optionalString("usr_1001"), UpdatedBy: optionalString("usr_1001"), CreatedAt: now, UpdatedAt: now,
	}
	if err := app.repo.DB().WithContext(t.Context()).Create(&member).Error; err != nil {
		t.Fatalf("insert enterprise member: %v", err)
	}
}

func newAccountspaceOperatorTestApp(t *testing.T) *App {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_accountspace_operator")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	return New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
}

func testEnterpriseOwnerAuth() AuthContext {
	return AuthContext{
		UserID: "usr_1001", SpaceID: "sp_enterprise_1001", EnterpriseID: "ent_1001",
		EnterpriseRole: RoleOwner, LoginIdentityType: IdentityEnterprise, SessionID: "sess_operator_enterprise",
	}
}

func requireAccountOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
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
	if value(row.CreatedBy) != wantCreatedBy || value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
}

func requireAccountUpdatedBy(t *testing.T, app *App, table string, where string, wantUpdatedBy string, args ...any) {
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
	if value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected updated_by in %s: %q", table, value(row.UpdatedBy))
	}
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
