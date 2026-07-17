package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/FigoGoo/Dora-Agent/business/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mediaPreviewPromptSourceSQL = `
SELECT
    prompt_record.id,
    prompt_record.version,
    prompt_record.content_json,
    prompt_record.content_digest
FROM business.project AS project_record
JOIN business.prompt_preview_draft AS prompt_record
  ON prompt_record.project_id = project_record.id
 AND prompt_record.user_id = project_record.owner_user_id
WHERE project_record.id = ?
  AND project_record.owner_user_id = ?
  AND project_record.lifecycle_status = 'active'
  AND prompt_record.id = ?
  AND prompt_record.status = 'draft'
  AND prompt_record.schema_version = 'prompt.preview.draft.v1'
FOR SHARE OF project_record, prompt_record`

const mediaPreviewImageSourceSQL = `
SELECT asset_record.*
FROM business.project AS project_record
JOIN business.media_preview_asset AS asset_record
  ON asset_record.project_id = project_record.id
 AND asset_record.owner_user_id = project_record.owner_user_id
WHERE project_record.id = ?
  AND project_record.owner_user_id = ?
  AND project_record.lifecycle_status = 'active'
  AND asset_record.id = ?
FOR SHARE OF project_record, asset_record`

const mediaPreviewReadyContentSQL = `
SELECT asset_record.*
FROM business.project AS project_record
JOIN business.media_preview_asset AS asset_record
  ON asset_record.project_id = project_record.id
 AND asset_record.owner_user_id = project_record.owner_user_id
WHERE project_record.id = ?
  AND project_record.owner_user_id = ?
  AND project_record.lifecycle_status IN ('active', 'archived')
  AND asset_record.id = ?
  AND asset_record.status = 'ready'`

// MediaPreviewRepository 使用 Business PostgreSQL 与受控本地对象根实现媒体预览权威边界。
type MediaPreviewRepository struct {
	db    *gorm.DB
	store mediapreview.ArtifactStore
}

var _ mediapreview.Repository = (*MediaPreviewRepository)(nil)

// NewMediaPreviewRepository 校验 PostgreSQL Client 与对象根后创建 Repository。
func NewMediaPreviewRepository(client *Client, store mediapreview.ArtifactStore) (*MediaPreviewRepository, error) {
	if client == nil || client.db == nil || store == nil {
		return nil, errors.New("create media preview repository: required dependency is nil")
	}
	return &MediaPreviewRepository{db: client.db, store: store}, nil
}

// Prepare 校验 Project Owner 和 exact Source，再 first-write-wins 创建 reserved Asset 与 Preparation Receipt。
func (repository *MediaPreviewRepository) Prepare(ctx context.Context, command mediapreview.PrepareCommand, allocation mediapreview.PreparationAllocation) (mediapreview.PrepareResult, error) {
	if ctx == nil || command.Validate() != nil || allocation.ValidateFor(command) != nil {
		return mediapreview.PrepareResult{}, mediapreview.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.PrepareResult{}, err
	}
	var result mediapreview.PrepareResult
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		source, err := repository.loadPreparationSource(transactionDB, command)
		if err != nil {
			return err
		}
		assetModel, receiptModel, err := mediaPreviewPreparationModels(command, allocation, source)
		if err != nil {
			return err
		}
		inserted := transactionDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&receiptModel)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			existing, loadErr := loadMediaPreviewPreparationByCommand(transactionDB, command.CommandID)
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return mediapreview.ErrIdempotencyConflict
			}
			if loadErr != nil {
				return loadErr
			}
			if !samePrepareSemantic(existing, command) {
				return mediapreview.ErrIdempotencyConflict
			}
			result = mediapreview.PrepareResult{
				Disposition: mediapreview.CommandDispositionReplayed,
				Preparation: existing,
			}
			return nil
		}
		if err := repository.store.EnsurePreparation(receiptModel.StagingObjectKey, receiptModel.FinalObjectKey); err != nil {
			return err
		}
		if err := transactionDB.Create(&assetModel).Error; err != nil {
			return err
		}
		preparation, err := mediaPreviewPreparationEntity(receiptModel, assetModel)
		if err != nil {
			return err
		}
		result = mediapreview.PrepareResult{
			Disposition: mediapreview.CommandDispositionCreated,
			Preparation: preparation,
		}
		return nil
	})
	if err != nil {
		return mediapreview.PrepareResult{}, mapMediaPreviewRepositoryError(err)
	}
	return result, nil
}

// QueryPreparation 按原 command/digest/owner/project 查询首次 Prepare 回执。
func (repository *MediaPreviewRepository) QueryPreparation(ctx context.Context, query mediapreview.PreparationQuery) (mediapreview.PreparationQueryResult, error) {
	if ctx == nil || query.Validate() != nil {
		return mediapreview.PreparationQueryResult{}, mediapreview.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.PreparationQueryResult{}, err
	}
	var receipt mediaPreviewPreparationReceiptModel
	err := repository.db.WithContext(ctx).
		Where("command_id = ? AND owner_user_id = ? AND project_id = ?", query.CommandID, query.OwnerUserID, query.ProjectID).
		Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediapreview.PreparationQueryResult{Status: mediapreview.QueryStatusNotFound}, nil
	}
	if err != nil {
		return mediapreview.PreparationQueryResult{}, mapMediaPreviewRepositoryError(err)
	}
	digest, err := mediapreview.DigestFromBytes(receipt.RequestDigest)
	if err != nil {
		return mediapreview.PreparationQueryResult{}, mediapreview.ErrPersistence
	}
	if digest != query.RequestDigest {
		return mediapreview.PreparationQueryResult{Status: mediapreview.QueryStatusConflict}, nil
	}
	preparation, err := loadMediaPreviewPreparation(repository.db.WithContext(ctx), receipt)
	if err != nil {
		return mediapreview.PreparationQueryResult{}, mapMediaPreviewRepositoryError(err)
	}
	return mediapreview.PreparationQueryResult{Status: mediapreview.QueryStatusCompleted, Preparation: &preparation}, nil
}

// Finalize 锁定 Preparation 与 reserved Asset，first-write-wins 绑定 Job/Fence 并发布终态文件或失败事实。
func (repository *MediaPreviewRepository) Finalize(ctx context.Context, command mediapreview.FinalizeCommand, allocation mediapreview.FinalizationAllocation) (mediapreview.FinalizeResult, error) {
	if ctx == nil || command.Validate() != nil || allocation.Validate() != nil {
		return mediapreview.FinalizeResult{}, mediapreview.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.FinalizeResult{}, err
	}
	var result mediapreview.FinalizeResult
	promoted := false
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		if existing, err := loadMediaPreviewFinalizationByCommand(transactionDB, command.CommandID); err == nil {
			if !sameFinalizeSemantic(existing, command) {
				return mediapreview.ErrIdempotencyConflict
			}
			result = mediapreview.FinalizeResult{
				Disposition:  mediapreview.CommandDispositionReplayed,
				Finalization: existing,
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var receipt mediaPreviewPreparationReceiptModel
		if err := transactionDB.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", command.PreparationID).Take(&receipt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return mediapreview.ErrNotFound
			}
			return err
		}
		if receipt.OperationID != command.OperationID {
			return mediapreview.ErrNotFound
		}
		var asset mediaPreviewAssetModel
		if err := transactionDB.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", receipt.AssetID).Take(&asset).Error; err != nil {
			return err
		}
		preparation, err := mediaPreviewPreparationEntity(receipt, asset)
		if err != nil {
			return err
		}

		if existing, err := loadMediaPreviewFinalizationByPreparation(transactionDB, receipt.ID); err == nil {
			if sameFinalizeSemantic(existing, command) {
				result = mediapreview.FinalizeResult{
					Disposition:  mediapreview.CommandDispositionReplayed,
					Finalization: existing,
				}
				return nil
			}
			return mediapreview.ErrFenceStale
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if asset.Status != mediapreview.StatusReserved {
			return mediapreview.ErrFenceStale
		}
		if command.TerminalStatus == mediapreview.StatusReady && command.Output.MIMEType != asset.MIMEType {
			return mediapreview.ErrArtifactInvalid
		}

		finalizationModel, finalization, err := mediaPreviewFinalizationModel(command, allocation, preparation)
		if err != nil {
			return err
		}
		inserted := transactionDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&finalizationModel)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			existing, loadErr := loadMediaPreviewFinalizationByCommand(transactionDB, command.CommandID)
			if loadErr != nil {
				return mediapreview.ErrIdempotencyConflict
			}
			if !sameFinalizeSemantic(existing, command) {
				return mediapreview.ErrIdempotencyConflict
			}
			result = mediapreview.FinalizeResult{
				Disposition:  mediapreview.CommandDispositionReplayed,
				Finalization: existing,
			}
			return nil
		}

		updates := mediaPreviewAssetTerminalUpdates(command, allocation, preparation.FinalObjectKey)
		if command.TerminalStatus == mediapreview.StatusReady {
			if err := repository.store.Promote(preparation.StagingObjectKey, preparation.FinalObjectKey, *command.Output); err != nil {
				return err
			}
			promoted = true
		}
		updated := transactionDB.Model(&mediaPreviewAssetModel{}).
			Where("id = ? AND status = ?", asset.ID, mediapreview.StatusReserved).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return mediapreview.ErrFenceStale
		}
		result = mediapreview.FinalizeResult{
			Disposition:  mediapreview.CommandDispositionCreated,
			Finalization: finalization,
		}
		return nil
	})
	if err != nil {
		mapped := mapMediaPreviewRepositoryError(err)
		if promoted && !errors.Is(mapped, mediapreview.ErrUnknownOutcome) {
			return mediapreview.FinalizeResult{}, mediapreview.ErrUnknownOutcome
		}
		return mediapreview.FinalizeResult{}, mapped
	}
	return result, nil
}

// QueryFinalization 按原 command/digest/preparation 查询首次 Finalize 终态回执。
func (repository *MediaPreviewRepository) QueryFinalization(ctx context.Context, query mediapreview.FinalizationQuery) (mediapreview.FinalizationQueryResult, error) {
	if ctx == nil || query.Validate() != nil {
		return mediapreview.FinalizationQueryResult{}, mediapreview.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.FinalizationQueryResult{}, err
	}
	var receipt mediaPreviewFinalizationReceiptModel
	err := repository.db.WithContext(ctx).
		Where("command_id = ? AND preparation_id = ?", query.CommandID, query.PreparationID).
		Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediapreview.FinalizationQueryResult{Status: mediapreview.QueryStatusNotFound}, nil
	}
	if err != nil {
		return mediapreview.FinalizationQueryResult{}, mapMediaPreviewRepositoryError(err)
	}
	digest, err := mediapreview.DigestFromBytes(receipt.RequestDigest)
	if err != nil {
		return mediapreview.FinalizationQueryResult{}, mediapreview.ErrPersistence
	}
	if digest != query.RequestDigest {
		return mediapreview.FinalizationQueryResult{Status: mediapreview.QueryStatusConflict}, nil
	}
	finalization, err := mediaPreviewFinalizationEntity(receipt)
	if err != nil {
		return mediapreview.FinalizationQueryResult{}, err
	}
	return mediapreview.FinalizationQueryResult{Status: mediapreview.QueryStatusCompleted, Finalization: &finalization}, nil
}

// OpenReadyContent 在固定 JOIN 中校验 Owner/Project/ready Asset，再在同一安全句柄上复核文件。
func (repository *MediaPreviewRepository) OpenReadyContent(ctx context.Context, query mediapreview.ContentQuery) (mediapreview.ReadyContent, *os.File, error) {
	if ctx == nil || query.Validate() != nil {
		return mediapreview.ReadyContent{}, nil, mediapreview.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.ReadyContent{}, nil, err
	}
	var asset mediaPreviewAssetModel
	loaded := repository.db.WithContext(ctx).Raw(
		mediaPreviewReadyContentSQL, query.ProjectID, query.OwnerUserID, query.AssetID,
	).Scan(&asset)
	if loaded.Error != nil {
		return mediapreview.ReadyContent{}, nil, mapMediaPreviewRepositoryError(loaded.Error)
	}
	if loaded.RowsAffected != 1 {
		return mediapreview.ReadyContent{}, nil, mediapreview.ErrNotFound
	}
	content, err := readyContentFromMediaPreviewAsset(asset)
	if err != nil {
		return mediapreview.ReadyContent{}, nil, err
	}
	file, err := repository.store.OpenVerified(content.ObjectKey, content.Output)
	if err != nil {
		return mediapreview.ReadyContent{}, nil, err
	}
	return content, file, nil
}

type mediaPreviewPromptSourceRecord struct {
	ID            string     `gorm:"column:id"`
	Version       int64      `gorm:"column:version"`
	ContentJSON   jsonbValue `gorm:"column:content_json"`
	ContentDigest []byte     `gorm:"column:content_digest"`
}

func (repository *MediaPreviewRepository) loadPreparationSource(db *gorm.DB, command mediapreview.PrepareCommand) (mediapreview.SourceRef, error) {
	if command.ToolKey == mediapreview.ToolGenerateMedia {
		return loadMediaPreviewPromptSource(db, command)
	}
	var asset mediaPreviewAssetModel
	loaded := db.Raw(mediaPreviewImageSourceSQL, command.ProjectID, command.OwnerUserID, command.ImageAssetSource.ID).Scan(&asset)
	if loaded.Error != nil {
		return mediapreview.SourceRef{}, loaded.Error
	}
	if loaded.RowsAffected != 1 || asset.Status != mediapreview.StatusReady ||
		asset.MediaKind != mediapreview.MediaKindImage || asset.MIMEType != mediapreview.MIMEPNG {
		return mediapreview.SourceRef{}, mediapreview.ErrNotFound
	}
	reference, output, objectKey, err := readyMediaPreviewAssetFacts(asset)
	if err != nil {
		return mediapreview.SourceRef{}, err
	}
	if reference.Version != command.ImageAssetSource.Version || reference.ContentDigest != command.ImageAssetSource.ContentDigest {
		return mediapreview.SourceRef{}, mediapreview.ErrVersionConflict
	}
	if err := repository.store.Verify(objectKey, output); err != nil {
		if errors.Is(err, mediapreview.ErrNotFound) {
			return mediapreview.SourceRef{}, mediapreview.ErrDependencyNotReady
		}
		return mediapreview.SourceRef{}, err
	}
	return mediapreview.SourceRef{
		SourceType: mediapreview.SourceTypeImageAsset, SourceID: reference.AssetID,
		SourceVersion: reference.Version, SourceDigest: reference.ContentDigest,
		SourceObjectKey: objectKey,
	}, nil
}

func loadMediaPreviewPromptSource(db *gorm.DB, command mediapreview.PrepareCommand) (mediapreview.SourceRef, error) {
	var record mediaPreviewPromptSourceRecord
	loaded := db.Raw(mediaPreviewPromptSourceSQL, command.ProjectID, command.OwnerUserID, command.PromptSource.ID).Scan(&record)
	if loaded.Error != nil {
		return mediapreview.SourceRef{}, loaded.Error
	}
	if loaded.RowsAffected != 1 {
		return mediapreview.SourceRef{}, mediapreview.ErrNotFound
	}
	digest, err := mediapreview.DigestFromBytes(record.ContentDigest)
	if err != nil {
		return mediapreview.SourceRef{}, mediapreview.ErrPersistence
	}
	if record.Version != command.PromptSource.Version || digest != command.PromptSource.ContentDigest {
		return mediapreview.SourceRef{}, mediapreview.ErrVersionConflict
	}
	content, err := promptpreview.ParseContentJSON(record.ContentJSON)
	if err != nil {
		return mediapreview.SourceRef{}, mediapreview.ErrPersistence
	}
	calculatedPromptDigest, err := promptpreview.ContentDigest(content)
	if err != nil || !equalMediaPreviewDigest(record.ContentDigest, calculatedPromptDigest.Bytes()) {
		return mediapreview.SourceRef{}, mediapreview.ErrPersistence
	}
	for _, prompt := range content.Prompts {
		if prompt.TargetLocalKey != command.PromptSource.TargetLocalKey {
			continue
		}
		if prompt.MediaKind != mediapreview.MediaKindImage {
			return mediapreview.SourceRef{}, mediapreview.ErrNotFound
		}
		canonical, err := json.Marshal(prompt)
		if err != nil {
			return mediapreview.SourceRef{}, mediapreview.ErrPersistence
		}
		targetDigest, err := mediapreview.TargetDigest(canonical)
		if err != nil {
			return mediapreview.SourceRef{}, err
		}
		return mediapreview.SourceRef{
			SourceType: mediapreview.SourceTypePromptPreview, SourceID: record.ID,
			SourceVersion: record.Version, SourceDigest: digest,
			TargetLocalKey: prompt.TargetLocalKey, TargetDigest: targetDigest,
		}, nil
	}
	return mediapreview.SourceRef{}, mediapreview.ErrNotFound
}

func mediaPreviewPreparationModels(command mediapreview.PrepareCommand, allocation mediapreview.PreparationAllocation, source mediapreview.SourceRef) (mediaPreviewAssetModel, mediaPreviewPreparationReceiptModel, error) {
	if command.Validate() != nil || allocation.ValidateFor(command) != nil || source.Validate() != nil {
		return mediaPreviewAssetModel{}, mediaPreviewPreparationReceiptModel{}, mediapreview.ErrInvalidArgument
	}
	mediaKind, mimeType := mediaPreviewOutputPair(command.ToolKey)
	var targetLocalKey *string
	var targetDigest []byte
	var sourceObjectKey *string
	if source.SourceType == mediapreview.SourceTypePromptPreview {
		targetLocalKey = pointer(source.TargetLocalKey)
		targetDigest = source.TargetDigest.Bytes()
	} else {
		sourceObjectKey = pointer(source.SourceObjectKey)
	}
	asset := mediaPreviewAssetModel{
		ID: allocation.AssetID, OwnerUserID: command.OwnerUserID, ProjectID: command.ProjectID,
		AssetVersion: mediapreview.AssetVersion, Status: mediapreview.StatusReserved,
		MediaKind: mediaKind, MIMEType: mimeType, OutputProfile: command.OutputProfile,
		SourceType: source.SourceType, SourceID: source.SourceID, SourceVersion: source.SourceVersion,
		SourceDigest: source.SourceDigest.Bytes(), TargetLocalKey: targetLocalKey, TargetDigest: targetDigest,
		CreatedAt: allocation.CreatedAt,
	}
	receipt := mediaPreviewPreparationReceiptModel{
		ID: allocation.PreparationID, RequestID: command.RequestID, CommandID: command.CommandID,
		RequestDigest: command.RequestDigest.Bytes(), OperationID: command.OperationID,
		OwnerUserID: command.OwnerUserID, ProjectID: command.ProjectID, ToolKey: command.ToolKey,
		ScopeDigest: command.ScopeDigest.Bytes(), OutputProfile: command.OutputProfile,
		SourceType: source.SourceType, SourceID: source.SourceID, SourceVersion: source.SourceVersion,
		SourceDigest: source.SourceDigest.Bytes(), TargetLocalKey: targetLocalKey, TargetDigest: targetDigest,
		SourceObjectKey: sourceObjectKey, AssetID: allocation.AssetID, AssetVersion: mediapreview.AssetVersion,
		AssetStatus: mediapreview.StatusReserved, MediaKind: mediaKind, MIMEType: mimeType,
		StagingObjectKey: allocation.StagingObjectKey, FinalObjectKey: allocation.FinalObjectKey,
		CreatedAt: allocation.CreatedAt,
	}
	return asset, receipt, nil
}

func loadMediaPreviewPreparationByCommand(db *gorm.DB, commandID string) (mediapreview.Preparation, error) {
	var receipt mediaPreviewPreparationReceiptModel
	if err := db.Where("command_id = ?", commandID).Take(&receipt).Error; err != nil {
		return mediapreview.Preparation{}, err
	}
	return loadMediaPreviewPreparation(db, receipt)
}

func loadMediaPreviewPreparation(db *gorm.DB, receipt mediaPreviewPreparationReceiptModel) (mediapreview.Preparation, error) {
	var asset mediaPreviewAssetModel
	if err := db.Where("id = ?", receipt.AssetID).Take(&asset).Error; err != nil {
		return mediapreview.Preparation{}, err
	}
	return mediaPreviewPreparationEntity(receipt, asset)
}

func mediaPreviewPreparationEntity(receipt mediaPreviewPreparationReceiptModel, asset mediaPreviewAssetModel) (mediapreview.Preparation, error) {
	requestDigest, err := mediapreview.DigestFromBytes(receipt.RequestDigest)
	if err != nil {
		return mediapreview.Preparation{}, mediapreview.ErrPersistence
	}
	scopeDigest, err := mediapreview.DigestFromBytes(receipt.ScopeDigest)
	if err != nil {
		return mediapreview.Preparation{}, mediapreview.ErrPersistence
	}
	sourceDigest, err := mediapreview.DigestFromBytes(receipt.SourceDigest)
	if err != nil {
		return mediapreview.Preparation{}, mediapreview.ErrPersistence
	}
	source := mediapreview.SourceRef{
		SourceType: receipt.SourceType, SourceID: receipt.SourceID,
		SourceVersion: receipt.SourceVersion, SourceDigest: sourceDigest,
	}
	if receipt.TargetLocalKey != nil {
		source.TargetLocalKey = *receipt.TargetLocalKey
		source.TargetDigest, err = mediapreview.DigestFromBytes(receipt.TargetDigest)
		if err != nil {
			return mediapreview.Preparation{}, mediapreview.ErrPersistence
		}
	}
	if receipt.SourceObjectKey != nil {
		source.SourceObjectKey = *receipt.SourceObjectKey
	}
	preparation := mediapreview.Preparation{
		PreparationID: receipt.ID, CommandID: receipt.CommandID, RequestDigest: requestDigest,
		OperationID: receipt.OperationID, OwnerUserID: receipt.OwnerUserID, ProjectID: receipt.ProjectID,
		ToolKey: receipt.ToolKey, ScopeDigest: scopeDigest, OutputProfile: receipt.OutputProfile,
		SourceRef: source,
		AssetRef: mediapreview.AssetRef{
			AssetID: receipt.AssetID, Version: receipt.AssetVersion, Status: receipt.AssetStatus,
			MediaKind: receipt.MediaKind, MIMEType: receipt.MIMEType,
		},
		StagingObjectKey: receipt.StagingObjectKey, FinalObjectKey: receipt.FinalObjectKey,
		CreatedAt: receipt.CreatedAt.UTC(),
	}
	if preparation.Validate() != nil || asset.ID != receipt.AssetID || asset.OwnerUserID != receipt.OwnerUserID ||
		asset.ProjectID != receipt.ProjectID || asset.AssetVersion != receipt.AssetVersion ||
		asset.MediaKind != receipt.MediaKind || asset.MIMEType != receipt.MIMEType ||
		asset.OutputProfile != receipt.OutputProfile || asset.SourceType != receipt.SourceType ||
		asset.SourceID != receipt.SourceID || asset.SourceVersion != receipt.SourceVersion ||
		!equalMediaPreviewDigest(asset.SourceDigest, receipt.SourceDigest) || !asset.CreatedAt.Equal(receipt.CreatedAt) {
		return mediapreview.Preparation{}, mediapreview.ErrPersistence
	}
	return preparation, nil
}

func mediaPreviewFinalizationModel(command mediapreview.FinalizeCommand, allocation mediapreview.FinalizationAllocation, preparation mediapreview.Preparation) (mediaPreviewFinalizationReceiptModel, mediapreview.Finalization, error) {
	if command.Validate() != nil || allocation.Validate() != nil || preparation.Validate() != nil ||
		command.PreparationID != preparation.PreparationID || command.OperationID != preparation.OperationID ||
		allocation.CompletedAt.Before(preparation.CreatedAt) {
		return mediaPreviewFinalizationReceiptModel{}, mediapreview.Finalization{}, mediapreview.ErrInvalidArgument
	}
	assetRef := preparation.AssetRef
	assetRef.Status = command.TerminalStatus
	finalization := mediapreview.Finalization{
		ReceiptID: allocation.ReceiptID, CommandID: command.CommandID, RequestDigest: command.RequestDigest,
		PreparationID: command.PreparationID, OperationID: command.OperationID, BatchID: command.BatchID,
		JobID: command.JobID, AttemptID: command.AttemptID, Fence: command.Fence,
		TerminalStatus: command.TerminalStatus, AssetRef: assetRef, ErrorCode: command.ErrorCode,
		CompletedAt: allocation.CompletedAt,
	}
	model := mediaPreviewFinalizationReceiptModel{
		ID: allocation.ReceiptID, RequestID: command.RequestID, CommandID: command.CommandID,
		RequestDigest: command.RequestDigest.Bytes(), PreparationID: command.PreparationID,
		OperationID: command.OperationID, BatchID: command.BatchID, JobID: command.JobID,
		AttemptID: command.AttemptID, Fence: command.Fence, TerminalStatus: command.TerminalStatus,
		AssetID: assetRef.AssetID, AssetVersion: assetRef.Version, AssetStatus: assetRef.Status,
		MediaKind: assetRef.MediaKind, MIMEType: assetRef.MIMEType, CompletedAt: allocation.CompletedAt,
	}
	if command.TerminalStatus == mediapreview.StatusReady {
		output := *command.Output
		finalization.Output = &output
		finalization.AssetRef.ContentDigest = output.ContentDigest
		finalization.AssetRef.SizeBytes = output.SizeBytes
		model.ContentDigest = output.ContentDigest.Bytes()
		model.SizeBytes = pointer(output.SizeBytes)
		model.Width = pointer(output.Width)
		model.Height = pointer(output.Height)
		if output.MIMEType == mediapreview.MIMEMP4 {
			model.DurationMS = pointer(output.DurationMS)
			model.Codec = pointer(output.Codec)
			model.PixelFormat = pointer(output.PixelFormat)
		}
	} else {
		model.ErrorCode = pointer(command.ErrorCode)
	}
	if finalization.Validate() != nil {
		return mediaPreviewFinalizationReceiptModel{}, mediapreview.Finalization{}, mediapreview.ErrInvalidArgument
	}
	return model, finalization, nil
}

func loadMediaPreviewFinalizationByCommand(db *gorm.DB, commandID string) (mediapreview.Finalization, error) {
	var receipt mediaPreviewFinalizationReceiptModel
	if err := db.Where("command_id = ?", commandID).Take(&receipt).Error; err != nil {
		return mediapreview.Finalization{}, err
	}
	return mediaPreviewFinalizationEntity(receipt)
}

func loadMediaPreviewFinalizationByPreparation(db *gorm.DB, preparationID string) (mediapreview.Finalization, error) {
	var receipt mediaPreviewFinalizationReceiptModel
	if err := db.Where("preparation_id = ?", preparationID).Take(&receipt).Error; err != nil {
		return mediapreview.Finalization{}, err
	}
	return mediaPreviewFinalizationEntity(receipt)
}

func mediaPreviewFinalizationEntity(model mediaPreviewFinalizationReceiptModel) (mediapreview.Finalization, error) {
	requestDigest, err := mediapreview.DigestFromBytes(model.RequestDigest)
	if err != nil {
		return mediapreview.Finalization{}, mediapreview.ErrPersistence
	}
	finalization := mediapreview.Finalization{
		ReceiptID: model.ID, CommandID: model.CommandID, RequestDigest: requestDigest,
		PreparationID: model.PreparationID, OperationID: model.OperationID, BatchID: model.BatchID,
		JobID: model.JobID, AttemptID: model.AttemptID, Fence: model.Fence,
		TerminalStatus: model.TerminalStatus,
		AssetRef: mediapreview.AssetRef{
			AssetID: model.AssetID, Version: model.AssetVersion, Status: model.AssetStatus,
			MediaKind: model.MediaKind, MIMEType: model.MIMEType,
		},
		CompletedAt: model.CompletedAt.UTC(),
	}
	if model.TerminalStatus == mediapreview.StatusReady {
		output, err := outputMetadataFromFinalizationModel(model)
		if err != nil {
			return mediapreview.Finalization{}, err
		}
		finalization.Output = &output
		finalization.AssetRef.ContentDigest = output.ContentDigest
		finalization.AssetRef.SizeBytes = output.SizeBytes
	} else if model.ErrorCode != nil {
		finalization.ErrorCode = *model.ErrorCode
	}
	if finalization.Validate() != nil {
		return mediapreview.Finalization{}, mediapreview.ErrPersistence
	}
	return finalization, nil
}

func outputMetadataFromFinalizationModel(model mediaPreviewFinalizationReceiptModel) (mediapreview.OutputMetadata, error) {
	digest, err := mediapreview.DigestFromBytes(model.ContentDigest)
	if err != nil || model.SizeBytes == nil || model.Width == nil || model.Height == nil {
		return mediapreview.OutputMetadata{}, mediapreview.ErrPersistence
	}
	output := mediapreview.OutputMetadata{
		ContentDigest: digest, SizeBytes: *model.SizeBytes, MIMEType: model.MIMEType,
		Width: *model.Width, Height: *model.Height,
	}
	if model.MIMEType == mediapreview.MIMEMP4 {
		if model.DurationMS == nil || model.Codec == nil || model.PixelFormat == nil {
			return mediapreview.OutputMetadata{}, mediapreview.ErrPersistence
		}
		output.DurationMS = *model.DurationMS
		output.Codec = *model.Codec
		output.PixelFormat = *model.PixelFormat
	}
	if output.Validate() != nil {
		return mediapreview.OutputMetadata{}, mediapreview.ErrPersistence
	}
	return output, nil
}

func readyMediaPreviewAssetFacts(model mediaPreviewAssetModel) (mediapreview.AssetRef, mediapreview.OutputMetadata, string, error) {
	if model.Status != mediapreview.StatusReady || model.ObjectKey == nil || model.SizeBytes == nil ||
		model.Width == nil || model.Height == nil {
		return mediapreview.AssetRef{}, mediapreview.OutputMetadata{}, "", mediapreview.ErrPersistence
	}
	digest, err := mediapreview.DigestFromBytes(model.ContentDigest)
	if err != nil {
		return mediapreview.AssetRef{}, mediapreview.OutputMetadata{}, "", mediapreview.ErrPersistence
	}
	output := mediapreview.OutputMetadata{
		ContentDigest: digest, SizeBytes: *model.SizeBytes, MIMEType: model.MIMEType,
		Width: *model.Width, Height: *model.Height,
	}
	if model.MIMEType == mediapreview.MIMEMP4 {
		if model.DurationMS == nil || model.Codec == nil || model.PixelFormat == nil {
			return mediapreview.AssetRef{}, mediapreview.OutputMetadata{}, "", mediapreview.ErrPersistence
		}
		output.DurationMS = *model.DurationMS
		output.Codec = *model.Codec
		output.PixelFormat = *model.PixelFormat
	}
	reference := mediapreview.AssetRef{
		AssetID: model.ID, Version: model.AssetVersion, Status: model.Status,
		MediaKind: model.MediaKind, MIMEType: model.MIMEType,
		ContentDigest: digest, SizeBytes: *model.SizeBytes,
	}
	if reference.Validate() != nil || output.Validate() != nil || !mediapreview.ValidObjectKey(*model.ObjectKey) {
		return mediapreview.AssetRef{}, mediapreview.OutputMetadata{}, "", mediapreview.ErrPersistence
	}
	return reference, output, *model.ObjectKey, nil
}

func readyContentFromMediaPreviewAsset(model mediaPreviewAssetModel) (mediapreview.ReadyContent, error) {
	reference, output, objectKey, err := readyMediaPreviewAssetFacts(model)
	if err != nil {
		return mediapreview.ReadyContent{}, err
	}
	content := mediapreview.ReadyContent{AssetRef: reference, ObjectKey: objectKey, Output: output}
	if content.Validate() != nil {
		return mediapreview.ReadyContent{}, mediapreview.ErrPersistence
	}
	return content, nil
}

func mediaPreviewAssetTerminalUpdates(command mediapreview.FinalizeCommand, allocation mediapreview.FinalizationAllocation, finalObjectKey string) map[string]any {
	updates := map[string]any{
		"status":               command.TerminalStatus,
		"finalized_job_id":     command.JobID,
		"finalized_attempt_id": command.AttemptID,
		"finalized_fence":      command.Fence,
		"finalized_at":         allocation.CompletedAt,
	}
	if command.TerminalStatus == mediapreview.StatusReady {
		updates["object_key"] = finalObjectKey
		updates["content_digest"] = command.Output.ContentDigest.Bytes()
		updates["size_bytes"] = command.Output.SizeBytes
		updates["width"] = command.Output.Width
		updates["height"] = command.Output.Height
		if command.Output.MIMEType == mediapreview.MIMEMP4 {
			updates["duration_ms"] = command.Output.DurationMS
			updates["codec"] = command.Output.Codec
			updates["pixel_format"] = command.Output.PixelFormat
		}
	} else {
		updates["error_code"] = command.ErrorCode
	}
	return updates
}

func samePrepareSemantic(existing mediapreview.Preparation, command mediapreview.PrepareCommand) bool {
	if existing.CommandID != command.CommandID || existing.RequestDigest != command.RequestDigest ||
		existing.OperationID != command.OperationID || existing.OwnerUserID != command.OwnerUserID ||
		existing.ProjectID != command.ProjectID || existing.ToolKey != command.ToolKey ||
		existing.ScopeDigest != command.ScopeDigest || existing.OutputProfile != command.OutputProfile {
		return false
	}
	if command.ToolKey == mediapreview.ToolGenerateMedia {
		return existing.SourceRef.SourceType == mediapreview.SourceTypePromptPreview &&
			existing.SourceRef.SourceID == command.PromptSource.ID &&
			existing.SourceRef.SourceVersion == command.PromptSource.Version &&
			existing.SourceRef.SourceDigest == command.PromptSource.ContentDigest &&
			existing.SourceRef.TargetLocalKey == command.PromptSource.TargetLocalKey
	}
	return existing.SourceRef.SourceType == mediapreview.SourceTypeImageAsset &&
		existing.SourceRef.SourceID == command.ImageAssetSource.ID &&
		existing.SourceRef.SourceVersion == command.ImageAssetSource.Version &&
		existing.SourceRef.SourceDigest == command.ImageAssetSource.ContentDigest
}

func sameFinalizeSemantic(existing mediapreview.Finalization, command mediapreview.FinalizeCommand) bool {
	if existing.CommandID != command.CommandID || existing.RequestDigest != command.RequestDigest ||
		existing.PreparationID != command.PreparationID || existing.OperationID != command.OperationID ||
		existing.BatchID != command.BatchID || existing.JobID != command.JobID ||
		existing.AttemptID != command.AttemptID || existing.Fence != command.Fence ||
		existing.TerminalStatus != command.TerminalStatus || existing.ErrorCode != command.ErrorCode {
		return false
	}
	if command.Output == nil {
		return existing.Output == nil
	}
	return existing.Output != nil && *existing.Output == *command.Output
}

func mediaPreviewOutputPair(toolKey string) (string, string) {
	if toolKey == mediapreview.ToolGenerateMedia {
		return mediapreview.MediaKindImage, mediapreview.MIMEPNG
	}
	return mediapreview.MediaKindVideo, mediapreview.MIMEMP4
}

func equalMediaPreviewDigest(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func pointer[T any](value T) *T { return &value }

func mapMediaPreviewRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if mapped := mediapreview.MapInfrastructureError(err); !errors.Is(mapped, mediapreview.ErrPersistence) {
		return mapped
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "media_preview_preparation_command_unique", "media_preview_preparation_operation_unique",
			"media_preview_finalization_command_unique":
			return mediapreview.ErrIdempotencyConflict
		case "media_preview_finalization_preparation_unique", "media_preview_finalization_job_unique":
			return mediapreview.ErrFenceStale
		}
	}
	return fmt.Errorf("%w", mediapreview.ErrPersistence)
}

// Compile-time guard：Project 生命周期常量仍需包含媒体 Preview 允许读取的两种状态。
