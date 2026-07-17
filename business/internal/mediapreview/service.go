package mediapreview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Clock 为媒体预览回执提供可测试 UTC 时间。
type Clock interface {
	// Now 返回当前时间；Service 在构造持久化事实前统一转 UTC。
	Now() time.Time
}

// IDGenerator 为 Asset、Preparation 与 Finalization 生成 UUIDv7。
type IDGenerator interface {
	// New 返回规范 UUIDv7。
	New() (string, error)
}

// Service 编排媒体 Preview 严格校验、Business 身份分配和 Repository 边界。
type Service struct {
	repository  Repository
	clock       Clock
	idGenerator IDGenerator
}

// NewService 校验依赖后创建媒体预览应用服务。
func NewService(repository Repository, clock Clock, idGenerator IDGenerator) (*Service, error) {
	if repository == nil || clock == nil || idGenerator == nil {
		return nil, errors.New("create media preview service: required dependency is missing")
	}
	return &Service{repository: repository, clock: clock, idGenerator: idGenerator}, nil
}

// Prepare 分配 Business Asset/Receipt 身份和唯一相对对象键，再执行 first-write-wins Prepare。
func (service *Service) Prepare(ctx context.Context, command PrepareCommand) (PrepareResult, error) {
	if ctx == nil || command.Validate() != nil {
		return PrepareResult{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return PrepareResult{}, err
	}
	preparationID, err := service.newID("preparation")
	if err != nil {
		return PrepareResult{}, err
	}
	assetID, err := service.newID("asset")
	if err != nil {
		return PrepareResult{}, err
	}
	stagingKey, finalKey, err := ObjectKeys(assetID, preparationID, command.ToolKey)
	if err != nil {
		return PrepareResult{}, err
	}
	allocation := PreparationAllocation{
		PreparationID:    preparationID,
		AssetID:          assetID,
		StagingObjectKey: stagingKey,
		FinalObjectKey:   finalKey,
		CreatedAt:        service.clock.Now().UTC(),
	}
	if allocation.ValidateFor(command) != nil {
		return PrepareResult{}, fmt.Errorf("allocate media preview preparation: %w", ErrPersistence)
	}
	result, err := service.repository.Prepare(ctx, command, allocation)
	if err != nil {
		return PrepareResult{}, MapInfrastructureError(err)
	}
	if result.Disposition != CommandDispositionCreated && result.Disposition != CommandDispositionReplayed {
		return PrepareResult{}, ErrPersistence
	}
	if result.Preparation.Validate() != nil || result.Preparation.CommandID != command.CommandID ||
		result.Preparation.RequestDigest != command.RequestDigest ||
		result.Preparation.OwnerUserID != command.OwnerUserID || result.Preparation.ProjectID != command.ProjectID {
		return PrepareResult{}, ErrPersistence
	}
	return result, nil
}

// QueryPreparation 以原 command/digest/owner/project 查询 Prepare 权威事实。
func (service *Service) QueryPreparation(ctx context.Context, query PreparationQuery) (PreparationQueryResult, error) {
	if ctx == nil || query.Validate() != nil {
		return PreparationQueryResult{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return PreparationQueryResult{}, err
	}
	result, err := service.repository.QueryPreparation(ctx, query)
	if err != nil {
		return PreparationQueryResult{}, MapInfrastructureError(err)
	}
	if err := validatePreparationQueryResult(result, query); err != nil {
		return PreparationQueryResult{}, err
	}
	return result, nil
}

// Finalize 分配 Business Finalization Receipt 身份并执行文件验证、原子发布和终态提交。
func (service *Service) Finalize(ctx context.Context, command FinalizeCommand) (FinalizeResult, error) {
	if ctx == nil || command.Validate() != nil {
		return FinalizeResult{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return FinalizeResult{}, err
	}
	receiptID, err := service.newID("finalization")
	if err != nil {
		return FinalizeResult{}, err
	}
	allocation := FinalizationAllocation{ReceiptID: receiptID, CompletedAt: service.clock.Now().UTC()}
	if allocation.Validate() != nil {
		return FinalizeResult{}, fmt.Errorf("allocate media preview finalization: %w", ErrPersistence)
	}
	result, err := service.repository.Finalize(ctx, command, allocation)
	if err != nil {
		return FinalizeResult{}, MapInfrastructureError(err)
	}
	if result.Disposition != CommandDispositionCreated && result.Disposition != CommandDispositionReplayed {
		return FinalizeResult{}, ErrPersistence
	}
	if result.Finalization.Validate() != nil || result.Finalization.CommandID != command.CommandID ||
		result.Finalization.RequestDigest != command.RequestDigest ||
		result.Finalization.PreparationID != command.PreparationID {
		return FinalizeResult{}, ErrPersistence
	}
	return result, nil
}

// QueryFinalization 以原 command/digest/preparation 查询 Finalize 权威事实。
func (service *Service) QueryFinalization(ctx context.Context, query FinalizationQuery) (FinalizationQueryResult, error) {
	if ctx == nil || query.Validate() != nil {
		return FinalizationQueryResult{}, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return FinalizationQueryResult{}, err
	}
	result, err := service.repository.QueryFinalization(ctx, query)
	if err != nil {
		return FinalizationQueryResult{}, MapInfrastructureError(err)
	}
	if err := validateFinalizationQueryResult(result, query); err != nil {
		return FinalizationQueryResult{}, err
	}
	return result, nil
}

// OpenReadyContent 返回当前可信 Owner 可读取的 ready 文件事实与已经安全复核的文件句柄。
func (service *Service) OpenReadyContent(ctx context.Context, query ContentQuery) (ReadyContent, *os.File, error) {
	if ctx == nil || query.Validate() != nil {
		return ReadyContent{}, nil, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return ReadyContent{}, nil, err
	}
	content, file, err := service.repository.OpenReadyContent(ctx, query)
	if err != nil {
		return ReadyContent{}, nil, MapInfrastructureError(err)
	}
	if content.Validate() != nil || content.AssetRef.AssetID != query.AssetID || file == nil {
		if file != nil {
			_ = file.Close()
		}
		return ReadyContent{}, nil, ErrPersistence
	}
	return content, file, nil
}

func (service *Service) newID(kind string) (string, error) {
	id, err := service.idGenerator.New()
	if err != nil || !CanonicalUUIDv7(id) {
		return "", fmt.Errorf("generate media preview %s id: %w", kind, ErrPersistence)
	}
	return id, nil
}

func validatePreparationQueryResult(result PreparationQueryResult, query PreparationQuery) error {
	switch result.Status {
	case QueryStatusNotFound, QueryStatusConflict:
		if result.Preparation != nil {
			return ErrPersistence
		}
	case QueryStatusCompleted:
		if result.Preparation == nil || result.Preparation.Validate() != nil ||
			result.Preparation.CommandID != query.CommandID || result.Preparation.RequestDigest != query.RequestDigest ||
			result.Preparation.OwnerUserID != query.OwnerUserID || result.Preparation.ProjectID != query.ProjectID {
			return ErrPersistence
		}
	default:
		return ErrPersistence
	}
	return nil
}

func validateFinalizationQueryResult(result FinalizationQueryResult, query FinalizationQuery) error {
	switch result.Status {
	case QueryStatusNotFound, QueryStatusConflict:
		if result.Finalization != nil {
			return ErrPersistence
		}
	case QueryStatusCompleted:
		if result.Finalization == nil || result.Finalization.Validate() != nil ||
			result.Finalization.CommandID != query.CommandID || result.Finalization.RequestDigest != query.RequestDigest ||
			result.Finalization.PreparationID != query.PreparationID {
			return ErrPersistence
		}
	default:
		return ErrPersistence
	}
	return nil
}
