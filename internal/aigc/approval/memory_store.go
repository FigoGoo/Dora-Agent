package approval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

type MemoryStore struct {
	mu    sync.RWMutex
	clock func() time.Time

	approvals           map[string]Approval
	approvalByCreateKey map[string]string
	decisions           map[string]ApprovalDecision
	decisionByKey       map[string]string
	continuations       map[string]sessionruntime.ApprovalContinuation
	outbox              map[string]OutboxEvent
	outboxByKey         map[string]string
	candidateBatches    map[string]CandidateApprovalBatch
	candidateBatchByKey map[string]string
}

func NewMemoryStore() *MemoryStore { return NewMemoryStoreWithClock(time.Now) }

func NewMemoryStoreWithClock(clock func() time.Time) *MemoryStore {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryStore{
		clock: clock, approvals: map[string]Approval{}, approvalByCreateKey: map[string]string{},
		decisions: map[string]ApprovalDecision{}, decisionByKey: map[string]string{},
		continuations: map[string]sessionruntime.ApprovalContinuation{},
		outbox:        map[string]OutboxEvent{}, outboxByKey: map[string]string{},
		candidateBatches: map[string]CandidateApprovalBatch{}, candidateBatchByKey: map[string]string{},
	}
}

func (s *MemoryStore) Create(_ context.Context, requested Approval) (CreateResult, error) {
	if s == nil {
		return CreateResult{}, fmt.Errorf("approval memory store is required")
	}
	normalized, err := normalizeApproval(requested, s.clock())
	if err != nil {
		return CreateResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.approvals[normalized.ID]; ok {
		if !sameApproval(existing, normalized) {
			return CreateResult{}, fmt.Errorf("%w: approval_id=%s", ErrIdempotencyConflict, normalized.ID)
		}
		return CreateResult{Approval: cloneApproval(existing)}, nil
	}
	if existingID, ok := s.approvalByCreateKey[normalized.IdempotencyKey]; ok {
		existing := s.approvals[existingID]
		if existing.ID != normalized.ID || !sameApproval(existing, normalized) {
			return CreateResult{}, fmt.Errorf("%w: idempotency_key=%s", ErrIdempotencyConflict, normalized.IdempotencyKey)
		}
		return CreateResult{Approval: cloneApproval(existing)}, nil
	}
	s.approvals[normalized.ID] = cloneApproval(normalized)
	s.approvalByCreateKey[normalized.IdempotencyKey] = normalized.ID
	return CreateResult{Approval: cloneApproval(normalized), Created: true}, nil
}

func (s *MemoryStore) Get(_ context.Context, approvalID string) (Approval, error) {
	if s == nil {
		return Approval{}, fmt.Errorf("approval memory store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return Approval{}, fmt.Errorf("%w: %s", ErrNotFound, approvalID)
	}
	return cloneApproval(value), nil
}

func (s *MemoryStore) GetDecision(_ context.Context, approvalID string, decisionVersion int) (ApprovalDecision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	decision, ok := s.decisions[continuationKey(approvalID, decisionVersion)]
	if !ok {
		return ApprovalDecision{}, fmt.Errorf("%w: decision %s/%d", ErrNotFound, approvalID, decisionVersion)
	}
	return cloneDecision(decision), nil
}

func (s *MemoryStore) CreateCandidateApprovalBatch(_ context.Context, requested CandidateApprovalBatch) (CandidateApprovalBatchCreateResult, error) {
	if s == nil {
		return CandidateApprovalBatchCreateResult{}, fmt.Errorf("approval memory store is required")
	}
	normalized, err := normalizeCandidateApprovalBatch(requested, s.clock())
	if err != nil {
		return CandidateApprovalBatchCreateResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.candidateBatches[normalized.ID]; ok {
		if !sameCandidateApprovalBatch(existing, normalized) {
			return CandidateApprovalBatchCreateResult{}, fmt.Errorf("%w: candidate approval batch id=%s", ErrIdempotencyConflict, normalized.ID)
		}
		return CandidateApprovalBatchCreateResult{Batch: cloneCandidateApprovalBatch(existing)}, nil
	}
	if existingID, ok := s.candidateBatchByKey[normalized.IdempotencyKey]; ok {
		existing := s.candidateBatches[existingID]
		if !sameCandidateApprovalBatch(existing, normalized) {
			return CandidateApprovalBatchCreateResult{}, fmt.Errorf("%w: candidate approval batch key=%s", ErrIdempotencyConflict, normalized.IdempotencyKey)
		}
		return CandidateApprovalBatchCreateResult{Batch: cloneCandidateApprovalBatch(existing)}, nil
	}
	s.candidateBatches[normalized.ID] = cloneCandidateApprovalBatch(normalized)
	s.candidateBatchByKey[normalized.IdempotencyKey] = normalized.ID
	return CandidateApprovalBatchCreateResult{Batch: cloneCandidateApprovalBatch(normalized), Created: true}, nil
}

func (s *MemoryStore) GetCandidateApprovalBatchByKey(_ context.Context, idempotencyKey string) (CandidateApprovalBatch, error) {
	if s == nil {
		return CandidateApprovalBatch{}, fmt.Errorf("approval memory store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := strings.TrimSpace(idempotencyKey)
	id, ok := s.candidateBatchByKey[key]
	if !ok {
		return CandidateApprovalBatch{}, fmt.Errorf("%w: candidate approval batch key=%s", ErrNotFound, key)
	}
	return cloneCandidateApprovalBatch(s.candidateBatches[id]), nil
}

func (s *MemoryStore) Decide(_ context.Context, command DecideCommand) (DecisionResult, error) {
	request, err := decisionRequestFromCommand(command)
	if err != nil {
		return DecisionResult{}, err
	}
	return s.decide(request)
}

func (s *MemoryStore) Close(_ context.Context, command CloseCommand) (DecisionResult, error) {
	request, err := decisionRequestFromClose(command)
	if err != nil {
		return DecisionResult{}, err
	}
	return s.decide(request)
}

func (s *MemoryStore) decide(request decisionRequest) (DecisionResult, error) {
	if s == nil {
		return DecisionResult{}, fmt.Errorf("approval memory store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if decisionID, ok := s.decisionByKey[request.IdempotencyKey]; ok {
		decision := s.decisions[decisionID]
		if !sameDecisionRequest(decision, request) {
			return DecisionResult{}, fmt.Errorf("%w: decision key=%s", ErrIdempotencyConflict, request.IdempotencyKey)
		}
		return s.decisionResultLocked(decision, false)
	}
	approval, ok := s.approvals[request.ApprovalID]
	if !ok {
		return DecisionResult{}, fmt.Errorf("%w: %s", ErrNotFound, request.ApprovalID)
	}
	updated, decision, continuation, outbox, err := prepareDecision(approval, request)
	if err != nil {
		return DecisionResult{}, err
	}
	decisionID := continuationKey(updated.ID, updated.DecisionVersion)
	if _, exists := s.decisions[decisionID]; exists {
		return DecisionResult{}, fmt.Errorf("%w: decision %s", ErrIdempotencyConflict, decisionID)
	}
	if err := s.appendOutboxLocked(outbox); err != nil {
		return DecisionResult{}, err
	}
	s.approvals[updated.ID] = cloneApproval(updated)
	s.decisions[decisionID] = cloneDecision(decision)
	s.decisionByKey[decision.IdempotencyKey] = decisionID
	s.continuations[decisionID] = continuation
	return DecisionResult{
		Approval: cloneApproval(updated), Decision: cloneDecision(decision),
		Continuation: continuation, Outbox: cloneOutbox(outbox), Created: true,
	}, nil
}

func (s *MemoryStore) BindInterruptMapping(_ context.Context, command MappingCommand) (Approval, error) {
	if s == nil {
		return Approval{}, fmt.Errorf("approval memory store is required")
	}
	command.ApprovalID = strings.TrimSpace(command.ApprovalID)
	command.CheckpointMappingID = strings.TrimSpace(command.CheckpointMappingID)
	if command.ApprovalID == "" || command.CheckpointMappingID == "" || command.ExpectedExecutionEpoch <= 0 || command.MappingEpoch <= 0 {
		return Approval{}, fmt.Errorf("approval id, mapping id, and positive epochs are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	approval, ok := s.approvals[command.ApprovalID]
	if !ok {
		return Approval{}, fmt.Errorf("%w: %s", ErrNotFound, command.ApprovalID)
	}
	if approval.ReviewMode != ReviewModeInterrupt {
		return Approval{}, fmt.Errorf("%w: durable approval has no interrupt mapping", ErrInvalidTransition)
	}
	if approval.Status != StatusPending || approval.ExecutionMode != ExecutionModeInterrupt {
		return Approval{}, fmt.Errorf("%w: mapping cannot be bound in %s/%s", ErrInvalidTransition, approval.Status, approval.ExecutionMode)
	}
	if approval.ExecutionEpoch != command.ExpectedExecutionEpoch {
		return Approval{}, fmt.Errorf("%w: execution epoch current=%d expected=%d", ErrFallbackFenced, approval.ExecutionEpoch, command.ExpectedExecutionEpoch)
	}
	if approval.CheckpointMappingID != "" {
		if approval.CheckpointMappingID == command.CheckpointMappingID && approval.MappingEpoch == command.MappingEpoch {
			return cloneApproval(approval), nil
		}
		return Approval{}, fmt.Errorf("%w: checkpoint mapping is already frozen", ErrFallbackFenced)
	}
	approval.CheckpointMappingID = command.CheckpointMappingID
	approval.MappingEpoch = command.MappingEpoch
	approval.UpdatedAt = s.clock().UTC()
	s.approvals[approval.ID] = cloneApproval(approval)
	return cloneApproval(approval), nil
}

func (s *MemoryStore) SwitchToDurableFallback(_ context.Context, command FallbackCommand) (FallbackResult, error) {
	if s == nil {
		return FallbackResult{}, fmt.Errorf("approval memory store is required")
	}
	command.ApprovalID = strings.TrimSpace(command.ApprovalID)
	if command.ApprovalID == "" || command.ExpectedExecutionEpoch <= 0 || command.ExpectedDecisionVersion < 0 {
		return FallbackResult{}, fmt.Errorf("approval id, positive execution epoch, and decision version are required")
	}
	if command.ExpectedExecutionMode == "" {
		command.ExpectedExecutionMode = ExecutionModeInterrupt
	}
	if command.Now.IsZero() {
		command.Now = s.clock().UTC()
	} else {
		command.Now = command.Now.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	approval, ok := s.approvals[command.ApprovalID]
	if !ok {
		return FallbackResult{}, fmt.Errorf("%w: %s", ErrNotFound, command.ApprovalID)
	}
	if approval.ReviewMode != ReviewModeInterrupt {
		return FallbackResult{}, fmt.Errorf("%w: durable review cannot become fallback", ErrReviewModeImmutable)
	}
	if approval.ExecutionMode == ExecutionModeDurableFallback && approval.ExecutionEpoch == command.ExpectedExecutionEpoch+1 && approval.DecisionVersion == command.ExpectedDecisionVersion {
		return s.fallbackResultLocked(approval, false)
	}
	if approval.ExecutionMode != command.ExpectedExecutionMode || approval.ExecutionEpoch != command.ExpectedExecutionEpoch || approval.DecisionVersion != command.ExpectedDecisionVersion {
		return FallbackResult{}, fmt.Errorf("%w: mode=%s epoch=%d decision_version=%d", ErrFallbackFenced, approval.ExecutionMode, approval.ExecutionEpoch, approval.DecisionVersion)
	}

	approval.ExecutionMode = ExecutionModeDurableFallback
	approval.ExecutionEpoch++
	approval.UpdatedAt = command.Now
	var continuation *sessionruntime.ApprovalContinuation
	continuationMapKey := ""
	if approval.DecisionVersion > 0 {
		key := continuationKey(approval.ID, approval.DecisionVersion)
		current, ok := s.continuations[key]
		if !ok {
			return FallbackResult{}, fmt.Errorf("%w: continuation %s", ErrNotFound, key)
		}
		if current.Status == sessionruntime.ContinuationStatusApplied {
			return FallbackResult{}, fmt.Errorf("%w: continuation is already applied", ErrFallbackFenced)
		}
		if current.Status == sessionruntime.ContinuationStatusClaimed && current.LeaseUntil != nil && current.LeaseUntil.After(command.Now) {
			return FallbackResult{}, ErrContinuationBusy
		}
		current.Executor = sessionruntime.ContinuationExecutorDeterministic
		current.ExecutionEpoch = approval.ExecutionEpoch
		current.Status = sessionruntime.ContinuationStatusRequested
		current.LeaseOwner, current.LeaseUntil = "", nil
		current.ErrorCode, current.ErrorMessage = "", ""
		current.UpdatedAt = command.Now
		continuationMapKey = key
		copy := current
		continuation = &copy
	}
	outbox, err := newFallbackOutbox(approval, continuation, command.Now)
	if err != nil {
		return FallbackResult{}, err
	}
	if err := s.appendOutboxLocked(outbox); err != nil {
		return FallbackResult{}, err
	}
	if continuation != nil {
		s.continuations[continuationMapKey] = *continuation
	}
	s.approvals[approval.ID] = cloneApproval(approval)
	return FallbackResult{Approval: cloneApproval(approval), Continuation: continuation, Outbox: cloneOutbox(outbox), Switched: true}, nil
}

func (s *MemoryStore) GetContinuation(_ context.Context, approvalID string, decisionVersion int) (sessionruntime.ApprovalContinuation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.continuations[continuationKey(approvalID, decisionVersion)]
	if !ok {
		return sessionruntime.ApprovalContinuation{}, fmt.Errorf("%w: continuation %s/%d", ErrNotFound, approvalID, decisionVersion)
	}
	return value, nil
}

func (s *MemoryStore) ListOutbox(_ context.Context, status string, limit int) ([]OutboxEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status = strings.TrimSpace(status)
	values := make([]OutboxEvent, 0)
	for _, event := range s.outbox {
		if status == "" || event.Status == status {
			values = append(values, cloneOutbox(event))
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	if limit > 0 {
		limit = normalizeListLimit(limit)
		if len(values) > limit {
			values = values[:limit]
		}
	}
	return values, nil
}

// MarkOutboxPublished is an idempotent relay ACK. Only pending events may make
// the transition; a duplicate ACK keeps the original PublishedAt timestamp.
func (s *MemoryStore) MarkOutboxPublished(_ context.Context, eventID string, at time.Time) error {
	if s == nil {
		return fmt.Errorf("approval memory store is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("outbox event id is required")
	}
	if at.IsZero() {
		at = s.clock().UTC()
	} else {
		at = at.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.outbox[eventID]
	if !ok {
		return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
	}
	if event.Status == OutboxStatusPublished {
		return nil
	}
	if event.Status != OutboxStatusPending {
		return fmt.Errorf("%w: outbox event %s cannot publish from %s", ErrInvalidTransition, eventID, event.Status)
	}
	event.Status = OutboxStatusPublished
	event.PublishedAt = timePointer(at)
	s.outbox[eventID] = cloneOutbox(event)
	return nil
}

func (s *MemoryStore) MarkOutboxFailed(_ context.Context, eventID string, at time.Time, maxAttempts int) error {
	if s == nil {
		return fmt.Errorf("approval memory store is required")
	}
	if at.IsZero() {
		at = s.clock().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.outbox[strings.TrimSpace(eventID)]
	if !ok {
		return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
	}
	if event.Status != OutboxStatusPending {
		return nil
	}
	event.Attempts++
	if maxAttempts > 0 && event.Attempts >= maxAttempts {
		event.Status = OutboxStatusDead
	} else {
		event.AvailableAt = at.Add(approvalOutboxBackoff(event.Attempts))
	}
	s.outbox[event.ID] = cloneOutbox(event)
	return nil
}

func approvalOutboxBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func (s *MemoryStore) appendOutboxLocked(event OutboxEvent) error {
	if existingID, ok := s.outboxByKey[event.IdempotencyKey]; ok {
		existing := s.outbox[existingID]
		if existing.ID != event.ID || existing.EventType != event.EventType || existing.AggregateID != event.AggregateID {
			return fmt.Errorf("%w: outbox key=%s", ErrIdempotencyConflict, event.IdempotencyKey)
		}
		return nil
	}
	s.outbox[event.ID] = cloneOutbox(event)
	s.outboxByKey[event.IdempotencyKey] = event.ID
	return nil
}

func (s *MemoryStore) decisionResultLocked(decision ApprovalDecision, created bool) (DecisionResult, error) {
	approval := s.approvals[decision.ApprovalID]
	continuation := s.continuations[continuationKey(decision.ApprovalID, decision.DecisionVersion)]
	outbox, ok := s.currentOutboxLocked(approval, continuation)
	if !ok {
		return DecisionResult{}, fmt.Errorf("%w: decision outbox", ErrNotFound)
	}
	return DecisionResult{Approval: cloneApproval(approval), Decision: cloneDecision(decision), Continuation: continuation, Outbox: cloneOutbox(outbox), Created: created}, nil
}

func (s *MemoryStore) fallbackResultLocked(approval Approval, switched bool) (FallbackResult, error) {
	var continuation *sessionruntime.ApprovalContinuation
	if approval.DecisionVersion > 0 {
		value, ok := s.continuations[continuationKey(approval.ID, approval.DecisionVersion)]
		if !ok {
			return FallbackResult{}, fmt.Errorf("%w: continuation", ErrNotFound)
		}
		continuation = &value
	}
	outbox, ok := s.currentFallbackOutboxLocked(approval, continuation)
	if !ok {
		return FallbackResult{}, fmt.Errorf("%w: fallback outbox", ErrNotFound)
	}
	return FallbackResult{Approval: cloneApproval(approval), Continuation: continuation, Outbox: cloneOutbox(outbox), Switched: switched}, nil
}

func (s *MemoryStore) currentOutboxLocked(approval Approval, continuation sessionruntime.ApprovalContinuation) (OutboxEvent, bool) {
	eventType := EventApprovalContinuationRequested
	if continuation.Executor == sessionruntime.ContinuationExecutorRunnerResume {
		eventType = EventSessionInputRequested
	}
	key := decisionOutboxKey(approval.ID, approval.DecisionVersion, continuation.ExecutionEpoch, eventType)
	id, ok := s.outboxByKey[key]
	if !ok {
		return OutboxEvent{}, false
	}
	return s.outbox[id], true
}

func (s *MemoryStore) currentFallbackOutboxLocked(approval Approval, continuation *sessionruntime.ApprovalContinuation) (OutboxEvent, bool) {
	eventType := EventApprovalFallbackEnabled
	if continuation != nil {
		eventType = EventApprovalContinuationRequested
	}
	key := decisionOutboxKey(approval.ID, approval.DecisionVersion, approval.ExecutionEpoch, eventType)
	id, ok := s.outboxByKey[key]
	if !ok {
		return OutboxEvent{}, false
	}
	return s.outbox[id], true
}

func sameDecisionRequest(decision ApprovalDecision, request decisionRequest) bool {
	if decision.ApprovalID != request.ApprovalID || decision.RequestedDecision != request.RequestedDecision || decision.ActorID != request.ActorID || decision.Reason != request.Reason {
		return false
	}
	if decision.ObservedBinding == nil || request.ObservedBinding == nil {
		return decision.ObservedBinding == nil && request.ObservedBinding == nil
	}
	return *decision.ObservedBinding == *request.ObservedBinding
}

func continuationKey(approvalID string, decisionVersion int) string {
	return fmt.Sprintf("%s\x00%d", strings.TrimSpace(approvalID), decisionVersion)
}

func cloneApproval(value Approval) Approval {
	value.ApproveCommand.Payload = append([]byte(nil), value.ApproveCommand.Payload...)
	value.RejectCommand.Payload = append([]byte(nil), value.RejectCommand.Payload...)
	if value.ExpiresAt != nil {
		value.ExpiresAt = timePointer(*value.ExpiresAt)
	}
	if value.DecidedAt != nil {
		value.DecidedAt = timePointer(*value.DecidedAt)
	}
	return value
}

func cloneDecision(value ApprovalDecision) ApprovalDecision {
	value.ObservedBinding = cloneBindingPointer(value.ObservedBinding)
	value.CommandPayload = append([]byte(nil), value.CommandPayload...)
	return value
}

func cloneOutbox(value OutboxEvent) OutboxEvent {
	value.Payload = append([]byte(nil), value.Payload...)
	if value.PublishedAt != nil {
		value.PublishedAt = timePointer(*value.PublishedAt)
	}
	return value
}

var _ Store = (*MemoryStore)(nil)
