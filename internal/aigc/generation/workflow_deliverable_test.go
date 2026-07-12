package generation

import (
	"context"
	"strings"
	"testing"
)

func TestCreateWorkflowValidatesDeliverableToken(t *testing.T) {
	store := NewMemoryStore()
	command := testWorkflowCommand("op-d1", "batch-d1", []GenerationJob{{
		ID: "job-d1", IdempotencyKey: "job-key-d1", Provider: "mock", Required: true,
		// AssetSlot 与 InputFingerprint 缺失：deliverable token 必须在创建时被拒，
		// 不能因 StoryboardID 为空而跳过校验。
		BindingToken: BindingToken{TargetKind: TargetKindSessionDeliverable, TargetID: "deliverable:x"},
	}})
	if _, _, err := store.CreateWorkflow(context.Background(), command); err == nil || !strings.Contains(err.Error(), "asset slot") {
		t.Fatalf("deliverable token without asset slot must be rejected at creation, got %v", err)
	}

	valid := testWorkflowCommand("op-d2", "batch-d2", []GenerationJob{{
		ID: "job-d2", IdempotencyKey: "job-key-d2", Provider: "mock", Required: true,
		BindingToken: BindingToken{TargetKind: TargetKindSessionDeliverable, TargetID: "deliverable:y", AssetSlot: "primary", InputFingerprint: "fp"},
	}})
	if _, _, err := store.CreateWorkflow(context.Background(), valid); err != nil {
		t.Fatalf("valid deliverable job must be accepted: %v", err)
	}
}
