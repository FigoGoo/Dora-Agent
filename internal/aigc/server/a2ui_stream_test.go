package server

import (
	"testing"

	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

// The agent is NOT allowed to fabricate confirmation checkpoints by embedding an
// a2ui.interrupt_request in its message. Real interrupts only come from tool
// checkpoints (media_graph / runner). normalizeA2UIEvents must drop any
// interrupt_request emitted from assistant message content, while keeping other
// a2ui.* surface events.
func TestNormalizeA2UIEventsDropsAgentInterruptRequest(t *testing.T) {
	events := []aigctools.RenderEventHint{
		{Event: "a2ui.surface_update", SurfaceID: "storyboard", Payload: map[string]any{"root": "r"}},
		{Event: "a2ui.interrupt_request", SurfaceID: "storyboard", Payload: map[string]any{
			"interrupt_id": "fabricated", "checkpoint_id": "media_graph:",
			"actions": []any{map[string]any{"key": "confirm_reference_image"}},
		}},
		{Event: "a2ui.data_model_update", SurfaceID: "chat", Payload: map[string]any{"contents": []any{}}},
	}

	out := normalizeA2UIEvents(events)

	for _, e := range out {
		if e.Event == "a2ui.interrupt_request" {
			t.Fatalf("interrupt_request from agent message must be dropped, got %#v", out)
		}
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving events (surface_update, data_model_update), got %d: %#v", len(out), out)
	}
	if out[0].Event != "a2ui.surface_update" || out[1].Event != "a2ui.data_model_update" {
		t.Fatalf("surviving events wrong: %#v", out)
	}
}
