package skill

import "testing"

func TestRouteMatchesRealScenarioKeywords(t *testing.T) {
	router := NewRouter()
	candidates := []Summary{
		{
			SkillID: "sk_storyboard", SkillName: "故事板 Skill", Status: "published",
			RouteHints: map[string]string{
				"keywords": "广告,短片,分镜,故事板,镜头",
				"priority": "80",
			},
		},
		{
			SkillID: "sk_product_copy", SkillName: "商品文案 Skill", Status: "published",
			RouteHints: map[string]string{
				"keywords": "商品文案,卖点,详情页,标题",
				"priority": "50",
			},
		},
	}

	result := router.Route("请给城市香水做一个30秒广告短片，包含三条分镜建议", candidates)
	if !result.Matched || result.Skill.SkillID != "sk_storyboard" || result.Reason != "route_hint:keywords" {
		t.Fatalf("unexpected route: %#v", result)
	}
}

func TestRouteUsesPriorityWhenMultipleSkillsMatch(t *testing.T) {
	router := NewRouter()
	candidates := []Summary{
		{SkillID: "sk_low", Status: "published", RouteHints: map[string]string{"keywords": "广告", "priority": "10"}},
		{SkillID: "sk_high", Status: "published", RouteHints: map[string]string{"keywords": "广告,分镜", "priority": "90"}},
	}

	result := router.Route("做一个广告分镜", candidates)
	if !result.Matched || result.Skill.SkillID != "sk_high" {
		t.Fatalf("expected high priority route, got %#v", result)
	}
}

func TestRouteHonorsNegativeKeywords(t *testing.T) {
	router := NewRouter()
	candidates := []Summary{
		{SkillID: "sk_image", Status: "published", RouteHints: map[string]string{"keywords": "广告,图片", "negative_keywords": "视频,短片"}},
		{SkillID: "sk_video", Status: "published", RouteHints: map[string]string{"keywords": "广告,视频,短片"}},
	}

	result := router.Route("做一个广告短片脚本", candidates)
	if !result.Matched || result.Skill.SkillID != "sk_video" {
		t.Fatalf("expected video route, got %#v", result)
	}
}

func TestRouteIgnoresNegativeKeywordInNegatedContext(t *testing.T) {
	router := NewRouter()
	candidates := []Summary{
		{
			SkillID: "sk_product_copy", Status: "published",
			RouteHints: map[string]string{
				"keywords":          "直播间,转化短文案",
				"negative_keywords": "品牌定位,定位策略",
				"priority":          "70",
			},
		},
		{
			SkillID: "sk_brand", Status: "published",
			RouteHints: map[string]string{
				"keywords": "品牌定位,定位策略",
				"priority": "75",
			},
		},
	}

	result := router.Route("不要做品牌定位，只写一条直播间转化短文案", candidates)
	if !result.Matched || result.Skill.SkillID != "sk_product_copy" {
		t.Fatalf("negated negative keyword should not block target route: %#v", result)
	}
}

func TestRouteDoesNotDefaultToUnrelatedPublishedSkill(t *testing.T) {
	router := NewRouter()
	candidates := []Summary{
		{SkillID: "sk_e2e", Status: "published", RouteHints: map[string]string{"keyword": "agent-e2e-skill-20260629154450"}},
		{SkillID: "sk_storyboard", Status: "published", RouteHints: map[string]string{"keywords": "故事板,分镜"}},
	}

	result := router.Route("帮我写一封给客户的道歉邮件", candidates)
	if result.Matched || result.Reason != "no_route_hint_match" {
		t.Fatalf("unrelated prompt should not route to a random skill: %#v", result)
	}
}
