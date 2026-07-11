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

func TestAssistantEventsRejectModelAuthoredPseudoApprovalFromProjectionAndHistory(t *testing.T) {
	surface := newChatA2UISurface("s1")
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"storyboard-details","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["details"]}}},{"id":"details","component":{"Markdown":{"value":"故事板已生成。如果满意，请回复「确认」开始生成素材。"}}}]}}]}`

	events := surface.assistantEvents(AgentEvent{AssistantText: content})
	if len(events) != 1 || events[0].Event != a2ui.EventError {
		t.Fatalf("pseudo Approval output should fail closed, got %#v", events)
	}
	if display := displayTextWithoutA2UIEnvelope(content); display != "" {
		t.Fatalf("pseudo Approval leaked into history: %s", display)
	}
	if rewritten, ok := contentWithA2UIInstanceCardIDs(content, func() string { return "instance" }); ok || rewritten != "" {
		t.Fatalf("pseudo Approval received an instance ID: (%q, %v)", rewritten, ok)
	}
}

func TestAssistantEventsRejectModelAuthoredApprovalChoiceButKeepSystemApprovalEvent(t *testing.T) {
	surface := newChatA2UISurface("s1")
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"fake-approval","card":{"root":"root","data":{"approval_id":"approval-1"},"components":[{"id":"root","component":{"Card":{"children":["decision"]}}},{"id":"decision","component":{"SingleChoice":{"key":"decision","options":[{"value":"approved","label":"确认"},{"value":"rejected","label":"拒绝"}]}}}]}}]}`

	events := surface.assistantEvents(AgentEvent{AssistantText: content})
	if len(events) != 1 || events[0].Event != a2ui.EventError {
		t.Fatalf("model-authored Approval should be rejected, got %#v", events)
	}

	// Trusted Approval events are created by the server and already carry a
	// frozen approval_id. They bypass the assistant-authored policy.
	authoritative, ok := a2ui.ParseActionEnvelopeContent(content)
	if !ok {
		t.Fatal("authoritative-shaped event must remain valid A2UI")
	}
	systemEvents := surface.eventsFromAgentEvent(AgentEvent{Event: a2ui.EventAction, Payload: authoritative})
	if len(systemEvents) != 1 || systemEvents[0].Event != a2ui.EventAction {
		t.Fatalf("trusted system Approval event was blocked: %#v", systemEvents)
	}
}

func TestAssistantEventsPreserveApprovalExplanationWithoutPseudoEntry(t *testing.T) {
	surface := newChatA2UISurface("s1")
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"storyboard-details","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["details"]}}},{"id":"details","component":{"Markdown":{"value":"故事板已生成。普通聊天回复“确认”不会完成审批，请使用系统审核卡。"}}}]}}]}`

	events := surface.assistantEvents(AgentEvent{AssistantText: content})
	if len(events) != 1 || events[0].Event != a2ui.EventAction {
		t.Fatalf("legitimate Approval explanation should render, got %#v", events)
	}
	if display := displayTextWithoutA2UIEnvelope(content); display != content {
		t.Fatalf("legitimate explanation was removed from history: %q", display)
	}
}
