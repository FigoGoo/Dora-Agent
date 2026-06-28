package idempotency_test

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
)

func TestHashRequestCanonicalizesJSONAndIgnoresObservabilityFields(t *testing.T) {
	inputA := idempotency.RequestHashInput{
		TenantID:    "tenant_1",
		SpaceID:     "space_1",
		ActorUserID: "user_1",
		Body:        []byte(`{"trace_id":"trace-a","request_id":"req-a","name":"demo","items":[{"id":"b","qty":2},{"qty":1,"id":"a"}]}`),
		Extra:       map[string]any{"X-Client-Request-ID": "client-a", "target_status": "active"},
	}
	inputB := idempotency.RequestHashInput{
		TenantID:    "tenant_1",
		SpaceID:     "space_1",
		ActorUserID: "user_1",
		Body:        []byte(`{"items":[{"qty":2,"id":"b"},{"id":"a","qty":1}],"name":"demo","request_id":"req-b","trace_id":"trace-b"}`),
		Extra:       map[string]any{"target_status": "active", "x_client_request_id": "client-b"},
	}

	hashA := mustHash(t, inputA)
	hashB := mustHash(t, inputB)
	if hashA != hashB {
		t.Fatalf("expected canonical hashes to match: %s != %s", hashA, hashB)
	}

	inputB.SpaceID = "space_2"
	if hashA == mustHash(t, inputB) {
		t.Fatal("expected space id to participate in request hash")
	}

	inputB.SpaceID = "space_1"
	inputB.ActorUserID = "user_2"
	if hashA == mustHash(t, inputB) {
		t.Fatal("expected actor user id to participate in request hash")
	}
}

func TestHashRequestRequiresTenantAndActor(t *testing.T) {
	if _, err := idempotency.HashRequest(idempotency.RequestHashInput{ActorUserID: "user_1", Body: []byte(`{}`)}); err == nil {
		t.Fatal("expected tenant requirement")
	}
	if _, err := idempotency.HashRequest(idempotency.RequestHashInput{TenantID: "tenant_1", Body: []byte(`{}`)}); err == nil {
		t.Fatal("expected actor requirement")
	}
}

func mustHash(t *testing.T, input idempotency.RequestHashInput) string {
	t.Helper()
	hash, err := idempotency.HashRequest(input)
	if err != nil {
		t.Fatalf("hash request: %v", err)
	}
	return hash
}
