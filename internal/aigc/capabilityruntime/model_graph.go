package capabilityruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type modelGraphRequest struct {
	System string
	Input  any
}

// compileModelGraph turns every internal inference step, including prompt
// preparation, into an explicit Eino ChatModel node. It is deliberately not
// registered as an Agent Tool.
func compileModelGraph(ctx context.Context, model einomodel.BaseChatModel) (compose.Runnable[modelGraphRequest, json.RawMessage], error) {
	if model == nil {
		return nil, nil
	}
	graph := compose.NewGraph[modelGraphRequest, json.RawMessage]()
	prepare := func(_ context.Context, request modelGraphRequest) ([]*schema.Message, error) {
		system := strings.TrimSpace(request.System)
		if system == "" {
			return nil, fmt.Errorf("internal ChatModel system instruction is required")
		}
		raw, err := json.Marshal(request.Input)
		if err != nil {
			return nil, fmt.Errorf("marshal internal ChatModel input: %w", err)
		}
		return []*schema.Message{schema.SystemMessage(system), schema.UserMessage(string(raw))}, nil
	}
	decode := func(_ context.Context, message *schema.Message) (json.RawMessage, error) {
		if message == nil {
			return nil, fmt.Errorf("internal ChatModel returned an empty message")
		}
		content := strings.TrimSpace(message.Content)
		start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
		if start < 0 || end < start {
			return nil, fmt.Errorf("internal ChatModel returned non-JSON output")
		}
		raw := json.RawMessage(append([]byte(nil), content[start:end+1]...))
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode internal ChatModel JSON: %w", err)
		}
		return raw, nil
	}
	if err := graph.AddLambdaNode("prepare_messages", compose.InvokableLambda(prepare)); err != nil {
		return nil, err
	}
	if err := graph.AddChatModelNode("chat_model", model); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("decode_json", compose.InvokableLambda(decode)); err != nil {
		return nil, err
	}
	for _, edge := range [][2]string{{compose.START, "prepare_messages"}, {"prepare_messages", "chat_model"}, {"chat_model", "decode_json"}, {"decode_json", compose.END}} {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	return graph.Compile(ctx, compose.WithGraphName("aigc_internal_chat_model"), compose.WithNodeTriggerMode(compose.AllPredecessor))
}
