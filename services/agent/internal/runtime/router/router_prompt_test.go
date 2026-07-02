package router

import (
	"strings"
	"testing"
)

func TestBuildRouterUserPromptFiltersLegacyKeywordHints(t *testing.T) {
	prompt, err := BuildRouterUserPrompt(Input{
		UserInput: "做个 MV",
		RunIntent: "normal",
		Catalog: []CatalogSkill{{
			SkillID:        "skill_video",
			SkillName:      "视频 Skill",
			SkillSource:    "system_default",
			Status:         "published",
			RouteHints:     map[string]string{"keywords": "MV,视频", "negative_keywords": "邮件", "intent_examples": "做一支音乐视频"},
			PricingSummary: map[string]any{},
		}},
	})
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(prompt, "keywords") || strings.Contains(prompt, "negative_keywords") {
		t.Fatalf("legacy keyword hints must not be sent to router prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "intent_examples") {
		t.Fatalf("non-keyword route hints should remain in prompt:\n%s", prompt)
	}
}
