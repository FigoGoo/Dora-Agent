package router

import (
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

// Guard 置信度带（docs/02 §2.3）。
const (
	guardAutoSelectConfidence = 0.85
	guardCandidateConfidence  = 0.60
	guardClarifyConfidence    = 0.35
)

// ApplyRouterGuard 对 LLM（或任何来源）的 RouterDecision 做确定性校验与降级：
// 不存在/未发布 Skill 拦截、市场 entitlement 校验、置信度分带、safe_to_execute 强制 false。
// Guard 只依赖输入目录，不做任何业务写入。
func ApplyRouterGuard(decision foundation.RouterDecision, input Input) foundation.RouterDecision {
	decision.SchemaVersion = foundation.SchemaVersionRouterDecision
	decision.SafeToExecute = false
	decision = normalizeDecisionCollections(decision)
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}

	byID := make(map[string]CatalogSkill, len(input.Catalog))
	for _, skill := range input.Catalog {
		byID[skill.SkillID] = skill
	}

	explicitSelection := input.SelectedSkillID != "" || input.SelectedListingID != "" || input.RunIntent == "select_skill"

	switch decision.Decision {
	case foundation.RouterDecisionSelectSkill:
		skillID := stringValue(decision.SkillID)
		skill, exists := byID[skillID]
		if skillID == "" || !exists {
			// Router 不能选择不存在的 Skill；只按结构化置信度降级，不扫描用户文本。
			if decision.Confidence >= guardCandidateConfidence {
				return genericCreationDecision(input.UserInput)
			}
			return clarifyDecision(map[string]any{"requested_skill_id": skillID}, []string{"creative_goal"}, "router_selected_unknown_skill", "router selected unknown skill", clampClarifyConfidence(decision.Confidence))
		}
		if skill.Status != "published" {
			return unavailableDecision(skill)
		}
		if skillSource(skill) == foundation.SkillSourceMarketplace {
			if entitlementStatus(skill) != foundation.EntitlementAvailable && !explicitSelection {
				// 未安装市场 Skill 不得自动执行，只作为候选；主路径进入场景引导。
				generic := genericCreationDecision(input.UserInput)
				generic.MarketplaceCandidates = append(generic.MarketplaceCandidates, foundation.MarketplaceCandidate{
					SkillID:           skill.SkillID,
					ListingID:         skill.ListingID,
					Score:             decision.Confidence,
					PricingSummary:    nonNilMap(skill.PricingSummary),
					CreatorSummary:    nonNilMap(skill.CreatorSummary),
					EntitlementStatus: entitlementStatus(skill),
				})
				return generic
			}
			decision.RequiresSkillUsageConfirmation = skillRequiresUsageConfirmation(skill)
		}
		// 与目录事实对齐，防止 LLM 幻觉 source/listing/entitlement。
		decision.SkillSource = stringPtr(skillSource(skill))
		decision.ListingID = nullableStringPtr(skill.ListingID)
		decision.EntitlementStatus = stringPtr(entitlementStatus(skill))
		if decision.PricingSummary == nil {
			decision.PricingSummary = nullableMap(skill.PricingSummary)
		}
		if len(decision.CandidateSkills) == 0 {
			decision.CandidateSkills = []foundation.CandidateSkill{{SkillID: skill.SkillID, Score: decision.Confidence, Why: "路由命中 " + displaySkillName(skill)}}
		}
		if explicitSelection || decision.Confidence >= guardAutoSelectConfidence {
			return decision
		}
		if decision.Confidence >= guardCandidateConfidence {
			// 中置信：展示候选让用户确认，不自动进入 Skill。
			confirm := clarifyDecision(decision.ExtractedParams, []string{"creative_goal"}, "candidate_confirmation_required", "confidence below auto-select threshold", decision.Confidence)
			confirm.CandidateSkills = decision.CandidateSkills
			confirm.MarketplaceCandidates = decision.MarketplaceCandidates
			return confirm
		}
		return clarifyDecision(decision.ExtractedParams, missingOrDefault(decision.MissingFields), "low_confidence_route", "confidence below candidate threshold", decision.Confidence)
	case foundation.RouterDecisionGenericCreation:
		// 场景引导必须绑定内置 skill_generic_creation（docs/02 §2.5）。
		generic := genericCreationDecision(input.UserInput)
		generic.ExtractedParams = mergeParams(generic.ExtractedParams, decision.ExtractedParams)
		generic.MarketplaceCandidates = decision.MarketplaceCandidates
		if decision.Confidence > 0 {
			generic.Confidence = decision.Confidence
		}
		return generic
	case foundation.RouterDecisionClarify:
		if decision.Confidence >= guardAutoSelectConfidence {
			decision.Confidence = guardCandidateConfidence
		}
		decision.MissingFields = missingOrDefault(decision.MissingFields)
		decision.RequiresSkillUsageConfirmation = false
		return decision
	case foundation.RouterDecisionCapabilityAnswer, foundation.RouterDecisionTextAnswer, foundation.RouterDecisionReject:
		decision.RequiresSkillUsageConfirmation = false
		return decision
	default:
		return clarifyDecision(map[string]any{}, []string{"creative_goal"}, "router_decision_unknown", "unknown decision "+decision.Decision, guardClarifyConfidence)
	}
}

func normalizeDecisionCollections(decision foundation.RouterDecision) foundation.RouterDecision {
	if decision.ExtractedParams == nil {
		decision.ExtractedParams = map[string]any{}
	}
	if decision.MissingFields == nil {
		decision.MissingFields = []string{}
	}
	if decision.CandidateSkills == nil {
		decision.CandidateSkills = []foundation.CandidateSkill{}
	}
	if decision.MarketplaceCandidates == nil {
		decision.MarketplaceCandidates = []foundation.MarketplaceCandidate{}
	}
	if strings.TrimSpace(decision.ReasonCode) == "" {
		decision.ReasonCode = "router_reason_missing"
	}
	return decision
}

func missingOrDefault(fields []string) []string {
	if len(fields) == 0 {
		return []string{"creative_goal"}
	}
	if len(fields) > 3 {
		return fields[:3]
	}
	return fields
}

func mergeParams(base, extra map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func clampClarifyConfidence(confidence float64) float64 {
	if confidence <= 0 || confidence >= guardCandidateConfidence {
		return guardClarifyConfidence
	}
	return confidence
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
