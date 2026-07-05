package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestToolExceptionMiddlewareWrapInvokableToolCall(t *testing.T) {
	mw := NewToolExceptionMiddleware[*schema.Message]()
	wrapped, err := mw.WrapInvokableToolCall(
		context.Background(),
		func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
			return "", errors.New("message is required")
		},
		&adk.ToolContext{Name: "echo_tool", CallID: "call-1"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall() error = %v", err)
	}
	out, err := wrapped(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("wrapped endpoint should convert tool error to result, got %v", err)
	}
	if !strings.Contains(out, `"status":"error"`) || !strings.Contains(out, `"validation_error"`) {
		t.Fatalf("unexpected wrapped output: %s", out)
	}
}
