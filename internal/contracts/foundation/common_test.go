package foundation

import "testing"

func TestValidateDigest(t *testing.T) {
	valid := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := ValidateDigest(valid); err != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	if err := ValidateDigest("sha256:AAA"); err == nil {
		t.Fatalf("invalid digest accepted")
	}
}

func TestCanonicalDigestIsStable(t *testing.T) {
	first, err := CanonicalDigest(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	second, err := CanonicalDigest(map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if first != second {
		t.Fatalf("canonical digest should be stable, got %s and %s", first, second)
	}
	if err := ValidateDigest(first); err != nil {
		t.Fatalf("computed digest invalid: %v", err)
	}
}

func TestValidateID(t *testing.T) {
	for _, value := range []string{"run_001", "listing:auto-001", "acct-001"} {
		if err := ValidateID(value); err != nil {
			t.Fatalf("valid id %q rejected: %v", value, err)
		}
	}
	if err := ValidateID(" bad"); err == nil {
		t.Fatalf("invalid id accepted")
	}
}
