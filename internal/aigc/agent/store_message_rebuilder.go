package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

type MessageStore interface {
	AppendMessage(ctx context.Context, record session.MessageRecord) (session.MessageRecord, error)
	ListMessages(ctx context.Context, sessionID string, window session.MessageWindow) ([]session.MessageRecord, error)
}

type StoreMessageRebuilder struct {
	store MessageStore
}

func NewStoreMessageRebuilder(store MessageStore) *StoreMessageRebuilder {
	return &StoreMessageRebuilder{store: store}
}

func (r *StoreMessageRebuilder) AppendRecord(ctx context.Context, record session.MessageRecord) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("message store is required")
	}
	_, err := r.store.AppendMessage(ctx, record)
	return err
}

func (r *StoreMessageRebuilder) BuildAgenticMessages(ctx context.Context, sessionID string, window MessageWindow) ([]*schema.AgenticMessage, error) {
	if r == nil || r.store == nil {
		return nil, fmt.Errorf("message store is required")
	}
	records, err := r.store.ListMessages(ctx, sessionID, window)
	if err != nil {
		return nil, err
	}
	messages := make([]*schema.AgenticMessage, 0, len(records))
	for _, record := range records {
		msg, err := recordToAgenticMessage(record)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}
