// Package approvalruntime executes the frozen deterministic commands selected
// by the approval domain. It is the only bridge from an ApprovalDecision to
// spec/storyboard/artifact mutation or Runner resume.
package approvalruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type SpecRevisionStore interface {
	GetRevision(context.Context, string, int) (spec.FinalVideoSpec, error)
	GetLatestReviewingBySession(context.Context, string) (spec.FinalVideoSpec, error)
	DecideRevision(context.Context, string, int, bool) (spec.FinalVideoSpec, error)
}

type ConfirmedSpecSource interface {
	GetConfirmedBySession(context.Context, string) (spec.FinalVideoSpec, error)
}

type RuntimeInputEnqueuer interface {
	Enqueue(context.Context, string, sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error)
	Wake(string)
}

type GenerationJobSource interface {
	ListBySession(context.Context, string) ([]generation.GenerationJob, error)
}

type Config struct {
	Approvals          approval.Store
	Continuations      sessionruntime.Store
	Inputs             RuntimeInputEnqueuer
	Specs              SpecRevisionStore
	Artifacts          artifact.Store
	Storyboards        storyboard.AggregateRepository
	StoryboardCommands *storyboard.CommandService
	GenerationJobs     GenerationJobSource
	OwnerID            string
	LeaseTTL           time.Duration
	Now                func() time.Time
}

type Service struct {
	cfg           Config
	claimSequence atomic.Uint64
}

func New(config Config) (*Service, error) {
	if config.Approvals == nil || config.Continuations == nil {
		return nil, fmt.Errorf("approval and continuation stores are required")
	}
	if config.OwnerID == "" {
		config.OwnerID = "approval-continuation"
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{cfg: config}, nil
}

type DecideRequest struct {
	ApprovalID              string
	ExpectedDecisionVersion int
	IdempotencyKey          string
	Decision                string
	ActorID                 string
	Reason                  string
}

type DecideResult struct {
	Decision approval.DecisionResult `json:"decision"`
	Applied  bool                    `json:"applied"`
}

// GetContinuation returns the continuation receipt from the same store used
// by Apply. Callers use it to honor an active claim lease instead of treating
// "another executor currently owns the continuation" as successful
// completion.
func (s *Service) GetContinuation(ctx context.Context, approvalID string, decisionVersion int) (sessionruntime.ApprovalContinuation, error) {
	if s == nil || s.cfg.Continuations == nil {
		return sessionruntime.ApprovalContinuation{}, fmt.Errorf("approval continuation store is required")
	}
	return s.cfg.Continuations.GetContinuation(ctx, strings.TrimSpace(approvalID), decisionVersion)
}

// Decide resolves current semantic versions server-side. If an interrupt-mode
// approval has no usable mapping, it is fenced to durable fallback before the
// one-time decision is recorded.
func (s *Service) Decide(ctx context.Context, request DecideRequest) (DecideResult, error) {
	record, err := s.cfg.Approvals.Get(ctx, strings.TrimSpace(request.ApprovalID))
	if err != nil {
		return DecideResult{}, err
	}
	if record.ExecutionMode == approval.ExecutionModeInterrupt && (record.CheckpointMappingID == "" || record.MappingEpoch <= 0) {
		fallback, err := s.cfg.Approvals.SwitchToDurableFallback(ctx, approval.FallbackCommand{
			ApprovalID: record.ID, ExpectedExecutionMode: record.ExecutionMode,
			ExpectedExecutionEpoch: record.ExecutionEpoch, ExpectedDecisionVersion: record.DecisionVersion,
			Now: s.cfg.Now(),
		})
		if err != nil {
			return DecideResult{}, fmt.Errorf("switch approval to durable fallback: %w", err)
		}
		record = fallback.Approval
	}
	var current approval.VersionBinding
	// If the decision was durably recorded but its continuation or response
	// projection failed, retry against the exact binding observed by that first
	// decision. Re-resolving after the frozen command ran would intentionally
	// see a changed artifact state and incorrectly turn a safe retry into an
	// idempotency conflict.
	if record.DecisionVersion > 0 {
		decision, decisionErr := s.cfg.Approvals.GetDecision(ctx, record.ID, record.DecisionVersion)
		if decisionErr == nil && decision.IdempotencyKey == strings.TrimSpace(request.IdempotencyKey) && decision.ObservedBinding != nil {
			current = *decision.ObservedBinding
		} else {
			current, err = s.ResolveBinding(ctx, record)
		}
	} else {
		current, err = s.ResolveBinding(ctx, record)
	}
	if err != nil {
		return DecideResult{}, err
	}
	result, err := s.cfg.Approvals.Decide(ctx, approval.DecideCommand{
		ApprovalID: record.ID, ExpectedDecisionVersion: request.ExpectedDecisionVersion,
		IdempotencyKey: request.IdempotencyKey, Decision: request.Decision,
		ActorID: request.ActorID, Reason: request.Reason, CurrentBinding: current, Now: s.cfg.Now(),
	})
	if err != nil {
		return DecideResult{}, err
	}
	if result.Approval.Status == approval.StatusStale {
		if err := s.cleanupStaleArtifact(ctx, result.Approval); err != nil {
			return DecideResult{Decision: result}, err
		}
	}
	continuation, _, err := s.cfg.Continuations.RequestContinuation(ctx, result.Continuation)
	if err != nil {
		return DecideResult{Decision: result}, err
	}
	// HTTP retries can observe an already-applied continuation. The durable
	// continuation record is authoritative. Apply recognizes that receipt and
	// retries only the stable Agent input enqueue; it never executes the frozen
	// command twice merely because the response projection was retried.
	if continuation.Status == sessionruntime.ContinuationStatusApplied {
		applied, applyErr := s.Apply(ctx, continuation)
		if applyErr != nil {
			return DecideResult{Decision: result}, applyErr
		}
		if applied {
			_ = s.cfg.Approvals.MarkOutboxPublished(ctx, result.Outbox.ID, s.cfg.Now())
		}
		return DecideResult{Decision: result, Applied: applied}, nil
	}
	if continuation.Executor == sessionruntime.ContinuationExecutorRunnerResume {
		if s.cfg.Inputs == nil {
			return DecideResult{Decision: result}, fmt.Errorf("session input enqueuer is required for runner resume")
		}
		var resume sessionruntime.ResumeRequested
		if err := json.Unmarshal(result.Outbox.Payload, &resume); err != nil {
			return DecideResult{Decision: result}, fmt.Errorf("decode resume outbox: %w", err)
		}
		if _, err := s.cfg.Inputs.Enqueue(ctx, record.SessionID, resume); err != nil {
			return DecideResult{Decision: result}, err
		}
		_ = s.cfg.Approvals.MarkOutboxPublished(ctx, result.Outbox.ID, s.cfg.Now())
		return DecideResult{Decision: result}, nil
	}
	applied, err := s.Apply(ctx, continuation)
	if err != nil {
		return DecideResult{Decision: result}, err
	}
	if applied {
		_ = s.cfg.Approvals.MarkOutboxPublished(ctx, result.Outbox.ID, s.cfg.Now())
	}
	return DecideResult{Decision: result, Applied: applied}, nil
}

func (s *Service) cleanupStaleArtifact(ctx context.Context, record approval.Approval) error {
	if s.cfg.StoryboardCommands == nil || s.cfg.Storyboards == nil {
		return nil
	}
	switch record.ArtifactType {
	case "storyboard_revision":
		aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, record.Binding.StoryboardID)
		if err != nil {
			return err
		}
		if aggregate.PendingRevisionID != record.Binding.ArtifactID {
			return nil
		}
		_, _, err = s.cfg.StoryboardCommands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{
			CommandID: "approval:" + record.ID + ":stale-cleanup", IdempotencyKey: "approval:" + record.ID + ":stale-cleanup",
			StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: record.Binding.ArtifactID,
			Decision: "stale", Source: "approval_stale_cleanup",
		})
		return err
	case "candidate_asset":
		aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, record.Binding.StoryboardID)
		if err != nil {
			return err
		}
		for _, binding := range aggregate.Bindings {
			if binding.ID != record.Binding.ArtifactID || binding.State != storyboard.BindingStateCandidate {
				continue
			}
			_, err = s.cfg.StoryboardCommands.RejectBinding(ctx, storyboard.RejectBindingCommand{
				CommandID: "approval:" + record.ID + ":stale-cleanup", StoryboardID: aggregate.ID,
				BaseVersion: aggregate.Version, BindingID: binding.ID,
			})
			return err
		}
	}
	return nil
}

// Apply claims the current executor/epoch, executes the selected idempotent
// domain command, records the shared command ledger, and only then emits the
// stable internal Agent continuation input. If the process stops between the
// domain commit and enqueue, replaying the approval outbox observes the
// applied continuation and safely retries the same input identity.
func (s *Service) Apply(ctx context.Context, continuation sessionruntime.ApprovalContinuation) (bool, error) {
	claimOwner := fmt.Sprintf("%s:%d", s.cfg.OwnerID, s.claimSequence.Add(1))
	claim := sessionruntime.ContinuationClaim{ApprovalID: continuation.ApprovalID, DecisionVersion: continuation.DecisionVersion, Executor: continuation.Executor, ExecutionEpoch: continuation.ExecutionEpoch, LeaseOwner: claimOwner}
	claimed, err := s.cfg.Continuations.ClaimContinuation(ctx, claim, s.cfg.LeaseTTL)
	if errors.Is(err, sessionruntime.ErrContinuationClaimed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if claimed.Status == sessionruntime.ContinuationStatusApplied {
		return s.finishApplied(ctx, claimed)
	}
	decision, err := s.cfg.Approvals.GetDecision(ctx, claimed.ApprovalID, claimed.DecisionVersion)
	if err != nil {
		return false, s.fail(ctx, claim, err)
	}
	record, err := s.cfg.Approvals.Get(ctx, claimed.ApprovalID)
	if err != nil {
		return false, s.fail(ctx, claim, err)
	}
	if record.DecisionVersion != claimed.DecisionVersion || record.ExecutionEpoch != claimed.ExecutionEpoch || record.Status != decision.EffectiveStatus {
		err = fmt.Errorf("approval continuation version fence rejected")
		return false, s.fail(ctx, claim, err)
	}
	current, resolveErr := s.ResolveBinding(ctx, record)
	if resolveErr != nil {
		return false, s.fail(ctx, claim, resolveErr)
	}
	if result, applied, inspectErr := s.inspectAppliedCommand(ctx, record, decision); inspectErr != nil {
		return false, s.fail(ctx, claim, inspectErr)
	} else if applied {
		payload, _ := json.Marshal(result)
		commands := []sessionruntime.ApprovalCommandLedger{{
			ApprovalID: decision.ApprovalID, DecisionVersion: decision.DecisionVersion,
			CommandKind: decision.CommandKind, ExecutionEpoch: claimed.ExecutionEpoch,
			IdempotencyKey: decision.CommandIdempotencyKey, CommandPayload: decision.CommandPayload,
			ResultPayload: payload,
		}}
		appliedContinuation, applyErr := s.cfg.Continuations.ApplyContinuation(ctx, claim, commands)
		if applyErr != nil {
			return false, applyErr
		}
		return s.finishApplied(ctx, appliedContinuation)
	}
	if !record.Binding.Matches(current) {
		return s.applySupersededNoop(ctx, claim, claimed, record, decision)
	}
	result := map[string]any{"status": record.Status}
	if decision.CommandKind != "" {
		result, err = s.executeCommand(ctx, record, decision)
		if errors.Is(err, artifact.ErrStale) || errors.Is(err, artifact.ErrNotReviewable) {
			// ResolveBinding and the actual transition are deliberately separated by
			// the approval continuation claim. The artifact store repeats the latest
			// and reviewable CAS inside its transaction; losing that race is a
			// terminal superseded no-op, not a transient command failure.
			return s.applySupersededNoop(ctx, claim, claimed, record, decision)
		}
		if err != nil {
			return false, s.fail(ctx, claim, err)
		}
	}
	payload, _ := json.Marshal(result)
	commands := []sessionruntime.ApprovalCommandLedger{}
	if decision.CommandKind != "" {
		commands = append(commands, sessionruntime.ApprovalCommandLedger{
			ApprovalID: decision.ApprovalID, DecisionVersion: decision.DecisionVersion,
			CommandKind: decision.CommandKind, ExecutionEpoch: claimed.ExecutionEpoch,
			IdempotencyKey: decision.CommandIdempotencyKey, CommandPayload: decision.CommandPayload,
			ResultPayload: payload,
		})
	}
	appliedContinuation, err := s.cfg.Continuations.ApplyContinuation(ctx, claim, commands)
	if err != nil {
		return false, err
	}
	return s.finishApplied(ctx, appliedContinuation)
}

func (s *Service) applySupersededNoop(ctx context.Context, claim sessionruntime.ContinuationClaim, claimed sessionruntime.ApprovalContinuation, record approval.Approval, decision approval.ApprovalDecision) (bool, error) {
	if cleanupErr := s.cleanupStaleArtifact(ctx, record); cleanupErr != nil {
		return false, s.fail(ctx, claim, cleanupErr)
	}
	payload, _ := json.Marshal(map[string]any{"status": approval.StatusStale, "superseded": true})
	commands := []sessionruntime.ApprovalCommandLedger{{
		ApprovalID: decision.ApprovalID, DecisionVersion: decision.DecisionVersion,
		CommandKind: "SupersededApprovalNoop", ExecutionEpoch: claimed.ExecutionEpoch,
		IdempotencyKey: "approval:" + decision.ApprovalID + ":superseded:" + fmt.Sprint(decision.DecisionVersion),
		ResultPayload:  payload,
	}}
	appliedContinuation, applyErr := s.cfg.Continuations.ApplyContinuation(ctx, claim, commands)
	if applyErr != nil {
		return false, applyErr
	}
	return s.finishApplied(ctx, appliedContinuation)
}

func (s *Service) finishApplied(ctx context.Context, continuation sessionruntime.ApprovalContinuation) (bool, error) {
	if continuation.Executor != sessionruntime.ContinuationExecutorDeterministic {
		return true, nil
	}
	if continuation.Status != sessionruntime.ContinuationStatusApplied {
		return false, fmt.Errorf("approval continuation is not applied: %s", continuation.Status)
	}
	if s.cfg.Inputs == nil {
		return false, fmt.Errorf("session input enqueuer is required for deterministic approval continuation")
	}
	record, err := s.cfg.Approvals.Get(ctx, continuation.ApprovalID)
	if err != nil {
		return false, err
	}
	decision, err := s.cfg.Approvals.GetDecision(ctx, continuation.ApprovalID, continuation.DecisionVersion)
	if err != nil {
		return false, err
	}
	if record.SessionID != continuation.SessionID || record.DecisionVersion != continuation.DecisionVersion ||
		record.ExecutionEpoch != continuation.ExecutionEpoch || record.Status != decision.EffectiveStatus {
		return false, fmt.Errorf("approval continuation result fence rejected")
	}

	input := sessionruntime.NewApprovalContinuationResult(record.ID, decision.DecisionVersion, continuation.ExecutionEpoch, "")
	input.RequestedDecision = string(decision.RequestedDecision)
	input.EffectiveStatus = string(decision.EffectiveStatus)
	input.ArtifactType = record.ArtifactType
	input.ArtifactID = record.Binding.ArtifactID
	input.ArtifactVersion = record.Binding.ArtifactVersion
	input.StoryboardID = record.Binding.StoryboardID
	input.StoryboardVersion = record.Binding.StoryboardVersion
	input.CommandKind = decision.CommandKind
	input.CommandResult, err = s.appliedCommandResult(ctx, decision)
	if err != nil {
		return false, err
	}
	if _, err := s.cfg.Inputs.Enqueue(ctx, record.SessionID, input); err != nil {
		return false, err
	}
	// Manager.Enqueue already wakes the local lane. This explicit signal also
	// keeps alternate enqueuer implementations responsive; discovery remains
	// the durable fallback if the process stops here.
	s.cfg.Inputs.Wake(record.SessionID)
	return true, nil
}

func (s *Service) appliedCommandResult(ctx context.Context, decision approval.ApprovalDecision) (json.RawMessage, error) {
	if strings.TrimSpace(decision.CommandKind) == "" {
		return json.Marshal(map[string]any{"effective_status": decision.EffectiveStatus})
	}
	command, err := s.cfg.Continuations.GetCommand(ctx, decision.ApprovalID, decision.DecisionVersion, decision.CommandKind)
	if errors.Is(err, sessionruntime.ErrContinuationNotFound) {
		// A semantic target can be superseded after the decision is frozen but
		// before its command runs. That terminal no-op has its own durable ledger
		// receipt and must still wake the Agent so it can explain/replan.
		command, err = s.cfg.Continuations.GetCommand(ctx, decision.ApprovalID, decision.DecisionVersion, "SupersededApprovalNoop")
	}
	if err != nil {
		return nil, err
	}
	result := append(json.RawMessage(nil), command.ResultPayload...)
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("approval command result is invalid JSON")
	}
	return result, nil
}

// inspectAppliedCommand closes the crash window between a successful domain
// commit and the continuation ledger write. It recognizes the frozen command's
// own durable receipt instead of misclassifying the resulting version change as
// a superseding user action.
func (s *Service) inspectAppliedCommand(ctx context.Context, record approval.Approval, decision approval.ApprovalDecision) (map[string]any, bool, error) {
	if strings.TrimSpace(decision.CommandKind) == "" || strings.TrimSpace(decision.CommandIdempotencyKey) == "" {
		return nil, false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(decision.CommandPayload, &payload); err != nil {
		return nil, false, err
	}
	switch decision.CommandKind {
	case "PromoteStoryboardRevision", "RejectAndArchivePendingRevision", "ActivateArtifactBinding", "RejectArtifactBinding":
		if s.cfg.Storyboards == nil {
			return nil, false, fmt.Errorf("storyboard repository is required")
		}
		aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, stringValue(payload, "storyboard_id"))
		if err != nil {
			return nil, false, err
		}
		if !aggregate.HasApplied(decision.CommandIdempotencyKey) {
			return nil, false, nil
		}
		return map[string]any{"aggregate": aggregate.PublicView(), "recovered_domain_commit": true}, true, nil
	case "ActivateCreationSpecRevision", "RejectCreationSpecRevision":
		if s.cfg.Specs == nil {
			return nil, false, fmt.Errorf("spec revision store is required")
		}
		value, err := s.cfg.Specs.GetRevision(ctx, stringValue(payload, "spec_id"), intValue(payload, "spec_version"))
		if err != nil {
			return nil, false, err
		}
		expected := spec.StatusConfirmed
		if decision.CommandKind == "RejectCreationSpecRevision" {
			expected = spec.StatusRejected
		}
		if value.Status != expected {
			return nil, false, nil
		}
		return structMap(value), true, nil
	case "MarkExportAccepted", "RejectExportResult":
		if s.cfg.Artifacts == nil {
			return nil, false, fmt.Errorf("artifact store is required")
		}
		receipt, err := s.cfg.Artifacts.GetReviewReceipt(ctx, decision.CommandIdempotencyKey)
		if errors.Is(err, artifact.ErrNotFound) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		expectedDecision := artifact.ReviewDecisionApprove
		if decision.CommandKind == "RejectExportResult" {
			expectedDecision = artifact.ReviewDecisionReject
		}
		artifactID, artifactVersion := stringValue(payload, "artifact_id"), intValue(payload, "artifact_version")
		if receipt.IdempotencyKey != decision.CommandIdempotencyKey || receipt.SessionID != record.SessionID ||
			receipt.ArtifactID != artifactID || receipt.ArtifactKind != artifact.KindExportResult ||
			receipt.ArtifactVersion != artifactVersion || receipt.ExpectedStatus != artifact.StatusReviewing ||
			receipt.Decision != expectedDecision || !receipt.RequireLatest {
			return nil, false, fmt.Errorf("%w: export approval command receipt changed", artifact.ErrIdempotencyConflict)
		}
		expectedResultStatus := artifact.StatusActive
		if expectedDecision == artifact.ReviewDecisionReject {
			expectedResultStatus = artifact.StatusRejected
		}
		if receipt.Result.ID != artifactID || receipt.Result.SessionID != record.SessionID ||
			receipt.Result.Kind != artifact.KindExportResult || receipt.Result.Version != artifactVersion ||
			receipt.Result.Status != expectedResultStatus {
			return nil, false, fmt.Errorf("%w: export approval command result receipt changed", artifact.ErrIdempotencyConflict)
		}
		result := structMap(receipt.Result)
		result["recovered_domain_commit"] = true
		return result, true, nil
	default:
		return nil, false, nil
	}
}

func (s *Service) fail(ctx context.Context, claim sessionruntime.ContinuationClaim, cause error) error {
	_, failErr := s.cfg.Continuations.FailContinuation(ctx, claim, sessionruntime.Failure{Code: "approval_command_failed", Message: cause.Error()})
	if failErr != nil {
		return errors.Join(cause, failErr)
	}
	return cause
}

func (s *Service) executeCommand(ctx context.Context, record approval.Approval, decision approval.ApprovalDecision) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(decision.CommandPayload, &payload); err != nil {
		return nil, err
	}
	commandID := decision.CommandIdempotencyKey
	switch decision.CommandKind {
	case "ActivateCreationSpecRevision", "RejectCreationSpecRevision":
		if s.cfg.Specs == nil {
			return nil, fmt.Errorf("spec revision store is required")
		}
		value, err := s.cfg.Specs.DecideRevision(ctx, stringValue(payload, "spec_id"), intValue(payload, "spec_version"), decision.CommandKind == "ActivateCreationSpecRevision")
		return structMap(value), err
	case "PromoteStoryboardRevision", "RejectAndArchivePendingRevision":
		if s.cfg.StoryboardCommands == nil || s.cfg.Storyboards == nil {
			return nil, fmt.Errorf("storyboard command service is required")
		}
		current, loadErr := s.cfg.Storyboards.GetAggregate(ctx, stringValue(payload, "storyboard_id"))
		if loadErr != nil {
			return nil, loadErr
		}
		decisionValue := "approved"
		if decision.CommandKind != "PromoteStoryboardRevision" {
			decisionValue = "rejected"
		}
		value, diff, err := s.cfg.StoryboardCommands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{
			CommandID: commandID, IdempotencyKey: decision.CommandIdempotencyKey,
			StoryboardID: current.ID, BaseVersion: current.Version,
			RevisionID: stringValue(payload, "revision_id"), Decision: decisionValue, Source: "approval",
		})
		return map[string]any{"aggregate": value, "diff": diff}, err
	case "ActivateArtifactBinding":
		if s.cfg.StoryboardCommands == nil || s.cfg.Storyboards == nil {
			return nil, fmt.Errorf("storyboard command service is required")
		}
		current, err := s.cfg.Storyboards.GetAggregate(ctx, stringValue(payload, "storyboard_id"))
		if err != nil {
			return nil, err
		}
		value, stale, err := s.cfg.StoryboardCommands.Activate(ctx, storyboard.ActivateBindingCommand{CommandID: commandID, StoryboardID: current.ID, BaseVersion: current.Version, BindingID: stringValue(payload, "binding_id")})
		return map[string]any{"aggregate": value, "stale_targets": stale}, err
	case "RejectArtifactBinding":
		if s.cfg.StoryboardCommands == nil || s.cfg.Storyboards == nil {
			return nil, fmt.Errorf("storyboard command service is required")
		}
		current, err := s.cfg.Storyboards.GetAggregate(ctx, stringValue(payload, "storyboard_id"))
		if err != nil {
			return nil, err
		}
		value, err := s.cfg.StoryboardCommands.RejectBinding(ctx, storyboard.RejectBindingCommand{CommandID: commandID, StoryboardID: current.ID, BaseVersion: current.Version, BindingID: stringValue(payload, "binding_id")})
		return map[string]any{"aggregate": value}, err
	case "MarkExportAccepted", "RejectExportResult":
		if s.cfg.Artifacts == nil {
			return nil, fmt.Errorf("artifact store is required")
		}
		id, version := stringValue(payload, "artifact_id"), intValue(payload, "artifact_version")
		reviewDecision := artifact.ReviewDecisionApprove
		if decision.CommandKind == "RejectExportResult" {
			reviewDecision = artifact.ReviewDecisionReject
		}
		result, err := s.cfg.Artifacts.ApplyReview(ctx, artifact.ReviewCommand{
			IdempotencyKey: decision.CommandIdempotencyKey, SessionID: record.SessionID,
			ArtifactID: id, ArtifactKind: artifact.KindExportResult, ArtifactVersion: version,
			ExpectedStatus: artifact.StatusReviewing, Decision: reviewDecision, RequireLatest: true,
		})
		return structMap(result.Revision), err
	default:
		return nil, fmt.Errorf("unsupported frozen approval command %q", decision.CommandKind)
	}
}

// ResolveBinding reloads the reviewed semantic target; clients never supply
// authoritative versions.
func (s *Service) ResolveBinding(ctx context.Context, record approval.Approval) (approval.VersionBinding, error) {
	binding := record.Binding
	switch record.ArtifactType {
	case "creation_spec_revision":
		if s.cfg.Specs == nil {
			return binding, fmt.Errorf("spec revision store is required")
		}
		value, err := s.cfg.Specs.GetRevision(ctx, binding.ArtifactID, binding.ArtifactVersion)
		if err != nil {
			return approval.VersionBinding{}, err
		}
		if value.Status != spec.StatusReviewing || value.SessionID != record.SessionID {
			binding.ArtifactVersion = -1
			return binding, nil
		}
		latest, err := s.cfg.Specs.GetLatestReviewingBySession(ctx, value.SessionID)
		if err != nil {
			if errors.Is(err, spec.ErrNotFound) {
				binding.ArtifactVersion = -1
				return binding, nil
			}
			return approval.VersionBinding{}, err
		}
		if latest.ID != value.ID || latest.Version != value.Version {
			binding.ArtifactVersion = -1
		}
		return binding, nil
	case "storyboard_revision":
		if s.cfg.Storyboards == nil {
			return binding, fmt.Errorf("storyboard repository is required")
		}
		aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, binding.StoryboardID)
		if err != nil {
			return approval.VersionBinding{}, err
		}
		if aggregate.PendingRevisionID != binding.ArtifactID {
			binding.ArtifactVersion = -1
		}
		if matches, matchErr := s.storyboardMatchesConfirmedSpec(ctx, aggregate, binding.ArtifactID); matchErr != nil {
			return approval.VersionBinding{}, matchErr
		} else if !matches {
			binding.ArtifactVersion = -1
		}
		// Direct edits performed by the reviewing user are part of this same
		// pending revision. The frozen command rebases on the current aggregate
		// version while the stable pending revision ID remains authoritative.
		return binding, nil
	case "candidate_asset":
		if s.cfg.Storyboards == nil {
			return binding, fmt.Errorf("storyboard repository is required")
		}
		aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, binding.StoryboardID)
		if err != nil {
			return approval.VersionBinding{}, err
		}
		if matches, matchErr := s.storyboardMatchesConfirmedSpec(ctx, aggregate, aggregate.ActiveRevisionID); matchErr != nil {
			return approval.VersionBinding{}, matchErr
		} else if !matches {
			binding.ArtifactVersion = -1
			return binding, nil
		}
		return currentCandidateBinding(aggregate, binding)
	case "export_result":
		if s.cfg.Artifacts == nil {
			return binding, fmt.Errorf("artifact store is required")
		}
		value, err := s.cfg.Artifacts.Get(ctx, binding.ArtifactID)
		if err != nil {
			return approval.VersionBinding{}, err
		}
		if value.SessionID != record.SessionID || value.Kind != artifact.KindExportResult ||
			value.Version != binding.ArtifactVersion || value.Status != artifact.StatusReviewing {
			binding.ArtifactVersion = -1
			return binding, nil
		}
		latest, err := s.cfg.Artifacts.GetLatest(ctx, record.SessionID, artifact.KindExportResult)
		if errors.Is(err, artifact.ErrNotFound) {
			binding.ArtifactVersion = -1
			return binding, nil
		}
		if err != nil {
			return approval.VersionBinding{}, err
		}
		if latest.ID != value.ID || latest.Version != value.Version {
			binding.ArtifactVersion = -1
		}
		return binding, nil
	default:
		return binding, nil
	}
}

func (s *Service) storyboardMatchesConfirmedSpec(ctx context.Context, aggregate storyboard.StoryboardAggregate, revisionID string) (bool, error) {
	confirmedSource, ok := s.cfg.Specs.(ConfirmedSpecSource)
	if !ok || strings.TrimSpace(revisionID) == "" {
		return true, nil
	}
	var revision *storyboard.StoryboardRevision
	for index := range aggregate.Revisions {
		if aggregate.Revisions[index].ID == revisionID {
			revision = &aggregate.Revisions[index]
			break
		}
	}
	if revision == nil {
		return false, nil
	}
	confirmed, err := confirmedSource.GetConfirmedBySession(ctx, aggregate.SessionID)
	if err != nil {
		if errors.Is(err, spec.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return revision.DerivedFromSpecVersion == 0 || revision.DerivedFromSpecVersion == confirmed.Version, nil
}

func currentCandidateBinding(aggregate storyboard.StoryboardAggregate, binding approval.VersionBinding) (approval.VersionBinding, error) {
	var candidate *storyboard.ArtifactBinding
	for _, stored := range aggregate.Bindings {
		if stored.ID == binding.ArtifactID && stored.State == storyboard.BindingStateCandidate {
			copy := stored
			candidate = &copy
			break
		}
	}
	if candidate == nil || candidate.ArtifactRevision != binding.ArtifactVersion {
		binding.ArtifactVersion = -1
		return binding, nil
	}
	current, err := aggregate.ResolveGenerationInput(candidate.TargetID, candidate.AssetSlot)
	if err != nil || candidate.TargetID != binding.TargetID ||
		candidate.TargetRevision != current.TargetRevision ||
		candidate.PromptRevision != current.PromptRevision ||
		candidate.GenerationEpoch != current.GenerationEpoch ||
		strings.TrimSpace(candidate.InputFingerprint) == "" || candidate.InputFingerprint != current.Fingerprint {
		binding.ArtifactVersion = -1
		return binding, nil
	}
	revision, revisionErr := aggregate.ActiveRevision()
	if revisionErr != nil || !candidateAttachedToSlot(revision, candidate.TargetID, candidate.AssetSlot, candidate.ID) {
		binding.ArtifactVersion = -1
		return binding, nil
	}
	binding.TargetRevision = current.TargetRevision
	binding.PromptRevision = current.PromptRevision
	binding.GenerationEpoch = current.GenerationEpoch
	return binding, nil
}

func candidateAttachedToSlot(revision *storyboard.StoryboardRevision, targetID, slotKey, bindingID string) bool {
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			if element.ID != targetID {
				continue
			}
			for _, slot := range element.AssetSlots {
				if slot.Key != slotKey {
					continue
				}
				for _, candidateID := range slot.CandidateIDs {
					if candidateID == bindingID {
						return true
					}
				}
			}
		}
	}
	return false
}

func stringValue(values map[string]any, key string) string {
	return strings.TrimSpace(fmt.Sprint(values[key]))
}
func intValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}
func structMap(value any) map[string]any {
	raw, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}
