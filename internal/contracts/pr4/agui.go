package pr4

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const EventTypeCostDisclosureSkillUsagePresented = "cost_disclosure.skill_usage.presented"

type SkillUsageCostDisclosurePayload struct {
	DisclosureID                 string        `json:"disclosure_id"`
	UsageID                      string        `json:"usage_id"`
	ListingID                    string        `json:"listing_id"`
	SkillUsageFee                SkillUsageFee `json:"skill_usage_fee"`
	ToolGenerationFeeNotice      string        `json:"tool_generation_fee_notice"`
	CreatorDataVisibilitySummary *string       `json:"creator_data_visibility_summary"`
	ConfirmationRequired         bool          `json:"confirmation_required"`
	SkillUsageDigest             string        `json:"skill_usage_digest"`
	PayloadDigest                string        `json:"payload_digest"`
}

type SkillUsageFee struct {
	Points              int    `json:"points"`
	ChargeTiming        string `json:"charge_timing"`
	RefundPolicySummary string `json:"refund_policy_summary"`
}

func ValidateSkillUsageCostDisclosurePayload(payload SkillUsageCostDisclosurePayload) error {
	if strings.TrimSpace(payload.DisclosureID) == "" || strings.TrimSpace(payload.UsageID) == "" {
		return errors.New("disclosure_id and usage_id are required")
	}
	if err := validatePrefixID(payload.ListingID, "listing_"); err != nil {
		return fmt.Errorf("listing_id: %w", err)
	}
	if payload.SkillUsageFee.Points < 0 {
		return errors.New("skill_usage_fee.points must be >= 0")
	}
	if strings.TrimSpace(payload.SkillUsageFee.ChargeTiming) == "" || strings.TrimSpace(payload.SkillUsageFee.RefundPolicySummary) == "" {
		return errors.New("charge_timing and refund_policy_summary are required")
	}
	if strings.TrimSpace(payload.ToolGenerationFeeNotice) == "" {
		return errors.New("tool_generation_fee_notice is required")
	}
	if !payload.ConfirmationRequired {
		return errors.New("confirmation_required must be true")
	}
	if err := pr1.ValidateDigest(payload.SkillUsageDigest); err != nil {
		return fmt.Errorf("skill_usage_digest: %w", err)
	}
	if payload.PayloadDigest != "" {
		if err := pr1.ValidateDigest(payload.PayloadDigest); err != nil {
			return fmt.Errorf("payload_digest: %w", err)
		}
	}
	return nil
}
