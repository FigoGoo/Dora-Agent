package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

func TestSkillRepositoryListReviewQueueUsesFrozenRevisionAndOldestFirst(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	submittedAt := aggregate.Skill.CreatedAt.Add(time.Minute)
	mock.ExpectQuery(`(?s)FROM business\.skill_review_submission AS review_record.*review_record\.status = \$1.*ORDER BY review_record\.submitted_at ASC, review_record\.id ASC LIMIT \$2`).
		WithArgs(skill.ReviewStatusReviewing, 21).
		WillReturnRows(sqlmock.NewRows([]string{
			"review_id", "skill_id", "name", "summary", "category", "status", "submitted_at",
			"review_content_digest", "revision_content_digest", "definition_schema_version",
		}).AddRow(reviewID, aggregate.Skill.ID, aggregate.Draft.Definition.Name, aggregate.Draft.Definition.Summary,
			aggregate.Draft.Definition.Category, string(skill.ReviewStatusReviewing), submittedAt,
			aggregate.Draft.ContentDigest[:], aggregate.Draft.ContentDigest[:], skill.DefinitionSchemaVersionV1))
	page, err := repository.ListReviewQueue(context.Background(), nil, 20)
	if err != nil || page.HasMore || len(page.Items) != 1 || page.Items[0].ReviewID != reviewID || page.Items[0].Name != aggregate.Draft.Definition.Name {
		t.Fatalf("invalid review queue: page=%+v err=%v", page, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("queue query drifted: %v", err)
	}
}

func TestSkillRepositoryFindReviewDetailRevalidatesSubmittedAndPublished(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	publishedID := newSkillRepositoryUUIDv7(t)
	publisherID := newSkillRepositoryUUIDv7(t)
	now := aggregate.Skill.CreatedAt.Add(time.Hour)
	mock.ExpectQuery(`(?s)FROM business\.skill_review_submission AS review_record.*LEFT JOIN business\.skill AS skill_record.*LEFT JOIN business\.skill_content_revision AS content_record.*LEFT JOIN business\.skill_published_snapshot AS published_record.*WHERE review_record\.id = \$1`).
		WithArgs(reviewID).
		WillReturnRows(sqlmock.NewRows([]string{
			"review_id", "skill_id", "joined_skill_id", "owner_user_id", "current_published_snapshot_id",
			"content_revision_id", "joined_content_revision_id", "review_content_digest", "review_status", "review_version",
			"safe_reason_code", "submitted_by_user_id", "decided_by_user_id", "submitted_at", "decided_at", "updated_at",
			"revision_no", "definition_schema_version", "definition_json", "revision_content_digest", "revision_created_by_user_id", "revision_created_at",
			"published_id", "published_source_revision_id", "published_review_id", "published_revision", "published_definition_schema_version",
			"published_definition_json", "published_content_digest", "published_by_user_id", "published_at",
		}).AddRow(
			reviewID, aggregate.Skill.ID, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, publishedID,
			aggregate.Draft.ID, aggregate.Draft.ID, aggregate.Draft.ContentDigest[:],
			string(skill.ReviewStatusReviewing), int64(2), nil, aggregate.Skill.OwnerUserID, nil, now, nil, now,
			aggregate.Draft.RevisionNo, skill.DefinitionSchemaVersionV1, aggregate.Draft.CanonicalJSON, aggregate.Draft.ContentDigest[:],
			aggregate.Skill.OwnerUserID, aggregate.Draft.CreatedAt, publishedID, aggregate.Draft.ID, reviewID, int64(1),
			skill.DefinitionSchemaVersionV1, aggregate.Draft.CanonicalJSON, aggregate.Draft.ContentDigest[:], publisherID, now,
		))
	detail, err := repository.FindReviewDetail(context.Background(), reviewID)
	if err != nil || detail.Review.ID != reviewID || detail.Definition.Name != aggregate.Draft.Definition.Name ||
		detail.CurrentPublished == nil || detail.CurrentPublished.ID != publishedID {
		t.Fatalf("invalid review detail: detail=%+v err=%v", detail, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("detail query drifted: %v", err)
	}
}

func TestReviewDetailFromReadDTOFailsClosedForBrokenLogicalAssociations(t *testing.T) {
	aggregate := newSkillRepositoryCreateAggregate(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	publishedID := newSkillRepositoryUUIDv7(t)
	joinedSkillID := aggregate.Skill.ID
	ownerID := aggregate.Skill.OwnerUserID
	joinedRevisionID := aggregate.Draft.ID
	revisionNo := aggregate.Draft.RevisionNo
	schemaVersion := skill.DefinitionSchemaVersionV1
	createdBy := aggregate.Draft.CreatedByUserID
	createdAt := aggregate.Draft.CreatedAt
	now := createdAt.Add(time.Hour)
	base := skillReviewDetailReadDTO{
		ReviewID: reviewID, SkillID: aggregate.Skill.ID, JoinedSkillID: &joinedSkillID, OwnerUserID: &ownerID,
		ContentRevisionID: aggregate.Draft.ID, JoinedContentRevisionID: &joinedRevisionID,
		ReviewContentDigest: aggregate.Draft.ContentDigest[:], ReviewStatus: string(skill.ReviewStatusReviewing), ReviewVersion: 1,
		SubmittedByUserID: aggregate.Skill.OwnerUserID, SubmittedAt: now, UpdatedAt: now,
		RevisionNo: &revisionNo, DefinitionSchemaVersion: &schemaVersion, DefinitionJSON: aggregate.Draft.CanonicalJSON,
		RevisionContentDigest: aggregate.Draft.ContentDigest[:], RevisionCreatedByUserID: &createdBy, RevisionCreatedAt: &createdAt,
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*skillReviewDetailReadDTO)
	}{
		{name: "skill missing", mutate: func(record *skillReviewDetailReadDTO) { record.JoinedSkillID = nil }},
		{name: "submitted revision missing", mutate: func(record *skillReviewDetailReadDTO) { record.JoinedContentRevisionID = nil }},
		{name: "current published pointer dangling", mutate: func(record *skillReviewDetailReadDTO) { record.CurrentPublishedSnapshotID = &publishedID }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			record := base
			testCase.mutate(&record)
			_, err := reviewDetailFromReadDTO(record)
			if !errors.Is(err, skill.ErrPersistence) {
				t.Fatalf("broken logical association must fail closed: %v", err)
			}
		})
	}
}

func TestSkillRepositoryDecisionReplayLocksAuthorityBeforeReceipt(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	reviewerID := newSkillRepositoryUUIDv7(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	skillID := newSkillRepositoryUUIDv7(t)
	snapshotID := newSkillRepositoryUUIDv7(t)
	receiptID := newSkillRepositoryUUIDv7(t)
	requestID := newSkillRepositoryUUIDv7(t)
	assignmentID := newSkillRepositoryUUIDv7(t)
	now := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	keyDigest := sha256.Sum256([]byte("key"))
	semanticDigest := sha256.Sum256([]byte("semantic"))
	command := skill.ApproveAndPublishCommand{
		ReviewID: reviewID, ReviewerUserID: reviewerID, SnapshotID: newSkillRepositoryUUIDv7(t), ReceiptID: newSkillRepositoryUUIDv7(t),
		RequestID: newSkillRepositoryUUIDv7(t), KeyDigest: keyDigest, SemanticDigest: semanticDigest,
		IfMatch: `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`, AuditID: newSkillRepositoryUUIDv7(t), DecidedAt: now.Add(time.Hour),
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business\.user_account .*FOR UPDATE`).WithArgs(reviewerID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(reviewerID))
	mock.ExpectQuery(`SELECT id FROM business\.user_role_assignment .*FOR UPDATE`).WithArgs(reviewerID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(assignmentID))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "business"."skill_command_receipt"`)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "actor_user_id", "command_type", "scope_id", "key_digest", "semantic_digest", "result_skill_id",
			"result_review_submission_id", "result_published_snapshot_id", "response_draft_revision_id",
			"response_review_status", "response_review_updated_at", "response_governance_status", "request_id", "created_at",
		}).AddRow(receiptID, reviewerID, skill.CommandTypeApproveAndPublish, reviewID, keyDigest[:], semanticDigest[:], skillID,
			reviewID, snapshotID, newSkillRepositoryUUIDv7(t), string(skill.ReviewStatusApproved), now,
			string(skill.GovernanceStatusActive), requestID, now))
	mock.ExpectCommit()
	result, err := repository.ApproveAndPublish(context.Background(), command)
	if err != nil || !result.IdempotentReplay || result.ReviewID != reviewID || result.PublishedSnapshotID != snapshotID || !result.DecidedAt.Equal(now) {
		t.Fatalf("invalid dedicated receipt replay: result=%+v err=%v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("replay order drifted or touched Review: %v", err)
	}
}
