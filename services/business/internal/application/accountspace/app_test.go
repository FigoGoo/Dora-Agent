package accountspace

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func TestAuthenticateTokenRejectsRemovedEnterpriseMember(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_accountspace_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	app := New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	session, err := app.Login(t.Context(), LoginInput{
		LoginType: IdentityEnterprise, Account: "user1001@dora.local", Password: "local-user-change-me", EnterpriseID: "ent_1001",
		Meta: RequestMeta{TraceID: "trace-enterprise-login", RequestID: "req-enterprise-login", Source: "test"},
	})
	if err != nil {
		t.Fatalf("login enterprise: %v", err)
	}
	auth, err := app.AuthenticateToken(t.Context(), session.AccessToken)
	if err != nil || auth.EnterpriseID != "ent_1001" {
		t.Fatalf("authenticate enterprise token before removal: %#v err=%v", auth, err)
	}

	now := time.Now().UTC()
	if err := db.DB.WithContext(t.Context()).Model(&businesscore.EnterpriseMember{}).
		Where("enterprise_id = ? AND user_id = ?", "ent_1001", "usr_1001").
		Updates(map[string]any{"status": StatusRemoved, "updated_at": now}).Error; err != nil {
		t.Fatalf("remove enterprise member: %v", err)
	}

	_, err = app.AuthenticateToken(t.Context(), session.AccessToken)
	if codeOf(err) != bizerrors.CodePermissionDenied {
		t.Fatalf("expected removed enterprise token denied, got %v", err)
	}
}

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	if businessErr, ok := err.(*bizerrors.BusinessError); ok {
		return businessErr.Code
	}
	return ""
}
