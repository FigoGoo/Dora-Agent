package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"gorm.io/gorm"
)

// promptPreviewFactCounts 是集成断言使用的固定集合查询 DTO。
type promptPreviewFactCounts struct {
	// Drafts 是已提交的 Prompt Preview Draft 数量。
	Drafts int64 `gorm:"column:drafts"`
	// Receipts 是已提交的 Prompt Preview 命令回执数量。
	Receipts int64 `gorm:"column:receipts"`
}

// promptPreviewSchemaContract 是实库 Schema 门禁使用的固定查询 DTO。
type promptPreviewSchemaContract struct {
	// ForeignKeys 是两张隔离表上的数据库物理外键数量，必须为零。
	ForeignKeys int64 `gorm:"column:foreign_keys"`
	// MissingTableComments 是缺失表中文说明的数量，必须为零。
	MissingTableComments int64 `gorm:"column:missing_table_comments"`
	// MissingColumnComments 是缺失字段中文说明的数量，必须为零。
	MissingColumnComments int64 `gorm:"column:missing_column_comments"`
}

// TestPromptPreviewRepositoryPostgreSQLSemantics 使用真实 PostgreSQL 验证 Owner、并发幂等、Source Guard、Query 和事务回滚。
func TestPromptPreviewRepositoryPostgreSQLSemantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	assertPromptPreviewSchemaContract(t, db)
	repository, err := NewPromptPreviewRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewPromptPreviewRepository() error = %v", err)
	}
	creationSpecRepository, err := NewCreationSpecRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewCreationSpecRepository() error = %v", err)
	}
	storyboardRepository, err := NewStoryboardPreviewRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewStoryboardPreviewRepository() error = %v", err)
	}

	userID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: userID, Title: "Prompt Preview 集成项目",
		LifecycleStatus: "active", RecentRunStatus: "idle", InitialPromptStatus: "absent",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Prompt Preview Project: %v", err)
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
	storyboardAggregate := newStoryboardPreviewRepositoryAggregate(t, userID, projectID, 1, storyboardpreview.CreationSpecRef{
		ID: creationSpecResult.Draft.ID, Version: creationSpecResult.Draft.Version, ContentDigest: creationSpecDigest,
	})
	storyboardResult, err := storyboardRepository.SaveDraft(context.Background(), storyboardAggregate)
	if err != nil {
		t.Fatalf("seed Storyboard Preview Draft: %v", err)
	}
	storyboardDigest, err := promptpreview.DigestFromBytes(storyboardResult.Draft.ContentDigest.Bytes())
	if err != nil {
		t.Fatalf("convert Storyboard digest: %v", err)
	}
	storyboardRef := promptpreview.StoryboardPreviewRef{
		ID: storyboardResult.Draft.ID, Version: storyboardResult.Draft.Version, ContentDigest: storyboardDigest.Hex(),
	}

	generationContext, err := repository.FindGenerationContext(context.Background(), promptpreview.ContextQuery{
		UserID: userID, ProjectID: projectID, StoryboardPreviewRef: storyboardRef,
	})
	if err != nil || generationContext.ProjectID != projectID || generationContext.ProjectVersion != 1 ||
		generationContext.Storyboard.ID != storyboardRef.ID || generationContext.Storyboard.ContentDigest.Hex() != storyboardRef.ContentDigest {
		t.Fatalf("FindGenerationContext() result=%+v error=%v", generationContext, err)
	}
	if _, err := repository.FindGenerationContext(context.Background(), promptpreview.ContextQuery{
		UserID: newRepositoryTestUUIDv7(t), ProjectID: projectID, StoryboardPreviewRef: storyboardRef,
	}); !errors.Is(err, promptpreview.ErrNotFound) {
		t.Fatalf("cross-owner Storyboard was not hidden: %v", err)
	}

	unknown, err := repository.QueryCommand(context.Background(), promptpreview.QueryCommand{
		CommandID: newRepositoryTestUUIDv7(t), RequestDigest: promptpreview.Digest{1},
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || unknown.Status != promptpreview.QueryStatusNotFound || unknown.Draft != nil {
		t.Fatalf("unknown command query result=%+v error=%v", unknown, err)
	}

	aggregate := newPromptPreviewRepositoryAggregate(t, userID, projectID, 1, storyboardRef, storyboardResult.Draft.Content)
	const concurrency = 20
	candidates := make([]promptpreview.SaveAggregate, concurrency)
	for index := range candidates {
		candidates[index] = aggregate
		if index == 0 {
			continue
		}
		// 并发请求可预分配不同本地 ID，但同 command_id 与 request_digest 必须收敛到首次事实。
		candidates[index].Draft.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.PromptPreviewID = candidates[index].Draft.ID
	}
	results := make(chan promptpreview.SaveResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for index := range concurrency {
		go func(candidate promptpreview.SaveAggregate) {
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
		case promptpreview.CommandDispositionCreated:
			created++
		case promptpreview.CommandDispositionReplayed:
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
	if counts := readPromptPreviewFactCounts(t, db); counts != (promptPreviewFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("same command created duplicate facts: %+v", counts)
	}

	query, err := repository.QueryCommand(context.Background(), promptpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != promptpreview.QueryStatusCompleted || query.Draft == nil || query.Draft.ID != canonicalDraftID {
		t.Fatalf("QueryCommand() result=%+v error=%v", query, err)
	}
	wrongDigest := aggregate.Receipt.RequestDigest
	wrongDigest[0] ^= 0xff
	query, err = repository.QueryCommand(context.Background(), promptpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: wrongDigest, UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != promptpreview.QueryStatusConflict || query.Draft != nil {
		t.Fatalf("wrong digest query result=%+v error=%v", query, err)
	}
	query, err = repository.QueryCommand(context.Background(), promptpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: newRepositoryTestUUIDv7(t), ProjectID: projectID,
	})
	if err != nil || query.Status != promptpreview.QueryStatusNotFound {
		t.Fatalf("cross-owner query result=%+v error=%v", query, err)
	}

	conflicting := newPromptPreviewRepositoryAggregate(t, userID, projectID, 1, storyboardRef, storyboardResult.Draft.Content)
	conflicting.Receipt.CommandID = aggregate.Receipt.CommandID
	conflicting.Draft.Content.Prompts[0].PositivePrompt = "同一命令的不同正文"
	resetPromptPreviewAggregateDigests(t, &conflicting)
	if _, err := repository.SaveDraft(context.Background(), conflicting); !errors.Is(err, promptpreview.ErrIdempotencyConflict) {
		t.Fatalf("same command different digest error=%v", err)
	}

	staleProject := newPromptPreviewRepositoryAggregate(t, userID, projectID, 2, storyboardRef, storyboardResult.Draft.Content)
	assertPromptPreviewSaveRollback(t, repository, staleProject, promptpreview.ErrProjectVersionConflict)

	staleStoryboard := newPromptPreviewRepositoryAggregate(t, userID, projectID, 1, storyboardRef, storyboardResult.Draft.Content)
	staleDigest, _ := promptpreview.ParseDigest(staleStoryboard.Draft.StoryboardPreviewRef.ContentDigest)
	staleDigest[0] ^= 0xff
	staleStoryboard.Draft.StoryboardPreviewRef.ContentDigest = staleDigest.Hex()
	staleStoryboard.Draft.Content.SourceStoryboardPreviewRef = staleStoryboard.Draft.StoryboardPreviewRef
	resetPromptPreviewAggregateDigests(t, &staleStoryboard)
	assertPromptPreviewSaveRollback(t, repository, staleStoryboard, promptpreview.ErrStoryboardVersionConflict)

	tamperedTarget := newPromptPreviewRepositoryAggregate(t, userID, projectID, 1, storyboardRef, storyboardResult.Draft.Content)
	tamperedTarget.Draft.Content.Prompts[0].Purpose = "并非 Source Slot 的用途"
	resetPromptPreviewAggregateDigests(t, &tamperedTarget)
	assertPromptPreviewSaveRollback(t, repository, tamperedTarget, promptpreview.ErrInvalidInput)

	if counts := readPromptPreviewFactCounts(t, db); counts != (promptPreviewFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("conflict/fence/rollback paths changed original facts: %+v", counts)
	}
}

// assertPromptPreviewSchemaContract 用一条 PostgreSQL Catalog 查询验证无物理外键且表、字段 COMMENT 已真实落库。
func assertPromptPreviewSchemaContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	var contract promptPreviewSchemaContract
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*)
			 FROM pg_constraint constraint_record
			 JOIN pg_class table_record ON table_record.oid = constraint_record.conrelid
			 JOIN pg_namespace schema_record ON schema_record.oid = table_record.relnamespace
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('prompt_preview_draft', 'prompt_preview_command_receipt')
			   AND constraint_record.contype = 'f') AS foreign_keys,
			(SELECT COUNT(*)
			 FROM pg_class table_record
			 JOIN pg_namespace schema_record ON schema_record.oid = table_record.relnamespace
			 LEFT JOIN pg_description description_record
			   ON description_record.objoid = table_record.oid AND description_record.objsubid = 0
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('prompt_preview_draft', 'prompt_preview_command_receipt')
			   AND COALESCE(description_record.description, '') = '') AS missing_table_comments,
			(SELECT COUNT(*)
			 FROM pg_attribute column_record
			 JOIN pg_class table_record ON table_record.oid = column_record.attrelid
			 JOIN pg_namespace schema_record ON schema_record.oid = table_record.relnamespace
			 LEFT JOIN pg_description description_record
			   ON description_record.objoid = table_record.oid AND description_record.objsubid = column_record.attnum
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('prompt_preview_draft', 'prompt_preview_command_receipt')
			   AND column_record.attnum > 0 AND NOT column_record.attisdropped
			   AND COALESCE(description_record.description, '') = '') AS missing_column_comments`).Scan(&contract).Error; err != nil {
		t.Fatalf("query Prompt Preview schema contract: %v", err)
	}
	if contract != (promptPreviewSchemaContract{}) {
		t.Fatalf("Prompt Preview schema contract violated: %+v", contract)
	}
}

// assertPromptPreviewSaveRollback 断言稳定错误不会留下可被 Unknown Outcome Query 观察到的孤立 Receipt。
func assertPromptPreviewSaveRollback(t *testing.T, repository *PromptPreviewRepository, aggregate promptpreview.SaveAggregate, expectedError error) {
	t.Helper()
	if _, err := repository.SaveDraft(context.Background(), aggregate); !errors.Is(err, expectedError) {
		t.Fatalf("SaveDraft() error=%v, want %v", err, expectedError)
	}
	query, err := repository.QueryCommand(context.Background(), promptpreview.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: aggregate.Draft.UserID, ProjectID: aggregate.Draft.ProjectID,
	})
	if err != nil || query.Status != promptpreview.QueryStatusNotFound || query.Draft != nil {
		t.Fatalf("rolled-back command remains observable: result=%+v error=%v", query, err)
	}
}

// readPromptPreviewFactCounts 使用一条固定 SQL 查询 Draft 与回执数量。
func readPromptPreviewFactCounts(t *testing.T, db *gorm.DB) promptPreviewFactCounts {
	t.Helper()
	var counts promptPreviewFactCounts
	if err := db.Raw(`SELECT
		(SELECT COUNT(*) FROM business.prompt_preview_draft) AS drafts,
		(SELECT COUNT(*) FROM business.prompt_preview_command_receipt) AS receipts`).Scan(&counts).Error; err != nil {
		t.Fatalf("count Prompt Preview facts: %v", err)
	}
	return counts
}

// newPromptPreviewRepositoryAggregate 构造覆盖 Source 全部 Slot 的 Repository 测试聚合。
func newPromptPreviewRepositoryAggregate(t *testing.T, userID string, projectID string, expectedProjectVersion int64, reference promptpreview.StoryboardPreviewRef, source storyboardpreview.Content) promptpreview.SaveAggregate {
	t.Helper()
	elementOrders := make(map[string]int32, len(source.Elements))
	for _, element := range source.Elements {
		elementOrders[element.Key] = element.Order
	}
	type sourceTarget struct {
		slot  storyboardpreview.Slot
		order int32
	}
	targets := make([]sourceTarget, len(source.Slots))
	for index, slot := range source.Slots {
		targets[index] = sourceTarget{slot: slot, order: elementOrders[slot.ElementKey]}
	}
	// 测试 Source 已按 Element/Slot 顺序生成；若后续 Fixture 改序，这里显式失败避免隐藏产品语义变化。
	for index := 1; index < len(targets); index++ {
		if targets[index].order < targets[index-1].order {
			t.Fatal("test Storyboard fixture is not ordered by element")
		}
	}
	prompts := make([]promptpreview.PromptEntry, len(targets))
	for index, target := range targets {
		mediaKind, ok := promptpreview.MediaKindForSlotType(string(target.slot.Type))
		if !ok {
			t.Fatalf("unknown test slot type %q", target.slot.Type)
		}
		prompts[index] = promptpreview.PromptEntry{
			TargetLocalKey: target.slot.Key, ElementLocalKey: target.slot.ElementKey,
			SlotType: string(target.slot.Type), MediaKind: mediaKind, Purpose: target.slot.Purpose,
			Required: target.slot.Required, PositivePrompt: "为 " + target.slot.Purpose + " 编写清晰提示词",
			NegativeConstraints: []string{}, OutputLanguage: "zh-CN",
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	aggregate := promptpreview.SaveAggregate{
		Draft: promptpreview.Draft{
			ID: newRepositoryTestUUIDv7(t), ProjectID: projectID, UserID: userID,
			StoryboardPreviewRef: reference, Status: promptpreview.DraftStatus,
			Version: promptpreview.InitialDraftVersion, SchemaVersion: promptpreview.DraftSchemaVersion,
			Content: promptpreview.Content{
				SchemaVersion: promptpreview.DraftSchemaVersion, Mode: promptpreview.DraftMode,
				SourceStoryboardPreviewRef: reference, Prompts: prompts,
			},
			ExactTargetSetDigest: promptpreview.Digest{7, 7, 7}, SourceToolCallID: newRepositoryTestUUIDv7(t),
			SourcePromptVersion:            "graph_tool.write_prompts.preview.v1",
			SourceValidatorVersion:         "write_prompts.preview.validator.v1",
			SourceExactSetValidatorVersion: "write_prompts.preview.exact-set-validator.v1",
			CreatedAt:                      now, UpdatedAt: now,
		},
		Receipt: promptpreview.CommandReceipt{
			ID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t),
			UserID: userID, ProjectID: projectID, ExpectedProjectVersion: expectedProjectVersion,
			StoryboardPreviewRef: reference, CreatedAt: now,
		},
	}
	aggregate.Receipt.SourceToolCallID = aggregate.Draft.SourceToolCallID
	aggregate.Receipt.SourcePromptVersion = aggregate.Draft.SourcePromptVersion
	aggregate.Receipt.SourceValidatorVersion = aggregate.Draft.SourceValidatorVersion
	aggregate.Receipt.SourceExactSetValidatorVersion = aggregate.Draft.SourceExactSetValidatorVersion
	aggregate.Receipt.ExactTargetSetDigest = aggregate.Draft.ExactTargetSetDigest
	aggregate.Receipt.PromptPreviewID = aggregate.Draft.ID
	aggregate.Receipt.ResultVersion = aggregate.Draft.Version
	aggregate.Receipt.ResultStatus = aggregate.Draft.Status
	resetPromptPreviewAggregateDigests(t, &aggregate)
	return aggregate
}

// resetPromptPreviewAggregateDigests 在变更测试语义后同步冻结 Content、Request 与 Result 摘要。
func resetPromptPreviewAggregateDigests(t *testing.T, aggregate *promptpreview.SaveAggregate) {
	t.Helper()
	contentDigest, err := promptpreview.ContentDigest(aggregate.Draft.Content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	requestDigest, err := promptpreview.SaveRequestDigest(
		aggregate.Draft.UserID, aggregate.Draft.ProjectID, aggregate.Receipt.ExpectedProjectVersion,
		aggregate.Draft.StoryboardPreviewRef, aggregate.Draft.SourceToolCallID,
		aggregate.Draft.SourcePromptVersion, aggregate.Draft.SourceValidatorVersion,
		aggregate.Draft.SourceExactSetValidatorVersion, aggregate.Draft.ExactTargetSetDigest, aggregate.Draft.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	aggregate.Draft.ContentDigest = contentDigest
	aggregate.Receipt.RequestDigest = requestDigest
	aggregate.Receipt.ResultContentDigest = contentDigest
	// 每次测试变更都以 Draft 的精确 Source 引用为事实，避免 Receipt 不一致先于目标 Guard 失败。
	aggregate.Receipt.StoryboardPreviewRef = aggregate.Draft.StoryboardPreviewRef
}
