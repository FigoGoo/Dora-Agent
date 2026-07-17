package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
)

const (
	processorTestSessionID       = "019f68e8-3001-7000-8000-000000000001"
	processorTestInputID         = "019f68e8-3002-7000-8000-000000000002"
	processorTestToolCallID      = "019f68e8-3003-7000-8000-000000000003"
	processorTestBusinessCmdID   = "019f68e8-3004-7000-8000-000000000004"
	processorTestTerminalEventID = "019f68e8-3005-7000-8000-000000000005"
)

var processorTestNow = time.Date(2026, 7, 16, 13, 0, 0, 0, time.UTC)

// TestProcessorReplaysFrozenTerminalWithoutRunner 验证已冻结终态直接投影；Complete 故障只延后投影且绝不重跑 Runner 或消耗执行重试。
func TestProcessorReplaysFrozenTerminalWithoutRunner(t *testing.T) {
	tests := []struct {
		name            string
		loadErr         error
		completeErr     error
		wantComplete    int
		wantProjection  int
		wantLoadCalls   int
		wantDisposition CompletionDisposition
	}{
		{
			name: "complete frozen terminal", wantComplete: 1, wantLoadCalls: 1,
			wantDisposition: CompletionResolved,
		},
		{
			name: "complete failure defers projection", completeErr: ErrPersistence,
			wantComplete: 1, wantProjection: 1, wantLoadCalls: 1, wantDisposition: CompletionResolved,
		},
		{
			name: "terminal read failure defers projection", loadErr: ErrPersistence,
			wantProjection: 1, wantLoadCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			terminal := processorTestTerminal(CompletionResolved)
			repository := &processorTestRepository{
				loadResponses: []processorTestLoadResponse{{terminal: &terminal, err: test.loadErr}},
				completeErr:   test.completeErr,
			}
			runner := &processorTestRunner{}
			processor := newProcessorForTest(t, repository, runner)
			claim := processorTestClaim(1, false)

			processor.process(context.Background(), claim)

			snapshot := repository.snapshot()
			if runner.callCount() != 0 || snapshot.loadCalls != test.wantLoadCalls || snapshot.completeCalls != test.wantComplete ||
				snapshot.deferProjectionCalls != test.wantProjection || snapshot.retryExecutionCalls != 0 ||
				snapshot.deferRecoveryCalls != 0 || snapshot.freezeFailureCalls != 0 {
				t.Fatalf("冻结终态路径调用异常: runner=%d snapshot=%+v", runner.callCount(), snapshot)
			}
			if test.wantComplete == 1 {
				if len(snapshot.completed) != 1 || snapshot.completed[0].Disposition != test.wantDisposition ||
					snapshot.completeClaims[0].TerminalEventID != processorTestTerminalEventID {
					t.Fatalf("Complete 未复用冻结终态或预分配 Event ID: terminals=%+v claims=%+v", snapshot.completed, snapshot.completeClaims)
				}
			}
		})
	}
}

// TestProcessorPoisonedClaimStillCompletesFrozenTerminal 验证 poison 只描述输入回读；已有 frozen terminal 时必须先完成投影，不能覆盖为 runtime failed。
func TestProcessorPoisonedClaimStillCompletesFrozenTerminal(t *testing.T) {
	terminal := processorTestTerminal(CompletionResolved)
	repository := &processorTestRepository{loadResponses: []processorTestLoadResponse{{terminal: &terminal}}}
	runner := &processorTestRunner{}
	processor := newProcessorForTest(t, repository, runner)

	processor.process(context.Background(), processorTestClaim(2, true))

	snapshot := repository.snapshot()
	if runner.callCount() != 0 || snapshot.loadCalls != 1 || snapshot.completeCalls != 1 ||
		snapshot.freezeFailureCalls != 0 || snapshot.retryExecutionCalls != 0 ||
		snapshot.deferRecoveryCalls != 0 || snapshot.deferProjectionCalls != 0 {
		t.Fatalf("poisoned claim 覆盖了已冻结终态: runner=%d snapshot=%+v", runner.callCount(), snapshot)
	}
}

// TestProcessorFrozenTerminalAfterRunnerWins 验证 Runner 即使返回技术错误，只要终态已经冻结，Processor 仍只投影该终态而不进入 execution retry。
func TestProcessorFrozenTerminalAfterRunnerWins(t *testing.T) {
	terminal := processorTestTerminal(CompletionResolved)
	repository := &processorTestRepository{loadResponses: []processorTestLoadResponse{{}, {terminal: &terminal}}}
	runner := &processorTestRunner{err: errors.New("runner stream ended after terminal freeze")}
	processor := newProcessorForTest(t, repository, runner)

	processor.process(context.Background(), processorTestClaim(1, false))

	snapshot := repository.snapshot()
	if runner.callCount() != 1 || snapshot.loadCalls != 2 || snapshot.completeCalls != 1 ||
		snapshot.retryExecutionCalls != 0 || snapshot.deferProjectionCalls != 0 || snapshot.freezeFailureCalls != 0 {
		t.Fatalf("Runner 后冻结终态未优先投影: runner=%d snapshot=%+v", runner.callCount(), snapshot)
	}
}

// TestProcessorTerminalReadFailureAfterRunnerDefersProjection 验证 Runner 可能已冻结终态时，二次读取故障只进入投影恢复而不消耗 execution max。
func TestProcessorTerminalReadFailureAfterRunnerDefersProjection(t *testing.T) {
	repository := &processorTestRepository{loadResponses: []processorTestLoadResponse{{}, {err: ErrPersistence}}}
	runner := &processorTestRunner{err: errors.New("runner returned after a possible terminal freeze")}
	processor := newProcessorForTest(t, repository, runner)

	processor.process(context.Background(), processorTestClaim(2, false))

	snapshot := repository.snapshot()
	if runner.callCount() != 1 || snapshot.loadCalls != 2 || snapshot.deferProjectionCalls != 1 ||
		snapshot.retryExecutionCalls != 0 || snapshot.freezeFailureCalls != 0 || snapshot.completeCalls != 0 {
		t.Fatalf("Runner 后终态读取故障错误消耗 execution budget: runner=%d snapshot=%+v", runner.callCount(), snapshot)
	}
}

// TestProcessorUnknownOutcomeDefersRecovery 验证 Business unknown 不被误判为 dead 或普通技术重试。
func TestProcessorUnknownOutcomeDefersRecovery(t *testing.T) {
	repository := &processorTestRepository{loadResponses: []processorTestLoadResponse{{}, {}}}
	runner := &processorTestRunner{err: plancreationspec.ErrBusinessUnknownOutcome}
	processor := newProcessorForTest(t, repository, runner)

	processor.process(context.Background(), processorTestClaim(1, false))

	snapshot := repository.snapshot()
	if runner.callCount() != 1 || snapshot.loadCalls != 2 || snapshot.deferRecoveryCalls != 1 ||
		snapshot.retryExecutionCalls != 0 || snapshot.deferProjectionCalls != 0 ||
		snapshot.freezeFailureCalls != 0 || snapshot.completeCalls != 0 {
		t.Fatalf("unknown outcome 未保持 recovery_pending: runner=%d snapshot=%+v", runner.callCount(), snapshot)
	}
}

// TestProcessorRecoveryOnlyNeverBecomesDead 验证 prepared/business_unknown 已跨过副作用边界后，poison 或 max-attempt 技术错误都只能查询恢复。
func TestProcessorRecoveryOnlyNeverBecomesDead(t *testing.T) {
	for _, stage := range []ReceiptStage{ReceiptStageBusinessPrepared, ReceiptStageBusinessUnknown} {
		for _, poisoned := range []bool{false, true} {
			name := string(stage) + "/technical"
			if poisoned {
				name = string(stage) + "/poisoned"
			}
			t.Run(name, func(t *testing.T) {
				responses := []processorTestLoadResponse{{stage: stage}}
				wantRunner := 0
				if !poisoned {
					responses = append(responses, processorTestLoadResponse{stage: stage})
					wantRunner = 1
				}
				repository := &processorTestRepository{loadResponses: responses}
				runner := &processorTestRunner{err: errors.New("query path technical failure")}
				processor := newProcessorForTest(t, repository, runner)

				processor.process(context.Background(), processorTestClaim(2, poisoned))

				snapshot := repository.snapshot()
				if runner.callCount() != wantRunner || snapshot.deferRecoveryCalls != 1 ||
					snapshot.retryExecutionCalls != 0 || snapshot.freezeFailureCalls != 0 ||
					snapshot.completeCalls != 0 || snapshot.deferProjectionCalls != 0 {
					t.Fatalf("recovery-only 被错误收口为执行终态: runner=%d snapshot=%+v", runner.callCount(), snapshot)
				}
			})
		}
	}
}

// TestProcessorExecutionFailureBudget 验证 poison/普通执行失败在预算内重试，耗尽时冻结 failed 并以 dead 处置完成终态事件。
func TestProcessorExecutionFailureBudget(t *testing.T) {
	tests := []struct {
		name           string
		poisoned       bool
		attempts       int
		wantRunner     int
		wantRetry      int
		wantFreeze     int
		wantComplete   int
		wantResultCode string
	}{
		{name: "poison retry", poisoned: true, attempts: 1, wantRetry: 1},
		{name: "technical retry", attempts: 1, wantRunner: 1, wantRetry: 1},
		{
			name: "poison exhausted emits failed", poisoned: true, attempts: 2,
			wantFreeze: 1, wantComplete: 1, wantResultCode: plancreationspec.ResultCodeRuntimeInputInvalid,
		},
		{
			name: "technical exhausted emits failed", attempts: 2, wantRunner: 1,
			wantFreeze: 1, wantComplete: 1, wantResultCode: plancreationspec.ResultCodeRuntimeProcessingFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &processorTestRepository{loadResponses: []processorTestLoadResponse{{}, {}}}
			runner := &processorTestRunner{err: errors.New("deterministic technical failure")}
			processor := newProcessorForTest(t, repository, runner)

			processor.process(context.Background(), processorTestClaim(test.attempts, test.poisoned))

			snapshot := repository.snapshot()
			if runner.callCount() != test.wantRunner || snapshot.retryExecutionCalls != test.wantRetry ||
				snapshot.freezeFailureCalls != test.wantFreeze || snapshot.completeCalls != test.wantComplete ||
				snapshot.deferRecoveryCalls != 0 || snapshot.deferProjectionCalls != 0 {
				t.Fatalf("执行失败预算路径异常: runner=%d snapshot=%+v", runner.callCount(), snapshot)
			}
			if test.wantFreeze == 0 {
				return
			}
			if len(snapshot.frozenFailures) != 1 || len(snapshot.completed) != 1 {
				t.Fatalf("耗尽未同时冻结并投影失败: frozen=%+v completed=%+v", snapshot.frozenFailures, snapshot.completed)
			}
			result := snapshot.frozenFailures[0]
			if result.Status != "failed" || result.ResultCode != test.wantResultCode || result.Retryable ||
				result.Summary == "" || result.ReceiptRef.ToolCallID != processorTestToolCallID ||
				result.ReceiptRef.BusinessCommandID != processorTestBusinessCmdID ||
				snapshot.completed[0].Disposition != CompletionDead ||
				snapshot.completed[0].Result.ResultCode != test.wantResultCode {
				t.Fatalf("耗尽 failed 终态不完整: frozen=%+v completed=%+v", result, snapshot.completed[0])
			}
		})
	}
}

// TestProcessorFailureFreezeOrProjectionErrorsOnlyDeferProjection 验证已到执行上限后，冻结/投影故障都不能再进入 execution retry 或丢失终态恢复机会。
func TestProcessorFailureFreezeOrProjectionErrorsOnlyDeferProjection(t *testing.T) {
	for _, test := range []struct {
		name         string
		freezeErr    error
		completeErr  error
		wantComplete int
	}{
		{name: "freeze failure", freezeErr: ErrPersistence},
		{name: "complete failure", completeErr: ErrPersistence, wantComplete: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &processorTestRepository{
				loadResponses: []processorTestLoadResponse{{}, {}}, freezeErr: test.freezeErr, completeErr: test.completeErr,
			}
			processor := newProcessorForTest(t, repository, &processorTestRunner{err: errors.New("technical failure")})

			processor.process(context.Background(), processorTestClaim(2, false))

			snapshot := repository.snapshot()
			if snapshot.freezeFailureCalls != 1 || snapshot.completeCalls != test.wantComplete ||
				snapshot.deferProjectionCalls != 1 || snapshot.retryExecutionCalls != 0 || snapshot.deferRecoveryCalls != 0 {
				t.Fatalf("耗尽后的存储故障错误消耗 execution budget: %+v", snapshot)
			}
		})
	}
}

// TestProcessorFenceLossNeverCommits 验证 MarkRunning 或 Heartbeat 丢失 fence 后不读取/冻结/投影任何终态。
func TestProcessorFenceLossNeverCommits(t *testing.T) {
	t.Run("mark running fence lost", func(t *testing.T) {
		repository := &processorTestRepository{markErr: ErrFenceLost}
		runner := &processorTestRunner{}
		newProcessorForTest(t, repository, runner).process(context.Background(), processorTestClaim(1, false))
		snapshot := repository.snapshot()
		if runner.callCount() != 0 || snapshot.loadCalls != 0 || snapshot.terminalMutationCalls() != 0 {
			t.Fatalf("MarkRunning 丢失 fence 后仍执行: runner=%d snapshot=%+v", runner.callCount(), snapshot)
		}
	})

	t.Run("heartbeat fence lost", func(t *testing.T) {
		repository := &processorTestRepository{
			loadResponses: []processorTestLoadResponse{{}}, renewErr: ErrFenceLost,
		}
		runner := &processorTestRunner{waitForCancel: true}
		processor := newProcessorForTest(t, repository, runner)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		processor.process(ctx, processorTestClaim(1, false))

		snapshot := repository.snapshot()
		if runner.callCount() != 1 || !runner.cancellationObserved() || snapshot.renewCalls != 1 || snapshot.loadCalls != 1 ||
			snapshot.terminalMutationCalls() != 0 {
			t.Fatalf("Heartbeat 丢失 fence 后仍提交: runner=%d snapshot=%+v", runner.callCount(), snapshot)
		}
	})
}

// TestProcessorStopHonorsCallerDeadline 验证第三方 Runner 违反取消契约时，Stop 仍按调用方 deadline 返回而不无界等待。
func TestProcessorStopHonorsCallerDeadline(t *testing.T) {
	releaseRunner := make(chan struct{})
	runnerStarted := make(chan struct{})
	runnerDone := make(chan struct{})
	repository := &processorTestRepository{
		claimQueue:    []Claim{processorTestClaim(1, false)},
		loadResponses: []processorTestLoadResponse{{stage: ReceiptStagePending}},
	}
	runner := &processorTestRunner{
		ignoreCancelUntil: releaseRunner, started: runnerStarted, done: runnerDone,
	}
	processor := newProcessorForTest(t, repository, runner)
	if err := processor.Start(); err != nil {
		t.Fatalf("启动 Processor 失败: %v", err)
	}
	processor.Wake()
	select {
	case <-runnerStarted:
	case <-time.After(time.Second):
		t.Fatal("Runner 未开始，无法验证 Stop deadline")
	}

	stopCtx, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	startedAt := time.Now()
	err := processor.Stop(stopCtx)
	if !errors.Is(err, context.Canceled) || time.Since(startedAt) > 100*time.Millisecond {
		t.Fatalf("Stop 未遵守已取消 deadline: err=%v elapsed=%s", err, time.Since(startedAt))
	}
	close(releaseRunner)
	select {
	case <-runnerDone:
	case <-time.After(time.Second):
		t.Fatal("释放后 Runner 未退出")
	}
	workersDone := make(chan struct{})
	go func() {
		processor.workers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(time.Second):
		t.Fatal("释放后 Processor worker 未退出")
	}
}

type processorTestLoadResponse struct {
	stage    ReceiptStage
	terminal *Terminal
	err      error
}

type processorTestRepository struct {
	mu sync.Mutex

	markErr       error
	renewErr      error
	completeErr   error
	freezeErr     error
	loadResponses []processorTestLoadResponse
	claimQueue    []Claim
	renewCalled   chan struct{}
	renewOnce     sync.Once

	markCalls            int
	renewCalls           int
	loadCalls            int
	completeCalls        int
	freezeFailureCalls   int
	deferRecoveryCalls   int
	deferProjectionCalls int
	retryExecutionCalls  int
	completed            []Terminal
	completeClaims       []Claim
	frozenFailures       []plancreationspec.Result
}

func (r *processorTestRepository) Enqueue(context.Context, EnqueuePlan) (EnqueueResult, error) {
	return EnqueueResult{}, errors.New("unexpected Enqueue")
}

func (r *processorTestRepository) ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.claimQueue) == 0 {
		return nil, nil
	}
	claim := r.claimQueue[0]
	r.claimQueue = r.claimQueue[1:]
	return &claim, nil
}

func (r *processorTestRepository) MarkRunning(context.Context, Claim, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markCalls++
	return r.markErr
}

func (r *processorTestRepository) RenewLease(context.Context, Claim, time.Time, time.Duration) error {
	r.mu.Lock()
	r.renewCalls++
	err := r.renewErr
	called := r.renewCalled
	r.mu.Unlock()
	if called != nil {
		r.renewOnce.Do(func() { close(called) })
	}
	return err
}

func (r *processorTestRepository) LoadReceipt(context.Context, Claim) (ReceiptSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.loadCalls
	r.loadCalls++
	if index >= len(r.loadResponses) {
		return ReceiptSnapshot{Stage: ReceiptStagePending}, nil
	}
	response := r.loadResponses[index]
	if response.terminal == nil {
		stage := response.stage
		if stage == "" {
			stage = ReceiptStagePending
		}
		return ReceiptSnapshot{Stage: stage}, response.err
	}
	copy := *response.terminal
	stage := response.stage
	if stage == "" {
		stage = ReceiptStageFailed
	}
	return ReceiptSnapshot{Stage: stage, Terminal: &copy}, response.err
}

func (r *processorTestRepository) Complete(_ context.Context, claim Claim, terminal Terminal, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeCalls++
	r.completeClaims = append(r.completeClaims, claim)
	r.completed = append(r.completed, terminal)
	return r.completeErr
}

func (r *processorTestRepository) FreezeExecutionFailure(_ context.Context, _ Claim, result plancreationspec.Result) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.freezeFailureCalls++
	r.frozenFailures = append(r.frozenFailures, result)
	return r.freezeErr
}

func (r *processorTestRepository) DeferRecovery(context.Context, Claim, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deferRecoveryCalls++
	return nil
}

func (r *processorTestRepository) DeferProjection(context.Context, Claim, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deferProjectionCalls++
	return nil
}

func (r *processorTestRepository) RetryExecution(context.Context, Claim, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retryExecutionCalls++
	return nil
}

func (r *processorTestRepository) snapshot() processorTestRepositorySnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return processorTestRepositorySnapshot{
		markCalls: r.markCalls, renewCalls: r.renewCalls, loadCalls: r.loadCalls,
		completeCalls: r.completeCalls, freezeFailureCalls: r.freezeFailureCalls,
		deferRecoveryCalls: r.deferRecoveryCalls, deferProjectionCalls: r.deferProjectionCalls,
		retryExecutionCalls: r.retryExecutionCalls,
		completed:           append([]Terminal(nil), r.completed...), completeClaims: append([]Claim(nil), r.completeClaims...),
		frozenFailures: append([]plancreationspec.Result(nil), r.frozenFailures...),
	}
}

type processorTestRepositorySnapshot struct {
	markCalls            int
	renewCalls           int
	loadCalls            int
	completeCalls        int
	freezeFailureCalls   int
	deferRecoveryCalls   int
	deferProjectionCalls int
	retryExecutionCalls  int
	completed            []Terminal
	completeClaims       []Claim
	frozenFailures       []plancreationspec.Result
}

func (snapshot processorTestRepositorySnapshot) terminalMutationCalls() int {
	return snapshot.completeCalls + snapshot.freezeFailureCalls + snapshot.deferRecoveryCalls +
		snapshot.deferProjectionCalls + snapshot.retryExecutionCalls
}

type processorTestRunner struct {
	mu                sync.Mutex
	err               error
	wait              <-chan struct{}
	waitForCancel     bool
	ignoreCancelUntil <-chan struct{}
	started           chan struct{}
	done              chan struct{}
	startOnce         sync.Once
	doneOnce          sync.Once
	cancelled         bool
	calls             int
}

func (r *processorTestRunner) Run(ctx context.Context, _ Claim) error {
	r.mu.Lock()
	r.calls++
	wait := r.wait
	err := r.err
	waitForCancel := r.waitForCancel
	ignoreCancelUntil := r.ignoreCancelUntil
	started := r.started
	r.mu.Unlock()
	if started != nil {
		r.startOnce.Do(func() { close(started) })
	}
	if ignoreCancelUntil != nil {
		<-ignoreCancelUntil
		r.doneOnce.Do(func() {
			if r.done != nil {
				close(r.done)
			}
		})
		return err
	}
	if waitForCancel {
		<-ctx.Done()
		r.mu.Lock()
		r.cancelled = true
		r.mu.Unlock()
		return ctx.Err()
	}
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (r *processorTestRunner) cancellationObserved() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

func (r *processorTestRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type processorTestClock struct{ now time.Time }

func (clock processorTestClock) Now() time.Time { return clock.now }

func newProcessorForTest(t *testing.T, repository *processorTestRepository, runner *processorTestRunner) *Processor {
	t.Helper()
	processor, err := NewProcessor(repository, runner, processorTestClock{now: processorTestNow}, "processor-test", ProcessorConfig{
		Concurrency: 1, PollInterval: time.Second, LeaseDuration: 50 * time.Millisecond,
		HeartbeatInterval: time.Millisecond, RetryDelay: time.Second, RecoveryDelay: 2 * time.Second, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("创建 Processor 失败: %v", err)
	}
	return processor
}

func processorTestClaim(attempts int, poisoned bool) Claim {
	return Claim{
		Owner: "processor-test", SessionID: processorTestSessionID, InputID: processorTestInputID,
		ToolCallID: processorTestToolCallID, BusinessCommandID: processorTestBusinessCmdID,
		TerminalEventID: processorTestTerminalEventID, FenceToken: 11, Attempts: attempts, Poisoned: poisoned,
	}
}

func processorTestTerminal(disposition CompletionDisposition) Terminal {
	return Terminal{
		Disposition: disposition,
		Result: plancreationspec.Result{
			Status: "failed", ResultCode: "CREATION_SPEC_PREVIEW_INVALID",
			ReceiptRef: plancreationspec.ReceiptRef{
				ToolCallID: processorTestToolCallID, BusinessCommandID: processorTestBusinessCmdID,
			},
			Summary: "已冻结的确定失败", Retryable: false,
		},
	}
}

var _ Repository = (*processorTestRepository)(nil)
var _ Runner = (*processorTestRunner)(nil)
