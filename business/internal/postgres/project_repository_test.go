package postgres

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newProjectRepositoryTestDB 创建由 sqlmock 驱动的 GORM Client，用于验证事务和 SQL 次数而不依赖真实网络。
func newProjectRepositoryTestDB(t *testing.T) (*ProjectRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm with sqlmock: %v", err)
	}
	repository, err := NewProjectRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("create project repository: %v", err)
	}
	return repository, mock
}

// newRepositoryTestUUIDv7 生成 Repository 测试使用的 UUIDv7。
func newRepositoryTestUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate repository test UUIDv7: %v", err)
	}
	return id.String()
}

// newRepositoryTestAggregate 创建带受保护首提示词的聚合，同时覆盖 Outbox 密文持久化与幂等分支。
func newRepositoryTestAggregate(t *testing.T) project.QuickCreateAggregate {
	t.Helper()
	aggregate, err := project.NewQuickCreateAggregate(project.QuickCreateSeed{
		ProjectID: newRepositoryTestUUIDv7(t), ReceiptID: newRepositoryTestUUIDv7(t),
		BindingID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t), OwnerUserID: newRepositoryTestUUIDv7(t),
		InitialPrompt: "initial prompt", KeyDigest: project.SHA256Digest([]byte("key")),
		EncryptedPayload: &project.EncryptedPayload{
			Algorithm: project.PromptEncryptionAlgorithm, KeyVersion: "test-key-v1", Nonce: []byte("123456789012"),
			Ciphertext: []byte("initial-ciphertext-with-auth-tag"), PayloadDigest: project.SHA256Digest([]byte("initial prompt")),
		},
		MaxAttempts: 5,
		OccurredAt:  time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create repository test aggregate: %v", err)
	}
	return aggregate
}

// newRepositoryTestConflictingAggregate 复用原用户和幂等键但改用另一首提示词，生成领域校验通过的同键异义命令。
func newRepositoryTestConflictingAggregate(t *testing.T, original project.QuickCreateAggregate) project.QuickCreateAggregate {
	t.Helper()
	aggregate, err := project.NewQuickCreateAggregate(project.QuickCreateSeed{
		ProjectID: newRepositoryTestUUIDv7(t), ReceiptID: newRepositoryTestUUIDv7(t),
		BindingID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t), OwnerUserID: original.Project.OwnerUserID,
		InitialPrompt: "different prompt", KeyDigest: original.Receipt.KeyDigest,
		EncryptedPayload: &project.EncryptedPayload{
			Algorithm: project.PromptEncryptionAlgorithm, KeyVersion: "test-key-v1", Nonce: []byte("123456789012"),
			Ciphertext: []byte("different-ciphertext-with-auth-tag"), PayloadDigest: project.SHA256Digest([]byte("different prompt")),
		},
		MaxAttempts: 5, OccurredAt: original.Project.CreatedAt,
	})
	if err != nil {
		t.Fatalf("create repository conflicting aggregate: %v", err)
	}
	return aggregate
}

// expectSuccessfulInsert 期望 GORM 对应用已生成 UUIDv7 主键的模型执行单次 INSERT。
func expectSuccessfulInsert(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."` + table + `"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// receiptRows 将领域回执转换为幂等重放查询的数据库行。
func receiptRows(receipt project.CreationReceipt, semanticDigest project.Digest) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "owner_user_id", "command_type", "key_digest", "semantic_digest", "project_id",
		"lifecycle_status", "recent_run_status", "session_provisioning_status", "initial_prompt_status", "created_at",
	}).AddRow(
		receipt.ID, receipt.OwnerUserID, receipt.CommandType, receipt.KeyDigest[:], semanticDigest[:], receipt.ProjectID,
		string(receipt.LifecycleStatus), string(receipt.RecentRunStatus), string(receipt.SessionProvisioningStatus),
		string(receipt.InitialPromptStatus), receipt.CreatedAt,
	)
}

func TestProjectSessionOutboxModelFromClearedEntityKeepsOnlyDigest(t *testing.T) {
	aggregate := newRepositoryTestAggregate(t)
	deliveredAt := aggregate.Outbox.CreatedAt.Add(time.Minute)
	clearedAt := deliveredAt.Add(time.Second)
	payloadDigest := aggregate.Outbox.EncryptedPayload.PayloadDigest
	outbox := aggregate.Outbox
	outbox.Status = project.OutboxStatusDelivered
	outbox.DeliveredAt = &deliveredAt
	outbox.PayloadClearedAt = &clearedAt
	outbox.UpdatedAt = clearedAt
	outbox.EncryptedPayload = &project.EncryptedPayload{PayloadDigest: payloadDigest}
	if err := outbox.Validate(); err != nil {
		t.Fatalf("validate cleared outbox entity: %v", err)
	}

	model := projectSessionOutboxModelFromEntity(outbox)
	if model.PayloadEncryptionAlgorithm != nil || model.PayloadKeyVersion != nil || len(model.PayloadNonce) != 0 || len(model.PayloadCiphertext) != 0 {
		t.Fatalf("cleared outbox model retained decryption material: %+v", model)
	}
	if !bytes.Equal(model.PayloadDigest, payloadDigest[:]) || model.PayloadClearedAt == nil || !model.PayloadClearedAt.Equal(clearedAt) {
		t.Fatalf("cleared outbox model lost digest or cleared time: %+v", model)
	}
}

func TestProjectRepositoryCreateQuickCommitsFourFacts(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	aggregate := newRepositoryTestAggregate(t)

	mock.ExpectBegin()
	expectSuccessfulInsert(mock, "project_creation_receipt")
	expectSuccessfulInsert(mock, "project")
	expectSuccessfulInsert(mock, "project_session_binding")
	expectSuccessfulInsert(mock, "project_session_outbox")
	mock.ExpectCommit()

	result, err := repository.CreateQuick(context.Background(), aggregate)
	if err != nil {
		t.Fatalf("create quick project: %v", err)
	}
	if result.IdempotentReplay || result.ProjectID != aggregate.Project.ID {
		t.Fatalf("unexpected first create result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepositoryCreateQuickReplaysSameDigest(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	aggregate := newRepositoryTestAggregate(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."project_creation_receipt"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT .* FROM "business"."project_creation_receipt" WHERE .*owner_user_id.*command_type.*key_digest.*LIMIT`).
		WillReturnRows(receiptRows(aggregate.Receipt, aggregate.Receipt.SemanticDigest))
	mock.ExpectCommit()

	result, err := repository.CreateQuick(context.Background(), aggregate)
	if err != nil {
		t.Fatalf("replay quick project: %v", err)
	}
	if !result.IdempotentReplay || result.ProjectID != aggregate.Project.ID {
		t.Fatalf("unexpected replay result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepositoryCreateQuickRejectsDifferentDigest(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	aggregate := newRepositoryTestAggregate(t)
	conflicting := newRepositoryTestConflictingAggregate(t, aggregate)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."project_creation_receipt"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT .* FROM "business"."project_creation_receipt" WHERE .*owner_user_id.*command_type.*key_digest.*LIMIT`).
		WillReturnRows(receiptRows(aggregate.Receipt, aggregate.Receipt.SemanticDigest))
	mock.ExpectRollback()

	_, err := repository.CreateQuick(context.Background(), conflicting)
	if !errors.Is(err, project.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepositoryRedactsUnknownDatabaseErrors(t *testing.T) {
	t.Run("create quick", func(t *testing.T) {
		repository, mock := newProjectRepositoryTestDB(t)
		aggregate := newRepositoryTestAggregate(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "business"."project_creation_receipt"`)).
			WillReturnError(errors.New("postgres password=super-secret SQL=INSERT payload"))
		mock.ExpectRollback()

		_, err := repository.CreateQuick(context.Background(), aggregate)
		if !errors.Is(err, project.ErrPersistence) {
			t.Fatalf("expected stable persistence error, got %v", err)
		}
		if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "INSERT") || strings.Contains(err.Error(), "postgres") {
			t.Fatalf("persistence error leaked database details: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet create error SQL expectations: %v", err)
		}
	})

	t.Run("find owned project", func(t *testing.T) {
		repository, mock := newProjectRepositoryTestDB(t)
		aggregate := newRepositoryTestAggregate(t)
		mock.ExpectQuery(`SELECT .* FROM "business"\."project" WHERE .*id.*owner_user_id.*LIMIT`).
			WillReturnError(errors.New("postgres dsn=super-secret SQL=SELECT project"))

		_, err := repository.FindOwnedByID(context.Background(), aggregate.Project.ID, aggregate.Project.OwnerUserID)
		if !errors.Is(err, project.ErrPersistence) {
			t.Fatalf("expected stable persistence error, got %v", err)
		}
		if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "SELECT") || strings.Contains(err.Error(), "postgres") {
			t.Fatalf("find error leaked database details: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet find error SQL expectations: %v", err)
		}
	})

	t.Run("bootstrap project", func(t *testing.T) {
		repository, mock := newProjectRepositoryTestDB(t)
		aggregate := newRepositoryTestAggregate(t)
		mock.ExpectQuery(`SELECT .*project_id.* FROM business\.project AS project JOIN business\.project_session_binding AS binding .*LIMIT`).
			WillReturnError(errors.New("postgres dsn=super-secret SQL=SELECT bootstrap"))

		_, err := repository.FindBootstrapOwnedByID(context.Background(), aggregate.Project.ID, aggregate.Project.OwnerUserID)
		if !errors.Is(err, project.ErrPersistence) {
			t.Fatalf("expected stable persistence error, got %v", err)
		}
		if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "SELECT") || strings.Contains(err.Error(), "postgres") {
			t.Fatalf("bootstrap error leaked database details: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet bootstrap error SQL expectations: %v", err)
		}
	})
}

func TestProjectRepositoryFindBootstrapUsesOneJoinAndMapsReadySession(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	aggregate := newRepositoryTestAggregate(t)
	sessionID := newRepositoryTestUUIDv7(t)
	inputID := newRepositoryTestUUIDv7(t)
	updatedAt := aggregate.Project.UpdatedAt.Add(time.Minute)
	rows := sqlmock.NewRows([]string{
		"project_id", "title", "lifecycle_status", "recent_run_status", "initial_prompt_status", "provisioning_status",
		"agent_session_id", "agent_input_id", "last_error_code", "project_updated_at", "binding_updated_at",
	}).AddRow(
		aggregate.Project.ID, aggregate.Project.Title, string(aggregate.Project.LifecycleStatus), string(aggregate.Project.RecentRunStatus),
		string(project.InitialPromptStatusAccepted), string(project.ProvisioningStatusReady), sessionID, inputID, nil,
		aggregate.Project.UpdatedAt, updatedAt,
	)
	mock.ExpectQuery(`SELECT .*project_id.* FROM business\.project AS project JOIN business\.project_session_binding AS binding .*LIMIT`).
		WillReturnRows(rows)

	result, err := repository.FindBootstrapOwnedByID(context.Background(), aggregate.Project.ID, aggregate.Project.OwnerUserID)
	if err != nil {
		t.Fatalf("find project bootstrap: %v", err)
	}
	if result.ProjectID != aggregate.Project.ID || result.AgentSessionID == nil || *result.AgentSessionID != sessionID ||
		result.AgentInputID == nil || *result.AgentInputID != inputID || result.CreationStatus() != "ready" || !result.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected bootstrap result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet bootstrap SQL expectations: %v", err)
	}
}

func TestProjectRepositoryFindBootstrapHidesMissingAndUnauthorized(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	aggregate := newRepositoryTestAggregate(t)
	mock.ExpectQuery(`SELECT .*project_id.* FROM business\.project AS project JOIN business\.project_session_binding AS binding .*LIMIT`).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repository.FindBootstrapOwnedByID(context.Background(), aggregate.Project.ID, aggregate.Project.OwnerUserID)
	if !errors.Is(err, project.ErrProjectNotFound) {
		t.Fatalf("expected hidden not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet bootstrap not-found SQL expectations: %v", err)
	}
}

func TestProjectRepositoryFindReadyAgentSessionAccessUsesOneOwnedReadyJoin(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	ownerUserID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	sessionID := newRepositoryTestUUIDv7(t)
	rows := sqlmock.NewRows([]string{"project_id", "agent_session_id"}).AddRow(projectID, sessionID)
	mock.ExpectQuery(`SELECT project\.id AS project_id, binding\.agent_session_id AS agent_session_id FROM business\.project AS project JOIN business\.project_session_binding AS binding .*owner_user_id.*lifecycle_status IN.*provisioning_status.*agent_session_id.*LIMIT`).
		WillReturnRows(rows)

	access, err := repository.FindReadyAgentSessionAccess(context.Background(), ownerUserID, sessionID)
	if err != nil {
		t.Fatalf("FindReadyAgentSessionAccess() error = %v", err)
	}
	if access.ProjectID != projectID || access.AgentSessionID != sessionID {
		t.Fatalf("unexpected access result: %+v", access)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet access SQL expectations: %v", err)
	}
}

func TestProjectRepositoryFindReadyAgentSessionAccessHidesMissingOwnerAndState(t *testing.T) {
	repository, mock := newProjectRepositoryTestDB(t)
	mock.ExpectQuery(`SELECT project\.id AS project_id, binding\.agent_session_id AS agent_session_id FROM business\.project AS project JOIN business\.project_session_binding AS binding .*LIMIT`).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repository.FindReadyAgentSessionAccess(context.Background(), newRepositoryTestUUIDv7(t), newRepositoryTestUUIDv7(t))
	if !errors.Is(err, project.ErrAgentSessionNotFound) {
		t.Fatalf("expected hidden session not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet access not-found SQL expectations: %v", err)
	}
}

func TestProjectRepositoryPreservesContextErrors(t *testing.T) {
	t.Run("create quick cancellation", func(t *testing.T) {
		repository, mock := newProjectRepositoryTestDB(t)
		aggregate := newRepositoryTestAggregate(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := repository.CreateQuick(ctx, aggregate)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unexpected SQL after context cancellation: %v", err)
		}
	})

	t.Run("find owned project deadline", func(t *testing.T) {
		repository, mock := newProjectRepositoryTestDB(t)
		aggregate := newRepositoryTestAggregate(t)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		_, err := repository.FindOwnedByID(ctx, aggregate.Project.ID, aggregate.Project.OwnerUserID)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unexpected SQL after context deadline: %v", err)
		}
	})
}
