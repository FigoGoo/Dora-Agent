package logger

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLoggerRedactsSecretsAndAddsBaseFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "business", "test", "debug")
	ctx := WithTraceID(t.Context(), "trace-business")

	log.InfoContext(ctx, "business", "trace_id", TraceID(ctx), "secret_access_key", "secret")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log json: %v", err)
	}
	if entry["service"] != "business" || entry["env"] != "test" || entry["trace_id"] != "trace-business" {
		t.Fatalf("missing fields: %#v", entry)
	}
	if entry["secret_access_key"] != "[REDACTED]" {
		t.Fatalf("secret was not redacted: %#v", entry)
	}
}
