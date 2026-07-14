package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// projectSkillBindingFactCounts 使用固定数量子查询验证同键并发只产生一个完整 v2 事实集合。
type projectSkillBindingFactCounts struct {
	// Projects 是新建 Project 数量。
	Projects int64 `gorm:"column:projects"`
	// CreationReceipts 是 QuickCreate 回执数量。
	CreationReceipts int64 `gorm:"column:creation_receipts"`
	// BindingSets 是绑定集合 Header 数量。
	BindingSets int64 `gorm:"column:binding_sets"`
	// Bindings 是当前 Binding 行数量。
	Bindings int64 `gorm:"column:bindings"`
	// BindingAudits 是追加式初始审计数量。
	BindingAudits int64 `gorm:"column:binding_audits"`
	// Resolutions 是不可变解析 Header 数量。
	Resolutions int64 `gorm:"column:resolutions"`
	// ResolutionItems 是不可变解析 Item 数量。
	ResolutionItems int64 `gorm:"column:resolution_items"`
	// SessionBindings 是默认 Agent Session Binding 数量。
	SessionBindings int64 `gorm:"column:session_bindings"`
	// Outboxes 是完整加密 v2 Outbox 数量。
	Outboxes int64 `gorm:"column:outboxes"`
}

// TestProjectRepositoryPostgreSQLQuickCreateV2BatchSQL 使用 16 个 Published Skill 验证集合读取与两个批量写入各只执行一条 SQL。
func TestProjectRepositoryPostgreSQLQuickCreateV2BatchSQL(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	ownerUserID := newRepositoryTestUUIDv7(t)
	skillIDs := make([]string, 16)
	for index := range skillIDs {
		skillIDs[index] = seedPublishedProjectSkillForOwner(t, db, ownerUserID, index+1).Skill.ID
	}
	counter := &projectSkillBindingSQLCounter{}
	repository := &ProjectRepository{db: db.Session(&gorm.Session{Logger: counter})}
	command := newProjectSkillBindingV2Command(t, ownerUserID, skillIDs, "batch-sixteen-key")
	result, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{})
	if err != nil || result.SkillCount != 16 {
		t.Fatalf("create sixteen-skill v2: result=%+v err=%v", result, err)
	}
	if counter.collectionReads != 1 || counter.bindingBatchInserts != 1 || counter.resolutionItemBatchInserts != 1 {
		t.Fatalf("v2 SQL count grew with item count: %+v", counter)
	}
	var itemCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM business.project_session_skill_resolution_item WHERE resolution_id = ?`, command.ResolutionID).
		Scan(&itemCount).Error; err != nil || itemCount != 16 {
		t.Fatalf("sixteen-skill resolution count: count=%d err=%v", itemCount, err)
	}
}

// TestProjectRepositoryPostgreSQLQuickCreateV2 使用真实 PostgreSQL 16 验证 v2 并发幂等、owner-private、治理失败关闭和整体回滚。
func TestProjectRepositoryPostgreSQLQuickCreateV2(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	skillAggregate := seedPublishedProjectSkill(t, db)
	command := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, []string{skillAggregate.Skill.ID}, "quick-v2-key")
	protector := deterministicOutboxProtector{}

	const concurrency = 100
	results := make(chan projectskillbinding.QuickCreateV2Result, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), protector)
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
		t.Errorf("concurrent quick create v2 failed: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	created, replayed := 0, 0
	var frozenDigest projectskillbinding.Digest
	for result := range results {
		if result.IdempotentReplay {
			replayed++
		} else {
			created++
		}
		if result.ProjectID != command.ProjectID || result.SkillCount != 1 || result.ResolutionID != command.ResolutionID {
			t.Fatalf("concurrent result drifted: %+v", result)
		}
		if frozenDigest == (projectskillbinding.Digest{}) {
			frozenDigest = result.SnapshotDigest
		} else if result.SnapshotDigest != frozenDigest {
			t.Fatal("concurrent replay changed snapshot digest")
		}
	}
	if created != 1 || replayed != concurrency-1 {
		t.Fatalf("unexpected v2 dispositions: created=%d replayed=%d", created, replayed)
	}
	if counts := readProjectSkillBindingFactCounts(t, db); counts != (projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 1, BindingAudits: 1,
		Resolutions: 1, ResolutionItems: 1, SessionBindings: 1, Outboxes: 1,
	}) {
		t.Fatalf("same-key v2 created duplicate or partial facts: %+v", counts)
	}

	var outbox projectSessionOutboxV2Model
	if err := db.Where("id = ?", command.CommandID).Take(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.SchemaVersion != projectskillbinding.OutboxPayloadSchemaVersionV2 || outbox.SkillCount != 1 ||
		!bytes.Equal(outbox.SkillSnapshotDigest, frozenDigest[:]) || len(outbox.PayloadCiphertext) <= 16 ||
		bytes.Contains(outbox.PayloadCiphertext, []byte("Prompt helper")) || bytes.Contains(outbox.PayloadCiphertext, []byte("首条创作请求")) {
		t.Fatalf("unexpected encrypted v2 outbox metadata: %+v", outbox)
	}
	var publicRefsJSON string
	if err := db.Raw(`SELECT public_tool_refs::text FROM business.project_session_skill_resolution_item WHERE resolution_id = ?`, command.ResolutionID).
		Scan(&publicRefsJSON).Error; err != nil || publicRefsJSON != "[]" {
		t.Fatalf("resolution public refs must be empty: json=%q err=%v", publicRefsJSON, err)
	}

	// 同一幂等键改变 Binding Set 必须在读取 Published Skill 前稳定冲突，不新增任何候选事实。
	conflict := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, make([]string, 0), "quick-v2-key")
	if _, err := repository.CreateQuickV2(context.Background(), conflict, projectskillbinding.DefaultLimitsV1(), protector); !errors.Is(err, project.ErrIdempotencyConflict) {
		t.Fatalf("expected v2 idempotency conflict, got %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 1, BindingAudits: 1,
		Resolutions: 1, ResolutionItems: 1, SessionBindings: 1, Outboxes: 1,
	})

	// 跨 Owner 选择同一个 Published Skill 必须 existence-safe 失败，并回滚 Project、Binding、Receipt、Resolution 和 Outbox。
	crossOwner := newProjectSkillBindingV2Command(t, newRepositoryTestUUIDv7(t), []string{skillAggregate.Skill.ID}, "cross-owner-key")
	if _, err := repository.CreateQuickV2(context.Background(), crossOwner, projectskillbinding.DefaultLimitsV1(), protector); !errors.Is(err, projectskillbinding.ErrSkillUnavailable) {
		t.Fatalf("expected cross-owner unavailable, got %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 1, BindingAudits: 1,
		Resolutions: 1, ResolutionItems: 1, SessionBindings: 1, Outboxes: 1,
	})

	// suspended 必须阻断整个 Bootstrap；不允许静默丢 Item 后创建 empty Snapshot。
	if err := db.Model(&skillModel{}).Where("id = ?", skillAggregate.Skill.ID).
		Updates(map[string]any{"governance_status": "suspended", "governance_epoch": gorm.Expr("governance_epoch + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	governanceCommand := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, []string{skillAggregate.Skill.ID}, "governance-key")
	if _, err := repository.CreateQuickV2(context.Background(), governanceCommand, projectskillbinding.DefaultLimitsV1(), protector); !errors.Is(err, projectskillbinding.ErrGovernanceUnavailable) {
		t.Fatalf("expected governance unavailable, got %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 1, BindingAudits: 1,
		Resolutions: 1, ResolutionItems: 1, SessionBindings: 1, Outboxes: 1,
	})
	if err := db.Model(&skillModel{}).Where("id = ?", skillAggregate.Skill.ID).
		Updates(map[string]any{"governance_status": "active", "governance_epoch": gorm.Expr("governance_epoch + 1")}).Error; err != nil {
		t.Fatal(err)
	}

	// 本地保护器失败时同一事务所有候选事实必须回滚，不能保存明文或降级 v1。
	protectionCommand := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, []string{skillAggregate.Skill.ID}, "protection-key")
	if _, err := repository.CreateQuickV2(context.Background(), protectionCommand, projectskillbinding.DefaultLimitsV1(), failingOutboxProtector{}); !errors.Is(err, projectskillbinding.ErrContentProtection) {
		t.Fatalf("expected content protection failure, got %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 1, BindingAudits: 1,
		Resolutions: 1, ResolutionItems: 1, SessionBindings: 1, Outboxes: 1,
	})

	// 显式 v2 空集合仍创建 Binding Set/Resolution/v2 Outbox，但不创建 Binding/Item，绝不转换为 V1。
	emptyCommand := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, make([]string, 0), "empty-v2-key")
	emptyResult, err := repository.CreateQuickV2(context.Background(), emptyCommand, projectskillbinding.DefaultLimitsV1(), protector)
	if err != nil || emptyResult.SkillCount != 0 || emptyResult.SnapshotDigest.Hex() != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatalf("explicit empty v2 failed: result=%+v err=%v", emptyResult, err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 2, CreationReceipts: 2, BindingSets: 2, Bindings: 1, BindingAudits: 1,
		Resolutions: 2, ResolutionItems: 1, SessionBindings: 2, Outboxes: 2,
	})
}

// seedPublishedProjectSkill 创建逻辑完整的 owner-private Published Skill fixture，供 v2 集合 JOIN 解析。
func seedPublishedProjectSkill(t *testing.T, db *gorm.DB) skill.CreateAggregate {
	t.Helper()
	return seedPublishedProjectSkillForOwner(t, db, "", 1)
}

// seedPublishedProjectSkillForOwner 创建指定 Owner 的逻辑完整 Published Skill，使 1/10/16 集合测试不依赖跨 Owner 数据。
func seedPublishedProjectSkillForOwner(t *testing.T, db *gorm.DB, ownerUserID string, ordinal int) skill.CreateAggregate {
	t.Helper()
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	if ownerUserID == "" {
		ownerUserID = newSkillRepositoryUUIDv7(t)
	}
	skillID := newSkillRepositoryUUIDv7(t)
	draft := newSkillRepositoryRevision(t, skillID, ownerUserID, 1, fmt.Sprintf("批量 Skill %02d", ordinal))
	aggregate := skill.CreateAggregate{
		Skill: skill.Skill{
			ID: skillID, OwnerUserID: ownerUserID, CurrentDraftRevisionID: draft.ID,
			GovernanceStatus: skill.GovernanceStatusActive, Version: 1, CreatedAt: draft.CreatedAt, UpdatedAt: draft.CreatedAt,
		},
		Draft: draft,
		Receipt: skill.CommandReceipt{
			ID: newSkillRepositoryUUIDv7(t), ActorUserID: ownerUserID, CommandType: skill.CommandTypeCreate,
			ScopeID: ownerUserID, KeyDigest: sha256.Sum256([]byte("batch-create-key-" + skillID)),
			SemanticDigest: sha256.Sum256([]byte("batch-create-semantic-" + skillID)),
			ResultSkillID:  skillID, ResultContentRevisionID: stringValuePointer(draft.ID),
			ResponseDraftRevisionID: draft.ID, ResponseGovernanceStatus: skill.GovernanceStatusActive, CreatedAt: draft.CreatedAt,
		},
	}
	if _, replay, err := repository.Create(context.Background(), aggregate); err != nil || replay {
		t.Fatalf("create published skill fixture: replay=%v err=%v", replay, err)
	}
	reviewID := newSkillRepositoryUUIDv7(t)
	snapshotID := newSkillRepositoryUUIDv7(t)
	reviewerID := newSkillRepositoryUUIDv7(t)
	decidedAt := aggregate.Draft.CreatedAt.Add(time.Minute)
	approved := string(skill.ReviewStatusApproved)
	review := skillReviewSubmissionModel{
		ID: reviewID, SkillID: aggregate.Skill.ID, ContentRevisionID: aggregate.Draft.ID,
		ContentDigest: append([]byte(nil), aggregate.Draft.ContentDigest[:]...), Status: approved,
		Version: 2, SubmittedByUserID: aggregate.Skill.OwnerUserID, DecidedByUserID: &reviewerID,
		SubmittedAt: aggregate.Draft.CreatedAt.Add(30 * time.Second), DecidedAt: &decidedAt,
		UpdatedAt: decidedAt,
	}
	snapshot := skillPublishedSnapshotModel{
		ID: snapshotID, SkillID: aggregate.Skill.ID, SourceContentRevisionID: aggregate.Draft.ID,
		ReviewSubmissionID: reviewID, PublicationRevision: 1,
		DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
		DefinitionJSON:          jsonbValue(append([]byte(nil), aggregate.Draft.CanonicalJSON...)),
		ContentDigest:           append([]byte(nil), aggregate.Draft.ContentDigest[:]...),
		PublishedByUserID:       reviewerID, PublishedAt: decidedAt,
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&review).Error; err != nil {
			return err
		}
		if err := tx.Create(&snapshot).Error; err != nil {
			return err
		}
		updated := tx.Model(&skillModel{}).Where("id = ? AND current_published_snapshot_id IS NULL", aggregate.Skill.ID).
			Updates(map[string]any{
				"current_published_snapshot_id": snapshotID, "publication_revision": 1,
				"version": gorm.Expr("version + 1"), "updated_at": decidedAt,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("published skill fixture pointer update lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return aggregate
}

// projectSkillBindingSQLCounter 只统计本次 v2 事务中应保持常数的集合读取与批量 INSERT，不记录 SQL 参数或正文。
type projectSkillBindingSQLCounter struct {
	// mutex 保护 GORM Trace 回调并发更新。
	mutex sync.Mutex
	// collectionReads 是 Published Skill 集合 JOIN 次数。
	collectionReads int
	// bindingBatchInserts 是 Binding batch insert 次数。
	bindingBatchInserts int
	// resolutionItemBatchInserts 是 Resolution Item batch insert 次数。
	resolutionItemBatchInserts int
}

// LogMode 返回当前计数器；测试不需要按级别改变行为。
func (counter *projectSkillBindingSQLCounter) LogMode(logger.LogLevel) logger.Interface {
	return counter
}

// Info 丢弃普通日志，避免测试输出 SQL 或敏感参数。
func (counter *projectSkillBindingSQLCounter) Info(context.Context, string, ...any) {}

// Warn 丢弃警告日志，断言只使用返回错误和稳定计数。
func (counter *projectSkillBindingSQLCounter) Warn(context.Context, string, ...any) {}

// Error 丢弃错误日志，避免失败路径输出 SQL 参数。
func (counter *projectSkillBindingSQLCounter) Error(context.Context, string, ...any) {}

// Trace 根据不含参数的稳定表名片段统计 SQL；fc 仅在本地测试库执行且返回 SQL 文本不进入测试输出。
func (counter *projectSkillBindingSQLCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sqlText, _ := fc()
	counter.mutex.Lock()
	defer counter.mutex.Unlock()
	if strings.Contains(sqlText, "FROM business.project AS project") && strings.Contains(sqlText, "FOR SHARE OF skill") {
		counter.collectionReads++
	}
	if strings.Contains(sqlText, `INSERT INTO "business"."project_skill_binding"`) {
		counter.bindingBatchInserts++
	}
	if strings.Contains(sqlText, `INSERT INTO "business"."project_session_skill_resolution_item"`) {
		counter.resolutionItemBatchInserts++
	}
}

// newProjectSkillBindingV2Command 生成严格构造的 v2 命令，所有 UUIDv7 在事务外完成。
func newProjectSkillBindingV2Command(t *testing.T, ownerUserID string, skillIDs []string, key string) projectskillbinding.QuickCreateV2Command {
	t.Helper()
	bindings := make([]projectskillbinding.BindingSeed, len(skillIDs))
	for index, skillID := range skillIDs {
		bindings[index] = projectskillbinding.BindingSeed{
			ID: newRepositoryTestUUIDv7(t), SkillID: skillID, AuditID: newRepositoryTestUUIDv7(t),
		}
	}
	command, err := projectskillbinding.NewQuickCreateV2Command(projectskillbinding.QuickCreateV2Seed{
		SchemaVersion: projectskillbinding.QuickCreateSchemaVersionV2,
		ProjectID:     newRepositoryTestUUIDv7(t), ReceiptID: newRepositoryTestUUIDv7(t),
		SessionBindingID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t),
		ResolutionID: newRepositoryTestUUIDv7(t), OwnerUserID: ownerUserID,
		InitialPrompt: "首条创作请求", KeyDigest: projectskillbinding.SHA256Digest([]byte(key)),
		Bindings: bindings, MaxAttempts: 5, OccurredAt: time.Now().UTC(),
	}, projectskillbinding.DefaultLimitsV1())
	if err != nil {
		t.Fatal(err)
	}
	return command
}

// readProjectSkillBindingFactCounts 使用一条固定 SQL 读取全部 v2 事实数量，避免测试断言引入 N+1。
func readProjectSkillBindingFactCounts(t *testing.T, db *gorm.DB) projectSkillBindingFactCounts {
	t.Helper()
	var counts projectSkillBindingFactCounts
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM business.project) AS projects,
			(SELECT COUNT(*) FROM business.project_creation_receipt) AS creation_receipts,
			(SELECT COUNT(*) FROM business.project_skill_binding_set) AS binding_sets,
			(SELECT COUNT(*) FROM business.project_skill_binding) AS bindings,
			(SELECT COUNT(*) FROM business.project_skill_binding_audit) AS binding_audits,
			(SELECT COUNT(*) FROM business.project_session_skill_resolution) AS resolutions,
			(SELECT COUNT(*) FROM business.project_session_skill_resolution_item) AS resolution_items,
			(SELECT COUNT(*) FROM business.project_session_binding) AS session_bindings,
			(SELECT COUNT(*) FROM business.project_session_outbox) AS outboxes`).Scan(&counts).Error; err != nil {
		t.Fatal(err)
	}
	return counts
}

// assertProjectSkillBindingFactCounts 断言失败路径没有留下 Project、Binding、Resolution、Receipt 或 Outbox 部分事实。
func assertProjectSkillBindingFactCounts(t *testing.T, db *gorm.DB, expected projectSkillBindingFactCounts) {
	t.Helper()
	if actual := readProjectSkillBindingFactCounts(t, db); actual != expected {
		t.Fatalf("project skill binding facts changed unexpectedly: actual=%+v expected=%+v", actual, expected)
	}
}

// deterministicOutboxProtector 返回不含 plaintext 的固定测试 envelope；它不模拟生产随机源或密钥管理。
type deterministicOutboxProtector struct{}

// Protect 只用于验证 Repository envelope 持久化边界，完整 plaintext 与 AAD 仍由 canonical 单元测试核对。
func (deterministicOutboxProtector) Protect(_ context.Context, _ []byte, _ []byte) (projectskillbinding.EncryptedEnvelopeV2, error) {
	return projectskillbinding.EncryptedEnvelopeV2{
		Algorithm: projectskillbinding.OutboxEncryptionAlgorithm, KeyVersion: "integration-bootstrap-v2",
		Nonce: bytes.Repeat([]byte{0x21}, 12), CiphertextAndTag: bytes.Repeat([]byte{0x42}, 64),
	}, nil
}

// failingOutboxProtector 模拟用途隔离密钥不可用；Repository 必须整体回滚且不保存明文。
type failingOutboxProtector struct{}

// Protect 返回不含 Secret 的稳定测试错误，由领域边界收敛为 ErrContentProtection。
func (failingOutboxProtector) Protect(_ context.Context, _ []byte, _ []byte) (projectskillbinding.EncryptedEnvelopeV2, error) {
	return projectskillbinding.EncryptedEnvelopeV2{}, errors.New("test protector unavailable")
}
