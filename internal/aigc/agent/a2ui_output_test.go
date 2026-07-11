package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/modelreceipt"
)

const validA2UIFinal = `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"result","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["message"]}}},{"id":"message","component":{"Text":{"value":"完成"}}}]}}]}`

func TestNormalizeSingleA2UIEnvelopeExtractsOnlyOneStrictObject(t *testing.T) {
	normalized, ok := normalizeSingleA2UIEnvelope("创作规范已生成，等待审批。\n\n" + validA2UIFinal)
	if !ok {
		t.Fatalf("expected one embedded envelope to normalize")
	}
	if _, valid := a2ui.ParseActionEnvelopeContent(normalized); !valid {
		t.Fatalf("normalized content is not strict A2UI: %s", normalized)
	}
	for _, rejected := range []string{
		validA2UIFinal + ` {"extra":true}`,
		validA2UIFinal + "\n<｜｜DSML｜｜tool_calls></｜｜DSML｜｜tool_calls>",
		"普通自然语言",
	} {
		if _, accepted := normalizeSingleA2UIEnvelope(rejected); accepted {
			t.Fatalf("unexpected normalization: %s", rejected)
		}
	}
}

func TestNormalizeSingleA2UIEnvelopeRepairsOneMismatchedCloser(t *testing.T) {
	malformed := strings.TrimSuffix(validA2UIFinal, "]}") + "}]}"
	normalized, ok := normalizeSingleA2UIEnvelope("创作规范已生成，等待您的审批。\n\n" + malformed)
	if !ok {
		t.Fatal("expected one mismatched closer to normalize")
	}
	if _, valid := a2ui.ParseActionEnvelopeContent(normalized); !valid {
		t.Fatalf("normalized content is not strict A2UI: %s", normalized)
	}
	if normalized != validA2UIFinal {
		t.Fatalf("normalized content=%s, want=%s", normalized, validA2UIFinal)
	}

	withDelimitersInString := strings.Replace(validA2UIFinal, "完成", `{}[]\\\"中文`, 1)
	malformed = strings.TrimSuffix(withDelimitersInString, "]}") + "}]}"
	if normalized, ok := normalizeSingleA2UIEnvelope("说明\n" + malformed); !ok || normalized != withDelimitersInString {
		t.Fatalf("escaped string normalization=(%q, %v), want=%q", normalized, ok, withDelimitersInString)
	}
}

func TestNormalizeSingleA2UIEnvelopeRejectsUnsafeRepairs(t *testing.T) {
	tests := map[string]string{
		"multiple mismatches": strings.TrimSuffix(validA2UIFinal, "]}") + "}}]}",
		"missing closer":      strings.TrimSuffix(validA2UIFinal, "}"),
		"unclosed string":     `说明 {"a2ui_version":"1.0","actions":[{"type":"append_card}`,
		"wrong closer kind":   `{]}`,
		"token after repair":  strings.TrimSuffix(validA2UIFinal, "]}") + "}true]}",
		"stray closer prefix": "}}" + strings.TrimSuffix(validA2UIFinal, "]}") + "}]}",
		"stray closer suffix": strings.TrimSuffix(validA2UIFinal, "]}") + "}]}}",
		"invalid protocol":    strings.TrimSuffix(strings.Replace(validA2UIFinal, `"1.0"`, `"9.0"`, 1), "]}") + "}]}",
		"dsml":                "<｜｜DSML｜｜tool_calls>" + strings.TrimSuffix(validA2UIFinal, "]}") + "}]}" + "</｜｜DSML｜｜tool_calls>",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if normalized, ok := normalizeSingleA2UIEnvelope(content); ok {
				t.Fatalf("unexpected normalization: %s", normalized)
			}
		})
	}
}

func TestA2UIOutputNormalizerKeepsRawReceiptAndAvoidsRetry(t *testing.T) {
	store := modelreceipt.NewMemoryStore()
	malformed := strings.TrimSuffix(validA2UIFinal, "]}") + "}]}"
	raw := "创作规范已生成，等待审批。\n\n" + malformed
	underlying := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage(raw, nil)}}
	runner := newA2UIOutputNormalizerTestRunner(t, underlying, store)
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-normalize-a2ui"})

	final := runReceiptTestTurn(t, runner, ctx)
	if underlying.CallCount() != 1 {
		t.Fatalf("underlying calls=%d, want 1", underlying.CallCount())
	}
	if strings.Contains(final.Content, "创作规范已生成") {
		t.Fatalf("final content retained provider narration: %s", final.Content)
	}
	if _, ok := a2ui.ParseActionEnvelopeContent(final.Content); !ok {
		t.Fatalf("final content is not strict A2UI: %s", final.Content)
	}
	receipt, err := store.Get(context.Background(), "turn-normalize-a2ui", 1)
	if err != nil {
		t.Fatal(err)
	}
	var frozen *schema.Message
	if err := jsonRoundTrip(receipt.OutputJSON, &frozen); err != nil {
		t.Fatalf("decode raw provider receipt: %v", err)
	}
	if frozen.Content != raw {
		t.Fatalf("raw provider output was not preserved in receipt: %q", frozen.Content)
	}
	if _, err := store.Get(context.Background(), "turn-normalize-a2ui", 2); err == nil {
		t.Fatal("repairable output unexpectedly created a retry receipt")
	}

	replayModel := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage("must-not-run", nil)}}
	replayed := runReceiptTestTurn(t, newA2UIOutputNormalizerTestRunner(t, replayModel, store), ctx)
	if replayModel.CallCount() != 0 || replayed.Content != final.Content {
		t.Fatalf("replay calls=%d content=%q, want calls=0 content=%q", replayModel.CallCount(), replayed.Content, final.Content)
	}
}

func newA2UIOutputNormalizerTestRunner(t *testing.T, underlying *sequenceChatModel, store modelreceipt.Store) *adk.Runner {
	t.Helper()
	receiptMiddleware, err := NewModelReceiptMiddleware(ModelReceiptMiddlewareConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "a2ui-normalizer-test", Description: "test", Model: underlying,
		Handlers:         []adk.ChatModelAgentMiddleware{newA2UIOutputNormalizerMiddleware(), receiptMiddleware},
		ModelRetryConfig: newA2UIModelRetryConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
}
