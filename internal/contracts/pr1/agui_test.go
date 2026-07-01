package pr1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAGUIFixturesValidate(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(repoRoot(t), "tests/fixtures/contracts/agui/*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("expected AG-UI fixtures")
	}
	for _, path := range fixtures {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var events []AGUIEnvelope
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if err := json.Unmarshal(data, &events); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if err := ValidateAGUISequence(events); err != nil {
				t.Fatalf("fixture violates AG-UI contract: %v", err)
			}
		})
	}
}

func TestBuildAGUIEnvelopeDefaultsContractFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	envelope, err := BuildAGUIEnvelope(AGUIInput{
		EventID:       "evt_run_001",
		EventType:     "agent.run.started",
		ProjectID:     "proj_001",
		SessionID:     "sess_001",
		RunID:         "run_001",
		Seq:           1,
		CreatedAt:     createdAt,
		PayloadDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Payload:       map[string]any{"run_status": RunStatusRouting},
	})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if envelope.SchemaVersion != SchemaVersionAGUIEvent {
		t.Fatalf("schema version = %q", envelope.SchemaVersion)
	}
	if envelope.PayloadSchemaVersion != "agent.run.started.v1" {
		t.Fatalf("payload schema version = %q", envelope.PayloadSchemaVersion)
	}
	if envelope.DedupeKey != "run_001:agent.run.started:1" {
		t.Fatalf("dedupe key = %q", envelope.DedupeKey)
	}
	if envelope.Visibility != VisibilityUserVisible {
		t.Fatalf("visibility = %q", envelope.Visibility)
	}
}

func TestValidateAGUIEnvelopeRejectsDuplicateContractDrift(t *testing.T) {
	envelope := AGUIEnvelope{
		EventID:              "evt_run_001",
		EventType:            "agent.run.started",
		SchemaVersion:        SchemaVersionAGUIEvent,
		PayloadSchemaVersion: "agent.run.started.v1",
		ProjectID:            "proj_001",
		SessionID:            "sess_001",
		RunID:                "run_001",
		Seq:                  1,
		CreatedAt:            time.Now().UTC(),
		Visibility:           VisibilityUserVisible,
		DedupeKey:            "custom",
		Payload:              map[string]any{},
	}
	if err := ValidateAGUIEnvelope(envelope); err == nil {
		t.Fatalf("non-deterministic dedupe_key must be rejected")
	}
}
