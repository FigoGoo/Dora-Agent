package server

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

func TestToolProgressEventsHideSkillLoaderAndKeepCapabilityProgress(t *testing.T) {
	surface := newChatA2UISurface("s1")
	event := messageToAgentEvent(schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call-skill", Type: "function", Function: schema.FunctionCall{Name: "skill", Arguments: `{"skill":"video"}`}},
		{ID: "call-plan", Type: "function", Function: schema.FunctionCall{Name: "plan_storyboard", Arguments: `{}`}},
	}))

	events := surface.eventsFromAgentEvent(event)
	if len(events) != 1 {
		t.Fatalf("render events = %#v, want one capability progress event", events)
	}
	envelope, ok := events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 {
		t.Fatalf("tool progress envelope = %#v", events[0].Payload)
	}
	action := envelope.Actions[0]
	if action.Target == nil || action.Target.CardID != "tool_run:call-plan" {
		t.Fatalf("visible tool action = %#v", action)
	}
}

func TestToolProgressEventsHideSkillLoaderError(t *testing.T) {
	surface := newChatA2UISurface("s1")
	event := messageToAgentEvent(schema.ToolMessage(
		`{"status":"error","message":"skill not found"}`,
		"call-skill",
		schema.WithToolName("skill"),
	))

	if events := surface.eventsFromAgentEvent(event); len(events) != 0 {
		t.Fatalf("Skill loader error leaked to user-visible tool_runs: %#v", events)
	}
}
