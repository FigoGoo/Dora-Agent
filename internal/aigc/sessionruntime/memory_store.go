package sessionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
)

type MemoryStore struct {
	mu  sync.RWMutex
	now func() time.Time

	inputCounters  map[string]int64
	inputs         map[string]SessionInputRecord
	inputSources   map[string]string
	leases         map[string]SessionRuntimeLease
	turns          map[string]SessionTurnRun
	turnByInput    map[string]string
	continuations  map[string]ApprovalContinuation
	commands       map[string]ApprovalCommandLedger
	commandKeys    map[string]string
	terminalEvents map[string]events.SessionEvent
}

func NewMemoryStore() *MemoryStore { return NewMemoryStoreWithClock(time.Now) }

func NewMemoryStoreWithClock(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{
		now:            now,
		inputCounters:  make(map[string]int64),
		inputs:         make(map[string]SessionInputRecord),
		inputSources:   make(map[string]string),
		leases:         make(map[string]SessionRuntimeLease),
		turns:          make(map[string]SessionTurnRun),
		turnByInput:    make(map[string]string),
		continuations:  make(map[string]ApprovalContinuation),
		commands:       make(map[string]ApprovalCommandLedger),
		commandKeys:    make(map[string]string),
		terminalEvents: make(map[string]events.SessionEvent),
	}
}

func (s *MemoryStore) EnqueueInput(_ context.Context, sessionID string, input SessionInput) (EnqueueResult, error) {
	if s == nil {
		return EnqueueResult{}, fmt.Errorf("memory session runtime store is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return EnqueueResult{}, fmt.Errorf("session id is required")
	}
	identity, payload, err := encodeInput(input)
	if err != nil {
		return EnqueueResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requested := SessionInputRecord{
		InputID: identity.InputID, SessionID: sessionID, InputType: identity.Type,
		SourceID: identity.SourceID, EventID: identity.EventID, Payload: payload,
		Priority: identity.Priority, ContextMessageSeq: inputContextMessageSeq(input),
	}
	if existing, ok := s.inputs[identity.InputID]; ok {
		if !sameInput(existing, requested) {
			return EnqueueResult{}, fmt.Errorf("%w: input_id=%s", ErrIdempotencyConflict, identity.InputID)
		}
		return EnqueueResult{Input: cloneInput(existing)}, nil
	}
	sourceKey := inputSourceIdentity(sessionID, identity.Type, identity.SourceID)
	if existingID, ok := s.inputSources[sourceKey]; ok {
		existing := s.inputs[existingID]
		if !sameInput(existing, requested) {
			return EnqueueResult{}, fmt.Errorf("%w: source_id=%s", ErrIdempotencyConflict, identity.SourceID)
		}
		return EnqueueResult{Input: cloneInput(existing)}, nil
	}
	now := s.now().UTC()
	s.inputCounters[sessionID]++
	requested.EnqueueSeq = s.inputCounters[sessionID]
	requested.Status = InputStatusPending
	requested.AvailableAt = now
	requested.CreatedAt = now
	requested.UpdatedAt = now
	s.inputs[requested.InputID] = cloneInput(requested)
	s.inputSources[sourceKey] = requested.InputID
	return EnqueueResult{Input: cloneInput(requested), Enqueued: true}, nil
}

func (s *MemoryStore) GetInput(_ context.Context, inputID string) (SessionInputRecord, error) {
	if s == nil {
		return SessionInputRecord{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.inputs[strings.TrimSpace(inputID)]
	if !ok {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrInputNotFound, inputID)
	}
	return cloneInput(record), nil
}

func (s *MemoryStore) ListRunnableSessions(_ context.Context, limit int) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("memory session runtime store is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now().UTC()
	// A claimed input remains the causal head of its session until its input is
	// terminal. In particular, a retry_wait head must not be bypassed by a
	// newly enqueued higher-priority input while its backoff is still active.
	startedHeads := make(map[string]SessionInputRecord)
	for _, record := range s.inputs {
		if isTerminalInputStatus(record.Status) || record.Attempts <= 0 {
			continue
		}
		if head, ok := startedHeads[record.SessionID]; !ok || record.EnqueueSeq < head.EnqueueSeq {
			startedHeads[record.SessionID] = record
		}
	}
	firstSeq := make(map[string]int64)
	for _, record := range s.inputs {
		if head, ok := startedHeads[record.SessionID]; ok {
			if record.InputID != head.InputID {
				continue
			}
		} else if record.Attempts > 0 {
			continue
		}
		runnable := (record.Status == InputStatusPending || record.Status == InputStatusRetryWait) && !record.AvailableAt.After(now)
		stranded := (record.Status == InputStatusClaimed || record.Status == InputStatusRunning) && (record.LeaseUntil == nil || !record.LeaseUntil.After(now))
		if !runnable && !stranded {
			continue
		}
		if seq, ok := firstSeq[record.SessionID]; !ok || record.EnqueueSeq < seq {
			firstSeq[record.SessionID] = record.EnqueueSeq
		}
	}
	sessions := make([]string, 0, len(firstSeq))
	for sessionID := range firstSeq {
		sessions = append(sessions, sessionID)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if firstSeq[sessions[i]] == firstSeq[sessions[j]] {
			return sessions[i] < sessions[j]
		}
		return firstSeq[sessions[i]] < firstSeq[sessions[j]]
	})
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (s *MemoryStore) ClaimNext(_ context.Context, options ClaimOptions) (SessionInputRecord, error) {
	if s == nil {
		return SessionInputRecord{}, fmt.Errorf("memory session runtime store is required")
	}
	if options.ClaimTTL <= 0 {
		return SessionInputRecord{}, fmt.Errorf("claim ttl must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, err := s.validateFenceLocked(options.Fence, now)
	if err != nil {
		return SessionInputRecord{}, err
	}
	var record SessionInputRecord
	for {
		started, hasStarted := s.startedHeadLocked(options.Fence.SessionID)
		if hasStarted {
			// A started input owns the lane even while it is in retry backoff.
			// Claimed/running heads are recovered by RecoverExpiredInputs after a
			// lease takeover; ClaimNext must never bypass them here.
			if started.Status != InputStatusPending && started.Status != InputStatusRetryWait {
				return SessionInputRecord{}, ErrNoInputAvailable
			}
			if started.AvailableAt.After(now) {
				return SessionInputRecord{}, ErrNoInputAvailable
			}
			if options.MaxAttempts > 0 && started.Attempts >= options.MaxAttempts && !s.inputHasFrozenOutputLocked(started) {
				s.deadExhaustedInputLocked(started, now)
				continue
			}
			record = started
			break
		}

		candidates := make([]SessionInputRecord, 0)
		for _, candidate := range s.inputs {
			if candidate.SessionID != options.Fence.SessionID || candidate.Attempts != 0 || candidate.AvailableAt.After(now) {
				continue
			}
			if candidate.Status != InputStatusPending && candidate.Status != InputStatusRetryWait {
				continue
			}
			candidates = append(candidates, candidate)
		}
		if len(candidates) == 0 {
			return SessionInputRecord{}, ErrNoInputAvailable
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Priority != candidates[j].Priority {
				return candidates[i].Priority > candidates[j].Priority
			}
			return candidates[i].EnqueueSeq < candidates[j].EnqueueSeq
		})
		record = candidates[0]
		break
	}
	until := cappedLeaseUntil(now, options.ClaimTTL, lease.LeaseUntil)
	record.Status = InputStatusClaimed
	record.ClaimOwner = options.Fence.OwnerID
	record.ClaimFence = options.Fence.FenceToken
	record.LeaseUntil = &until
	record.Attempts++
	record.UpdatedAt = now
	record.ErrorCode = ""
	record.ErrorMessage = ""
	s.inputs[record.InputID] = cloneInput(record)
	return cloneInput(record), nil
}

func (s *MemoryStore) inputHasFrozenOutputLocked(input SessionInputRecord) bool {
	turnID := strings.TrimSpace(input.TurnID)
	if turnID == "" {
		turnID = s.turnByInput[input.InputID]
	}
	turn, ok := s.turns[turnID]
	return ok && len(turn.OutputPayload) > 0
}

func (s *MemoryStore) startedHeadLocked(sessionID string) (SessionInputRecord, bool) {
	var head SessionInputRecord
	found := false
	for _, record := range s.inputs {
		if record.SessionID != sessionID || record.Attempts <= 0 || isTerminalInputStatus(record.Status) {
			continue
		}
		if !found || record.EnqueueSeq < head.EnqueueSeq {
			head, found = record, true
		}
	}
	return head, found
}

func (s *MemoryStore) deadExhaustedInputLocked(record SessionInputRecord, now time.Time) {
	s.recordTerminalFailureLocked(record, record.TurnID)
	record.Status = InputStatusDead
	record.ClaimOwner, record.ClaimFence, record.LeaseUntil = "", 0, nil
	record.ErrorCode = "max_attempts_exceeded"
	record.ErrorMessage = "session input exceeded maximum attempts"
	record.UpdatedAt = now
	s.inputs[record.InputID] = cloneInput(record)
	if strings.TrimSpace(record.TurnID) == "" {
		return
	}
	turn, ok := s.turns[record.TurnID]
	if !ok || isTerminalTurnStatus(turn.Status) {
		return
	}
	turn.Status = TurnStatusDead
	turn.ErrorCode, turn.ErrorMessage = record.ErrorCode, record.ErrorMessage
	turn.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
}

func (s *MemoryStore) MarkInputRunning(_ context.Context, fence Fence, inputID string, ttl time.Duration) (SessionInputRecord, error) {
	return s.transitionInput(fence, inputID, ttl, []InputStatus{InputStatusClaimed, InputStatusRunning}, InputStatusRunning, Failure{})
}

func (s *MemoryStore) RetryInput(_ context.Context, fence Fence, inputID string, availableAt time.Time, failure Failure) (SessionInputRecord, error) {
	if s == nil {
		return SessionInputRecord{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionInputRecord{}, err
	}
	record, err := s.claimedInputLocked(fence, inputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionInputRecord{}, err
	}
	if availableAt.IsZero() || availableAt.Before(now) {
		availableAt = now
	}
	record.Status = InputStatusRetryWait
	record.AvailableAt = availableAt.UTC()
	record.ClaimOwner = ""
	record.ClaimFence = 0
	record.LeaseUntil = nil
	record.ErrorCode = strings.TrimSpace(failure.Code)
	record.ErrorMessage = strings.TrimSpace(failure.Message)
	record.UpdatedAt = now
	s.inputs[record.InputID] = cloneInput(record)
	return cloneInput(record), nil
}

func (s *MemoryStore) ResolveInput(_ context.Context, fence Fence, inputID string) (SessionInputRecord, error) {
	return s.finishInput(fence, inputID, InputStatusResolved, Failure{})
}

func (s *MemoryStore) DeadInput(_ context.Context, fence Fence, inputID string, failure Failure) (SessionInputRecord, error) {
	return s.finishInput(fence, inputID, InputStatusDead, failure)
}

func (s *MemoryStore) RecoverExpiredInputs(_ context.Context, fence Fence) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return 0, err
	}
	var count int64
	for id, record := range s.inputs {
		if record.SessionID != fence.SessionID || (record.Status != InputStatusClaimed && record.Status != InputStatusRunning) {
			continue
		}
		if record.ClaimOwner == fence.OwnerID && record.ClaimFence == fence.FenceToken && record.LeaseUntil != nil && record.LeaseUntil.After(now) {
			continue
		}
		record.Status = InputStatusRetryWait
		record.AvailableAt = now
		record.ClaimOwner = ""
		record.ClaimFence = 0
		record.LeaseUntil = nil
		record.ErrorCode = "claim_recovered"
		record.ErrorMessage = "previous runtime claim expired or was fenced"
		record.UpdatedAt = now
		s.inputs[id] = cloneInput(record)
		if turnID := s.turnByInput[id]; turnID != "" {
			turn := s.turns[turnID]
			if turn.Status != TurnStatusCommitted && turn.Status != TurnStatusDead && turn.Status != TurnStatusWaitingInterrupt {
				turn.Status = TurnStatusRetryWait
				turn.ErrorCode = "claim_recovered"
				turn.ErrorMessage = "previous runtime claim expired or was fenced"
				turn.UpdatedAt = now
				s.turns[turnID] = cloneTurn(turn)
			}
		}
		count++
	}
	return count, nil
}

func (s *MemoryStore) AcquireLease(_ context.Context, sessionID, ownerID string, ttl time.Duration) (SessionRuntimeLease, error) {
	if s == nil {
		return SessionRuntimeLease{}, fmt.Errorf("memory session runtime store is required")
	}
	sessionID, ownerID = strings.TrimSpace(sessionID), strings.TrimSpace(ownerID)
	if sessionID == "" || ownerID == "" || ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("session id, owner id, and positive ttl are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, exists := s.leases[sessionID]
	if exists && lease.OwnerID != ownerID && lease.LeaseUntil.After(now) {
		return SessionRuntimeLease{}, fmt.Errorf("%w: session_id=%s owner_id=%s", ErrLeaseHeld, sessionID, lease.OwnerID)
	}
	if !exists {
		lease.FenceToken = 1
	} else if lease.OwnerID != ownerID {
		lease.FenceToken++
	}
	lease.SessionID = sessionID
	lease.OwnerID = ownerID
	lease.LeaseUntil = now.Add(ttl)
	lease.UpdatedAt = now
	s.leases[sessionID] = lease
	return lease, nil
}

func (s *MemoryStore) RenewLease(_ context.Context, fence Fence, ttl time.Duration) (SessionRuntimeLease, error) {
	if s == nil {
		return SessionRuntimeLease{}, fmt.Errorf("memory session runtime store is required")
	}
	if ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("lease ttl must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, err := s.validateFenceLocked(fence, now)
	if err != nil {
		return SessionRuntimeLease{}, err
	}
	lease.LeaseUntil = now.Add(ttl)
	lease.UpdatedAt = now
	s.leases[fence.SessionID] = lease
	return lease, nil
}

func (s *MemoryStore) HandoffLease(_ context.Context, fence Fence, newOwnerID string, ttl time.Duration) (SessionRuntimeLease, error) {
	if s == nil {
		return SessionRuntimeLease{}, fmt.Errorf("memory session runtime store is required")
	}
	newOwnerID = strings.TrimSpace(newOwnerID)
	if newOwnerID == "" || ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("new owner id and positive ttl are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, err := s.validateFenceLocked(fence, now)
	if err != nil {
		return SessionRuntimeLease{}, err
	}
	if newOwnerID != lease.OwnerID {
		lease.FenceToken++
	}
	lease.OwnerID = newOwnerID
	lease.LeaseUntil = now.Add(ttl)
	lease.UpdatedAt = now
	s.leases[fence.SessionID] = lease
	return lease, nil
}

func (s *MemoryStore) ReleaseLease(_ context.Context, fence Fence) error {
	if s == nil {
		return fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, err := s.validateFenceLocked(fence, now)
	if err != nil {
		return err
	}
	lease.LeaseUntil, lease.UpdatedAt = now, now
	s.leases[fence.SessionID] = lease
	return nil
}

func (s *MemoryStore) ValidateFence(_ context.Context, fence Fence) error {
	if s == nil {
		return fmt.Errorf("memory session runtime store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := s.validateFenceLocked(fence, s.now().UTC())
	return err
}

func (s *MemoryStore) GetOrCreateTurn(_ context.Context, fence Fence, inputID string, spec TurnSpec) (SessionTurnRun, bool, error) {
	if s == nil {
		return SessionTurnRun{}, false, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, false, err
	}
	input, err := s.claimedInputLocked(fence, inputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionTurnRun{}, false, err
	}
	if turnID := s.turnByInput[input.InputID]; turnID != "" {
		return cloneTurn(s.turns[turnID]), false, nil
	}
	turnID := strings.TrimSpace(spec.TurnID)
	if turnID == "" {
		turnID = StableTurnID(input.SessionID, input.InputID)
	}
	if existing, ok := s.turns[turnID]; ok {
		return SessionTurnRun{}, false, fmt.Errorf("%w: turn_id=%s belongs to input_id=%s", ErrIdempotencyConflict, turnID, existing.InputID)
	}
	runnerRunID := strings.TrimSpace(spec.RunnerRunID)
	if runnerRunID == "" {
		runnerRunID = StableRunnerRunID(input.SessionID, input.InputID)
	}
	turn := SessionTurnRun{
		TurnID: turnID, InputID: input.InputID, SessionID: input.SessionID,
		RunnerRunID: runnerRunID, ParentTurnID: strings.TrimSpace(spec.ParentTurnID),
		ClaimFence: fence.FenceToken, Kind: input.InputType, Status: TurnStatusPrepared,
		RunnerCheckpointID: strings.TrimSpace(spec.RunnerCheckpointID), Attempt: input.Attempts,
		ContextMessageSeq: input.ContextMessageSeq,
		ContextSeqFrozen:  input.InputType == InputTypeUserMessage && input.ContextMessageSeq > 0,
		CreatedAt:         now, UpdatedAt: now,
	}
	input.TurnID = turnID
	input.UpdatedAt = now
	s.inputs[input.InputID] = cloneInput(input)
	s.turns[turnID] = cloneTurn(turn)
	s.turnByInput[input.InputID] = turnID
	return cloneTurn(turn), true, nil
}

func (s *MemoryStore) GetTurn(_ context.Context, turnID string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	return cloneTurn(turn), nil
}

func (s *MemoryStore) BeginTurn(_ context.Context, fence Fence, turnID string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, input, err := s.turnAndClaimedInputLocked(fence, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status != TurnStatusPrepared && turn.Status != TurnStatusRetryWait && turn.Status != TurnStatusRunning {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot run from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
	}
	turn.Status = TurnStatusRunning
	turn.ClaimFence = fence.FenceToken
	turn.Attempt = input.Attempts
	turn.ErrorCode, turn.ErrorMessage = "", ""
	turn.UpdatedAt = now
	input.Status = InputStatusRunning
	input.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	s.inputs[input.InputID] = cloneInput(input)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) FreezeTurnContextMessageSeq(_ context.Context, fence Fence, turnID string, throughSeq int64) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	if throughSeq < 0 {
		return SessionTurnRun{}, fmt.Errorf("context message sequence cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, _, err := s.turnAndClaimedInputLocked(fence, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.ContextSeqFrozen {
		return cloneTurn(turn), nil
	}
	if turn.Status != TurnStatusRunning && turn.Status != TurnStatusRetryWait {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot freeze context from %s", ErrInvalidTransition, turnID, turn.Status)
	}
	turn.ContextMessageSeq = throughSeq
	turn.ContextSeqFrozen = true
	turn.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) FreezeTurnContextFromTerminalUserInputs(_ context.Context, fence Fence, turnID string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, _, err := s.turnAndClaimedInputLocked(fence, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.ContextSeqFrozen {
		return cloneTurn(turn), nil
	}
	if turn.Status != TurnStatusRunning && turn.Status != TurnStatusRetryWait {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot freeze context from %s", ErrInvalidTransition, turnID, turn.Status)
	}
	var throughSeq int64
	for _, input := range s.inputs {
		if input.SessionID != fence.SessionID || input.InputType != InputTypeUserMessage || !isTerminalInputStatus(input.Status) {
			continue
		}
		if input.ContextMessageSeq > throughSeq {
			throughSeq = input.ContextMessageSeq
		}
	}
	turn.ContextMessageSeq = throughSeq
	turn.ContextSeqFrozen = true
	turn.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) SaveTurnCheckpoint(_ context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return SessionTurnRun{}, fmt.Errorf("runner checkpoint id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, _, err := s.turnAndClaimedInputLocked(fence, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status != TurnStatusRunning && turn.Status != TurnStatusRetryWait {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot save checkpoint from %s", ErrInvalidTransition, turnID, turn.Status)
	}
	turn.RunnerCheckpointID = checkpointID
	turn.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) SaveTurnOutput(_ context.Context, fence Fence, turnID string, payload json.RawMessage, digest string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return SessionTurnRun{}, fmt.Errorf("turn output payload must be non-empty valid JSON")
	}
	digest = strings.TrimSpace(digest)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if turn.SessionID != fence.SessionID {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrFenceRejected, turnID)
	}
	if turn.Status != TurnStatusRunning && turn.Status != TurnStatusRetryWait {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot save output from %s", ErrInvalidTransition, turnID, turn.Status)
	}
	if _, err := s.claimedInputLocked(fence, turn.InputID, InputStatusClaimed, InputStatusRunning); err != nil {
		return SessionTurnRun{}, err
	}
	if len(turn.OutputPayload) != 0 {
		if turn.OutputDigest != digest || !jsonEqual(turn.OutputPayload, payload) {
			return SessionTurnRun{}, fmt.Errorf("%w: turn %s output receipt changed", ErrIdempotencyConflict, turnID)
		}
		return cloneTurn(turn), nil
	}
	turn.OutputPayload = append(json.RawMessage(nil), payload...)
	turn.OutputDigest = digest
	turn.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) WaitForInterrupt(_ context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return SessionTurnRun{}, fmt.Errorf("runner checkpoint id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if turn.Status == TurnStatusWaitingInterrupt && turn.RunnerCheckpointID == checkpointID {
		return cloneTurn(turn), nil
	}
	if turn.Status == TurnStatusWaitingInterrupt {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s checkpoint changed", ErrIdempotencyConflict, turnID)
	}
	input, err := s.claimedInputLocked(fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status != TurnStatusRunning {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot wait from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
	}
	turn.Status = TurnStatusWaitingInterrupt
	turn.RunnerCheckpointID = checkpointID
	turn.UpdatedAt = now
	input.Status = InputStatusResolved
	input.ClaimOwner, input.ClaimFence, input.LeaseUntil = "", 0, nil
	input.ResolvedAt = &now
	input.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	s.inputs[input.InputID] = cloneInput(input)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) RetryTurn(ctx context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error) {
	return s.RetryTurnAt(ctx, fence, turnID, time.Time{}, failure)
}

func (s *MemoryStore) RetryTurnAt(_ context.Context, fence Fence, turnID string, availableAt time.Time, failure Failure) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, input, err := s.turnAndClaimedInputLocked(fence, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status != TurnStatusPrepared && turn.Status != TurnStatusRunning && turn.Status != TurnStatusRetryWait {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot retry from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
	}
	turn.Status = TurnStatusRetryWait
	turn.ErrorCode, turn.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
	turn.UpdatedAt = now
	input.Status = InputStatusRetryWait
	if availableAt.IsZero() || availableAt.Before(now) {
		availableAt = now
	}
	input.AvailableAt = availableAt.UTC()
	input.ClaimOwner, input.ClaimFence, input.LeaseUntil = "", 0, nil
	input.ErrorCode, input.ErrorMessage = turn.ErrorCode, turn.ErrorMessage
	input.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	s.inputs[input.InputID] = cloneInput(input)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) CommitTurn(_ context.Context, fence Fence, turnID, outputDigest string) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if turn.Status == TurnStatusCommitted {
		if turn.OutputDigest != strings.TrimSpace(outputDigest) {
			return SessionTurnRun{}, fmt.Errorf("%w: turn %s output digest changed", ErrIdempotencyConflict, turnID)
		}
		return cloneTurn(turn), nil
	}
	if len(turn.OutputPayload) != 0 && turn.OutputDigest != strings.TrimSpace(outputDigest) {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s output digest changed", ErrIdempotencyConflict, turnID)
	}
	input, err := s.claimedInputLocked(fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status != TurnStatusRunning && turn.Status != TurnStatusCommitting {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot commit from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
	}
	turn.Status = TurnStatusCommitted
	turn.OutputDigest = strings.TrimSpace(outputDigest)
	turn.CommittedAt = &now
	turn.UpdatedAt = now
	input.Status = InputStatusResolved
	input.ClaimOwner, input.ClaimFence, input.LeaseUntil = "", 0, nil
	input.ResolvedAt = &now
	input.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	s.inputs[input.InputID] = cloneInput(input)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) DeadTurn(_ context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error) {
	if s == nil {
		return SessionTurnRun{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionTurnRun{}, err
	}
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if turn.Status == TurnStatusDead {
		if input, exists := s.inputs[turn.InputID]; exists {
			s.recordTerminalFailureLocked(input, turn.TurnID)
		}
		return cloneTurn(turn), nil
	}
	input, err := s.claimedInputLocked(fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.Status == TurnStatusCommitted || turn.Status == TurnStatusWaitingInterrupt {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s cannot die from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
	}
	s.recordTerminalFailureLocked(input, turn.TurnID)
	turn.Status = TurnStatusDead
	turn.ErrorCode, turn.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
	turn.UpdatedAt = now
	input.Status = InputStatusDead
	input.ClaimOwner, input.ClaimFence, input.LeaseUntil = "", 0, nil
	input.ErrorCode, input.ErrorMessage = turn.ErrorCode, turn.ErrorMessage
	input.UpdatedAt = now
	s.turns[turn.TurnID] = cloneTurn(turn)
	s.inputs[input.InputID] = cloneInput(input)
	return cloneTurn(turn), nil
}

func (s *MemoryStore) RequestContinuation(_ context.Context, continuation ApprovalContinuation) (ApprovalContinuation, bool, error) {
	if s == nil {
		return ApprovalContinuation{}, false, fmt.Errorf("memory session runtime store is required")
	}
	continuation, err := normalizeContinuation(continuation)
	if err != nil {
		return ApprovalContinuation{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := continuationIdentity(continuation.ApprovalID, continuation.DecisionVersion)
	if existing, ok := s.continuations[key]; ok {
		if existing.SessionID != continuation.SessionID || existing.Executor != continuation.Executor || existing.ExecutionEpoch != continuation.ExecutionEpoch {
			return ApprovalContinuation{}, false, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrIdempotencyConflict, continuation.ApprovalID, continuation.DecisionVersion)
		}
		return existing, false, nil
	}
	now := s.now().UTC()
	continuation.Status = ContinuationStatusRequested
	continuation.CreatedAt, continuation.UpdatedAt = now, now
	s.continuations[key] = continuation
	return continuation, true, nil
}

func (s *MemoryStore) GetContinuation(_ context.Context, approvalID string, decisionVersion int) (ApprovalContinuation, error) {
	if s == nil {
		return ApprovalContinuation{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.continuations[continuationIdentity(approvalID, decisionVersion)]
	if !ok {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, approvalID, decisionVersion)
	}
	return record, nil
}

func (s *MemoryStore) ClaimContinuation(_ context.Context, claim ContinuationClaim, ttl time.Duration) (ApprovalContinuation, error) {
	if s == nil {
		return ApprovalContinuation{}, fmt.Errorf("memory session runtime store is required")
	}
	if err := validateContinuationClaim(claim); err != nil {
		return ApprovalContinuation{}, err
	}
	if ttl <= 0 {
		return ApprovalContinuation{}, fmt.Errorf("continuation lease ttl must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := continuationIdentity(claim.ApprovalID, claim.DecisionVersion)
	record, ok := s.continuations[key]
	if !ok {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, claim.ApprovalID, claim.DecisionVersion)
	}
	now := s.now().UTC()
	if record.Executor != claim.Executor || record.ExecutionEpoch != claim.ExecutionEpoch {
		return ApprovalContinuation{}, fmt.Errorf("%w: continuation executor or epoch changed", ErrFenceRejected)
	}
	if record.Status == ContinuationStatusApplied {
		return record, nil
	}
	if record.Status == ContinuationStatusClaimed && record.LeaseUntil != nil && record.LeaseUntil.After(now) {
		return ApprovalContinuation{}, fmt.Errorf("%w: lease_owner=%s", ErrContinuationClaimed, record.LeaseOwner)
	}
	if record.Status != ContinuationStatusRequested && record.Status != ContinuationStatusClaimed && record.Status != ContinuationStatusFailed {
		return ApprovalContinuation{}, fmt.Errorf("%w: continuation cannot claim from %s", ErrInvalidTransition, record.Status)
	}
	until := now.Add(ttl)
	record.Status = ContinuationStatusClaimed
	record.LeaseOwner = claim.LeaseOwner
	record.LeaseUntil = &until
	record.ErrorCode = ""
	record.ErrorMessage = ""
	record.UpdatedAt = now
	s.continuations[key] = record
	return record, nil
}

func (s *MemoryStore) ApplyContinuation(_ context.Context, claim ContinuationClaim, commands []ApprovalCommandLedger) (ApprovalContinuation, error) {
	if s == nil {
		return ApprovalContinuation{}, fmt.Errorf("memory session runtime store is required")
	}
	if err := validateContinuationClaim(claim); err != nil {
		return ApprovalContinuation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := continuationIdentity(claim.ApprovalID, claim.DecisionVersion)
	record, ok := s.continuations[key]
	if !ok {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, claim.ApprovalID, claim.DecisionVersion)
	}
	if record.Status == ContinuationStatusApplied {
		return record, nil
	}
	if err := s.validateContinuationLeaseLocked(record, claim); err != nil {
		return ApprovalContinuation{}, err
	}
	normalized := make([]ApprovalCommandLedger, 0, len(commands))
	for _, command := range commands {
		item, err := normalizeCommand(command, claim)
		if err != nil {
			return ApprovalContinuation{}, err
		}
		ledgerKey := commandIdentity(item.ApprovalID, item.DecisionVersion, item.CommandKind)
		if existing, exists := s.commands[ledgerKey]; exists && !sameCommand(existing, item) {
			return ApprovalContinuation{}, fmt.Errorf("%w: command_kind=%s", ErrIdempotencyConflict, item.CommandKind)
		}
		if otherKey, exists := s.commandKeys[item.IdempotencyKey]; exists && otherKey != ledgerKey {
			return ApprovalContinuation{}, fmt.Errorf("%w: command idempotency_key=%s", ErrIdempotencyConflict, item.IdempotencyKey)
		}
		normalized = append(normalized, item)
	}
	now := s.now().UTC()
	for _, command := range normalized {
		if command.CreatedAt.IsZero() {
			command.CreatedAt = now
		}
		ledgerKey := commandIdentity(command.ApprovalID, command.DecisionVersion, command.CommandKind)
		s.commands[ledgerKey] = cloneCommand(command)
		s.commandKeys[command.IdempotencyKey] = ledgerKey
	}
	record.Status = ContinuationStatusApplied
	record.LeaseUntil = nil
	record.UpdatedAt = now
	record.AppliedAt = &now
	s.continuations[key] = record
	return record, nil
}

func (s *MemoryStore) FailContinuation(_ context.Context, claim ContinuationClaim, failure Failure) (ApprovalContinuation, error) {
	if s == nil {
		return ApprovalContinuation{}, fmt.Errorf("memory session runtime store is required")
	}
	if err := validateContinuationClaim(claim); err != nil {
		return ApprovalContinuation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := continuationIdentity(claim.ApprovalID, claim.DecisionVersion)
	record, ok := s.continuations[key]
	if !ok {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, claim.ApprovalID, claim.DecisionVersion)
	}
	if err := s.validateContinuationLeaseLocked(record, claim); err != nil {
		return ApprovalContinuation{}, err
	}
	now := s.now().UTC()
	record.Status = ContinuationStatusFailed
	record.LeaseUntil = nil
	record.ErrorCode = strings.TrimSpace(failure.Code)
	record.ErrorMessage = strings.TrimSpace(failure.Message)
	record.UpdatedAt = now
	s.continuations[key] = record
	return record, nil
}

func (s *MemoryStore) FallbackContinuation(_ context.Context, approvalID string, decisionVersion int, expectedEpoch int64) (ApprovalContinuation, error) {
	if s == nil {
		return ApprovalContinuation{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := continuationIdentity(approvalID, decisionVersion)
	record, ok := s.continuations[key]
	if !ok {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, approvalID, decisionVersion)
	}
	now := s.now().UTC()
	if record.Status == ContinuationStatusApplied {
		return record, nil
	}
	if record.Executor != ContinuationExecutorRunnerResume || record.ExecutionEpoch != expectedEpoch {
		return ApprovalContinuation{}, fmt.Errorf("%w: continuation executor or epoch changed", ErrFenceRejected)
	}
	if record.Status == ContinuationStatusClaimed && record.LeaseUntil != nil && record.LeaseUntil.After(now) {
		return ApprovalContinuation{}, fmt.Errorf("%w: lease_owner=%s", ErrContinuationClaimed, record.LeaseOwner)
	}
	record.Executor = ContinuationExecutorDeterministic
	record.ExecutionEpoch++
	record.Status = ContinuationStatusRequested
	record.LeaseOwner = ""
	record.LeaseUntil = nil
	record.ErrorCode, record.ErrorMessage = "", ""
	record.UpdatedAt = now
	s.continuations[key] = record
	return record, nil
}

func (s *MemoryStore) GetCommand(_ context.Context, approvalID string, decisionVersion int, commandKind string) (ApprovalCommandLedger, error) {
	if s == nil {
		return ApprovalCommandLedger{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.commands[commandIdentity(approvalID, decisionVersion, commandKind)]
	if !ok {
		return ApprovalCommandLedger{}, fmt.Errorf("%w: approval command not found", ErrContinuationNotFound)
	}
	return cloneCommand(record), nil
}

func (s *MemoryStore) transitionInput(fence Fence, inputID string, ttl time.Duration, from []InputStatus, to InputStatus, failure Failure) (SessionInputRecord, error) {
	if s == nil {
		return SessionInputRecord{}, fmt.Errorf("memory session runtime store is required")
	}
	if ttl <= 0 {
		return SessionInputRecord{}, fmt.Errorf("input lease ttl must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	lease, err := s.validateFenceLocked(fence, now)
	if err != nil {
		return SessionInputRecord{}, err
	}
	record, err := s.claimedInputLocked(fence, inputID, from...)
	if err != nil {
		return SessionInputRecord{}, err
	}
	until := cappedLeaseUntil(now, ttl, lease.LeaseUntil)
	record.Status = to
	record.LeaseUntil = &until
	record.ErrorCode = strings.TrimSpace(failure.Code)
	record.ErrorMessage = strings.TrimSpace(failure.Message)
	record.UpdatedAt = now
	s.inputs[record.InputID] = cloneInput(record)
	return cloneInput(record), nil
}

func (s *MemoryStore) finishInput(fence Fence, inputID string, status InputStatus, failure Failure) (SessionInputRecord, error) {
	if s == nil {
		return SessionInputRecord{}, fmt.Errorf("memory session runtime store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if _, err := s.validateFenceLocked(fence, now); err != nil {
		return SessionInputRecord{}, err
	}
	record, err := s.claimedInputLocked(fence, inputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionInputRecord{}, err
	}
	record.Status = status
	record.ClaimOwner, record.ClaimFence, record.LeaseUntil = "", 0, nil
	record.ErrorCode = strings.TrimSpace(failure.Code)
	record.ErrorMessage = strings.TrimSpace(failure.Message)
	if status == InputStatusDead {
		s.recordTerminalFailureLocked(record, record.TurnID)
	}
	if status == InputStatusResolved {
		record.ResolvedAt = &now
	}
	record.UpdatedAt = now
	s.inputs[record.InputID] = cloneInput(record)
	return cloneInput(record), nil
}

func (s *MemoryStore) recordTerminalFailureLocked(input SessionInputRecord, turnID string) {
	event := terminalFailureEvent(input, turnID)
	if existing, ok := s.terminalEvents[event.SourceKey]; ok {
		// The event is deterministic; retain the first receipt just like the
		// production append-once event log.
		_ = existing
		return
	}
	s.terminalEvents[event.SourceKey] = event
}

func (s *MemoryStore) validateFenceLocked(fence Fence, now time.Time) (SessionRuntimeLease, error) {
	fence.SessionID, fence.OwnerID = strings.TrimSpace(fence.SessionID), strings.TrimSpace(fence.OwnerID)
	lease, ok := s.leases[fence.SessionID]
	if !ok || fence.SessionID == "" || fence.OwnerID == "" || fence.FenceToken <= 0 || lease.OwnerID != fence.OwnerID || lease.FenceToken != fence.FenceToken || !lease.LeaseUntil.After(now) {
		return SessionRuntimeLease{}, fmt.Errorf("%w: session_id=%s owner_id=%s fence_token=%d", ErrFenceRejected, fence.SessionID, fence.OwnerID, fence.FenceToken)
	}
	return lease, nil
}

func (s *MemoryStore) claimedInputLocked(fence Fence, inputID string, statuses ...InputStatus) (SessionInputRecord, error) {
	record, ok := s.inputs[strings.TrimSpace(inputID)]
	if !ok {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrInputNotFound, inputID)
	}
	if record.SessionID != fence.SessionID || record.ClaimOwner != fence.OwnerID || record.ClaimFence != fence.FenceToken {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrFenceRejected, inputID)
	}
	if record.LeaseUntil == nil || !record.LeaseUntil.After(s.now().UTC()) {
		return SessionInputRecord{}, fmt.Errorf("%w: input claim expired for input_id=%s", ErrFenceRejected, inputID)
	}
	allowed := false
	for _, status := range statuses {
		allowed = allowed || record.Status == status
	}
	if !allowed {
		return SessionInputRecord{}, fmt.Errorf("%w: input %s is %s", ErrInvalidTransition, inputID, record.Status)
	}
	return record, nil
}

func (s *MemoryStore) turnAndClaimedInputLocked(fence Fence, turnID string) (SessionTurnRun, SessionInputRecord, error) {
	turn, ok := s.turns[strings.TrimSpace(turnID)]
	if !ok {
		return SessionTurnRun{}, SessionInputRecord{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if turn.SessionID != fence.SessionID {
		return SessionTurnRun{}, SessionInputRecord{}, fmt.Errorf("%w: turn_id=%s", ErrFenceRejected, turnID)
	}
	input, err := s.claimedInputLocked(fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
	if err != nil {
		return SessionTurnRun{}, SessionInputRecord{}, err
	}
	return cloneTurn(turn), input, nil
}

func (s *MemoryStore) validateContinuationLeaseLocked(record ApprovalContinuation, claim ContinuationClaim) error {
	now := s.now().UTC()
	if record.Executor != claim.Executor || record.ExecutionEpoch != claim.ExecutionEpoch || record.Status != ContinuationStatusClaimed || record.LeaseOwner != claim.LeaseOwner || record.LeaseUntil == nil || !record.LeaseUntil.After(now) {
		return fmt.Errorf("%w: approval continuation claim is stale", ErrFenceRejected)
	}
	return nil
}

func sameInput(existing, requested SessionInputRecord) bool {
	return existing.InputID == requested.InputID && existing.SessionID == requested.SessionID &&
		existing.InputType == requested.InputType && existing.SourceID == requested.SourceID &&
		existing.EventID == requested.EventID && existing.Priority == requested.Priority &&
		existing.ContextMessageSeq == requested.ContextMessageSeq &&
		jsonEqual(existing.Payload, requested.Payload)
}

func inputSourceIdentity(sessionID string, inputType InputType, sourceID string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", sessionID, inputType, sourceID)
}

func continuationIdentity(approvalID string, decisionVersion int) string {
	return fmt.Sprintf("%s\x00%d", strings.TrimSpace(approvalID), decisionVersion)
}

func commandIdentity(approvalID string, decisionVersion int, commandKind string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", strings.TrimSpace(approvalID), decisionVersion, strings.TrimSpace(commandKind))
}

func cappedLeaseUntil(now time.Time, ttl time.Duration, runtimeUntil time.Time) time.Time {
	until := now.Add(ttl)
	if runtimeUntil.Before(until) {
		return runtimeUntil
	}
	return until
}

func cloneInput(record SessionInputRecord) SessionInputRecord {
	record.Payload = append(record.Payload[:0:0], record.Payload...)
	if record.LeaseUntil != nil {
		value := *record.LeaseUntil
		record.LeaseUntil = &value
	}
	if record.ResolvedAt != nil {
		value := *record.ResolvedAt
		record.ResolvedAt = &value
	}
	return record
}

func cloneTurn(record SessionTurnRun) SessionTurnRun {
	record.OutputPayload = append(json.RawMessage(nil), record.OutputPayload...)
	if record.CommittedAt != nil {
		value := *record.CommittedAt
		record.CommittedAt = &value
	}
	return record
}

func cloneCommand(record ApprovalCommandLedger) ApprovalCommandLedger {
	record.CommandPayload = append(record.CommandPayload[:0:0], record.CommandPayload...)
	record.ResultPayload = append(record.ResultPayload[:0:0], record.ResultPayload...)
	return record
}

func normalizeContinuation(record ApprovalContinuation) (ApprovalContinuation, error) {
	record.ApprovalID = strings.TrimSpace(record.ApprovalID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	if record.ApprovalID == "" || record.SessionID == "" || record.DecisionVersion <= 0 {
		return ApprovalContinuation{}, fmt.Errorf("approval id, session id, and positive decision version are required")
	}
	if record.Executor != ContinuationExecutorRunnerResume && record.Executor != ContinuationExecutorDeterministic {
		return ApprovalContinuation{}, fmt.Errorf("invalid continuation executor %q", record.Executor)
	}
	if record.ExecutionEpoch <= 0 {
		return ApprovalContinuation{}, fmt.Errorf("execution epoch must be positive")
	}
	return record, nil
}

func validateContinuationClaim(claim ContinuationClaim) error {
	claim.ApprovalID = strings.TrimSpace(claim.ApprovalID)
	claim.LeaseOwner = strings.TrimSpace(claim.LeaseOwner)
	if claim.ApprovalID == "" || claim.DecisionVersion <= 0 || claim.ExecutionEpoch <= 0 || claim.LeaseOwner == "" {
		return fmt.Errorf("approval id, decision version, execution epoch, and lease owner are required")
	}
	if claim.Executor != ContinuationExecutorRunnerResume && claim.Executor != ContinuationExecutorDeterministic {
		return fmt.Errorf("invalid continuation executor %q", claim.Executor)
	}
	return nil
}

func normalizeCommand(command ApprovalCommandLedger, claim ContinuationClaim) (ApprovalCommandLedger, error) {
	if strings.TrimSpace(command.ApprovalID) == "" {
		command.ApprovalID = claim.ApprovalID
	}
	if command.DecisionVersion == 0 {
		command.DecisionVersion = claim.DecisionVersion
	}
	if command.ExecutionEpoch == 0 {
		command.ExecutionEpoch = claim.ExecutionEpoch
	}
	command.ApprovalID = strings.TrimSpace(command.ApprovalID)
	command.CommandKind = strings.TrimSpace(command.CommandKind)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.ApprovalID != claim.ApprovalID || command.DecisionVersion != claim.DecisionVersion || command.ExecutionEpoch != claim.ExecutionEpoch {
		return ApprovalCommandLedger{}, fmt.Errorf("%w: command continuation identity differs", ErrFenceRejected)
	}
	if command.CommandKind == "" || command.IdempotencyKey == "" {
		return ApprovalCommandLedger{}, fmt.Errorf("command kind and idempotency key are required")
	}
	if len(command.CommandPayload) == 0 {
		command.CommandPayload = json.RawMessage(`{}`)
	}
	if len(command.ResultPayload) == 0 {
		command.ResultPayload = json.RawMessage(`{}`)
	}
	if !json.Valid(command.CommandPayload) || !json.Valid(command.ResultPayload) {
		return ApprovalCommandLedger{}, fmt.Errorf("command and result payloads must be valid JSON")
	}
	command.CommandPayload = append(json.RawMessage(nil), command.CommandPayload...)
	command.ResultPayload = append(json.RawMessage(nil), command.ResultPayload...)
	return command, nil
}

func sameCommand(left, right ApprovalCommandLedger) bool {
	return left.ApprovalID == right.ApprovalID && left.DecisionVersion == right.DecisionVersion &&
		left.CommandKind == right.CommandKind && left.ExecutionEpoch == right.ExecutionEpoch &&
		left.IdempotencyKey == right.IdempotencyKey && jsonEqual(left.CommandPayload, right.CommandPayload) &&
		jsonEqual(left.ResultPayload, right.ResultPayload)
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
