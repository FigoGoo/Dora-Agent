package usermessageruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var processorTestNow = time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)

// TestProcessorReceiptFirstSkipsRunner 验证已有冻结 Output 时只 Complete，不进入 Runner/Model/Freeze。
func TestProcessorReceiptFirstSkipsRunner(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	direct := NewDirectResponse(claim)
	repository := &processorTestRepository{
		snapshot: OutputReceiptSnapshot{Stage: OutputReceiptCompleted, Output: &Output{DirectResponse: &direct}},
	}
	runner := &processorTestRunner{}
	newProcessorForRuntimeTest(t, repository, runner).process(context.Background(), claim)

	state := repository.state()
	if runner.calls != 0 || state.loadCalls != 1 || state.completeCalls != 1 || state.freezeCalls != 0 ||
		state.retryCalls != 0 || state.deferCalls != 0 {
		t.Fatalf("receipt-first 路径调用异常: runner=%d state=%+v", runner.calls, state)
	}
}

// TestProcessorFreezesReloadsThenCompletes 验证成功路径严格按 Run→Freeze first-write-wins→重读→Complete。
func TestProcessorFreezesReloadsThenCompletes(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	direct := NewDirectResponse(claim)
	repository := &processorTestRepository{snapshot: OutputReceiptSnapshot{Stage: OutputReceiptOpen}}
	runner := &processorTestRunner{output: Output{DirectResponse: &direct}}
	newProcessorForRuntimeTest(t, repository, runner).process(context.Background(), claim)

	state := repository.state()
	if runner.calls != 1 || state.loadCalls != 3 || state.freezeCalls != 1 || state.completeCalls != 1 ||
		state.retryCalls != 0 || state.deferCalls != 0 {
		t.Fatalf("成功冻结顺序异常: runner=%d state=%+v", runner.calls, state)
	}
	want := []string{"mark", "load", "load", "freeze", "load", "complete"}
	if len(state.order) != len(want) {
		t.Fatalf("调用顺序=%v want=%v", state.order, want)
	}
	for index := range want {
		if state.order[index] != want[index] {
			t.Fatalf("调用顺序=%v want=%v", state.order, want)
		}
	}
}

// TestProcessorProjectionFailureNeverRerunsModel 验证 Complete 失败后的下一次 Claim 只重放冻结 Output。
func TestProcessorProjectionFailureNeverRerunsModel(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	direct := NewDirectResponse(claim)
	repository := &processorTestRepository{
		snapshot: OutputReceiptSnapshot{Stage: OutputReceiptOpen}, completeErr: ErrPersistence,
	}
	runner := &processorTestRunner{output: Output{DirectResponse: &direct}}
	processor := newProcessorForRuntimeTest(t, repository, runner)
	processor.process(context.Background(), claim)
	repository.mu.Lock()
	repository.completeErr = nil
	repository.mu.Unlock()
	processor.process(context.Background(), claim)

	state := repository.state()
	if runner.calls != 1 || state.freezeCalls != 1 || state.completeCalls != 2 || state.deferCalls != 1 || state.retryCalls != 0 {
		t.Fatalf("投影恢复重复 Runner 或冻结: runner=%d state=%+v", runner.calls, state)
	}
}

// TestProcessorExecutionFailureRetriesThenFreezesFailure 验证未耗尽进入 retry_wait，耗尽后先冻结 Failure 再 Complete。
func TestProcessorExecutionFailureRetriesThenFreezesFailure(t *testing.T) {
	for _, test := range []struct {
		name         string
		attempts     int
		poisoned     bool
		wantRunner   int
		wantRetry    int
		wantFreeze   int
		wantComplete int
		wantCode     string
	}{
		{name: "technical retry", attempts: 1, wantRunner: 1, wantRetry: 1},
		{name: "poison retry", attempts: 1, poisoned: true, wantRetry: 1},
		{name: "technical exhausted", attempts: 2, wantRunner: 1, wantFreeze: 1, wantComplete: 1, wantCode: FailureCodeProcessingFailed},
		{name: "poison exhausted", attempts: 2, poisoned: true, wantFreeze: 1, wantComplete: 1, wantCode: FailureCodeInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := validRuntimeTestClaim(t)
			claim.Attempts, claim.Poisoned = test.attempts, test.poisoned
			repository := &processorTestRepository{snapshot: OutputReceiptSnapshot{Stage: OutputReceiptOpen}}
			runner := &processorTestRunner{err: errors.New("technical")}
			newProcessorForRuntimeTest(t, repository, runner).process(context.Background(), claim)
			state := repository.state()
			if runner.calls != test.wantRunner || state.retryCalls != test.wantRetry || state.freezeCalls != test.wantFreeze ||
				state.completeCalls != test.wantComplete || state.deferCalls != 0 {
				t.Fatalf("执行失败路径异常: runner=%d state=%+v", runner.calls, state)
			}
			if test.wantCode != "" {
				if state.snapshot.Output == nil || state.snapshot.Output.Failure == nil || state.snapshot.Output.Failure.ErrorCode != test.wantCode {
					t.Fatalf("冻结 Failure=%+v want code=%s", state.snapshot.Output, test.wantCode)
				}
			}
		})
	}
}

// TestProcessorHeartbeatFenceLossCancelsWithoutTerminalWrites 验证续租丢失 Fence 会取消 Runner 且后续零增量。
func TestProcessorHeartbeatFenceLossCancelsWithoutTerminalWrites(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	repository := &processorTestRepository{
		snapshot: OutputReceiptSnapshot{Stage: OutputReceiptOpen}, renewErr: ErrFenceLost,
	}
	runner := &processorTestRunner{waitForCancel: true}
	processor := newProcessorForRuntimeTest(t, repository, runner)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	processor.process(ctx, claim)

	state := repository.state()
	if runner.calls != 1 || !runner.cancelObserved || state.renewCalls != 1 || state.loadCalls != 1 ||
		state.freezeCalls != 0 || state.completeCalls != 0 || state.retryCalls != 0 || state.deferCalls != 0 {
		t.Fatalf("Fence 丢失后仍有终态写入: runner=%+v state=%+v", runner, state)
	}
}

type processorTestClock struct{ now time.Time }

func (clock processorTestClock) Now() time.Time { return clock.now }

type processorTestRunner struct {
	mu             sync.Mutex
	output         Output
	err            error
	calls          int
	waitForCancel  bool
	cancelObserved bool
}

func (runner *processorTestRunner) Run(ctx context.Context, _ Claim) (Output, error) {
	runner.mu.Lock()
	runner.calls++
	wait := runner.waitForCancel
	output, err := runner.output, runner.err
	runner.mu.Unlock()
	if wait {
		<-ctx.Done()
		runner.mu.Lock()
		runner.cancelObserved = true
		runner.mu.Unlock()
		return Output{}, ctx.Err()
	}
	return output, err
}

type processorTestRepository struct {
	mu            sync.Mutex
	snapshot      OutputReceiptSnapshot
	markErr       error
	loadErr       error
	freezeErr     error
	completeErr   error
	renewErr      error
	order         []string
	loadCalls     int
	freezeCalls   int
	completeCalls int
	retryCalls    int
	deferCalls    int
	renewCalls    int
}

type processorTestRepositoryState struct {
	snapshot                                                                  OutputReceiptSnapshot
	order                                                                     []string
	loadCalls, freezeCalls, completeCalls, retryCalls, deferCalls, renewCalls int
}

func (repository *processorTestRepository) ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error) {
	return nil, nil
}

func (repository *processorTestRepository) MarkRunning(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.order = append(repository.order, "mark")
	return repository.markErr
}

func (repository *processorTestRepository) RenewLease(context.Context, Claim, time.Time, time.Duration) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.renewCalls++
	return repository.renewErr
}

func (repository *processorTestRepository) LoadFrozenOutput(context.Context, Claim) (OutputReceiptSnapshot, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.loadCalls++
	repository.order = append(repository.order, "load")
	return cloneOutputReceiptSnapshot(repository.snapshot), repository.loadErr
}

func (repository *processorTestRepository) FreezeOutput(_ context.Context, _ Claim, output Output, _ time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.freezeCalls++
	repository.order = append(repository.order, "freeze")
	if repository.freezeErr != nil {
		return repository.freezeErr
	}
	if repository.snapshot.Stage == OutputReceiptOpen {
		copy := cloneOutput(output)
		stage := OutputReceiptFailed
		if copy.DirectResponse != nil {
			stage = OutputReceiptCompleted
		}
		repository.snapshot = OutputReceiptSnapshot{Stage: stage, Output: &copy}
	}
	return nil
}

func (repository *processorTestRepository) Complete(context.Context, Claim, Output, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completeCalls++
	repository.order = append(repository.order, "complete")
	return repository.completeErr
}

func (repository *processorTestRepository) RetryExecution(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.retryCalls++
	return nil
}

func (repository *processorTestRepository) DeferProjection(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.deferCalls++
	return nil
}

func (repository *processorTestRepository) state() processorTestRepositoryState {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return processorTestRepositoryState{
		snapshot: cloneOutputReceiptSnapshot(repository.snapshot), order: append([]string(nil), repository.order...),
		loadCalls: repository.loadCalls, freezeCalls: repository.freezeCalls, completeCalls: repository.completeCalls,
		retryCalls: repository.retryCalls, deferCalls: repository.deferCalls, renewCalls: repository.renewCalls,
	}
}

func cloneOutputReceiptSnapshot(snapshot OutputReceiptSnapshot) OutputReceiptSnapshot {
	if snapshot.Output != nil {
		copy := cloneOutput(*snapshot.Output)
		snapshot.Output = &copy
	}
	return snapshot
}

func cloneOutput(output Output) Output {
	if output.DirectResponse != nil {
		copy := *output.DirectResponse
		copy.AvailableActions = append([]string(nil), output.DirectResponse.AvailableActions...)
		output.DirectResponse = &copy
	}
	if output.Failure != nil {
		copy := *output.Failure
		output.Failure = &copy
	}
	return output
}

func newProcessorForRuntimeTest(t *testing.T, repository Repository, runner Runner) *Processor {
	t.Helper()
	processor, err := NewProcessor(repository, runner, processorTestClock{now: processorTestNow}, "processor-test", ProcessorConfig{
		Concurrency: 1, PollInterval: time.Hour, LeaseDuration: 100 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond, RetryDelay: time.Second,
		ProjectionDelay: time.Second, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("创建测试 Processor 失败: %v", err)
	}
	return processor
}

var _ Clock = processorTestClock{}
var _ Runner = (*processorTestRunner)(nil)
var _ Repository = (*processorTestRepository)(nil)
