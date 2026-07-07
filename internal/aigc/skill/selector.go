package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// 哨兵错误（与仓库约定一致：errors.New 值 + errors.Is 比较）。
var (
	// ErrNoSkillOptions 表示没有可供选择的候选 Skill。
	ErrNoSkillOptions = errors.New("no skill options")
	// ErrSkillSelectionParse 表示模型输出无法解析为期望的 JSON。
	ErrSkillSelectionParse = errors.New("skill selection parse failed")
	// ErrUnknownSkillID 表示模型选出的 id 不在候选范围内。
	ErrUnknownSkillID = errors.New("skill selector returned unknown id")
)

// SkillOption 是一个候选 Skill 的最小描述（供选择用）。
type SkillOption struct {
	ID          string
	Name        string
	Description string
}

// SkillSelection 是一次选择的结果。
type SkillSelection struct {
	SkillID  string
	Reason   string // 为什么选它（透传给前端）
	Fallback bool   // 是否走了兜底（本接口不置位，由调用方兜底逻辑设置）
}

// SkillSelector 只负责“从候选里选哪个”，不做绑定、不发事件、不碰 session。
type SkillSelector interface {
	Select(ctx context.Context, brief string, options []SkillOption) (SkillSelection, error)
}

// chatModel 是 selector 需要的最小聊天模型能力，einomodel.BaseChatModel 天然满足。
type chatModel interface {
	Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

type llmSkillSelector struct {
	model chatModel
}

// NewLLMSkillSelector 用一个聊天模型构造 LLM 版选择器。
func NewLLMSkillSelector(m chatModel) SkillSelector {
	return &llmSkillSelector{model: m}
}

func (s *llmSkillSelector) Select(ctx context.Context, brief string, options []SkillOption) (SkillSelection, error) {
	if len(options) == 0 {
		return SkillSelection{}, ErrNoSkillOptions
	}
	var b strings.Builder
	b.WriteString("你是一个短视频创作 Skill 路由器。根据用户诉求，从候选 Skill 中选出最合适的一个。\n")
	b.WriteString("只能从下列候选的 id 中选择，必须返回严格 JSON：{\"skill_id\":\"<候选id>\",\"reason\":\"<不超过30字理由>\"}，不要输出任何其它内容。\n\n")
	b.WriteString("用户诉求：\n")
	b.WriteString(brief)
	b.WriteString("\n\n候选 Skill：\n")
	for _, o := range options {
		fmt.Fprintf(&b, "- id=%s | 名称=%s | 说明=%s\n", o.ID, o.Name, o.Description)
	}

	msg, err := s.model.Generate(ctx, []*schema.Message{schema.UserMessage(b.String())})
	if err != nil {
		return SkillSelection{}, fmt.Errorf("skill selector generate: %w", err)
	}

	raw := extractJSONObject(msg.Content)
	var parsed struct {
		SkillID string `json:"skill_id"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return SkillSelection{}, fmt.Errorf("%w: %q: %v", ErrSkillSelectionParse, msg.Content, err)
	}
	for _, o := range options {
		if o.ID == parsed.SkillID {
			return SkillSelection{SkillID: parsed.SkillID, Reason: strings.TrimSpace(parsed.Reason)}, nil
		}
	}
	return SkillSelection{}, fmt.Errorf("%w: %q", ErrUnknownSkillID, parsed.SkillID)
}

// extractJSONObject 从可能含前后缀文本的模型输出里截取 JSON 对象。
// 假设输出中只有一个 JSON 对象，取最外层大括号区间（首个 { 到末个 }）。
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
