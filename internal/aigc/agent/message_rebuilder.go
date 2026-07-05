package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

type MessageWindow = session.MessageWindow

type MessageRebuilder interface {
	BuildAgenticMessages(ctx context.Context, sessionID string, limit MessageWindow) ([]*schema.AgenticMessage, error)
	AppendRecord(ctx context.Context, record session.MessageRecord) error
}

type MemoryMessageRebuilder struct {
	records []session.MessageRecord
}

func NewMemoryMessageRebuilder(records ...session.MessageRecord) *MemoryMessageRebuilder {
	r := &MemoryMessageRebuilder{}
	r.records = append(r.records, records...)
	return r
}

func (r *MemoryMessageRebuilder) AppendRecord(_ context.Context, record session.MessageRecord) error {
	r.records = append(r.records, record)
	return nil
}

func (r *MemoryMessageRebuilder) BuildAgenticMessages(_ context.Context, sessionID string, window MessageWindow) ([]*schema.AgenticMessage, error) {
	records := make([]session.MessageRecord, 0, len(r.records))
	for _, record := range r.records {
		if record.SessionID == sessionID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Seq < records[j].Seq
	})
	if window.Limit > 0 && len(records) > window.Limit {
		records = records[len(records)-window.Limit:]
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

func recordToAgenticMessage(record session.MessageRecord) (*schema.AgenticMessage, error) {
	if len(record.ContentBlocks) > 0 {
		var blocks []*schema.ContentBlock
		if err := json.Unmarshal(record.ContentBlocks, &blocks); err != nil {
			return nil, fmt.Errorf("decode content blocks for message %s: %w", record.ID, err)
		}
		return &schema.AgenticMessage{
			Role:          agenticRole(record.Role),
			ContentBlocks: blocks,
		}, nil
	}

	switch record.Role {
	case "system":
		return schema.SystemAgenticMessage(record.Content), nil
	case "user":
		return schema.UserAgenticMessage(record.Content), nil
	case "tool":
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.FunctionToolResult{
					CallID: record.ToolCallID,
					Name:   record.ToolName,
					Content: []*schema.FunctionToolResultContentBlock{
						{
							Type: schema.FunctionToolResultContentBlockTypeText,
							Text: &schema.UserInputText{Text: record.Content},
						},
					},
				}),
			},
		}, nil
	default:
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: record.Content}),
			},
		}, nil
	}
}

func agenticRole(role string) schema.AgenticRoleType {
	switch role {
	case "system":
		return schema.AgenticRoleTypeSystem
	case "user", "tool":
		return schema.AgenticRoleTypeUser
	default:
		return schema.AgenticRoleTypeAssistant
	}
}
