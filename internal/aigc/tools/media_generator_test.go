package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
)

func TestMediaGeneratorToolReturnsInterruptRequest(t *testing.T) {
	tool := NewMediaGeneratorTool(MediaGeneratorToolConfig{
		Checkpoints: mediagraph.NewMemoryCheckpointStore(),
	})

	args := ToolInvocationEnvelope[MediaGeneratorPayload]{
		SessionID:                 "session-1",
		RequestID:                 "request-1",
		IdempotencyKey:            "idem-1",
		ExpectedSpecVersion:       3,
		ExpectedStoryboardVersion: 7,
		Action:                    "generate_element_assets",
		Payload: MediaGeneratorPayload{
			StoryboardID: "storyboard-1",
			TargetType:   "all",
			MediaKinds:   []string{"image"},
			CheckpointID: "media-cp-1",
		},
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	out, err := tool.InvokableRun(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var got ToolResultEnvelope[MediaGeneratorToolResult]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.Status != ToolStatusQueued {
		t.Fatalf("status = %q", got.Status)
	}
	if got.NextConfirmationID == "" {
		t.Fatal("missing next confirmation id")
	}
	if got.Data.Interrupt == nil || got.Data.Interrupt.Event != "a2ui.interrupt_request" {
		t.Fatalf("interrupt = %#v", got.Data.Interrupt)
	}
	if got.Data.Interrupt.Payload.CheckpointID != "media-cp-1" {
		t.Fatalf("checkpoint id = %#v", got.Data.Interrupt.Payload)
	}
}

func TestMediaGeneratorToolPassesDispatcher(t *testing.T) {
	dispatcher := &fakeMediaJobDispatcher{}
	tool := NewMediaGeneratorTool(MediaGeneratorToolConfig{
		Checkpoints: mediagraph.NewMemoryCheckpointStore(),
		Dispatcher:  dispatcher,
		NewID:       sequentialMediaToolIDs("job-1"),
	})

	args := ToolInvocationEnvelope[MediaGeneratorPayload]{
		SessionID:                 "session-1",
		RequestID:                 "request-1",
		IdempotencyKey:            "idem-1",
		ExpectedStoryboardVersion: 7,
		Action:                    "generate_element_assets",
		Payload: MediaGeneratorPayload{
			StoryboardID: "storyboard-1",
			TargetType:   generation.TargetKeyElement,
			TargetIDs:    []string{"suji"},
			MediaKinds:   []string{"image"},
			CheckpointID: "media-cp-2",
		},
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	if _, err := tool.InvokableRun(context.Background(), string(raw)); err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if len(dispatcher.jobs) != 1 || dispatcher.jobs[0].ID != "job-1" || dispatcher.jobs[0].Provider != generation.ProviderImage2 {
		t.Fatalf("jobs = %#v", dispatcher.jobs)
	}
}

func TestMediaGeneratorToolRejectsDirectPayload(t *testing.T) {
	tool := NewMediaGeneratorTool(MediaGeneratorToolConfig{
		Checkpoints: mediagraph.NewMemoryCheckpointStore(),
	})

	_, err := tool.InvokableRun(context.Background(), `{"storyboard_id":"storyboard-1","target_type":"all","media_kinds":["image"]}`)
	if err == nil {
		t.Fatal("expected direct payload to be rejected")
	}
}

type fakeMediaJobDispatcher struct {
	jobs []generation.GenerationJob
}

func (d *fakeMediaJobDispatcher) Dispatch(_ context.Context, job generation.GenerationJob) (generation.GenerationJob, bool, error) {
	d.jobs = append(d.jobs, job)
	return job, true, nil
}

func sequentialMediaToolIDs(ids ...string) func() string {
	i := 0
	return func() string {
		if i >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[i]
		i++
		return id
	}
}
