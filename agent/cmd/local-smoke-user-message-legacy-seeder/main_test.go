//go:build localsmoke

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

func TestSeedOutputIsSingleLineSafeExactJSON(t *testing.T) {
	result := session.EnsureResult{
		CommandID:   "019f7000-0001-7000-8000-000000000001",
		SessionID:   "019f7000-0002-7000-8000-000000000002",
		MessageID:   stringPointer("019f7000-0003-7000-8000-000000000003"),
		InputID:     stringPointer("019f7000-0004-7000-8000-000000000004"),
		Disposition: session.EnsureDispositionCreated,
	}
	output, err := newSeedOutput(result)
	if err != nil {
		t.Fatalf("构造 legacy seeder 输出失败: %v", err)
	}
	var stdout bytes.Buffer
	if err := json.NewEncoder(&stdout).Encode(output); err != nil {
		t.Fatalf("编码 legacy seeder 输出失败: %v", err)
	}
	raw := stdout.String()
	if strings.Count(raw, "\n") != 1 || !strings.HasSuffix(raw, "\n") {
		t.Fatalf("stdout 不是单行 JSON: %q", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
		t.Fatalf("stdout 不是 JSON: %v", err)
	}
	want := map[string]string{
		"session_id": result.SessionID,
		"input_id":   *result.InputID,
		"message_id": *result.MessageID,
		"command_id": result.CommandID,
	}
	if len(fields) != len(want) {
		t.Fatalf("stdout 字段数=%d want=%d: %s", len(fields), len(want), raw)
	}
	for name, value := range want {
		var actual string
		if err := json.Unmarshal(fields[name], &actual); err != nil || actual != value {
			t.Fatalf("stdout 字段 %s=%q want=%q err=%v", name, actual, value, err)
		}
	}
	lower := strings.ToLower(raw)
	for _, forbidden := range []string{
		"prompt", "initial_prompt", "dsn", "database_url", "password", "secret", "key", "ciphertext", "digest",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("stdout 包含敏感字段或内容 %q: %s", forbidden, raw)
		}
	}
}

func TestIsLocalSmokeAgentDSNExactDatabaseAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{
			name: "production local database",
			dsn:  "postgres://dora_agent_app:local-password@127.0.0.1:15432/dora_agent?sslmode=disable",
			want: true,
		},
		{
			name: "contract local database",
			dsn:  "postgresql://dora_agent_app:local-password@localhost:15432/dora_agent_test?sslmode=disable",
			want: true,
		},
		{
			name: "other database",
			dsn:  "postgres://dora_agent_app:local-password@127.0.0.1:15432/postgres?sslmode=disable",
			want: false,
		},
		{
			name: "other role",
			dsn:  "postgres://postgres:local-password@127.0.0.1:15432/dora_agent?sslmode=disable",
			want: false,
		},
		{
			name: "non loopback",
			dsn:  "postgres://dora_agent_app:local-password@192.0.2.10:15432/dora_agent?sslmode=disable",
			want: false,
		},
		{
			name: "tls not explicit",
			dsn:  "postgres://dora_agent_app:local-password@127.0.0.1:15432/dora_agent",
			want: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isLocalSmokeAgentDSN(test.dsn); got != test.want {
				t.Fatalf("isLocalSmokeAgentDSN()=%t want=%t", got, test.want)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
