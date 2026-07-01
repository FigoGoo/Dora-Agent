package foundation

import (
	"errors"
	"fmt"
)

const (
	RouterDecisionSelectSkill      = "select_skill"
	RouterDecisionClarify          = "clarify"
	RouterDecisionCapabilityAnswer = "capability_answer"
	RouterDecisionGenericCreation  = "generic_creation"
	RouterDecisionTextAnswer       = "text_answer"
	RouterDecisionReject           = "reject"

	SkillSourceSystemDefault = "system_default"
	SkillSourceSystemBuiltin = "system_builtin"
	SkillSourceInstalled     = "installed"
	SkillSourceMarketplace   = "marketplace"

	EntitlementAvailable               = "available"
	EntitlementInstallRequired         = "install_required"
	EntitlementEnterpriseAdminRequired = "enterprise_admin_required"
	EntitlementUnavailable             = "unavailable"
)

type CandidateSkill struct {
	SkillID string  `json:"skill_id"`
	Score   float64 `json:"score"`
	Why     string  `json:"why"`
}

type MarketplaceCandidate struct {
	SkillID           string         `json:"skill_id"`
	ListingID         string         `json:"listing_id"`
	Score             float64        `json:"score"`
	PricingSummary    map[string]any `json:"pricing_summary"`
	CreatorSummary    map[string]any `json:"creator_summary"`
	EntitlementStatus string         `json:"entitlement_status"`
}

type RouterDecision struct {
	SchemaVersion                  string                 `json:"schema_version"`
	Decision                       string                 `json:"decision"`
	SkillSource                    *string                `json:"skill_source"`
	SkillID                        *string                `json:"skill_id"`
	ListingID                      *string                `json:"listing_id"`
	Confidence                     float64                `json:"confidence"`
	ReasonCode                     string                 `json:"reason_code"`
	SafeToExecute                  bool                   `json:"safe_to_execute"`
	RequiresSkillUsageConfirmation bool                   `json:"requires_skill_usage_confirmation"`
	ExtractedParams                map[string]any         `json:"extracted_params"`
	MissingFields                  []string               `json:"missing_fields"`
	CandidateSkills                []CandidateSkill       `json:"candidate_skills"`
	MarketplaceCandidates          []MarketplaceCandidate `json:"marketplace_candidates"`
	PricingSummary                 map[string]any         `json:"pricing_summary"`
	CreatorSummary                 map[string]any         `json:"creator_summary"`
	EntitlementStatus              *string                `json:"entitlement_status"`
	FallbackReason                 *string                `json:"fallback_reason"`
}

func ValidateRouterDecision(decision RouterDecision) error {
	if decision.SchemaVersion != SchemaVersionRouterDecision {
		return fmt.Errorf("router_decision schema_version must be %s", SchemaVersionRouterDecision)
	}
	if !isAllowed(decision.Decision, []string{
		RouterDecisionSelectSkill,
		RouterDecisionClarify,
		RouterDecisionCapabilityAnswer,
		RouterDecisionGenericCreation,
		RouterDecisionTextAnswer,
		RouterDecisionReject,
	}) {
		return fmt.Errorf("invalid router decision %q", decision.Decision)
	}
	if source := stringPtrValue(decision.SkillSource); source != "" && !isAllowed(source, []string{
		SkillSourceSystemDefault,
		SkillSourceSystemBuiltin,
		SkillSourceInstalled,
		SkillSourceMarketplace,
	}) {
		return fmt.Errorf("invalid skill_source %q", source)
	}
	if entitlement := stringPtrValue(decision.EntitlementStatus); entitlement != "" && !isAllowed(entitlement, []string{
		EntitlementAvailable,
		EntitlementInstallRequired,
		EntitlementEnterpriseAdminRequired,
		EntitlementUnavailable,
	}) {
		return fmt.Errorf("invalid entitlement_status %q", entitlement)
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return fmt.Errorf("router confidence out of range: %v", decision.Confidence)
	}
	if decision.ReasonCode == "" {
		return errors.New("router reason_code is required")
	}
	if decision.SafeToExecute {
		return errors.New("router safe_to_execute must be false before explicit confirmation gates")
	}
	if decision.ExtractedParams == nil {
		return errors.New("router extracted_params must be present")
	}
	if decision.MissingFields == nil {
		return errors.New("router missing_fields must be present")
	}
	if decision.CandidateSkills == nil {
		return errors.New("router candidate_skills must be present")
	}
	if decision.MarketplaceCandidates == nil {
		return errors.New("router marketplace_candidates must be present")
	}
	for _, candidate := range decision.CandidateSkills {
		if candidate.SkillID == "" || candidate.Why == "" {
			return errors.New("candidate skill_id and why are required")
		}
		if candidate.Score < 0 || candidate.Score > 1 {
			return fmt.Errorf("candidate score out of range: %v", candidate.Score)
		}
	}
	for _, candidate := range decision.MarketplaceCandidates {
		if candidate.SkillID == "" || candidate.ListingID == "" {
			return errors.New("marketplace candidate skill_id and listing_id are required")
		}
		if candidate.Score < 0 || candidate.Score > 1 {
			return fmt.Errorf("marketplace candidate score out of range: %v", candidate.Score)
		}
		if !isAllowed(candidate.EntitlementStatus, []string{
			EntitlementAvailable,
			EntitlementInstallRequired,
			EntitlementEnterpriseAdminRequired,
			EntitlementUnavailable,
		}) {
			return fmt.Errorf("marketplace candidate invalid entitlement_status %q", candidate.EntitlementStatus)
		}
	}
	return nil
}

func isAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
