package router

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

type fakeChatClient struct {
	output string
	err    error
	calls  int
}

func (f *fakeChatClient) Complete(_ context.Context, _, _ string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.output, nil
}

func llmTestCatalog() []CatalogSkill {
	return []CatalogSkill{
		{
			SkillID: "skill_city_tourism_video", SkillName: "城市文旅视频", SkillSource: "system_default",
			SkillVersion: "1.0.0", Status: "published",
		},
		{
			SkillID: "skill_market_pro", SkillName: "市场高级视频", SkillSource: "marketplace", ListingID: "listing_pro_001",
			SkillVersion: "1.0.0", Status: "published",
			PricingSummary:    map[string]any{"skill_usage_points": 120},
			EntitlementStatus: foundation.EntitlementInstallRequired,
		},
	}
}

func TestLLMRouterSelectsPublishedSkill(t *testing.T) {
	client := &fakeChatClient{output: `{
		"decision":"select_skill","skill_id":"skill_city_tourism_video","skill_source":"system_default",
		"confidence":0.91,"reason_code":"matched_city_tourism","extracted_params":{"city_or_destination":"杭州"},
		"missing_fields":[],"candidate_skills":[{"skill_id":"skill_city_tourism_video","score":0.91,"why":"明确的城市文旅视频需求"}],
		"marketplace_candidates":[],"clarify_question":"","suggested_questions":[]
	}`}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "帮我做一个杭州文旅宣传视频", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.Source != "llm" {
		t.Fatalf("expected llm source, got %q", result.Source)
	}
	decision := result.Decision
	if decision.Decision != foundation.RouterDecisionSelectSkill || decision.SkillID == nil || *decision.SkillID != "skill_city_tourism_video" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.SafeToExecute {
		t.Fatalf("safe_to_execute must stay false")
	}
	if decision.RequiresSkillUsageConfirmation {
		t.Fatalf("system default skill must not require usage confirmation")
	}
	if err := foundation.ValidateRouterDecision(decision); err != nil {
		t.Fatalf("decision must pass contract validation: %v", err)
	}
}

func TestLLMRouterClarifyQuestionPassthrough(t *testing.T) {
	client := &fakeChatClient{output: "```json\n" + `{
		"decision":"clarify","confidence":0.45,"reason_code":"creative_intent_unclear",
		"extracted_params":{},"missing_fields":["creative_goal"],
		"candidate_skills":[],"marketplace_candidates":[],
		"clarify_question":"你想做一支什么主题的 MV？说说风格和大概时长。",
		"suggested_questions":["城市夜景风格","热血运动风格"]
	}` + "\n```"}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "做个mv", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.Decision.Decision != foundation.RouterDecisionClarify {
		t.Fatalf("expected clarify, got %#v", result.Decision)
	}
	if result.ClarifyQuestion != "你想做一支什么主题的 MV？说说风格和大概时长。" {
		t.Fatalf("clarify question must pass through, got %q", result.ClarifyQuestion)
	}
	if len(result.SuggestedQuestions) != 2 {
		t.Fatalf("suggested questions must pass through, got %#v", result.SuggestedQuestions)
	}
	if strings.Contains(result.ClarifyQuestion, "creative_goal") {
		t.Fatalf("clarify question must not leak internal field names")
	}
}

func TestLLMRouterGuardsUnknownSkillToGenericCreation(t *testing.T) {
	client := &fakeChatClient{output: `{
		"decision":"select_skill","skill_id":"skill_hallucinated","confidence":0.9,
		"reason_code":"made_up","extracted_params":{},"missing_fields":[],
		"candidate_skills":[],"marketplace_candidates":[]
	}`}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "做个mv", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	decision := result.Decision
	if decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("unknown skill with creative intent must fall into generic creation, got %#v", decision)
	}
	if decision.SkillID == nil || *decision.SkillID != "skill_generic_creation" {
		t.Fatalf("generic creation must bind skill_generic_creation: %#v", decision)
	}
}

func TestLLMRouterFallsBackToMockOnBadOutput(t *testing.T) {
	client := &fakeChatClient{output: "抱歉，我无法输出 JSON"}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "做个mv", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route must degrade instead of failing: %v", err)
	}
	if result.Source != "mock" {
		t.Fatalf("expected mock fallback, got %q", result.Source)
	}
	if client.calls != 2 {
		t.Fatalf("expected 1 retry (2 calls), got %d", client.calls)
	}
	if result.Decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("mock fallback should enter generic creation for non-empty normal input, got %#v", result.Decision)
	}
}

func TestLLMRouterFallsBackToMockOnClientError(t *testing.T) {
	client := &fakeChatClient{err: errors.New("upstream timeout")}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "帮我做一个产品宣传片，年轻一点", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route must degrade instead of failing: %v", err)
	}
	if result.Source != "mock" || result.Decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("expected mock generic fallback, got source=%q decision=%#v", result.Source, result.Decision)
	}
}

func TestLLMRouterDemotesUninstalledMarketplaceSelection(t *testing.T) {
	client := &fakeChatClient{output: `{
		"decision":"select_skill","skill_id":"skill_market_pro","listing_id":"listing_pro_001",
		"skill_source":"marketplace","confidence":0.92,"reason_code":"marketplace_pick",
		"extracted_params":{},"missing_fields":[],"candidate_skills":[],"marketplace_candidates":[]
	}`}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "做个城市视频", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	decision := result.Decision
	if decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("uninstalled marketplace skill must not auto execute, got %#v", decision)
	}
	found := false
	for _, candidate := range decision.MarketplaceCandidates {
		if candidate.ListingID == "listing_pro_001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("marketplace candidate must be preserved: %#v", decision.MarketplaceCandidates)
	}
}

func TestLLMRouterMidConfidenceRequiresCandidateConfirmation(t *testing.T) {
	client := &fakeChatClient{output: `{
		"decision":"select_skill","skill_id":"skill_city_tourism_video","skill_source":"system_default",
		"confidence":0.7,"reason_code":"probable_match","extracted_params":{},
		"missing_fields":[],"candidate_skills":[],"marketplace_candidates":[]
	}`}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{UserInput: "做个城市视频", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	decision := result.Decision
	if decision.Decision != foundation.RouterDecisionClarify {
		t.Fatalf("mid confidence must ask user to confirm, got %#v", decision)
	}
	if len(decision.CandidateSkills) == 0 {
		t.Fatalf("candidate skills must be presented for confirmation: %#v", decision)
	}
}

func TestLLMRouterExplicitSelectionKeepsMarketplaceSkill(t *testing.T) {
	client := &fakeChatClient{output: `{
		"decision":"select_skill","skill_id":"skill_market_pro","listing_id":"listing_pro_001",
		"skill_source":"marketplace","confidence":0.95,"reason_code":"explicit_marketplace_selection",
		"extracted_params":{},"missing_fields":[],"candidate_skills":[],"marketplace_candidates":[]
	}`}
	router := NewLLMRouter(client)
	result, err := router.Route(t.Context(), Input{
		UserInput: "用市场高级视频做", RunIntent: "select_skill",
		SelectedSkillID: "skill_market_pro", SelectedListingID: "listing_pro_001",
		Catalog: llmTestCatalog(),
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	decision := result.Decision
	if decision.Decision != foundation.RouterDecisionSelectSkill {
		t.Fatalf("explicit selection must keep select_skill, got %#v", decision)
	}
	if !decision.RequiresSkillUsageConfirmation {
		t.Fatalf("paid marketplace skill must require usage confirmation")
	}
}

func TestApplyRouterGuardForcesSafeToExecuteFalse(t *testing.T) {
	skillID := "skill_city_tourism_video"
	source := foundation.SkillSourceSystemDefault
	decision := foundation.RouterDecision{
		SchemaVersion: foundation.SchemaVersionRouterDecision,
		Decision:      foundation.RouterDecisionSelectSkill,
		SkillID:       &skillID,
		SkillSource:   &source,
		Confidence:    0.95,
		ReasonCode:    "test",
		SafeToExecute: true,
	}
	guarded := ApplyRouterGuard(decision, Input{UserInput: "做个视频", Catalog: llmTestCatalog()})
	if guarded.SafeToExecute {
		t.Fatalf("guard must force safe_to_execute=false")
	}
	if err := foundation.ValidateRouterDecision(guarded); err != nil {
		t.Fatalf("guarded decision must pass contract validation: %v", err)
	}
}

func TestFallbackRouterDoesNotSelectSkillFromRouteHintsText(t *testing.T) {
	router := NewChatModelRouter()
	catalog := llmTestCatalog()
	catalog[0].RouteHints = map[string]string{
		"keywords":     "杭州文旅宣传视频,城市文旅,宣传视频",
		"output_types": "video,storyboard",
		"priority":     "90",
	}

	result, err := router.Route(t.Context(), Input{UserInput: "帮我做一个杭州文旅宣传视频", RunIntent: "normal", Catalog: catalog})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.Decision.Decision == foundation.RouterDecisionSelectSkill {
		t.Fatalf("fallback router must not select a skill from route_hints text, got %#v", result.Decision)
	}
	if result.Decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("fallback router should enter generic creation for non-empty normal input, got %#v", result.Decision)
	}
}

func TestApplyRouterGuardDoesNotUseUserTextToPromoteUnknownSkill(t *testing.T) {
	skillID := "skill_hallucinated"
	decision := foundation.RouterDecision{
		SchemaVersion: foundation.SchemaVersionRouterDecision,
		Decision:      foundation.RouterDecisionSelectSkill,
		SkillID:       &skillID,
		Confidence:    0.5,
		ReasonCode:    "weak_unknown_skill",
		SafeToExecute: false,
	}

	guarded := ApplyRouterGuard(decision, Input{UserInput: "做个MV", RunIntent: "normal", Catalog: llmTestCatalog()})
	if guarded.Decision != foundation.RouterDecisionClarify {
		t.Fatalf("low-confidence unknown skill must clarify without text heuristics, got %#v", guarded)
	}
}

func TestFallbackRouterUsesRunIntentInsteadOfCapabilityTextMatching(t *testing.T) {
	router := NewChatModelRouter()
	result, err := router.Route(t.Context(), Input{UserInput: "你有什么能力", RunIntent: "normal", Catalog: llmTestCatalog()})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.Decision.Decision != foundation.RouterDecisionGenericCreation {
		t.Fatalf("normal run must not become capability_answer by text matching, got %#v", result.Decision)
	}
}
