package guide

import (
	"fmt"
	"sort"
	"strings"
)

const SchemaVersionCreativeGuideOutput = "creative_guide_output.v1"

type CatalogSkill struct {
	SkillID              string
	SkillName            string
	SkillSource          string
	Status               string
	SupportedOutputTypes []string
	RoutingExamples      []string
}

type SuggestedPrompt struct {
	PromptID   string `json:"prompt_id"`
	Label      string `json:"label"`
	Text       string `json:"text"`
	OutputType string `json:"output_type,omitempty"`
}

type CreativeGuideOutput struct {
	SchemaVersion        string            `json:"schema_version"`
	GuideID              string            `json:"guide_id"`
	SessionID            string            `json:"session_id"`
	SuggestedPrompts     []SuggestedPrompt `json:"suggested_prompts"`
	SupportedOutputTypes []string          `json:"supported_output_types"`
	DefaultActions       []string          `json:"default_actions"`
}

func Build(sessionID string, catalog []CatalogSkill) CreativeGuideOutput {
	suggestions := make([]SuggestedPrompt, 0, 3)
	outputTypes := map[string]struct{}{}
	for _, skill := range catalog {
		if skill.Status != "published" || skill.SkillID == "" {
			continue
		}
		for _, outputType := range skill.SupportedOutputTypes {
			if outputType = strings.TrimSpace(outputType); outputType != "" {
				outputTypes[outputType] = struct{}{}
			}
		}
		if len(suggestions) >= 3 || skill.SkillID == "skill_generic_creation" {
			continue
		}
		text := firstNonEmpty(skill.RoutingExamples...)
		if text == "" {
			text = fmt.Sprintf("使用%s开始创作", displaySkillName(skill))
		}
		suggestions = append(suggestions, SuggestedPrompt{
			PromptID:   "prompt_" + skill.SkillID,
			Label:      displaySkillName(skill),
			Text:       text,
			OutputType: firstNonEmpty(skill.SupportedOutputTypes...),
		})
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, SuggestedPrompt{
			PromptID: "prompt_generic_creation",
			Label:    "自由创作",
			Text:     "帮我整理一个创作 brief，并推荐适合的下一步",
		})
	}
	supported := make([]string, 0, len(outputTypes))
	for value := range outputTypes {
		supported = append(supported, value)
	}
	sort.Strings(supported)
	if len(supported) == 0 {
		supported = []string{"brief", "prompt"}
	}
	return CreativeGuideOutput{
		SchemaVersion:        SchemaVersionCreativeGuideOutput,
		GuideID:              "guide_" + sessionID,
		SessionID:            sessionID,
		SuggestedPrompts:     suggestions,
		SupportedOutputTypes: supported,
		DefaultActions:       []string{"free_creation", "skill_marketplace"},
	}
}

func displaySkillName(skill CatalogSkill) string {
	if strings.TrimSpace(skill.SkillName) != "" {
		return strings.TrimSpace(skill.SkillName)
	}
	return strings.TrimSpace(skill.SkillID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
