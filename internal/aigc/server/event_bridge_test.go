package server

import (
	"context"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

func TestGenerationEventPublisherConvertsStoryboardPatch(t *testing.T) {
	broker := a2ui.NewMemoryBroker(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	publisher := GenerationEventPublisher{Broker: broker, Now: fixedNow}
	err := publisher.Publish(ctx, generation.WorkerEvent{
		ID:           "evt-1",
		SessionID:    "s1",
		Event:        generation.EventStoryboardPatch,
		SurfaceID:    "storyboard",
		DataModelKey: "storyboard",
		Payload: generation.StoryboardPatchPayload{
			StoryboardID: "storyboard-1",
			BaseVersion:  7,
			NextVersion:  8,
			Ops:          []patch.JSONPatchOp{{Op: "add", Path: "/shots/0/asset_ids", Value: []string{"asset-1"}}},
			Source:       "worker",
			ToolCallID:   "call-1",
		},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	got := <-events
	if got.Event != a2ui.EventAction || got.CreatedAt != fixedNow() {
		t.Fatalf("event = %#v", got)
	}
	envelope, ok := got.Payload.(a2ui.ActionEnvelope)
	if !ok {
		t.Fatalf("payload = %#v", got.Payload)
	}
	if len(envelope.Actions) != 1 || envelope.Actions[0].Type != a2ui.ActionUpdateCard || envelope.Actions[0].Target.Surface != "storyboard" {
		t.Fatalf("envelope = %#v", envelope)
	}
	payloadEnvelope, ok := envelope.Actions[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("action payload = %#v", envelope.Actions[0].Payload)
	}
	payload, ok := payloadEnvelope["patch"].(a2ui.StoryboardPatchPayload)
	if !ok {
		t.Fatalf("patch payload = %#v", payloadEnvelope)
	}
	if payload.StoryboardID != "storyboard-1" || payload.NextVersion != 8 || payload.Ops[0].Path != "/shots/0/asset_ids" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestGenerationEventPublisherWrapsWorkerEventsAsA2UIDataModelUpdates(t *testing.T) {
	broker := a2ui.NewMemoryBroker(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	publisher := GenerationEventPublisher{Broker: broker, Now: fixedNow}
	if err := publisher.Publish(ctx, generation.WorkerEvent{
		ID:        "evt-job",
		SessionID: "s1",
		Event:     generation.EventJobStatus,
		Payload: generation.JobStatusPayload{
			JobID:      "job-1",
			SessionID:  "s1",
			Provider:   generation.ProviderImage2,
			TargetType: generation.TargetShot,
			TargetID:   "shot-1",
			Status:     generation.StatusRunning,
			StageKey:   "generate_shot_image",
		},
		CreatedAt: fixedNow(),
	}); err != nil {
		t.Fatalf("Publish(job) error = %v", err)
	}

	gotJob := <-events
	if gotJob.Event != a2ui.EventAction {
		t.Fatalf("job event = %#v", gotJob)
	}
	jobEnvelope, ok := gotJob.Payload.(a2ui.ActionEnvelope)
	if !ok {
		t.Fatalf("job payload = %#v", gotJob.Payload)
	}
	if len(jobEnvelope.Actions) != 1 || jobEnvelope.Actions[0].Type != a2ui.ActionUpdateCard || jobEnvelope.Actions[0].Target.CardID != "tool_run:media_generator" {
		t.Fatalf("job envelope = %#v", jobEnvelope)
	}
	jobPayload, ok := jobEnvelope.Actions[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("job action payload = %#v", jobEnvelope.Actions[0].Payload)
	}
	toolRun, ok := jobPayload["tool_run"].(map[string]any)
	if !ok || toolRun["display_name"] != "Media Assets" || toolRun["tool_key"] != "media_generator" || toolRun["status"] != generation.StatusRunning {
		t.Fatalf("tool run = %#v", jobPayload["tool_run"])
	}
	nodes, ok := toolRun["nodes"].([]map[string]any)
	if !ok || len(nodes) != 1 || nodes[0]["display_name"] == "Image2" {
		t.Fatalf("worker provider should be an internal media node, nodes = %#v", toolRun["nodes"])
	}
	if got, _ := nodes[0]["display_name"].(string); got == "" || got == "generate shot image · shot-1" {
		t.Fatalf("node should include provider context, got %q", got)
	}

	if err := publisher.Publish(ctx, generation.WorkerEvent{
		ID:           "evt-patch",
		SessionID:    "s1",
		Event:        generation.EventStoryboardPatch,
		SurfaceID:    "storyboard",
		DataModelKey: "storyboard:s1",
		Payload: generation.StoryboardPatchPayload{
			StoryboardID: "storyboard-1",
			BaseVersion:  7,
			NextVersion:  8,
			Ops:          []patch.JSONPatchOp{{Op: "replace", Path: "/shots/0/status", Value: "ready"}},
			Source:       "worker",
		},
		CreatedAt: fixedNow(),
	}); err != nil {
		t.Fatalf("Publish(patch) error = %v", err)
	}

	gotPatch := <-events
	if gotPatch.Event != a2ui.EventAction {
		t.Fatalf("patch event = %#v", gotPatch)
	}
	patchEnvelope, ok := gotPatch.Payload.(a2ui.ActionEnvelope)
	if !ok {
		t.Fatalf("patch payload = %#v", gotPatch.Payload)
	}
	if len(patchEnvelope.Actions) != 1 || patchEnvelope.Actions[0].Type != a2ui.ActionUpdateCard || patchEnvelope.Actions[0].Target.CardID != "storyboard:s1" {
		t.Fatalf("patch envelope = %#v", patchEnvelope)
	}
	patchPayload, ok := patchEnvelope.Actions[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("patch action payload = %#v", patchEnvelope.Actions[0].Payload)
	}
	if _, ok := patchPayload["patch"].(a2ui.StoryboardPatchPayload); !ok {
		t.Fatalf("missing storyboard patch payload: %#v", patchPayload)
	}
}

func TestGenerationEventPublisherWakesAgentForTerminalJobStatus(t *testing.T) {
	broker := a2ui.NewMemoryBroker(1)
	wakeup := &fakeJobWakeupHandler{events: make(chan session.JobWakeupEvent, 1)}
	publisher := GenerationEventPublisher{Broker: broker, Wakeup: wakeup}

	err := publisher.Publish(context.Background(), generation.WorkerEvent{
		ID:        "evt-1",
		SessionID: "s1",
		Event:     generation.EventJobStatus,
		Payload: generation.JobStatusPayload{
			JobID:          "job-1",
			SessionID:      "s1",
			Status:         generation.StatusSucceeded,
			ResultAssetIDs: []string{"asset-1"},
		},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case got := <-wakeup.events:
		if got.SessionID != "s1" || got.JobID != "job-1" || got.Status != generation.StatusSucceeded || got.AssetIDs[0] != "asset-1" {
			t.Fatalf("wakeup = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for wakeup")
	}
}

func TestGenerationEventPublisherDropsUnknownWorkerEvents(t *testing.T) {
	broker := a2ui.NewMemoryBroker(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	publisher := GenerationEventPublisher{Broker: broker, Now: fixedNow}
	err := publisher.Publish(ctx, generation.WorkerEvent{
		ID:        "evt-legacy",
		SessionID: "s1",
		Event:     "worker.legacy",
		Payload:   map[string]any{"message": "legacy"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case got := <-events:
		t.Fatalf("unexpected event published: %#v", got)
	case <-time.After(20 * time.Millisecond):
	}
}

type fakeJobWakeupHandler struct {
	events chan session.JobWakeupEvent
}

func (h *fakeJobWakeupHandler) Wakeup(_ context.Context, event session.JobWakeupEvent) error {
	h.events <- event
	return nil
}
