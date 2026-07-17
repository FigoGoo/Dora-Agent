package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
)

// TestUserMessageLegacyUpgradePostgreSQLRecoveryAndConcurrency 覆盖 prepare/apply 响应丢失、rooted anti-join、
// 双 Helper 竞争与“Helper 不创建 Run/Receipt/Event”的真实 PostgreSQL 证据。
func TestUserMessageLegacyUpgradePostgreSQLRecoveryAndConcurrency(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(userMessageRuntimeIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_USER_MESSAGE_RUNTIME_POSTGRES_DSN，跳过真实 PostgreSQL Legacy Helper 探针")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{DSN: dsn, MaxOpenConns: 8, MaxIdleConns: 4,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("连接 Legacy Helper PostgreSQL 失败: %v", err)
	}
	defer client.Close()
	if err := client.VerifyUserMessageRuntimeSchema(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	key := bytes.Repeat([]byte{0x71}, 32)
	contentProtector, err := contentcrypto.NewAES256GCMProtector(key, "legacy-helper-v1")
	if err != nil {
		t.Fatal(err)
	}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtector(key, "legacy-helper-v1")
	if err != nil {
		t.Fatal(err)
	}
	sessionRepository, err := NewSessionRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	sessionService, err := session.NewServiceWithSkillSnapshot(sessionRepository, idgen.UUIDv7{}, clock.System{},
		contentProtector, snapshotProtector, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	upgradeRepository, err := NewUserMessageLegacyUpgradeRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	recoveryService := newLegacyUpgradeIntegrationService(t, upgradeRepository, contentProtector, sessionService)
	createdInputs := make([]string, 0, 3)
	defer func() { cleanupLegacyUpgradeIntegrationFacts(t, client, createdInputs) }()

	firstInput := createLegacyUpgradeIntegrationSession(t, ctx, sessionService, "legacy prepare crash replay")
	createdInputs = append(createdInputs, firstInput)
	before := legacyUpgradeIntegrationSideEffects(t, client, firstInput)
	prepareCrash := &legacyUpgradeCrashRepository{LegacyUpgradeRepository: upgradeRepository, afterStage: usermessageruntime.LegacyUpgradeStagePrepared}
	prepareService := newLegacyUpgradeIntegrationService(t, prepareCrash, contentProtector, sessionService)
	if _, err := prepareService.UpgradeBatch(ctx, 100); !errors.Is(err, errLegacyUpgradeIntegrationCrash) {
		t.Fatalf("prepare crash 未被注入: %v", err)
	}
	assertLegacyUpgradeIntegrationStage(t, client, firstInput, usermessageruntime.LegacyUpgradeStagePrepared)
	if _, err := recoveryService.UpgradeBatch(ctx, 100); err != nil {
		t.Fatalf("prepared restart 恢复失败: %v", err)
	}
	assertLegacyUpgradeIntegrationStage(t, client, firstInput, usermessageruntime.LegacyUpgradeStageVerified)
	after := legacyUpgradeIntegrationSideEffects(t, client, firstInput)
	if before != after || after != (legacyUpgradeSideEffects{Events: 2}) {
		t.Fatalf("Helper 创建了 Run/Receipt/Event: before=%+v after=%+v", before, after)
	}

	secondInput := createLegacyUpgradeIntegrationSession(t, ctx, sessionService, "legacy apply crash replay")
	createdInputs = append(createdInputs, secondInput)
	applyCrash := &legacyUpgradeCrashRepository{LegacyUpgradeRepository: upgradeRepository, afterStage: usermessageruntime.LegacyUpgradeStageApplied}
	applyService := newLegacyUpgradeIntegrationService(t, applyCrash, contentProtector, sessionService)
	if _, err := applyService.UpgradeBatch(ctx, 100); !errors.Is(err, errLegacyUpgradeIntegrationCrash) {
		t.Fatalf("apply crash 未被注入: %v", err)
	}
	assertLegacyUpgradeIntegrationStage(t, client, secondInput, usermessageruntime.LegacyUpgradeStageApplied)
	if _, err := recoveryService.UpgradeBatch(ctx, 100); err != nil {
		t.Fatalf("applied restart 恢复失败: %v", err)
	}
	assertLegacyUpgradeIntegrationStage(t, client, secondInput, usermessageruntime.LegacyUpgradeStageVerified)

	orphanInput := mustUserMessageRuntimeIntegrationID(t, idgen.UUIDv7{})
	orphanDB := client.db.WithContext(ctx).Begin()
	if orphanDB.Error != nil {
		t.Fatal(orphanDB.Error)
	}
	if result := orphanDB.Exec(`INSERT INTO agent.session_user_message_upgrade_ledger
		(input_id,session_id,stage,turn_id,context_digest,upgrade_generation,version,created_at,updated_at)
		VALUES (?,?, 'prepared', ?, ?, 1, 1, clock_timestamp(), clock_timestamp())`, orphanInput,
		mustUserMessageRuntimeIntegrationID(t, idgen.UUIDv7{}), mustUserMessageRuntimeIntegrationID(t, idgen.UUIDv7{}), strings.Repeat("d", 64)); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("插入 orphan Ledger 失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	orphanRepository, err := NewUserMessageLegacyUpgradeRepository(&Client{db: orphanDB})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := orphanRepository.Preview(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Blockers) != 1 || preview.Blockers[0].Code != "legacy_upgrade_incomplete" {
		t.Fatalf("global orphan Ledger 未触发稳定 blocker: %+v", preview.Blockers)
	}
	for _, candidate := range preview.Candidates {
		if candidate.InputID == orphanInput {
			t.Fatal("global orphan Ledger 绕过 Session-rooted anti-join")
		}
	}
	if err := orphanDB.Rollback().Error; err != nil {
		t.Fatal(err)
	}

	thirdInput := createLegacyUpgradeIntegrationSession(t, ctx, sessionService, "legacy double helper")
	createdInputs = append(createdInputs, thirdInput)
	services := []*usermessageruntime.LegacyUpgradeService{
		newLegacyUpgradeIntegrationService(t, upgradeRepository, contentProtector, sessionService),
		newLegacyUpgradeIntegrationService(t, upgradeRepository, contentProtector, sessionService),
	}
	var wait sync.WaitGroup
	errs := make(chan error, len(services))
	for _, service := range services {
		wait.Add(1)
		go func(s *usermessageruntime.LegacyUpgradeService) {
			defer wait.Done()
			_, runErr := s.UpgradeBatch(ctx, 100)
			errs <- runErr
		}(service)
	}
	wait.Wait()
	close(errs)
	for runErr := range errs {
		if runErr != nil {
			t.Fatalf("双 Helper 并发失败: %v", runErr)
		}
	}
	assertLegacyUpgradeIntegrationStage(t, client, thirdInput, usermessageruntime.LegacyUpgradeStageVerified)
	var facts struct{ Ledger, Turn, Context int64 }
	if err := client.db.WithContext(ctx).Raw(`SELECT
		(SELECT count(*) FROM agent.session_user_message_upgrade_ledger WHERE input_id=?) AS ledger,
		(SELECT count(*) FROM agent.session_user_message_turn WHERE input_id=?) AS turn,
		(SELECT count(*) FROM agent.session_user_message_turn_context WHERE input_id=?) AS context`,
		thirdInput, thirdInput, thirdInput).Scan(&facts).Error; err != nil {
		t.Fatal(err)
	}
	if facts.Ledger != 1 || facts.Turn != 1 || facts.Context != 1 {
		t.Fatalf("双 Helper 事实不唯一: %+v", facts)
	}
}

var errLegacyUpgradeIntegrationCrash = errors.New("injected legacy helper crash")

type legacyUpgradeCrashRepository struct {
	usermessageruntime.LegacyUpgradeRepository
	afterStage string
}

func (r *legacyUpgradeCrashRepository) Prepare(ctx context.Context, p usermessageruntime.LegacyUpgradePreparePlan) (usermessageruntime.LegacyUpgradeLedger, error) {
	ledger, err := r.LegacyUpgradeRepository.Prepare(ctx, p)
	if err == nil && r.afterStage == usermessageruntime.LegacyUpgradeStagePrepared {
		return ledger, errLegacyUpgradeIntegrationCrash
	}
	return ledger, err
}
func (r *legacyUpgradeCrashRepository) Apply(ctx context.Context, p usermessageruntime.LegacyUpgradeApplyPlan) (usermessageruntime.LegacyUpgradeLedger, error) {
	ledger, err := r.LegacyUpgradeRepository.Apply(ctx, p)
	if err == nil && r.afterStage == usermessageruntime.LegacyUpgradeStageApplied {
		return ledger, errLegacyUpgradeIntegrationCrash
	}
	return ledger, err
}

func newLegacyUpgradeIntegrationService(t *testing.T, repository usermessageruntime.LegacyUpgradeRepository,
	opener usermessageruntime.LegacyUpgradeContentOpener, snapshots usermessageruntime.LegacyUpgradeSnapshotLoader,
) *usermessageruntime.LegacyUpgradeService {
	t.Helper()
	service, err := usermessageruntime.NewLegacyUpgradeService(repository, opener, snapshots, idgen.UUIDv7{}, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func createLegacyUpgradeIntegrationSession(t *testing.T, ctx context.Context, service *session.Service, prompt string) string {
	t.Helper()
	ids := idgen.UUIDv7{}
	projectID := mustUserMessageRuntimeIntegrationID(t, ids)
	userID := mustUserMessageRuntimeIntegrationID(t, ids)
	requestDigest, promptDigest, present, err := session.CalculateRequestDigest(projectID, userID, prompt, session.SkillSnapshotKindEmpty)
	if err != nil || !present {
		t.Fatal(err)
	}
	result, err := service.EnsureProjectSession(ctx, session.EnsureCommand{SchemaVersion: session.EnsureCommandSchemaVersionV1,
		RequestID: mustUserMessageRuntimeIntegrationID(t, ids), CommandID: mustUserMessageRuntimeIntegrationID(t, ids),
		RequestDigest: requestDigest, ProjectID: projectID, OwnerUserID: userID, CreationSource: session.CreationSourceQuickCreate,
		InitialPrompt: prompt, PromptDigest: promptDigest, SkillSnapshotMode: session.SkillSnapshotKindEmpty, RequestedAt: time.Now().UTC()})
	if err != nil || result.InputID == nil {
		t.Fatalf("创建 legacy Session 失败: result=%+v err=%v", result, err)
	}
	return *result.InputID
}

func assertLegacyUpgradeIntegrationStage(t *testing.T, client *Client, inputID, want string) {
	t.Helper()
	var got string
	if err := client.db.Raw(`SELECT stage FROM agent.session_user_message_upgrade_ledger WHERE input_id=?`, inputID).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Ledger stage=%s want=%s", got, want)
	}
}

type legacyUpgradeSideEffects struct{ Runs, Models, Outputs, Events int64 }

func legacyUpgradeIntegrationSideEffects(t *testing.T, client *Client, inputID string) legacyUpgradeSideEffects {
	t.Helper()
	var value legacyUpgradeSideEffects
	if err := client.db.Raw(`SELECT
		(SELECT count(*) FROM agent.session_user_message_run WHERE input_id=?) AS runs,
		(SELECT count(*) FROM agent.session_user_message_model_receipt WHERE input_id=?) AS models,
		(SELECT count(*) FROM agent.session_user_message_output_receipt WHERE input_id=?) AS outputs,
		(SELECT count(*) FROM agent.session_event_log e JOIN agent.session_input i ON i.session_id=e.session_id WHERE i.id=?) AS events`,
		inputID, inputID, inputID, inputID).Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	return value
}

// cleanupLegacyUpgradeIntegrationFacts 只删除本测试创建的 Session facts。DDL 与删除位于同一事务：
// 任一步失败都会回滚 trigger 状态和全部删除，避免把共享测试库留在 Guard 关闭状态。
func cleanupLegacyUpgradeIntegrationFacts(t *testing.T, client *Client, inputIDs []string) {
	t.Helper()
	if len(inputIDs) == 0 {
		return
	}
	tx := client.db.Begin()
	if tx.Error != nil {
		t.Errorf("开启 Legacy Helper cleanup 事务失败: %v", tx.Error)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()

	var sessionIDs []string
	if err := tx.Raw(`SELECT DISTINCT session_id::text FROM agent.session_input WHERE id IN ?`, inputIDs).Scan(&sessionIDs).Error; err != nil {
		t.Errorf("读取 Legacy Helper cleanup Session 失败: %v", err)
		return
	}
	if len(sessionIDs) == 0 {
		return
	}
	guardTables := []struct{ table, trigger string }{
		{"agent.session_user_message_upgrade_ledger", "trg_session_user_message_upgrade_ledger__guard"},
		{"agent.session_user_message_turn_context", "trg_session_user_message_turn_context__immutable"},
		{"agent.session_user_message_model_receipt", "trg_session_user_message_model_receipt__guard"},
		{"agent.session_user_message_output_receipt", "trg_session_user_message_output_receipt__guard"},
		{"agent.session_message", "trg_session_message__immutable"},
		{"agent.session_command_receipt", "trg_session_command_receipt__immutable"},
		{"agent.session_skill_snapshot_item", "trg_session_skill_snapshot_item__immutable"},
		{"agent.session_skill_snapshot", "trg_session_skill_snapshot__immutable"},
	}
	for _, guard := range guardTables {
		if err := tx.Exec("ALTER TABLE " + guard.table + " DISABLE TRIGGER " + guard.trigger).Error; err != nil {
			t.Errorf("关闭 Legacy Helper cleanup trigger %s 失败: %v", guard.trigger, err)
			return
		}
	}
	deletes := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM agent.session_user_message_output_projection WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_user_message_model_receipt WHERE input_id IN ?`, []any{inputIDs}},
		{`DELETE FROM agent.session_user_message_output_receipt WHERE input_id IN ?`, []any{inputIDs}},
		{`DELETE FROM agent.session_user_message_run WHERE input_id IN ?`, []any{inputIDs}},
		{`DELETE FROM agent.session_user_message_turn_context WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_user_message_turn WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_user_message_upgrade_ledger WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_event_log WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_event_counter WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_input WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_message WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_skill_snapshot_item WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_skill_snapshot WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_sequence_counter WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_runtime_lease WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session_command_receipt WHERE session_id IN ?`, []any{sessionIDs}},
		{`DELETE FROM agent.session WHERE id IN ?`, []any{sessionIDs}},
	}
	for _, deletion := range deletes {
		if err := tx.Exec(deletion.query, deletion.args...).Error; err != nil {
			t.Errorf("执行 Legacy Helper cleanup 失败 (%s): %v", deletion.query, err)
			return
		}
	}
	for index := len(guardTables) - 1; index >= 0; index-- {
		guard := guardTables[index]
		if err := tx.Exec("ALTER TABLE " + guard.table + " ENABLE TRIGGER " + guard.trigger).Error; err != nil {
			t.Errorf("恢复 Legacy Helper cleanup trigger %s 失败: %v", guard.trigger, err)
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Errorf("提交 Legacy Helper cleanup 失败: %v", err)
		return
	}
	committed = true
}
