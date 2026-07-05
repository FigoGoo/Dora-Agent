package server

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

func TestRunnerInvokerConvertsAssistantMessageToChatDelta(t *testing.T) {
	agent := &mockRunnerAgent{
		events: []*adk.AgentEvent{
			{
				Output: &adk.AgentOutput{
					MessageOutput: &adk.MessageVariant{
						Message: schema.AssistantMessage("故事板草案已生成", nil),
						Role:    schema.Assistant,
					},
				},
			},
		},
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	invoker := NewRunnerInvoker(runner)

	events, err := invoker.Invoke(context.Background(), AgentInvokeRequest{
		Messages: []*schema.Message{schema.UserMessage("开始")},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	event, ok := <-events
	if !ok {
		t.Fatal("expected an event")
	}
	if event.Event != a2ui.EventChatDelta {
		t.Fatalf("event = %q", event.Event)
	}
	if event.AssistantText != "故事板草案已生成" {
		t.Fatalf("assistant text = %q", event.AssistantText)
	}
	if _, ok := <-events; ok {
		t.Fatal("expected events channel to close")
	}
}

func TestMessageToAgentEventCarriesSchemaMessages(t *testing.T) {
	call := schema.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "text_editor",
			Arguments: `{"document_type":"final_video_spec"}`,
		},
	}

	assistantEvent := messageToAgentEvent(schema.AssistantMessage("", []schema.ToolCall{call}))
	if assistantEvent.Event != a2ui.EventToolProgress {
		t.Fatalf("assistant tool call event = %q", assistantEvent.Event)
	}
	if assistantEvent.Message == nil || assistantEvent.Message.Role != schema.Assistant || len(assistantEvent.Message.ToolCalls) != 1 {
		t.Fatalf("assistant schema message = %#v", assistantEvent)
	}

	toolEvent := messageToAgentEvent(&schema.Message{
		Role:       schema.Tool,
		Content:    `{"status":"ok"}`,
		ToolCallID: "call-1",
		ToolName:   "text_editor",
	})
	if toolEvent.Message == nil || toolEvent.Message.Role != schema.Tool || toolEvent.Message.Content != `{"status":"ok"}` {
		t.Fatalf("tool schema message = %#v", toolEvent)
	}
	if toolEvent.Message.ToolCallID != "call-1" || toolEvent.Message.ToolName != "text_editor" {
		t.Fatalf("tool ids = %#v", toolEvent)
	}
}

func TestRunnerInvokerConvertsInterruptAction(t *testing.T) {
	agent := &mockRunnerAgent{
		events: []*adk.AgentEvent{
			adk.Interrupt(context.Background(), "请确认参考图"),
		},
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	invoker := NewRunnerInvoker(runner)

	events, err := invoker.Invoke(context.Background(), AgentInvokeRequest{
		Messages:     []*schema.Message{schema.UserMessage("生成素材")},
		CheckpointID: "s1",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	event, ok := <-events
	if !ok {
		t.Fatal("expected an interrupt event")
	}
	if event.Event != a2ui.EventInterruptRequest {
		t.Fatalf("event = %q", event.Event)
	}
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", event.Payload)
	}
	if payload["checkpoint_id"] != "s1" || payload["interrupt_id"] == "" {
		t.Fatalf("interrupt payload = %#v", payload)
	}
}

type mockRunnerAgent struct {
	events []*adk.AgentEvent
}

func (a *mockRunnerAgent) Name(context.Context) string {
	return "mock"
}

func (a *mockRunnerAgent) Description(context.Context) string {
	return "mock agent"
}

func (a *mockRunnerAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		for _, event := range a.events {
			gen.Send(event)
		}
	}()
	return iter
}
