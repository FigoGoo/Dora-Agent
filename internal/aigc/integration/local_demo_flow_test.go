package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capabilityruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation/handlers"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generationruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/server"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

// TestLocalDemoFlowFromCapabilityThroughWorkerToA2UI is the executable local
// acceptance contract. It deliberately uses deterministic placeholder media:
// the contract under test is orchestration, persistence, approval boundaries,
// finalization, durable A2UI projection, and locally accessible artifacts—not
// real synthesis or transcoding.
func TestLocalDemoFlowFromCapabilityThroughWorkerToA2UI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const sessionID = "local-demo-session"
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	assetRoot := t.TempDir()
	assetServer := httptest.NewServer(http.FileServer(http.Dir(assetRoot)))
	t.Cleanup(assetServer.Close)
	uploader, err := asset.NewLocalUploader(assetRoot, assetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	assets := newMemoryAssetStore()
	pendingAssets := generationruntime.PendingAssetStore{AssetStore: assets}

	specs := spec.NewMemoryStore(spec.WithMemoryClock(func() time.Time { return now }))
	createdSpec, err := specs.Save(ctx, spec.FinalVideoSpec{
		ID: "spec-local-demo", SessionID: sessionID, IdempotencyKey: "spec-local-demo-v1",
		Status: spec.StatusReviewing, Title: "本地全流程演示", VideoType: "short_video",
		DurationSeconds: 8, AspectRatio: "16:9", Fields: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmedSpec, err := specs.DecideRevision(ctx, createdSpec.ID, createdSpec.Version, true)
	if err != nil {
		t.Fatal(err)
	}

	storyboards := storyboard.NewMemoryAggregateRepository()
	storyboardCommands, err := storyboard.NewCommandService(storyboards)
	if err != nil {
		t.Fatal(err)
	}
	board := seedLocalDemoStoryboard(t, ctx, storyboardCommands, sessionID, confirmedSpec.Version)

	workflowStore := generation.NewMemoryStore(generation.WithMemoryClock(func() time.Time { return now }))
	queue := newMemoryJobQueue()
	approvalStore := approval.NewMemoryStore()
	continuationStore := sessionruntime.NewMemoryStoreWithClock(func() time.Time { return now })
	runtimeInputs := &memoryRuntimeInputs{store: continuationStore}
	approvalService, err := approvalruntime.New(approvalruntime.Config{
		Approvals: approvalStore, Continuations: continuationStore, Inputs: runtimeInputs,
		Specs: specs, Storyboards: storyboards, StoryboardCommands: storyboardCommands,
		OwnerID: "local-demo-approval", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	eventStore := events.NewMemoryStoreWithClock(func() time.Time { return now })
	durableEvents := server.NewDurableEventBroker(eventStore, a2ui.NewMemoryBroker(128), events.NewMemoryNotificationHub())
	durableEvents.Now = func() time.Time { return now }

	bindingAdapter := generationruntime.StoryboardBindingAdapter{
		Repository: storyboards, Commands: storyboardCommands, Assets: assets,
		Approvals: approvalStore, Specs: specs, Events: durableEvents,
	}
	barrier := generation.NewBatchBarrier(workflowStore, func() time.Time { return now })
	finalizer := generation.NewFinalizationEngine(generation.FinalizationEngineConfig{
		Store: workflowStore, Bindings: bindingAdapter, Committer: bindingAdapter,
		Inspector: bindingAdapter, Discarder: bindingAdapter, Barrier: barrier,
		Clock: func() time.Time { return now },
	})
	worker := generation.NewLifecycleWorker(generation.LifecycleWorkerConfig{
		Store: workflowStore, Queue: queue, Finalizer: finalizer, Barrier: barrier,
		ResultRecovery: pendingAssets, Clock: func() time.Time { return now }, WorkerID: "local-demo-worker",
		Providers: map[string]generation.ProviderAdapter{
			generation.ProviderImage2:   generation.NewJobHandlerProviderAdapter(handlers.NewDemoVisualJobHandler(handlers.DemoVisualJobHandlerConfig{Assets: pendingAssets, Uploader: uploader, Now: func() time.Time { return now }})),
			generation.ProviderSeedance: generation.NewJobHandlerProviderAdapter(handlers.NewDemoVisualJobHandler(handlers.DemoVisualJobHandlerConfig{Assets: pendingAssets, Uploader: uploader, Now: func() time.Time { return now }})),
			generation.ProviderAudio:    generation.NewJobHandlerProviderAdapter(handlers.NewDemoAudioJobHandler(handlers.DemoAudioJobHandlerConfig{Assets: pendingAssets, Uploader: uploader, Now: func() time.Time { return now }})),
			generation.ProviderAssembly: generation.NewJobHandlerProviderAdapter(handlers.NewDemoAssemblyJobHandler(handlers.DemoAssemblyJobHandlerConfig{Assets: pendingAssets, Uploader: uploader, Now: func() time.Time { return now }})),
		},
	})
	signalPublisher := generationruntime.SessionSignalPublisher{
		Store: workflowStore, Events: durableEvents, Barrier: barrier,
		Finalized: bindingAdapter, Now: func() time.Time { return now },
	}
	dispatcher := generation.NewOutboxDispatcher(workflowStore, generation.QueueOutboxPublisher{Queue: queue, Next: signalPublisher}, func() time.Time { return now })

	ids := &sequentialID{}
	capabilities, err := capabilityruntime.New(capabilityruntime.Config{
		Artifacts: artifact.NewMemoryStore(), Specs: specs,
		Storyboards: storyboards, StoryboardCommands: storyboardCommands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore, NewID: ids.Next, Clock: func() time.Time { return now }}),
		GenerationJobs:     workflowStore, GenerationWorkflow: workflowStore,
		NewID: ids.Next, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	handlersSet := capabilities.Handlers()
	imageResult, err := handlersSet.GenerateMedia(ctx, capability.Request[capability.GenerateMediaIntent]{
		Command: capability.CommandContext{
			SessionID: sessionID, UserID: "local-user", RequestID: "request-image",
			IdempotencyKey: "local-image", WorkflowID: "workflow-local", StageRunID: "stage-image", ToolCallID: "call-image",
		},
		Intent: capability.GenerateMediaIntent{Phase: "element_images", Policy: "all_eligible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedCapability(t, imageResult.Status, imageResult.OperationID, imageResult.BatchID, imageResult.Data.JobCount)
	runOneLocalWorkflow(t, ctx, dispatcher, worker, workflowStore, imageResult.BatchID)

	videoResult, err := handlersSet.GenerateMedia(ctx, capability.Request[capability.GenerateMediaIntent]{
		Command: capability.CommandContext{
			SessionID: sessionID, UserID: "local-user", RequestID: "request-video",
			IdempotencyKey: "local-video", WorkflowID: "workflow-local", StageRunID: "stage-video", ToolCallID: "call-video",
		},
		Intent: capability.GenerateMediaIntent{Phase: "videos", Policy: "all_eligible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedCapability(t, videoResult.Status, videoResult.OperationID, videoResult.BatchID, videoResult.Data.JobCount)
	runOneLocalWorkflow(t, ctx, dispatcher, worker, workflowStore, videoResult.BatchID)

	audioResult, err := handlersSet.GenerateMedia(ctx, capability.Request[capability.GenerateMediaIntent]{
		Command: capability.CommandContext{
			SessionID: sessionID, UserID: "local-user", RequestID: "request-audio",
			IdempotencyKey: "local-audio", WorkflowID: "workflow-local", StageRunID: "stage-audio", ToolCallID: "call-audio",
		},
		Intent: capability.GenerateMediaIntent{Phase: "audio", Policy: "all_eligible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedCapability(t, audioResult.Status, audioResult.OperationID, audioResult.BatchID, audioResult.Data.JobCount)
	runOneLocalWorkflow(t, ctx, dispatcher, worker, workflowStore, audioResult.BatchID)

	// Resolve the three durable review cards through the real approval service.
	// Its deterministic command activates each binding and enqueues a stable
	// ApprovalContinuationResult; the outer Agent turn itself is tested elsewhere.
	board = approveAllCandidates(t, ctx, storyboards, approvalStore, approvalService, continuationStore, runtimeInputs, board.ID)
	assertRequiredSlotsActive(t, board)

	assemblyResult, err := handlersSet.AssembleOutput(ctx, capability.Request[capability.AssembleOutputIntent]{
		Command: capability.CommandContext{
			SessionID: sessionID, UserID: "local-user", RequestID: "request-assembly",
			IdempotencyKey: "local-assembly", WorkflowID: "workflow-local", StageRunID: "stage-assembly", ToolCallID: "call-assembly",
		},
		Intent: capability.AssembleOutputIntent{Mode: "preview", OutputType: "local-demo-json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedCapability(t, assemblyResult.Status, assemblyResult.OperationID, assemblyResult.BatchID, 1)
	runOneLocalWorkflow(t, ctx, dispatcher, worker, workflowStore, assemblyResult.BatchID)

	imageAsset := onlyWorkflowAsset(t, ctx, workflowStore, assets, imageResult.BatchID)
	videoAsset := onlyWorkflowAsset(t, ctx, workflowStore, assets, videoResult.BatchID)
	audioAsset := onlyWorkflowAsset(t, ctx, workflowStore, assets, audioResult.BatchID)
	assemblyAsset := onlyWorkflowAsset(t, ctx, workflowStore, assets, assemblyResult.BatchID)
	assertLocalAsset(t, assetRoot, imageAsset, "image/png")
	assertLocalAsset(t, assetRoot, videoAsset, "video/mp4")
	assertLocalAsset(t, assetRoot, audioAsset, "audio/wav")
	assemblyRaw := assertLocalAsset(t, assetRoot, assemblyAsset, "application/json")
	assertAssemblyManifest(t, assemblyRaw, assemblyResult.OperationID, board.ID)

	rows, err := eventStore.Tail(ctx, sessionID, events.TailOptions{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	assertDurableA2UIFlow(t, rows)
}

func seedLocalDemoStoryboard(t *testing.T, ctx context.Context, commands *storyboard.CommandService, sessionID string, specVersion int) storyboard.StoryboardAggregate {
	t.Helper()
	aggregate, err := commands.Create(ctx, "storyboard-local-demo", sessionID)
	if err != nil {
		t.Fatal(err)
	}
	candidate := storyboard.StoryboardRevision{
		ID: "storyboard-local-demo-v1", StoryboardID: aggregate.ID,
		DerivedFromSpecVersion: specVersion, Scenario: "local_demo",
		Modules: []storyboard.StoryboardModule{
			{
				ID: "visuals", Key: "visuals", SemanticType: "scene", Title: "画面", Order: 1,
				PlannedCount: 1, Required: true, Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, OutputModality: "image"},
				Elements: []storyboard.StoryboardElement{{
					ID: "scene-1", Key: "scene-1", SemanticType: "scene", Title: "本地演示画面", Revision: 1,
					Content:     map[string]any{"description": "本地占位画面"},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "keyframe", Prompt: "deterministic local demo image", Revision: 1, Status: storyboard.PromptStatusReady, LockedByUser: true}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "keyframe", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
			{
				ID: "videos", Key: "videos", SemanticType: "shot", Title: "视频", Order: 2,
				PlannedCount: 1, Required: true, Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, OutputModality: "video"},
				Elements: []storyboard.StoryboardElement{{
					ID: "shot-1", Key: "shot-1", SemanticType: "shot", Title: "本地演示视频", Revision: 1,
					Content:     map[string]any{"description": "本地占位视频"},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "clip", Prompt: "deterministic local demo video", Revision: 1, Status: storyboard.PromptStatusReady, LockedByUser: true}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "clip", MediaKind: "video", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
			{
				ID: "audio", Key: "audio", SemanticType: "audio_layer", Title: "音频", Order: 3,
				PlannedCount: 1, Required: true, Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, OutputModality: "audio"},
				Elements: []storyboard.StoryboardElement{{
					ID: "audio-1", Key: "audio-1", SemanticType: "audio_layer", Title: "本地演示音频", Revision: 1,
					Content:     map[string]any{"description": "本地占位音频"},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "track", Prompt: "deterministic local demo tone", Revision: 1, Status: storyboard.PromptStatusReady, LockedByUser: true}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "track", MediaKind: "audio", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
		},
	}
	aggregate, _, err = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{
		CommandID: "plan-local-demo", IdempotencyKey: "plan-local-demo", StoryboardID: aggregate.ID,
		BaseVersion: aggregate.Version, Actor: "local-user", Source: "local-test", Candidate: candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{
		CommandID: "approve-plan-local-demo", IdempotencyKey: "approve-plan-local-demo",
		StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID,
		Decision: "approve", Actor: "local-user", Source: "local-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func runOneLocalWorkflow(t *testing.T, ctx context.Context, dispatcher *generation.OutboxDispatcher, worker *generation.LifecycleWorker, store generation.WorkflowStore, batchID string) {
	t.Helper()
	dispatchUntilIdle(t, ctx, dispatcher)
	worked, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run local worker for %s: %v", batchID, err)
	}
	if !worked {
		t.Fatalf("local worker had no queued job for %s", batchID)
	}
	dispatchUntilIdle(t, ctx, dispatcher)
	batch, err := store.GetBatch(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != generation.BatchStatusCompleted || batch.TerminalAt == nil {
		t.Fatalf("batch %s did not complete: %+v", batchID, batch)
	}
	operation, err := store.GetOperation(ctx, batch.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != generation.OperationStatusCompleted || operation.TerminalAt == nil {
		t.Fatalf("operation %s did not complete: %+v", operation.ID, operation)
	}
}

func dispatchUntilIdle(t *testing.T, ctx context.Context, dispatcher *generation.OutboxDispatcher) {
	t.Helper()
	for attempt := 0; attempt < 32; attempt++ {
		published, err := dispatcher.DispatchPending(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if published == 0 {
			return
		}
	}
	t.Fatal("generation outbox did not become idle")
}

func approveAllCandidates(t *testing.T, ctx context.Context, repository storyboard.AggregateRepository, approvals approval.Store, service *approvalruntime.Service, continuations *sessionruntime.MemoryStore, inputs *memoryRuntimeInputs, storyboardID string) storyboard.StoryboardAggregate {
	t.Helper()
	aggregate, err := repository.GetAggregate(ctx, storyboardID)
	if err != nil {
		t.Fatal(err)
	}
	candidates := make([]string, 0)
	for _, binding := range aggregate.Bindings {
		if binding.State == storyboard.BindingStateCandidate {
			candidates = append(candidates, binding.ID)
			if _, err := approvals.Get(ctx, "approval:"+binding.ID); err != nil {
				t.Fatalf("candidate %s has no durable approval: %v", binding.ID, err)
			}
		}
	}
	if len(candidates) != 3 {
		t.Fatalf("candidate bindings = %v, want image, video, and audio", candidates)
	}
	for index, bindingID := range candidates {
		approvalID := "approval:" + bindingID
		decision, decideErr := service.Decide(ctx, approvalruntime.DecideRequest{
			ApprovalID: approvalID, ExpectedDecisionVersion: 0,
			IdempotencyKey: fmt.Sprintf("local-approval-decision-%d", index+1),
			Decision:       approval.DecisionApprove, ActorID: "local-user",
		})
		if decideErr != nil || !decision.Applied || decision.Decision.Approval.Status != approval.StatusApproved {
			t.Fatalf("approve candidate %s: result=%+v err=%v", bindingID, decision, decideErr)
		}
		inputID := fmt.Sprintf("approval:%s:continuation-result:1", approvalID)
		stored, inputErr := continuations.GetInput(ctx, inputID)
		if inputErr != nil {
			t.Fatalf("stable approval continuation input %s: %v", inputID, inputErr)
		}
		decoded, decodeErr := sessionruntime.DecodeInput(stored)
		result, ok := decoded.(sessionruntime.ApprovalContinuationResult)
		if decodeErr != nil || !ok || result.ApprovalID != approvalID || result.DecisionVersion != 1 || result.CommandKind != "ActivateArtifactBinding" || result.EffectiveStatus != string(approval.StatusApproved) || !json.Valid(result.CommandResult) {
			t.Fatalf("approval continuation input = %#v err=%v", decoded, decodeErr)
		}
	}
	if inputs.wakes != len(candidates) {
		t.Fatalf("approval continuation wakes = %d, want %d", inputs.wakes, len(candidates))
	}
	aggregate, err = repository.GetAggregate(ctx, storyboardID)
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func assertRequiredSlotsActive(t *testing.T, aggregate storyboard.StoryboardAggregate) {
	t.Helper()
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				if !slot.Required {
					continue
				}
				if slot.Status != storyboard.AssetSlotStatusActive || slot.ActiveBindingID == "" {
					t.Fatalf("required slot %s:%s is not active: %+v", element.ID, slot.Key, slot)
				}
				active++
			}
		}
	}
	if active != 3 {
		t.Fatalf("active required slots = %d, want 3", active)
	}
}

func onlyWorkflowAsset(t *testing.T, ctx context.Context, workflows generation.WorkflowStore, assets *memoryAssetStore, batchID string) asset.Asset {
	t.Helper()
	jobs, err := workflows.ListJobsByBatch(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != generation.StatusSucceeded || len(jobs[0].ResultAssetIDs) != 1 {
		t.Fatalf("workflow %s jobs = %+v", batchID, jobs)
	}
	result, err := assets.Get(ctx, jobs[0].ResultAssetIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.Availability != asset.AvailabilityAvailable || result.StorageProvider != asset.StorageProviderLocal {
		t.Fatalf("asset was not finalized for local delivery: %+v", result)
	}
	return result
}

func assertLocalAsset(t *testing.T, assetRoot string, record asset.Asset, mimeType string) []byte {
	t.Helper()
	if record.MIMEType != mimeType || record.ObjectKey == "" || record.URL == "" {
		t.Fatalf("invalid local asset metadata: %+v", record)
	}
	path := filepath.Join(assetRoot, filepath.FromSlash(record.ObjectKey))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("local asset file %s: %v", path, err)
	}
	if info.Size() <= 0 || info.Size() != record.SizeBytes {
		t.Fatalf("local asset size = %d metadata=%d", info.Size(), record.SizeBytes)
	}
	response, err := http.Get(record.URL) // #nosec G107 -- URL belongs to this test's httptest server.
	if err != nil {
		t.Fatalf("GET local asset %s: %v", record.URL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET local asset status = %d", response.StatusCode)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) != record.SizeBytes {
		t.Fatalf("served local asset bytes = %d metadata=%d", len(raw), record.SizeBytes)
	}
	return raw
}

func assertAssemblyManifest(t *testing.T, raw []byte, operationID, storyboardID string) {
	t.Helper()
	var manifest struct {
		SchemaVersion string         `json:"schema_version"`
		Provider      string         `json:"provider"`
		OperationID   string         `json:"operation_id"`
		Payload       map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode local assembly manifest: %v", err)
	}
	if manifest.SchemaVersion != "1.0" || manifest.Provider != generation.ProviderAssembly || manifest.OperationID != operationID {
		t.Fatalf("assembly manifest header = %+v", manifest)
	}
	frozen, ok := manifest.Payload["manifest"].(map[string]any)
	if !ok || frozen["storyboard_id"] != storyboardID || frozen["mode"] != "preview" {
		t.Fatalf("assembly manifest payload = %+v", manifest.Payload)
	}
	bindings, ok := frozen["bindings"].([]any)
	if !ok || len(bindings) != 3 {
		t.Fatalf("assembly manifest active bindings = %#v", frozen["bindings"])
	}
}

func assertDurableA2UIFlow(t *testing.T, rows []events.SessionEvent) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("durable session event log is empty")
	}
	statuses := map[string]bool{}
	surfaces := map[string]bool{}
	approvalCards := 0
	for index, row := range rows {
		if row.Seq != int64(index+1) || row.EventType != a2ui.EventAction {
			t.Fatalf("unexpected durable event row %d: %+v", index, row)
		}
		var envelope a2ui.ActionEnvelope
		if err := json.Unmarshal(row.Payload, &envelope); err != nil {
			t.Fatalf("decode A2UI event %s: %v", row.EventID, err)
		}
		if envelope.Version != a2ui.Version1 || len(envelope.Actions) == 0 {
			t.Fatalf("invalid A2UI envelope for %s: %+v", row.EventID, envelope)
		}
		for _, action := range envelope.Actions {
			surfaces[action.Surface] = true
			if action.Type == a2ui.ActionAppendCard && strings.HasPrefix(action.CardID, "approval:") {
				approvalCards++
			}
			payload, _ := action.Payload.(map[string]any)
			for _, key := range []string{"tool_run", "operation"} {
				view, _ := payload[key].(map[string]any)
				status, _ := view["status"].(string)
				if status != "" {
					statuses[status] = true
				}
				nodes, _ := view["nodes"].([]any)
				for _, rawNode := range nodes {
					node, _ := rawNode.(map[string]any)
					status, _ := node["status"].(string)
					if status != "" {
						statuses[status] = true
					}
				}
			}
		}
	}
	for _, want := range []string{generation.StatusRunning, generation.StatusFinalizing, generation.StatusSucceeded, generation.BatchStatusCompleted} {
		if !statuses[want] {
			t.Fatalf("durable A2UI statuses = %v, missing %q", statuses, want)
		}
	}
	if !surfaces["tool_runs"] || !surfaces["storyboard"] || approvalCards != 0 {
		t.Fatalf("A2UI surfaces=%v approval_cards=%d", surfaces, approvalCards)
	}
}

func assertAcceptedCapability(t *testing.T, status, operationID, batchID string, jobCount int) {
	t.Helper()
	if status != capability.StatusAccepted || operationID == "" || batchID == "" || jobCount != 1 {
		t.Fatalf("capability status=%s operation=%s batch=%s jobs=%d", status, operationID, batchID, jobCount)
	}
}

type sequentialID struct {
	mu    sync.Mutex
	value int
}

func (s *sequentialID) Next() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value++
	return fmt.Sprintf("local-id-%d", s.value)
}

type memoryRuntimeInputs struct {
	store *sessionruntime.MemoryStore
	wakes int
}

func (r *memoryRuntimeInputs) Enqueue(ctx context.Context, sessionID string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	return r.store.EnqueueInput(ctx, sessionID, input)
}

func (r *memoryRuntimeInputs) Wake(string) { r.wakes++ }

type memoryJobQueue struct {
	mu      sync.Mutex
	items   []generation.QueuePayload
	seenKey map[string]struct{}
}

func newMemoryJobQueue() *memoryJobQueue {
	return &memoryJobQueue{seenKey: map[string]struct{}{}}
}

func (q *memoryJobQueue) Enqueue(_ context.Context, payload generation.QueuePayload) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := strings.TrimSpace(payload.IdempotencyKey)
	if key != "" {
		if _, exists := q.seenKey[key]; exists {
			return nil
		}
		q.seenKey[key] = struct{}{}
	}
	q.items = append(q.items, payload)
	return nil
}

func (q *memoryJobQueue) Dequeue(context.Context, time.Duration) (generation.QueuePayload, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return generation.QueuePayload{}, false, nil
	}
	payload := q.items[0]
	q.items = q.items[1:]
	return payload, true, nil
}

type memoryAssetStore struct {
	mu    sync.RWMutex
	items map[string]asset.Asset
}

func newMemoryAssetStore() *memoryAssetStore {
	return &memoryAssetStore{items: map[string]asset.Asset{}}
}

func (s *memoryAssetStore) Save(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record = cloneAsset(record)
	s.items[record.ID] = record
	return cloneAsset(record), nil
}

func (s *memoryAssetStore) Get(_ context.Context, id string) (asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return asset.Asset{}, fmt.Errorf("%w: %s", asset.ErrNotFound, id)
	}
	return cloneAsset(record), nil
}

func (s *memoryAssetStore) ListBySourceJob(_ context.Context, jobID string) ([]asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]asset.Asset, 0)
	for _, record := range s.items {
		if record.SourceJobID == strings.TrimSpace(jobID) {
			result = append(result, cloneAsset(record))
		}
	}
	return result, nil
}

func cloneAsset(record asset.Asset) asset.Asset {
	raw, _ := json.Marshal(record)
	var clone asset.Asset
	_ = json.Unmarshal(raw, &clone)
	return clone
}

var (
	_ generation.DispatchQueue              = (*memoryJobQueue)(nil)
	_ generation.JobQueue                   = (*memoryJobQueue)(nil)
	_ generationruntime.AssetStore          = (*memoryAssetStore)(nil)
	_ generationruntime.SourceJobAssetStore = (*memoryAssetStore)(nil)
)
