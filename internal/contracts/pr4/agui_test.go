package pr4

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

func TestSkillUsageCostDisclosureFromAGUIFixture(t *testing.T) {
	var events []pr1.AGUIEnvelope
	readFixture(t, "tests/fixtures/contracts/agui/paid_marketplace_skill_storyboard_ready.events.json", &events)

	var payload SkillUsageCostDisclosurePayload
	found := false
	for _, event := range events {
		if event.EventType != EventTypeCostDisclosureSkillUsagePresented {
			continue
		}
		body, err := json.Marshal(event.Payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("skill usage cost disclosure event not found")
	}
	if err := ValidateSkillUsageCostDisclosurePayload(payload); err != nil {
		t.Fatalf("fixture violates skill usage disclosure contract: %v", err)
	}
}

func TestSkillUsageCostDisclosureBuildsPR1Envelope(t *testing.T) {
	summary := "创作者不会看到用户私有创作数据。"
	payload := SkillUsageCostDisclosurePayload{
		DisclosureID:                 "disc_skill_usage_001",
		UsageID:                      "usage_001",
		ListingID:                    "listing_auto_storyboard_pro",
		SkillUsageFee:                SkillUsageFee{Points: 120, ChargeTiming: "storyboard_ready", RefundPolicySummary: "未交付前失败会释放冻结。"},
		ToolGenerationFeeNotice:      "后续生成图片、视频或音乐时会另行预估并再次确认。",
		CreatorDataVisibilitySummary: &summary,
		ConfirmationRequired:         true,
		SkillUsageDigest:             "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		PayloadDigest:                "sha256:4444444444444444444444444444444444444444444444444444444444444444",
	}
	if err := ValidateSkillUsageCostDisclosurePayload(payload); err != nil {
		t.Fatalf("validate payload: %v", err)
	}
	envelope, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_paid_market_004",
		EventType:     EventTypeCostDisclosureSkillUsagePresented,
		ProjectID:     "proj_001",
		SessionID:     "sess_001",
		RunID:         "run_001",
		Seq:           4,
		CreatedAt:     time.Date(2026, 7, 1, 0, 0, 4, 0, time.UTC),
		PayloadDigest: payload.PayloadDigest,
		Payload: map[string]any{
			"disclosure_id":                   payload.DisclosureID,
			"usage_id":                        payload.UsageID,
			"listing_id":                      payload.ListingID,
			"skill_usage_fee":                 map[string]any{"points": payload.SkillUsageFee.Points, "charge_timing": payload.SkillUsageFee.ChargeTiming, "refund_policy_summary": payload.SkillUsageFee.RefundPolicySummary},
			"tool_generation_fee_notice":      payload.ToolGenerationFeeNotice,
			"creator_data_visibility_summary": summary,
			"confirmation_required":           payload.ConfirmationRequired,
			"skill_usage_digest":              payload.SkillUsageDigest,
			"payload_digest":                  payload.PayloadDigest,
		},
	})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if envelope.PayloadSchemaVersion != "cost_disclosure.skill_usage.presented.v1" {
		t.Fatalf("payload schema version = %q", envelope.PayloadSchemaVersion)
	}
}
