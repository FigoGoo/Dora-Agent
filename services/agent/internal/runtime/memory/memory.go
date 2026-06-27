package memory

import (
	"encoding/json"
	"strings"
)

type Policy struct {
	Enabled       bool     `json:"enabled"`
	WriteScopes   []string `json:"write_scopes"`
	ReadScopes    []string `json:"read_scopes"`
	RetentionDays int      `json:"retention_days"`
}

type Decision struct {
	Enabled       bool
	ReadScopes    []string
	WriteScopes   []string
	RetentionDays int
	Reason        string
}

func Decide(policyJSON string) Decision {
	policy := Policy{Enabled: true, RetentionDays: 30}
	if strings.TrimSpace(policyJSON) != "" {
		_ = json.Unmarshal([]byte(policyJSON), &policy)
	}
	if !policy.Enabled {
		return Decision{Enabled: false, Reason: "memory_disabled_by_skill_policy"}
	}
	return Decision{
		Enabled: true, ReadScopes: compact(policy.ReadScopes), WriteScopes: compact(policy.WriteScopes),
		RetentionDays: policy.RetentionDays, Reason: "memory_policy_allowed",
	}
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
