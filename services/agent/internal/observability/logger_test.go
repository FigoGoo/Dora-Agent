package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestLoggerAddsServiceEnvTraceAndRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, "agent", "test", "debug")
	ctx := WithTraceID(t.Context(), "trace-agent")

	args := attrsToArgs(AttrsFromContext(ctx))
	args = append(args, "api_key", "secret-value")
	log.InfoContext(ctx, "hello", args...)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log json: %v", err)
	}
	if entry["service"] != "agent" || entry["env"] != "test" || entry["trace_id"] != "trace-agent" {
		t.Fatalf("missing structured attrs: %#v", entry)
	}
	if entry["api_key"] != "[REDACTED]" {
		t.Fatalf("secret was not redacted: %#v", entry)
	}
}

func attrsToArgs(attrs []slog.Attr) []any {
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	return args
}
