package approvalruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type memorySpecRevisions struct {
	value         spec.FinalVideoSpec
	latest        *spec.FinalVideoSpec
	decideCalls   int
	failDecisions int
}

type memoryRuntimeInputs struct {
	store      *sessionruntime.MemoryStore
	enqueueErr error
	calls      int
	wakes      int
}

func (r *memoryRuntimeInputs) Enqueue(ctx context.Context, sessionID string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	r.calls++
	if r.enqueueErr != nil {
		return sessionruntime.EnqueueResult{}, r.enqueueErr
	}
	return r.store.EnqueueInput(ctx, sessionID, input)
}

func (r *memoryRuntimeInputs) Wake(string) { r.wakes++ }

func runtimeInputs(store *sessionruntime.MemoryStore) *memoryRuntimeInputs {
	return &memoryRuntimeInputs{store: store}
}

func createExportApproval(t *testing.T, ctx context.Context, approvals approval.Store, revision artifact.Revision, approvalID string) approval.Approval {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"artifact_id": revision.ID, "artifact_version": revision.Version})
	if err != nil {
		t.Fatal(err)
	}
	created, err := approvals.Create(ctx, approval.Approval{
		ID: approvalID, IdempotencyKey: approvalID + ":create", SessionID: revision.SessionID,
		ArtifactType: artifact.KindExportResult,
		Binding:      approval.VersionBinding{ArtifactID: revision.ID, ArtifactVersion: revision.Version},
		ReviewMode:   approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
		ApproveCommand: approval.FrozenCommand{Kind: "MarkExportAccepted", IdempotencyKey: approvalID + ":approve", Payload: payload},
		RejectCommand:  approval.FrozenCommand{Kind: "RejectExportResult", IdempotencyKey: approvalID + ":reject", Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.Approval
}

func TestOlderExportApprovalCannotRollBackNewerActiveArtifact(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	first, err := artifacts.CreateRevision(ctx, artifact.Revision{
		ID: "export-a", SessionID: "s1", Kind: artifact.KindExportResult, Status: artifact.StatusReviewing,
		IdempotencyKey: "create-export-a", Content: map[string]any{"url": "a.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalA := createExportApproval(t, ctx, approvals, first.Revision, "approval-export-a")
	second, err := artifacts.CreateRevision(ctx, artifact.Revision{
		ID: "export-b", SessionID: "s1", Kind: artifact.KindExportResult, Status: artifact.StatusReviewing,
		IdempotencyKey: "create-export-b", Content: map[string]any{"url": "b.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalB := createExportApproval(t, ctx, approvals, second.Revision, "approval-export-b")
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := service.Decide(ctx, DecideRequest{ApprovalID: approvalB.ID, IdempotencyKey: "decision-export-b", Decision: approval.DecisionApprove})
	if err != nil || !newer.Applied || newer.Decision.Approval.Status != approval.StatusApproved {
		t.Fatalf("newer decision=%+v err=%v", newer, err)
	}
	older, err := service.Decide(ctx, DecideRequest{ApprovalID: approvalA.ID, IdempotencyKey: "decision-export-a", Decision: approval.DecisionApprove})
	if err != nil || !older.Applied {
		t.Fatalf("older decision=%+v err=%v", older, err)
	}
	if older.Decision.Approval.Status != approval.StatusStale || older.Decision.Decision.CommandKind != "" {
		t.Fatalf("older approval was not a stale no-op: %+v", older.Decision)
	}
	oldArtifact, _ := artifacts.Get(ctx, first.Revision.ID)
	currentArtifact, _ := artifacts.Get(ctx, second.Revision.ID)
	if oldArtifact.Status == artifact.StatusActive || currentArtifact.Status != artifact.StatusActive {
		t.Fatalf("old approval rolled back current artifact: old=%s current=%s", oldArtifact.Status, currentArtifact.Status)
	}
	if _, err := artifacts.GetReviewReceipt(ctx, approvalA.ApproveCommand.IdempotencyKey); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("stale approval unexpectedly executed: %v", err)
	}
}

func TestExportDomainCommitRecoveryUsesReceiptWithoutReactivatingOldArtifact(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	first, err := artifacts.CreateRevision(ctx, artifact.Revision{
		ID: "crash-export-a", SessionID: "s1", Kind: artifact.KindExportResult, Status: artifact.StatusReviewing,
		IdempotencyKey: "create-crash-export-a", Content: map[string]any{"url": "a.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalA := createExportApproval(t, ctx, approvals, first.Revision, "approval-crash-export-a")
	decision, err := approvals.Decide(ctx, approval.DecideCommand{
		ApprovalID: approvalA.ID, ExpectedDecisionVersion: 0, IdempotencyKey: "decision-crash-export-a",
		Decision: approval.DecisionApprove, CurrentBinding: approvalA.Binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	continuation, _, err := continuations.RequestContinuation(ctx, decision.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the exact crash window: the artifact command and its receipt
	// commit, but the process stops before ApprovalContinuation is applied.
	if result, err := artifacts.ApplyReview(ctx, artifact.ReviewCommand{
		IdempotencyKey: decision.Decision.CommandIdempotencyKey, SessionID: approvalA.SessionID,
		ArtifactID: first.Revision.ID, ArtifactKind: artifact.KindExportResult, ArtifactVersion: first.Revision.Version,
		ExpectedStatus: artifact.StatusReviewing, Decision: artifact.ReviewDecisionApprove, RequireLatest: true,
	}); err != nil || !result.Applied {
		t.Fatalf("simulated domain commit=%+v err=%v", result, err)
	}
	second, err := artifacts.CreateRevision(ctx, artifact.Revision{
		ID: "crash-export-b", SessionID: "s1", Kind: artifact.KindExportResult, Status: artifact.StatusReviewing,
		IdempotencyKey: "create-crash-export-b", Content: map[string]any{"url": "b.mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.ApplyReview(ctx, artifact.ReviewCommand{
		IdempotencyKey: "approve-crash-export-b", SessionID: "s1", ArtifactID: second.Revision.ID,
		ArtifactKind: artifact.KindExportResult, ArtifactVersion: second.Revision.Version,
		ExpectedStatus: artifact.StatusReviewing, Decision: artifact.ReviewDecisionApprove, RequireLatest: true,
	}); err != nil {
		t.Fatal(err)
	}

	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := service.Apply(ctx, continuation)
	if err != nil || !applied {
		t.Fatalf("continuation recovery applied=%v err=%v", applied, err)
	}
	oldArtifact, _ := artifacts.Get(ctx, first.Revision.ID)
	currentArtifact, _ := artifacts.Get(ctx, second.Revision.ID)
	if oldArtifact.Status != artifact.StatusSuperseded || currentArtifact.Status != artifact.StatusActive {
		t.Fatalf("recovery reactivated old export: old=%s current=%s", oldArtifact.Status, currentArtifact.Status)
	}
	ledger, err := continuations.GetCommand(ctx, approvalA.ID, decision.Decision.DecisionVersion, "MarkExportAccepted")
	if err != nil {
		t.Fatal(err)
	}
	var ledgerResult map[string]any
	if err := json.Unmarshal(ledger.ResultPayload, &ledgerResult); err != nil {
		t.Fatal(err)
	}
	if recovered, _ := ledgerResult["recovered_domain_commit"].(bool); !recovered {
		t.Fatalf("ledger did not record receipt recovery: %s", ledger.ResultPayload)
	}
	receipt, err := artifacts.GetReviewReceipt(ctx, decision.Decision.CommandIdempotencyKey)
	if err != nil || receipt.Result.ID != first.Revision.ID || receipt.Result.Status != artifact.StatusActive {
		t.Fatalf("frozen domain receipt=%+v err=%v", receipt, err)
	}
}

func (s *memorySpecRevisions) GetRevision(_ context.Context, id string, version int) (spec.FinalVideoSpec, error) {
	return s.value, nil
}
func (s *memorySpecRevisions) GetLatestReviewingBySession(_ context.Context, _ string) (spec.FinalVideoSpec, error) {
	if s.latest != nil {
		return *s.latest, nil
	}
	if s.value.Status != spec.StatusReviewing {
		return spec.FinalVideoSpec{}, spec.ErrNotFound
	}
	return s.value, nil
}

func TestResolveCreationSpecBindingRequiresLatestReviewingRevision(t *testing.T) {
	older := spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Version: 2, Status: spec.StatusReviewing}
	latest := spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Version: 3, Status: spec.StatusReviewing}
	service, err := New(Config{
		Approvals: approval.NewMemoryStore(), Continuations: sessionruntime.NewMemoryStore(),
		Specs: &memorySpecRevisions{value: older, latest: &latest},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.ResolveBinding(context.Background(), approval.Approval{
		SessionID: "s1", ArtifactType: "creation_spec_revision",
		Binding: approval.VersionBinding{ArtifactID: older.ID, ArtifactVersion: older.Version},
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.ArtifactVersion != -1 {
		t.Fatalf("older reviewing binding was not fenced: %+v", binding)
	}
}
func (s *memorySpecRevisions) DecideRevision(_ context.Context, id string, version int, approved bool) (spec.FinalVideoSpec, error) {
	if s.failDecisions > 0 {
		s.failDecisions--
		return s.value, errors.New("temporary spec store failure")
	}
	s.decideCalls++
	if approved {
		s.value.Status = spec.StatusConfirmed
	} else {
		s.value.Status = spec.StatusRejected
	}
	return s.value, nil
}

func TestCreationSpecApprovalCannotRollBackAfterLatestCandidateIsApproved(t *testing.T) {
	ctx := context.Background()
	specs := spec.NewMemoryStore()
	baseline, err := specs.Save(ctx, spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Status: spec.StatusReviewing, Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := specs.DecideRevision(ctx, baseline.ID, baseline.Version, true); err != nil {
		t.Fatal(err)
	}
	v2, err := specs.Save(ctx, spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Status: spec.StatusReviewing, Title: "v2"})
	if err != nil {
		t.Fatal(err)
	}

	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	createApproval := func(value spec.FinalVideoSpec) string {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"spec_id": value.ID, "spec_version": value.Version})
		id := fmt.Sprintf("approval-v%d", value.Version)
		created, createErr := approvals.Create(ctx, approval.Approval{
			ID: id, IdempotencyKey: id + ":create", SessionID: value.SessionID, ArtifactType: "creation_spec_revision",
			Binding:    approval.VersionBinding{ArtifactID: value.ID, ArtifactVersion: value.Version},
			ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
			ApproveCommand: approval.FrozenCommand{Kind: "ActivateCreationSpecRevision", IdempotencyKey: id + ":approve", Payload: payload},
			RejectCommand:  approval.FrozenCommand{Kind: "RejectCreationSpecRevision", IdempotencyKey: id + ":reject", Payload: payload},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return created.Approval.ID
	}
	v2ApprovalID := createApproval(v2)
	v3, err := specs.Save(ctx, spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Status: spec.StatusReviewing, Title: "v3"})
	if err != nil {
		t.Fatal(err)
	}
	// The primary-review invariant no longer allows v2 and v3 Approval rows to
	// remain pending together. Simulate the competing latest semantic commit
	// directly; the older frozen v2 decision must still resolve to stale.
	if _, err := specs.DecideRevision(ctx, v3.ID, v3.Version, true); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Specs: specs})
	if err != nil {
		t.Fatal(err)
	}

	staleDecision, err := service.Decide(ctx, DecideRequest{ApprovalID: v2ApprovalID, IdempotencyKey: "decision-v2", Decision: approval.DecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if staleDecision.Decision.Approval.Status != approval.StatusStale {
		t.Fatalf("old approval status=%s", staleDecision.Decision.Approval.Status)
	}
	confirmed, err := specs.GetConfirmedBySession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Version != v3.Version || confirmed.Title != "v3" {
		t.Fatalf("confirmed spec=%+v", confirmed)
	}
	old, err := specs.GetRevision(ctx, v2.ID, v2.Version)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != spec.StatusSuperseded {
		t.Fatalf("v2 status=%s", old.Status)
	}
}

func TestFailedApprovalContinuationIsRetriedByRelay(t *testing.T) {
	ctx := context.Background()
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	specs := &memorySpecRevisions{value: spec.FinalVideoSpec{ID: "spec-retry", SessionID: "s1", Version: 1, Status: spec.StatusReviewing}, failDecisions: 1}
	payload, _ := json.Marshal(map[string]any{"spec_id": "spec-retry", "spec_version": 1})
	_, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-retry", IdempotencyKey: "approval-retry-create", SessionID: "s1", ArtifactType: "creation_spec_revision",
		Binding:    approval.VersionBinding{ArtifactID: "spec-retry", ArtifactVersion: 1},
		ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
		ApproveCommand: approval.FrozenCommand{Kind: "ActivateCreationSpecRevision", IdempotencyKey: "approval-retry-activate", Payload: payload},
		RejectCommand:  approval.FrozenCommand{Kind: "RejectCreationSpecRevision", IdempotencyKey: "approval-retry-reject", Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Specs: specs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(ctx, DecideRequest{ApprovalID: "approval-retry", IdempotencyKey: "approval-retry-decision", Decision: approval.DecisionApprove}); err == nil {
		t.Fatal("first frozen command execution should fail")
	}
	failed, err := continuations.GetContinuation(ctx, "approval-retry", 1)
	if err != nil || failed.Status != sessionruntime.ContinuationStatusFailed {
		t.Fatalf("failed continuation = %+v, err=%v", failed, err)
	}
	published, err := service.RelayOnce(ctx, 10)
	if err != nil || published != 1 {
		t.Fatalf("relay published=%d err=%v", published, err)
	}
	applied, err := continuations.GetContinuation(ctx, "approval-retry", 1)
	if err != nil || applied.Status != sessionruntime.ContinuationStatusApplied || specs.value.Status != spec.StatusConfirmed {
		t.Fatalf("applied continuation = %+v spec=%+v err=%v", applied, specs.value, err)
	}
}

func TestAppliedApprovalContinuationEnqueueIsRecoveredByOutboxReplay(t *testing.T) {
	ctx := context.Background()
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	inputs := runtimeInputs(continuations)
	inputs.enqueueErr = errors.New("simulated process stop before session input enqueue")
	specs := &memorySpecRevisions{value: spec.FinalVideoSpec{ID: "spec-enqueue-recovery", SessionID: "s1", Version: 1, Status: spec.StatusReviewing}}
	payload, _ := json.Marshal(map[string]any{"spec_id": specs.value.ID, "spec_version": specs.value.Version})
	_, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-enqueue-recovery", IdempotencyKey: "create:approval-enqueue-recovery",
		SessionID: "s1", ArtifactType: "creation_spec_revision",
		Binding:    approval.VersionBinding{ArtifactID: specs.value.ID, ArtifactVersion: specs.value.Version},
		ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
		ApproveCommand: approval.FrozenCommand{Kind: "ActivateCreationSpecRevision", IdempotencyKey: "approve:enqueue-recovery", Payload: payload},
		RejectCommand:  approval.FrozenCommand{Kind: "RejectCreationSpecRevision", IdempotencyKey: "reject:enqueue-recovery", Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: inputs, Specs: specs})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Decide(ctx, DecideRequest{
		ApprovalID: "approval-enqueue-recovery", IdempotencyKey: "decision:enqueue-recovery",
		Decision: approval.DecisionApprove,
	}); err == nil {
		t.Fatal("decision unexpectedly hid the failed continuation enqueue")
	}
	applied, err := continuations.GetContinuation(ctx, "approval-enqueue-recovery", 1)
	if err != nil || applied.Status != sessionruntime.ContinuationStatusApplied || specs.decideCalls != 1 {
		t.Fatalf("continuation=%+v decide_calls=%d err=%v", applied, specs.decideCalls, err)
	}
	if pending, _ := approvals.ListOutbox(ctx, approval.OutboxStatusPending, 10); len(pending) != 1 {
		t.Fatalf("approval outbox was acknowledged before enqueue: %+v", pending)
	}

	inputs.enqueueErr = nil
	published, err := service.RelayOnce(ctx, 10)
	if err != nil || published != 1 {
		t.Fatalf("relay published=%d err=%v", published, err)
	}
	inputID := "approval:approval-enqueue-recovery:continuation-result:1"
	stored, err := continuations.GetInput(ctx, inputID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := sessionruntime.DecodeInput(stored)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.(sessionruntime.ApprovalContinuationResult)
	if !ok || result.EffectiveStatus != string(approval.StatusApproved) || result.CommandKind != "ActivateCreationSpecRevision" || !json.Valid(result.CommandResult) {
		t.Fatalf("approval continuation input=%#v", decoded)
	}
	if specs.decideCalls != 1 {
		t.Fatalf("frozen approval command was repeated: %d", specs.decideCalls)
	}
	// Outbox replay and HTTP replay both converge on the exact same input row.
	if _, err := service.Decide(ctx, DecideRequest{
		ApprovalID: "approval-enqueue-recovery", IdempotencyKey: "decision:enqueue-recovery",
		Decision: approval.DecisionApprove,
	}); err != nil {
		t.Fatal(err)
	}
	if replayed, err := continuations.GetInput(ctx, inputID); err != nil || replayed.EnqueueSeq != stored.EnqueueSeq {
		t.Fatalf("replayed input=%+v err=%v", replayed, err)
	}
}

func TestRelayDoesNotAckContinuationOwnedByAnotherExecutor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 14, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	approvals := approval.NewMemoryStoreWithClock(clock)
	created, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-relay-lease", IdempotencyKey: "create:approval-relay-lease", SessionID: "s1",
		ArtifactType: "generic_review", Binding: approval.VersionBinding{ArtifactID: "artifact-1", ArtifactVersion: 1},
		ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
		ApproveCommand: approval.FrozenCommand{Kind: "approve_generic", IdempotencyKey: "approve:relay-lease", Payload: json.RawMessage(`{}`)},
		RejectCommand:  approval.FrozenCommand{Kind: "reject_generic", IdempotencyKey: "reject:relay-lease", Payload: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := approvals.Decide(ctx, approval.DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision:relay-lease", Decision: approval.DecisionApprove,
		CurrentBinding: approval.VersionBinding{ArtifactID: "superseded", ArtifactVersion: 1}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	continuations := sessionruntime.NewMemoryStoreWithClock(clock)
	continuation, _, err := continuations.RequestContinuation(ctx, decision.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := continuations.ClaimContinuation(ctx, sessionruntime.ContinuationClaim{
		ApprovalID: continuation.ApprovalID, DecisionVersion: continuation.DecisionVersion,
		Executor: continuation.Executor, ExecutionEpoch: continuation.ExecutionEpoch, LeaseOwner: "crashed-owner",
	}, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), OwnerID: "relay-worker", LeaseTTL: 30 * time.Second, Now: clock})
	if err != nil {
		t.Fatal(err)
	}

	published, err := service.RelayOnce(ctx, 10)
	if err != nil || published != 0 {
		t.Fatalf("busy relay published=%d err=%v", published, err)
	}
	pending, err := approvals.ListOutbox(ctx, approval.OutboxStatusPending, 10)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 {
		t.Fatalf("pending outbox=%+v err=%v", pending, err)
	}
	if !pending[0].AvailableAt.After(now.Add(30 * time.Second)) {
		t.Fatalf("busy outbox retry_at=%s, want after lease expiry", pending[0].AvailableAt)
	}

	now = pending[0].AvailableAt.Add(time.Millisecond)
	published, err = service.RelayOnce(ctx, 10)
	if err != nil || published != 1 {
		t.Fatalf("recovered relay published=%d err=%v", published, err)
	}
	applied, err := continuations.GetContinuation(ctx, continuation.ApprovalID, continuation.DecisionVersion)
	if err != nil || applied.Status != sessionruntime.ContinuationStatusApplied {
		t.Fatalf("continuation=%+v err=%v", applied, err)
	}
	remaining, err := approvals.ListOutbox(ctx, approval.OutboxStatusPending, 10)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining outbox=%+v err=%v", remaining, err)
	}
}
func (s *memorySpecRevisions) GetConfirmedBySession(_ context.Context, _ string) (spec.FinalVideoSpec, error) {
	return s.value, nil
}

func TestDecideFallsBackWithoutCheckpointAndAppliesFrozenCommand(t *testing.T) {
	ctx := context.Background()
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	specs := &memorySpecRevisions{value: spec.FinalVideoSpec{ID: "spec-1", SessionID: "s1", Version: 1, Status: spec.StatusReviewing}}
	approve, _ := json.Marshal(map[string]any{"spec_id": "spec-1", "spec_version": 1})
	reject, _ := json.Marshal(map[string]any{"spec_id": "spec-1", "spec_version": 1})
	created, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-1", IdempotencyKey: "approval-create-1", SessionID: "s1", ArtifactType: "creation_spec_revision",
		Binding:    approval.VersionBinding{ArtifactID: "spec-1", ArtifactVersion: 1},
		ReviewMode: approval.ReviewModeInterrupt, ExecutionMode: approval.ExecutionModeInterrupt,
		ApproveCommand: approval.FrozenCommand{Kind: "ActivateCreationSpecRevision", IdempotencyKey: "approve-1", Payload: approve},
		RejectCommand:  approval.FrozenCommand{Kind: "RejectCreationSpecRevision", IdempotencyKey: "reject-1", Payload: reject},
	})
	if err != nil || !created.Created {
		t.Fatalf("create=%+v err=%v", created, err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Specs: specs})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Decide(ctx, DecideRequest{ApprovalID: "approval-1", IdempotencyKey: "decision-1", Decision: approval.DecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || specs.value.Status != spec.StatusConfirmed {
		t.Fatalf("result=%+v spec=%+v", result, specs.value)
	}
	replayed, err := service.Decide(ctx, DecideRequest{ApprovalID: "approval-1", IdempotencyKey: "decision-1", Decision: approval.DecisionApprove})
	if err != nil || !replayed.Applied || replayed.Decision.Created || specs.decideCalls != 1 {
		t.Fatalf("replayed=%+v calls=%d err=%v", replayed, specs.decideCalls, err)
	}
	stored, _ := approvals.Get(ctx, "approval-1")
	if stored.ReviewMode != approval.ReviewModeInterrupt || stored.ExecutionMode != approval.ExecutionModeDurableFallback || stored.Status != approval.StatusApproved {
		t.Fatalf("approval=%+v", stored)
	}
	continuation, err := continuations.GetContinuation(ctx, "approval-1", 1)
	if err != nil || continuation.Status != sessionruntime.ContinuationStatusApplied {
		t.Fatalf("continuation=%+v err=%v", continuation, err)
	}
}

func TestStaleStoryboardApprovalClearsPendingRevision(t *testing.T) {
	ctx := context.Background()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := commands.Create(ctx, "board-1", "s1")
	candidate := storyboard.StoryboardRevision{ID: "revision-1", DerivedFromSpecVersion: 1, Modules: []storyboard.StoryboardModule{{ID: "module-1", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1, Elements: []storyboard.StoryboardElement{{ID: "scene-1", Key: "scene-1", SemanticType: "scene", Title: "开场", Revision: 1}}}}}
	aggregate, _, err := commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	payload, _ := json.Marshal(map[string]any{"storyboard_id": aggregate.ID, "revision_id": aggregate.PendingRevisionID})
	created, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-board", IdempotencyKey: "approval-board-create", SessionID: "s1", ArtifactType: "storyboard_revision",
		Binding:    approval.VersionBinding{ArtifactID: aggregate.PendingRevisionID, ArtifactVersion: 1, StoryboardID: aggregate.ID, StoryboardVersion: aggregate.Version},
		ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
		ApproveCommand: approval.FrozenCommand{Kind: "PromoteStoryboardRevision", IdempotencyKey: "approve-board", Payload: payload},
		RejectCommand:  approval.FrozenCommand{Kind: "RejectAndArchivePendingRevision", IdempotencyKey: "reject-board", Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Approvals: approvals, Continuations: continuations, Inputs: runtimeInputs(continuations), Specs: &memorySpecRevisions{value: spec.FinalVideoSpec{Version: 2, Status: spec.StatusConfirmed}}, Storyboards: repository, StoryboardCommands: commands})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Decide(ctx, DecideRequest{ApprovalID: created.Approval.ID, ExpectedDecisionVersion: 0, IdempotencyKey: "stale-board-decision", Decision: approval.DecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Approval.Status != approval.StatusStale {
		t.Fatalf("approval status = %s", result.Decision.Approval.Status)
	}
	updated, err := repository.GetAggregate(ctx, aggregate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PendingRevisionID != "" {
		t.Fatalf("pending revision was not cleaned up: %+v", updated)
	}
}
