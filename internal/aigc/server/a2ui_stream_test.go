package server

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

func TestAssistantEventsRejectPlainAssistantText(t *testing.T) {
	surface := newChatA2UISurface("s1")

	events := surface.assistantEvents(AgentEvent{AssistantText: "普通说明"})

	if len(events) != 1 {
		t.Fatalf("plain assistant text should produce one protocol error, got %d events: %#v", len(events), events)
	}
	if events[0].Event != a2ui.EventError {
		t.Fatalf("event = %s, want %s", events[0].Event, a2ui.EventError)
	}
	if _, ok := events[0].Payload.(map[string]any); !ok {
		t.Fatalf("payload = %#v", events[0].Payload)
	}
}

func TestAssistantEventsPassesPureA2UIActionEnvelope(t *testing.T) {
	surface := newChatA2UISurface("s1")
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"brief-intake","card":{"card_type":"info_collection","root":"root","components":[{"id":"root","component":{"Card":{"children":["product"]}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类"}}}]}}]}`

	events := surface.assistantEvents(AgentEvent{AssistantText: content})

	if len(events) != 1 {
		t.Fatalf("pure ActionEnvelope should produce one event, got %d events: %#v", len(events), events)
	}
	envelope, ok := events[0].Payload.(a2ui.ActionEnvelope)
	if !ok {
		t.Fatalf("payload = %#v", events[0].Payload)
	}
	if len(envelope.Actions) != 1 || envelope.Actions[0].CardID != "brief-intake" {
		t.Fatalf("envelope actions = %#v", envelope.Actions)
	}
}

func TestAssistantEventsRejectMixedA2UIAsProtocolError(t *testing.T) {
	surface := newChatA2UISurface("s1")
	content := "好的，先补充信息：\n" +
		`{"a2ui_version":"1.0","actions":[{"type":"append_card","card_id":"brief-intake"}]}`

	events := surface.assistantEvents(AgentEvent{AssistantText: content})

	if len(events) != 1 || events[0].Event != a2ui.EventError {
		t.Fatalf("mixed A2UI output should be rejected, got %#v", events)
	}
}
