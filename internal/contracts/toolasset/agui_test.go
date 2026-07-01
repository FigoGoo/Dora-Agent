package toolasset

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

func TestAGUIPayloadsBuildFoundationEnvelope(t *testing.T) {
	status := "value_delivered"
	payload := GenerationCostDisclosurePayload{
		ToolPlanID:       "tpl_city_video_001",
		ToolPlanDigest:   "sha256:1414141414141414141414141414141414141414141414141414141414141414",
		BoardID:          "board_city_tourism_001",
		BoardVersion:     3,
		EstimatedCredits: 120,
		Currency:         CurrencyCredits,
		ExpiresAt:        time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC),
		SkillUsageStatus: &status,
	}
	if err := ValidateGenerationCostDisclosurePayload(payload); err != nil {
		t.Fatalf("validate generation disclosure payload: %v", err)
	}
	envelope, err := foundation.BuildAGUIEnvelope(foundation.AGUIInput{
		EventID:       "evt_generation_cost_001",
		EventType:     EventTypeCostDisclosureGenerationPresented,
		ProjectID:     "proj_city_001",
		SessionID:     "sess_city_001",
		RunID:         "run_city_tourism_001",
		Seq:           1,
		CreatedAt:     time.Date(2026, 7, 1, 2, 20, 0, 0, time.UTC),
		PayloadDigest: payload.ToolPlanDigest,
		Payload: map[string]any{
			"tool_plan_id":       payload.ToolPlanID,
			"tool_plan_digest":   payload.ToolPlanDigest,
			"board_id":           payload.BoardID,
			"board_version":      payload.BoardVersion,
			"estimated_credits":  payload.EstimatedCredits,
			"currency":           payload.Currency,
			"expires_at":         payload.ExpiresAt.Format(time.RFC3339),
			"skill_usage_status": status,
		},
	})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if envelope.PayloadSchemaVersion != "cost_disclosure.generation.presented.v1" {
		t.Fatalf("payload schema version = %q", envelope.PayloadSchemaVersion)
	}
}

func TestToolAndAssetAGUIPayloads(t *testing.T) {
	outputDigest := "sha256:2424242424242424242424242424242424242424242424242424242424242424"
	if err := ValidateToolTaskUpdatedPayload(ToolTaskUpdatedPayload{
		ToolTaskID:   "ttask_city_video_001",
		ToolPlanID:   "tpl_city_video_001",
		Status:       "succeeded",
		Progress:     100,
		OutputDigest: &outputDigest,
		ErrorCode:    nil,
	}); err != nil {
		t.Fatalf("validate tool task updated payload: %v", err)
	}
	if err := ValidateAssetCommitUpdatedPayload(AssetCommitUpdatedPayload{
		ToolTaskID:        "ttask_city_video_001",
		CommitStatus:      "partially_committed",
		CommittedAssetIDs: []string{"asset_city_video_001"},
		FailedAssetCount:  1,
	}); err != nil {
		t.Fatalf("validate asset commit updated payload: %v", err)
	}
}
