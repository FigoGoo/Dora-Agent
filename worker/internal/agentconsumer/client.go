// Package agentconsumer 消费 Agent 发布的 media_job_preview_v1 PostgreSQL View/Functions。
package agentconsumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediajob"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Client 使用专用最小权限 DSN 调用 Agent Preview 持久化消费契约。
type Client struct {
	// db 是只允许访问 Agent Preview View/Functions 的 GORM 连接。
	db *gorm.DB
}

// Open 创建 Agent Consumer GORM 连接池并完成有界 Ping，不执行 Migration 或普通表查询。
func Open(ctx context.Context, cfg config.MediaRuntimeConfig) (*Client, error) {
	db, err := gorm.Open(postgres.Open(cfg.AgentConsumerDSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open Agent media consumer: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get Agent media consumer pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.AgentMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.AgentMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.AgentConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.AgentConnMaxIdleTime)
	pingContext, cancel := context.WithTimeout(ctx, cfg.AgentPingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingContext); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping Agent media consumer: %w", err)
	}
	return &Client{db: db}, nil
}

// Readiness 验证 claimable View 和全部冻结函数签名存在；任一缺失时 Worker 不得 Ready。
func (c *Client) Readiness(ctx context.Context) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("Agent media consumer is nil")
	}
	var row struct {
		// ViewReady 表示最小 claimable View 存在。
		ViewReady bool `gorm:"column:view_ready"`
		// ClaimReady 表示 claim 函数签名存在。
		ClaimReady bool `gorm:"column:claim_ready"`
		// RenewReady 表示 renew 函数签名存在。
		RenewReady bool `gorm:"column:renew_ready"`
		// RetryReady 表示 schedule_retry 函数签名存在。
		RetryReady bool `gorm:"column:retry_ready"`
		// ReconcileReady 表示 mark_reconciling 函数签名存在。
		ReconcileReady bool `gorm:"column:reconcile_ready"`
		// TerminalReady 表示 commit_terminal 函数签名存在。
		TerminalReady bool `gorm:"column:terminal_ready"`
		// GetReady 表示 get 函数签名存在。
		GetReady bool `gorm:"column:get_ready"`
	}
	err := c.db.WithContext(ctx).Raw(`
		SELECT
			to_regclass('agent.media_job_preview_v1_claimable') IS NOT NULL AS view_ready,
			to_regprocedure('agent.media_job_preview_v1_claim(uuid,text,uuid,uuid,integer)') IS NOT NULL AS claim_ready,
			to_regprocedure('agent.media_job_preview_v1_renew(uuid,text,uuid,bigint,integer)') IS NOT NULL AS renew_ready,
			to_regprocedure('agent.media_job_preview_v1_schedule_retry(uuid,text,uuid,bigint,integer,text)') IS NOT NULL AS retry_ready,
			to_regprocedure('agent.media_job_preview_v1_mark_reconciling(uuid,text,uuid,bigint,text)') IS NOT NULL AS reconcile_ready,
			to_regprocedure('agent.media_job_preview_v1_commit_terminal(uuid,text,uuid,bigint,uuid,text,text,text,jsonb)') IS NOT NULL AS terminal_ready,
			to_regprocedure('agent.media_job_preview_v1_get(uuid)') IS NOT NULL AS get_ready`).Scan(&row).Error
	if err != nil {
		return fmt.Errorf("probe Agent media consumer contract: %w", err)
	}
	if !row.ViewReady || !row.ClaimReady || !row.RenewReady || !row.RetryReady ||
		!row.ReconcileReady || !row.TerminalReady || !row.GetReady {
		return fmt.Errorf("Agent media consumer contract is incomplete")
	}
	return nil
}

// NextClaimable 只读取 View 最小列并稳定排序返回一个 Job，避免单 Job 函数被包装为批量 N+1。
func (c *Client) NextClaimable(ctx context.Context) (uuid.UUID, bool, error) {
	var row struct {
		// JobID 是一个到期 pending/retry_wait Job UUIDv7。
		JobID uuid.UUID `gorm:"column:job_id"`
	}
	err := c.db.WithContext(ctx).Raw(`
		SELECT job_id
		FROM agent.media_job_preview_v1_claimable
		ORDER BY available_at ASC, priority DESC, job_id ASC
		LIMIT 1`).Scan(&row).Error
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("query Agent claimable media job: %w", err)
	}
	if row.JobID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	return row.JobID, true, nil
}

// claimRow 是 Agent claim 函数返回的完整但仍未信任的持久化契约行。
type claimRow struct {
	// SchemaVersion 是 Envelope 版本。
	SchemaVersion string `gorm:"column:schema_version" json:"schema_version"`
	// JobID 是 Agent Job UUIDv7。
	JobID uuid.UUID `gorm:"column:job_id" json:"job_id"`
	// BatchID 是 Agent Batch UUIDv7。
	BatchID uuid.UUID `gorm:"column:batch_id" json:"batch_id"`
	// OperationID 是 Agent Operation UUIDv7。
	OperationID uuid.UUID `gorm:"column:operation_id" json:"operation_id"`
	// SessionID 是 Agent Session UUIDv7。
	SessionID uuid.UUID `gorm:"column:session_id" json:"session_id"`
	// UserID 是可信 User UUIDv7。
	UserID uuid.UUID `gorm:"column:user_id" json:"user_id"`
	// ProjectID 是可信 Project UUIDv7。
	ProjectID uuid.UUID `gorm:"column:project_id" json:"project_id"`
	// JobType 是封闭媒体任务类型。
	JobType string `gorm:"column:job_type" json:"job_type"`
	// DefinitionVersion 是冻结 Tool Definition 版本。
	DefinitionVersion string `gorm:"column:definition_version" json:"definition_version"`
	// ScopeDigest 是 Tool Scope SHA-256。
	ScopeDigest string `gorm:"column:scope_digest" json:"scope_digest"`
	// OutputProfile 是冻结输出 Profile。
	OutputProfile string `gorm:"column:output_profile" json:"output_profile"`
	// SourceRef 是必须由 mediapreview 严格解码的原始 JSONB。
	SourceRef json.RawMessage `gorm:"column:source_ref" json:"source_ref"`
	// Target 是必须由 mediapreview 严格解码的原始 JSONB。
	Target json.RawMessage `gorm:"column:target" json:"target"`
	// ArtifactRequestDigest 是产物请求 SHA-256。
	ArtifactRequestDigest string `gorm:"column:artifact_request_digest" json:"artifact_request_digest"`
	// AttemptID 是当前 Claim Attempt UUIDv7。
	AttemptID uuid.UUID `gorm:"column:attempt_id" json:"attempt_id"`
	// Fence 是当前 Fencing Token。
	Fence int64 `gorm:"column:fence" json:"fence"`
	// LeaseExpiresAt 是 Agent PostgreSQL 权威租约到期时间。
	LeaseExpiresAt time.Time `gorm:"column:lease_expires_at" json:"lease_expires_at"`
	// CreatedAt 是 Agent Job 创建时间。
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	// DeadlineAt 是 Job 绝对截止时间。
	DeadlineAt time.Time `gorm:"column:deadline_at" json:"deadline_at"`
}

// Claim 调用 Agent first-write-wins 函数并把返回行再次经过 strict JSON Envelope 校验。
func (c *Client) Claim(ctx context.Context, request mediajob.ClaimRequest) (mediapreview.MediaJobEnvelopeV1, error) {
	var row claimRow
	result := c.db.WithContext(ctx).Raw(`
		SELECT * FROM agent.media_job_preview_v1_claim(?, ?, ?, ?, ?)`,
		request.JobID, request.WorkerID, request.AttemptID, request.ClaimRequestID, request.LeaseTTL.Milliseconds()).Scan(&row)
	if result.Error != nil {
		return mediapreview.MediaJobEnvelopeV1{}, fmt.Errorf("%w: claim Agent media job", mediajob.ErrOutcomeUnknown)
	}
	if result.RowsAffected != 1 || row.JobID == uuid.Nil {
		return mediapreview.MediaJobEnvelopeV1{}, mediajob.ErrNoClaim
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return mediapreview.MediaJobEnvelopeV1{}, fmt.Errorf("marshal Agent media envelope row: %w", err)
	}
	envelope, err := mediapreview.DecodeEnvelopeV1(payload)
	if err != nil {
		return mediapreview.MediaJobEnvelopeV1{}, fmt.Errorf("validate Agent media envelope: %w", err)
	}
	if envelope.JobID != request.JobID || envelope.AttemptID != request.AttemptID {
		return mediapreview.MediaJobEnvelopeV1{}, fmt.Errorf("Agent media envelope identity mismatch")
	}
	return envelope, nil
}

// Renew 调用 Agent 条件续租函数；空行或非 renewed 返回统一视为 Lease Lost。
func (c *Client) Renew(ctx context.Context, lease mediajob.LeaseRequest, ttl time.Duration) (time.Time, error) {
	var row struct {
		// Status 必须为 Agent 冻结函数返回的 renewed。
		Status string `gorm:"column:status"`
		// LeaseExpiresAt 是 Agent PostgreSQL 返回的新租约到期时间。
		LeaseExpiresAt time.Time `gorm:"column:lease_expires_at"`
	}
	result := c.db.WithContext(ctx).Raw(`
		SELECT * FROM agent.media_job_preview_v1_renew(?, ?, ?, ?, ?)`,
		lease.JobID, lease.WorkerID, lease.AttemptID, lease.Fence, ttl.Milliseconds()).Scan(&row)
	if result.Error != nil {
		return time.Time{}, fmt.Errorf("renew Agent media job: %w", result.Error)
	}
	if result.RowsAffected != 1 || row.Status != "renewed" || row.LeaseExpiresAt.IsZero() {
		return time.Time{}, mediajob.ErrLeaseLost
	}
	return row.LeaseExpiresAt, nil
}

// ScheduleRetry 条件更新当前 running Attempt 为 retry_wait；空行等价 Lease Lost。
func (c *Client) ScheduleRetry(ctx context.Context, request mediajob.ScheduleRetryRequest) error {
	var row struct {
		// Status 必须为 retry_wait。
		Status string `gorm:"column:status"`
		// AvailableAt 是 Agent PostgreSQL 持久化的下次可领取时间。
		AvailableAt time.Time `gorm:"column:available_at"`
	}
	result := c.db.WithContext(ctx).Raw(`
		SELECT * FROM agent.media_job_preview_v1_schedule_retry(?, ?, ?, ?, ?, ?)`,
		request.Lease.JobID, request.Lease.WorkerID, request.Lease.AttemptID, request.Lease.Fence,
		request.Delay.Milliseconds(), request.ErrorCode).Scan(&row)
	if result.Error != nil {
		return fmt.Errorf("%w: schedule Agent media job retry", mediajob.ErrOutcomeUnknown)
	}
	if result.RowsAffected != 1 || row.Status != "retry_wait" || row.AvailableAt.IsZero() {
		return mediajob.ErrLeaseLost
	}
	return nil
}

// MarkReconciling 条件更新当前 Attempt 为 reconciling，防止未知副作用进入普通热重试。
func (c *Client) MarkReconciling(ctx context.Context, lease mediajob.LeaseRequest, reasonCode string) error {
	var row struct {
		// Status 必须为 reconciling。
		Status string `gorm:"column:status"`
	}
	result := c.db.WithContext(ctx).Raw(`
		SELECT * FROM agent.media_job_preview_v1_mark_reconciling(?, ?, ?, ?, ?)`,
		lease.JobID, lease.WorkerID, lease.AttemptID, lease.Fence, reasonCode).Scan(&row)
	if result.Error != nil {
		return fmt.Errorf("%w: mark Agent media job reconciling", mediajob.ErrOutcomeUnknown)
	}
	if result.RowsAffected != 1 || row.Status != "reconciling" {
		return mediajob.ErrLeaseLost
	}
	return nil
}

// CommitTerminal 调用 Agent 原子终态函数并严格校验 Job/Batch/Operation/Event 联合结果。
func (c *Client) CommitTerminal(ctx context.Context, request mediajob.TerminalCommitRequest) (mediajob.TerminalCommitResult, error) {
	var row struct {
		// JobStatus 是 succeeded 或 dead。
		JobStatus string `gorm:"column:job_status"`
		// BatchStatus 是 completed 或 failed。
		BatchStatus string `gorm:"column:batch_status"`
		// OperationStatus 是 completed 或 failed。
		OperationStatus string `gorm:"column:operation_status"`
		// TerminalEventID 是 Agent 权威 Terminal Outbox UUIDv7。
		TerminalEventID uuid.UUID `gorm:"column:terminal_event_id"`
	}
	result := c.db.WithContext(ctx).Raw(`
		SELECT * FROM agent.media_job_preview_v1_commit_terminal(?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
		request.Lease.JobID, request.Lease.WorkerID, request.Lease.AttemptID, request.Lease.Fence,
		request.EventID, request.TerminalStatus, request.ResultSchemaVersion, request.ResultDigest,
		string(request.Result)).Scan(&row)
	if result.Error != nil {
		return mediajob.TerminalCommitResult{}, fmt.Errorf("%w: commit Agent media terminal", mediajob.ErrOutcomeUnknown)
	}
	if result.RowsAffected != 1 || row.TerminalEventID != request.EventID ||
		(request.TerminalStatus == "succeeded" && (row.JobStatus != "succeeded" || row.BatchStatus != "completed" || row.OperationStatus != "completed")) ||
		(request.TerminalStatus == "failed" && (row.JobStatus != "dead" || row.BatchStatus != "failed" || row.OperationStatus != "failed")) {
		return mediajob.TerminalCommitResult{}, mediajob.ErrLeaseLost
	}
	return mediajob.TerminalCommitResult{
		JobStatus: row.JobStatus, BatchStatus: row.BatchStatus,
		OperationStatus: row.OperationStatus, TerminalEventID: row.TerminalEventID,
	}, nil
}

// Get 查询 Agent Job 当前租约与终态摘要，用于 Claim/Retry/Terminal 响应未知后的权威核对。
func (c *Client) Get(ctx context.Context, jobID uuid.UUID) (mediajob.JobSnapshot, error) {
	var row struct {
		// JobStatus 是 Agent Job 权威状态。
		JobStatus string `gorm:"column:job_status"`
		// AttemptID 是当前或最后 Attempt UUIDv7。
		AttemptID uuid.NullUUID `gorm:"column:attempt_id"`
		// Fence 是当前或最后 Fencing Token。
		Fence sql.NullInt64 `gorm:"column:fence"`
		// LeaseOwner 是当前租约 Worker。
		LeaseOwner sql.NullString `gorm:"column:lease_owner"`
		// LeaseExpiresAt 是当前租约到期时间。
		LeaseExpiresAt sql.NullTime `gorm:"column:lease_expires_at"`
		// ResultSchemaVersion 是终态 Result 版本。
		ResultSchemaVersion sql.NullString `gorm:"column:result_schema_version"`
		// ResultDigest 是终态 Result 摘要。
		ResultDigest sql.NullString `gorm:"column:result_digest"`
		// TerminalEventID 是终态 Outbox UUIDv7。
		TerminalEventID uuid.NullUUID `gorm:"column:terminal_event_id"`
	}
	result := c.db.WithContext(ctx).Raw(`SELECT * FROM agent.media_job_preview_v1_get(?)`, jobID).Scan(&row)
	if result.Error != nil {
		return mediajob.JobSnapshot{}, fmt.Errorf("get Agent media job: %w", result.Error)
	}
	if result.RowsAffected != 1 || row.JobStatus == "" {
		return mediajob.JobSnapshot{}, mediajob.ErrNoClaim
	}
	snapshot := mediajob.JobSnapshot{
		JobStatus: row.JobStatus, Fence: row.Fence.Int64, LeaseOwner: row.LeaseOwner.String,
		LeaseExpiresAt: row.LeaseExpiresAt.Time, ResultSchemaVersion: row.ResultSchemaVersion.String,
		ResultDigest: row.ResultDigest.String,
	}
	if row.AttemptID.Valid {
		snapshot.AttemptID = row.AttemptID.UUID
	}
	if row.TerminalEventID.Valid {
		snapshot.TerminalEventID = row.TerminalEventID.UUID
	}
	return snapshot, nil
}

// Close 关闭 Agent Consumer PostgreSQL 底层连接池。
func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("get Agent media consumer pool for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close Agent media consumer: %w", err)
	}
	return nil
}

var _ mediajob.AgentClient = (*Client)(nil)
