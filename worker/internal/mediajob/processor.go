package mediajob

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
)

// ProcessorConfig 是媒体 Job Scanner、租约、重试和产物大小的启动后不可变预算。
type ProcessorConfig struct {
	// WorkerID 是 Agent Lease Owner 和 Worker 私有 Attempt 的稳定实例标识。
	WorkerID string
	// PollInterval 是 PostgreSQL claimable View 的兜底扫描周期。
	PollInterval time.Duration
	// LeaseTTL 是每次 Claim/Renew 请求的 Agent PostgreSQL 租约时长。
	LeaseTTL time.Duration
	// HeartbeatInterval 是持有 Attempt 时的续租周期。
	HeartbeatInterval time.Duration
	// AttemptTimeout 是单次 Claim Attempt 的最大执行时间。
	AttemptTimeout time.Duration
	// MaxAttempts 是同一 Job 进入失败终态前的 Worker 尝试上限。
	MaxAttempts int
	// AgentCallTimeout 是单次 Agent View/Function 调用上限。
	AgentCallTimeout time.Duration
	// BusinessCallTimeout 是单次 Business Finalize/Query 调用上限。
	BusinessCallTimeout time.Duration
	// MaxPNGBytes 是允许 Finalize ready 的 PNG 字节硬预算。
	MaxPNGBytes int64
	// MaxMP4Bytes 是允许 Finalize ready 的 MP4 字节硬预算。
	MaxMP4Bytes int64
	// RetryBaseDelay 是 Full Jitter 指数退避的初始上限。
	RetryBaseDelay time.Duration
	// RetryMaxDelay 是 Full Jitter 指数退避的最大上限。
	RetryMaxDelay time.Duration
}

// artifactExecutor 是 Processor 对 mediapreview.Engine 的最小依赖面。
type artifactExecutor interface {
	// GeneratePNG 生成、验证并原子发布确定性 PNG staging artifact。
	GeneratePNG(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error)
	// AssembleMP4 生成、探测并原子发布固定参数 MP4 staging artifact。
	AssembleMP4(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error)
}

// JitterSource 在给定非负上限内返回 Full Jitter 延迟，测试可注入确定值。
type JitterSource func(max time.Duration) time.Duration

// Processor 是 media.runtime.v3preview1 唯一的 PostgreSQL Scanner 和 Attempt 生命周期 Owner。
type Processor struct {
	// config 保存启动时已校验的全部硬预算。
	config ProcessorConfig
	// repository 保存 Worker-owned 恢复事实。
	repository Repository
	// agent 只调用 Agent 发布的 View/Functions。
	agent AgentClient
	// business 只调用 loopback Business 严格 JSON 端点。
	business BusinessClient
	// artifacts 生成并验证本地 PNG/MP4。
	artifacts artifactExecutor
	// idGenerator 在副作用前生成可持久化 UUIDv7。
	idGenerator IDGenerator
	// clock 提供非 Lease UTC 审计时间。
	clock Clock
	// jitter 生成有限 Full Jitter 延迟。
	jitter JitterSource
	// logger 只记录稳定 ID、状态和错误码，不记录 Payload、路径或 stderr。
	logger *slog.Logger
	// lifecycleMu 串行化 Start/Stop 状态。
	lifecycleMu sync.Mutex
	// started 表示唯一 Scanner 已启动。
	started bool
	// stopOnce 保证只关闭一次停止 Claim 信号。
	stopOnce sync.Once
	// stopClaims 先停止领取新 Job；当前 Attempt 继续 Heartbeat 并 Drain。
	stopClaims chan struct{}
	// runCancel 仅在 Shutdown Budget 耗尽时取消当前 Attempt。
	runCancel context.CancelFunc
	// done 在 Scanner 和当前 Attempt 完全退出后关闭。
	done chan struct{}
}

// NewProcessor 校验依赖与预算并构造尚未启动的唯一媒体 Scanner。
func NewProcessor(config ProcessorConfig, repository Repository, agent AgentClient, business BusinessClient, artifacts artifactExecutor, idGenerator IDGenerator, clock Clock, jitter JitterSource, logger *slog.Logger) (*Processor, error) {
	if config.WorkerID == "" || config.PollInterval <= 0 || config.LeaseTTL <= 0 ||
		config.HeartbeatInterval <= 0 || config.HeartbeatInterval > config.LeaseTTL/3 ||
		config.AttemptTimeout <= config.LeaseTTL || config.MaxAttempts <= 0 ||
		config.AgentCallTimeout <= 0 || config.AgentCallTimeout >= config.HeartbeatInterval ||
		config.BusinessCallTimeout <= 0 || config.MaxPNGBytes <= 0 || config.MaxMP4Bytes < config.MaxPNGBytes ||
		config.RetryBaseDelay <= 0 || config.RetryMaxDelay < config.RetryBaseDelay ||
		repository == nil || agent == nil || business == nil || artifacts == nil || idGenerator == nil || clock == nil {
		return nil, fmt.Errorf("invalid media job processor configuration")
	}
	if jitter == nil {
		jitter = cryptoFullJitter
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		config: config, repository: repository, agent: agent, business: business,
		artifacts: artifacts, idGenerator: idGenerator, clock: clock, jitter: jitter, logger: logger,
		stopClaims: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Readiness 在启动 Scanner 前依次验证 Agent 持久化消费契约和 Business 媒体内部端点。
func (p *Processor) Readiness(ctx context.Context) error {
	if err := p.repository.Readiness(ctx); err != nil {
		return fmt.Errorf("Worker media repository readiness failed: %w", err)
	}
	agentContext, cancelAgent := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	err := p.agent.Readiness(agentContext)
	cancelAgent()
	if err != nil {
		return fmt.Errorf("Agent media readiness failed: %w", err)
	}
	businessContext, cancelBusiness := context.WithTimeout(ctx, p.config.BusinessCallTimeout)
	err = p.business.Readiness(businessContext)
	cancelBusiness()
	if err != nil {
		return fmt.Errorf("Business media readiness failed: %w", err)
	}
	return nil
}

// Start 启动唯一有界 Scanner；重复启动失败，Profile 关闭时 Bootstrap 不构造也不调用本方法。
func (p *Processor) Start() error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.started {
		return fmt.Errorf("media job processor already started")
	}
	runContext, cancel := context.WithCancel(context.Background())
	p.runCancel = cancel
	p.started = true
	go p.scanLoop(runContext)
	return nil
}

// Stop 先停止新 Claim，再等待当前 Attempt 持续 Heartbeat 并 Drain；超时后才取消执行 Context。
func (p *Processor) Stop(ctx context.Context) error {
	p.lifecycleMu.Lock()
	if !p.started {
		p.lifecycleMu.Unlock()
		return nil
	}
	p.stopOnce.Do(func() { close(p.stopClaims) })
	done := p.done
	cancel := p.runCancel
	p.lifecycleMu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		cancel()
		<-done
		return fmt.Errorf("stop media job processor: %w", ctx.Err())
	}
}

// scanLoop 串行执行一个 Job；没有每 Job 无界 goroutine，Redis 缺失时 Poll 仍可恢复。
func (p *Processor) scanLoop(ctx context.Context) {
	defer close(p.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopClaims:
			return
		default:
		}

		processed, err := p.ProcessNext(ctx)
		if err != nil {
			// 普通日志只记录稳定错误分类，不展开可能含 DSN 或底层路径的错误链。
			p.logger.Warn("媒体 Preview Job 处理未收敛", "error_code", stableProcessorErrorCode(err))
		}
		if processed {
			continue
		}
		timer := time.NewTimer(p.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-p.stopClaims:
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

// ProcessNext 优先恢复当前 Worker 未结束 Claim；否则从 Agent View 读取并领取一个候选 Job。
//
// 返回 processed=false 表示当前没有任务；所有数据库调用均为单 Job、单 SQL，不在批量结果循环中执行同构 SQL。
func (p *Processor) ProcessNext(ctx context.Context) (bool, error) {
	intent, found, err := p.repository.NextRecoverableClaim(ctx, p.config.WorkerID)
	if err != nil {
		return false, err
	}
	if !found {
		jobID, found, err := p.nextClaimable(ctx)
		if err != nil || !found {
			return false, err
		}
		intent, err = p.createClaimIntent(ctx, jobID)
		if err != nil {
			return true, err
		}
	}
	if intent.Status == AttemptStatusTerminalUnknown {
		resolved, recoverErr := p.recoverTerminalUnknown(ctx, intent)
		if recoverErr != nil || resolved {
			return true, recoverErr
		}
	}

	envelope, err := p.claim(ctx, intent)
	if err != nil {
		return true, err
	}
	if err := p.repository.MarkClaimed(ctx, envelope, p.clock.Now()); err != nil {
		return true, err
	}
	return true, p.executeAttempt(ctx, envelope)
}

// recoverTerminalUnknown 在重放 Claim 前先读取 Agent 权威终态，避免已成功提交的 Job 因状态终结而拒绝原 Claim。
func (p *Processor) recoverTerminalUnknown(ctx context.Context, claim ClaimIntent) (bool, error) {
	intent, found, err := p.repository.GetTerminalIntent(ctx, claim.AttemptID)
	if err != nil {
		return true, err
	}
	if !found {
		return true, ErrStateConflict
	}
	getContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	snapshot, err := p.agent.Get(getContext, claim.JobID)
	cancel()
	if err != nil {
		return true, fmt.Errorf("%w: recover Agent terminal after restart", ErrOutcomeUnknown)
	}
	if snapshot.JobStatus == "succeeded" || snapshot.JobStatus == "dead" {
		expectedJobStatus := "succeeded"
		if intent.Status == "failed" {
			expectedJobStatus = "dead"
		}
		if snapshot.JobStatus != expectedJobStatus || snapshot.ResultSchemaVersion != TerminalResultSchemaV1 ||
			snapshot.ResultDigest != intent.ResultDigest || snapshot.TerminalEventID != intent.EventID {
			return true, p.markLeaseLost(ctx, claim.AttemptID)
		}
		return true, p.finishLocalAttempt(ctx, claim.AttemptID, intent.Status, "")
	}
	if snapshot.JobStatus != "running" && snapshot.JobStatus != "reconciling" {
		return true, p.markLeaseLost(ctx, claim.AttemptID)
	}
	return false, nil
}

// nextClaimable 使用有界 Context 读取 Agent View 的一个候选 Job。
func (p *Processor) nextClaimable(ctx context.Context) (uuid.UUID, bool, error) {
	callContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	defer cancel()
	return p.agent.NextClaimable(callContext)
}

// createClaimIntent 在 Agent Claim 副作用前持久化 AttemptID 和 ClaimRequestID。
func (p *Processor) createClaimIntent(ctx context.Context, jobID uuid.UUID) (ClaimIntent, error) {
	attemptID, err := p.idGenerator.NewUUID()
	if err != nil {
		return ClaimIntent{}, err
	}
	claimRequestID, err := p.idGenerator.NewUUID()
	if err != nil {
		return ClaimIntent{}, err
	}
	intent := ClaimIntent{
		AttemptID: attemptID, ClaimRequestID: claimRequestID, JobID: jobID,
		WorkerID: p.config.WorkerID, Status: AttemptStatusClaimPending, StartedAt: p.clock.Now(),
	}
	if err := p.repository.CreateClaimIntent(ctx, intent); err != nil {
		return ClaimIntent{}, err
	}
	return intent, nil
}

// claim 用原 claim_request_id 调用 Agent；响应未知时保留 claim_unknown 供下轮原键重放。
func (p *Processor) claim(ctx context.Context, intent ClaimIntent) (mediapreview.MediaJobEnvelopeV1, error) {
	callContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	envelope, err := p.agent.Claim(callContext, ClaimRequest{
		JobID: intent.JobID, WorkerID: intent.WorkerID, AttemptID: intent.AttemptID,
		ClaimRequestID: intent.ClaimRequestID, LeaseTTL: p.config.LeaseTTL,
	})
	cancel()
	if err == nil {
		return envelope, nil
	}
	now := p.clock.Now()
	if errors.Is(err, ErrNoClaim) || errors.Is(err, ErrLeaseLost) {
		_ = p.repository.UpdateAttemptStatus(ctx, intent.AttemptID,
			[]AttemptStatus{AttemptStatusClaimPending, AttemptStatusClaimUnknown},
			AttemptStatusClaimRejected, "LEASE_LOST", now, &now)
		return mediapreview.MediaJobEnvelopeV1{}, err
	}
	if markErr := p.repository.MarkClaimUnknown(ctx, intent.AttemptID, now); markErr != nil {
		return mediapreview.MediaJobEnvelopeV1{}, errors.Join(err, markErr)
	}
	return mediapreview.MediaJobEnvelopeV1{}, fmt.Errorf("%w: Agent claim", ErrOutcomeUnknown)
}

// executeAttempt 在当前 Attempt/Fence 下维持 Heartbeat，并依次收敛旧 Finalize、Artifact、Finalize 和 Terminal。
func (p *Processor) executeAttempt(parent context.Context, envelope mediapreview.MediaJobEnvelopeV1) error {
	deadline := p.clock.Now().Add(p.config.AttemptTimeout)
	if envelope.DeadlineAt.Before(deadline) {
		deadline = envelope.DeadlineAt
	}
	attemptContext, cancelAttempt := context.WithDeadline(parent, deadline)
	lease := LeaseRequest{
		JobID: envelope.JobID, WorkerID: p.config.WorkerID,
		AttemptID: envelope.AttemptID, Fence: envelope.Fence,
	}
	heartbeat := p.startHeartbeat(attemptContext, cancelAttempt, lease)
	defer func() {
		heartbeat.stop()
		cancelAttempt()
	}()

	// expired running/reconciling 可被新 Fence 接管；必须先按共享 Worker 摘要查询旧 Finalize，避免重复命令冲突。
	if resolved, err := p.reconcilePriorFinalization(attemptContext, envelope, lease); err != nil {
		return err
	} else if resolved {
		return nil
	}

	receipt, err := p.executeArtifact(attemptContext, envelope)
	if err != nil {
		if heartbeat.err() != nil {
			return p.markLeaseLost(parent, envelope.AttemptID)
		}
		return p.handleArtifactFailure(attemptContext, envelope, lease, err)
	}
	if heartbeat.err() != nil {
		return p.markLeaseLost(parent, envelope.AttemptID)
	}
	receiptID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	if err := p.repository.RecordArtifact(attemptContext, ArtifactRecord{
		ReceiptID: receiptID, AttemptID: envelope.AttemptID, Receipt: receipt, CreatedAt: p.clock.Now(),
	}); err != nil {
		return err
	}
	return p.finalizeReady(attemptContext, envelope, lease, receipt)
}

// executeArtifact 按 JobType 调用冻结 Engine，并在 Business Finalize 前检查产物字节硬预算。
func (p *Processor) executeArtifact(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error) {
	var receipt mediapreview.ArtifactReceiptV1
	var err error
	switch envelope.JobType {
	case mediapreview.JobTypeGeneratePNG:
		receipt, err = p.artifacts.GeneratePNG(ctx, envelope)
		if err == nil && receipt.SizeBytes > p.config.MaxPNGBytes {
			err = &mediapreview.ArtifactError{Code: mediapreview.ErrorCodeArtifactInvalid, Op: "png_byte_budget"}
		}
	case mediapreview.JobTypeAssembleMP4:
		receipt, err = p.artifacts.AssembleMP4(ctx, envelope)
		if err == nil && receipt.SizeBytes > p.config.MaxMP4Bytes {
			err = &mediapreview.ArtifactError{Code: mediapreview.ErrorCodeArtifactInvalid, Op: "mp4_byte_budget"}
		}
	default:
		err = &mediapreview.ArtifactError{Code: mediapreview.ErrorCodeUnsupportedProfile, Op: "job_type"}
	}
	return receipt, err
}

// reconcilePriorFinalization 在生成新 artifact 前查询同 Job 旧命令；completed 直接复用，not_found 才继续。
func (p *Processor) reconcilePriorFinalization(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest) (bool, error) {
	recovery, found, err := p.repository.LatestFinalizationRecovery(ctx, envelope.JobID)
	if err != nil || !found {
		return false, err
	}
	requestID, err := p.idGenerator.NewUUID()
	if err != nil {
		return false, err
	}
	queryContext, cancel := context.WithTimeout(ctx, p.config.BusinessCallTimeout)
	response, queryErr := p.business.QueryFinalization(queryContext, QueryFinalizationRequestV1{
		SchemaVersion: QueryFinalizationRequestSchemaV1, RequestID: requestID,
		CommandID: recovery.CommandID, RequestDigest: recovery.RequestDigest,
		PreparationID: envelope.Target.PreparationID,
	})
	cancel()
	if queryErr != nil {
		if err := p.enterReconciling(ctx, envelope.AttemptID, lease, "UNKNOWN_OUTCOME"); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := p.recordFinalizationResponse(ctx, envelope, recovery.CommandID, recovery.RequestDigest, response, recovery.ErrorCode); err != nil {
		return false, err
	}
	switch response.Status {
	case "completed":
		if response.Result == nil {
			return false, ErrStateConflict
		}
		if err := p.commitFromFinalization(ctx, envelope, lease, *response.Result, recovery.ErrorCode); err != nil {
			return false, err
		}
		return true, nil
	case "not_found":
		return false, nil
	case "conflict":
		if err := p.commitFailure(ctx, envelope, lease, "IDEMPOTENCY_CONFLICT"); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, ErrStateConflict
	}
}

// finalizeReady 冻结稳定 Finalize 命令，响应未知时按原键 Query 后才决定重放或 reconciling。
func (p *Processor) finalizeReady(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, receipt mediapreview.ArtifactReceiptV1) error {
	output := &FinalizeOutputV1{
		ContentDigest: receipt.ContentDigest, SizeBytes: receipt.SizeBytes, MIMEType: receipt.MIMEType,
		Width: receipt.Width, Height: receipt.Height, DurationMS: receipt.DurationMS,
		Codec: receipt.Codec, PixelFormat: receipt.PixelFormat,
	}
	return p.finalize(ctx, envelope, lease, "ready", output, "")
}

// finalizeFailed 使用同一 Business Prepare/Fence 提交白名单失败，确保 reserved Asset 不永久悬挂。
func (p *Processor) finalizeFailed(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, errorCode string) error {
	return p.finalize(ctx, envelope, lease, "failed", nil, errorCode)
}

// finalize 处理 ready/failed 公共 first-write-wins、Query 和 Terminal 收敛流程。
func (p *Processor) finalize(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, terminalStatus string, output *FinalizeOutputV1, errorCode string) error {
	commandID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	request := FinalizeRequestV1{
		SchemaVersion: FinalizeRequestSchemaV1, CommandID: commandID,
		PreparationID: envelope.Target.PreparationID, OperationID: envelope.OperationID,
		BatchID: envelope.BatchID, JobID: envelope.JobID, AttemptID: envelope.AttemptID,
		Fence: envelope.Fence, TerminalStatus: terminalStatus, Output: output, ErrorCode: errorCode,
	}
	digest, err := FinalizeSemanticDigest(request)
	if err != nil {
		return err
	}
	intent, err := p.repository.EnsureFinalizeIntent(ctx, FinalizeIntent{
		AttemptID: envelope.AttemptID, CommandID: commandID, RequestDigest: digest, ErrorCode: errorCode,
	}, p.clock.Now())
	if err != nil {
		return err
	}
	request.CommandID = intent.CommandID
	request.RequestDigest = intent.RequestDigest
	request.RequestID, err = p.idGenerator.NewUUID()
	if err != nil {
		return err
	}

	result, err := p.callFinalize(ctx, request)
	if err == nil {
		if err := p.recordCompletedFinalization(ctx, envelope, intent, result, errorCode); err != nil {
			return err
		}
		return p.commitFromFinalization(ctx, envelope, lease, result, errorCode)
	}
	if !errors.Is(err, ErrOutcomeUnknown) {
		return p.enterReconciling(ctx, envelope.AttemptID, lease, "DEPENDENCY_NOT_READY")
	}
	_ = p.repository.UpdateAttemptStatus(ctx, envelope.AttemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady, AttemptStatusFinalizeUnknown},
		AttemptStatusFinalizeUnknown, "UNKNOWN_OUTCOME", p.clock.Now(), nil)
	return p.queryAfterUnknownFinalize(ctx, envelope, lease, request, errorCode)
}

// callFinalize 使用单次 BusinessCallTimeout 调用原命令，不在 Client 层叠加隐藏重试。
func (p *Processor) callFinalize(ctx context.Context, request FinalizeRequestV1) (FinalizeResultV1, error) {
	callContext, cancel := context.WithTimeout(ctx, p.config.BusinessCallTimeout)
	defer cancel()
	return p.business.Finalize(callContext, request)
}

// queryAfterUnknownFinalize 按原键查询；not_found 权威证明未提交后只重放一次同命令。
func (p *Processor) queryAfterUnknownFinalize(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, request FinalizeRequestV1, failureCode string) error {
	queryRequestID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	queryContext, cancel := context.WithTimeout(ctx, p.config.BusinessCallTimeout)
	query, queryErr := p.business.QueryFinalization(queryContext, QueryFinalizationRequestV1{
		SchemaVersion: QueryFinalizationRequestSchemaV1, RequestID: queryRequestID,
		CommandID: request.CommandID, RequestDigest: request.RequestDigest,
		PreparationID: request.PreparationID,
	})
	cancel()
	if queryErr != nil {
		return p.enterReconciling(ctx, envelope.AttemptID, lease, "UNKNOWN_OUTCOME")
	}
	if err := p.recordFinalizationResponse(ctx, envelope, request.CommandID, request.RequestDigest, query, failureCode); err != nil {
		return err
	}
	switch query.Status {
	case "completed":
		if query.Result == nil {
			return ErrStateConflict
		}
		return p.commitFromFinalization(ctx, envelope, lease, *query.Result, failureCode)
	case "conflict":
		return p.commitFailure(ctx, envelope, lease, "IDEMPOTENCY_CONFLICT")
	case "not_found":
		request.RequestID, err = p.idGenerator.NewUUID()
		if err != nil {
			return err
		}
		result, retryErr := p.callFinalize(ctx, request)
		if retryErr != nil {
			return p.enterReconciling(ctx, envelope.AttemptID, lease, "UNKNOWN_OUTCOME")
		}
		if err := p.recordCompletedFinalization(ctx, envelope, FinalizeIntent{
			AttemptID: envelope.AttemptID, CommandID: request.CommandID, RequestDigest: request.RequestDigest,
			ErrorCode: failureCode,
		}, result, failureCode); err != nil {
			return err
		}
		return p.commitFromFinalization(ctx, envelope, lease, result, failureCode)
	default:
		return ErrStateConflict
	}
}

// recordCompletedFinalization 保存直接 Finalize 成功的权威 completed 摘要。
func (p *Processor) recordCompletedFinalization(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, intent FinalizeIntent, result FinalizeResultV1, errorCode string) error {
	observationID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	return p.repository.RecordFinalizationObservation(ctx, FinalizationObservation{
		ObservationID: observationID, AttemptID: envelope.AttemptID, JobID: envelope.JobID,
		Fence: envelope.Fence, CommandID: intent.CommandID, RequestDigest: intent.RequestDigest,
		PreparationID: envelope.Target.PreparationID, QueryStatus: "completed", Result: &result,
		ErrorCode: errorCode, ObservedAt: p.clock.Now(),
	})
}

// recordFinalizationResponse 保存 Query 的 not_found/completed/conflict 最小摘要。
func (p *Processor) recordFinalizationResponse(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, commandID uuid.UUID, requestDigest string, response QueryFinalizationResultV1, failureCode string) error {
	observationID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	errorCode := response.ErrorCode
	if response.Status == "completed" && response.Result != nil && response.Result.AssetRef.Status == "failed" {
		errorCode = failureCode
	}
	return p.repository.RecordFinalizationObservation(ctx, FinalizationObservation{
		ObservationID: observationID, AttemptID: envelope.AttemptID, JobID: envelope.JobID,
		Fence: envelope.Fence, CommandID: commandID, RequestDigest: requestDigest,
		PreparationID: envelope.Target.PreparationID, QueryStatus: response.Status,
		Result: response.Result, ErrorCode: errorCode, ObservedAt: p.clock.Now(),
	})
}

// commitFromFinalization 把 Business ready/failed 权威结果转换为严格 Agent Terminal Result。
func (p *Processor) commitFromFinalization(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, result FinalizeResultV1, failureCode string) error {
	if result.AssetRef.AssetID != envelope.Target.AssetID || result.AssetRef.Version != envelope.Target.AssetVersion ||
		(envelope.JobType == mediapreview.JobTypeGeneratePNG &&
			(result.AssetRef.MediaKind != "image" || result.AssetRef.MIMEType != "image/png")) ||
		(envelope.JobType == mediapreview.JobTypeAssembleMP4 &&
			(result.AssetRef.MediaKind != "video" || result.AssetRef.MIMEType != "video/mp4")) {
		return ErrStateConflict
	}
	if result.AssetRef.Status == "ready" {
		return p.commitSuccess(ctx, envelope, lease, result)
	}
	if failureCode == "" {
		failureCode = "INTERNAL"
	}
	return p.commitFailure(ctx, envelope, lease, failureCode)
}

// commitSuccess 提交只含 ready Asset Ref 和 Finalization Receipt 的成功 Terminal Result。
func (p *Processor) commitSuccess(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, finalization FinalizeResultV1) error {
	receiptID := finalization.FinalizationReceiptID
	result := TerminalResultV1{
		SchemaVersion: TerminalResultSchemaV1, Status: "succeeded",
		AssetRef: &TerminalAssetRefV1{
			AssetID: finalization.AssetRef.AssetID, Version: finalization.AssetRef.Version,
			Status: finalization.AssetRef.Status, MediaKind: finalization.AssetRef.MediaKind,
			MIMEType: finalization.AssetRef.MIMEType, ContentDigest: finalization.AssetRef.ContentDigest,
			SizeBytes: finalization.AssetRef.SizeBytes,
		},
		FinalizationReceiptID: &receiptID,
	}
	return p.commitTerminal(ctx, envelope, lease, result)
}

// commitFailure 提交只含白名单 error_code 的失败 Terminal Result。
func (p *Processor) commitFailure(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, errorCode string) error {
	if !isTerminalErrorCode(errorCode) {
		errorCode = "INTERNAL"
	}
	result := TerminalResultV1{SchemaVersion: TerminalResultSchemaV1, Status: "failed", ErrorCode: errorCode}
	return p.commitTerminal(ctx, envelope, lease, result)
}

// commitTerminal 持久化稳定 Event/ResultDigest，未知响应时用 Agent Get 核对后原键重放一次。
func (p *Processor) commitTerminal(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, result TerminalResultV1) error {
	payload, digest, err := terminalPayloadAndDigest(result)
	if err != nil {
		return err
	}
	eventID, err := p.idGenerator.NewUUID()
	if err != nil {
		return err
	}
	intent, err := p.repository.EnsureTerminalIntent(ctx, TerminalIntent{
		AttemptID: envelope.AttemptID, EventID: eventID, Status: result.Status, ResultDigest: digest,
	}, p.clock.Now())
	if err != nil {
		return err
	}
	request := TerminalCommitRequest{
		Lease: lease, EventID: intent.EventID, TerminalStatus: intent.Status,
		ResultSchemaVersion: TerminalResultSchemaV1, ResultDigest: intent.ResultDigest, Result: payload,
	}
	callContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	_, commitErr := p.agent.CommitTerminal(callContext, request)
	cancel()
	if commitErr == nil {
		return p.finishLocalAttempt(ctx, envelope.AttemptID, result.Status, result.ErrorCode)
	}
	_ = p.repository.UpdateAttemptStatus(ctx, envelope.AttemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady, AttemptStatusFinalizeUnknown,
			AttemptStatusReconciling, AttemptStatusTerminalUnknown},
		AttemptStatusTerminalUnknown, "UNKNOWN_OUTCOME", p.clock.Now(), nil)
	return p.reconcileTerminal(ctx, envelope, request, result.Status, result.ErrorCode)
}

// reconcileTerminal 用 Agent get 收敛 commit 响应未知；已提交匹配摘要则完成，未提交同 Fence 才原键重放。
func (p *Processor) reconcileTerminal(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, request TerminalCommitRequest, terminalStatus string, errorCode string) error {
	getContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	snapshot, err := p.agent.Get(getContext, envelope.JobID)
	cancel()
	if err != nil {
		return fmt.Errorf("%w: query Agent terminal", ErrOutcomeUnknown)
	}
	if (snapshot.JobStatus == "succeeded" || snapshot.JobStatus == "dead") &&
		snapshot.ResultSchemaVersion == request.ResultSchemaVersion && snapshot.ResultDigest == request.ResultDigest &&
		snapshot.TerminalEventID == request.EventID {
		return p.finishLocalAttempt(ctx, envelope.AttemptID, terminalStatus, errorCode)
	}
	if snapshot.AttemptID != envelope.AttemptID || snapshot.Fence != envelope.Fence ||
		(snapshot.JobStatus != "running" && snapshot.JobStatus != "reconciling") {
		return p.markLeaseLost(ctx, envelope.AttemptID)
	}
	retryContext, cancelRetry := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	_, err = p.agent.CommitTerminal(retryContext, request)
	cancelRetry()
	if err != nil {
		return fmt.Errorf("%w: replay Agent terminal", ErrOutcomeUnknown)
	}
	return p.finishLocalAttempt(ctx, envelope.AttemptID, terminalStatus, errorCode)
}

// finishLocalAttempt 在 Agent 权威终态后更新 Worker 私有投影；终态不允许原地重置。
func (p *Processor) finishLocalAttempt(ctx context.Context, attemptID uuid.UUID, terminalStatus string, errorCode string) error {
	now := p.clock.Now()
	to := AttemptStatusCompleted
	if terminalStatus == "failed" {
		to = AttemptStatusFailed
	}
	projectionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.AgentCallTimeout)
	defer cancel()
	return p.repository.UpdateAttemptStatus(projectionContext, attemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady, AttemptStatusFinalizeUnknown,
			AttemptStatusReconciling, AttemptStatusTerminalUnknown},
		to, errorCode, now, &now)
}

// handleArtifactFailure 把明确可重试、永久失败和 Unknown Outcome 分流，禁止 unknown 进入普通 retry_wait。
func (p *Processor) handleArtifactFailure(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, artifactErr error) error {
	code := string(mediapreview.CodeOf(artifactErr))
	switch mediapreview.CodeOf(artifactErr) {
	case mediapreview.ErrorCodeExecutionTimeout:
		return p.scheduleRetry(ctx, envelope, lease, code)
	case mediapreview.ErrorCodeUnknownOutcome, mediapreview.ErrorCodeInternal:
		return p.enterReconciling(ctx, envelope.AttemptID, lease, "UNKNOWN_OUTCOME")
	default:
		return p.finalizeFailed(ctx, envelope, lease, code)
	}
}

// scheduleRetry 使用共享 Attempt 数量和有限 Full Jitter 调用 Agent；超限时转为失败 Finalize/Terminal。
func (p *Processor) scheduleRetry(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1, lease LeaseRequest, errorCode string) error {
	attemptCount, err := p.repository.CountAttempts(ctx, envelope.JobID)
	if err != nil {
		return err
	}
	if attemptCount >= p.config.MaxAttempts {
		return p.finalizeFailed(ctx, envelope, lease, errorCode)
	}
	delayCap := p.retryDelayCap(attemptCount)
	delay := p.jitter(delayCap)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	callContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	err = p.agent.ScheduleRetry(callContext, ScheduleRetryRequest{
		Lease: lease, Delay: delay, ErrorCode: errorCode,
	})
	cancel()
	if err != nil {
		getContext, cancelGet := context.WithTimeout(ctx, p.config.AgentCallTimeout)
		snapshot, getErr := p.agent.Get(getContext, envelope.JobID)
		cancelGet()
		if getErr != nil || snapshot.JobStatus != "retry_wait" || snapshot.AttemptID != envelope.AttemptID || snapshot.Fence != envelope.Fence {
			return fmt.Errorf("%w: schedule Agent retry", ErrOutcomeUnknown)
		}
	}
	now := p.clock.Now()
	return p.repository.UpdateAttemptStatus(ctx, envelope.AttemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady},
		AttemptStatusRetryScheduled, errorCode, now, &now)
}

// enterReconciling 先请求 Agent 阻断普通 retry，再更新 Worker 投影；条件丢失立即停止。
func (p *Processor) enterReconciling(ctx context.Context, attemptID uuid.UUID, lease LeaseRequest, reasonCode string) error {
	callContext, cancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
	err := p.agent.MarkReconciling(callContext, lease, reasonCode)
	cancel()
	if err != nil && !errors.Is(err, ErrLeaseLost) {
		return fmt.Errorf("%w: mark Agent reconciling", ErrOutcomeUnknown)
	}
	if errors.Is(err, ErrLeaseLost) {
		return p.markLeaseLost(ctx, attemptID)
	}
	return p.repository.UpdateAttemptStatus(ctx, attemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady, AttemptStatusFinalizeUnknown, AttemptStatusReconciling},
		AttemptStatusReconciling, reasonCode, p.clock.Now(), nil)
}

// markLeaseLost 记录 Worker 私有终止事实；旧 Fence 不再 Finalize、Retry 或 Commit Terminal。
func (p *Processor) markLeaseLost(ctx context.Context, attemptID uuid.UUID) error {
	now := p.clock.Now()
	projectionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.AgentCallTimeout)
	defer cancel()
	err := p.repository.UpdateAttemptStatus(projectionContext, attemptID,
		[]AttemptStatus{AttemptStatusRunning, AttemptStatusArtifactReady, AttemptStatusFinalizeUnknown,
			AttemptStatusReconciling, AttemptStatusTerminalUnknown},
		AttemptStatusLeaseLost, "LEASE_LOST", now, &now)
	if err != nil && !errors.Is(err, ErrStateConflict) {
		return err
	}
	return ErrLeaseLost
}

// heartbeatController 持有单 Attempt Heartbeat goroutine 的取消、等待与首个错误。
type heartbeatController struct {
	// cancel 停止 Heartbeat ticker。
	cancel context.CancelFunc
	// done 在 Heartbeat goroutine 完全退出后关闭。
	done chan struct{}
	// errorMu 保护 firstError。
	errorMu sync.Mutex
	// firstError 是首次续租失败或 Lease Lost。
	firstError error
}

// startHeartbeat 启动唯一续租 goroutine；任何续租失败都取消 Attempt，防止无权继续副作用。
func (p *Processor) startHeartbeat(parent context.Context, cancelAttempt context.CancelFunc, lease LeaseRequest) *heartbeatController {
	ctx, cancel := context.WithCancel(parent)
	controller := &heartbeatController{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(controller.done)
		ticker := time.NewTicker(p.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				callContext, callCancel := context.WithTimeout(ctx, p.config.AgentCallTimeout)
				_, err := p.agent.Renew(callContext, lease, p.config.LeaseTTL)
				callCancel()
				if err != nil {
					controller.setError(err)
					cancelAttempt()
					return
				}
			}
		}
	}()
	return controller
}

// stop 取消 Heartbeat 并等待 goroutine 回收，禁止 fire-and-forget。
func (c *heartbeatController) stop() {
	c.cancel()
	<-c.done
}

// setError first-write-wins 保存首个 Heartbeat 失败。
func (c *heartbeatController) setError(err error) {
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	if c.firstError == nil {
		c.firstError = err
	}
}

// err 返回 Heartbeat 首个失败；nil 表示停止前续租均成功。
func (c *heartbeatController) err() error {
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	return c.firstError
}

// FinalizeSemanticDigest 对排除 RequestID、CommandID、RequestDigest 的语义 DTO 做规范 JSON SHA-256。
//
// 同一 Attempt 重启可提出新 CommandID 但保持相同摘要，从 Repository 恢复旧 first-write-wins ID。
func FinalizeSemanticDigest(request FinalizeRequestV1) (string, error) {
	semantic := struct {
		// SchemaVersion 是 Finalize 契约版本。
		SchemaVersion string `json:"schema_version"`
		// PreparationID 是 Business Prepare UUIDv7。
		PreparationID uuid.UUID `json:"preparation_id"`
		// OperationID 是 Agent Operation UUIDv7。
		OperationID uuid.UUID `json:"operation_id"`
		// BatchID 是 Agent Batch UUIDv7。
		BatchID uuid.UUID `json:"batch_id"`
		// JobID 是 Agent Job UUIDv7。
		JobID uuid.UUID `json:"job_id"`
		// AttemptID 是当前 Attempt UUIDv7。
		AttemptID uuid.UUID `json:"attempt_id"`
		// Fence 是当前 Fencing Token。
		Fence int64 `json:"fence"`
		// TerminalStatus 是 ready 或 failed。
		TerminalStatus string `json:"terminal_status"`
		// Output 是 ready 产物摘要。
		Output *FinalizeOutputV1 `json:"output,omitempty"`
		// ErrorCode 是 failed 白名单错误码。
		ErrorCode string `json:"error_code,omitempty"`
	}{
		SchemaVersion: request.SchemaVersion, PreparationID: request.PreparationID,
		OperationID: request.OperationID, BatchID: request.BatchID, JobID: request.JobID,
		AttemptID: request.AttemptID, Fence: request.Fence, TerminalStatus: request.TerminalStatus,
		Output: request.Output, ErrorCode: request.ErrorCode,
	}
	payload, err := json.Marshal(semantic)
	if err != nil {
		return "", fmt.Errorf("marshal Finalize semantic digest: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// terminalPayloadAndDigest 校验成功/失败联合并冻结可提交 Agent 的规范 JSON 与 SHA-256。
func terminalPayloadAndDigest(result TerminalResultV1) (json.RawMessage, string, error) {
	if result.SchemaVersion != TerminalResultSchemaV1 ||
		(result.Status == "succeeded" && (result.AssetRef == nil || result.FinalizationReceiptID == nil || result.ErrorCode != "")) ||
		(result.Status == "failed" && (result.AssetRef != nil || result.FinalizationReceiptID != nil || !isTerminalErrorCode(result.ErrorCode))) ||
		(result.Status != "succeeded" && result.Status != "failed") {
		return nil, "", fmt.Errorf("invalid media terminal result union")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, "", fmt.Errorf("marshal media terminal result: %w", err)
	}
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}

// retryDelayCap 计算有限指数退避上限；实际延迟由 Full Jitter 取 [0, cap)。
func (p *Processor) retryDelayCap(attemptCount int) time.Duration {
	delay := p.config.RetryBaseDelay
	for index := 1; index < attemptCount && delay < p.config.RetryMaxDelay; index++ {
		if delay > p.config.RetryMaxDelay/2 {
			return p.config.RetryMaxDelay
		}
		delay *= 2
	}
	if delay > p.config.RetryMaxDelay {
		return p.config.RetryMaxDelay
	}
	return delay
}

// cryptoFullJitter 使用 crypto/rand 生成 [0,max) 延迟；随机源失败时返回 max/2 的有界确定值。
func cryptoFullJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return max / 2
	}
	return time.Duration(value.Int64())
}

// isTerminalErrorCode 限制 Agent/Business 失败结果只包含跨 Module 稳定白名单。
func isTerminalErrorCode(code string) bool {
	switch code {
	case "FEATURE_DISABLED", "INVALID_ARGUMENT", "NOT_FOUND", "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT",
		"DEPENDENCY_NOT_READY", "UNSUPPORTED_PROFILE", "LEASE_LOST", "FENCE_STALE", "ARTIFACT_INVALID",
		"FFMPEG_UNAVAILABLE", "EXECUTION_TIMEOUT", "UNKNOWN_OUTCOME", "INTERNAL":
		return true
	default:
		return false
	}
}

// stableProcessorErrorCode 把内部错误链收敛为普通日志允许的稳定错误码。
func stableProcessorErrorCode(err error) string {
	if errors.Is(err, ErrLeaseLost) {
		return "LEASE_LOST"
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		return "UNKNOWN_OUTCOME"
	}
	if code := mediapreview.CodeOf(err); code != mediapreview.ErrorCodeInternal {
		return string(code)
	}
	return "INTERNAL"
}
