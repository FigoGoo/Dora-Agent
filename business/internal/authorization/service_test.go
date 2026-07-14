package authorization

import (
	"context"
	"errors"
	"testing"
	"time"
)

// authorizationTestRepository 为 Service 测试注入解析和角色生命周期结果。
type authorizationTestRepository struct {
	resolution    RoleResolution
	resolveErr    error
	grantResult   MutationResult
	grantErr      error
	revokeResult  MutationResult
	revokeErr     error
	grantInput    Assignment
	revokeInput   RevokeCommand
	revokedAt     time.Time
	resolveUserID string
}

// ResolveActiveRoles 返回预置角色集合并捕获解析用户。
func (repository *authorizationTestRepository) ResolveActiveRoles(_ context.Context, userID string) (RoleResolution, error) {
	repository.resolveUserID = userID
	return repository.resolution, repository.resolveErr
}

// Grant 捕获待授予事实并返回预置结果。
func (repository *authorizationTestRepository) Grant(_ context.Context, assignment Assignment) (MutationResult, error) {
	repository.grantInput = assignment
	if repository.grantResult.Assignment.ID == "" {
		return MutationResult{Assignment: assignment}, repository.grantErr
	}
	return repository.grantResult, repository.grantErr
}

// Revoke 捕获撤权命令与冻结时间并返回预置结果。
func (repository *authorizationTestRepository) Revoke(_ context.Context, command RevokeCommand, revokedAt time.Time) (MutationResult, error) {
	repository.revokeInput = command
	repository.revokedAt = revokedAt
	return repository.revokeResult, repository.revokeErr
}

// authorizationTestClock 返回测试冻结时间。
type authorizationTestClock struct{ now time.Time }

// Now 返回预置 UTC 时间。
func (clock authorizationTestClock) Now() time.Time { return clock.now }

// authorizationTestIDs 返回预置 UUIDv7 或错误。
type authorizationTestIDs struct {
	id  string
	err error
}

// New 返回预置标识。
func (ids authorizationTestIDs) New() (string, error) { return ids.id, ids.err }

func TestServiceResolveMapsClosedRoleAndRejectsUnknown(t *testing.T) {
	userID := "019f0000-0000-7000-8000-000000000011"
	repository := &authorizationTestRepository{resolution: RoleResolution{
		SubjectActive: true, Roles: []RoleKey{RoleSkillReviewer, RoleSkillReviewer},
	}}
	service, err := NewService(repository, authorizationTestClock{}, authorizationTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := service.Resolve(context.Background(), userID)
	if err != nil || len(projection.Roles) != 1 || projection.Roles[0] != "skill_reviewer" ||
		len(projection.Capabilities) != 1 || projection.Capabilities[0] != "skill.review" || repository.resolveUserID != userID {
		t.Fatalf("unexpected closed authorization projection: projection=%+v err=%v", projection, err)
	}

	repository.resolution.Roles = []RoleKey{"unknown_role"}
	if _, err := service.Resolve(context.Background(), userID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown role did not fail closed: %v", err)
	}
	repository.resolution = RoleResolution{SubjectActive: false, Roles: []RoleKey{}}
	if _, err := service.Resolve(context.Background(), userID); !errors.Is(err, ErrSubjectInactive) {
		t.Fatalf("inactive subject did not fail closed: %v", err)
	}
}

func TestServiceResolveReturnsNonNilEmptyProjection(t *testing.T) {
	repository := &authorizationTestRepository{resolution: RoleResolution{SubjectActive: true, Roles: []RoleKey{}}}
	service, _ := NewService(repository, authorizationTestClock{}, authorizationTestIDs{})
	projection, err := service.Resolve(context.Background(), "019f0000-0000-7000-8000-000000000011")
	if err != nil || projection.Roles == nil || projection.Capabilities == nil || len(projection.Roles) != 0 || len(projection.Capabilities) != 0 {
		t.Fatalf("empty projection is not stable non-nil arrays: %+v err=%v", projection, err)
	}
}

func TestServiceGrantBuildsAuditedUUIDv7Assignment(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	repository := &authorizationTestRepository{}
	service, _ := NewService(repository, authorizationTestClock{now: now}, authorizationTestIDs{
		id: "019f0000-0000-7000-8000-000000000013",
	})
	result, err := service.Grant(context.Background(), GrantCommand{
		TargetUserID: "019f0000-0000-7000-8000-000000000011",
		ActorUserID:  "019f0000-0000-7000-8000-000000000012",
		Role:         RoleSkillReviewer, ReasonCode: "reviewer_onboarding", ApprovalReference: "DEPLOY-123",
	})
	if err != nil || result.Assignment.ID == "" || repository.grantInput.Status != StatusActive ||
		repository.grantInput.AssignedAt != now || repository.grantInput.Version != 1 {
		t.Fatalf("Grant did not freeze audited assignment: result=%+v input=%+v err=%v", result, repository.grantInput, err)
	}
}

func TestServiceRejectsSelfGrantAndInvalidRevokeBeforeRepository(t *testing.T) {
	repository := &authorizationTestRepository{}
	service, _ := NewService(repository, authorizationTestClock{}, authorizationTestIDs{})
	userID := "019f0000-0000-7000-8000-000000000011"
	if _, err := service.Grant(context.Background(), GrantCommand{
		TargetUserID: userID, ActorUserID: userID, Role: RoleSkillReviewer,
		ReasonCode: "reason", ApprovalReference: "REF-1",
	}); !errors.Is(err, ErrInvalidCommand) || repository.grantInput.ID != "" {
		t.Fatalf("self grant reached Repository: err=%v input=%+v", err, repository.grantInput)
	}
	if _, err := service.Revoke(context.Background(), RevokeCommand{
		AssignmentID: "019f0000-0000-7000-8000-000000000013",
		TargetUserID: userID, ActorUserID: "019f0000-0000-7000-8000-000000000012",
		Role: RoleSkillReviewer, ExpectedVersion: 0, ReasonCode: "reason", ApprovalReference: "REF-2",
	}); !errors.Is(err, ErrInvalidCommand) || repository.revokeInput.AssignmentID != "" {
		t.Fatalf("invalid revoke reached Repository: err=%v input=%+v", err, repository.revokeInput)
	}
}
