package server

import (
	"context"
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
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "参考图已完成，我会继续规划下一步。"},
			AssistantText: "参考图已完成，我会继续规划下一步。",
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
	if got.Event != a2ui.EventChatDelta || got.RunID != "run-wakeup-1" {
		t.Fatalf("event = %#v", got)
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
