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
	ctx = WithRequestID(ctx, "req-business")

	log.InfoContext(ctx, "business", FieldTraceID, TraceID(ctx), FieldRequestID, RequestID(ctx), "secret_access_key", "secret")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log json: %v", err)
	}
	if entry[FieldService] != "business" || entry[FieldEnv] != "test" || entry[FieldTraceID] != "trace-business" || entry[FieldRequestID] != "req-business" {
		t.Fatalf("missing fields: %#v", entry)
	}
	if entry["secret_access_key"] != "[REDACTED]" {
		t.Fatalf("secret was not redacted: %#v", entry)
	}
}

func TestLoggerFieldSetsAreExplicit(t *testing.T) {
	for _, field := range []string{FieldService, FieldEnv, FieldTraceID, FieldRequestID, FieldMethod, FieldPath, FieldStatus, FieldLatencyMS} {
		if field == "" {
			t.Fatal("logger field constants must not be empty")
		}
	}
	assertContains(t, BaseFields, FieldService)
	assertContains(t, BaseFields, FieldEnv)
	for _, field := range []string{FieldTraceID, FieldRequestID, FieldMethod, FieldPath, FieldStatus, FieldLatencyMS} {
		assertContains(t, HTTPRequestFields, field)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %#v", want, values)
}
