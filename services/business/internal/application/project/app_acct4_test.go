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

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	if bizErr := bizerrors.FromError(err); bizErr != nil {
		return bizErr.Code
	}
	return ""
}
