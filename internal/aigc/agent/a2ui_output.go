package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

// a2uiOutputNormalizerMiddleware removes provider formatting noise around one
// complete final ActionEnvelope. Raw provider output is still frozen by the
// inner ModelReceipt middleware before this wrapper returns a normalized copy.
type a2uiOutputNormalizerMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newA2UIOutputNormalizerMiddleware() *a2uiOutputNormalizerMiddleware {
	return &a2uiOutputNormalizerMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *a2uiOutputNormalizerMiddleware) WrapModel(_ context.Context, inner model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	if inner == nil {
		return nil, fmt.Errorf("A2UI output normalizer inner model is required")
	}
	return &a2uiOutputNormalizingModel{inner: inner}, nil
}

type a2uiOutputNormalizingModel struct{ inner model.BaseChatModel }

func (m *a2uiOutputNormalizingModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	message, err := m.inner.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return normalizeFinalA2UIMessage(message), nil
}

func (m *a2uiOutputNormalizingModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reader, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, fmt.Errorf("A2UI output normalizer received a nil stream")
	}
	defer reader.Close()
	chunks := make([]*schema.Message, 0, 4)
	for {
		chunk, recvErr := reader.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("A2UI output normalizer received an empty stream")
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, fmt.Errorf("concat A2UI model stream: %w", err)
	}
	return schema.StreamReaderFromArray([]*schema.Message{normalizeFinalA2UIMessage(message)}), nil
}

func normalizeFinalA2UIMessage(message *schema.Message) *schema.Message {
	if message == nil || len(message.ToolCalls) > 0 {
		return message
	}
	normalized, ok := normalizeSingleA2UIEnvelope(message.Content)
	if !ok || normalized == message.Content {
		return message
	}
	copy := *message
	copy.Content = normalized
	return &copy
}

// normalizeSingleA2UIEnvelope accepts either strict A2UI JSON or exactly one
// embedded top-level JSON object. As a provider-formatting compatibility shim,
// it may remove one extra object closer immediately before the terminal array
// closers. It refuses emulated DSML ToolCalls, every other repair shape and
// every result that fails the ordinary strict A2UI parser.
func normalizeSingleA2UIEnvelope(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if envelope, ok := a2ui.ParseActionEnvelopeContent(trimmed); ok {
		return marshalA2UIEnvelope(envelope)
	}
	if strings.Contains(strings.ToLower(trimmed), "dsml") || strings.Contains(trimmed, "<｜｜") {
		return "", false
	}
	candidates, balanced := topLevelJSONObjects(trimmed)
	if balanced && len(candidates) == 1 {
		if envelope, ok := a2ui.ParseActionEnvelopeContent(candidates[0]); ok {
			return marshalA2UIEnvelope(envelope)
		}
	}

	// A mismatched closer can make the brace-only extractor terminate early.
	// Retry only the full first-{ through last-} span, and only when exactly one
	// unambiguously mismatched closer can be removed. Missing delimiters and any
	// second mismatch remain hard failures.
	first, last := strings.IndexByte(trimmed, '{'), strings.LastIndexByte(trimmed, '}')
	if first < 0 || last <= first {
		return "", false
	}
	if hasJSONStructuralDelimiter(trimmed[:first]) || hasJSONStructuralDelimiter(trimmed[last+1:]) {
		return "", false
	}
	repaired, ok := repairSingleMismatchedJSONCloser(trimmed[first : last+1])
	if !ok {
		return "", false
	}
	envelope, ok := a2ui.ParseActionEnvelopeContent(repaired)
	if !ok {
		return "", false
	}
	return marshalA2UIEnvelope(envelope)
}

func hasJSONStructuralDelimiter(content string) bool {
	return strings.ContainsAny(content, "{}[]")
}

func marshalA2UIEnvelope(envelope a2ui.ActionEnvelope) (string, bool) {
	raw, err := json.Marshal(envelope)
	return string(raw), err == nil
}

func repairSingleMismatchedJSONCloser(content string) (string, bool) {
	stack := make([]byte, 0, 8)
	repaired := make([]byte, 0, len(content))
	inString, escaped, repairedCloser, closingSuffix := false, false, false, false

	for index := 0; index < len(content); index++ {
		char := content[index]
		if inString {
			repaired = append(repaired, char)
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		if closingSuffix && !isJSONWhitespace(char) && char != '}' && char != ']' {
			return "", false
		}

		switch char {
		case '"':
			inString = true
			repaired = append(repaired, char)
		case '{', '[':
			stack = append(stack, char)
			repaired = append(repaired, char)
		case '}', ']':
			if len(stack) == 0 || !jsonDelimitersMatch(stack[len(stack)-1], char) {
				// This shim exists for one observed provider defect only: an
				// extra object closer immediately before an array closes.
				if repairedCloser || len(stack) == 0 || stack[len(stack)-1] != '[' || char != '}' {
					return "", false
				}
				repairedCloser = true
				closingSuffix = true
				continue
			}
			stack = stack[:len(stack)-1]
			repaired = append(repaired, char)
		default:
			repaired = append(repaired, char)
		}
	}

	if inString || escaped || len(stack) != 0 || !repairedCloser {
		return "", false
	}
	return string(repaired), true
}

func isJSONWhitespace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}

func jsonDelimitersMatch(open, close byte) bool {
	return open == '{' && close == '}' || open == '[' && close == ']'
}

func topLevelJSONObjects(content string) ([]string, bool) {
	objects := make([]string, 0, 1)
	start, depth := -1, 0
	inString, escaped := false, false
	for index := 0; index < len(content); index++ {
		char := content[index]
		if start < 0 {
			if char == '{' {
				start, depth = index, 1
			} else if char == '}' || char == '[' || char == ']' {
				return nil, false
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return nil, false
			}
			if depth == 0 {
				objects = append(objects, content[start:index+1])
				start = -1
			}
		}
	}
	return objects, start < 0 && !inString && !escaped
}

var _ adk.ChatModelAgentMiddleware = (*a2uiOutputNormalizerMiddleware)(nil)
var _ model.BaseChatModel = (*a2uiOutputNormalizingModel)(nil)
