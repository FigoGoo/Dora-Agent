package analyzematerialsruntime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
)

// ProcessorConfig 冻结本 Profile 的 worker、Lease、heartbeat 与重试预算。
type ProcessorConfig struct {
	Concurrency       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryDelay        time.Duration
	ProjectionDelay   time.Duration
	MaxAttempts       int
}

// Repository 是 Processor 的持久真源端口；ClaimNext 必须在 PostgreSQL 内先计算全 Source HOL。
type Repository interface {
	ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error)
	MarkRunning(context.Context, Claim, time.Time) error
	RenewLease(context.Context, Claim, time.Time, time.Duration) error
	LoadToolReceipt(context.Context, Claim) (ToolReceiptSnapshot, error)
	CompleteToolResult(context.Context, Claim, analyzematerials.Result, time.Time) error
	CompleteRuntimeFailure(context.Context, Claim, RuntimeFailure, time.Time) error
	RetryExecution(context.Context, Claim, time.Time) error
	DeferProjection(context.Context, Claim, time.Time) error
}

// ExecutionStore 是 HTTP 入队与 Processor 共享的完整最小持久化端口。
// Enqueue 必须在一个事务中写 typed Intent、Input、Turn Context、Run identity、open Tool Receipt 与 accepted Event。
type ExecutionStore interface {
	Repository
	Enqueue(context.Context, EnqueueCommand, time.Time) (EnqueueResult, error)
}

// Clock 为状态迁移注入 UTC 时间。
type Clock interface{ Now() time.Time }

// Processor 实现 receipt-first、Scanner 正确性路径和有界并发。
type Processor struct {
	repository   Repository
	runner       Runner
	clock        Clock
	owner        string
	config       ProcessorConfig
	wake         chan struct{}
	mu           sync.Mutex
	started      bool
	intakeCancel context.CancelFunc
	runCancel    context.CancelFunc
	workers      sync.WaitGroup
}

// NewProcessor 创建尚未启动的独立 Processor。
func NewProcessor(repository Repository, runner Runner, clock Clock, owner string, config ProcessorConfig) (*Processor, error) {
	if repository == nil || runner == nil || clock == nil || owner == "" || config.Concurrency < 1 || config.Concurrency > 32 || config.PollInterval <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration || config.RetryDelay <= 0 || config.ProjectionDelay <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("create analyze materials processor: invalid dependency or resource budget")
	}
	return &Processor{repository: repository, runner: runner, clock: clock, owner: owner, config: config, wake: make(chan struct{}, 1)}, nil
}

// Start 启动固定 worker；Bootstrap 必须保证其他 Preview Processor 均关闭。
func (p *Processor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fmt.Errorf("start analyze materials processor: already started")
	}
	intakeCtx, intakeCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	p.started, p.intakeCancel, p.runCancel = true, intakeCancel, runCancel
	for index := 0; index < p.config.Concurrency; index++ {
		p.workers.Add(1)
		go p.worker(intakeCtx, runCtx)
	}
	return nil
}

// Wake 是可丢失的延迟优化；Scanner 仍按 PollInterval 扫描 PostgreSQL。
func (p *Processor) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Stop 先停止 Claim，再等待在途执行；调用方 deadline 到期才取消运行。
func (p *Processor) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	intakeCancel, runCancel := p.intakeCancel, p.runCancel
	p.mu.Unlock()
	intakeCancel()
	done := make(chan struct{})
	go func() { p.workers.Wait(); close(done) }()
	select {
	case <-done:
		runCancel()
		return nil
	case <-ctx.Done():
		runCancel()
		return fmt.Errorf("stop analyze materials processor: %w", ctx.Err())
	}
}

// ProcessNext 同步执行一次全来源 HOL Claim 与既有 analyze_materials 状态机；无工作时返回 false。
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
	if result, frozen, err := validateClaimToolReceipt(claim, snapshot); err != nil {
		p.runtimeFailure(ctx, claim)
		return
	} else if frozen {
		p.complete(ctx, claim, result)
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go p.heartbeat(runCtx, cancel, claim, done)
	_, runErr := p.runner.Run(runCtx, claim)
	cancel()
	heartbeatErr := <-done
	if heartbeatErr != nil || ctx.Err() != nil {
		return
	}
	// Runner/Tool Wrapper 返回后只相信 PostgreSQL frozen Receipt，不直接投影内存结果。
	snapshot, err = p.repository.LoadToolReceipt(ctx, claim)
	if err != nil {
		p.retryOrRuntimeFailure(ctx, claim)
		return
	}
	result, frozen, receiptErr := validateClaimToolReceipt(claim, snapshot)
	if receiptErr != nil {
		p.runtimeFailure(ctx, claim)
		return
	}
	if frozen {
		p.complete(ctx, claim, result)
		return
	}
	if runErr != nil {
		p.retryOrRuntimeFailure(ctx, claim)
		return
	}
	// Runner 没有报错却未形成 frozen ToolReceipt 是执行契约破坏，不能伪装成投影恢复。
	p.runtimeFailure(ctx, claim)
}

func validateClaimToolReceipt(claim Claim, snapshot ToolReceiptSnapshot) (analyzematerials.Result, bool, error) {
	if snapshot.Stage == ToolReceiptOpen {
		if len(snapshot.ResultJSON) != 0 || snapshot.ResultDigest != "" {
			return analyzematerials.Result{}, false, ErrReceiptConflict
		}
		return analyzematerials.Result{}, false, nil
	}
	result, canonical, err := decodeToolResult(snapshot.ResultJSON, RuntimeContextFromClaim(claim))
	if err != nil || ToolReceiptStage(result.Status) != snapshot.Stage || digestBytes(canonical) != snapshot.ResultDigest {
		return analyzematerials.Result{}, false, ErrReceiptConflict
	}
	return result, true, nil
}

func (p *Processor) complete(ctx context.Context, claim Claim, result analyzematerials.Result) {
	if p.repository.CompleteToolResult(ctx, claim, result, p.clock.Now().UTC()) != nil {
		p.deferProjection(ctx, claim)
	}
}
func (p *Processor) runtimeFailure(ctx context.Context, claim Claim) {
	failure := RuntimeFailure{SchemaVersion: "analyze_materials.preview.runtime_failure.v1", InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID, Code: "ANALYZE_MATERIALS_RUNTIME_FAILED", Summary: "素材分析运行时暂时无法完成", Retryable: false}
	if p.repository.CompleteRuntimeFailure(ctx, claim, failure, p.clock.Now().UTC()) != nil {
		// 尚无 frozen Tool Result 时不得进入 projection recovery；释放为执行重试后，
		// 更高 Fence 会直接重放本运行时失败收口，不会再次调用 Tool。
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
func (p *Processor) deferProjection(ctx context.Context, claim Claim) {
	_ = p.repository.DeferProjection(ctx, claim, p.clock.Now().UTC().Add(p.config.ProjectionDelay))
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
