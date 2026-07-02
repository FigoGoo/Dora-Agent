package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

const routerPromptSchemaID = "chatmodel_router.v1"

const routerSystemPrompt = `你是 Dora-Agent 的创作意图路由器（chatmodel_router.v1）。
你的唯一任务：把用户输入分流为一个 JSON 决策对象，不要输出任何解释或 Markdown。

决策取值：
- "select_skill"：用户意图明确且命中 skill_catalog 中某个 published Skill。
- "generic_creation"：用户有明确创作意图，但没有合适的 Skill 命中（进入内置场景引导）。
- "clarify"：创作意图不明确，需要向用户提问补充。
- "capability_answer"：用户在询问平台能力。
- "text_answer"：与创作无关的普通问题。
- "reject"：违法违规或明显不可执行的请求。

硬规则：
1. skill_id 只能取 skill_catalog 里出现过的 skill_id，禁止编造。
2. safe_to_execute 恒为 false。
3. confidence 取 0~1；意图明确命中给 0.85 以上，需要用户确认给 0.6~0.85，模糊给 0.6 以下。
4. decision=clarify 时必须给 clarify_question（面向用户的中文提问，禁止出现字段名），missing_fields 用语义键（如 creative_goal、duration_sec）。
5. 未安装的市场 Skill 只能进 marketplace_candidates，不得作为 select_skill 结果，除非用户显式点选。
6. extracted_params 提取用户已给出的信息（城市、风格、时长、平台等）。
7. suggested_questions 最多 3 条，只在 clarify 时给。

输出 JSON 结构：
{
  "decision": "...",
  "skill_id": "或 null",
  "listing_id": "或 null",
  "skill_source": "system_default|system_builtin|installed|marketplace|null",
  "confidence": 0.0,
  "reason_code": "snake_case 简短原因",
  "extracted_params": {},
  "missing_fields": [],
  "candidate_skills": [{"skill_id":"...","score":0.0,"why":"..."}],
  "marketplace_candidates": [{"skill_id":"...","listing_id":"...","score":0.0,"entitlement_status":"install_required"}],
  "clarify_question": "或空字符串",
  "suggested_questions": []
}`

type routerCatalogEntry struct {
	SkillID           string            `json:"skill_id"`
	SkillName         string            `json:"skill_name"`
	SkillSource       string            `json:"skill_source"`
	ListingID         string            `json:"listing_id,omitempty"`
	Status            string            `json:"status"`
	OutputTypes       []string          `json:"output_types,omitempty"`
	RoutingExamples   []string          `json:"routing_examples,omitempty"`
	RouteHints        map[string]string `json:"route_hints,omitempty"`
	EntitlementStatus string            `json:"entitlement_status,omitempty"`
	UsagePricing      any               `json:"skill_usage_points,omitempty"`
}

// BuildRouterUserPrompt 组装用户侧 Prompt：目录摘要 + 用户输入。
// 目录只带路由所需字段（docs/02 §15：Catalog summary 限字段、候选数量上限）。
func BuildRouterUserPrompt(input Input) (string, error) {
	entries := make([]routerCatalogEntry, 0, len(input.Catalog))
	for index, skill := range input.Catalog {
		if index >= 20 {
			break
		}
		entry := routerCatalogEntry{
			SkillID:           skill.SkillID,
			SkillName:         skill.SkillName,
			SkillSource:       skillSource(skill),
			ListingID:         skill.ListingID,
			Status:            skill.Status,
			OutputTypes:       skill.SupportedOutputTypes,
			RoutingExamples:   limitStrings(skill.RoutingExamples, 3),
			RouteHints:        routerPromptRouteHints(skill.RouteHints),
			EntitlementStatus: entitlementStatus(skill),
		}
		if value, ok := skill.PricingSummary["skill_usage_points"]; ok {
			entry.UsagePricing = value
		}
		entries = append(entries, entry)
	}
	catalogJSON, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "run_intent: %s\n", strings.TrimSpace(input.RunIntent))
	if input.SelectedSkillID != "" || input.SelectedListingID != "" {
		fmt.Fprintf(&builder, "user_selected_skill_id: %s\nuser_selected_listing_id: %s\n", input.SelectedSkillID, input.SelectedListingID)
	}
	fmt.Fprintf(&builder, "skill_catalog: %s\n", string(catalogJSON))
	// 用户输入必须以引用块隔离，防止提示注入改变路由规则（quoted_user_input=true）。
	fmt.Fprintf(&builder, "user_input:\n\"\"\"\n%s\n\"\"\"", strings.TrimSpace(input.UserInput))
	return builder.String(), nil
}

func routerPromptRouteHints(hints map[string]string) map[string]string {
	if len(hints) == 0 {
		return nil
	}
	filtered := make(map[string]string, len(hints))
	for key, value := range hints {
		normalizedKey := strings.TrimSpace(key)
		switch normalizedKey {
		case "keyword", "keywords", "negative_keyword", "negative_keywords":
			continue
		}
		if strings.TrimSpace(value) != "" {
			filtered[normalizedKey] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func limitStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}
