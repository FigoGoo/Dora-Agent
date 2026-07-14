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

type governanceRepositoryFixture struct {
	SkillID     string
	DraftID     string
	SnapshotID  string
	SourceID    string
	ReviewID    string
	ReviewerID  string
	PublishedAt time.Time
	Canonical   []byte
	Digest      skill.Digest
	Definition  skill.SkillDefinitionV1
}

func newGovernanceRepositoryFixture(t *testing.T) governanceRepositoryFixture {
	t.Helper()
	definition, err := skill.NormalizeDefinitionV1(skillRepositoryDefinition())
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := skill.CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	return governanceRepositoryFixture{
		SkillID: newSkillRepositoryUUIDv7(t), DraftID: newSkillRepositoryUUIDv7(t),
		SnapshotID: newSkillRepositoryUUIDv7(t), SourceID: newSkillRepositoryUUIDv7(t),
		ReviewID: newSkillRepositoryUUIDv7(t), ReviewerID: newSkillRepositoryUUIDv7(t),
		PublishedAt: time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC),
		Canonical:   canonical, Digest: digest, Definition: definition,
	}
}

func governanceReadRows(fixture governanceRepositoryFixture, status skill.GovernanceStatus, epoch int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"skill_id", "current_draft_revision_id", "current_published_snapshot_id", "publication_revision",
		"governance_status", "governance_epoch", "skill_version", "published_id", "published_skill_id",
		"published_source_revision_id", "published_review_id", "published_publication_revision",
		"published_definition_schema", "published_definition_json", "published_content_digest",
		"published_by_user_id", "published_at",
	}).AddRow(
		fixture.SkillID, fixture.DraftID, fixture.SnapshotID, int64(1), status, epoch, int64(4),
		fixture.SnapshotID, fixture.SkillID, fixture.SourceID, fixture.ReviewID, int64(1),
		skill.DefinitionSchemaVersionV1, fixture.Canonical, fixture.Digest[:], fixture.ReviewerID, fixture.PublishedAt,
	)
}

func TestSkillGovernanceRepositoryListAndDetailUseCurrentPublishedSnapshot(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newGovernanceRepositoryFixture(t)
	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*LEFT JOIN business\.skill_published_snapshot.*WHERE skill_record\.governance_status = \$1.*current_published_snapshot_id IS NOT NULL.*ORDER BY published_record\.published_at DESC NULLS FIRST.*LIMIT \$2`).
		WithArgs(skill.GovernanceStatusActive, 21).
		WillReturnRows(governanceReadRows(fixture, skill.GovernanceStatusActive, 1))
	page, err := repository.ListGovernance(context.Background(), skill.GovernanceStatusActive, nil, 20)
	if err != nil || page.HasMore || len(page.Items) != 1 || page.Items[0].SkillID != fixture.SkillID ||
		page.Items[0].PublishedSnapshotID != fixture.SnapshotID || page.Items[0].Name != fixture.Definition.Name {
		t.Fatalf("list governance page=%+v err=%v", page, err)
	}

	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id = \$1`).
		WithArgs(fixture.SkillID).
		WillReturnRows(governanceReadRows(fixture, skill.GovernanceStatusActive, 1))
	state, err := repository.FindGovernanceDetail(context.Background(), fixture.SkillID)
	if err != nil || state.CurrentPublishedSnapshotID != fixture.SnapshotID || state.Published.ContentDigest != fixture.Digest ||
		state.GovernanceStatus != skill.GovernanceStatusActive || state.GovernanceEpoch != 1 {
		t.Fatalf("find governance state=%+v err=%v", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillGovernanceRepositoryDistinguishesUnpublishedFromDanglingPointer(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newGovernanceRepositoryFixture(t)
	baseColumns := []string{
		"skill_id", "current_draft_revision_id", "current_published_snapshot_id", "publication_revision",
		"governance_status", "governance_epoch", "skill_version", "published_id", "published_skill_id",
		"published_source_revision_id", "published_review_id", "published_publication_revision",
		"published_definition_schema", "published_definition_json", "published_content_digest", "published_by_user_id", "published_at",
	}

	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id = \$1`).WithArgs(fixture.SkillID).
		WillReturnRows(sqlmock.NewRows(baseColumns).AddRow(
			fixture.SkillID, fixture.DraftID, nil, int64(0), skill.GovernanceStatusActive, int64(1), int64(1),
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		))
	if _, err := repository.FindGovernanceDetail(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrGovernanceNotFound) {
		t.Fatalf("unpublished error = %v", err)
	}

	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id = \$1`).WithArgs(fixture.SkillID).
		WillReturnRows(sqlmock.NewRows(baseColumns).AddRow(
			fixture.SkillID, fixture.DraftID, fixture.SnapshotID, int64(1), skill.GovernanceStatusActive, int64(1), int64(2),
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		))
	if _, err := repository.FindGovernanceDetail(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("dangling pointer error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillGovernanceRepositoryTransitionWritesOneReceiptAndAudit(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newGovernanceRepositoryFixture(t)
	governorID := newSkillRepositoryUUIDv7(t)
	requestID := newSkillRepositoryUUIDv7(t)
	receiptID := newSkillRepositoryUUIDv7(t)
	auditID := newSkillRepositoryUUIDv7(t)
	ifMatch, _ := skill.GovernanceETag(fixture.SkillID, fixture.SnapshotID, skill.GovernanceStatusActive, 1)
	command := skill.GovernanceTransitionRepositoryCommand{
		GovernorUserID: governorID, SkillID: fixture.SkillID, Action: skill.GovernanceActionSuspend,
		ReasonCode: "content_safety", ApprovalReference: "TICKET-123", SourceAddress: "192.0.2.10",
		IfMatch: ifMatch, RequestID: requestID, ReceiptID: receiptID, AuditID: auditID,
		KeyDigest: sha256.Sum256([]byte("governance-key")), SemanticDigest: sha256.Sum256([]byte("governance-semantic")),
		TransitionedAt: fixture.PublishedAt.Add(time.Hour),
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business\.user_account .*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(governorID))
	mock.ExpectQuery(`(?s)FROM business\.user_role_assignment.*role_key = 'skill_governor'.*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(newSkillRepositoryUUIDv7(t)))
	mock.ExpectQuery(`SELECT \* FROM "business"\."skill_command_receipt"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id = \$1 FOR UPDATE OF skill_record`).
		WithArgs(fixture.SkillID).
		WillReturnRows(governanceReadRows(fixture, skill.GovernanceStatusActive, 1))
	mock.ExpectExec(`UPDATE "business"\."skill" SET .*governance_epoch.*governance_status.*updated_at.*version`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_command_receipt"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."skill_governance_audit"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.TransitionGovernance(context.Background(), command)
	if err != nil || result.IdempotentReplay || result.SkillID != fixture.SkillID ||
		result.PublishedSnapshotID != fixture.SnapshotID || result.GovernanceStatus != skill.GovernanceStatusSuspended ||
		result.GovernanceEpoch != 2 || !result.TransitionedAt.Equal(command.TransitionedAt) {
		t.Fatalf("transition result=%+v err=%v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillGovernanceRepositoryReplaysFrozenReceiptAfterAuthorization(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newGovernanceRepositoryFixture(t)
	governorID := newSkillRepositoryUUIDv7(t)
	requestID := newSkillRepositoryUUIDv7(t)
	receiptID := newSkillRepositoryUUIDv7(t)
	epoch := int64(3)
	keyDigest := sha256.Sum256([]byte("governance-key"))
	semanticDigest := sha256.Sum256([]byte("governance-semantic"))
	command := skill.GovernanceTransitionRepositoryCommand{
		GovernorUserID: governorID, SkillID: fixture.SkillID, Action: skill.GovernanceActionResume,
		RequestID: newSkillRepositoryUUIDv7(t), KeyDigest: keyDigest, SemanticDigest: semanticDigest,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business\.user_account .*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(governorID))
	mock.ExpectQuery(`(?s)FROM business\.user_role_assignment.*role_key = 'skill_governor'.*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(newSkillRepositoryUUIDv7(t)))
	mock.ExpectQuery(`SELECT \* FROM "business"\."skill_command_receipt"`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "actor_user_id", "command_type", "scope_id", "key_digest", "semantic_digest", "result_skill_id",
			"result_content_revision_id", "result_review_submission_id", "result_published_snapshot_id",
			"response_draft_revision_id", "response_published_snapshot_id", "response_review_submission_id",
			"response_review_status", "response_review_reason_code", "response_review_updated_at",
			"response_governance_status", "response_governance_epoch", "request_id", "created_at",
		}).AddRow(
			receiptID, governorID, skill.CommandTypeGovernanceTransition, fixture.SkillID, keyDigest[:], semanticDigest[:], fixture.SkillID,
			nil, nil, fixture.SnapshotID, fixture.DraftID, fixture.SnapshotID, nil, nil, nil, nil,
			skill.GovernanceStatusActive, epoch, requestID, fixture.PublishedAt,
		))
	mock.ExpectCommit()

	result, err := repository.TransitionGovernance(context.Background(), command)
	if err != nil || !result.IdempotentReplay || result.GovernanceStatus != skill.GovernanceStatusActive ||
		result.GovernanceEpoch != epoch || result.PublishedSnapshotID != fixture.SnapshotID {
		t.Fatalf("replay result=%+v err=%v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillGovernanceRepositoryDeniesRevokedGovernorBeforeReceipt(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	governorID := newSkillRepositoryUUIDv7(t)
	command := skill.GovernanceTransitionRepositoryCommand{GovernorUserID: governorID, SkillID: newSkillRepositoryUUIDv7(t)}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business\.user_account .*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(governorID))
	mock.ExpectQuery(`(?s)FROM business\.user_role_assignment.*role_key = 'skill_governor'.*FOR UPDATE`).WithArgs(governorID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	if _, err := repository.TransitionGovernance(context.Background(), command); !errors.Is(err, skill.ErrGovernanceCapabilityRequired) {
		t.Fatalf("revoked governor error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
