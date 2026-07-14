package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
)

// contractClock 为真实 PostgreSQL Repository 契约测试冻结 UTC 时间。
type contractClock struct{ now time.Time }

// Now 返回契约测试冻结时间。
func (clock contractClock) Now() time.Time { return clock.now }

// contractProtector 模拟 Agent 本地加密边界，确保 Repository 只接触版本化自描述 AEAD Envelope。
type contractProtector struct{}

// Protect 返回模拟包含算法、Nonce 和认证标签的非空 Envelope 及稳定密钥版本，绝不写入日志。
func (contractProtector) Protect(_ context.Context, plaintext []byte) (session.ProtectedContent, error) {
	ciphertextAndTag := append([]byte("contract-ciphertext:"), plaintext...)
	ciphertextAndTag = append(ciphertextAndTag, make([]byte, 16)...)
	envelope, err := session.BuildEnvelopeV1(session.EnvelopeAlgorithmAES256GCM, make([]byte, 12), ciphertextAndTag)
	if err != nil {
		return session.ProtectedContent{}, err
	}
	return session.ProtectedContent{Ciphertext: envelope, KeyVersion: "contract-key-v1"}, nil
}

// TestSessionRepositoryConcurrentEnsureContract 使用真实 PostgreSQL 16 验证命令锁、原子写入和 Event Seq。
// 仅在显式提供独立测试数据库时运行；不得把 DORA_POSTGRES_CONTRACT_DSN 指向开发或生产数据库。
func TestSessionRepositoryConcurrentEnsureContract(t *testing.T) {
	dsn := os.Getenv("DORA_POSTGRES_CONTRACT_DSN")
	if dsn == "" {
		t.Skip("未设置 DORA_POSTGRES_CONTRACT_DSN，跳过 Session Repository 真实 PostgreSQL 契约测试")
	}
	client, err := Open(context.Background(), config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 16, MaxIdleConns: 4,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Agent 契约测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("关闭 Agent 契约测试数据库失败: %v", err)
		}
	})
	repository, err := NewSessionRepository(client)
	if err != nil {
		t.Fatalf("创建 Session Repository 失败: %v", err)
	}
	service, err := session.NewService(
		repository, idgen.UUIDv7{}, contractClock{now: time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)}, contractProtector{},
	)
	if err != nil {
		t.Fatalf("创建 Session Service 失败: %v", err)
	}

	commandID := mustContractUUIDv7(t)
	projectID := mustContractUUIDv7(t)
	ownerUserID := mustContractUUIDv7(t)
	requestDigest, promptDigest, _, err := session.CalculateRequestDigest(
		projectID, ownerUserID, "真实 PostgreSQL 并发", session.SkillSnapshotKindEmpty,
	)
	if err != nil {
		t.Fatalf("计算契约请求摘要失败: %v", err)
	}
	command := session.EnsureCommand{
		SchemaVersion: session.EnsureCommandSchemaVersionV1, RequestID: mustContractUUIDv7(t),
		CommandID: commandID, RequestDigest: requestDigest, ProjectID: projectID, OwnerUserID: ownerUserID,
		CreationSource: session.CreationSourceQuickCreate, InitialPrompt: "真实 PostgreSQL 并发",
		PromptDigest: promptDigest, SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt: time.Date(2026, 7, 14, 4, 59, 0, 0, time.UTC),
	}

	const concurrent = 20
	results := make(chan session.EnsureResult, concurrent)
	errorsChannel := make(chan error, concurrent)
	var waitGroup sync.WaitGroup
	for range concurrent {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, ensureErr := service.EnsureProjectSession(context.Background(), command)
			if ensureErr != nil {
				errorsChannel <- ensureErr
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	close(results)
	for ensureErr := range errorsChannel {
		t.Fatalf("真实 PostgreSQL 并发 Ensure 失败: %v", ensureErr)
	}
	var frozenSessionID string
	for result := range results {
		if frozenSessionID == "" {
			frozenSessionID = result.SessionID
		}
		if result.SessionID != frozenSessionID {
			t.Fatalf("真实 PostgreSQL 重放结果漂移: got=%s want=%s", result.SessionID, frozenSessionID)
		}
	}

	// Unknown Outcome 查询必须在不产生任何新事实的前提下返回 completed/conflict/not_found 三态。
	queryBase := session.QueryCommand{
		SchemaVersion: session.QueryCommandSchemaVersionV1, RequestID: mustContractUUIDv7(t),
		CommandID: commandID, ExpectedRequestDigest: requestDigest,
	}
	completed, err := service.QueryProjectSessionCommand(context.Background(), queryBase)
	if err != nil || completed.Status != session.QueryCommandStatusCompleted || completed.Receipt == nil ||
		completed.Receipt.SessionID != frozenSessionID {
		t.Fatalf("真实 PostgreSQL Query completed=%+v err=%v", completed, err)
	}
	conflictQuery := queryBase
	conflictQuery.ExpectedRequestDigest = strings.Repeat("a", 64)
	conflict, err := service.QueryProjectSessionCommand(context.Background(), conflictQuery)
	if err != nil || conflict.Status != session.QueryCommandStatusConflict || conflict.Receipt != nil {
		t.Fatalf("真实 PostgreSQL Query conflict=%+v err=%v", conflict, err)
	}
	notFoundQuery := queryBase
	notFoundQuery.CommandID = mustContractUUIDv7(t)
	notFound, err := service.QueryProjectSessionCommand(context.Background(), notFoundQuery)
	if err != nil || notFound.Status != session.QueryCommandStatusNotFound || notFound.Receipt != nil {
		t.Fatalf("真实 PostgreSQL Query not_found=%+v err=%v", notFound, err)
	}

	assertContractCount(t, client, "agent.session", "project_id", projectID, 1)
	assertContractCount(t, client, "agent.session_message", "source_id", commandID, 1)
	assertContractCount(t, client, "agent.session_input", "source_id", commandID, 1)
	assertContractCount(t, client, "agent.session_command_receipt", "command_id", commandID, 1)
	assertContractCount(t, client, "agent.session_event_log", "source_id", commandID, 2)

	// Workspace Repository 必须在真实 PostgreSQL REPEATABLE READ 中同时观察完整集合与同一 Event 高水位。
	workspaceRepository, err := NewWorkspaceRepository(client)
	if err != nil {
		t.Fatalf("创建 Workspace Repository 失败: %v", err)
	}
	snapshot, err := workspaceRepository.LoadSnapshot(context.Background(), workspace.Identity{
		UserID: ownerUserID, ProjectID: projectID, SessionID: frozenSessionID,
	}, workspace.SnapshotLimits{MaxMessages: 1, MaxInputs: 1})
	if err != nil {
		t.Fatalf("真实 PostgreSQL Workspace Snapshot 失败: %v", err)
	}
	if snapshot.Session.ID != frozenSessionID || snapshot.Session.ProjectID != projectID ||
		len(snapshot.Messages) != 1 || len(snapshot.Inputs) != 1 || snapshot.EventHighWatermark != 2 || snapshot.MinAvailableSeq != 1 {
		t.Fatalf("真实 PostgreSQL Snapshot 不一致: %+v", snapshot)
	}
	batch, err := workspaceRepository.LoadEventBatch(context.Background(), workspace.Identity{
		UserID: ownerUserID, ProjectID: projectID, SessionID: frozenSessionID,
	}, 1, 1)
	if err != nil || batch.LastSeq != 2 || batch.MinAvailableSeq != 1 || len(batch.Events) != 1 || batch.Events[0].Seq != 2 {
		t.Fatalf("真实 PostgreSQL EventBatch=%+v err=%v", batch, err)
	}
	if _, err := workspaceRepository.LoadSnapshot(context.Background(), workspace.Identity{
		UserID: mustContractUUIDv7(t), ProjectID: projectID, SessionID: frozenSessionID,
	}, workspace.SnapshotLimits{MaxMessages: 1, MaxInputs: 1}); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("真实 PostgreSQL 越权 Snapshot 错误=%v", err)
	}
	secondPlaintext := []byte("第二条边界消息")
	secondProtected, err := (contractProtector{}).Protect(context.Background(), secondPlaintext)
	if err != nil {
		t.Fatalf("构造第二条边界消息失败: %v", err)
	}
	secondDigest := sha256.Sum256(secondPlaintext)
	if err := client.db.WithContext(context.Background()).Create(&sessionMessageModel{
		ID: mustContractUUIDv7(t), SessionID: frozenSessionID, MessageSeq: 2, Role: string(session.MessageRoleUser),
		ContentCiphertext: secondProtected.Ciphertext, ContentKeyVersion: secondProtected.KeyVersion,
		ContentDigest: hex.EncodeToString(secondDigest[:]), SourceKind: event.SourceKindEnsureProjectSession,
		SourceID: mustContractUUIDv7(t), CreatedAt: time.Date(2026, 7, 14, 5, 2, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("写入 Snapshot limit+1 Fixture 失败: %v", err)
	}
	if _, err := workspaceRepository.LoadSnapshot(context.Background(), workspace.Identity{
		UserID: ownerUserID, ProjectID: projectID, SessionID: frozenSessionID,
	}, workspace.SnapshotLimits{MaxMessages: 1, MaxInputs: 1}); !errors.Is(err, workspace.ErrSnapshotTooLarge) {
		t.Fatalf("Snapshot limit+1 错误=%v", err)
	}

	var sequences []int64
	if err := client.db.WithContext(context.Background()).Raw(
		"SELECT seq FROM agent.session_event_log WHERE session_id = ? ORDER BY seq", frozenSessionID,
	).Scan(&sequences).Error; err != nil {
		t.Fatalf("查询 Event Seq 失败: %v", err)
	}
	if len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 {
		t.Fatalf("Event Seq 不连续: %v", sequences)
	}

	// 同一 CommandID 即使携带内部自洽的新摘要，也只能返回稳定冲突，不能覆盖首次冻结结果。
	conflictingCommand := command
	conflictingCommand.InitialPrompt = "不同业务语义"
	conflictingCommand.RequestDigest, conflictingCommand.PromptDigest, _, err = session.CalculateRequestDigest(
		projectID, ownerUserID, conflictingCommand.InitialPrompt, session.SkillSnapshotKindEmpty,
	)
	if err != nil {
		t.Fatalf("计算冲突命令摘要失败: %v", err)
	}
	if _, err := service.EnsureProjectSession(context.Background(), conflictingCommand); !errors.Is(err, session.ErrCommandConflict) {
		t.Fatalf("同 Command 异摘要错误=%v，want ErrCommandConflict", err)
	}

	// 不同 CommandID 对同一 Project 的竞争无法证明是技术重试，必须拒绝隐式复用既有 Session。
	differentCommand := command
	differentCommand.CommandID = mustContractUUIDv7(t)
	differentCommand.RequestID = mustContractUUIDv7(t)
	if _, err := service.EnsureProjectSession(context.Background(), differentCommand); !errors.Is(err, session.ErrProjectSessionConflict) {
		t.Fatalf("不同 Command 同 Project 错误=%v，want ErrProjectSessionConflict", err)
	}

	// 空 Prompt 仍建立 Session/Receipt/Event，但不能产生 Message 或 Input。
	emptyCommandID := mustContractUUIDv7(t)
	emptyProjectID := mustContractUUIDv7(t)
	emptyRequestDigest, emptyPromptDigest, _, err := session.CalculateRequestDigest(
		emptyProjectID, ownerUserID, "\u00a0\u3000", session.SkillSnapshotKindEmpty,
	)
	if err != nil {
		t.Fatalf("计算空 Prompt 命令摘要失败: %v", err)
	}
	emptyCommand := session.EnsureCommand{
		SchemaVersion: session.EnsureCommandSchemaVersionV1, RequestID: mustContractUUIDv7(t),
		CommandID: emptyCommandID, RequestDigest: emptyRequestDigest, ProjectID: emptyProjectID, OwnerUserID: ownerUserID,
		CreationSource: session.CreationSourceQuickCreate, InitialPrompt: "\u00a0\u3000",
		PromptDigest: emptyPromptDigest, SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt: time.Date(2026, 7, 14, 4, 59, 0, 0, time.UTC),
	}
	emptyResult, err := service.EnsureProjectSession(context.Background(), emptyCommand)
	if err != nil {
		t.Fatalf("真实 PostgreSQL 空 Prompt Ensure 失败: %v", err)
	}
	if emptyResult.MessageID != nil || emptyResult.InputID != nil {
		t.Fatalf("空 Prompt 返回了 Message/Input: %+v", emptyResult)
	}
	assertContractCount(t, client, "agent.session", "project_id", emptyProjectID, 1)
	assertContractCount(t, client, "agent.session_message", "source_id", emptyCommandID, 0)
	assertContractCount(t, client, "agent.session_input", "source_id", emptyCommandID, 0)
	assertContractCount(t, client, "agent.session_command_receipt", "command_id", emptyCommandID, 1)
	assertContractCount(t, client, "agent.session_event_log", "source_id", emptyCommandID, 1)

	// 最后一个 Event 故意违反 JSON Object 约束，使事务在已写入多个前置事实后失败，验证完整回滚。
	rollbackCommandID := mustContractUUIDv7(t)
	rollbackProjectID := mustContractUUIDv7(t)
	rollbackSessionID := mustContractUUIDv7(t)
	rollbackEvent, err := event.NewSessionCreated(
		mustContractUUIDv7(t), rollbackSessionID, rollbackProjectID, string(session.StatusActive), rollbackCommandID, 1,
		time.Date(2026, 7, 14, 5, 1, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("创建回滚测试 Event 失败: %v", err)
	}
	rollbackEvent.PayloadJSON = []byte("[]")
	rollbackAt := time.Date(2026, 7, 14, 5, 1, 0, 0, time.UTC)
	rollbackPlan := session.EnsurePlan{
		Session: session.Session{
			ID: rollbackSessionID, ProjectID: rollbackProjectID, UserID: ownerUserID,
			Status: session.StatusActive, Version: 1, CreatedAt: rollbackAt, UpdatedAt: rollbackAt,
		},
		SkillSnapshot: session.SkillSnapshot{
			SessionID: rollbackSessionID, Kind: session.SkillSnapshotKindEmpty,
			Digest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefsJSON: "[]", CreatedAt: rollbackAt,
		},
		SequenceCounter: session.SequenceCounter{SessionID: rollbackSessionID, UpdatedAt: rollbackAt},
		RuntimeLease:    session.RuntimeLease{SessionID: rollbackSessionID, Version: 1, UpdatedAt: rollbackAt},
		Receipt: session.CommandReceipt{
			CommandID: rollbackCommandID, CommandType: session.CommandTypeEnsureProjectSessionV1,
			RequestDigest: emptyRequestDigest, SessionID: rollbackSessionID,
			ResultVersion: session.ResultVersionV1, CompletedAt: rollbackAt,
		},
		Events: []event.Record{rollbackEvent},
	}
	if _, err := repository.Ensure(context.Background(), rollbackPlan); !errors.Is(err, session.ErrPersistence) {
		t.Fatalf("事务故障错误=%v，want ErrPersistence", err)
	}
	assertContractCount(t, client, "agent.session", "project_id", rollbackProjectID, 0)
	assertContractCount(t, client, "agent.session_skill_snapshot", "session_id", rollbackSessionID, 0)
	assertContractCount(t, client, "agent.session_event_counter", "session_id", rollbackSessionID, 0)
	assertContractCount(t, client, "agent.session_command_receipt", "command_id", rollbackCommandID, 0)

	// 绕过 Service/Repository 直接写入裸明文时，Migration CHECK 仍必须作为最后一道防线拒绝。
	rawSourceID := mustContractUUIDv7(t)
	rawInsertErr := client.db.WithContext(context.Background()).Exec(`
		INSERT INTO agent.session_message (
			id, session_id, message_seq, role, content_ciphertext, content_key_version,
			content_digest, source_kind, source_id, created_at
		) VALUES (?, ?, 1, 'user', ?, 'key-v1', ?, 'ensure_project_session', ?, ?)`,
		mustContractUUIDv7(t), mustContractUUIDv7(t), []byte("plaintext"), strings.Repeat("a", 64), rawSourceID, rollbackAt,
	).Error
	if rawInsertErr == nil {
		t.Fatalf("Migration CHECK 接受了裸明文 + KeyVersion")
	}
	assertContractCount(t, client, "agent.session_message", "source_id", rawSourceID, 0)
}

// mustContractUUIDv7 生成契约测试 UUIDv7，失败时立即终止测试。
func mustContractUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := (idgen.UUIDv7{}).New()
	if err != nil {
		t.Fatalf("生成契约测试 UUIDv7 失败: %v", err)
	}
	return value
}

// assertContractCount 按白名单测试表和列断言唯一业务事实数量。
func assertContractCount(t *testing.T, client *Client, table, column, value string, want int64) {
	t.Helper()
	allowed := map[string]map[string]bool{
		"agent.session":                 {"project_id": true},
		"agent.session_skill_snapshot":  {"session_id": true},
		"agent.session_message":         {"source_id": true},
		"agent.session_input":           {"source_id": true},
		"agent.session_command_receipt": {"command_id": true},
		"agent.session_event_counter":   {"session_id": true},
		"agent.session_event_log":       {"source_id": true},
	}
	if !allowed[table][column] {
		t.Fatalf("契约测试使用了未白名单查询: %s.%s", table, column)
	}
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", table, column)
	if err := client.db.WithContext(context.Background()).Raw(query, value).Scan(&count).Error; err != nil {
		t.Fatalf("查询 %s 业务事实数量失败: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s 数量=%d，want %d", table, count, want)
	}
}
