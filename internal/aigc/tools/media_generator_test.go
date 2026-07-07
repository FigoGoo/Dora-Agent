package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type fakeMediaStoryboardReader struct {
	board storyboard.Storyboard
}

func (r *fakeMediaStoryboardReader) GetLatestBySession(_ context.Context, _ string) (storyboard.Storyboard, error) {
	return r.board, nil
}

// When the agent invokes media_generator without explicit target_ids (e.g.
// target_type="all"), the tool must enumerate the storyboard's REAL, still-unbound
// key elements as image2 targets — so it produces bindable reference images instead
// of one bogus "storyboard"/"all" target that can never bind.
func TestMediaGeneratorToolEnumeratesRealKeyElementsWhenNoTargets(t *testing.T) {
	dispatcher := &fakeMediaJobDispatcher{}
	boards := &fakeMediaStoryboardReader{board: storyboard.Storyboard{
		ID:        "storyboard-1",
		SessionID: "session-1",
		Version:   3,
		KeyElements: []storyboard.KeyElement{
			{Key: "ke_1", Name: "桃子特写"},                            // unbound → dispatch
			{Key: "ke_2", Name: "果园", AssetIDs: []string{"a1"}},     // already bound → skip
			{Key: "ke_3", Name: "价格卡"},                              // unbound → dispatch
		},
	}}
	tool := NewMediaGeneratorTool(MediaGeneratorToolConfig{
		Checkpoints: mediagraph.NewMemoryCheckpointStore(),
		Dispatcher:  dispatcher,
		Storyboards: boards,
		NewID:       sequentialMediaToolIDs("job-1", "job-2", "job-3"),
	})

	args := ToolInvocationEnvelope[MediaGeneratorPayload]{
		SessionID:      "session-1",
		IdempotencyKey: "idem-1",
		Payload: MediaGeneratorPayload{
			StoryboardID: "storyboard-1",
			TargetType:   "all", // agent's vague target, no target_ids
			MediaKinds:   []string{"image", "keyframe"},
		},
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), string(raw)); err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	if len(dispatcher.jobs) != 2 {
		t.Fatalf("want 2 jobs (unbound ke_1, ke_3), got %d: %#v", len(dispatcher.jobs), dispatcher.jobs)
	}
	seen := map[string]bool{}
	for _, j := range dispatcher.jobs {
		if j.Provider != generation.ProviderImage2 {
			t.Fatalf("provider = %q, want image2: %#v", j.Provider, j)
		}
		if j.TargetType != generation.TargetKeyElement {
			t.Fatalf("target_type = %q, want key_element: %#v", j.TargetType, j)
		}
		seen[j.TargetID] = true
	}
	if !seen["ke_1"] || !seen["ke_3"] || seen["ke_2"] {
		t.Fatalf("targets = %v, want {ke_1,ke_3} and NOT ke_2", seen)
	}
}

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
		IdempotencyKey:            "idem-1",
		ExpectedStoryboardVersion: 7,
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

// Two sessions that both omit checkpoint_id/idempotency_key must NOT collide on a
// shared media graph checkpoint (the old bug: both became "media_graph:", so the
// 2nd session resumed the 1st's already-interrupted checkpoint and never dispatched).
func TestMediaGraphCheckpointIDIsSessionScoped(t *testing.T) {
	inv := ToolInvocationEnvelope[MediaGeneratorPayload]{Payload: MediaGeneratorPayload{}}
	a := mediaGraphCheckpointID(inv, "session-A", "sb-A")
	b := mediaGraphCheckpointID(inv, "session-B", "sb-B")
	if a == b {
		t.Fatalf("distinct sessions must get distinct checkpoint ids, both = %q", a)
	}
	if a == "media_graph:" || b == "media_graph:" {
		t.Fatalf("checkpoint id must not degenerate to bare prefix: a=%q b=%q", a, b)
	}
	// Explicit checkpoint_id from the caller always wins (resume path).
	explicit := ToolInvocationEnvelope[MediaGeneratorPayload]{Payload: MediaGeneratorPayload{CheckpointID: "cp-explicit"}}
	if got := mediaGraphCheckpointID(explicit, "session-A", "sb-A"); got != "cp-explicit" {
		t.Fatalf("explicit checkpoint id must win, got %q", got)
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
