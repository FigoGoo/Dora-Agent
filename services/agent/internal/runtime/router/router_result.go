package router

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

// Result 是路由运行时的完整输出：RouterDecision 契约 + 面向用户的澄清话术。
// clarify 话术不进入 RouterDecision.v1 冻结契约，只用于 assistant 消息与控件渲染。
type Result struct {
	Decision           foundation.RouterDecision
	ClarifyQuestion    string
	SuggestedQuestions []string
	Source             string // "llm" | "mock"
}

// Router 是意图路由统一入口；docs/02-M1 约定实现为 ChatModel 结构化输出 + 确定性 Guard。
// 第三方模型不可用时降级到 mock 结构化路由，不允许直接失败。
type Router interface {
	Route(ctx context.Context, input Input) (Result, error)
}

// Route 让 mock 路由实现 Router 接口，作为 LLM 路由的无第三方依赖降级。
func (r ChatModelRouter) Route(ctx context.Context, input Input) (Result, error) {
	decision, err := r.Decide(ctx, input)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Decision:           decision,
		ClarifyQuestion:    humanClarifyQuestion(decision),
		SuggestedQuestions: nil,
		Source:             "mock",
	}, nil
}

var missingFieldQuestions = map[string]string{
	"creative_goal":          "你想创作什么内容？可以说说主题、想要的风格和用在什么场合。",
	"product_name":           "这次要宣传的产品或对象是什么？",
	"duration_sec":           "期望的时长大概多少秒？",
	"target_platform":        "打算投放在什么平台（抖音、朋友圈、大屏等）？",
	"available_skill_choice": "你选择的能力暂时不可用，要换一个方向继续吗？",
	"style":                  "想要什么样的风格或氛围？",
	"city_or_destination":    "主题城市或目的地是哪里？",
}

// humanClarifyQuestion 把 missing_fields 转成面向用户的引导性提问，禁止直出内部字段名。
func humanClarifyQuestion(decision foundation.RouterDecision) string {
	if decision.Decision != foundation.RouterDecisionClarify {
		return ""
	}
	questions := make([]string, 0, 3)
	for _, field := range decision.MissingFields {
		if question, ok := missingFieldQuestions[field]; ok {
			questions = append(questions, question)
		}
		if len(questions) == 3 {
			break
		}
	}
	if len(questions) == 0 {
		return "我需要再了解一点创作信息：你想做什么内容、偏好什么风格、准备用在哪里？"
	}
	return strings.Join(questions, " ")
}
