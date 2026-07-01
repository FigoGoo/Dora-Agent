package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunEvaluatesDefaultScenariosWithProductionRouter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.jsonl")
	if err := os.WriteFile(path, []byte(seedSkillJSONL()), 0o600); err != nil {
		t.Fatalf("write skills jsonl: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{path}, &out); err != nil {
		t.Fatalf("run eval: %v\noutput:\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"pass": 24`)) {
		t.Fatalf("expected 24 passing scenarios, got:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"accuracy": "1.00"`)) {
		t.Fatalf("expected full accuracy, got:\n%s", out.String())
	}
}

func seedSkillJSONL() string {
	return `{"skill_id":"sk_seed_storyboard","skill_name":"Storyboard","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"storyboard","keywords":"storyboard,故事板,分镜,镜头,广告短片,广告片,视觉方案,主视觉,product launch video","priority":"80","negative_keywords":"邮件,道歉信,合同,发票,提示词,mj,prompt,关键词,seo,报销"}}
{"skill_id":"sk_seed_product_copy","skill_name":"商品文案","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"product_copy","keywords":"商品文案,种草文案,卖点,详情页,短标题,直播间,转化短文案,cta,标题,电商文案,小红书风格","priority":"70","negative_keywords":"分镜,故事板,品牌定位,定位策略,会议纪要,客服,投诉,退款,seo,关键词,数据分析"}}
{"skill_id":"sk_seed_brand_strategy","skill_name":"品牌策略","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"brand_strategy","keywords":"品牌策略,品牌定位,定位策略,目标人群,差异化,品牌语气,人群,brand positioning,tone of voice,brand strategy","priority":"75","negative_keywords":"分镜,故事板,详情页,短标题,社媒日历,内容日历,会议纪要,客服回复,退款,seo文章"}}
{"skill_id":"sk_seed_social_calendar","skill_name":"社媒内容日历","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"social_calendar","keywords":"社媒日历,内容日历,选题日历,抖音,小红书,公众号排期,每周主题,发布计划,social calendar,content calendar","priority":"68","negative_keywords":"品牌定位,目标人群,seo,搜索收录,会议纪要,客服,退款,发票"}}
{"skill_id":"sk_seed_seo_article","skill_name":"SEO 长文","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"seo_article","keywords":"seo,SEO,搜索收录,关键词,长文结构,文章大纲,小标题,选购指南,搜索排名,search keywords","priority":"72","negative_keywords":"社媒日历,朋友圈,分镜,故事板,投放点击率,转化率,roi,会议纪要,客服"}}
{"skill_id":"sk_seed_meeting_summary","skill_name":"会议纪要整理","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"meeting_summary","keywords":"会议纪要,会议总结,复盘会议,决议,待办,负责人,行动项,纪要整理,meeting notes,action items","priority":"73","negative_keywords":"回复客户,客服回复,营销文案,分镜,故事板,seo,海报,提示词"}}
{"skill_id":"sk_seed_support_reply","skill_name":"客服回复","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"support_reply","keywords":"客服回复,客服话术,客户投诉,物流延迟,补偿建议,退款,售后,用户投诉,客诉,customer support","priority":"74","negative_keywords":"会议纪要,复盘会议,品牌定位,商品文案,营销文案,seo,分镜,故事板"}}
{"skill_id":"sk_seed_data_insight","skill_name":"经营数据分析","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"data_insight","keywords":"数据分析,经营分析,转化率,客单价,点击率,roi,ROI,投放数据,优化建议,指标解读,data insight","priority":"76","negative_keywords":"seo关键词,搜索收录,会议纪要,客服回复,分镜,故事板,提示词,海报"}}
{"skill_id":"sk_seed_image_prompt","skill_name":"出图提示词","skill_scope":"public","version":"published","status":"published","route_hints":{"intent":"image_prompt","keywords":"出图提示词,提示词,mj,MJ,midjourney,prompt,海报提示词,构图,光影,材质,风格词,negative prompt","priority":"78","negative_keywords":"分镜,故事板,广告片,剧情,会议纪要,客服,seo文章,品牌定位"}}
`
}
