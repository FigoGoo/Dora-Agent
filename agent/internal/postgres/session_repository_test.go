package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMapSessionRepositoryErrorHidesDatabaseDetail 验证未知数据库错误不会通过领域错误链泄漏 SQL 或参数。
func TestMapSessionRepositoryErrorHidesDatabaseDetail(t *testing.T) {
	mapped := mapSessionRepositoryError(errors.New("SELECT secret_prompt FROM hidden_table"))
	if !errors.Is(mapped, session.ErrPersistence) {
		t.Fatalf("未知数据库错误未映射为 ErrPersistence: %v", mapped)
	}
	if mapped.Error() != session.ErrPersistence.Error() || strings.Contains(mapped.Error(), "secret_prompt") {
		t.Fatalf("稳定错误泄漏数据库详情: %v", mapped)
	}
}

// TestMapSessionRepositoryErrorPreservesContext 验证取消和 Deadline 控制信号不会被压成普通持久化错误。
func TestMapSessionRepositoryErrorPreservesContext(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{name: "请求取消", err: fmt.Errorf("gorm transaction stopped: %w", context.Canceled), want: context.Canceled},
		{name: "Deadline 超时", err: fmt.Errorf("gorm transaction stopped: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mapped := mapSessionRepositoryError(testCase.err)
			if !errors.Is(mapped, testCase.want) || errors.Is(mapped, session.ErrPersistence) {
				t.Fatalf("Context 错误映射=%v，want %v 且非 ErrPersistence", mapped, testCase.want)
			}
		})
	}
}

// TestSessionRepositoryQueryThreeStates 验证 Query 每次只执行一条 Receipt 查询并严格返回三态。
func TestSessionRepositoryQueryThreeStates(t *testing.T) {
	const (
		commandID = "019f0000-0000-7000-8000-000000000001"
		digest    = "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4"
	)
	testCases := []struct {
		name        string
		rows        *sqlmock.Rows
		wantStatus  session.QueryCommandStatus
		wantReceipt bool
	}{
		{name: "not found", rows: sqlmock.NewRows(sessionReceiptColumns()), wantStatus: session.QueryCommandStatusNotFound},
		{name: "completed", rows: receiptRows(commandID, digest), wantStatus: session.QueryCommandStatusCompleted, wantReceipt: true},
		{name: "conflict", rows: receiptRows(commandID, strings.Repeat("a", 64)), wantStatus: session.QueryCommandStatusConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository, mock := newSessionRepositoryMock(t)
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_command_receipt" WHERE command_id = $1 LIMIT $2`)).
				WithArgs(commandID, 1).WillReturnRows(testCase.rows)
			result, err := repository.Query(context.Background(), session.QueryCommand{
				CommandID: commandID, ExpectedRequestDigest: digest,
				ExpectedCommandType: session.CommandTypeEnsureProjectSessionV1,
			})
			if err != nil || result.Status != testCase.wantStatus || (result.Receipt != nil) != testCase.wantReceipt {
				t.Fatalf("Query 结果=%+v err=%v", result, err)
			}
		})
	}
}

// TestSessionRepositoryQueryRejectsCrossVersionReceipt 验证同一 CommandID 命中另一协议版本时先返回版本冲突，不能按相同摘要重放。
func TestSessionRepositoryQueryRejectsCrossVersionReceipt(t *testing.T) {
	const (
		commandID = "019f0000-0000-7000-8000-000000000001"
		digest    = "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4"
	)
	repository, mock := newSessionRepositoryMock(t)
	rows := sqlmock.NewRows(sessionReceiptColumns()).AddRow(
		commandID, session.CommandTypeEnsureProjectSessionV2, digest,
		"019f0000-0000-7000-8000-000000000002", nil, nil, session.ResultVersionV2,
		session.EmptySkillSnapshotDigest, 0, time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_command_receipt" WHERE command_id = $1 LIMIT $2`)).
		WithArgs(commandID, 1).WillReturnRows(rows)
	_, err := repository.Query(context.Background(), session.QueryCommand{
		CommandID: commandID, ExpectedRequestDigest: digest,
		ExpectedCommandType: session.CommandTypeEnsureProjectSessionV1,
	})
	if !errors.Is(err, session.ErrCommandVersionConflict) {
		t.Fatalf("跨版本 Query 错误=%v，want ErrCommandVersionConflict", err)
	}
}

// TestSessionRepositoryQueryRejectsCorruptedReceipt 验证同摘要命中但冻结 Snapshot 结果不自洽时失败关闭，不能把损坏行重放为 completed。
func TestSessionRepositoryQueryRejectsCorruptedReceipt(t *testing.T) {
	const (
		commandID = "019f0000-0000-7000-8000-000000000001"
		digest    = "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4"
	)
	repository, mock := newSessionRepositoryMock(t)
	rows := sqlmock.NewRows(sessionReceiptColumns()).AddRow(
		commandID, session.CommandTypeEnsureProjectSessionV2, digest,
		"019f0000-0000-7000-8000-000000000002", nil, nil, session.ResultVersionV2,
		session.EmptySkillSnapshotDigest, 1,
		time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_command_receipt" WHERE command_id = $1 LIMIT $2`)).
		WithArgs(commandID, 1).WillReturnRows(rows)
	_, err := repository.Query(context.Background(), session.QueryCommand{
		CommandID: commandID, ExpectedRequestDigest: digest,
		ExpectedCommandType: session.CommandTypeEnsureProjectSessionV2,
	})
	if !errors.Is(err, session.ErrSnapshotIntegrity) {
		t.Fatalf("损坏 Receipt 错误=%v，want ErrSnapshotIntegrity", err)
	}
}

// TestSessionRepositoryLoadSkillSnapshotUsesAtMostTwoQueries 验证空快照只读 Header，非空快照固定增加一次有界 Item 查询且保持 load_order。
func TestSessionRepositoryLoadSkillSnapshotUsesAtMostTwoQueries(t *testing.T) {
	const sessionID = "019f0000-0000-7000-8000-000000000001"
	t.Run("empty uses one query", func(t *testing.T) {
		repository, mock := newSessionRepositoryMock(t)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_skill_snapshot" WHERE session_id = $1 LIMIT $2`)).
			WithArgs(sessionID, 1).
			WillReturnRows(skillSnapshotHeaderRows(sessionID, string(session.SkillSnapshotKindEmpty), 0, session.EmptySkillSnapshotDigest))
		stored, err := repository.LoadSkillSnapshot(context.Background(), sessionID, 16)
		if err != nil || stored.Header.SkillCount != 0 || len(stored.Items) != 0 {
			t.Fatalf("空 Snapshot=%+v err=%v", stored, err)
		}
	})

	t.Run("published refs uses second bounded query", func(t *testing.T) {
		repository, mock := newSessionRepositoryMock(t)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_skill_snapshot" WHERE session_id = $1 LIMIT $2`)).
			WithArgs(sessionID, 1).
			WillReturnRows(skillSnapshotHeaderRows(sessionID, string(session.SkillSnapshotKindPublishedRefs), 1, strings.Repeat("b", 64)))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_skill_snapshot_item" WHERE session_id = $1 ORDER BY load_order ASC LIMIT $2`)).
			WithArgs(sessionID, 17).
			WillReturnRows(skillSnapshotItemRows(sessionID, 1))
		stored, err := repository.LoadSkillSnapshot(context.Background(), sessionID, 16)
		if err != nil || len(stored.Items) != 1 || stored.Items[0].LoadOrder != 1 {
			t.Fatalf("非空 Snapshot=%+v err=%v", stored, err)
		}
	})
}

// TestSessionRepositoryLoadSkillSnapshotRejectsLimitPlusOne 验证 Item 查询以 max+1 防御损坏数据，超限不返回被截断集合。
func TestSessionRepositoryLoadSkillSnapshotRejectsLimitPlusOne(t *testing.T) {
	const sessionID = "019f0000-0000-7000-8000-000000000001"
	repository, mock := newSessionRepositoryMock(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_skill_snapshot" WHERE session_id = $1 LIMIT $2`)).
		WithArgs(sessionID, 1).
		WillReturnRows(skillSnapshotHeaderRows(sessionID, string(session.SkillSnapshotKindPublishedRefs), 1, strings.Repeat("b", 64)))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_skill_snapshot_item" WHERE session_id = $1 ORDER BY load_order ASC LIMIT $2`)).
		WithArgs(sessionID, 2).
		WillReturnRows(skillSnapshotItemRows(sessionID, 1).AddRow(skillSnapshotItemValues(sessionID, 2)...))
	stored, err := repository.LoadSkillSnapshot(context.Background(), sessionID, 1)
	if !errors.Is(err, session.ErrSnapshotLimitExceeded) || len(stored.Items) != 0 {
		t.Fatalf("limit+1 Snapshot=%+v err=%v", stored, err)
	}
}

// newSessionRepositoryMock 创建禁止真实网络的 GORM PostgreSQL Mock，并在测试结束验证 SQL 次数。
func newSessionRepositoryMock(t *testing.T) (*SessionRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 Session Repository SQL Mock 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Session Repository SQL 不符合契约: %v", err)
		}
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("创建 Session Repository GORM Mock 失败: %v", err)
	}
	return &SessionRepository{db: db}, mock
}

// sessionReceiptColumns 返回 Receipt 模型按 GORM 扫描使用的固定列名。
func sessionReceiptColumns() []string {
	return []string{
		"command_id", "command_type", "request_digest", "session_id", "message_id", "input_id",
		"result_version", "skill_snapshot_digest", "skill_count", "completed_at",
	}
}

// receiptRows 构造一条冻结 Receipt 查询结果。
func receiptRows(commandID, digest string) *sqlmock.Rows {
	return sqlmock.NewRows(sessionReceiptColumns()).AddRow(
		commandID, session.CommandTypeEnsureProjectSessionV1, digest,
		"019f0000-0000-7000-8000-000000000002", nil, nil, 1,
		session.EmptySkillSnapshotDigest, 0,
		time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	)
}

// skillSnapshotHeaderRows 构造 Snapshot Header GORM 扫描结果。
func skillSnapshotHeaderRows(sessionID, kind string, count int, digest string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"session_id", "schema_version", "snapshot_kind", "skill_count", "snapshot_digest",
		"published_snapshot_refs", "created_at",
	}).AddRow(
		sessionID, session.SkillSnapshotSchemaVersionV1, kind, count, digest, "[]",
		time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	)
}

// skillSnapshotItemRows 构造一条 Snapshot Item GORM 扫描结果。
func skillSnapshotItemRows(sessionID string, loadOrder int) *sqlmock.Rows {
	return sqlmock.NewRows(skillSnapshotItemColumns()).AddRow(skillSnapshotItemValues(sessionID, loadOrder)...)
}

// skillSnapshotItemColumns 返回 Snapshot Item 模型固定列名。
func skillSnapshotItemColumns() []string {
	return []string{
		"session_id", "load_order", "priority", "namespace", "skill_id", "publisher_user_id",
		"published_snapshot_id", "publication_revision", "definition_schema_version", "content_digest",
		"runtime_content_schema_version", "runtime_content_digest", "runtime_content_ciphertext",
		"runtime_content_key_version", "allowed_graph_tool_keys", "public_tool_refs",
		"permission_snapshot_digest", "runtime_policy_ref", "governance_epoch", "published_at_unix_ms", "created_at",
	}
}

// skillSnapshotItemValues 返回与固定列顺序匹配的测试 Item 值。
func skillSnapshotItemValues(sessionID string, loadOrder int) []driver.Value {
	envelope, _ := session.BuildEnvelopeV1(
		session.EnvelopeAlgorithmAES256GCM, make([]byte, 12), make([]byte, 17),
	)
	return []driver.Value{
		sessionID, loadOrder, 100, "user",
		fmt.Sprintf("019f0000-0000-7000-8000-%012d", loadOrder+100),
		"019f0000-0000-7000-8000-000000000201",
		fmt.Sprintf("019f0000-0000-7000-8000-%012d", loadOrder+300),
		int64(1), "skill_definition.v1", strings.Repeat("c", 64),
		"skill_runtime_content.v1", strings.Repeat("d", 64), envelope, "skill-key-v1",
		`["write_prompts"]`, `[]`, strings.Repeat("e", 64), "skill-runtime-policy:v1",
		int64(0), int64(1784011500123), time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	}
}

// TestMapSessionEventLogModelsAllocatesContinuousSequence 验证事件批量 Mapper 在内存分配连续 Seq，不修改输入记录。
func TestMapSessionEventLogModelsAllocatesContinuousSequence(t *testing.T) {
	records := []event.Record{
		{EventID: "event-1", SessionID: "session-1", Type: event.TypeSessionCreated, PayloadJSON: []byte("{}")},
		{EventID: "event-2", SessionID: "session-1", Type: event.TypeSessionInputAccepted, PayloadJSON: []byte("{}")},
	}
	models := mapSessionEventLogModels(records)
	if len(models) != 2 || models[0].Seq != 1 || models[1].Seq != 2 {
		t.Fatalf("批量 Event Seq 不连续: %+v", models)
	}
	if records[0].Seq != 0 || records[1].Seq != 0 {
		t.Fatalf("Mapper 修改了领域 Event 输入: %+v", records)
	}
}

// TestValidateEnsurePlanRejectsBlankPromptSideEffects 验证空 Prompt 计划不能夹带 Message/Input 序号或额外事件。
func TestValidateEnsurePlanRejectsBlankPromptSideEffects(t *testing.T) {
	now := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	plan := session.EnsurePlan{
		Session: session.Session{ID: "session", ProjectID: "project", UserID: "user", Status: session.StatusActive},
		SkillSnapshot: session.SkillSnapshot{
			SessionID: "session", SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			Kind: session.SkillSnapshotKindEmpty, SkillCount: 0,
			Digest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefsJSON: "[]",
		},
		SequenceCounter: session.SequenceCounter{SessionID: "session", LastMessageSeq: 1},
		RuntimeLease:    session.RuntimeLease{SessionID: "session"},
		Receipt: session.CommandReceipt{
			CommandID: "command", CommandType: session.CommandTypeEnsureProjectSessionV1,
			SessionID: "session", ResultVersion: session.ResultVersionV1,
			SkillSnapshotDigest: session.EmptySkillSnapshotDigest,
		},
		Events: []event.Record{{Type: event.TypeSessionCreated, ProjectionIndex: 0, CreatedAt: now}},
	}
	if err := validateEnsurePlan(plan); !errors.Is(err, session.ErrInvalidCommand) {
		t.Fatalf("空 Prompt 副作用计划错误=%v，want ErrInvalidCommand", err)
	}
}

// TestValidateEnsurePlanRejectsEmptyEvents 验证空事件计划返回稳定参数错误而不是越界 Panic。
func TestValidateEnsurePlanRejectsEmptyEvents(t *testing.T) {
	plan := session.EnsurePlan{
		Session: session.Session{ID: "session", ProjectID: "project", UserID: "user", Status: session.StatusActive},
		SkillSnapshot: session.SkillSnapshot{
			SessionID: "session", SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			Kind: session.SkillSnapshotKindEmpty, SkillCount: 0,
			Digest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefsJSON: "[]",
		},
		SequenceCounter: session.SequenceCounter{SessionID: "session"},
		RuntimeLease:    session.RuntimeLease{SessionID: "session"},
		Receipt: session.CommandReceipt{
			CommandID: "command", CommandType: session.CommandTypeEnsureProjectSessionV1,
			SessionID: "session", ResultVersion: session.ResultVersionV1,
			SkillSnapshotDigest: session.EmptySkillSnapshotDigest,
		},
	}
	if err := validateEnsurePlan(plan); !errors.Is(err, session.ErrInvalidCommand) {
		t.Fatalf("空 Events 计划错误=%v，want ErrInvalidCommand", err)
	}
}

// TestValidateEnsurePlanRejectsRawCiphertext 验证绕过 Session Service 的 Repository 调用也不能写入裸明文。
func TestValidateEnsurePlanRejectsRawCiphertext(t *testing.T) {
	plan := validEnsurePlanForValidation(t)
	plan.Message.Content = session.ProtectedContent{Ciphertext: []byte("plaintext"), KeyVersion: "key-v1"}
	if err := validateEnsurePlan(plan); !errors.Is(err, session.ErrInvalidCommand) {
		t.Fatalf("Repository 裸明文计划错误=%v，want ErrInvalidCommand", err)
	}
}

// TestValidateEnsurePlanRejectsUnfrozenRoleAndSource 验证 W0 Repository 只接受 user/user_message，未来类型必须前向扩展。
func TestValidateEnsurePlanRejectsUnfrozenRoleAndSource(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*session.EnsurePlan)
	}{
		{name: "assistant role", mutate: func(plan *session.EnsurePlan) { plan.Message.Role = session.MessageRole("assistant") }},
		{name: "resume source", mutate: func(plan *session.EnsurePlan) { plan.Input.SourceType = session.InputSourceType("resume_requested") }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := validEnsurePlanForValidation(t)
			testCase.mutate(&plan)
			if err := validateEnsurePlan(plan); !errors.Is(err, session.ErrInvalidCommand) {
				t.Fatalf("未冻结角色/来源错误=%v，want ErrInvalidCommand", err)
			}
		})
	}
}

// validEnsurePlanForValidation 构造通过 Repository 双重校验的最小非空 Prompt 计划。
func validEnsurePlanForValidation(t *testing.T) session.EnsurePlan {
	t.Helper()
	now := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	nonce := make([]byte, 12)
	ciphertextAndTag := make([]byte, 17)
	envelope, err := session.BuildEnvelopeV1(session.EnvelopeAlgorithmAES256GCM, nonce, ciphertextAndTag)
	if err != nil {
		t.Fatalf("构建 Repository 测试 Envelope 失败: %v", err)
	}
	messageID := "message"
	inputID := "input"
	createdEvent, err := event.NewSessionCreated("event-created", "session", "project", "active", "command", 1, now)
	if err != nil {
		t.Fatalf("构建 session.created 测试事件失败: %v", err)
	}
	acceptedEvent, err := event.NewSessionInputAccepted("event-input", "session", inputID, messageID, "command", "pending", 1, now)
	if err != nil {
		t.Fatalf("构建 session.input.accepted 测试事件失败: %v", err)
	}
	return session.EnsurePlan{
		Session: session.Session{
			ID: "session", ProjectID: "project", UserID: "user",
			Status: session.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		SkillSnapshot: session.SkillSnapshot{
			SessionID: "session", SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			Kind: session.SkillSnapshotKindEmpty, SkillCount: 0,
			Digest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefsJSON: "[]", CreatedAt: now,
		},
		SequenceCounter: session.SequenceCounter{
			SessionID: "session", LastMessageSeq: 1, LastInputEnqueueSeq: 1, UpdatedAt: now,
		},
		RuntimeLease: session.RuntimeLease{SessionID: "session", Version: 1, UpdatedAt: now},
		Message: &session.Message{
			ID: messageID, SessionID: "session", Seq: 1, Role: session.MessageRoleUser,
			Content:    session.ProtectedContent{Ciphertext: envelope, KeyVersion: "key-v1"},
			SourceKind: event.SourceKindEnsureProjectSession, SourceID: "command", CreatedAt: now,
		},
		Input: &session.Input{
			ID: inputID, SessionID: "session", SourceType: session.InputSourceTypeUserMessage,
			SourceID: "command", MessageID: messageID, Status: session.InputStatusPending,
			EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		},
		Receipt: session.CommandReceipt{
			CommandID: "command", CommandType: session.CommandTypeEnsureProjectSessionV1,
			SessionID: "session", MessageID: &messageID, InputID: &inputID,
			ResultVersion: session.ResultVersionV1, SkillSnapshotDigest: session.EmptySkillSnapshotDigest,
			SkillCount: 0, CompletedAt: now,
		},
		Events: []event.Record{createdEvent, acceptedEvent},
	}
}
