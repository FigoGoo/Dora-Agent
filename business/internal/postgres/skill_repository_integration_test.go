package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

// TestSkillRepositoryPostgreSQLW1Semantics 使用显式 PostgreSQL 测试库验证 W1 创建、CAS、审核发布、并发幂等和失败回滚。
func TestSkillRepositoryPostgreSQLW1Semantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	aggregate := newSkillRepositoryCreateAggregate(t)

	const concurrency = 100
	results := make(chan bool, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			_, replay, createErr := repository.Create(context.Background(), aggregate)
			if createErr != nil {
				errorsChannel <- createErr
				return
			}
			results <- replay
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for createErr := range errorsChannel {
		t.Errorf("concurrent Skill create failed: %v", createErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	createdCount := 0
	replayedCount := 0
	for replay := range results {
		if replay {
			replayedCount++
		} else {
			createdCount++
		}
	}
	if createdCount != 1 || replayedCount != concurrency-1 {
		t.Fatalf("unexpected concurrent create dispositions: created=%d replayed=%d", createdCount, replayedCount)
	}
	var factCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM business.skill WHERE id = ?`, aggregate.Skill.ID).Scan(&factCount).Error; err != nil || factCount != 1 {
		t.Fatalf("same-key create did not converge: count=%d err=%v", factCount, err)
	}

	draftTwo := newSkillRepositoryRevision(t, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, 2, "第二版草稿")
	state, err := repository.AppendDraft(context.Background(), skill.AppendDraftCommand{
		SkillID: aggregate.Skill.ID, OwnerUserID: aggregate.Skill.OwnerUserID,
		ExpectedDraftRevisionID: aggregate.Draft.ID, Draft: draftTwo, UpdatedAt: draftTwo.CreatedAt,
	})
	if err != nil || state.Draft.ID != draftTwo.ID {
		t.Fatalf("append draft two: state=%+v err=%v", state, err)
	}
	if _, err := repository.AppendDraft(context.Background(), skill.AppendDraftCommand{
		SkillID: aggregate.Skill.ID, OwnerUserID: aggregate.Skill.OwnerUserID,
		ExpectedDraftRevisionID: aggregate.Draft.ID,
		Draft:                   newSkillRepositoryRevision(t, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, 3, "丢失更新候选"),
		UpdatedAt:               draftTwo.CreatedAt.Add(time.Second),
	}); !errors.Is(err, skill.ErrDraftConflict) {
		t.Fatalf("expected stale draft CAS conflict, got %v", err)
	}

	reviewOneID := newSkillRepositoryUUIDv7(t)
	reviewOneAt := draftTwo.CreatedAt.Add(time.Minute)
	reviewOneStatus := skill.ReviewStatusReviewing
	submitOne := skill.SubmitReviewAggregate{
		ExpectedDraftRevisionID: draftTwo.ID,
		Review: skill.ReviewSubmission{
			ID: reviewOneID, SkillID: aggregate.Skill.ID, ContentRevisionID: draftTwo.ID,
			ContentDigest: draftTwo.ContentDigest, Status: reviewOneStatus, Version: 1,
			SubmittedByUserID: aggregate.Skill.OwnerUserID, SubmittedAt: reviewOneAt, UpdatedAt: reviewOneAt,
		},
		Receipt: skill.CommandReceipt{
			ID: newSkillRepositoryUUIDv7(t), ActorUserID: aggregate.Skill.OwnerUserID, CommandType: skill.CommandTypeSubmitReview,
			ScopeID: aggregate.Skill.ID, KeyDigest: sha256.Sum256([]byte("submit-one")), SemanticDigest: sha256.Sum256([]byte("submit-one-semantic")),
			ResultSkillID: aggregate.Skill.ID, ResultContentRevisionID: stringValuePointer(draftTwo.ID),
			ResultReviewSubmissionID: stringValuePointer(reviewOneID), ResponseDraftRevisionID: draftTwo.ID,
			ResponseReviewSubmissionID: stringValuePointer(reviewOneID), ResponseReviewStatus: &reviewOneStatus,
			ResponseReviewUpdatedAt: timeValuePointer(reviewOneAt), ResponseGovernanceStatus: skill.GovernanceStatusActive, CreatedAt: reviewOneAt,
		},
	}

	// 首次新 Key 的 If-Match 不匹配时，回执必须随 DraftConflict 一起回滚；同一个 Key 随后绑定当前 ETag 可以正常执行。
	staleSubmit := submitOne
	staleSubmit.ExpectedDraftRevisionID = ""
	staleSubmit.Review.ID = newSkillRepositoryUUIDv7(t)
	staleSubmit.Receipt.ID = newSkillRepositoryUUIDv7(t)
	staleSubmit.Receipt.SemanticDigest = sha256.Sum256([]byte("submit-one-stale-if-match"))
	staleSubmit.Receipt.ResultReviewSubmissionID = stringValuePointer(staleSubmit.Review.ID)
	staleSubmit.Receipt.ResponseReviewSubmissionID = stringValuePointer(staleSubmit.Review.ID)
	if _, err := repository.SubmitReview(context.Background(), staleSubmit); !errors.Is(err, skill.ErrDraftConflict) {
		t.Fatalf("expected stale submit review draft conflict, got %v", err)
	}
	var staleReceiptCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM business.skill_command_receipt WHERE actor_user_id = ? AND command_type = ? AND scope_id = ? AND key_digest = ?`,
		staleSubmit.Receipt.ActorUserID, staleSubmit.Receipt.CommandType, staleSubmit.Receipt.ScopeID, staleSubmit.Receipt.KeyDigest[:]).
		Scan(&staleReceiptCount).Error; err != nil || staleReceiptCount != 0 {
		t.Fatalf("stale submit receipt was not rolled back: count=%d err=%v", staleReceiptCount, err)
	}

	// 相同 Key、相同 If-Match 的 100 并发只允许一个审核事实，其余都重放冻结响应。
	submitResults := make(chan bool, concurrency)
	submitErrors := make(chan error, concurrency)
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, submitErr := repository.SubmitReview(context.Background(), submitOne)
			if submitErr != nil {
				submitErrors <- submitErr
				return
			}
			submitResults <- result.IdempotentReplay
		}()
	}
	waitGroup.Wait()
	close(submitResults)
	close(submitErrors)
	for submitErr := range submitErrors {
		t.Errorf("concurrent submit review failed: %v", submitErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	submittedCount := 0
	submitReplayCount := 0
	for replay := range submitResults {
		if replay {
			submitReplayCount++
		} else {
			submittedCount++
		}
	}
	if submittedCount != 1 || submitReplayCount != concurrency-1 {
		t.Fatalf("unexpected concurrent submit dispositions: submitted=%d replayed=%d", submittedCount, submitReplayCount)
	}
	var reviewFactCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM business.skill_review_submission WHERE skill_id = ? AND content_revision_id = ?`,
		aggregate.Skill.ID, draftTwo.ID).Scan(&reviewFactCount).Error; err != nil || reviewFactCount != 1 {
		t.Fatalf("same-key submit did not converge: count=%d err=%v", reviewFactCount, err)
	}

	reviewerID := newSkillRepositoryUUIDv7(t)
	grantActorID := newSkillRepositoryUUIDv7(t)
	for _, accountID := range []string{reviewerID, grantActorID} {
		if err := db.Create(&userAccountModel{
			ID: accountID, DisplayName: "Reviewer integration fixture", UserType: "personal", Status: "active", Version: 1,
			CreatedAt: reviewOneAt, UpdatedAt: reviewOneAt,
		}).Error; err != nil {
			t.Fatalf("create reviewer authority fixture: %v", err)
		}
	}
	authorizationRepository, err := NewAuthorizationRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizationRepository.Grant(context.Background(), authorization.Assignment{
		ID: newSkillRepositoryUUIDv7(t), UserID: reviewerID, Role: authorization.RoleSkillReviewer,
		Status: authorization.StatusActive, Version: 1, AssignedByUserID: grantActorID,
		AssignmentReasonCode: "skill_repository_integration", ApprovalReference: "skill-repository-integration-v1",
		AssignedAt: reviewOneAt, UpdatedAt: reviewOneAt,
	}); err != nil {
		t.Fatalf("grant reviewer authority fixture: %v", err)
	}
	snapshotOneID := newSkillRepositoryUUIDv7(t)
	auditOneID := newSkillRepositoryUUIDv7(t)
	approveOneAt := reviewOneAt.Add(time.Minute)
	approveOne := skill.ApproveAndPublishCommand{
		ReviewID: reviewOneID, ReviewerUserID: reviewerID, SnapshotID: snapshotOneID,
		ReceiptID: newSkillRepositoryUUIDv7(t), RequestID: newSkillRepositoryUUIDv7(t),
		KeyDigest: sha256.Sum256([]byte("approve-one")), SemanticDigest: sha256.Sum256([]byte("approve-one-semantic")),
		IfMatch: skill.ReviewETag(submitOne.Review), AuditID: auditOneID, DecidedAt: approveOneAt,
	}
	publishedDecision, err := repository.ApproveAndPublish(context.Background(), approveOne)
	if err != nil || publishedDecision.IdempotentReplay || publishedDecision.PublishedSnapshotID != snapshotOneID || publishedDecision.Status != skill.ReviewStatusApproved {
		t.Fatalf("approve and publish one: decision=%+v err=%v", publishedDecision, err)
	}
	var requestAuditFact struct {
		ReceiptActorUserID string `gorm:"column:receipt_actor_user_id"`
		ReceiptScopeID     string `gorm:"column:receipt_scope_id"`
		ReceiptRequestID   string `gorm:"column:receipt_request_id"`
		AuditActorUserID   string `gorm:"column:audit_actor_user_id"`
		AuditReviewID      string `gorm:"column:audit_review_id"`
		AuditRequestID     string `gorm:"column:audit_request_id"`
	}
	if err := db.Raw(`
SELECT
    receipt.actor_user_id AS receipt_actor_user_id,
    receipt.scope_id AS receipt_scope_id,
    receipt.request_id AS receipt_request_id,
    audit.actor_user_id AS audit_actor_user_id,
    audit.review_submission_id AS audit_review_id,
    audit.request_id AS audit_request_id
FROM business.skill_command_receipt AS receipt
JOIN business.skill_governance_audit AS audit ON audit.id = ?
WHERE receipt.id = ?`, auditOneID, approveOne.ReceiptID).Scan(&requestAuditFact).Error; err != nil {
		t.Fatalf("read decision request audit fact: %v", err)
	}
	if requestAuditFact.ReceiptActorUserID != reviewerID || requestAuditFact.ReceiptScopeID != reviewOneID ||
		requestAuditFact.AuditActorUserID != reviewerID || requestAuditFact.AuditReviewID != reviewOneID ||
		requestAuditFact.ReceiptRequestID != approveOne.RequestID || requestAuditFact.AuditRequestID != approveOne.RequestID {
		t.Fatalf("receipt/audit request identity drifted: %+v", requestAuditFact)
	}

	// 提审回执必须在审核已经批准后仍先于当前状态校验重放首次 reviewing 响应。
	replayedSubmit, err := repository.SubmitReview(context.Background(), submitOne)
	if err != nil || !replayedSubmit.IdempotentReplay || replayedSubmit.ReviewID != reviewOneID ||
		replayedSubmit.State.LatestReview == nil || replayedSubmit.State.LatestReview.Status != skill.ReviewStatusReviewing {
		t.Fatalf("submit replay drifted after approval: result=%+v err=%v", replayedSubmit, err)
	}

	// 同键重放仍按回执引用返回第一次批准时的草稿与状态。
	replayedPublished, err := repository.ApproveAndPublish(context.Background(), approveOne)
	if err != nil || !replayedPublished.IdempotentReplay || replayedPublished.PublishedSnapshotID != snapshotOneID {
		t.Fatalf("approve replay drifted: decision=%+v err=%v", replayedPublished, err)
	}

	draftThree := newSkillRepositoryRevision(t, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, 3, "第三版草稿")
	if _, err := repository.AppendDraft(context.Background(), skill.AppendDraftCommand{
		SkillID: aggregate.Skill.ID, OwnerUserID: aggregate.Skill.OwnerUserID,
		ExpectedDraftRevisionID: draftTwo.ID, Draft: draftThree, UpdatedAt: draftThree.CreatedAt,
	}); err != nil {
		t.Fatalf("append draft three: %v", err)
	}
	// 当前草稿已经切换到新 ETag 后，旧 ETag 的同键提交仍必须优先重放第一次冻结的 draft two 响应。
	replayedOldETag, err := repository.SubmitReview(context.Background(), submitOne)
	if err != nil || !replayedOldETag.IdempotentReplay || replayedOldETag.ReviewID != reviewOneID ||
		replayedOldETag.State.Draft.ID != draftTwo.ID || replayedOldETag.State.LatestReview == nil ||
		replayedOldETag.State.LatestReview.Status != skill.ReviewStatusReviewing {
		t.Fatalf("old ETag submit replay drifted after draft update: result=%+v err=%v", replayedOldETag, err)
	}
	reviewTwoID := newSkillRepositoryUUIDv7(t)
	reviewTwoAt := draftThree.CreatedAt.Add(time.Minute)
	submitTwo := submitOne
	submitTwo.ExpectedDraftRevisionID = draftThree.ID
	submitTwo.Review = skill.ReviewSubmission{
		ID: reviewTwoID, SkillID: aggregate.Skill.ID, ContentRevisionID: draftThree.ID,
		ContentDigest: draftThree.ContentDigest, Status: skill.ReviewStatusReviewing, Version: 1,
		SubmittedByUserID: aggregate.Skill.OwnerUserID, SubmittedAt: reviewTwoAt, UpdatedAt: reviewTwoAt,
	}
	submitTwo.Receipt.ID = newSkillRepositoryUUIDv7(t)
	submitTwo.Receipt.KeyDigest = sha256.Sum256([]byte("submit-two"))
	submitTwo.Receipt.SemanticDigest = sha256.Sum256([]byte("submit-two-semantic"))
	submitTwo.Receipt.ResultContentRevisionID = stringValuePointer(draftThree.ID)
	submitTwo.Receipt.ResultReviewSubmissionID = stringValuePointer(reviewTwoID)
	submitTwo.Receipt.ResponseDraftRevisionID = draftThree.ID
	submitTwo.Receipt.ResponsePublishedSnapshotID = stringValuePointer(snapshotOneID)
	submitTwo.Receipt.ResponseReviewSubmissionID = stringValuePointer(reviewTwoID)
	submitTwo.Receipt.ResponseReviewUpdatedAt = timeValuePointer(reviewTwoAt)
	submitTwo.Receipt.CreatedAt = reviewTwoAt
	if _, err := repository.SubmitReview(context.Background(), submitTwo); err != nil {
		t.Fatalf("submit review two: %v", err)
	}

	// 复用已存在 audit ID 在事务最后一步制造失败，快照、指针和审核决定必须全部回滚。
	snapshotTwoID := newSkillRepositoryUUIDv7(t)
	approveTwoAt := reviewTwoAt.Add(time.Minute)
	approveTwo := approveOne
	approveTwo.ReviewID = reviewTwoID
	approveTwo.SnapshotID = snapshotTwoID
	approveTwo.AuditID = auditOneID
	approveTwo.DecidedAt = approveTwoAt
	approveTwo.ReceiptID = newSkillRepositoryUUIDv7(t)
	approveTwo.RequestID = newSkillRepositoryUUIDv7(t)
	approveTwo.KeyDigest = sha256.Sum256([]byte("approve-two"))
	approveTwo.SemanticDigest = sha256.Sum256([]byte("approve-two-semantic"))
	approveTwo.IfMatch = skill.ReviewETag(submitTwo.Review)
	if _, err := repository.ApproveAndPublish(context.Background(), approveTwo); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("expected injected audit conflict to fail publish, got %v", err)
	}
	afterFailure, err := repository.FindOwnedByID(context.Background(), aggregate.Skill.ID, aggregate.Skill.OwnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Published == nil || afterFailure.Published.ID != snapshotOneID || afterFailure.Skill.PublicationRevision != 1 ||
		afterFailure.LatestReview == nil || afterFailure.LatestReview.ID != reviewTwoID || afterFailure.LatestReview.Status != skill.ReviewStatusReviewing {
		t.Fatalf("failed publication changed old snapshot/review: %+v", afterFailure)
	}
}
