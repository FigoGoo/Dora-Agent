package agentidentity

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

func TestSignerMatchesFrozenVector(t *testing.T) {
	key, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	nonce := make([]byte, 16)
	for index := range nonce {
		nonce[index] = byte(index)
	}
	signer, err := NewSigner(fixedClock{value: time.UnixMilli(1784011500123)}, bytes.NewReader(nonce), Config{
		KeyVersion: "test-2026-07-a", Secret: key, TTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	assertion, err := signer.Sign(Identity{
		RequestID:       "019f0000-0000-7000-8000-000000000001",
		CanonicalTarget: "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42",
		PrincipalUserID: "019f0000-0000-7000-8000-000000000002",
		WebSessionID:    "019f0000-0000-7000-8000-000000000003", WebSessionVersion: 7,
		ProjectID:      "019f0000-0000-7000-8000-000000000004",
		AgentSessionID: "019f0000-0000-7000-8000-000000000005", Scope: ScopeEventsRead,
	})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if assertion.KeyVersion != "test-2026-07-a" || assertion.Signature != "a7bd082fd06e94d0e09eff76608f240dde6390692c714c9e23fc4983c736c374" {
		t.Fatalf("frozen vector mismatch: %+v", assertion)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(assertion.EncodedCanonical)
	if err != nil {
		t.Fatalf("decode assertion: %v", err)
	}
	want := "agent_http_identity_assertion.v1\ndora-business-service\ndora.agent.http.v1\ntest-2026-07-a\n019f0000-0000-7000-8000-000000000001\nGET\n/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42\n019f0000-0000-7000-8000-000000000002\n019f0000-0000-7000-8000-000000000003\n7\n019f0000-0000-7000-8000-000000000004\n019f0000-0000-7000-8000-000000000005\nagent.session.events.read\n1784011500123\n1784011530123\nAAECAwQFBgcICQoLDA0ODw"
	if string(decoded) != want {
		t.Fatalf("canonical mismatch:\n%s", decoded)
	}
}

func TestSignerRejectsCrossPathAndRandomFailure(t *testing.T) {
	secret := bytes.Repeat([]byte{1}, 32)
	identity := Identity{
		RequestID:       "019f0000-0000-7000-8000-000000000001",
		CanonicalTarget: "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/workspace",
		PrincipalUserID: "019f0000-0000-7000-8000-000000000002",
		WebSessionID:    "019f0000-0000-7000-8000-000000000003", WebSessionVersion: 1,
		ProjectID:      "019f0000-0000-7000-8000-000000000004",
		AgentSessionID: "019f0000-0000-7000-8000-000000000005", Scope: ScopeEventsRead,
	}
	signer, _ := NewSigner(fixedClock{value: time.UnixMilli(1784011500123)}, bytes.NewReader(make([]byte, 16)), Config{
		KeyVersion: "active", Secret: secret, TTL: 30 * time.Second,
	})
	if _, err := signer.Sign(identity); !errors.Is(err, ErrInvalidAssertionInput) {
		t.Fatalf("cross path error = %v", err)
	}
	identity.Scope = ScopeWorkspaceRead
	signer, _ = NewSigner(fixedClock{value: time.UnixMilli(1784011500123)}, bytes.NewReader(nil), Config{
		KeyVersion: "active", Secret: secret, TTL: 30 * time.Second,
	})
	if _, err := signer.Sign(identity); !errors.Is(err, ErrAssertionSigningUnavailable) {
		t.Fatalf("random failure error = %v", err)
	}
}

func TestNewSignerRejectsUnsafeRotationConfig(t *testing.T) {
	valid := Config{KeyVersion: "active", Secret: bytes.Repeat([]byte{1}, 32), TTL: 30 * time.Second}
	tests := []Config{
		{KeyVersion: "bad\nkey", Secret: valid.Secret, TTL: valid.TTL},
		{KeyVersion: "Uppercase", Secret: valid.Secret, TTL: valid.TTL},
		{KeyVersion: "-leading-separator", Secret: valid.Secret, TTL: valid.TTL},
		{KeyVersion: valid.KeyVersion, Secret: []byte("short"), TTL: valid.TTL},
		{KeyVersion: valid.KeyVersion, Secret: valid.Secret, TTL: 61 * time.Second},
	}
	for _, test := range tests {
		if _, err := NewSigner(fixedClock{value: time.Now()}, bytes.NewReader(make([]byte, 16)), test); err == nil {
			t.Fatalf("expected config rejection: kid=%q secret_bytes=%d ttl=%s", test.KeyVersion, len(test.Secret), test.TTL)
		}
	}
}
