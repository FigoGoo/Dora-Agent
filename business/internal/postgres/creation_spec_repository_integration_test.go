package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"gorm.io/gorm"
)

// creationSpecFactCounts 是集成断言使用的固定集合查询 DTO。
type creationSpecFactCounts struct {
	// Drafts 是 CreationSpec Draft 数量。
	Drafts int64 `gorm:"column:drafts"`
	// Receipts 是保存命令回执数量。
	Receipts int64 `gorm:"column:receipts"`
}

// TestCreationSpecRepositoryPostgreSQLPreviewSemantics 使用真实 PostgreSQL 验证并发幂等、Owner、版本与 Query 收敛。
func TestCreationSpecRepositoryPostgreSQLPreviewSemantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewCreationSpecRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewCreationSpecRepository() error = %v", err)
	}
	userID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: userID, Title: "CreationSpec 集成项目",
		LifecycleStatus: "active", RecentRunStatus: "idle", InitialPromptStatus: "absent",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed CreationSpec Project: %v", err)
	}
	projectContext, err := repository.FindOwnedProject(context.Background(), userID, projectID)
	if err != nil || projectContext.ProjectID != projectID || projectContext.Version != 1 {
		t.Fatalf("FindOwnedProject() result=%+v error=%v", projectContext, err)
	}
	if _, err := repository.FindOwnedProject(context.Background(), newRepositoryTestUUIDv7(t), projectID); !errors.Is(err, creationspec.ErrNotFound) {
		t.Fatalf("cross-owner project was not hidden: %v", err)
	}

	aggregate := newCreationSpecRepositoryAggregate(t, userID, projectID, 1)
	const concurrency = 24
	candidates := make([]creationspec.SaveAggregate, concurrency)
	for index := range candidates {
		candidates[index] = aggregate
		if index == 0 {
			continue
		}
		// 真实 Service 的并发请求会各自产生候选 Draft/Receipt UUID；只有 command_id 与请求语义相同。
		candidates[index].Draft.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.ID = newRepositoryTestUUIDv7(t)
		candidates[index].Receipt.CreationSpecID = candidates[index].Draft.ID
	}
	results := make(chan creationspec.SaveResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for index := range concurrency {
		go func(candidate creationspec.SaveAggregate) {
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
		case creationspec.CommandDispositionCreated:
			created++
		case creationspec.CommandDispositionReplayed:
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
	if counts := readCreationSpecFactCounts(t, db); counts != (creationSpecFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("same command created duplicate facts: %+v", counts)
	}

	query, err := repository.QueryCommand(context.Background(), creationspec.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != creationspec.QueryStatusCompleted || query.Draft == nil || query.Draft.ID != canonicalDraftID {
		t.Fatalf("QueryCommand() result=%+v error=%v", query, err)
	}
	wrongDigest := aggregate.Receipt.RequestDigest
	wrongDigest[0] ^= 0xff
	query, err = repository.QueryCommand(context.Background(), creationspec.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: wrongDigest, UserID: userID, ProjectID: projectID,
	})
	if err != nil || query.Status != creationspec.QueryStatusConflict || query.Draft != nil {
		t.Fatalf("wrong digest query result=%+v error=%v", query, err)
	}
	query, err = repository.QueryCommand(context.Background(), creationspec.QueryCommand{
		CommandID: aggregate.Receipt.CommandID, RequestDigest: aggregate.Receipt.RequestDigest,
		UserID: newRepositoryTestUUIDv7(t), ProjectID: projectID,
	})
	if err != nil || query.Status != creationspec.QueryStatusNotFound {
		t.Fatalf("cross-owner query result=%+v error=%v", query, err)
	}

	conflicting := newCreationSpecRepositoryAggregate(t, userID, projectID, 1)
	conflicting.Receipt.CommandID = aggregate.Receipt.CommandID
	conflicting.Draft.Content.Title = "同一命令的不同标题"
	resetCreationSpecAggregateDigests(t, &conflicting)
	if _, err := repository.SaveDraft(context.Background(), conflicting); !errors.Is(err, creationspec.ErrIdempotencyConflict) {
		t.Fatalf("same command different digest error=%v", err)
	}
	stale := newCreationSpecRepositoryAggregate(t, userID, projectID, 2)
	if _, err := repository.SaveDraft(context.Background(), stale); !errors.Is(err, creationspec.ErrVersionConflict) {
		t.Fatalf("stale Project version error=%v", err)
	}
	if counts := readCreationSpecFactCounts(t, db); counts != (creationSpecFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("conflict/version failure changed facts: %+v", counts)
	}

	duplicateSource := newCreationSpecRepositoryAggregate(t, userID, projectID, 1)
	duplicateSource.Draft.SourceToolCallID = aggregate.Draft.SourceToolCallID
	duplicateSource.Receipt.SourceToolCallID = aggregate.Draft.SourceToolCallID
	resetCreationSpecAggregateDigests(t, &duplicateSource)
	if _, err := repository.SaveDraft(context.Background(), duplicateSource); !errors.Is(err, creationspec.ErrPersistence) {
		t.Fatalf("duplicate source Tool Call error=%v", err)
	}
	rolledBack, err := repository.QueryCommand(context.Background(), creationspec.QueryCommand{
		CommandID: duplicateSource.Receipt.CommandID, RequestDigest: duplicateSource.Receipt.RequestDigest,
		UserID: userID, ProjectID: projectID,
	})
	if err != nil || rolledBack.Status != creationspec.QueryStatusNotFound {
		t.Fatalf("failed Draft insert left command receipt: result=%+v error=%v", rolledBack, err)
	}
	if counts := readCreationSpecFactCounts(t, db); counts != (creationSpecFactCounts{Drafts: 1, Receipts: 1}) {
		t.Fatalf("failed Draft insert was not fully rolled back: %+v", counts)
	}
}

// readCreationSpecFactCounts 用一次固定 SQL 查询 Draft 与回执数量。
func readCreationSpecFactCounts(t *testing.T, db *gorm.DB) creationSpecFactCounts {
	t.Helper()
	var counts creationSpecFactCounts
	if err := db.Raw(`SELECT
		(SELECT COUNT(*) FROM business.creation_spec) AS drafts,
		(SELECT COUNT(*) FROM business.creation_spec_command_receipt) AS receipts`).Scan(&counts).Error; err != nil {
		t.Fatalf("count CreationSpec facts: %v", err)
	}
	return counts
}

func newCreationSpecRepositoryAggregate(t *testing.T, userID string, projectID string, expectedVersion int64) creationspec.SaveAggregate {
	t.Helper()
	content := creationspec.Content{
		Title: "夏日短片", Goal: "制作一支 30 秒新品短片", DeliverableType: creationspec.DeliverableTypeVideo,
		Audience: "年轻消费者", Locale: "zh-CN",
		Phases:      []creationspec.Phase{{Key: "phase_1", Title: "规划", Objective: "确定叙事", Output: "创意方案"}},
		Constraints: []string{"竖屏 9:16"}, AcceptanceCriteria: []string{"成片时长为 30 秒"},
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	aggregate := creationspec.SaveAggregate{
		Draft: creationspec.Draft{
			ID: newRepositoryTestUUIDv7(t), ProjectID: projectID, UserID: userID,
			Status: creationspec.DraftStatus, Version: creationspec.InitialDraftVersion,
			SchemaVersion: creationspec.DraftSchemaVersion, Content: content,
			SourceToolCallID: newRepositoryTestUUIDv7(t), SourcePromptVersion: "prompt.preview.v1",
			SourceValidatorVersion: "validator.preview.v1", CreatedAt: now, UpdatedAt: now,
		},
		Receipt: creationspec.CommandReceipt{
			ID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t), UserID: userID,
			ProjectID: projectID, ExpectedProjectVersion: expectedVersion, CreatedAt: now,
		},
	}
	aggregate.Receipt.SourceToolCallID = aggregate.Draft.SourceToolCallID
	aggregate.Receipt.SourcePromptVersion = aggregate.Draft.SourcePromptVersion
	aggregate.Receipt.SourceValidatorVersion = aggregate.Draft.SourceValidatorVersion
	aggregate.Receipt.CreationSpecID = aggregate.Draft.ID
	aggregate.Receipt.ResultVersion = aggregate.Draft.Version
	aggregate.Receipt.ResultStatus = aggregate.Draft.Status
	resetCreationSpecAggregateDigests(t, &aggregate)
	return aggregate
}

func resetCreationSpecAggregateDigests(t *testing.T, aggregate *creationspec.SaveAggregate) {
	t.Helper()
	contentDigest, err := creationspec.ContentDigest(aggregate.Draft.Content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	requestDigest, err := creationspec.SaveRequestDigest(
		aggregate.Draft.UserID, aggregate.Draft.ProjectID, aggregate.Receipt.ExpectedProjectVersion,
		aggregate.Draft.SourceToolCallID, aggregate.Draft.SourcePromptVersion,
		aggregate.Draft.SourceValidatorVersion, aggregate.Draft.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	aggregate.Draft.ContentDigest = contentDigest
	aggregate.Receipt.RequestDigest = requestDigest
	aggregate.Receipt.ResultContentDigest = contentDigest
}
