package promptpreview

import (
	"context"
	"fmt"
	"time"
)

// Repository 是 Prompt Preview 联合上下文、Draft 和命令回执的唯一持久化端口。
type Repository interface {
	// FindGenerationContext 用一次 Owner 集合查询读取指定 Storyboard Preview Draft 与 Project 快照。
	FindGenerationContext(ctx context.Context, query ContextQuery) (GenerationContext, error)
	// SaveDraft 以 command_id first-write-wins 语义原子保存 Draft 和回执。
	SaveDraft(ctx context.Context, aggregate SaveAggregate) (SaveResult, error)
	// QueryCommand 只按原 command_id、digest、user 与 project 查询首次结果。
	QueryCommand(ctx context.Context, query QueryCommand) (QueryResult, error)
}

// Clock 为一次保存命令提供可注入的冻结时间。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
}

// IDGenerator 为 Prompt Preview Draft 和命令回执生成 UUIDv7。
type IDGenerator interface {
	// New 返回新的 UUIDv7 字符串。
	New() (string, error)
}

// ContextQuery 是获取 Prompt 生成上下文的严格领域查询。
type ContextQuery struct {
	// UserID 是可信调用用户 UUIDv7。
	UserID string
	// ProjectID 是目标 Business Project UUIDv7。
	ProjectID string
	// StoryboardPreviewRef 是用户从当前 Workspace 选择的精确 Draft 引用。
	StoryboardPreviewRef StoryboardPreviewRef
}

// SaveCommand 是保存 Prompt Preview Draft 的完整冻结命令。
type SaveCommand struct {
	// CommandID 是 first-write-wins 幂等命令 UUIDv7。
	CommandID string
	// RequestDigestHex 是 Agent 计算的小写 SHA-256 Canonical 摘要。
	RequestDigestHex string
	// UserID 是可信调用用户 UUIDv7。
	UserID string
	// ProjectID 是目标 Project UUIDv7。
	ProjectID string
	// ExpectedProjectVersion 是生成上下文冻结的 Project 版本。
	ExpectedProjectVersion int64
	// StoryboardPreviewRef 是生成上下文冻结的 Storyboard Preview 精确引用。
	StoryboardPreviewRef StoryboardPreviewRef
	// ToolCallID 是来源 Graph Tool Call UUIDv7。
	ToolCallID string
	// PromptVersion 是来源 Prompt 冻结版本。
	PromptVersion string
	// ValidatorVersion 是来源候选 Validator 冻结版本。
	ValidatorVersion string
	// ExactSetValidatorVersion 是来源目标全集 Validator 冻结版本。
	ExactSetValidatorVersion string
	// ExactTargetSetDigestHex 是 Agent 冻结并由请求摘要绑定的目标全集摘要。
	ExactTargetSetDigestHex string
	// Content 是 Agent 双 Validator 已通过、但仍需 Business 复核 Source 全部 Slot 的候选。
	Content Content
}

// Service 编排严格验证、UUIDv7 生成和 Repository 事务边界。
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

// NewService 创建 Prompt Preview Service；缺少依赖时失败关闭。
func NewService(repository Repository, clock Clock, ids IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || ids == nil {
		return nil, fmt.Errorf("create prompt preview service: dependency is nil")
	}
	return &Service{repository: repository, clock: clock, ids: ids}, nil
}

// GetGenerationContext 校验可信身份和精确 Storyboard Preview 引用后读取联合上下文。
// Owner 或资源不匹配由 Repository 安全隐藏，已授权资源的版本或摘要漂移返回稳定冲突。
func (service *Service) GetGenerationContext(ctx context.Context, query ContextQuery) (GenerationContext, error) {
	if ctx == nil || !CanonicalUUIDv7(query.UserID) || !CanonicalUUIDv7(query.ProjectID) ||
		!ValidateStoryboardPreviewRef(query.StoryboardPreviewRef) {
		return GenerationContext{}, ErrInvalidInput
	}
	result, err := service.repository.FindGenerationContext(ctx, query)
	if err != nil {
		return GenerationContext{}, err
	}
	if err := ValidateGenerationContext(result); err != nil || result.ProjectID != query.ProjectID ||
		result.Storyboard.UserID != query.UserID || result.Storyboard.ID != query.StoryboardPreviewRef.ID {
		return GenerationContext{}, ErrPersistence
	}
	if result.Storyboard.Version != query.StoryboardPreviewRef.Version ||
		result.Storyboard.ContentDigest.Hex() != query.StoryboardPreviewRef.ContentDigest {
		return GenerationContext{}, ErrStoryboardVersionConflict
	}
	return result, nil
}

// SaveDraft 校验请求摘要并构造不可变 Draft 与回执，由 Repository 完成事务级 Source 复核和 first-write-wins。
func (service *Service) SaveDraft(ctx context.Context, command SaveCommand) (SaveResult, error) {
	if ctx == nil || !CanonicalUUIDv7(command.CommandID) {
		return SaveResult{}, ErrInvalidInput
	}
	providedDigest, err := ParseDigest(command.RequestDigestHex)
	if err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	exactTargetSetDigest, err := ParseDigest(command.ExactTargetSetDigestHex)
	if err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	calculatedDigest, err := SaveRequestDigest(
		command.UserID, command.ProjectID, command.ExpectedProjectVersion, command.StoryboardPreviewRef,
		command.ToolCallID, command.PromptVersion, command.ValidatorVersion, command.ExactSetValidatorVersion,
		exactTargetSetDigest, command.Content,
	)
	if err != nil || calculatedDigest != providedDigest {
		return SaveResult{}, ErrInvalidInput
	}
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	draftID, err := service.ids.New()
	if err != nil || !CanonicalUUIDv7(draftID) {
		return SaveResult{}, ErrPersistence
	}
	receiptID, err := service.ids.New()
	if err != nil || !CanonicalUUIDv7(receiptID) {
		return SaveResult{}, ErrPersistence
	}
	now := service.clock.Now().UTC()
	if now.IsZero() {
		return SaveResult{}, ErrPersistence
	}
	draft := Draft{
		ID: draftID, ProjectID: command.ProjectID, UserID: command.UserID,
		StoryboardPreviewRef: command.StoryboardPreviewRef, Status: DraftStatus, Version: InitialDraftVersion,
		SchemaVersion: DraftSchemaVersion, Content: cloneContent(command.Content), ContentDigest: contentDigest,
		ExactTargetSetDigest: exactTargetSetDigest, SourceToolCallID: command.ToolCallID,
		SourcePromptVersion: command.PromptVersion, SourceValidatorVersion: command.ValidatorVersion,
		SourceExactSetValidatorVersion: command.ExactSetValidatorVersion, CreatedAt: now, UpdatedAt: now,
	}
	receipt := CommandReceipt{
		ID: receiptID, CommandID: command.CommandID, RequestDigest: providedDigest,
		UserID: command.UserID, ProjectID: command.ProjectID, ExpectedProjectVersion: command.ExpectedProjectVersion,
		StoryboardPreviewRef: command.StoryboardPreviewRef, SourceToolCallID: command.ToolCallID,
		SourcePromptVersion: command.PromptVersion, SourceValidatorVersion: command.ValidatorVersion,
		SourceExactSetValidatorVersion: command.ExactSetValidatorVersion, ExactTargetSetDigest: exactTargetSetDigest,
		PromptPreviewID: draftID, ResultVersion: draft.Version, ResultStatus: draft.Status,
		ResultContentDigest: contentDigest, CreatedAt: now,
	}
	aggregate := SaveAggregate{Draft: draft, Receipt: receipt}
	if err := ValidateAggregate(aggregate); err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	result, err := service.repository.SaveDraft(ctx, aggregate)
	if err != nil {
		return SaveResult{}, err
	}
	if err := validateSaveResult(result, command); err != nil {
		return SaveResult{}, ErrPersistence
	}
	return result, nil
}

// QueryCommand 校验原 command_id、digest、user 与 Project 后查询首次冻结结果。
func (service *Service) QueryCommand(ctx context.Context, commandID string, requestDigestHex string, userID string, projectID string) (QueryResult, error) {
	if ctx == nil || !CanonicalUUIDv7(commandID) || !CanonicalUUIDv7(userID) || !CanonicalUUIDv7(projectID) {
		return QueryResult{}, ErrInvalidInput
	}
	digest, err := ParseDigest(requestDigestHex)
	if err != nil {
		return QueryResult{}, ErrInvalidInput
	}
	result, err := service.repository.QueryCommand(ctx, QueryCommand{
		CommandID: commandID, RequestDigest: digest, UserID: userID, ProjectID: projectID,
	})
	if err != nil {
		return QueryResult{}, err
	}
	if result.Status != QueryStatusNotFound && result.Status != QueryStatusCompleted && result.Status != QueryStatusConflict {
		return QueryResult{}, ErrPersistence
	}
	if result.Status == QueryStatusCompleted && result.Draft == nil {
		return QueryResult{}, ErrPersistence
	}
	if result.Status != QueryStatusCompleted && result.Draft != nil {
		return QueryResult{}, ErrPersistence
	}
	if result.Draft != nil {
		if err := ValidateDraft(*result.Draft); err != nil || result.Draft.UserID != userID || result.Draft.ProjectID != projectID {
			return QueryResult{}, ErrPersistence
		}
	}
	return result, nil
}

// validateSaveResult 校验 Repository 保存或重放结果仍绑定原命令的可信身份、Source、Validator 和内容。
func validateSaveResult(result SaveResult, command SaveCommand) error {
	if result.Disposition != CommandDispositionCreated && result.Disposition != CommandDispositionReplayed {
		return ErrPersistence
	}
	if err := ValidateDraft(result.Draft); err != nil || result.Draft.UserID != command.UserID ||
		result.Draft.ProjectID != command.ProjectID || result.Draft.StoryboardPreviewRef != command.StoryboardPreviewRef ||
		result.Draft.SourceToolCallID != command.ToolCallID || result.Draft.SourcePromptVersion != command.PromptVersion ||
		result.Draft.SourceValidatorVersion != command.ValidatorVersion ||
		result.Draft.SourceExactSetValidatorVersion != command.ExactSetValidatorVersion ||
		result.Draft.ExactTargetSetDigest.Hex() != command.ExactTargetSetDigestHex {
		return ErrPersistence
	}
	providedDigest, err := ParseDigest(command.RequestDigestHex)
	if err != nil {
		return ErrPersistence
	}
	calculatedDigest, err := SaveRequestDigest(
		command.UserID, command.ProjectID, command.ExpectedProjectVersion, command.StoryboardPreviewRef,
		command.ToolCallID, command.PromptVersion, command.ValidatorVersion, command.ExactSetValidatorVersion,
		result.Draft.ExactTargetSetDigest, result.Draft.Content,
	)
	if err != nil || providedDigest != calculatedDigest {
		return ErrPersistence
	}
	return nil
}
