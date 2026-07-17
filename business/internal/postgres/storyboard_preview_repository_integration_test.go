package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"gorm.io/gorm"
)

// storyboardPreviewFactCounts 是集成断言使用的固定集合查询 DTO。
type storyboardPreviewFactCounts struct {
	// Drafts 是已提交的 Storyboard Preview Draft 数量。
	Drafts int64 `gorm:"column:drafts"`
	// Receipts 是已提交的 Storyboard Preview 命令回执数量。
	Receipts int64 `gorm:"column:receipts"`
}

// TestStoryboardPreviewRepositoryPostgreSQLSemantics 使用真实 PostgreSQL 验证 Owner、并发幂等、Fence、Query 和事务回滚。
func TestStoryboardPreviewRepositoryPostgreSQLSemantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewStoryboardPreviewRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewStoryboardPreviewRepository() error = %v", err)
	}
	creationSpecRepository, err := NewCreationSpecRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewCreationSpecRepository() error = %v", err)
	}

	userID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: userID, Title: "Storyboard Preview 集成项目",
		LifecycleStatus: "active", RecentRunStatus: "idle", InitialPromptStatus: "absent",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Storyboard Preview Project: %v", err)
	}

	creationSpecAggregate := newCreationSpecRepositoryAggregate(t, userID, projectID, 1)
	creationSpecResult, err := creationSpecRepository.SaveDraft(context.Background(), creationSpecAggregate)
	if err != nil {
		t.Fatalf("seed CreationSpec Draft: %v", err)
	}
	creationSpecDigest, err := storyboardpreview.DigestFromBytes(creationSpecResult.Draft.ContentDigest.Bytes())
	if err != nil {
		t.Fatalf("convert CreationSpec digest: %v", err)
	}
	creationSpecRef := storyboardpreview.CreationSpecRef{
		ID: creationSpecResult.Draft.ID, Version: creationSpecResult.Draft.Version, ContentDigest: creationSpecDigest,
	}

	planningContext, err := repository.FindPlanningContext(context.Background(), storyboardpreview.ContextQuery{
		UserID: userID, ProjectID: projectID, CreationSpecRef: creationSpecRef,
	})
	if err != nil || planningContext.ProjectID != projectID || planningContext.ProjectVersion != 1 ||
		planningContext.CreationSpec.ID != creationSpecRef.ID || planningContext.CreationSpec.ContentDigest != creationSpecRef.ContentDigest {
		t.Fatalf("FindPlanningContext() result=%+v error=%v", planningContext, err)
	}
	if _, err := repository.FindPlanningContext(context.Background(), storyboardpreview.ContextQuery{
		UserID: newRepositoryTestUUIDv7(t), ProjectID: projectID, CreationSpecRef: creationSpecRef,
	}); !errors.Is(err, storyboardpreview.ErrNotFound) {
		t.Fatalf("cross-owner CreationSpec was not hidden: %v", err)
	}

	// 查询尚未执行的命令模拟 Save 前或明确 not_found 的 Unknown Outcome 收敛起点。
	unknownCommandID := newRepositoryTestUUIDv7(t)
	unknownDigest := storyboardpreview.Digest{1}
	unknown, err := repository.QueryCommand(context.Background(), storyboardpreview.QueryCommand{
		CommandID: unknownCommandID, RequestDigest: unknownDigest, UserID: userID, ProjectID: projectID,
	})
	if err != nil || unknown.Status != storyboardpreview.QueryStatusNotFound || unknown.Draft != nil {
		t.Fatalf("unknown command query result=%+v error=%v", unknown, err)
	}

	aggregate := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	const concurrency = 24
	candidates := make([]storyboardpreview.SaveAggregate, concurrency)
	for index := range candidates {
		candidates[index] = aggregate
		if index == 0 {
			continue
		}
		// 并发请求可各自预分配本地 Draft/Receipt ID，但稳定 command_id 与摘要必须收敛到首次事实。
		candidates[index].Draft.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.StoryboardPreviewID = candidates[index].Draft.ID
	}
	results := make(chan storyboardpreview.SaveResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for index := range concurrency {
		go func(candidate storyboardpreview.SaveAggregate) {
			defer waitGroup.Done()
			result, saveErr := repository.SaveDraft(context.Background(), candidate)
			if saveErr != nil {
				errorsChannel <- saveErr
				return
			}
			results <- result
		}(candidates[index])
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for saveErr := range errorsChannel {
		t.Errorf("concurrent SaveDraft() error = %v", saveErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	created, replayed := 0, 0
	canonicalDraftID := ""
	for result := range results {
		switch result.Disposition {
		case storyboardpreview.CommandDispositionCreated:
			created++
		case storyboardpreview.CommandDispositionReplayed:
			replayed++
		default:
			t.Fatalf("unexpected disposition %q", result.Disposition)
		}
		if canonicalDraftID == "" {
			canonicalDraftID = result.Draft.ID
		}
		if result.Draft.ID != canonicalDraftID {
			t.Fatalf("idempotent results did not converge: first=%s result=%+v", canonicalDraftID, result)
		}
	}
	if created != 1 || replayed != concurrency-1 {
		t.Fatalf("created=%d replayed=%d", created, replayed)
	}
	if counts := readStoryboardPreviewFactCounts(t, db); counts != (storyboardPreviewFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("same command created duplicate facts: %+v", counts)
	}

	// Save 响应未知时只查询原 command + digest，必须恢复首次 Draft 根 ID。
	query, err := repository.QueryCommand(context.Background(), storyboardpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != storyboardpreview.QueryStatusCompleted || query.Draft == nil || query.Draft.ID != canonicalDraftID {
		t.Fatalf("QueryCommand() result=%+v error=%v", query, err)
	}
	wrongQueryDigest := aggregate.Receipt.RequestDigest
	wrongQueryDigest[0] ^= 0xff
	query, err = repository.QueryCommand(context.Background(), storyboardpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: wrongQueryDigest,
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != storyboardpreview.QueryStatusConflict || query.Draft != nil {
		t.Fatalf("wrong digest query result=%+v error=%v", query, err)
	}
	query, err = repository.QueryCommand(context.Background(), storyboardpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: newRepositoryTestUUIDv7(t), ProjectID: projectID,
	})
	if err != nil || query.Status != storyboardpreview.QueryStatusNotFound {
		t.Fatalf("cross-owner query result=%+v error=%v", query, err)
	}

	// 同 command 的不同正文必须只返回幂等冲突，不得改写首次事实。
	conflicting := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	conflicting.Receipt.CommandID = aggregate.Receipt.CommandID
	conflicting.Draft.Content.Title = "同一命令的不同 Storyboard"
	resetStoryboardPreviewAggregateDigests(t, &conflicting)
	if _, err := repository.SaveDraft(context.Background(), conflicting); !errors.Is(err, storyboardpreview.ErrIdempotencyConflict) {
		t.Fatalf("same command different digest error=%v", err)
	}

	// Project 乐观版本必须在保存事务内重新校验，失败时先插入的 Receipt 必须随事务回滚。
	staleProject := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 2, creationSpecRef)
	assertStoryboardPreviewSaveRollback(t, repository, staleProject, storyboardpreview.ErrProjectVersionConflict)

	// CreationSpec 摘要必须在保存事务内与 Business 权威 Draft 重新比对，禁止使用过期快照。
	staleCreationSpec := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	staleCreationSpec.Draft.CreationSpecRef.ContentDigest[0] ^= 0xff
	staleCreationSpec.Receipt.CreationSpecRef = staleCreationSpec.Draft.CreationSpecRef
	resetStoryboardPreviewAggregateDigests(t, &staleCreationSpec)
	assertStoryboardPreviewSaveRollback(t, repository, staleCreationSpec, storyboardpreview.ErrCreationSpecVersionConflict)

	// CreationSpec ID、Owner 或 Draft 状态不满足 exact resource 时必须安全 not_found 并回滚 Receipt。
	missingCreationSpec := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	missingCreationSpec.Draft.CreationSpecRef.ID = newRepositoryTestUUIDv7(t)
	missingCreationSpec.Receipt.CreationSpecRef = missingCreationSpec.Draft.CreationSpecRef
	resetStoryboardPreviewAggregateDigests(t, &missingCreationSpec)
	assertStoryboardPreviewSaveRollback(t, repository, missingCreationSpec, storyboardpreview.ErrNotFound)

	// source_phase_key 必须在保存事务读取到的 CreationSpec Content 中存在。
	invalidPhase := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	invalidPhase.Draft.Content.Elements[0].SourcePhaseKey = "phase_2"
	resetStoryboardPreviewAggregateDigests(t, &invalidPhase)
	assertStoryboardPreviewSaveRollback(t, repository, invalidPhase, storyboardpreview.ErrInvalidInput)

	// Draft 唯一索引失败必须回滚同事务内已经写入的新 command receipt。
	duplicateSource := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, creationSpecRef)
	duplicateSource.Draft.SourceToolCallID = aggregate.Draft.SourceToolCallID
	duplicateSource.Receipt.SourceToolCallID = aggregate.Draft.SourceToolCallID
	resetStoryboardPreviewAggregateDigests(t, &duplicateSource)
	assertStoryboardPreviewSaveRollback(t, repository, duplicateSource, storyboardpreview.ErrPersistence)

	if counts := readStoryboardPreviewFactCounts(t, db); counts != (storyboardPreviewFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("conflict/fence/rollback paths changed original facts: %+v", counts)
	}
}

// assertStoryboardPreviewSaveRollback 断言稳定错误不会留下可被 Unknown Outcome Query 观察到的孤立 Receipt。
func assertStoryboardPreviewSaveRollback(t *testing.T, repository *StoryboardPreviewRepository, aggregate storyboardpreview.SaveAggregate, expectedError error) {
	t.Helper()
	if _, err := repository.SaveDraft(context.Background(), aggregate); !errors.Is(err, expectedError) {
		t.Fatalf("SaveDraft() error=%v, want %v", err, expectedError)
	}
	query, err := repository.QueryCommand(context.Background(), storyboardpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: aggregate.Draft.UserID, ProjectID: aggregate.Draft.ProjectID,
	})
	if err != nil || query.Status != storyboardpreview.QueryStatusNotFound || query.Draft != nil {
		t.Fatalf("rolled-back command remains observable: result=%+v error=%v", query, err)
	}
}

// readStoryboardPreviewFactCounts 使用一条固定 SQL 查询 Draft 与回执数量。
func readStoryboardPreviewFactCounts(t *testing.T, db *gorm.DB) storyboardPreviewFactCounts {
	t.Helper()
	var counts storyboardPreviewFactCounts
	if err := db.Raw(`SELECT
		(SELECT COUNT(*) FROM business.storyboard_preview_draft) AS drafts,
		(SELECT COUNT(*) FROM business.storyboard_preview_command_receipt) AS receipts`).Scan(&counts).Error; err != nil {
		t.Fatalf("count Storyboard Preview facts: %v", err)
	}
	return counts
}

// newStoryboardPreviewRepositoryAggregate 构造满足严格 local-key、引用和 DAG 约束的 Repository 测试聚合。
func newStoryboardPreviewRepositoryAggregate(t *testing.T, userID string, projectID string, expectedProjectVersion int64, reference storyboardpreview.CreationSpecRef) storyboardpreview.SaveAggregate {
	t.Helper()
	content := storyboardpreview.Content{
		Title: "夏日品牌短片故事板", Summary: "以产品亮相和使用场景串联叙事。",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立夏日氛围"}},
		Elements: []storyboardpreview.Element{
			{Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene, Title: "海边开场", NarrativePurpose: "建立轻快情绪", DurationSeconds: 10, SourcePhaseKey: "phase_1", DependencyKeys: []string{}},
			{Key: "element_2", SectionKey: "section_1", Order: 2, Type: storyboardpreview.ElementTypeShot, Title: "产品特写", NarrativePurpose: "突出核心卖点", DurationSeconds: 20, SourcePhaseKey: "phase_1", DependencyKeys: []string{"element_1"}},
		},
		Slots: []storyboardpreview.Slot{
			{Key: "slot_1", ElementKey: "element_1", Type: storyboardpreview.SlotTypeVideo, Purpose: "夏日海边环境画面", Required: true},
			{Key: "slot_2", ElementKey: "element_2", Type: storyboardpreview.SlotTypeImage, Purpose: "产品包装特写", Required: true},
		},
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	aggregate := storyboardpreview.SaveAggregate{
		Draft: storyboardpreview.Draft{
			ID: newRepositoryTestUUIDv7(t), ProjectID: projectID, UserID: userID,
			CreationSpecRef: reference, Status: storyboardpreview.DraftStatus,
			Version: storyboardpreview.InitialDraftVersion, SchemaVersion: storyboardpreview.DraftSchemaVersion,
			Content: content, SourceToolCallID: newRepositoryTestUUIDv7(t),
			SourcePromptVersion:    "graph_tool.plan_storyboard.preview.v1",
			SourceValidatorVersion: "plan_storyboard.preview.validator.v1", CreatedAt: now, UpdatedAt: now,
		},
		Receipt: storyboardpreview.CommandReceipt{
			ID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t),
			UserID: userID, ProjectID: projectID, ExpectedProjectVersion: expectedProjectVersion,
			CreationSpecRef: reference, CreatedAt: now,
		},
	}
	aggregate.Receipt.SourceToolCallID = aggregate.Draft.SourceToolCallID
	aggregate.Receipt.SourcePromptVersion = aggregate.Draft.SourcePromptVersion
	aggregate.Receipt.SourceValidatorVersion = aggregate.Draft.SourceValidatorVersion
	aggregate.Receipt.StoryboardPreviewID = aggregate.Draft.ID
	aggregate.Receipt.ResultVersion = aggregate.Draft.Version
	aggregate.Receipt.ResultStatus = aggregate.Draft.Status
	resetStoryboardPreviewAggregateDigests(t, &aggregate)
	return aggregate
}

// resetStoryboardPreviewAggregateDigests 在变更测试语义后同步冻结 Content、Request 与 Result 摘要。
func resetStoryboardPreviewAggregateDigests(t *testing.T, aggregate *storyboardpreview.SaveAggregate) {
	t.Helper()
	contentDigest, err := storyboardpreview.ContentDigest(aggregate.Draft.Content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	requestDigest, err := storyboardpreview.SaveRequestDigest(
		aggregate.Draft.UserID, aggregate.Draft.ProjectID, aggregate.Receipt.ExpectedProjectVersion,
		aggregate.Draft.CreationSpecRef, aggregate.Draft.SourceToolCallID,
		aggregate.Draft.SourcePromptVersion, aggregate.Draft.SourceValidatorVersion, aggregate.Draft.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	aggregate.Draft.ContentDigest = contentDigest
	aggregate.Receipt.RequestDigest = requestDigest
	aggregate.Receipt.ResultContentDigest = contentDigest
	// 每次测试变更都以 Draft 的精确依赖引用为事实，避免 Receipt 与 Draft 非业务性不一致先于目标 Guard 失败。
	aggregate.Receipt.CreationSpecRef = aggregate.Draft.CreationSpecRef
}
