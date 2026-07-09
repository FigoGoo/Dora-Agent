package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
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
