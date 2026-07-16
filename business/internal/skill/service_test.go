package skill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// skillServiceTestClock 返回固定 UTC 时间，便于比较写入事实。
type skillServiceTestClock struct{ now time.Time }

// Now 返回测试固定时间。
func (clock skillServiceTestClock) Now() time.Time { return clock.now }

// skillServiceTestIDs 使用真实 UUIDv7 生成器满足领域标识约束。
type skillServiceTestIDs struct{}

// New 返回一个新的 UUIDv7。
func (skillServiceTestIDs) New() (string, error) {
	id, err := uuid.NewV7()
	return id.String(), err
}

func newSkillServiceUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// skillServiceRepository 是应用测试使用的内存事务语义替身。
type skillServiceRepository struct {
	state                 OwnerState
	createAggregate       CreateAggregate
	appendCommand         AppendDraftCommand
	submitAggregate       SubmitReviewAggregate
	approveCommand        ApproveAndPublishCommand
	createReceiptKey      *Digest
	createReceiptSemantic *Digest
	createFrozenState     OwnerState
	submitReceiptSemantic *Digest
	submitReceiptReviewID string
	submitFrozenState     OwnerState
	approveErr            error
}

// Create 从聚合构造 Owner 状态并保存入参。
func (repository *skillServiceRepository) Create(_ context.Context, aggregate CreateAggregate) (OwnerState, bool, error) {
	repository.createAggregate = aggregate
	if repository.createReceiptKey != nil && *repository.createReceiptKey == aggregate.Receipt.KeyDigest {
		if repository.createReceiptSemantic == nil || *repository.createReceiptSemantic != aggregate.Receipt.SemanticDigest {
			return OwnerState{}, false, ErrIdempotencyConflict
		}
		return cloneOwnerStateForTest(repository.createFrozenState), true, nil
	}
	repository.state = OwnerState{Skill: aggregate.Skill, Draft: aggregate.Draft}
	key := aggregate.Receipt.KeyDigest
	semantic := aggregate.Receipt.SemanticDigest
	repository.createReceiptKey = &key
	repository.createReceiptSemantic = &semantic
	repository.createFrozenState = cloneOwnerStateForTest(repository.state)
	return repository.state, false, nil
}

// FindOwnedByID 返回当前内存状态并保持 owner-safe not found。
func (repository *skillServiceRepository) FindOwnedByID(_ context.Context, skillID string, ownerUserID string) (OwnerState, error) {
	if repository.state.Skill.ID != skillID || repository.state.Skill.OwnerUserID != ownerUserID {
		return OwnerState{}, ErrSkillNotFound
	}
	return repository.state, nil
}

// ListOwned 返回至多一个内存 Owner 状态。
func (repository *skillServiceRepository) ListOwned(_ context.Context, ownerUserID string, _ *PageBoundary, _ int) (OwnerPage, error) {
	if repository.state.Skill.OwnerUserID != ownerUserID {
		return OwnerPage{Items: []OwnerState{}}, nil
	}
	return OwnerPage{Items: []OwnerState{repository.state}}, nil
}

func (repository *skillServiceRepository) ListReviewQueue(_ context.Context, _ *ReviewQueueBoundary, _ int) (ReviewQueuePage, error) {
	if repository.state.LatestReview == nil {
		return ReviewQueuePage{Items: []ReviewQueueItem{}}, nil
	}
	return ReviewQueuePage{Items: []ReviewQueueItem{{
		ReviewID: repository.state.LatestReview.ID, SkillID: repository.state.Skill.ID,
		Name: repository.state.Draft.Definition.Name, Summary: repository.state.Draft.Definition.Summary,
		Category: repository.state.Draft.Definition.Category, Status: repository.state.LatestReview.Status,
		SubmittedAt: repository.state.LatestReview.SubmittedAt,
	}}}, nil
}

func (repository *skillServiceRepository) FindReviewDetail(_ context.Context, reviewID string) (ReviewDetail, error) {
	if repository.state.LatestReview == nil || repository.state.LatestReview.ID != reviewID {
		return ReviewDetail{}, ErrReviewNotFound
	}
	return ReviewDetail{Review: *repository.state.LatestReview, OwnerUserID: repository.state.Skill.OwnerUserID,
		Definition: repository.state.Draft.Definition, CurrentPublished: repository.state.Published}, nil
}

// AppendDraft 按旧修订指针模拟 CAS 并追加新草稿。
func (repository *skillServiceRepository) AppendDraft(_ context.Context, command AppendDraftCommand) (OwnerState, error) {
	repository.appendCommand = command
	if repository.state.Draft.ID != command.ExpectedDraftRevisionID {
		return OwnerState{}, ErrDraftConflict
	}
	repository.state.Draft = command.Draft
	repository.state.Skill.CurrentDraftRevisionID = command.Draft.ID
	repository.state.Skill.UpdatedAt = command.UpdatedAt
	repository.state.Skill.Version++
	return repository.state, nil
}

// SubmitReview 模拟数据库回执语义冲突和首次 reviewing 事实。
func (repository *skillServiceRepository) SubmitReview(_ context.Context, aggregate SubmitReviewAggregate) (SubmitReviewResult, error) {
	repository.submitAggregate = aggregate
	if repository.submitReceiptSemantic != nil {
		if *repository.submitReceiptSemantic != aggregate.Receipt.SemanticDigest {
			return SubmitReviewResult{}, ErrIdempotencyConflict
		}
		return SubmitReviewResult{State: cloneOwnerStateForTest(repository.submitFrozenState), ReviewID: repository.submitReceiptReviewID, IdempotentReplay: true}, nil
	}
	if aggregate.ExpectedDraftRevisionID == "" || aggregate.ExpectedDraftRevisionID != repository.state.Draft.ID {
		return SubmitReviewResult{}, ErrDraftConflict
	}
	semantic := aggregate.Receipt.SemanticDigest
	repository.submitReceiptSemantic = &semantic
	repository.submitReceiptReviewID = aggregate.Review.ID
	repository.state.LatestReview = &aggregate.Review
	repository.submitFrozenState = cloneOwnerStateForTest(repository.state)
	return SubmitReviewResult{State: repository.state, ReviewID: aggregate.Review.ID}, nil
}

// cloneOwnerStateForTest 深拷贝内存事务状态，模拟数据库回执引用重建的冻结响应。
func cloneOwnerStateForTest(input OwnerState) OwnerState {
	cloned := input
	cloned.Skill.CurrentPublishedSnapshotID = cloneStringPointer(input.Skill.CurrentPublishedSnapshotID)
	cloned.Draft.Definition = cloneDefinition(input.Draft.Definition)
	cloned.Draft.CanonicalJSON = append([]byte(nil), input.Draft.CanonicalJSON...)
	if input.Published != nil {
		published := *input.Published
		published.Definition = cloneDefinition(input.Published.Definition)
		published.CanonicalJSON = append([]byte(nil), input.Published.CanonicalJSON...)
		cloned.Published = &published
	}
	if input.LatestReview != nil {
		review := *input.LatestReview
		review.SafeReasonCode = cloneStringPointer(input.LatestReview.SafeReasonCode)
		review.DecidedByUserID = cloneStringPointer(input.LatestReview.DecidedByUserID)
		if input.LatestReview.DecidedAt != nil {
			decidedAt := *input.LatestReview.DecidedAt
			review.DecidedAt = &decidedAt
		}
		cloned.LatestReview = &review
	}
	return cloned
}

// ApproveAndPublish 模拟单事务成功或失败；失败分支不改变旧发布快照。
func (repository *skillServiceRepository) ApproveAndPublish(_ context.Context, command ApproveAndPublishCommand) (ReviewDecisionResult, error) {
	repository.approveCommand = command
	if repository.approveErr != nil {
		return ReviewDecisionResult{}, repository.approveErr
	}
	review := repository.state.LatestReview
	if review == nil || review.ID != command.ReviewID || review.Status != ReviewStatusReviewing {
		return ReviewDecisionResult{}, ErrReviewConflict
	}
	review.Status = ReviewStatusApproved
	review.UpdatedAt = command.DecidedAt
	repository.state.LatestReview = review
	repository.state.Published = &PublishedSnapshot{
		ID: command.SnapshotID, SkillID: review.SkillID, SourceContentRevisionID: review.ContentRevisionID,
		ReviewSubmissionID: review.ID, PublicationRevision: repository.state.Skill.PublicationRevision + 1,
		Definition: repository.state.Draft.Definition, CanonicalJSON: repository.state.Draft.CanonicalJSON,
		ContentDigest: review.ContentDigest, PublishedByUserID: command.ReviewerUserID, PublishedAt: command.DecidedAt,
	}
	repository.state.Skill.CurrentPublishedSnapshotID = &command.SnapshotID
	repository.state.Skill.PublicationRevision++
	return ReviewDecisionResult{
		ReviewID: review.ID, SkillID: review.SkillID, Status: ReviewStatusApproved,
		PublishedSnapshotID: command.SnapshotID, DecidedAt: command.DecidedAt,
	}, nil
}

// newSkillServiceState 构造指定 Owner 和定义的合法内存聚合状态。
func newSkillServiceState(t *testing.T, ownerID string, definition SkillDefinitionV1, revisionNo int64) OwnerState {
	t.Helper()
	normalized, err := NormalizeDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := CanonicalDefinitionV1(normalized)
	if err != nil {
		t.Fatal(err)
	}
	skillID, _ := uuid.NewV7()
	revisionID, _ := uuid.NewV7()
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	return OwnerState{
		Skill: Skill{
			ID: skillID.String(), OwnerUserID: ownerID, CurrentDraftRevisionID: revisionID.String(),
			GovernanceStatus: GovernanceStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Draft: ContentRevision{
			ID: revisionID.String(), SkillID: skillID.String(), RevisionNo: revisionNo,
			Definition: normalized, CanonicalJSON: canonical, ContentDigest: digest, CreatedByUserID: ownerID, CreatedAt: now,
		},
	}
}

func TestServiceCreateNormalizesDefinitionAndHashesIdempotencyKey(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{}
	service, err := NewService(repository, skillServiceTestClock{now: time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)}, skillServiceTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	definition := validDefinitionForTest()
	definition.Name = "  创作策划  "
	definition.Tags = []string{"视频", "策划", "视频"}
	result, err := service.Create(context.Background(), CreateCommand{
		OwnerUserID: ownerID.String(), IdempotencyKey: "create-intent-1", Definition: definition,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skill.Definition.Name != "创作策划" || len(result.Skill.Definition.Tags) != 2 ||
		repository.createAggregate.Receipt.KeyDigest == (Digest{}) || repository.createAggregate.Receipt.SemanticDigest == (Digest{}) {
		t.Fatalf("definition/idempotency was not frozen: result=%+v receipt=%+v", result.Skill, repository.createAggregate.Receipt)
	}
	if result.Skill.ContentStatus != "draft" || result.Skill.ReviewStatus != nil || len(result.Skill.AllowedActions) != 2 {
		t.Fatalf("invalid initial owner projection: %+v", result.Skill)
	}
}

func TestServiceUpdateDraftRejectsStaleOpaqueETag(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{state: newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Now()}, skillServiceTestIDs{})
	_, err := service.UpdateDraft(context.Background(), UpdateDraftCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IfMatch: `"stale"`, Definition: validDefinitionForTest(),
	})
	if !errors.Is(err, ErrDraftConflict) || repository.appendCommand.SkillID != "" {
		t.Fatalf("stale ETag reached repository: err=%v command=%+v", err, repository.appendCommand)
	}
}

func TestServiceCreateReplayAfterDraftUpdateReturnsFrozenInitialResponse(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{}
	clock := skillServiceTestClock{now: time.Date(2026, 7, 14, 2, 30, 0, 0, time.UTC)}
	service, _ := NewService(repository, clock, skillServiceTestIDs{})
	initialDefinition := validDefinitionForTest()
	initial, err := service.Create(context.Background(), CreateCommand{
		OwnerUserID: ownerID.String(), IdempotencyKey: "create-frozen-1", Definition: initialDefinition,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedDefinition := validDefinitionForTest()
	updatedDefinition.Name = "更新后的名称"
	if _, err := service.UpdateDraft(context.Background(), UpdateDraftCommand{
		OwnerUserID: ownerID.String(), SkillID: initial.Skill.SkillID,
		IfMatch: initial.Skill.DraftETag, Definition: updatedDefinition,
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(context.Background(), CreateCommand{
		OwnerUserID: ownerID.String(), IdempotencyKey: "create-frozen-1", Definition: initialDefinition,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay || replay.Skill.Definition.Name != initial.Skill.Definition.Name ||
		replay.Skill.DraftETag != initial.Skill.DraftETag || replay.Skill.ContentStatus != "draft" || replay.Skill.ReviewStatus != nil {
		t.Fatalf("create replay drifted after update: initial=%+v replay=%+v", initial.Skill, replay.Skill)
	}
}

func TestServiceSubmitReviewReplaysOldETagAndRejectsSameKeyWithNewETag(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{state: newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)}
	clock := skillServiceTestClock{now: time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)}
	service, _ := NewService(repository, clock, skillServiceTestIDs{})
	initialETag := draftETag(repository.state)
	first, err := service.SubmitReview(context.Background(), SubmitReviewCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IdempotencyKey: "submit-intent-1", IfMatch: initialETag,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ReviewID == "" || repository.submitReceiptSemantic == nil {
		t.Fatalf("first review was not frozen: %+v", first)
	}
	// 模拟审核提交后 Owner 继续编辑草稿；原回执仍冻结旧 revision ID 与 digest。
	newDefinition := validDefinitionForTest()
	newDefinition.Summary = "已经修改的摘要"
	newState := newSkillServiceState(t, ownerID.String(), newDefinition, 2)
	newState.Skill.ID = repository.state.Skill.ID
	newState.Draft.SkillID = repository.state.Skill.ID
	newState.Skill.CreatedAt = repository.state.Skill.CreatedAt
	newState.LatestReview = repository.state.LatestReview
	repository.state = newState

	replay, err := service.SubmitReview(context.Background(), SubmitReviewCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IdempotencyKey: "submit-intent-1", IfMatch: initialETag,
	})
	if err != nil || !replay.IdempotentReplay || replay.ReviewID != first.ReviewID || replay.Skill.DraftETag != first.Skill.DraftETag {
		t.Fatalf("old If-Match replay did not return frozen first response: result=%+v err=%v", replay, err)
	}

	_, err = service.SubmitReview(context.Background(), SubmitReviewCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IdempotencyKey: "submit-intent-1", IfMatch: draftETag(repository.state),
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected changed If-Match to conflict with old key, got %v", err)
	}
}

func TestServiceSubmitReviewRejectsMissingOrStaleIfMatchBeforeCreatingReview(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{state: newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Date(2026, 7, 14, 3, 30, 0, 0, time.UTC)}, skillServiceTestIDs{})
	for _, ifMatch := range []string{"", `"stale"`} {
		_, err := service.SubmitReview(context.Background(), SubmitReviewCommand{
			OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID,
			IdempotencyKey: "submit-stale-" + ifMatch, IfMatch: ifMatch,
		})
		if !errors.Is(err, ErrDraftConflict) || repository.state.LatestReview != nil {
			t.Fatalf("If-Match %q should fail before review fact: err=%v state=%+v", ifMatch, err, repository.state)
		}
	}
}

func TestServiceApproveAndPublishRequiresFormalCapability(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	reviewerID, _ := uuid.NewV7()
	state := newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)
	reviewID, _ := uuid.NewV7()
	state.LatestReview = &ReviewSubmission{
		ID: reviewID.String(), SkillID: state.Skill.ID, ContentRevisionID: state.Draft.ID,
		ContentDigest: state.Draft.ContentDigest, Status: ReviewStatusReviewing, Version: 1,
		SubmittedByUserID: ownerID.String(), SubmittedAt: state.Skill.CreatedAt, UpdatedAt: state.Skill.CreatedAt,
	}
	repository := &skillServiceRepository{state: state}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Now()}, skillServiceTestIDs{})
	_, err := service.ApproveAndPublish(context.Background(), ApproveAndPublishServiceCommand{
		Reviewer: ReviewerPrincipal{UserID: reviewerID.String(), Capabilities: []string{}},
		ReviewID: reviewID.String(), IdempotencyKey: "decision-1", Decision: string(ReviewStatusApproved),
		IfMatch: ReviewETag(*state.LatestReview), RequestID: newSkillServiceUUIDv7(t),
	})
	if !errors.Is(err, ErrReviewCapabilityRequired) || repository.approveCommand.ReviewID != "" {
		t.Fatalf("missing capability reached repository: err=%v command=%+v", err, repository.approveCommand)
	}
}

func TestServiceSubmitReplayAfterApprovalReturnsFrozenReviewingResponse(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	reviewerID, _ := uuid.NewV7()
	repository := &skillServiceRepository{state: newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 1)}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)}, skillServiceTestIDs{})
	initialETag := draftETag(repository.state)
	first, err := service.SubmitReview(context.Background(), SubmitReviewCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IdempotencyKey: "submit-frozen-1", IfMatch: initialETag,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Skill.ReviewStatus == nil || *first.Skill.ReviewStatus != ReviewStatusReviewing {
		t.Fatalf("first submit did not return reviewing: %+v", first.Skill)
	}
	if _, err := service.ApproveAndPublish(context.Background(), ApproveAndPublishServiceCommand{
		Reviewer: ReviewerPrincipal{UserID: reviewerID.String(), Capabilities: []string{ReviewCapability}},
		ReviewID: first.ReviewID, IdempotencyKey: "approve-frozen-1", Decision: string(ReviewStatusApproved),
		IfMatch: ReviewETag(*repository.state.LatestReview), RequestID: newSkillServiceUUIDv7(t),
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := service.SubmitReview(context.Background(), SubmitReviewCommand{
		OwnerUserID: ownerID.String(), SkillID: repository.state.Skill.ID, IdempotencyKey: "submit-frozen-1", IfMatch: initialETag,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay || replay.Skill.ReviewStatus == nil || *replay.Skill.ReviewStatus != ReviewStatusReviewing ||
		replay.Skill.ContentStatus != first.Skill.ContentStatus || replay.Skill.DraftETag != first.Skill.DraftETag {
		t.Fatalf("submit replay drifted after approval: first=%+v replay=%+v", first.Skill, replay.Skill)
	}
}

func TestServiceApproveFailurePreservesOldPublishedSnapshot(t *testing.T) {
	ownerID, _ := uuid.NewV7()
	reviewerID, _ := uuid.NewV7()
	state := newSkillServiceState(t, ownerID.String(), validDefinitionForTest(), 2)
	oldSnapshotID, _ := uuid.NewV7()
	oldSnapshot := PublishedSnapshot{
		ID: oldSnapshotID.String(), SkillID: state.Skill.ID, SourceContentRevisionID: state.Draft.ID,
		ReviewSubmissionID: state.Draft.ID, PublicationRevision: 1, Definition: state.Draft.Definition,
		CanonicalJSON: state.Draft.CanonicalJSON, ContentDigest: state.Draft.ContentDigest,
		PublishedByUserID: reviewerID.String(), PublishedAt: state.Skill.CreatedAt,
	}
	state.Published = &oldSnapshot
	state.Skill.CurrentPublishedSnapshotID = &oldSnapshot.ID
	state.Skill.PublicationRevision = 1
	reviewID, _ := uuid.NewV7()
	state.LatestReview = &ReviewSubmission{
		ID: reviewID.String(), SkillID: state.Skill.ID, ContentRevisionID: state.Draft.ID,
		ContentDigest: state.Draft.ContentDigest, Status: ReviewStatusReviewing, Version: 1,
		SubmittedByUserID: ownerID.String(), SubmittedAt: state.Skill.CreatedAt, UpdatedAt: state.Skill.CreatedAt,
	}
	repository := &skillServiceRepository{state: state, approveErr: ErrPersistence}
	service, _ := NewService(repository, skillServiceTestClock{now: time.Now()}, skillServiceTestIDs{})
	_, err := service.ApproveAndPublish(context.Background(), ApproveAndPublishServiceCommand{
		Reviewer: ReviewerPrincipal{UserID: reviewerID.String(), Capabilities: []string{ReviewCapability}},
		ReviewID: reviewID.String(), IdempotencyKey: "decision-failure-1", Decision: string(ReviewStatusApproved),
		IfMatch: ReviewETag(*state.LatestReview), RequestID: newSkillServiceUUIDv7(t),
	})
	if !errors.Is(err, ErrPersistence) {
		t.Fatalf("expected publish failure, got %v", err)
	}
	if repository.state.Published == nil || repository.state.Published.ID != oldSnapshot.ID ||
		repository.state.Skill.CurrentPublishedSnapshotID == nil || *repository.state.Skill.CurrentPublishedSnapshotID != oldSnapshot.ID ||
		repository.state.Skill.PublicationRevision != 1 {
		t.Fatalf("old published snapshot changed after failed transaction: %+v", repository.state)
	}
}
