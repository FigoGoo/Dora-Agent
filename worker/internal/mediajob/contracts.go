package mediajob

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
)

var (
	// ErrNoClaim 表示候选 Job 在当前调用时已不可领取，不属于执行失败。
	ErrNoClaim = errors.New("media job is not claimable")
	// ErrLeaseLost 表示当前 Attempt/Worker/Fence 条件不再匹配，必须立即停止副作用。
	ErrLeaseLost = errors.New("media job lease lost")
	// ErrOutcomeUnknown 表示远端调用可能已提交但响应未知，必须按原键查询收敛。
	ErrOutcomeUnknown = errors.New("media job remote outcome unknown")
	// ErrStateConflict 表示 Worker 私有状态或 first-write-wins 摘要与既有事实冲突。
	ErrStateConflict = errors.New("media job worker state conflict")
)

// Repository 保存 Worker-owned Attempt、Artifact Receipt 和 Finalize 查询摘要。
type Repository interface {
	// Readiness 验证 Worker-owned Attempt、Artifact Receipt 和 Finalization Observation 表存在。
	Readiness(ctx context.Context) error
	// CreateClaimIntent 在 Agent Claim 前持久化一个新的稳定 Attempt/Claim Request。
	CreateClaimIntent(ctx context.Context, intent ClaimIntent) error
	// NextRecoverableClaim 返回一个未结束 Attempt 的原 Claim 键；没有时 found=false。
	NextRecoverableClaim(ctx context.Context, workerID string) (intent ClaimIntent, found bool, err error)
	// MarkClaimUnknown 把响应未知的 Claim 标记为必须原键重放。
	MarkClaimUnknown(ctx context.Context, attemptID uuid.UUID, updatedAt time.Time) error
	// MarkClaimed 冻结 Agent 返回的 Job/Fence/Type/Artifact 摘要并进入 running。
	MarkClaimed(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, updatedAt time.Time) error
	// RecordArtifact 以 Attempt 和稳定 artifact identity first-write-wins 保存不可变摘要。
	RecordArtifact(ctx context.Context, record ArtifactRecord) error
	// EnsureFinalizeIntent first-write-wins 保存或返回当前 Attempt 的 Business 命令键。
	EnsureFinalizeIntent(ctx context.Context, proposed FinalizeIntent, updatedAt time.Time) (FinalizeIntent, error)
	// RecordFinalizationObservation 保存或幂等更新同一 Finalize 命令的权威查询摘要。
	RecordFinalizationObservation(ctx context.Context, observation FinalizationObservation) error
	// LatestFinalizationRecovery 按 Job 查询最近旧 Finalize 命令/观察，供不同 Worker/Fence 接管核对。
	LatestFinalizationRecovery(ctx context.Context, jobID uuid.UUID) (recovery FinalizationRecovery, found bool, err error)
	// EnsureTerminalIntent first-write-wins 保存或返回当前 Attempt 的 Agent Terminal 事件键。
	EnsureTerminalIntent(ctx context.Context, proposed TerminalIntent, updatedAt time.Time) (TerminalIntent, error)
	// GetTerminalIntent 读取 terminal_unknown 重启收敛所需的稳定 Event/Result 摘要。
	GetTerminalIntent(ctx context.Context, attemptID uuid.UUID) (TerminalIntent, bool, error)
	// UpdateAttemptStatus 使用旧状态集合条件更新 Worker 私有恢复状态和白名单错误码。
	UpdateAttemptStatus(ctx context.Context, attemptID uuid.UUID, allowedFrom []AttemptStatus, to AttemptStatus, errorCode string, updatedAt time.Time, finishedAt *time.Time) error
	// CountAttempts 返回共享 Worker PostgreSQL 中同一 Agent Job 的 Attempt 数量。
	CountAttempts(ctx context.Context, jobID uuid.UUID) (int, error)
}

// AgentClient 只消费 Agent 发布的 media_job_preview_v1 View/Functions。
type AgentClient interface {
	// Readiness 验证只读 View 和全部冻结函数存在且 PostgreSQL 可用。
	Readiness(ctx context.Context) error
	// NextClaimable 返回稳定排序的一个候选 Job；没有时 found=false。
	NextClaimable(ctx context.Context) (jobID uuid.UUID, found bool, err error)
	// Claim 使用已持久化 request/attempt ID 调用 first-write-wins Claim 函数。
	Claim(ctx context.Context, request ClaimRequest) (mediapreview.MediaJobEnvelopeV1, error)
	// Renew 续租当前 running Attempt；条件不匹配返回 ErrLeaseLost。
	Renew(ctx context.Context, lease LeaseRequest, ttl time.Duration) (time.Time, error)
	// ScheduleRetry 把当前 running Attempt 条件更新为 retry_wait。
	ScheduleRetry(ctx context.Context, request ScheduleRetryRequest) error
	// MarkReconciling 把远端副作用未知的当前 Attempt 条件更新为 reconciling。
	MarkReconciling(ctx context.Context, lease LeaseRequest, reasonCode string) error
	// CommitTerminal 原子更新 Agent Job/Batch/Operation 并 AppendOnce Terminal Outbox。
	CommitTerminal(ctx context.Context, request TerminalCommitRequest) (TerminalCommitResult, error)
	// Get 查询 Job 当前租约和终态摘要，用于未知响应核对。
	Get(ctx context.Context, jobID uuid.UUID) (JobSnapshot, error)
	// Close 关闭 Agent Consumer PostgreSQL 连接池。
	Close() error
}

// BusinessClient 只调用 loopback Business Finalize、Query-Finalization 和 Readiness。
type BusinessClient interface {
	// Readiness 验证 Profile、对象根及 Prepare/Finalize 能力精确开启。
	Readiness(ctx context.Context) error
	// Finalize 以稳定 command/digest 提交 ready 或 failed Asset 终态。
	Finalize(ctx context.Context, request FinalizeRequestV1) (FinalizeResultV1, error)
	// QueryFinalization 按原 command/digest/preparation 核对未知响应。
	QueryFinalization(ctx context.Context, request QueryFinalizationRequestV1) (QueryFinalizationResultV1, error)
}

// IDGenerator 生成所有副作用发生前持久化的 UUIDv7 稳定标识。
type IDGenerator interface {
	// NewUUID 生成一个新的 UUIDv7；随机源失败时不得降级。
	NewUUID() (uuid.UUID, error)
}

// Clock 提供非 Lease 审计和 HTTP RequestID 使用的可注入 UTC 时间。
type Clock interface {
	// Now 返回当前 UTC 时间；Lease 权威时间仍来自 Agent PostgreSQL。
	Now() time.Time
}
