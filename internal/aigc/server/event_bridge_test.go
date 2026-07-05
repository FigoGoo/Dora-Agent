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
	if got.Event != a2ui.EventStoryboardPatch || got.CreatedAt != fixedNow() {
		t.Fatalf("event = %#v", got)
	}
	payload, ok := got.Payload.(a2ui.StoryboardPatchPayload)
	if !ok {
		t.Fatalf("payload = %#v", got.Payload)
	}
	if payload.StoryboardID != "storyboard-1" || payload.NextVersion != 8 || payload.Ops[0].Path != "/shots/0/asset_ids" {
		t.Fatalf("payload = %#v", payload)
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

type fakeJobWakeupHandler struct {
	events chan session.JobWakeupEvent
}

func (h *fakeJobWakeupHandler) Wakeup(_ context.Context, event session.JobWakeupEvent) error {
	h.events <- event
	return nil
}
