//go:build localsmoke

package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAuthorityOutputIsExactSafeJSON(t *testing.T) {
	wantCounts := authorityCounts{
		InputCount: 1, RunCount: 1, ContextCount: 1, ContextPinsValid: true, ContextDigestsValid: true,
		ModelReceiptCount: 2, RouterModelReceiptCount: 1, GraphModelReceiptCount: 1,
		ToolReceiptCount: 1, ProjectionCount: 1, AcceptedEventCount: 1, TerminalEventCount: 1,
		EventHighWatermark: 3,
	}
	var buffer bytes.Buffer
	if err := encodeExactJSON(&buffer, authorityOutput{SchemaVersion: authoritySchemaVersion, Mode: "authority", Counts: wantCounts}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buffer.String(), "\n") != 1 {
		t.Fatalf("stdout is not one JSON line: %q", buffer.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(buffer.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if got, want := sortedMapKeys(fields), []string{"counts", "mode", "schema_version"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields=%v want=%v", got, want)
	}
	lower := strings.ToLower(buffer.String())
	for _, forbidden := range []string{"password", "database_url", "ciphertext", "evidence_content", "provider_payload", "reasoning", "secret"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("safe output contains %q: %s", forbidden, buffer.String())
		}
	}
}

func TestProbeOutputIsExactSafeJSON(t *testing.T) {
	var buffer bytes.Buffer
	if err := encodeExactJSON(&buffer, probeOutput{authoritySchemaVersion, "probe", true, true}); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(buffer.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if got, want := sortedMapKeys(fields), []string{"mode", "postgresql_direct", "redis_direct", "schema_version"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields=%v want=%v", got, want)
	}
}

func TestLocalAnalyzeMaterialsAgentDSN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{"canonical", "postgres://dora_agent_app:password@127.0.0.1:15432/dora_agent_test?sslmode=disable", true},
		{"production database", "postgres://dora_agent_app:password@127.0.0.1:15432/dora_agent?sslmode=disable", false},
		{"admin role", "postgres://dora_admin:password@127.0.0.1:15432/dora_agent_test?sslmode=disable", false},
		{"wrong port", "postgres://dora_agent_app:password@127.0.0.1:5432/dora_agent_test?sslmode=disable", false},
		{"nonloopback", "postgres://dora_agent_app:password@192.0.2.1:15432/dora_agent_test?sslmode=disable", false},
		{"tls absent", "postgres://dora_agent_app:password@127.0.0.1:15432/dora_agent_test", false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isLocalAnalyzeMaterialsAgentDSN(test.dsn); got != test.want {
				t.Fatalf("isLocalAnalyzeMaterialsAgentDSN()=%t want=%t", got, test.want)
			}
		})
	}
}

func TestAuthorityIdentityRejectsNonUUIDv7(t *testing.T) {
	t.Setenv("DORA_SMOKE_SESSION_ID", "019f1000-0000-7000-8000-000000000001")
	t.Setenv("DORA_SMOKE_INPUT_ID", "019f1000-0000-7000-8000-000000000002")
	t.Setenv("DORA_SMOKE_TURN_ID", "019f1000-0000-4000-8000-000000000003")
	t.Setenv("DORA_SMOKE_RUN_ID", "019f1000-0000-7000-8000-000000000004")
	t.Setenv("DORA_SMOKE_TOOL_CALL_ID", "019f1000-0000-7000-8000-000000000005")
	if _, err := authorityIdentityFromEnvironment(); err == nil {
		t.Fatal("UUIDv4 was accepted")
	}
}

func sortedMapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for index := 1; index < len(keys); index++ {
		for current := index; current > 0 && keys[current] < keys[current-1]; current-- {
			keys[current], keys[current-1] = keys[current-1], keys[current]
		}
	}
	return keys
}
