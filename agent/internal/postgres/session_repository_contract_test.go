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
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"gorm.io/gorm"
)

// contractClock 为真实 PostgreSQL Repository 契约测试冻结 UTC 时间。
type contractClock struct{ now time.Time }

// Now 返回契约测试冻结时间。
func (clock contractClock) Now() time.Time { return clock.now }

// contractProtector 模拟 Agent 本地加密边界，确保 Repository 只接触版本化自描述 AEAD Envelope。
type contractProtector struct{}

const (
	contractV2RuntimeDigest  = "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168"
	contractV2SnapshotDigest = "69ef1ba7ca41c90986204308043cb4587097ce3d4edbcea921b00eafc7cdfcdc"
)

// TestSessionSkillSnapshotV2MigrationDeclaresImmutableTriggers 静态锁定 Header/Item 的 UPDATE/DELETE 拒绝触发器及 Down 清理，避免真实库测试被跳过时失去门禁。
func TestSessionSkillSnapshotV2MigrationDeclaresImmutableTriggers(t *testing.T) {
	t.Parallel()
	upSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.up.sql")
	if err != nil {
		t.Fatalf("读取 V2 Up Migration 失败: %v", err)
	}
	downSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.down.sql")
	if err != nil {
		t.Fatalf("读取 V2 Down Migration 失败: %v", err)
	}
	for _, required := range []string{
		"CREATE FUNCTION agent.reject_session_skill_snapshot_mutation()",
		"CREATE TRIGGER trg_session_skill_snapshot__immutable",
		"BEFORE UPDATE OR DELETE ON agent.session_skill_snapshot",
		"CREATE TRIGGER trg_session_skill_snapshot_item__immutable",
		"BEFORE UPDATE OR DELETE ON agent.session_skill_snapshot_item",
	} {
		if !strings.Contains(string(upSQL), required) {
			t.Fatalf("V2 Up Migration 缺少不可变门禁: %s", required)
		}
	}
	for _, required := range []string{
		"DROP TRIGGER IF EXISTS trg_session_skill_snapshot_item__immutable",
		"DROP TRIGGER IF EXISTS trg_session_skill_snapshot__immutable",
		"DROP FUNCTION IF EXISTS agent.reject_session_skill_snapshot_mutation()",
	} {
		if !strings.Contains(string(downSQL), required) {
			t.Fatalf("V2 Down Migration 缺少触发器清理: %s", required)
		}
	}
}

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

// contractV2SnapshotFixture 返回设计 golden vector 对应的非空 Snapshot DTO。
func contractV2SnapshotFixture() skill.SessionSkillSnapshotV1 {
	notApplicable := skill.CapabilityGuidanceV1{
		Applicability: skill.SkillGuidanceNotApplicableV1, NotApplicableReason: "not used",
	}
	runtime := skill.SkillRuntimeContentV1{
		SchemaVersion: skill.RuntimeContentSchemaVersionV1, Name: "Prompt helper",
		InputDescription: "text", OutputDescription: "prompt", InvocationRules: "Use for prompt writing.",
		PlanCreationSpec: notApplicable, AnalyzeMaterials: notApplicable, PlanStoryboard: notApplicable,
		GenerateMedia: notApplicable,
		WritePrompts: skill.CapabilityGuidanceV1{
			Applicability: skill.SkillGuidanceEnabledV1, Guidance: "Write concise prompts.",
		},
		AssembleOutput: notApplicable, Examples: make([]skill.SkillExampleV1, 0),
		StarterPrompts: []string{"Improve this prompt."},
	}
	return skill.SessionSkillSnapshotV1{
		SchemaVersion: skill.SnapshotSchemaVersionV1,
		SnapshotKind:  skill.SessionSkillSnapshotKindPublishedRefsV1,
		SkillCount:    1, SnapshotSetDigest: contractV2SnapshotDigest,
		Skills: []skill.PublishedSkillSnapshotRefV1{{
			LoadOrder: 1, Priority: 100, Namespace: skill.SkillNamespaceUserV1,
			SkillID:             "019f0000-0000-7000-8000-000000000101",
			PublisherUserID:     "019f0000-0000-7000-8000-000000000102",
			PublishedSnapshotID: "019f0000-0000-7000-8000-000000000103",
			PublicationRevision: 2, DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
			ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
			RuntimeContentSchemaVersion: skill.RuntimeContentSchemaVersionV1,
			RuntimeContentDigest:        contractV2RuntimeDigest, RuntimeContent: runtime,
			AllowedGraphToolKeys:     []string{"write_prompts"},
			PublicToolRefs:           make([]skill.PublicToolSnapshotRefV1, 0),
			PermissionSnapshotDigest: "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25",
			RuntimePolicyRef:         skill.RuntimePolicyRefV1, GovernanceEpoch: 3,
			PublishedAtUnixMS: 1784011500123,
		}},
	}
}

// newContractEnsureCommandV2 独立计算 V2 request/prompt digest，供真实 PostgreSQL 并发契约测试使用。
func newContractEnsureCommandV2(t *testing.T) session.EnsureCommandV2 {
	t.Helper()
	snapshot := contractV2SnapshotFixture()
	projectID := mustContractUUIDv7(t)
	ownerID := mustContractUUIDv7(t)
	canonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
		SchemaVersion: skill.EnsureProjectSessionSchemaVersionV2,
		ProjectID:     projectID, OwnerUserID: ownerID, CreationSource: skill.CreationSourceQuickCreate,
		InitialPrompt: "真实 PostgreSQL V2", SkillSnapshot: snapshot,
	}, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatalf("计算 V2 契约命令摘要失败: %v", err)
	}
	return session.EnsureCommandV2{
		SchemaVersion: skill.EnsureProjectSessionSchemaVersionV2,
		RequestID:     mustContractUUIDv7(t), CommandID: mustContractUUIDv7(t),
		RequestDigest: canonical.RequestDigest.Hex(), ProjectID: projectID, OwnerUserID: ownerID,
		CreationSource: skill.CreationSourceQuickCreate, InitialPrompt: "真实 PostgreSQL V2",
		PromptDigest: canonical.PromptDigest, SkillSnapshot: snapshot,
		RequestedAt: time.Date(2026, 7, 14, 6, 59, 0, 0, time.UTC),
	}
}

// newContractRollbackV2Plan 构造 Item batch 唯一键在第二行失败的完整计划，用于证明事务中间失败无部分事实。
func newContractRollbackV2Plan(
	t *testing.T,
	protector session.SkillSnapshotContentProtector,
) session.EnsurePlan {
	t.Helper()
	snapshot := contractV2SnapshotFixture()
	fixture := snapshot.Skills[0]
	_, canonicalRuntime, runtimeDigest, err := skill.CanonicalRuntimeContentV1(
		fixture.RuntimeContent, skill.DefaultLimitsProfileV1(),
	)
	if err != nil {
		t.Fatalf("构造 V2 rollback Runtime canonical 失败: %v", err)
	}
	sessionID := mustContractUUIDv7(t)
	identities := make([]session.SkillSnapshotPlaintext, 2)
	for index := range identities {
		identities[index] = session.SkillSnapshotPlaintext{
			Identity: session.SkillSnapshotContentIdentity{
				SessionID: sessionID, SkillID: fixture.SkillID,
				PublishedSnapshotID: fixture.PublishedSnapshotID, RuntimeContentDigest: runtimeDigest.Hex(),
			},
			CanonicalBytes: canonicalRuntime,
		}
	}
	protected, err := protector.ProtectBatch(context.Background(), identities)
	if err != nil {
		t.Fatalf("构造 V2 rollback 密文失败: %v", err)
	}
	now := time.Date(2026, 7, 14, 7, 1, 0, 0, time.UTC)
	commandID := mustContractUUIDv7(t)
	projectID := mustContractUUIDv7(t)
	items := make([]session.SkillSnapshotItem, 2)
	for index := range items {
		items[index] = session.SkillSnapshotItem{
			SessionID: sessionID, LoadOrder: index + 1, Priority: int(fixture.Priority),
			Namespace: string(fixture.Namespace), SkillID: fixture.SkillID,
			PublisherUserID: fixture.PublisherUserID, PublishedSnapshotID: fixture.PublishedSnapshotID,
			PublicationRevision:     fixture.PublicationRevision,
			DefinitionSchemaVersion: fixture.DefinitionSchemaVersion, ContentDigest: fixture.ContentDigest,
			RuntimeContentSchemaVersion: fixture.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        fixture.RuntimeContentDigest, RuntimeContent: protected[index].Protected,
			AllowedGraphToolKeysJSON: `["write_prompts"]`, PublicToolRefsJSON: `[]`,
			PermissionSnapshotDigest: fixture.PermissionSnapshotDigest,
			RuntimePolicyRef:         fixture.RuntimePolicyRef, GovernanceEpoch: fixture.GovernanceEpoch,
			PublishedAtUnixMS: fixture.PublishedAtUnixMS, CreatedAt: now,
		}
	}
	createdEvent, err := event.NewSessionCreated(
		mustContractUUIDv7(t), sessionID, projectID, string(session.StatusActive), commandID, 1, now,
	)
	if err != nil {
		t.Fatalf("构造 V2 rollback Event 失败: %v", err)
	}
	return session.EnsurePlan{
		Session: session.Session{
			ID: sessionID, ProjectID: projectID, UserID: mustContractUUIDv7(t),
			Status: session.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		SkillSnapshot: session.SkillSnapshot{
			SessionID: sessionID, SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			Kind: session.SkillSnapshotKindPublishedRefs, SkillCount: 2,
			Digest:                    strings.Repeat("a", 64),
			PublishedSnapshotRefsJSON: `[{"load_order":1},{"load_order":2}]`, CreatedAt: now,
		},
		SkillSnapshotItems: items,
		SequenceCounter:    session.SequenceCounter{SessionID: sessionID, UpdatedAt: now},
		RuntimeLease:       session.RuntimeLease{SessionID: sessionID, Version: 1, UpdatedAt: now},
		Receipt: session.CommandReceipt{
			CommandID: commandID, CommandType: session.CommandTypeEnsureProjectSessionV2,
			RequestDigest: strings.Repeat("b", 64), SessionID: sessionID,
			ResultVersion: session.ResultVersionV2, SkillSnapshotDigest: strings.Repeat("a", 64),
			SkillCount: 2, CompletedAt: now,
		},
		Events: []event.Record{createdEvent},
	}
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
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtector(
		[]byte("0123456789abcdef0123456789abcdef"), "contract-skill-key-v1",
	)
	if err != nil {
		t.Fatalf("创建真实 Skill Snapshot 保护器失败: %v", err)
	}
	service, err := session.NewServiceWithSkillSnapshot(
		repository, idgen.UUIDv7{}, contractClock{now: time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)},
		contractProtector{}, snapshotProtector, skill.DefaultLimitsProfileV1(),
	)
	if err != nil {
		t.Fatalf("创建 Session Service 失败: %v", err)
	}
	assertContractV2UpBackfillsW0Data(t, client)

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
	assertContractV2DownWithoutV2Data(t, client)

	// V2 非空 Snapshot 使用真实 AES-GCM 并发 100 次，只能冻结一个 Session/Header/Item/Receipt。
	v2Command := newContractEnsureCommandV2(t)
	const v2Concurrent = 100
	v2Results := make(chan session.EnsureResult, v2Concurrent)
	v2Errors := make(chan error, v2Concurrent)
	var v2WaitGroup sync.WaitGroup
	for range v2Concurrent {
		v2WaitGroup.Add(1)
		go func() {
			defer v2WaitGroup.Done()
			result, ensureErr := service.EnsureProjectSessionV2(context.Background(), v2Command)
			if ensureErr != nil {
				v2Errors <- ensureErr
				return
			}
			v2Results <- result
		}()
	}
	v2WaitGroup.Wait()
	close(v2Results)
	close(v2Errors)
	for ensureErr := range v2Errors {
		t.Fatalf("真实 PostgreSQL 并发 Ensure V2 失败: %v", ensureErr)
	}
	var v2SessionID string
	var v2Created int
	for result := range v2Results {
		if v2SessionID == "" {
			v2SessionID = result.SessionID
		}
		if result.SessionID != v2SessionID || result.SkillSnapshotDigest != contractV2SnapshotDigest || result.SkillCount != 1 {
			t.Fatalf("真实 PostgreSQL V2 Receipt 漂移: %+v", result)
		}
		if result.Disposition == session.EnsureDispositionCreated {
			v2Created++
		}
	}
	if v2Created != 1 {
		t.Fatalf("真实 PostgreSQL V2 created=%d，want 1", v2Created)
	}
	assertContractCount(t, client, "agent.session", "project_id", v2Command.ProjectID, 1)
	assertContractCount(t, client, "agent.session_skill_snapshot", "session_id", v2SessionID, 1)
	assertContractCount(t, client, "agent.session_skill_snapshot_item", "session_id", v2SessionID, 1)
	assertContractCount(t, client, "agent.session_command_receipt", "command_id", v2Command.CommandID, 1)
	loadedV2, err := service.LoadSessionSkillSnapshotV1(context.Background(), v2SessionID)
	if err != nil || len(loadedV2.Snapshot.Skills) != 1 ||
		loadedV2.Snapshot.Skills[0].RuntimeContent.Name != "Prompt helper" {
		t.Fatalf("真实 PostgreSQL V2 Snapshot 加载=%+v err=%v", loadedV2, err)
	}
	var plaintextMatches int64
	if err := client.db.WithContext(context.Background()).Raw(`
		SELECT COUNT(*)
		FROM agent.session_skill_snapshot_item
		WHERE session_id = ?
		  AND position(convert_to('Prompt helper', 'UTF8') IN runtime_content_ciphertext) > 0`, v2SessionID,
	).Scan(&plaintextMatches).Error; err != nil {
		t.Fatalf("核对 Snapshot 密文明文泄漏失败: %v", err)
	}
	if plaintextMatches != 0 {
		t.Fatalf("真实 PostgreSQL Snapshot 密文包含 Runtime fixture 明文")
	}
	assertContractV2SnapshotImmutability(t, client, v2SessionID)
	v1QueryForV2 := session.QueryCommand{
		SchemaVersion: session.QueryCommandSchemaVersionV1, RequestID: mustContractUUIDv7(t),
		CommandID: v2Command.CommandID, ExpectedRequestDigest: v2Command.RequestDigest,
	}
	if _, err := service.QueryProjectSessionCommand(context.Background(), v1QueryForV2); !errors.Is(err, session.ErrCommandVersionConflict) {
		t.Fatalf("真实 PostgreSQL V1 Query 命中 V2 Receipt 错误=%v", err)
	}
	assertContractV2DownRefusesDataLoss(t, client, v2SessionID, v2Command.CommandID)

	// Snapshot Item batch 中的唯一键冲突发生在 Session/Header 已写后，整个事务必须回滚且不能留下部分 Item。
	rollbackV2Plan := newContractRollbackV2Plan(t, snapshotProtector)
	if _, err := repository.Ensure(context.Background(), rollbackV2Plan); !errors.Is(err, session.ErrPersistence) {
		t.Fatalf("V2 batch 中间失败错误=%v，want ErrPersistence", err)
	}
	assertContractCount(t, client, "agent.session", "project_id", rollbackV2Plan.Session.ProjectID, 0)
	assertContractCount(t, client, "agent.session_skill_snapshot", "session_id", rollbackV2Plan.Session.ID, 0)
	assertContractCount(t, client, "agent.session_skill_snapshot_item", "session_id", rollbackV2Plan.Session.ID, 0)
	assertContractCount(t, client, "agent.session_command_receipt", "command_id", rollbackV2Plan.Receipt.CommandID, 0)

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
			SessionID: rollbackSessionID, SchemaVersion: session.SkillSnapshotSchemaVersionV1,
			Kind: session.SkillSnapshotKindEmpty, SkillCount: 0,
			Digest: session.EmptySkillSnapshotDigest, PublishedSnapshotRefsJSON: "[]", CreatedAt: rollbackAt,
		},
		SequenceCounter: session.SequenceCounter{SessionID: rollbackSessionID, UpdatedAt: rollbackAt},
		RuntimeLease:    session.RuntimeLease{SessionID: rollbackSessionID, Version: 1, UpdatedAt: rollbackAt},
		Receipt: session.CommandReceipt{
			CommandID: rollbackCommandID, CommandType: session.CommandTypeEnsureProjectSessionV1,
			RequestDigest: emptyRequestDigest, SessionID: rollbackSessionID,
			ResultVersion: session.ResultVersionV1, SkillSnapshotDigest: session.EmptySkillSnapshotDigest,
			SkillCount: 0, CompletedAt: rollbackAt,
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

// assertContractV2SnapshotImmutability 验证数据库独立拒绝 Header/Item 的无变化 UPDATE 与 DELETE，并保留原冻结事实。
func assertContractV2SnapshotImmutability(t *testing.T, client *Client, sessionID string) {
	t.Helper()
	mutations := []struct {
		name string
		sql  string
	}{
		{name: "update header", sql: "UPDATE agent.session_skill_snapshot SET snapshot_digest = snapshot_digest WHERE session_id = ?"},
		{name: "delete header", sql: "DELETE FROM agent.session_skill_snapshot WHERE session_id = ?"},
		{name: "update item", sql: "UPDATE agent.session_skill_snapshot_item SET priority = priority WHERE session_id = ?"},
		{name: "delete item", sql: "DELETE FROM agent.session_skill_snapshot_item WHERE session_id = ?"},
	}
	for _, mutation := range mutations {
		if err := client.db.WithContext(context.Background()).Exec(mutation.sql, sessionID).Error; err == nil {
			t.Fatalf("不可变触发器接受了 %s", mutation.name)
		}
	}
	assertContractCount(t, client, "agent.session_skill_snapshot", "session_id", sessionID, 1)
	assertContractCount(t, client, "agent.session_skill_snapshot_item", "session_id", sessionID, 1)
}

// assertContractV2UpBackfillsW0Data 在事务内还原到 W0、写入旧 Header/Receipt，再执行完整 Up 并核对原语义与新增字段。
func assertContractV2UpBackfillsW0Data(t *testing.T, client *Client) {
	t.Helper()
	downSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.down.sql")
	if err != nil {
		t.Fatalf("读取 V2 Down Migration 失败: %v", err)
	}
	upSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.up.sql")
	if err != nil {
		t.Fatalf("读取 V2 Up Migration 失败: %v", err)
	}
	tx := client.db.WithContext(context.Background()).Begin()
	if tx.Error != nil {
		t.Fatalf("开始 V2 Up 回填验证事务失败: %v", tx.Error)
	}
	// latest Schema 的 009 已将 Receipt 冻结；本用例验证的是历史 005 自身的 W0→V2
	// 回填语义，因此只在这个最终会回滚的事务内暂停 009 Guard。
	if err := tx.Exec(`ALTER TABLE agent.session_command_receipt
		DISABLE TRIGGER trg_session_command_receipt__immutable`).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("暂停 latest Receipt Guard 失败: %v", err)
	}
	if err := tx.Exec(string(downSQL)).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("准备 W0 Schema 失败: %v", err)
	}
	sessionID := mustContractUUIDv7(t)
	commandID := mustContractUUIDv7(t)
	now := time.Date(2026, 7, 14, 4, 30, 0, 0, time.UTC)
	if err := tx.Exec(`
		INSERT INTO agent.session_skill_snapshot (
			session_id, snapshot_kind, snapshot_digest, published_snapshot_refs, created_at
		) VALUES (?, 'empty', ?, '[]'::jsonb, ?)`,
		sessionID, session.EmptySkillSnapshotDigest, now,
	).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("写入 W0 Header 回填 Fixture 失败: %v", err)
	}
	if err := tx.Exec(`
		INSERT INTO agent.session_command_receipt (
			command_id, command_type, request_digest, session_id, result_version, completed_at
		) VALUES (?, 'ensure_project_session_v1', ?, ?, 1, ?)`,
		commandID, strings.Repeat("a", 64), sessionID, now,
	).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("写入 W0 Receipt 回填 Fixture 失败: %v", err)
	}
	if err := tx.Exec(string(upSQL)).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("W0 数据执行 V2 Up 失败: %v", err)
	}
	var header sessionSkillSnapshotModel
	if err := tx.Where("session_id = ?", sessionID).Take(&header).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("读取 V2 Header 回填失败: %v", err)
	}
	var receipt sessionCommandReceiptModel
	if err := tx.Where("command_id = ?", commandID).Take(&receipt).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("读取 V2 Receipt 回填失败: %v", err)
	}
	if header.SchemaVersion != session.SkillSnapshotSchemaVersionV1 || header.SkillCount != 0 ||
		header.SnapshotDigest != session.EmptySkillSnapshotDigest ||
		receipt.CommandType != session.CommandTypeEnsureProjectSessionV1 || receipt.SkillCount != 0 ||
		receipt.SkillSnapshotDigest != session.EmptySkillSnapshotDigest {
		_ = tx.Rollback().Error
		t.Fatalf("W0 回填语义漂移: header=%+v receipt=%+v", header, receipt)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("回滚 V2 Up 回填验证事务失败: %v", err)
	}
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("回滚 V2 Up 回填验证后 Schema 未恢复: %v", err)
	}
}

// assertContractV2DownWithoutV2Data 在事务内执行完整 Down，证明只有 V1 empty 数据时能够恢复 W0 Schema；随后回滚以继续契约测试。
func assertContractV2DownWithoutV2Data(t *testing.T, client *Client) {
	t.Helper()
	downSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.down.sql")
	if err != nil {
		t.Fatalf("读取 V2 Down Migration 失败: %v", err)
	}
	tx := client.db.WithContext(context.Background()).Begin()
	if tx.Error != nil {
		t.Fatalf("开始 V2 Down 验证事务失败: %v", tx.Error)
	}
	if err := tx.Exec(string(downSQL)).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("无 V2 数据时 Down Migration 失败: %v", err)
	}
	var itemTableCount int64
	if err := tx.Raw(`
		SELECT COUNT(*)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'agent' AND relation.relname = 'session_skill_snapshot_item'`,
	).Scan(&itemTableCount).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("检查 Down 后 Item 表失败: %v", err)
	}
	if itemTableCount != 0 {
		_ = tx.Rollback().Error
		t.Fatalf("无 V2 数据 Down 后 Item 表仍存在")
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("回滚 V2 Down 验证事务失败: %v", err)
	}
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("回滚 Down 验证后 W1 Schema 未恢复: %v", err)
	}
}

// assertContractV2DownRefusesDataLoss 验证存在 Header/Item/V2 Receipt 时 Down 首段即抛错，回滚后全部数据仍可读取。
func assertContractV2DownRefusesDataLoss(t *testing.T, client *Client, sessionID, commandID string) {
	t.Helper()
	downSQL, err := os.ReadFile("../../migrations/20260714000500_expand_session_skill_snapshot_v2.down.sql")
	if err != nil {
		t.Fatalf("读取 V2 Down Migration 失败: %v", err)
	}
	testCases := []struct {
		name    string
		prepare func(*gorm.DB) error
	}{
		{name: "all facts", prepare: func(*gorm.DB) error { return nil }},
		{name: "item only", prepare: func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM agent.session_skill_snapshot WHERE session_id = ?", sessionID).Error; err != nil {
				return err
			}
			return tx.Exec("DELETE FROM agent.session_command_receipt WHERE command_id = ?", commandID).Error
		}},
		{name: "header only", prepare: func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM agent.session_skill_snapshot_item WHERE session_id = ?", sessionID).Error; err != nil {
				return err
			}
			return tx.Exec("DELETE FROM agent.session_command_receipt WHERE command_id = ?", commandID).Error
		}},
		{name: "v2 receipt only", prepare: func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM agent.session_skill_snapshot_item WHERE session_id = ?", sessionID).Error; err != nil {
				return err
			}
			return tx.Exec("DELETE FROM agent.session_skill_snapshot WHERE session_id = ?", sessionID).Error
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tx := client.db.WithContext(context.Background()).Begin()
			if tx.Error != nil {
				t.Fatalf("开始 fail-safe Down 验证事务失败: %v", tx.Error)
			}
			// 单独构造 Down 拒绝条件时需要临时删除其他 V2 fixture；只在待回滚的测试事务中关闭触发器，
			// 并在执行被测 Down 前重新开启，生产 Migration 与运行路径始终保持 append-only。
			if err := tx.Exec("ALTER TABLE agent.session_skill_snapshot DISABLE TRIGGER trg_session_skill_snapshot__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("临时关闭 Header 不可变触发器失败: %v", err)
			}
			if err := tx.Exec("ALTER TABLE agent.session_skill_snapshot_item DISABLE TRIGGER trg_session_skill_snapshot_item__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("临时关闭 Item 不可变触发器失败: %v", err)
			}
			if err := tx.Exec("ALTER TABLE agent.session_command_receipt DISABLE TRIGGER trg_session_command_receipt__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("临时关闭 Receipt 不可变触发器失败: %v", err)
			}
			if err := testCase.prepare(tx); err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("准备独立拒绝条件失败: %v", err)
			}
			if err := tx.Exec("ALTER TABLE agent.session_skill_snapshot ENABLE TRIGGER trg_session_skill_snapshot__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("重新开启 Header 不可变触发器失败: %v", err)
			}
			if err := tx.Exec("ALTER TABLE agent.session_skill_snapshot_item ENABLE TRIGGER trg_session_skill_snapshot_item__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("重新开启 Item 不可变触发器失败: %v", err)
			}
			if err := tx.Exec("ALTER TABLE agent.session_command_receipt ENABLE TRIGGER trg_session_command_receipt__immutable").Error; err != nil {
				_ = tx.Rollback().Error
				t.Fatalf("重新开启 Receipt 不可变触发器失败: %v", err)
			}
			downErr := tx.Exec(string(downSQL)).Error
			_ = tx.Rollback().Error
			if downErr == nil || !strings.Contains(downErr.Error(), "cannot rollback session skill snapshot v2") {
				t.Fatalf("存在 V2 数据时 Down 未 fail-safe 拒绝: %v", downErr)
			}
		})
	}
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("fail-safe Down 后 W1 Schema 损坏: %v", err)
	}
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
		"agent.session":                     {"project_id": true},
		"agent.session_skill_snapshot":      {"session_id": true},
		"agent.session_skill_snapshot_item": {"session_id": true},
		"agent.session_message":             {"source_id": true},
		"agent.session_input":               {"source_id": true},
		"agent.session_command_receipt":     {"command_id": true},
		"agent.session_event_counter":       {"session_id": true},
		"agent.session_event_log":           {"source_id": true},
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
