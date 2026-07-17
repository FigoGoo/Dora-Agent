package textmaterial

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Clock 为文本素材创建提供可测试的 UTC 当前时间。
type Clock interface {
	// Now 返回当前时间；Service 在构造持久化事实前统一转换为 UTC。
	Now() time.Time
}

// IDGenerator 为每个新文本素材生成唯一 Evidence UUIDv7。
type IDGenerator interface {
	// New 返回规范 UUIDv7；失败时不得开始数据库事务。
	New() (string, error)
}

// Service 编排文本素材严格校验、Evidence 身份生成和 Repository 事务边界。
type Service struct {
	repository  Repository
	clock       Clock
	idGenerator IDGenerator
}

// NewService 创建文本素材应用服务并校验所有必需依赖。
func NewService(repository Repository, clock Clock, idGenerator IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || idGenerator == nil {
		return nil, errors.New("create text material service: required dependency is missing")
	}
	return &Service{repository: repository, clock: clock, idGenerator: idGenerator}, nil
}

// Create 校验可信命令，以 Idempotency-Key 固定 asset_id，并原子创建或重放素材。
// 同键异义由 Repository 在 Project Owner 校验后的同一事务内收敛为 ErrIdempotencyConflict。
func (service *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	if ctx == nil || !CanonicalUUIDv7(command.OwnerUserID) || !CanonicalUUIDv7(command.ProjectID) ||
		!CanonicalUUIDv7(command.IdempotencyKey) || !ValidContent(command.Content) {
		return CreateResult{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, err
	}
	evidenceID, err := service.idGenerator.New()
	if err != nil || !CanonicalUUIDv7(evidenceID) {
		return CreateResult{}, fmt.Errorf("generate text material evidence id: %w", ErrPersistence)
	}
	material := TextMaterial{
		AssetID: command.IdempotencyKey, EvidenceID: evidenceID,
		OwnerUserID: command.OwnerUserID, ProjectID: command.ProjectID,
		AssetVersion: AssetVersion, ContentDigest: ContentDigest(command.Content), Content: command.Content,
		CreatedAt: service.clock.Now().UTC(),
	}
	if err := material.Validate(); err != nil {
		return CreateResult{}, err
	}
	return service.repository.CreateOrReplay(ctx, material)
}

// ListOwned 返回当前可信用户在 Project 下最近创建的最多一百条完整文本素材。
func (service *Service) ListOwned(ctx context.Context, ownerUserID string, projectID string) ([]TextMaterial, error) {
	if ctx == nil {
		return nil, ErrInvalidArgument
	}
	query := ListQuery{OwnerUserID: ownerUserID, ProjectID: projectID, Limit: MaxListItems}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return service.repository.ListOwned(ctx, query)
}
