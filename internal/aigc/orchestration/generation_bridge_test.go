package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

type recordingWorkflowStore struct {
	generation.WorkflowStore
	commands []generation.CreateWorkflowCommand
}

func (s *recordingWorkflowStore) CreateWorkflow(ctx context.Context, command generation.CreateWorkflowCommand) (generation.WorkflowAggregate, bool, error) {
	s.commands = append(s.commands, command)
	return s.WorkflowStore.CreateWorkflow(ctx, command)
}

func TestGenerationBridgeCreatesOneAtomicDeliverableWorkflow(t *testing.T) {
	store := &recordingWorkflowStore{WorkflowStore: generation.NewMemoryStore()}
	nextID := 0
	commands := generation.NewCommandService(generation.CommandServiceConfig{
		Store: store,
		NewID: func() string {
			nextID++
			return fmt.Sprintf("generated-%d", nextID)
		},
	})
	bridge := NewGenerationBridge(commands)

	result, err := bridge.Dispatch(context.Background(), vocabulary.GenerationDispatchRequest{
		SessionID: "session-1", UserID: "user-1", PlanRunID: "run-1", NodeID: "dispatch", Attempt: 2,
		IdempotencyKey: "plan:run-1:dispatch:2",
		Inputs: map[string]any{
			"media_kind": "image",
			"ratio":      "16:9",
			"targets": []any{
				map[string]any{"prompt": "wide establishing shot", "seed": 9007199254740993},
				map[string]any{"prompt": "close-up", "media_kind": "video"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.commands) != 1 {
		t.Fatalf("CreateWorkflow calls = %d, want 1", len(store.commands))
	}
	command := store.commands[0]
	if command.Operation.WorkflowRunID != "run-1" || command.Operation.StageRunID != "dispatch" || command.Operation.IdempotencyKey != "plan:run-1:dispatch:2" {
		t.Fatalf("operation mapping = %+v", command.Operation)
	}
	if command.Batch.WorkflowRunID != "run-1" || command.Batch.StageRunID != "dispatch" || command.Batch.Kind != generation.TargetKindSessionDeliverable {
		t.Fatalf("batch mapping = %+v", command.Batch)
	}
	if len(command.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(command.Jobs))
	}
	for index, job := range command.Jobs {
		if job.WorkflowRunID != "run-1" || job.StageRunID != "dispatch" || job.TargetType != generation.TargetKindSessionDeliverable || job.BindingToken.NormalizedKind() != generation.TargetKindSessionDeliverable {
			t.Fatalf("job[%d] mapping = %+v", index, job)
		}
		if job.BindingToken.StoryboardID != "" || job.BindingToken.AssetSlot != "primary" || job.BindingToken.InputFingerprint == "" {
			t.Fatalf("job[%d] token = %+v", index, job.BindingToken)
		}
	}
	if seed, ok := command.Jobs[0].Payload["seed"].(json.Number); !ok || seed.String() != "9007199254740993" {
		t.Fatalf("large seed lost precision: %#v", command.Jobs[0].Payload["seed"])
	}
	if result.OperationID != "generated-1" || result.BatchID != "generated-2" || strings.Join(result.JobIDs, ",") != "generated-3,generated-4" {
		t.Fatalf("dispatch result = %+v", result)
	}
	storedOperation, err := store.GetOperation(context.Background(), result.OperationID)
	if err != nil || storedOperation.RequestFingerprint == "" {
		t.Fatalf("stored operation = %+v err=%v", storedOperation, err)
	}
}

func TestGenerationBridgeDefaultsPromptOnlyTargetToImage(t *testing.T) {
	store := generation.NewMemoryStore()
	commands := generation.NewCommandService(generation.CommandServiceConfig{Store: store})
	result, err := NewGenerationBridge(commands).Dispatch(context.Background(), vocabulary.GenerationDispatchRequest{
		SessionID: "session-1", UserID: "user-1", PlanRunID: "run-1", NodeID: "dispatch", Attempt: 1,
		IdempotencyKey: "plan:run-1:dispatch:1",
		Inputs:         map[string]any{"targets": []any{map[string]any{"prompt": "cinematic rain"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(context.Background(), result.JobIDs[0])
	if err != nil || job.MediaKind != "image" || job.Provider != generation.ProviderImage2 {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestGenerationBridgeFingerprintCoversPlanNodeTargetAndAttempt(t *testing.T) {
	base := vocabulary.GenerationDispatchRequest{
		SessionID: "session-1", UserID: "user-1", PlanRunID: "run-1", NodeID: "dispatch", Attempt: 1,
		IdempotencyKey: "stable-key",
		Inputs:         map[string]any{"media_kind": "image", "targets": []any{map[string]any{"prompt": "first"}}},
	}
	fingerprint := func(request vocabulary.GenerationDispatchRequest) string {
		t.Helper()
		store := generation.NewMemoryStore()
		commands := generation.NewCommandService(generation.CommandServiceConfig{Store: store})
		result, err := NewGenerationBridge(commands).Dispatch(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		operation, err := store.GetOperation(context.Background(), result.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		return operation.RequestFingerprint
	}
	wantDifferent := []vocabulary.GenerationDispatchRequest{
		func() vocabulary.GenerationDispatchRequest {
			changed := base
			changed.PlanRunID = "run-2"
			return changed
		}(),
		func() vocabulary.GenerationDispatchRequest { changed := base; changed.NodeID = "other"; return changed }(),
		func() vocabulary.GenerationDispatchRequest { changed := base; changed.Attempt = 2; return changed }(),
		func() vocabulary.GenerationDispatchRequest {
			changed := base
			changed.Inputs = map[string]any{"media_kind": "image", "targets": []any{map[string]any{"prompt": "second"}}}
			return changed
		}(),
	}
	baseFingerprint := fingerprint(base)
	for index, changed := range wantDifferent {
		if got := fingerprint(changed); got == baseFingerprint {
			t.Fatalf("variant %d did not change request fingerprint %q", index, got)
		}
	}
}
