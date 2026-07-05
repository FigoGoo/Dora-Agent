package agent

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

type fakeMessageStore struct {
	records []session.MessageRecord
}

func (s *fakeMessageStore) AppendMessage(_ context.Context, record session.MessageRecord) (session.MessageRecord, error) {
	if record.Seq == 0 {
		record.Seq = int64(len(s.records) + 1)
	}
	s.records = append(s.records, record)
	return record, nil
}

func (s *fakeMessageStore) ListMessages(_ context.Context, sessionID string, window session.MessageWindow) ([]session.MessageRecord, error) {
	out := make([]session.MessageRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.SessionID == sessionID {
			out = append(out, record)
		}
	}
	if window.Limit > 0 && len(out) > window.Limit {
		out = out[len(out)-window.Limit:]
	}
	return out, nil
}

func TestStoreMessageRebuilderBuildsAgenticMessages(t *testing.T) {
	store := &fakeMessageStore{}
	rebuilder := NewStoreMessageRebuilder(store)

	if err := rebuilder.AppendRecord(context.Background(), session.MessageRecord{
		ID:        "m1",
		SessionID: "s1",
		Role:      "user",
		Content:   "hello",
	}); err != nil {
		t.Fatalf("AppendRecord(user) error = %v", err)
	}
	if err := rebuilder.AppendRecord(context.Background(), session.MessageRecord{
		ID:        "m2",
		SessionID: "s1",
		Role:      "assistant",
		Content:   "hi",
	}); err != nil {
		t.Fatalf("AppendRecord(assistant) error = %v", err)
	}

	messages, err := rebuilder.BuildAgenticMessages(context.Background(), "s1", MessageWindow{Limit: 1})
	if err != nil {
		t.Fatalf("BuildAgenticMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d", len(messages))
	}
	if got := messages[0].ContentBlocks[0].AssistantGenText.Text; got != "hi" {
		t.Fatalf("assistant content = %q", got)
	}
}
