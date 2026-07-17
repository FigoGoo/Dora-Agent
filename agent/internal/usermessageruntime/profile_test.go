package usermessageruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestApprovedSessionProfileCanonicalDigests 防止 canonical 文本、摘要与 Session Writer pins 静默漂移。
func TestApprovedSessionProfileCanonicalDigests(t *testing.T) {
	tests := []struct {
		name, canonical, digest string
	}{
		{"prompt", PromptCanonical, PromptDigest},
		{"empty tool registry", EmptyToolRegistryCanonical, EmptyToolRegistryDigest},
		{"runtime policy", RuntimePolicyCanonical, RuntimePolicyDigest},
		{"model route", LocalFakeModelRouteCanonical, LocalFakeModelRouteDigest},
		{"budget", BudgetCanonical, BudgetDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest := sha256.Sum256([]byte(test.canonical))
			if got := hex.EncodeToString(digest[:]); got != test.digest {
				t.Fatalf("canonical digest=%s want=%s", got, test.digest)
			}
		})
	}
	profile := ApprovedSessionProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("批准 Session Profile 未通过领域校验: %v", err)
	}
	if !profile.Enabled || profile.Profile != Profile || profile.PromptDigest != PromptDigest ||
		profile.ToolRegistryDigest != EmptyToolRegistryDigest || profile.ModelRouteDigest != LocalFakeModelRouteDigest {
		t.Fatalf("批准 Session Profile pins 漂移: %+v", profile)
	}
}
