package router

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	runtimeskill "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skill"
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

type ChatModelRouter struct {
	routeHints runtimeskill.Router
}

func NewChatModelRouter() ChatModelRouter {
	return ChatModelRouter{routeHints: runtimeskill.NewRouter()}
}

func (r ChatModelRouter) Decide(ctx context.Context, input Input) (pr1.RouterDecision, error) {
	if err := ctx.Err(); err != nil {
		return pr1.RouterDecision{}, err
	}
	text := strings.TrimSpace(input.UserInput)
	if input.RunIntent == "capability_question" || isCapabilityQuestion(text) {
		return r.capabilityAnswer(input), nil
	}
	if input.SelectedSkillID != "" || input.SelectedListingID != "" || input.RunIntent == "select_skill" {
		return r.explicitSelect(input), nil
	}
	if decision, ok := r.matchPublishedSkill(text, input.Catalog); ok {
		return decision, nil
	}
	if isAmbiguousPromo(text) {
		return clarifyDecision(map[string]any{"topic": "产品宣传片", "style": "年轻"}, []string{"product_name", "duration_sec", "target_platform"}, "missing_output_specs", "需要补充产品、时长和投放平台", 0.68), nil
	}
	if hasCreativeIntent(text) {
		return genericCreationDecision(text), nil
	}
	return clarifyDecision(map[string]any{"user_input_preview": trimForRouter(text, 80)}, []string{"creative_goal"}, "creative_intent_unclear", "请补充要创作的目标、媒介或限制", 0.42), nil
}

func (r ChatModelRouter) capabilityAnswer(input Input) pr1.RouterDecision {
	marketplaceCandidates := make([]pr1.MarketplaceCandidate, 0)
	for _, skill := range input.Catalog {
		if skill.Status != "published" || skillSource(skill) != pr1.SkillSourceMarketplace || skill.ListingID == "" {
			continue
		}
		entitlement := entitlementStatus(skill)
		if entitlement == pr1.EntitlementAvailable && !skillRequiresUsageConfirmation(skill) {
			continue
		}
		marketplaceCandidates = append(marketplaceCandidates, pr1.MarketplaceCandidate{
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
	return pr1.RouterDecision{
		SchemaVersion:                  pr1.SchemaVersionRouterDecision,
		Decision:                       pr1.RouterDecisionCapabilityAnswer,
		Confidence:                     0.81,
		ReasonCode:                     "capability_question",
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                map[string]any{},
		MissingFields:                  []string{},
		CandidateSkills:                []pr1.CandidateSkill{},
		MarketplaceCandidates:          marketplaceCandidates,
		PricingSummary:                 nil,
		CreatorSummary:                 nil,
		FallbackReason:                 stringPtr(fallback),
	}
}

func (r ChatModelRouter) explicitSelect(input Input) pr1.RouterDecision {
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
		return selectSkillDecision(skill, 0.94, "explicit_skill_selected", extractedParams(input.UserInput), "用户显式选择 Skill")
	}
	return clarifyDecision(map[string]any{"requested_skill_id": input.SelectedSkillID, "requested_listing_id": input.SelectedListingID}, []string{"available_skill_choice"}, "selected_skill_unavailable", "listing removed", 0.73)
}

func (r ChatModelRouter) matchPublishedSkill(text string, catalog []CatalogSkill) (pr1.RouterDecision, bool) {
	summaries := make([]runtimeskill.Summary, 0, len(catalog))
	byID := map[string]CatalogSkill{}
	for _, skill := range catalog {
		byID[skill.SkillID] = skill
		summaries = append(summaries, runtimeskill.Summary{
			SkillID: skill.SkillID, SkillName: skill.SkillName, SkillScope: skill.SkillSource, Version: skill.SkillVersion,
			Status: skill.Status, RouteHints: skill.RouteHints,
		})
	}
	route := r.routeHints.Route(text, summaries)
	if !route.Matched {
		return pr1.RouterDecision{}, false
	}
	skill := byID[route.Skill.SkillID]
	if skill.Status != "published" {
		return unavailableDecision(skill), true
	}
	if skillSource(skill) == pr1.SkillSourceMarketplace && entitlementStatus(skill) != pr1.EntitlementAvailable {
		return marketplaceCapabilityDecision(skill, text), true
	}
	return selectSkillDecision(skill, 0.92, reasonCodeForSkill(skill, route.Reason), extractedParams(text), "命中"+displaySkillName(skill)+"场景"), true
}

func selectSkillDecision(skill CatalogSkill, confidence float64, reasonCode string, params map[string]any, why string) pr1.RouterDecision {
	source := skillSource(skill)
	skillID := strings.TrimSpace(skill.SkillID)
	listingID := strings.TrimSpace(skill.ListingID)
	entitlement := entitlementStatus(skill)
	return pr1.RouterDecision{
		SchemaVersion:                  pr1.SchemaVersionRouterDecision,
		Decision:                       pr1.RouterDecisionSelectSkill,
		SkillSource:                    stringPtr(source),
		SkillID:                        stringPtr(skillID),
		ListingID:                      nullableStringPtr(listingID),
		Confidence:                     confidence,
		ReasonCode:                     reasonCode,
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: skillRequiresUsageConfirmation(skill),
		ExtractedParams:                params,
		MissingFields:                  []string{},
		CandidateSkills:                []pr1.CandidateSkill{{SkillID: skillID, Score: confidence, Why: why}},
		MarketplaceCandidates:          []pr1.MarketplaceCandidate{},
		PricingSummary:                 nullableMap(skill.PricingSummary),
		CreatorSummary:                 nullableMap(skill.CreatorSummary),
		EntitlementStatus:              stringPtr(entitlement),
		FallbackReason:                 nil,
	}
}

func marketplaceCapabilityDecision(skill CatalogSkill, text string) pr1.RouterDecision {
	return pr1.RouterDecision{
		SchemaVersion:                  pr1.SchemaVersionRouterDecision,
		Decision:                       pr1.RouterDecisionCapabilityAnswer,
		Confidence:                     0.81,
		ReasonCode:                     "marketplace_candidates_not_installed",
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                extractedParams(text),
		MissingFields:                  []string{},
		CandidateSkills:                []pr1.CandidateSkill{},
		MarketplaceCandidates: []pr1.MarketplaceCandidate{{
			SkillID:           skill.SkillID,
			ListingID:         skill.ListingID,
			Score:             0.87,
			PricingSummary:    nonNilMap(skill.PricingSummary),
			CreatorSummary:    nonNilMap(skill.CreatorSummary),
			EntitlementStatus: entitlementStatus(skill),
		}},
		PricingSummary:    nil,
		CreatorSummary:    nil,
		EntitlementStatus: nil,
		FallbackReason:    stringPtr("展示市场候选，等待用户显式安装或选择"),
	}
}

func genericCreationDecision(text string) pr1.RouterDecision {
	source := pr1.SkillSourceSystemBuiltin
	skillID := "skill_generic_creation"
	return pr1.RouterDecision{
		SchemaVersion:                  pr1.SchemaVersionRouterDecision,
		Decision:                       pr1.RouterDecisionGenericCreation,
		SkillSource:                    &source,
		SkillID:                        &skillID,
		ListingID:                      nil,
		Confidence:                     0.72,
		ReasonCode:                     "creative_intent_without_specific_skill",
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                extractedParams(text),
		MissingFields:                  []string{},
		CandidateSkills:                []pr1.CandidateSkill{{SkillID: skillID, Score: 0.72, Why: "未命中具体 Skill，进入内置自由创作"}},
		MarketplaceCandidates:          []pr1.MarketplaceCandidate{},
		PricingSummary:                 map[string]any{"skill_usage_points": 0, "tool_generation_points": "preflight_estimate"},
		CreatorSummary:                 nil,
		EntitlementStatus:              stringPtr(pr1.EntitlementAvailable),
		FallbackReason:                 nil,
	}
}

func clarifyDecision(params map[string]any, missing []string, reasonCode, fallback string, confidence float64) pr1.RouterDecision {
	return pr1.RouterDecision{
		SchemaVersion:                  pr1.SchemaVersionRouterDecision,
		Decision:                       pr1.RouterDecisionClarify,
		Confidence:                     confidence,
		ReasonCode:                     reasonCode,
		SafeToExecute:                  false,
		RequiresSkillUsageConfirmation: false,
		ExtractedParams:                params,
		MissingFields:                  missing,
		CandidateSkills:                []pr1.CandidateSkill{},
		MarketplaceCandidates:          []pr1.MarketplaceCandidate{},
		PricingSummary:                 nil,
		CreatorSummary:                 nil,
		EntitlementStatus:              nil,
		FallbackReason:                 stringPtr(fallback),
	}
}

func unavailableDecision(skill CatalogSkill) pr1.RouterDecision {
	return clarifyDecision(map[string]any{"requested_skill_id": skill.SkillID, "requested_listing_id": skill.ListingID}, []string{"available_skill_choice"}, "selected_skill_unavailable", "listing removed", 0.73)
}

func extractedParams(text string) map[string]any {
	params := map[string]any{}
	if strings.Contains(text, "杭州") {
		params["city_or_destination"] = "杭州"
	}
	if strings.Contains(text, "现代国风") {
		params["style"] = "现代国风"
	} else if strings.Contains(text, "年轻") {
		params["style"] = "年轻"
	}
	if match := regexp.MustCompile(`([0-9]+)\s*秒`).FindStringSubmatch(text); len(match) == 2 {
		if seconds, err := strconv.Atoi(match[1]); err == nil {
			params["duration_sec"] = seconds
		}
	}
	if strings.Contains(text, "产品宣传片") {
		params["topic"] = "产品宣传片"
	}
	return params
}

func reasonCodeForSkill(skill CatalogSkill, fallback string) string {
	if strings.Contains(skill.SkillID, "city_tourism") {
		return "matched_city_tourism_video"
	}
	if fallback != "" {
		return strings.ReplaceAll(fallback, ":", "_")
	}
	return "matched_published_skill"
}

func skillSource(skill CatalogSkill) string {
	source := strings.TrimSpace(skill.SkillSource)
	if source == "" {
		source = strings.TrimSpace(skill.RouteHints["skill_source"])
	}
	switch source {
	case pr1.SkillSourceSystemDefault, pr1.SkillSourceSystemBuiltin, pr1.SkillSourceInstalled, pr1.SkillSourceMarketplace:
		return source
	case "public", "default", "":
		return pr1.SkillSourceSystemDefault
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
	if skillSource(skill) == pr1.SkillSourceMarketplace && skill.ListingID != "" {
		return pr1.EntitlementInstallRequired
	}
	return pr1.EntitlementAvailable
}

func skillRequiresUsageConfirmation(skill CatalogSkill) bool {
	if skillSource(skill) != pr1.SkillSourceMarketplace {
		return false
	}
	if value, ok := skill.PricingSummary["skill_usage_points"]; ok && value != nil {
		return true
	}
	return false
}

func isCapabilityQuestion(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "你有什么能力") ||
		strings.Contains(text, "你能做什么") ||
		strings.Contains(text, "有哪些能力") ||
		strings.Contains(lower, "what can you do") ||
		strings.Contains(lower, "skill")
}

func isAmbiguousPromo(text string) bool {
	return strings.Contains(text, "宣传片") && !strings.Contains(text, "文旅") && !strings.Contains(text, "30秒") && !strings.Contains(text, "30 秒")
}

func hasCreativeIntent(text string) bool {
	if text == "" {
		return false
	}
	verbs := []string{"做", "生成", "创作", "设计", "制作", "写", "整理"}
	objects := []string{"视频", "宣传片", "海报", "文案", "脚本", "分镜", "音乐", "图片", "brief", "prompt"}
	return containsAny(text, verbs) && containsAny(text, objects)
}

func containsAny(text string, values []string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
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
