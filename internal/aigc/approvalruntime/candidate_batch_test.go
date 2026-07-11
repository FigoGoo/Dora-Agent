package approvalruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type candidateBatchJobSource struct{ jobs []generation.GenerationJob }

func (s candidateBatchJobSource) ListBySession(_ context.Context, sessionID string) ([]generation.GenerationJob, error) {
	result := make([]generation.GenerationJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.SessionID == sessionID {
			result = append(result, job)
		}
	}
	return result, nil
}

type flakyCandidateBatchInputs struct {
	store     *sessionruntime.MemoryStore
	failCalls int
	calls     int
	wakes     int
}

func (i *flakyCandidateBatchInputs) Enqueue(ctx context.Context, sessionID string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	i.calls++
	if i.failCalls > 0 {
		i.failCalls--
		return sessionruntime.EnqueueResult{}, errors.New("temporary enqueue failure")
	}
	return i.store.EnqueueInput(ctx, sessionID, input)
}

func (i *flakyCandidateBatchInputs) Wake(string) { i.wakes++ }

type candidateBatchFixture struct {
	repository    storyboard.AggregateRepository
	commands      *storyboard.CommandService
	approvals     *approval.MemoryStore
	continuations *sessionruntime.MemoryStore
	aggregate     storyboard.StoryboardAggregate
}

func newCandidateBatchFixture(t *testing.T) candidateBatchFixture {
	t.Helper()
	ctx := context.Background()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, err := storyboard.NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := commands.Create(ctx, "board-batch", "session-batch")
	if err != nil {
		t.Fatal(err)
	}
	revision := storyboard.StoryboardRevision{ID: "revision-batch", Modules: []storyboard.StoryboardModule{{
		ID: "module", Key: "shots", SemanticType: "shot", Title: "Shots", PlannedCount: 2,
		Elements: []storyboard.StoryboardElement{
			{ID: "target1", Key: "target1", SemanticType: "shot", Title: "One", Revision: 1, PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "one", Revision: 1, Status: storyboard.PromptStatusReady}}, AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}}},
			{ID: "target2", Key: "target2", SemanticType: "shot", Title: "Two", Revision: 1, PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "two", Revision: 1, Status: storyboard.PromptStatusReady}}, AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}}},
		},
	}}}
	aggregate, _, err = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan-batch", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: revision})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "promote-batch", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: approval.DecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	approvals := approval.NewMemoryStore()
	for index, targetID := range []string{"target1", "target2"} {
		input, resolveErr := aggregate.ResolveGenerationInput(targetID, "image")
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		bindingID := fmt.Sprintf("binding-%d", index+1)
		approvalID := "approval:" + bindingID
		aggregate, _, err = commands.Bind(ctx, storyboard.BindAssetCommand{
			CommandID: fmt.Sprintf("bind-%d", index+1), StoryboardID: aggregate.ID, BaseVersion: aggregate.Version,
			BindingID: bindingID, TargetID: targetID, AssetSlot: "image", AssetID: fmt.Sprintf("asset-%d", index+1),
			ApprovalID: approvalID, TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision,
			GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint,
		})
		if err != nil {
			t.Fatal(err)
		}
		var stored storyboard.ArtifactBinding
		for _, binding := range aggregate.Bindings {
			if binding.ID == bindingID {
				stored = binding
				break
			}
		}
		payload, _ := json.Marshal(map[string]any{"storyboard_id": aggregate.ID, "binding_id": bindingID})
		if _, err := approvals.Create(ctx, approval.Approval{
			ID: approvalID, IdempotencyKey: "candidate:" + bindingID, SessionID: aggregate.SessionID,
			ArtifactType: "candidate_asset",
			Binding:      approval.VersionBinding{ArtifactID: bindingID, ArtifactVersion: stored.ArtifactRevision, StoryboardID: aggregate.ID, StoryboardVersion: aggregate.Version, TargetID: targetID, TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch},
			ReviewMode:   approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
			ApproveCommand: approval.FrozenCommand{Kind: "ActivateArtifactBinding", IdempotencyKey: approvalID + ":activate", Payload: payload},
			RejectCommand:  approval.FrozenCommand{Kind: "RejectArtifactBinding", IdempotencyKey: approvalID + ":reject", Payload: payload},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return candidateBatchFixture{repository: repository, commands: commands, approvals: approvals, continuations: sessionruntime.NewMemoryStore(), aggregate: aggregate}
}

func (f candidateBatchFixture) service(t *testing.T, inputs RuntimeInputEnqueuer, jobs []generation.GenerationJob) *Service {
	t.Helper()
	service, err := New(Config{
		Approvals: f.approvals, Continuations: f.continuations, Inputs: inputs,
		Storyboards: f.repository, StoryboardCommands: f.commands,
		GenerationJobs: candidateBatchJobSource{jobs: jobs}, OwnerID: "candidate-batch-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestApproveCandidateBatchFreezesTargetsAndReplaysWithoutDomainMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newCandidateBatchFixture(t)
	inputs := &flakyCandidateBatchInputs{store: fixture.continuations}
	request := CandidateBatchApproveRequest{
		SessionID: fixture.aggregate.SessionID, StoryboardID: fixture.aggregate.ID,
		ExpectedStoryboardVersion: fixture.aggregate.Version, IdempotencyKey: "confirm-candidates-1",
		Decision: approval.DecisionApprove, ActorID: "user-1",
	}
	running := fixture.service(t, inputs, []generation.GenerationJob{{ID: "job-running", SessionID: fixture.aggregate.SessionID, StoryboardID: fixture.aggregate.ID, Status: generation.StatusRunning}})
	if _, err := running.ApproveCandidateBatch(ctx, request); !errors.Is(err, ErrCandidateBatchGenerationRunning) {
		t.Fatalf("running job error = %v", err)
	}

	service := fixture.service(t, inputs, nil)
	result, err := service.ApproveCandidateBatch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Summary.Complete || !result.Summary.AllApproved || result.Summary.Approved != 2 || len(result.Results) != 2 {
		t.Fatalf("batch result = %+v", result)
	}
	for _, binding := range result.Storyboard.Bindings {
		if binding.ID == "binding-1" || binding.ID == "binding-2" {
			if binding.State != storyboard.BindingStateActive {
				t.Fatalf("binding %s state = %s", binding.ID, binding.State)
			}
		}
	}
	versionAfterFirst := result.Storyboard.Version
	replayed, err := service.ApproveCandidateBatch(ctx, request)
	if err != nil || !replayed.Summary.AllApproved || replayed.Storyboard.Version != versionAfterFirst {
		t.Fatalf("replay result=%+v err=%v", replayed, err)
	}
	changed := request
	changed.Reason = "different payload"
	if _, err := service.ApproveCandidateBatch(ctx, changed); !errors.Is(err, approval.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	noPending := request
	noPending.IdempotencyKey = "confirm-candidates-empty"
	noPending.ExpectedStoryboardVersion = replayed.Storyboard.Version
	if _, err := service.ApproveCandidateBatch(ctx, noPending); !errors.Is(err, ErrNoPendingCandidateApprovals) {
		t.Fatalf("empty batch error = %v", err)
	}
}

func TestApproveCandidateBatchRecoversPartialContinuationFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newCandidateBatchFixture(t)
	inputs := &flakyCandidateBatchInputs{store: fixture.continuations, failCalls: 1}
	service := fixture.service(t, inputs, nil)
	request := CandidateBatchApproveRequest{
		SessionID: fixture.aggregate.SessionID, StoryboardID: fixture.aggregate.ID,
		ExpectedStoryboardVersion: fixture.aggregate.Version, IdempotencyKey: "confirm-candidates-recover",
		Decision: approval.DecisionApprove, ActorID: "user-1",
	}
	partial, err := service.ApproveCandidateBatch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Summary.Complete || partial.Summary.Failed != 1 || partial.Summary.Approved != 1 || !partial.Results[0].Retryable {
		t.Fatalf("partial result = %+v", partial)
	}
	versionAfterPartial := partial.Storyboard.Version
	recovered, err := service.ApproveCandidateBatch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Summary.Complete || !recovered.Summary.AllApproved || recovered.Summary.Approved != 2 || recovered.Storyboard.Version != versionAfterPartial {
		t.Fatalf("recovered result = %+v", recovered)
	}
}
