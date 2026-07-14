package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newSkillRepositoryTestDB 创建由 sqlmock 驱动的 Skill GORM Repository。
func newSkillRepositoryTestDB(t *testing.T) (*SkillRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create skill sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open skill gorm test database: %v", err)
	}
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	return repository, mock
}

// newSkillRepositoryUUIDv7 生成 Repository 测试 UUIDv7。
func newSkillRepositoryUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// skillRepositoryDefinition 返回六字段完整的最小结构化定义。
func skillRepositoryDefinition() skill.SkillDefinitionV1 {
	capability := skill.CapabilityGuidanceV1{Applicability: "enabled", Guidance: "执行稳定步骤"}
	return skill.SkillDefinitionV1{
		SchemaVersion: skill.DefinitionSchemaVersionV1, Name: "Repository Skill", Summary: "测试摘要", Category: "test",
		Tags: []string{}, InputDescription: "输入", OutputDescription: "输出", InvocationRules: "匹配时使用",
		PlanCreationSpec: capability, AnalyzeMaterials: capability, PlanStoryboard: capability,
		GenerateMedia: capability, WritePrompts: capability, AssembleOutput: capability,
		Examples: []skill.SkillExampleV1{}, StarterPrompts: []string{}, MarketListing: skill.MarketListingV1{},
		PublicToolRefs: []skill.PublicToolReferenceV1{},
	}
}

// newSkillRepositoryRevision 构造带 Canonical JSON 与摘要的不可变内容修订。
func newSkillRepositoryRevision(t *testing.T, skillID string, ownerID string, revisionNo int64, name string) skill.ContentRevision {
	t.Helper()
	definition := skillRepositoryDefinition()
	definition.Name = name
	normalized, err := skill.NormalizeDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := skill.CanonicalDefinitionV1(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return skill.ContentRevision{
		ID: newSkillRepositoryUUIDv7(t), SkillID: skillID, RevisionNo: revisionNo,
		Definition: normalized, CanonicalJSON: canonical, ContentDigest: digest,
		CreatedByUserID: ownerID, CreatedAt: time.Date(2026, 7, 14, int(revisionNo), 0, 0, 0, time.UTC),
	}
}

// newSkillRepositoryCreateAggregate 构造创建事务全部事实。
func newSkillRepositoryCreateAggregate(t *testing.T) skill.CreateAggregate {
	t.Helper()
	ownerID := newSkillRepositoryUUIDv7(t)
	skillID := newSkillRepositoryUUIDv7(t)
	draft := newSkillRepositoryRevision(t, skillID, ownerID, 1, "初始名称")
	now := draft.CreatedAt
	return skill.CreateAggregate{
		Skill: skill.Skill{
			ID: skillID, OwnerUserID: ownerID, CurrentDraftRevisionID: draft.ID,
			GovernanceStatus: skill.GovernanceStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Draft: draft,
		Receipt: skill.CommandReceipt{
			ID: newSkillRepositoryUUIDv7(t), ActorUserID: ownerID, CommandType: skill.CommandTypeCreate,
			ScopeID: ownerID, KeyDigest: sha256.Sum256([]byte("create-key")), SemanticDigest: sha256.Sum256([]byte("create-semantic")),
			ResultSkillID: skillID, ResultContentRevisionID: stringValuePointer(draft.ID),
			ResponseDraftRevisionID: draft.ID, ResponseGovernanceStatus: skill.GovernanceStatusActive, CreatedAt: now,
		},
	}
}

// skillReceiptRows 构造 GORM Take 回执查询结果，覆盖冻结安全响应字段。
func skillReceiptRows(receipt skill.CommandReceipt, semantic skill.Digest) *sqlmock.Rows {
	var reviewStatus any
	if receipt.ResponseReviewStatus != nil {
		reviewStatus = string(*receipt.ResponseReviewStatus)
	}
	return sqlmock.NewRows([]string{
		"id", "actor_user_id", "command_type", "scope_id", "key_digest", "semantic_digest",
		"result_skill_id", "result_content_revision_id", "result_review_submission_id", "result_published_snapshot_id",
		"response_draft_revision_id", "response_published_snapshot_id", "response_review_submission_id",
		"response_review_status", "response_review_reason_code", "response_review_updated_at", "response_governance_status", "created_at",
	}).AddRow(
		receipt.ID, receipt.ActorUserID, receipt.CommandType, receipt.ScopeID, receipt.KeyDigest[:], semantic[:],
		receipt.ResultSkillID, receipt.ResultContentRevisionID, receipt.ResultReviewSubmissionID, receipt.ResultPublishedSnapshotID,
		receipt.ResponseDraftRevisionID, receipt.ResponsePublishedSnapshotID, receipt.ResponseReviewSubmissionID,
		reviewStatus, receipt.ResponseReviewReasonCode, receipt.ResponseReviewUpdatedAt, string(receipt.ResponseGovernanceStatus), receipt.CreatedAt,
	)
}

// skillFrozenRows 构造通过 immutable revision 引用重建首次响应的一行结果。
func skillFrozenRows(aggregate skill.CreateAggregate) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"skill_id", "owner_user_id", "skill_created_at", "draft_revision_id", "draft_revision_no",
		"draft_definition_json", "draft_content_digest", "draft_created_by_user_id", "draft_created_at",
		"published_id", "published_source_revision_id", "published_review_id", "published_revision",
		"published_definition_json", "published_content_digest", "published_by_user_id", "published_at",
	}).AddRow(
		aggregate.Skill.ID, aggregate.Skill.OwnerUserID, aggregate.Skill.CreatedAt, aggregate.Draft.ID, aggregate.Draft.RevisionNo,
		aggregate.Draft.CanonicalJSON, aggregate.Draft.ContentDigest[:], aggregate.Draft.CreatedByUserID, aggregate.Draft.CreatedAt,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

// skillOwnerRows 构造当前 Owner 集合查询结果；当前用例不包含发布和审核。
func skillOwnerRows(states []skill.OwnerState) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"skill_id", "owner_user_id", "current_draft_revision_id", "current_published_snapshot_id", "publication_revision",
		"governance_status", "skill_version", "skill_created_at", "skill_updated_at", "draft_revision_no",
		"draft_definition_json", "draft_content_digest", "draft_created_by_user_id", "draft_created_at",
		"published_id", "published_source_revision_id", "published_review_id", "published_definition_json",
		"published_content_digest", "published_by_user_id", "published_at", "latest_review_id", "latest_review_revision_id",
		"latest_review_content_digest", "latest_review_status", "latest_review_reason_code", "latest_review_version",
		"latest_review_submitted_by", "latest_review_decided_by", "latest_review_submitted_at", "latest_review_decided_at", "latest_review_updated_at",
	})
	for _, state := range states {
		rows.AddRow(
			state.Skill.ID, state.Skill.OwnerUserID, state.Draft.ID, nil, 0,
			string(state.Skill.GovernanceStatus), state.Skill.Version, state.Skill.CreatedAt, state.Skill.UpdatedAt, state.Draft.RevisionNo,
			state.Draft.CanonicalJSON, state.Draft.ContentDigest[:], state.Draft.CreatedByUserID, state.Draft.CreatedAt,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		)
	}
	return rows
}

func TestSkillRepositoryCreateReplayUsesFrozenRevisionAfterCurrentDraftChanged(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_command_receipt"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT .* FROM "business"\."skill_command_receipt" WHERE .*actor_user_id.*command_type.*scope_id.*key_digest.*LIMIT`).
		WillReturnRows(skillReceiptRows(aggregate.Receipt, aggregate.Receipt.SemanticDigest))
	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*draft_record\.id = .*published_record\.id = .*WHERE skill_record\.id`).
		WillReturnRows(skillFrozenRows(aggregate))
	mock.ExpectCommit()

	state, replay, err := repository.Create(context.Background(), aggregate)
	if err != nil {
		t.Fatalf("replay frozen create: %v", err)
	}
	if !replay || state.Draft.ID != aggregate.Draft.ID || state.Draft.Definition.Name != "初始名称" || state.Published != nil || state.LatestReview != nil {
		t.Fatalf("create replay did not use frozen response: %+v", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet frozen replay SQL: %v", err)
	}
}

func TestSkillRepositoryAppendDraftCASConflictRollsBack(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)
	newDraft := newSkillRepositoryRevision(t, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, 2, "并发新草稿")
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "business"\."skill" SET .*current_draft_revision_id.*WHERE .*current_draft_revision_id`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	_, err := repository.AppendDraft(context.Background(), skill.AppendDraftCommand{
		SkillID: aggregate.Skill.ID, OwnerUserID: aggregate.Skill.OwnerUserID,
		ExpectedDraftRevisionID: aggregate.Draft.ID, Draft: newDraft, UpdatedAt: newDraft.CreatedAt,
	})
	if !errors.Is(err, skill.ErrDraftConflict) {
		t.Fatalf("expected draft conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet draft CAS SQL: %v", err)
	}
}

func TestSkillRepositorySubmitStaleDraftRollsBackClaimedReceipt(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	created := newSkillRepositoryCreateAggregate(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	now := created.Draft.CreatedAt.Add(time.Minute)
	reviewStatus := skill.ReviewStatusReviewing
	aggregate := skill.SubmitReviewAggregate{
		ExpectedDraftRevisionID: "",
		Review: skill.ReviewSubmission{
			ID: reviewID, SkillID: created.Skill.ID, ContentRevisionID: created.Draft.ID,
			ContentDigest: created.Draft.ContentDigest, Status: reviewStatus, Version: 1,
			SubmittedByUserID: created.Skill.OwnerUserID, SubmittedAt: now, UpdatedAt: now,
		},
		Receipt: skill.CommandReceipt{
			ID: newSkillRepositoryUUIDv7(t), ActorUserID: created.Skill.OwnerUserID, CommandType: skill.CommandTypeSubmitReview,
			ScopeID: created.Skill.ID, KeyDigest: sha256.Sum256([]byte("stale-submit-key")), SemanticDigest: sha256.Sum256([]byte("stale-if-match")),
			ResultSkillID: created.Skill.ID, ResultContentRevisionID: stringValuePointer(created.Draft.ID),
			ResultReviewSubmissionID: stringValuePointer(reviewID), ResponseDraftRevisionID: created.Draft.ID,
			ResponseReviewSubmissionID: stringValuePointer(reviewID), ResponseReviewStatus: &reviewStatus,
			ResponseReviewUpdatedAt: timeValuePointer(now), ResponseGovernanceStatus: skill.GovernanceStatusActive, CreatedAt: now,
		},
	}
	state := skill.OwnerState{Skill: created.Skill, Draft: created.Draft}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_command_receipt"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id`).
		WillReturnRows(skillOwnerRows([]skill.OwnerState{state}))
	mock.ExpectRollback()

	_, err := repository.SubmitReview(context.Background(), aggregate)
	if !errors.Is(err, skill.ErrDraftConflict) {
		t.Fatalf("expected stale submit draft conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("stale submit did not rollback claimed receipt: %v", err)
	}
}

func TestSkillRepositoryListOwnedUsesOneSQLForOneHundredRows(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	ownerID := newSkillRepositoryUUIDv7(t)
	states := make([]skill.OwnerState, 0, 100)
	for index := range 100 {
		skillID := newSkillRepositoryUUIDv7(t)
		draft := newSkillRepositoryRevision(t, skillID, ownerID, 1, fmt.Sprintf("Skill %03d", index))
		states = append(states, skill.OwnerState{
			Skill: skill.Skill{
				ID: skillID, OwnerUserID: ownerID, CurrentDraftRevisionID: draft.ID,
				GovernanceStatus: skill.GovernanceStatusActive, Version: 1,
				CreatedAt: draft.CreatedAt, UpdatedAt: draft.CreatedAt.Add(time.Duration(index) * time.Second),
			},
			Draft: draft,
		})
	}
	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*LEFT JOIN LATERAL.*WHERE skill_record\.owner_user_id.*LIMIT`).
		WillReturnRows(skillOwnerRows(states))
	page, err := repository.ListOwned(context.Background(), ownerID, nil, 100)
	if err != nil {
		t.Fatalf("list 100 owner skills: %v", err)
	}
	if len(page.Items) != 100 || page.HasMore {
		t.Fatalf("unexpected owner page: count=%d has_more=%v", len(page.Items), page.HasMore)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("list introduced extra SQL/N+1: %v", err)
	}
}

func TestSkillRepositoryApproveAuditFailureRollsBackPublishedPointer(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	reviewerID := newSkillRepositoryUUIDv7(t)
	snapshotID := newSkillRepositoryUUIDv7(t)
	oldSnapshotID := newSkillRepositoryUUIDv7(t)
	now := aggregate.Skill.CreatedAt.Add(time.Hour)
	review := skill.ReviewSubmission{ID: reviewID, SkillID: aggregate.Skill.ID, ContentRevisionID: aggregate.Draft.ID,
		ContentDigest: aggregate.Draft.ContentDigest, Status: skill.ReviewStatusReviewing, Version: 1}
	command := skill.ApproveAndPublishCommand{
		ReviewID: reviewID, ReviewerUserID: reviewerID, SnapshotID: snapshotID,
		ReceiptID: newSkillRepositoryUUIDv7(t), RequestID: newSkillRepositoryUUIDv7(t),
		KeyDigest: sha256.Sum256([]byte("approve-key")), SemanticDigest: sha256.Sum256([]byte("approve-semantic")),
		IfMatch: skill.ReviewETag(review), AuditID: newSkillRepositoryUUIDv7(t), DecidedAt: now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business\.user_account .*FOR UPDATE`).WithArgs(reviewerID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(reviewerID))
	mock.ExpectQuery(`SELECT id FROM business\.user_role_assignment .*FOR UPDATE`).WithArgs(reviewerID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(newSkillRepositoryUUIDv7(t)))
	mock.ExpectQuery(`SELECT \* FROM "business"\."skill_command_receipt"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`(?s)FROM business\.skill_review_submission AS review_record.*FOR UPDATE OF review_record, skill_record`).
		WillReturnRows(sqlmock.NewRows([]string{
			"review_id", "skill_id", "owner_user_id", "content_revision_id", "review_content_digest", "review_status", "review_version",
			"submitted_by_user_id", "submitted_at", "revision_no", "definition_schema_version", "definition_json",
			"revision_content_digest", "revision_created_at", "current_published_snapshot_id", "publication_revision", "governance_status", "skill_version",
		}).AddRow(
			reviewID, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, aggregate.Draft.ID, aggregate.Draft.ContentDigest[:],
			string(skill.ReviewStatusReviewing), int64(1), aggregate.Skill.OwnerUserID, now.Add(-time.Minute), aggregate.Draft.RevisionNo,
			skill.DefinitionSchemaVersionV1, aggregate.Draft.CanonicalJSON, aggregate.Draft.ContentDigest[:], aggregate.Draft.CreatedAt,
			oldSnapshotID, int64(1), string(skill.GovernanceStatusActive), int64(4),
		))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_command_receipt"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_published_snapshot"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "business"\."skill" SET .*current_published_snapshot_id`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "business"\."skill_review_submission" SET .*status`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_governance_audit"`)).
		WillReturnError(errors.New("injected audit write failure"))
	mock.ExpectRollback()

	_, err := repository.ApproveAndPublish(context.Background(), command)
	if !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("expected stable persistence error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("publish failure did not rollback transaction: %v", err)
	}
}

func TestFreezeSkillReceiptResponseUsesFinalTransactionState(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	aggregate := newSkillRepositoryCreateAggregate(t)
	finalDraft := newSkillRepositoryRevision(t, aggregate.Skill.ID, aggregate.Skill.OwnerUserID, 2, "事务最终草稿")
	reviewID := newSkillRepositoryUUIDv7(t)
	snapshotID := newSkillRepositoryUUIDv7(t)
	reviewTime := finalDraft.CreatedAt.Add(time.Minute)
	review := skill.ReviewSubmission{
		ID: reviewID, SkillID: aggregate.Skill.ID, ContentRevisionID: finalDraft.ID,
		ContentDigest: finalDraft.ContentDigest, Status: skill.ReviewStatusReviewing,
		SubmittedByUserID: aggregate.Skill.OwnerUserID, SubmittedAt: reviewTime, UpdatedAt: reviewTime,
	}
	state := skill.OwnerState{
		Skill: skill.Skill{
			ID: aggregate.Skill.ID, OwnerUserID: aggregate.Skill.OwnerUserID, CurrentDraftRevisionID: finalDraft.ID,
			CurrentPublishedSnapshotID: &snapshotID, PublicationRevision: 1,
			GovernanceStatus: skill.GovernanceStatusSuspended, Version: 5,
			CreatedAt: aggregate.Skill.CreatedAt, UpdatedAt: reviewTime,
		},
		Draft: finalDraft,
		Published: &skill.PublishedSnapshot{
			ID: snapshotID, SkillID: aggregate.Skill.ID, SourceContentRevisionID: aggregate.Draft.ID,
			ReviewSubmissionID: reviewID, PublicationRevision: 1, Definition: aggregate.Draft.Definition,
			CanonicalJSON: aggregate.Draft.CanonicalJSON, ContentDigest: aggregate.Draft.ContentDigest,
			PublishedByUserID: newSkillRepositoryUUIDv7(t), PublishedAt: reviewTime,
		},
		LatestReview: &review,
	}
	mock.ExpectExec(`UPDATE "business"\."skill_command_receipt" SET .*response_draft_revision_id.*response_governance_status`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := freezeSkillReceiptResponse(repository.db, aggregate.Receipt.ID, state); err != nil {
		t.Fatalf("freeze final receipt response: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("receipt was not overwritten with final transaction state: %v", err)
	}
}
