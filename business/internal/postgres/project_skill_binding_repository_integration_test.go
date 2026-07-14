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
	"gorm.io/gorm/clause"
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

// TestProjectRepositoryPostgreSQLQuickCreateV2 使用真实 PostgreSQL 16 验证 v2 并发幂等、双权限版本、治理失败关闭和整体回滚。
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

	// 跨 Owner 的 active Published Skill 现在生成 public-market v2 权限，同时 Publisher 仍冻结为 Skill Owner。
	consumerUserID := newRepositoryTestUUIDv7(t)
	ensureProjectSkillBindingUserAccount(t, db, consumerUserID, "active")
	crossOwner := newProjectSkillBindingV2Command(t, consumerUserID, []string{skillAggregate.Skill.ID}, "cross-owner-key")
	crossOwnerResult, err := repository.CreateQuickV2(context.Background(), crossOwner, projectskillbinding.DefaultLimitsV1(), protector)
	if err != nil || crossOwnerResult.SkillCount != 1 {
		t.Fatalf("cross-owner public-market quick create failed: result=%+v err=%v", crossOwnerResult, err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 2, CreationReceipts: 2, BindingSets: 2, Bindings: 2, BindingAudits: 2,
		Resolutions: 2, ResolutionItems: 2, SessionBindings: 2, Outboxes: 2,
	})
	assertPersistedPermissionAudit(t, db, crossOwner.ResolutionID, projectskillbinding.PermissionSnapshotSchemaVersionV2,
		projectskillbinding.PermissionBasisPublicMarket, skillAggregate.Skill.OwnerUserID)

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
		Projects: 2, CreationReceipts: 2, BindingSets: 2, Bindings: 2, BindingAudits: 2,
		Resolutions: 2, ResolutionItems: 2, SessionBindings: 2, Outboxes: 2,
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
		Projects: 2, CreationReceipts: 2, BindingSets: 2, Bindings: 2, BindingAudits: 2,
		Resolutions: 2, ResolutionItems: 2, SessionBindings: 2, Outboxes: 2,
	})

	// 显式 v2 空集合仍创建 Binding Set/Resolution/v2 Outbox，但不创建 Binding/Item，绝不转换为 V1。
	emptyCommand := newProjectSkillBindingV2Command(t, skillAggregate.Skill.OwnerUserID, make([]string, 0), "empty-v2-key")
	emptyResult, err := repository.CreateQuickV2(context.Background(), emptyCommand, projectskillbinding.DefaultLimitsV1(), protector)
	if err != nil || emptyResult.SkillCount != 0 || emptyResult.SnapshotDigest.Hex() != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatalf("explicit empty v2 failed: result=%+v err=%v", emptyResult, err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 3, CreationReceipts: 3, BindingSets: 3, Bindings: 2, BindingAudits: 2,
		Resolutions: 3, ResolutionItems: 2, SessionBindings: 3, Outboxes: 3,
	})
}

// TestProjectRepositoryPostgreSQLPublicMarketMixedAtomicity 验证混合自有/市场集合、Publisher 身份和整体回滚。
func TestProjectRepositoryPostgreSQLPublicMarketMixedAtomicity(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	consumerUserID := newRepositoryTestUUIDv7(t)
	ensureProjectSkillBindingUserAccount(t, db, consumerUserID, "active")
	ownerSkill := seedPublishedProjectSkillForOwner(t, db, consumerUserID, 1)
	publicSkill := seedPublishedProjectSkillForOwner(t, db, newRepositoryTestUUIDv7(t), 2)

	// Publisher 账户状态不属于 W1-F 资格；逻辑关联存在时 disabled 账户仍允许其 active Published Skill 被使用。
	if err := db.Model(&userAccountModel{}).Where("id = ?", publicSkill.Skill.OwnerUserID).
		Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	command := newProjectSkillBindingV2Command(t, consumerUserID,
		[]string{publicSkill.Skill.ID, ownerSkill.Skill.ID}, "mixed-public-market-key")
	result, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{})
	if err != nil || result.SkillCount != 2 {
		t.Fatalf("mixed public-market create failed: result=%+v err=%v", result, err)
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{
		Projects: 1, CreationReceipts: 1, BindingSets: 1, Bindings: 2, BindingAudits: 2,
		Resolutions: 1, ResolutionItems: 2, SessionBindings: 1, Outboxes: 1,
	})
	assertPersistedPermissionAudit(t, db, command.ResolutionID, projectskillbinding.PermissionSnapshotSchemaVersionV1,
		projectskillbinding.PermissionBasisOwnerPrivate, consumerUserID)
	assertPersistedPermissionAudit(t, db, command.ResolutionID, projectskillbinding.PermissionSnapshotSchemaVersionV2,
		projectskillbinding.PermissionBasisPublicMarket, publicSkill.Skill.OwnerUserID)

	// mixed 集合任一公共 Skill 暂停时，九类候选事实必须全部回滚，不能只丢弃失败 Item。
	if err := db.Model(&skillModel{}).Where("id = ?", publicSkill.Skill.ID).
		Updates(map[string]any{"governance_status": "suspended", "governance_epoch": gorm.Expr("governance_epoch + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	before := readProjectSkillBindingFactCounts(t, db)
	failed := newProjectSkillBindingV2Command(t, consumerUserID,
		[]string{ownerSkill.Skill.ID, publicSkill.Skill.ID}, "mixed-public-market-suspended-key")
	if _, err := repository.CreateQuickV2(context.Background(), failed, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{}); !errors.Is(err, projectskillbinding.ErrGovernanceUnavailable) {
		t.Fatalf("mixed suspended item must fail closed: %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, before)

	// public-market 权限要求同一 SQL 能关联 Publisher Account；逻辑引用缺失时统一 unavailable 且零部分写入。
	orphanSkill := seedPublishedProjectSkillForOwner(t, db, newRepositoryTestUUIDv7(t), 3)
	if err := db.Where("id = ?", orphanSkill.Skill.OwnerUserID).Delete(&userAccountModel{}).Error; err != nil {
		t.Fatal(err)
	}
	orphanBefore := readProjectSkillBindingFactCounts(t, db)
	orphanCommand := newProjectSkillBindingV2Command(t, consumerUserID, []string{orphanSkill.Skill.ID}, "orphan-publisher-key")
	if _, err := repository.CreateQuickV2(context.Background(), orphanCommand, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{}); !errors.Is(err, projectskillbinding.ErrSkillUnavailable) {
		t.Fatalf("missing Publisher Account must fail unavailable: %v", err)
	}
	assertProjectSkillBindingFactCounts(t, db, orphanBefore)
}

// TestProjectRepositoryPostgreSQLPublicMarketGovernanceTOCTOU 使用真实行锁证明治理提交后的等待中 QuickCreate 失败且零部分写入。
func TestProjectRepositoryPostgreSQLPublicMarketGovernanceTOCTOU(t *testing.T) {
	repository, db := openBusinessIntegrationRepository(t)
	publicSkill := seedPublishedProjectSkill(t, db)
	consumerUserID := newRepositoryTestUUIDv7(t)
	ensureProjectSkillBindingUserAccount(t, db, consumerUserID, "active")
	command := newProjectSkillBindingV2Command(t, consumerUserID, []string{publicSkill.Skill.ID}, "public-market-toctou-key")

	lockTx := db.Begin()
	if lockTx.Error != nil {
		t.Fatal(lockTx.Error)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			_ = lockTx.Rollback().Error
		}
	})
	var lockedSkillID string
	if err := lockTx.Raw(`SELECT id FROM business.skill WHERE id = ? FOR UPDATE`, publicSkill.Skill.ID).
		Scan(&lockedSkillID).Error; err != nil || lockedSkillID != publicSkill.Skill.ID {
		t.Fatalf("lock public Skill: id=%s err=%v", lockedSkillID, err)
	}
	errChannel := make(chan error, 1)
	go func() {
		_, err := repository.CreateQuickV2(context.Background(), command, projectskillbinding.DefaultLimitsV1(), deterministicOutboxProtector{})
		errChannel <- err
	}()
	waitForProjectSkillBindingShareLock(t, db, errChannel)
	if err := lockTx.Model(&skillModel{}).Where("id = ?", publicSkill.Skill.ID).
		Updates(map[string]any{"governance_status": "suspended", "governance_epoch": gorm.Expr("governance_epoch + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := lockTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	committed = true
	select {
	case err := <-errChannel:
		if !errors.Is(err, projectskillbinding.ErrGovernanceUnavailable) {
			t.Fatalf("TOCTOU QuickCreate must observe committed suspension: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TOCTOU QuickCreate did not finish after governance commit")
	}
	assertProjectSkillBindingFactCounts(t, db, projectSkillBindingFactCounts{})
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
	ensureProjectSkillBindingUserAccount(t, db, ownerUserID, "active")
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

// ensureProjectSkillBindingUserAccount 幂等创建 Publisher/Consumer 逻辑账户 fixture；状态只用于证明查询不追加账户状态条件。
func ensureProjectSkillBindingUserAccount(t *testing.T, db *gorm.DB, userID string, status string) {
	t.Helper()
	now := time.Now().UTC()
	result := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&userAccountModel{
		ID: userID, DisplayName: "Project Skill Publisher", UserType: "personal", Status: status,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	if result.Error != nil {
		t.Fatalf("ensure Project Skill user account: %v", result.Error)
	}
}

// assertPersistedPermissionAudit 从数据库 Header/Item 恢复权限 Canonical，证明无需冗余 basis/policy 列也能逐字节审计。
func assertPersistedPermissionAudit(
	t *testing.T,
	db *gorm.DB,
	resolutionID string,
	expectedSchema string,
	expectedBasis string,
	expectedPublisherUserID string,
) {
	t.Helper()
	var headerModel projectSessionSkillResolutionModel
	if err := db.Where("id = ?", resolutionID).Take(&headerModel).Error; err != nil {
		t.Fatal(err)
	}
	var itemModel projectSessionSkillResolutionItemModel
	if err := db.Where("resolution_id = ? AND publisher_user_id = ?", resolutionID, expectedPublisherUserID).
		Take(&itemModel).Error; err != nil {
		t.Fatal(err)
	}
	selectionDigest, err := projectSkillBindingDigest(headerModel.BindingSelectionDigest)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDigest, err := projectSkillBindingDigest(headerModel.SnapshotSetDigest)
	if err != nil {
		t.Fatal(err)
	}
	permissionDigest, err := projectSkillBindingDigest(itemModel.PermissionSnapshotDigest)
	if err != nil {
		t.Fatal(err)
	}
	header := projectskillbinding.ResolutionHeader{
		ID: headerModel.ID, CommandID: headerModel.CommandID, ProjectID: headerModel.ProjectID,
		OwnerUserID: headerModel.OwnerUserID, BindingSetVersion: headerModel.BindingSetVersion,
		BindingSelectionDigest: selectionDigest, SnapshotSchemaVersion: headerModel.SnapshotSchemaVersion,
		SnapshotKind: headerModel.SnapshotKind, SkillCount: headerModel.SkillCount,
		SnapshotSetDigest: snapshotDigest, RuntimePolicyRef: headerModel.RuntimePolicyRef, ResolvedAt: headerModel.ResolvedAt,
	}
	item := projectskillbinding.ResolutionItem{
		ResolutionID: itemModel.ResolutionID, ProjectID: itemModel.ProjectID, CommandID: itemModel.CommandID,
		LoadOrder: itemModel.LoadOrder, Priority: itemModel.Priority, Namespace: itemModel.Namespace,
		BindingID: itemModel.BindingID, BindingVersion: itemModel.BindingVersion, SkillID: itemModel.SkillID,
		PublisherUserID: itemModel.PublisherUserID, PublishedSnapshotID: itemModel.PublishedSnapshotID,
		PermissionSnapshotDigest: permissionDigest,
	}
	audit, err := projectskillbinding.ReconstructResolutionPermissionAudit(header, item)
	if err != nil || audit.SchemaVersion != expectedSchema || audit.Basis != expectedBasis || len(audit.CanonicalJSON) == 0 {
		t.Fatalf("persisted permission audit mismatch: audit=%+v err=%v", audit, err)
	}
	var reviewerUserID string
	if err := db.Raw(`SELECT published_by_user_id FROM business.skill_published_snapshot WHERE id = ?`, item.PublishedSnapshotID).
		Scan(&reviewerUserID).Error; err != nil {
		t.Fatal(err)
	}
	if reviewerUserID == "" || reviewerUserID == item.PublisherUserID {
		t.Fatalf("Reviewer was confused with Publisher: reviewer=%s publisher=%s", reviewerUserID, item.PublisherUserID)
	}
}

// waitForProjectSkillBindingShareLock 等待 QuickCreate 集合查询真实阻塞在治理持有的 Skill 排他锁上。
func waitForProjectSkillBindingShareLock(t *testing.T, db *gorm.DB, errChannel <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errChannel:
			t.Fatalf("QuickCreate finished before reaching Skill SHARE lock: %v", err)
		default:
		}
		var waitingCount int64
		if err := db.Raw(`
			SELECT COUNT(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%project.owner_user_id AS project_owner_user_id%'`).Scan(&waitingCount).Error; err != nil {
			t.Fatal(err)
		}
		if waitingCount > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("QuickCreate did not reach Publisher/Skill SHARE lock")
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
