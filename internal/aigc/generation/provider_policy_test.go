package generation

import (
	"context"
	"errors"
	"testing"
)

func TestValidateProviderJobBoundsLLMControlledParameters(t *testing.T) {
	valid := GenerationJob{Provider: ProviderImage2, MediaKind: "image", Payload: map[string]any{"prompt": "cat", "model": DefaultImageModel, "size": "1024x1024", "n": 4}}
	if err := ValidateProviderJob(valid); err != nil {
		t.Fatalf("ValidateProviderJob(valid) error = %v", err)
	}
	invalid := valid
	invalid.Payload = cloneMap(valid.Payload)
	invalid.Payload["n"] = 1000
	if err := ValidateProviderJob(invalid); err == nil {
		t.Fatal("unbounded image variants were accepted")
	}
	video := GenerationJob{Provider: ProviderSeedance, MediaKind: "video", Payload: map[string]any{"prompt": "scene", "model": DefaultVideoModel, "ratio": "9:16", "resolution": "720p", "duration_seconds": 31}}
	if err := ValidateProviderJob(video); err == nil {
		t.Fatal("unbounded video duration was accepted")
	}
}

func TestValidateProviderJobRejectsStringIntegersThatHandlersCannotDecode(t *testing.T) {
	tests := []GenerationJob{
		{Provider: ProviderImage2, Payload: map[string]any{"prompt": "cat", "n": "2"}},
		{Provider: ProviderImage2, Payload: map[string]any{"prompt": "cat", "n": ""}},
		{Provider: ProviderImage2, Payload: map[string]any{"prompt": "cat", "n": "  "}},
		{Provider: ProviderSeedance, Payload: map[string]any{"prompt": "scene", "duration_seconds": "5"}},
		{Provider: ProviderSeedance, Payload: map[string]any{"prompt": "scene", "duration_seconds": "<nil>"}},
		{Provider: ProviderSeedance, Payload: map[string]any{"prompt": "scene", "fps": "24"}},
	}
	for _, job := range tests {
		if err := ValidateProviderJob(job); err == nil {
			t.Fatalf("ValidateProviderJob(%s, %#v) accepted a string in an integer field", job.Provider, job.Payload)
		}
	}
}

func TestWorkflowFingerprintUsesInheritedJobDeliveryPolicy(t *testing.T) {
	active := DeliveryPolicy{BindingMode: BindingModeActive, ApprovalPolicy: ApprovalAutoApprove, ChargePolicy: ChargePostpaidNoReservation}
	candidate := DeliveryPolicy{BindingMode: BindingModeCandidate, ApprovalPolicy: ApprovalReviewRequired, ChargePolicy: ChargePostpaidNoReservation}

	inherited := testWorkflowCommand("op-inherited", "batch-inherited", []GenerationJob{{ID: "job-inherited", IdempotencyKey: "job-inherited", Provider: "mock"}})
	inherited.Batch.DeliveryPolicy = active
	if err := freezeWorkflowRequest(&inherited); err != nil {
		t.Fatal(err)
	}

	explicitInherited := testWorkflowCommand("op-explicit", "batch-explicit", []GenerationJob{{ID: "job-explicit", IdempotencyKey: "job-explicit", Provider: "mock", DeliveryPolicy: active}})
	explicitInherited.Batch.DeliveryPolicy = active
	if err := freezeWorkflowRequest(&explicitInherited); err != nil {
		t.Fatal(err)
	}
	if inherited.Operation.RequestFingerprint != explicitInherited.Operation.RequestFingerprint {
		t.Fatalf("inherited policy fingerprint %q differs from equivalent explicit policy %q", inherited.Operation.RequestFingerprint, explicitInherited.Operation.RequestFingerprint)
	}

	explicitCandidate := testWorkflowCommand("op-candidate", "batch-candidate", []GenerationJob{{ID: "job-candidate", IdempotencyKey: "job-candidate", Provider: "mock", DeliveryPolicy: candidate}})
	explicitCandidate.Batch.DeliveryPolicy = active
	if err := freezeWorkflowRequest(&explicitCandidate); err != nil {
		t.Fatal(err)
	}
	if inherited.Operation.RequestFingerprint == explicitCandidate.Operation.RequestFingerprint {
		t.Fatal("different effective job delivery policies produced the same request fingerprint")
	}
}

func TestLegacyProviderAdapterClassifiesRetryability(t *testing.T) {
	transient := NewJobHandlerProviderAdapter(JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
		return HandlerResult{}, errors.New("upload object: connection reset by peer")
	}))
	_, err := transient.Submit(context.Background(), GenerationJob{})
	var execution *ExecutionError
	if !errors.As(err, &execution) || !execution.Retryable {
		t.Fatalf("transient error = %#v", err)
	}

	permanent := NewJobHandlerProviderAdapter(JobHandlerFunc(func(context.Context, GenerationJob) (HandlerResult, error) {
		return HandlerResult{}, errors.New("prompt is required")
	}))
	_, err = permanent.Submit(context.Background(), GenerationJob{})
	if !errors.As(err, &execution) || execution.Retryable {
		t.Fatalf("permanent error = %#v", err)
	}
}

func TestWorkflowIdempotencyKeyRejectsCrossSessionReplay(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first := testWorkflowCommand("op-a", "batch-a", []GenerationJob{{ID: "job-a", IdempotencyKey: "job-key", Provider: "mock", Required: true}})
	first.Operation.IdempotencyKey = "shared-key"
	if _, _, err := store.CreateWorkflow(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := testWorkflowCommand("op-b", "batch-b", []GenerationJob{{ID: "job-b", IdempotencyKey: "job-key", Provider: "mock", Required: true}})
	second.Operation.IdempotencyKey = "shared-key"
	second.Operation.SessionID = "another-session"
	second.Batch.SessionID = "another-session"
	if _, _, err := store.CreateWorkflow(ctx, second); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("CreateWorkflow(cross-session) error = %v", err)
	}
}
