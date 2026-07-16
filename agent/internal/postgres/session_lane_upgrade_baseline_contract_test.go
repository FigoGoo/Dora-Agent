package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// sessionLaneUpgradeBaselineExpectation 冻结 Migration 005 legacy 升级输入所需的逐值基线。
type sessionLaneUpgradeBaselineExpectation struct {
	commandID     string
	projectID     string
	ownerUserID   string
	commandType   string
	requestDigest string
	prompt        string
	promptDigest  string
	resultVersion int
	hasPrompt     bool
	createdAt     time.Time
}

// sessionLaneUpgradeMigrationState 承载 golang-migrate 在契约库记录的单一版本与脏状态。
type sessionLaneUpgradeMigrationState struct {
	Version int64 `gorm:"column:version"`
	Dirty   bool  `gorm:"column:dirty"`
}

// sessionLaneUpgradeBaselineV005CaptureRepository 只承接正式 Service 已完成 canonical、加密、
// UUIDv7、时间和 Event 构造后的 EnsurePlan。它刻意不复用当前生产 SessionRepository：未来
// Migration 006+ 即使扩展生产 Model/Repository，这个测试仍须能在精确 Migration 005 上生成旧事实。
// 真正落库由下方显式列名 writer 完成，因此这里不模拟生产锁、重放或查询能力。
type sessionLaneUpgradeBaselineV005CaptureRepository struct {
	pendingPlan *session.EnsurePlan
}

// Ensure 捕获一次正式 Service 生成的计划并返回与 Migration 005 一致的创建结果；
// 上一计划未消费时失败，避免不同 cohort 的事实被静默串写。
func (repository *sessionLaneUpgradeBaselineV005CaptureRepository) Ensure(
	_ context.Context,
	plan session.EnsurePlan,
) (session.EnsureResult, error) {
	if repository.pendingPlan != nil {
		return session.EnsureResult{}, fmt.Errorf("legacy-v005 capture repository has an unconsumed plan")
	}
	repository.pendingPlan = &plan
	return session.EnsureResult{
		CommandID:           plan.Receipt.CommandID,
		SessionID:           plan.Session.ID,
		MessageID:           cloneSessionLaneUpgradeBaselineOptionalID(plan.Receipt.MessageID),
		InputID:             cloneSessionLaneUpgradeBaselineOptionalID(plan.Receipt.InputID),
		Disposition:         session.EnsureDispositionCreated,
		ResultVersion:       plan.Receipt.ResultVersion,
		SkillSnapshotDigest: plan.Receipt.SkillSnapshotDigest,
		SkillCount:          plan.Receipt.SkillCount,
		AcceptedAt:          plan.Receipt.CompletedAt,
	}, nil
}

// Query 固定返回未找到；legacy fixture 只覆盖首次创建，不模拟生产 Receipt 查询与重放。
func (*sessionLaneUpgradeBaselineV005CaptureRepository) Query(
	context.Context,
	session.QueryCommand,
) (session.QueryCommandResult, error) {
	return session.QueryCommandResult{Status: session.QueryCommandStatusNotFound}, nil
}

// LoadSkillSnapshot 明确拒绝读取；本基线只接受 Service 已构造的 exact empty Snapshot。
func (*sessionLaneUpgradeBaselineV005CaptureRepository) LoadSkillSnapshot(
	context.Context,
	string,
	int,
) (session.StoredSkillSnapshot, error) {
	return session.StoredSkillSnapshot{}, fmt.Errorf("legacy-v005 capture repository does not support snapshot reads")
}

// takePlan 原子消费当前捕获计划；缺少计划时失败，防止写入空或上一 cohort 的事实。
func (repository *sessionLaneUpgradeBaselineV005CaptureRepository) takePlan() (session.EnsurePlan, error) {
	if repository.pendingPlan == nil {
		return session.EnsurePlan{}, fmt.Errorf("legacy-v005 capture repository has no pending plan")
	}
	plan := *repository.pendingPlan
	repository.pendingPlan = nil
	return plan, nil
}

// TestSessionLaneUpgradeBaselineJSONStrict 锁定 legacy Event Payload 对未知字段和尾随 JSON 的失败关闭行为。
func TestSessionLaneUpgradeBaselineJSONStrict(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		encoded string
		wantErr bool
	}{
		{name: "exact", encoded: `{"session_id":"s","project_id":"p","status":"active","version":1}`},
		{name: "unknown_field", encoded: `{"session_id":"s","project_id":"p","status":"active","version":1,"future":true}`, wantErr: true},
		{name: "trailing_json", encoded: `{"session_id":"s","project_id":"p","status":"active","version":1}{}`, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var payload event.SessionCreatedPayload
			err := decodeSessionLaneUpgradeBaselineJSON(testCase.encoded, &payload)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("strict JSON err=%v wantErr=%t", err, testCase.wantErr)
			}
		})
	}
}

// TestSessionLaneUpgradeBaselinePostgreSQLContract 使用正式 canonical/Service 生成 EnsurePlan，再由测试内冻结的
// legacy-v005 writer 写入真实 Migration 005。V1 prompt、V2 empty Skill prompt 与 V1 empty prompt 三类 cohort
// 不依赖未来生产 Repository/Model，且逐值锁定后续升级 Helper 的输入基线。
func TestSessionLaneUpgradeBaselinePostgreSQLContract(t *testing.T) {
	dsn := os.Getenv("DORA_POSTGRES_CONTRACT_DSN")
	if dsn == "" {
		t.Skip("未设置 DORA_POSTGRES_CONTRACT_DSN，跳过真实 PostgreSQL Session Lane 升级基线契约测试")
	}

	ctx := context.Background()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 8, MaxIdleConns: 4,
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
	assertSessionLaneUpgradeBaselineTables(t, client)

	captureRepository := &sessionLaneUpgradeBaselineV005CaptureRepository{}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtector(
		[]byte("0123456789abcdef0123456789abcdef"), "upgrade-baseline-skill-key-v1",
	)
	if err != nil {
		t.Fatalf("创建 Skill Snapshot 保护器失败: %v", err)
	}
	baselineAt := time.Date(2026, 7, 15, 6, 30, 0, 123456000, time.UTC)
	service, err := session.NewServiceWithSkillSnapshot(
		captureRepository, idgen.UUIDv7{}, contractClock{now: baselineAt},
		contractProtector{}, snapshotProtector, skill.DefaultLimitsProfileV1(),
	)
	if err != nil {
		t.Fatalf("创建 Session Service 失败: %v", err)
	}

	t.Run("v1_prompt", func(t *testing.T) {
		projectID := mustContractUUIDv7(t)
		ownerUserID := mustContractUUIDv7(t)
		commandID := mustContractUUIDv7(t)
		prompt := "W2 legacy V1 prompt"
		requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
			projectID, ownerUserID, prompt, session.SkillSnapshotKindEmpty,
		)
		if err != nil {
			t.Fatalf("计算 V1 prompt canonical 摘要失败: %v", err)
		}
		if !promptPresent {
			t.Fatal("V1 prompt canonical 被错误折叠为空")
		}
		result, err := service.EnsureProjectSession(ctx, session.EnsureCommand{
			SchemaVersion:     session.EnsureCommandSchemaVersionV1,
			RequestID:         mustContractUUIDv7(t),
			CommandID:         commandID,
			RequestDigest:     requestDigest,
			ProjectID:         projectID,
			OwnerUserID:       ownerUserID,
			CreationSource:    session.CreationSourceQuickCreate,
			InitialPrompt:     prompt,
			PromptDigest:      promptDigest,
			SkillSnapshotMode: session.SkillSnapshotKindEmpty,
			RequestedAt:       baselineAt.Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("创建 V1 prompt legacy cohort 失败: %v", err)
		}
		persistSessionLaneUpgradeBaselineV005Plan(t, ctx, client, captureRepository)
		assertSessionLaneUpgradeBaseline(t, client, result, sessionLaneUpgradeBaselineExpectation{
			commandID: commandID, projectID: projectID, ownerUserID: ownerUserID,
			commandType: session.CommandTypeEnsureProjectSessionV1, requestDigest: requestDigest,
			prompt: prompt, promptDigest: promptDigest, resultVersion: session.ResultVersionV1,
			hasPrompt: true, createdAt: baselineAt,
		})
	})

	t.Run("v2_empty_skill_prompt", func(t *testing.T) {
		projectID := mustContractUUIDv7(t)
		ownerUserID := mustContractUUIDv7(t)
		commandID := mustContractUUIDv7(t)
		prompt := "W2 legacy V2 empty Skill prompt"
		snapshot := skill.SessionSkillSnapshotV1{
			SchemaVersion:     skill.SnapshotSchemaVersionV1,
			SnapshotKind:      skill.SessionSkillSnapshotKindEmptyV1,
			SkillCount:        0,
			SnapshotSetDigest: skill.EmptySnapshotSetDigestHex,
			Skills:            make([]skill.PublishedSkillSnapshotRefV1, 0),
		}
		canonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
			SchemaVersion: skill.EnsureProjectSessionSchemaVersionV2,
			ProjectID:     projectID, OwnerUserID: ownerUserID,
			CreationSource: skill.CreationSourceQuickCreate,
			InitialPrompt:  prompt, SkillSnapshot: snapshot,
		}, skill.DefaultLimitsProfileV1())
		if err != nil {
			t.Fatalf("计算 V2 empty Skill prompt canonical 摘要失败: %v", err)
		}
		if !canonical.PromptPresent {
			t.Fatal("V2 prompt canonical 被错误折叠为空")
		}
		result, err := service.EnsureProjectSessionV2(ctx, session.EnsureCommandV2{
			SchemaVersion:  skill.EnsureProjectSessionSchemaVersionV2,
			RequestID:      mustContractUUIDv7(t),
			CommandID:      commandID,
			RequestDigest:  canonical.RequestDigest.Hex(),
			ProjectID:      projectID,
			OwnerUserID:    ownerUserID,
			CreationSource: skill.CreationSourceQuickCreate,
			InitialPrompt:  prompt,
			PromptDigest:   canonical.PromptDigest,
			SkillSnapshot:  snapshot,
			RequestedAt:    baselineAt.Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("创建 V2 empty Skill prompt legacy cohort 失败: %v", err)
		}
		persistSessionLaneUpgradeBaselineV005Plan(t, ctx, client, captureRepository)
		assertSessionLaneUpgradeBaseline(t, client, result, sessionLaneUpgradeBaselineExpectation{
			commandID: commandID, projectID: projectID, ownerUserID: ownerUserID,
			commandType:   session.CommandTypeEnsureProjectSessionV2,
			requestDigest: canonical.RequestDigest.Hex(), prompt: canonical.NormalizedPrompt,
			promptDigest: canonical.PromptDigest, resultVersion: session.ResultVersionV2,
			hasPrompt: true, createdAt: baselineAt,
		})
	})

	t.Run("v1_empty_prompt", func(t *testing.T) {
		projectID := mustContractUUIDv7(t)
		ownerUserID := mustContractUUIDv7(t)
		commandID := mustContractUUIDv7(t)
		prompt := "\u00a0\u3000"
		requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
			projectID, ownerUserID, prompt, session.SkillSnapshotKindEmpty,
		)
		if err != nil {
			t.Fatalf("计算 V1 empty prompt canonical 摘要失败: %v", err)
		}
		if promptPresent || promptDigest != "" {
			t.Fatalf("V1 Unicode blank 未折叠为空: present=%t digest=%q", promptPresent, promptDigest)
		}
		result, err := service.EnsureProjectSession(ctx, session.EnsureCommand{
			SchemaVersion:     session.EnsureCommandSchemaVersionV1,
			RequestID:         mustContractUUIDv7(t),
			CommandID:         commandID,
			RequestDigest:     requestDigest,
			ProjectID:         projectID,
			OwnerUserID:       ownerUserID,
			CreationSource:    session.CreationSourceQuickCreate,
			InitialPrompt:     prompt,
			PromptDigest:      promptDigest,
			SkillSnapshotMode: session.SkillSnapshotKindEmpty,
			RequestedAt:       baselineAt.Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("创建 V1 empty prompt legacy cohort 失败: %v", err)
		}
		persistSessionLaneUpgradeBaselineV005Plan(t, ctx, client, captureRepository)
		assertSessionLaneUpgradeBaseline(t, client, result, sessionLaneUpgradeBaselineExpectation{
			commandID: commandID, projectID: projectID, ownerUserID: ownerUserID,
			commandType: session.CommandTypeEnsureProjectSessionV1, requestDigest: requestDigest,
			promptDigest: "", resultVersion: session.ResultVersionV1,
			hasPrompt: false, createdAt: baselineAt,
		})
	})
}

// persistSessionLaneUpgradeBaselineV005Plan 消费一次 Service 产出的计划并写入精确 005 Schema。
// capture 与 writer 分离可防止测试误调用未来生产 Repository，同时仍让 canonical、保护和 Event
// 构造继续走正式领域代码。
func persistSessionLaneUpgradeBaselineV005Plan(
	t *testing.T,
	ctx context.Context,
	client *Client,
	repository *sessionLaneUpgradeBaselineV005CaptureRepository,
) {
	t.Helper()
	plan, err := repository.takePlan()
	if err != nil {
		t.Fatalf("取得 legacy-v005 EnsurePlan 失败: %v", err)
	}
	if err := writeSessionLaneUpgradeBaselineV005Plan(ctx, client, plan); err != nil {
		t.Fatalf("写入 legacy-v005 EnsurePlan 失败: %v", err)
	}
}

// writeSessionLaneUpgradeBaselineV005Plan 是 Migration 005 专用 fixture writer。全部 INSERT 固定表名和
// 列名，禁止复用 GORM Model、AutoMigrate、生产 mapper 或 SessionRepository；006+ 的字段扩展不得改变
// 这段 legacy 输入。它只服务 reset 后的单线程契约库，不模拟生产 advisory lock/replay。
func writeSessionLaneUpgradeBaselineV005Plan(
	ctx context.Context,
	client *Client,
	plan session.EnsurePlan,
) error {
	if client == nil || client.db == nil {
		return fmt.Errorf("legacy-v005 writer requires postgres client")
	}
	if plan.Session.ID == "" || plan.SkillSnapshot.SessionID != plan.Session.ID ||
		plan.SequenceCounter.SessionID != plan.Session.ID || plan.RuntimeLease.SessionID != plan.Session.ID ||
		plan.Receipt.SessionID != plan.Session.ID {
		return fmt.Errorf("legacy-v005 plan has inconsistent session identity")
	}
	if plan.SkillSnapshot.Kind != session.SkillSnapshotKindEmpty ||
		plan.SkillSnapshot.SchemaVersion != session.SkillSnapshotSchemaVersionV1 ||
		plan.SkillSnapshot.SkillCount != 0 || len(plan.SkillSnapshotItems) != 0 ||
		plan.SkillSnapshot.Digest != session.EmptySkillSnapshotDigest ||
		plan.SkillSnapshot.PublishedSnapshotRefsJSON != "[]" {
		return fmt.Errorf("legacy-v005 baseline only accepts the exact empty Skill Snapshot")
	}
	if (plan.Message == nil) != (plan.Input == nil) || len(plan.Events) < 1 || len(plan.Events) > 2 {
		return fmt.Errorf("legacy-v005 plan has invalid optional facts")
	}

	return client.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO agent.session
				(id, project_id, user_id, status, version, created_at, updated_at, archived_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			plan.Session.ID, plan.Session.ProjectID, plan.Session.UserID, string(plan.Session.Status),
			plan.Session.Version, plan.Session.CreatedAt, plan.Session.UpdatedAt, plan.Session.ArchivedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 session: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO agent.session_skill_snapshot
				(session_id, snapshot_kind, snapshot_digest, published_snapshot_refs,
				 schema_version, skill_count, created_at)
			VALUES (?, ?, ?, CAST(? AS jsonb), ?, ?, ?)`,
			plan.SkillSnapshot.SessionID, string(plan.SkillSnapshot.Kind), plan.SkillSnapshot.Digest,
			plan.SkillSnapshot.PublishedSnapshotRefsJSON, plan.SkillSnapshot.SchemaVersion,
			plan.SkillSnapshot.SkillCount, plan.SkillSnapshot.CreatedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 Skill Snapshot: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO agent.session_sequence_counter
				(session_id, last_message_seq, last_input_enqueue_seq, updated_at)
			VALUES (?, ?, ?, ?)`,
			plan.SequenceCounter.SessionID, plan.SequenceCounter.LastMessageSeq,
			plan.SequenceCounter.LastInputEnqueueSeq, plan.SequenceCounter.UpdatedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 Sequence Counter: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO agent.session_runtime_lease
				(session_id, lease_owner, lease_until, fence_token, version, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			plan.RuntimeLease.SessionID, plan.RuntimeLease.LeaseOwner, plan.RuntimeLease.LeaseUntil,
			plan.RuntimeLease.FenceToken, plan.RuntimeLease.Version, plan.RuntimeLease.UpdatedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 Runtime Lease: %w", err)
		}

		if plan.Message != nil {
			if err := tx.Exec(`
				INSERT INTO agent.session_message
					(id, session_id, message_seq, role, content_ciphertext, content_key_version,
					 content_digest, source_kind, source_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				plan.Message.ID, plan.Message.SessionID, plan.Message.Seq, string(plan.Message.Role),
				plan.Message.Content.Ciphertext, plan.Message.Content.KeyVersion, plan.Message.ContentDigest,
				plan.Message.SourceKind, plan.Message.SourceID, plan.Message.CreatedAt,
			).Error; err != nil {
				return fmt.Errorf("insert legacy-v005 Message: %w", err)
			}
			if err := tx.Exec(`
				INSERT INTO agent.session_input
					(id, session_id, source_type, source_id, message_id, status, enqueue_seq, attempts,
					 available_at, lease_owner, lease_until, fence_token, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				plan.Input.ID, plan.Input.SessionID, string(plan.Input.SourceType), plan.Input.SourceID,
				plan.Input.MessageID, string(plan.Input.Status), plan.Input.EnqueueSeq, plan.Input.Attempts,
				plan.Input.AvailableAt, plan.Input.LeaseOwner, plan.Input.LeaseUntil, plan.Input.FenceToken,
				plan.Input.CreatedAt, plan.Input.UpdatedAt,
			).Error; err != nil {
				return fmt.Errorf("insert legacy-v005 Input: %w", err)
			}
		}

		if err := tx.Exec(`
			INSERT INTO agent.session_event_counter
				(session_id, last_seq, min_available_seq, updated_at)
			VALUES (?, ?, ?, ?)`,
			plan.Session.ID, len(plan.Events), 1, plan.Session.CreatedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 Event Counter: %w", err)
		}
		// 005 Ensure 只有一条 created Event，或 created+accepted 两条 Event。固定两种批量 SQL，
		// 避免在同一事务循环执行同构 INSERT，并把投影序号与行数保持为显式契约。
		switch len(plan.Events) {
		case 1:
			record := plan.Events[0]
			if err := tx.Exec(`
				INSERT INTO agent.session_event_log
					(event_id, session_id, seq, event_type, schema_version, source_kind, source_id,
					 projection_index, aggregate_type, aggregate_id, aggregate_version, payload, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb), ?)`,
				record.EventID, record.SessionID, 1, string(record.Type), record.SchemaVersion,
				record.SourceKind, record.SourceID, record.ProjectionIndex, string(record.AggregateType),
				record.AggregateID, record.AggregateVersion, string(record.PayloadJSON), record.CreatedAt,
			).Error; err != nil {
				return fmt.Errorf("insert legacy-v005 Event: %w", err)
			}
		case 2:
			created := plan.Events[0]
			accepted := plan.Events[1]
			if err := tx.Exec(`
				INSERT INTO agent.session_event_log
					(event_id, session_id, seq, event_type, schema_version, source_kind, source_id,
					 projection_index, aggregate_type, aggregate_id, aggregate_version, payload, created_at)
				VALUES
					(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb), ?),
					(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb), ?)`,
				created.EventID, created.SessionID, 1, string(created.Type), created.SchemaVersion,
				created.SourceKind, created.SourceID, created.ProjectionIndex, string(created.AggregateType),
				created.AggregateID, created.AggregateVersion, string(created.PayloadJSON), created.CreatedAt,
				accepted.EventID, accepted.SessionID, 2, string(accepted.Type), accepted.SchemaVersion,
				accepted.SourceKind, accepted.SourceID, accepted.ProjectionIndex, string(accepted.AggregateType),
				accepted.AggregateID, accepted.AggregateVersion, string(accepted.PayloadJSON), accepted.CreatedAt,
			).Error; err != nil {
				return fmt.Errorf("insert legacy-v005 Event batch: %w", err)
			}
		default:
			return fmt.Errorf("legacy-v005 Event count changed after validation: %d", len(plan.Events))
		}
		if err := tx.Exec(`
			INSERT INTO agent.session_command_receipt
				(command_id, command_type, request_digest, session_id, message_id, input_id,
				 result_version, skill_snapshot_digest, skill_count, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			plan.Receipt.CommandID, plan.Receipt.CommandType, plan.Receipt.RequestDigest,
			plan.Receipt.SessionID, plan.Receipt.MessageID, plan.Receipt.InputID,
			plan.Receipt.ResultVersion, plan.Receipt.SkillSnapshotDigest,
			plan.Receipt.SkillCount, plan.Receipt.CompletedAt,
		).Error; err != nil {
			return fmt.Errorf("insert legacy-v005 Receipt: %w", err)
		}
		return nil
	})
}

// assertSessionLaneUpgradeBaselineTables 锁定当前真实基线只包含 Client 明确要求的 Foundation 表。
// Authority、Turn 与 Upgrade Ledger 尚无生产表，因此不得用测试私建表伪造目标升级事实。
func assertSessionLaneUpgradeBaselineTables(t *testing.T, client *Client) {
	t.Helper()
	var migrationStates []sessionLaneUpgradeMigrationState
	if err := client.db.WithContext(context.Background()).Raw(
		"SELECT version, dirty FROM schema_migrations",
	).Scan(&migrationStates).Error; err != nil {
		t.Fatalf("查询 Agent Migration 版本失败: %v", err)
	}
	if len(migrationStates) != 1 || migrationStates[0].Version != 20260714000500 || migrationStates[0].Dirty {
		t.Fatalf("Session Lane 升级基线必须精确位于干净 Migration 005: got=%+v", migrationStates)
	}
	var records []tableNameRecord
	if err := client.db.WithContext(context.Background()).Raw(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = ? AND table_type = 'BASE TABLE'
		ORDER BY table_name`, schemaName).Scan(&records).Error; err != nil {
		t.Fatalf("查询 Agent Foundation 表集合失败: %v", err)
	}
	actual := make([]string, 0, len(records))
	for _, record := range records {
		actual = append(actual, record.TableName)
	}
	expected := []string{
		"session",
		"session_command_receipt",
		"session_event_counter",
		"session_event_log",
		"session_input",
		"session_message",
		"session_runtime_lease",
		"session_sequence_counter",
		"session_skill_snapshot",
		"session_skill_snapshot_item",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Migration 005 Foundation 表集合漂移: got=%v want=%v", actual, expected)
	}
}

// assertSessionLaneUpgradeBaseline 逐值断言一个 legacy cohort 的全部现有 Foundation 事实。
func assertSessionLaneUpgradeBaseline(
	t *testing.T,
	client *Client,
	result session.EnsureResult,
	expected sessionLaneUpgradeBaselineExpectation,
) {
	t.Helper()
	ctx := context.Background()
	assertSessionLaneUpgradeBaselineUUIDv7(t, "result.session_id", result.SessionID)
	if result.CommandID != expected.commandID || result.Disposition != session.EnsureDispositionCreated ||
		result.ResultVersion != expected.resultVersion ||
		result.SkillSnapshotDigest != session.EmptySkillSnapshotDigest || result.SkillCount != 0 ||
		!result.AcceptedAt.Equal(expected.createdAt) {
		t.Fatalf("Ensure result 基线漂移: got=%+v", result)
	}
	if expected.hasPrompt {
		if result.MessageID == nil || result.InputID == nil {
			t.Fatalf("非空 Prompt Ensure result 缺少 Message/Input: %+v", result)
		}
		assertSessionLaneUpgradeBaselineUUIDv7(t, "result.message_id", *result.MessageID)
		assertSessionLaneUpgradeBaselineUUIDv7(t, "result.input_id", *result.InputID)
	} else if result.MessageID != nil || result.InputID != nil {
		t.Fatalf("空 Prompt Ensure result 意外生成 Message/Input: %+v", result)
	}

	var sessions []sessionModel
	if err := client.db.WithContext(ctx).Where("id = ?", result.SessionID).Find(&sessions).Error; err != nil {
		t.Fatalf("查询 Session 基线失败: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Session 基线数量错误: got=%d want=1", len(sessions))
	}
	storedSession := sessions[0]
	if storedSession.ID != result.SessionID || storedSession.ProjectID != expected.projectID ||
		storedSession.UserID != expected.ownerUserID || storedSession.Status != string(session.StatusActive) ||
		storedSession.Version != 1 || storedSession.ArchivedAt != nil ||
		!storedSession.CreatedAt.Equal(expected.createdAt) || !storedSession.UpdatedAt.Equal(expected.createdAt) {
		t.Fatalf("Session 基线逐值断言失败: got=%+v", storedSession)
	}

	var receipts []sessionCommandReceiptModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Find(&receipts).Error; err != nil {
		t.Fatalf("查询 Receipt 基线失败: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("Receipt 基线数量错误: got=%d want=1", len(receipts))
	}
	receipt := receipts[0]
	if receipt.CommandID != expected.commandID || receipt.CommandType != expected.commandType ||
		receipt.RequestDigest != expected.requestDigest || receipt.SessionID != result.SessionID ||
		receipt.ResultVersion != expected.resultVersion || receipt.SkillSnapshotDigest != session.EmptySkillSnapshotDigest ||
		receipt.SkillCount != 0 || !receipt.CompletedAt.Equal(expected.createdAt) {
		t.Fatalf("Receipt 基线逐值断言失败: got=%+v", receipt)
	}
	assertSessionLaneUpgradeBaselineOptionalID(t, "receipt.message_id", receipt.MessageID, result.MessageID)
	assertSessionLaneUpgradeBaselineOptionalID(t, "receipt.input_id", receipt.InputID, result.InputID)

	var snapshots []sessionSkillSnapshotModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Find(&snapshots).Error; err != nil {
		t.Fatalf("查询 Snapshot Header 基线失败: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("Snapshot Header 基线数量错误: got=%d want=1", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.SessionID != result.SessionID || snapshot.SchemaVersion != session.SkillSnapshotSchemaVersionV1 ||
		snapshot.SnapshotKind != string(session.SkillSnapshotKindEmpty) || snapshot.SkillCount != 0 ||
		snapshot.SnapshotDigest != session.EmptySkillSnapshotDigest || !snapshot.CreatedAt.Equal(expected.createdAt) {
		t.Fatalf("Snapshot Header 基线逐值断言失败: got=%+v", snapshot)
	}
	var publishedRefs []json.RawMessage
	if err := json.Unmarshal([]byte(snapshot.PublishedSnapshotRefs), &publishedRefs); err != nil {
		t.Fatalf("Snapshot published refs 不是合法 JSON: %v", err)
	}
	if publishedRefs == nil || len(publishedRefs) != 0 {
		t.Fatalf("Snapshot published refs 不是显式空数组: raw=%q", snapshot.PublishedSnapshotRefs)
	}
	var snapshotItemCount int64
	if err := client.db.WithContext(ctx).Model(&sessionSkillSnapshotItemModel{}).
		Where("session_id = ?", result.SessionID).Count(&snapshotItemCount).Error; err != nil {
		t.Fatalf("查询 Snapshot Item 基线失败: %v", err)
	}
	if snapshotItemCount != 0 {
		t.Fatalf("empty Skill cohort 意外生成 Snapshot Item: got=%d", snapshotItemCount)
	}

	wantSequence := int64(0)
	if expected.hasPrompt {
		wantSequence = 1
	}
	var sequenceCounters []sessionSequenceCounterModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Find(&sequenceCounters).Error; err != nil {
		t.Fatalf("查询 Sequence Counter 基线失败: %v", err)
	}
	if len(sequenceCounters) != 1 {
		t.Fatalf("Sequence Counter 基线数量错误: got=%d want=1", len(sequenceCounters))
	}
	sequenceCounter := sequenceCounters[0]
	if sequenceCounter.SessionID != result.SessionID || sequenceCounter.LastMessageSeq != wantSequence ||
		sequenceCounter.LastInputEnqueueSeq != wantSequence || !sequenceCounter.UpdatedAt.Equal(expected.createdAt) {
		t.Fatalf("Sequence Counter 基线逐值断言失败: got=%+v", sequenceCounter)
	}

	var runtimeLeases []sessionRuntimeLeaseModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Find(&runtimeLeases).Error; err != nil {
		t.Fatalf("查询 Runtime Lease 基线失败: %v", err)
	}
	if len(runtimeLeases) != 1 {
		t.Fatalf("Runtime Lease 基线数量错误: got=%d want=1", len(runtimeLeases))
	}
	runtimeLease := runtimeLeases[0]
	if runtimeLease.SessionID != result.SessionID || runtimeLease.LeaseOwner != nil || runtimeLease.LeaseUntil != nil ||
		runtimeLease.FenceToken != 0 || runtimeLease.Version != 1 || !runtimeLease.UpdatedAt.Equal(expected.createdAt) {
		t.Fatalf("Runtime Lease 基线逐值断言失败: got=%+v", runtimeLease)
	}

	assertSessionLaneUpgradeBaselineMessageAndInput(t, client, result, expected)
	assertSessionLaneUpgradeBaselineEvents(t, client, result, expected)
}

// assertSessionLaneUpgradeBaselineMessageAndInput 断言 prompt cohort 的单一 Message/Input 及 empty cohort 的零事实。
func assertSessionLaneUpgradeBaselineMessageAndInput(
	t *testing.T,
	client *Client,
	result session.EnsureResult,
	expected sessionLaneUpgradeBaselineExpectation,
) {
	t.Helper()
	ctx := context.Background()
	var messages []sessionMessageModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Order("message_seq").Find(&messages).Error; err != nil {
		t.Fatalf("查询 Message 基线失败: %v", err)
	}
	var inputs []sessionInputModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Order("enqueue_seq").Find(&inputs).Error; err != nil {
		t.Fatalf("查询 Input 基线失败: %v", err)
	}
	if !expected.hasPrompt {
		if len(messages) != 0 || len(inputs) != 0 {
			t.Fatalf("空 Prompt cohort 意外生成 Message/Input: messages=%d inputs=%d", len(messages), len(inputs))
		}
		return
	}
	if len(messages) != 1 || len(inputs) != 1 {
		t.Fatalf("Prompt cohort Message/Input 数量错误: messages=%d inputs=%d", len(messages), len(inputs))
	}

	protected, err := (contractProtector{}).Protect(ctx, []byte(expected.prompt))
	if err != nil {
		t.Fatalf("构造预期 Prompt Envelope 失败: %v", err)
	}
	message := messages[0]
	if message.ID != *result.MessageID || message.SessionID != result.SessionID || message.MessageSeq != 1 ||
		message.Role != string(session.MessageRoleUser) || message.ContentKeyVersion != protected.KeyVersion ||
		message.ContentDigest != expected.promptDigest || message.SourceKind != event.SourceKindEnsureProjectSession ||
		message.SourceID != expected.commandID || !message.CreatedAt.Equal(expected.createdAt) ||
		!bytes.Equal(message.ContentCiphertext, protected.Ciphertext) {
		t.Fatalf("Message 基线逐值断言失败: got=%+v", message)
	}
	if err := session.ValidateEnvelopeV1(message.ContentCiphertext); err != nil {
		t.Fatalf("Message Prompt Envelope 非法: %v", err)
	}

	input := inputs[0]
	if input.ID != *result.InputID || input.SessionID != result.SessionID ||
		input.SourceType != string(session.InputSourceTypeUserMessage) || input.SourceID != expected.commandID ||
		input.MessageID == nil || *input.MessageID != *result.MessageID || input.Status != string(session.InputStatusPending) ||
		input.EnqueueSeq != 1 || input.Attempts != 0 || !input.AvailableAt.Equal(expected.createdAt) ||
		input.LeaseOwner != nil || input.LeaseUntil != nil || input.FenceToken != 0 ||
		!input.CreatedAt.Equal(expected.createdAt) || !input.UpdatedAt.Equal(expected.createdAt) {
		t.Fatalf("Input 基线逐值断言失败: got=%+v", input)
	}
}

// assertSessionLaneUpgradeBaselineEvents 断言创建/接收事件、载荷及 Event Counter 的完整现有基线。
func assertSessionLaneUpgradeBaselineEvents(
	t *testing.T,
	client *Client,
	result session.EnsureResult,
	expected sessionLaneUpgradeBaselineExpectation,
) {
	t.Helper()
	ctx := context.Background()
	wantEventCount := 1
	if expected.hasPrompt {
		wantEventCount = 2
	}
	var events []sessionEventLogModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Order("seq").Find(&events).Error; err != nil {
		t.Fatalf("查询 Event 基线失败: %v", err)
	}
	if len(events) != wantEventCount {
		t.Fatalf("Event 基线数量错误: got=%d want=%d", len(events), wantEventCount)
	}

	created := events[0]
	assertSessionLaneUpgradeBaselineUUIDv7(t, "session.created event_id", created.EventID)
	if created.SessionID != result.SessionID || created.Seq != 1 || created.EventType != string(event.TypeSessionCreated) ||
		created.SchemaVersion != event.SchemaVersionV1 || created.SourceKind != event.SourceKindEnsureProjectSession ||
		created.SourceID != expected.commandID || created.ProjectionIndex != 0 ||
		created.AggregateType != string(event.AggregateTypeSession) || created.AggregateID != result.SessionID ||
		created.AggregateVersion != 1 || !created.CreatedAt.Equal(expected.createdAt) {
		t.Fatalf("session.created Event 基线逐值断言失败: got=%+v", created)
	}
	var createdPayload event.SessionCreatedPayload
	if err := decodeSessionLaneUpgradeBaselineJSON(created.Payload, &createdPayload); err != nil {
		t.Fatalf("解析 session.created payload 失败: %v", err)
	}
	wantCreatedPayload := event.SessionCreatedPayload{
		SessionID: result.SessionID, ProjectID: expected.projectID,
		Status: string(session.StatusActive), Version: 1,
	}
	if !reflect.DeepEqual(createdPayload, wantCreatedPayload) {
		t.Fatalf("session.created payload 基线漂移: got=%+v want=%+v", createdPayload, wantCreatedPayload)
	}

	if expected.hasPrompt {
		accepted := events[1]
		assertSessionLaneUpgradeBaselineUUIDv7(t, "session.input.accepted event_id", accepted.EventID)
		if accepted.SessionID != result.SessionID || accepted.Seq != 2 ||
			accepted.EventType != string(event.TypeSessionInputAccepted) || accepted.SchemaVersion != event.SchemaVersionV1 ||
			accepted.SourceKind != event.SourceKindEnsureProjectSession || accepted.SourceID != expected.commandID ||
			accepted.ProjectionIndex != 1 || accepted.AggregateType != string(event.AggregateTypeSessionInput) ||
			accepted.AggregateID != *result.InputID || accepted.AggregateVersion != 1 ||
			!accepted.CreatedAt.Equal(expected.createdAt) {
			t.Fatalf("session.input.accepted Event 基线逐值断言失败: got=%+v", accepted)
		}
		var acceptedPayload event.SessionInputAcceptedPayload
		if err := decodeSessionLaneUpgradeBaselineJSON(accepted.Payload, &acceptedPayload); err != nil {
			t.Fatalf("解析 session.input.accepted payload 失败: %v", err)
		}
		wantAcceptedPayload := event.SessionInputAcceptedPayload{
			SessionID: result.SessionID, InputID: *result.InputID, MessageID: *result.MessageID,
			EnqueueSeq: 1, Status: string(session.InputStatusPending),
		}
		if !reflect.DeepEqual(acceptedPayload, wantAcceptedPayload) {
			t.Fatalf("session.input.accepted payload 基线漂移: got=%+v want=%+v", acceptedPayload, wantAcceptedPayload)
		}
	}

	var eventCounters []sessionEventCounterModel
	if err := client.db.WithContext(ctx).Where("session_id = ?", result.SessionID).Find(&eventCounters).Error; err != nil {
		t.Fatalf("查询 Event Counter 基线失败: %v", err)
	}
	if len(eventCounters) != 1 {
		t.Fatalf("Event Counter 基线数量错误: got=%d want=1", len(eventCounters))
	}
	eventCounter := eventCounters[0]
	if eventCounter.SessionID != result.SessionID || eventCounter.LastSeq != int64(wantEventCount) ||
		eventCounter.MinAvailableSeq != 1 || !eventCounter.UpdatedAt.Equal(expected.createdAt) {
		t.Fatalf("Event Counter 基线逐值断言失败: got=%+v", eventCounter)
	}
}

// decodeSessionLaneUpgradeBaselineJSON 严格拒绝未知字段与尾随 JSON，确保 event.v1 的 exact payload
// 不会在基线测试中因 encoding/json 默认忽略扩展字段而被静默放宽。
func decodeSessionLaneUpgradeBaselineJSON(encoded string, target any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	return nil
}

// cloneSessionLaneUpgradeBaselineOptionalID 复制可选 ID，避免测试结果与捕获计划共享可变指针。
func cloneSessionLaneUpgradeBaselineOptionalID(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// assertSessionLaneUpgradeBaselineOptionalID 比较 Receipt 与 Result 的可选 UUIDv7 引用。
func assertSessionLaneUpgradeBaselineOptionalID(t *testing.T, field string, got, want *string) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s nil 状态漂移: got=%v want=%v", field, got, want)
	}
	if got != nil && (*got != *want) {
		t.Fatalf("%s 漂移: got=%s want=%s", field, *got, *want)
	}
}

// assertSessionLaneUpgradeBaselineUUIDv7 校验真实数据库中新生成的稳定 ID 没有降级为其他 UUID 版本。
func assertSessionLaneUpgradeBaselineUUIDv7(t *testing.T, field, value string) {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 {
		t.Fatalf("%s 不是 RFC UUIDv7: value=%q err=%v", field, value, err)
	}
}
