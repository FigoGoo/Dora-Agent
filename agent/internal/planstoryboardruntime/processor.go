package planstoryboardruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
)

// ProcessorConfig 冻结本 Profile 的 worker、Lease、heartbeat、重试和恢复轮询预算。
type ProcessorConfig struct {
	Concurrency       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryDelay        time.Duration
	RecoveryDelay     time.Duration
	ProjectionDelay   time.Duration
	MaxAttempts       int
}

// Repository 是 Processor 的持久真源端口；ClaimNext 必须先在 PostgreSQL 计算全 Source HOL。
type Repository interface {
	ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error)
	MarkRunning(context.Context, Claim, time.Time) error
	RenewLease(context.Context, Claim, time.Time, time.Duration) error
	LoadToolReceipt(context.Context, Claim) (ToolReceiptSnapshot, error)
	CompleteToolResult(context.Context, Claim, planstoryboard.Result, time.Time) error
	CompleteRuntimeFailure(context.Context, Claim, RuntimeFailure, time.Time) error
	RetryExecution(context.Context, Claim, time.Time) error
	DeferRecovery(context.Context, Claim, time.Time) error
	DeferProjection(context.Context, Claim, time.Time) error
}

// ExecutionStore 是 HTTP 入队与 Processor 共享的最小持久化端口。
type ExecutionStore interface {
	Repository
	Enqueue(context.Context, EnqueueCommand, time.Time) (EnqueueResult, error)
}

// Processor 实现 receipt-first、全 Source HOL/Fence 和 prepared 后只恢复不重跑 Agent。
type Processor struct {
	repository Repository
	runner     Runner
	recovery   Recovery
	clock      Clock
	owner      string
	config     ProcessorConfig
	wake       chan struct{}
	mu         sync.Mutex
	started    bool
	intakeStop context.CancelFunc
	runStop    context.CancelFunc
	workers    sync.WaitGroup
}

// NewProcessor 创建尚未启动的固定预算 Processor。
func NewProcessor(
	repository Repository,
	runner Runner,
	recovery Recovery,
	clock Clock,
	owner string,
	config ProcessorConfig,
) (*Processor, error) {
	if repository == nil || runner == nil || recovery == nil || clock == nil || owner == "" ||
		config.Concurrency < 1 || config.Concurrency > 32 || config.PollInterval <= 0 || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration || config.RetryDelay <= 0 ||
		config.RecoveryDelay <= 0 || config.ProjectionDelay <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("create plan storyboard processor: invalid dependency or resource budget")
	}
	return &Processor{
		repository: repository, runner: runner, recovery: recovery, clock: clock, owner: owner,
		config: config, wake: make(chan struct{}, 1),
	}, nil
}

// Start 启动固定 worker；Bootstrap 必须保证与其他互斥 Preview Profile 不同时开启。
func (p *Processor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fmt.Errorf("start plan storyboard processor: already started")
	}
	intakeCtx, intakeStop := context.WithCancel(context.Background())
	runCtx, runStop := context.WithCancel(context.Background())
	p.started, p.intakeStop, p.runStop = true, intakeStop, runStop
	for index := 0; index < p.config.Concurrency; index++ {
		p.workers.Add(1)
		go p.worker(intakeCtx, runCtx)
	}
	return nil
}

// Wake 是可丢失的延迟优化；正确性仍由 PostgreSQL Scanner 提供。
func (p *Processor) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Stop 先停 Claim，再等待在途执行；调用方 deadline 到期才取消运行。
func (p *Processor) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	intakeStop, runStop := p.intakeStop, p.runStop
	p.mu.Unlock()
	intakeStop()
	done := make(chan struct{})
	go func() { p.workers.Wait(); close(done) }()
	select {
	case <-done:
		runStop()
		return nil
	case <-ctx.Done():
		runStop()
		return fmt.Errorf("stop plan storyboard processor: %w", ctx.Err())
	}
}

// ProcessNext 同步执行一次全来源 HOL Claim 与既有 plan_storyboard 状态机；无工作时返回 false。
func (p *Processor) ProcessNext(ctx context.Context) (bool, error) {
	return p.processNext(ctx, ctx)
}

func (p *Processor) processNext(claimCtx context.Context, runCtx context.Context) (bool, error) {
	claim, err := p.repository.ClaimNext(claimCtx, p.owner, p.clock.Now().UTC(), p.config.LeaseDuration)
	if err != nil || claim == nil {
		return false, err
	}
	p.process(runCtx, *claim)
	return true, nil
}

func (p *Processor) worker(intakeCtx, runCtx context.Context) {
	defer p.workers.Done()
	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-intakeCtx.Done():
			return
		case <-ticker.C:
		case <-p.wake:
		}
		for intakeCtx.Err() == nil {
			claimed, err := p.processNext(intakeCtx, runCtx)
			if err != nil || !claimed {
				break
			}
		}
	}
}

func (p *Processor) process(ctx context.Context, claim Claim) {
	if ValidateClaim(claim) != nil {
		p.runtimeFailure(ctx, claim)
		return
	}
	if err := p.repository.MarkRunning(ctx, claim, p.clock.Now().UTC()); err != nil {
		return
	}
	snapshot, err := p.repository.LoadToolReceipt(ctx, claim)
	if err != nil {
		p.retryOrRuntimeFailure(ctx, claim)
		return
	}
	result, terminal, receiptErr := validateClaimToolReceipt(claim, snapshot)
	if receiptErr != nil {
		p.runtimeFailure(ctx, claim)
		return
	}
	if terminal {
		p.complete(ctx, claim, snapshot, result)
		return
	}
	if snapshot.Stage == ToolReceiptBusinessPrepared || snapshot.Stage == ToolReceiptBusinessUnknown {
		p.recover(ctx, claim, snapshot)
		return
	}
	if snapshot.Stage != ToolReceiptOpen {
		p.runtimeFailure(ctx, claim)
		return
	}

	runErr, heartbeatErr := p.withHeartbeat(ctx, claim, func(runCtx context.Context) error {
		_, err := p.runner.Run(runCtx, claim)
		return err
	})
	if heartbeatErr != nil || ctx.Err() != nil {
		return
	}
	// Runner/Tool Wrapper 返回后只相信 PostgreSQL Receipt，不直接投影内存结果。
	snapshot, err = p.repository.LoadToolReceipt(ctx, claim)
	if err != nil {
		p.retryOrRuntimeFailure(ctx, claim)
		return
	}
	result, terminal, receiptErr = validateClaimToolReceipt(claim, snapshot)
	if receiptErr != nil {
		p.runtimeFailure(ctx, claim)
		return
	}
	if terminal {
		p.complete(ctx, claim, snapshot, result)
		return
	}
	if snapshot.Stage == ToolReceiptBusinessPrepared || snapshot.Stage == ToolReceiptBusinessUnknown {
		p.deferRecovery(ctx, claim)
		return
	}
	if runErr != nil {
		p.retryOrRuntimeFailure(ctx, claim)
		return
	}
	// Runner 无错误却仍为 open 是执行契约破坏。
	p.runtimeFailure(ctx, claim)
}

func (p *Processor) recover(ctx context.Context, claim Claim, snapshot ToolReceiptSnapshot) {
	runErr, heartbeatErr := p.withHeartbeat(ctx, claim, func(runCtx context.Context) error {
		return p.recovery.Recover(runCtx, claim, snapshot)
	})
	if heartbeatErr != nil || ctx.Err() != nil {
		return
	}
	if errors.Is(runErr, ErrFenceLost) {
		return
	}
	if runErr != nil && !errors.Is(runErr, ErrRecoveryDeferred) && !errors.Is(runErr, ErrPersistence) {
		p.runtimeFailure(ctx, claim)
		return
	}
	// prepared 已跨过潜在副作用边界；暂态/未决结果只重读 Receipt，永久契约冲突已在上方终结为 runtime failure。
	reloaded, err := p.repository.LoadToolReceipt(ctx, claim)
	if err != nil {
		if errors.Is(err, ErrFenceLost) {
			return
		}
		if errors.Is(err, ErrReceiptConflict) || errors.Is(err, ErrInvalidClaim) || errors.Is(err, ErrOutputContract) {
			p.runtimeFailure(ctx, claim)
			return
		}
		p.deferRecovery(ctx, claim)
		return
	}
	result, terminal, receiptErr := validateClaimToolReceipt(claim, reloaded)
	if receiptErr == nil && terminal {
		p.complete(ctx, claim, reloaded, result)
		return
	}
	if receiptErr != nil {
		p.runtimeFailure(ctx, claim)
		return
	}
	p.deferRecovery(ctx, claim)
}

func validateClaimToolReceipt(claim Claim, snapshot ToolReceiptSnapshot) (planstoryboard.Result, bool, error) {
	switch snapshot.Stage {
	case ToolReceiptOpen:
		if snapshot.PreparedCommand != nil || snapshot.PreparedCommandDigest != "" || snapshot.ContentDigest != "" ||
			snapshot.Recovery != nil || len(snapshot.ResultJSON) != 0 || snapshot.ResultDigest != "" {
			return planstoryboard.Result{}, false, ErrReceiptConflict
		}
		return planstoryboard.Result{}, false, nil
	case ToolReceiptBusinessPrepared, ToolReceiptBusinessUnknown:
		_, _, err := validateRecoverySnapshot(claim, snapshot)
		return planstoryboard.Result{}, false, err
	case ToolReceiptCompleted, ToolReceiptFailed:
		trusted := RuntimeContextFromClaim(claim)
		result, canonical, err := decodeToolResult(snapshot.ResultJSON, trusted)
		if err != nil || ToolReceiptStage(result.Status) != snapshot.Stage || digestBytes(canonical) != snapshot.ResultDigest ||
			snapshot.RequestDigest != digestToolRequest(claim.Context, claim.Context.IntentDigest) {
			return planstoryboard.Result{}, false, ErrReceiptConflict
		}
		if snapshot.Stage == ToolReceiptCompleted {
			if snapshot.PreparedCommand == nil {
				return planstoryboard.Result{}, false, ErrReceiptConflict
			}
			if err := validateCompletedCommand(claim, snapshot, result); err != nil {
				return planstoryboard.Result{}, false, err
			}
		}
		return result, true, nil
	default:
		return planstoryboard.Result{}, false, ErrReceiptConflict
	}
}

func validateCompletedCommand(claim Claim, snapshot ToolReceiptSnapshot, result planstoryboard.Result) error {
	command := *snapshot.PreparedCommand
	if command.TrustedContext != CoreContextFromRuntime(RuntimeContextFromClaim(claim)) || result.ResourceRef == nil {
		return ErrReceiptConflict
	}
	commandDigest, err := digestPreparedCommand(command)
	if err != nil || commandDigest != snapshot.PreparedCommandDigest {
		return ErrReceiptConflict
	}
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil || contentDigest != snapshot.ContentDigest || contentDigest != result.ResourceRef.Digest ||
		result.ResourceRef.CreationSpecRef != command.TrustedContext.CreationSpecRef {
		return ErrReceiptConflict
	}
	return nil
}

func (p *Processor) complete(ctx context.Context, claim Claim, snapshot ToolReceiptSnapshot, result planstoryboard.Result) {
	if result.Status == "completed" {
		result.Card = cardFromPreparedResult(*snapshot.PreparedCommand, result, p.clock.Now().UTC())
		if planstoryboard.ValidateTerminalResult(result, CoreContextFromRuntime(RuntimeContextFromClaim(claim))) != nil {
			p.runtimeFailure(ctx, claim)
			return
		}
	}
	if err := p.repository.CompleteToolResult(ctx, claim, result, p.clock.Now().UTC()); err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrFenceLost):
			return
		case errors.Is(err, ErrOutputContract), errors.Is(err, ErrReceiptConflict), errors.Is(err, ErrInvalidClaim):
			// 已冻结 Result 的确定性契约冲突不会因再次投影而改变，必须终结为独立 Runtime Failure。
			p.runtimeFailure(ctx, claim)
		default:
			// 数据库提交结果未知仍只补投影，绝不重跑模型或 Business Save。
			p.deferProjection(ctx, claim)
		}
	}
}

func cardFromPreparedResult(command planstoryboard.DraftCommand, result planstoryboard.Result, now time.Time) *planstoryboard.Card {
	ref := result.ResourceRef
	if ref == nil {
		return nil
	}
	return &planstoryboard.Card{
		SchemaVersion: planstoryboard.CardSchemaVersion, StoryboardPreviewID: ref.StoryboardPreviewID,
		ProjectID: command.TrustedContext.ProjectID, CreationSpecRef: ref.CreationSpecRef, Version: ref.Version,
		Status: ref.Status, ContentDigest: ref.Digest, Title: command.Content.Title, Summary: command.Content.Summary,
		Sections: cloneStoryboardSections(command.Content.Sections),
		Elements: cloneStoryboardElements(command.Content.Elements),
		Slots:    cloneStoryboardSlots(command.Content.Slots), UpdatedAt: now.UTC(),
	}
}

func (p *Processor) runtimeFailure(ctx context.Context, claim Claim) {
	failure := RuntimeFailure{
		SchemaVersion: "plan_storyboard.preview.runtime_failure.v1", InputID: claim.Context.InputID,
		TurnID: claim.Context.TurnID, RunID: claim.Context.RunID, Code: "PLAN_STORYBOARD_RUNTIME_FAILED",
		Summary: "故事板规划运行时暂时无法完成", Retryable: false,
	}
	if p.repository.CompleteRuntimeFailure(ctx, claim, failure, p.clock.Now().UTC()) != nil {
		_ = p.repository.RetryExecution(ctx, claim, p.clock.Now().UTC().Add(p.config.RetryDelay))
	}
}

func (p *Processor) retryOrRuntimeFailure(ctx context.Context, claim Claim) {
	if claim.Attempts < p.config.MaxAttempts {
		_ = p.repository.RetryExecution(ctx, claim, p.clock.Now().UTC().Add(p.config.RetryDelay))
		return
	}
	p.runtimeFailure(ctx, claim)
}

func (p *Processor) deferRecovery(ctx context.Context, claim Claim) {
	_ = p.repository.DeferRecovery(ctx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
}

func (p *Processor) deferProjection(ctx context.Context, claim Claim) {
	_ = p.repository.DeferProjection(ctx, claim, p.clock.Now().UTC().Add(p.config.ProjectionDelay))
}

func (p *Processor) withHeartbeat(
	ctx context.Context,
	claim Claim,
	run func(context.Context) error,
) (error, error) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go p.heartbeat(runCtx, cancel, claim, done)
	runErr := run(runCtx)
	cancel()
	return runErr, <-done
}

func (p *Processor) heartbeat(ctx context.Context, cancel context.CancelFunc, claim Claim, done chan<- error) {
	ticker := time.NewTicker(p.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := p.repository.RenewLease(ctx, claim, p.clock.Now().UTC(), p.config.LeaseDuration); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}
