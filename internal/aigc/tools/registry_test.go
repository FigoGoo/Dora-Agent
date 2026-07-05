package tools

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	if err := registry.Register("echo_tool", EchoTool{}, ToolMeta{Category: "demo"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register("echo_tool", EchoTool{}, ToolMeta{}); !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}

	if registry.Len() != 1 {
		t.Fatalf("registry length = %d", registry.Len())
	}
	tools := registry.ListByKeys([]string{"missing", "echo_tool"})
	if len(tools) != 1 {
		t.Fatalf("ListByKeys() len = %d", len(tools))
	}

	summaries, err := registry.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "echo_tool" || summaries[0].Key != "echo_tool" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
}

func TestEchoTool(t *testing.T) {
	out, err := EchoTool{}.InvokableRun(context.Background(), `{"message":"hello"}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if out != `{"message":"hello"}` {
		t.Fatalf("unexpected output: %s", out)
	}
}
