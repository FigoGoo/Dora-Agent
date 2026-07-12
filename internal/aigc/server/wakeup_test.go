package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

func TestJobWakeupRunnerInvokesAgentAndPublishesEvents(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "m1", SessionID: "s1", Role: "user", Content: "生成参考图", Seq: 1},
	}
	assistantEnvelope := assistantCardEnvelope(t, "wakeup-next", "参考图已完成，我会继续规划下一步。")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
		}},
	}
	broker := a2ui.NewMemoryBroker(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	runner := NewJobWakeupRunner(JobWakeupRunnerConfig{
		Store:   store,
		Invoker: invoker,
		Events:  broker,
		NewID:   sequentialIDs("run-wakeup-1", "msg-wakeup-1", "evt-wakeup-1", "msg-assistant-1"),
		Now:     fixedNow,
	})
	err := runner.Wakeup(ctx, session.JobWakeupEvent{
		SessionID: "s1",
		JobID:     "job-1",
		Status:    generation.StatusSucceeded,
		AssetIDs:  []string{"asset-1"},
	})
	if err != nil {
		t.Fatalf("Wakeup() error = %v", err)
	}

	got := <-events
	if got.Event != a2ui.EventAction || got.RunID != "run-wakeup-1" {
		t.Fatalf("event = %#v", got)
	}
	envelope, ok := got.Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 || envelope.Actions[0].Type != a2ui.ActionAppendCard {
		t.Fatalf("payload = %#v", got.Payload)
	}
	if len(invoker.seenMessages) != 2 {
		t.Fatalf("seen messages = %#v", invoker.seenMessages)
	}
	if invoker.seenMessages[1].Role != schema.System || invoker.seenMessages[1].Content == "" {
		t.Fatalf("wakeup message = %#v", invoker.seenMessages[1])
	}
	appended := store.messages["s1"]
	if len(appended) != 3 || appended[1].Role != "system" || appended[2].Role != "assistant" {
		t.Fatalf("appended messages = %#v", appended)
	}
}

func TestJobWakeupRunnerUsesLatestCompleteAssistantMessage(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "m1", SessionID: "s1", Role: "user", Content: "生成参考图", Seq: 1},
	}
	historyEnvelope := assistantCardEnvelope(t, "history-reply", "历史回复")
	latestEnvelope := assistantCardEnvelope(t, "latest-reply", "最新回复")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": historyEnvelope},
				AssistantText: historyEnvelope,
				Message:       schema.AssistantMessage(historyEnvelope, nil),
			},
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": latestEnvelope},
				AssistantText: latestEnvelope,
				Message:       schema.AssistantMessage(latestEnvelope, nil),
			},
		},
	}
	broker := a2ui.NewMemoryBroker(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := broker.Subscribe(ctx, "s1")
	defer unsubscribe()

	runner := NewJobWakeupRunner(JobWakeupRunnerConfig{
		Store:   store,
		Invoker: invoker,
		Events:  broker,
		NewID:   sequentialIDs("run-wakeup-1", "msg-wakeup-1", "evt-wakeup-1", "msg-assistant-1"),
		Now:     fixedNow,
	})
	err := runner.Wakeup(ctx, session.JobWakeupEvent{
		SessionID: "s1",
		JobID:     "job-1",
		Status:    generation.StatusSucceeded,
	})
	if err != nil {
		t.Fatalf("Wakeup() error = %v", err)
	}

	got := <-events
	rawBytes, err := json.Marshal(got.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	raw := string(rawBytes)
	if strings.Contains(raw, "历史回复") || !strings.Contains(raw, "最新回复") {
		t.Fatalf("payload should contain only latest assistant response, got %#v", got.Payload)
	}
	appended := store.messages["s1"]
	if len(appended) != 3 || appended[1].Role != "system" || appended[2].Role != "assistant" {
		t.Fatalf("appended messages = %#v", appended)
	}
}

func TestJobWakeupRunnerSuppressesModelSummaryWhenCapabilityCardOwnsProgress(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	broker := &fakeEventSubscriber{}
	modelSummary := assistantCardEnvelope(t, "wakeup-media-progress", "继续生成下一批素材。")
	events := make(chan AgentEvent, 3)
	events <- messageToAgentEvent(
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-media", Type: "function",
			Function: schema.FunctionCall{Name: capability.GenerateMediaToolKey, Arguments: `{}`},
		}}),
	)
	events <- messageToAgentEvent(
		schema.ToolMessage(
			`{"status":"accepted","operation_id":"operation-1"}`,
			"call-media", schema.WithToolName(capability.GenerateMediaToolKey),
		),
	)
	events <- AgentEvent{
		Event: a2ui.EventChatMessage, AssistantText: modelSummary,
		Message: schema.AssistantMessage(modelSummary, nil),
	}
	close(events)

	runner := NewJobWakeupRunner(JobWakeupRunnerConfig{
		Store: store, Events: broker,
		NewID: sequentialIDs("evt-call", "msg-call", "evt-result", "msg-result"),
		Now:   fixedNow,
	})
	if err := runner.publishAgentEvents(context.Background(), "s1", "run-wakeup-media", events); err != nil {
		t.Fatal(err)
	}
	if len(broker.published) != 2 {
		t.Fatalf("durable wakeup Tool progress events = %d, want 2: %#v", len(broker.published), broker.published)
	}
	got := store.messages["s1"]
	if len(got) != 2 || got[0].Role != string(schema.Assistant) || got[1].Role != string(schema.Tool) {
		t.Fatalf("wakeup Tool history was not preserved: %#v", got)
	}
	for _, record := range got {
		if strings.Contains(record.Content, "wakeup-media-progress") {
			t.Fatalf("wakeup model summary leaked into history: %#v", got)
		}
	}
}
