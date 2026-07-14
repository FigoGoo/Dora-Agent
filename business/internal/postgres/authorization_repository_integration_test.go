package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
)

// authorizationIntegrationClock 为真实 PostgreSQL 生命周期测试冻结时间。
type authorizationIntegrationClock struct{ now time.Time }

// Now 返回预置 UTC 时间。
func (clock authorizationIntegrationClock) Now() time.Time { return clock.now }

// authorizationIntegrationIDs 为首次授予返回固定 UUIDv7。
type authorizationIntegrationIDs struct{ id string }

// New 返回预置 UUIDv7。
func (ids authorizationIntegrationIDs) New() (string, error) { return ids.id, nil }

func TestAuthorizationRepositoryPostgreSQLLifecycle(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	targetID := "019f0000-0000-7000-8000-000000000081"
	actorID := "019f0000-0000-7000-8000-000000000082"
	otherActorID := "019f0000-0000-7000-8000-000000000083"
	for _, seed := range []struct {
		id   string
		name string
	}{{targetID, "Reviewer"}, {actorID, "Provisioner"}, {otherActorID, "Other Provisioner"}} {
		if err := db.Exec(`
			INSERT INTO business.user_account (id, display_name, user_type, status, version, created_at, updated_at)
			VALUES (?, ?, 'personal', 'active', 1, ?, ?)`, seed.id, seed.name, now, now).Error; err != nil {
			t.Fatalf("seed authorization account: %v", err)
		}
	}
	repository, err := NewAuthorizationRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	service, err := authorization.NewService(repository, authorizationIntegrationClock{now: now}, authorizationIntegrationIDs{
		id: "019f0000-0000-7000-8000-000000000084",
	})
	if err != nil {
		t.Fatal(err)
	}

	empty, err := service.Resolve(context.Background(), targetID)
	if err != nil || empty.Roles == nil || len(empty.Roles) != 0 {
		t.Fatalf("empty role resolution failed: %+v err=%v", empty, err)
	}
	grant := authorization.GrantCommand{
		TargetUserID: targetID, ActorUserID: actorID, Role: authorization.RoleSkillReviewer,
		ReasonCode: "reviewer_onboarding", ApprovalReference: "DEPLOY-123",
	}
	created, err := service.Grant(context.Background(), grant)
	if err != nil || created.IdempotentReplay || created.Assignment.Version != 1 {
		t.Fatalf("grant role: result=%+v err=%v", created, err)
	}
	replayed, err := service.Grant(context.Background(), grant)
	if err != nil || !replayed.IdempotentReplay || replayed.Assignment.ID != created.Assignment.ID {
		t.Fatalf("replay grant role: result=%+v err=%v", replayed, err)
	}
	conflictingGrant := grant
	conflictingGrant.ActorUserID = otherActorID
	if _, err := service.Grant(context.Background(), conflictingGrant); !errors.Is(err, authorization.ErrAssignmentConflict) {
		t.Fatalf("different grant semantics did not conflict: %v", err)
	}
	projected, err := service.Resolve(context.Background(), targetID)
	if err != nil || len(projected.Capabilities) != 1 || projected.Capabilities[0] != "skill.review" {
		t.Fatalf("active reviewer did not project capability: %+v err=%v", projected, err)
	}

	revoke := authorization.RevokeCommand{
		AssignmentID: created.Assignment.ID, TargetUserID: targetID, ActorUserID: actorID,
		Role: authorization.RoleSkillReviewer, ExpectedVersion: 1,
		ReasonCode: "reviewer_offboarding", ApprovalReference: "DEPLOY-456",
	}
	revoked, err := service.Revoke(context.Background(), revoke)
	if err != nil || revoked.IdempotentReplay || revoked.Assignment.Version != 2 || revoked.Assignment.RevocationApprovalReference == nil {
		t.Fatalf("revoke role: result=%+v err=%v", revoked, err)
	}
	revokedReplay, err := service.Revoke(context.Background(), revoke)
	if err != nil || !revokedReplay.IdempotentReplay || revokedReplay.Assignment.Version != 2 {
		t.Fatalf("replay revoke role: result=%+v err=%v", revokedReplay, err)
	}
	conflictingRevoke := revoke
	conflictingRevoke.ApprovalReference = "DEPLOY-OTHER"
	if _, err := service.Revoke(context.Background(), conflictingRevoke); !errors.Is(err, authorization.ErrAssignmentConflict) {
		t.Fatalf("different revoke semantics did not conflict: %v", err)
	}
	afterRevoke, err := service.Resolve(context.Background(), targetID)
	if err != nil || len(afterRevoke.Roles) != 0 {
		t.Fatalf("revoked reviewer remained projected: %+v err=%v", afterRevoke, err)
	}
	if err := db.Exec("DELETE FROM business.user_role_assignment WHERE id = ?", created.Assignment.ID).Error; err == nil {
		t.Fatal("append-only trigger allowed role assignment DELETE")
	}
}
