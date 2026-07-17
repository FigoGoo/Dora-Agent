package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
)

// ProcessorConfig 冻结 Preview Session Lane 的并发、租约、恢复和技术重试预算。
type ProcessorConfig struct {
	Concurrency       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryDelay        time.Duration
	RecoveryDelay     time.Duration
	MaxAttempts       int
}

// Processor 只消费 PostgreSQL 真源；wake 是延迟优化，不承载可靠性或顺序。
type Processor struct {
	repository Repository
	runner     Runner
	clock      Clock
	owner      string
	config     ProcessorConfig

	wake chan struct{}

	mu           sync.Mutex
	started      bool
	intakeCancel context.CancelFunc
	runCancel    context.CancelFunc
	workers      sync.WaitGroup
}

// NewProcessor 创建尚未启动的 Preview Processor。
func NewProcessor(
	repository Repository,
	runner Runner,
	clock Clock,
	owner string,
	config ProcessorConfig,
) (*Processor, error) {
	if repository == nil || runner == nil || clock == nil || owner == "" ||
		config.Concurrency < 1 || config.Concurrency > 32 || config.PollInterval <= 0 ||
		config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 ||
		config.HeartbeatInterval >= config.LeaseDuration || config.RetryDelay <= 0 ||
		config.RecoveryDelay <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("create preview processor: invalid dependency or resource budget")
	}
	return &Processor{
		repository: repository, runner: runner, clock: clock, owner: owner, config: config,
		wake: make(chan struct{}, 1),
	}, nil
}

// Start 启动固定数量 worker；重复启动失败关闭。
func (p *Processor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fmt.Errorf("start preview processor: already started")
	}
	intakeCtx, intakeCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	p.started = true
	p.intakeCancel = intakeCancel
	p.runCancel = runCancel
	for index := 0; index < p.config.Concurrency; index++ {
		p.workers.Add(1)
		go p.worker(intakeCtx, runCtx)
	}
	return nil
}

// Wake 对丢失通知容忍，channel 满时直接返回。
func (p *Processor) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Stop 先停止新 Claim，再等待在途 Runner；超时才取消在途 Context。
func (p *Processor) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	intakeCancel := p.intakeCancel
	runCancel := p.runCancel
	p.mu.Unlock()
	intakeCancel()
	done := make(chan struct{})
	go func() {
		p.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		runCancel()
		return nil
	case <-ctx.Done():
		runCancel()
		// Runner 必须响应 cancel；若第三方实现违约，不得再无界突破调用方 shutdown deadline。
		return fmt.Errorf("stop preview processor: %w", ctx.Err())
	}
}

// ProcessNext 同步执行一次 PostgreSQL Claim 与既有处理状态机；无可领取 HOL 时返回 false。
// Coordinator 只获得是否命中与 Claim 错误，不接触 Claim DTO，也不能跨 source 改写状态。
func (p *Processor) ProcessNext(ctx context.Context) (bool, error) {
	return p.processNext(ctx, ctx)
}

func (p *Processor) processNext(claimCtx context.Context, runCtx context.Context) (bool, error) {
	claim, err := p.repository.ClaimNext(
		claimCtx, p.owner, p.clock.Now().UTC(), p.config.LeaseDuration,
	)
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

func (p *Processor) process(runCtx context.Context, claim Claim) {
	now := p.clock.Now().UTC()
	if err := p.repository.MarkRunning(runCtx, claim, now); err != nil {
		return
	}
	// Tool Result 已冻结时直接恢复投影；Complete/进程崩溃不得重新进入 Router、模型或 Graph。
	receipt, loadErr := p.repository.LoadReceipt(runCtx, claim)
	if loadErr != nil {
		_ = p.repository.DeferProjection(runCtx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
		return
	}
	if receipt.Terminal != nil {
		p.completeTerminal(runCtx, claim, *receipt.Terminal)
		return
	}
	if claim.Poisoned {
		if receipt.RecoveryOnly() {
			// prepared/unknown 可能已在 Business 提交；Intent poison 也不得覆盖原命令终态。
			_ = p.repository.DeferRecovery(runCtx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
			return
		}
		p.handleExecutionFailure(runCtx, claim, true)
		return
	}
	claimRunCtx, cancelClaimRun := context.WithCancel(runCtx)
	heartbeatDone := make(chan error, 1)
	go p.heartbeat(claimRunCtx, cancelClaimRun, claim, heartbeatDone)
	runErr := p.runner.Run(claimRunCtx, claim)
	cancelClaimRun()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil || runCtx.Err() != nil {
		return
	}

	receipt, loadErr = p.repository.LoadReceipt(runCtx, claim)
	if loadErr != nil {
		// Runner 可能已冻结权威终态；回执读取故障绝不能消耗 execution max 或转 dead。
		_ = p.repository.DeferProjection(runCtx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
		return
	}
	if receipt.Terminal != nil {
		p.completeTerminal(runCtx, claim, *receipt.Terminal)
		return
	}
	if receipt.RecoveryOnly() || errors.Is(runErr, plancreationspec.ErrBusinessUnknownOutcome) {
		_ = p.repository.DeferRecovery(runCtx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
		return
	}
	p.handleExecutionFailure(runCtx, claim, false)
}

func (p *Processor) handleExecutionFailure(ctx context.Context, claim Claim, poisoned bool) {
	if claim.Attempts < p.config.MaxAttempts {
		_ = p.repository.RetryExecution(ctx, claim, p.clock.Now().UTC().Add(p.config.RetryDelay))
		return
	}
	result := plancreationspec.Result{
		Status: "failed", ResultCode: plancreationspec.ResultCodeRuntimeProcessingFailed,
		ReceiptRef: plancreationspec.ReceiptRef{
			ToolCallID: claim.ToolCallID, BusinessCommandID: claim.BusinessCommandID,
		},
		Summary: "创作规格预览处理失败，请重新提交。", Retryable: false,
	}
	if poisoned {
		result.ResultCode = plancreationspec.ResultCodeRuntimeInputInvalid
		result.Summary = "创作规格预览输入无法安全处理，请重新提交。"
	}
	if err := p.repository.FreezeExecutionFailure(ctx, claim, result); err != nil {
		// 冻结故障可能意味着另一终态已经写入；只进入无限有界投影恢复。
		_ = p.repository.DeferProjection(ctx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
		return
	}
	p.completeTerminal(ctx, claim, Terminal{Result: result, Disposition: CompletionDead})
}

func (p *Processor) completeTerminal(ctx context.Context, claim Claim, terminal Terminal) {
	if err := p.repository.Complete(ctx, claim, terminal, p.clock.Now().UTC()); err != nil {
		// 已冻结终态的投影失败不能改写为 dead 或重跑 Router/Tool/Model。
		_ = p.repository.DeferProjection(ctx, claim, p.clock.Now().UTC().Add(p.config.RecoveryDelay))
	}
}

func (p *Processor) heartbeat(ctx context.Context, cancelRun context.CancelFunc, claim Claim, done chan<- error) {
	ticker := time.NewTicker(p.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := p.repository.RenewLease(ctx, claim, p.clock.Now().UTC(), p.config.LeaseDuration); err != nil {
				cancelRun()
				done <- err
				return
			}
		}
	}
}
