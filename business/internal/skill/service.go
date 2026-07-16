package skill

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// defaultOwnerPageSize 是 Owner Skill 列表固定页大小，避免客户端扩大单次查询资源占用。
	defaultOwnerPageSize = 20
	// maxIdempotencyKeyBytes 是 Owner 和内部 Reviewer 幂等键的可见 ASCII 字节上限。
	maxIdempotencyKeyBytes = 128
)

// Clock 为 Skill 应用服务提供可测试的 UTC 当前时间。
type Clock interface {
	// Now 返回当前时间；应用服务写事实前统一转换为 UTC。
	Now() time.Time
}

// IDGenerator 为 Skill、修订、审核、快照、回执和审计生成 UUIDv7。
type IDGenerator interface {
	// New 返回新的 UUIDv7；生成失败时应用服务不得开始事务。
	New() (string, error)
}

// OwnerSkillDTO 是 Owner HTTP 使用的安全 Skill 投影，不包含内部 revision、digest 或 GORM Model。
type OwnerSkillDTO struct {
	// SkillID 是 Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// Definition 是当前草稿的完整结构化定义。
	Definition SkillDefinitionV1 `json:"definition"`
	// ContentStatus 只允许 draft 或 published。
	ContentStatus string `json:"content_status"`
	// HasUnpublishedChanges 表示当前草稿尚未成为当前发布快照。
	HasUnpublishedChanges bool `json:"has_unpublished_changes"`
	// ReviewStatus 是最新审核状态；从未提交时为 null。
	ReviewStatus *ReviewStatus `json:"review_status"`
	// ReviewReasonCode 是最新审核的安全原因代码；无原因时为 null。
	ReviewReasonCode *string `json:"review_reason_code"`
	// ReviewUpdatedAt 是最新审核 UTC RFC3339 时间；从未提交时为 null。
	ReviewUpdatedAt *string `json:"review_updated_at"`
	// GovernanceStatus 是 active、suspended 或 offline。
	GovernanceStatus GovernanceStatus `json:"governance_status"`
	// AllowedActions 是按固定顺序返回的 Owner 当前允许动作。
	AllowedActions []string `json:"allowed_actions"`
	// DraftETag 是可原样传入 If-Match 的 quoted opaque ETag。
	DraftETag string `json:"draft_etag"`
}

// CreateCommand 是 Owner 创建首个 Skill 草稿的应用命令。
type CreateCommand struct {
	// OwnerUserID 只来自可信 Auth Principal。
	OwnerUserID string
	// IdempotencyKey 是请求头原值，只在应用层计算摘要。
	IdempotencyKey string
	// Definition 是严格 JSON DTO 解码的结构化定义。
	Definition SkillDefinitionV1
}

// CreateResult 是首次创建或同键重放返回的安全 Owner 投影。
type CreateResult struct {
	// Skill 是当前 Owner 投影。
	Skill OwnerSkillDTO
	// IdempotentReplay 表示命中同语义既有创建回执。
	IdempotentReplay bool
}

// UpdateDraftCommand 是 Owner 使用 opaque ETag 全量替换草稿的应用命令。
type UpdateDraftCommand struct {
	// OwnerUserID 只来自可信 Auth Principal。
	OwnerUserID string
	// SkillID 来自路径并按 UUIDv7 验证。
	SkillID string
	// IfMatch 是客户端原样返回的 quoted opaque ETag。
	IfMatch string
	// Definition 是完整替换后的结构化定义。
	Definition SkillDefinitionV1
}

// SubmitReviewCommand 是 Owner 提交当前精确草稿审核的应用命令。
type SubmitReviewCommand struct {
	// OwnerUserID 只来自可信 Auth Principal。
	OwnerUserID string
	// SkillID 是目标 Skill。
	SkillID string
	// IdempotencyKey 是审核提交意图键。
	IdempotencyKey string
	// IfMatch 是 Owner 最近读取并原样返回的 quoted opaque draft_etag。
	IfMatch string
}

// SubmitReviewServiceResult 是提交审核的安全应用结果。
type SubmitReviewServiceResult struct {
	// Skill 是提交后的 Owner 当前投影。
	Skill OwnerSkillDTO
	// ReviewID 是首次命令冻结的审核提交 UUIDv7。
	ReviewID string
	// IdempotentReplay 表示命中同语义既有提交回执。
	IdempotentReplay bool
}

// ReviewerPrincipal 是内部受信身份解析后交给应用服务的最小 Reviewer Principal。
type ReviewerPrincipal struct {
	// UserID 是 Reviewer 用户 UUIDv7。
	UserID string
	// Capabilities 是权威身份系统解析的能力集合。
	Capabilities []string
}

// ApproveAndPublishServiceCommand 是内部 Reviewer 原子批准并发布的应用命令。
type ApproveAndPublishServiceCommand struct {
	// Reviewer 是受信内部 Principal，不接受 HTTP Body 或模型声明覆盖。
	Reviewer ReviewerPrincipal
	// ReviewID 是待决定审核提交 UUIDv7。
	ReviewID string
	// IdempotencyKey 是内部决定 ID，只保存摘要。
	IdempotencyKey string
	// Decision 当前只允许 approved。
	Decision string
	// IfMatch 是详情返回的原样 strong review_etag。
	IfMatch string
	// RequestID 是本次 HTTP 请求的服务端 UUIDv7。
	RequestID string
}

// ReviewerQueueItemDTO 是管理队列稳定 HTTP 投影。
type ReviewerQueueItemDTO struct {
	ReviewID       string       `json:"review_id"`
	SkillID        string       `json:"skill_id"`
	Name           string       `json:"name"`
	Summary        string       `json:"summary"`
	Category       string       `json:"category"`
	Status         ReviewStatus `json:"status"`
	SubmittedAt    string       `json:"submitted_at"`
	AllowedActions []string     `json:"allowed_actions"`
}

// ReviewerQueueResult 是 oldest-first keyset 队列结果。
type ReviewerQueueResult struct {
	Items      []ReviewerQueueItemDTO
	NextCursor string
}

// ReviewerCurrentPublishedDTO 是详情中的当前发布只读对照。
type ReviewerCurrentPublishedDTO struct {
	PublishedSnapshotID string            `json:"published_snapshot_id"`
	PublishedAt         string            `json:"published_at"`
	Definition          SkillDefinitionV1 `json:"definition"`
}

// ReviewerComparisonDTO 描述冻结提交与当前发布摘要关系。
type ReviewerComparisonDTO struct {
	HasCurrentPublished bool `json:"has_current_published"`
	SameContent         bool `json:"same_content"`
}

// ReviewerDetailDTO 是 Reviewer 冻结详情安全投影。
type ReviewerDetailDTO struct {
	ReviewID         string                       `json:"review_id"`
	SkillID          string                       `json:"skill_id"`
	OwnerUserID      string                       `json:"owner_user_id"`
	Status           ReviewStatus                 `json:"status"`
	SubmittedAt      string                       `json:"submitted_at"`
	UpdatedAt        string                       `json:"updated_at"`
	Definition       SkillDefinitionV1            `json:"definition"`
	CurrentPublished *ReviewerCurrentPublishedDTO `json:"current_published"`
	Comparison       ReviewerComparisonDTO        `json:"comparison"`
	ReviewETag       string                       `json:"review_etag"`
	AllowedActions   []string                     `json:"allowed_actions"`
}

// ReviewerDecisionDTO 是 Reviewer 决定专用安全投影。
type ReviewerDecisionDTO struct {
	ReviewID            string       `json:"review_id"`
	SkillID             string       `json:"skill_id"`
	Status              ReviewStatus `json:"status"`
	PublishedSnapshotID string       `json:"published_snapshot_id"`
	DecidedAt           string       `json:"decided_at"`
	AllowedActions      []string     `json:"allowed_actions"`
}

// ApproveAndPublishResult 是内部批准发布后的专用决定结果。
type ApproveAndPublishResult struct {
	// Review 是首次决定或同义重放的冻结 Reviewer 投影。
	Review ReviewerDecisionDTO
	// IdempotentReplay 表示命中同语义既有审核决定。
	IdempotentReplay bool
}

// OwnerListResult 是 keyset 分页的 Owner Skill 安全结果。
type OwnerListResult struct {
	// Items 是当前页安全投影。
	Items []OwnerSkillDTO
	// NextCursor 是下一页 opaque 游标，无更多数据时为空。
	NextCursor string
}

// Service 编排 Skill 草稿、Owner 读取、审核提交与内部批准发布。
type Service struct {
	repository  Repository
	clock       Clock
	idGenerator IDGenerator
}

// NewService 校验 Skill Repository、Clock 和 UUIDv7 Generator 后创建应用服务。
func NewService(repository Repository, clock Clock, idGenerator IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || idGenerator == nil {
		return nil, errors.New("create skill service: required dependency is missing")
	}
	return &Service{repository: repository, clock: clock, idGenerator: idGenerator}, nil
}

// Create 规范化定义、冻结幂等语义，并原子创建聚合、首修订和回执。
func (s *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	if !isUUIDv7(command.OwnerUserID) {
		return CreateResult{}, ErrInvalidDefinition
	}
	keyDigest, err := idempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return CreateResult{}, err
	}
	definition, canonical, contentDigest, err := normalizeAndCanonicalize(command.Definition)
	if err != nil {
		return CreateResult{}, err
	}
	semanticDigest := commandSemanticDigest(CommandTypeCreate, command.OwnerUserID, "", contentDigest)
	ids, err := s.newIDs(3)
	if err != nil {
		return CreateResult{}, err
	}
	now := s.clock.Now().UTC()
	revisionID := ids[1]
	aggregate := CreateAggregate{
		Skill: Skill{
			ID: ids[0], OwnerUserID: command.OwnerUserID, CurrentDraftRevisionID: revisionID,
			PublicationRevision: 0, GovernanceStatus: GovernanceStatusActive, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		Draft: ContentRevision{
			ID: revisionID, SkillID: ids[0], RevisionNo: 1, Definition: definition,
			CanonicalJSON: canonical, ContentDigest: contentDigest,
			CreatedByUserID: command.OwnerUserID, CreatedAt: now,
		},
		Receipt: CommandReceipt{
			ID: ids[2], ActorUserID: command.OwnerUserID, CommandType: CommandTypeCreate,
			ScopeID: command.OwnerUserID, KeyDigest: keyDigest, SemanticDigest: semanticDigest,
			ResultSkillID: ids[0], ResultContentRevisionID: &revisionID, CreatedAt: now,
			ResponseDraftRevisionID: revisionID, ResponseGovernanceStatus: GovernanceStatusActive,
		},
	}
	state, replay, err := s.repository.Create(ctx, aggregate)
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Skill: ownerDTO(state), IdempotentReplay: replay}, nil
}

// FindOwnedByID 返回当前 Owner 草稿、发布摘要和审核投影；跨 Owner 与不存在统一 ErrSkillNotFound。
func (s *Service) FindOwnedByID(ctx context.Context, skillID string, ownerUserID string) (OwnerSkillDTO, error) {
	if !isUUIDv7(skillID) || !isUUIDv7(ownerUserID) {
		return OwnerSkillDTO{}, ErrSkillNotFound
	}
	state, err := s.repository.FindOwnedByID(ctx, skillID, ownerUserID)
	if err != nil {
		return OwnerSkillDTO{}, err
	}
	return ownerDTO(state), nil
}

// ListOwned 使用固定页大小和 opaque keyset cursor 返回 Owner Skill，Repository 只执行一次集合查询。
func (s *Service) ListOwned(ctx context.Context, ownerUserID string, cursor string) (OwnerListResult, error) {
	if !isUUIDv7(ownerUserID) {
		return OwnerListResult{}, ErrSkillNotFound
	}
	boundary, err := decodeCursor(cursor)
	if err != nil {
		return OwnerListResult{}, err
	}
	page, err := s.repository.ListOwned(ctx, ownerUserID, boundary, defaultOwnerPageSize)
	if err != nil {
		return OwnerListResult{}, err
	}
	result := OwnerListResult{Items: make([]OwnerSkillDTO, 0, len(page.Items))}
	for _, item := range page.Items {
		result.Items = append(result.Items, ownerDTO(item))
	}
	if page.HasMore && len(page.Items) != 0 {
		last := page.Items[len(page.Items)-1].Skill
		result.NextCursor, err = encodeCursor(PageBoundary{UpdatedAt: last.UpdatedAt, SkillID: last.ID})
		if err != nil {
			return OwnerListResult{}, ErrPersistence
		}
	}
	return result, nil
}

// ListReviewQueue 返回冻结修订派生的 reviewing 队列，并在 Service 再次校验正式 capability。
func (s *Service) ListReviewQueue(ctx context.Context, reviewer ReviewerPrincipal, status string, cursor string) (ReviewerQueueResult, error) {
	if !isUUIDv7(reviewer.UserID) || !hasCapability(reviewer.Capabilities, ReviewCapability) {
		return ReviewerQueueResult{}, ErrReviewCapabilityRequired
	}
	if status != string(ReviewStatusReviewing) {
		return ReviewerQueueResult{}, ErrInvalidReviewRequest
	}
	boundary, err := decodeReviewQueueCursor(status, cursor)
	if err != nil {
		return ReviewerQueueResult{}, err
	}
	page, err := s.repository.ListReviewQueue(ctx, boundary, defaultOwnerPageSize)
	if err != nil {
		return ReviewerQueueResult{}, err
	}
	result := ReviewerQueueResult{Items: make([]ReviewerQueueItemDTO, 0, len(page.Items))}
	for _, item := range page.Items {
		if item.Status != ReviewStatusReviewing {
			return ReviewerQueueResult{}, ErrPersistence
		}
		result.Items = append(result.Items, ReviewerQueueItemDTO{
			ReviewID: item.ReviewID, SkillID: item.SkillID, Name: item.Name, Summary: item.Summary,
			Category: item.Category, Status: item.Status, SubmittedAt: item.SubmittedAt.UTC().Format(time.RFC3339Nano),
			AllowedActions: []string{CommandTypeApproveAndPublish},
		})
	}
	if page.HasMore && len(page.Items) != 0 {
		last := page.Items[len(page.Items)-1]
		result.NextCursor, err = encodeReviewQueueCursor(ReviewQueueBoundary{SubmittedAt: last.SubmittedAt, ReviewID: last.ReviewID})
		if err != nil {
			return ReviewerQueueResult{}, ErrPersistence
		}
	}
	return result, nil
}

// FindReviewDetail 返回提交时冻结 Definition，并只把当前发布作为只读摘要对照。
func (s *Service) FindReviewDetail(ctx context.Context, reviewer ReviewerPrincipal, reviewID string) (ReviewerDetailDTO, error) {
	if !isUUIDv7(reviewer.UserID) || !hasCapability(reviewer.Capabilities, ReviewCapability) {
		return ReviewerDetailDTO{}, ErrReviewCapabilityRequired
	}
	if !isUUIDv7(reviewID) {
		return ReviewerDetailDTO{}, ErrInvalidReviewRequest
	}
	detail, err := s.repository.FindReviewDetail(ctx, reviewID)
	if err != nil {
		return ReviewerDetailDTO{}, err
	}
	current := (*ReviewerCurrentPublishedDTO)(nil)
	comparison := ReviewerComparisonDTO{}
	if detail.CurrentPublished != nil {
		comparison.HasCurrentPublished = true
		comparison.SameContent = detail.CurrentPublished.ContentDigest == detail.Review.ContentDigest
		current = &ReviewerCurrentPublishedDTO{
			PublishedSnapshotID: detail.CurrentPublished.ID,
			PublishedAt:         detail.CurrentPublished.PublishedAt.UTC().Format(time.RFC3339Nano),
			Definition:          cloneDefinition(detail.CurrentPublished.Definition),
		}
	}
	return ReviewerDetailDTO{
		ReviewID: detail.Review.ID, SkillID: detail.Review.SkillID, OwnerUserID: detail.OwnerUserID,
		Status: detail.Review.Status, SubmittedAt: detail.Review.SubmittedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: detail.Review.UpdatedAt.UTC().Format(time.RFC3339Nano), Definition: cloneDefinition(detail.Definition),
		CurrentPublished: current, Comparison: comparison, ReviewETag: ReviewETag(detail.Review),
		AllowedActions: reviewAllowedActions(detail.Review.Status),
	}, nil
}

// UpdateDraft 校验 If-Match 后追加不可变修订，并用旧草稿指针 CAS 防止丢失更新。
func (s *Service) UpdateDraft(ctx context.Context, command UpdateDraftCommand) (OwnerSkillDTO, error) {
	if !isUUIDv7(command.SkillID) || !isUUIDv7(command.OwnerUserID) {
		return OwnerSkillDTO{}, ErrSkillNotFound
	}
	definition, canonical, digest, err := normalizeAndCanonicalize(command.Definition)
	if err != nil {
		return OwnerSkillDTO{}, err
	}
	current, err := s.repository.FindOwnedByID(ctx, command.SkillID, command.OwnerUserID)
	if err != nil {
		return OwnerSkillDTO{}, err
	}
	if command.IfMatch == "" || command.IfMatch != draftETag(current) {
		return OwnerSkillDTO{}, ErrDraftConflict
	}
	revisionID, err := s.newID()
	if err != nil {
		return OwnerSkillDTO{}, err
	}
	now := s.clock.Now().UTC()
	updated, err := s.repository.AppendDraft(ctx, AppendDraftCommand{
		SkillID: command.SkillID, OwnerUserID: command.OwnerUserID,
		ExpectedDraftRevisionID: current.Draft.ID,
		Draft: ContentRevision{
			ID: revisionID, SkillID: command.SkillID, RevisionNo: current.Draft.RevisionNo + 1,
			Definition: definition, CanonicalJSON: canonical, ContentDigest: digest,
			CreatedByUserID: command.OwnerUserID, CreatedAt: now,
		},
		UpdatedAt: now,
	})
	if err != nil {
		return OwnerSkillDTO{}, err
	}
	return ownerDTO(updated), nil
}

// SubmitReview 冻结当前草稿，并用 Skill 作用域幂等键避免重复审核事实。
func (s *Service) SubmitReview(ctx context.Context, command SubmitReviewCommand) (SubmitReviewServiceResult, error) {
	if !isUUIDv7(command.SkillID) || !isUUIDv7(command.OwnerUserID) {
		return SubmitReviewServiceResult{}, ErrSkillNotFound
	}
	keyDigest, err := idempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return SubmitReviewServiceResult{}, err
	}
	current, err := s.repository.FindOwnedByID(ctx, command.SkillID, command.OwnerUserID)
	if err != nil {
		return SubmitReviewServiceResult{}, err
	}
	expectedDraftRevisionID := ""
	if command.IfMatch == draftETag(current) {
		expectedDraftRevisionID = current.Draft.ID
	}
	ids, err := s.newIDs(2)
	if err != nil {
		return SubmitReviewServiceResult{}, err
	}
	now := s.clock.Now().UTC()
	reviewID := ids[0]
	semanticDigest := submitReviewSemanticDigest(command.SkillID, command.IfMatch)
	result, err := s.repository.SubmitReview(ctx, SubmitReviewAggregate{
		ExpectedDraftRevisionID: expectedDraftRevisionID,
		Review: ReviewSubmission{
			ID: reviewID, SkillID: command.SkillID, ContentRevisionID: current.Draft.ID,
			ContentDigest: current.Draft.ContentDigest, Status: ReviewStatusReviewing, Version: 1,
			SubmittedByUserID: command.OwnerUserID, SubmittedAt: now, UpdatedAt: now,
		},
		Receipt: CommandReceipt{
			ID: ids[1], ActorUserID: command.OwnerUserID, CommandType: CommandTypeSubmitReview,
			ScopeID: command.SkillID, KeyDigest: keyDigest, SemanticDigest: semanticDigest,
			ResultSkillID: command.SkillID, ResultContentRevisionID: stringPointer(current.Draft.ID),
			ResultReviewSubmissionID: &reviewID, CreatedAt: now,
			ResponseDraftRevisionID:     current.Draft.ID,
			ResponsePublishedSnapshotID: publishedSnapshotID(current.Published),
			ResponseReviewSubmissionID:  &reviewID, ResponseReviewStatus: reviewStatusPointer(ReviewStatusReviewing),
			ResponseReviewUpdatedAt: timePointer(now), ResponseGovernanceStatus: current.Skill.GovernanceStatus,
		},
	})
	if err != nil {
		return SubmitReviewServiceResult{}, err
	}
	return SubmitReviewServiceResult{
		Skill: ownerDTO(result.State), ReviewID: result.ReviewID, IdempotentReplay: result.IdempotentReplay,
	}, nil
}

// ApproveAndPublish 在进入 Repository 前校验正式 capability，再由单事务重校验审核内容并原子切换发布指针。
func (s *Service) ApproveAndPublish(ctx context.Context, command ApproveAndPublishServiceCommand) (ApproveAndPublishResult, error) {
	if !isUUIDv7(command.Reviewer.UserID) || !hasCapability(command.Reviewer.Capabilities, ReviewCapability) {
		return ApproveAndPublishResult{}, ErrReviewCapabilityRequired
	}
	if !isUUIDv7(command.ReviewID) || !isUUIDv7(command.RequestID) || command.Decision != string(ReviewStatusApproved) ||
		ValidateStrongReviewETag(command.IfMatch) != nil {
		return ApproveAndPublishResult{}, ErrInvalidReviewRequest
	}
	keyDigest, err := idempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return ApproveAndPublishResult{}, err
	}
	ids, err := s.newIDs(3)
	if err != nil {
		return ApproveAndPublishResult{}, err
	}
	now := s.clock.Now().UTC()
	snapshotID := ids[0]
	semanticDigest := reviewDecisionSemanticDigest(command.ReviewID, command.Decision, command.IfMatch)
	result, err := s.repository.ApproveAndPublish(ctx, ApproveAndPublishCommand{
		ReviewID: command.ReviewID, ReviewerUserID: command.Reviewer.UserID, SnapshotID: snapshotID,
		ReceiptID: ids[1], RequestID: command.RequestID, KeyDigest: keyDigest, SemanticDigest: semanticDigest,
		IfMatch: command.IfMatch, AuditID: ids[2], DecidedAt: now,
	})
	if err != nil {
		return ApproveAndPublishResult{}, err
	}
	return ApproveAndPublishResult{
		Review: ReviewerDecisionDTO{
			ReviewID: result.ReviewID, SkillID: result.SkillID, Status: result.Status,
			PublishedSnapshotID: result.PublishedSnapshotID, DecidedAt: result.DecidedAt.UTC().Format(time.RFC3339Nano),
			AllowedActions: []string{},
		},
		IdempotentReplay: result.IdempotentReplay,
	}, nil
}

// ownerDTO 将内部 revision、digest、Owner ID 和发布时间收敛为冻结 Owner HTTP 投影。
func ownerDTO(state OwnerState) OwnerSkillDTO {
	contentStatus := "draft"
	hasUnpublishedChanges := true
	if state.Published != nil {
		contentStatus = "published"
		hasUnpublishedChanges = state.Published.ContentDigest != state.Draft.ContentDigest
	}
	var reviewStatus *ReviewStatus
	var reviewReason *string
	var reviewUpdatedAt *string
	if state.LatestReview != nil {
		status := state.LatestReview.Status
		reviewStatus = &status
		reviewReason = cloneStringPointer(state.LatestReview.SafeReasonCode)
		formatted := state.LatestReview.UpdatedAt.UTC().Format(time.RFC3339Nano)
		reviewUpdatedAt = &formatted
	}
	allowed := []string{"edit_draft"}
	if (state.LatestReview == nil || state.LatestReview.Status != ReviewStatusReviewing) && hasUnpublishedChanges {
		allowed = append(allowed, "submit_review")
	}
	return OwnerSkillDTO{
		SkillID: state.Skill.ID, Definition: cloneDefinition(state.Draft.Definition), ContentStatus: contentStatus,
		HasUnpublishedChanges: hasUnpublishedChanges, ReviewStatus: reviewStatus,
		ReviewReasonCode: reviewReason, ReviewUpdatedAt: reviewUpdatedAt,
		GovernanceStatus: state.Skill.GovernanceStatus, AllowedActions: allowed, DraftETag: draftETag(state),
	}
}

// draftETag 只覆盖当前草稿内容事实，审核或发布状态变化不会无意义地使 Builder 草稿过期。
func draftETag(state OwnerState) string {
	value := "skill_draft_etag.v1\x00" + state.Skill.ID + "\x00" + state.Draft.ID + "\x00" + state.Draft.ContentDigest.Hex()
	digest := sha256.Sum256([]byte(value))
	return `"s1-` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`
}

// normalizeAndCanonicalize 保证 Repository 只接收完成规范化且摘要可重算的定义。
func normalizeAndCanonicalize(input SkillDefinitionV1) (SkillDefinitionV1, []byte, Digest, error) {
	normalized, err := NormalizeDefinitionV1(input)
	if err != nil {
		return SkillDefinitionV1{}, nil, Digest{}, err
	}
	canonical, digest, err := CanonicalDefinitionV1(normalized)
	if err != nil {
		return SkillDefinitionV1{}, nil, Digest{}, err
	}
	if len(canonical) > MaxCanonicalDefinitionBytes {
		return SkillDefinitionV1{}, nil, Digest{}, ErrInvalidDefinition
	}
	return normalized, canonical, digest, nil
}

// ReviewETag 为审核状态、版本和冻结摘要生成单个 strong opaque tag。
func ReviewETag(review ReviewSubmission) string {
	value := "skill_review_etag.v1\x00" + review.ID + "\x00" + string(review.Status) + "\x00" +
		fmt.Sprintf("%d", review.Version) + "\x00" + review.ContentDigest.Hex()
	digest := sha256.Sum256([]byte(value))
	return `"sr1-` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`
}

// ValidateStrongReviewETag 拒绝通配符、weak/list 标签、空白和非规范 Base64URL。
func ValidateStrongReviewETag(value string) error {
	const prefix = `"sr1-`
	if len(value) != len(prefix)+43+1 || !strings.HasPrefix(value, prefix) || value[len(value)-1] != '"' ||
		strings.TrimSpace(value) != value || strings.Contains(value, ",") {
		return ErrInvalidReviewRequest
	}
	raw := value[len(prefix) : len(value)-1]
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return ErrInvalidReviewRequest
	}
	return nil
}

func reviewAllowedActions(status ReviewStatus) []string {
	if status == ReviewStatusReviewing {
		return []string{CommandTypeApproveAndPublish}
	}
	return []string{}
}

// commandSemanticDigest 固定命令类型、作用域与可选内容摘要的 JSON 字段顺序。
func commandSemanticDigest(commandType string, scopeID string, contentRevisionID string, contentDigest Digest) Digest {
	encoded, _ := json.Marshal(struct {
		SchemaVersion     string `json:"schema_version"`
		CommandType       string `json:"command_type"`
		ScopeID           string `json:"scope_id"`
		ContentRevisionID string `json:"content_revision_id"`
		ContentDigest     string `json:"content_digest"`
	}{
		SchemaVersion: "skill_command.v1", CommandType: commandType, ScopeID: scopeID,
		ContentRevisionID: contentRevisionID, ContentDigest: contentDigest.Hex(),
	})
	return sha256.Sum256(encoded)
}

// submitReviewSemanticDigest 将客户端实际提交的 opaque ETag 纳入幂等语义。
// Repository 因而可以先重放已提交结果，再判断当前草稿是否仍匹配首次新命令。
func submitReviewSemanticDigest(skillID string, draftETag string) Digest {
	encoded, _ := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		CommandType   string `json:"command_type"`
		SkillID       string `json:"skill_id"`
		DraftETag     string `json:"draft_etag"`
	}{
		SchemaVersion: "skill_command.v1", CommandType: CommandTypeSubmitReview,
		SkillID: skillID, DraftETag: draftETag,
	})
	return sha256.Sum256(encoded)
}

// reviewDecisionSemanticDigest 精确冻结 Review、approved 与客户端原样 strong If-Match。
func reviewDecisionSemanticDigest(reviewID string, decision string, ifMatch string) Digest {
	encoded, _ := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		ReviewID      string `json:"review_id"`
		Decision      string `json:"decision"`
		IfMatch       string `json:"if_match"`
	}{
		SchemaVersion: "skill_review_decision.v1", ReviewID: reviewID, Decision: decision, IfMatch: ifMatch,
	})
	return sha256.Sum256(encoded)
}

// idempotencyKeyDigest 校验代理可安全转发的非空可见 ASCII 键，并只返回 SHA-256 摘要。
func idempotencyKeyDigest(value string) (Digest, error) {
	if value == "" || len(value) > maxIdempotencyKeyBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return Digest{}, ErrInvalidIdempotencyKey
	}
	for _, character := range value {
		if character > unicode.MaxASCII || unicode.IsControl(character) || unicode.IsSpace(character) {
			return Digest{}, ErrInvalidIdempotencyKey
		}
	}
	return sha256.Sum256([]byte(value)), nil
}

// pageCursorV1 是 Owner 列表 opaque keyset cursor 的内部固定结构。
type pageCursorV1 struct {
	// SchemaVersion 防止后续分页规则变化时误读旧游标。
	SchemaVersion string `json:"schema_version"`
	// UpdatedAtUnixNano 是上一页最后一项 UTC 排序时间。
	UpdatedAtUnixNano int64 `json:"updated_at_unix_nano"`
	// SkillID 是稳定次级排序 UUIDv7。
	SkillID string `json:"skill_id"`
}

type reviewQueueCursorV1 struct {
	SchemaVersion       string `json:"schema_version"`
	Status              string `json:"status"`
	SubmittedAtUnixNano int64  `json:"submitted_at_unix_nano"`
	ReviewID            string `json:"review_id"`
}

func encodeReviewQueueCursor(boundary ReviewQueueBoundary) (string, error) {
	if boundary.SubmittedAt.IsZero() || !isUUIDv7(boundary.ReviewID) {
		return "", ErrInvalidCursor
	}
	encoded, err := json.Marshal(reviewQueueCursorV1{
		SchemaVersion: "skill_review_queue_cursor.v1", Status: string(ReviewStatusReviewing),
		SubmittedAtUnixNano: boundary.SubmittedAt.UTC().UnixNano(), ReviewID: boundary.ReviewID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeReviewQueueCursor(status string, cursor string) (*ReviewQueueBoundary, error) {
	if cursor == "" {
		return nil, nil
	}
	if len(cursor) > 1024 {
		return nil, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var decoded reviewQueueCursorV1
	if err := decoder.Decode(&decoded); err != nil || ensureDecoderEOF(decoder) != nil ||
		decoded.SchemaVersion != "skill_review_queue_cursor.v1" || decoded.Status != status ||
		decoded.Status != string(ReviewStatusReviewing) || decoded.SubmittedAtUnixNano <= 0 || !isUUIDv7(decoded.ReviewID) {
		return nil, ErrInvalidCursor
	}
	return &ReviewQueueBoundary{SubmittedAt: time.Unix(0, decoded.SubmittedAtUnixNano).UTC(), ReviewID: decoded.ReviewID}, nil
}

// encodeCursor 将内部 keyset 边界编码为不含填充的 URL-safe opaque 字符串。
func encodeCursor(boundary PageBoundary) (string, error) {
	if boundary.UpdatedAt.IsZero() || !isUUIDv7(boundary.SkillID) {
		return "", ErrInvalidCursor
	}
	encoded, err := json.Marshal(pageCursorV1{
		SchemaVersion: "skill_owner_cursor.v1", UpdatedAtUnixNano: boundary.UpdatedAt.UTC().UnixNano(), SkillID: boundary.SkillID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodeCursor 严格解码 Owner keyset cursor；空值表示第一页。
func decodeCursor(cursor string) (*PageBoundary, error) {
	if cursor == "" {
		return nil, nil
	}
	if len(cursor) > 1024 {
		return nil, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var decoded pageCursorV1
	if err := decoder.Decode(&decoded); err != nil {
		return nil, ErrInvalidCursor
	}
	if err := ensureDecoderEOF(decoder); err != nil || decoded.SchemaVersion != "skill_owner_cursor.v1" ||
		decoded.UpdatedAtUnixNano <= 0 || !isUUIDv7(decoded.SkillID) {
		return nil, ErrInvalidCursor
	}
	return &PageBoundary{UpdatedAt: time.Unix(0, decoded.UpdatedAtUnixNano).UTC(), SkillID: decoded.SkillID}, nil
}

// ensureDecoderEOF 拒绝游标 JSON 后的第二个值。
func ensureDecoderEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidCursor
	}
	return nil
}

// newIDs 在任何事务开始前生成固定数量 UUIDv7 并拒绝不合规实现。
func (s *Service) newIDs(count int) ([]string, error) {
	ids := make([]string, count)
	for index := range ids {
		id, err := s.newID()
		if err != nil {
			return nil, err
		}
		ids[index] = id
	}
	return ids, nil
}

// newID 生成并校验一个 UUIDv7，失败收敛为持久化不可用而不进入 Repository。
func (s *Service) newID() (string, error) {
	id, err := s.idGenerator.New()
	if err != nil || !isUUIDv7(id) {
		return "", fmt.Errorf("generate skill UUIDv7: %w", ErrPersistence)
	}
	return id, nil
}

// hasCapability 只接受权威 Principal 中精确 capability，不把企业用户类型或角色名当成审核授权。
func hasCapability(capabilities []string, required string) bool {
	for _, capability := range capabilities {
		if capability == required {
			return true
		}
	}
	return false
}

// cloneDefinition 深拷贝所有 slice，避免 transport 修改 Repository 返回的领域投影。
func cloneDefinition(input SkillDefinitionV1) SkillDefinitionV1 {
	cloned := input
	cloned.MarketListing.CoverAssetID = cloneStringPointer(input.MarketListing.CoverAssetID)
	cloned.Tags = append([]string(nil), input.Tags...)
	cloned.Examples = append([]SkillExampleV1(nil), input.Examples...)
	cloned.StarterPrompts = append([]string(nil), input.StarterPrompts...)
	cloned.PublicToolRefs = append([]PublicToolReferenceV1(nil), input.PublicToolRefs...)
	if cloned.Tags == nil {
		cloned.Tags = []string{}
	}
	if cloned.Examples == nil {
		cloned.Examples = []SkillExampleV1{}
	}
	if cloned.StarterPrompts == nil {
		cloned.StarterPrompts = []string{}
	}
	if cloned.PublicToolRefs == nil {
		cloned.PublicToolRefs = []PublicToolReferenceV1{}
	}
	return cloned
}

// stringPointer 返回字符串副本地址，避免后续局部变量复用改变命令。
func stringPointer(value string) *string { return &value }

// cloneStringPointer 深拷贝可选字符串，避免领域状态被 transport 修改。
func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// reviewStatusPointer 返回审核状态副本地址，供冻结回执保存首次响应状态。
func reviewStatusPointer(value ReviewStatus) *ReviewStatus { return &value }

// timePointer 返回 UTC 时间副本地址，供冻结回执保存首次响应状态时间。
func timePointer(value time.Time) *time.Time { return &value }

// publishedSnapshotID 返回当前发布快照引用副本，尚未发布时为空。
func publishedSnapshotID(snapshot *PublishedSnapshot) *string {
	if snapshot == nil {
		return nil
	}
	return stringPointer(snapshot.ID)
}
