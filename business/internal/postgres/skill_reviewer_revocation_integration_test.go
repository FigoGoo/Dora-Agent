package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"gorm.io/gorm"
)

// runPinnedPostgreSQLOperation 在单一专用数据库连接上执行并发操作，并暴露 backend PID 供锁等待屏障观测。
func runPinnedPostgreSQLOperation(
	ctx context.Context,
	db *gorm.DB,
	start <-chan struct{},
	ready chan<- int,
	result chan<- error,
	operation func(*gorm.DB) error,
) {
	err := db.WithContext(ctx).Connection(func(connectionDB *gorm.DB) error {
		var backendPID int
		if err := connectionDB.Raw(`SELECT pg_backend_pid()`).Scan(&backendPID).Error; err != nil {
			return fmt.Errorf("read pinned PostgreSQL backend PID: %w", err)
		}
		select {
		case ready <- backendPID:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-start:
		case <-ctx.Done():
			return ctx.Err()
		}
		return operation(connectionDB)
	})
	result <- err
}

// waitForPostgreSQLBlocker 轮询 PostgreSQL 锁图，只在数据库确认 blockedPID 正被 blockerPID 阻塞后才打开下一道屏障。
func waitForPostgreSQLBlocker(ctx context.Context, observer *gorm.DB, blockedPID int, blockerPID int) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for PostgreSQL blocker %d -> %d: %w", blockerPID, blockedPID, err)
		}
		var blocked bool
		err := observer.WithContext(ctx).Raw(`
SELECT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_stat_activity
    WHERE pid = CAST(? AS integer)
      AND wait_event_type = 'Lock'
      AND CAST(? AS integer) = ANY(pg_catalog.pg_blocking_pids(pid))
)`, blockedPID, blockerPID).Scan(&blocked).Error
		if err != nil {
			return fmt.Errorf("observe PostgreSQL blocker %d -> %d: %w", blockerPID, blockedPID, err)
		}
		if blocked {
			return nil
		}
		// 让出本地调度器即可；屏障只依赖 PostgreSQL 权威锁图，不依赖任意时长。
		runtime.Gosched()
	}
}

// TestSkillApprovalCannotCommitAfterConcurrentReviewerRevocation 以真实 PostgreSQL 锁屏障证明撤权先线性化时，已开始但尚未取得权限锁的审批不能越权提交。
func TestSkillApprovalCannotCommitAfterConcurrentReviewerRevocation(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	skillRepository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	authorizationRepository, err := NewAuthorizationRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}

	aggregate := newSkillRepositoryCreateAggregate(t)
	if _, replay, err := skillRepository.Create(context.Background(), aggregate); err != nil || replay {
		t.Fatalf("create approval barrier Skill fixture: replay=%t err=%v", replay, err)
	}
	reviewID := newSkillRepositoryUUIDv7(t)
	reviewAt := aggregate.Draft.CreatedAt.Add(time.Minute)
	reviewStatus := skill.ReviewStatusReviewing
	submission := skill.SubmitReviewAggregate{
		ExpectedDraftRevisionID: aggregate.Draft.ID,
		Review: skill.ReviewSubmission{
			ID: reviewID, SkillID: aggregate.Skill.ID, ContentRevisionID: aggregate.Draft.ID,
			ContentDigest: aggregate.Draft.ContentDigest, Status: reviewStatus, Version: 1,
			SubmittedByUserID: aggregate.Skill.OwnerUserID, SubmittedAt: reviewAt, UpdatedAt: reviewAt,
		},
		Receipt: skill.CommandReceipt{
			ID: newSkillRepositoryUUIDv7(t), ActorUserID: aggregate.Skill.OwnerUserID, CommandType: skill.CommandTypeSubmitReview,
			ScopeID: aggregate.Skill.ID, KeyDigest: sha256.Sum256([]byte("revoke-barrier-submit")),
			SemanticDigest: sha256.Sum256([]byte("revoke-barrier-submit-semantic")), ResultSkillID: aggregate.Skill.ID,
			ResultContentRevisionID: stringValuePointer(aggregate.Draft.ID), ResultReviewSubmissionID: stringValuePointer(reviewID),
			ResponseDraftRevisionID: aggregate.Draft.ID, ResponseReviewSubmissionID: stringValuePointer(reviewID),
			ResponseReviewStatus: &reviewStatus, ResponseReviewUpdatedAt: timeValuePointer(reviewAt),
			ResponseGovernanceStatus: skill.GovernanceStatusActive, CreatedAt: reviewAt,
		},
	}
	if _, err := skillRepository.SubmitReview(context.Background(), submission); err != nil {
		t.Fatalf("submit approval barrier review fixture: %v", err)
	}

	reviewerID := newSkillRepositoryUUIDv7(t)
	provisionerID := newSkillRepositoryUUIDv7(t)
	for _, account := range []struct {
		id   string
		name string
	}{{reviewerID, "Barrier Reviewer"}, {provisionerID, "Barrier Provisioner"}} {
		if err := db.Create(&userAccountModel{
			ID: account.id, DisplayName: account.name, UserType: "personal", Status: "active", Version: 1,
			CreatedAt: reviewAt, UpdatedAt: reviewAt,
		}).Error; err != nil {
			t.Fatalf("create approval barrier account: %v", err)
		}
	}
	assignmentID := newSkillRepositoryUUIDv7(t)
	if _, err := authorizationRepository.Grant(context.Background(), authorization.Assignment{
		ID: assignmentID, UserID: reviewerID, Role: authorization.RoleSkillReviewer,
		Status: authorization.StatusActive, Version: 1, AssignedByUserID: provisionerID,
		AssignmentReasonCode: "concurrent_approval_barrier", ApprovalReference: "TEST-APPROVAL-BARRIER-GRANT",
		AssignedAt: reviewAt, UpdatedAt: reviewAt,
	}); err != nil {
		t.Fatalf("grant approval barrier reviewer: %v", err)
	}

	approveAt := reviewAt.Add(time.Minute)
	approveCommand := skill.ApproveAndPublishCommand{
		ReviewID: reviewID, ReviewerUserID: reviewerID, SnapshotID: newSkillRepositoryUUIDv7(t),
		ReceiptID: newSkillRepositoryUUIDv7(t), RequestID: newSkillRepositoryUUIDv7(t),
		KeyDigest:      sha256.Sum256([]byte("revoke-barrier-approve")),
		SemanticDigest: sha256.Sum256([]byte("revoke-barrier-approve-semantic")),
		IfMatch:        skill.ReviewETag(submission.Review), AuditID: newSkillRepositoryUUIDv7(t), DecidedAt: approveAt,
	}
	revokeCommand := authorization.RevokeCommand{
		AssignmentID: assignmentID, TargetUserID: reviewerID, ActorUserID: provisionerID,
		Role: authorization.RoleSkillReviewer, ExpectedVersion: 1,
		ReasonCode: "concurrent_approval_barrier", ApprovalReference: "TEST-APPROVAL-BARRIER-REVOKE",
	}

	operationContext, cancelOperations := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelOperations()
	gate := db.WithContext(operationContext).Begin()
	if gate.Error != nil {
		t.Fatalf("begin approval barrier gate: %v", gate.Error)
	}
	gateReleased := false
	defer func() {
		if !gateReleased {
			_ = gate.Rollback().Error
		}
	}()
	var gatePID int
	if err := gate.Raw(`SELECT pg_backend_pid()`).Scan(&gatePID).Error; err != nil {
		t.Fatalf("read approval barrier gate PID: %v", err)
	}
	var lockedAssignmentID string
	if err := gate.Raw(`SELECT id FROM business.user_role_assignment WHERE id = ? FOR UPDATE`, assignmentID).
		Scan(&lockedAssignmentID).Error; err != nil || lockedAssignmentID != assignmentID {
		t.Fatalf("lock approval barrier assignment: id=%s err=%v", lockedAssignmentID, err)
	}

	revokeStart := make(chan struct{})
	revokeReady := make(chan int, 1)
	revokeResult := make(chan error, 1)
	go runPinnedPostgreSQLOperation(operationContext, db, revokeStart, revokeReady, revokeResult, func(connectionDB *gorm.DB) error {
		repository, repositoryErr := NewAuthorizationRepository(&Client{db: connectionDB})
		if repositoryErr != nil {
			return repositoryErr
		}
		_, revokeErr := repository.Revoke(operationContext, revokeCommand, approveAt)
		return revokeErr
	})
	var revokePID int
	select {
	case revokePID = <-revokeReady:
	case earlyErr := <-revokeResult:
		t.Fatalf("prepare pinned reviewer revocation: %v", earlyErr)
	case <-operationContext.Done():
		t.Fatalf("prepare pinned reviewer revocation: %v", operationContext.Err())
	}
	close(revokeStart)
	// Gate 只锁 assignment；观测到此关系时，撤权事务已持有 reviewer/provisioner account 锁并等待 assignment。
	if err := waitForPostgreSQLBlocker(operationContext, db, revokePID, gatePID); err != nil {
		t.Fatal(err)
	}

	approveStart := make(chan struct{})
	approveReady := make(chan int, 1)
	approveResult := make(chan error, 1)
	go runPinnedPostgreSQLOperation(operationContext, db, approveStart, approveReady, approveResult, func(connectionDB *gorm.DB) error {
		repository, repositoryErr := NewSkillRepository(&Client{db: connectionDB})
		if repositoryErr != nil {
			return repositoryErr
		}
		_, approveErr := repository.ApproveAndPublish(operationContext, approveCommand)
		return approveErr
	})
	var approvePID int
	select {
	case approvePID = <-approveReady:
	case earlyErr := <-approveResult:
		t.Fatalf("prepare pinned Skill approval: %v", earlyErr)
	case <-operationContext.Done():
		t.Fatalf("prepare pinned Skill approval: %v", operationContext.Err())
	}
	close(approveStart)
	// 审批锁 reviewer account 的等待者必须被撤权事务直接阻塞，这一锁图固定了 revoke -> approve 的线性化次序。
	if err := waitForPostgreSQLBlocker(operationContext, db, approvePID, revokePID); err != nil {
		t.Fatal(err)
	}

	if err := gate.Commit().Error; err != nil {
		t.Fatalf("release approval barrier assignment: %v", err)
	}
	gateReleased = true
	if err := <-revokeResult; err != nil {
		t.Fatalf("reviewer revocation did not commit first: %v", err)
	}
	if err := <-approveResult; !errors.Is(err, skill.ErrReviewCapabilityRequired) {
		t.Fatalf("approval crossed committed reviewer revocation: %v", err)
	}

	var facts struct {
		AssignmentStatus  string `gorm:"column:assignment_status"`
		AssignmentVersion int64  `gorm:"column:assignment_version"`
		ReviewStatus      string `gorm:"column:review_status"`
		ReviewVersion     int64  `gorm:"column:review_version"`
		PublicationAbsent bool   `gorm:"column:publication_absent"`
		ReceiptCount      int64  `gorm:"column:receipt_count"`
		SnapshotCount     int64  `gorm:"column:snapshot_count"`
		AuditCount        int64  `gorm:"column:audit_count"`
	}
	if err := db.Raw(`
SELECT
    (SELECT status FROM business.user_role_assignment WHERE id = ?) AS assignment_status,
    (SELECT version FROM business.user_role_assignment WHERE id = ?) AS assignment_version,
    (SELECT status FROM business.skill_review_submission WHERE id = ?) AS review_status,
    (SELECT version FROM business.skill_review_submission WHERE id = ?) AS review_version,
    (SELECT current_published_snapshot_id IS NULL FROM business.skill WHERE id = ?) AS publication_absent,
    (SELECT COUNT(*) FROM business.skill_command_receipt WHERE id = ?) AS receipt_count,
    (SELECT COUNT(*) FROM business.skill_published_snapshot WHERE id = ?) AS snapshot_count,
    (SELECT COUNT(*) FROM business.skill_governance_audit WHERE id = ?) AS audit_count`,
		assignmentID, assignmentID, reviewID, reviewID, aggregate.Skill.ID,
		approveCommand.ReceiptID, approveCommand.SnapshotID, approveCommand.AuditID).Scan(&facts).Error; err != nil {
		t.Fatalf("read approval/revocation linearization facts: %v", err)
	}
	if facts.AssignmentStatus != string(authorization.StatusRevoked) || facts.AssignmentVersion != 2 ||
		facts.ReviewStatus != string(skill.ReviewStatusReviewing) || facts.ReviewVersion != 1 || !facts.PublicationAbsent ||
		facts.ReceiptCount != 0 || facts.SnapshotCount != 0 || facts.AuditCount != 0 {
		t.Fatalf("revocation-first linearization left unauthorized approval facts: %+v", facts)
	}
}
