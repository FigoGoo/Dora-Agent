package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/mediajob"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// mediaPreviewAttemptModel 映射 Worker 私有 Attempt 恢复表，不保存 Agent Job Payload 或路径。
type mediaPreviewAttemptModel struct {
	// AttemptID 是 Agent Claim 使用的 UUIDv7 Attempt 标识。
	AttemptID uuid.UUID `gorm:"column:attempt_id;type:uuid;primaryKey"`
	// ClaimRequestID 是 Agent Claim first-write-wins UUIDv7 请求标识。
	ClaimRequestID uuid.UUID `gorm:"column:claim_request_id;type:uuid"`
	// JobID 是 Agent-owned Job UUIDv7 逻辑标识。
	JobID uuid.UUID `gorm:"column:job_id;type:uuid"`
	// WorkerID 是创建并恢复该 Attempt 的 Worker 实例标识。
	WorkerID string `gorm:"column:worker_id"`
	// Fence 是 Claim 成功后冻结的 Fencing Token。
	Fence *int64 `gorm:"column:fence"`
	// JobType 是 Claim 成功后冻结的媒体 Job 类型。
	JobType *string `gorm:"column:job_type"`
	// ArtifactRequestDigest 是 Claim 成功后冻结的产物请求摘要。
	ArtifactRequestDigest *string `gorm:"column:artifact_request_digest"`
	// Status 是 Worker 私有恢复状态。
	Status string `gorm:"column:status"`
	// FinalizeCommandID 是 Business Finalize first-write-wins 命令标识。
	FinalizeCommandID *uuid.UUID `gorm:"column:finalize_command_id;type:uuid"`
	// FinalizeRequestDigest 是 Business Finalize 语义请求摘要。
	FinalizeRequestDigest *string `gorm:"column:finalize_request_digest"`
	// FinalizeErrorCode 是 failed Finalize first-write-wins 的稳定错误码。
	FinalizeErrorCode *string `gorm:"column:finalize_error_code"`
	// TerminalEventID 是 Agent Terminal Outbox AppendOnce 事件标识。
	TerminalEventID *uuid.UUID `gorm:"column:terminal_event_id;type:uuid"`
	// TerminalStatus 是 succeeded 或 failed。
	TerminalStatus *string `gorm:"column:terminal_status"`
	// TerminalResultDigest 是 Agent Terminal Result 规范摘要。
	TerminalResultDigest *string `gorm:"column:terminal_result_digest"`
	// ErrorCode 是白名单稳定错误码。
	ErrorCode *string `gorm:"column:error_code"`
	// StartedAt 是 Claim Intent 首次持久化时间。
	StartedAt time.Time `gorm:"column:started_at"`
	// UpdatedAt 是 Worker 私有状态最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// FinishedAt 是已确认结束时间。
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

// TableName 返回 Worker-owned Attempt 表全限定名。
func (mediaPreviewAttemptModel) TableName() string { return "worker.media_preview_attempts" }

// mediaPreviewArtifactReceiptModel 映射不可变产物摘要表，刻意不包含 Object Key 或路径字段。
type mediaPreviewArtifactReceiptModel struct {
	// ReceiptID 是 Worker UUIDv7 回执标识。
	ReceiptID uuid.UUID `gorm:"column:receipt_id;type:uuid;primaryKey"`
	// AttemptID 是 Worker Attempt UUIDv7 逻辑标识。
	AttemptID uuid.UUID `gorm:"column:attempt_id;type:uuid"`
	// JobID 是 Agent Job UUIDv7 逻辑标识。
	JobID uuid.UUID `gorm:"column:job_id;type:uuid"`
	// Fence 是生成产物时的 Agent Fencing Token。
	Fence int64 `gorm:"column:fence"`
	// SchemaVersion 是媒体产物回执版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// JobType 是 generate_png 或 assemble_mp4。
	JobType string `gorm:"column:job_type"`
	// GeneratorVersion 是确定性 PNG 算法版本；MP4 为空。
	GeneratorVersion *string `gorm:"column:generator_version"`
	// ArtifactRequestDigest 是产物请求摘要。
	ArtifactRequestDigest string `gorm:"column:artifact_request_digest"`
	// ContentDigest 是产物字节摘要。
	ContentDigest string `gorm:"column:content_digest"`
	// SizeBytes 是产物精确字节数。
	SizeBytes int64 `gorm:"column:size_bytes"`
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string `gorm:"column:mime_type"`
	// Width 是探针宽度。
	Width int `gorm:"column:width"`
	// Height 是探针高度。
	Height int `gorm:"column:height"`
	// DurationMS 是 MP4 时长；PNG 为空。
	DurationMS *int64 `gorm:"column:duration_ms"`
	// Codec 是 MP4 codec；PNG 为空。
	Codec *string `gorm:"column:codec"`
	// PixelFormat 是 MP4 pixel format；PNG 为空。
	PixelFormat *string `gorm:"column:pixel_format"`
	// CreatedAt 是产物验证完成时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Worker-owned Artifact Receipt 表全限定名。
func (mediaPreviewArtifactReceiptModel) TableName() string {
	return "worker.media_preview_artifact_receipts"
}

// mediaPreviewFinalizationObservationModel 映射 Business Finalize 权威查询摘要。
type mediaPreviewFinalizationObservationModel struct {
	// ObservationID 是 Worker UUIDv7 观察标识。
	ObservationID uuid.UUID `gorm:"column:observation_id;type:uuid;primaryKey"`
	// AttemptID 是 Worker Attempt UUIDv7 逻辑标识。
	AttemptID uuid.UUID `gorm:"column:attempt_id;type:uuid"`
	// JobID 是 Agent Job UUIDv7 逻辑标识。
	JobID uuid.UUID `gorm:"column:job_id;type:uuid"`
	// Fence 是 Finalize 使用的 Agent Fencing Token。
	Fence int64 `gorm:"column:fence"`
	// CommandID 是 Business Finalize 命令 UUIDv7。
	CommandID uuid.UUID `gorm:"column:command_id;type:uuid"`
	// RequestDigest 是 Business Finalize 语义摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// PreparationID 是 Business Prepare UUIDv7 逻辑标识。
	PreparationID uuid.UUID `gorm:"column:preparation_id;type:uuid"`
	// QueryStatus 是 not_found、completed 或 conflict。
	QueryStatus string `gorm:"column:query_status"`
	// FinalizationReceiptID 是 Business Finalization Receipt UUIDv7。
	FinalizationReceiptID *uuid.UUID `gorm:"column:finalization_receipt_id;type:uuid"`
	// AssetID 是 Business Media Asset UUIDv7。
	AssetID *uuid.UUID `gorm:"column:asset_id;type:uuid"`
	// AssetVersion 是 Business Asset 版本。
	AssetVersion *int64 `gorm:"column:asset_version"`
	// AssetStatus 是 Business Asset 权威 ready 或 failed 状态。
	AssetStatus *string `gorm:"column:asset_status"`
	// MediaKind 是 Business Asset image 或 video 类型。
	MediaKind *string `gorm:"column:media_kind"`
	// ContentDigest 是 Business 权威产物摘要。
	ContentDigest *string `gorm:"column:content_digest"`
	// SizeBytes 是 Business 权威产物字节数。
	SizeBytes *int64 `gorm:"column:size_bytes"`
	// MIMEType 是 Business 权威产物 MIME。
	MIMEType *string `gorm:"column:mime_type"`
	// ErrorCode 是 conflict 的白名单错误码。
	ErrorCode *string `gorm:"column:error_code"`
	// ObservedAt 是获得权威事实的时间。
	ObservedAt time.Time `gorm:"column:observed_at"`
}

// TableName 返回 Worker-owned Finalization Observation 表全限定名。
func (mediaPreviewFinalizationObservationModel) TableName() string {
	return "worker.media_preview_finalization_observations"
}

// MediaJobRepository 使用 GORM 保存媒体 Preview Worker 私有恢复事实。
type MediaJobRepository struct {
	// db 是 Worker 自有 PostgreSQL GORM 连接，不用于访问 Agent 或 Business 表。
	db *gorm.DB
}

// NewMediaJobRepository 从已校验 Worker Client 构造媒体私有 Repository。
func NewMediaJobRepository(client *Client) (*MediaJobRepository, error) {
	if client == nil || client.db == nil {
		return nil, fmt.Errorf("create media job repository: nil worker postgres client")
	}
	return &MediaJobRepository{db: client.db}, nil
}

// Readiness 验证三个 Worker-owned 媒体恢复表已由版本化 Migration 创建。
func (r *MediaJobRepository) Readiness(ctx context.Context) error {
	var row struct {
		// AttemptsReady 表示 Attempt 恢复表存在。
		AttemptsReady bool `gorm:"column:attempts_ready"`
		// ArtifactsReady 表示 Artifact Receipt 表存在。
		ArtifactsReady bool `gorm:"column:artifacts_ready"`
		// FinalizationsReady 表示 Finalization Observation 表存在。
		FinalizationsReady bool `gorm:"column:finalizations_ready"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			to_regclass('worker.media_preview_attempts') IS NOT NULL AS attempts_ready,
			to_regclass('worker.media_preview_artifact_receipts') IS NOT NULL AS artifacts_ready,
			to_regclass('worker.media_preview_finalization_observations') IS NOT NULL AS finalizations_ready`).
		Scan(&row).Error; err != nil {
		return fmt.Errorf("probe Worker media receipt tables: %w", err)
	}
	if !row.AttemptsReady || !row.ArtifactsReady || !row.FinalizationsReady {
		return fmt.Errorf("Worker media receipt tables are incomplete; run worker migrations")
	}
	return nil
}

// CreateClaimIntent 在 Agent Claim 前插入稳定 Attempt/Claim Request；重复 ID 由数据库唯一约束拒绝。
func (r *MediaJobRepository) CreateClaimIntent(ctx context.Context, intent mediajob.ClaimIntent) error {
	model := mediaPreviewAttemptModel{
		AttemptID: intent.AttemptID, ClaimRequestID: intent.ClaimRequestID, JobID: intent.JobID,
		WorkerID: intent.WorkerID, Status: string(mediajob.AttemptStatusClaimPending),
		StartedAt: intent.StartedAt, UpdatedAt: intent.StartedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("create media claim intent: %w", err)
	}
	return nil
}

// NextRecoverableClaim 以固定排序返回当前 Worker 一个未结束 Attempt，不循环执行 SQL。
func (r *MediaJobRepository) NextRecoverableClaim(ctx context.Context, workerID string) (mediajob.ClaimIntent, bool, error) {
	statuses := []string{
		string(mediajob.AttemptStatusClaimPending), string(mediajob.AttemptStatusClaimUnknown),
		string(mediajob.AttemptStatusRunning), string(mediajob.AttemptStatusArtifactReady),
		string(mediajob.AttemptStatusFinalizeUnknown), string(mediajob.AttemptStatusReconciling),
		string(mediajob.AttemptStatusTerminalUnknown),
	}
	var model mediaPreviewAttemptModel
	err := r.db.WithContext(ctx).Select("attempt_id", "claim_request_id", "job_id", "worker_id", "status", "started_at").
		Where("worker_id = ? AND status IN ?", workerID, statuses).
		Order("updated_at ASC, attempt_id ASC").Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediajob.ClaimIntent{}, false, nil
	}
	if err != nil {
		return mediajob.ClaimIntent{}, false, fmt.Errorf("query recoverable media claim: %w", err)
	}
	return mediajob.ClaimIntent{
		AttemptID: model.AttemptID, ClaimRequestID: model.ClaimRequestID, JobID: model.JobID,
		WorkerID: model.WorkerID, Status: mediajob.AttemptStatus(model.Status), StartedAt: model.StartedAt,
	}, true, nil
}

// MarkClaimUnknown 仅把尚未确认的 Claim 状态更新为 claim_unknown，保留原幂等键。
func (r *MediaJobRepository) MarkClaimUnknown(ctx context.Context, attemptID uuid.UUID, updatedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&mediaPreviewAttemptModel{}).
		Where("attempt_id = ? AND status IN ?", attemptID, []string{"claim_pending", "claim_unknown"}).
		Updates(map[string]any{"status": string(mediajob.AttemptStatusClaimUnknown), "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("mark media claim unknown: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return mediajob.ErrStateConflict
	}
	return nil
}

// MarkClaimed 条件更新原 Claim Intent，并只冻结执行所需摘要而不保存 Envelope Payload。
func (r *MediaJobRepository) MarkClaimed(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, updatedAt time.Time) error {
	jobType := string(envelope.JobType)
	result := r.db.WithContext(ctx).Model(&mediaPreviewAttemptModel{}).
		Where("attempt_id = ? AND job_id = ? AND status IN ?", envelope.AttemptID, envelope.JobID,
			[]string{"claim_pending", "claim_unknown", "running", "artifact_ready", "finalize_unknown", "reconciling", "terminal_unknown"}).
		Updates(map[string]any{
			"fence": envelope.Fence, "job_type": jobType,
			"artifact_request_digest": envelope.ArtifactRequestDigest,
			"status":                  string(mediajob.AttemptStatusRunning), "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("mark media claim succeeded: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return mediajob.ErrStateConflict
	}
	return nil
}

// RecordArtifact 在一个 Worker 事务中 first-write-wins 保存摘要并把 Attempt 置为 artifact_ready。
func (r *MediaJobRepository) RecordArtifact(ctx context.Context, record mediajob.ArtifactRecord) error {
	model := artifactModelFromRecord(record)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model).Error; err != nil {
			return fmt.Errorf("insert media artifact receipt: %w", err)
		}
		var existing mediaPreviewArtifactReceiptModel
		if err := tx.Where("attempt_id = ?", record.AttemptID).Take(&existing).Error; err != nil {
			return fmt.Errorf("query media artifact receipt: %w", err)
		}
		if !sameArtifactModel(existing, model) {
			return mediajob.ErrStateConflict
		}
		result := tx.Model(&mediaPreviewAttemptModel{}).
			Where("attempt_id = ? AND status IN ?", record.AttemptID, []string{"running", "artifact_ready"}).
			Updates(map[string]any{"status": string(mediajob.AttemptStatusArtifactReady), "updated_at": record.CreatedAt})
		if result.Error != nil {
			return fmt.Errorf("mark media artifact ready: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return mediajob.ErrStateConflict
		}
		return nil
	})
}

// EnsureFinalizeIntent 在行锁事务中 first-write-wins 保存或恢复同一 Attempt 的 Finalize 键。
//
// 进程重启可提出新的 CommandID；只要语义 RequestDigest 相同就返回旧 CommandID，异义摘要才冲突。
func (r *MediaJobRepository) EnsureFinalizeIntent(ctx context.Context, proposed mediajob.FinalizeIntent, updatedAt time.Time) (mediajob.FinalizeIntent, error) {
	var resolved mediajob.FinalizeIntent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model mediaPreviewAttemptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("attempt_id", "finalize_command_id", "finalize_request_digest", "finalize_error_code").
			Where("attempt_id = ?", proposed.AttemptID).Take(&model).Error; err != nil {
			return fmt.Errorf("lock media finalize intent: %w", err)
		}
		if model.FinalizeCommandID != nil || model.FinalizeRequestDigest != nil {
			if model.FinalizeCommandID == nil || model.FinalizeRequestDigest == nil || *model.FinalizeRequestDigest != proposed.RequestDigest ||
				valueOrEmpty(model.FinalizeErrorCode) != proposed.ErrorCode {
				return mediajob.ErrStateConflict
			}
			resolved = mediajob.FinalizeIntent{
				AttemptID: proposed.AttemptID, CommandID: *model.FinalizeCommandID,
				RequestDigest: *model.FinalizeRequestDigest, ErrorCode: valueOrEmpty(model.FinalizeErrorCode),
			}
			return nil
		}
		updates := map[string]any{
			"finalize_command_id": proposed.CommandID, "finalize_request_digest": proposed.RequestDigest,
			"finalize_error_code": nil, "updated_at": updatedAt,
		}
		if proposed.ErrorCode != "" {
			updates["finalize_error_code"] = proposed.ErrorCode
		}
		result := tx.Model(&mediaPreviewAttemptModel{}).Where("attempt_id = ?", proposed.AttemptID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("save media finalize intent: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return mediajob.ErrStateConflict
		}
		resolved = proposed
		return nil
	})
	if err != nil {
		return mediajob.FinalizeIntent{}, err
	}
	return resolved, nil
}

// RecordFinalizationObservation 幂等更新同一 command/digest 的最新权威摘要，不保存响应正文。
func (r *MediaJobRepository) RecordFinalizationObservation(ctx context.Context, observation mediajob.FinalizationObservation) error {
	model := finalizationModelFromObservation(observation)
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "command_id"}, {Name: "request_digest"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"query_status", "finalization_receipt_id", "asset_id", "asset_version", "asset_status", "media_kind",
			"content_digest", "size_bytes", "mime_type", "error_code", "observed_at",
		}),
	}).Create(&model).Error
	if err != nil {
		return fmt.Errorf("record media finalization observation: %w", err)
	}
	return nil
}

// LatestFinalizationRecovery 用一次 LEFT JOIN 查询同一 Job 最近旧 Finalize 命令和权威观察。
//
// 查询只覆盖 Worker 自有表并映射专用 DTO，不按 Attempt 循环执行 SQL。
func (r *MediaJobRepository) LatestFinalizationRecovery(ctx context.Context, jobID uuid.UUID) (mediajob.FinalizationRecovery, bool, error) {
	var row struct {
		// CommandID 是旧 Attempt 的 Finalize 命令 UUIDv7。
		CommandID uuid.UUID `gorm:"column:command_id"`
		// RequestDigest 是旧 Finalize 请求摘要。
		RequestDigest string `gorm:"column:request_digest"`
		// QueryStatus 是最近权威观察状态。
		QueryStatus sql.NullString `gorm:"column:query_status"`
		// FinalizationReceiptID 是 completed 的 Business Receipt UUIDv7。
		FinalizationReceiptID uuid.NullUUID `gorm:"column:finalization_receipt_id"`
		// AssetID 是 completed 的 Business Asset UUIDv7。
		AssetID uuid.NullUUID `gorm:"column:asset_id"`
		// AssetVersion 是 completed 的 Asset 版本。
		AssetVersion sql.NullInt64 `gorm:"column:asset_version"`
		// AssetStatus 是 completed 的 ready 或 failed 状态。
		AssetStatus sql.NullString `gorm:"column:asset_status"`
		// MediaKind 是 completed 的 image 或 video 类型。
		MediaKind sql.NullString `gorm:"column:media_kind"`
		// ContentDigest 是 ready Asset 摘要。
		ContentDigest sql.NullString `gorm:"column:content_digest"`
		// SizeBytes 是 ready Asset 字节数。
		SizeBytes sql.NullInt64 `gorm:"column:size_bytes"`
		// MIMEType 是 Asset MIME。
		MIMEType sql.NullString `gorm:"column:mime_type"`
		// ObservationErrorCode 是 conflict 观察错误码。
		ObservationErrorCode sql.NullString `gorm:"column:observation_error_code"`
		// AttemptErrorCode 是旧 Attempt 失败终态错误码。
		AttemptErrorCode sql.NullString `gorm:"column:attempt_error_code"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			attempt.finalize_command_id AS command_id,
			attempt.finalize_request_digest AS request_digest,
			observation.query_status,
			observation.finalization_receipt_id,
			observation.asset_id,
			observation.asset_version,
			observation.asset_status,
			observation.media_kind,
			observation.content_digest,
			observation.size_bytes,
			observation.mime_type,
			observation.error_code AS observation_error_code,
			attempt.finalize_error_code AS attempt_error_code
		FROM worker.media_preview_attempts AS attempt
		LEFT JOIN worker.media_preview_finalization_observations AS observation
		  ON observation.command_id = attempt.finalize_command_id
		 AND observation.request_digest = attempt.finalize_request_digest
		WHERE attempt.job_id = ?
		  AND attempt.finalize_command_id IS NOT NULL
		  AND attempt.finalize_request_digest IS NOT NULL
		ORDER BY attempt.updated_at DESC, observation.observed_at DESC NULLS LAST, attempt.attempt_id DESC
		LIMIT 1`, jobID).Scan(&row).Error
	if err != nil {
		return mediajob.FinalizationRecovery{}, false, fmt.Errorf("query latest media finalization recovery: %w", err)
	}
	if row.CommandID == uuid.Nil {
		return mediajob.FinalizationRecovery{}, false, nil
	}
	recovery := mediajob.FinalizationRecovery{
		CommandID: row.CommandID, RequestDigest: row.RequestDigest,
		QueryStatus: row.QueryStatus.String, ErrorCode: row.ObservationErrorCode.String,
	}
	if recovery.ErrorCode == "" {
		recovery.ErrorCode = row.AttemptErrorCode.String
	}
	if row.QueryStatus.String == "completed" && row.FinalizationReceiptID.Valid && row.AssetID.Valid {
		recovery.Result = &mediajob.FinalizeResultV1{
			CommandID: row.CommandID, FinalizationReceiptID: row.FinalizationReceiptID.UUID,
			AssetRef: mediajob.MediaAssetRefV1{
				AssetID: row.AssetID.UUID, Version: row.AssetVersion.Int64, Status: row.AssetStatus.String,
				MediaKind: row.MediaKind.String, MIMEType: row.MIMEType.String,
				ContentDigest: row.ContentDigest.String, SizeBytes: row.SizeBytes.Int64,
			},
		}
	}
	return recovery, true, nil
}

// EnsureTerminalIntent 在行锁事务中 first-write-wins 保存或恢复 Agent Terminal 事件键。
//
// 重启后的新 EventID 不会覆盖旧 ID；Status/ResultDigest 同义时返回旧 ID，异义才冲突。
func (r *MediaJobRepository) EnsureTerminalIntent(ctx context.Context, proposed mediajob.TerminalIntent, updatedAt time.Time) (mediajob.TerminalIntent, error) {
	var resolved mediajob.TerminalIntent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model mediaPreviewAttemptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("attempt_id", "terminal_event_id", "terminal_status", "terminal_result_digest").
			Where("attempt_id = ?", proposed.AttemptID).Take(&model).Error; err != nil {
			return fmt.Errorf("lock media terminal intent: %w", err)
		}
		if model.TerminalEventID != nil || model.TerminalStatus != nil || model.TerminalResultDigest != nil {
			if model.TerminalEventID == nil || model.TerminalStatus == nil || model.TerminalResultDigest == nil ||
				*model.TerminalStatus != proposed.Status || *model.TerminalResultDigest != proposed.ResultDigest {
				return mediajob.ErrStateConflict
			}
			resolved = mediajob.TerminalIntent{
				AttemptID: proposed.AttemptID, EventID: *model.TerminalEventID,
				Status: *model.TerminalStatus, ResultDigest: *model.TerminalResultDigest,
			}
			return nil
		}
		result := tx.Model(&mediaPreviewAttemptModel{}).Where("attempt_id = ?", proposed.AttemptID).
			Updates(map[string]any{
				"terminal_event_id": proposed.EventID, "terminal_status": proposed.Status,
				"terminal_result_digest": proposed.ResultDigest, "updated_at": updatedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("save media terminal intent: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return mediajob.ErrStateConflict
		}
		resolved = proposed
		return nil
	})
	if err != nil {
		return mediajob.TerminalIntent{}, err
	}
	return resolved, nil
}

// GetTerminalIntent 读取已冻结的 Terminal Event/Status/ResultDigest，供响应丢失后的进程重启核对。
func (r *MediaJobRepository) GetTerminalIntent(ctx context.Context, attemptID uuid.UUID) (mediajob.TerminalIntent, bool, error) {
	var model mediaPreviewAttemptModel
	err := r.db.WithContext(ctx).
		Select("attempt_id", "terminal_event_id", "terminal_status", "terminal_result_digest").
		Where("attempt_id = ?", attemptID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediajob.TerminalIntent{}, false, nil
	}
	if err != nil {
		return mediajob.TerminalIntent{}, false, fmt.Errorf("query media terminal intent: %w", err)
	}
	if model.TerminalEventID == nil && model.TerminalStatus == nil && model.TerminalResultDigest == nil {
		return mediajob.TerminalIntent{}, false, nil
	}
	if model.TerminalEventID == nil || model.TerminalStatus == nil || model.TerminalResultDigest == nil {
		return mediajob.TerminalIntent{}, false, mediajob.ErrStateConflict
	}
	return mediajob.TerminalIntent{
		AttemptID: attemptID, EventID: *model.TerminalEventID,
		Status: *model.TerminalStatus, ResultDigest: *model.TerminalResultDigest,
	}, true, nil
}

// UpdateAttemptStatus 使用明确旧状态集合做条件更新，RowsAffected=0 返回状态冲突。
func (r *MediaJobRepository) UpdateAttemptStatus(ctx context.Context, attemptID uuid.UUID, allowedFrom []mediajob.AttemptStatus, to mediajob.AttemptStatus, errorCode string, updatedAt time.Time, finishedAt *time.Time) error {
	from := make([]string, len(allowedFrom))
	for index, status := range allowedFrom {
		from[index] = string(status)
	}
	updates := map[string]any{"status": string(to), "updated_at": updatedAt, "finished_at": finishedAt}
	if errorCode == "" {
		updates["error_code"] = nil
	} else {
		updates["error_code"] = errorCode
	}
	result := r.db.WithContext(ctx).Model(&mediaPreviewAttemptModel{}).
		Where("attempt_id = ? AND status IN ?", attemptID, from).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update media attempt status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return mediajob.ErrStateConflict
	}
	return nil
}

// CountAttempts 使用单条聚合 SQL 返回同一 Agent Job 的 Worker Attempt 数量。
func (r *MediaJobRepository) CountAttempts(ctx context.Context, jobID uuid.UUID) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&mediaPreviewAttemptModel{}).Where("job_id = ?", jobID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count media job attempts: %w", err)
	}
	return int(count), nil
}

// artifactModelFromRecord 把内存回执显式映射为不含 ObjectKey 的 Worker GORM Model。
func artifactModelFromRecord(record mediajob.ArtifactRecord) mediaPreviewArtifactReceiptModel {
	receipt := record.Receipt
	model := mediaPreviewArtifactReceiptModel{
		ReceiptID: record.ReceiptID, AttemptID: record.AttemptID, JobID: receipt.JobID, Fence: receipt.Fence,
		SchemaVersion: receipt.SchemaVersion, JobType: string(receipt.JobType),
		ArtifactRequestDigest: receipt.ArtifactRequestDigest, ContentDigest: receipt.ContentDigest,
		SizeBytes: receipt.SizeBytes, MIMEType: receipt.MIMEType, Width: receipt.Width, Height: receipt.Height,
		CreatedAt: record.CreatedAt,
	}
	if receipt.GeneratorVersion != "" {
		value := string(receipt.GeneratorVersion)
		model.GeneratorVersion = &value
	}
	if receipt.DurationMS != 0 {
		model.DurationMS = &receipt.DurationMS
	}
	if receipt.Codec != "" {
		model.Codec = &receipt.Codec
	}
	if receipt.PixelFormat != "" {
		model.PixelFormat = &receipt.PixelFormat
	}
	return model
}

// sameArtifactModel 比较 first-write-wins 产物语义字段，避免同 Attempt 被不同字节覆盖。
func sameArtifactModel(left mediaPreviewArtifactReceiptModel, right mediaPreviewArtifactReceiptModel) bool {
	return left.AttemptID == right.AttemptID && left.JobID == right.JobID && left.Fence == right.Fence &&
		left.SchemaVersion == right.SchemaVersion && left.JobType == right.JobType &&
		left.ArtifactRequestDigest == right.ArtifactRequestDigest && left.ContentDigest == right.ContentDigest &&
		left.SizeBytes == right.SizeBytes && left.MIMEType == right.MIMEType && left.Width == right.Width &&
		left.Height == right.Height && equalOptional(left.GeneratorVersion, right.GeneratorVersion) &&
		equalOptional(left.DurationMS, right.DurationMS) && equalOptional(left.Codec, right.Codec) &&
		equalOptional(left.PixelFormat, right.PixelFormat)
}

// equalOptional 比较可空摘要字段的 nil/value 语义。
func equalOptional[T comparable](left *T, right *T) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// valueOrEmpty 返回可空字符串字段的稳定空值语义。
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// finalizationModelFromObservation 把权威 Finalize 联合响应映射为最小摘要 Model。
func finalizationModelFromObservation(observation mediajob.FinalizationObservation) mediaPreviewFinalizationObservationModel {
	model := mediaPreviewFinalizationObservationModel{
		ObservationID: observation.ObservationID, AttemptID: observation.AttemptID,
		JobID: observation.JobID, Fence: observation.Fence, CommandID: observation.CommandID,
		RequestDigest: observation.RequestDigest, PreparationID: observation.PreparationID,
		QueryStatus: observation.QueryStatus, ObservedAt: observation.ObservedAt,
	}
	if observation.ErrorCode != "" {
		model.ErrorCode = &observation.ErrorCode
	}
	if observation.Result != nil {
		result := observation.Result
		model.FinalizationReceiptID = &result.FinalizationReceiptID
		model.AssetID = &result.AssetRef.AssetID
		model.AssetVersion = &result.AssetRef.Version
		model.AssetStatus = &result.AssetRef.Status
		model.MediaKind = &result.AssetRef.MediaKind
		if result.AssetRef.ContentDigest != "" {
			model.ContentDigest = &result.AssetRef.ContentDigest
		}
		if result.AssetRef.SizeBytes != 0 {
			model.SizeBytes = &result.AssetRef.SizeBytes
		}
		if result.AssetRef.MIMEType != "" {
			model.MIMEType = &result.AssetRef.MIMEType
		}
	}
	return model
}

var _ mediajob.Repository = (*MediaJobRepository)(nil)
