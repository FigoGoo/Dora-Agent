package boardgraph

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

func TestAGUIPayloadsBuildFoundationEnvelope(t *testing.T) {
	patch := BoardPatchAppliedPayload{
		BoardID:       "board_city_tourism_001",
		PatchID:       "patch_add_scene_001",
		BaseVersion:   1,
		TargetVersion: 2,
		Operation:     BoardPatchOperationAddElement,
		PatchDigest:   "sha256:6666666666666666666666666666666666666666666666666666666666666666",
	}
	if err := ValidateBoardPatchAppliedPayload(patch); err != nil {
		t.Fatalf("validate patch payload: %v", err)
	}

	envelope, err := foundation.BuildAGUIEnvelope(foundation.AGUIInput{
		EventID:       "evt_board_patch_001",
		EventType:     EventTypeBoardPatchApplied,
		ProjectID:     "proj_city_001",
		SessionID:     "sess_city_001",
		RunID:         "run_city_tourism_001",
		Seq:           1,
		CreatedAt:     time.Date(2026, 7, 1, 2, 1, 0, 0, time.UTC),
		PayloadDigest: patch.PatchDigest,
		Payload: map[string]any{
			"board_id":       patch.BoardID,
			"patch_id":       patch.PatchID,
			"base_version":   patch.BaseVersion,
			"target_version": patch.TargetVersion,
			"operation":      patch.Operation,
			"patch_digest":   patch.PatchDigest,
		},
	})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if envelope.PayloadSchemaVersion != "board.patch.applied.v1" {
		t.Fatalf("payload schema version = %q", envelope.PayloadSchemaVersion)
	}
	if envelope.DedupeKey != "run_city_tourism_001:board.patch.applied:1" {
		t.Fatalf("dedupe_key = %q", envelope.DedupeKey)
	}
}

func TestAGUIPayloadValidators(t *testing.T) {
	if err := ValidateBoardSnapshotUpdatedPayload(BoardSnapshotUpdatedPayload{
		BoardID:           "board_city_tourism_001",
		BoardVersion:      2,
		BoardStatus:       "ready",
		BoardDigest:       "sha256:8888888888888888888888888888888888888888888888888888888888888888",
		ChangedElementIDs: []string{"elem_city_scene_002"},
		SnapshotRequired:  true,
	}); err != nil {
		t.Fatalf("validate board snapshot payload: %v", err)
	}

	if err := ValidateGraphPlanCreatedPayload(GraphPlanCreatedPayload{
		GraphPlanID:         "gplan_generic_city_001",
		GraphTemplateID:     "gtemplate_generic_creation",
		GraphPlanStatus:     "compiled",
		GraphPlanDigest:     "sha256:1212121212121212121212121212121212121212121212121212121212121212",
		BoardID:             "board_city_tourism_001",
		ValueDeliveredStage: ValueDeliveredStageStoryboardReady,
	}); err != nil {
		t.Fatalf("validate graph plan payload: %v", err)
	}

	outputDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if err := ValidateGraphNodeUpdatedPayload(GraphNodeUpdatedPayload{
		GraphPlanID:  "gplan_generic_city_001",
		NodeID:       "brief_parser",
		NodeType:     GraphNodeTypeBriefParser,
		NodeStatus:   GraphPlanNodeStatusCompleted,
		Progress:     100,
		OutputDigest: &outputDigest,
	}); err != nil {
		t.Fatalf("validate graph node payload: %v", err)
	}
}
