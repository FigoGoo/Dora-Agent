package project

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestCheckProjectAccessRejectsRemovedEnterpriseMember(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_project_acct4")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	now := time.Now().UTC()
	entID := "ent_1001"
	project := businesscore.Project{
		ID: "prj_enterprise_acct4", ProjectNo: "P-ACCT4", OwnerUserID: "usr_1001", SpaceID: "sp_enterprise_1001", EnterpriseID: &entID,
		Title: "ACCT4 enterprise project", Status: StatusActive, CreativeStatus: "editable", CreativeAllowed: true, LastActivityAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.WithContext(t.Context()).Create(&project).Error; err != nil {
		t.Fatalf("seed enterprise project: %v", err)
	}

	app := New(businesscore.New(db.DB), idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	auth := accountspace.AuthContext{
		UserID: "usr_1001", SpaceID: "sp_enterprise_1001", LoginIdentityType: accountspace.IdentityEnterprise,
		EnterpriseID: "ent_1001", EnterpriseRole: accountspace.RoleOwner,
	}
	if _, err := app.CheckProjectAccess(t.Context(), auth, project.ID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION); err != nil {
		t.Fatalf("active enterprise member should access project: %v", err)
	}
	if err := db.DB.WithContext(t.Context()).Model(&businesscore.EnterpriseMember{}).
		Where("enterprise_id = ? AND user_id = ?", "ent_1001", "usr_1001").
		Updates(map[string]any{"status": accountspace.StatusRemoved, "updated_at": now}).Error; err != nil {
		t.Fatalf("remove enterprise member: %v", err)
	}
	if _, err := app.CheckProjectAccess(t.Context(), auth, project.ID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION); codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("removed enterprise member should be denied, got %v", err)
	}
}

func TestProjectOperatorColumnsAreFilledFromAuthContext(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_project_operator")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	app := New(businesscore.New(db.DB), idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	auth := accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: accountspace.IdentityPersonal}

	created, err := app.CreateProject(t.Context(), CreateInput{
		Auth: auth, Title: "Operator project", Meta: RequestMeta{TraceID: "trace-project-operator-create", IdempotencyKey: "idem-project-operator-create"},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	requireOperatorColumns(t, app, "projects", "id = ?", auth.UserID, auth.UserID, created.ProjectID)

	title := "Operator project updated"
	if _, err := app.UpdateProject(t.Context(), UpdateInput{
		Auth: auth, ProjectID: created.ProjectID, Title: &title,
		Meta: RequestMeta{TraceID: "trace-project-operator-update", IdempotencyKey: "idem-project-operator-update"},
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}
	requireOperatorColumns(t, app, "projects", "id = ?", auth.UserID, auth.UserID, created.ProjectID)

	if _, err := app.ArchiveProject(t.Context(), ArchiveInput{
		Auth: auth, ProjectID: created.ProjectID, Reason: "operator audit",
		Meta: RequestMeta{TraceID: "trace-project-operator-archive", IdempotencyKey: "idem-project-operator-archive"},
	}); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	requireOperatorColumns(t, app, "projects", "id = ?", auth.UserID, auth.UserID, created.ProjectID)

	attached, err := app.AttachAssetToProject(t.Context(), AttachAssetInput{
		Auth: auth, ProjectID: "prj_active_1001", AssetID: "ast_generated_1001", AssetRole: "operator_reference", SourceType: "user",
		Meta: RequestMeta{TraceID: "trace-project-operator-attach", IdempotencyKey: "idem-project-operator-attach"},
	})
	if err != nil {
		t.Fatalf("attach project asset: %v", err)
	}
	if attached.AssetRole != "operator_reference" {
		t.Fatalf("unexpected attached asset role: %#v", attached)
	}
	requireOperatorColumns(t, app, "project_assets", "project_id = ? AND asset_id = ? AND asset_role = ?", auth.UserID, auth.UserID, "prj_active_1001", "ast_generated_1001", "operator_reference")
}

func requireOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		CreatedBy *string `gorm:"column:created_by"`
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := app.repo.DB().Raw("SELECT created_by, updated_by FROM "+table+" WHERE "+where, args...).Scan(&row)
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

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	if bizErr := bizerrors.FromError(err); bizErr != nil {
		return bizErr.Code
	}
	return ""
}
