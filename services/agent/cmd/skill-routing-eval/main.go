package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skill"
)

type skillSummaryJSON struct {
	SkillID    string            `json:"skill_id"`
	SkillName  string            `json:"skill_name"`
	SkillScope string            `json:"skill_scope"`
	Version    string            `json:"version"`
	Status     string            `json:"status"`
	RouteHints map[string]string `json:"route_hints"`
}

type scenario struct {
	Name     string
	Prompt   string
	Expected string
}

type result struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Reason   string `json:"reason"`
	Pass     bool   `json:"pass"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return errors.New("usage: skill-routing-eval <skills-jsonl>")
	}
	skills, err := loadSkills(args[0])
	if err != nil {
		return err
	}
	results, pass := evaluate(skills, defaultScenarios())
	accuracy := 0.0
	if len(results) > 0 {
		accuracy = float64(pass) / float64(len(results))
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"total":    len(results),
		"pass":     pass,
		"accuracy": fmt.Sprintf("%.2f", accuracy),
		"results":  results,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(encoded))
	if pass != len(results) {
		return fmt.Errorf("skill routing eval failed: %d/%d passed", pass, len(results))
	}
	return nil
}

func loadSkills(path string) ([]skill.Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []skill.Summary
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item skillSummaryJSON
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("decode skill summary: %w", err)
		}
		out = append(out, skill.Summary{
			SkillID:    item.SkillID,
			SkillName:  item.SkillName,
			SkillScope: item.SkillScope,
			Version:    item.Version,
			Status:     item.Status,
			RouteHints: item.RouteHints,
		})
	}
	return out, scanner.Err()
}

func evaluate(candidates []skill.Summary, scenarios []scenario) ([]result, int) {
	router := skill.NewRouter()
	results := make([]result, 0, len(scenarios))
	pass := 0
	for _, item := range scenarios {
		route := router.Route(item.Prompt, candidates)
		actual := ""
		if route.Matched {
			actual = route.Skill.SkillID
		}
		ok := actual == item.Expected
		if ok {
			pass++
		}
		results = append(results, result{Name: item.Name, Expected: item.Expected, Actual: actual, Reason: route.Reason, Pass: ok})
	}
	return results, pass
}

func defaultScenarios() []scenario {
	return []scenario{
		{Name: "storyboard_cn", Expected: "sk_seed_storyboard", Prompt: "请给城市香水做一个30秒广告短片，包含三条分镜建议和风险提醒"},
		{Name: "storyboard_visual_plan", Expected: "sk_seed_storyboard", Prompt: "帮我做高端护肤新品主视觉方案，要有镜头氛围和故事板"},
		{Name: "storyboard_en", Expected: "sk_seed_storyboard", Prompt: "Create a storyboard for a product launch video with 3 shots"},
		{Name: "product_copy_cn", Expected: "sk_seed_product_copy", Prompt: "帮我写一版小红书风格的护肤品种草文案，包含标题、卖点和CTA"},
		{Name: "product_copy_detail_page", Expected: "sk_seed_product_copy", Prompt: "给这款便携咖啡机生成电商详情页卖点和短标题"},
		{Name: "brand_strategy_cn", Expected: "sk_seed_brand_strategy", Prompt: "为新中式茶饮品牌做定位策略，包含人群、差异化和品牌语气"},
		{Name: "brand_strategy_en", Expected: "sk_seed_brand_strategy", Prompt: "Create brand positioning and tone of voice for a premium skincare startup"},
		{Name: "social_calendar_cn", Expected: "sk_seed_social_calendar", Prompt: "帮我规划下个月抖音和小红书内容日历，按每周主题输出"},
		{Name: "seo_article_cn", Expected: "sk_seed_seo_article", Prompt: "写一篇关于家用投影仪选购的SEO文章大纲，包含关键词和小标题"},
		{Name: "meeting_summary_cn", Expected: "sk_seed_meeting_summary", Prompt: "把这段会议纪要整理成决议、待办和负责人列表"},
		{Name: "customer_support_reply", Expected: "sk_seed_support_reply", Prompt: "客户投诉物流延迟，帮我写客服回复话术并给出补偿建议"},
		{Name: "data_insight_cn", Expected: "sk_seed_data_insight", Prompt: "根据这组转化率和客单价数据，输出经营分析和优化建议"},
		{Name: "image_prompt_cn", Expected: "sk_seed_image_prompt", Prompt: "给高级感香水海报生成一组MJ提示词，包含构图、光影和材质"},
		{Name: "storyboard_vs_copy", Expected: "sk_seed_storyboard", Prompt: "不是单纯写卖点文案，我要一个新品广告片分镜脚本"},
		{Name: "copy_vs_brand", Expected: "sk_seed_product_copy", Prompt: "不要做品牌定位，只写一条直播间转化短文案"},
		{Name: "brand_vs_social", Expected: "sk_seed_brand_strategy", Prompt: "先别排社媒日历，帮我确定这个咖啡品牌的目标人群和差异化"},
		{Name: "seo_vs_social", Expected: "sk_seed_seo_article", Prompt: "不是发朋友圈，帮我写可被搜索收录的长文结构和SEO关键词"},
		{Name: "summary_vs_support", Expected: "sk_seed_meeting_summary", Prompt: "请整理客服复盘会议的纪要，提取待办，而不是回复客户"},
		{Name: "support_vs_copy", Expected: "sk_seed_support_reply", Prompt: "不用写营销文案，帮我回复一位要求退款的用户"},
		{Name: "data_vs_seo", Expected: "sk_seed_data_insight", Prompt: "这些不是SEO关键词，是投放点击率、转化率和ROI数据，请做分析"},
		{Name: "prompt_vs_storyboard", Expected: "sk_seed_image_prompt", Prompt: "不要三幕剧情分镜，只要一组可直接用于出图的海报提示词"},
		{Name: "email_negative", Expected: "", Prompt: "帮我写一封给客户的道歉邮件，语气诚恳一些"},
		{Name: "invoice_negative", Expected: "", Prompt: "帮我整理这张发票的报销说明"},
		{Name: "generic_chat", Expected: "", Prompt: "今天适合做什么运动？"},
	}
}
