package credit

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/datatypes"
)

// ACCT-3：企业拥有者看企业全部流水；普通成员只看自己在企业空间产生的消耗明细(本人 project 的流水)。
func TestEnterpriseUsageMemberSeesOnlyOwnProjectLedger(t *testing.T) {
	app := newCreditTestApp(t)
	db := app.repo.DB().WithContext(t.Context())
	entID := "ent_1001"

	mkProject := func(id, owner string) *businesscore.Project {
		return &businesscore.Project{
			ID: id, ProjectNo: id, OwnerUserID: owner, SpaceID: "sp_enterprise_1001", EnterpriseID: &entID,
			Title: id, Status: "active", CreativeStatus: "editable", CreativeAllowed: true,
			LastActivityAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
	}
	mkLedger := func(id, projectID string, delta int64, at time.Time) *businesscore.CreditLedgerEntry {
		pid := projectID
		return &businesscore.CreditLedgerEntry{
			ID: id, AccountID: "ca_enterprise_1001", EntryType: "charge", PointsDelta: delta,
			BalanceAfter: 100000, FrozenAfter: 0, SourceType: "tool_charge", SourceID: id,
			ProjectID: &pid, MetadataJSON: datatypes.JSON([]byte("{}")), CreatedAt: at,
		}
	}
	for _, row := range []any{
		mkProject("prj_ent_owner", "usr_1001"),
		mkProject("prj_ent_member", "usr_member"),
		mkLedger("cled_owner", "prj_ent_owner", -10, time.Now().UTC().Add(-time.Minute)),
		mkLedger("cled_member", "prj_ent_member", -20, time.Now().UTC()),
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}

	ownerAuth := AuthContext{UserID: "usr_1001", SpaceID: "sp_enterprise_1001", EnterpriseID: entID, EnterpriseRole: accountspace.RoleOwner, LoginIdentityType: "enterprise"}
	memberAuth := AuthContext{UserID: "usr_member", SpaceID: "sp_enterprise_1001", EnterpriseID: entID, EnterpriseRole: accountspace.RoleMember, LoginIdentityType: "enterprise"}

	ownerPage, err := app.ListEnterpriseUsage(t.Context(), ownerAuth, 10, 0)
	if err != nil {
		t.Fatalf("owner usage: %v", err)
	}
	if ownerPage.Total != 2 {
		t.Fatalf("owner 应看到企业全部流水，got total=%d", ownerPage.Total)
	}

	memberPage, err := app.ListEnterpriseUsage(t.Context(), memberAuth, 10, 0)
	if err != nil {
		t.Fatalf("member usage: %v", err)
	}
	if memberPage.Total != 1 || len(memberPage.Items) != 1 || memberPage.Items[0].EntryID != "cled_member" {
		t.Fatalf("member 应只看到本人 project 流水 cled_member，got total=%d items=%#v", memberPage.Total, memberPage.Items)
	}
}
