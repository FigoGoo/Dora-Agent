package router

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

type CatalogSkill struct {
	SkillID              string
	SkillName            string
	SkillSource          string
	ListingID            string
	SkillVersion         string
	Status               string
	SupportedOutputTypes []string
	RoutingExamples      []string
	RouteHints           map[string]string
	PricingSummary       map[string]any
	CreatorSummary       map[string]any
	EntitlementStatus    string
}

type Input struct {
	UserInput         string
	RunIntent         string
	SelectedSkillID   string
	SelectedListingID string
	Catalog           []CatalogSkill
}

// ChatModelRouter 是无第三方 Key 环境下的结构化 mock router。
// 它只使用显式 run_intent / selected_skill_id，不再扫描用户文本或 route_hints 做自动匹配。
type ChatModelRouter struct{}

func NewChatModelRouter() ChatModelRouter {
	return ChatModelRouter{}
}

func (r ChatModelRouter) Decide(ctx context.Context, input Input) (foundation.RouterDecision, error) {
	if err := ctx.Err(); err != nil {
		return foundation.RouterDecision{}, err
	}
	text := strings.TrimSpace(input.UserInput)
	if input.RunIntent == "capability_question" {
		return r.capabilityAnswer(input), nil
	}
	if input.SelectedSkillID != "" || input.SelectedListingID != "" || input.RunIntent == "select_skill" {
		return r.explicitSelect(input), nil
	}
	if text == "" {
		return clarifyDecision(map[string]any{}, []string{"creative_goal"}, "empty_user_input", "user input is empty", 0.35), nil
	}
	return genericCreationDecision(text), nil
}

func (r ChatModelRouter) capabilityAnswer(input Input) foundation.RouterDecision {
	marketplaceCandidates := make([]foundation.MarketplaceCandidate, 0)
	for _, skill := range input.Catalog {
		if skill.Status != "published" || skillSource(skill) != foundation.SkillSourceMarketplace || skill.ListingID == "" {
			continue
		}
		entitlement := entitlementStatus(skill)
		if entitlement == foundation.EntitlementAvailable && !skillRequiresUsageConfirmation(skill) {
			continue
		}
		marketplaceCandidates = append(marketplaceCandidates, foundation.MarketplaceCandidate{
			SkillID:           skill.SkillID,
			ListingID:         skill.ListingID,
			Score:             0.82,
			PricingSummary:    nonNilMap(skill.PricingSummary),
			CreatorSummary:    nonNilMap(skill.CreatorSummary),
			EntitlementStatus: entitlement,
		})
	}
	fallback := "展示当前可用能力，等待用户选择"
	if len(marketplaceCandidates) > 0 {
		fallback = "展示市场候选，等待用户显式安装或选择"
	}
	return foundation.RouterDecision{
		SchemaVersion:                  foundation.SchemaVersionRouterDecision,
		Decision:                       foundation.RouterDecisionCapabilityAnswer,
		Confidence:                     0.81,
		ReasonCode:                     "capability_question",
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                map[string]any{},
		MissingFields:                  []string{},
		CandidateSkills:                []foundation.CandidateSkill{},
		MarketplaceCandidates:          marketplaceCandidates,
		PricingSummary:                 nil,
		CreatorSummary:                 nil,
		FallbackReason:                 stringPtr(fallback),
	}
}

func (r ChatModelRouter) explicitSelect(input Input) foundation.RouterDecision {
	for _, skill := range input.Catalog {
		if input.SelectedSkillID != "" && skill.SkillID != input.SelectedSkillID {
			continue
		}
		if input.SelectedListingID != "" && skill.ListingID != input.SelectedListingID {
			continue
		}
		if skill.Status != "published" {
			return unavailableDecision(skill)
		}
		return selectSkillDecision(skill, 0.94, "explicit_skill_selected", map[string]any{}, "用户显式选择 Skill")
	}
	return clarifyDecision(map[string]any{"requested_skill_id": input.SelectedSkillID, "requested_listing_id": input.SelectedListingID}, []string{"available_skill_choice"}, "selected_skill_unavailable", "listing removed", 0.73)
}

func selectSkillDecision(skill CatalogSkill, confidence float64, reasonCode string, params map[string]any, why string) foundation.RouterDecision {
	source := skillSource(skill)
	skillID := strings.TrimSpace(skill.SkillID)
	listingID := strings.TrimSpace(skill.ListingID)
	entitlement := entitlementStatus(skill)
	return foundation.RouterDecision{
		SchemaVersion:                  foundation.SchemaVersionRouterDecision,
		Decision:                       foundation.RouterDecisionSelectSkill,
		SkillSource:                    stringPtr(source),
		SkillID:                        stringPtr(skillID),
		ListingID:                      nullableStringPtr(listingID),
		Confidence:                     confidence,
		ReasonCode:                     reasonCode,
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: skillRequiresUsageConfirmation(skill),
		ExtractedParams:                params,
		MissingFields:                  []string{},
		CandidateSkills:                []foundation.CandidateSkill{{SkillID: skillID, Score: confidence, Why: why}},
		MarketplaceCandidates:          []foundation.MarketplaceCandidate{},
		PricingSummary:                 nullableMap(skill.PricingSummary),
		CreatorSummary:                 nullableMap(skill.CreatorSummary),
		EntitlementStatus:              stringPtr(entitlement),
		FallbackReason:                 nil,
	}
}

func genericCreationDecision(text string) foundation.RouterDecision {
	source := foundation.SkillSourceSystemBuiltin
	skillID := "skill_generic_creation"
	return foundation.RouterDecision{
		SchemaVersion:                  foundation.SchemaVersionRouterDecision,
		Decision:                       foundation.RouterDecisionGenericCreation,
		SkillSource:                    &source,
		SkillID:                        &skillID,
		ListingID:                      nil,
		Confidence:                     0.72,
		ReasonCode:                     "mock_router_generic_creation",
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                map[string]any{"user_input_preview": trimForRouter(text, 80)},
		MissingFields:                  []string{},
		CandidateSkills:                []foundation.CandidateSkill{{SkillID: skillID, Score: 0.72, Why: "无 LLM 路由结果，进入内置自由创作"}},
		MarketplaceCandidates:          []foundation.MarketplaceCandidate{},
		PricingSummary:                 map[string]any{"skill_usage_points": 0, "tool_generation_points": "preflight_estimate"},
		CreatorSummary:                 nil,
		EntitlementStatus:              stringPtr(foundation.EntitlementAvailable),
		FallbackReason:                 nil,
	}
}

func clarifyDecision(params map[string]any, missing []string, reasonCode, fallback string, confidence float64) foundation.RouterDecision {
	return foundation.RouterDecision{
		SchemaVersion:                  foundation.SchemaVersionRouterDecision,
		Decision:                       foundation.RouterDecisionClarify,
		Confidence:                     confidence,
		ReasonCode:                     reasonCode,
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                params,
		MissingFields:                  missing,
		CandidateSkills:                []foundation.CandidateSkill{},
		MarketplaceCandidates:          []foundation.MarketplaceCandidate{},
		PricingSummary:                 nil,
		CreatorSummary:                 nil,
		EntitlementStatus:              nil,
		FallbackReason:                 stringPtr(fallback),
	}
}

func unavailableDecision(skill CatalogSkill) foundation.RouterDecision {
	return clarifyDecision(map[string]any{"requested_skill_id": skill.SkillID, "requested_listing_id": skill.ListingID}, []string{"available_skill_choice"}, "selected_skill_unavailable", "listing removed", 0.73)
}

func skillSource(skill CatalogSkill) string {
	source := strings.TrimSpace(skill.SkillSource)
	if source == "" {
		source = strings.TrimSpace(skill.RouteHints["skill_source"])
	}
	switch source {
	case foundation.SkillSourceSystemDefault, foundation.SkillSourceSystemBuiltin, foundation.SkillSourceInstalled, foundation.SkillSourceMarketplace:
		return source
	case "public", "default", "":
		return foundation.SkillSourceSystemDefault
	default:
		return source
	}
}

func entitlementStatus(skill CatalogSkill) string {
	if skill.EntitlementStatus != "" {
		return skill.EntitlementStatus
	}
	if value := strings.TrimSpace(skill.RouteHints["entitlement"]); value != "" {
		return value
	}
	if skillSource(skill) == foundation.SkillSourceMarketplace && skill.ListingID != "" {
		return foundation.EntitlementInstallRequired
	}
	return foundation.EntitlementAvailable
}

func skillRequiresUsageConfirmation(skill CatalogSkill) bool {
	if skillSource(skill) != foundation.SkillSourceMarketplace {
		return false
	}
	if value, ok := skill.PricingSummary["skill_usage_points"]; ok && value != nil {
		return true
	}
	return false
}

func displaySkillName(skill CatalogSkill) string {
	if strings.TrimSpace(skill.SkillName) != "" {
		return strings.TrimSpace(skill.SkillName)
	}
	return strings.TrimSpace(skill.SkillID)
}

func nullableStringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return stringPtr(value)
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func nullableMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func trimForRouter(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
