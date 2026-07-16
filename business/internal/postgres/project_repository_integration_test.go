package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const businessIntegrationPostgreSQLDSNEnv = "DORA_BUSINESS_TEST_POSTGRES_DSN"

const businessIntegrationAllowDestructiveEnv = "DORA_BUSINESS_TEST_ALLOW_DESTRUCTIVE"

// quickCreateFactCounts 是 PostgreSQL 集成断言使用的固定查询 DTO，查询次数不随并发数增长。
type quickCreateFactCounts struct {
	// Projects 已提交的 Project 数量。
	Projects int64 `gorm:"column:projects"`
	// Receipts 已提交的创建回执数量。
	Receipts int64 `gorm:"column:receipts"`
	// Bindings 已提交的 Session 绑定数量。
	Bindings int64 `gorm:"column:bindings"`
	// Outboxes 已提交的 Session 初始化 Outbox 数量。
	Outboxes int64 `gorm:"column:outboxes"`
}

// openBusinessIntegrationRepository 使用显式测试 DSN 创建真实 PostgreSQL Repository 并在独立 business Schema 执行全部 Up Migration。
func openBusinessIntegrationRepository(t *testing.T) (*ProjectRepository, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv(businessIntegrationPostgreSQLDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run PostgreSQL integration tests", businessIntegrationPostgreSQLDSNEnv)
	}
	if os.Getenv(businessIntegrationAllowDestructiveEnv) != "1" {
		t.Fatalf("set %s=1 to confirm destructive integration schema reset", businessIntegrationAllowDestructiveEnv)
	}
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction:                   true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open integration PostgreSQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration PostgreSQL pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(32)
	sqlDB.SetMaxIdleConns(32)
	t.Cleanup(func() { _ = sqlDB.Close() })

	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read integration PostgreSQL database name: %v", err)
	}
	if !strings.HasSuffix(databaseName, "_test") {
		t.Fatalf("refuse destructive schema reset in database %q: name must end with _test", databaseName)
	}

	// 显式测试 DSN、破坏性确认开关和 _test 数据库名三重门禁通过后，才允许重建独占 business Schema。
	if err := db.Exec("DROP SCHEMA IF EXISTS business CASCADE").Error; err != nil {
		t.Fatalf("drop old integration business schema: %v", err)
	}
	applyBusinessUpMigrations(t, db)
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS business CASCADE").Error })

	client := &Client{db: db}
	if err := client.VerifySchema(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("verify migrated business schema contract: %v", err)
	}
	repository, err := NewProjectRepository(client)
	if err != nil {
		t.Fatalf("create integration project repository: %v", err)
	}
	return repository, db
}

// applyBusinessUpMigrations 按文件名顺序执行 Business Up Migration，确保测试覆盖真实 PostgreSQL 16 DDL 而非 AutoMigrate。
func applyBusinessUpMigrations(t *testing.T, db *gorm.DB) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test source path")
	}
	migrationFiles, err := filepath.Glob(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob business migrations: %v", err)
	}
	if len(migrationFiles) == 0 {
		t.Fatal("no business up migrations found")
	}
	sort.Strings(migrationFiles)
	for _, migrationFile := range migrationFiles {
		sqlBytes, err := os.ReadFile(migrationFile)
		if err != nil {
			t.Fatalf("read migration %s: %v", filepath.Base(migrationFile), err)
		}
		if err := db.Exec(string(sqlBytes)).Error; err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(migrationFile), err)
		}
	}
}

// readQuickCreateFactCounts 使用一条固定 SQL 查询四类权威事实数量，避免断言本身引入 N+1。
func readQuickCreateFactCounts(t *testing.T, db *gorm.DB) quickCreateFactCounts {
	t.Helper()
	var counts quickCreateFactCounts
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM business.project) AS projects,
			(SELECT COUNT(*) FROM business.project_creation_receipt) AS receipts,
			(SELECT COUNT(*) FROM business.project_session_binding) AS bindings,
			(SELECT COUNT(*) FROM business.project_session_outbox) AS outboxes`).Scan(&counts).Error; err != nil {
		t.Fatalf("count quick create facts: %v", err)
	}
	return counts
}

func TestProjectRepositoryPostgreSQLW0Semantics(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	aggregate := newRepositoryTestAggregate(t)

	const concurrency = 100
	results := make(chan project.QuickCreateResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, err := repository.CreateQuick(context.Background(), aggregate)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent same-digest quick create failed: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	createdCount := 0
	replayCount := 0
	for result := range results {
		if result.ProjectID != aggregate.Project.ID {
			t.Fatalf("concurrent result changed project ID: %s", result.ProjectID)
		}
		if result.IdempotentReplay {
			replayCount++
		} else {
			createdCount++
		}
	}
	if createdCount != 1 || replayCount != concurrency-1 {
		t.Fatalf("unexpected concurrent dispositions: created=%d replayed=%d", createdCount, replayCount)
	}
	if counts := readQuickCreateFactCounts(t, db); counts != (quickCreateFactCounts{Projects: 1, Receipts: 1, Bindings: 1, Outboxes: 1}) {
		t.Fatalf("same-key concurrency created duplicate facts: %+v", counts)
	}

	// 同一幂等键绑定了不同 HTTP 语义时，必须保留原 Project 四类事实并返回稳定冲突。
	conflicting := newRepositoryTestConflictingAggregate(t, aggregate)
	if _, err := repository.CreateQuick(context.Background(), conflicting); !errors.Is(err, project.ErrIdempotencyConflict) {
		t.Fatalf("expected PostgreSQL idempotency conflict, got %v", err)
	}
	if counts := readQuickCreateFactCounts(t, db); counts != (quickCreateFactCounts{Projects: 1, Receipts: 1, Bindings: 1, Outboxes: 1}) {
		t.Fatalf("conflicting digest changed original facts: %+v", counts)
	}

	// Prompt 密文只能在 Agent Receipt 已确认后清除；pending 状态提前清理必须由 PostgreSQL CHECK 失败关闭。
	if err := db.Exec(`
		UPDATE business.project_session_outbox
		SET payload_encryption_algorithm = NULL,
			payload_key_version = NULL,
			payload_nonce = NULL,
			payload_ciphertext = NULL,
			payload_cleared_at = statement_timestamp(),
			updated_at = statement_timestamp()
		WHERE id = ?`, aggregate.Outbox.ID).Error; err == nil {
		t.Fatal("expected prompt clear before delivered to violate PostgreSQL constraint")
	}
	if err := db.Exec(`
		UPDATE business.project_session_outbox
		SET status = 'delivered',
			delivered_at = statement_timestamp(),
			payload_encryption_algorithm = NULL,
			payload_key_version = NULL,
			payload_nonce = NULL,
			payload_ciphertext = NULL,
			payload_cleared_at = statement_timestamp(),
			updated_at = statement_timestamp()
		WHERE id = ?`, aggregate.Outbox.ID).Error; err != nil {
		t.Fatalf("clear delivered prompt ciphertext: %v", err)
	}
	var clearedOutbox projectSessionOutboxModel
	if err := db.Where("id = ?", aggregate.Outbox.ID).Take(&clearedOutbox).Error; err != nil {
		t.Fatalf("read cleared delivered outbox: %v", err)
	}
	if clearedOutbox.Status != string(project.OutboxStatusDelivered) || clearedOutbox.DeliveredAt == nil || clearedOutbox.PayloadClearedAt == nil || clearedOutbox.PayloadClearedAt.Before(*clearedOutbox.DeliveredAt) {
		t.Fatalf("unexpected delivered prompt clear timestamps: %+v", clearedOutbox)
	}
	if clearedOutbox.PayloadEncryptionAlgorithm != nil || clearedOutbox.PayloadKeyVersion != nil || len(clearedOutbox.PayloadNonce) != 0 || len(clearedOutbox.PayloadCiphertext) != 0 {
		t.Fatalf("delivered outbox retained prompt decryption material: %+v", clearedOutbox)
	}
	if !bytes.Equal(clearedOutbox.PayloadDigest, aggregate.Outbox.EncryptedPayload.PayloadDigest[:]) {
		t.Fatal("delivered prompt clear did not retain integrity digest")
	}

	// Repository 的真实 Claim/Delivery 路径必须在一个事务内更新 Binding、Project 并清除密文，Bootstrap 随即返回 ready。
	dispatchAggregate := newRepositoryTestAggregate(t)
	dispatchAggregate.Receipt.KeyDigest = project.SHA256Digest([]byte("dispatch-key"))
	if _, err := repository.CreateQuick(context.Background(), dispatchAggregate); err != nil {
		t.Fatalf("create dispatch aggregate: %v", err)
	}
	claimAt := dispatchAggregate.Outbox.CreatedAt.Add(time.Minute)
	claimed, err := repository.ClaimNext(context.Background(), "business-dispatcher-1", claimAt, claimAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim project session outbox: %v", err)
	}
	if claimed.ID != dispatchAggregate.Outbox.ID || claimed.Status != project.OutboxStatusProcessing || claimed.AttemptCount != 1 || claimed.LeaseVersion != 1 || claimed.RecoveryRequired {
		t.Fatalf("unexpected claimed outbox: %+v", claimed)
	}
	sessionID := newRepositoryTestUUIDv7(t)
	inputID := newRepositoryTestUUIDv7(t)
	deliveredAt := claimAt.Add(time.Second)
	if err := repository.MarkDelivered(context.Background(), claimed, project.EnsureSessionReceipt{
		CommandID: claimed.ID, RequestDigest: claimed.RequestDigest, SessionID: sessionID, InputID: &inputID,
	}, deliveredAt); err != nil {
		t.Fatalf("mark project session delivered: %v", err)
	}
	bootstrap, err := repository.FindBootstrapOwnedByID(context.Background(), dispatchAggregate.Project.ID, dispatchAggregate.Project.OwnerUserID)
	if err != nil {
		t.Fatalf("read delivered project bootstrap: %v", err)
	}
	if bootstrap.CreationStatus() != "ready" || bootstrap.AgentSessionID == nil || *bootstrap.AgentSessionID != sessionID ||
		bootstrap.AgentInputID == nil || *bootstrap.AgentInputID != inputID || bootstrap.InitialPromptStatus != project.InitialPromptStatusAccepted {
		t.Fatalf("delivery did not project ready bootstrap: %+v", bootstrap)
	}
	access, err := repository.FindReadyAgentSessionAccess(context.Background(), dispatchAggregate.Project.OwnerUserID, sessionID)
	if err != nil || access.ProjectID != dispatchAggregate.Project.ID || access.AgentSessionID != sessionID {
		t.Fatalf("ready binding did not authorize owner: access=%+v err=%v", access, err)
	}
	if _, err := repository.FindReadyAgentSessionAccess(context.Background(), newRepositoryTestUUIDv7(t), sessionID); !errors.Is(err, project.ErrAgentSessionNotFound) {
		t.Fatalf("cross-owner session lookup was not hidden: %v", err)
	}
	if err := db.Exec("UPDATE business.project SET lifecycle_status = 'trash', version = version + 1 WHERE id = ?", dispatchAggregate.Project.ID).Error; err != nil {
		t.Fatalf("move access test project to trash: %v", err)
	}
	if _, err := repository.FindReadyAgentSessionAccess(context.Background(), dispatchAggregate.Project.OwnerUserID, sessionID); !errors.Is(err, project.ErrAgentSessionNotFound) {
		t.Fatalf("trash project remained readable through BFF access: %v", err)
	}
	var deliveredOutbox projectSessionOutboxModel
	if err := db.Where("id = ?", dispatchAggregate.Outbox.ID).Take(&deliveredOutbox).Error; err != nil {
		t.Fatalf("read repository-delivered outbox: %v", err)
	}
	if deliveredOutbox.PayloadClearedAt == nil || len(deliveredOutbox.PayloadCiphertext) != 0 || deliveredOutbox.PayloadKeyVersion != nil {
		t.Fatalf("repository delivery retained prompt decryption material: %+v", deliveredOutbox)
	}

	// Unknown Outcome 先进入 reconciling；再次 Claim 后的永久错误必须按 Fence 收敛 blocked/failed。
	retryAggregate := newRepositoryTestAggregate(t)
	retryAggregate.Receipt.KeyDigest = project.SHA256Digest([]byte("retry-key"))
	if _, err := repository.CreateQuick(context.Background(), retryAggregate); err != nil {
		t.Fatalf("create retry aggregate: %v", err)
	}
	retryClaimAt := retryAggregate.Outbox.CreatedAt.Add(2 * time.Minute)
	retryClaimed, err := repository.ClaimNext(context.Background(), "business-dispatcher-1", retryClaimAt, retryClaimAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim retry outbox: %v", err)
	}
	retryAvailableAt := retryClaimAt.Add(5 * time.Second)
	if err := repository.MarkRetry(context.Background(), retryClaimed, retryAvailableAt, retryClaimAt.Add(time.Second)); err != nil {
		t.Fatalf("mark outbox retry: %v", err)
	}
	reconciling, err := repository.FindBootstrapOwnedByID(context.Background(), retryAggregate.Project.ID, retryAggregate.Project.OwnerUserID)
	if err != nil || reconciling.ProvisioningStatus != project.ProvisioningStatusReconciling {
		t.Fatalf("retry did not project reconciling: result=%+v err=%v", reconciling, err)
	}
	reclaimed, err := repository.ClaimNext(context.Background(), "business-dispatcher-2", retryAvailableAt, retryAvailableAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("reclaim retry outbox: %v", err)
	}
	if reclaimed.AttemptCount != 2 || reclaimed.LeaseVersion != 2 || !reclaimed.RecoveryRequired {
		t.Fatalf("retry did not advance attempt/fence: %+v", reclaimed)
	}
	if err := repository.MarkDead(context.Background(), reclaimed, "AGENT_SESSION_COMMAND_INVALID", retryAvailableAt.Add(time.Second)); err != nil {
		t.Fatalf("mark outbox dead: %v", err)
	}
	blocked, err := repository.FindBootstrapOwnedByID(context.Background(), retryAggregate.Project.ID, retryAggregate.Project.OwnerUserID)
	if err != nil || blocked.CreationStatus() != "failed" || blocked.InitialPromptStatus != project.InitialPromptStatusFailed || blocked.LastErrorCode == nil || *blocked.LastErrorCode != "AGENT_SESSION_COMMAND_INVALID" {
		t.Fatalf("dead transition did not project failed bootstrap: result=%+v err=%v", blocked, err)
	}

	// 测试触发器只用于注入 Outbox 最后一步失败，验证显式事务会回滚此前写入的 Receipt、Project 和 Binding。
	if err := db.Exec(`
		CREATE FUNCTION business.reject_project_session_outbox_for_test() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected outbox failure';
		END;
		$$`).Error; err != nil {
		t.Fatalf("create rollback test function: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_project_session_outbox_for_test
		BEFORE INSERT ON business.project_session_outbox
		FOR EACH ROW EXECUTE FUNCTION business.reject_project_session_outbox_for_test()`).Error; err != nil {
		t.Fatalf("create rollback test trigger: %v", err)
	}
	failedAggregate := newRepositoryTestAggregate(t)
	if _, err := repository.CreateQuick(context.Background(), failedAggregate); err == nil {
		t.Fatal("expected injected outbox failure")
	}
	if err := db.Exec("DROP TRIGGER reject_project_session_outbox_for_test ON business.project_session_outbox").Error; err != nil {
		t.Fatalf("drop rollback test trigger: %v", err)
	}
	if err := db.Exec("DROP FUNCTION business.reject_project_session_outbox_for_test()").Error; err != nil {
		t.Fatalf("drop rollback test function: %v", err)
	}

	var failedCounts quickCreateFactCounts
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM business.project WHERE id = ?) AS projects,
			(SELECT COUNT(*) FROM business.project_creation_receipt WHERE id = ?) AS receipts,
			(SELECT COUNT(*) FROM business.project_session_binding WHERE id = ?) AS bindings,
			(SELECT COUNT(*) FROM business.project_session_outbox WHERE id = ?) AS outboxes`,
		failedAggregate.Project.ID, failedAggregate.Receipt.ID, failedAggregate.Binding.ID, failedAggregate.Outbox.ID,
	).Scan(&failedCounts).Error; err != nil {
		t.Fatalf("count injected failure facts: %v", err)
	}
	if failedCounts != (quickCreateFactCounts{}) {
		t.Fatalf("failed local transaction left partial facts: %+v", failedCounts)
	}
}
