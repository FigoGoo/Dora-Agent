package sessionruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TurnOutcome string

const (
	TurnOutcomeCommit           TurnOutcome = "commit"
	TurnOutcomeWaitingInterrupt TurnOutcome = "waiting_interrupt"
	TurnOutcomeRetry            TurnOutcome = "retry"
	TurnOutcomeDead             TurnOutcome = "dead"
)

type TurnResult struct {
	Outcome            TurnOutcome
	OutputDigest       string
	RunnerCheckpointID string
	RetryAt            time.Time
	Failure            Failure
}

// Processor bridges the durable runtime to an Eino Runner (or any other turn
// engine) without importing an Agent implementation into this package.
type Processor interface {
	Process(ctx context.Context, input SessionInput, turn SessionTurnRun, fence Fence) (TurnResult, error)
}

type ProcessorFunc func(context.Context, SessionInput, SessionTurnRun, Fence) (TurnResult, error)

func (fn ProcessorFunc) Process(ctx context.Context, input SessionInput, turn SessionTurnRun, fence Fence) (TurnResult, error) {
	return fn(ctx, input, turn, fence)
}

type ManagerConfig struct {
	Store          Store
	Processor      Processor
	OwnerID        string
	LeaseTTL       time.Duration
	RenewInterval  time.Duration
	ClaimTTL       time.Duration
	PollInterval   time.Duration
	IdleTimeout    time.Duration
	DiscoveryLimit int
	MaxAttempts    int
	RetryBackoff   func(attempt int, err error) time.Duration
	TurnSpec       func(ctx context.Context, record SessionInputRecord, input SessionInput) (TurnSpec, error)
	OnError        func(sessionID string, err error)
}

type Manager struct {
	config ManagerConfig
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	running  map[string]struct{}
	wakes    map[string]chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopped  bool
}

func NewManager(config ManagerConfig) (*Manager, error) {
	if config.Store == nil || config.Processor == nil {
		return nil, fmt.Errorf("session runtime store and processor are required")
	}
	config.OwnerID = strings.TrimSpace(config.OwnerID)
	if config.OwnerID == "" {
		config.OwnerID = newOwnerID()
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = 30 * time.Second
	}
	if config.RenewInterval <= 0 {
		config.RenewInterval = config.LeaseTTL / 3
	}
	if config.RenewInterval >= config.LeaseTTL {
		return nil, fmt.Errorf("renew interval must be shorter than lease ttl")
	}
	if config.ClaimTTL <= 0 {
		config.ClaimTTL = config.LeaseTTL
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.DiscoveryLimit <= 0 || config.DiscoveryLimit > 1000 {
		config.DiscoveryLimit = 100
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 5
	}
	if config.RetryBackoff == nil {
		config.RetryBackoff = func(attempt int, _ error) time.Duration {
			shift := min(max(attempt-1, 0), 6)
			return time.Second * time.Duration(1<<shift)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{config: config, ctx: ctx, cancel: cancel, running: make(map[string]struct{}), wakes: make(map[string]chan struct{})}, nil
}

func (m *Manager) Enqueue(ctx context.Context, sessionID string, input SessionInput) (EnqueueResult, error) {
	if m == nil {
		return EnqueueResult{}, fmt.Errorf("session runtime manager is required")
	}
	result, err := m.config.Store.EnqueueInput(ctx, sessionID, input)
	if err == nil {
		m.Wake(sessionID)
	}
	return result, err
}

// Run continuously discovers durable inputs that have no in-process wakeup,
// including inputs left behind by a process restart.
func (m *Manager) Run(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("session runtime manager is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()
	for {
		if err := m.Discover(ctx); err != nil && !errors.Is(err, context.Canceled) {
			if m.config.OnError != nil {
				m.config.OnError("", err)
			}
		}
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), m.config.LeaseTTL)
			defer cancel()
			return errors.Join(ctx.Err(), m.Stop(stopCtx))
		case <-m.ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (m *Manager) Discover(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("session runtime manager is required")
	}
	sessions, err := m.config.Store.ListRunnableSessions(ctx, m.config.DiscoveryLimit)
	if err != nil {
		return err
	}
	for _, sessionID := range sessions {
		if _, err := m.EnsureSession(context.Background(), sessionID); err != nil && !errors.Is(err, ErrManagerStopped) {
			return err
		}
	}
	return nil
}

// Wake is only a latency hint. It deliberately carries no input payload.
func (m *Manager) Wake(sessionID string) {
	if m == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	wake := m.wakeChannelLocked(sessionID)
	m.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

// StartSession starts a managed background lane. RunSession is the blocking
// form and is useful when lifecycle is owned by a caller.
func (m *Manager) StartSession(ctx context.Context, sessionID string) error {
	_, err := m.EnsureSession(ctx, sessionID)
	return err
}

// EnsureSession atomically starts a local session lane only when one is not
// already running. The boolean reports whether this call started the lane.
func (m *Manager) EnsureSession(ctx context.Context, sessionID string) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("session runtime manager is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, fmt.Errorf("session id is required")
	}
	wake, reserved, err := m.reserveSession(sessionID)
	if err != nil || !reserved {
		return false, err
	}
	laneCtx := context.Background()
	if ctx != nil {
		laneCtx = context.WithoutCancel(ctx)
	}
	go func() {
		if err := m.runReservedSession(laneCtx, sessionID, wake); err != nil && !errors.Is(err, context.Canceled) && m.config.OnError != nil {
			m.config.OnError(sessionID, err)
		}
	}()
	return true, nil
}

func (m *Manager) RunSession(ctx context.Context, sessionID string) error {
	if m == nil {
		return fmt.Errorf("session runtime manager is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	wake, reserved, err := m.reserveSession(sessionID)
	if err != nil {
		return err
	}
	if !reserved {
		return fmt.Errorf("session %s runtime is already running", sessionID)
	}
	return m.runReservedSession(ctx, sessionID, wake)
}

func (m *Manager) runReservedSession(ctx context.Context, sessionID string, wake chan struct{}) error {
	defer func() {
		m.mu.Lock()
		delete(m.running, sessionID)
		m.mu.Unlock()
		m.wg.Done()
	}()

	runCtx, cancel := context.WithCancel(ctx)
	stopManagerWatch := context.AfterFunc(m.ctx, cancel)
	defer stopManagerWatch()
	defer cancel()

	lease, err := m.config.Store.AcquireLease(runCtx, sessionID, m.config.OwnerID, m.config.LeaseTTL)
	if err != nil {
		return err
	}
	fence := lease.Fence()
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		_ = m.config.Store.ReleaseLease(releaseCtx, fence)
	}()
	if _, err := m.config.Store.RecoverExpiredInputs(runCtx, fence); err != nil {
		return err
	}
	poll := time.NewTicker(m.config.PollInterval)
	renew := time.NewTicker(m.config.RenewInterval)
	idle := time.NewTimer(m.config.IdleTimeout)
	nextRenew := time.Now().Add(m.config.RenewInterval)
	defer poll.Stop()
	defer renew.Stop()
	defer idle.Stop()

	for {
		processed, err := m.drain(runCtx, fence)
		if err != nil {
			return err
		}
		if processed {
			resetTimer(idle, m.config.IdleTimeout)
			if !time.Now().Before(nextRenew) {
				if _, err := m.config.Store.RenewLease(runCtx, fence, m.config.LeaseTTL); err != nil {
					return err
				}
				nextRenew = time.Now().Add(m.config.RenewInterval)
			}
			continue
		}
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-idle.C:
			return nil
		case <-wake:
			resetTimer(idle, m.config.IdleTimeout)
		case <-poll.C:
		case <-renew.C:
			if _, err := m.config.Store.RenewLease(runCtx, fence, m.config.LeaseTTL); err != nil {
				return err
			}
			nextRenew = time.Now().Add(m.config.RenewInterval)
		}
	}
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.stopped = true
		m.cancel()
		m.mu.Unlock()
	})
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) reserveSession(sessionID string) (chan struct{}, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, false, ErrManagerStopped
	}
	wake := m.wakeChannelLocked(sessionID)
	if _, exists := m.running[sessionID]; exists {
		return wake, false, nil
	}
	m.running[sessionID] = struct{}{}
	m.wg.Add(1)
	return wake, true, nil
}

func (m *Manager) drain(ctx context.Context, fence Fence) (bool, error) {
	record, err := m.config.Store.ClaimNext(ctx, ClaimOptions{Fence: fence, ClaimTTL: m.config.ClaimTTL, MaxAttempts: m.config.MaxAttempts})
	if errors.Is(err, ErrNoInputAvailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	input, err := DecodeInput(record)
	if err != nil {
		_, deadErr := m.config.Store.DeadInput(ctx, fence, record.InputID, Failure{Code: "invalid_input_payload", Message: err.Error()})
		return true, deadErr
	}
	var spec TurnSpec
	if m.config.TurnSpec != nil {
		spec, err = m.config.TurnSpec(ctx, record, input)
		if err != nil {
			return true, m.retryClaimedInput(ctx, fence, record, err)
		}
	}
	turn, _, err := m.config.Store.GetOrCreateTurn(ctx, fence, record.InputID, spec)
	if err != nil {
		return true, err
	}
	turn, err = m.config.Store.BeginTurn(ctx, fence, turn.TurnID)
	if err != nil {
		return true, err
	}
	result, processErr, leaseErr := m.processWithHeartbeat(ctx, input, turn, fence)
	if leaseErr != nil {
		return true, leaseErr
	}
	if processErr != nil {
		if result.RunnerCheckpointID != "" {
			if _, checkpointErr := m.config.Store.SaveTurnCheckpoint(ctx, fence, turn.TurnID, result.RunnerCheckpointID); checkpointErr != nil {
				return true, checkpointErr
			}
		}
		preserveFrozenOutput := len(turn.OutputPayload) > 0
		if record.Attempts >= m.config.MaxAttempts && !preserveFrozenOutput {
			latest, loadErr := m.config.Store.GetTurn(ctx, turn.TurnID)
			if loadErr != nil {
				return true, loadErr
			}
			preserveFrozenOutput = len(latest.OutputPayload) > 0
		}
		if record.Attempts >= m.config.MaxAttempts && !preserveFrozenOutput {
			_, err = m.config.Store.DeadTurn(ctx, fence, turn.TurnID, Failure{Code: "processor_failed", Message: processErr.Error()})
			return true, err
		}
		// A frozen output is authoritative work that only needs projection or
		// completion repair. Never discard it because the presentation/event
		// dependency stayed unavailable longer than the ordinary attempt budget.
		availableAt := time.Now().Add(m.config.RetryBackoff(record.Attempts, processErr))
		_, err = m.config.Store.RetryTurnAt(ctx, fence, turn.TurnID, availableAt, Failure{Code: "processor_failed", Message: processErr.Error()})
		return true, err
	}
	if result.Outcome == "" {
		result.Outcome = TurnOutcomeCommit
	}
	switch result.Outcome {
	case TurnOutcomeCommit:
		_, err = m.config.Store.CommitTurn(ctx, fence, turn.TurnID, result.OutputDigest)
	case TurnOutcomeWaitingInterrupt:
		_, err = m.config.Store.WaitForInterrupt(ctx, fence, turn.TurnID, result.RunnerCheckpointID)
	case TurnOutcomeRetry:
		if result.RunnerCheckpointID != "" {
			if _, checkpointErr := m.config.Store.SaveTurnCheckpoint(ctx, fence, turn.TurnID, result.RunnerCheckpointID); checkpointErr != nil {
				return true, checkpointErr
			}
		}
		if result.RetryAt.IsZero() {
			result.RetryAt = time.Now().Add(m.config.RetryBackoff(record.Attempts, errors.New(result.Failure.Message)))
		}
		_, err = m.config.Store.RetryTurnAt(ctx, fence, turn.TurnID, result.RetryAt, result.Failure)
		if err == nil && result.RetryAt.After(time.Now()) {
			time.AfterFunc(time.Until(result.RetryAt), func() { m.Wake(fence.SessionID) })
		}
	case TurnOutcomeDead:
		_, err = m.config.Store.DeadTurn(ctx, fence, turn.TurnID, result.Failure)
	default:
		err = fmt.Errorf("unknown turn outcome %q", result.Outcome)
		_, _ = m.config.Store.DeadTurn(ctx, fence, turn.TurnID, Failure{Code: "invalid_turn_outcome", Message: err.Error()})
	}
	return true, err
}

func (m *Manager) processWithHeartbeat(ctx context.Context, input SessionInput, turn SessionTurnRun, fence Fence) (TurnResult, error, error) {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	leaseErrors := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(m.config.RenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-processCtx.Done():
				return
			case <-ticker.C:
				if _, err := m.config.Store.RenewLease(processCtx, fence, m.config.LeaseTTL); err != nil {
					select {
					case leaseErrors <- err:
					default:
					}
					cancel()
					return
				}
				if _, err := m.config.Store.MarkInputRunning(processCtx, fence, turn.InputID, m.config.ClaimTTL); err != nil {
					select {
					case leaseErrors <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	result, err := callProcessor(processCtx, m.config.Processor, input, turn, fence)
	cancel()
	<-done
	select {
	case leaseErr := <-leaseErrors:
		return TurnResult{}, err, leaseErr
	default:
		return result, err, nil
	}
}

func (m *Manager) retryClaimedInput(ctx context.Context, fence Fence, record SessionInputRecord, cause error) error {
	if record.Attempts >= m.config.MaxAttempts {
		_, err := m.config.Store.DeadInput(ctx, fence, record.InputID, Failure{Code: "prepare_turn_failed", Message: cause.Error()})
		return err
	}
	available := time.Now().Add(m.config.RetryBackoff(record.Attempts, cause))
	_, err := m.config.Store.RetryInput(ctx, fence, record.InputID, available, Failure{Code: "prepare_turn_failed", Message: cause.Error()})
	return err
}

func (m *Manager) wakeChannelLocked(sessionID string) chan struct{} {
	wake := m.wakes[sessionID]
	if wake == nil {
		wake = make(chan struct{}, 1)
		m.wakes[sessionID] = wake
	}
	return wake
}

func callProcessor(ctx context.Context, processor Processor, input SessionInput, turn SessionTurnRun, fence Fence) (result TurnResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("processor panic: %v", recovered)
		}
	}()
	return processor.Process(ctx, input, turn, fence)
}

func newOwnerID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "runtime_" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("runtime_%d", time.Now().UnixNano())
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
