package creationspec

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Repository 是 CreationSpec Project 上下文、草稿和命令回执的唯一持久化端口。
type Repository interface {
	// FindOwnedProject 返回当前用户可用的 Project 最小上下文。
	FindOwnedProject(ctx context.Context, userID string, projectID string) (ProjectContext, error)
	// SaveDraft 以 command_id first-write-wins 语义原子保存草稿和回执。
	SaveDraft(ctx context.Context, aggregate SaveAggregate) (SaveResult, error)
	// QueryCommand 只按原 command_id、digest、user 与 project 查询首次结果。
	QueryCommand(ctx context.Context, query QueryCommand) (QueryResult, error)
}

// Clock 为一次保存命令提供可注入的冻结时间。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
}

// IDGenerator 为草稿和命令回执生成应用侧 UUIDv7。
type IDGenerator interface {
	// New 返回新的 UUIDv7 字符串。
	New() (string, error)
}

// ContextQuery 是获取 Project 生成上下文的领域查询。
type ContextQuery struct {
	// UserID 是可信调用用户 UUIDv7。
	UserID string
	// ProjectID 是目标 Business Project UUIDv7。
	ProjectID string
}

// SaveCommand 是保存 CreationSpec Preview 草稿的完整冻结命令。
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
	// ToolCallID 是来源 Graph Tool 调用 UUIDv7。
	ToolCallID string
	// PromptVersion 是来源 Prompt 版本。
	PromptVersion string
	// ValidatorVersion 是来源 Validator 版本。
	ValidatorVersion string
	// Content 是已由 Agent Validator 产生且仍需 Business 严格复核的内容。
	Content Content
}

// QueryCommand 是查询首次命令结果的完整键。
type QueryCommand struct {
	// CommandID 是原保存命令 UUIDv7。
	CommandID string
	// RequestDigest 是原保存命令的小写 SHA-256 摘要。
	RequestDigest Digest
	// UserID 是原保存命令用户 UUIDv7。
	UserID string
	// ProjectID 是原保存命令 Project UUIDv7。
	ProjectID string
}

// Service 编排严格验证、UUIDv7 生成和 Repository 原子边界。
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

// NewService 创建 CreationSpec Preview Service；缺少依赖时失败关闭。
func NewService(repository Repository, clock Clock, ids IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || ids == nil {
		return nil, fmt.Errorf("create creation spec service: dependency is nil")
	}
	return &Service{repository: repository, clock: clock, ids: ids}, nil
}

// GetContext 严格校验 owner 与 Project ID 后读取最小 Project 上下文。
func (service *Service) GetContext(ctx context.Context, query ContextQuery) (ProjectContext, error) {
	if ctx == nil || !CanonicalUUIDv7(query.UserID) || !CanonicalUUIDv7(query.ProjectID) {
		return ProjectContext{}, ErrInvalidInput
	}
	result, err := service.repository.FindOwnedProject(ctx, query.UserID, query.ProjectID)
	if err != nil {
		return ProjectContext{}, err
	}
	if err := ValidateProjectContext(result); err != nil || result.ProjectID != query.ProjectID {
		return ProjectContext{}, ErrPersistence
	}
	return result, nil
}

// SaveDraft 校验请求摘要并构造草稿与回执，由 Repository 实现事务级 first-write-wins。
func (service *Service) SaveDraft(ctx context.Context, command SaveCommand) (SaveResult, error) {
	if ctx == nil || !CanonicalUUIDv7(command.CommandID) {
		return SaveResult{}, ErrInvalidInput
	}
	providedDigest, err := ParseDigest(command.RequestDigestHex)
	if err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	calculatedDigest, err := SaveRequestDigest(
		command.UserID, command.ProjectID, command.ExpectedProjectVersion, command.ToolCallID,
		command.PromptVersion, command.ValidatorVersion, command.Content,
	)
	if err != nil || providedDigest != calculatedDigest {
		return SaveResult{}, ErrInvalidInput
	}
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	creationSpecID, err := service.ids.New()
	if err != nil || !CanonicalUUIDv7(creationSpecID) {
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
		ID: creationSpecID, ProjectID: command.ProjectID, UserID: command.UserID,
		Status: DraftStatus, Version: InitialDraftVersion, SchemaVersion: DraftSchemaVersion,
		Content: command.Content, ContentDigest: contentDigest, SourceToolCallID: command.ToolCallID,
		SourcePromptVersion: command.PromptVersion, SourceValidatorVersion: command.ValidatorVersion,
		CreatedAt: now, UpdatedAt: now,
	}
	receipt := CommandReceipt{
		ID: receiptID, CommandID: command.CommandID, RequestDigest: providedDigest,
		UserID: command.UserID, ProjectID: command.ProjectID, ExpectedProjectVersion: command.ExpectedProjectVersion,
		SourceToolCallID: command.ToolCallID, SourcePromptVersion: command.PromptVersion,
		SourceValidatorVersion: command.ValidatorVersion, CreationSpecID: creationSpecID,
		ResultVersion: draft.Version, ResultStatus: draft.Status, ResultContentDigest: contentDigest, CreatedAt: now,
	}
	aggregate := SaveAggregate{Draft: draft, Receipt: receipt}
	if err := ValidateAggregate(aggregate); err != nil {
		return SaveResult{}, ErrInvalidInput
	}
	result, err := service.repository.SaveDraft(ctx, aggregate)
	if err != nil {
		return SaveResult{}, err
	}
	if err := ValidateSaveResult(result, command); err != nil {
		return SaveResult{}, ErrPersistence
	}
	return result, nil
}

// QueryCommand 严格校验原 command_id、digest、user 与 Project 后查询冻结结果。
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

// IsStableError 判断错误是否已经属于允许透传到 RPC 映射层的稳定领域分类。
func IsStableError(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrVersionConflict) ||
		errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrPersistence) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
