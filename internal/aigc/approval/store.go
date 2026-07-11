package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

type CreateResult struct {
	Approval Approval `json:"approval"`
	Created  bool     `json:"created"`
}

type Store interface {
	Create(ctx context.Context, approval Approval) (CreateResult, error)
	Get(ctx context.Context, approvalID string) (Approval, error)
	GetDecision(ctx context.Context, approvalID string, decisionVersion int) (ApprovalDecision, error)
	CreateCandidateApprovalBatch(ctx context.Context, batch CandidateApprovalBatch) (CandidateApprovalBatchCreateResult, error)
	GetCandidateApprovalBatchByKey(ctx context.Context, idempotencyKey string) (CandidateApprovalBatch, error)
	Decide(ctx context.Context, command DecideCommand) (DecisionResult, error)
	Close(ctx context.Context, command CloseCommand) (DecisionResult, error)
	BindInterruptMapping(ctx context.Context, command MappingCommand) (Approval, error)
	SwitchToDurableFallback(ctx context.Context, command FallbackCommand) (FallbackResult, error)
	GetContinuation(ctx context.Context, approvalID string, decisionVersion int) (sessionruntime.ApprovalContinuation, error)
	ListOutbox(ctx context.Context, status string, limit int) ([]OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, eventID string, at time.Time) error
	MarkOutboxFailed(ctx context.Context, eventID string, at time.Time, maxAttempts int) error
}

type decisionRequest struct {
	ApprovalID              string
	ExpectedDecisionVersion int
	IdempotencyKey          string
	RequestedDecision       string
	ActorID                 string
	Reason                  string
	ObservedBinding         *VersionBinding
	TerminalStatus          Status
	Now                     time.Time
}

func decisionRequestFromCommand(command DecideCommand) (decisionRequest, error) {
	decision := strings.ToLower(strings.TrimSpace(command.Decision))
	if decision != DecisionApprove && decision != DecisionReject {
		return decisionRequest{}, fmt.Errorf("decision must be approved or rejected")
	}
	current := normalizeBinding(command.CurrentBinding)
	return normalizeDecisionRequest(decisionRequest{
		ApprovalID: command.ApprovalID, ExpectedDecisionVersion: command.ExpectedDecisionVersion,
		IdempotencyKey: command.IdempotencyKey, RequestedDecision: decision,
		ActorID: command.ActorID, Reason: command.Reason, ObservedBinding: &current, Now: command.Now,
	})
}

func decisionRequestFromClose(command CloseCommand) (decisionRequest, error) {
	if command.Status != StatusStale && command.Status != StatusExpired && command.Status != StatusCancelled {
		return decisionRequest{}, fmt.Errorf("close status must be stale, expired, or cancelled")
	}
	return normalizeDecisionRequest(decisionRequest{
		ApprovalID: command.ApprovalID, ExpectedDecisionVersion: command.ExpectedDecisionVersion,
		IdempotencyKey: command.IdempotencyKey, RequestedDecision: string(command.Status),
		ActorID: command.ActorID, Reason: command.Reason, TerminalStatus: command.Status, Now: command.Now,
	})
}

func normalizeDecisionRequest(request decisionRequest) (decisionRequest, error) {
	request.ApprovalID = strings.TrimSpace(request.ApprovalID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.RequestedDecision = strings.TrimSpace(request.RequestedDecision)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ApprovalID == "" || request.IdempotencyKey == "" {
		return decisionRequest{}, fmt.Errorf("approval id and decision idempotency key are required")
	}
	if request.ExpectedDecisionVersion < 0 {
		return decisionRequest{}, fmt.Errorf("expected decision version cannot be negative")
	}
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	} else {
		request.Now = request.Now.UTC()
	}
	return request, nil
}

func prepareDecision(approval Approval, request decisionRequest) (Approval, ApprovalDecision, sessionruntime.ApprovalContinuation, OutboxEvent, error) {
	if approval.Status != StatusPending {
		return Approval{}, ApprovalDecision{}, sessionruntime.ApprovalContinuation{}, OutboxEvent{}, ErrAlreadyDecided
	}
	if approval.DecisionVersion != request.ExpectedDecisionVersion {
		return Approval{}, ApprovalDecision{}, sessionruntime.ApprovalContinuation{}, OutboxEvent{}, fmt.Errorf("%w: current=%d expected=%d", ErrVersionConflict, approval.DecisionVersion, request.ExpectedDecisionVersion)
	}

	effective := request.TerminalStatus
	if effective == "" {
		switch {
		case approval.ExpiresAt != nil && !approval.ExpiresAt.After(request.Now):
			effective = StatusExpired
		case request.ObservedBinding == nil || !approval.Binding.Matches(*request.ObservedBinding):
			effective = StatusStale
		case request.RequestedDecision == DecisionApprove:
			effective = StatusApproved
		default:
			effective = StatusRejected
		}
	}

	decisionVersion := approval.DecisionVersion + 1
	decision := ApprovalDecision{
		ApprovalID: approval.ID, DecisionVersion: decisionVersion,
		IdempotencyKey: request.IdempotencyKey, RequestedDecision: request.RequestedDecision,
		EffectiveStatus: effective, ActorID: request.ActorID, Reason: request.Reason,
		ObservedBinding: cloneBindingPointer(request.ObservedBinding), CreatedAt: request.Now,
	}
	var selected FrozenCommand
	switch effective {
	case StatusApproved:
		selected = approval.ApproveCommand
	case StatusRejected:
		selected = approval.RejectCommand
	case StatusStale, StatusExpired, StatusCancelled:
		// Terminal no-op branch: a continuation is still emitted so an outer
		// interrupt or durable stage can close deterministically.
	default:
		return Approval{}, ApprovalDecision{}, sessionruntime.ApprovalContinuation{}, OutboxEvent{}, fmt.Errorf("%w: terminal status %q", ErrInvalidTransition, effective)
	}
	if selected.Kind != "" {
		decision.CommandKind = selected.Kind
		decision.CommandIdempotencyKey = selected.IdempotencyKey
		decision.CommandPayload = append(json.RawMessage(nil), selected.Payload...)
	}

	approval.Status = effective
	approval.DecisionVersion = decisionVersion
	approval.UpdatedAt = request.Now
	approval.DecidedAt = timePointer(request.Now)
	continuation, err := newContinuation(approval, request.Now)
	if err != nil {
		return Approval{}, ApprovalDecision{}, sessionruntime.ApprovalContinuation{}, OutboxEvent{}, err
	}
	outbox, err := newDecisionOutbox(approval, continuation, request.Now)
	if err != nil {
		return Approval{}, ApprovalDecision{}, sessionruntime.ApprovalContinuation{}, OutboxEvent{}, err
	}
	return approval, decision, continuation, outbox, nil
}

func newContinuation(approval Approval, now time.Time) (sessionruntime.ApprovalContinuation, error) {
	executor := sessionruntime.ContinuationExecutorDeterministic
	if approval.ExecutionMode == ExecutionModeInterrupt {
		if approval.CheckpointMappingID == "" || approval.MappingEpoch <= 0 {
			return sessionruntime.ApprovalContinuation{}, fmt.Errorf("interrupt approval requires a persisted checkpoint mapping before decision")
		}
		executor = sessionruntime.ContinuationExecutorRunnerResume
	}
	return sessionruntime.ApprovalContinuation{
		ApprovalID: approval.ID, DecisionVersion: approval.DecisionVersion, SessionID: approval.SessionID,
		Executor: executor, ExecutionEpoch: approval.ExecutionEpoch,
		Status: sessionruntime.ContinuationStatusRequested, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func newDecisionOutbox(approval Approval, continuation sessionruntime.ApprovalContinuation, now time.Time) (OutboxEvent, error) {
	eventType := EventApprovalContinuationRequested
	destination := DestinationApprovalContinuations
	var payload any = ApprovalContinuationRequested{
		ApprovalID: approval.ID, DecisionVersion: approval.DecisionVersion,
		Executor: continuation.Executor, ExecutionEpoch: continuation.ExecutionEpoch,
	}
	if continuation.Executor == sessionruntime.ContinuationExecutorRunnerResume {
		eventType = EventSessionInputRequested
		destination = DestinationSessionInputs
		resume := sessionruntime.NewResumeRequested(approval.ID, approval.DecisionVersion, decisionEventID(approval.ID, approval.DecisionVersion))
		resume.MappingID = approval.CheckpointMappingID
		resume.MappingEpoch = approval.MappingEpoch
		payload = resume
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("marshal approval decision outbox: %w", err)
	}
	key := decisionOutboxKey(approval.ID, approval.DecisionVersion, continuation.ExecutionEpoch, eventType)
	return OutboxEvent{
		ID: key, IdempotencyKey: key, EventType: eventType, Destination: destination,
		AggregateType: "approval", AggregateID: approval.ID, AggregateVersion: approval.DecisionVersion,
		SessionID: approval.SessionID, Payload: raw, Status: OutboxStatusPending,
		AvailableAt: now, CreatedAt: now,
	}, nil
}

func newFallbackOutbox(approval Approval, continuation *sessionruntime.ApprovalContinuation, now time.Time) (OutboxEvent, error) {
	eventType := EventApprovalFallbackEnabled
	destination := DestinationApprovalContinuations
	var payload any = map[string]any{
		"approval_id": approval.ID, "decision_version": approval.DecisionVersion,
		"execution_mode": approval.ExecutionMode, "execution_epoch": approval.ExecutionEpoch,
	}
	if continuation != nil {
		eventType = EventApprovalContinuationRequested
		payload = ApprovalContinuationRequested{
			ApprovalID: approval.ID, DecisionVersion: approval.DecisionVersion,
			Executor: continuation.Executor, ExecutionEpoch: continuation.ExecutionEpoch,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("marshal approval fallback outbox: %w", err)
	}
	key := decisionOutboxKey(approval.ID, approval.DecisionVersion, approval.ExecutionEpoch, eventType)
	return OutboxEvent{
		ID: key, IdempotencyKey: key, EventType: eventType, Destination: destination,
		AggregateType: "approval", AggregateID: approval.ID, AggregateVersion: approval.DecisionVersion,
		SessionID: approval.SessionID, Payload: raw, Status: OutboxStatusPending,
		AvailableAt: now, CreatedAt: now,
	}, nil
}

func decisionEventID(approvalID string, decisionVersion int) string {
	return fmt.Sprintf("approval:%s:decision:%d", approvalID, decisionVersion)
}

func decisionOutboxKey(approvalID string, decisionVersion int, executionEpoch int64, eventType string) string {
	return fmt.Sprintf("approval:%s:decision:%d:epoch:%d:%s", approvalID, decisionVersion, executionEpoch, eventType)
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

func cloneBindingPointer(value *VersionBinding) *VersionBinding {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeListLimit(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 100
	}
	return limit
}
