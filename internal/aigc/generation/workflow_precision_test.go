package generation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func precisionWorkflowCommand(operationID, batchID, key, number string) CreateWorkflowCommand {
	command := testWorkflowCommand(operationID, batchID, []GenerationJob{{
		ID: "job-" + operationID, IdempotencyKey: "job-key-" + operationID,
		Provider: "mock", MediaKind: "image", Payload: map[string]any{"seed": json.Number(number)},
	}})
	command.Operation.IdempotencyKey = key
	return command
}

func TestMemoryWorkflowPreservesLargePayloadNumber(t *testing.T) {
	store := NewMemoryStore()
	command := precisionWorkflowCommand("operation-precision", "batch-precision", "precision-key", "9007199254740993")
	aggregate, _, err := store.CreateWorkflow(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(context.Background(), aggregate.Jobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	seed, ok := job.Payload["seed"].(json.Number)
	if !ok || seed.String() != "9007199254740993" {
		t.Fatalf("seed lost precision: %T(%v)", job.Payload["seed"], job.Payload["seed"])
	}
}

func TestWorkflowFingerprintRejectsAdjacentLargeNumberReplay(t *testing.T) {
	store := NewMemoryStore()
	first := precisionWorkflowCommand("operation-first", "batch-first", "shared-key", "9007199254740992")
	if _, _, err := store.CreateWorkflow(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := precisionWorkflowCommand("operation-second", "batch-second", "shared-key", "9007199254740993")
	if _, _, err := store.CreateWorkflow(context.Background(), second); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("adjacent large-number replay error = %v", err)
	}
}

func TestDecodeWorkflowJobPreservesLargePayloadNumber(t *testing.T) {
	record := workflowJobRecord{ID: "job-pg", Data: []byte(`{"id":"job-pg","payload":{"seed":9007199254740993}}`)}
	job, err := decodeWorkflowJob(record)
	if err != nil {
		t.Fatal(err)
	}
	seed, ok := job.Payload["seed"].(json.Number)
	if !ok || seed.String() != "9007199254740993" {
		t.Fatalf("seed lost precision: %T(%v)", job.Payload["seed"], job.Payload["seed"])
	}
}

func TestDecodeLegacyGenerationJobPreservesLargePayloadNumber(t *testing.T) {
	job, err := fromRecord(jobRecord{ID: "legacy-job", Payload: []byte(`{"seed":9007199254740993}`)})
	if err != nil {
		t.Fatal(err)
	}
	seed, ok := job.Payload["seed"].(json.Number)
	if !ok || seed.String() != "9007199254740993" {
		t.Fatalf("seed lost precision: %T(%v)", job.Payload["seed"], job.Payload["seed"])
	}
}

func TestDecodeGenerationJSONRejectsTrailingValue(t *testing.T) {
	var value map[string]any
	if err := decodeGenerationJSON([]byte(`{"seed":1} {"seed":2}`), &value); err == nil {
		t.Fatal("expected trailing JSON value to be rejected")
	}
}
