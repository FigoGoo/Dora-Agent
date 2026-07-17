package usermessageruntime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProcessorConfig 冻结本 Profile 的固定 worker、Lease、heartbeat、retry 与 projection recovery 预算。
type ProcessorConfig struct {
	Concurrency       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryDelay        time.Duration
	ProjectionDelay   time.Duration
	MaxAttempts       int
}

// Repository 是 B3 Processor 依赖的领域端口；统一 HOL 选择与物理事务由 B2/PostgreSQL Adapter 负责。
type Repository interface {
	// ClaimNext 必须先选择每个 Session 的全 source 真正 HOL，再由 dispatcher 交付 eligible user_message。
	ClaimNext(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (*Claim, error)
	// MarkRunning 使用 owner/fence CAS，并创建或复用首次 Claim 的稳定 Run。
	MarkRunning(ctx context.Context, claim Claim, now time.Time) error
	// RenewLease 只允许当前 owner/fence 延长 Session Lane Lease。
	RenewLease(ctx context.Context, claim Claim, now time.Time, leaseDuration time.Duration) error
	// LoadFrozenOutput 返回 open/completed/failed 真源；Processor 在任何 Runner 调用前必须先调用它。
	LoadFrozenOutput(ctx context.Context, claim Claim) (OutputReceiptSnapshot, error)
	// FreezeOutput first-write-wins 冻结完整 Direct Response 或 Failure DTO；不承担 Projection。
	FreezeOutput(ctx context.Context, claim Claim, output Output, now time.Time) error
	// Complete 在一个事务中重放 Projection/Event、CAS Input/Turn/Run 并释放当前 owner/fence Lease。
	Complete(ctx context.Context, claim Claim, output Output, now time.Time) error
	// RetryExecution 只对尚无冻结 Output 的确定技术失败进入 retry_wait。
	RetryExecution(ctx context.Context, claim Claim, availableAt time.Time) error
	// DeferProjection 对 Receipt 读取、Freeze 响应未知或 Complete 故障有界退避，绝不重调模型。
	DeferProjection(ctx context.Context, claim Claim, availableAt time.Time) error
}

// Clock 为状态迁移冻结 UTC 时间；生产实现使用系统时钟，SQL 资格判断仍必须使用 PostgreSQL 时间。
type Clock interface {
	Now() time.Time
}

// Processor 只消费持久真源；wake 是可丢失延迟优化，scanner 才是可靠兜底。
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

// NewProcessor 创建尚未启动的隔离 B3 Processor。
func NewProcessor(repository Repository, runner Runner, clock Clock, owner string, config ProcessorConfig) (*Processor, error) {
	if repository == nil || runner == nil || clock == nil || owner == "" ||
		config.Concurrency < 1 || config.Concurrency > 32 || config.PollInterval <= 0 ||
		config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 ||
		config.HeartbeatInterval >= config.LeaseDuration || config.RetryDelay <= 0 ||
		config.ProjectionDelay <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("create user message processor: invalid dependency or resource budget")
	}
	return &Processor{
		repository: repository, runner: runner, clock: clock, owner: owner, config: config,
		wake: make(chan struct{}, 1),
	}, nil
}

// Start 启动固定 worker；Bootstrap 必须保证它不会与旧 Preview Claim loop 重叠。
func (p *Processor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fmt.Errorf("start user message processor: already started")
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

// Wake 容忍通知丢失；channel 满时 scanner 仍会在 PollInterval 内恢复。
func (p *Processor) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Stop 先停止新 Claim，再 drain 在途 Runner；调用方 deadline 到期后才取消运行 Context。
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
		return fmt.Errorf("stop user message processor: %w", ctx.Err())
	}
}

// ProcessNext 同步执行一次全来源 HOL Claim 与既有 user_message 状态机；无工作时返回 false。
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

func (p *Processor) process(runCtx context.Context, claim Claim) {
	if err := p.repository.MarkRunning(runCtx, claim, p.clock.Now().UTC()); err != nil {
		return
	}
	// Receipt-first：已有冻结 Output 时只重放 Projection/Event，永不进入 Runner/Model。
	snapshot, err := p.repository.LoadFrozenOutput(runCtx, claim)
	if err != nil || ValidateOutputReceipt(snapshot, claim) != nil {
		p.deferProjection(runCtx, claim)
		return
	}
	if snapshot.Output != nil {
		p.completeFrozen(runCtx, claim, *snapshot.Output)
		return
	}
	if claim.Poisoned {
		p.handleExecutionFailure(runCtx, claim, true)
		return
	}

	claimRunCtx, cancelClaimRun := context.WithCancel(runCtx)
	heartbeatDone := make(chan error, 1)
	go p.heartbeat(claimRunCtx, cancelClaimRun, claim, heartbeatDone)
	output, runErr := p.runner.Run(claimRunCtx, claim)
	cancelClaimRun()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil || runCtx.Err() != nil {
		return
	}

	// Runner 之后再次 receipt-first：未来适配器或并发恢复可能已经冻结首写 Output。
	snapshot, err = p.repository.LoadFrozenOutput(runCtx, claim)
	if err != nil || ValidateOutputReceipt(snapshot, claim) != nil {
		p.deferProjection(runCtx, claim)
		return
	}
	if snapshot.Output != nil {
		p.completeFrozen(runCtx, claim, *snapshot.Output)
		return
	}
	if runErr != nil || ValidateOutput(output, claim) != nil {
		p.handleExecutionFailure(runCtx, claim, false)
		return
	}
	if err := p.repository.FreezeOutput(runCtx, claim, output, p.clock.Now().UTC()); err != nil {
		// Freeze 可能已提交但响应丢失；只进入回执/投影恢复，禁止重跑。
		p.deferProjection(runCtx, claim)
		return
	}
	// FreezeOutput 必须 first-write-wins；重读确保竞争时只投影数据库首写结果。
	snapshot, err = p.repository.LoadFrozenOutput(runCtx, claim)
	if err != nil || ValidateOutputReceipt(snapshot, claim) != nil || snapshot.Output == nil {
		p.deferProjection(runCtx, claim)
		return
	}
	p.completeFrozen(runCtx, claim, *snapshot.Output)
}

func (p *Processor) handleExecutionFailure(ctx context.Context, claim Claim, invalidInput bool) {
	if claim.Attempts < p.config.MaxAttempts {
		_ = p.repository.RetryExecution(ctx, claim, p.clock.Now().UTC().Add(p.config.RetryDelay))
		return
	}
	failure := NewFailure(claim, invalidInput)
	output := Output{Failure: &failure}
	if err := p.repository.FreezeOutput(ctx, claim, output, p.clock.Now().UTC()); err != nil {
		p.deferProjection(ctx, claim)
		return
	}
	snapshot, err := p.repository.LoadFrozenOutput(ctx, claim)
	if err != nil || ValidateOutputReceipt(snapshot, claim) != nil || snapshot.Output == nil {
		p.deferProjection(ctx, claim)
		return
	}
	p.completeFrozen(ctx, claim, *snapshot.Output)
}

func (p *Processor) completeFrozen(ctx context.Context, claim Claim, output Output) {
	if err := p.repository.Complete(ctx, claim, output, p.clock.Now().UTC()); err != nil {
		p.deferProjection(ctx, claim)
	}
}

func (p *Processor) deferProjection(ctx context.Context, claim Claim) {
	_ = p.repository.DeferProjection(ctx, claim, p.clock.Now().UTC().Add(p.config.ProjectionDelay))
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
