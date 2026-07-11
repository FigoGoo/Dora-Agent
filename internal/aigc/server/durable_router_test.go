package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type captureRuntime struct {
	input     sessionruntime.SessionInput
	sessionID string
	ensured   bool
}

func (r *captureRuntime) Enqueue(_ context.Context, sessionID string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	r.sessionID, r.input = sessionID, input
	return sessionruntime.EnqueueResult{Enqueued: true}, nil
}
func (r *captureRuntime) EnsureSession(_ context.Context, sessionID string) (bool, error) {
	r.ensured = true
	return true, nil
}
func (r *captureRuntime) Wake(string) {}

func TestCreateMessageUsesDurableRuntimeWhenConfigured(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	runtime := &captureRuntime{}
	router := NewRouter(Config{Store: store, Runtime: runtime, NewID: func() string { return "message-1" }})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", bytes.NewBufferString(`{"content":"创建短剧"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	input, ok := runtime.input.(sessionruntime.UserMessage)
	if !ok || input.MessageID != "message-1" || !runtime.ensured {
		t.Fatalf("runtime=%+v input=%+v", runtime, runtime.input)
	}
	if len(store.messages["s1"]) != 1 || store.messages["s1"][0].ID != "message-1" {
		t.Fatalf("messages=%+v", store.messages["s1"])
	}
}

func TestCandidateApprovalBatchEndpointFailsClosedWithoutPendingCandidates(t *testing.T) {
	ctx := context.Background()
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	repository := storyboard.NewMemoryAggregateRepository()
	commands, err := storyboard.NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := commands.Create(ctx, "board-1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{
		CommandID: "plan-empty-batch", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version,
		Candidate: storyboard.StoryboardRevision{ID: "revision-1", Modules: []storyboard.StoryboardModule{{
			ID: "module", Key: "shots", SemanticType: "shot", Title: "Shots", PlannedCount: 1,
			Elements: []storyboard.StoryboardElement{{ID: "target", Key: "target", SemanticType: "shot", Title: "Target", Revision: 1}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvals := approval.NewMemoryStore()
	continuations := sessionruntime.NewMemoryStore()
	runtime := &captureRuntime{}
	approvalService, err := approvalruntime.New(approvalruntime.Config{
		Approvals: approvals, Continuations: continuations, Inputs: runtime,
		Storyboards: repository, StoryboardCommands: commands,
		GenerationJobs: &fakeGenerationJobStore{bySession: map[string][]generation.GenerationJob{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Config{Store: store, DynamicStoryboards: repository, StoryboardCommands: commands, Approvals: approvals, ApprovalRuntime: approvalService})
	recorder := httptest.NewRecorder()
	body := fmt.Sprintf(`{"decision":"approved","expected_storyboard_version":%d,"idempotency_key":"confirm-empty"}`, aggregate.Version)
	request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/storyboards/board-1/candidate-approvals/decision", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !bytes.Contains(recorder.Body.Bytes(), []byte("no pending candidate approvals")) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCandidateApprovalBatchStoryboardEventIDFitsPostgresLimit(t *testing.T) {
	batchID := strings.Repeat("candidate-batch:", 16)
	storyboardID := strings.Repeat("storyboard:", 16)
	first := candidateApprovalBatchStoryboardEventID(batchID, storyboardID, 19)
	if len(first) > events.MaxEventIDLength {
		t.Fatalf("event id length=%d id=%q", len(first), first)
	}
	if replay := candidateApprovalBatchStoryboardEventID(batchID, storyboardID, 19); replay != first {
		t.Fatalf("stable event id changed: first=%q replay=%q", first, replay)
	}
	if changed := candidateApprovalBatchStoryboardEventID(batchID, storyboardID, 20); changed == first {
		t.Fatal("different storyboard version collided")
	}

	broker := &fakeEventSubscriber{}
	cfg := Config{Events: broker, Now: time.Now}
	err := cfg.publishCandidateApprovalBatch(context.Background(), approvalruntime.CandidateBatchApproveResult{
		Batch:      approval.CandidateApprovalBatch{ID: batchID, SessionID: "session-1"},
		Storyboard: storyboard.StoryboardAggregate{ID: storyboardID, SessionID: "session-1", Version: 19},
	})
	if err != nil || len(broker.published) != 1 || broker.published[0].ID != first {
		t.Fatalf("published=%#v err=%v", broker.published, err)
	}
}

func TestResumeAgentUsesDurableRuntimeWithoutClaimingCheckpointInHTTP(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	runtime := &captureRuntime{}
	checkpoints := &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-1", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1",
		MappingEpoch: 1, Status: session.CheckpointStatusResuming,
	}}
	invoker := &fakeAgentInvoker{}
	router := NewRouter(Config{Store: store, Runtime: runtime, Checkpoints: checkpoints, Invoker: invoker, Events: &fakeEventSubscriber{}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", bytes.NewBufferString(`{"checkpoint_id":"checkpoint-1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	input, ok := runtime.input.(sessionruntime.ResumeRequested)
	if !ok || input.MappingID != "mapping-1" || input.MappingEpoch != 1 || input.CheckpointID != "checkpoint-1" || input.InterruptID != "interrupt-1" {
		t.Fatalf("runtime input=%#v", runtime.input)
	}
	if checkpoints.record.Status != session.CheckpointStatusResuming || invoker.resumeCalls != 0 {
		t.Fatalf("checkpoint=%+v resume_calls=%d", checkpoints.record, invoker.resumeCalls)
	}
}

func TestResumeAgentReenqueuesResumeAppliedUntilDurableProjectionCompletes(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	runtime := &captureRuntime{}
	checkpoints := &fakeTransitionCheckpointStore{fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-applied", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1",
		MappingEpoch: 1, Status: session.CheckpointStatusResumeApplied,
	}}}
	invoker := &fakeAgentInvoker{}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{Store: store, Runtime: runtime, Checkpoints: checkpoints, Invoker: invoker, Events: broker})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", bytes.NewBufferString(`{"checkpoint_id":"checkpoint-1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, ok := runtime.input.(sessionruntime.ResumeRequested); !ok || !runtime.ensured {
		t.Fatalf("runtime=%+v input=%#v", runtime, runtime.input)
	}
	if checkpoints.record.Status != session.CheckpointStatusResumeApplied || invoker.resumeCalls != 0 || eventOfType(broker.published, a2ui.EventInterruptResolved) != nil {
		t.Fatalf("checkpoint=%+v resume_calls=%d events=%#v", checkpoints.record, invoker.resumeCalls, broker.published)
	}
}

func TestResumeAgentRejectsApprovalBoundCheckpointBeforeReceiptShortcuts(t *testing.T) {
	for _, status := range []string{session.CheckpointStatusResumeApplied, session.CheckpointStatusResumed} {
		t.Run(status, func(t *testing.T) {
			store := newFakeSessionStore()
			store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
			runtime := &captureRuntime{}
			checkpoints := &fakeCheckpointStore{record: session.CheckpointMapping{
				ID: "mapping-approval", SessionID: "s1", Scope: session.CheckpointScopeRunner,
				RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1", ApprovalID: "approval-1",
				MappingEpoch: 1, Status: status,
			}}
			invoker := &fakeAgentInvoker{}
			broker := &fakeEventSubscriber{}
			router := NewRouter(Config{Store: store, Runtime: runtime, Checkpoints: checkpoints, Invoker: invoker, Events: broker})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", bytes.NewBufferString(`{"checkpoint_id":"checkpoint-1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusConflict || !bytes.Contains(recorder.Body.Bytes(), []byte("approval-bound")) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if runtime.input != nil || invoker.resumeCalls != 0 || checkpoints.resumedID != "" || len(broker.published) != 0 {
				t.Fatalf("runtime=%#v calls=%d resumed=%q events=%#v", runtime.input, invoker.resumeCalls, checkpoints.resumedID, broker.published)
			}
		})
	}
}

func TestReplayAndStreamDurableSessionEvents(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	eventStore := events.NewMemoryStore()
	payload, _ := events.MarshalPayload(a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{{Type: a2ui.ActionUpdateCard, Surface: "storyboard"}}})
	_, err := eventStore.AppendSessionEventOnce(context.Background(), events.SessionEvent{SessionID: "s1", EventID: "event-1", EventType: a2ui.EventAction, ProducerKind: events.ProducerDomainProjector, SourceKey: "domain-1", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Config{Store: store, EventLog: eventStore})
	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/events?after_seq=0", nil))
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var body struct {
		Events  []a2ui.SSEEvent `json:"events"`
		NextSeq int64           `json:"next_seq"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.NextSeq != 1 {
		t.Fatalf("replay=%+v", body)
	}
	stream := httptest.NewRecorder()
	router.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/events/stream?once=1", nil))
	if !bytes.Contains(stream.Body.Bytes(), []byte("id: event-1")) || !bytes.Contains(stream.Body.Bytes(), []byte("event: a2ui.action")) {
		t.Fatalf("stream=%s", stream.Body.String())
	}
}

func TestUpdatePromptEditsPendingDynamicRevision(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	repository := storyboard.NewMemoryAggregateRepository()
	commands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := commands.Create(context.Background(), "board-1", "s1")
	candidate := storyboard.StoryboardRevision{ID: "revision-1", Modules: []storyboard.StoryboardModule{{ID: "module-1", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1, Elements: []storyboard.StoryboardElement{{ID: "scene-1", Key: "scene-1", SemanticType: "scene", Title: "开场", Revision: 1, PromptSlots: []storyboard.PromptSlot{{Purpose: "keyframe", Prompt: "old", Revision: 1, Status: storyboard.PromptStatusReady}}, AssetSlots: []storyboard.AssetSlot{{Key: "keyframe", MediaKind: "image", Required: true}}}}}}}
	aggregate, _, err := commands.CreatePending(context.Background(), storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Config{Store: store, DynamicStoryboards: repository, StoryboardCommands: commands, NewID: func() string { return "prompt-command" }})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/aigc/sessions/s1/storyboards/board-1/targets/scene_1/prompt", bytes.NewBufferString(`{"expected_version":1,"target_revision":1,"prompt_revision":1,"purpose":"keyframe","prompt":"user prompt"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated, _ := repository.GetAggregate(context.Background(), "board-1")
	pending, _ := updated.PendingRevision()
	if prompt := pending.Modules[0].Elements[0].PromptSlots[0]; prompt.Prompt != "user prompt" || !prompt.LockedByUser {
		t.Fatalf("prompt=%+v", prompt)
	}
	retry := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(http.MethodPatch, "/api/aigc/sessions/s1/storyboards/board-1/targets/scene_1/prompt", bytes.NewBufferString(`{"expected_version":1,"target_revision":1,"prompt_revision":1,"purpose":"keyframe","prompt":"user prompt"}`))
	retryRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(retry, retryRequest)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	replayed, _ := repository.GetAggregate(context.Background(), "board-1")
	replayedPending, _ := replayed.PendingRevision()
	if replayed.Version != updated.Version || replayedPending.Modules[0].Elements[0].PromptSlots[0].Revision != 2 {
		t.Fatalf("prompt retry was not idempotent: version=%d prompt=%+v", replayed.Version, replayedPending.Modules[0].Elements[0].PromptSlots[0])
	}
}

func TestRegenerationDispatchSnapshotRecoversFrozenInputAfterStoryboardChanges(t *testing.T) {
	ctx := context.Background()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := commands.Create(ctx, "board-regeneration", "session-regeneration")
	candidate := storyboard.StoryboardRevision{ID: "revision-regeneration", DerivedFromSpecVersion: 3, Modules: []storyboard.StoryboardModule{{
		ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Elements: []storyboard.StoryboardElement{{ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "frozen prompt", Revision: 1, Status: storyboard.PromptStatusReady}},
			AssetSlots:  []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, _ = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	aggregate, _, _ = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	preview := aggregate.Clone()
	_, _ = preview.RegenerateAsset(storyboard.RegenerateAssetCommand{CommandID: "preview", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "scene", AssetSlot: "image"})
	input, err := preview.ResolveGenerationInput("scene", "image")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := storyboard.RegenerationDispatchSnapshot{Provider: generation.ProviderImage2, MediaKind: "image", UserID: "user-1", SpecVersion: 3, StoryboardVersion: aggregate.Version + 1, EstimatedPoints: 12, Input: input, Payload: map[string]any{"prompt": input.Prompt}}
	aggregate, _, err = commands.Regenerate(ctx, storyboard.RegenerateAssetCommand{CommandID: "request-1:epoch", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "scene", AssetSlot: "image", DispatchSnapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a later user edit while the process was down before workflow
	// creation. Recovery must not rebuild the provider request from this prompt.
	aggregate, _, err = commands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{CommandID: "later-edit", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "scene", Purpose: "image", ExpectedRevision: 1, Prompt: "new prompt", LockedByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	frozen, found, err := loadRegenerationDispatchSnapshot(ctx, repository, aggregate.ID, "request-1:epoch")
	if err != nil || !found || frozen.Input.Prompt != "frozen prompt" {
		t.Fatalf("snapshot=%+v found=%t err=%v", frozen, found, err)
	}
	workflowStore := generation.NewMemoryStore()
	workflowCommands := generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore})
	id := 0
	cfg := Config{GenerationCommands: workflowCommands, NewID: func() string { id++; return fmt.Sprintf("recovery-%d", id) }}
	workflow, err := cfg.createTargetRegenerationWorkflow(ctx, aggregate.SessionID, aggregate.ID, "request-1", frozen)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) != 1 || workflow.Jobs[0].Payload["prompt"] != "frozen prompt" || workflow.Jobs[0].BindingToken.InputFingerprint != frozen.Input.Fingerprint || workflow.Jobs[0].StoryboardVersionAtDispatch != frozen.StoryboardVersion {
		t.Fatalf("recovered workflow did not use frozen input: %+v", workflow.Jobs)
	}
}

func TestRegenerateTargetNormalizesAndFencesOptionalMediaKind(t *testing.T) {
	ctx := context.Background()
	sessions := newFakeSessionStore()
	sessions.sessions["session-media-kind"] = session.SessionRecord{ID: "session-media-kind", UserID: "user-1", Status: "active"}
	repository := storyboard.NewMemoryAggregateRepository()
	storyboardCommands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := storyboardCommands.Create(ctx, "board-media-kind", "session-media-kind")
	candidate := storyboard.StoryboardRevision{ID: "revision-media-kind", Modules: []storyboard.StoryboardModule{{
		ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Elements: []storyboard.StoryboardElement{{ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "frozen prompt", Revision: 1, Status: storyboard.PromptStatusReady}},
			AssetSlots:  []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, _ = storyboardCommands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	aggregate, _, _ = storyboardCommands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	workflowStore := generation.NewMemoryStore()
	workflowCommands := generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore})
	router := NewRouter(Config{
		Store: sessions, DynamicStoryboards: repository, StoryboardCommands: storyboardCommands,
		GenerationWorkflow: workflowStore, GenerationCommands: workflowCommands,
	})
	body := fmt.Sprintf(`{"expected_version":%d,"target_revision":1,"asset_slot":"image","media_kind":" Image ","idempotency_key":"regenerate-media-kind"}`, aggregate.Version)

	for attempt := 1; attempt <= 2; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/session-media-kind/storyboards/board-media-kind/targets/scene/regenerate", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("attempt %d status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
	}

	updated, _ := repository.GetAggregate(ctx, aggregate.ID)
	invalid := httptest.NewRecorder()
	invalidBody := fmt.Sprintf(`{"expected_version":%d,"target_revision":1,"asset_slot":"image","media_kind":"video","idempotency_key":"regenerate-wrong-media-kind"}`, updated.Version)
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/session-media-kind/storyboards/board-media-kind/targets/scene/regenerate", bytes.NewBufferString(invalidBody))
	invalidRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusConflict || !bytes.Contains(invalid.Body.Bytes(), []byte("media_kind does not match")) {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}
