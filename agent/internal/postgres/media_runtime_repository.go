package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mediaDispatchOutboxSchemaVersion = "media_job.preview.dispatch.v1"

// MediaRuntimeRepository 是 media.runtime.v3preview1 的 Operation/Batch/Job/Outbox 唯一 Agent Adapter。
// 它不执行 Migration、不调用 Business/Worker，也不持久化 Prompt、绝对路径、URL 或执行参数。
type MediaRuntimeRepository struct {
	db  *gorm.DB
	ids mediapreview.IDGenerator
}

// NewMediaRuntimeRepository 创建启动期固定的 PostgreSQL Adapter。
func NewMediaRuntimeRepository(client *Client, ids mediapreview.IDGenerator) (*MediaRuntimeRepository, error) {
	if client == nil || client.db == nil || ids == nil {
		return nil, fmt.Errorf("create media runtime repository: client and id generator are required")
	}
	return &MediaRuntimeRepository{db: client.db, ids: ids}, nil
}

// EnsureOperation 按 tool_call_id + scope_digest first-write-wins 创建或恢复一组稳定 Operation/Batch/Job ID。
func (r *MediaRuntimeRepository) EnsureOperation(
	ctx context.Context,
	command mediapreview.EnsureOperationCommand,
) (mediapreview.Operation, error) {
	if mediapreview.ValidateEnsureOperationCommand(command) != nil {
		return mediapreview.Operation{}, mediapreview.ErrInvalidArgument
	}
	if existing, err := r.findOperationByToolCall(ctx, command.TrustedContext.ToolCallID); err != nil || existing != nil {
		if err != nil {
			return mediapreview.Operation{}, err
		}
		return replayMediaOperation(*existing, command)
	}

	var result mediapreview.Operation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
			"media-preview-operation:"+command.TrustedContext.ToolCallID,
		).Error; err != nil {
			return err
		}
		var existing mediaPreviewOperationModel
		lookupErr := tx.Where("tool_call_id = ?", command.TrustedContext.ToolCallID).Take(&existing).Error
		switch {
		case lookupErr == nil:
			replayed, err := replayMediaOperation(existing, command)
			if err != nil {
				return err
			}
			result = replayed
			return nil
		case !errors.Is(lookupErr, gorm.ErrRecordNotFound):
			return lookupErr
		}

		generated := make([]string, 6)
		for index := range generated {
			value, err := r.ids.New()
			if err != nil {
				return fmt.Errorf("generate media runtime UUIDv7: %w", err)
			}
			if !mediapreview.ValidUUIDv7(value) {
				return fmt.Errorf("generate media runtime UUIDv7: generator returned invalid value")
			}
			generated[index] = value
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		model := mediaPreviewOperationModel{
			OperationID: generated[0], ToolCallID: command.TrustedContext.ToolCallID,
			ScopeDigest: command.ScopeDigest, ToolKey: command.ToolKey, OutputProfile: command.OutputProfile,
			SessionID: command.TrustedContext.SessionID, UserID: command.TrustedContext.UserID,
			ProjectID: command.TrustedContext.ProjectID, InputID: command.TrustedContext.InputID,
			TurnID: command.TrustedContext.TurnID, RunID: command.TrustedContext.RunID,
			PlannedBatchID: generated[1], PlannedJobID: generated[2], PlannedDispatchEventID: generated[3],
			PreparationRequestID: generated[4], PreparationCommandID: generated[5],
			Status: mediapreview.OperationStatusPreparing, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&model).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		result = operationFromMediaModel(model, false)
		return nil
	})
	if err != nil {
		return mediapreview.Operation{}, err
	}
	return result, nil
}

// FreezePreparationRequest 在任何 Business 副作用前冻结原 command/request digest；同键异义冲突。
func (r *MediaRuntimeRepository) FreezePreparationRequest(
	ctx context.Context,
	operationID string,
	request mediapreview.PrepareRequest,
) error {
	if !mediapreview.ValidUUIDv7(operationID) || operationID != request.OperationID ||
		mediapreview.ValidatePrepareRequest(request) != nil {
		return mediapreview.ErrInvalidArgument
	}
	encoded, err := mediapreview.CanonicalJSON(request)
	if err != nil {
		return fmt.Errorf("encode media preparation request: %w", err)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation mediaPreviewOperationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_id = ?", operationID).Take(&operation).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		if operation.ScopeDigest != request.ScopeDigest || operation.ToolKey != request.ToolKey ||
			operation.OutputProfile != request.OutputProfile || operation.UserID != request.UserID ||
			operation.ProjectID != request.ProjectID || operation.PreparationRequestID != request.RequestID ||
			operation.PreparationCommandID != request.CommandID {
			return mediapreview.ErrIdempotencyConflict
		}
		if operation.PreparationRequestDigest != nil {
			if *operation.PreparationRequestDigest != request.RequestDigest {
				return mediapreview.ErrIdempotencyConflict
			}
			return nil
		}
		if operation.Status != mediapreview.OperationStatusPreparing && operation.Status != mediapreview.OperationStatusRecoveryPending {
			return mediapreview.ErrVersionConflict
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"preparation_request_digest": request.RequestDigest,
			"preparation_request":        string(encoded),
			"updated_at":                 now,
			"version":                    gorm.Expr("version + 1"),
		}
		result := tx.Model(&mediaPreviewOperationModel{}).
			Where("operation_id = ? AND preparation_request_digest IS NULL", operationID).
			Updates(updates)
		if result.Error != nil {
			return mapMediaRepositoryError(result.Error)
		}
		if result.RowsAffected != 1 {
			return mediapreview.ErrVersionConflict
		}
		return nil
	})
}

// RecordPreparation 冻结 Business 权威 Prepare 回执；同 command 同义重放不改写首值。
func (r *MediaRuntimeRepository) RecordPreparation(
	ctx context.Context,
	operationID string,
	response mediapreview.PrepareResult,
) error {
	if !mediapreview.ValidUUIDv7(operationID) {
		return mediapreview.ErrInvalidArgument
	}
	encoded, err := mediapreview.CanonicalJSON(response)
	if err != nil {
		return fmt.Errorf("encode media preparation response: %w", err)
	}
	responseDigest, err := mediapreview.DigestJSON(response)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation mediaPreviewOperationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_id = ?", operationID).Take(&operation).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		request, err := mediaPrepareRequestFromOperation(operation)
		if err != nil || mediapreview.ValidatePrepareResult(response, request) != nil {
			return mediapreview.ErrInvalidArgument
		}
		if operation.PreparationID != nil {
			if *operation.PreparationID != response.PreparationID || operation.PreparationResponseDigest == nil ||
				*operation.PreparationResponseDigest != responseDigest {
				return mediapreview.ErrIdempotencyConflict
			}
			return nil
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		result := tx.Model(&mediaPreviewOperationModel{}).
			Where("operation_id = ? AND preparation_id IS NULL", operationID).
			Updates(map[string]any{
				"preparation_id":              response.PreparationID,
				"preparation_response_digest": responseDigest,
				"preparation_response":        string(encoded),
				"updated_at":                  now,
				"version":                     gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return mapMediaRepositoryError(result.Error)
		}
		if result.RowsAffected != 1 {
			return mediapreview.ErrVersionConflict
		}
		return nil
	})
}

// Dispatch 原子创建单 Batch、单 Job、Dispatch Outbox 并把 Operation 置 accepted。
func (r *MediaRuntimeRepository) Dispatch(
	ctx context.Context,
	command mediapreview.DispatchCommand,
) (mediapreview.DispatchReceipt, error) {
	if mediapreview.ValidateDispatchCommand(command) != nil {
		return mediapreview.DispatchReceipt{}, mediapreview.ErrInvalidArgument
	}
	var receipt mediapreview.DispatchReceipt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation mediaPreviewOperationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("operation_id = ?", command.Operation.OperationID).Take(&operation).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		if !sameMediaOperationIdentity(operation, command.Operation) || operation.PreparationID == nil ||
			*operation.PreparationID != command.Preparation.PreparationID {
			return mediapreview.ErrIdempotencyConflict
		}
		if operation.DispatchDigest != nil {
			if *operation.DispatchDigest != command.DispatchDigest {
				return mediapreview.ErrIdempotencyConflict
			}
			replayed, err := mediaDispatchReceipt(tx, operation, true)
			if err != nil {
				return err
			}
			receipt = replayed
			return nil
		}
		if operation.Status != mediapreview.OperationStatusPreparing && operation.Status != mediapreview.OperationStatusRecoveryPending {
			return mediapreview.ErrVersionConflict
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		if !command.Job.DeadlineAt.After(now) {
			return mediapreview.ErrInvalidArgument
		}
		sourceJSON, err := mediapreview.CanonicalJSON(command.Job.SourceRef)
		if err != nil {
			return err
		}
		targetJSON, err := mediapreview.CanonicalJSON(command.Job.Target)
		if err != nil {
			return err
		}
		batch := mediaPreviewBatchModel{
			BatchID: command.Job.BatchID, OperationID: command.Job.OperationID,
			Status: "accepted", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		job := mediaPreviewJobModel{
			JobID: command.Job.JobID, BatchID: command.Job.BatchID, OperationID: command.Job.OperationID,
			SessionID: command.Job.SessionID, UserID: command.Job.UserID, ProjectID: command.Job.ProjectID,
			JobType: command.Job.JobType, DefinitionVersion: command.Job.DefinitionVersion,
			ScopeDigest: command.Job.ScopeDigest, OutputProfile: command.Job.OutputProfile,
			SourceRef: string(sourceJSON), Target: string(targetJSON),
			ArtifactRequestDigest: command.Job.ArtifactRequestDigest,
			Status:                "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now, DeadlineAt: command.Job.DeadlineAt,
		}
		payload := struct {
			SchemaVersion string `json:"schema_version"`
			JobID         string `json:"job_id"`
			Profile       string `json:"profile"`
		}{SchemaVersion: mediaDispatchOutboxSchemaVersion, JobID: job.JobID, Profile: mediapreview.Profile}
		payloadJSON, err := mediapreview.CanonicalJSON(payload)
		if err != nil {
			return err
		}
		payloadDigest, err := mediapreview.DigestJSON(payload)
		if err != nil {
			return err
		}
		outbox := mediaPreviewDispatchOutboxModel{
			EventID: operation.PlannedDispatchEventID, JobID: job.JobID,
			SchemaVersion: mediaDispatchOutboxSchemaVersion, PayloadDigest: payloadDigest,
			Payload: string(payloadJSON), CreatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		if err := tx.Create(&job).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return mapMediaRepositoryError(err)
		}
		result := tx.Model(&mediaPreviewOperationModel{}).
			Where("operation_id = ? AND dispatch_digest IS NULL AND status IN ?", operation.OperationID,
				[]string{mediapreview.OperationStatusPreparing, mediapreview.OperationStatusRecoveryPending}).
			Updates(map[string]any{
				"dispatch_digest":      command.DispatchDigest,
				"status":               mediapreview.OperationStatusAccepted,
				"recovery_reason_code": nil,
				"version":              gorm.Expr("version + 1"),
				"updated_at":           now,
				"accepted_at":          now,
			})
		if result.Error != nil {
			return mapMediaRepositoryError(result.Error)
		}
		if result.RowsAffected != 1 {
			return mediapreview.ErrVersionConflict
		}
		receipt = mediapreview.DispatchReceipt{
			Status: mediapreview.DispatchStatusCommitted, OperationID: operation.OperationID,
			BatchID: batch.BatchID, JobID: job.JobID, DispatchEventID: outbox.EventID,
			AssetRef: command.Preparation.AssetRef,
		}
		return nil
	})
	if err != nil {
		return mediapreview.DispatchReceipt{}, err
	}
	return receipt, nil
}

// QueryDispatch 只查询原 Operation/scope 是否已提交，不创建第二个 Job。
func (r *MediaRuntimeRepository) QueryDispatch(
	ctx context.Context,
	operationID string,
	scopeDigest string,
) (mediapreview.DispatchQueryResult, error) {
	if !mediapreview.ValidUUIDv7(operationID) || !mediapreview.ValidDigest(scopeDigest) {
		return mediapreview.DispatchQueryResult{}, mediapreview.ErrInvalidArgument
	}
	var operation mediaPreviewOperationModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).Take(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediapreview.DispatchQueryResult{Status: mediapreview.DispatchStatusNotFound}, nil
	}
	if err != nil {
		return mediapreview.DispatchQueryResult{}, err
	}
	if operation.ScopeDigest != scopeDigest {
		return mediapreview.DispatchQueryResult{Status: mediapreview.DispatchStatusConflict}, nil
	}
	if operation.DispatchDigest == nil {
		return mediapreview.DispatchQueryResult{Status: mediapreview.DispatchStatusNotFound}, nil
	}
	receipt, err := mediaDispatchReceipt(r.db.WithContext(ctx), operation, true)
	if err != nil {
		return mediapreview.DispatchQueryResult{}, err
	}
	return mediapreview.DispatchQueryResult{Status: mediapreview.DispatchStatusCommitted, Receipt: &receipt}, nil
}

// DeferRecovery 保持同一 Operation/Prepare/Dispatch key 并阻止把 Unknown Outcome 冻结为失败。
func (r *MediaRuntimeRepository) DeferRecovery(ctx context.Context, operationID string, reasonCode string) error {
	if !mediapreview.ValidUUIDv7(operationID) || !validMediaErrorCode(reasonCode) {
		return mediapreview.ErrInvalidArgument
	}
	now, err := mediaDatabaseNow(r.db.WithContext(ctx))
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&mediaPreviewOperationModel{}).
		Where("operation_id = ? AND status IN ?", operationID,
			[]string{mediapreview.OperationStatusPreparing, mediapreview.OperationStatusRecoveryPending}).
		Updates(map[string]any{
			"status": mediapreview.OperationStatusRecoveryPending, "recovery_reason_code": reasonCode,
			"updated_at": now, "version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return mapMediaRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return mediapreview.ErrVersionConflict
	}
	return nil
}

func (r *MediaRuntimeRepository) findOperationByToolCall(
	ctx context.Context,
	toolCallID string,
) (*mediaPreviewOperationModel, error) {
	var model mediaPreviewOperationModel
	err := r.db.WithContext(ctx).Where("tool_call_id = ?", toolCallID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func replayMediaOperation(
	model mediaPreviewOperationModel,
	command mediapreview.EnsureOperationCommand,
) (mediapreview.Operation, error) {
	trusted := command.TrustedContext
	if model.ToolCallID != trusted.ToolCallID || model.ScopeDigest != command.ScopeDigest ||
		model.ToolKey != command.ToolKey || model.OutputProfile != command.OutputProfile ||
		model.SessionID != trusted.SessionID || model.UserID != trusted.UserID || model.ProjectID != trusted.ProjectID ||
		model.InputID != trusted.InputID || model.TurnID != trusted.TurnID || model.RunID != trusted.RunID {
		return mediapreview.Operation{}, mediapreview.ErrIdempotencyConflict
	}
	return operationFromMediaModel(model, true), nil
}

func operationFromMediaModel(model mediaPreviewOperationModel, replayed bool) mediapreview.Operation {
	return mediapreview.Operation{
		OperationID: model.OperationID, BatchID: model.PlannedBatchID, JobID: model.PlannedJobID,
		DispatchEventID: model.PlannedDispatchEventID, PreparationRequestID: model.PreparationRequestID,
		PreparationCommandID: model.PreparationCommandID, ToolKey: model.ToolKey,
		ScopeDigest: model.ScopeDigest, OutputProfile: model.OutputProfile, Status: model.Status, Replayed: replayed,
	}
}

func sameMediaOperationIdentity(model mediaPreviewOperationModel, operation mediapreview.Operation) bool {
	return model.OperationID == operation.OperationID && model.PlannedBatchID == operation.BatchID &&
		model.PlannedJobID == operation.JobID && model.PlannedDispatchEventID == operation.DispatchEventID &&
		model.PreparationRequestID == operation.PreparationRequestID &&
		model.PreparationCommandID == operation.PreparationCommandID && model.ToolKey == operation.ToolKey &&
		model.ScopeDigest == operation.ScopeDigest && model.OutputProfile == operation.OutputProfile
}

func mediaPrepareRequestFromOperation(operation mediaPreviewOperationModel) (mediapreview.PrepareRequest, error) {
	if operation.PreparationRequest == nil {
		return mediapreview.PrepareRequest{}, mediapreview.ErrVersionConflict
	}
	return mediapreview.DecodePrepareRequest([]byte(*operation.PreparationRequest))
}

func mediaPreparationFromOperation(operation mediaPreviewOperationModel) (mediapreview.PrepareResult, error) {
	if operation.PreparationResponse == nil {
		return mediapreview.PrepareResult{}, mediapreview.ErrVersionConflict
	}
	request, err := mediaPrepareRequestFromOperation(operation)
	if err != nil {
		return mediapreview.PrepareResult{}, err
	}
	return mediapreview.DecodePrepareResult([]byte(*operation.PreparationResponse), request)
}

func mediaDispatchReceipt(
	db *gorm.DB,
	operation mediaPreviewOperationModel,
	replayed bool,
) (mediapreview.DispatchReceipt, error) {
	var batch mediaPreviewBatchModel
	if err := db.Where("batch_id = ? AND operation_id = ?", operation.PlannedBatchID, operation.OperationID).
		Take(&batch).Error; err != nil {
		return mediapreview.DispatchReceipt{}, err
	}
	var job mediaPreviewJobModel
	if err := db.Where("job_id = ? AND batch_id = ? AND operation_id = ?", operation.PlannedJobID,
		operation.PlannedBatchID, operation.OperationID).Take(&job).Error; err != nil {
		return mediapreview.DispatchReceipt{}, err
	}
	preparation, err := mediaPreparationFromOperation(operation)
	if err != nil {
		return mediapreview.DispatchReceipt{}, err
	}
	return mediapreview.DispatchReceipt{
		Status: mediapreview.DispatchStatusCommitted, OperationID: operation.OperationID,
		BatchID: batch.BatchID, JobID: job.JobID, DispatchEventID: operation.PlannedDispatchEventID,
		AssetRef: preparation.AssetRef, Replayed: replayed,
	}, nil
}

func mediaDatabaseNow(db *gorm.DB) (time.Time, error) {
	var row struct {
		Now time.Time `gorm:"column:database_now"`
	}
	if err := db.Raw("SELECT clock_timestamp() AS database_now").Scan(&row).Error; err != nil {
		return time.Time{}, err
	}
	if row.Now.IsZero() {
		return time.Time{}, fmt.Errorf("read media runtime database clock: empty result")
	}
	return row.Now.UTC(), nil
}

func validMediaErrorCode(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func mapMediaRepositoryError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediapreview.ErrVersionConflict
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return mediapreview.ErrIdempotencyConflict
	}
	return err
}

var _ mediapreview.Repository = (*MediaRuntimeRepository)(nil)
