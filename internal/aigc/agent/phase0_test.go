package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

func TestNewPhase0Runner(t *testing.T) {
	runner, err := NewPhase0Runner(context.Background(), Phase0Config{})
	if err != nil {
		t.Fatalf("NewPhase0Runner() error = %v", err)
	}

	iter := runner.Query(context.Background(), "hello")
	event, ok := iter.Next()
	if !ok {
		t.Fatalf("expected agent event")
	}
	if event.Err != nil {
		t.Fatalf("event error = %v", event.Err)
	}
	if event.Output == nil || event.Output.MessageOutput == nil {
		t.Fatalf("expected message output, got %#v", event.Output)
	}
	msg, err := event.Output.MessageOutput.GetMessage()
	if err != nil {
		t.Fatalf("GetMessage() error = %v", err)
	}
	if len(msg.ContentBlocks) == 0 || !strings.Contains(msg.ContentBlocks[0].AssistantGenText.Text, "Phase 0") {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestMemoryMessageRebuilder(t *testing.T) {
	rebuilder := NewMemoryMessageRebuilder(
		session.MessageRecord{ID: "m2", SessionID: "s1", Role: "assistant", Content: "hi", Seq: 2, CreatedAt: time.Now()},
		session.MessageRecord{ID: "m1", SessionID: "s1", Role: "user", Content: "hello", Seq: 1, CreatedAt: time.Now()},
	)

	msgs, err := rebuilder.BuildAgenticMessages(context.Background(), "s1", MessageWindow{})
	if err != nil {
		t.Fatalf("BuildAgenticMessages() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("message count = %d", len(msgs))
	}
	if msgs[0].ContentBlocks[0].UserInputText.Text != "hello" {
		t.Fatalf("messages were not sorted or rebuilt correctly: %#v", msgs[0])
	}
	if msgs[1].ContentBlocks[0].AssistantGenText.Text != "hi" {
		t.Fatalf("assistant message not rebuilt correctly: %#v", msgs[1])
	}
}
